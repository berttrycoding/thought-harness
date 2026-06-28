// hybrid.go wires the shared hybrid retriever (internal/retrieval) into zoommem's working-set
// selection (memory-stack §2 / P1.3). The working set needs the SAME "given a focus, which past
// memories are relevant" operation everything else does — and lexical-only hit a ~66% paraphrase
// ceiling. HybridShortlist ranks candidates through retrieval.Hybrid (lexical + semantic, RRF-fused).
//
// The embedder is a PARAMETER, not a global: the engine passes its reachable embedder (or nil) and
// tests pass a real one. With emb==nil the ranking is pure lexical — IDENTICAL to the existing
// relevanceOnly ordering — so every deterministic zoommem test (T1/T4 budget + durability) is
// untouched; the semantic lift (T2/T3 recall) only engages when an embedder is reachable.
package zoommem

import "github.com/berttrycoding/thought-harness/internal/retrieval"

// HybridShortlist ranks past-memory candidates by relevance to the current focus+goal and returns the
// top-k as Units in ranked order. With emb!=nil it fuses a semantic (embedding cosine) ranking with the
// lexical one via reciprocal-rank fusion (so the embedding cosine baseline never distorts the lexical
// scale); with emb==nil it is lexical-only and matches relevanceOnly's ordering exactly.
func HybridShortlist(cands []Unit, focus Unit, goal string, k int, emb retrieval.Embedder) []Unit {
	if len(cands) == 0 || k <= 0 {
		return nil
	}
	query := goal + " " + focus.Thought
	items := make([]retrieval.Item, len(cands))
	byID := make(map[int]Unit, len(cands))
	for i, u := range cands {
		items[i] = retrieval.Item{ID: u.ID, Text: u.Thought, Entities: u.Entities}
		byID[u.ID] = u
	}
	ranked, _ := retrieval.Hybrid(query, items, emb)
	out := make([]Unit, 0, k)
	for _, s := range ranked {
		if u, ok := byID[s.ID]; ok {
			out = append(out, u)
			if len(out) == k {
				break
			}
		}
	}
	return out
}
