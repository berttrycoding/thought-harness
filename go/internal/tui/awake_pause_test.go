package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// TestAwakeStartsPausedUntilUserSpeaks: an awake/continuous TUI starts PAUSED on launch (it does not
// auto-spin its loop), and the first user message un-pauses it (the awake startup UX). Thereafter the
// loop is under the user's ^P control.
func TestAwakeStartsPausedUntilUserSpeaks(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")

	if !a.paused {
		t.Fatal("awake/continuous TUI must start PAUSED on launch")
	}
	if a.userEngaged {
		t.Fatal("userEngaged must be false before any input")
	}

	a.input.SetValue("hello there")
	a.submitInput()

	if a.paused {
		t.Fatal("the first user message must un-pause the awake mind")
	}
	if !a.userEngaged {
		t.Fatal("userEngaged must latch true after the first input")
	}
}

// TestReactiveDoesNotStartPaused: reactive mode idles until a turn — it has no auto-spinning loop to
// pause, so it must NOT start paused.
func TestReactiveDoesNotStartPaused(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	if a.paused {
		t.Fatal("reactive TUI must not start paused")
	}
}
