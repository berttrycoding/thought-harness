package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/memory"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// must1 fails the test on a save error.
func must1(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
}

// newPersistEngine builds a heuristic engine wired to a JSONL store at dir (cross-session persistence on).
func newPersistEngine(t *testing.T, dir string) *Engine {
	t.Helper()
	st, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Store = st
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// TestPersistEpisodeRoundTrip is the M4 definition-of-done: run a grounded episode with a Store, exit
// (Flush), then restart a FRESH engine on the SAME store — the grounded episode + distilled belief are
// restored and recallable. Learned state survives a restart (it used to evaporate on exit).
func TestPersistEpisodeRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// session 1: ground an arithmetic episode (the heuristic backend computes 9x9 → a grounded outcome),
	// which records an episode and (at reflect) may distil a belief. Flush persists it.
	e1 := newPersistEngine(t, dir)
	e1.SubmitDefault("What's 9 times 9?")
	e1.Run(10)
	e1.FlushState()
	if e1.Episodic().Len() == 0 {
		t.Fatalf("session 1 should have recorded a grounded episode")
	}
	wantEpisodes := e1.Episodic().Len()

	// session 2: a FRESH engine on the same store re-seeds episodic memory from disk BEFORE episode 1.
	e2 := newPersistEngine(t, dir)
	if got := e2.Episodic().Len(); got != wantEpisodes {
		t.Fatalf("restart should restore %d episode(s), got %d", wantEpisodes, got)
	}
	// the restored episode is recallable by a related goal (the recall port reaches the re-seeded store).
	if got := e2.Episodic().Recall("what is 9 times 9", 3); len(got) == 0 {
		t.Fatalf("a restored grounded episode must be recallable after a restart")
	}
}

// TestPersistSubstrateProvenance: every saved record carries the thinking-substrate tag (the backend
// display name) so a frontier-derived dataset stays distinguishable for re-localization
// (claude-code-substrate-mapping.md §6.2). The test backend stamps "test".
func TestPersistSubstrateProvenance(t *testing.T) {
	dir := t.TempDir()
	e := newPersistEngine(t, dir)
	e.SubmitDefault("What's 9 times 9?")
	e.Run(10)
	e.FlushState()

	snap := e.Store().Snapshot()
	if len(snap.Episodes) == 0 {
		t.Fatalf("expected at least one persisted episode")
	}
	for _, ep := range snap.Episodes {
		if ep.Meta.Substrate != "test" {
			t.Fatalf("episode substrate = %q, want %q", ep.Meta.Substrate, "test")
		}
	}
}

// TestPersistNilStoreIsByteIdentical: with no Store (the default), every persistence hook is a no-op —
// the engine behaves exactly as pre-M4 (no disk, no persist.* events). This guards the goldens.
func TestPersistNilStoreIsByteIdentical(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	if e.Store() != nil {
		t.Fatal("a default engine must have a nil Store (in-memory only)")
	}
	// running with no store must not panic and must not emit any persist.* event.
	var persistEvents int
	e.Bus().Subscribe(func(ev events.Event) {
		if strings.HasPrefix(ev.Kind, "persist.") {
			persistEvents++
		}
	})
	e.SubmitDefault("What's 6 times 7?")
	e.Run(10)
	if persistEvents != 0 {
		t.Fatalf("a nil-store engine must emit no persist.* events, got %d", persistEvents)
	}
}

// TestPersistMintedPrimitiveSubAgentRoundTrip: a minted specialist persisted to the store re-registers into the
// subconscious roster on a fresh engine (loadState → convert.SeedPrimitiveSubAgents), so a learned reflex
// survives a restart and fires again. Drives the engine wiring end to end (store → engine → convert →
// subconscious).
func TestPersistMintedPrimitiveSubAgentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// session 1: persist a minted specialist record directly through the store, then flush.
	st1, _ := persist.NewJSONLStore(dir)
	must1(t, st1.SaveSpecialist(persist.SpecialistRecord{
		Meta:   persist.Meta{Grounded: true, Status: persist.StatusActive, LastUsedTick: 3, UseCount: 1},
		Domain: "learned:cache ttl", GoalKey: "cache ttl", Triggers: []string{"cache"},
		Answer: "the cache ttl is 60 seconds", Relevance: 0.9, Generated: 3, Value: 0.8,
	}))
	if err := st1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// session 2: a fresh engine on the same store re-seeds the specialist; it must be in the roster + fire.
	e2 := newPersistEngine(t, dir)
	var restored bool
	var fires bool
	for _, sp := range e2.Subconscious().Specialists() {
		if sp.Domain() == "learned:cache ttl" {
			restored = true
			fires = sp.Relevance([]types.Thought{{Text: "what is the cache ttl"}}) > 0
		}
	}
	if !restored {
		t.Fatal("a persisted minted specialist must be re-registered on restart")
	}
	if !fires {
		t.Fatal("the re-registered specialist should fire for its own trigger after a restart")
	}
}

// TestPersistGroundedBeliefSurvivesRefutationStatus: a belief is persisted with its bi-temporal validity;
// a refuted (invalidated) belief reconstructs as invalidated across a restart (invalidate-not-delete).
func TestPersistBeliefBiTemporalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	e1 := newPersistEngine(t, dir)
	// seed a grounded belief directly, invalidate it, then persist — the refutation must survive.
	e1.semantic.Record(memory.Belief{Statement: "the cache ttl is 30s", Grounded: true, ValidFrom: 1})
	e1.semantic.Invalidate("the cache ttl is 30s", 5)
	e1.persistLearned()
	if err := e1.Store().Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	e2 := newPersistEngine(t, dir)
	// the invalidated belief must NOT surface as current (its ValidTo survived the restart).
	if got := e2.semantic.Recall("cache ttl", 3); len(got) != 0 {
		t.Fatalf("an invalidated belief must not surface as current after a restart, got %d", len(got))
	}
	// but the full history (incl. the invalidated row) is preserved for audit.
	if e2.semantic.Len() == 0 {
		t.Fatalf("the invalidated belief's history should survive the restart (invalidate-not-delete)")
	}
}
