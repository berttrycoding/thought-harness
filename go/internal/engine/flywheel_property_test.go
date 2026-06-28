package engine_test

// Cognitive-property tests for the OFFLINE-RL DATA FLYWHEEL (Track C, docs/internal/2026-06-21-harness-
// rl-ml-roadmap.md §6 Phase-0 + §6.5). These assert the CAPTURE substrate actually FIRES on the live loop
// and labels the tuples with the INDEPENDENT grounded outcome — not merely that the loop runs:
//
//   - with flywheel.capture ON, every Controller decision of a live episode becomes a captured
//     (state, action) tuple, and at episode close the terminal grounded Outcome is BACKFILLED onto all of
//     them (the Monte-Carlo return assignment);
//   - the OUTCOME label is the INDEPENDENT terminal signal (§6.5): an episode that grounds an arithmetic
//     claim against the deterministic compute spine carries GroundedObs>0 / EpisodeGrounded, and a FALSE
//     claim carries RefutedObs>0 — sourced from the grounding spine, never a self-judgment. A FABRICATED
//     observation (the offline watched-seam stand-in) is REJECTED upstream and so NEVER inflates the label
//     (the invariant working as designed — the S5 test pins exactly that);
//   - the flywheel.capture event fires once per finalised tuple (the observability contract);
//   - with the knob OFF the loop captures NOTHING and emits NO flywheel.* event (byte-identical default).
//
// Deterministic: the TestBackend test double + the seeded cpyrand stream, so the captured dataset is
// reproducible (no clock/RNG in the flywheel — the seeded engine tick stamps every tuple).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/flywheel"
	"github.com/berttrycoding/thought-harness/internal/scenarios"
)

// newRLFlywheelEngine builds a reactive engine with flywheel.capture set to `on`, an injected in-memory
// dataset sink, and an eventLog sink — the Go form of running the live loop with the capture instrument
// wired. The MemSink is returned so a test can read the captured corpus directly.
func newRLFlywheelEngine(t *testing.T, on bool) (*engine.Engine, *flywheel.MemSink, *eventLog) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.AllOn()
	feat.Flywheel.Capture = on
	cfg.Features = &feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	mem := flywheel.NewMemSink()
	e.SetFlywheelSink(mem) // adopts the sink + rebuilds the recorder around it when the knob is ON
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, mem, log
}

// TestFlywheelCapturesPerDecisionTuplesOnLiveLoop is the core wiring-into-the-live-loop assertion: the
// flywheel taps the LIVE decision spine of the S5 stuck->act->reality scenario, captures one (state,
// action) tuple per Controller decision (incl. the ACT edge), backfills the terminal StopKind label, and
// emits the observability event — and on the OFFLINE double the grounded label is HONESTLY empty (the S5
// watched-seam observations are fabricated tier-0 and rejected upstream — the §6.5 invariant: fabricated
// reality never grounds).
func TestFlywheelCapturesPerDecisionTuplesOnLiveLoop(t *testing.T) {
	eng, mem, log := newRLFlywheelEngine(t, true)
	if _, err := scenarios.RunScenario("S5", eng); err != nil {
		t.Fatalf("RunScenario(S5): %v", err)
	}

	tuples := mem.All()
	if len(tuples) == 0 {
		t.Fatal("flywheel.capture ON: the live loop captured NO decision tuples — the instrument is not wired into the decision spine")
	}

	validActions := map[string]bool{"THINK": true, "BRANCH": true, "MERGE": true, "BACKTRACK": true, "ACT": true, "STOP": true, "DELIVER": true}
	var sawACT bool
	for i, tup := range tuples {
		if !tup.Filled {
			t.Errorf("tuple[%d] not Filled — its terminal outcome was never backfilled at episode close", i)
		}
		if tup.Episode == "" {
			t.Errorf("tuple[%d] has no episode id (the trajectory key)", i)
		}
		if !validActions[tup.Action] {
			t.Errorf("tuple[%d] action = %q, not a Controller spine action", i, tup.Action)
		}
		if tup.State.Mode != "reactive" {
			t.Errorf("tuple[%d] state mode = %q, want reactive", i, tup.State.Mode)
		}
		if tup.Outcome.StopKind == "" {
			t.Errorf("tuple[%d] StopKind label empty — the terminal label was not backfilled", i)
		}
		if tup.Action == "ACT" {
			sawACT = true
		}
	}
	if !sawACT {
		t.Fatal("S5 is the stuck->ACT->reality scenario but no ACT decision was captured — the action edge is not in the dataset")
	}

	// THE §6.5 INVARIANT (negative arm): the S5 watched-seam observations are FABRICATED on the offline
	// double, and fabricated reality is rejected by the grounding spine — so the independent grounded label
	// is HONESTLY empty here. A non-zero grounded/refuted tally would mean the label was sourced from a
	// self-judgment rather than the grounding spine.
	for i, tup := range tuples {
		if tup.Outcome.EpisodeGrounded || tup.Outcome.GroundedObs != 0 || tup.Outcome.RefutedObs != 0 {
			t.Errorf("tuple[%d]: fabricated S5 reality must NOT ground (got grounded=%v g=%d r=%d) — the label is not the independent grounding signal",
				i, tup.Outcome.EpisodeGrounded, tup.Outcome.GroundedObs, tup.Outcome.RefutedObs)
		}
	}

	// the observability contract: the flywheel.capture event fired once per finalised tuple.
	captureEvents := log.of(events.FlywheelCapture)
	if len(captureEvents) != len(tuples) {
		t.Fatalf("flywheel.capture fired %d times, want one per finalised tuple (%d) — the dataset and the bus must agree", len(captureEvents), len(tuples))
	}
	for i, ev := range captureEvents {
		if _, ok := ev.Data["action"]; !ok {
			t.Errorf("flywheel.capture[%d] event missing action in payload", i)
		}
		if _, ok := ev.Data["stop_kind"]; !ok {
			t.Errorf("flywheel.capture[%d] event missing stop_kind (the grounded-label source)", i)
		}
	}
}

// TestFlywheelLabelIsIndependentGroundedSignal is the §6.5 positive arm: an episode that grounds an
// arithmetic claim against the DETERMINISTIC compute spine (offline, no model, no fabrication) carries a
// GROUNDED label on its captured tuples — the INDEPENDENT terminal signal sourced from the grounding spine,
// never a self-judgment. (The REFUTED arm of the tally — counting grounding.Refuted — is unit-tested at
// the package level via flywheel.Recorder.CloseEpisode with a RefutedObs Outcome; it cannot be driven
// through the offline content double, which always VOICES the true arithmetic regardless of the prompt, so
// the refuted-through-the-live-loop case is part of the live-claude proof, not this offline suite.)
func TestFlywheelLabelIsIndependentGroundedSignal(t *testing.T) {
	eng, mem, _ := newRLFlywheelEngine(t, true)
	eng.SubmitDefault("Is 12 * 31 = 372 ? verify the arithmetic")
	eng.Run(30)
	tuples := mem.All()
	if len(tuples) == 0 {
		t.Fatal("no tuples captured for the arithmetic episode")
	}
	var grounded bool
	for _, tup := range tuples {
		if tup.Outcome.EpisodeGrounded && tup.Outcome.GroundedObs >= 1 {
			grounded = true
		}
	}
	if !grounded {
		t.Fatalf("a grounded arithmetic claim must produce a GROUNDED label on the captured tuples; got %+v", tuples[len(tuples)-1].Outcome)
	}
}

// TestFlywheelOffCapturesNothing is the byte-identical-default guard: with the knob OFF the live loop
// builds no Recorder, captures no tuple, and emits no flywheel.* event.
func TestFlywheelOffCapturesNothing(t *testing.T) {
	eng, mem, log := newRLFlywheelEngine(t, false)
	if _, err := scenarios.RunScenario("S5", eng); err != nil {
		t.Fatalf("RunScenario(S5): %v", err)
	}
	if got := len(mem.All()); got != 0 {
		t.Fatalf("flywheel.capture OFF: captured %d tuples, want 0 (the default must be inert)", got)
	}
	if got := len(log.of(events.FlywheelCapture)); got != 0 {
		t.Fatalf("flywheel.capture OFF: emitted %d flywheel.capture events, want 0 (byte-identical default)", got)
	}
}

// TestFlywheelDatasetReproducible: two identical seeded runs capture a byte-identical dataset (the
// determinism contract — the flywheel holds no clock/RNG of its own).
func TestFlywheelDatasetReproducible(t *testing.T) {
	run := func() []flywheel.DecisionTuple {
		eng, mem, _ := newRLFlywheelEngine(t, true)
		if _, err := scenarios.RunScenario("S5", eng); err != nil {
			t.Fatalf("RunScenario(S5): %v", err)
		}
		return mem.All()
	}
	a, b := run(), run()
	if len(a) != len(b) {
		t.Fatalf("dataset size not reproducible: %d vs %d tuples", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("tuple[%d] not reproducible across seeded runs:\n a=%+v\n b=%+v", i, a[i], b[i])
		}
	}
}
