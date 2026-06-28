package tiera

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// evalGrid is the ARC-AGI-2-shaped GRID oracle (docs/internal/notes/2026-06-21-sota-benchmark-suite.md §7.1:
// "ARC-AGI grid match"). It scores an arm's free-text answer as an exact, full-grid match against the
// canonical Expected grid — the bank's NATIVE grader shape (programmatic, deterministic, ungameable; spec
// §8.2 trustworthy-grader class). There is NO partial credit: a grid is correct iff it has the EXACT
// dimensions of Expected AND every cell matches (the ARC pass/fail discipline — a single wrong cell fails
// the whole task, which is precisely what makes ARC-AGI-2 a fluid-intelligence test rather than a similarity
// metric). Both Expected and the answer pass through parseGrid, which extracts the grid from the surrounding
// prose and accepts the two common ARC serializations (a JSON array-of-arrays, or whitespace/newline rows of
// integers). A grid that does not parse on either side is a clean FAIL with a reason, never a silent pass.
func evalGrid(oracle benchtypes.Oracle, answer string) OracleResult {
	want, werr := parseGrid(oracle.Expected)
	if werr != nil {
		// A malformed Expected is a BANK misconfiguration, not a model failure — surface it loudly so it
		// fails the item rather than passing a comparison against an empty grid.
		return OracleResult{OK: false, Reason: "grid oracle: malformed Expected grid: " + werr.Error()}
	}
	if len(want) == 0 {
		return OracleResult{OK: false, Reason: "grid oracle: Expected grid is empty (bank misconfiguration)"}
	}
	got, gerr := parseGrid(answer)
	if gerr != nil {
		return OracleResult{OK: false, Reason: "grid mismatch: answer is not a parseable grid (" + gerr.Error() + ")"}
	}
	if eq, why := gridsEqual(want, got); !eq {
		return OracleResult{OK: false, Reason: "grid mismatch: " + why}
	}
	return OracleResult{OK: true, Reason: fmt.Sprintf("grid match (%dx%d, every cell equal)", len(want), len(want[0]))}
}

// gridsEqual reports whether two grids are dimensionally identical AND equal cell-by-cell, returning a
// one-line reason on the first divergence (for the ledger). A rectangular grid is assumed (parseGrid
// rejects ragged rows), so a row-length check on row 0 suffices for width.
func gridsEqual(want, got [][]int) (bool, string) {
	if len(got) != len(want) {
		return false, fmt.Sprintf("row count %d != want %d", len(got), len(want))
	}
	if len(want) == 0 {
		return true, ""
	}
	for r := range want {
		if len(got[r]) != len(want[r]) {
			return false, fmt.Sprintf("row %d width %d != want %d", r, len(got[r]), len(want[r]))
		}
		for c := range want[r] {
			if got[r][c] != want[r][c] {
				return false, fmt.Sprintf("cell [%d,%d] = %d != want %d", r, c, got[r][c], want[r][c])
			}
		}
	}
	return true, ""
}

// parseGrid extracts a rectangular integer grid from a string that may carry surrounding prose. It tries,
// in order: (1) a JSON array-of-arrays of integers found anywhere in the text (the ARC canonical encoding);
// (2) a whitespace/newline-delimited block of integer rows (rows separated by newlines, cells by spaces or
// commas). It rejects a ragged grid (rows of differing widths) and an empty grid. The two encodings cover
// every common way a model or a bank serializes an ARC grid, so a correct answer wrapped in explanation
// ("Here is the output grid: [[1,2],[3,4]]") still parses.
func parseGrid(s string) ([][]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty input")
	}
	// (1) JSON array-of-arrays anywhere in the text. Find the first balanced [[...]] span and try to decode
	// it — robust to leading/trailing prose.
	if span, ok := firstJSONArraySpan(s); ok {
		var grid [][]int
		if err := json.Unmarshal([]byte(span), &grid); err == nil {
			if len(grid) == 0 {
				return nil, fmt.Errorf("JSON grid has no rows")
			}
			if err := assertRectangular(grid); err != nil {
				return nil, err
			}
			return grid, nil
		}
	}
	// (2) whitespace/newline rows of integers. Each non-blank line is a row; cells are split on spaces and
	// commas. A line that contains a non-integer token disqualifies the whitespace parse (so prose lines do
	// not masquerade as rows).
	grid, err := parseWhitespaceGrid(s)
	if err != nil {
		return nil, err
	}
	if err := assertRectangular(grid); err != nil {
		return nil, err
	}
	return grid, nil
}

// firstJSONArraySpan returns the substring from the first '[' that opens a nested array (i.e. is followed,
// ignoring whitespace, by another '[') through its matching ']', so a leading prose + trailing prose answer
// still yields the bracketed array-of-arrays. ok=false when no such span exists.
func firstJSONArraySpan(s string) (string, bool) {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		// require the next non-space rune to be another '[' (array-of-arrays), to avoid grabbing a 1-D list.
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
			j++
		}
		if j < len(s) && s[j] == '[' {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false // unbalanced
}

// parseWhitespaceGrid parses newline-delimited rows of space/comma-separated integers. A blank line is
// skipped; a line with any non-integer token fails the whole parse (so a prose answer is not silently read
// as a degenerate grid).
func parseWhitespaceGrid(s string) ([][]int, error) {
	var grid [][]int
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' })
		if len(fields) == 0 {
			continue
		}
		row := make([]int, 0, len(fields))
		for _, f := range fields {
			n, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("non-integer cell %q", f)
			}
			row = append(row, n)
		}
		grid = append(grid, row)
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("no integer rows found")
	}
	return grid, nil
}

// assertRectangular rejects a ragged grid (an ARC grid is always rectangular) and an empty grid / empty row.
func assertRectangular(grid [][]int) error {
	if len(grid) == 0 {
		return fmt.Errorf("grid has no rows")
	}
	w := len(grid[0])
	if w == 0 {
		return fmt.Errorf("grid row 0 is empty")
	}
	for r, row := range grid {
		if len(row) != w {
			return fmt.Errorf("ragged grid: row %d has %d cells, row 0 has %d", r, len(row), w)
		}
	}
	return nil
}
