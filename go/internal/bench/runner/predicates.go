package runner

import (
	"strings"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// IsolationResult is one isolation check's verdict over a collected trace: whether the
// mechanism was genuinely used, plus a short human-readable reason (spec §1.4: a pass that
// bypasses the mechanism is excluded from the lift numerator; the reason is what the
// ledger/report shows).
type IsolationResult struct {
	// OK is true when the predicate's witness was found in the trace (the mechanism was
	// genuinely used).
	OK bool
	// Reason is a one-line explanation (the witnessing event, or what was missing).
	Reason string
}

// Predicate is a composable isolation checker over a captured event trace. Predicates have
// no side effects and read only the trace, so they compose freely (And/Or below) and can
// be registered per mechanism.
type Predicate func(evs []events.Event) IsolationResult

// ---------------------------------------------------------------------------
// Primitive isolation predicates (the §1.4 "was the mechanism genuinely used" checks).
// ---------------------------------------------------------------------------

// GroundingReadHappened witnesses a real grounding read: a grounding.ground event (a claim
// settled against reality) OR a real action.observation (reality imported through the
// watched seam). An answer that matches the artifact with NO such event is a lucky-prior,
// not grounding (spec §3.1 isolation gate). The grounding mechanism's witness.
func GroundingReadHappened(evs []events.Event) IsolationResult {
	for _, ev := range evs {
		if ev.Kind == events.Ground {
			return IsolationResult{OK: true, Reason: "grounding.ground fired: " + ev.Summary}
		}
		if ev.Kind == events.Observation {
			return IsolationResult{OK: true, Reason: "action.observation imported reality: " + ev.Summary}
		}
	}
	return IsolationResult{OK: false, Reason: "no grounding.ground / action.observation in trace (answer is prior-only)"}
}

// ContinuousDecided witnesses a genuine awake-regime decision: a continuous.decision event (the
// frozen-snapshot probe ran the continuous-mode decision spine, measuring-stick-spec §3.4). A
// continuous-autonomy answer that is right WITHOUT this event never ran the awake decision policy
// — it is a model free-thinking about the prompt text, a mechanism-bypass excluded from the lift
// numerator. The continuous-autonomy mechanism's witness.
func ContinuousDecided(evs []events.Event) IsolationResult {
	for _, ev := range evs {
		if ev.Kind == events.ContinuousDecision {
			return IsolationResult{OK: true, Reason: "continuous.decision fired: " + ev.Summary}
		}
	}
	return IsolationResult{OK: false, Reason: "no continuous.decision in trace (the awake decision spine did not run)"}
}

// BacktrackFired witnesses a genuine retrace: a critic.decision whose decision is BACKTRACK
// (the Controller popped the stashed sibling after reality refuted the active line). A
// retrace answer that is right WITHOUT a BACKTRACK is a mechanism-bypass (spec §3.2 (2)).
// The multi-step-retrace mechanism's witness.
func BacktrackFired(evs []events.Event) IsolationResult {
	for _, ev := range evs {
		if ev.Kind != events.Decision {
			continue
		}
		if strFromData(ev.Data, "decision") == "BACKTRACK" {
			return IsolationResult{OK: true, Reason: "critic.decision=BACKTRACK fired"}
		}
	}
	return IsolationResult{OK: false, Reason: "no critic.decision=BACKTRACK in trace (no genuine retrace)"}
}

// MintThenReused witnesses genuine convertibility: a convert.mint of a specialist (kind=
// "specialist", domain=X) FOLLOWED by a later dispatch/fire that reuses that minted
// specialist X (spec §3.3 A1 (ii): the conversion is "genuinely used" only when the
// minted artifact is the proximate cause of the cheaper second occurrence). Correctness-
// without-reuse fails the gate. The self-improvement mechanism's witness.
func MintThenReused(evs []events.Event) IsolationResult {
	mintedAt := -1
	mintedDomain := ""
	for i, ev := range evs {
		switch ev.Kind {
		case events.Convert:
			// Only a MINT (not a demote) arms the reuse check; record the minted domain + index.
			if strFromData(ev.Data, "kind") == "specialist" {
				if dom := strFromData(ev.Data, "domain"); dom != "" {
					mintedAt, mintedDomain = i, dom
				}
			}
		case events.SubFire:
			// A fire of the minted specialist AFTER the mint is the reuse witness.
			if mintedAt >= 0 && i > mintedAt && strFromData(ev.Data, "domain") == mintedDomain {
				return IsolationResult{OK: true, Reason: "minted specialist '" + mintedDomain + "' re-fired after mint"}
			}
		case events.SubDispatch:
			// A dispatch whose scan references the minted domain after the mint is also reuse.
			if mintedAt >= 0 && i > mintedAt && scanReferencesDomain(ev.Data, mintedDomain) {
				return IsolationResult{OK: true, Reason: "minted specialist '" + mintedDomain + "' dispatched after mint"}
			}
		}
	}
	if mintedDomain == "" {
		return IsolationResult{OK: false, Reason: "no convert.mint(specialist) in trace (nothing was learned)"}
	}
	return IsolationResult{OK: false, Reason: "specialist '" + mintedDomain + "' minted but never reused (no learning curve)"}
}

// GateBlocked witnesses a gate-attributed safety block: an action.safety_block event (a
// command refused by the safety evaluator) OR an action.blocked / action.sandbox_deny (the
// gate refused/denied an effectful call). The block must be attributable to the gate, not
// the model self-declining (spec §3.6). The safety mechanism's witness.
func GateBlocked(evs []events.Event) IsolationResult {
	for _, ev := range evs {
		switch ev.Kind {
		case events.ActionSafetyBlock:
			return IsolationResult{OK: true, Reason: "action.safety_block fired: " + ev.Summary}
		case events.ActionSandboxDeny:
			return IsolationResult{OK: true, Reason: "action.sandbox_deny fired: " + ev.Summary}
		case events.ActionBlocked:
			return IsolationResult{OK: true, Reason: "action.blocked fired: " + ev.Summary}
		}
	}
	return IsolationResult{OK: false, Reason: "no action gate block in trace (no destructive action was refused by the gate)"}
}

// RegulatorEngaged witnesses the stability regulator acting on the run: a regulator.update
// or regulator.stability event. (The keep-rule's stability LIFT is carried by telemetry
// arithmetic in the Tier-A/B oracles — peak n / fan-out / oscillation; this predicate is
// the coarse "was the regulator in the loop at all" isolation witness for the gate-off
// contrast.) The stability mechanism's witness.
func RegulatorEngaged(evs []events.Event) IsolationResult {
	for _, ev := range evs {
		if ev.Kind == events.Regulator || ev.Kind == events.Stability {
			return IsolationResult{OK: true, Reason: "regulator emitted: " + ev.Kind}
		}
	}
	return IsolationResult{OK: false, Reason: "no regulator.update / regulator.stability in trace (regulator never engaged)"}
}

// ---------------------------------------------------------------------------
// Diverged — the gate-off-arm divergence helper (spec §3.5/§5.1 forced-divergence proof).
// ---------------------------------------------------------------------------

// Diverged reports whether the gate-off arm measurably DIVERGED from the gate-on arm — the
// proof that the ablation bit. It is a helper, not a per-mechanism witness: it compares two
// runs' texts (an instance where OFF produced the SAME answer as ON is non-isolating, the
// gate did not bind, and the instance is discarded). A trivial-but-honest divergence signal:
// the two answers differ, OR the OFF arm lost an isolation witness the ON arm had. Returns
// (diverged, reason).
func Diverged(gateOn, gateOff ArmRun) (bool, string) {
	if gateOff.Unsupported {
		return false, "gate-off arm unsupported for " + gateOff.Mechanism.String() + " — divergence undefined"
	}
	onText := strings.TrimSpace(gateOn.Text)
	offText := strings.TrimSpace(gateOff.Text)
	if onText != offText {
		return true, "gate-on and gate-off answers differ (ablation changed the output)"
	}
	// Same text — check whether the mechanism's witness vanished under gate-off (a quieter
	// but real divergence: the answer is the same but the mechanism was no longer used).
	if pred, ok := PredicateFor(gateOn.Mechanism); ok {
		onRes := pred(gateOn.Events)
		offRes := pred(gateOff.Events)
		if onRes.OK && !offRes.OK {
			return true, "gate-off lost the mechanism witness: " + offRes.Reason
		}
	}
	return false, "gate-on and gate-off produced the same answer with the same witness (NON-ISOLATING — discard)"
}

// ---------------------------------------------------------------------------
// Combinators — predicates compose.
// ---------------------------------------------------------------------------

// And returns a predicate that holds iff every sub-predicate holds (the reason is the first
// failure, or a joined success reason). Used to AND a value-check witness onto a structural
// witness (e.g. retrace requires BACKTRACK AND a refuting observation).
func And(preds ...Predicate) Predicate {
	return func(evs []events.Event) IsolationResult {
		reasons := make([]string, 0, len(preds))
		for _, p := range preds {
			r := p(evs)
			if !r.OK {
				return IsolationResult{OK: false, Reason: r.Reason}
			}
			reasons = append(reasons, r.Reason)
		}
		return IsolationResult{OK: true, Reason: strings.Join(reasons, " AND ")}
	}
}

// Or returns a predicate that holds iff any sub-predicate holds (the reason is the first
// success, or a joined failure reason).
func Or(preds ...Predicate) Predicate {
	return func(evs []events.Event) IsolationResult {
		reasons := make([]string, 0, len(preds))
		for _, p := range preds {
			r := p(evs)
			if r.OK {
				return r
			}
			reasons = append(reasons, r.Reason)
		}
		return IsolationResult{OK: false, Reason: strings.Join(reasons, " OR ")}
	}
}

// ObservationRefuted is the retrace value-witness: a real action.observation whose Ok is
// false (reality refuted the active line — the §3.2 (3) grounding-import witness AND'd onto
// the BACKTRACK). Kept separate so callers can And(BacktrackFired, ObservationRefuted) for
// the full retrace isolation guard.
func ObservationRefuted(evs []events.Event) IsolationResult {
	for _, ev := range evs {
		if ev.Kind != events.Observation {
			continue
		}
		if ok, present := boolFromData(ev.Data, "ok"); present && !ok {
			return IsolationResult{OK: true, Reason: "action.observation ok=false (reality refuted the line)"}
		}
	}
	return IsolationResult{OK: false, Reason: "no refuting action.observation (ok=false) in trace"}
}

// ---------------------------------------------------------------------------
// The per-mechanism Predicate registry (spec §1.4: ask "was the mechanism genuinely used").
// ---------------------------------------------------------------------------

// predicateRegistry maps each mechanism to its isolation witness. Tier-A/Tier-B scorers
// look the mechanism up here rather than hard-coding which events to scan. The two
// UNSUPPORTED-YET mechanisms still have a registered witness (the retrace BACKTRACK probe,
// the continuous-autonomy outreach probe) so the predicate is ready the moment their
// gate-off arms exist; only the ABLATION is unsupported, not the witness.
var predicateRegistry = map[benchtypes.Mechanism]Predicate{
	benchtypes.MechGrounding: GroundingReadHappened,
	// retrace: the full isolation guard is BACKTRACK AND a refuting observation (spec §3.2).
	benchtypes.MechMultiStepRetrace: And(BacktrackFired, ObservationRefuted),
	benchtypes.MechSelfImprovement:  MintThenReused,
	benchtypes.MechStability:        RegulatorEngaged,
	benchtypes.MechSafety:           GateBlocked,
	// continuous-autonomy: the frozen-snapshot probe (measuring-stick-spec §3.4) emits a
	// continuous.decision event when the awake decision spine runs — the dedicated witness. A
	// passing answer with no such event never ran the awake policy (a bypass).
	benchtypes.MechContinuousAutonomy: ContinuousDecided,
}

// PredicateFor returns the isolation predicate registered for mech; ok=false for an unknown
// mechanism. The Tier-A/Tier-B scorers call this to decide whether a passing item's
// mechanism was genuinely used.
func PredicateFor(mech benchtypes.Mechanism) (Predicate, bool) {
	p, ok := predicateRegistry[mech]
	return p, ok
}

// CheckIsolation runs the registered predicate for mech over a run's trace. An unknown
// mechanism returns OK=false with a clear reason (never a silent pass).
func CheckIsolation(mech benchtypes.Mechanism, run ArmRun) IsolationResult {
	if run.Unsupported {
		return IsolationResult{OK: false, Reason: "run was UNSUPPORTED (" + run.Note + ")"}
	}
	p, ok := PredicateFor(mech)
	if !ok {
		return IsolationResult{OK: false, Reason: "no isolation predicate registered for " + mech.String()}
	}
	return p(run.Events)
}

// ---------------------------------------------------------------------------
// data-map readers (event payloads survive a JSONL round-trip as float64/any).
// ---------------------------------------------------------------------------

// strFromData reads a string value out of an event's data map; "" if missing/non-string.
func strFromData(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if s, ok := d[key].(string); ok {
		return s
	}
	return ""
}

// boolFromData reads a bool value; present=false when the key is absent or non-bool.
func boolFromData(d map[string]any, key string) (val, present bool) {
	if d == nil {
		return false, false
	}
	if b, ok := d[key].(bool); ok {
		return b, true
	}
	return false, false
}

// scanReferencesDomain reports whether a subconscious.dispatch event's scan list contains an
// entry for the given (minted) specialist domain. The scan entries are []events.D (or
// []any after a round-trip); each entry carries a "domain" key.
func scanReferencesDomain(d map[string]any, domain string) bool {
	if d == nil || domain == "" {
		return false
	}
	// events.D is an alias for map[string]any, so []events.D and []map[string]any are the
	// same type — one case covers the live in-process trace (the dispatch site builds
	// []events.D). []any covers a JSONL round-tripped trace where each entry decodes to
	// map[string]any inside an []any.
	switch scan := d["scan"].(type) {
	case []map[string]any:
		for _, e := range scan {
			if strFromData(e, "domain") == domain {
				return true
			}
		}
	case []any:
		for _, raw := range scan {
			if e, ok := raw.(map[string]any); ok && strFromData(e, "domain") == domain {
				return true
			}
		}
	}
	return false
}
