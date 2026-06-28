package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// streamOf builds a small stream of GENERATED thoughts for relevance matching.
func streamOf(texts ...string) []types.Thought {
	out := make([]types.Thought, 0, len(texts))
	for i, t := range texts {
		out = append(out, types.Thought{ID: i, Text: t, Source: types.GENERATED})
	}
	return out
}

// TestCapabilityRelevanceMatch is the entry's trigger: a Capability lights up by RELEVANCE (pull),
// matching the stream against its triggers with the same has-any phrase/word-boundary matcher the
// retired Workflow.Recognize used (§3.3). A trigger match -> 1.0; a miss -> 0.0.
func TestCapabilityRelevanceMatch(t *testing.T) {
	cap := NewCapability("compare-things", []string{"compare", "trade-off"},
		cognition.NewOperatorRegistry(), backends.NewTest())

	hit := streamOf("we should COMPARE the two options")
	if got := cap.Relevance(hit); got != 1.0 {
		t.Fatalf("a trigger match must fire relevance 1.0; got %v", got)
	}
	miss := streamOf("let me just write the function")
	if got := cap.Relevance(miss); got != 0.0 {
		t.Fatalf("no trigger in the stream must be 0.0; got %v", got)
	}
}

// TestCapabilityRelevanceWordBoundary confirms the has-any matcher's word-boundary semantics carry
// over (a single-word trigger matches a whole word, not a prefix) — so the trigger role transfers
// with identical behaviour to the recognition it replaces.
func TestCapabilityRelevanceWordBoundary(t *testing.T) {
	cap := NewCapability("clean", []string{"clean"}, cognition.NewOperatorRegistry(), backends.NewTest())
	if got := cap.Relevance(streamOf("clean it up")); got != 1.0 {
		t.Fatalf("the whole word 'clean' must match; got %v", got)
	}
	if got := cap.Relevance(streamOf("do it cleanly")); got != 0.0 {
		t.Fatalf("'cleanly' must NOT match the word trigger 'clean'; got %v", got)
	}
}

// TestCapabilityNoTriggersNeverFires: a capability with no triggers stays silent (0.0) rather than
// firing on everything — a mis-constructed capability must not become always-on by default.
func TestCapabilityNoTriggersNeverFires(t *testing.T) {
	cap := NewCapability("bare", nil, cognition.NewOperatorRegistry(), backends.NewTest())
	if got := cap.Relevance(streamOf("anything at all")); got != 0.0 {
		t.Fatalf("a no-trigger capability must never fire on relevance; got %v", got)
	}
	if got := cap.RelevanceText("anything at all"); got != 0.0 {
		t.Fatalf("RelevanceText must also be 0.0 with no triggers; got %v", got)
	}
}

// TestCapabilityCaptureContext: the entry captures the active-branch Context at trigger time (§3.3
// (a)). The captured Context carries the L1 snapshot of the live active branch (its thought IDs +
// gist) and the goal that drove the trigger.
func TestCapabilityCaptureContext(t *testing.T) {
	g := graph.New("solve the puzzle")
	g.Append(&types.Thought{ID: -1, Text: "consider the constraints", Source: types.GENERATED}, 1)
	g.Append(&types.Thought{ID: -1, Text: "enumerate the options", Source: types.GENERATED}, 2)

	cap := NewCapability("solve", []string{"solve"}, cognition.NewOperatorRegistry(), backends.NewTest())
	ctx := cap.CaptureContext(g, "solve the puzzle")

	if ctx.Goal != "solve the puzzle" {
		t.Fatalf("captured context goal = %q, want the trigger goal", ctx.Goal)
	}
	// the active branch had 3 thoughts (root + 2 appended) -> L1 spine has 3 IDs.
	if len(ctx.L1.ThoughtIDs) != 3 {
		t.Fatalf("L1 snapshot should freeze all 3 active-branch thought IDs; got %d", len(ctx.L1.ThoughtIDs))
	}
	if ctx.L1.Gist == "" {
		t.Fatal("L1 snapshot should carry a non-empty gist (backend.Summarize)")
	}
	if ctx.L1.BranchID != g.ActiveBranch {
		t.Fatalf("L1 snapshot branch = %d, want active branch %d", ctx.L1.BranchID, g.ActiveBranch)
	}
}

// TestProduceWorkflowFromSeed: a Capability with a SeedProgram REUSES it (reuse-seed-or-synthesise,
// §2.5) — the produced Workflow wraps that template, not a synthesised one.
func TestProduceWorkflowFromSeed(t *testing.T) {
	seed := &cognition.Program{
		Root: cognition.NewSeq(
			cognition.NewStep("decompose", "general", ""),
			cognition.NewStep("validate", "general", ""),
		),
		Goal:        "seeded goal",
		Synthesized: false,
	}
	cap := NewCapability("seeded", []string{"seed"}, cognition.NewOperatorRegistry(), backends.NewTest())
	cap.SeedProgram = seed

	wf, ok := cap.ProduceWorkflow("seeded goal", streamOf("seed it"))
	if !ok || wf == nil {
		t.Fatal("a seeded capability must produce a workflow")
	}
	if wf.Program != seed {
		t.Fatal("the produced workflow must wrap the seed program (reuse, not synthesise)")
	}
	if len(wf.Phases) != 2 {
		t.Fatalf("the seed's two steps should schedule to 2 phases; got %d", len(wf.Phases))
	}
	if wf.Bespoke {
		t.Fatal("a reused seed (Synthesized=false) must not be marked bespoke")
	}
}

// TestProduceWorkflowSynthesises: with no seed, the Capability synthesises on the fly via the
// existing cognition.Synthesize — a "compare" goal yields a real workflow shape (it does not
// re-implement synthesis, it reuses it).
func TestProduceWorkflowSynthesises(t *testing.T) {
	cap := NewCapability("compare", []string{"compare"}, cognition.NewOperatorRegistry(), backends.NewTest())
	wf, ok := cap.ProduceWorkflow("compare A and B", streamOf("compare A and B"))
	if !ok || wf == nil {
		t.Fatal("a compare goal must synthesise a workflow shape")
	}
	if len(wf.Phases) == 0 {
		t.Fatal("the synthesised workflow must have phases")
	}
	if !wf.Bespoke {
		t.Fatal("a synthesised (on-the-fly) workflow must be marked bespoke")
	}
}

// TestProduceWorkflowDeclines: when the synthesiser declines (no workflow shape applies and no seed),
// the Capability propagates (nil, false) rather than inventing a workflow — specialists handle the
// goal directly (§2.5).
func TestProduceWorkflowDeclines(t *testing.T) {
	cap := NewCapability("x", []string{"x"}, cognition.NewOperatorRegistry(), backends.NewTest())
	// an empty goal + empty stream gives the synthesiser nothing to recognise a shape from.
	wf, ok := cap.ProduceWorkflow("", nil)
	if ok || wf != nil {
		t.Fatalf("with no shape and no seed, ProduceWorkflow must decline; got ok=%v wf=%v", ok, wf)
	}
}
