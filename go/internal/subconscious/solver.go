// solver.go — the 5th-axis CLASSICAL SOLVER specialist (PAL / Logic-LM "orchestrate-vs-compute"
// split, docs/internal/notes/2026-06-19-specialized-component-registry-axis.md §5). It is a TOOL-BACKED
// primitive in the M2 trichotomy sense (it carries real ground truth — an EXACT computed value — it
// does not opine), but its sub-problem comes from a model formalization (Pattern B) bounded by a hard
// safety hook. The cluster it targets is the MEASURED compute-class gap: multi-hop-then-multiply, a
// min()-cap clamp, a chain/ledger fold — structured-and-exact problems where the frontier model
// mis-COMPUTES even with a correct PLAN.
//
// The split (and the load-bearing safety boundary):
//
//   - FORMALIZE (Pattern B, the model): the LLM writes ONLY the EXPRESSION STRUCTURE — the
//     operators/shape with NAMED operand placeholders (min(a*b, c), a+b+c, a chain) — and NEVER a
//     literal number. A bare numeric literal anywhere in the expression is a HARD parse-validate REJECT
//     (the model is not allowed to smuggle a number in; if it could, this would be a confident-wrong-
//     answer generator — the FATAL failure the red-team named).
//   - BIND (the grounded-operand safety hook): every named operand must bind to a GROUNDED READ — a
//     number that appears in an OBSERVATION-sourced thought (reality imported through the watched seam)
//     or an INJECTED read/search tool-result thought in the live context. If ANY operand cannot be
//     traced to such a grounded read, the specialist fires NOTHING (returns nil) — never a number with
//     an ungrounded input. This converts "trust the LLM's formula" into "every operand traces to a real
//     read", which the grounding-isolation gate (bench/runner.GroundingReadHappened) can then witness.
//   - COMPUTE (pure control, deterministic): a math/big rational evaluator computes the AST exactly. It
//     cannot hallucinate the solve — only the formalization, which is cheap-checked above.
//
// We deliberately do NOT add a Watched-seam "reality re-check" of the computed value (the red-team's
// dropped mitigation): no external tool re-derives a computed arithmetic value, so an offline re-check
// would fabricate "checks out". The grounded-operand hook is the real substitute. The Filter is NOT
// relied on either — it checks output-vs-history, never the input formalization.
//
// Distinct from ComputePrimitiveSubAgent (domain "compute"): ComputePrimitiveSubAgent owns the bare binary
// `\d+ op \d+` regex at relevance 0.95 and makes NO model call. SolverPrimitiveSubAgent (domain "solver")
// fires on a CONSERVATIVE multi-step / clamp / chain / ledger STRUCTURE, makes one Pattern-B
// formalization call, and stays DARK on decline/selection/lookup/CSP shapes (over-firing on a decline
// task is a confabulation that scores 0 = measurable harm).
package subconscious

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// solverStructureTriggers are the CONSERVATIVE structural cues for the compute-class gap: a clamp
// (min/max/cap), a multi-step chain, or a ledger/running-total fold — shapes whose ANSWER is a
// computed value. They are NOT bare arithmetic keywords (those belong to ComputePrimitiveSubAgent) and NOT
// decline/selection/lookup/CSP cues (the solver must stay dark there — see solverDarkTriggers).
var solverStructureTriggers = []string{
	"capped at", "cap of", "no more than", "at most", "up to a maximum",
	"whichever is smaller", "whichever is lower", "whichever is less",
	"whichever is larger", "whichever is greater", "whichever is more",
	"clamp", "clamped",
	"running total", "running balance", "ledger", "net total", "grand total",
	"then multiply", "then multiplied", "multiply the result", "then divide the result",
	"total cost", "compute the total", "work out the total",
}

// solverDarkTriggers are the shapes the solver must NOT fire on even if a structural cue brushes them:
// a decline/refusal task, a selection/choice task, a pure lookup, or an ordering/constraint (CSP)
// problem. Over-firing on a decline task fabricates a confident number that scores 0 — a measurable
// harm, not a missed opportunity (§6 over-routing risk). A dark cue VETOES the fire outright.
var solverDarkTriggers = []string{
	"decline", "refuse", "should i", "do not answer", "don't answer", "cannot answer",
	"which option", "choose between", "pick the", "select the best", "which is better",
	"who is", "what is the capital", "define ", "look up", "recall whether",
	"order them", "in what order", "arrange", "schedule the", "constraint", "satisfies all",
}

// numberRe matches a standalone non-negative number (integer or decimal) in grounded text. It is
// deliberately word-boundaried so "8443" in a port and "12.5" in a ledger both read, but a substring of
// a larger token does not. The solver only binds operands from numbers found in GROUNDED thoughts.
var numberRe = regexp.MustCompile(`(?:^|[^\w.])(\d+(?:\.\d+)?)`)

// SolverPrimitiveSubAgent is the 5th-axis classical arithmetic solver specialist (domain "solver"). It is
// constructed only when the opt-in subconscious.solver_specialist knob is on (DefaultPrimitiveSubAgents),
// and it is DARK (never fires) unless a real StructureFormalizer model backend is wired — the test
// double does NOT implement StructureFormalizer, so on the test backend the specialist is present
// (when the knob is on) but silent, keeping goldens byte-identical. Pattern B owns the SHAPE; the
// engine owns the grounded-operand bind + the exact math/big compute + the control/gate around it.
type SolverPrimitiveSubAgent struct {
	formalizer backends.StructureFormalizer // the Pattern-B shape-writer (nil ⇒ dark — no canned stand-in)
	emit       events.Emit                  // optional audit hook (subconscious.solver_formalize); nil ⇒ silent
}

// NewSolverPrimitiveSubAgent builds the solver specialist over the Pattern-B formalizer port. A nil formalizer
// is a dark specialist (it never fires): a solve with no model-written shape would mean the engine
// invented the structure, which it must not. emit may be nil (silent — the dispatch loop still emits
// subconscious.fire for the candidate; this is the formalize-detail audit the dispatch event can't carry).
func NewSolverPrimitiveSubAgent(formalizer backends.StructureFormalizer, emit events.Emit) *SolverPrimitiveSubAgent {
	return &SolverPrimitiveSubAgent{formalizer: formalizer, emit: emit}
}

func (*SolverPrimitiveSubAgent) Domain() string { return "solver" }

// solverShaped reports whether the context is the conservative compute-class STRUCTURE the solver
// targets: it must (i) carry a structural cue, (ii) NOT carry a dark (decline/selection/lookup/CSP)
// cue, and (iii) have at least TWO numbers somewhere in context (a single-number problem is not a
// multi-operand structure — that is ComputePrimitiveSubAgent's or a plain lookup's job). This is the FLOOR
// trigger; it is flat, not learned (§3.2 "flat beats learned routing").
func solverShaped(ctx []types.Thought) bool {
	text := ctxTextDefault(ctx)
	if hasAny(text, solverDarkTriggers) {
		return false // a decline/selection/lookup/CSP cue VETOES — stay dark (over-firing = harm)
	}
	if !hasAny(text, solverStructureTriggers) {
		return false
	}
	// at least two numbers present (a multi-operand structured problem, not a single value)
	return len(numberRe.FindAllString(text, -1)) >= 2
}

func (s *SolverPrimitiveSubAgent) Relevance(ctx []types.Thought) float64 {
	if s.formalizer == nil { // no model ⇒ no model-written shape ⇒ dark (never invent the structure)
		return 0.0
	}
	if !solverShaped(ctx) {
		return 0.0
	}
	// Fire BELOW ComputePrimitiveSubAgent's 0.95 so a bare binary expression is owned by the cheaper compute
	// primitive; the solver claims the multi-step/clamp/ledger structures compute can't.
	return 0.82
}

// Fire runs the formalize -> validate -> bind -> compute pipeline. It returns nil (fires NOTHING) at
// every safety gate: the model declined, the expression contains a literal or is malformed, an operand
// could not be traced to a grounded read, or the compute is undefined (division by zero). It NEVER
// emits a number with an ungrounded or invented input.
func (s *SolverPrimitiveSubAgent) Fire(ctx []types.Thought, _ *cpyrand.Random) *types.Candidate {
	if s.formalizer == nil || !solverShaped(ctx) {
		return nil
	}
	// (1) FORMALIZE (Pattern B): the model writes the expression SHAPE + the ordered operand names.
	expr, operands, ok := s.formalizer.FormalizeExpression(ctx)
	if !ok || strings.TrimSpace(expr) == "" {
		return nil // declined / no shape ⇒ fire nothing (no deterministic stand-in)
	}
	// (2) AST PARSE-VALIDATE: well-formedness + the HARD no-literal rule. A bare numeric literal anywhere
	// in the expression is a reject (the model is forbidden from supplying a number). Returns the set of
	// operand names the expression actually references.
	ast, refNames, err := parseSolverExpr(expr)
	if err != nil {
		return nil // malformed shape or a smuggled literal ⇒ fire nothing
	}
	// (3) BIND — the grounded-operand safety hook. Collect the numbers that appear in GROUNDED thoughts
	// (OBSERVATION / read-result), in order of appearance, then bind the declared operands positionally.
	grounded := groundedNumbers(ctx)
	ratBind, binding, sources, ok := bindOperands(operands, refNames, grounded)
	if !ok {
		return nil // an operand could not be traced to a grounded read ⇒ fire NOTHING (the safety hook)
	}
	// (4) COMPUTE (deterministic, exact). math/big rationals: division, min/max, chains are all exact.
	val, err := evalSolverAST(ast, ratBind)
	if err != nil {
		return nil // undefined compute (e.g. division by zero) ⇒ fire nothing, never manufacture a value
	}
	valS := formatRat(val)
	// (5) AUDIT — emit subconscious.solver_formalize carrying the shape, the bound operands, and the
	// grounded source each operand traced to (observability: a wrong-but-exact answer is traceable to the
	// reads it was built from).
	if s.emit != nil {
		s.emit(events.SubSolverFormalize, "solver formalized "+expr+" = "+valS, events.D{
			"expr":    expr,
			"bound":   binding,
			"sources": sources,
			"value":   valS,
		})
	}
	return cand("solver", expr+" = "+valS, 0.82, withOperator(types.VALIDATE), withPayload(val))
}

// ============================================================================
// grounded-operand binding (the safety hook)
// ============================================================================

// groundedNumber is one number traced to a grounded read: its value (as a *big.Rat for exact compute)
// and the grounded thought it came from (for the audit + the GroundingReadHappened witness story).
type groundedNumber struct {
	rat    *big.Rat
	source string // a short reference to the grounded thought (its source-class + a text clip)
}

// groundedNumbers extracts, in order of appearance, every number that lives in a GROUNDED thought —
// one whose Source is OBSERVATION/PERCEPT (reality imported through the watched seam) OR an INJECTED
// candidate from a read/search tool-result (a real read of an artifact). A number that appears ONLY in
// a GENERATED (the model's own working) or USER_INPUT thought is NOT grounded for this purpose: the
// operand must trace to a real READ, not to the model's restatement or the raw prompt. This is the
// precise in-context witness of req #1 — an operand binds only to a number a real read imported.
func groundedNumbers(ctx []types.Thought) []groundedNumber {
	var out []groundedNumber
	for _, t := range ctx {
		if !isGroundedReadThought(t) {
			continue
		}
		for _, m := range numberRe.FindAllStringSubmatch(t.Text, -1) {
			r := new(big.Rat)
			if _, ok := r.SetString(m[1]); !ok {
				continue
			}
			out = append(out, groundedNumber{
				rat:    r,
				source: t.Source.String() + ":" + clipRunes(strings.TrimSpace(t.Text), 32),
			})
		}
	}
	return out
}

// isGroundedReadThought reports whether a thought is a grounded read — reality imported, not the model's
// own restatement. OBSERVATION/PERCEPT are reality feedback through the watched seam (the canonical
// grounded read). An INJECTED thought whose payload is an action.ToolResult is a read/search/run result
// (also a real read of an artifact). Everything else (GENERATED working, raw USER_INPUT, METACOG) is NOT
// a grounded read for operand binding — the operand must trace to a read, never to a prior-only number.
func isGroundedReadThought(t types.Thought) bool {
	if t.Source == types.OBSERVATION || t.Source == types.PERCEPT {
		return true
	}
	if t.Source == types.INJECTED && carriesToolResult(t.RawReturn) {
		return true
	}
	return false
}

// carriesToolResult reports whether a thought's raw payload is a real tool result (a read/search/run
// observation). The tool-backed read/search/run primitives stamp their candidate Payload with the
// genuine action.ToolResult (primitive_subagent.go newReadPrimitive et al., withPayload(result)); when that
// candidate is voiced into the graph the payload rides on the thought's RawReturn. Recognising it
// (value or pointer) is what marks the operand's source as a real artifact read, not a prior-only number.
func carriesToolResult(payload any) bool {
	switch payload.(type) {
	case action.ToolResult, *action.ToolResult:
		return true
	}
	return false
}

// bindOperands is the grounded-operand bind: it assigns each DECLARED operand name (in the formalizer's
// declared order) to the next available grounded number (in order of appearance), then verifies that
// EVERY operand the expression references was bound. It rejects (ok=false, fire NOTHING) when:
//
//   - the expression references an operand the formalizer did not declare (unknown operand);
//   - there are FEWER grounded numbers than declared operands (an operand has no read to trace to —
//     the adversarial mis-bound case the mechanism-bench asserts);
//   - a declared operand the expression actually uses ends up unbound.
//
// It returns the operand->value rational binding (for the exact compute), the operand->decimal-string
// binding (for the audit), and the operand->grounded-source references (the read each operand traced to).
func bindOperands(declared, refNames []string, grounded []groundedNumber) (
	ratBind map[string]*big.Rat, binding map[string]string, sources map[string]string, ok bool) {

	refSet := map[string]bool{}
	for _, n := range refNames {
		refSet[n] = true
	}
	// Every operand the expression USES must be among the declared names (no free/undeclared operand).
	declaredSet := map[string]bool{}
	for _, n := range declared {
		declaredSet[n] = true
	}
	for n := range refSet {
		if !declaredSet[n] {
			return nil, nil, nil, false // the expression references an operand the model did not declare
		}
	}
	// Positional bind: declared operand[i] <- grounded number[i]. FEWER grounded numbers than declared
	// operands ⇒ an operand has no grounded read to trace to ⇒ reject (the safety hook / mis-bound case).
	if len(grounded) < len(declared) {
		return nil, nil, nil, false
	}
	ratBind = map[string]*big.Rat{}
	binding = map[string]string{}
	sources = map[string]string{}
	for i, name := range declared {
		gn := grounded[i]
		ratBind[name] = gn.rat
		binding[name] = formatRat(gn.rat)
		sources[name] = gn.source
	}
	// Verify every REFERENCED operand resolved to a grounded number (defence in depth: a declared-but-
	// unbound operand the expression uses is a reject).
	for n := range refSet {
		if ratBind[n] == nil {
			return nil, nil, nil, false
		}
	}
	return ratBind, binding, sources, true
}

// ============================================================================
// the AST: parse-validate + exact math/big evaluation
// ============================================================================

// solverNode is one node of the validated expression AST. Exactly one of {fn, op, operand} is set per
// node (a tiny tagged shape, no interface needed for this bounded grammar).
type solverNode struct {
	kind     string        // "operand" | "binop" | "fn"
	operand  string        // kind=="operand": the named placeholder
	op       byte          // kind=="binop": one of + - * /
	fn       string        // kind=="fn": "min" | "max"
	children []*solverNode // binop: 2; fn: >=2
}

// parseSolverExpr parses+validates the expression structure and returns the AST + the sorted set of
// operand names it references. The grammar is a bounded arithmetic-with-clamp language:
//
//	expr   := term (('+' | '-') term)*
//	term   := factor (('*' | '/') factor)*
//	factor := IDENT | '(' expr ')' | ('min' | 'max') '(' expr (',' expr)+ ')'
//
// THE HARD RULE: a bare numeric LITERAL is NOT in the grammar — a factor is an IDENT, a parenthesised
// expr, or a min/max call. A digit where a factor is expected is a parse error (the model is forbidden
// from supplying a number). An unknown function name is a parse error. Trailing tokens are a parse error.
func parseSolverExpr(src string) (*solverNode, []string, error) {
	p := &solverParser{toks: tokenizeSolver(src)}
	node, err := p.parseExpr()
	if err != nil {
		return nil, nil, err
	}
	if p.pos != len(p.toks) {
		return nil, nil, fmt.Errorf("solver: trailing tokens after expression at %d", p.pos)
	}
	names := map[string]bool{}
	collectOperands(node, names)
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("solver: expression has no operands")
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	return node, sorted, nil
}

func collectOperands(n *solverNode, into map[string]bool) {
	if n == nil {
		return
	}
	if n.kind == "operand" {
		into[n.operand] = true
		return
	}
	for _, c := range n.children {
		collectOperands(c, into)
	}
}

// solverToken is one lexed token: a kind and (for ident/punct) its literal text.
type solverToken struct {
	kind string // "ident" | "num" | "(" | ")" | "," | "op"
	text string
}

// solverIdentRe matches an identifier (operand name or a function name): a letter then word chars.
var solverIdentRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*`)
var solverNumRe = regexp.MustCompile(`^\d+(?:\.\d+)?`)

// tokenizeSolver lexes the expression. A NUMBER token is intentionally lexed (not skipped) so the parser
// can REJECT it loudly with a "literal not allowed" error rather than silently dropping it.
func tokenizeSolver(src string) []solverToken {
	var toks []solverToken
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' || c == ')' || c == ',':
			toks = append(toks, solverToken{kind: string(c)})
			i++
		case c == '+' || c == '-' || c == '*' || c == '/' || c == '×':
			sym := string(c)
			if c == '×' { // multi-byte rune; advance by its byte length below
				sym = "*"
			}
			toks = append(toks, solverToken{kind: "op", text: sym})
			if c == '×' {
				i += len("×")
			} else {
				i++
			}
		case solverNumRe.MatchString(src[i:]):
			m := solverNumRe.FindString(src[i:])
			toks = append(toks, solverToken{kind: "num", text: m})
			i += len(m)
		case solverIdentRe.MatchString(src[i:]):
			m := solverIdentRe.FindString(src[i:])
			toks = append(toks, solverToken{kind: "ident", text: m})
			i += len(m)
		default:
			// an unrecognised byte becomes an "unknown" token the parser rejects.
			toks = append(toks, solverToken{kind: "unknown", text: string(c)})
			i++
		}
	}
	return toks
}

type solverParser struct {
	toks []solverToken
	pos  int
}

func (p *solverParser) peek() (solverToken, bool) {
	if p.pos >= len(p.toks) {
		return solverToken{}, false
	}
	return p.toks[p.pos], true
}

func (p *solverParser) parseExpr() (*solverNode, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != "op" || (t.text != "+" && t.text != "-") {
			return left, nil
		}
		p.pos++
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		left = &solverNode{kind: "binop", op: t.text[0], children: []*solverNode{left, right}}
	}
}

func (p *solverParser) parseTerm() (*solverNode, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != "op" || (t.text != "*" && t.text != "/") {
			return left, nil
		}
		p.pos++
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		left = &solverNode{kind: "binop", op: t.text[0], children: []*solverNode{left, right}}
	}
}

func (p *solverParser) parseFactor() (*solverNode, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("solver: unexpected end of expression")
	}
	switch t.kind {
	case "num":
		// THE HARD RULE: a numeric literal is forbidden — the model must never supply a number.
		return nil, fmt.Errorf("solver: numeric literal %q not allowed (operands must be grounded reads)", t.text)
	case "(":
		p.pos++
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if c, ok := p.peek(); !ok || c.kind != ")" {
			return nil, fmt.Errorf("solver: missing closing paren")
		}
		p.pos++
		return inner, nil
	case "ident":
		// a function call (min/max) or a plain operand
		name := strings.ToLower(t.text)
		if name == "min" || name == "max" {
			p.pos++
			if c, ok := p.peek(); !ok || c.kind != "(" {
				return nil, fmt.Errorf("solver: %s must be followed by (", name)
			}
			p.pos++
			args, err := p.parseArgs()
			if err != nil {
				return nil, err
			}
			if len(args) < 2 {
				return nil, fmt.Errorf("solver: %s needs at least 2 arguments", name)
			}
			return &solverNode{kind: "fn", fn: name, children: args}, nil
		}
		// a bare ident that is NOT a known function and IS followed by "(" is an unknown function — reject.
		p.pos++
		if c, ok := p.peek(); ok && c.kind == "(" {
			return nil, fmt.Errorf("solver: unknown function %q", t.text)
		}
		return &solverNode{kind: "operand", operand: t.text}, nil
	default:
		return nil, fmt.Errorf("solver: unexpected token %q", t.kind+t.text)
	}
}

func (p *solverParser) parseArgs() ([]*solverNode, error) {
	var args []*solverNode
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		t, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("solver: unterminated argument list")
		}
		if t.kind == "," {
			p.pos++
			continue
		}
		if t.kind == ")" {
			p.pos++
			return args, nil
		}
		return nil, fmt.Errorf("solver: expected , or ) in argument list, got %q", t.kind+t.text)
	}
}

// evalSolverAST computes the validated AST against the operand bindings, exactly, with math/big
// rationals. A division by zero (or an unbound operand, which bindOperands has already excluded) is an
// error ⇒ the specialist fires nothing rather than manufacture a value.
func evalSolverAST(n *solverNode, binding map[string]*big.Rat) (*big.Rat, error) {
	switch n.kind {
	case "operand":
		v, ok := binding[n.operand]
		if !ok || v == nil {
			return nil, fmt.Errorf("solver: operand %q is unbound", n.operand)
		}
		return new(big.Rat).Set(v), nil
	case "binop":
		l, err := evalSolverAST(n.children[0], binding)
		if err != nil {
			return nil, err
		}
		r, err := evalSolverAST(n.children[1], binding)
		if err != nil {
			return nil, err
		}
		out := new(big.Rat)
		switch n.op {
		case '+':
			return out.Add(l, r), nil
		case '-':
			return out.Sub(l, r), nil
		case '*':
			return out.Mul(l, r), nil
		case '/':
			if r.Sign() == 0 {
				return nil, fmt.Errorf("solver: division by zero")
			}
			return out.Quo(l, r), nil
		}
		return nil, fmt.Errorf("solver: unknown operator %q", string(n.op))
	case "fn":
		vals := make([]*big.Rat, 0, len(n.children))
		for _, c := range n.children {
			v, err := evalSolverAST(c, binding)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		best := new(big.Rat).Set(vals[0])
		for _, v := range vals[1:] {
			if n.fn == "min" && v.Cmp(best) < 0 {
				best.Set(v)
			}
			if n.fn == "max" && v.Cmp(best) > 0 {
				best.Set(v)
			}
		}
		return best, nil
	}
	return nil, fmt.Errorf("solver: unknown node kind %q", n.kind)
}

// formatRat renders an exact rational as a clean decimal: an integer with no fractional part prints as
// an integer; otherwise it prints the shortest exact-or-rounded decimal (math/big FloatString trims
// trailing zeros via a final cleanup). Mirrors the spirit of formatArith for the compute primitive.
func formatRat(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	// exact decimal if it terminates, else a 6-place rounding (the operands are read values; 6 places is
	// ample for the clamp/chain/ledger cluster). Trim trailing zeros + a dangling point.
	s := r.FloatString(6)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}
