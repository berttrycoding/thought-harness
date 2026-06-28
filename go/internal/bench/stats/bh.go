package stats

import "sort"

// BHResult is the outcome of Benjamini-Hochberg FDR control over a family of
// p-values (measuring-stick §4.4: BH at q=0.05, q=0.10 for stability). It is
// returned in the CALLER's original order so a result lines up index-for-index
// with the tests that produced the p-values.
type BHResult struct {
	// Adjusted holds the BH-adjusted p-values ("q-values"), in the input order.
	// Adjusted[i] is the smallest FDR level at which test i is rejected, with the
	// monotone (step-up) enforcement applied so adjusted p never decreases as raw
	// p increases.
	Adjusted []float64
	// Reject[i] is true iff test i is rejected at the chosen q (equivalently,
	// Adjusted[i] <= q), in the input order.
	Reject []bool
	// NumReject is the size of the reject set.
	NumReject int
	// Q is the FDR level applied.
	Q float64
}

// BenjaminiHochberg applies the Benjamini-Hochberg step-up procedure at FDR
// level q to `pvals`, returning adjusted p-values and the reject set in the
// original input order.
//
// Procedure:
//   - sort p ascending: p_(1) <= ... <= p_(m);
//   - the BH critical value at rank i is (i/m)·q; find the largest k with
//     p_(k) <= (k/m)·q and reject ranks 1..k;
//   - adjusted p at rank i is min over j>=i of (m/j)·p_(j), clamped to <=1
//     (the standard monotone "q-value" form), then scattered back to input
//     order.
//
// An empty input yields an empty result. NaN p-values are treated as 1 (never
// rejected) so a failed/absent test can't accidentally enter the reject set.
func BenjaminiHochberg(pvals []float64, q float64) BHResult {
	m := len(pvals)
	res := BHResult{
		Adjusted: make([]float64, m),
		Reject:   make([]bool, m),
		Q:        q,
	}
	if m == 0 {
		return res
	}

	// Pair each p with its original index, sanitize NaN to 1.
	type pp struct {
		p   float64
		idx int
	}
	order := make([]pp, m)
	for i, p := range pvals {
		if p != p { // NaN
			p = 1
		}
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		order[i] = pp{p: p, idx: i}
	}
	sort.Slice(order, func(i, j int) bool { return order[i].p < order[j].p })

	mf := float64(m)

	// Reject set: largest k (1-based rank) with p_(k) <= (k/m)*q.
	maxK := 0
	for rank := 1; rank <= m; rank++ {
		crit := float64(rank) / mf * q
		if order[rank-1].p <= crit {
			maxK = rank
		}
	}

	// Adjusted p-values: enforce monotonicity from the largest rank down.
	adjSorted := make([]float64, m)
	running := 1.0
	for rank := m; rank >= 1; rank-- {
		raw := mf / float64(rank) * order[rank-1].p
		if raw < running {
			running = raw
		}
		if running > 1 {
			adjSorted[rank-1] = 1
		} else {
			adjSorted[rank-1] = running
		}
	}

	// Scatter back to input order; mark rejects (ranks 1..maxK).
	for rank := 1; rank <= m; rank++ {
		oi := order[rank-1].idx
		res.Adjusted[oi] = adjSorted[rank-1]
		if rank <= maxK {
			res.Reject[oi] = true
		}
	}
	res.NumReject = maxK
	return res
}
