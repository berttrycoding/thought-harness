package wfproof

import (
	"encoding/json"
	"sort"
	"testing"
)

// TestSwarm_EncodesAndVerifies is the headline proof: the lathe swarm encodes in the unified structure,
// captures all FOUR roles (not just skills), and passes the whole-library bound+acyclic gate.
func TestSwarm_EncodesAndVerifies(t *testing.T) {
	lib, root := LatheSwarm()

	ok, issues := VerifyLibrary(lib, root)
	if !ok {
		for _, is := range issues {
			t.Errorf("verify: %s", is)
		}
		t.Fatalf("swarm did NOT pass the library gate")
	}

	// global tally across every reachable workflow — proves hooks+scripts are first-class, not dropped.
	var total Counts
	names := sortedReachable(lib, root)
	for _, n := range names {
		total = total.add(lib[n].CountLocal())
	}
	t.Logf("swarm library: %d workflows reachable", len(names))
	t.Logf("global roles: skills=%d scripts=%d hooks=%d dispatch=%d loops=%d par-groups=%d",
		total.Skills, total.Scripts, total.Hooks, total.Dispatch, total.Loops, total.ParGroups)

	if total.Skills == 0 {
		t.Error("no skills captured")
	}
	if total.Scripts == 0 {
		t.Error("no SCRIPTS captured — the structure dropped the deterministic steps (the gap)")
	}
	if total.Hooks == 0 {
		t.Error("no HOOKS captured — the structure dropped the event-triggered gates (the gap)")
	}
	if total.Dispatch == 0 {
		t.Error("no DISPATCH — without spawn, the swarm cannot scale")
	}
}

// TestSwarm_EachEpisodeIsBounded proves the "at this scale" claim: the whole composition is large, yet
// every single workflow (episode) is small — within MaxSteps/MaxDepth. Scale comes from bounded
// hierarchical SPAWN, not from one giant program.
func TestSwarm_EachEpisodeIsBounded(t *testing.T) {
	lib, root := LatheSwarm()
	for _, name := range sortedReachable(lib, root) {
		wf := lib[name]
		ok, issues := Verify(wf)
		c := wf.CountLocal()
		t.Logf("  %-16s steps=%d depth=%d  (skills=%d scripts=%d hooks=%d dispatch=%d)",
			name, c.Skills+c.Scripts, depthOf(wf.Root), c.Skills, c.Scripts, c.Hooks, c.Dispatch)
		if !ok {
			for _, is := range issues {
				t.Errorf("%s", is)
			}
		}
		if c.Skills+c.Scripts > MaxSteps {
			t.Errorf("%s exceeds MaxSteps as a single episode", name)
		}
	}
}

// TestSwarm_FlatInliningOverflows is the NEGATIVE / falsifiable test that proves WHY dispatch (spawn)
// is the necessary scale mechanism: if we DON'T spawn — if we inline every sub-workflow into one flat
// program (the naive "one big chain") — the step count blows past MaxSteps. Spawn succeeds exactly
// where flattening fails.
func TestSwarm_FlatInliningOverflows(t *testing.T) {
	lib, root := LatheSwarm()

	flat := InlineStepCount(lib, root)
	t.Logf("fully-inlined swarm = %d skill+script steps (one flat program)", flat)
	t.Logf("MaxSteps for ONE workflow = %d", MaxSteps)

	if flat <= MaxSteps {
		t.Fatalf("expected flat inlining to exceed MaxSteps (%d) — the negative test is mis-set if it doesn't", MaxSteps)
	}
	// the same swarm, spawned hierarchically, DID pass (TestSwarm_EncodesAndVerifies). So:
	t.Logf("=> flat inlining FAILS (%d > %d); the same swarm PASSES when spawned per-episode. "+
		"Dispatch is what makes scale possible.", flat, MaxSteps)
}

// TestSwarm_SchedulesToPhases shows a single chain linearises into runnable phases, with the loop
// unrolled to its bound (the tick-loop form).
func TestSwarm_SchedulesToPhases(t *testing.T) {
	lib, _ := LatheSwarm()
	phases := Schedule(lib["chain:feature"])
	t.Logf("chain:feature schedules to %d phases:", len(phases))
	for i, p := range phases {
		par := ""
		if p.Parallel {
			par = " [parallel]"
		}
		t.Logf("  phase %d%s: %v", i, par, p.Steps)
	}
	if len(phases) == 0 {
		t.Fatal("chain:feature scheduled to zero phases")
	}
}

// TestSwarm_RoundTripsAsData proves the whole workflow is captured AS DATA (serialisable -> the
// "logged, standardised, trainable" property). The control-flow tree, the role of every step, AND the
// hooks all survive a JSON round-trip.
func TestSwarm_RoundTripsAsData(t *testing.T) {
	lib, _ := LatheSwarm()
	wf := lib["chain:feature"]

	raw, err := json.Marshal(wf.ToDict())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back["name"] != "chain:feature" {
		t.Errorf("name lost in round-trip: %v", back["name"])
	}
	if hooks, ok := back["hooks"].([]any); !ok || len(hooks) == 0 {
		t.Errorf("workflow hooks lost in round-trip")
	}
	t.Logf("chain:feature serialises to %d bytes of JSON (captured as data)", len(raw))
}

// TestLibrary_RejectsDispatchCycle proves the library gate is REAL: a workflow that spawns a cycle is
// rejected (a non-terminating spawn graph must not pass).
func TestLibrary_RejectsDispatchCycle(t *testing.T) {
	lib := Library{
		"a": {Name: "a", Root: Dispatch{Workflow: "b"}},
		"b": {Name: "b", Root: Dispatch{Workflow: "a"}},
	}
	ok, issues := VerifyLibrary(lib, "a")
	if ok {
		t.Fatal("expected the gate to REJECT a dispatch cycle a->b->a")
	}
	t.Logf("cycle correctly rejected: %v", issues)
}

// TestLibrary_RejectsUnknownDispatch proves a dispatch to a non-existent workflow is caught.
func TestLibrary_RejectsUnknownDispatch(t *testing.T) {
	lib := Library{
		"a": {Name: "a", Root: Dispatch{Workflow: "ghost"}},
	}
	ok, issues := VerifyLibrary(lib, "a")
	if ok {
		t.Fatal("expected the gate to REJECT a dispatch to an unknown workflow")
	}
	t.Logf("unknown dispatch correctly rejected: %v", issues)
}

// sortedReachable is the deterministic name order for logging/iteration.
func sortedReachable(lib Library, root string) []string {
	reach := reachable(lib, root)
	out := make([]string, 0, len(reach))
	for n := range reach {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
