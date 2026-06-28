package engine

// ledger_wire_test.go — W1 engine wiring: the engine writes the self-change LEDGER when it mints,
// takes a once-per-session pre-mint baseline snapshot, and does NOT log re-loaded prior mints.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// A genuinely NEW mint this session is recorded as a SAFE/S1 self-change ledger entry at the next
// consolidation, with a revert handle and the cognition as author.
func TestEngineRecordsMintInLedger(t *testing.T) {
	dir := t.TempDir()
	e := newPersistEngine(t, dir) // empty store ⇒ post-load baseline mint count is 0

	// mint a new operator into the live catalog (a real self-change this session).
	if _, ok := e.catalog.MintWithMove("ledgerprobeop", "generative", "invent a probe candidate move", cognition.Move("ground")); !ok {
		t.Fatal("precondition: MintWithMove should mint a fresh operator")
	}
	e.FlushState() // consolidation → recordLedger

	entries, err := e.Store().LoadLedger()
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 ledger entry for the new mint, got %d", len(entries))
	}
	ent := entries[0]
	if ent.Scope != persist.LedgerScopeS1 {
		t.Errorf("scope = %q, want S1 (registry content)", ent.Scope)
	}
	if ent.SafetyMode != persist.SafetyModeSafe {
		t.Errorf("safety mode = %q, want SAFE (default)", ent.SafetyMode)
	}
	if ent.SubmittedBy != "cognition" {
		t.Errorf("author = %q, want cognition", ent.SubmittedBy)
	}
	if ent.RevertHandle == "" {
		t.Error("ledger entry must carry a revert handle")
	}

	// a second consolidation with NO further mint writes NO new entry (records growth only).
	e.FlushState()
	entries2, _ := e.Store().LoadLedger()
	if len(entries2) != 1 {
		t.Fatalf("a no-mint consolidation must not append a ledger entry; got %d", len(entries2))
	}
}

// The engine takes a once-per-session auto-baseline snapshot (the pre-mint revert point), and a
// no-mint session writes no ledger entry.
func TestEngineBaselineSnapshotAndNoSpuriousLedger(t *testing.T) {
	dir := t.TempDir()
	e := newPersistEngine(t, dir)
	e.SubmitDefault("What's 6 times 7?") // a grounded episode, but the test backend mints nothing here
	e.Run(8)
	e.FlushState()

	metas, err := e.Store().ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	found := false
	for _, m := range metas {
		if m.Name == ledgerBaselineSnapshot {
			found = true
		}
	}
	if !found {
		t.Fatalf("the session must take an %q snapshot; snapshots: %+v", ledgerBaselineSnapshot, metas)
	}

	entries, _ := e.Store().LoadLedger()
	if len(entries) != 0 {
		t.Fatalf("a session that minted nothing must write no ledger entries, got %d", len(entries))
	}
}

// Re-loading prior-session mints is the BASELINE, not a fresh self-change: a restart on a store that
// already holds a minted operator writes no ledger entry until something NEW is minted.
func TestEngineDoesNotLogReloadedMints(t *testing.T) {
	dir := t.TempDir()

	// session 1: mint, flush (writes operators.jsonl + one ledger entry).
	e1 := newPersistEngine(t, dir)
	e1.catalog.MintWithMove("carryoverop", "generative", "invent a carryover candidate", cognition.Move("ground"))
	e1.FlushState()
	first, _ := e1.Store().LoadLedger()
	if len(first) != 1 {
		t.Fatalf("session 1 should log its mint once, got %d", len(first))
	}

	// session 2: fresh engine on the SAME dir — re-loads carryover-op as baseline, mints nothing.
	e2 := newPersistEngine(t, dir)
	if _, ok := e2.catalog.Get("carryoverop"); !ok {
		t.Fatal("precondition: session 2 should reload the persisted operator")
	}
	e2.FlushState()
	second, _ := e2.Store().LoadLedger()
	if len(second) != 1 {
		t.Fatalf("reloading a prior mint must NOT append a ledger entry (still 1 from session 1), got %d", len(second))
	}
}
