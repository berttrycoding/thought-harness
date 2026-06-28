package engine_test

// refine_loop_wiring_test.go is the WIRING-GATE proof for GAP 11 (the uniform
// per-registry refine loop, 01-subconscious.md §3.17/§3.20): tests passing is not
// enough — the loop must actually FIRE on the engine's live idle-consolidation
// tick when convert.refine_loop is ON, and be byte-silent when OFF. It drives the
// REAL engine (not the convert unit in isolation): prime a mint via the engine's
// own Convertibility, then step into idle consolidation and assert the engine's
// RefineRegistry call site emitted convert.refine on the live bus.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// primeMint drives the engine's own Convertibility to mint one specialist for a
// goal (the same flywheel the live loop uses: an effortful, high-value pattern
// converges, then idle consolidation compiles it) so there is a real registry
// entry for the refine loop to measure.
func primeMint(t *testing.T, e *engine.Engine, goal string) {
	t.Helper()
	g := graph.New(goal)
	for i := 0; i < 3; i++ { // MintAfter effortful GENERATED steps
		g.Append(&types.Thought{ID: -1, Text: "worked step", Source: types.GENERATED, Confidence: 0.6}, 1)
	}
	g.Active().Value = 0.9
	g.Active().Epistemic = 0.9 // the mint gate reads the epistemic projection
	e.Convert().Observe(g)
	if minted := e.Convert().Consolidate(); len(minted) != 1 {
		t.Fatalf("priming: expected one mint for %q; got %v", goal, minted)
	}
}

// refineEvents returns the convert.refine events captured on the bus.
func refineEvents(evs []events.Event) []events.Event {
	var out []events.Event
	for _, ev := range evs {
		if ev.Kind == string(events.RegistryRefine) {
			out = append(out, ev)
		}
	}
	return out
}

// TestRefineLoopFiresOnLiveIdleTick: with convert.refine_loop ON, a reactive
// engine that has a minted entry runs the per-registry refine pass at its idle
// consolidation tick and emits convert.refine on the live bus — proving the
// wiring is real, not just that the convert unit works in isolation.
func TestRefineLoopFiresOnLiveIdleTick(t *testing.T) {
	feat := config.New()
	feat.Convert.RefineLoop = true

	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var captured []events.Event
	e.Bus().Subscribe(func(ev events.Event) { captured = append(captured, ev) })

	primeMint(t, e, "compute the tax bracket value")

	// a fresh reactive engine with no submission has graph==nil -> Step() goes straight to the idle
	// consolidation branch, where the engine calls RefineRegistry() (the live call site).
	e.Step()

	refs := refineEvents(captured)
	if len(refs) == 0 {
		t.Fatal("convert.refine_loop ON: the engine's idle tick must FIRE the per-registry refine pass on the live bus")
	}
	// the summary event names the registry it refined.
	var sawSummary bool
	for _, ev := range refs {
		if ev.Data["kind"] == "registry_refine" && ev.Data["registry"] == "specialist" {
			sawSummary = true
		}
	}
	if !sawSummary {
		t.Fatalf("the live refine pass must emit a per-registry summary for the specialist registry; got %d refine events", len(refs))
	}
}

// TestRefineLoopOffIsLiveByteSilent: with the flag EXPLICITLY OFF, the SAME live
// idle tick emits NO convert.refine event — the OFF byte-identical contract at the
// live call site, proving the legacy path stays reachable. NOTE: since the
// 2026-06-20 redesign go-live convert.refine_loop DEFAULTS ON (AllOn), so this test
// must build with the flag flipped OFF rather than rely on the default. The
// default-ON live firing is covered by TestRefineLoopFiresOnLiveIdleTick.
func TestRefineLoopOffIsLiveByteSilent(t *testing.T) {
	feat := config.New()            // AllOn baseline: refine_loop is ON by default since go-live...
	feat.Convert.RefineLoop = false // ...so flip it explicitly OFF to exercise the legacy/toggleable path.

	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var captured []events.Event
	e.Bus().Subscribe(func(ev events.Event) { captured = append(captured, ev) })

	primeMint(t, e, "compute the tax bracket value")
	e.Step() // the same idle tick

	if refs := refineEvents(captured); len(refs) != 0 {
		t.Fatalf("with convert.refine_loop OFF the live idle tick must emit no convert.refine; got %d", len(refs))
	}
}
