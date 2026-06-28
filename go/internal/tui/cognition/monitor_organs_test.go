package cognition

import (
	"strings"
	"testing"
)

func TestRenderVitalsMonitor(t *testing.T) {
	v := VitalsView{
		Horizon: 10, Condition: "ENGAGED", Arousal: "AWAKE", AwakeAge: 412, Lull: 0,
		TickPerMin: 19, TickSecs: 3.1, Reserve: 72, N: 0.073, U: 0.751,
		GroundingInWindow: 4, UserWaiting: true, WaitingAge: 6, Ambiguity: 0.42, Fallbacks: 0,
		Input: []bool{true, false, false, false, false, false, false, false, true, false},
	}
	out := stripANSI(RenderVitalsMonitor(v))
	for _, s := range []string{"condition", "ENGAGED", "cadence", "19 ticks/min", "reserve", "72/100",
		"excitation", "STABLE", "load", "OK", "grounding", "GOOD", "pressure", "ELEVATED",
		"user waiting 6t", "faults", "NONE", "input", "2 salient in 10t"} {
		if !strings.Contains(out, s) {
			t.Errorf("vitals missing %q:\n%s", s, out)
		}
	}
}

func TestRenderControllerMonitor(t *testing.T) {
	v := ControllerView{
		Horizon: 10, Mode: "control", GoalMet: false, LineSpent: false, NeedsTruth: true,
		Ambiguity: 0.42, Escalate: make([]bool, 10),
		LastOutcome: "KEPT OWN JUDGMENT", LastTick: 181, LastReason: "a structural move",
	}
	out := stripANSI(RenderControllerMonitor(v))
	for _, s := range []string{"mode", "control", "judgment", "goal met [NO]", "needs ground truth [YES]",
		"ambiguity", "0.42", "escalate", "0 in 10t", "last", "KEPT OWN JUDGMENT · tick 181"} {
		if !strings.Contains(out, s) {
			t.Errorf("controller missing %q:\n%s", s, out)
		}
	}
}

func TestRenderValueMonitor(t *testing.T) {
	v := ValueView{
		Horizon: 10, ActiveID: 4, Priority: 1.00, Quality: 0.71,
		WhyText: "user is waiting on this line", WhyTerm: 0.50,
		Ranking:    []RankRow{{4, 1.00}, {2, 0.69}, {0, 0.31}},
		Reward:     []bool{true, false, false, false, false, false, false, false, false, false},
		LastReward: 1.0, LastTick: 172, LastReason: "ran the test suite — 12/12 pass",
	}
	out := stripANSI(RenderValueMonitor(v))
	for _, s := range []string{"active", "priority 1.00", "quality 0.71", "why", "user is waiting",
		"+0.50", "ranking", "b4 1.00", "b2 0.69", "reward", "GROUNDED REWARD +1.0 · tick 172"} {
		if !strings.Contains(out, s) {
			t.Errorf("value missing %q:\n%s", s, out)
		}
	}
}

func TestRenderRegulatorMonitor(t *testing.T) {
	v := RegulatorView{
		Horizon: 10, N: 0.073, U: 0.751, Mu: 0.28, Theta: 0.80, LlmUsed: 3, LlmCap: 5,
		Deferred: []bool{true, false, false, true, true, false, false, false, false, false},
	}
	out := stripANSI(RenderRegulatorMonitor(v))
	for _, s := range []string{"stability", "STABLE", "n 0.073", "load", "OK", "U 0.751",
		"baseline 0.28/tick", "threshold", "θ 0.80", "budget", "3/5", "deferred", "3 in 10t"} {
		if !strings.Contains(out, s) {
			t.Errorf("regulator missing %q:\n%s", s, out)
		}
	}
}
