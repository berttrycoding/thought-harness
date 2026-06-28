package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// The off-loop Step() mutates the whole engine graph (and the bus tick) while the Bubble Tea main loop
// renders + handles input concurrently. Run BOTH at once under `go test -race`: the discipline (View
// reads the snapshot + the cfg/reg caches, never the live engine; mutations queue while stepping) must
// keep the main loop off the engine so the race detector stays silent and no concurrent-map panic fires.
func TestNoRaceStepVsViewAndMutations(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	a.w, a.h, a.ready = 120, 40, true
	a.mode = ModeCognition
	a.recomputeLayout() // populates the cfg/reg caches

	eng := a.bridge.Engine()

	// the off-loop step goroutine: the engine's only writer while a.stepping holds.
	a.stepping = true
	done := make(chan struct{})
	go func() {
		for i := 0; i < 60; i++ {
			eng.Step() // mutates the subsystem graph + bus.Tick
		}
		close(done)
	}()

	// the "main loop": render every tab + queue mutations + a resize re-clamp, all while the step runs.
	for i := 0; i < 300; i++ {
		a.cogTab = i % len(cognitionTabs)
		_ = a.View()                                                    // reads snapshot + caches only
		a.engineMutate(func() { a.bridge.Engine().SubmitDefault("x") }) // queued (a.stepping)
		if i%50 == 0 {
			a.recomputeLayout() // refreshCogCaches early-returns while stepping; touches no live engine
		}
	}

	<-done
	// the step finished: clear the flag and drain the queued mutations (now the engine is quiescent).
	a.stepping = false
	a.drainEngineOps()
	a.refreshCogCaches()
	if len(a.pendingEngineOps) != 0 {
		t.Fatalf("queue not drained: %d ops left", len(a.pendingEngineOps))
	}
}
