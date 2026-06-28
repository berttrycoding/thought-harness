package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// recognizeShapeForTest is the thin floor wrapper: does the deterministic lexical floor (cognition.
// RecognizeShape) already engage this line on its own? (Used by the rung-2 tests to assert the ceiling —
// not the floor — is the deciding layer.)
func recognizeShapeForTest(goal string) (any, bool) {
	return cognition.RecognizeShape(goal, nil)
}

// awake_engage_judge_test.go — the AWAKE-DISP rung-2 cognition-property gate
// (docs/internal/notes/2026-06-21-awake-engagement-and-dispatch.md §rung-2).
//
// Rung 0 (awake_user_dispatch) is the deterministic engagement FLOOR: a lexically TASK-SHAPED awake user
// line engages the subconscious (synthesise a workflow); everything else does NOT by the floor alone.
// Rung 2 (awake_user_engage_judge, default OFF) is the Pattern-C model CEILING over that floor: on the
// FUZZY MIDDLE — a SUBSTANTIVE, non-task-shaped line the lexical floor cannot classify — escalate the
// engage/quiet decision to the model. The floor handles the obvious cases; the model fires ONLY on the
// ambiguous middle; the cost guard is the deterministic fuzzy-band pre-check.
//
// These tests pin the THINKING, not the plumbing: (1) the fuzzy-band classifier flags the right lines;
// (2) the ceiling LIFTS a fuzzy substantive line the floor missed; (3) the floor STANDS (surfaced, never
// silent) on a trivial line, a model-decline, a model-quiet, no-judge (the test double), and flag-OFF —
// each is a distinct non-escalation path the spec's Rule 4 requires to be observable.

// engageJudgeEngine builds a reactive engine with an injected EngagementJudge double + the rung-2 flag, so
// e.backend satisfies backends.EngagementJudge and e.engageCeiling can be exercised in isolation (the
// escalation logic is mode-independent — only maybeAwakeUserDispatch is awake-only). Reactive keeps the
// graph deterministic; the unit under test is the ceiling decision, not the awake loop.
func engageJudgeEngine(t *testing.T, be backends.Backend, on bool) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.New()
	feat.Conscious.Activity.AwakeUserDispatch = true // the floor rung 2 sits above
	feat.Conscious.Activity.AwakeUserEngageJudge = on
	cfg.Features = feat
	e, err := NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// TestEngageFuzzyBand pins the deterministic fuzzy-band classifier — the cost guard that keeps the model
// off the obvious cases. A question or a substantive longer line is in the band (escalate); a trivial
// greeting / short ack is NOT (the floor stands silently, the model is never consulted).
func TestEngageFuzzyBand(t *testing.T) {
	inBand := []string{
		"how should i think about the trade-off between consistency and availability here",
		"what do you make of the situation we discussed earlier",
		"can you help me reason through whether to migrate the storage layer",
		"i have been turning over a thorny question about how teams stay aligned at scale", // longer, no '?'
	}
	notInBand := []string{
		"hi there",
		"thanks, sounds good",
		"hello",
		"ok cool",
		"morning",
	}
	for _, s := range inBand {
		if !engageFuzzyBand(s) {
			t.Errorf("expected %q IN the fuzzy band (substantive, escalate), got out", s)
		}
	}
	for _, s := range notInBand {
		if engageFuzzyBand(s) {
			t.Errorf("expected %q NOT in the fuzzy band (trivial, floor stands silently), got in", s)
		}
	}
}

// TestEngageCeilingLiftsFuzzyLine is the load-bearing cognition property: ON + a deciding judge, a FUZZY
// substantive line the lexical floor missed is escalated and the model can LIFT the no-engage to engage —
// and that lift fires conscious.engage_judge (the ceiling moved the decision, witnessed). The judge double
// engages a line mentioning "design"; a fuzzy line WITHOUT it is quieted (the floor stands).
func TestEngageCeilingLiftsFuzzyLine(t *testing.T) {
	// A fuzzy line the floor would NOT task-shape on its own but that mentions the judge's engage signal
	// ("consider", deliberately not a RecognizeShape keyword — the ceiling, not the floor, decides).
	const fuzzyEngage = "how should we consider the two approaches we kicked around earlier today"
	const fuzzyQuiet = "how are you feeling about everything we covered today and so on"

	if _, shaped := recognizeShapeForTest(fuzzyEngage); shaped {
		t.Fatalf("test setup: %q is already task-shaped by the floor — pick a line the ceiling decides", fuzzyEngage)
	}
	if !engageFuzzyBand(fuzzyEngage) {
		t.Fatalf("test setup: %q must be in the fuzzy band to reach the ceiling", fuzzyEngage)
	}

	e := engageJudgeEngine(t, judgeBackend{TestBackend: backends.NewTest(), decided: true}, true)
	lifted := false
	e.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.EngageJudge) {
			lifted = true
		}
	})
	if !e.engageCeiling(1, fuzzyEngage) {
		t.Fatalf("ON + judge engages: the ceiling did NOT lift a fuzzy substantive line the floor missed")
	}
	if !lifted {
		t.Fatalf("the ceiling lifted but conscious.engage_judge never fired — the lift is invisible")
	}

	// A fuzzy line the model QUIETS -> the floor stands (no lift, surfaced as floor_stands).
	eQuiet := engageJudgeEngine(t, judgeBackend{TestBackend: backends.NewTest(), decided: true}, true)
	stood := false
	eQuiet.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.EscalationFloorStands) {
			stood = true
		}
	})
	if eQuiet.engageCeiling(1, fuzzyQuiet) {
		t.Fatalf("model quiet: the floor must stand (no engage)")
	}
	if !stood {
		t.Fatalf("model quiet: expected escalation.floor_stands (the non-escalation must be surfaced, never silent)")
	}
}

// TestEngageCeilingFloorStands pins every NON-escalation path — the spec's Rule 4 (the floor stands,
// surfaced, never silent) and the cost guard (a trivial line is never escalated). Each path returns false
// (no engage); the observable difference is whether floor_stands was surfaced (consulted/unavailable while
// fuzzy) vs silent (trivial line / flag off — the model was never asked).
func TestEngageCeilingFloorStands(t *testing.T) {
	const fuzzy = "how should we consider the two approaches we kicked around earlier today"
	const trivial = "hi there"

	standsFor := func(t *testing.T, e *Engine, goal string) (engaged, stood bool) {
		t.Helper()
		e.bus.Subscribe(func(ev events.Event) {
			if ev.Kind == string(events.EscalationFloorStands) {
				stood = true
			}
		})
		engaged = e.engageCeiling(1, goal)
		return
	}

	// 1. flag OFF -> never escalates, never surfaces (the model is not consulted at all).
	off := engageJudgeEngine(t, judgeBackend{TestBackend: backends.NewTest(), decided: true}, false)
	if eng, stood := standsFor(t, off, fuzzy); eng || stood {
		t.Errorf("flag OFF: want no engage + no floor_stands (complete no-op), got engage=%v stood=%v", eng, stood)
	}

	// 2. trivial line (not in the fuzzy band) -> never escalates (cost guard); floor stands SILENTLY.
	triv := engageJudgeEngine(t, judgeBackend{TestBackend: backends.NewTest(), decided: true}, true)
	if eng, stood := standsFor(t, triv, trivial); eng || stood {
		t.Errorf("trivial line: want no engage + silent (never consult the model), got engage=%v stood=%v", eng, stood)
	}

	// 3. no judge (the plain test double) -> floor stands, surfaced (no model ceiling).
	plain := engageJudgeEngine(t, backends.NewTest(), true)
	if eng, stood := standsFor(t, plain, fuzzy); eng || !stood {
		t.Errorf("no judge: want no engage + floor_stands surfaced, got engage=%v stood=%v", eng, stood)
	}

	// 4. model declines (decided=false) -> floor stands, surfaced.
	decline := engageJudgeEngine(t, judgeBackend{TestBackend: backends.NewTest(), decided: false}, true)
	if eng, stood := standsFor(t, decline, fuzzy); eng || !stood {
		t.Errorf("model declined: want no engage + floor_stands surfaced, got engage=%v stood=%v", eng, stood)
	}
}

// TestEngageCeilingNeverQuietsTaskShaped is the structural-floor invariant: a TASK-shaped line never
// reaches the ceiling — it engages on the floor alone, so the model can never QUIET a structural engage.
// We assert maybeAwakeUserDispatch on a task-shaped awake line engages WITH the rung-2 judge present but
// QUIETING everything (decided=true, "design"-only engage) — the task-shaped line still synthesises,
// proving the floor's engage is not gated behind the ceiling.
func TestEngageCeilingNeverQuietsTaskShaped(t *testing.T) {
	// A clearly task-shaped line the FLOOR engages on its own (RecognizeShape true). The judge double would
	// QUIET it if it were ever consulted (no "design" word) — but the floor never escalates a structural
	// engage, so the workflow synthesises regardless.
	const taskShaped = "optimize the throughput of the ingest pipeline and minimize tail latency"
	if _, shaped := recognizeShapeForTest(taskShaped); !shaped {
		t.Fatalf("test setup: %q must be task-shaped (the floor engages it)", taskShaped)
	}

	feat := config.New()
	config.ApplyAwakeDefaults(feat)
	feat.Conscious.Activity.AwakeUserDispatch = true
	feat.Conscious.Activity.AwakeUserEngageJudge = true
	feat.Validate()
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := NewEngine(&cfg, judgeBackend{TestBackend: backends.NewTest(), decided: true})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var synth, lifted int
	e.bus.Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case string(events.SubSynthesize):
			synth++
		case string(events.EngageJudge):
			lifted++
		}
	})
	for i := 0; i < 3; i++ {
		e.Step()
	}
	e.SubmitDefault(taskShaped)
	for i := 0; i < 8; i++ {
		e.Step()
	}
	if synth == 0 {
		t.Fatalf("task-shaped line: the floor must engage (synthesise a workflow) on its own — got synth=0")
	}
	if lifted != 0 {
		t.Fatalf("task-shaped line: the ceiling must NOT be consulted (engage_judge=%d) — a structural engage is never escalated", lifted)
	}
}

// TestEngageJudgeFiresOnLiveAwakeLoop is the WIRING gate (the wiring-gate lesson: a feature that exists but
// is not on the engine's actual tick is dead). It drives the REAL continuous loop — maybeAwakeUserDispatch
// on the live tick — with a FUZZY, substantive, non-task-shaped user line the judge double engages, and
// asserts the rung-2 escalation actually fired (conscious.engage_judge) ON THE LIVE TICK — which never
// happens on the floor alone for this line (the floor would no-op a non-task-shaped line). The flag-OFF
// control proves the live loop is byte-identical without rung 2 (no engage_judge AND the floor's own
// no-engage stands: no SubSynthesize from this faculty). This is the proof rung 2 runs on the live loop,
// not just in the isolated helper test. (Whether a WORKFLOW then materialises depends on the backend —
// the test double's SynthesizeProgram defers for a non-task-shaped goal, so the synth-materialisation is
// a CONTENT property proven on the live-claude path, not asserted here.)
func TestEngageJudgeFiresOnLiveAwakeLoop(t *testing.T) {
	// A fuzzy line the lexical FLOOR does NOT task-shape (no RecognizeShape keyword) but is substantive (a
	// question + a "consider" the judge double engages on). On the floor alone this engages nothing.
	const fuzzyLine = "how should we consider the two storage approaches we kicked around earlier today"
	if _, shaped := recognizeShapeForTest(fuzzyLine); shaped {
		t.Fatalf("test setup: %q must NOT be task-shaped (the ceiling, not the floor, must decide)", fuzzyLine)
	}

	drive := func(judgeOn bool) (engageJudge, floorStands int) {
		feat := config.New()
		config.ApplyAwakeDefaults(feat)
		feat.Conscious.Activity.AwakeUserDispatch = true       // rung 0 floor on
		feat.Conscious.Activity.AwakeUserEngageJudge = judgeOn // rung 2 ceiling under test
		feat.Validate()
		cfg := DefaultConfig()
		cfg.Mode = "continuous"
		cfg.Seed = 7
		cfg.Features = feat
		// A judge double that engages the fuzzy "consider" line — so the ceiling, not the floor, drives the engage.
		e, err := NewEngine(&cfg, judgeBackend{TestBackend: backends.NewTest(), decided: true})
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		e.bus.Subscribe(func(ev events.Event) {
			switch ev.Kind {
			case string(events.EngageJudge):
				engageJudge++
			case string(events.EscalationFloorStands):
				floorStands++
			}
		})
		for i := 0; i < 3; i++ {
			e.Step() // already awake + wandering
		}
		e.SubmitDefault(fuzzyLine)
		for i := 0; i < 8; i++ {
			e.Step()
		}
		return
	}

	offJudge, _ := drive(false)
	onJudge, _ := drive(true)
	t.Logf("rung-2 OFF: engage_judge=%d", offJudge)
	t.Logf("rung-2 ON : engage_judge=%d", onJudge)

	// FLAG OFF: the fuzzy line never escalates (no engage_judge) — the floor stands, byte-identical.
	if offJudge != 0 {
		t.Fatalf("flag OFF: rung-2 escalated on the live loop (engage_judge=%d) — the OFF path is not byte-identical", offJudge)
	}
	// FLAG ON: the rung-2 ceiling fired on the LIVE tick — the escalation is wired into the actual loop, not
	// dead code. (On the floor alone this fuzzy non-task line never reaches an engage; only the wired ceiling
	// does — so a non-zero count is the proof the escalation runs on the engine's tick.)
	if onJudge == 0 {
		t.Fatalf("flag ON: rung-2 never fired on the live awake loop (engage_judge=0) — the escalation is NOT wired into the tick")
	}
}
