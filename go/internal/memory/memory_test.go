package memory

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

func seedEpisodes(r *EpisodicRegistry) {
	r.Record(Episode{Goal: "make the database queries faster", Entities: []string{"database", "query"},
		Outcome: "added an index on customer_id; queries dropped to milliseconds", Grounded: true, Value: 0.8})
	r.Record(Episode{Goal: "bake a chocolate cake", Entities: []string{"cake", "bake"},
		Outcome: "creamed the butter and eggs at room temperature so the batter did not split", Grounded: true, Value: 0.7})
	r.Record(Episode{Goal: "book a cheap flight to Berlin", Entities: []string{"flight", "travel"},
		Outcome: "booked on a tuesday afternoon for a much lower fare", Grounded: true, Value: 0.6})
	r.Record(Episode{Goal: "improve running endurance", Entities: []string{"running", "fitness"},
		Outcome: "a rest day between sessions let the muscle repair and grow stronger", Grounded: true, Value: 0.7})
}

// TestEpisodicCrossEpisodeRecall is the P2.3 M1 gate: a NEW goal related to a past episode surfaces that
// episode, and an UNRELATED goal surfaces NOTHING (the precision floor). Deterministic offline (lexical).
func TestEpisodicCrossEpisodeRecall(t *testing.T) {
	r := NewEpisodicRegistry(nil)
	seedEpisodes(r)

	got := r.Recall("the database is slow, speed up the queries", 2)
	if len(got) == 0 || got[0].Goal != "make the database queries faster" {
		t.Fatalf("a related goal must recall the database episode first; got %v", goals(got))
	}

	// precision floor: an unrelated goal must surface nothing.
	if none := r.Recall("organize a surprise birthday party for my friend", 3); len(none) != 0 {
		t.Fatalf("an unrelated goal must recall nothing (precision floor); got %v", goals(none))
	}
}

// TestNeverFabricateEpisodic: an ungrounded episode is rejected; only grounded outcomes become memory.
func TestNeverFabricateEpisodic(t *testing.T) {
	r := NewEpisodicRegistry(nil)
	if r.Record(Episode{Goal: "guessed answer", Outcome: "probably 42", Grounded: false}) {
		t.Fatal("an ungrounded episode must not be stored (never-fabricate)")
	}
	if r.Len() != 0 {
		t.Fatalf("ungrounded record leaked into the store; len=%d", r.Len())
	}
	if !r.Record(Episode{Goal: "computed answer", Outcome: "12*31 = 372", Grounded: true}) {
		t.Fatal("a grounded episode must be stored")
	}
	if r.Len() != 1 {
		t.Fatalf("grounded record not stored; len=%d", r.Len())
	}
}

// TestSemanticNeverFabricateAndInvalidate: beliefs obey never-fabricate, and an invalidated belief never
// surfaces as current (invalidate-not-delete; the bi-temporal hook for P6.1).
func TestSemanticNeverFabricateAndInvalidate(t *testing.T) {
	r := NewSemanticRegistry(nil)
	if r.Record(Belief{Statement: "rumour: the cache never expires", Grounded: false}) {
		t.Fatal("an ungrounded belief must be rejected (never-fabricate)")
	}
	if !r.Record(Belief{Statement: "the cache TTL is sixty seconds", Entities: []string{"cache", "ttl"},
		Grounded: true, ValidFrom: 1}) {
		t.Fatal("a grounded belief must be stored")
	}
	if got := r.Recall("how long does the cache live", 1); len(got) == 0 {
		t.Fatal("a grounded belief should be recallable by a related query")
	}
	if n := r.Invalidate("the cache TTL is sixty seconds", 10); n != 1 {
		t.Fatalf("Invalidate should have invalidated 1 belief, got %d", n)
	}
	if got := r.Recall("how long does the cache live", 1); len(got) != 0 {
		t.Fatalf("an invalidated belief must never surface as current; got %v", got)
	}
}

// TestEpisodicSemanticRecallParaphrase (model-gated): a PARAPHRASED goal recalls the right past episode
// via the semantic channel even with no shared words. Skips when no embedder is reachable.
func TestEpisodicSemanticRecallParaphrase(t *testing.T) {
	emb := retrieval.ReachableEmbedder()
	if emb == nil {
		t.Skip("no embeddings endpoint reachable — semantic recall is model-gated")
	}
	r := NewEpisodicRegistry(emb)
	seedEpisodes(r)
	// "sluggish lookups on a big table" paraphrases the database episode with no shared content words.
	got := r.Recall("our lookups got sluggish once the table grew huge", 1)
	if len(got) == 0 || got[0].Goal != "make the database queries faster" {
		t.Fatalf("semantic recall should surface the database episode for a paraphrase; got %v", goals(got))
	}
}

func goals(es []Episode) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Goal
	}
	return out
}
