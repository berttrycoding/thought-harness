// Package popup holds the FOUR modal overlays of the Bubble Tea app (DESIGN §4.4): the Settings
// editor (a), the Command palette (b), the Slash autocomplete (c), and the Confirmation gate (d).
// Each is a small self-contained modal sub-model with Visible()/Show()/Hide() and — for the
// interactive ones — its own Update(msg) (T, tea.Cmd) that the root App delegates to while the popup
// owns the key sink (the modal-priority chain in App.Update runs each Visible() before any global key).
//
// This package is part of the TUI and so MAY import charmbracelet/* (DESIGN §0): it is never imported
// by the headless-pure engine. It reuses the lathe/options modal patterns ported onto the single
// design language (DESIGN §5): foreground-only, NO emoji, DoubleBorder for the centered popups,
// NormalBorder for the inline slash box.
//
// The shared palette + styles for all four popups live here (settings.go is the package's style
// foundation). They are the by-value port of internal/tui/theme.go's Palette — the popup package keeps
// its own copy rather than importing `package tui` to avoid an import cycle (app.go imports popup),
// honouring the same hexes so the design language is identical (DESIGN §5).
package popup

import (
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/llm"

	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// -- the shared palette (the by-value port of theme.py `C` / internal/tui/theme.go, DESIGN §5) ------
//
// Foreground-only, 24-bit hex only (terminals remap the 16-color names). Distinct keys that share a
// hex are kept for the semantic names the renderers read.
const (
	colText    = lipgloss.Color("#e6e9ef") // primary content / the harness's voice
	colTitle   = lipgloss.Color("#ffffff") // titles / the user turn (bold at the use site)
	colSubtext = lipgloss.Color("#b8bcc6") // secondary content, descriptions, section headers
	colMuted   = lipgloss.Color("#8891a0") // de-emphasised labels, hints
	colFaint   = lipgloss.Color("#5b6270") // dimmest still-readable (footer hints, separators)
	colAccent  = lipgloss.Color("#81a1c1") // steel blue: focus / the popup border / the selected row
	colSurface = lipgloss.Color("#3b4452") // panel + inline-box borders
)

// -- the shared styles every popup reuses ----------------------------------------------------------

var (
	// popupBox is the DoubleBorder overlay signature for the three centered popups (Settings, Command,
	// Confirm) — accent border, modal Padding(1, 2). The inline Slash box uses acBox (NormalBorder)
	// instead. Foreground-only — no Background (DESIGN §5).
	popupBox = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colAccent).
			Padding(1, 2)

	// previewBox is the profile-config side panel — a quieter NormalBorder (muted) so it reads as
	// attached detail next to the Settings popup, not a second modal.
	previewBox = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colMuted).
			Padding(1, 2)

	// acBox is the inline autocomplete box for the Slash popup — square NormalBorder, WHITE border (to
	// match every other border), NOT centered (rendered above the input, DESIGN §4.4 row c).
	acBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#ffffff"))

	titleStyle     = lipgloss.NewStyle().Foreground(colTitle)
	emphStyle      = lipgloss.NewStyle().Foreground(colText)
	selStyle       = lipgloss.NewStyle().Foreground(colAccent) // the selected row marker + label
	mutedStyle     = lipgloss.NewStyle().Foreground(colMuted)
	faintStyle     = lipgloss.NewStyle().Foreground(colFaint)
	sectionStyle   = lipgloss.NewStyle().Foreground(colSubtext) // palette category headers
	separatorStyle = lipgloss.NewStyle().Foreground(colSurface)
)

// SettingsValues is the harness-knob config the Settings popup edits, a plain-data view the App maps
// to/from its engine.EngineConfig (the popup never imports engine — plain data flows in, DESIGN §4.3).
// It carries exactly the six knobs DESIGN §4.4 row a lists: mode / seed / cognition / substrate /
// max-ticks / proactive-floor (ported from the Python tui/screens/settings.py knob set).
// ProfileCustom is the picker value when no named profile is active (the raw Features/Mode). Selecting
// it on apply is a no-op — it never overwrites a hand-tuned config with a preset.
const ProfileCustom = "(custom)"

type SettingsValues struct {
	Profile          string  // active cognition profile (config.Profiles()) or ProfileCustom
	Mode             string  // "reactive" | "continuous"
	Seed             int     // seeded RNG
	Cognition        string  // "control" | "llm" | "hybrid"
	Substrate        string  // one of substrateChoices
	MaxTicks         int     // run() budget
	ProactivityFloor float64 // the concluded-line value floor for proactive outreach
}

// knobKind tags how a knob is adjusted by ←/→.
type knobKind int

const (
	knobChoice knobKind = iota // cycle through a fixed option list
	knobInt                    // bump an int by step, clamped
	knobFloat                  // bump a float by step, clamped
)

// substrateChoices is the full thinking-substrate menu the Settings picker cycles — the documented set:
// auto (just pick) · frontier (remote API) · local (LM Studio) · session (the open CC session via MCP) ·
// claude (the headless CC CLI bridge) · test (the offline deterministic double, for UAT). (S7.)
var substrateChoices = llm.SubstrateMenu

// settingKnob is one row of the Settings modal: the label, how it adjusts, its bounds/step, and the
// getter/setter onto a *SettingsValues. The knob list is the single source of truth for both the
// rendered rows and the ←/→ adjustment (mirrors the options app's settingsFields table).
type settingKnob struct {
	label   string
	group   string // section header it sits under ("Mind" / "Engine" / "Advanced"); same-group rows are contiguous
	kind    knobKind
	choices []string                     // knobChoice: the cycle list
	min     float64                      // knobInt/knobFloat: clamp lower
	max     float64                      // knobInt/knobFloat: clamp upper
	step    float64                      // knobInt/knobFloat: bump amount
	get     func(*SettingsValues) string // formatted value for display
	cycle   func(*SettingsValues, int)   // knobChoice: advance by dir (±1)
	bump    func(*SettingsValues, int)   // knobInt/knobFloat: bump by dir*step, clamped

	// visibleIn gates whether the row shows for the current values (nil ⇒ always). Used to hide a knob
	// that does not apply to the active mode — e.g. "step limit" is a reactive-run budget (dead in the
	// endless awake stream), "outreach threshold" is awake-only — so the editor only shows what applies.
	visibleIn func(*SettingsValues) bool
}

// settingKnobs is the authoritative knob table — the six harness knobs from DESIGN §4.4 row a, ported
// from tui/screens/settings.py. Order is the displayed order.
// profileChoices is the picker list: the custom sentinel first (no preset), then every named profile.
var profileChoices = append([]string{ProfileCustom}, config.ProfileNames()...)

var settingKnobs = []settingKnob{
	// — Mind — the cognition shape (the profile owns most of it; mode/proactive shown for tuning).
	{
		label: "profile", group: "Mind", kind: knobChoice, choices: profileChoices,
		get: func(s *SettingsValues) string {
			if s.Profile == "" {
				return ProfileCustom
			}
			return s.Profile
		},
		cycle: func(s *SettingsValues, d int) {
			cur := s.Profile
			if cur == "" {
				cur = ProfileCustom
			}
			s.Profile = cycleChoice(profileChoices, cur, d)
			// Reflect the profile's regime into the live mode so the mode row + the mode-gated rows
			// (outreach threshold / step limit) update immediately, not only after close. (custom) keeps
			// the current mode.
			if p, ok := config.ProfileByName(s.Profile); ok {
				s.Mode = p.Mode
			}
		},
	},
	{
		label: "mode", group: "Mind", kind: knobChoice, choices: []string{"reactive", "continuous"},
		get:   func(s *SettingsValues) string { return config.ModeLabel(s.Mode) }, // shows "awake" for continuous
		cycle: func(s *SettingsValues, d int) { s.Mode = cycleChoice([]string{"reactive", "continuous"}, s.Mode, d) },
	},
	{
		// "outreach threshold": how developed/valuable an idea must be (0..1) before the awake mind
		// messages the user UNPROMPTED. Higher = quieter, lower = chattier. (Internal: ProactivityFloor.)
		// Awake-only — proactive outreach never fires in reactive mode, so hide it there.
		label: "outreach threshold", group: "Mind", kind: knobFloat, min: 0, max: 1, step: 0.05,
		get:       func(s *SettingsValues) string { return fmt.Sprintf("%.2f", s.ProactivityFloor) },
		bump:      func(s *SettingsValues, d int) { s.ProactivityFloor = clampF(s.ProactivityFloor+float64(d)*0.05, 0, 1) },
		visibleIn: func(s *SettingsValues) bool { return s.Mode == "continuous" },
	},
	// — Engine — where/how the mind runs (orthogonal to its shape).
	{
		// "model": the AI model the cognition thinks on (internal: Substrate / the thinking substrate).
		label: "model", group: "Engine", kind: knobChoice, choices: substrateChoices,
		get: func(s *SettingsValues) string { return s.Substrate },
		cycle: func(s *SettingsValues, d int) {
			s.Substrate = cycleChoice(substrateChoices, s.Substrate, d)
		},
	},
	{
		// "decisions": who makes the control decisions — rules (deterministic floor) / AI (the model) /
		// hybrid (floor + model on the hard calls). (Internal: Cognition = control | llm | hybrid.)
		label: "decisions", group: "Engine", kind: knobChoice, choices: []string{"control", "llm", "hybrid"},
		get: func(s *SettingsValues) string { return cognitionLabel(s.Cognition) },
		cycle: func(s *SettingsValues, d int) {
			s.Cognition = cycleChoice([]string{"control", "llm", "hybrid"}, s.Cognition, d)
		},
	},
	// — Advanced — dev/run knobs most users never touch.
	{
		label: "random seed", group: "Advanced", kind: knobInt, min: 0, max: 9999, step: 1,
		get:  func(s *SettingsValues) string { return strconv.Itoa(s.Seed) },
		bump: func(s *SettingsValues, d int) { s.Seed = int(clampF(float64(s.Seed+d), 0, 9999)) },
	},
	{
		// "step limit": the run step budget (one tick = one think-loop step). Mostly reactive/headless;
		// the live awake stream is endless so this barely applies there. (Internal: MaxTicks.)
		// Reactive-only — it is a per-run give-up budget; the endless awake stream ignores it, so hide it.
		label: "step limit", group: "Advanced", kind: knobInt, min: 1, max: 999, step: 5,
		get:       func(s *SettingsValues) string { return strconv.Itoa(s.MaxTicks) },
		bump:      func(s *SettingsValues, d int) { s.MaxTicks = int(clampF(float64(s.MaxTicks)+float64(d)*5, 1, 999)) },
		visibleIn: func(s *SettingsValues) bool { return s.Mode == "reactive" },
	},
}

// cognitionLabel renders the decision-engine value in plain words (the internal enum stays
// control/llm/hybrid): rules = the deterministic floor decides; AI = the model decides; hybrid = both.
func cognitionLabel(c string) string {
	switch c {
	case "control":
		return "rules"
	case "llm":
		return "AI"
	default:
		return c // "hybrid"
	}
}

// cycleChoice returns the next/prev option in opts relative to cur (wrapping). An unknown cur starts
// at index 0.
func cycleChoice(opts []string, cur string, dir int) string {
	idx := 0
	for i, o := range opts {
		if o == cur {
			idx = i
			break
		}
	}
	n := len(opts)
	return opts[((idx+dir)%n+n)%n]
}

// clampF clamps v to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Settings is the Settings modal sub-model (DESIGN §4.4 row a): a DoubleBorder, centered editor of the
// harness knobs with a ▸ on the selected row + footer hints. ↑↓ navigate (wrapping), ←/→ adjust the
// selected knob, Esc/q/Enter close. It edits an in-memory SettingsValues; the App reads Values() on
// close and rebuilds the engine (the Python "Save returns the changes; App applies + rebuilds live").
type Settings struct {
	visible bool
	vals    SettingsValues
	idx     int // the selected knob row
}

// NewSettings creates a hidden Settings modal seeded with the given starting knob values.
func NewSettings(start SettingsValues) Settings {
	return Settings{vals: start}
}

// Visible reports whether the modal is showing (the modal-priority chain in App.Update checks this).
func (s *Settings) Visible() bool { return s.visible }

// Show opens the modal, (re)seeding it with the App's current knob values and resetting the cursor.
func (s *Settings) Show(cur SettingsValues) {
	s.visible = true
	s.vals = cur
	s.idx = 0
}

// Hide closes the modal.
func (s *Settings) Hide() { s.visible = false }

// Values returns the edited knob values (the App reads this on close to rebuild the engine).
func (s *Settings) Values() SettingsValues { return s.vals }

// Update drives the modal while it owns the key sink: ↑↓ navigate (wrapping), ←/→ (or -/+) adjust the
// selected knob, Esc/q/Enter close. Mirrors the options app's handleSettingsKey. It mutates in place
// and returns itself by value (the lathe/options value-receiver modal idiom the App folds back in).
func (s Settings) Update(msg tea.Msg) (Settings, tea.Cmd) {
	if !s.visible {
		return s, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch km.String() {
	case "esc", "q", "enter":
		s.visible = false
	case "up", "k":
		s.step(-1)
	case "down", "j":
		s.step(+1)
	case "left", "h", "-", "_":
		s.adjust(s.idx, -1)
	case "right", "l", "+", "=":
		s.adjust(s.idx, +1)
	}
	return s, nil
}

// knobVisible reports whether the knob at i shows for the current values (nil gate ⇒ always).
func (s *Settings) knobVisible(i int) bool {
	v := settingKnobs[i].visibleIn
	return v == nil || v(&s.vals)
}

// step moves the cursor to the next VISIBLE knob in direction dir (wrapping), so a hidden row (one that
// does not apply to the current mode) is skipped over rather than landed on.
func (s *Settings) step(dir int) {
	n := len(settingKnobs)
	for i := 0; i < n; i++ {
		s.idx = (s.idx + dir + n) % n
		if s.knobVisible(s.idx) {
			return
		}
	}
}

// adjust bumps the knob at idx by dir (±1): a choice knob cycles, an int/float knob bumps-and-clamps.
func (s *Settings) adjust(idx, dir int) {
	k := settingKnobs[idx]
	switch k.kind {
	case knobChoice:
		k.cycle(&s.vals, dir)
	default:
		k.bump(&s.vals, dir)
	}
}

// View renders the centered Settings modal: a DoubleBorder box with the title, the knob rows (▸ on the
// selected, label left-padded to the widest), and the footer hint. Returns the natural-sized box; the
// App lipgloss.Place's it center/center (DESIGN §4.4). Mirrors options' SettingsModal.
func (s *Settings) View() string {
	if !s.visible {
		return ""
	}
	labelW := 0
	for _, k := range settingKnobs {
		if w := lipgloss.Width(k.label); w > labelW {
			labelW = w
		}
	}
	lines := []string{titleStyle.Render("SETTINGS"), ""}
	group := ""
	for i, k := range settingKnobs {
		if !s.knobVisible(i) { // hide a knob that does not apply to the current mode (and its now-empty header)
			continue
		}
		if k.group != group { // a new section: emit its header (Mind / Engine / Advanced)
			if group != "" {
				lines = append(lines, "")
			}
			lines = append(lines, faintStyle.Render(k.group))
			group = k.group
		}
		marker := mutedStyle.Render("  ")
		lbl := mutedStyle.Render(fmt.Sprintf("%-*s", labelW, k.label))
		if i == s.idx {
			marker = selStyle.Render("▸ ")
			lbl = selStyle.Render(fmt.Sprintf("%-*s", labelW, k.label))
		}
		lines = append(lines, "  "+marker+lbl+"   "+emphStyle.Render(k.get(&s.vals)))
	}
	lines = append(lines, "", faintStyle.Render("↑↓ select · ←/→ adjust · Esc close"))
	box := popupBox.Render(strings.Join(lines, "\n"))

	// When the cursor is on the profile row, show a side panel spelling out exactly what the selected
	// profile changes — so "awake" is never an opaque word (the user's request).
	if settingKnobs[s.idx].label == "profile" {
		if panel := s.profilePanel(); panel != "" {
			return lipgloss.JoinHorizontal(lipgloss.Top, box, "  ", panel)
		}
	}
	return box
}

// profilePanel renders the "what this profile does" preview: its loop mode + every knob it flips away
// from the safe baseline. The (custom) sentinel shows a keep-current note instead.
func (s *Settings) profilePanel() string {
	name := s.vals.Profile
	if name == "" || name == ProfileCustom {
		return previewBox.Render(titleStyle.Render("PROFILE") + "\n\n" +
			faintStyle.Render("(custom)\nkeeps the current\nhand-tuned config"))
	}
	p, ok := config.ProfileByName(name)
	if !ok {
		return ""
	}
	out := []string{titleStyle.Render("PROFILE · " + p.Name), ""}
	for _, ln := range wrapWords(p.Desc, 34) {
		out = append(out, faintStyle.Render(ln))
	}
	out = append(out, "", mutedStyle.Render("sets:"))
	changes := p.Changes()
	colW := 0 // align the values into a column at the widest label (capped so the panel stays narrow)
	for _, ch := range changes {
		if w := lipgloss.Width(ch.Label); w > colW {
			colW = w
		}
	}
	if colW > 34 {
		colW = 34
	}
	for _, ch := range changes {
		out = append(out, "  "+mutedStyle.Render(padLabel(ch.Label, colW))+"  "+emphStyle.Render(ch.Value))
	}
	return previewBox.Render(strings.Join(out, "\n"))
}

// padLabel truncates s (rune-aware, with an ellipsis) past w columns, else pads it to w visual columns
// so a column of values lines up regardless of label width.
func padLabel(s string, w int) string {
	if r := []rune(s); len(r) > w {
		return string(r[:w-1]) + "…"
	}
	for lipgloss.Width(s) < w {
		s += " "
	}
	return s
}

// wrapWords soft-wraps s to at most w columns per line, breaking on spaces.
func wrapWords(s string, w int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line == "" {
			line = word
		} else if lipgloss.Width(line)+1+lipgloss.Width(word) <= w {
			line += " " + word
		} else {
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
