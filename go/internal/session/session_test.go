package session

import "testing"

func boundedSpec() Spec {
	return Spec{Horizon: Bounded, Schedule: Schedule{Kind: OnDemand}, TickBudget: 8}
}

// TestSessionSpawnTreeBounded is the P3.3 gate (wfproof-style): a Session can Dispatch child sessions,
// but the spawn tree is BOUNDED — a dispatch chain is accepted up to MaxSessionDepth and rejected one
// past it, so the tree is always finite.
func TestSessionSpawnTreeBounded(t *testing.T) {
	root, err := NewSession("root goal", boundedSpec())
	if err != nil {
		t.Fatalf("root session: %v", err)
	}

	// dispatch a chain exactly to the depth bound — every hop accepted.
	cur := root
	for d := 1; d <= MaxSessionDepth; d++ {
		child, err := cur.Dispatch("subgoal", boundedSpec())
		if err != nil {
			t.Fatalf("dispatch at depth %d should be accepted: %v", d, err)
		}
		cur = child
	}
	// one hop past the bound — rejected (the tree stays bounded).
	if _, err := cur.Dispatch("too deep", boundedSpec()); err == nil {
		t.Fatalf("a dispatch past MaxSessionDepth=%d must be rejected", MaxSessionDepth)
	}
	if got := root.MaxDepthReached(); got != MaxSessionDepth {
		t.Fatalf("max depth reached = %d, want %d", got, MaxSessionDepth)
	}
	if root.TreeSize() != MaxSessionDepth+1 {
		t.Fatalf("tree size = %d, want %d", root.TreeSize(), MaxSessionDepth+1)
	}
}

// TestSessionRunsAndTerminates: a bounded session runs its workflow and terminates (the lifecycle
// guarantee carries through). A child with an invalid (immortal) lifecycle is rejected at dispatch.
func TestSessionRunsAndTerminates(t *testing.T) {
	root, _ := NewSession("do the thing", boundedSpec())
	steps := 0
	term, _ := root.Run(func(int) bool { return false }, func(int) { steps++ })
	if term == NotTerminated {
		t.Fatal("a bounded session must terminate")
	}
	if steps == 0 {
		t.Fatal("the session should have run its step")
	}

	// dispatching a child with no termination budget (immortal) is rejected.
	immortal := Spec{Horizon: LongHorizon, Schedule: Schedule{Kind: Continuous}, TickBudget: 0}
	if _, err := root.Dispatch("watcher", immortal); err == nil {
		t.Fatal("dispatching an immortal (no-budget) child must be rejected")
	}
}

// TestRootSessionGeneralizesSubAgent: the simplest setting — a single-shot on-demand session — is the
// old SubAgent (one move, gone), proving Session is the generalization, not a replacement.
func TestRootSessionGeneralizesSubAgent(t *testing.T) {
	subagent, err := NewSession("one move", Spec{Horizon: SingleShot, Schedule: Schedule{Kind: OnDemand}})
	if err != nil {
		t.Fatalf("single-shot session should be valid: %v", err)
	}
	moves := 0
	term, tick := subagent.Run(nil, func(int) { moves++ })
	if term != GoalMet || tick != 1 || moves != 1 {
		t.Fatalf("a single-shot session does exactly one move then ends; term=%v tick=%d moves=%d", term, tick, moves)
	}
}
