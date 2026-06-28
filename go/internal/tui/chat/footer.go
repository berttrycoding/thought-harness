package chat

// footer.go — the FooterBar (DESIGN §4.2 item 3, lathe ui/footer.go). Three lines around the input
// slot: the IDENTITY line ABOVE the input box (mode + model + branch), then the input box itself
// (owned by app.go), then the STATS line and the WORKSPACE cwd BELOW. The footer is a pure renderer:
// app.go composites ViewIdentity() above and ViewStats()+ViewWorkspace() below its textarea.
//
// Foreground-only styling sourced from the single Palette (DESIGN §5). No Background — the footer is
// plain text on the screen base, not a solid chrome bar.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Footer styles — all foreground only, no Background() (DESIGN §5). Sourced from the leaf-local
// palette (palette.go) so this package never imports the root tui package back (import-cycle).
var (
	ftLabel   = lipgloss.NewStyle().Foreground(pal.Muted)   // "tools:", "branch:" — dim labels
	ftStat    = lipgloss.NewStyle().Foreground(pal.Subtext) // stat values
	ftModel   = lipgloss.NewStyle().Foreground(pal.Accent)
	ftBranch  = lipgloss.NewStyle().Foreground(pal.Status)
	ftSep     = lipgloss.NewStyle().Foreground(pal.Faint) // the " | " stat separator
	ftDim     = lipgloss.NewStyle().Foreground(pal.Faint) // "mode", "[Shift+Tab]" suffixes
	ftThink   = lipgloss.NewStyle().Foreground(pal.Accent)
	ftReactiv = lipgloss.NewStyle().Foreground(pal.Ok)   // reactive (episodic) mode value
	ftContin  = lipgloss.NewStyle().Foreground(pal.Warn) // continuous (awake) mode value
	ftPath    = lipgloss.NewStyle().Foreground(pal.Subtext)
)

// FooterBar renders the identity line above the input and the stats + workspace lines below it. It
// holds only display state; app.go pushes updates through the setters each frame / on engine events.
type FooterBar struct {
	width    int
	mode     string // "reactive" | "continuous" (engine loop regime)
	model    string // active model label (or backend label when no model)
	branch   string // git branch / session label, right-anchored on the identity line
	workDir  string // workspace cwd, middle-truncated on the workspace line
	thinking bool   // true while a step is in flight — colours the mode value

	toolCount  int
	skillCount int
	agentCount int
	msgCount   int
	stepsRun   int
}

// NewFooterBar builds a footer bar with the static identity fields (mode, model, branch). Counts and
// the workspace are pushed in later via the setters as the engine wires up.
func NewFooterBar(mode, model, branch string) FooterBar {
	return FooterBar{mode: mode, model: model, branch: branch}
}

// --- Setters (single-writer: app.go calls these on the Update loop) ---

func (f *FooterBar) SetWidth(w int)      { f.width = w }
func (f *FooterBar) SetMode(m string)    { f.mode = m }
func (f *FooterBar) SetModel(m string)   { f.model = m }
func (f *FooterBar) SetBranch(b string)  { f.branch = b }
func (f *FooterBar) SetWorkDir(d string) { f.workDir = d }
func (f *FooterBar) SetThinking(v bool)  { f.thinking = v }
func (f *FooterBar) SetToolCount(n int)  { f.toolCount = n }
func (f *FooterBar) SetSkillCount(n int) { f.skillCount = n }
func (f *FooterBar) SetAgentCount(n int) { f.agentCount = n }
func (f *FooterBar) IncrementMessages()  { f.msgCount++ }
func (f *FooterBar) SetMsgCount(n int)   { f.msgCount = n }
func (f *FooterBar) IncrementSteps()     { f.stepsRun++ }
func (f *FooterBar) SetStepsRun(n int)   { f.stepsRun = n }

// --- Getters (the unified chrome header/footer in package tui reads identity/stats from here) ---

func (f FooterBar) Mode() string   { return f.mode }
func (f FooterBar) Model() string  { return f.model }
func (f FooterBar) Branch() string { return f.branch }
func (f FooterBar) Thinking() bool { return f.thinking }
func (f FooterBar) MsgCount() int  { return f.msgCount }
func (f FooterBar) StepsRun() int  { return f.stepsRun }

// ViewHeight is the total lines the footer occupies around the input (identity above + stats +
// workspace below). app.go subtracts this from the viewport budget.
func (f FooterBar) ViewHeight() int { return 3 }

// ViewIdentity renders the line ABOVE the input box:
//
//	reactive mode [Shift+Tab]   qwen2.5-coder                          branch:main
//
// The mode value is coloured per regime (ok=reactive, warn=continuous), with a dim "mode" +
// "[Shift+Tab]" suffix. The model is accent-bold; the branch is right-anchored. While a step is in
// flight the mode value is replaced by an accent "thinking…" cue.
func (f FooterBar) ViewIdentity() string {
	w := f.width
	if w < 10 {
		w = 80
	}

	// Mode indicator (or the thinking cue while a step is off-loop).
	var modeVal string
	if f.thinking {
		modeVal = ftThink.Render("thinking…")
	} else if f.mode == "continuous" {
		modeVal = ftContin.Render("awake") // product vocabulary: "continuous" is the internal enum; the user sees "awake"
	} else {
		modeVal = ftReactiv.Render("reactive")
	}
	modeStr := " " + modeVal + " " + ftDim.Render("mode") + " " + ftDim.Render("[Shift+Tab]")

	// Model name (or a "no model loaded" placeholder).
	model := f.model
	if model == "" {
		model = "no model loaded"
	}
	modelStr := ftModel.Render(model)

	left := modeStr + "   " + modelStr

	// Branch, right-anchored with its label.
	right := ""
	if f.branch != "" {
		right = ftLabel.Render("branch:") + ftBranch.Render(f.branch)
	}

	// Narrow fallback: drop the branch first, then collapse the left to the mode alone.
	if lipgloss.Width(left)+lipgloss.Width(right)+3 > w {
		right = ""
	}
	if lipgloss.Width(left)+3 > w {
		left = modeStr
	}

	return spaceFill(left, right, w)
}

// ViewStats renders the line BELOW the input box:
//
//	tools:14 | skills:6 | agents:2 | msgs:12 | steps:30
//
// Items are pipe-separated; zero-valued items are omitted. At narrow widths items drop from the end
// until the line fits (lathe progressive-drop discipline).
func (f FooterBar) ViewStats() string {
	w := f.width
	if w < 10 {
		w = 80
	}

	sep := ftSep.Render(" | ")

	var items []string
	if f.toolCount > 0 {
		items = append(items, ftLabel.Render("tools:")+ftStat.Render(fmt.Sprintf("%d", f.toolCount)))
	}
	if f.skillCount > 0 {
		items = append(items, ftLabel.Render("skills:")+ftStat.Render(fmt.Sprintf("%d", f.skillCount)))
	}
	if f.agentCount > 0 {
		items = append(items, ftLabel.Render("agents:")+ftStat.Render(fmt.Sprintf("%d", f.agentCount)))
	}
	if f.msgCount > 0 {
		items = append(items, ftLabel.Render("msgs:")+ftStat.Render(fmt.Sprintf("%d", f.msgCount)))
	}
	if f.stepsRun > 0 {
		items = append(items, ftLabel.Render("steps:")+ftStat.Render(fmt.Sprintf("%d", f.stepsRun)))
	}

	if len(items) == 0 {
		return ""
	}

	// Progressively drop items from the end until the line fits.
	for n := len(items); n > 0; n-- {
		candidate := " " + strings.Join(items[:n], sep)
		if lipgloss.Width(candidate) <= w {
			return candidate
		}
	}
	return ""
}

// ViewWorkspace renders the workspace cwd line below the stats:
//
//	workspace: ~/.../thought_harness/go
//
// The path is middle-truncated to fit the width. Returns "" when no workspace is set.
func (f FooterBar) ViewWorkspace() string {
	if f.workDir == "" {
		return ""
	}
	w := f.width
	if w < 10 {
		w = 80
	}

	label := ftLabel.Render(" workspace:")
	labelW := lipgloss.Width(label)

	pathBudget := w - labelW - 1
	if pathBudget < 5 {
		return ""
	}
	path := truncateMiddle(f.workDir, pathBudget)
	return label + ftPath.Render(path)
}

// View renders the full footer (identity + stats + workspace) as one block. app.go prefers the three
// methods separately so the input box can sit between identity and stats; this is the convenience
// fallback (and what headless tests render).
func (f FooterBar) View() string {
	return f.ViewIdentity() + "\n" + f.ViewStats() + "\n" + f.ViewWorkspace()
}

// spaceFill renders " left … right " with the gap padded so right anchors to the far edge. When the
// two don't fit, the right is dropped (recursing once) and the left returned bare.
func spaceFill(left, right string, w int) string {
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)

	fill := w - leftW - rightW - 1
	if fill < 1 {
		if right != "" {
			return spaceFill(left, "", w)
		}
		return left
	}
	if right == "" {
		return left
	}
	return left + strings.Repeat(" ", fill) + right + " "
}

// truncateMiddle shortens s to maxLen by replacing the middle with "...", keeping more of the start
// (project root) and the leaf directory (lathe footer discipline).
func truncateMiddle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 5 {
		return s[:maxLen]
	}
	const ellipsis = "..."
	avail := maxLen - len(ellipsis)
	headLen := avail / 3
	tailLen := avail - headLen
	return s[:headLen] + ellipsis + s[len(s)-tailLen:]
}
