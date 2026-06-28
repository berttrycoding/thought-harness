package calibrate

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
)

// res builds a non-gated residual carrying the prediction (priorMean) and the grounded outcome (obs)
// for a calibration outcome — the only two fields the calibrator reads off the M1 residual.
func res(priorMean, obs float64) control.Residual {
	return control.Residual{PriorMean: priorMean, Obs: obs}
}

func newCal(min int) *Calibrator {
	c := New(Config{Enabled: true, MinSamples: min}, nil)
	return c
}

// TestLearnedPrecision_HonestFallbackUnderSampled is the regulator/gain.go honesty discipline (the §0
// of M9): below the identification gate the LEARNED precision is the FIXED PRIOR exactly — a noisy
// half-dozen samples must NEVER swing the measurement update. This is what keeps a wrong "measured"
// reliability from being worse than the honest prior.
func TestLearnedPrecision_HonestFallbackUnderSampled(t *testing.T) {
	c := newCal(8)
	tier := 3 // FirsthandObservation
	prior := control.TierPrecision(tier)
	// fold in only 4 outcomes (below the gate of 8) — even all-refuting.
	for i := 0; i < 4; i++ {
		c.Observe(tier, res(0.9, -1)) // confident prediction, refuted
	}
	if got := c.LearnedPrecision(tier); math.Abs(got-prior) > 1e-9 {
		t.Fatalf("under-sampled tier must fall back to the fixed prior %v; got %v", prior, got)
	}
	if _, measured := c.reliabilityOf(tier); measured {
		t.Fatalf("a tier below the identification gate must report measured=false")
	}
}

// TestLearnedPrecision_LowReliabilityDownWeights is the same-model-ceiling lever (the headline G9
// cognition): a source whose observations SYSTEMATICALLY catch the prediction being wrong (the model
// confidently asserts, reality refutes) has a LOW hit-rate, so once identified its learned precision is
// DOWN-WEIGHTED below the fixed prior — the system DISCOVERS it is overconfident against that source and
// trusts the prior less. This is "learn R", and it is exactly what a fixed prior cannot do.
func TestLearnedPrecision_LowReliabilityDownWeights(t *testing.T) {
	c := newCal(8)
	tier := 3
	prior := control.TierPrecision(tier)
	// 10 confident-but-refuted outcomes: hit-rate 0 -> reliability floored.
	for i := 0; i < 10; i++ {
		c.Observe(tier, res(0.9, -1))
	}
	learned := c.LearnedPrecision(tier)
	if learned >= prior {
		t.Fatalf("a systematically-refuted source must be DOWN-weighted below the prior; prior=%v learned=%v", prior, learned)
	}
	if rel, measured := c.reliabilityOf(tier); !measured || rel > 0.5 {
		t.Fatalf("a 0-hit-rate identified tier must report a low reliability; rel=%v measured=%v", rel, measured)
	}
	// and it surfaces as overconfidence (confident assertions refuted).
	if oc, measured := c.Overconfidence(tier); !measured || oc < 0.99 {
		t.Fatalf("10/10 confident refutes must read as overconfidence ~1.0; oc=%v measured=%v", oc, measured)
	}
}

// TestLearnedPrecision_HighReliabilityBoosts is the dual: a source whose observations reliably CONFIRM
// the prediction (high hit-rate) is BOOSTED above the prior — its observations are diagnostic, so the
// measurement update should weight them more. A perfectly-calibrated 50/50 source keeps the prior
// exactly (the centring point).
func TestLearnedPrecision_HighReliabilityBoosts(t *testing.T) {
	c := newCal(8)
	tier := 1 // Web
	prior := control.TierPrecision(tier)
	for i := 0; i < 12; i++ {
		c.Observe(tier, res(0.8, 1)) // confident prediction, confirmed
	}
	if learned := c.LearnedPrecision(tier); learned <= prior {
		t.Fatalf("a reliably-confirming source must be BOOSTED above the prior; prior=%v learned=%v", prior, learned)
	}

	// the 50/50 centring point: a separate, evenly-mixed tier keeps the prior weight.
	cc := newCal(8)
	mixTier := 2
	mixPrior := control.TierPrecision(mixTier)
	for i := 0; i < 6; i++ {
		cc.Observe(mixTier, res(0.7, 1))  // confirmed
		cc.Observe(mixTier, res(0.7, -1)) // refuted
	}
	if learned := cc.LearnedPrecision(mixTier); math.Abs(learned-mixPrior) > 1e-9 {
		t.Fatalf("a 50/50 source must keep the prior weight; prior=%v learned=%v", mixPrior, learned)
	}
}

// TestObserve_GatedNotEvidence asserts a DATA-ASSOCIATION-FAILED residual (a mismatched observation the
// M1 Mahalanobis gate rejected) is NOT folded into calibration — a rejected obs is evidence the obs was
// about a DIFFERENT belief, not evidence about this source's reliability. Folding it in would corrupt
// the learned R with off-target measurements.
func TestObserve_GatedNotEvidence(t *testing.T) {
	c := newCal(8)
	tier := 3
	gated := control.Residual{PriorMean: 0.9, Obs: -1, Gated: true}
	for i := 0; i < 10; i++ {
		c.Observe(tier, gated)
	}
	if st := c.byTier[tier]; st != nil && st.Samples != 0 {
		t.Fatalf("gated residuals must not accumulate as calibration samples; got %d", st.Samples)
	}
	if got := c.LearnedPrecision(tier); math.Abs(got-control.TierPrecision(tier)) > 1e-9 {
		t.Fatalf("a tier with only gated obs stays at the prior; got %v", got)
	}
}

// TestDisabled_ByteIdentical is the default-OFF guarantee at the unit level: a disabled calibrator
// accumulates nothing, learns nothing, and LearnedPrecision returns the fixed prior EXACTLY — so the M1
// estimator's behaviour is byte-identical to the no-M9 path.
func TestDisabled_ByteIdentical(t *testing.T) {
	c := New(Config{Enabled: false}, nil)
	for i := 0; i < 20; i++ {
		c.Observe(3, res(0.9, -1))
	}
	for tier := 0; tier < control.TierCount(); tier++ {
		if got := c.LearnedPrecision(tier); math.Abs(got-control.TierPrecision(tier)) > 1e-9 {
			t.Fatalf("disabled calibrator must return the fixed prior for tier %d; got %v", tier, got)
		}
	}
	if id, _, _ := c.Vitals(); id != 0 {
		t.Fatalf("disabled calibrator vitals must be zero; identified=%d", id)
	}
}

// TestDeterminism pins that the same outcome sequence yields the same learned precision (no RNG, no
// clock) — calibration is pure CONTROL.
func TestDeterminism(t *testing.T) {
	run := func() float64 {
		c := newCal(8)
		seq := []control.Residual{res(0.9, 1), res(0.8, -1), res(0.6, 1), res(0.95, -1),
			res(0.7, 1), res(0.5, 1), res(0.9, -1), res(0.85, 1), res(0.6, 1), res(0.9, -1)}
		for _, r := range seq {
			c.Observe(3, r)
		}
		return c.LearnedPrecision(3)
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("calibration must be deterministic; %v != %v", a, b)
	}
}
