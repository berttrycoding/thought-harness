// concretize.go is the concretization step (representation-space-rebuild.md §3.3): the stage that USES
// the abstraction ladder. It sits AFTER a specialist/operator fires a fuel-needing candidate and BEFORE
// that candidate enters the hidden seam (between Dispatch returning `fired` and HiddenSeam.Relay).
//
// Some candidates come out ABSTRACT — a GROUND/REFRAME move (generate, hypothesize, analogize, vary,
// extrapolate) produces a shape MISSING its concrete content. Concretization pulls the missing fuel via
// the sourcing ladder (§3.2) and FUSES it into a concrete candidate, stamping provenance, before the
// seam sees it.
//
// WHY BEFORE THE SEAM (the load-bearing invariant): the hidden seam's whole discipline is "validate the
// RAW candidate before voicing — kill laundered hallucination at source." Running concretize BEFORE the
// Filter means a fabricated-fuel candidate arrives still wearing its types.GENERATED low-trust stamp,
// gets distrusted at 0.42, and (on conflict) is forked as a loser rather than voiced. Concretize FEEDS
// the seam; it never bypasses it.
//
// Which moves need fuel is DATA-DRIVEN, not a string-sniff: the engine supplies a resolver that consults
// OperatorSpec.FuelNeeding (the M2 flag) for the candidate's operator. A non-fuel-needing candidate
// (a tool-backed primitive that already carries ground truth, a role stance) passes through untouched.
package subconscious

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// FuelNeedClassifier resolves, for a fired candidate, the operator name it embodies, whether that
// operator is fuel-needing (OperatorSpec.FuelNeeding — the M2 flag, data-driven), and the knowledge
// KIND the move wants ("fact"|"pattern"|"snippet"|""). The engine supplies it (it owns the catalog +
// the candidate→operator mapping); keeping it a function keeps concretize decoupled from cognition.
type FuelNeedClassifier func(c *types.Candidate) (opName string, needs bool, kind string)

// Fuser fuses the sourced fuel into the abstract candidate's placeholder — a backend call whose CONTENT
// is the model's, conditioned on the grounded fuel so the model fills the gap with the sourced fact, not
// an invention (feedback-heuristic-control-only: heuristics route, the model writes). For a present /
// reality / memory / knowledge hit it returns the grounded, fused thought; for a generated-rung fuel it
// returns the model's text (which stays GENERATED low-trust). text=="" ⇒ fusion declined (keep the raw).
type Fuser func(c *types.Candidate, fuel Fuel) string

// Concretize walks the fired candidates and, for each FUEL-NEEDING one, sources its missing material via
// the ladder and fuses it in before the seam. For each fuel-needing candidate:
//
//  1. derive a FuelNeed (query = candidate text + entities; kind by operator; context = ctx);
//  2. fuel := policy.Source(need);
//  3. FUSE — a backend call conditioned on the sourced fuel so the placeholder is filled with the
//     grounded fact, not an invention;
//  4. SUFFICIENCY GATE (A-RAG1, OPT-IN) — grade the sourced fuel sufficient / ambiguous / insufficient; on
//     INSUFFICIENT ABSTAIN (drop the candidate, same path as FuelNone) rather than over-commit a hollow
//     recall. nil/OFF gate ⇒ no grading ⇒ byte-identical;
//  5. STAMP PROVENANCE — FuelGenerated keeps types.GENERATED (the Filter distrusts at 0.42); a sourced
//     rung blends the candidate's relevance with fuel.Trust and records a FuelProvenance payload;
//  6. if FuelNone (nothing sourced, generation forbidden): DROP the candidate rather than emit a hollow
//     one (the strict-grounding posture).
//
// allowReality / allowGenerated are the per-call permission knobs (a reason-only context forbids reality;
// a strict-grounding context forbids invention). suff is the OPT-IN CRAG sufficiency gate (nil ⇒ off ⇒
// byte-identical). A non-fuel-needing candidate passes through untouched. The concretizeGate
// (subconscious.concretize) bypasses the WHOLE stage (raw relay) when off. Returns a new slice (dropped
// candidates removed); deterministic (the ladder order is fixed and total).
func Concretize(cands []*types.Candidate, ctx []types.Thought, policy *SourcingPolicy,
	classify FuelNeedClassifier, fuse Fuser, allowReality, allowGenerated bool,
	suff *SufficiencyGate, gate *config.Gate, emit events.Emit) []*types.Candidate {
	// subconscious.concretize OFF ⇒ bypass the whole stage (raw relay — the seam sees the candidates as
	// they fired). Toggle = bypass, not delete. The wire/panel stays; config.skip records it.
	if gate.Disabled() {
		gate.Skip("concretization bypassed")
		return cands
	}
	if policy == nil || classify == nil {
		return cands
	}

	out := make([]*types.Candidate, 0, len(cands))
	for _, c := range cands {
		opName, needs, kind := classify(c)
		if !needs {
			out = append(out, c) // already concrete (tool-backed ground truth / a stance) — untouched
			continue
		}
		need := FuelNeed{
			Query:          c.Text,
			Kind:           kind,
			Context:        ctx,
			Entities:       candEntities(c),
			AllowReality:   allowReality,
			AllowGenerated: allowGenerated,
		}
		fuel := policy.Source(need)

		if fuel.Source == FuelNone {
			// nothing sourced AND invention forbidden/declined — drop rather than voice a hollow shape
			// (the strict-grounding posture; the candidate never reaches the seam).
			emitConcretize(emit, opName, fuel, c.Text, "", true)
			continue
		}

		// A-RAG1 (OPT-IN): CRAG-style sufficiency gate over the SOURCED fuel. nil/OFF gate ⇒ no-op ⇒ byte-
		// identical (Grade short-circuits to SUFFICIENT). On INSUFFICIENT the harness ABSTAINS — the
		// candidate is DROPPED before fusion, exactly as the FuelNone strict-grounding drop, so a hollow
		// recall never reaches the seam (the structural abstain-vs-over-commit fix). The grading happens
		// AFTER the ladder (so the rung's trust is known) but BEFORE fusion (so we never pay a fuse call on
		// fuel we are about to abstain on).
		if suff.Enabled() {
			if _, abstain := suff.Grade(opName, need.Query, fuel); abstain {
				emitConcretize(emit, opName, fuel, c.Text, "", true)
				continue
			}
		}

		fusedText := c.Text
		if fuse != nil {
			if t := strings.TrimSpace(fuse(c, fuel)); t != "" {
				fusedText = t
			}
		}
		before := c.Text
		c.Text = fusedText
		// STAMP PROVENANCE. A generated-rung fuel keeps types.GENERATED so the Filter distrusts it at
		// 0.42 (the laundered-hallucination guard). A sourced rung blends the specialist's relevance with
		// the fuel's trust and records a FuelProvenance for trace-back.
		if fuel.Source == FuelGenerated {
			c.Source = types.GENERATED
		} else {
			c.Relevance = blendRelevance(c.Relevance, fuel.Trust)
		}
		c.Payload = types.FuelProvenance{
			Source: fuel.Source.String(), Provider: fuel.Provider, Grounded: fuel.Grounded,
		}
		emitConcretize(emit, opName, fuel, before, fusedText, false)
		out = append(out, c)
	}
	return out
}

// blendRelevance blends the producing specialist's relevance with the sourced fuel's trust — a sourced
// candidate is at least as trusted as its fuel (a grounded fact should not be downgraded by a thin
// specialist relevance), capped at 1. The max keeps a strongly-sourced candidate strong; it never lifts
// a generated-rung candidate (that path keeps GENERATED and skips this).
func blendRelevance(rel, trust float64) float64 {
	v := rel
	if trust > v {
		v = trust
	}
	if v > 1 {
		v = 1
	}
	return v
}

// candEntities derives relevance keys for the need from the candidate (its domain + the content words of
// its text). The domain is the strongest cue (it is what the specialist lit up on); the text supplies the
// rest. Deterministic.
func candEntities(c *types.Candidate) []string {
	var ents []string
	if c.Domain != nil && *c.Domain != "" {
		ents = append(ents, *c.Domain)
	}
	return ents
}

// emitConcretize emits subconscious.concretize for one fuel-needing candidate's fusion (or drop). It is
// the observability point so a concretization is visible: the operator, the rung the fuel came from,
// whether it was grounded, the from→to text (clipped), and whether the candidate was dropped.
func emitConcretize(emit events.Emit, opName string, fuel Fuel, from, to string, dropped bool) {
	if emit == nil {
		return
	}
	summary := "concretize " + opName + " [" + fuel.Source.String() + "]"
	if dropped {
		summary += " -> DROP (unsourced)"
	} else {
		summary += ": " + clipRunes(to, 36)
	}
	emit(events.SubConcretize, summary, events.D{
		"operator": opName, "rung": fuel.Source.String(), "grounded": fuel.Grounded,
		"from": clipRunes(from, 48), "to": clipRunes(to, 48), "dropped": dropped,
	})
}
