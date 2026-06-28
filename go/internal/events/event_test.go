package events

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestAllKindsMatchConstBlock is the Tier-0 wire-vocabulary gate, DERIVED (no magic count number).
// It AST-parses this package's event.go, collects every Kind-typed const value declared in the const
// block, and asserts that set is EXACTLY the set registered in allKinds -- catching a kind that was
// declared-but-not-registered, registered-but-not-declared, dropped, or duplicated. The old gate
// asserted a hand-maintained EXACT COUNT in three files; every parallel "add a kind" bumped the same
// literal, so it was a guaranteed merge conflict. Deriving the check from the const block removes that
// number entirely: two sessions can each add a kind and the edits no longer collide.
func TestAllKindsMatchConstBlock(t *testing.T) {
	// Collect every Kind-typed const value declared in event.go (the source of truth).
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "event.go", nil, 0)
	if err != nil {
		t.Fatalf("parse event.go: %v", err)
	}
	declared := make(map[string]string) // kind value -> const name
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			id, ok := vs.Type.(*ast.Ident) // explicit `Name Kind = "..."`
			if !ok || id.Name != "Kind" {
				continue
			}
			for i, nm := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote const %s: %v", nm.Name, err)
				}
				if prev, dup := declared[val]; dup {
					t.Fatalf("two Kind consts map to the same value %q: %s and %s", val, prev, nm.Name)
				}
				declared[val] = nm.Name
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("parsed zero Kind consts from event.go — the AST gate is mis-wired")
	}

	// Build the registered set from allKinds (the runtime enumeration slice).
	registered := make(map[string]bool, len(allKinds))
	for i, k := range allKinds {
		ks := string(k)
		if ks == "" {
			t.Fatalf("allKinds[%d] is empty", i)
		}
		if registered[ks] {
			t.Fatalf("allKinds contains duplicate kind %q", ks)
		}
		registered[ks] = true
	}

	// The two sets must be IDENTICAL — this is the whole gate (no magic count).
	for val, name := range declared {
		if !registered[val] {
			t.Errorf("Kind const %s (%q) is declared in event.go but MISSING from allKinds — append it there", name, val)
		}
	}
	for val := range registered {
		if _, ok := declared[val]; !ok {
			t.Errorf("allKinds contains %q, which is not a declared Kind const in event.go — declare it or remove it", val)
		}
	}
}

// TestEventJSONLKeyOrderAndShape asserts the marshalled Event matches the Python JSONL line
// format byte-for-byte: the top-level key order is exactly tick, kind, layer, summary, data
// (Python dict insertion order, reproduced by struct field declaration order). This is the
// cross-language conformance seam — the golden writer emits this verbatim.
func TestEventJSONLKeyOrderAndShape(t *testing.T) {
	b := New(16)
	b.Tick = 5
	ev := b.Emit(Filter, "admit (0.90)", D{"verdict": "ADMIT", "confidence": 0.9})

	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Exact top-level key order: tick, kind, layer, summary, data. encoding/json emits struct
	// fields in declaration order and sorts only the keys *inside* the data map.
	want := `{"tick":5,"kind":"seam.filter","layer":"seam","summary":"admit (0.90)","data":{"confidence":0.9,"verdict":"ADMIT"}}`
	if string(out) != want {
		t.Fatalf("JSONL shape mismatch:\n got %s\nwant %s", out, want)
	}

	// Round-trip: the five canonical top-level keys are present and nothing extra. Parse back
	// into an ordered key list to assert the exact key set (order-sensitive at the top level).
	var top map[string]json.RawMessage
	if err := json.Unmarshal(out, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"tick", "kind", "layer", "summary", "data"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("missing required top-level key %q in %s", k, out)
		}
	}
	if len(top) != 5 {
		t.Fatalf("Event must have exactly 5 top-level keys, got %d: %s", len(top), out)
	}
}

// TestEmitLayerDerivation asserts the bus derives `layer` correctly for BOTH a namespaced
// kind (substring before the first dot) AND a bare kind like "tick"/"port" (the whole
// string — there is no dot to split on). Layer derivation lives in Emit, not at call sites.
func TestEmitLayerDerivation(t *testing.T) {
	b := New(16)
	cases := []struct {
		kind      string
		wantLayer string
	}{
		// namespaced kinds -> head before the first "."
		{Filter, "seam"},              // "seam.filter"
		{SubFire, "subconscious"},     // "subconscious.fire"
		{Decision, "critic"},          // "critic.decision"
		{Value, "value"},              // "value.update"
		{Lifecycle, "lifecycle"},      // "lifecycle.transition"
		{ActionSandboxDeny, "action"}, // "action.sandbox_deny"
		{Arousal, "arousal"},          // "arousal.transition"
		// bare kinds (NO dot) -> the whole string IS the layer
		{Tick, "tick"},
		{Port, "port"},
	}
	for _, c := range cases {
		ev := b.Emit(c.kind, "s", nil)
		if ev.Layer != c.wantLayer {
			t.Errorf("Emit(%q): layer = %q, want %q", c.kind, ev.Layer, c.wantLayer)
		}
	}
}
