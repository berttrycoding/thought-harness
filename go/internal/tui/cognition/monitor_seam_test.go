package cognition

import (
	"strings"
	"testing"
)

func TestRenderSeamMonitorLanesAndVoice(t *testing.T) {
	v := SeamView{
		Horizon: 10,
		Admit:   []bool{true, false, true, true, false, true, true, true, false, true},       // 7/10 = 70%
		Flag:    []bool{false, true, false, false, true, false, false, false, true, false},   // 3/10 = 30%
		Reject:  []bool{false, false, false, false, false, false, true, false, false, false}, // 1/10 = 10%
		Voiced:  []bool{true, false, true, false, false, true, false, false, false, true},
		RawText: "12 / 12 = 1", VoicedText: "Right, that gives 12 / 12 = 1.", VoicedAge: 0,
	}
	out := stripANSI(RenderSeamMonitor(v))
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("seam monitor should be 6 rows (4 lanes + raw + voiced), got %d:\n%s", len(lines), out)
	}
	checks := []struct {
		row  int
		subs []string
	}{
		{0, []string{"admit", "70%"}},
		{1, []string{"flag", "30%"}},
		{2, []string{"reject", "10%"}},
		{3, []string{"voiced"}},
		{4, []string{"raw", "12 / 12 = 1"}},
		{5, []string{"voiced", "└", "Right, that gives 12 / 12 = 1.", "now"}},
	}
	for _, c := range checks {
		for _, s := range c.subs {
			if !strings.Contains(lines[c.row], s) {
				t.Errorf("row %d missing %q:\n%q", c.row, s, lines[c.row])
			}
		}
	}
	// each lane strip is exactly the horizon width in glyphs.
	for _, row := range lines[:4] {
		on := strings.Count(row, "█")
		off := strings.Count(row, "_")
		if on+off != 10 {
			t.Errorf("lane strip width = %d, want 10: %q", on+off, row)
		}
	}
}

func TestRollingPct(t *testing.T) {
	if got := rollingPct([]bool{true, true, false, false}, 4); got != 50 {
		t.Errorf("2/4 = %d, want 50", got)
	}
	if got := rollingPct(nil, 10); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	// honors the window: only the last w ticks count.
	if got := rollingPct([]bool{true, true, true, true, false, false}, 2); got != 0 {
		t.Errorf("last 2 of [...,f,f] = %d, want 0", got)
	}
}
