package engine

import (
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// drive_agenda.go wires the awake DRIVE-goal mint + conscience gate (slice k, 02-conscious.md §7.2): the
// endogenous fresh line is seeded with a goal minted from the (process-drive x agenda-domain) cross —
// STEM-primary, Social-balancing — and EVERY minted drive goal passes the conscience floor (VetAction)
// before pursuit. conscious.activity.drive_agenda OFF (default) ⇒ the awake loop keeps the plain FreshGoal.

// wander is the engine's CONTENT author for the awake idle content (the cognition.Wanderer closure
// injected into Drives + DefaultMode + consumed by mintAgendaDriveGoal). It asks the backend's Wander
// CONTENT role to author ONE short first-person idle thought of the given kind ("curiosity" |
// "association" | "develop"), passing the LIVE active-line context + the seeded rng so the test double
// rotates its offline pool deterministically. On a model decline Wander returns "" and the caller goes
// DARK — there is NO canned fallback here (the awake stream goes quiet this tick, surfacing the gap).
// The graph is created per-run (after construction), so guard nil with an empty context.
func (e *Engine) wander(kind, hint string) string {
	if e.backend == nil {
		return ""
	}
	var ctx []types.Thought
	if e.graph != nil {
		ctx = e.graph.ActiveContext()
	}
	// wanderRNG (NOT e.rng): the dedicated content-rotation stream. The real model ignores the rng; only
	// the test double rotates its offline pool with it. Keeping it off e.rng means the offline awake
	// trajectory's CONTROL decisions match what the production loop (model authors, no engine-rng draw)
	// would take — so the test double perturbs the loop dynamics no more than the model does (none).
	rng := e.wanderRNG
	if rng == nil {
		rng = e.rng // defensive: a hand-built engine that skipped seeding still rotates deterministically
	}
	return e.backend.Wander(kind, hint, ctx, rng)
}

// mintAgendaDriveGoal mints an awake DRIVE goal from the (process-drive x agenda-domain) cross (§7.2) and
// conscience-gates it (VetAction) before it can be pursued. The agenda domain is drawn weighted
// (STEM-primary, Social balancing-but-kept, §7.2b); the process drive rotates deterministically. Returns
// the seed thought, or nil if the conscience floor VETOES it — a guard that is unconditional per §7.2 ("no
// drive goal is pursued without passing its discern-good/bad check") even though the development cross does
// not itself produce prohibited text.
func (e *Engine) mintAgendaDriveGoal() *types.Thought {
	// SELECTION is control (preserved): the process drive rotates deterministically; the agenda domain
	// is drawn weighted (STEM-primary, Social-balancing, §7.2b).
	drives := cognition.ProcessDrives()
	drive := drives[e.driveAgendaSeq%len(drives)]
	e.driveAgendaSeq++
	domain := e.drawAgendaDomain()

	// TEXT is now CONTENT (= the model): the goal text is AUTHORED by backend.Wander("develop", <domain
	// theme>, ...) instead of the old verb x theme MintDriveGoal table (manufactured intelligence). The
	// domain THEME rides as the hint so the minted text reflects WHAT the drive aims at. On a model
	// decline (text=="") the awake loop seeds NOTHING this tick — DARK, surface the gap — never a canned
	// goal string.
	text := e.wander("develop", domain.Theme())
	if text == "" {
		e.bus.Emit(events.Port, "drive: mint-agenda dark (model declined) ["+drive.String()+" x "+domain.String()+"]",
			events.D{"drive": "mint-agenda-dark", "domain": domain.String()})
		return nil
	}

	// The conscience VetAction gate is unconditional (§7.2: "no drive goal is pursued without passing
	// its discern-good/bad check") — kept verbatim, now over the model-authored text.
	if allow, reason := cognition.VetAction(text); !allow {
		e.bus.Emit(events.Port, "drive goal vetoed by conscience: "+reason,
			events.D{"drive": "vetoed", "reason": reason, "text": text})
		return nil
	}
	e.bus.Emit(events.Port, "drive: mint ["+drive.String()+" x "+domain.String()+"] -> "+text,
		events.D{"drive": "mint-agenda", "domain": domain.String(), "text": text})
	return &types.Thought{ID: -1, Text: text, Source: types.GENERATED, Confidence: 0.5}
}

// conscienceCeilingRefuses is the conscience model CEILING (slice k ceiling, §7.2, Pattern-C). The
// VetAction FLOOR has already allowed the action; this escalates a flagged-fuzzy case to a
// backends.ConscienceJudge for a nuanced good/bad judgment — which may only TIGHTEN (refuse). Returns
// (true, reason) iff the model refuses. A non-escalation (ceiling off / not fuzzy / no judge wired / model
// declined) lets the floor stand and is surfaced via escalation.floor_stands (Rule 4, never silent). With
// no LLM backend (the test double does not implement ConscienceJudge) this is a no-op → byte-identical.
func (e *Engine) conscienceCeilingRefuses(text string) (refuse bool, reason string) {
	if e.features == nil || !e.features.Conscious.Activity.ConscienceCeiling || !cognition.ConscienceFuzzy(text) {
		return false, ""
	}
	judge, ok := e.backend.(backends.ConscienceJudge)
	if !ok {
		e.bus.Emit(events.EscalationFloorStands, "conscience floor stands (no model ceiling)",
			events.D{"text": text, "site": "conscience"})
		return false, ""
	}
	allow, why, decided := judge.JudgeConscience(text)
	if !decided {
		e.bus.Emit(events.EscalationFloorStands, "conscience floor stands (model declined)",
			events.D{"text": text, "site": "conscience"})
		return false, ""
	}
	if !allow {
		return true, why
	}
	return false, ""
}

// drawAgendaDomain draws an agenda domain weighted by its agenda weight (§7.2b — STEM primary, Social
// balancing-but-kept). Deterministic under the seeded RNG; the Social domain keeps a positive share (the
// self-development floor that stops the system becoming a narrow STEM savant).
func (e *Engine) drawAgendaDomain() cognition.AgendaDomain {
	doms := cognition.AgendaDomains()
	total := 0.0
	for _, d := range doms {
		total += d.Weight()
	}
	if total <= 0 || e.rng == nil {
		return doms[0]
	}
	r := e.rng.Float64() * total
	acc := 0.0
	for _, d := range doms {
		if acc += d.Weight(); r < acc {
			return d
		}
	}
	return doms[len(doms)-1]
}
