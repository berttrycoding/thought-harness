package stats

import (
	"math"
	"math/rand"
	"sort"
)

// DefaultBootstrapB is the spec default replicate count (measuring-stick §4.3:
// "bias-corrected accelerated, 10,000 resamples").
const DefaultBootstrapB = 10000

// BCaResult is a bias-corrected accelerated bootstrap confidence interval for a
// paired statistic of the per-pair differences (mean diff, median diff, …).
type BCaResult struct {
	Theta float64 // the statistic on the observed sample (the point estimate)
	Lower float64 // lower CI endpoint at level alpha/2
	Upper float64 // upper CI endpoint at level 1-alpha/2
	Alpha float64 // the two-sided alpha used (0.05 => a 95% CI)
	B     int     // number of bootstrap replicates actually run
	Z0    float64 // bias-correction term
	A     float64 // acceleration term (from the jackknife)
	// Degenerate is set when the BCa correction could not be formed (all
	// replicates identical, or every jackknife value identical). The interval
	// then falls back to the plain percentile bootstrap and this flag warns the
	// caller that z0/a are not meaningful.
	Degenerate bool
}

// BootstrapBCa computes the BCa confidence interval for statFn evaluated on the
// paired differences `diffs`, resampling the PAIRS (so the pairing the spec
// requires is respected — each resample draws whole differences with
// replacement). B is the replicate count (use DefaultBootstrapB / 10000 when 0
// is passed), `seed` makes the resampling reproducible, and `alpha` is the
// two-sided miss probability (0.05 => 95% CI).
//
// statFn receives a resampled slice of differences and returns the scalar of
// interest — pass MeanStat or MedianStat for the common "mean/median paired
// diff" the spec calls for, or any custom paired statistic.
//
// The BCa machinery (Efron):
//   - theta_hat = statFn(diffs);
//   - run B resamples, theta*_b = statFn(resample_b);
//   - z0 = Φ⁻¹( #{theta*_b < theta_hat} / B )  (bias correction);
//   - a = Σ(θ̄_(.) − θ_(i))³ / [6 (Σ(θ̄_(.) − θ_(i))²)^{3/2}]  via leave-one-out
//     jackknife (acceleration);
//   - the percentile rank for each endpoint is adjusted by z0 and a, then read
//     off the sorted replicate distribution.
//
// Determinism: with a fixed `seed` the resampling is bit-reproducible, so two
// runs at the same seed return identical endpoints (the property the test
// asserts).
func BootstrapBCa(diffs []float64, statFn func([]float64) float64, B int, seed int64, alpha float64) BCaResult {
	if B <= 0 {
		B = DefaultBootstrapB
	}
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.05
	}
	n := len(diffs)
	res := BCaResult{Alpha: alpha, B: B}
	if n == 0 {
		res.Theta = math.NaN()
		res.Lower = math.NaN()
		res.Upper = math.NaN()
		return res
	}
	res.Theta = statFn(diffs)
	if n == 1 {
		// A single pair carries no spread; the interval is the point itself.
		res.Lower = res.Theta
		res.Upper = res.Theta
		res.Degenerate = true
		return res
	}

	rng := rand.New(rand.NewSource(seed))

	// Bootstrap replicates (paired resample = draw whole diffs with replacement).
	thetas := make([]float64, B)
	resample := make([]float64, n)
	below := 0
	for b := 0; b < B; b++ {
		for i := 0; i < n; i++ {
			resample[i] = diffs[rng.Intn(n)]
		}
		t := statFn(resample)
		thetas[b] = t
		if t < res.Theta {
			below++
		}
	}
	sort.Float64s(thetas)

	// Bias correction z0 from the fraction of replicates below the point estimate.
	prop := float64(below) / float64(B)
	z0 := normalQuantile(prop)
	res.Z0 = z0

	// Acceleration a from the leave-one-out jackknife of the statistic.
	jack := make([]float64, n)
	loo := make([]float64, n-1)
	var jackMean float64
	for i := 0; i < n; i++ {
		// fill loo with every diff except i
		idx := 0
		for j := 0; j < n; j++ {
			if j == i {
				continue
			}
			loo[idx] = diffs[j]
			idx++
		}
		jack[i] = statFn(loo)
		jackMean += jack[i]
	}
	jackMean /= float64(n)
	var num, den float64
	for i := 0; i < n; i++ {
		d := jackMean - jack[i]
		num += d * d * d
		den += d * d
	}
	var a float64
	if den > 0 {
		a = num / (6 * math.Pow(den, 1.5))
	}
	res.A = a

	// If z0 is not finite (all replicates on one side) the BCa correction is
	// undefined — fall back to the plain percentile interval.
	if math.IsInf(z0, 0) || math.IsNaN(z0) {
		res.Degenerate = true
		res.Z0 = 0
		res.A = 0
		res.Lower = percentile(thetas, alpha/2)
		res.Upper = percentile(thetas, 1-alpha/2)
		return res
	}

	zLo := normalQuantile(alpha / 2)
	zHi := normalQuantile(1 - alpha/2)
	alphaLo := bcaAdjust(z0, a, zLo)
	alphaHi := bcaAdjust(z0, a, zHi)

	res.Lower = percentile(thetas, alphaLo)
	res.Upper = percentile(thetas, alphaHi)
	return res
}

// bcaAdjust maps a target normal quantile z (= zLo or zHi) to the adjusted
// percentile alpha' = Φ( z0 + (z0 + z) / (1 - a(z0 + z)) ).
func bcaAdjust(z0, a, z float64) float64 {
	num := z0 + z
	denom := 1 - a*num
	if denom == 0 {
		denom = 1e-12
	}
	return clamp01(normalCDF(z0 + num/denom))
}

// percentile reads the p-quantile (p in [0,1]) from an ASCENDING-sorted slice
// using linear interpolation between order statistics (the same convention as
// numpy's default and R's type-7) so the BCa endpoints are smooth in B.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return math.NaN()
	}
	if n == 1 {
		return sorted[0]
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[n-1]
	}
	h := p * float64(n-1)
	lo := int(math.Floor(h))
	frac := h - float64(lo)
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

// MeanStat is the mean of the differences — the most common BCa statistic for
// the spec's "mean paired diff".
func MeanStat(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// MedianStat is the median of the differences — the spec's robust alternative
// (Hodges-Lehmann-style "median diff") for skewed cost/score deltas.
func MedianStat(xs []float64) float64 {
	n := len(xs)
	if n == 0 {
		return math.NaN()
	}
	cp := make([]float64, n)
	copy(cp, xs)
	sort.Float64s(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return 0.5 * (cp[n/2-1] + cp[n/2])
}
