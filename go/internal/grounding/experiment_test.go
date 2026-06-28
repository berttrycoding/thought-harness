package grounding

import "testing"

// TestExperimentMemoryReuseNotRerun is the N.1e gate: a claim already validated is REUSED from memory,
// not re-grounded. First Ground computes + stores; the second Ground returns the stored verdict with
// reused=true.
func TestExperimentMemoryReuseNotRerun(t *testing.T) {
	m := NewExperimentMemory()

	r1, reused1 := m.Ground("12 * 31 = 372", 1)
	if reused1 {
		t.Fatal("the first grounding of a fresh claim must NOT be a reuse")
	}
	if r1.Verdict != Grounded {
		t.Fatalf("12*31=372 should ground; got %v", r1.Verdict)
	}
	if m.Len() != 1 {
		t.Fatalf("a real grounding result should be recorded once; len=%d", m.Len())
	}

	r2, reused2 := m.Ground("12*31=372", 2) // same claim, different surface form
	if !reused2 {
		t.Fatal("a re-asked validated claim must be REUSED from memory, not re-run")
	}
	if r2.Verdict != Grounded {
		t.Fatalf("the reused result should still be Grounded; got %v", r2.Verdict)
	}
	if m.Len() != 1 {
		t.Fatalf("reuse must not record a second attempt; len=%d", m.Len())
	}
	if m.Status("12 * 31 = 372") != Know {
		t.Fatalf("a deterministically-grounded claim should be KNOW; got %v", m.Status("12 * 31 = 372"))
	}
}

// TestExperimentMemoryNeverFabricate: a non-real (tier-0 / fabricated) result, and a non-computable
// claim, are never stored — validation memory holds only genuine grounding.
func TestExperimentMemoryNeverFabricate(t *testing.T) {
	m := NewExperimentMemory()

	if m.Record(Experiment{Claim: "the build passed", Verdict: Grounded, Real: false}) {
		t.Fatal("a fabricated (Real=false) result must be rejected (never-fabricate)")
	}
	if m.Record(Experiment{Claim: "x", Verdict: NotComputable, Real: true}) {
		t.Fatal("a not-computable result is not a grounding and must not be stored")
	}
	if m.Len() != 0 {
		t.Fatalf("nothing should have been stored; len=%d", m.Len())
	}

	// a non-arithmetic claim grounds to NotComputable and is NOT stored (stays ungrounded).
	if _, reused := m.Ground("the refactor is safe to ship", 1); reused {
		t.Fatal("a non-computable claim cannot be a reuse")
	}
	if m.Len() != 0 {
		t.Fatalf("a non-computable claim must not enter validation memory; len=%d", m.Len())
	}
	if m.Status("the refactor is safe to ship") != Unknown {
		t.Fatal("an ungrounded claim must be UNKNOWN")
	}
}

// TestExperimentMemoryRefutationKnown: a refuted claim is recorded (we DID ground it — to false) and is
// reused, with BELIEVE status (we know something, not a positive KNOW).
func TestExperimentMemoryRefutationStored(t *testing.T) {
	m := NewExperimentMemory()
	r, _ := m.Ground("2 + 2 = 5", 1)
	if r.Verdict != Refuted {
		t.Fatalf("2+2=5 should be refuted; got %v", r.Verdict)
	}
	if _, reused := m.Ground("2+2=5", 2); !reused {
		t.Fatal("a refuted claim is still grounded knowledge and must be reused")
	}
	if m.Status("2 + 2 = 5") != Believe {
		t.Fatalf("a refuted claim should be BELIEVE (we grounded it to false); got %v", m.Status("2 + 2 = 5"))
	}
}
