package grounding

import "testing"

// TestGroundingSpineRefutesFalseClaims is the N.1 validation: the SR-4 anti-hallucination spine kills a
// hallucination by GROUNDING a claim against reality, not by judging how plausible it looks. The Filter
// is triage; this is the verdict. The check: a battery of confident, plausible-sounding-but-FALSE
// computable claims is REFUTED by the grounding loop; true claims are grounded; and a claim with no
// grounding handle is left UNGROUNDED (never fabricated as true). This is the central thesis, validated
// end-to-end over the layered store (experiment memory → deterministic compute).
func TestGroundingSpineRefutesFalseClaims(t *testing.T) {
	m := NewExperimentMemory()

	false_ := []string{
		"12 * 31 = 999", // confident, wrong
		"the total is 50 + 50 = 110",
		"2 ^ 10 = 1000", // close to 1024, still wrong
		"8472 / 31 = 280",
	}
	for _, c := range false_ {
		r, _ := m.Ground(c, 1)
		if r.Verdict != Refuted {
			t.Errorf("a false claim must be REFUTED, not accepted: %q -> %v", c, r.Verdict)
		}
	}

	true_ := []string{"12 * 31 = 372", "50 + 50 = 100", "2 ^ 10 = 1024"}
	for _, c := range true_ {
		r, _ := m.Ground(c, 1)
		if r.Verdict != Grounded {
			t.Errorf("a true claim must be GROUNDED: %q -> %v", c, r.Verdict)
		}
	}

	// a plausible-but-unverifiable claim (no grounding handle) must stay UNGROUNDED — the spine refuses
	// to fabricate a "true". It is NotComputable, and its epistemic status is UNKNOWN, never KNOW.
	plausible := "the refactor is definitely safe to ship to production"
	if r, _ := m.Ground(plausible, 1); r.Verdict != NotComputable {
		t.Fatalf("an unverifiable claim must stay ungrounded (NotComputable), not be accepted; got %v", r.Verdict)
	}
	if m.Status(plausible) != Unknown {
		t.Fatalf("an ungrounded claim must be UNKNOWN, never KNOW; got %v", m.Status(plausible))
	}
}

// TestGroundingBeatsConfidentHallucination is the load-bearing case: a hallucination asserted with FULL
// confidence by testimony (the model "knows" it) is overturned by grounding it. Plausibility/confidence
// does not protect a false claim — reality does the deciding.
func TestGroundingBeatsConfidentHallucination(t *testing.T) {
	m := NewExperimentMemory()

	// the model confidently asserts a wrong sum (testimony, high "confidence" but low trust tier).
	m.RecordSource("17 * 23 = 400", Grounded, TierTestimony, "the model is sure of it", 1)
	// grounding the same claim deterministically refutes it (17*23 = 391), and the higher tier wins.
	if r, _ := m.Ground("17 * 23 = 400", 2); r.Verdict != Refuted {
		t.Fatalf("deterministic grounding must overturn the confident hallucination; got %v", r.Verdict)
	}
	if e, _ := m.Recall("17 * 23 = 400"); e.Verdict != Refuted || e.Tier != TierDeterministic {
		t.Fatalf("the claim should resolve to the refuting deterministic verdict; got %v (%s)", e.Verdict, e.Tier)
	}
	// and the system now KNOWS-something (BELIEVE: it's false), not a fabricated KNOW-true.
	if m.Status("17 * 23 = 400") != Believe {
		t.Fatalf("a refuted claim is BELIEVE (grounded to false), not KNOW; got %v", m.Status("17 * 23 = 400"))
	}
}
