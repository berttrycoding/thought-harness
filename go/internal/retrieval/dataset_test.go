package retrieval

import "testing"

// (contentWords / jaccard live in text.go — the package's own lexical tokenizer, shared by the
// retriever and this gate so the benchmark and the dataset measure overlap the same way.)

// TestParaphraseDatasetGate is the P1.1 gate: the dataset is a real PARAPHRASE benchmark.
//   - at least 25 probes (the spec's ">=25 probes");
//   - every probe names at least one valid relevant item;
//   - every relevant item is genuinely PARAPHRASED — its lexical overlap with the query is LOW
//     (<=0.25 Jaccard), so a word-overlap retriever cannot find it by surface match;
//   - listed distractors are valid, irrelevant items;
//   - DATASET-WIDE, a word-overlap retriever has LOW recall@1: on most probes it cannot surface the
//     paraphrase as the top hit (no shared words, or a non-relevant item ties/beats it). That low
//     lexical ceiling is exactly what P1.2's hybrid retriever must lift.
func TestParaphraseDatasetGate(t *testing.T) {
	const paraphraseCeil = 0.25

	totalProbes, lexicalHits := 0, 0
	for _, ep := range ParaphraseDataset() {
		byID := map[int]Item{}
		for _, it := range ep.Items {
			byID[it.ID] = it
		}
		for pi := range ep.Probes {
			p := &ep.Probes[pi]
			totalProbes++
			if len(p.Relevant) == 0 {
				t.Errorf("[%s] probe %q has no relevant items", ep.Name, p.Query)
			}
			rel := map[int]bool{}
			maxRel := 0.0
			for _, id := range p.Relevant {
				rel[id] = true
				it, ok := byID[id]
				if !ok {
					t.Errorf("[%s] probe %q references unknown relevant id %d", ep.Name, p.Query, id)
					continue
				}
				ov := jaccard(p.Query, it.Text)
				if ov > paraphraseCeil {
					t.Errorf("[%s] probe %q vs relevant %d: overlap %.2f > %.2f — not a paraphrase (too lexical)",
						ep.Name, p.Query, id, ov, paraphraseCeil)
				}
				if ov > maxRel {
					maxRel = ov
				}
			}
			for _, id := range p.Distractor {
				if _, ok := byID[id]; !ok {
					t.Errorf("[%s] probe %q references unknown distractor id %d", ep.Name, p.Query, id)
				}
				if rel[id] {
					t.Errorf("[%s] probe %q lists item %d as BOTH relevant and distractor", ep.Name, p.Query, id)
				}
			}
			// Would a word-overlap retriever surface the paraphrase as its top hit? Only if the best
			// relevant item has a strictly-positive overlap AND no non-relevant item ties or beats it.
			bestOther := 0.0
			for _, it := range ep.Items {
				if rel[it.ID] {
					continue
				}
				if ov := jaccard(p.Query, it.Text); ov > bestOther {
					bestOther = ov
				}
			}
			if maxRel > 0 && maxRel > bestOther {
				lexicalHits++
			}
		}
	}

	if totalProbes < 25 {
		t.Fatalf("paraphrase dataset has %d probes, need >= 25", totalProbes)
	}
	// Lexical recall@1 must be LOW — else the set is solvable by surface match and doesn't test the
	// paraphrase gap. (P1.2's hybrid retriever is what lifts this.)
	if lexicalHits*2 >= totalProbes {
		t.Fatalf("a word-overlap retriever already gets %d/%d probes — too lexical to test the ceiling",
			lexicalHits, totalProbes)
	}
	t.Logf("paraphrase dataset: %d probes across %d episodes; lexical recall@1 = %d/%d (%.0f%%) — the ceiling P1.2 must lift",
		totalProbes, len(ParaphraseDataset()), lexicalHits, totalProbes, 100*float64(lexicalHits)/float64(totalProbes))
}
