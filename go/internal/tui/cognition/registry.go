// registry.go — the REGISTRY browser view (the 6th COGNITION tab). Unlike the other panels, which are
// LIVE views of "what the engine is doing this tick", this is a STATIC reference view of "what is in
// the engine's catalogs": the operators, sub-agents, skills, workflows, tools, prompts, and memory
// primitives. It is a left INDEX of registries + a right DETAIL pane of the selected registry's
// entries — one surface for the whole capability inventory.
//
// The data is plain strings (RegistryCatalog), assembled by the bridge from the engine's read-only
// registry accessors (the same translation discipline as SnapshotData) — so this file stays a pure
// view: no engine imports, no behaviour, just layout. The render is contents-agnostic: it shows
// today's catalogs now and the redesigned ones (docs/internal/design/registry-redesign.md) after they land.
package cognition

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// RegEntry is one registered item: its name, a one-line detail, optional trailing tags (tool-scope,
// triggers, a status word), and flags that tint it (minted = accent; Status drives present/idea tones).
type RegEntry struct {
	Name   string
	Detail string   // one-line description / intent
	Tags   []string // small trailing tags, e.g. tool-scope or trigger words
	Minted bool     // synthesised at runtime (vs a seed) — rendered in the accent tone
	Status string   // "" | "present" | "partial" | "idea" — colours the entry (memory primitives)
}

// RegGroup is a labelled sub-group within a section (an operator family, a skill tier, "tool-backed").
type RegGroup struct {
	Label   string
	Entries []RegEntry
}

// RegSection is one registry: its id (the index bucket), title, a live count, a one-line note, and the
// grouped entries shown in the detail pane.
type RegSection struct {
	ID     string
	Title  string
	Count  int
	Note   string
	Groups []RegGroup
}

// RegistryCatalog is the whole inventory — one section per registry, in index order. Assembled by the
// bridge (BuildRegistryCatalog) from the engine's registry accessors.
type RegistryCatalog struct {
	Sections []RegSection
}

// regIndexW is the fixed width of the left index column (label + count + the divider gutter sits to its
// right). Wide enough for "Sub-agents   (12)" plus the selection caret.
const regIndexW = 22

// RenderRegistry composes the registry tab body: the left index (one row per section, the selected one
// accented) and the right detail (the selected section's groups + entries, wrapped). The two are
// composed line-by-line with a faint vertical divider so the index stays put while the detail is the
// tall, scrolling pane. The returned body is FULL height; the app windows it (windowGrid) like any tab.
func RenderRegistry(cat RegistryCatalog, sel, width int) string {
	if len(cat.Sections) == 0 {
		return faintStr("(no registries — configure a model so the engine builds its catalogs)")
	}
	if sel < 0 {
		sel = 0
	}
	if sel >= len(cat.Sections) {
		sel = len(cat.Sections) - 1
	}
	rightW := width - regIndexW - 3 // 3 = " │ " gutter
	if rightW < 16 {
		rightW = 16
	}
	left := regIndexLines(cat, sel)
	right := regDetailLines(cat.Sections[sel], rightW)

	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	div := txt("│", colFaint).render()
	var out []string
	for i := 0; i < n; i++ {
		l := strings.Repeat(" ", regIndexW)
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

// regIndexLines renders the left index: a header, then one row per section — "▸ Title (n)" for the
// selected section (accent), "  Title (n)" for the rest (muted). Each line is padded to regIndexW so the
// divider column lines up.
func regIndexLines(cat RegistryCatalog, sel int) []string {
	lines := []string{txt(padRight("REGISTRIES", regIndexW), colSubtext).render(), ""}
	for i, s := range cat.Sections {
		caret, c := "  ", colMuted
		if i == sel {
			caret, c = "▸ ", colAccent
		}
		count := fmt.Sprintf("(%d)", s.Count)
		label := caret + s.Title
		// pad the label so the count right-aligns within the index column.
		pad := regIndexW - lipgloss.Width(label) - lipgloss.Width(count)
		if pad < 1 {
			pad = 1
		}
		row := txt(label, c).render() + strings.Repeat(" ", pad) + txt(count, colFaint).render()
		lines = append(lines, row)
	}
	lines = append(lines, "", txt(padRight("↑↓ pick", regIndexW), colFaint).render(),
		txt(padRight("PgUp/Dn scroll", regIndexW), colFaint).render())
	return lines
}

// regDetailLines renders the right detail pane for one section: a header (title · count + note), then
// each group (a faint label) and its entries (name column + wrapped detail + trailing tags).
func regDetailLines(s RegSection, w int) []string {
	head := strings.ToUpper(s.Title)
	if s.Note != "" {
		head += " · " + s.Note
	}
	lines := []string{}
	for _, ln := range wrapPlain(head, w, w) {
		lines = append(lines, txt(ln, colSubtext).render())
	}
	lines = append(lines, "")

	for _, g := range s.Groups {
		if g.Label != "" {
			lines = append(lines, txt(clip(g.Label, w), colMuted).render())
		}
		for _, e := range g.Entries {
			lines = append(lines, regEntryLines(e, w)...)
		}
		lines = append(lines, "")
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// regEntryLines renders one entry: "  name            detail…" with the detail wrapped under a hanging
// indent, then any tags on a faint continuation line. The name tone is accent (minted) / status-tinted
// / plain.
func regEntryLines(e RegEntry, w int) []string {
	const nameW = 18
	nameColor := colText
	switch {
	case e.Minted:
		nameColor = colAccent
	case e.Status == "present":
		nameColor = colOk
	case e.Status == "partial":
		nameColor = colWarn
	case e.Status == "idea":
		nameColor = colFaint
	}
	name := e.Name
	if e.Minted {
		name += "*"
	}
	prefix := txt("  "+padRight(clip(name, nameW), nameW)+" ", nameColor).render()
	out := wrapEntry(prefix, nameW+3, e.Detail, colSubtext, w)
	if len(e.Tags) > 0 {
		tag := "    " + strings.Join(e.Tags, " ")
		out = append(out, txt(ansi.Truncate(tag, w, "…"), colFaint).render())
	}
	return out
}
