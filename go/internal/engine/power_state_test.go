package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
)

// TestPowerStateProjection: a fresh engine is "booting" (no tick yet); after stepping it
// projects a live power state. Read-only projection — no behavior assertion beyond the label.
func TestPowerStateProjection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 5
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if got := e.PowerState(); got != PowerBooting {
		t.Fatalf("pre-tick PowerState = %q, want %q", got, PowerBooting)
	}
	for i := 0; i < 5; i++ {
		e.Step()
	}
	if got := e.PowerState(); got == PowerBooting {
		t.Fatalf("post-tick PowerState still %q (booting)", got)
	}
}

// TestRequestStopBreaksRun: RequestStop before Run makes the loop exit immediately (0 ticks),
// so the edge can flush without racing the loop.
func TestRequestStopBreaksRun(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 5
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.RequestStop()
	e.Run(50)
	if e.bus.Tick != 0 {
		t.Fatalf("after RequestStop+Run(50), tick = %d, want 0 (loop broke immediately)", e.bus.Tick)
	}
}
