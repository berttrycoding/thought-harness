package stats

import (
	"math"
	"sort"
)

// WilcoxonResult is the verdict of the Wilcoxon signed-rank test on paired
// continuous deltas (the spec's per-item score / cost / hold-rate differences,
// measuring-stick §4.2). It is the non-parametric companion to McNemar for
// outcomes that are scored rather than pass/fail.
type WilcoxonResult struct {
	// W is the signed-rank statistic: the sum of ranks of the positive
	// differences (the conventional W+). Zero differences are dropped before
	// ranking (the Wilcoxon convention).
	W float64
	// WMinus is the sum of ranks of the negative differences (W+ + W- = n(n+1)/2
	// over the non-zero pairs), exposed because some references report the min.
	WMinus float64
	// N is the number of non-zero paired differences actually ranked.
	N int
	// Z is the normal-approximation statistic with continuity correction and the
	// tie correction applied to the variance.
	Z float64
	// P is the two-sided p-value from the normal approximation.
	P float64
	// RankBiserial is the matched-pairs rank-biserial effect size
	// r = (W+ - W-) / (W+ + W-) in [-1, 1]: +1 = every difference favours the
	// first member of the pair, -1 = every difference against it, 0 = balanced.
	RankBiserial float64
}

// WilcoxonSignedRank runs the test on the per-pair differences `diffs`
// (typically arm1_value - arm2_value, computed by the caller). It uses the
// normal approximation with a continuity correction and the standard tie
// correction to the variance — appropriate for the measuring stick's bank sizes
// (tens to hundreds of pairs) and matching the published worked examples used in
// the tests.
//
// Procedure:
//  1. drop zero differences (no information under H0: median = 0);
//  2. rank the remaining |diff| ascending, assigning average ranks to ties;
//  3. W+ = sum of ranks where diff>0, W- = sum where diff<0;
//  4. mean = n(n+1)/4, var = n(n+1)(2n+1)/24 minus the tie correction
//     sum(t^3 - t)/48;
//  5. z = (W+ - mean - 0.5*sign) / sqrt(var) with a continuity correction
//     toward the mean.
//
// Returns a zero-N result with P=1 when there are no non-zero differences.
func WilcoxonSignedRank(diffs []float64) WilcoxonResult {
	// Keep only non-zero differences.
	type entry struct {
		abs  float64
		pos  bool
		rank float64
	}
	entries := make([]entry, 0, len(diffs))
	for _, d := range diffs {
		if d == 0 || math.IsNaN(d) {
			continue
		}
		entries = append(entries, entry{abs: math.Abs(d), pos: d > 0})
	}
	n := len(entries)
	res := WilcoxonResult{N: n}
	if n == 0 {
		res.P = 1
		res.RankBiserial = math.NaN()
		return res
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].abs < entries[j].abs })

	// Assign average ranks to ties; accumulate the tie correction sum(t^3 - t).
	var tieCorrection float64
	for i := 0; i < n; {
		j := i
		for j < n && entries[j].abs == entries[i].abs {
			j++
		}
		// ties group is [i, j); average rank = mean of (i+1 .. j) in 1-based ranks.
		avg := float64(i+1+j) / 2.0
		t := j - i
		if t > 1 {
			tieCorrection += float64(t)*float64(t)*float64(t) - float64(t)
		}
		for k := i; k < j; k++ {
			entries[k].rank = avg
		}
		i = j
	}

	var wPlus, wMinus float64
	for _, e := range entries {
		if e.pos {
			wPlus += e.rank
		} else {
			wMinus += e.rank
		}
	}
	res.W = wPlus
	res.WMinus = wMinus
	res.RankBiserial = (wPlus - wMinus) / (wPlus + wMinus)

	nf := float64(n)
	mean := nf * (nf + 1) / 4
	variance := nf*(nf+1)*(2*nf+1)/24 - tieCorrection/48
	if variance <= 0 {
		// Degenerate (e.g. n=1 with the single pair); no usable normal approx.
		res.P = 1
		res.Z = 0
		return res
	}

	// Continuity correction: shrink (W+ - mean) toward 0 by 0.5.
	diff := wPlus - mean
	cc := 0.0
	switch {
	case diff > 0:
		cc = -0.5
	case diff < 0:
		cc = 0.5
	}
	res.Z = (diff + cc) / math.Sqrt(variance)
	res.P = clamp01(2 * normalSF(math.Abs(res.Z)))
	return res
}
