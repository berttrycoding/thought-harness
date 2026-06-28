package control

// infogain.go is the SLAM M6 ACTIVE-INFERENCE info-gain math (next-best-observation / active-SLAM
// next-best-view), made explicit — Pattern A (pure CONTROL, NO model call ever). It is the principled
// explore/exploit term: given the current joint posterior, which observation reduces the most
// uncertainty? The math is closed-form, deterministic, RNG-free and clock-free, so it lives in this
// Tier-1 leaf (never imports backends, never references the test double).
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3b.3 #7 (Exploration / active inference)
// + §5 #4 (directed grounding / principled exploration: "choose what to verify next by expected
// uncertainty reduction, not just outcome reward — directly targeting the measured under-grounding /
// give-up behaviour") + §6 M6. The STATEFUL side (ranking the live tracked beliefs, ticked across the
// run) lives in internal/estimate; this file holds only the pure per-candidate step it calls.
//
// THE THING THIS ADDS over M1/M2: M1 CORRECTS a belief once reality has spoken; M2 records WHICH beliefs
// co-vary. M6's info-gain answers the question that comes BEFORE either — given a budget for ONE more
// grounding observation, which belief should the harness verify NEXT? The answer is the one whose
// grounding shrinks the most JOINT uncertainty: a high-variance belief gains more from being grounded
// (it has more uncertainty to remove), and a belief that many siblings CO-VARY with gains a bonus (the
// active-inference epistemic value of grounding a shared root is leveraged across everything that root
// backs — grounding a high-fan-out upstream beats grounding an isolated leaf at equal variance).
//
// CONSISTENCY (stays inside the M1 §0 / M5 invariant): info-gain is a PURE RANKING signal — it computes
// an expected uncertainty reduction to choose what to observe, and NEVER itself alters a belief's
// variance/mean. Only a DIRECT grounded observation (control.Innovate via estimate.Observe) may ever
// shrink a variance. So M6 cannot fabricate certainty — it only DIRECTS the grounding that legitimately
// can.

// ExpectedInfoGain is the expected information (inverse-variance) gain from grounding ONE belief at a
// given measurement precision, accounting for the JOINT structure via its co-varying siblings. Pure;
// no RNG, no clock; a deterministic function of its arguments only.
//
//	priorVar     the belief's current variance P (high = uncertain / self-derived; 0 = already certain)
//	obsPrec      R^-1 of the observation that would ground it (the trust-tier precision, control.TierPrecision)
//	corrReach    the joint-reach bonus from co-varying siblings: sum over siblings of their correlation
//	             coefficient rho in [0,1] (0 = an isolated belief, no joint leverage; M2's covGraph supplies it)
//
// Scalar self-info (the belief's own uncertainty reduction). The post-observation variance of a scalar
// Kalman update is P_post = P*R/(P+R) = P/(1 + P*obsPrec), so the information added (1/P_post - 1/P) is
// EXACTLY obsPrec — grounding adds R^-1 of information regardless of the prior. But the VALUE of removing
// uncertainty is larger when there is more of it, so we weight by the fraction of the belief's own
// information the observation supplies: selfGain = obsPrec * P/(P+R) = obsPrec * (P*obsPrec)/(1+P*obsPrec).
// This is 0 for an already-certain belief (P=0: nothing to learn) and rises monotonically with P toward
// obsPrec — so a high-variance (uncertain) belief is preferred, the next-best-VIEW criterion.
//
//	selfGain = obsPrec * (P*obsPrec) / (1 + P*obsPrec)
//	joint    = selfGain * (1 + corrReach)     # the active-SLAM leverage across co-varying siblings
//
// Returns the JOINT expected info gain. Monotone non-decreasing in priorVar, obsPrec and corrReach; 0
// when priorVar<=0 (certain — nothing to learn) or obsPrec<=0 (the observation carries no information).
// Grounding a fully-isolated belief (corrReach=0) reduces to its own selfGain — so an episode with no
// shared grounding ranks purely by per-belief uncertainty, exactly the M1 view.
func ExpectedInfoGain(priorVar, obsPrec, corrReach float64) float64 {
	if priorVar <= 0 || obsPrec <= 0 {
		return 0 // already certain, or the observation carries no information
	}
	if corrReach < 0 {
		corrReach = 0
	}
	pInfo := priorVar * obsPrec               // P/R ratio: how much of the prior the observation can explain
	selfGain := obsPrec * pInfo / (1 + pInfo) // obsPrec * P/(P+R) — own uncertainty reduction, in [0, obsPrec)
	return selfGain * (1 + corrReach)         // joint leverage across the co-varying siblings
}
