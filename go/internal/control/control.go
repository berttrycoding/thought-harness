// Package control holds the deterministic CONTROL floor — the admission scoring, the candidate
// ranking, and the floor math the architecture runs WITHOUT a model. It is a Tier-1 leaf:
// it imports only internal/types (+ stdlib math/sort/strings). It NEVER imports internal/backends
// and NEVER references the test double. The production control path (the seam's Gate, the Filter
// floor) calls these directly; the test double (TestBackend) also delegates here, so floor and
// production share ONE implementation of the floor.
//
// Pattern A (pure CONTROL): the math here is a closed-form function of the candidates / history /
// value — no language understanding required, so it never calls a model. The functions are LIFTED
// verbatim from backends/test.go (the source priors, the signal terms, the Neumaier-compensated
// insertion-ordered sum, the round3/roundf64 helpers, the verdict bands, the Rank scoring); only
// their HOME changed. The math is byte-identical so the golden scenarios stay byte-identical.
package control

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// Hedges are markers the control floor reads as low-trust signals (laundered-hallucination guard).
// OverConfidentMarkers — over-confident phrasing is itself a mild red flag (S8).
var (
	Hedges               = []string{"maybe", "perhaps", "might", "unsure", "guess", "probably", "i think"}
	OverConfidentMarkers = []string{"definitely", "certainly", "clearly", "obviously"}
)

// ungroundedAssertionPenalty is the (display-only) signal weight the floor stamps on a thought that
// ASSERTS an observed/read RESULT it never actually observed (the "I read the file, it's 10"
// hallucination — truth 6, lure 8). It is a STRUCTURAL fact like refuted_by_reality: when the signal is
// present the verdict is FORCED to REJECT regardless of the score (the model may not override a claim of
// reality with no reality behind it). The weight is large+negative so the reason line names it; the
// verdict override (not the sum) is what kills the thought, so the score math for every OTHER candidate
// is byte-identical (the signal only ever appears on the narrow lure case, never in the goldens).
const ungroundedAssertionPenalty = -0.60

// deniedAccessPenalty is the (display-only) signal weight stamped on a thought that DENIES it can reach
// reality ("I don't have filesystem access", "I can't read files", "type /mcp") when the loop has in fact
// ALREADY reached reality (a real grounded observation is in recent history). Like asserts_ungrounded_
// observation it is a STRUCTURAL fact — the candidate contradicts a capability the loop just demonstrated,
// so the verdict is FORCED to REJECT regardless of the score (a tool-affordance hallucination must not be
// voiced as the conscious's own next thought). The weight is large+negative so the reason line names it;
// the verdict override (not the sum) is what kills it, so every OTHER candidate's score is byte-identical
// (the signal only appears on this narrow refusal, never in the goldens).
const deniedAccessPenalty = -0.60

// reDeniesAccess matches the tool-affordance REFUSAL — a first-person claim that the conscious cannot reach
// the workspace/files/tools (the #43 "no filesystem access" / "type /mcp" hallucination). It is the OTHER
// face of the grounding-integrity guard: asserts_ungrounded_observation kills a FABRICATED observation;
// this kills a DENIAL of the (demonstrably available) means to observe. Deliberately TIGHT — it requires
// a denial verb ("don't/cannot/can't/no/wasn't able to/unable to/lack") next to an access/tool/file noun,
// or the literal "/mcp" affordance-instruction the model emits when it forgets it already has tools.
var reDeniesAccess = regexp.MustCompile("(?i)\\b(?:don't|do not|cannot|can't|can not|couldn't|could not|wasn't able to|was not able to|unable to|no|lack(?:ing)?)\\b[^.!?\\n]*\\b(?:access|filesystem|file system|read (?:the )?files?|shell access|tool access|directly read|read the file contents)\\b|\\btype\\s+`?/mcp\\b|\\bno (?:direct )?(?:shell|filesystem|file) access\\b")

// observationCues are first-person READ/OBSERVE verbs tied to an external artifact — the "I looked at
// reality" half of an asserted observation. Lowercased substrings; deliberately TIGHT so a plan ("I will
// read the file") or hedged reasoning ("if I read it, it might be 10") does not trip the guard on its own
// (the result cue + the non-hedged + no-grounding gates below all must ALSO hold).
var observationCues = []string{
	"i read", "i checked", "i looked at", "i observed", "i ran", "i measured", "i inspected",
	"i opened", "i examined", "reading the file", "checking the file",
}

// resultCues are tokens that present a CONCRETE observed RESULT — the "and it is N / it contains X"
// half. A bare read verb with no concrete result is just narration; a concrete-result claim ATTRIBUTED to
// a read is the lure. Tight on purpose (definite "is/are/equals/contains/returned/says" result phrasing).
var resultCues = []string{
	"it is ", "it's ", "it was ", "it says ", "it contains ", "it returned ", "it equals ",
	"the value is ", "the value was ", "the result is ", "the result was ", "the output is ",
	"the output was ", "the contents are ", "the file contains ", "the file says ", "equals ",
	"returned ", "evaluates to ", "the answer is ",
}

// reArtifactRef matches an EXTERNAL-artifact reference as a WHOLE WORD (word boundaries are load-bearing:
// "log" must NOT match "logic", "file" must NOT match "profile" — the substring bug an earlier pass hit).
// The ungrounded-observation guard fires ONLY when the asserted observation names such an external artifact,
// so internal reasoning ("I checked the logic and it is sound", "I ran through the steps and it's fine")
// NEVER trips it. External-only nouns — never "logic / steps / reasoning / math / proof / argument".
var reArtifactRef = regexp.MustCompile(`\b(?:file|config|yaml|yml|json|toml|output|stdout|stderr|log|logs|contents|document|readme|entrypoint|dockerfile|makefile|script)\b`)

// reArtifactPath matches a file-path-like token (a name with a code/config extension) — the OTHER way
// an asserted observation names a concrete external artifact ("I read config/limits.go and it's 10").
var reArtifactPath = regexp.MustCompile(`[\w./-]+\.(?:go|py|yaml|yml|md|txt|json|toml|sh|js|ts|rs|c|h|cpp|hpp|java|rb|cfg|ini|conf|env|csv|xml|lock|mod|sum)\b`)

// referencesExternalArtifact reports whether the text names a concrete EXTERNAL artifact (a file/output/
// log/etc. as a whole word, or a file-path token). It is the gate that keeps the ungrounded-observation
// guard off ordinary internal reasoning — only a claim of having observed an EXTERNAL thing can be the lure.
func referencesExternalArtifact(low string) bool {
	return reArtifactRef.MatchString(low) || reArtifactPath.MatchString(low)
}

// Appraiser is the name the deterministic floor stamps onto its verdicts/emits (the P6 "who
// appraised" tag). It is the SINGLE source of truth for the floor appraiser string: the Filter
// floor verdict (ScoreAdmit's Source) and the Gate's deterministic-rank appraiser both read it, so
// they always agree. It was held at "heuristic" through M1–M5 to keep the goldens byte-identical;
// in M6 it flipped to "control" (the truthful name — the deterministic CONTROL floor, not a
// second-rate guess) and the scenario goldens were regenerated with that string-only delta.
const Appraiser = "control"

// ---------------------------------------------------------------------------
// Filter admission scoring (the Pattern-C deterministic FLOOR)
// ---------------------------------------------------------------------------

// ScoreAdmit is the Filter admission FLOOR: validate a RAW candidate before it is voiced. It
// captures the SIGNAL BREAKDOWN that produces the score (P6 structured "why"), rather than
// collapsing it into one number — the score is their sum, so the math is byte-identical to a
// plain prior+value; we just stop discarding the terms. This is the DETERMINISTIC FLOOR of the
// Filter's Pattern-C escalation; NO model is ever consulted here.
//
// The returned verdict's Source is the floor Appraiser constant ("control" since M6 — the
// deterministic CONTROL floor; was "heuristic" through M1–M5 to hold the goldens byte-identical).
func ScoreAdmit(c types.Candidate, hist []types.Thought, value float64) types.FilterVerdict {
	text := strings.TrimSpace(c.Text)
	if len([]rune(text)) < 2 { // empty or single-char -> malformed (Python: not text or len < 2)
		return types.FilterVerdict{
			Verdict: types.REJECT, Confidence: 0.0, Reason: "empty/malformed",
			Signals: map[string]any{"malformed": -1.0}, Source: Appraiser,
		}
	}

	// Source priors: reality and the user are trusted; injections must earn it (a high-relevance
	// specialist is trusted nearly as much as its competence warrants).
	prior := SourcePrior(c)

	// Prior-dominant early on: the value signal is uninformed until reality grounds it (§12.2).
	// The signals are accumulated in a key-ORDERED list (not a Go map) so the score sums in
	// Python's dict-INSERTION order. Float addition is not associative; Python's sum(dict.values())
	// folds in insertion order, and a random Go map-iteration order would yield a 1-ULP-different
	// score nondeterministically (e.g. 0.464 vs 0.4640000000000001 for the 3-signal hedged case),
	// breaking both determinism and golden parity. The insertion order below mirrors value.py exactly.
	signals := newOrderedSignals()
	signals.set("source_prior", 0.8*prior)
	signals.set("value_prior", 0.2*value)
	low := strings.ToLower(text)
	if ContainsAny(low, Hedges) {
		signals.set("hedged", -0.18) // hedged -> less trustworthy
	}
	if ContainsAny(low, OverConfidentMarkers) {
		signals.set("over_confident", -0.08) // over-confident phrasing is itself a mild red flag (S8)
	}
	// Contradiction with a recent confident thought -> distrust (laundered-memory guard).
	if Contradicts(c, hist) {
		signals.set("contradicts_belief", -0.30)
	}
	// Reality has spoken: an injection re-asserting success after a FAILED observation is a
	// refuted guess — ground truth must override recombined optimism, or the loop never closes.
	if RefutedByReality(c, hist) {
		signals.set("refuted_by_reality", -0.45)
	}
	// Grounding integrity (Item 4): a thought that ASSERTS an observed/read RESULT with no real
	// observation behind it ("I read the file, it's 10") is a manufactured reality — killed, not flagged.
	// This is a STRUCTURAL fact (the verdict is FORCED to REJECT below); the signal is recorded so the
	// reason names it, but the OVERRIDE — not the sum — is what kills it, so every other candidate's score
	// is byte-identical (the signal only ever appears on this narrow lure, never in the goldens).
	assertsUngrounded := AssertsUngroundedObservation(c, hist)
	if assertsUngrounded {
		signals.set("asserts_ungrounded_observation", ungroundedAssertionPenalty)
	}
	// Tool-affordance hallucination (#43): a thought that DENIES it can reach reality ("no filesystem
	// access" / "type /mcp") AFTER the loop already reached reality is a refusal that contradicts a
	// demonstrated capability — killed (a STRUCTURAL REJECT), not flagged. Like asserts_ungrounded the
	// signal only ever appears on this narrow refusal (never in the goldens), so every other candidate's
	// score is byte-identical; the verdict OVERRIDE below — not the sum — is what kills it.
	deniesAccess := DeniesAvailableReality(c, hist)
	if deniesAccess {
		signals.set("denies_available_reality", deniedAccessPenalty)
	}

	score := Clamp01(signals.sum())

	// round for display only (the math used the raw sum above) — matches Python {k: round(v,3)}.
	rounded := make(map[string]any, len(signals.keys))
	for _, k := range signals.keys {
		rounded[k] = round3(signals.vals[k])
	}
	reason := admitReason(rounded)

	var verdict types.Verdict
	switch {
	case score >= 0.6:
		verdict = types.ADMIT
	case score >= 0.32:
		verdict = types.FLAG
	default:
		verdict = types.REJECT
	}
	// Grounding integrity (Item 4): an asserted-observation-without-grounding is a STRUCTURAL REJECT —
	// the conscious may NOT voice a reality result it never observed, however high the source prior scored.
	// Force REJECT (mirrors how refuted_by_reality is treated as authoritative); the verdict override, not
	// the score, kills it, so no other candidate's verdict moves.
	if assertsUngrounded {
		verdict = types.REJECT
	}
	// Tool-affordance hallucination is a STRUCTURAL REJECT too (#43): the conscious may not voice a refusal
	// to use a capability the loop just demonstrated, however high the source prior scored.
	if deniesAccess {
		verdict = types.REJECT
	}
	return types.FilterVerdict{Verdict: verdict, Confidence: score, Reason: reason, Signals: rounded, Source: Appraiser}
}

// SourcePrior maps a candidate's source to its trust prior. INJECTED scales with relevance
// (a high-relevance specialist is trusted nearly as much as its competence warrants).
func SourcePrior(c types.Candidate) float64 {
	switch c.Source {
	case types.OBSERVATION:
		return 0.92
	case types.USER_INPUT:
		return 0.88
	case types.PERCEPT:
		return 0.82
	case types.INJECTED:
		// 0.5 + 0.45*relevance, with the multiply ROUNDED to float64 before the add so the result
		// matches CPython's runtime double-rounding (0.45*0.85 -> 0.3825 -> +0.5 -> 0.8825000000000001).
		// Go would otherwise constant-fold 0.5 + 0.45*<const relevance> to a single correctly-rounded
		// value (0.8825), diverging from the Python wire by 1 ULP and cascading into the Filter's
		// emitted confidence. roundf64 is a compiler-opaque float64 round that defeats the fold/FMA.
		return 0.5 + roundf64(0.45*c.Relevance)
	case types.GENERATED:
		return 0.42
	case types.METACOG:
		return 0.72
	default:
		return 0.5
	}
}

// Hedged reports whether the text carries a low-trust hedge marker (maybe/perhaps/…). The text is
// lowered internally so callers may pass any casing.
func Hedged(text string) bool { return ContainsAny(strings.ToLower(text), Hedges) }

// OverConfident reports whether the text carries over-confident phrasing (definitely/certainly/…).
// The text is lowered internally so callers may pass any casing.
func OverConfident(text string) bool { return ContainsAny(strings.ToLower(text), OverConfidentMarkers) }

// admitReason builds a human reason FROM the (rounded) signals — naming the factors that moved
// the score. Mirrors Python _admit_reason: sort the negatives ascending by value, take the two
// most-negative, format "key (+/-X.XX)"; with no negatives, report the source prior.
func admitReason(signals map[string]any) string {
	type kv struct {
		k string
		v float64
	}
	var neg []kv
	for k, raw := range signals {
		v := raw.(float64)
		if v < 0 {
			neg = append(neg, kv{k, v})
		}
	}
	if len(neg) > 0 {
		// sorted((v, k) ...) — primary key is the value (ascending), tie-broken by the key name.
		sort.Slice(neg, func(i, j int) bool {
			if neg[i].v != neg[j].v {
				return neg[i].v < neg[j].v
			}
			return neg[i].k < neg[j].k
		})
		if len(neg) > 2 {
			neg = neg[:2]
		}
		parts := make([]string, len(neg))
		for i, n := range neg {
			parts[i] = fmt.Sprintf("%s (%+.2f)", n.k, n.v)
		}
		return strings.Join(parts, "; ") + " lowered trust"
	}
	sp := 0.0
	if raw, ok := signals["source_prior"]; ok {
		sp = raw.(float64)
	}
	return fmt.Sprintf("source-trusted (prior %.2f)", sp)
}

// ---------------------------------------------------------------------------
// Gate ranking (Pattern A — no model, ever)
// ---------------------------------------------------------------------------

// Rank is the Gate ordering (Pattern A): score each candidate by relevance + length-of-substance,
// penalising hedging; returns (scores, reasons) — the per-candidate WHY captured alongside the
// score (P6) rather than collapsed to a bare float. Plausible, not calibrated (spec §12.2). NO
// model is ever consulted for Rank.
func Rank(cands []types.Candidate, hist []types.Thought) (scores []float64, reasons []string) {
	scores = make([]float64, 0, len(cands))
	reasons = make([]string, 0, len(cands))
	for _, c := range cands {
		s := 0.6*c.Relevance + 0.2*math.Min(1.0, float64(len([]rune(c.Text)))/60.0) + 0.2
		hedged := ContainsAny(strings.ToLower(c.Text), Hedges)
		if hedged {
			s -= 0.15
		}
		scores = append(scores, Clamp01(s))
		reason := fmt.Sprintf("relevance %.2f", c.Relevance)
		if hedged {
			reason += ", hedged"
		}
		reasons = append(reasons, reason)
	}
	return scores, reasons
}

// ---------------------------------------------------------------------------
// Pattern-C escalation gating (the deterministic flagged-fuzzy flag)
// ---------------------------------------------------------------------------

// AdmitAmbiguityThreshold is the flagged-fuzzy cutoff for the Filter's Pattern-C escalation:
// the model is consulted as a CEILING only when AdmitAmbiguity >= this. It mirrors the
// Controller's `ambiguity >= 0.5` gate exactly (controller.go:372) so the two hybrid escalators
// share one posture.
const AdmitAmbiguityThreshold = 0.5

// admitBandWindow is the confidence half-window around a verdict band edge inside which the score
// is "near the edge" — the SAME 0.12 window the Controller's ambiguity uses (controller.go:424,429).
const admitBandWindow = 0.12

// AdmitBandEdges are the two verdict band edges (FLAG/REJECT at 0.32, ADMIT/FLAG at 0.6) the
// floor's score is compared against to detect a borderline admission. They are the exact bands
// ScoreAdmit switches on; kept as a named slice so AdmitAmbiguity and the bands cannot drift apart.
var AdmitBandEdges = []float64{0.32, 0.6}

// AdmitAmbiguity scores how FUZZY a floor verdict is — the deterministic flag the Filter's
// Pattern-C escalation gates on (the analogue of the Controller's `ambiguity`, controller.go:420).
// It is a PURE function of the floor verdict + candidate, mirroring §3.2 of the refactor spec:
//
//   - a HARD floor signal (refuted_by_reality / contradicts_belief) ZEROES it — that is a
//     STRUCTURAL fact (reality/belief said no) the model may not override, so it must NOT be
//     flagged-fuzzy (the structural-protection rule, §3.1). This check comes first.
//   - confidence sitting near a verdict band edge (0.32 or 0.6) raises it:
//     1.0 - min(1.0, |conf - edge| / 0.12) — the SAME shape/window the Controller uses;
//   - an INJECTED / GENERATED source (the sources the membrane exists to police) raises it.
//
// The result is clamped to [0,1]. NO model is consulted (this is the deterministic gate that
// DECIDES whether to consult one).
func AdmitAmbiguity(floor types.FilterVerdict, c types.Candidate) float64 {
	// Structural facts are not fuzzy — a refuted-by-reality / contradicts-belief / asserts-ungrounded-
	// observation verdict is the floor speaking with authority; the model may not lift it, so it is never
	// escalation-eligible.
	if _, refuted := floor.Signals["refuted_by_reality"]; refuted {
		return 0.0
	}
	if _, contra := floor.Signals["contradicts_belief"]; contra {
		return 0.0
	}
	if _, ungrounded := floor.Signals["asserts_ungrounded_observation"]; ungrounded {
		return 0.0
	}
	if _, denied := floor.Signals["denies_available_reality"]; denied {
		return 0.0
	}
	score := 0.0
	// Near a verdict band edge — the borderline "is this admissible?" the model's language
	// judgment can refine (the floor's lexical signals are coarse near the threshold).
	for _, edge := range AdmitBandEdges {
		score = math.Max(score, 1.0-math.Min(1.0, math.Abs(floor.Confidence-edge)/admitBandWindow))
	}
	// The membrane exists to police recombined/generated claims; a borderline one from those
	// sources is exactly where a laundered hallucination hides, so raise the fuzziness floor.
	if c.Source == types.INJECTED || c.Source == types.GENERATED {
		score = math.Max(score, 0.5)
	}
	return math.Min(1.0, score)
}

// ---------------------------------------------------------------------------
// CRAG-style sufficiency floor (A-RAG1 — the deterministic coverage FLOOR)
// ---------------------------------------------------------------------------

// SufficiencyVerdict is the CRAG-style grading of retrieved FUEL against the need it was sourced for:
// is the recalled material enough to commit on, or should the harness ABSTAIN (decline / set aside /
// re-source) rather than over-commit a hollow guess? This is the structural answer to Google's
// "abstention paradox" (RAG context suppresses abstention) and the lesson from the harness's failed
// THOUGHT_GROUND_COMPLETE prompt-fix (a "be careful" prompt cannot deliver it). NO model is consulted
// in this enum's production — it is the deterministic floor; the model is a Pattern-C CEILING above it.
type SufficiencyVerdict int

const (
	// SuffSufficient — the fuel covers the need and is grounded enough to commit on.
	SuffSufficient SufficiencyVerdict = iota
	// SuffAmbiguous — borderline coverage/trust; the floor cannot decide alone (the flagged-fuzzy case
	// the model CEILING may refine when escalation is wired).
	SuffAmbiguous
	// SuffInsufficient — the fuel does NOT cover the need (or is the ungrounded GENERATED floor with no
	// real coverage); the harness ABSTAINS rather than over-commit.
	SuffInsufficient
)

// String renders the sufficiency verdict (the wire value on seam.sufficiency).
func (v SufficiencyVerdict) String() string {
	switch v {
	case SuffSufficient:
		return "sufficient"
	case SuffAmbiguous:
		return "ambiguous"
	default:
		return "insufficient"
	}
}

// Sufficiency is the floor's structured grading of retrieved fuel: the verdict, the coverage scalar
// it was decided on (lexical content-word overlap of the fuel against the need), and the trust prior
// of the rung the fuel came from. Coverage is the CRAG "relevance" estimate; trust folds the
// provenance ladder's source confidence (a grounded reality hit is trusted, the GENERATED floor is
// not). Source is the floor Appraiser ("control") so the appraiser is explicit on the wire.
type Sufficiency struct {
	Verdict  SufficiencyVerdict
	Coverage float64 // content-word overlap of fuel.text against the need.query, in [0,1]
	Trust    float64 // the rung's trust prior (provenance confidence), in [0,1]
	Grounded bool    // whether the fuel came from a grounded rung (present/knowledge/memory/reality)
	Reason   string
	Source   string // the appraiser — Appraiser ("control"); a successful ceiling escalation overrides it
}

// Sufficiency verdict bands over the combined coverage-and-trust score (kept as named constants so the
// floor and its ambiguity gate cannot drift apart). The combined score is coverage (the CRAG "does the
// fuel speak to the need?" signal — content-word Jaccard, which for genuinely-relevant short texts sits
// around 0.4-0.7) MODULATED by trust: combined = coverage * (0.5 + 0.5*trust). A high-trust grounded rung
// barely discounts coverage; a low-trust rung discounts it materially. The bands are calibrated to
// Jaccard reality (not the raw product, which would crush two sub-1 factors and over-abstain).
const (
	suffSufficientBand   = 0.45 // combined score >= this ⇒ sufficient (commit)
	suffInsufficientBand = 0.15 // combined score <  this ⇒ insufficient (abstain); between ⇒ ambiguous
)

// suffTrustWeight blends trust into the combined score: combined = coverage * (suffTrustFloor +
// suffTrustWeight*trust). At trust=1 the coverage passes through nearly whole; at trust=0 it is halved.
const (
	suffTrustFloor  = 0.5
	suffTrustWeight = 0.5
)

// ScoreSufficiency is the CRAG-style sufficiency FLOOR (Pattern-A core, NO model). It grades the
// sourced fuel against the need: coverage = content-word overlap of the fuel text against the need
// query; the combined score = coverage * trust (provenance confidence). The verdict bands:
//
//   - combined >= suffSufficientBand ⇒ SUFFICIENT (the fuel covers the need and is trusted);
//   - combined <  suffInsufficientBand ⇒ INSUFFICIENT (the harness ABSTAINS — a hollow recall is
//     worse than no recall: over-committing it is exactly the abstention paradox);
//   - otherwise ⇒ AMBIGUOUS (the flagged-fuzzy case the model ceiling refines).
//
// A grounded==false fuel (the GENERATED rung) is capped at AMBIGUOUS regardless of coverage — an
// invented "fact" must not read as sufficient grounding (it stays the Filter's 0.42-trust floor). An
// empty fuel text is INSUFFICIENT outright. The returned Source is the floor Appraiser ("control").
func ScoreSufficiency(query, fuelText string, trust float64, grounded bool) Sufficiency {
	ft := strings.TrimSpace(fuelText)
	if ft == "" {
		return Sufficiency{
			Verdict: SuffInsufficient, Coverage: 0, Trust: round3(Clamp01(trust)), Grounded: grounded,
			Reason: suffReason(SuffInsufficient, 0, 0, grounded), Source: Appraiser,
		}
	}
	coverage := contentOverlap(query, ft)
	combined := coverage * (suffTrustFloor + suffTrustWeight*Clamp01(trust))
	var v SufficiencyVerdict
	switch {
	case combined >= suffSufficientBand:
		v = SuffSufficient
	case combined < suffInsufficientBand:
		v = SuffInsufficient
	default:
		v = SuffAmbiguous
	}
	// An ungrounded (GENERATED) fuel can never read as SUFFICIENT on the floor — an invented fact is
	// not grounding, so it is capped at AMBIGUOUS (the model ceiling may keep/lower it, never lift past
	// the floor's structural cap). This is the laundered-hallucination guard applied to retrieval: the
	// Filter already prices GENERATED at 0.42 trust; here the SAME provenance distrust caps sufficiency.
	if !grounded && v == SuffSufficient {
		v = SuffAmbiguous
	}
	return Sufficiency{
		Verdict:  v,
		Coverage: round3(coverage),
		Trust:    round3(Clamp01(trust)),
		Grounded: grounded,
		Reason:   suffReason(v, coverage, combined, grounded),
		Source:   Appraiser,
	}
}

// SufficiencyAmbiguity scores how FUZZY a sufficiency floor verdict is — the deterministic flag the
// gate's Pattern-C escalation gates on (the analogue of AdmitAmbiguity). The model CEILING is consulted
// ONLY when this clears SufficiencyAmbiguityThreshold:
//
//   - a floor verdict of SuffAmbiguous is itself the flagged-fuzzy case (ambiguity 1.0 — the floor
//     explicitly could not decide);
//   - a SUFFICIENT/INSUFFICIENT score sitting near a band edge raises it (the same band-edge shape the
//     admit ambiguity uses), so a borderline-by-a-hair commit/abstain can be refined by the model;
//   - a CLEAR sufficient (high coverage AND grounded) or a CLEAR insufficient (near-zero coverage) is
//     NOT fuzzy — the floor speaks with authority and the model is not consulted (the structural cases
//     never escalate, mirroring the Filter's structural-fact protection).
//
// NO model is consulted (this is the deterministic gate that DECIDES whether to consult one).
func SufficiencyAmbiguity(s Sufficiency) float64 {
	if s.Verdict == SuffAmbiguous {
		return 1.0 // the floor explicitly could not decide -> escalate to the ceiling
	}
	combined := s.Coverage * (suffTrustFloor + suffTrustWeight*s.Trust)
	score := 0.0
	for _, edge := range []float64{suffInsufficientBand, suffSufficientBand} {
		score = math.Max(score, 1.0-math.Min(1.0, math.Abs(combined-edge)/suffBandWindow))
	}
	return math.Min(1.0, score)
}

// SufficiencyAmbiguityThreshold is the flagged-fuzzy cutoff for the sufficiency gate's Pattern-C
// escalation — the model is consulted as a CEILING only when SufficiencyAmbiguity >= this. It mirrors
// the Filter's AdmitAmbiguityThreshold (0.5) so the two hybrid escalators share one posture.
const SufficiencyAmbiguityThreshold = 0.5

// suffBandWindow is the half-window around a sufficiency band edge inside which the combined score is
// "near the edge" (the same 0.12 window the admit ambiguity uses).
const suffBandWindow = 0.12

// suffReason renders a short human-readable WHY for the sufficiency verdict (carried on the wire).
func suffReason(v SufficiencyVerdict, coverage, combined float64, grounded bool) string {
	g := "grounded"
	if !grounded {
		g = "ungrounded"
	}
	return fmt.Sprintf("%s: coverage=%.2f combined=%.2f (%s)", v.String(), coverage, combined, g)
}

// contentOverlap is the content-word Jaccard overlap of two raw strings — the SAME shape the lexical
// retriever uses (retrieval.LexicalScore), reimplemented here so control stays a Tier-1 leaf (it never
// imports internal/retrieval). Lowercases, splits on non-alphanumerics, drops stopwords + 1-2 char
// tokens, then |A∩B| / |A∪B|. Deterministic; no model.
func contentOverlap(a, b string) float64 {
	sa := contentWordSet(a)
	sb := contentWordSet(b)
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for w := range sa {
		if sb[w] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// suffStopwords are dropped before the content-word overlap so coverage reflects CONTENT, not glue.
// Mirrors retrieval.stopwords (kept local to preserve control's leaf isolation).
var suffStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "to": true, "and": true, "or": true, "is": true,
	"it": true, "in": true, "on": true, "for": true, "with": true, "that": true, "this": true,
	"as": true, "at": true, "by": true, "be": true, "are": true, "was": true, "your": true, "you": true,
	"how": true, "why": true, "what": true, "out": true, "so": true, "if": true, "into": true,
	"more": true, "most": true, "than": true, "do": true, "does": true, "not": true, "we": true,
	"our": true, "i": true, "my": true, "they": true, "their": true, "from": true, "up": true,
	"down": true, "off": true, "over": true, "then": true, "when": true, "which": true, "its": true,
}

// contentWordSet lowercases s, splits on non-alphanumerics, and drops stopwords + 1-2 char tokens —
// the content-word set the sufficiency overlap compares over (mirrors retrieval.contentWords).
func contentWordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(w) <= 2 || suffStopwords[w] {
			continue
		}
		set[w] = true
	}
	return set
}

// ---------------------------------------------------------------------------
// the floor's structural checks (exported so a test can assert each independently)
// ---------------------------------------------------------------------------

// Contradicts reports whether the candidate's stance conflicts with a recent confident thought
// (the laundered-memory guard). Python scans reversed(history[-6:]), reads getattr(raw, "stance",
// None) off the thought's raw_return; here the candidate-like payload is a *types.Candidate
// carrying a Stance.
func Contradicts(c types.Candidate, hist []types.Thought) bool {
	if c.Stance == nil {
		return false
	}
	window := LastN(hist, 6)
	for i := len(window) - 1; i >= 0; i-- {
		t := window[i]
		stance := stanceOf(t.RawReturn)
		if stance != "" && stance != *c.Stance && t.Confidence >= 0.6 {
			return true
		}
	}
	return false
}

// RefutedByReality reports whether a recent OBSERVATION failed (reality said no) and this candidate
// re-asserts success — a refuted guess that ground truth must override.
func RefutedByReality(c types.Candidate, hist []types.Thought) bool {
	recentFailure := false
	for _, t := range LastN(hist, 5) {
		if t.Source != types.OBSERVATION {
			continue
		}
		if o, ok := t.RawReturn.(types.Observation); ok && !o.Ok {
			recentFailure = true
			break
		}
	}
	if !recentFailure {
		return false
	}
	low := strings.ToLower(c.Text)
	if c.Stance != nil && (*c.Stance == "runs" || *c.Stance == "safe") {
		return true
	}
	for _, w := range []string{"runs cleanly", "comes out fine", "it runs", "looks safe", "works"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// AssertsUngroundedObservation reports whether the candidate ASSERTS a concrete observed/read RESULT
// (a measured value, "I read X and it is N", a file's content) with NO non-fabricated action.observation
// behind it — the "I read the file, it's 10" hallucination (truth 6, lure 8). It is the grounding-
// integrity guard at the ADMISSION floor: a thought that claims it observed reality, when the loop has no
// real observation to back the claim, is killed (a STRUCTURAL REJECT) rather than merely flagged — the
// conscious must never be allowed to voice an observed result it never observed.
//
// It is gated TIGHTLY so it fires ONLY on that narrow lure and never on ordinary effortful reasoning,
// plans, or hedged thoughts (all of which must still pass):
//
//   - SOURCE: only INJECTED / GENERATED candidates are policed (the sources the membrane exists to
//     guard). A real OBSERVATION candidate already came from reality; USER_INPUT / PERCEPT / METACOG are
//     not the membrane's concern — they are excluded so the guard cannot regress them.
//   - NOT HEDGED: a hedged thought ("maybe it's 10", "I think the file says X") is not an ASSERTION of
//     fact, so it is never killed by this guard (it still passes — possibly FLAGged by the hedge term).
//   - BOTH CUES: the text must carry a first-person READ/OBSERVE verb tied to an artifact (observationCues)
//     AND present a CONCRETE result (resultCues). A plan ("I will read the file") has no result cue; a
//     pure result mention with no read verb ("the answer is 56" from a compute) has no observation cue —
//     neither trips the guard. Both must hold.
//   - NO REAL GROUNDING: there must be NO recent NON-FABRICATED OBSERVATION in history (a real tool read)
//     that could legitimately back the claim. If a real observation IS present, the assertion is grounded
//     and passes normally. A FABRICATED observation does not count (it never came from reality).
//
// When all hold the claim is an ungrounded assertion of reality → a structural REJECT (see ScoreAdmit +
// AdmitAmbiguity, which zero its fuzziness so the model may not lift it). NO model is consulted here.
func AssertsUngroundedObservation(c types.Candidate, hist []types.Thought) bool {
	// Only the policed (membrane-guarded) sources can launder an observation; a real OBSERVATION
	// candidate already came from a tool, and trusted/structural sources are not this guard's concern.
	if c.Source != types.INJECTED && c.Source != types.GENERATED {
		return false
	}
	low := strings.ToLower(strings.TrimSpace(c.Text))
	// A hedged thought is not an assertion of observed fact — it must still pass (never killed here).
	if ContainsAny(low, Hedges) {
		return false
	}
	// Require BOTH halves: it claims to have READ/OBSERVED reality AND presents a concrete RESULT.
	if !ContainsAny(low, observationCues) || !ContainsAny(low, resultCues) {
		return false
	}
	// And require an EXTERNAL ARTIFACT to be named — this is what keeps the guard OFF internal reasoning.
	// "I checked the logic and it is sound" / "I ran through the steps and it's fine" carry a read verb +
	// a result cue but NO external artifact, so they pass (never killed). Only a claim of having observed
	// an external file/output/log ("I read config/limits.go and it's 10") can be the ungrounded lure.
	if !referencesExternalArtifact(low) {
		return false
	}
	// If a REAL (non-fabricated) observation is in recent history, the read could be grounded — let it
	// pass (the assertion may legitimately echo a tool result). Only an assertion with NO real
	// observation behind it anywhere recent is the ungrounded hallucination this guard kills.
	if hasRealObservation(hist) {
		return false
	}
	return true
}

// DeniesAvailableReality reports whether the candidate DENIES it can reach reality ("I don't have
// filesystem access", "I wasn't able to directly read the file", "type /mcp") WHILE the loop has in fact
// ALREADY reached reality — a real, non-fabricated observation is in recent history. That is the #43
// tool-affordance hallucination: the conscious refuses to use a capability the loop just demonstrated. It
// is a STRUCTURAL contradiction (a denial of demonstrated reality), so ScoreAdmit forces it to REJECT and
// AdmitAmbiguity zeroes its fuzziness (the model may not lift it). NO model is consulted here.
//
// It is gated TIGHTLY so it only fires on that refusal and never on an honest "I cannot determine X" or on
// the OFFLINE path (where no tools are wired, so a "no access" claim may be truthful):
//
//   - SOURCE: only INJECTED / GENERATED candidates are policed (the membrane's sources). A real
//     OBSERVATION / USER_INPUT / METACOG is not a laundered refusal.
//   - DENIAL SHAPE: the text must match reDeniesAccess (a denial verb next to an access/tool/file noun, or
//     the literal "/mcp" instruction). An honest unknown ("I can't determine the value") names no
//     access/tool/file noun, so it never trips.
//   - REALITY WAS REACHED: there MUST be a recent NON-FABRICATED observation (a genuine tool read). With
//     no real observation in history (the offline path / a first-turn refusal before any tool ran) the
//     guard does NOT fire — the denial may be honest, and this guard never punishes an honest "no tools".
func DeniesAvailableReality(c types.Candidate, hist []types.Thought) bool {
	if c.Source != types.INJECTED && c.Source != types.GENERATED {
		return false
	}
	if !reDeniesAccess.MatchString(c.Text) {
		return false
	}
	// Only a denial that CONTRADICTS demonstrated reality is the hallucination this guard kills. With no
	// real observation behind the loop yet, a "no access" claim is not refutable here — let it pass.
	return hasRealObservation(hist)
}

// hasRealObservation reports whether recent history carries a NON-FABRICATED observation — a genuine
// tool/executor read the conscious could legitimately be echoing. A fabricated observation (the offline
// stand-in) does NOT count: it never came from reality, so it can never ground an asserted read.
func hasRealObservation(hist []types.Thought) bool {
	for _, t := range LastN(hist, 6) {
		if t.Source != types.OBSERVATION {
			continue
		}
		if o, ok := t.RawReturn.(types.Observation); ok && o.GroundsReality() {
			return true
		}
	}
	return false
}

// stanceOf reads a "stance" off a duck-typed raw_return: the candidate-like member of the union
// (*types.Candidate) carries it. Mirrors Python getattr(raw, "stance", None) — anything without
// a stance yields "".
func stanceOf(raw any) string {
	if cand, ok := raw.(*types.Candidate); ok && cand.Stance != nil {
		return *cand.Stance
	}
	return ""
}

// ---------------------------------------------------------------------------
// shared floor primitives (exported — the test double's CONTENT roles reuse these too)
// ---------------------------------------------------------------------------

// RealThoughts drops METACOG bookkeeping nodes (Python [t for t in ctx if t.source is not
// Source.METACOG]).
func RealThoughts(ctx []types.Thought) []types.Thought {
	out := make([]types.Thought, 0, len(ctx))
	for _, t := range ctx {
		if t.Source != types.METACOG {
			out = append(out, t)
		}
	}
	return out
}

// LastN returns the last n elements (Python xs[-n:]) without copying when possible.
func LastN(xs []types.Thought, n int) []types.Thought {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}

// ContainsAny reports whether low contains any of the needles (already-lowercased low).
func ContainsAny(low string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

// Clamp01 clamps to [0, 1] (Python max(0.0, min(1.0, x))).
func Clamp01(x float64) float64 { return math.Max(0.0, math.Min(1.0, x)) }

// round3 rounds to 3 decimals — the per-emit-site rounding obligation (DESIGN §3). Python
// round() uses banker's rounding; math.Round is half-away-from-zero, which differs only on
// exact .0005 ties that the floor's arithmetic does not produce, so the wire stays byte-identical
// for these fixtures.
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// roundf64 is a compiler-opaque identity that forces its argument to be rounded to a genuine
// float64 value (defeating Go's arbitrary-precision constant folding and any FMA contraction).
// It exists so an intermediate multiply rounds independently — matching CPython, which rounds
// every operation to a double. The bits round-trip is provably value-preserving yet the optimiser
// cannot fold across it, so `0.5 + roundf64(0.45*rel)` evaluates with the same per-op rounding as
// Python regardless of whether `rel` is a compile-time constant at the call site.
func roundf64(x float64) float64 { return math.Float64frombits(math.Float64bits(x)) }

// ---------------------------------------------------------------------------
// insertion-ordered signal accumulation + Neumaier compensated summation
// ---------------------------------------------------------------------------

// orderedSignals is an insertion-ordered string->float64 collection — the Go stand-in for a
// Python dict whose `sum(values())` folds in insertion order. A plain Go map would iterate
// randomly, making the Filter score (a non-associative float sum) nondeterministic and divergent
// from the Python wire by up to 1 ULP. keys records insertion order; vals holds the values.
type orderedSignals struct {
	keys []string
	vals map[string]float64
}

// newOrderedSignals builds an empty insertion-ordered signal set.
func newOrderedSignals() *orderedSignals {
	return &orderedSignals{vals: map[string]float64{}}
}

// set records key=v, appending key to the insertion order on first set (a re-set keeps the
// original position, matching Python dict assignment semantics).
func (o *orderedSignals) set(key string, v float64) {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// sum folds the values in insertion order — byte-identical to Python's sum(signals.values()).
// CPython 3.12+ sum() over floats is NOT a naive left-fold: it uses Neumaier compensated
// summation (an error-correcting Kahan variant). A naive Go fold diverges by 1 ULP on the
// 3-signal hedged case (0.464 vs Python's 0.4640000000000001), so the score is folded with the
// SAME compensated algorithm in the SAME insertion order to keep the emitted confidence identical.
func (o *orderedSignals) sum() float64 {
	vals := make([]float64, len(o.keys))
	for i, k := range o.keys {
		vals[i] = o.vals[k]
	}
	return neumaierSum(vals)
}

// neumaierSum reproduces CPython's built-in sum() over floats — Neumaier's improved Kahan
// compensated summation (the algorithm CPython's bltinmodule sum_float uses). The running
// compensation c captures the low-order bits lost at each add; the final s+c is the corrected
// total. This is what makes Python's sum([a,b,c]) differ from ((a+b)+c) and must be matched for
// byte-identical emitted scores.
func neumaierSum(vals []float64) float64 {
	s := 0.0
	c := 0.0
	for _, x := range vals {
		t := s + x
		if math.Abs(s) >= math.Abs(x) {
			c += (s - t) + x
		} else {
			c += (x - t) + s
		}
		s = t
	}
	return s + c
}
