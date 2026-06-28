package engine

import (
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// seed_intents.go wires the SEED-INTENT PORTFOLIO (C1, 02-conscious.md §1.8 "Seed intents — the standing
// forest roots") into the AWAKE engine loop. The standing endogenous DRIVE roots are planted into the
// forest at the FIRST awake tick so the loop has something to think about BEFORE any user input — the
// awake regime's μ>0 positive-baseline realised as NAMED standing intents, not just a reserved attention
// fraction.
//
// ADDITIVE + FLAG-GATED + DEFAULT-OFF. The seeding runs ONLY when:
//   - the engine is in continuous/awake mode (the reactive loop never calls seedForestIntents), AND
//   - conscious.activity.seed_intents is ON (the C1 opt-in knob, default OFF).
// With the flag OFF the awake loop never plants a root and never emits conscious.seed_intent, so the
// forest is seeded reactively only (a USER turn / a wandered DRIVE line) — byte-identical to pre-C1.
//
// Each root is a STANDING DRIVE line bound via BindDriveBranch, so it is a NON-USER line counted toward
// the μ self-development floor in cross-goal focus (§1.8) — USER lanes still take priority, and the μ-floor
// keeps the introspection/perception roots from starving. The roots are forked off the boot episode's root
// branch and parked in the frontier; the existing forest rerank values each per-branch binding and a
// standing root may then be resumed under the μ-floor. (The live post-interrupt focus path is the
// Controller's OnInterrupt, which re-focuses the USER line over the standing roots — NOT forestFocus in
// forest_value.go, which is currently dead/uncalled; do not cite it as the live mechanism.)

// seedForestIntents plants the standing seed-intent forest roots once per engine (C1, §1.8). It is a no-op
// unless seed_intents is on AND it has not already seeded (seedIntentsDone). Called at the first awake tick
// after the boot episode opens (so e.graph + e.mcp exist). Deterministic: the only ordering source is the
// declared portfolio order (kernel-of-3 first) and the engine's seeded RNG is NOT consulted here — the set
// is a fixed prefix sized by the seed_intent_count knob.
//
// For each root it: mints a NEW branch forked off the active (root) branch, appends the standing-root
// thought there, binds the branch as a DRIVE line (BindDriveBranch — the μ-floor counts it as non-user),
// records a first-class DRIVE Goal entity, and emits conscious.seed_intent. Every root passes the
// conscience FLOOR (VetAction) before it is planted — a standing intent is an endogenous goal and §7.2
// requires it be vetted; the development-grade portfolio carries no prohibited text, but the gate is
// unconditional. After seeding, focus returns to the boot branch so the active line is unchanged (the
// roots sit in the frontier, reranked + resumable — they ENTER the forest and are eligible for focus).
func (e *Engine) seedForestIntents(tick int) {
	if e.seedIntentsDone {
		return
	}
	if e.features == nil || !e.features.Conscious.Activity.SeedIntents {
		return
	}
	if e.graph == nil || e.mcp == nil {
		return // no episode open yet — nothing to fork roots from
	}
	e.seedIntentsDone = true

	bootBranch := e.graph.ActiveBranch
	count := e.features.Conscious.Activity.SeedIntentCount
	portfolio := cognition.SeedPortfolio(count) // clamped to [kernel-of-3, full portfolio]

	seeded := 0
	for _, si := range portfolio {
		// Conscience FLOOR: an endogenous DRIVE goal is never pursued without passing the discern-good/bad
		// check (§7.2). The development-grade portfolio is benign, but the gate is unconditional — a veto
		// drops THAT root (the others still seed).
		if allow, reason := cognition.VetAction(si.Goal); !allow {
			e.bus.Emit(events.SeedIntent, "seed-intent vetoed by conscience: "+si.Name,
				events.D{"name": si.Name, "vetoed": true, "reason": reason})
			continue
		}

		// Fork a fresh standing-root branch off the boot branch and plant the standing-root thought there.
		reason := "seed-intent: " + si.Name
		bid := e.graph.NewBranch(intPtr(bootBranch), &reason)
		e.mcp.Tick = tick
		e.mcp.Focus(bid)
		root := &types.Thought{ID: -1, Text: si.Goal, Source: types.GENERATED, Confidence: 0.5}
		e.appendThought(root, tick)

		// Bind it as a NON-USER drive line (the μ self-development floor counts it; USER lanes keep priority).
		e.BindDriveBranch(bid, si.Goal, false)

		// Record branch -> faculty (the faculty attention scheduler reads this to arbitrate focus
		// fair-share across faculties). Additive bookkeeping — only the scheduler consults it.
		if e.branchFaculty == nil {
			e.branchFaculty = map[int]cognition.SeedFaculty{}
		}
		e.branchFaculty[bid] = si.Faculty

		// Record the first-class DRIVE Goal entity (the setpoint, §1.6). Acceptance is the standing watch's
		// never-self-declared condition (SeedPortfolio authored it). The Goal holds no subconscious pointer.
		goalObj := cognition.Goal{
			ID:         e.processID + ":seed:" + si.Name,
			Text:       si.Goal,
			Source:     si.Source, // GoalDrive
			Status:     cognition.GoalActive,
			Acceptance: si.Acceptance,
		}
		e.goals = append(e.goals, goalObj)

		e.bus.Emit(events.SeedIntent, "seed-intent ["+si.Faculty.String()+"] "+si.Name+" -> "+si.Goal,
			events.D{
				"name": si.Name, "faculty": si.Faculty.String(), "backed_by": si.BackedBy,
				"kernel": si.Kernel, "count": len(portfolio), "branch": bid, "source": si.Source.String(),
			})
		seeded++
	}

	// Return focus to the boot branch: the standing roots sit in the frontier (reranked + resumable), the
	// active line is unchanged. The roots have ENTERED the forest; cross-goal focus may now resume one.
	e.mcp.Tick = tick
	e.mcp.Focus(bootBranch)
	e.pruneBranches() // re-apply the regulator's stashed-branch cap (U≤1) after planting the roots
	e.bus.Emit(events.SeedIntent, "seed-intent portfolio planted: "+itoa(seeded)+" standing forest root(s)",
		events.D{"seeded": seeded, "count": len(portfolio), "branch": bootBranch})
}
