package assembly

import (
	"strings"
	"testing"
)

// itemCost is a stand-in token cost: the rendered length (the assembler's job is to cut cost without
// dropping a needed layer).
func cost(view string) int { return len(view) }

// TestTemplatesAreNoWorseAndCheaper is the P4.3 gate: per consumer, the template view COVERS what that
// consumer needs (no-worse — CoverageMiss is empty) while costing LESS than the full context (cheaper).
func TestTemplatesAreNoWorseAndCheaper(t *testing.T) {
	items := fixture()
	full := Assemble(Items, items, 0) // everything = the full-context baseline
	fullCost := cost(full)

	// Search consumer needs the most-relevant item (the caching one, rel 0.9).
	searchNeeded := []Item{items[2]} // ID 3, "explored option B about caching"
	searchView := Assemble(Search, items, 2)
	if miss := CoverageMiss(searchView, searchNeeded); len(miss) != 0 {
		t.Fatalf("Search must cover the consumer's needed relevant item; missed %v", miss)
	}
	if cost(searchView) >= fullCost {
		t.Fatalf("Search (budget 2) must be cheaper than full context (%d vs %d)", cost(searchView), fullCost)
	}

	// Work-slice consumer needs every ACTIVE-branch item.
	var activeNeeded []Item
	for _, it := range items {
		if it.Active {
			activeNeeded = append(activeNeeded, it)
		}
	}
	wsView := Assemble(WorkSlice, items, 10)
	if miss := CoverageMiss(wsView, activeNeeded); len(miss) != 0 {
		t.Fatalf("WorkSlice must cover every active-branch item; missed %v", miss)
	}
	if cost(wsView) >= fullCost {
		t.Fatalf("WorkSlice must be cheaper than full context (%d vs %d)", cost(wsView), fullCost)
	}
}

// TestMissingLayerIsDetected: when a template view DROPS a needed item (a too-tight budget), the
// missing-layer detector catches it — so a silent loss can't pass validation (it gets promoted to a rule).
func TestMissingLayerIsDetected(t *testing.T) {
	items := fixture()
	// the Search consumer needs the top-2 relevant items, but we give the assembler a budget of 1.
	needed := []Item{items[2], items[1]} // the two most relevant
	tightView := Assemble(Search, items, 1)
	miss := CoverageMiss(tightView, needed)
	if len(miss) == 0 {
		t.Fatal("a too-tight budget DROPPED a needed item — the detector must catch it, not pass silently")
	}
	// and the dropped one is the lower-relevance of the two (the budget kept the top).
	if !strings.Contains(miss[0].Text, "option A") {
		t.Fatalf("the detector should flag the dropped (lower-relevance) item; got %v", miss)
	}
}
