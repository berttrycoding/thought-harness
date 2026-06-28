package grounding

import "testing"

// TestExternalRealityRefutesConfidentFalsehood is the N.1a gate: a confident novel claim, believed at a
// low trust tier, is REFUTED the moment a REAL observation from the action subsystem contradicts it.
// This is the external-reality wire — grounding against what actually happened, not plausibility.
func TestExternalRealityRefutesConfidentFalsehood(t *testing.T) {
	m := NewExperimentMemory()
	claim := "the migration runs cleanly"

	// the system confidently believes the claim (testimony / the model is sure).
	m.RecordSource(claim, Grounded, TierTestimony, "the plan looks right", 1)
	if m.Status(claim) != Heard {
		t.Fatalf("a testimony-level belief should be HEARD; got %v", m.Status(claim))
	}

	// it ACTs, and a REAL observation comes back FAILED (the migration broke) — not fabricated.
	if !m.IngestObservation(claim, false /*ok*/, false /*fabricated*/, 2) {
		t.Fatal("a real observation must be ingested into the grounding loop")
	}

	// the confident falsehood is now refuted — firsthand observation outranks testimony.
	if e, _ := m.Recall(claim); e.Verdict != Refuted || e.Tier != TierFirsthandObservation {
		t.Fatalf("a real failing observation must refute the confident claim; got %v (%s)", e.Verdict, e.Tier)
	}
	if m.Status(claim) != Believe {
		t.Fatalf("after refutation the stance is BELIEVE (grounded to false); got %v", m.Status(claim))
	}
}

// TestFabricatedObservationCannotRefute is the P0.6 integrity carried through N.1a: a FABRICATED
// observation (the offline stand-in) is tier-0 and must NOT ground — it can neither validate nor refute,
// so the grounding loop is never poisoned by fake reality.
func TestFabricatedObservationCannotRefute(t *testing.T) {
	m := NewExperimentMemory()
	claim := "the tests pass"
	m.RecordSource(claim, Grounded, TierFirsthandObservation, "we ran them once", 1)

	// a FABRICATED failing observation tries to refute — it must be rejected.
	if m.IngestObservation(claim, false, true /*fabricated*/, 2) {
		t.Fatal("a fabricated observation must NOT be ingested (tier-0)")
	}
	if e, _ := m.Recall(claim); e.Verdict != Grounded {
		t.Fatalf("fabricated 'reality' must not overturn a genuinely-grounded claim; got %v", e.Verdict)
	}
}

// TestRealObservationGroundsSuccess: a real SUCCEEDING observation grounds a claim to true (the positive
// direction of the wire).
func TestRealObservationGroundsSuccess(t *testing.T) {
	m := NewExperimentMemory()
	claim := "the build compiles"
	m.IngestObservation(claim, true, false, 1)
	if e, _ := m.Recall(claim); e.Verdict != Grounded {
		t.Fatalf("a real succeeding observation should ground the claim true; got %v", e.Verdict)
	}
}
