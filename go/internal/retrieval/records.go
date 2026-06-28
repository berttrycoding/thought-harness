package retrieval

import "sort"

// records.go holds the shared relevance + bi-temporal-validity floor lifted out of internal/memory so
// BOTH the memory registries (Episodic/Semantic) AND the new knowledge registry (internal/knowledge)
// reuse one proven precision floor instead of duplicating it (representation-space-rebuild.md §3.1).
// Keeping it here (the retrieval leaf that already owns LexicalScore/Cosine/Item) means neither
// registry re-implements ranking, and the "recall nothing for an unrelated query" precision floor is
// identical across stores.

// SemFloor is the cosine above which a semantic match is genuinely on-topic. Tuned for precision (the
// "recall nothing for an unrelated query" floor): sentence embeddings sit ~0.3–0.45 for unrelated text
// and ~0.45–0.55 for *weakly* associated text (e.g. "birthday party" ~0.52 to a cake memory), so the
// floor is set just above that band — a genuinely on-topic paraphrase clears ~0.58+. Below it, recall
// returns nothing rather than a loosely-associated memory.
const SemFloor = 0.55

// ScoreRecords ranks records (given their retrieval Items) against a query, returning the indices of
// the relevant ones best-first — relevant := positive lexical overlap OR cosine >= SemFloor. An
// unrelated query yields an empty slice (the precision floor). embedder may be nil (lexical-only).
// Deterministic: ties broken by ascending index. Lifted from memory.scoreRecords so memory + knowledge
// share one floor.
func ScoreRecords(query string, items []Item, embedder Embedder) []int {
	var qv []float32
	if embedder != nil {
		if v, err := embedder.Embed(query); err == nil {
			qv = v
		}
	}
	type scored struct {
		idx   int
		score float64
	}
	var rel []scored
	for i, it := range items {
		lex := LexicalScore(query, it)
		sem := 0.0
		if qv != nil {
			if iv, err := embedder.Embed(it.Text); err == nil {
				sem = Cosine(qv, iv)
			}
		}
		relevant := lex > 0 || sem >= SemFloor
		if !relevant {
			continue
		}
		score := lex
		if sem >= SemFloor {
			score += sem // boost a semantically on-topic record above a bare lexical hit
		}
		rel = append(rel, scored{i, score})
	}
	sort.SliceStable(rel, func(a, b int) bool {
		if rel[a].score != rel[b].score {
			return rel[a].score > rel[b].score
		}
		return rel[a].idx < rel[b].idx
	})
	out := make([]int, len(rel))
	for i, s := range rel {
		out[i] = s.idx
	}
	return out
}

// CurrentTick is the sentinel asOf value meaning "now" — a record is current iff its ValidTo==0.
const CurrentTick = -1

// ValidAt reports whether a bi-temporal record (validFrom, validTo) held at tick asOf. asOf==CurrentTick
// asks "is it current now" (validTo==0). Otherwise it held at asOf iff it had been asserted
// (validFrom<=asOf) and not yet invalidated (still current, or asOf is before validTo). Lifted from
// memory.validAt so beliefs (Belief) and durable knowledge (Knowledge) share one validity rule.
func ValidAt(validFrom, validTo, asOf int) bool {
	if asOf == CurrentTick {
		return validTo == 0
	}
	if validFrom > asOf {
		return false
	}
	return validTo == 0 || asOf < validTo
}
