// sufficiency.go is the CRAG-style sufficiency gate (A-RAG1, docs/internal/2026-06-20-rag-integration-
// analysis.md §7.1): a Pattern-C escalation that grades the FUEL the sourcing ladder returned for a
// fuel-needing candidate — sufficient / ambiguous / insufficient — so the harness can ABSTAIN instead
// of over-commit a hollow recall.
//
// WHERE it sits (the load-bearing placement): inside Concretize, AFTER policy.Source(need) returns the
// fuel and BEFORE the fuel is fused into the candidate. It grades whether the recalled material actually
// COVERS the need it was sourced for. On INSUFFICIENT the candidate is DROPPED (the same abstain path the
// existing FuelNone drop uses) — a fabricated-coverage candidate never reaches the Filter, so the seam
// is FED, never bypassed (the hidden seam's own discipline, extended to retrieval).
//
// WHY this is structural, not a prompt (the lesson): Google's "Sufficient Context" (ICLR 2025) shows RAG
// context SUPPRESSES abstention — the model over-commits a hollow recall. The harness's own
// THOUGHT_GROUND_COMPLETE prompt-fix ("be careful, check supersession") was NET-NEGATIVE (it induced
// hedging, crashed answer tasks). A "be careful" prompt cannot deliver abstention. A DETERMINISTIC
// coverage floor that DROPS the under-covered candidate can — it removes the hollow fuel from the stream
// rather than asking the model to be careful with it.
//
// Pattern C (floor + ceiling): the deterministic FLOOR (control.ScoreSufficiency) ALWAYS runs and is
// always a valid verdict; the model CEILING (backends.SufficiencyJudge) is consulted ONLY on a flagged-
// fuzzy verdict (control.SufficiencyAmbiguity >= threshold) and only when a judge is wired. A clear-
// grounded-sufficient or a clear-insufficient is STRUCTURAL — the floor speaks with authority and the
// model is not consulted. A non-escalation surfaces escalation.floor_stands (Rule 4), never silent.
package subconscious

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// SufficiencyGate grades sourced fuel against the need it was sourced for, as a Pattern-C escalation. It
// is OPT-IN: when the seam.sufficiency_gate toggle is OFF (the default) the gate is a no-op pass-through
// (every fuel is treated SUFFICIENT, nothing dropped, no event) so the concretize stage stays byte-
// identical. Mirrors the Filter's NewFilter/Admit structure (the project's Pattern-C template).
type SufficiencyGate struct {
	// mode: "control" (deterministic floor only — the default), "llm" (escalate every flagged grading),
	// "hybrid" (escalate only flagged-fuzzy gradings). llm/hybrid require a backend that satisfies
	// backends.SufficiencyJudge; otherwise the mode resolves to "control" (the hasattr guard, mirror
	// NewFilter).
	mode  string
	judge backends.SufficiencyJudge // nil ⇒ no model ceiling (the floor is authoritative)
	gate  *config.Gate              // seam.sufficiency_gate; nil ⇒ OFF (opt-in instrument)
	emit  events.Emit               // bus closure (nil ⇒ silent)
}

// suffControlMode is the deterministic-floor mode (the gate's resolved-mode default), matching the
// Filter's controlMode. Named here so the floor-only posture has a single source of truth.
const suffControlMode = "control"

// NewSufficiencyGate builds the gate over the cognition mode + the (optional) model backend + the
// config gate + emit. Like NewFilter, the mode resolves to the deterministic floor ("control") UNLESS
// the backend actually satisfies backends.SufficiencyJudge (the hasattr guard). The test double does NOT
// implement SufficiencyJudge, so under it the gate is always the pure floor — deterministic + offline.
func NewSufficiencyGate(mode string, backend backends.Backend, gate *config.Gate, emit events.Emit) *SufficiencyGate {
	resolved := suffControlMode
	var j backends.SufficiencyJudge
	if backend != nil {
		if sj, ok := backend.(backends.SufficiencyJudge); ok {
			j = sj
			resolved = mode
		}
	}
	return &SufficiencyGate{mode: resolved, judge: j, gate: gate, emit: emit}
}

// Enabled reports whether the gate is live (the seam.sufficiency_gate opt-in toggle is ON). Unlike a
// bypass-not-delete gate, this OPT-IN instrument is OFF unless a gate is wired AND its toggle is ON — so
// the default (nil gate, or the default-OFF toggle) keeps the concretize stage byte-identical. Nil-safe.
func (g *SufficiencyGate) Enabled() bool {
	return g != nil && g.gate != nil && g.gate.Enabled()
}

// Grade is the Pattern-C sufficiency ladder (mirrors the Filter's Admit exactly):
//  1. the deterministic control FLOOR always runs (control.ScoreSufficiency) — Pattern-A core, always a
//     valid verdict;
//  2. compute the deterministic fuzziness flag (control.SufficiencyAmbiguity over the floor verdict);
//  3. escalate to the model CEILING ONLY when eligible — mode=="llm", or mode=="hybrid" AND flagged-fuzzy
//     — AND a judge is wired. A clear-grounded-sufficient / clear-insufficient (zero ambiguity) is never
//     escalated (the floor is authoritative);
//  4. on a successful escalation adopt the model's refined verdict (it may move sufficient↔ambiguous↔
//     insufficient) BUT it may never re-grade a STRUCTURAL floor verdict (those are not escalation-eligible);
//  5. on every non-escalation of an eligible case the floor STANDS and escalation.floor_stands is emitted
//     (Rule 4 — never silent). The default control mode never reaches there.
//
// It returns the final verdict + whether the harness should ABSTAIN (drop the candidate). It emits
// seam.sufficiency with the FINAL verdict. The caller (Concretize) MUST gate the whole call on Enabled()
// so the OFF path never reaches here — but Grade is itself defensive (a nil gate / OFF gate treats every
// fuel as sufficient).
func (g *SufficiencyGate) Grade(opName, query string, fuel Fuel) (verdict control.SufficiencyVerdict, abstain bool) {
	if !g.Enabled() {
		return control.SuffSufficient, false
	}

	// 1. the deterministic FLOOR always runs (Pattern-A core) — control, never the backend.
	s := control.ScoreSufficiency(query, fuel.Text, fuel.Trust, fuel.Grounded)

	// 2. the deterministic fuzziness flag.
	ambiguity := control.SufficiencyAmbiguity(s)

	// A floor verdict at the structural extremes — a clear grounded SUFFICIENT or a clear INSUFFICIENT
	// (ambiguity below threshold) — is the floor speaking with authority; the model may not re-grade it,
	// so it is never escalation-eligible (mirrors the Filter's structural-fact protection).
	flaggedFuzzy := ambiguity >= control.SufficiencyAmbiguityThreshold

	// 3. escalate ONLY when eligible: llm escalates every flagged grading; hybrid escalates only the
	// flagged-fuzzy case. Either way a non-flagged (structural) verdict is never escalated.
	eligible := g.mode == "llm" || (g.mode == "hybrid" && flaggedFuzzy)
	doEscalate := eligible && g.judge != nil

	if doEscalate {
		if refined, ok := g.judge.JudgeSufficiency(query, fuel.Text, s.Verdict.String()); ok {
			if rv, parsed := parseSufficiencyVerdict(refined); parsed {
				// 4. adopt the model's refined verdict (its grading is the ceiling above the floor). The
				// seam.sufficiency `appraiser` field records the escalation; the wire stays observable.
				s.Verdict = rv
				s.Source = "llm"
				s.Reason = "model: " + refined
			} else {
				// model returned an off-shape verdict string -> the floor stands (Rule 4).
				g.emitFloorStands(s, ambiguity, "model-offshape", true)
			}
		} else {
			// 5. the model declined/no-judge -> the floor stands; surface it (Rule 4).
			g.emitFloorStands(s, ambiguity, "model-declined", true)
		}
	} else if g.mode != suffControlMode && eligible && g.judge == nil {
		// flagged-fuzzy (or llm-mode) but no judge is wired to consult -> the floor stands (Rule 4).
		g.emitFloorStands(s, ambiguity, "no-model", false)
	}

	abstain = s.Verdict == control.SuffInsufficient
	g.emitSufficiency(opName, query, fuel, s, abstain)
	return s.Verdict, abstain
}

// emitSufficiency emits seam.sufficiency for one grading — the observability point so a grading + its
// abstain decision is fully visible (the spine of a TUI sufficiency panel).
func (g *SufficiencyGate) emitSufficiency(opName, query string, fuel Fuel, s control.Sufficiency, abstain bool) {
	if g.emit == nil {
		return
	}
	summary := "sufficiency [" + s.Verdict.String() + "] " + opName + ": " + clipRunes(query, 36)
	if abstain {
		summary += " -> ABSTAIN"
	}
	g.emit(events.Sufficiency, summary, events.D{
		"verdict":   s.Verdict.String(),
		"coverage":  s.Coverage,
		"trust":     s.Trust,
		"grounded":  s.Grounded,
		"rung":      fuel.Source.String(),
		"appraiser": s.Source,
		"abstained": abstain,
		"operator":  opName,
		"reason":    s.Reason,
		"query":     clipRunes(query, 64),
	})
}

// emitFloorStands surfaces a Pattern-C non-escalation (Rule 4): the gate's deterministic FLOOR stood as
// the grading because the model was not consulted or declined. modelConsulted is true only when the model
// was asked and declined (vs never asked). Mirrors the Filter's escalation.floor_stands emit exactly.
func (g *SufficiencyGate) emitFloorStands(s control.Sufficiency, ambiguity float64, reason string, modelConsulted bool) {
	if g.emit == nil {
		return
	}
	g.emit(events.EscalationFloorStands,
		"sufficiency floor stands ("+s.Verdict.String()+", "+reason+", ambiguity="+format2f(ambiguity)+")",
		events.D{
			"site":            "concretize.sufficiency",
			"decision":        s.Verdict.String(),
			"floor_decision":  s.Verdict.String(),
			"ambiguity":       round3(ambiguity),
			"reason":          reason,
			"model_consulted": modelConsulted,
		})
}

// parseSufficiencyVerdict maps a model-returned verdict string to the enum (case-insensitive, substring-
// tolerant on the three canonical tokens — a model may answer "INSUFFICIENT - the recall is off-topic").
// The order matters: "insufficient" contains "sufficient", so test it FIRST. parsed=false on any other
// string -> the caller keeps the floor (the off-shape guard, Rule 4).
func parseSufficiencyVerdict(s string) (control.SufficiencyVerdict, bool) {
	low := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(low, "insufficient"):
		return control.SuffInsufficient, true
	case strings.Contains(low, "ambiguous"):
		return control.SuffAmbiguous, true
	case strings.Contains(low, "sufficient"):
		return control.SuffSufficient, true
	default:
		return control.SuffSufficient, false
	}
}
