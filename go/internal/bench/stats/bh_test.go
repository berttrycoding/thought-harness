package stats

import (
	"math"
	"testing"
)

func TestBenjaminiHochberg(t *testing.T) {
	// Canonical Benjamini-Hochberg (1995) worked example: 15 p-values, q=0.05.
	// The step-up rejects the first 4 (largest k with p_(k) <= (k/m)*q is k=4).
	pvals := []float64{
		0.0001, 0.0004, 0.0019, 0.0095, 0.0201, 0.0278, 0.0298, 0.0344,
		0.0459, 0.3240, 0.4262, 0.5719, 0.6528, 0.7590, 1.0,
	}
	r := BenjaminiHochberg(pvals, 0.05)
	if r.NumReject != 4 {
		t.Fatalf("NumReject = %d, want 4", r.NumReject)
	}
	// The first 4 (already in ascending order here) are rejected, rest not.
	for i := 0; i < 15; i++ {
		want := i < 4
		if r.Reject[i] != want {
			t.Errorf("Reject[%d] = %v, want %v", i, r.Reject[i], want)
		}
	}
	// Adjusted q-values are monotone non-decreasing and clamped to [0,1].
	wantAdj := []float64{
		0.0015, 0.0030, 0.0095, 0.03562, 0.0603, 0.06386, 0.06386, 0.0645,
		0.0765, 0.486, 0.58118, 0.71487, 0.75323, 0.81321, 1.0,
	}
	for i := range wantAdj {
		if !approx(r.Adjusted[i], wantAdj[i], 5e-4) {
			t.Errorf("Adjusted[%d] = %.5f, want ~%.5f", i, r.Adjusted[i], wantAdj[i])
		}
		if i > 0 && r.Adjusted[i] < r.Adjusted[i-1]-1e-12 {
			t.Errorf("adjusted p not monotone at %d: %.5f < %.5f", i, r.Adjusted[i], r.Adjusted[i-1])
		}
	}
}

func TestBenjaminiHochbergOrderPreserved(t *testing.T) {
	// Results must come back in INPUT order even when p-values are shuffled.
	pvals := []float64{0.5719, 0.0001, 0.7590, 0.0004} // mixed order
	r := BenjaminiHochberg(pvals, 0.05)
	// Only the two tiny p's (indices 1 and 3) should be candidates; with m=4,
	// crit for rank2 = 0.025, p_(2)=0.0004 <= 0.025 -> reject ranks 1,2.
	if !r.Reject[1] || !r.Reject[3] {
		t.Errorf("the two small p's should reject; reject=%v", r.Reject)
	}
	if r.Reject[0] || r.Reject[2] {
		t.Errorf("the two large p's should not reject; reject=%v", r.Reject)
	}
}

func TestBenjaminiHochbergEmpty(t *testing.T) {
	r := BenjaminiHochberg(nil, 0.05)
	if r.NumReject != 0 || len(r.Adjusted) != 0 {
		t.Errorf("empty input should give empty result, got %+v", r)
	}
}

func TestBenjaminiHochbergNaN(t *testing.T) {
	// A NaN p-value is treated as 1 and never rejected.
	pvals := []float64{0.001, math.NaN(), 0.002}
	r := BenjaminiHochberg(pvals, 0.05)
	if r.Reject[1] {
		t.Errorf("NaN p should never be rejected")
	}
	if r.Adjusted[1] != 1 {
		t.Errorf("NaN p should adjust to 1, got %v", r.Adjusted[1])
	}
}
