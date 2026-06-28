package cognition

// monitor_subconscious.go — the SUBCONSCIOUS runtime monitor (W2 panel b), "did the right
// specialists fire — and is the roster real and in use?". The top line is the running PROCESS
// (workflow/operator), then the live agents that fired recently (decaying off the panel). Pure.

import (
	"fmt"
	"strings"
)

// AgentRow is one live specialist on the SUBCONSCIOUS monitor: who fired, how strongly, what it
// produced (raw), its age, and — for runtime-minted machinery — the birth tick.
type AgentRow struct {
	Name      string
	Relevance float64
	Note      string // the raw candidate text, or "below θ(...)" when it didn't fire
	Age       int    // ticks since it last fired (0 ⇒ now)
	MintedAt  int    // birth tick if minted at runtime (0 ⇒ seed, no tag)
}

// SubconsciousView is the SUBCONSCIOUS monitor's data contract: the running process + the live agents.
type SubconsciousView struct {
	// the running process (empty ⇒ plain dispatch, no workflow this tick).
	Workflow      string // active workflow/program name
	WorkflowMinTk int    // birth tick if the workflow was synthesised at runtime (0 ⇒ seed)
	Phase         string // e.g. "2/3"
	Operator      string // the operator currently applied

	Theta  float64    // the dispatch threshold (the regulator moves it)
	Agents []AgentRow // live agents, most-recent first (the bridge caps + decays the list)
}

// RenderSubconsciousMonitor renders the SUBCONSCIOUS monitor body on the primitives. Pure.
func RenderSubconsciousMonitor(v SubconsciousView) string {
	var lines []string

	// running process line (or a quiet marker).
	if v.Workflow != "" {
		run := label("running") + txt(quote(v.Workflow), colSubtext).render()
		if v.WorkflowMinTk > 0 {
			run += txt(fmt.Sprintf(" (minted t%d)", v.WorkflowMinTk), colFaint).render()
		}
		if v.Phase != "" {
			run += txt(" · phase "+v.Phase, colFaint).render()
		}
		if v.Operator != "" {
			run += txt(" · op ", colFaint).render() + txt(v.Operator, colAccent).render()
		}
		lines = append(lines, run)
	} else {
		lines = append(lines, label("running")+txt("plain dispatch (no workflow)", colFaint).render())
	}

	// live agents — decaying list. A fired agent leads with ▸; one below θ is dim.
	for _, a := range v.Agents {
		marker := "  "
		nameTone := colSubtext
		if a.Age == 0 {
			marker = txt("▸ ", colOk).render()
			nameTone = colText
		}
		row := marker + txt(fmt.Sprintf("%-18s", a.Name), nameTone).render() +
			txt(fmt.Sprintf("%.2f ", a.Relevance), colAccent).render() +
			txt(a.Note, colMuted).render()
		if a.MintedAt > 0 {
			row += txt(fmt.Sprintf("  minted t%d", a.MintedAt), colFaint).render()
		}
		row += txt("   "+ageLabel(a.Age), colFaint).render()
		lines = append(lines, row)
	}
	if len(v.Agents) == 0 {
		lines = append(lines, txt("  (no specialist fired recently)", colFaint).render())
	}
	return strings.Join(lines, "\n")
}
