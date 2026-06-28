// orient.go is the ORIENTATION PASS — the read-current-state mechanism that re-grounds BOTH layers on
// the first wake of a resumed session (cognitive power-cycle, Track 3; proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §5 + §11 Track 3;
// red-team amendment 4 — the gate is a CORRECTNESS ORACLE, not a firing count).
//
// WHAT IT DOES. On the FIRST wake, when the sense.orient knob is ON AND this is a resume boot (a prior
// compressed spine was rehydrated OR clock-sensing is enabled), it:
//
//	(a) injects ONE re-grounding THOUGHT into the conscious stream — "Resuming. Prior focus: <gist>.
//	    Current time: <clock>. <self-state>." — as a GENERATED source, so the Filter/voicing treats it
//	    as the stream's own next thought (it is the mind re-orienting itself, not an external dump);
//	(b) writes the sensed date as a grounded BELIEF via the semantic memory (the perception->memory
//	    handshake, gap §1.1.5) — the clock is the named high-reliability sensor that may write a
//	    grounded belief directly (the resolved Fork 3 grounded-trust carve-out for a real, known read);
//	(c) emits ONE perception.orient event carrying the exact sensed values (the witness on the bus).
//
// THE SENSES, ONE PASS. It folds the grounded reads, NOT one:
//   - PriorContext().L1.Gist — "where I was" (Track 2's rehydrated prior focus);
//   - senseClock(tick)      — "what time is it now" (Track 1.5's logged/replayable clock);
//   - senseSelf(tick)       — "what is my own state now" (the reach=self INTROSPECTION read): the engine's
//     OWN in-memory fields (current goal, open-branch/line count, tick) PLUS the two seam-injected
//     reach=self reads — read_host (the harness's OWN process footprint, across the host.Host seam) and
//     read_event_log (the engine's OWN recent-event count, off its bounded own-event ring). The in-memory
//     fields need no seam; the host + event-log reads enter through injected, deterministic seams (host.go
//     and introspect.go) so the orientation stays headless-pure + byte-stable.
//
// DETERMINISTIC + TEMPLATED. The orientation TEXT is assembled from the sensed values by a fixed
// template — it is NOT model-generated, so it draws no seeded-RNG and makes no backend call. The clock
// value is deterministic offline (clock.Fake), and senseSelf reads in-memory fields, so two identical
// resume boots produce byte-identical orientation.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. The pass fires only when sense.orient is ON AND it is a resume boot AND
// it has not fired yet (the e.oriented once-flag). Default OFF ⇒ no orientation thought, no belief, no
// perception.orient event ⇒ the live loop is byte-identical. Mirrors the senseClock / seedForestIntents
// gating shape.
//
// HEADLESS-PURE. No I/O, no wall clock (only the injected e.clk via senseClock), no unseeded randomness.
package engine

import (
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	hostpkg "github.com/berttrycoding/thought-harness/internal/host"
	"github.com/berttrycoding/thought-harness/internal/memory"
	"github.com/berttrycoding/thought-harness/internal/types"
	webpkg "github.com/berttrycoding/thought-harness/internal/web"
)

// selfState is the result of senseSelf — a deterministic, in-memory read of the engine's OWN state
// (the reach=self introspection sense). It carries no pointers into live engine structures (the values
// are copied at the read), so it is a frozen snapshot the orientation template can fold in safely.
type selfState struct {
	Goal      string // the goal currently being thought (e.graph.Goal), "" when none
	OpenLines int    // live branches/lines in the active graph (ACTIVE or STASHED) — the "open work" count
	Tick      int    // the logical tick at the read (the engine's own clock position)
	HasPrior  bool   // a prior compressed spine was rehydrated (a genuine resume, not a cold first boot)

	// reach=self INTROSPECTION reads (Track 3 — introspect.go). HostOK reports whether read_host fired
	// (sense.host ON + a Host wired); Host carries the harness's OWN process footprint when so. RecentEvents
	// is the read_event_log count — how many of the engine's own emitted events its own-event ring currently
	// holds (sense.event_log ON). Both are zero/false unless their sensor is enabled, so the orientation
	// fold below adds NOTHING to the text when the knobs are off (byte-identical).
	Host         hostpkg.Sample
	HostOK       bool
	RecentEvents int
}

// senseSelf is the reach=self INTROSPECTION sensor (proposal §1, §5 step 2): a deterministic read of the
// engine's OWN state. The in-memory half — current goal, open-line count, tick, and whether a prior spine
// was rehydrated — reads engine fields directly (no tool registry, no model, no clock/RNG). The two
// seam-injected reach=self reads are folded in too: read_host (senseHost — the harness's OWN process
// footprint across the host.Host seam) and read_event_log (recentEventCount — the engine's OWN recent
// emitted-event count off its bounded own-event ring). Each seam read is a no-op (zero/false) unless its
// opt-in knob is on AND its seam is wired, so a default boot reads neither and senseSelf is byte-identical.
// It is the "what is my own state now" half of the orientation, complementing senseClock's "what time is it
// now" and fetch_web's "what is happening in the world now" (the reach=world OUTWARD sense, web_sense.go —
// passed into orientOnce from startEpisode's budgeted senseWeb call, not re-fetched here).
func (e *Engine) senseSelf(tick int) selfState {
	st := selfState{Tick: tick, HasPrior: e.priorContext != nil}
	if e.graph != nil {
		st.Goal = e.graph.Goal
	}
	st.OpenLines = e.branchesLive()
	// reach=self introspection (Track 3 — introspect.go): fold in the harness's OWN footprint (read_host)
	// + its OWN recent-event count (read_event_log). Each is a no-op returning zero/false unless its
	// opt-in knob is on AND its seam is wired — so a default boot reads neither and senseSelf is unchanged.
	st.Host, st.HostOK = e.senseHost()
	st.RecentEvents = e.recentEventCount()
	return st
}

// senseOrientEnabled reports whether the orientation pass may fire: the opt-in sense.orient knob is ON
// AND the pass has not already fired (the once-flag). nil features ⇒ false (the default path never reads
// the flag, so it stays byte-identical). The resume-boot precondition is checked separately in orientOnce
// so a non-resume boot still BURNS the once-flag (the pass is attempted at most once per engine).
func (e *Engine) senseOrientEnabled() bool {
	return e.features != nil && e.features.Sense.Orient && !e.oriented
}

// orientGist returns the prior focus gist to re-ground on — the rehydrated prior compressed spine's L1
// gist (Track 2), or "" when there is none (a cold boot with only clock-sensing on, or an empty prior
// spine). "" is rendered as "(none)" in the template so the orientation reads honestly on a cold boot.
func (e *Engine) orientGist() string {
	if e.priorContext == nil {
		return ""
	}
	return strings.TrimSpace(e.priorContext.L1.Gist)
}

// orientOnce runs the ORIENTATION PASS at the first wake (called from startEpisode after the clock sense,
// so the freshly-opened episode's graph + goal are live). It is a no-op unless sense.orient is ON; it then
// fires only on a RESUME boot (a rehydrated prior spine OR clock-sensing enabled) — a non-resume boot
// still sets e.oriented so the pass is attempted at most once. On a real resume it (a) injects one
// re-grounding GENERATED thought, (b) writes the sensed date as a grounded belief (the perception->memory
// handshake), and (c) emits one perception.orient event carrying the exact sensed values.
//
// The clock value is passed in from startEpisode's existing senseClock call (clockVal, clockOK) so the
// clock is sensed at most ONCE per episode-open — the orientation REUSES that read rather than re-sensing
// (which would double-record the percept-log and double-emit perception.clock). The web snippet (webRes,
// webOK) is likewise passed in from startEpisode's budgeted senseWeb call (fetch_web fires AT MOST ONCE
// per episode-open — the orientation REUSES that read, never re-fetching).
//
// Determinism: the text is templated from sensed values (no RNG / no backend); the clock value is
// deterministic offline (clock.Fake); the web snippet is deterministic offline (web.Fake); senseSelf
// reads in-memory fields. So two identical resume boots orient byte-identically.
func (e *Engine) orientOnce(tick int, clockVal string, clockOK bool, webRes webpkg.Result, webOK bool) {
	if !e.senseOrientEnabled() {
		return
	}
	// Burn the once-flag up front: the orientation is attempted at most once per engine, whether or not
	// the resume-boot precondition is met this wake (a cold boot that later resumes is a separate engine).
	e.oriented = true

	gist := e.orientGist()
	self := e.senseSelf(tick) // reach=self: the engine's own state (introspection read)

	// GENUINE-RESUME precondition (§9 fork 1 + the 2026-06-20 default-on decision: "resume is automatic").
	// Orient ONLY when a prior session's compressed spine was actually rehydrated (priorContext != nil) —
	// a REAL resume, not every episode-open. With clock-sensing default-ON, gating on senseEnabled() would
	// fire orientation at the start of EVERY episode (even a cold boot: "Resuming. Prior focus: (none)"),
	// which is wrong. A cold boot has no prior spine, so it stays silent; the once-flag (already burned)
	// prevents a retry.
	resume := e.priorContext != nil
	if !resume {
		return
	}

	// (a) Inject ONE re-grounding GENERATED thought — the mind's own next thought re-orienting itself.
	// GENERATED source so the Filter/voicing treats it as the stream's own continuation (not an external
	// dump). The text is the deterministic template over the sensed values (clock + self + the outward
	// fetch_web snippet when it succeeded).
	text := e.orientationText(gist, clockVal, clockOK, self, webRes, webOK)
	e.appendThought(&types.Thought{
		ID:         -1,
		Text:       text,
		Source:     types.GENERATED,
		Confidence: 0.6, // a grounded re-orientation is moderately confident — it is sensed, not invented
	}, tick)

	// (b) The perception->memory handshake: write the sensed DATE as a grounded BELIEF. The clock is the
	// named high-reliability sensor that writes grounded directly (resolved Fork 3) — a real, known read,
	// not a model claim. Only when the clock actually sensed a value (clockOK); ValidFrom is the tick (the
	// seeded engine clock, deterministic). Record applies never-fabricate (it stores only Grounded beliefs).
	beliefWritten := false
	if clockOK && clockVal != "" && e.semantic != nil {
		b := memory.Belief{
			Statement: orientDateStatement(clockVal),
			Entities:  []string{"date", "clock", "now"},
			Source:    "orientation:read_clock",
			Grounded:  true, // the clock read is grounded reality (the resolved Fork 3 grounded-sensor write)
			ValidFrom: tick,
			ValidTo:   0, // currently valid
		}
		beliefWritten = e.semantic.Record(b)
	}

	// (c) The witness on the bus — the exact sensed values, so the pass is visible + testable.
	e.bus.Emit(events.PerceptionOrient,
		"orient: prior focus="+orNoneShort(gist)+" time="+orNone(clockVal)+" open_lines="+strconv.Itoa(self.OpenLines),
		events.D{
			"tick":       tick,
			"gist":       gist,
			"clock":      clockVal,
			"self":       self.Goal,
			"open_lines": self.OpenLines,
			"belief":     beliefWritten,
			"resume":     self.HasPrior,
			// reach=self introspection witness (Track 3): the exact footprint + recent-event count the
			// orientation re-grounded on, so a TUI/trace reads the true reach=self reality. Present
			// (non-zero) only when the respective sensor fired; zero/absent otherwise.
			"host_ok":       self.HostOK,
			"alloc_mb":      self.Host.AllocMB,
			"sys_mb":        self.Host.SysMB,
			"goroutines":    self.Host.Goroutines,
			"recent_events": self.RecentEvents,
			// reach=world OUTWARD-perception witness (follow-up #15): the exact fetch_web snippet (+ ok/source)
			// the orientation re-grounded on, so a TUI/trace reads the true outward reality. Present (ok=true,
			// non-empty value) only when fetch_web fired AND read successfully; absent/blank otherwise.
			"web_ok":     webOK && webRes.OK,
			"web":        webRes.Text,
			"web_source": webRes.Source,
		})
}

// orientationText assembles the re-grounding thought from the sensed values, deterministically (a fixed
// template, no RNG / no backend). It always carries the EXACT clock value and the EXACT prior gist so the
// correctness-oracle test can assert the orientation re-grounded on the real sensed reality (not a
// paraphrase). A missing gist / clock renders as "(none)" so the thought reads honestly on a cold-ish boot.
func (e *Engine) orientationText(gist, clockVal string, clockOK bool, self selfState, webRes webpkg.Result, webOK bool) string {
	var b strings.Builder
	b.WriteString("Resuming. Prior focus: ")
	b.WriteString(orNone(gist))
	b.WriteString(". Current time: ")
	if clockOK {
		b.WriteString(orNone(clockVal))
	} else {
		b.WriteString("(none)")
	}
	b.WriteString(". Self-state: ")
	b.WriteString(strconv.Itoa(self.OpenLines))
	b.WriteString(" open line(s) at tick ")
	b.WriteString(strconv.Itoa(self.Tick))
	if strings.TrimSpace(self.Goal) != "" {
		b.WriteString(", working on: ")
		b.WriteString(runeSlice(strings.TrimSpace(self.Goal), 80))
	}
	b.WriteString(".")
	// reach=self introspection fold (Track 3 — introspect.go). Each clause is added ONLY when its sensor
	// fired (HostOK / RecentEvents>0), so with sense.host + sense.event_log OFF the orientation text is
	// EXACTLY the pre-introspection template (byte-identical). The host clause carries the EXACT footprint
	// values so the correctness-oracle test can assert the orientation re-grounded on the real read.
	if self.HostOK {
		b.WriteString(" My footprint: AllocMB=")
		b.WriteString(strconv.FormatUint(self.Host.AllocMB, 10))
		b.WriteString(", SysMB=")
		b.WriteString(strconv.FormatUint(self.Host.SysMB, 10))
		b.WriteString(", Goroutines=")
		b.WriteString(strconv.Itoa(self.Host.Goroutines))
		b.WriteString(".")
	}
	if self.RecentEvents > 0 {
		b.WriteString(" Recent: ")
		b.WriteString(strconv.Itoa(self.RecentEvents))
		b.WriteString(" event(s) in my log.")
	}
	// reach=world OUTWARD fold (follow-up #15 — fetch_web, web_sense.go). The clause is added ONLY when the
	// fetch fired AND read successfully (webOK && OK && a non-empty snippet), so with sense.web OFF (the
	// default) — or a blind/failed read — the orientation text is EXACTLY the pre-fetch_web template
	// (byte-identical). It carries the EXACT snippet so the correctness-oracle test can assert the
	// orientation re-grounded on the real outward read.
	if webOK && webRes.OK && strings.TrimSpace(webRes.Text) != "" {
		b.WriteString(" Current events: ")
		b.WriteString(runeSlice(strings.TrimSpace(webRes.Text), 120))
	}
	return b.String()
}

// orientDateStatement renders the grounded date belief written by the handshake. It carries the exact
// sensed clock value verbatim so the correctness-oracle test can assert the belief holds the CORRECT date
// (== the Fake instant), not a paraphrase.
func orientDateStatement(clockVal string) string {
	return "The current date/time is " + clockVal + " (sensed via read_clock on orientation)."
}

// orNone renders an empty string as "(none)" for the human-readable orientation text + summary.
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

// orNoneShort is orNone with a code-point cap for the event summary (the full value rides the data map).
func orNoneShort(s string) string {
	return orNone(runeSlice(strings.TrimSpace(s), 40))
}
