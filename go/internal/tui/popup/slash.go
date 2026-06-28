package popup

// slash.go — (c) the Slash autocomplete (DESIGN §4.4 row c), ported from lathe ui/autocomplete.go.
//
// Inline above the input (NOT centered) in a NormalBorder box, up to 8 rows of `> name  desc`, its
// Height() shrinks the viewport budget. Activates implicitly when the input starts with "/", has no
// space, and is not an exact match. Tab/ctrl+n next, Shift+Tab/ctrl+p prev (wrapping); Enter accepts
// the selection; Esc dismisses. Unlike the other three popups this one is NOT a key-sink-owning modal
// driven by a tea.Msg Update — the App re-filters it on every textarea change (Update(input string))
// and asks it for the selection — so its surface mirrors lathe's SlashComplete exactly. DESIGN §4.4.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// maxCompletions is the inline window height (lathe's cap).
const maxCompletions = 8

// Cached styles for the autocomplete rows (built once, not per keystroke — lathe's pattern).
var (
	acSelectedName = lipgloss.NewStyle().Foreground(colAccent)
	acSelectedDesc = lipgloss.NewStyle().Foreground(colSubtext)
	acNormalName   = lipgloss.NewStyle().Foreground(colText)
	acNormalDesc   = lipgloss.NewStyle().Foreground(colMuted)
	acTag          = lipgloss.NewStyle().Foreground(colMuted)
	acEllipsis     = lipgloss.NewStyle().Foreground(colMuted)
)

// CompletionItem is one inline suggestion: the command name, its description, and an optional dim tag.
type CompletionItem struct {
	Name string // e.g. "/settings"
	Desc string // e.g. "Open the settings editor"
	Tag  string // optional source label shown in dim brackets (e.g. "builtin")
}

// CompletionsFromCommands derives the inline completion set from the palette command registry, so the
// slash autocomplete and the command palette share one source of truth (lathe's SlashCmdCompletions).
func CompletionsFromCommands(cmds []CommandItem) []CompletionItem {
	out := make([]CompletionItem, len(cmds))
	for i, c := range cmds {
		out[i] = CompletionItem{Name: c.Name, Desc: c.Desc}
	}
	return out
}

// Slash is the inline slash-autocomplete sub-model (DESIGN §4.4 row c).
type Slash struct {
	visible  bool
	items    []CompletionItem // the full set
	filtered []CompletionItem // matching the current prefix
	selected int
	offset   int // scroll offset for the visible window
	width    int
}

// NewSlash creates an autocomplete over the given items (use CompletionsFromCommands(DefaultCommands)).
func NewSlash(items []CompletionItem) Slash {
	return Slash{items: items}
}

// SetItems replaces the full item list (e.g. after a command-set reload).
func (s *Slash) SetItems(items []CompletionItem) { s.items = items }

// SetWidth records the render width (the box is the input's width, NOT centered).
func (s *Slash) SetWidth(w int) { s.width = w }

// Visible reports whether the popup is showing — true only when active AND there are matches.
func (s *Slash) Visible() bool { return s.visible && len(s.filtered) > 0 }

// Update re-filters from the current input value — call after every textarea change (lathe's
// Update(input string)). It activates only when the input starts with "/" and has no space (the user
// is still typing the command name), name-prefix matches first then desc-substring matches, and hides
// on an exact match. This is the popup's "Show/Hide": there is no separate trigger — the input drives it.
func (s *Slash) Update(input string) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		s.visible = false
		s.filtered = nil
		s.selected = 0
		return
	}

	query := strings.ToLower(input)
	descQuery := strings.ToLower(input[1:]) // strip the leading /

	var nameMatches, descMatches []CompletionItem
	for _, item := range s.items {
		nameLower := strings.ToLower(item.Name)
		if strings.HasPrefix(nameLower, query) {
			nameMatches = append(nameMatches, item)
		} else if descQuery != "" && strings.Contains(strings.ToLower(item.Desc), descQuery) {
			descMatches = append(descMatches, item)
		}
	}
	s.filtered = append(nameMatches, descMatches...)

	// Hide once the user has typed an exact command (nothing left to complete).
	exact := false
	for _, item := range s.filtered {
		if strings.EqualFold(item.Name, input) {
			exact = true
			break
		}
	}
	s.visible = len(s.filtered) > 0 && !exact

	if s.selected >= len(s.filtered) {
		s.selected = max(0, len(s.filtered)-1)
	}
	s.offset = 0
}

// Next moves the selection down (Tab / ctrl+n), wrapping.
func (s *Slash) Next() {
	if len(s.filtered) == 0 {
		return
	}
	s.selected = (s.selected + 1) % len(s.filtered)
	s.scrollToSelected()
}

// Prev moves the selection up (Shift+Tab / ctrl+p), wrapping.
func (s *Slash) Prev() {
	if len(s.filtered) == 0 {
		return
	}
	s.selected--
	if s.selected < 0 {
		s.selected = len(s.filtered) - 1
	}
	s.scrollToSelected()
}

// scrollToSelected adjusts the window so the selection stays visible.
func (s *Slash) scrollToSelected() {
	if s.selected < s.offset {
		s.offset = s.selected
	}
	if s.selected >= s.offset+maxCompletions {
		s.offset = s.selected - maxCompletions + 1
	}
}

// Selected returns the highlighted item's name (what Enter accepts), or "".
func (s *Slash) Selected() string {
	if len(s.filtered) == 0 || s.selected >= len(s.filtered) {
		return ""
	}
	return s.filtered[s.selected].Name
}

// Dismiss hides the popup (Esc) and resets its window.
func (s *Slash) Dismiss() {
	s.visible = false
	s.filtered = nil
	s.selected = 0
	s.offset = 0
}

// Height returns the terminal lines the rendered box occupies (0 when not visible) — calcViewportHeight
// subtracts this so the inline box never overlaps the chat. Counts the visible rows, the two ellipsis
// rows when the window is scrolled, plus the NormalBorder top+bottom (lathe's accounting).
func (s *Slash) Height() int {
	if !s.Visible() {
		return 0
	}
	end := s.offset + maxCompletions
	if end > len(s.filtered) {
		end = len(s.filtered)
	}
	lines := end - s.offset
	if s.offset > 0 {
		lines++ // top ellipsis row
	}
	if end < len(s.filtered) {
		lines++ // bottom ellipsis row
	}
	return lines + 2 // NormalBorder top + bottom
}

// View renders the inline autocomplete box (NormalBorder, surface tone, the input's width) as a compact
// `> name  desc [tag]` list, with `   ...` ellipsis rows when the window is scrolled. Ported from lathe
// autocomplete.go's View. NOT centered — the App composites it above the input box.
func (s *Slash) View() string {
	if !s.Visible() {
		return ""
	}

	end := s.offset + maxCompletions
	if end > len(s.filtered) {
		end = len(s.filtered)
	}
	shown := s.filtered[s.offset:end]

	var sb strings.Builder
	if s.offset > 0 {
		sb.WriteString(acEllipsis.Render("   ...") + "\n")
	}
	for i, item := range shown {
		idx := s.offset + i
		tag := ""
		if item.Tag != "" {
			tag = "  " + acTag.Render("["+item.Tag+"]")
		}
		if idx == s.selected {
			sb.WriteString(" > " + acSelectedName.Render(item.Name) + "  " + acSelectedDesc.Render(item.Desc) + tag)
		} else {
			sb.WriteString("   " + acNormalName.Render(item.Name) + "  " + acNormalDesc.Render(item.Desc) + tag)
		}
		if i < len(shown)-1 {
			sb.WriteString("\n")
		}
	}
	if end < len(s.filtered) {
		sb.WriteString("\n" + acEllipsis.Render("   ..."))
	}

	boxWidth := s.width - 2
	if boxWidth < 20 {
		boxWidth = 60
	}
	return acBox.Width(boxWidth).Render(sb.String())
}
