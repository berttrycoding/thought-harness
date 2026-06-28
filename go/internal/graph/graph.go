// Package graph holds the thought graph — the Conscious substrate (Tier 1).
//
// Re-entrant and indexed: thoughts are counted, so each is an addressable node you can branch
// from or return to. Forks into branches; exactly one branch is EXPANDED, the rest COMPRESSED to
// gist (bounded focus). This is the parallel->serial conversion at the heart of the Conscious layer.
//
// Ported from the (now-removed) Python thought_harness/graph.py. The mutators mcp.go (Tier 2, co-located in
// this package, NOT written here) needs are EXPORTED. The form_intention regex router lives in
// intention.go.
//
// Mutate-in-place semantics: nodes and branches are held as map[int]*T so a mutation through any
// reference (the value signal writing Branch.Value, mcp transferring thought ownership) is seen
// by all readers — exactly like Python's dataclass-by-reference dicts.
package graph

import (
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// XRefTypes are the typed cross-reference edges between branches (beyond the parent/came_from
// tree): a CONTRADICTS edge is the Gate's fork-on-conflict made first-class + durable; SUPERSEDES
// marks a branch replaced by another (principled compaction — the superseded branch is droppable
// first); SUPPORTS is reinforcement. (an earlier-project concept, made tractable by the Gate that already
// detects the conflict.) Mirrors Python XREF_TYPES (order preserved).
var XRefTypes = []string{"CONTRADICTS", "SUPERSEDES", "SUPPORTS"}

// isXRefType reports whether kind is one of the legal cross-reference edge types.
func isXRefType(kind string) bool {
	for _, k := range XRefTypes {
		if k == kind {
			return true
		}
	}
	return false
}

// XRef is a typed cross-reference edge (src_branch, type, dst_branch). The Python list of
// 3-tuples becomes a named struct slice so call sites read the fields by name.
type XRef struct {
	Src  int
	Kind string
	Dst  int
}

// ThoughtGraph is the Conscious substrate: an indexed, re-entrant thought graph that forks into
// branches. Nodes and branches are stored by id as pointers (mutate-in-place semantics).
type ThoughtGraph struct {
	Goal     string                 // the episode goal (root thought text)
	Nodes    map[int]*types.Thought // every thought, by id — the addressable node set
	Branches map[int]*types.Branch  // every branch, by id
	Xrefs    []XRef                 // typed cross-references between branches

	ActiveBranch int // the EXPANDED branch's id

	tid int // the thought counter
	bid int // the branch counter (starts at -1, _new_branch pre-increments)

	// deliveredUpTo is the conversational high-water mark: a USER_INPUT thought with id <= this
	// has been answered (a response crossed the watched seam after it arrived). The zero value is
	// correct — thought ids start at 1, so before any delivery every user thought is unresolved.
	deliveredUpTo int
}

// New constructs a ThoughtGraph for a goal: it mints the root branch and appends the goal as the
// first thought (GENERATED). Mirrors Python ThoughtGraph.__init__.
func New(goal string) *ThoughtGraph {
	g := &ThoughtGraph{
		Goal:     goal,
		Nodes:    map[int]*types.Thought{},
		Branches: map[int]*types.Branch{},
		Xrefs:    []XRef{},
		tid:      0,
		bid:      -1,
	}
	g.ActiveBranch = g.newBranch(nil, nil)
	// The root thought is built with an explicit id from nextID (not -1), so Append does not
	// re-number it — faithful to Python's `self.append(Thought(self._next_id(), goal, GENERATED))`.
	root := &types.Thought{ID: g.nextID(), Text: goal, Source: types.GENERATED}
	g.Append(root, 0)
	return g
}

// -- primitives ----------------------------------------------------------

// nextID advances and returns the thought counter (Python _next_id).
func (g *ThoughtGraph) nextID() int {
	g.tid++
	return g.tid
}

// newBranch mints a new branch with the given optional parent + reason and returns its id
// (Python _new_branch). Exported wrappers (NewBranch) below give mcp.go access.
func (g *ThoughtGraph) newBranch(parent *int, reason *string) int {
	g.bid++
	b := types.NewBranch(g.bid)
	b.ParentBranch = parent
	b.Reason = reason
	g.Branches[g.bid] = &b
	return g.bid
}

// NewBranch mints a new branch (parent/reason optional, pass nil for none) and returns its id.
// EXPORTED for mcp.go (Tier 2), which forks/merges branches. Mirrors Python ThoughtGraph._new_branch
// (the leading-underscore "private" name is exported in Go because the co-located MCP mutates it).
func (g *ThoughtGraph) NewBranch(parent *int, reason *string) int {
	return g.newBranch(parent, reason)
}

// NextID advances and returns the thought counter. EXPORTED for mcp.go, which mints METACOG
// thoughts directly. Mirrors Python ThoughtGraph._next_id.
func (g *ThoughtGraph) NextID() int { return g.nextID() }

// -- conversational resolution (§4.12; mandate 2026-06-12 A1/A4) ----------

// StampGoalSource re-stamps the root (goal) thought's source with its TRUE provenance. New mints
// the root GENERATED; the engine, which alone knows who seeded the episode, corrects it to
// USER_INPUT for a user turn. The graph must hold honest provenance for conversational
// resolution (UnresolvedUserInput) to be derivable from it.
func (g *ThoughtGraph) StampGoalSource(src types.Source) {
	if b, ok := g.Branches[0]; ok && len(b.ThoughtIDs) > 0 {
		if t := g.Nodes[b.ThoughtIDs[0]]; t != nil {
			t.Source = src
		}
	}
}

// MarkDelivered records that an answer crossed the watched seam: every thought minted so far —
// including every USER_INPUT — is now conversationally resolved. Resolution is GRAPH STATE
// (derivable by anyone holding the graph), never a sticky engine flag. NOT a reward-bearing
// observation: the engine marking its own speech "ok" would be self-grading (value.go forbids it);
// this is positional bookkeeping only.
func (g *ThoughtGraph) MarkDelivered() { g.deliveredUpTo = g.tid }

// UserWaiting reports whether ANY live (non-DEAD) branch holds an unanswered USER_INPUT — the
// graph-derived "a user is waiting" (A4). One definition, shared by the engine (scheduler/outreach)
// and the Controller's terminal fork (DELIVER vs silent STOP): a pruned line is skipped — pressure
// with no resumable line would wedge the loop.
func (g *ThoughtGraph) UserWaiting() bool {
	for bid, b := range g.Branches {
		if b.Status == types.DEAD {
			continue
		}
		if g.UnresolvedUserInput(bid) {
			return true
		}
	}
	return false
}

// UnresolvedUserInput reports whether branch bid holds a USER_INPUT thought no delivery has
// answered yet (id beyond the high-water mark). The Pattern-A primitive behind the
// pending-conversational-goal term in V(s) (value.AppraiseBranch) and the derived
// "a user is waiting" condition — the §4.12 interrupt re-seed persists exactly as long as
// this is true.
func (g *ThoughtGraph) UnresolvedUserInput(bid int) bool {
	b, ok := g.Branches[bid]
	if !ok {
		return false
	}
	for _, tid := range b.ThoughtIDs {
		if t := g.Nodes[tid]; t != nil && t.Source == types.USER_INPUT && t.ID > g.deliveredUpTo {
			return true
		}
	}
	return false
}

// Append adds a thought to the active branch. If the thought's id is negative it is assigned the
// next counter value (so callers can defer id allocation). The thought's branch_id and tick are
// stamped, and its parent is wired to the branch's previous tip unless already set. Returns the
// (now-registered) thought pointer. Mirrors Python ThoughtGraph.append.
//
// EXPORTED — mcp.go appends METACOG thoughts and the engine appends across the loop.
func (g *ThoughtGraph) Append(t *types.Thought, tick int) *types.Thought {
	if t.ID < 0 {
		t.ID = g.nextID()
	}
	ab := g.ActiveBranch
	t.BranchID = &ab
	t.Tick = tick
	b := g.Branches[g.ActiveBranch]
	if len(b.ThoughtIDs) > 0 && t.Parent == nil {
		prev := b.ThoughtIDs[len(b.ThoughtIDs)-1]
		t.Parent = &prev
	}
	g.Nodes[t.ID] = t
	b.ThoughtIDs = append(b.ThoughtIDs, t.ID)
	return t
}

// -- views ---------------------------------------------------------------

// Active returns the currently EXPANDED branch (Python ThoughtGraph.active).
func (g *ThoughtGraph) Active() *types.Branch { return g.Branches[g.ActiveBranch] }

// ActiveContext returns the active branch at FULL resolution — what the Subconscious reads and the Conscious reasons
// over (Python ThoughtGraph.active_context).
func (g *ThoughtGraph) ActiveContext() []types.Thought {
	return g.branchThoughtsByID(g.ActiveBranch)
}

// BranchThoughts returns a branch's thoughts in order (Python ThoughtGraph.branch_thoughts).
func (g *ThoughtGraph) BranchThoughts(bid int) []types.Thought {
	return g.branchThoughtsByID(bid)
}

// branchThoughtsByID materialises a branch's thought_ids into a Thought slice (value copies of
// the node structs, matching Python's list comprehension which yields the live objects). The
// values are copies so a caller cannot mutate a node through the returned slice; mutators that
// must write a node do so via the Nodes map. Python returns the live objects, but every reader of
// branch_thoughts only READS; the mutate-in-place writers go through Nodes directly.
func (g *ThoughtGraph) branchThoughtsByID(bid int) []types.Thought {
	b := g.Branches[bid]
	out := make([]types.Thought, 0, len(b.ThoughtIDs))
	for _, i := range b.ThoughtIDs {
		out = append(out, *g.Nodes[i])
	}
	return out
}

// History returns every thought (re-entrant: the Conscious layer can re-read and re-process its own prior
// thoughts). Order matches Python dict insertion order (id-ascending, since ids are monotonic).
// Mirrors Python ThoughtGraph.history.
func (g *ThoughtGraph) History() []types.Thought {
	ids := make([]int, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids) // ids are assigned monotonically, so this reproduces Python's insertion order
	out := make([]types.Thought, 0, len(ids))
	for _, id := range ids {
		out = append(out, *g.Nodes[id])
	}
	return out
}

// Last returns the active branch's tip thought, or nil if the branch is empty (Python
// ThoughtGraph.last). Returns a pointer into the live Nodes map so mcp/engine can mutate it.
func (g *ThoughtGraph) Last() *types.Thought {
	ids := g.Active().ThoughtIDs
	if len(ids) == 0 {
		return nil
	}
	return g.Nodes[ids[len(ids)-1]]
}

// StashedBranches returns every live branch that is not the active one (Python
// ThoughtGraph.stashed_branches). Iteration order is by branch id (Python dict insertion order =
// id-ascending, since bids are monotonic) so frontier/rerank are deterministic.
func (g *ThoughtGraph) StashedBranches() []*types.Branch {
	ids := make([]int, 0, len(g.Branches))
	for id := range g.Branches {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]*types.Branch, 0, len(ids))
	for _, id := range ids {
		b := g.Branches[id]
		if b.ID != g.ActiveBranch && (b.Status == types.ACTIVE || b.Status == types.STASHED) {
			out = append(out, b)
		}
	}
	return out
}

// Frontier returns the stashed siblings ordered by value (best-first) — the rerank frontier (the
// A* open set). O(b log b) where b is the live-branch count, kept small (~focus_capacity) by
// pruning. Mirrors Python ThoughtGraph.frontier (sorted by value, reverse=True).
//
// sort.SliceStable is used so equal-value branches keep StashedBranches' id-ascending order,
// matching Python's stable Timsort over the already-id-ordered stashed list.
func (g *ThoughtGraph) Frontier() []*types.Branch {
	out := g.StashedBranches()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	return out
}

// ReconstructPath walks parent pointers from a thought back to the root — A*'s came_from path
// reconstruction. Returns the line of reasoning that led here, root-first. Pass nil for thoughtID
// to default to the active branch's latest thought. Mirrors Python ThoughtGraph.reconstruct_path.
func (g *ThoughtGraph) ReconstructPath(thoughtID *int) []types.Thought {
	ids := g.Active().ThoughtIDs
	var tid *int
	if thoughtID != nil {
		tid = thoughtID
	} else if len(ids) > 0 {
		last := ids[len(ids)-1]
		tid = &last
	}
	path := []types.Thought{}
	seen := map[int]struct{}{}
	for tid != nil {
		t, ok := g.Nodes[*tid]
		if !ok {
			break
		}
		if _, dup := seen[*tid]; dup {
			break
		}
		seen[*tid] = struct{}{}
		path = append(path, *t)
		tid = t.Parent
	}
	// reverse in place (root-first)
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// AddXref records a typed cross-reference between two branches (dedup). Returns true if new.
// Mirrors Python ThoughtGraph.add_xref. EXPORTED — the Gate (via mcp/engine) records CONTRADICTS
// /SUPERSEDES/SUPPORTS edges.
func (g *ThoughtGraph) AddXref(src int, kind string, dst int) bool {
	if !isXRefType(kind) || src == dst {
		return false
	}
	for _, x := range g.Xrefs {
		if x.Src == src && x.Kind == kind && x.Dst == dst {
			return false
		}
	}
	g.Xrefs = append(g.Xrefs, XRef{Src: src, Kind: kind, Dst: dst})
	return true
}

// Superseded returns the set of branches that another branch SUPERSEDES — droppable first during
// compaction. Mirrors Python ThoughtGraph.superseded (returns a set).
func (g *ThoughtGraph) Superseded() map[int]struct{} {
	out := map[int]struct{}{}
	for _, x := range g.Xrefs {
		if x.Kind == "SUPERSEDES" {
			out[x.Dst] = struct{}{}
		}
	}
	return out
}

// StateKey returns a coarse canonical key for a branch's *state* — the significant words of its
// latest real (non-METACOG) thought. Lets duplicate-state detection (merge) use exact key
// equality, A*-style, before falling back to fuzzy similarity. Mirrors Python
// ThoughtGraph.state_key.
//
// Faithful to Python: filter out METACOG thoughts, take the LAST remaining, lower-case + split on
// whitespace, keep words longer than 3 chars, dedup to a set, sort ascending, join with spaces,
// truncate to 96 BYTES (Python str slicing on an ASCII-after-lower string; the significant words
// are word-tokens with len>3 so this stays byte/rune-equivalent for the ASCII corpus).
func (g *ThoughtGraph) StateKey(bid int) string {
	thoughts := g.BranchThoughts(bid)
	var real []types.Thought
	for _, t := range thoughts {
		if t.Source != types.METACOG {
			real = append(real, t)
		}
	}
	if len(real) == 0 {
		return ""
	}
	last := real[len(real)-1]
	set := map[string]struct{}{}
	for _, w := range strings.Fields(strings.ToLower(last.Text)) {
		if len([]rune(w)) > 3 {
			set[w] = struct{}{}
		}
	}
	words := make([]string, 0, len(set))
	for w := range set {
		words = append(words, w)
	}
	sort.Strings(words)
	joined := strings.Join(words, " ")
	r := []rune(joined)
	if len(r) > 96 {
		joined = string(r[:96])
	}
	return joined
}

// -- structural depth for the TUI ---------------------------------------

// Depth returns how many parent-branch hops separate a branch from a root branch (Python
// ThoughtGraph.depth).
func (g *ThoughtGraph) Depth(bid int) int {
	d := 0
	b := g.Branches[bid]
	for b.ParentBranch != nil {
		d++
		b = g.Branches[*b.ParentBranch]
	}
	return d
}
