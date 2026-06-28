package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestEngineGoalFeedback pins the engine integration of slice (a.5c): when goal_feedback is on, concluding
// every subgoal of a parent as unmeetable DRIVES the parent's transition and emits a goal event; OFF, the
// engine never propagates. Built directly on the engine's goal set (the actuator is exercised in cognition).
func TestEngineGoalFeedback(t *testing.T) {
	mk := func(on bool) (*Engine, *bool) {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Conscious.Activity.GoalFeedback = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		// a matched parent with one (about-to-be-unmeetable) subgoal, both held in e.goals.
		parent := cognition.NewGoal("P", "build the thing")
		parent.Transition(cognition.GoalMatched)
		child := cognition.NewSubgoal("C", "the only part", &parent)
		parent.Decompose(&child)
		e.goals = append(e.goals, parent, child)
		revised := false
		e.bus.Subscribe(func(ev events.Event) {
			if ev.Kind == string(events.Goal) {
				if r, _ := ev.Data["revised"].(bool); r {
					revised = true
				}
			}
		})
		return e, &revised
	}

	// ON: the parent is revised to refined + an event fires.
	e, revised := mk(true)
	if pid := e.reviseGoalOnUnmeetable("C"); pid != "P" {
		t.Fatalf("goal_feedback ON: revised pid=%q, want \"P\"", pid)
	}
	if !*revised {
		t.Error("goal_feedback ON: expected a goal-feedback event")
	}
	gm := e.goalMap()
	if gm["P"].Status != cognition.GoalRefined {
		t.Errorf("parent status = %s, want refined", gm["P"].Status)
	}

	// OFF: no propagation, no event, parent untouched.
	eOff, revisedOff := mk(false)
	if pid := eOff.reviseGoalOnUnmeetable("C"); pid != "" {
		t.Errorf("goal_feedback OFF: must not propagate (pid=%q)", pid)
	}
	if *revisedOff {
		t.Error("goal_feedback OFF: no event expected")
	}
}
