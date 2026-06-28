package realhard

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
)

// bernoulli.go — the BERNOULLI HIGH-K SINGLE-LAUNCH estimator (theory:
// docs/internal/notes/2026-06-19-noise-modeled-measurement-theory.md §6 + §2;
// docs/internal/notes/2026-06-19-noise-aware-estimator-spec.md §1.1/§4.3).
//
// WHY IT EXISTS. The variance-component path in estimator.go models the claude
// ±56pp swing as a LAUNCH random effect u_r and needs R≥2 (really ~8-20) INDEPENDENT
// LAUNCHES to identify it — the expensive path. But the on-disk measurement
// (runEffectVar≈0 on the K1/K3 data) found there is NO identifiable shared launch
// shock: the claude per-task noise is per-task-INDEPENDENT Bernoulli (batch-
// nondeterminism is per-CALL, not per-launch). When that holds, a task's outcome is a
// coin with a FIXED true p, and:
//
//   - Var(rate) = p(1−p) is a KNOWN function of the mean — so a variance does NOT
//     need a separate repeated-LAUNCH estimate; it follows from p̂.
//   - Each task's p̂ can be pinned CHEAPLY with high REPLAYS in ONE launch (the
//     replay noise averages at 1/√K) instead of repeating whole launches.
//   - Robustness (the deliberative-K lever's target) follows by FORMULA (the
//     binomial-majority concentration q = Σ_{j>k/2} C(k,j) p^j (1−p)^{k−j}) AND by
//     direct measurement of the deliberative arm's own solved/K.
//
// THE GUARDRAIL (load-bearing). The Bernoulli formula is VALID ONLY IF the within-
// task replay outcomes are iid Bernoulli (no shared shock). The OverdispersionCheck
// TESTS that and, if the data is overdispersed (a shared shock IS present), emits a
// verdict that says "Bernoulli-formula invalid → fall back to the variance-component
// path (estimator.go)" rather than silently reporting a wrong variance. This makes the
// method self-validating: it certifies its own precondition before it trusts the
// p(1−p) shortcut.
//
// SCOPE / HONESTY. This mode estimates the per-task TRUE p (capability), its Wilson
// CI, the two-proportion config effect, the analytic vs empirical deliberative-q, an
// adaptive replay allocation, and an overdispersion self-check — all from ONE launch's
// high-replays per-task counts. It is additive and default-OFF (Mode "" / EstBernOff
// → no Bernoulli pass). It does NOT replace estimator.go: when overdispersion fires,
// it explicitly defers to the launch-variance-component path. The assumption it CANNOT
// itself enforce: that ONE launch's K replays are genuinely independent draws of the
// same task (the overdispersion check is the in-data proxy for that, but with K alone
// it cannot distinguish a shared CALL-batch shock from true per-call independence —
// see OverdispersionCheck's honesty note).
//
// DETERMINISM. Pure arithmetic over the per-task counts; the ONLY randomness is the
// SEEDED Monte-Carlo used by tests and the optional bootstrap CI for the graded mode
// (cpyrand). Same input + same seed ⇒ same report, bit-for-bit (CLAUDE.md). No model,
// no wall clock, no I/O.

// --- mode + verdict --------------------------------------------------------

// BernMode selects the Bernoulli pass (additive to EstimatorConfig; default OFF →
// the Bernoulli estimator does not run and EstimateBernoulli is a no-op anchor).
type BernMode string

const (
	// EstBernOff — no Bernoulli pass (default). EstimateBernoulli returns the
	// degenerate anchor (counts only), byte-identical-safe.
	EstBernOff BernMode = "off"
	// EstBernOn — the high-K single-launch Bernoulli estimator: per-task p̂ +
	// Wilson CI + variance-by-formula, the two-proportion capability test, the
	// deliberative analytic-vs-empirical q, the adaptive-K recommender, and the
	// overdispersion self-check.
	EstBernOn BernMode = "bernoulli"
)

// BernVerdict is the Bernoulli pass's verdict (parallel to EstVerdict; the two new
// states are the overdispersion trip and the capability resolution).
type BernVerdict string

const (
	// BernFeasible — iid Bernoulli holds (no overdispersion) AND the capability
	// effect's CI lower bound clears 0 (a resolved, valid effect).
	BernFeasible BernVerdict = "FEASIBLE"
	// BernNoisy — iid holds but the effect does NOT resolve at this K (CI straddles
	// 0): raise K (the adaptive recommender sizes it).
	BernNoisy BernVerdict = "NOISY-RULER"
	// BernOverdispersed — the within-task replays are NOT consistent with iid
	// Bernoulli (a shared shock is present): the p(1−p) variance shortcut is INVALID
	// → fall back to the launch-variance-component path (estimator.go). NOT a pass.
	BernOverdispersed BernVerdict = "OVERDISPERSED"
	// BernGradedLeak — the graded (continuous) outcome FAILED the leakage guard
	// (bernoulli.go applyGradedGuard): it moved where the binary outcome was pinned
	// (saturated-divergence) OR it ranked the ON/OFF arms differently from the binary
	// oracle (arm-rank-sign flip). Either signature is a treatment-contaminated /
	// variance-manufacturing covariate → the graded scores were DROPPED and the report
	// is binary-only. Like OVERDISPERSED, it is an INSTRUMENT-integrity flag, not a pass
	// (the binary capability gate is still derived; this only says "graded was unsafe").
	BernGradedLeak BernVerdict = "GRADED-LEAK"
	// BernDegenerate — not enough exercised data (no informative task, K<2, or no
	// AB contrast to gate). The instrument has not been exercised.
	BernDegenerate BernVerdict = "DEGENERATE"
)

// the same one-sided α=0.05 / power=0.80 multipliers the rest of the estimator uses.
const (
	// bernZ95 is Φ⁻¹(0.975) — the two-sided 95% z for the Wilson interval and the
	// two-proportion CI (so the reported interval is a standard 95% CI; its one-sided
	// lower bound at α=0.025 is the conservative keep-gate).
	bernZ95 = 1.959963984540054
)

// --- per-task input + estimate ---------------------------------------------

// BernTaskInput is one task's high-replays single-launch result for ONE arm:
// `Solved` successes out of `K` replays. The Bernoulli estimator pins p̂ = Solved/K
// per task. Graded is the OPTIONAL continuous-score input (mean per-attempt score +
// the per-attempt sample SD); when GradedN>0 the graded estimator is used for this
// task instead of the binary p̂ (lower variance when the graded value is a valid
// outcome proxy — see BernTaskEstimate.GradedMean).
type BernTaskInput struct {
	TaskID     string
	Capability Capability
	Solved     int // successes
	K          int // replays (trials)

	// --- optional graded-signal mode (§6) ---
	// GradedMean / GradedSD / GradedN describe a CONTINUOUS per-attempt score (e.g.
	// a partial-credit oracle or mean V(s)). When GradedN>=2 the per-task mean+SE is
	// estimated from these (lower variance than the binary p̂) AND validity-checked:
	// the graded value must be a real outcome proxy, NOT a treatment-contaminated
	// covariate — the caller asserts that; the estimator reports the binary p̂ too so
	// a divergence (graded ≠ p̂) is visible.
	GradedMean float64
	GradedSD   float64
	GradedN    int
}

// BernTaskEstimate is one task's Bernoulli read: p̂, its Wilson CI, the outcome
// variance p̂(1−p̂), and (when supplied) the graded mean + SE.
type BernTaskEstimate struct {
	TaskID     string
	Capability Capability
	K          int
	Solved     int

	PHat       float64 // Solved/K
	WilsonLo   float64 // Wilson score CI lower bound
	WilsonHi   float64 // Wilson score CI upper bound
	OutcomeVar float64 // p̂(1−p̂) — the Bernoulli variance by formula
	SEMean     float64 // SE of the mean estimate = sqrt(p̂(1−p̂)/K) (binary) or graded SE

	// PassKAt is the k used for the pass^k reliability read on this task (the report's
	// configured BernoulliConfig.PassK; 0 ⇒ unset, PassK left at p̂). PassK is the
	// derived reliability = p̂^PassKAt (PassK(PHat, PassKAt)) — the BRITTLENESS axis from
	// tau2-bench: a task that passes@1 (high p̂) can collapse under pass^k. It is trusted
	// ONLY when the overdispersion guard passes (the iid precondition the closed form
	// rests on); when overdispersion fires the report flags pass^k as untrustworthy.
	// PassKLo / PassKHi propagate the per-task Wilson CI through the same p^k map.
	PassKAt int
	PassK   float64
	PassKLo float64
	PassKHi float64

	// Informative is false for a SATURATED task (p̂≡0 or p̂≡1 with the CI pinned at
	// the boundary): it carries ~0 Fisher information and is excluded from the
	// adaptive recommender's mid-range concentration (Fisher: I = K/(p(1−p)) → ∞
	// info per unit variance is meaningless at the boundary; p(1−p)→0 carries ~0
	// info about a DIFFERENCE).
	Informative bool

	// --- graded mode (when GradedN>=2) ---
	GradedUsed bool
	GradedMean float64
	GradedSE   float64
}

// estimateBernTask reduces one task's (solved, K) [+ optional graded] into its
// per-task estimate. passK is the pass^k reliability k (0 ⇒ no pass^k read).
func estimateBernTask(in BernTaskInput, passK int) BernTaskEstimate {
	est := BernTaskEstimate{
		TaskID:     in.TaskID,
		Capability: in.Capability,
		K:          in.K,
		Solved:     in.Solved,
	}
	if in.K <= 0 {
		return est
	}
	p := float64(in.Solved) / float64(in.K)
	est.PHat = p
	est.OutcomeVar = p * (1 - p)
	est.SEMean = math.Sqrt(est.OutcomeVar / float64(in.K))
	est.WilsonLo, est.WilsonHi = wilsonCI(in.Solved, in.K, bernZ95)
	// a task is informative (carries effect-discrimination info) when its p̂ is not
	// pinned at the 0/1 boundary — i.e. it actually MOVED across its K replays.
	est.Informative = in.Solved > 0 && in.Solved < in.K

	// pass^k reliability (the brittleness axis): the closed-form p̂^k under the iid
	// model, with the per-task Wilson CI propagated through the same p^k map (monotone
	// increasing in p ⇒ the CI bounds map directly: lo→lo^k, hi→hi^k). Trusted only
	// when the overdispersion guard passes (the report flags it otherwise).
	if passK > 0 {
		est.PassKAt = passK
		est.PassK = PassK(p, passK)
		est.PassKLo = PassK(est.WilsonLo, passK)
		est.PassKHi = PassK(est.WilsonHi, passK)
	}

	// graded-signal mode: a continuous per-attempt score, lower-variance than binary.
	if in.GradedN >= 2 {
		est.GradedUsed = true
		est.GradedMean = in.GradedMean
		est.GradedSE = in.GradedSD / math.Sqrt(float64(in.GradedN))
	}
	return est
}

// wilsonCI is the WILSON SCORE confidence interval for a binomial proportion —
// CORRECT near 0/1 where the normal (Wald) approximation gives nonsense (negative
// lower bounds, zero width at the boundary). z is the two-sided normal quantile
// (bernZ95 for a 95% CI). For x successes in n trials:
//
//	centre = (p̂ + z²/2n) / (1 + z²/n)
//	half   = (z/(1+z²/n)) · sqrt( p̂(1−p̂)/n + z²/4n² )
//	[lo,hi] = centre ∓ half,  clamped to [0,1]
//
// At the boundary (x=0 or x=n) the interval is one-sided-correct (lo>0 when x=0 is
// false; hi<1 when x=n is false) — the property the Wald interval lacks.
func wilsonCI(x, n int, z float64) (lo, hi float64) {
	if n <= 0 {
		return 0, 1
	}
	nf := float64(n)
	p := float64(x) / nf
	z2 := z * z
	denom := 1 + z2/nf
	centre := (p + z2/(2*nf)) / denom
	half := (z / denom) * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	lo = centre - half
	hi = centre + half
	// clamp to [0,1], snapping floating-point dust at the boundary to exact 0/1 (the
	// Wilson lo at x=0 is mathematically 0 but the formula leaves ~1e-18 residue).
	if lo < wilsonEps {
		lo = 0
	}
	if hi > 1-wilsonEps {
		hi = 1
	}
	return lo, hi
}

// wilsonEps snaps boundary floating-point dust (e.g. the ~1e-18 the Wilson lo carries
// at x=0) to an exact 0/1. Far below any real interval width.
const wilsonEps = 1e-12

// --- two-proportion capability test (§3a, paired-by-task) ------------------

// BernPairedTaskDiff is one task's ON−OFF proportion difference with a CI (the
// per-task capability move).
type BernPairedTaskDiff struct {
	TaskID     string
	Capability Capability
	POff       float64
	POn        float64
	Diff       float64 // POn − POff
	CILo       float64
	CIHi       float64
	// Moved is true when the per-task CI EXCLUDES 0 (the task moved beyond its CI).
	Moved bool
}

// twoProportionDiffTask computes the per-task ON−OFF difference and a Newcombe
// hybrid-score (Wilson-derived) CI — the boundary-correct interval for a difference
// of two independent proportions (unpaired at the replay level: the OFF and ON
// replays are distinct draws). Newcombe (1998) method 10:
//
//	[Wilson l1,u1] for p_OFF, [Wilson l2,u2] for p_ON
//	diff CI = [ d − sqrt((p1−l1)² + (u2−p2)²),  d + sqrt((u1−p1)² + (p2−l2)²) ]
//
// This is correct near 0/1 (the suite's saturated tasks) where the normal-approx
// two-proportion SE collapses to 0 and falsely declares significance.
func twoProportionDiffTask(off, on BernTaskInput, z float64) BernPairedTaskDiff {
	pOff := safeRate(off.Solved, off.K)
	pOn := safeRate(on.Solved, on.K)
	l1, u1 := wilsonCI(off.Solved, off.K, z)
	l2, u2 := wilsonCI(on.Solved, on.K, z)
	d := pOn - pOff
	// Newcombe method-10 difference interval.
	lo := d - math.Sqrt((pOff-l1)*(pOff-l1)+(u2-pOn)*(u2-pOn))
	hi := d + math.Sqrt((u1-pOff)*(u1-pOff)+(pOn-l2)*(pOn-l2))
	if lo < -1 {
		lo = -1
	}
	if hi > 1 {
		hi = 1
	}
	return BernPairedTaskDiff{
		TaskID:     off.TaskID,
		Capability: off.Capability,
		POff:       pOff,
		POn:        pOn,
		Diff:       d,
		CILo:       lo,
		CIHi:       hi,
		Moved:      lo > 0 || hi < 0,
	}
}

func safeRate(x, n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64(x) / float64(n)
}

// aggregateProportionDiff combines the per-task ON−OFF diffs into a suite-level
// mean diff with a CI. Paired-by-task (each task is its own control): the aggregate
// effect is the MEAN of the per-task diffs, and its SE folds in each task's binomial
// variance (sum of per-task SE² / T², since the tasks are independent draws). This is
// the Miller-2024 paired-by-item aggregate.
func aggregateProportionDiff(diffs []BernPairedTaskDiff, offs, ons []BernTaskInput, z float64) (mean, lo, hi, se float64) {
	t := len(diffs)
	if t == 0 {
		return 0, 0, 0, 0
	}
	var sum float64
	var varSum float64
	for i := range diffs {
		sum += diffs[i].Diff
		pOff := safeRate(offs[i].Solved, offs[i].K)
		pOn := safeRate(ons[i].Solved, ons[i].K)
		vOff := 0.0
		if offs[i].K > 0 {
			vOff = pOff * (1 - pOff) / float64(offs[i].K)
		}
		vOn := 0.0
		if ons[i].K > 0 {
			vOn = pOn * (1 - pOn) / float64(ons[i].K)
		}
		varSum += vOff + vOn // per-task diff variance
	}
	mean = sum / float64(t)
	// SE of the MEAN of T independent per-task diffs = sqrt(sum of per-task var) / T.
	se = math.Sqrt(varSum) / float64(t)
	lo = mean - z*se
	hi = mean + z*se
	return mean, lo, hi, se
}

// --- deliberative robustness: analytic vs empirical q (§3) -----------------

// BernDeliberativeTask is one task's deliberative-K read: the empirical q̂ from the
// deliberative arm (its own solved/K), the ANALYTIC q predicted by binomial-majority
// concentration of the base p̂ over k sub-episodes, the variance reduction
// p̂(1−p̂)→q̂(1−q̂), and a divergence flag (empirical q̂ far from analytic q ⇒ the k
// sub-episodes are NOT independent — a real finding).
type BernDeliberativeTask struct {
	TaskID      string
	Capability  Capability
	K           int     // the deliberative sub-episode count (THOUGHT_DELIBERATIVE_K)
	PBase       float64 // the base (K=1) per-task p̂
	QEmpirical  float64 // the deliberative arm's own solved/replays
	QAnalytic   float64 // binomial-majority(p̂, k)
	VarBase     float64 // p̂(1−p̂)
	VarEmp      float64 // q̂(1−q̂) — the realized outcome variance after deliberation
	VarAnalytic float64 // q_analytic(1−q_analytic)
	Diverged    bool    // |q̂ − q_analytic| beyond the joint sampling tolerance
}

// binomialMajority returns q = P(majority of k iid Bernoulli(p) succeed)
//
//	q = Σ_{j > k/2} C(k,j) p^j (1−p)^{k−j}
//
// with the EVEN-k tie (j == k/2) broken at 1/2 (a tie is resolved by a fair
// coin / the V(s) tie-break — the deliberative reconcile's behaviour). This is the
// concentration the deliberative lever buys: for p>1/2, q>p (majority pushes toward
// the mode); for p<1/2, q<p (it pushes away). At p=1/2, q=1/2 (no movement). k=1 → q=p.
func binomialMajority(p float64, k int) float64 {
	if k <= 1 {
		return p
	}
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	var q float64
	for j := 0; j <= k; j++ {
		c := binomCoef(k, j)
		term := c * math.Pow(p, float64(j)) * math.Pow(1-p, float64(k-j))
		switch {
		case 2*j > k:
			q += term
		case 2*j == k: // even-k tie → fair break
			q += 0.5 * term
		}
	}
	return q
}

// --- pass^k reliability metric (tau2-bench, §7.3) --------------------------

// PassK is the closed-form pass^k RELIABILITY metric: the probability that ALL k
// INDEPENDENT tries of a task succeed, under the iid Bernoulli model the
// overdispersion guard (checkOverdispersion*) already validates:
//
//	pass^k = p^k
//
// It is the OPPOSITE use of the same K samples from binomialMajority (the
// deliberative/robustness q = "majority of k succeed"): pass^k is the BRITTLENESS
// axis (the tau2-bench finding — agents that pass@1 collapse under pass^k because
// every one of k tries must succeed), whereas binomialMajority is the ROBUSTNESS
// axis (best/majority of k). The two MUST NOT be conflated (the design doc's
// open-question #4); pass^k is a DERIVED quantity of the existing per-task p̂, not a
// new measurement — so it inherits the Bernoulli precondition (iid replays) the
// overdispersion check tests, and the report only trusts it when that check passes.
//
// pass^1 = p (single try). For 0<p<1 it DECAYS geometrically in k (the brittleness
// the metric exists to surface); for p>=1 it stays 1 (a perfectly reliable task);
// for p<=0 it is 0. k<=0 is the empty conjunction → 1 (no try has failed). Pure
// arithmetic; deterministic.
func PassK(p float64, k int) float64 {
	if k <= 0 {
		return 1
	}
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	return math.Pow(p, float64(k))
}

// binomCoef is C(n,k) computed multiplicatively (exact for the small k the
// deliberative lever uses; no factorial overflow).
func binomCoef(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	c := 1.0
	for i := 0; i < k; i++ {
		c = c * float64(n-i) / float64(i+1)
	}
	return c
}

// estimateDeliberativeTask compares the deliberative arm's empirical q̂ against the
// analytic binomial-majority q of the base p̂. divTol is the divergence tolerance (a
// joint sampling SE multiple); when |q̂ − q_analytic| exceeds it the sub-episodes are
// flagged as non-independent.
func estimateDeliberativeTask(base, delib BernTaskInput, k int, divTol float64) BernDeliberativeTask {
	pBase := safeRate(base.Solved, base.K)
	qEmp := safeRate(delib.Solved, delib.K)
	qAna := binomialMajority(pBase, k)
	out := BernDeliberativeTask{
		TaskID:      base.TaskID,
		Capability:  base.Capability,
		K:           k,
		PBase:       pBase,
		QEmpirical:  qEmp,
		QAnalytic:   qAna,
		VarBase:     pBase * (1 - pBase),
		VarEmp:      qEmp * (1 - qEmp),
		VarAnalytic: qAna * (1 - qAna),
	}
	// the joint sampling SE of (q̂ − q_analytic): q̂ has SE sqrt(q(1−q)/K_delib), and
	// q_analytic inherits p̂'s uncertainty through the majority map; a coarse but
	// honest bound is the q̂ SE plus the p̂ SE (the dominant terms). The divergence
	// gate fires when the gap is divTol multiples of that combined SE.
	seQ := 0.0
	if delib.K > 0 {
		seQ = math.Sqrt(qEmp*(1-qEmp)/float64(delib.K)) + 1e-9
	}
	seP := 0.0
	if base.K > 0 {
		seP = math.Sqrt(pBase*(1-pBase)/float64(base.K)) + 1e-9
	}
	seJoint := seQ + seP
	if seJoint <= 0 {
		seJoint = 1e-9
	}
	out.Diverged = math.Abs(qEmp-qAna) > divTol*seJoint
	return out
}

// --- adaptive-K (Neyman/Fisher) allocation recommender (§4) ----------------

// BernAllocation is the adaptive replay-allocation recommendation for one task:
// the K needed to hit the target CI half-width, concentrating budget on mid-range
// p≈0.5 tasks (max variance / max Fisher info per the difference) and minimizing it
// on near-saturated tasks (p≈0 or 1 carry ~0 info).
type BernAllocation struct {
	TaskID       string
	Capability   Capability
	PHat         float64
	CurrentK     int
	RecommendedK int
	CurrentHalf  float64 // current Wilson-ish half-width at CurrentK
	TargetHalf   float64 // the requested target half-width
}

// recommendAllocation sizes the per-task replay count to reach targetHalf (the CI
// half-width target) under Neyman allocation. For a proportion, the normal-approx CI
// half-width is z·sqrt(p(1−p)/K), so to hit `targetHalf`:
//
//	K_target = z² · p̂(1−p̂) / targetHalf²
//
// which is MAXIMAL at p̂=0.5 (the informative band) and →0 as p̂→0 or 1 (saturated
// tasks need almost no budget — Fisher's "P near 0/1 carries ~0 info"). The
// recommender clamps K_target to [kMin, kMax] and never recommends fewer than the
// current K for an informative task that has not yet hit the target. minInfoVar
// floors the variance so an exactly-saturated p̂ (0 or 1) still gets kMin (you cannot
// trust a 0/0-information point estimate from K replays of a boundary).
func recommendAllocation(est BernTaskEstimate, targetHalf float64, z float64, kMin, kMax int) BernAllocation {
	alloc := BernAllocation{
		TaskID:      est.TaskID,
		Capability:  est.Capability,
		PHat:        est.PHat,
		CurrentK:    est.K,
		TargetHalf:  targetHalf,
		CurrentHalf: z * est.SEMean,
	}
	if targetHalf <= 0 {
		alloc.RecommendedK = est.K
		return alloc
	}
	v := est.PHat * (1 - est.PHat)
	// a boundary p̂ (0 or 1) has v=0 → it would recommend 0; but a boundary estimate
	// from finite K is itself uncertain, so floor it to kMin (the minimum-info budget,
	// the Fisher "saturated → minimal but non-zero" allocation).
	kTarget := kMin
	if v > 0 {
		kt := z * z * v / (targetHalf * targetHalf)
		kTarget = int(math.Ceil(kt))
	}
	if kTarget < kMin {
		kTarget = kMin
	}
	if kMax > 0 && kTarget > kMax {
		kTarget = kMax
	}
	alloc.RecommendedK = kTarget
	return alloc
}

// --- overdispersion self-check (the guardrail, §1.1 honesty flag) ----------

// BernOverdispersion is the iid-Bernoulli self-check result. It tests whether the
// per-task p̂s are consistent with a SINGLE shared shock having moved them together
// (overdispersion = a launch random effect that the Bernoulli formula assumes away).
type BernOverdispersion struct {
	// Statistic is the dispersion ratio (observed between-task variance of the
	// solved-counts vs the variance expected under iid Bernoulli with each task's own
	// p̂). ~1 ⇒ consistent with iid; >>1 ⇒ overdispersed (a shared shock present).
	Statistic float64
	// CrossLaunchAvailable is true when >=2 launches were supplied, enabling the
	// STRONGER within-launch vs cross-launch variance comparison (the direct test).
	CrossLaunchAvailable bool
	// WithinVar / CrossVar are the within-launch and cross-launch per-task rate
	// variances when CrossLaunchAvailable (the direct overdispersion read).
	WithinVar float64
	CrossVar  float64
	// Overdispersed is the DEFINITIVE verdict: true ⇒ the Bernoulli p(1−p) shortcut is
	// INVALID, fall back to the variance-component path. It is driven ONLY by the
	// CROSS-LAUNCH test (which isolates a shared shock from per-task difficulty); the
	// single-launch screen is ADVISORY (it cannot tell heterogeneity from a shock with
	// one launch) and sets PooledMisfit, NOT Overdispersed.
	Overdispersed bool
	// PooledMisfit is the single-launch ADVISORY flag: the pooled-rate model does not
	// fit (a shared shock OR — more likely with one launch — genuine per-task
	// difficulty heterogeneity, which is BENIGN for the per-task Wilson CIs). It does
	// NOT flip the verdict; it is a note that the DEFINITIVE cross-launch test should
	// be run before trusting any POOLED/aggregate Bernoulli quantity.
	PooledMisfit bool
	// Reason is the human-readable basis (single-launch chi-square vs cross-launch).
	Reason string
}

// overdispersionThreshold is the dispersion-ratio above which the single-launch
// screen raises the ADVISORY PooledMisfit flag (NOT the verdict). A ratio of ~1 means
// the tasks share one rate; >1.5 means the pooled-rate model misfits (heterogeneity OR
// a shock — indistinguishable with one launch). It is advisory because per-task
// heterogeneity is expected on a real hard suite and is benign for the per-task CIs.
const overdispersionThreshold = 1.5

// crossOverdispersionThreshold is the cross-launch/within-launch variance-ratio trip
// point when >=2 launches are available (the direct test: a shared launch shock makes
// the cross-launch variance EXCEED the within-launch Bernoulli variance).
const crossOverdispersionThreshold = 1.5

// checkOverdispersionSingle tests a SINGLE launch's per-task (solved, K) for
// overdispersion via a dispersion statistic. Under iid Bernoulli the solved-count of
// task i is Binomial(K_i, p_i) with variance K_i·p̂_i(1−p̂_i). The check compares the
// OBSERVED spread of the standardized residuals against its iid expectation:
//
//	D = (1/(T−1)) · Σ_i (solved_i − K_i·p̄)² / (K_i·p̄(1−p̄))
//
// where p̄ is the pooled rate — a Pearson-chi-square-over-df dispersion ratio. D≈1 ⇒
// the tasks share one rate; D≫1 ⇒ the tasks are MORE spread than independent coins at
// a COMMON rate allow. NOTE (honesty, load-bearing): with one launch this CANNOT
// distinguish a shared launch shock from genuine per-task difficulty heterogeneity —
// both inflate D, and on a real hard suite the tasks legitimately have DIFFERENT
// difficulties (so D≫1 is EXPECTED and BENIGN). Therefore the single-launch screen
// sets only the ADVISORY PooledMisfit flag — it does NOT drive the OVERDISPERSED
// verdict. The per-task Bernoulli p̂ + Wilson CI + the per-task two-proportion test are
// all per-task (never pooled) and remain VALID regardless of D. The DEFINITIVE shock
// test that flips the verdict needs >=2 launches (checkOverdispersionCross), which
// isolates the shared shock from per-task difficulty.
func checkOverdispersionSingle(ins []BernTaskInput) BernOverdispersion {
	out := BernOverdispersion{Reason: "single-launch Pearson dispersion (pooled p̄)"}
	// pool an overall rate over informative tasks (saturated tasks add 0 variance and
	// would deflate D; the dispersion question is about the tasks that actually move).
	var solSum, kSum float64
	var infos []BernTaskInput
	for _, in := range ins {
		if in.K <= 0 {
			continue
		}
		p := float64(in.Solved) / float64(in.K)
		if p > 0 && p < 1 {
			infos = append(infos, in)
		}
		solSum += float64(in.Solved)
		kSum += float64(in.K)
	}
	if len(infos) < 2 || kSum <= 0 {
		out.Statistic = 1 // not enough informative tasks to detect overdispersion
		return out
	}
	pBar := solSum / kSum
	if pBar <= 0 || pBar >= 1 {
		out.Statistic = 1
		return out
	}
	var chi float64
	for _, in := range infos {
		kf := float64(in.K)
		exp := kf * pBar
		denom := kf * pBar * (1 - pBar)
		if denom <= 0 {
			continue
		}
		d := float64(in.Solved) - exp
		chi += d * d / denom
	}
	df := float64(len(infos) - 1)
	if df <= 0 {
		out.Statistic = 1
		return out
	}
	out.Statistic = chi / df
	// ADVISORY only: the single-launch screen cannot tell a shock from heterogeneity,
	// so it sets PooledMisfit (a note), NOT Overdispersed (the verdict-flipping flag).
	out.PooledMisfit = out.Statistic > overdispersionThreshold
	return out
}

// checkOverdispersionCross is the DEFINITIVE overdispersion test when >=2 launches
// are available: it compares the WITHIN-launch Bernoulli variance (the replay noise)
// against the CROSS-launch variance of the per-task rates (the run-to-run swing). A
// shared launch shock makes the cross-launch variance EXCEED the within-launch
// expectation; pure per-call Bernoulli makes them equal. rates[launch][task]. This is
// the direct read of the same quantity runEffectVar (estimator.go) estimates — when
// it fires, defer to that path.
func checkOverdispersionCross(rates [][]float64, ks []int) BernOverdispersion {
	out := BernOverdispersion{CrossLaunchAvailable: true, Reason: "cross-launch vs within-launch variance"}
	r := len(rates)
	if r < 2 {
		out.CrossLaunchAvailable = false
		out.Statistic = 1
		return out
	}
	t := len(rates[0])
	var crossSum, withinSum float64
	var n int
	for c := 0; c < t; c++ {
		// the task's per-launch rates.
		col := make([]float64, r)
		for l := 0; l < r; l++ {
			col[l] = rates[l][c]
		}
		// skip a task that is saturated across all launches (no variance either way).
		if columnConstant(rates, c) {
			continue
		}
		crossSum += sampleVar(col) // cross-launch variance of this task's rate
		// within-launch Bernoulli variance EXPECTED at the task's mean rate, p(1−p)/K.
		p := meanOf(col)
		k := 1
		if c < len(ks) && ks[c] > 0 {
			k = ks[c]
		}
		withinSum += p * (1 - p) / float64(k)
		n++
	}
	if n == 0 || withinSum <= 0 {
		out.Statistic = 1
		return out
	}
	out.WithinVar = withinSum / float64(n)
	out.CrossVar = crossSum / float64(n)
	out.Statistic = out.CrossVar / out.WithinVar
	out.Overdispersed = out.Statistic > crossOverdispersionThreshold
	return out
}

// --- graded leakage guard (§2.4 CUPED guard ported to the graded outcome) ---

// gradedSatEps is the saturated-divergence tolerance: on a task whose binary p̂ is pinned
// at the 0/1 boundary, a VALID answer-derived graded score must agree with the binary
// outcome to within this ε. A larger swing means the graded score moved where the binary
// outcome cannot → it is manufacturing variance (the contamination signature) → DROP.
const gradedSatEps = 0.05

// applyGradedGuard is the leakage guard for the graded outcome — the §2.4 CUPED guard
// (estimator.go applyCUPED) ported to the answer-derived graded score. It gates whether
// the graded path may be TRUSTED: graded is kept ONLY when BOTH sub-guards pass; any
// trip DROPS graded entirely (the report falls back to binary, verdict GRADED-LEAK).
//
// THE TWO SUB-GUARDS (both required):
//
//  1. SATURATED-DIVERGENCE. On any task where the binary p̂∈{0,1} (saturated), require
//     |GradedMean − p̂| ≤ ε (gradedSatEps). A graded score that MOVES where the binary
//     outcome is pinned is manufacturing variance — the contamination signature (the
//     direct analogue of CUPED's "swung β beyond its SE" leak: the covariate moved a
//     quantity it should not). This is a property of EITHER arm's own data and runs even
//     without an AB contrast.
//
//  2. ARM-RANK-SIGN. When both an ON and OFF arm are present, require the graded ON−OFF
//     per-task direction to agree in SIGN with the binary ON−OFF direction on EVERY
//     informative task (a task that moved on at least one arm's binary outcome). A graded
//     sign flip means the graded score ranks the arms differently from the oracle — the
//     ranking contamination the red-team's arm-orthogonality requirement forbids. (This
//     is the direct AB analogue of CUPED's x̄_ON ≠ x̄_OFF leak probe.)
//
// offs is the baseline arm's per-task inputs; ons is the ON arm (nil ⇒ guard 2 is
// skipped — single-arm characterization, only guard 1 applies). They are aligned by task.
// Returns the GradedGuard with Used = Present && !trip(any).
func applyGradedGuard(offs, ons []BernTaskInput) GradedGuard {
	g := GradedGuard{}
	// guard 1 (saturated-divergence) over BOTH arms' own data.
	checkSaturated := func(in BernTaskInput) {
		if in.GradedN < 2 || in.K <= 0 {
			return
		}
		g.Present = true
		p := float64(in.Solved) / float64(in.K)
		// saturated = the binary outcome is pinned at the 0/1 boundary.
		if p == 0 || p == 1 {
			swing := math.Abs(in.GradedMean - p)
			if swing > g.MaxSatSwing {
				g.MaxSatSwing = swing
			}
			if swing > gradedSatEps {
				g.SaturatedDiverged = true
				g.SaturatedTasks = append(g.SaturatedTasks, in.TaskID)
			}
		}
	}
	for _, in := range offs {
		checkSaturated(in)
	}
	for _, in := range ons {
		checkSaturated(in)
	}

	// guard 2 (arm-rank-sign): only when an aligned AB contrast is present.
	if len(ons) == len(offs) && len(ons) > 0 {
		for i := range offs {
			off, on := offs[i], ons[i]
			// only tasks that BOTH supplied a graded vector can be rank-checked.
			if off.GradedN < 2 || on.GradedN < 2 || off.K <= 0 || on.K <= 0 {
				continue
			}
			binDiff := safeRate(on.Solved, on.K) - safeRate(off.Solved, off.K)
			// informative = the binary outcome actually moved across arms (a flat binary
			// diff carries no rank to agree/disagree with).
			if binDiff == 0 {
				continue
			}
			gradDiff := on.GradedMean - off.GradedMean
			// a sign flip: the graded ON−OFF points the OPPOSITE way from the binary
			// ON−OFF. A graded diff of exactly 0 where the binary moved is also a
			// disagreement (the graded signal lost a real arm difference) → flip.
			if gradDiff == 0 || (binDiff > 0) != (gradDiff > 0) {
				g.ArmRankFlipped = true
				g.FlippedTasks = append(g.FlippedTasks, off.TaskID)
			}
		}
	}

	g.Used = g.Present && !g.SaturatedDiverged && !g.ArmRankFlipped
	return g
}

// gradedLeakNote renders the human-readable reason the graded outcome was DROPPED.
func gradedLeakNote(g GradedGuard) string {
	var reasons []string
	if g.SaturatedDiverged {
		reasons = append(reasons, fmt.Sprintf("saturated-divergence (max |graded-p̂|=%.3f > ε=%.2f on %s)",
			g.MaxSatSwing, gradedSatEps, strings.Join(g.SaturatedTasks, ",")))
	}
	if g.ArmRankFlipped {
		reasons = append(reasons, "arm-rank sign flip on "+strings.Join(g.FlippedTasks, ","))
	}
	return "GRADED DROPPED (leakage guard): " + strings.Join(reasons, "; ") +
		". The graded outcome is treatment-contaminated / variance-manufacturing -> report is binary-only."
}

// --- the report ------------------------------------------------------------

// BernoulliConfig parameterizes the Bernoulli pass (additive; zero value = off).
type BernoulliConfig struct {
	Mode BernMode
	// Delta is the capability effect the gate must resolve (the target two-proportion
	// CI half-width). 0 → the estimator default (0.15, the ruler claimable lift).
	Delta float64
	// TargetHalf is the per-task Wilson CI half-width the adaptive recommender sizes
	// for. 0 → defaults to Delta (resolve a per-task effect of size Delta).
	TargetHalf float64
	// DeliberativeK is the sub-episode count k the analytic binomial-majority q uses
	// (THOUGHT_DELIBERATIVE_K). When >1 and a deliberative arm is supplied, the
	// analytic-vs-empirical q cross-check runs.
	DeliberativeK int
	// AllocKMin / AllocKMax bound the adaptive recommender's per-task K. 0 → defaults
	// (kMin=4, kMax=200).
	AllocKMin int
	AllocKMax int
	// DivergenceTol is the analytic-vs-empirical q divergence tolerance (SE multiples).
	// 0 → default 3.0.
	DivergenceTol float64
	// PassK is the k for the pass^k RELIABILITY read (the tau2-bench brittleness axis):
	// per-task pass^k = p̂^PassK + the aggregate mean pass^k over informative tasks. 0 ⇒
	// no pass^k pass (the report's PassK fields stay 0; byte-identical to the pre-metric
	// path). Must be >=2 to be a meaningful reliability read (pass^1 == pass@1 == p̂).
	PassK int
}

func (c BernoulliConfig) delta() float64 {
	if c.Delta > 0 {
		return c.Delta
	}
	return 0.15
}
func (c BernoulliConfig) targetHalf() float64 {
	if c.TargetHalf > 0 {
		return c.TargetHalf
	}
	return c.delta()
}
func (c BernoulliConfig) allocKMin() int {
	if c.AllocKMin > 0 {
		return c.AllocKMin
	}
	return 4
}
func (c BernoulliConfig) allocKMax() int {
	if c.AllocKMax > 0 {
		return c.AllocKMax
	}
	return 200
}
func (c BernoulliConfig) divergenceTol() float64 {
	if c.DivergenceTol > 0 {
		return c.DivergenceTol
	}
	return 3.0
}

// BernoulliReport is the full Bernoulli high-K single-launch read.
type BernoulliReport struct {
	Mode BernMode

	Tasks           int
	TaskEff         int // informative (non-saturated) task count
	HasABContrast   bool
	HasDeliberative bool

	// --- per-task capability ---
	PerTask []BernTaskEstimate

	// --- pass^k reliability (tau2-bench brittleness axis) ---
	// PassKAt is the configured k (0 ⇒ no pass^k pass). MeanPass1 / MeanPassK are the
	// mean pass@1 (p̂) and mean pass^k over the informative tasks — the headline
	// "capability vs reliability" contrast (pass@1 can be high while pass^k collapses).
	// PassKTrusted is false when the overdispersion guard fired (the iid precondition
	// the p^k closed form rests on is violated ⇒ do not trust the per-task pass^k).
	PassKAt      int
	MeanPass1    float64
	MeanPassK    float64
	PassKTrusted bool

	// --- two-proportion capability test ---
	PerTaskDiff  []BernPairedTaskDiff
	MeanDiff     float64
	MeanDiffCILo float64
	MeanDiffCIHi float64
	MeanDiffSE   float64
	TasksMoved   []string // tasks whose per-task CI excluded 0

	// --- deliberative robustness ---
	PerTaskDelib         []BernDeliberativeTask
	MeanVarBase          float64  // mean p̂(1−p̂) across informative tasks
	MeanVarEmp           float64  // mean q̂(1−q̂) (the realized post-deliberation variance)
	MeanVarAnalytic      float64  // mean q_analytic(1−q_analytic)
	DeliberativeDiverged []string // tasks where empirical q̂ diverged from analytic q

	// --- adaptive-K allocation ---
	Allocation     []BernAllocation
	UniformK       int // the uniform per-task K to hit the same aggregate precision
	AdaptiveTotalK int // sum of recommended per-task K
	UniformTotalK  int // T · UniformK
	AllocSavingK   int // UniformTotalK − AdaptiveTotalK (>0 ⇒ adaptive saves budget)

	// --- overdispersion self-check ---
	Overdispersion BernOverdispersion

	// --- graded leakage guard (§2.4 ported to the graded outcome) ---
	Graded GradedGuard

	Delta   float64
	Verdict BernVerdict
	Notes   []string
}

// GradedGuard is the result of the graded-outcome leakage guard (the §2.4 CUPED guard
// ported from estimator.go to the answer-derived graded score). The graded path is
// USED (GradedUsed flips ON for the per-task estimates) ONLY when BOTH sub-guards pass.
type GradedGuard struct {
	// Present is true when at least one task supplied a graded vector (GradedN>=2).
	Present bool
	// Used is the FINAL decision: graded is trusted (the per-task GradedUsed stayed ON)
	// iff Present AND both sub-guards passed. When false the report is binary-only.
	Used bool
	// SaturatedDiverged is true when the saturated-divergence sub-guard tripped: on some
	// task with a saturated binary p̂∈{0,1} the graded mean moved beyond ε from p̂ — a
	// graded score manufacturing variance where the binary outcome is pinned.
	SaturatedDiverged bool
	// ArmRankFlipped is true when the arm-rank-sign sub-guard tripped: on some
	// informative task the graded ON−OFF direction disagreed in SIGN with the binary
	// ON−OFF direction — graded ranks the arms differently from the oracle.
	ArmRankFlipped bool
	// MaxSatSwing is the largest |GradedMean − p̂| observed on any saturated task (the
	// leak magnitude for the saturated-divergence guard; 0 when no saturated task had
	// graded).
	MaxSatSwing float64
	// FlippedTasks lists the task IDs where the arm-rank sign disagreed (for the report).
	FlippedTasks []string
	// SaturatedTasks lists the saturated task IDs whose graded swing exceeded ε.
	SaturatedTasks []string
}

// EstimateBernoulli is the pure reduction over per-task high-replays counts. offs is
// the BASELINE (flag-OFF, K=1-equivalent high-replays) arm. ons (optional) is the ON
// (config-under-test) arm for the two-proportion capability test. delib (optional) is
// the DELIBERATIVE arm (THOUGHT_DELIBERATIVE_K) for the robustness analytic-vs-
// empirical q cross-check. All three are aligned by task (same order, same TaskID).
//
// crossRates (optional) is a multi-launch rate matrix [launch][task] for the STRONGER
// cross-launch overdispersion test; nil → the single-launch dispersion screen only.
//
// Default Mode (off / "") → the degenerate counts-only anchor (no Bernoulli pass), so
// the call is byte-identical-safe when off.
func EstimateBernoulli(
	offs, ons, delib []BernTaskInput,
	crossRates [][]float64, crossKs []int,
	cfg BernoulliConfig,
) BernoulliReport {
	rep := BernoulliReport{
		Mode:  normBernMode(cfg.Mode),
		Delta: cfg.delta(),
	}
	rep.Tasks = len(offs)
	if rep.Mode == EstBernOff {
		rep.Verdict = BernDegenerate
		return rep
	}

	// --- per-task Bernoulli estimates (capability + variance-by-formula + pass^k) ---
	rep.PassKAt = cfg.PassK
	rep.PerTask = make([]BernTaskEstimate, len(offs))
	var pass1Sum, passKSum float64
	for i, in := range offs {
		rep.PerTask[i] = estimateBernTask(in, cfg.PassK)
		if rep.PerTask[i].Informative {
			rep.TaskEff++
		}
		if cfg.PassK > 0 {
			pass1Sum += rep.PerTask[i].PHat
			passKSum += rep.PerTask[i].PassK
		}
	}
	if cfg.PassK > 0 && len(offs) > 0 {
		rep.MeanPass1 = pass1Sum / float64(len(offs))
		rep.MeanPassK = passKSum / float64(len(offs))
	}

	// --- graded leakage guard (§2.4 ported): gate GradedUsed behind BOTH sub-guards ---
	// estimateBernTask sets per-task GradedUsed=true whenever GradedN>=2; the guard is the
	// CONTRAST-AWARE gate that DROPS graded (clears GradedUsed across the OFF arm's
	// estimates → the report falls back to binary) unless both the saturated-divergence
	// and the arm-rank-sign sub-guards pass. This mirrors estimator.go's CUPED guard:
	// trust the variance-reducing covariate ONLY after it is proven treatment-orthogonal.
	rep.Graded = applyGradedGuard(offs, ons)
	if rep.Graded.Present && !rep.Graded.Used {
		for i := range rep.PerTask {
			rep.PerTask[i].GradedUsed = false
			rep.PerTask[i].GradedMean = 0
			rep.PerTask[i].GradedSE = 0
		}
		rep.Notes = append(rep.Notes, gradedLeakNote(rep.Graded))
	}

	z := bernZ95

	// --- two-proportion capability test ---
	rep.HasABContrast = len(ons) == len(offs) && len(ons) > 0
	if rep.HasABContrast {
		rep.PerTaskDiff = make([]BernPairedTaskDiff, len(offs))
		for i := range offs {
			rep.PerTaskDiff[i] = twoProportionDiffTask(offs[i], ons[i], z)
			if rep.PerTaskDiff[i].Moved {
				rep.TasksMoved = append(rep.TasksMoved, offs[i].TaskID)
			}
		}
		rep.MeanDiff, rep.MeanDiffCILo, rep.MeanDiffCIHi, rep.MeanDiffSE = aggregateProportionDiff(rep.PerTaskDiff, offs, ons, z)
	}

	// --- deliberative robustness: analytic vs empirical q ---
	rep.HasDeliberative = len(delib) == len(offs) && len(delib) > 0 && cfg.DeliberativeK > 1
	if rep.HasDeliberative {
		rep.PerTaskDelib = make([]BernDeliberativeTask, len(offs))
		var vb, ve, va float64
		var n int
		for i := range offs {
			dt := estimateDeliberativeTask(offs[i], delib[i], cfg.DeliberativeK, cfg.divergenceTol())
			rep.PerTaskDelib[i] = dt
			if dt.Diverged {
				rep.DeliberativeDiverged = append(rep.DeliberativeDiverged, offs[i].TaskID)
			}
			// average the variances over informative base tasks (saturated → 0 either way).
			if rep.PerTask[i].Informative {
				vb += dt.VarBase
				ve += dt.VarEmp
				va += dt.VarAnalytic
				n++
			}
		}
		if n > 0 {
			rep.MeanVarBase = vb / float64(n)
			rep.MeanVarEmp = ve / float64(n)
			rep.MeanVarAnalytic = va / float64(n)
		}
	}

	// --- adaptive-K allocation recommender ---
	target := cfg.targetHalf()
	kMin, kMax := cfg.allocKMin(), cfg.allocKMax()
	rep.Allocation = make([]BernAllocation, len(rep.PerTask))
	for i := range rep.PerTask {
		rep.Allocation[i] = recommendAllocation(rep.PerTask[i], target, z, kMin, kMax)
		rep.AdaptiveTotalK += rep.Allocation[i].RecommendedK
	}
	// the UNIFORM K to hit the same WORST-CASE precision: size for the mid-range
	// p=0.5 task (the most demanding), applied to every task. This is the honest
	// uniform baseline the adaptive plan saves against.
	rep.UniformK = recommendAllocation(
		BernTaskEstimate{PHat: 0.5, K: 1, SEMean: math.Sqrt(0.25)},
		target, z, kMin, kMax,
	).RecommendedK
	rep.UniformTotalK = rep.UniformK * len(rep.PerTask)
	rep.AllocSavingK = rep.UniformTotalK - rep.AdaptiveTotalK

	// --- overdispersion self-check (the guardrail) ---
	if len(crossRates) >= 2 {
		rep.Overdispersion = checkOverdispersionCross(crossRates, crossKs)
	} else {
		rep.Overdispersion = checkOverdispersionSingle(offs)
		if rep.Overdispersion.PooledMisfit {
			rep.Notes = append(rep.Notes,
				"single-launch pooled-rate misfit (D="+fmt.Sprintf("%.2f", rep.Overdispersion.Statistic)+
					"): the tasks do not share one rate (heterogeneity OR a shock). With ONE launch these are "+
					"indistinguishable; the per-task CIs are still valid. Run >=2 launches for the DEFINITIVE "+
					"shock test (cross-launch) before trusting any POOLED Bernoulli quantity.")
		}
	}

	// pass^k trust: the closed form p^k rests on iid replays — the same precondition
	// the overdispersion guard tests. Trust pass^k only when the DEFINITIVE cross-launch
	// overdispersion test did NOT fire (a single-launch PooledMisfit is advisory and does
	// not revoke trust, mirroring how it does not flip the OVERDISPERSED verdict).
	rep.PassKTrusted = cfg.PassK > 0 && !rep.Overdispersion.Overdispersed
	if cfg.PassK > 0 && rep.Overdispersion.Overdispersed {
		rep.Notes = append(rep.Notes,
			"pass^k UNTRUSTWORTHY: the overdispersion guard fired (replays are NOT iid Bernoulli) "+
				"-> the p^k closed form is invalid here; the per-task pass^k are advisory only.")
	}

	rep.Verdict = deriveBernVerdict(rep)
	return rep
}

// deriveBernVerdict applies the gate: OVERDISPERSED (fall back) first, then
// GRADED-LEAK (graded dropped), then DEGENERATE, then FEASIBLE/NOISY off the capability
// resolution.
func deriveBernVerdict(rep BernoulliReport) BernVerdict {
	// OVERDISPERSED dominates: the p(1−p) shortcut is invalid → defer to the
	// variance-component path. Reported even with no AB contrast (it is a property of
	// the OFF arm's data). This is the most fundamental flag — the BINARY variance
	// itself is wrong — so it precedes GRADED-LEAK (which is only about the graded
	// enrichment, not the binary gate).
	if rep.Overdispersion.Overdispersed {
		return BernOverdispersed
	}
	// GRADED-LEAK dominates next (mirrors OVERDISPERSED): the graded outcome failed the
	// leakage guard and was DROPPED. It is an instrument-integrity flag — the binary
	// capability quantities below are still valid; this surfaces that the graded
	// enrichment was unsafe and the report fell back to binary. Only fires when graded
	// was actually PRESENT (a graded vector was supplied) AND dropped.
	if rep.Graded.Present && !rep.Graded.Used {
		return BernGradedLeak
	}
	if rep.TaskEff < 1 {
		return BernDegenerate
	}
	if !rep.HasABContrast {
		// no effect to gate — single-arm characterization only.
		return BernDegenerate
	}
	// FEASIBLE iff the aggregate effect's CI lower bound clears 0 (a resolved positive
	// effect) OR the upper bound is below 0 (a resolved negative effect) — a resolved
	// effect either way; NOISY when the CI straddles 0.
	if rep.MeanDiffCILo > 0 || rep.MeanDiffCIHi < 0 {
		return BernFeasible
	}
	return BernNoisy
}

func normBernMode(m BernMode) BernMode {
	if m == EstBernOn {
		return EstBernOn
	}
	return EstBernOff
}

// --- render ----------------------------------------------------------------

// Render produces the plain-text Bernoulli report (no emoji, box-drawing only).
func (rep BernoulliReport) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("BERNOULLI HIGH-K SINGLE-LAUNCH ESTIMATOR (realhard)\n")
	w("mode: %-9s  tasks: %d  T_eff(informative): %d  AB: %v  deliberative: %v\n",
		rep.Mode, rep.Tasks, rep.TaskEff, rep.HasABContrast, rep.HasDeliberative)
	w("%s\n", strings.Repeat("=", 72))

	if rep.Mode == EstBernOff {
		w("BERNOULLI MODE OFF — no pass (counts-only anchor).\n")
		return b.String()
	}

	w("VERDICT: %s\n", rep.Verdict)
	if rep.Verdict == BernOverdispersed {
		w("  *** OVERDISPERSED: within-task replays are NOT iid Bernoulli (shared shock present).\n")
		w("      The p(1-p) variance shortcut is INVALID -> fall back to the launch\n")
		w("      VARIANCE-COMPONENT path (estimator.go --estimator paired). Do NOT trust\n")
		w("      the Bernoulli variances below.\n")
	}
	if rep.Verdict == BernGradedLeak {
		w("  *** GRADED-LEAK: the graded (continuous) outcome FAILED the leakage guard ->\n")
		w("      DROPPED. The binary capability quantities below are still valid; the graded\n")
		w("      enrichment was treatment-contaminated / variance-manufacturing.\n")
	}
	w("\n")

	// per-task capability + Wilson CI + variance.
	w("PER-TASK CAPABILITY (p-hat = solved/K, Wilson 95%% CI, outcome variance p(1-p))\n")
	sorted := append([]BernTaskEstimate(nil), rep.PerTask...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TaskID < sorted[j].TaskID })
	for _, e := range sorted {
		grad := ""
		if e.GradedUsed {
			grad = fmt.Sprintf("  graded=%.3f±%.3f", e.GradedMean, e.GradedSE)
		}
		w("  %-22s  p=%.3f (%d/%d)  CI[%.3f,%.3f]  var=%.3f  SE=%.3f%s\n",
			e.TaskID, e.PHat, e.Solved, e.K, e.WilsonLo, e.WilsonHi, e.OutcomeVar, e.SEMean, grad)
	}
	w("\n")

	// pass^k reliability (the brittleness axis): per-task pass@1 (p̂) vs pass^k (p̂^k).
	if rep.PassKAt > 0 {
		w("PASS^k RELIABILITY (k=%d): pass@1=p-hat vs pass^k=p^k (the tau2-bench brittleness axis)\n", rep.PassKAt)
		if !rep.PassKTrusted {
			w("  *** pass^k UNTRUSTWORTHY: overdispersion fired -> the iid p^k closed form is invalid.\n")
		}
		for _, e := range sorted {
			w("  %-22s  pass@1=%.3f  pass^%d=%.3f  CI[%.3f,%.3f]\n",
				e.TaskID, e.PHat, e.PassKAt, e.PassK, e.PassKLo, e.PassKHi)
		}
		w("  ----\n")
		w("  AGGREGATE mean pass@1=%.4f  mean pass^%d=%.4f  (reliability gap %.4f)\n",
			rep.MeanPass1, rep.PassKAt, rep.MeanPassK, rep.MeanPass1-rep.MeanPassK)
		w("\n")
	}

	if rep.HasABContrast {
		w("TWO-PROPORTION CAPABILITY TEST (ON-OFF per task, Newcombe CI)\n")
		sd := append([]BernPairedTaskDiff(nil), rep.PerTaskDiff...)
		sort.SliceStable(sd, func(i, j int) bool { return sd[i].TaskID < sd[j].TaskID })
		for _, d := range sd {
			moved := ""
			if d.Moved {
				moved = "  MOVED"
			}
			w("  %-22s  OFF=%.3f ON=%.3f  diff=%+.3f  CI[%+.3f,%+.3f]%s\n",
				d.TaskID, d.POff, d.POn, d.Diff, d.CILo, d.CIHi, moved)
		}
		w("  ----\n")
		w("  AGGREGATE diff: %+.4f  95%% CI[%+.4f,%+.4f]  SE=%.4f\n",
			rep.MeanDiff, rep.MeanDiffCILo, rep.MeanDiffCIHi, rep.MeanDiffSE)
		if len(rep.TasksMoved) > 0 {
			w("  tasks moved beyond their CI: %s\n", strings.Join(rep.TasksMoved, ", "))
		}
		w("\n")
	}

	if rep.HasDeliberative {
		w("DELIBERATIVE ROBUSTNESS (k=%d): empirical q-hat vs analytic binomial-majority q\n", rep.PerTaskDelib[0].K)
		dd := append([]BernDeliberativeTask(nil), rep.PerTaskDelib...)
		sort.SliceStable(dd, func(i, j int) bool { return dd[i].TaskID < dd[j].TaskID })
		for _, d := range dd {
			div := ""
			if d.Diverged {
				div = "  DIVERGED(sub-episodes not independent?)"
			}
			w("  %-22s  p=%.3f  q_emp=%.3f  q_analytic=%.3f  var %.3f->%.3f%s\n",
				d.TaskID, d.PBase, d.QEmpirical, d.QAnalytic, d.VarBase, d.VarEmp, div)
		}
		w("  ----\n")
		w("  mean outcome-variance: base p(1-p)=%.4f  empirical q(1-q)=%.4f  analytic=%.4f\n",
			rep.MeanVarBase, rep.MeanVarEmp, rep.MeanVarAnalytic)
		if rep.MeanVarBase > 0 {
			w("  variance reduction (empirical): x%.3f  (lower = more robust)\n", rep.MeanVarEmp/rep.MeanVarBase)
		}
		if len(rep.DeliberativeDiverged) > 0 {
			w("  *** analytic-vs-empirical DIVERGENCE on: %s\n", strings.Join(rep.DeliberativeDiverged, ", "))
			w("      (the k sub-episodes are NOT independent -> the majority-concentration\n")
			w("       formula over-predicts; a real finding, not an estimator bug.)\n")
		}
		w("\n")
	}

	w("ADAPTIVE-K ALLOCATION (Neyman/Fisher: target CI half-width %.3f)\n", rep.targetHalfForRender())
	al := append([]BernAllocation(nil), rep.Allocation...)
	sort.SliceStable(al, func(i, j int) bool { return al[i].TaskID < al[j].TaskID })
	for _, a := range al {
		w("  %-22s  p=%.3f  current K=%d -> recommend K=%d\n", a.TaskID, a.PHat, a.CurrentK, a.RecommendedK)
	}
	w("  ----\n")
	w("  uniform plan: %d tasks x K=%d = %d replays\n", len(rep.Allocation), rep.UniformK, rep.UniformTotalK)
	w("  adaptive plan: %d replays total  (saving %d replays vs uniform)\n", rep.AdaptiveTotalK, rep.AllocSavingK)
	w("\n")

	w("OVERDISPERSION SELF-CHECK (iid-Bernoulli guardrail)\n")
	w("  basis: %s\n", rep.Overdispersion.Reason)
	if rep.Overdispersion.CrossLaunchAvailable {
		w("  within-launch var=%.5f  cross-launch var=%.5f  ratio=%.3f\n",
			rep.Overdispersion.WithinVar, rep.Overdispersion.CrossVar, rep.Overdispersion.Statistic)
	} else {
		w("  dispersion statistic (Pearson/df): %.3f  (~1 = iid-consistent; >>1 = overdispersed)\n",
			rep.Overdispersion.Statistic)
	}
	switch {
	case rep.Overdispersion.Overdispersed:
		w("  -> OVERDISPERSED (cross-launch): the Bernoulli formula is INVALID; use the variance-component path.\n")
	case rep.Overdispersion.PooledMisfit:
		w("  -> POOLED-MISFIT (advisory): tasks do not share one rate (heterogeneity OR a shock).\n")
		w("     Indistinguishable with ONE launch; per-task CIs still valid. Run >=2 launches for the\n")
		w("     DEFINITIVE cross-launch shock test before trusting any POOLED Bernoulli quantity.\n")
	default:
		w("  -> iid-consistent: the Bernoulli p(1-p) shortcut holds.\n")
	}

	if rep.Graded.Present {
		w("\nGRADED LEAKAGE GUARD (decline-ordinal, §2.4 ported)\n")
		w("  max saturated swing |graded - p-hat| : %.3f  (eps=%.2f)\n", rep.Graded.MaxSatSwing, gradedSatEps)
		switch {
		case rep.Graded.Used:
			w("  -> graded KEPT: both sub-guards passed (no saturated divergence, no arm-rank flip).\n")
		default:
			if rep.Graded.SaturatedDiverged {
				w("  DROPPED (leak): saturated-divergence on %s (graded moved where binary is pinned).\n",
					strings.Join(rep.Graded.SaturatedTasks, ","))
			}
			if rep.Graded.ArmRankFlipped {
				w("  DROPPED (leak): arm-rank sign flip on %s (graded ranks the arms vs the oracle).\n",
					strings.Join(rep.Graded.FlippedTasks, ","))
			}
			w("  -> graded DROPPED; report is binary-only.\n")
		}
	}

	if len(rep.Notes) > 0 {
		w("\nNOTES\n")
		for _, n := range rep.Notes {
			w("  - %s\n", n)
		}
	}
	return b.String()
}

func (rep BernoulliReport) targetHalfForRender() float64 {
	if len(rep.Allocation) > 0 {
		return rep.Allocation[0].TargetHalf
	}
	return rep.Delta
}

// --- single-launch high-replays driver -------------------------------------

// launchTaskCounts reduces ONE launch's flat harness results into per-task (solved,
// K) counts aligned to taskIDs/caps — the BernTaskInput the Bernoulli estimator
// consumes. Unlike launchTaskRates (which collapses to a rate, losing K), this keeps
// the integer success-count + trial-count the Wilson CI / two-proportion test / chi-
// square dispersion all need. The harness arm only (the Bernoulli question is the
// harness's per-task p, the same arm σ_R measures).
func launchTaskCounts(results []RunResult, taskIDs []string, caps []Capability) []BernTaskInput {
	return launchTaskCountsArm(results, taskIDs, caps, ArmHarness)
}

// launchTaskCountsArm is launchTaskCounts parameterized by the arm to tally — the
// bare-vs-harness A/B (abreport.go) needs the per-task (solved, K) counts for the
// bare, harness, AND single-strong arms (the guard pairs harness against single-
// strong). launchTaskCounts is the ArmHarness specialization the Bernoulli path uses.
func launchTaskCountsArm(results []RunResult, taskIDs []string, caps []Capability, arm string) []BernTaskInput {
	solved := map[string]int{}
	total := map[string]int{}
	// per-task graded vectors (the answer-derived decline-ordinal scores) — kept as the
	// raw per-attempt values so the mean + SAMPLE SD are exact, not running moments. Only
	// HasGraded results contribute (today: OracleDecline tasks). When a task has no graded
	// result, GradedN stays 0 and the estimator falls back to binary for it (byte-safe).
	graded := map[string][]float64{}
	for _, r := range results {
		if r.Arm != arm {
			continue
		}
		total[r.TaskID]++
		if r.Verdict.Solved {
			solved[r.TaskID]++
		}
		if r.HasGraded {
			graded[r.TaskID] = append(graded[r.TaskID], r.Graded)
		}
	}
	out := make([]BernTaskInput, len(taskIDs))
	for i, id := range taskIDs {
		var cp Capability
		if i < len(caps) {
			cp = caps[i]
		}
		in := BernTaskInput{TaskID: id, Capability: cp, Solved: solved[id], K: total[id]}
		if gv := graded[id]; len(gv) > 0 {
			in.GradedMean = meanOf(gv)
			in.GradedSD = sampleSD(gv)
			in.GradedN = len(gv)
		}
		out[i] = in
	}
	return out
}

// RunBernoulli drives the BERNOULLI HIGH-K SINGLE-LAUNCH measurement: it runs the
// whole realhard suite ONCE at `replays` K per task (one launch — the cheap path, NOT
// R independent launches) and reduces to per-task (solved, K) counts. With onConfig
// non-nil it also runs an ON arm (the config-under-test) on the SAME launch seed, and
// with delibConfig non-nil a DELIBERATIVE arm (THOUGHT_DELIBERATIVE_K) — both as
// counts. The three count slices feed EstimateBernoulli.
//
// This is the SINGLE-LAUNCH analogue of RunSigmaREstimator: same seed discipline, same
// harness-arm-only, but it retains the integer counts (not just rates) the Bernoulli
// CIs need, and it does ONE launch (the whole point — high K, one launch). For the
// cross-launch overdispersion test the caller may run RunBernoulli at several seedBases
// and pass the rate matrix; the default single-launch path uses the in-data Pearson
// dispersion screen.
//
// Determinism: the per-replay seed is seedBase + r (RunSuite's discipline). Headless-
// pure: no model in the reducer; the only nondeterminism is the upstream episodes.
func RunBernoulli(
	replays int, seedBase int64, maxTicks, concurrency int,
	workspaceRoot string, factory BackendFactory, substrate string,
	onConfig, delibConfig func(run func() error) error,
	taskFilter string,
) (off, on, delib []BernTaskInput, err error) {
	if replays < 1 {
		replays = 1
	}
	tasks := FilterTasks(Tasks(), taskFilter)
	taskIDs := make([]string, len(tasks))
	caps := make([]Capability, len(tasks))
	for i, tk := range tasks {
		taskIDs[i] = tk.ID
		caps[i] = tk.Capability
	}
	runSuiteAt := func() ([]RunResult, error) {
		res, _, err := RunSuite(SuiteConfig{
			Factory:       factory,
			Replays:       replays,
			SeedBase:      seedBase,
			MaxTicks:      maxTicks,
			Concurrency:   concurrency,
			WorkspaceRoot: workspaceRoot,
			OnlyArm:       ArmHarness,
			TaskFilter:    taskFilter,
		})
		return res, err
	}

	offRes, err := runSuiteAt()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("bernoulli OFF launch: %w", err)
	}
	off = launchTaskCounts(offRes, taskIDs, caps)

	if onConfig != nil {
		var onRes []RunResult
		if e := onConfig(func() error { r, er := runSuiteAt(); onRes = r; return er }); e != nil {
			return nil, nil, nil, fmt.Errorf("bernoulli ON launch: %w", e)
		}
		on = launchTaskCounts(onRes, taskIDs, caps)
	}
	if delibConfig != nil {
		var dRes []RunResult
		if e := delibConfig(func() error { r, er := runSuiteAt(); dRes = r; return er }); e != nil {
			return nil, nil, nil, fmt.Errorf("bernoulli DELIB launch: %w", e)
		}
		delib = launchTaskCounts(dRes, taskIDs, caps)
	}
	return off, on, delib, nil
}

// --- graded-mode bootstrap CI (optional, seeded) ---------------------------

// gradedMeanBootstrapCI is the optional seeded bootstrap CI for a graded-signal
// per-task mean (when the analytic SE is not trusted, e.g. a non-Gaussian partial-
// credit distribution). values are the per-attempt continuous scores. Deterministic:
// the cpyrand stream is seeded. Kept available for callers that supply raw graded
// vectors; the default graded read uses the analytic SE (GradedSD/sqrt(N)).
func gradedMeanBootstrapCI(values []float64, nBoot int, seed uint64, z float64) (mean, lo, hi float64) {
	n := len(values)
	if n == 0 {
		return 0, 0, 0
	}
	mean = meanOf(values)
	if n < 2 {
		return mean, mean, mean
	}
	rng := cpyrand.New(seed)
	means := make([]float64, 0, nBoot)
	for b := 0; b < nBoot; b++ {
		var s float64
		for i := 0; i < n; i++ {
			s += values[rng.Intn(n)]
		}
		means = append(means, s/float64(n))
	}
	sort.Float64s(means)
	_ = z
	lo = percentile(means, 0.025)
	hi = percentile(means, 0.975)
	return mean, lo, hi
}
