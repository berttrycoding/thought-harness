// Package wfproof is a THROWAWAY proof prototype (like internal/zoommem). It answers one question:
// can our workflow substrate capture lathe's WHOLE workflow — not just skills, but hooks and scripts
// too — and can it express something as complex as the /swarm at scale?
//
// It is not wired into the engine. It exists to be compiled and tested (`go test ./internal/wfproof/`),
// so the claim "we can build a workflow at this scale" is proven by construction, not asserted.
//
// THE UNIFIED STRUCTURE — one node model, four roles (the controller's deterministic-vs-agentic
// principle, generalised to the workflow):
//
//   - SKILL    an AGENTIC step    — a sub-agent applies an operator (semantic, not calculable)
//   - SCRIPT   a DETERMINISTIC step — a pure function of well-understood inputs (calculable)
//   - HOOK     a Guard            — a DETERMINISTIC, EVENT-triggered interceptor that may gate
//     (block / inject context / set a verdict floor the agent can't override)
//   - DISPATCH spawn a NAMED sub-workflow into a fresh bounded sub-agent (spawn, don't inline)
//
// The first three are exactly lathe's three ingredients (skill .md / hook .sh / python script). The
// fourth is the SCALE mechanism: a workflow stays bounded by spawning its children as their own
// episodes (the swarm spawns sprints, sprints spawn chains) rather than flattening everything into one
// program. Depth is bounded PER WORKFLOW; Dispatch starts a fresh budget. That is what lets the whole
// composition be large while every individual episode stays in the durable regime.
package wfproof

import (
	"sort"
	"strconv"
	"strings"
)

// Per-workflow bounds — mirror internal/cognition/program.go. A SINGLE workflow past these is runaway;
// the whole library may be arbitrarily large because Dispatch spawns rather than nests.
const (
	MaxSteps    = 24 // total skill+script steps in ONE workflow's tree (Dispatch is a leaf, not a step)
	MaxDepth    = 6  // nesting depth of ONE workflow's tree (Dispatch resets the budget)
	MaxIter     = 6  // a loop's max_iter ceiling
	MaxParWidth = 8  // parallel fan-out width within one workflow
)

// Kind is the node role the controller principle gives us: calculable -> deterministic, semantic ->
// agentic. This is the difference between a script and a skill.
type Kind int

const (
	Agentic       Kind = iota // a SKILL: a sub-agent applies an operator
	Deterministic             // a SCRIPT: a pure function of known inputs
)

func (k Kind) String() string {
	if k == Deterministic {
		return "script"
	}
	return "skill"
}

// Event is when a guard (hook) fires — the lifecycle points lathe/Claude Code hooks attach to.
type Event int

const (
	OnPre  Event = iota // before the node runs   (PreToolUse / UserPromptSubmit)
	OnPost              // after the node runs     (PostToolUse)
	OnStop              // at episode end          (Stop)
)

func (e Event) String() string { return [...]string{"pre", "post", "stop"}[e] }

// GuardAction is what a hook may do — the gate vocabulary, lifted straight from the lathe survey.
type GuardAction int

const (
	Allow  GuardAction = iota // no-op (let normal flow proceed)
	Block                     // hard stop (lathe exit 2 — e.g. post-critic attendance gate)
	Inject                    // add context, don't block (inject-consults / inject-inbox)
	Floor                     // set a verdict floor the agent CANNOT override (contract-check)
)

func (a GuardAction) String() string { return [...]string{"allow", "block", "inject", "floor"}[a] }

// Guard is a HOOK: an event-triggered deterministic interceptor on a node or a whole workflow. This is
// the mechanism my first pass missed — it captures lathe's hooks (and CC's PreToolUse/PostToolUse) in
// the SAME structure as everything else. Check names the deterministic script that computes the gate.
type Guard struct {
	On     Event
	Check  string // the deterministic check/script that decides (e.g. "contract-check", "chain-invariants")
	Action GuardAction
	Note   string
}

// Node is the sealed node interface (mirrors cognition.Node, plus Dispatch).
type Node interface {
	node()
	toDict() map[string]any
}

// Step is one leaf: a SKILL (Agentic) or a SCRIPT (Deterministic). Guards are the hooks bound to it.
type Step struct {
	Op     string
	Kind   Kind
	Domain string
	Guards []Guard
	Note   string
}

func (Step) node() {}

// Seq runs children in order. Par runs them at once (fan-out). Loop repeats a bounded body.
type Seq struct {
	Children []Node
	Guards   []Guard
}

func (Seq) node() {}

type Par struct {
	Children []Node
}

func (Par) node() {}

type Loop struct {
	Body    Node
	Until   string
	MaxIter int
}

func (Loop) node() {}

// Dispatch is the SCALE mechanism: spawn a NAMED sub-workflow into a fresh bounded sub-agent. It is a
// LEAF in the local tree (its local cost is "spawn one worker"); the target workflow is validated on
// its own. Over records what it fans over (a cluster, a wave-item) for provenance. This is how the
// swarm composes past the inline depth bound — exactly as lathe's /swarm spawns /sprint sessions.
type Dispatch struct {
	Workflow string // the named sub-workflow to run (resolved against the Library)
	Over     string // what each spawn corresponds to (provenance only)
	Guards   []Guard
}

func (Dispatch) node() {}

// Workflow is a named, stored, reusable program: a root node + workflow-level hooks (Stop /
// UserPromptSubmit guards that aren't tied to a single step). A Library of these IS the "chain library"
// — named topologies you dispatch by name.
type Workflow struct {
	Name  string
	Root  Node
	Hooks []Guard // workflow-level hooks (fire around the whole episode)
}

// Library is the registry of named workflows. Dispatch resolves names against it. This is the
// unit-skills-AND-how-to-link-them library in one place: leaves are skills/scripts, named workflows are
// the linking topologies.
type Library map[string]*Workflow

// ---------------------------------------------------------------------------
// Walks / counts — prove all FOUR roles are captured, not just skills.
// ---------------------------------------------------------------------------

// Counts tallies the roles in ONE workflow's local tree (not following Dispatch).
type Counts struct {
	Skills    int
	Scripts   int
	Hooks     int
	Dispatch  int
	Loops     int
	ParGroups int
}

func (c Counts) add(o Counts) Counts {
	return Counts{c.Skills + o.Skills, c.Scripts + o.Scripts, c.Hooks + o.Hooks,
		c.Dispatch + o.Dispatch, c.Loops + o.Loops, c.ParGroups + o.ParGroups}
}

// CountLocal walks one workflow's tree (Dispatch is a leaf) and tallies every role, including hooks on
// nodes and the workflow-level hooks.
func (wf *Workflow) CountLocal() Counts {
	c := countNode(wf.Root)
	c.Hooks += len(wf.Hooks)
	return c
}

func countNode(n Node) Counts {
	switch v := n.(type) {
	case Step:
		c := Counts{Hooks: len(v.Guards)}
		if v.Kind == Deterministic {
			c.Scripts = 1
		} else {
			c.Skills = 1
		}
		return c
	case Seq:
		c := Counts{Hooks: len(v.Guards)}
		for _, ch := range v.Children {
			c = c.add(countNode(ch))
		}
		return c
	case Par:
		c := Counts{ParGroups: 1}
		for _, ch := range v.Children {
			c = c.add(countNode(ch))
		}
		return c
	case Loop:
		c := Counts{Loops: 1}
		return c.add(countNode(v.Body))
	case Dispatch:
		return Counts{Dispatch: 1, Hooks: len(v.Guards)}
	}
	return Counts{}
}

// stepCount is the bound-relevant size: skill+script leaves in one tree (Dispatch is NOT a step).
func stepCount(n Node) int { c := countNode(n); return c.Skills + c.Scripts }

// depthOf is the local nesting depth; Dispatch is a leaf (depth 1) because it spawns, not nests.
func depthOf(n Node) int {
	switch v := n.(type) {
	case Seq:
		return 1 + maxChild(v.Children)
	case Par:
		return 1 + maxChild(v.Children)
	case Loop:
		return 1 + depthOf(v.Body)
	default: // Step, Dispatch
		return 1
	}
}

func maxChild(ns []Node) int {
	best := 0
	for _, n := range ns {
		if d := depthOf(n); d > best {
			best = d
		}
	}
	return best
}

// dispatchTargets returns the names this workflow spawns (the edges of the dispatch graph).
func dispatchTargets(n Node) []string {
	var out []string
	var walk func(Node)
	walk = func(x Node) {
		switch v := x.(type) {
		case Dispatch:
			out = append(out, v.Workflow)
		case Seq:
			for _, c := range v.Children {
				walk(c)
			}
		case Par:
			for _, c := range v.Children {
				walk(c)
			}
		case Loop:
			walk(v.Body)
		}
	}
	walk(n)
	return out
}

// ---------------------------------------------------------------------------
// Verification — the structural + durability gate, per workflow and library-wide.
// ---------------------------------------------------------------------------

// Verify checks ONE workflow against the per-workflow bounds (well-formed + stays in the durable
// regime locally). Returns (ok, issues).
func Verify(wf *Workflow) (bool, []string) {
	var issues []string
	if n := stepCount(wf.Root); n > MaxSteps {
		issues = append(issues, wf.Name+": too many steps ("+itoa(n)+" > "+itoa(MaxSteps)+")")
	}
	if d := depthOf(wf.Root); d > MaxDepth {
		issues = append(issues, wf.Name+": nesting too deep ("+itoa(d)+" > "+itoa(MaxDepth)+")")
	}
	checkPar(wf.Root, wf.Name, &issues)
	checkLoops(wf.Root, wf.Name, &issues)
	return len(issues) == 0, issues
}

func checkPar(n Node, wf string, issues *[]string) {
	switch v := n.(type) {
	case Par:
		if len(v.Children) < 2 {
			*issues = append(*issues, wf+": parallel group with <2 branches")
		}
		if len(v.Children) > MaxParWidth {
			*issues = append(*issues, wf+": fan-out width "+itoa(len(v.Children))+" > "+itoa(MaxParWidth))
		}
		for _, c := range v.Children {
			checkPar(c, wf, issues)
		}
	case Seq:
		for _, c := range v.Children {
			checkPar(c, wf, issues)
		}
	case Loop:
		checkPar(v.Body, wf, issues)
	}
}

func checkLoops(n Node, wf string, issues *[]string) {
	switch v := n.(type) {
	case Loop:
		if v.MaxIter < 1 || v.MaxIter > MaxIter {
			*issues = append(*issues, wf+": loop max_iter "+itoa(v.MaxIter)+" out of [1,"+itoa(MaxIter)+"]")
		}
		checkLoops(v.Body, wf, issues)
	case Seq:
		for _, c := range v.Children {
			checkLoops(c, wf, issues)
		}
	case Par:
		for _, c := range v.Children {
			checkLoops(c, wf, issues)
		}
	}
}

// VerifyLibrary is the WHOLE-SYSTEM gate: every workflow reachable from root must (1) pass its local
// bounds, (2) dispatch only to workflows that EXIST, and (3) the dispatch graph must be ACYCLIC (so the
// spawn recursion terminates). This is what proves a large composition is still safe: each episode is
// bounded, and the spawn graph can't loop forever.
func VerifyLibrary(lib Library, root string) (bool, []string) {
	var issues []string

	// 1. every reachable workflow passes local bounds + dispatches only to existing targets.
	reach := reachable(lib, root)
	names := make([]string, 0, len(reach))
	for n := range reach {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		wf, ok := lib[name]
		if !ok {
			issues = append(issues, "missing workflow: '"+name+"'")
			continue
		}
		if ok, iss := Verify(wf); !ok {
			issues = append(issues, iss...)
		}
		for _, tgt := range dispatchTargets(wf.Root) {
			if _, ok := lib[tgt]; !ok {
				issues = append(issues, name+": dispatch to unknown workflow '"+tgt+"'")
			}
		}
	}

	// 2. the dispatch graph is acyclic (terminating spawn).
	if cyc := findCycle(lib, root); cyc != "" {
		issues = append(issues, "dispatch cycle: "+cyc)
	}
	return len(issues) == 0, issues
}

// reachable returns every workflow name reachable from root by following Dispatch edges.
func reachable(lib Library, root string) map[string]bool {
	seen := map[string]bool{}
	var dfs func(string)
	dfs = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if wf, ok := lib[name]; ok {
			for _, t := range dispatchTargets(wf.Root) {
				dfs(t)
			}
		}
	}
	dfs(root)
	return seen
}

// findCycle returns a non-empty "a -> b -> a" path if the dispatch graph has a cycle, else "".
func findCycle(lib Library, root string) string {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var path []string
	var dfs func(string) string
	dfs = func(name string) string {
		color[name] = gray
		path = append(path, name)
		if wf, ok := lib[name]; ok {
			for _, t := range dispatchTargets(wf.Root) {
				if color[t] == gray {
					return strings.Join(append(path, t), " -> ")
				}
				if color[t] == white {
					if c := dfs(t); c != "" {
						return c
					}
				}
			}
		}
		path = path[:len(path)-1]
		color[name] = black
		return ""
	}
	return dfs(root)
}

// InlineStepCount is the NEGATIVE-test instrument: what if we DON'T spawn — if we flatten the whole
// composition into one program by inlining every Dispatch? It returns the total skill+script steps of
// the fully-inlined tree, so the test can show it blows MaxSteps (which is WHY Dispatch/spawn is
// necessary for scale). Recursion is bounded by the (verified-acyclic) library.
func InlineStepCount(lib Library, root string) int {
	wf, ok := lib[root]
	if !ok {
		return 0
	}
	return inline(wf.Root, lib)
}

func inline(n Node, lib Library) int {
	switch v := n.(type) {
	case Step:
		return 1
	case Seq:
		t := 0
		for _, c := range v.Children {
			t += inline(c, lib)
		}
		return t
	case Par:
		t := 0
		for _, c := range v.Children {
			t += inline(c, lib)
		}
		return t
	case Loop:
		return inline(v.Body, lib)
	case Dispatch:
		if sub, ok := lib[v.Workflow]; ok {
			return inline(sub.Root, lib)
		}
		return 0
	}
	return 0
}

// ---------------------------------------------------------------------------
// Schedule — linearise one workflow into phase-groups (the runnable form).
// ---------------------------------------------------------------------------

// Phase is one scheduled group: >1 step => a parallel fan-out tick.
type Phase struct {
	Steps    []string // "skill:research" / "script:verify" / "dispatch:sprint"
	Parallel bool
}

// Schedule linearises one workflow's tree into phases (Dispatch is one scheduled item — a spawn tick).
func Schedule(wf *Workflow) []Phase {
	var out []Phase
	scheduleNode(wf.Root, &out)
	return out
}

func scheduleNode(n Node, out *[]Phase) {
	switch v := n.(type) {
	case Step:
		*out = append(*out, Phase{Steps: []string{v.Kind.String() + ":" + v.Op}})
	case Dispatch:
		*out = append(*out, Phase{Steps: []string{"dispatch:" + v.Workflow}})
	case Seq:
		for _, c := range v.Children {
			scheduleNode(c, out)
		}
	case Par:
		var labels []string
		collectLabels(v, &labels)
		*out = append(*out, Phase{Steps: labels, Parallel: true})
	case Loop:
		for i := 0; i < max1(v.MaxIter); i++ {
			scheduleNode(v.Body, out)
		}
	}
}

func collectLabels(n Node, out *[]string) {
	switch v := n.(type) {
	case Step:
		*out = append(*out, v.Kind.String()+":"+v.Op)
	case Dispatch:
		*out = append(*out, "dispatch:"+v.Workflow)
	case Seq:
		for _, c := range v.Children {
			collectLabels(c, out)
		}
	case Par:
		for _, c := range v.Children {
			collectLabels(c, out)
		}
	case Loop:
		collectLabels(v.Body, out)
	}
}

// ---------------------------------------------------------------------------
// Serialisation — prove the whole thing is captured AS DATA (logged/standardised/trainable).
// ---------------------------------------------------------------------------

func (s Step) toDict() map[string]any {
	return map[string]any{"kind": "step", "op": s.Op, "role": s.Kind.String(),
		"domain": s.Domain, "guards": guardsToDict(s.Guards)}
}

func (s Seq) toDict() map[string]any {
	return map[string]any{"kind": "seq", "children": childrenToDict(s.Children), "guards": guardsToDict(s.Guards)}
}

func (p Par) toDict() map[string]any {
	return map[string]any{"kind": "par", "children": childrenToDict(p.Children)}
}

func (l Loop) toDict() map[string]any {
	return map[string]any{"kind": "loop", "until": l.Until, "max_iter": l.MaxIter, "body": l.Body.toDict()}
}

func (d Dispatch) toDict() map[string]any {
	return map[string]any{"kind": "dispatch", "workflow": d.Workflow, "over": d.Over, "guards": guardsToDict(d.Guards)}
}

// ToDict serialises a whole workflow (root + workflow-level hooks).
func (wf *Workflow) ToDict() map[string]any {
	return map[string]any{"name": wf.Name, "root": wf.Root.toDict(), "hooks": guardsToDict(wf.Hooks)}
}

func childrenToDict(ns []Node) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = n.toDict()
	}
	return out
}

func guardsToDict(gs []Guard) []any {
	out := make([]any, len(gs))
	for i, g := range gs {
		out[i] = map[string]any{"on": g.On.String(), "check": g.Check, "action": g.Action.String(), "note": g.Note}
	}
	return out
}

// ---------------------------------------------------------------------------
// tiny helpers
// ---------------------------------------------------------------------------

func itoa(n int) string { return strconv.Itoa(n) }

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
