package stats

import (
	"math"
	"testing"
)

func TestICC21(t *testing.T) {
	// Canonical worked example: Shrout & Fleiss (1979) Table 1 — 6 subjects, 4
	// judges. The published ICC(2,1) for this matrix is 0.290.
	matrix := [][]float64{
		{9, 2, 5, 8},
		{6, 1, 3, 2},
		{8, 4, 6, 8},
		{7, 1, 2, 6},
		{10, 5, 6, 9},
		{6, 2, 4, 7},
	}
	r := ICC21(matrix)
	if !approx(r.ICC, 0.28976377952755905, 1e-9) {
		t.Errorf("ICC(2,1) = %.10f, want 0.2897638 (Shrout & Fleiss 1979)", r.ICC)
	}
	if r.N != 6 || r.K != 4 {
		t.Errorf("dims = (%d,%d), want (6,4)", r.N, r.K)
	}
}

func TestICC21PerfectAgreement(t *testing.T) {
	// Identical raters with subject variance -> ICC = 1.
	matrix := [][]float64{
		{1, 1, 1},
		{5, 5, 5},
		{9, 9, 9},
		{3, 3, 3},
	}
	r := ICC21(matrix)
	if !approx(r.ICC, 1.0, 1e-9) {
		t.Errorf("perfect agreement ICC = %v, want 1.0", r.ICC)
	}
}

func TestICC21Degenerate(t *testing.T) {
	// Too few rows / columns -> NaN.
	if r := ICC21([][]float64{{1, 2}}); !math.IsNaN(r.ICC) {
		t.Errorf("single row should give NaN, got %v", r.ICC)
	}
	if r := ICC21([][]float64{{1}, {2}}); !math.IsNaN(r.ICC) {
		t.Errorf("single column should give NaN, got %v", r.ICC)
	}
}
