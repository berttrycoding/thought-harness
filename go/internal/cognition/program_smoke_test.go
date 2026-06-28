package cognition

import (
	"strings"
	"testing"
)

// TestProgramScheduleAndShape checks the linearisation + shape signature against the Python semantics:
// seq(decompose, par(compare,contrast), loop(generate)) -> 1 step phase, 1 parallel phase (2 steps),
// then the loop body unrolled max_iter times with a loop label + iteration index.
func TestProgramScheduleAndShape(t *testing.T) {
	root := NewSeq(
		StepOp("decompose"),
		NewPar(StepOp("compare"), StepOp("contrast")),
		NewLoop(StepOp("generate"), "good enough", 3),
	)
	p := Program{Root: root, Goal: "g"}

	if got, want := p.Shape(), "seq(decompose, par(compare, contrast), loop(generate))"; got != want {
		t.Fatalf("shape: got %q want %q", got, want)
	}

	phases := p.Schedule()
	// 1 (decompose) + 1 (par group) + 3 (loop unrolled) = 5 phases.
	if len(phases) != 5 {
		t.Fatalf("schedule len: got %d want 5", len(phases))
	}
	if phases[0].Parallel || len(phases[0].Steps) != 1 || phases[0].Steps[0].Operator != "decompose" {
		t.Fatalf("phase0 wrong: %+v", phases[0])
	}
	if !phases[1].Parallel || len(phases[1].Steps) != 2 {
		t.Fatalf("phase1 (par) wrong: %+v", phases[1])
	}
	// loop phases carry the loop label + ascending iteration index.
	for i := 0; i < 3; i++ {
		ph := phases[2+i]
		if ph.Loop == nil || *ph.Loop != "loop@2" {
			t.Fatalf("loop phase %d label wrong: %+v", i, ph)
		}
		if ph.Iteration != i || ph.Until != "good enough" {
			t.Fatalf("loop phase %d iteration/until wrong: %+v", i, ph)
		}
	}
}

// TestNodeRoundTrip confirms NodeFromDict(ToDict) reconstructs an equal tree (the golden requirement
// node_from_dict(to_dict(p)) == p), and that a Loop's max_iter survives a JSON-style float64.
func TestNodeRoundTrip(t *testing.T) {
	root := NewSeq(StepOp("decompose"), NewLoop(StepOp("generate"), "refined", 4))
	p := Program{Root: root, Goal: "g", Synthesized: true, Rationale: "why"}

	encoded := NodeToDict(p.Root)
	back, err := NodeFromDict(encoded)
	if err != nil {
		t.Fatalf("NodeFromDict error: %v", err)
	}
	roundTripped := Program{Root: back}
	if roundTripped.Shape() != p.Shape() {
		t.Fatalf("round-trip shape mismatch: %q vs %q", roundTripped.Shape(), p.Shape())
	}

	// simulate a JSON-decoded loop (max_iter arrives as float64).
	jsonLoop := map[string]any{
		"kind": "loop", "until": "x", "max_iter": float64(5),
		"body": map[string]any{"kind": "step", "operator": "generate", "domain": "general", "note": ""},
	}
	n, err := NodeFromDict(jsonLoop)
	if err != nil {
		t.Fatalf("loop decode error: %v", err)
	}
	if l, ok := n.(Loop); !ok || l.MaxIter != 5 {
		t.Fatalf("loop max_iter coercion failed: %+v", n)
	}
}

// TestProgramFromDictRoundTrip confirms the WHOLE-PROGRAM round-trip ProgramFromDict(ToDict(p)) == p
// (goal/synthesized/rationale/root), and pins the W5-2b body save/load bug it closes: the whole-program
// ToDict ENVELOPE ({goal,synthesized,rationale,root}) is NOT a node dict, so NodeFromDict(envelope)
// errors "unknown program node kind: None" — that mismatch made a real minted-skill body unreloadable.
// ProgramFromDict peels the envelope and parses the root, so save (ToDict) and load (ProgramFromDict)
// ROUND-TRIP.
func TestProgramFromDictRoundTrip(t *testing.T) {
	// a real multi-node Program body (the analyze shape the flywheel mints): seq(decompose, hypothesize, measure)
	root := NewSeq(StepOp("decompose"), StepOp("hypothesize"), StepOp("measure"))
	p := Program{Root: root, Goal: "analyze why x", Synthesized: true, Rationale: "analysis shape"}

	envelope := p.ToDict() // what SaveSkill writes (sk.Body.ToDict())

	// the bug: the whole-program envelope is NOT a node dict — NodeFromDict must reject it.
	if _, err := NodeFromDict(envelope); err == nil {
		t.Fatal("NodeFromDict on the whole-program envelope should error (no top-level 'kind'); " +
			"this is the mismatch ProgramFromDict closes")
	}

	// the fix: ProgramFromDict round-trips the whole program.
	back, err := ProgramFromDict(envelope)
	if err != nil {
		t.Fatalf("ProgramFromDict error: %v", err)
	}
	if back.Shape() != p.Shape() {
		t.Fatalf("round-trip shape mismatch: %q vs %q", back.Shape(), p.Shape())
	}
	if back.Goal != p.Goal || !back.Synthesized || back.Rationale != p.Rationale {
		t.Fatalf("round-trip metadata mismatch: %+v vs %+v", back, p)
	}
	if len(back.Steps()) != 3 {
		t.Fatalf("round-trip lost steps: got %d, want 3", len(back.Steps()))
	}

	// a malformed envelope (missing/non-object root) is an error, never a panic.
	if _, err := ProgramFromDict(map[string]any{"goal": "g"}); err == nil {
		t.Fatal("ProgramFromDict with no 'root' should error")
	}
}

// TestNodeFromDictUnknownKind confirms an unknown/malformed node is an error, not a panic.
func TestNodeFromDictUnknownKind(t *testing.T) {
	if _, err := NodeFromDict(map[string]any{"kind": "nope"}); err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if _, err := NodeFromDict(map[string]any{"kind": "step"}); err == nil {
		t.Fatal("expected error for step missing operator")
	}
	if _, err := NodeFromDict(map[string]any{"kind": "loop"}); err == nil {
		t.Fatal("expected error for loop missing body")
	}
}

// TestVerifyProgram exercises the structural bounds against a seeded catalog (real OperatorRegistry).
func TestVerifyProgram(t *testing.T) {
	cat := NewOperatorRegistry()

	// valid program: known ops, bounded loop, 2-branch par.
	good := Program{Root: NewSeq(
		StepOp("decompose"),
		NewPar(StepOp("compare"), StepOp("contrast")),
		NewLoop(StepOp("generate"), "good enough", 3),
	)}
	if ok, issues := VerifyProgram(good, cat); !ok {
		t.Fatalf("expected valid program, issues=%v", issues)
	}

	// empty program -> issue.
	if ok, _ := VerifyProgram(Program{Root: NewSeq()}, cat); ok {
		t.Fatal("expected empty program to fail")
	}

	// unknown operator -> issue.
	if ok, issues := VerifyProgram(Program{Root: StepOp("no-such-op")}, cat); ok || len(issues) == 0 {
		t.Fatalf("expected unknown-operator failure, ok=%v issues=%v", ok, issues)
	}

	// out-of-bounds loop max_iter -> issue.
	if ok, _ := VerifyProgram(Program{Root: NewLoop(StepOp("generate"), "x", MaxIter+1)}, cat); ok {
		t.Fatal("expected loop max_iter overflow to fail")
	}

	// single-branch par -> issue.
	if ok, _ := VerifyProgram(Program{Root: NewPar(StepOp("compare"))}, cat); ok {
		t.Fatal("expected <2-branch par to fail")
	}

	// NESTED-loop unroll blowup -> issue (the runaway-unroll guard). 4 loops of max_iter=6 over one
	// step pass the STATIC bounds (1 step, depth 4 < MaxDepth, each iter <= MaxIter) yet schedule
	// 6^4 = 1296 phases > MaxScheduledPhases (144). It must fail SPECIFICALLY on the phase-count issue.
	nested := Node(StepOp("generate"))
	for i := 0; i < 4; i++ {
		nested = NewLoop(nested, "x", MaxIter)
	}
	ok, issues := VerifyProgram(Program{Root: nested}, cat)
	if ok {
		t.Fatalf("expected nested-loop unroll blowup (6^4=1296 phases) to fail")
	}
	foundPhaseIssue := false
	for _, is := range issues {
		if strings.Contains(is, "schedules too many phases") {
			foundPhaseIssue = true
		}
	}
	if !foundPhaseIssue {
		t.Fatalf("expected a 'schedules too many phases' issue, got %v", issues)
	}

	// Boundary: a 2-nested loop (6*6=36 phases <= 144) is LEGAL — proves the guard is not over-tight.
	twoNested := NewLoop(NewLoop(StepOp("generate"), "x", MaxIter), "y", MaxIter)
	if ok, issues := VerifyProgram(Program{Root: twoNested}, cat); !ok {
		t.Fatalf("expected 2-nested loop (36 phases) to pass, issues=%v", issues)
	}
}

// TestScheduledPhaseCountMatchesSchedule is the DRIFT GUARD: scheduledPhaseCount (the verify-time
// runaway bound) must equal len(Schedule()) (the real linearisation) for representative shapes — so
// the count the guard checks can never silently diverge from what actually gets scheduled.
func TestScheduledPhaseCountMatchesSchedule(t *testing.T) {
	shapes := []Program{
		{Root: StepOp("generate")},
		{Root: NewSeq(StepOp("decompose"), NewPar(StepOp("compare"), StepOp("contrast")), StepOp("rank"))},
		{Root: NewLoop(StepOp("generate"), "x", 3)},
		{Root: NewLoop(NewLoop(StepOp("generate"), "x", MaxIter), "y", MaxIter)}, // 6*6=36
		{Root: NewSeq(StepOp("a"), NewLoop(NewSeq(StepOp("b"), StepOp("c")), "x", 4))},
		{Root: NewPar(NewLoop(StepOp("x"), "u", 5), StepOp("y"))}, // Par collapses to 1 phase
	}
	for i, p := range shapes {
		want := len(p.Schedule())
		got := scheduledPhaseCount(p.Root, 1<<30) // big cap so it never saturates here
		if got != want {
			t.Fatalf("shape %d (%s): scheduledPhaseCount=%d but len(Schedule())=%d — the guard drifted from the linearisation",
				i, p.Shape(), got, want)
		}
	}
}
