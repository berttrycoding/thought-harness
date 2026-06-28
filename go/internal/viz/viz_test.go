package viz

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/session"
)

// TestRenderPhaseTimeline is the P3.7 gate (program view): a synthesised program renders as an ordered
// phase timeline, marking parallel fan-out, script nodes, and loop iterations.
func TestRenderPhaseTimeline(t *testing.T) {
	prog := cognition.Program{
		Root: cognition.NewSeq(
			cognition.NewStep("decompose", "general", ""),
			cognition.NewPar(cognition.NewStep("compare", "general", ""), cognition.NewStep("contrast", "general", "")),
			cognition.Step{Operator: "validate", Domain: "general", Role: cognition.RoleScript},
		),
		Synthesized: true,
	}
	out := RenderPhaseTimeline(prog)
	if !strings.Contains(out, "decompose") || !strings.Contains(out, "compare ‖ contrast") {
		t.Fatalf("timeline should show ordered phases incl. the parallel fan-out; got:\n%s", out)
	}
	if !strings.Contains(out, "validate [script]") {
		t.Fatalf("a script node should be marked; got:\n%s", out)
	}
	if !strings.Contains(out, "⇉") {
		t.Fatalf("a parallel phase should carry the fan-out marker; got:\n%s", out)
	}
}

// TestRenderSessionTree (session view): a dispatched session spawn tree renders as an indented tree with
// each node's horizon + budget, bounded by design.
func TestRenderSessionTree(t *testing.T) {
	spec := session.Spec{Horizon: session.Bounded, Schedule: session.Schedule{Kind: session.OnDemand}, TickBudget: 8}
	root, _ := session.NewSession("design the API", spec)
	root.Budget = &session.Budget{TokenCap: 1000, Spent: 120}
	child, _ := root.Dispatch("draft the schema", spec)
	child.Budget = &session.Budget{TokenCap: 500, Spent: 60}
	root.Dispatch("write the handlers", spec)

	out := RenderSessionTree(root)
	if !strings.Contains(out, "design the API") || !strings.Contains(out, "bounded") {
		t.Fatalf("session tree should show the root with its horizon; got:\n%s", out)
	}
	if !strings.Contains(out, "├─ ") || !strings.Contains(out, "└─ ") {
		t.Fatalf("the tree should draw branch connectors for children; got:\n%s", out)
	}
	if !strings.Contains(out, "120/1000 tok") {
		t.Fatalf("a metered session should show its budget; got:\n%s", out)
	}
	// the two children are both present.
	if !strings.Contains(out, "draft the schema") || !strings.Contains(out, "write the handlers") {
		t.Fatalf("both dispatched children should appear; got:\n%s", out)
	}
}

// TestRenderIsDeterministic: the renderers are pure (same input -> same output).
func TestRenderIsDeterministic(t *testing.T) {
	prog := cognition.Program{Root: cognition.NewSeq(cognition.NewStep("measure", "g", "")), Synthesized: true}
	first := RenderPhaseTimeline(prog)
	for i := 0; i < 20; i++ {
		if RenderPhaseTimeline(prog) != first {
			t.Fatal("phase-timeline render is non-deterministic")
		}
	}
}
