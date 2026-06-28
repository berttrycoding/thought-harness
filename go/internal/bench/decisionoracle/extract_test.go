package decisionoracle

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

// TestExtractThenScoreEndToEnd is the COGNITION-PROPERTY check on the full pipe: realistic
// free-text worker prose -> ExtractVerdict -> ScoreVerdict must produce the RIGHT verdict
// score. It pins the THINKING the oracle measures (a sound trade-off answer / a correct
// ship verdict / a confabulated answer), not just the plumbing — a worker that reasons
// well scores a pass, one that ships a bug or confabulates scores a fail.
func TestExtractThenScoreEndToEnd(t *testing.T) {
	fixtures := loadBank(t)
	byID := map[string]Fixture{}
	for _, fx := range fixtures {
		byID[fx.ID] = fx
	}

	cases := []struct {
		name     string
		fixture  string
		response string
		wantPass bool
	}{
		{
			name:     "deliberator sound pick passes",
			fixture:  "do-deliberator-0001",
			response: "Both have merits, but the incremental refactor carries much lower risk of regression, so I recommend the refactor.",
			wantPass: true,
		},
		{
			name:     "deliberator wrong pick fails",
			fixture:  "do-deliberator-0001",
			response: "A clean rewrite is more maintainable, so I'd go with the rewrite from scratch.",
			wantPass: false,
		},
		{
			name:     "deliberator mutex right pick passes",
			fixture:  "do-deliberator-0002",
			response: "Lay out the options: a channel is faster but a mutex is simpler for guarding one counter. We should use the mutex for its simplicity.",
			wantPass: true,
		},
		{
			name:     "verifier accept-true passes",
			fixture:  "do-verifier-0001",
			response: "I ran the checks: the full test suite passes and behaviour is unchanged, so this is safe to ship.",
			wantPass: true,
		},
		{
			name:     "verifier accept-false (ship a bug) fails",
			fixture:  "do-verifier-0002",
			response: "The code looks reasonable and compiles, so it should run correctly. Safe to ship.",
			wantPass: false,
		},
		{
			name:     "verifier refuse-false passes",
			fixture:  "do-verifier-0002",
			response: "The final iteration reads arr[i+1] which is out of range; it will crash at runtime. Do not ship.",
			wantPass: true,
		},
		{
			name:     "verifier honest refuse on unknowable passes",
			fixture:  "do-verifier-0003",
			response: "I cannot verify the value of that private constant — there is no accessible source, so I cannot confirm it is 42.",
			wantPass: true,
		},
		{
			name:     "verifier confident accept of unknowable fails",
			fixture:  "do-verifier-0003",
			response: "Yes, INTERNAL_SEED_OFFSET is 42, confirmed.",
			wantPass: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fx, ok := byID[tc.fixture]
			if !ok {
				t.Fatalf("fixture %q not in bank", tc.fixture)
			}
			v := ExtractVerdict(tc.response, fx)
			s := ScoreVerdict(v, fx)
			if s.Pass != tc.wantPass {
				t.Errorf("pass=%v want=%v\n  extracted: %+v\n  reason: %s", s.Pass, tc.wantPass, v, s.Reason)
			}
		})
	}
}

// TestExtractDeliberatorRankedListPick is the A2-hardening-#1 regression: a Deliberator
// that states its pick as a RANKED LIST ("Ranked choice: X first" / a numbered "1. X"
// conclusion) rather than a verbal cue ("I recommend X") must extract the #1-ranked option
// and SCORE it — the verbal-cue extractor under-credited these as "no option picked (hard
// fail)" (the false-negative the claude baseline + oracle-doctor flagged). These are the
// exact phrasings the live claude Deliberator workers produced (runs/decision-quality-
// claude-auto.jsonl): a numbered #1 with the option NAME ("Incremental refactoring"), an
// explicit "Ranked choice: Relational SQL database first", and the "1. Mutex (preferred)".
func TestExtractDeliberatorRankedListPick(t *testing.T) {
	fixtures := loadBank(t)
	byID := map[string]Fixture{}
	for _, fx := range fixtures {
		byID[fx.ID] = fx
	}
	cases := []struct {
		name     string
		fixture  string
		response string
		wantPick string // the option ID the ranked #1 names
		wantPass bool
	}{
		{
			// do-deliberator-0001 winner=refactor: numbered #1 names the NAME
			// ("Incremental refactoring") which the multi-word name match must resolve,
			// and a top decomposition list ALSO opens with "1." — the conclusion ranking
			// (the LAST "1." line) is the pick, not the first sub-question.
			name:    "numbered #1 with multi-word name after a decompose list",
			fixture: "do-deliberator-0001",
			response: "I break this into sub-questions:\n" +
				"1. What is the current state of the legacy parser?\n" +
				"2. What is the risk tolerance?\n\n" +
				"Both a full rewrite and an incremental refactor share the same goal; they differ on risk.\n" +
				"I rank the two options from most to least preferred:\n\n" +
				"1. **Incremental refactoring** — delivers continuous, shippable improvements with no big-bang cutover risk.\n" +
				"2. **Full rewrite from scratch** — a clean slate but high risk.",
			wantPick: "refactor",
			wantPass: true,
		},
		{
			// do-deliberator-0003 winner=sql: explicit inline "Ranked choice: ... first".
			name:    "explicit ranked-choice cue",
			fixture: "do-deliberator-0003",
			response: "I rank the options for a ledger store (highest to lowest):\n\n" +
				"1. **Relational SQL database** — ledgers demand ACID transactions and strong consistency.\n" +
				"2. **NoSQL store** — scales but sacrifices transactional guarantees.\n\n" +
				"**Ranked choice: Relational SQL database first.**",
			wantPick: "sql",
			wantPass: true,
		},
		{
			// do-deliberator-0002 winner=mutex: a "(preferred)" tag on the #1 item.
			name:    "preferred-tagged #1 ranked item",
			fixture: "do-deliberator-0002",
			response: "I rank the two options by appropriateness:\n\n" +
				"1. **Mutex** *(preferred)* — directly protects shared memory with minimal overhead.\n" +
				"2. **Channel** *(secondary)* — adds indirection unnecessary here.",
			wantPick: "mutex",
			wantPass: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fx := byID[tc.fixture]
			v := ExtractVerdict(tc.response, fx)
			if v.PickedOption != tc.wantPick {
				t.Fatalf("ranked-list pick = %q, want %q", v.PickedOption, tc.wantPick)
			}
			s := ScoreVerdict(v, fx)
			if s.Pass != tc.wantPass {
				t.Errorf("pass=%v want=%v (score=%.3f) reason=%s", s.Pass, tc.wantPass, s.Score, s.Reason)
			}
		})
	}
}

// TestExtractDeliberatorBaselineLedgerExtracts replays the ACTUAL claude baseline ledger
// (runs/decision-quality-claude-auto.jsonl, the run that flagged this false-negative) through
// the extractor+oracle and asserts every Deliberator row now extracts a non-empty pick and
// scores correctly against ground truth. It is guarded to SKIP when the ledger file is absent
// (it is an uncommitted run artifact) so the suite stays portable — when present it pins the
// fix against the verbatim worker prose, not a paraphrase. Before the fix, do-deliberator-0001
// and -0003 extracted no pick (a hard fail); after it they extract refactor / sql and PASS.
func TestExtractDeliberatorBaselineLedgerExtracts(t *testing.T) {
	const ledger = "../../../runs/decision-quality-claude-auto.jsonl"
	f, err := os.Open(ledger)
	if err != nil {
		t.Skipf("baseline ledger %s not present (uncommitted run artifact) — skipping verbatim replay", ledger)
	}
	defer f.Close()

	byID := map[string]Fixture{}
	for _, fx := range loadBank(t) {
		byID[fx.ID] = fx
	}

	type row struct {
		ItemID    string `json:"item_id"`
		RawOutput string `json:"raw_output"`
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	seen := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r row
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		fx, ok := byID[r.ItemID]
		if !ok || fx.Worker != WorkerDeliberator {
			continue
		}
		seen++
		v := ExtractVerdict(r.RawOutput, fx)
		s := ScoreVerdict(v, fx)
		winner, _, _ := BetterOption(fx)
		// A GENUINE-TIE fixture (correct_verdict=undecided) has NO determinable winner: the
		// honest verdict is `undecided`, so a baseline row that FABRICATED a confident pick
		// correctly scores 0 (that is the tie-fabrication calibration gap, not an extractor
		// false-negative). The extractor must still READ whatever stance the prose stated
		// (a pick OR an honest abstain) — but it must NOT be asserted to PASS, since a pick
		// there is the wrong answer. Skip the PASS assertion for these; the determinable-
		// winner rows below carry the "extracted-and-scored-right" guarantee.
		if fx.CorrectVerdict == "undecided" {
			if v.PickedOption == "" && !v.Undecided {
				t.Errorf("[%s] genuine-tie row extracted NEITHER a pick nor an undecided abstain — a non-decision the extractor should not silently drop", r.ItemID)
			}
			continue
		}
		if v.PickedOption == "" {
			t.Errorf("[%s] verbatim worker output extracted NO pick (the false-negative this fix targets); winner=%s", r.ItemID, winner)
			continue
		}
		// Every baseline determinable-winner Deliberator row picked the ground-truth winner
		// (claude is saturated on this axis); the prior bug only mis-READ the pick. So each
		// should now PASS.
		if !s.Pass {
			t.Errorf("[%s] pick=%s winner=%s should PASS after the fix: score=%.3f reason=%s",
				r.ItemID, v.PickedOption, winner, s.Score, s.Reason)
		}
	}
	if seen == 0 {
		t.Fatalf("ledger %s present but held no Deliberator rows — stale fixture/bank mismatch", ledger)
	}
}

// TestExtractRankedWrongPickStillScoresZero is the FALSE-POSITIVE guard for the ranked-list
// extractor: a worker that ranks the WRONG option #1 must extract THAT (wrong) pick and the
// oracle must then SCORE it 0 — the extractor reads the ranking honestly, it does not snap to
// the ground-truth winner. (This is the soundness half of the fix: crediting genuine ranked
// picks must NOT loosen the oracle into passing a wrong pick.)
func TestExtractRankedWrongPickStillScoresZero(t *testing.T) {
	fx := loadFixture(t, "do-deliberator-0001") // winner = refactor
	resp := "On reflection I rank them, most preferred first:\n\n" +
		"1. **Rewrite from scratch** — the cleanest long-term architecture wins.\n" +
		"2. **Incremental refactoring** — too slow.\n\n" +
		"**Ranked choice: rewrite from scratch first.**"
	v := ExtractVerdict(resp, fx)
	if v.PickedOption != "rewrite" {
		t.Fatalf("ranked WRONG pick should extract %q, got %q", "rewrite", v.PickedOption)
	}
	s := ScoreVerdict(v, fx)
	if s.Pass || s.Score != 0 {
		t.Errorf("a ranked pick of the WRONG option must score 0 (not loosened to a pass): pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
	}
	if !s.Decided {
		t.Errorf("a wrong ranked pick is still a DECISION (decided=true), it just scores 0; got decided=false")
	}
}

// TestExtractRankedNoOptionIsNonDecision proves a ranked-list conclusion whose #1 item names
// NO option (e.g. "it depends on context") still yields no pick — the ranked-list path never
// invents a decision the surface does not state.
func TestExtractRankedNoOptionIsNonDecision(t *testing.T) {
	fx := loadFixture(t, "do-deliberator-0001")
	resp := "Ranking, most preferred first:\n\n" +
		"1. It depends entirely on the team's context and risk appetite.\n" +
		"2. Either path can work with discipline."
	v := ExtractVerdict(resp, fx)
	if v.PickedOption != "" {
		t.Errorf("a ranking that names no option should extract no pick, got %q", v.PickedOption)
	}
	if s := ScoreVerdict(v, fx); s.Decided {
		t.Errorf("no option named on #1 should be a hard non-decision, got decided=%v", s.Decided)
	}
}

// TestExtractRankedDirectionBlindFalsePassesClosed pins the THREE proven false-passes the
// bench-oracle-doctor re-vet of 135d170 caught — the unsound max-overlap (POSITION-blind +
// decision-blind) extractor credited a wrong-picker / non-decider as a correct pick. The
// fix makes pickFromRanking POSITION-aware (the option ranked FIRST, not the longest-named)
// and DECISION-aware (a decomposition step / hedged conclusion is not a pick). Each case
// FAILED before the fix and must hold after it.
func TestExtractRankedDirectionBlindFalsePassesClosed(t *testing.T) {
	t.Run("co-mention loser-ranked-first extracts the LOSER (direction preserved)", func(t *testing.T) {
		// do-deliberator-0003 winner=sql. The worker ranked NoSQL #1 (WRONG). The old
		// max-overlap extractor scored "relational SQL" (2 tokens) over "NoSQL" (1 token)
		// and returned sql -> a false PASS, identical whether the loser or the winner was
		// named first (direction-blind, THE serious one). The fix takes the FIRST-named
		// option after the cue -> nosql -> a DECISION that the oracle scores 0 (wrong pick).
		fx := loadFixture(t, "do-deliberator-0003")
		v := ExtractVerdict("Ranked choice: the NoSQL store first, relational SQL second.", fx)
		if v.PickedOption != "nosql" {
			t.Fatalf("loser-ranked-first must extract the LOSER %q (position-aware), got %q", "nosql", v.PickedOption)
		}
		s := ScoreVerdict(v, fx)
		if s.Pass || s.Score != 0 {
			t.Errorf("a wrong (loser-first) pick must score 0, not a false PASS: pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
		}
		if !s.Decided {
			t.Errorf("a wrong pick is still a DECISION (decided=true); got decided=false")
		}
	})

	t.Run("decompose-list non-decider yields NO pick", func(t *testing.T) {
		// do-deliberator-0001 winner=refactor. A worker that is still DECOMPOSING the
		// problem ("1. Assess ...") and has NOT decided ("I need more data") must not be
		// read as a ranking #1. The old extractor matched the "1." line via name-overlap
		// ("incremental refactor") and returned refactor, decided=true -> a false PASS.
		fx := loadFixture(t, "do-deliberator-0001")
		resp := "Before I can choose, I need to investigate:\n" +
			"1. Assess the incremental refactor's blast radius on the parser.\n" +
			"2. Estimate the rewrite's test-coverage gap.\n\n" +
			"I need more data before I can commit to either path."
		v := ExtractVerdict(resp, fx)
		if v.PickedOption != "" {
			t.Fatalf("a decomposition step (not a ranking) must extract NO pick, got %q", v.PickedOption)
		}
		if s := ScoreVerdict(v, fx); s.Decided {
			t.Errorf("a non-decider must be a hard non-decision (decided=false), got decided=true")
		}
	})

	t.Run("hedge 'too close to call' yields NO pick", func(t *testing.T) {
		// do-deliberator-0001. An EXPLICIT non-decision conclusion. The old extractor read
		// the inline "Ranked:" cue, scored "rewrite from scratch" (3 tokens) over
		// "incremental refactor" (2 tokens), returned rewrite -> a false PASS. The fix
		// suppresses any pick when the rank span hedges.
		fx := loadFixture(t, "do-deliberator-0001")
		v := ExtractVerdict("Ranked: incremental refactor vs rewrite from scratch — too close to call.", fx)
		if v.PickedOption != "" {
			t.Fatalf("an explicit hedge must extract NO pick, got %q", v.PickedOption)
		}
		if s := ScoreVerdict(v, fx); s.Decided {
			t.Errorf("an explicit hedge must be a hard non-decision (decided=false), got decided=true")
		}
	})
}

// TestExtractRankedLegitimatePicksStillPass confirms the SOUND true-positives the fix must
// PRESERVE: a single-option numbered #1 conclusion, an inline "Ranked choice: <winner>
// first", and a "(preferred)" tag each still extract the CORRECT winner and PASS. (The
// fix tightens against false-positives; it must not regress the genuine ranked picks the
// A2-hardening added.)
func TestExtractRankedLegitimatePicksStillPass(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		response string
		wantPick string
	}{
		{
			// Both options named as whole tokens (so extraction falls through to the
			// ranking path), the #1 line names the winner, the #2 line the loser.
			name:    "numbered #1 conclusion ranks the winner first",
			fixture: "do-deliberator-0001",
			response: "Weighing the incremental refactor against the rewrite, I rank them:\n" +
				"1. **incremental refactor** — lower regression risk, the deciding factor.\n" +
				"2. **rewrite from scratch** — cleaner but a risky big-bang cutover.",
			wantPick: "refactor",
		},
		{
			name:     "inline ranked-choice winner-first",
			fixture:  "do-deliberator-0003",
			response: "Ranked choice: Relational SQL database first; NoSQL second.",
			wantPick: "sql",
		},
		{
			name:     "preferred-tagged #1 item",
			fixture:  "do-deliberator-0002",
			response: "1. **Mutex** *(preferred)* — simplest construct here.\n2. **Channel** *(secondary)*.",
			wantPick: "mutex",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fx := loadFixture(t, tc.fixture)
			v := ExtractVerdict(tc.response, fx)
			if v.PickedOption != tc.wantPick {
				t.Fatalf("legitimate ranked pick = %q, want %q", v.PickedOption, tc.wantPick)
			}
			if s := ScoreVerdict(v, fx); !s.Pass {
				t.Errorf("a correct ranked winner must PASS: pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
			}
		})
	}
}

// TestExtractRankedSecondRevetFalsePassesClosed pins the THREE additional false-passes
// the SECOND bench-oracle-doctor re-vet found — the deterministic prose parser was still
// fragile in three places the first re-vet's fix did not reach. Each probe FAILED before
// the D1-D3 fix and must hold after it. These are committed regression tests so a future
// edit cannot silently reopen them.
func TestExtractRankedSecondRevetFalsePassesClosed(t *testing.T) {
	byID := map[string]Fixture{}
	for _, fx := range loadBank(t) {
		byID[fx.ID] = fx
	}

	// D1 (DOMINANT): a multi-word Name was invisible to the single-token `mentioned` gate
	// (containsToken normalised the whole Name to one spaced string and looked for it as a
	// SINGLE token, which a multi-word Name can never be). A worker ranking the WINNER #1 by
	// its multi-word Name ("Incremental refactoring") — with NO bare "refactor" token anywhere
	// else — was seen to mention only the OTHER option, len(mentioned)==1 short-circuited to
	// the loser, and it scored a false FAIL. The fix detects a multi-word Name as a CONTIGUOUS
	// token subsequence (stem-tolerant, so "refactoring" matches the Name word "refactor").
	t.Run("D1 gerund-only multi-word winner ranking extracts the WINNER and PASSES", func(t *testing.T) {
		fx := byID["do-deliberator-0001"] // winner = refactor
		// "Incremental refactoring" (gerund) is the ONLY occurrence of the winner — there is
		// NO bare "refactor" token elsewhere (the body says "this work", not "the refactor").
		resp := "Both approaches share the goal; they differ on the risk of this work.\n" +
			"I rank them, most preferred first:\n\n" +
			"1. **Incremental refactoring** — delivers continuous, shippable improvements with no big-bang cutover.\n" +
			"2. **Rewrite from scratch** — a clean slate but a risky one."
		// Guard the probe's premise: there is genuinely no bare "refactor" token to match on.
		if containsToken(resp, "refactor") {
			t.Fatalf("probe invalid: response contains a bare 'refactor' token, so D1 would pass incidentally")
		}
		v := ExtractVerdict(resp, fx)
		if v.PickedOption != "refactor" {
			t.Fatalf("D1: a multi-word-Name winner ranking must extract the WINNER %q, got %q", "refactor", v.PickedOption)
		}
		if s := ScoreVerdict(v, fx); !s.Pass || s.Score != 1.0 {
			t.Errorf("D1: the winner ranked #1 must PASS at score 1.0: pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
		}
	})

	// D2: position anchoring was hijacked by a stray occurrence of a multi-word Name's FIRST
	// word. "relational SQL" was anchored on the earliest "relational" token, so an aside
	// ("from a relational mindset") put sql ahead of the genuinely-#1-ranked NoSQL — a false
	// PASS on a worker that ranked the LOSER first. The fix anchors a multi-word Name on its
	// FULL CONTIGUOUS occurrence, so a stray shared adjective can no longer steal position.
	t.Run("D2 stray first-word aside no longer steals position from the #1-ranked loser", func(t *testing.T) {
		fx := byID["do-deliberator-0003"] // winner = sql; worker ranks the LOSER nosql first.
		resp := "Ranked choice: from a relational mindset, NoSQL first, relational SQL second."
		v := ExtractVerdict(resp, fx)
		if v.PickedOption != "nosql" {
			t.Fatalf("D2: a loser-first ranking with a stray 'relational' aside must extract the LOSER %q, got %q", "nosql", v.PickedOption)
		}
		s := ScoreVerdict(v, fx)
		if s.Pass || s.Score != 0 {
			t.Errorf("D2: a wrong (loser-first) pick must score 0, not a false PASS: pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
		}
		if !s.Decided {
			t.Errorf("D2: a wrong pick is still a DECISION (decided=true); got decided=false")
		}
	})

	// D3: an article-led decompose step ("1. The refactor's blast radius must be assessed")
	// puts an option at the line head AND has a distinct "2." sibling, so head+sibling ALONE
	// was read as a ranking #1 — fabricating a decision out of a pure to-investigate list. The
	// fix requires a POSITIVE ranking signal on the #1 line (a rank cue / "(preferred)" tag /
	// a verdict-shaped em-dash-or-colon conclusion). A pure decompose list with no conclusion
	// ranking and no hedge must yield NO pick.
	t.Run("D3 article-led decompose steps with no conclusion yield NO pick", func(t *testing.T) {
		fx := byID["do-deliberator-0001"]
		resp := "Before I can decide I must scope the work:\n" +
			"1. The refactor's blast radius must be assessed across the legacy parser.\n" +
			"2. The rewrite's coverage gap must be estimated against the suite."
		v := ExtractVerdict(resp, fx)
		if v.PickedOption != "" {
			t.Fatalf("D3: an article-led decompose list (no conclusion ranking) must extract NO pick, got %q", v.PickedOption)
		}
		if s := ScoreVerdict(v, fx); s.Decided {
			t.Errorf("D3: a pure decompose list must be a hard non-decision (decided=false), got decided=true")
		}
	})
}

// TestExtractDeliberatorAmbiguousIsNonDecision proves an answer that names both options
// without a clear pick yields NO pick — a non-decision (a hard fail), not a guess.
func TestExtractDeliberatorAmbiguousIsNonDecision(t *testing.T) {
	fx := loadFixture(t, "do-deliberator-0001")
	v := ExtractVerdict("There are merits to both the refactor and the rewrite; it depends on the team.", fx)
	if v.PickedOption != "" {
		t.Errorf("an undecided answer should extract no pick, got %q", v.PickedOption)
	}
	if s := ScoreVerdict(v, fx); s.Decided {
		t.Errorf("no pick should be a hard non-decision, got decided=%v", s.Decided)
	}
}

// TestExtractVerifierNoMarkerIsNonDecision proves a verifier response with neither an
// accept nor a refuse marker yields no decision (a hard fail), not a default.
func TestExtractVerifierNoMarkerIsNonDecision(t *testing.T) {
	fx := loadFixture(t, "do-verifier-0001")
	v := ExtractVerdict("This is an interesting refactor with several considerations.", fx)
	if v.Decision != "" {
		t.Errorf("a marker-free response should extract no decision, got %q", v.Decision)
	}
}

func loadFixture(t *testing.T, id string) Fixture {
	t.Helper()
	for _, fx := range loadBank(t) {
		if fx.ID == id {
			return fx
		}
	}
	t.Fatalf("fixture %q not in bank", id)
	return Fixture{}
}
