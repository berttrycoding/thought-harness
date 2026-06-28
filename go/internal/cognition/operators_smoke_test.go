package cognition

import "testing"

// TestSeedRegistry confirms the seed taxonomy loads with the expected size, families, and
// tool-scope declarations, and that a seed operator is a frozen invariant.
func TestSeedRegistry(t *testing.T) {
	r := NewOperatorRegistry()
	if got := len(r.Names()); got != 34 {
		t.Fatalf("seed op count: want 34, got %d", got)
	}
	if !r.Has("decompose") || !r.Has("curate") {
		t.Fatal("expected seed ops missing")
	}
	if fam, ok := r.Family("measure"); !ok || fam != "relational" {
		t.Fatalf("measure family: want relational, got %q ok=%v", fam, ok)
	}
	if spec, ok := r.Get("measure"); !ok || len(spec.ToolScope) != 1 || spec.ToolScope[0] != "run_tests" {
		t.Fatalf("measure tool_scope: %+v ok=%v", spec, ok)
	}
	if spec, ok := r.Get("expose-affordances"); !ok || len(spec.ToolScope) != 2 {
		t.Fatalf("expose-affordances tool_scope: %+v ok=%v", spec, ok)
	}
	// a seed operator is frozen — Mint must reject redefinition.
	if _, ok := r.Mint("decompose", "transformative", "redefine a frozen seed operator"); ok {
		t.Fatal("expected redefining a seed operator to be rejected")
	}
	if ok, reason := r.Verify("decompose", "transformative", "redefine a frozen seed operator"); ok {
		t.Fatalf("expected verify reject for seed op, got ok with reason %q", reason)
	}
}

// TestMint exercises the runtime minting path: verify (identifier + family + >=3-word intent)
// then insert; the minted flag; idempotency; and Names() ordering (seed then minted).
func TestMint(t *testing.T) {
	r := NewOperatorRegistry()

	// rejects: empty/non-identifier name, bad family, short intent.
	for _, tc := range []struct {
		name, family, intent string
	}{
		{"", "transformative", "a perfectly fine intent here"},
		{"  ", "transformative", "a perfectly fine intent here"},
		{"bad name", "transformative", "name has a space so not alnum"}, // space -> not identifier
		{"newop", "not-a-family", "a perfectly fine intent here"},
		{"newop", "transformative", "too short"}, // 2 words
	} {
		if _, ok := r.Mint(tc.name, tc.family, tc.intent); ok {
			t.Fatalf("expected Mint reject for %+v", tc)
		}
	}

	// accepts a valid new op; hyphen/underscore are stripped for the identifier check.
	spec, ok := r.Mint("multi-step_planner", "synthesized", "plan a multi step task")
	if !ok {
		t.Fatal("expected Mint to accept a valid op")
	}
	if !spec.Synthesized {
		t.Fatal("minted spec must have Synthesized=true")
	}
	if spec.Family != "synthesized" || spec.Intent != "plan a multi step task" {
		t.Fatalf("minted spec fields wrong: %+v", spec)
	}
	if !r.Has("multi-step_planner") {
		t.Fatal("minted op should be registered")
	}
	if minted := r.Minted(); len(minted) != 1 || minted[0] != "multi-step_planner" {
		t.Fatalf("minted list wrong: %v", minted)
	}

	// idempotent: re-mint the same name does not duplicate the minted entry.
	if _, ok := r.Mint("multi-step_planner", "synthesized", "plan a multi step task again"); !ok {
		t.Fatal("re-mint should succeed (synthesized op may be redefined)")
	}
	if minted := r.Minted(); len(minted) != 1 {
		t.Fatalf("re-mint must not duplicate minted entry: %v", minted)
	}

	// Names() returns seed order first, the minted name last.
	names := r.Names()
	if len(names) != 35 {
		t.Fatalf("want 34 seed + 1 minted = 35, got %d", len(names))
	}
	if names[0] != "decompose" || names[len(names)-1] != "multi-step_planner" {
		t.Fatalf("Names ordering wrong: first=%q last=%q", names[0], names[len(names)-1])
	}
}

// TestToEnum checks the name->Operator-enum fallback table.
func TestToEnum(t *testing.T) {
	if got := ToEnum("measure"); got.String() != "VALIDATE" {
		t.Fatalf("measure -> %s, want VALIDATE", got)
	}
	if got := ToEnum("hypothesize"); got.String() != "SIMULATE" {
		t.Fatalf("hypothesize -> %s, want SIMULATE", got)
	}
	if got := ToEnum("not-in-table"); got.String() != "GENERATE" {
		t.Fatalf("unknown op -> %s, want GENERATE default", got)
	}
}

// TestGoal checks Goal defaults, GoalSource round-trip, and Short truncation.
func TestGoal(t *testing.T) {
	g := NewGoal("g1", "ship the release")
	if g.Source != GoalUser || g.Status != GoalOpen || g.Parent != nil {
		t.Fatalf("Goal defaults wrong: %+v", g)
	}
	if GoalUser.String() != "USER" || GoalDrive.String() != "DRIVE" || GoalSubgoal.String() != "SUBGOAL" {
		t.Fatal("GoalSource String() wrong")
	}
	if v, ok := ParseGoalSource("DRIVE"); !ok || v != GoalDrive {
		t.Fatalf("ParseGoalSource round-trip failed: %v %v", v, ok)
	}
	if _, ok := ParseGoalSource("NOPE"); ok {
		t.Fatal("ParseGoalSource should reject unknown name")
	}
	long := "this is a goal description that is quite a lot longer than sixty runes for sure yes"
	if s := g2(long).ShortDefault(); len([]rune(s)) != 60 {
		t.Fatalf("ShortDefault should be 60 runes, got %d (%q)", len([]rune(s)), s)
	}
}

func g2(text string) Goal { return NewGoal("x", text) }
