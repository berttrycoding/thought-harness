package session

import "testing"

// TestEveryScheduleRunsAndTerminates is the P3.8 gate: each schedule type runs (does its steps) and is
// GUARANTEED to terminate, all in ticks. Run with a goal that never fires, every standing schedule must
// still terminate by budget; with a goal that fires, it terminates on goal_met.
func TestEveryScheduleRunsAndTerminates(t *testing.T) {
	never := func(int) bool { return false }

	cases := []struct {
		name      string
		spec      Spec
		wantSteps int // steps over a 12-tick budget with the goal never met
	}{
		{"on_demand/single_shot", Spec{Horizon: SingleShot, Schedule: Schedule{Kind: OnDemand}}, 1},
		{"heartbeat(3)/long", Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Heartbeat, Period: 3}, TickBudget: 12}, 4},
		{"async(2)/bounded", Spec{Horizon: Bounded, Schedule: Schedule{Kind: Async, Latency: 2}, TickBudget: 12}, 2},
		{"continuous/long", Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Continuous}, TickBudget: 12}, 12},
	}
	for _, c := range cases {
		l := New(c.spec)
		term, endTick := l.Run(never, nil)
		if term == NotTerminated {
			t.Fatalf("[%s] must terminate, not run forever", c.name)
		}
		if c.spec.Horizon == SingleShot {
			if term != GoalMet {
				t.Errorf("[%s] single-shot terminates after its one move; got %v", c.name, term)
			}
		} else if term != BudgetExhausted {
			t.Errorf("[%s] a standing session with an unmet goal must terminate by budget; got %v at tick %d",
				c.name, term, endTick)
		}
		if l.Steps != c.wantSteps {
			t.Errorf("[%s] ran %d steps, want %d", c.name, l.Steps, c.wantSteps)
		}
	}
}

// TestGoalMetTerminatesEarly: a continuous session whose goal is met at tick 4 stops at tick 4 (not at
// the budget) — termination is the earliest of goal_met / budget.
func TestGoalMetTerminatesEarly(t *testing.T) {
	l := New(Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Continuous}, TickBudget: 100})
	term, end := l.Run(func(tick int) bool { return tick >= 4 }, nil)
	if term != GoalMet || end != 4 {
		t.Fatalf("a met goal should terminate early at tick 4; got %v at %d", term, end)
	}
}

// TestImmortalSessionRejected is the durability invariant: a multi-tick / standing session with no tick
// budget is INVALID (an immortal agent is a durability hazard).
func TestImmortalSessionRejected(t *testing.T) {
	bad := Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Continuous}, TickBudget: 0}
	if ok, _ := bad.Valid(); ok {
		t.Fatal("a long-horizon/continuous session with no TickBudget must be rejected (no guaranteed termination)")
	}
	// a heartbeat with no period, and an async with negative latency, are also invalid.
	if ok, _ := (Spec{Horizon: Bounded, Schedule: Schedule{Kind: Heartbeat, Period: 0}, TickBudget: 5}).Valid(); ok {
		t.Fatal("a heartbeat with period 0 must be rejected")
	}
	// a single-shot on-demand needs no budget (it is capped at one move).
	if ok, _ := (Spec{Horizon: SingleShot, Schedule: Schedule{Kind: OnDemand}}).Valid(); !ok {
		t.Fatal("a single-shot on-demand session is valid without a tick budget")
	}
}
