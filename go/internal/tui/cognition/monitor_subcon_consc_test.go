package cognition

import (
	"strings"
	"testing"
)

func TestRenderSubconsciousMonitor(t *testing.T) {
	v := SubconsciousView{
		Workflow: "verify-refactor", WorkflowMinTk: 142, Phase: "2/3", Operator: "GROUND",
		Theta: 0.80,
		Agents: []AgentRow{
			{Name: "compute", Relevance: 0.95, Note: "12 * 7 = 84", Age: 0},
			{Name: "social", Relevance: 0.95, Note: "greeting on the user line", Age: 0},
			{Name: "recall", Relevance: 0.41, Note: "below θ(0.80)", Age: 3},
			{Name: "learned:brainstorm", Relevance: 0.38, Note: "below θ", Age: 9, MintedAt: 41},
		},
	}
	out := stripANSI(RenderSubconsciousMonitor(v))
	for _, s := range []string{"running", "verify-refactor", "minted t142", "phase 2/3", "op GROUND",
		"compute", "12 * 7 = 84", "social", "recall", "learned:brainstorm", "minted t41", "9t"} {
		if !strings.Contains(out, s) {
			t.Errorf("subconscious monitor missing %q:\n%s", s, out)
		}
	}
	// a fired agent (age 0) leads with the ▸ marker; the workflow line has no marker.
	if !strings.Contains(out, "▸") {
		t.Error("a fired agent should be marked with ▸")
	}
}

func TestRenderSubconsciousMonitorPlainDispatch(t *testing.T) {
	out := stripANSI(RenderSubconsciousMonitor(SubconsciousView{}))
	if !strings.Contains(out, "plain dispatch") {
		t.Errorf("no workflow should read 'plain dispatch':\n%s", out)
	}
	if !strings.Contains(out, "no specialist fired") {
		t.Errorf("no agents should read the quiet marker:\n%s", out)
	}
}

func TestRenderConsciousMonitor(t *testing.T) {
	v := ConsciousView{
		ActiveID: 4, ActiveText: "is this refactor safe to ship?", Thoughts: 9, ActiveValue: 0.71,
		LiveBranches: 6, DeadBranches: 2,
		BestID: 2, BestText: "where does the value signal pull hardest", BestValue: 0.69,
		Op: "COMPRESS", OpTick: 184, OpDetail: "gist[9]: is this refactor safe to ship",
	}
	out := stripANSI(RenderConsciousMonitor(v))
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("conscious monitor should be 6 rows, got %d:\n%s", len(lines), out)
	}
	for _, s := range []string{"active", "b4", "9 thoughts", "V 0.71", "branches", "6 live", "2 dead",
		"best", "b2", "0.69", "operation", "COMPRESS", "BRANCH", "MERGE", "EXPAND", "last", "tick 184"} {
		if !strings.Contains(out, s) {
			t.Errorf("conscious monitor missing %q:\n%s", s, out)
		}
	}
}

func TestRenderConsciousMonitorNoBest(t *testing.T) {
	v := ConsciousView{ActiveID: 0, ActiveText: "solo line", Thoughts: 3, BestID: -1, Op: "EXPAND", OpTick: 5}
	out := stripANSI(RenderConsciousMonitor(v))
	if !strings.Contains(out, "no other live line") {
		t.Errorf("BestID<0 should read the no-other-line marker:\n%s", out)
	}
}
