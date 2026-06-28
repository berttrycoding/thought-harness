package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// The empty welcome screen must animate on arrival but SETTLE (stop animating) after the window, so an
// idle welcome no longer pins a core at 30fps forever (T3). A /clear back to the welcome re-opens it.
func TestWelcomeAnimationSettles(t *testing.T) {
	cfg := engine.DefaultConfig()
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	a.mode = ModeChat
	a.substrateLoading = false // not waiting on a model — the settle window governs

	if !a.animating() {
		t.Fatal("the welcome should animate on arrival (frame 0 < settle window)")
	}
	// fast-forward past the settle window: it must stop animating (idle CPU drops to 0).
	a.frame = a.welcomeSettle + 1
	if a.animating() {
		t.Fatal("the welcome should settle (stop animating) after the window")
	}
	// while the substrate is still loading, the spinner keeps animating regardless of the window.
	a.substrateLoading = true
	if !a.animating() {
		t.Fatal("a loading welcome must keep animating (the spinner)")
	}
	a.substrateLoading = false

	// re-opening the window (a /clear back to the welcome) animates again from the current frame.
	a.armWelcomeSettle()
	if !a.animating() {
		t.Fatal("re-arming the settle window should animate again")
	}
}
