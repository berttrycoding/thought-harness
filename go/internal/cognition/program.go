// program.go ports program.py — a workflow is a PROGRAM, not a fixed list.
//
// The spec calls a workflow "a learned composition of operators into phased sequences." But real
// control flow is richer than a sequence: operators compose in series, in parallel (fan-out), and
// in bounded loops (refine-until-good) — like a program. This file is that structure:
//
//	Step   one operator applied by a runtime sub-agent (operator + rough domain)
//	Seq    do children in order
//	Par    do children at once (fan-out — independent sub-agents)
//	Loop   repeat a body until a condition, bounded by max_iter (always terminating)
//
// A Program is a tree of these. It is constructed on the fly by the synthesiser (synth.go) from the
// context — not hand-written — and verified before it is trusted (the two-layer discipline:
// structure is checked — known/verified operators, bounded loops, bounded size — before execution).
// ToDict serialises the whole structure so every synthesised program is logged as data we can later
// standardise and train on.
//
// Schedule linearises the tree into phase-groups the tick-based engine can run: a parallel group
// becomes one phase that instantiates several sub-agents; a loop is unrolled to its bound.
//
// HARD PORT #2 (program tree). Python's Node = Step | Seq | Par | Loop union, walked by isinstance
// everywhere. The Go port is a sealed interface (unexported marker method node()) implemented by the
// four struct types; every isinstance ladder becomes a type switch over the closed set. ToDict /
// NodeFromDict encode/decode keyed on a "kind" discriminator — NodeFromDict is also the parser for an
// LLM-written program tree, so an unknown kind (or a malformed node) returns an error, never a panic.
package cognition

import (
	"os"
	"strconv"
	"strings"
)

// Structural bounds — a synthesised program past these is rejected as runaway. Mirror the Python
// module-level constants.
const (
	MaxSteps = 24 // total operator steps across the whole tree
	MaxDepth = 6  // nesting depth of the Node tree
	MaxIter  = 6  // a loop's max_iter ceiling
	// MaxScheduledPhases bounds the FULLY-UNROLLED phase count (what Program.Schedule() emits, where
	// every phase is a sub-agent firing = a model call under --backend llm). MaxSteps/MaxDepth/MaxIter
	// bound the STATIC tree, but NOT the multiplicative product of NESTED loops: a 1-step body wrapped
	// in 5 loops of max_iter=6 passes all three static bounds (1 step, depth 6) yet schedules 6^5 =
	// 7776 phases. This cap rejects that runaway. Set to MaxSteps*MaxIter (144) — generous enough for
	// any legitimate program (a single max-iter loop over the whole step budget) while killing the
	// nested-loop blowup that would otherwise hammer the model and break the bounded-loop invariant.
	MaxScheduledPhases = MaxSteps * MaxIter
)

// MaxParWidth is the bounded parallel fan-out width. NOTE (re-modelled): this is NOT a durability
// bound. The branching ratio n is measured as recursive FORKS (regulator), which a parallel fan-out
// does not drive — w candidates collapse to one gate winner and fork only on conflict, so durability
// (n<1) is independent of w. So MaxParWidth is a schedulability / compute budget: how many sub-agent
// calls a single tick may launch. Aligned with the regulator's focus_capacity (8) and overridable via
// env so it scales with the host's concurrency. (See docs/reference/stability-dynamic-dimensionality.md.)
//
// Python reads THOUGHT_MAX_PAR_WIDTH at import time (int(os.environ.get(..., "8"))). Go mirrors that
// with a package-level var resolved once at init; a missing/garbage value falls back to the default 8
// (Python's int(...) would raise on garbage, but a non-numeric override is operator error and the safe
// reproduction is the documented default, not a panic in a core package).
var MaxParWidth = resolveMaxParWidth()

// resolveMaxParWidth reads THOUGHT_MAX_PAR_WIDTH once, defaulting to 8.
func resolveMaxParWidth() int {
	if v, ok := os.LookupEnv("THOUGHT_MAX_PAR_WIDTH"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 8
}

// Node is the sealed interface for a program-tree node. The unexported marker method node() seals the
// set to the four implementors in this file (Step/Seq/Par/Loop) — the Go expression of Python's
// closed Node union. Every walk over a node is a type switch over exactly these four.
type Node interface {
	node()
	// toDict encodes this node to the "kind"-discriminated map (the serialised form). Exposed via the
	// package-level NodeToDict / Program.ToDict.
	toDict() map[string]any
}

// RoleSkill / RoleScript are a Step's typed ROLE (P3.4 — the unified node, script half): a SKILL step is
// agentic (a sub-agent reasons, may call the model + dispatch tools); a SCRIPT step is DETERMINISTIC —
// fixed code, no model call (a parser, a formula/units evaluator, a transform). Role is a plain string
// (kept comparable; default "" == skill) so adding it does not change Step's value semantics. The hook
// and dispatch node KINDS pair with the Session runtime (P3.3).
const (
	RoleSkill  = "" // the default: an agentic step
	RoleScript = "script"
)

// A Step's typed SOURCE (M5, representation-space-rebuild.md §1.3 + §1.4): which rung of the sourcing
// ladder a fuel-needing step PREFERS to draw its material from. It is a path's structural annotation —
// the path skills (analogy/induction/deduction) declare that, e.g., analogy's `analogize` step should
// privilege MEMORY (a prior case) while its `validate` step closes on REALITY. Source is a plain string
// (kept comparable; default SourceModel == "model" == the bare recombination rung) so adding it does not
// change Step's value semantics, and it ROUND-TRIPS through toDict/NodeFromDict but is OMITTED when it is
// the default so every seeded/golden program serialises byte-for-byte unchanged. It is a workflow-Gate
// NUDGE (a privilege, not a hard route): the ladder still walks its strict order — Source only biases the
// Gate toward the privileged source's stance, exactly as Role biases nothing structural.
const (
	SourceModel     = "" // the default: the bare recombination rung (the model invents — no privilege)
	SourcePresent   = "present"
	SourceKnowledge = "knowledge"
	SourceMemory    = "memory"
	SourceReality   = "reality"
	SourceCompute   = "compute" // a tool-backed deterministic source (validate/measure via run_tests)
	SourceStore     = "store"   // the induction terminus: curate → a durable store (knowledge/semantic)
)

// Step is one operator applied by a runtime sub-agent (operator + rough domain). Mirrors the Python
// @dataclass Step; defaults domain="general", note="", role=skill, source=model.
type Step struct {
	Operator string
	Domain   string // default "general"
	Note     string // default ""
	Role     string // "" (skill, agentic) | "script" (deterministic, no model call); default skill
	Source   string // "" (model) | present | knowledge | memory | reality | compute | store; default model
}

func (Step) node() {}

// IsScript reports whether this step is a deterministic SCRIPT node (no model call).
func (s Step) IsScript() bool { return s.Role == RoleScript }

func (s Step) toDict() map[string]any {
	d := map[string]any{"kind": "step", "operator": s.Operator, "domain": s.Domain, "note": s.Note}
	if s.Role != RoleSkill { // omit the default role so seeded/golden programs serialise unchanged
		d["role"] = s.Role
	}
	if s.Source != SourceModel { // omit the default source so seeded/golden programs serialise unchanged
		d["source"] = s.Source
	}
	return d
}

// Seq does its children in order. Mirrors the Python @dataclass Seq (children default empty list).
type Seq struct {
	Children []Node
}

func (Seq) node() {}

func (s Seq) toDict() map[string]any {
	children := make([]any, len(s.Children))
	for i, c := range s.Children {
		children[i] = c.toDict()
	}
	return map[string]any{"kind": "seq", "children": children}
}

// Par does its children at once (fan-out — independent sub-agents). Mirrors the Python @dataclass Par.
type Par struct {
	Children []Node
}

func (Par) node() {}

func (p Par) toDict() map[string]any {
	children := make([]any, len(p.Children))
	for i, c := range p.Children {
		children[i] = c.toDict()
	}
	return map[string]any{"kind": "par", "children": children}
}

// Loop repeats a body until a condition, bounded by MaxIter (always terminating). Mirrors the Python
// @dataclass Loop; defaults until="good enough", max_iter=3.
type Loop struct {
	Body    Node
	Until   string // default "good enough"
	MaxIter int    // default 3
}

func (Loop) node() {}

func (l Loop) toDict() map[string]any {
	return map[string]any{
		"kind": "loop", "until": l.Until, "max_iter": l.MaxIter, "body": l.Body.toDict(),
	}
}

// NodeToDict is the exported encoder for a single node (Python node.to_dict()).
func NodeToDict(n Node) map[string]any { return n.toDict() }

// PhasePlan is one scheduled phase-group: a list of steps to run together (>1 ⇒ parallel fan-out).
// Mirrors the Python @dataclass PhasePlan. Loop is a pointer here to reproduce Python's `loop: str |
// None` — nil == None (an ordinary, non-loop phase carries no loop label).
type PhasePlan struct {
	Steps     []Step
	Parallel  bool    // default false
	Loop      *string // loop label, if this group is part of an (unrolled) loop; nil == None
	Iteration int     // default 0
	Until     string  // the loop's stopping condition (carried for the Critic); default ""
}

// Program is the whole control-flow tree plus its provenance. Mirrors the Python @dataclass Program.
type Program struct {
	Root        Node
	Goal        string // default ""
	Synthesized bool   // constructed on the fly vs a canonical template; default false
	Rationale   string // why this shape was chosen (logged for standardisation/training); default ""
}

// Steps returns every operator step in the tree, in walk order. Mirrors Python Program.steps().
func (p Program) Steps() []Step {
	var out []Step
	collectSteps(p.Root, &out)
	return out
}

// ToDict serialises the whole program (Python Program.to_dict()) — goal, synthesized, rationale, root.
func (p Program) ToDict() map[string]any {
	return map[string]any{
		"goal": p.Goal, "synthesized": p.Synthesized, "rationale": p.Rationale,
		"root": p.Root.toDict(),
	}
}

// Shape is a one-line signature of the control flow, e.g. "seq(decompose, par(compare,contrast),
// rank)". Mirrors Python Program.shape().
func (p Program) Shape() string { return shapeOf(p.Root) }

// Schedule linearises the tree into phase-groups the tick loop can run (Python Program.schedule()): a
// parallel group becomes one phase; a loop is unrolled to its bound.
func (p Program) Schedule() []PhasePlan {
	var out []PhasePlan
	scheduleNode(p.Root, &out, nil, 0, "")
	return out
}

// collectSteps walks the tree appending every Step (Python _collect_steps). The type switch replaces
// the isinstance ladder; Loop recurses into its body.
func collectSteps(n Node, out *[]Step) {
	switch v := n.(type) {
	case Step:
		*out = append(*out, v)
	case Seq:
		for _, c := range v.Children {
			collectSteps(c, out)
		}
	case Par:
		for _, c := range v.Children {
			collectSteps(c, out)
		}
	case Loop:
		collectSteps(v.Body, out)
	}
}

// shapeOf renders the one-line control-flow signature (Python _shape). Unknown node => "?" (the
// Python fall-through), though the sealed interface makes that unreachable.
func shapeOf(n Node) string {
	switch v := n.(type) {
	case Step:
		return v.Operator
	case Seq:
		return "seq(" + joinShapes(v.Children) + ")"
	case Par:
		return "par(" + joinShapes(v.Children) + ")"
	case Loop:
		return "loop(" + shapeOf(v.Body) + ")"
	}
	return "?"
}

// joinShapes renders a child list as comma-joined shapes (the ", ".join(...) in Python _shape).
func joinShapes(children []Node) string {
	parts := make([]string, len(children))
	for i, c := range children {
		parts[i] = shapeOf(c)
	}
	return strings.Join(parts, ", ")
}

// scheduleNode is the linearising walk (Python _schedule). A Step emits one (non-parallel) PhasePlan;
// a Seq recurses in order; a Par flattens to all its immediate+nested steps in one parallel group; a
// Loop labels itself by its position in the output and unrolls its body max(1, max_iter) times.
//
// loop is the current loop label (nil == Python None — a non-loop phase). It is passed by value
// (pointer to a string) so each recursion sees the same label without aliasing the caller's variable.
func scheduleNode(n Node, out *[]PhasePlan, loop *string, iteration int, until string) {
	switch v := n.(type) {
	case Step:
		*out = append(*out, PhasePlan{
			Steps: []Step{v}, Parallel: false, Loop: loop, Iteration: iteration, Until: until,
		})
	case Seq:
		for _, c := range v.Children {
			scheduleNode(c, out, loop, iteration, until)
		}
	case Par:
		// all immediate steps fan out into one parallel group; nested composites flatten to steps.
		var steps []Step
		collectSteps(v, &steps)
		*out = append(*out, PhasePlan{
			Steps: steps, Parallel: true, Loop: loop, Iteration: iteration, Until: until,
		})
	case Loop:
		// the label is the loop's position in the output stream at entry (Python f"loop@{len(out)}").
		label := "loop@" + strconv.Itoa(len(*out))
		iters := v.MaxIter
		if iters < 1 {
			iters = 1 // Python range(max(1, node.max_iter))
		}
		for i := 0; i < iters; i++ {
			scheduleNode(v.Body, out, &label, i, v.Until)
		}
	}
}

// VerifyProgram applies the structural verification before trust (Python verify_program): every
// operator known/verified, loops bounded, size bounded, parallel groups are valid leaves. Returns
// (ok, issues). The catalog is consulted via the OperatorChecker interface (the OperatorRegistry's
// Has) so this stays a pure structural check.
//
// Issues are accumulated in the SAME order as Python (empty/too-many first, then per-step unknown
// operators in walk order, then depth, then the loop walk, then the par walk) so the failure messages
// match the reference exactly (golden-tested later).
func VerifyProgram(program Program, catalog OperatorChecker) (bool, []string) {
	var issues []string
	steps := program.Steps()
	if len(steps) == 0 {
		issues = append(issues, "empty program (no operator steps)")
	}
	if len(steps) > MaxSteps {
		issues = append(issues, "too many steps ("+strconv.Itoa(len(steps))+" > "+strconv.Itoa(MaxSteps)+")")
	}
	for _, s := range steps {
		if !catalog.Has(s.Operator) {
			issues = append(issues, "unknown operator '"+s.Operator+"' (not in catalog; mint+verify it first)")
		}
	}
	depth := depthOf(program.Root)
	if depth > MaxDepth {
		issues = append(issues, "nesting too deep ("+strconv.Itoa(depth)+" > "+strconv.Itoa(MaxDepth)+")")
	}
	checkLoops(program.Root, &issues)
	checkPar(program.Root, &issues)
	// Runaway-unroll guard: the static bounds above do not bound the multiplicative product of NESTED
	// loops, so a small tree can still schedule thousands of phases (each a model call). Reject when the
	// fully-unrolled phase count exceeds MaxScheduledPhases. Saturating so a pathological max_iter cannot
	// overflow int.
	if n := scheduledPhaseCount(program.Root, MaxScheduledPhases); n > MaxScheduledPhases {
		issues = append(issues, "schedules too many phases ("+strconv.Itoa(n)+" > "+
			strconv.Itoa(MaxScheduledPhases)+"; nested loops unroll multiplicatively)")
	}
	return len(issues) == 0, issues
}

// scheduledPhaseCount returns how many phase-groups Program.Schedule() would emit for this node,
// mirroring scheduleNode's unroll semantics: a Step is one phase, a Seq is the sum of its children, a
// Par collapses to ONE parallel group, and a Loop multiplies its body by max(1, max_iter). It
// SATURATES at cap+1 (early-exit) so an absurd loop bound cannot overflow int — the caller only needs
// to know whether the count exceeds the cap, not its exact astronomical value.
func scheduledPhaseCount(n Node, cap int) int {
	switch v := n.(type) {
	case Step:
		return 1
	case Seq:
		total := 0
		for _, c := range v.Children {
			total += scheduledPhaseCount(c, cap)
			if total > cap {
				return cap + 1
			}
		}
		return total
	case Par:
		return 1 // scheduleNode flattens a Par into a single parallel PhasePlan
	case Loop:
		body := scheduledPhaseCount(v.Body, cap)
		if body == 0 {
			return 0
		}
		iters := v.MaxIter
		if iters < 1 {
			iters = 1 // matches scheduleNode's range(max(1, max_iter))
		}
		if iters > (cap+1)/body+1 { // saturating multiply guard against overflow
			return cap + 1
		}
		if prod := iters * body; prod <= cap {
			return prod
		}
		return cap + 1
	}
	return 0
}

// OperatorChecker is the minimal catalog contract VerifyProgram needs: membership. Satisfied
// structurally by *OperatorRegistry (its Has method). Narrowing to an interface keeps VerifyProgram
// from depending on the full registry surface and mirrors Python's duck-typed `catalog.has(...)`.
type OperatorChecker interface {
	Has(name string) bool
}

// depthOf returns the nesting depth of the tree (Python _depth). A Step is depth 1; a Seq/Par is 1 +
// the max child depth (default 0 for empty); a Loop is 1 + its body's depth.
func depthOf(n Node) int {
	switch v := n.(type) {
	case Step:
		return 1
	case Seq:
		return 1 + maxChildDepth(v.Children)
	case Par:
		return 1 + maxChildDepth(v.Children)
	case Loop:
		return 1 + depthOf(v.Body)
	}
	return 1
}

// maxChildDepth is max(_depth(c) for c in children, default=0) — 0 for an empty child list.
func maxChildDepth(children []Node) int {
	best := 0
	for _, c := range children {
		if d := depthOf(c); d > best {
			best = d
		}
	}
	return best
}

// checkLoops verifies every Loop's max_iter is in [1, MaxIter] (Python _check_loops), recursing into
// loop bodies and Seq/Par children.
func checkLoops(n Node, issues *[]string) {
	switch v := n.(type) {
	case Loop:
		if !(v.MaxIter >= 1 && v.MaxIter <= MaxIter) {
			*issues = append(*issues, "loop max_iter "+strconv.Itoa(v.MaxIter)+
				" out of bounds [1,"+strconv.Itoa(MaxIter)+"] (must terminate)")
		}
		checkLoops(v.Body, issues)
	case Seq:
		for _, c := range v.Children {
			checkLoops(c, issues)
		}
	case Par:
		for _, c := range v.Children {
			checkLoops(c, issues)
		}
	}
}

// checkPar verifies every Par group: >=2 branches, non-empty after flattening, width <= MaxParWidth
// (Python _check_par). Recurses into Par children, Seq children, and Loop bodies.
func checkPar(n Node, issues *[]string) {
	switch v := n.(type) {
	case Par:
		if len(v.Children) < 2 {
			*issues = append(*issues, "parallel group with <2 branches (should be a step or a seq)")
		}
		var flat []Step
		collectSteps(v, &flat)
		if len(flat) == 0 { // children flatten to zero steps (e.g. Par[Seq([]), Seq([])])
			*issues = append(*issues, "parallel group has no steps after flattening (empty fan-out)")
		}
		if len(v.Children) > MaxParWidth {
			*issues = append(*issues, "parallel fan-out width "+strconv.Itoa(len(v.Children))+" > "+
				strconv.Itoa(MaxParWidth)+" (per-tick compute/schedulability budget; raise "+
				"THOUGHT_MAX_PAR_WIDTH or split into sub-phases)")
		}
		for _, c := range v.Children {
			checkPar(c, issues)
		}
	case Seq:
		for _, c := range v.Children {
			checkPar(c, issues)
		}
	case Loop:
		checkPar(v.Body, issues)
	}
}

// ProgramFromDict is the whole-Program inverse of Program.ToDict — it reconstructs goal/synthesized/
// rationale AND the root tree (via NodeFromDict on d["root"]), so a saved Program.ToDict() round-trips
// back to an equal Program. This is the canonical whole-program deserializer: ToDict emits the
// {goal, synthesized, rationale, root} envelope, so the matching load must peel that envelope before
// parsing the root node. (NodeFromDict alone expects a NODE dict keyed on "kind" — handed the whole-
// program envelope it errors with "unknown program node kind: None"; that mismatch is the body
// save/load round-trip bug ProgramFromDict closes for engine/persist.go.) A missing/non-object "root"
// is an error, never a panic — the same defensive contract as NodeFromDict.
func ProgramFromDict(d map[string]any) (Program, error) {
	rootDict, ok := d["root"].(map[string]any)
	if !ok {
		return Program{}, &programParseError{"program dict missing object 'root' field"}
	}
	root, err := NodeFromDict(rootDict)
	if err != nil {
		return Program{}, err
	}
	return Program{
		Root:        root,
		Goal:        strOr(d, "goal", ""),
		Synthesized: boolOr(d, "synthesized", false),
		Rationale:   strOr(d, "rationale", ""),
	}, nil
}

// NodeFromDict parses a backend-written program node (Python node_from_dict). It is the decoder side
// of ToDict AND the parser for an LLM-written program tree, so it is defensive: an unknown kind, a
// missing required field, or a malformed child returns an error (never a panic) — the Go form of
// Python's ValueError / KeyError, surfaced as an error per the port rule "unknown kind → error not
// panic".
//
// Numeric fields arriving from JSON decode as float64; max_iter is coerced via toInt to mirror
// Python's int(d.get("max_iter", 3)).
func NodeFromDict(d map[string]any) (Node, error) {
	kind, _ := d["kind"].(string)
	switch kind {
	case "step":
		op, ok := d["operator"].(string)
		if !ok {
			return nil, &programParseError{"step node missing string 'operator' field"}
		}
		return Step{Operator: op, Domain: strOr(d, "domain", "general"), Note: strOr(d, "note", ""),
			Role: strOr(d, "role", RoleSkill), Source: strOr(d, "source", SourceModel)}, nil
	case "seq":
		children, err := childrenFromDict(d)
		if err != nil {
			return nil, err
		}
		return Seq{Children: children}, nil
	case "par":
		children, err := childrenFromDict(d)
		if err != nil {
			return nil, err
		}
		return Par{Children: children}, nil
	case "loop":
		bodyRaw, ok := d["body"].(map[string]any)
		if !ok {
			return nil, &programParseError{"loop node missing object 'body' field"}
		}
		body, err := NodeFromDict(bodyRaw)
		if err != nil {
			return nil, err
		}
		return Loop{Body: body, Until: strOr(d, "until", "good enough"), MaxIter: intOr(d, "max_iter", 3)}, nil
	default:
		return nil, &programParseError{"unknown program node kind: " + kindRepr(d["kind"])}
	}
}

// childrenFromDict parses a "children" list of node dicts (the seq/par branches). A missing key is an
// empty list (Python d.get("children", [])); a non-list, or a non-object child, is a parse error.
func childrenFromDict(d map[string]any) ([]Node, error) {
	raw, present := d["children"]
	if !present {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, &programParseError{"'children' must be a list of nodes"}
	}
	out := make([]Node, 0, len(list))
	for _, c := range list {
		cm, ok := c.(map[string]any)
		if !ok {
			return nil, &programParseError{"program child must be an object"}
		}
		child, err := NodeFromDict(cm)
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, nil
}

// strOr returns d[key] as a string, or def if absent/not-a-string — the Go form of d.get(key, def)
// where the value is expected to be a str.
func strOr(d map[string]any, key, def string) string {
	if v, ok := d[key].(string); ok {
		return v
	}
	return def
}

// boolOr returns d[key] as a bool, or def if absent/not-a-bool — the Go form of bool(d.get(key, def)).
// JSON booleans decode to Go bool, so a persisted Program.ToDict()["synthesized"] round-trips exactly.
func boolOr(d map[string]any, key string, def bool) bool {
	if v, ok := d[key].(bool); ok {
		return v
	}
	return def
}

// intOr returns d[key] coerced to int, or def if absent — the Go form of int(d.get(key, def)). JSON
// numbers decode to float64; an int, int64, or numeric string is also accepted so a hand- or
// LLM-written program parses the same whether the number came through JSON or a Go literal.
func intOr(d map[string]any, key string, def int) int {
	v, ok := d[key]
	if !ok {
		return def
	}
	if n, ok := toInt(v); ok {
		return n
	}
	return def
}

// toInt coerces a JSON-decoded number (float64), a Go int/int64, or a numeric string to int,
// truncating toward zero like Python's int(). ok=false for anything non-numeric.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

// kindRepr renders the offending kind value for the unknown-kind error, mirroring Python's {kind!r}:
// a string is single-quoted, None/absent is "None", a non-string scalar uses its bare repr.
func kindRepr(v any) string {
	switch k := v.(type) {
	case nil:
		return "None"
	case string:
		return "'" + k + "'"
	case float64:
		return strconv.FormatFloat(k, 'g', -1, 64)
	case bool:
		if k {
			return "True"
		}
		return "False"
	default:
		return "'?'"
	}
}

// programParseError is the error type NodeFromDict returns for a malformed/unknown node — the Go form
// of Python's ValueError raised in node_from_dict.
type programParseError struct{ msg string }

func (e *programParseError) Error() string { return e.msg }

// -- a tiny builder DSL the synthesiser uses --------------------------------- //

// NewStep builds a Step (Python step(operator, domain, note)). Defaults are applied by the caller via
// the *Default helpers; this is the explicit 3-arg form.
func NewStep(operator, domain, note string) Step {
	return Step{Operator: operator, Domain: domain, Note: note}
}

// StepOp builds a Step with the Python defaults domain="general", note="" (the common 1-arg call).
func StepOp(operator string) Step { return Step{Operator: operator, Domain: "general", Note: ""} }

// SourceStep builds a Step that PREFERS a given sourcing-ladder rung for its material (M5): the path
// skills use it to annotate, e.g., analogy's `analogize` step with SourceMemory and its `validate` step
// with SourceReality. domain/note default to general/"". A SourceModel argument yields a default (un-
// annotated) step that serialises byte-for-byte like StepOp.
func SourceStep(operator, domain, note, source string) Step {
	return Step{Operator: operator, Domain: domain, Note: note, Source: source}
}

// NewSeq builds a Seq from its children (Python seq(*children)).
func NewSeq(children ...Node) Seq { return Seq{Children: append([]Node(nil), children...)} }

// NewPar builds a Par from its children (Python par(*children)).
func NewPar(children ...Node) Par { return Par{Children: append([]Node(nil), children...)} }

// NewLoop builds a Loop with the given body/until/max_iter (Python loop(body, until, max_iter)).
func NewLoop(body Node, until string, maxIter int) Loop {
	return Loop{Body: body, Until: until, MaxIter: maxIter}
}

// LoopBody builds a Loop with the Python defaults until="good enough", max_iter=3.
func LoopBody(body Node) Loop { return Loop{Body: body, Until: "good enough", MaxIter: 3} }

// -- the RPIV program template (the Validative faculty's standing capability) ----------------------- //

// RPIVPhase names the four ordered phases of the RPIV (Research -> Plan -> Implement -> Validate)
// program template, the standing capability of the Validative faculty (redesign §13.5 "the missing
// validation faculty", cognitive-functions research §4.3). Each phase maps to one EXISTING seed
// operator — RPIV is a COMPOSITION of primitives the catalog already verifies, not a new operator:
//
//	RESEARCH  -> expose-affordances (search/read — discover what is there before committing)
//	PLAN      -> decompose          (break the goal into independent, sequenceable parts)
//	IMPLEMENT -> generate           (draft the concrete candidate — the work)
//	VALIDATE  -> validate           (ToolScope run_tests — close on a GROUNDED check, NOT self-judgment)
//
// The VALIDATE step is sourced from REALITY/COMPUTE (the sourcing-ladder terminus) so the workflow
// Gate privileges a grounded close — this is the loop's INDEPENDENT reward signal (the antidote to the
// same-model ceiling: a real test / held-out outcome cannot be reward-hacked the way self-judgment can).
const (
	RPIVResearch  = "research"
	RPIVPlan      = "plan"
	RPIVImplement = "implement"
	RPIVValidate  = "validate"
)

// RPIVOperators is the ordered (phase -> backing seed-operator) map the template instantiates. Exposed
// so the engine + tests can assert the template grounds on the EXISTING verified catalog (every operator
// here is a seedOps row, so VerifyProgram passes against a fresh OperatorRegistry — no mint needed).
var RPIVOperators = []struct {
	Phase    string
	Operator string
	Source   string // the sourcing-ladder rung the phase privileges (M5 Source annotation)
}{
	{RPIVResearch, "expose-affordances", SourceKnowledge}, // discover what is already known/available
	{RPIVPlan, "decompose", SourceModel},                  // sequence the parts (pure recombination)
	{RPIVImplement, "generate", SourceModel},              // draft the candidate (the model owns CONTENT)
	{RPIVValidate, "validate", SourceReality},             // close on a grounded check (run_tests / outcome)
}

// RPIVProgram builds the RPIV (Research -> Plan -> Implement -> Validate) program for a goal: an ordered
// Seq of the four phases, each one Step backed by an EXISTING verified operator (so VerifyProgram passes
// against the seed catalog with no minting). The VALIDATE phase is annotated SourceReality — the
// workflow Gate then privileges a grounded close (the EvalGate / a test / a held-out outcome), which is
// the whole point: the validation phase is the loop's independent reward source, not same-model
// self-judgment. domain defaults to "general" when empty. The returned Program is marked Synthesized
// (constructed on the fly for this goal) with a rationale logged for standardisation/training.
func RPIVProgram(goal, domain string) Program {
	if domain == "" {
		domain = "general"
	}
	children := make([]Node, 0, len(RPIVOperators))
	for _, p := range RPIVOperators {
		children = append(children, SourceStep(p.Operator, domain, "RPIV:"+p.Phase, p.Source))
	}
	return Program{
		Root:        NewSeq(children...),
		Goal:        goal,
		Synthesized: true,
		Rationale: "RPIV template (research->plan->implement->validate): the Validative faculty's standing " +
			"capability — the VALIDATE phase closes on a grounded check, the loop's independent reward signal",
	}
}
