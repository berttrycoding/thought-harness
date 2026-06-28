// context.go — the subconscious Context object (cognition redesign §3.11): the INSTANCE-ONLY,
// three-layer material a Capability captures at trigger time and passes down the stack to
// concretize an operator. See docs/cognition/01-subconscious.md §3.11 / §2.4.
//
// Context is the ONE object type that is instance-only (§2.4): it is always derived from the
// runtime, time-based thought graph, so it can never be a reusable seeded template. There is no
// Context registry and no Context reference — only instances. (The mintable *lesson* about a
// context is the Context prior, §3.12 — a separate, later object.)
//
// The three layers (§3.11):
//
//	L1 — compressed active branch (spine). A SNAPSHOT at trigger time of the *entire* active
//	     branch in compressed (gist) form, plus the thought IDs so a worker can traverse/expand
//	     on demand (OPT-3). This replaces the ≤5-thought ContextSlice window (subagent.go:154)
//	     so context SCALES with the branch instead of clipping to a fixed tail. The snapshot
//	     FREEZES the branch as it stood at the trigger — later thinking does not mutate it.
//	L2 — runtime-derived. Context the workflow / sub-agent produces DURING the run, written back
//	     as the program executes (e.g. an intermediate result a later operator concretizes against).
//	L3 — paired domain knowledge (RAG-like). A reference to a knowledge index declared in the
//	     operator / sub-agent definition, to be pulled from the knowledge store and handed to the
//	     worker. Here it is a lightweight REF (id + query) only — the store pull is wired later
//	     (OPT-4 pre-attach); this object just carries the declaration so the seam is in place.
//
// SCOPE OF THIS SLICE. Context is a NEW object built additively to be wired in later (it does not
// yet replace the live ContextSlice path in subagent.go / workflow.go). It owns capture + the three
// layers; it does not touch the existing specialist / dispatch / engine runtime.
package subconscious

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// L1Snapshot is the L1 layer: a frozen, compressed view of the active branch at trigger time.
// It carries the gist (the lossy one-line summary the backend produces — the same Summarize the
// graph's Compress uses, graph/mcp.go:146) and the ordered thought IDs of the branch so a worker
// can expand any node on demand (OPT-3 — live traversal is the optimisation; the snapshot of IDs
// + gist is the core). The snapshot is a value copy: it does NOT alias the live branch, so the
// branch's resolution shifting as thinking continues (graph focus/compress) never mutates it.
type L1Snapshot struct {
	BranchID   int    // the active branch this was snapshotted from
	Gist       string // the compressed one-line gist (lossy by design — backend.Summarize)
	ThoughtIDs []int  // the branch's thought IDs in order, for on-demand traverse/expand
	Resolution string // the branch's resolution AT capture ("EXPANDED" | "COMPRESSED")
}

// KnowledgeRef is the L3 layer: a declaration of a paired knowledge index to pull from the
// knowledge store (RAG-like) and hand to the worker. It is a lightweight ref ONLY — the actual
// store pull (and the §3.12 context-prior pre-pull) is wired later. IndexID names the index a
// definition declares; Query is the active-branch-derived query to retrieve against.
type KnowledgeRef struct {
	IndexID string // the knowledge index the operator/sub-agent definition declares
	Query   string // the retrieval query derived from the trigger context
}

// Context is the three-layer, instance-only material a Capability captures and passes down. It is
// never seeded and never minted (§2.4) — it is produced fresh from the runtime graph each trigger.
//
// L1 freezes at capture; L2 is appended to during the run (Set); L3 is a list of declared knowledge
// refs (a definition may pair more than one index). The goal that drove the trigger is carried for
// the workers that concretize against this context.
type Context struct {
	Goal string         // the episode/sub-goal that drove the trigger
	L1   L1Snapshot     // compressed active-branch snapshot (frozen at trigger time)
	L2   map[string]any // runtime-derived, written back during the run (per-instance, non-nil)
	L3   []KnowledgeRef // paired-knowledge index refs declared by the definition

	// Snapshot is the L1 spine MATERIALISED: a value copy of the ENTIRE active branch's thoughts at
	// trigger time, frozen. It is what a worker CONSUMES instead of the ≤5 ContextSliceDefault window
	// (subagent.go) — the gap-2 fix: a worker staffed under this Context sees the whole branch the
	// trigger fired on, not a starved 5-thought tail (the flaky-grounding root cause). The IDs are also
	// in L1.ThoughtIDs (for on-demand re-traverse of the LIVE graph, OPT-3); this is the FROZEN copy a
	// worker reads with no graph handle. nil ⇒ no snapshot captured (an empty/graph-less capture).
	Snapshot []types.Thought
}

// CaptureContext snapshots the live thought graph's ACTIVE branch into a fresh Context (the L1
// snapshot). It is the Capability's context-capture step (§3.3 (a)): freeze the active branch's
// gist + thought IDs at trigger time so the material the stack concretizes against does not drift
// as the conscious keeps thinking.
//
// The gist is produced by the backend (the same lossy Summarize the graph's Compress uses), so an
// EXPANDED active branch still yields a one-line spine for L1 without forcing the live branch into
// COMPRESSED state — the snapshot is compressed, the live branch is left untouched. knowledge may
// be nil (no paired index declared yet). A nil graph or backend yields an empty-but-valid Context
// (L2 always non-nil) so callers never nil-panic on capture.
func CaptureContext(g *graph.ThoughtGraph, backend backends.Backend, goal string,
	knowledge []KnowledgeRef) *Context {
	ctx := &Context{
		Goal: goal,
		L2:   map[string]any{},
		L3:   append([]KnowledgeRef(nil), knowledge...),
	}
	if g == nil {
		return ctx
	}
	active := g.Active()
	if active == nil {
		return ctx
	}
	ids := append([]int(nil), active.ThoughtIDs...)
	// Materialise the FULL active branch (value copies) at trigger time — the frozen snapshot a worker
	// consumes instead of the ≤5 slice (gap 2). BranchThoughts already returns value copies, so the
	// snapshot does not alias the live graph (later thinking never mutates it).
	ctx.Snapshot = g.BranchThoughts(active.ID)
	gist := ""
	if active.Summary != nil {
		// already-compressed branch: reuse its stored gist (avoids a redundant backend call).
		gist = *active.Summary
	} else if backend != nil {
		gist = backend.Summarize(ctx.Snapshot)
	}
	ctx.L1 = L1Snapshot{
		BranchID:   active.ID,
		Gist:       gist,
		ThoughtIDs: ids,
		Resolution: active.Resolution.String(),
	}
	return ctx
}

// Quality scores the Context's richness as a [0,1] CONTEXT PRIOR (§3.12 — the mintable LESSON: a context
// that carried a substantive snapshot + grounding material + declared knowledge is a better prior than a
// thin one). Deterministic, no clock/backend. This is the term the §3.12 context mint gate adds ON TOP of
// the value+frequency the convert mint gate uses today (convert.go:525) — the mint sites (convert.
// Consolidate, OperatorRegistry.Mint, SkillRegistry.Mint) read it to factor CONTEXT quality into the keep
// decision, so a lesson learned in a rich context outranks one learned in a thin one.
func (c *Context) Quality() float64 {
	if c == nil {
		return 0
	}
	score := 0.0
	if strings.TrimSpace(c.L1.Gist) != "" {
		score += 0.4 // a real spine snapshot
	}
	if n := len(c.L1.ThoughtIDs); n > 0 {
		d := float64(n) * 0.08 // saturating grounding material: ~1 thought 0.08 … 5+ thoughts 0.4
		if d > 0.4 {
			d = 0.4
		}
		score += d
	}
	if len(c.L3) > 0 {
		score += 0.2 // declared paired-knowledge
	}
	if score > 1 {
		score = 1
	}
	return score
}

// WorkerContext returns the REAL thoughts of the frozen branch snapshot a worker concretizes against —
// the gap-2 replacement for SubAgent.ContextSliceDefault's ≤5 window. It drops METACOG and recap-preamble
// thoughts exactly as ContextSlice does (so the worker sees the substantive line, not the bookkeeping),
// but keeps the WHOLE frozen branch rather than clipping to the last 5: this is the "context SCALES with
// the branch instead of clipping to a fixed tail" guarantee (§3.11). A nil/empty Snapshot returns nil, so
// a worker with no captured Context falls back to its own ContextSliceDefault (byte-identical). The
// returned slice is fresh (it does not alias the snapshot), so a caller cannot mutate the frozen spine.
func (c *Context) WorkerContext() []types.Thought {
	if c == nil || len(c.Snapshot) == 0 {
		return nil
	}
	out := make([]types.Thought, 0, len(c.Snapshot))
	for _, t := range c.Snapshot {
		if t.Source != types.METACOG && !strings.HasPrefix(t.Text, types.RecapPrefix) {
			out = append(out, t)
		}
	}
	return out
}

// Set writes a key into the L2 (runtime-derived) layer — what a workflow / sub-agent does as the
// program executes to hand material forward to a later operator. L2 is per-instance and non-nil
// (allocated at capture), so this never nil-panics.
func (c *Context) Set(key string, val any) {
	if c.L2 == nil {
		c.L2 = map[string]any{}
	}
	c.L2[key] = val
}

// Get reads a key from the L2 (runtime-derived) layer, ok=false on a miss.
func (c *Context) Get(key string) (any, bool) {
	if c.L2 == nil {
		return nil, false
	}
	v, ok := c.L2[key]
	return v, ok
}

// ExpandIDs returns the L1 snapshot's thought IDs — the addresses a worker traverses/expands on
// demand (OPT-3). It is the snapshot's stable spine; the gist is the lossy view, these IDs are the
// way back to full detail through the live graph. The returned slice is a copy so a caller cannot
// mutate the snapshot's spine.
func (c *Context) ExpandIDs() []int {
	return append([]int(nil), c.L1.ThoughtIDs...)
}

// Expand materialises the full thoughts behind the L1 snapshot's IDs from the LIVE graph — the
// on-demand traverse (OPT-3): the snapshot froze the gist + the IDs, this walks those IDs back to
// the live nodes when a worker needs detail the gist compressed away. A nil graph or a missing node
// is skipped (the snapshot's IDs are stable, but a node could be superseded), so the result is the
// subset still resolvable. The live graph is read-only here.
func (c *Context) Expand(g *graph.ThoughtGraph) []types.Thought {
	if g == nil {
		return nil
	}
	out := make([]types.Thought, 0, len(c.L1.ThoughtIDs))
	for _, id := range c.L1.ThoughtIDs {
		if node, ok := g.Nodes[id]; ok && node != nil {
			out = append(out, *node)
		}
	}
	return out
}
