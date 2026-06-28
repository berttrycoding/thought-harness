package convert

import "testing"

// fakePromoter is a FactPromoter test double: it records the Promote/DemoteFact calls so a test can
// assert the consolidation decisions. promoteFrom seeds the trust a statement is "currently at" so the
// monotone-up guard (Promote only raises) and the n==0 "already at the tier" case are exercisable.
type fakePromoter struct {
	current  map[string]float64 // statement -> current trust (the registry's state)
	promotes []promoteCall
	demotes  []demoteCall
}

type promoteCall struct {
	statement string
	toTrust   float64
	recalls   int
	value     float64
}
type demoteCall struct {
	statement string
	toTrust   float64
}

func newFakePromoter() *fakePromoter { return &fakePromoter{current: map[string]float64{}} }

func (f *fakePromoter) Promote(statement string, toTrust float64, recalls int, value float64) (int, float64, float64) {
	from := f.current[statement]
	if toTrust <= from { // mirror the registry: monotone-up, n==0 when nothing to raise / fact absent
		return 0, from, from
	}
	f.current[statement] = toTrust
	f.promotes = append(f.promotes, promoteCall{statement, toTrust, recalls, value})
	return 1, from, toTrust
}

func (f *fakePromoter) DemoteFact(statement string, toTrust float64, nowTick int) int {
	cur, ok := f.current[statement]
	if !ok || toTrust >= cur {
		return 0
	}
	f.current[statement] = toTrust
	f.demotes = append(f.demotes, demoteCall{statement, toTrust})
	return 1
}

// newFacts builds a Convertibility with facts-convert ON and the default gate (MintAfter=3, MintValue=0.2),
// ready for the consolidation tests.
func newFacts() *Convertibility {
	c := New(nil, nil, nil, nil)
	c.EnableFactConvert(true)
	return c
}

// TestFactConsolidationPromotesHighValueRecalledFact is the CORE A-RAG5 cognition assertion: a fact
// RECALLED on enough HIGH-VALUE lines is consolidated into a prior (CLS hippocampus->neocortex). The
// mint basis is recall x value, NOT age. Mutation-sensitive: drop the recall increment or the promote
// call and this fails.
func TestFactConsolidationPromotesHighValueRecalledFact(t *testing.T) {
	c := newFacts()
	const fact = "the deploy script reads PORT from the env"
	// three HIGH-VALUE episodes the fact was active in (clears MintAfter=3 x MintValue=0.2).
	for i := 0; i < 3; i++ {
		c.NoteFactRecall(fact)
		c.AttributeFactValue(0.7)
	}
	pr := newFakePromoter()
	pr.current[fact] = factWarmTrust // it exists at the WARM tier
	promoted := c.ConsolidateFacts(pr, 10)

	if len(promoted) != 1 || promoted[0] != fact {
		t.Fatalf("want exactly the fact consolidated, got %v", promoted)
	}
	if len(pr.promotes) != 1 {
		t.Fatalf("want exactly one Promote call, got %d: %v", len(pr.promotes), pr.promotes)
	}
	got := pr.promotes[0]
	if got.toTrust != factPriorTrust {
		t.Fatalf("must promote to the PRIOR tier %.2f, got %.2f", factPriorTrust, got.toTrust)
	}
	if got.recalls != 3 {
		t.Fatalf("the recall x value basis must carry the recall count 3, got %d", got.recalls)
	}
	if got.value < 0.69 {
		t.Fatalf("the peak value basis must be the high line value ~0.7, got %.2f", got.value)
	}
	// a SECOND consolidate pass must NOT re-promote (already a prior) — idempotent.
	if again := c.ConsolidateFacts(pr, 11); len(again) != 0 {
		t.Fatalf("a consolidated prior must not re-promote, got %v", again)
	}
}

// TestFactConsolidationRespectsValueGate proves a fact recalled MANY times on LOW-value lines is NOT
// consolidated — the consolidation basis is recall x VALUE, not recall alone (a fact present only on
// failing lines is not a prior). This is the value half of the gate; without it the harness would
// consolidate noise (the §9.5 lesson applied to facts).
func TestFactConsolidationRespectsValueGate(t *testing.T) {
	c := newFacts()
	const fact = "an unhelpful low-value note"
	for i := 0; i < 6; i++ { // plenty of recalls...
		c.NoteFactRecall(fact)
		c.AttributeFactValue(0.05) // ...but every line is BELOW the value floor (0.2)
	}
	pr := newFakePromoter()
	pr.current[fact] = factWarmTrust
	if promoted := c.ConsolidateFacts(pr, 10); len(promoted) != 0 {
		t.Fatalf("a low-value fact must NOT consolidate regardless of recall count, got %v", promoted)
	}
	if len(pr.promotes) != 0 {
		t.Fatalf("no Promote call expected for a sub-floor fact, got %v", pr.promotes)
	}
	// the fact WAS tracked (lastVal/peak recorded) but recalls stayed 0 (no high-value episode).
	fs := c.Facts()
	if len(fs) != 1 || fs[0].Recalls != 0 || fs[0].Promoted {
		t.Fatalf("expected one tracked, never-promoted, zero-recall fact, got %+v", fs)
	}
}

// TestFactConsolidationFrequencyGate proves the FREQUENCY half: two high-value recalls (below MintAfter=3)
// do not yet consolidate — a single lucky high-value hit is not a prior. Mutation-sensitive on MintAfter.
func TestFactConsolidationFrequencyGate(t *testing.T) {
	c := newFacts()
	const fact = "recalled only twice on a good line"
	for i := 0; i < 2; i++ {
		c.NoteFactRecall(fact)
		c.AttributeFactValue(0.8)
	}
	pr := newFakePromoter()
	pr.current[fact] = factWarmTrust
	if promoted := c.ConsolidateFacts(pr, 10); len(promoted) != 0 {
		t.Fatalf("two recalls (below MintAfter=3) must NOT consolidate, got %v", promoted)
	}
	// a third high-value recall crosses the gate.
	c.NoteFactRecall(fact)
	c.AttributeFactValue(0.8)
	if promoted := c.ConsolidateFacts(pr, 11); len(promoted) != 1 {
		t.Fatalf("the third high-value recall must consolidate, got %v", promoted)
	}
}

// TestFactConsolidationKeepOrRevert is the keep-or-revert assertion: a promoted prior whose LATEST line
// reality REFUTES (its value falls below the floor) is reverted — the discredited prior stops being
// trusted as a prior (re-priced down to the WARM tier). This mirrors the specialist Demote lineage and is
// the safety half of A-RAG5 (a consolidated fact reality later overturned must not stay a high prior).
func TestFactConsolidationKeepOrRevert(t *testing.T) {
	c := newFacts()
	const fact = "the cache TTL is 60 seconds"
	for i := 0; i < 3; i++ { // consolidate it first
		c.NoteFactRecall(fact)
		c.AttributeFactValue(0.7)
	}
	pr := newFakePromoter()
	pr.current[fact] = factWarmTrust
	if promoted := c.ConsolidateFacts(pr, 10); len(promoted) != 1 {
		t.Fatalf("precondition: the fact must consolidate, got %v", promoted)
	}
	if pr.current[fact] != factPriorTrust {
		t.Fatalf("precondition: trust must be at the prior tier, got %.2f", pr.current[fact])
	}
	// now a REFUTING line: the fact was recalled again but the line failed (value below the floor).
	c.NoteFactRecall(fact)
	c.AttributeFactValue(0.05)
	c.ConsolidateFacts(pr, 11)

	if len(pr.demotes) != 1 || pr.demotes[0].statement != fact {
		t.Fatalf("a refuted prior must be DemoteFact'd, got %v", pr.demotes)
	}
	if pr.current[fact] != factWarmTrust {
		t.Fatalf("a reverted prior must drop back to the WARM tier %.2f, got %.2f", factWarmTrust, pr.current[fact])
	}
	if len(c.FactsReverted) != 1 || c.FactsReverted[0] != fact {
		t.Fatalf("the revert must be recorded in FactsReverted, got %v", c.FactsReverted)
	}
}

// TestFactConsolidationRecallIsOncePerEpisode proves a fact recalled MULTIPLE times in ONE episode counts
// ONCE toward the frequency gate — the consolidation unit is the high-value EXPERIENCE, not recall
// multiplicity (the CLS basis). Three recalls in a single episode must NOT cross MintAfter=3.
func TestFactConsolidationRecallIsOncePerEpisode(t *testing.T) {
	c := newFacts()
	const fact = "recalled thrice in one episode"
	c.NoteFactRecall(fact)
	c.NoteFactRecall(fact)
	c.NoteFactRecall(fact)
	c.AttributeFactValue(0.9) // ONE high-value episode closes
	pr := newFakePromoter()
	pr.current[fact] = factWarmTrust
	if promoted := c.ConsolidateFacts(pr, 10); len(promoted) != 0 {
		t.Fatalf("three recalls in ONE episode = one experience, must not cross MintAfter, got %v", promoted)
	}
	if fs := c.Facts(); len(fs) != 1 || fs[0].Recalls != 1 {
		t.Fatalf("the fact's recall count must be 1 (one episode), got %+v", fs)
	}
}

// TestFactConsolidationOffIsNoOp is the byte-identical guard: with facts-convert OFF (the default), every
// entry point is a pure no-op — no tally, no promote, no demote. This is what keeps the default path
// byte-identical.
func TestFactConsolidationOffIsNoOp(t *testing.T) {
	c := New(nil, nil, nil, nil) // facts-convert OFF (default)
	for i := 0; i < 5; i++ {
		c.NoteFactRecall("anything")
		c.AttributeFactValue(0.9)
	}
	pr := newFakePromoter()
	pr.current["anything"] = factWarmTrust
	if promoted := c.ConsolidateFacts(pr, 10); promoted != nil {
		t.Fatalf("facts-convert OFF must be a no-op, got %v", promoted)
	}
	if len(pr.promotes) != 0 || len(c.Facts()) != 0 {
		t.Fatalf("facts-convert OFF must track nothing, promotes=%v facts=%v", pr.promotes, c.Facts())
	}
}

// TestFactConsolidationNilPromoter proves ConsolidateFacts is safe with no knowledge registry wired
// (the bare offline path) — a nil promoter is a no-op even with facts-convert on.
func TestFactConsolidationNilPromoter(t *testing.T) {
	c := newFacts()
	c.NoteFactRecall("x")
	c.AttributeFactValue(0.9)
	if promoted := c.ConsolidateFacts(nil, 10); promoted != nil {
		t.Fatalf("nil promoter must be a no-op, got %v", promoted)
	}
}
