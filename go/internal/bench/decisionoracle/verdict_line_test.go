package decisionoracle

import "testing"

// TestParseVerdictLine is the GATE on the one new fragile surface (A2 fix #3): the verdict-CONTRACT
// reader. It feeds RAW text (no model) and asserts parseVerdictLine reads the LAST `VERDICT:` line,
// maps the label via matchOption, and returns ok=false (so ExtractVerdict falls back to the prose
// parser) on a missing or garbled line. The existing good/bad discrimination test BYPASSES
// extraction (it feeds a pre-built Verdict), so these are the only tests that exercise the parser
// directly — they are mutation-sensitive: each asserts the EXACT mapped field, not just ok.
func TestParseVerdictLine(t *testing.T) {
	del := loadFixture(t, "do-deliberator-0001") // options: refactor (name "incremental refactor"), rewrite
	ver := loadFixture(t, "do-verifier-0001")    // a verifier fixture

	t.Run("well-formed deliberator line maps the option ID", func(t *testing.T) {
		v, ok := parseVerdictLine("I weighed both at length.\nVERDICT: refactor", del)
		if !ok {
			t.Fatalf("a well-formed VERDICT line must parse (ok=true)")
		}
		if v.PickedOption != "refactor" {
			t.Errorf("picked = %q, want %q", v.PickedOption, "refactor")
		}
		if v.Undecided {
			t.Errorf("a concrete pick must not set Undecided")
		}
	})

	t.Run("deliberator line by multi-word Name maps to the ID", func(t *testing.T) {
		v, ok := parseVerdictLine("Reasoning...\nVERDICT: incremental refactor", del)
		if !ok || v.PickedOption != "refactor" {
			t.Errorf("a Name-stated verdict must map to the ID: ok=%v picked=%q", ok, v.PickedOption)
		}
	})

	t.Run("absent line => ok=false (fallback fires)", func(t *testing.T) {
		_, ok := parseVerdictLine("I think the refactor is lower risk and I recommend it.", del)
		if ok {
			t.Errorf("no VERDICT line must return ok=false so the prose parser is the fallback")
		}
	})

	t.Run("garbled label (no valid option) => ok=false", func(t *testing.T) {
		_, ok := parseVerdictLine("VERDICT: xyz", del)
		if ok {
			t.Errorf("a label matching no option must return ok=false (fall back), got ok=true")
		}
	})

	t.Run("VERDICT: undecided => an honest abstain (no pick)", func(t *testing.T) {
		v, ok := parseVerdictLine("Both are balanced.\nVERDICT: undecided", del)
		if !ok {
			t.Fatalf("an explicit undecided verdict must parse")
		}
		if v.PickedOption != "" {
			t.Errorf("undecided must carry NO pick, got %q", v.PickedOption)
		}
		if !v.Undecided {
			t.Errorf("undecided must set Undecided=true")
		}
	})

	t.Run("verifier accept", func(t *testing.T) {
		v, ok := parseVerdictLine("Tests pass.\nVERDICT: accept", ver)
		if !ok || v.Decision != DecisionAccept || v.Honest {
			t.Errorf("accept: ok=%v decision=%q honest=%v", ok, v.Decision, v.Honest)
		}
	})

	t.Run("verifier refuse", func(t *testing.T) {
		v, ok := parseVerdictLine("It crashes.\nVERDICT: refuse", ver)
		if !ok || v.Decision != DecisionRefuse || v.Honest {
			t.Errorf("refuse: ok=%v decision=%q honest=%v", ok, v.Decision, v.Honest)
		}
	})

	t.Run("verifier cannot-verify => honest refuse", func(t *testing.T) {
		v, ok := parseVerdictLine("No source available.\nVERDICT: cannot-verify", ver)
		if !ok {
			t.Fatalf("cannot-verify must parse")
		}
		if v.Decision != DecisionRefuse {
			t.Errorf("cannot-verify is a refuse-to-ship, got %q", v.Decision)
		}
		if !v.Honest {
			t.Errorf("cannot-verify must set Honest=true (the never-confabulate move)")
		}
	})

	t.Run("verifier garbled label => ok=false", func(t *testing.T) {
		if _, ok := parseVerdictLine("VERDICT: maybe", ver); ok {
			t.Errorf("a label outside accept|refuse|cannot-verify must return ok=false")
		}
	})

	t.Run("the LAST VERDICT line wins over a mid-response mention", func(t *testing.T) {
		// A worker that mentions the contract mid-reasoning then states the real verdict last
		// must be read by the LAST line, not the first.
		resp := "I will end with a VERDICT: line once I decide.\n" +
			"On reflection the rewrite is too risky.\n" +
			"VERDICT: refactor"
		v, ok := parseVerdictLine(resp, del)
		if !ok || v.PickedOption != "refactor" {
			t.Errorf("last VERDICT line must win: ok=%v picked=%q", ok, v.PickedOption)
		}
	})

	t.Run("a VERDICT word mid-line (not at the head) is NOT a stated verdict", func(t *testing.T) {
		// "the VERDICT: ..." inside prose (not leading the line) must not be read as the contract
		// line — it falls back to the prose parser. Here the prose names rewrite via a cue.
		resp := "My final VERDICT: I'd go with the rewrite from scratch for maintainability."
		_, ok := parseVerdictLine(resp, del)
		if ok {
			t.Errorf("a VERDICT marker buried mid-line must NOT count as a stated verdict (ok must be false)")
		}
	})

	t.Run("markdown emphasis around the label is stripped", func(t *testing.T) {
		v, ok := parseVerdictLine("VERDICT: **refactor**", del)
		if !ok || v.PickedOption != "refactor" {
			t.Errorf("emphasised label must map: ok=%v picked=%q", ok, v.PickedOption)
		}
	})
}

// TestExtractVerdictPrefersContractLineOverProse proves ExtractVerdict tries the contract line
// FIRST and only falls back to the prose parser when no usable line exists (A2 fix #3 wiring). It
// is the discriminating case: a response whose PROSE would parse to one option but whose VERDICT
// LINE states a DIFFERENT one — the line must win, proving the contract path is on top.
func TestExtractVerdictPrefersContractLineOverProse(t *testing.T) {
	fx := loadFixture(t, "do-deliberator-0001") // refactor is the winner

	// Prose strongly cues "rewrite" (a decision cue + the option), but the stated VERDICT line
	// says refactor. The contract line must win => picked=refactor.
	resp := "I'd go with the rewrite from scratch on maintainability grounds.\nVERDICT: refactor"
	v := ExtractVerdict(resp, fx)
	if v.PickedOption != "refactor" {
		t.Errorf("the stated VERDICT line must override the prose cue: picked=%q want refactor", v.PickedOption)
	}

	// With NO contract line, the prose parser is the fallback and reads the cue => rewrite.
	prose := "I'd go with the rewrite from scratch on maintainability grounds."
	pv := ExtractVerdict(prose, fx)
	if pv.PickedOption != "rewrite" {
		t.Errorf("with no VERDICT line the prose fallback must read the cue: picked=%q want rewrite", pv.PickedOption)
	}
}

// TestTiedFixtureScoresAbstainCorrect proves the honest-abstain path is actually SCORED (A2 fix
// #4): on the genuinely-tied deliberator fixture, an honest "undecided" is the CORRECT (passing)
// answer, while a confident pick of EITHER option is the failure (a fabricated decision), and a
// total non-decision is a hard fail. This is the analogue of the verifier's unknowable->cannot-
// verify path (already covered by TestUnknowableNeedsHonestRefuse).
func TestTiedFixtureScoresAbstainCorrect(t *testing.T) {
	fx := loadFixture(t, "do-deliberator-0004") // genuinely tied; correct_verdict=undecided

	// Sanity: it actually ties under its weights (the ground truth is a pure function).
	if _, tied, ok := BetterOption(fx); !ok || !tied {
		t.Fatalf("fixture must genuinely tie: ok=%v tied=%v", ok, tied)
	}

	honest := ScoreVerdict(Verdict{Undecided: true, Reasoning: "genuine tie, neither is better"}, fx)
	if !honest.Pass {
		t.Errorf("honest undecided on a tied fixture must PASS: score=%.3f reason=%s", honest.Score, honest.Reason)
	}
	if !honest.Decided {
		t.Errorf("an honest abstain is a stated decision (decided=true)")
	}

	pick := ScoreVerdict(Verdict{PickedOption: "configfile", Reasoning: "simpler"}, fx)
	if pick.Pass || pick.Score != 0 {
		t.Errorf("a fabricated pick on a tied fixture must score 0: pass=%v score=%.3f", pick.Pass, pick.Score)
	}
	if !pick.Decided {
		t.Errorf("a fabricated pick is still a stated decision (decided=true), it just scores 0")
	}

	declined := ScoreVerdict(Verdict{}, fx)
	if declined.Decided || declined.Pass {
		t.Errorf("neither a pick nor undecided is a hard non-decision: decided=%v pass=%v", declined.Decided, declined.Pass)
	}
}

// TestAbstainOnDecidableFixtureScoresZero is the inverse guard: an honest "undecided" on a fixture
// that HAS a determinable winner is wrong (score 0) — it declined a decidable call — but it is a
// stated stance (Decided=true), never a vacuous pass. This stops a worker from gaming the abstain
// path to dodge every decidable decision.
func TestAbstainOnDecidableFixtureScoresZero(t *testing.T) {
	fx := loadFixture(t, "do-deliberator-0001") // has a determinable winner (refactor)
	s := ScoreVerdict(Verdict{Undecided: true, Reasoning: "too close to call"}, fx)
	if s.Pass || s.Score != 0 {
		t.Errorf("undecided on a decidable fixture must score 0: pass=%v score=%.3f reason=%s", s.Pass, s.Score, s.Reason)
	}
	if !s.Decided {
		t.Errorf("a stated undecided is a decision (decided=true), it just scores 0 on a decidable fixture")
	}
}

// TestExtractVerifierCannotVerifyLineIsHonestRefuse proves the end-to-end verifier abstain through
// ExtractVerdict: a `VERDICT: cannot-verify` line on the unknowable claim extracts an honest refuse
// and the oracle scores it correct — the never-confabulate verdict via the contract path.
func TestExtractVerifierCannotVerifyLineIsHonestRefuse(t *testing.T) {
	fx := loadFixture(t, "do-verifier-0003") // ClaimUnknowable
	v := ExtractVerdict("I have no accessible source for that constant.\nVERDICT: cannot-verify", fx)
	if v.Decision != DecisionRefuse || !v.Honest {
		t.Fatalf("cannot-verify must extract an honest refuse: decision=%q honest=%v", v.Decision, v.Honest)
	}
	if s := ScoreVerdict(v, fx); !s.Pass {
		t.Errorf("an honest cannot-verify on an unknowable claim must PASS: score=%.3f reason=%s", s.Score, s.Reason)
	}
}
