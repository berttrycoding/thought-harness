// Package resolve holds the uniform Resolve spine every registry runs (registry-architecture.md / P2.1):
// SEARCH → reuse-or-create → VERIFY → dedup/STORE. The spine is identical across registries; only the
// Create and Verify steps are polymorphic, which is what distinguishes the two registry FAMILIES:
//
//   - CAPABILITY registries (skills / operators / workflows) — Create SYNTHESISES a new item when none
//     is found (reuse-or-CREATE). Library-first, synthesise-fallback.
//   - KNOWLEDGE registries (episodic / semantic / experiment) — Create RECORDS only a grounded fact and
//     NEVER fabricates one (reuse-or-RECORD). A query with no real grounding resolves to Failed, not a
//     made-up item.
//
// Tools are the one capability registry whose Create is reuse-or-ESCALATE (a new tool is human-gated
// code, never auto-minted) — modelled by a Create that always returns ok=false.
package resolve

// Registry is the uniform contract Resolve runs over. T is the registry's item type.
type Registry[T any] interface {
	// Find searches the existing library for an item satisfying the query (reuse).
	Find(query string) (item T, found bool)
	// Create builds a new item for the query — SYNTHESISE (capability) or RECORD-if-grounded
	// (knowledge) or refuse (tools / ungrounded knowledge). ok=false means "could not create".
	Create(query string) (item T, ok bool)
	// Verify applies the two-layer discipline before trust (ok + reason). Never mutates.
	Verify(item T) (ok bool, reason string)
	// Store deduplicates + persists the verified item into the library.
	Store(item T)
}

// Outcome reports what Resolve did.
type Outcome int

const (
	// Failed: no existing match and creation/verification did not succeed (incl. never-fabricate refusal).
	Failed Outcome = iota
	// Reused: an existing library item satisfied the query.
	Reused
	// Created: a new item was synthesised/recorded, verified, and stored.
	Created
)

func (o Outcome) String() string {
	switch o {
	case Reused:
		return "reused"
	case Created:
		return "created"
	default:
		return "failed"
	}
}

// Resolve is the uniform spine: reuse an existing match if found; else create, verify, and store a new
// one. A creation that fails (or a knowledge registry refusing to fabricate) and a verification that
// fails both yield Failed, leaving the library untouched. The reason (from Verify) is returned for the
// trace; it is "" on Reused/Created.
func Resolve[T any](r Registry[T], query string) (item T, outcome Outcome, reason string) {
	if it, found := r.Find(query); found {
		return it, Reused, ""
	}
	it, ok := r.Create(query)
	if !ok {
		var zero T
		return zero, Failed, "no existing match and could not create"
	}
	if vok, why := r.Verify(it); !vok {
		var zero T
		return zero, Failed, why
	}
	r.Store(it)
	return it, Created, ""
}
