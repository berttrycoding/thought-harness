package campaign

// cogoracle.go — the OBJECTIVE OUTCOME ORACLE for the v2 faculty suite (the load-bearing validity fix).
//
// The legacy cognition probe scored a task purely on whether the faculty SIGNATURE fired (process
// detection) — outcome-decoupled and gameable. The v2 suite ties the faculty signal to a deterministic,
// offline-vettable ANSWER oracle so the gate becomes "faculty fired AND the outcome improved", not "fired".
//
// This mirrors internal/bench/realhard/oracle.go VERBATIM in SEMANTICS (the same four kinds, the same
// conservative decline rule, the same lure-assertion check). It is a SEPARATE copy on purpose: the campaign
// package CANNOT import realhard (realhard → ruler → campaign is an import cycle), and an independently
// auditable oracle is what the held-out discipline wants anyway. The two oracles are kept in lock-step by
// a shared mutation-test battery (cogoracle_test.go mirrors realhard/oracle_test.go): ground truth solves,
// the System-1 lure fails, an unmarked number on a decline question fails, an empty give-up fails.

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// the four oracle kinds (string values match the realhard.OracleKind values so a JSON suite is portable).
const (
	cogOracleExact      = "exact"
	cogOracleNumericTol = "numeric-tolerance"
	cogOracleSetMember  = "set-membership"
	cogOracleDecline    = "decline"
)

// outcomeVerdict is the scored outcome of one answer against one task's objective oracle.
type outcomeVerdict struct {
	Solved bool
	Reason string
	// AssertedLure is true when the answer asserted the task's PriorLure (the System-1 / confabulation
	// signal), reported even when Solved would be false for another reason.
	AssertedLure bool
}

// cogDeclineMarkers signal an honest "I cannot determine this from the material" — the correct move on a
// genuinely-unanswerable question. Mirrors realhard.declineMarkers AND the engine's own
// honestUnknownMarkers (the test double's "I couldn't work that out from what I know" surface declines
// honestly via "couldn't" — but a BARE empty is NOT a decline; see scoreCogDecline).
var cogDeclineMarkers = []string{
	"not determinable", "cannot be determined", "cannot determine", "can't determine",
	"not specified", "not defined", "no value", "not present", "not available",
	"not in the file", "not in this file", "not in the material", "no current",
	"i don't know", "i do not know", "unable to determine", "insufficient",
	"cannot verify", "can't verify", "not derivable", "no definition", "undefined here",
	"couldn't work that out", "could not work that out", "couldn't", "not found in",
	"isn't defined", "is not defined", "does not define", "depends on",
}

// scoreOutcome evaluates answer against the task's objective oracle and returns the verdict. Pure +
// deterministic (no model, no RNG, no clock) — offline-vettable and mutation-testable.
func scoreOutcome(t CognitionTask, answer string) outcomeVerdict {
	a := strings.TrimSpace(answer)
	switch t.Oracle {
	case cogOracleExact:
		return scoreCogExact(t, a)
	case cogOracleNumericTol:
		return scoreCogNumeric(t, a)
	case cogOracleSetMember:
		return scoreCogSet(t, a)
	case cogOracleDecline:
		return scoreCogDecline(t, a)
	default:
		return outcomeVerdict{Solved: false, Reason: "unknown oracle kind: " + t.Oracle}
	}
}

// scoreCogExact normalizes both sides per t.Normalizer and compares. For a "number" normalizer the answer
// may be a sentence; any number in it equal to the expected solves.
func scoreCogExact(t CognitionTask, answer string) outcomeVerdict {
	asserted := cogAssertsLure(t, answer)
	switch t.Normalizer {
	case "number":
		want, werr := strconv.ParseFloat(strings.TrimSpace(t.Expected), 64)
		if werr == nil {
			for _, n := range cogExtractNumbers(answer) {
				if n == want {
					return outcomeVerdict{Solved: true, Reason: "exact number match", AssertedLure: asserted}
				}
			}
		}
		return outcomeVerdict{Solved: false, Reason: "no number equals expected " + t.Expected, AssertedLure: asserted}
	case "token", "lower", "":
		na := cogNormToken(answer, t.Normalizer)
		ne := cogNormToken(t.Expected, t.Normalizer)
		if na == ne {
			return outcomeVerdict{Solved: true, Reason: "exact token match", AssertedLure: asserted}
		}
		for _, tok := range cogTokenize(answer) {
			if cogNormToken(tok, t.Normalizer) == ne {
				return outcomeVerdict{Solved: true, Reason: "expected token present", AssertedLure: asserted}
			}
		}
		return outcomeVerdict{Solved: false, Reason: "token mismatch: want " + t.Expected, AssertedLure: asserted}
	default:
		return outcomeVerdict{Solved: false, Reason: "unknown normalizer: " + t.Normalizer}
	}
}

// scoreCogNumeric accepts the answer if ANY number in it is within the absolute tolerance of Expected.
func scoreCogNumeric(t CognitionTask, answer string) outcomeVerdict {
	want, werr := strconv.ParseFloat(strings.TrimSpace(t.Expected), 64)
	asserted := cogAssertsLure(t, answer)
	if werr != nil {
		return outcomeVerdict{Solved: false, Reason: "task expected is not a number: " + t.Expected}
	}
	for _, n := range cogExtractNumbers(answer) {
		if math.Abs(n-want) <= t.Tolerance {
			return outcomeVerdict{Solved: true, Reason: "numeric within tolerance", AssertedLure: asserted}
		}
	}
	return outcomeVerdict{Solved: false, Reason: "no number within tol of " + t.Expected, AssertedLure: asserted}
}

// scoreCogSet requires the answer's token set to EQUAL the expected set (order-free, lowered): a missed
// member OR a hallucinated extra (within the closed candidate vocabulary) both fail.
func scoreCogSet(t CognitionTask, answer string) outcomeVerdict {
	want := map[string]bool{}
	for _, tok := range cogTokenize(strings.ToLower(t.Expected)) {
		want[tok] = true
	}
	got := map[string]bool{}
	for _, tok := range cogTokenize(strings.ToLower(answer)) {
		if want[tok] {
			got[tok] = true
		}
	}
	missing := false
	for w := range want {
		if !got[w] {
			missing = true
			break
		}
	}
	if !missing && len(got) == len(want) {
		return outcomeVerdict{Solved: true, Reason: "set equality"}
	}
	return outcomeVerdict{Solved: false, Reason: "set mismatch (missing or extra member)"}
}

// scoreCogDecline credits ONLY an explicit honest decline that does NOT assert the lure. A confident
// number (especially the lure) fails — that IS the confabulation. An EMPTY give-up FAILS (not an explicit
// decline) — crediting "" would score the early-give-up failure mode as correct cognition (the
// bench-oracle-doctor lesson). The test double's "I couldn't work that out from what I know" surface IS
// an explicit decline (the "couldn't" marker), so a genuinely-absent anti-confab task it cannot answer
// SOLVES — which is exactly the correct anti-confabulation behaviour.
func scoreCogDecline(t CognitionTask, answer string) outcomeVerdict {
	asserted := cogAssertsLure(t, answer)
	if asserted {
		return outcomeVerdict{Solved: false, Reason: "asserted the lure value (confabulation)", AssertedLure: true}
	}
	low := strings.ToLower(answer)
	for _, m := range cogDeclineMarkers {
		if strings.Contains(low, m) {
			return outcomeVerdict{Solved: true, Reason: "honest decline (marker: " + m + ")"}
		}
	}
	if len(cogExtractNumbers(answer)) > 0 {
		return outcomeVerdict{Solved: false, Reason: "emitted a number on an unanswerable question (confabulation)"}
	}
	if strings.TrimSpace(answer) == "" {
		return outcomeVerdict{Solved: false, Reason: "empty answer (give-up / no commitment) — not an explicit honest decline"}
	}
	return outcomeVerdict{Solved: false, Reason: "no decline marker and no number — ambiguous, scored fail"}
}

// cogAssertsLure reports whether the answer asserts the task's PriorLure as its value (numeric: the lure
// is the headline/last number; token: the lure token is present). Empty lure ⇒ never asserted.
func cogAssertsLure(t CognitionTask, answer string) bool {
	if strings.TrimSpace(t.PriorLure) == "" {
		return false
	}
	if lure, err := strconv.ParseFloat(strings.TrimSpace(t.PriorLure), 64); err == nil {
		nums := cogExtractNumbers(answer)
		if len(nums) == 0 {
			return false
		}
		last := nums[len(nums)-1]
		if last == lure {
			return true
		}
		if len(nums) == 1 && nums[0] == lure {
			return true
		}
		return false
	}
	ll := strings.ToLower(strings.TrimSpace(t.PriorLure))
	for _, tok := range cogTokenize(strings.ToLower(answer)) {
		if tok == ll {
			return true
		}
	}
	return false
}

// ---- normalization helpers (mirror realhard/oracle.go) ----------------------

var cogNumberRe = regexp.MustCompile(`-?\d{1,3}(?:,\d{3})+(?:\.\d+)?|-?\d+(?:\.\d+)?`)

func cogExtractNumbers(s string) []float64 {
	matches := cogNumberRe.FindAllString(s, -1)
	out := make([]float64, 0, len(matches))
	for _, m := range matches {
		clean := strings.ReplaceAll(m, ",", "")
		if v, err := strconv.ParseFloat(clean, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func cogNormToken(s, normalizer string) string {
	s = strings.TrimSpace(s)
	switch normalizer {
	case "lower", "token":
		return strings.ToLower(strings.Trim(s, ".,;:!?\"'`()[]{}"))
	default:
		return strings.Trim(s, ".,;:!?\"'`()[]{}")
	}
}

var cogTokenSplitRe = regexp.MustCompile(`[\s,;]+`)

func cogTokenize(s string) []string {
	parts := cogTokenSplitRe.Split(s, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, ".,;:!?\"'`()[]{}")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
