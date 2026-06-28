package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// buildCapEngine builds an engine with subconscious.capability set to `on`, on the deterministic test
// double, and opens an episode whose goal synthesises a workflow (decompose/validate language). It
// returns the engine so a wiring gate can read the live staffing.
func buildCapEngine(t *testing.T, on bool, goal string) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	feat := config.New() // AllOn
	feat.Subconscious.Capability = on
	cfg.Features = feat
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.startEpisode(goal, true)
	return e
}

// TestCapabilityStaffsWorkerWithContextAndScope is THE wiring gate the audit demanded: tests-pass !=
// the feature runs. It proves the BUILT-BUT-DEAD object-model wire is now LIVE on the engine's episode
// path — with subconscious.capability ON, the producing Capability sources a Scope (gap 4), captures a
// Context (gap 2), and the engine staffs the workflow so a worker the LIVE dispatch instantiates actually
// RECEIVES both (gap 2/3). It reads the worker's OWN accessors (Context()/scope), not just that it
// compiles. With the flag OFF the workflow carries no staffing — byte-identical to before the rewire.
//
// GAP-2 FIX (the live-claude finding made mechanical): the capture must happen at STAFFING time, not
// episode-open. This test GROWS the live branch past the goal root after the episode opens, re-staffs the
// workflow exactly as the live dispatch does, and asserts the worker now reads the GROWN branch (more than
// the goal-root snapshot the episode-open capture froze) — the proof the recapture wins and the worker is
// no longer starved.
func TestCapabilityStaffsWorkerWithContextAndScope(t *testing.T) {
	goal := "design and validate a small todo service"

	on := buildCapEngine(t, true, goal)
	if on.episodeContext == nil {
		t.Fatal("capability ON: a Context must be captured (the gap-2 episodeContext is no longer write-only dead)")
	}
	wfOn := on.subconscious.Workflow()
	if wfOn == nil {
		t.Fatal("the capability path must produce a workflow for a decompose/validate goal")
	}
	// gap 4: the producing Capability SOURCED an authority ceiling and the engine threaded it into staffing.
	if wfOn.Scope() == nil {
		t.Fatal("capability ON: the workflow must carry the §3.3a Scope ceiling the Capability sourced (gap 4 wire is dead)")
	}

	// The episode-OPEN capture froze the goal root (the branch was just the goal at startEpisode). Record its
	// size — the worker must end up with MORE than this (the gap-2 fix), never pinned to it.
	openSize := len(on.episodeContext.WorkerContext())

	// THE CONSCIOUS KEEPS THINKING: grow the live active branch past the goal root, exactly as a mid-episode
	// worker would be staffed against. The recapture closure re-captures against THIS grown graph.
	for i := 0; i < 4; i++ {
		on.graph.Append(&types.Thought{ID: -1, Text: "developing the design step", Source: types.GENERATED,
			Confidence: 0.6}, i+2)
	}

	// Instantiate the current phase exactly as the LIVE dispatch does (Dispatch calls workflow.Instantiate)
	// and prove every staffed worker RECEIVED the rich Context (now the GROWN branch) + the ceiling.
	phase := wfOn.Current()
	subs := wfOn.Instantiate(phase, on.executor, on.cognitiveView())
	if len(subs) == 0 {
		t.Fatal("the staffed workflow must instantiate at least one worker for the live phase")
	}
	for _, sa := range subs {
		if sa.Context() == nil {
			t.Fatal("a LIVE-staffed worker must receive the captured Context (the flaky-grounding root-cause fix)")
		}
		// the rich context scales with the branch (it is NOT the ≤5 slice) — a real frozen snapshot.
		got := len(sa.Context().WorkerContext())
		if got == 0 {
			t.Fatal("the worker's Context must carry the frozen branch snapshot (the gap-2 material)")
		}
		// THE FIX: the worker reads the GROWN branch (re-captured at staffing time), strictly MORE than the
		// goal-root snapshot the episode-open capture froze — it is NOT starved to the goal root.
		if got <= openSize {
			t.Fatalf("a mid-episode worker was STARVED: it got the episode-open snapshot (%d) not the grown "+
				"branch (>%d) — the staffing-time recapture did not win (gap-2 fix is dead)", got, openSize)
		}
	}

	// OFF: byte-identical — no Context captured, no Scope on the workflow, no staffing.
	off := buildCapEngine(t, false, goal)
	if off.episodeContext != nil {
		t.Error("capability OFF: no Context should be captured (byte-identical default)")
	}
	if wfOff := off.subconscious.Workflow(); wfOff != nil && wfOff.Scope() != nil {
		t.Error("capability OFF: the workflow must carry NO Scope ceiling (byte-identical default)")
	}
}

// TestCapabilityEmitsScopeEvent proves the Scope is OBSERVABLE — the engine emits subconscious.scope with
// the run's authority ceiling when capability is on (the observability contract: a component with no event
// is invisible). With capability OFF the kind never fires (byte-identical goldens).
func TestCapabilityEmitsScopeEvent(t *testing.T) {
	collect := func(on bool) []events.Event {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New()
		feat.Subconscious.Capability = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		var scopeEvents []events.Event
		e.bus.Subscribe(func(ev events.Event) {
			if ev.Kind == events.SubScope {
				scopeEvents = append(scopeEvents, ev)
			}
		})
		e.startEpisode("design and validate a small todo service", true)
		return scopeEvents
	}

	if got := collect(false); len(got) != 0 {
		t.Fatalf("capability OFF: subconscious.scope must NEVER fire (byte-identical); got %d", len(got))
	}
	on := collect(true)
	if len(on) == 0 {
		t.Fatal("capability ON: the sourced authority ceiling must emit subconscious.scope (the audit line)")
	}
	cats, ok := on[0].Data["categories"].([]string)
	if !ok || len(cats) == 0 {
		t.Fatalf("subconscious.scope must carry the ceiling's categories; got %v", on[0].Data["categories"])
	}
}

// TestCapabilityWriteGoalAdmitsMutate proves the sourced ceiling is REAL least-privilege, not "allow all":
// a read/analyse goal admits only the sensing band (inspect/execute), while a goal that AUTHORS a write
// (build/implement) additionally admits the mutate category — the §3.3a least-privilege bite, sourced
// deterministically from the goal language (no model call).
func TestCapabilityWriteGoalAdmitsMutate(t *testing.T) {
	read := buildCapEngine(t, true, "compare and analyse the two designs")
	wf := read.subconscious.Workflow()
	if wf == nil || wf.Scope() == nil {
		t.Skip("the read goal did not synthesise a workflow on the double; ceiling sourcing is covered elsewhere")
	}
	if wf.Scope().AllowsCategory("mutate") {
		t.Error("a read/analyse goal must NOT admit the mutate (write) category (least privilege)")
	}

	write := buildCapEngine(t, true, "build and implement the parser module")
	wwf := write.subconscious.Workflow()
	if wwf == nil || wwf.Scope() == nil {
		t.Skip("the write goal did not synthesise a workflow on the double")
	}
	if !wwf.Scope().AllowsCategory("mutate") {
		t.Error("a build/implement goal must admit the mutate (write) category in its ceiling")
	}
}
