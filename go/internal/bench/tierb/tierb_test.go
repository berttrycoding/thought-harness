package tierb

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// fixedSeed is the paired seed the test runs every arm under (spec §5.1: same seed across arms).
const fixedSeed int64 = 7

// tinyTwoTurnScenario is a deliberately small inline Tier-B fixture: a 2-turn grounding arc with
// a trivially-satisfiable end-state (an event-presence oracle that any engine run emits) and the
// grounding isolation predicate. It exists to prove the DRIVER threads two turns through ONE
// engine and returns a well-formed ScenarioResult — NOT to assert a particular cognition outcome
// (that is the property tests' job). Run under the OFFLINE test backend, no LLM, fully reproducible.
func tinyTwoTurnScenario() benchtypes.TierBScenario {
	return benchtypes.TierBScenario{
		ID:         "B-TINY-0001",
		Mechanism:  benchtypes.MechGrounding,
		Family:     "tiny-smoke",
		Difficulty: "low",
		Domain:     "core-knowledge",
		Turns: []benchtypes.Turn{
			{Index: 1, Text: "What is 17 plus 25?"},
			{Index: 2, Text: "Are you sure? Re-check the arithmetic."},
		},
		// A trivially-true end-state: every engine episode emits at least one tick. Using
		// event-presence keeps the oracle deterministic and engine-agnostic so the test pins the
		// DRIVER (threading + result shape), not a cognition outcome.
		EndStateOracles: []benchtypes.Oracle{
			{Kind: benchtypes.OracleEventPresence, Expected: events.Tick},
		},
		// No declared required-events ⇒ the scorer falls back to the runner's grounding witness.
		IsolationPredicate: benchtypes.IsolationPredicate{Kind: "grounding-read-preceded-answer"},
		Ablation: benchtypes.AblationConfig{
			Arms:     []benchtypes.Arm{benchtypes.ArmBare, benchtypes.ArmHarness},
			GateFlag: "seam.hidden_filter",
		},
	}
}

// TestRunScenarioThreadsTwoTurnsOneEngine is the deliverable's required unit test: a tiny inline
// 2-turn scenario, run under the OFFLINE test backend, asserting the driver threads BOTH turns
// through ONE engine and returns a well-formed ScenarioResult.
func TestRunScenarioThreadsTwoTurnsOneEngine(t *testing.T) {
	scn := tinyTwoTurnScenario()

	// Drive the raw run directly so we can inspect the per-turn trace split (the engine-threading
	// witness) before scoring projects it into a ScenarioResult.
	run := runScenarioRaw(scn, benchtypes.ArmHarness, fixedSeed, runner.TestFactory, "", 20)
	if run.Unsupported {
		t.Fatalf("harness arm must not be Unsupported: %s", run.Note)
	}

	// (1) Two turns were threaded: one TurnTrace per scripted turn (no planted bucket-0 turns here).
	if len(run.Turns) != 2 {
		t.Fatalf("want 2 threaded turns, got %d (%+v)", len(run.Turns), turnIndices(run.Turns))
	}
	if run.Turns[0].Index != 1 || run.Turns[1].Index != 2 {
		t.Fatalf("turn indices want [1 2], got %v", turnIndices(run.Turns))
	}

	// (2) The turns ran through ONE engine, so the trace ACCUMULATES across turns: the second
	// turn's first tick number is strictly greater than the first turn's last tick (a fresh engine
	// per turn would restart the tick counter at 1). This is the load-bearing "same engine threaded"
	// assertion — distinct engines could not produce a monotonically rising tick sequence.
	t1last := lastTick(run.Turns[0].Events)
	t2first := firstTick(run.Turns[1].Events)
	if t1last == 0 {
		t.Fatal("turn 1 emitted no tick events — the engine never stepped")
	}
	if t2first <= t1last {
		t.Fatalf("ticks did not accumulate across turns (turn1 last tick=%d, turn2 first tick=%d) — "+
			"the engine was NOT threaded across turns", t1last, t2first)
	}

	// (3) The flattened trace is the union of both turns and is non-empty (the oracle substrate).
	if len(run.AllEvents) != len(run.Turns[0].Events)+len(run.Turns[1].Events) {
		t.Fatalf("AllEvents (%d) must equal the sum of per-turn events (%d+%d)",
			len(run.AllEvents), len(run.Turns[0].Events), len(run.Turns[1].Events))
	}
	if len(run.AllEvents) == 0 {
		t.Fatal("the threaded run captured 0 events")
	}

	// (4) The projected ScenarioResult is well-formed: right ID/arm/seed, the deterministic
	// end-state oracle (a tick is present) is satisfied, and the cost reflects a multi-tick run.
	res := scoreScenario(scn, run)
	if res.ID != scn.ID {
		t.Errorf("result ID want %q, got %q", scn.ID, res.ID)
	}
	if res.Arm != benchtypes.ArmHarness {
		t.Errorf("result Arm want harness, got %s", res.Arm)
	}
	if res.Seed != fixedSeed {
		t.Errorf("result Seed want %d, got %d", fixedSeed, res.Seed)
	}
	if !res.OracleVerdict {
		t.Errorf("end-state oracle (tick present) should be satisfied; raw:\n%s", res.RawOutput)
	}
	if res.Cost.Steps < 2 {
		t.Errorf("a 2-turn threaded run should take ≥2 steps, got cost=%+v", res.Cost)
	}
}

// TestRunScenarioEndToEnd exercises the public RunScenario entry point on the same fixture under
// both the bare and harness arms (the two arms the fixture's ablation declares), asserting each
// returns a well-formed result with the right pairing fields.
func TestRunScenarioEndToEnd(t *testing.T) {
	scn := tinyTwoTurnScenario()
	for _, arm := range scn.Ablation.Arms {
		res := RunScenario(scn, arm, fixedSeed, runner.TestFactory, "")
		if res.ID != scn.ID {
			t.Errorf("[%s] result ID want %q, got %q", arm, scn.ID, res.ID)
		}
		if res.Arm != arm {
			t.Errorf("[%s] result Arm mismatch, got %s", arm, res.Arm)
		}
		if res.Seed != fixedSeed {
			t.Errorf("[%s] result Seed want %d, got %d", arm, fixedSeed, res.Seed)
		}
		if res.RawOutput == "" {
			t.Errorf("[%s] result RawOutput must not be empty (audit field)", arm)
		}
	}
}

// TestRetraceGateOffNowSupported asserts the multi-step-retrace gate-off arm now constructs and
// runs a real ablated engine (conscious.allow_backtrack OFF) rather than returning an Unsupported /
// faked result — the §5.8 toggle is wired. It runs to a real OracleVerdict on the trivially-true
// end-state oracle (the driver-level check, not a cognition outcome).
func TestRetraceGateOffNowSupported(t *testing.T) {
	scn := tinyTwoTurnScenario()
	scn.Mechanism = benchtypes.MechMultiStepRetrace
	res := RunScenario(scn, benchtypes.ArmGateOff, fixedSeed, runner.TestFactory, "")
	// A real run scores the event-presence end-state oracle; the engine always emits a tick, so the
	// oracle verdict is true (the arm RAN, it was not stubbed out).
	if !res.OracleVerdict {
		t.Fatalf("retrace gate-off must now run a real engine and satisfy the tick oracle; got %+v", res)
	}
}

// TestUnknownMechanismGateOffIsHonest asserts a gate-off arm for a mechanism with NO registered
// ablation toggle still returns a clean Pass=false result with the TODO reason in RawOutput and
// OracleVerdict=false — never a faked ablation. (All six real mechanisms are now supported, so this
// pins the honesty contract using a synthetic unknown mechanism.)
func TestUnknownMechanismGateOffIsHonest(t *testing.T) {
	scn := tinyTwoTurnScenario()
	scn.Mechanism = benchtypes.Mechanism("no-such-mechanism") // not in the gate-off toggle map
	res := RunScenario(scn, benchtypes.ArmGateOff, fixedSeed, runner.TestFactory, "")
	if res.Pass {
		t.Fatal("an unsupported gate-off ablation must not pass")
	}
	if res.RawOutput == "" || res.OracleVerdict {
		t.Fatalf("unsupported gate-off must carry a reason and OracleVerdict=false; got %+v", res)
	}
}

// TestEvalOracleDeterministicKinds pins the DRY deterministic oracle helpers (exact / numeric /
// set / event-presence) independent of the engine, so the scoring half is unit-tested apart from
// the driver.
func TestEvalOracleDeterministicKinds(t *testing.T) {
	// exact (containment): a sentence containing the canonical value passes.
	exact := benchtypes.Oracle{Kind: benchtypes.OracleExact, Expected: "42", Normalizer: "number"}
	if ok, why := EvalOracle(exact, "the answer is 42", nil); !ok {
		t.Errorf("exact-contains should pass: %s", why)
	}
	if ok, _ := EvalOracle(exact, "the answer is 99", nil); ok {
		t.Error("exact must reject a wrong value")
	}

	// numeric-tolerance.
	num := benchtypes.Oracle{Kind: benchtypes.OracleNumericTolerance, Expected: "3.14", Tolerance: 0.01}
	if ok, why := EvalOracle(num, "approximately 3.139", nil); !ok {
		t.Errorf("numeric within tolerance should pass: %s", why)
	}
	if ok, _ := EvalOracle(num, "3.5", nil); ok {
		t.Error("numeric outside tolerance must fail")
	}

	// set-membership.
	set := benchtypes.Oracle{Kind: benchtypes.OracleSetMembership, ExpectedSet: []string{"red", "green", "blue"}}
	if ok, why := EvalOracle(set, "I'd pick green", nil); !ok {
		t.Errorf("set-membership should pass for a member: %s", why)
	}
	if ok, _ := EvalOracle(set, "purple", nil); ok {
		t.Error("set-membership must reject a non-member")
	}

	// event-presence (reads the trace, not the answer).
	pres := benchtypes.Oracle{Kind: benchtypes.OracleEventPresence, Expected: "critic.decision=BACKTRACK"}
	trace := []events.Event{{Kind: "critic.decision", Data: map[string]any{"decision": "BACKTRACK"}}}
	if ok, why := EvalOracle(pres, "", trace); !ok {
		t.Errorf("event-presence should witness a BACKTRACK decision: %s", why)
	}
	if ok, _ := EvalOracle(pres, "", []events.Event{{Kind: "critic.decision", Data: map[string]any{"decision": "THINK"}}}); ok {
		t.Error("event-presence must fail when the keyed event is absent")
	}

	// rubric is deferred (not silently passed).
	rub := benchtypes.Oracle{Kind: benchtypes.OracleRubric, Expected: "is the summary faithful?"}
	if ok, why := EvalOracle(rub, "anything", nil); ok {
		t.Errorf("rubric must be deferred, not passed deterministically: %s", why)
	}
}

// --- tiny test helpers -----------------------------------------------------

func turnIndices(ts []TurnTrace) []int {
	out := make([]int, len(ts))
	for i, t := range ts {
		out[i] = t.Index
	}
	return out
}

func firstTick(evs []events.Event) int {
	for _, ev := range evs {
		if ev.Kind == events.Tick {
			return ev.Tick
		}
	}
	return 0
}

func lastTick(evs []events.Event) int {
	last := 0
	for _, ev := range evs {
		if ev.Kind == events.Tick {
			last = ev.Tick
		}
	}
	return last
}
