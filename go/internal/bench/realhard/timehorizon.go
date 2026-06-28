package realhard

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// timehorizon.go — the METR-STYLE TIME-HORIZON calculator (design doc §7.4 + §5).
//
// WHY IT EXISTS. The single most harness-relevant AUTONOMY-axis metric is METR's
// time-horizon: the human-task LENGTH (in minutes) at which the agent succeeds 50%
// of the time. It maps 1:1 onto the L2->L3 autonomy ladder and is the right "are we
// levelling up?" curve (the frontier doubles ~every 7 months; the harness's own
// curve is what this computes over OUR banks — we adopt the METHOD, not METR's task
// set, per the doc).
//
// THE METHOD (METR, arXiv 2503.14499). Each task carries an estimated HUMAN-MINUTES
// (how long a skilled human takes) and a measured per-task SOLVE-RATE p (from the
// Bernoulli per-task p̂ — the SAME quantity pass^k and the A/B arms use). Fit a
// LOGISTIC in log-minutes:
//
//	logit(p) = beta0 + beta1 * log2(minutes)          (beta1 < 0: longer ⇒ harder)
//
// The 50%-reliable horizon is where p = 0.5, i.e. logit(p) = 0:
//
//	H50 = 2 ^ ( -beta0 / beta1 )   minutes
//
// (log2 so the slope reads directly in "doublings of task length per logit unit",
// matching METR's doubling framing.) A general T%-reliable horizon solves
// logit(T) = beta0 + beta1*log2(H) ⇒ H_T = 2 ^ ((logit(T) - beta0)/beta1).
//
// THE FIT. Weighted least squares on the linearized logit (each task's logit(p̂)
// regressed on log2(minutes), weighted by its binomial information w = K*p(1-p) so a
// well-pinned mid-range task counts more than a saturated/under-sampled one). Tasks
// pinned at p=0 or p=1 have no finite logit, so their p̂ is CLAMPED to [eps, 1-eps]
// (the standard logistic-fit boundary fix) and they carry near-zero weight — they
// contribute the direction (a saturated easy task pulls H50 up; an all-fail hard
// task pulls it down) without an infinite residual. This is a CLOSED-FORM WLS, not
// an iterative MLE: deterministic, no RNG, no model — pure arithmetic over the
// per-task (minutes, solve-rate) pairs (CLAUDE.md determinism).
//
// HONESTY. WLS-on-linearized-logit is the standard cheap logistic fit; it is NOT a
// full IRLS/MLE and is reported as such. The fit is DEGENERATE (no horizon) when
// there is <2 distinct-minutes informative tasks or the slope does not have the
// expected sign (beta1 >= 0 ⇒ "longer tasks are not harder" ⇒ the horizon is not
// identifiable from this data) — reported as an honest NA, never a fabricated number.

// THTask is one task's input to the time-horizon fit: its estimated human-minutes and
// its measured per-task solve-rate (PHat, K). It is the METR (minutes, success) pair.
type THTask struct {
	TaskID     string
	Capability Capability
	HumanMin   float64 // estimated skilled-human task length, minutes (>0)
	PHat       float64 // measured per-task solve-rate in [0,1]
	K          int     // replays behind PHat (the binomial weight; >=1)
}

// THResult is the time-horizon fit.
type THResult struct {
	// Fitted reports whether a horizon was identified (>=2 distinct-minutes
	// informative tasks AND a negative slope). When false the horizon fields are 0
	// and Reason explains the NA.
	Fitted bool
	Reason string

	// Logistic coefficients in log2(minutes): logit(p) = Beta0 + Beta1*log2(min).
	Beta0 float64
	Beta1 float64

	// Horizon50 is the 50%-reliable task length (minutes): H50 = 2^(-Beta0/Beta1).
	Horizon50 float64
	// Horizon80 / Horizon20 are the 80% and 20% reliable lengths (the same fit at
	// other reliability levels) — the band around the median horizon.
	Horizon80 float64
	Horizon20 float64

	// Tasks is the (sorted) per-task input echoed for the report.
	Tasks []THTask
	// NEff is the number of informative tasks (distinct-minutes, weight>0) the fit used.
	NEff int
}

// thLogitClampEps is the boundary clamp for a saturated p̂ (0 or 1) so its logit is
// finite (the standard logistic-fit boundary fix). Far enough from 0.5 that a
// saturated task still pulls the fit in the right direction.
const thLogitClampEps = 1e-3

// thSlopeEps is the minimum |Beta1| (logit per log2-minute) for the slope to count as a
// resolvable difficulty gradient. A fitted |slope| below this is treated as numerically
// zero (the all-saturated degenerate case) and reported UNIDENTIFIED — it is FAR below
// any real gradient (the instrument set fits ~2.3) yet comfortably above the ~1e-15 FP
// noise an all-equal-logit dataset produces.
const thSlopeEps = 1e-9

// logit is log(p/(1-p)) with p clamped to [eps, 1-eps] so the boundary is finite.
func logit(p float64) float64 {
	if p < thLogitClampEps {
		p = thLogitClampEps
	}
	if p > 1-thLogitClampEps {
		p = 1 - thLogitClampEps
	}
	return math.Log(p / (1 - p))
}

// invLogit is the logistic 1/(1+e^-x) (the inverse of logit; used to map a target
// reliability T to its logit for the H_T solve).
func invLogit(x float64) float64 {
	return 1 / (1 + math.Exp(-x))
}

// TimeHorizon fits the logistic success-vs-log2(minutes) model and returns the
// 50%-reliable horizon (METR-style). Pure, deterministic: weighted least squares on
// the linearized logit. tasks with HumanMin<=0 are dropped (no length to place them
// on the x-axis); tasks with K<=0 are dropped (no measurement).
func TimeHorizon(tasks []THTask) THResult {
	res := THResult{}
	// keep only placeable, measured tasks; clamp + weight.
	type pt struct {
		x float64 // log2(minutes)
		y float64 // logit(p̂) (clamped)
		w float64 // binomial information weight K*p(1-p), floored so saturated tasks count a little
	}
	var pts []pt
	var kept []THTask
	for _, t := range tasks {
		if t.HumanMin <= 0 || t.K <= 0 {
			continue
		}
		p := t.PHat
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		w := float64(t.K) * p * (1 - p)
		// floor the weight for a saturated task so it still nudges the slope/direction
		// (METR keeps saturated tasks in the fit; they are just near-certain points).
		if w < 1e-6 {
			w = 1e-6
		}
		pts = append(pts, pt{x: math.Log2(t.HumanMin), y: logit(p), w: w})
		kept = append(kept, t)
	}
	res.Tasks = append(res.Tasks, kept...)
	sort.SliceStable(res.Tasks, func(i, j int) bool { return res.Tasks[i].HumanMin < res.Tasks[j].HumanMin })

	// need >=2 tasks at DISTINCT minutes to identify a slope.
	distinct := map[float64]bool{}
	nInfo := 0
	for _, p := range pts {
		distinct[p.x] = true
		nInfo++
	}
	res.NEff = nInfo
	if nInfo < 2 || len(distinct) < 2 {
		res.Reason = "DEGENERATE: need >=2 informative tasks at >=2 distinct task-lengths to fit a horizon"
		return res
	}

	// weighted least squares: minimize Σ w (y - (b0 + b1 x))^2.
	var sw, swx, swy, swxx, swxy float64
	for _, p := range pts {
		sw += p.w
		swx += p.w * p.x
		swy += p.w * p.y
		swxx += p.w * p.x * p.x
		swxy += p.w * p.x * p.y
	}
	denom := sw*swxx - swx*swx
	if denom == 0 {
		res.Reason = "DEGENERATE: zero variance in task-length (cannot identify a slope)"
		return res
	}
	res.Beta1 = (sw*swxy - swx*swy) / denom
	res.Beta0 = (swy - res.Beta1*swx) / sw

	// the slope must be RESOLVABLY NEGATIVE (longer ⇒ harder) for the horizon to be
	// meaningful. A slope that is non-negative OR numerically indistinguishable from zero
	// means task-length does not predict difficulty in this data — the horizon is not
	// identifiable. The |Beta1| >= thSlopeEps floor is the load-bearing guard for the
	// ALL-SATURATED case (every task p=0 or every task p=1): the clamped logits are then
	// all equal, the true slope is exactly 0, and floating-point noise leaves a tiny
	// b1≈±1e-15. Without this floor a b1 that happened to land at -1e-15 would slip past
	// `>= 0`, pass as "fitted", and report a meaningless H50 = 2^(-b0/b1) ≈ 0s (a
	// degenerate non-fit masquerading as a horizon). Both signs of that FP noise are the
	// SAME degenerate data — report the honest UNIDENTIFIED NA for either.
	if res.Beta1 >= -thSlopeEps {
		res.Reason = fmt.Sprintf("UNIDENTIFIED: slope beta1=%.4g is not resolvably negative (no difficulty gradient — e.g. every task saturated at one rate); the time-horizon is not identifiable from this data", res.Beta1)
		return res
	}

	res.Fitted = true
	res.Reason = "fitted (weighted least squares on the linearized logit; not a full MLE)"
	// H_T = 2 ^ ((logit(T) - b0)/b1).
	horizonAt := func(T float64) float64 {
		return math.Pow(2, (logit(T)-res.Beta0)/res.Beta1)
	}
	res.Horizon50 = horizonAt(0.5) // logit(0.5)=0 ⇒ 2^(-b0/b1)
	res.Horizon80 = horizonAt(0.8)
	res.Horizon20 = horizonAt(0.2)
	_ = invLogit // exported helper for callers that want the fitted p at a length
	return res
}

// FittedProbabilityAt returns the model's predicted solve-rate at a given task length
// (minutes) under the fitted logistic — the curve's value, for a caller that wants to
// place a new task. Returns (0,false) when the fit is degenerate.
func (r THResult) FittedProbabilityAt(minutes float64) (float64, bool) {
	if !r.Fitted || minutes <= 0 {
		return 0, false
	}
	return invLogit(r.Beta0 + r.Beta1*math.Log2(minutes)), true
}

// Render produces the plain-text time-horizon report (no emoji, box-drawing only).
func (r THResult) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("METR-STYLE TIME-HORIZON (50%%-reliable task length; autonomy axis)\n")
	w("%s\n", strings.Repeat("=", 72))
	w("method: logistic logit(p) = b0 + b1*log2(minutes), weighted LS on the linearized logit\n")
	w("tasks(informative): %d\n", r.NEff)
	if !r.Fitted {
		w("VERDICT: NA — %s\n", r.Reason)
		return b.String()
	}
	w("fit: logit(p) = %+.4f %+.4f*log2(min)   (b1<0 = longer is harder)\n", r.Beta0, r.Beta1)
	w("%s\n", strings.Repeat("-", 72))
	w("  TIME-HORIZON (50%%-reliable) : %s\n", fmtMinutes(r.Horizon50))
	w("  80%%-reliable length         : %s\n", fmtMinutes(r.Horizon80))
	w("  20%%-reliable length         : %s\n", fmtMinutes(r.Horizon20))
	w("%s\n", strings.Repeat("-", 72))
	w("PER-TASK (length -> measured solve-rate -> model-predicted)\n")
	for _, t := range r.Tasks {
		pred, _ := r.FittedProbabilityAt(t.HumanMin)
		w("  %-22s  %-9s  p=%.3f (K=%d)  model=%.3f\n",
			t.TaskID, fmtMinutes(t.HumanMin), t.PHat, t.K, pred)
	}
	return b.String()
}

// fmtMinutes renders a minutes value in a human-legible unit (METR reports in
// seconds/minutes/hours). Pure formatting; no clock.
func fmtMinutes(m float64) string {
	switch {
	case m < 1:
		return fmt.Sprintf("%.0fs", m*60)
	case m < 60:
		return fmt.Sprintf("%.1fmin", m)
	case m < 60*24:
		return fmt.Sprintf("%.1fh", m/60)
	default:
		return fmt.Sprintf("%.1fd", m/(60*24))
	}
}
