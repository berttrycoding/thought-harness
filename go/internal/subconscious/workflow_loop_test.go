package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
)

func loopWorkflow() *Workflow {
	prog := &cognition.Program{
		Root:        cognition.LoopBody(cognition.NewStep("validate", "test", "")), // MaxIter 3, Until "good enough"
		Synthesized: true,
	}
	return FromProgram(prog, cognition.NewOperatorRegistry(), backends.NewTest(), nil, "iterate until good")
}

// TestLoopEarlyExitsOnSuccess is the P3.2 gate: a loop is a feedback operator — when its stopping
// condition is satisfied after an iteration, the remaining iterations are skipped (it does not run to
// the MaxIter bound). The MaxIter unroll remains the hard upper bound; early-exit just stops sooner.
func TestLoopEarlyExitsOnSuccess(t *testing.T) {
	wf := loopWorkflow()
	loopPhases := 0
	for _, ph := range wf.Phases {
		if ph.Plan.Loop != nil {
			loopPhases++
		}
	}
	if loopPhases < 2 {
		t.Fatalf("the loop should unroll to >= 2 phases to test early-exit; got %d", loopPhases)
	}

	// success after the first iteration -> collapse the remaining iterations.
	if !wf.SkipLoopIfSatisfied(func(string) bool { return true }) {
		t.Fatal("a satisfied loop must early-exit instead of running every iteration")
	}
	wf.Advance()
	if !wf.Exhausted() {
		t.Fatalf("after early-exit the loop should be exhausted; cursor=%d of %d phases", wf.I(), len(wf.Phases))
	}
}

// TestLoopRunsToBoundWhenUnsatisfied guards the other side: an unsatisfied condition (or no feedback
// wired at all) leaves the loop running its iterations — back-compatible with the prior unroll.
func TestLoopRunsToBoundWhenUnsatisfied(t *testing.T) {
	wf := loopWorkflow()
	if wf.SkipLoopIfSatisfied(func(string) bool { return false }) {
		t.Fatal("an unsatisfied loop must not early-exit")
	}
	if wf.SkipLoopIfSatisfied(nil) {
		t.Fatal("a nil feedback predicate must not early-exit (back-compat / golden-preserving)")
	}
	if wf.Exhausted() {
		t.Fatal("the loop should still have iterations remaining when not satisfied")
	}
}

// TestEarlyExitNoLoopIsNoop: outside a loop the feedback check is a no-op (never skips a plain Seq).
func TestEarlyExitNoLoopIsNoop(t *testing.T) {
	prog := &cognition.Program{
		Root:        cognition.NewSeq(cognition.NewStep("decompose", "general", ""), cognition.NewStep("generate", "general", "")),
		Synthesized: true,
	}
	wf := FromProgram(prog, cognition.NewOperatorRegistry(), backends.NewTest(), nil, "g")
	if wf.SkipLoopIfSatisfied(func(string) bool { return true }) {
		t.Fatal("a non-loop phase must never early-exit")
	}
}
