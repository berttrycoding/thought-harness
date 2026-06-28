// gain.go estimates the plant gain g = -∂λ̂/∂θ from the regulator's own history ring, replacing the
// fixed GEst placeholder in the `0<K·g<2` durability check with a MEASURED quantity (X.6 #15 — until
// now the check was tautological: a compile-time constant that could never fail).
//
// What is estimated — and why it is the right quantity: the control law acts on the EMA-smoothed
// intensity (θ_{k+1} = θ_k + K·(λ̂_k − λ*)), so the loop's stability is governed by the SMOOTHED
// plant response ∂λ̂/∂θ — not the instantaneous physical plant. The lag-1 regression below therefore
// estimates exactly the gain the discrete loop sees.
//
// Identifiability is the hard part: λ̂ varies mostly for EXOGENOUS reasons (the workload), not because
// θ moved — a naive regression returns noise. So the estimate carries a CONFIDENCE GATE (enough moves,
// enough θ variance, enough correlation, the right sign) and falls back HONESTLY to the configured
// prior when the data does not identify the plant. The check is then: measured when measurable,
// assumed-prior when not — never a noisy estimate silently swinging a durability verdict.
package regulator

// gain estimation constants — deliberately conservative: a wrong "measured" gain is worse than the
// honest prior.
const (
	gainMinPairs  = 8    // need at least this many usable (Δθ, Δλ̂) pairs
	gainMinVar    = 1e-8 // minimum var(Δθ): below this θ never really moved (e.g. pinned at a bound)
	gainMinCorrSq = 0.25 // minimum r² between Δθ and the lagged Δλ̂: below this the plant is not identified
	gainClampLo   = 0.01 // numeric floor for a measured gain
	gainClampHi   = 10.0 // numeric ceiling — NOTE: with K=0.4 a measured g ≥ 5 makes K·g ≥ 2 and the
	// check FAILS, which is the entire point: the condition can now actually fire on a hot plant.
)

// saturation-detector constants. A SATURATED controller is one driven against a clamp and held
// there: the control law cannot move θ, so the loop is OPEN (no persistent excitation, the plant
// gain unidentifiable by construction). These thresholds make "pinned at a clamp" a MEASURED
// predicate over the θ history ring, not an assumption.
//
// The predicate is the TRAILING CONTIGUOUS PINNED RUN: how many of the most-recent θ snapshots have
// been railed at a SINGLE clamp without interruption. This is robust to a startup transient of ANY
// length (θ ratchets from its 0.3 init toward a clamp over the first few ticks; the transient simply
// is not part of the trailing run) and to the history length, unlike a fixed-fraction window. A
// controller flapping between the two clamps (bang-bang active control) has a trailing run of 1 — so
// it is correctly NOT saturated.
const (
	satMinRun = gainMinPairs // SATURATED when θ has been railed at ONE clamp for ≥ this many consecutive
	//                            most-recent snapshots (the same sample budget the gain estimator needs to
	//                            attempt identification — a railed stretch this long is a regime, not a blip).
	satClampEps = 1e-6 // |θ − clamp| ≤ this counts as "at the clamp"
	satMaxVar   = 1e-6 // var(θ) ≤ this counts θ as flat/inactive (the LoopOpen "inactive" gate)
	satWindow   = 32   // trailing-window length for the inactive (flat-θ) test in LoopOpen
)

// sustained-identifiability constants (HOLE 2 / C0b honesty). The honest unidentified-active FAIL is
// reserved for a loop that SUSTAINS identifiable excitation (θ keeps moving) yet the plant is still not
// identified. A short, converging-and-terminating reactive transient (θ ratchets once toward its
// settle point then stops moving as the episode ends) NEVER sustains that excitation — it has only a
// handful of θ-moving sample-pairs — so the loop-gain (an asymptotic property) is VACUOUS on it, not a
// failure. These thresholds make "sustained identifiable excitation" a MEASURED predicate.
const (
	moveEps = 5e-4 // |Δθ| > this counts as a real θ MOVE (a settled/pinned tick has |Δθ|≈0). Comfortably
	//                  above the round3 wire quantum (1e-3 is the snap precision, but a genuine ratchet
	//                  step is ≥1e-2) yet well below a real control step — so it admits real moves and
	//                  rejects settle-noise.
	//
	// sustainHorizon is the number of θ-MOVING sample-pairs a loop must accumulate before its
	// loop-gain may be declared an honest FAIL. The argument is principled, not curve-fit: 0<K·g<2 is
	// an ASYMPTOTIC (steady-state) stability property. gainMinPairs (=8) is the minimum budget to
	// ATTEMPT identification; but to assert "sustained-active yet unidentified" (the honest FAIL) the
	// loop must have PERSISTED in a moving regime long enough to BE a steady state rather than a brief
	// transient. A short reactive episode that opens, ratchets θ once, and quiesces (the S3/S9 ~15–30-
	// tick runs: 12–16 moving pairs) is a TRANSIENT — its asymptotic loop-gain is undefined, so it is
	// vacuous (insufficient-loop), NOT unstable. A genuinely sustained loop (the anti-tautology case
	// runs ~118 moving pairs) clears this horizon with wide margin and still reaches the honest FAIL.
	// 4×gainMinPairs = 32 sits cleanly between the two populations (16 < 32 < 118).
	sustainHorizon = 4 * gainMinPairs
)

// MovingPairs counts the (Δθ, Δλ̂) lag-1 sample-pairs over the history ring in which θ ACTUALLY MOVED
// (|Δθ| > moveEps). It is the measure of SUSTAINED identifiable excitation: a controller that keeps
// pushing θ around generates many moving pairs (a sustained loop); one that ratchets once and settles
// — or pins — generates few. This is what distinguishes the honest unidentified-active FAIL (a
// sustained, moving, unidentified loop) from a vacuous short/converging transient (HOLE 2). Pure read.
func (r *Regulator) MovingPairs() int {
	snaps := r.history.items()
	thetas := make([]float64, 0, len(snaps))
	for _, s := range snaps {
		if th, ok := s["theta"].(float64); ok {
			thetas = append(thetas, th)
		}
	}
	moving := 0
	for k := 1; k < len(thetas)-1; k++ { // same lag-1 indexing window the gain estimator uses
		if abs(thetas[k]-thetas[k-1]) > moveEps {
			moving++
		}
	}
	return moving
}

// Saturated reports whether the controller is in open-loop saturation: θ has been railed at a SINGLE
// clamp (ThetaMin or ThetaMax) for at least satMinRun consecutive most-recent snapshots. runLen is the
// measured length of that trailing pinned run and thetaVar the variance of θ over it (≈0 by
// construction when saturated; returned for the regime report). Pure read — no state is mutated.
func (r *Regulator) Saturated() (saturated bool, runLen int, thetaVar float64) {
	saturated, runLen, thetaVar, _ = r.saturatedAt()
	return saturated, runLen, thetaVar
}

// SaturatedAt reports the saturation verdict PLUS which clamp θ is railed at: "min" (railed at
// ThetaMin), "max" (railed at ThetaMax), or "" (not at a clamp). The clamp identity is load-bearing
// for the regime verdict (HOLE 1 / C0a soundness): a ThetaMin rail is BENIGN open-loop (little to
// control — the awake steady state, λ̄=μ/(1−n) finite), whereas a ThetaMax rail means the controller
// is at MAX suppression and STILL cannot bring λ̂ down — a control-loss signal that must be guarded by
// an intensity bound, not waved through as "saturated-bounded". Pure read.
func (r *Regulator) SaturatedAt() (saturated bool, clamp string) {
	sat, _, _, c := r.saturatedAt()
	return sat, c
}

// saturatedAt is the shared implementation: the trailing-contiguous-pinned-run detector, returning the
// run length, its θ-variance, and the clamp identity ("min"/"max"/"").
func (r *Regulator) saturatedAt() (saturated bool, runLen int, thetaVar float64, clampID string) {
	snaps := r.history.items()
	thetas := make([]float64, 0, len(snaps))
	for _, s := range snaps {
		if th, ok := s["theta"].(float64); ok {
			thetas = append(thetas, th)
		}
	}
	if len(thetas) == 0 {
		return false, 0, 0, ""
	}
	lo, hi := r.cfg.ThetaMin, r.cfg.ThetaMax
	// Walk backwards from the newest snapshot, counting the contiguous run at whichever clamp the
	// newest θ sits at (a run must be at a SINGLE clamp — flapping breaks the run).
	last := thetas[len(thetas)-1]
	var clamp float64
	switch {
	case abs(last-lo) <= satClampEps:
		clamp, clampID = lo, "min"
	case abs(last-hi) <= satClampEps:
		clamp, clampID = hi, "max"
	default:
		return false, 0, 0, "" // not currently at a clamp → not saturated
	}
	runLen = 0
	for i := len(thetas) - 1; i >= 0; i-- {
		if abs(thetas[i]-clamp) <= satClampEps {
			runLen++
		} else {
			break
		}
	}
	// variance over the pinned run (≈0; reported for the regime line).
	run := thetas[len(thetas)-runLen:]
	var sum float64
	for _, th := range run {
		sum += th
	}
	mean := sum / float64(len(run))
	var ss float64
	for _, th := range run {
		d := th - mean
		ss += d * d
	}
	thetaVar = ss / float64(len(run))
	if thetaVar < 0 {
		thetaVar = 0
	}
	saturated = runLen >= satMinRun
	if !saturated {
		clampID = "" // a sub-satMinRun run is not a saturation regime; do not report its clamp
	}
	return saturated, runLen, thetaVar, clampID
}

// LoopOpen reports whether the control loop is effectively OPEN — i.e. the loop-gain condition
// 0<K·g<2 is VACUOUS because there is no sustained closed loop to be unstable. Three measured
// sub-reasons, in priority order:
//
//	"saturated"        — the controller is pinned at a clamp (Saturated() above): the control law is
//	                     railed, so it cannot close the loop. The awake steady-state case.
//	"inactive"         — θ exhibits no persistent excitation (windowed var(θ) ≈ 0): the loop is running
//	                     but flat, so the gain is unidentifiable by construction. Vacuous.
//	"insufficient-loop"— the loop never established a SUSTAINED identifiable regime. Two measured
//	                     sub-cases (HOLE 2): (a) too few regulator ticks to even attempt identification
//	                     (history shorter than the gain estimator's minimum); or (b) too few θ-MOVING
//	                     sample-pairs (MovingPairs < moveMinPairs) — a short, converging-and-terminating
//	                     transient that ratchets θ once and settles. In both, loop-gain (an asymptotic
//	                     property) is VACUOUS, NOT a failure.
//
// open is false ONLY when there is a SUSTAINED loop (enough history AND enough θ-moving pairs) AND θ is
// genuinely MOVING (real windowed variance) yet the plant is still not identified — that is the honest
// unidentified-active failure, the case the old prior-fallback silently hid. reason is "" then. Pure
// read.
func (r *Regulator) LoopOpen() (open bool, reason string) {
	if sat, _, _ := r.Saturated(); sat {
		return true, "saturated"
	}
	snaps := r.history.items()
	thetas := make([]float64, 0, len(snaps))
	for _, s := range snaps {
		if th, ok := s["theta"].(float64); ok {
			thetas = append(thetas, th)
		}
	}
	// Insufficient sustained loop (length): below the gain estimator's identification minimum
	// (gainMinPairs+2 usable snapshots) there is no asymptotic regime to be unstable. (Checked before the
	// variance test so a short MOVING transient is correctly vacuous, not an honest fail.)
	if len(thetas) < gainMinPairs+2 {
		return true, "insufficient-loop"
	}
	// Insufficient sustained loop (horizon): even with enough history, a loop that has not PERSISTED in
	// a θ-moving regime for the sustain horizon — a short converging-and-terminating reactive transient
	// that opens, ratchets θ once toward its settle point, and quiesces — is a TRANSIENT, not a steady
	// state. Loop-gain is an asymptotic property, so it is VACUOUS on a transient (insufficient-loop),
	// NOT an honest fail. This is the C0b honesty grace (HOLE 2): the grace is NARROW — a genuinely
	// SUSTAINED moving loop (the anti-tautology case, ~118 moving pairs) clears the horizon with wide
	// margin and is NOT caught here, so it still reaches the honest unidentified-active fail.
	if r.MovingPairs() < sustainHorizon {
		return true, "insufficient-loop"
	}
	// Inactive: θ has not exhibited persistent excitation over the current (trailing-window) regime. We
	// reuse the saturation variance gate as the "static θ" test, dropping the single-clamp requirement
	// (θ may rest anywhere when flat). A genuinely moving θ is NOT inactive, so it gets no vacuity pass.
	if len(thetas) > satWindow {
		thetas = thetas[len(thetas)-satWindow:]
	}
	var sum float64
	for _, th := range thetas {
		sum += th
	}
	mean := sum / float64(len(thetas))
	var ss float64
	for _, th := range thetas {
		d := th - mean
		ss += d * d
	}
	if ss/float64(len(thetas)) <= satMaxVar {
		return true, "inactive"
	}
	return false, ""
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// GainEstimate returns the plant gain used by the 0<K·g<2 check and whether it was MEASURED from the
// history ring (true) or is the configured prior (false). Pure read — no state is mutated.
func (r *Regulator) GainEstimate() (g float64, measured bool) {
	snaps := r.history.items()
	if len(snaps) < gainMinPairs+2 {
		return r.cfg.GEst, false
	}
	// Extract the (θ, λ̂) series oldest→newest. Snap values are the round3'd floats pushed by Update.
	thetas := make([]float64, 0, len(snaps))
	lams := make([]float64, 0, len(snaps))
	for _, s := range snaps {
		th, ok1 := s["theta"].(float64)
		lh, ok2 := s["lam_hat"].(float64)
		if !ok1 || !ok2 {
			continue
		}
		thetas = append(thetas, th)
		lams = append(lams, lh)
	}
	if len(thetas) < gainMinPairs+2 {
		return r.cfg.GEst, false
	}
	// Lag-1 pairs: a θ move at k (θ_k − θ_{k-1}) shows up in the smoothed intensity one tick later
	// (λ̂_{k+1} − λ̂_k). Snap k records θ AFTER the control step and λ̂ AFTER folding tick k's events,
	// so tick k's events were generated under θ_{k-1} — hence the lag.
	var dth, dlm []float64
	for k := 1; k < len(thetas)-1; k++ {
		dt := thetas[k] - thetas[k-1]
		dl := lams[k+1] - lams[k]
		dth = append(dth, dt)
		dlm = append(dlm, dl)
	}
	if len(dth) < gainMinPairs {
		return r.cfg.GEst, false
	}
	// Least squares: slope = cov(Δθ, Δλ̂)/var(Δθ); confidence = r² = cov²/(var·var).
	n := float64(len(dth))
	var sumT, sumL float64
	for i := range dth {
		sumT += dth[i]
		sumL += dlm[i]
	}
	meanT, meanL := sumT/n, sumL/n
	var covTL, varT, varL float64
	for i := range dth {
		covTL += (dth[i] - meanT) * (dlm[i] - meanL)
		varT += (dth[i] - meanT) * (dth[i] - meanT)
		varL += (dlm[i] - meanL) * (dlm[i] - meanL)
	}
	if varT/n < gainMinVar || varL == 0 {
		return r.cfg.GEst, false // θ never moved (pinned) or λ̂ flat — not identifiable
	}
	corrSq := (covTL * covTL) / (varT * varL)
	if corrSq < gainMinCorrSq {
		return r.cfg.GEst, false // exogenous noise dominates — the plant is not identified
	}
	slope := covTL / varT
	if slope >= 0 {
		// A non-negative slope (raising θ raises intensity) contradicts the plant model — treat as
		// unidentified rather than report a meaningless negative gain.
		return r.cfg.GEst, false
	}
	g = -slope
	if g < gainClampLo {
		g = gainClampLo
	}
	if g > gainClampHi {
		g = gainClampHi
	}
	return g, true
}
