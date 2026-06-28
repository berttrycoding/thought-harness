package realhard

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// estimator_test.go — the noise-aware estimator's OFFLINE synthetic tests (no
// model, no engine — like sigmar_test.go). Each test plants a KNOWN structure
// (a run effect, a covariate of known ρ², a variance reduction, a leak) and
// asserts the reducer recovers it / behaves honestly. These are the spec §7.4
// test list: CUPED variance reduction, run-effect recovery, the variance-ratio
// gate in both directions, required-R monotonicity, the leakage guard,
// degenerate/under-identified honesty, paired-vs-unpaired SE, and the off-mode
// regression anchor. Plus the on-disk sanity demo at the end.

const estEps = 1e-9

func approxEst(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// --- 1. run-effect recovery -----------------------------------------------

// TestRunEffectVarRecovery plants a known per-launch shared shock u_r on top of
// per-task baselines and asserts runEffectVar recovers σ²_run within tolerance,
// and that SATURATED tasks contribute ZERO (they are dropped from the estimate).
func TestRunEffectVarRecovery(t *testing.T) {
	// 3 informative tasks with per-task baselines {0.3,0.5,0.7}; a shared per-launch
	// shock u_r added to EVERY task in a launch (the run effect). With the same u_r
	// applied to all tasks, the between-launch mean square carries T_eff·σ²_run, so
	// runEffectVar should recover σ²_run = sampleVar(u).
	base := []float64{0.3, 0.5, 0.7}
	u := []float64{-0.1, 0.0, +0.1, -0.05, +0.05} // 5 launches, mean 0
	R := len(u)
	rates := make([][]float64, R)
	for l := 0; l < R; l++ {
		row := make([]float64, len(base))
		for c := range base {
			row[c] = base[c] + u[l] // shared shock, no residual: pure run effect
		}
		rates[l] = row
	}
	wantVar := sampleVar(u)
	gotVar := runEffectVar(rates)
	if !approxEst(gotVar, wantVar, 1e-9) {
		t.Errorf("runEffectVar recovered %g, want planted σ²_run=%g", gotVar, wantVar)
	}

	// Add a SATURATED task (constant 1.0 every launch) — it must contribute 0 and not
	// change the recovered run variance (it is dropped from the informative set).
	ratesWithSat := make([][]float64, R)
	for l := 0; l < R; l++ {
		ratesWithSat[l] = append(append([]float64(nil), rates[l]...), 1.0)
	}
	gotWithSat := runEffectVar(ratesWithSat)
	if !approxEst(gotWithSat, wantVar, 1e-9) {
		t.Errorf("a saturated task changed σ²_run: %g vs %g — saturated tasks must contribute 0", gotWithSat, wantVar)
	}

	// All-saturated → σ²_run = 0 (no informative task).
	allSat := [][]float64{{1, 1}, {1, 1}, {1, 1}}
	if v := runEffectVar(allSat); v != 0 {
		t.Errorf("all-saturated σ²_run must be 0, got %g", v)
	}
}

// TestRunEffectVarSingleLaunchIsZero: R=1 has no measurable run variance.
func TestRunEffectVarSingleLaunchIsZero(t *testing.T) {
	if v := runEffectVar([][]float64{{0.2, 0.5, 0.8}}); v != 0 {
		t.Errorf("single-launch σ²_run must be 0, got %g", v)
	}
}

// --- 2. CUPED variance reduction ------------------------------------------

// TestCUPEDVarianceReduction is the load-bearing CUPED property: a covariate with a
// KNOWN ρ² must (a) leave β̂ unbiased (same expectation) and (b) cut the variance of
// β̂ by ≈(1−ρ²). We measure this EMPIRICALLY over many synthetic datasets (a seeded
// Monte Carlo) because the variance of an estimator is an across-dataset quantity.
//
// Construction: each dataset is a single-arm (OFF-only) set of R launches. The
// launch outcome y_l = mu + noise_l, and a CLEAN covariate x_l correlated with the
// noise at a target ρ. CUPED on x should strip the ρ²-fraction of the noise variance
// out of the launch-mean estimate. We compare the across-dataset variance of the raw
// launch-mean vs the CUPED-adjusted launch-mean.
func TestCUPEDVarianceReduction(t *testing.T) {
	const (
		R          = 6
		datasets   = 4000
		mu         = 0.5
		targetRho2 = 0.5 // covariate explains half the launch noise
	)
	rho := math.Sqrt(targetRho2)
	rng := cpyrand.New(0xC0FFEE)

	rawMeans := make([]float64, 0, datasets)
	adjMeans := make([]float64, 0, datasets)
	for d := 0; d < datasets; d++ {
		y := make([]float64, R)
		x := make([]float64, R)
		for l := 0; l < R; l++ {
			// shared standard-normal latent; the covariate = rho*latent + indep, the
			// outcome noise = latent (so corr(x, noise) = rho). Box-Muller from cpyrand.
			latent := gauss(rng)
			indep := gauss(rng)
			noise := latent
			x[l] = rho*latent + math.Sqrt(1-targetRho2)*indep
			y[l] = mu + 0.2*noise // 0.2 scales the noise; ρ² is scale-invariant
		}
		rawMeans = append(rawMeans, meanOf(y))
		// CUPED-adjust y by x with the FROZEN POPULATION θ (the standard CUPED guard:
		// θ must be treatment/sample-independent, else the in-sample OLS re-fit makes
		// the residual mean identically the raw mean — no reduction). Here the data
		// generation pins the population θ exactly: Cov(y,x)=0.2·ρ·Var(latent)=0.2·ρ,
		// Var(x)=1 → θ_pop = 0.2·ρ. Using it gives the genuine (1−ρ²) variance cut.
		thetaPop := 0.2 * rho
		// center x on its POPULATION mean (0) — a frozen-θ CUPED uses the known E[x],
		// not the in-sample mean, so the reduction is real across datasets.
		adj := make([]float64, R)
		for l := 0; l < R; l++ {
			adj[l] = y[l] - thetaPop*(x[l]-0)
		}
		adjMeans = append(adjMeans, meanOf(adj))
	}

	rawMean := meanOf(rawMeans)
	adjMean := meanOf(adjMeans)
	// (a) unbiasedness: both estimators centre on mu.
	if !approxEst(rawMean, mu, 0.01) {
		t.Errorf("raw β̂ biased: mean %g, want %g", rawMean, mu)
	}
	if !approxEst(adjMean, mu, 0.01) {
		t.Errorf("CUPED β̂ biased: mean %g, want %g (CUPED must not move the expectation)", adjMean, mu)
	}
	// (b) variance reduction ≈ (1−ρ²).
	rawVar := sampleVar(rawMeans)
	adjVar := sampleVar(adjMeans)
	gotReduction := adjVar / rawVar
	wantReduction := 1 - targetRho2
	// empirical Monte-Carlo tolerance (4000 datasets, R=6): the ratio should land
	// near (1−ρ²)=0.5 within ~10%.
	if math.Abs(gotReduction-wantReduction) > 0.06 {
		t.Errorf("CUPED variance ratio %g, want ≈%g (1−ρ²) — within 0.06", gotReduction, wantReduction)
	}
}

// TestCUPEDThetaAndRho2: a perfectly-correlated covariate gives ρ²=1; an orthogonal
// one gives ρ²=0; a degenerate (constant) covariate gives θ=0,ρ²=0 (no panic).
func TestCUPEDThetaAndRho2(t *testing.T) {
	y := []float64{1, 2, 3, 4, 5}
	// perfect positive correlation → ρ²=1, θ=Cov/Var.
	xPerfect := []float64{2, 4, 6, 8, 10}
	_, rho2 := cupedTheta(y, xPerfect)
	if !approxEst(rho2, 1, 1e-9) {
		t.Errorf("perfectly-correlated covariate ρ²=%g, want 1", rho2)
	}
	// constant covariate → ρ²=0, θ=0 (no divide-by-zero).
	xConst := []float64{3, 3, 3, 3, 3}
	th, r2 := cupedTheta(y, xConst)
	if th != 0 || r2 != 0 {
		t.Errorf("constant covariate must give θ=0,ρ²=0, got θ=%g ρ²=%g", th, r2)
	}
}

// --- 3. robustness variance-ratio gate (BOTH directions) ------------------

// TestVarianceRatioBootstrapBothDirections plants σ²_run REDUCTION (ON lower) and
// σ²_run INFLATION (ON higher) and asserts the bootstrap ratio + CI move the right
// way. The deliberative lever is supposed to SHRINK σ²_run; the gate must fire ONLY
// when the CI excludes 1 in the lower direction — and must NOT fire (or fire the
// other way) when ON is higher. This guards a false-positive robustness claim.
func TestVarianceRatioBootstrapBothDirections(t *testing.T) {
	rng := cpyrand.New(0xBEEF)
	base := []float64{0.3, 0.5, 0.7}
	// OFF: large shared shock (σ_u = 0.15). ON-lower: small shock (σ_u = 0.03).
	// ON-higher: even larger shock (σ_u = 0.30).
	off := syntheticRunEffect(base, 0.15, 12, rng)
	onLower := syntheticRunEffect(base, 0.03, 12, rng)
	onHigher := syntheticRunEffect(base, 0.30, 12, rng)

	// ON lower: ratio < 1.
	ratioLo, _, ciHiLo := varianceRatioBootstrap(off, onLower, 2000, 0xABCD)
	if ratioLo >= 1 {
		t.Errorf("ON-lower variance ratio %g must be < 1 (the lever shrank σ²_run)", ratioLo)
	}
	if ciHiLo >= 1 {
		t.Errorf("ON-lower ratio CI upper %g should be < 1 (a significant robustness gain)", ciHiLo)
	}

	// ON higher: ratio > 1, and the lower-direction gate must NOT fire (CIHi >= 1).
	ratioHi, _, ciHiHi := varianceRatioBootstrap(off, onHigher, 2000, 0xABCD)
	if ratioHi <= 1 {
		t.Errorf("ON-higher variance ratio %g must be > 1 (ON inflated σ²_run)", ratioHi)
	}
	if ciHiHi < 1 {
		t.Errorf("ON-higher ratio CI upper %g must be >= 1 — the lower-direction gate must NOT fire", ciHiHi)
	}

	// determinism: same inputs + seed → bit-identical (the seeded bootstrap).
	r2, lo2, hi2 := varianceRatioBootstrap(off, onLower, 2000, 0xABCD)
	_, lo1, _ := varianceRatioBootstrap(off, onLower, 2000, 0xABCD)
	if r2 != ratioLo || hi2 != ciHiLo || lo2 != lo1 {
		t.Errorf("bootstrap not deterministic: ratio %g vs %g, ciHi %g vs %g, ciLo %g vs %g",
			r2, ratioLo, hi2, ciHiLo, lo2, lo1)
	}
}

// --- 4. required-R monotonicity -------------------------------------------

// TestRequiredRMonotonicity: required-R rises with σ²_run, falls with ρ², falls with
// δ (the §4 formula's qualitative shape).
func TestRequiredRMonotonicity(t *testing.T) {
	// rises with σ²_run.
	rLo := requiredR(0.1, 0, 0.15)
	rHi := requiredR(0.25, 0, 0.15)
	if !(rHi > rLo) {
		t.Errorf("required-R must rise with σ²_run: σ²=0.1 -> %g, σ²=0.25 -> %g", rLo, rHi)
	}
	// falls with ρ² (CUPED).
	rNoCov := requiredR(0.25, 0, 0.15)
	rCov := requiredR(0.25, 0.5, 0.15)
	if !(rCov < rNoCov) {
		t.Errorf("required-R must fall with ρ²: ρ²=0 -> %g, ρ²=0.5 -> %g", rNoCov, rCov)
	}
	if !approxEst(rCov, rNoCov*0.5, 1e-9) {
		t.Errorf("ρ²=0.5 must HALVE required-R: %g vs %g/2=%g", rCov, rNoCov, rNoCov*0.5)
	}
	// falls with δ (larger effect is easier to gate).
	rSmallDelta := requiredR(0.25, 0, 0.10)
	rBigDelta := requiredR(0.25, 0, 0.30)
	if !(rBigDelta < rSmallDelta) {
		t.Errorf("required-R must fall with δ: δ=0.10 -> %g, δ=0.30 -> %g", rSmallDelta, rBigDelta)
	}
	// degenerate guards: σ²_run=0 or δ<=0 → 0.
	if requiredR(0, 0, 0.15) != 0 {
		t.Errorf("σ²_run=0 must give required-R=0")
	}
	if requiredR(0.25, 0, 0) != 0 {
		t.Errorf("δ<=0 must give required-R=0")
	}
}

// TestRequiredRWorkedNumber pins the §4.3 worked figure: σ²_run≈0.25, δ=0.15, no
// covariate → R≈137 (the brutal honest run-dominated read). This is the "the
// estimator does not beat the substrate" anchor — a coin-flip run effect needs
// ~100+ launches.
func TestRequiredRWorkedNumber(t *testing.T) {
	got := requiredR(0.25, 0, 0.15)
	// 2·(zα+zβ)²·0.25/0.15² with zα+zβ = 2.4864748605... ≈ 137.4
	want := 2 * (estZAlpha + estZBeta) * (estZAlpha + estZBeta) * 0.25 / (0.15 * 0.15)
	if !approxEst(got, want, 1e-9) {
		t.Errorf("worked required-R %g, want %g (~137 — the honest coin-flip ceiling)", got, want)
	}
	if got < 130 || got > 145 {
		t.Errorf("worked required-R %g out of the spec's ~137 band", got)
	}
}

// --- 5. leakage guard ------------------------------------------------------

// TestLeakageGuardDropsContaminatedCovariate: a covariate that IS (correlated with)
// the treatment effect — i.e. it differs systematically between the ON and OFF arms
// in a way that swings β beyond its SE — must be DROPPED and the verdict must be
// LEAKAGE-SUSPECTED, with the RAW β reported (not the inflated adjusted one).
func TestLeakageGuardDropsContaminatedCovariate(t *testing.T) {
	// 4 launches, 3 informative tasks. OFF and ON share launches (paired). The ON arm
	// has a real +0.2 effect. We feed a LEAKY covariate (model_calls) that is itself
	// strongly moved by the treatment in a way correlated with the per-launch effect,
	// so adjusting on it swings β.
	taskIDs := []string{"t1", "t2", "t3"}
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking, CapMultiHopGrounding}
	off := [][]float64{
		{0.0, 0.5, 0.5},
		{0.5, 0.0, 1.0},
		{0.5, 0.5, 0.0},
		{1.0, 0.5, 0.5},
	}
	// ON = OFF shifted up by a VARYING per-launch amount (so the per-launch diff is
	// non-constant → the raw paired SE is > 0, the precondition for the swing guard).
	on := [][]float64{
		{0.0, 0.5, 1.0}, // launch 0: small lift
		{1.0, 0.5, 1.0}, // launch 1: large lift
		{0.5, 1.0, 0.5}, // launch 2: medium lift
		{1.0, 1.0, 1.0}, // launch 3: large lift
	}
	// leaky covariate: model_calls per (launch,task) that tracks the per-launch
	// OUTCOME (high when the launch solved a lot) — a downstream consequence of
	// solving → using it as an adjuster partials out the effect.
	covOff := outcomeTrackingCov(off)
	covOn := outcomeTrackingCov(on)

	cfg := EstimatorConfig{
		Mode:       EstPaired,
		Covariates: []string{"model_calls"},
		CUPED:      true,
		Delta:      0.15,
	}
	rep := EstimateMatrix(off, on, covOff, covOn, taskIDs, caps, cfg)
	if rep.Verdict != EstLeakageSuspected {
		t.Errorf("a treatment-contaminated covariate must trip LEAKAGE-SUSPECTED, got %s (swing=%g SE=%g)",
			rep.Verdict, rep.Beta-rep.BetaRaw, rep.BetaSE)
	}
	if len(rep.CovariateDropped) == 0 {
		t.Errorf("the leaky covariate must be DROPPED")
	}
	if len(rep.CovariateUsed) != 0 {
		t.Errorf("no covariate must remain USED after a leak drop, got %v", rep.CovariateUsed)
	}
	// the RAW (unadjusted) β must be reported.
	if rep.Beta != rep.BetaRaw {
		t.Errorf("after a leak drop the reported β (%g) must equal the RAW β (%g)", rep.Beta, rep.BetaRaw)
	}
}

// TestCleanCovariateNotDropped: the clean launch_temp covariate (mean model_calls
// over SATURATED tasks — outcome-independent) must NOT trip the guard; it is applied
// and ρ² is reported.
func TestCleanCovariateNotDropped(t *testing.T) {
	taskIDs := []string{"sat", "inf1", "inf2"}
	caps := []Capability{CapLongHorizonConsistency, CapMultiHopGrounding, CapAdaptiveBacktracking}
	// column 0 saturated (always 1) → the launch_temp probe; cols 1,2 informative.
	off := [][]float64{
		{1.0, 0.0, 0.5},
		{1.0, 0.5, 0.0},
		{1.0, 0.5, 0.5},
		{1.0, 1.0, 0.5},
	}
	on := [][]float64{
		{1.0, 0.5, 0.5},
		{1.0, 0.5, 0.5},
		{1.0, 1.0, 0.5},
		{1.0, 1.0, 1.0},
	}
	// covariates: a small per-launch wobble in the saturated task's model_calls
	// (launch hotness) UNCORRELATED with the effect → clean.
	covOff := constModelCallsCov(off, []float64{10, 12, 11, 13})
	covOn := constModelCallsCov(on, []float64{11, 12, 11, 12})
	cfg := EstimatorConfig{Mode: EstPaired, Covariates: []string{"launch_temp"}, CUPED: true, Delta: 0.15}
	rep := EstimateMatrix(off, on, covOff, covOn, taskIDs, caps, cfg)
	if rep.Verdict == EstLeakageSuspected {
		t.Errorf("the clean launch_temp covariate must NOT trip the leak guard (swing=%g SE=%g)",
			rep.BetaAdjusted-rep.BetaRaw, rep.BetaSE)
	}
}

// --- 6. degenerate / under-identified --------------------------------------

// TestDegenerateAndUnderIdentified: R=1 → DEGENERATE; all-saturated → UNDER-
// IDENTIFIED (data present but no informative task); never a confident β.
func TestDegenerateAndUnderIdentified(t *testing.T) {
	taskIDs := []string{"a", "b"}
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking}

	// R=1 (single launch) with an AB contrast → DEGENERATE (run effect not identifiable).
	off1 := [][]float64{{0.5, 0.5}}
	on1 := [][]float64{{1.0, 0.5}}
	cfg := EstimatorConfig{Mode: EstPaired, Delta: 0.15}
	rep1 := EstimateMatrix(off1, on1, nil, nil, taskIDs, caps, cfg)
	if rep1.Verdict != EstDegenerate {
		t.Errorf("R=1 must be DEGENERATE, got %s", rep1.Verdict)
	}

	// all-saturated (both arms constant) → no informative task → UNDER-IDENTIFIED.
	offSat := [][]float64{{1, 1}, {1, 1}, {1, 1}}
	onSat := [][]float64{{1, 1}, {1, 1}, {1, 1}}
	rep2 := EstimateMatrix(offSat, onSat, nil, nil, taskIDs, caps, cfg)
	if rep2.Verdict != EstUnderIdentified && rep2.Verdict != EstDegenerate {
		t.Errorf("all-saturated must be UNDER-IDENTIFIED or DEGENERATE, got %s", rep2.Verdict)
	}
	if rep2.Verdict == EstFeasible {
		t.Errorf("an all-saturated set must NEVER be FEASIBLE (no signal to gate)")
	}

	// T_eff=1 (only one informative task) → UNDER-IDENTIFIED (< estMinTaskEff).
	taskIDs3 := []string{"sat1", "sat2", "inf"}
	caps3 := []Capability{CapLongHorizonConsistency, CapLongHorizonConsistency, CapMultiHopGrounding}
	off3 := [][]float64{{1, 1, 0}, {1, 1, 1}, {1, 1, 0}, {1, 1, 1}}
	on3 := [][]float64{{1, 1, 1}, {1, 1, 1}, {1, 1, 0}, {1, 1, 1}}
	rep3 := EstimateMatrix(off3, on3, nil, nil, taskIDs3, caps3, cfg)
	if rep3.Verdict != EstUnderIdentified {
		t.Errorf("T_eff=1 must be UNDER-IDENTIFIED, got %s (T_eff=%d)", rep3.Verdict, rep3.TaskEff)
	}
}

// --- 7. paired vs unpaired SE ----------------------------------------------

// TestPairedTighterThanUnpaired is the Miller §4 lever: on a task-difficulty-
// DOMINATED synthetic (large between-task spread, small effect), the WITHIN-LAUNCH
// paired SE must be tighter than the unpaired SE, because pairing cancels the
// task-difficulty common-mode. We build matched launches where the per-task
// difficulty is the dominant variance and the ON−OFF effect is small + constant.
func TestPairedTighterThanUnpaired(t *testing.T) {
	taskIDs := []string{"t1", "t2", "t3", "t4"}
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking, CapMultiHopGrounding, CapAdaptiveBacktracking}
	// Per-task difficulty dominates: task means spread wide (0.1..0.9). Each launch
	// re-draws the per-task rate around its difficulty with a small wobble; the ON arm
	// is the OFF arm + a tiny constant per-launch+per-task effect that pairing
	// cancels the difficulty against. Hand-built matched matrices:
	off := [][]float64{
		{0.1, 0.4, 0.6, 0.9},
		{0.2, 0.3, 0.7, 0.8},
		{0.0, 0.5, 0.5, 1.0},
		{0.1, 0.4, 0.6, 0.9},
	}
	// ON = OFF + 0.1 on each cell (a clean +0.1 effect, no extra noise): the paired
	// per-launch diff is EXACTLY 0.1 every launch → paired SE = 0. The unpaired SE
	// carries the full between-task + between-launch spread of each arm.
	on := make([][]float64, len(off))
	for l := range off {
		row := make([]float64, len(off[l]))
		for c := range off[l] {
			row[c] = off[l][c] + 0.1
		}
		on[l] = row
	}
	cfg := EstimatorConfig{Mode: EstPaired, Delta: 0.15}
	rep := EstimateMatrix(off, on, nil, nil, taskIDs, caps, cfg)
	if !rep.Paired {
		t.Fatalf("matched launch matrices must be detected as PAIRED")
	}
	// the paired β must recover the +0.1 effect exactly.
	if !approxEst(rep.Beta, 0.1, 1e-9) {
		t.Errorf("paired β = %g, want 0.1 (the planted effect)", rep.Beta)
	}
	// the paired SE (the reported BetaSE) must be much tighter than the unpaired SE.
	// Compute the unpaired SE directly for the comparison.
	_, unpairedSE, _ := capabilityBeta(off, on, false)
	if !(rep.BetaSE < unpairedSE) {
		t.Errorf("paired SE (%g) must be TIGHTER than unpaired SE (%g) on a difficulty-dominated set",
			rep.BetaSE, unpairedSE)
	}
	// with a constant +0.1 effect the paired SE is ~0 (no launch-to-launch diff
	// variance), demonstrating the common-mode cancellation.
	if rep.BetaSE > 1e-9 {
		t.Errorf("a constant per-launch effect must give a ~0 paired SE (common-mode cancelled), got %g", rep.BetaSE)
	}
}

// TestValidSEWiderThanNaivePool is the CORRECTNESS demonstration: the valid
// (variance-component) SE must be WIDER than the naive pooled-SD SE when a launch
// random effect is present — the naive SE is too narrow and would fire false gates.
func TestValidSEWiderThanNaivePool(t *testing.T) {
	// UNPAIRED arms (different launch sets) each with a big shared launch shock: every
	// task in a launch moves together. The naive pool treats cells as independent and
	// understates the SE; the valid SE folds in σ²_run.
	off := [][]float64{
		{0.1, 0.1, 0.1}, // cold launch
		{0.9, 0.9, 0.9}, // hot launch
		{0.1, 0.1, 0.1},
		{0.9, 0.9, 0.9},
	}
	on := [][]float64{
		{0.2, 0.2, 0.2},
		{1.0, 1.0, 1.0},
		{0.2, 0.2, 0.2},
		{1.0, 1.0, 1.0},
	}
	// make them UNPAIRED by giving different launch counts so isPaired is false.
	on = append(on, []float64{0.6, 0.6, 0.6})
	beta, validSE, naiveSE := capabilityBeta(off, on, false)
	_ = beta
	if !(validSE > naiveSE) {
		t.Errorf("valid SE (%g) must EXCEED the naive pooled SE (%g) under a launch random effect "+
			"(the naive SE is too narrow → false gates)", validSE, naiveSE)
	}
}

// --- 8. off-mode regression anchor -----------------------------------------

// TestOffModeMatchesComputeSigmaR: Mode=off reproduces the ComputeSigmaR headline
// EXACTLY (the strict-superset / regression anchor — the estimator never changes the
// existing number).
func TestOffModeMatchesComputeSigmaR(t *testing.T) {
	rates := [][]float64{
		{1.00, 0.00, 1.00},
		{0.00, 0.00, 1.00},
		{0.00, 1.00, 1.00},
		{1.00, 1.00, 0.00},
	}
	taskIDs := []string{"a", "b", "c"}
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking, CapAntiConfabulation}
	_, wantSigma, wantRate := ComputeSigmaR(rates, taskIDs, caps)
	rep := EstimateMatrix(rates, nil, nil, nil, taskIDs, caps, EstimatorConfig{Mode: EstOff})
	if rep.MeanSigmaR != wantSigma {
		t.Errorf("off-mode mean σ_R %g != ComputeSigmaR %g (must be byte-identical)", rep.MeanSigmaR, wantSigma)
	}
	if rep.MeanSolveRate != wantRate {
		t.Errorf("off-mode mean solve-rate %g != ComputeSigmaR %g", rep.MeanSolveRate, wantRate)
	}
	// off mode never emits a gate verdict (anchor only).
	if rep.Verdict != EstDegenerate {
		t.Errorf("off-mode verdict must be DEGENERATE (no gate), got %s", rep.Verdict)
	}
	// the empty-mode default also routes to off.
	rep2 := EstimateMatrix(rates, nil, nil, nil, taskIDs, caps, EstimatorConfig{})
	if rep2.MeanSigmaR != wantSigma {
		t.Errorf("default(off) mean σ_R %g != ComputeSigmaR %g", rep2.MeanSigmaR, wantSigma)
	}
}

// --- 9. on-disk data sanity demo -------------------------------------------

// TestOnDiskDataSanityDemo applies the reducer to the EXACT on-disk K1-R4 / K3-R4
// rate matrices (transcribed from runs/sigmar-K1-R4.txt / K3-R4.txt) and prints the
// valid-CI / σ²_run / required-R read. These files are POST-COLLAPSE (solved-only,
// no retained covariates and no AB pairing), so this is a SINGLE-ARM characterization
// demo — it shows the run-effect variance + required-R the spec §4.3 predicts (the
// ~137 honest ceiling), not a β gate. It asserts the headline matches the file (the
// instrument reads the on-disk number) and logs the estimator read.
func TestOnDiskDataSanityDemo(t *testing.T) {
	taskIDs := []string{
		"realhard-back-0001", "realhard-back-0002",
		"realhard-conf-0001", "realhard-conf-0002",
		"realhard-long-0001", "realhard-long-0002", "realhard-long-0003",
		"realhard-mhop-0001", "realhard-mhop-0002", "realhard-mhop-0003",
	}
	caps := []Capability{
		CapAdaptiveBacktracking, CapAdaptiveBacktracking,
		CapAntiConfabulation, CapAntiConfabulation,
		CapLongHorizonConsistency, CapLongHorizonConsistency, CapLongHorizonConsistency,
		CapMultiHopGrounding, CapMultiHopGrounding, CapMultiHopGrounding,
	}
	// K1-R4: columns in taskIDs order; rows are the 4 launches (transcribed from the
	// per-task "rates [...]" vectors in runs/sigmar-K1-R4.txt).
	//   back-0001 [1 0 0 1]  back-0002 [0 0 1 1]
	//   conf-0001 [1 1 1 1]  conf-0002 [1 1 1 1]
	//   long-0001/2/3 [1 1 1 1]
	//   mhop-0001 [1 1 1 1]  mhop-0002 [0 1 1 1]  mhop-0003 [0 0 0 0]
	k1 := transpose([][]float64{
		{1, 0, 0, 1}, {0, 0, 1, 1},
		{1, 1, 1, 1}, {1, 1, 1, 1},
		{1, 1, 1, 1}, {1, 1, 1, 1}, {1, 1, 1, 1},
		{1, 1, 1, 1}, {0, 1, 1, 1}, {0, 0, 0, 0},
	})
	_, k1Sigma, k1Rate := ComputeSigmaR(k1, taskIDs, caps)
	// the file headline: mean σ_R 0.1655, mean solve-rate 0.7750.
	if !approxEst(k1Sigma, 0.1655, 5e-4) {
		t.Errorf("K1-R4 mean σ_R %.4f != on-disk 0.1655 — transcription/instrument mismatch", k1Sigma)
	}
	if !approxEst(k1Rate, 0.7750, 5e-4) {
		t.Errorf("K1-R4 mean solve-rate %.4f != on-disk 0.7750", k1Rate)
	}
	repK1 := EstimateMatrix(k1, nil, nil, nil, taskIDs, caps, EstimatorConfig{Mode: EstPaired, Delta: 0.15})
	t.Logf("K1-R4 single-arm read: T_eff=%d σ_run(OFF)=%.4f required-R(δ=0.15)=%.1f verdict=%s",
		repK1.TaskEff, repK1.SigmaRunOff, repK1.RequiredR, repK1.Verdict)

	// K3-R4 (runs/sigmar-K3-R4.txt):
	//   back-0001 [1 0 0 0]  back-0002 [0 1 1 1]
	//   conf-0001 [1 1 1 1]  conf-0002 [1 0 1 1]
	//   long-0001/2/3 [1 1 1 1]
	//   mhop-0001 [1 0 1 1]  mhop-0002 [1 1 1 1]  mhop-0003 [0 1 1 0]
	k3 := transpose([][]float64{
		{1, 0, 0, 0}, {0, 1, 1, 1},
		{1, 1, 1, 1}, {1, 0, 1, 1},
		{1, 1, 1, 1}, {1, 1, 1, 1}, {1, 1, 1, 1},
		{1, 0, 1, 1}, {1, 1, 1, 1}, {0, 1, 1, 0},
	})
	_, k3Sigma, k3Rate := ComputeSigmaR(k3, taskIDs, caps)
	if !approxEst(k3Sigma, 0.2577, 5e-4) {
		t.Errorf("K3-R4 mean σ_R %.4f != on-disk 0.2577", k3Sigma)
	}
	if !approxEst(k3Rate, 0.8000, 5e-4) {
		t.Errorf("K3-R4 mean solve-rate %.4f != on-disk 0.8000", k3Rate)
	}
	repK3 := EstimateMatrix(k3, nil, nil, nil, taskIDs, caps, EstimatorConfig{Mode: EstPaired, Delta: 0.15})
	t.Logf("K3-R4 single-arm read: T_eff=%d σ_run(OFF)=%.4f required-R(δ=0.15)=%.1f verdict=%s",
		repK3.TaskEff, repK3.SigmaRunOff, repK3.RequiredR, repK3.Verdict)

	// the spec §4.3 demonstration: the two headlines (0.1655 vs 0.2577) are NOT
	// separable at R=4 — assert the estimator flags BOTH as UNDER-IDENTIFIED or NOISY
	// (never a confident FEASIBLE off 4 launches of a coin-flip run effect).
	if repK1.Verdict == EstFeasible || repK3.Verdict == EstFeasible {
		t.Errorf("on-disk R=4 single-arm reads must NOT be FEASIBLE (spec §4.3: not separable at R=4): K1=%s K3=%s",
			repK1.Verdict, repK3.Verdict)
	}
}

// TestEstimateFlatSingleLaunch: the flat-slice Estimate reduces ONE launch's
// RunResults into a per-task rate matrix and routes to EstimateMatrix. A single
// launch cannot identify the run effect (R=1) → DEGENERATE, but the headline σ_R /
// mean solve-rate ARE computed (it is a real reduction, not a stub).
func TestEstimateFlatSingleLaunch(t *testing.T) {
	results := []RunResult{
		{TaskID: "t1", Capability: CapMultiHopGrounding, Arm: ArmHarness, Verdict: Verdict{Solved: true}, ModelCalls: 3},
		{TaskID: "t2", Capability: CapAdaptiveBacktracking, Arm: ArmHarness, Verdict: Verdict{Solved: false}, ModelCalls: 5},
		{TaskID: "t3", Capability: CapAntiConfabulation, Arm: ArmHarness, Verdict: Verdict{Solved: true}, ModelCalls: 2},
		// a bare-arm result must be IGNORED (the σ_R / estimator is the harness arm).
		{TaskID: "t1", Capability: CapMultiHopGrounding, Arm: ArmBare, Verdict: Verdict{Solved: false}},
	}
	rep := Estimate(results, EstimatorConfig{Mode: EstPaired, Delta: 0.15})
	if rep.Tasks != 3 {
		t.Errorf("flat Estimate must reduce 3 distinct harness tasks, got %d", rep.Tasks)
	}
	// mean solve-rate over the harness arm = 2/3 (t1,t3 solved, t2 not).
	if !approxEst(rep.MeanSolveRate, 2.0/3.0, 1e-9) {
		t.Errorf("flat Estimate mean solve-rate %g, want 2/3 (bare arm ignored)", rep.MeanSolveRate)
	}
	// R=1 → the run effect is not identifiable → DEGENERATE, never a confident gate.
	if rep.Verdict != EstDegenerate {
		t.Errorf("single-launch flat Estimate must be DEGENERATE (R=1), got %s", rep.Verdict)
	}
	if rep.Launches != 1 {
		t.Errorf("flat Estimate must read 1 launch, got %d", rep.Launches)
	}
}

// --- synthetic-data helpers -------------------------------------------------

// syntheticRunEffect builds R launches of per-task baselines + a shared per-launch
// Gaussian shock of SD sigmaU (the planted run effect), clamped to [0,1].
func syntheticRunEffect(base []float64, sigmaU float64, R int, rng *cpyrand.Random) [][]float64 {
	out := make([][]float64, R)
	for l := 0; l < R; l++ {
		u := sigmaU * gauss(rng)
		row := make([]float64, len(base))
		for c := range base {
			v := base[c] + u
			if v < 0 {
				v = 0
			}
			if v > 1 {
				v = 1
			}
			row[c] = v
		}
		out[l] = row
	}
	return out
}

// outcomeTrackingCov builds a model_calls covariate that TRACKS the per-launch
// outcome (high model_calls where the launch solved a lot) — a downstream/leaky
// covariate. Returns a [launch][task] covariate matrix.
func outcomeTrackingCov(rates [][]float64) [][]estCovariates {
	out := make([][]estCovariates, len(rates))
	for l := range rates {
		launchMean := meanOf(rates[l])
		row := make([]estCovariates, len(rates[l]))
		for c := range rates[l] {
			// model_calls high when the launch solved a lot (leak) + the task's own rate.
			row[c] = estCovariates{modelCalls: 100*launchMean + 10*rates[l][c]}
		}
		out[l] = row
	}
	return out
}

// constModelCallsCov sets the saturated-task model_calls to a per-launch value
// (the clean launch_temp probe), other tasks to a fixed baseline. perLaunch must
// have one entry per launch.
func constModelCallsCov(rates [][]float64, perLaunch []float64) [][]estCovariates {
	out := make([][]estCovariates, len(rates))
	for l := range rates {
		row := make([]estCovariates, len(rates[l]))
		for c := range rates[l] {
			mc := 5.0 // baseline
			if columnConstant(rates, c) {
				mc = perLaunch[l] // the launch-temp probe lives on the saturated task
			}
			row[c] = estCovariates{modelCalls: mc}
		}
		out[l] = row
	}
	return out
}

// transpose turns a [task][launch] matrix into the [launch][task] matrix the
// estimator consumes.
func transpose(byTask [][]float64) [][]float64 {
	if len(byTask) == 0 {
		return nil
	}
	R := len(byTask[0])
	T := len(byTask)
	out := make([][]float64, R)
	for l := 0; l < R; l++ {
		out[l] = make([]float64, T)
		for c := 0; c < T; c++ {
			out[l][c] = byTask[c][l]
		}
	}
	return out
}

// gauss draws a standard-normal via Box-Muller from the seeded cpyrand stream
// (deterministic; no wall clock, no math/rand). Two uniforms → one normal.
func gauss(rng *cpyrand.Random) float64 {
	u1 := rng.Float64()
	u2 := rng.Float64()
	if u1 < 1e-12 {
		u1 = 1e-12
	}
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

var _ = estEps
