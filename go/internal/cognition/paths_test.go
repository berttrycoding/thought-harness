package cognition

import (
	"strings"
	"testing"
)

// paths_test.go is the M5 (representation-space-rebuild.md §1.4 + §5) cognition-package gate for the
// three canonical PATHS (analogy/induction/deduction), the Step.Source annotation, the four workflow
// shapes, and the anti-filler gate (§2.6). It proves the populate is real at the registry/program level;
// the engine-driven proof that each path FIRES is in internal/engine/cognition_property_test.go.

// TestStepSourceFirstClass is the M5 Step.Source gate (mirrors the Role first-class gate): a SOURCE-
// annotated step encodes its source, round-trips through NodeFromDict, and verifies; while a default
// (model) step OMITS the source from its encoding, so seeded/golden programs serialise byte-for-byte
// unchanged.
func TestStepSourceFirstClass(t *testing.T) {
	cat := NewOperatorRegistry()
	prog := Program{
		Root: NewSeq(
			SourceStep("validate", "general", "", SourceReality), // a reality-sourced step
			NewStep("rank", "general", ""),                       // a default model step (no source)
		),
		Synthesized: true,
	}

	// it verifies (a typed source does not break structural verification).
	if ok, issues := VerifyProgram(prog, cat); !ok {
		t.Fatalf("a source-annotated program must verify; issues=%v", issues)
	}

	// it encodes: the sourced step carries source; the default step does NOT (golden-safe omit-default).
	d := prog.Root.toDict()
	children := d["children"].([]any)
	if got := children[0].(map[string]any)["source"]; got != SourceReality {
		t.Fatalf("the sourced step must encode source=%q; got %v", SourceReality, got)
	}
	if _, has := children[1].(map[string]any)["source"]; has {
		t.Fatal("a default (model) step must OMIT source from toDict (so goldens are unchanged)")
	}

	// it round-trips through the decoder.
	back, err := NodeFromDict(d)
	if err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	steps := Program{Root: back}.Steps()
	if len(steps) != 2 || steps[0].Source != SourceReality || steps[1].Source != SourceModel {
		t.Fatalf("source must survive the round-trip; got s0=%q s1=%q", steps[0].Source, steps[1].Source)
	}
}

// TestThreePathsExistAndResolve is the M5 path-registry gate: the three canonical paths are seed skills,
// each expands to a bounded pure-operator program, each verifies, and each carries a GROUNDED terminus
// (the DoD precondition — a path that closed on a model-only DoD is recorded but never minted, §1.4).
func TestThreePathsExistAndResolve(t *testing.T) {
	lib := NewSkillRegistry(true)
	cat := NewOperatorRegistry()

	// the canonical set is exactly three and IsPath agrees.
	if len(PathNames) != 3 {
		t.Fatalf("there must be exactly three canonical paths, got %v", PathNames)
	}
	for _, n := range PathNames {
		if !IsPath(n) {
			t.Fatalf("IsPath(%q) should be true", n)
		}
	}
	if IsPath("diagnose") {
		t.Fatal("IsPath must NOT report a non-path composite as a path")
	}

	want := map[string]struct {
		shapeContains []string // operators the body must walk
		groundedRung  string   // a step Source that grounds the DoD
	}{
		"analogy":   {[]string{"analogize", "generate", "validate"}, SourceReality},
		"induction": {[]string{"generalize", "curate"}, SourceStore},
		"deduction": {[]string{"specialize", "generate", "validate"}, SourceReality},
	}
	for _, name := range PathNames {
		sk, ok := lib.Get(name)
		if !ok {
			t.Fatalf("seed library is missing the %q path", name)
		}
		if sk.Tier != "composite" {
			t.Fatalf("path %q must be a composite skill (so it is matchable), got tier %q", name, sk.Tier)
		}
		prog, err := lib.Expand(sk)
		if err != nil {
			t.Fatalf("path %q must expand to a pure-operator program: %v", name, err)
		}
		if ok, issues := VerifyProgram(prog, cat); !ok {
			t.Fatalf("path %q failed structural verification: %v", name, issues)
		}
		shape := prog.Shape()
		for _, op := range want[name].shapeContains {
			if !strings.Contains(shape, op) {
				t.Errorf("path %q shape %q must walk the %q move", name, shape, op)
			}
		}
		// the path closes on at least one GROUNDED source (the DoD precondition).
		grounded := false
		for _, st := range prog.Steps() {
			if st.Source == want[name].groundedRung {
				grounded = true
			}
		}
		if !grounded {
			t.Errorf("path %q has no %q-sourced step — its DoD cannot be grounded", name, want[name].groundedRung)
		}
	}
}

// TestFourWorkflowShapesResolve confirms the four canonical control-flow shapes (§2.5: sequence,
// parallel-merge, bounded-refine-loop, dispatch-fan-out) each build a program of seed operators that
// passes structural verification — the minimal-real workflow registry's shapes all resolve.
func TestFourWorkflowShapesResolve(t *testing.T) {
	cat := NewOperatorRegistry()
	shapes := map[string]Program{
		"sequence":            {Root: NewSeq(StepOp("decompose"), StepOp("generate"), StepOp("validate")), Synthesized: true},
		"parallel-merge":      {Root: NewSeq(StepOp("decompose"), NewPar(StepOp("compare"), StepOp("contrast")), StepOp("rank")), Synthesized: true},
		"bounded-refine-loop": {Root: NewLoop(NewSeq(StepOp("measure"), StepOp("eliminate")), "good enough", 3), Synthesized: true},
		"dispatch-fan-out":    {Root: NewPar(StepOp("hypothesize"), StepOp("measure"), StepOp("validate")), Synthesized: true},
	}
	for name, prog := range shapes {
		if ok, issues := VerifyProgram(prog, cat); !ok {
			t.Errorf("workflow shape %q must resolve, but failed: %v", name, issues)
		}
	}
}

// TestAntiFillerGateOnSeedRegistry is the M5 anti-filler gate (§2.6): EVERY seed operator that a path or
// seed skill walks must be (1) TRACEABLE — it carries a real Move tag (it is a move on the ladder, not a
// dangling verb); (2) CROSS-LINKED — every skill/path body names only operators the catalog actually
// holds (no skill references a ghost operator); (3) EXERCISED — every operator a path/skill body walks is
// in the live catalog. An entry no path/skill/test reaches is filler. This asserts no dangling references
// and that the walked operators are all real moves.
func TestAntiFillerGateOnSeedRegistry(t *testing.T) {
	lib := NewSkillRegistry(true)
	cat := NewOperatorRegistry()

	walked := map[string]bool{}
	for _, name := range lib.Names() {
		sk, _ := lib.Get(name)
		prog, err := lib.Expand(sk)
		if err != nil {
			t.Fatalf("seed skill %q must expand (no dangling sub-skill): %v", name, err)
		}
		for _, st := range prog.Steps() {
			// (2) CROSS-LINKED: the operator the body names must exist in the catalog (not a ghost).
			spec, ok := cat.Get(st.Operator)
			if !ok {
				t.Errorf("skill %q references operator %q that is not in the catalog (dangling)", name, st.Operator)
				continue
			}
			// (1) TRACEABLE: the walked operator carries a real Move tag (a move on the ladder).
			if spec.Move == "" {
				t.Errorf("operator %q (walked by skill %q) has no Move — it is not traceable to the framework", st.Operator, name)
			}
			walked[st.Operator] = true // (3) EXERCISED
		}
	}
	// the three paths together must exercise the grounded-source moves: analogize/generalize/curate/
	// specialize/generate/validate must all be walked (the populate is real, not filler).
	for _, op := range []string{"analogize", "generalize", "curate", "specialize", "generate", "validate", "abstract", "compare"} {
		if !walked[op] {
			t.Errorf("operator %q is never walked by any seed skill/path — it is unexercised filler", op)
		}
	}
}
