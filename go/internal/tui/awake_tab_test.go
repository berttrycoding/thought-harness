package tui

import "testing"

// The continuous panel must live in its own Awake tab (E1), not buried in Systems, and the tab must
// exist in the strip and claim the panel via railPanelsFor.
func TestAwakeTabOwnsContinuous(t *testing.T) {
	if got := tabForPanel("continuous"); got != "awake" {
		t.Fatalf("tabForPanel(continuous) = %q, want awake", got)
	}
	found := false
	for _, s := range cognitionTabs {
		if s.id == "awake" {
			found = true
		}
	}
	if !found {
		t.Fatal("no Awake tab in cognitionTabs")
	}
	if !contains(railPanelsFor("awake"), "continuous") {
		t.Fatalf("railPanelsFor(awake) = %v, missing continuous", railPanelsFor("awake"))
	}
	// it must NOT also land in Systems.
	if contains(railPanelsFor("systems"), "continuous") {
		t.Fatal("continuous still leaks into the Systems tab")
	}
}
