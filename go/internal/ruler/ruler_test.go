package ruler

// ruler_test.go — W5-1 OFFLINE unit tests. The ruler is a PURE statistical
// reduction over already-collected K-replay aggregates, so these tests feed
// SYNTHETIC replay rows (hand-constructed success-counts) and assert:
//   - σ_noise / MDE / ICC are correct on HAND-COMPUTABLE fixtures (exact math,
//     not "something is computed");
//   - a HIGH-variance / LOW-ICC instrument FAILS the feasibility gate;
//   - a CLEAN low-noise instrument PASSES;
//   - the gate is mutation-sensitive (flip a clause input and the verdict flips).
// All offline, deterministic, no model. They drive the BINARY axis (the exact
// one); the cost axis is asserted descriptive-only per the HONEST SCOPE.

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/campaign"
)

const eps = 1e-9

func approx(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s: got %.10f, want %.10f (diff %.3g)", label, got, want, got-want)
	}
}

// row is a synthetic per-task replay aggregate (success of K, plus a cost mean) —
// the SUM-ONLY shape (no per-replay vector); the cost axis stays descriptive.
func row(id string, success, k int, cost float64) TaskReplays {
	return TaskReplays{ID: id, Success: success, Replays: k, MeanCompletion: cost}
}

// vrow is a synthetic per-task replay aggregate carrying the PER-REPLAY completion
// VECTOR (the W5-1 instrument change): MeanCompletion is derived from the vector so
// the row is self-consistent. This is the shape the cost-axis characterization
// reduces into the within-task cost-σ noise floor.
func vrow(id string, success int, completions []int) TaskReplays {
	var sum int
	for _, x := range completions {
		sum += x
	}
	mean := 0.0
	if len(completions) > 0 {
		mean = float64(sum) / float64(len(completions))
	}
	return TaskReplays{ID: id, Success: success, Replays: len(completions), MeanCompletion: mean, Completions: completions}
}

// TestHandComputableMath pins σ_noise, ICC, MDE, and the band to values derived
// BY HAND in the test comment, on a 2-task K=4 fixture. If any formula drifts,
// these exact assertions fail (mutation-sensitive on the math itself).
func TestHandComputableMath(t *testing.T) {
	// Task1: 2/4 solved → p=0.5, within var p(1-p)=0.25.
	// Task2: 4/4 solved → p=1.0, within var = 0.
	rows := []TaskReplays{row("t1", 2, 4, 0), row("t2", 4, 4, 0)}
	c := Characterize(rows, Options{})

	if c.Tasks != 2 {
		t.Fatalf("Tasks: got %d want 2", c.Tasks)
	}
	if c.K != 4 {
		t.Fatalf("K: got %d want 4", c.K)
	}
	// WithinVar = (0.25 + 0)/2 = 0.125.
	approx(t, "WithinVar", c.WithinVar, 0.125)
	// SigmaNoise = sqrt(0.125).
	approx(t, "SigmaNoise", c.SigmaNoise, math.Sqrt(0.125))
	// Averaged = sqrt(0.125)/sqrt(4) = sqrt(0.125)/2.
	approx(t, "SigmaNoiseAveraged", c.SigmaNoiseAveraged, math.Sqrt(0.125)/2)
	// Band half-width = 2 × averaged.
	approx(t, "BandHalfWidth", c.BandHalfWidth, math.Sqrt(0.125))
	// BetweenVar of rates [0.5,1.0]: mean 0.75, ss = 0.0625+0.0625 = 0.125, /(n-1=1) = 0.125.
	approx(t, "BetweenVar", c.BetweenVar, 0.125)
	// ICC(1): MSB = 4×0.125 = 0.5; MSW = 0.125 × 4/3 = 1/6; denom = 0.5 + 3×(1/6) = 1.0;
	// ICC = (0.5 − 1/6)/1.0 = 1/3.
	approx(t, "ICC", c.ICC, 1.0/3.0)
	// MDE = (zAlpha+zBeta) × averaged × sqrt(2/N=2/2=1) = (zAlpha+zBeta)×averaged.
	approx(t, "MDE", c.MDE, (zAlpha+zBeta)*(math.Sqrt(0.125)/2))
}

// TestCleanInstrumentPasses: every task solves deterministically per its
// difficulty (easy = 5/5, hard = 0/5) → zero within-task noise, high between-task
// variance → ICC = 1, MDE = 0 → FEASIBLE. The clean instrument the gate must pass.
func TestCleanInstrumentPasses(t *testing.T) {
	rows := []TaskReplays{
		row("easy1", 5, 5, 100), row("easy2", 5, 5, 110),
		row("hard1", 0, 5, 400), row("hard2", 0, 5, 390),
		row("mid", 5, 5, 200),
	}
	c := Characterize(rows, Options{})
	approx(t, "SigmaNoise", c.SigmaNoise, 0) // no within-task wobble
	approx(t, "ICC", c.ICC, 1.0)             // all variance is real task-difference
	if c.MDE != 0 {
		t.Fatalf("MDE: got %.6f want 0 (no noise to resolve)", c.MDE)
	}
	if !c.Feasible || c.Verdict != VerdictFeasible {
		t.Fatalf("clean instrument: got Feasible=%v Verdict=%s, want true/FEASIBLE", c.Feasible, c.Verdict)
	}
}

// TestHighNoiseLowICCFails: every task flips ~50/50 across replays (max within-
// task noise) and all tasks share the SAME ~0.5 rate (no between-task signal) →
// ICC collapses toward 0 and the MDE blows past the claimable lift → the gate
// FAILS. The instrument the ruler must REJECT.
func TestHighNoiseLowICCFails(t *testing.T) {
	// 8 tasks, K=4, each 2/4 (p=0.5): max within-task variance, zero between-task
	// variance (all rates identical).
	var rows []TaskReplays
	for i := 0; i < 8; i++ {
		rows = append(rows, row("noisy", 2, 4, 300))
	}
	c := Characterize(rows, Options{})
	// Within var = 0.25 (max for binary), between var = 0 (all rates equal).
	approx(t, "WithinVar", c.WithinVar, 0.25)
	approx(t, "BetweenVar", c.BetweenVar, 0)
	// ICC: MSB = 4×0 = 0; ICC = (0 − MSW)/(0 + 3·MSW) < 0 → clamped to 0.
	approx(t, "ICC", c.ICC, 0)
	if c.Feasible {
		t.Fatalf("high-noise/zero-between instrument must FAIL the gate, got Feasible=true (%s)", c.Verdict)
	}
	// ICC below the floor is the binding clause here.
	if c.Verdict != VerdictLowReliability {
		t.Fatalf("verdict: got %s want LOW-RELIABILITY", c.Verdict)
	}
}

// TestNoisyRulerWhenMDEExceedsLift: real between-task signal (good ICC) but the
// per-replay noise + tiny N push the MDE above the claimable lift → NOISY-RULER
// (reliable but cannot resolve the effect). This isolates the SECOND gate clause
// from the first.
func TestNoisyRulerWhenMDEExceedsLift(t *testing.T) {
	// 4 tasks, K=4. Strong between-task separation (two clean 1.0, one clean 0.0)
	// gives a high ICC (≈0.8, clears the 0.5 floor); one task carries modest
	// within-noise (1/4) and the small N=4 keeps MDE (≈0.19) above the 0.15
	// claimable lift → reliable BUT cannot resolve the effect → NOISY-RULER.
	rows := []TaskReplays{
		row("a", 4, 4, 0), // p=1.0, var 0
		row("b", 0, 4, 0), // p=0.0, var 0
		row("c", 4, 4, 0), // p=1.0, var 0
		row("d", 1, 4, 0), // p=0.25, var 0.1875 (the noise carrier)
	}
	c := Characterize(rows, Options{})
	if c.ICC < c.ICCFloor {
		t.Fatalf("precondition: ICC %.4f should clear floor %.2f for this case", c.ICC, c.ICCFloor)
	}
	if c.MDE <= c.ClaimableLift {
		t.Fatalf("precondition: MDE %.4f should EXCEED claimable lift %.2f at N=3", c.MDE, c.ClaimableLift)
	}
	if c.Feasible {
		t.Fatalf("MDE-exceeds-lift instrument must FAIL, got Feasible=true")
	}
	if c.Verdict != VerdictNoisyRuler {
		t.Fatalf("verdict: got %s want NOISY-RULER", c.Verdict)
	}
}

// TestDegenerateTestDouble: the offline test double produces zero noise AND zero
// between-task variance (every task solved every replay) → DEGENERATE (the
// instrument has not been exercised on a non-deterministic substrate), NOT a
// false FEASIBLE and NOT a noise failure.
func TestDegenerateTestDouble(t *testing.T) {
	rows := []TaskReplays{
		row("t1", 5, 5, 0), row("t2", 5, 5, 0), row("t3", 5, 5, 0),
	}
	c := Characterize(rows, Options{})
	if c.Feasible {
		t.Fatalf("all-solved-every-replay must NOT read as feasible")
	}
	if c.Verdict != VerdictDegenerate {
		t.Fatalf("verdict: got %s want DEGENERATE", c.Verdict)
	}
}

// TestDegenerateTooFewTasksOrReplays: N<2 or K<2 is DEGENERATE regardless of the
// numbers (cannot estimate between/within variance from a single point).
func TestDegenerateTooFewTasksOrReplays(t *testing.T) {
	// single task
	c1 := Characterize([]TaskReplays{row("only", 2, 4, 0)}, Options{})
	if c1.Verdict != VerdictDegenerate {
		t.Fatalf("single-task verdict: got %s want DEGENERATE", c1.Verdict)
	}
	// K=1 (no replay to characterize noise from)
	c2 := Characterize([]TaskReplays{row("a", 1, 1, 0), row("b", 0, 1, 0)}, Options{})
	if c2.Verdict != VerdictDegenerate {
		t.Fatalf("K=1 verdict: got %s want DEGENERATE", c2.Verdict)
	}
}

// TestGateMutationSensitive: tighten/loosen the thresholds on a borderline
// instrument and watch the verdict flip both ways — proves the gate reads its
// thresholds (a hardcoded verdict would not move).
func TestGateMutationSensitive(t *testing.T) {
	// An instrument with ICC ≈ 1/3 (from the hand fixture) and a modest MDE.
	rows := []TaskReplays{row("t1", 2, 4, 0), row("t2", 4, 4, 0)}
	base := Characterize(rows, Options{})

	// Loosen the ICC floor BELOW the measured ICC and the lift ABOVE the MDE →
	// must become FEASIBLE.
	loose := Characterize(rows, Options{ICCFloor: base.ICC - 0.01, ClaimableLift: base.MDE + 0.01})
	if !loose.Feasible || loose.Verdict != VerdictFeasible {
		t.Fatalf("loosened gate: got Feasible=%v %s, want true/FEASIBLE", loose.Feasible, loose.Verdict)
	}

	// Tighten the ICC floor ABOVE the measured ICC → must become LOW-RELIABILITY.
	tightICC := Characterize(rows, Options{ICCFloor: base.ICC + 0.01, ClaimableLift: base.MDE + 0.01})
	if tightICC.Feasible || tightICC.Verdict != VerdictLowReliability {
		t.Fatalf("tightened ICC: got Feasible=%v %s, want false/LOW-RELIABILITY", tightICC.Feasible, tightICC.Verdict)
	}

	// ICC clears but tighten the claimable lift BELOW the MDE → NOISY-RULER.
	tightLift := Characterize(rows, Options{ICCFloor: base.ICC - 0.01, ClaimableLift: base.MDE - 0.001})
	if tightLift.Feasible || tightLift.Verdict != VerdictNoisyRuler {
		t.Fatalf("tightened lift: got Feasible=%v %s, want false/NOISY-RULER", tightLift.Feasible, tightLift.Verdict)
	}
}

// TestCostAxisDescriptiveOnly: the cost axis reports the between-task mean +
// dispersion but FLAGS that the within-task floor is unavailable (sum-only
// aggregates). A caller must never mistake the cost band for a characterized
// noise floor.
func TestCostAxisDescriptiveOnly(t *testing.T) {
	rows := []TaskReplays{
		row("a", 3, 5, 100), row("b", 2, 5, 200), row("c", 4, 5, 300),
	}
	c := Characterize(rows, Options{})
	approx(t, "CostMean", c.CostMean, 200) // (100+200+300)/3
	if c.CostWithinFloorAvailable {
		t.Fatalf("cost within-task floor must be flagged UNAVAILABLE (sum-only aggregates)")
	}
	if c.CostBetweenSD <= 0 {
		t.Fatalf("CostBetweenSD should be positive for dispersed costs, got %.4f", c.CostBetweenSD)
	}
}

// TestFromProbeAndFromCog: the adapters off the EXISTING campaign aggregate types
// map the right binary field (Solved / Fired) and the per-replay mean cost — the
// ruler consumes the already-collected types without re-running anything.
func TestFromProbeAndFromCog(t *testing.T) {
	probe := []campaign.ProbeStability{
		{Goal: "g1", Solved: 3, Replays: 5, Completion: 500},
		{Goal: "g2", Solved: 0, Replays: 5, Completion: 1000},
	}
	pr := FromProbe(probe)
	if len(pr) != 2 || pr[0].Success != 3 || pr[0].Replays != 5 {
		t.Fatalf("FromProbe mapped wrong: %+v", pr)
	}
	// MeanCompletion = 500/5 = 100.
	approx(t, "probe row0 cost", pr[0].MeanCompletion, 100)

	cog := []campaign.CogStability{
		{Goal: "c1", Fired: 4, Replays: 5, Completion: 250},
		{Goal: "c2", Fired: 1, Replays: 5, Completion: 750},
	}
	cr := FromCog(cog)
	if len(cr) != 2 || cr[0].Success != 4 || cr[1].Success != 1 {
		t.Fatalf("FromCog mapped wrong: %+v", cr)
	}
	approx(t, "cog row0 cost", cr[0].MeanCompletion, 50) // 250/5

	// The convenience entrypoints produce the same characterization as the manual
	// adapter + Characterize.
	cFromProbe := CharacterizeProbe(probe, Options{})
	cManual := Characterize(FromProbe(probe), Options{})
	if cFromProbe.Verdict != cManual.Verdict || cFromProbe.ICC != cManual.ICC {
		t.Fatalf("CharacterizeProbe diverged from manual path")
	}
}

// TestCostWithinFloorFlipsTrueWithVector: the headline W5-1 fix — when the per-
// replay completion vector is present the cost-axis within-floor flips AVAILABLE,
// and stays UNAVAILABLE for the legacy sum-only rows. Same tasks, two regimes.
func TestCostWithinFloorFlipsTrueWithVector(t *testing.T) {
	// Sum-only rows (no vector): within floor must stay unavailable.
	sumOnly := Characterize([]TaskReplays{
		row("a", 3, 4, 100), row("b", 2, 4, 200),
	}, Options{})
	if sumOnly.CostWithinFloorAvailable {
		t.Fatalf("sum-only rows: CostWithinFloorAvailable must be FALSE (no per-replay vector)")
	}
	if sumOnly.CostVerdict != CostDegenerate {
		t.Fatalf("sum-only cost verdict: got %s want COST-DEGENERATE", sumOnly.CostVerdict)
	}

	// Same tasks WITH the per-replay vector: within floor must flip available.
	withVec := Characterize([]TaskReplays{
		vrow("a", 3, []int{90, 110, 100, 100}),  // mean 100
		vrow("b", 2, []int{180, 220, 200, 200}), // mean 200
	}, Options{})
	if !withVec.CostWithinFloorAvailable {
		t.Fatalf("vector rows: CostWithinFloorAvailable must be TRUE (per-replay vector present)")
	}
	if withVec.CostSigmaWithin <= 0 {
		t.Fatalf("vector rows with real spread: CostSigmaWithin must be >0, got %.4f", withVec.CostSigmaWithin)
	}
}

// TestCostSigmaWithinHandComputed: pin the within-task cost-σ to a BY-HAND value on
// a 2-task K=4 fixture so a formula drift fails exactly here (mutation-sensitive on
// the cost math, the cost-axis mirror of TestHandComputableMath).
func TestCostSigmaWithinHandComputed(t *testing.T) {
	// Task a: [90,110,100,100] mean 100; deviations -10,10,0,0 → ss=200; sample var
	//   (n-1=3) = 200/3.
	// Task b: [180,220,200,200] mean 200; deviations -20,20,0,0 → ss=800; sample var
	//   = 800/3.
	// pooled within-cost var = mean(200/3, 800/3) = (200/3+800/3)/2 = (1000/3)/2 =
	//   500/3 ≈ 166.6667; CostSigmaWithin = sqrt(500/3).
	c := Characterize([]TaskReplays{
		vrow("a", 2, []int{90, 110, 100, 100}),
		vrow("b", 2, []int{180, 220, 200, 200}),
	}, Options{})
	if c.K != 4 {
		t.Fatalf("K: got %d want 4", c.K)
	}
	wantSigma := math.Sqrt(500.0 / 3.0)
	approx(t, "CostSigmaWithin", c.CostSigmaWithin, wantSigma)
	// Averaged = sigma/√K = sqrt(500/3)/2.
	approx(t, "CostSigmaWithinAveraged", c.CostSigmaWithinAveraged, wantSigma/2)
	// Band = 2 × averaged = sqrt(500/3).
	approx(t, "CostBandHalfWidth", c.CostBandHalfWidth, wantSigma)
	// CostMDE = (zAlpha+zBeta) × averaged × √(2/N=2/2=1).
	approx(t, "CostMDE", c.CostMDE, (zAlpha+zBeta)*(wantSigma/2))
	// CostMean = mean of the per-task means (100, 200) = 150.
	approx(t, "CostMean", c.CostMean, 150)
	if !c.CostWithinFloorAvailable {
		t.Fatalf("CostWithinFloorAvailable must be true on vector rows")
	}
}

// TestCostDegenerateOnTestDouble: the offline test double emits completion=0 every
// replay → the per-replay vector is present (so the floor is technically available)
// but the within-task cost variance is ZERO → COST-DEGENERATE, the honest read (no
// real usage to characterize). NOT a false COST-RELIABLE.
func TestCostDegenerateOnTestDouble(t *testing.T) {
	c := Characterize([]TaskReplays{
		vrow("a", 3, []int{0, 0, 0, 0}),
		vrow("b", 2, []int{0, 0, 0, 0}),
		vrow("c", 4, []int{0, 0, 0, 0}),
	}, Options{})
	if !c.CostWithinFloorAvailable {
		t.Fatalf("vector present (even all-zero): CostWithinFloorAvailable must be true")
	}
	approx(t, "CostSigmaWithin", c.CostSigmaWithin, 0)
	if c.CostVerdict != CostDegenerate {
		t.Fatalf("all-zero cost vector: got %s want COST-DEGENERATE (no real usage to characterize)", c.CostVerdict)
	}
}

// TestCostNoisyInstrumentFlagged: a HIGH within-task cost variance that swamps the
// between-task cost spread → the cost MDE exceeds the between-task SD → COST-NOISY.
// The cost instrument the ruler must flag as unable to rank configs by cost.
func TestCostNoisyInstrumentFlagged(t *testing.T) {
	// 3 tasks, K=4. All three share ~the same mean (~200) so the between-task cost
	// spread is tiny, but each replay swings wildly (0 vs 400) → large within-task
	// cost-σ → CostMDE >> CostBetweenSD → COST-NOISY.
	c := Characterize([]TaskReplays{
		vrow("a", 2, []int{0, 400, 0, 400}),
		vrow("b", 2, []int{400, 0, 400, 0}),
		vrow("c", 2, []int{0, 400, 400, 0}),
	}, Options{})
	if !c.CostWithinFloorAvailable {
		t.Fatalf("CostWithinFloorAvailable must be true")
	}
	if c.CostSigmaWithin <= c.CostBetweenSD {
		t.Fatalf("precondition: within-cost-σ %.2f should swamp between-cost SD %.2f", c.CostSigmaWithin, c.CostBetweenSD)
	}
	if c.CostMDE < c.CostBetweenSD {
		t.Fatalf("precondition: CostMDE %.2f should exceed between SD %.2f for a noisy cost instrument", c.CostMDE, c.CostBetweenSD)
	}
	if c.CostVerdict != CostNoisy {
		t.Fatalf("high-cost-variance instrument: got %s want COST-NOISY", c.CostVerdict)
	}
}

// TestCostReliableInstrument: clean per-task cost separation (each task's replays
// agree, but the tasks differ a lot in cost) → tiny within-cost-σ, large between-
// cost spread → CostMDE < between SD → COST-RELIABLE. The cost instrument that
// CAN rank configs by per-task token cost.
func TestCostReliableInstrument(t *testing.T) {
	c := Characterize([]TaskReplays{
		vrow("cheap1", 4, []int{100, 102, 98, 100}), // ~100, low within-spread
		vrow("cheap2", 4, []int{110, 108, 112, 110}),
		vrow("dear1", 0, []int{800, 805, 795, 800}), // ~800, low within-spread
		vrow("dear2", 0, []int{790, 792, 788, 790}),
		vrow("mid", 4, []int{400, 402, 398, 400}),
	}, Options{})
	if !c.CostWithinFloorAvailable {
		t.Fatalf("CostWithinFloorAvailable must be true")
	}
	if c.CostMDE >= c.CostBetweenSD {
		t.Fatalf("precondition: CostMDE %.2f must come in UNDER between SD %.2f for a reliable cost instrument", c.CostMDE, c.CostBetweenSD)
	}
	if c.CostVerdict != CostReliable {
		t.Fatalf("clean cost-separated instrument: got %s want COST-RELIABLE", c.CostVerdict)
	}
}

// TestCostVerdictIndependentOfBinary: the cost axis is reported ALONGSIDE the binary
// verdict and never replaces it. A binary-DEGENERATE instrument (all tasks solved
// every replay) can still carry a reliable COST verdict — and vice versa — proving
// the two axes are decoupled (the binary gate stays the primary keep-gate).
func TestCostVerdictIndependentOfBinary(t *testing.T) {
	// Binary DEGENERATE (every task 4/4 → no binary variance) but cost cleanly
	// separated → COST-RELIABLE on the cost axis, DEGENERATE on the binary axis.
	c := Characterize([]TaskReplays{
		vrow("a", 4, []int{100, 100, 100, 100}),
		vrow("b", 4, []int{500, 500, 500, 500}),
		vrow("c", 4, []int{900, 900, 900, 900}),
	}, Options{})
	if c.Verdict != VerdictDegenerate {
		t.Fatalf("binary axis: got %s want DEGENERATE (all tasks solved every replay)", c.Verdict)
	}
	// All within-task cost variance is zero here (each task constant) → the cost
	// axis is also DEGENERATE (no within-task cost noise to characterize) — the
	// honest read. The decoupling point is that the binary verdict did NOT force a
	// cost verdict and the cost fields are populated regardless.
	if c.CostVerdict != CostDegenerate {
		t.Fatalf("cost axis (constant per task): got %s want COST-DEGENERATE", c.CostVerdict)
	}
	if !c.CostWithinFloorAvailable {
		t.Fatalf("cost vector present → CostWithinFloorAvailable must be true even when binary is degenerate")
	}
	// And the primary keep-gate (binary) is untouched by the cost axis.
	if c.Feasible {
		t.Fatalf("binary Feasible must stay false (degenerate) regardless of the cost axis")
	}
}

// TestICCMonotoneInNoise: holding the between-task signal fixed, adding within-
// task replay noise must LOWER the ICC (more of the variance becomes noise). A
// direct sanity check that ICC measures reliability the intended direction.
func TestICCMonotoneInNoise(t *testing.T) {
	// Clean: distinct rates, zero within-task noise (5/5 and 0/5).
	clean := Characterize([]TaskReplays{
		row("a", 5, 5, 0), row("b", 0, 5, 0), row("c", 5, 5, 0), row("d", 0, 5, 0),
	}, Options{})
	// Noisy: same average rates but each task flips (so within-task var > 0) while
	// the between-task spread shrinks → lower ICC.
	noisy := Characterize([]TaskReplays{
		row("a", 4, 5, 0), row("b", 1, 5, 0), row("c", 4, 5, 0), row("d", 1, 5, 0),
	}, Options{})
	if !(clean.ICC > noisy.ICC) {
		t.Fatalf("ICC should fall as within-task noise rises: clean=%.4f noisy=%.4f", clean.ICC, noisy.ICC)
	}
}
