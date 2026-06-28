package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/memory"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestMemoryRecordsGroundedEpisode is the declarative-memory gate: an episode that grounds a claim is
// recorded into episodic memory at episode-end and emits memory.record; an ungrounded episode records
// nothing (never-fabricate). An arithmetic goal grounds (compute), so it records.
func TestMemoryRecordsGroundedEpisode(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	var records int
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.MemoryRecord {
			records++
		}
	})
	e.SubmitDefault("What's 9 times 9?")
	e.Run(8)
	if records != 1 {
		t.Fatalf("a grounded arithmetic episode should record once, got %d", records)
	}
	if e.Episodic().Len() != 1 {
		t.Fatalf("episodic memory should hold 1 grounded episode, got %d", e.Episodic().Len())
	}
	// the recorded episode is recallable by a related goal (relevance-gated; cross-episode transfer).
	if got := e.Episodic().Recall("what is 9 times 9", 3); len(got) == 0 {
		t.Fatalf("the grounded episode should be recallable by a related goal")
	}
}

// TestMemoryNeverFabricatesUngrounded confirms an episode that grounds NOTHING is not recorded — memory
// holds only outcomes reality confirmed. (A goal with no computable claim and a fabricated heuristic act
// grounds nothing.)
func TestMemoryNeverFabricatesUngrounded(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	records := 0
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.MemoryRecord {
			records++
		}
	})
	e.SubmitDefault("Tell me a story about a cat")
	e.Run(8)
	if records != 0 {
		t.Fatalf("an ungrounded episode must not record, got %d", records)
	}
	if e.Episodic().Len() != 0 {
		t.Fatalf("episodic memory must stay empty for an ungrounded episode, got %d", e.Episodic().Len())
	}
}

// TestRecallPrimitiveSurfacesGroundedMemory is the M2 worst-gap-fix gate: the `recall` primitive reads
// the REAL memory registry, so a belief the engine grounded becomes REACHABLE by the conscious stream
// it pulls from — the old MemorySpecialist read a hardcoded 5-fact KB completely decoupled from the
// SemanticRegistry, so accumulated grounded knowledge could never surface. This proves the link two
// ways: (1) the engine's MemoryRecaller port surfaces the grounded belief; (2) a recall-shaped prompt
// makes the `recall` specialist fire that belief as an INJECTED candidate into the stream.
func TestRecallPrimitiveSurfacesGroundedMemory(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	// ground a belief into the REAL semantic store (never-fabricate: Grounded:true is required).
	stored := e.Semantic().Record(memory.Belief{
		Statement: "the deployment pipeline runs nightly at 0200 UTC",
		Entities:  []string{"deployment", "pipeline", "nightly"},
		Source:    "reality:run_shell", Grounded: true, ValidFrom: 1,
	})
	if !stored {
		t.Fatal("a grounded belief must be accepted by the semantic store")
	}

	// (1) the MemoryRecaller port reaches the real store (the toy KB is gone).
	got, ok := e.RecallFact("when does the deployment pipeline run nightly")
	if !ok || !strings.Contains(got, "0200 UTC") {
		t.Fatalf("recall port did not surface the grounded belief: ok=%v got=%q", ok, got)
	}
	// a miss must never fabricate (precision floor / never-fabricate).
	if _, ok := e.RecallFact("the airspeed velocity of an unladen swallow"); ok {
		t.Fatal("recall fabricated a hit for an unrelated query")
	}

	// (2) a recall-shaped prompt fires the `recall` specialist, injecting the grounded belief.
	var injectedRecall bool
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.SubFire && ev.Data["domain"] == "recall" {
			injectedRecall = true
		}
	})
	e.SubmitDefault("recall what we know about the deployment pipeline running nightly")
	e.Run(10)
	if !injectedRecall {
		t.Fatal("the recall primitive never fired the grounded belief into the conscious stream")
	}
	// the belief reached the stream as an INJECTED thought (re-voiced by the hidden seam).
	surfaced := false
	for _, tt := range e.Graph().History() {
		if tt.Source == types.INJECTED && strings.Contains(strings.ToLower(tt.Text), "0200 utc") {
			surfaced = true
			break
		}
	}
	if !surfaced {
		t.Fatal("the grounded belief never surfaced as an INJECTED thought (recall->seam link broken)")
	}
}

// TestRetrieverModeAndRecallEvent covers the retrieval wire: the heuristic test double stays lexical
// (no network probe), and a SECOND grounded episode related to a recorded first triggers a recall +
// a retrieval.fused event (the shared primitive observed at the point it runs).
func TestRetrieverModeAndRecallEvent(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	if e.RetrieverMode() != "lexical" {
		t.Fatalf("the heuristic backend must stay lexical (no embedder probe), got %q", e.RetrieverMode())
	}
	var recalls, retrievals int
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.MemoryRecall:
			recalls++
		case events.Retrieval:
			retrievals++
		}
	})
	// first grounded arithmetic episode -> recorded; second related one -> recalls the first.
	e.SubmitDefault("What's 8 times 8?")
	e.Run(8)
	e.SubmitDefault("What's 8 times 8 again?")
	e.Run(8)
	if recalls == 0 || retrievals == 0 {
		t.Fatalf("a second related episode should recall the first + emit retrieval (recalls=%d retrievals=%d)",
			recalls, retrievals)
	}
}
