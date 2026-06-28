package cognition

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// The Awake panel must surface the full continuous-mode picture: arousal transitions, the awake
// decision policy, endogenous (drives/default-mode) activity, and — distinctly — proactive outreach,
// which a reactive reply must NOT be confused with (E1).
func TestRenderContinuousSurfacesAwakeSignals(t *testing.T) {
	vm := ViewModel{
		Width: 72,
		Snap:  SnapshotData{Mode: "continuous", Arousal: "AWAKE"},
		Events: []events.Event{
			{Kind: events.Arousal, Summary: "DROWSY -> AWAKE (lull=0)", Data: map[string]any{"to": "AWAKE"}},
			{Kind: events.ContinuousDecision, Summary: "awake decision: CURIOSITY_GOAL", Data: map[string]any{}},
			{Kind: events.Port, Summary: "default-mode: associating X with Y", Data: map[string]any{}},
			{Kind: events.Respond, Summary: "(unprompted) you might also consider Z", Data: map[string]any{"kind": "outreach", "proactive": true, "value": 0.71}},
			{Kind: events.Respond, Summary: "a normal reactive reply", Data: map[string]any{}}, // must NOT show as outreach
		},
	}
	body := renderContinuous(vm).Body
	for _, want := range []string{"arousal transitions", "awake decision", "CURIOSITY_GOAL", "endogenous activity", "proactive outreach", "you might also consider Z", "V=0.71"} {
		if !strings.Contains(body, want) {
			t.Errorf("awake panel missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "a normal reactive reply") {
		t.Errorf("a plain reply leaked into the outreach section:\n%s", body)
	}
}

// In reactive mode (no awake events) the panel says the regime is dormant rather than rendering empty.
func TestRenderContinuousReactiveDormant(t *testing.T) {
	vm := ViewModel{Width: 72, Snap: SnapshotData{Mode: "reactive", Arousal: "AWAKE"}}
	body := renderContinuous(vm).Body
	if !strings.Contains(body, "dormant") {
		t.Fatalf("reactive panel should note the dormant awake regime:\n%s", body)
	}
}
