package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// TestForestFocusSelfDevFloor pins the cross-goal-focus μ floor (§1.8): even when a USER line carries
// the highest value, a μ_min share of focus is reserved for the non-user (self-development) line — so
// E[attention to non-user] ≥ μ_min. With μ_min=0 the selector is the plain argmax; with no non-user
// line bound it also reduces to the argmax. Deterministic under the seeded RNG.
func TestForestFocusSelfDevFloor(t *testing.T) {
	mk := func(mu float64) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Conscious.Activity.Forest = true
		feat.Conscious.Activity.SelfDevFloor = mu
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		return e
	}

	// branch 0 = a high-value USER line; branch 1 = a lower-value non-user (drive) line.
	values := map[int]float64{0: 0.9, 1: 0.2}

	// μ_min = 0 -> always the argmax (the user line).
	e0 := mk(0)
	e0.BindBranchGoal(0, "user goal", false)
	e0.BindDriveBranch(1, "self-development", false)
	for i := 0; i < 50; i++ {
		if got := e0.forestFocus(values); got != 0 {
			t.Fatalf("μ=0: focus=%d, want 0 (argmax)", got)
		}
	}

	// μ_min = 0.3 -> the non-user line is picked SOME of the time, but not the majority (user still wins).
	e := mk(0.3)
	e.BindBranchGoal(0, "user goal", false)
	e.BindDriveBranch(1, "self-development", false)
	const N = 400
	nonUser := 0
	for i := 0; i < N; i++ {
		if e.forestFocus(values) == 1 {
			nonUser++
		}
	}
	frac := float64(nonUser) / float64(N)
	if frac < 0.15 { // a real floor — must clear well above zero (target μ=0.3)
		t.Errorf("μ=0.3: non-user attention share %.3f, want >= ~0.15 (the self-development floor)", frac)
	}
	if frac > 0.5 {
		t.Errorf("μ=0.3: non-user attention share %.3f, want < 0.5 (user line still takes priority)", frac)
	}

	// no non-user line bound -> argmax only, never strays off the user line even with μ>0.
	eU := mk(0.3)
	eU.BindBranchGoal(0, "user goal", false)
	eU.BindBranchGoal(1, "another user goal", false)
	for i := 0; i < 50; i++ {
		if got := eU.forestFocus(values); got != 0 {
			t.Fatalf("no non-user line: focus=%d, want 0 (argmax)", got)
		}
	}
}
