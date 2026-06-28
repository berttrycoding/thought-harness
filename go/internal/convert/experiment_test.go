package convert

import "testing"

// The keep-or-revert primitive (slice h). These tests pin the repo's lineage contract:
// propose -> measure -> keep iff STRICTLY BETTER than the best so far (strict >, best-relative).
// They are written test-first against the Experiment / Decision API below.

// TestExperimentKeepsStrictlyBetter: a candidate above the best is KEPT and becomes the new best.
func TestExperimentKeepsStrictlyBetter(t *testing.T) {
	e := NewExperiment(0.2) // best-so-far starts at the floor 0.2

	if got := e.Best(); got != 0.2 {
		t.Fatalf("initial best = %v, want 0.2", got)
	}
	d := e.Propose(0.5)
	if d != Keep {
		t.Fatalf("0.5 vs best 0.2: got %v, want Keep", d)
	}
	if got := e.Best(); got != 0.5 {
		t.Fatalf("best after keep = %v, want 0.5 (best updates on keep)", got)
	}
	if e.Kept() != 1 || e.Reverted() != 0 {
		t.Fatalf("counts after one keep: kept=%d reverted=%d, want 1/0", e.Kept(), e.Reverted())
	}
}

// TestExperimentRevertsWorse: a candidate below the best is REVERTED and best is unchanged.
func TestExperimentRevertsWorse(t *testing.T) {
	e := NewExperiment(0.5)

	d := e.Propose(0.3)
	if d != Revert {
		t.Fatalf("0.3 vs best 0.5: got %v, want Revert", d)
	}
	if got := e.Best(); got != 0.5 {
		t.Fatalf("best after revert = %v, want 0.5 (unchanged)", got)
	}
	if e.Kept() != 0 || e.Reverted() != 1 {
		t.Fatalf("counts after one revert: kept=%d reverted=%d, want 0/1", e.Kept(), e.Reverted())
	}
}

// TestExperimentTieReverts is the load-bearing strict-> contract: an EQUAL score is NOT better, so it
// reverts. This is exactly the convert.go / W1-ledger keep-or-revert rule (strict >, never >=) — a tie
// must not churn the kept value.
func TestExperimentTieReverts(t *testing.T) {
	e := NewExperiment(0.4)

	d := e.Propose(0.4) // equal, not strictly greater
	if d != Revert {
		t.Fatalf("0.4 vs best 0.4 (tie): got %v, want Revert (strict > only)", d)
	}
	if got := e.Best(); got != 0.4 {
		t.Fatalf("best after tie = %v, want 0.4 (unchanged)", got)
	}
	if e.Reverted() != 1 {
		t.Fatalf("a tie must count as a revert; reverted=%d, want 1", e.Reverted())
	}
}

// TestExperimentRatchetsBestUpwards: across a sequence, best only ever moves up, and each decision is
// taken relative to the CURRENT best (best-relative, not relative to the immediately-previous score).
func TestExperimentRatchetsBestUpwards(t *testing.T) {
	e := NewExperiment(0.0)

	// 0.3 keep -> 0.7 keep -> 0.5 revert (worse than 0.7, though better than the floor) -> 0.9 keep.
	want := []struct {
		cand float64
		dec  Decision
		best float64
	}{
		{0.3, Keep, 0.3},
		{0.7, Keep, 0.7},
		{0.5, Revert, 0.7}, // best-relative: 0.5 < current best 0.7 -> revert, best holds
		{0.9, Keep, 0.9},
	}
	for i, w := range want {
		if d := e.Propose(w.cand); d != w.dec {
			t.Fatalf("step %d propose(%v): got %v, want %v", i, w.cand, d, w.dec)
		}
		if got := e.Best(); got != w.best {
			t.Fatalf("step %d best = %v, want %v", i, got, w.best)
		}
	}
	if e.Kept() != 3 || e.Reverted() != 1 {
		t.Fatalf("final counts: kept=%d reverted=%d, want 3/1", e.Kept(), e.Reverted())
	}
}

// TestExperimentHistoryRecordsEveryProposal: the audit trail keeps one row per Propose, in order, with
// the candidate score and the decision taken — the append-only run record the lineage requires.
func TestExperimentHistoryRecordsEveryProposal(t *testing.T) {
	e := NewExperiment(0.0)
	e.Propose(0.5) // keep
	e.Propose(0.2) // revert
	e.Propose(0.9) // keep

	h := e.History()
	if len(h) != 3 {
		t.Fatalf("history length = %d, want 3 (one row per proposal)", len(h))
	}
	wantCand := []float64{0.5, 0.2, 0.9}
	wantDec := []Decision{Keep, Revert, Keep}
	wantBest := []float64{0.5, 0.5, 0.9} // best-so-far AFTER the decision
	for i := range h {
		if h[i].Candidate != wantCand[i] {
			t.Errorf("history[%d].Candidate = %v, want %v", i, h[i].Candidate, wantCand[i])
		}
		if h[i].Decision != wantDec[i] {
			t.Errorf("history[%d].Decision = %v, want %v", i, h[i].Decision, wantDec[i])
		}
		if h[i].Best != wantBest[i] {
			t.Errorf("history[%d].Best = %v, want %v (best-so-far after the decision)", i, h[i].Best, wantBest[i])
		}
	}
	// History returns a copy: mutating it must not corrupt the experiment's record.
	h[0].Best = -999
	if e.History()[0].Best != 0.5 {
		t.Fatal("History() must return a defensive copy")
	}
}

// TestDecisionString gives the two decisions stable, non-empty labels (for events/trace).
func TestDecisionString(t *testing.T) {
	if Keep.String() != "keep" {
		t.Errorf("Keep.String() = %q, want \"keep\"", Keep.String())
	}
	if Revert.String() != "revert" {
		t.Errorf("Revert.String() = %q, want \"revert\"", Revert.String())
	}
}

// TestRunExperiment is the tiny composite: run a slice of candidate scores through one Experiment and
// get back the kept set (in proposal order) plus the final best — a tested keep-or-revert loop, the
// shape both the minting gate and the §5.3 theta bandit drive.
func TestRunExperiment(t *testing.T) {
	// floor 0.1; proposals climb then dip then exceed.
	kept, best := RunExperiment(0.1, []float64{0.3, 0.25, 0.8, 0.6, 0.95})

	// kept = strictly-improving prefix-maxima: 0.3 (>0.1), 0.8 (>0.3), 0.95 (>0.8).
	wantKept := []float64{0.3, 0.8, 0.95}
	if len(kept) != len(wantKept) {
		t.Fatalf("kept = %v, want %v", kept, wantKept)
	}
	for i := range wantKept {
		if kept[i] != wantKept[i] {
			t.Fatalf("kept[%d] = %v, want %v (kept=%v)", i, kept[i], wantKept[i], kept)
		}
	}
	if best != 0.95 {
		t.Fatalf("final best = %v, want 0.95", best)
	}
}

// TestRunExperimentNoneKept: when nothing beats the floor, the kept set is empty and best is the floor.
func TestRunExperimentNoneKept(t *testing.T) {
	kept, best := RunExperiment(0.9, []float64{0.1, 0.5, 0.9}) // 0.9 ties, never strictly exceeds
	if len(kept) != 0 {
		t.Fatalf("kept = %v, want empty (nothing beats the floor)", kept)
	}
	if best != 0.9 {
		t.Fatalf("best = %v, want 0.9 (floor held)", best)
	}
}

// TestRunExperimentEmpty: zero proposals -> empty kept set, best = the starting floor.
func TestRunExperimentEmpty(t *testing.T) {
	kept, best := RunExperiment(0.42, nil)
	if len(kept) != 0 {
		t.Fatalf("kept = %v, want empty", kept)
	}
	if best != 0.42 {
		t.Fatalf("best = %v, want 0.42", best)
	}
}
