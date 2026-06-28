package estimate

// staleness.go is SLAM M4 — FRESHNESS / STALENESS DECAY (the dynamic-map process noise, P4), the
// stateful side. M1 (estimate.go/innovation.go) is the only var-REDUCER (a grounded observation shrinks a
// belief's variance); M2 (covariance.go) added the correlated loss-of-certainty. M4 adds the per-tick
// process noise the design's P4 mandates: a belief grounded long ago, left un-refreshed, must GROW its
// variance back toward uncertain — because the non-stationary world it described may have MOVED since it
// was last observed. The longer un-refreshed, the less it can be trusted, the more the estimator wants to
// re-observe it.
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §4 (P4) "Non-stationary world =>
// mandatory decay (Q>0)" + §3b.2 (the Estimate envelope's LastObs / Dynamics fields: "world/time MUST
// drift and decay toward stale, re-observe; identity is near-stationary") + §6 M4. Pure CONTROL: the
// decay math is control.StalenessInflation (a closed-form geometric approach to a saturating ceiling); this
// file holds the per-belief last-observation index + the per-tick sweep that calls it. No model call ever.
//
// CONSISTENCY (stays inside the M1 §0 / M5 invariant): staleness decay only ever RAISES a variance (loses
// certainty), never lowers it. Becoming LESS certain can never be spurious information — you cannot
// fabricate certainty by admitting a fact has gone stale — so M4 is provably safe against the Huang-2010
// EKF-inconsistency the M5 witness guards (the M5 infoGain accounting returns 0 for any variance GROWTH;
// only a direct grounded Observe() may ever shrink a variance). The Decay() sweep never touches lastObs or
// firstGround, so a re-grounding still resets the freshness clock and the FEJ anchor is untouched.

import (
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// decaying reports whether the M4 staleness layer should run the per-tick decay sweep: the estimator is
// active (slam.innovation, so there IS a grounded variance trajectory to decay) AND the slam.staleness
// knob is on AND the process-noise rate is positive (q==0 is the stationary case — no decay). When false
// the sweep is skipped entirely so a staleness-OFF run does exactly the M1/M2 work and is byte-identical.
func (e *Estimator) decaying() bool {
	return e.Enabled() && e.cfg.Staleness && e.cfg.StalenessQ > 0
}

// Decaying is the exported guard the engine checks before calling Decay (so the OFF path costs nothing
// and is byte-identical). Nil-safe.
func (e *Estimator) Decaying() bool { return e != nil && e.decaying() }

// SetStaleness honours a live flip of the slam.staleness knob (the M4 freshness layer). The layer only
// does anything when the estimator is also Enabled (it decays the M1 variance trajectory); flipping it
// OFF freezes the variances where they are (a re-flip-ON resumes decaying from the current ages). Nil-safe.
func (e *Estimator) SetStaleness(on bool) {
	if e == nil {
		return
	}
	e.cfg.Staleness = on
}

// SetStalenessQ honours a live tune of the slam.staleness_q rate. Clamped to [0,1] (a negative rate is
// stationary; >1 is full decay in one tick). Nil-safe.
func (e *Estimator) SetStalenessQ(q float64) {
	if e == nil {
		return
	}
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	e.cfg.StalenessQ = q
}

// Decay is the SLAM M4 per-tick PROCESS-NOISE sweep: every belief that has been grounded (has a freshness
// stamp in lastObs) has its variance GROWN by control.StalenessInflation as a function of its un-refreshed
// age (age = curTick - lastObs), toward the PriorVar0 ceiling (a forever-stale belief becomes at most as
// uncertain as a never-grounded one, never more). A belief grounded THIS tick (age 0) is fresh and
// untouched; a belief never grounded (absent from lastObs) is already at the high prior and has nothing to
// decay. Emits one estimate.decay per belief whose variance actually changed (a no-op decay — fresh, or
// already at ceiling — emits nothing, so the bus is not spammed). Returns the count decayed (for the
// caller / tests / observability).
//
// Called once per LIVE tick from the engine's Step() (after SetTick stamps the seeded tick). No-op
// (returns 0, emits nothing) unless the M4 layer is active, so a staleness-OFF run is byte-identical. The
// sweep iterates lastObs in a DETERMINISTIC order (it sorts the keys) so the event stream is reproducible
// on the seeded loop.
func (e *Estimator) Decay() int {
	if !e.decaying() {
		return 0
	}
	now := e.tick()
	q := e.cfg.StalenessQ
	ceiling := e.cfg.PriorVar0
	ids := e.sortedBeliefIDs(e.lastObs)
	decayed := 0
	for _, id := range ids {
		obsTick := e.lastObs[id]
		age := float64(now - obsTick)
		if age <= 0 {
			continue // grounded this tick (or a non-monotone clock) -> fresh, no decay
		}
		before := e.varOf(id)
		after := control.StalenessInflation(before, age, q, ceiling)
		if after <= before {
			continue // already at/above the ceiling, or q produced no change -> skip (no event)
		}
		// Grow the variance. This is the ONLY place M4 writes a variance, and it always GROWS it (loses
		// certainty), so it can never gain spurious information (the §0/M5 invariant holds — Decay never
		// adds to GroundedGain and the M5 monitor's infoGain returns 0 for a growth). The write uses the RAW
		// (un-rounded) value so the per-tick recurrence stays exact; only the EVENT below is rounded.
		e.varByID[id] = after
		// Only EMIT a decay witness when the change is visible at wire precision — as a belief approaches the
		// saturation ceiling the per-tick growth shrinks below 0.001 and would round to a flat no-op event,
		// spamming the bus; the variance still grows (the write above is raw), it just stops being newsworthy.
		if round3(after) <= round3(before) {
			continue
		}
		decayed++
		e.emit(events.EstimateDecay, "decay "+string(id)+" (stale "+itoa(int(age))+"t)", events.D{
			"id":         string(id),
			"age":        int(age),
			"q":          round3(q),
			"priorVar":   round3(before),
			"postVar":    round3(after),
			"varInflate": round3(after - before),
			"ceiling":    round3(ceiling),
		})
	}
	return decayed
}

// sortedBeliefIDs returns the keys of a BeliefID-keyed map in a deterministic (sorted) order, so the
// per-tick decay sweep's event stream is reproducible on the seeded loop. Kept here (rather than reusing
// covariance.go's sort over a different type) to avoid a generic-over-map dependency.
func (e *Estimator) sortedBeliefIDs(m map[BeliefID]int) []BeliefID {
	if len(m) == 0 {
		return nil
	}
	out := make([]BeliefID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	// simple insertion sort over the small per-run belief set (no sort import churn in this leaf file)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// StalenessVitals is the compact M4 readout for the Ctrl+O runtime monitor: how many grounded beliefs the
// freshness index tracks and the oldest un-refreshed age (the staleness pressure — high = the harness is
// sitting on facts it has not re-observed in a while). Returns zeros when the M4 layer is off. Read-only.
func (e *Estimator) StalenessVitals() (tracked, oldestAge int) {
	if !e.decaying() {
		return 0, 0
	}
	now := e.tick()
	for _, t := range e.lastObs {
		tracked++
		if age := now - t; age > oldestAge {
			oldestAge = age
		}
	}
	return tracked, oldestAge
}
