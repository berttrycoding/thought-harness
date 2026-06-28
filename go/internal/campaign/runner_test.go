package campaign

// runner_test.go — the campaign orchestration loop, proven on fixtures (zero tokens): the right
// sequence runs, and the verdict drives the right commit/revert/stage action + ledger record.

import (
	"errors"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/funnel"
)

// --- fakes ---------------------------------------------------------------

type fakeGen struct{ cands []funnel.Candidate }

func (g fakeGen) Generate(n int) ([]funnel.Candidate, error) { return g.cands, nil }

type fakeFunnel struct {
	admitted  []funnel.Candidate
	tier1Pass bool
}

func (f fakeFunnel) Screen(c []funnel.Candidate) ([]funnel.Candidate, bool, error) {
	return f.admitted, f.tier1Pass, nil
}

type fakeBench struct{ base, batch ArmResult }

func (b fakeBench) Bench(stateDir string) (ArmResult, error) {
	if stateDir == "" {
		return b.base, nil
	}
	return b.batch, nil
}

type fakeStore struct{ committed, discarded, wrote bool }

func (s *fakeStore) WriteBatch(id string, a []funnel.Candidate) (string, error) {
	s.wrote = true
	return "/tmp/batch-" + id, nil
}
func (s *fakeStore) Commit(string) error  { s.committed = true; return nil }
func (s *fakeStore) Discard(string) error { s.discarded = true; return nil }

type fakeLedger struct {
	snapped   bool
	decisions []string
}

func (l *fakeLedger) Snapshot(string) error    { l.snapped = true; return nil }
func (l *fakeLedger) Record(d, _ string) error { l.decisions = append(l.decisions, d); return nil }

func cand(id string) funnel.Candidate { return funnel.Candidate{ID: id} }

// helper: build a runner with the given bench arms + tier1.
func newRunner(base, batch ArmResult, tier1 bool, store *fakeStore, led *fakeLedger) *Runner {
	cands := []funnel.Candidate{cand("a"), cand("b"), cand("c")}
	return &Runner{
		Gen:    fakeGen{cands},
		Funnel: fakeFunnel{admitted: cands, tier1Pass: tier1},
		Bench:  fakeBench{base, batch},
		Store:  store,
		Ledger: led,
		Rule:   DefaultKeepRule(),
	}
}

// A smarter batch is COMMITTED, snapshot taken first, ledger records KEEP.
func TestRunBatchKeepsSmarter(t *testing.T) {
	store, led := &fakeStore{}, &fakeLedger{}
	base := ArmResult{PerItem: bits(5, 30), Tokens: 30000}
	bp := bits(5, 30)
	for i := 5; i < 17; i++ {
		bp[i] = true
	}
	batch := ArmResult{PerItem: bp, Tokens: 30000}
	r := newRunner(base, batch, true, store, led)

	out, err := r.RunBatch("001", 10)
	if err != nil {
		t.Fatalf("RunBatch: %v", err)
	}
	if !led.snapped {
		t.Error("the baseline must be snapshotted before screening")
	}
	if out.Verdict.Decision != Keep {
		t.Fatalf("smarter batch should KEEP, got %s (%s)", out.Verdict.Decision, out.Verdict.Reason)
	}
	if !store.committed || store.discarded {
		t.Errorf("KEEP must commit (not discard): committed=%v discarded=%v", store.committed, store.discarded)
	}
	if out.Action != "committed" || len(led.decisions) != 1 || led.decisions[0] != "KEEP" {
		t.Errorf("ledger/action wrong: action=%q decisions=%v", out.Action, led.decisions)
	}
}

// A flat-but-cheaper batch is COMMITTED (the client-confirmed case).
func TestRunBatchKeepsCheaper(t *testing.T) {
	store, led := &fakeStore{}, &fakeLedger{}
	pass := bits(10, 20)
	r := newRunner(ArmResult{PerItem: pass, Tokens: 20000}, ArmResult{PerItem: pass, Tokens: 12000}, true, store, led)
	out, _ := r.RunBatch("002", 10)
	if out.Verdict.Decision != Keep || !store.committed {
		t.Fatalf("flat-but-cheaper should KEEP+commit, got %s committed=%v", out.Verdict.Decision, store.committed)
	}
}

// A filler batch (no gain) is REVERTED — discarded, never committed.
func TestRunBatchRevertsFiller(t *testing.T) {
	store, led := &fakeStore{}, &fakeLedger{}
	pass := bits(10, 20)
	r := newRunner(ArmResult{PerItem: pass, Tokens: 20000}, ArmResult{PerItem: pass, Tokens: 20000}, true, store, led)
	out, _ := r.RunBatch("003", 10)
	if out.Verdict.Decision != Revert {
		t.Fatalf("filler should REVERT, got %s", out.Verdict.Decision)
	}
	if store.committed || !store.discarded {
		t.Errorf("REVERT must discard (not commit): committed=%v discarded=%v", store.committed, store.discarded)
	}
	if led.decisions[0] != "REVERT" {
		t.Errorf("ledger should record REVERT, got %v", led.decisions)
	}
}

// A Tier-1 regression reverts BEFORE the capability even matters.
func TestRunBatchRevertsOnTier1(t *testing.T) {
	store, led := &fakeStore{}, &fakeLedger{}
	base := ArmResult{PerItem: bits(5, 30), Tokens: 30000}
	bp := bits(5, 30)
	for i := 5; i < 20; i++ {
		bp[i] = true // big "lift" — but Tier-1 regressed, so it must still revert
	}
	r := newRunner(base, ArmResult{PerItem: bp, Tokens: 30000}, false /* tier1 regressed */, store, led)
	out, _ := r.RunBatch("004", 10)
	if out.Verdict.Decision != Revert || !store.discarded {
		t.Fatalf("Tier-1 regression must REVERT, got %s discarded=%v", out.Verdict.Decision, store.discarded)
	}
}

// A margin batch is STAGED — neither committed nor discarded; the human decides.
func TestRunBatchStagesMargin(t *testing.T) {
	store, led := &fakeStore{}, &fakeLedger{}
	base := ArmResult{PerItem: bits(10, 40), Tokens: 40000}
	bp := bits(10, 40)
	bp[10], bp[11] = true, true
	r := newRunner(base, ArmResult{PerItem: bp, Tokens: 48000}, true, store, led) // flat tok/solved
	out, _ := r.RunBatch("005", 10)
	if out.Verdict.Decision != Margin {
		t.Fatalf("expected MARGIN, got %s (%s)", out.Verdict.Decision, out.Verdict.Reason)
	}
	if store.committed || store.discarded {
		t.Errorf("MARGIN must STAGE (not commit/discard): committed=%v discarded=%v", store.committed, store.discarded)
	}
	if out.Action != "staged (margin — human decides)" {
		t.Errorf("action = %q", out.Action)
	}
}

// An empty funnel reverts immediately — no bench, nothing to test.
func TestRunBatchEmptyFunnelReverts(t *testing.T) {
	store, led := &fakeStore{}, &fakeLedger{}
	r := &Runner{
		Gen:    fakeGen{[]funnel.Candidate{cand("a")}},
		Funnel: fakeFunnel{admitted: nil, tier1Pass: true}, // everything filtered out
		Bench:  fakeBench{},
		Store:  store,
		Ledger: led,
		Rule:   DefaultKeepRule(),
	}
	out, _ := r.RunBatch("006", 10)
	if out.Verdict.Decision != Revert || store.wrote {
		t.Fatalf("empty funnel should REVERT without staging, got %s wrote=%v", out.Verdict.Decision, store.wrote)
	}
}

// A generate error aborts the batch — but the baseline was already snapshotted (live state safe).
func TestRunBatchGenerateErrorAborts(t *testing.T) {
	led := &fakeLedger{}
	r := &Runner{Gen: errGen{}, Ledger: led, Store: &fakeStore{}, Rule: DefaultKeepRule()}
	if _, err := r.RunBatch("007", 10); err == nil {
		t.Fatal("a generate error must abort the batch")
	}
	if !led.snapped {
		t.Error("the baseline snapshot must precede generation (so an abort leaves a revert point)")
	}
}

type errGen struct{}

func (errGen) Generate(int) ([]funnel.Candidate, error) { return nil, errors.New("boom") }
