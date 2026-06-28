package search

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

func TestUCBScore(t *testing.T) {
	if got := UCBScore(0.7, 3, 10, 0); got != 0.7 {
		t.Errorf("c=0: %v, want 0.7 (pure value)", got)
	}
	// the bonus decreases with visits...
	if hi, lo := UCBScore(0.5, 1, 100, 1.0), UCBScore(0.5, 10, 100, 1.0); !(hi > lo) {
		t.Errorf("bonus should fall with visits: visits=1 %v vs visits=10 %v", hi, lo)
	}
	// ...and increases with the total count.
	if a, b := UCBScore(0.5, 2, 10, 1.0), UCBScore(0.5, 2, 1000, 1.0); !(b > a) {
		t.Errorf("bonus should rise with total: %v vs %v", a, b)
	}
}

func TestUCBFrontier(t *testing.T) {
	g := graph.New("x")
	parent := g.ActiveBranch
	hi := g.NewBranch(&parent, nil) // high value, heavily visited
	g.Branches[hi].Value, g.Branches[hi].Status = 0.8, types.STASHED
	lo := g.NewBranch(&parent, nil) // low value, unvisited
	g.Branches[lo].Value, g.Branches[lo].Status = 0.4, types.STASHED
	visits := map[int]int{hi: 20, lo: 0}

	if fr := UCBFrontier(g, visits, 0); len(fr) == 0 || fr[0].ID != hi {
		t.Errorf("c=0 (greedy): leader should be the high-value branch %d", hi)
	}
	if fr := UCBFrontier(g, visits, 2.0); len(fr) == 0 || fr[0].ID != lo {
		t.Errorf("c=2: leader should be the unexplored branch %d (exploration)", lo)
	}
}
