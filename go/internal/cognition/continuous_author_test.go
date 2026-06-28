package cognition

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestAwakeIdleContentHasNoCannedFallback is the STANDING GUARD: the awake-mind's idle content
// (curiosity goals, fresh-line seeds, default-mode associations) is CONTENT (= the model), so a
// Drives / DefaultMode with NO content author wired must go DARK — return nil / empty — NEVER a
// hardcoded musing. There is no canned pool in the production path by construction; if anyone
// re-adds one (a fixed string list voiced as a thought, the "manufactured intelligence" the Filter
// exists to kill — feedback-heuristic-control-only), one of these assertions fails immediately.
//
// The CONTROL is preserved and asserted separately (the d.n%3 curiosity cycle, the <0.2 lull skip):
// the rng draws still advance; only the TEXT is gone.
func TestAwakeIdleContentHasNoCannedFallback(t *testing.T) {
	// Drives with NO author (the production default before the engine wires backend.Wander).
	d := NewDrives(nil)

	// FreshGoal must be DARK with no author — it seeds NOTHING (no canned musing).
	if got := d.FreshGoal(); got != nil {
		t.Fatalf("FreshGoal with nil author must be DARK (nil); got a canned thought: %q", got.Text)
	}

	// ProposeGoal's curiosity branch fires on the d.n%3==0 cycle. Drive the cycle with a graph whose
	// frontier is BELOW the pursuit threshold (so the maintenance-drive resume branch never fires and
	// the curiosity branch is the one under test). With no author the curiosity branch must be DARK.
	g := graph.New("a low-value goal")
	for i := 0; i < 6; i++ { // hits n%3==0 twice (n=3, n=6) — the curiosity branch under test
		if got := d.ProposeGoal(g, 0.0); got != nil {
			t.Fatalf("ProposeGoal curiosity with nil author must be DARK (nil); got: %q", got.Text)
		}
	}

	// DefaultMode.Wander with NO author must wander DARK (empty slice), never a canned association.
	// Seed the rng so the FIRST draw is >= 0.2 (PAST the lull skip — so we are testing the AUTHORING
	// branch, not the lull early-return). cpyrand reproduces CPython MT19937; seed 2's first draw is
	// ~0.956, well above the 0.2 lull threshold.
	m := NewDefaultMode(nil)
	rng := cpyrand.New(2)
	if got := m.Wander(rng); len(got) != 0 {
		t.Fatalf("Wander with nil author must be DARK (empty); got %d candidate(s): %q", len(got), got[0].Text)
	}
}

// TestAwakeIdleContentFlowsWithAuthor is the POSITIVE control proving the dark-on-nil above is real
// (not a permanently-broken generator): with a content author wired, the SAME paths DO produce
// content authored by the closure — and the closure (not a hardcoded pool) is the source. It also
// proves the kind/hint contract reaches the author and the d.n cycle / lull control still gates.
func TestAwakeIdleContentFlowsWithAuthor(t *testing.T) {
	var seenKinds []string
	author := func(kind, hint string) string {
		seenKinds = append(seenKinds, kind)
		return "authored:" + kind // a non-empty, kind-distinct content string
	}

	d := NewDrives(nil)
	d.SetAuthor(author)

	if got := d.FreshGoal(); got == nil || got.Text != "authored:curiosity" {
		t.Fatalf("FreshGoal with author must author via the closure; got %+v", got)
	}

	g := graph.New("a low-value goal")
	curiosityFired := false
	for i := 0; i < 6; i++ {
		if got := d.ProposeGoal(g, 0.0); got != nil {
			if got.Text != "authored:curiosity" {
				t.Fatalf("ProposeGoal curiosity must author via the closure; got %q", got.Text)
			}
			if got.Source != types.GENERATED {
				t.Fatalf("an authored curiosity goal must be GENERATED; got %v", got.Source)
			}
			curiosityFired = true
		}
	}
	if !curiosityFired {
		t.Fatal("the d.n%3 curiosity cycle never fired with an author wired — control gate broken")
	}

	m := NewDefaultMode(nil)
	m.SetAuthor(author)
	rng := cpyrand.New(2) // first draw ~0.956 — past the 0.2 lull, so the authoring branch runs
	got := m.Wander(rng)
	if len(got) != 1 || got[0].Text != "authored:association" {
		t.Fatalf("Wander with author must author the association via the closure; got %v", got)
	}

	// the author saw the documented kinds (proving the kind contract reaches the content layer).
	for _, want := range []string{"curiosity", "association"} {
		found := false
		for _, k := range seenKinds {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("author never received kind %q; saw %v", want, seenKinds)
		}
	}
}
