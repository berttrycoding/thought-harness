// durability.go adds two ADDITIVE durability-frontier units that COUPLE the regulator's measured
// regime to the policies that drive it — without touching the existing control loop (regulator.go) or
// the gain estimator (gain.go). They are the two pieces slice (f) of the cognition redesign asks for:
//
//  1. FrontierBarrier — a penalty that rises sharply as the measured excitation n → 1, so an activity
//     optimizer (the §5 experiment objective in docs/cognition/02-conscious.md) can push richness
//     (β_branch, τ, c) "up to the durability frontier and no further" (02 §5.4). Pure, no state.
//
//  2. MaxOutstanding / OutstandingAllowed — a MAX_OUTSTANDING back-pressure cap (default 8, mirroring
//     the fan-out ceiling W_max) the watched seam can consult BEFORE Fire, bounding async dead-time
//     (docs/cognition/04-seams.md §4 "Gap — bounded outstanding actions"). A pure predicate/cap;
//     the seam stays the owner of the outstanding set.
//
// NEITHER mutates the regulator. The barrier reads the live n via the existing N() accessor; the cap is
// a free function + a thin method that reads only the configured ceiling. This keeps the existing
// durability loop intact (the constraint of slice (f)) while exposing the frontier as a usable signal.
package regulator

import "math"

// MaxOutstandingDefault is the default MAX_OUTSTANDING back-pressure cap on in-flight async actions —
// the bound on async dead-time the watched seam consults before Fire (04-seams.md §4). It mirrors the
// fan-out ceiling W_max (config.WMax == 8): both are "8 concurrent things in flight" durability bounds,
// one on parallel branch fan-out, one on outstanding async actions. Kept here (not imported from config)
// so internal/regulator stays a low-level leaf with no config dependency; config.WMax documents the same
// constant on its side.
const MaxOutstandingDefault = 8

// barrierNMax clamps the n fed to the barrier just below 1.0, so a measured n that has already crossed
// the subcritical cliff yields a large-but-finite penalty (a usable gradient for the optimizer) instead
// of +Inf / NaN. 1 - 1e-6 caps -ln(1-n) near 13.8 — steep enough to dominate any richness reward, finite
// enough to stay a real number the experiment objective can subtract.
const barrierNMax = 1.0 - 1e-6

// FrontierBarrier returns the durability-frontier penalty for a measured branching ratio n ∈ [0,1).
//
// Shape: a logarithmic barrier  -ln(1 - n).  Properties (all tested):
//   - ~0 at small n        : -ln(1-0) = 0; -ln(1-0.05) ≈ 0.051   → cheap deep in the safe region.
//   - strictly increasing  : more excitation always costs more (monotone in n).
//   - → ∞ as n → 1         : the cost blows up at the subcritical cliff, so an optimizer maximizing
//     richness minus this barrier is pushed up to the frontier and no further (02 §5.4).
//
// Inputs are clamped: n < 0 → 0 (no negative penalty / no credit for being calm), n ≥ 1 → barrierNMax
// (a large finite penalty, never +Inf — so the term is always a usable number in the §5 objective). The
// classic interior-point barrier choice; -ln(1-n) is preferred over 1/(1-n)-1 because its gradient
// 1/(1-n) grows more gently away from the wall (gentler far from the frontier, still → ∞ at it), which
// makes the optimizer climb toward the frontier rather than being repelled from the whole region.
func FrontierBarrier(n float64) float64 {
	if n <= 0 {
		return 0
	}
	if n >= barrierNMax {
		n = barrierNMax
	}
	return -math.Log(1 - n)
}

// FrontierBarrier (method) reads the regulator's live measured n and returns its frontier penalty —
// the form the experiment objective uses (it has the *Regulator, not a bare n). Pure read; no mutation.
func (r *Regulator) FrontierBarrier() float64 { return FrontierBarrier(r.N()) }

// MaxOutstanding is the configured MAX_OUTSTANDING cap. It reads FocusCapacity — the regulator's
// schedulable budget (default 8) — so the async-action ceiling tracks the same schedulability bound the
// rest of the durability math uses (U = branches_live / focus_capacity), and a deployment that tightens
// the focus budget tightens the outstanding cap with it. With the default config this is 8, matching
// MaxOutstandingDefault and W_max.
func (r *Regulator) MaxOutstanding() int { return r.cfg.FocusCapacity }

// OutstandingAllowed reports whether one MORE async action may be fired given `current` already
// outstanding and a `cap` ceiling — the predicate the watched seam consults before Fire (04 §4). True
// iff current < cap, so firing keeps the outstanding count at or below the cap. A cap <= 0 is treated as
// MaxOutstandingDefault (a missing/uninitialised cap must not silently disable back-pressure).
func OutstandingAllowed(current, cap int) bool {
	if cap <= 0 {
		cap = MaxOutstandingDefault
	}
	return current < cap
}

// OutstandingAllowed (method) is the convenience form keyed to this regulator's MaxOutstanding() cap:
// the watched seam holds a *Regulator, asks `OutstandingAllowed(count)` before Fire, and gets a verdict
// against the schedulable budget. Pure read.
func (r *Regulator) OutstandingAllowed(current int) bool {
	return OutstandingAllowed(current, r.MaxOutstanding())
}
