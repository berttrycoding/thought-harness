package cognition

import "testing"

func TestGoalDefaults(t *testing.T) {
	g := NewGoal("g1", "ship the feature")
	if g.Source != GoalUser || g.Status != GoalOpen || g.Level != 0 || !g.IsTopGoal() {
		t.Fatalf("NewGoal defaults wrong: %+v", g)
	}
	if g.Acceptance != nil || g.Parent != nil || len(g.Children) != 0 {
		t.Fatalf("NewGoal should have nil acceptance/parent and no children: %+v", g)
	}
}

func TestGoalLifecycleTransitions(t *testing.T) {
	g := NewGoal("g1", "x")

	// legal path: open -> matched -> active -> done.
	for _, to := range []GoalStatus{GoalMatched, GoalActive, GoalDone} {
		if !g.Transition(to) {
			t.Fatalf("legal transition to %s rejected (from prior state)", to)
		}
	}
	if g.Status != GoalDone {
		t.Fatalf("ended at %s, want done", g.Status)
	}
	if !g.Status.IsTerminal() {
		t.Fatal("done should be terminal")
	}

	// no transitions out of a terminal state.
	if g.Transition(GoalActive) {
		t.Fatal("transition out of terminal done should be rejected")
	}

	// an illegal skip (open -> done) is rejected.
	g2 := NewGoal("g2", "y")
	if g2.Transition(GoalDone) {
		t.Fatal("open -> done should be illegal (must pass through active)")
	}
	if g2.Status != GoalOpen {
		t.Fatalf("rejected transition mutated status to %s", g2.Status)
	}

	// abandon/supersede are reachable from open/matched/active.
	for _, from := range []GoalStatus{GoalOpen, GoalMatched, GoalActive} {
		if !CanTransition(from, GoalAbandoned) || !CanTransition(from, GoalSuperseded) {
			t.Errorf("%s should be able to abandon/supersede", from)
		}
	}
	// refined is reachable from matched/active but not open.
	if CanTransition(GoalOpen, GoalRefined) {
		t.Error("open -> refined should be illegal")
	}
	if !CanTransition(GoalActive, GoalRefined) {
		t.Error("active -> refined should be legal")
	}
}

func TestSubgoalTree(t *testing.T) {
	parent := NewGoal("p", "build a house")
	child := NewSubgoal("c", "pour the foundation", &parent)
	parent.Decompose(&child)

	if child.Source != GoalSubgoal || child.Level != 1 || child.IsTopGoal() {
		t.Fatalf("subgoal wrong: %+v", child)
	}
	if child.Parent == nil || *child.Parent != "p" {
		t.Fatalf("subgoal parent not set to p: %+v", child.Parent)
	}
	if len(parent.Children) != 1 || parent.Children[0] != "c" {
		t.Fatalf("decompose did not record the child: %+v", parent.Children)
	}
}

func TestAcceptance(t *testing.T) {
	g := NewGoal("g", "make tests pass")
	g.Acceptance = &Acceptance{Kind: AcceptMarker, Predicate: "tests pass"}
	if g.Acceptance.Kind != AcceptMarker || g.Acceptance.Predicate != "tests pass" {
		t.Fatalf("acceptance not stored: %+v", g.Acceptance)
	}
	// the outcome enum is distinct (pending != met != unmeetable).
	if AcceptancePending == AcceptanceMet || AcceptanceMet == AcceptanceUnmeetable {
		t.Fatal("acceptance outcomes must be distinct")
	}
}

func TestAuthorAcceptance(t *testing.T) {
	if a := AuthorAcceptance("make the unit tests pass", GoalUser); a.Kind != AcceptMarker || a.Predicate == "" {
		t.Fatalf("marker goal: got %+v, want AcceptMarker with a predicate", a)
	}
	if b := AuthorAcceptance("tell me about the history of rome", GoalUser); b.Kind != AcceptUserConfirm {
		t.Fatalf("fuzzy goal: got %+v, want AcceptUserConfirm", b)
	}
}

func TestConclude(t *testing.T) {
	g := NewGoal("g", "x")
	g.Conclude(true)
	if g.Status != GoalDone {
		t.Fatalf("Conclude(met) = %s, want done", g.Status)
	}
	g2 := NewGoal("g2", "y")
	g2.Conclude(false)
	if g2.Status != GoalAbandoned {
		t.Fatalf("Conclude(!met) = %s, want abandoned", g2.Status)
	}
	g.Conclude(false) // idempotent on a terminal goal
	if g.Status != GoalDone {
		t.Fatalf("Conclude on terminal mutated status to %s", g.Status)
	}
}

func TestGoalSourceRoundTrip(t *testing.T) {
	for _, s := range []GoalSource{GoalUser, GoalDrive, GoalSubgoal} {
		if got, ok := ParseGoalSource(s.String()); !ok || got != s {
			t.Errorf("round-trip %s failed: got %v ok=%v", s, got, ok)
		}
	}
}
