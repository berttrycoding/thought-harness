package persist

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResumeRecordSaveLoad: a cold store has no cursor; after SaveResume the cursor
// round-trips (streams + tick + substrate) through LoadResume.
func TestResumeRecordSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if r, err := s.LoadResume(); err != nil || r != nil {
		t.Fatalf("cold LoadResume = (%v, %v), want (nil, nil)", r, err)
	}

	rec := ResumeRecord{
		Streams: map[string]RNGStreamState{
			"main":   {Words: []uint32{1, 2, 3}, Index: 624},
			"wander": {Words: []uint32{9, 8}, Index: 7},
		},
		Tick:      42,
		Substrate: "test",
	}
	if err := s.SaveResume(rec); err != nil {
		t.Fatal(err)
	}

	got, err := s.LoadResume()
	if err != nil || got == nil {
		t.Fatalf("LoadResume = (%v, %v)", got, err)
	}
	if got.Tick != 42 || got.Substrate != "test" {
		t.Fatalf("meta mismatch: tick=%d substrate=%q", got.Tick, got.Substrate)
	}
	if len(got.Streams) != 2 || got.Streams["main"].Index != 624 || len(got.Streams["wander"].Words) != 2 {
		t.Fatalf("streams mismatch: %+v", got.Streams)
	}
}

// TestResumeCorruptFileColdStarts: an unparsable resume.json degrades to a cold start
// (nil, nil), never a crash — a corrupt cursor must not brick the boot.
func TestResumeCorruptFileColdStarts(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileResume), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := s.LoadResume(); err != nil || r != nil {
		t.Fatalf("corrupt LoadResume = (%v, %v), want (nil, nil)", r, err)
	}
}
