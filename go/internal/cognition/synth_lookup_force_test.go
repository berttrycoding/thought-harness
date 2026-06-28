package cognition

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// synth_lookup_force_test.go is the SUBSTRATE-INDEPENDENT proof of the web_search under-staffing fix
// (step 3 of synthesize, the LOOKUP-FORCE). It is the test that SIMULATES THE LIVE FAILURE the offline
// staffing test could not: the live MODEL (SynthesizeProgram) returns a VALID program for a factual
// lookup question that simply OMITS expose-affordances. On that path the step-2 heuristic shape
// (RecognizeShapeWeb) never runs (step 1 already produced a valid program), so before the force the
// lookup operator was never staffed and web_search never fired (~1/N on HotpotQA-fullwiki). These tests
// use a fakeToolmaker whose program OMITS expose-affordances — exactly the live model behaviour — and
// prove the force re-injects it REGARDLESS of synthesis source, never relying on the deterministic
// RecognizeShape path that the existing TestRecognizeShapeWeb* tests already cover.

// omitExposeToolmaker is a backend whose SynthesizeProgram returns a VALID program that does NOT staff
// expose-affordances — the live-model stand-in. It returns whatever spec it is given, ok=true.
type omitExposeToolmaker struct {
	*backends.TestBackend
	spec map[string]any
}

func (f *omitExposeToolmaker) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	return f.spec, true
}

// decomposeGenerateSpec is a valid seq(decompose, generate) program — a plausible thing the live model
// writes for a lookup question, and one that LACKS expose-affordances (so web_search would never fire).
func decomposeGenerateSpec() map[string]any {
	return map[string]any{
		"program": map[string]any{"kind": "seq", "children": []any{
			map[string]any{"kind": "step", "operator": "decompose", "domain": "general", "note": "break the question down"},
			map[string]any{"kind": "step", "operator": "generate", "domain": "general", "note": "answer it"},
		}},
		"rationale": "model wrote decompose>generate",
		"source":    "llm",
	}
}

// TestWebSearchForcesExposeAffordancesEvenWhenSynthOmitsIt is the live-failure simulation: with
// webLookup ON, a factual lookup question whose synthesised (model-written) program OMITS
// expose-affordances STILL ends up staffing expose-affordances — the deterministic force prepends it.
// The force is keyed off the lookup goal + the resolved program, NOT off RecognizeShapeWeb, so it fires
// on the LIVE model path the offline RecognizeShapeWebDict toolmaker masked.
func TestWebSearchForcesExposeAffordancesEvenWhenSynthOmitsIt(t *testing.T) {
	cat := NewOperatorRegistry()
	be := &omitExposeToolmaker{TestBackend: backends.NewTest(), spec: decomposeGenerateSpec()}

	const lookup = "Were Scott Derrickson and Ed Wood the same nationality?"
	res, ok := SynthesizeWeb(lookup, nil, cat, be, nil, nil, true)
	if !ok || res == nil {
		t.Fatalf("SynthesizeWeb ok=%v res=%v, want a program", ok, res)
	}

	// the model omitted expose-affordances, but the force must have injected it.
	if !programStaffsExposeAffordances(res.Program) {
		t.Fatalf("lookup question + web_search ON: the synth OMITTED expose-affordances and the force did NOT inject it (shape=%q) — web_search would never fire on the live path", res.Program.Shape())
	}
	// the force PREPENDS so the research happens before the model's own steps; first operator is expose-affordances.
	if first := firstOperator(&res.Program); first != "expose-affordances" {
		t.Fatalf("forced program first operator = %q, want expose-affordances (the research must run first)", first)
	}
	// the model's own steps are preserved after the forced research step.
	if !programHasOperator(res.Program, "decompose") || !programHasOperator(res.Program, "generate") {
		t.Fatalf("forced program must KEEP the model's own steps (decompose, generate); shape=%q", res.Program.Shape())
	}
}

// TestWebSearchForceOffIsByteIdentical is the byte-identical-OFF arm of the force: with webLookup OFF,
// the SAME lookup question + the SAME model program that omits expose-affordances is left EXACTLY as the
// model wrote it (no force, no expose-affordances) — the default path is unchanged.
func TestWebSearchForceOffIsByteIdentical(t *testing.T) {
	cat := NewOperatorRegistry()
	be := &omitExposeToolmaker{TestBackend: backends.NewTest(), spec: decomposeGenerateSpec()}

	const lookup = "Were Scott Derrickson and Ed Wood the same nationality?"
	res, ok := SynthesizeWeb(lookup, nil, cat, be, nil, nil, false) // webLookup OFF (default)
	if !ok || res == nil {
		t.Fatalf("SynthesizeWeb(OFF) ok=%v res=%v, want the model program", ok, res)
	}
	if programStaffsExposeAffordances(res.Program) {
		t.Fatalf("web_search OFF: the force must NOT fire — expose-affordances was injected (shape=%q)", res.Program.Shape())
	}
	if got := res.Program.Shape(); got != "seq(decompose, generate)" {
		t.Fatalf("web_search OFF: program must be byte-identical to the model's; shape=%q, want seq(decompose, generate)", got)
	}
}

// TestWebSearchForceDoesNotDoubleStaff is the idempotence guard: when the model ALREADY staffed
// expose-affordances, the force does NOT prepend a second one (it would double the research + waste a
// dispatch). The program is left as-is.
func TestWebSearchForceDoesNotDoubleStaff(t *testing.T) {
	cat := NewOperatorRegistry()
	be := &omitExposeToolmaker{TestBackend: backends.NewTest(), spec: map[string]any{
		"program": map[string]any{"kind": "seq", "children": []any{
			map[string]any{"kind": "step", "operator": "expose-affordances", "domain": "general", "note": "research"},
			map[string]any{"kind": "step", "operator": "generate", "domain": "general", "note": "answer"},
		}},
		"rationale": "model included expose-affordances itself",
		"source":    "llm",
	}}

	const lookup = "What year did Marie Curie win her first Nobel Prize?"
	res, ok := SynthesizeWeb(lookup, nil, cat, be, nil, nil, true)
	if !ok || res == nil {
		t.Fatalf("SynthesizeWeb ok=%v res=%v", ok, res)
	}
	if n := countOperator(res.Program, "expose-affordances"); n != 1 {
		t.Fatalf("force must be idempotent: expose-affordances appears %d times, want exactly 1 (shape=%q)", n, res.Program.Shape())
	}
}

// TestWebSearchForceDoesNotFireOnNonLookup is the calibration guard at the synthesise level: a NON-lookup
// goal (a statement) whose model program omits expose-affordances must NOT acquire it via the force —
// the GAIA-L1 over-grounding regime is not widened. (A statement hits no shape via RecognizeShapeWeb and
// is not isLookupQuestion, so the force is gated off.)
func TestWebSearchForceDoesNotFireOnNonLookup(t *testing.T) {
	cat := NewOperatorRegistry()
	be := &omitExposeToolmaker{TestBackend: backends.NewTest(), spec: decomposeGenerateSpec()}

	const statement = "The capital of France is Paris and it is a city."
	res, ok := SynthesizeWeb(statement, nil, cat, be, nil, nil, true)
	if !ok || res == nil {
		t.Fatalf("SynthesizeWeb ok=%v res=%v", ok, res)
	}
	if programStaffsExposeAffordances(res.Program) {
		t.Fatalf("a STATEMENT (not a lookup question) must NOT get the forced expose-affordances (over-grounding); shape=%q", res.Program.Shape())
	}
}

// programHasOperator reports whether any step in p uses operator op.
func programHasOperator(p Program, op string) bool { return countOperator(p, op) > 0 }

// countOperator counts how many steps in p use operator op (for the idempotence assertion).
func countOperator(p Program, op string) int {
	n := 0
	for _, s := range p.Steps() {
		if s.Operator == op {
			n++
		}
	}
	return n
}
