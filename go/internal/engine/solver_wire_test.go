package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// solverWireBackend is the test double PLUS the StructureFormalizer port (the test double alone does NOT
// implement it, by design). It lets the wiring test prove the engine asserts the formalizer port and
// wires it into the live SolverPrimitiveSubAgent when the knob is on — without a real model. It returns a fixed
// clamp shape with named operands (never literals), exactly as the Pattern-B model would.
type solverWireBackend struct {
	*backends.TestBackend
}

func (solverWireBackend) FormalizeExpression(ctx []types.Thought) (string, []string, bool) {
	return "min(a * b, c)", []string{"a", "b", "c"}, true
}

// hasSolverDomain reports whether the engine's LIVE subconscious roster contains the solver specialist —
// the proof that the opt-in knob actually registered it into the running dispatch loop (not just a unit).
func hasSolverDomain(e *Engine) bool {
	for _, sp := range e.subconscious.Specialists() {
		if sp.Domain() == "solver" {
			return true
		}
	}
	return false
}

// TestSolverPrimitiveSubAgentWiredOnlyWhenKnobOn is the WIRING gate (tests-pass != the feature runs): the
// SolverPrimitiveSubAgent must be ABSENT from the live engine roster when subconscious.solver_specialist is OFF
// (default ⇒ byte-identical) and PRESENT when it is ON. This pins the engine call site
// (engine.go DefaultPrimitiveSubAgents(..., solverFormalizer, e.features.Subconscious.SolverPrimitiveSubAgent)).
func TestSolverPrimitiveSubAgentWiredOnlyWhenKnobOn(t *testing.T) {
	build := func(on bool) *Engine {
		cfg := DefaultConfig()
		feat := config.New() // AllOn
		feat.Subconscious.SolverPrimitiveSubAgent = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, solverWireBackend{backends.NewTest()})
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		return e
	}
	if hasSolverDomain(build(false)) {
		t.Fatal("solver_specialist OFF: the LIVE roster must NOT contain the solver specialist (byte-identical)")
	}
	if !hasSolverDomain(build(true)) {
		t.Fatal("solver_specialist ON: the solver specialist must be wired into the LIVE dispatch roster")
	}
}

// TestSolverFiresThroughLiveDispatch proves the wired specialist actually FIRES on the engine's real
// dispatch loop (the wiring scanner's "prove it fires with the flag on"): with the knob ON, a
// StructureFormalizer wired, and a context carrying grounded reads for every operand, a live
// e.subconscious.Dispatch produces the solver Candidate AND emits subconscious.solver_formalize with the
// exact computed value. This is the cognition-on-the-live-loop proof, not just a unit assertion.
func TestSolverFiresThroughLiveDispatch(t *testing.T) {
	cfg := DefaultConfig()
	feat := config.New()
	feat.Subconscious.SolverPrimitiveSubAgent = true
	cfg.Features = feat
	e, err := NewEngine(&cfg, solverWireBackend{backends.NewTest()})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	var formalizeEvents []events.Event
	e.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.SubSolverFormalize {
			formalizeEvents = append(formalizeEvents, ev)
		}
	})

	// A clamp structure with all three operands traced to GROUNDED reads (OBSERVATION thoughts carrying a
	// real ToolResult payload — what GroundingReadHappened witnesses). 8*40=320, capped at 250 -> 250.
	read := func(id int, text string) types.Thought {
		return types.Thought{ID: id, Text: text, Source: types.OBSERVATION,
			RawReturn: action.ToolResult{Name: "read_file", Content: text}}
	}
	ctx := []types.Thought{
		{ID: 1, Text: "Each unit bills at the rate but the invoice is capped. Compute the total cost.", Source: types.GENERATED},
		read(2, "manifest: units = 8"),
		read(3, "rate.yaml: hourly_rate = 40"),
		read(4, "policy.yaml: invoice_cap = 250"),
	}

	// Drive the LIVE subconscious dispatch at a theta below the solver's 0.82 firing relevance.
	fired, _ := e.subconscious.Dispatch(ctx, 0.3, nil)

	var solverCand *types.Candidate
	for _, c := range fired {
		if c.Domain != nil && *c.Domain == "solver" {
			solverCand = c
		}
	}
	if solverCand == nil {
		t.Fatal("the wired solver specialist must FIRE through the live dispatch on a grounded clamp structure")
	}
	if len(formalizeEvents) == 0 {
		t.Fatal("the live fire must emit subconscious.solver_formalize (the observability contract)")
	}
	if got, ok := formalizeEvents[0].Data["value"]; !ok || got != "250" {
		t.Fatalf("solver_formalize value = %v, want 250 (the exact clamp the bare model mis-computes)", got)
	}
}
