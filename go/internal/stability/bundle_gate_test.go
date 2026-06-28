package stability

// bundle_gate_test.go — the COMBINED awake go-live BUNDLE durability gate (continuous-mode-operator,
// 2026-06-21). The three flags were each durability-gated INDIVIDUALLY:
//   - conscious.activity.awake_user_dispatch (awake engages the subconscious on a user line; worst peak_n≈0.605
//     on the lighter awake plant)
//   - sense.self_model (the standing declarative self-model percept — a μ-baseline immigrant, n unchanged)
//   - subconscious.dispatch.sparse (sparsemax dispatch admission — a competitive NARROWING, n unchanged-or-REDUCED)
//
// The B4 LESSON: a COMBINED pass catches interactions a single-knob pass misses (soft turned out to be the
// dominant fork-excitation knob only when combined). So this gates all THREE flags ON TOGETHER on the
// ACTUAL go-live plant (config.ApplyAwakeDefaults — soft + forest + drive_agenda + seed_intents@20 + β=0.5 +
// proactive_outreach, the B4-flipped default-ON awake mind, peak_n≈0.888/600t baseline), NOT the lighter
// awakeFlagOnFeatures() plant the standing self-model/sparse cells use. It samples the every-tick peak over a
// representative awake stream INCLUDING MULTI-INPUT (the awake-dispatch flag's plant change only fires on a
// focused unresolved user line — a bare stream would never exercise it). Offline TestBackend double, seed=7.
//
// This file is ADDITIVE (constructs engines + measures; flips no default) and is the durability half of the
// user-authorized bundle go-live. Verbose rows are `-v`-gated; the load-bearing PASS/FAIL guard is
// TestBundleGoLiveDurable.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// bundleGoLiveFeatures builds the EXACT go-live plant: config.ApplyAwakeDefaults (the B4-flipped default-ON
// awake mind, the real base the three flags would be folded onto) PLUS the three bundle flags ON. This is
// apples-to-apples with what flipping the bundle into ApplyAwakeDefaults would produce.
func bundleGoLiveFeatures() *config.HarnessConfig {
	feat := config.New()
	config.ApplyAwakeDefaults(feat)                  // the real go-live base: soft + forest + drive_agenda + seed_intents@20 + β=0.5 + outreach
	feat.Conscious.Activity.AwakeUserDispatch = true // BUNDLE flag 1 — awake engages the subconscious on a user line
	feat.Sense.SelfModel = true                      // BUNDLE flag 2 — the standing declarative self-model percept (μ-baseline)
	feat.Subconscious.SparseDispatch = true          // BUNDLE flag 3 — sparsemax dispatch admission
	feat.Validate()
	return feat
}

// awakeBaseGoLiveFeatures is the go-live BASE alone (ApplyAwakeDefaults, no bundle flags) — the control row
// for the interaction diff: does the bundle add anything on top of the soft-dominated baseline?
func awakeBaseGoLiveFeatures() *config.HarnessConfig {
	feat := config.New()
	config.ApplyAwakeDefaults(feat)
	feat.Validate()
	return feat
}

// bundleInputs are representative program-shaped (engineering) user lines — the case awake-dispatch targets
// (a goal with a workflow shape that the subconscious recognises). Multi-input is required: the flag's plant
// change (synthesise a workflow on a focused unresolved user line) fires once per user line.
var bundleInputs = []string{
	"design a rate limiter that supports BOTH per-tenant and a global cap, and explain how the two interact when a tenant's burst would push the system past the global cap",
	"now, given that design, what happens to in-flight requests when the global cap is hot-reloaded to a LOWER value mid-traffic?",
	"separately: trace how a token refill and a burst-drain race if they fire on the same tick, and which wins",
}

// bundlePeaksMultiInput runs an awake engine step-by-step on the given features over a LONG horizon and
// samples the regulator EVERY tick (the C1 lesson: n's EMA spikes BETWEEN every-100 sample points), while
// REPEATEDLY submitting the bundleInputs mid-stream (warm-up wander, then a submit every `cadence` ticks,
// cycling the inputs) so the awake-dispatch plant change is exercised AND the soft policy's n-EMA has the
// horizon to climb to its steady peak (the dominant excitation). μ is the STEADY-STATE baseline (μ=0 during
// the first-tick warmup transient by design), so it is sampled on the every-100 cadence like the standing
// guards — over a horizon long enough for that cadence to fire. Returns the over-the-run peaks + worst-case
// async outstanding + the END-of-run regime provenance + the submitted-input count.
func bundlePeaksMultiInput(feat *config.HarnessConfig, ticks, settle, cadence int) (peakN, peakU, minMu float64, maxOut, inputs int, regime regulator.Regime, measured, kgNA bool) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("bundlePeaksMultiInput: NewEngine failed: " + err.Error())
	}
	minMu = 1.0
	for t := 1; t <= ticks; t++ {
		// Submit a fresh user line on the cadence (after the warm-up settle), cycling the inputs — keeps a
		// focused unresolved user line recurring so awake-dispatch fires repeatedly over the long run.
		if t > settle && (t-settle)%cadence == 0 {
			e.SubmitDefault(bundleInputs[inputs%len(bundleInputs)])
			inputs++
		}
		e.Step()
		r := e.Regulator()
		peakN = maxF(peakN, r.N())
		peakU = maxF(peakU, r.Util())
		if out := e.ActionOutstanding(); out > maxOut {
			maxOut = out
		}
		if t%100 == 0 {
			if r.Mu() < minMu {
				minMu = r.Mu()
			}
		}
	}
	checks, rg, _, m := e.Regulator().StabilityRegime("continuous")
	regime, measured = rg, m
	for _, c := range checks {
		if c.Name == "0<K*g<2 (regulator stable)" {
			kgNA = c.NA
		}
	}
	return peakN, peakU, minMu, maxOut, inputs, regime, measured, kgNA
}

// bundlePeaksBareLong runs the bundle on a BARE long awake stream (no user input) — the steady-state
// excitation the soft policy sets, the same horizon the B4 600t/2000t crossing-checks use. This is the
// dominant-n confirmation: does the bundle drift the soft-set peak toward the n=1 cliff over a long run?
func bundlePeaksBareLong(feat *config.HarnessConfig, ticks int) (peakN, peakU, minMu float64, maxOut int, regime regulator.Regime, measured, kgNA bool) {
	return awakePeaks(feat, ticks)
}

// bundleRow is one config's measured row.
type bundleRow struct {
	label    string
	peakN    float64
	peakU    float64
	minMu    float64
	maxOut   int
	regime   regulator.Regime
	measured bool
	kgNA     bool
}

func (r bundleRow) durable() bool {
	conds := r.peakN < 1.0 && r.peakU <= 1.0 && r.minMu > 0.0 && r.maxOut <= cognition.MaxParWidth
	benign := r.regime == regulator.RegimeSaturatedBounded && !r.measured && r.kgNA
	return conds && benign
}

// TestBundleGoLiveDurable is the LOAD-BEARING combined-bundle durability guard. It asserts the three bundle
// flags ON TOGETHER on the go-live plant hold the durable regime as a SINGLE pass — the gap the three
// single-knob passes could not catch — under BOTH a multi-input stream (exercises awake-dispatch) and a
// long bare stream (confirms the soft-set steady peak does not drift toward the cliff). Offline, seed=7.
func TestBundleGoLiveDurable(t *testing.T) {
	full := bundleGoLiveFeatures()

	// (1) the SOUND gate (24-tick continuous row, the exact subcommand path) PASSES and is benign.
	rep := CheckEngine(runAwakeWithFeatures(full, 24), "BUNDLE (awake_user_dispatch+self_model+sparse on go-live awake)", "continuous")
	if !rep.OK() {
		t.Fatalf("BUNDLE failed the 24-tick durability gate:\n%s", rep.Format())
	}
	if rep.Regime != regulator.RegimeSaturatedBounded.String() {
		t.Fatalf("BUNDLE regime = %q, want saturated-bounded (θ pinned at the floor => benign open loop)", rep.Regime)
	}
	if rep.GainMeasured {
		t.Fatalf("BUNDLE gain must be the PRIOR fallback (loop open), not identified — an open-loop K·g must never be a real check")
	}

	// (2) MULTI-INPUT every-tick peaks over a LONG horizon (the awake-dispatch plant change is exercised
	//     repeatedly AND the soft policy's n-EMA has room to climb to its steady peak).
	pN, pU, mMu, mOut, nIn, rg, m, kgNA := bundlePeaksMultiInput(full, 600, 6, 40)
	mi := bundleRow{"bundle multi-input", pN, pU, mMu, mOut, rg, m, kgNA}
	t.Logf("BUNDLE multi-input (600t, %d inputs): peak_n=%.4f peak_U=%.4f min_mu=%.4f max_out=%d regime=%s g=prior kgNA=%v",
		nIn, mi.peakN, mi.peakU, mi.minMu, mi.maxOut, mi.regime.String(), mi.kgNA)
	if !mi.durable() {
		t.Fatalf("BUNDLE not durable on the multi-input stream: peak_n=%.4f peak_U=%.4f min_mu=%.4f max_out=%d regime=%v measured=%v kgNA=%v",
			mi.peakN, mi.peakU, mi.minMu, mi.maxOut, mi.regime, mi.measured, mi.kgNA)
	}

	// (3) BARE long-horizon every-tick peaks (600t) — the soft-dominated steady-state cliff check.
	bN, bU, bMu, bOut, brg, bm, bkg := bundlePeaksBareLong(full, 600)
	bl := bundleRow{"bundle bare 600t", bN, bU, bMu, bOut, brg, bm, bkg}
	t.Logf("BUNDLE bare (600t): peak_n=%.4f peak_U=%.4f min_mu=%.4f max_out=%d regime=%s g=prior kgNA=%v",
		bl.peakN, bl.peakU, bl.minMu, bl.maxOut, bl.regime.String(), bl.kgNA)
	if !bl.durable() {
		t.Fatalf("BUNDLE not durable on the bare 600t stream: peak_n=%.4f peak_U=%.4f min_mu=%.4f max_out=%d",
			bl.peakN, bl.peakU, bl.minMu, bl.maxOut)
	}

	// (4) the ADVERSARIAL excitation bound — the bundle must not push the soft-set peak past the recorded
	//     ~0.93 (2000t) toward the n=1 cliff. MEASURED 2026-06-21 (every-tick peak, TestBackend, the go-live
	//     ApplyAwakeDefaults base): the bundle peaks at seed=7 0.909/600t (0.929/2000t, 49 inputs; 0.901 on a
	//     dense 99-input flood — the denser flood does NOT compound, confirming awake-dispatch ROUTES rather
	//     than forks) and WORST 0.942 at seed=11 across the 6-seed sweep {7,11,42,101,1337,2024} (~0.058
	//     headroom). The B4 BASE (ApplyAwakeDefaults alone) is 0.888/600t (0.925/2000t) — `soft` is the
	//     DOMINANT n-knob and the three flags ride under it: on the BARE stream the bundle is LOWER than base
	//     (-0.055, the narrowing flags), and on the MULTI-INPUT stream the bundle adds a small BOUNDED +0.052
	//     (awake-dispatch routing recurring user lines) — NOT a super-additive spike. The bound 0.95 (mirrors
	//     b4_combined_test.go) guards against the peak drifting up from the recorded ~0.94 worst-seed; the hard
	//     fail is the n=1 cliff (asserted by durable()). seed=7 is the suite-convention seed; raise this bound
	//     only with a re-measure. β=0.5 is the dial that holds the headroom (β=1.0 base → 0.948/2000t).
	worst := maxF(mi.peakN, bl.peakN)
	if worst >= 0.95 {
		t.Fatalf("BUNDLE excited branching past the recorded bound: peak_n=%.4f >= 0.95 (drifted toward the n=1 cliff — re-tune branch_propensity or reduce excitation)", worst)
	}
}

// TestBundleInteractionDiff prints the per-config interaction table (verbose, never fails). It compares the
// go-live BASE (ApplyAwakeDefaults alone) vs the BUNDLE (base + the three flags) on the multi-input stream,
// so the super-additive-spike question is answered with a number: does adding the three flags move peak_n?
func TestBundleInteractionDiff(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("verbose-only interaction diff (run with -v)")
	}
	type cfgRow struct {
		label string
		feat  *config.HarnessConfig
	}
	rows := []cfgRow{
		{"go-live BASE (ApplyAwakeDefaults, no bundle)", awakeBaseGoLiveFeatures()},
		{"BUNDLE (base + 3 flags)", bundleGoLiveFeatures()},
	}
	t.Logf("%-46s | gate | peak_n(mi) | peak_n(600t) | peak_U | min_mu | fan | regime", "config")
	var miBase, miBundle, blBase, blBundle float64
	for i, c := range rows {
		rep := CheckEngine(runAwakeWithFeatures(c.feat, 24), "diff:"+c.label, "continuous")
		gate := "FAIL"
		if rep.OK() {
			gate = "PASS"
		}
		pN, pU, mMu, mOut, _, rg, _, _ := bundlePeaksMultiInput(c.feat, 600, 6, 40)
		bN, _, _, _, _, _, _ := bundlePeaksBareLong(c.feat, 600)
		t.Logf("%-46s | %-4s | %10.4f | %12.4f | %6.4f | %6.4f | %3d | %s",
			c.label, gate, pN, bN, pU, mMu, mOut, rg.String())
		if i == 0 {
			miBase, blBase = pN, bN
		} else {
			miBundle, blBundle = pN, bN
		}
	}
	t.Logf("INTERACTION (peak_n): multi-input base=%.4f bundle=%.4f (delta %+.4f) | bare-600t base=%.4f bundle=%.4f (delta %+.4f)",
		miBase, miBundle, miBundle-miBase, blBase, blBundle, blBundle-blBase)
}
