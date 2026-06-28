package popup

// cogmodels.go — the COGNITION MODELS popup (proposal docs/cognition/
// 2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §6 + §11 Track 1). A modal version-manager
// over the existing persist snapshot machinery: it lists the saved "cognition models" (named brain
// snapshots) and lets the user Save / Load / Set-baseline / Reset-to-baseline / Diff / Delete. The
// self-learning harness's GROWTH is made legible — a cold-baseline plus the named versions grown off it.
//
// HONESTY CONTRACT (mandatory, §6): the popup shows TWO clearly-separated metric classes per row —
//   - STRUCTURAL (exact, free): the real snapshot-diff counts vs the baseline (skills/operators/
//     specialists/beliefs/episodes/… added). Always trustworthy; computed by persist.DiffSnapshots.
//   - CAPABILITY (noisy): a LITERAL placeholder ("needs K-replay"). Capability deltas on the claude
//     substrate are noise-dominated (measured ±56pp) and require a K-replay noise band this panel does
//     NOT compute. Faking a solve-rate/cost number here is the project's thrice-paid lying-green trap —
//     the column EXISTS and is labelled not-yet-measured, never fabricated.
//
// Same modal idiom + design language as the other popups (foreground-only, NO emoji, DoubleBorder,
// box-drawing). This package is part of the TUI and so MAY import charmbracelet/* — it is never
// imported by the headless-pure engine. The popup owns no engine handle: it is a pure view over the
// plain-data rows the App feeds in (the App/bridge own the persist.Store calls off-loop), and it emits
// typed action Msgs the App folds in (mirroring the Model picker / Switch plan idiom).

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ColdBaselineName is the reserved snapshot name the popup treats as the cold-start baseline (dev reset =
// load this version → cold boot; §6 "same load path, no special case"). The App persists which version is
// baseline by this convention name; when no snapshot carries it, the popup falls back to the oldest row.
const ColdBaselineName = "cold-baseline"

// CogModelRow is one row of the version list — a plain-data view the App builds from a persist.SnapshotMeta
// plus the structural diff vs the baseline (persist.DiffSnapshots). The popup never imports persist: the
// App projects the records into these rows so the popup stays a pure renderer (DESIGN §4.3).
type CogModelRow struct {
	Name        string // the snapshot name (the user-given version label)
	Runtime     string // human runtime proxy (rendered from CreatedTick; "tick N" when no wall-time field)
	Substrate   string // substrate provenance tag (claude:sonnet / llm:qwen / test) — lineages never cross-compare
	StructuralΔ string // the EXACT snapshot diff vs the baseline, pre-rendered ("+6 skills +3 spec +41 bel"); "—" on the baseline
	IsBaseline  bool   // true for the row that is the current baseline (marked ★)
}

// the action Msgs the popup emits (Enter / key handlers), folded by the App into the off-loop persist
// calls. Each carries the selected version name (and, for Save, the typed new name).

// CogModelSaveMsg — [s]ave the current live state as a new named snapshot.
type CogModelSaveMsg struct{ Name string }

// CogModelLoadMsg — [l]oad / [r] reset the live state to the selected snapshot (persist.ResetToSnapshot).
// Reset is the dev-reset when Name == ColdBaselineName.
type CogModelLoadMsg struct{ Name string }

// CogModelBaselineMsg — [b] set the selected snapshot as the baseline the structural deltas measure from.
type CogModelBaselineMsg struct{ Name string }

// CogModelDiffMsg — [d] diff the selected snapshot vs the baseline (persist.DiffSnapshots).
type CogModelDiffMsg struct{ From, To string }

// CogModelDeleteMsg — [x] delete the selected snapshot (persist.DeleteSnapshot).
type CogModelDeleteMsg struct{ Name string }

// CogModels is the cognition-model version-manager modal sub-model.
type CogModels struct {
	visible      bool
	rows         []CogModelRow
	baselineName string // which row is the baseline (the [b] target / the diff "from"); "" ⇒ fall back to oldest
	substrate    string // the live substrate tag shown in the header (the running brain's lineage)
	note         string // a transient status line under the list (e.g. the last diff result), "" when none
	selected     int
	offset       int

	// save-name entry mode: [s] opens an inline name prompt; typing edits `nameBuf`, Enter emits the save.
	naming  bool
	nameBuf string

	width  int
	height int
}

// NewCogModels builds a hidden cognition-models popup.
func NewCogModels() CogModels { return CogModels{} }

// Visible reports whether the popup is showing (the modal-priority chain in App.Update checks this).
func (c *CogModels) Visible() bool { return c.visible }

// Show opens the popup over the given version rows, the current baseline name, and the live substrate tag.
// The selection starts on the baseline row (else the first). Any transient note is cleared.
func (c *CogModels) Show(rows []CogModelRow, baselineName, substrate string) {
	c.visible = true
	c.rows = rows
	c.baselineName = baselineName
	c.substrate = substrate
	c.note = ""
	c.naming = false
	c.nameBuf = ""
	c.selected = 0
	c.offset = 0
	for i, r := range rows {
		if r.IsBaseline {
			c.selected = i
			break
		}
	}
}

// SetNote sets the transient status line under the list (e.g. the App reports a completed diff/save here).
// It does not open the popup; the App calls it while the popup is already visible.
func (c *CogModels) SetNote(note string) { c.note = note }

// Note returns the current transient status line (the App preserves it across a post-action refresh).
func (c *CogModels) Note() string { return c.note }

// Hide closes the popup.
func (c *CogModels) Hide() { c.visible = false }

// SetSize records terminal dimensions (the popup is full-screen centered).
func (c *CogModels) SetSize(w, h int) { c.width, c.height = w, h }

// selectedName returns the name of the selected row (empty when there are no rows).
func (c *CogModels) selectedName() string {
	if c.selected >= 0 && c.selected < len(c.rows) {
		return c.rows[c.selected].Name
	}
	return ""
}

// baselineTarget returns the effective baseline name the structural deltas + diff measure from: the
// tracked baselineName when set, else the oldest row (the bottom of the newest-first list), else "".
func (c *CogModels) baselineTarget() string {
	if c.baselineName != "" {
		return c.baselineName
	}
	if n := len(c.rows); n > 0 {
		return c.rows[n-1].Name
	}
	return ""
}

// Update drives the popup while it owns the key sink. In list mode: ↑↓ navigate, [s] open the save-name
// prompt, [l]/[r] load/reset, [b] set baseline, [d] diff vs baseline, [x] delete, Esc/q close. In naming
// mode: typing edits the buffer, Enter emits the save, Esc cancels back to the list. Value-receiver modal
// idiom (the App folds it back).
func (c CogModels) Update(msg tea.Msg) (CogModels, tea.Cmd) {
	if !c.visible {
		return c, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}

	// -- save-name entry mode owns the key sink while open --------------------
	if c.naming {
		switch km.String() {
		case "esc":
			c.naming, c.nameBuf = false, ""
		case "enter":
			name := strings.TrimSpace(c.nameBuf)
			c.naming, c.nameBuf = false, ""
			if name != "" {
				c.note = "saving snapshot: " + name + "…"
				n := name
				return c, func() tea.Msg { return CogModelSaveMsg{Name: n} }
			}
		case "backspace":
			if r := []rune(c.nameBuf); len(r) > 0 {
				c.nameBuf = string(r[:len(r)-1])
			}
		default:
			// accept a single printable rune (a plain name; box-drawing chars / control keys ignored).
			if r := []rune(km.String()); len(r) == 1 && r[0] >= 0x20 {
				c.nameBuf += km.String()
			}
		}
		return c, nil
	}

	// -- list mode ------------------------------------------------------------
	switch km.String() {
	case "esc", "q":
		c.visible = false
	case "up", "k":
		if c.selected > 0 {
			c.selected--
		}
	case "down", "j":
		if c.selected < len(c.rows)-1 {
			c.selected++
		}
	case "s":
		c.naming, c.nameBuf, c.note = true, "", ""
	case "l", "r":
		if name := c.selectedName(); name != "" {
			c.note = "loading " + name + "…"
			n := name
			return c, func() tea.Msg { return CogModelLoadMsg{Name: n} }
		}
	case "b":
		if name := c.selectedName(); name != "" {
			c.baselineName = name // optimistic: the App persists the convention + re-Shows with fresh deltas
			c.note = "baseline → " + name
			n := name
			return c, func() tea.Msg { return CogModelBaselineMsg{Name: n} }
		}
	case "d":
		from := c.baselineTarget()
		to := c.selectedName()
		if from != "" && to != "" {
			c.note = "diffing " + from + " → " + to + "…"
			f, t := from, to
			return c, func() tea.Msg { return CogModelDiffMsg{From: f, To: t} }
		}
	case "x":
		if name := c.selectedName(); name != "" {
			c.note = "deleting " + name + "…"
			n := name
			return c, func() tea.Msg { return CogModelDeleteMsg{Name: n} }
		}
	}
	return c, nil
}

// View renders the centered popup: the title + the live substrate tag, the two-class column header, one
// row per version (★ on the baseline, the EXACT structural delta, the "needs K-replay" capability
// placeholder, the substrate tag), the transient note, and the action legend. Returns the natural-sized
// box; the App lipgloss.Place's it center/center.
func (c *CogModels) View() string {
	if !c.visible {
		return ""
	}
	boxWidth := 80
	if c.width > 0 && c.width < boxWidth+8 {
		boxWidth = c.width - 8
	}
	if boxWidth < 48 {
		boxWidth = 48
	}
	maxRows := 14
	if c.height > 0 {
		if r := c.height - 12; r < maxRows {
			maxRows = r
		}
		if maxRows < 3 {
			maxRows = 3
		}
	}

	// header: title left, the running brain's substrate lineage right (never cross-compare blindly).
	title := titleStyle.Render("COGNITION MODELS")
	sub := mutedStyle.Render("substrate: " + dashIfEmpty(c.substrate))
	header := title + "   " + sub

	lines := []string{header, separatorStyle.Render(strings.Repeat("─", boxWidth-6))}

	// the two-class column header — the separation is the honesty contract made visible (§6).
	colHead := sectionStyle.Render(
		pad("name", 18) + pad("runtime", 11) + pad("Δ structural (exact)", 26) + "Δ capability (noisy)")
	lines = append(lines, colHead)

	if len(c.rows) == 0 {
		lines = append(lines, "", mutedStyle.Render("(no saved cognition models yet — press s to save the current brain)"))
	}

	// window the list around the selection.
	c.offset = scrollToCursor(c.selected, c.offset, maxRows)
	end := c.offset + maxRows
	if end > len(c.rows) {
		end = len(c.rows)
	}
	baseTarget := c.baselineTarget()
	for i := c.offset; i < end; i++ {
		r := c.rows[i]
		cursor := "  "
		nameStyle := lipgloss.NewStyle().Foreground(colText)
		if i == c.selected {
			cursor, nameStyle = "> ", selStyle
		}
		star := "  "
		if r.IsBaseline || (c.baselineName == "" && r.Name == baseTarget) {
			star = selStyle.Render("★ ")
		}
		struct_ := dashIfEmpty(r.StructuralΔ)
		// CAPABILITY column: the literal not-yet-measured placeholder — NEVER a fabricated number (the
		// claude substrate's ±56pp noise needs a K-replay band this panel does not compute).
		cap := faintStyle.Render("needs K-replay")
		row := cursor + star + nameStyle.Render(pad(r.Name, 16)) +
			mutedStyle.Render(pad(r.Runtime, 11)) +
			emphStyle.Render(pad(struct_, 26)) + cap
		lines = append(lines, row)
		// the per-row substrate lineage tag (claude-grown vs other), faint under the row.
		lines = append(lines, faintStyle.Render("      "+dashIfEmpty(r.Substrate)))
	}

	// the inline save-name prompt (replaces the legend while naming).
	if c.naming {
		lines = append(lines, "", emphStyle.Render("save current brain as: ")+selStyle.Render(c.nameBuf+"_"))
		lines = append(lines, mutedStyle.Render("Enter save · Esc cancel"))
		return popupBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
	}

	if c.note != "" {
		lines = append(lines, "", emphStyle.Render(c.note))
	}
	lines = append(lines,
		separatorStyle.Render(strings.Repeat("─", boxWidth-6)),
		mutedStyle.Render("[s]ave  [l]oad/[r]eset  [b]aseline  [d]iff vs baseline  [x]delete  ↑↓ select  Esc close"))
	return popupBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}

// pad left-justifies s into a width-w cell (runes, not bytes), truncating with an ellipsis when too long.
func pad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w <= 1 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}

// dashIfEmpty renders an em-dash for an empty field (the baseline's structural delta, a missing substrate).
func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
