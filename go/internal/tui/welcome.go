package tui

// welcome.go — a static port of the lathe welcome/splash screen (ui/welcome.go), without the 60fps
// animation machinery (the wave field, dissolve, shimmer, spinning border). It renders the THOUGHT
// wordmark, the subtitle + capability tags, a system-info box, Quick Start + Shortcuts, and a tip
// line, centered in the chat content area. Shown in CHAT mode while the conversation is empty
// (app.go viewChat); replaced by the conversation on the first turn. Foreground-only, NO bold
// (DESIGN §5); responsive height tiers progressively disclose the lower sections (lathe's tiers).

import (
	"fmt"
	"math"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/charmbracelet/lipgloss"
)

// welcomeLogo is the THOUGHT block wordmark (5 rows, 48 cols). Toned top→bottom by welcomeGradient.
var welcomeLogo = []string{
	"██████ ██  ██ ██████ ██  ██ ██████ ██  ██ ██████",
	"  ██   ██  ██ ██  ██ ██  ██ ██     ██  ██   ██  ",
	"  ██   ██████ ██  ██ ██  ██ ██ ███ ██████   ██  ",
	"  ██   ██  ██ ██  ██ ██  ██ ██  ██ ██  ██   ██  ",
	"  ██   ██  ██ ██████ ██████ ██████ ██  ██   ██  ",
}

// welcomeGradient tones the logo rows top→bottom — a gentle fade from title-white through the single
// steel-blue accent to the muted gray (the one design language, no extra hues; lathe's mauve→green).
var welcomeGradient = []lipgloss.Color{Pal.Title, Pal.Accent, Pal.Accent, Pal.Status, Pal.Trace}

// welcomeTips rotate in the tip line on the animation frame clock (~5s each, see welcomeTip).
var welcomeTips = []string{
	"Type a goal to start a thinking episode",
	"Shift+Tab switches the CHAT and COGNITION surfaces",
	"^K opens the command palette",
	"COGNITION mode shows the live mind — graph, seams, critic, durability, memory",
	"The harness thinks silently; only world-changing action is watched",
}

// welcomeView centers the welcome content in a width×height box (the chat content area when empty) and
// composites it OVER an animated wave-field background, which ripples in the margins around the
// content card. Lower sections appear only as the terminal gets taller (the responsive tiers).
func (a *App) welcomeView(width, height int) string {
	// build the content sections, center each to a uniform card width, pad to a clean rectangle.
	sections := a.welcomeSections(width, height)
	cardW := 0
	for _, s := range sections {
		if w := lipgloss.Width(s); w > cardW {
			cardW = w
		}
	}
	centered := make([]string, len(sections))
	for i, s := range sections {
		centered[i] = lipgloss.PlaceHorizontal(cardW, lipgloss.Center, s)
	}
	card := lipgloss.NewStyle().Width(cardW).Render(strings.Join(centered, "\n\n"))
	cardLines := strings.Split(card, "\n")
	cardH := len(cardLines)

	// too narrow/short for a wave margin — just center the card plainly (no background). On a terminal
	// shorter than the card the section tiers can't shed enough rows, so window the card to `height`
	// (keep the top — the wordmark identifies the screen) rather than overflow the content region (L8).
	if width < cardW+8 || height < cardH+2 {
		if height > 0 && cardH > height {
			cardLines = cardLines[:height]
			card = strings.Join(cardLines, "\n")
		}
		return lipgloss.PlaceVertical(height, lipgloss.Center,
			lipgloss.PlaceHorizontal(width, lipgloss.Center, card))
	}

	// composite: the card overwrites the centered rectangle; the wave shows in every margin cell. Each
	// cell carries a density (0-4); seg() renders a column span by appending the per-density styled glyph.
	density := waveField(width, height, a.frame)
	sc := waveStyledChars(a.frame)
	seg := func(y, x0, x1 int) string {
		var b strings.Builder
		for x := x0; x < x1; x++ {
			b.WriteString(sc[density[y][x]])
		}
		return b.String()
	}
	top, left := (height-cardH)/2, (width-cardW)/2
	out := make([]string, height)
	for y := 0; y < height; y++ {
		if y >= top && y < top+cardH {
			out[y] = seg(y, 0, left) + cardLines[y-top] + seg(y, left+cardW, width)
		} else {
			out[y] = seg(y, 0, width)
		}
	}
	return strings.Join(out, "\n")
}

// welcomeSections returns the welcome content blocks gated by the responsive height tiers (lathe's
// progressive disclosure): logo + subtitle always; the substrate-loading status (when the product
// path is resolving/auto-loading a model) right under the subtitle so it shows at every height; then
// info box, quick start, shortcuts as height allows.
func (a *App) welcomeSections(width, height int) []string {
	parts := []string{a.welcomeLogoView(width), a.welcomeSubtitle()}
	if a.substrateLoading || a.loadErr != "" {
		parts = append(parts, a.welcomeLoadStatus())
	}
	if height >= 16 {
		parts = append(parts, a.welcomeInfoBox())
	}
	if height >= 22 {
		parts = append(parts, welcomeQuickStart())
	}
	if height >= 28 {
		parts = append(parts, welcomeShortcuts())
	}
	return append(parts, a.welcomeTip())
}

// welcomeSpinner is the braille spinner cycled off the animation frame while the substrate loads.
var welcomeSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// welcomeLoadStatus renders the substrate-resolution status on the welcome card: while loading, a
// spinner + the auto-loader's latest progress line (model + size); on failure, the reason + a hint
// (the harness has no offline path, so it must reach a model). The welcome anim re-renders it live.
func (a *App) welcomeLoadStatus() string {
	if a.loadErr != "" {
		head := lipgloss.NewStyle().Foreground(Pal.Warn).Render("no model reached")
		why := lipgloss.NewStyle().Foreground(Pal.Muted).Render(a.loadErr)
		hint := lipgloss.NewStyle().Foreground(Pal.Faint).
			Render("/settings to configure a model  ·  /substrate to retry")
		return leftBlock(head + "\n" + why + "\n" + hint)
	}
	spin := welcomeSpinner[(a.frame/3)%len(welcomeSpinner)]
	status := a.loadStatus
	if status == "" {
		status = "preparing the thinking substrate…"
	}
	return lipgloss.NewStyle().Foreground(Pal.Accent).Render(spin) + " " +
		lipgloss.NewStyle().Foreground(Pal.Subtext).Render(status)
}

// waveField generates the animated background behind the welcome card as a width×height DENSITY grid
// (0-4) — a wave-field welcome animation (ported from a sibling Go agent harness): THREE
// sine curves positioned against a 50-row reference, each painted over ±3 rows with a density falloff
// (4 at the crest, down to 1 at the edge), later curves overwriting earlier (fixed z-order). The phase
// drifts with `frame`. waveStyledChars maps each density to a glyph + tone; the caller composites the
// card opaquely over the rendered field.
func waveField(width, height, frame int) [][]int {
	grid := make([][]int, height)
	for y := range grid {
		grid[y] = make([]int, width)
	}
	if width < 10 || height < 5 {
		return grid
	}
	t := float64(frame) * 0.03
	const refH = 50.0
	fScale := 120.0 / float64(width) // frequency scales with 1/width so the wavelength stays proportional
	type curve struct{ centerY, amp, freq, phase, speed float64 }
	curves := []curve{
		{refH * 0.3, refH * 0.15, 0.04 * fScale, 0, 1.0},
		{refH * 0.5, refH * 0.2, 0.05 * fScale, 1.5, 0.8},
		{refH * 0.7, refH * 0.18, 0.035 * fScale, 3.0, 1.2},
	}
	for _, c := range curves {
		for x := 0; x < width; x++ {
			fx := float64(x)
			wy := c.centerY + c.amp*math.Sin(c.freq*fx+c.phase+t*c.speed) +
				c.amp*0.3*math.Sin(c.freq*2.3*fx+c.phase*0.7-t*c.speed*0.5)
			for dy := -3; dy <= 3; dy++ {
				row := int(math.Round(wy)) + dy
				if row < 0 || row >= height {
					continue
				}
				adist := dy
				if adist < 0 {
					adist = -adist
				}
				density := 4 - adist
				if density < 1 {
					density = 1
				}
				grid[row][x] = density // layering: a later (front) curve overwrites an earlier one
			}
		}
	}
	return grid
}

// waveStyledChars precomputes the per-density styled glyph for the wave field (lathe's styledChars):
// density 0 = a blank, 1 = a dim scatter dot, 2-3 = a mid scatter, 4 = the bright crest core. The
// shimmer glyph set swaps every ~8 frames so the field flickers. Tones are mapped to the harness
// palette (dim=Surface, mid=Muted, core=Accent — the steel-blue analogue of lathe's lavender core).
func waveStyledChars(frame int) [5]string {
	shimmer := [5]string{" ", "·", "∙", ":", "∘"}
	if (frame/8)%2 == 1 {
		shimmer = [5]string{" ", "∙", "·", "∙", "·"}
	}
	dim := lipgloss.NewStyle().Foreground(Pal.Surface)
	mid := lipgloss.NewStyle().Foreground(Pal.Muted)
	core := lipgloss.NewStyle().Foreground(Pal.Accent)
	return [5]string{
		" ",
		dim.Render(shimmer[1]),
		mid.Render(shimmer[2]),
		mid.Render(shimmer[3]),
		core.Render(shimmer[4]),
	}
}

// welcomeLogoView tones the block wordmark row-by-row, falling back to a plain title when the terminal
// is narrower than the wordmark.
func (a *App) welcomeLogoView(width int) string {
	if width < lipgloss.Width(welcomeLogo[0]) {
		return lipgloss.NewStyle().Foreground(Pal.Title).Render("T H O U G H T")
	}
	rows := make([]string, len(welcomeLogo))
	for i, r := range welcomeLogo {
		c := Pal.Accent
		if i < len(welcomeGradient) {
			c = welcomeGradient[i]
		}
		rows[i] = lipgloss.NewStyle().Foreground(c).Render(r)
	}
	return strings.Join(rows, "\n")
}

// welcomeSubtitle is the tagline + the three capability tags (lathe's subtitle + Pre-AGI tags).
func (a *App) welcomeSubtitle() string {
	sub := lipgloss.NewStyle().Foreground(Pal.Text).Render("Silent-Injection Cognition Harness")
	dot := lipgloss.NewStyle().Foreground(Pal.Faint).Render(" · ")
	tag := lipgloss.NewStyle().Foreground(Pal.Accent)
	tags := tag.Render("Pre-AGI") + dot + tag.Render("Self-Aware") + dot + tag.Render("Self-Learning")
	return sub + "\n" + tags
}

// welcomeInfoBox is the system-info box: the active model, the loop regime, the lifecycle state, and
// the branch (the live state a returning dev wants at a glance; lathe's info box, statically).
func (a *App) welcomeInfoBox() string {
	model := a.footer.Model()
	if model == "" {
		model = "no model loaded"
	}
	state := a.vm.Snap.LifecycleState
	if state == "" {
		state = "IDLE"
	}
	label := lipgloss.NewStyle().Foreground(Pal.Muted)
	value := lipgloss.NewStyle().Foreground(Pal.Text)
	row := func(k, v string) string { return label.Render(fmt.Sprintf("%-10s", k)) + value.Render(v) }

	rows := []string{row("model", model), row("mode", config.ModeLabel(a.footer.Mode())+" (loop)"), row("lifecycle", state)}
	if b := a.footer.Branch(); b != "" {
		rows = append(rows, row("branch", b))
	}
	box := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(Pal.Surface).Padding(0, 2)
	return box.Render(strings.Join(rows, "\n"))
}

// welcomeQuickStart is the two-line Quick Start hint block.
func welcomeQuickStart() string {
	head := lipgloss.NewStyle().Foreground(Pal.Subtext).Render("Quick Start")
	key := lipgloss.NewStyle().Foreground(Pal.Accent)
	desc := lipgloss.NewStyle().Foreground(Pal.Text)
	items := []string{
		key.Render("›") + " " + desc.Render("type a goal, or ") + key.Render("/help") + desc.Render(" for commands"),
		key.Render("›") + " " + key.Render("^K") + desc.Render(" opens the command palette"),
	}
	return leftBlock(head + "\n" + strings.Join(items, "\n"))
}

// welcomeShortcuts is the keyboard-shortcut reference block.
func welcomeShortcuts() string {
	head := lipgloss.NewStyle().Foreground(Pal.Subtext).Render("Shortcuts")
	key := lipgloss.NewStyle().Foreground(Pal.Accent)
	desc := lipgloss.NewStyle().Foreground(Pal.Muted)
	rows := [][2]string{
		{"Enter", "send a goal"},
		{"Shift+Tab", "switch CHAT / COGNITION"},
		{"^K", "command palette"},
		{"^B", "toggle the panel surface"},
		{"PgUp/PgDn", "scroll"},
		{"^C", "quit"},
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = key.Render(fmt.Sprintf("%-11s", r[0])) + desc.Render(r[1])
	}
	return leftBlock(head + "\n" + strings.Join(lines, "\n"))
}

// leftBlock pads every line of s to the block's widest line (left-aligned), turning a ragged
// multi-line string into a rectangle so welcomeView's PlaceHorizontal centers it as ONE block (a
// clean left-aligned list) rather than centering each line independently.
func leftBlock(s string) string {
	return lipgloss.NewStyle().Width(lipgloss.Width(s)).Render(s)
}

// welcomeTip renders the rotating tip line. It cycles on the ANIMATION frame clock (a.frame, ~30fps),
// one rotation every ~5s — a steady wall-clock cadence INDEPENDENT of the engine tick. (It used to
// index by Snap.Tick, which made it flicker in lockstep with cognition in continuous mode — the engine
// ticks fast and irregularly there — and freeze entirely in reactive idle. The welcome screen always
// animates a.frame while it is shown, so no extra clock is needed.)
func (a *App) welcomeTip() string {
	tip := welcomeTips[0]
	if n := len(welcomeTips); n > 0 {
		tip = welcomeTips[(a.frame/150)%n] // 150 frames ≈ 5s at the ~30fps animTick
	}
	return lipgloss.NewStyle().Foreground(Pal.Accent).Render("tip") +
		lipgloss.NewStyle().Foreground(Pal.Faint).Render(" › ") +
		lipgloss.NewStyle().Foreground(Pal.Subtext).Render(tip)
}
