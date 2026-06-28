package cognition

// monitor_conscious.go — the CONSCIOUS / thought-graph runtime monitor (W2 panel d), simplified to
// five aligned rows: the active line, the branch counts, the strongest waiting line (resume pull),
// the graph-op indicator lights, and the last op spelled out. Pure content renderer.

import (
	"fmt"
	"strings"
)

// consciousOps is the graph-op indicator vocabulary in fixed order (the CONSCIOUS op lights).
var consciousOps = []string{"COMPRESS", "BRANCH", "MERGE", "EXPAND"}

// ConsciousView is the CONSCIOUS monitor's data contract.
type ConsciousView struct {
	ActiveID    int     // the EXPANDED branch id
	ActiveText  string  // its goal/first-thought gist
	Thoughts    int     // thought count on the active branch
	ActiveValue float64 // V(s) of the active branch

	LiveBranches int // ACTIVE + STASHED
	DeadBranches int

	BestID    int     // the strongest waiting (frontier) branch (the resume pull; -1 ⇒ none)
	BestText  string  // its gist
	BestValue float64 // its V — an unanswered user line sits here at 1.00

	Op       string // the graph op that fired this tick (lit in the indicator row)
	OpTick   int
	OpDetail string // the op's gist/detail (its own line)
}

// RenderConsciousMonitor renders the CONSCIOUS monitor body (five aligned rows) on the primitives. Pure.
func RenderConsciousMonitor(v ConsciousView) string {
	var lines []string

	// active   b4 "..." · N thoughts · V x.xx
	active := label("active") +
		txt(fmt.Sprintf("b%d ", v.ActiveID), colAccent).render() +
		txt(quote(v.ActiveText), colText).render() +
		txt(fmt.Sprintf(" · %d thoughts · V ", v.Thoughts), colFaint).render() +
		txt(fmt.Sprintf("%.2f", v.ActiveValue), colAccent).render()
	lines = append(lines, active)

	// branches N live · M dead
	lines = append(lines, label("branches")+
		txt(fmt.Sprintf("%d", v.LiveBranches), colAccent).render()+txt(" live · ", colFaint).render()+
		txt(fmt.Sprintf("%d", v.DeadBranches), colAccent).render()+txt(" dead", colFaint).render())

	// best     bN "..." · V x.xx   (its own row — what is pulling to be resumed)
	if v.BestID >= 0 {
		lines = append(lines, label("best")+
			txt(fmt.Sprintf("b%d ", v.BestID), colAccent).render()+
			txt(quote(v.BestText), colSubtext).render()+
			txt(" · V ", colFaint).render()+txt(fmt.Sprintf("%.2f", v.BestValue), colAccent).render())
	} else {
		lines = append(lines, label("best")+txt("(no other live line)", colFaint).render())
	}

	// operation indicator lights
	lines = append(lines, label("operation")+IndicatorRow(consciousOps, v.Op, colAccent))

	// last     OP · tick N   +   detail on its own row
	if v.Op != "" {
		hdr, _ := lastBlock(v.Op, v.OpTick, "")
		lines = append(lines, label("last")+txt(hdr, colSubtext).render())
		if v.OpDetail != "" {
			lines = append(lines, label("")+txt(quote(v.OpDetail), colMuted).render())
		}
	}
	return strings.Join(lines, "\n")
}
