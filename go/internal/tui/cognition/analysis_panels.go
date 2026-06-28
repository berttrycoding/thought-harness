package cognition

// analysis_panels.go — the ANALYSIS surface renderers (pure, over an AnalysisRecord at a scrub
// cursor). The post-session twin of the runtime monitors: full-session time-series + ledgers + the
// power-ON/OFF COMPARE. Mock: 2026-06-20-tui-mockups-analysis.md. Driven by SampleAnalysisRecord
// until the G1 loader lands; the renderers don't care where the record came from.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// analysisTabs is the ordered analysis tab strip (mirrors the ^O monitor families). Index 0 becomes
// "DIFF" in COMPARE mode.
var analysisTabs = []string{"SESSION", "LOOP·CTRL", "VALUE·REG", "TRACE", "CONSCIOUS", "REGISTRIES", "ACTION·SESS", "THROUGHPUT", "SELF"}

// AnalysisTabCount is the number of tabs (for the Update-side cursor clamp).
func AnalysisTabCount() int { return len(analysisTabs) }

// AnalysisTabName returns the tab name at index i (clamped), so a caller can resolve a panel index by
// name without reaching into the unexported strip. Used by the wiring-gate test to target REGISTRIES.
func AnalysisTabName(i int) string {
	if i < 0 || i >= len(analysisTabs) {
		return ""
	}
	return analysisTabs[i]
}

func peakOf(vals []float64) (float64, int) {
	best, bi := 0.0, 0
	for i, v := range vals {
		if v > best {
			best, bi = v, i
		}
	}
	return best, bi
}

func fmtWall(s int) string { return fmt.Sprintf("%dm %02ds", s/60, s%60) }

func secsForTicks(rec AnalysisRecord, n int) int {
	if rec.Ticks == 0 {
		return 0
	}
	return n * rec.WallSecs / rec.Ticks
}

// RenderAnalysisShell renders the top chrome: title · scrub bar · stimulus axis · cursor line · tab
// strip. width is the inner box width. Pure.
func RenderAnalysisShell(rec AnalysisRecord, cursor int, compare bool, activeTab, width int) string {
	const pad = "       " // aligns the stimulus row under the "tick 0 " prefix on the scrub row
	barW := width - 18
	if barW < 12 {
		barW = 12
	}
	mode := "SINGLE"
	if compare {
		mode = "COMPARE"
	}
	title := txt("ANALYSIS · "+rec.Name, colAccent).render() + txt("    "+mode, colSubtext).render()
	scrub := label("scrub") + txt("tick 0 ", colFaint).render() + txt(scrubBar(cursor, rec.Ticks, barW), colSubtext).render() +
		txt(fmt.Sprintf(" %d", rec.Ticks), colFaint).render()
	stim := label("stimuli") + pad + txt(stimuliRow(rec.Stimuli, rec.Ticks, barW), colWarn).render()
	cur := label("cursor") + txt(fmt.Sprintf("tick %d", cursor), colAccent).render() +
		txt(fmt.Sprintf(" · %s · %s", rec.Substrate, strings.ToUpper(rec.Mode)), colFaint).render() +
		txt("   ([ ] scrub · { } stimulus · Tab panel · c compare · ^Y close)", colFaint).render()

	parts := make([]string, len(analysisTabs))
	for i, t := range analysisTabs {
		if compare && i == 0 {
			t = "DIFF"
		}
		if i == activeTab {
			parts[i] = txt("["+t+"]", colAccent).render()
		} else {
			parts[i] = txt(t, colFaint).render()
		}
	}
	strip := "  " + strings.Join(parts, "  ")
	return strings.Join([]string{title, scrub, stim, cur, strip}, "\n")
}

// RenderAnalysisTab dispatches to the active panel body (or the COMPARE diff) and returns a boxed
// panel at the given width. Pure. Three flags gate the deeper tabs (all default OFF, so a default-OFF
// surface is byte-identical to the G2 benchmarking-core state): registryHeat gates the G3 REGISTRIES
// family heat map + ledger (tui.registry_heatmap); deepLedgers gates the G4 DEEP tabs — CONSCIOUS
// thought tree + compression (§5), ACTION·SESS ledger + spawn tree (§7), THROUGHPUT (§8), SELF (§9);
// traceFlow gates the G6 TRACE swimlane + phase/freq readout (tui.trace_flow). When a flag is OFF its
// tab keeps the "panel pending" placeholder; when ON it renders the real panel.
func RenderAnalysisTab(rec, recB AnalysisRecord, cursor int, compare bool, activeTab, width int, registryHeat, deepLedgers, traceFlow bool) string {
	if compare {
		return BoxedMonitor("COMPARE · A="+rec.Name+"  vs  B="+recB.Name, renderCompareBody(rec, recB, width), width)
	}
	name := analysisTabs[activeTab]
	var body string
	switch {
	case name == "SESSION":
		body = renderSessionBody(rec, cursor, width)
	case name == "LOOP·CTRL":
		body = renderLoopCtrlBody(rec, cursor, width)
	case name == "VALUE·REG":
		body = renderValueRegBody(rec, cursor, width)
	case name == "TRACE" && traceFlow:
		body = renderTraceFlowBody(rec, cursor, width)
	case name == "REGISTRIES" && registryHeat:
		body = renderRegistriesBody(rec, cursor, width)
	case name == "CONSCIOUS" && deepLedgers:
		body = renderConsciousBody(rec, cursor, width)
	case name == "ACTION·SESS" && deepLedgers:
		body = renderActionSessBody(rec, cursor, width)
	case name == "THROUGHPUT" && deepLedgers:
		body = renderThroughputBody(rec, cursor, width)
	case name == "SELF" && deepLedgers:
		body = renderSelfBody(rec, cursor, width)
	default:
		// the tab is gated OFF — name the flag that turns THIS tab on (the REGISTRIES heat map + the
		// TRACE swimlane each have their own flag; the four DEEP tabs share tui.deep_ledgers) rather than
		// claim a gated tab is live.
		flag := "tui.deep_ledgers"
		switch name {
		case "REGISTRIES":
			flag = "tui.registry_heatmap"
		case "TRACE":
			flag = "tui.trace_flow"
		}
		body = txt("— "+name+" panel pending — enable "+flag+" to render it (SESSION · LOOP·CTRL · VALUE·REG + COMPARE are always live) —", colFaint).render()
	}
	return BoxedMonitor(name, body, width)
}

func chartW(width int) int {
	w := width - 16
	if w < 16 {
		w = 16
	}
	if w > 56 {
		w = 56
	}
	return w
}

// renderSessionBody — §1: whole-session vitals + the impulse-response capture. Pure.
func renderSessionBody(rec AnalysisRecord, cursor, width int) string {
	cw := chartW(width)
	var l []string
	l = append(l, label("run")+txt(rec.Name, colSubtext).render()+
		txt(fmt.Sprintf(" · %s · %s · %dt · %s", rec.Substrate, rec.Mode, rec.Ticks, fmtWall(rec.WallSecs)), colFaint).render())

	verdTone := colOk
	if rec.SolveVerdict != "SOLVED" {
		verdTone = colWarn
	}
	l = append(l, label("outcome")+txt(rec.SolveVerdict, verdTone).render()+
		txt(fmt.Sprintf(" · delivered %d · grounded %d · refuted %d · fabricated-blocked %d · reverts %d",
			rec.Delivered, rec.Grounded, rec.Refuted, rec.Fabricated, rec.Reverts), colFaint).render())

	l = append(l, label("condition")+txt(sparkW(rec.Condition, cw), colAccent).render()+
		txt("   NOMINAL→ENGAGED→LOADED→NOMINAL", colFaint).render())

	np, nt := peakOf(rec.N)
	l = append(l, label("excitation")+txt("n ", colFaint).render()+txt(sparkW(rec.N, cw), colAccent).render()+
		txt(fmt.Sprintf("  peak %.2f @t%d  (runaway 1.0)", np, nt), colFaint).render())
	up, ut := peakOf(rec.U)
	l = append(l, label("load")+txt("U ", colFaint).render()+txt(sparkW(rec.U, cw), colAccent).render()+
		txt(fmt.Sprintf("  peak %.2f @t%d", up, ut), colFaint).render())

	gh := 0
	for _, b := range rec.GroundHits {
		if b {
			gh++
		}
	}
	l = append(l, label("grounding")+Strip(rec.GroundHits, cw, colOk)+
		txt(fmt.Sprintf("  %d reality checks", gh), colFaint).render())

	l = append(l, txt(fmt.Sprintf("── impulse · stimulus @t%d %q (user) ───────────", rec.ImpStimulusTick, rec.ImpStimulusText), colMuted).render())
	l = append(l, label("response")+txt(fmt.Sprintf("interrupt→fire %dt · →inject %dt · →ACT %dt · →DELIVER %dt",
		rec.ImpToFire, rec.ImpToInject, rec.ImpToAct, rec.ImpToDeliver), colSubtext).render())
	l = append(l, label("latency")+txt(fmt.Sprintf("%d ticks (%ds) to DELIVER", rec.ImpToDeliver, secsForTicks(rec, rec.ImpToDeliver)), colAccent).render()+
		txt("  ◀ the responsiveness number", colFaint).render())
	l = append(l, label("reserve")+txt(sparkW(rec.Reserve, cw), colAccent).render()+txt("  drains on the spike, refills after DELIVER", colFaint).render())
	l = append(l, label("pressure")+txt(sparkW(rec.Pressure, cw), colAccent).render()+txt("  user-waiting spikes then releases at DELIVER", colFaint).render())
	return strings.Join(l, "\n")
}

// renderLoopCtrlBody — §3: the decision history + the cursor decision + the log. Pure.
func renderLoopCtrlBody(rec AnalysisRecord, cursor, width int) string {
	var l []string
	moves := make([]string, len(rec.Decisions))
	ticks := make([]string, len(rec.Decisions))
	for i, d := range rec.Decisions {
		moves[i] = d.Move
		ticks[i] = fmt.Sprintf("·%d", d.Tick)
	}
	l = append(l, label("decisions")+txt(strings.Join(moves, "  "), colSubtext).render())
	l = append(l, label("at ticks")+txt(strings.Join(ticks, "  "), colFaint).render())

	// the decision at/before the cursor
	var cd *DecisionEvent
	for i := range rec.Decisions {
		if rec.Decisions[i].Tick <= cursor {
			cd = &rec.Decisions[i]
		}
	}
	if cd != nil {
		head := cd.Move
		if cd.StopKind != "" {
			head += " (" + cd.StopKind + ")"
		}
		l = append(l, label("cursor →")+txt(fmt.Sprintf("tick %d · %s", cd.Tick, head), colAccent).render())
		l = append(l, label("reason")+txt(quote(cd.Reason), colMuted).render())
	}
	l = append(l, label("fingerprint")+txt(moveCounts(rec.Decisions), colSubtext).render())

	l = append(l, txt("── decision log ────────────────────────────────", colMuted).render())
	for _, d := range rec.Decisions {
		head := d.Move
		if d.StopKind != "" {
			head += "/" + d.StopKind
		}
		l = append(l, txt(fmt.Sprintf("t%-4d ", d.Tick), colFaint).render()+
			txt(fmt.Sprintf("%-10s ", head), colSubtext).render()+txt(quote(d.Reason), colMuted).render())
	}
	return strings.Join(l, "\n")
}

// renderValueRegBody — §4: the V / n / U / θ time-series + the grounded reward ledger. Pure.
func renderValueRegBody(rec AnalysisRecord, cursor, width int) string {
	cw := chartW(width)
	var l []string
	l = append(l, label("V(active)")+txt(sparkW(rec.VActive, cw), colAccent).render()+
		txt(fmt.Sprintf("  now %.2f (priority) at the cursor", valAt(rec.VActive, cursor)), colFaint).render())
	np, nt := peakOf(rec.N)
	l = append(l, label("n")+txt(sparkW(rec.N, cw), colAccent).render()+
		txt(fmt.Sprintf("  peak %.2f @t%d · %s (runaway 1.0)", np, nt, stabilityWord(np)), colFaint).render())
	up, ut := peakOf(rec.U)
	l = append(l, label("U")+txt(sparkW(rec.U, cw), colAccent).render()+
		txt(fmt.Sprintf("  peak %.2f @t%d · %s (schedulable 1.0)", up, ut, loadWord(up)), colFaint).render())
	l = append(l, label("theta")+txt(sparkW(rec.Theta, cw), colAccent).render()+
		txt("  θ rises under load — fewer specialists admitted", colFaint).render())

	l = append(l, txt("── reward ledger (grounded only — never self-graded) ───", colMuted).render())
	for _, rw := range rec.Rewards {
		tone := colOk
		sign := "+"
		if rw.Value < 0 {
			tone, sign = colWarn, ""
		}
		l = append(l, txt(fmt.Sprintf("t%-4d ", rw.Tick), colFaint).render()+
			txt(fmt.Sprintf("%s%.1f ", sign, rw.Value), tone).render()+txt(quote(rw.Reason), colMuted).render())
	}
	return strings.Join(l, "\n")
}

// renderCompareBody — §2: the power-ON/OFF benchmark diff. Pure. The benchmark math is computed ONCE
// into a CompareReport (compare.go — the single source of truth the cognition-property test asserts);
// this function is the VIEW over that report, leading with the one-line verdict headline.
func renderCompareBody(a, b AnalysisRecord, width int) string {
	cw := chartW(width)
	rep := BuildCompareReport(a, b)
	var l []string

	// the headline verdict — the one thing the user reads first (the §2 benchmark answer).
	l = append(l, CompareHeadlineLine(rep))

	aTone, bTone := colOk, colOk
	if a.SolveVerdict != "SOLVED" {
		aTone = colWarn
	}
	if b.SolveVerdict != "SOLVED" {
		bTone = colWarn
	}
	l = append(l, label("outcome")+txt("A "+rep.AVerdict, aTone).render()+txt(" · B "+rep.BVerdict, bTone).render()+
		txt(fmt.Sprintf("    A grounded %d / B %d · A reverts %d / B %d", rep.AGrounded, rep.BGrounded, a.Reverts, b.Reverts), colFaint).render())

	faster := ""
	if rep.FasterArm != "" {
		faster = fmt.Sprintf("  (%s is +%d%% faster to DELIVER)", rep.FasterArm, rep.FasterPct)
	}
	l = append(l, label("latency")+txt(fmt.Sprintf("A %dt (%ds)   B %dt (%ds)", rep.ALatency, secsForTicks(a, rep.ALatency), rep.BLatency, secsForTicks(b, rep.BLatency)), colAccent).render()+
		txt(fmt.Sprintf("   Δ %+dt%s", rep.LatencyDeltaTicks, faster), colFaint).render())

	ap, _ := peakOf(a.N)
	bp, _ := peakOf(b.N)
	l = append(l, label("excitation")+txt("A "+sparkW(a.N, cw), colOk).render()+txt(fmt.Sprintf("  peak %.2f", ap), colFaint).render())
	bnote := ""
	if bp > ap+0.2 {
		bnote = "  ◀ B drifted toward the cliff"
	}
	l = append(l, strings.Repeat(" ", 13)+txt("B "+sparkW(b.N, cw), colWarn).render()+txt(fmt.Sprintf("  peak %.2f%s", bp, bnote), colFaint).render())

	tokNote := ""
	if rep.TokenDeltaPct != 0 {
		tokNote = fmt.Sprintf("   Δ B %+d%% out", rep.TokenDeltaPct)
	}
	l = append(l, label("tokens")+txt(fmt.Sprintf("A out %s", fmtTokK(rep.ATokOut)), colAccent).render()+
		txt(fmt.Sprintf("   B out %s%s", fmtTokK(rep.BTokOut), tokNote), colFaint).render())

	l = append(l, label("decisions")+fingerprintLine("A", a.Decisions))
	l = append(l, strings.Repeat(" ", 13)+fingerprintLine("B", b.Decisions))

	if rep.DivergenceTick >= 0 {
		l = append(l, label("diverge")+txt(fmt.Sprintf("tick %d — %s", rep.DivergenceTick, rep.DivergenceWhy), colAccent).render())
	}
	return strings.Join(l, "\n")
}

// divergenceTick finds the first decision index where A and B chose different moves, returning the
// tick + a one-line description of the fork. Pure.
func divergenceTick(a, b []DecisionEvent) (int, string) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i].Move != b[i].Move {
			return a[i].Tick, fmt.Sprintf("A %s (%s) vs B %s — the fork", a[i].Move, strings.SplitN(a[i].Reason, " ", 4)[0]+"…", b[i].Move)
		}
	}
	return -1, ""
}

// renderRegistriesBody — §6 (G3): the registry/memory FAMILY heat map + the mint/demote ledger. The
// "is the learned machinery real AND in use?" view: one heat-strip row per learned item (operator /
// specialist / skill / knowledge / source), coloured by how hot it ran over the session, plus the
// evidence ledger of every mint and demote. Pure over the reconstructed record (analysis.go heat
// primitives + loader.go fillFamily). Mock: 2026-06-20-tui-mockups-analysis.md §6.
func renderRegistriesBody(rec AnalysisRecord, cursor, width int) string {
	cw := chartW(width)
	var l []string

	// the colour/intensity legend — the same `· ▪ ▩ █` ramp every heat strip uses.
	l = append(l, label("colour")+
		txt("idle ", colFaint).render()+txt("·", colFaint).render()+
		txt("  fired ", colFaint).render()+txt("▪", colSubtext).render()+
		txt("  hot ", colFaint).render()+txt("▩", colWarn).render()+
		txt("  peak ", colFaint).render()+txt("█", colOk).render()+
		txt("    keyed to firing density, not glyph size", colFaint).render())

	if len(rec.FamilyEntries) == 0 {
		l = append(l, txt("── per-entry use · no learned machinery fired this session ──", colMuted).render())
		l = append(l, txt("an empty heat map is itself the read: nothing learned was exercised", colFaint).render())
	} else {
		l = append(l, txt("── operators / specialists / skills / knowledge / sources ──", colMuted).render())
		for _, e := range rec.FamilyEntries {
			l = append(l, familyHeatRow(e, rec.Ticks, cw))
		}
	}

	l = append(l, txt("── mint / demote ledger · with evidence ──────────────────", colMuted).render())
	if len(rec.Ledger) == 0 {
		l = append(l, txt("no mint or demote this session — the registry held steady", colFaint).render())
	} else {
		for _, le := range rec.Ledger {
			l = append(l, ledgerRow(le))
		}
	}
	return strings.Join(l, "\n")
}

// familyHeatRow renders one §6 heat-map row: the `fam:name` label, the coldness-vs-topics heat strip,
// and a status note (hot/warm/cold-since/new/fires). The label tone keys to the family; the status
// word keys to use (hot=ok, cold=warn-prune, new=accent). Pure.
func familyHeatRow(e FamilyEntry, ticks, width int) string {
	name := e.Family + ":" + e.Name
	if len(name) > 14 {
		name = name[:13] + "…"
	}
	strip := txt(heatStrip(e.Fires, width), heatTone(e)).render()
	// the cold-since horizon is the entry's own firing-strip span (the reconstructed session length),
	// which holds whether or not a SignalFrame sidecar set rec.Ticks (the events still size the strip).
	span := ticks
	if len(e.Fires) > span {
		span = len(e.Fires)
	}
	note, noteTone := familyNote(e, span)
	return label2(name) + strip + txt("  "+note, noteTone).render()
}

// heatTone colours the heat strip itself by the entry's overall warmth: a busy item runs green (ok), a
// lukewarm one amber-ish (subtext), an idle/cold one faint. (lipgloss colours the whole strip one tone;
// the GLYPH ramp carries the per-tick intensity within it, per the mock's "live = colour, not shade".)
func heatTone(e FamilyEntry) lipgloss.Color {
	switch {
	case e.Total >= 12:
		return colOk
	case e.Total >= 3:
		return colSubtext
	default:
		return colFaint
	}
}

// familyNote is the trailing status for a heat row: a fresh mint reads NEW, a cold-since-tNN item is
// the prune candidate (warn), a busy one is hot/warm (ok/subtext). Returns the text + its tone.
func familyNote(e FamilyEntry, ticks int) (string, lipgloss.Color) {
	if e.Demoted {
		return fmt.Sprintf("DEMOTED · %d fires", e.Total), colErr
	}
	if e.BornTick >= 0 {
		// a minted item: how many times has it fired since it was minted?
		return fmt.Sprintf("NEW · minted t%d · %d fires since", e.BornTick, e.Total), colAccent
	}
	last := lastFireTick(e.Fires)
	if e.Total == 0 {
		return "cold · never fired ◀ prune?", colWarn
	}
	// cold if it has not fired in the back third of the session (the mock's "cold since t52 ◀ prune?").
	if ticks > 0 && last >= 0 && last < ticks*2/3 {
		return fmt.Sprintf("cold since t%d · %d fires ◀ prune?", last, e.Total), colWarn
	}
	if e.Total >= 12 {
		return fmt.Sprintf("hot · %d fires", e.Total), colOk
	}
	return fmt.Sprintf("warm · %d fires", e.Total), colSubtext
}

// ledgerRow renders one mint/demote/prune/invalidate ledger line: tick · action · fam:name · evidence.
// The action tone reads green for a MINT (growth that earned its keep), warn/err for a removal. Pure.
func ledgerRow(le LedgerEvent) string {
	tone := colOk
	switch le.Action {
	case "DEMOTED", "INVALIDATED":
		tone = colErr
	case "PRUNED":
		tone = colWarn
	}
	name := le.Name
	if le.Family != "" {
		name = le.Family + ":" + le.Name
	}
	return txt(fmt.Sprintf("t%-4d ", le.Tick), colFaint).render() +
		txt(fmt.Sprintf("%-11s ", le.Action), tone).render() +
		txt(clipName(name, 22)+"  ", colSubtext).render() +
		txt(quote(le.Evidence), colMuted).render()
}

// label2 is the wider-label variant for the §6 entity rows (a `fam:name` entry name fills the label
// column instead of a fixed word). Padded to a 15-wide aligned column in muted, like label().
func label2(name string) string {
	return txt(fmt.Sprintf("%-15s", name), colMuted).render()
}

// clipName trims an entry/statement name to n runes with an ellipsis (keeps the ledger one-line). Pure.
func clipName(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// -- the DEEP ledgers + tree renderers (§5/§7/§8/§9, G4) --------------------

// renderConsciousBody — §5: the full thought tree + the compression history. The tree is the conscious
// stream's train of thought; each row is a branch (one line of reasoning), parent-indented, value-state
// tagged. A DEAD branch carries the REASON it died (the mock's "✗ b27 DEAD @t88 (refuted)") — the read
// "did dead branches die for a reason, or is that wasted effort?". Compression is the lossy-graph memory
// management (fold to a gist, reopen to push further). Pure over the reconstructed record.
func renderConsciousBody(rec AnalysisRecord, cursor, width int) string {
	var l []string
	live, dead, merged := 0, 0, 0
	for _, b := range rec.Branches {
		switch b.State {
		case "DEAD":
			dead++
		case "MERGED":
			merged++
		default:
			live++
		}
	}
	l = append(l, label("branches")+txt(fmt.Sprintf("%d live · %d dead · %d merged · %d total",
		live, dead, merged, len(rec.Branches)), colSubtext).render())

	if len(rec.Branches) == 0 {
		l = append(l, txt("── tree · no thought branches recorded this session ──", colMuted).render())
		l = append(l, txt("a single-line session never forked — nothing to draw", colFaint).render())
	} else {
		l = append(l, txt("── tree · line of thinking · state ──────────────────", colMuted).render())
		// depth from the parent chain (bounded — the regulator keeps the live set ≈8), one row per branch.
		depth := branchDepths(rec.Branches)
		for _, b := range rec.Branches {
			l = append(l, branchRow(b, depth[b.ID]))
		}
	}

	l = append(l, txt("── compression history · fold to gist · reopen ──────", colMuted).render())
	if len(rec.Compression) == 0 {
		l = append(l, txt("no compress/expand this session — the graph fit in focus", colFaint).render())
	} else {
		for _, c := range rec.Compression {
			tone := colSubtext
			if c.Op == "EXPAND" {
				tone = colAccent
			}
			arrow := "→ gist"
			if c.Op == "EXPAND" {
				arrow = "← gist reopened"
			}
			l = append(l, txt(fmt.Sprintf("t%-4d ", c.Tick), colFaint).render()+
				txt(fmt.Sprintf("%-9s ", c.Op), tone).render()+
				txt("b"+fmt.Sprintf("%-3d", c.Branch), colSubtext).render()+txt("  "+arrow, colFaint).render())
		}
	}
	return strings.Join(l, "\n")
}

// branchDepths computes each branch's depth from its parent chain (root = 0), bounded by the branch
// count so a malformed/cyclic parent link can never loop. Pure.
func branchDepths(bs []BranchNode) map[int]int {
	parent := map[int]int{}
	for _, b := range bs {
		parent[b.ID] = b.Parent
	}
	depth := map[int]int{}
	for _, b := range bs {
		d, cur, guard := 0, b.ID, 0
		for guard < len(bs)+1 {
			p, ok := parent[cur]
			if !ok || p < 0 {
				break
			}
			d++
			cur = p
			guard++
		}
		depth[b.ID] = d
	}
	return depth
}

// branchRow renders one §5 tree row: the parent-indented `bN` id, the line gist, and the value-state
// tag. A DEAD branch leads with `✗` and trails its death reason (the "died for a reason" read); a
// MERGED one reads its merge target; a live branch reads its EXPANDED/COMPRESSED state. Pure.
func branchRow(b BranchNode, depth int) string {
	indent := strings.Repeat("  ", depth)
	mark := "├ "
	if depth == 0 {
		mark = ""
	}
	idTone := colSubtext
	stateTone := colFaint
	state := b.State
	if b.State == "DEAD" {
		mark, idTone, stateTone = "✗ ", colErr, colErr
		state = "DEAD @t" + fmt.Sprintf("%d", b.DeadTick)
	} else if b.State == "MERGED" {
		idTone, stateTone = colMuted, colMuted
	} else if b.State == "EXPANDED" {
		stateTone = colOk
	}
	id := txt(indent+mark+"b"+fmt.Sprintf("%-3d", b.ID), idTone).render()
	gist := txt(quote(clipName(firstNonEmptyStr(b.Text, "(no gist)"), 34)), colSubtext).render()
	tag := txt("  "+state, stateTone).render()
	if b.DeadReason != "" {
		tag += txt(" ("+clipName(b.DeadReason, 26)+")", colFaint).render()
	}
	return id + " " + gist + tag
}

// renderActionSessBody — §7: the ACTION·GROUNDING reality ledger + the SESSIONS·SUB-AGENTS spawn tree.
// The ledger is the watched seam in audit form: an ACT (intention out), a GROUNDED (reality confirmed),
// a REFUTED (reality corrected a belief — a HEALTHY revert), a BLOCKED (a fabrication the safety check
// caught — the alarm), a DENIAL (a scope violation). The spawn tree is the bounded helper team, each
// limited to a tool scope. The two reads: "did reality push back, and was a fabrication blocked?" +
// "did the helper team stay bounded and scoped (no orphans, no out-of-scope tool)?". Pure.
func renderActionSessBody(rec AnalysisRecord, cursor, width int) string {
	var l []string
	acts, grounded, refuted, blocked := 0, 0, 0, 0
	for _, a := range rec.Actions {
		switch a.Kind {
		case "ACT":
			acts++
		case "GROUNDED":
			grounded++
		case "REFUTED":
			refuted++
		case "BLOCKED":
			blocked++
		}
	}
	l = append(l, label("tally")+txt(fmt.Sprintf("%d actions · grounded %d · refuted %d · fabrication blocked %d",
		acts, grounded, refuted, blocked), colSubtext).render())

	l = append(l, txt("── action ledger · reach out · reality back ──────────", colMuted).render())
	if len(rec.Actions) == 0 {
		l = append(l, txt("no actions this session — it never opened to reality", colFaint).render())
	} else {
		for _, a := range rec.Actions {
			l = append(l, actionRow(a))
		}
	}

	l = append(l, txt("── sub-agents · the helper team it spawned ───────────", colMuted).render())
	orphan := "no orphans"
	l = append(l, label("spawn tree")+txt(fmt.Sprintf("%d spawned · depth %d · %s",
		len(rec.Workers), rec.SpawnDepth, orphan), colSubtext).render())
	if rec.SpawnTokens > 0 {
		l = append(l, label("tree spend")+txt(fmtTokK(rec.SpawnTokens)+" tokens", colAccent).render())
	}
	if len(rec.Workers) == 0 {
		l = append(l, txt("no helpers spawned — answered on the main line", colFaint).render())
	} else {
		for _, w := range rec.Workers {
			l = append(l, workerRow(w))
		}
	}
	return strings.Join(l, "\n")
}

// actionRow renders one §7 ledger line: tick · kind · text (· tool). A REFUTED reads OK (reality
// correcting a belief is GOOD); a BLOCKED/DENIAL reads as the alarm (err); an ACT/GROUNDED is neutral.
// Pure.
func actionRow(a ActionEvent) string {
	tone := colSubtext
	switch a.Kind {
	case "GROUNDED":
		tone = colOk
	case "REFUTED":
		tone = colWarn // a healthy revert — amber, not red (reality pushed back, by design)
	case "BLOCKED", "DENIAL":
		tone = colErr
	}
	row := txt(fmt.Sprintf("t%-4d ", a.Tick), colFaint).render() +
		txt(fmt.Sprintf("%-9s ", a.Kind), tone).render() +
		txt(quote(clipName(a.Text, 40)), colMuted).render()
	if a.Tool != "" {
		row += txt(" ·"+a.Tool, colFaint).render()
	}
	return row
}

// workerRow renders one §7 spawn-tree row: the role · domain · the tool scope it was limited to. An
// empty scope reads "no tools" (a pure summarizer); a populated one lists the allowed tools — the
// watched-seam guarantee the panel exists to surface. Pure.
func workerRow(w Worker) string {
	scope := "no tools"
	if len(w.ToolScope) > 0 {
		scope = "tools " + strings.Join(w.ToolScope, ",")
	}
	head := firstNonEmptyStr(w.Role, "worker")
	if w.Domain != "" {
		head += " " + w.Domain
	}
	return txt("▸ ", colFaint).render() +
		txt(clipName(head, 22), colSubtext).render() +
		txt("  ·  "+scope, colFaint).render()
}

// renderThroughputBody — §8: the metabolism — where the token budget went, split by CONTENT role and by
// model tier (the big primary vs the cheap utility), plus the single most expensive tick (the slow
// point). The reads: "where is the spend?" + "is the cost-saving tier doing real work?" + "is there a
// recurring expensive step worth eliminating?". Pure over the reconstructed record.
func renderThroughputBody(rec AnalysisRecord, cursor, width int) string {
	cw := chartW(width)
	var l []string

	totalCompletion := 0
	for _, rs := range rec.Roles {
		totalCompletion += rs.Tokens
	}
	l = append(l, label("tokens out")+txt(sparkW(rec.TokOutRate, cw), colAccent).render()+
		txt(fmt.Sprintf("  %s completion over the session", fmtTokK(totalCompletion)), colFaint).render())

	l = append(l, txt("── where the budget went · by role ───────────────────", colMuted).render())
	if len(rec.Roles) == 0 {
		l = append(l, txt("no model calls recorded — nothing to split", colFaint).render())
	} else {
		for _, rs := range rec.Roles {
			pct := 0
			if totalCompletion > 0 {
				pct = rs.Tokens * 100 / totalCompletion
			}
			l = append(l, label2(rs.Role)+txt(sparkW([]float64{float64(pct) / 100.0}, 8), colAccent).render()+
				txt(fmt.Sprintf("  %s · %d%%", fmtTokK(rs.Tokens), pct), colFaint).render())
		}
	}

	l = append(l, txt("── tiers · cost-saving routing ───────────────────────", colMuted).render())
	totalCalls := rec.TierBig + rec.TierCheap
	bigPct, cheapPct := 0, 0
	if totalCalls > 0 {
		bigPct = rec.TierBig * 100 / totalCalls
		cheapPct = rec.TierCheap * 100 / totalCalls
	}
	l = append(l, label("tiers")+txt(fmt.Sprintf("big model %d%% (%d calls)", bigPct, rec.TierBig), colAccent).render()+
		txt(fmt.Sprintf("  ·  cheap model %d%% (%d calls)", cheapPct, rec.TierCheap), colFaint).render())

	l = append(l, txt("── the expensive moment ──────────────────────────────", colMuted).render())
	if rec.PeakTick >= 0 && rec.PeakTokens > 0 {
		l = append(l, label("peak")+txt(fmt.Sprintf("%s @t%d", fmtTokK(rec.PeakTokens), rec.PeakTick), colAccent).render()+
			txt("  ◀ the slow point — if it recurs, the cost to eliminate", colFaint).render())
	} else {
		l = append(l, txt("no standout peak — spend was even", colFaint).render())
	}
	return strings.Join(l, "\n")
}

// renderSelfBody — §9: the SELF·EVOLUTION change ledger — what the harness changed about ITSELF. The
// reads: "every self-change is in the log with a cause" (the "off-the-books change" bug-signature the
// audit guards), and "did the keep-or-revert actually undo a bad change?" (a ledger of only ADDs with
// zero UNDIDs means the gate isn't catching the bad ones). Scope is the governance mode it ran under.
// Pure over the reconstructed record.
func renderSelfBody(rec AnalysisRecord, cursor, width int) string {
	var l []string
	base := "no persisted baseline (fresh state)"
	if rec.SelfBaseSet {
		base = "baseline loaded · lineage anchored"
	}
	l = append(l, label("baseline")+txt(base, colSubtext).render())
	scope := rec.SelfScope
	if scope == "" {
		scope = "SAFE · settings only (default)"
	}
	l = append(l, label("scope")+txt(scope, colSubtext).render()+
		txt("  · structure + code LOCKED", colFaint).render())

	added, undone, refused := 0, 0, 0
	for _, c := range rec.SelfChanges {
		switch c.Action {
		case "ADDED":
			added++
		case "UNDID":
			undone++
		case "REFUSED":
			refused++
		}
	}
	l = append(l, label("summary")+txt(fmt.Sprintf("+%d changes · %d undone · %d refused",
		added, undone, refused), colSubtext).render())

	l = append(l, txt("── change ledger · what · evidence · gate ────────────", colMuted).render())
	if len(rec.SelfChanges) == 0 {
		l = append(l, txt("no self-change this session — it held its learned state", colFaint).render())
	} else {
		for _, c := range rec.SelfChanges {
			l = append(l, selfRow(c))
		}
	}
	return strings.Join(l, "\n")
}

// selfRow renders one §9 self-change line: tick · action · item · evidence. An ADDED (a change kept)
// reads OK; an UNDID (a reversal — the keep-or-revert working) reads amber; a REFUSED (the gate
// refusing) reads err. Pure.
func selfRow(c SelfChange) string {
	tone := colOk
	switch c.Action {
	case "UNDID":
		tone = colWarn
	case "REFUSED":
		tone = colErr
	}
	return txt(fmt.Sprintf("t%-4d ", c.Tick), colFaint).render() +
		txt(fmt.Sprintf("%-8s ", c.Action), tone).render() +
		txt(clipName(c.Item, 22)+"  ", colSubtext).render() +
		txt(quote(c.Evidence), colMuted).render()
}

// firstNonEmptyStr returns a if non-blank, else b (a render-side gist fallback for an empty branch
// text / worker role). Distinct from the loader's firstNonEmpty so the view layer is self-contained.
func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
