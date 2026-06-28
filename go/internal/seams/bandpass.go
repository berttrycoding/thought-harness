// This file is the hidden seam's BAND-PASS filter (04-seams.md §2.1) — noise-suppression realised as
// loop-shaping, not an ad-hoc relevance gate, so it lives inside the same stability budget the
// regulator enforces (n<1, 0<K·g<2). Two complementary discrete-time filters over TICKS:
//
//   - LOW-PASS (LPF): an EMA  y[t] = (1−α_low)·y[t−1] + α_low·x[t]  — passes the PERSISTENT /
//     corroborated signal, rejects high-frequency transients. Cognitive job: kill the flash-in-the-pan
//     hallucination — a one-tick spike is not yet real. Control analogue: integral-like, adds phase LAG.
//   - HIGH-PASS (HPF): x[t] − LPF(x)[t] — passes the NOVEL / changed signal, rejects the constant known
//     background (DC). Cognitive job: inject only what ADDS information; let the already-known fade.
//     Control analogue: derivative-like, adds phase LEAD — the D of the PID regulator.
//
// Together = a BAND-PASS: pass only what is persistent ENOUGH to be real (LPF high) AND novel ENOUGH to
// be worth it (HPF high). The stream stays clean from both sides — not flooded by transient noise, not
// flooded by restatement of the known.
//
// Determinism (04 §2.1, hard constraint): cheap deterministic recurrences over TICKS — NO wall clock,
// NO RNG — fitting the tick-clocked engine and the golden oracle. The two cutoff frequencies are the
// tunable-but-FROZEN-at-runtime "skin" (04 §1): tuned offline in dev (keep-or-revert), inside the
// n<1 / 0<K·g<2 durability budget — a band too wide / high-gain raises the injection rate and pushes
// excitation n toward 1, so the cutoffs are HOW the seam keeps its share of n subcritical.
package seams

import "math"

// BandPassConfig holds the two control parameters — the cutoff frequencies expressed as EMA smoothing
// factors in (0,1]. AlphaLow is the LPF cutoff (persistence): SMALL α_low ⇒ a slow, heavily-smoothing
// LPF (strong transient rejection, more lag); large α_low ⇒ a fast LPF (lets sharper changes through).
// AlphaHigh is the LPF cutoff used to FORM the high-pass reference (novelty time-constant): the HPF is
// x − LPF_high, so SMALL α_high ⇒ a slow reference ⇒ a signal stays "novel" for longer (the
// relevance-decay time constant, 04 §2.1); large α_high ⇒ novelty fades fast.
type BandPassConfig struct {
	AlphaLow  float64 // LPF cutoff for the persistence/corroboration path
	AlphaHigh float64 // LPF cutoff that forms the novelty/high-pass reference
	// ColdStartZeroRef selects the FIRST-APPEARANCE handling (B1f, 04-seams §2.1). The default
	// (false) seeds BOTH EMAs to x[0] on the priming tick, which makes HPF = x − x = 0 on first
	// appearance — so a signal that appears HIGH and SUSTAINS high reads as novelty 0 FOREVER and is
	// suppressed on every tick (the documented cold-start spec-divergence: a first-appearance step is a
	// NOVEL step-edge the conscious has never seen, yet it is killed). When true the filter instead
	// COLD-STARTS from 0 — both EMAs ramp up from zero rather than being seeded to x[0] — and suppresses
	// ONLY the single priming tick (a one-tick warm-up so a true flash-in-the-pan still never injects on
	// appearance). From the next tick a SUSTAINED first-appearance-high signal reads as novel (HPF =
	// x − a small ramping reference > 0) AND persistent (the LPF has begun to build), so it INJECTS at
	// the step and then FADES to DC as the reference catches up — exactly the spec's HPF intent ("inject
	// only what ADDS information; let the already-known fade"). Tunable skin (04 §1): it does NOT widen
	// the band or raise the gain (a transient is still killed, the floor is unchanged), so it stays inside
	// the n<1 / 0<K·g<2 budget — it only repairs WHICH first appearance the HPF lets through.
	ColdStartZeroRef bool
}

// DefaultBandPassConfig is the conservative dev-tuned default (04 §6 leaves the numeric cutoffs open as
// a dev-tuning target; these are sensible starting priors, NOT a benchmark-validated value):
//   - AlphaLow 0.30 — a moderately slow LPF: a one-tick spike carries only ~0.30 of its amplitude, so a
//     flash-in-the-pan is attenuated, while a few sustained ticks build the LPF up past the persistence
//     band. (More noise rejection = more lag = less margin; balanced against the HPF lead.)
//   - AlphaHigh 0.20 — a slower novelty reference, so a genuinely new step reads as novel for several
//     ticks (long enough to earn a retracement) before the reference catches up and it fades to "known".
func DefaultBandPassConfig() BandPassConfig {
	return BandPassConfig{AlphaLow: 0.30, AlphaHigh: 0.20}
}

// BandResult is one tick's output of the filter — the band-pass scalar plus the two intermediate
// channels (exposed for tracing/diagnostics and so the caller can route on either half: LPF on the
// Filter/admission side for persistence, HPF on the Gate/held-buffer side for novelty — 04 §2.1).
type BandResult struct {
	Passed   float64 // the band-pass output: persistent (LPF) AND novel (HPF), in [0,1]
	LowPass  float64 // the LPF state after this tick (persistence/corroboration channel)
	HighPass float64 // the HPF value this tick: x − LPF_high, clamped to [0,1] (novelty channel)
}

// BandPass is a stateful, tick-domain band-pass filter. It is a pure recurrence: Step(x) advances one
// tick and returns the band-pass output. NOT goroutine-safe (one filter per stream); deterministic
// under a fixed input sequence (no clock, no RNG). The zero value is NOT ready — build with NewBandPass.
type BandPass struct {
	cfg BandPassConfig

	lowState  float64 // the LPF accumulator (persistence path)
	highState float64 // the LPF accumulator that forms the novelty reference (the HPF subtracts it)
	primed    bool    // false until the first Step seeds the EMAs (legacy: to x[0]; cold-start: from 0)
	warmup    bool    // true for exactly the priming tick under cold-start: its output is forced to 0
}

// NewBandPass builds a filter from its config, clamping each cutoff into (0,1] so a misconfigured knob
// can never make the recurrence diverge or stall (a control parameter must stay inside the stability
// budget — 04 §2.1). A non-positive alpha is lifted to a tiny epsilon (a maximally-smoothing filter,
// never a frozen one); an alpha above 1 is clamped to 1 (instantaneous pass-through, no smoothing).
func NewBandPass(cfg BandPassConfig) *BandPass {
	return &BandPass{cfg: BandPassConfig{
		AlphaLow:         clampAlpha(cfg.AlphaLow),
		AlphaHigh:        clampAlpha(cfg.AlphaHigh),
		ColdStartZeroRef: cfg.ColdStartZeroRef,
	}}
}

// Step advances the filter by one TICK with input sample x and returns this tick's band-pass output.
//
//	LPF:  low[t]  = (1−α_low)·low[t−1]   + α_low·x[t]        (persistence)
//	ref:  high[t] = (1−α_high)·high[t−1] + α_high·x[t]       (the slow novelty reference)
//	HPF:  hp[t]   = clamp_[0,1]( x[t] − high[t] )            (novelty: only POSITIVE change counts)
//	band: passed  = min( low[t], hp[t] )                     (a true AND: persistent AND novel)
//
// FIRST-APPEARANCE handling (B1f). With ColdStartZeroRef OFF (the legacy default) both EMAs are SEEDED
// to x on the priming tick so a constant input does not ramp up from zero (which would spuriously read
// as "novel" for the first few ticks) — but the side-effect is HPF = x − x = 0 on first appearance, so a
// signal that appears HIGH and SUSTAINS high reads as novelty 0 FOREVER and is suppressed every tick (the
// documented spec-divergence). With ColdStartZeroRef ON the EMAs instead ramp from 0 and the priming tick
// alone is forced to output 0 (a one-tick warm-up): a true flash-in-the-pan still never injects on
// appearance, but from the next tick a SUSTAINED first-appearance-high signal reads as novel (HPF > 0)
// AND persistent (LPF building), so it injects at the step and fades to DC as the reference catches up.
//
// The band-pass uses min — the strictest AND — so a high score requires BOTH halves: a transient (low
// LPF) is killed by the LPF term, a stale restatement (HPF→0 once the reference converges) is killed by
// the HPF term.
func (b *BandPass) Step(x float64) BandResult {
	if !b.primed {
		if b.cfg.ColdStartZeroRef {
			// Cold-start ramp: both EMAs begin at 0, then absorb this tick. The novelty reference is now
			// BELOW x on every fresh appearance (HPF > 0), so a first-appearance step is no longer killed —
			// but to keep a one-tick flash from injecting on appearance we suppress THIS (priming) tick only.
			al := b.cfg.AlphaLow
			ah := b.cfg.AlphaHigh
			b.lowState = al * x
			b.highState = ah * x
			b.warmup = true
		} else {
			b.lowState = x
			b.highState = x
		}
		b.primed = true
	} else {
		al := b.cfg.AlphaLow
		ah := b.cfg.AlphaHigh
		b.lowState = (1-al)*b.lowState + al*x
		b.highState = (1-ah)*b.highState + ah*x
	}

	// HPF = the part of x the slow reference has NOT yet absorbed. Only a positive excess is novelty
	// (a signal fading BELOW its reference is "already known / receding", not new) — clamp to [0,1].
	hp := x - b.highState
	if hp < 0 {
		hp = 0
	}
	if hp > 1 {
		hp = 1
	}

	// the persistence channel, clamped to [0,1] for a clean band-pass scalar (inputs are expected in
	// [0,1]; clamp keeps the output well-formed even if a caller passes a slightly out-of-range sample).
	low := b.lowState
	if low < 0 {
		low = 0
	}
	if low > 1 {
		low = 1
	}

	passed := math.Min(low, hp)
	if b.warmup {
		// One-tick cold-start warm-up: the priming tick of a fresh stream never injects on its own (a
		// flash-in-the-pan is one tick), so its band-pass output is forced to 0 — the persistence test
		// is deferred to the next tick, which is where a SUSTAINED first appearance earns its injection.
		passed = 0
		b.warmup = false
	}
	return BandResult{Passed: passed, LowPass: low, HighPass: hp}
}

// Reset returns the filter to its just-constructed state (config retained) so a fresh episode starts
// cold — the per-episode determinism the engine relies on. The next Step re-primes the EMAs to its x.
func (b *BandPass) Reset() {
	b.lowState = 0
	b.highState = 0
	b.primed = false
	b.warmup = false
}

// clampAlpha forces an EMA smoothing factor into (0,1]: a non-positive value becomes a tiny epsilon (a
// maximally-smoothing but still-live filter — never a frozen pole at 1.0), an above-1 value becomes 1
// (instantaneous, no smoothing). Keeps the recurrence stable for any configured cutoff.
func clampAlpha(a float64) float64 {
	const eps = 1e-6
	if math.IsNaN(a) || a <= 0 {
		return eps
	}
	if a > 1 {
		return 1
	}
	return a
}
