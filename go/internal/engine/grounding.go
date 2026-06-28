package engine

import (
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/estimate"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/grounding"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// groundObservation feeds a watched-seam observation into the reality-grounding spine (SR-4 / N.1a):
// a REAL observation grounds (ok) or refutes (!ok) the claim it bears on at the firsthand-observation
// tier; a FABRICATED tier-0 observation (the offline heuristic stand-in) is REJECTED — fake reality can
// never ground, so the loop can't be turned into a hallucination amplifier. When a real observation is
// ingested, grounding.ground is emitted so the loop is observable (claim · verdict · tier · epistemic
// status). On the heuristic backend every act is fabricated, so nothing is ingested and the event
// stream is unchanged (the scenario goldens hold for free).
func (e *Engine) groundObservation(intention types.Intention, obs types.Thought) {
	raw, ok := obs.RawReturn.(types.Observation)
	if !ok {
		return
	}
	// record HOW reality was reached (N.4 bridge: structured|scraped|none) and whether it was a tier-0
	// FABRICATION (P0.6) — surfaced in the action panel so a faked reality is visible, even though the
	// grounding loop below already rejects it (fabricated reality can never ground).
	e.lastBridge, e.lastFabricated = raw.Bridge, raw.Fabricated
	claim := intention.Claim
	if claim == "" {
		claim = intention.Text
	}
	if !e.grounding.IngestObservation(claim, raw.Ok, raw.Fabricated, e.bus.Tick) {
		return // fabricated / no claim — never grounds (and never emits)
	}
	verdict := "grounded"
	if !raw.Ok {
		verdict = "refuted"
	}
	exp, _ := e.grounding.Recall(claim)
	e.bus.Emit(events.Ground, "grounding: "+runeSlice(claim, 60)+" -> "+verdict, events.D{
		"claim":   claim,
		"verdict": verdict,
		"tier":    exp.Tier.String(),
		"status":  e.grounding.Status(claim).String(),
		"method":  "observation",
		"bridge":  raw.Bridge,
	})

	// SLAM self-state measurement update (Track F / M1): the real observation just grounded/refuted the
	// active line's belief — fold it in as an EXPLICIT innovation/residual (the scalar Kalman update),
	// instead of the implicit, model-mediated comparison. INERT unless the opt-in slam.innovation knob
	// is ON, so the OFF path adds zero estimator calls and the loop is byte-identical.
	e.slamObserve(raw.Ok, exp.Tier)
}

// slamObserve folds one grounded observation into the SLAM self-state estimator (the action->reality
// measurement update, Track F / M1). The belief is the active line's TIP thought (M1 keys per-belief
// on the tip-thought id; cross-belief correlation is M2). It Notes the prior belief mean from the
// tip's confidence (mapped to the [-1,1] stance axis — a confident assertion is +conf), then Observes
// the grounded outcome: +1 when reality CONFIRMS (ok), -1 when it REFUTES (!ok), weighted by the
// trust-tier precision R^-1. The estimator emits estimate.innovate/correct (+ estimate.gate on a
// Mahalanobis reject). No-op when the knob is OFF or there is no active tip.
//
// SLAM M9 (Track F): when slam.calibration is ALSO on, the precision is the LEARNED R from the
// calibration meta-estimator (control.TierPrecision scaled by the tier's measured reliability) instead
// of the fixed prior, and the residual the estimator returns is fed BACK to the calibrator so it learns
// each source's reliability from the predicted-vs-actual outcomes — the lever on the measured same-model
// ceiling. When calibration is off (the default), learnedPrecision returns the fixed prior exactly, so
// the M1 behaviour is byte-identical.
func (e *Engine) slamObserve(ok bool, tier grounding.TrustTier) {
	if !e.slamInnovationEnabled() || e.graph == nil {
		return
	}
	tip := e.graph.Last()
	if tip == nil {
		return
	}
	id := estimate.FromThoughtID(tip.ID)
	// SLAM M2 (Track F): record the tip belief's grounding UPSTREAMS — the ancestor thoughts it was
	// derived from in the active line. Two tip-beliefs grounded under the same lineage prefix SHARE these
	// upstreams and therefore CO-VARY (their errors share the common conscious state that first grounded
	// them — the SLAM non-factorization result). This builds the SPARSE correlation graph; it changes NO
	// estimate, and is a no-op (no graph walk) when the slam.covariance layer is off (byte-identical).
	// Bounded by the active line's depth (the Controller's ExhaustAfter caps it), so no fan-out.
	e.slamLinkUpstreams(id, tip.ID)
	// Prior mean: the belief's asserted stance toward its claim. The tip ASSERTED the claim, so a
	// confident tip is a confident +stance; confidence in [0,1] maps to a mean in [0,1] on the stance
	// axis (the §0 invariant means a confidently-stated-but-ungrounded belief keeps HIGH variance, so
	// reality corrects it hard — the calibration-not-caution win).
	e.estimator.Note(id, tip.Confidence)
	obs := -1.0 // reality refutes the asserted belief
	if ok {
		obs = 1.0 // reality confirms it
	}
	// M9: the precision is the calibrator's LEARNED R for this tier (the fixed prior when calibration is
	// off or the tier is under-sampled) — "learn R per source/tier".
	prec := e.calibrator.LearnedPrecision(int(tier))
	r := e.estimator.Observe(id, obs, prec)
	// M9: feed the residual back to the calibration meta-estimator so it updates this tier's reliability
	// from the predicted-vs-actual outcome. INERT (no-op, no event) unless slam.calibration is on.
	e.calibrator.Observe(int(tier), r)
	// SLAM M2 (Track F): when reality REFUTED the belief (and the obs was associated, not Mahalanobis-
	// gated), propagate a correlated loss-of-certainty to its CO-VARYING siblings (the beliefs sharing an
	// upstream) — their variance INFLATES because the shared grounding that backed them just proved
	// unreliable. This catches CORRELATED self-deception (two beliefs confidently wrong because one bad
	// upstream) that no per-belief scalar can see. A propagation only RAISES variance, so it stays inside
	// the §0/M5 consistency invariant. No-op (no event) when the obs CONFIRMED, the obs was gated, the
	// covariance layer is off, or there are no siblings (byte-identical).
	if !ok && !r.Gated {
		e.estimator.PropagateRefutation(id, absFloat(r.Innov))
	}
}

// slamLinkUpstreams registers the tip belief's grounding ancestors (the SLAM M2 sparse-covariance
// Information layer). It walks the active line's lineage root-first to the tip and links the tip belief
// to each ancestor thought id — so two tip-beliefs grounded under the same lineage prefix become
// correlated through their shared early conscious state. No-op (and no graph walk) unless the
// slam.covariance layer is active, so the OFF path adds zero work and is byte-identical.
func (e *Engine) slamLinkUpstreams(id estimate.BeliefID, tipID int) {
	if !e.estimator.Correlating() || e.graph == nil {
		return
	}
	tid := tipID
	for _, anc := range e.graph.ReconstructPath(&tid) {
		if anc.ID == tipID {
			continue // a belief does not co-vary with itself
		}
		e.estimator.Link(id, estimate.FromUpstreamThoughtID(anc.ID))
	}
}

// slamNextBestObservation is the SLAM M6 active-inference wire (Track F / M6): once V over branches is
// fresh and BEFORE the Controller decides what to do, it asks the estimator which belief — of all the
// ones the harness currently holds — would reduce the most JOINT uncertainty if grounded next (the
// active-SLAM next-best-observation / what to verify next). This is the directed-grounding signal the
// design names: it ranks by expected uncertainty reduction (a high-variance belief that many siblings
// co-vary with), not just outcome reward, targeting the measured under-grounding / give-up behaviour.
// The ranking precision is the GOLD trust tier (FirsthandValidated) — the question is "if I took this
// belief to reality, how much would I learn", so it scores against the best observation the harness
// could make.
//
// It is a PURE RANKING signal: it reads the variance trajectory and emits estimate.infogain (the
// next-best target is now visible on the bus / in the trace / TUI), and NEVER alters a belief — so it
// stays inside the §0/M5 consistency invariant (it directs grounding; it never fabricates certainty).
// No-op (no ranking, no event) unless the slam.infogain layer is active (which requires slam.innovation),
// so the OFF path adds zero work and the live loop is byte-identical. Called from reason() in BOTH the
// reactive and continuous loops (the awake default-mode generator is M6's natural curiosity home).
func (e *Engine) slamNextBestObservation() {
	if !e.estimator.Exploring() || e.graph == nil {
		return
	}
	// Make the active line's CURRENT tip belief known to the estimator before ranking, so a belief the
	// harness is actively thinking about but has NOT yet grounded is a ranking candidate — the whole point
	// of "verify what you do NOT yet know". This is a Note (a self-restatement): by the §0 invariant it
	// updates the belief's mean but NEVER lowers its variance, so a never-grounded tip stays at the high
	// PriorVar0 (maximally worth grounding) and the M5 consistency witness is untouched. A belief already
	// grounded keeps its (low) grounded variance — Note cannot fabricate certainty.
	if tip := e.graph.Last(); tip != nil {
		e.estimator.Note(estimate.FromThoughtID(tip.ID), tip.Confidence)
	}
	// The precision a gold-tier (firsthand-validated) reality observation would carry — the most a single
	// grounding can teach. Ranking against it asks "which belief gains the most from being taken to reality".
	e.estimator.NextBestObservation(control.TierPrecision(int(grounding.TierFirsthandValidated)))
}

// absFloat is |x| (the magnitude of the innovation passed to the correlated propagation). Kept local to
// the engine package to avoid a math import in this control-only wiring file.
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// groundClaim consults the reality-grounding spine on a VOICED thought (the Filter/Controller side of
// SR-4, N.1): when the harness asserts a COMPUTABLE claim, the deterministic compute layer grounds or
// refutes it offline — no model, no ACT, "math doesn't lie" — and grounding.ground is emitted at the
// deterministic tier. This is what catches a confident arithmetic hallucination ("2+2=5") the moment it
// is voiced, and what makes the grounding ledger live on the offline path (where every ACT is fabricated
// and never grounds). A non-computable thought defers silently — never fabricated, never emitted — so
// only genuinely checkable assertions reach the ledger. A claim already validated is reused (the spine
// never re-runs a settled experiment), so the ledger holds one row per distinct claim, not one per tick.
func (e *Engine) groundClaim(text string) {
	res := grounding.EvaluateCompute(text)
	if res.Verdict == grounding.NotComputable || res.Claim == "" {
		return
	}
	if _, reused := e.grounding.Ground(res.Claim, e.bus.Tick); reused {
		return // already in the ledger — the spine doesn't re-run a settled experiment
	}
	exp, _ := e.grounding.Recall(res.Claim)
	e.bus.Emit(events.Ground, "grounding: "+runeSlice(res.Claim, 60)+" -> "+res.Verdict.String(), events.D{
		"claim":   res.Claim,
		"verdict": res.Verdict.String(),
		"tier":    exp.Tier.String(),
		"status":  e.grounding.Status(res.Claim).String(),
		"method":  "compute",
		"bridge":  "",
	})
}

// groundSensors runs the standing re-grounding step (N.1a-cont / AR-6): poll every registered sensor
// and ingest its percepts, so a hallucination arising BETWEEN acts is refuted the moment a watcher
// sees the contradiction — no ACT required. Called on the awake/continuous tick. Each percept that
// changes the epistemic standing of a claim emits grounding.percept. No sensors (the default, incl.
// every heuristic run) => no-op, goldens unchanged.
func (e *Engine) groundSensors() {
	for _, s := range e.sensors {
		before := e.grounding.Len()
		n := e.grounding.IngestSensor(s, e.bus.Tick)
		if n == 0 || e.grounding.Len() == before {
			continue
		}
		e.bus.Emit(events.Percept, "sensor re-grounded "+itoa(n)+" claim(s)", events.D{
			"count":  n,
			"sensor": sensorName(s),
		})
	}
}

// sensorName returns a stable label for a sensor for the event payload (the Sensor interface carries
// no name; the scripted test double and any real watcher are identified structurally).
func sensorName(s grounding.Sensor) string {
	if n, ok := s.(interface{ Name() string }); ok {
		return n.Name()
	}
	return "sensor"
}
