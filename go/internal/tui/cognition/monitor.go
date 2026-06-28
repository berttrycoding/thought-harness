package cognition

// monitor.go — the shared rendering primitives for the W2 live validation instruments (the
// 2026-06-12 mockup conventions, locked across 11 client review rounds). Every runtime MONITOR
// panel renders on these, so the conventions live in ONE place and cannot drift between panels:
//
//   - full-width tick STRIP: one char per tick, █ = happened, _ = didn't, newest on the right,
//     horizon from config (default monitorHorizon). A timeline is always its own full row.
//   - INDICATOR row: fixed names in fixed positions, the one that fired this tick lit (bright),
//     the rest dim — LEDs, not a scrolling history. (Color, not bold; bold is retired harness-wide.)
//   - FRESHNESS: every metric carries its age — "now" / "Nt"; past the horizon it reads stale.
//   - VITAL: an analog status as WORD + number ("STABLE (n 0.073)"), never a bare float.
//   - LAST block: a terminal event as two rows — header (STATE · tick N) then the sentence alone.
//
// The layout core (stripCells/ageLabel/stale/vitalText/indicatorPlain) is pure and unit-tested; the
// exported helpers add color through the package's existing run/txt styling.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	glyphOn  = "█" // a tick where the tracked thing happened
	glyphOff = "_" // a tick where it didn't

	// monitorHorizon is the default strip window (ticks). Per the locked spec it is config-driven
	// (tui.strip_horizon); this is the fallback when no override is wired. Panels pass their own
	// width, so this is only the convention's documented default.
	monitorHorizon = 50
)

// stripCells renders a tick history as the raw glyph string: one char per tick, newest on the
// RIGHT, exactly `width` columns. A shorter history is LEFT-padded with the off glyph (the window
// has not filled yet); a longer one keeps only the most recent `width` ticks. Pure — no styling.
func stripCells(hits []bool, width int) string {
	if width <= 0 {
		return ""
	}
	// keep the last `width` ticks (newest-right).
	if len(hits) > width {
		hits = hits[len(hits)-width:]
	}
	var b strings.Builder
	b.Grow(width)
	for i := 0; i < width-len(hits); i++ {
		b.WriteString(glyphOff) // left-pad the unfilled window
	}
	for _, h := range hits {
		if h {
			b.WriteString(glyphOn)
		} else {
			b.WriteString(glyphOff)
		}
	}
	return b.String()
}

// Strip renders a colored full-width tick strip (the on-glyphs in `on`, the off-glyphs dim) — one
// dedicated row. The caller supplies the boolean history and the on-tone (e.g. colOk for admits).
func Strip(hits []bool, width int, on lipgloss.Color) string {
	cells := stripCells(hits, width)
	// color the off-glyphs faint, the on-glyphs in `on`, char by char (cheap at ≤50 cols).
	var b strings.Builder
	for _, r := range cells {
		if string(r) == glyphOn {
			b.WriteString(txt(glyphOn, on).render())
		} else {
			b.WriteString(txt(glyphOff, colFaint).render())
		}
	}
	return b.String()
}

// ageLabel formats a tick-age the locked way: 0 ⇒ "now", else "Nt" (N ticks ago). Pure.
func ageLabel(ageTicks int) string {
	if ageTicks <= 0 {
		return "now"
	}
	return fmt.Sprintf("%dt", ageTicks)
}

// stale reports whether an age has fallen past the freshness horizon — a metric older than the
// window is no longer a claim about NOW and must read dim. Pure.
func stale(ageTicks, horizon int) bool {
	if horizon <= 0 {
		horizon = monitorHorizon
	}
	return ageTicks > horizon
}

// vitalText is the analog-status layout: a status WORD with its number in parens — "STABLE (n
// 0.073)". Pure (no color); the styled VitalRow adds the word's semantic tone. detail is the
// already-formatted number string (the caller controls decimals).
func vitalText(word, detail string) string {
	if detail == "" {
		return word
	}
	return word + " (" + detail + ")"
}

// indicatorPlain lays out a fixed-name indicator row in plain text, the lit name wrapped in
// brackets so the layout (and which name is lit) is unit-testable without ANSI. Names sit in fixed
// positions separated by two spaces; an empty `lit` lights nothing. Pure.
func indicatorPlain(names []string, lit string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		if n == lit {
			parts[i] = "[" + n + "]"
		} else {
			parts[i] = n
		}
	}
	return strings.Join(parts, "  ")
}

// IndicatorRow renders the fixed-name indicator row with color carrying the lit/dim state: the name
// that fired this tick is bright (`on`), the rest are dim. (The flash-and-fade is the caller cycling
// the tone over ~3 ticks; this renders one frame.) An empty `lit` dims every name.
func IndicatorRow(names []string, lit string, on lipgloss.Color) string {
	parts := make([]string, len(names))
	for i, n := range names {
		if n == lit {
			parts[i] = txt(n, on).render()
		} else {
			parts[i] = txt(n, colFaint).render()
		}
	}
	return strings.Join(parts, "  ")
}

// lastBlock is the two-row terminal-event block as plain text: a header ("STATE · tick N") and the
// reason sentence on its OWN line (sentences read alone). Returns the two lines. Pure.
func lastBlock(state string, tick int, reason string) (header, sentence string) {
	header = fmt.Sprintf("%s · tick %d", state, tick)
	sentence = reason
	return header, sentence
}

// -- shared vital words (the translated-status convention) ------------------

// stabilityWord translates the branching ratio n into the durability status word: the engineering
// number stays, the WORD is the at-a-glance read (the locked VITALS/REGULATOR convention). 1.0 is
// the runaway cliff; 0.7 the margin. Pure.
func stabilityWord(n float64) string {
	switch {
	case n >= 1.0:
		return "RUNAWAY"
	case n >= 0.7:
		return "MARGINAL"
	default:
		return "STABLE"
	}
}

// stabilityTone is the semantic colour for a stability word (green stable / amber margin / red cliff).
func stabilityTone(n float64) lipgloss.Color {
	switch {
	case n >= 1.0:
		return colErr
	case n >= 0.7:
		return colWarn
	default:
		return colOk
	}
}

// loadWord translates utilisation U into its status word: schedulable up to 1.0, SATURATED at it. Pure.
func loadWord(u float64) string {
	if u >= 1.0 {
		return "SATURATED"
	}
	return "OK"
}

// loadTone is the semantic colour for a load word.
func loadTone(u float64) lipgloss.Color {
	if u >= 1.0 {
		return colErr
	}
	return colOk
}
