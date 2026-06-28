package regulator

import (
	"math"
	"testing"
)

// TestRegulatorSuppressesForkStorm is the X.3 gate: under an adversarial fork storm the homeostatic
// regulator pulls the branching ratio n back below the n<1 subcritical cliff.
//
// It is a CLOSED-LOOP test. First an unmodelled burst drives n supercritical (a cascade that, left
// alone, diverges). Then a θ-gated plant runs — excitation falls as θ rises (higher admission
// threshold ⇒ fewer specialists fire ⇒ fewer forks) — and the regulator's negative feedback
// (θ ← θ + K·(λ̂ − λ*)) raises θ until the storm is suppressed: n returns subcritical and intensity
// settles near λ*. Fully deterministic (no clock, no RNG).
func TestRegulatorSuppressesForkStorm(t *testing.T) {
	r := New(nil, nil) // default config: LamStar=1.0, GainK=0.4, θ∈[0.05,0.95]

	var peakN float64

	// Phase 1 — an adversarial burst the regulator hasn't caught up to yet: many forks per tick.
	for tick := 0; tick < 5; tick++ {
		o := DefaultUpdateOpts()
		o.Fired = 16
		o.Forked = 8 // 8 offspring/thought — wildly supercritical
		o.BranchesLive = 8
		r.Update(o)
		if r.N() > peakN {
			peakN = r.N()
		}
	}
	if peakN <= 1.0 {
		t.Fatalf("the burst should have driven n supercritical (>1); peak n=%.2f", peakN)
	}

	// Phase 2 — the θ-gated plant: excitation is throttled by the threshold the regulator controls.
	// Offspring follow the natural "each thought beyond the first may fork" model (forks = fired-1), so
	// when the regulator drives intensity down to λ*≈1 the cascade collapses to ~0 offspring.
	const stormRate = 10.0
	for tick := 0; tick < 120; tick++ {
		fired := int(math.Round(stormRate * (1 - r.Theta()))) // higher θ ⇒ fewer fire
		if fired < 0 {
			fired = 0
		}
		forked := fired - 1
		if forked < 0 {
			forked = 0
		}
		o := DefaultUpdateOpts()
		o.Fired = fired
		o.Forked = forked
		o.BranchesLive = forked
		r.Update(o)
	}

	// the regulator responded — θ climbed well above the floor to throttle the storm.
	if r.Theta() < 0.5 {
		t.Fatalf("regulator did not raise θ against the storm; θ=%.2f", r.Theta())
	}
	// and it pulled the cascade back into the stable regime: n subcritical, intensity near λ*.
	if r.N() >= 1.0 {
		t.Fatalf("regulator failed to pull n back below the subcritical cliff; n=%.2f (peak was %.2f)", r.N(), peakN)
	}
	if math.IsInf(r.LamBar(), 1) {
		t.Fatalf("λ̄ diverged — n did not return subcritical (n=%.2f)", r.N())
	}
	if math.Abs(r.LamHat()-r.cfg.LamStar) > 0.7 {
		t.Fatalf("intensity not controlled toward λ*=%.1f; λ̂=%.2f", r.cfg.LamStar, r.LamHat())
	}
	t.Logf("fork storm suppressed: peak n=%.2f -> final n=%.2f, θ=%.2f, λ̂=%.2f", peakN, r.N(), r.Theta(), r.LamHat())
}
