package cognition

import (
	"strings"
	"testing"
)

// TestSkillExpandsItsSubSkills ports test_skills.py::test_higher_level_skill_expands_its_sub_skills —
// a composite skill ('diagnose') that calls a sub-skill ('decompose-task') resolves at BUILD time into
// a bounded, pure-operator Program. After Expand there are NO skill: calls left, the sub-skill's
// operators appear in the shape, and the expanded program passes structural verification. This is the
// Tier-3 gate (PORT-PLAN §3.3): skills.Expand produces the same bounded pure-operator Program.
func TestSkillExpandsItsSubSkills(t *testing.T) {
	lib := NewSkillRegistry(true)
	cat := NewOperatorRegistry()

	diagnose, ok := lib.Get("diagnose")
	if !ok {
		t.Fatal("seed library is missing the 'diagnose' composite skill")
	}
	// it calls a sub-skill (mirrors `assert "decompose-task" in diagnose.sub_skills()`)
	if !containsString(diagnose.SubSkills(), "decompose-task") {
		t.Fatalf("diagnose.SubSkills() = %v, want it to contain decompose-task", diagnose.SubSkills())
	}

	prog, err := lib.Expand(diagnose)
	if err != nil {
		t.Fatalf("expanding diagnose must not error: %v", err)
	}

	// resolved to pure operators: 'decompose' (from the decompose-task sub-skill) + 'hypothesize'
	// (from diagnose's own body) both appear in the shape (mirrors the Python shape asserts).
	shape := prog.Shape()
	if !strings.Contains(shape, "decompose") || !strings.Contains(shape, "hypothesize") {
		t.Fatalf("expanded shape %q must contain both decompose and hypothesize", shape)
	}

	// the expanded artifact is a PURE operator program — every Step is a real operator, none is a
	// residual skill: call (the build-time resolution obligation).
	for _, s := range prog.Steps() {
		if IsSkillCall(s) {
			t.Fatalf("expanded program still carries a sub-skill call: %q", s.Operator)
		}
	}

	// the expanded program is structurally valid (mirrors `verify_program(prog, cat)[0]`).
	if ok, issues := VerifyProgram(prog, cat); !ok {
		t.Fatalf("expanded program failed verification: %v", issues)
	}

	// it is marked synthesized + rationale'd to the source skill (Expand's contract).
	if !prog.Synthesized {
		t.Fatal("expanded program must be marked Synthesized=true")
	}
	if prog.Rationale != "skill 'diagnose'" {
		t.Fatalf("expanded program rationale = %q, want \"skill 'diagnose'\"", prog.Rationale)
	}
}

// TestSubSkillCycleIsRejected ports test_skills.py::test_sub_skill_cycle_is_rejected — a
// self-referential skill must NOT expand (Expand returns an error whose message names the cycle), and
// a mint that would introduce a cycle never enters the library. This is the Tier-3 gate's cycle half.
func TestSubSkillCycleIsRejected(t *testing.T) {
	lib := NewSkillRegistry(true)

	// loopy calls itself (skill:loopy inside loopy's own body) — inject it directly into the registry,
	// mirroring the Python test's `lib._skills["loopy"] = loopy`.
	loopy := Skill{
		Name:        "loopy",
		Tier:        "composite",
		Triggers:    []string{"x"},
		Body:        seedProgramSynth(NewSeq(SkillStep("loopy", "general", ""))),
		Synthesized: true,
	}
	lib.skills["loopy"] = loopy

	_, err := lib.Expand(loopy)
	if err == nil {
		t.Fatal("a self-referential skill must not expand")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expand error must name the cycle, got: %v", err)
	}

	// and a cyclic mint never enters the library (mirrors the `lib.mint(...) is None` assert).
	_, ok := lib.Mint("loopy2", []string{"y"},
		seedProgramSynth(NewSeq(SkillStep("loopy2", "general", ""))), "composite", "")
	if ok {
		t.Fatal("a cyclic mint must be rejected (return ok=false), but it succeeded")
	}
	if lib.Has("loopy2") {
		t.Fatal("a rejected cyclic mint must not enter the library")
	}
}

// TestSubSkillDepthBoundIsEnforced asserts the MaxSkillDepth=3 bound explicitly (the gate row: "assert
// the depth bound"). The Python test suite covers the cycle path; the depth bound is the OTHER
// durability obligation, so we build an acyclic chain a -> b -> c -> d that nests one level past
// MaxSkillDepth and assert Expand rejects it with the nesting message, while a chain exactly at the
// bound expands cleanly. Structural fixture (built here, not in a Python file) — noted per the gate.
func TestSubSkillDepthBoundIsEnforced(t *testing.T) {
	// A linear, ACYCLIC chain of composite skills, each calling the next: depthCall("s0") -> s1 -> ...
	// Each call consumes one MaxSkillDepth level. With MaxSkillDepth=3, a chain whose ROOT calls a
	// sub-skill that itself nests 3 sub-skill calls deep trips `depth+1 > MaxSkillDepth`.
	build := func(n int) *SkillRegistry {
		lib := NewSkillRegistry(true)
		for i := 0; i < n; i++ {
			name := "depth" + itoa(i)
			var body Program
			if i < n-1 {
				// composite: calls the next link in the chain
				body = seedProgramSynth(NewSeq(SkillStep("depth"+itoa(i+1), "general", "")))
			} else {
				// the tail is a pure leaf operator (a real catalog operator: "measure")
				body = seedProgramSynth(NewSeq(NewStep("measure", "general", "")))
			}
			lib.skills[name] = Skill{
				Name: name, Tier: "composite", Triggers: []string{name},
				Body: body, Synthesized: true,
			}
		}
		return lib
	}

	// Chain length 4: depth0 -> depth1 -> depth2 -> depth3(leaf). Expanding depth0 makes 3 sub-skill
	// hops (depth0->1 at depth1, ->2 at depth2, ->3 at depth3), each `depth+1 > 3` check passes
	// (1,2,3 are all <= 3). This is exactly AT the bound and must expand.
	atBound := build(4)
	root, _ := atBound.Get("depth0")
	if _, err := atBound.Expand(root); err != nil {
		t.Fatalf("a chain exactly at MaxSkillDepth=%d must expand: %v", MaxSkillDepth, err)
	}

	// Chain length 5: depth0 -> ... -> depth4(leaf). The 4th sub-skill hop is `depth+1 = 4 > 3` and
	// must be rejected with the nesting message.
	overBound := build(5)
	root2, _ := overBound.Get("depth0")
	_, err := overBound.Expand(root2)
	if err == nil {
		t.Fatalf("a chain one level past MaxSkillDepth=%d must NOT expand", MaxSkillDepth)
	}
	if !strings.Contains(err.Error(), "nesting") || !strings.Contains(err.Error(), "MaxSkillDepth") {
		t.Fatalf("over-deep expand error must name the depth bound, got: %v", err)
	}
}

// seedProgramSynth builds a synthesized Program from a root — the test fixtures mirror the Python
// `Program(seq(...), synthesized=True)` (seedProgram in skills.go builds a NON-synthesized seed body,
// so this is the synthesized counterpart the cycle/depth fixtures need).
func seedProgramSynth(root Node) Program {
	return Program{Root: root, Synthesized: true}
}
