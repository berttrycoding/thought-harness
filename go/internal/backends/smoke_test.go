package backends

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// the TestBackend must satisfy the Backend interface (compile-time pin).
var _ Backend = (*TestBackend)(nil)

// it also satisfies SpecialistCaller (M2): the test-double content stand-in for the model-driven
// skeptic/advocate roles, so fork-on-conflict is exercised deterministically offline.
var _ SpecialistCaller = (*TestBackend)(nil)

// TestTestDoubleIsNotFilterEscalator pins conformance §6.6: the test double must NOT satisfy
// FilterEscalator — admission ESCALATION is a model-only CONTENT-adjacent judgment, so under the
// test double the Filter is always the pure deterministic floor (the model never enters the control
// path through the test package). A compile-time var _ can only assert satisfaction, so this asserts
// the NEGATIVE at runtime.
func TestTestDoubleIsNotFilterEscalator(t *testing.T) {
	var b Backend = NewTest()
	if _, ok := b.(FilterEscalator); ok {
		t.Fatal("the test double must NOT satisfy FilterEscalator (escalation is a model-only role)")
	}
}

func TestAppraiserName(t *testing.T) {
	if got := NewTest().AppraiserName(); got != "test" {
		t.Fatalf("AppraiserName()=%q want test", got)
	}
}

func TestSummarize(t *testing.T) {
	h := NewTest()
	if got := h.Summarize(nil); got != "(empty)" {
		t.Fatalf("empty summarize=%q", got)
	}
	one := []types.Thought{{Text: "alpha"}}
	if got := h.Summarize(one); got != "gist: alpha" {
		t.Fatalf("single summarize=%q", got)
	}
	two := []types.Thought{{Text: "alpha"}, {Text: "omega"}}
	if got := h.Summarize(two); got != "gist[2]: alpha … omega" {
		t.Fatalf("two summarize=%q", got)
	}
}

// TestEmitVerdictDeterministicWellFormed pins the A2 fix #5 contract: the test double's
// EmitVerdict states a well-formed `VERDICT: <first label>` line deterministically (a fixed first-
// option pick, NOT random), so the OFFLINE A2 path exercises the verdict-CONTRACT surface. It also
// pins the edge cases: no labels => no VERDICT line (the present-rate guard sees the disobedience),
// and the priorReasoning is echoed back ahead of the line.
func TestEmitVerdictDeterministicWellFormed(t *testing.T) {
	h := NewTest()

	// A deliberator with options: the line states the FIRST label, deterministically.
	got := h.EmitVerdict("deliberator", "mutex or channel?", []string{"mutex", "channel"}, "I weighed both.")
	want := "I weighed both.\nVERDICT: mutex"
	if got != want {
		t.Fatalf("EmitVerdict=%q want %q", got, want)
	}
	// Two calls are byte-identical (no clock, no RNG).
	if got2 := h.EmitVerdict("deliberator", "mutex or channel?", []string{"mutex", "channel"}, "I weighed both."); got2 != got {
		t.Fatalf("EmitVerdict not deterministic: %q != %q", got2, got)
	}

	// A verifier: the first of the fixed accept|refuse|cannot-verify set the caller passes.
	if got := h.EmitVerdict("verifier", "safe to ship?", []string{"accept", "refuse", "cannot-verify"}, ""); got != "VERDICT: accept" {
		t.Fatalf("verifier EmitVerdict=%q want %q", got, "VERDICT: accept")
	}

	// No labels (a caller/bank error): NO VERDICT line — the guard sees the disobedience rather
	// than a faked line. Just the reasoning prefix survives.
	if got := h.EmitVerdict("deliberator", "x?", nil, "some reasoning"); got != "some reasoning" {
		t.Fatalf("no-labels EmitVerdict should emit no VERDICT line, got %q", got)
	}
}

func TestTransformDeterministic(t *testing.T) {
	h := NewTest()
	c := types.Candidate{Text: "42"}
	// idx = (len(hist=0) + len(raw="42")=2) % 4 = 2 -> "It comes to me: 42."
	if got := h.Transform(c, nil); got != "It comes to me: 42." {
		t.Fatalf("Transform=%q", got)
	}
}

// NOTE: the admission-FLOOR and candidate-RANK math (ScoreAdmit malformed/observation/hedged/
// contradicts/refuted, Rank scores+reasons) moved OUT of the test double into internal/control in
// M3; the floor-math assertions live in internal/control/control_test.go. The test double no longer
// owns ScoreAdmit/Rank (they are gone from the Backend interface), so the smoke tests here cover the
// CONTENT roles the test double still owns. The coverage moved WITH the code; it was not dropped.

func TestRespondSmallTalk(t *testing.T) {
	h := NewTest()
	want := "Hi — I'm a harness that thinks for itself. Give me a question or a task to work on."
	if got := h.Respond("hi", nil); got != want {
		t.Fatalf("smalltalk respond=%q", got)
	}
	if got := h.Respond("how are you doing", nil); got != want {
		t.Fatalf("how-are-you respond=%q", got)
	}
}

func TestRespondHonestWhenNothingConcluded(t *testing.T) {
	h := NewTest()
	// only internal monologue -> nothing to report
	ctx := []types.Thought{{Text: "Working it out from first principles: foo…", Source: types.GENERATED}}
	if got := h.Respond("solve foo", ctx); got != "I couldn't work that out from what I know." {
		t.Fatalf("honest respond=%q", got)
	}
}

func TestRespondReportsConclusion(t *testing.T) {
	h := NewTest()
	ctx := []types.Thought{
		{Text: "the answer to solve foo is 7", Source: types.INJECTED, Confidence: 0.9},
	}
	got := h.Respond("solve foo", ctx)
	if got == "" || got == "I couldn't work that out from what I know." {
		t.Fatalf("expected a real conclusion, got %q", got)
	}
}

func TestOperatorApplyKnownAndUnknown(t *testing.T) {
	h := NewTest()
	ctx := []types.Thought{{Text: "the parser handles nested input", Source: types.GENERATED}}
	if got := h.OperatorApply("decompose", "", "", "code", "build a parser", ctx); got == "" ||
		got[:12] != "[decompose] " {
		t.Fatalf("decompose apply=%q", got)
	}
	// unknown role falls through to the intent template, still tagged
	got := h.OperatorApply("frobnicate", "", "do the thing", "code", "goal", ctx)
	if got[:13] != "[frobnicate] " {
		t.Fatalf("unknown-role apply=%q", got)
	}
}

func TestGenerateDeterministicWithSeed(t *testing.T) {
	h := NewTest()
	rng := cpyrand.New(7)
	a := h.Generate("solve x", nil, rng)
	rng2 := cpyrand.New(7)
	b := h.Generate("solve x", nil, rng2)
	if a != b {
		t.Fatalf("same seed must reproduce: %q vs %q", a, b)
	}
	if a == "" {
		t.Fatalf("empty generation")
	}
}

func TestSynthesizeProgramDefersWithoutRecognizer(t *testing.T) {
	h := NewTest()
	if d, ok := h.SynthesizeProgram("goal", nil, nil); d != nil || ok {
		t.Fatalf("nil recogniser must defer (nil,false), got (%v,%v)", d, ok)
	}
}

func TestSynthesizeProgramDelegates(t *testing.T) {
	called := false
	h := &TestBackend{ShapeRecognizer: func(goal string, ctx []types.Thought) (map[string]any, bool) {
		called = true
		return map[string]any{"program": map[string]any{}, "source": "heuristic"}, true
	}}
	d, ok := h.SynthesizeProgram("compare A and B", nil, nil)
	if !called || !ok || d["source"] != "heuristic" {
		t.Fatalf("delegation failed: called=%v ok=%v d=%v", called, ok, d)
	}
}
