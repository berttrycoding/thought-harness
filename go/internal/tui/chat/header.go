package chat

// header.go — the HeaderBar: a 1-line, 3-zone frozen top bar (DESIGN §4.2 item 1, lathe
// ui/headerbar.go). Layout: ` [^B Panel] ──── <title> ──── [Shift+Tab Cognition][^K Cmds] `. The
// three zones are measured with lipgloss.Width and the gap is filled with repeated `─` styled faint;
// the title centers between the two rules. Progressive degradation on narrow widths drops the right
// zone, then the rules, then the left zone — never wrapping (the bar is frozen at one line).
//
// Pure renderer: View() returns a string. The root app.go (package tui) owns no state here beyond
// what it sets via the setters; HeaderBar holds only its own display fields.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Header zone glyphs / labels (no emoji — plain text + Unicode box-drawing, DESIGN §5).
const (
	hbRule       = "─" // ─ the divider rule
	hbPanelKey   = "^B"
	hbPanelLabel = " Panel"
	hbModeLabel  = "Shift+Tab Cognition"
	hbCmdsKey    = "^K"
	hbCmdsLabel  = " Cmds"
)

// Header styles — foreground-only, sourced from the single Palette (DESIGN §5). Keys are bold accent;
// labels are muted; the divider rule is faint; the title is bold white (never coloured); the version
// suffix is the trace tone.
var (
	hbKeyStyle     = lipgloss.NewStyle().Foreground(pal.Accent)
	hbLabelStyle   = lipgloss.NewStyle().Foreground(pal.Muted)
	hbTitleStyle   = lipgloss.NewStyle().Foreground(pal.Title)
	hbTopicStyle   = lipgloss.NewStyle().Foreground(pal.Text)
	hbVersionStyle = lipgloss.NewStyle().Foreground(pal.Trace)
	hbDividerStyle = lipgloss.NewStyle().Foreground(pal.Faint)
)

// HeaderBar is the frozen top bar: a sidebar/panel affordance on the left, the session topic (or the
// app name + version) centered, and the mode-toggle + command-palette shortcuts on the right.
type HeaderBar struct {
	width   int
	topic   string // session topic; empty => the "thought <version>" default title
	version string
}

// NewHeaderBar builds a header bar with the given version string (shown in the default title when no
// session topic is set).
func NewHeaderBar(version string) HeaderBar {
	return HeaderBar{version: version}
}

// SetWidth sets the terminal width for the next render.
func (h *HeaderBar) SetWidth(w int) { h.width = w }

// SetVersion sets the version shown next to the app name in the default title.
func (h *HeaderBar) SetVersion(v string) { h.version = v }

// SetTopic sets the session topic shown in the center of the header. Empty string resets to the
// default `thought <version>` title.
func (h *HeaderBar) SetTopic(topic string) { h.topic = topic }

// Topic returns the current session topic ("" when none is set).
func (h *HeaderBar) Topic() string { return h.topic }

// View renders the one-line header (lathe ui/headerbar.go View, ported). The three zones are measured
// and the title is centered between two `─` rules; progressive degradation handles narrow widths.
func (h HeaderBar) View() string {
	w := h.width
	if w < 10 {
		w = 80
	}

	left := hbKeyStyle.Render(hbPanelKey) + hbLabelStyle.Render(hbPanelLabel)

	// Center: topic (plain text tone) or the app name + version default.
	var center string
	if h.topic != "" {
		center = hbTopicStyle.Render(h.truncateTopic(w))
	} else if h.version != "" {
		center = hbTitleStyle.Render("thought") + " " + hbVersionStyle.Render(h.version)
	} else {
		center = hbTitleStyle.Render("thought")
	}

	// Right: the mode toggle + the command palette.
	right := hbKeyStyle.Render("Shift+Tab") + hbLabelStyle.Render(" Cognition") +
		"  " + hbKeyStyle.Render(hbCmdsKey) + hbLabelStyle.Render(hbCmdsLabel)

	leftW := lipgloss.Width(left)
	centerW := lipgloss.Width(center)
	rightW := lipgloss.Width(right)

	// Budget: 6 single-space separators (' '+left+' '+ruleL+' '+center+' '+ruleR+' '+right+' ').
	totalPad := w - leftW - centerW - rightW - 6
	if totalPad < 2 {
		// Progressive degradation — drop the right zone, then the rules, then the left zone.
		if leftW+centerW+4 <= w {
			pad := w - leftW - centerW - 4
			if pad < 1 {
				pad = 1
			}
			return " " + left + " " + hbDividerStyle.Render(strings.Repeat(hbRule, pad)) + " " + center + " "
		}
		// Just the centered title.
		return " " + center + " "
	}

	ruleL := totalPad / 2
	ruleR := totalPad - ruleL
	lRule := hbDividerStyle.Render(strings.Repeat(hbRule, ruleL))
	rRule := hbDividerStyle.Render(strings.Repeat(hbRule, ruleR))

	return " " + left + " " + lRule + " " + center + " " + rRule + " " + right + " "
}

// truncateTopic trims the topic to fit the available center span (reserving the left/right chrome +
// the two rules + spacing). Byte-length truncation is sufficient — topics are ASCII-ish labels.
func (h HeaderBar) truncateTopic(totalWidth int) string {
	maxLen := totalWidth - 40
	if maxLen < 10 {
		maxLen = 10
	}
	t := h.topic
	if len(t) > maxLen {
		t = t[:maxLen-3] + "..."
	}
	return t
}
