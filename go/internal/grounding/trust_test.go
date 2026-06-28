package grounding

import "testing"

// TestTrustTierOverridesOnConflict is the N.1d gate: when two sources DISAGREE about a claim, the
// claim resolves to the HIGHER-trust source's verdict — regardless of arrival order.
func TestTrustTierOverridesOnConflict(t *testing.T) {
	claim := "the deploy is safe"

	// low tier first, high tier second.
	m1 := NewExperimentMemory()
	m1.RecordSource(claim, Grounded, TierTestimony, "a teammate said so", 1) // HEARD: "it's safe"
	m1.RecordSource(claim, Refuted, TierFirsthandValidated, "we tested it and it broke", 2)
	if e, _ := m1.Recall(claim); e.Verdict != Refuted || e.Tier != TierFirsthandValidated {
		t.Fatalf("the firsthand-validated refutation must override the testimony; got %v (%s)", e.Verdict, e.Tier)
	}

	// high tier first, low tier second — order must not matter: the high tier still wins.
	m2 := NewExperimentMemory()
	m2.RecordSource(claim, Refuted, TierFirsthandValidated, "we tested it and it broke", 1)
	m2.RecordSource(claim, Grounded, TierTestimony, "a teammate said so", 2)
	if e, _ := m2.Recall(claim); e.Verdict != Refuted || e.Tier != TierFirsthandValidated {
		t.Fatalf("a later lower-tier source must NOT override a higher-tier one; got %v (%s)", e.Verdict, e.Tier)
	}
	if m2.Status(claim) != Believe {
		t.Fatalf("a refuted high-tier claim should be BELIEVE; got %v", m2.Status(claim))
	}
}

// TestTrustTierFullOrdering pins the source ordering firsthand-validated > deterministic >
// firsthand-observation > authoritative-ref > web > testimony: each higher tier overrides the one below.
func TestTrustTierFullOrdering(t *testing.T) {
	order := []TrustTier{
		TierTestimony, TierWeb, TierAuthoritativeRef, TierFirsthandObservation, TierDeterministic, TierFirsthandValidated,
	}
	for i := 0; i+1 < len(order); i++ {
		lo, hi := order[i], order[i+1]
		if !(hi > lo) {
			t.Fatalf("tier %s must outrank %s", hi, lo)
		}
		m := NewExperimentMemory()
		m.RecordSource("claim c", Grounded, lo, "low", 1)
		m.RecordSource("claim c", Refuted, hi, "high", 2)
		if e, _ := m.Recall("claim c"); e.Tier != hi || e.Verdict != Refuted {
			t.Fatalf("tier %s should override %s; resolved to %s/%v", hi, lo, e.Tier, e.Verdict)
		}
	}
}

// TestComputeBeatsTestimony ties it together with the real grounding loop: a claim "heard" to be true is
// overridden by deterministic computation showing it false.
func TestComputeBeatsTestimony(t *testing.T) {
	m := NewExperimentMemory()
	m.RecordSource("2 + 2 = 5", Grounded, TierTestimony, "someone insisted", 1) // hearsay says it's true
	m.Ground("2 + 2 = 5", 2)                                                    // compute (deterministic) says false
	if e, _ := m.Recall("2 + 2 = 5"); e.Verdict != Refuted || e.Tier != TierDeterministic {
		t.Fatalf("deterministic computation must override testimony; got %v (%s)", e.Verdict, e.Tier)
	}
}
