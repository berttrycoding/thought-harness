package realhard

import (
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/engine"
)

// canonical.go — the deliberative VOTE-EQUIVALENCE KEY for the realhard suite.
//
// THE DEFECT it fixes. The deliberative robustness lever (THOUGHT_DELIBERATIVE_K) reconciles K
// independent episodes by majority-voting on a normalized answer. With the engine's coarse
// NormalizeAnswer (lower + collapse whitespace) as the vote key, K episodes that reach the SAME
// conclusion in different phrasings ("After tracing, the answer is 12." / "The pool is 12
// connections." / "I computed 12.") become K DISTINCT vote keys → no majority → a K-way tie → the
// V(s) tie-break fires. That degenerates the mechanism to best-of-N-by-V(s) (V(s) is the harness's
// own noisy value signal, NOT a correctness oracle), so it does not concentrate and it made
// rock-solid p=1.0 tasks go flippy in the σ_R gate.
//
// THE FIX. The vote key must match the SCORING notion of "same answer" — the same equivalence the
// oracle (Score) uses to credit an answer. canonicalAnswer mirrors that oracle equivalence per
// Task.Oracle, REUSING the oracle helpers (extractNumbers / declineMarkers / tokenize / normToken),
// so two episodes the oracle would score identically also VOTE together — restoring real
// self-consistency. It is the only behavioral arms.go change beyond threading the param: arms.go
// passes func(a string){ return canonicalAnswer(task, a) } into engine.RunDeliberative.
//
// It is NOT the oracle: it never decides Solved. It only groups equivalent answers so the majority
// forms. (The oracle still scores the reconciled winner.)
func canonicalAnswer(task Task, answer string) string {
	switch task.Oracle {
	case OracleNumericTolerance:
		// Numeric tasks score on a number within tolerance; group on the LAST extracted number's
		// canonical form (the conclusion a trajectory lands on). No number → fall back to the coarse key.
		return canonicalLastNumber(answer)
	case OracleExact:
		switch task.Normalizer {
		case "number":
			// Exact-number: same equivalence as numeric — the LAST number canonicalized.
			return canonicalLastNumber(answer)
		case "squad-em", "em":
			// SQuAD-EM exact: the oracle compares the whole-span squad-em normalization; group on that so
			// two phrasings the oracle would score identically also vote together. Empty (a non-answer)
			// falls back to the coarse phrasing key.
			if k := strings.TrimSpace(normSquadEM(answer)); k != "" {
				return k
			}
			return engine.NormalizeAnswer(answer)
		case "token", "lower", "":
			// Token/lower exact: the oracle compares the canonical normToken; group on that. (normToken
			// on the whole answer canonicalizes punctuation/case the same way the oracle does for the
			// expected token; a multi-token answer that contains the expected token still groups with
			// another answer that contains it because normToken is applied to the same whole-string
			// input on both — and where it does not, the coarse fallback keeps near-identical phrasings
			// together.)
			if tok := strings.TrimSpace(normToken(answer, task.Normalizer)); tok != "" {
				return tok
			}
			return engine.NormalizeAnswer(answer)
		default:
			return engine.NormalizeAnswer(answer)
		}
	case OracleSetMembership:
		// Set tasks score on the relevant token SET (order-free, lowered); group on the sorted lowered
		// token set joined — two answers with the same membership (any order) vote together.
		toks := tokenize(strings.ToLower(answer))
		sort.Strings(toks)
		return strings.Join(toks, " ")
	case OracleDecline:
		// Decline tasks separate an HONEST decline (the SOLVE group) from a confident-number
		// confabulation (the FAIL group). An honest decline commits to NO numeric value; a decline
		// marker that ALSO states a number is a hedged confabulation (the oracle FAILS it when that
		// number is the lure), so it must NOT pool with clean declines. The key therefore is:
		//   - marker present AND no number  -> "DECLINE" (clean honest decline; SOLVE-equivalent)
		//   - a number present (with or without a marker) -> that number (committed/confabulated value;
		//     same-number confabulations cluster, and a lure-citing "decline" lands here, NOT in DECLINE)
		//   - no marker and no number       -> the coarse phrasing key (an ambiguous/empty non-answer the
		//     oracle also fails; kept OUT of the DECLINE group so it cannot ride an honest-decline majority)
		// This mirrors the oracle's SOLVE/FAIL split WITHOUT reading the lure value (no Task.PriorLure /
		// Task.Expected — the key stays a pure equivalence, never the answer).
		nums := extractNumbers(answer)
		low := strings.ToLower(answer)
		hasMarker := false
		for _, m := range declineMarkers {
			if strings.Contains(low, m) {
				hasMarker = true
				break
			}
		}
		if len(nums) > 0 {
			return canonicalNumber(nums[len(nums)-1])
		}
		if hasMarker {
			// declineVoteKey (oracle_graded.go) — the SAME literal the decline-ordinal
			// graded scorer keys on, so the graded score and this vote key are one notion
			// of "same answer" (the red-team's coherence fence).
			return declineVoteKey
		}
		return engine.NormalizeAnswer(answer)
	default:
		return engine.NormalizeAnswer(answer)
	}
}

// canonicalLastNumber returns the LAST extracted number's canonical string form, or the coarse
// NormalizeAnswer when the answer has no number (so a no-number answer still groups by phrasing).
func canonicalLastNumber(answer string) string {
	nums := extractNumbers(answer)
	if len(nums) == 0 {
		return engine.NormalizeAnswer(answer)
	}
	return canonicalNumber(nums[len(nums)-1])
}

// canonicalNumber renders a float to a stable canonical string (so 12, 12.0, "12,0"-thousands all
// canonicalize to "12"). Uses the shortest round-trippable form.
func canonicalNumber(n float64) string {
	return strconv.FormatFloat(n, 'g', -1, 64)
}
