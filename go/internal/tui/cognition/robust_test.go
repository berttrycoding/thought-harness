package cognition

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// clip must bound by DISPLAY WIDTH, not rune count: a wide rune (CJK) occupies two cells, so clipping
// "你好世界" to 4 columns yields two runes, not four (L6 — rune-count clipping overflowed the column).
func TestClipByDisplayWidth(t *testing.T) {
	got := clip("你好世界", 4) // each ideograph is 2 cols wide ⇒ 4 cols == 2 runes
	if w := ansi.StringWidth(got); w > 4 {
		t.Fatalf("clip overflowed the column budget: %q is %d cols (want ≤4)", got, w)
	}
	// an ASCII string still clips to exactly n columns.
	if a := clip("abcdefgh", 3); a != "abc" {
		t.Fatalf("ascii clip wrong: %q (want \"abc\")", a)
	}
	if z := clip("anything", 0); z != "" {
		t.Fatalf("clip(_, 0) must be empty, got %q", z)
	}
}

// A degenerate-narrow Panel (total width below the border+padding) must stay BOXED and aligned, never
// pass a negative dimension to lipgloss (which renders unconstrained and de-aligns the row — L7/L8).
func TestPanelRenderNarrowStaysBoxed(t *testing.T) {
	p := Panel{Body: "x"}
	out := p.render(2, 0) // 2 < border(2)+padding(2): inner would be negative without the floor
	if out == "" {
		t.Fatal("narrow panel rendered empty")
	}
	// every rendered row is bounded (no runaway unconstrained width).
	for _, ln := range strings.Split(out, "\n") {
		if ansi.StringWidth(ln) > 8 { // a 1-col inner box is ~ border+pad+1; well under 8
			t.Fatalf("narrow panel row not bounded: %d cols: %q", ansi.StringWidth(ln), ln)
		}
	}
	if floorDim(-5) != 1 || floorDim(0) != 1 || floorDim(7) != 7 {
		t.Fatal("floorDim must floor at 1 and pass values ≥1 through")
	}
}
