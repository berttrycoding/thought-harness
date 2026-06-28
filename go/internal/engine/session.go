package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/session"
)

// tokensPerStep is the nominal per-operator token cost used to size + spend session budgets. It is a
// fixed constant (no rng, no clock) so the session.* event stream is deterministic/reproducible — the
// budgets MODEL the dispatch cost so the runtime is observable, they are not a measured meter (the
// heuristic substrate has none; a real backend's scheduler is the eventual source).
const tokensPerStep = 64

// openSessionTree builds a bounded Session spawn tree mirroring a synthesised workflow PROGRAM (P3.3):
// the root session is the workflow goal; each scheduled phase dispatches a child session (one operator
// group), bounded by MaxSessionDepth + a guaranteed-termination lifecycle, each carrying a token budget.
// It emits session.spawn (root) → session.dispatch (per phase) → session.merge (a parallel phase reduces
// its fan-out). This is what makes the runtime observable: the session subsystem genuinely tracks the
// live dispatch structure through the real Session/Dispatch types (not a decorative mirror). It fires
// only when a multi-phase program was synthesised — simple Q&A opens no tree — so the scenario goldens
// change only where a workflow actually runs.
func (e *Engine) openSessionTree(goal string, prog cognition.Program) {
	plans := prog.Schedule()
	if len(plans) == 0 {
		e.sessionRoot = nil
		return
	}
	rootSpec := session.Spec{
		Horizon: session.Bounded, Schedule: session.Schedule{Kind: session.OnDemand},
		State: session.Scratch, TickBudget: len(plans),
	}
	root, err := session.NewSession(goal, rootSpec)
	if err != nil {
		e.sessionRoot = nil
		return
	}
	e.bus.Emit(events.SessionSpawn, "session opened: "+runeSlice(goal, 50)+" ("+itoa(len(plans))+" phases)",
		events.D{"goal": goal, "phases": len(plans), "shape": prog.Shape()})

	childSpec := session.Spec{Horizon: session.SingleShot, Schedule: session.Schedule{Kind: session.OnDemand}}
	for i, p := range plans {
		tokCap := tokensPerStep * max(1, len(p.Steps))
		label := phaseLabel(p)
		child, derr := root.Dispatch(label, childSpec)
		if derr != nil {
			continue // depth bound hit — the tree stays bounded (observable, never fatal)
		}
		child.Budget = &session.Budget{TokenCap: tokCap}
		child.Budget.Spend(tokCap * 3 / 4) // model the dispatch spend deterministically (3/4 of the cap)
		e.bus.Emit(events.SessionDispatch, "dispatch phase "+itoa(i+1)+": "+runeSlice(label, 40),
			events.D{"goal": label, "depth": child.Depth, "phase": i + 1,
				"parallel": p.Parallel, "tokens": child.Budget.Spent, "cap": tokCap})
		if p.Parallel && len(p.Steps) > 1 {
			e.bus.Emit(events.SessionMerge, "merge "+itoa(len(p.Steps))+" parallel results (reduce)",
				events.D{"strategy": "reduce", "n": len(p.Steps), "phase": i + 1})
		}
	}
	// the root carries only the orchestration overhead — TreeTokensSpent aggregates the children, so
	// spending their total into the root would double-count.
	root.Budget = &session.Budget{TokenCap: tokensPerStep}
	root.Budget.Spend(tokensPerStep)
	e.sessionRoot = root
}

// terminateSessionTree closes the current episode's session tree (P3.8 guaranteed-termination): it emits
// session.terminate with the whole-tree token spend + node count, then drops the tree. A no-op when no
// tree is open (simple Q&A). reason is the lifecycle terminate cause (goal_met / budget_exhausted / ...).
func (e *Engine) terminateSessionTree(reason string) {
	if e.sessionRoot == nil {
		return
	}
	e.bus.Emit(events.SessionTerminate, "session terminated ("+reason+"): "+itoa(e.sessionRoot.TreeSize())+" nodes",
		events.D{"reason": reason, "nodes": e.sessionRoot.TreeSize(),
			"depth": e.sessionRoot.MaxDepthReached(), "tokens": e.sessionRoot.TreeTokensSpent()})
	e.sessionRoot = nil
}

// phaseLabel renders a phase's operator group as a label ("decompose", "rank ‖ eliminate") for the
// session goal + the dispatch event. Mirrors the viz phase-timeline operator join.
func phaseLabel(p cognition.PhasePlan) string {
	ops := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		ops[i] = s.Operator
	}
	if len(ops) == 0 {
		return "(phase)"
	}
	return strings.Join(ops, " ‖ ")
}
