package retrieval

import (
	"math"
	"sort"
)

// Scored pairs an item id with a score. Rankings are slices of Scored, sorted by Score descending with
// ties broken by ascending ID (so every ranking is deterministic).
type Scored struct {
	ID    int
	Score float64
}

// sortScored sorts in place: Score desc, ID asc on ties (deterministic).
func sortScored(s []Scored) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Score != s[j].Score {
			return s[i].Score > s[j].Score
		}
		return s[i].ID < s[j].ID
	})
}

// Embedder maps text to a dense vector. The real implementation calls an OpenAI-compatible
// /v1/embeddings endpoint; tests inject a deterministic double. A nil Embedder means "no semantic
// signal reachable" — the retriever falls back to lexical-only (the offline path stays alive).
type Embedder interface {
	Embed(text string) ([]float32, error)
}

// Cosine is the cosine similarity of two equal-length vectors (0 if either is degenerate).
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// RankLexical scores every item by word overlap and returns the ranking (desc, deterministic).
func RankLexical(query string, items []Item) []Scored {
	out := make([]Scored, len(items))
	for i, it := range items {
		out[i] = Scored{ID: it.ID, Score: LexicalScore(query, it)}
	}
	sortScored(out)
	return out
}

// RankSemantic embeds the query + every item and ranks by cosine similarity. Returns an error if the
// embedder fails on any text (so the caller can fall back to lexical). Vectors should be cached by the
// embedder for determinism + cost.
func RankSemantic(query string, items []Item, emb Embedder) ([]Scored, error) {
	qv, err := emb.Embed(query)
	if err != nil {
		return nil, err
	}
	out := make([]Scored, len(items))
	for i, it := range items {
		iv, err := emb.Embed(it.Text)
		if err != nil {
			return nil, err
		}
		out[i] = Scored{ID: it.ID, Score: Cosine(qv, iv)}
	}
	sortScored(out)
	return out, nil
}

// rrfK is the standard reciprocal-rank-fusion constant (60): it damps the influence of exact rank so
// the fusion is robust to score-scale differences between the lexical and semantic rankers.
const rrfK = 60.0

// FuseRRF combines several rankings by reciprocal rank fusion: each item's fused score is the sum over
// rankings of 1/(k + rank), where rank is 0-based position in that ranking. Parameter-free (no tuned
// weight), robust to incommensurable score scales. Items missing from a ranking simply contribute
// nothing from it. Returns the fused ranking (desc, deterministic).
func FuseRRF(rankings ...[]Scored) []Scored {
	fused := map[int]float64{}
	for _, r := range rankings {
		for rank, s := range r {
			fused[s.ID] += 1.0 / (rrfK + float64(rank))
		}
	}
	out := make([]Scored, 0, len(fused))
	for id, sc := range fused {
		out = append(out, Scored{ID: id, Score: sc})
	}
	sortScored(out)
	return out
}

// Hybrid is the shared retrieval primitive: lexical always, fused with semantic when an embedder is
// reachable. With emb==nil (or a semantic failure) it degrades to lexical-only, so the offline path
// stays alive. Returns the ranking (desc, deterministic) and whether the semantic signal was used.
func Hybrid(query string, items []Item, emb Embedder) (ranking []Scored, usedSemantic bool) {
	lex := RankLexical(query, items)
	if emb == nil {
		return lex, false
	}
	sem, err := RankSemantic(query, items, emb)
	if err != nil {
		return lex, false // embedder unreachable mid-run -> lexical-only, never a hard failure
	}
	return FuseRRF(lex, sem), true
}

// SubmodularReserve fills a budget of items by monotone-submodular (facility-location) greedy: each
// pick maximises the MARGINAL coverage gain over the items already chosen, so near-duplicates are not
// both selected (a second copy of an already-covered item adds ~no gain). sim(a,b) in [0,1] is the
// item-item similarity. Deterministic single pass (ties broken by ascending id). Returns the chosen ids
// in pick order. This is the retrieval-reserve fill the spec calls for (provable (1-1/e) floor).
func SubmodularReserve(items []Item, sim func(a, b Item) float64, budget int) []int {
	if budget <= 0 || len(items) == 0 {
		return nil
	}
	if budget > len(items) {
		budget = len(items)
	}
	idx := map[int]Item{}
	for _, it := range items {
		idx[it.ID] = it
	}
	chosen := []int{}
	chosenSet := map[int]bool{}
	// coverage[j] = best similarity of item j to any already-chosen item (how well j is represented).
	coverage := map[int]float64{}
	for _, it := range items {
		coverage[it.ID] = 0
	}
	for len(chosen) < budget {
		bestID, bestGain := -1, math.Inf(-1)
		for _, c := range items {
			if chosenSet[c.ID] {
				continue
			}
			gain := 0.0
			for _, j := range items {
				s := sim(j, c)
				if s > coverage[j.ID] {
					gain += s - coverage[j.ID] // marginal new coverage c brings to j
				}
			}
			if gain > bestGain || (gain == bestGain && (bestID == -1 || c.ID < bestID)) {
				bestID, bestGain = c.ID, gain
			}
		}
		if bestID == -1 {
			break
		}
		chosen = append(chosen, bestID)
		chosenSet[bestID] = true
		for _, j := range items { // update coverage with the new pick
			if s := sim(j, idx[bestID]); s > coverage[j.ID] {
				coverage[j.ID] = s
			}
		}
	}
	return chosen
}
