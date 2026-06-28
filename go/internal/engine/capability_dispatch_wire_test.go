package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// TestCapabilityDispatchWiring is the GAP 5-DEEPER wiring gate (engine side): with subconscious.
// capability AND subconscious.capability_dispatch both ON, the engine installs the producing Capability
// as the subconscious engine's RECOGNIZER — so the dispatch loop routes its per-tick workflow-recognition
// through the Capability (the Capability is the live relevance/dispatch ENTRY, subsuming the Workflow
// self-trigger). With capability_dispatch OFF (or capability OFF), NO recognizer is installed ⇒ the
// Workflow self-triggers, byte-identical to the legacy path. This proves the FEATURE RUNS when the flag
// is on (not merely that the unit exists), and that OFF is the legacy path.
func TestCapabilityDispatchWiring(t *testing.T) {
	run := func(capability, dispatch bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Subconscious.Capability = capability
		feat.Subconscious.CapabilityDispatch = dispatch
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		// a goal the synthesiser shapes into a workflow (so a Capability is produced + retained).
		e.startEpisode("design and validate a small todo service", true)
		return e
	}

	// BOTH ON: the producing Capability is the live entry — the recognizer is wired to it.
	both := run(true, true)
	rec := both.subconscious.Recognizer()
	if rec == nil {
		t.Fatal("capability+capability_dispatch ON: the dispatch loop must route recognition through the Capability (recognizer must be wired)")
	}
	if both.episodeCap == nil {
		t.Fatal("capability ON: the producing Capability must be retained for the dispatch entry")
	}
	// The recognizer IS the producing Capability (the entry that produced the workflow owns its trigger).
	if rec != subconscious.WorkflowRecognizer(both.episodeCap) {
		t.Fatal("the wired recognizer must be the producing Capability itself (one entry owns produce + trigger)")
	}

	// capability_dispatch OFF (capability still ON): the Capability produces the workflow, but the
	// Workflow self-triggers — NO recognizer wired (legacy dispatch path, byte-identical).
	prodOnly := run(true, false)
	if prodOnly.subconscious.Recognizer() != nil {
		t.Fatal("capability_dispatch OFF: the Workflow must self-trigger (no recognizer ⇒ legacy path, byte-identical)")
	}

	// capability OFF: no producing Capability exists, so the dispatch entry cannot be wired regardless.
	none := run(false, true)
	if none.subconscious.Recognizer() != nil {
		t.Fatal("capability OFF: no producing Capability ⇒ no recognizer (the entry needs a producer)")
	}
	if none.episodeCap != nil {
		t.Fatal("capability OFF: no producing Capability is retained")
	}
}
