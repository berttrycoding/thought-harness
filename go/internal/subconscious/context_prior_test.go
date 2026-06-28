package subconscious

import "testing"

// TestContextQuality pins the Context PRIOR (§3.12): a richer context (real gist + grounding thoughts +
// declared knowledge) scores higher than a thin one, in [0,1]; a nil context is 0.
func TestContextQuality(t *testing.T) {
	var nilCtx *Context
	if nilCtx.Quality() != 0 {
		t.Error("nil context quality must be 0")
	}

	thin := &Context{}
	rich := &Context{
		L1: L1Snapshot{Gist: "a worked spine", ThoughtIDs: []int{1, 2, 3, 4, 5, 6}},
		L3: []KnowledgeRef{{IndexID: "risk"}},
	}

	if rich.Quality() <= thin.Quality() {
		t.Errorf("a rich context must outrank a thin one: rich=%.2f thin=%.2f", rich.Quality(), thin.Quality())
	}
	if q := rich.Quality(); q < 0 || q > 1 {
		t.Errorf("quality must be in [0,1], got %.2f", q)
	}
	// the gist term alone lifts a context above an empty one.
	gistOnly := &Context{L1: L1Snapshot{Gist: "spine"}}
	if gistOnly.Quality() <= thin.Quality() {
		t.Error("a context with a real gist must outrank an empty one")
	}
}
