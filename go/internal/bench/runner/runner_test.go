package runner

import (
	"strings"
	"testing"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// ev builds a synthetic Event with the layer derived from the kind (as the bus does), for
// the trace-reading predicate tests.
func ev(kind string, data map[string]any) events.Event {
	layer := kind
	if i := strings.IndexByte(kind, '.'); i >= 0 {
		layer = kind[:i]
	}
	if data == nil {
		data = map[string]any{}
	}
	return events.Event{Kind: kind, Layer: layer, Data: data}
}

// traceOf collects synthetic events into a slice, oldest-to-newest.
func traceOf(evs ...events.Event) []events.Event { return evs }

// fixedSeed is the paired seed every arm in the smoke test runs under (spec §5.1: the same
// seed across arms makes the contrast paired).
const fixedSeed int64 = 7

// newSmokeRunner builds a Runner on the OFFLINE deterministic test double (backends.NewTest
// via TestFactory) with no Action workspace — no network, no real LLM, fully reproducible.
func newSmokeRunner() *Runner { return New(TestFactory, "") }

// TestBareArmReturnsAnswer asserts the bare arm runs the minimal one-shot backend.Generate
// loop, returns text without error, and reports the single-call cost — no harness, no events.
func TestBareArmReturnsAnswer(t *testing.T) {
	r := newSmokeRunner()
	run := r.Run(Spec{
		Prompt:    "What is 17 plus 25?",
		Arm:       benchtypes.ArmBare,
		Mechanism: benchtypes.MechGrounding,
		Seed:      fixedSeed,
	})
	if run.Unsupported {
		t.Fatalf("bare arm must not be Unsupported: %s", run.Note)
	}
	if run.Text == "" {
		t.Fatal("bare arm returned empty text (the single Generate call produced nothing)")
	}
	if len(run.Events) != 0 {
		t.Fatalf("bare arm has no event bus; want 0 events, got %d", len(run.Events))
	}
	if run.Cost.ModelCalls != 1 || run.Cost.Steps != 1 {
		t.Fatalf("bare cost want {ModelCalls:1 Steps:1}, got %+v", run.Cost)
	}
}

// TestHarnessArmProducesEvents asserts the harness arm constructs the full engine, runs a
// reactive episode without error, and the in-memory collector captured > 0 events (the
// observability spine the isolation predicates read).
func TestHarnessArmProducesEvents(t *testing.T) {
	r := newSmokeRunner()
	run := r.Run(Spec{
		Prompt:    "What is 17 plus 25?",
		Arm:       benchtypes.ArmHarness,
		Mechanism: benchtypes.MechGrounding,
		Seed:      fixedSeed,
		MaxTicks:  20,
	})
	if run.Unsupported {
		t.Fatalf("harness arm must not be Unsupported: %s", run.Note)
	}
	if len(run.Events) == 0 {
		t.Fatal("harness arm captured 0 events (the collector never received the trace)")
	}
	if run.Cost.Steps == 0 {
		t.Fatalf("harness arm ran 0 steps; cost=%+v", run.Cost)
	}
}

// TestGateOnIsHarnessAlias asserts gate-on builds the same all-on engine as harness and
// likewise produces a trace.
func TestGateOnIsHarnessAlias(t *testing.T) {
	r := newSmokeRunner()
	run := r.Run(Spec{
		Prompt:    "Settle the value of the constant.",
		Arm:       benchtypes.ArmGateOn,
		Mechanism: benchtypes.MechGrounding,
		Seed:      fixedSeed,
		MaxTicks:  20,
	})
	if run.Unsupported {
		t.Fatalf("gate-on arm must not be Unsupported: %s", run.Note)
	}
	if len(run.Events) == 0 {
		t.Fatal("gate-on arm captured 0 events")
	}
}

// TestAblationArmConstructsAndRuns asserts a supported gate-off arm (stability via
// regulator.enforce=off) constructs with exactly that toggle flipped and runs to completion.
func TestAblationArmConstructsAndRuns(t *testing.T) {
	// Sanity: the gate-off map flips exactly regulator.enforce for stability.
	toggles := GateOffToggles(benchtypes.MechStability)
	if len(toggles) != 1 || toggles[0] != "regulator.enforce" {
		t.Fatalf("stability gate-off toggles want [regulator.enforce], got %v", toggles)
	}
	r := newSmokeRunner()
	run := r.Run(Spec{
		Prompt:    "Keep diagnosing until you find the failing case.",
		Arm:       benchtypes.ArmGateOff,
		Mechanism: benchtypes.MechStability,
		Seed:      fixedSeed,
		MaxTicks:  20,
	})
	if run.Unsupported {
		t.Fatalf("stability gate-off must be supported (regulator.enforce exists): %s", run.Note)
	}
	if len(run.Events) == 0 {
		t.Fatal("stability gate-off arm captured 0 events")
	}
}

// TestRetraceAndAwakeAblationsAreSupported asserts the two formerly-UNSUPPORTED mechanisms now
// build a real gate-off arm (the conscious.allow_backtrack / conscious.endogenous_drive toggles
// added per measuring-stick-spec §5.8) rather than returning a faked/Unsupported ablation.
func TestRetraceAndAwakeAblationsAreSupported(t *testing.T) {
	r := newSmokeRunner()
	cases := []struct {
		mech   benchtypes.Mechanism
		toggle string
	}{
		{benchtypes.MechMultiStepRetrace, "conscious.allow_backtrack"},
		{benchtypes.MechContinuousAutonomy, "conscious.endogenous_drive"},
	}
	for _, c := range cases {
		if !SupportedGateOff(c.mech) {
			t.Fatalf("%s must now report SupportedGateOff=true (toggle %s added)", c.mech, c.toggle)
		}
		toggles := GateOffToggles(c.mech)
		if len(toggles) != 1 || toggles[0] != c.toggle {
			t.Fatalf("%s gate-off toggles want [%s], got %v", c.mech, c.toggle, toggles)
		}
		run := r.Run(Spec{
			Prompt:    "Keep working the problem until it is settled.",
			Arm:       benchtypes.ArmGateOff,
			Mechanism: c.mech,
			Seed:      fixedSeed,
			MaxTicks:  20,
		})
		if run.Unsupported {
			t.Fatalf("%s gate-off must now be supported: %s", c.mech, run.Note)
		}
		if len(run.Events) == 0 {
			t.Fatalf("%s gate-off arm captured 0 events", c.mech)
		}
	}
}

// TestSingleStrongArmBuildsTheGuardReference asserts the SUB-AGENT GUARD reference arm (the BENCH-SUITE-A2
// residue): the `single-strong` arm builds the FULL engine with EXACTLY the single_strong_agent collapse
// flipped ON (not a faked ablation, not the bare model), runs to completion, and — on a multi-faculty goal —
// EMITS subconscious.single_strong, proving the collapse is genuinely wired and the arm is a NON-identical
// engine from the full-harness arm (the guard's A/B is two distinct plants). This is the failure mode the
// build closes: a single-strong arm that silently ran the full-fan-out engine would make the
// teams-vs-best-member guard compare a plant against itself.
func TestSingleStrongArmBuildsTheGuardReference(t *testing.T) {
	r := newSmokeRunner()
	run := r.Run(Spec{
		// a multi-faculty goal (safety/change + arithmetic + a social opener) so a tick fans out a real team.
		Prompt:    "hi — is it safe to ship this refactor, and also what is 6 times 7? think it through.",
		Arm:       benchtypes.ArmSingleStrong,
		Mechanism: benchtypes.MechGrounding,
		Seed:      fixedSeed,
		MaxTicks:  30,
	})
	if run.Unsupported {
		t.Fatalf("single-strong arm must not be Unsupported (the knob path must resolve): %s", run.Note)
	}
	if len(run.Events) == 0 {
		t.Fatal("single-strong arm captured 0 events (the engine never ran)")
	}
	// the collapse MUST be witnessed on the trace — proving the arm's engine genuinely DIFFERS from the
	// full harness (a teammate was dropped). A single-strong arm with no collapse event is the BENCH-SUITE-A2
	// bug: the flag changed nothing and the guard's two arms are identical engines.
	var collapsed bool
	for _, e := range run.Events {
		if e.Kind == string(events.SubSingleStrong) {
			collapsed = true
			break
		}
	}
	if !collapsed {
		t.Fatal("single-strong arm fired no subconscious.single_strong event — the fan-out was NOT collapsed, " +
			"so this arm is IDENTICAL to the full-harness arm (the guard would be vacuous)")
	}
}

// TestSingleStrongArmDiffersFromHarness is the load-bearing guard property at the runner level: the
// single-strong arm and the harness arm, on the SAME paired seed and prompt, are NOT the same engine — the
// harness fans a team out, the single-strong arm collapses it. The single-strong run carries the collapse
// event; the harness run never does. (The two are intentionally distinct plants — exactly what lets the
// teams-vs-best-member guard prove the harness BEATS its best single member, or that the sub-agent layer is
// anti-value.)
func TestSingleStrongArmDiffersFromHarness(t *testing.T) {
	r := newSmokeRunner()
	const prompt = "hi — is it safe to ship this refactor, and also what is 6 times 7? think it through."
	hasCollapse := func(arm benchtypes.Arm) bool {
		run := r.Run(Spec{Prompt: prompt, Arm: arm, Mechanism: benchtypes.MechGrounding, Seed: fixedSeed, MaxTicks: 30})
		if run.Unsupported {
			t.Fatalf("%s arm Unsupported: %s", arm, run.Note)
		}
		for _, e := range run.Events {
			if e.Kind == string(events.SubSingleStrong) {
				return true
			}
		}
		return false
	}
	if hasCollapse(benchtypes.ArmHarness) {
		t.Fatal("the full-harness arm emitted a single_strong collapse — the full fan-out must be the default (no collapse)")
	}
	if !hasCollapse(benchtypes.ArmSingleStrong) {
		t.Fatal("the single-strong arm did NOT collapse the team — it is identical to the harness arm (guard vacuous)")
	}
}

// TestSupportedGateOffMap pins the supported-vs-unsupported split (the deliverable's
// arm-support contract). All six mechanisms now have a gate-off toggle.
func TestSupportedGateOffMap(t *testing.T) {
	supported := []benchtypes.Mechanism{
		benchtypes.MechGrounding,
		benchtypes.MechSelfImprovement,
		benchtypes.MechStability,
		benchtypes.MechSafety,
		benchtypes.MechMultiStepRetrace,
		benchtypes.MechContinuousAutonomy,
	}
	for _, m := range supported {
		if !SupportedGateOff(m) {
			t.Errorf("%s must be a supported gate-off mechanism", m)
		}
	}
}

// TestPredicateRegistryIsTotal asserts every mechanism has a registered isolation predicate
// (so a Tier-A/Tier-B scorer can always ask "was the mechanism genuinely used").
func TestPredicateRegistryIsTotal(t *testing.T) {
	for _, m := range []benchtypes.Mechanism{
		benchtypes.MechGrounding,
		benchtypes.MechMultiStepRetrace,
		benchtypes.MechSelfImprovement,
		benchtypes.MechContinuousAutonomy,
		benchtypes.MechStability,
		benchtypes.MechSafety,
	} {
		if _, ok := PredicateFor(m); !ok {
			t.Errorf("no isolation predicate registered for %s", m)
		}
	}
}

// TestIsolationPredicatesReadTrace exercises the primitive predicates against synthetic
// traces so the witness/no-witness logic is pinned independent of engine behaviour.
func TestIsolationPredicatesReadTrace(t *testing.T) {
	// BacktrackFired: a critic.decision=BACKTRACK is the witness.
	withBacktrack := traceOf(ev("critic.decision", map[string]any{"decision": "BACKTRACK"}))
	if r := BacktrackFired(withBacktrack); !r.OK {
		t.Errorf("BacktrackFired should witness a BACKTRACK decision: %s", r.Reason)
	}
	noBacktrack := traceOf(ev("critic.decision", map[string]any{"decision": "THINK"}))
	if r := BacktrackFired(noBacktrack); r.OK {
		t.Error("BacktrackFired must not witness a THINK-only trace")
	}

	// GateBlocked: an action.safety_block is the witness.
	blocked := traceOf(ev("action.safety_block", nil))
	if r := GateBlocked(blocked); !r.OK {
		t.Errorf("GateBlocked should witness action.safety_block: %s", r.Reason)
	}
	if r := GateBlocked(traceOf(ev("action.tool", nil))); r.OK {
		t.Error("GateBlocked must not witness a plain action.tool trace")
	}

	// MintThenReused: a convert.mint(specialist, domain=X) followed by a subconscious.fire(domain=X).
	minted := traceOf(
		ev("convert.mint", map[string]any{"kind": "specialist", "domain": "learned:zip"}),
		ev("subconscious.fire", map[string]any{"domain": "learned:zip"}),
	)
	if r := MintThenReused(minted); !r.OK {
		t.Errorf("MintThenReused should witness mint-then-fire: %s", r.Reason)
	}
	mintNoReuse := traceOf(ev("convert.mint", map[string]any{"kind": "specialist", "domain": "learned:zip"}))
	if r := MintThenReused(mintNoReuse); r.OK {
		t.Error("MintThenReused must fail when the minted specialist is never reused")
	}

	// GroundingReadHappened: a grounding.ground is the witness.
	if r := GroundingReadHappened(traceOf(ev("grounding.ground", nil))); !r.OK {
		t.Errorf("GroundingReadHappened should witness grounding.ground: %s", r.Reason)
	}
	if r := GroundingReadHappened(traceOf(ev("conscious.generate", nil))); r.OK {
		t.Error("GroundingReadHappened must not witness a generate-only trace (prior-only answer)")
	}

	// ObservationRefuted: an action.observation ok=false is the witness.
	if r := ObservationRefuted(traceOf(ev("action.observation", map[string]any{"ok": false}))); !r.OK {
		t.Errorf("ObservationRefuted should witness ok=false: %s", r.Reason)
	}
	if r := ObservationRefuted(traceOf(ev("action.observation", map[string]any{"ok": true}))); r.OK {
		t.Error("ObservationRefuted must not witness an ok=true observation")
	}
}

// TestDivergedHelper pins the gate-on/gate-off divergence helper.
func TestDivergedHelper(t *testing.T) {
	on := ArmRun{Mechanism: benchtypes.MechGrounding, Text: "the value is 42", Arm: benchtypes.ArmGateOn}
	off := ArmRun{Mechanism: benchtypes.MechGrounding, Text: "the value is 99", Arm: benchtypes.ArmGateOff}
	if diverged, _ := Diverged(on, off); !diverged {
		t.Error("Diverged must report true when gate-on and gate-off answers differ")
	}
	same := ArmRun{Mechanism: benchtypes.MechGrounding, Text: "the value is 42", Arm: benchtypes.ArmGateOff}
	if diverged, _ := Diverged(on, same); diverged {
		t.Error("Diverged must report false (non-isolating) when answers match and no witness was lost")
	}
	unsup := ArmRun{Mechanism: benchtypes.MechMultiStepRetrace, Unsupported: true, Arm: benchtypes.ArmGateOff}
	if diverged, _ := Diverged(on, unsup); diverged {
		t.Error("Diverged must report false for an unsupported gate-off arm")
	}
}
