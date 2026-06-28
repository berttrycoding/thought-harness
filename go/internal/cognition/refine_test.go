package cognition

import "testing"

// TestRefineKeepsBetterRevertsWorse is the P7.2 gate: a refined variant is KEPT only if it strictly
// beats the incumbent on the fast eval, otherwise reverted; a seed skill is frozen.
func TestRefineKeepsBetterRevertsWorse(t *testing.T) {
	lib := NewSkillRegistry(true)
	body := seedProgramSynth(NewSeq(NewStep("measure", "general", "")))
	if _, ok := lib.Mint("learned-thing", []string{"frobnicate"}, body, "", "v1"); !ok {
		t.Fatal("mint must succeed")
	}

	// a fast eval that prefers the variant carrying the sharper trigger "frobnicate-fast".
	score := func(s Skill) float64 {
		for _, tr := range s.Triggers {
			if tr == "frobnicate-fast" {
				return 1.0
			}
		}
		return 0.5
	}

	better := Skill{Triggers: []string{"frobnicate-fast"}, Body: seedProgramSynth(NewSeq(NewStep("rank", "general", "")))}
	kept, reason := lib.Refine("learned-thing", better, score)
	if !kept {
		t.Fatalf("a strictly-better variant must be kept; got reverted (%s)", reason)
	}
	if got, _ := lib.Get("learned-thing"); got.Triggers[0] != "frobnicate-fast" {
		t.Fatalf("the kept refinement must replace the incumbent; triggers=%v", got.Triggers)
	}

	// a variant that does NOT beat the incumbent is reverted (incumbent stands).
	worse := Skill{Triggers: []string{"meh"}, Body: seedProgramSynth(NewSeq(NewStep("measure", "general", "")))}
	if kept, _ := lib.Refine("learned-thing", worse, score); kept {
		t.Fatal("a non-improving variant must be reverted")
	}
	if got, _ := lib.Get("learned-thing"); got.Triggers[0] != "frobnicate-fast" {
		t.Fatalf("after a revert the incumbent must be unchanged; triggers=%v", got.Triggers)
	}
}

// TestRefineSeedIsFrozen: a seed skill is a frozen invariant — refining it is rejected.
func TestRefineSeedIsFrozen(t *testing.T) {
	lib := NewSkillRegistry(true)
	variant := Skill{Triggers: []string{"x"}, Body: seedProgramSynth(NewSeq(NewStep("measure", "general", "")))}
	kept, reason := lib.Refine("diagnose", variant, func(Skill) float64 { return 999 })
	if kept {
		t.Fatal("refining a seed skill must be rejected (frozen invariant)")
	}
	if reason == "" {
		t.Fatal("a rejection should carry a reason")
	}
}

// TestRefineRejectsMalformedVariant: a refinement can't install a malformed (empty) body.
func TestRefineRejectsMalformedVariant(t *testing.T) {
	lib := NewSkillRegistry(true)
	body := seedProgramSynth(NewSeq(NewStep("measure", "general", "")))
	lib.Mint("learned-thing", []string{"frobnicate"}, body, "", "v1")
	bad := Skill{Triggers: []string{"x"}, Body: seedProgramSynth(NewSeq())} // empty body
	if kept, _ := lib.Refine("learned-thing", bad, func(Skill) float64 { return 999 }); kept {
		t.Fatal("a variant with an empty body must be rejected, not kept")
	}
}
