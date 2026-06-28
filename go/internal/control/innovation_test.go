package control

import (
	"math"
	"testing"
)

// TestInnovate_ConfidentUngroundedCorrectedHard is the P1/P2 thinking: a CONFIDENT belief that has NOT
// been grounded carries HIGH variance, so a refuting observation produces a LARGE correction (large
// Kalman gain). This is "stop being confidently wrong" — reality overrides a confidently-stated-but-
// ungrounded guess hard.
func TestInnovate_ConfidentUngroundedCorrectedHard(t *testing.T) {
	// prior: confidently asserted (+0.9), but ungrounded -> high variance (1.0).
	// obs: reality REFUTES it (-1), at the firsthand-observation tier (precision 4.0).
	r := Innovate(0.9, 1.0, -1.0, TierPrecision(3), 9.0)
	if r.Gated {
		t.Fatalf("a refutation within ~3 sigma must NOT be gated; got gated")
	}
	// innovation nu = obs - priorMean = -1 - 0.9 = -1.9 (a big residual).
	if math.Abs(r.Innov-(-1.9)) > 1e-9 {
		t.Fatalf("innovation = %v, want -1.9", r.Innov)
	}
	// the correction must move the mean a LOT toward -1 (gain is large because var is high).
	moved := r.PriorMean - r.PostMean
	if moved < 1.0 {
		t.Fatalf("confident-ungrounded refutation must move the mean hard; moved only %v (post=%v)", moved, r.PostMean)
	}
	// the static penalty was a flat -0.45; the graded correction must be MUCH larger here.
	if moved <= 0.45 {
		t.Fatalf("graded correction (%v) must exceed the old static -0.45 for a high-variance belief", moved)
	}
}

// TestInnovate_GroundedBeliefBarelyMoves is the dual: a LOW-variance (already grounded) belief resists
// a spurious later contradiction — the gain is small, so reality barely moves it. (And the obs must be
// close enough not to be gated; a far obs against a tight prior is an association failure — see below.)
func TestInnovate_GroundedBeliefBarelyMoves(t *testing.T) {
	// prior: grounded, low variance (0.05); obs CONFIRMS (+1) at the gold tier.
	r := Innovate(0.8, 0.05, 1.0, TierPrecision(5), 9.0)
	if r.Gated {
		t.Fatalf("a confirming obs must not be gated; got gated")
	}
	moved := math.Abs(r.PostMean - r.PriorMean)
	if moved > 0.2 {
		t.Fatalf("a low-variance (grounded) belief must barely move; moved %v", moved)
	}
	// and a confirming observation shrinks the variance further (more certain).
	if r.PostVar >= r.PriorVar {
		t.Fatalf("a grounded confirming obs must shrink variance: post=%v prior=%v", r.PostVar, r.PriorVar)
	}
}

// TestInnovate_VarianceShrinksOnlyOnGrounding pins the §0 invariant at the math level: a NON-gated
// observation reduces variance (post < prior), and a gated one leaves it UNCHANGED (the obs was not
// folded in). control.Innovate is the ONLY var-reducer.
func TestInnovate_VarianceShrinksOnlyOnGrounding(t *testing.T) {
	assoc := Innovate(0.5, 1.0, 1.0, TierPrecision(4), 9.0)
	if assoc.PostVar >= assoc.PriorVar {
		t.Fatalf("an associated observation must shrink variance: post=%v prior=%v", assoc.PostVar, assoc.PriorVar)
	}
	// A wildly mismatched obs against a tight prior -> Mahalanobis-gated -> variance UNCHANGED.
	gated := Innovate(0.95, 0.01, -1.0, TierPrecision(5), 9.0)
	if !gated.Gated {
		t.Fatalf("a far obs (nu^2/S >> chi2) against a tight prior must be gated; got not gated")
	}
	if gated.PostVar != gated.PriorVar {
		t.Fatalf("a GATED obs must leave variance unchanged: post=%v prior=%v", gated.PostVar, gated.PriorVar)
	}
	if gated.PostMean != gated.PriorMean {
		t.Fatalf("a GATED obs must leave the mean unchanged: post=%v prior=%v", gated.PostMean, gated.PriorMean)
	}
}

// TestInnovate_MahalanobisGateRejectsMismatch is the data-association gate (the JCBB-lite of M1): a
// refuting observation too far from the prior+noise to be plausibly about this belief is REJECTED
// rather than folded in (don't corrupt the map with a mismatched measurement).
func TestInnovate_MahalanobisGateRejectsMismatch(t *testing.T) {
	// tight, certain prior at +1; a -1 obs at the gold tier: nu=-2, S=0.01+1/18 ~ 0.066, nu^2/S ~ 60 >> 9.
	r := Innovate(1.0, 0.01, -1.0, TierPrecision(5), 9.0)
	if !r.Gated {
		t.Fatalf("an obs > chi2Gate Mahalanobis distance must be gated; mahalanobis=%v", (r.Innov*r.Innov)/r.InnovVar)
	}
	// disabling the gate (chi2 <= 0) must let the same obs through (the opt-out path).
	ungated := Innovate(1.0, 0.01, -1.0, TierPrecision(5), 0)
	if ungated.Gated {
		t.Fatalf("chi2Gate<=0 must disable the gate; got gated")
	}
}

// TestTierPrecision_MonotoneInTrust pins the calibration the trust ladder supplies as R^-1: a more
// trustworthy source carries strictly more information (precision), so it corrects a belief harder.
func TestTierPrecision_MonotoneInTrust(t *testing.T) {
	prev := -1.0
	for ord := 0; ord < TierCount(); ord++ {
		p := TierPrecision(ord)
		if p <= prev {
			t.Fatalf("tier precision must be strictly increasing in trust: ord %d -> %v (prev %v)", ord, p, prev)
		}
		prev = p
	}
	// out-of-range clamps to the ends (defensive, not an expected path).
	if TierPrecision(-5) != TierPrecision(0) {
		t.Fatalf("negative ordinal must clamp to tier 0")
	}
	if TierPrecision(999) != TierPrecision(TierCount()-1) {
		t.Fatalf("over-range ordinal must clamp to the top tier")
	}
}

// TestInnovate_Deterministic guards Pattern-A purity: the same inputs always yield the same residual
// (no RNG, no clock).
func TestInnovate_Deterministic(t *testing.T) {
	a := Innovate(0.3, 0.7, -1.0, TierPrecision(2), 9.0)
	b := Innovate(0.3, 0.7, -1.0, TierPrecision(2), 9.0)
	if a != b {
		t.Fatalf("Innovate is not deterministic: %+v vs %+v", a, b)
	}
}
