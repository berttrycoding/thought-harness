// gateroute.go — the action redesign's two-axis taxonomy + gate ROUTER (docs/cognition/
// 03-action.md §1/§2/§3/§4/§8). This is a package-local ADDITION alongside the existing executor —
// it does NOT replace the gate pipeline in executor.go. It is the pure-classification half of the
// "gate = router + conscious-set bounds" model; the engine wires it into the watched seam / executor
// later (it imports nothing but stdlib + this package's existing types).
//
// THE CUT (03 §1): the subconscious<->conscious boundary is EFFECT on SUBSTRATE, not "who holds a
// tool". A tool call is classified on two independent axes —
//
//	Operation = inspect | mutate | execute   (does it read, write, or run?)
//	Reach     = self | localWorld | external  (what does it touch — the self-substrate, the local
//	                                            world, or the network?)
//
// — and the gate routes (Operation x Reach) into one of three gating CLASSES (03 §2):
//
//	LocalSense  — inspect on the machine            -> subconscious, FREE (frictionless, still grounded)
//	DistalSense — inspect over the network          -> subconscious + automatic, BUDGETED by a conscious
//	                                                    policy + quota (offline-safe)
//	WorldChange — mutate / external write           -> CONSCIOUS, per-act decision, witnessed at the
//	                                                    watched seam
//
// This file owns the operation x reach taxonomy ONLY as a Go enum that mirrors the category-registry
// seed sets — the owner-of-record for the taxonomy is 01-subconscious §3.10a (the growable Category
// registry); this is its in-code projection so the router can switch on it. The seed wire strings are
// the §3.10a values ("inspect"/"mutate"/"execute" x "self"/"local"/"external"); the Reach Go constant
// for the local-world reach is LocalWorld to read unambiguously in code (its wire string stays "local").
package action

import "strings"

// Operation is the first taxonomy axis: what a tool call DOES to its target (03 §8; the §3.10a
// "tool operation" seed set). Independent of reach — a mutate on the network and a mutate on disk are
// the same operation, different reach.
type Operation int

const (
	// OpInspect reads / senses; it changes nothing. The basis of perception (03 §4): a read is sensing,
	// never a conscious-decided action.
	OpInspect Operation = iota
	// OpMutate writes / alters its target — the operation that, on the WORLD, IS action (03 §1).
	OpMutate
	// OpExecute runs something. Whether it is sense or action is decided by the SANDBOX, not the
	// operation (03 §6): in-sandbox run = perception (a grounding probe); escaping run = world-change.
	OpExecute
)

// String returns the §3.10a wire value for the operation (so a serialized category tag round-trips).
func (o Operation) String() string {
	switch o {
	case OpInspect:
		return "inspect"
	case OpMutate:
		return "mutate"
	case OpExecute:
		return "execute"
	default:
		return "inspect"
	}
}

// Reach is the second taxonomy axis: WHAT a tool call touches (03 §8; the §3.10a "tool reach" seed
// set self/local/external). Independent of operation.
type Reach int

const (
	// ReachSelf is the self-substrate — the registries, memory, the thought graph (03 §1, the SELF
	// column). A WRITE here is self-modification, owned by the self-improvement loop + self-access
	// ladder — NEVER an action tool (the §4 invariant; see TargetsSelfSubstrate).
	ReachSelf Reach = iota
	// ReachLocalWorld is the local world — the repo, the filesystem, the sandbox: reachable without the
	// network. Its §3.10a wire value is "local". Named LocalWorld in code to read unambiguously next to
	// ReachSelf / ReachExternal.
	ReachLocalWorld
	// ReachExternal is the network — services, remote APIs, anything off-machine (the §3.10a "external"
	// reach). Distal: reads are budgeted (03 §7), writes are world-change.
	ReachExternal
)

// String returns the §3.10a wire value for the reach (note ReachLocalWorld -> "local").
func (r Reach) String() string {
	switch r {
	case ReachSelf:
		return "self"
	case ReachLocalWorld:
		return "local"
	case ReachExternal:
		return "external"
	default:
		return "local"
	}
}

// ToolClass is the resulting gating discipline a call routes into (03 §2). One of three; every routed
// call is exactly one.
type ToolClass int

const (
	// ClassLocalSense — inspect on the machine: subconscious, FREE (03 §2 row 1). Frictionless; still
	// grounded (never-fabricate applies to all three classes).
	ClassLocalSense ToolClass = iota
	// ClassDistalSense — inspect over the network: subconscious + automatic, BUDGETED by the conscious
	// network policy + quota (03 §2 row 2, §7). Offline-safe.
	ClassDistalSense
	// ClassWorldChange — mutate / external write / sandbox-escaping execute: CONSCIOUS, per-act decision,
	// witnessed at the watched seam (03 §2 row 3). The only class that "counts as action".
	ClassWorldChange
)

// String returns a stable label for the class (for events / traces / tests).
func (c ToolClass) String() string {
	switch c {
	case ClassLocalSense:
		return "local_sense"
	case ClassDistalSense:
		return "distal_sense"
	case ClassWorldChange:
		return "world_change"
	default:
		return "world_change"
	}
}

// OperationWireValues and ReachWireValues are the CANONICAL §3.10a tool-taxonomy wire strings, in their
// enum order. They are the single in-code source of the two action axes (gap 7) — the action gate is the
// owner of the in-code projection (this file's header), and the eval.CategoryRegistry SEEDS its
// tool-operation / tool-reach facets FROM these (eval/category.go, eval imports action), so the two
// taxonomies can never drift (TestCategoryRegistrySeedsFromActionTaxonomy pins the agreement). The action
// gate-router routes on these directly; the registry is the growable, mintable owner-of-record that holds
// the same seed set + the refine/mint loop.
var OperationWireValues = []string{OpInspect.String(), OpMutate.String(), OpExecute.String()}
var ReachWireValues = []string{ReachSelf.String(), ReachLocalWorld.String(), ReachExternal.String()}

// TaxClass is a tool's position on the two axes — its (Operation x Reach) classification. It is the
// growable category tag projected into Go: a tool carries one. The classifier (ClassifyFlatCategory)
// maps the old flat category set onto it; new tools can be tagged directly.
type TaxClass struct {
	Op    Operation
	Reach Reach
}

// String renders the pair as "operation/reach" (e.g. "inspect/local"), the §3.10a wire shape.
func (t TaxClass) String() string { return t.Op.String() + "/" + t.Reach.String() }

// --- the classifier: flat category set -> (Operation x Reach) ------------------------------------

// ClassifyFlatCategory maps the historical FLAT tool-category tags {inspect, mutate, execute,
// external} (03 §8 "code today" — a flat set that CONFLATED operation + reach) onto the two-axis
// (Operation x Reach) model that supersedes it (01 §3.10a). The mapping, per the §8 delta row:
//
//	"inspect"  -> inspect / localWorld   (a read of the local world — the common case)
//	"mutate"   -> mutate  / localWorld   (a write to the local world — repo/fs)
//	"execute"  -> execute / localWorld   (run on the machine; sandbox decides sense-vs-act at routing)
//	"external" -> inspect / external     (the flat tag meant "network read" — a distal SENSE; a network
//	                                      WRITE was never a distinct flat tag, it folded into "mutate")
//
// It returns ok=false for an unrecognized tag so the caller can decline rather than guess (the flat
// set is closed; an unknown tag is a bug to surface, not silently route). Case/space tolerant.
func ClassifyFlatCategory(flat string) (TaxClass, bool) {
	switch strings.ToLower(strings.TrimSpace(flat)) {
	case "inspect":
		return TaxClass{Op: OpInspect, Reach: ReachLocalWorld}, true
	case "mutate":
		return TaxClass{Op: OpMutate, Reach: ReachLocalWorld}, true
	case "execute":
		return TaxClass{Op: OpExecute, Reach: ReachLocalWorld}, true
	case "external":
		// The flat "external" tag was a network READ (distal sense). Its reach is external; its
		// operation is inspect. A network WRITE was not its own flat tag (it lived under "mutate"); the
		// two-axis model expresses that as {mutate, external} directly, no flat equivalent.
		return TaxClass{Op: OpInspect, Reach: ReachExternal}, true
	default:
		return TaxClass{}, false
	}
}

// classifyTool returns a tool's (operation x reach) taxonomy from the tool's OWN Category() tag (gap 6 —
// the tool is the owner of its taxonomy, not a name switch). This is the preferred path the executor's
// router stage uses (it has the resolved tool object): a new/minted tool ships its category and is routed
// correctly with no edit to a hardcoded switch. classifyCall(name) below stays the name-only FALLBACK for
// a caller that has only a name (no tool object) — a bench / a path that never resolved the tool.
func classifyTool(t Tool) TaxClass { return t.Category() }

// classifyCall maps a builtin tool NAME onto its (operation x reach) taxonomy — the NAME-ONLY FALLBACK
// for a caller that does not hold the tool object (the executor now prefers classifyTool, gap 6). It
// mirrors the builtins' own Category() tags so a name-only classification agrees with the tool-owned one
// (the TestClassifyCallMatchesToolCategory gate pins that they never drift):
//
//	FileModifyTools (write_file, edit_file) -> mutate  / localWorld  (a write — world-change)
//	CommandTools    (run_shell, run_tests)  -> execute / localWorld  (a run — sense unless it escapes)
//	everything else                         -> inspect / localWorld  (a read — free local perception)
//
// A `mutate` whose path targets the self-substrate is additionally refused by RefuseSelfMutation (§4) —
// classification only assigns the taxonomy; the refusal is a separate, harder layer in the executor.
func classifyCall(name string) TaxClass {
	switch {
	case FileModifyTools[name]:
		return TaxClass{Op: OpMutate, Reach: ReachLocalWorld}
	case CommandTools[name]:
		return TaxClass{Op: OpExecute, Reach: ReachLocalWorld}
	default:
		return TaxClass{Op: OpInspect, Reach: ReachLocalWorld}
	}
}

// ClassifyToolName is the exported name-only classifier (classifyCall) for a caller OUTSIDE this package
// that has only a builtin tool NAME and no tool object — e.g. the gate-router bench's mockEffect, which
// carries a real builtin name and must taxonomize exactly as the real builtin's Category() would. A
// caller that holds the tool object should use the tool's own Category() (gap 6); this is the fallback.
func ClassifyToolName(name string) TaxClass { return classifyCall(name) }

// --- the router: (Operation x Reach) + bounds -> ToolClass ---------------------------------------

// RouteBounds is the conscious-set ceiling input the router reads (03 §3): the run's Scope ceiling,
// reduced here to the two knobs the routing decision needs. It is supplied BY the conscious gate; a
// worker can never widen it at runtime (03 §3 — only the conscious gate widens the ceiling).
type RouteBounds struct {
	// NetworkEnabled is the conscious network POLICY (03 §7 top level): is distal sensing on at all?
	// Offline-safe default is false — the zero value runs the system on local perception only.
	NetworkEnabled bool
	// NetworkQuota is the remaining distal-sense budget (03 §7): the number of network reads still
	// permitted under the policy. <= 0 means the budget is spent (or none was granted). The router
	// reports exhaustion via RouteDecision.QuotaExceeded; it does not itself decrement (the executor /
	// scheduler owns spending against the quota).
	NetworkQuota int
}

// RouteDecision is the router's verdict for one call: the gating class it falls into, plus the two
// gate-relevant flags the executor acts on. It is pure data — the router decides nothing about
// whether to RUN (that is the executor's job, given this verdict + the per-act conscious authorization).
type RouteDecision struct {
	// Class is the gating discipline (local-sense free / distal-sense budgeted / world-change gated).
	Class ToolClass
	// NeedsConsciousAuthor is true iff the class is WorldChange — i.e. the conscious must have AUTHORED
	// this act (03 §3, §5: writes require the conscious's per-act authorization). The executor refuses
	// a world-change that the conscious did not author.
	NeedsConsciousAuthor bool
	// QuotaExceeded is true iff this is a DistalSense call but the network policy is off OR the budget
	// is spent — the executor then declines the distal read (offline-safe: it falls back to local
	// perception, 03 §7). Never set for LocalSense / WorldChange.
	QuotaExceeded bool
}

// Route classifies a call by its (Operation x Reach) taxonomy against the conscious-set bounds and
// returns the gating verdict (03 §2/§3). The routing table:
//
//	inspect / self            -> LocalSense  (introspection — a free self-read; 03 §1 SELF/READ quadrant)
//	inspect / localWorld      -> LocalSense  (free local perception)
//	inspect / external        -> DistalSense (budgeted network read; QuotaExceeded if policy off / spent)
//	execute / self|localWorld -> LocalSense  (run-to-measure inside the sandbox = a grounding probe;
//	                                          03 §6 — the SANDBOX, not this router, distinguishes an
//	                                          escaping run, which the caller flags as WorldChange)
//	execute / external        -> WorldChange (a run with external effect escapes the sandbox = action)
//	mutate / *                -> WorldChange (any write is a world-change; 03 §1/§2 — and a self-mutate
//	                                          is the §4 invariant violation the executor must REFUSE
//	                                          separately, see TargetsSelfSubstrate / RefuseSelfMutation)
//
// NOTE on execute (03 §6): a non-network `execute` defaults to LocalSense here because the in-sandbox
// run is a perception. The sandbox-ESCAPE that turns it into action is not visible to a pure
// (operation x reach) router — the executor marks an escaping run as external reach (or sets the
// world-change flag) before routing. This router encodes the rule; the sandbox boundary supplies the
// discriminator (kept as a documented seam, not silently collapsed).
func Route(tc TaxClass, bounds RouteBounds) RouteDecision {
	switch tc.Op {
	case OpMutate:
		// Any write is a world-change, regardless of reach. (A self-mutate is additionally an invariant
		// violation — see the §4 predicate — but it is STILL a world-change-class gated call here; the
		// refusal is a separate, harder stop layered by the executor.)
		return RouteDecision{Class: ClassWorldChange, NeedsConsciousAuthor: true}

	case OpExecute:
		if tc.Reach == ReachExternal {
			// A run with external / persistent effect escapes the sandbox = world-change = action (03 §6).
			return RouteDecision{Class: ClassWorldChange, NeedsConsciousAuthor: true}
		}
		// Run-to-measure inside the sandbox = sense (a grounding probe). Free, local (03 §6).
		return RouteDecision{Class: ClassLocalSense}

	default: // OpInspect — a read changes nothing; it is perception (03 §4).
		if tc.Reach == ReachExternal {
			// Distal sense — budgeted by the conscious network policy + quota (03 §7). Offline-safe: if
			// the policy is off or the budget is spent, flag QuotaExceeded so the executor declines and
			// the system falls back to local perception (it does not error).
			dec := RouteDecision{Class: ClassDistalSense}
			if !bounds.NetworkEnabled || bounds.NetworkQuota <= 0 {
				dec.QuotaExceeded = true
			}
			return dec
		}
		// inspect / self or inspect / localWorld -> free local perception.
		return RouteDecision{Class: ClassLocalSense}
	}
}

// --- the §4 invariant: an action (mutate) tool never targets the self-substrate -------------------

// selfSubstrateRoots are the path-namespace prefixes that name the SELF-substrate — the registries,
// memory, and the thought-graph state the system persists about ITSELF (03 §1 SELF column; §4
// invariant). A write whose target resolves under one of these is self-modification, which must go
// through the self-improvement loop + self-access ladder (01 §2.6/§2.8) — NEVER an action tool.
//
// This is the "path-namespace convention" arm of the §4-open mechanism ("a registered set of
// self-substrates / a path-namespace convention / a tool capability flag"). The convention: the
// system's own state lives under a `data/` registry/memory tree and `runs/` audit tree; the action
// layer only ever touches the WORLD outside it. Kept as a package var so the engine can extend it for
// a concrete state-dir layout (e.g. a custom --state path) without editing the predicate.
var selfSubstrateRoots = []string{
	"data/registry", // the per-registry stores (specialists, skills, operators, memory)
	"data/memory",   // explicit memory registry
	"data/graph",    // persisted thought-graph state
	"data/state",    // generic engine self-state
	"runs/",         // the append-only run audit (invalidate-not-delete) — system's own ledger
	".thought",      // a dotdir convention for self-state if adopted
}

// SelfSubstrateRoots returns a copy of the registered self-substrate path prefixes (read-only view for
// the engine / tests). Mutating the returned slice does not affect the package set.
func SelfSubstrateRoots() []string {
	out := make([]string, len(selfSubstrateRoots))
	copy(out, selfSubstrateRoots)
	return out
}

// RegisterSelfSubstrateRoot adds a path prefix to the self-substrate set (03 §4-open: "a registered
// set of self-substrates"). Idempotent — a duplicate prefix is ignored. The engine calls this once at
// startup if it persists self-state under a non-default root (e.g. a custom --state dir), so the §4
// invariant covers the actual layout. Empty/whitespace prefixes are ignored.
func RegisterSelfSubstrateRoot(prefix string) {
	p := strings.TrimSpace(prefix)
	if p == "" {
		return
	}
	for _, e := range selfSubstrateRoots {
		if e == p {
			return
		}
	}
	selfSubstrateRoots = append(selfSubstrateRoots, p)
}

// TargetsSelfSubstrate reports whether a write target (a path or a capability tag) names the
// SELF-substrate (03 §4 invariant). It is the predicate the executor consults to REFUSE a self-write
// from an action tool — "an action (mutate) tool never targets the self-substrate (memory/registries/
// graph)."
//
// It checks two arms of the §4-open mechanism:
//   - the CAPABILITY-FLAG arm: a target explicitly tagged self-reach ("self", "self:..." or the
//     §3.10a reach value) is the self-substrate by declaration;
//   - the PATH-NAMESPACE arm: a normalized target path that lies under a registered self-substrate
//     root (selfSubstrateRoots) is the self-substrate by location.
//
// Path matching is prefix-based on a forward-slash-normalized, cleaned target, anchored at a path
// segment boundary so "data/registry-claude/..." matches the "data/registry" root but
// "datacenter/..." does not match "data/". A relative or absolute target is normalized the same way
// (a leading "/" or "./" is trimmed for the comparison). The check is conservative: it errs toward
// FLAGGING (refusing) an ambiguous self-looking target rather than letting a self-write slip through.
func TargetsSelfSubstrate(target string) bool {
	t := strings.TrimSpace(target)
	if t == "" {
		return false
	}

	// Capability-flag arm: an explicit self-reach tag.
	low := strings.ToLower(t)
	if low == "self" || strings.HasPrefix(low, "self:") || strings.HasPrefix(low, "self/") {
		return true
	}

	// Path-namespace arm. Normalize: backslashes -> slashes, trim a leading scheme-less root / "./".
	norm := strings.ReplaceAll(t, "\\", "/")
	norm = strings.TrimPrefix(norm, "./")
	norm = strings.TrimPrefix(norm, "/")
	norm = strings.ToLower(norm)
	if norm == "" {
		return false
	}
	for _, root := range selfSubstrateRoots {
		r := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.ReplaceAll(root, "\\", "/"), "./"), "/"))
		if r == "" {
			continue
		}
		// Match exactly the root, or the root followed by a path-segment boundary "/", so a root like
		// "data/registry" matches "data/registry" and "data/registry/foo" but NOT "data/registryfoo".
		// A root that already ends in "/" (e.g. "runs/") is a pure prefix match by intent.
		if strings.HasSuffix(r, "/") {
			if strings.HasPrefix(norm, r) {
				return true
			}
			continue
		}
		if norm == r || strings.HasPrefix(norm, r+"/") {
			return true
		}
	}
	return false
}

// RefuseSelfMutation reports whether a call MUST be refused by the §4 invariant: it is a mutate
// (write) whose target is the self-substrate. This is the composed predicate the executor calls — a
// world-change route is allowed to touch the WORLD, but a write aimed at the self-substrate is never
// an action and must be refused (it belongs to the self-improvement loop, 01 §2.6/§2.8). Only OpMutate
// triggers it: an inspect/execute on the self-substrate is introspection / a self-probe, not a write.
func RefuseSelfMutation(op Operation, target string) bool {
	return op == OpMutate && TargetsSelfSubstrate(target)
}
