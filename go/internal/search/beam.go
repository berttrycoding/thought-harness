package search

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// Verifier-guided beam search over the thought graph (07-OPTIMISATION-SURVEY §D #1 — "THE capability
// lever": A* best-first → verifier-guided beam scored by V(s) + grounding). The current model expands
// the single highest-VALUE open node (greedy A*, View.Best). This adds a PROCESS-REWARD policy: blend
// the value heuristic V(s) with a per-branch GROUNDING signal (the verifier) into one score, and keep
// the top-K beam rather than committing greedily to the single best — so a slightly-lower-value but
// better-GROUNDED line is not prematurely abandoned, the classic best-of-N → verifier-guided-beam win.
//
// This file is the POLICY CORE: pure, deterministic, model-free (the verifier is INJECTED, so it tests
// offline). λ=0 reduces EXACTLY to today's value-only A* ordering, so wiring it behind a flag is safe.
// REMAINING (gated): the engine seam (the Controller picks the next node to expand via Beam instead of
// Best when the flag is on) and the LIFT validation (does it raise answer quality — an internal/bench
// run, GPU-gated). Until both land it changes nothing.

// Verifier returns a per-branch GROUNDING signal in [0,1] — the model-free half of the process reward
// (1.0 = the branch's claims are fully grounded in reality/compute; 0.0 = ungrounded). It is injected
// so the policy is testable offline; in production it closes over the experiment memory / grounded-claim
// fraction for the branch's thoughts.
type Verifier func(b *types.Branch) float64

// ScoredBranch pairs a frontier branch with its blended process-reward score and the two components
// that produced it (so the selection is legible, never a bare number).
type ScoredBranch struct {
	Branch    *types.Branch
	Score     float64 // the blended process reward used for ranking
	Value     float64 // V(s) component (Branch.Value)
	Grounding float64 // verifier component, [0,1]
}

// BeamScore is the process-reward blend: (1-λ)·V(s) + λ·grounding. λ∈[0,1] trades the value heuristic
// against the grounding signal; λ=0 is pure value (today's A*), λ=1 is pure grounding. A NIL verifier
// means no grounding signal exists, so the score is pure V(s) regardless of λ (no spurious (1-λ)
// scaling). λ is clamped to [0,1].
func BeamScore(b *types.Branch, verify Verifier, lambda float64) float64 {
	if verify == nil || lambda <= 0 {
		return b.Value // no verifier or no weight on it → pure value (process reward off)
	}
	return blendScore(b.Value, verify(b), lambda)
}

// blendScore is the SINGLE definition of the process-reward blend (1-λ)·value + λ·grounding, with λ
// clamped to [0,1]. Both BeamScore and Beam route through it so the formula cannot drift between them.
func blendScore(value, grounding, lambda float64) float64 {
	if lambda < 0 {
		lambda = 0
	}
	if lambda > 1 {
		lambda = 1
	}
	return (1-lambda)*value + lambda*grounding
}

// Beam re-ranks the open frontier by the blended process-reward score and returns the top-K (the kept
// beam), best-first. Deterministic: a stable sort with a Branch.ID ascending tiebreak, so equal scores
// keep frontier/id order (matching the value-only path's determinism). k<=0 returns the whole ranked
// frontier (no truncation). With λ=0 (or a nil verifier) the ordering is value-only — identical ranking
// to View.Best/Open's value order, so the beam head equals the greedy best. The verifier is called at
// most once per branch.
func (v *View) Beam(verify Verifier, lambda float64, k int) []ScoredBranch {
	active := verify != nil && lambda > 0
	open := v.G.Frontier()
	scored := make([]ScoredBranch, 0, len(open))
	for _, b := range open {
		g := 0.0
		score := b.Value
		if active {
			g = verify(b)
			score = blendScore(b.Value, g, lambda) // one blend definition, shared with BeamScore
		}
		scored = append(scored, ScoredBranch{
			Branch:    b,
			Score:     score,
			Value:     b.Value,
			Grounding: g,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score // best-first
		}
		return scored[i].Branch.ID < scored[j].Branch.ID // deterministic tiebreak
	})
	if k > 0 && k < len(scored) {
		scored = scored[:k]
	}
	return scored
}

// BeamBest returns the single next node to expand under the verifier-guided policy: the head of the
// beam (highest blended process reward), or nil when the frontier is empty. This is the drop-in
// replacement for View.Best when the policy is enabled — with λ=0 it returns exactly View.Best.
func (v *View) BeamBest(verify Verifier, lambda float64) *types.Branch {
	beam := v.Beam(verify, lambda, 1)
	if len(beam) == 0 {
		return nil
	}
	return beam[0].Branch
}
