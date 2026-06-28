package cognition

import "testing"

// synth_weblookup_test.go pins the LOOKUP-RESEARCH shape (the subconscious.web_search under-staffing fix):
// RecognizeShapeWeb(goal, ctx, true) produces a program that STAFFS expose-affordances for a factual-lookup
// QUESTION that hit no other shape — and RecognizeShape / webLookup=false NEVER do (byte-identical).

const lookupRationale = "lookup shape -> expose-affordances(web_search research)>generate(grounded answer)"

// staffsExposeAffordances reports whether p is the lookup-research program (a Seq whose first step staffs
// expose-affordances), keyed off the rationale the lookup branch stamps.
func isLookupProgram(p *Program, ok bool) bool {
	return ok && p != nil && p.Rationale == lookupRationale
}

// TestRecognizeShapeWebStaffsExposeForLookupQuestions: a factual-lookup question that matches no specific
// shape gets the lookup-research program; the staffed first operator is expose-affordances (the operator
// the engine grants web_search), so a downstream sub-agent can dispatch web_search.
func TestRecognizeShapeWebStaffsExposeForLookupQuestions(t *testing.T) {
	lookups := []string{
		"Were Scott Derrickson and Ed Wood the same nationality?",
		"What year did Marie Curie win her first Nobel Prize?",
		"Who wrote the novel the 1994 film was based on?",
		"Which city hosted the 2008 Summer Olympics?",
		"How many moons does Jupiter have?",
		"List the planets in the solar system",
		"Name the author of Pride and Prejudice",
	}
	for _, goal := range lookups {
		p, ok := RecognizeShapeWeb(goal, nil, true)
		if !isLookupProgram(p, ok) {
			t.Errorf("RecognizeShapeWeb(%q, true): want the lookup-research shape, got ok=%v rationale=%q", goal, ok, rationaleOf(p))
			continue
		}
		// The lookup program must STAFF expose-affordances as its research step (the web_search holder).
		if first := firstOperator(p); first != "expose-affordances" {
			t.Errorf("RecognizeShapeWeb(%q): lookup program first operator = %q, want expose-affordances", goal, first)
		}
	}
}

// TestRecognizeShapeWebDoesNotOverStaff is the calibration guard: goals that are NOT external-fact lookup
// questions must NOT get the lookup shape, even with webLookup=true — a statement, a local-file task, and a
// goal that already hit a specific shape (compare/optimize/analyze/design). This keeps expose-affordances
// from being staffed indiscriminately (the GAIA-L1 over-grounding risk).
func TestRecognizeShapeWebDoesNotOverStaff(t *testing.T) {
	notLookups := []string{
		"The capital of France is Paris.",                // a statement, no question
		"read the value assigned to Margin in config.go", // names a local file
		"compare the two designs and pick one",           // comparison shape (its own program)
		"optimize the throughput of the parser",          // optimisation shape (its own program)
		"investigate why the build is failing",           // analysis shape (its own program)
		"design a small REST api for users",              // design/build shape (its own program)
		"refactor this messy function",                   // not a question, no lookup lead
	}
	for _, goal := range notLookups {
		if p, ok := RecognizeShapeWeb(goal, nil, true); isLookupProgram(p, ok) {
			t.Errorf("RecognizeShapeWeb(%q, true) produced the lookup shape — over-staffing the web", goal)
		}
	}
}

// TestRecognizeShapeWebOffIsByteIdentical: webLookup=false (and the legacy RecognizeShape) NEVER produce the
// lookup-research shape — the default-OFF byte-identical contract. A lookup question that hits no other
// shape returns (nil, false) exactly as before the flag.
func TestRecognizeShapeWebOffIsByteIdentical(t *testing.T) {
	lookups := []string{
		"Were Scott Derrickson and Ed Wood the same nationality?",
		"What year did Marie Curie win her first Nobel Prize?",
		"How many moons does Jupiter have?",
	}
	for _, goal := range lookups {
		if p, ok := RecognizeShapeWeb(goal, nil, false); isLookupProgram(p, ok) || ok {
			t.Errorf("RecognizeShapeWeb(%q, false): want (nil,false), got ok=%v rationale=%q", goal, ok, rationaleOf(p))
		}
		if p, ok := RecognizeShape(goal, nil); ok {
			t.Errorf("RecognizeShape(%q): want (nil,false) (no lookup shape on the legacy entry), got rationale=%q", goal, rationaleOf(p))
		}
	}
}

// firstOperator returns the operator name of the first step of a Seq-rooted program, or "" — used to assert
// the lookup program staffs expose-affordances first.
func firstOperator(p *Program) string {
	if p == nil {
		return ""
	}
	seq, ok := p.Root.(Seq)
	if !ok || len(seq.Children) == 0 {
		return ""
	}
	if st, ok := seq.Children[0].(Step); ok {
		return st.Operator
	}
	return ""
}

func rationaleOf(p *Program) string {
	if p == nil {
		return "<nil>"
	}
	return p.Rationale
}
