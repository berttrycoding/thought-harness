package graph

import "testing"

// TestLevelGist pins multi-level compression (§3.8): level 0 is lossless (all thoughts), and each higher
// level is progressively lossier (recent -> headline -> bare count). Levels clamp to [0, Max].
func TestLevelGist(t *testing.T) {
	texts := []string{"first thought", "second thought", "a very long final thought that exceeds the headline cutoff for sure yes indeed"}

	full := LevelGist(texts, 0)
	if full != "first thought second thought "+texts[2] {
		t.Errorf("level 0 must be lossless, got %q", full)
	}
	if LevelGist(texts, 1) != texts[2] {
		t.Errorf("level 1 must be the most recent thought, got %q", LevelGist(texts, 1))
	}
	if l2 := LevelGist(texts, 2); len([]rune(l2)) > 49 || l2 == texts[2] {
		t.Errorf("level 2 must be a truncated headline, got %q (len %d)", l2, len([]rune(l2)))
	}
	if l3 := LevelGist(texts, 3); l3 != "(3 thoughts)" {
		t.Errorf("level 3 must be a bare count, got %q", l3)
	}

	// monotonic lossiness: each deeper level is no longer than the previous.
	prev := len(LevelGist(texts, 0))
	for lvl := 1; lvl <= MaxCompressionLevel; lvl++ {
		got := len(LevelGist(texts, lvl))
		if got > prev {
			t.Errorf("level %d (%d chars) is longer than level %d (%d) — not monotonic", lvl, got, lvl-1, prev)
		}
		prev = got
	}

	// clamping + empty.
	if LevelGist(texts, 99) != LevelGist(texts, MaxCompressionLevel) {
		t.Error("a level above Max must clamp to Max")
	}
	if LevelGist(texts, -5) != LevelGist(texts, 0) {
		t.Error("a negative level must clamp to 0")
	}
	if LevelGist(nil, 1) != "" {
		t.Error("empty input must gist to \"\"")
	}
}

// TestStepCompression pins the multi-level compress/expand stepper: +1 deepens, -1 restores, clamped.
func TestStepCompression(t *testing.T) {
	if StepCompression(0, +1) != 1 || StepCompression(1, -1) != 0 {
		t.Error("step +1/-1 must move one level")
	}
	if StepCompression(0, -1) != 0 {
		t.Error("expanding past full must clamp at 0")
	}
	if StepCompression(MaxCompressionLevel, +1) != MaxCompressionLevel {
		t.Error("compressing past Max must clamp at Max")
	}
}
