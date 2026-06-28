package cognition

import "testing"

// TestProposeNeverAutoPromotes is the P7.4 gate: the offline optimizer produces a strictly-better
// variant as a REVIEWABLE proposal, and the live registry is UNCHANGED until a human/PR approves it
// (by invoking Refine). No auto-promotion.
func TestProposeNeverAutoPromotes(t *testing.T) {
	lib := NewSkillRegistry(true)
	body := seedProgramSynth(NewSeq(NewStep("measure", "general", "")))
	lib.Mint("learned-thing", []string{"frob"}, body, "", "v1")

	score := func(s Skill) float64 {
		for _, tr := range s.Triggers {
			if tr == "frob-fast" {
				return 1.0
			}
		}
		return 0.4
	}
	variant := Skill{Triggers: []string{"frob-fast"}, Body: seedProgramSynth(NewSeq(NewStep("rank", "general", "")))}

	p, ok := lib.ProposeRefinement("learned-thing", variant, score)
	if !ok {
		t.Fatal("a proposal should be produced for a minted skill")
	}
	if !p.Improvement || !p.Applicable {
		t.Fatalf("a strictly-better, clean variant should be an applicable improvement; got %+v", p)
	}
	if p.VariantScore <= p.IncumbentScore {
		t.Fatalf("the proposal should record a positive delta; %v vs %v", p.VariantScore, p.IncumbentScore)
	}

	// CRITICAL: proposing must NOT have changed the live skill (no auto-promotion).
	if cur, _ := lib.Get("learned-thing"); cur.Triggers[0] != "frob" {
		t.Fatalf("proposing must not mutate the live registry; triggers=%v", cur.Triggers)
	}

	// approval = applying via Refine; only THEN does the live system change.
	if kept, _ := lib.Refine("learned-thing", p.Variant, score); !kept {
		t.Fatal("the approved proposal should apply via Refine")
	}
	if cur, _ := lib.Get("learned-thing"); cur.Triggers[0] != "frob-fast" {
		t.Fatalf("after approval the live skill should be the variant; triggers=%v", cur.Triggers)
	}
}

// TestProposeNonImprovementNotApplicable: a variant that doesn't beat the incumbent is proposed but
// marked not-applicable (reviewer sees it's not worth promoting).
func TestProposeNonImprovementNotApplicable(t *testing.T) {
	lib := NewSkillRegistry(true)
	lib.Mint("learned-thing", []string{"frob"}, seedProgramSynth(NewSeq(NewStep("measure", "general", ""))), "", "v1")
	flat := func(Skill) float64 { return 0.5 }
	variant := Skill{Triggers: []string{"other"}, Body: seedProgramSynth(NewSeq(NewStep("rank", "general", "")))}
	p, _ := lib.ProposeRefinement("learned-thing", variant, flat)
	if p.Improvement || p.Applicable {
		t.Fatalf("a non-improving variant must not be applicable; got %+v", p)
	}
}

// TestProposeSeedRejected: a seed skill can't be optimised (frozen).
func TestProposeSeedRejected(t *testing.T) {
	lib := NewSkillRegistry(true)
	if _, ok := lib.ProposeRefinement("diagnose", Skill{}, func(Skill) float64 { return 1 }); ok {
		t.Fatal("proposing against a seed skill must be rejected (frozen)")
	}
}
