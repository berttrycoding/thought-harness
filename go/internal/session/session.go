// session.go is the Session runtime (P3.3): the generalization of a SubAgent (which runs ONE operator,
// single-shot) to a bounded worker that RunWorkflow — runs a whole sub-workflow as a child episode,
// scheduled by its lifecycle (P3.8). A Session may DISPATCH child Sessions; Dispatch SPAWNS a fresh
// budget rather than nesting, which is what lets the overall tree be large while each session's own tree
// stays bounded (wfproof's proven shape). Two hard bounds make the spawn tree finite and durable:
//
//   - MaxSessionDepth bounds the dispatch chain (a child deeper than this is rejected);
//   - every session carries a lifecycle with a guaranteed termination (Spec.Valid / TickBudget).
//
// This is headless + deterministic (no cc-spawn, no wall clock): the engine drives it in ticks via the
// regulator (which caps concurrent standing sessions at U<=1 — P3.5/P3.9 add the resource accounting).
package session

import (
	"errors"
	"fmt"
)

// MaxSessionDepth bounds how deep a dispatch chain may nest (mirrors wfproof MaxDepth). A Dispatch past
// this is rejected — the spawn tree is bounded.
const MaxSessionDepth = 6

// Session is a bounded worker episode running a (sub-)workflow under a lifecycle.
type Session struct {
	Goal     string
	Spec     Spec
	Depth    int        // 0 == a root session; a child is parent.Depth+1
	Children []*Session // dispatched sub-sessions (the spawn tree)
	Budget   *Budget    // per-session token budget (P3.5); nil == unmetered (lifecycle bound only)
	life     *Lifecycle
}

// NewSession builds a root session for a valid spec (error on an invalid lifecycle).
func NewSession(goal string, spec Spec) (*Session, error) {
	if ok, why := spec.Valid(); !ok {
		return nil, errors.New("invalid session spec: " + why)
	}
	return &Session{Goal: goal, Spec: spec, Depth: 0, life: New(spec)}, nil
}

// Dispatch spawns a CHILD session for a sub-goal (the unified node's dispatch). It is rejected if it
// would exceed MaxSessionDepth (the spawn tree stays bounded) or if the child's lifecycle is invalid
// (no guaranteed termination). The child starts a fresh budget (spawn, not nest).
func (s *Session) Dispatch(subGoal string, spec Spec) (*Session, error) {
	if s.Depth+1 > MaxSessionDepth {
		return nil, fmt.Errorf("dispatch exceeds MaxSessionDepth=%d (spawn tree must stay bounded)", MaxSessionDepth)
	}
	if ok, why := spec.Valid(); !ok {
		return nil, errors.New("child session spec invalid: " + why)
	}
	child := &Session{Goal: subGoal, Spec: spec, Depth: s.Depth + 1, life: New(spec)}
	s.Children = append(s.Children, child)
	return child, nil
}

// Run runs this session's lifecycle to termination, invoking step on each scheduled tick. It is
// guaranteed to terminate (the lifecycle invariant). Returns the terminate reason + the final tick.
func (s *Session) Run(goalMet func(tick int) bool, step func(tick int)) (Terminate, int) {
	return s.life.Run(goalMet, step)
}

// TreeSize counts this session plus every descendant (the whole spawn tree). Always finite (bounded by
// MaxSessionDepth and the per-dispatch admission).
func (s *Session) TreeSize() int {
	n := 1
	for _, c := range s.Children {
		n += c.TreeSize()
	}
	return n
}

// MaxDepthReached returns the deepest Depth in the tree (for the durability accounting).
func (s *Session) MaxDepthReached() int {
	max := s.Depth
	for _, c := range s.Children {
		if d := c.MaxDepthReached(); d > max {
			max = d
		}
	}
	return max
}
