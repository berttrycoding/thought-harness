package value

// pending_user_test.go — A1 of the 2026-06-12 arch-validation mandate: the §4.12 interrupt
// value re-seed is a pending-conversational-goal TERM inside V(s) (Pattern-A math), not a
// one-tick blip. Before this mechanism, OnInterrupt wrote Branch.Value=1.0 and the very next
// value.Update recomputed the branch from its priors, clobbering the re-seed — the user's line
// could sink below wander lines while the user was still waiting.
//
// The property: while the graph holds an unanswered USER_INPUT thought, every recompute of V(s)
// keeps that line resume-worthy (>= the Controller's 0.4 pursuit threshold) and above the wander
// line; delivery (graph.MarkDelivered) releases the term; a NEW user turn re-raises it.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// pendingFixture builds the awake-interrupt shape: a wander line on the root branch, then a user
// turn arriving as a USER_INPUT-seeded branch (what Controller.OnInterrupt mints), goal re-aimed
// at the user's words (what the engine does on a salient user percept). Returns (graph, userBid).
func pendingFixture() (*graph.ThoughtGraph, int) {
	g := graph.New("(awake — no task; mind is wandering)")
	g.Append(&types.Thought{ID: -1, Text: "idly mapping the shape of the problem space",
		Source: types.GENERATED, Confidence: 0.4}, 1)

	parent := 0
	reason := "user interrupt"
	ub := g.NewBranch(&parent, &reason)
	prev := g.ActiveBranch
	g.ActiveBranch = ub
	g.Append(&types.Thought{ID: -1, Text: "are you there?",
		Source: types.USER_INPUT, Confidence: 0.7}, 2)
	g.ActiveBranch = prev
	g.Goal = "are you there?"
	return g, ub
}

// The re-seed SURVIVES recomputation: across repeated value.Update calls the unanswered user line
// stays above the pursuit threshold and above the wander line — standing pressure, not a blip.
func TestPendingUserTermSurvivesUpdate(t *testing.T) {
	g, ub := pendingFixture()
	vs := New(nil)
	for i := 0; i < 5; i++ {
		values := vs.Update(g)
		if values[ub] < 0.5 {
			t.Fatalf("update %d: unanswered user line sank to %.3f (< 0.5) — the re-seed did not survive",
				i, values[ub])
		}
		if values[ub] <= values[0] {
			t.Fatalf("update %d: wander line (%.3f) outranks the waiting user (%.3f)",
				i, values[0], values[ub])
		}
	}
	ap := vs.AppraiseBranch(g, ub)
	if _, ok := ap.Signals["user_pending"]; !ok {
		t.Fatalf("appraisal carries no user_pending signal while the user waits: %v", ap.Signals)
	}
}

// Delivery RESOLVES the line: MarkDelivered releases the term — V falls back to the bootstrap
// priors and the user_pending signal disappears.
func TestPendingUserTermReleasedByDelivery(t *testing.T) {
	g, ub := pendingFixture()
	vs := New(nil)
	before := vs.AppraiseBranch(g, ub).Value

	g.MarkDelivered()
	after := vs.AppraiseBranch(g, ub)
	if after.Value >= before {
		t.Fatalf("delivery did not release the pending term: V %.3f -> %.3f", before, after.Value)
	}
	if _, ok := after.Signals["user_pending"]; ok {
		t.Fatalf("user_pending signal survives delivery: %v", after.Signals)
	}
	if g.UnresolvedUserInput(ub) {
		t.Fatal("graph still reports the user line unresolved after MarkDelivered")
	}
}

// A NEW turn after delivery re-raises the pressure: resolution is a high-water mark, not a
// one-shot — every later USER_INPUT thought is pending until the next delivery.
func TestPendingUserTermReopensOnNewTurn(t *testing.T) {
	g, ub := pendingFixture()
	g.MarkDelivered()
	if g.UnresolvedUserInput(ub) {
		t.Fatal("precondition: first turn should be resolved")
	}

	prev := g.ActiveBranch
	g.ActiveBranch = ub
	g.Append(&types.Thought{ID: -1, Text: "and what about the second thing I asked?",
		Source: types.USER_INPUT, Confidence: 0.7}, 3)
	g.ActiveBranch = prev

	if !g.UnresolvedUserInput(ub) {
		t.Fatal("a fresh user turn after delivery is not pending — the high-water mark is broken")
	}
	vs := New(nil)
	if v := vs.Update(g)[ub]; v < 0.5 {
		t.Fatalf("re-opened user line scores %.3f (< 0.5) — no standing pressure on the second turn", v)
	}
}
