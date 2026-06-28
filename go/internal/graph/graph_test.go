package graph

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestFormIntentionRouting pins the form_intention regex router against the Python oracle. The
// expected (kind,text) pairs were captured from thought_harness/graph.py form_intention on the
// same goals (see the porting session). This is the load-bearing keyword-table check the port note
// called out ("test keyword tables carefully").
func TestFormIntentionRouting(t *testing.T) {
	cases := []struct {
		goal, kind, text string
	}{
		{"why am I getting 500 errors", "reflect", "reason about it as far as I can"},
		{"send the email to the team", "send", "send: send the email to the team"},
		{"reply to the customer", "send", "send: reply to the customer"},
		{"should I reply to this", "reflect", "reason about it as far as I can"},
		{"divide 128 by 4", "measure", "work the arithmetic through carefully and check it"},
		{"compute the sum by hand", "measure", "work the arithmetic through carefully and check it"},
		{"refactor the auth module", "measure", "run the test suite to confirm behaviour is preserved"},
		{"is it safe to deploy", "measure", "run the test suite to confirm behaviour is preserved"},
		{"run the deployment script", "run", "run it for real: run the deployment script"},
		{"what is the meaning of life", "reflect", "reason about it as far as I can"},
		{"optimize the query", "measure", "run the test suite to confirm behaviour is preserved"},
		{"tell them apart", "reflect", "reason about it as far as I can"},
		{"checkout the branch", "reflect", "reason about it as far as I can"},
		{"benchmark the endpoint", "measure", "run the test suite to confirm behaviour is preserved"},
	}
	for _, c := range cases {
		g := New(c.goal)
		in := g.FormIntention()
		if in.Kind != c.kind || in.Text != c.text {
			t.Errorf("goal %q: got (%s, %q), want (%s, %q)", c.goal, in.Kind, in.Text, c.kind, c.text)
		}
		if in.BranchID == nil || *in.BranchID != g.ActiveBranch {
			t.Errorf("goal %q: intention branch_id = %v, want active %d", c.goal, in.BranchID, g.ActiveBranch)
		}
	}
}

// TestConstruction verifies the root branch + root thought are wired exactly as Python __init__.
func TestConstruction(t *testing.T) {
	g := New("solve x")
	if g.ActiveBranch != 0 {
		t.Fatalf("active branch = %d, want 0", g.ActiveBranch)
	}
	ctx := g.ActiveContext()
	if len(ctx) != 1 {
		t.Fatalf("active context len = %d, want 1", len(ctx))
	}
	root := ctx[0]
	if root.ID != 1 {
		t.Errorf("root id = %d, want 1", root.ID)
	}
	if root.Text != "solve x" || root.Source != types.GENERATED {
		t.Errorf("root = (%q, %s), want (solve x, GENERATED)", root.Text, root.Source)
	}
	if root.Parent != nil {
		t.Errorf("root parent = %v, want nil", root.Parent)
	}
	if root.BranchID == nil || *root.BranchID != 0 {
		t.Errorf("root branch_id = %v, want 0", root.BranchID)
	}
}

// TestAppendParentWiring checks Append wires parent to the prior tip and assigns ids for id<0.
func TestAppendParentWiring(t *testing.T) {
	g := New("goal")
	a := g.Append(&types.Thought{ID: -1, Text: "step a", Source: types.GENERATED}, 3)
	if a.ID != 2 {
		t.Errorf("appended id = %d, want 2", a.ID)
	}
	if a.Tick != 3 {
		t.Errorf("appended tick = %d, want 3", a.Tick)
	}
	if a.Parent == nil || *a.Parent != 1 {
		t.Errorf("appended parent = %v, want 1 (root)", a.Parent)
	}
	// An explicitly-set parent is NOT overwritten.
	p := 1
	b := g.Append(&types.Thought{ID: -1, Text: "step b", Source: types.GENERATED, Parent: &p}, 4)
	if b.Parent == nil || *b.Parent != 1 {
		t.Errorf("explicit parent = %v, want preserved 1", b.Parent)
	}
}

// TestFrontierOrdering checks frontier() returns stashed siblings best-first by value, stable on ties.
func TestFrontierOrdering(t *testing.T) {
	g := New("goal")
	// mint three sibling branches, stash them with values
	b1 := g.NewBranch(nil, nil)
	b2 := g.NewBranch(nil, nil)
	b3 := g.NewBranch(nil, nil)
	g.Branches[b1].Value = 0.5
	g.Branches[b2].Value = 0.9
	g.Branches[b3].Value = 0.5
	g.Branches[b1].Status = types.STASHED
	g.Branches[b2].Status = types.STASHED
	g.Branches[b3].Status = types.STASHED
	fr := g.Frontier()
	if len(fr) != 3 {
		t.Fatalf("frontier len = %d, want 3", len(fr))
	}
	if fr[0].ID != b2 {
		t.Errorf("frontier[0] = %d, want highest-value %d", fr[0].ID, b2)
	}
	// tie between b1 and b3 (both 0.5): stable -> id-ascending (b1 before b3)
	if fr[1].ID != b1 || fr[2].ID != b3 {
		t.Errorf("tie order = [%d,%d], want id-ascending [%d,%d]", fr[1].ID, fr[2].ID, b1, b3)
	}
	// the active branch is never in the frontier
	for _, b := range fr {
		if b.ID == g.ActiveBranch {
			t.Errorf("frontier contains the active branch %d", b.ID)
		}
	}
}

// TestStateKey checks the canonical state key drops METACOG thoughts, keeps len>3 words, dedups,
// sorts, and joins — matching Python state_key.
func TestStateKey(t *testing.T) {
	g := New("goal")
	// last real thought
	g.Append(&types.Thought{ID: -1, Text: "the Quick brown fox fox jumps", Source: types.GENERATED}, 0)
	// a METACOG thought after it must be IGNORED by state_key (uses the last REAL thought)
	g.Append(&types.Thought{ID: -1, Text: "rerank now", Source: types.METACOG}, 0)
	key := g.StateKey(g.ActiveBranch)
	// words len>3, lower, dedup, sorted: brown, jumps, quick (fox=3 dropped, "the"=3 dropped)
	want := "brown jumps quick"
	if key != want {
		t.Errorf("state_key = %q, want %q", key, want)
	}
}

// TestStateKeyEmpty: a branch with only METACOG thoughts has no real state.
func TestStateKeyEmpty(t *testing.T) {
	g := New("goal")
	bid := g.NewBranch(nil, nil)
	g.ActiveBranch = bid
	g.Append(&types.Thought{ID: -1, Text: "metacog only", Source: types.METACOG}, 0)
	if k := g.StateKey(bid); k != "" {
		t.Errorf("state_key of metacog-only branch = %q, want empty", k)
	}
}

// TestXrefs checks add_xref dedups, rejects self-edges + unknown kinds, and superseded() collects
// SUPERSEDES targets.
func TestXrefs(t *testing.T) {
	g := New("goal")
	if !g.AddXref(0, "CONTRADICTS", 1) {
		t.Error("first add_xref should be new")
	}
	if g.AddXref(0, "CONTRADICTS", 1) {
		t.Error("duplicate add_xref should be rejected")
	}
	if g.AddXref(2, "CONTRADICTS", 2) {
		t.Error("self-edge should be rejected")
	}
	if g.AddXref(0, "BOGUS", 1) {
		t.Error("unknown xref kind should be rejected")
	}
	g.AddXref(3, "SUPERSEDES", 4)
	g.AddXref(5, "SUPERSEDES", 6)
	sup := g.Superseded()
	if _, ok := sup[4]; !ok {
		t.Error("superseded should contain 4")
	}
	if _, ok := sup[6]; !ok {
		t.Error("superseded should contain 6")
	}
	if _, ok := sup[1]; ok {
		t.Error("superseded should NOT contain CONTRADICTS target 1")
	}
}

// TestReconstructPath walks parent pointers root-first.
func TestReconstructPath(t *testing.T) {
	g := New("root")
	g.Append(&types.Thought{ID: -1, Text: "a", Source: types.GENERATED}, 0)
	g.Append(&types.Thought{ID: -1, Text: "b", Source: types.GENERATED}, 0)
	path := g.ReconstructPath(nil)
	if len(path) != 3 {
		t.Fatalf("path len = %d, want 3", len(path))
	}
	if path[0].Text != "root" || path[1].Text != "a" || path[2].Text != "b" {
		t.Errorf("path = [%q,%q,%q], want [root,a,b]", path[0].Text, path[1].Text, path[2].Text)
	}
}

// TestDepth counts parent-branch hops.
func TestDepth(t *testing.T) {
	g := New("goal") // branch 0, depth 0
	root := 0
	b1 := g.NewBranch(&root, nil) // depth 1
	b2 := g.NewBranch(&b1, nil)   // depth 2
	if d := g.Depth(0); d != 0 {
		t.Errorf("depth(0) = %d, want 0", d)
	}
	if d := g.Depth(b1); d != 1 {
		t.Errorf("depth(b1) = %d, want 1", d)
	}
	if d := g.Depth(b2); d != 2 {
		t.Errorf("depth(b2) = %d, want 2", d)
	}
}
