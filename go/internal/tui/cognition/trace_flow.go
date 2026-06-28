package cognition

// trace_flow.go — the §G6 TRACE/FLOW swimlane renderer + the phase/freq math. The post-session twin
// of the by-hand round-trip trace (run -> --log -> parse JSONL): it turns the recorded event stream
// into a SWIMLANE timeline so the seed->thought->seam->subconscious->action ROUND-TRIP reads
// diagonally across lanes/ticks, with the late-injection / Reenter DESYNC markers highlighted and a
// PHASE/FREQ readout line (the operating-frequency / resonance number we are chasing). Pure over the
// record's TraceEvents projection (loader.go fillTrace) — no model, no clock, no RNG. Gated by the
// tui.trace_flow knob in RenderAnalysisTab; default OFF ⇒ the TRACE tab keeps the "panel pending"
// placeholder ⇒ byte-identical. (Track G, G6; design: 2026-06-20-shift-tab-analysis-redesign.md.)

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// traceLanes is the locked swimlane row order — the architecture's five layers top-to-bottom, so the
// round-trip reads DIAGONALLY (a stimulus enters at PORT, the trip descends through CONSCIOUS/SEAM/
// SUBCONSCIOUS and out at ACTION). laneOf (loader.go) maps each event to one of these.
var traceLanes = []string{"PORT", "CONSCIOUS", "SEAM", "SUBCONSCIOUS", "ACTION"}

// TraceReport is the §G6 PHASE/FREQ readout — the "operating frequency / resonance" numbers computed
// once over the record's TraceEvents (the single source of truth the cognition-property test asserts;
// the renderer is the VIEW over it). All deterministic Pattern-A reads off the recorded stream.
type TraceReport struct {
	TripTicks     int     // the round-trip length: first PORT arrival -> the DELIVER (Respond), in ticks
	TripStartTick int     // the tick the trip opened (the first PORT arrival), -1 if none
	DeliverTick   int     // the tick the trip delivered (the last Respond), -1 if no delivery
	Retracements  int     // count of conscious retracements (reenter/expand/focus) — the looping-back signal
	LateInjects   int     // count of late seam.inject markers — an injection re-opening a passed node
	LandToDeliver int     // ticks from the last USEFUL injection (seam.inject) to the DELIVER — the land->deliver LAG
	HasInject     bool    // whether any seam.inject landed at all (an injection-less trip has no land->deliver lag)
	Theta         float64 // the regulator dispatch threshold θ at the cursor (the cadence/admission knob)
	EventsOnLane  int     // total events placed on the five lanes (the trip's footprint)
}

// BuildTraceReport computes the §G6 phase/frequency numbers from a record's TraceEvents + the θ series.
// Pure. The land->deliver lag is the gap between the LAST seam.inject that landed before the DELIVER
// and the DELIVER itself — the "subconscious-land -> deliver" desync we are chasing. The retracement
// count is the number of conscious reopen ops (the trip looping back). Pure over its inputs.
func BuildTraceReport(rec AnalysisRecord, cursor int) TraceReport {
	rep := TraceReport{TripStartTick: -1, DeliverTick: -1, Theta: valAt(rec.Theta, cursor)}
	for _, te := range rec.TraceEvents {
		rep.EventsOnLane++
		if te.Lane == "PORT" && rep.TripStartTick < 0 {
			rep.TripStartTick = te.Tick
		}
		if te.Kind == "seam.inject" {
			rep.HasInject = true
			if te.Desync {
				rep.LateInjects++
			}
		}
		if te.Kind == "action.respond" || te.Kind == "action.ask" {
			rep.DeliverTick = te.Tick // the LAST delivery wins (the trip's close)
		}
		if te.Desync && te.Lane == "CONSCIOUS" {
			rep.Retracements++
		}
	}
	if rep.TripStartTick >= 0 && rep.DeliverTick >= rep.TripStartTick {
		rep.TripTicks = rep.DeliverTick - rep.TripStartTick
	}
	// the land->deliver lag: the gap from the LAST USEFUL injection — the most recent seam.inject that
	// landed AT OR BEFORE the DELIVER (a later inject is an afterthought outside this trip, not the one
	// that fed the answer) — to the DELIVER. This is the "subconscious-land -> deliver" desync we chase.
	if rep.HasInject && rep.DeliverTick >= 0 {
		lastUseful := -1
		for _, te := range rec.TraceEvents {
			if te.Kind == "seam.inject" && te.Tick <= rep.DeliverTick && te.Tick > lastUseful {
				lastUseful = te.Tick
			}
		}
		if lastUseful >= 0 {
			rep.LandToDeliver = rep.DeliverTick - lastUseful
		}
	}
	return rep
}

// renderTraceFlowBody renders the §G6 TRACE/FLOW swimlane: a header, the five lane rows (each placing
// its events at (tick, lane) across the shared scrub axis), the cursor column marker, the desync legend,
// and the phase/freq readout line. Pure over the record.
func renderTraceFlowBody(rec AnalysisRecord, cursor, width int) string {
	var l []string

	if len(rec.TraceEvents) == 0 {
		l = append(l, txt("── round-trip · no lane events recorded this session ──", colMuted).render())
		l = append(l, txt("the stream carried no port/conscious/seam/subconscious/action events to place", colFaint).render())
		return strings.Join(l, "\n")
	}

	span := traceSpan(rec)
	lw := traceLaneWidth(width)

	rep := BuildTraceReport(rec, cursor)

	// the header: the single-trip framing + the focused episode (the round-trip isolated).
	hdr := label("round-trip")
	if rep.TripStartTick >= 0 {
		hdr += txt(fmt.Sprintf("PORT t%d ", rep.TripStartTick), colWarn).render()
		hdr += txt("→ CONSCIOUS → SEAM → SUBCONSCIOUS → ", colFaint).render()
		if rep.DeliverTick >= 0 {
			hdr += txt(fmt.Sprintf("DELIVER t%d", rep.DeliverTick), colOk).render()
		} else {
			hdr += txt("(no DELIVER)", colWarn).render()
		}
	} else {
		hdr += txt("(no PORT arrival — the trip never opened)", colFaint).render()
	}
	l = append(l, hdr)

	// the scrub axis (the same `[ ]` axis the shell scrubs), labelled t0 .. span.
	l = append(l, txt(fmt.Sprintf("%-13s", "ticks"), colMuted).render()+
		txt("t0 ", colFaint).render()+txt(scrubBar(cursor, span, lw), colSubtext).render()+
		txt(fmt.Sprintf(" t%d", span), colFaint).render())

	// the five lane rows — each event placed at its proportional column on its lane.
	cursorCol := axisCol(cursor, span, lw)
	for _, lane := range traceLanes {
		l = append(l, traceLaneRow(lane, rec.TraceEvents, span, lw, cursorCol))
	}

	// the legend for the lane markers + the desync highlight (so a reviewer can read the glyphs).
	l = append(l, txt("── legend ─ PORT ◆ · CONSCIOUS • · SEAM f/g/t/▸inject · SUBCON s/▽ · ACTION ↑out ↓in *tool ▣deliver ──", colFaint).render())
	l = append(l, txt(traceMarker('!', colErr)+" desync (late inject re-opens a passed node / conscious retracement)  "+
		traceMarker('●', colAccent)+" cursor", colFaint).render())

	// the PHASE / FREQ readout — the operating-frequency / resonance number.
	l = append(l, txt("── phase / freq ────────────────────────────────────", colMuted).render())
	tripNote := "no completed trip"
	if rep.TripStartTick >= 0 && rep.DeliverTick >= 0 {
		tripNote = fmt.Sprintf("%d ticks (%ds)", rep.TripTicks, secsForTicks(rec, rep.TripTicks))
	}
	l = append(l, label("trip length")+txt(tripNote, colAccent).render()+
		txt("  ◀ PORT→DELIVER round-trip", colFaint).render())

	retTone := colSubtext
	if rep.Retracements > 0 {
		retTone = colWarn
	}
	l = append(l, label("retrace")+txt(fmt.Sprintf("%d", rep.Retracements), retTone).render()+
		txt(fmt.Sprintf("  conscious reopen (reenter/expand) · %d late inject", rep.LateInjects), colFaint).render())

	lagNote := "no injection landed (nothing to lag)"
	lagTone := colFaint
	if rep.HasInject && rep.DeliverTick >= 0 {
		lagNote = fmt.Sprintf("%d ticks (%ds)", rep.LandToDeliver, secsForTicks(rec, rep.LandToDeliver))
		lagTone = colAccent
		if rep.LandToDeliver == 0 {
			lagNote += " — landed at delivery"
		}
	}
	l = append(l, label("land-lag")+txt(lagNote, lagTone).render()+
		txt("  ◀ last useful injection → DELIVER (the desync lag)", colFaint).render())

	l = append(l, label("cadence")+txt(fmt.Sprintf("θ %.2f", rep.Theta), colAccent).render()+
		txt(fmt.Sprintf("  at the cursor · %d events on the trip lanes", rep.EventsOnLane), colFaint).render())

	return strings.Join(l, "\n")
}

// traceLaneRow renders one swimlane row: the lane label + a width-lw track with each lane event placed
// at its proportional column. A desync event overwrites its glyph with the highlighted `!` marker; the
// cursor column is marked with `●` only where no event sits (so an event is never hidden). Pure.
func traceLaneRow(lane string, evs []TraceEvent, span, lw, cursorCol int) string {
	track := make([]rune, lw)
	tone := make([]int, lw) // 0 = plain (subtext), 1 = desync (err), 2 = cursor (accent)
	for i := range track {
		track[i] = '·'
	}
	for _, te := range evs {
		if te.Lane != lane {
			continue
		}
		c := axisCol(te.Tick, span, lw)
		if c < 0 || c >= lw {
			continue
		}
		g := []rune(te.Glyph)
		if len(g) == 0 {
			continue
		}
		if te.Desync {
			track[c] = '!'
			tone[c] = 1
		} else if tone[c] != 1 { // never overwrite a desync marker with a plain one
			track[c] = g[0]
		}
	}
	// mark the cursor column where it does not collide with an event glyph (keep events visible).
	if cursorCol >= 0 && cursorCol < lw && track[cursorCol] == '·' {
		track[cursorCol] = '●'
		tone[cursorCol] = 2
	}

	// build the styled row, one styled run per column so desync columns light up err-red.
	var b strings.Builder
	b.WriteString(txt(fmt.Sprintf("%-13s", strings.ToLower(lane)), colMuted).render())
	for i, r := range track {
		c := colSubtext
		switch tone[i] {
		case 1:
			c = colErr
		case 2:
			c = colAccent
		default:
			if r == '·' {
				c = colFaint
			}
		}
		b.WriteString(txt(string(r), c).render())
	}
	return b.String()
}

// traceSpan returns the X-axis span (the last tick to place against): the record's Ticks when a
// SignalFrame sidecar set the scrub axis, else the largest TraceEvent tick (so the swimlane renders
// from the events alone on the no-sidecar / frozen-live path). Minimum 1 so axisCol never divides by 0.
func traceSpan(rec AnalysisRecord) int {
	span := rec.Ticks
	for _, te := range rec.TraceEvents {
		if te.Tick > span {
			span = te.Tick
		}
	}
	if span < 1 {
		span = 1
	}
	return span
}

// axisCol maps a tick onto a [0, width) column on the shared scrub axis (the same proportional mapping
// stimuliRow uses), clamped into range. Pure.
func axisCol(tick, span, width int) int {
	if width <= 0 {
		return -1
	}
	if span <= 0 {
		return 0
	}
	c := tick * (width - 1) / span
	if c < 0 {
		c = 0
	}
	if c >= width {
		c = width - 1
	}
	return c
}

// traceLaneWidth is the lane track width — the panel width minus the 13-wide lane label, clamped to a
// readable band. Pure.
func traceLaneWidth(width int) int {
	w := width - 16
	if w < 24 {
		w = 24
	}
	if w > 96 {
		w = 96
	}
	return w
}

// traceMarker renders a single legend glyph in a tone (a tiny styled-run helper for the legend line).
func traceMarker(r rune, c lipgloss.Color) string {
	return txt(string(r), c).render()
}
