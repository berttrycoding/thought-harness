package cognition

// monitor_test.go — the locked rendering conventions, pinned. These are the contract every W2
// monitor panel renders on, so a drift in one place is caught here once for all panels.

import "testing"

func TestStripCellsNewestRightFixedWidth(t *testing.T) {
	// fewer ticks than width: left-padded with off-glyphs, newest on the right.
	got := stripCells([]bool{true, false, true}, 6)
	if got != "___█_█" {
		t.Errorf("short history: got %q, want %q", got, "___█_█")
	}
	// more ticks than width: keep only the most recent `width`.
	got = stripCells([]bool{true, true, true, false, false}, 3)
	if got != "█__" {
		t.Errorf("long history: got %q, want %q (last 3, newest right)", got, "█__")
	}
	// exact width.
	if got := stripCells([]bool{false, true}, 2); got != "_█" {
		t.Errorf("exact width: got %q, want %q", got, "_█")
	}
	// zero width is empty (no panic).
	if got := stripCells([]bool{true}, 0); got != "" {
		t.Errorf("zero width: got %q, want empty", got)
	}
	// empty history fills the window with off-glyphs.
	if got := stripCells(nil, 4); got != "____" {
		t.Errorf("empty history: got %q, want %q", got, "____")
	}
}

func TestStripCellsAlwaysExactWidth(t *testing.T) {
	for _, w := range []int{1, 5, 50} {
		for _, n := range []int{0, 3, 50, 80} {
			hits := make([]bool, n)
			if got := len([]rune(stripCells(hits, w))); got != w {
				t.Errorf("width %d, history %d: cell count %d, want exactly %d", w, n, got, w)
			}
		}
	}
}

func TestAgeLabel(t *testing.T) {
	cases := map[int]string{0: "now", -1: "now", 1: "1t", 6: "6t", 47: "47t"}
	for age, want := range cases {
		if got := ageLabel(age); got != want {
			t.Errorf("ageLabel(%d) = %q, want %q", age, got, want)
		}
	}
}

func TestStaleHorizon(t *testing.T) {
	if stale(10, 50) {
		t.Error("age 10 within a 50t horizon must be fresh")
	}
	if !stale(51, 50) {
		t.Error("age 51 past a 50t horizon must be stale")
	}
	if stale(50, 50) {
		t.Error("age exactly at the horizon is still fresh")
	}
	// a zero/garbage horizon falls back to the default, not "everything stale".
	if stale(10, 0) {
		t.Error("zero horizon must fall back to the default (10t is fresh)")
	}
}

func TestVitalText(t *testing.T) {
	if got := vitalText("STABLE", "n 0.073"); got != "STABLE (n 0.073)" {
		t.Errorf("got %q", got)
	}
	if got := vitalText("NONE", ""); got != "NONE" {
		t.Errorf("empty detail should be the bare word, got %q", got)
	}
}

func TestIndicatorPlainFixedPositionsLit(t *testing.T) {
	names := []string{"THINK", "BRANCH", "MERGE", "BACKTRACK", "ACT", "STOP", "DELIVER"}
	got := indicatorPlain(names, "ACT")
	want := "THINK  BRANCH  MERGE  BACKTRACK  [ACT]  STOP  DELIVER"
	if got != want {
		t.Errorf("lit ACT:\n got %q\nwant %q", got, want)
	}
	// nothing lit: every name plain, fixed positions unchanged.
	got = indicatorPlain(names, "")
	if want := "THINK  BRANCH  MERGE  BACKTRACK  ACT  STOP  DELIVER"; got != want {
		t.Errorf("none lit:\n got %q\nwant %q", got, want)
	}
}

func TestLastBlockTwoRows(t *testing.T) {
	h, s := lastBlock("ACT", 184, "question demands ground truth")
	if h != "ACT · tick 184" {
		t.Errorf("header = %q", h)
	}
	if s != "question demands ground truth" {
		t.Errorf("sentence = %q (must be the reason alone)", s)
	}
}

// The styled helpers must keep the underlying text intact (ANSI wraps it; the chars survive as a
// substring) and preserve fixed-position order — so a panel built on them is still readable/scrapable.
func TestStyledHelpersPreserveText(t *testing.T) {
	names := []string{"COMPRESS", "BRANCH", "MERGE", "EXPAND"}
	row := IndicatorRow(names, "COMPRESS", colAccent)
	last := 0
	for _, n := range names {
		i := indexOf(row, n)
		if i < 0 {
			t.Fatalf("IndicatorRow dropped %q: %q", n, row)
		}
		if i < last {
			t.Fatalf("IndicatorRow reordered %q (fixed positions broken)", n)
		}
		last = i
	}
	strip := Strip([]bool{true, false, true}, 3, colOk)
	if indexOf(strip, glyphOn) < 0 || indexOf(strip, glyphOff) < 0 {
		t.Errorf("Strip lost its glyphs: %q", strip)
	}
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
