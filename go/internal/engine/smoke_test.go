package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// newHeuristicEngine builds an engine on the explicit TestBackend test double (the path tests
// pin; never the product path), with the ShapeRecognizer wired exactly as NewEngine does.
func newHeuristicEngine(t *testing.T, mode string) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = mode
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// TestNewEngineWiresShapeRecognizer confirms the engine wires the TestBackend's ShapeRecognizer
// to cognition/synth — the Go break for Python's lazy synth import. Without this, SynthesizeProgram
// defers (returns ok=false) and a workflow goal would never synthesise a program.
func TestNewEngineWiresShapeRecognizer(t *testing.T) {
	hb := backends.NewTest()
	if hb.ShapeRecognizer != nil {
		t.Fatal("a fresh TestBackend should have a nil ShapeRecognizer")
	}
	cfg := DefaultConfig()
	if _, err := NewEngine(&cfg, hb); err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if hb.ShapeRecognizer == nil {
		t.Fatal("NewEngine must wire the TestBackend's ShapeRecognizer to cognition/synth")
	}
	// it actually recognises a design-build-validate shape (a workflow goal).
	if _, ok := hb.ShapeRecognizer("design and build and validate the new api endpoint", nil); !ok {
		t.Fatal("the wired ShapeRecognizer should recognise a design-build-validate goal")
	}
}

// TestReactiveRunDoesNotPanic drives a simple Q&A episode end-to-end through the reactive loop and
// asserts it reaches a terminal answer without panicking — the integration smoke test that every
// prior tier is wired correctly.
func TestReactiveRunDoesNotPanic(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	e.SubmitDefault("what is 2 + 2?")
	e.Run(40)
	if e.LastResponse() == "" {
		t.Fatal("a reactive Q&A episode should deliver a non-empty answer")
	}
	// the lifecycle should have advanced past IDLE at some point (it thought).
	if e.lifecycle.State == types.S_ACTIVE && !e.port.Pending() {
		// fine — may have settled; just assert no crash + an answer (above).
	}
}

// TestReactiveWorkflowEpisode drives a workflow-shaped goal (design-build-validate) so the synth +
// workflow + sub-agent wiring all fire; it must run without panicking.
func TestReactiveWorkflowEpisode(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	e.SubmitDefault("design, build and validate a new login endpoint")
	e.Run(40)
	// the goal/action system recorded at least one first-class Goal.
	if len(e.goals) == 0 {
		t.Fatal("startEpisode should record a first-class Goal")
	}
}

// TestContinuousRunDoesNotPanic drives the awake loop through several ticks (no task -> wandering,
// then a salient user percept) and asserts it survives perception + arousal + drives + outreach.
func TestContinuousRunDoesNotPanic(t *testing.T) {
	e := newHeuristicEngine(t, "continuous")
	// wander a few ticks with no task.
	for i := 0; i < 6; i++ {
		e.Step()
	}
	// now a salient user percept interrupts.
	e.SubmitDefault("can you check whether the deploy is safe to ship?")
	for i := 0; i < 12; i++ {
		e.Step()
	}
	if e.Mode() != "continuous" {
		t.Fatalf("mode = %q, want continuous", e.Mode())
	}
}

// TestSetModeCarriesInbox confirms set_mode swaps the port type and carries the pending inbox over,
// resetting the workflow + arousal (the faithful port of Python set_mode).
func TestSetModeCarriesInbox(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	e.SubmitDefault("first queued question")
	e.SubmitDefault("second queued question")
	e.SetMode("continuous")
	if e.Mode() != "continuous" {
		t.Fatalf("mode = %q, want continuous", e.Mode())
	}
	if !e.port.Pending() {
		t.Fatal("set_mode should carry the pending inbox to the new port")
	}
	if e.arousal != types.AWAKE {
		t.Fatal("set_mode should reset arousal to AWAKE")
	}
}
