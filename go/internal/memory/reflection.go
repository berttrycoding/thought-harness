// reflection.go is the idle-time consolidation pass (P6.2 / memory-stack §4): distil EPISODES into
// SEMANTIC beliefs, off the hot path. It is the declarative analogue of convertibility (which compiles
// procedural skills): a grounded, high-value episode's worked conclusion becomes a reusable belief, so
// a later related task can recall the lesson instead of re-deriving it (transfer).
//
// It is BEHAVIOURALLY GATED to avoid injecting false beliefs: only a GROUNDED episode whose grounded
// value clears the floor distils a belief (never-fabricate + a value gate), and an episode the system
// only BELIEVED (low value) or that reality refuted contributes nothing. A belief later contradicted by
// a refuting episode is INVALIDATED (bi-temporal, not deleted) — so reflection can never leave a stale
// false belief standing.
package memory

// Reflect distils beliefs from the episodic store into the semantic store. For each grounded episode
// whose value >= valueFloor, its worked outcome becomes a (currently-valid) belief; a grounded but
// LOW-value (refuted) episode INVALIDATES any standing belief that repeats the same outcome, so the
// stale belief stops surfacing as current. Returns the number of new beliefs distilled. Idempotent
// across calls via the already-present check (a belief is not distilled twice).
func Reflect(epis *EpisodicRegistry, sem *SemanticRegistry, valueFloor float64, tick int) int {
	distilled := 0
	for _, e := range epis.episodes {
		if !e.Grounded || e.Outcome == "" {
			continue
		}
		if e.Value < valueFloor {
			// reality did not back this line — make sure no belief asserting it stands as current.
			sem.Invalidate(e.Outcome, tick)
			continue
		}
		if beliefExists(sem, e.Outcome) {
			continue // already distilled (idempotent)
		}
		sem.Record(Belief{
			Statement: e.Outcome,
			Entities:  e.Entities,
			Source:    "reflection:" + e.Goal,
			Grounded:  true, // distilled only from a grounded episode (never-fabricate holds)
			ValidFrom: tick,
		})
		distilled++
	}
	return distilled
}

// beliefExists reports whether a currently-valid belief with this exact statement is already stored.
func beliefExists(sem *SemanticRegistry, statement string) bool {
	for _, b := range sem.beliefs {
		if b.ValidTo == 0 && b.Statement == statement {
			return true
		}
	}
	return false
}
