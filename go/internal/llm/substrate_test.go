package llm

// substrate_test.go — the consolidated substrate surface (2026-06-12): ONE alias table
// (CanonicalSubstrate), ONE selectable menu (SubstrateMenu), ONE class truth (ClassOf, stamped at
// construction — never parsed off display labels), and every menu entry actually resolvable.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
)

// Every name the harness ever accepted normalizes through the ONE table; unknown names refuse.
func TestCanonicalSubstrateAliases(t *testing.T) {
	cases := map[string]string{
		"": "auto", "auto": "auto", "AUTO": "auto",
		"test": "test", "none": "test",
		"local": "local", "llm": "local", "lmstudio": "local", "openai": "local",
		"frontier": "frontier", "api": "frontier",
		"session": "session", "cc": "session",
		"claude": "claude", "claudecode": "claude", "claude-code": "claude",
	}
	for in, want := range cases {
		got, ok := CanonicalSubstrate(in)
		if !ok || got != want {
			t.Errorf("CanonicalSubstrate(%q) = %q,%v; want %q", in, got, ok, want)
		}
	}
	if _, ok := CanonicalSubstrate("gpt-magic"); ok {
		t.Error("an unknown substrate name must refuse, not guess")
	}
}

// THE BUG THIS CONSOLIDATION FIXED: "claude" was on the TUI Settings menu but unhandled by
// ResolveSubstrate — selecting it fell through every case to "no reachable model". Every
// offline-constructible menu entry must resolve to a backend of the right class.
func TestResolveSubstrateCoversItsOwnMenu(t *testing.T) {
	for name, wantClass := range map[string]string{
		"test": "test", "session": "session", "cc": "session",
		"claude": "claude", "claude-code": "claude",
	} {
		be, err := ResolveSubstrate(name, SubstrateConfig{})
		if err != nil {
			t.Fatalf("ResolveSubstrate(%q): %v — a menu entry that cannot resolve", name, err)
		}
		if got := ClassOf(be); got != wantClass {
			t.Errorf("ClassOf(ResolveSubstrate(%q)) = %q, want %q", name, got, wantClass)
		}
	}
	if _, err := ResolveSubstrate("gpt-magic", SubstrateConfig{}); err == nil {
		t.Error("an unknown substrate must error loudly, not fall through to 'no reachable model'")
	}
}

// ClassOf is stamped at construction — local vs frontier derive from the endpoint, the bridges
// stamp themselves, the test double is test. No display-label parsing anywhere.
func TestClassOfStampedAtConstruction(t *testing.T) {
	if got := ClassOf(backends.NewTest()); got != "test" {
		t.Errorf("test double class = %q", got)
	}
	if got := ClassOf(NewOpenAICompat(Options{BaseURL: "http://localhost:1234/v1"})); got != "local" {
		t.Errorf("loopback endpoint class = %q, want local", got)
	}
	if got := ClassOf(NewOpenAICompat(Options{BaseURL: "https://api.anthropic.com/v1", APIKey: "k"})); got != "frontier" {
		t.Errorf("remote endpoint class = %q, want frontier", got)
	}
	be, err := MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("MakeBackend(claude): %v", err)
	}
	if got := ClassOf(be); got != "claude" {
		t.Errorf("claude bridge class = %q", got)
	}
	se, err := MakeBackend("cc", "", "", 0)
	if err != nil {
		t.Fatalf("MakeBackend(cc): %v", err)
	}
	if got := ClassOf(se); got != "session" {
		t.Errorf("session bridge class = %q", got)
	}
}

// The dev override accepts the FULL menu. The policy names (auto/frontier) are validated by the
// alias table only — actually resolving them dials endpoints and can AUTO-LOAD a model onto the
// GPU (EnsureLocalModel), which a test must never do (the gpu.lock rule). The constructible
// entries are built for real, offline.
func TestMakeBackendAcceptsTheFullMenu(t *testing.T) {
	for _, name := range SubstrateMenu {
		if canonical, ok := CanonicalSubstrate(name); !ok || canonical == "" {
			t.Errorf("menu entry %q does not normalize — the menu and the alias table diverged", name)
		}
	}
	for _, name := range []string{"test", "local", "session", "claude"} { // offline-constructible
		if _, err := MakeBackend(name, "", "", 0); err != nil {
			t.Errorf("MakeBackend(%q): %v", name, err)
		}
	}
	if _, err := MakeBackend("gpt-magic", "", "", 0); err == nil {
		t.Error("unknown backend name must error")
	}
}
