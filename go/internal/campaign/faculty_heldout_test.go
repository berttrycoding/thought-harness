package campaign

import (
	"strings"
	"testing"
)

// faculty_heldout_test.go — HELD-OUT-CONSTRUCTION discipline for the v2 faculty suite (directive gate #4):
// the tasks must NOT be string-matchable to a specific faculty-LEVER skill's trigger/example phrasing, so a
// later faculty-lever A/B measures ELICITATION GENERALIZATION (does the faculty engage on a novel surface),
// NOT string-match INJECTION (the candidate skill's own example phrasing was copied into the task). This is
// the W3-S3 / offer-enrichment lesson encoded as a guard (feedback-enrich-descriptions-heldout-test).
//
// THE CONTRACT. A faculty lever (a minted skill / an enriched operator description / a softmax-policy knob)
// keys on META-COGNITION JARGON — words that NAME the cognitive move the lever drives. If a probe task
// literally contains that jargon, the lever would fire by KEYWORD reflex and the A/B would overstate the
// lift. So a v2 task's GOAL must elicit the faculty through its STRUCTURE (a genuine fork, a real multi-part
// goal, a System-1 trap, a genuine absence), never by naming the move. The banned vocabulary below is the
// lever-injection meta-jargon; the elicitation framing (safe/ship/refactor for branch; the listed parts for
// decompose; the arithmetic for deliberate; the unknowable fact for honest) is the DEFAULT-config dispatch
// surface, which is legitimate and NOT banned (those are the engine's built-in triggers, not a lever's).

// bannedLeverJargon are the meta-cognition trigger words a faculty-LEVER skill would key on. A v2 task's
// goal must not name the move with these — it must elicit it structurally. Lower-cased; matched as
// whole-word-ish substrings on the goal.
var bannedLeverJargon = []string{
	// branch-lever jargon (the lever that drives the fork faculty)
	"branch", "fork", "explore alternatives", "consider both options", "weigh both sides",
	"two branches", "alternative branch",
	// decompose-lever jargon (the lever that drives the decomposition faculty)
	"decompose", "decomposition", "sub-goals", "subgoals", "break it down using",
	// deliberate-lever jargon
	"deliberate", "deliberation", "think step by step", "chain of thought", "reason carefully",
	// honest / anti-confab-lever jargon
	"do not confabulate", "anti-confabulation", "refuse to hallucinate", "epistemic",
	// generic lever/skill naming
	"use the skill", "apply the operator", "the workflow program", "synthesize a program",
}

// TestFacultyHeldOutKeywordHygiene is the load-bearing INTEGRITY check: no v2 task's GOAL may contain a
// banned lever-injection jargon word. If this fails, the suite is no longer a clean generalization probe (a
// faculty lever could pass it by keyword reflex) and the goal MUST be reworded to elicit the faculty
// structurally instead of naming the move.
func TestFacultyHeldOutKeywordHygiene(t *testing.T) {
	for _, tk := range FacultySuite() {
		goal := strings.ToLower(tk.Goal)
		for _, kw := range bannedLeverJargon {
			if strings.Contains(goal, kw) {
				t.Errorf("[%s] GOAL contains banned lever-injection jargon %q — the suite must elicit the "+
					"faculty STRUCTURALLY, not by naming the move (reword the goal)", clipGoal(tk.Goal), kw)
			}
		}
	}
}

// TestFacultyNotesNamingIsFine documents that the JARGON BAN applies to the GOAL only, not the Note. A Note
// (the design rationale, never fed to the engine) may freely say "decompose" / "branch" — only the GOAL the
// engine sees must be jargon-free. This test asserts at least one Note legitimately uses the jargon (so a
// future over-zealous "ban everywhere" refactor that scrubs the Notes is caught).
func TestFacultyNotesNamingIsFine(t *testing.T) {
	jargonInANote := false
	for _, tk := range FacultySuite() {
		low := strings.ToLower(tk.Note)
		if strings.Contains(low, "decompose") || strings.Contains(low, "fork") || strings.Contains(low, "deliberate") {
			jargonInANote = true
			break
		}
	}
	if !jargonInANote {
		t.Errorf("no Note uses the design jargon — the ban must scope to the GOAL only, not strip the Notes")
	}
}

// TestFacultyGoalsAreDistinct guards against trivial near-duplicates (a suite of 28 copies of one phrasing
// would game the spread). Every goal must be unique and the suite must use varied surface forms per faculty.
func TestFacultyGoalsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, tk := range FacultySuite() {
		g := strings.ToLower(strings.TrimSpace(tk.Goal))
		if seen[g] {
			t.Errorf("duplicate goal %q", clipGoal(tk.Goal))
		}
		seen[g] = true
	}
}

// TestFacultyDeclineGoalsHaveNoBannedTrapKeywords mirrors the realhard held-out banned-keyword hygiene for
// the anti-confab subset: a genuine-absence task must not telegraph the decline with the realhard in-suite
// trap keywords (deprecated/superseded/erratum), so a lift on it proves the general anti-confab capability,
// not a keyword reflex.
func TestFacultyDeclineGoalsHaveNoBannedTrapKeywords(t *testing.T) {
	banned := []string{"deprecated", "superseded", "erratum"}
	for _, tk := range FacultySuite() {
		if tk.Oracle != cogOracleDecline {
			continue
		}
		low := strings.ToLower(tk.Goal)
		for _, kw := range banned {
			if strings.Contains(low, kw) {
				t.Errorf("[%s] decline goal contains realhard trap keyword %q — reword to elicit the "+
					"absence structurally", clipGoal(tk.Goal), kw)
			}
		}
	}
}
