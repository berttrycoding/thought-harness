package action

import "strings"

// protectedcore.go is the PROTECTED CORE — the immutable kernel (01-subconscious.md §2.8, 00-INDEX
// "Protected core"): the components the system must be UNABLE to optimize away or edit, so level-3/4
// self-improvement cannot wirehead. Per §2.8 the core is: the durability invariants / regulator, the
// hidden-seam discipline, the Filter honesty floor, the eval gate + its core measuring sticks, and the
// conscience / creator-assigned identity standard. It is READ-ONLY TO THE SYSTEM ENTIRELY — unlike the
// general self-substrate (§4), which the self-improvement loop MAY modify under eval + keep-or-revert,
// the protected core is off-limits to every loop, including self-improvement. This is the anti-wireheading
// invariant: the system must not be able to edit its own stick so bad changes pass.
//
// Runtime enforcement is path-based at the persisted arm (the in-memory components are immutable by
// construction — no mutator is exposed). A write whose target lands under a protected-core root is refused
// with a distinct anti-wireheading reason, regardless of authoring.

// protectedCoreRoots are the persisted-state path prefixes of the immutable kernel. Distinct from
// selfSubstrateRoots (which the self-improvement loop may revise): these are read-only to the SYSTEM.
var protectedCoreRoots = []string{
	".thought/identity",       // the creator-assigned, agent-immutable identity + conscience standard (02 §7.3)
	"data/registry/eval-core", // the eval gate's CORE measuring sticks (anti-wireheading — §2.8 (4))
	"data/core",               // a generic protected-kernel root (regulator/durability/Filter-floor state)
}

// ProtectedCoreRoots returns a copy of the protected-core path prefixes (read-only view).
func ProtectedCoreRoots() []string {
	out := make([]string, len(protectedCoreRoots))
	copy(out, protectedCoreRoots)
	return out
}

// RegisterProtectedCoreRoot adds a path prefix to the protected-core set. Idempotent; empty/whitespace
// ignored. The engine calls this if it persists a core component under a non-default root.
func RegisterProtectedCoreRoot(prefix string) {
	p := strings.TrimSpace(prefix)
	if p == "" {
		return
	}
	for _, e := range protectedCoreRoots {
		if e == p {
			return
		}
	}
	protectedCoreRoots = append(protectedCoreRoots, p)
}

// TargetsProtectedCore reports whether a target path lies under a protected-core root. Same normalization
// + segment-boundary matching as TargetsSelfSubstrate (a root "data/core" matches "data/core/x" but not
// "data/core-extra"); conservative — it errs toward flagging an ambiguous core-looking target.
func TargetsProtectedCore(target string) bool {
	t := strings.TrimSpace(target)
	if t == "" {
		return false
	}
	norm := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.ReplaceAll(t, "\\", "/"), "./"), "/"))
	if norm == "" {
		return false
	}
	for _, root := range protectedCoreRoots {
		r := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(strings.ReplaceAll(root, "\\", "/"), "./"), "/"))
		if r == "" {
			continue
		}
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

// RefuseProtectedCoreMutation reports whether a call MUST be refused by the anti-wireheading invariant: a
// mutate whose target is the protected core. Unlike RefuseSelfMutation (which the self-improvement loop can
// route around under eval gating), this is absolute — no loop may write the core. Only OpMutate triggers
// it (a read / self-probe of the core is introspection, which the meta-Capability needs and §2.8 allows).
func RefuseProtectedCoreMutation(op Operation, target string) bool {
	return op == OpMutate && TargetsProtectedCore(target)
}
