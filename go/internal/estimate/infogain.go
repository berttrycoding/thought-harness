package estimate

// infogain.go is SLAM M6 — the ACTIVE-INFERENCE next-best-observation ranking (active-SLAM
// next-best-view). It is the principled explore/exploit signal: given the live joint posterior the M1/M2
// machinery maintains, which belief should the harness GROUND next to reduce the most uncertainty?
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3b.3 #7 (Exploration / active inference)
// + §5 #4 (directed grounding: "choose what to verify next by expected uncertainty reduction, not just
// outcome reward — directly targeting the measured under-grounding / give-up behaviour") + §5b ("M6 gives
// the default-mode generator a principled curiosity drive") + §6 M6. Pure CONTROL (the per-candidate
// expected-info-gain math is control.ExpectedInfoGain; this file holds the stateful ranking over the live
// tracked beliefs and emits estimate.infogain). No model call ever.
//
// WHAT IT ADDS over M1/M2: M1 corrects a belief after reality has spoken; M2 records which beliefs
// co-vary. M6's ranking answers the question BEFORE either: of the beliefs the harness currently holds,
// which one, if grounded with ONE more observation, shrinks the most JOINT uncertainty? The answer
// weights two things the lower milestones already track — a belief's own variance (M1: more uncertainty
// to remove) AND its correlation REACH (M2: grounding a shared root leverages the observation across
// every co-varying sibling). So a high-fan-out, high-variance root is the next-best-view; an
// already-grounded (low-variance) isolated leaf is not.
//
// CONSISTENCY (stays inside the M1 §0 / M5 invariant): this is a PURE RANKING — it reads the variance
// trajectory and computes an expected uncertainty reduction to CHOOSE what to observe; it NEVER alters a
// belief's variance or mean. Only a direct grounded Observe() may shrink a variance. So M6 cannot
// fabricate certainty — it only DIRECTS the grounding that legitimately can (the active-inference
// epistemic value, never the self-restatement the §0 invariant forbids).

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// InfoGainCandidate is one ranked grounding target: the belief, its current variance, its correlation
// reach (the joint leverage from co-varying siblings), and the expected JOINT information gain of
// grounding it next at the given observation precision. The default-mode generator / Controller reads
// the top candidate as the next-best-observation (what to verify next).
type InfoGainCandidate struct {
	ID       BeliefID // the belief whose grounding is being scored
	PriorVar float64  // its current variance P (the M1 uncertainty)
	Reach    float64  // sum of co-varying siblings' correlation coefficients (the M2 joint reach; 0 = isolated)
	Gain     float64  // control.ExpectedInfoGain(PriorVar, obsPrec, Reach) — the joint expected info gain
}

// corrReach returns a belief's correlation REACH: the sum over its co-varying siblings of the pairwise
// correlation coefficient. It is the active-inference leverage term — grounding a belief many siblings
// co-vary with reduces THEIR uncertainty too, so it is worth more. Reads ONLY the sparse M2 covGraph (so
// the cost is bounded by the belief's own small sibling set, never the whole graph). Returns 0 when the
// covariance layer is off or the belief is isolated (no shared upstream) — then the ranking is purely by
// per-belief variance, exactly the M1 view.
func (e *Estimator) corrReach(id BeliefID) float64 {
	if e.cov == nil {
		return 0
	}
	var reach float64
	for _, sib := range e.cov.siblings(id) {
		reach += control.CorrelationCoefficient(e.cov.sharedCount(id, sib))
	}
	return reach
}

// exploring reports whether the M6 active-inference info-gain layer should rank this tick: the estimator
// is active (slam.innovation, so there IS a variance trajectory to rank) AND the slam.infogain knob is
// on. When false, NextBestObservation is a no-op (returns nil), so an infogain-OFF run does exactly the
// M1/M2 work and is byte-identical.
func (e *Estimator) exploring() bool { return e.Enabled() && e.cfg.InfoGain }

// Exploring is the exported guard the engine checks BEFORE doing the (otherwise-wasted) ranking work, so
// the OFF path adds zero info-gain computation and is byte-identical. Nil-safe.
func (e *Estimator) Exploring() bool { return e != nil && e.exploring() }

// SetInfoGain honours a live flip of the slam.infogain knob (the M6 active-inference layer). The layer
// only does anything when the estimator is also Enabled (it ranks the M1 variance trajectory); flipping
// it OFF stops ranking (no estimate.infogain event), a re-flip-ON resumes. Nil-safe.
func (e *Estimator) SetInfoGain(on bool) {
	if e == nil {
		return
	}
	e.cfg.InfoGain = on
}

// NextBestObservation ranks the live tracked beliefs by expected JOINT information gain of grounding
// them next at observation precision obsPrec, and returns the candidates best-first (the head is the
// next-best-observation: what to verify next). It is the active-SLAM next-best-view over the self-state
// estimator.
//
// The ranking weights each belief by its own variance (M1: uncertainty to remove) AND its correlation
// reach (M2: joint leverage across co-varying siblings) via control.ExpectedInfoGain. An ALREADY-grounded
// belief whose variance has shrunk to ~0 contributes ~0 gain and sinks to the bottom — so the harness is
// directed toward what it does NOT yet know, not toward re-confirming what it already grounded (which the
// P1 first-grounding lower bound says it cannot improve anyway). Ties break on BeliefID so the order is
// DETERMINISTIC (the seeded loop and any golden depend on it).
//
// It emits estimate.infogain carrying the ranked head + the candidate count, so the directed-grounding
// decision is visible on the bus / in the trace / in the TUI. No-op (returns nil, emits nothing) when the
// info-gain layer is off OR there are no tracked beliefs — so an episode with nothing to rank is
// byte-identical. PURE: it reads the side-table, never writes it.
func (e *Estimator) NextBestObservation(obsPrec float64) []InfoGainCandidate {
	if !e.exploring() || len(e.varByID) == 0 {
		return nil
	}
	cands := make([]InfoGainCandidate, 0, len(e.varByID))
	for id := range e.varByID {
		pv := e.varOf(id)
		reach := e.corrReach(id)
		cands = append(cands, InfoGainCandidate{
			ID:       id,
			PriorVar: pv,
			Reach:    reach,
			Gain:     control.ExpectedInfoGain(pv, obsPrec, reach),
		})
	}
	// Best-first by expected joint info gain; deterministic tie-break on the belief id so the ranking is
	// reproducible on the seeded loop (a non-deterministic sort would break the goldens when ON).
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].Gain != cands[j].Gain {
			return cands[i].Gain > cands[j].Gain
		}
		return cands[i].ID < cands[j].ID
	})

	top := cands[0]
	e.emit(events.EstimateInfoGain, "next-best-observation: "+string(top.ID), events.D{
		"id":         string(top.ID),
		"priorVar":   round3(top.PriorVar),
		"reach":      round3(top.Reach),
		"gain":       round3(top.Gain),
		"obsPrec":    round3(obsPrec),
		"candidates": len(cands),
	})
	return cands
}
