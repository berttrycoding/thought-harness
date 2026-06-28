// sourcing.go is the sourcing / injection policy (representation-space-rebuild.md §3.2): the single
// function any fuel-needing move consults. It walks the FIVE knowledge sources in strict preference
// order (cheapest-and-most-trusted first) and returns the first hit with its provenance:
//
//  1. present    — already in the conscious stream  -> fetching is a NO-OP (~0 cost)
//  2. knowledge  — durable vetted domain knowledge  -> the knowledge registry (high trust)
//  3. memory     — first-person grounded experience -> memory.Semantic/Episodic (grounded-only)
//     3b. graph     — a grounded fact reached by multi-hop graph traversal -> the unified cognition graph
//     (A-RAG3 GraphRAG Local search; extraction cost SUNK; opt-in behind subconscious.graph_recall)
//  4. reality    — a tool crossed the watched seam  -> high cost, gated; the HIGHEST trust (0.92)
//  5. generated  — the model invents it             -> the LOW-trust floor (GENERATED prior 0.42)
//
// The ordering, plainly: don't fetch what you already have (present is a no-op); prefer what you
// already know and trust; only touch reality when the cheaper grounded sources came up empty; and only
// ever invent as a last resort, FLAGGED so the membrane knows not to trust it. Crucially REALITY
// OUTRANKS GENERATED — we would rather pay for a real tool call than launder a guess.
//
// The grounding tie is automatic: rungs 1-4 produce GROUNDED fuel; rung 5 is the only ungrounded rung,
// and it is exactly the types.GENERATED source the Filter already prices at 0.42. So "injected
// knowledge must be sourced, not fabricated" is enforced by the EXISTING Filter trust machinery — the
// policy routes provenance into trust, it does not re-implement trust.
package subconscious

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// presentFloor is the lexical-overlap floor a current thought must clear to count as "already carrying"
// the need's material (rung 1). Set above incidental word overlap so present only fires when the stream
// genuinely holds the fact — otherwise the ladder correctly falls through to the grounded stores.
const presentFloor = 0.34

// presentMatch reports whether a current thought's text already carries the need's material — a lexical
// relevance above presentFloor (the same content-word overlap the stores rank by). Deterministic.
func presentMatch(query, text string) bool {
	return retrieval.LexicalScore(query, retrieval.Item{Text: text}) >= presentFloor
}

// FuelSource is the rung of the sourcing ladder a need resolved at (the strict preference order above).
type FuelSource int

const (
	FuelPresent   FuelSource = iota // already in the conscious stream (no-op fetch)
	FuelKnowledge                   // the durable knowledge registry
	FuelMemory                      // first-person grounded experience
	FuelGraph                       // a grounded fact reached by multi-hop graph traversal (A-RAG3)
	FuelReality                     // a tool crossed the watched seam (highest trust)
	FuelGenerated                   // the model invented it (LOW trust — the floor)
	FuelNone                        // nothing sourced (and generation forbidden / declined)
)

// String renders the rung name (the wire value carried on subconscious.source / the FuelProvenance).
func (s FuelSource) String() string {
	switch s {
	case FuelPresent:
		return "present"
	case FuelKnowledge:
		return "knowledge"
	case FuelMemory:
		return "memory"
	case FuelGraph:
		return "graph"
	case FuelReality:
		return "reality"
	case FuelGenerated:
		return "generated"
	default:
		return "none"
	}
}

// The trust priors stamped per rung. They mirror the existing Filter source priors so provenance routes
// into the SAME trust the membrane already uses: reality is the highest grounded prior; generated is the
// GENERATED floor (0.42, verified backends/heuristic.go) the Filter already distrusts.
const (
	trustPresent   = 0.85 // inherits the present thought's standing (high — it is already in the stream)
	trustKnowledge = 0.85 // durable vetted knowledge (its own Trust overrides this when present)
	trustMemory    = 0.80 // grounded first-person experience
	trustGraph     = 0.78 // a grounded fact reached by multi-hop graph traversal (slightly below memory — same grounded material, one indirection removed)
	trustReality   = 0.92 // a real observation — the highest prior (matches the grounding observation tier)
	trustGenerated = 0.42 // the GENERATED floor — the membrane distrusts this
)

// Fuel is the material the ladder returned for a need, with its provenance. Trust is the prior to stamp
// onto the candidate's Relevance / Filter input; Grounded is true for rungs 1-4 (false for generated).
type Fuel struct {
	Text     string
	Source   FuelSource
	Provider string  // "conscious:t42" | "knowledge:fact" | "memory:semantic" | "reality:run_tests" | "generated"
	Trust    float64 // the prior to stamp on the candidate's Relevance / Filter input
	Grounded bool    // true for rungs 1-4 (sourced); false for FuelGenerated
}

// FuelNeed is one fuel-needing request. Query is the relevance text; Kind narrows the knowledge recall
// ("fact"|"pattern"|"snippet"|""); Context is the conscious stream (for rung 1, present); Entities are
// the relevance keys. The two permission knobs gate the costly / ungrounded rungs:
//
//	AllowReality   — a reason-only context may forbid touching reality (rung 4 skipped).
//	AllowGenerated — a strict-grounding context may forbid invention (rung 5 -> FuelNone, never a guess).
type FuelNeed struct {
	Query          string
	Kind           string
	Context        []types.Thought
	Entities       []string
	AllowReality   bool
	AllowGenerated bool
}

// KnowledgeRecaller is the rung-2 port (the durable knowledge registry). knowledge.KnowledgeRegistry
// satisfies it structurally; an interface keeps the subconscious package decoupled + swappable.
type KnowledgeRecaller interface {
	Recall(query, kind string, n int) []knowledge.Knowledge
	Record(k knowledge.Knowledge) bool
}

// MemorySourcer is the rung-3 port (first-person grounded experience). The engine's MemoryRecaller
// (M2 §2.4) already returns the single best relevant grounded statement, so the policy reuses it — a
// memory hit is the same never-fabricate, relevance-gated recall the `recall` primitive uses.
type MemorySourcer = MemoryRecaller

// RealitySourcer is the rung-4 port: it forms an Intention and crosses the WATCHED SEAM (never a direct
// tool call), so the observation is gated/observed/Fabricated-aware. It returns the observed text, ok
// (a usable observation), and grounds (the observation actually came from reality — !Fabricated). A
// fabricated observation returns grounds=false so rung 4 falls through (a fake reality is not sourced).
type RealitySourcer interface {
	SourceReality(need FuelNeed) (text string, ok bool, grounds bool, tool string)
}

// Generator is the rung-5 port: the model invents the material. It is the backend.Generate path — the
// ONLY ungrounded rung. text=="" means the model declined (-> FuelNone, never a fabricated stand-in).
type Generator interface {
	GenerateFuel(need FuelNeed) string
}

// GraphRecaller is the A-RAG3 graph-native recall port (the rung between memory and reality). It
// TRAVERSES the unified cognition graph from the active line up to several hops to recall a GROUNDED
// fact reachable only via the relation graph — GraphRAG "Local search" with the extraction cost already
// SUNK (the cogngraph is reconstructed for free off the event bus). It returns the recalled statement,
// ok (a usable, relevance-passing hit), the hop distance, and the provider (the relation chain). The
// engine implements it over internal/cogngraph; nil ⇒ the rung is skipped (the bare offline path has no
// graph recaller). Like every grounded rung it NEVER fabricates: a miss returns ok=false.
type GraphRecaller interface {
	RecallGraph(need FuelNeed) (text string, ok bool, hops int, provider string)
}

// SourcingPolicy walks the five rungs in strict order. Each port may be nil (that rung is skipped —
// the offline path has no reality/generator). The cfg gate (the §4.2 Source toggles) skips a disabled
// source in the walk; the emit closure makes the resolution observable on subconscious.source. The
// sourcing gate (subconscious.sourcing) bypasses the WHOLE policy to FuelNone when off.
type SourcingPolicy struct {
	knowledge    KnowledgeRecaller
	memory       MemorySourcer
	graph        GraphRecaller // A-RAG3 graph-native recall (the rung between memory and reality; nil ⇒ skipped)
	reality      RealitySourcer
	generator    Generator
	sources      *config.SourceToggles // §4.2 per-source toggles (nil ⇒ all sources permitted)
	sourcingGate *config.Gate          // subconscious.sourcing (nil-safe ⇒ enabled)
	graphGate    *config.Gate          // subconscious.graph_recall (A-RAG3; nil/off ⇒ the FuelGraph rung is skipped, byte-identical)
	emit         events.Emit           // bus closure (nil ⇒ silent)
	// factRecall is the A-RAG5 convertibility-on-facts hook (nil ⇒ disabled, default): on a rung-2
	// KNOWLEDGE hit it is called with the VERBATIM recalled statement (which downstream fusion may
	// paraphrase away), so the consolidation tracker counts the fact's recall on this line. Set by the
	// engine behind convert.facts; a nil hook is a pure no-op (byte-identical).
	factRecall func(statement string)
}

// SetGraphRecaller installs the A-RAG3 graph-native recall port + its gate (additive — keeps the
// existing constructor call sites untouched). With a nil recaller OR a disabled gate the FuelGraph rung
// is skipped entirely, so the ladder is byte-identical to the pre-A-RAG3 walk. The engine calls this
// once at construction with a cogngraph-backed recaller and the subconscious.graph_recall gate.
func (p *SourcingPolicy) SetGraphRecaller(g GraphRecaller, gate *config.Gate) {
	p.graph = g
	p.graphGate = gate
}

// SetFactRecallNoter installs the A-RAG5 fact-recall hook (additive — keeps the constructor call sites
// untouched). A nil noter (the default) ⇒ the rung-2 knowledge hit notifies nothing ⇒ byte-identical. The
// engine calls this once at construction with convert.NoteFactRecall, only when convert.facts is on.
func (p *SourcingPolicy) SetFactRecallNoter(note func(statement string)) { p.factRecall = note }

// NewSourcingPolicy builds the policy over its (optionally nil) source ports + the config + emit. The
// engine wires the live knowledge registry, the memory recaller, the watched-seam reality port, and the
// backend generator. Any nil port is a rung the walk skips (so the bare offline path resolves through
// present/memory/knowledge only).
func NewSourcingPolicy(kn KnowledgeRecaller, mem MemorySourcer, real RealitySourcer, gen Generator,
	sources *config.SourceToggles, gate *config.Gate, emit events.Emit) *SourcingPolicy {
	return &SourcingPolicy{
		knowledge: kn, memory: mem, reality: real, generator: gen,
		sources: sources, sourcingGate: gate, emit: emit,
	}
}

// sourceOn reports whether a given rung is permitted by the §4.2 Source toggles (nil ⇒ all on). A
// disabled source is skipped in the walk (Reality=off makes rung 4 a no-op; Generated=off is the
// strict-grounding posture).
func (p *SourcingPolicy) sourceOn(s FuelSource) bool {
	if p.sources == nil {
		return true
	}
	switch s {
	case FuelPresent:
		return p.sources.Present
	case FuelKnowledge:
		return p.sources.Knowledge
	case FuelMemory:
		return p.sources.Memory
	case FuelReality:
		return p.sources.Reality
	case FuelGenerated:
		return p.sources.Generated
	default:
		return true
	}
}

// Source walks the five rungs in strict order and returns the first hit with its provenance. The walk:
//
//  1. present   — scan the conscious context for a thought already carrying the fact (zero fetch).
//  2. knowledge — knowledge.Recall(query, kind, 1).
//  3. memory    — the never-fabricate, relevance-gated recall (semantic then episodic, via the port).
//  4. reality   — (if AllowReality) form an Intention, cross the WATCHED SEAM; a REAL observation ->
//     FuelReality AND a write-back (knowledge.Record) so next time it is a rung-2 hit (the
//     ladder teaches the registry). A Fabricated observation is NOT sourced -> fall through.
//  5. generated — (if AllowGenerated) backend.Generate -> FuelGenerated, Grounded:false, Trust=0.42.
//  6. else FuelNone.
//
// A disabled source (§4.2) or a nil port is skipped. The resolution emits subconscious.source.
func (p *SourcingPolicy) Source(need FuelNeed) Fuel {
	// subconscious.sourcing OFF ⇒ bypass the whole ladder to FuelNone (toggle = bypass, not delete).
	if p.sourcingGate.Disabled() {
		p.sourcingGate.Skip("sourcing ladder bypassed")
		return Fuel{Source: FuelNone, Provider: "none"}
	}

	// 1. present — already in the conscious stream (no-op fetch).
	if p.sourceOn(FuelPresent) {
		if text, ok := p.present(need); ok {
			return p.resolved(need, Fuel{Text: text, Source: FuelPresent,
				Provider: "conscious", Trust: trustPresent, Grounded: true})
		}
	}

	// 2. knowledge — durable vetted domain knowledge.
	if p.sourceOn(FuelKnowledge) && p.knowledge != nil {
		if hits := p.knowledge.Recall(need.Query, need.Kind, 1); len(hits) > 0 {
			k := hits[0]
			trust := k.Trust
			if trust <= 0 {
				trust = trustKnowledge
			}
			// A-RAG5: a durable knowledge fact was RECALLED on this line. Notify the consolidation tracker
			// with the VERBATIM statement (downstream fusion paraphrases it away). nil hook ⇒ no-op.
			if p.factRecall != nil {
				p.factRecall(k.Statement)
			}
			return p.resolved(need, Fuel{Text: k.Statement, Source: FuelKnowledge,
				Provider: "knowledge:" + k.Kind, Trust: trust, Grounded: true})
		}
	}

	// 3. memory — first-person grounded experience.
	if p.sourceOn(FuelMemory) && p.memory != nil {
		if stmt, ok := p.memory.RecallFact(need.Query); ok {
			return p.resolved(need, Fuel{Text: stmt, Source: FuelMemory,
				Provider: "memory:semantic", Trust: trustMemory, Grounded: true})
		}
	}

	// 3.5 graph — A-RAG3 GRAPH-NATIVE multi-hop recall (GraphRAG Local search over the unified cognition
	// graph). Cheaper than touching reality (no tool call — the extraction cost is already sunk in the
	// event-sourced cogngraph), grounded (it surfaces only written-back reality facts + OBSERVATION
	// thoughts, never speculation), so it ranks between memory and reality. Gated by subconscious.graph_
	// recall: nil recaller OR disabled gate ⇒ the rung is skipped (byte-identical). A miss falls through.
	if p.graph != nil && p.graphGate.Enabled() {
		if text, ok, _, provider := p.graph.RecallGraph(need); ok && strings.TrimSpace(text) != "" {
			return p.resolved(need, Fuel{Text: text, Source: FuelGraph,
				Provider: provider, Trust: trustGraph, Grounded: true})
		}
	}

	// 4. reality — only via the WATCHED SEAM (gated/observed/Fabricated-aware), and only if allowed.
	if need.AllowReality && p.sourceOn(FuelReality) && p.reality != nil {
		text, ok, grounds, tool := p.reality.SourceReality(need)
		if ok && grounds { // a fabricated observation (grounds=false) is NOT sourced -> fall through
			fuel := Fuel{Text: text, Source: FuelReality,
				Provider: "reality:" + tool, Trust: trustReality, Grounded: true}
			// write-back: a reality-sourced fact becomes durable knowledge so next time it is a rung-2
			// hit (the ladder teaches the registry). Never-fabricate holds (grounds==true here).
			if p.knowledge != nil {
				p.knowledge.Record(knowledge.Knowledge{
					Statement: text, Kind: "fact", Entities: need.Entities,
					Source: "reality:" + tool, Grounded: true, Trust: trustReality,
				})
			}
			return p.resolved(need, fuel)
		}
	}

	// 5. generated — the model invents it; the ONLY ungrounded rung, flagged LOW-trust (0.42).
	if need.AllowGenerated && p.sourceOn(FuelGenerated) && p.generator != nil {
		if text := strings.TrimSpace(p.generator.GenerateFuel(need)); text != "" {
			return p.resolved(need, Fuel{Text: text, Source: FuelGenerated,
				Provider: "generated", Trust: trustGenerated, Grounded: false})
		}
	}

	// 6. nothing sourced (and invention forbidden / declined) — never a guess.
	return p.resolved(need, Fuel{Source: FuelNone, Provider: "none"})
}

// present scans the conscious context (most recent first) for a thought that already carries the need's
// material — a current thought whose content is relevant to the query. A hit is a zero-fetch FuelPresent
// (we don't go looking for what is already in the stream). Relevance uses the same lexical floor the
// stores use, so "present" is decided the same way recall is. The recap preamble is excluded (it is
// reference scaffolding, not fuel).
func (p *SourcingPolicy) present(need FuelNeed) (string, bool) {
	if len(need.Context) == 0 || strings.TrimSpace(need.Query) == "" {
		return "", false
	}
	for i := len(need.Context) - 1; i >= 0; i-- {
		t := need.Context[i]
		if strings.HasPrefix(t.Text, types.RecapPrefix) || strings.TrimSpace(t.Text) == "" {
			continue
		}
		if presentMatch(need.Query, t.Text) {
			return t.Text, true
		}
	}
	return "", false
}

// resolved emits subconscious.source for the ladder resolution and returns the fuel unchanged. It is
// the single observability point so a need's fall-through (present -> knowledge -> memory -> reality ->
// generated, or bottoming out at none) is fully visible — the spine of a future TUI ladder panel.
func (p *SourcingPolicy) resolved(need FuelNeed, f Fuel) Fuel {
	if p.emit == nil {
		return f
	}
	p.emit(events.SubSource, "source ["+f.Source.String()+"]: "+clipRunes(need.Query, 40), events.D{
		"rung": f.Source.String(), "provider": f.Provider, "trust": round3(f.Trust),
		"grounded": f.Grounded, "query": clipRunes(need.Query, 64),
	})
	return f
}
