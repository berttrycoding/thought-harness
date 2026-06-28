package convert

// experiment.go — the keep-or-revert PRIMITIVE (cognition redesign slice h).
//
// The repo's whole self-improvement lineage is one move: propose -> measure -> keep-or-revert on a
// scalar metric, KEEP iff the candidate STRICTLY beats the best so far (strict `>`, best-relative).
// It already appears inline in several places (convert.Consolidate's mint/demote gate against the
// MintValue floor `convert.go:510`; the W1 registry ledger's snapshot/revert; a keep-or-revert
// autoresearch discipline; a control-loop gate). This factors that move out as ONE small, tested unit so the
// places that need it stop re-deriving it.
//
// Two callers (02-conscious.md §5.3 + §6 / 01-subconscious.md §3.15, §3.17) reuse the SAME primitive:
//   - the MINTING gate (§3.15): "does this candidate reference beat the best the registry holds?" — the
//     mint/refine decision is keep-or-revert over a reference's measured value.
//   - the ACTIVITY-theta bandit (02 §5.3): treat a parameter vector theta as an arm, run a window,
//     score J(theta) = success - gamma*cost + delta*diversity, KEEP iff J > J_best (strict). The outer
//     learning loop is exactly this primitive over the scalar J.
//
// This is ADDITIVE: the existing mint/demote logic in convert.go is untouched. The primitive is the
// shared shape those callers can adopt; it does not rewrite them.
//
// Determinism: pure scalar comparison, no clock, no RNG — fits the engine's determinism-by-default rule.

// Decision is the keep-or-revert verdict for one proposal.
type Decision int

const (
	// Revert keeps the incumbent best: the candidate did NOT strictly beat it (worse OR a tie).
	Revert Decision = iota
	// Keep adopts the candidate: it STRICTLY beat the best so far, which now becomes the new best.
	Keep
)

// String gives each decision a stable lowercase label (for events / trace rows).
func (d Decision) String() string {
	if d == Keep {
		return "keep"
	}
	return "revert"
}

// Trial is one append-only row of an Experiment's run record: the candidate scored, the decision taken,
// and the best-so-far AFTER that decision. The history is the audit trail (invalidate-not-delete: rows
// are only ever appended).
type Trial struct {
	Candidate float64
	Decision  Decision
	Best      float64
}

// Experiment is the keep-or-revert primitive: it holds the best-so-far on a scalar objective and, for
// each proposed candidate score, decides Keep (strictly better -> adopt, ratchet best up) or Revert
// (not strictly better -> incumbent holds). It tracks kept/reverted counts and an append-only history.
//
// Objective convention: HIGHER is better (a value/J/reward score). A caller minimizing a cost negates
// it (or scores `-cost`) before proposing — the primitive stays a single, unambiguous `>`-comparator,
// which is the whole point (every keep-or-revert site in the repo is strict-greater on a benefit).
//
// Not goroutine-safe — the caller serializes proposals (the engine is fully serial; the §5.3 bandit
// runs one window at a time).
type Experiment struct {
	best     float64
	kept     int
	reverted int
	history  []Trial
}

// NewExperiment starts an experiment with the given initial best-so-far (the floor / incumbent score).
// Pass the current best the registry already holds (minting) or J_best (the theta bandit); pass 0 (or a
// minimum-sentinel) for a fresh objective where any positive candidate should be adopted.
func NewExperiment(initialBest float64) *Experiment {
	return &Experiment{best: initialBest}
}

// Propose scores one candidate against the best so far and returns the keep-or-revert decision. It
// KEEPS iff candidate is STRICTLY greater than the current best (strict `>`, best-relative) — a tie
// reverts, so the kept value never churns on an equal result. On a Keep the best ratchets up to the
// candidate; on a Revert the best is unchanged. Every proposal appends one row to the history.
func (e *Experiment) Propose(candidate float64) Decision {
	d := Revert
	if candidate > e.best { // strict: a tie is NOT an improvement (the lineage's invariant)
		e.best = candidate
		d = Keep
	}
	if d == Keep {
		e.kept++
	} else {
		e.reverted++
	}
	e.history = append(e.history, Trial{Candidate: candidate, Decision: d, Best: e.best})
	return d
}

// Best returns the best-so-far score (the incumbent the next Propose is judged against).
func (e *Experiment) Best() float64 { return e.best }

// Kept returns how many proposals were kept (strictly improved the best).
func (e *Experiment) Kept() int { return e.kept }

// Reverted returns how many proposals were reverted (failed to strictly improve — worse or a tie).
func (e *Experiment) Reverted() int { return e.reverted }

// History returns a defensive copy of the append-only run record, one row per Propose, in order.
func (e *Experiment) History() []Trial {
	out := make([]Trial, len(e.history))
	copy(out, e.history)
	return out
}

// RunExperiment is the tiny composite: a tested keep-or-revert loop. It runs every candidate score
// through one Experiment starting from `floor`, and returns the KEPT scores (in proposal order — the
// strictly-improving prefix maxima) plus the final best. This is the loop both the minting gate and the
// §5.3 theta bandit drive: feed a window's candidate objectives in, get back what survived and the new
// incumbent. Returns an empty (non-nil) kept slice when nothing beats the floor.
func RunExperiment(floor float64, candidates []float64) (kept []float64, best float64) {
	e := NewExperiment(floor)
	kept = make([]float64, 0, len(candidates))
	for _, c := range candidates {
		if e.Propose(c) == Keep {
			kept = append(kept, c)
		}
	}
	return kept, e.Best()
}
