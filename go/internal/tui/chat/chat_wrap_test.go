package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A long harness turn must WORD-WRAP to the set width, not render as one over-wide line the viewport
// would clip mid-word (the L1 bug: c.width was set but never read).
func TestChatWrapsLongTurn(t *testing.T) {
	c := NewChatView()
	c.SetWidth(40)
	long := strings.Repeat("idempotent ", 20) // ~220 cols on one logical line
	c.Say("harness", long)

	out := c.View()
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Fatalf("line exceeds width 40: %d cols: %q", w, line)
		}
	}
	if len(strings.Split(out, "\n")) < 3 {
		t.Fatalf("expected the long turn to wrap to several lines, got:\n%s", out)
	}
}

// An unset/tiny width must NOT panic and must render (degenerate: the viewport then clips).
func TestChatNoWrapWhenWidthUnset(t *testing.T) {
	c := NewChatView() // width 0
	c.Say("harness", "a short answer")
	if out := c.View(); !strings.Contains(out, "a short answer") {
		t.Fatalf("unset-width render lost the text: %q", out)
	}
}
