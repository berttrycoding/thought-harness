// Package cogngraph is the unified cognition model — ONE addressable graph spanning every
// layer and subsystem (Tier 6, read-only/derived).
//
// search.View (internal/search) unified the Conscious thought-stream as A* best-first search.
// But the architecture is wider than the thought stream: a thought is *produced* by a
// Subconscious sub-agent (specialist), *admitted/voiced* through the hidden seam, *decided
// upon* by the Critic, and may be *acted out* by an Action effector — and the whole run is one
// cognitive **process**. Those are all connected; this package makes that one model, not five.
//
// The design twist: we don't bolt a new bookkeeping layer onto every subsystem. The event bus
// is already the connective tissue — every subsystem emits typed events carrying the ids and
// relations. So CognitionGraph *subscribes to the bus* and assembles a single entity/relation
// graph from the event stream. Non-invasive, and the fact that one coherent graph reconstructs
// cleanly from the events is itself the proof that the subsystems are genuinely one connected
// model. Because it only consumes the stream, it is validated by REPLAYING a recorded event
// stream and comparing the reconstructed node/edge model byte-for-byte against the Python.
//
// Entities (each with a stable id, scoped to a `process`):
//
//	layer     entity        id scheme            born from
//	───────   ───────────   ──────────────────   ─────────────────────
//	process   episode       ep:{n}               lifecycle.episode      (the "process id")
//	back      specialist    sp:{domain}          back.fire              (the persistent sub-agent)
//	back      firing        fire:{domain}@{tick} back.fire              (one activation of a sub-agent)
//	back      workflow      wf:{name}            back.workflow
//	back      phase         ph:{name}#{i}        back.workflow
//	seam      injection     inj:{tick}#{k}       seam.inject            (admitted + voiced)
//	middle    thought       th:{id}              middle.append
//	middle    branch        br:{id}              middle.mcp / append
//	critic    decision      dec:{tick}           critic.decision
//	front     intention     int:{tick}           front.intention
//	front     observation   th:{id} (a thought)  front.observation
//	front     response      resp:{tick}          front.respond
//
// Relations (the cross-layer provenance — read "src REL dst"):
//
//	sp        fired         firing          a sub-agent produced this activation
//	thought   triggered     firing          the active line that pulled the sub-agent
//	firing    admitted_as   injection       the candidate survived the hidden seam
//	injection voiced_as     thought         the injection became this INJECTED thought
//	decision  decided_on    branch          the Critic ruled on this line
//	decision  produced      intention       an ACT decision formed an effector intention
//	intention returned      thought         reality answered (the OBSERVATION thought)
//	branch    forked_from   branch          a deliberate / conflict fork
//	workflow  has_phase     phase           the operator pipeline
//	episode   contains      *               process membership (scope)
//
// With this, any entity's full lineage is traceable across layers, e.g. a thought back through
// its injection, the firing that produced it, the sub-agent behind it, and the line that
// triggered it.
//
// Ported from the (now-removed) Python thought_harness/cognition_graph.py. It imports events, types and
// search (for the A* projection), and consumes nothing but the event stream.
package cogngraph

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/search"
)

// Node is one entity in the unified model (Python cognition_graph.Node). attrs holds the
// per-kind mutable payload (a thought's confidence, a firing's stance, …) — refreshed in place
// the way the Python dataclass field is.
type Node struct {
	ID      string
	Type    string
	Layer   string
	Label   string
	Process string // "" == Python None (the node was created before any episode)
	Tick    int
	Attrs   map[string]any // Python field(default_factory=dict) — allocated per node
}

// Edge is one directed relation between two entities (Python cognition_graph.Edge): "src REL
// dst".
type Edge struct {
	Src string
	Rel string
	Dst string
}

// obsSources is the set of middle.append sources that are observations returned from reality
// (the watched seam). Mirrors Python _OBS_SOURCES = {"OBSERVATION"}.
var obsSources = map[string]struct{}{"OBSERVATION": {}}

// edgeKey is the dedup key for an edge — Python's (src, rel, dst) tuple in the _edgeset.
type edgeKey struct {
	src, rel, dst string
}

// CognitionGraph is the single, unified, cross-layer model — assembled live from the event bus
// (Python cognition_graph.CognitionGraph). It is read-only/derived: it only consumes events.
type CognitionGraph struct {
	Nodes map[string]*Node // every entity, by id (insertion-tracked via nodeOrder)
	Edges []*Edge          // every relation, in first-seen order

	edgeset   map[edgeKey]struct{} // dedup set (Python _edgeset)
	nodeOrder []string             // ids in first-insertion order (Python dict preserves this)
	graph     *graph.ThoughtGraph  // live Conscious graph for the A* projection (nil == None)

	// per-tick scratch used to stitch same-tick cause->effect relations. The TEMPORAL COUPLING
	// here is load-bearing and ported exactly: TICK resets, SUB_FIRE records, INJECT/APPEND
	// consume — see OnEvent.
	process          string   // current process id ("" == None)
	lastThought      string   // most recent thought = active line tip ("" == None)
	tickFires        []firing // (domain, firing_id) fired this tick
	pendingInjection string   // ("" == None)
	lastDecision     string   // ("" == None)
	pendingIntention string   // ("" == None)
	injSeq           int
}

// firing is one (domain, firing_id) pair recorded for the current tick (Python's
// _tick_fires list of 2-tuples).
type firing struct {
	domain   string
	firingID string
}

// New builds an empty CognitionGraph, exactly as the Python __init__ leaves every scratch
// field None/empty.
func New() *CognitionGraph {
	return &CognitionGraph{
		Nodes:     map[string]*Node{},
		Edges:     []*Edge{},
		edgeset:   map[edgeKey]struct{}{},
		nodeOrder: []string{},
	}
}

// -- wiring --------------------------------------------------------------

// Attach subscribes to a live bus and returns the unsubscribe handle (Python attach).
func (c *CognitionGraph) Attach(bus *events.Bus) (unsubscribe func()) {
	return bus.Subscribe(c.OnEvent)
}

// BindGraph binds the live Conscious graph so Search (the A* projection) is available here too —
// one object exposing both the structural view and the cross-layer provenance (Python
// bind_graph).
func (c *CognitionGraph) BindGraph(g *graph.ThoughtGraph) { c.graph = g }

// Search returns the A* projection over the bound Conscious graph, or nil when no graph is bound
// (Python's `search` property: SearchView(self._graph) if self._graph is not None else None).
func (c *CognitionGraph) Search() *search.View {
	if c.graph == nil {
		return nil
	}
	return search.New(c.graph)
}

// -- node/edge helpers ---------------------------------------------------

// node creates (or refreshes) a node and returns it. On first creation it scopes the node to
// the current process via a 'contains' edge (Python _node). On a repeat it refreshes the
// mutable attrs and, if a non-empty label is supplied, the label — mirroring Python's
// n.attrs.update(attrs); if label: n.label = label.
func (c *CognitionGraph) node(id, typ, layer, label string, tick int, attrs map[string]any) *Node {
	n, ok := c.Nodes[id]
	if !ok {
		na := map[string]any{}
		for k, v := range attrs { // dict(attrs): a fresh per-node map, never aliasing the caller's
			na[k] = v
		}
		n = &Node{ID: id, Type: typ, Layer: layer, Label: label, Process: c.process, Tick: tick, Attrs: na}
		c.Nodes[id] = n
		c.nodeOrder = append(c.nodeOrder, id)
		if c.process != "" && id != c.process {
			c.edge(c.process, "contains", id)
		}
	} else { // refresh mutable attrs (e.g. a thought's confidence after a rerank)
		for k, v := range attrs {
			n.Attrs[k] = v
		}
		if label != "" {
			n.Label = label
		}
	}
	return n
}

// tn scopes a thought id by the current process (Python _tn). Thought/branch ids restart at 1
// each episode (a fresh ThoughtGraph), so scoping keeps ep:1's th:1 distinct from ep:2's th:1.
// The id value is formatted with Python str() semantics (int -> "5", float -> "5.0").
func (c *CognitionGraph) tn(i any) string { return "th:" + c.process + ":" + pyStr(i) }

// bn scopes a branch id by the current process (Python _bn).
func (c *CognitionGraph) bn(b any) string { return "br:" + c.process + ":" + pyStr(b) }

// Process returns the current process (episode) id ("" before any episode). Exported for A-RAG3 so the
// engine can form the SAME process-scoped node ids the fold uses (th:{process}:{id}, fact:{process}:{seq}).
func (c *CognitionGraph) Process() string { return c.process }

// ThoughtNodeID returns the process-scoped node id of a thought (th:{process}:{id}) — the same id the
// Append fold assigns. The engine uses it as the START node for graph-native recall and as the GROUNDS
// source for a write-back, so both anchor on the live conscious line. Exported counterpart of tn.
func (c *CognitionGraph) ThoughtNodeID(id int) string { return c.tn(id) }

// FactNodeID returns the process-scoped node id of a written-back reality fact (fact:{process}:{seq}).
// The engine forms this id and rides it on the GraphWriteBack event; the fold creates the node under it.
func (c *CognitionGraph) FactNodeID(seq int) string { return "fact:" + c.process + ":" + pyStr(seq) }

// BranchNodeID returns the process-scoped node id of a branch (br:{process}:{id}) — the LINE HUB that
// graph-native recall anchors on. From the hub, the line's own thoughts (branch-`contains`) and their
// grounded facts (one more hop) are within the GraphRAG-Local radius. Exported counterpart of bn.
func (c *CognitionGraph) BranchNodeID(id int) string { return c.bn(id) }

// edge appends a deduplicated directed edge (Python _edge): no self-loops, no empties, no
// duplicates. First-seen order is preserved.
func (c *CognitionGraph) edge(src, rel, dst string) {
	if src == "" || dst == "" || src == dst {
		return
	}
	k := edgeKey{src, rel, dst}
	if _, seen := c.edgeset[k]; seen {
		return
	}
	c.edgeset[k] = struct{}{}
	c.Edges = append(c.Edges, &Edge{Src: src, Rel: rel, Dst: dst})
}

// -- the event -> model mapping -----------------------------------------

// OnEvent folds one event into the model (Python on_event). It is a long if-chain keyed on
// ev.Kind doing typed data lookups with defaults; the per-tick scratch stitches same-tick
// cause->effect edges. The temporal coupling (TICK resets, SUB_FIRE records, INJECT/APPEND
// consume) is preserved EXACTLY.
func (c *CognitionGraph) OnEvent(ev events.Event) {
	d, t := ev.Data, ev.Tick

	switch ev.Kind {
	case events.Tick:
		c.tickFires = nil
		c.pendingInjection = ""
		return

	case events.Episode:
		c.process = getStr(d, "process", "")
		c.node(c.process, "episode", "process", runeTrunc(getStr(d, "goal", ""), 60), t,
			map[string]any{"goal": getStr(d, "goal", "")})
		c.lastThought = ""
		c.lastDecision = ""
		c.pendingIntention = ""
		return

	case events.Goal:
		gid := "goal:" + pyStr(d["id"])
		c.node(gid, "goal", "process",
			"goal ["+pyStr(d["source"])+"]: "+runeTrunc(pyStr2(d["text"], ""), 48), t,
			map[string]any{"source": d["source"], "text": d["text"]})
		if c.process != "" { // the goal (this process) was satisfied by matching this skill
			c.edge(c.process, "pursues", gid)
		}
		return

	case events.SkillMatch:
		name := getStrDefault(d, "skill", "?")
		sk := "sk:" + name
		c.node(sk, "skill", "subconscious", "skill "+name+" ("+pyStr(d["tier"])+")", t,
			map[string]any{"tier": d["tier"], "shape": d["shape"], "sub_skills": d["sub_skills"]})
		if c.process != "" { // the goal (this process) was satisfied by matching this skill
			c.edge(c.process, "matched", sk)
		}
		return

	case events.SubFire:
		domain := getStrDefault(d, "domain", "?")
		sp := "sp:" + domain
		c.node(sp, "specialist", "subconscious", domain+" sub-agent", t,
			map[string]any{"domain": domain})
		fid := "fire:" + domain + "@" + pyStr(t)
		c.node(fid, "firing", "subconscious", domain+" fired: "+runeTrunc(pyStr2(d["text"], ""), 40), t,
			map[string]any{"domain": domain, "stance": d["stance"], "relevance": d["relevance"],
				"text": d["text"]})
		c.edge(sp, "fired", fid)
		if c.lastThought != "" { // the active line that pulled this sub-agent
			c.edge(c.lastThought, "triggered", fid)
		}
		c.tickFires = append(c.tickFires, firing{domain: domain, firingID: fid})
		return

	case events.Inject:
		c.injSeq++
		iid := "inj:" + pyStr(t) + "#" + pyStr(c.injSeq)
		domain := getStrDefault(d, "domain", "?")
		c.node(iid, "injection", "seam", "injected ("+domain+")", t,
			map[string]any{"domain": domain, "confidence": d["confidence"]})
		for _, f := range c.tickFires { // the firing whose candidate won the gate
			if f.domain == domain {
				c.edge(f.firingID, "admitted_as", iid)
				break
			}
		}
		c.pendingInjection = iid
		return

	case events.SubWorkflow:
		name := getStrDefault(d, "workflow", "workflow")
		wf := "wf:" + name
		c.node(wf, "workflow", "subconscious", "workflow "+name, t,
			map[string]any{"name": name})
		ph := "ph:" + name + "#" + pyStr(d["phase"])
		c.node(ph, "phase", "subconscious", "phase "+pyStr(d["phase"])+" ("+pyStr(d["operator"])+")", t,
			map[string]any{"operator": d["operator"], "index": d["phase"]})
		c.edge(wf, "has_phase", ph)
		return

	case events.Append:
		tid := c.tn(d["id"])
		src := getStr(d, "source", "")
		bid := c.bn(d["branch"])
		c.node(bid, "branch", "conscious", "branch "+pyStr(d["branch"]), t, nil)
		c.node(tid, "thought", "conscious", "["+src+"] "+runeTrunc(pyStr2(d["text"], ""), 48), t,
			map[string]any{"source": src, "branch": d["branch"], "confidence": d["confidence"],
				"text": d["text"]})
		c.edge(bid, "contains", tid)
		if src == "INJECTED" && c.pendingInjection != "" {
			c.edge(c.pendingInjection, "voiced_as", tid)
			c.pendingInjection = ""
		}
		if _, isObs := obsSources[src]; isObs && c.pendingIntention != "" {
			c.edge(c.pendingIntention, "returned", tid)
			c.pendingIntention = ""
		}
		c.lastThought = tid
		return

	case events.Decision:
		if truthy(d["soft"]) || strings.Contains(ev.Summary, "escalate") { // the soft-stop / escalation notes
			return
		}
		did := "dec:" + pyStr(t)
		c.node(did, "decision", "critic", pyStr(d["decision"])+": "+runeTrunc(pyStr2(d["reason"], ""), 40),
			t, map[string]any{"decision": d["decision"], "reason": d["reason"]})
		if c.lastThought != "" {
			bid := c.Nodes[c.lastThought].Attrs["branch"]
			c.edge(did, "decided_on", c.bn(bid))
		}
		c.lastDecision = did
		return

	case events.XRef:
		src, dst := c.bn(d["src"]), c.bn(d["dst"])
		c.node(src, "branch", "conscious", "branch "+pyStr(d["src"]), t, nil)
		c.node(dst, "branch", "conscious", "branch "+pyStr(d["dst"]), t, nil)
		c.edge(src, strings.ToLower(pyStr2(d["kind"], "xref")), dst) // contradicts / supersedes / supports
		return

	case events.MCP:
		if pyStr2(d["op"], "") != "branch" {
			return
		}
		newB := c.bn(d["branch"])
		c.node(newB, "branch", "conscious", "branch "+pyStr(d["branch"])+": "+runeTrunc(pyStr2(d["reason"], ""), 32),
			t, map[string]any{"reason": d["reason"]})
		if c.lastDecision != "" {
			c.edge(c.lastDecision, "caused", newB)
		}
		return

	case events.Intention:
		iid := "int:" + pyStr(t)
		c.node(iid, "intention", "action", "intention ("+pyStr(d["kind"])+"): "+runeTrunc(ev.Summary, 36),
			t, map[string]any{"kind": d["kind"]})
		if c.lastDecision != "" {
			c.edge(c.lastDecision, "produced", iid)
		}
		c.pendingIntention = iid
		return

	case events.Respond:
		rid := "resp:" + pyStr(t)
		proactive := truthy(d["proactive"])
		prefix := "answer: "
		if proactive {
			prefix = "outreach: "
		}
		label := prefix + runeTrunc(ev.Summary, 46)
		c.node(rid, "response", "action", label, t,
			map[string]any{"text": ev.Summary, "proactive": proactive})
		return

	case events.GraphWriteBack:
		// A-RAG3: a rung-4 reality fact is written BACK into the unified graph as a `fact` node + a
		// `grounds` edge from the importing line (the Zep/Graphiti pattern on the existing event-sourced
		// substrate). The fact carries its full statement + provenance in Attrs so graph-native recall can
		// score + return it. The node id is supplied by the emitter (fact:{process}:{seq}); the line is the
		// thought node id the reality observation landed on. Both are no-ops if empty (no self-loop, deduped).
		// This case only fires when subconscious.graph_recall is ON (the only emitter is gated). NOTE: that
		// gate is now ON BY DEFAULT — the A-RAG3 default-flip (config.go AllOn() GraphRecall:true, user-
		// authorized 2026-06-21) means the DEFAULT product/claude path DOES reach here, folds a `fact` node +
		// `grounds` edge, and the unified model is NO LONGER byte-identical to a pre-A-RAG3 recorded stream.
		// The fold is correct (intended behaviour); replay parity against a pre-A-RAG3 golden now requires the
		// gate to be explicitly disabled (`--disable subconscious.graph_recall`) or the goldens regenerated.
		fid := getStr(d, "node", "")
		if fid == "" {
			return
		}
		stmt := getStr(d, "statement", "")
		c.node(fid, "fact", "subconscious", "fact: "+runeTrunc(stmt, 48), t,
			map[string]any{"statement": stmt, "tool": d["tool"], "trust": d["trust"],
				"entities": d["entities"]})
		if line := getStr(d, "line", ""); line != "" {
			// the line that crossed the seam to import this reality GROUNDS the fact (read "line grounds fact").
			c.edge(line, "grounds", fid)
		}
		return
	}
}

// -- queries -------------------------------------------------------------

// nodesInOrder returns every node in first-insertion order (Python dict-values order over
// self.nodes). The query methods walk this so their result order is deterministic and matches
// Python's insertion-ordered dict iteration.
func (c *CognitionGraph) nodesInOrder() []*Node {
	out := make([]*Node, 0, len(c.nodeOrder))
	for _, id := range c.nodeOrder {
		out = append(out, c.Nodes[id])
	}
	return out
}

// ByType returns every node of the given type, in insertion order (Python by_type).
func (c *CognitionGraph) ByType(typ string) []*Node {
	out := []*Node{}
	for _, n := range c.nodesInOrder() {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

// ByLayer returns every node in the given layer, in insertion order (Python by_layer).
func (c *CognitionGraph) ByLayer(layer string) []*Node {
	out := []*Node{}
	for _, n := range c.nodesInOrder() {
		if n.Layer == layer {
			out = append(out, n)
		}
	}
	return out
}

// Processes returns every episode node — the cognitive processes (Python processes).
func (c *CognitionGraph) Processes() []*Node { return c.ByType("episode") }

// Incoming returns every edge whose dst is nodeID, in first-seen edge order (Python incoming).
func (c *CognitionGraph) Incoming(nodeID string) []*Edge {
	out := []*Edge{}
	for _, e := range c.Edges {
		if e.Dst == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// Outgoing returns every edge whose src is nodeID, in first-seen edge order (the dual of Incoming).
// Added for A-RAG3 graph-native recall, which walks the relation graph in BOTH directions.
func (c *CognitionGraph) Outgoing(nodeID string) []*Edge {
	out := []*Edge{}
	for _, e := range c.Edges {
		if e.Src == nodeID {
			out = append(out, e)
		}
	}
	return out
}

// GraphFact is one node reachable from a start node via graph traversal, with its hop distance and the
// relation chain that reached it (A-RAG3 graph-native recall). It carries the statement to be scored by
// the caller's lexical relevance so cogngraph stays free of a retrieval dependency.
type GraphFact struct {
	NodeID    string // the reached node's id
	Type      string // the node's type ("fact" / "thought" / "observation"-thought / …)
	Statement string // the recall-able text (a fact's statement, a thought's text)
	Hops      int    // graph distance from the start node (1 = direct neighbour)
	Provider  string // the relation that reached it (e.g. "graph:grounds@1")
}

// graphRecallType reports whether a node type A-RAG3 recall will SURFACE as fuel: a written-back reality
// `fact` (the Zep/Graphiti node) and an OBSERVATION-sourced `thought` (reality the conscious imported).
// A plain reasoning thought is NOT surfaced — graph recall imports GROUNDED material, not speculation.
func graphRecallType(typ, source string) bool {
	if typ == "fact" {
		return true
	}
	return typ == "thought" && source == "OBSERVATION"
}

// GraphRecall walks the relation graph OUTWARD from fromNode (both edge directions, BFS) up to maxHops
// and returns every grounded node it reaches whose statement is non-empty — the GraphRAG "Local search"
// over the unified cognition graph (A-RAG3, extraction cost already SUNK because the graph is
// reconstructed for free off the event bus). The caller scores the returned statements by lexical
// relevance to the need and picks the best — so a fact reachable ONLY via traversal (e.g. a written-back
// reality fact two hops from the active line through a prior observation, with no direct lexical overlap)
// becomes recall-able. The start node itself is excluded. One `contains` edge is special-cased: the
// EPISODE-ROOT `contains` (process membership) is NOT traversed — it would flood the walk from the
// process node to every entity in the run. BRANCH-`contains` IS traversed (the line's own prior thoughts
// are exactly the "prior thoughts/memories" the spec recalls over). Deterministic: first-seen edge order
// + a visited set. maxHops<=0 ⇒ no walk.
func (c *CognitionGraph) GraphRecall(fromNode string, maxHops int) []GraphFact {
	if maxHops <= 0 || fromNode == "" {
		return nil
	}
	if _, ok := c.Nodes[fromNode]; !ok {
		return nil
	}
	type frontierEntry struct {
		id    string
		depth int
		rel   string // the relation that first reached this node (for the provider tag)
	}
	seen := map[string]struct{}{fromNode: {}}
	out := []GraphFact{}
	frontier := []frontierEntry{{id: fromNode, depth: 0}}
	for len(frontier) > 0 {
		cur := frontier[0]
		frontier = frontier[1:]
		if cur.depth >= maxHops {
			continue
		}
		// neighbours via BOTH edge directions (outgoing dst, incoming src), first-seen order. Only the
		// EPISODE-ROOT `contains` (process membership) is skipped — it is the flood vector (the process
		// node contains every entity). The branch's own `contains` (line structure / prior thoughts) is a
		// legitimate recall path.
		step := func(neighborID, rel, edgeSrc string) {
			if neighborID == "" {
				return
			}
			if rel == "contains" && edgeSrc == c.process {
				return // process-membership scope: do not flood the walk through the episode root
			}
			if _, ok := seen[neighborID]; ok {
				return
			}
			seen[neighborID] = struct{}{}
			depth := cur.depth + 1
			frontier = append(frontier, frontierEntry{id: neighborID, depth: depth, rel: rel})
			n, ok := c.Nodes[neighborID]
			if !ok {
				return
			}
			source, _ := n.Attrs["source"].(string)
			if !graphRecallType(n.Type, source) {
				return
			}
			stmt := graphNodeStatement(n)
			if stmt == "" {
				return
			}
			out = append(out, GraphFact{
				NodeID: neighborID, Type: n.Type, Statement: stmt, Hops: depth,
				Provider: "graph:" + rel + "@" + pyStr(depth),
			})
		}
		for _, e := range c.Outgoing(cur.id) {
			step(e.Dst, e.Rel, e.Src)
		}
		for _, e := range c.Incoming(cur.id) {
			step(e.Src, e.Rel, e.Src)
		}
	}
	return out
}

// graphNodeStatement reads the recall-able text off a reached node: a fact's `statement`, else a
// thought's `text`. Both are stored in Attrs by the OnEvent fold. Returns "" when neither is present.
func graphNodeStatement(n *Node) string {
	if s, ok := n.Attrs["statement"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	if s, ok := n.Attrs["text"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return ""
}

// ProvEntry is one (depth, edge) pair from the provenance walk (Python's tuple[int, Edge]).
type ProvEntry struct {
	Depth int
	Edge  *Edge
}

// Provenance walks the cause edges backward from a node, across layers, breadth-first by depth
// (Python provenance). Returns (depth, edge) pairs — the full lineage of how this entity came
// to be. The BFS uses a FIFO frontier and a visited set keyed on the edge source, exactly as
// Python does.
func (c *CognitionGraph) Provenance(nodeID string) []ProvEntry {
	seen := map[string]struct{}{}
	out := []ProvEntry{}
	frontier := []provNode{{depth: 0, id: nodeID}}
	for len(frontier) > 0 {
		cur := frontier[0] // pop(0): FIFO, breadth-first
		frontier = frontier[1:]
		for _, e := range c.Incoming(cur.id) {
			if _, ok := seen[e.Src]; ok {
				continue
			}
			seen[e.Src] = struct{}{}
			out = append(out, ProvEntry{Depth: cur.depth, Edge: e})
			frontier = append(frontier, provNode{depth: cur.depth + 1, id: e.Src})
		}
	}
	return out
}

// provNode is one frontier entry in the provenance BFS (Python's (depth, node_id) tuple).
type provNode struct {
	depth int
	id    string
}

// Lineage renders a human-readable, cross-layer story of an entity's origin — a properly
// nested tree of the incoming (cause) edges, so each entity sits under the thing that produced
// it (Python lineage). Returns "<id>: (unknown)" for an absent node.
func (c *CognitionGraph) Lineage(nodeID string) string {
	head, ok := c.Nodes[nodeID]
	if !ok {
		return nodeID + ": (unknown)"
	}
	lines := []string{head.ID + " [" + head.Layer + "] " + head.Label}
	seen := map[string]struct{}{nodeID: {}}
	c.renderCauses(nodeID, 1, seen, &lines)
	return strings.Join(lines, "\n")
}

// renderCauses appends the nested cause tree under nid (Python _render_causes). 'contains' is
// scope/membership, not cause, so it is skipped; an already-seen source is skipped to break
// cycles. Indentation is two spaces per depth, matching Python's '  ' * depth.
func (c *CognitionGraph) renderCauses(nid string, depth int, seen map[string]struct{}, lines *[]string) {
	for _, e := range c.Incoming(nid) {
		if e.Rel == "contains" { // scope/membership, not cause
			continue
		}
		if _, ok := seen[e.Src]; ok {
			continue
		}
		seen[e.Src] = struct{}{}
		src, found := c.Nodes[e.Src]
		tag := ""
		label := e.Src
		if found {
			tag = "[" + src.Layer + "]"
			label = src.Label
		}
		*lines = append(*lines, strings.Repeat("  ", depth)+"<-"+e.Rel+"- "+e.Src+" "+tag+" "+label)
		c.renderCauses(e.Src, depth+1, seen, lines)
	}
}

// Stats is the heterogeneous summary Python's stats() returns as a dict.
type Stats struct {
	Nodes     int
	Edges     int
	Processes int
	ByLayer   map[string]int
}

// Stats returns the per-layer entity counts + totals (Python stats). ByLayer is keyed by layer
// and counts nodes in insertion order (key insertion order does not affect the counts).
func (c *CognitionGraph) Stats() Stats {
	counts := map[string]int{}
	for _, n := range c.nodesInOrder() {
		counts[n.Layer]++
	}
	return Stats{
		Nodes:     len(c.Nodes),
		Edges:     len(c.Edges),
		Processes: len(c.ByType("episode")),
		ByLayer:   counts,
	}
}

// layerOrder is the fixed render order for the summary line (Python's local `order` list).
var layerOrder = []string{"process", "subconscious", "seam", "conscious", "critic", "action"}

// Summary renders the one-line cognition summary (Python summary). Only layers present in
// ByLayer are listed, in the fixed layerOrder.
func (c *CognitionGraph) Summary() string {
	s := c.Stats()
	parts := []string{}
	for _, k := range layerOrder {
		if v, ok := s.ByLayer[k]; ok {
			parts = append(parts, k+":"+pyStr(v))
		}
	}
	return "cognition: " + pyStr(s.Nodes) + " entities / " + pyStr(s.Edges) + " links across " +
		strings.Join(parts, " ")
}
