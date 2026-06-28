package retrieval

import "testing"

// TestR1HybridBeatsLexical is the P1.2 gate (memory-stack EXPERIMENT R1): on the paraphrase set, the
// hybrid retriever (lexical + semantic, RRF-fused) must beat lexical-only on recall — the lexical
// ceiling is lifted by the semantic signal.
//
// It is MODEL-GATED: it needs a reachable embeddings endpoint (a model loaded in LM Studio). With none
// reachable it SKIPS — the offline test suite stays green and the mechanics are already covered
// deterministically by retrieval_test.go (FuseRRF lifts the semantic winner; lexical ceiling is low).
// When a model IS loaded, this runs the real A/B and asserts the win, with cached vectors for
// reproducibility.
func TestR1HybridBeatsLexical(t *testing.T) {
	emb := ReachableEmbedder()
	if emb == nil {
		t.Skip("no embeddings endpoint reachable (no model loaded) — R1 semantic win is model-gated; " +
			"mechanics covered by retrieval_test.go")
	}

	const k = 3
	var lexRecall, hybRecall float64
	probes := 0
	for _, ep := range ParaphraseDataset() {
		for _, p := range ep.Probes {
			probes++
			lex := RankLexical(p.Query, ep.Items)
			hyb, used := Hybrid(p.Query, ep.Items, emb)
			if !used {
				t.Fatal("embedder was reachable but Hybrid did not use the semantic signal")
			}
			lexRecall += recallAtK(lex, p.Relevant, k)
			hybRecall += recallAtK(hyb, p.Relevant, k)
		}
	}
	lexRecall /= float64(probes)
	hybRecall /= float64(probes)
	t.Logf("R1 paraphrase recall@%d over %d probes: lexical=%.3f  hybrid=%.3f", k, probes, lexRecall, hybRecall)

	if hybRecall <= lexRecall {
		t.Fatalf("R1 GATE FAILED: hybrid recall@%d (%.3f) did not beat lexical (%.3f)", k, hybRecall, lexRecall)
	}
}
