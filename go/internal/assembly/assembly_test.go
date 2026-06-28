package assembly

import (
	"strings"
	"testing"
)

func fixture() []Item {
	return []Item{
		{ID: 1, Text: "framed the problem", Tick: 0, Relevance: 0.2, Branch: 0, Value: 0.5, Active: true},
		{ID: 2, Text: "explored option A", Tick: 1, Relevance: 0.3, Branch: 1, Value: 0.8, Active: false},
		{ID: 3, Text: "explored option B about caching", Tick: 2, Relevance: 0.9, Branch: 2, Value: 0.4, Active: false},
		{ID: 4, Text: "back on the main line", Tick: 3, Relevance: 0.1, Branch: 0, Value: 0.6, Active: true},
		{ID: 5, Text: "most recent thought", Tick: 4, Relevance: 0.0, Branch: 0, Value: 0.6, Active: true},
	}
}

// TestEachTemplateProducesItsView is the P4.2 gate (consumer-view half): each template shapes the SAME
// snapshot into its own distinct, consumer-appropriate view.
func TestEachTemplateProducesItsView(t *testing.T) {
	items := fixture()

	// A: recent-content -> the most recent thought leads.
	if a := Assemble(RecentContent, items, 2); !strings.HasPrefix(a, "most recent thought") {
		t.Fatalf("A recent-content should lead with the most recent; got %q", a)
	}
	// B: work-slice -> only the ACTIVE branch, in tick order (no off-branch items).
	b := Assemble(WorkSlice, items, 10)
	if strings.Contains(b, "option A") || strings.Contains(b, "option B") {
		t.Fatalf("B work-slice must exclude off-branch items; got %q", b)
	}
	if !strings.HasPrefix(b, "framed the problem") {
		t.Fatalf("B work-slice should be the active line in tick order; got %q", b)
	}
	// D: executive -> branches by value frontier; the highest-value branch (1, v=0.8) leads.
	d := Assemble(Executive, items, 10)
	if !strings.HasPrefix(d, "branch 1") {
		t.Fatalf("D executive should lead with the highest-value branch; got %q", d)
	}
	// E: search -> the most relevant item (the caching one, rel 0.9) leads.
	if e := Assemble(Search, items, 1); !strings.Contains(e, "caching") {
		t.Fatalf("E search should surface the most relevant item first; got %q", e)
	}
}

// TestAssemblerIsDeterministic is the P4.2 gate (deterministic half): identical inputs always produce
// identical output, across templates and repeats.
func TestAssemblerIsDeterministic(t *testing.T) {
	items := fixture()
	for _, tmpl := range []Template{RecentContent, WorkSlice, Items, Executive, Search} {
		first := Assemble(tmpl, items, 3)
		for i := 0; i < 50; i++ {
			if got := Assemble(tmpl, items, 3); got != first {
				t.Fatalf("[%s] non-deterministic on repeat %d:\n%q\nvs\n%q", tmpl, i, got, first)
			}
		}
	}
}

// TestBudgetTruncates: the assembler respects the item budget (never over-fills the working set).
func TestBudgetTruncates(t *testing.T) {
	items := fixture()
	got := Assemble(Items, items, 2)
	if lines := strings.Count(got, "\n") + 1; lines != 2 {
		t.Fatalf("a budget of 2 should yield 2 items; got %d lines:\n%s", lines, got)
	}
}
