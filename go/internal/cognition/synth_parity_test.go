package cognition

import (
	"reflect"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// capturedEvent is one (kind, summary, data) the synthesiser emitted, recorded in emit order so the
// EXACT strategy/event order can be asserted against the Python golden.
type capturedEvent struct {
	kind    string
	summary string
	data    events.D
}

// captureEmit returns an events.Emit closure that appends every emit into *out (kept in order). It
// matches the events.Emit signature exactly (the same closure the engine wires); it returns a
// zero events.Event because no caller reads the synth emit's return value.
func captureEmit(out *[]capturedEvent) events.Emit {
	return func(kind, summary string, data events.D) events.Event {
		*out = append(*out, capturedEvent{kind: kind, summary: summary, data: data})
		return events.Event{}
	}
}

// heuristicToolmaker builds a TestBackend with its ShapeRecognizer wired to RecognizeShapeDict —
// exactly what the engine does at build (the Go break for Python's lazy `from .synth import
// recognize_shape`). This is the heuristic-backend synth path the Tier-4 gate pins.
func heuristicToolmaker() *backends.TestBackend {
	be := backends.NewTest()
	be.ShapeRecognizer = RecognizeShapeDict
	return be
}

// TestSynthesizeHeuristicShapeParity is the Tier-4 gate (PORT-PLAN §2 row 4): synth.synthesize on the
// HEURISTIC backend returns the same program shape + the same subconscious.synthesize event for the
// comparison / optimisation / design-build goal fixtures, in the EXACT strategy order. The golden
// values are captured by running Python `synth.synthesize(goal, [], OperatorRegistry(),
// TestBackend(), emit)` (cross-checked — see the per-field comments).
//
// Strong (byte/map-exact) parity: the heuristic path makes no RNG draw that reaches the wire, so the
// full program dict + event data compare exactly. NOTE on the internal ROUTE: Python reaches
// source="heuristic" via the LLM-toolmaker step (its synthesize_program returns {program:
// root.to_dict(), source:"heuristic"}); the Go ShapeRecognizer intentionally returns the FULL
// Program.ToDict() (PORT-PLAN §31: `RecognizeShape().ToDict()`), so NodeFromDict can't parse it and
// the Go path falls through to step 2 (RecognizeShape) — both converge on the IDENTICAL observable
// output (source, shape, program dict, events). Observable parity is exact; only the route differs.
func TestSynthesizeHeuristicShapeParity(t *testing.T) {
	type want struct {
		goal      string
		source    string
		shape     string
		rationale string
		root      map[string]any // Program.ToDict()["root"] — the exact Python prog.to_dict() root
	}
	wants := []want{
		{
			goal:      "compare A and B",
			source:    "heuristic",
			shape:     "seq(decompose, par(compare, contrast), rank)",
			rationale: "comparison shape -> parallel compare||contrast then rank",
			root: map[string]any{
				"kind": "seq",
				"children": []any{
					map[string]any{"kind": "step", "operator": "decompose", "domain": "general", "note": "find the dimensions"},
					map[string]any{"kind": "par", "children": []any{
						map[string]any{"kind": "step", "operator": "compare", "domain": "general", "note": "what they share"},
						map[string]any{"kind": "step", "operator": "contrast", "domain": "general", "note": "where they differ"},
					}},
					map[string]any{"kind": "step", "operator": "rank", "domain": "general", "note": "order by the criterion"},
				},
			},
		},
		{
			goal:      "optimize the loop",
			source:    "heuristic",
			shape:     "loop(seq(measure, eliminate))",
			rationale: "optimisation shape -> loop(measure>eliminate)",
			root: map[string]any{
				"kind": "loop", "until": "good enough", "max_iter": 3,
				"body": map[string]any{"kind": "seq", "children": []any{
					map[string]any{"kind": "step", "operator": "measure", "domain": "general", "note": "score the current candidate"},
					map[string]any{"kind": "step", "operator": "eliminate", "domain": "general", "note": "drop the weakest part"},
				}},
			},
		},
		{
			goal:      "design a service",
			source:    "heuristic",
			shape:     "seq(decompose, generate, validate)",
			rationale: "design/build shape -> decompose>generate>validate",
			root: map[string]any{
				"kind": "seq",
				"children": []any{
					map[string]any{"kind": "step", "operator": "decompose", "domain": "planning", "note": "split into parts"},
					map[string]any{"kind": "step", "operator": "generate", "domain": "planning", "note": "draft each part"},
					map[string]any{"kind": "step", "operator": "validate", "domain": "planning", "note": "check it holds together"},
				},
			},
		},
	}

	for _, w := range wants {
		t.Run(w.goal, func(t *testing.T) {
			cat := NewOperatorRegistry()
			be := heuristicToolmaker()
			var emitted []capturedEvent
			res, ok := Synthesize(w.goal, nil, cat, be, captureEmit(&emitted), nil)
			if !ok || res == nil {
				t.Fatalf("Synthesize returned ok=%v res=%v (a recognised shape must yield a program)", ok, res)
			}

			// source + shape + rationale parity.
			if res.Source != w.source {
				t.Errorf("source = %q, want %q", res.Source, w.source)
			}
			if got := res.Program.Shape(); got != w.shape {
				t.Errorf("shape = %q, want %q", got, w.shape)
			}
			if res.Program.Rationale != w.rationale {
				t.Errorf("rationale = %q, want %q", res.Program.Rationale, w.rationale)
			}
			if len(res.Minted) != 0 {
				t.Errorf("minted = %v, want [] (heuristic shapes use only seed operators)", res.Minted)
			}

			// program.to_dict() parity — full root tree, exact Python prog.to_dict().
			gotDict := res.Program.ToDict()
			wantDict := map[string]any{
				"goal": w.goal, "synthesized": true, "rationale": w.rationale, "root": w.root,
			}
			if !programDictEqual(gotDict, wantDict) {
				t.Errorf("program.ToDict() mismatch:\n got  = %#v\n want = %#v", gotDict, wantDict)
			}

			// EXACT strategy/event order: a heuristic shape with no mint emits exactly ONE event,
			// subconscious.synthesize (Python: ['subconscious.synthesize']). No subconscious.operator
			// (nothing minted), no subconscious.skill_match (no library).
			if len(emitted) != 1 {
				t.Fatalf("emitted %d events %v, want exactly 1 (subconscious.synthesize)",
					len(emitted), kindsOf(emitted))
			}
			ev := emitted[0]
			if ev.kind != string(events.SubSynthesize) {
				t.Errorf("event kind = %q, want %q", ev.kind, events.SubSynthesize)
			}
			wantSummary := "synthesised program (heuristic): " + w.shape
			if ev.summary != wantSummary {
				t.Errorf("event summary = %q, want %q", ev.summary, wantSummary)
			}
			// event data: shape/source/rationale/minted plus the full program dict.
			if ev.data["shape"] != w.shape {
				t.Errorf("event data[shape] = %v, want %q", ev.data["shape"], w.shape)
			}
			if ev.data["source"] != w.source {
				t.Errorf("event data[source] = %v, want %q", ev.data["source"], w.source)
			}
			if ev.data["rationale"] != w.rationale {
				t.Errorf("event data[rationale] = %v, want %q", ev.data["rationale"], w.rationale)
			}
			if m, ok := ev.data["minted"].([]string); !ok || len(m) != 0 {
				t.Errorf("event data[minted] = %v, want [] ([]string)", ev.data["minted"])
			}
			if pd, ok := ev.data["program"].(map[string]any); !ok || !programDictEqual(pd, wantDict) {
				t.Errorf("event data[program] != program.ToDict():\n got = %#v", ev.data["program"])
			}
		})
	}
}

// fakeToolmaker is a backend that WRITES a program + an operator to mint — the LLM-toolmaker path
// (strategy step 1). It satisfies backends.Backend by embedding the heuristic backend (so only
// SynthesizeProgram is overridden); the embedded methods are never reached on this path.
type fakeToolmaker struct {
	*backends.TestBackend
	spec map[string]any
}

func (f *fakeToolmaker) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	return f.spec, true
}

// TestSynthesizeLLMToolmakerMintParity pins the OTHER half of the Tier-4 strategy order: when the
// backend WRITES a program that references a brand-new operator, synth mints+verifies it FIRST (emit
// subconscious.operator) THEN parses+verifies the program (emit subconscious.synthesize) — the event
// order ['subconscious.operator', 'subconscious.synthesize'] and the operator-event data pinned from
// the Python golden (FakeToolmaker returning a new 'triagex' operator). Strong (map-exact): the
// fixture is fully deterministic (no RNG, no real I/O).
func TestSynthesizeLLMToolmakerMintParity(t *testing.T) {
	cat := NewOperatorRegistry()
	be := &fakeToolmaker{
		TestBackend: backends.NewTest(),
		spec: map[string]any{
			"operators": []any{
				map[string]any{"name": "triagex", "family": "relational", "intent": "rank the candidate fixes here"},
			},
			"program": map[string]any{"kind": "seq", "children": []any{
				map[string]any{"kind": "step", "operator": "decompose", "domain": "code", "note": "split"},
				map[string]any{"kind": "step", "operator": "triagex", "domain": "code", "note": "rank fixes"},
			}},
			"rationale": "toolmaker-written",
			"source":    "llm",
		},
	}
	var emitted []capturedEvent
	res, ok := Synthesize("fix the bug in the endpoint", nil, cat, be, captureEmit(&emitted), nil)
	if !ok || res == nil {
		t.Fatalf("Synthesize ok=%v res=%v, want a toolmaker-written program", ok, res)
	}

	if res.Source != "llm" {
		t.Errorf("source = %q, want \"llm\"", res.Source)
	}
	if got := res.Program.Shape(); got != "seq(decompose, triagex)" {
		t.Errorf("shape = %q, want \"seq(decompose, triagex)\"", got)
	}
	if !reflect.DeepEqual(mintedNames(res.Minted), []string{"triagex"}) {
		t.Errorf("minted = %v, want [triagex]", mintedNames(res.Minted))
	}
	if !cat.Has("triagex") {
		t.Error("triagex must be minted into the catalog")
	}

	// EXACT strategy order: mint FIRST, then synthesise (Python golden:
	// ['subconscious.operator', 'subconscious.synthesize']).
	gotKinds := kindsOf(emitted)
	wantKinds := []string{string(events.SubOperator), string(events.SubSynthesize)}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("event order = %v, want %v", gotKinds, wantKinds)
	}

	// subconscious.operator data parity (the minted-operator definition).
	op := emitted[0]
	wantOpSummary := "minted operator 'triagex' (relational): rank the candidate fixes here"
	if op.summary != wantOpSummary {
		t.Errorf("operator event summary = %q, want %q", op.summary, wantOpSummary)
	}
	for k, want := range map[string]any{
		"name": "triagex", "family": "relational", "intent": "rank the candidate fixes here", "synthesized": true,
	} {
		if op.data[k] != want {
			t.Errorf("operator event data[%q] = %v, want %v", k, op.data[k], want)
		}
	}

	// the synthesise event carries minted=[triagex] and the llm source.
	syn := emitted[1]
	if syn.data["source"] != "llm" {
		t.Errorf("synthesize event data[source] = %v, want \"llm\"", syn.data["source"])
	}
	if m, ok := syn.data["minted"].([]string); !ok || !reflect.DeepEqual(m, []string{"triagex"}) {
		t.Errorf("synthesize event data[minted] = %v, want [triagex]", syn.data["minted"])
	}
}

// TestSynthesizeNoShapeReturnsNil confirms a simple Q&A goal (no workflow shape) yields (nil, false) —
// the synthesiser defers to the specialists. Mirrors Python `synthesize(...) is None`.
func TestSynthesizeNoShapeReturnsNil(t *testing.T) {
	cat := NewOperatorRegistry()
	be := heuristicToolmaker()
	res, ok := Synthesize("what is 2 + 2", nil, cat, be, nil, nil)
	if ok || res != nil {
		t.Fatalf("a plain Q&A goal must yield (nil,false); got ok=%v res=%v", ok, res)
	}
}

// kindsOf extracts the ordered event-kind list from captured events.
func kindsOf(evs []capturedEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.kind
	}
	return out
}
