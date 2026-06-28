package session

import "testing"

// TestMergeReduceDedups is the P3.6 gate (reduce half): Reduce combines a fan-out's results into a
// unique set, dropping near-duplicates (whitespace/case-insensitive).
func TestMergeReduceDedups(t *testing.T) {
	res := Merge([]string{"add an index", "add  an INDEX", "use a cache", "add an index"}, Reduce)
	if res.Conflict {
		t.Fatal("Reduce is combining a plan — it must never conflict")
	}
	if len(res.Combined) != 2 {
		t.Fatalf("Reduce should dedup to 2 unique results; got %v", res.Combined)
	}
}

// TestMergeVoteMajority is the vote half: a strict majority wins, no conflict.
func TestMergeVoteMajority(t *testing.T) {
	res := Merge([]string{"ship it", "ship it", "hold"}, Vote)
	if res.Conflict {
		t.Fatalf("a strict majority must not be a conflict; got %+v", res)
	}
	if res.Winner != "ship it" {
		t.Fatalf("the majority result should win; got %q", res.Winner)
	}
}

// TestMergeVoteSplitIsConflict is the conflict→branch case: a split vote (no majority) is a genuine
// disagreement and must surface as a Conflict (the engine branches it), never silently merged.
func TestMergeVoteSplitIsConflict(t *testing.T) {
	if res := Merge([]string{"yes", "no"}, Vote); !res.Conflict {
		t.Fatalf("a tied vote must be a conflict (branch), not a silent merge; got %+v", res)
	}
	if res := Merge([]string{"a", "b", "c"}, Vote); !res.Conflict {
		t.Fatalf("a 3-way split (no majority) must be a conflict; got %+v", res)
	}
	if res := Merge([]string{"a", "a", "b", "b", "c"}, Vote); !res.Conflict {
		t.Fatalf("no strict majority (2-2-1) must be a conflict; got %+v", res)
	}
}
