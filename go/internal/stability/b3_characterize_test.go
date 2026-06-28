package stability

// B3 characterization harness (throwaway-but-kept as a verbose, opt-in characterization). It runs the
// SOUND C0a/C0b durability gate (the same StabilityRegime path CheckEngine + the stability subcommand
// use) across the COMBINED awake config space — forest + drive_agenda + seed_intents at kernel-of-3, a
// mid sizing, and the full portfolio — and prints the per-config durability table. It is `-v`-gated
// (t.Logf only) and never fails on a verdict: the standing PASS/FAIL guard is
// TestB3CombinedAwakeConfigDurable below. Offline TestBackend double, seed=7 (deterministic).

import (
	"fmt"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/regulator"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// b3Config is one point in the combined awake config space.
type b3Config struct {
	name        string
	forest      bool
	driveAgenda bool
	seedIntents bool
	seedCount   int
}

// b3ConfigSpace enumerates the combined awake configs B3 characterizes. The progression isolates each
// knob's plant contribution, then combines all three at each seed sizing (kernel-of-3 / mid / full
// portfolio). The seed sizings are the C1-validated extremes plus a mid value the C1 guard did NOT
// cover. seedCount is only consulted when seedIntents is on.
func b3ConfigSpace() []b3Config {
	mid := (cognition.SeedKernelSize + cognition.SeedPortfolioSize()) / 2 // ~11
	return []b3Config{
		{name: "baseline (all awake knobs OFF)"},
		{name: "forest only", forest: true},
		{name: "drive_agenda only", driveAgenda: true},
		{name: "seed_intents only (kernel=3)", seedIntents: true, seedCount: cognition.SeedKernelSize},
		{name: "forest+drive_agenda", forest: true, driveAgenda: true},
		{name: "COMBINED kernel-of-3", forest: true, driveAgenda: true, seedIntents: true, seedCount: cognition.SeedKernelSize},
		{name: "COMBINED mid", forest: true, driveAgenda: true, seedIntents: true, seedCount: mid},
		{name: "COMBINED full portfolio", forest: true, driveAgenda: true, seedIntents: true, seedCount: cognition.SeedPortfolioSize()},
	}
}

// featuresFor builds an AllOn config with the B3 awake knobs flipped per the config point (Validated).
func featuresFor(bc b3Config) *config.HarnessConfig {
	feat := config.New()
	feat.Conscious.Activity.Forest = bc.forest
	feat.Conscious.Activity.DriveAgenda = bc.driveAgenda
	feat.Conscious.Activity.SeedIntents = bc.seedIntents
	if bc.seedIntents {
		feat.Conscious.Activity.SeedIntentCount = bc.seedCount
	}
	feat.Validate()
	return feat
}

// runAwakeWithFeatures builds a continuous-mode engine on the test double with the given features,
// runs it for `ticks`, and returns the engine. seed=7 keeps it deterministic — the same seed the C1
// guard + TestAwakeDurabilityHoldsOverAnExtendedRun pin.
func runAwakeWithFeatures(feat *config.HarnessConfig, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("runAwakeWithFeatures: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(ticks)
	return e
}

// awakePeaks runs an awake engine step-by-step and samples the regulator EVERY tick (the C1 lesson: n's
// EMA spikes BETWEEN every-100 sample points, so a sparse sample under-reads the true peak). It returns
// the over-the-run peaks + the worst-case async outstanding, and the END-of-run regime/measured flag.
func awakePeaks(feat *config.HarnessConfig, ticks int) (peakN, peakU, minMu float64, maxOut int, regime regulator.Regime, measured bool, kgNA bool) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("awakePeaks: NewEngine failed: " + err.Error())
	}
	minMu = 1.0
	for tick := 1; tick <= ticks; tick++ {
		e.Step()
		r := e.Regulator()
		peakN = maxF(peakN, r.N())
		peakU = maxF(peakU, r.Util())
		if out := e.ActionOutstanding(); out > maxOut {
			maxOut = out
		}
		// μ>0 is a STEADY-STATE baseline (μ=0 during the first-tick warmup transient by design), so
		// sample it on the every-100 cadence like the standing guards.
		if tick%100 == 0 {
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
	return peakN, peakU, minMu, maxOut, regime, measured, kgNA
}

// awakeQuality is the deterministic awake-stream quality summary for a config (the same dimensions the
// awake property test TestContinuousStreamDurableAndNotDegenerate asserts on the default config, lifted
// to a per-config measure): the real-thought count (alive, not died-out), the diversity ratio
// (unique/total real thoughts — not degenerate repetition), the act count (does-not-compulsively-act),
// and the distinct seed faculties planted (the portfolio's breadth — the point of dialling the count up).
type awakeQuality struct {
	realThoughts  int
	diversity     float64
	acts          int
	seedRoots     int // standing seed roots planted (conscious.seed_intent "-> ..." events)
	seedFaculties int // distinct faculties among the planted roots
	outreaches    int // proactive (unprompted) responses
}

// measureAwakeQuality runs an awake engine for `ticks` with a subscribed event log and returns the
// deterministic quality dimensions. seed=7 (deterministic).
func measureAwakeQuality(feat *config.HarnessConfig, ticks int) awakeQuality {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("measureAwakeQuality: NewEngine failed: " + err.Error())
	}
	var acts, outreaches int
	seedFaculties := map[string]struct{}{}
	var seedRoots int
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.Act:
			acts++
		case events.SeedIntent:
			if fac, ok := ev.Data["faculty"].(string); ok {
				// the per-root planted event carries a faculty (the portfolio-planted summary does not).
				seedFaculties[fac] = struct{}{}
				seedRoots++
			}
		case events.Respond:
			if pro, ok := ev.Data["proactive"].(bool); ok && pro {
				outreaches++
			}
		}
	})
	for i := 0; i < ticks; i++ {
		e.Step()
	}
	var real []types.Thought
	for _, tt := range e.Graph().History() {
		if tt.Source != types.METACOG {
			real = append(real, tt)
		}
	}
	uniq := map[string]struct{}{}
	for _, tt := range real {
		uniq[tt.Text] = struct{}{}
	}
	div := 0.0
	if len(real) > 0 {
		div = float64(len(uniq)) / float64(len(real))
	}
	return awakeQuality{
		realThoughts:  len(real),
		diversity:     div,
		acts:          acts,
		seedRoots:     seedRoots,
		seedFaculties: len(seedFaculties),
		outreaches:    outreaches,
	}
}

// TestB3CharacterizeAwakeQuality prints the awake-stream QUALITY table across the combined-config space
// (run with -v). Deterministic (test double, seed=7); verbose only — the standing quality guard is
// TestB3CombinedAwakeConfigDurable.
func TestB3CharacterizeAwakeQuality(t *testing.T) {
	const ticks = 60 // the property-test horizon (TestContinuousStreamDurableAndNotDegenerate uses 60)
	t.Logf("B3 awake-stream QUALITY characterization (TestBackend double, seed=7, %d ticks)", ticks)
	t.Logf("%-34s | real | divrs | acts | seedRoots | facult | outreach", "config")
	t.Logf("%s", "------------------------------------------------------------------------------------")
	for _, bc := range b3ConfigSpace() {
		q := measureAwakeQuality(featuresFor(bc), ticks)
		t.Logf("%-34s | %4d | %.2f  | %4d | %9d | %6d | %d",
			bc.name, q.realThoughts, q.diversity, q.acts, q.seedRoots, q.seedFaculties, q.outreaches)
	}
}

// TestB3CharacterizeAwakeConfigSpace prints the combined-config durability table (run with -v). It uses
// the EXACT gate the stability subcommand uses (CheckEngine over a continuous run) for the PASS/FAIL +
// regime verdict, and a parallel every-tick peak sampler for the worst-case n/U/dead-time. Verbose only;
// it asserts nothing about the verdict (the standing guard is TestB3CombinedAwakeConfigDurable).
func TestB3CharacterizeAwakeConfigSpace(t *testing.T) {
	const gateTicks = 24  // the stability subcommand's continuous-row horizon
	const longTicks = 600 // the extended-run horizon the C1 guard uses

	t.Logf("B3 awake config-space durability characterization (TestBackend double, seed=7)")
	t.Logf("portfolio: kernel=%d  full=%d  (mid=%d)", cognition.SeedKernelSize, cognition.SeedPortfolioSize(),
		(cognition.SeedKernelSize+cognition.SeedPortfolioSize())/2)
	t.Logf("%-34s | %-6s | %-22s | gmeas | KgNA | %s", "config", "verdict", "regime (24t gate)", "every-tick peaks (600t)")
	t.Logf("%s", "-----------------------------------------------------------------------------------------------------------------------")

	for _, bc := range b3ConfigSpace() {
		feat := featuresFor(bc)
		// the gate (24-tick continuous row, the exact subcommand path).
		e := runAwakeWithFeatures(feat, gateTicks)
		rep := CheckEngine(e, "B3:"+bc.name, "continuous")
		// the extended-run every-tick peaks (the worst case the sparse gate could miss).
		pN, pU, mMu, mOut, rg, gm, _ := awakePeaks(featuresFor(bc), longTicks)

		verdict := "PASS"
		if !rep.OK() {
			verdict = "FAIL"
		}
		gmeas := "prior"
		if rep.GainMeasured {
			gmeas = "ident"
		}
		kgNA := "no"
		for _, c := range rep.Checks {
			if c.Name == "0<K*g<2 (regulator stable)" && c.OK {
				// OK either because PASS-band or because NA(vacuous); distinguish via regime
				if rep.Regime == regulator.RegimeSaturatedBounded.String() || rep.Regime == regulator.RegimeInsufficientLoop.String() {
					kgNA = "yes"
				}
			}
		}
		_ = rg
		_ = gm
		t.Logf("%-34s | %-6s | %-22s | %-5s | %-4s | peak_n=%.2f peak_U=%.2f min_mu=%.2f max_out=%d (regime=%s)",
			bc.name, verdict, rep.Regime, gmeas, kgNA, pN, pU, mMu, mOut, rg.String())
		// dump the per-check detail under any FAIL so the diagnosis is in the log.
		if !rep.OK() {
			t.Logf("    %s", rep.Format())
		}
	}
	_ = fmt.Sprintf
}

// TestB3CombinedAwakeConfigDurable is the B3 STANDING durability-gate regression guard for the COMBINED
// awake config (forest + drive_agenda + seed_intents), across the seed sizing space (kernel-of-3 / mid /
// full portfolio). It is the load-bearing PASS/FAIL guard the characterization tables above visualise:
// every combined config must
//   - PASS the sound C0a/C0b durability gate (CheckEngine over a continuous run) — the same path the
//     stability subcommand uses, AND
//   - be in the BENIGN saturated-bounded regime (θ pinned at the FLOOR, loop open, K·g vacuously NA),
//     never a ThetaMax control-loss rail, AND with the gain on the PRIOR fallback (an open-loop K·g must
//     not be trusted as a measured check), AND
//   - hold the five conditions at EVERY-TICK peak over a 600-tick extended run (the C1 lesson: n's EMA
//     spikes between every-100 samples), with a regression bound on peak_n.
//
// This extends the C1 guard (TestSeedIntentDurabilityHoldsOnTheAwakeForest, which covered seed_intents
// ALONE at the extremes) to the COMBINED plant + a mid sizing the C1 guard did not cover. Offline test
// double, seed=7 (deterministic).
func TestB3CombinedAwakeConfigDurable(t *testing.T) {
	mid := (cognition.SeedKernelSize + cognition.SeedPortfolioSize()) / 2
	for _, seedCount := range []int{cognition.SeedKernelSize, mid, cognition.SeedPortfolioSize()} {
		seedCount := seedCount
		t.Run("combined,seed="+itoa(seedCount), func(t *testing.T) {
			bc := b3Config{name: "combined", forest: true, driveAgenda: true, seedIntents: true, seedCount: seedCount}
			feat := featuresFor(bc)

			// (1) the sound gate (24-tick continuous row, the exact subcommand path) PASSES and is benign.
			rep := CheckEngine(runAwakeWithFeatures(feat, 24), "B3 combined", "continuous")
			if !rep.OK() {
				t.Fatalf("combined awake config (seed=%d) failed the durability gate:\n%s", seedCount, rep.Format())
			}
			if rep.Regime != regulator.RegimeSaturatedBounded.String() {
				t.Fatalf("combined awake config (seed=%d) regime = %q, want saturated-bounded (θ pinned at the floor => benign open loop)",
					seedCount, rep.Regime)
			}
			if rep.GainMeasured {
				t.Fatalf("combined awake config (seed=%d) gain must be the PRIOR fallback (loop open), not identified — "+
					"an open-loop K·g must never be trusted as a real check", seedCount)
			}

			// (2) every-tick peaks over the extended (600-tick) run hold the five conditions, and the clamp
			//     stays at the floor ("min", benign) the whole way — never a ThetaMax (control-loss) rail.
			pN, pU, mMu, mOut, rg, measured, kgNA := awakePeaks(featuresFor(bc), 600)
			if !(pN < 1.0 && pU <= 1.0 && mMu > 0.0 && mOut <= cognition.MaxParWidth) {
				t.Fatalf("combined awake config (seed=%d) not durable over 600t: peak_n=%.2f peak_U=%.2f min_mu=%.2f max_out=%d",
					seedCount, pN, pU, mMu, mOut)
			}
			if rg != regulator.RegimeSaturatedBounded {
				t.Fatalf("combined awake config (seed=%d) end-regime = %v, want saturated-bounded", seedCount, rg)
			}
			if measured || !kgNA {
				t.Fatalf("combined awake config (seed=%d) end K·g must be NA on the prior fallback (saturated/open-loop)", seedCount)
			}
			// regression bound on the recursive fork ratio n. MEASURED 2026-06-18 (every-tick peak, seed=7,
			// TestBackend): peak_n ∈ [0.38, 0.40] for the combined config at every sizing (the standing roots
			// + drive-agenda mint raise μ but do NOT excite the fork ratio). The bound is 0.50 — ~2.5x headroom
			// to the n=1 cliff, the same bound the C1 seed-intent guard carries. Raise only with a re-measure.
			if pN > 0.50 {
				t.Fatalf("combined awake config (seed=%d) excited branching past the recorded bound: peak_n=%.2f > 0.50 (measured ~0.39)",
					seedCount, pN)
			}

			// (3) the awake stream stays ALIVE, DIVERSE, and does NOT compulsively act (the awake quality
			//     property, lifted per-config). The seed portfolio gives the loop MORE to think about, so the
			//     combined stream must be at least as alive as the property-test floor.
			q := measureAwakeQuality(featuresFor(bc), 60)
			if q.realThoughts <= 20 {
				t.Fatalf("combined awake config (seed=%d) stream died out: %d real thoughts", seedCount, q.realThoughts)
			}
			if q.diversity <= 0.30 {
				t.Fatalf("combined awake config (seed=%d) stream degenerated into repetition: diversity=%.2f", seedCount, q.diversity)
			}
			if q.acts > 3 {
				t.Fatalf("combined awake config (seed=%d) compulsively acted (%dx) instead of moving on", seedCount, q.acts)
			}
			// faculty breadth: the seeded portfolio must plant exactly its sized prefix of standing roots, and
			// the FULL portfolio must keep ALL faculties alive (the completeness property — the point of
			// dialling the count up to the two-digit portfolio). The faculty count is derived from the
			// portfolio (now SIX faculties incl. the Validative root).
			if q.seedRoots != seedCount {
				t.Fatalf("combined awake config (seed=%d) planted %d standing roots, want %d", seedCount, q.seedRoots, seedCount)
			}
			if seedCount == cognition.SeedPortfolioSize() && q.seedFaculties != cognition.SeedFacultyCount() {
				t.Fatalf("the FULL seed portfolio must keep all %d faculties alive, got %d",
					cognition.SeedFacultyCount(), q.seedFaculties)
			}
		})
	}
}
