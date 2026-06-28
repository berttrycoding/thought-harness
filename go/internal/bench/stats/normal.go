package stats

import "math"

// This file holds the shared normal-distribution primitives every other test in
// the package leans on (the standard-normal CDF and its inverse). They are
// implemented locally so the package stays on stdlib + math with no third-party
// numerics dependency.

// normalCDF is the standard-normal cumulative distribution function Φ(z),
// computed from the complementary error function (math.Erfc) for accuracy in
// both tails.
func normalCDF(z float64) float64 {
	return 0.5 * math.Erfc(-z/math.Sqrt2)
}

// normalSF is the standard-normal survival function 1-Φ(z) = Φ(-z), kept as a
// distinct name because the one-sided p-values read more clearly with it.
func normalSF(z float64) float64 {
	return 0.5 * math.Erfc(z/math.Sqrt2)
}

// normalPDF is the standard-normal density φ(z).
func normalPDF(z float64) float64 {
	return math.Exp(-0.5*z*z) / math.Sqrt(2*math.Pi)
}

// normalQuantile is the inverse standard-normal CDF (the probit function):
// returns z such that Φ(z) = p, for p in (0,1). It uses Peter Acklam's rational
// approximation (relative error < 1.15e-9 across the open interval), then a
// single Halley refinement step to push it to near machine precision — which
// matters because the BCa endpoints are sensitive to the quantile.
//
// Edge behaviour: p<=0 returns -Inf, p>=1 returns +Inf (so callers that pass a
// degenerate proportion get an honest infinity rather than a silent NaN).
func normalQuantile(p float64) float64 {
	if math.IsNaN(p) {
		return math.NaN()
	}
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}

	// Acklam's coefficients.
	a := [...]float64{
		-3.969683028665376e+01, 2.209460984245205e+02,
		-2.759285104469687e+02, 1.383577518672690e+02,
		-3.066479806614716e+01, 2.506628277459239e+00,
	}
	b := [...]float64{
		-5.447609879822406e+01, 1.615858368580409e+02,
		-1.556989798598866e+02, 6.680131188771972e+01,
		-1.328068155288572e+01,
	}
	c := [...]float64{
		-7.784894002430293e-03, -3.223964580411365e-01,
		-2.400758277161838e+00, -2.549732539343734e+00,
		4.374664141464968e+00, 2.938163982698783e+00,
	}
	d := [...]float64{
		7.784695709041462e-03, 3.224671290700398e-01,
		2.445134137142996e+00, 3.754408661907416e+00,
	}

	const pLow = 0.02425
	const pHigh = 1 - pLow

	var x float64
	switch {
	case p < pLow:
		q := math.Sqrt(-2 * math.Log(p))
		x = (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= pHigh:
		q := p - 0.5
		r := q * q
		x = (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		x = -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}

	// One Halley step: e = Φ(x)-p, u = e/φ(x), x -= u/(1 + x*u/2).
	e := normalCDF(x) - p
	u := e / normalPDF(x)
	x -= u / (1 + x*u/2)
	return x
}
