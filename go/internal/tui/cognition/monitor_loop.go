package cognition

// monitor_loop.go — the LOOP runtime monitor (W2 panel a), the "is the mind alive, in the right
// regime, working on the right thing — now?" glance. Built entirely on the monitor primitives so it
// holds the locked conventions automatically. This is the CONTENT renderer (a multi-line body); the
// pull-up frame + the live data wiring are the joint-validation integration step.

import (
	"fmt"
	"strings"
)

// loopDecisions is the executive's decision vocabulary in fixed indicator-row order (the LOOP
// decision lights). Matches types.Decision: THINK BRANCH MERGE BACKTRACK ACT STOP DELIVER.
var loopDecisions = []string{"THINK", "BRANCH", "MERGE", "BACKTRACK", "ACT", "STOP", "DELIVER"}

// loopSeamCrossing reports whether a decision crossed a seam (ACT imports reality, DELIVER speaks) —
// those light bright (Ok); the structural/think moves light in the accent tone.
func loopSeamCrossing(d string) bool { return d == "ACT" || d == "DELIVER" }

// LoopView is the LOOP monitor's data contract — the minimal set the panel reads, so the renderer
// stays pure and the bridge maps the live engine onto it (UserWaiting from graph.UserWaiting, the
// decision from controller.LastMeta, n/U from the regulator, budget from the scheduler).
type LoopView struct {
	Arousal   string // AWAKE | DROWSY | ASLEEP (continuous); "" in reactive
	State     string // lifecycle state (ACTIVE | IDLE | ...)
	StateAge  int    // ticks in the current state (0 ⇒ unknown/omit)
	Tick      int
	TickSecs  float64 // wall seconds per tick (0 ⇒ unknown/omit)
	Substrate string  // running substrate class:model (e.g. "cc:sonnet")

	Goal           string
	UserWaiting    bool
	UserWaitingAge int

	Decision       string // the decision that fired this tick (lit in the indicator row)
	DecisionTick   int
	DecisionReason string

	N       float64 // branching ratio (stability)
	U       float64 // utilisation (load)
	LlmUsed int     // model calls granted this tick
	LlmCap  int     // per-tick budget
	Lull    int     // consecutive idle ticks
	LullCap int     // the drowse/sleep threshold (for context)
}

// RenderLoopMonitor renders the LOOP monitor body (≤6 lines) on the primitives. Pure — given the
// same LoopView it produces the same string (modulo ANSI). The box title/border is added by the
// panel frame; this returns the rows.
func RenderLoopMonitor(v LoopView) string {
	var lines []string

	// row 1 — state: arousal · STATE Nt · tick T (X.Xs/tick, substrate)
	var b strings.Builder
	b.WriteString(label("state"))
	if v.Arousal != "" {
		b.WriteString(txt(v.Arousal, colOk).render() + txt(" · ", colFaint).render())
	}
	b.WriteString(txt(v.State, colSubtext).render())
	if v.StateAge > 0 {
		b.WriteString(" " + txt(ageLabel(v.StateAge), colFaint).render())
	}
	b.WriteString(txt(" · tick ", colFaint).render() + txt(fmt.Sprintf("%d", v.Tick), colAccent).render())
	if meta := loopTickMeta(v); meta != "" {
		b.WriteString(txt(" ("+meta+")", colFaint).render())
	}
	lines = append(lines, b.String())

	// row 2 — goal "..."                         USER WAITING Nt
	goal := label("goal") + txt(quote(v.Goal), colText).render()
	if v.UserWaiting {
		goal += txt("   USER WAITING ", colWarn).render() + txt(ageLabel(v.UserWaitingAge), colWarn).render()
	}
	lines = append(lines, goal)

	// row 3 — decision indicator lights (fixed names, the fired one lit)
	tone := colAccent
	if loopSeamCrossing(v.Decision) {
		tone = colOk
	}
	lines = append(lines, label("decision")+IndicatorRow(loopDecisions, v.Decision, tone))

	// row 4 — last  DECISION · tick N   (sentence on its own row)
	if v.Decision != "" {
		hdr, sentence := lastBlock(v.Decision, v.DecisionTick, v.DecisionReason)
		lines = append(lines, label("last")+txt(hdr, colSubtext).render())
		if sentence != "" {
			lines = append(lines, label("reason")+txt(quote(sentence), colMuted).render())
		}
	}

	// row 5 — vitals: stability WORD (n ..) · load WORD (U ..) · llm u/c · lull l
	vit := label("vitals") +
		txt("stability ", colFaint).render() + txt(stabilityWord(v.N), stabilityTone(v.N)).render() +
		txt(fmt.Sprintf(" (n %.3f)", v.N), colAccent).render() +
		txt(" · load ", colFaint).render() + txt(loadWord(v.U), loadTone(v.U)).render() +
		txt(fmt.Sprintf(" (U %.3f)", v.U), colAccent).render()
	if v.LlmCap > 0 {
		vit += txt(" · llm ", colFaint).render() + txt(fmt.Sprintf("%d/%d", v.LlmUsed, v.LlmCap), colAccent).render()
	}
	vit += txt(" · lull ", colFaint).render() + txt(fmt.Sprintf("%d", v.Lull), colAccent).render()
	lines = append(lines, vit)

	return strings.Join(lines, "\n")
}

// LoopViewFromSnapshot maps a live end-of-tick SnapshotData onto the LOOP monitor's view contract —
// the bridge between the running engine and the renderer (the live data path). Strip histories and
// the per-tick wall clock are not in the snapshot; the pull-up frame supplies those as it accrues
// them. This covers the scalar spine: state, the decision + reason, the user-waiting marker, and the
// regulator vitals.
func LoopViewFromSnapshot(d SnapshotData) LoopView {
	v := LoopView{
		State:       d.LifecycleState,
		Tick:        d.Tick,
		Substrate:   d.Substrate,
		UserWaiting: d.UserWaiting,
		N:           d.N,
		U:           d.U,
	}
	if d.Mode == "continuous" {
		v.Arousal = d.Arousal
	}
	if d.ActiveBranch != nil {
		// the goal is the active branch's first thought when present.
		if len(d.ActiveContext) > 0 {
			v.Goal = d.ActiveContext[0].Text
		}
	}
	if d.LastMeta != nil {
		v.Decision = d.LastMeta.Decision
		v.DecisionTick = d.Tick
		v.DecisionReason = d.LastMeta.Reason
	}
	return v
}

// loopTickMeta renders the parenthetical tick rate + substrate ("3.1s/tick, cc:sonnet"), omitting
// each part that is unknown.
func loopTickMeta(v LoopView) string {
	var parts []string
	if v.TickSecs > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs/tick", v.TickSecs))
	}
	if v.Substrate != "" {
		parts = append(parts, v.Substrate)
	}
	return strings.Join(parts, ", ")
}

// label renders an aligned lowercase row label (the locked grammar: lowercase full word, padded to
// the widest label "specialists" + a gutter so no label ever runs into its value).
func label(name string) string {
	return txt(fmt.Sprintf("%-12s", name), colMuted).render()
}

// quote wraps text in double quotes, the convention for a verbatim string/sentence on a row.
func quote(s string) string {
	if s == "" {
		return ""
	}
	return "\"" + s + "\""
}
