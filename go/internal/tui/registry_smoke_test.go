package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// TestBuildRegistryCatalog_Smoke renders the catalog from a REAL engine (heuristic test double) so the
// whole registry data flow is exercised end-to-end, and prints it for an eyeball check (go test -v).
func TestBuildRegistryCatalog_Smoke(t *testing.T) {
	cfg := engine.DefaultConfig()
	cfg.Seed = 7
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	b := NewBridge(e)
	cat := b.BuildRegistryCatalog()

	if len(cat.Sections) != 8 {
		t.Fatalf("want 8 sections, got %d", len(cat.Sections))
	}
	want := map[string]bool{"operators": true, "subagents": true, "skills": true,
		"workflows": true, "tools": true, "prompts": true, "memory": true, "knowledge": true}
	for _, s := range cat.Sections {
		if !want[s.ID] {
			t.Errorf("unexpected section id %q", s.ID)
		}
		// every section except the dynamic/proposed/earn-it-empty ones must carry real entries off the
		// engine. knowledge starts empty (it earns items from reality + distillation, §7 open flag #2).
		if s.ID != "memory" && s.ID != "workflows" && s.ID != "knowledge" && s.Count == 0 {
			t.Errorf("section %q has 0 entries", s.Title)
		}
		t.Logf("[%-10s] %-12s (%d) — %s", s.ID, s.Title, s.Count, s.Note)
	}
	for _, s := range cat.Sections {
		if s.ID == "operators" && s.Count < 30 {
			t.Errorf("operators: want >=30 seed, got %d", s.Count)
		}
		if s.ID == "tools" && s.Count != 5 {
			t.Errorf("tools: want 5 built-ins (static fallback when no workspace), got %d", s.Count)
		}
	}

	// print the real render at 120 cols for operators (sel 0), sub-agents (sel 1), memory (sel 6),
	// and the new knowledge section (sel 7).
	t.Log("\n--- OPERATORS ---\n" + cognition.RenderRegistry(cat, 0, 120))
	t.Log("\n--- SUB-AGENTS ---\n" + cognition.RenderRegistry(cat, 1, 120))
	t.Log("\n--- MEMORY ---\n" + cognition.RenderRegistry(cat, 6, 120))
	t.Log("\n--- KNOWLEDGE ---\n" + cognition.RenderRegistry(cat, 7, 120))
}
