package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestCaptureContextL1Snapshot: CaptureContext freezes the active branch's L1 spine — the branch's
// thought IDs in order + a lossy gist + the resolution at capture (§3.11 L1). This is the snapshot
// that replaces the ≤5 ContextSlice window so context scales with the branch.
func TestCaptureContextL1Snapshot(t *testing.T) {
	g := graph.New("root goal")
	g.Append(&types.Thought{ID: -1, Text: "first move", Source: types.GENERATED}, 1)
	g.Append(&types.Thought{ID: -1, Text: "second move", Source: types.GENERATED}, 2)

	ctx := CaptureContext(g, backends.NewTest(), "root goal", nil)

	want := g.Active().ThoughtIDs // root + 2 appended
	if len(ctx.L1.ThoughtIDs) != len(want) {
		t.Fatalf("L1 should snapshot all %d active-branch IDs; got %d", len(want), len(ctx.L1.ThoughtIDs))
	}
	for i := range want {
		if ctx.L1.ThoughtIDs[i] != want[i] {
			t.Fatalf("L1 ID[%d] = %d, want %d (spine order must be preserved)", i, ctx.L1.ThoughtIDs[i], want[i])
		}
	}
	if ctx.L1.Gist == "" {
		t.Fatal("L1 gist should be non-empty (backend.Summarize over the branch)")
	}
	if ctx.L1.Resolution != "EXPANDED" {
		t.Fatalf("a live active branch is EXPANDED at capture; got %q", ctx.L1.Resolution)
	}
}

// TestCaptureContextSnapshotIsFrozen: the L1 snapshot is a VALUE copy — it does not alias the live
// branch, so thoughts added AFTER capture do not mutate the frozen snapshot (the §3.11 "freezes it"
// guarantee, which makes context stable while the conscious keeps thinking).
func TestCaptureContextSnapshotIsFrozen(t *testing.T) {
	g := graph.New("goal")
	g.Append(&types.Thought{ID: -1, Text: "a", Source: types.GENERATED}, 1)

	ctx := CaptureContext(g, backends.NewTest(), "goal", nil)
	frozen := len(ctx.L1.ThoughtIDs)

	// keep thinking AFTER the snapshot.
	g.Append(&types.Thought{ID: -1, Text: "b", Source: types.GENERATED}, 2)
	g.Append(&types.Thought{ID: -1, Text: "c", Source: types.GENERATED}, 3)

	if len(ctx.L1.ThoughtIDs) != frozen {
		t.Fatalf("the snapshot must not grow as the branch grows; was %d, now %d", frozen, len(ctx.L1.ThoughtIDs))
	}
	if len(g.Active().ThoughtIDs) <= frozen {
		t.Fatal("sanity: the live branch should have grown past the snapshot")
	}
}

// TestContextL2RuntimeDerived: the L2 layer is runtime-derived — written back during the run via
// Set, read via Get (§3.11 L2). It is per-instance and non-nil after capture.
func TestContextL2RuntimeDerived(t *testing.T) {
	ctx := CaptureContext(graph.New("g"), backends.NewTest(), "g", nil)
	if ctx.L2 == nil {
		t.Fatal("L2 must be a non-nil per-instance map after capture")
	}
	if _, ok := ctx.Get("partial"); ok {
		t.Fatal("L2 should miss before anything is set")
	}
	ctx.Set("partial", 42)
	v, ok := ctx.Get("partial")
	if !ok || v.(int) != 42 {
		t.Fatalf("L2 Set/Get round-trip failed; got (%v, %v)", v, ok)
	}
}

// TestContextL3KnowledgeRefs: the L3 layer carries the paired-knowledge index refs the definition
// declares (§3.11 L3) — a lightweight ref (id + query), the store pull wired later.
func TestContextL3KnowledgeRefs(t *testing.T) {
	refs := []KnowledgeRef{
		{IndexID: "domain-facts", Query: "constraints of the puzzle"},
		{IndexID: "prior-cases", Query: "similar solved puzzles"},
	}
	ctx := CaptureContext(graph.New("g"), backends.NewTest(), "g", refs)
	if len(ctx.L3) != 2 {
		t.Fatalf("L3 should carry both declared refs; got %d", len(ctx.L3))
	}
	if ctx.L3[0].IndexID != "domain-facts" || ctx.L3[1].Query != "similar solved puzzles" {
		t.Fatalf("L3 refs not carried through faithfully: %+v", ctx.L3)
	}
}

// TestContextExpand: Expand materialises the full thoughts behind the L1 snapshot's IDs from the
// LIVE graph (OPT-3 on-demand traverse) — the snapshot froze the gist + IDs; Expand walks those IDs
// back to detail when a worker needs it.
func TestContextExpand(t *testing.T) {
	g := graph.New("expand goal")
	g.Append(&types.Thought{ID: -1, Text: "detail one", Source: types.GENERATED}, 1)
	g.Append(&types.Thought{ID: -1, Text: "detail two", Source: types.GENERATED}, 2)

	ctx := CaptureContext(g, backends.NewTest(), "expand goal", nil)

	ids := ctx.ExpandIDs()
	if len(ids) != len(ctx.L1.ThoughtIDs) {
		t.Fatalf("ExpandIDs should return the snapshot spine; got %d", len(ids))
	}
	// the returned slice is a copy — mutating it must not corrupt the snapshot.
	if len(ids) > 0 {
		ids[0] = -999
		if ctx.L1.ThoughtIDs[0] == -999 {
			t.Fatal("ExpandIDs must return a copy, not the live spine")
		}
	}

	full := ctx.Expand(g)
	if len(full) != len(ctx.L1.ThoughtIDs) {
		t.Fatalf("Expand should resolve all snapshot IDs from the live graph; got %d of %d",
			len(full), len(ctx.L1.ThoughtIDs))
	}
	// the last thought materialised should be the tip we appended.
	if full[len(full)-1].Text != "detail two" {
		t.Fatalf("Expand tip = %q, want the live branch tip", full[len(full)-1].Text)
	}
}

// TestCaptureContextNilSafe: a nil graph (or backend) yields an empty-but-valid Context (L2 non-nil)
// so callers never nil-panic on capture.
func TestCaptureContextNilSafe(t *testing.T) {
	ctx := CaptureContext(nil, nil, "g", nil)
	if ctx == nil || ctx.L2 == nil {
		t.Fatal("a nil-graph capture must still return a valid Context with a non-nil L2")
	}
	if len(ctx.L1.ThoughtIDs) != 0 {
		t.Fatal("a nil-graph capture must have an empty L1 spine")
	}
	if got := ctx.Expand(nil); got != nil {
		t.Fatalf("Expand(nil graph) must be nil; got %v", got)
	}
}
