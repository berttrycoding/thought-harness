package main

import "testing"

// TestSubstrateLabel pins the substrate-provenance tag per backend. The claude
// case is the regression guard: a Claude-Code-bridge run MUST tag claude:<model>,
// never llm:<model> — mislabeling a frontier (subscription, temperature-
// uncontrolled) run as local-llm would let the rows mix into the local dataset
// and break re-localization (CLAUDE.md substrate hygiene). Found 2026-06-13: a
// real --backend claude grounding run wrote substrate=llm:sonnet.
func TestSubstrateLabel(t *testing.T) {
	cases := []struct {
		backend  string
		model    string
		expected string
	}{
		{"test", "anything", "test"},
		{"session", "sonnet", "cc:session"},
		{"claude", "sonnet", "claude:sonnet"},
		{"claude", "haiku", "claude:haiku"},
		{"llm", "google/gemma-4-26b-a4b", "llm:google/gemma-4-26b-a4b"},
	}
	for _, c := range cases {
		got := substrateLabel(config{backend: c.backend, llmModel: c.model})
		if got != c.expected {
			t.Errorf("substrateLabel(backend=%q model=%q) = %q, want %q", c.backend, c.model, got, c.expected)
		}
	}
}
