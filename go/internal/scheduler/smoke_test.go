package scheduler

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

func TestForegroundMatchesTailNotHead(t *testing.T) {
	// the critical bug the Python comment warns about: matching the head
	// demoted every reasoning call to background. Match the TAIL.
	if !IsForeground("action.respond") {
		t.Fatal("action.respond must be foreground (tail=respond)")
	}
	if !IsForeground("conscious.generate") {
		t.Fatal("conscious.generate must be foreground (tail=generate)")
	}
	if !IsForeground("respond") {
		t.Fatal("bare respond must be foreground")
	}
	if IsForeground("subconscious.specialist") {
		t.Fatal("subconscious.specialist must be background (tail=specialist)")
	}
	if IsForeground("synthesize_program") {
		t.Fatal("synthesize_program must be background")
	}
}

func TestGrantBudget(t *testing.T) {
	var deferred []events.Event
	emit := func(kind, summary string, data map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: data}
		if kind == events.Schedule {
			deferred = append(deferred, ev)
		}
		return ev
	}
	s := New(emit, nil) // default bg_budget=3
	// foreground always granted, never spends budget
	for i := 0; i < 5; i++ {
		if !s.Grant("conscious.generate") {
			t.Fatal("foreground must always be granted")
		}
	}
	// background: 3 grants then defer
	granted := 0
	for i := 0; i < 5; i++ {
		if s.Grant("subconscious.specialist") {
			granted++
		}
	}
	if granted != 3 {
		t.Fatalf("background budget 3: granted %d", granted)
	}
	if len(deferred) != 2 {
		t.Fatalf("want 2 deferral events, got %d", len(deferred))
	}
	// tick reset restores budget
	s.TickResetDefault()
	if !s.Grant("subconscious.specialist") {
		t.Fatal("budget should be restored after TickReset")
	}
}

func TestTickResetIdleVsEngaged(t *testing.T) {
	s := New(nil, nil)
	s.TickReset(0.1, false) // low value, no user -> idle budget (1)
	if s.Grant("bg.x") != true || s.Grant("bg.x") != false {
		t.Fatal("idle budget should be exactly 1")
	}
	s.TickReset(0.5, false) // engaged
	g := 0
	for i := 0; i < 5; i++ {
		if s.Grant("bg.x") {
			g++
		}
	}
	if g != 3 {
		t.Fatalf("engaged budget 3, got %d", g)
	}
}
