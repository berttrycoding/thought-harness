package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// newCapabilityPrimitiveSubAgentsEngine builds a reactive engine with subconscious.capability +
// subconscious.capability_specialists flipped to the requested state (everything else all-on), on the test
// double, with the event log subscribed. capability ON is required for a producing Capability to EXIST;
// capability_specialists ON routes specialist admission through it.
func newCapabilityPrimitiveSubAgentsEngine(t *testing.T, capability, specialists bool) (*engine.Engine, *eventLog) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.New() // AllOn (capability_specialists is opt-in ⇒ default OFF even here)
	feat.Subconscious.Capability = capability
	feat.Subconscious.CapabilityPrimitiveSubAgents = specialists
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// TestCapabilityPrimitiveSubAgentsEntryFiresLive is the GAP 5-DEEPER PART 2 COGNITION property (not plumbing):
// with the specialists flag ON the Capability becomes the LIVE specialist-firing entry — and that entry
// actually RUNS on the engine's tick. The proof is that across a real reactive episode:
//
//   - subconscious.spec_gate FIRES (the Capability owns specialist firing — the live path activated), AND
//   - the cognition is unchanged on the GENERAL-Scope episode path: the episode Scope is general (empty
//     domain), so every domain is still admitted ⇒ specialists still fire (subconscious.fire still appears).
//
// The wiring-gate lesson in test form: a gate that is merely SET but never consulted on the live tick would
// pass the engine-side wiring gate yet emit no subconscious.spec_gate here. The event firing on the LIVE
// loop is what proves the entry is the real firing entry, not a dead field.
func TestCapabilityPrimitiveSubAgentsEntryFiresLive(t *testing.T) {
	const goal = "design and validate a small todo service step by step"

	on, onLog := newCapabilityPrimitiveSubAgentsEngine(t, true, true)
	on.SubmitDefault(goal)
	on.Run(30)

	gates := onLog.of(string(events.SubSpecGate))
	if len(gates) == 0 {
		t.Fatal("specialists flag ON: subconscious.spec_gate must fire — the Capability is not the LIVE specialist-firing entry (the wire is dead on the tick)")
	}
	// The gate event must name the producing Capability + the §3.3a domain band it enforces (observability
	// is the contract: on the general-Scope path the admission set is byte-identical, so only this event
	// distinguishes the Capability-gated firing from the bare relevance firing).
	first := gates[0]
	if first.Data["capability"] != "episode" {
		t.Errorf("subconscious.spec_gate must name the producing Capability; got %v", first.Data["capability"])
	}
	if _, ok := first.Data["domain"]; !ok {
		t.Error("subconscious.spec_gate must carry the §3.3a domain band it enforces")
	}
	// The episode Scope is GENERAL (empty domain) ⇒ every domain is admitted ⇒ specialists still fire
	// through the gate (the general-Scope path is byte-identical to bare firing — no specialist is starved).
	if len(onLog.of(string(events.SubFire))) == 0 {
		t.Fatal("specialists flag ON: on the general-Scope path specialists must STILL fire through the gate (no subconscious.fire ⇒ the gate wrongly starved the roster)")
	}
}

// TestCapabilityPrimitiveSubAgentsOffIsSilent is the flag-OFF half: with the specialists flag OFF the bare
// relevance firing runs exactly as today — NO subconscious.spec_gate event fires (the entry seam is inert).
// The SAME specialist firing still occurs (the cognition is unchanged), proving OFF is the legacy path:
// byte-identical behaviour, only the new gate event suppressed.
func TestCapabilityPrimitiveSubAgentsOffIsSilent(t *testing.T) {
	const goal = "design and validate a small todo service step by step"

	off, offLog := newCapabilityPrimitiveSubAgentsEngine(t, true, false) // capability ON (producer runs), specialists OFF
	off.SubmitDefault(goal)
	off.Run(30)

	if n := len(offLog.of(string(events.SubSpecGate))); n != 0 {
		t.Fatalf("specialists flag OFF: subconscious.spec_gate must NOT fire (bare relevance firing); got %d", n)
	}
	// Specialists still fire on bare relevance — the cognition is unchanged.
	if len(offLog.of(string(events.SubFire))) == 0 {
		t.Fatal("specialists flag OFF: specialists must still fire on bare relevance (no subconscious.fire ⇒ the producer path itself regressed)")
	}
}
