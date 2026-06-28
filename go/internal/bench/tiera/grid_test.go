package tiera

import (
	"testing"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// gridOracle builds a grid oracle whose canonical Expected is the JSON array-of-arrays form.
func gridOracle(expected string) benchtypes.Oracle {
	return benchtypes.Oracle{Kind: benchtypes.OracleGrid, Expected: expected}
}

// TestGridOracleExactMatchPasses asserts the ARC-shaped grid oracle PASSES on an exact full-grid match,
// across the serializations a model actually emits: the JSON array-of-arrays (possibly wrapped in prose)
// and the whitespace/newline integer-row form. This is the bank's native ARC grader (deterministic,
// programmatic — no model in the scorer), so a correct grid in any common encoding must score correct.
func TestGridOracleExactMatchPasses(t *testing.T) {
	want := "[[1,2,3],[4,5,6]]"
	cases := []struct {
		name   string
		answer string
	}{
		{"json-bare", "[[1,2,3],[4,5,6]]"},
		{"json-in-prose", "Here is the output grid:\n[[1, 2, 3], [4, 5, 6]]\nThat is my answer."},
		{"whitespace-rows", "1 2 3\n4 5 6"},
		{"comma-rows", "1,2,3\n4,5,6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Evaluate(gridOracle(want), tc.answer, nil)
			if res.Unsupported {
				t.Fatalf("grid oracle reported Unsupported (it must be a deterministic scorer): %s", res.Reason)
			}
			if !res.OK {
				t.Fatalf("grid oracle should PASS an exact match, got FAIL: %s", res.Reason)
			}
		})
	}
}

// TestGridOracleWrongCellFails is the load-bearing ARC property: NO partial credit. A grid that differs in
// a single cell, or in its dimensions, must FAIL — the pass/fail discipline that makes ARC-AGI-2 a
// fluid-intelligence test (a near-miss is a miss). A mutation-sensitive check: each variant flips exactly
// one thing and must be caught.
func TestGridOracleWrongCellFails(t *testing.T) {
	want := "[[1,2,3],[4,5,6]]"
	cases := []struct {
		name   string
		answer string
	}{
		{"one-cell-off", "[[1,2,3],[4,5,9]]"},        // a single wrong cell
		{"wrong-width", "[[1,2],[4,5]]"},             // a column dropped
		{"wrong-height", "[[1,2,3]]"},                // a row dropped
		{"transposed", "[[1,4],[2,5],[3,6]]"},        // right cells, wrong shape
		{"not-a-grid", "I think the answer is blue"}, // prose, no grid at all
		{"ragged", "[[1,2,3],[4,5]]"},                // ragged -> not a valid ARC grid
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Evaluate(gridOracle(want), tc.answer, nil)
			if res.OK {
				t.Fatalf("grid oracle must FAIL %q (no partial credit), but it PASSED: %s", tc.answer, res.Reason)
			}
		})
	}
}

// TestGridOracleMalformedExpectedFailsLoud asserts a BANK misconfiguration (an Expected that is not a grid)
// fails the item with a clear reason rather than silently passing against an empty grid — the trustworthy-
// grader discipline (a broken oracle must be loud, not a false PASS).
func TestGridOracleMalformedExpectedFailsLoud(t *testing.T) {
	res := Evaluate(gridOracle("not a grid at all"), "[[1,2],[3,4]]", nil)
	if res.OK {
		t.Fatal("grid oracle PASSED against a malformed Expected (it must fail loud on bank misconfiguration)")
	}
}

// TestGridOracleWhitespaceParseRejectsProse guards the whitespace parser: a free-text line that is not a row
// of integers must NOT be silently read as a degenerate grid (which could fabricate a false match). A prose
// answer with no parseable grid must FAIL.
func TestGridOracleWhitespaceParseRejectsProse(t *testing.T) {
	res := Evaluate(gridOracle("1 2\n3 4"), "the grid is one two then three four", nil)
	if res.OK {
		t.Fatal("grid oracle read a prose sentence as a grid — the whitespace parser must reject non-integer rows")
	}
}
