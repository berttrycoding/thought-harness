package control

// covariance.go is the SLAM M2 SPARSE-COVARIANCE / Information-layer math, made explicit — Pattern A
// (pure CONTROL, NO model call ever). M1 (innovation.go) made the per-belief scalar Kalman update
// explicit; M2 adds the OFF-DIAGONAL structure SLAM exists for: when two beliefs were derived from a
// COMMON upstream (a shared grounding / a shared ancestor thought), their errors share that common
// source and are therefore CORRELATED. The single SLAM payoff (Durrant-Whyte & Bailey, Part I, Thm 2):
// "the correlations ARE the information" — collapsing the joint posterior to independent scalars throws
// away exactly the structure that catches CORRELATED self-deception (two beliefs wrong in the same
// direction because one bad upstream). The math is closed-form, deterministic, RNG-free and clock-free,
// so it lives in this Tier-1 leaf (never imports backends, never references the test double).
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3b.3 #2 (Information / correlations) +
// §1 ("all map errors share a common source — the pose error at the moment each was observed") + §6 M2.
// The STATEFUL side (the sparse correlation graph, ticked across the run) lives in internal/estimate;
// this file holds only the pure step it calls.
//
// THE LOAD-BEARING CONSISTENCY GUARANTEE (keeps M2 inside the M1 §0 / M5 invariant): a correlated
// REFUTATION may only RAISE a sibling's variance (LOSE certainty), never lower it. Becoming LESS certain
// can never be spurious information (you cannot fabricate certainty by admitting doubt), so the
// correlated propagation is provably safe against the Huang-2010 EKF-inconsistency the M5 witness guards
// — only a DIRECT grounded observation (M1 Observe) may ever shrink a variance.

// CorrelatedInflation is the variance INCREASE a correlated sibling suffers when reality REFUTES a
// belief it co-varies with. The refuted belief's shared upstream just proved unreliable, so every belief
// derived from that same upstream becomes LESS trustworthy — its certainty is partly borrowed from a
// source that just failed. Returns the NEW (larger) sibling variance.
//
//	priorVar  the sibling's current variance P (high = uncertain / self-derived)
//	rho       the correlation coefficient in [0,1] (1 = same upstream, perfectly co-varying)
//	innovMag  |nu| of the refutation = how hard reality contradicted the co-varying belief
//
//	infl = rho * tanh(innovMag) * priorVar    # the borrowed-certainty the shared upstream can no longer back
//	post = priorVar + infl                     # variance GROWS — the sibling is now less certain
//
// tanh(innovMag) saturates the contradiction strength to (0,1) so a single huge residual cannot blow the
// variance up unboundedly (a bounded, graded loss of confidence). rho scales it by how strongly the two
// beliefs share their upstream: an independent belief (rho=0) is untouched; a fully-shared one (rho=1)
// loses the most. The result is monotone non-decreasing in priorVar, rho and innovMag, and equals
// priorVar exactly when rho==0 — so an empty correlation graph is a no-op (byte-identical).
func CorrelatedInflation(priorVar, rho, innovMag float64) float64 {
	if rho <= 0 || priorVar <= 0 || innovMag <= 0 {
		return priorVar // no correlation, nothing to lose, or no contradiction -> unchanged
	}
	if rho > 1 {
		rho = 1
	}
	infl := rho * tanhPos(innovMag) * priorVar
	return priorVar + infl
}

// CorrelationCoefficient maps the count of UPSTREAMS two beliefs share to a correlation coefficient in
// [0,1]. The more grounding ancestors two beliefs have in common, the more their errors co-vary. It is
// deliberately a saturating function of the shared count (1 - 1/(1+shared)) so the FIRST shared upstream
// already establishes strong correlation (rho=0.5) and additional shared ancestors push it toward 1
// with diminishing returns — sharing one bad root is already most of the danger. Zero shared upstreams
// -> rho 0 (independent beliefs, no edge, the sparse graph never stores them).
func CorrelationCoefficient(shared int) float64 {
	if shared <= 0 {
		return 0
	}
	return 1.0 - 1.0/(1.0+float64(shared))
}

// tanhPos is tanh for a non-negative argument, computed without importing math elsewhere in this leaf
// (control imports only stdlib). It returns a value in [0,1) that saturates: tanh(0)=0, tanh(1)~=0.76,
// tanh(3)~=0.995. Used to bound the contradiction strength so a correlated inflation stays graded.
func tanhPos(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// tanh(x) = (e^2x - 1) / (e^2x + 1). For large x this saturates to 1; guard the overflow.
	if x > 20 {
		return 1
	}
	e2 := expPos(2 * x)
	return (e2 - 1) / (e2 + 1)
}

// expPos is e^x for x >= 0 via the standard series, terminated when a term stops contributing. Kept
// local so this leaf depends only on stdlib arithmetic (no math import needed for these two helpers).
// Accurate to well within the 3-decimal wire precision the events round to; the inputs are small
// (2*innovMag, innovMag <= a few sigma) so convergence is fast.
func expPos(x float64) float64 {
	sum := 1.0
	term := 1.0
	for i := 1; i < 64; i++ {
		term *= x / float64(i)
		sum += term
		if term < 1e-15*sum {
			break
		}
	}
	return sum
}
