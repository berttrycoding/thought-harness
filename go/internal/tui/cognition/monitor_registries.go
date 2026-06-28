package cognition

// monitor_registries.go — the REGISTRIES runtime monitor (W2 panel h), "growing AND in use — is the
// learned machinery real?". The campaign's live verdict panel: each registry pairs SCALE with
// IN-USE on one row, and the `used` strip is dense when scale is real, thin when a batch is filler.
// Pure content renderer.

import (
	"fmt"
	"strings"
)

// RegistryRow is one registry on the rollup: its seed/minted scale and this session's in-use count.
type RegistryRow struct {
	Name    string // operators | specialists | skills | knowledge | memory
	Seed    int    // seed/base count
	Minted  int    // minted/added this lineage (0 ⇒ omit the "+ N minted")
	InUse   int    // fired / matched / recalled this session
	UseVerb string // "fired" | "matched" | "recalled" | "consulted"
	Extra   string // optional parenthetical (e.g. memory's "12 episodes · 7 beliefs · 3 prefs")
}

// RegistriesView is the REGISTRIES monitor's data contract.
type RegistriesView struct {
	Horizon int           // strip width (ticks); 0 ⇒ default
	Rows    []RegistryRow // one per registry
	Used    []bool        // any registry entry was exercised this tick (the campaign's live verdict)

	LastEvent  string // MINTED | DEMOTED | BATCH (the most recent registry event)
	LastDetail string // e.g. specialist "learned:verify"
	LastTick   int
	LastReason string // the evidence (e.g. "3 grounded repeats")
}

// RenderRegistriesMonitor renders the REGISTRIES rollup body on the primitives. Pure.
func RenderRegistriesMonitor(v RegistriesView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string

	for _, r := range v.Rows {
		scale := fmt.Sprintf("%d", r.Seed)
		if r.Minted > 0 {
			scale += fmt.Sprintf(" + %d minted", r.Minted)
		}
		row := label(r.Name) + txt(scale, colAccent).render()
		if r.Extra != "" {
			row += txt(" ("+r.Extra+")", colFaint).render()
		}
		// per-registry in-use count only when a verb is set; otherwise the aggregate `used` strip
		// below carries the in-use signal (no misleading per-row "in use 0").
		if r.UseVerb != "" {
			row += txt(" · "+r.UseVerb+" ", colFaint).render() + txt(fmt.Sprintf("%d", r.InUse), colOk).render()
		}
		lines = append(lines, row)
	}

	// used strip — the live verdict: dense = real capability, thinning = filler.
	used := 0
	for _, u := range v.Used {
		if u {
			used++
		}
	}
	lines = append(lines, label("used")+Strip(v.Used, w, colOk)+
		txt(fmt.Sprintf("   %d in %dt", used, w), colFaint).render())

	// last registry event + its evidence (two-row block).
	if v.LastEvent != "" {
		hdr := v.LastEvent
		if v.LastDetail != "" {
			hdr += " " + v.LastDetail
		}
		hdr += fmt.Sprintf(" · tick %d", v.LastTick)
		lines = append(lines, label("last")+txt(hdr, colSubtext).render())
		if v.LastReason != "" {
			lines = append(lines, label("reason")+txt(v.LastReason, colMuted).render())
		}
	}
	return strings.Join(lines, "\n")
}
