package cognition

// monitor_seam.go — the HIDDEN SEAM runtime monitor (W2 panel c), "is the Filter killing what it
// should, and is the injection honest?". The four verdict/voiced lanes are full-width tick strips
// (the █/_ rolling window); the voice is the raw→voiced pair. Pure content renderer on the primitives.

import (
	"fmt"
	"strings"
)

// SeamView is the HIDDEN SEAM monitor's data contract. The four lanes are per-tick boolean
// histories over the strip horizon (newest last); the bridge fills them from the seam.* event
// stream (admit/flag/reject verdicts + the inject that voiced a candidate).
type SeamView struct {
	Horizon int // strip width (ticks); 0 ⇒ the default

	Admit  []bool // a candidate was ADMITTED this tick
	Flag   []bool // a candidate was FLAGGED this tick
	Reject []bool // a candidate was REJECTED this tick
	Voiced []bool // an injection crossed into the conscious stream this tick

	// the most recent re-voicing (the claim-preserved check): raw candidate → voiced thought.
	RawText    string
	VoicedText string
	VoicedAge  int // ticks since the voicing (for the freshness marker)
}

// RenderSeamMonitor renders the SEAM monitor body on the primitives: four labelled █/_ lanes each
// with a rolling %, then the raw→voiced pair. Pure.
func RenderSeamMonitor(v SeamView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string
	lane := func(name string, hits []bool, tone func() string) string {
		strip := tone()
		pct := rollingPct(hits, w)
		return label(name) + strip + txt(fmt.Sprintf("   %d%%", pct), colFaint).render()
	}
	lines = append(lines, lane("admit", v.Admit, func() string { return Strip(v.Admit, w, colOk) }))
	lines = append(lines, lane("flag", v.Flag, func() string { return Strip(v.Flag, w, colWarn) }))
	lines = append(lines, lane("reject", v.Reject, func() string { return Strip(v.Reject, w, colErr) }))
	lines = append(lines, lane("voiced", v.Voiced, func() string { return Strip(v.Voiced, w, colAccent) }))

	// the raw→voiced pair (an explicit connected pair; the claim-preserved check is a visual diff).
	if v.RawText != "" || v.VoicedText != "" {
		lines = append(lines, label("raw")+txt(quote(v.RawText), colSubtext).render())
		voiced := txt(" └ voiced ", colFaint).render() + txt(quote(v.VoicedText), colAccent).render()
		voiced += txt("   "+ageLabel(v.VoicedAge), colFaint).render()
		lines = append(lines, voiced)
	}
	return strings.Join(lines, "\n")
}

// rollingPct is the percentage of TRUE ticks over the last `w` of hits — the lane's rolling rate.
func rollingPct(hits []bool, w int) int {
	if w <= 0 {
		return 0
	}
	if len(hits) > w {
		hits = hits[len(hits)-w:]
	}
	if len(hits) == 0 {
		return 0
	}
	n := 0
	for _, h := range hits {
		if h {
			n++
		}
	}
	return (n*100 + len(hits)/2) / len(hits) // rounded
}
