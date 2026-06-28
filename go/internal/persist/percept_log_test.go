package persist

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPerceptLogSaveLoad: a cold store has no percept-log; after SavePerceptLog the
// record round-trips (version + substrate + entries) through LoadPerceptLog.
func TestPerceptLogSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if r, err := s.LoadPerceptLog(); err != nil || r != nil {
		t.Fatalf("cold LoadPerceptLog = (%v, %v), want (nil, nil)", r, err)
	}

	rec := PerceptLogRecord{
		Version:   PerceptLogVersion,
		Substrate: "test",
		Entries: []PerceptEntry{
			{Tick: 0, Kind: "clock", Value: "2026-01-01T00:00:00Z"},
			{Tick: 3, Kind: "clock", Value: "2026-01-01T00:00:03Z"},
		},
	}
	if err := s.SavePerceptLog(rec); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadPerceptLog()
	if err != nil || got == nil {
		t.Fatalf("LoadPerceptLog = (%v, %v)", got, err)
	}
	if got.Version != PerceptLogVersion || got.Substrate != "test" {
		t.Fatalf("meta mismatch: version=%d substrate=%q", got.Version, got.Substrate)
	}
	if len(got.Entries) != 2 || got.Entries[1].Tick != 3 || got.Entries[1].Value != "2026-01-01T00:00:03Z" {
		t.Fatalf("entries mismatch: %+v", got.Entries)
	}
}

// TestPerceptLogCorruptFileColdStarts: an unparsable percept_log.json degrades to a
// cold start (nil, nil), never a crash — a corrupt log must not brick the boot (the
// engine then cold-senses).
func TestPerceptLogCorruptFileColdStarts(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filePerceptLog), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := s.LoadPerceptLog(); err != nil || r != nil {
		t.Fatalf("corrupt LoadPerceptLog = (%v, %v), want (nil, nil)", r, err)
	}
}
