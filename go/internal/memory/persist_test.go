package memory

import (
	"bytes"
	"testing"
)

// TestEpisodicPersistenceRoundTrip: episodes survive a restart and stay recallable; never-fabricate is
// re-applied on load (an ungrounded row is rejected).
func TestEpisodicPersistenceRoundTrip(t *testing.T) {
	a := NewEpisodicRegistry(nil)
	seedEpisodes(a)

	var buf bytes.Buffer
	if err := a.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// append a hand-forged ungrounded line — it must NOT load (never-fabricate survives restart).
	buf.WriteString(`{"Goal":"forged","Outcome":"made up","Grounded":false}` + "\n")

	b := NewEpisodicRegistry(nil)
	n, err := b.Load(&buf)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n != a.Len() {
		t.Fatalf("loaded %d episodes, expected %d (the ungrounded forgery must be rejected)", n, a.Len())
	}
	got := b.Recall("the database is slow, speed up the queries", 1)
	if len(got) == 0 || got[0].Goal != "make the database queries faster" {
		t.Fatalf("a recalled episode must survive the restart; got %v", got)
	}
}

// TestSemanticBiTemporalPersistence: a belief and its invalidation reconstruct EXACTLY across a restart —
// current recall returns the new value, as-of recall returns the old (bi-temporal history preserved).
func TestSemanticBiTemporalPersistence(t *testing.T) {
	a := NewSemanticRegistry(nil)
	a.Record(Belief{Statement: "the cache TTL is sixty seconds", Entities: []string{"cache", "ttl"}, Grounded: true, ValidFrom: 1})
	a.Invalidate("the cache TTL is sixty seconds", 5)
	a.Record(Belief{Statement: "the cache TTL is one hundred and twenty seconds", Entities: []string{"cache", "ttl"}, Grounded: true, ValidFrom: 5})

	var buf bytes.Buffer
	if err := a.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}

	b := NewSemanticRegistry(nil)
	if _, err := b.Load(&buf); err != nil {
		t.Fatalf("Load: %v", err)
	}

	q := "how long does the cache live"
	if cur := b.Recall(q, 1); len(cur) != 1 || cur[0].Statement != "the cache TTL is one hundred and twenty seconds" {
		t.Fatalf("current recall after restart should return the new belief; got %v", cur)
	}
	if old := b.RecallAsOf(q, 1, 3); len(old) != 1 || old[0].Statement != "the cache TTL is sixty seconds" {
		t.Fatalf("as-of recall after restart should return the old belief; got %v", old)
	}
}
