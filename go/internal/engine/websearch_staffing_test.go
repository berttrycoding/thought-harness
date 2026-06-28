package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// websearch_staffing_test.go is the COGNITION/WIRE test for the web_search UNDER-STAFFING fix
// (subconscious.web_search). The measured bug was UPSTREAM of the dispatch: a bare factual-lookup
// QUESTION recognised NO workflow shape, so the engine staffed NO sub-agent, so the expose-affordances
// operator (which holds the granted web_search scope) never got a turn — web_search fired only ~1/N on
// live web-lookup benchmarks. The fix makes the synthesiser produce a lookup-research program that STAFFS
// expose-affordances for such a goal, AND makes the capability-ON tool picker reach the granted web_search.
//
// These tests drive a REAL reactive episode end-to-end on the deterministic test double + a web.Fake seam
// (NEVER the live network) and assert the staffing + dispatch actually FIRE (the live failure was that they
// did not), plus the byte-identical-OFF arm.

// driveLookupEpisode runs one reactive episode for goal on a fresh workspace engine under feat, with the
// web seam wired to a deterministic web.Fake. It returns whether an expose-affordances sub-agent was
// STAFFED (a subconscious.subagent event names the role) and whether web_search DISPATCHED through the
// gated executor (an action.tool event names the tool) — the two witnesses of the under-staffing fix.
func driveLookupEpisode(t *testing.T, feat *config.HarnessConfig, goal string) (staffedExpose, webDispatched bool) {
	t.Helper()
	e, _ := newWorkspaceEngine(t, feat)
	e.SetWeb(web.NewFake()) // deterministic offline seam (the edge would wire web.NewDuckDuckGo())
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.ActionTool:
			if tn, _ := ev.Data["tool"].(string); tn == "web_search" {
				webDispatched = true
			}
		case events.SubSubagent:
			if role, _ := ev.Data["role"].(string); role == "expose-affordances" {
				staffedExpose = true
			}
		}
	})
	e.Submit(goal, true)
	e.Run(30)
	return staffedExpose, webDispatched
}

// lookupQuestion is the canonical bare factual-lookup question from the bug report: a multi-hop nationality
// comparison that names no local file and matches none of the specific (compare/optimize/analyze/design)
// keyword shapes — exactly the goal that recognised NO shape before the fix.
const lookupQuestion = "Were Scott Derrickson and Ed Wood the same nationality?"

// TestWebSearchFlagOnStaffsExposeAffordancesAndDispatches is the COGNITION proof of the under-staffing fix:
// with subconscious.web_search ON, a bare factual-lookup question STAFFS the expose-affordances lookup
// operator (the synthesiser now recognises a lookup-research shape for it) and that staffed worker
// DISPATCHES web_search through the gated executor (the capability-ON picker now reaches the granted tool).
// Both witnesses must fire — the live failure was that neither did, so the harness ran retrieving-blind.
func TestWebSearchFlagOnStaffsExposeAffordancesAndDispatches(t *testing.T) {
	feat := config.New() // AllOn (capability ON — the path the live bench runs)
	feat.Subconscious.WebSearch = true
	feat.Validate()

	staffedExpose, webDispatched := driveLookupEpisode(t, feat, lookupQuestion)

	if !staffedExpose {
		t.Fatalf("flag ON: a factual-lookup question must STAFF expose-affordances (the under-staffing fix); none staffed for %q", lookupQuestion)
	}
	if !webDispatched {
		t.Fatalf("flag ON: the staffed expose-affordances worker must DISPATCH web_search (the live failure); no web_search action.tool fired for %q", lookupQuestion)
	}
}

// TestWebSearchFlagOffNoStaffingByteIdentical is the byte-identical-OFF arm: with subconscious.web_search
// OFF (the default), the SAME lookup question STAFFS no expose-affordances worker and DISPATCHES no
// web_search — the engine behaves exactly as before the flag (the lookup-research shape is never produced,
// the picker never unions web_search, the tool is not even registered). The flag-OFF pipeline is unchanged.
func TestWebSearchFlagOffNoStaffingByteIdentical(t *testing.T) {
	feat := config.New() // AllOn, web_search OFF (the default)

	staffedExpose, webDispatched := driveLookupEpisode(t, feat, lookupQuestion)

	if staffedExpose {
		t.Fatalf("flag OFF: a lookup question must NOT staff expose-affordances (no lookup-research shape exists); it was staffed for %q", lookupQuestion)
	}
	if webDispatched {
		t.Fatalf("flag OFF: web_search must NOT dispatch (the OFF path is byte-identical); it fired for %q", lookupQuestion)
	}
}

// omitExposeBackend is the LIVE-MODEL stand-in for the engine-level live-failure simulation: it embeds the
// deterministic test double (so every other CONTENT role stays canned + offline) but its SynthesizeProgram
// returns a VALID program for a lookup question that OMITS expose-affordances — exactly what the live claude
// model does, and exactly the case the test double's RecognizeShapeWebDict toolmaker masks (it always staffs
// expose). Driving an episode on this backend exercises the step-3 LOOKUP-FORCE, not the deterministic
// RecognizeShape path the existing staffing test runs.
type omitExposeBackend struct {
	*backends.TestBackend
}

func (omitExposeBackend) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	return map[string]any{
		"program": map[string]any{"kind": "seq", "children": []any{
			map[string]any{"kind": "step", "operator": "decompose", "domain": "general", "note": "break the question down"},
			map[string]any{"kind": "step", "operator": "generate", "domain": "general", "note": "answer it"},
		}},
		"rationale": "model wrote decompose>generate (no expose-affordances)",
		"source":    "llm",
	}, true
}

// driveLookupEpisodeBackend is driveLookupEpisode with an INJECTED backend (the live-model stand-in). It
// keeps the embedder probe offline + deterministic by pointing THOUGHT_LLM_BASE_URL at an unreachable
// address (the wrapper backend is not the *TestBackend the engine's offline contract recognises, so the
// semantic-recall probe would otherwise dial localhost:1234). Semantic recall is irrelevant to web_search,
// so this is a faithful offline reproduction of the live synthesis path.
func driveLookupEpisodeBackend(t *testing.T, feat *config.HarnessConfig, be backends.Backend, goal string) (staffedExpose, webDispatched bool) {
	t.Helper()
	t.Setenv("THOUGHT_LLM_BASE_URL", "http://127.0.0.1:1/v1") // unreachable: the embedder probe fails fast + offline
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = t.TempDir()
	cfg.Features = feat
	e, err := NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	e.SetWeb(web.NewFake())
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.ActionTool:
			if tn, _ := ev.Data["tool"].(string); tn == "web_search" {
				webDispatched = true
			}
		case events.SubSubagent:
			if role, _ := ev.Data["role"].(string); role == "expose-affordances" {
				staffedExpose = true
			}
		}
	})
	e.Submit(goal, true)
	e.Run(30)
	return staffedExpose, webDispatched
}

// TestWebSearchForcesExposeAffordancesEvenWhenSynthOmitsIt is the ENGINE-LEVEL live-failure proof: with
// subconscious.web_search ON and a backend whose SynthesizeProgram returns a valid program that OMITS
// expose-affordances (the measured live-claude behaviour), the engine STILL staffs expose-affordances and
// DISPATCHES web_search through the gated executor. This is the substrate-independent fix end-to-end — it
// does NOT rely on the deterministic RecognizeShapeWebDict toolmaker that the test double wires (which
// already staffs expose and so cannot reproduce the live bug). web.Fake is the offline seam (never the net).
func TestWebSearchForcesExposeAffordancesEvenWhenSynthOmitsIt(t *testing.T) {
	feat := config.New() // AllOn (capability ON — the live bench path)
	feat.Subconscious.WebSearch = true
	feat.Validate()

	be := omitExposeBackend{TestBackend: backends.NewTest()}
	staffedExpose, webDispatched := driveLookupEpisodeBackend(t, feat, be, lookupQuestion)

	if !staffedExpose {
		t.Fatalf("flag ON + model OMITTED expose-affordances: the FORCE must still staff expose-affordances; none staffed for %q", lookupQuestion)
	}
	if !webDispatched {
		t.Fatalf("flag ON + model OMITTED expose-affordances: the forced expose-affordances worker must DISPATCH web_search (the live failure was that it never fired); no web_search action.tool for %q", lookupQuestion)
	}
}

// TestWebSearchForceOffWithOmittingBackendIsByteIdentical is the byte-identical-OFF arm for the live path:
// with the flag OFF, the SAME omitting backend staffs no expose-affordances and dispatches no web_search —
// the force never fires (the model's decompose>generate program runs unchanged).
func TestWebSearchForceOffWithOmittingBackendIsByteIdentical(t *testing.T) {
	feat := config.New() // AllOn, web_search OFF (the default)

	be := omitExposeBackend{TestBackend: backends.NewTest()}
	staffedExpose, webDispatched := driveLookupEpisodeBackend(t, feat, be, lookupQuestion)

	if staffedExpose {
		t.Fatalf("flag OFF: the force must NOT fire — expose-affordances was staffed for %q", lookupQuestion)
	}
	if webDispatched {
		t.Fatalf("flag OFF: web_search must NOT dispatch (byte-identical OFF); it fired for %q", lookupQuestion)
	}
}

// TestWebSearchDoesNotOverStaffOnNonLookupGoals is the CALIBRATION guard (don't trade one bug for another):
// with the flag ON, goals that are NOT external-fact lookup questions must NOT acquire the lookup-research
// shape — a STATEMENT (no question), a goal naming a LOCAL FILE, and an OPTIMIZE goal (which has its own
// program). None of these should staff expose-affordances via the lookup shape, so the known GAIA-L1
// over-grounding regime is not widened. (A local-file goal MAY staff expose-affordances for a file READ on
// some paths; the assertion here is only that web_search does NOT dispatch — the local read wins.)
func TestWebSearchDoesNotOverStaffOnNonLookupGoals(t *testing.T) {
	feat := config.New()
	feat.Subconscious.WebSearch = true
	feat.Validate()

	// A plain statement (not a question, no interrogative lead) — must not reach the open web.
	if _, web := driveLookupEpisode(t, feat, "The capital of France is Paris and it is a city."); web {
		t.Fatal("flag ON: a STATEMENT goal must not dispatch web_search (the lookup shape is question-only)")
	}
	// A goal naming a concrete local file — the local read wins, no web lookup (the precedence guard).
	if _, web := driveLookupEpisode(t, feat, "read the value assigned to ActionMargin in regulator.go"); web {
		t.Fatal("flag ON: a goal naming a local file must read the file, not dispatch web_search")
	}
}
