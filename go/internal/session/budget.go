// budget.go is per-session resource governance (P3.5): a TOKEN budget per session + tree-wide spend
// monitoring — the cost-units the regulator lacked (it bounds branching to a stability boundary, not
// dollars/tokens). A session that spends past its budget HALTS (a bounded, deterministic stop), and the
// whole spawn tree's spend is observable so a runaway sub-tree is caught.
package session

// Budget caps a session's token spend.
type Budget struct {
	TokenCap int
	Spent    int
}

// Spend debits n tokens.
func (b *Budget) Spend(n int) { b.Spent += n }

// Over reports whether the budget has been exceeded.
func (b *Budget) Over() bool { return b.Spent > b.TokenCap }

// Remaining is how much budget is left (negative once over).
func (b *Budget) Remaining() int { return b.TokenCap - b.Spent }

// RunBudgeted runs the session like Run, but each SCHEDULED step debits the tokens work returns, and the
// session HALTS with BudgetExhausted the moment its token budget is exceeded — the bounded halt. With no
// Budget set it degrades to the lifecycle's own termination. Returns the terminate reason + final tick.
func (s *Session) RunBudgeted(goalMet func(tick int) bool, work func(tick int) (tokens int)) (Terminate, int) {
	budget := s.Spec.TickBudget
	if s.Spec.Horizon == SingleShot && budget <= 0 {
		budget = 1
	}
	for s.life.Tick = 1; s.life.Tick <= budget; s.life.Tick++ {
		t := s.life.Tick
		if s.Spec.scheduledAt(t) {
			if work != nil {
				n := work(t)
				if s.Budget != nil {
					s.Budget.Spend(n)
				}
			}
			s.life.Steps++
			if s.Budget != nil && s.Budget.Over() {
				s.life.Terminated = BudgetExhausted
				return BudgetExhausted, t
			}
			if s.Spec.Horizon == SingleShot {
				s.life.Terminated = GoalMet
				return GoalMet, t
			}
		}
		if goalMet != nil && goalMet(t) {
			s.life.Terminated = GoalMet
			return GoalMet, t
		}
	}
	s.life.Terminated = BudgetExhausted
	return BudgetExhausted, budget
}

// TreeTokensSpent sums the token spend across the whole spawn tree (tree-wide memory monitoring) — what
// a tree-budget governor watches to catch a runaway sub-tree.
func (s *Session) TreeTokensSpent() int {
	total := 0
	if s.Budget != nil {
		total = s.Budget.Spent
	}
	for _, c := range s.Children {
		total += c.TreeTokensSpent()
	}
	return total
}
