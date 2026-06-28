package regulator

// Tests for the C0a/C0b durability-gate reframe: a SATURATED controller is open-loop, so the
// 0<K·g<2 loop-gain condition is vacuous (boundedness governs); the verdict is three-way and the
// honest FAIL is reserved for a sustained-but-unidentified loop — killing the old prior-fallback
// tautology where K·prior=0.2<2 always passed.

import (
	"math"
	"testing"
)

// driveSaturating runs the closed loop against a plant so weak that λ̂ stays well below λ*, so the
// control law ratchets θ down and PINS it at ThetaMin — the awake-mode saturation case. A small
// periodic baseline keeps μ>0 (so the awake μ check is a real passing bool) while λ̂'s mean stays
// below λ*=1.0 (mirrors the real awake run: λ̂≈0.5, μ≈0.55, both below λ*).
func driveSaturating(r *Regulator, n int) {
	for i := 0; i < n; i++ {
		o := DefaultUpdateOpts()
		o.Fired = 0
		if i%2 == 0 {
			o.Baseline = 1 // ~0.5 mean baseline => μ>0 yet λ̂ stays below λ*=1 => θ pins at the floor
		}
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
	}
}

// driveInteriorUnidentified runs the loop with an EXOGENOUS, θ-independent intensity centred on λ*
// and of large amplitude: λ̂ swings both sides of λ*, so the control law keeps θ wandering the
// INTERIOR (never pinned), yet the intensity is uncorrelated with θ — so the plant is NOT
// identifiable. This is the sustained, moving, unidentified loop: the honest-FAIL case.
func driveInteriorUnidentified(r *Regulator, n int) {
	var x uint64 = 0x1234567
	next := func() int {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return int(x % 21) // 0..20, mean 10 == LamStar, theta-independent
	}
	for i := 0; i < n; i++ {
		o := DefaultUpdateOpts()
		o.Fired = next()
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
	}
}

// TestSaturatedDetectsPinnedController: a controller ratcheted to ThetaMin and held there is detected
// as saturated (a long trailing pinned run, var(θ)≈0), and LoopOpen reports "saturated".
func TestSaturatedDetectsPinnedController(t *testing.T) {
	r := New(nil, nil) // LamStar 1.0, ThetaMin 0.05
	driveSaturating(r, 40)
	if got := r.Theta(); math.Abs(got-r.cfg.ThetaMin) > 1e-9 {
		t.Fatalf("expected θ pinned at ThetaMin=%.2f, got %.4f", r.cfg.ThetaMin, got)
	}
	sat, runLen, tvar := r.Saturated()
	if !sat {
		t.Fatalf("a pinned controller must read as saturated (runLen=%d var=%.2e)", runLen, tvar)
	}
	if runLen < satMinRun {
		t.Fatalf("trailing pinned run %d below the saturation minimum %d", runLen, satMinRun)
	}
	if tvar > satMaxVar {
		t.Fatalf("pinned-run var(θ)=%.2e should be ~0", tvar)
	}
	open, reason := r.LoopOpen()
	if !open || reason != "saturated" {
		t.Fatalf("LoopOpen on a pinned controller must be (true,\"saturated\"), got (%v,%q)", open, reason)
	}
}

// TestSaturatedRejectsFlapping: a controller whose θ alternates between the two clamps is ACTIVE
// (bang-bang) control, not an open loop — the trailing run at a SINGLE clamp is length 1, so it is
// NOT saturated. Driven directly by forcing alternating large errors that fully rail θ each tick.
func TestSaturatedRejectsFlapping(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ThetaMin, cfg.ThetaMax = 0.0, 1.0
	cfg.GainK = 1000.0 // huge gain => one tick's error fully rails θ to a clamp
	cfg.Alpha = 1.0    // no EMA smoothing => λ̂ = this tick's events => the error sign alternates cleanly
	r := New(nil, &cfg)
	for i := 0; i < 40; i++ {
		o := DefaultUpdateOpts()
		if i%2 == 0 {
			o.Fired = 100 // λ̂=100 >> λ*=1 => θ slams to ThetaMax
		} else {
			o.Fired = 0 // λ̂=0 << λ*=1 => θ slams to ThetaMin
		}
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
	}
	sat, runLen, _ := r.Saturated()
	if sat {
		t.Fatalf("a flapping (bang-bang) controller must NOT read as saturated (runLen=%d)", runLen)
	}
	if runLen > 1 {
		t.Fatalf("a flapping θ should have a single-clamp trailing run of 1, got %d", runLen)
	}
}

// TestStabilitySaturatedBoundedRegime: under saturation the 0<K·g<2 check is NA (open-loop, vacuous)
// rather than a counted pass, the regime label is saturated-bounded, and durability holds on the
// other four conditions. This is the awake-mode reframe — a PASS-by-saturation, NOT a prior-pass.
func TestStabilitySaturatedBoundedRegime(t *testing.T) {
	r := New(nil, nil)
	driveSaturating(r, 40)
	_, measured := r.GainEstimate()
	if measured {
		t.Fatalf("a pinned controller cannot identify the plant; expected the prior fallback")
	}
	checks, regime, _, gmeasured := r.StabilityRegime("awake")
	if regime != RegimeSaturatedBounded {
		t.Fatalf("pinned controller regime = %v, want saturated-bounded", regime)
	}
	if gmeasured {
		t.Fatalf("regime g-provenance should be prior-fallback (measured=false)")
	}
	kg := checks[2]
	if kg.Name != "0<K*g<2 (regulator stable)" || !kg.NA || kg.Pass {
		t.Fatalf("saturated K·g must be NA (vacuous), not a pass: %+v", kg)
	}
	if kg.NADetail != "K·g N/A — saturated/open-loop" {
		t.Fatalf("saturated K·g NA detail wrong: %q", kg.NADetail)
	}
	// the other four conditions are real, measured booleans.
	for _, c := range []Check{checks[0], checks[1], checks[3], checks[4]} {
		if c.NA {
			t.Fatalf("the boundedness condition %q must be a real bool under saturation, not NA", c.Name)
		}
	}
}

// TestStabilityUnidentifiedActiveFAILS is the ANTI-TAUTOLOGY proof: a sustained loop with θ genuinely
// MOVING in the interior (never pinned) whose plant is NOT identifiable must FAIL the 0<K·g<2 check —
// it is neither an actively-controlled-stable identified loop nor a saturated/open-loop vacuity. The
// OLD code passed this silently (K·prior = 0.4·0.5 = 0.2 < 2); the reframe fails it honestly. Without
// this case the reframe would just be a new tautology.
func TestStabilityUnidentifiedActiveFAILS(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LamStar = 10.0
	cfg.GainK = 0.02 // gentle gain => θ wanders the interior rather than railing to a clamp
	cfg.ThetaMin, cfg.ThetaMax = 0.0, 1.0
	r := New(nil, &cfg)
	driveInteriorUnidentified(r, 120)

	// preconditions: NOT identified, NOT pinned, a sustained loop with real θ movement.
	g, measured := r.GainEstimate()
	if measured {
		t.Fatalf("the exogenous-noise plant must be UNIDENTIFIED (got measured g=%.3f)", g)
	}
	if g != cfg.GEst {
		t.Fatalf("unidentified g must be the prior %.2f, got %.3f", cfg.GEst, g)
	}
	if sat, runLen, _ := r.Saturated(); sat {
		t.Fatalf("θ must be wandering the interior, not saturated (runLen=%d)", runLen)
	}
	if open, reason := r.LoopOpen(); open {
		t.Fatalf("a sustained moving loop must NOT be open-loop, got open=true reason=%q", reason)
	}

	// verdict: K·g FAILS honestly (Pass=false, NOT NA), regime unidentified-active-FAIL.
	checks, regime, _, _ := r.StabilityRegime("reactive")
	if regime != RegimeUnidentifiedActive {
		t.Fatalf("regime = %v, want unidentified-active-FAIL", regime)
	}
	kg := checks[2]
	if kg.NA {
		t.Fatalf("an unidentified ACTIVE loop must FAIL, not be reported NA: %+v", kg)
	}
	if kg.Pass {
		t.Fatalf("the 0<K·g<2 check must FAIL on an unidentified active loop — still tautological")
	}
	// and the OLD code would have passed it: K·prior is squarely inside (0,2).
	if !(0 < cfg.GainK*cfg.GEst && cfg.GainK*cfg.GEst < 2) {
		t.Fatalf("sanity: the prior K·g should sit inside (0,2) — that is the tautology being killed")
	}
}

// TestRegimeStringLabels pins the three regime labels reported on the stability output + event.
func TestRegimeStringLabels(t *testing.T) {
	cases := map[Regime]string{
		RegimeActivelyControlled: "actively-controlled-stable",
		RegimeSaturatedBounded:   "saturated-bounded",
		RegimeUnidentifiedActive: "unidentified-active-FAIL",
	}
	for rg, want := range cases {
		if got := rg.String(); got != want {
			t.Fatalf("Regime(%d).String() = %q, want %q", rg, got, want)
		}
	}
}

// driveRunaway pins θ at ThetaMax under a plant the controller CANNOT suppress: the intensity is a
// constant load far above λ* regardless of θ. The control law ratchets θ up to ThetaMax (maximum
// suppression) and holds it there, yet λ̂ stays pinned at the load — the controller is trying and
// FAILING to bring intensity down. This is the HOLE 1 control-loss case (the red-team 100×λ* repro).
func driveRunaway(r *Regulator, load, n int) {
	for i := 0; i < n; i++ {
		o := DefaultUpdateOpts()
		o.Fired = load // θ-independent, far over λ* => θ rails to ThetaMax but cannot bring λ̂ down
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
	}
}

// driveBenignThetaMax pins θ at ThetaMax under a plant only MILDLY over setpoint: a steady intensity
// just above λ* (within the runaway ceiling 1.5·λ*). The control law ratchets θ to ThetaMax and holds
// it (λ̂ > λ* every tick), but λ̂ never exceeds the intensity ceiling — a transient overshoot the
// controller is still holding, NOT a runaway. This must STILL pass saturated-bounded.
func driveBenignThetaMax(r *Regulator, n int) {
	for i := 0; i < n; i++ {
		o := DefaultUpdateOpts()
		o.Fired = 1 // λ̂ -> ~1.0; with a tiny baseline below the 1.5×λ* ceiling, θ rails to ThetaMax
		if i%4 == 0 {
			o.Baseline = 1 // nudge λ̂ just above λ*=1.0 so θ ratchets UP and pins at ThetaMax (≈1.25 mean)
		}
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
	}
}

// TestStabilitySaturatedRunawayFAILS is the HOLE 1 soundness proof: a controller railed at ThetaMax
// (MAXIMUM suppression) while λ̂ stays far over λ* has LOST intensity control — it must FAIL with the
// distinct saturated-runaway regime, NOT pass as saturated-bounded. The red-team repro: drive load =
// 100×λ* for 40 ticks. θ rails at ThetaMax, λ̂ pins at ~100, n/U still hold — and the gate must NOT
// call it durable.
func TestStabilitySaturatedRunawayFAILS(t *testing.T) {
	r := New(nil, nil) // LamStar 1.0, ThetaMax 0.95
	driveRunaway(r, 100, 40)
	// precondition: θ railed at ThetaMax, λ̂ far over setpoint, saturation detected at the MAX clamp.
	if got := r.Theta(); abs(got-r.cfg.ThetaMax) > 1e-9 {
		t.Fatalf("expected θ pinned at ThetaMax=%.2f, got %.4f", r.cfg.ThetaMax, got)
	}
	if r.LamHat() <= runawayLamFactor*r.cfg.LamStar {
		t.Fatalf("precondition: λ̂=%.2f must exceed the intensity ceiling %.2f", r.LamHat(), runawayLamFactor*r.cfg.LamStar)
	}
	sat, clamp := r.SaturatedAt()
	if !sat || clamp != "max" {
		t.Fatalf("a controller railed at ThetaMax must read saturated at the max clamp, got (sat=%v clamp=%q)", sat, clamp)
	}
	// verdict: regime saturated-runaway-FAIL, K·g FAILS (Pass=false, NOT NA) — control-loss, not vacuous.
	checks, regime, _, _ := r.StabilityRegime("reactive")
	if regime != RegimeSaturatedRunaway {
		t.Fatalf("a ThetaMax-railed runaway (λ̂=%.0f >> λ*) must be saturated-runaway-FAIL, got %v", r.LamHat(), regime)
	}
	kg := checks[2]
	if kg.NA {
		t.Fatalf("a control-loss runaway must FAIL the K·g check, not be reported NA: %+v", kg)
	}
	if kg.Pass {
		t.Fatalf("the 0<K·g<2 check must FAIL under saturated-runaway (control lost) — got Pass")
	}
}

// TestStabilityBenignThetaMaxStillPasses is the HOLE 1 non-over-fire guard: a ThetaMax rail with λ̂
// only mildly over λ* (within the runaway intensity ceiling) is a benign overshoot the controller is
// holding, NOT a control-loss — it must STILL pass saturated-bounded with K·g NA (vacuous). Without
// this, HOLE 1's fix would over-fire on every ThetaMax rail.
func TestStabilityBenignThetaMaxStillPasses(t *testing.T) {
	r := New(nil, nil) // LamStar 1.0, ThetaMax 0.95
	driveBenignThetaMax(r, 60)
	if got := r.Theta(); abs(got-r.cfg.ThetaMax) > 1e-9 {
		t.Fatalf("expected θ pinned at ThetaMax=%.2f, got %.4f", r.cfg.ThetaMax, got)
	}
	if r.LamHat() > runawayLamFactor*r.cfg.LamStar {
		t.Fatalf("precondition: λ̂=%.2f must stay within the intensity ceiling %.2f (benign overshoot)",
			r.LamHat(), runawayLamFactor*r.cfg.LamStar)
	}
	if sat, clamp := r.SaturatedAt(); !sat || clamp != "max" {
		t.Fatalf("expected saturated at the max clamp, got (sat=%v clamp=%q)", sat, clamp)
	}
	checks, regime, _, _ := r.StabilityRegime("reactive")
	if regime != RegimeSaturatedBounded {
		t.Fatalf("a benign ThetaMax overshoot (λ̂=%.2f within ceiling) must be saturated-bounded, got %v",
			r.LamHat(), regime)
	}
	kg := checks[2]
	if !kg.NA || kg.NADetail != "K·g N/A — saturated/open-loop" {
		t.Fatalf("benign ThetaMax K·g must be NA (vacuous, saturated/open-loop): %+v", kg)
	}
}

// TestActivelyControlledStableRegime: an IDENTIFIED plant with K·g inside (0,2) reads as
// actively-controlled-stable with a REAL (failable) passing K·g check — the measured branch.
func TestActivelyControlledStableRegime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LamStar = 15.0 // equilibrium inside the θ band so the loop genuinely moves θ (identifiable)
	r := New(nil, &cfg)
	// an identifiable plant: fired = round(c - g*theta), lag-1 identifiable (as gain_test). True gain
	// 10 => α-attenuated measured g ≈ 3.5 => K·g ≈ 1.4, squarely inside (0,2): actively-controlled-stable.
	theta := r.Theta()
	for i := 0; i < 80; i++ {
		fired := int(math.Round(20.0 - 10.0*theta))
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
	g, measured := r.GainEstimate()
	if !measured {
		t.Fatalf("a steep identifiable plant must yield a MEASURED gain (got prior %.3f)", g)
	}
	kg := cfg.GainK * g
	checks, regime, _, gmeasured := r.StabilityRegime("reactive")
	if regime != RegimeActivelyControlled {
		t.Fatalf("identified plant regime = %v, want actively-controlled-stable (K·g=%.3f)", regime, kg)
	}
	if !gmeasured {
		t.Fatalf("regime g-provenance must be IDENTIFIED on an identifiable plant")
	}
	c := checks[2]
	if c.NA {
		t.Fatalf("an identified loop's K·g check must be a real bool, not NA: %+v", c)
	}
	if c.Pass != (0 < kg && kg < 2) {
		t.Fatalf("K·g check Pass=%v disagrees with 0<%.3f<2", c.Pass, kg)
	}
}
