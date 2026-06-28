package regulator

import (
	"math"
	"testing"
)

// drivePlant runs the closed loop for n ticks against a synthetic linear plant: the tick's Fired
// responds to the PREVIOUS tick's theta as round(c - g*theta) (the lag the estimator models). Baseline
// 0, forks 0 — pure excitation so lam_hat tracks the plant.
func drivePlant(r *Regulator, c, g float64, n int) {
	theta := r.Theta()
	for i := 0; i < n; i++ {
		fired := int(math.Round(c - g*theta))
		if fired < 0 {
			fired = 0
		}
		o := DefaultUpdateOpts()
		o.Fired = fired
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
		theta = r.Theta()
	}
}

// TestGainEstimateIdentifiablePlant: a plant with a real, steep response to theta is IDENTIFIED — the
// estimate is measured (not the prior) and lands in a plausible band around the EMA-attenuated true
// gain (alpha*g = 0.35*20 = 7), never at the 0.5 prior.
func TestGainEstimateIdentifiablePlant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LamStar = 30.0 // equilibrium inside the theta band so the loop genuinely moves theta
	r := New(nil, &cfg)
	drivePlant(r, 40.0, 20.0, 80)
	g, measured := r.GainEstimate()
	if !measured {
		t.Fatalf("a steep identifiable plant must yield a MEASURED gain, got fallback g=%v", g)
	}
	if g < 1.0 || g > 10.0 {
		t.Fatalf("measured gain %v outside the plausible band [1,10] (alpha-attenuated true gain ~7)", g)
	}
	if g == cfg.GEst {
		t.Fatalf("measured gain exactly equals the prior %v — suspicious fallback", cfg.GEst)
	}
}

// TestGainEstimateFallsBackWhenPinned: theta pinned (ThetaMin==ThetaMax) means var(dtheta)=0 — the
// plant is unidentifiable and the estimator must fall back HONESTLY to the configured prior.
func TestGainEstimateFallsBackWhenPinned(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ThetaMin, cfg.ThetaMax = 0.3, 0.3
	r := New(nil, &cfg)
	drivePlant(r, 40.0, 20.0, 80)
	g, measured := r.GainEstimate()
	if measured {
		t.Fatalf("a pinned theta cannot identify the plant; expected the prior, got measured g=%v", g)
	}
	if g != cfg.GEst {
		t.Fatalf("fallback gain = %v, want the configured prior %v", g, cfg.GEst)
	}
}

// TestGainEstimateTooFewSnaps: a fresh regulator (empty/short history) falls back to the prior.
func TestGainEstimateTooFewSnaps(t *testing.T) {
	r := New(nil, nil)
	if g, measured := r.GainEstimate(); measured || g != DefaultConfig().GEst {
		t.Fatalf("short history must fall back to the prior, got g=%v measured=%v", g, measured)
	}
}

// TestHotPlantFailsTheKgCheck is the NON-TAUTOLOGY proof (X.6 #15): a plant hot enough that the
// measured K*g leaves (0,2) must FAIL the "0<K*g<2 (regulator stable)" check — the condition can now
// actually fire, which the fixed placeholder never could.
func TestHotPlantFailsTheKgCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LamStar = 40.0
	r := New(nil, &cfg)
	drivePlant(r, 60.0, 40.0, 80) // alpha*g ~ 14 -> clamped to 10 -> K*g = 4 >= 2
	g, measured := r.GainEstimate()
	if !measured {
		t.Fatalf("the hot plant should be identifiable, got fallback g=%v", g)
	}
	if cfg.GainK*g < 2.0 {
		t.Fatalf("expected K*g >= 2 on the hot plant (g=%v, K*g=%v)", g, cfg.GainK*g)
	}
	failed := false
	for _, c := range r.Stability("reactive", false) {
		if c.Name == "0<K*g<2 (regulator stable)" && !c.Pass {
			failed = true
		}
	}
	if !failed {
		t.Fatalf("the 0<K*g<2 check must FAIL on the hot plant — it is still tautological (g=%v)", g)
	}
}
