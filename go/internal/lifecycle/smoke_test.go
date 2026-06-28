package lifecycle

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// captureEmit returns an emit closure that appends every event to *out.
func captureEmit(out *[]events.Event) events.Emit {
	return func(kind, summary string, data map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: data}
		*out = append(*out, ev)
		return ev
	}
}

func TestTransitionEmitsWireShape(t *testing.T) {
	var ev []events.Event
	l := NewDefault(captureEmit(&ev)) // IDLE
	l.Transition(types.S_ACTIVE, "user goal")

	if l.State != types.S_ACTIVE {
		t.Fatalf("state = %v, want ACTIVE", l.State)
	}
	if len(ev) != 1 {
		t.Fatalf("want 1 event, got %d", len(ev))
	}
	e := ev[0]
	if e.Kind != events.Lifecycle {
		t.Fatalf("kind = %q, want %q", e.Kind, events.Lifecycle)
	}
	if e.Summary != "IDLE -> ACTIVE (user goal)" {
		t.Fatalf("summary = %q", e.Summary)
	}
	if e.Data["frm"] != "IDLE" || e.Data["to"] != "ACTIVE" || e.Data["reason"] != "user goal" {
		t.Fatalf("data frm/to/reason wrong: %+v", e.Data)
	}
	if e.Data["legal"] != true { // IDLE -> ACTIVE is in the legal table
		t.Fatalf("IDLE -> ACTIVE should be legal=true, got %v", e.Data["legal"])
	}
	// history seeded with init, then the transition
	if len(l.History) != 2 || l.History[0].Reason != "init" || l.History[1].State != types.S_ACTIVE {
		t.Fatalf("history wrong: %+v", l.History)
	}
}

func TestIllegalTransitionIsAdvisoryNotRefused(t *testing.T) {
	var ev []events.Event
	l := NewDefault(captureEmit(&ev)) // IDLE
	// IDLE -> DONE is NOT in the legal table; it must still happen, flagged legal=false.
	l.Transition(types.DONE, "forced")
	if l.State != types.DONE {
		t.Fatalf("illegal transition must still apply; state = %v", l.State)
	}
	if ev[0].Data["legal"] != false {
		t.Fatalf("illegal transition should carry legal=false, got %v", ev[0].Data["legal"])
	}
}

func TestSelfTransitionIsNoOp(t *testing.T) {
	var ev []events.Event
	l := New(captureEmit(&ev), types.S_ACTIVE)
	l.Transition(types.S_ACTIVE, "again") // ACTIVE -> ACTIVE: Python returns early
	if len(ev) != 0 {
		t.Fatalf("self-transition must not emit; got %d events", len(ev))
	}
	if len(l.History) != 1 { // only the init entry
		t.Fatalf("self-transition must not record history; got %+v", l.History)
	}
}

func TestStopTaxonomy(t *testing.T) {
	cases := []struct {
		kind   types.StopKind
		target types.SystemState
	}{
		{types.GOAL_MET, types.DONE},
		{types.GIVE_UP, types.DONE},
		{types.BLOCKED_REALITY, types.AWAITING_REALITY},
		{types.BLOCKED_USER, types.AWAITING_USER},
		{types.INTERRUPTED, types.SUSPENDED},
	}
	for _, c := range cases {
		var ev []events.Event
		l := New(captureEmit(&ev), types.S_ACTIVE)
		got := l.Stop(c.kind, "because")
		if got != c.target {
			t.Fatalf("Stop(%v) target = %v, want %v", c.kind, got, c.target)
		}
		if l.State != c.target {
			t.Fatalf("Stop(%v) state = %v, want %v", c.kind, l.State, c.target)
		}
		// reason format: "<KIND>: <reason>"
		wantReason := c.kind.String() + ": because"
		if ev[0].Data["reason"] != wantReason {
			t.Fatalf("Stop reason = %q, want %q", ev[0].Data["reason"], wantReason)
		}
	}
}

func TestCompositeIdle(t *testing.T) {
	cases := []struct {
		mid, front, back, pending bool
		want                      string
	}{
		{true, true, false, true, "transitioning -> ACTIVE"},
		{true, true, true, false, "IDLE (background consolidating)"},
		{true, true, false, false, "FULLY IDLE"},
		{true, false, false, false, "AWAITING_REALITY"},
		{false, false, false, false, "ACTIVE"},
		// pending only flips when both middle and front are idle:
		{false, true, false, true, "ACTIVE"},
	}
	for _, c := range cases {
		if got := CompositeIdle(c.mid, c.front, c.back, c.pending); got != c.want {
			t.Fatalf("CompositeIdle(%v,%v,%v,%v) = %q, want %q",
				c.mid, c.front, c.back, c.pending, got, c.want)
		}
	}
}
