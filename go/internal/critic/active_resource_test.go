package critic

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// A-RAG4: V(s)-triggered active re-sourcing — the Controller's epistemic re-source DECISION.
//
// These are mutation-sensitive unit tests on the decision math (Pattern A, no model). They assert the
// THINKING the spec intends: re-source PRECISELY when a goal-relevant line is uncertain (low V(s)), and
// NOT otherwise — never on the default-OFF flag, never on a high-V line, never on an idle tangent, and
// at most ONCE per branch (the bound that keeps the plant subcritical). Each NEGATIVE case would pass if
// the corresponding guard were dropped, so a regression that loosens the trigger fails here.

// resourceCtrl builds a Controller with the controller.active_resource gate set to `on` and an emit that
// captures events, plus a graph whose active node has the given text + active-branch value.
func resourceCtrl(t *testing.T, on bool, goal, nodeText string, branchValue float64) (*Controller, *[]capturedEvent, *graph.ThoughtGraph) {
	t.Helper()
	emit, log := newCapture()
	ctrl := NewController(emit, nil, "control", nil)
	enabled := on
	gate := config.NewGate("controller.active_resource", func() bool { return enabled }, emit)
	ctrl.SetActiveResourceGate(gate)

	g := graph.New(goal)
	appendT(g, nodeText, types.INJECTED, 0.5)
	g.Active().Value = branchValue
	return ctrl, log, g
}

// TestActiveResourceFiresOnLowVGoalRelevant — the POSITIVE case: flag ON, a goal-relevant active node
// whose V(s) sits at/below the exhaustion-confidence floor (the line is not earning its keep) triggers a
// re-source, returns the node's text as the re-source query, and emits critic.resource_trigger.
func TestActiveResourceFiresOnLowVGoalRelevant(t *testing.T) {
	// goal + node share content words ("token bucket rate limiter") so the node is goal-relevant; V below
	// the 0.5 exhaustion floor.
	ctrl, log, g := resourceCtrl(t, true,
		"how does the token bucket rate limiter refill",
		"the token bucket rate limiter refill is unclear", 0.30)

	fire, query, reason := ctrl.ResourceTrigger(g, g.Active().Value, false)
	if !fire {
		t.Fatal("expected a re-source trigger on a low-V(s), goal-relevant node")
	}
	if query == "" {
		t.Fatal("a fired trigger must return a non-empty re-source query (the node's text)")
	}
	if reason == "" {
		t.Fatal("a fired trigger must carry a human reason")
	}
	if _, ok := lastEvent(log, "critic.resource_trigger"); !ok {
		t.Fatal("a fired trigger must emit critic.resource_trigger (the observability contract)")
	}
}

// TestActiveResourceSilentWhenFlagOff — the OPT-IN guard: with the flag OFF the trigger never fires and
// emits NOTHING (a silent no-op, so default-OFF goldens stay byte-identical — no config.skip either).
func TestActiveResourceSilentWhenFlagOff(t *testing.T) {
	ctrl, log, g := resourceCtrl(t, false,
		"how does the token bucket rate limiter refill",
		"the token bucket rate limiter refill is unclear", 0.30)

	if fire, _, _ := ctrl.ResourceTrigger(g, g.Active().Value, false); fire {
		t.Fatal("flag OFF must never fire the trigger")
	}
	if len(*log) != 0 {
		t.Fatalf("flag OFF must be a SILENT no-op (no events), got %d events: %+v", len(*log), *log)
	}
}

// TestActiveResourceNotOnHighV — a CONFIDENT line (V well above the exhaustion floor) is earning its
// keep; re-sourcing it would be wasted retrieval, so the trigger must not fire. (Drops the low-V guard ->
// this fails.)
func TestActiveResourceNotOnHighV(t *testing.T) {
	ctrl, _, g := resourceCtrl(t, true,
		"how does the token bucket rate limiter refill",
		"the token bucket rate limiter refills one token per interval", 0.90)

	if fire, _, _ := ctrl.ResourceTrigger(g, g.Active().Value, false); fire {
		t.Fatal("a high-V(s) line must not trigger a re-source")
	}
}

// TestActiveResourceNotOnIrrelevantNode — a low-V line that is OFF-GOAL (no overlap with the goal) is an
// idle tangent; re-sourcing it does not pay into the goal, so the trigger must not fire. (Drops the
// goal-relevance guard -> this fails.)
func TestActiveResourceNotOnIrrelevantNode(t *testing.T) {
	ctrl, _, g := resourceCtrl(t, true,
		"how does the token bucket rate limiter refill",
		"i wonder what the weather is like on mars today", 0.20)

	if fire, _, _ := ctrl.ResourceTrigger(g, g.Active().Value, false); fire {
		t.Fatal("an off-goal (idle-tangent) node must not trigger a re-source")
	}
}

// TestActiveResourceBoundedOncePerBranch — the BOUND: once a branch has re-sourced (alreadySourced=true)
// the trigger never fires again for it, even while the line stays low-V and goal-relevant. This is what
// keeps the re-source from looping (the plant stays subcritical). (Drops the alreadySourced guard -> a
// low-V line re-sources every tick -> this fails.)
func TestActiveResourceBoundedOncePerBranch(t *testing.T) {
	ctrl, _, g := resourceCtrl(t, true,
		"how does the token bucket rate limiter refill",
		"the token bucket rate limiter refill is unclear", 0.30)

	if fire, _, _ := ctrl.ResourceTrigger(g, g.Active().Value, true); fire {
		t.Fatal("a branch that already re-sourced must not re-source again (the once-per-branch bound)")
	}
}
