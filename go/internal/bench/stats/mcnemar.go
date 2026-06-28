package stats

import "math"

// McNemarResult is the verdict of a McNemar test on a paired-binary contrast
// (e.g. harness-vs-bare, gate-on-vs-off). The two arms agree on the concordant
// cells (both pass / both fail) which carry no information; all the signal is in
// the DISCORDANT cells b and c.
//
//	            arm-2 pass   arm-2 fail
//	arm-1 pass      a            b        <- b = arm-1 pass & arm-2 fail
//	arm-1 fail      c            d        <- c = arm-1 fail & arm-2 pass
//
// The spec convention (measuring-stick §3) is arm-1 = HARNESS / GATE-ON and
// arm-2 = BARE / GATE-OFF, so b = items the mechanism rescued, c = items it
// broke, and OddsRatio = b/c > 1 means the mechanism helped.
type McNemarResult struct {
	B         int     // discordant: arm-1 pass, arm-2 fail
	C         int     // discordant: arm-1 fail, arm-2 pass
	N         int     // discordant total b+c (the effective sample)
	PExact    float64 // exact two-sided p (binomial, n=b+c, p=0.5)
	PMid      float64 // mid-p two-sided (exact minus half the point mass; less conservative)
	OddsRatio float64 // b/c on the discordant split (+Inf if c==0 & b>0; NaN if both 0)
	PropB     float64 // b/(b+c): the share of discordant pairs favouring arm-1
}

// McNemar runs the exact McNemar test on the discordant counts (b, c).
//
// The exact test treats b as Binomial(n=b+c, p=0.5) under H0 (a discordant pair
// is equally likely to fall either way). The two-sided exact p is the total
// mass at least as extreme as observed, in both tails. The mid-p variant
// subtracts half the probability of the observed point — the standard
// less-conservative correction that keeps the test honest at small n (the
// measuring stick's Tier-A banks live near these small discordant counts).
//
// Both p-values are clamped to [0,1]. With no discordant pairs (b==c==0) there
// is no evidence either way: p=1, OddsRatio=NaN.
func McNemar(b, c int) McNemarResult {
	res := McNemarResult{B: b, C: c, N: b + c}

	switch {
	case b == 0 && c == 0:
		res.OddsRatio = math.NaN()
		res.PropB = math.NaN()
	case c == 0:
		res.OddsRatio = math.Inf(1)
		res.PropB = 1
	case b == 0:
		res.OddsRatio = 0
		res.PropB = 0
	default:
		res.OddsRatio = float64(b) / float64(c)
		res.PropB = float64(b) / float64(b+c)
	}

	n := b + c
	if n == 0 {
		res.PExact = 1
		res.PMid = 1
		return res
	}

	// Two-sided exact: sum binomial point masses <= the observed point mass.
	// With p=0.5 the distribution is symmetric, so the cleanest exact two-sided
	// rule is "sum every mass no larger than the observed one".
	obs := binomPMFHalf(b, n)
	var pExact, pPoint float64
	for k := 0; k <= n; k++ {
		m := binomPMFHalf(k, n)
		if k == b {
			pPoint += m
		}
		// Tolerance guards float wobble on the symmetric twin (e.g. k and n-k).
		if m <= obs*(1+1e-9) {
			pExact += m
		}
	}
	res.PExact = clamp01(pExact)
	res.PMid = clamp01(pExact - 0.5*pPoint)
	return res
}

// binomPMFHalf is the Binomial(n, 0.5) probability mass at k, i.e.
// C(n,k) / 2^n, computed in log-space via lgamma to stay exact for large n.
func binomPMFHalf(k, n int) float64 {
	if k < 0 || k > n {
		return 0
	}
	logC, _ := math.Lgamma(float64(n + 1))
	lk, _ := math.Lgamma(float64(k + 1))
	lnk, _ := math.Lgamma(float64(n - k + 1))
	logProb := (logC - lk - lnk) - float64(n)*math.Ln2
	return math.Exp(logProb)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
