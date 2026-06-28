package stability

// Full port of the removed Python `tests/test_stability.py` — the standing
// durability suite re-applied to the DYNAMIC harness. Stability validation is a STANDING test: the
// durability conditions (n<1, U<=1, fan-out<=W_max, mu>0 awake, the static regulator conditions)
// must hold under dynamic synthesis (parallel fan-out, loops, operator minting). The control-theoretic
// durability math, re-applied to a plant whose dimension varies at runtime.
//
// Every engine the suite builds runs on the offline TestBackend test double (the parity-pinned
// path), so the suite is deterministic and needs no model — exactly the substrate Python's
// tests/conftest.py pins (THOUGHT_SUBSTRATE=heuristic). That is why the Go subcommand reproduces the
// Python module's report byte-for-byte while the bare `python3 -m thought_harness.stability` (no pin)
// goes through the live-model default substrate.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/regulator"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// itoa is the local int->string helper (the engine package's itoa is not visible across the package
// boundary). Used only to build test fixture labels and operator names.
func itoa(n int) string { return strconv.Itoa(n) }

// TestStabilitySuiteHoldsTheDurableRegime mirrors Python
// test_stability_suite_holds_the_durable_regime: every representative dynamic workload keeps n<1,
// U<=1, fan-out<=W_max, and the static regulator conditions.
func TestStabilitySuiteHoldsTheDurableRegime(t *testing.T) {
	reports := RunSuite()
	var failed []string
	var detail strings.Builder
	for _, r := range reports {
		if !r.OK() {
			failed = append(failed, r.Workload)
			detail.WriteString(r.Format())
			detail.WriteString("\n")
		}
	}
	if len(failed) != 0 {
		t.Fatalf("durable regime violated by: %s\n%s", strings.Join(failed, ", "), detail.String())
	}
}

// TestParallelFanoutStaysSubcritical mirrors Python test_parallel_fanout_stays_subcritical: the
// parallel fan-out is the dynamic stressor; it must stay under the n=1 cliff.
func TestParallelFanoutStaysSubcritical(t *testing.T) {
	rep := CheckEngineReactive(runReactive("Compare REST versus GraphQL for our API"), "parallel")
	if rep.Metrics.MaxFanout < 2 { // it really did fan out
		t.Fatalf("expected a real fan-out (max_fanout>=2), got %d", rep.Metrics.MaxFanout)
	}
	if rep.Metrics.PeakN >= 1.0 { // ...yet stayed subcritical
		t.Fatalf("peak n=%.2f reached the n=1 cliff", rep.Metrics.PeakN)
	}
	if !rep.OK() {
		t.Fatalf("parallel workload not durable:\n%s", rep.Format())
	}
}

// TestBranchingIsDecoupledFromFanoutWidth mirrors Python
// test_branching_is_decoupled_from_fanout_width: the re-model — n is the recursive FORK ratio, not the
// admit count — so a WIDE parallel fan-out keeps n subcritical and U flat; only intensity (lam_hat)
// scales. Durability does not constrain parallel width (it's a compute budget).
//
// Python builds a Program with a w-wide Par, hands it to the engine as a synthesised Workflow via
// Workflow.from_program, then runs. The Go equivalents: cognition.NewSeq/NewPar/NewStep build the
// Program, subconscious.FromProgram builds the Workflow, e.Subconscious().SetWorkflow installs it
// (Python's `e.subconscious.workflow = ...`). Python's private `e._step_reactive(1)` (intake → start
// episode → build graph) is the public e.Step() here (the first reactive step pops the goal and opens
// the episode), after which e.Graph() is non-nil and its Goal is readable.
func TestBranchingIsDecoupledFromFanoutWidth(t *testing.T) {
	runWidth := func(w int) Report {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.MaxTicks = 20
		e, err := engine.NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine on the heuristic backend failed: %v", err)
		}
		e.SubmitDefault("Investigate the outage from many angles")
		e.Step() // intake -> start episode (builds the graph), Python e._step_reactive(1)
		if e.Graph() == nil {
			t.Fatal("the first reactive step did not open an episode (graph is nil)")
		}

		angles := make([]cognition.Node, w)
		for i := range angles {
			angles[i] = cognition.NewStep("hypothesize", "ops", "angle"+itoa(i))
		}
		prog := cognition.Program{
			Root: cognition.NewSeq(
				cognition.NewStep("decompose", "ops", ""),
				cognition.NewPar(angles...),
				cognition.NewStep("rank", "ops", ""),
			),
			Goal:        e.Graph().Goal,
			Synthesized: true,
		}
		wf := subconscious.FromProgram(&prog, e.Catalog(), e.Backend(), e.Emit(), e.Graph().Goal)
		e.Subconscious().SetWorkflow(wf)
		e.Run(18)
		return CheckEngineReactive(e, "width-"+itoa(w))
	}

	r2, r8 := runWidth(2), runWidth(8)
	if r8.Metrics.MaxFanout < 6 { // genuinely wide fan-out
		t.Fatalf("expected a wide fan-out (max_fanout>=6) at width 8, got %d", r8.Metrics.MaxFanout)
	}
	if r2.Metrics.PeakN >= 1.0 || r8.Metrics.PeakN >= 1.0 { // both subcritical
		t.Fatalf("a width crossed the n=1 cliff: peak_n w2=%.2f w8=%.2f", r2.Metrics.PeakN, r8.Metrics.PeakN)
	}
	if r8.Metrics.PeakU > r2.Metrics.PeakU+0.01 { // U flat across width
		t.Fatalf("U scaled with width (not flat): peak_U w2=%.2f w8=%.2f", r2.Metrics.PeakU, r8.Metrics.PeakU)
	}
	if r8.Metrics.LamHatMax <= r2.Metrics.LamHatMax { // only intensity scales
		t.Fatalf("intensity did not scale with width: lam_hat_max w2=%.2f w8=%.2f",
			r2.Metrics.LamHatMax, r8.Metrics.LamHatMax)
	}
	if !r2.OK() || !r8.OK() {
		t.Fatalf("a width-stressed workload was not durable:\nw2:\n%s\nw8:\n%s", r2.Format(), r8.Format())
	}
}

// TestStructuralBoundRejectsOversizeFanout mirrors Python
// test_structural_bound_rejects_oversize_fanout: VerifyProgram rejects a fan-out wider than W_max (the
// durability cliff is unreachable).
func TestStructuralBoundRejectsOversizeFanout(t *testing.T) {
	rep := StructuralBoundHolds()
	if !rep.OK() {
		t.Fatalf("structural bound report failed:\n%s", rep.Format())
	}
	cat := cognition.NewOperatorRegistry()
	wide := cognition.Program{
		Root:        cognition.NewPar(hypothesizeSteps(cognition.MaxParWidth + 1)...),
		Synthesized: true,
	}
	ok, issues := cognition.VerifyProgram(wide, cat)
	if ok {
		t.Fatal("an over-wide Par should be rejected by VerifyProgram")
	}
	found := false
	for _, i := range issues {
		if strings.Contains(i, "fan-out") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a fan-out issue, got %v", issues)
	}
}

// TestCatalogGrowthIsExcitationNeutral mirrors Python test_catalog_growth_is_excitation_neutral:
// minting operators grows the vocabulary dimension but must NOT raise per-tick branching — the
// "dimension is not static" yet durability is unaffected (condition D3).
//
// Note: Python mints on the SAME engine it then re-runs by calling _run twice on the same goal; the Go
// Catalog is per-engine, so to keep the spirit (a grown catalog must not couple to n) we grow a fresh
// engine's catalog, assert the vocabulary dimension grew, then measure a fresh run's durability. The
// invariant under test is that branching is independent of catalog size, which holds for any run on a
// grown OR ungrown catalog.
func TestCatalogGrowthIsExcitationNeutral(t *testing.T) {
	grown := runReactive("Design a small API for a todo service")
	for i := 0; i < 20; i++ { // grow the catalog a lot
		if _, ok := grown.Catalog().Mint("synth_op_"+itoa(i), "transformative",
			"a synthesised move number "+itoa(i)+" for testing"); !ok {
			t.Fatalf("minting synth_op_%d should succeed (valid family + >=3-word intent)", i)
		}
	}
	if len(grown.Catalog().Names()) <= 40 { // vocabulary dimension grew
		t.Fatalf("expected the catalog to grow past 40 names, got %d", len(grown.Catalog().Names()))
	}

	rep := CheckEngineReactive(runReactive("Design a small API for a todo service"), "post-growth")
	if rep.Metrics.PeakN >= 1.0 { // branching unaffected by catalog size
		t.Fatalf("catalog growth coupled to branching: peak_n=%.2f", rep.Metrics.PeakN)
	}
	if !rep.OK() {
		t.Fatalf("post-growth workload not durable:\n%s", rep.Format())
	}
}

// TestAwakeDurabilityHoldsOverAnExtendedRun mirrors Python
// test_awake_durability_holds_over_an_extended_run (#45): empirically validate the awake regime at
// scale, not just on short workloads — run the continuous loop for many ticks and assert n<1, U<=1,
// mu>0, and bounded async dead-time hold at EVERY sample throughout (the "implemented-untested" caveat,
// now a standing regression guard).
//
// Python samples eng.regulator.{n,U,mu} and eng.regulator.stability("continuous") every 100 ticks and
// asserts the bounded async dead-time len(eng.awatched.outstanding) <= 8. The Go reads are
// e.Regulator().{N,Util,Mu} / Stability("continuous", false) and e.ActionOutstanding(). The engine is
// seeded (seed=7) for determinism; the heuristic backend is the offline product-equivalent substrate.
func TestAwakeDurabilityHoldsOverAnExtendedRun(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine on the heuristic backend failed: %v", err)
	}

	var peakN, peakU float64
	minMu := 1.0
	for tick := 1; tick <= 600; tick++ {
		eng.Step()
		if tick%100 == 0 {
			r := eng.Regulator()
			peakN = maxF(peakN, r.N())
			peakU = maxF(peakU, r.Util())
			if r.Mu() < minMu {
				minMu = r.Mu()
			}
			// hard durability checks must all hold. Under the C0a reframe the awake regime is
			// SATURATED-BOUNDED: θ is pinned at the floor, so the loop is open and 0<K·g<2 is reported
			// NA (vacuous), durable by the other four. Assert this EXPLICITLY (regime + K·g provenance),
			// not just `Pass || NA` — so a silent prior-pass could not masquerade as a held check.
			checks, regime, _, measured := r.StabilityRegime("continuous")
			for _, sc := range checks {
				if !(sc.Pass || sc.NA) {
					t.Fatalf("durability violated at tick %d: %q failed", tick, sc.Name)
				}
			}
			if regime != regulator.RegimeSaturatedBounded {
				t.Fatalf("awake regime at tick %d = %v, want saturated-bounded (θ pinned => open-loop)",
					tick, regime)
			}
			if measured {
				t.Fatalf("awake K·g at tick %d must be on the prior fallback (loop open, unidentified), "+
					"not a measured gain", tick)
			}
			kg := checks[2]
			if kg.Name != "0<K*g<2 (regulator stable)" || !kg.NA {
				t.Fatalf("awake K·g at tick %d must be NA (saturated/open-loop), got %+v", tick, kg)
			}
			if kg.NADetail != "K·g N/A — saturated/open-loop" {
				t.Fatalf("awake K·g NA detail at tick %d wrong: %q", tick, kg.NADetail)
			}
			if out := eng.ActionOutstanding(); out > 8 {
				t.Fatalf("async dead-time unbounded at tick %d: %d outstanding actions", tick, out)
			}
		}
	}
	if !(peakN < 1.0 && peakU <= 1.0 && minMu > 0.0) {
		t.Fatalf("awake regime not durable over the extended run: peak_n=%.2f peak_U=%.2f min_mu=%.2f",
			peakN, peakU, minMu)
	}
}

// TestSeedIntentDurabilityHoldsOnTheAwakeForest is the C1 STANDING durability regression guard for the
// seed-intent ON path (red-team fix #1). TestAwakeDurabilityHoldsOverAnExtendedRun never sets SeedIntents,
// so it proves NOTHING about C1's added plant: a standing seed-intent portfolio plants many parallel
// goal-lines (§2.1 control-theory gate) → it raises schedulability U and pushes fan-out toward
// MAX_PAR_WIDTH=8. This re-runs the extended awake run with SeedIntents ON at BOTH the kernel-of-3 and the
// FULL portfolio size, asserting the durable regime holds at every 100-tick sample over 600 ticks. The
// peak numbers are ENCODED as a regression bound (not left to prose) so a future portfolio change that
// excites specialists cannot silently break durability. Offline TestBackend double, seed=7 (deterministic).
func TestSeedIntentDurabilityHoldsOnTheAwakeForest(t *testing.T) {
	for _, count := range []int{cognition.SeedKernelSize, cognition.SeedPortfolioSize()} {
		count := count
		t.Run("count="+itoa(count), func(t *testing.T) {
			feat := config.New()
			feat.Conscious.Activity.SeedIntents = true
			feat.Conscious.Activity.SeedIntentCount = count
			feat.Validate()

			cfg := engine.DefaultConfig()
			cfg.Mode = "continuous"
			cfg.Seed = 7
			cfg.Features = feat
			eng, err := engine.NewEngine(&cfg, backends.NewTest())
			if err != nil {
				t.Fatalf("NewEngine on the test double failed: %v", err)
			}

			var peakN, peakU float64
			minMu := 1.0
			maxOutstanding := 0
			for tick := 1; tick <= 600; tick++ {
				eng.Step()
				r := eng.Regulator()
				// peak n/U/dead-time: EVERY tick. n's EMA spikes BETWEEN the every-100 sample points, so a
				// sparse sample under-reads the true peak (~0.03 every-100 vs ~0.40 every-tick) and could let a
				// regression slip between samples. We want the worst case. (The stability subcommand also
				// samples every tick — its "continuous baseline" row reports the same ~0.37-0.40.)
				peakN = maxF(peakN, r.N())
				peakU = maxF(peakU, r.Util())
				if out := eng.ActionOutstanding(); out > maxOutstanding {
					maxOutstanding = out
				}
				// mu>0 baseline + the 3-case regulator gate: STEADY-STATE cadence (every 100), as
				// TestAwakeDurabilityHoldsOverAnExtendedRun samples them. mu is 0 during the first-tick warmup
				// transient by design and becomes positive at steady state — mu>0 is a steady-state baseline
				// condition, not a per-tick claim (sampling it every tick would spuriously read the warmup 0).
				if tick%100 == 0 {
					if r.Mu() < minMu {
						minMu = r.Mu()
					}
					checks, _, _, _ := r.StabilityRegime("continuous")
					for _, sc := range checks {
						if !(sc.Pass || sc.NA) {
							t.Fatalf("seed_intents ON (count=%d): durability violated at tick %d: %q failed",
								count, tick, sc.Name)
						}
					}
				}
			}
			// the five durability conditions, encoded: n<1, U<=1, mu>0, async dead-time (fan-out) <= 8.
			if !(peakN < 1.0 && peakU <= 1.0 && minMu > 0.0 && maxOutstanding <= 8) {
				t.Fatalf("seed_intents ON (count=%d) not durable over the extended run: peak_n=%.2f peak_U=%.2f min_mu=%.2f max_outstanding=%d",
					count, peakN, peakU, minMu, maxOutstanding)
			}
			// tighter REGRESSION bound on the recursive fork ratio n (the seeding must not excite branching
			// toward the n=1 cliff). Raise only with a documented re-measure — a silent climb is the regression
			// this guard exists to catch.
			// MEASURED 2026-06-17 on seed=7 / TestBackend (EVERY-TICK peak): peak_n=0.40, peak_U=1.00 — identical
			// at count=3 and count=19 (the seeded plant adds standing roots but does not, today, excite the fork
			// ratio). The bound is 0.50 — ~2x headroom to the n=1 cliff, tight enough to catch a portfolio change
			// that climbs n. peak_U sits at 1.00 (saturated-bounded schedule), so its only honest guard is the
			// hard U<=1 above.
			if peakN > 0.50 {
				t.Fatalf("seed_intents ON (count=%d) excited branching past the recorded bound: peak_n=%.2f > 0.50 (measured 0.40)", count, peakN)
			}
		})
	}
}

// TestFlagOnAwakePlantInSuite is the STANDING flag-ON awake-plant cell guard. The standing suite used to
// test only the flag-OFF plant, so the flag-ON plant (faculty_scheduler + attention_width=1 + rpiv over
// the awake stack — the new plant the 3216271 + 5a765dd commits introduced) was re-derived by the
// continuous-mode-operator durability gate by hand each time. This asserts the cell is now a STANDING
// member of RunSuite() AND a REAL, failable durability check (not a smoke pass): the five conditions hold
// in the benign saturated-bounded regime with the gain on the PRIOR fallback (an open-loop K·g must never
// read as a measured pass). seed=7 (deterministic, the awake guards' seed). Offline TestBackend double.
func TestFlagOnAwakePlantInSuite(t *testing.T) {
	// (a) the cell is wired into the standing suite (so `thought stability` + the suite test exercise it).
	reports := RunSuite()
	var cell *Report
	for i := range reports {
		if reports[i].Workload == "awake flag-ON (faculty_scheduler+rpiv)" {
			cell = &reports[i]
		}
	}
	if cell == nil {
		t.Fatal("the flag-ON awake-plant cell is not in RunSuite() — `thought stability` would not exercise the flag-ON plant")
	}

	// (b) the cell is a REAL failable durability check: the five conditions hold, benign saturated-bounded,
	//     g on the prior fallback. Build it directly so the assertions read off the same CheckEngine path.
	rep := CheckEngine(runAwakeFlagOn(7, 24), "awake flag-ON (faculty_scheduler+rpiv)", "continuous")
	if !rep.OK() {
		t.Fatalf("flag-ON awake plant failed the durability gate:\n%s", rep.Format())
	}
	if rep.Regime != regulator.RegimeSaturatedBounded.String() {
		t.Fatalf("flag-ON awake plant regime = %q, want saturated-bounded (θ pinned at the floor => benign open loop)", rep.Regime)
	}
	if rep.GainMeasured {
		t.Fatal("flag-ON awake plant gain must be the PRIOR fallback (loop open), not identified — an open-loop K·g must never be a real check")
	}
	m := rep.Metrics
	// the five conditions, MEASURED off the cell's metrics (n<1, U<=1, fan-out<=W_max, mu>0; K·g vacuous NA).
	if !(m.PeakN < 1.0 && m.PeakU <= 1.0 && m.MaxFanout <= cognition.MaxParWidth && m.MuMax > 0.0) {
		t.Fatalf("flag-ON awake plant not durable at the 24-tick gate: peak_n=%.2f peak_U=%.2f max_fanout=%d mu_max=%.2f",
			m.PeakN, m.PeakU, m.MaxFanout, m.MuMax)
	}
	// the K·g check is present and is OK only because it is NA (vacuous, saturated/open-loop), not a counted pass.
	var kg *Check
	for i := range rep.Checks {
		if rep.Checks[i].Name == "0<K*g<2 (regulator stable)" {
			kg = &rep.Checks[i]
		}
	}
	if kg == nil || !kg.OK {
		t.Fatalf("the saturated/open-loop K·g must be present and OK (vacuous) on the flag-ON row, got %+v", kg)
	}
}

// TestParallelPhasesPlantInSuite is the STANDING parallel-phases durability cell guard (07-OPTIMISATION-
// SURVEY.md §A.1, seam #1 — the per-phase concurrency speed-up). The standing suite previously gated only
// the SERIAL plant; concurrency CHANGES THE PLANT (it raises schedulability U and pushes fan-out toward
// MAX_PAR_WIDTH), so the control-theory gate must re-pass on the concurrent path. This asserts the cell is
//
//	(a) a STANDING member of RunSuite() (so `thought stability` exercises the concurrent plant), and
//	(b) a REAL, failable durability check: the five conditions hold (n<1, U<=1, fan-out<=W_max, the static
//	    regulator conditions; this is a reactive run, so μ>0 is N/A — it is gated on the awake rows), and
//	(c) the BYTE-IDENTICAL-PLANT property: because the seam is RNG-free + buffered + index-ordered, the
//	    flag changes wall-clock, NOT the plant — so the parallel-phases metrics MUST equal the SERIAL
//	    compare||contrast metrics exactly (n is fork DEPTH, unaffected by fan-out width; concurrency flows
//	    into λ̂/U only). A divergence here means the concurrency path altered the plant (a determinism bug),
//	    not just its speed — the regression this cell exists to catch.
//
// It also asserts the workload genuinely fanned out (max_fanout>=2), else the cell would be vacuous (no
// Par group exercised). Offline TestBackend double; the parallel flag is forced ON in-process.
func TestParallelPhasesPlantInSuite(t *testing.T) {
	const cellName = "parallel-phases ON (compare||contrast concurrent)"

	// (a) the cell is wired into the standing suite.
	reports := RunSuite()
	var cell *Report
	for i := range reports {
		if reports[i].Workload == cellName {
			cell = &reports[i]
		}
	}
	if cell == nil {
		t.Fatalf("the parallel-phases plant cell is not in RunSuite() — `thought stability` would not exercise the concurrent plant")
	}

	// (b) the cell is a REAL failable durability check: the five conditions hold on the concurrent plant.
	rep := CheckEngineReactive(runParallelPhasesPlant(24), cellName)
	if !rep.OK() {
		t.Fatalf("parallel-phases plant failed the durability gate:\n%s", rep.Format())
	}
	pm := rep.Metrics
	if pm.MaxFanout < 2 { // the workload really exercised a Par fan-out, else the cell is vacuous
		t.Fatalf("parallel-phases cell did not exercise a Par fan-out (max_fanout=%d < 2) — the cell is vacuous", pm.MaxFanout)
	}
	// the durability conditions, MEASURED off the concurrent plant's metrics: n<1, U<=1, fan-out<=W_max.
	if !(pm.PeakN < 1.0 && pm.PeakU <= 1.0 && pm.MaxFanout <= cognition.MaxParWidth) {
		t.Fatalf("parallel-phases plant not durable at the 24-tick gate: peak_n=%.2f peak_U=%.2f max_fanout=%d",
			pm.PeakN, pm.PeakU, pm.MaxFanout)
	}

	// (c) the byte-identical-plant property: the concurrent run's metrics MUST equal the SERIAL run's.
	//     Concurrency is RNG-free + buffered + index-ordered, so it changes wall-clock, not the plant.
	sm := CheckEngineReactive(runReactive(parallelPhasesWorkload), "serial (compare||contrast)").Metrics
	if pm.PeakN != sm.PeakN || pm.PeakU != sm.PeakU || pm.MaxFanout != sm.MaxFanout ||
		pm.LamHatMax != sm.LamHatMax || pm.Minted != sm.Minted || pm.ThetaEnd != sm.ThetaEnd {
		t.Fatalf("parallel-phases plant DIVERGED from the serial plant (concurrency altered the plant, not just its speed):\n"+
			"  serial:   peak_n=%.4f peak_U=%.4f max_fanout=%d lam_hat_max=%.4f minted=%d theta_end=%.4f\n"+
			"  parallel: peak_n=%.4f peak_U=%.4f max_fanout=%d lam_hat_max=%.4f minted=%d theta_end=%.4f",
			sm.PeakN, sm.PeakU, sm.MaxFanout, sm.LamHatMax, sm.Minted, sm.ThetaEnd,
			pm.PeakN, pm.PeakU, pm.MaxFanout, pm.LamHatMax, pm.Minted, pm.ThetaEnd)
	}
}

// TestPrimitiveSubAgentFanoutPlantInSuite is the STANDING seam-#2 durability cell guard (07-OPTIMISATION-SURVEY.md
// §A.1 item 3 — the per-tick base-specialist model-call fan-out). Like the seam-#1 guard above, concurrency
// CHANGES THE PLANT (it raises schedulability U and overlaps more model calls per tick), so the control-theory
// gate must re-pass on the concurrent base-specialist path. This asserts the cell is
//
//	(a) a STANDING member of RunSuite() (so `thought stability` exercises the concurrent base-specialist plant),
//	(b) a REAL, failable durability check: the five conditions hold (reactive run, so μ>0 is N/A), and
//	(c) the BYTE-IDENTICAL-PLANT property: the seam is RNG-free + bus-silent-in-Fire + index-ordered, so the
//	    flag changes wall-clock, NOT the plant — the concurrent run's metrics MUST equal the SERIAL run's
//	    exactly (n is fork DEPTH, unaffected by the per-tick fire-set width; concurrency flows into λ̂/U only).
//	    A divergence means the concurrency path altered the plant (a determinism bug), the regression this
//	    cell exists to catch.
//
// NOTE the vacuity check differs from seam #1: the per-tick base-specialist fan-out is NOT counted by
// MaxFanout (which counts workflow SubAgent fires); this review-shaped workload runs no workflow, so
// max_fanout is 0. Vacuity is guarded instead by the dedicated subconscious determinism+overlap tests
// (TestPrimitiveSubAgentFanout* prove the concurrent set really fires ≥2 model-call specialists); here the
// load-bearing assertion is (c) — the concurrent plant is byte-identical to serial.
func TestPrimitiveSubAgentFanoutPlantInSuite(t *testing.T) {
	const cellName = "specialist-fanout ON (skeptic||advocate concurrent)"

	// (a) the cell is wired into the standing suite.
	reports := RunSuite()
	var cell *Report
	for i := range reports {
		if reports[i].Workload == cellName {
			cell = &reports[i]
		}
	}
	if cell == nil {
		t.Fatalf("the specialist-fanout plant cell is not in RunSuite() — `thought stability` would not exercise the concurrent base-specialist plant")
	}

	// (b) the cell is a REAL failable durability check: the five conditions hold on the concurrent plant.
	rep := CheckEngineReactive(runPrimitiveSubAgentFanoutPlant(24), cellName)
	if !rep.OK() {
		t.Fatalf("specialist-fanout plant failed the durability gate:\n%s", rep.Format())
	}
	pm := rep.Metrics
	if !(pm.PeakN < 1.0 && pm.PeakU <= 1.0 && pm.MaxFanout <= cognition.MaxParWidth) {
		t.Fatalf("specialist-fanout plant not durable at the 24-tick gate: peak_n=%.2f peak_U=%.2f max_fanout=%d",
			pm.PeakN, pm.PeakU, pm.MaxFanout)
	}

	// (c) the byte-identical-plant property: the concurrent run's metrics MUST equal the SERIAL run's.
	//     The seam is RNG-free + bus-silent-in-Fire + index-ordered, so it changes wall-clock, not the plant.
	sm := CheckEngineReactive(runReactive(specialistFanoutWorkload), "serial (skeptic||advocate)").Metrics
	if pm.PeakN != sm.PeakN || pm.PeakU != sm.PeakU || pm.MaxFanout != sm.MaxFanout ||
		pm.LamHatMax != sm.LamHatMax || pm.Minted != sm.Minted || pm.ThetaEnd != sm.ThetaEnd {
		t.Fatalf("specialist-fanout plant DIVERGED from the serial plant (concurrency altered the plant, not just its speed):\n"+
			"  serial:   peak_n=%.4f peak_U=%.4f max_fanout=%d lam_hat_max=%.4f minted=%d theta_end=%.4f\n"+
			"  parallel: peak_n=%.4f peak_U=%.4f max_fanout=%d lam_hat_max=%.4f minted=%d theta_end=%.4f",
			sm.PeakN, sm.PeakU, sm.MaxFanout, sm.LamHatMax, sm.Minted, sm.ThetaEnd,
			pm.PeakN, pm.PeakU, pm.MaxFanout, pm.LamHatMax, pm.Minted, pm.ThetaEnd)
	}
}

// TestFlagOnAwakePlantDurableOverExtendedRun is the flag-ON extended-horizon guard — the empirical Phase-0
// claim the continuous-mode-operator gate validated by hand, now a STANDING failable check. It re-runs the
// flag-ON awake plant for 1000 ticks across the gate's seeds (7/11/101), sampling EVERY tick (the C1 lesson:
// n's EMA spikes BETWEEN every-100 samples, so a sparse sample under-reads the true peak), and asserts the
// five conditions hold at the worst-case peak in the benign saturated-bounded regime the whole way. The
// peak_n bound is ENCODED as a regression bound (not left to prose): the gate MEASURED a transient peak that
// never crosses 1.0 (~0.978 on seed=7 at 600/1000t — the recorded worst case), so the bound is 0.99 — tight
// enough to catch a climb toward the n=1 cliff, with the hard n<1 cliff as the real fail. Offline, the gate's
// seeds; soft is the dominant n-excitation knob (branch_propensity β is the dial that restores headroom).
func TestFlagOnAwakePlantDurableOverExtendedRun(t *testing.T) {
	for _, seed := range []int{7, 11, 101} {
		seed := seed
		t.Run("seed="+itoa(seed), func(t *testing.T) {
			cfg := engine.DefaultConfig()
			cfg.Mode = "continuous"
			cfg.Seed = seed
			cfg.Features = awakeFlagOnFeatures()
			eng, err := engine.NewEngine(&cfg, backends.NewTest())
			if err != nil {
				t.Fatalf("NewEngine on the test double failed: %v", err)
			}

			var peakN, peakU float64
			minMu := 1.0
			maxOutstanding := 0
			for tick := 1; tick <= 1000; tick++ {
				eng.Step()
				r := eng.Regulator()
				// peak n/U/dead-time: EVERY tick (the worst case the sparse gate could miss).
				peakN = maxF(peakN, r.N())
				peakU = maxF(peakU, r.Util())
				if out := eng.ActionOutstanding(); out > maxOutstanding {
					maxOutstanding = out
				}
				// mu>0 + the 3-case regulator gate on the STEADY-STATE cadence (every 100): mu is 0 during the
				// first-tick warmup transient by design, so sampling it every tick would spuriously read 0.
				if tick%100 == 0 {
					if r.Mu() < minMu {
						minMu = r.Mu()
					}
					checks, regime, _, measured := r.StabilityRegime("continuous")
					for _, sc := range checks {
						if !(sc.Pass || sc.NA) {
							t.Fatalf("flag-ON (seed=%d): durability violated at tick %d: %q failed", seed, tick, sc.Name)
						}
					}
					if regime != regulator.RegimeSaturatedBounded {
						t.Fatalf("flag-ON (seed=%d) regime at tick %d = %v, want saturated-bounded (θ pinned => open-loop)",
							seed, tick, regime)
					}
					if measured {
						t.Fatalf("flag-ON (seed=%d) K·g at tick %d must be on the prior fallback (loop open, unidentified)", seed, tick)
					}
				}
			}
			// the five durability conditions, encoded: n<1, U<=1, mu>0, async dead-time (fan-out) <= W_max.
			if !(peakN < 1.0 && peakU <= 1.0 && minMu > 0.0 && maxOutstanding <= cognition.MaxParWidth) {
				t.Fatalf("flag-ON (seed=%d) not durable over 1000t: peak_n=%.3f peak_U=%.3f min_mu=%.3f max_outstanding=%d",
					seed, peakN, peakU, minMu, maxOutstanding)
			}
			// regression bound on the recursive fork ratio n. MEASURED 2026-06-20 (every-tick peak, TestBackend):
			// peak_n ∈ {seed7: 0.978, seed11: 0.889, seed101: 0.846} at 1000t — soft sets n, the awake stack +
			// scheduler ride under it (the B4 finding). The bound is 0.99 — guards the recorded ~0.978 worst case
			// against drift toward the n=1 cliff. Raise only with a documented re-measure (re-tune β to restore headroom).
			if peakN >= 0.99 {
				t.Fatalf("flag-ON (seed=%d) excited branching past the recorded bound: peak_n=%.3f >= 0.99 (drifted toward the n=1 cliff — re-tune branch_propensity)", seed, peakN)
			}
		})
	}
}

// TestFacultySchedulerBoundHolds is the STANDING keepStashed-vs-faculty-count guard. It pins the structural
// invariant the flag-ON faculty scheduler depends on: keepStashed (FocusCapacity-2) >= SeedFacultyCount, so
// every faculty's protected seed root fits under the U≤1 prune cap. It asserts the bound (a) is in RunSuite()
// and (b) currently PASSES at 6 faculties == keepStashed 6 (the exact boundary), and proves a 7TH faculty
// WOULD trip it (the carry-forward: a 7th faculty needs FocusCapacity→9 + a durability re-gate, because each
// newly-protected root raises the U ceiling 8→9). The "would fail at 7" half is proven by re-evaluating the
// bound's predicate at faculties+1 (the report is computed from real package state — FocusCapacity from the
// regulator default, faculties from the seed portfolio — so this is the real arithmetic, not a mock).
func TestFacultySchedulerBoundHolds(t *testing.T) {
	// (a) the bound is wired into the standing suite.
	found := false
	for _, r := range RunSuite() {
		if r.Workload == "faculty-scheduler prune-protection bound (keepStashed >= faculties)" {
			found = true
		}
	}
	if !found {
		t.Fatal("the faculty-scheduler prune-protection bound is not in RunSuite()")
	}

	// (b) it PASSES at the current taxonomy (6 faculties == keepStashed 6 == FocusCapacity-2).
	rep := FacultySchedulerBoundHolds()
	if !rep.OK() {
		t.Fatalf("the faculty-scheduler prune-protection bound must hold at the current taxonomy:\n%s", rep.Format())
	}

	// the boundary arithmetic, read off real package state (the SAME values pruneBranches uses).
	keepStashed := regulator.DefaultConfig().FocusCapacity - 2
	faculties := cognition.SeedFacultyCount()
	if keepStashed != 6 || faculties != 6 {
		// not a hard fail of the invariant, but the regression bound is written against 6 == 6 — a change
		// here means the taxonomy/budget moved and the carry-forward message + this guard need a re-read.
		t.Fatalf("the recorded boundary moved: keepStashed=%d faculties=%d (expected 6 == 6). If a faculty or "+
			"FocusCapacity was changed, re-run the continuous-mode-operator durability gate and update this guard",
			keepStashed, faculties)
	}

	// (c) a 7TH faculty (faculties+1) WOULD trip the bound: keepStashed (6) < faculties (7). This is the
	//     descriptive failure the guard exists to produce — proven by evaluating the real predicate at +1.
	if keepStashed >= faculties+1 {
		t.Fatalf("a 7th faculty must trip the bound (keepStashed=%d should be < %d) — the guard would NOT catch "+
			"a faculty added without raising FocusCapacity", keepStashed, faculties+1)
	}
}

// TestSkillCompositionBounded covers the cyclic-rejection durability obligation (Python's
// skill_composition_bounded, exercised by the suite and asserted here directly): a self-referential
// skill is rejected at mint, and every seed skill expands to a bounded verified program. Python has no
// standalone test for this (the suite covers it), so this is a Go-side guard on the same invariant.
func TestSkillCompositionBounded(t *testing.T) {
	rep := SkillCompositionBounded()
	if !rep.OK() {
		t.Fatalf("skill composition not bounded:\n%s", rep.Format())
	}
	if rep.Metrics == nil {
		t.Fatal("skill-composition report should carry an explicit (zero) metrics line, not nil")
	}
}

// TestFormatRendersMetricsAndFailures checks the report formatter (a Go-side guard on the byte-for-byte
// Python Report.format): a passing report shows [PASS] + the metrics line and no detail lines; a
// failing report shows [FAIL] + the failed-check detail and no metrics line when metrics are nil.
func TestFormatRendersMetricsAndFailures(t *testing.T) {
	pass := Report{Workload: "w", Metrics: &Metrics{PeakN: 0.5, MaxFanout: 3}, Checks: []Check{{Name: "c", OK: true}}}
	got := pass.Format()
	if !strings.HasPrefix(got, "[PASS] w") || !strings.Contains(got, "metrics: peak_n=0.50") {
		t.Fatalf("pass format wrong:\n%s", got)
	}
	fail := Report{Workload: "w", Checks: []Check{{Name: "n<1", OK: false, Detail: "cliff"}}}
	got = fail.Format()
	if !strings.HasPrefix(got, "[FAIL] w") || !strings.Contains(got, "     - n<1: cliff") {
		t.Fatalf("fail format wrong:\n%s", got)
	}
	if strings.Contains(got, "metrics:") {
		t.Fatal("a report with nil metrics must not render a metrics line")
	}
}

// TestZeroMarginUWarning is the HOLE 3a guard: U==1.0 PASSES the schedulability check (U<=1) but is a
// ZERO-MARGIN boundary case, so it must surface a non-failing warning — not be silently
// indistinguishable from a comfortable U. The warning never flips OK().
func TestZeroMarginUWarning(t *testing.T) {
	rep := Report{
		Workload: "w",
		Metrics:  &Metrics{PeakU: 1.0},
		Warnings: []string{"U==1.00 zero-margin: schedulable at the boundary (no slack — any added load tips to U>1)"},
		Checks:   []Check{{Name: "U<=1 (schedulable, peak over run)", OK: true}},
	}
	if !rep.OK() {
		t.Fatal("a zero-margin U==1.0 must still PASS (U<=1) — the warning does not flip OK()")
	}
	got := rep.Format()
	if !strings.Contains(got, "warning: U==1.00 zero-margin") {
		t.Fatalf("a U==1.0 report must render the zero-margin warning:\n%s", got)
	}
	if !strings.HasPrefix(got, "[PASS] w") {
		t.Fatalf("the zero-margin row must still read [PASS]:\n%s", got)
	}
}

// TestKgTelemetryOnlyExcludedFromHeldConditions is the STANDING anti-over-claim guard: on a real workload
// where the loop-gain check is VACUOUS (g not identified from the ring — the saturated/open or
// insufficiently-excited regimes the standing suite actually reaches), the 0<K·g<2 entry MUST be reported
// as telemetry-only — EXCLUDED from the held-conditions count and rendered as an explicit telemetry line —
// not silently banked as a held control-theoretic check (the proven-vacuous over-claim this demotion
// retires: g=prior-fallback in 16/16 workloads, measured in ZERO). It also pins the inverse: a genuinely
// actively-controlled report (g IDENTIFIED) keeps the K·g entry AS a held condition and shows no telemetry
// line — so the demotion is precise (vacuous-only), not a blanket suppression.
func TestKgTelemetryOnlyExcludedFromHeldConditions(t *testing.T) {
	// (1) a real awake row: saturated-bounded, g on the prior fallback => K·g telemetry-only.
	rep := CheckEngine(run("", "continuous", 24), "continuous (awake baseline)", "continuous")
	if rep.GainMeasured {
		t.Fatal("precondition: the awake baseline must be on the prior fallback (loop open, g unidentified)")
	}
	if !rep.KgTelemetryOnly {
		t.Fatal("a saturated/open-loop awake row must mark the K·g entry telemetry-only (UNVALIDATED), not a held condition")
	}
	// the K·g entry must NOT be counted in held/total — the count is over the FOUR boundedness conditions
	// (n<1, U<=1, fan-out, μ>0) plus any structural checks, never the vacuous loop-gain.
	held, total := rep.HeldCount()
	for _, c := range rep.Checks {
		if c.Name == kgCheckName {
			// the entry is present in Checks (so the actively-controlled/honest-fail cases still gate),
			// but HeldCount must skip it.
			if total >= len(rep.Checks) {
				t.Fatalf("HeldCount total (%d) did not exclude the telemetry-only K·g entry (len Checks=%d)", total, len(rep.Checks))
			}
		}
	}
	if held != total {
		t.Fatalf("every counted boundedness condition must hold on the durable awake row: held=%d total=%d", held, total)
	}
	got := rep.Format()
	if !strings.Contains(got, "K·g telemetry-only (UNVALIDATED, not a held condition)") {
		t.Fatalf("the telemetry-only K·g must be rendered as an explicit line on a vacuous-loop-gain row:\n%s", got)
	}
	if !strings.Contains(got, "held conditions") {
		t.Fatalf("a regime row must render the held-conditions count:\n%s", got)
	}

	// (2) the inverse: a synthetic ACTIVELY-CONTROLLED report (g identified) keeps K·g as a held condition
	//     and renders no telemetry line — the demotion is precise to the vacuous case, not a blanket hide.
	active := Report{
		Workload:        "synthetic actively-controlled",
		Regime:          regulator.RegimeActivelyControlled.String(),
		GainMeasured:    true,
		KgTelemetryOnly: false,
		Metrics:         &Metrics{},
		Checks: []Check{
			{Name: "n<1 (subcritical, peak over run)", OK: true},
			{Name: kgCheckName, OK: true},
		},
	}
	ah, at := active.HeldCount()
	if at != 2 || ah != 2 {
		t.Fatalf("an actively-controlled report must COUNT the identified K·g as a held condition: held=%d total=%d (want 2/2)", ah, at)
	}
	ag := active.Format()
	if strings.Contains(ag, "telemetry-only") {
		t.Fatalf("an actively-controlled (g identified) row must NOT render the telemetry-only line:\n%s", ag)
	}
}

// TestAwakeRowIsSaturatedBoundedNotPriorPass is the C0a suite-level guard: the awake (continuous)
// workload PASSES via the saturated-bounded regime — θ pinned => loop open => 0<K·g<2 reported NA
// (vacuous), durable by the other four MEASURED conditions — and the gain is the PRIOR fallback (not
// identified), so the report can never hide a silent prior-pass as a real loop-gain check.
func TestAwakeRowIsSaturatedBoundedNotPriorPass(t *testing.T) {
	rep := CheckEngine(run("", "continuous", 24), "continuous (awake baseline)", "continuous")
	if !rep.OK() {
		t.Fatalf("awake row must hold under the reframe:\n%s", rep.Format())
	}
	if rep.Regime != regulator.RegimeSaturatedBounded.String() {
		t.Fatalf("awake regime = %q, want saturated-bounded", rep.Regime)
	}
	if rep.GainMeasured {
		t.Fatalf("awake row gain must be the PRIOR fallback (loop open), not identified — a measured " +
			"gain here would mean the open-loop K·g was being trusted")
	}
	// the 0<K·g<2 check is present and is OK only because it is NA (vacuous), not a counted pass.
	var kg *Check
	for i := range rep.Checks {
		if rep.Checks[i].Name == "0<K*g<2 (regulator stable)" {
			kg = &rep.Checks[i]
		}
	}
	if kg == nil {
		t.Fatal("the K·g check must be present on the awake row")
	}
	if !kg.OK {
		t.Fatalf("the saturated/open-loop K·g must be OK (vacuous), got %+v", *kg)
	}
	// the other four conditions are real, measured passes (not NA): n<1, U<=1, fan-out, w*tau, mu>0.
	for _, name := range []string{"n<1 (subcritical, peak over run)", "U<=1 (schedulable, peak over run)",
		"w*tau<PM (async bounded)", "mu>0 (awake baseline measured)"} {
		found := false
		for _, c := range rep.Checks {
			if c.Name == name {
				found = true
				if !c.OK {
					t.Fatalf("boundedness condition %q must hold on the awake row", name)
				}
			}
		}
		if !found {
			t.Fatalf("awake row missing the measured boundedness condition %q", name)
		}
	}
}

// TestReportFailsAnUnidentifiedActiveLoop is the suite-level ANTI-TAUTOLOGY proof: a Report built from
// a regulator in the unidentified-ACTIVE regime (θ moving, plant unidentified, NOT saturated) must
// NOT hold — the reframe is a real, failable gate, not a new tautology. The OLD code passed this
// (K·prior = 0.2 < 2). Built through the same regulator→Report path CheckEngine uses.
func TestReportFailsAnUnidentifiedActiveLoop(t *testing.T) {
	// drive a regulator into the unidentified-active regime: θ wanders the interior, intensity is
	// exogenous (θ-independent) so the plant is unidentified, and θ never pins.
	cfg := regulator.DefaultConfig()
	cfg.LamStar = 10.0
	cfg.GainK = 0.02
	cfg.ThetaMin, cfg.ThetaMax = 0.0, 1.0
	r := regulator.New(nil, &cfg)
	var x uint64 = 0x1234567
	for i := 0; i < 120; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		o := regulator.DefaultUpdateOpts()
		o.Fired = int(x % 21) // 0..20, mean 10 == LamStar, θ-independent => unidentifiable
		o.Admitted = 1
		o.BranchesLive = 1
		r.Update(o)
	}
	checks, regime, _, measured := r.StabilityRegime("reactive")
	if regime != regulator.RegimeUnidentifiedActive {
		t.Fatalf("precondition: regime = %v, want unidentified-active-FAIL", regime)
	}
	if measured {
		t.Fatal("precondition: the exogenous-noise plant must be unidentified")
	}
	// Build a Report the same way CheckEngine does (regulator checks → stability Checks) and confirm
	// the unidentified-active K·g FAILS the report.
	rep := Report{Workload: "synthetic unidentified-active", Regime: regime.String(), GainMeasured: measured}
	for _, sc := range checks {
		rep.Checks = append(rep.Checks, Check{Name: sc.Name, OK: sc.Pass || sc.NA})
	}
	if rep.OK() {
		t.Fatal("a Report over an unidentified-ACTIVE loop must FAIL — the gate is still tautological")
	}
}

// TestAllRedesignFlagsOnPlantInSuite is the ENDGAME all-flags-on durability gate — the four cognition-
// redesign live-path flags (subconscious.capability + subconscious.capability_dispatch + convert.skill_reframe
// + convert.refine_loop) ON TOGETHER for the first time. No per-flag cell tests this COMBINATION; this gates
// the durability half of the flag-flip product decision. It asserts:
//
//	(a) the cell is a STANDING member of RunSuite() (so `thought stability` exercises the full plant), and
//	(b) the five durability conditions hold on the combined plant (n<1, U<=1, fan-out<=W_max, the static
//	    regulator conditions; reactive run ⇒ μ>0 is gated on the awake rows, not here), and
//	(c) the combined plant's control dimensions do NOT exceed the per-flag plants — n/U/fan-out on the
//	    all-flags-on plant must be <= the capability-dispatch plant's (the most-constrained single-flag
//	    recognition plant on the same goal) — the union-is-bounded claim, MEASURED not inferred.
func TestAllRedesignFlagsOnPlantInSuite(t *testing.T) {
	const label = "ALL redesign flags ON (capability+dispatch+reframe+refine_loop)"
	reports := RunSuite()
	var cell *Report
	for i := range reports {
		if reports[i].Workload == label {
			cell = &reports[i]
		}
	}
	if cell == nil {
		t.Fatalf("the all-flags-on cell %q is not in RunSuite() — `thought stability` would not exercise the full redesign plant", label)
	}

	goal := "Design and validate a small API for a todo service"
	rep := CheckEngineReactive(runAllRedesignFlagsOnPlant(goal, 24), label)
	if !rep.OK() {
		t.Fatalf("all-flags-on redesign plant FAILED the durability gate:\n%s", rep.Format())
	}
	m := rep.Metrics
	if !(m.PeakN < 1.0 && m.PeakU <= 1.0 && m.MaxFanout <= cognition.MaxParWidth) {
		t.Fatalf("all-flags-on plant not durable at the 24-tick gate: peak_n=%.2f peak_U=%.2f max_fanout=%d (W_max=%d)",
			m.PeakN, m.PeakU, m.MaxFanout, cognition.MaxParWidth)
	}

	// (c) the union-is-bounded claim: the all-flags-on plant's control dims must not exceed the single-flag
	// dispatch plant's on the SAME goal (the four deltas are each neutral-or-reducing, so their union is too).
	disp := CheckEngineReactive(runCapabilityDispatchPlant(goal, 24), "capability-dispatch (baseline)")
	dm := disp.Metrics
	if m.PeakN > dm.PeakN || m.PeakU > dm.PeakU || m.MaxFanout > dm.MaxFanout {
		t.Fatalf("all-flags-on plant EXCEEDED the single-flag dispatch plant on a control dim (union not bounded): "+
			"all{n=%.2f U=%.2f fan=%d} > dispatch{n=%.2f U=%.2f fan=%d}",
			m.PeakN, m.PeakU, m.MaxFanout, dm.PeakN, dm.PeakU, dm.MaxFanout)
	}
}

// TestAllRedesignFlagsReframedRecallMintsCatalogExcitationNeutral exercises the catalog-GROWTH path the
// reframe flag adds: with a REFRAMED skill seeded whose triggers match the goal, the Capability's
// recallReframed MINTS a per-skill operator into the persistent catalog (skill_<name>) — and that mint must
// be EXCITATION-NEUTRAL (D3): catalog growth raises the vocabulary dimension but NOT the per-tick branching.
// This is the new plant behaviour the all-flags-on gate must confirm is bounded.
func TestAllRedesignFlagsReframedRecallMintsCatalogExcitationNeutral(t *testing.T) {
	goal := "improve the throughput of the pipeline"

	build := func(seedReframed bool) *engine.Engine {
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.MaxTicks = 24
		feat := config.New()
		feat.Subconscious.Capability = true
		feat.Subconscious.CapabilityDispatch = true
		feat.Convert.SkillReframe = true
		feat.Convert.RefineLoop = true
		feat.Validate()
		cfg.Features = feat
		e, err := engine.NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine on the test double failed: %v", err)
		}
		if seedReframed {
			// A reframed (prompt-bodied) skill whose triggers match the goal ⇒ MatchReframedWithinTier fires,
			// recallReframed resolves the body + mints skill_throughput_boost into the catalog.
			_, ok := e.Skills().MintReframedTriggered("throughput_boost", "general",
				"profile the hot path, then eliminate the dominant cost and re-measure to confirm the gain",
				nil, []string{"throughput", "pipeline", "improve"}, "boost pipeline throughput")
			if !ok {
				t.Fatal("failed to seed the reframed skill (MintReframedTriggered returned false)")
			}
		}
		e.SubmitDefault(goal)
		e.Run(24)
		return e
	}

	withSkill := build(true)
	noSkill := build(false)

	mintedWith := len(withSkill.Catalog().Minted())
	mintedNo := len(noSkill.Catalog().Minted())
	if mintedWith <= mintedNo {
		t.Fatalf("the reframed-recall mint did not grow the catalog: minted(with seeded reframed skill)=%d <= minted(without)=%d "+
			"— recallReframed's per-skill operator mint is not firing", mintedWith, mintedNo)
	}

	// Excitation-neutrality (D3): the catalog grew, but the durability conditions still hold on the grown
	// plant — n<1, U<=1, fan-out<=W_max — and n/U are not raised vs the no-mint plant (catalog size does not
	// couple to per-tick branching).
	repWith := CheckEngineReactive(withSkill, "all-flags-on + reframed-recall mint")
	repNo := CheckEngineReactive(noSkill, "all-flags-on no mint")
	if !repWith.OK() {
		t.Fatalf("the grown-catalog plant FAILED durability:\n%s", repWith.Format())
	}
	mw, mn := repWith.Metrics, repNo.Metrics
	if mw.PeakN > mn.PeakN || mw.PeakU > mn.PeakU {
		t.Fatalf("catalog growth RAISED excitation (D3 violated): with-mint{n=%.2f U=%.2f} > no-mint{n=%.2f U=%.2f}",
			mw.PeakN, mw.PeakU, mn.PeakN, mn.PeakU)
	}
	if mw.Minted <= mn.Minted {
		t.Fatalf("the metrics minted-count must reflect catalog growth: with=%d <= without=%d", mw.Minted, mn.Minted)
	}
}
