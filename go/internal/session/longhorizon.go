// longhorizon.go is long-horizon state + durability (P3.9). A long-horizon session keeps its own BOUNDED
// SCRATCH context — standard harness context-management, explicitly NOT the navigable thought graph
// (that is the Conscious layer's one steerable stream). And a long-horizon session is a STANDING FORK:
// it consumes schedulability (U) every tick it is alive, so the regulator must bound how many run
// concurrently (U ≤ 1) exactly as for spawned Sessions, and each MUST carry a guaranteed termination
// (the lifecycle invariant, P3.8). "long_horizon" means bounded multi-tick, never unbounded.
package session

// Scratch is a long-horizon session's bounded working context: at most Cap recent entries, oldest
// evicted first. It is plain context-management infrastructure (like internal/zoommem), usable by any
// worker — NOT the thought graph (no branch/merge/rerank/focus). That separation is the whole point:
// only the one Conscious stream needs a navigable graph; a watcher/monitor just needs bounded recall.
type ScratchCtx struct {
	Cap     int
	entries []string
}

// NewScratch builds a bounded scratch context (cap<=0 → unbounded is rejected: 1).
func NewScratch(cap int) *ScratchCtx {
	if cap <= 0 {
		cap = 1
	}
	return &ScratchCtx{Cap: cap}
}

// Add appends an entry, evicting the oldest when over capacity (the bound holds).
func (s *ScratchCtx) Add(e string) {
	s.entries = append(s.entries, e)
	if len(s.entries) > s.Cap {
		s.entries = s.entries[len(s.entries)-s.Cap:]
	}
}

// Entries returns the current (bounded) context, oldest-first.
func (s *ScratchCtx) Entries() []string { return append([]string(nil), s.entries...) }

// Len is the current entry count (always <= Cap).
func (s *ScratchCtx) Len() int { return len(s.entries) }

// StandingLoad is the standing-fork accounting: a long-horizon session consumes schedulability every
// tick it is alive, so U = (live standing sessions) / focusCapacity. schedulable reports U ≤ 1 — the
// regulator admits a new standing session only while this holds. Single-shot / bounded sessions are not
// standing forks (they are transient), so they do not count toward U.
func StandingLoad(sessions []*Session, focusCapacity int) (u float64, schedulable bool) {
	if focusCapacity <= 0 {
		focusCapacity = 1
	}
	standing := 0
	for _, s := range sessions {
		if s.Spec.Horizon == LongHorizon {
			standing++
		}
	}
	u = float64(standing) / float64(focusCapacity)
	return u, u <= 1.0
}

// AllTerminate reports whether every session in the set carries a guaranteed termination (a valid
// lifecycle) — the durability precondition for admitting standing agents at all.
func AllTerminate(sessions []*Session) bool {
	for _, s := range sessions {
		if ok, _ := s.Spec.Valid(); !ok {
			return false
		}
	}
	return true
}
