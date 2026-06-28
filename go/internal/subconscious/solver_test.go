package subconscious

import (
	"math/big"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// These tests pin the COGNITION of the 5th-axis classical solver specialist, not the plumbing
// (docs/internal/notes/2026-06-19-specialized-component-registry-axis.md §5). The make-or-break property is the
// GROUNDED-OPERAND safety hook: the LLM writes ONLY the expression shape (operators/named operands,
// never a literal), and every operand must trace to a GROUNDED READ (an OBSERVATION-sourced thought) or
// the specialist fires NOTHING. A mechanical test that only checks "computes correctly given correct
// inputs" dodges the actual risk (a confident-wrong computed answer on a mis-formalize); these assert
// the hook rejects the ungrounded / mis-bound / literal-smuggling cases.

// fakeFormalizer is the test-local backends.StructureFormalizer (the test double does NOT implement the
// port, by design, so the solver stays dark on the test backend). It returns a fixed shape + ordered
// operand names — the model's Pattern-B output, stripped of all numbers.
type fakeFormalizer struct {
	expr     string
	operands []string
	ok       bool
}

func (f *fakeFormalizer) FormalizeExpression(ctx []types.Thought) (string, []string, bool) {
	return f.expr, f.operands, f.ok
}

// groundedReadThought builds an OBSERVATION-sourced thought carrying a real tool-result payload — the
// in-graph representation of a watched-seam read (what GroundingReadHappened witnesses). Numbers in such
// a thought are grounded operands; numbers in a GENERATED thought (dispatchCtx) are NOT.
func groundedReadThought(id int, text string) types.Thought {
	return types.Thought{
		ID: id, Text: text, Source: types.OBSERVATION,
		RawReturn: action.ToolResult{Name: "read_file", Content: text},
	}
}

// generatedThought builds a GENERATED-source thought (the model's own working) — deliberately NOT a
// grounded read, so a number in it must NOT bind an operand.
func generatedThought(id int, text string) types.Thought {
	return types.Thought{ID: id, Text: text, Source: types.GENERATED}
}

func ratOf(t *testing.T, s string) *big.Rat {
	t.Helper()
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		t.Fatalf("bad rat %q", s)
	}
	return r
}

// TestSolverGroundsOperandsThenComputesExactly_MinCap is the min()-cap clamp gap (the measured cluster).
// The model writes the SHAPE min(a*b, c); the three operands bind positionally to the three grounded
// reads (units, rate, cap); the deterministic evaluator clamps EXACTLY. 8*40=320, capped at 250 -> 250.
func TestSolverGroundsOperandsThenComputesExactly_MinCap(t *testing.T) {
	fm := &fakeFormalizer{expr: "min(a * b, c)", operands: []string{"a", "b", "c"}, ok: true}
	s := NewSolverPrimitiveSubAgent(fm, nil)
	ctx := []types.Thought{
		generatedThought(1, "Each unit bills at the hourly rate but the invoice is capped. Compute the total cost."),
		groundedReadThought(2, "manifest: units = 8"),
		groundedReadThought(3, "rate.yaml: hourly_rate = 40"),
		groundedReadThought(4, "policy.yaml: invoice_cap = 250"),
	}
	if rel := s.Relevance(ctx); rel == 0 {
		t.Fatalf("solver should be relevant on a clamp+multi-number structure, got 0")
	}
	c := s.Fire(ctx, cpyrand.New(7))
	if c == nil {
		t.Fatal("solver must fire on grounded clamp structure (all operands traced to reads)")
	}
	if c.Domain == nil || *c.Domain != "solver" {
		t.Fatalf("candidate domain = %v, want solver", c.Domain)
	}
	got, ok := c.Payload.(*big.Rat)
	if !ok {
		t.Fatalf("payload is %T, want *big.Rat", c.Payload)
	}
	if got.Cmp(ratOf(t, "250")) != 0 {
		t.Fatalf("min(8*40, 250) = %s, want 250 (the clamp the bare model mis-computes)", formatRat(got))
	}
}

// TestSolverGroundsOperandsThenComputesExactly_MultiHopMultiply is the multi-hop-then-multiply gap. The
// model writes a+b then *c as the SHAPE (a+b)*c; operands bind to three grounded reads; exact compute.
// (12+18)*7 = 210.
func TestSolverGroundsOperandsThenComputesExactly_MultiHopMultiply(t *testing.T) {
	fm := &fakeFormalizer{expr: "(a + b) * c", operands: []string{"a", "b", "c"}, ok: true}
	s := NewSolverPrimitiveSubAgent(fm, nil)
	ctx := []types.Thought{
		generatedThought(1, "Sum the two batches then multiply the result by the run count."),
		groundedReadThought(2, "batch_one count: 12"),
		groundedReadThought(3, "batch_two count: 18"),
		groundedReadThought(4, "runs.txt: 7"),
	}
	c := s.Fire(ctx, cpyrand.New(7))
	if c == nil {
		t.Fatal("solver must fire on grounded multi-hop structure")
	}
	got := c.Payload.(*big.Rat)
	if got.Cmp(ratOf(t, "210")) != 0 {
		t.Fatalf("(12+18)*7 = %s, want 210", formatRat(got))
	}
}

// TestSolverFiresNothingWhenOperandUngrounded is THE safety hook: when a needed operand has no grounded
// read to trace to (it lives only in the model's GENERATED working, or simply isn't present), the
// specialist fires NOTHING — it never emits a number built on an ungrounded input.
func TestSolverFiresNothingWhenOperandUngrounded(t *testing.T) {
	fm := &fakeFormalizer{expr: "min(a * b, c)", operands: []string{"a", "b", "c"}, ok: true}
	s := NewSolverPrimitiveSubAgent(fm, nil)

	// Case 1: the cap (the 3rd operand) appears ONLY in the model's GENERATED working, never read.
	ctxGenOnly := []types.Thought{
		generatedThought(1, "The cap is 250, capped at that amount. Compute the total cost."),
		groundedReadThought(2, "manifest: units = 8"),
		groundedReadThought(3, "rate.yaml: hourly_rate = 40"),
	}
	if c := s.Fire(ctxGenOnly, cpyrand.New(7)); c != nil {
		t.Fatalf("solver must fire NOTHING when an operand is ungrounded (cap only in GENERATED), got %q", c.Text)
	}

	// Case 2: only TWO grounded reads exist for THREE declared operands — the 3rd cannot bind.
	ctxTooFew := []types.Thought{
		generatedThought(1, "Each unit bills at the rate, capped. Compute the total cost."),
		groundedReadThought(2, "manifest: units = 8"),
		groundedReadThought(3, "rate.yaml: hourly_rate = 40"),
	}
	if c := s.Fire(ctxTooFew, cpyrand.New(7)); c != nil {
		t.Fatalf("solver must fire NOTHING when fewer grounded reads than operands, got %q", c.Text)
	}
}

// TestSolverStaysDarkOnDeclineOrSelection: the conservative trigger must NOT fire on a decline/refusal or
// a selection/choice task even though numbers are present — over-firing there fabricates a confident
// number that scores 0 (a measurable harm, §6 over-routing). The veto is structural (solverDarkTriggers).
func TestSolverStaysDarkOnDeclineOrSelection(t *testing.T) {
	fm := &fakeFormalizer{expr: "min(a * b, c)", operands: []string{"a", "b", "c"}, ok: true}
	s := NewSolverPrimitiveSubAgent(fm, nil)

	decline := []types.Thought{
		generatedThought(1, "Should I answer this, or decline? It mentions 8 and 40 and a cap of 250."),
		groundedReadThought(2, "policy: cap = 250"),
		groundedReadThought(3, "doc: 8"),
	}
	if rel := s.Relevance(decline); rel != 0 {
		t.Fatalf("solver must stay dark on a decline task (relevance 0), got %g", rel)
	}
	if c := s.Fire(decline, cpyrand.New(7)); c != nil {
		t.Fatalf("solver must fire NOTHING on a decline task, got %q", c.Text)
	}

	selection := []types.Thought{
		generatedThought(1, "Which option is better, plan A capped at 8 or plan B capped at 40? Choose between them."),
		groundedReadThought(2, "planA: 8"),
		groundedReadThought(3, "planB: 40"),
	}
	if rel := s.Relevance(selection); rel != 0 {
		t.Fatalf("solver must stay dark on a selection task (relevance 0), got %g", rel)
	}
}

// TestSolverParseValidateRejectsMalformed: the AST parse-validate is the cheap-check half of the safety
// hook. It rejects (a) a numeric LITERAL anywhere in the shape (the model is forbidden from supplying a
// number), (b) malformed structure, (c) an unknown function name — in every case the specialist fires
// NOTHING. Driven directly through parseSolverExpr (the validator) and through Fire (the integration).
func TestSolverParseValidateRejectsMalformed(t *testing.T) {
	reject := []string{
		"min(a * b, 250)", // a smuggled literal — THE hard rule
		"8 * b",           // leading literal
		"a + ",            // dangling operator
		"min(a)",          // min needs >= 2 args
		"foo(a, b)",       // unknown function
		"(a + b",          // unbalanced paren
		"a b",             // two operands, no operator (trailing token)
		"",                // empty
		"42",              // a bare literal alone
	}
	for _, src := range reject {
		if _, _, err := parseSolverExpr(src); err == nil {
			t.Errorf("parseSolverExpr(%q) should reject (malformed / literal / unknown fn), but accepted", src)
		}
	}
	accept := map[string][]string{
		"min(a * b, c)": {"a", "b", "c"},
		"(a + b) * c":   {"a", "b", "c"},
		"a + b + c + d": {"a", "b", "c", "d"},
		"max(a, b, c)":  {"a", "b", "c"},
		"a / b":         {"a", "b"},
	}
	for src, want := range accept {
		_, names, err := parseSolverExpr(src)
		if err != nil {
			t.Errorf("parseSolverExpr(%q) should accept, got %v", src, err)
			continue
		}
		if len(names) != len(want) {
			t.Errorf("parseSolverExpr(%q) operands = %v, want %v", src, names, want)
		}
	}

	// Integration: a formalizer that smuggles a literal makes the specialist fire NOTHING even with
	// fully-grounded reads available.
	fm := &fakeFormalizer{expr: "min(a * b, 250)", operands: []string{"a", "b"}, ok: true}
	s := NewSolverPrimitiveSubAgent(fm, nil)
	ctx := []types.Thought{
		generatedThought(1, "compute the total cost, capped"),
		groundedReadThought(2, "units = 8"),
		groundedReadThought(3, "rate = 40"),
	}
	if c := s.Fire(ctx, cpyrand.New(7)); c != nil {
		t.Fatalf("solver must fire NOTHING when the shape smuggles a literal, got %q", c.Text)
	}
}

// TestSolverMechBench is the OFFLINE deterministic mechanism-bench (--backend test, zero noise floor):
// it drives the REAL mechanism over fixtures and asserts mechanism-CORRECTNESS, not an end-to-end lift.
// Critically it includes ADVERSARIAL MIS-BOUND fixtures (a wrong-operand / undeclared-operand case),
// asserting the grounded-operand hook REJECTS them — not just "computes correctly given correct inputs"
// (that trivial half dodges the actual risk). A fixture is {shape, declared operands, the grounded reads,
// the model "intent", expect-fire, and the exact expected value when it should fire}.
func TestSolverMechBench(t *testing.T) {
	type fixture struct {
		name      string
		expr      string
		operands  []string
		reads     []string // grounded read texts (each becomes an OBSERVATION thought)
		structure string   // the structural cue line (GENERATED) so the trigger fires
		wantFire  bool
		wantValue string
	}
	fixtures := []fixture{
		// --- correct half: grounded operands, well-formed shape, exact compute ---
		{"clamp_under_cap", "min(a * b, c)", []string{"a", "b", "c"},
			[]string{"units = 5", "rate = 30", "cap = 1000"}, "capped at the cap, compute the total cost", true, "150"},
		{"clamp_at_cap", "min(a * b, c)", []string{"a", "b", "c"},
			[]string{"units = 8", "rate = 40", "cap = 250"}, "no more than the cap; compute the total cost", true, "250"},
		{"chain_multiply", "(a + b) * c", []string{"a", "b", "c"},
			[]string{"x = 12", "y = 18", "z = 7"}, "then multiply the result; running total", true, "210"},
		{"ledger_fold", "a + b + c", []string{"a", "b", "c"},
			[]string{"credit = 100", "credit = 50", "debit = 25"}, "running balance ledger net total", true, "175"},
		{"exact_division", "a / b", []string{"a", "b"},
			[]string{"numerator = 1", "denominator = 4"}, "compute the total, whichever is smaller", true, "0.25"},

		// --- ADVERSARIAL MIS-BOUND half: the grounded-operand hook MUST reject (fire NOTHING) ---
		// (1) the model declares an operand the grounded reads cannot supply (3 operands, 2 reads).
		{"misbound_too_few_reads", "min(a * b, c)", []string{"a", "b", "c"},
			[]string{"units = 8", "rate = 40"}, "capped at the cap; compute the total cost", false, ""},
		// (2) the model's shape REFERENCES an operand it never declared (free operand 'z').
		{"misbound_undeclared_operand", "min(a * b, z)", []string{"a", "b", "c"},
			[]string{"units = 8", "rate = 40", "cap = 250"}, "capped; compute the total cost", false, ""},
		// (3) the model smuggles a LITERAL into the shape (the cap), bypassing the grounded read.
		{"misbound_literal_cap", "min(a * b, 250)", []string{"a", "b"},
			[]string{"units = 8", "rate = 40"}, "capped at 250; compute the total cost", false, ""},
		// (4) no grounded reads at all — every number lives only in the model's working.
		{"misbound_no_grounded_reads", "a + b", []string{"a", "b"},
			nil, "running total of 100 and 50; compute the total", false, ""},
	}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			fm := &fakeFormalizer{expr: fx.expr, operands: fx.operands, ok: true}
			s := NewSolverPrimitiveSubAgent(fm, nil)
			ctx := []types.Thought{generatedThought(1, fx.structure)}
			for i, r := range fx.reads {
				ctx = append(ctx, groundedReadThought(i+2, r))
			}
			c := s.Fire(ctx, cpyrand.New(7))
			if fx.wantFire {
				if c == nil {
					t.Fatalf("%s: solver should fire (grounded + well-formed), fired nothing", fx.name)
				}
				got := c.Payload.(*big.Rat)
				if got.Cmp(ratOf(t, fx.wantValue)) != 0 {
					t.Fatalf("%s: %s = %s, want %s", fx.name, fx.expr, formatRat(got), fx.wantValue)
				}
			} else if c != nil {
				t.Fatalf("%s: the grounded-operand hook must REJECT this mis-bound fixture (fire nothing), got %q",
					fx.name, c.Text)
			}
		})
	}
}

// TestSolverDarkWithoutFormalizer: with no StructureFormalizer wired (the test/offline posture, and the
// product posture on the test double), the specialist is DARK — relevance 0, fires nothing — even on a
// perfectly grounded clamp structure. This is the property that keeps the test backend byte-identical
// even with the opt-in knob ON: the specialist is present in the roster but silent.
func TestSolverDarkWithoutFormalizer(t *testing.T) {
	s := NewSolverPrimitiveSubAgent(nil, nil)
	ctx := []types.Thought{
		generatedThought(1, "capped at the cap; compute the total cost"),
		groundedReadThought(2, "units = 8"),
		groundedReadThought(3, "rate = 40"),
		groundedReadThought(4, "cap = 250"),
	}
	if rel := s.Relevance(ctx); rel != 0 {
		t.Fatalf("solver with nil formalizer must be dark (relevance 0), got %g", rel)
	}
	if c := s.Fire(ctx, cpyrand.New(7)); c != nil {
		t.Fatalf("solver with nil formalizer must fire nothing, got %q", c.Text)
	}
}

// TestSolverNotRegisteredWhenKnobOff is the default-OFF byte-identical guard at the roster level: with
// solverEnabled=false, DefaultPrimitiveSubAgents produces the SAME roster (no "solver" domain present); with
// it on, the solver IS appended (and only then). This pins the wiring: the knob default-OFF means the
// specialist is absent, so the pipeline is byte-identical to before.
func TestSolverNotRegisteredWhenKnobOff(t *testing.T) {
	hasSolver := func(roster []PrimitiveSubAgent) bool {
		for _, sp := range roster {
			if sp.Domain() == "solver" {
				return true
			}
		}
		return false
	}
	off := DefaultPrimitiveSubAgents(nil, nil, nil, nil, nil, nil, false)
	if hasSolver(off) {
		t.Fatal("solver_specialist OFF: the roster must NOT contain a solver specialist (byte-identical)")
	}
	on := DefaultPrimitiveSubAgents(nil, nil, nil, nil, nil, nil, true)
	if !hasSolver(on) {
		t.Fatal("solver_specialist ON: the roster must contain the solver specialist")
	}
	if len(on) != len(off)+1 {
		t.Fatalf("solver-on roster should be exactly one longer (just the solver appended): off=%d on=%d",
			len(off), len(on))
	}
}
