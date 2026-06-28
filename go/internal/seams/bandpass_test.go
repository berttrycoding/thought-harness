package seams

import (
	"math"
	"testing"
)

// TestBandPassTransientSpikeAttenuated pins the LOW-PASS half (04 §2.1): a one-tick transient spike
// is NOT trusted — the EMA carries little of a single sample, so the band-pass output for that tick
// is far below the spike's amplitude. A "flash in the pan" hallucination cannot pass on one tick.
func TestBandPassTransientSpikeAttenuated(t *testing.T) {
	bp := NewBandPass(DefaultBandPassConfig())
	// A long run of zeros, then a single one-tick spike, then zeros again.
	var spikeOut float64
	for tick := 0; tick < 30; tick++ {
		x := 0.0
		if tick == 20 {
			x = 1.0 // the transient
		}
		out := bp.Step(x)
		if tick == 20 {
			spikeOut = out.Passed
		}
	}
	// The LPF, seeing one sample of 1.0 after a flat background, carries only alpha_low of it; the
	// band-pass output is therefore heavily attenuated relative to the raw 1.0 spike.
	if spikeOut >= 0.5 {
		t.Fatalf("a one-tick transient spike must be attenuated by the LPF; got passed=%.3f (want < 0.5)", spikeOut)
	}
}

// TestBandPassSustainedNovelSignalPasses pins the band (04 §2.1): a signal that is both PERSISTENT
// (rises and holds — the LPF builds up) AND NOVEL (it is a change from the prior background — the HPF
// fires while it rises) passes strongly at least once. This is the "real, worth-it" insight.
func TestBandPassSustainedNovelSignalPasses(t *testing.T) {
	bp := NewBandPass(DefaultBandPassConfig())
	// Flat zero background, then a step up to 1.0 that is HELD for many ticks (persistent + novel).
	var peak float64
	for tick := 0; tick < 60; tick++ {
		x := 0.0
		if tick >= 20 {
			x = 1.0 // the sustained step
		}
		out := bp.Step(x)
		if tick >= 20 && out.Passed > peak {
			peak = out.Passed
		}
	}
	// While the step is rising the LPF is climbing (persistence) and the HPF is positive (novelty vs
	// the slow LPF), so the product/min passes a real signal through. It must clear a meaningful floor.
	if peak <= 0.15 {
		t.Fatalf("a sustained novel signal must pass the band-pass; got peak passed=%.3f (want > 0.15)", peak)
	}
}

// TestBandPassConstantDCRejected pins the HIGH-PASS half (04 §2.1): a constant DC background — the
// already-known signal restated forever — is rejected. Once the LPF has tracked the constant, the HPF
// (x − LPF) goes to ~0: nothing NEW is being added, so nothing passes. "Let the known fade."
func TestBandPassConstantDCRejected(t *testing.T) {
	bp := NewBandPass(DefaultBandPassConfig())
	// A constant DC level held for a long time — the LPF converges to it, the HPF decays to ~0.
	var lastOut float64
	for tick := 0; tick < 200; tick++ {
		out := bp.Step(0.8) // constant background
		lastOut = out.Passed
	}
	if lastOut >= 0.05 {
		t.Fatalf("a converged constant DC background must be rejected by the HPF; got passed=%.4f (want < 0.05)", lastOut)
	}
}

// TestBandPassDeterministic pins the determinism contract (04 §2.1 / 03 §3.4): the filter is a pure
// tick-domain recurrence — no wall clock, no RNG — so two instances fed the IDENTICAL sequence
// produce byte-identical outputs. The golden oracle requires it.
func TestBandPassDeterministic(t *testing.T) {
	seq := []float64{0, 0.1, 0.9, 0.9, 0.2, 0.0, 0.7, 0.7, 0.7, 0.3}
	a := NewBandPass(DefaultBandPassConfig())
	b := NewBandPass(DefaultBandPassConfig())
	for i, x := range seq {
		oa := a.Step(x)
		ob := b.Step(x)
		if oa.Passed != ob.Passed || oa.LowPass != ob.LowPass || oa.HighPass != ob.HighPass {
			t.Fatalf("step %d: nondeterministic output a=%+v b=%+v", i, oa, ob)
		}
	}
}

// TestBandPassReset pins that Reset returns the filter to its constructed state so a fresh episode
// starts cold (the per-episode determinism the engine relies on).
func TestBandPassReset(t *testing.T) {
	bp := NewBandPass(DefaultBandPassConfig())
	for i := 0; i < 10; i++ {
		bp.Step(0.9)
	}
	bp.Reset()
	fresh := NewBandPass(DefaultBandPassConfig())
	out := bp.Step(0.5)
	want := fresh.Step(0.5)
	if math.Abs(out.Passed-want.Passed) > 1e-12 || math.Abs(out.LowPass-want.LowPass) > 1e-12 {
		t.Fatalf("after Reset the filter must behave like a fresh one; got %+v want %+v", out, want)
	}
}

// TestBandPassCutoffsTunable pins that the cutoffs are configurable (the tunable-but-frozen "skin",
// 04 §1): a wider/narrower band changes the response. A very high alpha_low (fast LPF) lets a sharper
// transient through than the conservative default — the dev-tuning knob is real, not cosmetic.
func TestBandPassCutoffsTunable(t *testing.T) {
	slow := NewBandPass(BandPassConfig{AlphaLow: 0.1, AlphaHigh: 0.5})
	fast := NewBandPass(BandPassConfig{AlphaLow: 0.9, AlphaHigh: 0.5})
	var slowSpike, fastSpike float64
	for tick := 0; tick < 10; tick++ {
		x := 0.0
		if tick == 5 {
			x = 1.0
		}
		so := slow.Step(x)
		fo := fast.Step(x)
		if tick == 5 {
			slowSpike, fastSpike = so.Passed, fo.Passed
		}
	}
	if !(fastSpike > slowSpike) {
		t.Fatalf("a faster LPF must let more of the transient through; slow=%.3f fast=%.3f", slowSpike, fastSpike)
	}
}

// TestBandPassColdStartFirstAppearanceHighInjects pins the B1f cold-start FIX (04-seams §2.1): a signal
// that appears HIGH on its FIRST tick and SUSTAINS high is a NOVEL step-edge the conscious has never
// seen, so the spec's HPF must INJECT it at the step (then let it fade to DC). The LEGACY filter
// (ColdStartZeroRef off) seeds the HPF reference to x[0] ⇒ HPF = x − x = 0 ⇒ it is suppressed FOREVER;
// the FIX cold-starts from 0 and suppresses only the one-tick priming warm-up.
func TestBandPassColdStartFirstAppearanceHighInjects(t *testing.T) {
	const floor = 0.05
	feed := func(bp *BandPass) (primingPassed float64, postPrimingPeak float64, lateTail float64) {
		for tick := 0; tick < 40; tick++ {
			out := bp.Step(0.95) // appears HIGH on tick 0 and SUSTAINS high
			switch {
			case tick == 0:
				primingPassed = out.Passed
			case tick <= 3:
				if out.Passed > postPrimingPeak {
					postPrimingPeak = out.Passed
				}
			}
			lateTail = out.Passed // the last tick, once the reference has converged (now DC)
		}
		return
	}

	// LEGACY (the documented divergence): suppressed on EVERY tick — never clears the floor.
	legacy := NewBandPass(DefaultBandPassConfig())
	lp, lpeak, _ := feed(legacy)
	if lp != 0 || lpeak >= floor {
		t.Fatalf("legacy cold-start must suppress a first-appearance-high signal forever; priming=%.4f post-priming-peak=%.4f (want both < floor %.2f)", lp, lpeak, floor)
	}

	// FIX: the priming tick is still suppressed (one-tick warm-up — a flash never injects on appearance),
	// but a SUSTAINED first appearance clears the floor right after, then fades to DC.
	cfg := DefaultBandPassConfig()
	cfg.ColdStartZeroRef = true
	fix := NewBandPass(cfg)
	fp, fpeak, ftail := feed(fix)
	if fp != 0 {
		t.Errorf("cold-start FIX: the priming tick must still be suppressed (one-tick warm-up); got passed=%.4f", fp)
	}
	if fpeak < floor {
		t.Errorf("cold-start FIX: a SUSTAINED first appearance must INJECT at the step (clear the floor) right after priming; got post-priming peak=%.4f (want >= %.2f)", fpeak, floor)
	}
	if ftail >= floor {
		t.Errorf("cold-start FIX: once the reference converges the signal is DC and must fade below the floor; got late tail=%.4f", ftail)
	}
}

// TestBandPassColdStartFlashStillSuppressed pins that the B1f fix does NOT break transient suppression:
// a one-tick flash-in-the-pan on a fresh stream must still NEVER inject on its appearance tick under the
// cold-start fix (the one-tick warm-up is exactly the guard that distinguishes a flash from a sustained
// first appearance — only the latter survives to the next tick).
func TestBandPassColdStartFlashStillSuppressed(t *testing.T) {
	const floor = 0.05
	cfg := DefaultBandPassConfig()
	cfg.ColdStartZeroRef = true
	bp := NewBandPass(cfg)

	var maxPassed float64
	for tick := 0; tick < 20; tick++ {
		x := 0.0
		if tick == 0 {
			x = 0.95 // the one-tick flash on first appearance, then silence
		}
		out := bp.Step(x)
		if out.Passed > maxPassed {
			maxPassed = out.Passed
		}
	}
	if maxPassed >= floor {
		t.Fatalf("cold-start FIX: a one-tick flash-in-the-pan must still be suppressed (never clears the floor); got max passed=%.4f", maxPassed)
	}
}

// TestBandPassClampsAlpha pins that out-of-range cutoffs are clamped into (0,1] so a misconfigured
// knob can never make the recurrence diverge (a control-parameter inside the stability budget, 04 §2.1).
func TestBandPassClampsAlpha(t *testing.T) {
	bp := NewBandPass(BandPassConfig{AlphaLow: 5.0, AlphaHigh: -2.0})
	// Must not panic or diverge; outputs stay finite over a long run.
	for i := 0; i < 50; i++ {
		out := bp.Step(0.6)
		if math.IsNaN(out.Passed) || math.IsInf(out.Passed, 0) {
			t.Fatalf("clamped filter produced a non-finite output at step %d: %v", i, out.Passed)
		}
	}
}
