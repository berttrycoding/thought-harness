package realhard

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// bernoulli_test.go — the BERNOULLI HIGH-K SINGLE-LAUNCH estimator's OFFLINE
// synthetic tests (no model, no engine — like sigmar_test.go / estimator_test.go).
// Each test plants a KNOWN structure (a true p, a planted effect, a shared shock, a
// majority-of-k coin) and asserts the reducer recovers it / behaves honestly. These
// are the task's test list: Wilson-CI coverage vs Monte-Carlo truth (incl. p near
// 0/1), variance-by-formula, two-proportion recovery + CI, analytic-q ==
// majority-of-k simulation, adaptive allocation hits a target with fewer total
// replays + concentrates on mid-range, the overdispersion check FIRES on shared-shock
// and PASSES on clean iid, graceful degenerate handling, and the off-mode anchor.

// --- 1. Wilson CI coverage (vs Monte-Carlo truth) --------------------------

// TestWilsonCICoverage checks the Wilson 95% interval's COVERAGE against a seeded
// Monte-Carlo: over many simulated binomial samples at a known true p, the fraction
// of intervals that contain p must be ~0.95 — INCLUDING p near the 0/1 boundary
// where the Wald interval fails (it under-covers badly at the edges). We assert
// coverage is close to nominal at p in {0.5, 0.9, 0.05}.
func TestWilsonCICoverage(t *testing.T) {
	cases := []struct {
		p      float64
		n      int
		minCov float64 // Wilson is slightly conservative; require >= this
	}{
		{0.50, 30, 0.93},
		{0.90, 30, 0.90}, // near the boundary — Wald would collapse here
		{0.05, 40, 0.88}, // near 0 — Wilson stays valid; Wald gives lo<0
	}
	const trials = 20000
	rng := cpyrand.New(0x5151)
	for _, c := range cases {
		covered := 0
		for tr := 0; tr < trials; tr++ {
			x := binomDraw(c.n, c.p, rng)
			lo, hi := wilsonCI(x, c.n, bernZ95)
			if lo <= c.p && c.p <= hi {
				covered++
			}
		}
		cov := float64(covered) / float64(trials)
		if cov < c.minCov {
			t.Errorf("Wilson coverage at p=%.2f n=%d: %.3f, want >= %.3f (boundary-correct CI must not under-cover)",
				c.p, c.n, cov, c.minCov)
		}
		// Wilson is bounded and conservative — it should not WILDLY over-cover either.
		if cov > 0.995 {
			t.Errorf("Wilson coverage at p=%.2f suspiciously high %.3f (interval too wide?)", c.p, cov)
		}
	}
}

// TestWilsonCIBoundaryProperties: at the data boundary (x=0 or x=n) the Wilson lower
// (resp. upper) bound is strictly inside (0,1) — the property the Wald interval lacks
// (Wald gives a zero-width interval at the boundary, falsely certain).
func TestWilsonCIBoundaryProperties(t *testing.T) {
	// x=0, n=20: p̂=0 but the CI upper bound must be > 0 (we are NOT certain p=0).
	lo, hi := wilsonCI(0, 20, bernZ95)
	if lo != 0 {
		t.Errorf("Wilson lo at x=0 must be 0, got %g", lo)
	}
	if hi <= 0 || hi >= 1 {
		t.Errorf("Wilson hi at x=0/n=20 must be in (0,1), got %g (Wald would give 0 — falsely certain)", hi)
	}
	// x=n: p̂=1 but the lower bound must be < 1.
	lo2, hi2 := wilsonCI(20, 20, bernZ95)
	if hi2 != 1 {
		t.Errorf("Wilson hi at x=n must be 1, got %g", hi2)
	}
	if lo2 <= 0 || lo2 >= 1 {
		t.Errorf("Wilson lo at x=n=20 must be in (0,1), got %g", lo2)
	}
	// width shrinks with n (more data → tighter).
	_, h10 := wilsonCI(5, 10, bernZ95)
	l10, _ := wilsonCI(5, 10, bernZ95)
	_, h100 := wilsonCI(50, 100, bernZ95)
	l100, _ := wilsonCI(50, 100, bernZ95)
	if (h100 - l100) >= (h10 - l10) {
		t.Errorf("Wilson width must shrink with n: width(n=100)=%g >= width(n=10)=%g", h100-l100, h10-l10)
	}
}

// --- 2. variance-by-formula ------------------------------------------------

// TestVarianceByFormula: the per-task outcome variance is EXACTLY p̂(1−p̂) and the
// mean SE is sqrt(p̂(1−p̂)/K) — the Bernoulli variance-is-a-function-of-the-mean
// shortcut (no repeated-launch estimate needed).
func TestVarianceByFormula(t *testing.T) {
	in := BernTaskInput{TaskID: "t", Solved: 7, K: 10}
	e := estimateBernTask(in, 0)
	if !approxEst(e.PHat, 0.7, 1e-12) {
		t.Errorf("p̂ = %g, want 0.7", e.PHat)
	}
	wantVar := 0.7 * 0.3
	if !approxEst(e.OutcomeVar, wantVar, 1e-12) {
		t.Errorf("outcome variance %g, want p(1-p)=%g", e.OutcomeVar, wantVar)
	}
	wantSE := math.Sqrt(wantVar / 10)
	if !approxEst(e.SEMean, wantSE, 1e-12) {
		t.Errorf("SE of mean %g, want sqrt(p(1-p)/K)=%g", e.SEMean, wantSE)
	}
	// p=0.5 is the maximal-variance point.
	half := estimateBernTask(BernTaskInput{Solved: 5, K: 10}, 0)
	if half.OutcomeVar <= e.OutcomeVar {
		t.Errorf("p=0.5 variance (%g) must exceed p=0.7 variance (%g) — 0.5 is the max", half.OutcomeVar, e.OutcomeVar)
	}
	// saturated task: variance 0, NOT informative.
	sat := estimateBernTask(BernTaskInput{Solved: 10, K: 10}, 0)
	if sat.OutcomeVar != 0 {
		t.Errorf("saturated p=1 variance must be 0, got %g", sat.OutcomeVar)
	}
	if sat.Informative {
		t.Errorf("a saturated task must not be flagged informative")
	}
}

// --- 3. two-proportion test recovers a planted effect + correct CI ---------

// TestTwoProportionRecoversPlantedEffect: build OFF/ON arms with a KNOWN per-task
// effect at high K; the per-task diff recovers it, the aggregate CI brackets the
// truth, and a task with a large clean effect is flagged MOVED while a no-effect task
// is not.
func TestTwoProportionRecoversPlantedEffect(t *testing.T) {
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking, CapMultiHopGrounding}
	K := 200
	// big: OFF p=0.30, ON p=0.70 (+0.40, large, should MOVE).
	// none: OFF p=0.50, ON p=0.50 (0, should NOT move).
	// small: OFF p=0.60, ON p=0.65 (+0.05, may or may not move at this K).
	offs := []BernTaskInput{
		{TaskID: "big", Capability: caps[0], Solved: 60, K: K},
		{TaskID: "none", Capability: caps[1], Solved: 100, K: K},
		{TaskID: "small", Capability: caps[2], Solved: 120, K: K},
	}
	ons := []BernTaskInput{
		{TaskID: "big", Capability: caps[0], Solved: 140, K: K},
		{TaskID: "none", Capability: caps[1], Solved: 100, K: K},
		{TaskID: "small", Capability: caps[2], Solved: 130, K: K},
	}
	rep := EstimateBernoulli(offs, ons, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})

	// per-task diffs recover the planted effects.
	byID := map[string]BernPairedTaskDiff{}
	for _, d := range rep.PerTaskDiff {
		byID[d.TaskID] = d
	}
	if !approxEst(byID["big"].Diff, 0.40, 1e-9) {
		t.Errorf("big diff %g, want +0.40", byID["big"].Diff)
	}
	if !approxEst(byID["none"].Diff, 0.0, 1e-9) {
		t.Errorf("none diff %g, want 0", byID["none"].Diff)
	}
	// big effect's CI must EXCLUDE 0 (MOVED); none must INCLUDE 0 (not moved).
	if !byID["big"].Moved {
		t.Errorf("big +0.40 effect at K=%d must be MOVED, CI[%g,%g]", K, byID["big"].CILo, byID["big"].CIHi)
	}
	if byID["none"].Moved {
		t.Errorf("none (0 effect) must NOT be flagged MOVED, CI[%g,%g]", byID["none"].CILo, byID["none"].CIHi)
	}
	// aggregate diff = mean of (0.40, 0, 0.05) = 0.15; its CI must bracket the truth.
	wantMean := (0.40 + 0.0 + 0.05) / 3
	if !approxEst(rep.MeanDiff, wantMean, 1e-9) {
		t.Errorf("aggregate diff %g, want %g", rep.MeanDiff, wantMean)
	}
	if !(rep.MeanDiffCILo < wantMean && wantMean < rep.MeanDiffCIHi) {
		t.Errorf("aggregate CI [%g,%g] must bracket the true mean %g", rep.MeanDiffCILo, rep.MeanDiffCIHi, wantMean)
	}
	// a wider CI at smaller K (sanity: precision scales with K).
	K2 := 20
	offsLo := []BernTaskInput{{TaskID: "big", Solved: 6, K: K2}, {TaskID: "none", Solved: 10, K: K2}, {TaskID: "small", Solved: 12, K: K2}}
	onsLo := []BernTaskInput{{TaskID: "big", Solved: 14, K: K2}, {TaskID: "none", Solved: 10, K: K2}, {TaskID: "small", Solved: 13, K: K2}}
	repLo := EstimateBernoulli(offsLo, onsLo, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if !(repLo.MeanDiffSE > rep.MeanDiffSE) {
		t.Errorf("aggregate SE at K=%d (%g) must exceed K=%d (%g)", K2, repLo.MeanDiffSE, K, rep.MeanDiffSE)
	}
}

// --- 4. analytic-q binomial-majority == simulation -------------------------

// TestAnalyticQMatchesSimulation: the analytic binomialMajority(p,k) must match a
// seeded Monte-Carlo simulation of majority-of-k iid Bernoulli(p) draws, across p and
// odd/even k. This is the formula the deliberative-K cross-check uses.
func TestAnalyticQMatchesSimulation(t *testing.T) {
	rng := cpyrand.New(0x9A0C) // any fixed seed
	cases := []struct {
		p float64
		k int
	}{
		{0.30, 3}, {0.70, 3}, {0.50, 3},
		{0.30, 5}, {0.70, 5}, {0.60, 4}, {0.40, 4}, {0.55, 7},
	}
	const sims = 200000
	for _, c := range cases {
		ana := binomialMajority(c.p, c.k)
		// simulate majority-of-k.
		win := 0
		for s := 0; s < sims; s++ {
			succ := 0
			for j := 0; j < c.k; j++ {
				if rng.Float64() < c.p {
					succ++
				}
			}
			switch {
			case 2*succ > c.k:
				win++
			case 2*succ == c.k:
				// even-k tie → fair coin (matches binomialMajority's 0.5 split).
				if rng.Float64() < 0.5 {
					win++
				}
			}
		}
		emp := float64(win) / float64(sims)
		if math.Abs(ana-emp) > 0.005 {
			t.Errorf("binomialMajority(p=%.2f,k=%d)=%.4f, simulation=%.4f (gap %.4f > 0.005)",
				c.p, c.k, ana, emp, math.Abs(ana-emp))
		}
	}
	// concentration direction: p>0.5 → q>p; p<0.5 → q<p; p=0.5 → q=0.5; k=1 → q=p.
	if !(binomialMajority(0.7, 5) > 0.7) {
		t.Errorf("majority concentration must push p=0.7 UP for k=5")
	}
	if !(binomialMajority(0.3, 5) < 0.3) {
		t.Errorf("majority concentration must push p=0.3 DOWN for k=5")
	}
	if !approxEst(binomialMajority(0.5, 5), 0.5, 1e-12) {
		t.Errorf("p=0.5 must stay 0.5 under majority")
	}
	if !approxEst(binomialMajority(0.42, 1), 0.42, 1e-12) {
		t.Errorf("k=1 must be identity (q=p)")
	}
}

// TestDeliberativeQEmpiricalVsAnalytic: the per-task deliberative read flags
// divergence when the empirical q̂ is FAR from the analytic q (the sub-episodes are
// not independent), and does NOT flag when they agree.
func TestDeliberativeQEmpiricalVsAnalytic(t *testing.T) {
	// base p=0.7, k=5 → analytic q≈0.837. A deliberative arm whose empirical q̂ matches
	// (≈0.84 at high K) must NOT diverge.
	base := BernTaskInput{TaskID: "t", Solved: 700, K: 1000}
	delibAgree := BernTaskInput{TaskID: "t", Solved: 837, K: 1000}
	dtAgree := estimateDeliberativeTask(base, delibAgree, 5, 3.0)
	if dtAgree.Diverged {
		t.Errorf("agreeing empirical q̂=%.3f vs analytic %.3f must NOT diverge", dtAgree.QEmpirical, dtAgree.QAnalytic)
	}
	// a deliberative arm that did NOT concentrate (q̂≈p, sub-episodes correlated) must
	// diverge from the analytic prediction of concentration.
	delibCorr := BernTaskInput{TaskID: "t", Solved: 700, K: 1000}
	dtCorr := estimateDeliberativeTask(base, delibCorr, 5, 3.0)
	if !dtCorr.Diverged {
		t.Errorf("a non-concentrating empirical q̂=%.3f vs analytic %.3f must DIVERGE (sub-episodes not independent)",
			dtCorr.QEmpirical, dtCorr.QAnalytic)
	}
}

// --- 5. adaptive allocation ------------------------------------------------

// TestAdaptiveAllocationConcentratesAndSaves: the adaptive recommender (a) hits the
// target half-width, (b) concentrates budget on mid-range p≈0.5 tasks and minimizes
// it on near-saturated tasks, and (c) the total adaptive budget is LESS than the
// uniform (worst-case) budget.
func TestAdaptiveAllocationConcentratesAndSaves(t *testing.T) {
	// a suite with one mid-range task (p=0.5) and several near-saturated tasks.
	offs := []BernTaskInput{
		{TaskID: "mid", Solved: 10, K: 20},   // p=0.5 — the demanding task
		{TaskID: "sat1", Solved: 19, K: 20},  // p=0.95 — near-saturated
		{TaskID: "sat0", Solved: 1, K: 20},   // p=0.05 — near-saturated
		{TaskID: "sat1b", Solved: 20, K: 20}, // p=1.0 — saturated
	}
	cfg := BernoulliConfig{Mode: EstBernOn, Delta: 0.15, TargetHalf: 0.10, AllocKMin: 4, AllocKMax: 1000}
	rep := EstimateBernoulli(offs, nil, nil, nil, nil, cfg)

	byID := map[string]BernAllocation{}
	for _, a := range rep.Allocation {
		byID[a.TaskID] = a
	}
	// concentration: the mid-range task gets the MOST replays; the saturated ones the
	// fewest (Fisher: p≈0.5 carries the most info; p≈0/1 carries ~0).
	if !(byID["mid"].RecommendedK > byID["sat1"].RecommendedK) {
		t.Errorf("mid-range p=0.5 (K=%d) must get MORE replays than near-saturated p=0.95 (K=%d)",
			byID["mid"].RecommendedK, byID["sat1"].RecommendedK)
	}
	if !(byID["mid"].RecommendedK > byID["sat0"].RecommendedK) {
		t.Errorf("mid-range p=0.5 (K=%d) must get MORE replays than near-saturated p=0.05 (K=%d)",
			byID["mid"].RecommendedK, byID["sat0"].RecommendedK)
	}
	// the exactly-saturated task floors to kMin (minimal but non-zero budget).
	if byID["sat1b"].RecommendedK != cfg.allocKMin() {
		t.Errorf("a fully-saturated task must floor to kMin=%d, got %d", cfg.allocKMin(), byID["sat1b"].RecommendedK)
	}
	// the mid-range task hits the target: its recommended K must give half-width <= target.
	wantK := int(math.Ceil(bernZ95 * bernZ95 * 0.25 / (0.10 * 0.10)))
	if byID["mid"].RecommendedK != wantK {
		t.Errorf("mid p=0.5 recommended K=%d, want %d (z²·0.25/target²)", byID["mid"].RecommendedK, wantK)
	}
	// total saving: adaptive < uniform (the uniform plan sizes EVERY task for the
	// worst-case p=0.5; adaptive sizes the saturated ones down).
	if !(rep.AllocSavingK > 0) {
		t.Errorf("adaptive allocation must SAVE replays vs uniform: adaptive=%d uniform=%d saving=%d",
			rep.AdaptiveTotalK, rep.UniformTotalK, rep.AllocSavingK)
	}
	if rep.AdaptiveTotalK >= rep.UniformTotalK {
		t.Errorf("adaptive total (%d) must be < uniform total (%d)", rep.AdaptiveTotalK, rep.UniformTotalK)
	}
}

// TestAllocationHitsTargetHalfWidth: at the recommended K, the achieved Wilson half-
// width is <= the target for the mid-range task (the allocation is correct, not just
// monotone).
func TestAllocationHitsTargetHalfWidth(t *testing.T) {
	target := 0.05
	est := estimateBernTask(BernTaskInput{Solved: 50, K: 100}, 0) // p=0.5
	alloc := recommendAllocation(est, target, bernZ95, 4, 100000)
	// simulate the achieved half-width at the recommended K with the same p̂.
	x := int(math.Round(est.PHat * float64(alloc.RecommendedK)))
	lo, hi := wilsonCI(x, alloc.RecommendedK, bernZ95)
	achieved := (hi - lo) / 2
	if achieved > target*1.05 { // small slack for the Wilson vs normal-approx gap
		t.Errorf("achieved half-width %.4f exceeds target %.4f at recommended K=%d", achieved, target, alloc.RecommendedK)
	}
}

// --- 6. overdispersion self-check ------------------------------------------

// TestOverdispersionCrossFiresOnSharedShock: the DEFINITIVE cross-launch check must
// FIRE (Overdispersed=true) on data with a planted shared launch shock, and PASS
// (Overdispersed=false) on clean per-call iid Bernoulli data.
func TestOverdispersionCrossFiresOnSharedShock(t *testing.T) {
	rng := cpyrand.New(0xD15)
	T := 6
	R := 8
	K := 10
	// CLEAN iid: each (launch,task) rate is an independent Binomial(K, p_task)/K, no
	// shared shock — the cross-launch variance should match p(1-p)/K.
	clean := make([][]float64, R)
	ks := make([]int, T)
	for c := 0; c < T; c++ {
		ks[c] = K
	}
	pTask := []float64{0.3, 0.4, 0.5, 0.6, 0.7, 0.45}
	for l := 0; l < R; l++ {
		row := make([]float64, T)
		for c := 0; c < T; c++ {
			row[c] = float64(binomDraw(K, pTask[c], rng)) / float64(K)
		}
		clean[l] = row
	}
	odClean := checkOverdispersionCross(clean, ks)
	if odClean.Overdispersed {
		t.Errorf("clean iid Bernoulli must NOT be flagged overdispersed (ratio=%.3f within=%.5f cross=%.5f)",
			odClean.Statistic, odClean.WithinVar, odClean.CrossVar)
	}

	// SHARED SHOCK: add a per-launch shock u_l to EVERY task's true p (the run effect the
	// Bernoulli formula assumes away), then draw each launch's per-task rate as a
	// BINOMIAL at the shocked p — the cross-launch variance now far exceeds the
	// within-launch Bernoulli expectation.
	shock := make([][]float64, R)
	for l := 0; l < R; l++ {
		u := 0.25 * gauss(rng) // big shared shock
		row := make([]float64, T)
		for c := 0; c < T; c++ {
			p := pTask[c] + u
			if p < 0 {
				p = 0
			}
			if p > 1 {
				p = 1
			}
			row[c] = float64(binomDraw(K, p, rng)) / float64(K)
		}
		shock[l] = row
	}
	odShock := checkOverdispersionCross(shock, ks)
	if !odShock.Overdispersed {
		t.Errorf("a planted shared launch shock MUST be flagged overdispersed (ratio=%.3f within=%.5f cross=%.5f)",
			odShock.Statistic, odShock.WithinVar, odShock.CrossVar)
	}
	if !(odShock.Statistic > odClean.Statistic) {
		t.Errorf("shock dispersion ratio (%.3f) must exceed clean (%.3f)", odShock.Statistic, odClean.Statistic)
	}
}

// TestOverdispersionSingleAdvisoryFlag: the single-launch Pearson screen sets only the
// ADVISORY PooledMisfit flag (NOT the verdict-driving Overdispersed): ~1 when the
// informative tasks share a common rate, raised when they are wildly heterogeneous —
// and CRUCIALLY the single-launch screen NEVER sets Overdispersed (only the
// cross-launch test does), because with one launch heterogeneity is indistinguishable
// from a shock and is benign for the per-task CIs.
func TestOverdispersionSingleAdvisoryFlag(t *testing.T) {
	// homogeneous informative tasks (all p≈0.5) at high K → dispersion ≈ 1, not flagged.
	homo := []BernTaskInput{
		{TaskID: "a", Solved: 50, K: 100},
		{TaskID: "b", Solved: 49, K: 100},
		{TaskID: "c", Solved: 51, K: 100},
		{TaskID: "d", Solved: 50, K: 100},
	}
	od := checkOverdispersionSingle(homo)
	if od.PooledMisfit {
		t.Errorf("homogeneous informative tasks must NOT trip the single-launch advisory (D=%.3f)", od.Statistic)
	}
	if od.Overdispersed {
		t.Errorf("the single-launch screen must NEVER set Overdispersed (only cross-launch does)")
	}
	// wildly heterogeneous (some p≈0.1, some p≈0.9) → high dispersion, ADVISORY flag set
	// but the verdict-driving Overdispersed stays false.
	hetero := []BernTaskInput{
		{TaskID: "a", Solved: 10, K: 100},
		{TaskID: "b", Solved: 90, K: 100},
		{TaskID: "c", Solved: 15, K: 100},
		{TaskID: "d", Solved: 85, K: 100},
	}
	odH := checkOverdispersionSingle(hetero)
	if !odH.PooledMisfit {
		t.Errorf("wildly heterogeneous tasks must trip the single-launch ADVISORY (D=%.3f)", odH.Statistic)
	}
	if odH.Overdispersed {
		t.Errorf("the single-launch screen must NEVER set Overdispersed even on heterogeneity (D=%.3f)", odH.Statistic)
	}
}

// TestOverdispersionVerdictFallsBack: when the cross-launch check fires, the full
// report's VERDICT is OVERDISPERSED (the graceful fallback signal), NOT a confident
// FEASIBLE — the method refuses to report a wrong Bernoulli variance.
func TestOverdispersionVerdictFallsBack(t *testing.T) {
	rng := cpyrand.New(0xFA11)
	T := 6
	R := 12
	K := 20
	ks := make([]int, T)
	for c := range ks {
		ks[c] = K
	}
	// pTask centered so a σ=0.25 shared shock survives the [0,1] clamp; each launch's
	// per-task rate is a BINOMIAL DRAW at the shocked p (realistic — the within term and
	// the cross term are then on the same footing). The shock shows up as cross-launch
	// variance EXCEEDING the within-launch Bernoulli expectation — the definitive test.
	pTask := []float64{0.3, 0.4, 0.5, 0.6, 0.5, 0.45}
	shock := make([][]float64, R)
	for l := 0; l < R; l++ {
		u := 0.25 * gauss(rng) // big shared launch shock
		row := make([]float64, T)
		for c := 0; c < T; c++ {
			p := pTask[c] + u
			if p < 0 {
				p = 0
			}
			if p > 1 {
				p = 1
			}
			row[c] = float64(binomDraw(K, p, rng)) / float64(K)
		}
		shock[l] = row
	}
	offs := []BernTaskInput{
		{TaskID: "a", Solved: 3, K: K}, {TaskID: "b", Solved: 4, K: K}, {TaskID: "c", Solved: 5, K: K},
		{TaskID: "d", Solved: 6, K: K}, {TaskID: "e", Solved: 5, K: K}, {TaskID: "f", Solved: 4, K: K},
	}
	ons := []BernTaskInput{
		{TaskID: "a", Solved: 7, K: K}, {TaskID: "b", Solved: 8, K: K}, {TaskID: "c", Solved: 9, K: K},
		{TaskID: "d", Solved: 8, K: K}, {TaskID: "e", Solved: 9, K: K}, {TaskID: "f", Solved: 7, K: K},
	}
	rep := EstimateBernoulli(offs, ons, nil, shock, ks, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if rep.Verdict != BernOverdispersed {
		t.Errorf("a shared-shock cross-launch matrix must yield VERDICT=OVERDISPERSED, got %s (ratio=%.3f within=%.5f cross=%.5f)",
			rep.Verdict, rep.Overdispersion.Statistic, rep.Overdispersion.WithinVar, rep.Overdispersion.CrossVar)
	}
	if !rep.Overdispersion.Overdispersed {
		t.Errorf("the overdispersion flag must be set")
	}
}

// --- 7. graded-signal mode -------------------------------------------------

// TestGradedModeLowerVariance: a graded (continuous) per-attempt score gives a per-
// task SE from the graded values; with a faithful proxy of lower spread it is tighter
// than the binary p̂ SE. We assert the graded SE is computed and is the analytic
// SD/sqrt(N).
func TestGradedModeLowerVariance(t *testing.T) {
	in := BernTaskInput{TaskID: "g", Solved: 5, K: 10, GradedMean: 0.5, GradedSD: 0.1, GradedN: 10}
	e := estimateBernTask(in, 0)
	if !e.GradedUsed {
		t.Fatalf("graded mode must engage when GradedN>=2")
	}
	wantSE := 0.1 / math.Sqrt(10)
	if !approxEst(e.GradedSE, wantSE, 1e-12) {
		t.Errorf("graded SE %g, want SD/sqrt(N)=%g", e.GradedSE, wantSE)
	}
	// the graded SE (lower-spread continuous proxy) is tighter than the binary p̂ SE
	// (which is sqrt(0.25/10)=0.158 at p=0.5).
	if !(e.GradedSE < e.SEMean) {
		t.Errorf("a lower-spread graded proxy SE (%g) should be tighter than the binary SE (%g)", e.GradedSE, e.SEMean)
	}
	// GradedN<2 → graded mode does NOT engage (graceful).
	e2 := estimateBernTask(BernTaskInput{Solved: 5, K: 10, GradedMean: 0.5, GradedSD: 0.1, GradedN: 1}, 0)
	if e2.GradedUsed {
		t.Errorf("graded mode must NOT engage with GradedN<2")
	}
}

// TestGradedBootstrapCIDeterministic: the optional seeded graded bootstrap CI is
// deterministic (same values + seed → identical CI) and brackets the mean.
func TestGradedBootstrapCIDeterministic(t *testing.T) {
	vals := []float64{0.2, 0.4, 0.5, 0.6, 0.8, 0.5, 0.3, 0.7}
	m1, lo1, hi1 := gradedMeanBootstrapCI(vals, 2000, 0x1234, bernZ95)
	m2, lo2, hi2 := gradedMeanBootstrapCI(vals, 2000, 0x1234, bernZ95)
	if m1 != m2 || lo1 != lo2 || hi1 != hi2 {
		t.Errorf("graded bootstrap not deterministic: (%g,%g,%g) vs (%g,%g,%g)", m1, lo1, hi1, m2, lo2, hi2)
	}
	if !(lo1 <= m1 && m1 <= hi1) {
		t.Errorf("bootstrap CI [%g,%g] must bracket the mean %g", lo1, hi1, m1)
	}
}

// --- 7b. decline-ordinal graded scorer + leakage guard ---------------------

// (declineTask is the package-level OracleDecline fixture from canonical_test.go,
// PriorLure "3" — reused here so the decline-ordinal scorer is exercised against the SAME
// canonicalAnswer split the deliberative reconciler votes on.)

// TestDeclineOrdinalScorer: the decline-ordinal graded scorer reuses canonicalAnswer's
// decline split — clean honest decline → 1.0, a confabulated/committed number (incl. the
// lure) → 0.0, empty/ambiguous/hedge → 0.0 (strict). It is defined ONLY for OracleDecline
// (ok=false otherwise). The graded score must AGREE with the binary oracle's solved bit on
// each case (they read the SAME final answer) — the coherence that makes the graded score
// a valid binary proxy.
func TestDeclineOrdinalScorer(t *testing.T) {
	task := declineTask
	cases := []struct {
		name   string
		answer string
		want   float64
		solved bool // what the binary oracle scores — must match the graded sign
	}{
		{"clean decline", "This value is not determinable from the file.", 1.0, true},
		{"decline marker no number", "The RetryBudget is not defined in this material.", 1.0, true},
		{"confabulated lure number", "RetryBudget is 3.", 0.0, false},
		{"confabulated other number", "I'd estimate it is 7.", 0.0, false},
		{"hedged decline citing lure", "It's hard to say, but probably around 3.", 0.0, false},
		{"empty answer (give-up)", "", 0.0, false},
		{"ambiguous non-answer", "The scheduler wires several knobs together.", 0.0, false},
	}
	for _, c := range cases {
		got, ok := declineOrdinal(task, c.answer)
		if !ok {
			t.Errorf("[%s] declineOrdinal must define a score for an OracleDecline task", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("[%s] declineOrdinal(%q)=%.1f, want %.1f", c.name, c.answer, got, c.want)
		}
		// coherence with the binary oracle: graded==1 iff the oracle solved it.
		solved := Score(task, c.answer).Solved
		if solved != c.solved {
			t.Errorf("[%s] binary oracle solved=%v, test expected %v (scorer/oracle drift)", c.name, solved, c.solved)
		}
		if (got == 1.0) != solved {
			t.Errorf("[%s] graded sign (%.1f) must match binary solved (%v) — they read the same answer", c.name, got, solved)
		}
	}
}

// TestDeclineOrdinalOnlyDecline: the scorer returns ok=false for every non-decline oracle
// kind (no graded signal is defined there — the producer leaves Graded unset).
func TestDeclineOrdinalOnlyDecline(t *testing.T) {
	for _, oracle := range []OracleKind{OracleExact, OracleNumericTolerance, OracleSetMembership} {
		task := Task{ID: "x", Oracle: oracle, Expected: "5"}
		if _, ok := declineOrdinal(task, "5"); ok {
			t.Errorf("declineOrdinal must NOT define a graded score for oracle %q", oracle)
		}
	}
}

// TestDeclineOrdinalReusesCanonicalSplit: the scorer is keyed on the EXACT canonicalAnswer
// decline split (the red-team's coherence fence) — graded==1 iff canonicalAnswer==declineVoteKey.
func TestDeclineOrdinalReusesCanonicalSplit(t *testing.T) {
	task := declineTask
	answers := []string{
		"not determinable from this file",
		"the value is 3",
		"unable to determine the retry budget",
		"",
		"a plain sentence with no marker",
		"cannot determine — perhaps 3 though",
	}
	for _, a := range answers {
		got, ok := declineOrdinal(task, a)
		if !ok {
			t.Fatalf("decline task must yield ok=true")
		}
		isDeclineKey := canonicalAnswer(task, a) == declineVoteKey
		if (got == 1.0) != isDeclineKey {
			t.Errorf("answer %q: graded=%.1f but canonicalAnswer==DECLINE is %v — the scorer must key on the SAME split",
				a, got, isDeclineKey)
		}
	}
}

// TestSaturatedDivergenceGuard: the saturated-divergence sub-guard FIRES when a graded
// score MOVES on a task whose binary p̂ is pinned at 0/1 (the contamination signature) and
// PASSES when graded == p̂ on the saturated task (clean). On a fire, the graded path is
// DROPPED (GradedUsed false on all per-task estimates) and the verdict is GRADED-LEAK.
func TestSaturatedDivergenceGuard(t *testing.T) {
	// CLEAN: a saturated p̂=1 task whose graded mean equals 1.0 (graded agrees with the
	// pinned binary outcome) + an informative task with a coherent graded value. No trip.
	K := 50
	cleanOff := []BernTaskInput{
		{TaskID: "sat", Solved: K, K: K, GradedMean: 1.0, GradedSD: 0.0, GradedN: K},  // p̂=1, graded=1 (agree)
		{TaskID: "inf", Solved: 25, K: K, GradedMean: 0.5, GradedSD: 0.1, GradedN: K}, // informative
	}
	g := applyGradedGuard(cleanOff, nil)
	if !g.Present {
		t.Fatalf("guard must register the graded vectors as present")
	}
	if g.SaturatedDiverged {
		t.Errorf("clean saturated task (graded==p̂) must NOT trip the saturated-divergence guard (swing=%.3f)", g.MaxSatSwing)
	}
	if !g.Used {
		t.Errorf("clean graded data must be USED (both sub-guards pass)")
	}

	// CONTAMINATED: the SAME saturated p̂=1 task but its graded mean is 0.3 — the graded
	// score MOVED where the binary outcome is pinned (manufacturing variance). Must trip.
	contamOff := []BernTaskInput{
		{TaskID: "sat", Solved: K, K: K, GradedMean: 0.30, GradedSD: 0.2, GradedN: K}, // p̂=1, graded=0.3 (MOVED)
		{TaskID: "inf", Solved: 25, K: K, GradedMean: 0.5, GradedSD: 0.1, GradedN: K},
	}
	gc := applyGradedGuard(contamOff, nil)
	if !gc.SaturatedDiverged {
		t.Errorf("a graded score that moved on a saturated p̂=1 task MUST trip saturated-divergence (swing=%.3f > ε=%.2f)", gc.MaxSatSwing, gradedSatEps)
	}
	if gc.Used {
		t.Errorf("a tripped guard must NOT use graded")
	}
	if gc.MaxSatSwing <= gradedSatEps {
		t.Errorf("max saturated swing %.3f should exceed ε=%.2f on the contaminated case", gc.MaxSatSwing, gradedSatEps)
	}

	// end-to-end: the contaminated case yields VERDICT=GRADED-LEAK and the per-task graded
	// is DROPPED (GradedUsed false everywhere); the binary p̂ is untouched.
	rep := EstimateBernoulli(contamOff, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if rep.Verdict != BernGradedLeak {
		t.Errorf("contaminated graded must yield VERDICT=GRADED-LEAK, got %s", rep.Verdict)
	}
	for _, e := range rep.PerTask {
		if e.GradedUsed {
			t.Errorf("task %s: GradedUsed must be FALSE after a leak drop", e.TaskID)
		}
	}
	// the binary p̂ survives the drop (the report falls back to binary, not nothing).
	byID := map[string]BernTaskEstimate{}
	for _, e := range rep.PerTask {
		byID[e.TaskID] = e
	}
	if byID["sat"].PHat != 1.0 {
		t.Errorf("binary p̂ must survive a graded drop, got %.3f", byID["sat"].PHat)
	}
}

// TestArmRankSignGuard: the arm-rank-sign sub-guard FIRES when the graded ON−OFF
// direction disagrees in SIGN with the binary ON−OFF direction on an informative task,
// and PASSES when they agree. A fire drops graded → VERDICT=GRADED-LEAK.
func TestArmRankSignGuard(t *testing.T) {
	K := 100
	// AGREEMENT: ON improves the binary outcome (Solved 40→70) AND the graded mean moves
	// the SAME way (0.40→0.70). No flip.
	offAgree := []BernTaskInput{{TaskID: "t", Solved: 40, K: K, GradedMean: 0.40, GradedSD: 0.1, GradedN: K}}
	onAgree := []BernTaskInput{{TaskID: "t", Solved: 70, K: K, GradedMean: 0.70, GradedSD: 0.1, GradedN: K}}
	ga := applyGradedGuard(offAgree, onAgree)
	if ga.ArmRankFlipped {
		t.Errorf("agreeing arm directions (binary + graded both ON>OFF) must NOT trip the arm-rank guard (flipped on %v)", ga.FlippedTasks)
	}
	if !ga.Used {
		t.Errorf("agreeing graded data must be USED")
	}

	// SIGN FLIP: the binary outcome IMPROVES under ON (40→70) but the graded mean DROPS
	// (0.40→0.20) — graded ranks the arms the OPPOSITE way from the oracle. Must trip.
	offFlip := []BernTaskInput{{TaskID: "t", Solved: 40, K: K, GradedMean: 0.40, GradedSD: 0.1, GradedN: K}}
	onFlip := []BernTaskInput{{TaskID: "t", Solved: 70, K: K, GradedMean: 0.20, GradedSD: 0.1, GradedN: K}}
	gf := applyGradedGuard(offFlip, onFlip)
	if !gf.ArmRankFlipped {
		t.Errorf("a graded sign flip vs the binary ON−OFF MUST trip the arm-rank guard")
	}
	if gf.Used {
		t.Errorf("a tripped arm-rank guard must NOT use graded")
	}

	// end-to-end: the flip yields VERDICT=GRADED-LEAK with graded dropped.
	rep := EstimateBernoulli(offFlip, onFlip, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if rep.Verdict != BernGradedLeak {
		t.Errorf("an arm-rank sign flip must yield VERDICT=GRADED-LEAK, got %s", rep.Verdict)
	}
	for _, e := range rep.PerTask {
		if e.GradedUsed {
			t.Errorf("task %s: GradedUsed must be FALSE after an arm-rank flip drop", e.TaskID)
		}
	}
}

// TestGradedUsedGatedBehindBoth: GradedUsed flips ON only when BOTH sub-guards pass —
// the saturated-divergence guard alone passing is NOT enough if the arm-rank flips, and
// vice versa.
func TestGradedUsedGatedBehindBoth(t *testing.T) {
	K := 100
	// both pass: informative tasks, graded agrees in rank, no saturated movement.
	offOK := []BernTaskInput{
		{TaskID: "a", Solved: 30, K: K, GradedMean: 0.30, GradedSD: 0.1, GradedN: K},
		{TaskID: "b", Solved: 50, K: K, GradedMean: 0.50, GradedSD: 0.1, GradedN: K},
	}
	onOK := []BernTaskInput{
		{TaskID: "a", Solved: 60, K: K, GradedMean: 0.60, GradedSD: 0.1, GradedN: K},
		{TaskID: "b", Solved: 70, K: K, GradedMean: 0.70, GradedSD: 0.1, GradedN: K},
	}
	repOK := EstimateBernoulli(offOK, onOK, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if !repOK.Graded.Used {
		t.Errorf("both-pass case must USE graded")
	}
	usedCount := 0
	for _, e := range repOK.PerTask {
		if e.GradedUsed {
			usedCount++
		}
	}
	if usedCount == 0 {
		t.Errorf("both-pass case must keep per-task GradedUsed ON for graded tasks")
	}

	// saturated passes but arm-rank flips → DROP.
	offMix := []BernTaskInput{{TaskID: "a", Solved: 30, K: K, GradedMean: 0.30, GradedSD: 0.1, GradedN: K}}
	onMix := []BernTaskInput{{TaskID: "a", Solved: 60, K: K, GradedMean: 0.10, GradedSD: 0.1, GradedN: K}} // binary up, graded down
	repMix := EstimateBernoulli(offMix, onMix, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if repMix.Graded.Used {
		t.Errorf("arm-rank flip (even with no saturated divergence) must DROP graded")
	}
}

// TestGradedSEShrinksOnInformativeUnchangedOnSaturated: the honest claim — on a clean,
// guard-passing informative decline task the graded SE is TIGHTER than the binary SE
// (lower-variance proxy), while a saturated task's binary quantities are UNCHANGED by the
// graded path. This is the only kept-if condition: informative SE shrinks AND identical
// arm-rank AND no saturated movement.
func TestGradedSEShrinksOnInformativeUnchangedOnSaturated(t *testing.T) {
	K := 50
	// informative task at p=0.5 (binary SE = sqrt(0.25/50)). A clean graded proxy with
	// lower spread (SD=0.1) gives SE=0.1/sqrt(50) < binary SE. The saturated task agrees.
	off := []BernTaskInput{
		{TaskID: "inf", Solved: 25, K: K, GradedMean: 0.50, GradedSD: 0.10, GradedN: K},
		{TaskID: "sat", Solved: K, K: K, GradedMean: 1.00, GradedSD: 0.00, GradedN: K},
	}
	rep := EstimateBernoulli(off, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if rep.Verdict == BernGradedLeak {
		t.Fatalf("clean graded data must NOT trip the leak guard")
	}
	byID := map[string]BernTaskEstimate{}
	for _, e := range rep.PerTask {
		byID[e.TaskID] = e
	}
	inf := byID["inf"]
	if !inf.GradedUsed {
		t.Fatalf("clean informative task must USE graded")
	}
	if !(inf.GradedSE < inf.SEMean) {
		t.Errorf("informative decline task: graded SE (%.4f) must be TIGHTER than binary SE (%.4f) — the honest claim", inf.GradedSE, inf.SEMean)
	}
	// the saturated task's BINARY quantities are unchanged by the graded path (p̂=1, var=0,
	// binary SE=0): the graded enrichment only adds resolution on the informative task.
	sat := byID["sat"]
	if sat.PHat != 1.0 || sat.OutcomeVar != 0 || sat.SEMean != 0 {
		t.Errorf("saturated task binary quantities must be unchanged: p̂=%.3f var=%.3f SE=%.3f", sat.PHat, sat.OutcomeVar, sat.SEMean)
	}
	// determinism: re-running gives the identical guard result + SE.
	rep2 := EstimateBernoulli(off, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if rep2.Graded.Used != rep.Graded.Used || rep2.PerTask[0].GradedSE != rep.PerTask[0].GradedSE {
		t.Errorf("graded path must be deterministic")
	}
}

// TestLaunchTaskCountsGradedAccumulation: launchTaskCounts is the PRODUCER — it
// accumulates the per-attempt graded scores (HasGraded results only) into per-task
// GradedMean/GradedSD/GradedN, leaving them unset for tasks with no graded result.
func TestLaunchTaskCountsGradedAccumulation(t *testing.T) {
	results := []RunResult{
		// decline task d1: 3 harness attempts, 2 clean declines (1.0) + 1 confab (0.0).
		{TaskID: "d1", Arm: ArmHarness, Verdict: Verdict{Solved: true}, Graded: 1.0, HasGraded: true},
		{TaskID: "d1", Arm: ArmHarness, Verdict: Verdict{Solved: true}, Graded: 1.0, HasGraded: true},
		{TaskID: "d1", Arm: ArmHarness, Verdict: Verdict{Solved: false}, Graded: 0.0, HasGraded: true},
		// a bare-arm result must be IGNORED (the Bernoulli question is the harness arm).
		{TaskID: "d1", Arm: ArmBare, Verdict: Verdict{Solved: true}, Graded: 1.0, HasGraded: true},
		// a non-decline task n1: no graded → GradedN stays 0.
		{TaskID: "n1", Arm: ArmHarness, Verdict: Verdict{Solved: true}, HasGraded: false},
		{TaskID: "n1", Arm: ArmHarness, Verdict: Verdict{Solved: false}, HasGraded: false},
	}
	taskIDs := []string{"d1", "n1"}
	caps := []Capability{CapAntiConfabulation, CapMultiHopGrounding}
	counts := launchTaskCounts(results, taskIDs, caps)
	byID := map[string]BernTaskInput{}
	for _, c := range counts {
		byID[c.TaskID] = c
	}
	d1 := byID["d1"]
	if d1.K != 3 || d1.Solved != 2 {
		t.Errorf("d1 binary must be 2/3 (bare ignored), got %d/%d", d1.Solved, d1.K)
	}
	if d1.GradedN != 3 {
		t.Errorf("d1 GradedN must be 3 (harness graded attempts only), got %d", d1.GradedN)
	}
	wantMean := (1.0 + 1.0 + 0.0) / 3
	if !approxEst(d1.GradedMean, wantMean, 1e-12) {
		t.Errorf("d1 GradedMean %.4f, want %.4f", d1.GradedMean, wantMean)
	}
	if !(d1.GradedSD > 0) {
		t.Errorf("d1 GradedSD must be > 0 (the 1,1,0 vector has spread), got %.4f", d1.GradedSD)
	}
	// coherence: GradedMean must equal the binary solve-rate for a decline task (both
	// read the same final answer — the decline-ordinal IS the binary outcome here).
	if !approxEst(d1.GradedMean, float64(d1.Solved)/float64(d1.K), 1e-12) {
		t.Errorf("decline GradedMean (%.4f) must equal binary rate (%.4f) — same answer surface", d1.GradedMean, float64(d1.Solved)/float64(d1.K))
	}
	n1 := byID["n1"]
	if n1.GradedN != 0 {
		t.Errorf("non-graded task must have GradedN=0 (graded disengaged), got %d", n1.GradedN)
	}
}

// --- 8. degenerate / off-mode anchors --------------------------------------

// TestBernoulliDegenerateHandling: empty input, all-saturated, no-AB → never a
// confident FEASIBLE; graceful DEGENERATE.
func TestBernoulliDegenerateHandling(t *testing.T) {
	// empty.
	repEmpty := EstimateBernoulli(nil, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn})
	if repEmpty.Verdict == BernFeasible {
		t.Errorf("empty input must never be FEASIBLE, got %s", repEmpty.Verdict)
	}
	// all-saturated single arm → T_eff=0 → DEGENERATE.
	sat := []BernTaskInput{{TaskID: "a", Solved: 10, K: 10}, {TaskID: "b", Solved: 0, K: 10}}
	repSat := EstimateBernoulli(sat, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn})
	if repSat.Verdict != BernDegenerate {
		t.Errorf("all-saturated single arm must be DEGENERATE, got %s (T_eff=%d)", repSat.Verdict, repSat.TaskEff)
	}
	// informative single arm but NO AB contrast → DEGENERATE (no effect to gate),
	// never FEASIBLE.
	inf := []BernTaskInput{{TaskID: "a", Solved: 5, K: 10}, {TaskID: "b", Solved: 6, K: 10}}
	repInf := EstimateBernoulli(inf, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOn})
	if repInf.Verdict != BernDegenerate {
		t.Errorf("single-arm (no AB) must be DEGENERATE, got %s", repInf.Verdict)
	}
	if repInf.TaskEff != 2 {
		t.Errorf("two informative tasks must count T_eff=2, got %d", repInf.TaskEff)
	}
	// K=0 task → graceful (no panic, p̂=0).
	zero := estimateBernTask(BernTaskInput{Solved: 0, K: 0}, 0)
	if zero.PHat != 0 || zero.OutcomeVar != 0 {
		t.Errorf("K=0 must give p̂=0,var=0 gracefully, got p̂=%g var=%g", zero.PHat, zero.OutcomeVar)
	}
}

// TestBernoulliFeasibleResolvesEffect: a clean, non-overdispersed AB contrast with a
// resolved positive aggregate effect yields FEASIBLE.
func TestBernoulliFeasibleResolvesEffect(t *testing.T) {
	K := 300
	// every task moves +0.2, high K → tight CI clearing 0; homogeneous → not flagged.
	offs := []BernTaskInput{
		{TaskID: "a", Solved: 120, K: K}, {TaskID: "b", Solved: 150, K: K}, {TaskID: "c", Solved: 135, K: K},
	}
	ons := []BernTaskInput{
		{TaskID: "a", Solved: 180, K: K}, {TaskID: "b", Solved: 210, K: K}, {TaskID: "c", Solved: 195, K: K},
	}
	rep := EstimateBernoulli(offs, ons, nil, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15})
	if rep.Verdict != BernFeasible {
		t.Errorf("a resolved +0.2 effect at K=%d on iid data must be FEASIBLE, got %s (CI[%g,%g] disp=%.3f)",
			K, rep.Verdict, rep.MeanDiffCILo, rep.MeanDiffCIHi, rep.Overdispersion.Statistic)
	}
	if !(rep.MeanDiffCILo > 0) {
		t.Errorf("the aggregate CI lower bound (%g) must clear 0 for FEASIBLE", rep.MeanDiffCILo)
	}
}

// --- 9. off-mode regression anchor (no behavior change) --------------------

// TestBernoulliOffModeAnchor: Mode=off (and the zero-value default) runs NO Bernoulli
// pass — it returns the degenerate counts-only anchor with no per-task estimates, no
// allocation, no gate. This is the byte-identical-when-off guarantee at the estimator
// level (the cmd path is gated by --bernoulli=false → the branch is never entered).
func TestBernoulliOffModeAnchor(t *testing.T) {
	offs := []BernTaskInput{{TaskID: "a", Solved: 5, K: 10}, {TaskID: "b", Solved: 6, K: 10}}
	repOff := EstimateBernoulli(offs, nil, nil, nil, nil, BernoulliConfig{Mode: EstBernOff})
	if repOff.Verdict != BernDegenerate {
		t.Errorf("off-mode verdict must be DEGENERATE (no pass), got %s", repOff.Verdict)
	}
	if len(repOff.PerTask) != 0 {
		t.Errorf("off-mode must run no per-task pass, got %d estimates", len(repOff.PerTask))
	}
	if len(repOff.Allocation) != 0 {
		t.Errorf("off-mode must run no allocation, got %d", len(repOff.Allocation))
	}
	if repOff.Tasks != 2 {
		t.Errorf("off-mode anchor still reports the task count, got %d", repOff.Tasks)
	}
	// the zero-value config (Mode "") also routes to off.
	repDefault := EstimateBernoulli(offs, nil, nil, nil, nil, BernoulliConfig{})
	if repDefault.Verdict != BernDegenerate || len(repDefault.PerTask) != 0 {
		t.Errorf("zero-value config must route to off (DEGENERATE, no pass), got %s / %d estimates",
			repDefault.Verdict, len(repDefault.PerTask))
	}
	// Render of an off-mode report is the anchor banner (no panic).
	if s := repOff.Render(); len(s) == 0 {
		t.Errorf("off-mode Render must produce the anchor banner")
	}
}

// TestRenderNonEmpty: the full Bernoulli report renders without panic and is non-empty
// across the interesting branches (capability + deliberative + overdispersion).
func TestRenderNonEmpty(t *testing.T) {
	K := 100
	offs := []BernTaskInput{{TaskID: "a", Solved: 30, K: K}, {TaskID: "b", Solved: 50, K: K}}
	ons := []BernTaskInput{{TaskID: "a", Solved: 60, K: K}, {TaskID: "b", Solved: 55, K: K}}
	delib := []BernTaskInput{{TaskID: "a", Solved: 65, K: K}, {TaskID: "b", Solved: 50, K: K}}
	rep := EstimateBernoulli(offs, ons, delib, nil, nil, BernoulliConfig{Mode: EstBernOn, Delta: 0.15, DeliberativeK: 5})
	s := rep.Render()
	for _, want := range []string{"BERNOULLI HIGH-K", "PER-TASK CAPABILITY", "TWO-PROPORTION", "DELIBERATIVE", "ADAPTIVE-K", "OVERDISPERSION"} {
		if !contains(s, want) {
			t.Errorf("rendered report missing section %q", want)
		}
	}
}

// --- 10. launch reduction helper -------------------------------------------

// TestLaunchTaskCounts: one launch's RunResults reduce to per-task (solved, K) counts,
// ignoring the bare arm (the Bernoulli question is the harness arm).
func TestLaunchTaskCounts(t *testing.T) {
	results := []RunResult{
		{TaskID: "t1", Arm: ArmHarness, Verdict: Verdict{Solved: true}},
		{TaskID: "t1", Arm: ArmHarness, Verdict: Verdict{Solved: false}},
		{TaskID: "t1", Arm: ArmHarness, Verdict: Verdict{Solved: true}},
		{TaskID: "t2", Arm: ArmHarness, Verdict: Verdict{Solved: false}},
		{TaskID: "t1", Arm: ArmBare, Verdict: Verdict{Solved: true}}, // ignored
	}
	taskIDs := []string{"t1", "t2"}
	caps := []Capability{CapMultiHopGrounding, CapAdaptiveBacktracking}
	counts := launchTaskCounts(results, taskIDs, caps)
	if len(counts) != 2 {
		t.Fatalf("want 2 task counts, got %d", len(counts))
	}
	if counts[0].Solved != 2 || counts[0].K != 3 {
		t.Errorf("t1 must be 2/3 (bare ignored), got %d/%d", counts[0].Solved, counts[0].K)
	}
	if counts[1].Solved != 0 || counts[1].K != 1 {
		t.Errorf("t2 must be 0/1, got %d/%d", counts[1].Solved, counts[1].K)
	}
}

// --- helpers ----------------------------------------------------------------

// binomDraw draws a Binomial(n,p) count from the seeded cpyrand stream (deterministic).
func binomDraw(n int, p float64, rng *cpyrand.Random) int {
	x := 0
	for i := 0; i < n; i++ {
		if rng.Float64() < p {
			x++
		}
	}
	return x
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
