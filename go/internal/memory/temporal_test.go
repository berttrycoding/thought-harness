package memory

import "testing"

// TestBiTemporalAsOfCorrectness is the P6.1 (T1) gate: assert a belief at t1, overturn it at t2, then
//   - a CURRENT query returns the NEW belief, never the stale one;
//   - an AS-OF t1 query returns the OLD belief (history is exactly reconstructable);
//   - an AS-OF after t2 returns the new belief.
//
// Fully deterministic on the seeded tick clock.
func TestBiTemporalAsOfCorrectness(t *testing.T) {
	r := NewSemanticRegistry(nil)
	const t1, t2 = 1, 5

	old := Belief{Statement: "the cache TTL is sixty seconds", Entities: []string{"cache", "ttl"}, Grounded: true, ValidFrom: t1}
	r.Record(old)

	// at t2 reality overturns it: invalidate the old, assert the corrected belief.
	if n := r.Invalidate(old.Statement, t2); n != 1 {
		t.Fatalf("invalidating the old belief should affect 1 row, got %d", n)
	}
	r.Record(Belief{Statement: "the cache TTL is one hundred and twenty seconds", Entities: []string{"cache", "ttl"}, Grounded: true, ValidFrom: t2})

	q := "how long does the cache live"

	// current -> the new value, never the stale one.
	cur := r.Recall(q, 5)
	if len(cur) != 1 {
		t.Fatalf("current recall should return exactly the one valid belief; got %d: %v", len(cur), statements(cur))
	}
	if cur[0].Statement != "the cache TTL is one hundred and twenty seconds" {
		t.Fatalf("current recall returned the stale belief: %q", cur[0].Statement)
	}

	// as of t1 (between assertion and overturn) -> the OLD value.
	asOfT1 := r.RecallAsOf(q, 5, 3)
	if len(asOfT1) != 1 || asOfT1[0].Statement != "the cache TTL is sixty seconds" {
		t.Fatalf("as-of t=3 should return the old belief; got %v", statements(asOfT1))
	}

	// as of after the overturn -> the NEW value.
	asOfLater := r.RecallAsOf(q, 5, 10)
	if len(asOfLater) != 1 || asOfLater[0].Statement != "the cache TTL is one hundred and twenty seconds" {
		t.Fatalf("as-of t=10 should return the new belief; got %v", statements(asOfLater))
	}

	// as of BEFORE the old belief was asserted -> nothing.
	if early := r.RecallAsOf(q, 5, 0); len(early) != 0 {
		t.Fatalf("as-of before any assertion should return nothing; got %v", statements(early))
	}
}

func statements(bs []Belief) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Statement
	}
	return out
}
