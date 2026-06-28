package persist

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGraphSpineSaveLoad: a cold store has no spine; after SaveGraphSpine the record
// round-trips (version + substrate + the L1 projection) through LoadGraphSpine.
func TestGraphSpineSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if r, err := s.LoadGraphSpine(); err != nil || r != nil {
		t.Fatalf("cold LoadGraphSpine = (%v, %v), want (nil, nil)", r, err)
	}

	rec := GraphSpineRecord{
		Version:    GraphSpineVersion,
		Substrate:  "test",
		Goal:       "diagnose the flaky deploy",
		BranchID:   4,
		Gist:       "narrowed to a race in the cache warm-up",
		ThoughtIDs: []int{1, 2, 5, 8},
		Resolution: "EXPANDED",
	}
	if err := s.SaveGraphSpine(rec); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadGraphSpine()
	if err != nil || got == nil {
		t.Fatalf("LoadGraphSpine = (%v, %v)", got, err)
	}
	if got.Version != GraphSpineVersion || got.Substrate != "test" {
		t.Fatalf("meta mismatch: version=%d substrate=%q", got.Version, got.Substrate)
	}
	if got.Goal != rec.Goal || got.BranchID != rec.BranchID || got.Gist != rec.Gist || got.Resolution != rec.Resolution {
		t.Fatalf("spine fields mismatch: %+v", got)
	}
	if len(got.ThoughtIDs) != 4 || got.ThoughtIDs[3] != 8 {
		t.Fatalf("thought IDs mismatch: %+v", got.ThoughtIDs)
	}
}

// TestGraphSpineCorruptFileColdStarts: an unparsable graph_spine.json degrades to a cold
// start (nil, nil), never a crash — a corrupt spine must not brick the boot (the engine
// then boots as-if-cold).
func TestGraphSpineCorruptFileColdStarts(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileGraphSpine), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := s.LoadGraphSpine(); err != nil || r != nil {
		t.Fatalf("corrupt LoadGraphSpine = (%v, %v), want (nil, nil)", r, err)
	}
}
