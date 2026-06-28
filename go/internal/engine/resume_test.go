package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

func drawN(r *cpyrand.Random, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = r.Float64()
	}
	return out
}

func floatsEqual(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRNGRegistryEnumeratesAndRestores: the registry captures every registered stream
// (nil skipped), and restoreRNG reproduces each stream's draws — the guard that no
// stream silently escapes the snapshot (red-team amendment 2).
func TestRNGRegistryEnumeratesAndRestores(t *testing.T) {
	e := &Engine{bus: events.NewDefault()}
	r1, r2 := cpyrand.New(1), cpyrand.New(2)
	e.registerRNG("main", r1)
	e.registerRNG("wander", r2)
	e.registerRNG("absent", nil) // nil ignored — an optional stream that does not exist

	drawN(r1, 37) // advance both off the seed boundary
	drawN(r2, 11)

	snap := e.snapshotRNG()
	if len(snap) != 2 {
		t.Fatalf("registry size = %d, want 2 (nil skipped)", len(snap))
	}
	for _, name := range []string{"main", "wander"} {
		if _, ok := snap[name]; !ok {
			t.Fatalf("stream %q not in snapshot", name)
		}
	}

	want1, want2 := drawN(r1, 50), drawN(r2, 50)
	e.restoreRNG(snap)
	if got1, got2 := drawN(r1, 50), drawN(r2, 50); !floatsEqual(got1, want1) || !floatsEqual(got2, want2) {
		t.Fatal("restoreRNG did not reproduce both streams")
	}
}

// TestResumeRecordRoundTrip: snapshotResumeRecord -> applyResumeRecord into a
// DIFFERENTLY-seeded engine reproduces the stream and the tick — exercising the
// cpyrand.State <-> persist.RNGStreamState conversion (only SetState can make a
// seed-999 generator match a seed-12345 one).
func TestResumeRecordRoundTrip(t *testing.T) {
	e := &Engine{bus: events.NewDefault()}
	r := cpyrand.New(12345)
	e.registerRNG("main", r)
	drawN(r, 100)
	e.bus.Tick = 99

	rec := e.snapshotResumeRecord()
	if rec.Tick != 99 {
		t.Fatalf("record tick = %d, want 99", rec.Tick)
	}
	if got := len(rec.Streams["main"].Words); got != 624 {
		t.Fatalf("main words = %d, want 624 (full MT19937 state)", got)
	}
	want := drawN(r, 50)

	e2 := &Engine{bus: events.NewDefault()}
	r2 := cpyrand.New(999) // different seed
	e2.registerRNG("main", r2)
	e2.applyResumeRecord(&rec)
	if e2.bus.Tick != 99 {
		t.Fatalf("restored tick = %d, want 99", e2.bus.Tick)
	}
	if got := drawN(r2, 50); !floatsEqual(got, want) {
		t.Fatal("applyResumeRecord did not reproduce the stream")
	}
}

// TestResumeCursorPersistsAcrossEngines is the end-to-end power-cycle gate for the
// persistence path: engine A runs + flushes a resume cursor to a state dir; engine B
// sharing that dir with the resume knob ON boots restored to A's tick; engine C with
// the knob OFF cold-boots at tick 0 (byte-identical default). The tick assertion is the
// proof — a fresh engine is tick 0, so tick==A's only if the cursor was loaded+applied.
func TestResumeCursorPersistsAcrossEngines(t *testing.T) {
	dir := t.TempDir()

	storeA, err := persist.NewJSONLStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := mkResumeEngine(t, storeA, true)
	for i := 0; i < 10; i++ {
		a.Step()
	}
	a.FlushState()
	wantTick := a.bus.Tick
	if wantTick == 0 {
		t.Fatal("engine A tick did not advance")
	}

	storeB, _ := persist.NewJSONLStore(dir)
	b := mkResumeEngine(t, storeB, true)
	if b.bus.Tick != wantTick {
		t.Fatalf("resume ON: restored tick = %d, want %d", b.bus.Tick, wantTick)
	}

	storeC, _ := persist.NewJSONLStore(dir)
	c := mkResumeEngine(t, storeC, false)
	if c.bus.Tick != 0 {
		t.Fatalf("resume OFF: tick = %d, want 0 (cold boot, byte-identical default)", c.bus.Tick)
	}
}

func mkResumeEngine(t *testing.T, store persist.Store, resume bool) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Store = store
	feats := config.AllOn()
	feats.Persist.Enabled = true
	feats.Persist.Resume = resume
	cfg.Features = &feats
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}
