package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// newCapabilityDispatchEngine builds a reactive engine with subconscious.capability +
// subconscious.capability_dispatch flipped to the requested state (everything else all-on), on the test
// double, with the event log subscribed. The two flags are independent: capability ON is required for a
// producing Capability to EXIST; capability_dispatch ON routes the dispatch entry through it.
func newCapabilityDispatchEngine(t *testing.T, capability, dispatch bool) (*engine.Engine, *eventLog) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.New() // AllOn
	feat.Subconscious.Capability = capability
	feat.Subconscious.CapabilityDispatch = dispatch
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// TestCapabilityDispatchEntryFiresLive is the GAP 5-DEEPER COGNITION property (not plumbing): with the
// dispatch flag ON the Capability becomes the LIVE relevance/dispatch entry — and that entry actually
// RUNS on the engine's tick. The proof is that across a real reactive episode whose goal shapes a
// workflow:
//
//   - subconscious.entry FIRES (the Capability owns the dispatch entry — the live path activated), AND
//   - the workflow is STILL recognised + dispatched THROUGH it (subconscious.workflow still appears) —
//     so routing the entry through the Capability did NOT break the cognition; the phase still fires.
//
// This is the wiring-gate lesson in test form: a recognizer that is merely SET but never consulted on
// the live tick would pass the engine-side wiring gate yet emit no subconscious.entry here. The event
// firing on the LIVE loop is what proves the entry is the real dispatch entry, not a dead field.
func TestCapabilityDispatchEntryFiresLive(t *testing.T) {
	// A goal the synthesiser shapes into a (bespoke) workflow, so a Capability is produced + recognised.
	const goal = "design and validate a small todo service step by step"

	on, onLog := newCapabilityDispatchEngine(t, true, true)
	on.SubmitDefault(goal)
	on.Run(30)

	entries := onLog.of(string(events.SubEntry))
	if len(entries) == 0 {
		t.Fatal("dispatch flag ON: subconscious.entry must fire — the Capability is not the LIVE dispatch entry (the wire is dead on the tick)")
	}
	// The entry event must name the producing Capability + the workflow it triggers (observability is
	// the contract: the entry is invisible otherwise, since the verdict is byte-identical to self-trigger).
	first := entries[0]
	if first.Data["capability"] != "episode" {
		t.Errorf("subconscious.entry must name the producing Capability; got %v", first.Data["capability"])
	}
	if _, ok := first.Data["workflow"]; !ok {
		t.Error("subconscious.entry must carry the workflow it is the entry for")
	}
	// The cognition still fires through the entry: a workflow was recognised + dispatched (phase events).
	if len(onLog.of(string(events.SubWorkflow))) == 0 {
		t.Fatal("dispatch flag ON: the workflow must still be recognised + dispatched THROUGH the entry (no subconscious.workflow ⇒ the entry broke dispatch)")
	}
}

// TestCapabilityDispatchOffIsSilent is the flag-OFF half of the property: with the dispatch flag OFF the
// Workflow self-triggers exactly as today — NO subconscious.entry event fires (the entry seam is inert).
// The SAME workflow recognition still occurs (the cognition is unchanged), proving OFF is the legacy
// path: byte-identical behaviour, only the new entry event suppressed.
func TestCapabilityDispatchOffIsSilent(t *testing.T) {
	const goal = "design and validate a small todo service step by step"

	off, offLog := newCapabilityDispatchEngine(t, true, false) // capability ON (producer runs), dispatch OFF
	off.SubmitDefault(goal)
	off.Run(30)

	if n := len(offLog.of(string(events.SubEntry))); n != 0 {
		t.Fatalf("dispatch flag OFF: subconscious.entry must NOT fire (the Workflow self-triggers); got %d", n)
	}
	// The workflow is still recognised + dispatched via the self-trigger path — the cognition is unchanged.
	if len(offLog.of(string(events.SubWorkflow))) == 0 {
		t.Fatal("dispatch flag OFF: the workflow must still self-trigger (no subconscious.workflow ⇒ the producer path itself regressed)")
	}
}
