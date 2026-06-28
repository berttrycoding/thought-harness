package cognition

import (
	"reflect"
	"testing"
)

// fixtureProgram is the fixed tree the Python golden was captured from (running program.py +
// synth.node_from_dict):
//
//	seq( step(decompose, math, "split it"),
//	     par( step(compare), step(contrast) ),
//	     loop( step(rank), until="good enough", max_iter=3 ) )
//	Program(goal="rank the options", synthesized=True, rationale="test fixture")
func fixtureProgram() Program {
	root := NewSeq(
		NewStep("decompose", "math", "split it"),
		NewPar(StepOp("compare"), StepOp("contrast")),
		NewLoop(StepOp("rank"), "good enough", 3),
	)
	return Program{Root: root, Goal: "rank the options", Synthesized: true, Rationale: "test fixture"}
}

// TestProgramToDictParity pins Program.ToDict against the exact Python program.py to_dict() output
// (PORT-PLAN Tier-2 gate: program serialisation). The golden map below is byte-for-byte the JSON
// thought_harness/program.py emits for the fixture (string keys, "kind" discriminator, defaults
// "general"/"" filled in, max_iter as a number).
func TestProgramToDictParity(t *testing.T) {
	got := fixtureProgram().ToDict()
	want := map[string]any{
		"goal":        "rank the options",
		"synthesized": true,
		"rationale":   "test fixture",
		"root": map[string]any{
			"kind": "seq",
			"children": []any{
				map[string]any{"kind": "step", "operator": "decompose", "domain": "math", "note": "split it"},
				map[string]any{
					"kind": "par",
					"children": []any{
						map[string]any{"kind": "step", "operator": "compare", "domain": "general", "note": ""},
						map[string]any{"kind": "step", "operator": "contrast", "domain": "general", "note": ""},
					},
				},
				map[string]any{
					"kind": "loop", "until": "good enough", "max_iter": 3,
					"body": map[string]any{"kind": "step", "operator": "rank", "domain": "general", "note": ""},
				},
			},
		},
	}
	if !programDictEqual(got, want) {
		t.Errorf("ToDict mismatch:\n got  = %#v\n want = %#v", got, want)
	}
}

// TestProgramNodeRoundTripParity is the golden requirement node_from_dict(to_dict(p)) == p for the
// fixed tree — a FULL structural equality (operator, domain, note, until, max_iter at every node),
// not just the one-line shape. The Python reference (synth.node_from_dict over program.to_dict)
// returns a node that `== p.root` (dataclass equality); the Go round-trip must reconstruct the
// identical Node tree.
func TestProgramNodeRoundTripParity(t *testing.T) {
	p := fixtureProgram()
	encoded := p.ToDict()
	rootDict, _ := encoded["root"].(map[string]any)
	back, err := NodeFromDict(rootDict)
	if err != nil {
		t.Fatalf("NodeFromDict error: %v", err)
	}
	// reflect.DeepEqual over the closed Node tree IS the Go equivalent of Python's dataclass
	// `node_from_dict(to_dict(p).root) == p.root`. Every Step's domain/note + the Loop's
	// until/max_iter must survive the round-trip.
	if !reflect.DeepEqual(back, p.Root) {
		t.Errorf("round-trip node != original:\n got  = %#v\n want = %#v", back, p.Root)
	}
}

// TestProgramScheduleParity pins Program.Schedule() to the exact PhasePlan list Python's
// program.schedule() produces for the fixture: one serial step, one parallel pair, then the loop
// body unrolled 3x with the label "loop@2" (its position in the output at loop entry) and ascending
// iteration index. Captured by RUNNING program.py.
func TestProgramScheduleParity(t *testing.T) {
	plans := fixtureProgram().Schedule()

	type want struct {
		ops       []string
		parallel  bool
		loop      string // "" == nil/None
		iteration int
		until     string
	}
	wants := []want{
		{ops: []string{"decompose"}, parallel: false, loop: "", iteration: 0, until: ""},
		{ops: []string{"compare", "contrast"}, parallel: true, loop: "", iteration: 0, until: ""},
		{ops: []string{"rank"}, parallel: false, loop: "loop@2", iteration: 0, until: "good enough"},
		{ops: []string{"rank"}, parallel: false, loop: "loop@2", iteration: 1, until: "good enough"},
		{ops: []string{"rank"}, parallel: false, loop: "loop@2", iteration: 2, until: "good enough"},
	}
	if len(plans) != len(wants) {
		t.Fatalf("schedule len=%d want %d", len(plans), len(wants))
	}
	for i, w := range wants {
		p := plans[i]
		gotOps := make([]string, len(p.Steps))
		for j, s := range p.Steps {
			gotOps[j] = s.Operator
		}
		if !reflect.DeepEqual(gotOps, w.ops) {
			t.Errorf("phase[%d] ops=%v want %v", i, gotOps, w.ops)
		}
		if p.Parallel != w.parallel {
			t.Errorf("phase[%d] parallel=%v want %v", i, p.Parallel, w.parallel)
		}
		gotLoop := ""
		if p.Loop != nil {
			gotLoop = *p.Loop
		}
		if gotLoop != w.loop {
			t.Errorf("phase[%d] loop=%q want %q", i, gotLoop, w.loop)
		}
		if p.Iteration != w.iteration {
			t.Errorf("phase[%d] iteration=%d want %d", i, p.Iteration, w.iteration)
		}
		if p.Until != w.until {
			t.Errorf("phase[%d] until=%q want %q", i, p.Until, w.until)
		}
	}
	// shape signature parity (Python p.shape()).
	if got := fixtureProgram().Shape(); got != "seq(decompose, par(compare, contrast), loop(rank))" {
		t.Errorf("shape=%q", got)
	}
}

// programDictEqual deep-compares the serialised program maps with numeric coercion (max_iter may be
// an int literal in the golden vs the Go int the encoder writes; JSON would render both as a bare
// number).
func programDictEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !programValEqual(av, bv) {
			return false
		}
	}
	return true
}

func programValEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && programDictEqual(av, bv)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !programValEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		if an, aok := progFloat(a); aok {
			if bn, bok := progFloat(b); bok {
				return an == bn
			}
			return false
		}
		return a == b
	}
}

func progFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
