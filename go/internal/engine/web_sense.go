// web_sense.go is the reach=world OUTWARD-perception sensor for the cognitive power-cycle (power-cycle
// follow-up #15 — fetch_web, the OUTWARD half of grounded sensing that complements the shipped INWARD
// sensors read_clock / read_host / read_event_log). It adds the one distal sense the orientation pass
// can fold in beyond the engine's own state + the clock: "what is happening in the world right now".
//
// THE SEAM + DETERMINISM. A real web/news read reaches OUTSIDE the harness (the network) and is the most
// non-deterministic sensor of all, so it enters EXACTLY like the clock and the host footprint do —
// through the INJECTED web.Web seam (web.Wall at the edge, web.Fake in tests). The engine never calls
// net/http directly (headless-pure); the only network read lives in web.Wall, constructed at the edge.
// And like read_clock, the sensed SNIPPET rides the replayable PERCEPT-LOG: a LIVE run RECORDS the fetch
// (e.web.Fetch) once and persists it; a GOLDEN/resumed run REPLAYS the logged snippet for the (tick,
// "web") key, so a replay is deterministic even though a live fetch would differ run-to-run. The Fake
// returns a FIXED snippet offline, so the suite is byte-stable.
//
// BUDGETED (resolved Fork 2 — sensing autonomy OFF/budgeted). A distal sense is bounded: the fetch fires
// AT MOST ONCE per episode-open (e.webSensedEpisode), never per tick. startEpisode resets the guard and
// calls senseWeb once; the result enters the orientation pass alongside the clock/self/host.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. senseWeb fires only when sense.web is ON AND a Web is wired AND it has
// not already fetched this episode. The sense.web knob DEFAULTS OFF (unlike the other senses) — web
// touches the network + costs, so it is opt-in + budgeted. Default OFF / nil web ⇒ no fetch, no percept
// entry, no perception.web event ⇒ the live loop is byte-identical to the web-blind engine. Mirrors the
// senseClock / senseHost gating shape.
//
// HEADLESS-PURE. No net/http (only the injected e.web); no I/O (only the injected Store via the
// percept-log); no wall clock, no unseeded randomness.
package engine

import (
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
	webpkg "github.com/berttrycoding/thought-harness/internal/web"
)

// perceptWebKind names the fetch_web sensor in the percept-log (the entry's Kind) — the string key under
// which a recorded web snippet is logged + replayed, distinct from perceptClockKind ("clock").
const perceptWebKind = "web"

// SetWeb wires the outward web/news seam: w is the web sensor (webpkg.Wall at the edge, webpkg.Fake in
// tests; nil reverts to web-blind). Call before Process/Run; the fetch is taken at episode-open (a
// boot-time read), never on a hot tick. Mirrors SetClock / SetHost.
func (e *Engine) SetWeb(w webpkg.Web) { e.web = w }

// SetPageFetcher wires the outward page-FETCH seam for the model-callable fetch_url tool
// (subconscious.fetch_url, T1.4): p is the page fetcher (webpkg.Pager at the edge, webpkg.FakePager in
// tests; nil reverts to page-blind). Call before Run (the SetWeb-before-Run contract); the fetch_url tool
// reads it lazily at dispatch time. Mirrors SetWeb. The engine carries no net/http itself.
func (e *Engine) SetPageFetcher(p webpkg.PageFetcher) { e.pageFetcher = p }

// senseWebEnabled reports whether the fetch_web sensor may fire: the opt-in sense.web knob is ON AND a
// Web is wired. nil features / nil web ⇒ false (the web-blind default), so the bare path never reaches
// the sensor and stays byte-identical. Mirrors senseEnabled (the clock gate) + senseHostEnabled.
func (e *Engine) senseWebEnabled() bool {
	return e.features != nil && e.features.Sense.Web && e.web != nil
}

// webLookup reports whether the synthesiser may produce the lookup-RESEARCH shape — i.e. whether the
// subconscious.web_search knob is ON. It is the gate the engine threads into cognition.SynthesizeWeb so a
// factual lookup question that hit no other shape gets a program that STAFFS expose-affordances (the
// operator the engine granted the web_search tool above). DISTINCT from senseWebEnabled (the fetch_web
// AMBIENT sensor): this gates the on-demand model-callable web_search TOOL's staffing, and unlike the
// sensor it does NOT require a wired Web seam at synthesis time — the seam is checked at dispatch
// (floorToolCall is the second gate; a blind read errors honestly). Default OFF ⇒ false ⇒ no lookup shape.
func (e *Engine) webLookup() bool {
	return e.features != nil && e.features.Subconscious.WebSearch
}

// queryFormulation reports whether a staffed web_search worker FORMULATES its query from the actual
// question (strips a leading instruction/wrapper clause) instead of searching the whole goal verbatim —
// i.e. whether the subconscious.query_formulation knob is ON (T1.1; FLARE arXiv:2305.06983). The engine
// threads it onto every workflow it staffs (Workflow.WithQueryFormulation) at each SetWorkflow site, so the
// gate reaches each SubAgent's web_search branch. Default OFF ⇒ false ⇒ the query is the trimmed goal,
// byte-identical. Inert unless web_search also fires (it only changes the query string of a web_search call).
func (e *Engine) queryFormulation() bool {
	return e.features != nil && e.features.Subconscious.QueryFormulation
}

// senseWeb is the fetch_web reach=world OUTWARD sensor. When sensing is disabled (knob off / nil web) it
// is a NO-OP (returns zero, false) — no fetch, no log entry, no event, byte-identical. BUDGETED: it
// fetches at most ONCE per episode-open (the e.webSensedEpisode guard, reset in startEpisode); a second
// call within the same episode is a no-op. When enabled and not yet fetched this episode:
//   - REPLAY mode (a version/substrate-matching log holds this (tick, "web")): return the LOGGED snippet
//     (deterministic even if a live fetch would differ) — the golden/resume path.
//   - RECORD mode: do the live e.web.Fetch, encode the snippet, APPEND it to the percept-log, and return
//     it — the live path that captures reality once for later replay.
//
// Either way one perception.web event is emitted carrying {value, mode, tick, ok, source}, so the sense
// is visible on the bus. A FAILED/blind live read (Result.OK=false) is still RECORDED (an empty snippet
// is a valid, replayable percept — "the world was unreadable at this tick"), so the replay is faithful;
// the orientation fold only voices a snippet when OK is true. Deterministic offline because e.web is
// web.Fake (a fixed snippet), exactly as the seeded RNG gives deterministic randomness.
func (e *Engine) senseWeb(tick int) (webpkg.Result, bool) {
	if !e.senseWebEnabled() || e.webSensedEpisode {
		return webpkg.Result{}, false
	}
	// Burn the per-episode budget up front: the fetch is attempted at most once per episode-open, whether
	// it replays or records (a budgeted distal sense, resolved Fork 2).
	e.webSensedEpisode = true

	key := perceptKey(tick, perceptWebKind)
	if e.perceptReplayOK {
		if v, ok := e.perceptReplay[key]; ok {
			res := decodeWebPercept(v)
			e.emitWebSense(res, "replay", tick)
			return res, true
		}
	}
	// RECORD: do the live fetch once and log the snippet (the only e.web read in the sensor path). A blind/
	// failed read (OK=false) records an empty snippet — a valid, replayable "unreadable" percept.
	res := e.web.Fetch(e.webQuery())
	e.perceptLog = append(e.perceptLog, persist.PerceptEntry{Tick: tick, Kind: perceptWebKind, Value: encodeWebPercept(res)})
	e.emitWebSense(res, "record", tick)
	return res, true
}

// webQuery is the deterministic, templated cue passed to the web seam — the engine's current goal (a
// short "current events" hint), or a stable default when there is no goal yet. It draws NO RNG / clock /
// backend, so the recorded percept key + the query are reproducible. The Fake ignores the query (its
// value is fixed), so this only shapes a live Wall fetch.
func (e *Engine) webQuery() string {
	if e.graph != nil {
		if g := e.graph.Goal; g != "" {
			return runeSlice(g, 80)
		}
	}
	return "current events"
}

// emitWebSense emits the perception.web event (the sense's witness on the bus). It carries the exact
// snippet + ok/source so a TUI/trace + the correctness-oracle test read the true outward reality. A
// failed read (ok=false) still emits — the sense fired; it just had nothing to voice.
func (e *Engine) emitWebSense(res webpkg.Result, mode string, tick int) {
	summary := "fetch_web [" + mode + "]: "
	if res.OK {
		summary += runeSlice(res.Text, 80)
	} else {
		summary += "(no reading)"
	}
	e.bus.Emit(events.PerceptionWeb, summary,
		events.D{"value": res.Text, "ok": res.OK, "source": res.Source, "mode": mode, "tick": tick})
}

// encodeWebPercept serialises a Result into the percept-log Value string. Only the SNIPPET text is the
// replayable payload (the OK/Source are derived from it on decode: a non-empty snippet means a real
// read). This keeps the log entry a plain string (the same shape every sensor uses) and round-trips
// byte-identically. The Fake's fixed snippet is what the replay test asserts.
func encodeWebPercept(res webpkg.Result) string {
	if !res.OK {
		return "" // a blind/failed read logs an empty snippet (a valid "unreadable" percept)
	}
	return res.Text
}

// decodeWebPercept reconstructs a Result from a logged Value: an empty snippet replays as a blind read
// (OK=false), a non-empty snippet as a successful read (OK=true) sourced from the replay log. The Source
// is stamped "percept-log" so a replayed read is auditably distinct from a live fetch.
func decodeWebPercept(v string) webpkg.Result {
	if v == "" {
		return webpkg.Result{}
	}
	return webpkg.Result{Text: v, OK: true, Source: "percept-log"}
}

// lazyWeb is the LATE-BOUND web.Web seam the web_search TOOL is registered with (subconscious.web_search).
// The tool registry is built at NewEngine (buildExecutor), but the edge wires the live seam AFTER NewEngine
// via SetWeb (the SetWeb-before-Run contract). So the tool cannot capture the seam at construction — it
// captures the engine and reads e.web at Execute time. A nil e.web (no edge wired — the go-test / no-Web
// path) is a blind read (OK=false) ⇒ web_search returns IsError with no content ⇒ no fabricated grounding,
// best-effort. It carries no net/http itself (the only network read lives in the injected webpkg.Wall /
// webpkg.DuckDuckGo, constructed at the edge), so the engine stays headless-pure.
type lazyWeb struct{ e *Engine }

// Fetch delegates to the engine's currently-wired web seam, or returns a blind read when none is wired.
func (l lazyWeb) Fetch(query string) webpkg.Result {
	if l.e == nil || l.e.web == nil {
		return webpkg.Result{}
	}
	return l.e.web.Fetch(query)
}

// lazyPager is the LATE-BOUND web.PageFetcher seam the fetch_url TOOL is registered with
// (subconscious.fetch_url, T1.4) — the page-fetch twin of lazyWeb. The tool registry is built at NewEngine
// (buildExecutor), but the edge wires the live seam AFTER NewEngine via SetPageFetcher (the SetWeb-before-
// Run contract), so the tool captures the engine and reads e.pageFetcher at Execute time. A nil
// e.pageFetcher (no edge wired — the go-test / no-seam path) is a blind read (OK=false) ⇒ fetch_url returns
// IsError with no content ⇒ no fabricated grounding, best-effort. It carries no net/http itself (the only
// network read lives in the injected webpkg.Pager, constructed at the edge), so the engine stays
// headless-pure.
type lazyPager struct{ e *Engine }

// FetchPage delegates to the engine's currently-wired page-fetch seam, or returns a blind read when none
// is wired.
func (l lazyPager) FetchPage(url string) webpkg.Result {
	if l.e == nil || l.e.pageFetcher == nil {
		return webpkg.Result{}
	}
	return l.e.pageFetcher.FetchPage(url)
}
