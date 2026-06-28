package seams

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// recordingExecutor records the ToolCall it received so a test can see WHICH tool the bridge dispatched.
type recordingExecutor struct{ lastCall action.ToolCall }

func (r *recordingExecutor) Execute(call action.ToolCall) action.ToolResult {
	r.lastCall = call
	return action.ToolResult{Name: call.Name, Content: "ran " + call.Name}
}

// TestStructuredIntentionReachesAnyTool is the N.4 gate: a structured intention (Tool+Args, formed at
// the decision point) dispatches the named tool DIRECTLY — so a tool the regex scraper can't reach
// (read_file, search, edit_file, …) is now reachable. The bridge path is recorded as "structured".
func TestStructuredIntentionReachesAnyTool(t *testing.T) {
	rec := &recordingExecutor{}
	f := NewFrontActuator(rec)

	// read_file is NOT one of the scraper's two tools (run_tests / run_shell) — only structured reaches it.
	obs := f.Act(types.Intention{Kind: "measure", Tool: "read_file", Args: map[string]any{"path": "engine.go"}})
	if rec.lastCall.Name != "read_file" {
		t.Fatalf("a structured intention must dispatch its named tool directly; dispatched %q", rec.lastCall.Name)
	}
	if rec.lastCall.Args["path"] != "engine.go" {
		t.Fatalf("the structured args must reach the tool; got %v", rec.lastCall.Args)
	}
	if obs.Bridge != "structured" {
		t.Fatalf("the bridge path should be 'structured'; got %q", obs.Bridge)
	}
}

// TestUnstructuredIntentionScrapes: with no Tool set, the seam falls back to the regex scraper (the
// offline path), tagged "scraped".
func TestUnstructuredIntentionScrapes(t *testing.T) {
	rec := &recordingExecutor{}
	f := NewFrontActuator(rec)
	obs := f.Act(types.Intention{Kind: "measure", Text: "run the test suite"})
	if rec.lastCall.Name != "run_tests" {
		t.Fatalf("the scraper should map a measure intention to run_tests; got %q", rec.lastCall.Name)
	}
	if obs.Bridge != "scraped" {
		t.Fatalf("the bridge path should be 'scraped'; got %q", obs.Bridge)
	}
}

// TestBridgeMissIsVisible: an intention neither structured nor scrapable produces an explicit "none"
// bridge marker — the grounding-bridge failure is visible, never a silent drop.
func TestBridgeMissIsVisible(t *testing.T) {
	rec := &recordingExecutor{}
	f := NewFrontActuator(rec)
	obs := f.Act(types.Intention{Kind: "reflect", Text: "just think it through"})
	if obs.Bridge != "none" {
		t.Fatalf("an unmappable intention must record a visible bridge miss ('none'); got %q", obs.Bridge)
	}
	// and a missed bridge is NOT grounding (it fell to the fabricated stand-in, P0.6).
	if obs.GroundsReality() {
		t.Fatal("a bridge miss falls to the stand-in and must not ground reality")
	}
}

// TestNoExecutorIsBridgeNone: with no executor at all, the bridge is "none" (no real path to reality).
func TestNoExecutorIsBridgeNone(t *testing.T) {
	obs := NewFrontActuator(nil).Act(types.Intention{Kind: "measure", Text: "run the suite"})
	if obs.Bridge != "none" {
		t.Fatalf("no executor must mark the bridge 'none'; got %q", obs.Bridge)
	}
}
