package realhard

import (
	"strings"
	"testing"
)

// abdelta_test.go — the bare-vs-harness A/B arm (design doc §7.2; instrument 3). The
// arm machinery (RunBare / RunHarness / RunSuite / Reduce) already exists; this test
// pins the LOAD-BEARING contract the methodology rests on: the reducer computes a
// CORRECT per-arm solve-rate and the bare->harness LIFT (does the harness ADD over
// the raw model?), per-arm and per-capability, with the arms NOT cross-contaminated.

// TestABDeltaReportsLift: bare solves a meaningful fraction (the headroom), harness
// recovers more; the report's lift = harness-rate - bare-rate is computed correctly
// and the per-arm columns are not swapped.
func TestABDeltaReportsLift(t *testing.T) {
	// 2 tasks x 2 replays. bare: 1/4 solved (25%). harness: 3/4 solved (75%). lift +50pp.
	var results []RunResult
	results = append(results,
		// task t1: bare 0/2, harness 2/2
		mkResult("t1", CapMultiHopGrounding, ArmBare, 0, false),
		mkResult("t1", CapMultiHopGrounding, ArmBare, 1, false),
		mkResult("t1", CapMultiHopGrounding, ArmHarness, 0, true),
		mkResult("t1", CapMultiHopGrounding, ArmHarness, 1, true),
		// task t2: bare 1/2, harness 1/2
		mkResult("t2", CapAdaptiveBacktracking, ArmBare, 0, true),
		mkResult("t2", CapAdaptiveBacktracking, ArmBare, 1, false),
		mkResult("t2", CapAdaptiveBacktracking, ArmHarness, 0, true),
		mkResult("t2", CapAdaptiveBacktracking, ArmHarness, 1, false),
	)
	rep := Reduce(results)

	if rep.Bare.Runs != 4 || rep.Harness.Runs != 4 {
		t.Fatalf("each arm should have 4 runs; bare=%d harness=%d", rep.Bare.Runs, rep.Harness.Runs)
	}
	if rep.Bare.Solved != 1 {
		t.Errorf("bare solved = %d, want 1 (the headroom)", rep.Bare.Solved)
	}
	if rep.Harness.Solved != 3 {
		t.Errorf("harness solved = %d, want 3 (the recovery)", rep.Harness.Solved)
	}
	if rep.Bare.solveRate() != 0.25 {
		t.Errorf("bare solve-rate = %g, want 0.25", rep.Bare.solveRate())
	}
	if rep.Harness.solveRate() != 0.75 {
		t.Errorf("harness solve-rate = %g, want 0.75", rep.Harness.solveRate())
	}
	// the LIFT (the methodology's headline): harness - bare = +50pp.
	lift := rep.Harness.solveRate() - rep.Bare.solveRate()
	if lift != 0.50 {
		t.Errorf("bare->harness lift = %g, want +0.50 (the harness ADDS over the raw model)", lift)
	}

	// per-capability: t1 (multi-hop) shows the full lift; t2 (backtracking) shows none.
	mhBare := rep.ByCapBare[CapMultiHopGrounding]
	mhHarn := rep.ByCapHarness[CapMultiHopGrounding]
	if mhBare.solveRate() != 0 || mhHarn.solveRate() != 1 {
		t.Errorf("multi-hop: bare %g -> harness %g, want 0 -> 1", mhBare.solveRate(), mhHarn.solveRate())
	}
	btBare := rep.ByCapBare[CapAdaptiveBacktracking]
	btHarn := rep.ByCapHarness[CapAdaptiveBacktracking]
	if btBare.solveRate() != 0.5 || btHarn.solveRate() != 0.5 {
		t.Errorf("backtracking: bare %g -> harness %g, want 0.5 -> 0.5 (no lift)", btBare.solveRate(), btHarn.solveRate())
	}

	// the rendered report names the lift and the arms (mutation guard on the renderer).
	out := rep.Render("test")
	if !strings.Contains(out, "LIFT") || !strings.Contains(out, "bare") || !strings.Contains(out, "harness") {
		t.Error("the rendered A/B report must name the lift and both arms")
	}
}

// TestABArmsNotCrossContaminated: a bare solve must not leak into the harness tally
// (the arm-swap guard the methodology depends on).
func TestABArmsNotCrossContaminated(t *testing.T) {
	results := []RunResult{
		mkResult("t1", CapMultiHopGrounding, ArmBare, 0, true), // bare solves
		mkResult("t1", CapMultiHopGrounding, ArmHarness, 0, false),
	}
	rep := Reduce(results)
	if rep.Bare.Solved != 1 || rep.Harness.Solved != 0 {
		t.Errorf("arm tallies cross-contaminated: bare.Solved=%d (want 1) harness.Solved=%d (want 0)",
			rep.Bare.Solved, rep.Harness.Solved)
	}
}
