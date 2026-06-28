package cognition

// monitor_action.go — the ACTION · GROUNDING runtime monitor (W2 panel l), "is it actually touching
// reality — and does reality talk back?". The acts strip + the grounding scoreboard, where a REFUTED
// reads healthy (reality correcting the mind) and a fabricated>0 is the alarm. Pure content renderer.

import (
	"fmt"
	"strings"
)

// ActionView is the ACTION·GROUNDING monitor's data contract.
type ActionView struct {
	Horizon int // strip width (ticks); 0 ⇒ default

	Intention string // the current outward claim (the watched seam's outbound half)
	Acts      []bool // ACT/DELIVER seam crossings per tick

	Returned int // async observations that came back
	Fired    int // async actions fired (Returned of Fired ⇒ pending = Fired-Returned)
	DueTick  int // the next pending observation's due tick (0 ⇒ none pending)

	Grounded   int // reality confirmed a claim
	Refuted    int // reality refuted a claim (HEALTHY — the mind being corrected)
	Fabricated int // a tier-0 fabrication was rejected (>0 is the alarm)

	LastVerdict string // GROUNDED | REFUTED | FABRICATED
	LastTick    int
	LastReason  string
}

// RenderActionMonitor renders the ACTION·GROUNDING monitor body on the primitives. Pure.
func RenderActionMonitor(v ActionView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string

	if v.Intention != "" {
		lines = append(lines, label("intention")+txt(quote(v.Intention), colSubtext).render())
	}

	// acts strip (seam crossings).
	acts := 0
	for _, a := range v.Acts {
		if a {
			acts++
		}
	}
	lines = append(lines, label("acts")+Strip(v.Acts, w, colOk)+
		txt(fmt.Sprintf("   %d in %dt", acts, w), colFaint).render())

	// returned X of Y (Z pending, due in Nt)
	ret := label("returned") + txt(fmt.Sprintf("%d of %d", v.Returned, v.Fired), colAccent).render()
	if pending := v.Fired - v.Returned; pending > 0 {
		ret += txt(fmt.Sprintf(" (%d pending", pending), colFaint).render()
		if v.DueTick > 0 {
			ret += txt(fmt.Sprintf(", due tick %d", v.DueTick), colFaint).render()
		}
		ret += txt(")", colFaint).render()
	}
	lines = append(lines, ret)

	// verdicts scoreboard — REFUTED is healthy (Ok-toned), fabricated is the alarm (Err).
	verd := label("verdicts") +
		txt("grounded ", colFaint).render() + txt(fmt.Sprintf("%d", v.Grounded), colOk).render() +
		txt(" · refuted ", colFaint).render() + txt(fmt.Sprintf("%d", v.Refuted), colOk).render()
	fabTone := colFaint
	if v.Fabricated > 0 {
		fabTone = colErr
	}
	verd += txt(" · fabricated ", colFaint).render() + txt(fmt.Sprintf("%d", v.Fabricated), fabTone).render()
	lines = append(lines, verd)

	// last verdict + reason (two-row block). A REFUTED last verdict is NOT painted red.
	if v.LastVerdict != "" {
		hdr, sentence := lastBlock(v.LastVerdict, v.LastTick, v.LastReason)
		lines = append(lines, label("last")+txt(hdr, colSubtext).render())
		if sentence != "" {
			lines = append(lines, label("reason")+txt(quote(sentence), colMuted).render())
		}
	}
	return strings.Join(lines, "\n")
}
