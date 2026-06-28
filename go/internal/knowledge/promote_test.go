package knowledge

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// TestPromoteRaisesTrustAndMarksConsolidated is the A-RAG5 consolidation primitive: Promote migrates a
// recalled fact UP to the prior tier and marks it Consolidated, raising its recall trust above the WARM
// default so the sourcing ladder stamps the higher prior next time.
func TestPromoteRaisesTrustAndMarksConsolidated(t *testing.T) {
	var got []events.Event
	r := NewKnowledgeRegistry(nil, func(k, s string, d events.D) events.Event {
		e := events.Event{Kind: k, Summary: s, Data: d}
		got = append(got, e)
		return e
	})
	const fact = "the build cache lives under .cache/go-build"
	r.Record(Knowledge{Statement: fact, Kind: "fact", Source: "reality:read", Grounded: true, Trust: 0.85})

	n, from, to := r.Promote(fact, PriorTrust, 4, 0.7)
	if n != 1 {
		t.Fatalf("Promote must hit the one matching fact, got n=%d", n)
	}
	if from != 0.85 || to != PriorTrust {
		t.Fatalf("Promote must raise 0.85 -> %.2f, got from=%.2f to=%.2f", PriorTrust, from, to)
	}
	cur := r.Current()
	if len(cur) != 1 || !cur[0].Consolidated || cur[0].Trust != PriorTrust {
		t.Fatalf("fact must be Consolidated at the prior tier, got %+v", cur)
	}
	// the recall now stamps the promoted (higher) trust.
	hits := r.Recall("where is the build cache", "", 1)
	if len(hits) != 1 || hits[0].Trust != PriorTrust {
		t.Fatalf("recall must surface the promoted trust %.2f, got %+v", PriorTrust, hits)
	}
	// a knowledge.promote event fired.
	var sawPromote bool
	for _, e := range got {
		if e.Kind == events.KnowledgePromote && e.Data["demote"] == nil {
			sawPromote = true
		}
	}
	if !sawPromote {
		t.Fatalf("a knowledge.promote event must fire on consolidation, got %d events", len(got))
	}
}

// TestPromoteIsMonotoneUp proves Promote NEVER lowers trust — a fact already above the target tier is
// left at its higher trust (consolidation is monotone-up; lowering is the DemoteFact/Invalidate path).
func TestPromoteIsMonotoneUp(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	const fact = "a high-trust seeded fact"
	r.Record(Knowledge{Statement: fact, Kind: "fact", Source: "ingest:seed", Grounded: true, Trust: 0.98})
	n, _, to := r.Promote(fact, PriorTrust, 5, 0.9) // PriorTrust 0.92 < 0.98
	if n != 1 || to != 0.98 {
		t.Fatalf("Promote must not LOWER trust (0.98 stays), got n=%d to=%.2f", n, to)
	}
}

// TestPromoteNeverFabricates proves Promote can only re-price an EXISTING fact — it never conjures a
// statement into a high-trust prior. A statement that was never sourced (absent) promotes nothing, so a
// fabricated "fact" can never enter the registry as a prior via the consolidation path.
func TestPromoteNeverFabricates(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	n, _, _ := r.Promote("a statement that was never sourced", PriorTrust, 9, 0.99)
	if n != 0 {
		t.Fatalf("Promote must not create a fact, got n=%d", n)
	}
	if r.Len() != 0 {
		t.Fatalf("the registry must stay empty, Len=%d", r.Len())
	}
}

// TestPromoteSkipsInvalidated proves a bi-temporally invalidated (refuted) fact is NOT promotable — only
// a currently-valid fact can be consolidated, so a fact reality already overturned can never be lifted
// back to a prior by a stale recall streak.
func TestPromoteSkipsInvalidated(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	const fact = "the API key rotates weekly"
	r.Record(Knowledge{Statement: fact, Kind: "fact", Source: "reality:read", Grounded: true, Trust: 0.85})
	r.Invalidate(fact, retrieval.CurrentTick) // reality refuted it
	if n, _, _ := r.Promote(fact, PriorTrust, 4, 0.7); n != 0 {
		t.Fatalf("an invalidated fact must not be promotable, got n=%d", n)
	}
}

// TestDemoteFactRevertsConsolidation is the keep-or-revert primitive: a consolidated prior reality later
// refuted is re-priced DOWN to the WARM tier and its Consolidated flag is cleared, monotone-down.
func TestDemoteFactRevertsConsolidation(t *testing.T) {
	r := NewKnowledgeRegistry(nil, nil)
	const fact = "the worker pool size is 8"
	r.Record(Knowledge{Statement: fact, Kind: "fact", Source: "reality:read", Grounded: true, Trust: 0.85})
	r.Promote(fact, PriorTrust, 4, 0.7)

	n := r.DemoteFact(fact, 0.85, 12)
	if n != 1 {
		t.Fatalf("DemoteFact must revert the one prior, got n=%d", n)
	}
	cur := r.Current()
	if len(cur) != 1 || cur[0].Consolidated || cur[0].Trust != 0.85 {
		t.Fatalf("a reverted prior must drop to the WARM tier, un-consolidated, got %+v", cur)
	}
	// DemoteFact only touches a CONSOLIDATED fact: a non-consolidated fact is left alone.
	if n2 := r.DemoteFact(fact, 0.5, 13); n2 != 0 {
		t.Fatalf("a non-consolidated fact must not be re-demoted, got n=%d", n2)
	}
}

// TestPriorTrustMatchesConvert pins the cross-package trust-tier agreement: the knowledge prior tier and
// the convert-side constant must never drift (convert defines its own factPriorTrust to stay decoupled).
// This is asserted here because the knowledge package owns the canonical constant.
func TestPriorTrustValue(t *testing.T) {
	if PriorTrust != 0.92 {
		t.Fatalf("PriorTrust drifted from the reality-observation prior 0.92, got %.2f", PriorTrust)
	}
}
