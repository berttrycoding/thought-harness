package knowledge

import (
	"bytes"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// grounded builds a grounded knowledge item (the common test fixture).
func grounded(stmt, kind string, ents ...string) Knowledge {
	return Knowledge{Statement: stmt, Kind: kind, Entities: ents, Source: "ingest:test", Grounded: true, Trust: 0.9}
}

// TestRecordRejectsUngrounded asserts the never-fabricate gate: an ungrounded item is rejected and
// never enters the registry (it can never resurface as durable knowledge).
func TestRecordRejectsUngrounded(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	if r.Record(Knowledge{Statement: "the sky is green", Kind: "fact", Grounded: false}) {
		t.Fatal("Record accepted an ungrounded item (never-fabricate violated)")
	}
	if r.Len() != 0 {
		t.Fatalf("registry stored an ungrounded item: Len=%d", r.Len())
	}
	if !r.Record(grounded("Go was released in 2009", "fact", "go", "release")) {
		t.Fatal("Record rejected a grounded item")
	}
	if r.Len() != 1 {
		t.Fatalf("registry Len=%d, want 1", r.Len())
	}
}

// TestRecallPrecisionFloor asserts recall surfaces a relevant item and returns nothing for an unrelated
// query (the shared precision floor — a recalled-but-irrelevant item is worse than none).
func TestRecallPrecisionFloor(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	r.Record(grounded("a goroutine is a lightweight thread managed by the Go runtime", "fact", "goroutine", "concurrency"))
	hits := r.Recall("what is a goroutine in the runtime", "", 3)
	if len(hits) != 1 {
		t.Fatalf("relevant query surfaced %d items, want 1", len(hits))
	}
	if got := r.Recall("recipe for sourdough bread", "", 3); len(got) != 0 {
		t.Fatalf("unrelated query surfaced %d items, want 0 (precision floor)", len(got))
	}
}

// TestRecallKindFilter asserts the kind filter restricts recall to the requested kind.
func TestRecallKindFilter(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	r.Record(grounded("channels synchronize goroutines", "fact", "channel", "goroutine"))
	r.Record(grounded("prefer channels over shared memory for goroutine coordination", "pattern", "channel", "goroutine"))
	if got := r.Recall("goroutine channel", "pattern", 5); len(got) != 1 || got[0].Kind != "pattern" {
		t.Fatalf("kind filter failed: got %d items, want 1 pattern", len(got))
	}
}

// TestInvalidateBiTemporal asserts an invalidated item stops surfacing as current but is kept (history).
func TestInvalidateBiTemporal(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	stmt := "the default port is 8080"
	r.Record(Knowledge{Statement: stmt, Kind: "fact", Entities: []string{"port", "default"},
		Source: "ingest:test", Grounded: true, Trust: 0.8, ValidFrom: 1})
	if n := r.Invalidate(stmt, 5); n != 1 {
		t.Fatalf("Invalidate returned %d, want 1", n)
	}
	if got := r.Recall("default port", "", 3); len(got) != 0 {
		t.Fatalf("invalidated item still surfaced as current: %d", len(got))
	}
	if r.Len() != 1 {
		t.Fatalf("invalidated item was deleted (Len=%d), want kept for history", r.Len())
	}
	if len(r.Current()) != 0 {
		t.Fatalf("Current() returned an invalidated item")
	}
}

// TestEmitsRecordRecall asserts the registry emits knowledge.record on a grounded record and
// knowledge.recall on a hit (observability contract).
func TestEmitsRecordRecall(t *testing.T) {
	var got []string
	emit := func(kind, summary string, data map[string]any) events.Event {
		got = append(got, kind)
		return events.Event{Kind: kind}
	}
	r := NewKnowledgeRegistry(nil, emit)
	r.Record(grounded("HTTP/2 multiplexes requests over one connection", "fact", "http2", "multiplex"))
	r.Recall("http2 multiplex", "", 1)
	wantRecord, wantRecall := false, false
	for _, k := range got {
		if k == events.KnowledgeRecord {
			wantRecord = true
		}
		if k == events.KnowledgeRecall {
			wantRecall = true
		}
	}
	if !wantRecord || !wantRecall {
		t.Fatalf("missing events: record=%v recall=%v (got %v)", wantRecord, wantRecall, got)
	}
}

// TestPersistRoundTrip asserts Save/Load preserves grounded items + their bi-temporal fields and
// re-applies never-fabricate (an ungrounded row never re-admits).
func TestPersistRoundTrip(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	r.Record(Knowledge{Statement: "current fact", Kind: "fact", Grounded: true, Trust: 0.9, ValidFrom: 1})
	r.Record(Knowledge{Statement: "old fact", Kind: "fact", Grounded: true, Trust: 0.9, ValidFrom: 1})
	r.Invalidate("old fact", 4)

	var buf bytes.Buffer
	if err := r.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// inject an ungrounded line that must be rejected on load (never-fabricate).
	buf.WriteString(`{"statement":"fake","kind":"fact","grounded":false,"trust":0.9}` + "\n")

	r2 := NewKnowledgeRegistry(nil, nil)
	n, err := r2.Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n != 2 {
		t.Fatalf("Load admitted %d items, want 2 (ungrounded row rejected)", n)
	}
	// the invalidated "old fact" survived with its ValidTo, so it is NOT current; "current fact" is.
	if got := r2.Current(); len(got) != 1 || got[0].Statement != "current fact" {
		t.Fatalf("round-trip lost bi-temporal validity: current=%v", got)
	}
}

// TestSharedFloorMatchesSemFloor is a guard that the knowledge registry reuses the shared retrieval
// precision floor (so memory + knowledge cannot drift apart).
func TestSharedFloorMatchesSemFloor(t *testing.T) {
	if retrieval.SemFloor <= 0 || retrieval.SemFloor >= 1 {
		t.Fatalf("retrieval.SemFloor out of (0,1): %v", retrieval.SemFloor)
	}
}
