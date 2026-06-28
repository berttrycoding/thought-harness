package engine_test

import (
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	clockpkg "github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// deadlineEngine builds a test-backend engine with a Fake clock and the given per-episode deadline,
// returning the engine, the fake clock, and a captured event log.
func deadlineEngine(t *testing.T, deadline time.Duration) (*engine.Engine, *clockpkg.Fake, *[]events.Event) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var log []events.Event
	e.Bus().Subscribe(func(ev events.Event) { log = append(log, ev) })
	fake := clockpkg.NewFake()
	e.SetClock(fake, deadline)
	return e, fake, &log
}

// TestDeadlineForcesStopBestSoFar (WF-G, 09 §4): an episode whose wall-clock budget expires is forced
// to STOP — the run ends early, the lifecycle.deadline event fires exactly once with the elapsed/
// deadline accounting, and the engine still RESPONDS (best-so-far, never a hang or a silent drop).
func TestDeadlineForcesStopBestSoFar(t *testing.T) {
	e, fake, log := deadlineEngine(t, 50*time.Millisecond)
	e.SubmitDefault("weigh the options for the rollout and recommend one")

	// drive ticks, advancing the fake clock 30ms per tick: tick 1 is inside the budget, the deadline
	// trips on tick 2 (60ms >= 50ms) — the episode must stop within a couple of ticks after that.
	steps := 0
	for i := 0; i < 20; i++ {
		e.Step()
		steps++
		fake.Advance(30 * time.Millisecond)
		if done(*log) {
			break
		}
	}

	deadlines := byKind(*log, events.Deadline)
	if len(deadlines) != 1 {
		t.Fatalf("lifecycle.deadline must fire exactly once, got %d", len(deadlines))
	}
	d := deadlines[0]
	if d.Data["deadline_ms"].(int64) != 50 || d.Data["elapsed_ms"].(int64) < 50 {
		t.Fatalf("deadline accounting wrong: %v", d.Data)
	}
	if steps > 6 {
		t.Fatalf("the deadline should have stopped the episode within a few ticks, took %d", steps)
	}
	if len(byKind(*log, events.Respond)) == 0 {
		t.Fatalf("a deadline STOP must still RESPOND best-so-far (never a silent drop)")
	}
}

// TestTimeBlindDefaultIdentical: the SAME engine config WITHOUT a clock runs the same goal with no
// lifecycle.deadline event and no behavior change — the nil-clock default never reads time. (The full
// suite's goldens are the stronger version of this; here we assert the seam's contract directly.)
func TestTimeBlindDefaultIdentical(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var log []events.Event
	e.Bus().Subscribe(func(ev events.Event) { log = append(log, ev) })
	e.SubmitDefault("weigh the options for the rollout and recommend one")
	e.Run(20)
	if n := len(byKind(log, events.Deadline)); n != 0 {
		t.Fatalf("a time-blind engine must never emit lifecycle.deadline, got %d", n)
	}
	if len(byKind(log, events.Respond)) == 0 {
		t.Fatalf("the baseline run should respond")
	}
}

// TestDeadlineZeroWithClockIsOff: a wired clock with deadline 0 never trips (the knob, not the clock,
// arms the check).
func TestDeadlineZeroWithClockIsOff(t *testing.T) {
	e, fake, log := deadlineEngine(t, 0)
	e.SubmitDefault("weigh the options for the rollout and recommend one")
	for i := 0; i < 20; i++ {
		e.Step()
		fake.Advance(time.Hour) // absurd elapsed; must still never trip with deadline 0
		if done(*log) {
			break
		}
	}
	if n := len(byKind(*log, events.Deadline)); n != 0 {
		t.Fatalf("deadline 0 must disable the check, got %d events", n)
	}
}

// byKind filters the captured events by kind.
func byKind(log []events.Event, kind string) []events.Event {
	var out []events.Event
	for _, ev := range log {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// done reports whether the episode has produced its outward answer (action.respond seen).
func done(log []events.Event) bool { return len(byKind(log, "action.respond")) > 0 }
