package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// knowledge.go wires the M3 knowledge layer into the engine: the sourcing ladder's reality + generator
// ports, the concretization classifier + fuser, and the concretize stage the loop runs BETWEEN dispatch
// and the hidden seam (representation-space-rebuild.md §3.3). The engine already satisfies
// subconscious.MemoryRecaller (RecallFact, M2 §2.4), so it is rung 3 directly; the knowledge registry is
// rung 2; the two ports below are rungs 4 (reality) and 5 (generated).

// Knowledge exposes the durable domain-knowledge registry (M3) so the TUI registry browser can list
// facts/patterns/snippets (a §6 anti-filler-gate row) and the CLI summary can count them. Read-only.
func (e *Engine) Knowledge() *knowledge.KnowledgeRegistry { return e.knowledge }

// ---------------------------------------------------------------------------
// rung 4: reality — only via the WATCHED SEAM (gated/observed/Fabricated-aware)
// ---------------------------------------------------------------------------

// realitySourcer is the rung-4 port: it forms an Intention and crosses the watched seam, NEVER a direct
// tool call (so the observation is gated, observed, and Fabricated-aware — the §6 invariant "reality
// only via the watched seam"). A FABRICATED observation (the offline heuristic stand-in) returns
// grounds=false so the ladder falls through (a fake reality is not sourced). Offline (no executor /
// watched-async disabled) it declines, so the bare heuristic path resolves through the grounded stores
// only — never a manufactured "reality" (goldens unchanged).
type realitySourcer struct{ e *Engine }

// SourceReality forms a read-only Intention from the need and opens it to reality through the watched
// seam. It returns the observed text, ok (a usable observation), grounds (it actually came from reality,
// !Fabricated), and the tool name. It feeds the observation into the grounding spine the SAME way the
// synchronous act path does, so a sourced reality fact is also a grounded claim.
func (rs *realitySourcer) SourceReality(need subconscious.FuelNeed) (text string, ok, grounds bool, tool string) {
	e := rs.e
	// The watched-sync seam must be reachable (an executor wired) AND enabled. Without a real executor
	// every act is FABRICATED — which never grounds — so there is no point crossing the seam offline.
	// CONFIG (grounding-read ablation): seam.watched_sync OFF ⇒ the rung-4 reality port declines too,
	// so the sourcing ladder never imports reality (no action.observation, no grounding) and the answer
	// resolves through the grounded stores / generator only — the same bypass as the ACT path.
	if e.executor == nil || e.watched == nil || !e.watchedSyncEnabled() {
		return "", false, false, ""
	}
	// The comprehender (the structured to-operator step) resolves {need, target} from the live context;
	// when it names a read/search target, the intention dispatches read_file/search — never run_tests.
	// (The bare "source: <query>" text has no read VERB, so SelectTool's Kind="measure" default would
	// route to run_tests — the confabulation cause for grounding-A-gold-0017/0019.) NO-OP on the test
	// double (not a RealityComprehender) → goldens unchanged; declines (target=="") → legacy measure path.
	intention := types.Intention{Text: "source: " + need.Query, Kind: "measure", Claim: need.Query}
	if comp, ok := e.backend.(backends.RealityComprehender); ok {
		if cneed, target, cok := comp.Comprehend(need.Context); cok && target != "" {
			switch cneed {
			case "read":
				intention.Text, intention.Kind = "read "+target, ""
			case "search":
				intention.Text, intention.Kind = "search "+target, ""
			}
		}
	}
	obs := e.watched.OpenToReality(intention)
	e.episodeActsIssued++ // FIX 2: a grounding read crossed the watched seam this episode (the sourcing-ladder rung-4 path)
	o, isObs := obs.RawReturn.(types.Observation)
	if !isObs {
		return "", false, false, ""
	}
	// feed the observation into the grounding spine (a fabricated one is rejected there, never grounds).
	e.groundObservation(intention, obs)
	if !o.GroundsReality() { // fabricated — not sourced
		return "", false, false, o.Tool
	}
	// A1 "grounded-but-not-voiced" fix: a rung-4 reality observation crosses the seam and GROUNDS
	// (grounded=true), but its VERBATIM imported value (e.g. the read/searched "ActionMargin: 1.5")
	// then flows on ONLY as fuel into a model fusion (fuseFuel's OperatorApply), which may paraphrase
	// the literal value away — and the observation is never recorded as a thought. Then neither the
	// answer nor any thought in g.History() carries the imported value, so a grounded episode still
	// scores FALSE. Appending the observation as an OBSERVATION thought lands the verbatim value in the
	// graph (and the active context) the SAME way the direct ACT path does, so the imported reality is
	// voiceable/scoreable regardless of whether the model's fusion preserves it. This is a genuine
	// observation the conscious crossed the seam to import — recording it is correct, not additive.
	// Golden-safe: gated on GroundsReality() (a fabricated heuristic obs returns above), and the rung-4
	// sourcer already declined when no executor is wired — so the no-workspace heuristic/golden path
	// never reaches here.
	obsCopy := obs
	landed := e.appendThought(&obsCopy, e.bus.Tick)
	// A-RAG3 write-back: fold the imported reality fact into the unified cognition graph as a `fact` node
	// + a `grounds` edge from the line that just landed the observation, so a LATER need can reach it by
	// graph-native multi-hop recall (the Zep/Graphiti pattern on the event-sourced substrate; NO separate
	// vector store). No-op only when subconscious.graph_recall is explicitly DISABLED — that gate is now ON
	// by default (A-RAG3 default-flip 2026-06-21), so the DEFAULT path DOES write back (intended; the model
	// is no longer byte-identical to a pre-A-RAG3 stream). lineID is the just-appended OBSERVATION thought's
	// id (the importing line).
	lineID := -1
	if landed != nil {
		lineID = landed.ID
	}
	e.writeBackGraphFact(o.Text, o.Tool, trustObservationFact, need.Entities, lineID)
	return o.Text, o.Ok && strings.TrimSpace(o.Text) != "", true, o.Tool
}

// trustObservationFact is the trust prior stamped on a written-back reality fact node (A-RAG3). It
// mirrors the reality rung's prior — the fact came from a real observation across the watched seam, the
// highest-trust grounded tier.
const trustObservationFact = 0.92

// ---------------------------------------------------------------------------
// rung 5: generated — the model invents it (the ONLY ungrounded rung, low-trust)
// ---------------------------------------------------------------------------

// fuelGenerator is the rung-5 port: the model invents the material via backend.Generate. It is the only
// ungrounded rung, flagged GENERATED so the Filter distrusts it at 0.42. text=="" ⇒ the model declined
// (the ladder bottoms out at FuelNone — never a fabricated heuristic stand-in, per
// feedback-heuristic-control-only: output CONTENT must be the model, not a heuristic).
type fuelGenerator struct{ e *Engine }

// GenerateFuel asks the backend to invent the missing material for the need. The CONTENT is the model's;
// the engine only routes. The query frames it as a request for the concrete fact the abstract move needs.
func (fg *fuelGenerator) GenerateFuel(need subconscious.FuelNeed) string {
	e := fg.e
	if e.backend == nil {
		return ""
	}
	goal := need.Query
	if e.graph != nil {
		goal = e.graph.Goal
	}
	return e.backend.Generate(goal, need.Context, e.rng)
}

// ---------------------------------------------------------------------------
// concretization: classifier + fuser + the loop stage
// ---------------------------------------------------------------------------

// fuelNeedClassifier resolves, data-driven, whether a fired candidate is a FUEL-NEEDING abstract move.
// A candidate is fuel-needing iff (a) it carries no concrete payload (a pure reason output — a
// tool-backed primitive or a cognition-op result carries a payload and is already concrete), AND (b) its
// stamped operator maps to a seed operator the catalog marks FuelNeeding (the M2 flag). This keys on the
// OperatorSpec.FuelNeeding flag, not a string-sniff. Returns the operator name, the flag, and the
// knowledge KIND the move wants.
func (e *Engine) fuelNeedClassifier(c *types.Candidate) (string, bool, string) {
	if c == nil || c.Payload != nil {
		return "", false, "" // already concrete (tool result / recall / minted / cognition op)
	}
	if c.Operator == nil {
		return "", false, ""
	}
	opName := representativeOpName(*c.Operator)
	if opName == "" {
		return "", false, ""
	}
	needs, ok := e.catalog.IsFuelNeeding(opName)
	if !ok || !needs {
		return "", false, ""
	}
	return opName, true, knowledgeKindFor(opName)
}

// representativeOpName maps a stamped Operator enum back to a representative fuel-needing seed operator
// name the catalog can answer IsFuelNeeding for. The enum is coarser than the operator name (GENERATE
// covers generate/vary/sample/analogize/extrapolate; SIMULATE covers hypothesize), so a representative
// fuel-needing op stands in. A non-generative enum returns "" (not fuel-needing).
func representativeOpName(op types.Operator) string {
	switch op {
	case types.GENERATE:
		return "generate"
	case types.SIMULATE:
		return "hypothesize"
	default:
		return "" // DECOMPOSE/VALIDATE/COMPARE/GENERALIZE/ABSTRACT are not fuel-needing GROUND/REFRAME moves
	}
}

// knowledgeKindFor maps a fuel-needing operator to the knowledge KIND it wants from rung 2: a
// hypothesize move wants a reusable pattern; a generate move wants a concrete fact/snippet ("" = any).
func knowledgeKindFor(opName string) string {
	switch opName {
	case "hypothesize":
		return "pattern"
	default:
		return "" // generate/analogize/vary/extrapolate: any kind
	}
}

// fuseFuel fuses sourced fuel into an abstract candidate's placeholder — a backend call whose CONTENT is
// the model's, conditioned on the grounded fuel so the model fills the gap with the sourced fact (not an
// invention). For a GENERATED-rung fuel the fuel text IS the model's invention, so it is used directly
// (re-asking would just be a second guess). For a sourced rung the model is asked to weave the grounded
// fact into the move's output via OperatorApply (its scoped one-move job). text=="" ⇒ keep the raw.
func (e *Engine) fuseFuel(c *types.Candidate, fuel subconscious.Fuel) string {
	if fuel.Source == subconscious.FuelGenerated {
		return fuel.Text // the generated rung already IS the model's content; no second guess
	}
	if e.backend == nil || strings.TrimSpace(fuel.Text) == "" {
		return ""
	}
	// Condition the move on the sourced fact: the sub-agent's OperatorApply with the fuel folded into the
	// context as a grounded premise, so the model's content is anchored on the sourced material.
	opName := representativeOpName(deref(c.Operator))
	domain := ""
	if c.Domain != nil {
		domain = *c.Domain
	}
	goal := fuel.Text
	if e.graph != nil {
		goal = e.graph.Goal
	}
	ctx := append([]types.Thought(nil), e.workingContext()...)
	ctx = append(ctx, types.Thought{
		ID: -1, Text: "Grounded " + fuel.Source.String() + ": " + fuel.Text,
		Source: types.OBSERVATION, Confidence: fuel.Trust,
	})
	return e.backend.OperatorApply(opName, "fuse the grounded fact into this move", c.Text, domain, goal, ctx)
}

// concretize is the engine stage the loop runs BETWEEN dispatch and the hidden seam (§3.3): it sources +
// fuses the fuel-needing fired candidates' missing material via the ladder, stamping provenance, so a
// fabricated-fuel candidate reaches the Filter still marked GENERATED (the seam is FED, never bypassed).
// allowReality / allowGenerated come from the §4.2 Source toggles + the open-flag default (generation
// allowed by default — a marked low-trust guess keeps the loop moving; Sources.Generated=off is the
// strict-grounding knob that drops an unsourced candidate). Returns the (possibly shrunk) candidate set.
func (e *Engine) concretize(cands []*types.Candidate, ctx []types.Thought) []*types.Candidate {
	if len(cands) == 0 {
		return cands
	}
	allowReality := e.features.Repr.Sources.Reality
	allowGenerated := e.features.Repr.Sources.Generated
	return subconscious.Concretize(cands, ctx, e.sourcing, e.fuelNeedClassifier, e.fuseFuel,
		allowReality, allowGenerated, e.suffGate, e.gates.concretize, e.bus.Emit)
}

// deref returns the pointed-to Operator, or the zero value (0, an invalid enum) when nil — a small guard
// for the fuser's operator read.
func deref(p *types.Operator) types.Operator {
	if p == nil {
		return 0
	}
	return *p
}

// ---------------------------------------------------------------------------
// A-RAG4: V(s)-triggered active re-sourcing
// ---------------------------------------------------------------------------

// maybeActiveResource is the engine half of A-RAG4 (docs/internal/notes/2026-06-20-rag-integration-analysis.md
// §7.4). It asks the Controller — the executive — whether the active node is a LOW-V(s), goal-relevant
// line worth ACTIVELY RE-SOURCING (the FLARE / active-inference epistemic trigger: retrieve precisely
// when the line is uncertain about a goal-relevant question). On a fired trigger it RE-INVOKES the
// existing sourcing ladder for the node's text and, when the ladder returns GROUNDED fuel (rungs 1-4 —
// present/knowledge/memory/reality, never the GENERATED guess that low V(s) signals is failing), appends
// that fact as an OBSERVATION thought so it is in the active context for the very DecideNext it informs.
//
// The decision lives in the Controller (Pattern A — no model call); this is the WIRING that acts on it.
// BOUNDED: the per-branch resourcedBranches marker enforces at most ONE re-source per branch, so the
// plant's branching ratio is untouched (a re-source is a fuel READ, not a fork) and the awake durability
// conditions hold. Default OFF (controller.active_resource) ⇒ ResourceTrigger returns fire=false ⇒ a pure
// no-op (no ladder walk, no thought, no event) ⇒ byte-identical. Sourcing being OFF/declining (FuelNone)
// is also a no-op. Honors the §4.2 Source reality toggle; never generates (a re-source must import NEW
// grounded information, not re-ask the same model).
func (e *Engine) maybeActiveResource(branch, tick int) {
	if e.graph == nil || e.sourcing == nil {
		return
	}
	vActive := e.graph.Active().Value
	already := setHas(e.resourcedBranches, branch)
	fire, query, _ := e.controller.ResourceTrigger(e.graph, vActive, already)
	if !fire {
		return
	}
	// Mark the branch re-sourced even if the ladder bottoms out — the trigger FIRED, so the bound is
	// spent (one principled re-source attempt per branch; do not re-attempt the same low-V line every tick).
	e.resourcedBranches[branch] = struct{}{}

	need := subconscious.FuelNeed{
		Query:          query,
		Kind:           "",
		Context:        e.workingContext(),
		Entities:       nil,
		AllowReality:   e.features.Repr.Sources.Reality,
		AllowGenerated: false, // a re-source imports NEW grounded info — never re-ask the same model (rung 5)
	}
	fuel := e.sourcing.Source(need)
	if fuel.Source == subconscious.FuelNone || !fuel.Grounded || strings.TrimSpace(fuel.Text) == "" {
		return // nothing newly grounded was sourced — leave the line for the Controller's normal decision
	}
	// A grounded re-sourced fact: land it in the conscious stream as an OBSERVATION (the same way the
	// rung-4 reality sourcer lands an imported observation), at the fuel's source trust, so DecideNext +
	// the next rerank see the freshly-imported material on the active line.
	obs := types.Thought{
		ID:         -1,
		Text:       "Re-sourced (" + fuel.Source.String() + "): " + fuel.Text,
		Source:     types.OBSERVATION,
		Confidence: fuel.Trust,
		BranchID:   &branch,
	}
	e.appendThought(&obs, tick)
}
