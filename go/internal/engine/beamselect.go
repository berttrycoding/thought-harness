// beamselect.go wires the verifier-guided beam policy (internal/search/beam.go, 07 §D #1) into the
// engine's ONE next-node selection site: the BACKTRACK move's "which open branch do we resume?". The
// policy is flag-gated and OFF by default:
//
//	THOUGHT_BEAM_LAMBDA unset/0  ⇒ Frontier()[0] — the value-greedy pick, byte-identical to before.
//	THOUGHT_BEAM_LAMBDA in (0,1] ⇒ BeamBest over the blend (1−λ)·V(s) + λ·grounding — a slightly
//	                                lower-value but better-GROUNDED line wins the resume.
//
// The verifier here is the DETERMINISTIC v1 (Pattern-A, no model, no new state): a branch's grounding
// is the fraction of its thoughts whose Source is a REALITY IMPORT (OBSERVATION / PERCEPT) — a branch
// that has imported reality is better-grounded than one of pure GENERATED chatter. Coarse by design;
// an experiment-memory-keyed verifier is the candidate v2, but only after the lift run judges v1
// (07's gate: capability claims need the benchmark — this stays OFF until then).
package engine

import (
	"os"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/search"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// beamLambda is the λ resolved ONCE from THOUGHT_BEAM_LAMBDA (0 ⇒ the policy is off; clamped to [0,1]).
var beamLambda = resolveBeamLambda()

func resolveBeamLambda() float64 {
	raw := strings.TrimSpace(os.Getenv("THOUGHT_BEAM_LAMBDA"))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0
	}
	if v > 1 {
		v = 1
	}
	return v
}

// ucbC is the UCB exploration constant c resolved ONCE from THOUGHT_UCB_C (0 ⇒ the policy is off;
// negatives clamp to 0). It is the exploration-policy SIBLING of beamLambda at the same BACKTRACK
// resume site: c>0 draws BACKTRACK toward an UNDER-visited line (value + c*sqrt(ln N / (1+visits)),
// internal/search/ucb.go), shrinking a branch's bonus the more it is resumed. There is NO upper clamp
// (c is a free exploration weight, unlike λ which is a [0,1] blend).
var ucbC = resolveUCBC()

func resolveUCBC() float64 {
	raw := strings.TrimSpace(os.Getenv("THOUGHT_UCB_C"))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// branchGroundingVerifier builds the deterministic v1 verifier over a graph: the fraction of the
// branch's thoughts that are reality imports (OBSERVATION or PERCEPT). An empty branch grounds 0.
func branchGroundingVerifier(g *graph.ThoughtGraph) search.Verifier {
	return func(b *types.Branch) float64 {
		if b == nil || len(b.ThoughtIDs) == 0 {
			return 0
		}
		grounded := 0
		total := 0
		for _, id := range b.ThoughtIDs {
			t, ok := g.Nodes[id]
			if !ok {
				continue
			}
			total++
			if t.Source == types.OBSERVATION || t.Source == types.PERCEPT {
				grounded++
			}
		}
		if total == 0 {
			return 0
		}
		return float64(grounded) / float64(total)
	}
}

// nextFocusBranch picks the open branch BACKTRACK should focus: the value-greedy frontier head when
// the beam policy is off (lam<=0 — exactly the old Frontier()[0]), else the verifier-guided BeamBest.
// nil when the frontier is empty (the caller then does nothing, as before).
func nextFocusBranch(g *graph.ThoughtGraph, lam float64) *types.Branch {
	if lam <= 0 {
		fr := g.Frontier()
		if len(fr) == 0 {
			return nil
		}
		return fr[0]
	}
	return search.New(g).BeamBest(branchGroundingVerifier(g), lam)
}

// greedyFocusBranch is the value-greedy frontier head (the historical default pick) — nil on an empty
// frontier. Both the beam-off path and the UCB-off path bottom out here, so the default selection is
// EXACTLY Frontier()[0].
func greedyFocusBranch(g *graph.ThoughtGraph) *types.Branch {
	fr := g.Frontier()
	if len(fr) == 0 {
		return nil
	}
	return fr[0]
}

// resolveFocusBranch is the BACKTRACK resume selector with the full policy resolution order (T1.3).
// UCB and beam are mutually exclusive exploration policies at this one site; default (both 0) is the
// value-greedy head, byte-identical to before:
//
//	c   > 0  ⇒ UCBFrontier(g, visits, c)[0] — a less-visited line whose exploration bonus lifts it
//	           above the higher-value head can win the resume (draws the search toward neglected lines).
//	c  <= 0 && lam > 0 ⇒ the verifier-guided BeamBest (the grounding-blend policy, unchanged).
//	c  <= 0 && lam <= 0 ⇒ Frontier()[0] — the value-greedy pick (the historical default).
//
// The visits map is READ only when c>0; when c<=0 this function never touches it (so the maintained
// map cannot change the default-path output). greedy is the value-greedy head returned alongside the
// pick so the caller can detect (and only then emit) a non-greedy UCB resume; nil/nil on empty frontier.
func resolveFocusBranch(g *graph.ThoughtGraph, c, lam float64, visits map[int]int) (pick, greedy *types.Branch) {
	greedy = greedyFocusBranch(g)
	if c > 0 {
		if fr := search.UCBFrontier(g, visits, c); len(fr) > 0 {
			return fr[0], greedy
		}
		return nil, greedy
	}
	return nextFocusBranch(g, lam), greedy
}
