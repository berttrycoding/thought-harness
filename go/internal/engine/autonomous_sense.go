// autonomous_sense.go is #19 — AUTONOMOUS STANDING-INTENT SENSING for the cognitive power-cycle. It
// closes the gap a premise-check confirmed: there is NO silent self-initiated sensing today. Sensing
// fires only at episode-open (the orientation pass) or by relevance to the active conscious context.
// The standing seed-intent roots (Perceive / Self-monitor, seedintent.go) sit in the forest, but their
// BackedBy is DEAD AS A TRIGGER (it rides only the event data) — a standing PERCEPTUAL/INTROSPECTIVE
// root never fires its own sensor on its own.
//
// THIS FILE IS THE LIVE-WIRE. When conscious.activity.autonomous_sense is ON AND a standing seed root
// whose faculty is FacultyPerceptual or FacultyIntrospective holds focus in the AWAKE loop, the engine
// fires ONE bounded sensor read FOR THAT FOCUS, ON ITS OWN:
//   - PERCEPTUAL  -> senseClock (the "watch reality" sensor) + senseWeb when sense.web is on — the
//     OUTWARD/boundary reads. Whatever sensed (clock value / web snippet) folds into the percept text.
//   - INTROSPECTIVE -> senseSelf, which already folds read_self (goal/open-lines/tick) + read_host
//     (process footprint) + read_event_log (own recent-event count) — the "track my own state" sensor.
//
// The sensed percept is INJECTED as a GENERATED thought via the existing appendThought path (so the
// stream reads it as its own next thought) AND witnessed on perception.sense (the bus observability
// contract — the thing the premise-check found missing, now visible + testable).
//
// BOUNDED (the regulator obligation). The autonomous sense is a SINGLE bounded read per focus: a
// per-(branch, tick) guard (autoSenseBranch/autoSenseTick) prevents a second read on the same focus, and
// it spawns NO operator / sub-agent / fork — it is one append, like any percept. So it adds NO standing
// excitation source that pushes the branching ratio n→1: it is a μ-baseline immigrant (a percept), not a
// fork. The #18 self-watching stability cell (stability.go) PROVES n<1 with this ON.
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. autonomousSenseOn() reads the opt-in knob (default OFF). With it OFF the
// awake loop never calls maybeAutonomousSense, never injects a percept, never emits perception.sense ⇒
// the live loop is byte-identical to the pre-#19 engine. Reactive mode never reaches stepContinuous, so
// it never senses autonomously either way.
//
// DETERMINISM + HEADLESS-PURE. The percept text is a deterministic template over the sensed values (no
// RNG, no backend). The sensors enter time/world/host through the SAME injected seams the other power-
// cycle sensors use (clock.Fake / web.Fake / host.Fake offline), so two identical seeded runs sense
// byte-identically. No wall clock is read here (only via the clock seam inside senseClock).
package engine

import (
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// autonomousSenseOn reports whether autonomous standing-intent sensing is enabled (the opt-in knob,
// default OFF). Only meaningful in the awake/continuous loop. nil features ⇒ false (the default path
// never reads the flag, so it stays byte-identical).
func (e *Engine) autonomousSenseOn() bool {
	return e.features != nil && e.features.Conscious.Activity.AutonomousSense
}

// maybeAutonomousSense is the #19 live-wire, called once per awake tick AFTER focus is settled
// (continuous.go, after afterSelect). It fires ONE bounded sensor read iff:
//   - the flag is ON (autonomousSenseOn), AND
//   - the currently-focused branch is a standing seed root tagged PERCEPTUAL or INTROSPECTIVE
//     (e.branchFaculty[e.graph.ActiveBranch]), AND
//   - this focus has not already been autonomously sensed this tick (the per-(branch,tick) guard).
//
// It is a NO-OP (returns immediately) when any precondition fails — so the default/reactive path is
// byte-identical. On a fire it injects a GENERATED percept thought + emits perception.sense, and burns
// the per-focus guard so a re-entry on the same focus/tick cannot fire a second read (bounded: one read
// per focus, no fan-out).
func (e *Engine) maybeAutonomousSense(tick int) {
	if !e.autonomousSenseOn() || e.graph == nil || len(e.branchFaculty) == 0 {
		return
	}
	bid := e.graph.ActiveBranch
	fac, tagged := e.branchFaculty[bid]
	if !tagged {
		return // the focused branch is not a standing seed root — nothing to sense for
	}
	// BOUNDED: at most one autonomous sense per (branch, tick). A re-focus on the same branch within the
	// same tick must not fire a second read (no fan-out, no standing excitation).
	if e.autoSenseBranch == bid && e.autoSenseTick == tick {
		return
	}

	// The standing root's NAME (off the branch reason "seed-intent: <name>") rides the witness so a trace
	// reads WHICH standing intent fired its sensor (the live-wire of that root's BackedBy).
	name := seedRootName(e, bid)

	switch fac {
	case cognition.FacultyPerceptual:
		e.autonomousSensePerceptual(tick, bid, name)
	case cognition.FacultyIntrospective:
		e.autonomousSenseIntrospective(tick, bid, name)
	default:
		return // only PERCEPTUAL / INTROSPECTIVE roots sense autonomously (the standing watch faculties)
	}

	// Burn the per-focus guard (one read per focus). Recorded only on a real fire so a no-op faculty does
	// not block a later legitimate sense on the same branch.
	e.autoSenseBranch = bid
	e.autoSenseTick = tick
}

// autonomousSensePerceptual fires the PERCEPTUAL standing root's sensors ON ITS OWN: senseClock (the
// boundary clock read) and, when sense.web is on, senseWeb (the outward world read). Whatever the
// sensors returned (offline with no seam wired they return blank/false — the percept then records that
// the watch fired but the world was unreadable this tick) folds into a deterministic percept thought,
// which is injected as a GENERATED thought and witnessed on perception.sense.
func (e *Engine) autonomousSensePerceptual(tick, bid int, name string) {
	clockVal, clockOK := e.senseClock(tick) // the boundary clock read (no-op + blank unless sense.clock + a clock wired)
	webRes, webOK := e.senseWeb(tick)       // the outward world read (no-op unless sense.web + a web seam wired; budgeted)

	var b strings.Builder
	b.WriteString("(perceiving) I watched the intake port unprompted.")
	if clockOK && clockVal != "" {
		b.WriteString(" Time now: ")
		b.WriteString(clockVal)
		b.WriteString(".")
	}
	if webOK && webRes.OK && strings.TrimSpace(webRes.Text) != "" {
		b.WriteString(" World: ")
		b.WriteString(runeSlice(strings.TrimSpace(webRes.Text), 120))
		b.WriteString(".")
	}
	if (!clockOK || clockVal == "") && (!webOK || !webRes.OK) {
		b.WriteString(" Nothing new to admit right now.")
	}
	text := b.String()

	e.injectAutonomousPercept(tick, text)
	e.bus.Emit(events.PerceptionSense,
		"autonomous-sense [perceptual] "+orNoneShort(name)+": "+runeSlice(text, 80),
		events.D{
			"faculty": cognition.FacultyPerceptual.String(),
			"branch":  bid,
			"name":    name,
			"kind":    "clock",
			"value":   clockVal,
			"web":     webRes.Text,
			"tick":    tick,
		})
}

// autonomousSenseIntrospective fires the INTROSPECTIVE standing root's sensor ON ITS OWN: senseSelf — the
// reach=self read that already folds the engine's own goal/open-lines/tick PLUS read_host (the process
// footprint) and read_event_log (the own recent-event count) when those knobs are on. It folds the
// self-state into a deterministic percept thought, injects it as a GENERATED thought, and witnesses it on
// perception.sense.
func (e *Engine) autonomousSenseIntrospective(tick, bid int, name string) {
	self := e.senseSelf(tick) // reach=self: in-memory state + (when wired) host footprint + own-event count

	var b strings.Builder
	b.WriteString("(self-monitoring) I checked my own state unprompted: ")
	b.WriteString(strconv.Itoa(self.OpenLines))
	b.WriteString(" open line(s) at tick ")
	b.WriteString(strconv.Itoa(self.Tick))
	if g := strings.TrimSpace(self.Goal); g != "" {
		b.WriteString(", working on: ")
		b.WriteString(runeSlice(g, 80))
	}
	b.WriteString(".")
	if self.HostOK {
		b.WriteString(" Footprint: AllocMB=")
		b.WriteString(strconv.FormatUint(self.Host.AllocMB, 10))
		b.WriteString(", Goroutines=")
		b.WriteString(strconv.Itoa(self.Host.Goroutines))
		b.WriteString(".")
	}
	if self.RecentEvents > 0 {
		b.WriteString(" Recent: ")
		b.WriteString(strconv.Itoa(self.RecentEvents))
		b.WriteString(" event(s) in my log.")
	}
	text := b.String()

	e.injectAutonomousPercept(tick, text)
	e.bus.Emit(events.PerceptionSense,
		"autonomous-sense [introspective] "+orNoneShort(name)+": "+runeSlice(text, 80),
		events.D{
			"faculty":       cognition.FacultyIntrospective.String(),
			"branch":        bid,
			"name":          name,
			"kind":          "self",
			"value":         text,
			"open_lines":    self.OpenLines,
			"host_ok":       self.HostOK,
			"recent_events": self.RecentEvents,
			"tick":          tick,
		})
}

// injectAutonomousPercept appends the autonomous percept as a GENERATED thought via the existing append
// path — so the stream reads it as its own next thought (the silent-injection voicing contract). A
// moderate confidence (0.6) marks it as a sensed re-grounding (it is read, not invented), the same level
// orientOnce uses for its grounded re-orientation thought. It is a single append (a μ-baseline immigrant),
// NOT a fork — so it does not raise the branching plant n.
func (e *Engine) injectAutonomousPercept(tick int, text string) {
	e.appendThought(&types.Thought{
		ID:         -1,
		Text:       text,
		Source:     types.GENERATED,
		Confidence: 0.6,
	}, tick)
}

// seedRootName returns the standing root's name parsed off its branch reason ("seed-intent: <name>"), or
// "" when the branch has no such reason. Deterministic (a pure string read), no clock/RNG.
func seedRootName(e *Engine, bid int) string {
	if e.graph == nil {
		return ""
	}
	b, ok := e.graph.Branches[bid]
	if !ok || b.Reason == nil {
		return ""
	}
	const prefix = "seed-intent: "
	r := *b.Reason
	if strings.HasPrefix(r, prefix) {
		return strings.TrimSpace(r[len(prefix):])
	}
	return ""
}
