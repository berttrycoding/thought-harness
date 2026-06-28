package subconscious

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
	"github.com/berttrycoding/thought-harness/internal/value"
)

// saEvent is one captured subconscious.subagent emit (kind/summary/data), recorded in order.
type saEvent struct {
	kind    string
	summary string
	data    events.D
}

// saCapture returns an events.Emit closure that records every emit into *out (order-preserving),
// matching the events.Emit signature exactly.
func saCapture(out *[]saEvent) events.Emit {
	return func(kind, summary string, data events.D) events.Event {
		*out = append(*out, saEvent{kind: kind, summary: summary, data: data})
		return events.Event{}
	}
}

// onlySubagentEvents filters to the subconscious.subagent emits (the effectual path's executor may
// also emit action.* events; the subagent definition is the one the gate pins).
func onlySubagentEvents(evs []saEvent) []saEvent {
	out := make([]saEvent, 0, len(evs))
	for _, e := range evs {
		if e.kind == string(events.SubSubagent) {
			out = append(out, e)
		}
	}
	return out
}

// genThought is a one-thought context whose source is GENERATED (a real, non-METACOG line the
// sub-agent's context_slice keeps).
func genThought(text string) []types.Thought {
	return []types.Thought{{Text: text, Source: types.GENERATED}}
}

// TestSubAgentFireReasonOnlyParity is the Tier-4 gate (PORT-PLAN §2 row 4): subagent.fire with NO
// collaborators (no executor, no cognition) takes the REASON-ONLY path and emits the same
// subconscious.subagent definition. STRONG (byte-exact) parity: the deterministic TestBackend's
// OperatorApply produces fixed text, so the Candidate text + the full definition compare exactly to
// the Python golden (captured by running subagent.fire on TestBackend).
func TestSubAgentFireReasonOnlyParity(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	be := backends.NewTest()

	type want struct {
		role           string
		candText       string
		stance         string // "" == None
		operator       string // Candidate.Operator.String()
		summary        string
		persona        string
		responsibility string
		family         string
		intent         string
		systemPrompt   string
	}
	wants := []want{
		{
			role:     "decompose",
			candText: "[decompose] break 'fix the login bug' into parts: inputs, core step, check",
			stance:   "", operator: "DECOMPOSE",
			summary:        "sub-agent decompose/code: [decompose] break 'fix the login bug' into p",
			persona:        "a methodical engineer who reshapes the problem",
			responsibility: "Apply the 'decompose' operator to the current line: break the problem into independent parts.",
			family:         "transformative",
			intent:         "break the problem into independent parts",
			systemPrompt: "You are a methodical engineer who reshapes the problem, acting as the 'decompose' operator for a code task.\n" +
				"Responsibility: Apply the 'decompose' operator to the current line: break the problem into independent parts.\n" +
				"Out of scope: Produce ONLY the result of this one 'decompose' move, scoped to code. Do not solve the whole task, do not take any external action, and stay silent if the current line is not about code.\n" +
				"Tools: none (reason from context only).\n" +
				"Return one concise result; you run once.",
		},
		{
			role:     "validate",
			candText: "[validate] checks for code: inputs valid, edge cases handled, result matches intent",
			stance:   "checks", operator: "VALIDATE",
			summary:        "sub-agent validate/code: [validate] checks for code: inputs valid, ed",
			persona:        "a careful analyst who relates and weighs",
			responsibility: "Apply the 'validate' operator to the current line: check the result against the requirements.",
			family:         "relational",
			intent:         "check the result against the requirements",
			systemPrompt: "You are a careful analyst who relates and weighs, acting as the 'validate' operator for a code task.\n" +
				"Responsibility: Apply the 'validate' operator to the current line: check the result against the requirements.\n" +
				"Out of scope: Produce ONLY the result of this one 'validate' move, scoped to code. Do not solve the whole task, do not take any external action, and stay silent if the current line is not about code.\n" +
				"Tools: none (reason from context only).\n" +
				"Return one concise result; you run once.",
		},
	}

	for _, w := range wants {
		t.Run(w.role, func(t *testing.T) {
			spec, ok := cat.Get(w.role)
			if !ok {
				t.Fatalf("seed catalog missing %q", w.role)
			}
			var emitted []saEvent
			sa := NewSubAgent(spec, "code", "fix the login bug", be, saCapture(&emitted),
				"sa:"+w.role+":code@0.0", nil, nil, nil) // nil executor + nil cognition ⇒ reason-only
			cand := sa.Fire(genThought("the login endpoint returns 500"), cpyrand.New(0))

			// Candidate parity (the seam-facing artefact).
			if cand == nil {
				t.Fatal("reason-only fire must return a Candidate")
			}
			if cand.Text != w.candText {
				t.Errorf("cand.Text = %q, want %q", cand.Text, w.candText)
			}
			if cand.Source != types.INJECTED {
				t.Errorf("cand.Source = %v, want INJECTED", cand.Source)
			}
			if cand.Operator == nil || cand.Operator.String() != w.operator {
				t.Errorf("cand.Operator = %v, want %s", cand.Operator, w.operator)
			}
			gotStance := ""
			if cand.Stance != nil {
				gotStance = *cand.Stance
			}
			if gotStance != w.stance {
				t.Errorf("cand.Stance = %q, want %q", gotStance, w.stance)
			}

			// PATH parity: exactly one subconscious.subagent event with NO path-specific extras
			// (no executed / tool / ok / exit_code — the reason-only signature).
			sub := onlySubagentEvents(emitted)
			if len(sub) != 1 {
				t.Fatalf("emitted %d subagent events, want exactly 1", len(sub))
			}
			ev := sub[0]
			if ev.summary != w.summary {
				t.Errorf("event summary = %q, want %q", ev.summary, w.summary)
			}
			for _, extra := range []string{"executed", "tool", "ok", "exit_code"} {
				if _, present := ev.data[extra]; present {
					t.Errorf("reason-only event must NOT carry %q extra; got %v", extra, ev.data[extra])
				}
			}
			// Full subagent DEFINITION parity (every key Python's _emit carries).
			assertDefinition(t, ev.data, definition{
				id: "sa:" + w.role + ":code@0.0", role: w.role, persona: w.persona,
				domain: "code", responsibility: w.responsibility, family: w.family,
				toolScope: []string{}, verifierType: "soft", intent: w.intent, systemPrompt: w.systemPrompt,
			})
		})
	}
}

// TestSubAgentFireCognitionExecParity is the Tier-4 gate's COGNITION-EXEC collaborator-presence
// fixture: a sub-agent whose role has cognition semantics (rank) WITH a bound CognitiveView takes the
// cognition-execution path (not reason) and emits the subconscious.subagent definition PLUS the
// `executed` extra. STRUCTURAL parity (PORT-PLAN §3.2): the result TEXT (`ranked the lines by value:
// b0(...)`) is value-math derived and not asserted byte-exact; the gate asserts the PATH (executed),
// the operator, and that the full definition keys are present — cross-checked vs Python (which fires
// the same path with `executed: rank`).
func TestSubAgentFireCognitionExecParity(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	be := backends.NewTest()

	// build a tiny live graph + value signal (the cognition substrate rank/eliminate read).
	g := graph.New("rank the options")
	g.Append(&types.Thought{Text: "option A is fast but risky", Source: types.GENERATED, Confidence: 0.6}, 0)
	g.Append(&types.Thought{Text: "option B is slow but safe", Source: types.GENERATED, Confidence: 0.7}, 0)
	val := value.New(nil)
	view := NewCognitiveView(g, val)

	spec, ok := cat.Get("rank")
	if !ok {
		t.Fatal("seed catalog missing 'rank'")
	}
	var emitted []saEvent
	sa := NewSubAgent(spec, "planning", "rank the options", be, saCapture(&emitted),
		"sa:rank:planning@0.0", nil, nil, view) // cognition bound, no executor
	cand := sa.Fire(genThought("we have two options to weigh"), cpyrand.New(0))

	if cand == nil {
		t.Fatal("cognition-exec fire must return a Candidate")
	}
	if cand.Source != types.INJECTED {
		t.Errorf("cand.Source = %v, want INJECTED", cand.Source)
	}
	// the cognition path carries a structured payload (Python: dict with op/ordering keys).
	if cand.Payload == nil {
		t.Error("cognition-exec Candidate must carry a payload")
	}

	sub := onlySubagentEvents(emitted)
	if len(sub) != 1 {
		t.Fatalf("emitted %d subagent events, want exactly 1", len(sub))
	}
	ev := sub[0]
	// PATH parity: the cognition path stamps the `executed` extra = the op (Python golden: rank).
	if ev.data["executed"] != "rank" {
		t.Errorf("event data[executed] = %v, want \"rank\" (the cognition-exec path marker)", ev.data["executed"])
	}
	// NOT the reason/tool path: no tool/ok/exit_code.
	for _, extra := range []string{"tool", "ok", "exit_code"} {
		if _, present := ev.data[extra]; present {
			t.Errorf("cognition-exec event must NOT carry the effectual extra %q", extra)
		}
	}
	// the full definition is still emitted (structural: keys present + scalars correct).
	assertDefinition(t, ev.data, definition{
		id: "sa:rank:planning@0.0", role: "rank", persona: "a careful analyst who relates and weighs",
		domain: "planning", responsibility: "Apply the 'rank' operator to the current line: " + spec.Intent + ".",
		family: spec.Family, toolScope: []string{}, verifierType: "soft", intent: spec.Intent,
		systemPrompt: sa.SystemPrompt(),
	})
}

// TestSubAgentFireEffectualToolParity is the Tier-4 gate's EFFECTUAL collaborator-presence fixture: a
// sub-agent with a non-empty toolScope AND a bound executor dispatches a REAL scoped tool (search) and
// emits the subconscious.subagent definition PLUS the tool/ok/exit_code extras. STRUCTURAL parity: the
// result TEXT carries an absolute temp path (environment-specific), so the gate asserts the PATH
// (tool=search, ok=true, exit_code=nil) + the definition keys — cross-checked vs Python (which fires
// the same path with tool='search', ok=True, exit_code=None over a temp workspace).
func TestSubAgentFireEffectualToolParity(t *testing.T) {
	cat := cognition.NewOperatorRegistry()
	be := backends.NewTest()

	// a temp workspace with one file the search will hit.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "auth.py"),
		[]byte("def login(): return 500  # login bug\n"), 0o644); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	exec := action.NewToolExecutor(
		action.NewToolRegistry(action.DefaultTools(ws, 5*time.Second)),
		&action.ExecutorOptions{Sandbox: action.NewSandbox([]string{ws})},
	)

	// expose-affordances seeds tool_scope=(search, read_file); the subagent's toolCall builds a real
	// search call (goal 'login' -> keyword 'login' > 3 chars).
	spec, ok := cat.Get("expose-affordances")
	if !ok {
		t.Fatal("seed catalog missing 'expose-affordances'")
	}
	var emitted []saEvent
	sa := NewSubAgent(spec, "code", "fix the login bug", be, saCapture(&emitted),
		"sa:expose-affordances:code@0.0", spec.ToolScope, exec, nil) // executor bound, no cognition
	cand := sa.Fire(genThought("the login endpoint returns 500"), cpyrand.New(0))

	if cand == nil {
		t.Fatal("effectual fire must return a Candidate")
	}
	if cand.Source != types.INJECTED {
		t.Errorf("cand.Source = %v, want INJECTED", cand.Source)
	}
	if cand.Payload == nil {
		t.Error("effectual Candidate must carry the real ToolResult as payload")
	}

	ev := onlySubagentEvents(emitted)
	if len(ev) != 1 {
		t.Fatalf("emitted %d subagent events, want exactly 1", len(ev))
	}
	d := ev[0].data
	// PATH parity: the effectual path stamps tool/ok/exit_code (Python golden: search/True/None).
	if d["tool"] != "search" {
		t.Errorf("event data[tool] = %v, want \"search\"", d["tool"])
	}
	if d["ok"] != true {
		t.Errorf("event data[ok] = %v, want true", d["ok"])
	}
	if ec, present := d["exit_code"]; !present {
		t.Error("effectual event must carry exit_code")
	} else if ec != (*int)(nil) {
		t.Errorf("event data[exit_code] = %v, want nil (*int) (search has no exit code)", ec)
	}
	// NOT the cognition path.
	if _, present := d["executed"]; present {
		t.Error("effectual event must NOT carry the cognition-exec `executed` extra")
	}
	// the full definition is still emitted (structural). toolScope is non-empty here.
	assertDefinition(t, d, definition{
		id: "sa:expose-affordances:code@0.0", role: "expose-affordances",
		persona: sa.Persona(), domain: "code",
		responsibility: "Apply the 'expose-affordances' operator to the current line: " + spec.Intent + ".",
		family:         spec.Family, toolScope: spec.ToolScope, verifierType: "soft",
		intent: spec.Intent, systemPrompt: sa.SystemPrompt(),
	})
}

// definition is the full subconscious.subagent definition Python's _emit always carries (the base
// map, before any path-specific extras).
type definition struct {
	id, role, persona, domain, responsibility, family string
	toolScope                                         []string
	verifierType, intent, systemPrompt                string
}

// assertDefinition checks that the emitted event data carries every base definition key with the
// expected value. tool_scope is asserted as the exact ordered []string (Python list(self.tool_scope),
// non-nil so an empty scope is [] on the wire, never null).
func assertDefinition(t *testing.T, data events.D, w definition) {
	t.Helper()
	scalars := map[string]string{
		"id": w.id, "role": w.role, "persona": w.persona, "domain": w.domain,
		"responsibility": w.responsibility, "family": w.family, "verifier_type": w.verifierType,
		"intent": w.intent, "system_prompt": w.systemPrompt,
	}
	for k, want := range scalars {
		if got, _ := data[k].(string); got != want {
			t.Errorf("definition[%q] = %q, want %q", k, got, want)
		}
	}
	gotScope, ok := data["tool_scope"].([]string)
	if !ok {
		t.Fatalf("definition[tool_scope] is %T, want []string (non-nil even when empty)", data["tool_scope"])
	}
	if len(gotScope) != len(w.toolScope) {
		t.Fatalf("tool_scope = %v, want %v", gotScope, w.toolScope)
	}
	gs, ws := append([]string(nil), gotScope...), append([]string(nil), w.toolScope...)
	sort.Strings(gs)
	sort.Strings(ws)
	for i := range gs {
		if gs[i] != ws[i] {
			t.Errorf("tool_scope = %v, want %v", gotScope, w.toolScope)
			break
		}
	}
}
