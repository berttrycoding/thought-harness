package stats

import "math"

// This file holds the regularized incomplete beta function I_x(a,b) and its
// inverse, used by the exact Clopper-Pearson upper bound. Implemented locally
// (Lentz's continued fraction + bisection inversion) to keep the package on
// stdlib + math with no numerics dependency. Accuracy is well within what the
// benchmark reporting needs (< 1e-10 absolute on the CDF; the inverse is driven
// to 1e-12 or 100 bisection steps).

// betaCDF is the regularized incomplete beta function I_x(a,b) = B(x;a,b)/B(a,b),
// the CDF of the Beta(a,b) distribution at x in [0,1].
func betaCDF(x, a, b float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	// log B(a,b) via lgamma.
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	front := math.Exp(math.Log(x)*a + math.Log(1-x)*b + lab - la - lb)
	// Use the continued fraction, swapping tails for fast convergence.
	if x < (a+1)/(a+b+2) {
		return front * betaContinuedFraction(x, a, b) / a
	}
	return 1 - front*betaContinuedFraction(1-x, b, a)/b
}

// betaContinuedFraction evaluates the continued fraction for the incomplete beta
// function via the modified Lentz algorithm.
func betaContinuedFraction(x, a, b float64) float64 {
	const (
		maxIter = 300
		tiny    = 1e-30
		eps     = 1e-14
	)
	qab := a + b
	qap := a + 1
	qam := a - 1
	c := 1.0
	d := 1 - qab*x/qap
	if math.Abs(d) < tiny {
		d = tiny
	}
	d = 1 / d
	h := d
	for m := 1; m <= maxIter; m++ {
		mf := float64(m)
		m2 := 2 * mf
		// even step
		aa := mf * (b - mf) * x / ((qam + m2) * (a + m2))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		h *= d * c
		// odd step
		aa = -(a + mf) * (qab + mf) * x / ((a + m2) * (qap + m2))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return h
}

// betaInv inverts the regularized incomplete beta: returns x in [0,1] such that
// I_x(a,b) = p. Bisection on the monotone CDF — slower than a Newton step but
// unconditionally robust (the Clopper-Pearson upper bound is called rarely, so
// robustness beats speed here).
func betaInv(p, a, b float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 200; i++ {
		mid := 0.5 * (lo + hi)
		if betaCDF(mid, a, b) < p {
			lo = mid
		} else {
			hi = mid
		}
		if hi-lo < 1e-13 {
			break
		}
	}
	return 0.5 * (lo + hi)
}
