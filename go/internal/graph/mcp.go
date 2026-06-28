// The Thought MCP — the "being able to think" interface (Tier 2, PORT-PLAN #18).
//
// Deliberate (METACOG) graph operations the CONSCIOUS layer calls on its own graph. The *same*
// ops also fire automatically from the Gate/Controller — deliberate->automatic is convertibility.
// Every op emits a conscious.mcp event and drops a METACOG marker so it is visible in the stream.
//
// CO-LOCATED with graph (not a separate package) because it mutates graph internals directly —
// it forks/merges branches via the exported mutators (NewBranch / NextID / Append / AddXref) and
// flips ActiveBranch / Branches in place. Ported from the (now-removed) Python thought_harness/mcp.py.
package graph

import (
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ThoughtMCP exposes the metacognitive ops (branch/merge/rerank/compress/expand/focus) over a
// ThoughtGraph. It holds the graph it mutates, the backend it summarises through, and the emit
// closure it traces on. Mirrors Python ThoughtMCP.
type ThoughtMCP struct {
	g       *ThoughtGraph
	backend backends.Backend
	emit    events.Emit
	Tick    int // the tick stamped on METACOG markers; advanced by the engine (Python self.tick)
}

// NewThoughtMCP builds the Thought MCP over a graph, backend and emit closure. Mirrors Python
// ThoughtMCP.__init__ (tick starts at 0).
func NewThoughtMCP(g *ThoughtGraph, backend backends.Backend, emit events.Emit) *ThoughtMCP {
	return &ThoughtMCP{g: g, backend: backend, emit: emit, Tick: 0}
}

// marker drops a METACOG breadcrumb thought ("[op] reason") on the active branch so the
// metacognitive move is visible in the stream. Mirrors Python ThoughtMCP._marker.
func (m *ThoughtMCP) marker(op, reason string) {
	m.g.Append(&types.Thought{ID: -1, Text: "[" + op + "] " + reason, Source: types.METACOG}, m.Tick)
}

// markerOn drops a METACOG breadcrumb on a SPECIFIC branch (not necessarily the active one) by
// briefly retargeting ActiveBranch around the Append (the only branch-scoped mutator). Used by
// Reenter to leave a retracement breadcrumb on the PAST decision node — the line being re-entered —
// so the old line itself records that attention traced back to it. The swap is local + restored, so
// the caller's active-branch invariant is untouched. A no-op if the branch is unknown.
func (m *ThoughtMCP) markerOn(branchID int, op, reason string) {
	if _, ok := m.g.Branches[branchID]; !ok {
		return
	}
	prev := m.g.ActiveBranch
	m.g.ActiveBranch = branchID
	m.g.Append(&types.Thought{ID: -1, Text: "[" + op + "] " + reason, Source: types.METACOG}, m.Tick)
	m.g.ActiveBranch = prev
}

// -- branch --------------------------------------------------------------

// Branch forks the context. The active branch stays in focus; the fork is a COMPRESSED, STASHED
// sibling to revisit. Pass seed=nil for an empty fork. Returns the new branch id. Mirrors Python
// ThoughtMCP.branch.
func (m *ThoughtMCP) Branch(reason string, seed *types.Thought) int {
	m.marker("branch", reason)
	parent := m.g.ActiveBranch
	r := reason
	newID := m.g.NewBranch(&parent, &r)
	nb := m.g.Branches[newID]
	nb.Resolution = types.COMPRESSED
	nb.Status = types.STASHED
	if seed != nil {
		// Copy, never mutate the caller's Thought: the seed may already be appended to the active
		// branch (continuous-mode interrupt), and re-id'ing it in place would register one object
		// under two ids in two branches (graph corruption). The explicit struct copy resets
		// id/branch_id/parent — faithful to Python's replace(seed, id=-1, branch_id=None, parent=None).
		fresh := *seed
		fresh.ID = -1
		fresh.BranchID = nil
		fresh.Parent = nil
		prevActive := m.g.ActiveBranch
		m.g.ActiveBranch = newID
		appended := m.g.Append(&fresh, m.Tick)
		m.g.ActiveBranch = prevActive
		summary := m.backend.Summarize([]types.Thought{*appended})
		nb.Summary = &summary
	}
	m.emit(events.MCP, "branch -> b"+strconv.Itoa(newID)+": "+reason,
		events.D{"op": "branch", "branch": newID, "reason": reason})
	return newID
}

// -- merge ---------------------------------------------------------------

// Merge folds branch b into branch a: two branches are the same point / should combine into one
// line of thought. Ownership of b's thoughts transfers to a, b is marked MERGED (terminal, holds
// no live thoughts), and a SUPERSEDES xref records that a replaced b. Returns the surviving branch
// id (a). A no-op (returns a) if a==b or b is unknown. Mirrors Python ThoughtMCP.merge.
func (m *ThoughtMCP) Merge(a, b int) int {
	if a == b {
		return a
	}
	if _, ok := m.g.Branches[b]; !ok {
		return a
	}
	ba, bb := m.g.Branches[a], m.g.Branches[b]
	for _, tid := range bb.ThoughtIDs {
		if !contains(ba.ThoughtIDs, tid) {
			ba.ThoughtIDs = append(ba.ThoughtIDs, tid)
			aID := a
			m.g.Nodes[tid].BranchID = &aID // ownership transfers to the surviving branch
		}
	}
	bb.ThoughtIDs = []int{} // a MERGED branch holds no live thoughts (§5.1, terminal)
	bb.Status = types.MERGED
	m.marker("merge", "b"+strconv.Itoa(b)+" into b"+strconv.Itoa(a))
	m.emit(events.MCP, "merge b"+strconv.Itoa(b)+" -> b"+strconv.Itoa(a),
		events.D{"op": "merge", "into": a, "gone": b})
	if m.g.AddXref(a, "SUPERSEDES", b) { // the surviving line supersedes the merged one
		m.emit(events.XRef, "b"+strconv.Itoa(a)+" SUPERSEDES b"+strconv.Itoa(b),
			events.D{"src": a, "kind": "SUPERSEDES", "dst": b})
	}
	return a
}

// -- rerank --------------------------------------------------------------

// Rerank reprioritises the frontier by the value signal (best-first ordering of stashed siblings,
// excluding active/DEAD/MERGED). Returns the branch ids in rerank order. Mirrors Python
// ThoughtMCP.rerank.
func (m *ThoughtMCP) Rerank() []int {
	front := m.g.Frontier() // excludes active, DEAD, MERGED
	order := make([]int, 0, len(front))
	for _, b := range front {
		order = append(order, b.ID)
	}
	m.emit(events.MCP, "rerank -> "+head4(order),
		events.D{"op": "rerank", "order": order})
	return order
}

// -- compress ------------------------------------------------------------

// Compress fades an inactive branch to gist (frees focus). LOSSY BY DESIGN. Mirrors Python
// ThoughtMCP.compress.
func (m *ThoughtMCP) Compress(branchID int) {
	b := m.g.Branches[branchID]
	summary := m.backend.Summarize(m.g.BranchThoughts(branchID))
	b.Summary = &summary
	b.Resolution = types.COMPRESSED
	if branchID != m.g.ActiveBranch && b.Status == types.ACTIVE {
		b.Status = types.STASHED
	}
	m.emit(events.MCP, "compress b"+strconv.Itoa(branchID)+": "+summary,
		events.D{"op": "compress", "branch": branchID})
}

// -- expand --------------------------------------------------------------

// Expand restores a stashed branch to full detail (resolution only; Focus sets ACTIVE). Mirrors
// Python ThoughtMCP.expand.
func (m *ThoughtMCP) Expand(branchID int) {
	b := m.g.Branches[branchID]
	b.Resolution = types.EXPANDED
	m.emit(events.MCP, "expand b"+strconv.Itoa(branchID),
		events.D{"op": "expand", "branch": branchID})
}

// -- focus ---------------------------------------------------------------

// Focus switches the active branch: it compresses the one we leave and expands the one we enter.
// The ordering is load-bearing (compress-leaving -> stash-leaving -> expand-entering ->
// activate-entering -> swap ActiveBranch) so exactly one branch is ACTIVE at all times (§5.1).
// A no-op if branchID is already active or unknown. Mirrors Python ThoughtMCP.focus.
func (m *ThoughtMCP) Focus(branchID int) {
	if branchID == m.g.ActiveBranch {
		return
	}
	if _, ok := m.g.Branches[branchID]; !ok {
		return
	}
	leaving := m.g.ActiveBranch
	m.marker("focus", "b"+strconv.Itoa(leaving)+" -> b"+strconv.Itoa(branchID))
	m.Compress(leaving)
	m.g.Branches[leaving].Status = types.STASHED
	m.Expand(branchID)
	m.g.Branches[branchID].Status = types.ACTIVE // exactly one ACTIVE branch (§5.1)
	m.g.ActiveBranch = branchID
	m.emit(events.MCP, "focus b"+strconv.Itoa(leaving)+" -> b"+strconv.Itoa(branchID),
		events.D{"op": "focus", "branch": branchID})
}

// -- reenter -------------------------------------------------------------

// Reenter re-opens a PAST decision node with a late injection (02-conscious.md §2b; 04-seams.md §3.3).
// A late subconscious injection — the "light-bulb after the calculation came back" — arrives anchored
// to a decision node the conscious has already passed; re-validating which branch to traverse there is
// meaningful, so the conscious RE-ENTERS and thinks forward again.
//
// Unlike Branch (which forks only from the ACTIVE branch and parks the sibling, mcp.go:Branch) and
// Focus (which targets any branch but never forks, mcp.go:Focus), Reenter is the missing composite:
// fork a NEW line from the TARGET branch + Focus to it + record a RETRACEMENT. The graph FORKS —
// nothing is overwritten: the old (target) line stays exactly as it was, only no longer the EXPANDED
// one, and the timeline appends the re-entry (02 §2a: graph forks, timeline appends). The one-way
// mirror is intact: the conscious experiences "a new thought about this earlier decision", not "the
// seam moved me" — the seam PROPOSES (anchor + retracement), the Controller FIRES this op (04 §3.3).
//
// Branch-granular (DECIDED, 02 §2b): the target IS the decision node; node-granular re-entry (splitting
// a line at an exact thought) is deferred until a benchmark shows the coarser anchor loses the thread.
//
// Pass seed=nil for an empty re-entry line, or a Thought (the late injection) to seed it — the seed is
// COPIED (never the caller's object, never re-id'd in place) so it cannot corrupt the graph. Returns
// the new fork's branch id, or -1 (a no-op) if target is unknown or is the active branch (re-entering
// the line you are already on is meaningless — that is just thinking forward, not a retracement).
func (m *ThoughtMCP) Reenter(target int, reason string, seed *types.Thought) int {
	if _, ok := m.g.Branches[target]; !ok {
		return -1 // stale/unknown anchor -> no-op (the seam may anchor to a pruned id)
	}
	if target == m.g.ActiveBranch {
		return -1 // already on this line -> no retracement to make (Focus/Branch is the right move)
	}

	// 1. fork a NEW line parented on the TARGET (not the active branch — that is the whole point). The
	// PAST decision node (target) is left exactly as it was: non-destructive, nothing overwritten
	// (02 §2b / 04 §6 — graph forks, the old line stays). It starts COMPRESSED/STASHED like any fork;
	// Focus (step 4) flips it to EXPANDED/ACTIVE.
	parent := target
	r := reason
	newID := m.g.NewBranch(&parent, &r)
	nb := m.g.Branches[newID]
	nb.Resolution = types.COMPRESSED
	nb.Status = types.STASHED

	// 2. leave the retracement breadcrumb on the NEW re-entry line (not the past node — the past node
	// stays untouched). This is "a new thought about this earlier decision" (the one-way mirror, 04
	// §3.3): the re-entry line OPENS with the [reenter] marker recording where it traced back from.
	m.markerOn(newID, "reenter", "from b"+strconv.Itoa(target)+": "+reason)

	// 3. optionally SEED the re-entry line with the late injection (the light-bulb), copied so the
	// caller's Thought is never re-id'd in place (graph corruption — same discipline as Branch).
	if seed != nil {
		fresh := *seed
		fresh.ID = -1
		fresh.BranchID = nil
		fresh.Parent = nil
		prevActive := m.g.ActiveBranch
		m.g.ActiveBranch = newID
		appended := m.g.Append(&fresh, m.Tick)
		m.g.ActiveBranch = prevActive
		summary := m.backend.Summarize([]types.Thought{*appended})
		nb.Summary = &summary
	}

	// 4. Focus to the re-entry line — compress the line we were on, expand+activate the fork. After
	// this exactly one branch is ACTIVE (the new fork) and the old line is preserved, compressed.
	m.Focus(newID)

	// 5. emit the RETRACEMENT as a distinct conscious.mcp event (op=reenter) carrying the target it
	// traced back to and the new fork — so the episodic timeline reads it as ONE retracement, not two
	// ordinary moves. Reuses the existing conscious.mcp kind (no new event kind — the vocabulary gate
	// holds); a dedicated conscious.retracement kind is a possible future refinement (see the package's
	// callers / the integration notes).
	m.emit(events.MCP, "reenter b"+strconv.Itoa(target)+" -> b"+strconv.Itoa(newID)+": "+reason,
		events.D{"op": "reenter", "target": target, "branch": newID, "reason": reason})
	return newID
}

// -- local helpers -------------------------------------------------------

// contains reports whether xs holds v (the membership test Python's `tid not in ba.thought_ids`).
func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// head4 formats the first four ids of an order as Python's `f"{order[:4]}"` — a list repr like
// "[2, 1]" (the summary string; the full order rides in the event data, unrounded).
func head4(order []int) string {
	n := len(order)
	if n > 4 {
		n = 4
	}
	out := "["
	for i := 0; i < n; i++ {
		if i > 0 {
			out += ", "
		}
		out += strconv.Itoa(order[i])
	}
	return out + "]"
}
