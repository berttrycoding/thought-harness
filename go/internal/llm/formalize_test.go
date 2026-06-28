package llm

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// These tests pin the 5th-axis classical solver's Pattern-B formalization role (FormalizeExpression)
// at the llm layer: the PROMPT enforces the shape-only + no-numbers + NONE contract; the PARSER
// robustly extracts shape + ordered operand names, rejects a literal-bearing shape, and routes a NONE
// answer to the dark path; the claude bridge (TieredBackend) CARRIES the method (a forwarder, not a
// silent drop); and the test double deliberately does NOT implement the port (the solver stays dark
// offline, goldens byte-identical). They are OFFLINE — no model call (the role's HTTP/exec path is
// covered by exercising it against an unreachable backend, which surfaces the gap).

// ----------------------------------------------------------------------------
// the prompt: shape-only, no-numbers, NONE-path
// ----------------------------------------------------------------------------

// TestFormalizePromptDemandsShapeOnlyNoNumbers: the FormalizeExpression system prompt must (a) ask for
// the expression STRUCTURE with NAMED operand placeholders, (b) FORBID numeric literals (the hard rule),
// (c) name the allowed operators/functions, (d) carry the NONE dark-path instruction, and (e) state the
// JSON output contract (expr + operands). These are the load-bearing instructions the contract rests on.
func TestFormalizePromptDemandsShapeOnlyNoNumbers(t *testing.T) {
	ctx := []types.Thought{
		{Text: "Each unit bills at the rate but the invoice is capped. Compute the total cost.", Source: types.GENERATED},
		{Text: "manifest: units = 8", Source: types.OBSERVATION},
	}
	system, user := PromptFormalizeExpression(ctx)
	lower := strings.ToLower(system)

	// (a) shape with named placeholders.
	for _, must := range []string{"expression structure", "named", "placeholder"} {
		if !strings.Contains(lower, must) {
			t.Fatalf("prompt must ask for the shape with named placeholders (%q); got %q", must, system)
		}
	}
	// (b) the HARD no-numbers rule — stated explicitly.
	if !strings.Contains(lower, "never write a numeric literal") {
		t.Fatalf("prompt must FORBID numeric literals (the hard rule); got %q", system)
	}
	if !strings.Contains(lower, "must not supply the number") && !strings.Contains(lower, "not supply the number") {
		t.Fatalf("prompt must say the model must not supply the number; got %q", system)
	}
	// (c) the allowed operators + min/max clamp functions.
	for _, must := range []string{"+ - * /", "min(", "max("} {
		if !strings.Contains(system, must) {
			t.Fatalf("prompt must name the allowed operator/function %q; got %q", must, system)
		}
	}
	// (d) the NONE dark path for non-compute tasks.
	if !strings.Contains(system, "NONE") {
		t.Fatalf("prompt must carry the NONE dark-path instruction; got %q", system)
	}
	for _, must := range []string{"decline", "selection", "lookup"} {
		if !strings.Contains(lower, must) {
			t.Fatalf("prompt must describe the non-compute shapes that get NONE (%q); got %q", must, system)
		}
	}
	// (e) the JSON output contract.
	for _, must := range []string{`"expr"`, `"operands"`, "JSON"} {
		if !strings.Contains(system, must) {
			t.Fatalf("prompt must state the JSON output contract (%q); got %q", must, system)
		}
	}
	// The user message carries the grounded thinking the model formalizes over.
	if !strings.Contains(user, "manifest: units = 8") {
		t.Fatalf("user prompt must carry the grounded thinking; got %q", user)
	}
}

// TestFormalizePromptSystemCarriesNoNumber: belt-and-braces — the SYSTEM prompt must not itself contain a
// numeric literal that a model could echo back into the shape (the directive describes behaviour, never a
// value). Mirrors the ground-complete directive's no-digit invariant.
func TestFormalizePromptSystemCarriesNoNumber(t *testing.T) {
	system, _ := PromptFormalizeExpression(nil)
	for _, r := range system {
		if r >= '0' && r <= '9' {
			t.Fatalf("the formalize SYSTEM prompt must carry NO digit (no leaked number); got %q", system)
		}
	}
}

// ----------------------------------------------------------------------------
// the parser: extract shape+operands, reject literals, route NONE to dark
// ----------------------------------------------------------------------------

// TestParseFormalizationExtractsShapeAndOperands: a realistic model output (the documented object shape,
// even wrapped in a code fence + preamble the loadsObject slice tolerates) yields the exact shape and the
// ORDERED operand names. Both operand shapes are exercised: [{"name","desc"}] and a bare string list.
func TestParseFormalizationExtractsShapeAndOperands(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantExpr string
		wantOps  []string
	}{
		{"object_operands_with_fence",
			"Here is the structure:\n```json\n" +
				`{"expr":"min(a * b, c)","operands":[{"name":"a","desc":"units"},{"name":"b","desc":"hourly rate"},{"name":"c","desc":"the invoice cap"}]}` +
				"\n```\n",
			"min(a * b, c)", []string{"a", "b", "c"}},
		{"bare_string_operands",
			`{"expr":"(a + b) * c","operands":["a","b","c"]}`,
			"(a + b) * c", []string{"a", "b", "c"}},
		{"dedupes_preserves_order",
			`{"expr":"a + b + a","operands":["a","b","a"]}`,
			"a + b + a", []string{"a", "b"}},
		{"preserves_case",
			`{"expr":"min(A * B, C)","operands":[{"name":"A"},{"name":"B"},{"name":"C"}]}`,
			"min(A * B, C)", []string{"A", "B", "C"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, ops, ok := parseFormalization(tc.raw)
			if !ok {
				t.Fatalf("parseFormalization(%q) should parse, got ok=false", tc.raw)
			}
			if expr != tc.wantExpr {
				t.Fatalf("expr = %q, want %q", expr, tc.wantExpr)
			}
			if strings.Join(ops, ",") != strings.Join(tc.wantOps, ",") {
				t.Fatalf("operands = %v, want %v", ops, tc.wantOps)
			}
		})
	}
}

// TestParseFormalizationRejectsLiteralBearingShape: THE hard rule at the role boundary — a shape that
// smuggles a numeric literal anywhere is rejected (ok=false) so the number never reaches the binder. The
// AST validator (parseSolverExpr) is the load-bearing reject; this is the defensive pre-check.
func TestParseFormalizationRejectsLiteralBearingShape(t *testing.T) {
	reject := []string{
		`{"expr":"min(a * b, 250)","operands":["a","b"]}`, // a smuggled cap literal
		`{"expr":"8 * b","operands":["b"]}`,               // a leading literal
		`{"expr":"a + 1","operands":["a"]}`,               // a trailing literal
		`{"expr":"a2 + b","operands":["a2","b"]}`,         // a digit even inside an identifier (conservative reject)
	}
	for _, raw := range reject {
		if expr, ops, ok := parseFormalization(raw); ok {
			t.Fatalf("parseFormalization(%q) must REJECT a literal-bearing shape, got expr=%q ops=%v ok=true", raw, expr, ops)
		}
	}
}

// TestParseFormalizationRoutesNoneToDark: the NONE dark path — an explicit NONE / null / empty expr (in
// any quoting/case) routes to ok=false so the solver stays dark on a non-compute task. Also: a well-formed
// object with NO recoverable operands fires nothing (there is nothing to bind).
func TestParseFormalizationRoutesNoneToDark(t *testing.T) {
	dark := []string{
		`{"expr":"NONE"}`,
		`{"expr":"none"}`,
		`{"expr":" None "}`,
		`{"expr":"null"}`,
		`{"expr":null}`,
		`{"expr":""}`,
		`{"expr":"N/A"}`,
		`{"foo":"bar"}`,                          // no expr key
		"not json at all",                        // no object
		`{"expr":"min(a * b, c)","operands":[]}`, // no operands ⇒ nothing to bind
		`{"expr":"min(a * b, c)","operands":"a, b, c"}`, // operands not a list ⇒ none recovered
		`{"expr":"min(a * b, c)"}`,                      // operands key absent
	}
	for _, raw := range dark {
		if expr, ops, ok := parseFormalization(raw); ok {
			t.Fatalf("parseFormalization(%q) must route to the DARK path (ok=false), got expr=%q ops=%v ok=true", raw, expr, ops)
		}
	}
}

// TestContainsDigit pins the defensive no-literal helper.
func TestContainsDigit(t *testing.T) {
	for _, s := range []string{"min(a * b, 250)", "a + 1", "x9", "0"} {
		if !containsDigit(s) {
			t.Errorf("containsDigit(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"min(a * b, c)", "(a + b) * c", "", "abc"} {
		if containsDigit(s) {
			t.Errorf("containsDigit(%q) = true, want false", s)
		}
	}
}

// ----------------------------------------------------------------------------
// the bridge forwarder + the never-fabricate gap
// ----------------------------------------------------------------------------

// TestFormalizeReachesClaudeBridge is the WIRING proof: the --backend claude bridge (NewClaudeCode →
// TieredBackend) MUST satisfy backends.StructureFormalizer, so the engine's
// `e.backend.(backends.StructureFormalizer)` assertion succeeds on the claude substrate and the solver is
// NOT dark there. Without the explicit TieredBackend forwarder this assertion fails (the embedded core
// Backend does not declare StructureFormalizer) and the solver silently never fires — the exact
// silent-drop the SetGroundCompleteFragment forwarder was added to prevent.
func TestFormalizeReachesClaudeBridge(t *testing.T) {
	bridge := NewClaudeCode(ClaudeCodeOptions{Model: "sonnet", UtilityModel: "haiku"})
	if _, ok := bridge.(*TieredBackend); !ok {
		t.Fatalf("the claude bridge with a utility tier must be a *TieredBackend (the forwarder lives there); got %T", bridge)
	}
	if _, ok := bridge.(backends.StructureFormalizer); !ok {
		t.Fatalf("the claude bridge MUST implement StructureFormalizer so the solver fires on --backend claude")
	}

	// A single-tier bridge (utility "none" ⇒ a bare *OpenAICompatBackend) must carry it too.
	single := NewClaudeCode(ClaudeCodeOptions{Model: "sonnet", UtilityModel: "none"})
	if _, ok := single.(backends.StructureFormalizer); !ok {
		t.Fatalf("the single-tier claude bridge MUST implement StructureFormalizer")
	}

	// And the local --backend llm path carries it identically.
	local := NewOpenAICompat(Options{BaseURL: "http://127.0.0.1:0/v1", Model: "x"})
	if _, ok := backends.Backend(local).(backends.StructureFormalizer); !ok {
		t.Fatalf("the local OpenAICompatBackend must implement StructureFormalizer")
	}
}

// TestTieredFormalizeForwardsToPrimary: the TieredBackend forwarder routes FormalizeExpression to the
// PRIMARY tier (the reasoning role, not the utility one). Proven by routing each tier's transport to a
// distinct canned envelope and asserting the primary's shape comes back.
func TestTieredFormalizeForwardsToPrimary(t *testing.T) {
	primary := NewOpenAICompat(Options{BaseURL: "primary", Model: "p"})
	primary.transport = func(map[string]any, bool) (postResult, error) {
		return postResult{content: `{"expr":"min(a * b, c)","operands":["a","b","c"]}`, finish: "stop",
			reasoningTokens: -1, promptTokens: -1, completionTokens: -1, totalTokens: -1,
			cachedInputTokens: -1, cacheMissTokens: -1}, nil
	}
	utility := NewOpenAICompat(Options{BaseURL: "utility", Model: "u"})
	utility.transport = func(map[string]any, bool) (postResult, error) {
		t.Fatal("FormalizeExpression must NOT route to the utility tier (it is a reasoning role)")
		return postResult{}, nil
	}
	tiered := NewTiered(primary, utility)

	expr, ops, ok := tiered.FormalizeExpression([]types.Thought{{Text: "capped total cost", Source: types.GENERATED}})
	if !ok {
		t.Fatal("tiered FormalizeExpression should return the primary's shape, got ok=false")
	}
	if expr != "min(a * b, c)" || strings.Join(ops, ",") != "a,b,c" {
		t.Fatalf("tiered forwarder returned expr=%q ops=%v, want the primary's min(a * b, c)/[a b c]", expr, ops)
	}
}

// TestFormalizeSurfacesGapOnModelFailure: the never-fabricate discipline — with the model UNREACHABLE,
// FormalizeExpression returns ok=false (the solver fires NOTHING), NEVER a substituted shape. A fabricated
// shape here would be exactly the manufactured intelligence the safety boundary forbids.
func TestFormalizeSurfacesGapOnModelFailure(t *testing.T) {
	b := NewOpenAICompat(Options{BaseURL: "http://127.0.0.1:0/v1", Model: "x"})
	expr, ops, ok := b.FormalizeExpression([]types.Thought{{Text: "capped total cost", Source: types.GENERATED}})
	if ok || expr != "" || ops != nil {
		t.Fatalf("on model failure FormalizeExpression must surface the gap (ok=false, empty), never a substitute; got expr=%q ops=%v ok=%v", expr, ops, ok)
	}
}

// TestFormalizeTestDoubleDoesNotImplement: the test double (backends.TestBackend) deliberately does NOT
// implement StructureFormalizer, so on --backend test the solver stays dark (present-but-silent), keeping
// goldens byte-identical even with the opt-in knob ON. This pins the design invariant at the type level.
func TestFormalizeTestDoubleDoesNotImplement(t *testing.T) {
	var td backends.Backend = backends.NewTest()
	if _, ok := td.(backends.StructureFormalizer); ok {
		t.Fatal("the test double must NOT implement StructureFormalizer (the solver must stay dark offline)")
	}
}
