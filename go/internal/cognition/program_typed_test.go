package cognition

import "testing"

// TestScriptNodeFirstClass is the P3.4 (script half) gate: a SCRIPT (deterministic) step is first-class
// in the live Program — it encodes its role, round-trips through NodeFromDict, and verifies; while a
// default SKILL step OMITS the role from its encoding, so seeded/golden programs serialise byte-for-byte
// unchanged.
func TestScriptNodeFirstClass(t *testing.T) {
	cat := NewOperatorRegistry()
	prog := Program{
		Root: NewSeq(
			Step{Operator: "measure", Domain: "general", Role: RoleScript}, // a deterministic script node
			NewStep("rank", "general", ""),                                 // a default agentic skill node
		),
		Synthesized: true,
	}

	// it verifies (a typed role does not break structural verification).
	if ok, issues := VerifyProgram(prog, cat); !ok {
		t.Fatalf("a typed (script) program must verify; issues=%v", issues)
	}

	// it encodes: the script step carries role; the skill step does NOT (golden-safe omit-default).
	d := prog.Root.toDict()
	children := d["children"].([]any)
	if got := children[0].(map[string]any)["role"]; got != RoleScript {
		t.Fatalf("the script step must encode role=%q; got %v", RoleScript, got)
	}
	if _, has := children[1].(map[string]any)["role"]; has {
		t.Fatal("a default skill step must OMIT role from toDict (so goldens are unchanged)")
	}

	// it round-trips through the decoder.
	back, err := NodeFromDict(d)
	if err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	steps := Program{Root: back}.Steps()
	if len(steps) != 2 || !steps[0].IsScript() || steps[1].IsScript() {
		t.Fatalf("script/skill roles must survive the round-trip; got script0=%v script1=%v",
			steps[0].IsScript(), steps[1].IsScript())
	}
}
