package subconscious

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// chainComprehender is the scripted RealityComprehender for the multi-hop grounding-chain test. It is
// the deterministic stand-in for what a correct live agent's "to_operator" call does on a chain: it
// reads the NEXT-HOP path off the PRIOR observation in context — the path that does NOT appear in the
// static goal. This is the exact shape FIX 1 exists for: the regex floor (over the static goal) can
// never extract config/profiles/prod.yaml because that token is only known from hop-1's read.
type chainComprehender struct {
	*backends.TestBackend
}

// Comprehend: if hop-1's observation (which names the prod profile path) is in context, READ that
// next-hop path; else READ the entrypoint env.yaml (the path the goal names). The test asserts the
// flag-ON path reaches hop 2 (the context-derived path), which the floor cannot.
func (b *chainComprehender) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	for _, t := range ctx {
		if containsSub(t.Text, "config/profiles/prod.yaml") {
			return "read", "config/profiles/prod.yaml", true // next hop: the path lives in the prior observation
		}
	}
	return "read", "env.yaml", true // hop 1: the entrypoint the goal names
}

var _ backends.RealityComprehender = (*chainComprehender)(nil)

// containsSub is a tiny substring check (the test avoids importing strings into this assertion-only file).
func containsSub(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// buildChainSubAgent builds a read/search-scoped SubAgent over a REAL executor on a temp workspace
// holding the two-hop config chain, plus the scripted chain comprehender. The static goal names ONLY
// the entrypoint (env.yaml) — the next-hop path is reachable only from hop-1's observation.
func buildChainSubAgent(t *testing.T, capture *[]events.Event) (*SubAgent, string) {
	t.Helper()
	ws := t.TempDir()
	// hop 1: env.yaml names the prod profile path (the only place the next-hop token appears).
	if err := os.WriteFile(filepath.Join(ws, "env.yaml"),
		[]byte("active_profile: prod\nprofile_path: config/profiles/prod.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// hop 2: the prod profile holds the answer (the checkout block value).
	if err := os.MkdirAll(filepath.Join(ws, "config", "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "config", "profiles", "prod.yaml"),
		[]byte("checkout:\n  max_retries: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	emit := func(kind, summary string, data events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: data}
		*capture = append(*capture, ev)
		return ev
	}
	reg := action.NewToolRegistry(action.DefaultTools(ws, 5*time.Second))
	exe := action.NewToolExecutor(reg, &action.ExecutorOptions{Emit: emit})
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "primitive",
		Intent: "surface the files reality offers"}
	sa := NewSubAgent(spec, "code", "trace the active checkout config", &chainComprehender{TestBackend: backends.NewTest()},
		emit, "sa:expose:code", []string{"read_file", "search"}, exe, nil)
	return sa, ws
}

// TestModelSelectAdvancesGroundingChain is the FIX 1 COGNITION test: with THOUGHT_MODEL_SELECT ON, a
// SubAgent on a multi-hop grounding chain READS the next-hop path the model reasoned to FROM THE PRIOR
// OBSERVATION (config/profiles/prod.yaml) — a path the static-goal regex floor can never extract. With
// the flag OFF, the floor behaves exactly as today: it does NOT escalate, so the context-derived path
// is never read. This proves the chain advances past hop 1 only when the model ceiling is enabled.
func TestModelSelectAdvancesGroundingChain(t *testing.T) {
	prev := modelSelectEnabled
	defer func() { modelSelectEnabled = prev }()

	// hop-1 observation in context: env.yaml has been read, naming the next-hop path.
	ctx := []types.Thought{
		{ID: 1, Text: "trace the active checkout config", Source: types.USER_INPUT},
		{ID: 2, Text: "read env.yaml -> active_profile: prod, profile_path: config/profiles/prod.yaml",
			Source: types.OBSERVATION},
	}

	// FLAG ON: the model ceiling fires, the next-hop path (from the observation) is read.
	modelSelectEnabled = true
	var onEvents []events.Event
	saOn, _ := buildChainSubAgent(t, &onEvents)
	cOn := saOn.Fire(ctx, cpyrand.New(1))
	if cOn == nil {
		t.Fatal("flag ON: the sub-agent fired no candidate (expected a real read of the next-hop file)")
	}
	if !containsSub(cOn.Text, "max_retries") && !containsSub(cOn.Text, "7") {
		t.Fatalf("flag ON: the chain did NOT advance to hop 2 — the next-hop file was not read; got %q", cOn.Text)
	}
	sawToolSelect, readNextHop := false, false
	for _, ev := range onEvents {
		if ev.Kind == events.EscalationToolSelect {
			sawToolSelect = true
			if tgt, _ := ev.Data["model_target"].(string); tgt == "config/profiles/prod.yaml" {
				readNextHop = true
			}
		}
	}
	if !sawToolSelect {
		t.Fatal("flag ON: escalation.tool_select was never emitted — the Pattern-C escalation is invisible")
	}
	if !readNextHop {
		t.Fatal("flag ON: the model did not pick the context-derived next-hop path config/profiles/prod.yaml")
	}

	// FLAG OFF: byte-identical to today — no escalation, the model ceiling never fires. The floor over
	// the static goal ("trace the active checkout config") names no readable file, so the read scope
	// falls to its goal-keyword path; the context-derived next-hop file is NOT read and no
	// escalation.tool_select is emitted.
	modelSelectEnabled = false
	var offEvents []events.Event
	saOff, _ := buildChainSubAgent(t, &offEvents)
	cOff := saOff.Fire(ctx, cpyrand.New(1))
	for _, ev := range offEvents {
		if ev.Kind == events.EscalationToolSelect {
			t.Fatal("flag OFF: escalation.tool_select fired — the floor-only path is NOT byte-identical")
		}
	}
	if cOff != nil && containsSub(cOff.Text, "max_retries") {
		t.Fatal("flag OFF: the chain advanced to hop 2 without the model ceiling — the floor must not read the context-derived path")
	}
}

// TestModelSelectFloorStandsWhenGoalNamesFile is the precision control: when the STATIC goal already
// names a readable file (the floor is usable), the model ceiling does NOT fire even with the flag ON —
// the deterministic path is sufficient and is not second-guessed. This is the flagged-fuzzy gate: only
// a goal the floor cannot resolve escalates.
func TestModelSelectFloorStandsWhenGoalNamesFile(t *testing.T) {
	prev := modelSelectEnabled
	defer func() { modelSelectEnabled = prev }()
	modelSelectEnabled = true

	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "limits.go"),
		[]byte("package config\n\nconst MaxRetries = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var captured []events.Event
	emit := func(kind, summary string, data events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: data}
		captured = append(captured, ev)
		return ev
	}
	reg := action.NewToolRegistry(action.DefaultTools(ws, 5*time.Second))
	exe := action.NewToolExecutor(reg, &action.ExecutorOptions{Emit: emit})
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "primitive", Intent: "read it"}
	// the goal NAMES the file -> the regex floor resolves read_file{limits.go}; the ceiling must stand down.
	sa := NewSubAgent(spec, "code", "read limits.go and report MaxRetries", &chainComprehender{TestBackend: backends.NewTest()},
		emit, "sa:expose:code", []string{"read_file", "search"}, exe, nil)

	c := sa.Fire([]types.Thought{{ID: 1, Text: "read limits.go and report MaxRetries", Source: types.USER_INPUT}}, cpyrand.New(1))
	if c == nil {
		t.Fatal("the sub-agent fired no candidate (the floor should have read limits.go)")
	}
	for _, ev := range captured {
		if ev.Kind == events.EscalationToolSelect {
			t.Fatal("the model ceiling fired even though the floor named a readable file (the flagged-fuzzy gate is broken)")
		}
	}
	if !containsSub(c.Text, "MaxRetries") && !containsSub(c.Text, "3") {
		t.Fatalf("the floor did not read the goal-named file; got %q", c.Text)
	}
}

// TestResolveModelSelectParsing pins the env-knob parser: only the explicit truthy values enable it;
// unset / garbage / false is OFF (the default that keeps the floor-only byte-identical path).
func TestResolveModelSelectParsing(t *testing.T) {
	cases := map[string]bool{"": false, "0": false, "false": false, "no": false, "garbage": false,
		"1": true, "true": true, "TRUE": true, "yes": true, "on": true}
	for val, want := range cases {
		t.Setenv("THOUGHT_MODEL_SELECT", val)
		if got := resolveModelSelect(); got != want {
			t.Errorf("resolveModelSelect(%q) = %v, want %v", val, got, want)
		}
	}
}
