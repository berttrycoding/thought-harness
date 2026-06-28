// Package verify is the INDEPENDENT answer-verifier (T2.1, the flagship Tier-2 capability —
// docs/internal/notes/2026-06-23-cognitive-engine-capability-audit.md P1; docs/internal/2026-06-23-capability-
// enhancement-roadmap.md T2.1).
//
// THE PRINCIPLE (the whole reason this exists). We MEASURED a same-model ceiling: a model re-judging its
// OWN reasoning chain cannot catch its own systematic errors, and self-consistency cannot fix a bias
// (Huang 2024 arXiv:2310.01798 "LLMs Cannot Self-Correct Reasoning Yet"; the project's own measurement —
// a 0-of-K systematic mis-read stays 0). The ONLY thing that breaks this is an INDEPENDENT signal — a
// tool / the world / a programmatic check — NOT the same model looking again. So a Verifier here MUST
// check the committed answer against an INDEPENDENT signal, never against the model's own re-read of its
// chain. The first (and so far only) implementation is web-grounded: it RE-RETRIEVES web evidence for the
// answer claim and checks whether that fresh, externally-sourced evidence supports the committed answer.
//
// PLUGGABLE. Verifier is an interface so future independent signals slot in behind it without touching
// the engine wiring: a run_tests verifier (execute the claimed code, observe the result), a compute
// verifier (re-derive a computation deterministically), a different-model verifier. Only the web-grounded
// one is BUILT in this slice; the others are designed-for, not stubbed (no fake opinions).
//
// PATTERN C. A deterministic FLOOR + an OPTIONAL model CEILING:
//   - FLOOR (Pattern A, no model): is the answer web-checkable at all (web wired + a short, lookup-shaped
//     answer claim)? Does the RE-RETRIEVED evidence literally CONTAIN / lexically align with the answer?
//     The independence is structural here — the comparison is answer-vs-fresh-evidence, never a re-read.
//   - CEILING (Pattern C, optional model): on a flagged-fuzzy case (evidence present + topical but the
//     lexical overlap is inconclusive) the model judges "does THIS independently-retrieved evidence
//     SUPPORT this answer?". It judges against the EVIDENCE, never the original chain — so it is NOT the
//     same-model self-correction the ceiling exists to break. The floor stands when the model declines.
//
// HEADLESS-PURE / DETERMINISTIC. No net/http, no clock, no unseeded randomness here — the only outward
// read is through the INJECTED web.Web seam (web.Wall at the edge, web.Fake in tests), exactly as the
// rest of the engine reaches the world. Same input + same injected seam ⇒ same verdict, so the seeded-RNG
// determinism contract and the goldens hold (and it only runs at all behind the default-OFF
// critic.answer_verify flag, wired by the engine).
package verify

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// Verdict is the verifier's three-valued outcome over an INDEPENDENT signal.
type Verdict int

const (
	// Unverifiable: the answer could not be checked against an independent signal at all (no web seam, or
	// the answer claim is not lookup-shaped, or the re-retrieval returned nothing). The gate is a NO-OP —
	// the committed answer falls through to today's behaviour, byte-identical. This is the DEFAULT for any
	// answer the harness cannot independently check, so the verifier never blocks an unverifiable answer.
	Unverifiable Verdict = iota
	// Supported: the re-retrieved independent evidence contains / aligns with the committed answer. Commit.
	Supported
	// Unsupported: the independent evidence is present and topical but does NOT support the committed
	// answer (it is absent, or it contradicts). Do NOT commit as-is — signal the executive to continue.
	Unsupported
)

// String renders the verdict as the wire/string form the ceiling and the events use.
func (v Verdict) String() string {
	switch v {
	case Supported:
		return "supported"
	case Unsupported:
		return "unsupported"
	default:
		return "unverifiable"
	}
}

// ParseVerdict maps a ceiling's verdict string back to a Verdict (the inverse of String); ok=false on an
// unrecognised string so an off-shape model reply is treated as a decline (the floor stands).
func ParseVerdict(s string) (Verdict, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "supported":
		return Supported, true
	case "unsupported":
		return Unsupported, true
	case "unverifiable":
		return Unverifiable, true
	}
	return Unverifiable, false
}

// Request is one answer-verification ask: the QUESTION the harness was answering and the candidate ANSWER
// it is about to commit. The verifier derives its own independent re-retrieval query from these.
type Request struct {
	Question string // the episode goal / question being answered
	Answer   string // the candidate answer claim about to be committed (the active line's tip)
}

// Result is the verifier's verdict plus the audit trail that makes it observable: the independent query
// it issued, the evidence it retrieved (capped), whether the model ceiling refined the floor, and a human
// reason. Source names where the independent signal came from (the web seam's provenance, or "" when none).
type Result struct {
	Verdict      Verdict
	Query        string  // the INDEPENDENT re-retrieval query (derived from the question, not the model's chain)
	Evidence     string  // the re-retrieved evidence snippet (the independent signal), capped
	Source       string  // provenance of the independent signal (the web seam's Source), "" when none
	Reason       string  // human-readable reason for the verdict
	Escalated    bool    // whether the optional model ceiling was consulted (and answered) to set the verdict
	FloorVerdict Verdict // the deterministic floor's own verdict (before any ceiling refinement)
}

// Verifier checks a committed answer against an INDEPENDENT signal. Implementations MUST source their
// signal from outside the generator's own re-read (the world / a tool / a different model) — never from
// the same model re-judging its original chain. The web-grounded implementation re-retrieves web evidence;
// future run_tests / compute / different-model implementations slot in behind this same interface.
type Verifier interface {
	Verify(req Request) Result
}

// answerClaimMaxLen bounds a candidate answer claim we will try to verify. A long, multi-sentence answer
// is not a single web-checkable fact (it is a synthesis), so it is left Unverifiable (the gate no-ops) —
// the verifier targets short factual/lookup answers, the lane where an independent re-retrieval is a real
// check. Code points, not bytes (Unicode-safe).
const answerClaimMaxLen = 160

// evidenceCap bounds how much re-retrieved evidence we carry on the Result (the audit trail) so an
// oversized snippet can never bloat an event. Code points.
const evidenceCap = 240

// minAnswerTokens is the smallest token count a candidate answer must have to be worth checking — a
// one-token answer ("yes" / "42") has too little lexical surface for a meaningful evidence-contains check,
// so it falls through Unverifiable (the floor never claims a false support/refute on a bare token).
const minAnswerTokens = 1

// WebGroundedVerifier is the web-grounded INDEPENDENT verifier (the only built implementation this slice).
// It re-retrieves web evidence for the answer claim and checks the answer against THAT — the world is the
// independent signal. The deterministic floor decides web-checkable + evidence-contains; the optional
// model ceiling (AnswerSupportJudge, if the backend implements it) refines a flagged-fuzzy support call
// against the SAME re-retrieved evidence (never the original chain). With no web seam wired, or a non-
// lookup-shaped answer, it returns Unverifiable (the gate no-ops).
type WebGroundedVerifier struct {
	web     web.Web                              // the INJECTED outward signal (web.Wall at the edge, web.Fake in tests); nil ⇒ web-blind ⇒ Unverifiable
	judge   backends.AnswerSupportJudge          // the OPTIONAL Pattern-C ceiling (only the model backend implements it; nil/test ⇒ floor stands)
	queryFn func(question, answer string) string // pluggable query formulation (reuses the FLARE-shaped sub-goal query); nil ⇒ default
}

// NewWebGrounded builds a web-grounded verifier over the injected web seam + optional ceiling. queryFn may
// be nil (the default formulation is used). A nil web seam makes every Verify return Unverifiable (web-
// blind, the gate no-ops). The judge may be nil (the floor always stands then).
func NewWebGrounded(w web.Web, judge backends.AnswerSupportJudge, queryFn func(question, answer string) string) *WebGroundedVerifier {
	return &WebGroundedVerifier{web: w, judge: judge, queryFn: queryFn}
}

// Verify is the Pattern-C verification pass. BOUNDED: at most ONE independent re-retrieval + at most one
// ceiling call, no loop, no fan-out. The independence is structural: the only inputs to the verdict are
// the candidate ANSWER and the FRESHLY re-retrieved EVIDENCE — the original reasoning chain is never re-read.
func (v *WebGroundedVerifier) Verify(req Request) Result {
	answer := strings.TrimSpace(req.Answer)
	question := strings.TrimSpace(req.Question)
	// FLOOR rung 0: is this answer web-checkable at all? No web seam ⇒ web-blind ⇒ Unverifiable (no-op).
	if v.web == nil {
		return Result{Verdict: Unverifiable, Reason: "web-blind: no independent signal available"}
	}
	// FLOOR rung 0b: a non-lookup-shaped answer (empty, too long to be a single fact, or too few tokens to
	// match) is not independently web-checkable — leave it Unverifiable so the gate no-ops. A long synthesis
	// answer is NOT a single claim a re-retrieval can confirm; the verifier targets short factual answers.
	if !checkableAnswer(answer) {
		return Result{Verdict: Unverifiable, Reason: "answer is not a web-checkable factual claim"}
	}
	// Form the INDEPENDENT re-retrieval query from the question + the candidate answer (a fresh query, NOT
	// the model's chain). This is what makes the signal independent: we go back to the world with the
	// claim, rather than asking the model to re-read what it wrote.
	query := v.formulateQuery(question, answer)
	res := v.web.Fetch(query)
	if !res.OK || strings.TrimSpace(res.Text) == "" {
		// The independent read failed/blind — we could not obtain an independent signal, so we cannot
		// refute the answer. Unverifiable (no-op): the verifier never blocks an answer it could not check.
		return Result{Verdict: Unverifiable, Query: query, Source: res.Source,
			Reason: "independent re-retrieval returned no evidence"}
	}
	evidence := capRunes(res.Text, evidenceCap)
	// FLOOR rung 1: does the re-retrieved evidence literally CONTAIN / lexically align with the answer? This
	// is the deterministic, Pattern-A independence check — answer-vs-fresh-evidence, no model.
	floor, floorReason := scoreSupport(answer, evidence)
	out := Result{Verdict: floor, FloorVerdict: floor, Query: query, Evidence: evidence,
		Source: res.Source, Reason: floorReason}
	// CEILING (Pattern C): consult the model ONLY on the flagged-fuzzy band — the floor saw topical evidence
	// but the lexical overlap was inconclusive (a partial/ambiguous align). The model judges support against
	// the SAME re-retrieved evidence (never the original chain), so independence is preserved. The floor
	// stands when there is no judge or it declines (Rule 4). A clear floor verdict (clear support / clear
	// refute) is NOT escalated — the structural floor is authoritative there.
	if v.judge != nil && floor == Unverifiable && evidenceIsTopical(answer, evidence) {
		if verdictStr, ok := v.judge.JudgeAnswerSupport(question, answer, evidence, floor.String()); ok {
			if vv, ok2 := ParseVerdict(verdictStr); ok2 {
				out.Verdict = vv
				out.Escalated = true
				out.Reason = "model ceiling judged support against independent evidence: " + vv.String()
			}
		}
	}
	return out
}

// formulateQuery builds the INDEPENDENT re-retrieval query from the question + the candidate answer. It
// prefers the injected query formulation (the FLARE-shaped wrapper-strip the subconscious already uses,
// passed in by the engine) applied to the question, and appends the answer claim so the retrieval is about
// "is THIS the answer to THAT question" rather than re-asking the open question. A nil queryFn falls back
// to a trimmed question. Deterministic string op (no model, no clock, no RNG).
func (v *WebGroundedVerifier) formulateQuery(question, answer string) string {
	base := question
	if v.queryFn != nil {
		base = v.queryFn(question, answer)
	}
	base = strings.TrimSpace(base)
	answer = strings.TrimSpace(answer)
	if base == "" {
		return answer
	}
	if answer == "" {
		return base
	}
	// "<question> <answer>" — the retrieval is about whether the world corroborates the answer to the
	// question, not a re-ask of the bare question (forward-looking, FLARE-style).
	return base + " " + answer
}

// checkableAnswer reports whether a candidate answer is a short factual/lookup claim worth independently
// re-retrieving (non-empty, within the single-fact length bound, enough tokens to match). A long synthesis
// or a bare token is left Unverifiable so the gate no-ops on it.
//
// IT EXCLUDES COMPUTATIONAL CLAIMS. A claim that is a computation / equality ("7 × 8 = 56", "x = 42") is
// NOT a web-lookup question — it already has its own INDEPENDENT signal (the deterministic compute grounder
// — "math doesn't lie"), so routing it to a web re-retrieval is the WRONG signal and risks a FALSE refute
// (a correct sum the world's snippet does not happen to mention). Such a claim is left Unverifiable here so
// the web gate no-ops and the compute grounding does its job. The verifier targets FACTUAL/LOOKUP answers
// (who/what/where), the lane where re-retrieving the world is the right independent check.
func checkableAnswer(answer string) bool {
	if answer == "" {
		return false
	}
	if len([]rune(answer)) > answerClaimMaxLen {
		return false
	}
	if len(tokens(answer)) < minAnswerTokens {
		return false
	}
	if isComputational(answer) {
		return false // a computation has its own independent signal (compute grounding) — not a web lookup
	}
	if !hasAlphaContentToken(answer) {
		return false // a purely numeric/symbolic answer (e.g. "56", "3.14") is not a web-lookup factual claim
	}
	return true
}

// isComputational reports whether the answer reads as a computation / equality rather than a factual
// lookup — it contains an equals sign or a math operator between values. Deterministic; conservative (it
// keys on the equality/operator surface, so a factual answer like "founded in 1842" is NOT computational).
func isComputational(answer string) bool {
	if strings.Contains(answer, "=") {
		return true
	}
	// A math operator flanked by digits ("7 x 8", "7×8", "12 + 30") — a computation, not a lookup. The
	// unicode multiply sign and the ascii operators are the common forms the compute grounder recognises.
	for _, op := range []string{"×", "÷", "*", " x ", " + ", " - ", " / "} {
		if i := strings.Index(answer, op); i > 0 {
			before := strings.TrimSpace(answer[:i])
			after := strings.TrimSpace(answer[i+len(op):])
			if endsWithDigit(before) && startsWithDigit(after) {
				return true
			}
		}
	}
	return false
}

// hasAlphaContentToken reports whether the answer has at least one alphabetic content token (a word, not a
// bare number/symbol) — the surface a web lookup matches on. "Ada Lovelace" has one; "56" / "3.14" do not.
func hasAlphaContentToken(answer string) bool {
	for _, t := range contentTokens(answer) {
		for _, r := range t {
			if r >= 'a' && r <= 'z' {
				return true
			}
		}
	}
	return false
}

// endsWithDigit / startsWithDigit are the cheap digit-boundary tests isComputational uses to confirm a math
// operator sits between numeric values (vs. an arithmetic operator appearing incidentally in prose).
func endsWithDigit(s string) bool {
	if s == "" {
		return false
	}
	r := s[len(s)-1]
	return r >= '0' && r <= '9'
}

func startsWithDigit(s string) bool {
	if s == "" {
		return false
	}
	r := s[0]
	return r >= '0' && r <= '9'
}

// scoreSupport is the deterministic FLOOR: does the re-retrieved evidence support the answer? It is a
// content-word containment/overlap test — the INDEPENDENT, Pattern-A check (answer-vs-evidence, no model):
//   - Supported: the evidence CONTAINS the answer as a phrase, OR every content token of the answer appears
//     in the evidence (a full lexical cover — the world corroborates the claim).
//   - Unverifiable (the FUZZY band the ceiling refines): SOME but not all answer content tokens appear — the
//     evidence is topical but the lexical align is inconclusive (a model ceiling, if present, decides).
//   - Unsupported: NONE of the answer's content tokens appear in the topical evidence — the world does not
//     corroborate the claim at all (a refutation by absence).
//
// It never claims support/refute it cannot justify from the evidence text.
func scoreSupport(answer, evidence string) (Verdict, string) {
	ansLow := strings.ToLower(answer)
	evLow := strings.ToLower(evidence)
	// Phrase containment is the strongest, cheapest support signal: the world's evidence literally contains
	// the claim. (A short answer like a name / value is exactly this case.)
	if phrase := strings.TrimSpace(ansLow); phrase != "" && strings.Contains(evLow, phrase) {
		return Supported, "evidence contains the answer verbatim"
	}
	ansTok := contentTokens(answer)
	if len(ansTok) == 0 {
		// The answer was all stop-words / punctuation — nothing to match against. Unverifiable (no-op).
		return Unverifiable, "answer has no content tokens to match"
	}
	evSet := tokenSet(evidence)
	hits := 0
	for _, t := range ansTok {
		if _, ok := evSet[t]; ok {
			hits++
		}
	}
	switch {
	case hits == len(ansTok):
		return Supported, "evidence covers every content token of the answer"
	case hits == 0:
		return Unsupported, "evidence (topical) contains none of the answer's content tokens"
	default:
		return Unverifiable, "evidence partially aligns with the answer (inconclusive — fuzzy band)"
	}
}

// evidenceIsTopical reports whether the re-retrieved evidence is at least on-topic for the answer (it
// shares at least one content token). The ceiling is only consulted when there is topical evidence to
// judge against — a totally off-topic re-retrieval is left to the deterministic floor, not the model.
func evidenceIsTopical(answer, evidence string) bool {
	evSet := tokenSet(evidence)
	for _, t := range contentTokens(answer) {
		if _, ok := evSet[t]; ok {
			return true
		}
	}
	return false
}

// stopWords are common function words excluded from the content-token overlap so the support check keys on
// the meaningful surface of the answer (names, values, nouns), not the connective tissue every snippet has.
var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "of": {}, "to": {}, "in": {}, "is": {}, "are": {},
	"was": {}, "were": {}, "be": {}, "been": {}, "it": {}, "this": {}, "that": {}, "for": {}, "on": {},
	"with": {}, "as": {}, "at": {}, "by": {}, "from": {}, "they": {}, "he": {}, "she": {}, "yes": {},
	"no": {}, "not": {}, "same": {}, "both": {}, "their": {}, "its": {},
}

// tokens splits text into lower-cased alphanumeric tokens (punctuation is a separator). Deterministic.
func tokens(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	return fields
}

// contentTokens are the answer's tokens with stop-words removed — the meaningful surface to match.
func contentTokens(text string) []string {
	out := make([]string, 0, 8)
	for _, t := range tokens(text) {
		if _, stop := stopWords[t]; stop {
			continue
		}
		out = append(out, t)
	}
	return out
}

// tokenSet is the set of all tokens in text (membership test for the overlap). Includes stop-words on the
// EVIDENCE side (we only filter stop-words on the answer side — what we are matching FOR).
func tokenSet(text string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, t := range tokens(text) {
		set[t] = struct{}{}
	}
	return set
}

// capRunes truncates s to at most n code points (Unicode-safe), no ellipsis — bounds the audit trail.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
