package engine

// selfbench_test.go — H-SB2 loop-close: a recorded batch of self-changes is GATED on a measured SelfBench
// fitness delta + a durability re-gate, with keep-or-revert against the pre-mint baseline. These are
// COGNITION-property tests (the thinking the spec intends), not plumbing: a measured WIN that holds
// durability PROMOTES; a net-negative batch is REVERTED; the default propose-and-gate MEASURES + PROPOSES
// but never self-commits a revert, while closed-loop holds its own keep/revert key (ResetToSnapshot).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// selfBenchEngine builds a heuristic persist engine with the SelfBench loop-close ON and (optionally)
// closed-loop autonomy ON, plus an event recorder. The ledger + auto-baseline default ON (so the
// pre-mint revert point exists).
func selfBenchEngine(t *testing.T, dir string, closedLoop bool) (*Engine, *[]events.Event) {
	t.Helper()
	e := newPersistEngine(t, dir)
	e.features.Ledger.SelfBenchGate = true
	e.features.Ledger.SelfBenchClosedLoop = closedLoop
	// take the once-per-session pre-mint baseline snapshot NOW (empty store), so a revert has a target.
	e.ensureBaselineSnapshot()
	var got []events.Event
	e.Bus().Subscribe(func(ev events.Event) { got = append(got, ev) })
	return e, &got
}

// seedMintBatch injects a batch of NEW minted specialists this session at a chosen grounded value — the
// SelfBench fitness reads their mean grounded value, so the value chooses whether the batch is a win or a
// loss. It mirrors a freq×value-admitted batch (the cheap pre-filter has already passed).
func seedMintBatch(e *Engine, value float64, domains ...string) {
	recs := make([]convert.SpecialistRecord, 0, len(domains))
	for _, d := range domains {
		recs = append(recs, convert.SpecialistRecord{
			Domain: d, GoalKey: d, Triggers: []string{d}, Answer: "worked answer for " + d,
			Relevance: 0.9, Generated: 3, Value: value,
		})
	}
	e.convert.SeedPrimitiveSubAgents(recs)
}

func kindsSeen(evs []events.Event, kind string) []events.Event {
	var out []events.Event
	for _, ev := range evs {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// A batch whose measured fitness clears the mint-value floor (by more than the noise floor) AND holds
// durability is PROMOTED — the harness measures its own self-change and judges it a real improvement.
func TestSelfBenchPromotesAMeasuredWin(t *testing.T) {
	dir := t.TempDir()
	e, got := selfBenchEngine(t, dir, false)

	// floor = MintValue (0.2 default); a 0.9-value batch lands +0.7 over the floor — a clean win.
	seedMintBatch(e, 0.9, "learned:winA", "learned:winB")
	e.FlushState() // consolidation -> recordLedger -> selfBenchGate

	verdicts := kindsSeen(*got, events.SelfBenchVerdict)
	if len(verdicts) != 1 {
		t.Fatalf("want exactly one selfbench.verdict for the recorded batch, got %d", len(verdicts))
	}
	v := verdicts[0]
	if v.Data["verdict"] != "promote" {
		t.Fatalf("a +0.7 measured win must PROMOTE, got verdict=%v (delta=%v, durable=%v, regime=%v)",
			v.Data["verdict"], v.Data["delta"], v.Data["durable"], v.Data["regime"])
	}
	if v.Data["durable"] != true {
		t.Fatalf("a settling reactive batch must hold durability, got durable=%v regime=%v", v.Data["durable"], v.Data["regime"])
	}
	if d, _ := v.Data["delta"].(float64); d <= selfBenchNoiseFloor {
		t.Fatalf("the measured delta must clear the noise floor, got %v", d)
	}
	if len(kindsSeen(*got, events.SelfBenchPromote)) != 1 {
		t.Fatalf("a promote verdict must emit selfbench.promote exactly once")
	}
	if len(kindsSeen(*got, events.SelfBenchRevert)) != 0 {
		t.Fatalf("a win must not emit selfbench.revert")
	}
}

// A batch whose measured fitness falls BELOW the floor is a net-negative — REVERTED. Under the DEFAULT
// propose-and-gate the harness MEASURES + PROPOSES the revert but does NOT self-commit it (committed=false):
// the live state is untouched, an explicit gate / human turns the key.
func TestSelfBenchProposesRevertButDoesNotSelfCommit(t *testing.T) {
	dir := t.TempDir()
	e, got := selfBenchEngine(t, dir, false) // propose-and-gate

	before := e.mintCount()
	seedMintBatch(e, 0.05, "learned:lossA", "learned:lossB") // 0.05 << floor 0.2 -> Δ = -0.15
	e.FlushState()

	verdicts := kindsSeen(*got, events.SelfBenchVerdict)
	if len(verdicts) != 1 || verdicts[0].Data["verdict"] != "revert" {
		t.Fatalf("a below-floor batch must REVERT; verdicts=%+v", verdicts)
	}
	reverts := kindsSeen(*got, events.SelfBenchRevert)
	if len(reverts) != 1 {
		t.Fatalf("a revert verdict must emit selfbench.revert once, got %d", len(reverts))
	}
	if reverts[0].Data["committed"] != false {
		t.Fatalf("propose-and-gate must NOT self-commit the revert, got committed=%v", reverts[0].Data["committed"])
	}
	// the live state is UNTOUCHED — the batch's mints are still present (the harness only proposed).
	if got := e.mintCount(); got <= before {
		t.Fatalf("propose-and-gate must leave the live mints in place (before=%d, after=%d)", before, got)
	}
}

// Under CLOSED-LOOP the harness holds its own keep-or-revert key: a net-negative batch is ACTUALLY reverted
// via ResetToSnapshot(auto:baseline). The unit of revert is the DURABLE CHECKPOINT (§7.2): the persisted
// state is rolled back to the clean pre-mint baseline (no specialists), so a restart boots WITHOUT the
// rejected batch. This is the genuine self-improving regime the design gates behind the flag.
func TestSelfBenchClosedLoopSelfRevertsANetNegative(t *testing.T) {
	dir := t.TempDir()
	e, got := selfBenchEngine(t, dir, true) // closed-loop autonomy

	seedMintBatch(e, 0.04, "learned:badA", "learned:badB", "learned:badC")
	if len(e.convert.Minted) == 0 {
		t.Fatalf("precondition: the seeded batch must register minted specialists")
	}
	e.FlushState()

	reverts := kindsSeen(*got, events.SelfBenchRevert)
	if len(reverts) != 1 {
		t.Fatalf("closed-loop must emit selfbench.revert for the net-negative batch, got %d", len(reverts))
	}
	if reverts[0].Data["committed"] != true {
		t.Fatalf("closed-loop must SELF-COMMIT the revert (ResetToSnapshot), got committed=%v", reverts[0].Data["committed"])
	}
	// a registry.reset event proves the snapshot store actually reverted to the baseline.
	if len(kindsSeen(*got, events.RegistryReset)) == 0 {
		t.Fatalf("a committed revert must reset the store to the baseline snapshot (registry.reset)")
	}
	// the DURABLE checkpoint is rolled back: the store's live snapshot is the clean pre-mint baseline
	// (the rejected batch's specialists are gone), so a fresh engine boots without them.
	if specs := e.Store().Snapshot().Specialists; len(specs) != 0 {
		t.Fatalf("the committed revert must roll the durable checkpoint back to the clean baseline; got %d specialist(s) still persisted", len(specs))
	}
}

// The durability INTERLOCK (§7.5, non-negotiable): a batch with a clean MEASURED fitness WIN that BREAKS
// the durability re-gate is REVERTED, never promoted — a self-change may not be kept just because it scores
// well if it pushed the plant out of the durable regime. Here a fork-storm drives n supercritical (n>=1, the
// subcritical condition fails), so even a +0.7 fitness win reverts.
func TestSelfBenchDurabilityFailVetoesAFitnessWin(t *testing.T) {
	dir := t.TempDir()
	e, got := selfBenchEngine(t, dir, false)

	// drive the live regulator supercritical (n>=1): a sustained fork-storm breaks the n<1 condition.
	for i := 0; i < 12; i++ {
		e.Regulator().Update(regulator.UpdateOpts{Fired: 5, Admitted: 10, Baseline: 0, BranchesLive: 8, Forked: 8})
	}
	if e.Regulator().N() < 1.0 {
		t.Fatalf("precondition: the fork-storm must drive n supercritical, got n=%.3f", e.Regulator().N())
	}

	seedMintBatch(e, 0.9, "learned:winButUnstable") // a fitness WIN (+0.7 over the floor)
	e.FlushState()

	verdicts := kindsSeen(*got, events.SelfBenchVerdict)
	if len(verdicts) != 1 {
		t.Fatalf("want one selfbench.verdict, got %d", len(verdicts))
	}
	v := verdicts[0]
	if v.Data["durable"] != false {
		t.Fatalf("the supercritical plant must read NOT durable, got durable=%v (regime=%v)", v.Data["durable"], v.Data["regime"])
	}
	if v.Data["verdict"] != "revert" {
		t.Fatalf("a durability FAIL must veto a fitness win (REVERT), got verdict=%v", v.Data["verdict"])
	}
	if d, _ := v.Data["delta"].(float64); d <= selfBenchNoiseFloor {
		t.Fatalf("the fitness delta should still be a clean win (the interlock vetoes a WIN), got delta=%v", d)
	}
	reverts := kindsSeen(*got, events.SelfBenchRevert)
	if len(reverts) != 1 {
		t.Fatalf("a durability-fail revert must emit selfbench.revert once, got %d", len(reverts))
	}
	if reason, _ := reverts[0].Data["reason"].(string); reason == "net-negative delta" {
		t.Fatalf("the revert reason must cite the durability fail, not a net-negative delta (the delta was a win)")
	}
	if len(kindsSeen(*got, events.SelfBenchPromote)) != 0 {
		t.Fatalf("a durability fail must never promote")
	}
}

// Default OFF is byte-identical: the SelfBench loop never runs, so no selfbench.* event is ever emitted —
// the freq×value heuristic alone decides, exactly as before this slice.
func TestSelfBenchOffEmitsNoSelfBenchEvents(t *testing.T) {
	dir := t.TempDir()
	e := newPersistEngine(t, dir) // SelfBenchGate stays OFF (DefaultConfig)
	var got []events.Event
	e.Bus().Subscribe(func(ev events.Event) { got = append(got, ev) })

	seedMintBatch(e, 0.9, "learned:offA", "learned:offB")
	e.FlushState()

	for _, kind := range []string{events.SelfBenchVerdict, events.SelfBenchPromote, events.SelfBenchRevert} {
		if n := len(kindsSeen(got, kind)); n != 0 {
			t.Fatalf("with the gate OFF no %s may be emitted, got %d", kind, n)
		}
	}
}
