package memory

import "testing"

// TestReflectionTransferAndNoFalseBeliefs is the P6.2 (C1) gate: reflection distils a grounded,
// high-value episode into a belief a later related task can recall (TRANSFER), while a low-value /
// refuted episode produces NO belief and invalidates any matching one (ZERO false beliefs).
func TestReflectionTransferAndNoFalseBeliefs(t *testing.T) {
	epis := NewEpisodicRegistry(nil)
	sem := NewSemanticRegistry(nil)

	// a grounded, high-value episode — its lesson should transfer.
	epis.Record(Episode{
		Goal: "make the database queries faster", Entities: []string{"database", "index"},
		Outcome:  "adding an index on the hot column drops query latency to milliseconds",
		Grounded: true, Value: 0.85, Tick: 1,
	})
	// a grounded but LOW-value episode (reality didn't back it) — must NOT become a belief.
	epis.Record(Episode{
		Goal: "speed it up", Entities: []string{"cache"},
		Outcome:  "caching everything forever makes it fast",
		Grounded: true, Value: 0.05, Tick: 1,
	})

	n := Reflect(epis, sem, 0.5, 2)
	if n != 1 {
		t.Fatalf("exactly one high-value episode should distil a belief; got %d", n)
	}

	// TRANSFER: a later related query recalls the distilled lesson.
	got := sem.Recall("how do I cut down slow database lookups", 1)
	if len(got) == 0 {
		t.Fatal("the distilled belief should transfer to a later related query")
	}

	// ZERO false beliefs: the low-value 'caching everything' line never became a belief.
	for _, b := range sem.beliefs {
		if b.Statement == "caching everything forever makes it fast" {
			t.Fatal("a low-value (refuted) episode must not become a belief")
		}
	}

	// idempotent: reflecting again distils nothing new.
	if again := Reflect(epis, sem, 0.5, 3); again != 0 {
		t.Fatalf("re-reflection should distil 0 new beliefs; got %d", again)
	}
}

// TestReflectionInvalidatesRefutedBelief: if a belief was distilled, then a later episode with the same
// outcome grounds out LOW (reality refuted it), reflection invalidates the standing belief — it stops
// surfacing as current.
func TestReflectionInvalidatesRefutedBelief(t *testing.T) {
	epis := NewEpisodicRegistry(nil)
	sem := NewSemanticRegistry(nil)

	epis.Record(Episode{Goal: "tune it", Outcome: "the batch size of 64 is optimal", Grounded: true, Value: 0.8, Tick: 1})
	Reflect(epis, sem, 0.5, 2)
	if len(sem.Recall("optimal batch size", 1)) == 0 {
		t.Fatal("the belief should be current after the first reflection")
	}

	// later, reality refutes it (a low-value episode with the same outcome).
	epis.Record(Episode{Goal: "retune it", Outcome: "the batch size of 64 is optimal", Grounded: true, Value: 0.1, Tick: 5})
	Reflect(epis, sem, 0.5, 6)

	if len(sem.Recall("optimal batch size", 1)) != 0 {
		t.Fatal("a belief reality later refuted must be invalidated (no stale false belief stands)")
	}
}
