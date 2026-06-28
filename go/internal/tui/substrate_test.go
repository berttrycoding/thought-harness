package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// runningSubstrateClass must read the ACTUAL backend (so a --backend override is seen), not just
// cfg.Substrate. A test-double engine reports the "test" class even though cfg.Substrate is "auto" — this
// is the gap the live /model guard fell through before (the UAT catch).
func TestRunningSubstrateClassReadsBackend(t *testing.T) {
	cfg := engine.DefaultConfig() // Substrate defaults to "auto" (class "local")
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	if substrateClass(a.cfg.Substrate) != "local" {
		t.Fatalf("precondition: cfg.Substrate %q should be class local", a.cfg.Substrate)
	}
	if got := a.runningSubstrateClass(); got != "test" {
		t.Fatalf("runningSubstrateClass = %q, want test (the actual backend), not the cfg class", got)
	}
}

// substrateClass groups names into provenance classes — a CROSS-class change is the consequential one
// the UI gates + rebuilds fresh (S2/S3/S5). Same-class names must collapse; different families must not.
func TestSubstrateClass(t *testing.T) {
	same := [][]string{
		{"local", "llm", "lmstudio", "openai", "auto"}, // model-backed local (auto = optimistic pre-resolution guess)
		{"frontier", "api"},                            // its OWN class: frontier-minted state never mixes with local (hygiene)
		{"session", "cc"},
		{"claude", "claudecode", "claude-code"},
		{"test", "none"},
	}
	for _, group := range same {
		want := substrateClass(group[0])
		for _, s := range group[1:] {
			if got := substrateClass(s); got != want {
				t.Errorf("substrateClass(%q)=%q, want %q (same class as %q)", s, got, want, group[0])
			}
		}
	}
	// a representative cross-class pair must differ (so the confirm gate + fresh rebuild trigger).
	if substrateClass("local") == substrateClass("session") {
		t.Fatal("local and session must be different classes")
	}
	if substrateClass("session") == substrateClass("claude") {
		t.Fatal("session and claude must be different classes")
	}
	if substrateClass("local") == substrateClass("frontier") {
		t.Fatal("local and frontier must be different classes (substrate hygiene: state never mixes)")
	}
}
