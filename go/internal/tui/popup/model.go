package popup

// model.go — the Model picker (an Engine-tab affordance): a centered DoubleBorder list of the local
// LLMs the harness can switch to, the currently-served one marked. ↑↓ navigate, Enter switches (emits
// ModelChosenMsg{Key} the App turns into a load + engine rebuild), Esc closes. Opened by /model (or the
// command palette). Same modal idiom + design language as the Command palette (foreground-only, no
// emoji); it is a plain list (no text filter) since the local model set is small.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ModelChoice is one row of the picker: the model key (also the /v1 id + `lms load` key), a one-line
// detail (params · arch · size), and whether it is currently loaded/served.
type ModelChoice struct {
	Key    string
	Detail string
	Loaded bool
}

// ModelChosenMsg carries the model the user selected (Enter) — its key + display detail. The App opens
// the memory plan for it, then loads it + rebuilds the engine.
type ModelChosenMsg struct {
	Key    string
	Detail string
}

// ModelPicker is the model-switch modal sub-model.
type ModelPicker struct {
	visible  bool
	models   []ModelChoice
	selected int
	offset   int
	width    int
	height   int
}

// NewModelPicker builds a hidden picker.
func NewModelPicker() ModelPicker { return ModelPicker{} }

// Visible reports whether the picker is showing (the modal-priority chain checks this).
func (m *ModelPicker) Visible() bool { return m.visible }

// Show opens the picker over the given model set, selecting the loaded one (else the first).
func (m *ModelPicker) Show(models []ModelChoice) {
	m.visible = true
	m.models = models
	m.selected = 0
	m.offset = 0
	for i, c := range models {
		if c.Loaded {
			m.selected = i
			break
		}
	}
}

// Hide closes the picker.
func (m *ModelPicker) Hide() { m.visible = false }

// SetSize records terminal dimensions (the picker is full-screen centered).
func (m *ModelPicker) SetSize(w, h int) { m.width, m.height = w, h }

// Update drives the picker while it owns the key sink: Esc/q close, ↑↓ navigate, Enter emits the chosen
// model. Value-receiver modal idiom (the App folds it back).
func (m ModelPicker) Update(msg tea.Msg) (ModelPicker, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "esc", "q":
		m.visible = false
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.models)-1 {
			m.selected++
		}
	case "enter":
		if len(m.models) > 0 && m.selected < len(m.models) {
			c := m.models[m.selected]
			m.visible = false
			return m, func() tea.Msg { return ModelChosenMsg{Key: c.Key, Detail: c.Detail} }
		}
		m.visible = false
	}
	return m, nil
}

// View renders the centered picker: title, the model rows (▸ on the selected, "· loaded" on the served
// one), and a nav hint. Returns the natural-sized box; the App lipgloss.Place's it center/center.
func (m *ModelPicker) View() string {
	if !m.visible {
		return ""
	}
	boxWidth := 56
	if m.width > 0 && m.width < boxWidth+10 {
		boxWidth = m.width - 10
	}
	if boxWidth < 30 {
		boxWidth = 30
	}
	maxRows := 16
	if m.height > 0 {
		if r := m.height - 10; r < maxRows {
			maxRows = r
		}
		if maxRows < 3 {
			maxRows = 3
		}
	}

	lines := []string{titleStyle.Render("SWITCH MODEL"), ""}
	if len(m.models) == 0 {
		lines = append(lines, mutedStyle.Render("(no local LLMs found — is LM Studio installed?)"))
	}
	// window the list around the selection.
	m.offset = scrollToCursor(m.selected, m.offset, maxRows)
	end := m.offset + maxRows
	if end > len(m.models) {
		end = len(m.models)
	}
	for i := m.offset; i < end; i++ {
		c := m.models[i]
		cursor, name := "  ", lipgloss.NewStyle().Render(c.Key)
		if i == m.selected {
			cursor, name = "> ", selStyle.Render(c.Key)
		}
		tag := ""
		if c.Loaded {
			tag = selStyle.Render("  · loaded")
		}
		lines = append(lines, cursor+name+tag)
		if c.Detail != "" {
			lines = append(lines, faintStyle.Render("    "+c.Detail))
		}
	}
	lines = append(lines, "", mutedStyle.Render("↑↓ Navigate  Enter Switch  Esc Close"))
	return popupBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}
