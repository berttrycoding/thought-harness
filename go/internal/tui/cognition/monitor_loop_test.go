package cognition

// monitor_loop_test.go — the LOOP monitor renders the locked layout on the primitives: the state
// line, the user-waiting marker, the decision indicator lights, the two-row last block, and the
// translated vitals.

import (
	"strings"
	"testing"
)

func baseLoopView() LoopView {
	return LoopView{
		Arousal: "AWAKE", State: "ACTIVE", StateAge: 12, Tick: 184, TickSecs: 3.1, Substrate: "cc:sonnet",
		Goal: "is this refactor safe to ship?", UserWaiting: true, UserWaitingAge: 6,
		Decision: "ACT", DecisionTick: 184, DecisionReason: "question demands ground truth",
		N: 0.073, U: 0.751, LlmUsed: 3, LlmCap: 5, Lull: 0, LullCap: 12,
	}
}

func TestRenderLoopMonitorLayout(t *testing.T) {
	out := stripANSI(RenderLoopMonitor(baseLoopView()))
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("LOOP monitor should be 6 rows, got %d:\n%s", len(lines), out)
	}
	want := []struct {
		row  int
		subs []string
	}{
		{0, []string{"state", "AWAKE", "ACTIVE", "12t", "tick 184", "3.1s/tick", "cc:sonnet"}},
		{1, []string{"goal", "is this refactor safe to ship?", "USER WAITING", "6t"}},
		{2, []string{"decision", "THINK", "BRANCH", "MERGE", "BACKTRACK", "ACT", "STOP", "DELIVER"}},
		{3, []string{"last", "ACT · tick 184"}},
		{4, []string{"reason", "question demands ground truth"}},
		{5, []string{"vitals", "stability", "STABLE", "n 0.073", "load", "OK", "U 0.751", "llm 3/5", "lull 0"}},
	}
	for _, w := range want {
		for _, s := range w.subs {
			if !strings.Contains(lines[w.row], s) {
				t.Errorf("row %d missing %q:\n%q", w.row, s, lines[w.row])
			}
		}
	}
}

func TestRenderLoopMonitorNoUserWaiting(t *testing.T) {
	v := baseLoopView()
	v.UserWaiting = false
	out := stripANSI(RenderLoopMonitor(v))
	if strings.Contains(out, "USER WAITING") {
		t.Errorf("USER WAITING must not show when no user waits:\n%s", out)
	}
}

func TestRenderLoopMonitorTranslatesVitals(t *testing.T) {
	v := baseLoopView()
	v.N = 1.0 // runaway cliff
	v.U = 1.0 // saturated
	out := stripANSI(RenderLoopMonitor(v))
	if !strings.Contains(out, "RUNAWAY") {
		t.Errorf("n=1.0 must read RUNAWAY:\n%s", out)
	}
	if !strings.Contains(out, "SATURATED") {
		t.Errorf("U=1.0 must read SATURATED:\n%s", out)
	}
}

// the decision indicator is fixed-position: the lit one is present and all seven names render in order.
func TestRenderLoopMonitorDecisionIsFixedRow(t *testing.T) {
	v := baseLoopView()
	v.Decision = "DELIVER"
	out := stripANSI(RenderLoopMonitor(v))
	decRow := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "decision") {
			decRow = l
		}
	}
	last := -1
	for _, name := range loopDecisions {
		i := strings.Index(decRow, name)
		if i < 0 {
			t.Fatalf("decision row missing %q: %q", name, decRow)
		}
		if i < last {
			t.Fatalf("decision names out of fixed order at %q", name)
		}
		last = i
	}
}

// stripANSI removes lipgloss color escapes so a test can assert on the visible text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if c == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// LoopViewFromSnapshot maps live snapshot data onto the LOOP view (the live data path).
func TestLoopViewFromSnapshot(t *testing.T) {
	ab := 4
	d := SnapshotData{
		Tick: 184, Mode: "continuous", LifecycleState: "ACTIVE", Arousal: "AWAKE",
		UserWaiting: true, Substrate: "cc:sonnet", N: 0.073, U: 0.751,
		ActiveBranch:  &ab,
		ActiveContext: []ThoughtVM{{ID: 1, Text: "is this refactor safe to ship?"}},
		LastMeta:      &ControllerMetaVM{Decision: "ACT", Reason: "question demands ground truth"},
	}
	v := LoopViewFromSnapshot(d)
	if v.State != "ACTIVE" || v.Tick != 184 || v.Arousal != "AWAKE" || v.Substrate != "cc:sonnet" {
		t.Errorf("scalar spine wrong: %+v", v)
	}
	if !v.UserWaiting {
		t.Error("UserWaiting must carry through")
	}
	if v.Decision != "ACT" || v.DecisionReason != "question demands ground truth" || v.DecisionTick != 184 {
		t.Errorf("decision mapping wrong: %+v", v)
	}
	if v.Goal != "is this refactor safe to ship?" {
		t.Errorf("goal = %q", v.Goal)
	}
	if v.N != 0.073 || v.U != 0.751 {
		t.Errorf("vitals wrong: n=%v U=%v", v.N, v.U)
	}
	// reactive mode carries no arousal word.
	d.Mode = "reactive"
	if LoopViewFromSnapshot(d).Arousal != "" {
		t.Error("reactive mode should carry no arousal")
	}
	// the mapped view renders without panic and shows the live state.
	out := stripANSI(RenderLoopMonitor(LoopViewFromSnapshot(d)))
	if !strings.Contains(out, "ACTIVE") || !strings.Contains(out, "ACT") {
		t.Errorf("rendered live view missing state/decision:\n%s", out)
	}
}
