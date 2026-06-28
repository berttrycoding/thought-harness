package engine

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	clockpkg "github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	hostpkg "github.com/berttrycoding/thought-harness/internal/host"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// introspectEngine builds a full engine (the LIVE wiring path, like orientEngine) with the sense.host /
// sense.event_log / sense.orient / sense.clock knobs set per arg, a Fake clock + a Fake host at KNOWN
// values, and (optionally) a rehydrated priorContext with a KNOWN gist — the smallest harness exercising
// the reach=self introspection sensors through senseSelf / the orientation pass.
func introspectEngine(t *testing.T, hostOn, eventLogOn, orientOn, clockOn bool, gist string, fakeHost hostpkg.Host) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feats := config.AllOn()
	feats.Sense.Host = hostOn
	feats.Sense.EventLog = eventLogOn
	feats.Sense.Orient = orientOn
	feats.Sense.Clock = clockOn
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.SetClock(clockpkg.NewFake(), 0) // a Clock is WIRED; only the knobs gate the senses
	if fakeHost != nil {
		e.SetHost(fakeHost)
	}
	if gist != "" {
		e.priorContext = &subconscious.Context{
			Goal: "the prior episode goal",
			L1: subconscious.L1Snapshot{
				BranchID:   3,
				Gist:       gist,
				ThoughtIDs: []int{1, 2, 3},
				Resolution: "EXPANDED",
			},
			L2: map[string]any{},
		}
	}
	return e
}

// TestSenseHostReturnsFakeValues is the CORRECTNESS ORACLE for read_host: with sense.host ON and a Fake
// host wired, senseHost returns the Fake's EXACT fixed footprint (ok=true). A paraphrase / a zero value
// that loses the read would fail — the engine must surface the real sensed footprint, verbatim.
func TestSenseHostReturnsFakeValues(t *testing.T) {
	want := hostpkg.Sample{AllocMB: 11, SysMB: 33, Goroutines: 5}
	e := introspectEngine(t, true, false, false, false, "", &hostpkg.Fake{S: want})
	got, ok := e.senseHost()
	if !ok {
		t.Fatal("senseHost: ok=false with sense.host ON and a host wired, want true")
	}
	if got != want {
		t.Fatalf("senseHost() = %+v, want the Fake's EXACT %+v", got, want)
	}
}

// TestSenseHostOffIsNoOp: with sense.host OFF (but a host wired), senseHost is a pure no-op — (zero,false),
// no read. The knob, not the wiring, gates the sense (byte-identical default).
func TestSenseHostOffIsNoOp(t *testing.T) {
	e := introspectEngine(t, false, false, false, false, "", hostpkg.NewFake())
	if got, ok := e.senseHost(); ok || got != (hostpkg.Sample{}) {
		t.Fatalf("senseHost with sense.host OFF = (%+v, %v), want (zero, false)", got, ok)
	}
}

// TestSenseHostNilHostIsNoOp: with sense.host ON but NO host wired (nil seam), senseHost is host-blind —
// (zero,false). The wired Host, not just the knob, is required (mirrors senseClock's nil-clock guard).
func TestSenseHostNilHostIsNoOp(t *testing.T) {
	e := introspectEngine(t, true, false, false, false, "", nil) // host ON, but SetHost never called
	if got, ok := e.senseHost(); ok || got != (hostpkg.Sample{}) {
		t.Fatalf("senseHost with nil host = (%+v, %v), want (zero, false) (host-blind)", got, ok)
	}
}

// TestEventRingLastNInOrder is the CORRECTNESS ORACLE for read_event_log: emit a KNOWN sequence of events
// on the engine's bus, then senseEventLog(n) returns the LAST n summaries in oldest-to-newest order, each
// carrying the emitted kind + summary.
func TestEventRingLastNInOrder(t *testing.T) {
	e := introspectEngine(t, false, true, false, false, "", nil) // event_log ON ⇒ the tap is wired
	// Emit a known sequence directly on the bus (the tap subscribes at NewEngine).
	e.bus.Emit("tick", "one", events.D{})
	e.bus.Emit("tick", "two", events.D{})
	e.bus.Emit("tick", "three", events.D{})

	got := e.senseEventLog(2)
	want := []string{"tick: two", "tick: three"}
	if len(got) != len(want) {
		t.Fatalf("senseEventLog(2) = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("senseEventLog(2)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEventRingCapDropsOldest: the ring caps at its bound — after emitting MORE than introspectRingCap
// events, only the most-recent cap survive (oldest dropped), and senseEventLog(cap+10) returns exactly cap.
func TestEventRingCapDropsOldest(t *testing.T) {
	e := introspectEngine(t, false, true, false, false, "", nil)
	total := introspectRingCap + 20
	for i := 0; i < total; i++ {
		e.bus.Emit("tick", "ev-"+itoa(i), events.D{})
	}
	got := e.senseEventLog(total) // ask for more than the cap
	if len(got) != introspectRingCap {
		t.Fatalf("senseEventLog after %d emits returned %d summaries, want the cap %d", total, len(got), introspectRingCap)
	}
	// The oldest survivor is event index (total - cap); the newest is total-1.
	wantFirst := "tick: ev-" + itoa(total-introspectRingCap)
	wantLast := "tick: ev-" + itoa(total-1)
	if got[0] != wantFirst {
		t.Fatalf("ring oldest survivor = %q, want %q (oldest beyond the cap dropped)", got[0], wantFirst)
	}
	if got[len(got)-1] != wantLast {
		t.Fatalf("ring newest = %q, want %q", got[len(got)-1], wantLast)
	}
}

// TestEventTapIsPassive proves the tap does NOT change the event stream a SEPARATE subscriber sees: with
// the tap wired, a second subscriber registered on the same bus still sees EXACTLY the N events emitted,
// in order — the tap neither re-orders, duplicates, nor drops an emit. This is the byte-identical guard
// for the passive ring (the project's "a tap must not perturb the stream" rule).
func TestEventTapIsPassive(t *testing.T) {
	e := introspectEngine(t, false, true, false, false, "", nil) // the tap IS wired
	var seen []string
	unsub := e.bus.Subscribe(func(ev events.Event) {
		seen = append(seen, ev.Kind+":"+ev.Summary)
	})
	defer unsub()

	const n = 5
	for i := 0; i < n; i++ {
		e.bus.Emit("tick", "x"+itoa(i), events.D{})
	}
	if len(seen) != n {
		t.Fatalf("separate subscriber saw %d events, want exactly %d (the tap must not drop/duplicate)", len(seen), n)
	}
	for i := 0; i < n; i++ {
		want := "tick:x" + itoa(i)
		if seen[i] != want {
			t.Fatalf("separate subscriber event[%d] = %q, want %q (order preserved)", i, seen[i], want)
		}
	}
}

// TestEventLogOffNoTap: with sense.event_log OFF, NO tap is wired — senseEventLog returns nil even after
// emits, and the ring is nil. The default engine adds no subscriber (byte-identical).
func TestEventLogOffNoTap(t *testing.T) {
	e := introspectEngine(t, false, false, false, false, "", nil)
	if e.eventRing != nil {
		t.Fatal("sense.event_log OFF: an own-event ring was wired, want none (no tap)")
	}
	e.bus.Emit("tick", "ignored", events.D{})
	if got := e.senseEventLog(10); got != nil {
		t.Fatalf("senseEventLog with event_log OFF = %v, want nil (no read)", got)
	}
}

// TestOrientFoldCarriesHostAndEvents is the CORRECTNESS ORACLE for the FOLD: with sense.host +
// sense.event_log + sense.orient + sense.clock all ON on a resume boot, the injected orientation thought
// CONTAINS the EXACT host footprint ("AllocMB=.. SysMB=.. Goroutines=..") AND a recent-event marker — so
// a resumed session re-grounds on its own footprint + its own log, verbatim.
func TestOrientFoldCarriesHostAndEvents(t *testing.T) {
	const gist = "resuming the regulator-gain trace"
	hostVals := hostpkg.Sample{AllocMB: 13, SysMB: 41, Goroutines: 6}
	e := introspectEngine(t, true, true, true, true, gist, &hostpkg.Fake{S: hostVals})

	e.startEpisode("continue the analysis", true)

	th, ok := orientationThought(e)
	if !ok {
		t.Fatal("orient resume-boot with introspection ON: no re-grounding thought injected")
	}
	// The EXACT host footprint the Fake produced (the orientation re-grounded on the real read, verbatim).
	for _, want := range []string{
		"AllocMB=13", "SysMB=41", "Goroutines=6",
	} {
		if !strings.Contains(th.Text, want) {
			t.Fatalf("orientation thought missing the EXACT host clause %q.\n thought: %q", want, th.Text)
		}
	}
	// The recent-event marker (the engine emitted events before the orientation, so the ring is non-empty).
	if !strings.Contains(th.Text, "event(s) in my log") {
		t.Fatalf("orientation thought missing the recent-event marker.\n thought: %q", th.Text)
	}
	if e.recentEventCount() == 0 {
		t.Fatal("recentEventCount() = 0 after a live episode-open, want > 0 (the tap captured emits)")
	}
}

// TestOrientWitnessCarriesHostAndEvents: the perception.orient event's data carries the EXACT footprint +
// recent-event count, so a TUI/trace reads the same reach=self reality the thought carries.
func TestOrientWitnessCarriesHostAndEvents(t *testing.T) {
	hostVals := hostpkg.Sample{AllocMB: 8, SysMB: 24, Goroutines: 4}
	e := introspectEngine(t, true, true, true, true, "witness gist", &hostpkg.Fake{S: hostVals})

	e.startEpisode("resume", true)

	var saw bool
	for _, ev := range e.bus.Recent(10000, nil) {
		if ev.Kind != events.PerceptionOrient {
			continue
		}
		saw = true
		if b, _ := ev.Data["host_ok"].(bool); !b {
			t.Fatal("perception.orient host_ok=false, want true (read_host fired)")
		}
		if v, _ := ev.Data["alloc_mb"].(uint64); v != hostVals.AllocMB {
			t.Fatalf("perception.orient alloc_mb = %d, want the EXACT %d", v, hostVals.AllocMB)
		}
		if v, _ := ev.Data["goroutines"].(int); v != hostVals.Goroutines {
			t.Fatalf("perception.orient goroutines = %d, want the EXACT %d", v, hostVals.Goroutines)
		}
		if v, _ := ev.Data["recent_events"].(int); v <= 0 {
			t.Fatalf("perception.orient recent_events = %d, want > 0 (the ring captured emits)", v)
		}
	}
	if !saw {
		t.Fatal("no perception.orient event emitted on a resume-wake with introspection ON")
	}
}

// TestOrientFoldOffByteIdentical is the FLAG-OFF byte-identical guard for the FOLD: with sense.host +
// sense.event_log OFF (but orient + clock ON so the pass still fires), the orientation thought contains
// NEITHER a host clause NOR the recent-event marker — the text is EXACTLY the pre-introspection template,
// even with a Fake host wired. (Only the KNOBS, not the wiring, gate the fold.)
func TestOrientFoldOffByteIdentical(t *testing.T) {
	const gist = "the prior focus, no introspection this boot"
	// host OFF, event_log OFF, but orient + clock ON and a Fake host IS wired.
	e := introspectEngine(t, false, false, true, true, gist, hostpkg.NewFake())

	e.startEpisode("resume without introspection", true)

	th, ok := orientationThought(e)
	if !ok {
		t.Fatal("orient ON resume: no re-grounding thought injected")
	}
	if strings.Contains(th.Text, "My footprint") || strings.Contains(th.Text, "AllocMB") {
		t.Fatalf("fold OFF: orientation thought carries a host clause, want none (byte-identical).\n thought: %q", th.Text)
	}
	if strings.Contains(th.Text, "event(s) in my log") {
		t.Fatalf("fold OFF: orientation thought carries the recent-event marker, want none.\n thought: %q", th.Text)
	}
	// The reach=self reads themselves are no-ops with the knobs off.
	if _, ok := e.senseHost(); ok {
		t.Fatal("fold OFF: senseHost fired, want no-op")
	}
	if got := e.recentEventCount(); got != 0 {
		t.Fatalf("fold OFF: recentEventCount = %d, want 0 (no ring)", got)
	}
}
