// Package estimate is the STATEFUL self-state estimator — SLAM M1 (innovation + FEJ-anchored Filter).
//
// It mirrors how internal/regulator holds state while internal/control holds the pure math: the
// closed-form scalar-Kalman step lives in control.Innovate (a Tier-1 leaf, no model, no state); this
// package holds the per-belief variance side-table + the first-grounding (FEJ) anchor map, is ticked
// by the engine across a run, calls control.Innovate, and emits the estimate.* events.
//
// Design: docs/internal/notes/2026-06-20-slam-M1-build-spec.md §3.2 + the parent
// docs/internal/notes/2026-06-20-slam-self-state-estimation.md §3.3/§3b (the Estimate envelope, P1/P2).
//
// THE ONE LOAD-BEARING INVARIANT (§0, the FEJ anchor + P2 consistency rule, in one sentence):
//
//	belief variance P is reduced ONLY by a grounded Observe(), NEVER by the model re-asserting the
//	belief (Note()).
//
// Note() updates the mean estimate but MUST NOT lower the variance — a self-restatement carries no new
// information about the world. Observe() is the ONLY place P shrinks. Trust() reads the FEJ anchor (the
// belief's FIRST grounding), not the latest self-reinforced restatement, so a confidently-restated-
// but-ungrounded belief stays high-variance and reality corrects it hard. Letting Note() shrink P
// reproduces the measured overconfidence (EKF gains spurious information in the unobservable
// direction) and the whole thing inverts.
package estimate

import (
	"math"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// BeliefID keys the per-belief side-table. M1 keys on the active line's tip-thought identity (the
// existing thought/belief identity used by the conscious stream); cross-belief correlation is M2.
type BeliefID string

// Anchor is the FEJ first-grounding record: the belief's mean + variance at the moment it was FIRST
// grounded by a real observation. The Filter's trust judgment reads THIS, not the latest restatement —
// the First-Estimates-Jacobian fix for the EKF-inconsistency overconfidence (P2).
type Anchor struct {
	Mean float64 // belief mean at first grounding
	Var  float64 // belief variance at first grounding (the floor the belief's certainty cannot beat — P1)
	Tick int     // the tick of first grounding (freshness anchor; staleness/decay is M4)
}

// Config holds the estimator's tunables.
type Config struct {
	Enabled   bool    // SLAMInnovation flag; when false the estimator is inert (Enabled() reports it)
	PriorVar0 float64 // the variance a NEW belief starts at (high = uncertain / self-derived)
	Chi2Gate  float64 // the Mahalanobis data-association threshold passed to control.Innovate
	// Monitor enables the SLAM M5 consistency/observability monitor (the slam.consistency knob): the
	// estimator ACCOUNTS every information gain (variance reduction) as grounded vs spurious and emits
	// estimate.consistency. It REQUIRES Enabled (the innovation update is what produces the variance
	// trajectory to monitor). Default OFF ⇒ no accounting, no event ⇒ byte-identical. The accounting is
	// a pure WITNESS — it never alters the estimate — so this is observability, not a behaviour change.
	Monitor bool
	// Covariance enables the SLAM M2 sparse-covariance / Information layer (the slam.covariance knob): the
	// estimator records WHICH beliefs co-vary (share a grounding upstream) and, on a grounded REFUTATION,
	// propagates a correlated loss-of-certainty (variance INFLATION) to the co-varying siblings — catching
	// CORRELATED self-deception no per-belief scalar can see (covariance.go). It REQUIRES Enabled (it
	// correlates the M1 variance trajectory). Default OFF ⇒ no correlation graph, no propagation, no
	// estimate.correlate event ⇒ byte-identical. A propagation only RAISES variance, so it stays inside
	// the §0/M5 consistency invariant (becoming less certain is never spurious information).
	Covariance bool
	// InfoGain enables the SLAM M6 active-inference info-gain layer (the slam.infogain knob): the estimator
	// RANKS the live tracked beliefs by expected JOINT information gain (active-SLAM next-best-observation —
	// what to verify next) over the M1 variances + the M2 correlation reach, and emits estimate.infogain.
	// It REQUIRES Enabled (it ranks the M1 variance trajectory). Default OFF ⇒ no ranking, no estimate.
	// infogain event ⇒ byte-identical. A PURE RANKING — it reads the side-table and never alters a variance,
	// so it stays inside the §0/M5 consistency invariant (it only DIRECTS the grounding that legitimately
	// shrinks a variance; it never shrinks one itself).
	InfoGain bool
	// Staleness enables the SLAM M4 freshness / staleness-decay layer (the slam.staleness knob): each tick
	// the estimator GROWS every grounded belief's variance back toward the prior ceiling as a function of
	// its un-refreshed AGE (control.StalenessInflation), modelling the process noise of a non-stationary
	// world (P4: Q>0). A belief grounded long ago decays toward "stale, re-observe" — forcing re-grounding
	// of a fact that may have moved since it was last observed. It REQUIRES Enabled (it decays the M1
	// variance trajectory). Default OFF ⇒ no decay sweep, no estimate.decay event ⇒ byte-identical. Decay
	// only RAISES variance (loses certainty), so it stays inside the §0/M5 consistency invariant (admitting
	// staleness can never be spurious information — the M5 accounting returns 0 for any variance growth).
	Staleness bool
	// StalenessQ is the per-tick process-noise rate in [0,1] the M4 decay uses (slam.staleness_q): the
	// FRACTION of the remaining gap to the prior ceiling a grounded belief loses per un-refreshed tick.
	// 0 = stationary (no decay even with Staleness on). DefaultConfig seeds a small slow-drift rate so a
	// belief stays usefully fresh for a few ticks but measurably decays over an idle stretch.
	StalenessQ float64
}

// DefaultConfig returns the M1 defaults: a high prior variance (a fresh belief is uncertain until
// reality grounds it) and a chi-square gate at 9.0 (~3 sigma for the scalar case — a refuting
// observation more than 3 standard deviations from the prior is treated as an association failure).
// The M5 consistency monitor, the M2 covariance layer + the M6 info-gain layer are OFF by default (each
// rides its own knob).
func DefaultConfig() Config {
	return Config{Enabled: false, PriorVar0: 1.0, Chi2Gate: 9.0, Monitor: false, Covariance: false, InfoGain: false, Staleness: false, StalenessQ: 0.08}
}

// Estimator is the ticked self-state estimator. Construct with New; it is NOT safe for concurrent use
// (the engine ticks it serially on the deterministic loop, like the regulator).
type Estimator struct {
	// varByID is the per-belief variance P. It is reduced ONLY in Observe() on a grounded obs (the §0
	// invariant). A belief absent from the map is at PriorVar0 (high) — see varOf.
	varByID map[BeliefID]float64
	// meanByID is the per-belief mean estimate (the LATEST stance/confidence the model stated). Updated
	// by Note() and Observe(); read by Trust() ONLY as a fallback before a first grounding exists.
	meanByID map[BeliefID]float64
	// firstGround is the FEJ anchor map: the belief's FIRST-grounding mean/var. Trust() reads THIS once
	// it exists, so a later self-reinforced restatement cannot lower the trusted variance.
	firstGround map[BeliefID]Anchor

	// cons is the SLAM M5 consistency/observability witness, accumulated across the run when the
	// slam.consistency monitor is on. It is a pure ACCOUNTING record (never read back into the estimate)
	// of how much information was gained from grounded observations vs. spuriously (in an unobservable
	// direction) — the failable check that the §0 invariant held over a long awake run.
	cons Consistency

	// cov is the SLAM M2 SPARSE correlation graph (covariance.go): which beliefs co-vary because they
	// share a grounding upstream. Lazily constructed on the first Link when the slam.covariance layer is
	// on; nil (and untouched) otherwise, so a covariance-OFF run is byte-identical to M1.
	cov *covGraph

	// lastObs is the SLAM M4 FRESHNESS index (staleness.go): the seeded loop tick at which each belief was
	// last GROUNDED by a real Observe(). The per-tick decay sweep computes age = curTick - lastObs and grows
	// the belief's variance toward the prior ceiling (P4 process noise). Stamped only on an associated
	// grounding (the §0 var-reducer); a belief absent here has never been grounded (it is already at the
	// high PriorVar0, so it has nothing to decay). Lazily populated; untouched when the M4 layer is off.
	lastObs map[BeliefID]int

	cfg     Config
	bus     events.Emit
	curTick int // the seeded loop tick (set by SetTick); deterministic, never the wall clock
}

// New builds an Estimator from a config + the event-bus emit closure (nil-safe: a nil bus disables
// emission). When cfg.Enabled is false the estimator is inert and the engine bypasses it (default OFF
// => byte-identical).
func New(cfg Config, bus events.Emit) *Estimator {
	if cfg.PriorVar0 <= 0 {
		cfg.PriorVar0 = 1.0
	}
	// Defend the M4 process-noise rate: a config that enabled Staleness but left StalenessQ at 0 (e.g. a
	// JSON load that omitted the field) would silently never decay. Seed the slow-drift default so the
	// layer-on case always decays; a deliberate 0 with Staleness OFF stays inert anyway (decaying() gates).
	if cfg.Staleness && cfg.StalenessQ <= 0 {
		cfg.StalenessQ = 0.08
	}
	if cfg.StalenessQ > 1 {
		cfg.StalenessQ = 1
	}
	return &Estimator{
		varByID:     map[BeliefID]float64{},
		meanByID:    map[BeliefID]float64{},
		firstGround: map[BeliefID]Anchor{},
		lastObs:     map[BeliefID]int{},
		cfg:         cfg,
		bus:         bus,
	}
}

// Enabled reports whether the estimator is active (the SLAMInnovation flag). The engine checks this at
// every call site so the OFF path is byte-identical.
func (e *Estimator) Enabled() bool { return e != nil && e.cfg.Enabled }

// SetEnabled honours a live config flip (the TUI's slam.innovation toggle): the estimator was built
// from the boot-time flag, so the engine syncs the live flag each tick. Flipping OFF leaves the
// accumulated side-table intact (a re-flip-ON resumes from it); flipping ON makes the next grounded
// observation start folding in. Nil-safe.
func (e *Estimator) SetEnabled(on bool) {
	if e == nil {
		return
	}
	e.cfg.Enabled = on
}

// SetMonitor honours a live flip of the slam.consistency knob (the M5 consistency monitor). The monitor
// only does anything when the estimator is also Enabled (it observes the innovation update's variance
// trajectory); flipping it OFF freezes the accumulated witness (a re-flip-ON resumes accruing). Nil-safe.
func (e *Estimator) SetMonitor(on bool) {
	if e == nil {
		return
	}
	e.cfg.Monitor = on
}

// monitoring reports whether the M5 consistency monitor should account this write (the estimator is
// active AND the slam.consistency knob is on). When false, the accounting paths are skipped entirely so
// a monitor-OFF run does exactly the M1 work.
func (e *Estimator) monitoring() bool { return e.Enabled() && e.cfg.Monitor }

// varOf returns the belief's current variance, defaulting a never-seen belief to the high PriorVar0.
func (e *Estimator) varOf(id BeliefID) float64 {
	if v, ok := e.varByID[id]; ok {
		return v
	}
	return e.cfg.PriorVar0
}

// Note records that the model (re)STATED a belief: it updates the MEAN estimate but MUST NOT lower the
// variance (§0 — a self-restatement carries no new information). A brand-new belief is seeded at
// PriorVar0 (high); an existing belief keeps its variance untouched. This is the structural antidote
// to "EKF gains spurious information from re-linearizing at the current estimate" (P2 overconfidence).
//
// It is intentionally a no-op when the estimator is disabled, so the engine can call it unconditionally
// on the live loop without a flag check at every site.
func (e *Estimator) Note(id BeliefID, mean float64) {
	if !e.Enabled() {
		return
	}
	// M5 consistency witness: a self-restatement must gain NO information. Snapshot the variance before
	// the write so the monitor can attribute any reduction as SPURIOUS (information gained in an
	// unobservable direction — the Huang-2010 inconsistency). In structurally-sound code this is always
	// 0; the snapshot is what makes a regression (a Note() that lowered P) failable instead of silent.
	monitor := e.monitoring()
	var before float64
	if monitor {
		before = e.varOf(id)
		e.cons.Notes++
	}
	if _, seen := e.varByID[id]; !seen {
		e.varByID[id] = e.cfg.PriorVar0 // a new belief starts uncertain
	}
	// The mean estimate follows the latest statement; the variance is LEFT EXACTLY AS IT WAS (the
	// invariant). varByID is never written with a smaller value here — only Observe() may shrink it.
	e.meanByID[id] = mean
	if monitor {
		// A self-restatement that LOWERED the variance is spurious information (it cannot have come from
		// an observation). before is the pre-write variance; for a brand-new belief it is PriorVar0 and the
		// write seeds the same PriorVar0 (no reduction). gain>0 ⇒ the §0 invariant was violated here.
		if gain := infoGain(before, e.varOf(id)); gain > consistencyEpsilon {
			e.cons.SpuriousGain += gain
			e.cons.Violations++
			e.emitConsistency("self-restatement gained information (id="+string(id)+")", false)
		}
	}
}

// Observe folds a GROUNDED observation into the belief — the measurement update, and the ONLY place
// variance shrinks. obs is +1 (reality CONFIRMS the belief) or -1 (reality REFUTES it); obsPrec is the
// trust-tier precision R^-1 (use control.TierPrecision(tierOrdinal)). It runs the scalar Kalman step
// (control.Innovate), writes back the posterior mean+var, records the FEJ anchor on FIRST grounding,
// and emits estimate.innovate + estimate.correct (+ estimate.gate when the Mahalanobis gate rejects).
// Returns the full Residual so the caller can attach it to the Observation for downstream/A-B
// legibility. A disabled estimator returns the zero Residual and emits nothing.
func (e *Estimator) Observe(id BeliefID, obs, obsPrec float64) control.Residual {
	if !e.Enabled() {
		return control.Residual{}
	}
	priorMean := e.meanByID[id] // 0 if the belief was never Note()d (a bare observation)
	priorVar := e.varOf(id)
	r := control.Innovate(priorMean, priorVar, obs, obsPrec, e.cfg.Chi2Gate)

	// The graded correction the static -0.45 penalty becomes — for A/B legibility on the event.
	deltaFromStatic := r.PostMean - priorMean - (-0.45)

	e.emit(events.EstimateInnovate, "innovate "+string(id), events.D{
		"id":        string(id),
		"priorMean": round3(priorMean),
		"priorVar":  round3(priorVar),
		"obs":       round3(obs),
		"obsPrec":   round3(obsPrec),
		"innov":     round3(r.Innov),
		"innovVar":  round3(r.InnovVar),
		"gain":      round3(r.Gain),
	})

	monitor := e.monitoring()
	if monitor {
		e.cons.Observations++
	}

	if r.Gated {
		// Data-association failure: the obs is too far from the prior to be about this belief. Do NOT
		// fold it in (prior unchanged) — emit the gate event and return without touching the side-table.
		e.emit(events.EstimateGate, "gate(reject) "+string(id), events.D{
			"id":          string(id),
			"mahalanobis": round3((r.Innov * r.Innov) / r.InnovVar),
			"chi2Gate":    round3(e.cfg.Chi2Gate),
			"gated":       true,
		})
		if monitor {
			e.cons.Gated++
			// A GATED observation is unassociated — it carries no information ABOUT THIS belief, so it must
			// not shrink P. control.Innovate returns the prior var on a gate (PostVar == priorVar), so this
			// is 0 in sound code; the check catches a regression that let a gated obs reduce P.
			if gain := infoGain(priorVar, r.PostVar); gain > consistencyEpsilon {
				e.cons.SpuriousGain += gain
				e.cons.Violations++
				e.emitConsistency("gated observation gained information (id="+string(id)+")", false)
			}
		}
		return r
	}

	// Associated: write back the posterior. THIS is the only var-reducer.
	e.meanByID[id] = r.PostMean
	e.varByID[id] = r.PostVar
	// SLAM M4 (Track F): stamp the freshness index — this belief was just grounded at the current tick, so
	// its staleness clock resets to zero (age 0 = fresh, no decay). The per-tick Decay() sweep reads this.
	// Stamped on EVERY associated grounding (re-observation refreshes), unconditionally cheap; the decay
	// sweep itself is the gated part, so a staleness-OFF run only pays this one map write (which never
	// emits and never alters a variance) — the OFF path stays byte-identical.
	if e.lastObs != nil {
		e.lastObs[id] = e.tick()
	}
	if monitor {
		// The legitimate (observable) information: variance shrank because reality measured the belief.
		// This is the ONLY path that may add to GroundedGain.
		e.cons.GroundedGain += infoGain(priorVar, r.PostVar)
	}
	// FEJ anchor: stamp the FIRST grounding only. Trust() reads this thereafter, so a later
	// self-reinforced restatement cannot lower the trusted variance (P2). We anchor at the POSTERIOR
	// of the first grounding — the belief's first reality-derived estimate.
	if _, anchored := e.firstGround[id]; !anchored {
		e.firstGround[id] = Anchor{Mean: r.PostMean, Var: r.PostVar, Tick: e.tick()}
	}

	e.emit(events.EstimateCorrect, "correct "+string(id), events.D{
		"id":              string(id),
		"postMean":        round3(r.PostMean),
		"postVar":         round3(r.PostVar),
		"deltaFromStatic": round3(deltaFromStatic),
	})
	return r
}

// Trust is the FEJ-anchored read for the Filter / value confidence: it returns the belief's FIRST-
// grounding mean+var when one exists (the FEJ anchor — robust to self-reinforced restatement), else
// the latest stated mean + its current variance. A belief with no grounding yet has high variance
// (PriorVar0), which is exactly "I asserted this confidently but reality has not confirmed it" —
// the calibration signal the Filter uses to distrust a laundered hallucination.
func (e *Estimator) Trust(id BeliefID) (mean, varr float64) {
	if !e.Enabled() {
		return 0, 0
	}
	if a, ok := e.firstGround[id]; ok {
		return a.Mean, a.Var
	}
	return e.meanByID[id], e.varOf(id)
}

// Grounded reports whether the belief has ever been grounded by a real observation (a FEJ anchor
// exists). Used by the calibration-vitals readout (how many beliefs are reality-anchored vs
// self-derived).
func (e *Estimator) Grounded(id BeliefID) bool {
	if !e.Enabled() {
		return false
	}
	_, ok := e.firstGround[id]
	return ok
}

// Vitals is the compact calibration readout for the Ctrl+O runtime monitor (Tier-2 observability):
// how many beliefs are tracked, how many are reality-grounded (have a FEJ anchor), and the mean
// variance across tracked beliefs (high = mostly self-derived, low = mostly grounded/calibrated).
func (e *Estimator) Vitals() (beliefs, grounded int, meanVar float64) {
	if !e.Enabled() {
		return 0, 0, 0
	}
	beliefs = len(e.varByID)
	grounded = len(e.firstGround)
	if beliefs == 0 {
		return beliefs, grounded, 0
	}
	var sum float64
	for _, v := range e.varByID {
		sum += v
	}
	return beliefs, grounded, round3(sum / float64(beliefs))
}

// ConsistencyState returns the M5 consistency/observability witness accumulated over the run: the
// grounded vs spurious information gain, the write counts, and whether the invariant held. Returns the
// zero Consistency (vacuously consistent) when the monitor is off. Read-only — the durability gate and
// the runtime monitor read this; it never feeds back into the estimate.
func (e *Estimator) ConsistencyState() Consistency {
	if e == nil || !e.cfg.Monitor {
		return Consistency{}
	}
	return e.cons
}

// CheckConsistency emits the periodic estimate.consistency witness (the NEES-style consistency summary)
// and returns whether the invariant held so far. The engine calls it on a representative cadence (e.g.
// each tick of the awake loop / at quiescence) so the runtime monitor and the trace show, live, that the
// estimator is not gaining spurious information. A no-op (returns true, emits nothing) when the monitor
// is off. The summary is rendered ONLY when there has been at least one write since boot, so a quiescent
// run does not spam the bus with empty witnesses.
func (e *Estimator) CheckConsistency() bool {
	if !e.monitoring() {
		return true
	}
	if e.cons.Notes == 0 && e.cons.Observations == 0 {
		return true // nothing happened yet — no witness to emit
	}
	consistent := e.cons.Consistent()
	summary := "consistent"
	if !consistent {
		summary = "INCONSISTENT (spurious info gained)"
	}
	e.emitConsistency("estimator "+summary, consistent)
	return consistent
}

// emitConsistency renders one estimate.consistency event carrying the full M5 witness payload. summary
// is the human-readable head; consistent is the verdict at this point. The payload is what the durability
// gate, the trace, and the Ctrl+O monitor read.
func (e *Estimator) emitConsistency(summary string, consistent bool) {
	c := e.cons
	e.emit(events.EstimateConsistency, summary, events.D{
		"groundedGain":     round3(c.GroundedGain),
		"spuriousGain":     round3(c.SpuriousGain),
		"groundedFraction": round3(c.GroundedFraction()),
		"notes":            c.Notes,
		"observations":     c.Observations,
		"gated":            c.Gated,
		"violations":       c.Violations,
		"consistent":       consistent,
	})
}

// SetTick records the current loop tick so the FEJ anchor's freshness stamp is the real observation
// tick rather than 0. The engine calls this each tick (cheap, no allocation). Kept off the constructor
// so the estimator stays clock-free: the tick is the seeded loop tick, never the wall clock.
func (e *Estimator) SetTick(tick int) {
	if e == nil {
		return
	}
	e.curTick = tick
}

func (e *Estimator) tick() int { return e.curTick }

func (e *Estimator) emit(kind, summary string, d events.D) {
	if e.bus == nil {
		return
	}
	e.bus(kind, summary, d)
}

// round3 rounds to 3 decimals for the wire payload (display-only; the math uses the raw values).
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// FromThoughtID builds the per-belief side-table key from a conscious thought id. M1 keys the
// estimator on the active line's tip-thought identity; this is the ONE place that mapping lives so a
// later milestone can change the belief identity without touching the engine call sites.
func FromThoughtID(id int) BeliefID { return BeliefID("t" + strconv.Itoa(id)) }
