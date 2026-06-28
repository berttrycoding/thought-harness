package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// graph_recall_test.go is the A-RAG3 WIRING + cognition gate (docs/internal/2026-06-20-rag-integration-
// analysis.md §7.3). It proves, on the REAL engine objects (the live cognition graph, the live sourcing
// policy the loop calls), that:
//   - OFF (`--disable subconscious.graph_recall`): the write-back emits NOTHING and the FuelGraph rung is
//     dormant ⇒ byte-identical (A-RAG3 went DEFAULT-ON 2026-06-21; these OFF tests pin the disable path).
//   - ON: a rung-4 reality fact written back via the engine's own writeBackGraphFact lands as a `fact`
//     node + `grounds` edge in the unified graph, AND a LATER need whose lexical stores miss recalls
//     that fact through the LIVE e.sourcing ladder at the FuelGraph rung — multi-hop GraphRAG recall.
//
// This is the wiring proof the saved lesson demands (tests passing != the feature runs): it drives the
// SAME e.sourcing.Source the concretize stage calls, with the SAME graphRecaller the engine installed.

// TestGraphWriteBackDormantWhenFlagOff is the byte-identical OFF gate: with subconscious.graph_recall OFF
// (the default), the engine's rung-4 write-back hook emits NO subconscious.graph_writeback and folds NO
// fact node — the wire is dormant.
func TestGraphWriteBackDormantWhenFlagOff(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	e.Features().Subconscious.GraphRecall = false // A-RAG3 is DEFAULT-ON now; pin the `--disable` path here
	if e.Features().Subconscious.GraphRecall {
		t.Fatal("precondition: subconscious.graph_recall must be OFF for this dormant-path test")
	}
	e.startEpisode("what is the deploy margin", true)

	// call the EXACT engine method the rung-4 reality path calls.
	e.writeBackGraphFact("ActionMargin: 1.5", "read_file", 0.92, []string{"ActionMargin"}, 1)

	if n := countKind(e.Bus(), events.GraphWriteBack); n != 0 {
		t.Fatalf("OFF: subconscious.graph_writeback fired %d times, want 0 (byte-identical)", n)
	}
	if len(e.CognitionGraph().ByType("fact")) != 0 {
		t.Fatal("OFF: a fact node was folded with the flag off (write-back not dormant)")
	}
}

// TestGraphRecallFiresOnLiveSourcingLadder is the A-RAG3 wiring + cognition claim ON the live loop: with
// subconscious.graph_recall ON, a reality fact written back into the unified graph is recalled through
// the ENGINE's OWN sourcing ladder (e.sourcing.Source — the one concretize calls) at the FuelGraph rung,
// via multi-hop traversal, when the lexical stores (present/knowledge/memory) miss. This is the proof the
// graphRecaller is wired into the live sourcing policy, not just constructed.
func TestGraphRecallFiresOnLiveSourcingLadder(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	e.Features().Subconscious.GraphRecall = true // flip ON (gates read the shared pointer live)

	e.startEpisode("what is the deploy margin", true)

	// Land an OBSERVATION thought on the active line (the importing line), exactly as the rung-4 reality
	// sourcer does after a grounded read. Its text is about "config in deploy.yaml" — note the FACT itself
	// ("ActionMargin: 1.5") shares NO lexical overlap with the recall query, so only the GRAPH path reaches it.
	obs := &types.Thought{ID: -1, Text: "config: action_margin set in deploy.yaml", Source: types.OBSERVATION, Confidence: 0.9}
	landed := e.appendThought(obs, e.bus.Tick)

	// Write the imported reality fact BACK into the graph (the A-RAG3 write-back), grounded by that line.
	e.writeBackGraphFact("ActionMargin: 1.5", "read_file", 0.92, []string{"ActionMargin"}, landed.ID)
	if wb := countKind(e.Bus(), events.GraphWriteBack); wb != 1 {
		t.Fatalf("ON: write-back emitted %d graph_writeback events, want exactly 1", wb)
	}
	// the fact is now a node in the unified graph (the Zep/Graphiti write-back), grounded by the line.
	if len(e.CognitionGraph().ByType("fact")) != 1 {
		t.Fatalf("ON: write-back folded %d fact nodes, want 1", len(e.CognitionGraph().ByType("fact")))
	}

	// Now a LATER need asks about the action margin. The conscious stream / knowledge / memory hold no
	// lexical hit for "ActionMargin" — but it is two hops away in the graph (branch -> observation ->
	// grounds -> fact). The LIVE sourcing ladder must resolve it at the FuelGraph rung.
	need := subconscious.FuelNeed{
		Query:        "ActionMargin value for the deploy",
		Context:      nil, // empty conscious stream for the need: present rung misses
		AllowReality: false,
	}
	fuel := e.sourcing.Source(need)
	if fuel.Source != subconscious.FuelGraph {
		t.Fatalf("ON: live sourcing ladder resolved at %s, want graph (the multi-hop fact must be recalled)", fuel.Source)
	}
	if fuel.Text != "ActionMargin: 1.5" {
		t.Fatalf("ON: graph recall returned %q, want the written-back fact verbatim", fuel.Text)
	}
	if !fuel.Grounded {
		t.Fatal("ON: a graph-recalled reality fact must be GROUNDED (it came from a real observation)")
	}
}

// TestGraphRecallDormantWhenFlagOff completes the OFF byte-identical gate from the recall side: with the
// flag OFF, even when a fact node exists in the graph, the FuelGraph rung is skipped and the ladder
// resolves elsewhere (here: FuelNone, all other rungs empty + reality forbidden) — the recaller never
// surfaces the fact.
func TestGraphRecallDormantWhenFlagOff(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	// build a graph that DOES contain a recallable fact, but with the flag OFF (so the write-back hook is a
	// no-op we instead seed the graph by flipping ON briefly only to write, then flip OFF for the recall).
	e.Features().Subconscious.GraphRecall = true
	e.startEpisode("what is the deploy margin", true)
	obs := &types.Thought{ID: -1, Text: "config: action_margin set in deploy.yaml", Source: types.OBSERVATION, Confidence: 0.9}
	landed := e.appendThought(obs, e.bus.Tick)
	e.writeBackGraphFact("ActionMargin: 1.5", "read_file", 0.92, []string{"ActionMargin"}, landed.ID)
	if len(e.CognitionGraph().ByType("fact")) != 1 {
		t.Fatal("setup: the fact node should exist")
	}

	// now flip the gate OFF: the FuelGraph rung must be skipped on the live ladder.
	e.Features().Subconscious.GraphRecall = false
	need := subconscious.FuelNeed{Query: "ActionMargin value for the deploy", AllowReality: false, AllowGenerated: false}
	fuel := e.sourcing.Source(need)
	if fuel.Source == subconscious.FuelGraph {
		t.Fatal("OFF: the FuelGraph rung fired with the flag off (recall not gated)")
	}
}
