// dashboard.go — the options-terminal layout composer, lifted WHOLESALE (DESIGN §4.3). It is the
// single most reusable thing in the options UI: a Panel/Row/Dashboard trio that boxes every cell to
// the same total width W so all borders line up for free. The COGNITION mode reduces it to one body
// column (the MIND rail is a vertical stack of fixed-height bordered panels, Python tui/widgets/
// mind_rail.py), but the full composer is ported so the chat-beside-rail split (RSplit) and the
// narrow-terminal stack (stackBelow) work exactly as in options.
//
// This file owns the cognition design language LOCALLY (it cannot import the parent tui package's
// theme.go — tui imports cognition, so the reverse would be an import cycle). The palette is the same
// single design language (DESIGN §5, the port of tui/theme.py `C`): foreground-only, NormalBorder
// (square) for panels, no Background except the chrome bars. theme.go in the tui package is the
// authority; these constants mirror it verbatim so the two surfaces stay identical.
package cognition

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// -- the cognition design language (mirror of tui/theme.py `C`; DESIGN §5) -------------------------
//
// The cognition package is engine-free AND tui-free, so it carries its own copy of the palette. Every
// hex is byte-for-byte the same as the parent tui.Palette; the panels read by the semantic name the
// Python panels.py module uses (TXT/SUB/MUT/FNT + ACC/ACT + OK/WARN/ERR), so a call site here matches
// the Python source line.
const (
	colText    = lipgloss.Color("#e6e9ef") // primary content / the harness's voice (TXT)
	colSubtext = lipgloss.Color("#b8bcc6") // secondary content, section headers (SUB)
	colMuted   = lipgloss.Color("#8891a0") // labels, de-emphasised notes (MUT)
	colFaint   = lipgloss.Color("#5b6270") // dimmest still-readable (ticks, inactive) (FNT)
	colAccent  = lipgloss.Color("#81a1c1") // steel blue: active line / voiced thought (ACC)
	colAction  = lipgloss.Color("#a0a8b4") // tool I/O — light cool gray (ACT)
	colSurface = lipgloss.Color("#3b4452") // panel borders
	colOk      = lipgloss.Color("#a3be8c") // pass / ADMIT / awake (OK)
	colWarn    = lipgloss.Color("#ebcb8b") // FLAG / drowsy (WARN)
	colErr     = lipgloss.Color("#e88388") // REJECT / conflict / fail (ERR)
	colWhite   = lipgloss.Color("#ffffff") // white borders + the chrome-bar fill
	colBase    = lipgloss.Color("#16181d") // near-black — dark text on the white chrome bars
)

// box is the shared square-bordered panel style — every Panel boxes with THIS, so border+padding is
// subtracted identically and the inner content width is exact (options' `box`). The border is WHITE
// (every border is white). Padding(0,1) matches options + the Python MindRail Pane padding "0 1".
var box = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(colWhite).
	Padding(0, 1)

// chromeBar is the header/status bar style: a solid WHITE bar with dark (base) text inside a white
// border, plain text only so the fill is unbroken (the header/footer have a white background).
var chromeBar = lipgloss.NewStyle().
	Background(colWhite).
	Foreground(colBase).
	Border(lipgloss.NormalBorder()).
	BorderForeground(colWhite).
	BorderBackground(colWhite).
	Padding(0, 1)

// -- the lifted composer (options internal/ui/render.go, verbatim shape) ----------------------------

// Panel is a not-yet-boxed dashboard cell: its inner Body plus an optional truecolor Border hex
// override ("" = the shared surface grey) and a Chrome flag (header/status bar — the solid-fill bar).
// Dashboard boxes it, so the width is decided once, centrally — that is what keeps every panel's
// border aligned. (options' Panel{body,border,chrome}, exported field names for the cognition pkg.)
type Panel struct {
	Body   string // the inner content (already styled with ANSI runs by the render_<pid> functions)
	Border string // optional truecolor border hex override; "" = the default surface grey
	Chrome bool   // header/status bar: a solid-fill bar (the one sanctioned Background)
}

// render boxes the panel. width>0 / height>0 force an exact TOTAL size (border + padding + content)
// on that axis; 0 means natural (content-sized). The border+padding is subtracted via lipgloss's
// GetHorizontal/VerticalBorderSize so the content area is exact (options' Panel.render).
func (p Panel) render(width, height int) string {
	if p.Chrome {
		st := chromeBar
		if width > 0 {
			st = st.Width(floorDim(width - st.GetHorizontalBorderSize()))
		}
		if height > 0 {
			st = st.Height(floorDim(height - st.GetVerticalBorderSize()))
		}
		return st.Render(p.Body)
	}
	st := box
	if p.Border != "" {
		st = st.BorderForeground(lipgloss.Color(p.Border))
	}
	if width > 0 {
		st = st.Width(floorDim(width - st.GetHorizontalBorderSize()))
	}
	if height > 0 {
		st = st.Height(floorDim(height - st.GetVerticalBorderSize()))
	}
	return st.Render(p.Body)
}

// floorDim floors a computed inner dimension at 1. When the requested TOTAL is smaller than the
// border+padding the subtraction goes negative, and lipgloss treats a negative Width/Height as
// "unconstrained" — which silently de-aligns the row (the panel grows to its natural size while its
// siblings stayed boxed). Flooring at 1 keeps a degenerate-narrow panel boxed and aligned (L7/L8).
func floorDim(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// width is the panel's natural rendered width (border + padding + content).
func (p Panel) width() int { return lipgloss.Width(p.render(0, 0)) }

// Width is the exported natural width — used by the app to size content to the space a row gives it.
func (p Panel) Width() int { return p.width() }

// WithBorder overrides the panel's outline colour (truecolor hex; "" = default grey). The app uses it
// to pulse a panel's border on an event (a specialist fires / a thought is injected) — a transient,
// brightness-only cue that never touches the square shape (options' Panel.WithBorder, DESIGN §4.5).
func (p Panel) WithBorder(hex string) Panel {
	if hex != "" {
		p.Border = hex
	}
	return p
}

// renderFit boxes the panel at total `width`, first ANSI-clipping each body line to the inner content
// width (ellipsised) so changing chrome text never widens the app (options' Panel.renderFit; the
// clip uses x/ansi.Truncate, which is ANSI-aware so a styled line clips at its VISIBLE width).
func (p Panel) renderFit(width int) string {
	if inner := width - 4; inner > 0 { // border(2) + padding(2)
		lines := strings.Split(p.Body, "\n")
		for i, ln := range lines {
			if lipgloss.Width(ln) > inner {
				lines[i] = ansi.Truncate(ln, inner, "…")
			}
		}
		p.Body = strings.Join(lines, "\n")
	}
	return p.render(width, 0)
}

// Row is one horizontal band of the body. By default its 1..N panels render at EQUAL width and a
// shared height, so every column's bottom border lines up. A LeftFixed row instead sizes its first
// panel to its natural width and lets the second fill the rest (the chat-beside-rail split).
type Row struct {
	Cols      []Panel
	LeftFixed bool
}

// R builds an equal-width Row from its columns (options' R).
func R(cols ...Panel) Row { return Row{Cols: cols} }

// RSplit pairs a primary panel (kept at its natural width) with a secondary panel that fills the
// remaining width — the cognition chat-column-beside-the-rail layout (options' RSplit).
func RSplit(primary, secondary Panel) Row {
	return Row{Cols: []Panel{primary, secondary}, LeftFixed: true}
}

// naturalWidth is the total width the row needs. Equal columns need n×widestColumn (else the wide
// column wraps); a LeftFixed row needs the sum (primary natural + secondary) (options' naturalWidth).
func (r Row) naturalWidth(gutter int) int {
	if r.LeftFixed {
		w := gutter * (len(r.Cols) - 1)
		for _, c := range r.Cols {
			w += c.width()
		}
		return w
	}
	widest := 0
	for _, c := range r.Cols {
		widest = max(widest, c.width())
	}
	return len(r.Cols)*widest + gutter*(len(r.Cols)-1)
}

// render lays the columns out at total width W: equal column widths (the last absorbs the rounding
// remainder so the row sums to exactly W) at a shared height. A LeftFixed 2-col row keeps the primary
// at its natural width (clamped so the secondary keeps ≥24) and gives the rest to the secondary.
// (options' Row.render, the RSplit "min ~24" rule preserved.)
func (r Row) render(W, gutter int) string {
	n := len(r.Cols)
	if n == 1 {
		return r.Cols[0].render(W, 0)
	}
	widths := make([]int, n)
	if r.LeftFixed && n == 2 {
		lw := r.Cols[0].width()
		if lw > W-gutter-24 {
			lw = W - gutter - 24
		}
		if lw < 1 {
			lw = (W - gutter) / 2
		}
		widths[0], widths[1] = lw, W-lw-gutter
	} else {
		colW := (W - gutter*(n-1)) / n
		for i := range widths {
			widths[i] = colW
		}
		widths[n-1] = W - (colW+gutter)*(n-1)
	}
	h := 0
	for i, p := range r.Cols {
		h = max(h, lipgloss.Height(p.render(widths[i], 0)))
	}
	parts := make([]string, 0, 2*n-1)
	for i, p := range r.Cols {
		if i > 0 {
			parts = append(parts, strings.Repeat(" ", gutter))
		}
		parts = append(parts, p.render(widths[i], h))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// stackBelow is the breakpoint: narrower than this → side-by-side columns stack into one (options').
const stackBelow = 84

// Dashboard composes the panels into one uniform-width block: chrome control panels stack full-width
// on top, then the body rows (each a band of equal-width, equal-height columns), then the chrome
// footer (status bar). Every row renders to the same total width W so all borders line up. W is the
// terminal width; with no size yet (termW<=0) it falls back to the widest natural panel/row. When the
// terminal is too narrow to hold a row's columns side by side (termW<stackBelow) every multi-column
// row stacks into one column. (options' Dashboard, gutter=1, the cognition body = one column per row.)
func Dashboard(termW int, controls []Panel, body []Row, footer ...Panel) string {
	const gutter = 1
	W := termW
	if termW <= 0 {
		for _, r := range body {
			W = max(W, r.naturalWidth(gutter))
		}
		for _, p := range controls {
			W = max(W, p.width())
		}
	}
	stack := termW > 0 && termW < stackBelow

	rows := make([]string, 0, len(controls)+len(body)+len(footer))
	for _, p := range controls {
		rows = append(rows, p.renderFit(W))
	}
	for _, r := range body {
		if stack {
			for _, c := range r.Cols {
				rows = append(rows, c.render(W, 0))
			}
		} else {
			rows = append(rows, r.render(W, gutter))
		}
	}
	for _, p := range footer {
		rows = append(rows, p.renderFit(W))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
