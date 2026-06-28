package tui

// cogtabs.go — the COGNITION "tab split": instead of one scrolling grid of every panel, COGNITION
// mode is a set of full-screen tabs you cycle with Tab — an Overview (the current grid, kept as-is)
// plus one full-WIDTH view per cognition system (Conscious / Subconscious / Action / Systems). Each
// system view reuses the cognition.RenderPanel renderers at full width; the Subconscious view renders
// a dedicated card per live specialist/sub-agent (cognition.SubconsciousCards).
//
// Tab membership is DERIVED from the live rail by system prefix (tabForPanel) so the panel split
// (conscious / critic_metrics / critic_text / …) sorts itself into the right tab without this file
// hardcoding ids. The tab strip + navigation (the cogTab field, the Tab key) live in app.go.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// cogTabSpec is one full-screen COGNITION tab: its id (also the tabForPanel bucket) and strip label.
type cogTabSpec struct {
	id    string
	label string
}

// cognitionTabs is the ordered tab strip. Overview is the untouched current grid (it evolves
// separately); the rest give each layer the full width. Critic folds into Conscious; the hidden seam
// sits with Subconscious; the cross-cutting organs group under Systems.
var cognitionTabs = []cogTabSpec{
	{id: "overview", label: "Overview"},
	{id: "conscious", label: "Conscious"},
	{id: "subconscious", label: "Subconscious"},
	{id: "action", label: "Action"},
	{id: "grounding", label: "Grounding"},
	{id: "awake", label: "Awake"},
	{id: "runtime", label: "Runtime"},
	{id: "systems", label: "Systems"},
	{id: "registry", label: "Registry"},
	{id: "config", label: "Config"},
}

// cogTabs returns the cognition tabs visible for the current loop mode — the awake-only "Awake" tab
// (arousal / drives / outreach) is hidden in reactive mode, where the continuous stream is off, so the
// view only shows what applies (the Settings hide-when-irrelevant rule, applied to the cognition mode).
// The cogTab cursor indexes THIS list, so the strip + navigation + the body all agree on what's shown.
func (a *App) cogTabs() []cogTabSpec {
	if a.footer.Mode() == "continuous" { // awake — every tab applies
		return cognitionTabs
	}
	out := make([]cogTabSpec, 0, len(cognitionTabs))
	for _, t := range cognitionTabs {
		if t.id == "awake" {
			continue // reactive: no continuous stream, so the Awake tab has nothing to show
		}
		out = append(out, t)
	}
	return out
}

// activeCogTab returns the current tab (clamped — the visible set shrinks when the mode flips to
// reactive, so a stale cogTab never indexes past the end).
func (a *App) activeCogTab() cogTabSpec {
	tabs := a.cogTabs()
	return tabs[clampIndex(a.cogTab, len(tabs))]
}

// tabForPanel assigns a rail panel id to a tab by its system prefix (resilient to the metrics/text
// panel split). Critic folds into Conscious; the hidden seam sits with Subconscious; the reality-
// grounding ledger gets its own Grounding tab; everything else (value / durability / lifecycle* /
// memory* / trace) groups under Systems.
func tabForPanel(id string) string {
	switch {
	case strings.HasPrefix(id, "conscious"), strings.HasPrefix(id, "critic"), id == "mcp", id == "frontier":
		return "conscious"
	case strings.HasPrefix(id, "subconscious"), strings.HasPrefix(id, "seam"), id == "generative",
		id == "sourcing", id == "knowledge":
		// sourcing + knowledge are the M3 fuel layer — they belong to Subconscious, where the custom
		// cognitionSubconsciousView renders them explicitly (so they do NOT also land in the Systems grid).
		return "subconscious"
	case strings.HasPrefix(id, "action"), id == "toolexec":
		return "action"
	case id == "grounding":
		return "grounding"
	case id == "continuous":
		return "awake" // the awake-regime home: arousal, decision policy, drives/default-mode, outreach (E1)
	case id == "session":
		return "runtime"
	case id == "dashboard":
		return "overview" // the compact all-subsystem header lives only in the Overview grid
	default: // value / durability / scheduler / backend / lifecycle* / memory* / trace
		return "systems"
	}
}

// railPanelsFor returns the rail panel ids assigned to a tab, in rail order.
func railPanelsFor(tabID string) []string {
	var ids []string
	for _, s := range cognition.Rail() {
		if tabForPanel(s.ID()) == tabID {
			ids = append(ids, s.ID())
		}
	}
	return ids
}

// railTitle looks up a rail panel's border title by id (the rail spec owns the titles).
func railTitle(id string) string {
	for _, s := range cognition.Rail() {
		if s.ID() == id {
			return s.Title()
		}
	}
	return id
}

// tabCols is the column count for a system tab's panels — Systems uses two when wide; the layer tabs
// stack full-width (one column) so each panel gets the whole screen width.
func (a *App) tabCols(tabID string) int {
	if tabID == "systems" && a.w >= cognitionTwoColMin {
		return 2
	}
	return 1
}

// colInnerWidth is the inner content width one panel renders into at `cols` columns across the
// terminal — the column total minus the box border (2) and horizontal padding (2). Mirrors
// Dashboard/Row.render's equal-column split (gutter 1). panelInnerWidth is the current-grid case.
func (a *App) colInnerWidth(cols int) int {
	if cols < 1 {
		cols = 1
	}
	const gutter = 1
	W := a.w
	if W <= 0 {
		W = cognitionTwoColMin
	}
	colW := W
	if cols > 1 {
		colW = (W - gutter*(cols-1)) / cols
	}
	if inner := colW - 4; inner >= 12 {
		return inner
	}
	return 12
}

// cogBodyHeight is the residual height available to a tab body (content height minus the one-line tab
// strip) — used to size full-screen panels so a single system's panels fill the screen.
func (a *App) cogBodyHeight() int {
	h := a.contentHeight() - lipgloss.Height(a.cognitionTabStrip())
	if h < 1 {
		h = 1
	}
	return h
}

// cognitionTabStrip renders the one-line tab bar at the top of COGNITION mode: the tab labels with the
// active one accented, and a right-side hint. Foreground-only (DESIGN §5). When the full strip is wider
// than the terminal (many tabs on a narrow window), it WINDOWS to a contiguous run that always includes
// the active tab — with "‹"/"›" ellipsis markers — so the active tab never scrolls off the edge.
func (a *App) cognitionTabStrip() string {
	active := lipgloss.NewStyle().Foreground(Pal.Accent)
	rest := lipgloss.NewStyle().Foreground(Pal.Muted)
	sepRaw := "  ·  "
	sep := lipgloss.NewStyle().Foreground(Pal.Faint).Render(sepRaw)
	right := lipgloss.NewStyle().Foreground(Pal.Faint).Render("Tab ▸ cycle")

	tabs := a.cogTabs()                    // mode-filtered (Awake tab hidden in reactive)
	cur := clampIndex(a.cogTab, len(tabs)) // the active visible tab (clamped — the set shrinks in reactive)

	// the plain (unstyled) label for each tab, so a width measure is byte-exact.
	plain := func(i int) string {
		if i == cur {
			return "▸ " + tabs[i].label
		}
		return tabs[i].label
	}
	render := func(i int) string {
		if i == cur {
			return active.Render("▸ " + tabs[i].label)
		}
		return rest.Render(tabs[i].label)
	}

	// fast path: the whole strip fits — render it all (the common wide-terminal case).
	full := 0
	for i := range tabs {
		if i > 0 {
			full += len(sepRaw)
		}
		full += lipgloss.Width(plain(i))
	}
	avail := a.w
	if avail < 1 {
		avail = full
	}
	if full <= avail {
		left := ""
		for i := range tabs {
			if i > 0 {
				left += sep
			}
			left += render(i)
		}
		return chromeRow(left, right, a.w)
	}

	// narrow path: window a contiguous run [lo,hi) around the active tab that fits, reserving room for
	// the ‹/› ellipsis markers. Grow outward from the active tab until the next add would overflow.
	const arrow = " ‹ "
	budget := avail - len(arrow)*2 // worst case: both ends elided
	if budget < lipgloss.Width(plain(cur)) {
		budget = lipgloss.Width(plain(cur)) // always show at least the active tab
	}
	lo, hi := cur, cur+1
	used := lipgloss.Width(plain(cur))
	for {
		grew := false
		if lo > 0 {
			cost := len(sepRaw) + lipgloss.Width(plain(lo-1))
			if used+cost <= budget {
				lo--
				used += cost
				grew = true
			}
		}
		if hi < len(tabs) {
			cost := len(sepRaw) + lipgloss.Width(plain(hi))
			if used+cost <= budget {
				hi++
				used += cost
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	left := ""
	if lo > 0 {
		left += lipgloss.NewStyle().Foreground(Pal.Faint).Render("‹ ")
	}
	for i := lo; i < hi; i++ {
		if i > lo {
			left += sep
		}
		left += render(i)
	}
	if hi < len(tabs) {
		left += lipgloss.NewStyle().Foreground(Pal.Faint).Render(" ›")
	}
	return chromeRow(left, right, a.w)
}

// cognitionTabBody builds the UNSCROLLED body string for the active tab (viewCognition windows it to
// the residual height). Overview = the current grid; Subconscious = the per-specialist cards; the rest
// = their rail panels laid out full-width.
func (a *App) cognitionTabBody() string {
	t := a.activeCogTab()
	switch t.id {
	case "overview":
		return a.cognitionOverviewGrid()
	case "subconscious":
		return a.cognitionSubconsciousView()
	case "registry":
		return a.cognitionRegistryView()
	case "config":
		return a.cognitionConfigView()
	default:
		return a.cognitionPanelGrid(railPanelsFor(t.id), a.tabCols(t.id))
	}
}

// cognitionConfigView renders the Config tab: the system-wide HarnessConfig as a left SECTION index +
// right toggle DETAIL (the representation section as a 3-block grid). The view is rebuilt from the live
// shared config each frame (cheap — it ranges the knob table), so a live flip shows immediately; ↑↓
// move the row, Tab/←→ switch section, Space flips a toggle (the keys are handled in app.go). The
// selection is clamped against the live section/row counts here so a stale index never panics.
func (a *App) cognitionConfigView() string {
	cv := a.cfgCache // the cache (refreshed in Update when not stepping) — never the live engine (D2)
	// PURE: render with LOCAL clamped indices; the authoritative clamp of a.cogCfgSel/Row lives in Update
	// (clampCogSelections, called from handleConfigKey + the resize re-clamp) so View never writes (D3/D4).
	sel := clampIndex(a.cogCfgSel, len(cv.Sections))
	row := 0
	if sel < len(cv.Sections) {
		row = clampIndex(a.cogCfgRow, len(cv.Sections[sel].Rows))
	}
	return cognition.RenderConfig(cv, sel, row, a.w)
}

// cognitionRegistryView renders the Registry tab: the whole capability inventory (operators /
// sub-agents / skills / workflows / tools / prompts / memory) as a left INDEX + right DETAIL. The
// catalog is rebuilt from the live engine each frame (cheap — it ranges a few maps), so minted entries
// show up live; ↑↓ pick the registry (a.cogRegSel), PgUp/PgDn scroll the detail (the windowGrid offset).
func (a *App) cognitionRegistryView() string {
	cat := a.regCache // the cache (refreshed in Update when not stepping) — never the live engine (D2)
	// PURE: render with a LOCAL clamped index; a.cogRegSel is clamped authoritatively in Update
	// (clampCogSelections) so View never writes the model (D3/D4).
	return cognition.RenderRegistry(cat, clampIndex(a.cogRegSel, len(cat.Sections)), a.w)
}

// cognitionOverviewGrid renders the Overview tab as cognition.Layout(): full-width TEXT rows (the
// thought stream, the seam, the action, the control reasons, the trace) interleaved with compact METRIC
// decks (the engine vitals; the control flags/state/counts). A layout row of one id is a full-width
// panel; a row of N ids is a metric deck reflowed into as many equal columns as the width holds
// (deckColumns), so long text reads on a full line and the numbers pack tight. (DESIGN: the approved
// "separate metrics from full-width text, interleaved by subsystem" remodel.)
func (a *App) cognitionOverviewGrid() string {
	const gutter = 1
	W := a.w
	if W <= 0 {
		W = cognitionTwoColMin
	}
	var rows []cognition.Row
	for _, lr := range cognition.Layout() {
		if len(lr.IDs) <= 1 {
			rows = append(rows, cognition.R(a.titledPanel(lr.IDs[0], W-4))) // full-width text panel
			continue
		}
		cols := deckColumns(W, len(lr.IDs))
		for i := 0; i < len(lr.IDs); i += cols {
			end := i + cols
			if end > len(lr.IDs) {
				end = len(lr.IDs)
			}
			group := lr.IDs[i:end]
			inner := (W-gutter*(len(group)-1))/len(group) - 4
			if inner < 12 {
				inner = 12
			}
			cells := make([]cognition.Panel, 0, len(group))
			for _, id := range group {
				cells = append(cells, a.titledPanel(id, inner))
			}
			rows = append(rows, cognition.R(cells...))
		}
	}
	return cognition.Dashboard(a.w, nil, rows)
}

// titledPanel renders one rail panel id at inner width, prefixes its title, applies its in-flight
// border pulse, and pads it up to its registry minimum height (it grows past that to fit wrapped text).
func (a *App) titledPanel(id string, inner int) cognition.Panel {
	vm := a.vm
	vm.Width = inner
	p := cognition.RenderPanel(id, vm)
	p.Body = a.panelTitle(railTitle(id)) + "\n" + p.Body
	if hex := a.borderHexFor(id); hex != "" {
		p = p.WithBorder(hex)
	}
	minH := 5
	if spec, ok := cognition.SpecByID(id); ok {
		minH = spec.Height()
	}
	p.Body = padPanelBody(p.Body, minH, inner)
	return p
}

// deckColumns picks how many equal columns a metrics deck of n panels uses at terminal width W: as many
// ~30-col panels as fit, rounded DOWN to a divisor of n so every sub-row is full (uniform column width,
// no lone stretched panel on a short last row). Collapses toward one column on a narrow terminal.
func deckColumns(W, n int) int {
	const minColTotal = 30 // a metrics panel reads comfortably at ~30 cols total
	fit := (W + 1) / (minColTotal + 1)
	if fit < 1 {
		fit = 1
	}
	if fit > n {
		fit = n
	}
	for c := fit; c > 1; c-- {
		if n%c == 0 {
			return c
		}
	}
	return 1
}

// cognitionPanelGrid renders the given rail panels full-width at `cols` columns — the generic
// per-system tab (Conscious / Action / Systems). Each panel is titled + border-pulsed exactly like the
// overview, but rendered into the full tab width, and sized so the tab's panels fill the screen height.
func (a *App) cognitionPanelGrid(ids []string, cols int) string {
	if cols < 1 {
		cols = 1
	}
	if len(ids) == 0 {
		return faintLine("(no panels)")
	}
	inner := a.colInnerWidth(cols)
	rows := (len(ids) + cols - 1) / cols
	minH := a.cogBodyHeight() / rows
	if minH < 5 {
		minH = 5
	}
	vm := a.vm
	vm.Width = inner
	panels := make([]cognition.Panel, 0, len(ids))
	heights := make([]int, 0, len(ids))
	for _, id := range ids {
		p := cognition.RenderPanel(id, vm)
		p.Body = a.panelTitle(railTitle(id)) + "\n" + p.Body
		if hex := a.borderHexFor(id); hex != "" {
			p = p.WithBorder(hex)
		}
		panels = append(panels, p)
		heights = append(heights, minH)
	}
	return cognition.Dashboard(a.w, nil, gridRows(panels, heights, cols, inner))
}

// cognitionSubconsciousView is the Subconscious full-screen tab: a dispatch header (θ + the recognised
// workflow) and the hidden-seam panel across the top, then ONE dedicated card per scanned
// specialist/sub-agent below ("a dedicated panel per item").
func (a *App) cognitionSubconsciousView() string {
	mut := lipgloss.NewStyle().Foreground(Pal.Muted)
	val := lipgloss.NewStyle().Foreground(Pal.Text)
	headBody := mut.Render("dispatch θ = ") + val.Render(ftoa2(a.vm.Snap.Theta)) +
		mut.Render("   (a specialist fires when its relevance crosses θ)")
	if wf := a.vm.Snap.Workflow; wf != nil && wf.Recognized {
		headBody += "\n" + lipgloss.NewStyle().Foreground(Pal.Accent).Render(
			fmt.Sprintf("workflow: %s · phase %d (%s)", wf.Name, wf.PhaseIndex, wf.OpName))
	}
	header := cognition.Panel{Body: a.panelTitle("SUBCONSCIOUS — dispatch (pull, θ-gated)") + "\n" + headBody}
	if hex := a.borderHexFor("subconscious"); hex != "" {
		header = header.WithBorder(hex)
	}
	rows := []cognition.Row{cognition.R(header)}

	fullVM := a.vm
	fullVM.Width = a.colInnerWidth(1)

	// the GENERATIVE layer (operators → synthesised programs → sub-agents → skills), full width.
	gen := cognition.RenderPanel("generative", fullVM)
	gen.Body = a.panelTitle(railTitle("generative")) + "\n" + gen.Body
	if hex := a.borderHexFor("subconscious"); hex != "" {
		gen = gen.WithBorder(hex)
	}
	rows = append(rows, cognition.R(gen))

	// the hidden seam (FILTER → GATE → TRANSFORM): the subconscious → conscious laundering, full width.
	seam := cognition.RenderPanel("seam", fullVM)
	seam.Body = a.panelTitle(railTitle("seam")) + "\n" + seam.Body
	if hex := a.borderHexFor("seam"); hex != "" {
		seam = seam.WithBorder(hex)
	}
	rows = append(rows, cognition.R(seam))

	// the M3 SOURCING ladder + KNOWLEDGE registry — the silent fuel layer that feeds the seam (the
	// ladder concretizes a fuel-needing move before it crosses; the knowledge registry is rung 2). Both
	// full-width, in the same Subconscious layer they belong to (one component → one panel in its tab).
	for _, id := range []string{"sourcing", "knowledge"} {
		p := cognition.RenderPanel(id, fullVM)
		p.Body = a.panelTitle(railTitle(id)) + "\n" + p.Body
		if hex := a.borderHexFor(id); hex != "" {
			p = p.WithBorder(hex)
		}
		rows = append(rows, cognition.R(p))
	}

	// one dedicated card per scanned specialist, 1/2/3 columns by width.
	cardCols := a.cognitionColumns()
	if a.w >= 150 {
		cardCols = 3
	}
	cardInner := a.colInnerWidth(cardCols)
	cvm := a.vm
	cvm.Width = cardInner
	cards := cognition.SubconsciousCards(cvm)
	cardHeights := make([]int, len(cards))
	for i := range cardHeights {
		cardHeights[i] = 7
	}
	rows = append(rows, gridRows(cards, cardHeights, cardCols, cardInner)...)
	return cognition.Dashboard(a.w, nil, rows)
}

// faintLine is a single faint line (the empty-tab placeholder).
func faintLine(s string) string {
	return lipgloss.NewStyle().Foreground(Pal.Faint).Render(s)
}
