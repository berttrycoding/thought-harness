// config_panel.go — the CONFIG browser view (the M6 Config tab). Like the Registry browser it is a
// left INDEX (the config SECTIONS: Subconscious / Conscious / Seam / … / Representation / Persistence)
// + a right DETAIL pane of that section's toggles, each shown [on]/[off] (or its tunable value), with
// the live count of non-default toggles + the loaded config path in a status line. The Representation
// section renders as a 3-BLOCK grid (Moves / Sources / Paths) per the spec.
//
// It is a PURE VIEW over a ConfigView (plain data the bridge assembles from the engine's shared
// HarnessConfig + the canonical knob table) — no engine / config import here, the same discipline as
// the SnapshotData / RegistryCatalog views. Behaviour (flipping a toggle, bumping a tunable) lives in
// the app, which calls back through the bridge's ApplyToggle; this file only lays the state out.
package cognition

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CfgKind distinguishes a bool toggle from a typed tunable so the panel renders [on]/[off] vs a value.
type CfgKind int

const (
	CfgBool   CfgKind = iota // an on/off toggle
	CfgInt                   // an int tunable (e.g. max_par_width)
	CfgString                // a string tunable (e.g. persistence.backend)
	CfgFloat                 // a float tunable (e.g. conscious.activity.*)
)

// CfgRow is one addressable config knob projected for the panel: its dotted path, human label, kind,
// current value (bool On / int / string), whether it is at its default (drives the non-default count),
// and a Regime flag for the durability-affecting knobs the spec wants surfaced (regulator.enforce +
// subconscious.max_par_width).
type CfgRow struct {
	Path     string
	Label    string
	Kind     CfgKind
	On       bool    // for CfgBool
	IntVal   int     // for CfgInt
	StrVal   string  // for CfgString
	FloatVal float64 // for CfgFloat
	Default  bool    // is this knob at its all-on default?
	Regime   bool    // regime-affecting (durability) — flagged in the panel
}

// CfgSection is one config section: its id (the index bucket + the representation special-case), its
// title, and its rows. The Representation section carries Move/Source/Path rows that the panel groups
// into a 3-block grid (keyed off the dotted path prefix).
type CfgSection struct {
	ID    string
	Title string
	Rows  []CfgRow
}

// ConfigView is the whole config picture the panel renders: the sections in rail order, the count of
// non-default toggles, the warnings the last Validate raised (regime flips), and the loaded config
// path. Assembled by the bridge (ConfigView) from the engine's shared HarnessConfig.
type ConfigView struct {
	Sections   []CfgSection
	OffCount   int      // non-default BOOL toggles currently OFF
	NonDefault int      // total knobs not at their default (bools + tunables)
	Warnings   []string // the last Validate() warnings (regime-affecting)
	Path       string   // the loaded config file path ("" == defaults/all-on, none loaded)
}

// cfgIndexW is the fixed width of the left index column (mirrors regIndexW; wide enough for the longest
// section title "Representation" (14) plus the "▸ " caret (2) and a "(n)" count (3+) — 20 holds it).
const cfgIndexW = 20

// RenderConfig composes the Config tab body: the left section index (the selected one accented) and the
// right detail (the selected section's toggles / the representation grid), composed line-by-line with a
// faint divider — the same two-pane shape as RenderRegistry. selRow is the cursor row within the
// section (the app moves it ↑↓); it is highlighted so the user sees what Space/Enter flips. The body is
// FULL height; the app windows it like any tab.
func RenderConfig(cv ConfigView, sel, selRow, width int) string {
	if len(cv.Sections) == 0 {
		return faintStr("(no config — configure a model so the engine builds its config)")
	}
	if sel < 0 {
		sel = 0
	}
	if sel >= len(cv.Sections) {
		sel = len(cv.Sections) - 1
	}
	rightW := width - cfgIndexW - 3 // 3 = " │ " gutter
	if rightW < 16 {
		rightW = 16
	}
	left := cfgIndexLines(cv, sel)
	sec := cv.Sections[sel]
	var right []string
	if sec.ID == "representation" {
		right = cfgReprLines(sec, selRow, rightW)
	} else {
		right = cfgDetailLines(sec, selRow, rightW)
	}

	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	div := txt("│", colFaint).render()
	var out []string
	for i := 0; i < n; i++ {
		l := strings.Repeat(" ", cfgIndexW)
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, l+" "+div+" "+r)
	}
	return strings.Join(out, "\n")
}

// cfgIndexLines renders the left index: a header + the global non-default count, one row per section
// ("▸ Title (k)" selected / "  Title (k)" otherwise, k = that section's non-default count), then the
// key hints. A section with a non-default toggle gets its count in the accent tone so the eye finds
// where a surprising run config lives.
func cfgIndexLines(cv ConfigView, sel int) []string {
	lines := []string{txt(padRight("CONFIG", cfgIndexW), colSubtext).render(), ""}
	for i, s := range cv.Sections {
		caret, c := "  ", colMuted
		if i == sel {
			caret, c = "▸ ", colAccent
		}
		nd := 0
		for _, r := range s.Rows {
			if !r.Default {
				nd++
			}
		}
		count := fmt.Sprintf("(%d)", nd)
		cc := colFaint
		if nd > 0 {
			cc = colWarn // a section holds a non-default toggle — flag it
		}
		// clip the label so the caret + label + count always fit the column (a 2-digit count + a long
		// title would otherwise overflow and shear the two-pane layout).
		label := caret + clip(s.Title, cfgIndexW-lipgloss.Width(caret)-lipgloss.Width(count)-1)
		pad := cfgIndexW - lipgloss.Width(label) - lipgloss.Width(count)
		if pad < 1 {
			pad = 1
		}
		lines = append(lines, txt(label, c).render()+strings.Repeat(" ", pad)+txt(count, cc).render())
	}
	lines = append(lines, "",
		txt(padRight("↑↓ row", cfgIndexW), colFaint).render(),
		txt(padRight("Tab/←→ section", cfgIndexW), colFaint).render(),
		txt(padRight("Space flip", cfgIndexW), colFaint).render(),
		txt(padRight("A all·O off·d def", cfgIndexW), colFaint).render())
	return lines
}

// cfgDetailLines renders a non-representation section's detail: a header (title + a status line with the
// off-count + the loaded path + any regime warnings), then one row per toggle/tunable — the selected
// row caret-marked, [on]/[off] in the ok/faint tones, a tunable's value in text, and a regime flag.
func cfgDetailLines(s CfgSection, selRow, w int) []string {
	lines := cfgHeader(s, w)
	for i, r := range s.Rows {
		lines = append(lines, cfgRowLine(r, i == selRow, w))
	}
	return lines
}

// cfgHeader renders the section header + a status line (off-count, regime note). It is shared by the
// plain and representation detail renderers.
func cfgHeader(s CfgSection, w int) []string {
	head := strings.ToUpper(s.Title)
	var lines []string
	for _, ln := range wrapPlain(head, w, w) {
		lines = append(lines, txt(ln, colSubtext).render())
	}
	off := 0
	for _, r := range s.Rows {
		if r.Kind == CfgBool && !r.On {
			off++
		}
	}
	status := fmt.Sprintf("%d toggle(s) · %d OFF", boolCount(s), off)
	lines = append(lines, txt(clip(status, w), colFaint).render(), "")
	return lines
}

// boolCount counts the bool toggles in a section (the status-line denominator).
func boolCount(s CfgSection) int {
	n := 0
	for _, r := range s.Rows {
		if r.Kind == CfgBool {
			n++
		}
	}
	return n
}

// cfgRowLine renders one toggle/tunable: "▸ Label ········· [on]" (selected caret accent). A bool shows
// [on]/[off] (ok/faint); a tunable shows its value (text). A regime knob carries a faint "· regime"
// suffix so the durability-affecting toggles are visible. A non-default value tints the label warn.
func cfgRowLine(r CfgRow, selected bool, w int) string {
	caret, lc := "  ", colText
	if selected {
		caret, lc = "▸ ", colAccent
	}
	if !r.Default && !selected {
		lc = colWarn
	}
	var value string
	var vc lipgloss.Color
	switch r.Kind {
	case CfgBool:
		if r.On {
			value, vc = "[on]", colOk
		} else {
			value, vc = "[off]", colFaint
		}
	case CfgInt:
		value, vc = fmt.Sprintf("%d", r.IntVal), colText
	case CfgString:
		value, vc = r.StrVal, colText
	case CfgFloat:
		value, vc = fmt.Sprintf("%g", r.FloatVal), colText
	}
	if r.Regime {
		value += " ·regime"
	}
	return leaderRow(caret+r.Label, lc, value, vc, w)
}

// cfgReprLines renders the Representation section as a 3-BLOCK grid: MOVES / SOURCES / PATHS, each block
// a labelled group of its toggles. The selected row caret tracks across all three blocks in row order
// (the app's selRow indexes s.Rows, which the bridge orders moves→sources→paths). This is the §4.6
// "3-block grid" requirement made concrete.
func cfgReprLines(s CfgSection, selRow, w int) []string {
	lines := cfgHeader(s, w)
	blocks := []struct {
		label  string
		prefix string
	}{
		{"moves (the 4 directed steps on the abstraction ladder)", "representation.moves."},
		{"sources (the 5 fuel wells, cheapest-trusted first)", "representation.sources."},
		{"paths (the 3 named traversals)", "representation.paths."},
	}
	for bi, blk := range blocks {
		if bi > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, txt(clip(blk.label, w), colMuted).render())
		for i, r := range s.Rows {
			if !strings.HasPrefix(r.Path, blk.prefix) {
				continue
			}
			lines = append(lines, cfgRowLine(r, i == selRow, w))
		}
	}
	return lines
}
