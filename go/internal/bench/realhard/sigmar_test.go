package realhard

import (
	"math"
	"testing"
)

// sigmar_test.go — the σ_R MATH unit tests (the gate prerequisite, OFFLINE: no model, no engine).
// They feed synthetic per-launch×per-task rate matrices into ComputeSigmaR (the PURE reduction) and
// assert the four properties the gate depends on:
//   - all-identical input → σ_R = 0 (no run-level variance);
//   - a known-variance input → the KNOWN sample SD (the instrument computes the right number);
//   - the mean(s) are correct (the mean-guard companion + per-task mean);
//   - PER-TASK ISOLATION — one task's variance does NOT leak into another task's σ.
// A sound σ_R instrument must pass all four before any claude spend.

const sigEps = 1e-9

func approx(a, b float64) bool { return math.Abs(a-b) <= sigEps }

// TestSigmaRAllIdenticalIsZero: every launch gives every task the SAME rate → each per-task σ_R is
// exactly 0, the mean σ_R is 0, and the mean solve-rate is the shared rate. A robust config (no
// run-to-run swing) reads as σ_R=0 — the floor the deliberative lever drives toward.
func TestSigmaRAllIdenticalIsZero(t *testing.T) {
	// R=4 launches, T=3 tasks, all 1.0 (every launch solves every task).
	rates := [][]float64{
		{1, 1, 1},
		{1, 1, 1},
		{1, 1, 1},
		{1, 1, 1},
	}
	rows, meanSigma, meanRate := ComputeSigmaR(rates, []string{"a", "b", "c"}, nil)
	if len(rows) != 3 {
		t.Fatalf("want 3 task rows, got %d", len(rows))
	}
	for _, r := range rows {
		if !approx(r.SigmaR, 0) {
			t.Errorf("[%s] all-identical input must give σ_R=0, got %g", r.TaskID, r.SigmaR)
		}
		if !approx(r.MeanRate, 1) {
			t.Errorf("[%s] mean-rate must be 1.0, got %g", r.TaskID, r.MeanRate)
		}
	}
	if !approx(meanSigma, 0) {
		t.Errorf("mean σ_R must be 0 for an all-identical matrix, got %g", meanSigma)
	}
	if !approx(meanRate, 1) {
		t.Errorf("mean solve-rate must be 1.0, got %g", meanRate)
	}

	// A different shared rate (0.5 everywhere) — still zero variance, mean 0.5.
	rates2 := [][]float64{{0.5, 0.5}, {0.5, 0.5}, {0.5, 0.5}}
	rows2, meanSigma2, meanRate2 := ComputeSigmaR(rates2, []string{"x", "y"}, nil)
	for _, r := range rows2 {
		if !approx(r.SigmaR, 0) {
			t.Errorf("[%s] identical-0.5 σ_R must be 0, got %g", r.TaskID, r.SigmaR)
		}
	}
	if !approx(meanSigma2, 0) || !approx(meanRate2, 0.5) {
		t.Errorf("identical-0.5: meanσ=%g (want 0) meanRate=%g (want 0.5)", meanSigma2, meanRate2)
	}
}

// TestSigmaRKnownVariance: a column with a HAND-COMPUTED sample SD must come back exactly. The
// vector {0,1,0,1} has mean 0.5 and sample SD sqrt( ((0.25*4)) / 3 ) = sqrt(1/3) ≈ 0.5773502692.
// This is the load-bearing "the instrument computes the right number" assertion.
func TestSigmaRKnownVariance(t *testing.T) {
	// Single task, R=4 launches with the {0,1,0,1} pattern (a maximally-swinging binary task).
	rates := [][]float64{{0}, {1}, {0}, {1}}
	rows, meanSigma, meanRate := ComputeSigmaR(rates, []string{"swing"}, nil)
	wantSD := math.Sqrt(1.0 / 3.0) // sample SD (n-1=3) of {0,1,0,1}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if !approx(rows[0].SigmaR, wantSD) {
		t.Errorf("σ_R of {0,1,0,1} = %g, want %g (sqrt(1/3))", rows[0].SigmaR, wantSD)
	}
	if !approx(meanSigma, wantSD) {
		t.Errorf("mean σ_R (single task) = %g, want %g", meanSigma, wantSD)
	}
	if !approx(meanRate, 0.5) {
		t.Errorf("mean solve-rate of {0,1,0,1} = %g, want 0.5", meanRate)
	}

	// A second hand-computed case: {0.2, 0.8, 0.5} → mean 0.5, sample SD = sqrt(((-0.3)^2+(0.3)^2+0^2)/2)
	// = sqrt(0.18/2) = sqrt(0.09) = 0.3 exactly.
	rates2 := [][]float64{{0.2}, {0.8}, {0.5}}
	rows2, _, meanRate2 := ComputeSigmaR(rates2, []string{"frac"}, nil)
	if !approx(rows2[0].SigmaR, 0.3) {
		t.Errorf("σ_R of {0.2,0.8,0.5} = %g, want 0.3", rows2[0].SigmaR)
	}
	if !approx(meanRate2, 0.5) {
		t.Errorf("mean of {0.2,0.8,0.5} = %g, want 0.5", meanRate2)
	}
}

// TestSigmaRSampleSDNotPopulation: the SD must be the SAMPLE SD (denominator n-1), not the population
// SD (denominator n). For {0,1} the sample SD is sqrt(0.5/1)=sqrt(0.5)≈0.7071 while the population SD
// is sqrt(0.25)=0.5 — they differ, so this pins which estimator the instrument uses (the unbiased
// cross-launch one). Getting this wrong would systematically under-state σ_R and let a non-robust
// config slip the gate.
func TestSigmaRSampleSDNotPopulation(t *testing.T) {
	rates := [][]float64{{0}, {1}}
	rows, _, _ := ComputeSigmaR(rates, []string{"t"}, nil)
	wantSample := math.Sqrt(0.5)
	wantPopulation := 0.5
	if !approx(rows[0].SigmaR, wantSample) {
		t.Errorf("σ_R of {0,1} = %g, want the SAMPLE SD sqrt(0.5)=%g (NOT the population SD %g)",
			rows[0].SigmaR, wantSample, wantPopulation)
	}
}

// TestSigmaRSingleLaunchIsZero: R=1 launch has no measurable run-level variance — the sample SD is
// undefined (n-1=0), reported as 0. A degenerate single-launch run must read σ_R=0, NOT NaN/Inf (that
// would corrupt the aggregate + the report).
func TestSigmaRSingleLaunchIsZero(t *testing.T) {
	rates := [][]float64{{1, 0, 0.5}}
	rows, meanSigma, meanRate := ComputeSigmaR(rates, []string{"a", "b", "c"}, nil)
	for _, r := range rows {
		if r.SigmaR != 0 {
			t.Errorf("[%s] single-launch σ_R must be 0 (undefined SD), got %g", r.TaskID, r.SigmaR)
		}
		if math.IsNaN(r.SigmaR) || math.IsInf(r.SigmaR, 0) {
			t.Errorf("[%s] single-launch σ_R must be finite, got %g", r.TaskID, r.SigmaR)
		}
	}
	if meanSigma != 0 {
		t.Errorf("single-launch mean σ_R must be 0, got %g", meanSigma)
	}
	// the mean solve-rate is still the mean of the lone launch's rates: (1+0+0.5)/3 = 0.5.
	if !approx(meanRate, 0.5) {
		t.Errorf("single-launch mean solve-rate = %g, want 0.5", meanRate)
	}
}

// TestSigmaRPerTaskIsolation is the CRITICAL property: one task's variance must NOT leak into
// another's σ. Task A swings hard ({0,1,0,1}); task B is constant ({1,1,1,1}). B's σ_R must be EXACTLY
// 0 regardless of A's swing — proving the σ is computed per-column, never pooled. A pooled SD over the
// flattened 8-value vector would be non-zero and would (wrongly) attribute variance to B.
func TestSigmaRPerTaskIsolation(t *testing.T) {
	// columns: 0 = swing {0,1,0,1}; 1 = constant {1,1,1,1}.
	rates := [][]float64{
		{0, 1},
		{1, 1},
		{0, 1},
		{1, 1},
	}
	rows, meanSigma, _ := ComputeSigmaR(rates, []string{"swing", "const"}, nil)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	swingSD := math.Sqrt(1.0 / 3.0)
	if !approx(rows[0].SigmaR, swingSD) {
		t.Errorf("swing task σ_R = %g, want %g", rows[0].SigmaR, swingSD)
	}
	if !approx(rows[1].SigmaR, 0) {
		t.Errorf("constant task σ_R MUST be 0 (no leak from the swing task), got %g", rows[1].SigmaR)
	}
	// the mean σ_R is the mean of the two columns' σ, NOT a pooled SD over all 8 values.
	wantMeanSigma := (swingSD + 0) / 2
	if !approx(meanSigma, wantMeanSigma) {
		t.Errorf("mean σ_R = %g, want %g (mean of per-task σ, not pooled)", meanSigma, wantMeanSigma)
	}
	// SANITY: a pooled SD over the flattened 8 values {0,1,1,1,0,1,1,1} would be a DIFFERENT, non-zero
	// number — assert the mean σ_R is NOT that pooled value, so the test would FAIL a pooled
	// implementation (the win-reshuffle blind spot the brief warns about).
	flat := []float64{0, 1, 1, 1, 0, 1, 1, 1}
	pooled := sampleSD(flat)
	if approx(meanSigma, pooled) {
		t.Errorf("mean σ_R (%g) must NOT equal the pooled SD (%g) — a pooled impl would conflate "+
			"a win-reshuffle with a real per-task variance change", meanSigma, pooled)
	}
}

// TestSigmaRWinReshuffleInvariant is the sharpest per-task-isolation probe: a launch-set where the
// MARGINALS are identical to a robust set but WHICH task solved is reshuffled between launches. A
// pooled SD is blind to this (the flattened multiset is unchanged); the per-task σ_R correctly rises.
// This is exactly the failure mode the brief calls out ("a pooled SD can't distinguish a real
// variance collapse from a win-reshuffle").
func TestSigmaRWinReshuffleInvariant(t *testing.T) {
	// ROBUST set: each task solves in BOTH launches (no per-task swing). Per-task σ_R = 0.
	robust := [][]float64{
		{1, 0},
		{1, 0},
	}
	// RESHUFFLE set: same per-launch marginals (one solve per launch) BUT task A solves launch 0 and
	// task B solves launch 1 — each task now swings {1,0} / {0,1}. Per-task σ_R > 0.
	reshuffle := [][]float64{
		{1, 0},
		{0, 1},
	}
	_, robustMeanSigma, robustMeanRate := ComputeSigmaR(robust, []string{"a", "b"}, nil)
	_, reshufMeanSigma, reshufMeanRate := ComputeSigmaR(reshuffle, []string{"a", "b"}, nil)

	// the mean solve-rates are IDENTICAL (the marginals match) — the mean-guard would see no change.
	if !approx(robustMeanRate, reshufMeanRate) {
		t.Fatalf("the two sets must share a mean solve-rate (matched marginals): %g vs %g",
			robustMeanRate, reshufMeanRate)
	}
	// but the σ_R MUST differ: robust = 0, reshuffle > 0. A pooled SD would report them EQUAL.
	if !approx(robustMeanSigma, 0) {
		t.Errorf("robust set mean σ_R must be 0, got %g", robustMeanSigma)
	}
	if reshufMeanSigma <= robustMeanSigma {
		t.Errorf("reshuffle mean σ_R (%g) must EXCEED the robust set's (%g) — the per-task instrument "+
			"must see the win-reshuffle a pooled SD is blind to", reshufMeanSigma, robustMeanSigma)
	}
}

// TestComputeSigmaREmptyMatrix: an empty launch set returns zeros + nil rows, never a panic (the
// driver's degenerate-input guard).
func TestComputeSigmaREmptyMatrix(t *testing.T) {
	rows, meanSigma, meanRate := ComputeSigmaR(nil, nil, nil)
	if rows != nil || meanSigma != 0 || meanRate != 0 {
		t.Errorf("empty matrix must return (nil, 0, 0), got (%v, %g, %g)", rows, meanSigma, meanRate)
	}
}
