package stats

import (
	"math"
	"testing"
)

func TestBootstrapBCaReproducible(t *testing.T) {
	// Same seed -> bit-identical endpoints (the determinism property the spec
	// requires of every randomized routine).
	diffs := []float64{0.12, -0.03, 0.20, 0.08, -0.10, 0.15, 0.05, 0.22, -0.01, 0.18,
		0.09, 0.13, -0.04, 0.07, 0.16, 0.11, -0.02, 0.19, 0.06, 0.14}
	r1 := BootstrapBCa(diffs, MeanStat, 2000, 42, 0.05)
	r2 := BootstrapBCa(diffs, MeanStat, 2000, 42, 0.05)
	if r1.Lower != r2.Lower || r1.Upper != r2.Upper {
		t.Errorf("not reproducible under fixed seed: r1=[%v,%v] r2=[%v,%v]",
			r1.Lower, r1.Upper, r2.Lower, r2.Upper)
	}
	// A different seed should (almost surely) move the endpoints.
	r3 := BootstrapBCa(diffs, MeanStat, 2000, 7, 0.05)
	if r1.Lower == r3.Lower && r1.Upper == r3.Upper {
		t.Errorf("different seed produced identical endpoints (suspicious): %v", r1)
	}
}

func TestBootstrapBCaCovers(t *testing.T) {
	// The point estimate (mean) and the interval ordering must hold; the CI must
	// bracket theta and exclude 0 for a clearly-positive sample.
	diffs := []float64{0.12, 0.03, 0.20, 0.08, 0.10, 0.15, 0.05, 0.22, 0.04, 0.18,
		0.09, 0.13, 0.04, 0.07, 0.16, 0.11, 0.02, 0.19, 0.06, 0.14}
	r := BootstrapBCa(diffs, MeanStat, DefaultBootstrapB, 1, 0.05)
	if !approx(r.Theta, MeanStat(diffs), 1e-12) {
		t.Errorf("Theta = %v, want mean %v", r.Theta, MeanStat(diffs))
	}
	if !(r.Lower < r.Theta && r.Theta < r.Upper) {
		t.Errorf("CI [%v, %v] should bracket theta %v", r.Lower, r.Upper, r.Theta)
	}
	if r.Lower <= 0 {
		t.Errorf("clearly-positive sample: BCa lower %v should exceed 0", r.Lower)
	}
	if r.B != DefaultBootstrapB {
		t.Errorf("B = %d, want default %d", r.B, DefaultBootstrapB)
	}
}

func TestBootstrapBCaKnownNormal(t *testing.T) {
	// On a symmetric, well-behaved sample the BCa 95% mean interval should be
	// close to the textbook t/normal interval mean ± 1.96·SE. We check the
	// midpoint is near the mean and the half-width is in the right ballpark
	// (BCa coincides with the percentile interval when bias & accel ~ 0).
	xs := []float64{-2, -1, 0, 1, 2, -2, -1, 0, 1, 2, -2, -1, 0, 1, 2, -2, -1, 0, 1, 2}
	r := BootstrapBCa(xs, MeanStat, DefaultBootstrapB, 123, 0.05)
	if !approx(r.Theta, 0, 1e-9) {
		t.Errorf("mean of symmetric sample should be 0, got %v", r.Theta)
	}
	mid := 0.5 * (r.Lower + r.Upper)
	if !approx(mid, 0, 0.15) {
		t.Errorf("CI midpoint %v should be near the mean 0", mid)
	}
	// Analytic SE of the mean = sd/sqrt(n). sd of this sample:
	mean := MeanStat(xs)
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	sd := math.Sqrt(ss / float64(len(xs)-1))
	se := sd / math.Sqrt(float64(len(xs)))
	wantHalf := 1.959963984540054 * se
	gotHalf := 0.5 * (r.Upper - r.Lower)
	if math.Abs(gotHalf-wantHalf) > 0.25*wantHalf {
		t.Errorf("BCa half-width %v not within 25%% of analytic %v", gotHalf, wantHalf)
	}
}

func TestBootstrapStats(t *testing.T) {
	if !approx(MeanStat([]float64{1, 2, 3, 4}), 2.5, 1e-12) {
		t.Errorf("MeanStat wrong")
	}
	if !approx(MedianStat([]float64{1, 2, 3, 4}), 2.5, 1e-12) {
		t.Errorf("MedianStat even wrong")
	}
	if !approx(MedianStat([]float64{1, 2, 3, 4, 5}), 3, 1e-12) {
		t.Errorf("MedianStat odd wrong")
	}
}

func TestBootstrapDegenerate(t *testing.T) {
	r := BootstrapBCa([]float64{0.5}, MeanStat, 1000, 1, 0.05)
	if !r.Degenerate || r.Lower != 0.5 || r.Upper != 0.5 {
		t.Errorf("single-pair sample should be a degenerate point interval, got %+v", r)
	}
	r = BootstrapBCa(nil, MeanStat, 1000, 1, 0.05)
	if !math.IsNaN(r.Theta) {
		t.Errorf("empty sample should give NaN theta, got %v", r.Theta)
	}
}
