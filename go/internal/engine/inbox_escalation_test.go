package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// inbox_escalation_test.go — the COGNITION-property gate for the async inbox push channel (O-5,
// 2026-06-20-auto-dev-lathe-vs-fleet.md §4#6/§6, dogfooded inward over proactive outreach). It pins the
// THINKING the design intends, not just that the loop runs:
//
//   - ON + IGNORED: an unacknowledged outreach is RE-SURFACED with escalating urgency (conscious.inbox_
//     escalate), the escalation count RAMPS, and it is durability-BOUNDED at InboxMaxEscalations — it does
//     NOT spam forever (the LATHE 7-identical-outreaches UAT bug is structurally impossible);
//   - ON + ACKNOWLEDGED: a user turn arriving CLEARS the pending item — the channel never escalates over a
//     live conversation (the social-awareness rule);
//   - OFF: byte-identical — no pending tracking, no re-surface, no conscious.inbox_escalate event ever.
//
// Internal (package engine) so it can drive the real continuous Step() loop AND seed/inspect the
// unexported pendingInbox + arousal — the escalation logic under test, on the live wire. Deterministic on
// the TestBackend double + seed 7. (countKind(*events.Bus, kind) lives in percept_test.go.)

// mkInboxEngine builds a continuous-mode engine on the test double with the given inbox_escalation flag.
// The wake-transcript + proactive-outreach config is irrelevant here — the test seeds the pending item
// directly (the unexported inboxItem) to exercise the ESCALATION half (the base maybeReachOut gate is
// separately exercised; this pins the re-surface/acknowledge/cap cognition).
func mkInboxEngine(t *testing.T, on bool) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = config.New()
	cfg.Features.Conscious.Activity.InboxEscalation = on
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// stepAwakeIgnored advances the continuous loop n ticks while keeping the mind AWAKE and the user SILENT
// — the "ignored outreach" condition the escalation channel exists for. It pins arousal=AWAKE before each
// tick (a real awake stream that keeps producing thought stays awake; this removes the test's dependence
// on the wander dodging the lull) and never submits a user turn, so UserWaiting stays false and the
// pending item is never acknowledged.
func stepAwakeIgnored(e *Engine, n int) {
	for i := 0; i < n; i++ {
		e.arousal = types.AWAKE
		e.lull = 0
		e.Step()
	}
}

// escalateEvents pulls every conscious.inbox_escalate event off the bus replay ring in emission order.
func escalateEvents(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.bus.Recent(10000, nil) {
		if ev.Kind == events.InboxEscalate {
			out = append(out, ev)
		}
	}
	return out
}

// TestInboxEscalationResurfacesIgnoredOutreachBoundedAndAcknowledges is the O-5 cognition gate. It asserts
// the three intended behaviours of the async inbox push channel in one mutation-sensitive test.
func TestInboxEscalationResurfacesIgnoredOutreachBoundedAndAcknowledges(t *testing.T) {
	// --- ON + IGNORED: re-surfaces with escalating urgency, bounded at the cap, never spams. ----------
	eOn := mkInboxEngine(t, true)
	eOn.cfg.InboxMaxEscalations = 2 // explicit: at most 2 re-pushes

	// Boot the awake loop a couple of ticks so a graph + episode exist (UserWaiting can be derived), then
	// plant the PENDING item the base outreach would have left (the developed line the user ignored).
	stepAwakeIgnored(eOn, 2)
	firstTick := eOn.bus.Tick
	eOn.pendingInbox = &inboxItem{
		text:       "the auth refactor lets us drop the legacy token path entirely",
		value:      0.6,
		firstTick:  firstTick,
		lastTick:   firstTick,
		escalation: 0,
	}

	// The escalation cooldown is strictly LONGER than first contact (1.5x OutreachCooldown). Step well
	// past three such cooldowns with the user silent: the channel must re-surface, ramping, then STOP.
	escCooldown := eOn.cfg.OutreachCooldown + eOn.cfg.OutreachCooldown/2 // 8 + 4 = 12
	stepAwakeIgnored(eOn, escCooldown*3)

	got := escalateEvents(eOn)
	if len(got) == 0 {
		t.Fatalf("ON+ignored: an unacknowledged outreach was NEVER re-surfaced (0 conscious.inbox_escalate) — the async push channel is dead on the live loop")
	}
	if len(got) > eOn.cfg.InboxMaxEscalations {
		t.Fatalf("ON+ignored: re-surfaced %d times, but InboxMaxEscalations=%d — the durability bound failed, the channel spams (the LATHE 7-outreaches bug)", len(got), eOn.cfg.InboxMaxEscalations)
	}
	if len(got) != eOn.cfg.InboxMaxEscalations {
		t.Fatalf("ON+ignored: re-surfaced %d times over 3 cooldowns with the user silent, want exactly the cap %d (escalate up to the bound, then stop)", len(got), eOn.cfg.InboxMaxEscalations)
	}
	// The urgency must RAMP: escalation counts strictly increasing 1,2 (not all 1), each carrying the
	// SAME developed-line insight (with its V(s) — not an empty notification).
	for i, ev := range got {
		esc, _ := ev.Data["escalation"].(int)
		if esc != i+1 {
			t.Fatalf("ON+ignored: escalation #%d carried count %d, want %d — urgency must escalate, not repeat flat", i, esc, i+1)
		}
		if txt, _ := ev.Data["text"].(string); txt == "" {
			t.Fatalf("ON+ignored: escalation #%d carried no text — it must re-push the SAME insight, not an empty ping", i)
		}
		if v, _ := ev.Data["value"].(float64); v == 0 {
			t.Fatalf("ON+ignored: escalation #%d carried no value — the re-push must keep the line's V(s)", i)
		}
	}
	// After the cap, the pending item is dropped (it will not nag forever).
	if eOn.pendingInbox != nil {
		t.Fatal("ON+ignored: the pending item was NOT dropped after exhausting the escalation budget — it would nag forever (durability)")
	}

	// --- ON + ACKNOWLEDGED: a user turn clears the pending item; no escalation over a live conversation.
	eAck := mkInboxEngine(t, true)
	eAck.cfg.InboxMaxEscalations = 2
	stepAwakeIgnored(eAck, 2)
	ackTick := eAck.bus.Tick
	eAck.pendingInbox = &inboxItem{
		text:       "the cache invalidation race is the root cause, not the lock",
		value:      0.7,
		firstTick:  ackTick,
		lastTick:   ackTick,
		escalation: 0,
	}
	// The user RESPONDS — a fresh turn arrives. This is the acknowledgement signal (UserWaiting goes true).
	eAck.SubmitDefault("oh interesting, tell me more about that race")
	eAck.arousal = types.AWAKE
	eAck.lull = 0
	eAck.Step() // the next awake tick must observe the response and clear the pending item

	if eAck.pendingInbox != nil {
		t.Fatal("ON+acknowledged: a user response did NOT clear the pending inbox item — the channel would escalate over a live conversation (the social-awareness rule broke)")
	}
	// Even far past the escalation cooldown afterwards, it must NEVER re-surface the acknowledged item.
	escCooldownAck := eAck.cfg.OutreachCooldown + eAck.cfg.OutreachCooldown/2
	stepAwakeIgnored(eAck, escCooldownAck*3)
	if n := len(escalateEvents(eAck)); n != 0 {
		t.Fatalf("ON+acknowledged: re-surfaced %d times after a user response — an acknowledged push must never escalate", n)
	}

	// --- OFF: byte-identical — no pending tracking, no re-surface, no event ever. --------------------
	eOff := mkInboxEngine(t, false)
	stepAwakeIgnored(eOff, 2)
	// Even if some path set a pending item, maybeEscalateInbox is a no-op when the flag is OFF.
	offTick := eOff.bus.Tick
	eOff.pendingInbox = &inboxItem{text: "should-never-surface", value: 0.9, firstTick: offTick, lastTick: offTick}
	escCooldownOff := eOff.cfg.OutreachCooldown + eOff.cfg.OutreachCooldown/2
	stepAwakeIgnored(eOff, escCooldownOff*3)
	if n := countKind(eOff.bus, events.InboxEscalate); n != 0 {
		t.Fatalf("OFF: emitted %d conscious.inbox_escalate — the default-OFF path must be byte-identical (silent)", n)
	}
}
