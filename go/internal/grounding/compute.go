// Package grounding holds the reality-grounding evaluators of the SR-4 spine: a claim is grounded
// against an actual source of truth, never against the closed loop itself. compute.go is grounding
// layer 2a (tracker N.1b) — INTERNAL SIMULATION by deterministic arithmetic. A computable claim ("12 *
// 31 = 372") is resolved by evaluating it, not by asking the model whether it looks right; a wrong
// computation ("2 + 2 = 5") is REFUTED. This is a tier-deterministic grounding source (it outranks
// firsthand-observation and testimony in the trust order, second only to a real validated experiment):
// math doesn't lie, so when a claim is arithmetic the answer is not a matter of opinion.
//
// Deterministic, offline, no model — so it is a reliable ground truth the grounding loop (N.1) and the
// experiment memory (N.1e) can trust. Scope is real arithmetic (+ - * / ^, parens, an = / != claim);
// units and symbolic algebra are later extensions. Anything it cannot parse as arithmetic returns
// NotComputable, deferring to another grounding layer — it never guesses.
package grounding

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Verdict is the grounding outcome of the compute evaluator.
type Verdict int

const (
	// NotComputable: the claim is not arithmetic — this layer has no opinion, defer to another source.
	NotComputable Verdict = iota
	// Grounded: the claim is computably TRUE (an equality that holds, or a bare expression that evaluates).
	Grounded
	// Refuted: the claim is computably FALSE (an equality that does not hold).
	Refuted
)

func (v Verdict) String() string {
	switch v {
	case Grounded:
		return "grounded"
	case Refuted:
		return "refuted"
	default:
		return "not-computable"
	}
}

// Result is the compute evaluator's grounding verdict plus the numbers behind it (for the trace).
type Result struct {
	Verdict  Verdict
	Computed float64 // the evaluated left-hand side (the value reality computes)
	Claimed  float64 // the claimed right-hand side (for an equality; else == Computed)
	HasClaim bool    // whether the claim was an equality (LHS compared to RHS) vs a bare expression
	Claim    string  // the extracted, ASCII-normalized arithmetic span ("12 * 31 = 372") — the ledger key
	Detail   string
}

// mathGlyphs normalizes the unambiguous non-ASCII math glyphs (× multiply, ÷ divide, − the true minus
// sign) to the ASCII operators the parser understands, so a claim voiced with the typographic forms a
// model or the UI uses still grounds. ASCII claims pass through unchanged. Ambiguous glyphs (the middle
// dot ·, em dash —) are deliberately NOT mapped — they are separators/punctuation far more often than
// operators, and a spurious mapping would manufacture a false "computable" claim.
var mathGlyphs = strings.NewReplacer("×", "*", "÷", "/", "−", "-")

// equality matches an arithmetic equality/inequality embedded in text: two arithmetic spans around an
// =, ==, !=, or ≠ comparator. The spans allow digits, decimal points, the four operators, ^, and
// parentheses/spaces. The first such occurrence is grounded.
var equality = regexp.MustCompile(`([0-9.\s()+\-*/^]+?)\s*(==|!=|≠|=)\s*([0-9.\s()+\-*/^]+)`)

// bareExpr matches a whole claim that is itself a pure arithmetic expression (no comparator).
var bareExpr = regexp.MustCompile(`^[0-9.\s()+\-*/^]+$`)

// compareTol is the float tolerance: a claim is grounded if |computed-claimed| <= max(abs, rel*scale).
const compareTol = 1e-9

// EvaluateCompute grounds a claim by deterministic arithmetic. It (1) extracts an equality/inequality
// span if present and grounds it (Grounded iff both sides evaluate equal within tolerance, inverted for
// != / ≠); else (2) evaluates a whole-claim bare expression as a pure computation (Grounded with the
// value); else (3) returns NotComputable. Never errors — an unparseable arithmetic span just falls
// through to NotComputable.
func EvaluateCompute(claim string) Result {
	claim = mathGlyphs.Replace(strings.TrimSpace(claim))
	// A symbolic equation ("x + 1 = 5") is not arithmetic — a letter sitting next to a math operator is
	// a VARIABLE with no value, so this layer must defer (never extract a spurious "+ 1 = 5"). Prose
	// around a real computation ("the answer is 50/2=25") has no operator-adjacent letter, so it still
	// extracts cleanly.
	if hasMathVariable(claim) {
		return Result{Verdict: NotComputable, Detail: "symbolic (contains a variable), not arithmetic"}
	}
	if m := equality.FindStringSubmatch(claim); m != nil {
		op := m[2]
		lhs, okL := evalExpr(m[1])
		rhs, okR := evalExpr(m[3])
		if okL && okR {
			eq := nearlyEqual(lhs, rhs)
			grounded := eq
			if op == "!=" || op == "≠" {
				grounded = !eq
			}
			v := Refuted
			if grounded {
				v = Grounded
			}
			span := cleanSpan(m[1]) + " " + op + " " + cleanSpan(m[3])
			return Result{
				Verdict: v, Computed: lhs, Claimed: rhs, HasClaim: true, Claim: span,
				Detail: span + " -> " + trimFloat(lhs) + " vs " + trimFloat(rhs),
			}
		}
	}
	if bareExpr.MatchString(claim) {
		if v, ok := evalExpr(claim); ok {
			return Result{Verdict: Grounded, Computed: v, Claimed: v, Claim: cleanSpan(claim),
				Detail: claim + " = " + trimFloat(v)}
		}
	}
	return Result{Verdict: NotComputable, Detail: "no arithmetic claim to ground"}
}

// hasMathVariable reports whether any letter in the claim is directly adjacent (ignoring spaces) to a
// math operator (+ - * / ^ =) — the signature of a variable in an equation. Prose words near a
// computation are separated from the operators by numbers/whitespace, so they don't trip this.
func hasMathVariable(s string) bool {
	r := []rune(s)
	isOp := func(c rune) bool { return strings.ContainsRune("+-*/^=", c) }
	isLetter := func(c rune) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
	nonSpace := func(idx, dir int) rune {
		for i := idx + dir; i >= 0 && i < len(r); i += dir {
			if r[i] != ' ' && r[i] != '\t' {
				return r[i]
			}
		}
		return 0
	}
	for i, c := range r {
		if isLetter(c) && (isOp(nonSpace(i, +1)) || isOp(nonSpace(i, -1))) {
			return true
		}
	}
	return false
}

// nearlyEqual compares two floats with an absolute + relative tolerance (so 0.1+0.2 == 0.3 grounds and
// large numbers aren't tripped by representation error).
func nearlyEqual(a, b float64) bool {
	diff := math.Abs(a - b)
	if diff <= compareTol {
		return true
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	// Relative slack is compareTol*scale (rel epsilon = compareTol = 1e-9), per the line-70 contract
	// max(abs, rel*scale). The earlier *1e3 inflated this to a 1e-6 relative epsilon, which at scale
	// >=1e6 exceeds 1.0 and stamped off-by-one INTEGER claims (e.g. 1000000 = 1000001) as Grounded —
	// a false positive in the highest-trust deterministic tier that real observation cannot then
	// refute. Keep it at the float-representation floor so genuine rounding grounds but wrong arithmetic does not.
	return diff <= compareTol*scale
}

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// cleanSpan trims surrounding whitespace and trailing sentence punctuation (a "." closing the sentence
// the claim sat in) from an extracted arithmetic span, so the ledger key is the bare claim ("12 * 31 =
// 372", not "372.") — a trailing dot is punctuation here, never a decimal point (a real decimal has a
// digit after it). Leading/internal dots (decimals) are untouched.
func cleanSpan(s string) string { return strings.TrimRight(strings.TrimSpace(s), ".") }

// ---- a tiny recursive-descent arithmetic evaluator (+ - * / ^, unary -, parens) ----
//
// Grammar:  expr := term (('+'|'-') term)*
//           term := power (('*'|'/') power)*
//           power := unary ('^' power)?        (right-assoc)
//           unary := '-' unary | primary
//           primary := number | '(' expr ')'

type parser struct {
	src []rune
	pos int
	err bool
}

// evalExpr evaluates a single arithmetic expression, returning (value, ok). ok=false on any parse
// error, an empty expression, division by zero, or trailing garbage.
func evalExpr(s string) (float64, bool) {
	p := &parser{src: []rune(strings.TrimSpace(s))}
	if len(p.src) == 0 {
		return 0, false
	}
	v := p.expr()
	if p.err {
		return 0, false
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return 0, false // trailing garbage
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) peek() rune {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) expr() float64 {
	v := p.term()
	for {
		switch p.peek() {
		case '+':
			p.pos++
			v += p.term()
		case '-':
			p.pos++
			v -= p.term()
		default:
			return v
		}
	}
}

func (p *parser) term() float64 {
	v := p.power()
	for {
		switch p.peek() {
		case '*':
			p.pos++
			v *= p.power()
		case '/':
			p.pos++
			d := p.power()
			if d == 0 {
				p.err = true
				return 0
			}
			v /= d
		default:
			return v
		}
	}
}

func (p *parser) power() float64 {
	base := p.unary()
	if p.peek() == '^' {
		p.pos++
		return math.Pow(base, p.power()) // right-associative
	}
	return base
}

func (p *parser) unary() float64 {
	if p.peek() == '-' {
		p.pos++
		return -p.unary()
	}
	if p.peek() == '+' {
		p.pos++
		return p.unary()
	}
	return p.primary()
}

func (p *parser) primary() float64 {
	if p.peek() == '(' {
		p.pos++
		v := p.expr()
		if p.peek() != ')' {
			p.err = true
			return 0
		}
		p.pos++
		return v
	}
	return p.number()
}

func (p *parser) number() float64 {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if (c >= '0' && c <= '9') || c == '.' {
			p.pos++
		} else {
			break
		}
	}
	if p.pos == start {
		p.err = true
		return 0
	}
	v, err := strconv.ParseFloat(string(p.src[start:p.pos]), 64)
	if err != nil {
		p.err = true
		return 0
	}
	return v
}
