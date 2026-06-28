package eval

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
)

// TestCategoryRegistrySeedsFromActionTaxonomy is the gap-7 reconciliation gate: the §3.10a Category
// registry's tool-operation and tool-reach facets must be SEEDED FROM the action gate's canonical wire
// strings (the ONE source of truth) — not a second hardcoded copy. If the action enum and this registry
// ever drift, this fails: the audit flagged exactly that silent duplication
// (gateroute.go's enum vs the dead eval registry). The skill facet has no action twin, so it stays local.
func TestCategoryRegistrySeedsFromActionTaxonomy(t *testing.T) {
	r := NewSeededCategoryRegistry()

	// every action operation wire value is a tool-operation category, and nothing extra is.
	for _, op := range action.OperationWireValues {
		if _, ok := r.Find("tool-operation", op); !ok {
			t.Errorf("tool-operation category %q (from action.OperationWireValues) missing from the registry", op)
		}
	}
	if got := len(r.Facet("tool-operation")); got != len(action.OperationWireValues) {
		t.Errorf("tool-operation facet has %d categories, want %d (the action taxonomy) — they drifted",
			got, len(action.OperationWireValues))
	}

	// every action reach wire value is a tool-reach category, and nothing extra is.
	for _, rc := range action.ReachWireValues {
		if _, ok := r.Find("tool-reach", rc); !ok {
			t.Errorf("tool-reach category %q (from action.ReachWireValues) missing from the registry", rc)
		}
	}
	if got := len(r.Facet("tool-reach")); got != len(action.ReachWireValues) {
		t.Errorf("tool-reach facet has %d categories, want %d (the action taxonomy) — they drifted",
			got, len(action.ReachWireValues))
	}
}

// TestActionTaxonomyWireValuesAreStable pins the exact canonical strings so a rename on either side is
// caught (the gate routes on these; a serialized category tag must round-trip).
func TestActionTaxonomyWireValuesAreStable(t *testing.T) {
	wantOp := []string{"inspect", "mutate", "execute"}
	wantReach := []string{"self", "local", "external"}
	if len(action.OperationWireValues) != len(wantOp) {
		t.Fatalf("action.OperationWireValues = %v, want %v", action.OperationWireValues, wantOp)
	}
	for i, v := range wantOp {
		if action.OperationWireValues[i] != v {
			t.Errorf("OperationWireValues[%d] = %q, want %q", i, action.OperationWireValues[i], v)
		}
	}
	if len(action.ReachWireValues) != len(wantReach) {
		t.Fatalf("action.ReachWireValues = %v, want %v", action.ReachWireValues, wantReach)
	}
	for i, v := range wantReach {
		if action.ReachWireValues[i] != v {
			t.Errorf("ReachWireValues[%d] = %q, want %q", i, action.ReachWireValues[i], v)
		}
	}
}
