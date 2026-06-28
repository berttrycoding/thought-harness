// forceground.go — FIX 2: FORCE A GROUNDING READ BEFORE GIVE-UP (the early-give-up fix).
//
// THE BUG (the A1 "gives up after search" symptom). On a grounding-shaped goal — one that needs
// reality import to answer — the harness quiesces at ~6 calls with grounded=false: the Controller's
// floor decides STOP/GIVE_UP (over-step-budget / loop-exhausted-already-acted / branch-exhausted) and
// the episode ends from priors, having never imported reality. (Measured: realhard K=3 — the
// give-up-at-6-calls signature on multi-hop + backtrack.)
//
// THE FIX (Pattern-C OVERRIDE at the engine give-up site, mirroring the WF-G deadline override that
// sits ABOVE the Controller). On a grounding-shaped goal, the engine may NOT let a PRE-grounding
// give-up STOP through until at least one ACT has imported reality this episode: when the floor
// decides STOP/GIVE_UP and zero acts have crossed the watched seam yet, the engine downgrades the
// decision to ACT (open to reality) instead of giving up. It NEVER blocks a legitimate fact-based STOP
// — a GOAL_MET stop, a non-grounding goal, or a give-up AFTER reality was already consulted all stand.
//
// FLAG. THOUGHT_FORCE_GROUND (env-knob, resolved ONCE at init like THOUGHT_MODEL_SELECT). Default OFF
// => the give-up paths are untouched => byte-identical. The override is also a structural no-op when
// the watched-sync seam is OFF (no reality path to force) so it can never spin.
package engine

import (
	"os"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// forceGroundEnabled is the THOUGHT_FORCE_GROUND toggle resolved ONCE at init (unset / false / 0 is
// OFF — byte-identical). Mirrors the resolveModelSelect / resolveBeamLambda env-knob pattern.
var forceGroundEnabled = resolveForceGround()

// resolveForceGround reads THOUGHT_FORCE_GROUND once. ON for "1"/"true"/"yes"/"on"; else OFF.
func resolveForceGround() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("THOUGHT_FORCE_GROUND"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// groundingShapedGoal reports whether the episode goal asks for reality import — a read/inspect/
// investigate/find/search/grep intent, OR a goal that names a concrete file path / well-known file.
// This is a deterministic Pattern-A predicate (the SAME action.SelectTool floor the selection path
// uses, plus the broad "investigate/report the value" grounding-goal shape): it is the gate that keeps
// FIX 2 from firing on a non-grounding goal (a chat turn, an arithmetic question), so a legitimate
// fact-based STOP on those is never blocked.
func (e *Engine) groundingShapedGoal() bool {
	if e.graph == nil {
		return false
	}
	goal := strings.ToLower(e.graph.Goal)
	if goal == "" {
		return false
	}
	for _, w := range groundingGoalWords {
		if strings.Contains(goal, w) {
			return true
		}
	}
	return false
}

// groundingGoalWords are the deterministic markers of a goal that wants reality imported. Phrase/word
// level so a non-grounding goal (a greeting, a pure-reasoning question) does not match. Aligned with
// the read/search verbs the selector recognises (action.selector) plus the grounded-investigator
// shape ("investigate ... report the value", the A1 / realhard multi-hop family).
var groundingGoalWords = []string{
	"read ", "inspect", "investigate", "examine", "look at", "look in", "open ",
	"find the", "search the", "search for", "grep", "locate", "value of", "value assigned",
	"contents of", "what is in", "config", ".yaml", ".yml", ".go", ".py", ".json", ".toml",
	"file", "source files", "the codebase", "trace the", "follow the",
}

// forceGroundDecision is the Pattern-C OVERRIDE: it returns the decision the engine should actually
// execute. With the flag OFF it is the floor decision verbatim (byte-identical). With the flag ON, on
// a grounding-shaped goal whose floor decided a PRE-grounding give-up STOP and where zero acts have
// imported reality this episode AND the watched-sync seam can reach reality, it downgrades the give-up
// to ACT (and stamps the active branch unacted so the ACT path runs). A non-override is silent; a
// fired override emits escalation.force_ground (Pattern-C: never silent).
//
// It NEVER touches a non-STOP decision, a GOAL_MET stop, or a give-up after an act already ran — those
// are legitimate and stand.
func (e *Engine) forceGroundDecision(floor types.Decision) types.Decision {
	if !forceGroundEnabled || floor != types.STOP {
		return floor
	}
	// Only the PRE-grounding GIVE_UP class — a fact-based GOAL_MET stop is legitimate and stands.
	meta := e.controller.LastMeta
	if meta.StopKind == nil || *meta.StopKind != types.GIVE_UP.String() {
		return floor
	}
	if !e.groundingShapedGoal() {
		return floor // a non-grounding goal can give up from priors — nothing to import
	}
	if e.episodeActsIssued > 0 {
		return floor // reality was already consulted this episode — the give-up is honest now
	}
	if !e.watchedSyncEnabled() {
		return floor // no reality path to force (the sync seam is off) — never spin
	}
	// Force the import: re-open the active branch so the ACT path runs (it would otherwise be marked
	// acted on the prior tick or below the act gate). The Controller's structural floor is untouched —
	// this is an engine-level override of the GIVE_UP terminal only, exactly like the deadline override.
	if e.graph != nil {
		delete(e.actedBranches, e.graph.ActiveBranch)
	}
	e.bus.Emit(events.EscalationForceGround,
		"force-ground: grounding goal give-up before any reality import -> ACT (import reality first)",
		events.D{
			"site":             "engine.give_up",
			"floor_decision":   floor.String(),
			"forced":           types.ACT.String(),
			"reads_issued":     e.episodeActsIssued,
			"grounding_shaped": true,
		})
	return types.ACT
}
