package popup

// switchplan.go — the model-switch PLAN screen (shown after picking a model in the picker): it lays out
// what is currently loaded + the target's memory estimate, and lets the user choose which resident
// models to UNLOAD to make room (the "ask me each time" policy). The embedding model is always kept (it
// is tiny and retrieval needs it). Space toggles a row, Enter applies (emits SwitchPlanMsg{Target,
// Unload}), Esc cancels. Same modal idiom + design language as the other popups.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LoadedRow is one currently-resident model on the plan screen: its unload identifier, a display label
// (key + size), whether it is the embedder (kept, not toggleable), and whether it is checked to unload.
type LoadedRow struct {
	ID       string
	Label    string
	Embedder bool
	Unload   bool
}

// SwitchPlanMsg is emitted on Enter: switch to Target, unloading the chosen resident models first.
type SwitchPlanMsg struct {
	Target string
	Unload []string
}

// SwitchPlan is the plan modal sub-model.
type SwitchPlan struct {
	visible      bool
	target       string
	targetDetail string
	note         string // a fit warning ("may not fit…"), shown in the warn tone
	rows         []LoadedRow
	selected     int
	width        int
	height       int
}

// NewSwitchPlan builds a hidden plan modal.
func NewSwitchPlan() SwitchPlan { return SwitchPlan{} }

// Visible reports whether the plan is showing.
func (s *SwitchPlan) Visible() bool { return s.visible }

// Show opens the plan for a target model + its detail/fit note over the current loaded set. The cursor
// starts on the first toggleable (non-embedder) row.
func (s *SwitchPlan) Show(target, targetDetail, note string, rows []LoadedRow) {
	s.visible = true
	s.target, s.targetDetail, s.note, s.rows = target, targetDetail, note, rows
	s.selected = 0
	for i, r := range rows {
		if !r.Embedder {
			s.selected = i
			break
		}
	}
}

// Hide closes the plan.
func (s *SwitchPlan) Hide() { s.visible = false }

// SetSize records terminal dimensions (the plan is centered).
func (s *SwitchPlan) SetSize(w, h int) { s.width, s.height = w, h }

// Update drives the plan: ↑↓ navigate, space toggles the selected non-embedder row, Enter applies, Esc
// cancels. Value-receiver modal idiom.
func (s SwitchPlan) Update(msg tea.Msg) (SwitchPlan, tea.Cmd) {
	if !s.visible {
		return s, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch km.String() {
	case "esc", "q":
		s.visible = false
	case "up", "k":
		if s.selected > 0 {
			s.selected--
		}
	case "down", "j":
		if s.selected < len(s.rows)-1 {
			s.selected++
		}
	case " ", "space", "x":
		if s.selected < len(s.rows) && !s.rows[s.selected].Embedder {
			s.rows[s.selected].Unload = !s.rows[s.selected].Unload
		}
	case "enter":
		s.visible = false
		var unload []string
		for _, r := range s.rows {
			if r.Unload && !r.Embedder {
				unload = append(unload, r.ID)
			}
		}
		target := s.target
		return s, func() tea.Msg { return SwitchPlanMsg{Target: target, Unload: unload} }
	}
	return s, nil
}

// View renders the centered plan: the target + its estimate, an optional fit warning, the loaded-model
// rows ([x]/[ ] toggle, the embedder shown "kept"), and the freed-memory hint + nav keys.
func (s *SwitchPlan) View() string {
	if !s.visible {
		return ""
	}
	boxWidth := 60
	if s.width > 0 && s.width < boxWidth+10 {
		boxWidth = s.width - 10
	}
	if boxWidth < 34 {
		boxWidth = 34
	}

	lines := []string{titleStyle.Render("SWITCH MODEL — MEMORY PLAN"), ""}
	lines = append(lines, emphStyle.Render("load: ")+selStyle.Render(s.target))
	if s.targetDetail != "" {
		lines = append(lines, faintStyle.Render("      "+s.targetDetail))
	}
	if s.note != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#ebcb8b")).Render("      ! "+s.note))
	}
	lines = append(lines, "", sectionStyle.Render("currently loaded — choose what to unload:"))
	if len(s.rows) == 0 {
		lines = append(lines, mutedStyle.Render("  (nothing else loaded)"))
	}
	for i, r := range s.rows {
		cursor := "  "
		if i == s.selected {
			cursor = "> "
		}
		var box, label string
		switch {
		case r.Embedder:
			box = faintStyle.Render("[kept] ")
			label = mutedStyle.Render(r.Label + "  (retrieval)")
		case r.Unload:
			box = selStyle.Render("[x] ")
			label = emphStyle.Render(r.Label)
		default:
			box = mutedStyle.Render("[ ] ")
			label = mutedStyle.Render(r.Label)
		}
		lines = append(lines, cursor+box+label)
	}
	lines = append(lines, "", mutedStyle.Render("↑↓ move · space toggle · Enter apply · Esc cancel"))
	return popupBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
}
