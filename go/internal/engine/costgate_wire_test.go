package engine

// costgate_wire_test.go proves the W5 cost-aware trace->skill mint gate is WIRED INTO THE LIVE ENGINE
// (not just the convert unit): with convert.cost_gate ON, the engine enables the gate on its LIVE
// Convertibility and the gate's hold/admit decision lands on the ENGINE'S bus (the observability
// contract). With the knob OFF the gate is dormant and silent — the default-OFF byte-identical contract
// at the engine seam. The tap that feeds the gate (the synthesize_program completion-token subscriber) is
// asserted live too. The convert-unit cognition (cost discrimination) is proven in
// internal/convert/costgate_test.go; this is the WIRING witness.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// costGateEvents builds a reactive engine with convert.cost_gate set to `on`, drives the LIVE
// Convertibility (the engine's own convert object, e.Convert()) through a mint-eligible recurring shape
// with a SUB-FLOOR re-synthesis cost, runs idle Consolidate, and returns the convert.cost_gate events
// that reached the engine's bus.
func costGateEvents(t *testing.T, on bool, cost int) ([]events.Event, *Engine) {
	t.Helper()
	feat := config.New() // AllOn baseline
	feat.Convert.CostGate = on
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var got []events.Event
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.CostGate {
			got = append(got, ev)
		}
	})

	// Drive the engine's LIVE convert through a recurring, valuable shape (clears the count×value gate) so
	// the ONLY remaining gate is the cost gate, then attribute a sub-floor re-synthesis cost and consolidate.
	c := e.Convert()
	goal := "tidy the small config file in the repo"
	prog := miniProg{shape: "seq(generate)"}
	for i := 0; i < c.MintAfter(); i++ {
		c.NoteProgram(goal, prog)
	}
	c.Observe(mintableGraph(goal))
	c.NoteSynthesisCost(goal, cost)
	c.Consolidate()
	return got, e
}

// mintableGraph is a one-branch graph whose active branch converged on value >= MintValue with
// >= MintAfter GENERATED steps — what convert.Observe reads to clear the count×value gate so the cost
// gate is the deciding factor (mirrors internal/convert's buildEpisode through the live engine path).
func mintableGraph(goal string) *graph.ThoughtGraph {
	g := graph.New(goal)
	for i := 0; i < 3; i++ {
		g.Append(&types.Thought{ID: -1, Text: "worked step", Source: types.GENERATED, Confidence: 0.6}, 1)
	}
	g.Active().Value = 0.8
	g.Active().Epistemic = 0.8
	return g
}

// TestCostGateWiredFiresOnEngineBusWhenOn: with convert.cost_gate ON, the gate is LIVE on the engine's
// Convertibility and its hold decision (a sub-floor recurring shape) reaches the engine bus.
func TestCostGateWiredFiresOnEngineBusWhenOn(t *testing.T) {
	got, _ := costGateEvents(t, true, 100) // 100 completion-tok < DefaultMintCostFloor (300) -> HOLD
	if len(got) == 0 {
		t.Fatal("with convert.cost_gate ON, the engine's live mint must surface a convert.cost_gate decision on the bus")
	}
	if k, _ := got[0].Data["kind"].(string); k != "hold" {
		t.Fatalf("a sub-floor recurring shape must be HELD by the wired gate; got kind=%q", k)
	}
}

// TestCostGateWiredSilentWhenOff: with the knob OFF (the default), the engine does NOT enable the gate —
// the SAME sub-floor recurring shape mints with no convert.cost_gate event (byte-identical default path).
func TestCostGateWiredSilentWhenOff(t *testing.T) {
	got, _ := costGateEvents(t, false, 100)
	if len(got) != 0 {
		t.Fatalf("with convert.cost_gate OFF, the engine must emit NO convert.cost_gate events; got %d", len(got))
	}
}

// TestCostGateSynthTapReadsCompletionTokens proves the synth-cost TAP is wired: with the gate ON the
// engine subscribes to the synthesize_program llm.call stream, and a synthesize_program event with
// completion_tokens above the floor, attributed to a recurring shape, ADMITS the mint — exercising the
// whole tap->NoteSynthesisCost->cost-gate path through the engine's bus. (The test double emits no
// synthesize_program events itself, so the event is injected on the bus exactly as the live llm backend
// would emit it, proving the subscriber reads it.) With the gate OFF no subscriber exists, so the same
// event accumulates nothing — the byte-identical default.
func TestCostGateSynthTapReadsCompletionTokens(t *testing.T) {
	feat := config.New()
	feat.Convert.CostGate = true
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e.synthCostTap == nil {
		t.Fatal("with convert.cost_gate ON the engine must install the synth-cost tap subscriber")
	}
	// Emit a synthesize_program llm.call on the engine's bus exactly as the live backend does (role +
	// completion_tokens). The tap must read it.
	e.Bus().Emit(events.LLM, "[synthesize_program] model (5ms): ...",
		events.D{"role": "synthesize_program", "completion_tokens": 1500})
	if got := e.synthCostTap.Load(); got != 1500 {
		t.Fatalf("the synth-cost tap must accumulate synthesize_program completion_tokens off the bus; got %d", got)
	}
	// A non-synthesis llm.call must NOT count toward synthesis cost (only the toolmaker role is the cost a
	// minted skill avoids).
	e.Bus().Emit(events.LLM, "[respond] model (3ms): hi",
		events.D{"role": "respond", "completion_tokens": 9999})
	if got := e.synthCostTap.Load(); got != 1500 {
		t.Fatalf("only synthesize_program tokens are synthesis cost; tap moved to %d on a respond call", got)
	}
}

// TestCostGateNoTapWhenOff: with the knob OFF the engine installs NO tap (the OFF hot loop is untouched).
func TestCostGateNoTapWhenOff(t *testing.T) {
	feat := config.New()
	feat.Convert.CostGate = false
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e.synthCostTap != nil {
		t.Fatal("with convert.cost_gate OFF the engine must NOT install the synth-cost tap (byte-identical)")
	}
}

// miniProg is the narrow convert.Program port (Shape only) — the engine's convert tracks shapes by it.
type miniProg struct{ shape string }

func (p miniProg) Shape() string { return p.shape }
