package cognition

// perception.go — the PERCEPTION ("Senses") rail panel: a pure VIEW over the perception.* event
// stream from the cognitive power-cycle. It surfaces the two senses that, until now, only appeared in
// the raw JSONL event log:
//
//   - perception.clock  — the read_clock boundary sense {value, mode (record|replay), tick}, plus the
//     divergence-refusal variant {reason:"divergence", log_version, want_version, log_substrate,
//     want_substrate} when a percept-log is rejected for a version/substrate mismatch.
//   - perception.orient — the orientation pass on a resumed wake: the prior-focus gist, the sensed
//     clock, the self-state (goal · open_lines · host footprint alloc_mb/sys_mb/goroutines ·
//     recent_events), and whether the sensed date was written back as a grounded belief.
//
// The renderer is a pure view over the ViewModel's recent event tail (it never imports the engine —
// plain data in, styled lines out), following the same shape as renderConfigEvents / renderContinuous.

import (
	"fmt"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// renderPerception — the live SENSES panel. It shows (a) the latest clock sense (value + record|replay
// mode, or a distinct divergence-refusal line), and (b) the latest orientation pass (prior-focus gist,
// self-state, host footprint, and the belief-written verdict). With no perception.* events it explains
// itself rather than rendering empty (sensing is a knob, off by default).
func renderPerception(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string

	// (a) the clock sense — newest of either the normal record/replay read or the divergence refusal.
	if ev := lastEvent(vm, events.PerceptionClock); ev != nil {
		if dataStr(ev, "reason") == "divergence" {
			// the refused-replay variant: a version/substrate mismatch left the on-disk log untouched and
			// cold-sensed instead. A distinct, warn-toned line so it never reads as a normal read.
			lines = append(lines, txt(clip("clock  REFUSED — divergence (cold-sense)", w), colWarn).render())
			vw := clip(fmt.Sprintf("log v%d != want v%d", intData(ev, "log_version"), intData(ev, "want_version")), w)
			lines = append(lines, txt("  "+vw, colFaint).render())
			if ls, ws := dataStr(ev, "log_substrate"), dataStr(ev, "want_substrate"); ls != "" || ws != "" {
				lines = append(lines, txt(clip("  substrate "+orNoneStr(ls)+" != "+orNoneStr(ws), w), colFaint).render())
			}
		} else {
			mode := dataStr(ev, "mode")   // "record" | "replay"
			value := dataStr(ev, "value") // the sensed UTC clock string
			tone := colAccent             // record = a fresh live read
			if mode == "replay" {
				tone = colSubtext // replay = a logged value re-played (deterministic), de-emphasised
			}
			lines = append(lines, txt("clock  ", colMuted).render()+txt(clip(orNoneStr(value), w-7), tone).render())
			lines = append(lines, txt(clip(fmt.Sprintf("       %s · tick %d", orNoneStr(mode), intData(ev, "tick")), w), colFaint).render())
		}
	}

	// (b) the orientation pass — the resume re-grounding witness.
	if ev := lastEvent(vm, events.PerceptionOrient); ev != nil {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		resume := dataBool(ev, "resume")
		head := "orient  cold (no prior)"
		if resume {
			head = "orient  resume — re-grounding both layers"
		}
		lines = append(lines, txt(clip(head, w), colAccent).render())

		// prior focus gist (the compressed prior-session focus, or "(none)").
		gist := dataStr(ev, "gist")
		lines = append(lines, wrapEntry(txt("  focus ", colMuted).render(), 8, orNoneStr(gist), colSubtext, w)...)

		// the self-state: the goal + the open-line count.
		self := dataStr(ev, "self")
		lines = append(lines, wrapEntry(txt("  self  ", colMuted).render(), 8, orNoneStr(self), colSubtext, w)...)
		lines = append(lines, txt(clip(fmt.Sprintf("  lines %d open", intData(ev, "open_lines")), w), colFaint).render())

		// the host footprint (reach=self introspection) — only meaningful when the host sensor fired.
		if dataBool(ev, "host_ok") {
			host := fmt.Sprintf("  host  %dMB alloc · %dMB sys · %d goroutines · %d recent events",
				intData(ev, "alloc_mb"), intData(ev, "sys_mb"), intData(ev, "goroutines"), intData(ev, "recent_events"))
			lines = append(lines, wrapEntry(txt("", colMuted).render(), 8, strings.TrimLeft(host, " "), colFaint, w-2)...)
		}

		// the perception->memory handshake verdict: was the sensed date written as a grounded belief?
		if dataBool(ev, "belief") {
			lines = append(lines, txt(clip("  belief  sensed date written (grounded)", w), colOk).render())
		} else {
			lines = append(lines, txt(clip("  belief  not written", w), colFaint).render())
		}
	}

	if len(lines) == 0 {
		return Panel{Body: faintStr(clip("(no senses — perception.* off; enable sense.clock / sense.orient)", w))}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// intData reads an int-ish field off an event's Data map (the clock tick, open_lines, the host
// footprint counts), reusing dataFloat's int/float coercion. Pure.
func intData(e *events.Event, k string) int { return int(dataFloat(e, k)) }

// orNoneStr renders an empty string as the explicit "(none)" placeholder (the panels never show a bare
// blank where a value is expected). Pure.
func orNoneStr(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
