package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestSpineGistFallback: when Summarize yields no gist (the claude reasoning-model edge — proven by the
// offline canned Summarize populating fine), the spine falls back to the branch's last non-empty thought
// so re-grounding carries real content instead of "(none)" (live-claude #14).
func TestSpineGistFallback(t *testing.T) {
	if got := spineGistFallback([]types.Thought{{Text: "first"}, {Text: "  last thought  "}, {Text: ""}}); got != "last thought" {
		t.Fatalf("fallback = %q, want %q (last non-empty thought, trimmed)", got, "last thought")
	}
	if got := spineGistFallback(nil); got != "" {
		t.Fatalf("empty branch fallback = %q, want empty", got)
	}
}

// TestGraphSpineRoundTrip is the STRUCTURAL gate (proposal §11 Track 2): build a Context with a
// known L1 spine, project it via snapshotGraphSpine → SaveGraphSpine → LoadGraphSpine →
// loadGraphSpine, and assert the rehydrated e.priorContext reconstructs the Goal + every L1 field.
// This exercises the Context -> record -> Context projection without the full episode loop, proving
// the compressed spine survives a power-cycle intact (Gist + ThoughtIDs are the re-grounding
// material; the heavy Snapshot is deliberately dropped).
func TestGraphSpineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// engine A: hand-set a known episodeContext (a real Context, but constructed so the spine fields
	// are fixed + asserted exactly), then project + persist it.
	a := mkSpineEngine(t, store, true)
	a.episodeContext = &subconscious.Context{
		Goal: "trace the regulator gain estimate",
		L1: subconscious.L1Snapshot{
			BranchID:   7,
			Gist:       "lag-1 regression identifies the plant; falls back to the prior",
			ThoughtIDs: []int{3, 6, 9, 12},
			Resolution: "EXPANDED",
		},
		L2:       map[string]any{},
		Snapshot: nil,
	}

	rec := a.snapshotGraphSpine()
	if rec.Goal != a.episodeContext.Goal || rec.BranchID != 7 || rec.Gist != a.episodeContext.L1.Gist {
		t.Fatalf("snapshotGraphSpine projection wrong: %+v", rec)
	}
	if len(rec.ThoughtIDs) != 4 || rec.ThoughtIDs[3] != 12 {
		t.Fatalf("snapshotGraphSpine IDs wrong: %+v", rec.ThoughtIDs)
	}
	if err := store.SaveGraphSpine(rec); err != nil {
		t.Fatal(err)
	}

	// engine B: rehydrate from the same store (resume ON) and assert e.priorContext matches A's spine.
	b := mkSpineEngine(t, store, true)
	pc := b.PriorContext()
	if pc == nil {
		t.Fatal("resume ON: PriorContext is nil after a matching spine was persisted")
	}
	if pc.Goal != a.episodeContext.Goal {
		t.Fatalf("rehydrated Goal = %q, want %q", pc.Goal, a.episodeContext.Goal)
	}
	if pc.L1.BranchID != 7 || pc.L1.Gist != a.episodeContext.L1.Gist || pc.L1.Resolution != "EXPANDED" {
		t.Fatalf("rehydrated L1 mismatch: %+v", pc.L1)
	}
	if len(pc.L1.ThoughtIDs) != 4 || pc.L1.ThoughtIDs[0] != 3 || pc.L1.ThoughtIDs[3] != 12 {
		t.Fatalf("rehydrated ThoughtIDs mismatch: %+v", pc.L1.ThoughtIDs)
	}
	// the heavy full Snapshot is NOT persisted (light re-orientation, §4/§9) — it stays nil.
	if pc.Snapshot != nil {
		t.Fatalf("rehydrated Snapshot = %v, want nil (full graph is not persisted)", pc.Snapshot)
	}
	// L2 is a fresh, non-nil map so a later consumer (Track 3) can write to it without nil-panic.
	if pc.L2 == nil {
		t.Fatal("rehydrated L2 is nil, want a fresh non-nil map for a later consumer")
	}
}

// TestGraphSpineDivergenceRefused: a spine record whose Version OR Substrate does not match the
// running engine's is REFUSED — e.priorContext stays nil (the divergence contract: never a
// best-effort partial rehydrate). Mirrors the percept-log divergence guard.
func TestGraphSpineDivergenceRefused(t *testing.T) {
	// case 1: version mismatch.
	dirV := t.TempDir()
	storeV, err := persist.NewJSONLStore(dirV)
	if err != nil {
		t.Fatal(err)
	}
	if err := storeV.SaveGraphSpine(persist.GraphSpineRecord{
		Version:   persist.GraphSpineVersion + 99,
		Substrate: "test",
		Goal:      "stale-version line",
		Gist:      "should be refused",
	}); err != nil {
		t.Fatal(err)
	}
	eV := mkSpineEngine(t, storeV, true)
	if eV.PriorContext() != nil {
		t.Fatal("version-divergent spine was rehydrated, want REFUSED (priorContext nil)")
	}

	// case 2: substrate mismatch (recorded against a different substrate).
	dirS := t.TempDir()
	storeS, _ := persist.NewJSONLStore(dirS)
	if err := storeS.SaveGraphSpine(persist.GraphSpineRecord{
		Version:   persist.GraphSpineVersion,
		Substrate: "claude:sonnet", // a different substrate than the running engine ("test")
		Goal:      "cross-substrate line",
		Gist:      "should be refused",
	}); err != nil {
		t.Fatal(err)
	}
	eS := mkSpineEngine(t, storeS, true)
	if eS.PriorContext() != nil {
		t.Fatal("substrate-divergent spine was rehydrated, want REFUSED (priorContext nil)")
	}
}

// TestGraphSpinePersistsAcrossEngines is the END-TO-END power-cycle gate via the LIVE capture path:
// engine A (resume ON, capability ON so a real episodeContext is captured) steps + flushes a spine
// to a state dir; engine B sharing that dir with resume ON rehydrates e.priorContext carrying A's
// Goal + a real gist; engine C with resume OFF leaves e.priorContext nil (byte-identical default).
// Mirrors TestResumeCursorPersistsAcrossEngines.
func TestGraphSpinePersistsAcrossEngines(t *testing.T) {
	dir := t.TempDir()
	const goal = "summarise the open redesign threads"

	storeA, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := mkSpineEngine(t, storeA, true)
	a.startEpisode(goal, true) // the live capability path captures a real episodeContext (capability ON)
	for i := 0; i < 5; i++ {
		a.Step()
	}
	if a.episodeContext == nil {
		t.Fatal("engine A: capability ON but no episodeContext captured (live capture path did not run)")
	}
	wantGoal := a.episodeContext.Goal
	a.FlushState()

	storeB, _ := persist.NewJSONLStore(dir)
	b := mkSpineEngine(t, storeB, true)
	pc := b.PriorContext()
	if pc == nil {
		t.Fatal("resume ON: PriorContext nil after engine A flushed a spine")
	}
	if pc.Goal != wantGoal {
		t.Fatalf("rehydrated Goal = %q, want A's %q", pc.Goal, wantGoal)
	}

	storeC, _ := persist.NewJSONLStore(dir)
	c := mkSpineEngine(t, storeC, false)
	if c.PriorContext() != nil {
		t.Fatal("resume OFF: PriorContext non-nil, want nil (cold boot, byte-identical default)")
	}
}

// TestGraphSpineSaveUnaffectedByResumeKnob: saving the spine is gated only by persistence (it writes
// a file, never mutates engine state), so a resume-OFF engine still WRITES graph_spine.json — only
// RESTORING into priorContext is gated by the resume knob. This is the asymmetry the task spec fixes.
func TestGraphSpineSaveUnaffectedByResumeKnob(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := mkSpineEngine(t, store, false) // resume OFF
	a.episodeContext = &subconscious.Context{
		Goal: "knob-off save",
		L1:   subconscious.L1Snapshot{BranchID: 1, Gist: "written even with resume off", Resolution: "EXPANDED"},
		L2:   map[string]any{},
	}
	a.FlushState()

	got, err := store.LoadGraphSpine()
	if err != nil || got == nil {
		t.Fatalf("resume OFF still SAVES: LoadGraphSpine = (%v, %v), want a written record", got, err)
	}
	if got.Goal != "knob-off save" {
		t.Fatalf("saved spine Goal = %q, want %q", got.Goal, "knob-off save")
	}
}

// mkSpineEngine builds a continuous-mode engine with persistence on, the capability knob on (so the
// LIVE episode path captures a real episodeContext), and the resume knob set per arg. Mirrors
// mkResumeEngine.
func mkSpineEngine(t *testing.T, store persist.Store, resume bool) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Store = store
	feats := config.AllOn()
	feats.Persist.Enabled = true
	feats.Persist.Resume = resume
	feats.Subconscious.Capability = true // route episode-workflow production through a Capability (captures Context)
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}
