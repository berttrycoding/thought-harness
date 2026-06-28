// Package search is the A*-shaped, read-only projection over the thought graph (Tier 2).
//
// The thought graph IS best-first graph search — this view makes the A* correspondence explicit.
// Thinking is continuous best-first search over the thought-space, with the value signal as the
// heuristic; the unified data model (Thought/Branch + the ThoughtGraph container) is exactly the
// data a graph search needs. Side by side with the canonical A*:
//
//	A* (the ~25-line classic)              this model (thought graph)
//	-------------------------              ---------------------------
//	open    priority queue, f = g + h      View.Open()  — Frontier(): branches by value
//	h       the heuristic                  Branch.Value — the value signal V(s)
//	g       cost so far                    branch depth / thought count (implicit)
//	graph / closed set                     ThoughtGraph.Nodes ; DEAD/MERGED branches
//	came_from   parent pointers            Thought.Parent / Branch.ParentBranch
//	pop-best, then expand                  Focus()  (compress current, expand the best sibling)
//	prune / memoize (bound the open set)   Compress() + Branch.Status=DEAD
//	collapse duplicate states              Merge() (Branch.Status=MERGED ; StateKey for dedup)
//	reconstruct(goal)                      Reconstruct()  (walk came_from to the root)
//
// The one twist vs textbook A*: exactly one node is EXPANDED at a time (bounded focus = a single
// search CPU); every other open node is COMPRESSED to gist. So thinking is A* run on one core,
// where the open set is reasoning branches and a node is "expanded" by *focusing* it.
//
// View is a pure read-only projection — it adds no state, it just names the search structure that
// is already there. That it projects cleanly is itself the validation that the model is unified.
//
// Ported from the (now-removed) Python thought_harness/search.py (Python's SearchView).
package search

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// View is an A*-shaped, read-only view over a graph.ThoughtGraph (Python SearchView).
type View struct {
	G *graph.ThoughtGraph
}

// New constructs a read-only A* View over a thought graph (Python SearchView.__init__).
func New(g *graph.ThoughtGraph) *View { return &View{G: g} }

// -- the open set (priority queue, best-first by the heuristic) ----------

// Open returns the frontier — the stashed siblings ordered best-first by value (the A* open set).
// Mirrors Python SearchView.open (returns g.frontier()).
func (v *View) Open() []*types.Branch { return v.G.Frontier() }

// Best returns the highest-value open node, or nil if the frontier is empty (Python
// SearchView.best).
func (v *View) Best() *types.Branch {
	fr := v.G.Frontier()
	if len(fr) == 0 {
		return nil
	}
	return fr[0]
}

// H is the heuristic estimate of a node's worth — the value signal (Python SearchView.h).
func (v *View) H(b *types.Branch) float64 { return b.Value }

// -- the node currently being expanded (bounded focus = one search CPU) --

// Current returns the branch currently being expanded — the active (EXPANDED) branch (Python
// SearchView.current, returns g.active()).
func (v *View) Current() *types.Branch { return v.G.Active() }

// -- the closed set (pruned / merged — kept for trace) ------------------

// Closed returns the closed set: branches that are DEAD or MERGED (pruned/collapsed, kept for the
// trace). Iteration order follows the graph's Branches map walked id-ascending so the result is
// deterministic. Mirrors Python SearchView.closed.
func (v *View) Closed() []*types.Branch {
	out := []*types.Branch{}
	for _, b := range v.branchesByID() {
		if b.Status == types.DEAD || b.Status == types.MERGED {
			out = append(out, b)
		}
	}
	return out
}

// branchesByID returns the graph's branches walked in id-ascending order — Python dict insertion
// order (bids are monotonic), so closed()/counts stay deterministic. Reads through the live
// Branches map (read-only).
func (v *View) branchesByID() []*types.Branch {
	ids := make([]int, 0, len(v.G.Branches))
	for id := range v.G.Branches {
		ids = append(ids, id)
	}
	// id-ascending == Python dict insertion order for monotonic bids
	sort.Ints(ids)
	out := make([]*types.Branch, 0, len(ids))
	for _, id := range ids {
		out = append(out, v.G.Branches[id])
	}
	return out
}

// -- path reconstruction (came_from) ------------------------------------

// Reconstruct walks parent pointers from a thought back to the root — A*'s came_from path
// reconstruction. Pass nil for thoughtID to default to the active branch's latest thought.
// Mirrors Python SearchView.reconstruct (delegates to g.reconstruct_path).
func (v *View) Reconstruct(thoughtID *int) []types.Thought {
	return v.G.ReconstructPath(thoughtID)
}

// -- a one-line search summary (for the dashboard) ----------------------

// Stats is the heterogeneous one-line search summary Python's SearchView.stats returns as a dict.
// BestH is *float64 because Python's "best_h" is None when the frontier is empty (nil == None).
// The Python emit site rounds best_h to 3 places; Stats() does the same (round-half-to-even,
// matching Python round(x, 3)).
type Stats struct {
	Open     int      // len(open set)
	Closed   int      // len(closed set)
	Expanded int      // the EXPANDED node's id (current().id)
	Depth    int      // structural depth of the active branch
	BestH    *float64 // the best open node's heuristic, rounded to 3; nil == None (empty frontier)
}

// Stats returns the one-line search summary (Python SearchView.stats). best_h is rounded to 3
// places at THIS site, exactly as the Python dict does (the round(openset[0].value, 3) call), and
// is nil when the frontier is empty.
func (v *View) Stats() Stats {
	openset := v.Open()
	s := Stats{
		Open:     len(openset),
		Closed:   len(v.Closed()),
		Expanded: v.Current().ID,
		Depth:    v.G.Depth(v.G.ActiveBranch),
	}
	if len(openset) > 0 {
		bh := round3(openset[0].Value)
		s.BestH = &bh
	}
	return s
}

// Summary renders the one-line search string for the dashboard (Python SearchView.summary). The
// "best h=" clause is appended only when best_h is not None; the float is formatted with Python's
// str(float) shortest-round-trip repr (strconv 'g', -1), matching the f-string interpolation.
func (v *View) Summary() string {
	s := v.Stats()
	out := fmt.Sprintf("search: %d open · %d closed · depth %d", s.Open, s.Closed, s.Depth)
	if s.BestH != nil {
		out += " · best h=" + pyFloatStr(*s.BestH)
	}
	return out
}

// -- helpers -------------------------------------------------------------

// round3 reproduces Python's round(x, 3): format to 3 fixed decimals (round-half-to-even, as both
// strconv.FormatFloat and CPython's float __round__ use) and parse back, so the value matches the
// Python wire byte-for-byte. Mirrors the regulator's round3.
func round3(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return v
}

// pyFloatStr renders a float the way Python's str()/repr() does — the shortest decimal that
// round-trips (strconv 'g', -1). Matches goStr in internal/trace for floats; used for the
// display-only summary string.
func pyFloatStr(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }
