package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// TestCapabilityPrimitiveSubAgentsWiring is the GAP 5-DEEPER PART 2 wiring gate (engine side): with subconscious.
// capability AND subconscious.capability_specialists both ON, the engine installs the producing Capability
// as the subconscious engine's SPECIALIST GATE — so the dispatch loop routes each base specialist's
// admission through the Capability (the Capability is the live SPECIALIST-firing ENTRY, subsuming the bare
// relevance firing). With capability_specialists OFF (or capability OFF), NO gate is installed ⇒ the bare
// eff>theta relevance firing, byte-identical to the legacy path. This proves the FEATURE RUNS when the flag
// is on (not merely that the unit exists), and that OFF is the legacy path.
func TestCapabilityPrimitiveSubAgentsWiring(t *testing.T) {
	run := func(capability, specialists bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn (capability_specialists is opt-in ⇒ default OFF even here)
		feat.Subconscious.Capability = capability
		feat.Subconscious.CapabilityPrimitiveSubAgents = specialists
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		e.startEpisode("design and validate a small todo service", true)
		return e
	}

	// BOTH ON: the producing Capability is the live specialist-firing entry — the gate is wired to it.
	both := run(true, true)
	g := both.subconscious.PrimitiveSubAgentGate()
	if g == nil {
		t.Fatal("capability+capability_specialists ON: the dispatch loop must route specialist admission through the Capability (gate must be wired)")
	}
	if both.episodeCap == nil {
		t.Fatal("capability ON: the producing Capability must be retained for the specialist gate")
	}
	if g != subconscious.PrimitiveSubAgentGate(both.episodeCap) {
		t.Fatal("the wired specialist gate must be the producing Capability itself (one entry owns produce + firing)")
	}

	// capability_specialists OFF (capability still ON): the Capability produces the workflow, but the
	// specialists fire on bare relevance — NO gate wired (legacy firing path, byte-identical).
	prodOnly := run(true, false)
	if prodOnly.subconscious.PrimitiveSubAgentGate() != nil {
		t.Fatal("capability_specialists OFF: specialists must fire on bare relevance (no gate ⇒ legacy path, byte-identical)")
	}

	// capability OFF: no producing Capability exists, so the specialist gate cannot be wired regardless.
	none := run(false, true)
	if none.subconscious.PrimitiveSubAgentGate() != nil {
		t.Fatal("capability OFF: no producing Capability ⇒ no specialist gate (the entry needs a producer)")
	}
}
