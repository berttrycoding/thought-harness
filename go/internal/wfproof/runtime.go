package wfproof

// runtime.go is the second half of the proof: not just "can the structure CAPTURE the swarm" but
// "can our harness RUN it headless, in-process, without an external session launcher."
//
// lathe's /swarm spawns /sprint sessions via cc-spawn — an MCP that launches separate Claude Code OS
// processes. That is a HACK forced by Claude Code not owning its own loop: the only way to get a fresh
// context window was a new process. OUR harness owns the loop, so a Dispatch is a NATIVE nested
// episode: run the named sub-workflow as its own bounded Session (own context / own thought-graph),
// return its result to the parent. No cc-spawn. This is the session / sub-agent-management concept.
//
// The Runner below is that executor, in miniature. It:
//   - spawns a child Session per Dispatch (recursion = the spawn tree), in-process;
//   - bounds the SPAWN DEPTH (sessions-deep) — distinct from a single workflow's node depth;
//   - records peak fan-out (the concurrency a scheduler would run at once) and checks it vs MaxParWidth
//     — the regulator's schedulability budget (U<=1), the same bound that governs real fan-out;
//   - EVALUATES hooks at runtime (a Block hook can halt the session; a Floor hook sets a verdict the
//     agent can't override) — proving the gates fire during execution, not just during verification.

import "strconv"

// Result is what one Session returns to its parent — its verdict, the work it did, and its children.
type Result struct {
	Workflow string
	Verdict  string // "ok" | "blocked:<check>" | "floored:<check>"
	Skills   int
	Scripts  int
	Children []Result
}

// Runner executes a workflow library headlessly, owning the spawn. No external launcher.
type Runner struct {
	lib           Library
	MaxSpawnDepth int             // how many sessions deep a spawn tree may go (the recursion budget)
	Failing       map[string]bool // checks that FAIL this run (default none => all gates pass)

	Spawned    int                 // total sessions spawned
	PeakFanout int                 // widest Par group encountered (concurrent-session width)
	HooksFired map[GuardAction]int // gate actions evaluated at runtime
	Halted     bool
	HaltReason string
	Err        string // first runtime error (e.g. spawn-depth budget exceeded)
}

// NewRunner builds a Runner with the default spawn-depth budget (4: swarm->sprint->chain is 3 deep).
func NewRunner(lib Library) *Runner {
	return &Runner{lib: lib, MaxSpawnDepth: 4, Failing: map[string]bool{}, HooksFired: map[GuardAction]int{}}
}

// Run executes the named workflow as the root session and returns its Result.
func (r *Runner) Run(name string) Result {
	return r.runSession(name, 0)
}

// runSession runs ONE workflow as its own bounded episode (a Session) and returns its Result. depth is
// the spawn depth (sessions deep). This is the native equivalent of cc-spawn launching a session.
func (r *Runner) runSession(name string, depth int) Result {
	if depth > r.MaxSpawnDepth {
		if r.Err == "" {
			r.Err = "spawn depth " + strconv.Itoa(depth) + " exceeds budget " + strconv.Itoa(r.MaxSpawnDepth)
		}
		r.Halted = true
		return Result{Workflow: name, Verdict: "depth-exceeded"}
	}
	wf, ok := r.lib[name]
	if !ok {
		r.Err = "unknown workflow '" + name + "'"
		r.Halted = true
		return Result{Workflow: name, Verdict: "unknown"}
	}
	r.Spawned++
	res := Result{Workflow: name, Verdict: "ok"}
	r.fireHooks(wf.Hooks, OnPre, &res)
	r.exec(wf.Root, depth, &res)
	r.fireHooks(wf.Hooks, OnStop, &res)
	return res
}

// exec walks one workflow's tree. A Dispatch SPAWNS a child session (depth+1). Skills/scripts do their
// (synthetic) work. Par records fan-out width. Hooks fire and may halt.
func (r *Runner) exec(n Node, depth int, res *Result) {
	if r.Halted {
		return
	}
	switch v := n.(type) {
	case Step:
		r.fireHooks(v.Guards, OnPre, res)
		if r.Halted {
			return
		}
		if v.Kind == Deterministic {
			r.scriptsInc(res)
		} else {
			r.skillsInc(res)
		}
		r.fireHooks(v.Guards, OnPost, res)
	case Seq:
		r.fireHooks(v.Guards, OnPre, res)
		for _, c := range v.Children {
			if r.Halted {
				return
			}
			r.exec(c, depth, res)
		}
	case Par:
		if len(v.Children) > r.PeakFanout {
			r.PeakFanout = len(v.Children) // the concurrency a scheduler would run at once
		}
		for _, c := range v.Children {
			if r.Halted {
				return
			}
			r.exec(c, depth, res) // sequential here; the real engine fans these out under the regulator
		}
	case Loop:
		for i := 0; i < max1(v.MaxIter); i++ {
			if r.Halted {
				return
			}
			r.exec(v.Body, depth, res)
		}
	case Dispatch:
		r.fireHooks(v.Guards, OnPre, res)
		if r.Halted {
			return
		}
		child := r.runSession(v.Workflow, depth+1) // <- the native headless spawn
		res.Children = append(res.Children, child)
	}
}

// fireHooks evaluates the guards for an event AT RUNTIME. A Block whose check is Failing halts the
// session (lathe's exit-2); a Floor whose check is Failing sets a verdict the agent can't override.
func (r *Runner) fireHooks(gs []Guard, ev Event, res *Result) {
	for _, g := range gs {
		if g.On != ev {
			continue
		}
		r.HooksFired[g.Action]++
		switch g.Action {
		case Block:
			if r.Failing[g.Check] {
				r.Halted = true
				r.HaltReason = "blocked by " + g.Check
				res.Verdict = "blocked:" + g.Check
			}
		case Floor:
			if r.Failing[g.Check] {
				res.Verdict = "floored:" + g.Check
			}
		}
	}
}

func (r *Runner) skillsInc(res *Result)  { res.Skills++ }
func (r *Runner) scriptsInc(res *Result) { res.Scripts++ }

// SessionCount totals the sessions in a result tree (for the test to cross-check Spawned).
func SessionCount(res Result) int {
	n := 1
	for _, c := range res.Children {
		n += SessionCount(c)
	}
	return n
}
