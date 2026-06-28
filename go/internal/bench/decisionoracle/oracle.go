package decisionoracle

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Score is one fixture's decision/ship verdict: the [0,1] Score, the Pass decision
// (Score >= the fixture's effective threshold), the per-axis breakdown, and a one-line
// Reason. Decided=false (with Reason set) when no verdict could be extracted — a hard
// fail, never a silent pass.
type Score struct {
	// Score is the verdict correctness in [0,1].
	Score float64
	// Pass is Score >= the fixture's effective PassThreshold AND a verdict was extracted.
	Pass bool
	// Decided is false when the worker produced no extractable verdict (no pick / no
	// accept-refuse) — a hard fail (Score 0), never a vacuous pass.
	Decided bool
	// Axes is the per-axis credit [0,1] (the "correct" axis and, when constrained, the
	// "sound" reasoning axis), keyed for the report.
	Axes map[string]float64
	// Reason is a compact human-readable explanation for the ledger pointer.
	Reason string
}

// Default pass thresholds per worker. A Deliberator pick is binary on the CORRECT axis
// (right option or not) but the realised score blends in the soundness axis. With the
// front-loaded weights (wCorrect=3, wSound=1, renormalised), a RIGHT pick with NO cited
// reason scores exactly 3/4 = 0.75 and so PASSES the 0.75 bar (the `>=` is inclusive),
// while a WRONG pick scores 0 (correct=0 caps it) regardless of how well-reasoned. The
// soundness axis is therefore a TIE-BREAK/CREDIT signal — a reasoned right pick scores a
// clean 1.0 above the bar — not a gate that a bare right pick must clear; a fixture that
// wants to REQUIRE cited reasoning raises its own pass_threshold above 0.75. A Verifier
// verdict is scored the same way; the dangerous false-accept is capped at 0 (it can never
// reach the bar no matter how plausible the rationale).
const (
	defaultDeliberatorThreshold = 0.75
	defaultVerifierThreshold    = 0.75
)

// axisWeights front-loads the CORRECT axis (the verdict matching ground truth is what
// matters) with the SOUND axis as supporting signal (right answer, right reason). They
// need not sum to 1 — the score renormalises over the axes a fixture actually scores.
const (
	wCorrect = 3.0
	wSound   = 1.0
)

// axisEntry is one scored axis (the "correct" or "sound" credit) the blend folds into
// the weighted-mean score.
type axisEntry struct {
	name   string
	weight float64
	credit float64
}

// ScoreVerdict is the oracle: it scores a worker's extracted Verdict against the
// fixture's ground truth, deterministically and offline. A missing verdict is a hard
// fail (Decided=false, Score 0). The score is the weighted mean of the scored axes,
// renormalised to [0,1].
func ScoreVerdict(v Verdict, fx Fixture) Score {
	switch fx.Worker {
	case WorkerDeliberator:
		return scoreDeliberator(v, fx)
	case WorkerVerifier:
		return scoreVerifier(v, fx)
	default:
		return Score{Axes: map[string]float64{}, Reason: "unknown worker " + string(fx.Worker) + " (bank error — never a vacuous pass)"}
	}
}

// BetterOption returns the ground-truth winner: the option with the highest weighted
// sum of its per-criterion Scores under CriteriaWeights. ok=false when the fixture has
// no options or no weights (a bank error). On a tie it returns the first-by-ID winner
// deterministically and tied=true (the fixture should avoid ties — caught by the bank
// soundness test). This is a PURE function the discrimination test re-derives, so the
// "winner" is computed, never an unchecked authored label.
func BetterOption(fx Fixture) (winner string, tied bool, ok bool) {
	if len(fx.Options) == 0 || len(fx.CriteriaWeights) == 0 {
		return "", false, false
	}
	type scored struct {
		id    string
		total float64
	}
	ranked := make([]scored, 0, len(fx.Options))
	for _, o := range fx.Options {
		total := 0.0
		for crit, w := range fx.CriteriaWeights {
			total += w * o.Scores[crit] // a missing criterion scores 0 on that option
		}
		ranked = append(ranked, scored{o.ID, total})
	}
	// Sort descending by total; ties broken by id ascending so the result is stable.
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].total != ranked[j].total {
			return ranked[i].total > ranked[j].total
		}
		return ranked[i].id < ranked[j].id
	})
	tied = len(ranked) > 1 && almostEqual(ranked[0].total, ranked[1].total)
	return ranked[0].id, tied, true
}

// scoreDeliberator scores a trade-off verdict: did the worker PICK the ground-truth
// better option, and did it cite a discriminating criterion (the soundness axis)?
//
// A fixture declaring CorrectVerdict=="undecided" is GENUINELY TIED: the honest "undecided"
// abstain (v.Undecided) is the CORRECT answer there, and a confident pick of EITHER option is the
// failure (a fabricated decision). On a fixture with a determinable winner the inverse holds: an
// "undecided" abstain scores 0 (it declined a decidable call — Decided=true, it is a stated
// stance, just the wrong one), and the right pick wins.
func scoreDeliberator(v Verdict, fx Fixture) Score {
	axes := map[string]float64{}
	winner, tied, ok := BetterOption(fx)
	if !ok {
		return Score{Axes: axes, Reason: "deliberator fixture has no options/weights (bank error — never a vacuous pass)"}
	}

	wantUndecided := fx.CorrectVerdict == "undecided"
	if wantUndecided {
		// A genuinely-tied fixture: it MUST actually tie under its weights (else the bank is
		// inconsistent — it declared "undecided" but the math has a winner). Never a vacuous pass.
		if !tied {
			return Score{Axes: axes, Reason: "fixture declares correct_verdict=undecided but its options do NOT tie under the weights (bank error)"}
		}
		if v.Undecided {
			// The honest abstain on a genuinely-tied decision — the CORRECT answer.
			axes["correct"] = 1.0
			score, reason := blend([]axisEntry{{"correct", wCorrect, 1.0}})
			threshold := fx.PassThreshold
			if threshold <= 0 {
				threshold = defaultDeliberatorThreshold
			}
			return Score{Score: score, Pass: score >= threshold, Decided: true, Axes: axes,
				Reason: fmt.Sprintf("genuinely-tied -> honest UNDECIDED (correct) score=%s thr=%s -> %s | %s",
					trim2(score), trim2(threshold), passWord(score >= threshold), reason)}
		}
		pick := strings.TrimSpace(v.PickedOption)
		if pick == "" {
			// No pick AND not an honest "undecided" — the worker declined entirely. Hard fail.
			return Score{Decided: false, Axes: axes, Reason: "tied fixture: worker neither picked nor stated undecided (declined — hard fail)"}
		}
		// A confident pick on a genuinely-tied decision is a FABRICATED decision — wrong (0), but
		// it IS a stated stance (Decided=true), never a vacuous pass.
		return Score{Decided: true, Score: 0, Axes: map[string]float64{"correct": 0},
			Reason: fmt.Sprintf("tied fixture: fabricated pick %q on a genuine tie (correct=0; the honest answer is undecided)", pick)}
	}

	// A fixture with a DETERMINABLE winner: a tie here is a bank error (the math has no winner but
	// the fixture did not declare itself undecided).
	if tied {
		return Score{Axes: axes, Reason: "deliberator fixture options tie under its weights (bank error — no determinable winner; set correct_verdict=undecided if intended)"}
	}
	if v.Undecided {
		// Abstained on a DECIDABLE fixture — it made an honest call (Decided=true) but the wrong
		// one (there was a determinable winner). Scores 0, never a vacuous pass.
		return Score{Decided: true, Score: 0, Axes: map[string]float64{"correct": 0},
			Reason: fmt.Sprintf("stated UNDECIDED on a decidable fixture (winner=%s; correct=0)", winner)}
	}
	pick := strings.TrimSpace(v.PickedOption)
	if pick == "" {
		// No pick extracted — the worker declined to decide. Hard fail, not a pass.
		return Score{Decided: false, Axes: axes, Reason: "no option picked (worker declined to decide — hard fail)"}
	}
	pickedID, matched := matchOption(pick, fx.Options)
	if !matched {
		return Score{Decided: true, Score: 0, Axes: map[string]float64{"correct": 0},
			Reason: fmt.Sprintf("picked %q matches no option (correct=0)", pick)}
	}

	correct := 0.0
	if pickedID == winner {
		correct = 1.0
	}
	axes["correct"] = correct

	entries := []axisEntry{{"correct", wCorrect, correct}}

	// Soundness axis: only when the fixture names discriminating criteria. A reasoned
	// pick references at least one of them; a bare pick does not.
	if len(fx.DiscriminatingCriteria) > 0 {
		sound := 0.0
		hit := citesAny(v.Reasoning, fx.DiscriminatingCriteria)
		if hit != "" {
			sound = 1.0
		}
		axes["sound"] = sound
		entries = append(entries, axisEntry{"sound", wSound, sound})
	}

	score, reason := blend(entries)
	threshold := fx.PassThreshold
	if threshold <= 0 {
		threshold = defaultDeliberatorThreshold
	}
	pass := score >= threshold
	return Score{
		Score:   score,
		Pass:    pass,
		Decided: true,
		Axes:    axes,
		Reason: fmt.Sprintf("pick=%s winner=%s score=%s thr=%s -> %s | %s",
			pickedID, winner, trim2(score), trim2(threshold), passWord(pass), reason),
	}
}

// scoreVerifier scores a ship verdict against the claim's ground-truth status. The
// correct axis is a confusion-matrix credit: a correct accept/refuse scores 1, a
// false-refuse of a true claim scores 0, and the dangerous FALSE-ACCEPT of a false
// claim scores 0 (and can never be rescued by the soundness axis — it caps the score).
// On an unknowable claim, an HONEST refuse scores 1; a confident accept or a
// counter-claiming "refuse" scores 0.
func scoreVerifier(v Verdict, fx Fixture) Score {
	axes := map[string]float64{}
	if fx.Truth == "" {
		return Score{Axes: axes, Reason: "verifier fixture has no ground-truth Truth (bank error — never a vacuous pass)"}
	}
	if v.Decision == "" {
		return Score{Decided: false, Axes: axes, Reason: "no accept/refuse verdict (worker declined — hard fail)"}
	}

	correct := 0.0
	dangerousFalseAccept := false
	switch fx.Truth {
	case ClaimTrue:
		if v.Decision == DecisionAccept {
			correct = 1.0
		}
	case ClaimFalse:
		if v.Decision == DecisionRefuse {
			correct = 1.0
		} else { // accepted a FALSE claim — the ship-a-bug failure
			dangerousFalseAccept = true
		}
	case ClaimUnknowable:
		// The right move is an HONEST refusal: refuse AND flag insufficient evidence.
		if v.Decision == DecisionRefuse && v.Honest {
			correct = 1.0
		} else if v.Decision == DecisionAccept {
			dangerousFalseAccept = true // accepted a claim it cannot actually verify
		}
		// refuse-but-not-honest (asserting a counter-claim it cannot ground) scores 0
		// without being flagged dangerous — wrong stance, but it did block the ship.
	default:
		return Score{Axes: axes, Reason: "verifier fixture has unknown Truth " + string(fx.Truth)}
	}
	axes["correct"] = correct

	entries := []axisEntry{{"correct", wCorrect, correct}}

	// Soundness axis: did the verdict cite the settling evidence?
	if len(fx.Evidence) > 0 {
		sound := 0.0
		if citesAny(v.Reasoning, fx.Evidence) != "" {
			sound = 1.0
		}
		axes["sound"] = sound
		entries = append(entries, axisEntry{"sound", wSound, sound})
	}

	score, reason := blend(entries)
	// A dangerous false-accept caps the score at 0 — a sound-sounding rationale must
	// NEVER lift a ship-the-bug verdict over the bar.
	if dangerousFalseAccept {
		score = 0
		reason = "DANGEROUS false-accept (shipped a false/unverifiable claim) — capped at 0; " + reason
	}
	threshold := fx.PassThreshold
	if threshold <= 0 {
		threshold = defaultVerifierThreshold
	}
	pass := score >= threshold
	return Score{
		Score:   score,
		Pass:    pass,
		Decided: true,
		Axes:    axes,
		Reason: fmt.Sprintf("truth=%s decision=%s honest=%v score=%s thr=%s -> %s | %s",
			fx.Truth, v.Decision, v.Honest, trim2(score), trim2(threshold), passWord(pass), reason),
	}
}

// blend computes the weighted-mean score over the scored axes (renormalised to [0,1])
// and the per-axis note string. A caller that passes no axes is a bank error (handled
// upstream); here an empty entries slice scores 0.
func blend(entries []axisEntry) (float64, string) {
	var sumW, sumWC float64
	notes := make([]string, 0, len(entries))
	for _, e := range entries {
		sumW += e.weight
		sumWC += e.weight * e.credit
		notes = append(notes, e.name+"="+trim2(e.credit))
	}
	score := 0.0
	if sumW > 0 {
		score = sumWC / sumW
	}
	sort.Strings(notes)
	return score, strings.Join(notes, " ")
}

// matchOption resolves a worker's free-text pick to a fixture option ID, matching
// case- and space-insensitively against each option's ID and Name. Returns
// (id, true) on a unique match. A pick that contains an option's ID/Name as a token
// also matches (so "I'd pick option A (mutex)" resolves to A). On no match (id, false).
func matchOption(pick string, options []Option) (string, bool) {
	norm := normalize(pick)
	// Exact-normalised match first (the strongest).
	for _, o := range options {
		if norm == normalize(o.ID) || (o.Name != "" && norm == normalize(o.Name)) {
			return o.ID, true
		}
	}
	// Token-contains match: the pick prose names the option. Guard against a substring
	// collision (e.g. id "a" inside "approach") by matching on whole normalised tokens.
	// A multi-word Name is located as a CONTIGUOUS token subsequence (D1), not by a
	// single-token scan that can never match it, so a pick stated as a multi-word Name
	// (e.g. "incremental refactor") resolves to its ID like a single-word one.
	pickToks := tokenize(pick)
	picked := ""
	hits := 0
	for _, o := range options {
		if optionPresent(pickToks, o) {
			picked = o.ID
			hits++
		}
	}
	if hits == 1 {
		return picked, true
	}
	return "", false
}

// citesAny returns the first of the given phrases that appears (case-insensitively) in
// the reasoning text, or "" if none does. Used for the soundness axis: did the verdict
// reference a discriminating criterion / the settling evidence?
func citesAny(reasoning string, phrases []string) string {
	low := strings.ToLower(reasoning)
	for _, p := range phrases {
		if p == "" {
			continue
		}
		if strings.Contains(low, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

// containsToken reports whether want appears as a whole space/punct-delimited token in
// text (case-insensitive) — so a 1-char option id "a" matches "option a" but not the
// "a" inside "approach".
func containsToken(text, want string) bool {
	if want == "" {
		return false
	}
	wantN := normalize(want)
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if tok == wantN {
			return true
		}
	}
	return false
}

// normalize lowercases and strips surrounding whitespace + trailing punctuation for a
// stable comparison of option labels.
func normalize(s string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(s)), " .,:;!?\"'()")
}

// almostEqual reports whether two weighted-sum totals are within a tiny epsilon (a tie
// in the deliberator ranking).
func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// trim2 renders a float to 2 decimals for the report.
func trim2(f float64) string { return fmt.Sprintf("%.2f", f) }

// passWord renders a pass/fail token for the ledger pointer.
func passWord(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}
