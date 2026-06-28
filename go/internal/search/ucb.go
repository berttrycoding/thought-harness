package search

import (
	"math"
	"sort"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ucb.go is the UCB exploration bonus (deferred task #28 / 02-conscious.md §4.4): draw the search toward
// UNDER-explored lines instead of always re-confirming the highest-value one. ADDITIVE — it does not
// touch the value-only Frontier/Open ordering or types.Branch (visit counts live in a passed-in map, so
// the parity-tested Branch stays unchanged). The engine maintains the visits map and calls UCBFrontier
// for FOCUS/BACKTRACK selection once this is wired in.

// UCBScore is the upper-confidence-bound score of a branch (§4.4): value (exploit) + an exploration
// bonus c·sqrt(ln N / (1+visits)) that grows the less a branch has been visited. c=0 ⇒ pure value
// (greedy A*). total is the sum of visits across the frontier.
func UCBScore(value float64, visits, total int, c float64) float64 {
	if total < 1 {
		total = 1
	}
	return value + c*math.Sqrt(math.Log(float64(total))/float64(1+visits))
}

// UCBFrontier ranks the frontier best-first by UCBScore given a per-branch visit map — an ADDITIVE
// alternative to the value-only Frontier(), so a neglected line can out-rank an over-confirmed one.
// Does not mutate the graph.
func UCBFrontier(g *graph.ThoughtGraph, visits map[int]int, c float64) []*types.Branch {
	fr := g.Frontier()
	total := 0
	for _, b := range fr {
		total += visits[b.ID]
	}
	sort.SliceStable(fr, func(i, j int) bool {
		return UCBScore(fr[i].Value, visits[fr[i].ID], total, c) > UCBScore(fr[j].Value, visits[fr[j].ID], total, c)
	})
	return fr
}
