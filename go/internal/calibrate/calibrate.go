// Package calibrate is the SLAM M9 calibration meta-estimator — it LEARNS each source's reliability
// (per trust tier) from the predicted-vs-actual outcome stream and RE-ESTIMATES the measurement
// precision R that the M1 innovation update (control.Innovate) uses, instead of trusting the fixed
// TierPrecision prior forever.
//
// Why this is the highest-value capability item (design doc §3b.3 #5, G9; build-plan §6 M9): the
// measured "same-model self-judging ceiling" (self-verify = attempt-#1-in-disguise; 0-of-K can't fix a
// bias) is a CALIBRATION failure — the harness trusts a fixed prior on how reliable a source is. M9
// lets it DISCOVER, from outcomes, that a source (especially its own confident self-prediction) is
// systematically over- or under-confident, and re-weight it. It does NOT re-judge a belief with the
// same model (that can't beat the first-grounding floor, P1); it re-estimates the NOISE MODEL of each
// source from an accumulating record of how often that source's observations actually confirmed the
// prediction.
//
// The signal it reads is the M1 residual stream: every grounded control.Residual carries the model's
// PRIOR stance PriorMean (the prediction) and the grounded Obs (+1 confirm / -1 refute) at a known
// trust tier. The agreement between sign(PriorMean) and Obs is one calibrated outcome for that tier.
//
// THE INVARIANT (mirrors regulator/gain.go's honesty discipline): a LEARNED precision is used ONLY when
// the tier is identified (enough samples). Under-sampled, it falls back HONESTLY to the prior tier
// precision — a noisy half-dozen-sample reliability must never silently swing the measurement update.
// And calibration NEVER manufactures certainty: it can only RE-WEIGHT how much an observation is
// trusted; the §0 invariant (variance shrinks only on a grounded observation) is untouched.
//
// Pure CONTROL: no model call ever (it consumes the control.Residual the Pattern-A innovation produced),
// deterministic, RNG-free, clock-free (the engine feeds the seeded loop tick for the wire only).
package calibrate

import (
	"math"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// calibration constants — deliberately conservative (a wrong "learned" precision is worse than the
// honest prior), mirroring regulator/gain.go's confidence-gate discipline.
const (
	// calMinSamples is the minimum number of grounded outcomes a tier must accumulate before its learned
	// reliability may replace the fixed prior. Below this the tier is UNIDENTIFIED and LearnedPrecision
	// returns the prior unchanged (the honest fallback). Same spirit as gainMinPairs.
	calMinSamples = 8
	// relFloor / relCeil bound the learned reliability multiplier so a run of bad luck cannot drive the
	// precision to zero (a source we'd never trust again) or inflate it unboundedly. A source can be
	// down-weighted to relFloor of its prior or boosted to relCeil.
	relFloor = 0.1
	relCeil  = 2.0
	// confidentStance is the |PriorMean| above which a prediction counts as a CONFIDENT assertion for the
	// overconfidence (self-calibration) readout. A belief asserted this confidently that reality refutes
	// is an overconfidence event — the headline same-model-ceiling signal.
	confidentStance = 0.5
)

// TierStat is the accumulated calibration record for one trust tier: how many grounded outcomes were
// seen and how many AGREED with the model's prediction (the prediction's sign matched the grounded
// observation). hitRate = hits/samples is the empirical reliability of that source's observations
// relative to the prediction; the learned precision multiplier is derived from it.
type TierStat struct {
	Samples int // grounded outcomes folded in for this tier
	Hits    int // outcomes where the grounded obs AGREED with the predicted stance
	// ConfidentSamples / ConfidentRefutes track CONFIDENT predictions (|PriorMean| >= confidentStance,
	// asserting the belief is true) that this tier REFUTED — the overconfidence (same-model-ceiling)
	// signal. A high refute fraction means the model is confidently wrong and must be down-weighted.
	ConfidentSamples int
	ConfidentRefutes int
}

// Config holds the calibrator's tunables.
type Config struct {
	Enabled    bool // slam.calibration flag; when false the calibrator is inert (no learning, no events)
	MinSamples int  // override calMinSamples (0 => the default); the identification gate
}

// DefaultConfig returns the M9 defaults: disabled, with the conservative identification gate.
func DefaultConfig() Config { return Config{Enabled: false, MinSamples: calMinSamples} }

// Calibrator is the ticked meta-estimator: per-tier outcome accumulators + the learned-precision read.
// Construct with New; like the estimator/regulator it is NOT safe for concurrent use (the engine ticks
// it serially on the deterministic loop).
type Calibrator struct {
	byTier  map[int]*TierStat // keyed on the grounding trust-tier ordinal (0..TierCount-1)
	cfg     Config
	bus     events.Emit
	curTick int
}

// New builds a Calibrator from a config + the event-bus emit closure (nil-safe bus disables emission).
// When cfg.Enabled is false the calibrator is inert and the engine bypasses it (default OFF =>
// byte-identical: no learning, no re-weighting, no events).
func New(cfg Config, bus events.Emit) *Calibrator {
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = calMinSamples
	}
	return &Calibrator{byTier: map[int]*TierStat{}, cfg: cfg, bus: bus}
}

// Enabled reports whether the calibrator is active (the slam.calibration flag). The engine checks this
// at the call site so the OFF path is byte-identical.
func (c *Calibrator) Enabled() bool { return c != nil && c.cfg.Enabled }

// SetEnabled honours a live config flip (the TUI's slam.calibration toggle); flipping OFF leaves the
// accumulated stats intact (a re-flip-ON resumes learning from them). Nil-safe.
func (c *Calibrator) SetEnabled(on bool) {
	if c == nil {
		return
	}
	c.cfg.Enabled = on
}

// SetTick records the seeded loop tick for the wire payload (deterministic; never the wall clock).
func (c *Calibrator) SetTick(tick int) {
	if c == nil {
		return
	}
	c.curTick = tick
}

// minSamples returns the identification gate (the configured override or the default).
func (c *Calibrator) minSamples() int {
	if c.cfg.MinSamples > 0 {
		return c.cfg.MinSamples
	}
	return calMinSamples
}

// Observe folds ONE grounded measurement outcome (the M1 residual) into the tier's calibration record.
// It reads the prediction (residual.PriorMean) and the grounded observation (residual.Obs, +1/-1) at
// the given trust tier, scores whether they AGREED, accumulates the overconfidence counter, and emits
// estimate.calibrate with the updated learned reliability for the tier. Gated (data-association-failed)
// residuals are NOT folded in — a rejected observation is not evidence about the source's reliability,
// it is evidence the obs was about a DIFFERENT belief. No-op when disabled. Pure: deterministic in its
// arguments + accumulated state.
func (c *Calibrator) Observe(tierOrdinal int, r control.Residual) {
	if !c.Enabled() {
		return
	}
	if r.Gated {
		return // a data-association reject is not evidence about this source's reliability
	}
	st := c.byTier[tierOrdinal]
	if st == nil {
		st = &TierStat{}
		c.byTier[tierOrdinal] = st
	}
	st.Samples++
	// AGREEMENT: the grounded observation matched the predicted stance. A positive prediction (the model
	// asserted the belief is true) AGREES with a +1 (confirming) observation; a refutation disagrees.
	// A near-zero prediction (no stance) counts as agreeing with a confirmation by convention (the model
	// took no strong position, so reality did not catch it being wrong).
	predictedTrue := r.PriorMean >= 0
	observedTrue := r.Obs > 0
	if predictedTrue == observedTrue {
		st.Hits++
	}
	// OVERCONFIDENCE (the same-model-ceiling headline): a CONFIDENT assertion (|PriorMean| high, positive
	// stance) that reality REFUTED.
	if r.PriorMean >= confidentStance {
		st.ConfidentSamples++
		if r.Obs < 0 {
			st.ConfidentRefutes++
		}
	}

	rel, measured := c.reliabilityOf(tierOrdinal)
	priorPrec := control.TierPrecision(tierOrdinal)
	c.emit(events.EstimateCalibrate, "calibrate tier "+itoa(tierOrdinal), events.D{
		"tier":           tierOrdinal,
		"samples":        st.Samples,
		"hits":           st.Hits,
		"hitRate":        round3(c.hitRate(st)),
		"reliability":    round3(rel),
		"priorPrec":      round3(priorPrec),
		"learnedPrec":    round3(priorPrec * rel),
		"overconfidence": round3(c.overconfidence(st)),
		"measured":       measured,
	})
}

// hitRate is the empirical agreement rate of a tier's observations with the prediction. A never-seen
// tier returns 0 (caller checks Samples first).
func (c *Calibrator) hitRate(st *TierStat) float64 {
	if st == nil || st.Samples == 0 {
		return 0
	}
	return float64(st.Hits) / float64(st.Samples)
}

// overconfidence is the fraction of CONFIDENT assertions a tier REFUTED — the same-model-ceiling
// signal. 0 = the model's confident beliefs always held against this source; high = confidently wrong.
func (c *Calibrator) overconfidence(st *TierStat) float64 {
	if st == nil || st.ConfidentSamples == 0 {
		return 0
	}
	return float64(st.ConfidentRefutes) / float64(st.ConfidentSamples)
}

// reliabilityOf returns the LEARNED reliability multiplier for a tier and whether it was MEASURED
// (identified) or is the prior (1.0). The multiplier scales the prior tier precision: a source whose
// observations confirm the prediction reliably (high hit-rate) keeps/boosts its precision; a source
// that frequently catches the prediction being wrong (low hit-rate) is DOWN-WEIGHTED.
//
// Mapping (linear, centred so a perfectly-calibrated 50/50 source keeps its prior weight 1.0):
//
//	reliability = clamp( 2 * hitRate, relFloor, relCeil )
//
// hitRate 0.5  -> 1.0  (no change: the source is as the prior assumed)
// hitRate 1.0  -> 2.0  (capped at relCeil: this source's observations are highly diagnostic -> boost R^-1)
// hitRate 0.0  -> relFloor (this source never agrees with the prediction -> distrust its observations)
//
// Under-sampled (< minSamples) => returns 1.0, false: the honest fallback to the prior (the regulator/
// gain.go discipline — a wrong "measured" weight is worse than the honest prior).
func (c *Calibrator) reliabilityOf(tierOrdinal int) (reliability float64, measured bool) {
	st := c.byTier[tierOrdinal]
	if st == nil || st.Samples < c.minSamples() {
		return 1.0, false // unidentified -> prior precision unchanged
	}
	rel := 2.0 * c.hitRate(st)
	if rel < relFloor {
		rel = relFloor
	}
	if rel > relCeil {
		rel = relCeil
	}
	return rel, true
}

// LearnedPrecision re-estimates the measurement precision R^-1 for a grounding trust tier: the fixed
// prior control.TierPrecision(tier) scaled by the LEARNED reliability when the tier is identified, else
// the prior unchanged. This is the "learn R" output — the single value the M1 innovation update should
// use for the observation noise instead of the fixed prior. When disabled it returns the prior exactly
// (byte-identical), so the engine can call it unconditionally.
func (c *Calibrator) LearnedPrecision(tierOrdinal int) float64 {
	prior := control.TierPrecision(tierOrdinal)
	if !c.Enabled() {
		return prior
	}
	rel, _ := c.reliabilityOf(tierOrdinal)
	return prior * rel
}

// Overconfidence reports the learned overconfidence fraction for a tier (confident assertions that tier
// refuted) and whether it is identified — the headline same-model-ceiling readout for the calibration
// vitals line. Returns (0,false) when disabled or never-seen.
func (c *Calibrator) Overconfidence(tierOrdinal int) (frac float64, measured bool) {
	if !c.Enabled() {
		return 0, false
	}
	st := c.byTier[tierOrdinal]
	if st == nil || st.ConfidentSamples < c.minSamples() {
		return c.overconfidence(st), false
	}
	return c.overconfidence(st), true
}

// Vitals is the compact calibration readout for the Ctrl+O runtime monitor: how many tiers have an
// IDENTIFIED reliability, the worst (most overconfident) tier and its refute fraction. High worst-
// overconfidence is the "I am confidently wrong against an independent source" alarm. Returns zeros when
// disabled.
func (c *Calibrator) Vitals() (identified int, worstTier int, worstOverconf float64) {
	if !c.Enabled() {
		return 0, -1, 0
	}
	worstTier = -1
	for ord, st := range c.byTier {
		if st.Samples >= c.minSamples() {
			identified++
		}
		oc := c.overconfidence(st)
		if st.ConfidentSamples >= c.minSamples() && oc > worstOverconf {
			worstOverconf = oc
			worstTier = ord
		}
	}
	return identified, worstTier, round3(worstOverconf)
}

func (c *Calibrator) emit(kind, summary string, d events.D) {
	if c.bus == nil {
		return
	}
	c.bus(kind, summary, d)
}

// round3 rounds to 3 decimals for the wire payload (display-only; the math uses the raw values).
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// itoa is a tiny dependency-free int formatter for the event summary (avoids importing strconv for one
// use; matches the leaf-purity discipline of the sibling control package).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
