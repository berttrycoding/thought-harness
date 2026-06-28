package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestAssembleViewEmittedAtResponse is the Context-Assembly gate: every episode that delivers a response
// assembles an EXECUTIVE-template view of its context and emits seam.assemble (the seam as a view-
// producer, SR-2). The event carries the template, the item count, and the working-set budget.
func TestAssembleViewEmittedAtResponse(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	var asm []events.Event
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Assemble {
			asm = append(asm, ev)
		}
	})
	e.SubmitDefault("What's 6 times 7?")
	e.Run(8)
	if len(asm) == 0 {
		t.Fatalf("delivering a response should assemble + emit a view")
	}
	if asm[0].Data["template"] != "D:executive" {
		t.Errorf("the responder consumes the executive template, got %v", asm[0].Data["template"])
	}
}
