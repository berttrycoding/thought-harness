package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestSoftPolicy is the graded-eval floor for slice (d): with the soft policy OFF (the default) the
// THINK-default zone returns THINK unchanged; with it ON at a high temperature, repeated decisions on
// the same calm THINK-zone graph produce a spread of moves — the conscious is now probabilistically
// active instead of flat-linear, tunable by τ.
func TestSoftPolicy(t *testing.T) {
	// a calm graph: 3 GENERATED thoughts @0.6 — no conflict / exhaustion / merge → the hard ladder = THINK.
	build := func() *graph.ThoughtGraph {
		g := graph.New("ponder the design")
		for _, txt := range []string{"first angle", "second angle", "third angle"} {
			appendT(g, txt, types.GENERATED, 0.6)
		}
		return g
	}

	// soft OFF (default): always THINK, deterministically.
	off := config.DefaultConsciousActivity()
	cOff := NewController(noEmit, nil, "control", nil)
	cOff.SetActivityConfig(&off)
	cOff.SetRNG(cpyrand.New(1))
	for i := 0; i < 20; i++ {
		if d := cOff.DecideNext(build(), dec(false, false)); d != types.THINK {
			t.Fatalf("soft OFF: got %v, want THINK", d)
		}
	}

	// soft ON, high τ: some non-THINK discretionary moves appear over 40 samples (seeded → deterministic).
	on := config.DefaultConsciousActivity()
	on.Soft = true
	on.Temperature = 0.8
	cOn := NewController(noEmit, nil, "control", nil)
	cOn.SetActivityConfig(&on)
	cOn.SetRNG(cpyrand.New(1))
	nonThink := 0
	for i := 0; i < 40; i++ {
		if d := cOn.DecideNext(build(), dec(false, false)); d != types.THINK {
			nonThink++
		}
	}
	if nonThink == 0 {
		t.Fatal("soft ON, high τ: expected some non-THINK moves over 40 samples, got 0 (policy inactive)")
	}
}
