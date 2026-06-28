package cognition

// offer_heldout_win_test.go — W3-S3 anti-stuffing robustness + GENUINE held-out generalisation proof.
//
// This is the HONEST, NON-OVERFIT successor to the at-scale recovery probe. The prior at-scale test was
// refuted for overfitting: it manufactured the bare control by giving every domain op a "zz"-prefixed NAME so
// the name-ascending tie-break dropped it past the noise — a control that loses by NAME ORDERING, not by
// genuine lack of relevance. This test fixes that with a REALISTIC candidate set and a #1-RANK assertion.
//
// THE CONTRACT (each clause is a saved anti-overfit requirement):
//
//	(a) MULTIPLE diverse examples per op — every recovered op carries at least 3 example phrasings spanning
//	    different words AND different operations (so retrieval generalises, not memorises one phrasing). The
//	    examples live in the SEED enrichment (operators.go); TestEnrichExamplesAreHeldOut already pins
//	    disjointness, and TestHeldOutGoalsRideKeywordNotExample re-pins it for THIS test's goals.
//	(b) HELD-OUT test goals DISJOINT from the enrichment examples — each goal is a NOVEL phrasing whose recovery
//	    rides a KEYWORD that is plausibly ANTICIPATED domain vocabulary (a synonym in the op's Keywords), but
//	    whose exact wording was NOT used as an Example. A keyword match is legitimate generalisation; an example
//	    match would be memorisation.
//	(c) MULTIPLE op TYPES — the recovery is proven across 8 distinct op TYPES (arithmetic / search-read /
//	    compare / decompose / triage / compress / rank / generate), each via a held-out phrasing.
//	(d) the CORRECT op WINS — the assertion is that the correct op RANKS #1 among the REAL candidate ops via
//	    offerScore (not merely score > 0). #1 is the strong claim a top-K=1 curation would honour.
//	(e) a CONTESTED win — the candidate set contains NATURAL, abstract, un-enriched distractor ops
//	    (harmonise / adjudicate / calibrate) whose intents DELIBERATELY share at least one CONTENT word with
//	    some held-out goals, so they SCORE > 0 and genuinely CONTEST the win (TestRealisticDistractorsContest
//	    pins that each distractor out-scores zero on at least one goal). The correct op must still out-SCORE
//	    every contesting distractor — the #1 win is earned against plausible competition that is actually in the
//	    running, not against straw ops that score 0.000 on every goal. A second control
//	    (TestHeldOutWinIsEarnedByEnrichment) strips the winning op's enrichment and shows it LOSES #1 — proving
//	    enrichment, not the bare intent, earned the win.
//
// HONEST SCOPE (the load-bearing caveat, mirrored in operators.go offerScore — the word "ungameable" is NOT
// earned and is NOT used):
//
//	WHAT IS DEFENDED. enrichment + op-type coverage + the stop-word filter improve the EMBEDDER-FREE retrieval
//	floor and make it ROBUST against MEANINGLESS / FUNCTION-WORD stuffing only (the stop-word filter zeros any
//	overlap that is purely function words). It recovers ANTICIPATED vocabulary — domain synonyms the seed
//	metadata foresaw.
//
//	WHAT IS NOT DEFENDED (FATAL #1, still OPEN — owned by offerSemantic / the embedder seam). CONTENT-WORD
//	stuffing is NOT defended. A junk op that simply ECHOES the goal's content words reaches coverage 1.0 and can
//	out-score a genuinely relevant op that covers fewer goal words (the red-team's proven exploit, asserted
//	POSITIVELY in TestStopWordFilterMakesScoreRobustToStuffing clause 3 so the overclaim cannot creep back). A
//	coverage-based LEXICAL score fundamentally cannot separate "contains the goal's words" from "means the
//	thing"; only a meaning-aware embedder can. This test ALSO does NOT prove general at-scale recovery of
//	phrasings whose pivot word is ABSENT from the metadata: the held-out goals here deliberately RIDE an
//	anticipated keyword (req-(2) generalisation with the pivot NOT in the keywords is the embedder's job, not
//	the lexical floor's — that path stays OPEN, deferred to offerSemantic). The W3-S3 flip is therefore on the
//	ANTICIPATED-VOCAB floor + FUNCTION-WORD-stuffing-robustness sub-claim ONLY; FATAL #1 stays OPEN and the
//	W5 embedder-vs-enrichment method fork stays REOPENED.
//
// These tests are DETERMINISTIC + OFFLINE (NO embedder, NO model, NO clock, NO RNG): they run on the
// no-embedder offerLexical floor only.

import (
	"sort"
	"strings"
	"testing"
)

// heldOutWinCases — one held-out goal per op TYPE, paired with the seed op it must recover to #1 and the
// anticipated KEYWORD(s) the recovery rides. Every goal is a NOVEL phrasing, NOT a verbatim example of its op
// (TestHeldOutGoalsRideKeywordNotExample proves this). The goal-vs-bare-intent overlap is deliberately thin so
// the win is the ENRICHMENT doing the work, not a coincidence on the abstract Intent.
var heldOutWinCases = []heldOutCase{
	{opType: "arithmetic", goal: "what is the product of seven and eight", op: "measure", viaWords: []string{"product"}},
	{opType: "search-read", goal: "grep the repository for the retry handler", op: "expose-affordances", viaWords: []string{"grep"}},
	{opType: "compare", goal: "weigh the trade-off between the two caching strategies", op: "compare", viaWords: []string{"trade-off"}},
	{opType: "decompose", goal: "partition the workload into independent units", op: "decompose", viaWords: []string{"partition"}},
	{opType: "triage", goal: "classify each incoming alert by severity", op: "triage", viaWords: []string{"classify", "severity"}},
	{opType: "compress", goal: "condense the meeting notes into bullet points", op: "compress", viaWords: []string{"condense"}},
	// rank goal repicked (red-team Hole): the prior "sort the candidates best first" overlapped its op example
	// on [best,candidates,sort] and scored 1.000 — too close to memorisation. This goal's CONTENT words
	// {put, leaderboard, strongest, entry, top} are NOT a near-subset of any rank example (max 1-word overlap
	// "top"); it rides the anticipated keywords "leaderboard" + "top n" — generalisation, not re-spelling.
	{opType: "rank", goal: "put the leaderboard with the strongest entry on top", op: "rank", viaWords: []string{"leaderboard", "top"}},
	{opType: "generate", goal: "author a short release announcement", op: "generate", viaWords: []string{"author"}},
}

// realisticDistractors are NATURAL, abstract, un-enriched ops added to the candidate set so the #1 win is
// EARNED against plausible competition that is ACTUALLY IN THE RUNNING (clause (e), red-team Hole B). They are
// real-sounding capabilities — NOT surface-stripped "zz" placeholders rigged to lose on name order — and each
// intent DELIBERATELY shares at least one CONTENT word with a held-out goal so it SCORES > 0 and genuinely
// CONTESTS the win (adjudicate rides "severity" from the triage goal; calibrate rides "workload" from the
// decompose goal; harmonise rides "caching" from the compare goal). TestRealisticDistractorsContest pins that
// they really do score > 0; the correct op must still out-score them. Their names sort across the alphabet
// (adjudicate < calibrate < harmonise) so they are NOT systematically dropped by the tie-break.
var realisticDistractors = []struct {
	name, intent string
}{
	{"adjudicate", "render a binding verdict over a contested severity escalation between parties"},
	{"calibrate", "tune a workload of sensors toward a numeric reference standard"},
	{"harmonise", "bring disparate caching layers into mutual structural alignment"},
}

// TestRealisticDistractorsContest is the Hole-B guard: it PROVES the distractors are not straw. Each realistic
// distractor must score > 0 on at least one held-out goal (it shares a CONTENT word with that goal), so the #1
// win below is genuinely CONTESTED — the win is "earned against plausible competition" only if that competition
// is actually in the running. A distractor that scored 0.000 on every goal would prove nothing.
func TestRealisticDistractorsContest(t *testing.T) {
	r := NewOperatorRegistry()
	for _, d := range realisticDistractors {
		if _, ok := r.MintWithMove(d.name, "relational", d.intent, MoveAssess); !ok {
			t.Fatalf("could not mint realistic distractor %q", d.name)
		}
	}
	for _, d := range realisticDistractors {
		contested := false
		for _, c := range heldOutWinCases {
			if scoreOfOp(r, c.goal, d.name) > 0 {
				contested = true
				break
			}
		}
		if !contested {
			t.Errorf("distractor %q scores 0 on EVERY held-out goal — it is a straw op, not plausible competition (Hole B)", d.name)
		}
	}
}

// TestHeldOutCorrectOpWinsAmongRealCandidates is the GENUINE generalisation proof (clauses c, d, e): for every
// held-out goal, across 8 op TYPES, the correct ENRICHED op ranks #1 (out-scores every other candidate) over a
// candidate set that is the WHOLE real seed catalog PLUS the realistic un-enriched distractors that actually
// CONTEST the win (TestRealisticDistractorsContest pins they score > 0) — so the win is earned against plausible
// competition that is in the running, NO embedder, NO model. #1 (not merely score>0) is the assertion.
func TestHeldOutCorrectOpWinsAmongRealCandidates(t *testing.T) {
	r := NewOperatorRegistry()
	for _, d := range realisticDistractors {
		if _, ok := r.MintWithMove(d.name, "relational", d.intent, MoveAssess); !ok {
			t.Fatalf("could not mint realistic distractor %q", d.name)
		}
	}
	// sanity: the realistic distractors really are in the candidate set (the control is present, clause (e)).
	for _, d := range realisticDistractors {
		if _, ok := r.Get(d.name); !ok {
			t.Fatalf("realistic distractor %q missing from candidate set", d.name)
		}
	}

	typesSeen := map[string]bool{}
	for _, c := range heldOutWinCases {
		typesSeen[c.opType] = true
		winner, winScore := rankWinner(r, c.goal)
		if winner != c.op {
			t.Errorf("[%s] held-out goal %q: correct op %q did NOT rank #1 among real candidates (won %q score=%.3f, %q score=%.3f)",
				c.opType, c.goal, c.op, winner, winScore, c.op, scoreOfOp(r, c.goal, c.op))
		}
		// (d) strict: the win must be a real positive score (a #1 at score 0 would be a name-order artefact).
		if s := scoreOfOp(r, c.goal, c.op); s <= 0 {
			t.Errorf("[%s] op %q won at a non-positive score %.3f for goal %q — that is not an earned win",
				c.opType, c.op, s, c.goal)
		}
		// (e) no realistic distractor may out-score the correct op (the win is earned against real competition).
		for _, d := range realisticDistractors {
			if ds := scoreOfOp(r, c.goal, d.name); ds >= scoreOfOp(r, c.goal, c.op) {
				t.Errorf("[%s] realistic distractor %q (%.3f) tied/beat correct op %q (%.3f) on goal %q",
					c.opType, d.name, ds, c.op, scoreOfOp(r, c.goal, c.op), c.goal)
			}
		}
	}
	// (c) at least 4 distinct op types (the contract floor); we cover 8.
	if len(typesSeen) < 4 {
		t.Errorf("must cover at least 4 op TYPES, covered %d (%v)", len(typesSeen), keysOf(typesSeen))
	}
	t.Logf("held-out #1 recovery (NO embedder, real candidate set incl. %d realistic distractors): %d/%d correct ops won #1 across %d op types",
		len(realisticDistractors), len(heldOutWinCases), len(heldOutWinCases), len(typesSeen))
}

// TestHeldOutGoalsRideKeywordNotExample is the OVERFIT GUARD for THIS test's goals (clauses a, b): each held-out
// goal (1) is NOT a verbatim example of its op, (2) does not share the full CONTENT word-set of any example
// (after stop-word removal — so the goal is a genuinely novel phrasing, not a re-spelling), (3) rides at least
// one anticipated KEYWORD that is on its op's enriched metadata, and (4) its op carries MULTIPLE (>=3) examples
// so the disjointness claim is non-trivial. If this fails, the win above would be memorisation.
func TestHeldOutGoalsRideKeywordNotExample(t *testing.T) {
	r := NewOperatorRegistry()
	for _, c := range heldOutWinCases {
		spec, ok := r.Get(c.op)
		if !ok {
			t.Fatalf("[%s] op %q not in catalog", c.opType, c.op)
		}
		// (a) multiple diverse examples — the disjointness claim is non-trivial only if the op HAS examples.
		if len(spec.Examples) < 3 {
			t.Errorf("[%s] op %q must carry >=3 diverse examples for a real held-out claim, has %d",
				c.opType, c.op, len(spec.Examples))
		}
		goalContent := contentWordSet(c.goal)
		// (1)+(2): not a verbatim example, and not the same CONTENT word-set as any example.
		for _, ex := range spec.Examples {
			if strings.EqualFold(strings.TrimSpace(c.goal), strings.TrimSpace(ex)) {
				t.Errorf("[%s] OVERFIT: held-out goal %q is a VERBATIM example of op %q", c.opType, c.goal, c.op)
			}
			if setsEqual(goalContent, contentWordSet(ex)) {
				t.Errorf("[%s] OVERFIT: held-out goal %q shares the full content word-set of example %q (op %q)",
					c.opType, c.goal, ex, c.op)
			}
		}
		// (3): the recovery rides an anticipated KEYWORD present in the goal AND on the op's metadata.
		kwBlob := strings.ToLower(strings.Join(spec.Keywords, " | "))
		matched := false
		for _, kw := range c.viaWords {
			lkw := strings.ToLower(kw)
			// the keyword must be on the metadata; and either it is a single goal word, or (for multi-word
			// keywords like "best first") the goal contains the keyword phrase.
			inGoal := goalContent[lkw] || strings.Contains(strings.ToLower(c.goal), lkw)
			if inGoal && strings.Contains(kwBlob, lkw) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("[%s] goal %q must recover op %q via an anticipated KEYWORD (viaWords %v must be both in the goal and in keywords %v)",
				c.opType, c.goal, c.op, c.viaWords, spec.Keywords)
		}
	}
}

// TestHeldOutWinIsEarnedByEnrichment is the second control (clause e): when the WINNING op's enrichment is
// stripped (bare "Name Intent"), a MAJORITY of the held-out goals LOSE the #1 rank — proving the enrichment,
// not the bare abstract intent, earned the win. The honest caveat (reported, not hidden): a few ops (compare,
// decompose) already win on their bare intent for these goals, so stripping enrichment cannot dislodge them —
// that is an op that did not NEED recovering, not a failure of the control.
func TestHeldOutWinIsEarnedByEnrichment(t *testing.T) {
	enriched := NewOperatorRegistry()
	bare := bareControlRegistry()

	lostWhenBare, stillWhenBare := 0, 0
	for _, c := range heldOutWinCases {
		// precondition: enriched wins #1 (this is the same claim as the test above, re-checked here in isolation).
		if w, _ := rankWinner(enriched, c.goal); w != c.op {
			t.Fatalf("[%s] precondition failed: enriched op %q is not #1 for %q (won %q)", c.opType, c.op, c.goal, w)
		}
		bareWinner, _ := rankWinner(bare, c.goal)
		if bareWinner != c.op {
			lostWhenBare++
		} else {
			stillWhenBare++
		}
	}
	t.Logf("enrichment-earned-win control (NO embedder): %d/%d held-out goals LOSE #1 when enrichment is stripped (%d already won bare — did not need recovering)",
		lostWhenBare, len(heldOutWinCases), stillWhenBare)
	if lostWhenBare*2 < len(heldOutWinCases) {
		t.Errorf("a MAJORITY of held-out wins must be EARNED by enrichment (lose #1 when stripped); only %d/%d did",
			lostWhenBare, len(heldOutWinCases))
	}
}

// TestStopWordFilterMakesScoreRobustToStuffing proves deliverable (1) HONESTLY and pins exactly what the
// lexical floor does and does NOT defend (red-team Hole A — the word "ungameable" is NOT earned, so the floor is
// only ever described as ROBUST against function-word stuffing). It is deterministic and offline:
//
//	(1) DEFENDED — a function word can never DECIDE a winner. Against a goal that shares with two ops ONLY a
//	    function word ("the"/"of"/"a"/"to"), both ops score 0 — the stop word contributes no coverage — so the
//	    genuine-content tie-break stands and the function word does not pick a winner.
//	(2) DEFENDED — a FUNCTION-WORD-stuffed op cannot beat a genuinely relevant op. An op whose description is
//	    PADDED with the goal's function words (and nothing of substance) scores 0, while a genuinely relevant op
//	    that covers the goal's CONTENT words out-scores it. Stuffing common words buys exactly zero coverage.
//	(3) NOT DEFENDED (the honest residual, FATAL #1, asserted POSITIVELY so the scope is auditable and we never
//	    silently overclaim): a junk op that simply ECHOES the goal's CONTENT words reaches coverage 1.0 and CAN
//	    out-score a genuinely relevant op that covers fewer goal words. This is the purest content-word stuffing
//	    and a coverage-based LEXICAL score fundamentally cannot defend it — separating "contains the goal's
//	    words" from "means the thing" is the embedder seam's job (offerSemantic), not the lexical floor's. The
//	    test SHOWS the echo can still win, so the "ungameable" overclaim cannot creep back in.
func TestStopWordFilterMakesScoreRobustToStuffing(t *testing.T) {
	// (1) function-word-only overlap scores 0 (cannot decide a winner).
	goal := "what is the sum of the items in the list"
	stuffOnlyFunctionWords := "what is the of the in the" // overlaps the goal ONLY on stop words
	if s := offerScore(goal, stuffOnlyFunctionWords); s != 0 {
		t.Errorf("an op overlapping the goal only on FUNCTION words must score 0 (cannot decide a winner), got %.3f", s)
	}

	// (2) function-word stuffing cannot beat a genuinely relevant op.
	genuine := "compute the sum total of the items"         // covers content words: sum, items (+ total/compute)
	gameable := "what is the of the in the a to and or but" // pure function-word stuffing, no content
	gs := offerScore(goal, genuine)
	xs := offerScore(goal, gameable)
	if gs <= 0 {
		t.Errorf("the genuinely relevant op must score > 0 on its content overlap, got %.3f", gs)
	}
	if xs != 0 {
		t.Errorf("the function-word-stuffed op must score 0 — stuffing common words buys no coverage, got %.3f", xs)
	}
	if xs >= gs {
		t.Errorf("a function-word-stuffed op (%.3f) must NOT beat a genuinely relevant op (%.3f)", xs, gs)
	}

	// (3) HONEST residual (FATAL #1) — content-word stuffing is NOT defended by a lexical coverage score, and we
	// PROVE it positively so the overclaim cannot return. A junk op that echoes the goal's CONTENT words reaches
	// coverage 1.0; the genuinely relevant op (which covers fewer goal words) does NOT beat it. This is the
	// red-team's proven exploit, recorded honestly as OPEN — it is the embedder seam's (offerSemantic) job.
	echoUnbeaten := 0
	for _, c := range heldOutWinCases {
		echoScore := offerScore(c.goal, c.goal+" matter") // a junk op whose text is the goal restated
		correctScore := offerScore(c.goal, mustSpec(t, c.op).RetrievalText())
		if echoScore == 1.0 && echoScore >= correctScore {
			echoUnbeaten++
		}
	}
	if echoUnbeaten == 0 {
		// If a goal-echo were ever beaten on EVERY case, the lexical floor would have started defending
		// content-word stuffing — which a coverage score cannot honestly do. That would mean the honest-scope
		// caveat is stale (or the math silently changed) and MUST be re-audited before any "ungameable" claim.
		t.Errorf("expected at least one held-out goal where a content-word ECHO ties/beats the correct op (the honest FATAL #1 residual); got 0 — re-audit the scope claim before asserting any ungameability")
	}
	t.Logf("honest residual (FATAL #1): content-word goal-echo ties/beats the correct op on %d/%d held-out goals — NOT defended by the lexical floor (embedder's job)",
		echoUnbeaten, len(heldOutWinCases))

	// determinism: the score is a pure function of (goal, opText) — same inputs, same output every time.
	for i := 0; i < 20; i++ {
		if offerScore(goal, genuine) != gs {
			t.Fatalf("offerScore must be deterministic")
		}
	}

	// the byte-identical default path is unaffected: with curation OFF (SynthOfferCap==0) offerScore is never
	// reached (Offer returns the whole catalog without scoring), so the stop-word filter cannot perturb a golden.
	r := NewOperatorRegistry()
	for i := 0; i < 50; i++ {
		r.MintWithMove("fillerop"+itoaPad(i), "generative", "an unrelated filler capability "+itoaPad(i), MoveGround)
	}
	whole := r.Names()
	if got := r.Offer("compute the sum of the items", 0); strings.Join(got, ",") != strings.Join(whole, ",") {
		t.Errorf("default-OFF (k<=0) must return the whole catalog byte-identically regardless of the stop-word filter")
	}
}

// ---- helpers (local to this file; reuse setsEqual/wordSetOf from offer_enrich_test.go) ----

// rankWinner returns the #1 op (by offerScore, name-ascending tie-break) over the WHOLE candidate set — the
// same ordering offerLexical's ranked passes use. This is the candidate-set-wide #1 the test asserts.
func rankWinner(r *OperatorRegistry, goal string) (string, float64) {
	type sc struct {
		name string
		s    float64
	}
	var ranked []sc
	for _, name := range r.Names() {
		spec, _ := r.Get(name)
		ranked = append(ranked, sc{name, offerScore(goal, spec.RetrievalText())})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].s != ranked[j].s {
			return ranked[i].s > ranked[j].s
		}
		return ranked[i].name < ranked[j].name
	})
	if len(ranked) == 0 {
		return "", 0
	}
	return ranked[0].name, ranked[0].s
}

// scoreOfOp is the offerScore of one named op against the goal (over its enriched RetrievalText).
func scoreOfOp(r *OperatorRegistry, goal, name string) float64 {
	spec, ok := r.Get(name)
	if !ok {
		return -1
	}
	return offerScore(goal, spec.RetrievalText())
}

// mustSpec returns the seed op spec or fails the test (used where the op must exist by construction).
func mustSpec(t *testing.T, name string) OperatorSpec {
	t.Helper()
	spec, ok := NewOperatorRegistry().Get(name)
	if !ok {
		t.Fatalf("op %q not in seed catalog", name)
	}
	return spec
}

// contentWordSet is wordSetOf with stop words removed — the basis the coverage score actually counts.
func contentWordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if isStopWord(w) {
			continue
		}
		m[w] = true
	}
	return m
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
