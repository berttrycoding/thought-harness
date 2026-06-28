package tui

// monitors.go — assembles the live runtime-monitor pull-up: it joins the SCALAR adapters (over the
// end-of-tick snapshot) with the STRIP histories (the per-tick event lanes the app accrues), builds
// each monitor's View, and renders the boxed stack. This is where the two data paths meet.

import (
	"os"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
)

// monitorPanel pairs a canonical panel ID (config.PanelRegistry vocabulary) with its display title and
// rendered body. The G5 customization selects + reorders this set by ID; the title is the human label.
type monitorPanel struct {
	id    string // canon ID — a member of config.PanelRegistry
	title string // human display title (the boxed header)
	body  string // the rendered monitor body
}

// monitorStack renders the live monitor pull-up at the given total box width — the FULL validation
// instrument stack (every W2 monitor, the 2026-06-12 mockups), top-to-bottom: organism → loop →
// subconscious → seam → conscious/value → action → systems → registries → self. The scalar fields
// come from the per-panel snapshot adapters; the per-tick strips come from the rolling event history
// (monHist). The stack is taller than a screen; the View() pull-up branch windows + scrolls it.
//
// G5 (Track G — panel customization): the visible panels + their order + the per-panel strip horizon
// come from config.ResolvePullupPanels() over the live engine config (tui.pullup.panels). Default OFF ⇒
// the canonical full PanelRegistry order at the default horizon ⇒ byte-identical to the pre-G5 stack.
func (a *App) monitorStack(width int) string {
	d := a.vm.Snap
	h := a.monHist

	order, hz := a.resolvePullup()

	// VITALS — the organism rollup; the salient-input strip is the live arrivals lane.
	vit := cognition.VitalsViewFromSnapshot(d)
	vit.Horizon = hz
	vit.Input = h.input

	// CONTROLLER — scalar judgment + the escalate strip is left to accrue (no escalation lane on the
	// control floor; the strip stays flat, which is itself the correct read in control mode).
	ctrl := cognition.ControllerViewFromSnapshot(d)
	ctrl.Horizon = hz

	// SUBCONSCIOUS — running process + θ (the live-agent roster is event-fed, populated later).
	subc := cognition.SubconsciousViewFromSnapshot(d)

	// OPERATORS — catalog scale + the apply strip (the aggregate registry used-lane).
	ops := cognition.OperatorsViewFromSnapshot(d)
	ops.Horizon = hz
	ops.Apply = h.used

	trig := cognition.TriggersViewFromSnapshot(d)
	trig.Horizon = hz

	// HIDDEN SEAM — strips + the last re-voicing, entirely event-derived.
	seam := cognition.SeamView{
		Horizon: hz,
		Admit:   h.admit, Flag: h.flag, Reject: h.reject, Voiced: h.voiced,
		RawText: h.rawVoice, VoicedText: h.voicedVoice,
	}

	// VALUE — priority + live ranking; the reward strip is sparse-by-design (grounded rewards only).
	val := cognition.ValueViewFromSnapshot(d)
	val.Horizon = hz
	val.Reward = h.reward

	// ACTION · GROUNDING — the acts strip (ACT/DELIVER seam crossings) + the verdict scoreboard.
	act := cognition.ActionViewFromSnapshot(d)
	act.Horizon = hz
	act.Acts = h.acts

	sess := cognition.SessionsViewFromSnapshot(d)
	sess.Horizon = hz

	// REGULATOR — scalar vitals + the deferred strip.
	regu := cognition.RegulatorViewFromSnapshot(d)
	regu.Horizon = hz
	regu.Deferred = h.deferred

	thr := cognition.ThroughputViewFromSnapshot(d)

	// REGISTRIES — scalar scale (minted counts) + the aggregate used strip from the history (the
	// per-registry in-use detail is a later refinement; the strip is the honest aggregate signal).
	reg := cognition.RegistriesViewFromSnapshot(d)
	reg.Horizon = hz
	reg.Used = h.used

	// MEMORY — declarative store sizes + the recall strip.
	mem := cognition.MemoryViewFromSnapshot(d)
	mem.Horizon = hz
	mem.Recall = h.recall

	// KNOWLEDGE — recall strip live; entry counts pending the snapshot feed.
	know := cognition.KnowledgeViewFromSnapshot(d)
	know.Horizon = hz
	know.Recall = h.recall

	// The canonical panel set, keyed by config.PanelRegistry ID — the SINGLE SOURCE of bodies the G5
	// resolver selects + reorders from.
	byID := map[string]monitorPanel{
		"VITALS":       {"VITALS", "VITALS", cognition.RenderVitalsMonitor(vit)},
		"LOOP":         {"LOOP", "LOOP", cognition.RenderLoopMonitor(cognition.LoopViewFromSnapshot(d))},
		"CONTROLLER":   {"CONTROLLER", "CONTROLLER", cognition.RenderControllerMonitor(ctrl)},
		"SUBCONSCIOUS": {"SUBCONSCIOUS", "SUBCONSCIOUS", cognition.RenderSubconsciousMonitor(subc)},
		"OPERATORS":    {"OPERATORS", "OPERATORS", cognition.RenderOperatorsMonitor(ops)},
		"TRIGGERS":     {"TRIGGERS", "TRIGGERS · SCHEDULE", cognition.RenderTriggersMonitor(trig)},
		"SEAM":         {"SEAM", "HIDDEN SEAM", cognition.RenderSeamMonitor(seam)},
		"CONSCIOUS":    {"CONSCIOUS", "CONSCIOUS · thought graph", cognition.RenderConsciousMonitor(cognition.ConsciousViewFromSnapshot(d))},
		"VALUE":        {"VALUE", "VALUE", cognition.RenderValueMonitor(val)},
		"ACTION":       {"ACTION", "ACTION · GROUNDING", cognition.RenderActionMonitor(act)},
		"SESSIONS":     {"SESSIONS", "SESSIONS · SUB-AGENTS", cognition.RenderSessionsMonitor(sess)},
		"REGULATOR":    {"REGULATOR", "REGULATOR · SCHEDULER", cognition.RenderRegulatorMonitor(regu)},
		"THROUGHPUT":   {"THROUGHPUT", "THROUGHPUT", cognition.RenderThroughputMonitor(thr)},
		"REGISTRIES":   {"REGISTRIES", "REGISTRIES", cognition.RenderRegistriesMonitor(reg)},
		"MEMORY":       {"MEMORY", "MEMORY", cognition.RenderMemoryMonitor(mem)},
		"KNOWLEDGE":    {"KNOWLEDGE", "KNOWLEDGE", cognition.RenderKnowledgeMonitor(know)},
		"SELF":         {"SELF", "SELF · EVOLUTION", cognition.RenderSelfMonitor(cognition.SelfViewFromSnapshot(d))},
	}

	out := make([]string, 0, len(order))
	for _, id := range order {
		p, ok := byID[id]
		if !ok {
			continue // a registry ID with no body (defensive; PanelRegistry and byID are kept in lockstep)
		}
		out = append(out, cognition.BoxedMonitor(p.title, p.body, width))
	}
	return strings.Join(out, "\n")
}

// resolvePullup returns the G5-resolved `^O` panel order + per-panel strip horizon for the CURRENT live
// config (nil engine/features ⇒ the canonical full order at the default horizon, byte-identical). It is
// the View-side adapter onto config.ResolvePullupPanels (the one place the customization is resolved).
func (a *App) resolvePullup() (order []string, horizon int) {
	feat := a.features()
	if feat == nil {
		return append([]string(nil), config.PanelRegistry...), config.DefaultStripHorizon
	}
	return feat.ResolvePullupPanels()
}

// features returns the live engine's shared HarnessConfig (read-only here), or nil when no engine is
// attached behind the bridge (the welcome/no-substrate path).
func (a *App) features() *config.HarnessConfig {
	eng := a.bridge.Engine()
	if eng == nil {
		return nil
	}
	return eng.Features()
}

// emitPullupCustomize emits the G5 tui.pullup observability event onto the live bus when a CUSTOMIZED
// panel layout is rendered (the master knob is ON and the resolved layout differs from the canonical
// full order/horizon). De-duplicated by a layout signature so it fires once per distinct layout, not
// every frame. Knob OFF ⇒ the canonical layout ⇒ no event (the default surface stays byte-identical).
// Called from the Update side (when the pull-up is opened), never from View (View stays pure).
func (a *App) emitPullupCustomize() {
	feat := a.features()
	if feat == nil || !feat.Tui.PullupPanels {
		return
	}
	order, horizon := feat.ResolvePullupPanels()
	sig := strings.Join(order, ",") + "|" + strconv.Itoa(horizon)
	canon := strings.Join(config.PanelRegistry, ",") + "|" + strconv.Itoa(config.DefaultStripHorizon)
	if sig == canon || sig == a.pullupLayoutSig {
		return // the canonical layout (nothing customized) or already emitted this layout
	}
	a.pullupLayoutSig = sig
	eng := a.bridge.Engine()
	if eng == nil {
		return
	}
	eng.Bus().Emit(string(events.PullupCustomize), "customized ^O pull-up: "+strconv.Itoa(len(order))+" panels, horizon "+strconv.Itoa(horizon), events.D{
		"panels":  append([]string(nil), order...),
		"count":   len(order),
		"horizon": horizon,
		"total":   len(config.PanelRegistry),
		"source":  "pullup",
	})
}

// pullupBounds renders the monitor stack at the current width and returns its lines, the viewport
// height (the screen minus the one hint row), and the maximum top-line scroll offset. Read-only — the
// Update-side clamp and the View-side window both call it so they agree on the bounds (D3 purity).
func (a *App) pullupBounds() (lines []string, viewH, maxScroll int) {
	w := a.w * 3 / 4
	if w < 60 {
		w = a.w - 4
	}
	lines = strings.Split(a.monitorStack(w), "\n")
	viewH = a.h - 1 // reserve the hint row at the bottom
	if viewH < 1 {
		viewH = 1
	}
	maxScroll = len(lines) - viewH
	if maxScroll < 0 {
		maxScroll = 0
	}
	return lines, viewH, maxScroll
}

// clampPullupScroll bounds the pull-up scroll offset to [0, maxScroll]. This is the Update-side owner
// of the bound (View only reads the offset), matching the clampCogScroll discipline.
func (a *App) clampPullupScroll() {
	_, _, maxScroll := a.pullupBounds()
	if a.pullupScroll > maxScroll {
		a.pullupScroll = maxScroll
	}
	if a.pullupScroll < 0 {
		a.pullupScroll = 0
	}
}

// clampAnalysis bounds the ANALYSIS preview's tab cursor (wraps) and scrub cursor ([0, Ticks-1]). The
// Update-side owner of the bounds (View only reads them) — the D3-purity discipline.
func (a *App) clampAnalysis() {
	n := cognition.AnalysisTabCount()
	if n > 0 {
		a.anTab = ((a.anTab % n) + n) % n // wrap both directions
	}
	if a.anCursor < 0 {
		a.anCursor = 0
	}
	if a.anRecA.Ticks > 0 && a.anCursor >= a.anRecA.Ticks {
		a.anCursor = a.anRecA.Ticks - 1
	}
}

// frozenOrSample builds the SINGLE analysis record opened by ^Y: the FROZEN RUNNING SESSION when the
// live mind has produced events (a real cognition.RecordFromFrozen off the bridge's bus tap — the G1
// data path), or the deterministic SAMPLE record before the first event (a fresh engine), so the
// surface always renders. The fall-back is taken when the freeze yields a record with no scrub axis
// AND no captured cognition (no stimuli/decisions) — i.e. nothing real to analyse yet.
func (a *App) frozenOrSample() cognition.AnalysisRecord {
	rec := a.bridge.FreezeRecord("live-frozen")
	if rec.Ticks == 0 && len(rec.Stimuli) == 0 && len(rec.Decisions) == 0 {
		return cognition.SampleAnalysisRecord("A")
	}
	return rec
}

// compareLoadEnabled reports whether the G2 disk-load COMPARE path is on (the tui.compare_load knob,
// read off the live engine's shared HarnessConfig). Default OFF ⇒ COMPARE keeps the prototype
// frozen-A/sample-B pair and never touches the filesystem (byte-identical). nil engine/features ⇒ OFF.
func (a *App) compareLoadEnabled() bool {
	eng := a.bridge.Engine()
	if eng == nil {
		return false
	}
	feat := eng.Features()
	return feat != nil && feat.Tui.CompareLoad
}

// registryHeatEnabled reports whether the G3 registry/memory FAMILY heat-map tab is on (the
// tui.registry_heatmap knob, read off the live engine's shared HarnessConfig). Default OFF ⇒ the
// REGISTRIES analysis tab keeps the "panel pending" placeholder (byte-identical to the G2 surface).
// nil engine/features ⇒ OFF.
func (a *App) registryHeatEnabled() bool {
	eng := a.bridge.Engine()
	if eng == nil {
		return false
	}
	feat := eng.Features()
	return feat != nil && feat.Tui.RegistryHeatmap
}

// deepLedgersEnabled reports whether the G4 DEEP ledgers + tree tabs are on (the tui.deep_ledgers knob,
// read off the live engine's shared HarnessConfig). Default OFF ⇒ the four deep analysis tabs
// (CONSCIOUS / ACTION·SESS / THROUGHPUT / SELF) keep the "panel pending" placeholder (byte-identical to
// the G2/G3 surface). nil engine/features ⇒ OFF.
func (a *App) deepLedgersEnabled() bool {
	eng := a.bridge.Engine()
	if eng == nil {
		return false
	}
	feat := eng.Features()
	return feat != nil && feat.Tui.DeepLedgers
}

// traceFlowEnabled reports whether the G6 TRACE/FLOW swimlane tab is on (the tui.trace_flow knob, read
// off the live engine's shared HarnessConfig). Default OFF ⇒ the TRACE analysis tab keeps the "panel
// pending" placeholder (byte-identical to the G2/G3/G4 surface). nil engine/features ⇒ OFF.
func (a *App) traceFlowEnabled() bool {
	eng := a.bridge.Engine()
	if eng == nil {
		return false
	}
	feat := eng.Features()
	return feat != nil && feat.Tui.TraceFlow
}

// emitTraceView witnesses the G6 TRACE/FLOW swimlane on the bus (the tui.trace_view event) when the
// TRACE tab is opened with the tui.trace_flow knob ON. Like emitPullupCustomize, it is a VIEW-surface
// signal emitted by the App, never by the engine tick, so a headless run never emits it. De-duplicated:
// it fires once per (record, cursor) the trace tab is shown for, not every frame. Knob OFF ⇒ no event
// (the TRACE tab is the "panel pending" placeholder) ⇒ the default surface stays byte-identical. Called
// from the Update side (when the analysis surface lands on the TRACE tab), never from View.
func (a *App) emitTraceView() {
	if !a.traceFlowEnabled() {
		return
	}
	if cognition.AnalysisTabName(a.anTab) != "TRACE" || a.anCompare {
		return
	}
	rep := cognition.BuildTraceReport(a.anRecA, a.anCursor)
	sig := a.anRecA.Name + "|" + strconv.Itoa(rep.TripTicks) + "|" + strconv.Itoa(rep.Retracements)
	if sig == a.traceViewSig {
		return // already witnessed this trip
	}
	a.traceViewSig = sig
	eng := a.bridge.Engine()
	if eng == nil {
		return
	}
	eng.Bus().Emit(string(events.TraceView), "TRACE/FLOW swimlane: round-trip "+strconv.Itoa(rep.TripTicks)+
		"t, "+strconv.Itoa(rep.Retracements)+" retracements", events.D{
		"trip_ticks":      rep.TripTicks,
		"retracements":    rep.Retracements,
		"land_to_deliver": rep.LandToDeliver,
		"theta":           rep.Theta,
	})
}

// compareRunsDir is the directory the COMPARE benchmark enumerates for recorded runs (the redesign §0
// picker source). The convention is a "runs" directory under the working directory (the mock's
// runs/*.jsonl) — the same place a --log run writes its event JSONL + the G0 *.signals.jsonl sidecar.
// THOUGHT_RUNS_DIR overrides it (an ops knob for where logs live; the View layer may read an env var,
// the engine never does).
func (a *App) compareRunsDir() string {
	if d := os.Getenv("THOUGHT_RUNS_DIR"); strings.TrimSpace(d) != "" {
		return d
	}
	return "runs"
}

// enterCompare toggles the COMPARE view. With the G2 tui.compare_load knob ON, turning compare ON
// LOADS the two most recent recorded runs from disk into A/B (newest = A, next = B) — the power-ON/OFF
// benchmark over REAL recordings (redesign §7). It reports a system line so the load is observable (and
// names the records it read). When the knob is OFF, or fewer than two records exist, it falls back to
// the prototype pair already in anRecA/anRecB — so the toggle always does SOMETHING and the default is
// byte-identical. Turning compare OFF restores the SINGLE record (the frozen/sample A).
func (a *App) enterCompare() {
	if a.anCompare {
		// leaving COMPARE — drop back to the SINGLE record (A), the frozen/sample run.
		a.anCompare = false
		return
	}
	a.anCompare = true
	if !a.compareLoadEnabled() {
		return // prototype A/B (frozen-A / sample-B) — the byte-identical default
	}
	ra, rb, ok := cognition.LoadComparePair(a.compareRunsDir())
	if !ok {
		a.chatView.System("compare: fewer than two recorded runs in " + a.compareRunsDir() + "/ — showing the sample A/B")
		return
	}
	a.anRecA, a.anRecB = ra, rb
	a.clampAnalysis() // the loaded A may have a different tick count — re-bound the scrub cursor
	rep := cognition.BuildCompareReport(ra, rb)
	a.chatView.System("compare A=" + ra.Name + " vs B=" + rb.Name + " — " + rep.Headline)
}

// nextStimulus returns the next/prev stimulus tick from the cursor (dir +1/-1), or the cursor itself
// if there is none in that direction — the `{`/`}` jump for the scrub axis.
func (a *App) nextStimulus(cur, dir int) int {
	st := a.anRecA.Stimuli
	if dir > 0 {
		for _, s := range st {
			if s.Tick > cur {
				return s.Tick
			}
		}
		return cur
	}
	res := cur
	for _, s := range st {
		if s.Tick < cur {
			res = s.Tick
		}
	}
	return res
}
