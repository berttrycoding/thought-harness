package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/timeline"
)

// TestTimelineFeed verifies the episodic timeline (slice i) is initialised and fed as the engine thinks:
// the helpers append the right kinds, and a real reactive episode populates the trajectory.
func TestTimelineFeed(t *testing.T) {
	e, err := NewEngine(&EngineConfig{Mode: "reactive", Seed: 1}, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e.Timeline() == nil {
		t.Fatal("Timeline() nil after NewEngine")
	}

	// helper-level: the kinds append in order.
	e.tlThought(3, 0, 7)
	e.tlActed(3, 0)
	if n := e.Timeline().Len(); n != 2 {
		t.Fatalf("after 2 helper appends, Len=%d, want 2", n)
	}
	all := e.Timeline().All()
	if all[0].Kind != timeline.ThoughtCreated || all[1].Kind != timeline.Acted {
		t.Fatalf("kinds = %v/%v, want thought-created/acted", all[0].Kind, all[1].Kind)
	}
	if all[0].ThoughtID == nil || *all[0].ThoughtID != 7 || all[0].Tick != 3 {
		t.Fatalf("thought event anchor wrong: %+v", all[0])
	}

	// end-to-end: a real episode populates the trajectory with thought-created events.
	e2, _ := NewEngine(&EngineConfig{Mode: "reactive", Seed: 1}, backends.NewTest())
	e2.Submit("what is 2 + 2?", true)
	e2.Run(12)
	if e2.Timeline().Len() == 0 {
		t.Fatal("timeline empty after an episode — the feed is not firing")
	}
}
