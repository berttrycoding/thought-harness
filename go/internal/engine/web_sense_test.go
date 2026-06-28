package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
	webpkg "github.com/berttrycoding/thought-harness/internal/web"
)

// webSenseEngine builds a minimal engine with the sense.web knob set, a Fake web seam returning a fixed
// snippet, and (optionally) a backing Store — the smallest harness exercising the fetch_web sensor
// directly (mirrors senseEngine in percept_test.go).
func webSenseEngine(t *testing.T, store persist.Store, senseOn bool, snippet string, substrate string) *Engine {
	t.Helper()
	feats := config.AllOn()
	feats.Persist.Enabled = true
	feats.Sense.Web = senseOn
	e := &Engine{
		bus:          events.NewDefault(),
		features:     &feats,
		web:          &webpkg.Fake{R: webpkg.Result{Text: snippet, OK: snippet != "", Source: "fake"}},
		backendLabel: substrate,
	}
	e.cfg.Store = store
	return e
}

// TestSenseWebOffIsNoOp: with sense.web OFF, senseWeb is a pure no-op — no fetch, no percept-log entry,
// no perception.web event (the byte-identical default; the knob DEFAULTS OFF unlike the other senses).
func TestSenseWebOffIsNoOp(t *testing.T) {
	e := webSenseEngine(t, nil, false, "SNIPPET-OFF", "test")
	if res, ok := e.senseWeb(0); ok || res.OK || res.Text != "" {
		t.Fatalf("sense OFF: senseWeb = (%+v, %v), want (zero, false)", res, ok)
	}
	if len(e.perceptLog) != 0 {
		t.Fatalf("sense OFF: perceptLog grew to %d, want 0 (no fetch)", len(e.perceptLog))
	}
	if n := countKind(e.bus, events.PerceptionWeb); n != 0 {
		t.Fatalf("sense OFF: emitted %d perception.web events, want 0", n)
	}
}

// TestSenseWebNilSeamIsNoOp: even with sense.web ON, a nil Web seam keeps the engine web-blind — no
// fetch, no log, no event. (Sensing needs BOTH the knob and a wired seam — the double-gate.)
func TestSenseWebNilSeamIsNoOp(t *testing.T) {
	feats := config.AllOn()
	feats.Sense.Web = true
	e := &Engine{bus: events.NewDefault(), features: &feats, backendLabel: "test"} // web == nil
	if res, ok := e.senseWeb(0); ok || res.OK || res.Text != "" {
		t.Fatalf("nil seam: senseWeb = (%+v, %v), want (zero, false)", res, ok)
	}
	if len(e.perceptLog) != 0 || countKind(e.bus, events.PerceptionWeb) != 0 {
		t.Fatal("nil seam: a fetch/log/event happened, want none (web-blind)")
	}
}

// TestSenseWebRecordsFakeValue: with sense.web ON and a Fake seam, senseWeb RECORDS the fixed snippet
// into the percept-log under kind "web" and emits one perception.web [record] event carrying the value.
func TestSenseWebRecordsFakeValue(t *testing.T) {
	const snippet = "Calm wire, nothing urgent."
	e := webSenseEngine(t, nil, true, snippet, "test")
	res, ok := e.senseWeb(4)
	if !ok || !res.OK {
		t.Fatalf("sense ON: senseWeb = (%+v, %v), want a successful read", res, ok)
	}
	if res.Text != snippet {
		t.Fatalf("sensed snippet = %q, want the Fake's fixed %q", res.Text, snippet)
	}
	if len(e.perceptLog) != 1 || e.perceptLog[0].Tick != 4 || e.perceptLog[0].Kind != perceptWebKind || e.perceptLog[0].Value != snippet {
		t.Fatalf("perceptLog = %+v, want one web@4 entry holding the snippet", e.perceptLog)
	}
	var sawRecord bool
	for _, ev := range e.bus.Recent(10000, nil) {
		if ev.Kind == events.PerceptionWeb && ev.Data["mode"] == "record" {
			if ev.Data["value"] != snippet || ev.Data["ok"] != true {
				t.Fatalf("perception.web data = %+v, want value=%q ok=true", ev.Data, snippet)
			}
			sawRecord = true
		}
	}
	if !sawRecord {
		t.Fatal("no perception.web [record] event emitted")
	}
}

// TestSenseWebBudgetOncePerEpisode: the distal sense is BUDGETED (resolved Fork 2) — once the
// per-episode guard is burned, a second senseWeb in the same episode is a no-op (no second fetch / log /
// event). startEpisode resets the guard; here we assert the guard alone bounds repeated calls.
func TestSenseWebBudgetOncePerEpisode(t *testing.T) {
	e := webSenseEngine(t, nil, true, "ONE", "test")
	if _, ok := e.senseWeb(1); !ok {
		t.Fatal("first senseWeb should fire")
	}
	res2, ok2 := e.senseWeb(2)
	if ok2 || res2.OK || res2.Text != "" {
		t.Fatalf("second senseWeb in same episode = (%+v, %v), want a no-op (budget spent)", res2, ok2)
	}
	if len(e.perceptLog) != 1 {
		t.Fatalf("budget: perceptLog = %d entries, want exactly 1 (one fetch per episode-open)", len(e.perceptLog))
	}
	if n := countKind(e.bus, events.PerceptionWeb); n != 1 {
		t.Fatalf("budget: emitted %d perception.web events, want exactly 1", n)
	}
}

// TestWebPerceptRecordReplayReproducesSnippet is the load-bearing determinism test: record a fetch
// against a Fake at snippet A, persist; a NEW engine with a Fake at a DIFFERENT snippet B, with the
// loaded log in REPLAY mode, returns A (the logged snippet), not B. This is exactly what keeps a golden
// replay deterministic when a live fetch would differ.
func TestWebPerceptRecordReplayReproducesSnippet(t *testing.T) {
	dir := t.TempDir()
	const snippetA = "WORLD-AS-OF-RECORD-A"
	const snippetB = "A-DIFFERENT-LIVE-READ-B"

	// RECORD against Fake A and persist.
	storeA, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := webSenseEngine(t, storeA, true, snippetA, "test")
	resA, _ := a.senseWeb(2)
	if resA.Text != snippetA {
		t.Fatalf("recorded A = %q, want %q", resA.Text, snippetA)
	}
	a.savePerceptLog()

	// REPLAY: a fresh engine whose live Fake would return B loads the matching log and replays A.
	storeB, _ := persist.NewJSONLStore(dir)
	b := webSenseEngine(t, storeB, true, snippetB, "test")
	b.loadPerceptLog()
	if !b.perceptReplayOK {
		t.Fatal("replay log was not accepted (version/substrate matched, should replay)")
	}
	got, ok := b.senseWeb(2)
	if !ok {
		t.Fatal("replay senseWeb returned ok=false")
	}
	if got.Text != snippetA {
		t.Fatalf("replay snippet = %q, want the LOGGED A %q (not the live B %q)", got.Text, snippetA, snippetB)
	}
	if got.Source != "percept-log" {
		t.Fatalf("replay source = %q, want \"percept-log\" (a replayed read is auditably distinct)", got.Source)
	}
	// The replay path read the LOG, not the live seam: no new RECORD entry was appended.
	if len(b.perceptLog) != 0 {
		t.Fatalf("replay appended %d record entries, want 0 (replay reads the log)", len(b.perceptLog))
	}
	var sawReplay bool
	for _, ev := range b.bus.Recent(10000, nil) {
		if ev.Kind == events.PerceptionWeb && ev.Data["mode"] == "replay" {
			sawReplay = true
		}
	}
	if !sawReplay {
		t.Fatal("no perception.web [replay] event emitted")
	}
}

// TestWebBlindReadRecordsEmptyPercept: a FAILED live read (Fake at OK=false / empty snippet) still
// RECORDS a valid "unreadable" percept (empty value) and emits the witness with ok=false — so a replay
// is faithful (the world was unreadable at that tick), and the orientation fold voices nothing.
func TestWebBlindReadRecordsEmptyPercept(t *testing.T) {
	e := webSenseEngine(t, nil, true, "", "test") // snippet "" -> Fake OK=false
	res, ok := e.senseWeb(3)
	if !ok {
		t.Fatal("a blind read still FIRES the sensor (ok=true that it sensed), the Result.OK is false")
	}
	if res.OK || res.Text != "" {
		t.Fatalf("blind read = %+v, want (Text:\"\", OK:false)", res)
	}
	if len(e.perceptLog) != 1 || e.perceptLog[0].Value != "" {
		t.Fatalf("blind read percept = %+v, want one empty-value web entry", e.perceptLog)
	}
	for _, ev := range e.bus.Recent(10000, nil) {
		if ev.Kind == events.PerceptionWeb {
			if ev.Data["ok"] != false {
				t.Fatalf("blind perception.web ok = %v, want false", ev.Data["ok"])
			}
		}
	}
}

// TestOrientationTextWebOffByteIdentical: with the web read absent (webOK=false), the orientation text
// is EXACTLY the pre-fetch_web template — the "Current events:" clause is added ONLY on a successful
// read, so a default (web-blind) boot's orientation is byte-identical.
func TestOrientationTextWebOffByteIdentical(t *testing.T) {
	feats := config.AllOn()
	e := &Engine{bus: events.NewDefault(), features: &feats, backendLabel: "test"}
	self := selfState{Tick: 5, OpenLines: 2}

	base := e.orientationText("prior gist", "2026-01-01T00:00:00Z", true, self, webpkg.Result{}, false)
	withBlind := e.orientationText("prior gist", "2026-01-01T00:00:00Z", true, self, webpkg.Result{Text: "ignored", OK: false}, true)
	if base != withBlind {
		t.Fatalf("a blind/failed web read must not change the orientation text:\n base=%q\n blind=%q", base, withBlind)
	}
	if containsSubstr(base, "Current events:") {
		t.Fatalf("web-off orientation text must NOT carry a 'Current events:' clause: %q", base)
	}

	// a SUCCESSFUL read DOES fold the snippet in (and only then).
	withWeb := e.orientationText("prior gist", "2026-01-01T00:00:00Z", true, self, webpkg.Result{Text: "Quiet news day.", OK: true, Source: "fake"}, true)
	if !containsSubstr(withWeb, "Current events: Quiet news day.") {
		t.Fatalf("web-on orientation text must carry the snippet: %q", withWeb)
	}
}

// containsSubstr is a tiny local helper (avoids importing strings into the test for one check).
func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
