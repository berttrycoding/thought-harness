package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// Every live-data subsystem that gained a dedicated panel must also pulse its panel on its events —
// the border-flash "something happened" cue. These prefixes previously fell to the nil default (E4).
func TestPanelsForKindCoversLiveSubsystems(t *testing.T) {
	cases := map[string]string{
		events.Ground:                "grounding",
		events.SessionSpawn:          "session",
		events.KnowledgeRecord:       "knowledge",
		events.Retrieval:             "retrieval",
		events.MemoryRecall:          "memory_metrics",
		events.PersistSave:           "persist",
		events.ConfigToggle:          "config_events",
		events.EscalationFloorStands: "critic_text",
		events.PerceptionClock:       "perception",
		events.PerceptionOrient:      "perception",
	}
	for kind, want := range cases {
		got := panelsForKind(kind)
		if !contains(got, want) {
			t.Errorf("panelsForKind(%q) = %v, missing %q", kind, got, want)
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
