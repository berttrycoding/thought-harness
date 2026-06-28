package verify

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/web"
)

// fakeWeb is a query-CONDITIONED web seam for the verifier tests: it returns evidence chosen by the query
// content (the production web.Fake returns a fixed snippet regardless of query, which cannot model a
// "supported vs unsupported" re-retrieval). It records the last query it was asked so a test can assert the
// INDEPENDENT re-retrieval query was actually formed from the question + answer (not the model's chain).
type fakeWeb struct {
	byContains map[string]web.Result // first key whose substring appears in the query wins
	def        web.Result            // fallback when no key matches
	lastQuery  string
}

func (f *fakeWeb) Fetch(query string) web.Result {
	f.lastQuery = query
	low := strings.ToLower(query)
	for k, r := range f.byContains {
		if strings.Contains(low, strings.ToLower(k)) {
			return r
		}
	}
	return f.def
}

// supportingJudge is a test-local AnswerSupportJudge ceiling that always returns the given verdict. It
// stands in for the model backend's optional capability (the real test double declines it, so the floor
// stands offline; this lets the ceiling path be exercised deterministically). It records the inputs so a
// test can assert the model judged against the EVIDENCE, not the original chain.
type supportingJudge struct {
	verdict     string
	ok          bool
	gotQuestion string
	gotAnswer   string
	gotEvidence string
	gotFloor    string
	called      bool
}

func (j *supportingJudge) JudgeAnswerSupport(question, answer, evidence, floorVerdict string) (string, bool) {
	j.called = true
	j.gotQuestion, j.gotAnswer, j.gotEvidence, j.gotFloor = question, answer, evidence, floorVerdict
	return j.verdict, j.ok
}

// TestWebBlindIsUnverifiable: a nil web seam ⇒ Unverifiable ⇒ the gate is a no-op (the verifier never
// blocks an answer it cannot independently check). This is the byte-identical-when-web-blind guarantee.
func TestWebBlindIsUnverifiable(t *testing.T) {
	v := NewWebGrounded(nil, nil, nil)
	res := v.Verify(Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"})
	if res.Verdict != Unverifiable {
		t.Fatalf("web-blind verifier must return Unverifiable, got %v (%s)", res.Verdict, res.Reason)
	}
}

// TestSupportedCommits: when the RE-RETRIEVED evidence contains the answer, the floor verdict is Supported
// — the world corroborates the claim, so the commit stands. This is the cognition: an answer the
// independent signal backs is admitted.
func TestSupportedCommits(t *testing.T) {
	fw := &fakeWeb{
		def: web.Result{Text: "Acme Corp was founded by Ada Lovelace in 1842 in London.", OK: true, Source: "fake"},
	}
	v := NewWebGrounded(fw, nil, nil)
	res := v.Verify(Request{Question: "Who founded Acme Corp?", Answer: "Ada Lovelace"})
	if res.Verdict != Supported {
		t.Fatalf("evidence contains the answer ⇒ Supported, got %v (%s)", res.Verdict, res.Reason)
	}
	if res.Source != "fake" {
		t.Errorf("Source must carry the independent signal's provenance, got %q", res.Source)
	}
}

// TestUnsupportedRefuses: the canonical cognition test. The committed answer is a SYSTEMATIC mis-read (a
// wrong name) that the model's own re-read would never catch (same-model ceiling); but the INDEPENDENT
// re-retrieved evidence names a DIFFERENT founder and contains NONE of the wrong answer's content tokens —
// so the floor returns Unsupported. This is exactly the break the verifier exists for: an independent
// signal refutes an answer self-judging could not.
func TestUnsupportedRefuses(t *testing.T) {
	fw := &fakeWeb{
		def: web.Result{Text: "Acme Corp was founded by Grace Hopper in 1842.", OK: true, Source: "fake"},
	}
	v := NewWebGrounded(fw, nil, nil)
	res := v.Verify(Request{Question: "Who founded Acme Corp?", Answer: "Zorblatt Penguintron"})
	if res.Verdict != Unsupported {
		t.Fatalf("topical evidence with none of the answer's content tokens ⇒ Unsupported, got %v (%s)",
			res.Verdict, res.Reason)
	}
}

// TestRetrievalFailureIsUnverifiable: when the independent re-retrieval fails (OK=false / empty), the
// verifier returns Unverifiable — it never refutes an answer on a failed read (it could not obtain an
// independent signal). The gate no-ops, the answer stands.
func TestRetrievalFailureIsUnverifiable(t *testing.T) {
	fw := &fakeWeb{def: web.Result{OK: false}}
	v := NewWebGrounded(fw, nil, nil)
	res := v.Verify(Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"})
	if res.Verdict != Unverifiable {
		t.Fatalf("a failed re-retrieval ⇒ Unverifiable (never refute on no evidence), got %v", res.Verdict)
	}
}

// TestNonLookupAnswerIsUnverifiable: an answer too long to be a single web-checkable fact (a synthesis) is
// left Unverifiable so the gate no-ops on it — the verifier targets short factual answers.
func TestNonLookupAnswerIsUnverifiable(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "irrelevant", OK: true}}
	v := NewWebGrounded(fw, nil, nil)
	long := strings.Repeat("this is a long multi-sentence synthesis answer that is not a single fact. ", 5)
	res := v.Verify(Request{Question: "Explain X", Answer: long})
	if res.Verdict != Unverifiable {
		t.Fatalf("a long synthesis answer ⇒ Unverifiable (not a single web-checkable fact), got %v", res.Verdict)
	}
}

// TestComputationalAnswerIsUnverifiable: a computation / equality answer ("7 × 8 = 56", "x = 42", a bare
// number) is NOT a web-lookup claim — it has its own independent signal (the compute grounder), so the web
// verifier leaves it Unverifiable (the gate no-ops). This prevents a FALSE refute of a correct sum the
// world's snippet does not happen to mention.
func TestComputationalAnswerIsUnverifiable(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "completely unrelated weather snippet", OK: true}}
	v := NewWebGrounded(fw, nil, nil)
	for _, ans := range []string{"7 × 8 = 56", "I can see it — 7 × 8 = 56.", "x = 42", "56", "3.14", "12 + 30 = 42"} {
		res := v.Verify(Request{Question: "compute it", Answer: ans})
		if res.Verdict != Unverifiable {
			t.Errorf("a computational/numeric answer %q must be Unverifiable (compute grounding owns it), got %v",
				ans, res.Verdict)
		}
	}
}

// TestFactualAnswerIsChecked: a factual lookup answer with an alphabetic content token IS checked (the
// complement of the computational guard) — the verifier does not over-exclude real lookups.
func TestFactualAnswerIsChecked(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "Acme was founded by Ada Lovelace.", OK: true}}
	v := NewWebGrounded(fw, nil, nil)
	res := v.Verify(Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"})
	if res.Verdict == Unverifiable {
		t.Fatalf("a factual lookup answer must be checked (not over-excluded), got %v (%s)", res.Verdict, res.Reason)
	}
}

// TestFloorStandsWhenJudgeDeclines: on the FUZZY band (partial lexical align), the floor verdict is
// Unverifiable; with a judge that DECLINES (ok=false, the test double's behaviour), the floor STANDS
// (Unverifiable, no escalation). This is the Pattern-C floor-stands guarantee — offline + deterministic.
func TestFloorStandsWhenJudgeDeclines(t *testing.T) {
	// Evidence shares ONE of the answer's two content tokens ⇒ floor Unverifiable (fuzzy).
	fw := &fakeWeb{def: web.Result{Text: "The report mentions Lovelace prominently.", OK: true, Source: "fake"}}
	declining := &supportingJudge{ok: false}
	v := NewWebGrounded(fw, declining, nil)
	res := v.Verify(Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"})
	if res.FloorVerdict != Unverifiable {
		t.Fatalf("partial align ⇒ floor Unverifiable (fuzzy band), got floor %v", res.FloorVerdict)
	}
	if res.Verdict != Unverifiable || res.Escalated {
		t.Fatalf("a declining judge ⇒ the floor stands (Unverifiable, not escalated), got %v escalated=%v",
			res.Verdict, res.Escalated)
	}
	if !declining.called {
		t.Error("the judge should have been consulted on the fuzzy band (it declined; the floor stood)")
	}
}

// TestCeilingRefinesFuzzyBand: on the fuzzy band, a judge that ANSWERS may move the verdict — here it
// refutes (Unsupported) an answer the floor could not decide. CRUCIALLY it judges against the EVIDENCE +
// the question, NOT the original reasoning chain (asserted via the recorded inputs) — the independence the
// ceiling must preserve.
func TestCeilingRefinesFuzzyBand(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "The report mentions Lovelace prominently.", OK: true, Source: "fake"}}
	judge := &supportingJudge{verdict: "unsupported", ok: true}
	v := NewWebGrounded(fw, judge, nil)
	res := v.Verify(Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"})
	if res.Verdict != Unsupported || !res.Escalated {
		t.Fatalf("an answering judge on the fuzzy band may move the verdict, got %v escalated=%v",
			res.Verdict, res.Escalated)
	}
	// Independence: the ceiling judged against the RE-RETRIEVED EVIDENCE, never a re-read of the chain.
	if !strings.Contains(judge.gotEvidence, "Lovelace") {
		t.Errorf("the ceiling must judge against the independent EVIDENCE, got evidence=%q", judge.gotEvidence)
	}
	if judge.gotAnswer != "Ada Lovelace" {
		t.Errorf("the ceiling must judge the committed ANSWER, got answer=%q", judge.gotAnswer)
	}
}

// TestClearVerdictNotEscalated: a CLEAR floor verdict (full support / full refute) is NOT escalated — the
// structural floor is authoritative there; the model is consulted only on the genuinely fuzzy band. Here a
// supporting judge is present but the floor already clearly supports, so the judge is never called.
func TestClearVerdictNotEscalated(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "Acme was founded by Ada Lovelace.", OK: true}}
	judge := &supportingJudge{verdict: "unsupported", ok: true}
	v := NewWebGrounded(fw, judge, nil)
	res := v.Verify(Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"})
	if res.Verdict != Supported {
		t.Fatalf("a clear floor support stands, got %v", res.Verdict)
	}
	if judge.called {
		t.Error("a clear floor verdict must NOT escalate to the model (structural floor authoritative)")
	}
}

// TestIndependentQueryFromQuestionAndAnswer: the re-retrieval query is formed from the QUESTION + the
// candidate ANSWER (a fresh query to the world), NOT a re-read of any reasoning. This is the structural
// independence — we assert the seam was asked with both the question and the answer claim.
func TestIndependentQueryFromQuestionAndAnswer(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "nothing relevant", OK: true}}
	v := NewWebGrounded(fw, nil, nil)
	v.Verify(Request{Question: "Who founded Acme Corp?", Answer: "Ada Lovelace"})
	if !strings.Contains(fw.lastQuery, "Acme Corp") {
		t.Errorf("the independent query must carry the question, got %q", fw.lastQuery)
	}
	if !strings.Contains(fw.lastQuery, "Ada Lovelace") {
		t.Errorf("the independent query must carry the candidate answer, got %q", fw.lastQuery)
	}
}

// TestDeterministic: the same request + same injected seam ⇒ the same verdict every call (the determinism
// contract the goldens depend on; no clock / RNG anywhere in the verifier).
func TestDeterministic(t *testing.T) {
	fw := &fakeWeb{def: web.Result{Text: "Acme was founded by Ada Lovelace.", OK: true}}
	v := NewWebGrounded(fw, nil, nil)
	req := Request{Question: "Who founded Acme?", Answer: "Ada Lovelace"}
	first := v.Verify(req).Verdict
	for i := 0; i < 8; i++ {
		if got := v.Verify(req).Verdict; got != first {
			t.Fatalf("verify not deterministic: call %d = %v, first = %v", i, got, first)
		}
	}
}
