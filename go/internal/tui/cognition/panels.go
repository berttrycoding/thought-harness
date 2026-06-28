// panels.go — the 9 MIND-rail panel renderers, a one-to-one port of tui/panels.py's render_*
// functions (DESIGN §4.3). Each render_<pid> is a PURE VIEW over a ViewModel (the end-of-tick
// SnapshotData + the recent event stream) and returns a Panel (a plain {Body} value, never a
// pre-sized string) — width is decided once, in Dashboard. The rail table (var rail) is the port of
// tui/widgets/mind_rail.py's RAIL: the 9 panels in order, with their ids, border titles, and fixed
// heights. A recover() per panel render shows "[render error]" in that pane only, never blanking the
// whole rail (Python MindRail.refresh_panels's per-pane try/except).
//
// Styling follows the monochrome-minimalist language (panels.py / theme.py): a grayscale ramp
// (colText -> colSubtext -> colMuted -> colFaint) carries structure, the single steel-blue colAccent
// marks the active line / voiced thought / chosen candidate, and the muted semantic tones (colOk /
// colWarn / colErr) appear only where colour is genuine signal — a passed check, an admit/reject
// verdict, a stability violation.
//
// LAYOUT CONTRACT (two hard rules, both learned the hard way):
//
//  1. A styled run NEVER contains "\n". lipgloss.Render("X\n") pads the trailing empty line out to
//     len("X") spaces; appending the next run after that lands it AFTER the padding, shoving every
//     following line right by the previous line's width (the "right-shift" bug). So each renderer
//     builds a []string of WHOLE lines — each line is one join(runs...) with no embedded newline — and
//     joins them with "\n" only at the very end, outside all styling.
//
//  2. Prose WRAPS, it never clips. The panels render into a column whose inner width is vm.Width (the
//     app measures it per frame; contentW falls back to a default for tests). Long prose (thought text,
//     a critic reason, an event summary, the re-voiced thought, …) is word-wrapped with a HANGING
//     INDENT (wrapEntry) so continuation lines line up under the first line's text — the full text is
//     always shown, never truncated. Only short scalar values (confidence, V, a metric) right-align to
//     the width to form a clean trailing column, and fixed tabular labels (domain/state) keep their
//     column width. We wrap ourselves rather than let lipgloss reflow, so the indent + columns are ours.
package cognition

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// ViewModel is the per-frame snapshot the panels read: the end-of-tick engine SnapshotData plus the
// recent event stream (the bounded tail of the bus the panels.py functions read off `eng.bus.log`).
// The bridge folds an eventBatchMsg + a SnapshotData into this each frame; the panels never touch the
// engine. Keeping Events here is what lets render_subconscious/seam/action/value/trace stay pure
// views — they scan this slice exactly as panels.py scans eng.bus.log.
type ViewModel struct {
	Snap   SnapshotData   // the end-of-tick live-engine reads (DESIGN §4.3)
	Events []events.Event // the recent bus tail (oldest-first), bounded by the app's ring
	Width  int            // inner content width the panel renders into (0 ⇒ the contentW default)
}

// panelSpec is one rail row: the panel id, its border title, and its fixed height (the port of
// mind_rail.py's RAIL triples). The id keys the render dispatch; the height sizes the boxed panel.
type panelSpec struct {
	id     string
	title  string
	height int
}

// rail is the panel REGISTRY — every panel's id, border title, and minimum height. It is the full set
// the app iterates to detect per-panel body changes (the shuffle cue); the on-screen ARRANGEMENT (which
// panels share a deck row vs. take a full-width row) is Layout(), separately. The hybrid subsystems are
// split here into a `_metrics` panel (compact, joins a deck) and a `_text` panel (full-width, readable):
// WATCHED, CRITIC, LIFECYCLE, MEMORY each contribute two ids. height is a MINIMUM (panels grow to fit
// wrapped content; padPanelBody pads a sparse panel up to this floor).
var rail = []panelSpec{
	{"dashboard", "DASHBOARD · all subsystems (compact)", 8},
	{"conscious", "CONSCIOUS · thought graph", 6},
	{"frontier", "CONSCIOUS · search (A* frontier)", 6},
	{"mcp", "CONSCIOUS · metacognitive ops (Thought MCP)", 6},
	{"subconscious", "SUBCONSCIOUS · specialists", 6},
	{"generative", "GENERATIVE · operators → programs → sub-agents", 6},
	{"sourcing", "SOURCING · the fuel ladder → concretize (M3)", 7},
	{"knowledge", "KNOWLEDGE · domain registry (M3)", 6},
	{"retrieval", "RETRIEVAL · hybrid scores + memory stream", 7},
	{"persist", "PERSISTENCE · store + curator (M4)", 7},
	{"config_events", "CONFIG · load · toggle · skip (M1)", 5},
	{"value", "VALUE", 5},
	{"durability", "DURABILITY", 8},
	{"scheduler", "SCHEDULER · LLM-call budget", 5},
	{"backend", "BACKEND · model health", 5},
	{"seam", "HIDDEN SEAM · FILTER → GATE → TRANSFORM", 6},
	{"action_text", "WATCHED / ACTION", 4},
	{"action_metrics", "WATCHED · async", 5},
	{"toolexec", "ACTION · tool execution (reality)", 5},
	{"critic_metrics", "CRITIC", 6},
	{"critic_text", "CRITIC · reason", 3},
	{"lifecycle_metrics", "LIFECYCLE", 5},
	{"lifecycle_text", "LIFECYCLE · transitions", 4},
	{"continuous", "CONTINUOUS · arousal · drives · default-mode", 5},
	{"perception", "PERCEPTION · senses (clock · orient)", 6},
	{"convert", "CONVERTIBILITY · effortful → automatic (live)", 5},
	{"memory_metrics", "MEMORY", 5},
	{"memory_text", "MEMORY · learned", 3},
	{"grounding", "GROUNDING · reality ledger (SR-4)", 8},
	{"session", "RUNTIME · session spawn tree (P3.3)", 6},
	{"trace", "TRACE · event tape", 8},
}

// LayoutRow is one on-screen row: one id ⇒ a FULL-WIDTH panel (readable text); two-or-more ids ⇒ a
// compact metrics DECK rendered as equal columns (the app reflows a deck into fewer columns / multiple
// sub-rows when the terminal is too narrow). The order is the cognition flow (DESIGN: interleaved by
// subsystem) — the thought stream, the engine vitals behind it, the seam that voiced it, the action it
// took, the control vitals + their reasons, then the raw tape.
type LayoutRow struct{ IDs []string }

// cogLayout is the COGNITION-mode arrangement: full-width text rows interleaved with metric decks.
var cogLayout = []LayoutRow{
	{[]string{"dashboard"}},                           // the all-subsystem compact status header
	{[]string{"conscious"}},                           // text, full width
	{[]string{"subconscious", "value", "durability"}}, // engine metrics deck
	{[]string{"seam"}},                                // text, full width
	{[]string{"action_text"}},                         // text, full width
	{[]string{"action_metrics", "critic_metrics", "lifecycle_metrics", "memory_metrics"}}, // control metrics deck
	{[]string{"critic_text"}},    // text, full width
	{[]string{"lifecycle_text"}}, // text, full width
	{[]string{"memory_text"}},    // text, full width
	{[]string{"grounding"}},      // text, full width — the reality-grounding ledger (SR-4)
	{[]string{"perception"}},     // text, full width — the live senses (clock · orient) from the power-cycle
	{[]string{"session"}},        // text, full width — the runtime session spawn tree (P3.3)
	{[]string{"trace"}},          // text, full width
}

// Rail returns the panel registry (every panel's id/title/minHeight) for the app to drive per-panel
// change detection + animation. Arrangement is Layout().
func Rail() []panelSpec { return rail }

// Layout returns the on-screen row arrangement (full-width rows + metric decks, in flow order).
func Layout() []LayoutRow { return cogLayout }

// SpecByID returns the registry entry for a panel id (and whether it exists).
func SpecByID(id string) (panelSpec, bool) {
	for _, s := range rail {
		if s.id == id {
			return s, true
		}
	}
	return panelSpec{}, false
}

// ID / Title / Height expose a spec's fields for the app (which builds the Dashboard body rows).
func (s panelSpec) ID() string    { return s.id }
func (s panelSpec) Title() string { return s.title }
func (s panelSpec) Height() int   { return s.height }

// RenderPanel dispatches to the render_<pid> for a panel id and returns its boxed-ready Panel body,
// wrapping the render in a recover() so a single bad panel shows "[render error] <msg>" in its own
// pane only — never blanking the rail (Python MindRail.refresh_panels per-pane try/except).
func RenderPanel(id string, vm ViewModel) (p Panel) {
	defer func() {
		if r := recover(); r != nil {
			p = Panel{Body: faintStr(fmt.Sprintf("[render error] %v", r))}
		}
	}()
	switch id {
	case "conscious":
		return renderConscious(vm)
	case "subconscious":
		return renderSubconscious(vm)
	case "generative":
		return renderGenerative(vm)
	case "sourcing":
		return renderSourcing(vm)
	case "knowledge":
		return renderKnowledge(vm)
	case "retrieval":
		return renderRetrieval(vm)
	case "persist":
		return renderPersist(vm)
	case "config_events":
		return renderConfigEvents(vm)
	case "mcp":
		return renderMCP(vm)
	case "frontier":
		return renderFrontier(vm)
	case "toolexec":
		return renderToolExec(vm)
	case "scheduler":
		return renderScheduler(vm)
	case "backend":
		return renderBackend(vm)
	case "continuous":
		return renderContinuous(vm)
	case "perception":
		return renderPerception(vm)
	case "convert":
		return renderConvert(vm)
	case "seam":
		return renderSeam(vm)
	case "action_text":
		return renderActionText(vm)
	case "action_metrics":
		return renderActionMetrics(vm)
	case "critic_metrics":
		return renderCriticMetrics(vm)
	case "critic_text":
		return renderCriticText(vm)
	case "durability":
		return renderDurability(vm)
	case "value":
		return renderValue(vm)
	case "lifecycle_metrics":
		return renderLifecycleMetrics(vm)
	case "lifecycle_text":
		return renderLifecycleText(vm)
	case "memory_metrics":
		return renderMemoryMetrics(vm)
	case "memory_text":
		return renderMemoryText(vm)
	case "trace":
		return renderTrace(vm)
	case "grounding":
		return renderGrounding(vm)
	case "session":
		return renderSession(vm)
	case "dashboard":
		return renderDashboard(vm)
	default:
		return Panel{Body: faintStr("(unknown panel " + id + ")")}
	}
}

// -- the styled-run builder (Rich Text.append, in lipgloss) ----------------------------------------

// run is one styled segment — the Go form of rich.Text.append(text, style). A panel body is a
// sequence of runs joined; styling a run is a lipgloss Foreground (+ optional Bold) over its text.
type run struct {
	text  string
	color lipgloss.Color
}

// txt builds a styled run (the Python `Text(s, style=color)` / `out.append(s, style=color)`).
func txt(s string, c lipgloss.Color) run { return run{text: s, color: c} }

// btxt formerly built a BOLD run (the Python `style=f"bold {color}"`); bold styling has been removed
// harness-wide, so it is now an alias of txt — kept so the panels.py-mirroring call sites read
// unchanged (a bold emphasis becomes a plain foreground tone, DESIGN §5).
func btxt(s string, c lipgloss.Color) run { return txt(s, c) }

// render renders one run through lipgloss (foreground-only, no bold, DESIGN §5).
func (r run) render() string {
	return lipgloss.NewStyle().Foreground(r.color).Render(r.text)
}

// join concatenates a run sequence into one body string (Rich's Text + Group, flattened to a panel
// body — the rail panels stack their runs vertically via embedded "\n" in the run text, exactly as
// panels.py builds a Text with newline-bearing appends).
func join(runs ...run) string {
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(r.render())
	}
	return b.String()
}

// faintStr is the dim "(empty)" / error voicing used where a panel has nothing to show.
func faintStr(s string) string { return lipgloss.NewStyle().Foreground(colFaint).Render(s) }

// warnStr is the amber voicing for a "this is stale / degraded but not an error" note (e.g. an awake
// scan that hasn't refreshed for several ticks).
func warnStr(s string) string { return lipgloss.NewStyle().Foreground(colWarn).Render(s) }

// -- the shared glyph helpers (panels.py _bar / _spark + the sparkline ramp) -----------------------

const spark = "▁▂▃▄▅▆▇█"

// bar renders a 10-wide fill bar (panels.py _bar): `round(value*width)` "█" then "·" to width.
func bar(value float64, width int) string {
	n := int(math.Round(value * float64(width)))
	if n < 0 {
		n = 0
	}
	if n > width {
		n = width
	}
	return strings.Repeat("█", n) + strings.Repeat("·", width-n)
}

// sparkline renders a value series as the 8-level ramp (panels.py _spark): min/max normalised, each
// value mapped to one of ▁..█. An empty series is "" (the panel skips the line).
func sparkline(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	span := hi - lo
	if span == 0 {
		span = 1.0
	}
	r := []rune(spark)
	var b strings.Builder
	for _, v := range values {
		idx := int((v - lo) / span * 7)
		if idx < 0 {
			idx = 0
		}
		if idx > 7 {
			idx = 7
		}
		b.WriteRune(r[idx])
	}
	return b.String()
}

// padRight pads s with spaces to a VISIBLE width of w (the Python `f"{s:<w}"` on plain text). Used for
// the label/flag columns; the inputs here are plain (unstyled) so a byte width == a visible width.
func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// clip truncates s to n VISIBLE COLUMNS (display width, not rune count) so a wide rune (CJK, an
// emoji) that occupies two cells doesn't overflow the column budget the caller measured (L6). It is
// ANSI-aware via x/ansi.Truncate, so it is also safe on the few call sites that pass a styled string.
// No ellipsis (tail "") — call sites budget exactly n columns and clip hard, the Python `s[:n]` shape.
// n<=0 ⇒ "".
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "")
}

// contentW is the inner width budget a panel renders into: the app-measured vm.Width when present, or
// a sane default for tests / the first paint before a size is known. Every renderer clips to this so a
// composed line never overflows the column (which would make lipgloss wrap it, DESIGN layout rule 2).
func contentW(vm ViewModel) int {
	if vm.Width > 0 {
		return vm.Width
	}
	return 64
}

// lineLR composes one whole line with `left` flush-left and `right` flush-right within width w (both
// args are already-styled, possibly-ANSI strings; widths are measured visibly). When the two would
// collide, left is ANSI-clipped to make room so the line never exceeds w. This is what aligns the
// trailing value columns (confidence, V, metric value) into a clean right-hand column.
func lineLR(left, right string, w int) string {
	rw := lipgloss.Width(right)
	gap := w - lipgloss.Width(left) - rw
	if gap < 1 {
		left = ansi.Truncate(left, max(0, w-rw-1), "…")
		gap = w - lipgloss.Width(left) - rw
	}
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// leaderRow renders "<label> ········· <value>" — label flush-left, value flush-right, a faint dot
// leader filling the gap so the eye tracks across to the value (the dashboard rows: subconscious θ,
// durability metrics, memory counts). label/value are plain text styled here with lc/vc.
func leaderRow(label string, lc lipgloss.Color, value string, vc lipgloss.Color, w int) string {
	vw := lipgloss.Width(value)
	if d := w - vw - 2; d > 0 && lipgloss.Width(label) > d { // keep at least " " + value visible
		// the label does not fit — clip WITH an ellipsis so the truncation is visible (a hard cut
		// mid-word, e.g. a long SLAM knob description, otherwise reads as if the label simply ends).
		label = ansi.Truncate(label, d, "…")
	}
	gap := w - lipgloss.Width(label) - vw
	var leader string
	switch {
	case gap >= 4:
		leader = " " + strings.Repeat("·", gap-2) + " "
	case gap > 0:
		leader = strings.Repeat(" ", gap)
	}
	return join(txt(label, lc), txt(leader, colFaint), txt(value, vc))
}

// wrapPlain word-wraps plain prose so the FIRST output line fits firstW columns and every following
// line fits restW (firstW lets a caller reserve room on line 1 for a right-aligned value such as a
// confidence). A single token longer than the line width is hard-split rather than overflow. Text is
// NEVER dropped — this is the "no clipping" rule: long content wraps, it is never truncated. Whitespace
// (incl. newlines) is collapsed to single spaces (our fields are single-line strings).
func wrapPlain(s string, firstW, restW int) []string {
	if firstW < 1 {
		firstW = 1
	}
	if restW < 1 {
		restW = 1
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	cur := ""
	lineW := func() int {
		if len(out) == 0 {
			return firstW
		}
		return restW
	}
	for _, word := range words {
		r := []rune(word)
		for len(r) > lineW() { // hard-split a token wider than the current line
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			n := lineW()
			out = append(out, string(r[:n]))
			r = r[n:]
		}
		word = string(r)
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= lineW():
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// wrapEntry renders one logical entry as styled lines with a HANGING INDENT: line 1 is `prefix` (an
// already-styled label of visible width prefixW) followed by `text` (styled tc) word-wrapped into the
// remaining width; continuation lines are indented by prefixW so the wrapped text hangs cleanly under
// the first line's text. The full text is always shown (wrapped, never clipped).
func wrapEntry(prefix string, prefixW int, text string, tc lipgloss.Color, w int) []string {
	avail := w - prefixW
	if avail < 8 {
		// the prefix is too wide to leave usable room beside it (a long event kind in a tiny panel):
		// keep the prefix on its own line and wrap the text full-width below it (hanging indent 2). The
		// only place anything is dropped is if the prefix ALONE exceeds w — then it's truncated, which
		// at these widths costs at most a trailing pad space.
		out := []string{ansi.Truncate(prefix, w, "")}
		for _, ln := range wrapPlain(text, w-2, w-2) {
			out = append(out, "  "+txt(ln, tc).render())
		}
		return out
	}
	indent := strings.Repeat(" ", prefixW)
	out := []string{}
	for i, ln := range wrapPlain(text, avail, avail) {
		if i == 0 {
			out = append(out, prefix+txt(ln, tc).render())
		} else {
			out = append(out, indent+txt(ln, tc).render())
		}
	}
	return out
}

// -- event helpers (the panels read the ViewModel's recent stream, panels.py reads eng.bus.log) ----

// lastEvent returns the most-recent event whose kind is in kinds, or nil (panels.py _last).
func lastEvent(vm ViewModel, kinds ...string) *events.Event {
	for i := len(vm.Events) - 1; i >= 0; i-- {
		for _, k := range kinds {
			if vm.Events[i].Kind == k {
				return &vm.Events[i]
			}
		}
	}
	return nil
}

// eventsOfKind returns the LAST n events of a single kind, oldest-first (panels.py's
// `[e for e in eng.bus.log if e.kind==K][-n:]`).
func eventsOfKind(vm ViewModel, kind string, n int) []events.Event {
	var out []events.Event
	for _, e := range vm.Events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// dataStr reads a string off an event's Data map (Python e.data.get(k, "")), tolerating absence /
// a non-string value with "".
func dataStr(e *events.Event, k string) string {
	if e == nil {
		return ""
	}
	if v, ok := e.Data[k].(string); ok {
		return v
	}
	return ""
}

// dataFloat reads a float off an event's Data map (Python e.data.get(k, 0)), coercing int/float and
// defaulting to 0.
func dataFloat(e *events.Event, k string) float64 {
	if e == nil {
		return 0
	}
	switch v := e.Data[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// dataBool reads a bool off an event's Data map (Python e.data.get(k)), defaulting to false.
func dataBool(e *events.Event, k string) bool {
	if e == nil {
		return false
	}
	b, _ := e.Data[k].(bool)
	return b
}

// dataStrings reads a string slice off an event's Data map (the gate's "losers" domain list), tolerating
// the wire's []any (each elem a string) or a native []string, and absence (nil).
func dataStrings(e *events.Event, k string) []string {
	if e == nil {
		return nil
	}
	switch v := e.Data[k].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// ===================================================================================================
// the 9 render_<pid> — ported one-to-one from panels.py
// ===================================================================================================

// renderConscious — the thought graph: the EXPANDED branch's last-12 thoughts (#id, 3-letter source
// tag, text clipped to 58 + confidence), then the branch tree summary, then the A*-search summary.
// (panels.py render_conscious.)
func renderConscious(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	if s.ActiveBranch == nil {
		return Panel{Body: faintStr(clip("(idle — no active thought graph; submit a prompt)", w))}
	}
	var lines []string
	ctx := s.ActiveContext
	if len(ctx) > 12 {
		ctx = ctx[len(ctx)-12:]
	}
	for _, th := range ctx {
		color := SrcColorFor(th.Source)
		// "#id  TAG  text" with a hanging indent (continuation lines align under the text); the
		// confidence (faint) right-aligns on line 1, with its width reserved out of line 1's wrap budget.
		prefix := join(
			txt(padLeft(fmt.Sprintf("#%d", th.ID), 4)+" ", colFaint),
			txt(padRight(th.SourceTag, 4)+" ", color),
		)
		const prefixW = 10
		conf := ""
		if th.Confidence != 0 {
			conf = fmt.Sprintf("%.2f", th.Confidence)
		}
		avail := w - prefixW
		firstW := avail
		if conf != "" {
			firstW = avail - lipgloss.Width(conf) - 1
		}
		indent := strings.Repeat(" ", prefixW)
		for i, ln := range wrapPlain(th.Text, firstW, avail) {
			if i == 0 {
				left := prefix + txt(ln, color).render()
				lines = append(lines, lineLR(left, txt(conf, colFaint).render(), w))
			} else {
				lines = append(lines, indent+txt(ln, color).render())
			}
		}
	}
	// branch tree summary — one whole line, clipped to width. Capped to the top branches by V (active
	// always shown) with a total count: at 100+ branches the raw list truncated after ~6 with no hint
	// of how many there were (L4).
	var tree strings.Builder
	treeBranches, treeOmitted := topBranchesByValue(s.Branches, maxBranchRows, s.ActiveBranch)
	tree.WriteString(txt(fmt.Sprintf("branches: %d · ", len(s.Branches)), colMuted).render())
	for _, b := range treeBranches {
		active := s.ActiveBranch != nil && b.ID == *s.ActiveBranch
		mark := "·"
		switch {
		case active:
			mark = "●"
		case b.Status == "STASHED":
			mark = "○"
		}
		res := "C"
		if b.Resolution == "EXPANDED" {
			res = "E"
		}
		label := fmt.Sprintf("%sb%d[%s v%.2f] ", mark, b.ID, res, b.Value)
		c := colFaint
		switch {
		case active:
			c = colAccent
		case b.Status == "STASHED":
			c = colMuted
		}
		tree.WriteString(txt(label, c).render())
	}
	if treeOmitted > 0 {
		tree.WriteString(txt(fmt.Sprintf("+%d…", treeOmitted), colFaint).render())
	}
	lines = append(lines, ansi.Truncate(tree.String(), w, "…"))
	// the same graph, seen as A* best-first search (the value signal is the heuristic).
	for _, ln := range wrapPlain(searchSummary(s), w, w) {
		lines = append(lines, txt(ln, colFaint).render())
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// searchSummary mirrors search.SearchView(g).summary() at the view layer: the A* framing is "frontier
// (stashed) vs explored (active), best V leads". The Go engine's SearchView is not reachable here
// (engine-free); the snapshot carries the branches + values, so the summary is reconstructed from
// them — frontier count, the best-V branch as the heuristic leader.
func searchSummary(s SnapshotData) string {
	if len(s.Branches) == 0 {
		return "search: (no frontier)"
	}
	frontier := 0
	bestID, bestV := -1, math.Inf(-1)
	for _, b := range s.Branches {
		if b.Status == "STASHED" {
			frontier++
		}
		if b.Value > bestV {
			bestV, bestID = b.Value, b.ID
		}
	}
	return fmt.Sprintf("search: frontier=%d  best b%d (V=%.2f) leads", frontier, bestID, bestV)
}

// renderSubconscious — the specialist scan: a per-domain relevance bar (fired domains in accent), the
// admission θ, and the live recognised-workflow line. (panels.py render_subconscious.)
func renderSubconscious(vm ViewModel) Panel {
	ev := lastEvent(vm, events.SubDispatch, events.SubQuiet)
	scan := scanPrimitiveSubAgents(vm)
	w := contentW(vm)
	var lines []string
	// head: θ line + the workflow line (when one is recognised), each a single clipped line.
	if ev != nil {
		if theta, ok := asNum(ev.Data["theta"]); ok {
			lines = append(lines, leaderRow("θ (admission)", colMuted, fmt.Sprintf("%.2f", theta), colText, w))
		}
	}
	if wf := vm.Snap.Workflow; wf != nil && wf.Recognized {
		body := fmt.Sprintf("%s · phase %d (%s)", wf.Name, wf.PhaseIndex, wf.OpName)
		lines = append(lines, wrapEntry(txt("workflow: ", colAccent).render(), 10, body, colAccent, w)...)
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	// body: one row per scanned specialist — domain[:12], a 10-wide effective bar, the .2f, ◀fire.
	for _, sc := range scan {
		dom := ""
		if d, ok := sc["domain"].(string); ok {
			dom = d
		}
		eff, _ := asNum(sc["effective"])
		fired, _ := sc["fired"].(bool)
		c := colFaint
		fireTag := ""
		if fired {
			c = colAccent
			fireTag = " ◀fire"
		}
		row := join(
			txt(padRight(clip(dom, 12), 12)+" ", c),
			txt(bar(eff, 10), c),
			txt(fmt.Sprintf(" %5.2f", eff), colText),
			txt(fireTag, c),
		)
		lines = append(lines, ansi.Truncate(row, w, "…"))
	}
	if len(scan) == 0 {
		lines = append(lines, faintStr(clip("(no specialists scanned yet)", w)))
	} else if age, ok := scanStaleTicks(vm); ok {
		// awake non-dispatch ticks leave the scan unrefreshed — say so rather than imply it is live (E2).
		lines = append(lines, warnStr(clip(fmt.Sprintf("idle %d tick(s) — scan above is the last dispatch", age), w)))
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderMemoryMetrics — the MEMORY deck panel (the numbers): the minted-count per kind (specialists /
// skills / operators, effortful→automatic) as right-aligned leader rows, plus the learned gate priors
// (compiled control habits, domain→+bias, top 4 by |bias|). The names themselves live in the text panel.
func renderMemoryMetrics(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	var lines []string
	lines = append(lines, leaderRow("specialists", colText, fmt.Sprintf("%d", len(s.MintedPrimitiveSubAgents)), colText, w))
	lines = append(lines, leaderRow("skills", colText, fmt.Sprintf("%d", len(s.MintedSkills)), colText, w))
	lines = append(lines, leaderRow("operators", colText, fmt.Sprintf("%d", len(s.MintedOperators)), colText, w))
	// declarative memory (P2.3): grounded episodes · valid beliefs · learned preferences (full entries
	// in the Registry tab). Accented when non-empty so a populating memory is visible at a glance.
	declc := colMuted
	if s.EpisodicCount+s.SemanticCount+s.PersonCount > 0 {
		declc = colText
	}
	lines = append(lines, leaderRow("episodes·beliefs·prefs", colMuted,
		fmt.Sprintf("%d·%d·%d", s.EpisodicCount, s.SemanticCount, s.PersonCount), declc, w))
	// the shared retriever's mode (P1.x): hybrid (lexical+semantic) when an embedder is reachable, else
	// lexical-only — accented when hybrid (the semantic side is live).
	if s.RetrieverMode != "" {
		rc := colMuted
		if s.RetrieverMode == "hybrid" {
			rc = colOk
		}
		lines = append(lines, leaderRow("retriever", colMuted, s.RetrieverMode, rc, w))
	}
	if len(s.GatePriors) > 0 {
		lines = append(lines, txt("gate priors", colMuted).render())
		type kv struct {
			dom string
			w   float64
		}
		kvs := make([]kv, 0, len(s.GatePriors))
		for d, val := range s.GatePriors {
			kvs = append(kvs, kv{d, val})
		}
		sort.Slice(kvs, func(i, j int) bool { return math.Abs(kvs[i].w) > math.Abs(kvs[j].w) })
		if len(kvs) > 4 {
			kvs = kvs[:4]
		}
		for _, e := range kvs {
			lines = append(lines, leaderRow("  "+e.dom, colSubtext, fmt.Sprintf("%+.2f", e.w), colAccent, w))
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderMemoryText — the MEMORY full-width text panel: the minted NAMES per kind (the actual learned
// specialist/skill/operator names, wrapped) and the conversation-memory turn count. When nothing has
// been minted it says so but still reports the turn count (the harness's episodic memory).
func renderMemoryText(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	var lines []string
	named := func(label string, names []string, c lipgloss.Color) {
		if len(names) == 0 {
			return
		}
		for _, ln := range wrapEntry(txt(label+": ", colMuted).render(), lipgloss.Width(label)+2, strings.Join(names, ", "), c, w) {
			lines = append(lines, ln)
		}
	}
	named("specialists", s.MintedPrimitiveSubAgents, colAccent)
	named("skills", s.MintedSkills, colAccent)
	named("operators", s.MintedOperators, colSubtext)
	if len(lines) == 0 {
		lines = append(lines, faintStr(clip("(nothing minted yet)", w)))
	}
	for _, ln := range wrapPlain(fmt.Sprintf("conversation memory: %d turns", s.TranscriptTurns), w, w) {
		lines = append(lines, txt(ln, colFaint).render())
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderSeam — the hidden-seam pipeline FILTER -> GATE -> TRANSFORM: the last-4 filter verdicts
// (text[:40]), the gate winner (+ conflict fork), the transform raw/voiced (each [:44]). (panels.py
// render_seam.)
func renderSeam(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string
	lines = append(lines, txt("FILTER (admit raw)", colSubtext).render())
	filters := eventsOfKind(vm, events.Filter, 4)
	if len(filters) == 0 {
		lines = append(lines, faintStr("  (nothing at intake)"))
	}
	for i := range filters {
		e := &filters[i]
		v := dataStr(e, "verdict")
		// "  VERDICT 0.NN <text wrapped/hanging>" — verdict (6) + confidence (5) are the fixed prefix,
		// then the filter's REASON (why admitted/rejected/flagged) as a faint hanging line beneath it.
		prefix := join(
			txt("  "+padRight(v, 6)+" ", VerdictColorFor(v)),
			txt(fmt.Sprintf("%.2f ", dataFloat(e, "confidence")), colFaint),
		)
		lines = append(lines, wrapEntry(prefix, lipgloss.Width(ansi.Strip(prefix)), dataStr(e, "text"), colSubtext, w)...)
		if reason := dataStr(e, "reason"); reason != "" {
			lines = append(lines, wrapEntry(txt("         ↳ ", colFaint).render(), 11, reason, colFaint, w)...)
		}
	}
	// Pattern-C floor-stands (Rule 4): the Filter's deterministic admission FLOOR standing because the
	// model was not consulted or declined. Appears only in an llm/hybrid run (the default control mode
	// escalates nothing); the admit FLOOR itself is pure control (internal/control), the model is an
	// optional ceiling above it.
	if n := countFloorStands(vm, "filter.admit"); n > 0 {
		lines = append(lines, lineLR(txt("  floor stood (no escalation)", colMuted).render(),
			txt(fmt.Sprintf("×%d", n), colWarn).render(), w))
	}
	lines = append(lines, txt("GATE (arbitrate)", colSubtext).render())
	if gate := lastEvent(vm, events.Gate); gate != nil {
		// the arbitration leaderboard: the winner, then the losers it beat (the admitted survivors that
		// did not win). A conflict (>1 distinct stance among losers) forks them instead of discarding.
		lines = append(lines, txt("  winner: ", colMuted).render()+txt(dataStr(gate, "winner"), colAccent).render())
		if losers := dataStrings(gate, "losers"); len(losers) > 0 {
			lines = append(lines, wrapEntry(txt("  over:   ", colMuted).render(), 9, strings.Join(losers, ", "), colFaint, w)...)
		}
		if dataBool(gate, "conflict") {
			lines = append(lines, txt(clip("  [CONFLICT → fork losers]", w), colErr).render())
		}
	}
	lines = append(lines, txt("TRANSFORM (re-voice)", colSubtext).render())
	if trans := lastEvent(vm, events.Transform); trans != nil {
		lines = append(lines, wrapEntry(txt("  raw:    ", colFaint).render(), 10, dataStr(trans, "raw"), colFaint, w)...)
		lines = append(lines, wrapEntry(txt("  voiced: ", colAccent).render(), 10, dataStr(trans, "voiced"), colAccent, w)...)
	}
	// the seam is also a view-PRODUCER (SR-2): a consumer's context is assembled through a template.
	if asm := lastEvent(vm, events.Assemble); asm != nil {
		bud := "off"
		if b := int(dataFloat(asm, "budget")); b > 0 {
			bud = fmt.Sprintf("%dw", b)
		}
		lines = append(lines, txt("ASSEMBLE (view-producer)", colSubtext).render())
		lines = append(lines, txt(clip(fmt.Sprintf("  %s · %d items · budget %s",
			dataStr(asm, "template"), int(dataFloat(asm, "items")), bud), w), colMuted).render())
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderActionText — the watched-seam TEXT (full width): the last intention out and the last reality
// (observation) in (green ok / red fail), each wrapped with a hanging indent so the full action /
// observation reads on the line. The async/acted COUNTERS live in renderActionMetrics (the deck).
func renderActionText(vm ViewModel) Panel {
	w := contentW(vm)
	const prefixW = 12 // "intention   " / "reality     " are 12 cols
	var lines []string
	intentText := "—"
	if intent := lastEvent(vm, events.Intention); intent != nil {
		intentText = dataStr(intent, "text")
	}
	lines = append(lines, wrapEntry(txt("intention   ", colMuted).render(), prefixW, intentText, colSubtext, w)...)
	if obs := lastEvent(vm, events.Observation); obs != nil {
		oc := colErr
		if dataBool(obs, "ok") {
			oc = colOk
		}
		lines = append(lines, wrapEntry(txt("reality     ", colMuted).render(), prefixW, obs.Summary, oc, w)...)
	} else {
		lines = append(lines, txt("reality     ", colMuted).render()+faintStr("—"))
	}
	// HOW reality was reached (N.4 bridge: structured|scraped|none) + whether it was a tier-0 FABRICATION
	// (P0.6 — the offline stand-in makes reality up; such an "observation" can never ground). Fabricated
	// reads in the warn tone — a faked reality is a thing to SEE, not trust.
	if s := vm.Snap; s.LastBridge != "" || s.LastFabricated {
		bridge := s.LastBridge
		if bridge == "" {
			bridge = "none"
		}
		ground := txt("bridge "+bridge, colMuted).render()
		if s.LastFabricated {
			ground += txt("  ·  FABRICATED (tier-0, cannot ground)", colWarn).render()
		} else {
			ground += txt("  ·  real (grounds reality)", colOk).render()
		}
		lines = append(lines, txt("grounded    ", colMuted).render()+ansi.Truncate(ground, max(0, w-prefixW), ""))
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderActionMetrics — the watched-seam COUNTERS (deck): async actions outstanding, the dead-time τ,
// and the acted-branches set, as right-aligned leader rows.
func renderActionMetrics(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	lines := []string{
		leaderRow("outstanding", colMuted, fmt.Sprintf("%d", s.ActionOutstanding), colText, w),
		leaderRow("dead-time", colMuted, fmt.Sprintf("τ=%d", s.ActionLatencyTicks), colText, w),
		leaderRow("acted", colMuted, intSlice(s.ActedBranches), colText, w),
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderCriticMetrics — the executive's decision + the four validate-before-trust flags (deck panel):
// the decision (ACT green / STOP neutral / THINK plain / else accent) and each flag right-aligned
// (truthy = warn tone). The decision's REASON is the full-width renderCriticText.
func renderCriticMetrics(vm ViewModel) Panel {
	m := vm.Snap.LastMeta
	w := contentW(vm)
	dec := "—"
	if m != nil {
		dec = m.Decision
	}
	// "decision  STOP · GOAL_MET" — the move + (on STOP) the stopping taxonomy (goal-met vs gave-up vs
	// blocked-on-reality/user), which is the difference between success and giving up.
	decLine := txt("decision  ", colMuted).render() + txt(dec, DecColorFor(dec)).render()
	if m != nil && m.StopKind != "" {
		decLine += txt(" · "+m.StopKind, colMuted).render()
	}
	lines := []string{decLine}
	if m != nil && m.Mode != "" {
		lines = append(lines, txt("mode      ", colMuted).render()+txt(m.Mode, colSubtext).render())
	}
	flags := []struct {
		name string
		val  bool
	}{
		{"branch_exhausted", m != nil && m.BranchExhausted},
		{"loop_exhausted", m != nil && m.LoopExhausted},
		{"flagged", m != nil && m.Flagged},
		{"needs_ground_truth", m != nil && m.NeedsGroundTruth},
	}
	for _, f := range flags {
		c := colFaint
		if f.val {
			c = colWarn
		}
		lines = append(lines, lineLR(txt("  "+f.name, colMuted).render(), txt(fmt.Sprintf("%v", f.val), c).render(), w))
	}
	// the smart-hybrid escalation: when the Controller consulted the model, show control→model and
	// whether they agreed (green agree / warn override) plus the ambiguity that triggered the call.
	if m != nil && m.Escalated {
		lines = append(lines, "")
		verdict, vc := "agree", colOk
		if !m.Agree {
			verdict, vc = "override", colWarn
		}
		lines = append(lines, lineLR(
			txt(fmt.Sprintf("  control→model %s→%s", m.HeuristicDecision, m.LLMDecision), colMuted).render(),
			txt(verdict, vc).render(), w))
		lines = append(lines, faintStr(clip(fmt.Sprintf("  ambiguity %.2f", m.Ambiguity), w)))
	}
	// Pattern-C floor-stands (Rule 4): how often the deterministic decision FLOOR stood at this site
	// because the model was not consulted or declined. Appears only in llm/hybrid runs (the default
	// control mode escalates nothing). Counts critic.decide floor-stands.
	if n := countFloorStands(vm, "critic.decide"); n > 0 {
		lines = append(lines, lineLR(txt("  floor stood", colMuted).render(),
			txt(fmt.Sprintf("×%d", n), colWarn).render(), w))
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// countFloorStands tallies escalation.floor_stands events at one site (e.g. "critic.decide" /
// "filter.admit") — the Pattern-C non-escalations where the deterministic floor stood (Rule 4).
func countFloorStands(vm ViewModel, site string) int {
	n := 0
	for i := range vm.Events {
		if vm.Events[i].Kind == events.EscalationFloorStands && dataStr(&vm.Events[i], "site") == site {
			n++
		}
	}
	return n
}

// renderCriticText — the decision's reason (full width, wrapped). "(no decision yet)" before the first.
func renderCriticText(vm ViewModel) Panel {
	m := vm.Snap.LastMeta
	w := contentW(vm)
	reason := ""
	if m != nil {
		reason = m.Reason
	}
	if strings.TrimSpace(reason) == "" {
		return Panel{Body: faintStr(clip("(no decision yet)", w))}
	}
	var lines []string
	for _, ln := range wrapPlain(reason, w, w) {
		lines = append(lines, txt(ln, colSubtext).render())
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderDurability — the regulator: λ̄ = μ/(1−n), the six metric rows (label padded to 14), the θ + n
// sparklines over the last 32 history points, and the stability checklist (✓ held / · not-held / ~
// N-A). (panels.py render_durability.)
func renderDurability(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	var lines []string
	rows := [][2]string{
		{"θ admission", fmt.Sprintf("%.2f", s.Theta)},
		{"λ̂ intensity", fmt.Sprintf("%.2f", s.LamHat)},
		{"λ̄ stationary", fmtLamBar(s.LamBar)},
		{"n branching", fmt.Sprintf("%.2f", s.N)},
		{"μ baseline", fmt.Sprintf("%.2f", s.Mu)},
		{"U utilisation", fmt.Sprintf("%.2f", s.U)},
	}
	for _, kv := range rows {
		lines = append(lines, leaderRow(kv[0], colMuted, kv[1], colText, w))
	}
	// θ + n sparklines on ONE line (the regulator history tail).
	hist := s.RegHistory
	if len(hist) > 32 {
		hist = hist[len(hist)-32:]
	}
	if len(hist) > 0 {
		thetas := make([]float64, len(hist))
		ns := make([]float64, len(hist))
		for i, h := range hist {
			thetas[i], ns[i] = h.Theta, h.N
		}
		spk := join(
			txt("θ ", colMuted), txt(sparkline(thetas), colAccent),
			txt("  n ", colMuted), txt(sparkline(ns), colAccent),
		)
		lines = append(lines, ansi.Truncate(spk, w, "…"))
	}
	// stability checklist: ONE compact glyph row when it fits the column, else one readable line each.
	if len(s.Stability) > 0 {
		plain := ""
		for i, c := range s.Stability {
			if i > 0 {
				plain += " "
			}
			plain += stabilityMark(c) + firstToken(c.Name)
		}
		if lipgloss.Width(plain) <= w {
			var b strings.Builder
			for i, c := range s.Stability {
				if i > 0 {
					b.WriteString(" ")
				}
				b.WriteString(txt(stabilityMark(c)+firstToken(c.Name), stabilityColor(c)).render())
			}
			lines = append(lines, b.String())
		} else {
			for _, c := range s.Stability {
				lines = append(lines, wrapEntry(txt(stabilityMark(c)+" ", stabilityColor(c)).render(), 2, c.Name, stabilityColor(c), w)...)
			}
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// stabilityMark / stabilityColor map a durability check to its glyph + tone (✓ held / ~ N-A / · not-held).
func stabilityMark(c StabilityCheckVM) string {
	switch {
	case c.NA:
		return "~"
	case c.Pass:
		return "✓"
	}
	return "·"
}
func stabilityColor(c StabilityCheckVM) lipgloss.Color {
	switch {
	case c.NA:
		return colWarn
	case c.Pass:
		return colOk
	}
	return colFaint
}

// firstToken returns the text up to the first space (the short label of a stability check, e.g.
// "n<1 (subcritical)" -> "n<1"), so the compact checklist row stays tight.
func firstToken(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

// renderValue — V(branch): one bar per branch (active in accent), then the last grounded reward
// (green >=0 / red <0). (panels.py render_value.)
// maxBranchRows caps the per-row branch lists (the VALUE bars, the frontier open set) so a graph with
// hundreds of branches renders a bounded, best-first panel instead of one row per branch that inflates
// the panel to arbitrary height and shoves its siblings off the scroll (the L2/L3 overflow).
const maxBranchRows = 12

// maxConvRows caps the convertibility lists (candidates / programs / gate-priors / demoted) and the
// specialist cards — these grow with learning over a long session / the registry-scaling work (L5).
const maxConvRows = 8

// topBranchesByValue returns up to n branches with the highest Value (the active branch ALWAYS
// included), in the original branch order for stable display, plus the count omitted.
func topBranchesByValue(branches []BranchVM, n int, active *int) (shown []BranchVM, omitted int) {
	if len(branches) <= n {
		return branches, 0
	}
	idx := make([]int, len(branches))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ba, bb := branches[idx[a]], branches[idx[b]]
		aa := active != nil && ba.ID == *active
		ab := active != nil && bb.ID == *active
		if aa != ab {
			return aa // the active branch sorts first so it is never dropped
		}
		return ba.Value > bb.Value
	})
	keep := append([]int(nil), idx[:n]...)
	sort.Ints(keep) // restore original branch order for display
	shown = make([]BranchVM, 0, n)
	for _, i := range keep {
		shown = append(shown, branches[i])
	}
	return shown, len(branches) - n
}

func renderValue(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	var lines []string
	for _, ln := range wrapPlain("V(branch) — rerank heuristic", w, w) {
		lines = append(lines, txt(ln, colSubtext).render())
	}
	lines = append(lines, "")
	if s.ActiveBranch == nil && len(s.Branches) == 0 {
		lines = append(lines, faintStr("  (no graph)"))
		return Panel{Body: strings.Join(lines, "\n")}
	}
	shownBranches, omitted := topBranchesByValue(s.Branches, maxBranchRows, s.ActiveBranch)
	for _, br := range shownBranches {
		lab, barC := colMuted, colFaint
		if s.ActiveBranch != nil && br.ID == *s.ActiveBranch {
			lab, barC = colAccent, colAccent
		}
		left := join(
			txt(fmt.Sprintf("  b%d ", br.ID), lab),
			txt(bar(br.Value, 10), barC),
		)
		lines = append(lines, lineLR(left, txt(fmt.Sprintf("%.2f", br.Value), colMuted).render(), w))
	}
	if omitted > 0 {
		lines = append(lines, faintStr(fmt.Sprintf("  …+%d more (by V; active always shown)", omitted)))
	}
	// the active branch's V(s) WITH its why (P6): the value.update event carries the signal breakdown
	// (recent_conf / goal_sim / grounded_reality), the one-line reason, the grounded reward, and the
	// appraiser. V(s) is THE signal (rerank / filter-trust / act-threshold / convertibility) — showing
	// only the scalar hid why it moved; the breakdown makes it explainable.
	if ev := lastEvent(vm, events.Value); ev != nil {
		lines = append(lines, "")
		if reason := dataStr(ev, "reason"); reason != "" {
			lines = append(lines, wrapEntry(txt("why ", colMuted).render(), 4, reason, colSubtext, w)...)
		}
		if sig, ok := ev.Data["signals"].(map[string]any); ok {
			for _, kv := range []struct{ key, label string }{
				{"recent_conf", "  recent conf"},
				{"goal_sim", "  goal sim"},
				{"grounded_reality", "  grounded"},
			} {
				if v, ok := asNum(sig[kv.key]); ok {
					lines = append(lines, leaderRow(kv.label, colMuted, fmt.Sprintf("%+.2f", v), colSubtext, w))
				}
			}
		}
		reward := dataFloat(ev, "reward")
		rc := colErr
		if reward >= 0 {
			rc = colOk
		}
		lines = append(lines, lineLR(txt("  grounded reward", colMuted).render(), txt(fmt.Sprintf("%+.1f", reward), rc).render(), w))
		if ap := dataStr(ev, "appraiser"); ap != "" {
			lines = append(lines, faintStr(clip("  appraiser: "+ap, w)))
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderLifecycleMetrics — the lifecycle state machine's current vitals (deck): the state, the arousal
// (tone by level), and the loop mode. The transition history is the full-width renderLifecycleText.
func renderLifecycleMetrics(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	lines := []string{
		txt("state    ", colMuted).render() + txt(s.LifecycleState, colText).render(),
		txt("arousal  ", colMuted).render() + txt(s.Arousal, ArousalColorFor(s.Arousal)).render(),
	}
	for _, ln := range wrapPlain("mode     "+s.Mode, w, w) {
		lines = append(lines, txt(ln, colSubtext).render())
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderLifecycleText — the lifecycle transition history (full width): the last-6 "<STATE>  <reason>"
// rows, the reason wrapped with a hanging indent under a fixed state column (full reason, never clipped).
func renderLifecycleText(vm ViewModel) Panel {
	w := contentW(vm)
	hist := vm.Snap.LifecycleHistory
	if len(hist) == 0 {
		return Panel{Body: faintStr(clip("(no transitions yet)", w))}
	}
	if len(hist) > 6 {
		hist = hist[len(hist)-6:]
	}
	var lines []string
	for _, h := range hist {
		state, reason := splitHist(h)
		prefix := txt(padRight(state, 16)+" ", colSubtext).render()
		lines = append(lines, wrapEntry(prefix, 17, reason, colFaint, w)...)
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderTrace — the raw event tape: the last-16 events ("[tick] kind(pad-18) summary[:70]").
// (panels.py render_trace.)
func renderTrace(vm ViewModel) Panel {
	w := contentW(vm)
	evs := vm.Events
	if len(evs) > 16 {
		evs = evs[len(evs)-16:]
	}
	if len(evs) == 0 {
		return Panel{Body: faintStr("(no events yet)")}
	}
	var lines []string
	for _, e := range evs {
		// "[tick] kind <summary wrapped/hanging>" — the [tick] (7) + kind (18) are the fixed prefix.
		prefix := join(
			txt(fmt.Sprintf("[%4d] ", e.Tick), colFaint),
			txt(padRight(e.Kind, 18)+" ", colSubtext),
		)
		lines = append(lines, wrapEntry(prefix, lipgloss.Width(ansi.Strip(prefix)), e.Summary, colMuted, w)...)
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderGrounding — the reality-grounding ledger (SR-4 anti-hallucination spine): every claim the
// harness has grounded or refuted against reality, with its trust tier + epistemic status
// (KNOW/BELIEVE/HEARD), plus the continuous sensor percepts that re-ground standing claims with no ACT.
// A pure view over the grounding.* stream. On the test backend every act is fabricated (tier-0),
// so nothing grounds and the panel says so honestly — live grounding needs a real backend (real tools /
// a model that forms claimed intentions) or a registered sensor. (Two-face: the compact tally + the
// full ledger; the Overview shows just the tally row, this tab the whole ledger.)
func renderGrounding(vm ViewModel) Panel {
	w := contentW(vm)
	grounds := eventsOfKind(vm, events.Ground, 8)
	percepts := eventsOfKind(vm, events.Percept, 3)

	if len(grounds) == 0 && len(percepts) == 0 {
		var empty []string
		for _, ln := range wrapPlain("(nothing grounded yet — test-backend acts are fabricated, so reality "+
			"never grounds; load a model or register a sensor for live grounding)", w, w) {
			empty = append(empty, faintStr(ln))
		}
		return Panel{Body: strings.Join(empty, "\n")}
	}

	var lines []string
	// COMPACT tally: grounded/refuted counts + the epistemic mix over what's on the tape.
	var nGround, nRefute, know, believe, heard int
	for i := range grounds {
		e := &grounds[i]
		if dataStr(e, "verdict") == "refuted" {
			nRefute++
		} else {
			nGround++
		}
		switch dataStr(e, "status") {
		case "KNOW":
			know++
		case "BELIEVE":
			believe++
		case "HEARD":
			heard++
		}
	}
	lines = append(lines, leaderRow("grounded / refuted", colMuted,
		fmt.Sprintf("%d / %d", nGround, nRefute), colText, w))
	lines = append(lines, leaderRow("KNOW · BELIEVE · HEARD", colMuted,
		fmt.Sprintf("%d · %d · %d", know, believe, heard), colSubtext, w))

	// DETAIL: the experiment ledger — each grounded/refuted claim (newest last), its verdict glyph, and
	// a faint trailing line with verdict · tier · status.
	lines = append(lines, txt(clip("EXPERIMENT LEDGER  (claim → verdict · tier · status)", w), colSubtext).render())
	for i := range grounds {
		e := &grounds[i]
		verdict := dataStr(e, "verdict")
		vc, glyph := colOk, "✓"
		if verdict == "refuted" {
			vc, glyph = colErr, "✗"
		}
		prefix := txt("  "+glyph+" ", vc).render()
		lines = append(lines, wrapEntry(prefix, 4, dataStr(e, "claim"), vc, w)...)
		meta := verdict + " · " + dataStr(e, "tier") + " · " + dataStr(e, "status")
		if b := dataStr(e, "bridge"); b != "" && b != "none" {
			meta += " · " + b
		}
		lines = append(lines, txt("    ↳ "+clip(meta, w-6), colFaint).render())
	}

	if len(percepts) > 0 {
		lines = append(lines, txt(clip("SENSORS  (continuous re-grounding, no ACT)", w), colSubtext).render())
		for i := range percepts {
			e := &percepts[i]
			lines = append(lines, wrapEntry(txt("  ~ ", colAccent).render(), 4, e.Summary, colSubtext, w)...)
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderSession — the RUNTIME spawn tree (P3.3): the bounded Session tree a synthesised workflow opens.
// A pure view over the session.* stream: the spawned workflow (goal · shape · phases), each dispatched
// phase as a row with a token-budget bar (a parallel phase marked ⇉ + its merge), and the terminate
// summary (reason · nodes · whole-tree spend). Empty when no multi-phase program is running (simple Q&A
// opens no tree — the honest empty-state).
func renderSession(vm ViewModel) Panel {
	w := contentW(vm)
	// find the most recent spawn; show the dispatch/merge/terminate that belong to it (after its index).
	spawnIdx := -1
	for i := len(vm.Events) - 1; i >= 0; i-- {
		if vm.Events[i].Kind == events.SessionSpawn {
			spawnIdx = i
			break
		}
	}
	if spawnIdx < 0 {
		var empty []string
		for _, ln := range wrapPlain("(no workflow running — a simple Q&A opens no session tree; a "+
			"multi-phase synthesised program spawns one)", w, w) {
			empty = append(empty, faintStr(ln))
		}
		return Panel{Body: strings.Join(empty, "\n")}
	}
	spawn := &vm.Events[spawnIdx]
	var lines []string
	// header: the workflow goal + its program shape + phase count.
	hdr := "session: " + dataStr(spawn, "goal")
	lines = append(lines, wrapEntry(txt("", colText).render(), 0, hdr, colText, w)...)
	lines = append(lines, txt(clip(fmt.Sprintf("  shape %s · %d phases", dataStr(spawn, "shape"),
		int(dataFloat(spawn, "phases"))), w), colMuted).render())

	// each dispatched phase: "  N op-group  ⇉  ███···  spent/cap tok  [merge k]"
	var term *events.Event
	for i := spawnIdx + 1; i < len(vm.Events); i++ {
		e := &vm.Events[i]
		switch e.Kind {
		case events.SessionSpawn:
			i = len(vm.Events) // a newer spawn — stop (shouldn't happen, spawnIdx is the last)
		case events.SessionDispatch:
			spent, cap := dataFloat(e, "tokens"), dataFloat(e, "cap")
			frac := 0.0
			if cap > 0 {
				frac = spent / cap
			}
			marker := "→"
			mc := colMuted
			if dataBool(e, "parallel") {
				marker, mc = "⇉", colAccent
			}
			label := fmt.Sprintf("%2d %s %s", int(dataFloat(e, "phase")), marker, dataStr(e, "goal"))
			budget := fmt.Sprintf("%s %d/%d", bar(frac, 8), int(spent), int(cap))
			lines = append(lines, lineLR("  "+txt(clip(label, w-22), mc).render(),
				txt(budget, colSubtext).render(), w))
		case events.SessionMerge:
			lines = append(lines, txt(clip(fmt.Sprintf("     ↳ merge %d parallel (%s)",
				int(dataFloat(e, "n")), dataStr(e, "strategy")), w), colFaint).render())
		case events.SessionTerminate:
			term = e
		}
	}
	if term != nil {
		lines = append(lines, lineLR(
			txt("terminated · "+dataStr(term, "reason"), colMuted).render(),
			txt(fmt.Sprintf("%d nodes · %d tok", int(dataFloat(term, "nodes")), int(dataFloat(term, "tokens"))),
				colSubtext).render(), w))
	} else {
		lines = append(lines, txt("  (running)", colOk).render())
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderDashboard — the all-subsystem COMPACT status (the Overview's at-a-glance header): one dense row
// per subsystem, live off the snapshot + event stream. This is the "compact information" face the re-ramp
// is built around — every subsystem's headline reads in a single glance, and each subsystem's own tab is
// the detail face. (Conscious state · subconscious fire · grounding K/B/H · runtime nodes · memory sizes ·
// retriever · value V · regulator n/U/θ.)
func renderDashboard(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	var lines []string
	// each row is "<label-12> <value>" left-aligned; the value is ANSI-clipped to the residual width so a
	// dense status line degrades cleanly on a narrow terminal (never overflows the compact header).
	const lblW = 12
	row := func(label, val string, vc lipgloss.Color) {
		prefix := txt(padRight(clip(label, lblW), lblW)+" ", colMuted).render()
		lines = append(lines, prefix+ansi.Truncate(txt(val, vc).render(), max(0, w-lblW-1), ""))
	}

	state := s.LifecycleState
	if state == "" {
		state = "—"
	}
	row("conscious", fmt.Sprintf("%s · %d branch", state, len(s.Branches)), colSubtext)

	subN := 0
	if d := lastEvent(vm, events.SubDispatch); d != nil {
		subN = int(dataFloat(d, "count"))
	}
	row("subconscious", fmt.Sprintf("%d fired · θ=%.2f", subN, s.Theta), colSubtext)

	// grounding: tally the ledger off the event tape.
	var g, r, know, bel, heard int
	for i := range vm.Events {
		e := &vm.Events[i]
		if e.Kind != events.Ground {
			continue
		}
		if dataStr(e, "verdict") == "refuted" {
			r++
		} else {
			g++
		}
		switch dataStr(e, "status") {
		case "KNOW":
			know++
		case "BELIEVE":
			bel++
		case "HEARD":
			heard++
		}
	}
	gc := colMuted
	if g+r > 0 {
		gc = colOk
	}
	row("grounding", fmt.Sprintf("%d✓/%d✗ · K%d B%d H%d", g, r, know, bel, heard), gc)

	// runtime: the last session tree's node count (spawn phases+root, or the terminate tally).
	nodes := 0
	if t := lastEvent(vm, events.SessionTerminate, events.SessionSpawn); t != nil {
		if t.Kind == events.SessionTerminate {
			nodes = int(dataFloat(t, "nodes"))
		} else {
			nodes = int(dataFloat(t, "phases")) + 1
		}
	}
	rtc := colMuted
	if nodes > 0 {
		rtc = colSubtext
	}
	row("runtime", fmt.Sprintf("%d session node(s)", nodes), rtc)

	mc := colMuted
	if s.EpisodicCount+s.SemanticCount+s.PersonCount > 0 {
		mc = colText
	}
	mode := s.RetrieverMode
	if mode == "" {
		mode = "lexical"
	}
	row("memory", fmt.Sprintf("%dep·%dbl·%dpr · %s", s.EpisodicCount, s.SemanticCount, s.PersonCount, mode), mc)

	v := 0.0
	if s.ActiveBranch != nil {
		v = s.Values[*s.ActiveBranch]
	}
	row("value", fmt.Sprintf("V(active)=%.2f", v), colSubtext)

	rc := colOk
	if s.N >= 1 {
		rc = colErr
	}
	row("regulator", fmt.Sprintf("n=%.2f U=%.2f θ=%.2f λ̄=%s", s.N, s.U, s.Theta, fmtLamBar(s.LamBar)), rc)
	return Panel{Body: strings.Join(lines, "\n")}
}

// -- small view-layer helpers ----------------------------------------------------------------------

// padLeft right-justifies s to a visible width of w (the Python `f"{s:>w}"`).
func padLeft(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

// asNum coerces an event-data number (int or float64 off the wire) to float64.
func asNum(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// fmtLamBar formats the stationary rate λ̄, showing "∞" at the n>=1 cliff (Python regulator._fmt:
// +Inf -> "inf"); we render the math glyph for the durable-regime read.
func fmtLamBar(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return "∞"
	}
	return fmt.Sprintf("%.2f", v)
}

// intSlice renders an int slice as Python's `sorted(...)` list literal "[a, b, c]" (the acted-branches
// line). The bridge already sorts; sort defensively so the panel is order-stable.
func intSlice(xs []int) string {
	cp := append([]int(nil), xs...)
	sort.Ints(cp)
	parts := make([]string, len(cp))
	for i, x := range cp {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// splitHist splits a bridge-formatted "<STATE>  <reason>" history line back into (state, reason). The
// bridge joins with a double space; split on the first run of 2+ spaces.
func splitHist(h string) (string, string) {
	if i := strings.Index(h, "  "); i >= 0 {
		return h[:i], strings.TrimLeft(h[i:], " ")
	}
	return h, ""
}

// clipANSI is the ANSI-aware clip for a styled string (unused by the panels, which clip plain text
// before styling, but kept for the chrome/footer where a styled summary must fit a width). Mirrors
// renderFit's x/ansi.Truncate usage.
func clipANSI(s string, n int) string { return ansi.Truncate(s, n, "…") }
