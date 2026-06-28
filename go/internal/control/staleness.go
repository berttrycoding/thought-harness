package control

// staleness.go is the SLAM M4 FRESHNESS / STALENESS-DECAY math, made explicit — Pattern A (pure
// CONTROL, NO model call ever). M1 (innovation.go) made the per-belief measurement update explicit (the
// ONLY var-REDUCER); M2 (covariance.go) added the off-diagonal correlated loss-of-certainty. M4 adds the
// PROCESS NOISE the dynamic-map case demands: a belief grounded long ago, left un-refreshed, must GROW
// back toward uncertain — because the world it described may have MOVED since it was last observed.
//
// Design: docs/internal/notes/2026-06-20-slam-self-state-estimation.md §4 (P4) "Non-stationary world =>
// mandatory decay" + §3b.2 (the Estimate envelope's LastObs/Dynamics fields, "world/time MUST drift and
// decay toward stale, re-observe; identity is near-stationary") + §6 M4. The math is closed-form,
// deterministic, RNG-free and clock-free (a function of the SEEDED-tick AGE, never the wall clock), so it
// lives in this Tier-1 leaf (never imports backends, never references the test double). The STATEFUL side
// (the per-belief last-observation tick + the per-tick decay sweep) lives in internal/estimate; this file
// holds only the pure step it calls.
//
// WHY Q > 0 IS MANDATORY HERE (the theorem, design §4 P4): Dissanayake/Thm-1 (monotone uncertainty
// DECREASE) holds ONLY for STATIONARY landmarks (Q_map = 0). For a world/web/time belief the map MOVES,
// so Q > 0 — the belief covariance MUST grow with staleness, forcing re-observation. Without it the
// estimator stays falsely confident in a fact that has since changed (a stale answer presented as
// fresh) — the "confidently wrong about yesterday's world" failure mode M4 designs out.
//
// CONSISTENCY (stays inside the M1 §0 / M5 invariant): staleness decay only ever RAISES a variance (loses
// certainty), never lowers it. Becoming LESS certain can never be spurious information — you cannot
// fabricate certainty by admitting that a fact has gone stale — so M4 is provably safe against the
// Huang-2010 EKF-inconsistency the M5 witness guards (the M5 infoGain accounting returns 0 for any
// variance GROWTH). Only a direct grounded Observe() (M1) may ever shrink a variance.

// StalenessInflation is the variance a once-grounded belief decays TO after `age` un-refreshed ticks. It
// grows the belief's variance back toward the prior (uncertain) ceiling as it ages, modelling the process
// noise of a non-stationary world — the longer since a fact was last observed, the less it can be trusted.
//
//	priorVar   the belief's current variance P (low = freshly grounded; high = stale / self-derived)
//	age        ticks since the belief was last GROUNDED by a real observation (LastObs); age <= 0 => fresh
//	q          the per-tick process-noise rate in [0,1] (slam.staleness_q): the FRACTION of the remaining
//	           gap to the ceiling the belief loses per un-refreshed tick (0 = stationary, no decay)
//	ceiling    the saturating upper bound — a stale belief is at most as uncertain as a never-grounded one
//	           (the estimator's PriorVar0), NEVER more, so decay can never blow the variance up unboundedly
//
//	decayed = priorVar + (1 - (1-q)^age) * (ceiling - priorVar)   # geometric approach to the ceiling
//
// The factor (1 - (1-q)^age) is the cumulative fraction of the gap closed after `age` ticks: it is 0 at
// age 0 (fresh — no decay), monotone increasing in both age and q, and saturates to 1 as age -> inf (a
// belief left un-refreshed forever decays to exactly the ceiling, never past it). This is the discrete
// geometric form of a continuous Q>0 process-noise integration, kept closed-form so a single call covers
// an arbitrary age gap (no per-tick loop) and stays deterministic. Returns priorVar unchanged whenever
// there is nothing to do (q<=0 stationary, age<=0 fresh, or the belief is already at/above the ceiling),
// so a staleness-OFF run — or a belief just grounded this tick — is byte-identical.
func StalenessInflation(priorVar, age, q, ceiling float64) float64 {
	if q <= 0 || age <= 0 || priorVar >= ceiling {
		return priorVar // stationary, fresh, or already maximally-uncertain -> no decay
	}
	if q > 1 {
		q = 1
	}
	// fraction = 1 - (1-q)^age : the share of the (ceiling - priorVar) gap closed after `age` ticks.
	fraction := 1.0 - powPos(1.0-q, age)
	decayed := priorVar + fraction*(ceiling-priorVar)
	if decayed > ceiling {
		decayed = ceiling // numerical guard: never overshoot the ceiling
	}
	return decayed
}

// powPos computes base^exp for base in [0,1] and exp >= 0, via base^exp = e^(exp*ln base). It is kept
// local so this leaf depends only on stdlib arithmetic (control imports only types + stdlib; the existing
// expPos/lnPos helpers keep it math-import-free). base 0 -> 0 for exp>0 (a q=1 belief decays fully in one
// tick); base 1 -> 1 (q=0, handled by the caller's guard, but defended here too).
func powPos(base, exp float64) float64 {
	if base <= 0 {
		return 0
	}
	if base >= 1 || exp <= 0 {
		return 1
	}
	return expPos2(exp * lnPos(base))
}

// lnPos is the natural log for base in (0,1], via the atanh series ln(x) = 2*atanh((x-1)/(x+1)) which
// converges fast for x near 1 (the (1-q) bases this leaf uses are close to 1). Defined locally so the
// control leaf needs no math import for the M4 decay.
func lnPos(x float64) float64 {
	if x <= 0 {
		return 0 // guarded by the caller (base<=0 short-circuits in powPos); defensive
	}
	if x == 1 {
		return 0
	}
	y := (x - 1) / (x + 1)
	y2 := y * y
	term := y
	sum := y
	for i := 1; i < 64; i++ {
		term *= y2
		add := term / float64(2*i+1)
		sum += add
		if add < 1e-16 && add > -1e-16 {
			break
		}
	}
	return 2 * sum
}

// expPos2 is e^x for x <= 0 (the M4 caller always passes a non-positive exponent: exp*ln(base) with
// 0<base<1 => ln(base)<0 => the product <= 0). Computed via the standard series, terminated when a term
// stops contributing. Named expPos2 to avoid colliding with covariance.go's expPos (which is for x>=0);
// for a non-positive argument the alternating series is numerically stable for the small magnitudes here.
func expPos2(x float64) float64 {
	if x == 0 {
		return 1
	}
	sum := 1.0
	term := 1.0
	for i := 1; i < 128; i++ {
		term *= x / float64(i)
		sum += term
		if term < 1e-18 && term > -1e-18 {
			break
		}
	}
	if sum < 0 {
		return 0 // numerical floor: e^x is never negative
	}
	return sum
}
