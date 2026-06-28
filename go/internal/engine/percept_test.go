package engine

import (
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	clockpkg "github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// senseEngine builds a minimal engine with the sense.clock knob set, a Fake clock at
// instant t, and (optionally) a backing Store — the smallest harness exercising the
// percept-log sensor directly (mirrors resume_test's unit-style construction).
func senseEngine(t *testing.T, store persist.Store, senseOn bool, fakeAt time.Time, substrate string) *Engine {
	t.Helper()
	feats := config.AllOn()
	feats.Persist.Enabled = true
	feats.Sense.Clock = senseOn
	e := &Engine{
		bus:          events.NewDefault(),
		features:     &feats,
		clk:          &clockpkg.Fake{T: fakeAt},
		backendLabel: substrate,
	}
	e.cfg.Store = store
	return e
}

func countKind(b *events.Bus, kind string) int {
	n := 0
	for _, ev := range b.Recent(10000, nil) {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// TestSenseClockOffIsNoOp: with sense.clock OFF, senseClock is a pure no-op — no value,
// no percept-log entry, and no perception.clock event (the byte-identical default).
func TestSenseClockOffIsNoOp(t *testing.T) {
	e := senseEngine(t, nil, false, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), "test")
	if v, ok := e.senseClock(0); ok || v != "" {
		t.Fatalf("sense OFF: senseClock = (%q, %v), want (\"\", false)", v, ok)
	}
	if len(e.perceptLog) != 0 {
		t.Fatalf("sense OFF: perceptLog grew to %d, want 0 (no read)", len(e.perceptLog))
	}
	if n := countKind(e.bus, events.PerceptionClock); n != 0 {
		t.Fatalf("sense OFF: emitted %d perception.clock events, want 0", n)
	}
}

// TestSenseClockNilClockIsNoOp: even with sense.clock ON, a nil Clock keeps the engine
// time-blind — no read, no log, no event. (Sensing needs BOTH the knob and a wired clock.)
func TestSenseClockNilClockIsNoOp(t *testing.T) {
	feats := config.AllOn()
	feats.Sense.Clock = true
	e := &Engine{bus: events.NewDefault(), features: &feats, backendLabel: "test"} // clk == nil
	if v, ok := e.senseClock(0); ok || v != "" {
		t.Fatalf("nil clock: senseClock = (%q, %v), want (\"\", false)", v, ok)
	}
	if len(e.perceptLog) != 0 || countKind(e.bus, events.PerceptionClock) != 0 {
		t.Fatal("nil clock: a read/log/event happened, want none (time-blind)")
	}
}

// TestSenseClockRecords: with sense.clock ON and a Fake clock, senseClock RECORDS the
// live instant into the percept-log and emits one perception.clock [record] event.
func TestSenseClockRecords(t *testing.T) {
	at := time.Date(2026, 5, 1, 12, 30, 45, 0, time.UTC)
	e := senseEngine(t, nil, true, at, "test")
	v, ok := e.senseClock(4)
	if !ok {
		t.Fatal("sense ON: senseClock returned ok=false, want a recorded read")
	}
	if want := at.UTC().Format(perceptClockFormat); v != want {
		t.Fatalf("recorded value = %q, want %q", v, want)
	}
	if len(e.perceptLog) != 1 || e.perceptLog[0].Tick != 4 || e.perceptLog[0].Kind != perceptClockKind {
		t.Fatalf("perceptLog = %+v, want one clock@4 entry", e.perceptLog)
	}
	if n := countKind(e.bus, events.PerceptionClock); n != 1 {
		t.Fatalf("emitted %d perception.clock events, want 1", n)
	}
}

// TestPerceptRecordReplayReproducesValue is the load-bearing determinism test: record a
// sense against Fake clock at instant A, persist; a NEW engine with a Fake at a DIFFERENT
// instant B, with the loaded log in REPLAY mode, returns A (the logged value), not B.
// This is exactly what keeps a golden replay deterministic when the live clock differs.
func TestPerceptRecordReplayReproducesValue(t *testing.T) {
	dir := t.TempDir()
	timeA := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	timeB := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)

	// RECORD against clock A and persist.
	storeA, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := senseEngine(t, storeA, true, timeA, "test")
	wantA, _ := a.senseClock(2)
	a.savePerceptLog()
	if wantA != timeA.UTC().Format(perceptClockFormat) {
		t.Fatalf("recorded A = %q, unexpected", wantA)
	}

	// REPLAY: a fresh engine with clock B loads the matching log and replays A's value.
	storeB, _ := persist.NewJSONLStore(dir)
	b := senseEngine(t, storeB, true, timeB, "test")
	b.loadPerceptLog()
	if !b.perceptReplayOK {
		t.Fatal("replay log was not accepted (version/substrate matched, should replay)")
	}
	got, ok := b.senseClock(2)
	if !ok {
		t.Fatal("replay senseClock returned ok=false")
	}
	if got != wantA {
		t.Fatalf("replay value = %q, want the LOGGED A %q (not the live clock B)", got, wantA)
	}
	// The replay path read the LOG, not the live clock: no new RECORD entry was appended.
	if len(b.perceptLog) != 0 {
		t.Fatalf("replay appended %d record entries, want 0 (replay reads the log)", len(b.perceptLog))
	}
	// and the witness event marks it as a replay.
	var sawReplay bool
	for _, ev := range b.bus.Recent(10000, nil) {
		if ev.Kind == events.PerceptionClock && ev.Data["mode"] == "replay" {
			sawReplay = true
		}
	}
	if !sawReplay {
		t.Fatal("no perception.clock [replay] event emitted")
	}
}

// TestPerceptDivergenceRefusesReplay: a log whose VERSION does not match is REFUSED — the
// engine falls back to cold-sense (records the live clock) instead of best-effort replay.
func TestPerceptDivergenceVersionRefused(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A log stamped with a FUTURE/unknown version.
	if err := store.SavePerceptLog(persist.PerceptLogRecord{
		Version:   persist.PerceptLogVersion + 99,
		Substrate: "test",
		Entries:   []persist.PerceptEntry{{Tick: 0, Kind: "clock", Value: "LOGGED-A"}},
	}); err != nil {
		t.Fatal(err)
	}

	liveAt := time.Date(2030, 6, 6, 6, 6, 6, 0, time.UTC)
	e := senseEngine(t, store, true, liveAt, "test")
	e.loadPerceptLog()
	if e.perceptReplayOK {
		t.Fatal("version-divergent log was accepted for replay, want REFUSED (cold-sense)")
	}
	got, _ := e.senseClock(0)
	if want := liveAt.UTC().Format(perceptClockFormat); got != want {
		t.Fatalf("after refusal, senseClock = %q, want the COLD-SENSED live value %q (not LOGGED-A)", got, want)
	}
}

// TestPerceptDivergenceSubstrateRefused: a log recorded against a DIFFERENT substrate is
// REFUSED (the same hygiene rule the resume cursor honours — never replay across substrates).
func TestPerceptDivergenceSubstrateRefused(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePerceptLog(persist.PerceptLogRecord{
		Version:   persist.PerceptLogVersion,
		Substrate: "claude:sonnet", // a different substrate than the running engine
		Entries:   []persist.PerceptEntry{{Tick: 0, Kind: "clock", Value: "LOGGED-CLAUDE"}},
	}); err != nil {
		t.Fatal(err)
	}

	liveAt := time.Date(2031, 7, 7, 7, 7, 7, 0, time.UTC)
	e := senseEngine(t, store, true, liveAt, "test") // running as "test"
	e.loadPerceptLog()
	if e.perceptReplayOK {
		t.Fatal("substrate-divergent log was accepted for replay, want REFUSED (cold-sense)")
	}
	got, _ := e.senseClock(0)
	if want := liveAt.UTC().Format(perceptClockFormat); got != want {
		t.Fatalf("after refusal, senseClock = %q, want the COLD-SENSED live value (not the claude log)", got)
	}
}

// TestSenseFlagOffStartEpisodeByteIdentical is the end-to-end byte-identical guard: a full
// engine with sense.clock OFF (the default) running episodes emits ZERO perception.clock
// events and records ZERO percepts — startEpisode's senseClock call is a no-op, so goldens
// are unchanged. (A Fake clock is wired so only the KNOB, not the clock, gates the sense.)
func TestSenseFlagOffStartEpisodeByteIdentical(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feats := config.AllOn()
	feats.Sense = config.SenseCfg{} // sense.* now DEFAULT-ON; this test asserts the explicit flag-OFF byte-identical path
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.SetClock(clockpkg.NewFake(), 0) // a clock is WIRED, but the knob is OFF
	e.startEpisode("hello there", true)

	if n := countKind(e.bus, events.PerceptionClock); n != 0 {
		t.Fatalf("sense OFF: %d perception.clock events after startEpisode, want 0 (byte-identical)", n)
	}
	if len(e.perceptLog) != 0 {
		t.Fatalf("sense OFF: perceptLog = %d, want 0 (no read)", len(e.perceptLog))
	}
}

// TestSenseFlagOnStartEpisodeRecords: with sense.clock ON, startEpisode senses the clock
// once at episode-open — one record entry + one perception.clock event — proving the wire
// fires on the LIVE loop (not just the unit method).
func TestSenseFlagOnStartEpisodeRecords(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feats := config.AllOn()
	feats.Sense.Clock = true
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.SetClock(clockpkg.NewFake(), 0)
	e.startEpisode("hello there", true)

	if n := countKind(e.bus, events.PerceptionClock); n != 1 {
		t.Fatalf("sense ON: %d perception.clock events after startEpisode, want 1", n)
	}
	if len(e.perceptLog) != 1 || e.perceptLog[0].Kind != perceptClockKind {
		t.Fatalf("sense ON: perceptLog = %+v, want one clock entry", e.perceptLog)
	}
}
