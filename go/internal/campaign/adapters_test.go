package campaign

// adapters_test.go — the real B1 adapters over persist + funnel, exercised on temp stores (no model).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/funnel"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

func knowledgeCand(id, text string) funnel.Candidate {
	return funnel.Candidate{
		ID: id, Kind: "knowledge", ClusterKey: id, Text: text,
		Provenance: "test feeder", Links: []string{"other"}, Exercised: true,
	}
}

// WriteBatch stages candidates as a seedable registry state; Commit folds them into the live store.
func TestBatchStoreWriteAndCommit(t *testing.T) {
	base := t.TempDir()
	live, _ := persist.NewJSONLStore(filepath.Join(base, "live"))
	bs := &JSONLBatchStore{Live: live, BaseDir: base, Substrate: "claude:sonnet"}

	cands := []funnel.Candidate{knowledgeCand("k1", "go test exits 0 on green"), knowledgeCand("k2", "the gate forks on conflict")}
	dir, err := bs.WriteBatch("001", cands)
	if err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}
	// the staged dir is loadable and holds the batch (grounded knowledge survives Load).
	staged, _ := persist.NewJSONLStore(dir)
	snap, _ := staged.Load()
	if len(snap.Knowledge) != 2 {
		t.Fatalf("staged batch has %d knowledge records, want 2", len(snap.Knowledge))
	}
	if snap.Knowledge[0].Meta.Substrate != "claude:sonnet" {
		t.Errorf("substrate tag lost: %q", snap.Knowledge[0].Meta.Substrate)
	}

	// before commit, live is empty; after commit it holds the batch.
	if lsnap, _ := live.Load(); len(lsnap.Knowledge) != 0 {
		t.Fatalf("live should be empty before commit, has %d", len(lsnap.Knowledge))
	}
	if err := bs.Commit(dir); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	fresh, _ := persist.NewJSONLStore(filepath.Join(base, "live"))
	lsnap, _ := fresh.Load()
	if len(lsnap.Knowledge) != 2 {
		t.Fatalf("after commit live has %d knowledge, want 2", len(lsnap.Knowledge))
	}

	// Discard removes a staged dir.
	if err := bs.Discard(dir); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("Discard should remove the staged dir")
	}
}

func TestBatchStoreUnsupportedKindErrors(t *testing.T) {
	base := t.TempDir()
	live, _ := persist.NewJSONLStore(filepath.Join(base, "live"))
	bs := &JSONLBatchStore{Live: live, BaseDir: base}
	_, err := bs.WriteBatch("x", []funnel.Candidate{{ID: "z", Kind: "wormhole", Text: "?"}})
	if err == nil {
		t.Fatal("an unsupported candidate kind must error (never a silent drop)")
	}
}

// StoreLedger snapshots the live state and records a decision into the persist ledger.
func TestStoreLedgerSnapshotAndRecord(t *testing.T) {
	dir := t.TempDir()
	st, _ := persist.NewJSONLStore(dir)
	led := StoreLedger{Store: st, Substrate: "claude:sonnet"}

	if err := led.Snapshot("auto:campaign-baseline"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	metas, _ := st.ListSnapshots()
	if len(metas) != 1 || metas[0].Name != "auto:campaign-baseline" {
		t.Fatalf("snapshot not recorded: %+v", metas)
	}
	if err := led.Record("KEEP", "SMARTER: significant lift"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, _ := st.LoadLedger()
	if len(entries) != 1 || entries[0].Substrate != "claude:sonnet" || entries[0].SubmittedBy != "campaign" {
		t.Fatalf("ledger entry wrong: %+v", entries)
	}
}

// FileGenerator reads a JSON candidate file and caps to the batch size.
func TestFileGenerator(t *testing.T) {
	cands := []funnel.Candidate{knowledgeCand("a", "x"), knowledgeCand("b", "y"), knowledgeCand("c", "z")}
	data, _ := json.Marshal(cands)
	path := filepath.Join(t.TempDir(), "cands.json")
	_ = os.WriteFile(path, data, 0o644)

	got, err := FileGenerator{Path: path}.Generate(2)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" {
		t.Fatalf("file generator capped wrong: %+v", got)
	}
}

// RealFunnel runs Tier-0 anti-filler: a filler candidate (no provenance/links/exercised) is dropped,
// a clean one is admitted; Tier-1 passes when no quizzes are supplied.
func TestRealFunnelTier0(t *testing.T) {
	clean := knowledgeCand("good", "a real grounded fact about the system")
	filler := funnel.Candidate{ID: "bad", Kind: "knowledge", ClusterKey: "bad", Text: "filler"} // no provenance/links/exercised
	admitted, tier1, err := RealFunnel{}.Screen([]funnel.Candidate{clean, filler})
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	if !tier1 {
		t.Error("Tier-1 should pass when no quizzes are configured")
	}
	if len(admitted) != 1 || admitted[0].ID != "good" {
		t.Fatalf("anti-filler should admit only the clean candidate, got %+v", admitted)
	}
}
