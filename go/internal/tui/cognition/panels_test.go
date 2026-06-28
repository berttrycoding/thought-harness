package cognition

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// sampleVM builds a populated ViewModel exercising every panel's live + event-fed paths.
func sampleVM() ViewModel {
	ab := 0
	snap := SnapshotData{
		Tick:           7,
		Mode:           "reactive",
		LifecycleState: "ACTIVE",
		Arousal:        "AWAKE",
		LifecycleHistory: []string{
			"IDLE              opened by a user prompt",
			"ACTIVE            a very long reason that should be clipped to thirty chars exactly here",
		},
		ActiveContext: []ThoughtVM{
			{ID: 1, Text: "user asks to ship the deploy", Source: "USER_INPUT", SourceTag: "USE", Confidence: 0},
			{ID: 2, Text: strings.Repeat("x", 80), Source: "INJECTED", SourceTag: "INJ", Confidence: 0.81},
			{ID: 3, Text: "reality: ran the suite", Source: "OBSERVATION", SourceTag: "OBS", Confidence: 0.95},
		},
		Branches: []BranchVM{
			{ID: 0, Resolution: "EXPANDED", Value: 0.62, Status: "ACTIVE"},
			{ID: 1, Resolution: "COMPRESSED", Value: 0.31, Status: "STASHED"},
		},
		ActiveBranch:       &ab,
		LastMeta:           &ControllerMetaVM{Decision: "BRANCH", Reason: "a conflict was detected", Flagged: true},
		Workflow:           &WorkflowVM{Name: "deploy", PhaseIndex: 1, OpName: "verify", Recognized: true},
		ActionOutstanding:  2,
		ActionLatencyTicks: 3,
		ActedBranches:      []int{1, 0},
		LastBridge:         "scraped",
		LastFabricated:     true,
		Demoted:            []string{"simulation"},
		Values:             map[int]float64{0: 0.62, 1: 0.31},
		Theta:              0.55,
		LamBar:             0.40,
		LamHat:             0.30,
		N:                  0.25,
		Mu:                 0.10,
		U:                  0.42,
		RegHistory: []RegSnapshotVM{
			{Theta: 0.5, N: 0.2}, {Theta: 0.55, N: 0.25}, {Theta: 0.52, N: 0.22},
		},
		Stability: []StabilityCheckVM{
			{Name: "n<1 (subcritical)", Pass: true},
			{Name: "U<=1 (schedulable)", Pass: false},
			{Name: "mu>0 (awake baseline)", NA: true},
		},
		EpisodicCount: 2,
		SemanticCount: 1,
		PersonCount:   0,
		RetrieverMode: "hybrid",
	}
	evs := []events.Event{
		{Tick: 5, Kind: events.SubDispatch, Summary: "2 specialist(s) fired", Data: map[string]any{
			"theta": 0.55,
			"scan": []any{
				map[string]any{"domain": "safety", "effective": 0.72, "fired": true},
				map[string]any{"domain": "risk", "effective": 0.20, "fired": false},
			},
		}},
		{Tick: 6, Kind: events.Filter, Summary: "admit", Data: map[string]any{
			"verdict": "ADMIT", "confidence": 0.82, "text": strings.Repeat("a", 60)}},
		{Tick: 6, Kind: events.Gate, Summary: "winner risk", Data: map[string]any{
			"winner": "risk", "conflict": true}},
		{Tick: 6, Kind: events.Transform, Summary: "voiced", Data: map[string]any{
			"raw": strings.Repeat("r", 60), "voiced": strings.Repeat("v", 60)}},
		{Tick: 6, Kind: events.Assemble, Summary: "assembled D:executive view (5 items)", Data: map[string]any{
			"template": "D:executive", "items": 5, "budget": 0, "view_chars": 240}},
		{Tick: 6, Kind: events.Intention, Summary: "act", Data: map[string]any{"text": strings.Repeat("i", 60)}},
		{Tick: 6, Kind: events.Observation, Summary: "reality: " + strings.Repeat("o", 60), Data: map[string]any{"ok": true}},
		{Tick: 7, Kind: events.Value, Summary: "v", Data: map[string]any{
			"reward": 1.0, "reason": strings.Repeat("z", 50), "appraiser": "heuristic",
			"signals": map[string]any{"recent_conf": 0.31, "goal_sim": 0.04, "grounded_reality": 0.3}}},
		// the generative + cross-cutting mechanism events (so the mechanism panels' populated paths are
		// width-tested, not just their empty states).
		{Tick: 7, Kind: events.SubSynthesize, Summary: "synth", Data: map[string]any{
			"shape": strings.Repeat("s", 50), "source": "skill:x", "rationale": strings.Repeat("y", 50)}},
		{Tick: 7, Kind: events.SubOperator, Summary: "op", Data: map[string]any{
			"name": "new-op", "family": "fam", "intent": strings.Repeat("o", 50)}},
		{Tick: 7, Kind: events.SubSubagent, Summary: "sa", Data: map[string]any{
			"role": "verifier", "domain": "build", "responsibility": strings.Repeat("r", 50),
			"tool_scope": []any{"run_tests"}}},
		{Tick: 7, Kind: events.SkillMatch, Summary: "sm", Data: map[string]any{"skill": "deploy", "shape": "seq(a)"}},
		{Tick: 7, Kind: events.MCP, Summary: "branch -> b1: " + strings.Repeat("m", 50), Data: map[string]any{"op": "branch"}},
		{Tick: 7, Kind: events.XRef, Summary: "b1 CONTRADICTS b0", Data: map[string]any{}},
		{Tick: 7, Kind: events.ActionTool, Summary: "run_tests (exit 0)", Data: map[string]any{"tool": "run_tests", "ok": true}},
		{Tick: 7, Kind: events.Schedule, Summary: "defer specialist.x: budget spent", Data: map[string]any{}},
		// escalation.floor_stands (Pattern-C, Rule 4) — the deterministic FLOOR stood at each site because
		// the model declined / was not consulted, so the scheduler/critic/seam panels surface the floor-stood
		// health line (this only happens in an llm/hybrid run). One per site (filter / critic).
		{Tick: 7, Kind: events.EscalationFloorStands, Summary: "filter.admit floor stands (ADMIT, model-declined, ambiguity=0.62)",
			Data: map[string]any{"site": "filter.admit", "decision": "ADMIT", "floor_decision": "ADMIT",
				"ambiguity": 0.62, "reason": "model-declined", "model_consulted": true}},
		{Tick: 7, Kind: events.EscalationFloorStands, Summary: "critic.decide floor stands (THINK, no-model, ambiguity=0.55)",
			Data: map[string]any{"site": "critic.decide", "decision": "THINK", "floor_decision": "THINK",
				"ambiguity": 0.55, "reason": "no-model", "model_consulted": false}},
		{Tick: 7, Kind: events.LLM, Summary: "call", Data: map[string]any{"role": "conscious.generate", "ms": 240}},
		{Tick: 7, Kind: events.LLMFallback, Summary: "[seam.transform] timeout -> heuristic", Data: map[string]any{}},
		{Tick: 7, Kind: events.Port, Summary: "drive: resume high-value b1", Data: map[string]any{"drive": "resume"}},
		// grounding.* — the reality ledger (a grounded claim, a refuted one with a long claim that must
		// wrap, and a continuous sensor percept) so renderGrounding's populated path is width-tested.
		{Tick: 7, Kind: events.Ground, Summary: "grounding: the build passes -> grounded", Data: map[string]any{
			"claim": "the build passes", "verdict": "grounded", "tier": "firsthand-observation",
			"status": "BELIEVE", "method": "observation", "bridge": "structured"}},
		{Tick: 7, Kind: events.Ground, Summary: "grounding: a refuted claim -> refuted", Data: map[string]any{
			"claim": strings.Repeat("q", 70), "verdict": "refuted", "tier": "deterministic",
			"status": "BELIEVE", "method": "compute", "bridge": "none"}},
		{Tick: 7, Kind: events.Percept, Summary: "sensor re-grounded 1 claim(s)", Data: map[string]any{
			"count": 1, "sensor": "test-watcher"}},
		// perception.* — the live senses from the cognitive power-cycle (clock + orient) so renderPerception's
		// populated path is width-tested, not just its empty placeholder.
		{Tick: 7, Kind: events.PerceptionClock, Summary: "read_clock [record]: 2026-06-20T18:30:00Z", Data: map[string]any{
			"value": "2026-06-20T18:30:00Z", "mode": "record", "tick": 7}},
		{Tick: 0, Kind: events.PerceptionOrient, Summary: "orient: prior focus=ship the deploy time=2026-06-20T18:30:00Z open_lines=2",
			Data: map[string]any{
				"tick": 0, "gist": strings.Repeat("focus on the deploy refactor safety ", 3), "clock": "2026-06-20T18:30:00Z",
				"self": "is this refactor safe to ship?", "open_lines": 2, "belief": true, "resume": true,
				"host_ok": true, "alloc_mb": 41, "sys_mb": 72, "goroutines": 18, "recent_events": 240}},
		// session.* — the runtime spawn tree (a 3-phase workflow with a parallel phase + merge) so
		// renderSession's populated path is width-tested.
		{Tick: 1, Kind: events.SessionSpawn, Summary: "session opened", Data: map[string]any{
			"goal": "Design a small API for a todo service", "phases": 3, "shape": "seq(decompose,generate,validate)"}},
		{Tick: 1, Kind: events.SessionDispatch, Summary: "dispatch phase 1: decompose", Data: map[string]any{
			"goal": "decompose", "depth": 1, "phase": 1, "parallel": false, "tokens": 48, "cap": 64}},
		{Tick: 1, Kind: events.SessionDispatch, Summary: "dispatch phase 2: generate ‖ rank", Data: map[string]any{
			"goal": strings.Repeat("g", 50), "depth": 1, "phase": 2, "parallel": true, "tokens": 96, "cap": 128}},
		{Tick: 1, Kind: events.SessionMerge, Summary: "merge 2 parallel results (reduce)", Data: map[string]any{
			"strategy": "reduce", "n": 2, "phase": 2}},
		{Tick: 4, Kind: events.SessionTerminate, Summary: "session terminated (GOAL_MET): 4 nodes", Data: map[string]any{
			"reason": "GOAL_MET", "nodes": 4, "depth": 1, "tokens": 208}},
	}
	return ViewModel{Snap: snap, Events: evs}
}

// TestAllPanelsRenderNoPanic renders every rail panel and asserts a non-empty body, no panic.
func TestAllPanelsRenderNoPanic(t *testing.T) {
	vm := sampleVM()
	for _, spec := range Rail() {
		p := RenderPanel(spec.ID(), vm)
		if strings.Contains(ansi.Strip(p.Body), "[render error]") {
			t.Fatalf("panel %q hit the recover path: %q", spec.ID(), p.Body)
		}
		if strings.TrimSpace(ansi.Strip(p.Body)) == "" {
			t.Fatalf("panel %q rendered empty", spec.ID())
		}
	}
}

// TestRailRegistry confirms every layout id resolves to a registry spec, the hybrid subsystems are
// split into a _metrics + _text panel, and Layout()'s ids all exist (no dangling reference).
func TestRailRegistry(t *testing.T) {
	for _, id := range []string{
		"conscious", "subconscious", "value", "durability", "seam",
		"action_text", "action_metrics", "critic_metrics", "critic_text",
		"lifecycle_metrics", "lifecycle_text", "memory_metrics", "memory_text", "trace",
	} {
		if _, ok := SpecByID(id); !ok {
			t.Fatalf("registry missing panel id %q", id)
		}
	}
	for _, lr := range Layout() {
		if len(lr.IDs) == 0 {
			t.Fatalf("layout row has no ids")
		}
		for _, id := range lr.IDs {
			if _, ok := SpecByID(id); !ok {
				t.Fatalf("layout references unknown panel id %q", id)
			}
		}
	}
}

// TestNoLineExceedsWidth is the core layout invariant after the width-aware rewrite: at a range of
// column widths, NO panel emits a body line wider than vm.Width. (A wider line would make lipgloss wrap
// it when boxed, which is exactly the bug that broke every column's alignment.) This also exercises the
// "no \n inside a styled run" rule — that bug produced over-wide lines via trailing-empty-line padding.
func TestNoLineExceedsWidth(t *testing.T) {
	for _, w := range []int{28, 40, 47, 64, 96} {
		vm := sampleVM()
		vm.Width = w
		for _, spec := range Rail() {
			body := ansi.Strip(RenderPanel(spec.ID(), vm).Body)
			for i, ln := range strings.Split(body, "\n") {
				if got := lipgloss.Width(ln); got > w {
					t.Fatalf("panel %q line %d width=%d > vm.Width=%d:\n%q", spec.ID(), i, got, w, ln)
				}
			}
		}
	}
}

// TestConsciousConfidenceRightAligned confirms the confidence forms a clean right-hand column: every
// thought line that carries a confidence ends with it, flush to the width.
func TestConsciousConfidenceRightAligned(t *testing.T) {
	const w = 50
	vm := sampleVM()
	vm.Width = w
	body := ansi.Strip(renderConscious(vm).Body)
	// thought #2 has confidence 0.81 — its line must END in "0.81" at the right edge.
	var found bool
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#2 ") {
			found = true
			if !strings.HasSuffix(ln, "0.81") {
				t.Fatalf("thought #2 confidence not right-aligned:\n%q", ln)
			}
		}
	}
	if !found {
		t.Fatalf("thought #2 line not found:\n%s", body)
	}
}

// TestSeamConflictMarker confirms the gate conflict fork marker still renders.
func TestSeamConflictMarker(t *testing.T) {
	vm := sampleVM()
	vm.Width = 64
	if !strings.Contains(ansi.Strip(renderSeam(vm).Body), "CONFLICT") {
		t.Fatalf("conflict fork marker missing:\n%s", ansi.Strip(renderSeam(vm).Body))
	}
}

// TestEscalationFloorStandsIndicator confirms the Pattern-C escalation/degradation indicator (M5)
// renders the floor-stood health from escalation.floor_stands: the scheduler panel shows the
// per-site tally, the critic-metrics panel shows the critic.decide floor-stood counter, and the seam
// panel shows the filter.admit floor-stood line. On a run with NO such events (the default control
// mode) none of the indicators appear.
func TestEscalationFloorStandsIndicator(t *testing.T) {
	vm := sampleVM()
	vm.Width = 72

	sched := ansi.Strip(renderScheduler(vm).Body)
	if !strings.Contains(sched, "Pattern-C") || !strings.Contains(sched, "floor stood") {
		t.Fatalf("scheduler panel missing the Pattern-C floor-stood health line:\n%s", sched)
	}
	if !strings.Contains(sched, "filter") || !strings.Contains(sched, "critic") {
		t.Fatalf("scheduler floor-stood health should tally per site (filter/critic):\n%s", sched)
	}
	// the filter site was a model-declined escalation → annotated "(1 model declined)".
	if !strings.Contains(sched, "model declined") {
		t.Fatalf("scheduler health should distinguish a model-declined escalation:\n%s", sched)
	}

	critic := ansi.Strip(renderCriticMetrics(vm).Body)
	if !strings.Contains(critic, "floor stood") || !strings.Contains(critic, "×1") {
		t.Fatalf("critic-metrics panel missing the critic.decide floor-stood counter:\n%s", critic)
	}

	seam := ansi.Strip(renderSeam(vm).Body)
	if !strings.Contains(seam, "floor stood") {
		t.Fatalf("seam panel missing the filter.admit floor-stood line:\n%s", seam)
	}

	// the default control mode emits no escalation.floor_stands → no indicator anywhere.
	empty := ViewModel{Width: 72}
	if s := ansi.Strip(renderScheduler(empty).Body); strings.Contains(s, "Pattern-C") {
		t.Fatalf("control mode (no floor_stands) must show no Pattern-C health line:\n%s", s)
	}
	if s := ansi.Strip(renderSeam(empty).Body); strings.Contains(s, "floor stood") {
		t.Fatalf("control mode (no floor_stands) must show no filter floor-stood line:\n%s", s)
	}
}

// TestNoTrailingEmptyLinePadding guards the "no \n inside a styled run" rule directly. That bug padded
// a run's trailing empty line out to the run's width, so the next appended content was shoved right.
// The watched-seam text panel was the worst offender (intention/reality shoved ~40 cols right). Its
// label lines start at column 0; any leading whitespace means the padding bug has returned.
func TestNoTrailingEmptyLinePadding(t *testing.T) {
	vm := sampleVM()
	vm.Width = 50
	body := ansi.Strip(renderActionText(vm).Body)
	for _, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(ln, " ")
		if strings.HasPrefix(trimmed, "intention") || strings.HasPrefix(trimmed, "reality") {
			if lead := len(ln) - len(trimmed); lead > 0 {
				t.Fatalf("over-indented action line (padding bug?): %q", ln)
			}
		}
	}
}

// TestRenderErrorIsolation confirms a panicking render is caught per-pane.
func TestRenderErrorIsolation(t *testing.T) {
	// an unknown id takes the default branch (no panic); a forced nil-deref is caught by recover.
	p := RenderPanel("conscious", ViewModel{Snap: SnapshotData{ActiveBranch: nil}})
	if strings.Contains(p.Body, "[render error]") {
		t.Fatalf("idle conscious should not error: %q", p.Body)
	}
}

// TestDashboardWidthAlignment confirms every boxed row renders to the same total width W.
func TestDashboardWidthAlignment(t *testing.T) {
	vm := sampleVM()
	const W = 60
	var body []Row
	for _, spec := range Rail() {
		p := RenderPanel(spec.ID(), vm)
		body = append(body, R(p))
	}
	out := Dashboard(W, []Panel{{Body: "header", Chrome: true}}, body, Panel{Body: "status", Chrome: true})
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != W {
			t.Fatalf("dashboard line %d width = %d, want %d:\n%q", i, w, W, line)
		}
	}
}

// TestRenderGroundingEmptyAndPopulated covers the grounding ledger's two states: an empty stream gives
// the honest "nothing grounded (heuristic acts are fabricated)" message; a populated stream shows the
// grounded/refuted tally, the epistemic mix, the ledger rows (✓/✗ + claim), and the sensor line.
func TestRenderGroundingEmptyAndPopulated(t *testing.T) {
	// empty
	empty := ansi.Strip(renderGrounding(ViewModel{Width: 64}).Body)
	if !strings.Contains(empty, "nothing grounded yet") {
		t.Fatalf("empty grounding panel should explain why it is empty:\n%q", empty)
	}
	// populated (sampleVM carries a grounded, a refuted, and a percept)
	vm := sampleVM()
	vm.Width = 64
	body := ansi.Strip(renderGrounding(vm).Body)
	for _, want := range []string{
		"grounded / refuted", "1 / 1", // one grounded, one refuted
		"EXPERIMENT LEDGER", "✓", "✗", "the build passes",
		"firsthand-observation", "BELIEVE", "SENSORS",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("populated grounding panel missing %q:\n%s", want, body)
		}
	}
}

// TestRenderSessionEmptyAndPopulated covers the runtime panel's two states: no workflow → the honest
// "simple Q&A opens no session tree" empty-state; a spawned workflow → the goal/shape header, the phase
// rows with budget bars (parallel marked + merge), and the terminate summary.
func TestRenderSessionEmptyAndPopulated(t *testing.T) {
	empty := ansi.Strip(renderSession(ViewModel{Width: 64}).Body)
	if !strings.Contains(empty, "no workflow running") {
		t.Fatalf("empty session panel should explain itself:\n%q", empty)
	}
	vm := sampleVM()
	vm.Width = 72
	body := ansi.Strip(renderSession(vm).Body)
	for _, want := range []string{
		"Design a small API", "shape", "3 phases",
		"decompose", "48/64", "⇉", "merge 2 parallel",
		"terminated", "GOAL_MET", "4 nodes", "208 tok",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("populated session panel missing %q:\n%s", want, body)
		}
	}
}

// TestRenderDashboardAllSubsystems confirms the compact Overview header shows one status row per
// subsystem, reading from the snapshot + event stream (the at-a-glance "compact" face).
func TestRenderDashboardAllSubsystems(t *testing.T) {
	vm := sampleVM()
	vm.Width = 80
	body := ansi.Strip(renderDashboard(vm).Body)
	for _, want := range []string{
		"conscious", "subconscious", "grounding", "runtime", "memory", "value", "regulator",
		"2ep·1bl·0pr", "hybrid", // memory sizes + retriever from sampleVM
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, body)
		}
	}
}
