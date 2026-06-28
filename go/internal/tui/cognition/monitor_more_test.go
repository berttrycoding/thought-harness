package cognition

import (
	"strings"
	"testing"
)

func TestRenderMemoryMonitor(t *testing.T) {
	out := stripANSI(RenderMemoryMonitor(MemoryView{
		Horizon: 10, Episodes: 12, EpisodesRecalled: 3, Beliefs: 7, BeliefsDistilled: 2, Consulted: 4,
		Prefs: 3, PrefsApplied: 1, Recall: []bool{true, false, true},
		LastEvent: "DISTILLED", LastDetail: "belief", LastTick: 171, LastReason: "from 2 grounded episodes",
	}))
	for _, s := range []string{"episodic", "12", "recalled 3", "semantic", "7", "2 distilled", "consulted 4",
		"person", "applied 1", "recall", "DISTILLED belief · tick 171"} {
		if !strings.Contains(out, s) {
			t.Errorf("memory missing %q:\n%s", s, out)
		}
	}
}

func TestRenderKnowledgeMonitorPrestockInvariant(t *testing.T) {
	out := stripANSI(RenderKnowledgeMonitor(KnowledgeView{
		Horizon: 10, Entries: 41, FromReality: 38, Distilled: 3, PreStocked: 0,
		Recalled: 6, FedToConcretize: 2, Recall: []bool{true},
	}))
	for _, s := range []string{"entries", "41", "born", "38 from reality", "3 distilled", "0 pre-stocked",
		"used", "6 recalled", "2 fed into concretize"} {
		if !strings.Contains(out, s) {
			t.Errorf("knowledge missing %q:\n%s", s, out)
		}
	}
}

func TestRenderOperatorsMonitorOfferedInvariant(t *testing.T) {
	out := stripANSI(RenderOperatorsMonitor(OperatorsView{
		Horizon: 10, Seed: 34, Minted: 2, Families: 6, Offered: 36, CatalogTotal: 36,
		FiredDistinct: 8, TopMovers: "GROUND 5 · REFRAME 3", Skills: 6, SkillsMinted: 1, Matched: 2, Workflows: 3,
		Apply: []bool{true}, LastEvent: "MINTED", LastDetail: "operator \"compare-against-baseline\"", LastTick: 142,
	}))
	for _, s := range []string{"catalog", "34 seed + 2 minted", "6 families", "offered", "36 of 36",
		"fired", "8 distinct", "GROUND 5", "skills", "6 + 1 minted", "matched 2", "3 workflows", "apply", "MINTED"} {
		if !strings.Contains(out, s) {
			t.Errorf("operators missing %q:\n%s", s, out)
		}
	}
}

func TestRenderSessionsMonitor(t *testing.T) {
	out := stripANSI(RenderSessionsMonitor(SessionsView{
		Horizon: 10, Episode: "ep:7", Spawned: 3, Running: 1, Depth: 2, MaxDepth: 3,
		Agents: []SubAgentRow{
			{Name: "verifier", State: "running", Op: "GROUND", Tools: "read,run", Calls: 2},
			{Name: "researcher", State: "done", Tools: "search"},
		},
		Spawn: []bool{true}, LastEvent: "SPAWNED", LastDetail: "\"verifier\"", LastTick: 180,
	}))
	for _, s := range []string{"tree", "episode ep:7", "3 spawned", "1 running", "depth 2 of 3",
		"verifier", "running", "op GROUND", "tools read,run", "researcher", "spawn", "SPAWNED"} {
		if !strings.Contains(out, s) {
			t.Errorf("sessions missing %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "▸") {
		t.Error("a running sub-agent should be marked ▸")
	}
}

func TestRenderTriggersMonitor(t *testing.T) {
	out := stripANSI(RenderTriggersMonitor(TriggersView{
		Horizon: 10, Sensors: 2, Drives: 3, CooldownLeft: 12,
		Due: "action feedback at tick 186 (in 2t) · consolidation on next idle",
		Armed: []ArmedRow{
			{Kind: "sensor", Name: "test-watcher", Note: "suite still green", Age: 8},
			{Kind: "drive", Name: "maintenance", Note: "resumes high-value lines"},
		},
		Fired: []bool{true}, LastEvent: "SENSOR", LastDetail: "\"test-watcher\"", LastTick: 176,
	}))
	for _, s := range []string{"armed", "2 sensors · 3 drives", "cooldown 12t", "due", "action feedback at tick 186",
		"sensor", "test-watcher", "drive", "maintenance", "fired", "SENSOR"} {
		if !strings.Contains(out, s) {
			t.Errorf("triggers missing %q:\n%s", s, out)
		}
	}
}

func TestRenderThroughputMonitor(t *testing.T) {
	out := stripANSI(RenderThroughputMonitor(ThroughputView{
		TokensInPerMin: 14200, TokensOutPerMin: 3800, ThinkingPerMin: 2100, AnswerPerMin: 1700,
		Roles: "generate 41% · transform 22%", IntakeReality: 1300, IntakeUser: 100, CachePct: 62,
		PeakRole: "synthesize_program", PeakTokens: 9400, PeakSecs: 21, PeakTick: 178,
	}))
	for _, s := range []string{"tokens", "in 14.2k/min", "out 3.8k/min", "thinking", "2.1k/min reasoning",
		"answer", "roles", "generate 41%", "intake", "reality 1.3k", "cache", "62%", "peak", "synthesize_program"} {
		if !strings.Contains(out, s) {
			t.Errorf("throughput missing %q:\n%s", s, out)
		}
	}
}

func TestRenderSelfMonitor(t *testing.T) {
	out := stripANSI(RenderSelfMonitor(SelfView{
		Build: "86e57d8", StateName: "campaign-3", Deltas: 6,
		DeltaDetail: "+2 specialists · +1 skill · +3 beliefs since baseline", Reverts: 1, Mode: "SAFE",
		StartupChecks: 8, StartupTotal: 8, StabilityPass: 8, StabilityTot: 8, StabilityAge: 412,
		LastEvent: "REVERTED", LastDetail: "batch-7", LastTick: 152, LastReason: "precision@1 regressed",
	}))
	for _, s := range []string{"version", "build 86e57d8", "campaign-3", "+6 deltas", "deltas",
		"+2 specialists", "reverts", "1 this session", "mode", "SAFE", "structure EXPERIMENTAL",
		"checks", "startup 8/8", "stability 8/8 PASS", "REVERTED batch-7 · tick 152"} {
		if !strings.Contains(out, s) {
			t.Errorf("self missing %q:\n%s", s, out)
		}
	}
}
