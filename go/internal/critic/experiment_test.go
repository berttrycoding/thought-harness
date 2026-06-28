package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/convert"
)

// TestExperimentWindowKeepRevert pins slice (h) / §5.3: the activity-θ outer loop accumulates a window of
// returns, scores J = the mean, and keep-or-reverts the θ snapshot — KEEP (inner-loop drift stands) iff J
// strictly beats the best window so far, else REVERT θ to the snapshot. winLen is 3.
func TestExperimentWindowKeepRevert(t *testing.T) {
	mk := func() (*Controller, *config.ConsciousActivityCfg) {
		a := config.DefaultConsciousActivity()
		c := NewController(noEmit, nil, "control", nil)
		c.SetActivityConfig(&a)
		return c, &a
	}

	// --- window 1: a strong window (mean 1.0 > floor 0) KEEPS; the inner-loop θ drift is locked in ---
	c, a := mk()
	a.BranchPropensity = 1.0
	c.ExperimentWindow(1.0) // episode 1 — snapshots θ (β=1.0) on window open
	a.BranchPropensity = 1.5
	c.ExperimentWindow(1.0)              // episode 2 — inner-loop drift to 1.5
	closed, d := c.ExperimentWindow(1.0) // episode 3 — window closes (winLen=3)
	if !closed || d != convert.Keep {
		t.Fatalf("strong window: want (closed, keep), got (%v, %s)", closed, d)
	}
	if a.BranchPropensity != 1.5 { // Keep does NOT restore — the drift stands
		t.Errorf("Keep must retain the drifted θ, β=%v want 1.5", a.BranchPropensity)
	}

	// --- window 2: a weak window (mean 0.0, NOT > J_best 1.0) REVERTS θ to its window-open snapshot ---
	c.ExperimentWindow(0.0) // episode 1 — snapshots θ (β=1.5) on window open
	a.BranchPropensity = 9.9
	c.ExperimentWindow(0.0)             // episode 2 — inner-loop drift to 9.9
	closed, d = c.ExperimentWindow(0.0) // episode 3 — window closes
	if !closed || d != convert.Revert {
		t.Fatalf("weak window: want (closed, revert), got (%v, %s)", closed, d)
	}
	if a.BranchPropensity != 1.5 { // Revert restores to the snapshot taken at window open
		t.Errorf("Revert must restore θ to the snapshot, β=%v want 1.5", a.BranchPropensity)
	}
}

// TestExperimentWindowNoActivity is the safety no-op: without an activity config, ExperimentWindow never
// closes a window or panics.
func TestExperimentWindowNoActivity(t *testing.T) {
	c := NewController(noEmit, nil, "control", nil)
	if closed, _ := c.ExperimentWindow(1.0); closed {
		t.Error("no activity config: a window must never close")
	}
}
