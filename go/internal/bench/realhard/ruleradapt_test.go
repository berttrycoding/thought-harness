package realhard

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/ruler"
)

// ruleradapt_test.go — the realhard → ruler adapter (CAP-EVAL noise-floor
// characterization). Mutation-sensitive: a wrong Success/Replays projection or a
// swapped arm column changes the recovered rows and the ruler's binary verdict.

// mkResult is a terse RunResult builder for the adapter tests.
func mkResult(task string, cap Capability, arm string, replay int, solved bool) RunResult {
	return RunResult{
		TaskID:     task,
		Capability: cap,
		Arm:        arm,
		Replay:     replay,
		Verdict:    Verdict{Solved: solved},
	}
}

// TestHarnessReplayRowsProjection verifies the harness arm's per-task K-replay
// solve count is projected 1:1 into ruler rows (Success = solved replays,
// Replays = K). Mutation guards: a wrong Success count or a swapped bare/harness
// column fails.
func TestHarnessReplayRowsProjection(t *testing.T) {
	// One task, K=3 harness replays: 2 solved, 1 failed. Bare: all 3 solved (must
	// NOT leak into the harness rows — guards an arm swap).
	var results []RunResult
	results = append(results,
		mkResult("t1", CapMultiHopGrounding, ArmHarness, 0, true),
		mkResult("t1", CapMultiHopGrounding, ArmHarness, 1, true),
		mkResult("t1", CapMultiHopGrounding, ArmHarness, 2, false),
		mkResult("t1", CapMultiHopGrounding, ArmBare, 0, true),
		mkResult("t1", CapMultiHopGrounding, ArmBare, 1, true),
		mkResult("t1", CapMultiHopGrounding, ArmBare, 2, true),
	)
	rep := Reduce(results)
	rows := rep.HarnessReplayRows()
	if len(rows) != 1 {
		t.Fatalf("HarnessReplayRows len = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.ID != "t1" {
		t.Errorf("row ID = %q, want t1", r.ID)
	}
	if r.Success != 2 {
		t.Errorf("harness Success = %d, want 2 (2 of 3 replays solved) — guards arm-swap (bare solved 3)", r.Success)
	}
	if r.Replays != 3 {
		t.Errorf("harness Replays = %d, want 3 (K)", r.Replays)
	}

	// Bare projection is the mirror: 3/3.
	bare := rep.BareReplayRows()
	if len(bare) != 1 || bare[0].Success != 3 || bare[0].Replays != 3 {
		t.Errorf("BareReplayRows = %+v, want one row Success=3 Replays=3", bare)
	}
}

// TestCharacterizeHarnessFeedsRuler verifies the adapter rows drive the real
// ruler: a perfectly-stable harness (every task solved every replay) yields a
// DEGENERATE binary verdict (no noise to characterize), while a harness that
// FLIPS run-to-run yields a non-zero within-task sigma. Mutation-sensitive: if
// the adapter dropped the per-task replay structure the sigma would be wrong.
func TestCharacterizeHarnessFeedsRuler(t *testing.T) {
	// Stable case: 2 tasks, K=3, all solved → within-var 0, between-var 0 → DEGENERATE.
	var stable []RunResult
	for _, task := range []string{"a", "b"} {
		for r := 0; r < 3; r++ {
			stable = append(stable, mkResult(task, CapLongHorizonConsistency, ArmHarness, r, true))
		}
	}
	cs := Reduce(stable).CharacterizeHarness(ruler.Options{})
	if cs.Verdict != ruler.VerdictDegenerate {
		t.Errorf("all-solved harness verdict = %s, want DEGENERATE (no noise to characterize)", cs.Verdict)
	}
	if cs.SigmaNoise != 0 {
		t.Errorf("all-solved sigma_noise = %v, want 0", cs.SigmaNoise)
	}

	// Wobbly case: task a flips (2/3), task b flips (1/3) → non-zero within sigma.
	var wobbly []RunResult
	aSolves := []bool{true, true, false}
	bSolves := []bool{true, false, false}
	for r := 0; r < 3; r++ {
		wobbly = append(wobbly, mkResult("a", CapMultiHopGrounding, ArmHarness, r, aSolves[r]))
		wobbly = append(wobbly, mkResult("b", CapMultiHopGrounding, ArmHarness, r, bSolves[r]))
	}
	cw := Reduce(wobbly).CharacterizeHarness(ruler.Options{})
	if cw.SigmaNoise <= 0 {
		t.Errorf("wobbly harness sigma_noise = %v, want > 0 (replay flips are noise)", cw.SigmaNoise)
	}
	if cw.K != 3 {
		t.Errorf("characterization K = %d, want 3", cw.K)
	}
	if cw.Tasks != 2 {
		t.Errorf("characterization Tasks = %d, want 2", cw.Tasks)
	}
	// Within-task variance for a=2/3 and b=1/3 is p(1-p) = 2/9 each → pooled 2/9.
	// sigma = sqrt(2/9) ≈ 0.4714. Guard the exact value (mutation-sensitive).
	want := 0.4714045207910317
	if d := cw.SigmaNoise - want; d > 1e-9 || d < -1e-9 {
		t.Errorf("wobbly sigma_noise = %v, want %v (sqrt of pooled p(1-p))", cw.SigmaNoise, want)
	}
}
