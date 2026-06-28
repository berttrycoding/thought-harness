// Package regulator is the homeostatic regulator — holding the durable regime (the
// durability analysis, in code).
//
// Durability is NOT a free consequence of triggering specialists; it is a *regulated
// subcritical seeded* regime. This controller holds it:
//
//	n < 1           subcritical branching — forks per thought (the recursive cascade, NOT fan-out)
//	μ > 0           positive endogenous baseline (drives/default-mode) — awake mode only
//	U ≤ 1           focus not over-subscribed (scheduler)
//	0 < K·g < 2     regulator stable (proportional control on θ)
//	ω·τ < PM        async action dead-time bounded
//
// Stationary rate (when stationary): λ̄ = μ / (1 − n). Control law: θ_{k+1} = θ_k + K·(λ̂ − λ*).
// Raising θ makes the gate stricter, fewer injections survive, the cascade falls — converting
// the §1.1 knife-edge into a stable operating point.
//
// n is the recursive branching ratio (forks/thought), not the raw admit count. A bounded
// parallel fan-out (w sub-agents in one tick) is a *schedulability/intensity* load — it shows
// up in λ̂ and U — but it collapses to one gate winner and forks only on conflict, so it does
// NOT drive n. This is what lets parallel breadth scale without touching durability (see
// docs/reference/stability-dynamic-dimensionality.md).
package regulator

import (
	"fmt"
	"math"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// Config holds the regulator constants (Python RegulatorConfig). All defaults are the Python
// dataclass defaults; DefaultConfig reproduces them.
type Config struct {
	LamStar float64 // target intensity (candidate-production rate)
	GainK   float64 // proportional gain
	Alpha   float64 // EMA smoothing
	GEst    float64 // -∂λ̂/∂θ plant-gain PRIOR. The `0<K·g<2` check now uses GainEstimate() — a
	//                       lag-1 regression over the history ring with a confidence gate (gain.go,
	//                       X.6 #15 RESOLVED) — and falls back to THIS prior only when the data does
	//                       not identify the plant (θ pinned / exogenous noise dominates / too few moves).
	FocusCapacity int     // schedulable branch budget
	ActionMargin  float64 // phase-margin proxy for async dead-time
	ThetaMin      float64
	ThetaMax      float64
}

// DefaultConfig returns the Python RegulatorConfig defaults.
func DefaultConfig() Config {
	return Config{
		LamStar:       1.0,
		GainK:         0.4,
		Alpha:         0.35,
		GEst:          0.5,
		FocusCapacity: 8,
		ActionMargin:  1.5,
		ThetaMin:      0.05,
		ThetaMax:      0.95,
	}
}

// ema is the exponential moving average step (Python _ema): alpha*new + (1-alpha)*old.
func ema(old, new, alpha float64) float64 {
	return alpha*new + (1-alpha)*old
}

// UpdateOpts carries the per-tick durability accounting handed to Update (Python update()'s
// 7 keyword args). The first four (Fired/Admitted/Baseline/BranchesLive) have no Python
// default — the caller always supplies them. The trailing three default to Python's values
// via DefaultUpdateOpts; in particular Forked carries the load-bearing -1 sentinel ("no fork
// count supplied → fall back to the admitted-1 proxy"), which is NOT the int zero value, so a
// bare UpdateOpts{} would silently change n-decoupling. Start from DefaultUpdateOpts (or set
// Forked: -1 explicitly).
type UpdateOpts struct {
	Fired             int     // excitation events this tick (specialists fired)
	Admitted          int     // candidates admitted through the gate this tick
	Baseline          int     // endogenous baseline / immigrant events this tick
	BranchesLive      int     // live branches (schedulability load)
	Forked            int     // forks actually created this tick; -1 == "not supplied" (load-bearing sentinel)
	ActionOutstanding int     // outstanding async actions (>0 => action_rate sample is 1)
	FeedbackLatency   float64 // async feedback dead-time (ticks); 0 keeps the previous value
}

// DefaultUpdateOpts returns an UpdateOpts pre-set to Python's keyword defaults: Forked = -1
// (the sentinel), ActionOutstanding = 0, FeedbackLatency = 0. The caller fills the four
// required counts.
func DefaultUpdateOpts() UpdateOpts {
	return UpdateOpts{Forked: -1}
}

// Check is one durability condition's verdict (the typed replacement for Python's
// heterogeneous dict[str, bool|str] entry). NA is set for a VACUOUS condition where Pass is
// meaningless: the μ>0 check in reactive mode (self-terminating), or (C0a) the 0<K·g<2 loop-gain
// check when the control loop is open (saturated / insufficient-loop) so the gain is unidentifiable
// by construction. NADetail carries the per-NA reason rendered on the wire + the failure detail (e.g.
// "K·g N/A — saturated/open-loop"); empty NADetail keeps the legacy reactive-μ string.
type Check struct {
	Name     string
	Pass     bool
	NA       bool
	NADetail string
}

// Regulator is the homeostatic controller. theta is the control variable (the admission
// threshold); the rest are the measured/derived durability metrics.
type Regulator struct {
	emit events.Emit
	cfg  Config

	theta           float64 // admission threshold (the control variable)
	lamHat          float64 // measured intensity (EMA)
	n               float64 // branching ratio (offspring per thought)
	mu              float64 // baseline / immigrant rate
	U               float64 // utilization
	actionRate      float64
	feedbackLatency float64

	history *ring // EMA history snapshots, ring buffer maxlen 240
}

// New builds a Regulator with the Python initial state (theta=0.3, lam_hat=1.0, the rest 0).
// A nil cfg pointer uses DefaultConfig.
func New(emit events.Emit, cfg *Config) *Regulator {
	c := DefaultConfig()
	if cfg != nil {
		c = *cfg
	}
	return &Regulator{
		emit:    emit,
		cfg:     c,
		theta:   0.3,
		lamHat:  1.0,
		history: newRing(240),
	}
}

// Update folds one tick of durability accounting into the metrics and runs the proportional
// control law, then emits one regulator.update event. Mirrors Python Regulator.update.
func (r *Regulator) Update(o UpdateOpts) {
	a := r.cfg.Alpha
	// measured intensity λ̂ = excitation + baseline events this tick. This carries the full
	// load, INCLUDING a parallel fan-out's burst — so throughput/intensity control still sees
	// fan-out.
	r.lamHat = ema(r.lamHat, float64(o.Fired+o.Baseline), a)
	// Branching ratio n = OFFSPRING per thought — the recursive Galton-Watson rate that must
	// be subcritical. The true offspring are the FORKS actually created (a thought spawning a
	// new branch), NOT the admitted candidates: a parallel fan-out's w candidates collapse to
	// one gate winner (voiced into the active branch) and fork only on genuine conflict, so
	// they do not recurse. Measuring forks decouples n from bounded fan-out width — the
	// parallel breadth is a schedulability/compute load (λ̂, U), not a branching cascade (n).
	// When the caller does not supply a fork count, fall back to the conservative admitted-1
	// proxy.
	var offspring float64
	if o.Forked >= 0 {
		offspring = float64(o.Forked)
	} else {
		offspring = float64(max(0, o.Admitted-1))
	}
	r.n = ema(r.n, offspring, a)
	r.mu = ema(r.mu, float64(o.Baseline), a)
	r.U = float64(o.BranchesLive) / float64(r.cfg.FocusCapacity)
	r.actionRate = ema(r.actionRate, b2f(o.ActionOutstanding > 0), a)
	if o.FeedbackLatency != 0.0 {
		r.feedbackLatency = o.FeedbackLatency
	}

	// CONTROL: raise θ when over-active, lower when under-active.
	r.theta += r.cfg.GainK * (r.lamHat - r.cfg.LamStar)
	r.theta = math.Max(r.cfg.ThetaMin, math.Min(r.cfg.ThetaMax, r.theta))

	// snap mirrors Python's snap dict: theta/lam_hat/n/mu/U rounded to 3 (round at the emit
	// site, per the wire contract), lam_bar the RAW property value (∞ possible).
	snap := events.D{
		"theta":   round3(r.theta),
		"lam_hat": round3(r.lamHat),
		"lam_bar": r.LamBar(),
		"n":       round3(r.n),
		"mu":      round3(r.mu),
		"U":       round3(r.U),
	}
	r.history.push(snap)
	if r.emit != nil {
		r.emit(events.Regulator,
			fmt.Sprintf("θ=%.2f λ̂=%.2f λ̄=%s n=%.2f μ=%.2f U=%.2f",
				r.theta, r.lamHat, fmtLamBar(r.LamBar()), r.n, r.mu, r.U),
			snap)
	}
}

// LamBar is the predicted stationary rate λ̄ = μ/(1−n); +∞ at/above the n=1 cliff. Mirrors
// the Python lam_bar property (math.inf guard at n>=1).
func (r *Regulator) LamBar() float64 {
	if r.n < 1.0 {
		return r.mu / (1 - r.n)
	}
	return math.Inf(1)
}

// Regime is the C0a/C0b loop-gain regime classification for the run — WHICH durability story the
// 0<K·g<2 condition is being held under. The label is reported on the stability output + wire so a
// verdict is never a hidden prior-pass nor a misleading FAIL: it says exactly why the loop-gain check
// is a real bool, vacuous, or an honest failure.
type Regime int

const (
	// RegimeActivelyControlled — the plant gain g is IDENTIFIED from the history ring and the loop is
	// genuinely closed; 0<K·g<2 is a REAL, failable check on the measured gain (its Pass holds or fails
	// honestly). The only regime in which the loop-gain condition is load-bearing.
	RegimeActivelyControlled Regime = iota
	// RegimeSaturatedBounded — the controller is railed at a clamp (the awake steady-state pins θ at
	// ThetaMin: little to control, the loop is open by choice). 0<K·g<2 is VACUOUS (the control law
	// cannot move θ, so the gain is unidentifiable by construction); durability is governed by the other
	// four boundedness conditions, with λ̄=μ/(1−n) finite under n<1. A PASS-by-openness, NOT a prior-pass.
	RegimeSaturatedBounded
	// RegimeInsufficientLoop — the loop never established a SUSTAINED identifiable regime: too few
	// regulator ticks, or a short converging-and-terminating reactive transient that ratchets θ once and
	// settles (HOLE 2). Loop-gain (an asymptotic property) is VACUOUS, NOT a failure — distinct from the
	// honest fail so a settling episode is never reported as unstable.
	RegimeInsufficientLoop
	// RegimeUnidentifiedActive — a SUSTAINED loop with θ genuinely MOVING (real excitation, many
	// θ-moving sample-pairs) whose plant is STILL not identified: the loop gain is unvouched on a hot,
	// active plant. This is the HONEST FAIL the old prior-fallback silently hid (K·prior=0.2<2 always
	// passed). 0<K·g<2 is reported Pass=false (not NA).
	RegimeUnidentifiedActive
	// RegimeSaturatedRunaway — the controller is railed at ThetaMax (MAXIMUM suppression) yet λ̂ remains
	// far above λ* (above the intensity ceiling): the controller is trying and FAILING to bring the
	// intensity down — it has LOST intensity control (HOLE 1). This is NOT a benign open loop; it is a
	// control-loss FAIL. 0<K·g<2 is reported Pass=false (not NA).
	RegimeSaturatedRunaway
)

// String renders the regime label shown on the stability output + wire.
func (rg Regime) String() string {
	switch rg {
	case RegimeActivelyControlled:
		return "actively-controlled-stable"
	case RegimeSaturatedBounded:
		return "saturated-bounded"
	case RegimeInsufficientLoop:
		return "insufficient-loop"
	case RegimeUnidentifiedActive:
		return "unidentified-active-FAIL"
	case RegimeSaturatedRunaway:
		return "saturated-runaway-FAIL"
	}
	return "unknown"
}

// runawayLamFactor is the intensity ceiling for the ThetaMax-saturation guard (HOLE 1), as a multiple
// of λ*: a controller railed at MAXIMUM suppression whose measured intensity λ̂ still exceeds
// runawayLamFactor·λ* has demonstrably LOST intensity control (it is at the limit of what it can do and
// the plant is still this far over setpoint). 1.5× is the principled ceiling: a ThetaMax rail with
// λ̂≈λ* (transient overshoot the controller is still holding) stays benign, but a genuine runaway
// (λ̂≫λ*, e.g. the 100×λ* red-team repro) trips it.
const runawayLamFactor = 1.5

// StabilityRegime is the C0a/C0b loop-gain-aware durability checklist: it re-derives the five
// durability conditions for the given mode AND classifies which loop-gain regime governs the 0<K·g<2
// condition, so the verdict is honest about whether that check is a real bool, vacuous (open loop /
// insufficient loop), or an honest fail. It returns the typed []Check (in the canonical order n<1,
// U<=1, 0<K*g<2, w*tau<PM, mu>0), the Regime, the held-count (checks strictly True, not NA), and
// whether g was MEASURED from the ring (true) or is the prior fallback (false). Pure read — no state
// is mutated, nothing is emitted.
//
// The 0<K·g<2 entry is resolved by the loop regime:
//   - open loop (saturated at ThetaMin, or insufficient/inactive) → NA (vacuous): saturated-bounded or
//     insufficient-loop. Durability rests on the other four boundedness conditions.
//   - saturated at ThetaMax with λ̂ over the intensity ceiling → control-loss FAIL: saturated-runaway.
//   - sustained-moving loop, plant unidentified → honest FAIL: unidentified-active.
//   - closed loop, plant identified → REAL bool on the measured gain: actively-controlled.
func (r *Regulator) StabilityRegime(mode string) (checks []Check, regime Regime, held int, measured bool) {
	g, measured := r.GainEstimate()
	kg := r.cfg.GainK * g

	// Resolve the loop-gain check + regime from the measured loop state.
	kgCheck := Check{Name: "0<K*g<2 (regulator stable)"}
	open, reason := r.LoopOpen()
	switch {
	case open && reason == "saturated":
		// Open by saturation — distinguish BENIGN (ThetaMin rail / bounded-λ̂ ThetaMax rail) from
		// PATHOLOGICAL (ThetaMax rail with λ̂ over the intensity ceiling = control-loss). HOLE 1.
		_, clamp := r.SaturatedAt()
		if clamp == "max" && r.lamHat > runawayLamFactor*r.cfg.LamStar {
			// Maxed-out suppression yet λ̂ still far over setpoint: the controller has lost intensity
			// control. NOT vacuous — a real FAIL.
			kgCheck.Pass = false
			regime = RegimeSaturatedRunaway
		} else {
			kgCheck.NA = true
			kgCheck.NADetail = "K·g N/A — saturated/open-loop"
			regime = RegimeSaturatedBounded
		}
	case open: // reason == "insufficient-loop" || "inactive" — no sustained identifiable loop.
		kgCheck.NA = true
		kgCheck.NADetail = "K·g N/A — insufficient-loop/open-loop"
		regime = RegimeInsufficientLoop
	case measured:
		// Closed, identified loop — the loop-gain condition is REAL and failable on the measured gain.
		kgCheck.Pass = 0.0 < kg && kg < 2.0
		regime = RegimeActivelyControlled
	default:
		// Sustained, MOVING loop whose plant is still NOT identified — the honest fail the old
		// prior-fallback hid. NOT vacuous.
		kgCheck.Pass = false
		regime = RegimeUnidentifiedActive
	}

	checks = []Check{
		{Name: "n<1 (subcritical)", Pass: r.n < 1.0},
		{Name: "U<=1 (schedulable)", Pass: r.U <= 1.0},
		kgCheck,
		{Name: "w*tau<PM (async bounded)", Pass: r.actionRate*r.feedbackLatency < r.cfg.ActionMargin},
	}
	muCheck := Check{Name: "mu>0 (awake baseline)"}
	if mode == "reactive" {
		muCheck.NA = true // Python: "N/A reactive (self-terminating)"
	} else {
		muCheck.Pass = r.mu > 0.0
	}
	checks = append(checks, muCheck)

	for _, c := range checks {
		if c.Pass && !c.NA {
			held++
		}
	}
	return checks, regime, held, measured
}

// Stability re-derives the durability checklist for the given mode and (when emit is true) emits one
// regulator.stability event carrying the per-check verdicts + the C0a loop-gain regime label. It is
// the emit-side wrapper over StabilityRegime; returns the typed []Check in the canonical order (n<1,
// U<=1, 0<K*g<2, w*tau<PM, mu>0).
//
// The μ>0 check is awake-mode only: in reactive mode μ=0 is correct (self-terminating), so it is
// reported NA. The 0<K·g<2 check is resolved by the loop regime (see StabilityRegime): a vacuous NA
// under an open/insufficient loop, an honest fail under a moving-unidentified or runaway loop, a real
// bool under an identified loop — never the old silent prior-pass.
func (r *Regulator) Stability(mode string, emit bool) []Check {
	checks, regime, held, _ := r.StabilityRegime(mode)
	if emit && r.emit != nil {
		data := events.D{"mode": mode, "regime": regime.String()}
		for _, c := range checks {
			// Wire shape: each check NAME is a data key whose value is its bool, or its NA detail
			// string (the loop-gain NA detail, or the legacy reactive-μ string for the μ entry).
			switch {
			case c.NA && c.NADetail != "":
				data[c.Name] = c.NADetail
			case c.NA:
				data[c.Name] = "N/A reactive (self-terminating)"
			default:
				data[c.Name] = c.Pass
			}
		}
		r.emit(events.Stability,
			fmt.Sprintf("durable regime [%s]: %d/%d hard checks hold", regime.String(), held, len(checks)),
			data)
	}
	return checks
}

// History returns the EMA snapshot ring (oldest-to-newest), bounded at 240 (the panels read
// it for the durability sparkline). A fresh slice; safe to retain.
func (r *Regulator) History() []events.D { return r.history.items() }

// Theta / LamHat / N / Mu / U expose the live metrics for the TUI snapshot. Read-only.
func (r *Regulator) Theta() float64  { return r.theta }
func (r *Regulator) LamHat() float64 { return r.lamHat }
func (r *Regulator) N() float64      { return r.n }
func (r *Regulator) Mu() float64     { return r.mu }
func (r *Regulator) Util() float64   { return r.U }

// FocusCapacity exposes the configured schedulable branch budget (Python reads
// regulator.cfg.focus_capacity directly; cfg is unexported here, so the engine's prune-branches
// bound reaches it through this accessor).
func (r *Regulator) FocusCapacity() int { return r.cfg.FocusCapacity }

// fmtLamBar formats λ̄ for the console summary: Python's _fmt — "∞" for +∞, else two decimals.
func fmtLamBar(x float64) string {
	if math.IsInf(x, 1) {
		return "∞"
	}
	return fmt.Sprintf("%.2f", x)
}

// round3 reproduces Python's round(x, 3): format to 3 fixed decimals (round-half-to-even, as
// both strconv.FormatFloat and CPython's float __round__ use) and parse back, so the emitted
// value matches the Python wire byte-for-byte. +∞/NaN pass through unchanged.
func round3(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}

// b2f maps a bool to the 0.0/1.0 EMA sample Python takes via float(bool).
func b2f(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
