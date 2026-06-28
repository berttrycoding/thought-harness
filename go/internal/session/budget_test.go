package session

import "testing"

// TestSessionOverBudgetHalts is the P3.5 gate: a session that spends past its token budget HALTS
// (bounded, deterministic) — it does not run to its tick budget.
func TestSessionOverBudgetHalts(t *testing.T) {
	s, _ := NewSession("expensive work", Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Continuous}, TickBudget: 100})
	s.Budget = &Budget{TokenCap: 100}

	// each step costs 40 tokens; after 3 steps (120 > 100) the budget is blown.
	term, endTick := s.RunBudgeted(func(int) bool { return false }, func(int) int { return 40 })

	if term != BudgetExhausted {
		t.Fatalf("an over-budget session must halt with BudgetExhausted; got %v", term)
	}
	if endTick != 3 {
		t.Fatalf("the halt should be at the step that blew the budget (tick 3), not the tick cap; got %d", endTick)
	}
	if s.Budget.Spent != 120 || !s.Budget.Over() {
		t.Fatalf("the budget should be over (spent=%d, over=%v)", s.Budget.Spent, s.Budget.Over())
	}
}

// TestSessionWithinBudgetTerminatesNormally: a session that stays within budget ends on goal/budget, not
// a budget halt.
func TestSessionWithinBudgetTerminatesNormally(t *testing.T) {
	s, _ := NewSession("cheap work", Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Continuous}, TickBudget: 5})
	s.Budget = &Budget{TokenCap: 1000}
	term, _ := s.RunBudgeted(func(int) bool { return false }, func(int) int { return 10 })
	if term != BudgetExhausted { // here BudgetExhausted means the TICK budget ran out, not the token budget
		t.Fatalf("a within-token-budget session should end by its tick budget; got %v", term)
	}
	if s.Budget.Over() {
		t.Fatalf("the token budget should NOT be over; spent=%d cap=%d", s.Budget.Spent, s.Budget.TokenCap)
	}
}

// TestTreeTokensMonitored: the whole spawn tree's token spend is observable (tree-wide monitoring), so a
// runaway sub-tree is visible to a governor.
func TestTreeTokensMonitored(t *testing.T) {
	root, _ := NewSession("root", Spec{Horizon: Bounded, Schedule: Schedule{Kind: OnDemand}, TickBudget: 4})
	root.Budget = &Budget{TokenCap: 1000, Spent: 30}
	child, _ := root.Dispatch("child", Spec{Horizon: Bounded, Schedule: Schedule{Kind: OnDemand}, TickBudget: 4})
	child.Budget = &Budget{TokenCap: 1000, Spent: 70}
	grand, _ := child.Dispatch("grandchild", Spec{Horizon: Bounded, Schedule: Schedule{Kind: OnDemand}, TickBudget: 4})
	grand.Budget = &Budget{TokenCap: 1000, Spent: 100}

	if got := root.TreeTokensSpent(); got != 200 {
		t.Fatalf("tree-wide spend should sum the whole tree (30+70+100=200); got %d", got)
	}
}
