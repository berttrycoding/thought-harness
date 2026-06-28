package retrieval

import "testing"

// recallAtK = fraction of a probe's relevant items that appear in the top-k of a ranking.
func recallAtK(ranking []Scored, relevant []int, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	rel := map[int]bool{}
	for _, id := range relevant {
		rel[id] = true
	}
	top := ranking
	if len(top) > k {
		top = top[:k]
	}
	hit := 0
	for _, s := range top {
		if rel[s.ID] {
			hit++
		}
	}
	return float64(hit) / float64(len(relevant))
}

// TestFuseRRFLiftsSemanticWinner is the fusion mechanics gate: an item that lexical ranks SECOND but
// semantic ranks FIRST must end up on top after RRF — i.e. fusion lets a semantically-strong,
// lexically-weak item beat a lexically-strong but semantically-weak one. This is the property that
// makes hybrid beat lexical on paraphrase (where the true answer is the semantic winner).
func TestFuseRRFLiftsSemanticWinner(t *testing.T) {
	// A: lexical winner, semantic loser. B: lexical 2nd, semantic winner.
	lexical := []Scored{{1, 0.5}, {2, 0.1}, {3, 0.1}, {4, 0.0}}  // A=1 first
	semantic := []Scored{{2, 0.9}, {3, 0.4}, {4, 0.2}, {1, 0.1}} // B=2 first
	fused := FuseRRF(lexical, semantic)
	if fused[0].ID != 2 {
		t.Fatalf("RRF should lift the semantic winner (item 2) to the top; got order %v", ids(fused))
	}
	// every item still present (fusion drops nothing).
	if len(fused) != 4 {
		t.Fatalf("fusion must keep all items; got %d", len(fused))
	}
}

// TestHybridLexicalFallback: with no embedder, Hybrid is exactly the lexical ranking (the offline path
// stays alive) and reports usedSemantic=false.
func TestHybridLexicalFallback(t *testing.T) {
	items := ParaphraseDataset()[0].Items
	got, used := Hybrid("a sluggish lookup spanning two tables", items, nil)
	if used {
		t.Fatal("Hybrid with nil embedder must not report semantic use")
	}
	want := RankLexical("a sluggish lookup spanning two tables", items)
	if len(got) != len(want) || got[0].ID != want[0].ID {
		t.Fatalf("Hybrid(nil) must equal RankLexical; got %v want %v", ids(got), ids(want))
	}
}

// TestSubmodularReserveDiversity: with two near-duplicate clusters, a budget of 2 must pick ONE from
// each cluster (diversity), never two near-identical items — the facility-location dedup.
func TestSubmodularReserveDiversity(t *testing.T) {
	// items 0,1 are near-duplicates; items 2,3 are near-duplicates; the two clusters are dissimilar.
	cluster := map[int]int{0: 0, 1: 0, 2: 1, 3: 1}
	items := []Item{{ID: 0}, {ID: 1}, {ID: 2}, {ID: 3}}
	sim := func(a, b Item) float64 {
		if a.ID == b.ID {
			return 1.0
		}
		if cluster[a.ID] == cluster[b.ID] {
			return 0.95 // near-duplicate
		}
		return 0.1 // different cluster
	}
	picked := SubmodularReserve(items, sim, 2)
	if len(picked) != 2 {
		t.Fatalf("expected 2 picks, got %v", picked)
	}
	if cluster[picked[0]] == cluster[picked[1]] {
		t.Fatalf("reserve picked two near-duplicates from the same cluster: %v", picked)
	}
}

// TestLexicalCeilingOnDataset re-confirms (via the package's own scorer) that lexical retrieval has a
// low recall@1 on the paraphrase set — the ceiling the model-gated R1 benchmark proves hybrid lifts.
func TestLexicalCeilingOnDataset(t *testing.T) {
	hits, n := 0, 0
	for _, ep := range ParaphraseDataset() {
		for _, p := range ep.Probes {
			n++
			r := RankLexical(p.Query, ep.Items)
			if len(r) > 0 && r[0].Score > 0 && recallAtK(r, p.Relevant, 1) > 0 {
				hits++
			}
		}
	}
	if hits*2 >= n {
		t.Fatalf("lexical recall@1 = %d/%d is too high — dataset not paraphrase-hard", hits, n)
	}
	t.Logf("lexical recall@1 on paraphrase set = %d/%d", hits, n)
}

func ids(s []Scored) []int {
	out := make([]int, len(s))
	for i, x := range s {
		out[i] = x.ID
	}
	return out
}
