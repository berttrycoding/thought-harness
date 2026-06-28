package scenarios

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// newHeuristicEngine builds an engine on the explicit TestBackend test double (the path the
// oracle pins; never the product path), wired exactly as NewEngine does.
func newHeuristicEngine(t *testing.T, mode string) *engine.Engine {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = mode
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// TestTableIsS1ThroughS16 confirms the ordered table holds exactly the 16 scenarios in insertion
// order, and that byID resolves each — the structural parity with the Python SCENARIOS dict.
func TestTableIsS1ThroughS16(t *testing.T) {
	all := All()
	if len(all) != 16 {
		t.Fatalf("len(All()) = %d, want 16", len(all))
	}
	want := []string{
		"S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8",
		"S9", "S10", "S11", "S12", "S13", "S14", "S15", "S16",
	}
	for i, id := range want {
		if all[i].ID != id {
			t.Fatalf("All()[%d].ID = %q, want %q (insertion order must match Python)", i, all[i].ID, id)
		}
		if _, ok := Get(id); !ok {
			t.Fatalf("Get(%q) missing", id)
		}
	}
	// All() returns a copy: mutating it must not corrupt the package table.
	all[0].ID = "MUTATED"
	if again, _ := Get("S1"); again.ID != "S1" {
		t.Fatal("All() must return a defensive copy")
	}
}

// TestGetCaseInsensitiveAndUnknown checks the case-insensitive lookup and the unknown-id error
// (Python's name.upper() + KeyError surface).
func TestGetCaseInsensitiveAndUnknown(t *testing.T) {
	if sc, ok := Get("s5"); !ok || sc.ID != "S5" {
		t.Fatalf("Get(\"s5\") = (%v, %v), want S5,true", sc.ID, ok)
	}
	if _, ok := Get("S99"); ok {
		t.Fatal("Get(\"S99\") should be (_, false)")
	}
	_, err := RunScenario("S99", newHeuristicEngine(t, "reactive"))
	if err == nil {
		t.Fatal("RunScenario on an unknown id should error")
	}
	if _, ok := err.(*UnknownScenarioError); !ok {
		t.Fatalf("error = %T, want *UnknownScenarioError", err)
	}
}

// TestRunScenarioDrivesEngine drives a couple of representative scenarios on the heuristic backend
// and asserts the runner completes without panicking and advances the bus tick — the integration
// smoke that the scripted submits + inject + idle-drive loop wire correctly to the live engine.
func TestRunScenarioDrivesEngine(t *testing.T) {
	for _, id := range []string{"S1", "S7", "S11", "S14"} {
		sc, _ := Get(id)
		eng := newHeuristicEngine(t, sc.Mode)
		out, err := RunScenario(id, eng)
		if err != nil {
			t.Fatalf("RunScenario(%s): %v", id, err)
		}
		if out != eng {
			t.Fatalf("RunScenario(%s) should return the supplied engine", id)
		}
		if out.Bus().Tick <= 0 {
			t.Fatalf("RunScenario(%s) should have advanced the bus tick", id)
		}
	}
}

// TestRunScenarioFreshEngine confirms eng=nil builds a fresh engine from config (substrate=test
// so the offline test path resolves without a model), and that it runs.
func TestRunScenarioFreshEngine(t *testing.T) {
	t.Setenv("THOUGHT_SUBSTRATE", "test")
	out, err := RunScenario("S1", nil)
	if err != nil {
		t.Fatalf("RunScenario(S1, nil): %v", err)
	}
	if out == nil || out.Bus().Tick <= 0 {
		t.Fatal("a fresh-engine scenario run should advance the tick")
	}
}
