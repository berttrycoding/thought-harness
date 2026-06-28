package graph

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// reenterFixture builds a small forest: the root line b0, a forked sibling b1 the conscious moved on
// to (so b1 is ACTIVE), and the past decision node b0 left compressed. This is the shape a late
// injection re-enters: the conscious is "ahead" on b1, the light-bulb is anchored back at b0.
func reenterFixture(t *testing.T) (*ThoughtGraph, *ThoughtMCP, *[]events.Event) {
	t.Helper()
	bus, got := captureBus()
	g := New("plan the migration")
	mcp := NewThoughtMCP(g, backends.NewTest(), bus.Emit)
	mcp.Tick = 0
	g.Append(&types.Thought{ID: -1, Text: "consider an in-place migration", Source: types.GENERATED}, 0)
	// fork a sibling and move onto it: b0 becomes the PAST decision node, b1 the line we are now on.
	b1 := mcp.Branch("explore a blue-green migration", nil)
	mcp.Focus(b1)
	if g.ActiveBranch != b1 {
		t.Fatalf("setup: active=%d want %d", g.ActiveBranch, b1)
	}
	return g, mcp, got
}

// TestReenterForksFromTargetAndFocuses pins the core contract (02 §2b, 04 §3.3): Reenter forks a NEW
// line from a TARGET branch that is NOT the active one, and Focuses to the fork. The active branch
// after the op is the new fork (not the old target, not the line we were on).
func TestReenterForksFromTargetAndFocuses(t *testing.T) {
	g, mcp, _ := reenterFixture(t)
	target := 0 // the past decision node
	before := len(g.Branches)

	newID := mcp.Reenter(target, "lookup table changes everything", nil)

	if newID == target || newID == 1 {
		t.Fatalf("Reenter must mint a NEW branch, not reuse target(%d) or the active line(1); got %d", target, newID)
	}
	if len(g.Branches) != before+1 {
		t.Fatalf("Reenter must add exactly one branch; before=%d after=%d", before, len(g.Branches))
	}
	// the fork's parent is the TARGET (we re-entered there), not the previously-active branch.
	nb := g.Branches[newID]
	if nb.ParentBranch == nil || *nb.ParentBranch != target {
		t.Fatalf("the fork must be parented on the target(%d); got parent=%v", target, nb.ParentBranch)
	}
	// Focus landed on the new fork — exactly one ACTIVE branch, and it is the re-entry line.
	if g.ActiveBranch != newID {
		t.Fatalf("Reenter must Focus the new fork; active=%d want %d", g.ActiveBranch, newID)
	}
	if nb.Status != types.ACTIVE || nb.Resolution != types.EXPANDED {
		t.Fatalf("the re-entered fork must be ACTIVE/EXPANDED; got %v/%v", nb.Status, nb.Resolution)
	}
}

// TestReenterPreservesOldLine pins non-destructiveness (02 §2b, 04 §6): the graph FORKS — the old
// line stays (compressed), nothing is overwritten. The target branch keeps all its thoughts and its
// identity; only its focus/resolution change (it is no longer the EXPANDED line).
func TestReenterPreservesOldLine(t *testing.T) {
	g, mcp, _ := reenterFixture(t)
	target := 0
	// snapshot the target's live thoughts before re-entry (the past decision's content).
	beforeIDs := append([]int(nil), g.Branches[target].ThoughtIDs...)

	mcp.Reenter(target, "new evidence", nil)

	after := g.Branches[target]
	if after.Status == types.MERGED || after.Status == types.DEAD {
		t.Fatalf("the old line must be PRESERVED, not retired; got status %v", after.Status)
	}
	if !intsEqual(after.ThoughtIDs, beforeIDs) {
		t.Fatalf("the old line's thoughts must be untouched; before=%v after=%v", beforeIDs, after.ThoughtIDs)
	}
	// every thought the target owned is still owned by the target (nothing was re-parented/overwritten).
	for _, tid := range beforeIDs {
		node := g.Nodes[tid]
		if node == nil || node.BranchID == nil || *node.BranchID != target {
			t.Fatalf("thought %d ownership changed; branch_id=%v want %d", tid, nodeBranch(node), target)
		}
	}
}

// TestReenterSeedsTheReentryLine pins that a late injection (the light-bulb) can SEED the new line:
// the re-entry fork carries the injected thought as its first live thought, so the conscious "thinks
// forward again" from that evidence. The seed is copied (never the caller's object) and owned by the
// new fork, not the target.
func TestReenterSeedsTheReentryLine(t *testing.T) {
	g, mcp, _ := reenterFixture(t)
	seed := types.Thought{ID: -1, Text: "the lookup table resolves the ambiguity", Source: types.INJECTED}
	newID := mcp.Reenter(0, "late insight", &seed)

	nb := g.Branches[newID]
	// the new line holds the seed (plus its retracement marker) — it is non-empty and the seed is live.
	var found bool
	for _, tid := range nb.ThoughtIDs {
		if n := g.Nodes[tid]; n != nil && n.Text == seed.Text && n.Source == types.INJECTED {
			found = true
			if n.BranchID == nil || *n.BranchID != newID {
				t.Fatalf("the seed must be owned by the new fork(%d); got %v", newID, nodeBranch(n))
			}
		}
	}
	if !found {
		t.Fatalf("the re-entry line must carry the injected seed; thoughts=%v", nb.ThoughtIDs)
	}
	// the caller's struct must be untouched (Reenter copies, never re-ids the caller's Thought).
	if seed.ID != -1 || seed.BranchID != nil {
		t.Fatalf("Reenter must not mutate the caller's seed; got id=%d branch=%v", seed.ID, seed.BranchID)
	}
}

// TestReenterEmitsRetracement pins the observability contract (02 §2b, 04 §3.3): the op records the
// re-entry as a DISTINCT retracement event (op="reenter") carrying the target it traced back to and
// the new fork — so the episodic timeline reads it as one retracement, not two ordinary moves. (It
// reuses the existing conscious.mcp kind; no new event kind is added — the vocabulary gate holds.)
func TestReenterEmitsRetracement(t *testing.T) {
	g, mcp, got := reenterFixture(t)
	start := len(*got)
	newID := mcp.Reenter(0, "trace back to the fork", nil)

	var retrace *events.Event
	for i := start; i < len(*got); i++ {
		e := (*got)[i]
		if e.Kind == events.MCP {
			if op, _ := e.Data["op"].(string); op == "reenter" {
				ev := e
				retrace = &ev
				break
			}
		}
	}
	if retrace == nil {
		t.Fatal("Reenter must emit a conscious.mcp event with op=reenter (the retracement marker)")
	}
	if tgt, _ := retrace.Data["target"].(int); tgt != 0 {
		t.Errorf("retracement event must carry the target branch 0; got %v", retrace.Data["target"])
	}
	if br, _ := retrace.Data["branch"].(int); br != newID {
		t.Errorf("retracement event must carry the new fork %d; got %v", newID, retrace.Data["branch"])
	}
	_ = g
}

// TestReenterNoOpOnUnknownTarget pins the guard: re-entering a branch that does not exist is a no-op
// (no new branch, focus unchanged) — the seam may anchor to a stale id, and a no-op is the safe floor.
func TestReenterNoOpOnUnknownTarget(t *testing.T) {
	g, mcp, _ := reenterFixture(t)
	before := len(g.Branches)
	active := g.ActiveBranch

	newID := mcp.Reenter(999, "ghost anchor", nil)

	if newID != -1 {
		t.Fatalf("re-entering an unknown target must return -1 (no-op); got %d", newID)
	}
	if len(g.Branches) != before {
		t.Fatalf("no branch must be minted for an unknown target; before=%d after=%d", before, len(g.Branches))
	}
	if g.ActiveBranch != active {
		t.Fatalf("focus must be unchanged on an unknown target; active=%d want %d", g.ActiveBranch, active)
	}
}

// nodeBranch renders a node's BranchID for an error message (nil-safe).
func nodeBranch(n *types.Thought) any {
	if n == nil || n.BranchID == nil {
		return nil
	}
	return *n.BranchID
}
