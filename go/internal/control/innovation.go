package control

// innovation.go is the SLAM self-state measurement update, made explicit — Pattern A (pure CONTROL,
// NO model call ever). It turns the currently-implicit "reality refuted the guess -> static -0.45
// penalty" into a scalar Kalman update with an explicit residual (innovation) and a Mahalanobis
// data-association gate. The math is closed-form, deterministic, RNG-free and clock-free, so it lives
// in this Tier-1 leaf (never imports backends, never references the test double).
//
// Design: docs/internal/notes/2026-06-20-slam-M1-build-spec.md §3.1 + the parent
// docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3.3 (the measurement update P1/P2). The
// STATEFUL side (the per-belief variance side-table + FEJ anchor, ticked across the run) lives in
// internal/estimate; this file holds only the pure step it calls.
//
// THE ONE LOAD-BEARING INVARIANT (§0): belief variance P shrinks ONLY through a grounded observation
// here, NEVER through a self-restatement. A confidently-stated-but-ungrounded belief keeps HIGH P, so
// reality corrects it HARD (large gain); a grounded belief has LOW P, so it resists a spurious later
// contradiction. That is "stop being confidently wrong" — the calibration-not-caution win. This file
// is the ONLY var-reducer; the estimator's Note() (self-restatement) must never call it.

// Residual is the explicit predicted-vs-observed measurement record — the math structure that
// replaces the implicit, model-mediated comparison plus the static -0.45 penalty. Every field is a
// term of the scalar Kalman update so the whole decision is legible on the event bus.
type Residual struct {
	PriorMean float64 // belief confidence/stance before the observation, in [-1,1] (sign = stance)
	PriorVar  float64 // belief variance P before (high = uncertain / self-derived)
	Obs       float64 // observation outcome: +1 grounded-confirms, -1 grounded-refutes
	ObsPrec   float64 // R^-1 from the trust tier (FirsthandValidated high ... Testimony low)
	Innov     float64 // nu = Obs - PriorMean (the innovation / residual)
	InnovVar  float64 // S = PriorVar + 1/ObsPrec (the innovation covariance)
	Gain      float64 // W = PriorVar / S (the Kalman gain)
	PostMean  float64 // PriorMean + W*nu (the graded correction; replaces the static penalty)
	PostVar   float64 // (1 - W) * PriorVar (shrinks ONLY here — the §0 invariant)
	Gated     bool    // true if Mahalanobis-rejected (data-association failed; no update applied)
}

// Innovate runs the scalar Kalman update for one grounded observation against a prior belief. Pure;
// no RNG, no clock; a deterministic function of its arguments only.
//
//	nu  = obs - priorMean                 # innovation (predicted vs observed)
//	S   = priorVar + 1/obsPrec            # innovation covariance (prior + measurement noise R)
//	d2  = nu*nu / S                        # squared Mahalanobis distance (data-association test)
//	if d2 > chi2Gate: GATED -> return prior unchanged (the JCBB-lite of M1)
//	W   = priorVar / S                     # Kalman gain
//	post = priorMean + W*nu                # graded correction
//	Post = (1 - W) * priorVar              # covariance shrinks (more certain) — ONLY here
//
// The Mahalanobis gate generalises the ad-hoc structural rejects (AssertsUngroundedObservation /
// DeniesAvailableReality): a refuting observation whose innovation is too large for the prior+noise to
// plausibly explain is an association failure (this obs is probably NOT about this belief), so it is
// NOT folded in — don't corrupt the map with a mismatched measurement (full JCBB is M6).
func Innovate(priorMean, priorVar, obs, obsPrec, chi2Gate float64) Residual {
	// Guard the measurement noise: a non-positive precision means "no information" (R -> inf), which
	// makes the gain 0 (the observation cannot move the belief). Clamp to a tiny floor so 1/obsPrec is
	// finite and the gain degrades gracefully rather than producing a NaN.
	if obsPrec <= 0 {
		obsPrec = 1e-9
	}
	// Guard a non-positive prior variance: a belief with zero/negative P is already maximally certain,
	// so reality cannot move it (gain 0). Clamp to zero to keep S strictly positive (R > 0).
	if priorVar < 0 {
		priorVar = 0
	}
	innov := obs - priorMean
	innovVar := priorVar + 1.0/obsPrec // S = P + R; strictly positive (R > 0)
	r := Residual{
		PriorMean: priorMean,
		PriorVar:  priorVar,
		Obs:       obs,
		ObsPrec:   obsPrec,
		Innov:     innov,
		InnovVar:  innovVar,
	}
	// Mahalanobis data-association gate: nu^2 / S < chi2Gate. A non-positive gate disables the test
	// (everything associates) so a caller can opt out by passing 0.
	if chi2Gate > 0 && (innov*innov)/innovVar > chi2Gate {
		r.Gated = true
		r.Gain = 0
		r.PostMean = priorMean // prior unchanged — the mismatched obs is not folded in
		r.PostVar = priorVar   // and the belief gets NO more certain (it was not confirmed)
		return r
	}
	gain := priorVar / innovVar // W = P / S in [0,1)
	r.Gain = gain
	r.PostMean = priorMean + gain*innov
	r.PostVar = (1.0 - gain) * priorVar // P shrinks toward 0 only on a grounded (associated) obs
	return r
}

// tierPrecisionTable maps a grounding trust-tier ORDINAL to its measurement precision R^-1 (the
// information weight of an observation from that source). It is held HERE as a []float64 indexed by
// tier ordinal — NOT keyed on grounding.TrustTier — so this leaf need not import internal/grounding
// (leaf-purity: control imports only types + stdlib). The caller maps its TrustTier to the ordinal.
//
// The ordinals MUST match internal/grounding.TrustTier's iota order (Testimony=0 .. FirsthandValidated
// =5); a mismatch is caught by the estimate package's TestTierPrecisionMatchesGroundingTiers gate
// (which DOES import grounding) so the two cannot silently drift apart. Monotone increasing in trust:
// a firsthand-validated observation carries ~36x the information of bare testimony, so it corrects the
// belief hard and shrinks its variance a lot; testimony barely moves it.
//
//	0 Testimony            -> 0.5   (heard, unverified; large R, low information)
//	1 Web                  -> 1.0
//	2 AuthoritativeRef     -> 2.0
//	3 FirsthandObservation -> 4.0   (we observed it once)
//	4 Deterministic        -> 8.0   (computed / proven)
//	5 FirsthandValidated   -> 18.0  (we tested it ourselves and it held — the gold tier; small R)
var tierPrecisionTable = []float64{0.5, 1.0, 2.0, 4.0, 8.0, 18.0}

// TierPrecision returns the measurement precision R^-1 for a grounding trust-tier ordinal. Monotone in
// trust; the single wire from grounding.TrustTier into the innovation math. An out-of-range ordinal
// clamps to the nearest end (a defensive floor, not an expected path — the gate test pins the range).
func TierPrecision(tierOrdinal int) float64 {
	if tierOrdinal < 0 {
		tierOrdinal = 0
	}
	if tierOrdinal >= len(tierPrecisionTable) {
		tierOrdinal = len(tierPrecisionTable) - 1
	}
	return tierPrecisionTable[tierOrdinal]
}

// TierCount is the number of trust tiers TierPrecision is defined over — exported so the estimate
// package's gate test can assert the table covers every grounding.TrustTier.
func TierCount() int { return len(tierPrecisionTable) }
