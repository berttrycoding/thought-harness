package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// staffingProgram builds a small synthesised program whose first phase staffs a worker for an effectful
// operator (one carrying a real ToolScope), so the staffing path actually produces a SubAgent.
func staffingProgram() *cognition.Program {
	return &cognition.Program{
		Root: cognition.NewSeq(
			cognition.NewStep("measure", "code", ""), // measure carries ToolScope {run_tests} (an effectful op)
			cognition.NewStep("validate", "code", ""),
		),
		Goal:        "build and validate the parser",
		Synthesized: true,
	}
}

// TestInstantiateThreadsContextAndScope is the SUBCONSCIOUS-LEVEL wiring gate (built != wired, gap 2/3/4):
// a Workflow staffed via WithStaffing(scope, context, recapture) must hand the Context+Scope down to EVERY
// SubAgent it Instantiates — proven by reading the worker's own accessors, not by it merely compiling. With
// NO staffing set (the default), the worker carries neither (byte-identical to before the rewire). Here the
// recapture closure is nil ⇒ the static episode-open context is used (the fallback arm).
func TestInstantiateThreadsContextAndScope(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	prog := staffingProgram()
	be := backends.NewTest()

	// A captured Context with a real frozen branch snapshot (gap 2 material).
	g := graph.New("build and validate the parser")
	g.Append(&types.Thought{ID: -1, Text: "the parser reads tokens", Source: types.GENERATED}, 1)
	g.Append(&types.Thought{ID: -1, Text: "the grammar is LL(1)", Source: types.GENERATED}, 2)
	ctx := CaptureContext(g, be, "build and validate the parser", nil)
	if len(ctx.Snapshot) == 0 {
		t.Fatal("precondition: the captured Context must carry a non-empty branch snapshot")
	}

	// A read-only ceiling (inspect/execute, NO mutate) — the §3.3a authority band.
	sc := NewScope("", []string{"inspect", "execute"}, 0)

	wfOff := FromProgram(prog, cat, be, nil, "build and validate the parser")
	wfOn := FromProgram(prog, cat, be, nil, "build and validate the parser").WithStaffing(sc, ctx, nil, nil)

	if wfOn.Scope() == nil {
		t.Fatal("WithStaffing must attach the Scope to the workflow (the run's authority ceiling)")
	}
	if wfOff.Scope() != nil {
		t.Fatal("a workflow with no staffing must carry NO scope (byte-identical default)")
	}

	phaseOn := wfOn.Current()
	subsOn := wfOn.Instantiate(phaseOn, nil, nil)
	if len(subsOn) == 0 {
		t.Fatal("the staffed workflow must instantiate at least one SubAgent for the phase")
	}
	for _, sa := range subsOn {
		// gap 2: the worker RECEIVED the rich Context (it reads the whole frozen branch, not the ≤5 slice).
		if sa.Context() == nil {
			t.Fatalf("SubAgent %s did NOT receive the captured Context (gap-2 wire is dead)", sa.id)
		}
		if got := sa.Context().WorkerContext(); len(got) != len(ctx.WorkerContext()) {
			t.Fatalf("SubAgent %s got a different worker context (%d) than the captured one (%d)",
				sa.id, len(got), len(ctx.WorkerContext()))
		}
		// gap 3/4: the worker RECEIVED the ceiling — its scoped tool list is filtered by category.
		if sa.scope == nil {
			t.Fatalf("SubAgent %s did NOT receive the authority ceiling (gap-3/4 wire is dead)", sa.id)
		}
	}

	// OFF: a worker with no staffing carries neither (the default-OFF byte-identical posture).
	phaseOff := wfOff.Current()
	subsOff := wfOff.Instantiate(phaseOff, nil, nil)
	for _, sa := range subsOff {
		if sa.Context() != nil {
			t.Errorf("unstaffed SubAgent %s must carry NO Context (byte-identical default)", sa.id)
		}
		if sa.scope != nil {
			t.Errorf("unstaffed SubAgent %s must carry NO scope (byte-identical default)", sa.id)
		}
	}
}

// TestStaffingRecaptureDeliversGrownBranch is the gap-2 FIX gate (the live-claude finding made offline-
// mechanical): the staffing-time recapture closure must hand a worker the branch AS IT IS WHEN STAFFED —
// the GROWN branch — not the goal-root snapshot an episode-OPEN capture froze. It models the exact bug: the
// static context is the 1-thought episode-open snapshot, but the live branch grows to 5 thoughts by the
// time the worker is staffed; the recapture must win, so the worker reads the grown branch, never the
// goal root.
func TestStaffingRecaptureDeliversGrownBranch(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	prog := staffingProgram()
	be := backends.NewTest()
	goal := "build and validate the parser"

	// THE LIVE GRAPH — starts as the goal root (1 thought), exactly as an episode opens.
	g := graph.New(goal)

	// The episode-OPEN capture freezes the goal root (1 thought) — the STALE fallback that starved workers.
	openCtx := CaptureContext(g, be, goal, nil)
	if got := openCtx.WorkerContext(); len(got) != 1 {
		t.Fatalf("precondition: the episode-open capture must freeze just the goal root (1 thought); got %d", len(got))
	}

	// The conscious keeps thinking: the branch GROWS past the goal root (the mid-episode reality).
	g.Append(&types.Thought{ID: -1, Text: "the parser reads tokens", Source: types.GENERATED}, 1)
	g.Append(&types.Thought{ID: -1, Text: "the grammar is LL(1)", Source: types.GENERATED}, 2)
	g.Append(&types.Thought{ID: -1, Text: "ambiguity in rule 4", Source: types.GENERATED}, 3)
	g.Append(&types.Thought{ID: -1, Text: "left-factor rule 4", Source: types.GENERATED}, 4)

	// The recapture closure re-captures against the LIVE (grown) graph at staffing time — what the engine wires.
	recapture := func() *Context { return CaptureContext(g, be, goal, nil) }

	sc := NewScope("", []string{"inspect", "execute"}, 0)
	wf := FromProgram(prog, cat, be, nil, goal).WithStaffing(sc, openCtx, recapture, nil)

	subs := wf.Instantiate(wf.Current(), nil, nil)
	if len(subs) == 0 {
		t.Fatal("the staffed workflow must instantiate at least one SubAgent for the phase")
	}
	for _, sa := range subs {
		if sa.Context() == nil {
			t.Fatalf("SubAgent %s did NOT receive a Context", sa.id)
		}
		// THE FIX: the worker reads the GROWN branch (5 thoughts), NOT the goal root (1) the open-capture froze.
		got := len(sa.Context().WorkerContext())
		if got <= len(openCtx.WorkerContext()) {
			t.Fatalf("SubAgent %s was STARVED: got the episode-open snapshot (%d) not the grown branch — "+
				"the recapture did not win (gap-2 fix is dead)", sa.id, got)
		}
		if got != 5 {
			t.Fatalf("SubAgent %s must read the WHOLE grown branch (5 thoughts); got %d", sa.id, got)
		}
		// And workerSlice (the prefer-richer safety belt) actually serves the grown branch, not the ≤5 live
		// tail — here the captured grown branch (5) >= the live tail, so the rich Context is used.
		if served := len(sa.workerSlice(g.ActiveContext())); served != 5 {
			t.Fatalf("workerSlice must serve the grown captured branch (5); got %d", served)
		}
	}
}

// TestWorkerSlicePrefersRicherNeverStarves is the safety-belt gate (gap-2 fix part 2): workerSlice must
// pick the RICHER of {captured Context, live ≤5 tail} by thought-count — so a captured Context that is
// somehow THINNER than the live tail (a stale/early capture) can NEVER starve the worker. This is the
// invariant that makes the feature net-safe even if the capture timing regresses: the live tail is the floor.
func TestWorkerSlicePrefersRicherNeverStarves(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	be := backends.NewTest()
	spec, ok := cat.Get("measure")
	if !ok {
		t.Fatal("precondition: the seed catalog must carry the measure operator")
	}

	// A THIN captured Context (the starving goal-root snapshot): 1 thought only.
	gThin := graph.New("g")
	thin := CaptureContext(gThin, be, "g", nil)
	if len(thin.WorkerContext()) != 1 {
		t.Fatalf("precondition: the thin capture must carry 1 thought; got %d", len(thin.WorkerContext()))
	}

	// A RICHER live context (3 substantive thoughts) — what the worker would have from its own ≤5 tail.
	liveCtx := []types.Thought{
		{Text: "step one", Source: types.GENERATED},
		{Text: "step two", Source: types.GENERATED},
		{Text: "step three", Source: types.GENERATED},
	}

	sa := NewSubAgent(spec, "code", "g", be, nil, "sa:test", nil, nil, nil).WithContext(thin)
	served := sa.workerSlice(liveCtx)
	// The live tail (3) is richer than the thin capture (1) ⇒ the live tail must win (NO starvation).
	if len(served) != len(liveCtx) {
		t.Fatalf("workerSlice must prefer the richer live tail (%d) over the thinner capture (1); got %d",
			len(liveCtx), len(served))
	}

	// And the converse: when the captured Context IS richer, it wins (the enrichment).
	gRich := graph.New("g")
	gRich.Append(&types.Thought{ID: -1, Text: "a", Source: types.GENERATED}, 1)
	gRich.Append(&types.Thought{ID: -1, Text: "b", Source: types.GENERATED}, 2)
	gRich.Append(&types.Thought{ID: -1, Text: "c", Source: types.GENERATED}, 3)
	gRich.Append(&types.Thought{ID: -1, Text: "d", Source: types.GENERATED}, 4)
	gRich.Append(&types.Thought{ID: -1, Text: "e", Source: types.GENERATED}, 5)
	rich := CaptureContext(gRich, be, "g", nil) // 6 thoughts (root + 5)
	saRich := NewSubAgent(spec, "code", "g", be, nil, "sa:test", nil, nil, nil).WithContext(rich)
	if got := len(saRich.workerSlice(liveCtx)); got != len(rich.WorkerContext()) {
		t.Fatalf("workerSlice must prefer the richer capture (%d) over the live tail (3); got %d",
			len(rich.WorkerContext()), got)
	}
}

// TestScopedToolScopeBitesUnderCeiling proves the staffed ceiling actually CONSTRAINS the worker (the
// §3.3a "a worker may never widen its ceiling" bite): a SubAgent whose flat toolScope includes a MUTATE
// tool (write_file), staffed under an inspect/execute-only ceiling, drops the write tool from its
// effective scope. This is the load-bearing behavioural difference the scope wiring buys — not just that
// the field is set, but that ScopedToolScope HONOURS it.
func TestScopedToolScopeBitesUnderCeiling(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	spec, ok := cat.Get("measure")
	if !ok {
		t.Fatal("precondition: the seed catalog must carry the measure operator")
	}
	// A worker with a deliberately over-broad flat scope (a read, a run, AND a write).
	broad := []string{"read_file", "run_tests", "write_file"}
	sa := NewSubAgent(spec, "code", "g", backends.NewTest(), nil, "sa:test", broad, nil, nil)

	// No ceiling ⇒ the flat scope passes through unchanged (byte-identical default).
	if got := sa.ScopedToolScope(); len(got) != len(broad) {
		t.Fatalf("with no scope the effective tool scope must be the flat list (%d); got %d", len(broad), len(got))
	}

	// Under an inspect/execute-only ceiling the MUTATE tool (write_file) must be dropped.
	sa.WithScope(NewScope("", []string{"inspect", "execute"}, 0))
	scoped := sa.ScopedToolScope()
	for _, tn := range scoped {
		if tn == "write_file" {
			t.Fatalf("the inspect/execute ceiling must DROP write_file (mutate); effective scope = %v", scoped)
		}
	}
	if len(scoped) != 2 {
		t.Fatalf("the ceiling must keep read_file + run_tests and drop write_file; got %v", scoped)
	}
}
