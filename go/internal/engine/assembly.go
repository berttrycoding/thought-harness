package engine

import (
	"github.com/berttrycoding/thought-harness/internal/assembly"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// emitAssembledView makes Context Assembly (SR-2 / P4.2) observable: the seam is a view-PRODUCER — a
// consumer's context is selected, ordered, and budget-truncated through one of five templates, not
// handed the raw graph. This builds the EXECUTIVE view (the structural branch+value summary the
// responder/Controller consumes) of the active working context and emits seam.assemble with the
// template, the raw→assembled sizes, and the working-set budget (P4.1). Called once per episode at the
// response, so the assembly is observable at the point a consumer's view is actually produced.
func (e *Engine) emitAssembledView() {
	ctx := e.workingContext()
	items := make([]assembly.Item, len(ctx))
	for i, t := range ctx {
		items[i] = assembly.Item{
			ID: t.ID, Text: t.Text, Tick: e.bus.Tick,
			Relevance: t.Confidence, Branch: derefIntOr(t.BranchID, 0),
			Value: e.valueScalar(), Active: true,
		}
	}
	view := assembly.Assemble(assembly.Executive, items, e.cfg.ContextBudget)
	e.bus.Emit(events.Assemble, "assembled "+assembly.Executive.String()+" view ("+itoa(len(items))+" items)",
		events.D{"template": assembly.Executive.String(), "items": len(items),
			"budget": e.cfg.ContextBudget, "view_chars": len(view)})
}
