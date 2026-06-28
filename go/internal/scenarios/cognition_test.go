package scenarios

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// TestCognitionGraphAccessorPopulated is the X.5 gate: the unified cross-layer CognitionGraph — built
// live from the bus but previously an orphan with no read accessor — is now reachable via
// Engine.CognitionGraph() and is populated after a run, spanning multiple layers including action.
func TestCognitionGraphAccessorPopulated(t *testing.T) {
	sc, ok := Get("S5") // exercises the Watched Seam / Action → the richest cross-layer graph
	if !ok {
		t.Fatal("scenario S5 missing")
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = sc.Mode
	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunScenario("S5", eng); err != nil {
		t.Fatal(err)
	}

	cg := eng.CognitionGraph()
	if cg == nil {
		t.Fatal("Engine.CognitionGraph() returned nil")
	}
	s := cg.Stats()
	if s.Nodes == 0 || s.Edges == 0 {
		t.Fatalf("cognition graph is empty after a run — the asset is not wired/accessible (%+v)", s)
	}
	if len(s.ByLayer) < 4 {
		t.Fatalf("the cross-layer model should span >=4 layers; got %v", s.ByLayer)
	}
	if s.ByLayer["action"] == 0 {
		t.Fatalf("S5 acts on reality — expected action-layer entities; got %v", s.ByLayer)
	}
	if cg.Summary() == "" {
		t.Fatal("Summary() should render a non-empty cross-layer line")
	}
}
