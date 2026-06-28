package synthfidelity

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
)

// bankPath is the A5 fixture bank, relative to this package dir.
const bankPath = "../banks/synth-fidelity/synth-fidelity.jsonl"

// loadBank loads the gold bank or fails the test loud.
func loadBank(t *testing.T) []Fixture {
	t.Helper()
	fx, err := LoadFixtures(bankPath)
	if err != nil {
		t.Fatalf("load bank %q: %v", bankPath, err)
	}
	if len(fx) == 0 {
		t.Fatalf("bank %q is empty", bankPath)
	}
	return fx
}

// TestDiscrimination is THE fail-discriminating control — the property that makes
// this oracle worth anything: on every fixture, the hand-written GOOD synthesis
// (the gold decomposition) must score >= the fixture's threshold (a structural
// PASS) and the hand-written BAD synthesis (plausible-but-wrong: a flattened
// branch, a wrong-family op, a missing act) must score < the threshold (a FAIL).
// An oracle that scored both the same — or scored the bad one a pass — could not
// tell faithful synthesis from plausible-but-wrong synthesis and would be useless.
func TestDiscrimination(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	w := DefaultWeights()
	fixtures := loadBank(t)

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.ID, func(t *testing.T) {
			threshold := fx.PassThreshold
			if threshold <= 0 {
				threshold = defaultPassThreshold
			}

			good := ScoreProgram(fx.GoodProgram, fx, cat, w)
			bad := ScoreProgram(fx.BadProgram, fx, cat, w)

			if !good.Parsed {
				t.Fatalf("GOOD program did not parse/verify: %s", good.Reason)
			}
			if !good.Pass {
				t.Errorf("GOOD synthesis should PASS but scored %.3f < threshold %.3f\n  reason: %s",
					good.Score, threshold, good.Reason)
			}
			if bad.Pass {
				t.Errorf("BAD synthesis should FAIL but scored %.3f >= threshold %.3f\n  reason: %s",
					bad.Score, threshold, bad.Reason)
			}
			// The discrimination MARGIN: good must out-score bad by a clear gap, not
			// scrape past on noise. (A non-trivial separation proves the oracle keys on
			// the structural difference, not on a coincidence of the threshold.)
			if good.Score <= bad.Score {
				t.Errorf("no discrimination: GOOD %.3f <= BAD %.3f (oracle cannot tell them apart)\n  good: %s\n  bad:  %s",
					good.Score, bad.Score, good.Reason, bad.Reason)
			}
			margin := good.Score - bad.Score
			if margin < 0.15 {
				t.Errorf("discrimination margin too thin: GOOD %.3f - BAD %.3f = %.3f (< 0.15)",
					good.Score, bad.Score, margin)
			}
			// The DECISION-BOUNDARY gap: the BAD must fail by a real distance BELOW the
			// threshold, not by a hair — else a future weight retune silently lets a
			// plausible-but-wrong program (e.g. a flattened branch) slip past. (sf-deliberator-0001
			// was the thin one at 0.011 before its threshold was raised to 0.80.)
			if gap := threshold - bad.Score; gap < 0.05 {
				t.Errorf("BAD-to-threshold gap too thin: threshold %.3f - BAD %.3f = %.3f (< 0.05) — weight-fragile",
					threshold, bad.Score, gap)
			}
			t.Logf("OK [%s] good=%.3f bad=%.3f margin=%.3f thr=%.2f", fx.Worker, good.Score, bad.Score, margin, threshold)
		})
	}
}

// TestUnparseableProgramFailsHard proves an unparseable or structurally-invalid
// program is a HARD fail (Parsed=false, Score 0), never a silent pass — the
// anti-vacuous-pass discipline. Three cases: a malformed dict (unknown kind), an
// empty program (no synthesis produced), and an UNKNOWN operator (verification
// rejects it).
func TestUnparseableProgramFailsHard(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	w := DefaultWeights()
	fx := Fixture{ID: "probe", Expect: Expect{MustOperators: []string{"decompose"}}, PassThreshold: 0.75}

	// Unknown node kind -> NodeFromDict errors.
	bad := ProgramShape{"kind": "frobnicate"}
	v := ScoreProgram(bad, fx, cat, w)
	if v.Parsed || v.Pass || v.Score != 0 {
		t.Errorf("malformed program should hard-fail: parsed=%v pass=%v score=%.3f", v.Parsed, v.Pass, v.Score)
	}

	// Empty / absent program.
	v = ScoreProgram(nil, fx, cat, w)
	if v.Parsed || v.Pass || v.Score != 0 {
		t.Errorf("empty program should hard-fail: parsed=%v pass=%v score=%.3f", v.Parsed, v.Pass, v.Score)
	}

	// Unknown operator -> VerifyProgram rejects (structural invalidity).
	unknownOp := ProgramShape{"kind": "step", "operator": "telekinesis", "domain": "general"}
	v = ScoreProgram(unknownOp, fx, cat, w)
	if v.Parsed || v.Pass {
		t.Errorf("unknown-operator program should fail verification: parsed=%v pass=%v reason=%s", v.Parsed, v.Pass, v.Reason)
	}
}

// TestVacuousFixtureNeverPasses proves a fixture that constrains NOTHING (no
// Expect) scores 0 and fails with a loud reason — a bank error must never read as a
// vacuous pass (the oracle is only as honest as its refusal to pass un-checked
// structure).
func TestVacuousFixtureNeverPasses(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	w := DefaultWeights()
	fx := Fixture{ID: "vacuous", Expect: Expect{}, PassThreshold: 0.75}
	// A perfectly fine program, but nothing to check it against.
	prog := ProgramShape{"kind": "step", "operator": "decompose", "domain": "general"}
	v := ScoreProgram(prog, fx, cat, w)
	if v.Pass || v.Score != 0 {
		t.Errorf("vacuous fixture should never pass: pass=%v score=%.3f reason=%s", v.Pass, v.Score, v.Reason)
	}
	if !strings.Contains(v.Reason, "no Expect constraints") {
		t.Errorf("vacuous fixture reason should name the bank error, got: %s", v.Reason)
	}
}

// TestActOnRealityDiscriminates is a focused control on the ACT faculty (the
// Investigator/Verifier signal): the SAME program, once with a tool-scoped/reality-
// source step (acts) and once without (pure reasoning), must score the act
// criterion 1 and 0 respectively. This proves the oracle keys on the grounded
// crossing, not on the operator name.
func TestActOnRealityDiscriminates(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	w := DefaultWeights()
	fx := Fixture{ID: "act-probe", PassThreshold: 0.75, Expect: Expect{ActOnReality: true}}

	// validate carries the run_tests tool-scope -> acts on reality.
	acts := programShapeOf(cognition.Program{Root: cognition.NewSeq(
		cognition.NewStep("decompose", "general", ""),
		cognition.NewStep("validate", "general", ""),
	)})
	va := ScoreProgram(acts, fx, cat, w)
	if !va.Pass || va.Criteria["act"] != 1.0 {
		t.Errorf("tool-scoped program should satisfy act-on-reality: pass=%v act=%v reason=%s", va.Pass, va.Criteria["act"], va.Reason)
	}

	// decompose + compare are pure reasoning -> no act.
	noAct := programShapeOf(cognition.Program{Root: cognition.NewSeq(
		cognition.NewStep("decompose", "general", ""),
		cognition.NewStep("compare", "general", ""),
	)})
	vn := ScoreProgram(noAct, fx, cat, w)
	if vn.Pass || vn.Criteria["act"] != 0.0 {
		t.Errorf("pure-reasoning program should fail act-on-reality: pass=%v act=%v reason=%s", vn.Pass, vn.Criteria["act"], vn.Reason)
	}
}
