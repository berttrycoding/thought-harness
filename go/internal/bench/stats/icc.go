package stats

import "math"

// ICCResult carries the ICC(2,1) point estimate plus the ANOVA mean squares it
// was built from (handy for the Phase-0 report and for debugging a surprising
// value).
type ICCResult struct {
	// ICC is the ICC(2,1) coefficient: two-way random-effects, single rater,
	// absolute agreement. Range (-… , 1]; the spec gate is >= 0.8 for a judged
	// checker (measuring-stick §4.1).
	ICC float64
	N   int     // subjects (rows)
	K   int     // raters / replicate columns
	MSR float64 // between-subjects (rows) mean square
	MSC float64 // between-raters (columns) mean square
	MSE float64 // residual mean square
}

// ICC21 computes ICC(2,1) — the two-way random-effects, single-rater,
// absolute-agreement intraclass correlation — for a subjects-by-raters matrix
// (`matrix[i]` = the k scores subject i received from the k raters/replicates).
// This is the test-retest reliability the Phase-0 verifier characterization
// reports (measuring-stick §4.1 / §7.1).
//
// Definition (Shrout & Fleiss 1979, McGraw & Wong 1996):
//
//	ICC(2,1) = (MSR - MSE) /
//	           (MSR + (k-1)·MSE + (k/n)·(MSC - MSE))
//
// where, over a two-way ANOVA without interaction (the single-rater "1"):
//   - MSR = between-rows (subjects) mean square,
//   - MSC = between-columns (raters) mean square,
//   - MSE = residual mean square,
//   - n = number of subjects, k = number of raters.
//
// It requires a rectangular matrix with n>=2 rows and k>=2 columns; otherwise
// it returns ICC=NaN with the dimensions it saw. A perfectly degenerate matrix
// (zero residual AND zero rater spread, i.e. all rows identical-and-flat) yields
// ICC=1 by the limit; total-zero variance yields NaN.
func ICC21(matrix [][]float64) ICCResult {
	n := len(matrix)
	if n < 2 {
		return ICCResult{ICC: math.NaN(), N: n}
	}
	k := len(matrix[0])
	if k < 2 {
		return ICCResult{ICC: math.NaN(), N: n, K: k}
	}
	for _, row := range matrix {
		if len(row) != k {
			// ragged matrix — undefined ANOVA
			return ICCResult{ICC: math.NaN(), N: n, K: k}
		}
	}

	nf := float64(n)
	kf := float64(k)

	// Grand mean, row means, column means.
	var grand float64
	rowMean := make([]float64, n)
	colMean := make([]float64, k)
	for i := 0; i < n; i++ {
		for j := 0; j < k; j++ {
			v := matrix[i][j]
			grand += v
			rowMean[i] += v
			colMean[j] += v
		}
	}
	grand /= nf * kf
	for i := range rowMean {
		rowMean[i] /= kf
	}
	for j := range colMean {
		colMean[j] /= nf
	}

	// Sums of squares.
	var ssRows, ssCols, ssTotal float64
	for i := 0; i < n; i++ {
		ssRows += (rowMean[i] - grand) * (rowMean[i] - grand)
	}
	ssRows *= kf
	for j := 0; j < k; j++ {
		ssCols += (colMean[j] - grand) * (colMean[j] - grand)
	}
	ssCols *= nf
	for i := 0; i < n; i++ {
		for j := 0; j < k; j++ {
			d := matrix[i][j] - grand
			ssTotal += d * d
		}
	}
	ssError := ssTotal - ssRows - ssCols
	if ssError < 0 {
		ssError = 0 // guard tiny negative from float cancellation
	}

	// Degrees of freedom: rows n-1, cols k-1, error (n-1)(k-1).
	dfRows := nf - 1
	dfCols := kf - 1
	dfError := (nf - 1) * (kf - 1)

	msr := ssRows / dfRows
	msc := ssCols / dfCols
	mse := ssError / dfError

	res := ICCResult{N: n, K: k, MSR: msr, MSC: msc, MSE: mse}

	denom := msr + (kf-1)*mse + (kf/nf)*(msc-mse)
	if denom == 0 {
		if msr == 0 {
			// no subject variance at all -> undefined.
			res.ICC = math.NaN()
		} else {
			res.ICC = 1
		}
		return res
	}
	res.ICC = (msr - mse) / denom
	return res
}
