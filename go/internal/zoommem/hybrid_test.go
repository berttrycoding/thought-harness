package zoommem

import (
	"sort"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// TestHybridShortlistLexicalOffline (deterministic, always runs): with no embedder, HybridShortlist is
// pure lexical — a candidate that shares words with the focus+goal ranks above an unrelated one, and
// the result is stable. This is the offline path the engine uses when no embedder is reachable.
func TestHybridShortlistLexicalOffline(t *testing.T) {
	goal := "tune the database read latency"
	focus := Unit{ID: 100, Thought: "the read queries are slow", Entities: []string{"read", "latency"}}
	cands := []Unit{
		{ID: 1, Thought: "add a database index to speed the slow read query", Entities: []string{"index", "read"}},
		{ID: 2, Thought: "the office plants need watering on fridays", Entities: []string{"plants"}},
	}
	got := HybridShortlist(cands, focus, goal, 2, nil)
	if len(got) != 2 || got[0].ID != 1 {
		t.Fatalf("lexical shortlist should rank the on-topic candidate (1) first; got %v", unitIDs(got))
	}
}

// TestR2HybridLiftsRecall is the P1.3 gate (memory-stack EXPERIMENT R2): wiring the hybrid retriever
// into the working-set selection must (A) NOT regress recall on the existing entity-cued dataset (where
// lexical is already strong — the "T1/T4 hold / no regression" half) and (B) clear the ~66% lexical
// ceiling on PARAPHRASE queries through the same wired path (the "T2/T3 rise" half — the paraphrase
// gap is where semantics pays off; on the entity-cued set lexical is near-optimal so RRF ties it).
//
// MODEL-GATED: needs a reachable embeddings endpoint; skips otherwise (the offline lexical path is
// covered deterministically above and is unchanged).
func TestR2HybridLiftsRecall(t *testing.T) {
	emb := retrieval.ReachableEmbedder()
	if emb == nil {
		t.Skip("no embeddings endpoint reachable — R2 recall lift is model-gated")
	}

	// A — no regression on the entity-cued dataset (lexical-friendly: relevant units share entities).
	const K = 6
	var lexRec, hybRec []float64
	for _, ep := range loadEpisodes(t) {
		goal := ep.Units[0].Thought
		for _, p := range ep.Probes {
			focus := find(ep.Units, p.FocusID)
			var cands []Unit
			for _, u := range ep.Units {
				if u.Tick < focus.Tick && u.Branch != focus.Branch {
					cands = append(cands, u)
				}
			}
			if len(cands) == 0 {
				continue
			}
			lexSorted := append([]Unit(nil), cands...)
			sort.SliceStable(lexSorted, func(i, j int) bool {
				return relevanceOnly(lexSorted[i], focus, goal) > relevanceOnly(lexSorted[j], focus, goal)
			})
			lexTop := lexSorted
			if len(lexTop) > K {
				lexTop = lexTop[:K]
			}
			lexRec = append(lexRec, recallOf(idSet(lexTop), p.ShouldSurface))
			hybRec = append(hybRec, recallOf(idSet(HybridShortlist(cands, focus, goal, K, emb)), p.ShouldSurface))
		}
	}
	lexMean, hybMean := mean(lexRec), mean(hybRec)
	t.Logf("R2-A entity-cued recall@%d: lexical=%.0f%%  hybrid=%.0f%% (no-regression check)", K, lexMean*100, hybMean*100)
	if hybMean+0.001 < lexMean {
		t.Fatalf("R2-A: hybrid recall %.3f regressed below lexical %.3f on the entity-cued set", hybMean, lexMean)
	}

	// B — the paraphrase lift, through the SAME wired path (HybridShortlist). Here lexical hits its
	// ceiling and the semantic channel clears it.
	const K2 = 3
	var lexPara, hybPara []float64
	for _, ep := range retrieval.ParaphraseDataset() {
		units := make([]Unit, len(ep.Items))
		for i, it := range ep.Items {
			units[i] = Unit{ID: it.ID, Thought: it.Text, Entities: it.Entities}
		}
		for _, p := range ep.Probes {
			focus := Unit{Thought: p.Query}
			lexPara = append(lexPara, recallOf(idSet(HybridShortlist(units, focus, "", K2, nil)), p.Relevant))
			hybPara = append(hybPara, recallOf(idSet(HybridShortlist(units, focus, "", K2, emb)), p.Relevant))
		}
	}
	lexPMean, hybPMean := mean(lexPara), mean(hybPara)
	t.Logf("R2-B paraphrase recall@%d through HybridShortlist: lexical=%.0f%%  hybrid=%.0f%%", K2, lexPMean*100, hybPMean*100)
	if hybPMean <= lexPMean {
		t.Fatalf("R2-B: wired hybrid (%.3f) did not beat lexical (%.3f) on paraphrase", hybPMean, lexPMean)
	}
	if hybPMean <= 0.66 {
		t.Fatalf("R2-B: wired hybrid recall %.3f did not clear the 66%% lexical ceiling", hybPMean)
	}
}

func unitIDs(us []Unit) []int {
	out := make([]int, len(us))
	for i, u := range us {
		out[i] = u.ID
	}
	return out
}
