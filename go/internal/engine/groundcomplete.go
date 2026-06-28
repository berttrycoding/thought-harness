// groundcomplete.go — the grounding-completeness reading directive (THOUGHT_GROUND_COMPLETE).
//
// THE GAP. A grounding answer reads the material, finds a value that NAME-MATCHES the question, and
// answers with it — even when a LATER statement in the SAME material corrected/replaced/overrode that
// first value (so the answer in force is a different one), or when the answer is a base value the
// material says to ADJUST/CONVERT before reporting. The harness imports the right material but the
// ANSWER-VOICING step (action.respond) reads it shallowly: first name-match wins, in-material
// corrections and modifiers are skipped.
//
// THE FIX (Pattern B — CONTENT/model-owned, never a hardcoded answer). When the flag is ON, append a
// GENERAL grounding-completeness reading directive to the RESPOND system prompt, so the model — before
// answering — (a) uses the value actually IN FORCE (a later in-material statement that corrects,
// replaces, or overrides an earlier value wins over the first name-matching one), and (b) applies an
// in-material adjustment/conversion to the base value. The directive describes a general reading
// BEHAVIOUR; it enumerates NO trigger keywords (that would overfit to one phrasing) and it carries NO
// answer/number (the model still does the reading — the harness only asks it to read completely).
//
// DECLINE-SAFETY (load-bearing). The directive EXPLICITLY preserves the never-fabricate discipline:
// when a value the question needs is NOT present in the material (it is only referenced via an external
// file/package/dashboard the closed loop cannot read), DECLINE — never invent a value to resolve a
// pointer. Without this clause a naive "use the value in force / resolve every pointer" directive would
// push the anti-confabulation tasks (which score p=1.0 by correctly declining an unreadable external
// pointer) into fabricating a value and dropping them. The clause keeps "use the in-force value" and
// "never invent a missing value" as the SAME discipline: prefer the corrected/adjusted value WHEN it is
// in the material; decline WHEN the needed value is not.
//
// FLAG. THOUGHT_GROUND_COMPLETE (env-knob, resolved ONCE at init like THOUGHT_FORCE_GROUND /
// THOUGHT_MODEL_SELECT). Default OFF => no fragment => the RESPOND prompt is byte-identical => goldens
// untouched. The fragment is pushed only to a backend that implements GroundCompletePrompter (the LLM
// backend + the claude bridge — NOT the test double), so the offline/golden path is unaffected by
// construction. A respond that engages the directive emits conscious.ground_complete (observability;
// the directive is never silent).
package engine

import (
	"os"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// groundCompleteEnabled is the THOUGHT_GROUND_COMPLETE toggle resolved ONCE at init (unset / false / 0
// / garbage is OFF — byte-identical). Mirrors the resolveForceGround / resolveModelSelect env-knob
// pattern: ON only for an explicit affirmative; everything else is the safe OFF default.
var groundCompleteEnabled = resolveGroundComplete()

// resolveGroundComplete reads THOUGHT_GROUND_COMPLETE once. ON for "1"/"true"/"yes"/"on"
// (case-insensitive, surrounding whitespace trimmed); anything else (incl. unset / "0" / "false" /
// garbage) is OFF — the default that keeps the RESPOND prompt byte-identical.
func resolveGroundComplete() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("THOUGHT_GROUND_COMPLETE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// groundCompleteDirective is the EXACT directive text appended to the RESPOND system prompt when the
// flag is ON. It is GENERAL (no enumerated trigger keywords — it describes a reading behaviour, not a
// keyword lookup), carries NO answer/number, and is DECLINE-SAFE (the last sentence preserves the
// never-fabricate discipline so the anti-confabulation decline is never broken). One source of truth:
// the test asserts against this exact string, so the directive and its tests can never drift.
const groundCompleteDirective = "Before you answer, read the material COMPLETELY for the value the " +
	"question needs, not just the first thing that matches the name. First, if a later statement in " +
	"the material corrects, replaces, or overrides an earlier value, use the value actually in force — " +
	"the later one — not the first one you found. Second, if the answer is a base value that the " +
	"material states should be adjusted or converted before it is reported, apply that stated " +
	"adjustment or conversion to the base value and report the result. But never invent a value to " +
	"satisfy the question: if a value the question needs is not actually present in the material — it " +
	"is only referenced via an external file, package, or dashboard you cannot read here — DECLINE and " +
	"say it is not determinable from the material rather than guessing a number."

// groundCompleteFragment renders the directive for the RESPOND prompt: the exact directive text when
// the flag is ON, else "" (the default ⇒ no append ⇒ byte-identical prompt). Pure; no I/O.
func (e *Engine) groundCompleteFragment() string {
	if !groundCompleteEnabled {
		return ""
	}
	return groundCompleteDirective
}

// applyGroundCompleteFragment pushes the grounding-completeness directive into the backend right before
// an outward-facing respond — the same optional-interface seam as the persona/legible fragments. The
// test double does not implement GroundCompletePrompter, so the offline/golden path is untouched. When
// the flag is ON it emits conscious.ground_complete so the engaged directive is observable (never
// silent); when OFF it clears the fragment ("") and emits nothing.
func (e *Engine) applyGroundCompleteFragment() {
	gp, ok := e.backend.(backends.GroundCompletePrompter)
	if !ok {
		return // the test double (and any non-LLM backend) ignores the directive ⇒ byte-identical
	}
	fragment := e.groundCompleteFragment()
	gp.SetGroundCompleteFragment(fragment)
	if fragment != "" {
		e.bus.Emit(events.GroundComplete,
			"ground-complete: respond reads for the in-force / adjusted value (decline if absent)",
			events.D{"site": "action.respond", "flag": "THOUGHT_GROUND_COMPLETE"})
	}
}
