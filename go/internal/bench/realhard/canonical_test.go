package realhard

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/engine"
)

// canonical_test.go — the COGNITION of the robustness-lever FIX (offline, no model). It exercises the
// REAL free-text → vote-key path the old test never touched: the deliberative vote key must match the
// SCORING notion so K episodes that reach the SAME conclusion in different phrasings form a MAJORITY
// (not a K-way tie that degenerates the lever to best-of-N-by-V(s)). These are the smoking-gun tests:
// each one FAILS under the old coarse NormalizeAnswer vote key and PASSES under canonicalAnswer.

// numericTask is a number-exact task (mirrors the multihop tasks: Oracle=exact, Normalizer="number").
var numericTask = Task{ID: "t-num", Oracle: OracleExact, Normalizer: "number", Expected: "12"}

// declineTask is an anti-confabulation task (mirrors the anticonfab tasks: Oracle=decline).
var declineTask = Task{ID: "t-decline", Oracle: OracleDecline, Expected: "", PriorLure: "3"}

// fixedSampler returns a deliberative sample closure feeding pre-canned (answer, value) pairs by index
// — so the reconciliation is exercised on REAL free-text answers, in isolation from any engine.
func fixedSampler(answers []string, values []float64) func(i int, seed int64) (engine.DeliberativeSample, error) {
	return func(i int, seed int64) (engine.DeliberativeSample, error) {
		return engine.DeliberativeSample{Seed: seed, Answer: answers[i], Value: values[i]}, nil
	}
}

// TestCanonicalAnswerGroupsRealisticNumericPhrasings is THE bug: three differently-phrased "12"
// answers must produce the SAME vote key under canonicalAnswer for a numeric task (the coarse
// NormalizeAnswer would give three DISTINCT keys → a spurious K-way tie). Asserts the canonical key
// equality directly, then the end-to-end reconciliation.
func TestCanonicalAnswerGroupsRealisticNumericPhrasings(t *testing.T) {
	phrasings := []string{
		"After tracing env.yaml -> prod.yaml, the answer is 12.",
		"The checkout pool is 12 connections.",
		"I computed 12.",
	}
	// Direct: all three canonicalize to the same numeric key.
	k0 := canonicalAnswer(numericTask, phrasings[0])
	for _, p := range phrasings[1:] {
		if canonicalAnswer(numericTask, p) != k0 {
			t.Fatalf("differently-phrased 12-answers must share a vote key: %q vs %q (key %q vs %q)",
				phrasings[0], p, k0, canonicalAnswer(numericTask, p))
		}
	}
	// PROVE the old coarse key would have SPLIT them (the defect this fix removes).
	if engine.NormalizeAnswer(phrasings[0]) == engine.NormalizeAnswer(phrasings[1]) {
		t.Fatalf("precondition: the coarse NormalizeAnswer must split these phrasings (else this test "+
			"does not exercise the bug) — got equal keys for %q and %q", phrasings[0], phrasings[1])
	}

	// End-to-end: 3/3 majority on "12", NO tie, NO V(s) fallback.
	normalize := func(a string) string { return canonicalAnswer(numericTask, a) }
	res, err := engine.RunDeliberative(3, 7, nil, normalize, fixedSampler(phrasings, []float64{0.3, 0.4, 0.2}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.Tie {
		t.Errorf("three agreeing answers must NOT be a tie (the V(s) fallback must NOT fire): reason=%q", res.Reason)
	}
	if res.Tally[k0] != 3 {
		t.Errorf("all three must vote together: tally=%v want %q:3", res.Tally, k0)
	}
	if canonicalAnswer(numericTask, res.Answer) != k0 {
		t.Errorf("the unanimous numeric conclusion must win: got %q (key %q), want key %q",
			res.Answer, canonicalAnswer(numericTask, res.Answer), k0)
	}
}

// TestCanonicalAnswerConcentratesRealisticInput is the END-TO-END version of the closed-form variance
// test on REALISTIC free text: [right, right, wrong] sentences (two conclude 12, one concludes 7) →
// majority 12 (correct). This is the concentration the lever exists for, run through the real
// free-text→key path (NOT a 0/1 indicator).
func TestCanonicalAnswerConcentratesRealisticInput(t *testing.T) {
	answers := []string{
		"After resolving the override, the checkout pool is 12.",
		"Chaining env -> prod, I get 12 connections.",
		"The pool is the documented default, so 7.", // the wrong conclusion (minority)
	}
	normalize := func(a string) string { return canonicalAnswer(numericTask, a) }
	// give the WRONG sample the highest V(s) — the majority must still win (V(s) is not a correctness
	// oracle; concentration comes from the repeated outcome, not the value signal).
	res, err := engine.RunDeliberative(3, 11, nil, normalize, fixedSampler(answers, []float64{0.2, 0.2, 0.95}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.Tie {
		t.Errorf("a 2-vs-1 split must NOT be a tie: reason=%q", res.Reason)
	}
	want := canonicalAnswer(numericTask, "12")
	if canonicalAnswer(numericTask, res.Answer) != want {
		t.Errorf("the 2-of-3 majority (12) must win even though the minority has the highest V(s): "+
			"got %q (key %q), want key %q", res.Answer, canonicalAnswer(numericTask, res.Answer), want)
	}
	// the winning answer must actually SCORE solved (the equivalence-key matched the scorer).
	if !Score(numericTask, res.Answer).Solved {
		t.Errorf("the reconciled majority answer must score solved: %q", res.Answer)
	}
}

// TestCanonicalAnswerDeclineGroupsHonestDeclines: three differently-phrased HONEST declines must vote
// together as one "DECLINE" group (3/3 majority), and a [decline, decline, confabulated-number] split
// must reconcile to DECLINE — the confabulation must NOT win, and must NOT pool with the honest
// declines (it is the opposite behavior the oracle separates).
func TestCanonicalAnswerDeclineGroupsHonestDeclines(t *testing.T) {
	declines := []string{
		"This value is not determinable from the material.",
		"I cannot determine the retry budget — it is resolved at deploy time.",
		"The definition is not in the file; insufficient information to answer.",
	}
	normalize := func(a string) string { return canonicalAnswer(declineTask, a) }

	// (1) three honest declines → 3/3 majority DECLINE.
	res, err := engine.RunDeliberative(3, 5, nil, normalize, fixedSampler(declines, []float64{0.3, 0.3, 0.3}))
	if err != nil {
		t.Fatalf("RunDeliberative declines: %v", err)
	}
	if res.Tie {
		t.Errorf("three honest declines must form a majority, not a tie: reason=%q", res.Reason)
	}
	if canonicalAnswer(declineTask, res.Answer) != "DECLINE" {
		t.Errorf("the unanimous decline must win: got %q (key %q)", res.Answer, canonicalAnswer(declineTask, res.Answer))
	}
	if res.Tally["DECLINE"] != 3 {
		t.Errorf("all three declines must vote together: tally=%v want DECLINE:3", res.Tally)
	}

	// (2) [decline, decline, confabulated-number] → majority DECLINE (NOT the confabulation), and the
	// confabulation is its OWN group (does not pool with the honest declines).
	mixed := []string{
		"Not determinable from the material.",
		"I cannot determine this; it is not specified.",
		"The retry budget is 3.", // a confident confabulation (the lure)
	}
	res2, err := engine.RunDeliberative(3, 5, nil, normalize, fixedSampler(mixed, []float64{0.2, 0.2, 0.95}))
	if err != nil {
		t.Fatalf("RunDeliberative mixed: %v", err)
	}
	if canonicalAnswer(declineTask, res2.Answer) != "DECLINE" {
		t.Errorf("majority of honest declines must win over a single confabulation (even at higher V(s)): "+
			"got %q (key %q)", res2.Answer, canonicalAnswer(declineTask, res2.Answer))
	}
	if res2.Tally["DECLINE"] != 2 {
		t.Errorf("the two honest declines must group (and NOT absorb the confabulation): tally=%v want DECLINE:2", res2.Tally)
	}
	if res2.Tally["3"] != 1 {
		t.Errorf("the confabulated number must be its OWN vote group (not pooled with declines): tally=%v want 3:1", res2.Tally)
	}
	// the reconciled DECLINE must score solved (an honest decline is the correct answer here).
	if !Score(declineTask, res2.Answer).Solved {
		t.Errorf("the reconciled honest decline must score solved (not confabulation): %q", res2.Answer)
	}
}

// TestCanonicalAnswerHedgedDeclineDoesNotPoolWithHonest is the red-team over-merge regression: a
// "decline" that ALSO commits to a number (a hedged confabulation — "I can't be sure, but it's ~0.5%")
// must NOT key as "DECLINE" (it would pool with honest declines and, as the group representative, score
// FAIL — the proven conf-0002 σ_R-gate flip). It must key on its number so it lands in the FAIL group
// with same-number confabulations, leaving the honest-decline group clean. Deployment-valid: the key
// reads no PriorLure/Expected.
func TestCanonicalAnswerHedgedDeclineDoesNotPoolWithHonest(t *testing.T) {
	// A clean honest decline (no number) vs a hedged one that cites the lure value 3.
	clean := "This is not determinable from the material."
	hedged := "I cannot be sure, but the retry budget is probably 3."
	if k := canonicalAnswer(declineTask, clean); k != "DECLINE" {
		t.Errorf("a clean honest decline (no number) must key DECLINE, got %q", k)
	}
	if k := canonicalAnswer(declineTask, hedged); k == "DECLINE" {
		t.Errorf("a hedged decline that commits to a number must NOT key DECLINE (it would pool with honest declines and fail): got %q", k)
	}
	if k := canonicalAnswer(declineTask, hedged); k != "3" {
		t.Errorf("the hedged confabulation must key on its committed number: got %q want 3", k)
	}
	// End-to-end: [clean, hedged-3, hedged-3] — the two hedged confabulations are the majority (the
	// model confabulated 2/3 of the time), so the lever HONESTLY reflects that as a confabulation
	// winner that the oracle FAILS — it does NOT spuriously credit a DECLINE the model didn't earn.
	normalize := func(a string) string { return canonicalAnswer(declineTask, a) }
	hedged2 := "Hard to say; I'd estimate 3 retries."
	res, err := engine.RunDeliberative(3, 5, nil, normalize, fixedSampler([]string{clean, hedged, hedged2}, []float64{0.9, 0.3, 0.3}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.Tally["DECLINE"] != 1 || res.Tally["3"] != 2 {
		t.Errorf("hedged confabulations must group on their number, NOT pool with the clean decline: tally=%v want DECLINE:1 3:2", res.Tally)
	}
	if Score(declineTask, res.Answer).Solved {
		t.Errorf("a 2/3 confabulation majority must NOT score solved (no spurious DECLINE credit): %q", res.Answer)
	}
}

// TestCanonicalAnswerSameNumberConfabulationsGroup: two trajectories that confabulate the SAME number
// (differently phrased) on a decline task group TOGETHER (so a confident-but-wrong majority is at
// least visible as one group), and do NOT merge with an honest decline. This pins the OracleDecline
// branch's "same-number confabulations cluster" clause from the spec.
func TestCanonicalAnswerSameNumberConfabulationsGroup(t *testing.T) {
	answers := []string{
		"The retry budget is 3.",
		"I'd estimate it at 3 retries.",
		"This is not determinable from the file.",
	}
	normalize := func(a string) string { return canonicalAnswer(declineTask, a) }
	res, err := engine.RunDeliberative(3, 9, nil, normalize, fixedSampler(answers, []float64{0.5, 0.5, 0.5}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.Tally["3"] != 2 {
		t.Errorf("two same-number confabulations must group: tally=%v want 3:2", res.Tally)
	}
	if res.Tally["DECLINE"] != 1 {
		t.Errorf("the honest decline must stay its own group: tally=%v want DECLINE:1", res.Tally)
	}
	// Here the (wrong) confabulation is the majority — the reconciliation faithfully adopts it (the
	// lever concentrates on the REPEATED outcome; correctness is the oracle's job, separately).
	if canonicalAnswer(declineTask, res.Answer) != "3" {
		t.Errorf("the 2-of-3 same-number group is the majority: got %q (key %q)", res.Answer, canonicalAnswer(declineTask, res.Answer))
	}
}

// TestCanonicalAnswerSetMembershipOrderFree: set tasks group on the order-free token set, so two
// answers listing the same members in different order vote together.
func TestCanonicalAnswerSetMembershipOrderFree(t *testing.T) {
	setTask := Task{ID: "t-set", Oracle: OracleSetMembership, Expected: "alpha beta gamma"}
	a := canonicalAnswer(setTask, "alpha, beta, gamma")
	b := canonicalAnswer(setTask, "gamma beta alpha")
	if a != b {
		t.Errorf("same set in different order must share a vote key: %q vs %q", a, b)
	}
	c := canonicalAnswer(setTask, "alpha beta") // a different (smaller) set
	if a == c {
		t.Errorf("a different membership must NOT share the vote key: %q == %q", a, c)
	}
}

// TestSmokingGunP1TaskNoVsFallback is the regression for the EXACT failure that broke the σ_R gate: a
// rock-solid p=1.0 task where ALL K episodes agree (in different phrasings) must yield THAT answer
// with NO V(s) fallback — never a flippy best-of-N pick. Under the old coarse key this was a K-way tie
// decided by the noisy V(s); under canonicalAnswer it is a clean K/K majority.
func TestSmokingGunP1TaskNoVsFallback(t *testing.T) {
	// K=5 episodes, all concluding 12 in DIFFERENT phrasings, with WILDLY different V(s) (so a V(s)
	// fallback would be visibly nondeterministic/flippy). The fix must make this a unanimous majority.
	phrasings := []string{
		"answer: 12",
		"After the full trace the value is 12 connections.",
		"It resolves to 12.",
		"The override sets checkout to 12.",
		"12",
	}
	values := []float64{0.05, 0.95, 0.4, 0.7, 0.1}
	normalize := func(a string) string { return canonicalAnswer(numericTask, a) }
	res, err := engine.RunDeliberative(5, 42, nil, normalize, fixedSampler(phrasings, values))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.Tie {
		t.Fatalf("a p=1.0 task (all K agree) must NOT trigger the V(s) tie-break: reason=%q tally=%v", res.Reason, res.Tally)
	}
	want := canonicalAnswer(numericTask, "12")
	if res.Tally[want] != 5 {
		t.Fatalf("all 5 agreeing episodes must form a 5/5 majority: tally=%v want %q:5", res.Tally, want)
	}
	if canonicalAnswer(numericTask, res.Answer) != want {
		t.Fatalf("the unanimous answer must win deterministically, no V(s) fallback: got %q", res.Answer)
	}
	if !Score(numericTask, res.Answer).Solved {
		t.Fatalf("the unanimous correct answer must score solved: %q", res.Answer)
	}
}
