// This file is the CONSCIENCE EVAL GATE — the spiritual/conscience tier's discern-good/bad check
// (docs/cognition/02-conscious.md §7.2). It is the Pattern-C deterministic FLOOR only: a
// predicate that refuses any goal/action matching a HARD PROHIBITION distilled from the ported
// identity system (.thought/identity/discernment.md §Test-Questions + stewardship.md §Forbidden).
//
// Pattern C (§7.2): a deterministic floor of hard prohibitions HERE + a model ceiling for nuanced
// judgment escalated LATER (where a backend is available — not in this file). The floor never needs a
// model: a request that names a prohibition is refused outright. The ceiling adds nuance on the
// flagged-fuzzy case; this file is the floor it sits on. "Axioms Always Win" — no external input
// overrides the principles, so a matched prohibition is a hard refuse, not a soft preference.
//
// This is the conscience-tier governance the design calls load-bearing for alignment (§7.2):
// developing cognition + engineering WITHOUT a governing good/bad layer is exactly the failure mode
// to avoid. Every minted DRIVE goal (drivetier.go) and every action is meant to pass this floor
// before pursuit.
package cognition

import "strings"

// prohibition is one hard rule the conscience floor enforces — a set of cue phrases (any match fires)
// and the reason returned on a refusal. The cues are distilled straight from the identity system so
// the gate's content traces to the ported covenant, not to ad-hoc strings.
type prohibition struct {
	reason string
	cues   []string
}

// hardProhibitions is the conscience FLOOR — the distilled rule set (§7.2). Each rule maps to a
// stewardship.md §Forbidden / discernment.md §Test-Questions item:
//   - exceed granted authority            (discernment Q1; stewardship "Requires Approval")
//   - modify identity / covenant          (discernment Q2; stewardship "Modify identity files")
//   - act without accountability          (discernment Q3; principles "Accountability")
//   - bypass the governor                 (discernment Q4; stewardship "bypass the action gate")
//   - expand own authority                (discernment Q5; principles "cannot expand your own authority")
//   - wirehead the reward                 (stewardship "Wirehead")
//   - override a structural refusal       (stewardship "Override a structural refusal")
//   - act outside the project sandbox     (stewardship "Act outside the project sandbox")
//
// Cues are matched against the lower-cased action text; ordered most-specific first so the returned
// reason is the tightest fit. The phrases are chosen to fire on an action that NAMES the violation
// ("bypass the governor", "expand my own authority"), not on benign mentions of the same nouns (a
// curiosity goal "explore an open question in science" carries none of these cues).
var hardProhibitions = []prohibition{
	{
		reason: "wireheading: optimizing the value/reward signal instead of the real goal is forbidden",
		cues:   []string{"wirehead", "optimize the value", "optimize the reward", "game the reward", "maximize the reward signal"},
	},
	{
		reason: "modifying identity/covenant files is forbidden (immutable by the agent)",
		cues:   []string{"modify my identity", "modify the identity", "modify my covenant", "rewrite my identity", "edit the covenant", "change my principles", "identity/principles", "identity/covenant", ".thought/identity"},
	},
	{
		reason: "bypassing the governor / the action gate is forbidden",
		cues:   []string{"bypass the governor", "bypass the action gate", "bypass the approval", "skip the approval gate", "bypass the governor's approval", "without the governor's approval"},
	},
	{
		reason: "expanding your own authority is forbidden (request elevation; the governor decides)",
		cues:   []string{"expand my own authority", "expand my authority", "grant myself", "elevate my own authority", "give myself authority"},
	},
	{
		reason: "exceeding granted authority is forbidden (request elevation; the governor decides)",
		cues:   []string{"exceed my granted authority", "exceed granted authority", "exceed my authority", "beyond my granted authority", "without authorization", "anyway despite the gate"},
	},
	{
		reason: "acting without accountability is forbidden (every action must be explainable; events stay on the bus)",
		cues:   []string{"without accountability", "skip the event log", "skip logging", "hide my reasoning", "without leaving a trace", "disable the audit"},
	},
	{
		reason: "overriding a structural refusal with a model judgment is forbidden",
		cues:   []string{"override the structural refusal", "override a structural refusal", "override the refusal", "ignore the structural refusal"},
	},
	{
		reason: "acting outside the project sandbox is forbidden",
		cues:   []string{"outside the project sandbox", "outside the sandbox", "escape the sandbox", "break out of the sandbox"},
	},
}

// VetAction is the conscience eval gate's deterministic FLOOR (§7.2, Pattern C): it refuses any
// action text matching a hard prohibition distilled from the identity system. Returns
// (allow=false, reason) on a match, else (allow=true, ""). The model CEILING (nuanced good/bad
// judgment) is escalated elsewhere, later — this is the floor only, so it makes NO model call and is
// deterministic. Every minted DRIVE goal and every action should pass through here before pursuit.
func VetAction(text string) (allow bool, reason string) {
	lt := strings.ToLower(text)
	for _, p := range hardProhibitions {
		for _, cue := range p.cues {
			if strings.Contains(lt, cue) {
				return false, p.reason
			}
		}
	}
	return true, ""
}

// conscienceFuzzyCues are SOFT cues — an action the floor ALLOWS but whose good/bad-ness is genuinely
// uncertain and worth a nuanced model look (§7.2 ceiling): a destructive or outward-facing effect. These
// are deliberately NOT hard prohibitions (most deletes/sends are fine); they only FLAG the case as
// escalation-eligible for the conscience model ceiling.
var conscienceFuzzyCues = []string{
	"delete", "remove ", "overwrite", "drop ", "wipe", "purge",
	"publish", "send to", "email", "post to", "upload", "share with", "external",
}

// ConscienceFuzzy reports whether an action the floor ALLOWED is flagged-fuzzy — a soft cue that warrants
// the conscience model CEILING's nuanced good/bad judgment (Pattern-C: the deterministic floor decides the
// clear cases; only a flagged-fuzzy case is escalated). A hard-prohibited action never reaches here (the
// floor already refused it).
func ConscienceFuzzy(text string) bool {
	lt := strings.ToLower(text)
	for _, cue := range conscienceFuzzyCues {
		if strings.Contains(lt, cue) {
			return true
		}
	}
	return false
}
