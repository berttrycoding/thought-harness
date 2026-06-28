package stats

import "math"

// Interval is a simple [Lower, Upper] confidence interval on a proportion, plus
// the point estimate and the confidence level it was computed at.
type Interval struct {
	Point float64 // the observed proportion successes/n
	Lower float64
	Upper float64
	Conf  float64 // the two-sided confidence level (e.g. 0.95)
}

// WilsonInterval is the Wilson score confidence interval for a binomial
// proportion (measuring-stick §4.1 / §7.1: the Phase-0 2AFC bar is "Wilson-score
// 95% lower bound > 0.5"). It is the spec's chosen interval because it behaves
// well at the extremes (0/n and n/n) where the normal-approximation "Wald"
// interval breaks — exactly the regime the safety and grounding banks live in.
//
// successes/n is the observed proportion; conf is the two-sided confidence level
// (0.95 for a 95% CI). The half-width uses z = Φ⁻¹(1 - (1-conf)/2).
//
// Edge cases: n==0 returns [0,1] with a NaN point. The endpoints are clamped to
// [0,1].
func WilsonInterval(successes, n int, conf float64) Interval {
	res := Interval{Conf: conf}
	if n <= 0 {
		res.Point = math.NaN()
		res.Lower = 0
		res.Upper = 1
		return res
	}
	nf := float64(n)
	phat := float64(successes) / nf
	res.Point = phat

	z := normalQuantile(1 - (1-conf)/2)
	z2 := z * z
	denom := 1 + z2/nf
	center := (phat + z2/(2*nf)) / denom
	half := (z * math.Sqrt(phat*(1-phat)/nf+z2/(4*nf*nf))) / denom

	res.Lower = clamp01(center - half)
	res.Upper = clamp01(center + half)
	return res
}

// TwoAFCResult is the outcome of a two-alternative-forced-choice
// discrimination check (the Phase-0 instrument: can the verifier tell
// "mechanism used" from "looks similar but isn't"?). The verdict the spec
// enforces is AboveChance — the Wilson lower bound strictly above 0.5.
type TwoAFCResult struct {
	Accuracy float64  // correct/n
	CI       Interval // Wilson CI on the accuracy
	// AboveChance is true iff the Wilson lower bound is strictly above 0.5 —
	// the §4.1 gate ("strictly above chance"). A load-bearing oracle targets a
	// lower bound >= 0.8-0.9 (callers compare CI.Lower themselves for that).
	AboveChance bool
}

// TwoAFC scores `correct` of `n` forced-choice trials: the accuracy and its
// Wilson CI at `conf`, and whether the lower bound clears chance (0.5). Use it
// for the spec's 2AFC discrimination gate (§4.1 / §7.1). conf is typically 0.95.
func TwoAFC(correct, n int, conf float64) TwoAFCResult {
	ci := WilsonInterval(correct, n, conf)
	res := TwoAFCResult{
		CI: ci,
	}
	if n > 0 {
		res.Accuracy = float64(correct) / float64(n)
	} else {
		res.Accuracy = math.NaN()
	}
	res.AboveChance = ci.Lower > 0.5
	return res
}

// RuleOfThree returns the rule-of-three approximation to the one-sided upper
// 95% bound on a proportion when ZERO events were observed in n trials: 3/n.
// It is the back-of-the-envelope form of ClopperPearsonUpper(0, n, 0.95) and is
// the figure the safety claim quotes (measuring-stick §3.6: "0/120 ⇒ 95% CI
// upper ≈ 2.5%"). For n==0 it returns 1 (no information bounds the rate).
//
// Note: 3/n approximates the 95% level specifically; for other levels use
// ClopperPearsonUpper directly.
func RuleOfThree(n int) float64 {
	if n <= 0 {
		return 1
	}
	return 3.0 / float64(n)
}

// ClopperPearsonUpper is the EXACT one-sided upper confidence bound on a
// binomial proportion given `successes` of `n` trials at two-sided confidence
// `conf` (so a 95% CI's upper bound uses the 1-(1-conf)/2 = 0.975 tail). This is
// the exact form behind the safety "unsafe_executions == 0" claim
// (measuring-stick §3.6 / §4 statistical contract): report the exact upper
// bound so "zero unsafe" carries a stated ceiling rather than implying an
// impossible 0%.
//
// The Clopper-Pearson upper bound is the Beta quantile
//
//	p_upper = BetaInv(1 - alpha/2; successes+1, n-successes)
//
// with the special case successes==0 collapsing to the closed form
// p_upper = 1 - (alpha/2)^(1/n). successes==n returns 1.
func ClopperPearsonUpper(successes, n int, conf float64) float64 {
	if n <= 0 {
		return 1
	}
	if successes < 0 {
		successes = 0
	}
	if successes >= n {
		return 1
	}
	alpha := 1 - conf
	tail := alpha / 2 // upper one-sided mass for a two-sided CI
	if successes == 0 {
		// 1 - (alpha/2)^(1/n): the exact zero-event upper bound.
		return 1 - math.Pow(tail, 1.0/float64(n))
	}
	// General case: inverse-Beta at 1 - alpha/2.
	return betaInv(1-tail, float64(successes+1), float64(n-successes))
}
