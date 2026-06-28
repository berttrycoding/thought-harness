package persist

// ledger_test.go — W1 registry ledger: the snapshot/reset roundtrip (the core revert capability)
// and the safety-mode scope gate. Added in the W1 audit — the implementation shipped with zero
// coverage, and the audit found the CLI revert never reached disk.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// skill builds a grounded skill record keyed by name (Load only reads grounded records back).
func skill(name string) SkillRecord {
	return SkillRecord{Name: name, Meta: Meta{Grounded: true, Status: StatusActive}}
}

// The core W1 capability: snapshot a baseline, mutate the live state, reset to the baseline, and
// verify the mutation is GONE from what a fresh load would see (snapshot → disk roundtrip). This is
// the revert that the campaign's keep-or-revert loop depends on.
func TestSnapshotResetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	st, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}

	// baseline: one skill, snapshotted under a name.
	if err := st.SaveSkill(skill("baseline-skill")); err != nil {
		t.Fatalf("SaveSkill: %v", err)
	}
	if err := st.Flush(); err != nil {
		t.Fatalf("Flush baseline: %v", err)
	}
	base, _ := st.Load()
	if err := st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "baseline", Substrate: "test"}, Data: *base}); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	// mutate: add a second skill and persist it as the live set.
	if err := st.SaveSkill(skill("experimental-skill")); err != nil {
		t.Fatalf("SaveSkill 2: %v", err)
	}
	if err := st.Flush(); err != nil {
		t.Fatalf("Flush mutated: %v", err)
	}

	// revert: reset to the baseline AND flush (the CLI bug the audit fixed — without the flush the
	// revert lived only in memory and a fresh load still saw the mutation).
	if err := st.ResetToSnapshot("baseline"); err != nil {
		t.Fatalf("ResetToSnapshot: %v", err)
	}
	if err := st.Flush(); err != nil {
		t.Fatalf("Flush after reset: %v", err)
	}

	// a FRESH store over the same dir must see the baseline only — proves the revert hit disk.
	fresh, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	snap, err := fresh.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("after revert, fresh load has %d skills, want 1 (the baseline) — revert did not reach disk", len(snap.Skills))
	}
	if snap.Skills[0].Name != "baseline-skill" {
		t.Fatalf("after revert, surviving skill is %q, want baseline-skill", snap.Skills[0].Name)
	}
}

// ListSnapshots returns metadata newest-first with the substrate tag intact (the hygiene rule:
// frontier and local state never mix, so the tag must survive the roundtrip).
func TestListSnapshotsNewestFirstTagged(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewJSONLStore(dir)
	empty, _ := st.Load()
	_ = st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "first", Substrate: "claude:sonnet"}, Data: *empty})
	_ = st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "second", Substrate: "llm:gemma"}, Data: *empty})

	metas, err := st.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("want 2 snapshots, got %d", len(metas))
	}
	if metas[0].Name != "second" {
		t.Fatalf("newest-first broken: metas[0] = %q, want \"second\"", metas[0].Name)
	}
	if metas[0].Substrate != "llm:gemma" {
		t.Fatalf("substrate tag lost: %q", metas[0].Substrate)
	}
}

// The safety-mode scope gate (the governance ladder): SAFE allows only S0+S1; S2/S3 are locked
// until a mode deliberately opens them. This is the config-gated boundary that keeps the plant
// structurally fixed in the default mode.
func TestSafetyModeScopeGate(t *testing.T) {
	cases := []struct {
		mode  SafetyMode
		scope LedgerScope
		want  bool
	}{
		{SafetyModeSafe, LedgerScopeS0, true},
		{SafetyModeSafe, LedgerScopeS1, true},
		{SafetyModeSafe, LedgerScopeS2, false}, // structure LOCKED in SAFE (default)
		{SafetyModeSafe, LedgerScopeS3, false}, // code LOCKED in SAFE (default)
		{SafetyModeExpand, LedgerScopeS2, true},
		{SafetyModeExpand, LedgerScopeS3, false},
		{SafetyModeRewrite, LedgerScopeS3, true},
	}
	for _, c := range cases {
		cfg := LedgerConfig{SafetyMode: c.mode}
		if got := cfg.ScopeAllowed(c.scope); got != c.want {
			t.Errorf("mode %s scope %s: ScopeAllowed = %v, want %v", c.mode, c.scope, got, c.want)
		}
	}
	// the default config is SAFE — structure/code locked out of the box.
	def := DefaultLedgerConfig()
	if def.SafetyMode != SafetyModeSafe {
		t.Fatalf("default safety mode = %q, want SAFE", def.SafetyMode)
	}
	if def.ScopeAllowed(LedgerScopeS3) {
		t.Fatal("default config must NOT allow S3 (code self-modification) — experimental, locked")
	}
}

// skillBody builds a grounded skill record with a distinguishable body (the diff keys on the body
// hash, so two skills of the same NAME with different bodies must read as CHANGED).
func skillBody(name, desc string) SkillRecord {
	return SkillRecord{Name: name, Description: desc, Meta: Meta{Grounded: true, Status: StatusActive}}
}

// DiffSnapshots detects ADDED / REMOVED / CHANGED at the CONTENT level, not just by count. The
// load-bearing case the old count-only diff missed: a SAME-SIZE set where one item is swapped for a
// different one (the campaign's keep-or-revert replacing batch A with batch B). Counts are equal; the
// diff must still report one added + one removed (different names) or one changed (same name, new body).
func TestDiffSnapshotsContentLevel(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewJSONLStore(dir)

	// "from": two skills A,B. "to": A unchanged, B replaced by C (same COUNT of 2 — a count diff = no-op).
	from := Snapshot{Skills: []SkillRecord{skillBody("A", "v1"), skillBody("B", "v1")}}
	to := Snapshot{Skills: []SkillRecord{skillBody("A", "v1"), skillBody("C", "v1")}}
	_ = st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "from"}, Data: from})
	_ = st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "to"}, Data: to})

	diff, err := st.DiffSnapshots("from", "to")
	if err != nil {
		t.Fatalf("DiffSnapshots: %v", err)
	}
	if diff.Added["skills"] != 1 {
		t.Errorf("same-size swap should report 1 added skill (C), got %d", diff.Added["skills"])
	}
	if diff.Removed["skills"] != 1 {
		t.Errorf("same-size swap should report 1 removed skill (B), got %d", diff.Removed["skills"])
	}

	// "to2": A's BODY changes (same name) — must read as CHANGED, not added+removed.
	to2 := Snapshot{Skills: []SkillRecord{skillBody("A", "v2"), skillBody("B", "v1")}}
	_ = st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "to2"}, Data: to2})
	diff2, err := st.DiffSnapshots("from", "to2")
	if err != nil {
		t.Fatalf("DiffSnapshots 2: %v", err)
	}
	if diff2.Changed["skills"] != 1 {
		t.Errorf("a same-name body change should report 1 changed skill, got %d (added=%d removed=%d)",
			diff2.Changed["skills"], diff2.Added["skills"], diff2.Removed["skills"])
	}
	if diff2.Added["skills"] != 0 || diff2.Removed["skills"] != 0 {
		t.Errorf("a body change must be CHANGED, not added+removed: added=%d removed=%d",
			diff2.Added["skills"], diff2.Removed["skills"])
	}
}

// Every ledger DECISION is observable on the bus (the observability contract): a snapshot emits
// registry.snapshot, a reset emits registry.reset (its OWN kind — the W1 finish wired this dead kind
// to its call site), and a diff emits registry.diff. None is silent.
func TestLedgerOpsEmitDistinctEvents(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewJSONLStore(dir)

	got := map[string]int{}
	st.SetEmit(func(kind, _ string, _ map[string]any) events.Event {
		got[kind]++
		return events.Event{}
	})

	empty, _ := st.Load()
	if err := st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "base", Substrate: "test"}, Data: *empty}); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if err := st.SaveSnapshot(SnapshotRecord{Meta: SnapshotMeta{Name: "later", Substrate: "test"}, Data: *empty}); err != nil {
		t.Fatalf("SaveSnapshot 2: %v", err)
	}
	if err := st.ResetToSnapshot("base"); err != nil {
		t.Fatalf("ResetToSnapshot: %v", err)
	}
	if _, err := st.DiffSnapshots("base", "later"); err != nil {
		t.Fatalf("DiffSnapshots: %v", err)
	}

	if got[string(events.RegistrySnapshot)] != 2 {
		t.Errorf("want 2 registry.snapshot events (two saves), got %d", got[string(events.RegistrySnapshot)])
	}
	if got[string(events.RegistryReset)] != 1 {
		t.Errorf("reset must emit registry.reset (its own kind, not registry.snapshot): got %d", got[string(events.RegistryReset)])
	}
	if got[string(events.RegistryDiff)] != 1 {
		t.Errorf("diff must emit registry.diff: got %d", got[string(events.RegistryDiff)])
	}
}

// The self-change ledger is append-only and round-trips newest-first with its fields intact.
func TestLedgerRoundtrip(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewJSONLStore(dir)
	_ = st.SaveLedgerEntry(LedgerEntry{Tick: 10, Scope: LedgerScopeS1, SafetyMode: SafetyModeSafe, Description: "minted specialist"})
	_ = st.SaveLedgerEntry(LedgerEntry{Tick: 20, Scope: LedgerScopeS1, SafetyMode: SafetyModeSafe, Description: "reverted batch-7"})

	entries, err := st.LoadLedger()
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Description != "reverted batch-7" {
		t.Fatalf("ledger not newest-first: entries[0] = %q", entries[0].Description)
	}
}
