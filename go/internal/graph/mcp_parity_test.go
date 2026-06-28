package graph

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// captureBus subscribes a slice collector to a fresh Bus. Mirrors the Python reference harness
// (Bus + a subscriber recording each Event).
func captureBus() (*events.Bus, *[]events.Event) {
	bus := events.NewDefault()
	var got []events.Event
	bus.Subscribe(func(e events.Event) { got = append(got, e) })
	return bus, &got
}

// TestMCPParity pins the Thought-MCP ops (branch/merge/focus + rerank/compress/expand) against
// the Python reference (PORT-PLAN Tier-2 gate: "mcp ops produce the same graph shape + the same
// conscious.mcp event sequence for a fixed graph fixture"). Both the event sequence AND the final
// graph shape were captured by RUNNING thought_harness/mcp.py over the identical fixture:
//
//	goal "compute 2+2 and verify it"
//	b0: +2 GENERATED thoughts
//	branch("explore lookup approach", seed=GENERATED "alternative: use a lookup table instead") -> b1
//	branch("explore recursion", seed=None) -> b2
//	focus(b1); rerank(); merge(b1, b2)
//
// All ops run at tick 0 (mcp.tick=0). The expected event sequence is the eight events below.
func TestMCPParity(t *testing.T) {
	bus, got := captureBus()
	backend := backends.NewTest()

	g := New("compute 2+2 and verify it")
	mcp := NewThoughtMCP(g, backend, bus.Emit)
	mcp.Tick = 0

	g.Append(&types.Thought{ID: -1, Text: "first idea about the sum", Source: types.GENERATED}, 0)
	g.Append(&types.Thought{ID: -1, Text: "second idea: maybe carry the digits", Source: types.GENERATED}, 0)

	seed := types.Thought{ID: -1, Text: "alternative: use a lookup table instead", Source: types.GENERATED}
	b1 := mcp.Branch("explore lookup approach", &seed)
	b2 := mcp.Branch("explore recursion", nil)
	mcp.Focus(b1)
	order := mcp.Rerank()
	surv := mcp.Merge(b1, b2)

	if b1 != 1 || b2 != 2 {
		t.Fatalf("branch ids = b%d, b%d want b1, b2", b1, b2)
	}
	if surv != 1 {
		t.Fatalf("merge survivor=%d want 1", surv)
	}
	// rerank excludes active(b1), DEAD, MERGED -> stashed b0, b2 (best-first; both value 0 so id order).
	if len(order) != 2 || order[0] != 0 || order[1] != 2 {
		t.Fatalf("rerank order=%v want [0 2]", order)
	}

	// -- event sequence parity (the conscious.mcp + conscious.xref stream) --
	type ev struct {
		kind, summary string
		data          events.D
	}
	want := []ev{
		{events.MCP, "branch -> b1: explore lookup approach",
			events.D{"op": "branch", "branch": 1, "reason": "explore lookup approach"}},
		{events.MCP, "branch -> b2: explore recursion",
			events.D{"op": "branch", "branch": 2, "reason": "explore recursion"}},
		{events.MCP, "compress b0: gist[6]: compute 2+2 and verify it … [focus] b0 -> b1",
			events.D{"op": "compress", "branch": 0}},
		{events.MCP, "expand b1", events.D{"op": "expand", "branch": 1}},
		{events.MCP, "focus b0 -> b1", events.D{"op": "focus", "branch": 1}},
		{events.MCP, "rerank -> [0, 2]", events.D{"op": "rerank", "order": []any{0, 2}}},
		{events.MCP, "merge b2 -> b1", events.D{"op": "merge", "into": 1, "gone": 2}},
		{events.XRef, "b1 SUPERSEDES b2", events.D{"src": 1, "kind": "SUPERSEDES", "dst": 2}},
	}
	if len(*got) != len(want) {
		for i, e := range *got {
			t.Logf("got[%d] %s %q %#v", i, e.Kind, e.Summary, e.Data)
		}
		t.Fatalf("emitted %d events, want %d", len(*got), len(want))
	}
	for i, w := range want {
		g := (*got)[i]
		if g.Kind != w.kind {
			t.Errorf("event[%d] kind=%q want %q", i, g.Kind, w.kind)
		}
		if g.Summary != w.summary {
			t.Errorf("event[%d] summary=%q want %q", i, g.Summary, w.summary)
		}
		if !mcpDataEqual(g.Data, w.data) {
			t.Errorf("event[%d] data:\n got  = %#v\n want = %#v", i, g.Data, w.data)
		}
	}

	// -- final graph shape parity --
	if g.ActiveBranch != 1 {
		t.Errorf("active_branch=%d want 1", g.ActiveBranch)
	}
	// b0: STASHED/COMPRESSED, thought_ids [1 2 3 4 6 7], parent None.
	b0 := g.Branches[0]
	if b0.Status != types.STASHED || b0.Resolution != types.COMPRESSED || b0.ParentBranch != nil {
		t.Errorf("b0 status/res/parent = %v/%v/%v", b0.Status, b0.Resolution, b0.ParentBranch)
	}
	if !intsEqual(b0.ThoughtIDs, []int{1, 2, 3, 4, 6, 7}) {
		t.Errorf("b0 thought_ids=%v want [1 2 3 4 6 7]", b0.ThoughtIDs)
	}
	if b0.Summary == nil || *b0.Summary != "gist[6]: compute 2+2 and verify it … [focus] b0 -> b1" {
		t.Errorf("b0 summary=%v", b0.Summary)
	}
	// b1: ACTIVE/EXPANDED, thought_ids [5 8], parent 0, summary the seed gist.
	b1b := g.Branches[1]
	if b1b.Status != types.ACTIVE || b1b.Resolution != types.EXPANDED {
		t.Errorf("b1 status/res = %v/%v", b1b.Status, b1b.Resolution)
	}
	if b1b.ParentBranch == nil || *b1b.ParentBranch != 0 {
		t.Errorf("b1 parent=%v want 0", b1b.ParentBranch)
	}
	if !intsEqual(b1b.ThoughtIDs, []int{5, 8}) {
		t.Errorf("b1 thought_ids=%v want [5 8]", b1b.ThoughtIDs)
	}
	if b1b.Summary == nil || *b1b.Summary != "gist: alternative: use a lookup table instead" {
		t.Errorf("b1 summary=%v", b1b.Summary)
	}
	// b2: MERGED, no live thoughts, parent 0.
	b2b := g.Branches[2]
	if b2b.Status != types.MERGED || len(b2b.ThoughtIDs) != 0 {
		t.Errorf("b2 status/ids = %v/%v", b2b.Status, b2b.ThoughtIDs)
	}
	if b2b.ParentBranch == nil || *b2b.ParentBranch != 0 {
		t.Errorf("b2 parent=%v want 0", b2b.ParentBranch)
	}
	// node ownership: thoughts 5,8 belong to b1, the rest to b0.
	wantOwner := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 1, 6: 0, 7: 0, 8: 1}
	for tid, ownerBid := range wantOwner {
		node := g.Nodes[tid]
		if node.BranchID == nil || *node.BranchID != ownerBid {
			t.Errorf("node %d branch_id=%v want %d", tid, node.BranchID, ownerBid)
		}
	}
	// xref: [(1, SUPERSEDES, 2)].
	if len(g.Xrefs) != 1 || g.Xrefs[0] != (XRef{Src: 1, Kind: "SUPERSEDES", Dst: 2}) {
		t.Errorf("xrefs=%v want [{1 SUPERSEDES 2}]", g.Xrefs)
	}
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mcpDataEqual deep-compares two event-data maps, coercing numeric types and []int<->[]any so a
// golden literal matches the emitted map regardless of int/float/slice representation.
func mcpDataEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !mcpValEqual(av, bv) {
			return false
		}
	}
	return true
}

func mcpValEqual(a, b any) bool {
	// normalise []int (the emitted rerank order) to []any for comparison with the golden literal.
	if ai, ok := a.([]int); ok {
		conv := make([]any, len(ai))
		for i, v := range ai {
			conv[i] = v
		}
		a = conv
	}
	if bi, ok := b.([]int); ok {
		conv := make([]any, len(bi))
		for i, v := range bi {
			conv[i] = v
		}
		b = conv
	}
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !mcpValEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && mcpDataEqual(av, bv)
	default:
		if an, aok := numFloat(a); aok {
			if bn, bok := numFloat(b); bok {
				return an == bn
			}
			return false
		}
		return a == b
	}
}

func numFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
