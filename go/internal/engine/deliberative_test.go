package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// deliberative_test.go — the COGNITION of the robustness lever (offline, no model): the flag parse,
// the deterministic per-sample seed derivation, and the reconciliation faculty itself — majority vote
// concentrates the outcome, V(s) breaks a tie through the existing rank machinery, the same samples
// give the same winner (determinism), and the conscious.deliberation event carries the WHY. These are
// the THINKING the lever does, not just that a loop runs.

// TestParseDeliberativeKRobust pins the robust parse: a positive int >= 2 engages; everything
// degenerate (empty / whitespace / non-int / negative / zero) clamps to 1 (OFF — byte-identical).
func TestParseDeliberativeKRobust(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 1},    // unset → OFF
		{"   ", 1}, // whitespace → OFF
		{"1", 1},   // explicit single episode → OFF
		{"2", 2},   // engages
		{"7", 7},   // engages
		{" 3 ", 3}, // surrounding space tolerated
		{"0", 1},   // zero → OFF (no degenerate K)
		{"-4", 1},  // negative → OFF
		{"abc", 1}, // non-integer → OFF
		{"3.5", 1}, // float → OFF (Atoi rejects)
		{"2x", 1},  // trailing junk → OFF
		{"64", 64}, // exactly the ceiling → as-is
		{"65", 64}, // above the ceiling → clamped DOWN to the cap (cost guardrail)
		{"10000", 64},
	}
	for _, c := range cases {
		if got := ParseDeliberativeK(c.in); got != c.want {
			t.Errorf("ParseDeliberativeK(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestDeliberativeSeedDeterministicAndDistinct: sample 0 keeps the base seed VERBATIM (the
// byte-identical anchor), every other sample gets a DISTINCT seed, and the derivation is a pure
// function of (base, i) — same inputs, same seed, always.
func TestDeliberativeSeedDeterministicAndDistinct(t *testing.T) {
	base := int64(1729)
	if DeliberativeSeed(base, 0) != base {
		t.Errorf("sample 0 must keep the base seed %d verbatim, got %d", base, DeliberativeSeed(base, 0))
	}
	seen := map[int64]int{}
	for i := 0; i < 16; i++ {
		s := DeliberativeSeed(base, i)
		if prev, dup := seen[s]; dup {
			t.Errorf("sample %d seed %d collides with sample %d", i, s, prev)
		}
		seen[s] = i
		// purity: the same call gives the same value.
		if DeliberativeSeed(base, i) != s {
			t.Errorf("DeliberativeSeed(%d,%d) is not a pure function (two calls differ)", base, i)
		}
	}
	// a different base produces a different family (no cross-base collision at i>0).
	if DeliberativeSeed(base, 3) == DeliberativeSeed(base+1, 3) {
		t.Errorf("different bases must give different per-sample seeds at i=3")
	}
}

// fixedSampler builds a sample closure that returns pre-canned (answer, value) pairs by sample index
// — so the reconciliation can be tested in isolation from any engine/episode (PURE reconciliation).
func fixedSampler(answers []string, values []float64) func(i int, seed int64) (DeliberativeSample, error) {
	return func(i int, seed int64) (DeliberativeSample, error) {
		return DeliberativeSample{Seed: seed, Answer: answers[i], Value: values[i]}, nil
	}
}

// TestReconcileMajorityVote: K=3 with a clear majority adopts the majority answer, not the minority —
// even when the minority sample has a higher V(s). The whole point of self-consistency is that the
// REPEATED outcome wins; V(s) is only the tie-break, never an override of a real majority.
func TestReconcileMajorityVote(t *testing.T) {
	// two trajectories say "42", one says "99" (the higher-V(s) outlier). Majority "42" must win.
	answers := []string{"42", "42", "99"}
	values := []float64{0.30, 0.30, 0.95} // the outlier has the highest V(s)
	res, err := RunDeliberative(3, 7, nil, NormalizeAnswer, fixedSampler(answers, values))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.K != 3 {
		t.Errorf("K = %d, want 3", res.K)
	}
	if NormalizeAnswer(res.Answer) != "42" {
		t.Errorf("majority must win: got %q, want 42 (a higher-V(s) minority must NOT override the majority)", res.Answer)
	}
	if res.Tally["42"] != 2 || res.Tally["99"] != 1 {
		t.Errorf("tally wrong: %v (want 42:2 99:1)", res.Tally)
	}
	if res.Tie {
		t.Errorf("a 2-vs-1 split is not a tie")
	}
}

// TestReconcileNormalizesVoteKey: trajectories that reach the SAME conclusion in different
// case/spacing vote TOGETHER — the vote key is the normalized answer, so "The Answer Is 42" and
// "the answer is 42" are one group (a real majority hidden behind phrasing is not split).
func TestReconcileNormalizesVoteKey(t *testing.T) {
	answers := []string{"The Answer  Is 42", "the answer is 42", "99"}
	values := []float64{0.5, 0.5, 0.9}
	res, err := RunDeliberative(3, 7, nil, NormalizeAnswer, fixedSampler(answers, values))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	key := NormalizeAnswer("the answer is 42")
	if res.Tally[key] != 2 {
		t.Errorf("case/space-variant answers must vote together: tally=%v want %q:2", res.Tally, key)
	}
	if NormalizeAnswer(res.Answer) != key {
		t.Errorf("the normalized majority must win, got %q", res.Answer)
	}
}

// TestReconcileTieBrokenByValue: K=2 with two DIFFERENT answers (a 1-1 tie). The tie-break must adopt
// the higher-V(s) answer — and it must go through the existing rank machinery (control.Rank over the
// group's summed V(s) as Relevance), NOT a coin flip. Higher V(s) → that answer wins.
func TestReconcileTieBrokenByValue(t *testing.T) {
	answers := []string{"alpha", "beta"}
	values := []float64{0.20, 0.80} // beta has the higher V(s)
	res, err := RunDeliberative(2, 7, nil, NormalizeAnswer, fixedSampler(answers, values))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if !res.Tie {
		t.Errorf("a 1-1 split must be flagged as a tie")
	}
	if NormalizeAnswer(res.Answer) != "beta" {
		t.Errorf("the higher-V(s) answer must win the tie via the rank machinery: got %q, want beta", res.Answer)
	}

	// flip the V(s): now alpha has the higher value → alpha must win (the tie-break tracks V(s)).
	res2, err := RunDeliberative(2, 7, nil, NormalizeAnswer, fixedSampler([]string{"alpha", "beta"}, []float64{0.80, 0.20}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if NormalizeAnswer(res2.Answer) != "alpha" {
		t.Errorf("tie-break must follow V(s): with alpha higher, got %q want alpha", res2.Answer)
	}
}

// TestReconcileDeterministic: the SAME samples give the SAME winner, every time (the reconciliation is
// a pure reduction). Run it twice and assert identical answer/winner/tally — the byte-stable property
// the gate relies on.
func TestReconcileDeterministic(t *testing.T) {
	answers := []string{"x", "y", "x", "y", "z"}
	values := []float64{0.4, 0.6, 0.4, 0.6, 0.9}
	r1, _ := RunDeliberative(5, 7, nil, NormalizeAnswer, fixedSampler(answers, values))
	r2, _ := RunDeliberative(5, 7, nil, NormalizeAnswer, fixedSampler(answers, values))
	if r1.Answer != r2.Answer || r1.WinnerIx != r2.WinnerIx || r1.Tie != r2.Tie {
		t.Errorf("reconciliation must be deterministic: run1=(%q,%d,%v) run2=(%q,%d,%v)",
			r1.Answer, r1.WinnerIx, r1.Tie, r2.Answer, r2.WinnerIx, r2.Tie)
	}
	if r1.Tally["x"] != 2 || r1.Tally["y"] != 2 || r1.Tally["z"] != 1 {
		t.Errorf("tally: %v (want x:2 y:2 z:1)", r1.Tally)
	}
}

// TestDeliberationEventEmitted: K>1 emits exactly ONE conscious.deliberation event carrying the WHY
// (k, tally, winner, tie) — the observability contract (a faculty with no event is invisible). K==1
// emits NONE (byte-identical: the wrapper does not engage).
func TestDeliberationEventEmitted(t *testing.T) {
	bus := events.NewDefault()
	var delibEvents []events.Event
	bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.Deliberation {
			delibEvents = append(delibEvents, ev)
		}
	})

	// K=3 → exactly one deliberation event.
	_, err := RunDeliberative(3, 7, bus.Emit, NormalizeAnswer, fixedSampler([]string{"a", "a", "b"}, []float64{0.5, 0.5, 0.9}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if len(delibEvents) != 1 {
		t.Fatalf("K=3 must emit exactly 1 conscious.deliberation event, got %d", len(delibEvents))
	}
	ev := delibEvents[0]
	if ev.Data["k"].(int) != 3 {
		t.Errorf("event k = %v, want 3", ev.Data["k"])
	}
	if ev.Data["tie"].(bool) {
		t.Errorf("a 2-1 split is not a tie in the event payload")
	}
	if NormalizeAnswer(ev.Data["winner"].(string)) != "a" {
		t.Errorf("event winner = %v, want a", ev.Data["winner"])
	}
	// the layer must be conscious.* (the namespacing contract).
	if ev.Layer != "conscious" {
		t.Errorf("deliberation event layer = %q, want conscious", ev.Layer)
	}

	// K==1 → NO deliberation event (byte-identical: the wrapper does not engage).
	delibEvents = nil
	_, err = RunDeliberative(1, 7, bus.Emit, NormalizeAnswer, fixedSampler([]string{"solo"}, []float64{0.5}))
	if err != nil {
		t.Fatalf("RunDeliberative K=1: %v", err)
	}
	if len(delibEvents) != 0 {
		t.Errorf("K=1 must emit NO deliberation event (byte-identical), got %d", len(delibEvents))
	}
}

// TestVarianceReductionConcentratesOutcome is the COGNITION-PROPERTY test for the lever's whole point:
// on a controlled NOISY per-sample distribution (a Bernoulli outcome that is correct with p>0.5),
// majority-vote over K=3 samples is correct on STRICTLY MORE of the noisy draw-sets than a single
// sample — i.e. self-consistency CONCENTRATES the outcome and reduces the per-task solve variance.
//
// This is proven exhaustively over the 2^3 draw outcomes (no RNG, fully deterministic): with the
// per-sample correct-probability p, P(single correct) = p, and P(majority-of-3 correct) =
// p^3 + 3p^2(1-p). For any p in (0.5,1) the majority probability is strictly greater — so the
// majority's outcome distribution is tighter around "correct" (lower variance of the 0/1 solve
// indicator). The test enumerates every 3-draw pattern, reconciles it through the REAL RunDeliberative
// majority path, and asserts the count of correct-majority patterns (weighted by p) exceeds the
// single-sample expectation.
func TestVarianceReductionConcentratesOutcome(t *testing.T) {
	const correct = "RIGHT"
	const wrong = "WRONG"
	// Enumerate all 2^3 = 8 patterns of (correct?) per sample. For each, reconcile via the REAL
	// majority machinery and record whether the reconciled answer is the correct one.
	majorityCorrect := 0
	singleCorrect := 0 // # patterns where sample 0 alone is correct (the no-deliberation baseline)
	patterns := 0
	for a := 0; a < 2; a++ {
		for b := 0; b < 2; b++ {
			for c := 0; c < 2; c++ {
				patterns++
				bits := []int{a, b, c}
				answers := make([]string, 3)
				values := make([]float64, 3)
				for i, bit := range bits {
					if bit == 1 {
						answers[i] = correct
					} else {
						answers[i] = wrong
					}
					values[i] = 0.5 // equal V(s): the OUTCOME (the majority), not value, must decide
				}
				res, err := RunDeliberative(3, 7, nil, NormalizeAnswer, fixedSampler(answers, values))
				if err != nil {
					t.Fatalf("RunDeliberative: %v", err)
				}
				if res.Answer == correct {
					majorityCorrect++
				}
				if answers[0] == correct {
					singleCorrect++
				}
			}
		}
	}
	if patterns != 8 {
		t.Fatalf("enumerated %d patterns, want 8", patterns)
	}
	// Of the 8 equally-weighted patterns, 4 have sample-0 correct (single baseline) and 4 have a
	// 2-or-3 correct majority (majority baseline) — equal at p=0.5. The CONCENTRATION shows when the
	// noisy draws are WEIGHTED by a per-sample p>0.5: then the majority-correct mass exceeds the
	// single-correct mass. Compute both expectations under p=0.7 and assert majority > single.
	p := 0.7
	// re-walk the patterns weighting each by p^(#correct) * (1-p)^(#wrong).
	var eMajority, eSingle float64
	for a := 0; a < 2; a++ {
		for b := 0; b < 2; b++ {
			for c := 0; c < 2; c++ {
				bits := []int{a, b, c}
				answers := make([]string, 3)
				ncorrect := 0
				for i, bit := range bits {
					if bit == 1 {
						answers[i] = correct
						ncorrect++
					} else {
						answers[i] = wrong
					}
				}
				w := 1.0
				for i := 0; i < 3; i++ {
					if bits[i] == 1 {
						w *= p
					} else {
						w *= (1 - p)
					}
				}
				res, _ := RunDeliberative(3, 7, nil, NormalizeAnswer, fixedSampler(answers, []float64{0.5, 0.5, 0.5}))
				if res.Answer == correct {
					eMajority += w
				}
				if answers[0] == correct {
					eSingle += w
				}
			}
		}
	}
	// closed-form sanity: eSingle == p; eMajority == p^3 + 3p^2(1-p).
	wantSingle := p
	wantMajority := p*p*p + 3*p*p*(1-p)
	if !approxEng(eSingle, wantSingle) {
		t.Errorf("E[single correct] = %g, want p=%g", eSingle, wantSingle)
	}
	if !approxEng(eMajority, wantMajority) {
		t.Errorf("E[majority correct] = %g, want p^3+3p^2(1-p)=%g", eMajority, wantMajority)
	}
	if eMajority <= eSingle {
		t.Errorf("self-consistency must CONCENTRATE the outcome: E[majority]=%g must exceed E[single]=%g "+
			"(at p=%g) — this is the variance-reduction mechanism the lever exists for", eMajority, eSingle, p)
	}
	// The variance of the 0/1 solve indicator is q(1-q); a higher success-prob q (closer to 1) means
	// LOWER variance. Assert the majority's solve variance is strictly below the single's.
	varSingle := eSingle * (1 - eSingle)
	varMajority := eMajority * (1 - eMajority)
	if varMajority >= varSingle {
		t.Errorf("majority solve-variance (%g) must be BELOW single (%g) — outcome concentration reduces σ",
			varMajority, varSingle)
	}
}

// TestNumericAwareNormalizeAnswerGroupsPhrasings pins the engine's DEFAULT (nil) vote key: it is
// numeric-aware, so differently-phrased numeric conclusions key on the LAST number's canonical form
// and vote together (the non-bench caller benefit). A no-number answer falls back to the coarse key.
// This is the engine-side mirror of the realhard canonicalAnswer fix.
func TestNumericAwareNormalizeAnswerGroupsPhrasings(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"After tracing, the answer is 12.", "The pool is 12 connections.", true},
		{"I computed 12", "12", true},
		{"the value is 12.0", "12", true},      // 12.0 and 12 canonicalize identically
		{"result: 1,200", "I get 1200.", true}, // thousands-comma normalized
		{"the answer is 12", "the answer is 7", false},
		{"no number here", "another phrase", false}, // no number → coarse fallback, distinct phrasings
		{"same phrase", "same phrase", true},        // no number → coarse fallback, identical
	}
	for _, c := range cases {
		ka := NumericAwareNormalizeAnswer(c.a)
		kb := NumericAwareNormalizeAnswer(c.b)
		if (ka == kb) != c.same {
			t.Errorf("NumericAwareNormalizeAnswer(%q)=%q vs (%q)=%q: same=%v want %v",
				c.a, ka, c.b, kb, ka == kb, c.same)
		}
	}
}

// TestDefaultNormalizerConcentratesRealisticInput is the variance-reduction assertion run THROUGH the
// real free-text → vote-key path with the DEFAULT (nil → numeric-aware) normalizer: [right, right,
// wrong] realistic sentences (two conclude 12, one concludes 7) reconcile to 12 with NO V(s) fallback
// — the end-to-end concentration the closed-form TestVarianceReductionConcentratesOutcome models but
// never actually exercised on free text (the proven defect). Passing nil exercises the shipped default.
func TestDefaultNormalizerConcentratesRealisticInput(t *testing.T) {
	answers := []string{
		"After resolving the override, the checkout pool is 12.",
		"Chaining env -> prod, I get 12 connections.",
		"The pool is the documented default, so 7.", // the wrong minority
	}
	// the wrong sample carries the HIGHEST V(s): the majority must still win (concentration is on the
	// repeated outcome, not the value signal) — and there must be no tie/fallback at all.
	res, err := RunDeliberative(3, 7, nil, nil, fixedSampler(answers, []float64{0.2, 0.2, 0.95}))
	if err != nil {
		t.Fatalf("RunDeliberative: %v", err)
	}
	if res.Tie {
		t.Errorf("a 2-vs-1 majority on free text must NOT be a tie under the default key: reason=%q tally=%v", res.Reason, res.Tally)
	}
	if NumericAwareNormalizeAnswer(res.Answer) != "12" {
		t.Errorf("the 2-of-3 majority (12) must win even with the minority at the highest V(s): got %q (key %q)",
			res.Answer, NumericAwareNormalizeAnswer(res.Answer))
	}
	// the SAME free text under the OLD coarse key would have been a 3-way tie (each sentence a distinct
	// key) → a V(s) fallback to the wrong (highest-V(s)) sample. Prove that explicitly: the three
	// coarse keys are all distinct.
	c0, c1, c2 := NormalizeAnswer(answers[0]), NormalizeAnswer(answers[1]), NormalizeAnswer(answers[2])
	if c0 == c1 || c1 == c2 || c0 == c2 {
		t.Fatalf("precondition: the OLD coarse key must split these sentences into distinct groups "+
			"(else the regression is not demonstrated): %q / %q / %q", c0, c1, c2)
	}
}

const engEps = 1e-9

func approxEng(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= engEps
}
