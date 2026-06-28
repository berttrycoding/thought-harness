package cognition

// PropagateUnmeetable records that subgoal childID came back UNMEETABLE (a constraint surfaced, or it
// could not be reached) and propagates that feasibility signal UP the goal tree (slice a.5c,
// 02-conscious.md §1.9). The child is concluded (abandoned, terminal); the parent should REVISE only
// once ALL its children are terminal — the low-pass filter of §1.3/§1.9 (leaf churn alone never moves
// the top; the signal must accumulate). Returns reviseParent + the parent id when that threshold is
// reached. `goals` is the id→*Goal map. A no-op for an unknown child or a top goal.
func PropagateUnmeetable(goals map[string]*Goal, childID string) (reviseParent bool, parentID string) {
	child, ok := goals[childID]
	if !ok {
		return false, ""
	}
	child.Conclude(false) // unmeetable → abandoned (terminal)
	if child.Parent == nil {
		return false, "" // a top goal has nowhere to propagate
	}
	parent, ok := goals[*child.Parent]
	if !ok || len(parent.Children) == 0 {
		return false, ""
	}
	// Accumulated feedback (§1.9): the parent revises only when every child is terminal.
	for _, cid := range parent.Children {
		if c, ok := goals[cid]; ok && !c.Status.IsTerminal() {
			return false, ""
		}
	}
	return true, parent.ID
}

// ReviseOnUnmeetable is the feedback loop's actuator (slice a.5c, §1.9): it concludes childID unmeetable,
// propagates up the tree (PropagateUnmeetable), and — when every sibling is terminal — DRIVES the parent's
// transition. A matched/active parent owes a sharper pursuit → GoalRefined; an open/unstarted parent with
// no feasible child is itself infeasible → GoalAbandoned (GoalRefined is not reachable from open, so the
// transition guard routes it to abandoned). Returns the parent id + its NEW status, or ("","") when the
// signal did not yet accumulate to the top (leaf churn alone never moves the parent — the §1.3 low-pass).
func ReviseOnUnmeetable(goals map[string]*Goal, childID string) (parentID string, newStatus GoalStatus) {
	revise, pid := PropagateUnmeetable(goals, childID)
	if !revise {
		return "", ""
	}
	parent := goals[pid]
	if parent.Transition(GoalRefined) { // matched/active → a sharper child/sibling is owed
		return pid, GoalRefined
	}
	parent.Transition(GoalAbandoned) // open/unstarted with no feasible child → infeasible
	return pid, parent.Status
}
