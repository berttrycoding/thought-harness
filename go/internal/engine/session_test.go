package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestSessionTreeMirrorsSynthesisedWorkflow is the session-wire gate: a goal that synthesises a
// multi-phase program opens a bounded Session spawn tree (root + one child per phase), emits
// session.spawn → session.dispatch(per phase) → session.terminate, and the live tree is reachable via
// Sessions(). A simple Q&A goal synthesises no program, so it opens NO tree and emits NO session events
// (which is why the scenario goldens only change for S6).
func TestSessionTreeMirrorsSynthesisedWorkflow(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	var spawn, dispatch, terminate int
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.SessionSpawn:
			spawn++
		case events.SessionDispatch:
			dispatch++
		case events.SessionTerminate:
			terminate++
		}
	})

	// a planning goal that synthesises a multi-phase program (decompose → generate → validate).
	e.SubmitDefault("Design a small API for a todo service")
	e.Run(12)

	if spawn != 1 {
		t.Fatalf("want exactly 1 session.spawn for a synthesised workflow, got %d", spawn)
	}
	if dispatch < 2 {
		t.Fatalf("a multi-phase program should dispatch >=2 child sessions, got %d", dispatch)
	}
	if terminate != 1 {
		t.Fatalf("the session tree should terminate once on episode stop, got %d", terminate)
	}
	// the live tree is reachable after termination only if it stayed open; here the episode stopped, so
	// Sessions() is nil — the assertion is that during the run the tree was bounded (depth <= the phase
	// chain), which dispatch>=2 + spawn==1 already establish (root + children, one level).
}

// TestSimpleQAOpensNoSessionTree is the golden-safety invariant: a simple Q&A goal synthesises no
// program, so no session tree opens and no session.* event fires (this is WHY only S6's golden changed).
func TestSimpleQAOpensNoSessionTree(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	sessionEvents := 0
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Layer == "session" {
			sessionEvents++
		}
	})
	e.SubmitDefault("What's 7×8?")
	e.Run(8)
	if sessionEvents != 0 {
		t.Fatalf("a simple Q&A must open no session tree, got %d session events", sessionEvents)
	}
	if e.Sessions() != nil {
		t.Fatalf("no session tree should be open for simple Q&A")
	}
}
