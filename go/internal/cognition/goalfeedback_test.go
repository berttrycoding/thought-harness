package cognition

import "testing"

// TestPropagateUnmeetable pins the goal feedback propagation (slice a.5c, §1.9): an unmeetable child is
// abandoned; the parent revises only once ALL its children are terminal (the low-pass accumulation).
func TestPropagateUnmeetable(t *testing.T) {
	parent := NewGoal("p", "build the thing")
	c1 := NewSubgoal("c1", "part one", &parent)
	c2 := NewSubgoal("c2", "part two", &parent)
	parent.Decompose(&c1)
	parent.Decompose(&c2)
	goals := map[string]*Goal{"p": &parent, "c1": &c1, "c2": &c2}

	// one child unmeetable → not yet (c2 still open); the child is abandoned.
	if revise, _ := PropagateUnmeetable(goals, "c1"); revise {
		t.Fatal("revise=true after only one of two children unmeetable")
	}
	if c1.Status != GoalAbandoned {
		t.Fatalf("c1 = %s, want abandoned", c1.Status)
	}

	// the second child unmeetable → all terminal → the parent should revise.
	revise, pid := PropagateUnmeetable(goals, "c2")
	if !revise || pid != "p" {
		t.Fatalf("revise=%v pid=%q, want true/\"p\" once all children are unmeetable", revise, pid)
	}

	// an unknown child is a safe no-op.
	if revise, _ := PropagateUnmeetable(goals, "nope"); revise {
		t.Fatal("unknown child returned revise=true")
	}
}

// TestReviseOnUnmeetable pins the feedback ACTUATOR (slice a.5c, §1.9): once all children are unmeetable,
// the parent's status is DRIVEN — a matched/active parent → refined; an open parent → abandoned.
func TestReviseOnUnmeetable(t *testing.T) {
	// matched parent -> refined (a sharper pursuit is owed).
	mp := NewGoal("p", "build")
	mp.Transition(GoalMatched)
	mc := NewSubgoal("c", "only part", &mp)
	mp.Decompose(&mc)
	goals := map[string]*Goal{"p": &mp, "c": &mc}
	pid, st := ReviseOnUnmeetable(goals, "c")
	if pid != "p" || st != GoalRefined {
		t.Fatalf("matched parent: got (%q,%s), want (\"p\",refined)", pid, st)
	}

	// open parent -> abandoned (refined is unreachable from open; no feasible child).
	op := NewGoal("q", "explore")
	oc := NewSubgoal("d", "only part", &op)
	op.Decompose(&oc)
	g2 := map[string]*Goal{"q": &op, "d": &oc}
	pid2, st2 := ReviseOnUnmeetable(g2, "d")
	if pid2 != "q" || st2 != GoalAbandoned {
		t.Fatalf("open parent: got (%q,%s), want (\"q\",abandoned)", pid2, st2)
	}

	// signal not yet accumulated -> no transition.
	np := NewGoal("r", "two parts")
	a := NewSubgoal("a", "one", &np)
	b := NewSubgoal("b", "two", &np)
	np.Transition(GoalMatched)
	np.Decompose(&a)
	np.Decompose(&b)
	g3 := map[string]*Goal{"r": &np, "a": &a, "b": &b}
	if pid3, _ := ReviseOnUnmeetable(g3, "a"); pid3 != "" {
		t.Fatalf("partial: parent transitioned early (pid=%q)", pid3)
	}
	if np.Status != GoalMatched {
		t.Fatalf("partial: parent moved off matched to %s", np.Status)
	}
}
