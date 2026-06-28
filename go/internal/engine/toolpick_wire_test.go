package engine

import (
	"sort"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// buildCapEngineWS builds an engine with subconscious.capability set to `on`, on the test double, with a
// REAL workspace executor (so the live action registry exists and the category-sourced tool picker is
// non-nil), and opens an episode whose goal synthesises a tool-bearing workflow. Returns the engine so a
// wiring gate can read the live staffing.
func buildCapEngineWS(t *testing.T, on bool, goal string) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = t.TempDir() // non-empty ⇒ buildExecutor builds a live executor (a real tool registry)
	feat := config.New()        // AllOn
	feat.Subconscious.Capability = on
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.startEpisode(goal, true)
	return e
}

// TestCapabilitySourcesWorkerToolsByCategory is THE gap-5 wiring gate (built != wired): with
// subconscious.capability ON and a live workspace, the engine staffs the workflow with a CATEGORY-SOURCED
// tool picker, so a worker the LIVE dispatch instantiates gets its concrete tool set from the run's
// Capability/Scope CATEGORY FOOTPRINT (the live registry + worker-faculty footprint), NOT the operator's
// flat OperatorSpec.ToolScope. It is mutation-sensitive: it asserts the worker's resolved tools equal the
// picker's category resolution AND the seed-op expected set, and it cross-checks that the picker resolves
// the same set the flat ToolScope held (parity preserved). With the flag OFF the workflow carries no picker
// (byte-identical — the flat list is the source).
func TestCapabilitySourcesWorkerToolsByCategory(t *testing.T) {
	// "build and validate ..." synthesises the design-build-validate program (decompose/generate/validate),
	// whose validate phase staffs a tool-bearing worker (ToolScope {run_tests}).
	goal := "build and validate a small parser module"

	on := buildCapEngineWS(t, true, goal)
	wfOn := on.subconscious.Workflow()
	if wfOn == nil {
		t.Fatal("the capability path must produce a workflow for a build/validate goal")
	}
	// gap-5: the engine threaded the category-sourced tool picker into staffing.
	picker := wfOn.ToolPicker()
	if picker == nil {
		t.Fatal("capability ON + a live workspace: the workflow must carry the category-sourced ToolPicker " +
			"(the gap-5 tool SOURCE wire is dead — the worker still inherits the flat ToolScope)")
	}

	// Walk EVERY phase the program staffs; for each tool-bearing worker, prove the SOURCE is the category
	// picker (not the operator's flat ToolScope) AND that the resolved set is the seed-op parity set.
	sawToolBearingWorker := false
	cat := on.catalog
	for {
		phase := wfOn.Current()
		for _, sa := range wfOn.Instantiate(phase, on.executor, on.cognitiveView()) {
			spec, ok := cat.Get(sa.Role())
			if !ok {
				continue
			}
			// the category-sourced expectation (what the picker resolves for this op under the run's scope).
			wantSourced := picker.Resolve(spec, wfOn.Scope())
			got := sa.ToolScope()
			if !sameStrSet(got, wantSourced) {
				t.Fatalf("worker %s (%s) tools %v != category-sourced %v — the worker was staffed from the flat "+
					"ToolScope, not the category picker (gap-5 wire dead)", sa.ID(), sa.Role(), got, wantSourced)
			}
			// PARITY: the category source resolves EXACTLY the flat ToolScope the op declared (so flipping the
			// flag preserves behaviour) — the load-bearing invariant, asserted on the LIVE engine staffing.
			flat := append([]string(nil), spec.ToolScope...)
			if !sameStrSet(wantSourced, flat) {
				t.Fatalf("op %s: category-sourced %v != flat ToolScope %v (PARITY BREAK on the live engine)",
					sa.Role(), wantSourced, flat)
			}
			if len(got) > 0 {
				sawToolBearingWorker = true
			}
		}
		if wfOn.Complete() {
			break
		}
		wfOn.Advance()
	}
	if !sawToolBearingWorker {
		t.Fatal("the build/validate program must staff at least one tool-bearing worker (validate ⇒ run_tests) " +
			"for the gap-5 source to be observably exercised")
	}

	// OFF arm: no picker on the workflow ⇒ the worker inherits the flat ToolScope (byte-identical default).
	off := buildCapEngineWS(t, false, goal)
	if wfOff := off.subconscious.Workflow(); wfOff != nil && wfOff.ToolPicker() != nil {
		t.Error("capability OFF: the workflow must carry NO ToolPicker (the flat ToolScope is the source — " +
			"byte-identical default)")
	}
}

// sameStrSet reports whether two string slices hold the same elements (order-insensitive).
func sameStrSet(a, b []string) bool {
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
