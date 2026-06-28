package value

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// branchWithFinal builds an identical 2-thought branch (so the bootstrap terms recent_conf + goal_sim
// are held equal) whose ONLY difference is the final thought's grounding: an OK observation, a refuted
// observation, or a non-observation (ungrounded). This isolates V(s)'s grounded component.
func branchWithFinal(source types.Source, raw any) *graph.ThoughtGraph {
	g := graph.New("is the deploy safe to ship to production")
	g.Append(&types.Thought{ID: -1, Text: "weighing the deploy against the safety checklist",
		Source: types.GENERATED, Confidence: 0.7}, 1)
	g.Append(&types.Thought{ID: -1, Text: "reality: ran the verification check",
		Source: source, Confidence: 0.9, RawReturn: raw}, 2)
	return g
}

// TestVsCalibrationProbe is the X.1 probe: MEASURE that the bootstrap value signal is calibrated to
// reality BEFORE any RL is layered on it — holding the bootstrap priors equal, a reality-CONFIRMED line
// must score strictly higher than an ungrounded one, which must score strictly higher than a
// reality-REFUTED one. If this ordering failed, V(s) would be a broken signal to RL-ground against.
func TestVsCalibrationProbe(t *testing.T) {
	v := New(nil)

	vOK := v.AppraiseBranch(branchWithFinal(types.OBSERVATION, types.Observation{Ok: true}), 0).Value
	vFail := v.AppraiseBranch(branchWithFinal(types.OBSERVATION, types.Observation{Ok: false}), 0).Value
	vNone := v.AppraiseBranch(branchWithFinal(types.GENERATED, nil), 0).Value

	t.Logf("V(s) calibration probe: confirmed=%.3f  ungrounded=%.3f  refuted=%.3f", vOK, vNone, vFail)
	t.Logf("  grounded lift (confirmed - ungrounded) = %+.3f ; penalty (refuted - ungrounded) = %+.3f",
		vOK-vNone, vFail-vNone)

	// calibration ordering: reality moves V in the right direction.
	if !(vOK > vNone && vNone > vFail) {
		t.Fatalf("V(s) is NOT calibrated to grounded reality: confirmed=%.3f ungrounded=%.3f refuted=%.3f",
			vOK, vNone, vFail)
	}

	// magnitude: the confirmed lift should be ~+0.3 and the refute penalty ~-0.2 (the grounded weights),
	// modulo [0,1] clamping. This quantifies the signal's strength for the "measure before RL" record.
	if vNone < 1.0 && math.Abs((vOK-vNone)-0.3) > 0.05 {
		t.Errorf("confirmed lift = %+.3f, expected ~+0.30", vOK-vNone)
	}
	if vNone > 0.0 && math.Abs((vFail-vNone)-(-0.2)) > 0.05 {
		t.Errorf("refuted penalty = %+.3f, expected ~-0.20", vFail-vNone)
	}
}

// TestVsRewardIsGroundedOnly pins the other calibration guarantee: Reward comes ONLY from grounded
// OBSERVATIONs, never from self-graded thinking — so the signal RL would optimise can't be gamed by the
// model confidently asserting success.
func TestVsRewardIsGroundedOnly(t *testing.T) {
	v := New(nil)

	// a branch full of confident GENERATED thoughts but no observation -> zero reward (no self-grading).
	g := graph.New("solve the puzzle")
	g.Append(&types.Thought{ID: -1, Text: "I'm certain this is right", Source: types.GENERATED, Confidence: 0.99}, 1)
	g.Append(&types.Thought{ID: -1, Text: "definitely solved it", Source: types.GENERATED, Confidence: 0.99}, 2)
	if r := v.Reward(g); r != 0 {
		t.Fatalf("confident self-grading must yield ZERO reward (grounded-only); got %.3f", r)
	}

	// add a real grounded OBSERVATION -> reward becomes non-zero.
	g.Append(&types.Thought{ID: -1, Text: "reality: the checker accepted it", Source: types.OBSERVATION,
		Confidence: 0.95, RawReturn: types.Observation{Ok: true}}, 3)
	if r := v.Reward(g); r <= 0 {
		t.Fatalf("a confirmed grounded observation should produce positive reward; got %.3f", r)
	}
}
