package stability

// B4 re-tune sweep (verbose, opt-in). The interaction diff found soft's Boltzmann branch policy pushes
// the fork-ratio peak_n to ~0.91 — ~0.09 from the n=1 cliff. This sweep answers the PROMOTE/BLOCK
// question: is 0.91 a SAFE bounded peak (durable, just less headroom) or a near-cliff risk that needs a
// re-tune (lower branch_propensity / higher temperature)? It walks the branch_propensity knob (the soft
// excitation actuator) over the full B4 combined config and reports peak_n at each, plus a long-horizon
// (2000t) crossing check. Offline TestBackend double, seed=7 (deterministic). Verbose only.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// b4FeaturesTuned is the FULL combined config with the soft excitation actuators (branch_propensity β,
// temperature τ) overridable, so the sweep can re-tune the one knob that drives n.
func b4FeaturesTuned(beta, tau float64) *config.HarnessConfig {
	feat := b4Combined()
	feat.Conscious.Activity.BranchPropensity = beta
	feat.Conscious.Activity.Temperature = tau
	feat.Validate()
	return feat
}

// TestB4SoftExcitationSweep walks branch_propensity (the soft fork-excitation actuator) over the full
// combined config and reports peak_n + the durable verdict at each. It shows whether 0.91 is the floor of
// a knob that can be tuned down for more headroom, or an irreducible property of soft. Verbose only.
func TestB4SoftExcitationSweep(t *testing.T) {
	t.Logf("B4 soft-excitation sweep over the FULL combined config (every-tick peak_n over 600t, seed=7)")
	t.Logf("default: branch_propensity=1.0 temperature=0.30")
	t.Logf("%-28s | peak_n | peak_U | min_mu | durable", "branch_propensity (β), τ=0.30")
	t.Logf("%s", "-------------------------------------------------------------------------")
	for _, beta := range []float64{0.25, 0.5, 0.75, 1.0} {
		r := measureB4("β="+ftoa(beta), b4FeaturesTuned(beta, 0.30))
		t.Logf("%-28s | %6.2f | %6.2f | %6.2f | %v", "β="+ftoa(beta), r.peakN, r.peakU, r.minMu, r.durableEveryTick())
	}
	t.Logf("temperature sweep (β=1.0 default): higher τ flattens the softmax toward uniform (less branch bias)")
	t.Logf("%-28s | peak_n | peak_U | min_mu | durable", "temperature (τ), β=1.0")
	t.Logf("%s", "-------------------------------------------------------------------------")
	for _, tau := range []float64{0.30, 0.5, 0.8, 1.2} {
		r := measureB4("τ="+ftoa(tau), b4FeaturesTuned(1.0, tau))
		t.Logf("%-28s | %6.2f | %6.2f | %6.2f | %v", "τ="+ftoa(tau), r.peakN, r.peakU, r.minMu, r.durableEveryTick())
	}
}

// TestB4LongHorizonCrossingCheck runs the full combined config over a LONG (2000-tick) horizon and asserts
// the fork-ratio n NEVER crosses the n=1 cliff at ANY tick — the durability question for a peak that sits
// at 0.91. If n stays strictly < 1 over 2000 ticks, 0.91 is a bounded steady peak (the regulator holds the
// regime), not a value drifting toward the cliff. Offline, seed=7. RUN WITH THOUGHT_WAKE_TRANSCRIPT=on.
func TestB4LongHorizonCrossingCheck(t *testing.T) {
	const ticks = 2000
	feat := b4Combined()
	pN, pU, mMu, mOut, rg, measured, kgNA := awakePeaks(feat, ticks)
	t.Logf("B4 combined LONG-horizon crossing check (%dt, seed=7): peak_n=%.3f peak_U=%.2f min_mu=%.2f max_out=%d regime=%v measured=%v kgNA=%v",
		ticks, pN, pU, mMu, mOut, rg.String(), measured, kgNA)
	if pN >= 1.0 {
		t.Fatalf("B4 combined CROSSED the n=1 cliff over %dt: peak_n=%.3f — supercritical, NOT durable", ticks, pN)
	}
	if !(pU <= 1.0 && mMu > 0.0 && mOut <= cognition.MaxParWidth) {
		t.Fatalf("B4 combined violated a condition over %dt: peak_U=%.2f min_mu=%.2f max_out=%d", ticks, pU, mMu, mOut)
	}
}

// ftoa formats a float for a row label (2 decimals, trimmed).
func ftoa(f float64) string {
	s := []byte{}
	whole := int(f)
	s = append(s, []byte(itoa(whole))...)
	frac := int((f-float64(whole))*100 + 0.5)
	s = append(s, '.')
	s = append(s, byte('0'+frac/10), byte('0'+frac%10))
	return string(s)
}
