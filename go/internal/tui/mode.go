package tui

// mode.go — the two primary modes (DESIGN §4.1), the per-mode View dispatch, and the layout sizing
// (the lathe residual-height discipline + the cognition Dashboard width). The root App (app.go) holds
// one Mode and a modeEpoch stamped at every switch so a stale async step result is dropped after a
// Shift+Tab. This file isolates the mode-keyed branching so app.go's Update/View read as one chain.
//
// CHAT mode is the lathe vertical-concat layout (§4.2); COGNITION mode is the options Dashboard
// composer driving the nine MIND-rail panels with the open chat column beside the rail (§4.3). The
// two share the single Palette (theme.go) — they differ only in their layout engine, which is exactly
// what the task asks (chat = lathe, cognition = options).

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// Mode is the primary surface the App shows. Shift+Tab toggles it (modulo the popup modal-priority
// chain — while a popup owns the key sink, Shift+Tab is that popup's prev-item, never a mode switch).
type Mode int

const (
	// ModeChat is the lathe chat layout: header / viewport(chat) / footer(identity, input, stats,
	// workspace), vertical string-concat (§4.2).
	ModeChat Mode = iota
	// ModeCognition is the options Dashboard: the nine MIND-rail panels with the open chat column
	// beside the rail (§4.3).
	ModeCognition
)

// modeCount is the number of modes (the Shift+Tab toggle is `(mode+1) % modeCount`).
const modeCount = 2

// String gives the mode a human label (the header / footer / status-bar mode indicator).
func (m Mode) String() string {
	switch m {
	case ModeCognition:
		return "cognition"
	default:
		return "chat"
	}
}

// next returns the mode the Shift+Tab toggle advances to (wrapping).
func (m Mode) next() Mode { return (m + 1) % modeCount }

// inputBorder is the two border lines the input box adds around the textarea content (NormalBorder
// top + bottom). The input area height is the textarea's own Height() plus this.
const inputBorder = 2

// -- the shared frame: one chrome around BOTH modes (DESIGN §4.1/§4.3) -----------------------------
//
// Both modes render the SAME frame: the cognition chrome header on top, the mode's content in the
// residual height, the lathe-style Shift+Tab line, the input bar, then the cognition chrome footer
// pinned to the bottom — so the header + footer appear in chat AND cognition, and the Shift+Tab line +
// input sit ABOVE the footer in both. Only the content differs (chat viewport vs cognition grid).

// chromeFrame stacks the unified chrome (header / content / Shift+Tab line / input bar / footer) around
// `content`, which the caller has already sized to contentHeight() lines.
func (a *App) chromeFrame(content string) string {
	header := cognition.Dashboard(a.w, []cognition.Panel{a.cognitionHeader()}, nil)
	footer := cognition.Dashboard(a.w, nil, nil, a.cognitionStatus())
	return lipgloss.JoinVertical(lipgloss.Left, header, content, a.aboveInputLine(), a.cognitionInput(), footer)
}

// contentHeight is the residual height for a mode's content: the terminal height minus the chrome
// header, the Shift+Tab line, the input bar (which grows with the slash-autocomplete box), and the
// chrome footer. ≥1.
func (a *App) contentHeight() int {
	header := cognition.Dashboard(a.w, []cognition.Panel{a.cognitionHeader()}, nil)
	footer := cognition.Dashboard(a.w, nil, nil, a.cognitionStatus())
	h := a.h - lipgloss.Height(header) - lipgloss.Height(footer) -
		lipgloss.Height(a.aboveInputLine()) - lipgloss.Height(a.cognitionInput())
	if h < 1 {
		h = 1
	}
	return h
}

// -- the per-mode View dispatch (DESIGN §4.1 View switch) ------------------------------------------

// viewChat renders CHAT mode: the chat viewport (the watched seam made open) as the content of the
// shared chrome frame (header / chat / input / footer). While the conversation is empty it shows the
// welcome screen instead (lathe's welcome-to-chat transition); the first turn switches to the chat.
// The content is sized to the residual height each frame, so the slash box growing the input never
// overflows the footer.
func (a *App) viewChat() string {
	if a.chatView.Len() == 0 {
		return a.chromeFrame(a.welcomeView(a.w, a.contentHeight()))
	}
	// PURE: the viewport's height + bottom-follow are maintained in Update (syncChatHeight — called on
	// resize, mode switch, and after the slash box changes the input height), so View only reads here.
	return a.chromeFrame(a.viewport.View())
}

// viewCognition renders COGNITION mode (DESIGN §4.3): the nine MIND-rail panels laid out as a
// full-width multi-column GRID, scrolled within the residual content height, inside the SAME shared
// chrome frame as CHAT (header / grid / input / footer). There is no conversation column here — the
// conversation lives in CHAT mode; the two surfaces are split cleanly by mode (the clean mode-switch).
func (a *App) viewCognition() string {
	// COGNITION is a set of full-screen TABS (cogtabs.go): a one-line tab strip on top, then the active
	// tab's body windowed into the residual height. Overview is the original full grid; the other tabs
	// give one cognition system the full width (chrome + input stay pinned by chromeFrame).
	strip := a.cognitionTabStrip()
	bodyH := a.contentHeight() - lipgloss.Height(strip)
	if bodyH < 1 {
		bodyH = 1
	}
	body := a.windowGrid(a.cognitionTabBody(), bodyH)
	return a.chromeFrame(lipgloss.JoinVertical(lipgloss.Left, strip, body))
}

// cognitionInput renders the input bar for COGNITION mode: the inline slash-autocomplete box (when
// visible) above the bordered textarea — the same widgets CHAT mode's input uses, so the input is
// identical across modes (the task asks for the input bar in both layouts).
func (a *App) cognitionInput() string {
	var sb strings.Builder
	if a.slash.Visible() {
		sb.WriteString(a.slash.View())
		sb.WriteString("\n")
	}
	sb.WriteString(inputBoxStyle.Width(a.w - inputBorder).Render(a.input.View()))
	return sb.String()
}

// cognitionColumns picks the inspect grid's column count: two columns once the terminal is wide enough
// to keep each panel near its ~48-col design width, one column otherwise (the panels then stack
// full-width, the original single-rail look). DESIGN §4.3.
func (a *App) cognitionColumns() int {
	if a.w >= cognitionTwoColMin {
		return 2
	}
	return 1
}

// cognitionTwoColMin is the width at/above which the grid uses two columns (2×~48 + a one-col gutter).
const cognitionTwoColMin = 100

// gridRows packs panels left-to-right into rows of `cols` equal-width columns. Each panel is padded up
// to its rail-spec height as a MINIMUM (so a sparse panel keeps its footprint) but is NEVER clipped
// vertically — a panel with more (wrapped) content than its spec grows to fit it, and the dashboard's
// Row.render equalizes the two columns of a row to the taller one. Nothing is cut: the full text is
// always shown (DESIGN "no clipping"). The assembled grid scrolls as a whole within the content height.
func gridRows(panels []cognition.Panel, heights []int, cols, inner int) []cognition.Row {
	if cols < 1 {
		cols = 1
	}
	rows := make([]cognition.Row, 0, (len(panels)+cols-1)/cols)
	for i := 0; i < len(panels); i += cols {
		end := i + cols
		if end > len(panels) {
			end = len(panels)
		}
		cells := make([]cognition.Panel, 0, end-i)
		for j := i; j < end; j++ {
			p := panels[j]
			p.Body = padPanelBody(p.Body, heights[j], inner)
			cells = append(cells, p)
		}
		rows = append(rows, cognition.R(cells...))
	}
	return rows
}

// padPanelBody guarantees a panel renders at least its rail-spec height (sparse panels keep their
// footprint) and clips NO content vertically — a panel taller than its spec grows. Every line is still
// hard-clipped to the inner content width as a horizontal safety net (the renderers already wrap to it,
// so this only ever trims a title/branch-tree at a degenerately narrow width). boxH<4 leaves the body.
func padPanelBody(body string, boxH, inner int) string {
	lines := strings.Split(body, "\n")
	if inner > 0 {
		for i, ln := range lines {
			if lipgloss.Width(ln) > inner {
				lines[i] = ansi.Truncate(ln, inner, "…")
			}
		}
	}
	for len(lines) < boxH-2 { // border top+bottom = 2; pad up to the spec minimum, never clip
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// windowGrid vertically scrolls the panel grid by a.cogScroll so it fits `height` lines, padding with
// blank lines when the grid is shorter than its viewport so the status bar + input stay pinned to the
// screen bottom. up/down/pgup/pgdown/home/end drive a.cogScroll (handleModeKey). This is a PURE view:
// it reads a.cogScroll and clamps a LOCAL copy to [0, lines-height] for rendering, but NEVER writes the
// model (the authoritative clamp lives in Update — clampCogScroll, called from the key handlers and the
// resize re-clamp). So a stale over-bound offset renders correctly without making state render-order-
// dependent (D3/D4).
func (a *App) windowGrid(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		for len(lines) < height {
			lines = append(lines, "")
		}
		return strings.Join(lines, "\n")
	}
	off := clampIndexOffset(a.cogScroll, len(lines)-height)
	return strings.Join(lines[off:off+height], "\n")
}
