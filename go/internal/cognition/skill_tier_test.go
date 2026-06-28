package cognition

import "testing"

// TestSkillTierCeiling pins the #31 Skill-reframe tier facet: a Skill's string tier maps to a numeric
// authority coordinate, and a §3.3a tier ceiling caps which skills a goal may staff — a shallow ceiling
// admits a unit skill but not a composite one; ceiling 0 is uncapped.
func TestSkillTierCeiling(t *testing.T) {
	unit := Skill{Name: "u", Tier: "unit"}
	comp := Skill{Name: "c", Tier: "composite"}

	if unit.TierLevel() != 1 || comp.TierLevel() != 2 {
		t.Fatalf("tier levels: unit=%d comp=%d, want 1/2", unit.TierLevel(), comp.TierLevel())
	}

	// ceiling 1 admits a unit, refuses a composite.
	if !unit.WithinTierCeiling(1) || comp.WithinTierCeiling(1) {
		t.Error("ceiling 1: unit allowed, composite refused")
	}
	// ceiling 2 admits both.
	if !unit.WithinTierCeiling(2) || !comp.WithinTierCeiling(2) {
		t.Error("ceiling 2: both allowed")
	}
	// ceiling 0 is uncapped.
	if !comp.WithinTierCeiling(0) {
		t.Error("ceiling 0 must be uncapped")
	}
}

// TestMatchWithinTier pins the ceiling-bounded match: a composite skill matched at a shallow ceiling is
// excluded; uncapped (0) returns it.
func TestMatchWithinTier(t *testing.T) {
	r := NewSkillRegistry(false)
	body := Program{Root: Step{Operator: "decompose", Domain: "code"}}
	if _, ok := r.Mint("deepskill", []string{"refactor"}, body, "composite", "a deep composite"); !ok {
		t.Fatal("mint a composite skill for the test")
	}

	// uncapped -> the composite skill matches.
	if _, ok := r.MatchWithinTier("please refactor this", 0); !ok {
		t.Error("uncapped: the composite skill should match")
	}
	// ceiling 1 (unit-only) -> the composite skill is excluded.
	if _, ok := r.MatchWithinTier("please refactor this", 1); ok {
		t.Error("ceiling 1: a composite skill must be excluded from staffing")
	}
}
