package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// goal_feedback.go wires the goal feedback loop (slice a.5c, 02-conscious.md §1.9): when an episode
// concludes a SUBGOAL as unmeetable, the feasibility signal propagates UP the goal tree and DRIVES the
// parent's transition (refined when a sharper pursuit is owed, else abandoned). conscious.activity
// .goal_feedback OFF (default) ⇒ no propagation. A no-op for a top goal regardless (nothing to propagate).

// goalMap builds the id→*Goal view the feedback actuator needs over the engine's goal set (top goals plus
// any decomposed subgoals — both live in e.goals). The pointers alias the slice, so a driven transition is
// reflected in e.goals.
func (e *Engine) goalMap() map[string]*cognition.Goal {
	m := make(map[string]*cognition.Goal, len(e.goals))
	for i := range e.goals {
		m[e.goals[i].ID] = &e.goals[i]
	}
	return m
}

// reviseGoalOnUnmeetable propagates an unmeetable childID up the goal tree and drives the parent's
// transition (§1.9). Emits a goal event when a parent is actually revised. Gated by goal_feedback; a no-op
// for a top goal / unaccumulated signal. Returns the revised parent id (or "").
func (e *Engine) reviseGoalOnUnmeetable(childID string) string {
	if e.features == nil || !e.features.Conscious.Activity.GoalFeedback {
		return ""
	}
	pid, st := cognition.ReviseOnUnmeetable(e.goalMap(), childID)
	if pid == "" {
		return ""
	}
	e.bus.Emit(events.Goal, "goal feedback: "+pid+" -> "+string(st)+" (child "+childID+" unmeetable)",
		events.D{"id": pid, "status": string(st), "child": childID, "revised": true})
	return pid
}

// acceptanceCeiling evaluates a goal's Acceptance via the Pattern-C floor + optional model CEILING (§1.6,
// #29). The deterministic CheckAcceptanceFloor decides the clear cases; only a flagged-fuzzy case (no
// checkable predicate) is escalated to a backends.AcceptanceJudge — and only when acceptance_ceiling is on
// and an LLM backend provides the judge. A non-escalation (ceiling off / not fuzzy / no judge / model
// declined) lets the floor stand, surfaced via escalation.floor_stands. Returns the final outcome.
func (e *Engine) acceptanceCeiling(goal *cognition.Goal) cognition.AcceptanceOutcome {
	ctx := e.workingContext()
	out, fuzzy := cognition.CheckAcceptanceFloor(goal.Acceptance, thoughtsText(ctx))
	if !fuzzy || e.features == nil || !e.features.Conscious.Activity.AcceptanceCeiling {
		return out
	}
	judge, ok := e.backend.(backends.AcceptanceJudge)
	if !ok {
		e.bus.Emit(events.EscalationFloorStands, "acceptance floor stands (no model ceiling)",
			events.D{"goal": goal.ID, "site": "acceptance"})
		return out
	}
	verdict, decided := judge.JudgeAcceptance(goal.Text, ctx)
	if !decided {
		e.bus.Emit(events.EscalationFloorStands, "acceptance floor stands (model declined)",
			events.D{"goal": goal.ID, "site": "acceptance"})
		return out
	}
	switch verdict {
	case "met":
		return cognition.AcceptanceMet
	case "unmeetable":
		return cognition.AcceptanceUnmeetable
	default:
		return cognition.AcceptancePending
	}
}

// thoughtsText joins a thought slice into a single lower-friendly blob for the deterministic acceptance
// floor to scan (the recent working context).
func thoughtsText(ctx []types.Thought) string {
	parts := make([]string, 0, len(ctx))
	for _, t := range ctx {
		parts = append(parts, t.Text)
	}
	return strings.Join(parts, " ")
}
