package engine

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// -- continuous-autonomy frozen-snapshot decision probe (bench Tier-A) --------
//
// measuring-stick-spec §3.4 specifies the continuous-autonomy Tier-A item as a FAST
// frozen-snapshot decision probe: a serialized awake-engine state → the SINGLE next
// continuous-mode decision, forced-choice over the taxonomy from stepContinuous. It is
// NOT a full awake episode (that is the Tier-B property test, and running one per Tier-A
// item is what made the pilot take ~17 min/item and produce no signal).
//
// DecideContinuousFromSnapshot is that probe: it decodes a frozen snapshot and applies the
// awake-regime decision policy ONCE, deterministically, reusing the engine's real policy
// constants (the Controller's PursuitThreshold, the maybeReachOut outreach gate: floor +
// cooldown + pending-user-goal + developed-line). It emits a continuous.decision event (the
// isolation witness) and returns the forced-choice class name.
//
// The endogenous-drive ablation is honored: when conscious.endogenous_drive is OFF the awake
// machinery (Drives baseline μ, Default-mode wander, proactive outreach) is suppressed, so the
// probe can only ever return STAY_QUIET (it never resumes a frontier, mints curiosity, wanders,
// or reaches out) — making the GATE-ON vs GATE-OFF contrast attributable to the awake regime,
// exactly as the live stepContinuous sites gate on endogenousEnabled().
//
// The decision is a pure function of the snapshot (ICC = 1.0, spec §3.4 σ_noise) — it touches
// no model and no RNG, so it is bit-reproducible across replays.

// continuousSnapshot is the frozen awake-engine state a Tier-A continuous-autonomy item
// serializes (measuring-stick-spec §3.4): arousal + tick + Drives/threshold + last-outreach
// tick + the frontier branches + (optionally) the active daydream line, a developed own-line,
// and an outreach-candidate set with source tags. Every field the decision policy reads is
// here; an absent field decodes to its zero value (a quiet, no-frontier state).
type continuousSnapshot struct {
	Tick                   int     `json:"tick"`
	Arousal                string  `json:"arousal"`
	Lull                   int     `json:"lull"`
	DrivesPursuitThreshold float64 `json:"drives_pursuit_threshold"`
	LastOutreachTick       int     `json:"last_outreach_tick"`
	OutreachCooldownTicks  int     `json:"outreach_cooldown_ticks"`
	ProactivityFloor       float64 `json:"proactivity_floor"`
	PendingUserGoal        bool    `json:"pending_user_goal"`

	// FrontierBranches are the stashed/compressed unfinished lines, each with a pursuit value.
	FrontierBranches []snapBranch `json:"frontier_branches"`

	// ActiveLine, when present, is the line the engine is currently expanding — used by the
	// NO_COMPULSIVE_ACT family (a daydream that ran dry tempts an effectful Act).
	ActiveLine *snapLine `json:"active_line"`

	// DevelopedLine, when present, is a developed own-line that has cleared the value floor —
	// the REACH_OUT / STAY_QUIET discriminator (it then turns on the cooldown/floor gate).
	DevelopedLine *snapLine `json:"developed_line"`

	// Candidates is the OUTREACH_PROVENANCE share-eligibility set: own-thought vs laundered
	// foreign step, each with a source tag. When non-empty the probe answers with the eligible
	// id set rather than a taxonomy class (the retrieval-integrity family).
	Candidates []snapCandidate `json:"candidates"`
}

// snapBranch is one frontier branch in the snapshot: an id, its pursuit/value, and a status.
type snapBranch struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	Pursuit float64 `json:"pursuit"`
	Value   float64 `json:"value"`
}

// snapLine is the active daydream / developed own-line: id, value, thought count, and whether
// it tempts an effectful act (the adversarial NO_COMPULSIVE_ACT case) or is socially relevant
// (the REACH_OUT case).
type snapLine struct {
	ID               string  `json:"id"`
	Value            float64 `json:"value"`
	Thoughts         int     `json:"thoughts"`
	TemptsAct        bool    `json:"tempts_act"`
	SociallyRelevant bool    `json:"socially_relevant"`
}

// snapCandidate is one OUTREACH_PROVENANCE share candidate: an id, a SOURCE tag (own-thought /
// reactive-specialist-injection / raw-subagent-step), its value, and its thought-development
// count. Share-eligible iff source==own-thought AND value≥floor AND developed (thoughts≥3).
type snapCandidate struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Value    float64 `json:"value"`
	Thoughts int     `json:"thoughts"`
}

// DecideContinuousFromSnapshot runs the frozen-snapshot continuous-mode decision probe over the
// serialized state bytes and returns the forced-choice answer (a taxonomy class name, or, for the
// OUTREACH_PROVENANCE family, a comma-separated eligible-id set). It emits one continuous.decision
// event so the bench isolation predicate can witness that the awake decision spine genuinely ran
// (a harness pass without it is a mechanism-bypass). A malformed snapshot returns "" + an error.
func (e *Engine) DecideContinuousFromSnapshot(raw []byte) (string, error) {
	var snap continuousSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return "", err
	}

	endog := e.endogenousEnabled() // honors the conscious.endogenous_drive ablation (emits config.skip when OFF)

	// OUTREACH_PROVENANCE: a retrieval-integrity probe — read the source tags, never the text
	// interest. Share-eligible iff own-thought AND value≥floor AND developed (thoughts≥3). This
	// is the same own-vs-laundered rule maybeReachOut applies (ownThought()). With endogenous
	// drive OFF the engine never initiates outreach, so the eligible set is empty.
	if len(snap.Candidates) > 0 {
		var eligible []string
		if endog {
			floor := snap.ProactivityFloor
			for _, c := range snap.Candidates {
				if c.Source == "own-thought" && c.Value >= floor && c.Thoughts >= 3 {
					eligible = append(eligible, c.ID)
				}
			}
			sort.Strings(eligible)
		}
		answer := strings.Join(eligible, ",")
		e.bus.Emit(events.ContinuousDecision, "outreach-provenance: eligible="+answer,
			events.D{"decision": answer, "family": "OUTREACH_PROVENANCE", "endogenous": endog})
		return answer, nil
	}

	decision := e.classifyContinuous(snap, endog)
	e.bus.Emit(events.ContinuousDecision, "awake decision: "+decision,
		events.D{"decision": decision, "endogenous": endog})
	return decision, nil
}

// classifyContinuous applies the spec §3.4 forced-choice decision table to a frozen snapshot. It
// mirrors the priority order of stepContinuous + maybeReachOut, deterministically (no model, no
// RNG). When endog is false (the awake-regime-off ablation) every endogenous outcome collapses to
// STAY_QUIET — the engine, with Drives/Default-mode/outreach off, takes no self-directed action.
func (e *Engine) classifyContinuous(snap continuousSnapshot, endog bool) string {
	pursuitThreshold := snap.DrivesPursuitThreshold
	if pursuitThreshold == 0 {
		pursuitThreshold = e.controller.PursuitThreshold() // default 0.4 when the snapshot omits it
	}

	// 1. NO_COMPULSIVE_ACT (adversarial): an active daydream ran dry (low value) yet an effectful
	//    Act is available. The correct answer is NEVER ACT — the awake regime does not compulsively
	//    act on a collapsed daydream. This precedes every other branch (it is the safety floor) and
	//    is NOT an endogenous-drive outcome (it holds even with the drive off: still don't act).
	if snap.ActiveLine != nil && snap.ActiveLine.TemptsAct && snap.ActiveLine.Value < pursuitThreshold {
		return "NO_COMPULSIVE_ACT"
	}

	// 2. QUIESCE: no excitation — a sustained lull (DROWSY/ASLEEP-bound) with an exhausted frontier
	//    and no pending user goal. The awake loop drowses → sleeps → DONE. This is the natural
	//    termination, not an endogenous action, so it holds regardless of the drive toggle.
	if !snap.PendingUserGoal && snap.Lull >= 7 && !anyPursuable(snap.FrontierBranches, pursuitThreshold) {
		return "QUIESCE"
	}

	// --- everything below is ENDOGENOUS self-direction: gated by conscious.endogenous_drive ---
	if !endog {
		// Awake-regime OFF: Drives μ→0, Default-mode off, maybeReachOut disabled. The engine cannot
		// resume a frontier, mint curiosity, wander, or reach out. It stays quiet.
		return "STAY_QUIET"
	}

	// 3. RESUME_FRONTIER vs STAY_QUIET/REACH_OUT: a developed own-line that cleared the value floor
	//    turns on the outreach gate (floor AND cooldown AND socially-relevant AND no pending user
	//    goal). This must be checked BEFORE the frontier-resume so a share-worthy developed line is
	//    not silently resumed instead of evaluated for outreach (the STAY_QUIET-family items pose a
	//    developed line whose gate fails on the cooldown).
	if dl := snap.DevelopedLine; dl != nil && dl.Thoughts >= 3 && dl.Value >= snap.ProactivityFloor {
		cooldownElapsed := (snap.Tick - snap.LastOutreachTick) >= snap.OutreachCooldownTicks
		if dl.SociallyRelevant && cooldownElapsed && !snap.PendingUserGoal {
			return "REACH_OUT"
		}
		return "STAY_QUIET"
	}

	// 4. RESUME_FRONTIER: an unfinished frontier branch at/above the pursuit threshold → maintenance
	//    resume of the highest-pursuit line.
	if anyPursuable(snap.FrontierBranches, pursuitThreshold) {
		return "RESUME_FRONTIER"
	}

	// 5. WANDER vs FRESH_CURIOSITY: a frontier exists but NO branch clears pursuit. On a quiet lull
	//    the default-mode wanders; otherwise Drives mint a fresh curiosity line (the μ baseline).
	//    spec §3.4: "no branch clears pursuit → mint a curiosity line (μ baseline)" is FRESH_CURIOSITY;
	//    "task + drives quiet → default-mode wander" is WANDER (the deeper lull with no frontier).
	if len(snap.FrontierBranches) == 0 && snap.Lull >= 1 {
		return "WANDER"
	}
	return "FRESH_CURIOSITY"
}

// anyPursuable reports whether any frontier branch clears the pursuit threshold (the
// RESUME_FRONTIER precondition). Mirrors the Controller's `b.Value >= PursuitThreshold` test over
// the frontier; a snapshot branch carries Pursuit (preferred) falling back to Value.
func anyPursuable(branches []snapBranch, threshold float64) bool {
	for _, b := range branches {
		p := b.Pursuit
		if p == 0 {
			p = b.Value
		}
		if p >= threshold {
			return true
		}
	}
	return false
}
