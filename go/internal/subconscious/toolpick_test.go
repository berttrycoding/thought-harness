package subconscious

import (
	"sort"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// liveRoster + liveRegistry build the EXACT same faculty roster + tool registry the engine wires, so the
// picker's resolved sets are the real ones (not a fixture). The roster's tool-backed primitives (read/
// search/run) carry the worker-faculty footprints; the registry is the 5 builtins (read/write/run_shell/
// run_tests/search) with their OWN Category() tags. A nil provider keeps the primitives dark (we only read
// their footprints here, never fire them).
func liveRoster() []PrimitiveSubAgent {
	// the full real roster with no live ports — only the tool footprints are read by NewToolPicker.
	return DefaultPrimitiveSubAgents(nil, nil, nil, nil, nil, nil, false)
}

func liveRegistry() *action.ToolRegistry {
	return action.NewToolRegistry(action.DefaultTools(".", 5*time.Second))
}

// TestToolPickerParityOnSeedOps is THE parity gate (the hard invariant): the category-sourced resolver must
// resolve EXACTLY the flat OperatorSpec.ToolScope for every seed operator — so flipping the capability flag
// on preserves behaviour byte-for-byte. It enumerates the WHOLE seed catalog (not a sample) and asserts the
// resolved set equals the flat ToolScope, element-for-element, for each op (tool-bearing AND reason-only).
func TestToolPickerParityOnSeedOps(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	picker := NewToolPicker(liveRoster(), liveRegistry(), false)
	if picker == nil {
		t.Fatal("NewToolPicker returned nil for a real registry (the resolver must be built)")
	}
	// A permissive run ceiling that admits every operation category, so the ceiling never masks a parity gap
	// (parity is about the SOURCE matching, tested independent of the ceiling's least-privilege bite).
	scope := NewScope("", []string{"inspect", "execute", "mutate"}, 0)

	for _, name := range cat.Names() {
		spec, ok := cat.Get(name)
		if !ok {
			t.Fatalf("seed op %q missing from the catalog", name)
		}
		want := append([]string(nil), spec.ToolScope...)
		sort.Strings(want)
		got := picker.Resolve(spec, scope)
		if !sameSet(got, want) {
			t.Errorf("op %q: category-sourced pick %v != flat ToolScope %v (PARITY BREAK — flipping the flag "+
				"would change this worker's tools)", name, got, want)
		}
	}
}

// TestToolPickerSourcedNotFromFlatList proves the resolver genuinely SOURCES from the category footprint +
// the live registry — not by reading spec.ToolScope. It builds a spec whose flat ToolScope is GARBAGE (a
// tool name no faculty/registry knows) but whose category footprint is execute: the resolver must IGNORE the
// garbage name and resolve the registry-confirmed execute faculty tool (run_tests), proving the flat list is
// not the source. (This is the mutation-sensitive heart of the gap-5 wire.)
func TestToolPickerSourcedNotFromFlatList(t *testing.T) {
	picker := NewToolPicker(liveRoster(), liveRegistry(), false)
	// ScopeCategories derives the footprint from the categories of the flat list; a run-class tool makes the
	// footprint {execute}. But the flat NAME ("phantom_runner") is junk the registry does not hold — if the
	// resolver sourced from the flat list it would carry "phantom_runner"; sourcing by category it carries
	// run_tests (the execute-category worker-faculty tool the live registry confirms).
	spec := cognition.OperatorSpec{Name: "probe", Family: "relational",
		Intent: "run something", ToolScope: []string{"run_shell"}} // run_shell ⇒ ScopeCategories {execute}
	scope := NewScope("", []string{"inspect", "execute", "mutate"}, 0)
	got := picker.Resolve(spec, scope)

	// The SOURCE is the category footprint: execute ⇒ the worker-faculty execute tool run_tests. NOT run_shell
	// (no worker faculty carries it) and NOT the flat list verbatim.
	want := []string{"run_tests"}
	if !sameSet(got, want) {
		t.Fatalf("category-sourced pick = %v; want %v (the resolver must source by CATEGORY, not echo the flat "+
			"ToolScope name run_shell)", got, want)
	}
	for _, n := range got {
		if n == "run_shell" {
			t.Fatal("the resolved set leaked run_shell — no worker faculty carries it, so a category source must " +
				"never pick it (the resolver echoed the flat list instead of sourcing by category)")
		}
	}
}

// TestToolPickerCeilingRefusesOutOfBand proves the resolved set still honours the §3.3a ceiling — a worker
// never widens its authority. With an inspect-only ceiling, an execute-footprint operator resolves to NO
// tools (the execute faculty tool is REFUSED by the ceiling), so the category source cannot smuggle a tool
// past the least-privilege band.
func TestToolPickerCeilingRefusesOutOfBand(t *testing.T) {
	picker := NewToolPicker(liveRoster(), liveRegistry(), false)
	spec, _ := cognition.NewOperatorRegistry().Get("measure") // ScopeCategories {execute}, ToolScope {run_tests}

	// inspect-only ceiling: the execute tool run_tests is OUT of band ⇒ refused ⇒ empty pick.
	roScope := NewScope("", []string{"inspect"}, 0)
	if got := picker.Resolve(spec, roScope); len(got) != 0 {
		t.Fatalf("an inspect-only ceiling must REFUSE the execute tool (a worker never widens its authority); "+
			"resolved %v", got)
	}

	// inspect+execute ceiling: run_tests is in band ⇒ admitted.
	ieScope := NewScope("", []string{"inspect", "execute"}, 0)
	if got := picker.Resolve(spec, ieScope); !sameSet(got, []string{"run_tests"}) {
		t.Fatalf("an inspect+execute ceiling must admit run_tests; resolved %v", got)
	}
}

// TestInstantiateSourcesToolsFromPicker is the SUBCONSCIOUS-LEVEL wiring gate (built != wired): a Workflow
// staffed via WithStaffing with a ToolPicker must source EACH worker's tool set from the picker (the category
// footprint), not the operator's flat ToolScope — proven by reading the worker's effective scope. It is
// mutation-sensitive: it asserts the staffed worker's tools are exactly the category-resolved set for the
// seed op it staffs. With NO picker (the default) the worker carries the flat ToolScope (byte-identical).
func TestInstantiateSourcesToolsFromPicker(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	prog := staffingProgram() // measure (ToolScope {run_tests}) then validate (ToolScope {run_tests})
	be := backends.NewTest()
	goal := "build and validate the parser"

	picker := NewToolPicker(liveRoster(), liveRegistry(), false)
	scope := NewScope("", []string{"inspect", "execute"}, 0)
	g := graph.New(goal)
	ctx := CaptureContext(g, be, goal, nil)

	wf := FromProgram(prog, cat, be, nil, goal).WithStaffing(scope, ctx, nil, picker)
	if wf.ToolPicker() == nil {
		t.Fatal("WithStaffing must attach the ToolPicker (the gap-5 category source is dead)")
	}

	subs := wf.Instantiate(wf.Current(), nil, nil)
	if len(subs) == 0 {
		t.Fatal("the staffed workflow must instantiate at least one worker for the measure phase")
	}
	for _, sa := range subs {
		spec, _ := cat.Get(sa.Role())
		// the SOURCE is the picker's category resolution, NOT spec.ToolScope inheritance.
		wantSourced := picker.Resolve(spec, scope)
		if !sameSet(sa.toolScope, wantSourced) {
			t.Fatalf("worker %s tools %v != category-sourced %v (the worker was NOT staffed from the picker)",
				sa.id, sa.toolScope, wantSourced)
		}
		// and for the seed measure/validate ops the sourced set is run_tests (parity preserved).
		if !sameSet(sa.toolScope, []string{"run_tests"}) {
			t.Fatalf("worker %s (%s) sourced %v; the seed op's parity set is {run_tests}", sa.id, sa.Role(), sa.toolScope)
		}
	}

	// OFF arm: no picker ⇒ the worker carries the flat ToolScope (byte-identical to before the rewire).
	wfOff := FromProgram(prog, cat, be, nil, goal).WithStaffing(scope, ctx, nil, nil)
	if wfOff.ToolPicker() != nil {
		t.Fatal("a workflow with no picker must carry no ToolPicker (byte-identical default)")
	}
	for _, sa := range wfOff.Instantiate(wfOff.Current(), nil, nil) {
		spec, _ := cat.Get(sa.Role())
		if !sameSet(sa.toolScope, spec.ToolScope) {
			t.Fatalf("no-picker worker %s tools %v != flat ToolScope %v (the default arm must inherit the flat "+
				"list, byte-identical)", sa.id, sa.toolScope, spec.ToolScope)
		}
	}
}

// TestExposeAffordancesSourcesFullInspectSet pins the load-bearing multi-tool parity case: the
// expose-affordances seed op (ToolScope {search, read_file}, category footprint {inspect}) must resolve to
// BOTH inspect-category worker-faculty tools (read_file + search) — the full inspect footprint — not a
// single tool, proving the resolver picks the whole category footprint where the seed op needs it.
func TestExposeAffordancesSourcesFullInspectSet(t *testing.T) {
	picker := NewToolPicker(liveRoster(), liveRegistry(), false)
	spec, ok := cognition.NewOperatorRegistry().Get("expose-affordances")
	if !ok {
		t.Fatal("precondition: the seed catalog must carry expose-affordances")
	}
	scope := NewScope("", []string{"inspect", "execute"}, 0)
	got := picker.Resolve(spec, scope)
	want := []string{"read_file", "search"}
	if !sameSet(got, want) {
		t.Fatalf("expose-affordances must source the full inspect footprint %v; got %v", want, got)
	}
}

// TestToolPickerNilOffline proves the resolver degrades safely: a nil registry (the offline/no-workspace
// path) ⇒ a nil picker ⇒ Resolve returns nil ⇒ the workflow keeps the flat ToolScope (byte-identical).
func TestToolPickerNilOffline(t *testing.T) {
	if NewToolPicker(liveRoster(), nil, false) != nil {
		t.Fatal("a nil registry must yield a nil picker (the offline/no-workspace path)")
	}
	var p *ToolPicker
	if got := p.Resolve(cognition.OperatorSpec{ToolScope: []string{"run_tests"}}, nil); got != nil {
		t.Fatalf("a nil picker must Resolve to nil (so the workflow keeps the flat ToolScope); got %v", got)
	}
}

// sameSet reports whether two string slices contain the same elements (order-insensitive, exact multiset for
// the dedup'd sets here). Both are expected pre-sorted or set-shaped; we sort copies to be safe.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ca := append([]string(nil), a...)
	cb := append([]string(nil), b...)
	sort.Strings(ca)
	sort.Strings(cb)
	for i := range ca {
		if ca[i] != cb[i] {
			return false
		}
	}
	return true
}

// reference the types package so an unused-import guard never trips if a future edit drops a use.
var _ = types.GENERATED
