package cognition

// monitor_organs.go — the remaining organ monitors (W2): VITALS, CONTROLLER, VALUE,
// REGULATOR·SCHEDULER. All pure content renderers on the locked primitives.

import (
	"fmt"
	"strings"
)

// -- VITALS -----------------------------------------------------------------

// VitalsView is the unified body-signal monitor's data contract. Names are the architecture's own
// terms (NOT biology): cadence/reserve/excitation/load/grounding/pressure/faults/input.
type VitalsView struct {
	Horizon int

	Condition  string // NOMINAL | ENGAGED | LOADED | DEGRADED | CONSOLIDATING
	Arousal    string // AWAKE | DROWSY | ASLEEP
	AwakeAge   int    // ticks awake
	Lull       int
	TickPerMin float64
	TickSecs   float64

	Reserve int // 0-100 budget headroom

	N float64 // excitation
	U float64 // load

	GroundingInWindow int  // reality checks in the window
	UserWaiting       bool // pressure
	WaitingAge        int
	Ambiguity         float64
	Fallbacks         int    // faults
	Input             []bool // salient arrivals per tick
}

// RenderVitalsMonitor renders the VITALS body on the primitives. Pure.
func RenderVitalsMonitor(v VitalsView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string

	cond := label("condition") + txt(v.Condition, colOk).render()
	if v.Arousal != "" {
		cond += txt(fmt.Sprintf(" (%s %s · lull %d)", strings.ToLower(v.Arousal), ageLabel(v.AwakeAge), v.Lull), colFaint).render()
	}
	lines = append(lines, cond)

	cad := label("cadence") + txt(fmt.Sprintf("%.0f", v.TickPerMin), colAccent).render() + txt(" ticks/min", colFaint).render()
	if v.TickSecs > 0 {
		cad += txt(fmt.Sprintf(" (%.1fs/tick)", v.TickSecs), colFaint).render()
	}
	lines = append(lines, cad)

	lines = append(lines, label("reserve")+txt(fmt.Sprintf("%d/100", v.Reserve), colAccent).render()+
		txt(" budget headroom", colFaint).render())

	lines = append(lines, label("excitation")+
		txt(stabilityWord(v.N), stabilityTone(v.N)).render()+txt(fmt.Sprintf(" (n %.3f of 1.0)", v.N), colAccent).render()+
		txt(" · load ", colFaint).render()+txt(loadWord(v.U), loadTone(v.U)).render()+txt(fmt.Sprintf(" (U %.3f)", v.U), colAccent).render())

	gword := "GOOD"
	gtone := colOk
	if v.GroundingInWindow == 0 {
		gword, gtone = "DRY", colWarn // a closed loop with no reality checks runs on priors
	}
	lines = append(lines, label("grounding")+txt(gword, gtone).render()+
		txt(fmt.Sprintf(" (%d reality checks in %dt)", v.GroundingInWindow, w), colFaint).render())

	pword := "STEADY"
	ptone := colOk
	if v.UserWaiting || v.Ambiguity >= 0.5 {
		pword, ptone = "ELEVATED", colWarn
	}
	pdetail := fmt.Sprintf("ambiguity %.2f", v.Ambiguity)
	if v.UserWaiting {
		pdetail = "user waiting " + ageLabel(v.WaitingAge) + " · " + pdetail
	}
	lines = append(lines, label("pressure")+txt(pword, ptone).render()+txt(" ("+pdetail+")", colFaint).render())

	fword := "NONE"
	ftone := colOk
	if v.Fallbacks > 0 {
		fword, ftone = "PRESENT", colErr
	}
	lines = append(lines, label("faults")+txt(fword, ftone).render()+
		txt(fmt.Sprintf(" (%d substrate fallbacks in %dt)", v.Fallbacks, w), colFaint).render())

	salient := 0
	for _, s := range v.Input {
		if s {
			salient++
		}
	}
	lines = append(lines, label("input")+Strip(v.Input, w, colAccent)+
		txt(fmt.Sprintf("   %d salient in %dt", salient, w), colFaint).render())

	return strings.Join(lines, "\n")
}

// -- CONTROLLER -------------------------------------------------------------

// ControllerView is the CONTROLLER monitor's data contract (the judgment machinery behind the LOOP
// decision lights).
type ControllerView struct {
	Horizon int

	Mode        string // control | hybrid | llm
	GoalMet     bool
	LineSpent   bool
	NeedsTruth  bool
	Ambiguity   float64
	Escalate    []bool // the model was consulted this tick (Pattern C)
	LastOutcome string // KEPT OWN JUDGMENT | MODEL ADJUSTED
	LastTick    int
	LastReason  string
}

// yesNo renders a live bracketed flip-state ([YES]/[NO]) with a semantic tone.
func yesNo(b bool) string {
	if b {
		return txt("[YES]", colOk).render()
	}
	return txt("[NO]", colFaint).render()
}

// RenderControllerMonitor renders the CONTROLLER body on the primitives. Pure.
func RenderControllerMonitor(v ControllerView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string

	modeNote := "deterministic floor · no model escalation"
	if v.Mode == "hybrid" {
		modeNote = "deterministic floor + model escalation on fuzzy cases"
	} else if v.Mode == "llm" {
		modeNote = "model consulted every decision"
	}
	lines = append(lines, label("mode")+txt(v.Mode, colSubtext).render()+txt(" ("+modeNote+")", colFaint).render())

	lines = append(lines, label("judgment")+
		txt("goal met ", colFaint).render()+yesNo(v.GoalMet)+
		txt(" · line spent ", colFaint).render()+yesNo(v.LineSpent)+
		txt(" · needs ground truth ", colFaint).render()+yesNo(v.NeedsTruth))

	lines = append(lines, label("ambiguity")+txt(fmt.Sprintf("%.2f", v.Ambiguity), colAccent).render()+
		txt("   (0 clear-cut · 1 on-the-line; high may earn a model look)", colFaint).render())

	esc := 0
	for _, e := range v.Escalate {
		if e {
			esc++
		}
	}
	lines = append(lines, label("escalate")+Strip(v.Escalate, w, colAccent)+
		txt(fmt.Sprintf("   %d in %dt", esc, w), colFaint).render())

	if v.LastOutcome != "" {
		hdr, sentence := lastBlock(v.LastOutcome, v.LastTick, v.LastReason)
		lines = append(lines, label("last")+txt(hdr, colSubtext).render())
		if sentence != "" {
			lines = append(lines, label("reason")+txt(quote(sentence), colMuted).render())
		}
	}
	return strings.Join(lines, "\n")
}

// -- VALUE ------------------------------------------------------------------

// RankRow is one branch in the VALUE ranking (id + its priority value).
type RankRow struct {
	ID    int
	Value float64
}

// ValueView is the VALUE monitor's data contract — the priority/quality split + the live ranking.
type ValueView struct {
	Horizon int

	ActiveID int
	Priority float64 // Branch.Value (includes the pending-user term)
	Quality  float64 // Branch.Epistemic (content quality, no urgency)

	WhyText string  // the dominant reason (fixed deterministic vocabulary)
	WhyTerm float64 // its contribution

	Ranking []RankRow // top branches by priority
	Reward  []bool    // a grounded reward event this tick (sparse by design)

	LastReward float64
	LastTick   int
	LastReason string
}

// RenderValueMonitor renders the VALUE body on the primitives. Pure.
func RenderValueMonitor(v ValueView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string

	lines = append(lines, label("active")+
		txt(fmt.Sprintf("b%d", v.ActiveID), colAccent).render()+
		txt(" · priority ", colFaint).render()+txt(fmt.Sprintf("%.2f", v.Priority), colAccent).render()+
		txt(" · quality ", colFaint).render()+txt(fmt.Sprintf("%.2f", v.Quality), colAccent).render())

	if v.WhyText != "" {
		why := label("why") + txt(quote(v.WhyText), colSubtext).render()
		if v.WhyTerm != 0 {
			why += txt(fmt.Sprintf("  (%+.2f, the largest term)", v.WhyTerm), colFaint).render()
		}
		lines = append(lines, why)
	}

	if len(v.Ranking) > 0 {
		parts := make([]string, 0, len(v.Ranking))
		for _, r := range v.Ranking {
			parts = append(parts, fmt.Sprintf("b%d %.2f", r.ID, r.Value))
		}
		lines = append(lines, label("ranking")+txt(strings.Join(parts, " · "), colAccent).render())
	}

	rew := 0
	for _, r := range v.Reward {
		if r {
			rew++
		}
	}
	lines = append(lines, label("reward")+Strip(v.Reward, w, colOk)+
		txt(fmt.Sprintf("   %d in %dt", rew, w), colFaint).render())

	if v.LastReason != "" {
		hdr, sentence := lastBlock(fmt.Sprintf("GROUNDED REWARD %+.1f", v.LastReward), v.LastTick, v.LastReason)
		lines = append(lines, label("last")+txt(hdr, colSubtext).render())
		lines = append(lines, label("reason")+txt(quote(sentence), colMuted).render())
	}
	return strings.Join(lines, "\n")
}

// -- REGULATOR · SCHEDULER --------------------------------------------------

// RegulatorView is the REGULATOR·SCHEDULER monitor's data contract.
type RegulatorView struct {
	Horizon int

	N        float64 // excitation/stability
	U        float64 // load
	Mu       float64 // baseline immigrant rate
	Theta    float64 // admission threshold
	LlmUsed  int
	LlmCap   int
	Deferred []bool // a model call was deferred this tick (budget pressure)
}

// RenderRegulatorMonitor renders the REGULATOR·SCHEDULER body on the primitives. Pure.
func RenderRegulatorMonitor(v RegulatorView) string {
	w := v.Horizon
	if w <= 0 {
		w = monitorHorizon
	}
	var lines []string

	lines = append(lines, label("stability")+txt(stabilityWord(v.N), stabilityTone(v.N)).render()+
		txt(fmt.Sprintf(" (n %.3f · runaway at 1.0)", v.N), colAccent).render())
	lines = append(lines, label("load")+txt(loadWord(v.U), loadTone(v.U)).render()+
		txt(fmt.Sprintf(" (U %.3f · schedulable to 1.0) · baseline %.2f/tick", v.U, v.Mu), colAccent).render())
	lines = append(lines, label("threshold")+txt(fmt.Sprintf("θ %.2f", v.Theta), colAccent).render()+
		txt(" (rises under load — fewer specialists fire)", colFaint).render())
	lines = append(lines, label("budget")+txt(fmt.Sprintf("%d/%d", v.LlmUsed, v.LlmCap), colAccent).render()+
		txt(" model calls this tick", colFaint).render())

	def := 0
	for _, d := range v.Deferred {
		if d {
			def++
		}
	}
	lines = append(lines, label("deferred")+Strip(v.Deferred, w, colWarn)+
		txt(fmt.Sprintf("   %d in %dt", def, w), colFaint).render())

	return strings.Join(lines, "\n")
}
