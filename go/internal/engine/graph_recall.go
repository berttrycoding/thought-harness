package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cogngraph"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// graph_recall.go is A-RAG3 — GRAPH-NATIVE recall + reality write-back over the existing unified
// cognition graph (docs/internal/notes/2026-06-20-rag-integration-analysis.md §7.3). Two halves, one knob
// (subconscious.graph_recall, default OFF ⇒ byte-identical):
//
//	RECALL    — a NEW sourcing-ladder rung (subconscious.FuelGraph, between memory and reality) that
//	            TRAVERSES the cogngraph from the active line up to maxGraphHops neighbours and recalls a
//	            GROUNDED fact reachable only via the relation graph. GraphRAG "Local search" with the
//	            extraction cost already SUNK: the cogngraph is reconstructed for free off the event bus,
//	            so there is no separate vector store, no embedding pass, no index build. The recall is a
//	            deterministic BFS + the existing lexical scorer — pure CONTROL, no model call.
//
//	WRITE-BACK— the rung-4 reality write-back ALSO emits subconscious.graph_writeback for each imported
//	            reality fact; the CognitionGraph folds it into a `fact` node + a `grounds` edge from the
//	            importing line (the Zep/Graphiti bitemporal-edge pattern on the event-sourced substrate).
//
// The two close a loop: a fact written back this episode is reachable by multi-hop recall on a LATER
// need whose direct lexical stores (present/knowledge/memory) miss but whose answer is two hops away in
// the graph (e.g. observation -> grounds -> fact). Both halves are no-ops when the gate is OFF.

// maxGraphHops bounds the GraphRAG Local-search radius. 2 is the GraphRAG-Local default (a node, its
// neighbours, and their neighbours) and the smallest radius that demonstrates genuine MULTI-hop recall
// (a fact reachable only through an intermediate node). Kept small so the walk is cheap + the surfaced
// material stays causally close to the active line (distant nodes are noise, the "lost in the middle"
// failure GraphRAG-Global has and Local avoids).
const maxGraphHops = 2

// graphRecallFloor is the lexical-relevance floor a traversed node's statement must clear to be RETURNED
// as graph fuel. It mirrors the sourcing ladder's presentFloor discipline (a grounded rung never
// surfaces incidental word overlap as a fact) — so graph recall imports a fact only when it is genuinely
// ABOUT the need, even though it was reached structurally rather than by a lexical store query.
//
// BORROWED-THRESHOLD NOTE (recognition discipline, MAD #2): this floor scores with the SAME scorer as
// the sourcing ladder's presentFloor (retrieval.LexicalScore) but deliberately sits at a DIFFERENT value
// (0.30 vs presentFloor's 0.34) — graph-traversed material is reached structurally, so the comment frames
// it as a slightly more permissive grounded-rung floor. It is NOT a value re-tune here because
// subconscious.graph_recall is now DEFAULT-ON (A-RAG3 default-flip, 2026-06-21), so changing 0.30 would
// change the live recall path — any re-tune is gated on a PAIRED LIVE-CLAUDE A/B (the recognition
// discipline: a near-borrow of a gate-A threshold must be measured, not silently re-tuned, at gate-B).
const graphRecallFloor = 0.30

// graphRecaller is the engine's subconscious.GraphRecaller — A-RAG3 graph-native recall over the live
// cognition graph. It is pure CONTROL (a graph walk + the deterministic lexical scorer); it never calls
// the model and never fabricates (a miss returns ok=false, exactly like the other grounded rungs).
type graphRecaller struct{ e *Engine }

// RecallGraph traverses the cogngraph OUTWARD from the active conscious line (BFS, up to maxGraphHops,
// following cause/grounds relations — not process membership) and returns the best lexically-relevant
// GROUNDED node it reaches: a written-back reality `fact` or an OBSERVATION thought. The start node is
// the active branch's tip thought (th:{process}:{tipID}); a fact reached only through an intermediate
// node is exactly the multi-hop recall this rung exists for. Returns ok=false on any miss (no graph, no
// active line, nothing reachable clears the relevance floor) — never a fabricated stand-in.
func (gr *graphRecaller) RecallGraph(need subconscious.FuelNeed) (text string, ok bool, hops int, provider string) {
	e := gr.e
	if e.cognitionGraph == nil || e.graph == nil {
		return "", false, 0, ""
	}
	query := strings.TrimSpace(need.Query)
	if query == "" {
		return "", false, 0, ""
	}
	start := gr.activeNodeID()
	if start == "" {
		return "", false, 0, ""
	}
	facts := e.cognitionGraph.GraphRecall(start, maxGraphHops)
	if len(facts) == 0 {
		return "", false, 0, ""
	}
	// pick the single most lexically-relevant reachable grounded node above the floor (deterministic:
	// GraphRecall returns first-seen/BFS order; strict-greater keeps the first on ties). One-best return
	// sidesteps the lost-in-the-middle/distractor failure the way the rest of the ladder does.
	best := -1.0
	var bestFact cogngraph.GraphFact
	for _, f := range facts {
		score := retrieval.LexicalScore(query, retrieval.Item{Text: f.Statement})
		if score < graphRecallFloor {
			continue
		}
		if score > best {
			best = score
			bestFact = f
		}
	}
	if best < 0 {
		return "", false, 0, "" // nothing reachable was relevant enough — fall through to reality
	}
	return bestFact.Statement, true, bestFact.Hops, bestFact.Provider
}

// activeNodeID returns the cogngraph node id of the active conscious LINE (the branch node
// br:{process}:{activeBranch}) — the GraphRAG-Local anchor for graph-native recall. The branch is the
// line HUB: from it the walk reaches the line's own prior thoughts (branch-`contains`, one hop) and the
// reality facts they grounded (one more hop) — the "node -> prior thoughts/memories" multi-hop the spec
// recalls over. Anchoring on the hub (not the tip thought) keeps every prior observation on the line
// within radius without needing a deeper walk. "" when there is no active branch yet so RecallGraph
// cleanly declines.
func (gr *graphRecaller) activeNodeID() string {
	b := gr.e.graph.Active()
	if b == nil {
		return ""
	}
	return gr.e.cognitionGraph.BranchNodeID(b.ID)
}

// writeBackGraphFact emits subconscious.graph_writeback for a rung-4 reality fact so the CognitionGraph
// folds it into a `fact` node + a `grounds` edge from the importing line — the A-RAG3 write-back half
// (the Zep/Graphiti pattern on the existing event-sourced substrate, NO separate vector store). It is
// called ONLY from the rung-4 reality path AFTER a GROUNDED (!Fabricated) observation, and ONLY when the
// graph_recall gate is ON. NOTE: that gate is now ON BY DEFAULT (A-RAG3 default-flip, config.go AllOn()
// GraphRecall:true, user-authorized 2026-06-21), so a DEFAULT run DOES emit this — the fold is intended;
// it just means the unified model is no longer byte-identical to a pre-A-RAG3 stream unless the gate is
// explicitly disabled (`--disable subconscious.graph_recall`). The fact's
// node id (fact:{process}:{seq}) and the importing line's node id (the OBSERVATION thought just appended)
// are formed off the cognition graph so they match the fold's id scheme exactly. lineThoughtID<0 means
// no line id is known (the edge is then omitted; the fact node still lands so recall can reach it via the
// observation thought's own graph neighbourhood).
func (e *Engine) writeBackGraphFact(statement, tool string, trust float64, entities []string, lineThoughtID int) {
	if e.gates.graphRecall.Disabled() {
		return // A-RAG3 explicitly disabled ⇒ no write-back ⇒ no fact node ⇒ byte-identical (the gate is ON by default since 2026-06-21)
	}
	if e.cognitionGraph == nil || strings.TrimSpace(statement) == "" {
		return
	}
	e.graphFactSeq++
	node := e.cognitionGraph.FactNodeID(e.graphFactSeq)
	line := ""
	if lineThoughtID >= 0 {
		line = e.cognitionGraph.ThoughtNodeID(lineThoughtID)
	}
	e.bus.Emit(events.GraphWriteBack, "graph write-back: "+clip(statement, 40), events.D{
		"statement": statement,
		"node":      node,
		"line":      line,
		"tool":      tool,
		"trust":     round2(trust),
		"entities":  entities,
	})
}

// clip is a small rune-safe truncation for the write-back event summary (no ellipsis — the full
// statement rides the data map). Local helper to avoid widening any shared utility.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
