// sparsemax.go is the closed-form SPARSEMAX projection (Martins & Astudillo 2016, "From Softmax to
// Sparsemax") used by the opt-in subconscious.dispatch.sparse admission path. It is the Euclidean
// projection of a score vector onto the probability simplex — the principled, normalized generalization
// of the dispatch loop's ad-hoc per-key absolute `eff > theta` gate (design:
// docs/internal/notes/2026-06-21-attention-mechanisms-litreview.md §4).
//
// WHY sparsemax (not softmax) for dispatch: softmax assigns STRICTLY POSITIVE weight to EVERY key, so it
// can never truly ignore a specialist (no exact zeros). The harness explicitly needs MOST specialists to
// NOT fire. Sparsemax yields a DATA-ADAPTIVE set of EXACT ZEROS — its induced threshold τ rises when
// strong competitors are present and falls when the field is weak, so it admits "a few relative to the
// field" rather than "everyone over a fixed absolute bar". It is closed-form, autodiff-free, O(K log K)
// by sort-and-threshold, and (with a STABLE sort, ties broken by input index) fully deterministic — no
// RNG, no clock. Pure Pattern-A CONTROL (no model call).
//
// HARD BOUNDARY (design §4): this projection is for the SUBCONSCIOUS pull ONLY. The CONSCIOUS focus stays
// hard argmax (GWT ignition — one EXPANDED branch); a sparsemax BLEND of branches at the conscious level
// would violate the one-EXPANDED-branch invariant. Nothing here touches the conscious focus.
package subconscious

import (
	"math"
	"sort"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// sparseEps is the positivity tolerance for the simplex projection: a mass at or below it is treated as an
// EXACT ZERO (the key is ignored). It guards against a key landing precisely on the induced threshold τ,
// where floating-point round-off can leave a ~1e-17 residual that would otherwise spuriously admit a
// specialist sparsemax meant to zero (a key exactly at τ has p_i = 0 mathematically). Determinism is
// preserved — the same fixed tolerance applies on every projection, RNG-free.
const sparseEps = 1e-9

// sparsemaxResult carries the output of one projection: the per-key probability mass p (parallel to the
// input, in input order), the induced threshold tau, and the support size (count of p_i > 0). p sums to
// 1 when the input is non-empty; the exact zeros are the keys sparsemax decided to ignore.
type sparsemaxResult struct {
	p       []float64 // p[i] = max(0, z[i] - tau), in input order; sums to 1 (non-empty input)
	tau     float64   // the induced threshold tau = (sum over support of z - 1) / |support|
	support int       // |{ i : p[i] > 0 }| — the data-adaptive support size
}

// sparsemax projects z onto the probability simplex (Martins & Astudillo 2016, Algorithm 1). The closed
// form:
//
//  1. Sort z DESCENDING into z_sorted (z_(1) >= z_(2) >= ... >= z_(K)); ties broken by the ORIGINAL index
//     ascending, via a STABLE sort, so the support set is deterministic on ties.
//  2. Find the support size k = max { j in 1..K : 1 + j*z_(j) > sum_{r<=j} z_(r) } — equivalently the
//     largest j with z_(j) + (1/j)*(1 - sum_{r<=j} z_(r)) > 0 (the form in the design doc).
//  3. tau = (sum_{r<=k} z_(r) - 1) / k.
//  4. p_i = max(0, z_i - tau).
//
// An empty input returns an empty, zero-valued result (support 0). A single-element input returns p=[1],
// tau=z[0]-1 (degenerate but correct: the lone key gets all the mass). The result's p is allocated in
// INPUT order, so p[i] corresponds to z[i] — the caller stamps p[i] back onto the i-th specialist.
func sparsemax(z []float64) sparsemaxResult {
	n := len(z)
	if n == 0 {
		return sparsemaxResult{p: nil, tau: 0, support: 0}
	}

	// Index permutation sorted by z DESCENDING, ties broken by original index ASCENDING (a stable
	// secondary key). sort.SliceStable keeps equal-key order, and we compare the original index as the
	// tiebreaker explicitly, so the support selection is reproducible regardless of input order.
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if z[ia] != z[ib] {
			return z[ia] > z[ib] // descending by score
		}
		return ia < ib // tie: lower original index first (roster order)
	})

	// Find the support size k and the running cumulative sum of the top-k sorted scores.
	// cumsum accumulates z_(1) + ... + z_(j); k is the largest j for which 1 + j*z_(j) > cumsum_j.
	cumSum := 0.0
	cumSumSupport := 0.0 // the cumulative sum AT the chosen support boundary (sum_{r<=k})
	k := 0
	for j := 1; j <= n; j++ {
		zj := z[order[j-1]]
		cumSum += zj
		// 1 + j*z_(j) > cumsum_j  <=>  z_(j) - (cumsum_j - 1)/j > 0 (the design-doc inequality). The
		// sparseEps margin keeps a key landing EXACTLY on τ (the boundary, where the residual is ~1e-17)
		// OUT of the support — that key's true mass is 0, so admitting it would be a round-off artifact.
		if 1.0+float64(j)*zj > cumSum+sparseEps {
			k = j
			cumSumSupport = cumSum
		}
	}
	if k == 0 {
		// Numerically defensive: the inequality holds for j=1 whenever z is finite (1 + z_(1) > z_(1)),
		// so k>=1 always; this branch cannot be reached with finite scores. Guard it anyway so a NaN/Inf
		// score can never produce a divide-by-zero — fall back to the top key taking all the mass.
		k = 1
		cumSumSupport = z[order[0]]
	}

	tau := (cumSumSupport - 1.0) / float64(k)

	p := make([]float64, n)
	support := 0
	for i := 0; i < n; i++ {
		v := z[i] - tau
		// Treat a sub-epsilon residual (a key on the boundary) as an EXACT ZERO so a round-off artifact
		// never admits a key sparsemax meant to ignore. math.Max defends a tiny negative round-off too.
		if v > sparseEps {
			p[i] = math.Max(0, v)
			support++
		}
		// else p[i] stays 0 (the exact zero — this key is ignored)
	}
	return sparsemaxResult{p: p, tau: tau, support: support}
}

// sparseAdmission is the dispatch-loop glue over one sparsemax projection: it maps a dispatch-roster INDEX
// to the sparsemax mass p_i of the base specialist at that index, and carries the τ / floor / counts for
// the subconscious.sparse event. It is built ONLY when sparse dispatch is on; on the OFF path the dispatch
// loop holds a nil *sparseAdmission and admission is the legacy absolute gate (byte-identical).
//
// The projection competes over the RELEVANCE-FIRED BASE SPECIALIST population only — a *SubAgent (a
// workflow's staffed worker) is excluded from the score vector (it is authorised by produce/staffing, not
// pulled on relevance), so workers never dilute the simplex projection nor get a sparsemax mass.
type sparseAdmission struct {
	weightByIndex map[int]float64 // roster index -> sparsemax mass p_i (only base specialists; absent ⇒ 0)
	tau           float64         // the induced sparsemax threshold τ over the base-specialist field
	support       int             // |{ base specialists with p_i > 0 }| — the sparsemax support size
	candidates    int             // |scored base specialists| (the field size the projection ran over)
	scan          []sparseScanRow // the per-base-specialist weights, in roster order, for the trace
}

// sparseScanRow is one base specialist's entry in the subconscious.sparse event: its domain, the bias-
// adjusted effective relevance eff that fed the projection, and the resulting sparsemax mass p.
type sparseScanRow struct {
	domain string
	eff    float64
	p      float64
}

// weight returns the sparsemax mass p_i for the roster member at index i, or 0 for any index not in the
// base-specialist field (a *SubAgent worker, or a specialist that scored an exact zero). Nil-safe.
func (sa *sparseAdmission) weight(i int) float64 {
	if sa == nil || sa.weightByIndex == nil {
		return 0
	}
	return sa.weightByIndex[i]
}

// computeSparseAdmission runs the SPARSEMAX projection over the per-call roster's BASE specialists (the
// relevance-fired population — *SubAgent workers are excluded) and returns the index→mass map plus the
// τ/support/counts for the event. The score vector is the SAME bias-adjusted effective relevance
// effectiveRelevance(Relevance(ctx), bias[domain]) the dispatch loop's admission uses, computed in roster
// order, so the pre-pass and the loop agree on every key's eff. Deterministic: Relevance is RNG-free, the
// projection uses a STABLE sort tie-broken by index, and no clock is read.
func (e *SubconsciousEngine) computeSparseAdmission(roster []PrimitiveSubAgent, ctx []types.Thought,
	bias map[string]float64, theta float64) *sparseAdmission {
	// Collect the base specialists' eff scores in roster order, remembering each one's roster index so the
	// resulting mass can be mapped back to the position the dispatch loop walks.
	indices := make([]int, 0, len(roster))
	scores := make([]float64, 0, len(roster))
	rows := make([]sparseScanRow, 0, len(roster))
	for i, s := range roster {
		if _, isSub := s.(*SubAgent); isSub {
			continue // a staffed worker is authorised by produce/staffing, never sparsemax-gated
		}
		eff := effectiveRelevance(s.Relevance(ctx), bias[s.Domain()])
		indices = append(indices, i)
		scores = append(scores, eff)
		rows = append(rows, sparseScanRow{domain: s.Domain(), eff: eff, p: 0})
	}

	res := sparsemax(scores)
	wbi := make(map[int]float64, len(indices))
	for k, idx := range indices {
		wbi[idx] = res.p[k]
		rows[k].p = res.p[k]
	}
	return &sparseAdmission{
		weightByIndex: wbi,
		tau:           res.tau,
		support:       res.support,
		candidates:    len(scores),
		scan:          rows,
	}
}
