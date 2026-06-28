package cogngraph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// recordedStream is a hand-built, recorded event stream that exercises every OnEvent branch and
// the per-tick temporal coupling — the analogue of feeding a recorded Python JSONL into a fresh
// CognitionGraph (Python test_assembles_purely_from_the_bus). Ticks group same-tick cause->effect
// pairs: a SUB_FIRE records a firing, an INJECT consumes it (admitted_as), an APPEND of an
// INJECTED thought consumes the pending injection (voiced_as); an INTENTION arms pendingIntention,
// a later OBSERVATION append consumes it (returned). Ids are ints (the live in-memory shape).
func recordedStream() []events.Event {
	ev := func(tick int, kind, summary string, data events.D) events.Event {
		layer := kind
		if i := strings.IndexByte(kind, '.'); i >= 0 {
			layer = kind[:i]
		}
		return events.Event{Tick: tick, Kind: kind, Layer: layer, Summary: summary, Data: data}
	}
	return []events.Event{
		// --- episode 1 ---------------------------------------------------------------
		ev(0, events.Episode, "episode 1", events.D{"process": "ep:1", "goal": "Is it safe to refactor this module?"}),
		ev(0, events.Goal, "goal", events.D{"id": 1, "source": "USER", "text": "Is it safe to refactor this module?"}),
		ev(0, events.Append, "append goal", events.D{"id": 1, "branch": 0, "source": "GENERATED", "text": "Is it safe to refactor this module?", "confidence": 0.0}),
		// tick 1: a sub-agent fires, its candidate is admitted + voiced
		ev(1, events.Tick, "", events.D{}),
		ev(1, events.SubFire, "safety fired", events.D{"domain": "safety", "stance": "unsafe", "relevance": 0.8, "text": "tests guard the change"}),
		ev(1, events.Inject, "inject", events.D{"domain": "safety", "confidence": 0.7}),
		ev(1, events.Append, "voiced", events.D{"id": 2, "branch": 0, "source": "INJECTED", "text": "the test suite covers this path", "confidence": 0.7}),
		// tick 2: the critic rules on the active line, then forks on conflict (mcp branch)
		ev(2, events.Tick, "", events.D{}),
		ev(2, events.Decision, "BRANCH: conflicting stances", events.D{"decision": "BRANCH", "reason": "conflicting stances on safety"}),
		ev(2, events.MCP, "branch", events.D{"op": "branch", "branch": 1, "reason": "explore the unsafe line"}),
		ev(2, events.XRef, "xref", events.D{"src": 0, "dst": 1, "kind": "CONTRADICTS"}),
		// a soft-stop decision + an escalation note must BOTH be skipped
		ev(2, events.Decision, "soft stop note", events.D{"decision": "STOP", "reason": "tentative", "soft": true}),
		ev(2, events.Decision, "escalate: heuristic=STOP -> THINK", events.D{"decision": "THINK", "escalated": true}),
		// tick 3: a workflow phase + a skill match
		ev(3, events.Tick, "", events.D{}),
		ev(3, events.SubWorkflow, "workflow phase", events.D{"workflow": "design-build-validate", "phase": 0, "operator": "decompose"}),
		ev(3, events.SkillMatch, "skill match", events.D{"skill": "audit", "tier": "higher", "shape": "investigate", "sub_skills": []any{"scan", "report"}}),
		// tick 4: an intention is produced, reality answers with an observation
		ev(4, events.Tick, "", events.D{}),
		ev(4, events.Decision, "ACT: open to reality", events.D{"decision": "ACT", "reason": "verify against the test suite"}),
		ev(4, events.Intention, "run the tests", events.D{"kind": "run"}),
		ev(4, events.Append, "observation", events.D{"id": 3, "branch": 0, "source": "OBSERVATION", "text": "all tests passed", "confidence": 0.9}),
		// tick 5: the outward answer
		ev(5, events.Tick, "", events.D{}),
		ev(5, events.Respond, "yes, it is safe — the suite covers it", events.D{"proactive": false}),
		// --- episode 2 (ids restart; must stay distinct via the process scope) -------
		ev(6, events.Episode, "episode 2", events.D{"process": "ep:2", "goal": "What is 6 times 7?"}),
		ev(6, events.Append, "append goal", events.D{"id": 1, "branch": 0, "source": "GENERATED", "text": "What is 6 times 7?", "confidence": 0.0}),
		ev(7, events.Tick, "", events.D{}),
		ev(7, events.Respond, "42 — that's worth sharing", events.D{"proactive": true}),
	}
}

func feed(c *CognitionGraph, stream []events.Event) {
	for _, e := range stream {
		c.OnEvent(e)
	}
}

// TestReplayReconstructsTheModel is the read-only validation (PORT-PLAN §6 cogngraph gate): a
// fresh CognitionGraph fed nothing but the recorded event stream reconstructs the full cross-layer
// node/edge model. It asserts the entity scheme, the temporal-coupling edges, process scoping, and
// the layer spread — the Go analogue of Python test_model_spans_all_layers +
// test_assembles_purely_from_the_bus.
func TestReplayReconstructsTheModel(t *testing.T) {
	c := New()
	feed(c, recordedStream())

	// every layer present in one graph (process -> subconscious -> seam -> conscious -> critic -> action)
	for _, layer := range []string{"process", "subconscious", "seam", "conscious", "critic", "action"} {
		if len(c.ByLayer(layer)) == 0 {
			t.Fatalf("layer %q missing from the unified model", layer)
		}
	}
	// the entity scheme, exact ids
	want := map[string]string{
		"ep:1":                       "episode",
		"ep:2":                       "episode",
		"goal:1":                     "goal",
		"sp:safety":                  "specialist",
		"fire:safety@1":              "firing",
		"inj:1#1":                    "injection",
		"th:ep:1:2":                  "thought",
		"br:ep:1:0":                  "branch",
		"br:ep:1:1":                  "branch",
		"dec:2":                      "decision",
		"wf:design-build-validate":   "workflow",
		"ph:design-build-validate#0": "phase",
		"sk:audit":                   "skill",
		"int:4":                      "intention",
		"th:ep:1:3":                  "thought", // the OBSERVATION
		"resp:5":                     "response",
		"th:ep:2:1":                  "thought", // episode-2 thought, distinct from ep:1's
	}
	for id, typ := range want {
		n, ok := c.Nodes[id]
		if !ok {
			t.Fatalf("expected node %q (type %s) missing", id, typ)
		}
		if n.Type != typ {
			t.Errorf("node %q: type = %q, want %q", id, n.Type, typ)
		}
	}

	// the cross-layer provenance edges (the temporal-coupling stitches)
	wantEdges := []Edge{
		{"sp:safety", "fired", "fire:safety@1"},
		{"th:ep:1:1", "triggered", "fire:safety@1"}, // last thought at fire time was the goal th:1
		{"fire:safety@1", "admitted_as", "inj:1#1"}, // same-tick firing -> injection
		{"inj:1#1", "voiced_as", "th:ep:1:2"},       // pending injection -> the INJECTED thought
		{"dec:2", "decided_on", "br:ep:1:0"},        // critic ruled on the active line's branch
		{"dec:2", "caused", "br:ep:1:1"},            // the mcp branch the decision caused
		{"br:ep:1:0", "contradicts", "br:ep:1:1"},   // the xref, lowercased
		{"dec:4", "produced", "int:4"},              // ACT decision -> intention
		{"int:4", "returned", "th:ep:1:3"},          // intention -> the OBSERVATION thought
		{"ep:1", "pursues", "goal:1"},
		{"ep:1", "matched", "sk:audit"},
		{"wf:design-build-validate", "has_phase", "ph:design-build-validate#0"},
	}
	for _, we := range wantEdges {
		if !hasEdge(c, we) {
			t.Errorf("missing edge %s -%s-> %s", we.Src, we.Rel, we.Dst)
		}
	}

	// the soft-stop + escalation decisions were skipped: only dec:2 and dec:4 exist
	decs := c.ByType("decision")
	if len(decs) != 2 {
		t.Errorf("expected exactly 2 decisions (soft-stop + escalation skipped), got %d: %v", len(decs), ids(decs))
	}

	// process scoping: every ep:2 entity is contained by ep:2
	owners := map[string]string{}
	for _, e := range c.Edges {
		if e.Rel == "contains" && strings.HasPrefix(e.Src, "ep:") {
			owners[e.Dst] = e.Src
		}
	}
	for _, n := range c.ByType("thought") {
		if n.Process == "ep:2" && owners[n.ID] != "ep:2" {
			t.Errorf("ep:2 thought %q not contained by ep:2 (owner=%q)", n.ID, owners[n.ID])
		}
	}

	// the proactive response is labelled "outreach: ", the plain one "answer: "
	if got := c.Nodes["resp:5"].Label; !strings.HasPrefix(got, "answer: ") {
		t.Errorf("resp:5 label = %q, want answer: prefix", got)
	}
	if got := c.Nodes["resp:7"].Label; !strings.HasPrefix(got, "outreach: ") {
		t.Errorf("resp:7 label = %q, want outreach: prefix", got)
	}
}

// TestProvenanceAndLineageCrossLayers mirrors Python
// test_cross_layer_provenance_of_an_injected_thought: an INJECTED thought traces back across
// layers to the sub-agent that produced it, via voiced_as / admitted_as / fired, and the readable
// lineage renders the sub-agent at the root.
func TestProvenanceAndLineageCrossLayers(t *testing.T) {
	c := New()
	feed(c, recordedStream())

	prov := c.Provenance("th:ep:1:2") // the INJECTED thought
	rels := map[string]struct{}{}
	layers := map[string]struct{}{}
	for _, p := range prov {
		rels[p.Edge.Rel] = struct{}{}
		if n, ok := c.Nodes[p.Edge.Src]; ok {
			layers[n.Layer] = struct{}{}
		}
	}
	for _, r := range []string{"voiced_as", "admitted_as", "fired"} {
		if _, ok := rels[r]; !ok {
			t.Errorf("provenance missing relation %q (rels=%v)", r, keys(rels))
		}
	}
	for _, l := range []string{"seam", "subconscious"} {
		if _, ok := layers[l]; !ok {
			t.Errorf("lineage does not cross layer %q (layers=%v)", l, keys(layers))
		}
	}
	if lin := c.Lineage("th:ep:1:2"); !strings.Contains(lin, "sub-agent") {
		t.Errorf("lineage of the injected thought should name the sub-agent:\n%s", lin)
	}
	// an unknown node renders the Python "(unknown)" sentinel
	if got := c.Lineage("nope"); got != "nope: (unknown)" {
		t.Errorf("Lineage(unknown) = %q, want %q", got, "nope: (unknown)")
	}
}

// TestStatsAndSummary pins the stats counts + the one-line summary shape (Python stats/summary).
func TestStatsAndSummary(t *testing.T) {
	c := New()
	feed(c, recordedStream())
	s := c.Stats()
	if s.Processes != 2 {
		t.Errorf("processes = %d, want 2", s.Processes)
	}
	if s.Nodes != len(c.Nodes) || s.Edges != len(c.Edges) {
		t.Errorf("stats totals (%d nodes / %d edges) disagree with the maps (%d / %d)",
			s.Nodes, s.Edges, len(c.Nodes), len(c.Edges))
	}
	sum := c.Summary()
	if !strings.HasPrefix(sum, "cognition: ") {
		t.Errorf("summary = %q, want cognition: prefix", sum)
	}
	// the layer order in the summary is fixed: process before subconscious before seam ...
	// (search with a leading space so "conscious:" does not match inside "subconscious:").
	for i := 0; i+1 < len(layerOrder); i++ {
		a := strings.Index(sum, " "+layerOrder[i]+":")
		b := strings.Index(sum, " "+layerOrder[i+1]+":")
		if a >= 0 && b >= 0 && a > b {
			t.Errorf("summary layer order wrong: %q before %q in %q", layerOrder[i+1], layerOrder[i], sum)
		}
	}
}

// TestJSONReplayFormatsFloatIDsLikePython characterises the NAIVE-decode hazard the id-builders
// must survive: if a recorded stream is round-tripped through Go's plain json.Marshal/Unmarshal,
// every numeric id decodes as float64, and pyStr then formats it as str(float) — "5.0", not "5".
// This pins the formatter to that float case so a float feed reconstructs self-consistent ids.
//
// NOTE: this is NOT the Tier-6 byte-identical gate. Python's own json.loads keeps an integer literal
// as an int (so its replay yields "th:ep:1:1", no ".0"). The gate-correct path —
// recorded Python JSONL → byte-identical Go model — is TestReplayRecordedPythonStreamByteIdentical
// (replay_parity_test.go), whose loader decodes whole-valued JSON numbers as Go int exactly as
// Python does. This test documents WHY that loader is necessary (a plain Unmarshal would diverge).
func TestJSONReplayFormatsFloatIDsLikePython(t *testing.T) {
	// round-trip the stream through JSON exactly as a recorded JSONL would
	stream := recordedStream()
	var jsonStream []events.Event
	for _, e := range stream {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back events.Event
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		jsonStream = append(jsonStream, back)
	}
	c := New()
	feed(c, jsonStream)

	// after JSON decode, id=1 is float64(1.0); Python f"th:{p}:{1.0}" == "th:ep:1:1.0"
	if _, ok := c.Nodes["th:ep:1:1.0"]; !ok {
		t.Errorf("JSON-replayed thought id should be th:ep:1:1.0 (Python str(1.0)); nodes: %v", nodeIDs(c))
	}
	if _, ok := c.Nodes["fire:safety@1"]; !ok {
		// the tick is the int Event.Tick field (not data), so it stays "1" not "1.0"
		t.Errorf("firing id should be fire:safety@1 (tick is an int field); nodes: %v", nodeIDs(c))
	}
	// goal:1 — d["id"] is float64(1.0) after JSON -> Python f"goal:{1.0}" == "goal:1.0"
	if _, ok := c.Nodes["goal:1.0"]; !ok {
		t.Errorf("JSON-replayed goal id should be goal:1.0 (Python str(1.0)); nodes: %v", nodeIDs(c))
	}
}

// TestPyFloatReprMatchesCPython pins the float formatter to CPython's repr(float).
func TestPyFloatReprMatchesCPython(t *testing.T) {
	cases := map[float64]string{
		0.0:  "0.0",
		1.0:  "1.0",
		5.0:  "5.0",
		42.0: "42.0",
		0.7:  "0.7",
		0.55: "0.55",
		-3.0: "-3.0",
	}
	for in, want := range cases {
		if got := pyFloatRepr(in); got != want {
			t.Errorf("pyFloatRepr(%v) = %q, want %q", in, got, want)
		}
	}
	// pyStr funnels int vs float distinctly: str(5)=="5", str(5.0)=="5.0", str(None)=="None"
	if got := pyStr(5); got != "5" {
		t.Errorf("pyStr(int 5) = %q, want 5", got)
	}
	if got := pyStr(5.0); got != "5.0" {
		t.Errorf("pyStr(float 5.0) = %q, want 5.0", got)
	}
	if got := pyStr(nil); got != "None" {
		t.Errorf("pyStr(nil) = %q, want None", got)
	}
}

// --- tiny test helpers -------------------------------------------------------------------

func hasEdge(c *CognitionGraph, want Edge) bool {
	for _, e := range c.Edges {
		if *e == want {
			return true
		}
	}
	return false
}

func ids(ns []*Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

func nodeIDs(c *CognitionGraph) []string { return c.nodeOrder }

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// graphRecallStream builds a tiny graph that makes A-RAG3 MULTI-HOP recall testable: an active line
// (th:ep:1:1) crosses the watched seam, lands an OBSERVATION thought (th:ep:1:2) carrying a TOOL output,
// and the engine writes that reality fact BACK as a `fact` node (fact:ep:1:1) with a `grounds` edge from
// the observation line. Crucially the fact's STATEMENT shares no lexical overlap with the active line's
// goal text — it is reachable ONLY by traversing line -> observation -> grounds -> fact, i.e. multi-hop.
func graphRecallStream() []events.Event {
	ev := func(tick int, kind, summary string, data events.D) events.Event {
		layer := kind
		if i := strings.IndexByte(kind, '.'); i >= 0 {
			layer = kind[:i]
		}
		return events.Event{Tick: tick, Kind: kind, Layer: layer, Summary: summary, Data: data}
	}
	return []events.Event{
		ev(0, events.Episode, "episode 1", events.D{"process": "ep:1", "goal": "what is the deploy margin"}),
		ev(0, events.Append, "goal", events.D{"id": 1, "branch": 0, "source": "GENERATED", "text": "what is the deploy margin", "confidence": 0.0}),
		ev(1, events.Tick, "", events.D{}),
		// the active line opens to reality and lands an OBSERVATION thought (id 2)
		ev(1, events.Decision, "ACT", events.D{"decision": "ACT", "reason": "read the config"}),
		ev(1, events.Intention, "read config", events.D{"kind": "read"}),
		ev(1, events.Append, "obs", events.D{"id": 2, "branch": 0, "source": "OBSERVATION", "text": "config: action_margin set in deploy.yaml", "confidence": 0.9}),
		// A-RAG3 write-back: the imported reality fact, folded as a `fact` node + `grounds` edge from line th:ep:1:2.
		// The statement is deliberately about "ActionMargin: 1.5" — NO overlap with the goal "deploy margin" text,
		// so only graph traversal (not a lexical store query against the goal) can reach it.
		ev(1, events.GraphWriteBack, "write-back", events.D{
			"statement": "ActionMargin: 1.5", "node": "fact:ep:1:1", "line": "th:ep:1:2",
			"tool": "read_file", "trust": 0.92, "entities": []any{"ActionMargin"},
		}),
	}
}

// TestGraphWriteBackFoldsFactNodeAndEdge asserts the A-RAG3 write-back HALF: a subconscious.graph_writeback
// event folds a `fact` node into the unified model AND a `grounds` edge from the importing line — the
// Zep/Graphiti pattern on the event-sourced substrate. Mutation-sensitive: drop the edge or the node and
// this fails.
func TestGraphWriteBackFoldsFactNodeAndEdge(t *testing.T) {
	c := New()
	feed(c, graphRecallStream())

	fact, ok := c.Nodes["fact:ep:1:1"]
	if !ok {
		t.Fatal("write-back did not create the fact node fact:ep:1:1")
	}
	if fact.Type != "fact" {
		t.Fatalf("fact node type = %q, want fact", fact.Type)
	}
	if got, _ := fact.Attrs["statement"].(string); got != "ActionMargin: 1.5" {
		t.Fatalf("fact statement = %q, want the verbatim reality value", got)
	}
	// the `grounds` edge: the importing line th:ep:1:2 GROUNDS the fact.
	found := false
	for _, e := range c.Incoming("fact:ep:1:1") {
		if e.Rel == "grounds" && e.Src == "th:ep:1:2" {
			found = true
		}
	}
	if !found {
		t.Fatal("write-back did not create the `grounds` edge th:ep:1:2 -> fact:ep:1:1")
	}
}

// TestGraphRecallReachesFactOnlyViaTraversal is the A-RAG3 RECALL cognition claim: a fact whose statement
// has ZERO lexical overlap with the active line is recalled ONLY because it is reachable through the
// relation graph (branch hub -> prior observation -> grounds -> fact), at hop distance 2. This is
// GraphRAG "Local search" — the recall a flat lexical store CANNOT do. Mutation-sensitive: cap maxHops at
// 1 (the fact is 2 hops from the branch hub) or drop the `grounds` edge and the fact becomes unreachable.
func TestGraphRecallReachesFactOnlyViaTraversal(t *testing.T) {
	c := New()
	feed(c, graphRecallStream())

	// anchor on the active LINE (the branch hub br:ep:1:0), exactly as the engine's graphRecaller does.
	// The fact is NOT adjacent to the hub; it is reachable only via the OBSERVATION thought th:ep:1:2
	// (branch-contains, hop 1) -> grounds -> fact:ep:1:1 (hop 2).
	start := c.BranchNodeID(0)

	// 1-hop walk CANNOT reach the fact (it is two hops away) — proves the recall is genuinely multi-hop.
	if oneHop := factStatements(c.GraphRecall(start, 1)); contains(oneHop, "ActionMargin: 1.5") {
		t.Fatalf("1-hop recall should NOT reach the 2-hop fact; got %v", oneHop)
	}

	// 2-hop walk DOES reach it.
	facts := c.GraphRecall(start, 2)
	var got *GraphFact
	for i := range facts {
		if facts[i].Statement == "ActionMargin: 1.5" {
			got = &facts[i]
		}
	}
	if got == nil {
		t.Fatalf("2-hop graph recall did not reach the written-back fact; got %v", factStatements(facts))
	}
	if got.Hops != 2 {
		t.Fatalf("fact hop distance = %d, want 2 (branch -> observation -> grounds -> fact)", got.Hops)
	}
	if got.Type != "fact" {
		t.Fatalf("recalled node type = %q, want fact", got.Type)
	}
}

// TestGraphRecallSkipsEpisodeScope asserts the walk does NOT traverse the EPISODE-ROOT `contains` (process
// membership) edge — otherwise it would flood from the episode root to every node in the run, making
// "multi-hop recall" meaningless. The episode root must never surface, and the walk must not reach
// scope-mates via the process node.
func TestGraphRecallSkipsEpisodeScope(t *testing.T) {
	c := New()
	feed(c, graphRecallStream())
	facts := c.GraphRecall(c.BranchNodeID(0), maxGraphHopsTest)
	for _, f := range facts {
		if f.NodeID == "ep:1" {
			t.Fatal("graph recall reached the episode root via the process `contains` scope edge (must be skipped)")
		}
	}
}

// maxGraphHopsTest mirrors the engine's GraphRAG-Local radius for the cogngraph-level tests.
const maxGraphHopsTest = 2

func factStatements(fs []GraphFact) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Statement)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
