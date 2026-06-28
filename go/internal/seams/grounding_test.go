package seams

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// stubExecutor is a real-executor stand-in for the watched seam: it returns a successful ToolResult so
// Act takes the REAL path (Fabricated stays false). It never touches the filesystem.
type stubExecutor struct{}

func (stubExecutor) Execute(call action.ToolCall) action.ToolResult {
	return action.ToolResult{Name: call.Name, Content: "ran: 5/5 pass"}
}

// TestFabricatedObservationIsTier0 is the P0.6 gate: with NO executor wired, the watched seam's offline
// stand-in MAKES UP every "reality: ..." observation — so each must be marked Fabricated and must NOT
// be a grounding source (GroundsReality()==false). A fabricated observation can never validate, refute,
// or be stored as ground truth, so it can never reach tier-1.
func TestFabricatedObservationIsTier0(t *testing.T) {
	f := NewFrontActuator(nil) // no executor -> the offline stand-in fabricates
	cases := []types.Intention{
		{Kind: "measure", Text: "run the test suite"},
		{Kind: "run", Text: "make build"},
		{Kind: "reflect", Text: "think it through"},
		{Kind: "send", Text: "ship it"},
		{Kind: "run", Text: "just retry, it should be fixed now"},
		{Kind: "run", Text: "do the arithmetic by hand"},
	}
	for _, in := range cases {
		obs := f.Act(in)
		if !obs.Fabricated {
			t.Fatalf("offline observation for %q must be marked Fabricated (tier-0)", in.Text)
		}
		if obs.GroundsReality() {
			t.Fatalf("a fabricated observation must NOT ground reality: %q", in.Text)
		}
	}
}

// TestRealObservationGrounds is the other side: a REAL executor's result is a genuine observation —
// not fabricated, and a valid grounding source (tier-1 eligible).
func TestRealObservationGrounds(t *testing.T) {
	f := NewFrontActuator(stubExecutor{})
	obs := f.Act(types.Intention{Kind: "measure", Text: "run tests on foo.py"})
	if obs.Fabricated {
		t.Fatal("a real executor's observation must NOT be marked fabricated")
	}
	if !obs.GroundsReality() {
		t.Fatal("a real observation must ground reality (tier-1 eligible)")
	}
}

// TestFabricatedBreadcrumbReachesTheStream proves the tier-0 marker rides onto the OBSERVATION thought
// the watched seam injects into the conscious stream (via RawReturn), so a downstream consumer (the
// grounding loop / experiment memory) can reject it without re-deriving that it was fabricated.
func TestFabricatedBreadcrumbReachesTheStream(t *testing.T) {
	bus, _ := captureBus()
	seam := NewWatchedSeam(NewFrontActuator(nil), bus.Emit)
	thought := seam.OpenToReality(types.Intention{Kind: "measure", Text: "run the suite"})

	obs, ok := thought.RawReturn.(types.Observation)
	if !ok {
		t.Fatalf("the OBSERVATION thought must carry its Observation on RawReturn; got %T", thought.RawReturn)
	}
	if !obs.Fabricated || obs.GroundsReality() {
		t.Fatal("the injected OBSERVATION thought must carry the tier-0 (fabricated, non-grounding) breadcrumb")
	}
}
