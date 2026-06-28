// Package assembly is Context Assembly — the down-face VIEW PRODUCER of the hidden seam (SR-2 / P4.2).
// Taking a snapshot of the thought-graph state and shaping it into the context a given consumer needs is
// NOT one universal "dump the active branch"; the consumers are few and cluster into ~5 TEMPLATES over
// the underlying info layers. Each template is a DETERMINISTIC selection + ordering + budget-truncation
// of the items — no model call — so the assembler is reproducible; only a residual case escalates to an
// agentic pass (not modelled here). This package is the assembler; P4.1 wires the live zoommem working
// set as its item source.
//
// The five templates (the combinatorics collapse because sub-agents cluster by operator FAMILY, not
// per-operator):
//
//	A RecentContent — the most recent thoughts (the down-face relay: what just happened)
//	B WorkSlice     — the current branch's working slice (the active line)
//	C Items         — a flat, deduped item list (a registry/candidate consumer)
//	D Executive     — a structural summary: branch/value frontier, for the Controller's decision
//	E Search        — query-relevant items first (a retrieval consumer)
package assembly

import (
	"fmt"
	"sort"
	"strings"
)

// Item is one unit of graph state the assembler shapes: its text plus the info-layer attributes the
// templates select on (recency tick, relevance to the query, which branch, its value).
type Item struct {
	ID        int
	Text      string
	Tick      int     // recency
	Relevance float64 // relevance to the current query (0..1)
	Branch    int     // which branch it sits on
	Value     float64 // the branch/item value
	Active    bool    // on the active branch
}

// Template selects a view shape.
type Template int

const (
	RecentContent Template = iota // A
	WorkSlice                     // B
	Items                         // C
	Executive                     // D
	Search                        // E
)

func (t Template) String() string {
	return [...]string{"A:recent-content", "B:work-slice", "C:items", "D:executive", "E:search"}[t]
}

// Assemble produces the deterministic context view for template over items, within budget (max items).
// Each template is a pure selection+ordering+truncation; identical inputs always yield identical output.
func Assemble(template Template, items []Item, budget int) string {
	if budget <= 0 {
		budget = len(items)
	}
	sel := append([]Item(nil), items...)

	switch template {
	case RecentContent: // A — most recent first
		sort.SliceStable(sel, func(i, j int) bool {
			if sel[i].Tick != sel[j].Tick {
				return sel[i].Tick > sel[j].Tick
			}
			return sel[i].ID < sel[j].ID
		})
		return render(top(sel, budget))

	case WorkSlice: // B — the active branch's line, in tick order
		var active []Item
		for _, it := range sel {
			if it.Active {
				active = append(active, it)
			}
		}
		sort.SliceStable(active, func(i, j int) bool { return active[i].Tick < active[j].Tick })
		return render(top(active, budget))

	case Items: // C — a flat, deduped item list (by id)
		seen := map[int]bool{}
		var uniq []Item
		for _, it := range sel {
			if seen[it.ID] {
				continue
			}
			seen[it.ID] = true
			uniq = append(uniq, it)
		}
		sort.SliceStable(uniq, func(i, j int) bool { return uniq[i].ID < uniq[j].ID })
		return render(top(uniq, budget))

	case Executive: // D — a structural summary for the Controller (branches + value frontier)
		byBranch := map[int]Item{} // best-value item per branch
		var order []int
		for _, it := range sel {
			if cur, ok := byBranch[it.Branch]; !ok || it.Value > cur.Value {
				if !ok {
					order = append(order, it.Branch)
				}
				byBranch[it.Branch] = it
			}
		}
		sort.SliceStable(order, func(i, j int) bool { return byBranch[order[i]].Value > byBranch[order[j]].Value })
		var b strings.Builder
		for n, br := range order {
			if n >= budget {
				break
			}
			it := byBranch[br]
			fmt.Fprintf(&b, "branch %d (v=%.2f): %s\n", br, it.Value, it.Text)
		}
		return strings.TrimRight(b.String(), "\n")

	case Search: // E — query-relevant first
		sort.SliceStable(sel, func(i, j int) bool {
			if sel[i].Relevance != sel[j].Relevance {
				return sel[i].Relevance > sel[j].Relevance
			}
			return sel[i].ID < sel[j].ID
		})
		return render(top(sel, budget))
	}
	return ""
}

// CoverageMiss is the missing-layer detector (P4.3): the needed items a template's view DROPPED. An
// empty result means the template covers what the consumer needs (no-worse than full context); a
// non-empty result is a missing layer — the assembler dropped something load-bearing, which the
// validation must surface so that layer is promoted to a rule rather than silently lost.
func CoverageMiss(view string, needed []Item) []Item {
	var missed []Item
	for _, it := range needed {
		if !strings.Contains(view, it.Text) {
			missed = append(missed, it)
		}
	}
	return missed
}

func top(items []Item, n int) []Item {
	if len(items) > n {
		return items[:n]
	}
	return items
}

func render(items []Item) string {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = it.Text
	}
	return strings.Join(parts, "\n")
}
