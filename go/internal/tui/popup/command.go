package popup

// command.go — (b) the Command palette (DESIGN §4.4 row b), ported from lathe ui/palette.go.
//
// An embedded bubbles textinput, full-screen centered DoubleBorder modal, a category-grouped command
// list, filtered by command-NAME substring only (never description/category — avoids "ex" matching
// "execution" in a desc, lathe's note), a fixed-height description pane below a ─ rule, and a nav hint.
// Enter emits a PaletteResultMsg{Command} the App routes through its slash-command handler; Esc/ctrl+k
// hide. Trigger: ctrl+k anywhere (works during a step). DESIGN §4.4.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PaletteResultMsg carries the command name chosen from the palette (Enter). The App routes it through
// handleSlashCommand. Exported so internal/tui/app.go can type-switch on it (it mirrors the package-
// internal paletteResultMsg the base already defines in msgs.go — the popup emits the carrier).
type PaletteResultMsg struct {
	Command string
}

// CommandItem is one palette entry: the command name, its one-line description, and its category (the
// palette groups by category). This is the popup's command registry shape (a slash-registry analog).
type CommandItem struct {
	Name     string // includes the leading slash, e.g. "/settings"
	Desc     string // one-line description shown in the desc pane
	Category string // groups commands: General / Session / Engine
}

// DefaultCommands is the authoritative palette command list for the harness — the slash commands the
// App's handler dispatches. (The App may override via NewCommand(items) after wiring its real set.)
var DefaultCommands = []CommandItem{
	// General
	{"/help", "Show available commands and keyboard shortcuts", "General"},
	{"/settings", "Open the settings editor (profile, mode, model, decisions, …)", "General"},
	{"/clear", "Clear the conversation history and start fresh", "General"},
	{"/exit", "Quit the application", "General"},
	// Session
	{"/reset", "Reset the engine: discard the session and rebuild from the current config", "Session"},
	{"/mode", "Toggle the loop mode (reactive ↔ awake)", "Session"},
	// Engine
	{"/model", "Manage LOCAL models (LM Studio) — pick from the downloaded models, load + rebuild live", "Engine"},
	{"/models", "Cognition models — saved brain snapshots: save / load / reset / set-baseline / diff", "Engine"},
	{"/doctor", "Probe each LLM-backed subsystem and report OK / parse-fail / fallback", "Engine"},
	{"/stability", "Re-run the control-theoretic durability checks (n<1, U≤1, 0<K·g<2, …)", "Engine"},
	{"/substrate", "Switch which model backend it thinks on (auto / frontier / local / claude / session)", "Engine"},
}

// paletteCategoryIndex fixes the category sort order (lathe's pattern).
var paletteCategoryIndex = map[string]int{
	"General": 0,
	"Session": 1,
	"Engine":  2,
}

// sortByCategory orders items by category (stable within a category) so the grouped list reads in a
// fixed order regardless of registry insertion order.
func sortByCategory(items []CommandItem) {
	sort.SliceStable(items, func(i, j int) bool {
		return paletteCategoryIndex[items[i].Category] < paletteCategoryIndex[items[j].Category]
	})
}

// Command is the Command palette sub-model (DESIGN §4.4 row b).
type Command struct {
	visible  bool
	input    textinput.Model
	items    []CommandItem // the full command set (category-sorted)
	filtered []CommandItem // the current name-substring matches
	selected int
	offset   int // scroll offset for the list window
	width    int
	height   int
}

// NewCommand builds a Command palette over the given command set (use DefaultCommands for the harness
// default). The items are category-sorted up front so the grouped view is stable.
func NewCommand(items []CommandItem) Command {
	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.CharLimit = 50
	sorted := append([]CommandItem(nil), items...)
	sortByCategory(sorted)
	return Command{
		input:    ti,
		items:    sorted,
		filtered: sorted,
	}
}

// Visible reports whether the palette is showing.
func (c *Command) Visible() bool { return c.visible }

// Show opens the palette: clears + focuses the input, resets the filter to the full list.
func (c *Command) Show() {
	c.visible = true
	c.input.Reset()
	c.input.Focus()
	c.filtered = c.items
	c.selected = 0
	c.offset = 0
}

// Hide closes the palette and blurs the input.
func (c *Command) Hide() {
	c.visible = false
	c.input.Blur()
}

// SetSize records the terminal dimensions (the palette is full-screen centered; it sizes its box and
// list window off these).
func (c *Command) SetSize(w, h int) {
	c.width = w
	c.height = h
}

// Update drives the palette while it owns the key sink: Esc/ctrl+k hide, Enter emits the selected
// command, ↑↓ navigate, all other keys feed the textinput and re-filter by command-name substring.
// Ported from lathe palette.go's Update (value-receiver modal idiom).
func (c Command) Update(msg tea.Msg) (Command, tea.Cmd) {
	if !c.visible {
		return c, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.Type {
		case tea.KeyEscape, tea.KeyCtrlK:
			c.visible = false
			return c, nil
		case tea.KeyEnter:
			name := ""
			if len(c.filtered) > 0 && c.selected < len(c.filtered) {
				name = c.filtered[c.selected].Name
			}
			c.visible = false
			if name != "" {
				return c, func() tea.Msg { return PaletteResultMsg{Command: name} }
			}
			return c, nil
		case tea.KeyUp:
			if c.selected > 0 {
				c.selected--
			}
			return c, nil
		case tea.KeyDown:
			if c.selected < len(c.filtered)-1 {
				c.selected++
			}
			return c, nil
		}
	}

	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)

	// Filter by command NAME substring only (lathe's discipline — never desc/category).
	query := strings.ToLower(c.input.Value())
	if query == "" {
		c.filtered = c.items
	} else {
		c.filtered = nil
		for _, item := range c.items {
			if strings.Contains(strings.ToLower(item.Name), query) {
				c.filtered = append(c.filtered, item)
			}
		}
	}
	c.selected = 0
	c.offset = 0
	return c, cmd
}

// paletteRow is a display row: a category header, a blank spacer before a header, or an item.
type paletteRow struct {
	isHeader bool
	isSpacer bool
	category string
	itemIdx  int // index into filtered (non-header rows)
}

// buildRows groups filtered items by category with a spacer between groups (lathe's buildRows).
func buildRows(filtered []CommandItem) []paletteRow {
	var rows []paletteRow
	last := ""
	for i, item := range filtered {
		if item.Category != last {
			if last != "" {
				rows = append(rows, paletteRow{isSpacer: true})
			}
			rows = append(rows, paletteRow{isHeader: true, category: item.Category})
			last = item.Category
		}
		rows = append(rows, paletteRow{itemIdx: i})
	}
	return rows
}

// descAreaHeight fixes the description pane height so the modal does not resize as descriptions vary.
const descAreaHeight = 2

// View renders the full-screen centered palette: title, input, optional match-count line, the
// (grouped or flat) command list, a ─ rule, the fixed-height description pane, and the nav hint.
// Returns the natural-sized box; the App lipgloss.Place's it center/center (DESIGN §4.4). Ported from
// lathe palette.go's View.
func (c *Command) View() string {
	if !c.visible {
		return ""
	}

	boxWidth := 50
	if c.width > 0 && c.width < boxWidth+10 {
		boxWidth = c.width - 10
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	innerWidth := boxWidth - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	// Max list rows = height minus the fixed chrome (lathe's reservation: 12 + descAreaHeight).
	maxRows := 20
	if c.height > 0 {
		maxRows = c.height - 12 - descAreaHeight
		if maxRows < 3 {
			maxRows = 3
		}
	}

	isFiltering := c.input.Value() != ""
	var list strings.Builder

	if isFiltering {
		// Flat list when filtering.
		visible := maxRows
		if visible > len(c.filtered) {
			visible = len(c.filtered)
		}
		c.offset = scrollToCursor(c.selected, c.offset, visible)
		end := c.offset + visible
		if end > len(c.filtered) {
			end = len(c.filtered)
		}
		for i := c.offset; i < end; i++ {
			list.WriteString(renderItemRow(c.filtered[i].Name, i == c.selected))
		}
	} else {
		// Grouped by category.
		rows := buildRows(c.filtered)
		selectedRow := 0
		for ri, r := range rows {
			if !r.isHeader && !r.isSpacer && r.itemIdx == c.selected {
				selectedRow = ri
				break
			}
		}
		visible := maxRows
		if visible > len(rows) {
			visible = len(rows)
		}
		c.offset = scrollRowsToSelection(rows, selectedRow, c.offset, visible)
		end := c.offset + visible
		if end > len(rows) {
			end = len(rows)
		}
		for ri := c.offset; ri < end; ri++ {
			r := rows[ri]
			switch {
			case r.isSpacer:
				list.WriteString("\n")
			case r.isHeader:
				list.WriteString(sectionStyle.Render(r.category) + "\n")
			default:
				list.WriteString(renderItemRow(c.filtered[r.itemIdx].Name, r.itemIdx == c.selected))
			}
		}
	}

	// Fixed-height description pane below a ─ rule.
	separator := separatorStyle.Render(strings.Repeat("─", innerWidth))
	descText := ""
	if len(c.filtered) > 0 && c.selected < len(c.filtered) {
		descText = c.filtered[c.selected].Desc
	}
	descBlock := lipgloss.NewStyle().Width(innerWidth).Height(descAreaHeight).Render(mutedStyle.Render(descText))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Command Palette") + "\n\n")
	sb.WriteString(c.input.View() + "\n")
	// Match-count line — one line in both states so the modal height stays stable.
	if isFiltering {
		sb.WriteString(mutedStyle.Render(fmt.Sprintf("showing %d of %d", len(c.filtered), len(c.items))) + "\n")
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString(list.String())
	sb.WriteString("\n" + separator + "\n")
	sb.WriteString(descBlock + "\n")
	sb.WriteString("\n" + mutedStyle.Render("↑↓ Navigate  Enter Select  Esc Close"))

	return popupBox.Width(boxWidth).Render(sb.String())
}

// renderItemRow formats one command row: a "> " cursor + bold name when selected (accent), a "  " gutter
// + bold name otherwise.
func renderItemRow(name string, selected bool) string {
	cursor := "  "
	styled := lipgloss.NewStyle().Render(name)
	if selected {
		cursor = "> "
		styled = lipgloss.NewStyle().Foreground(colAccent).Render(name)
	}
	return cursor + styled + "\n"
}

// scrollToCursor keeps the cursor within a flat window of size visible, returning the adjusted offset
// (lathe's ScrollToCursor).
func scrollToCursor(cursor, offset, visible int) int {
	if visible <= 0 {
		return 0
	}
	if cursor < offset {
		return cursor
	}
	if cursor >= offset+visible {
		return cursor - visible + 1
	}
	return offset
}

// scrollRowsToSelection keeps the selected row (and, where possible, its category header) within a
// window of size visible (lathe's ScrollRowsToSelection, simplified: anchor the selected row, and pull
// the preceding header into view when it would otherwise scroll off the top).
func scrollRowsToSelection(rows []paletteRow, selectedRow, offset, visible int) int {
	if visible <= 0 {
		return 0
	}
	if selectedRow < offset {
		offset = selectedRow
	}
	if selectedRow >= offset+visible {
		offset = selectedRow - visible + 1
	}
	// Pull the preceding category header into view if it sits just above the window.
	if offset > 0 && rows[offset].isSpacer {
		offset++
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}
