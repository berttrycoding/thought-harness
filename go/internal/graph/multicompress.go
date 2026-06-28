package graph

import (
	"strconv"
	"strings"
)

// multicompress.go is MULTI-LEVEL compression (01-subconscious.md §3.8 GAP: "compression is binary today,
// no multi-level states"). The binary EXPANDED/COMPRESSED enum stays the headline state; this adds GRADED
// lossiness WITHIN the compressed state — a stashed line can be lightly gisted (recent thought kept) or
// heavily gisted (a bare count label), so bounded focus can trade detail for room progressively instead of
// all-or-nothing. Deterministic (no clock, no RNG, no backend) — a pure function of the thoughts + level.

// MaxCompressionLevel is the deepest (lossiest) compression level. Level 0 is full detail (EXPANDED);
// levels 1..Max are progressively lossier gists.
const MaxCompressionLevel = 3

// LevelGist returns the gist of a branch's thought texts at a compression level (§3.8):
//
//	0 -> full      : every thought joined (the EXPANDED view — no loss)
//	1 -> recent    : only the most recent thought (the freshest state)
//	2 -> headline  : the first ~48 runes of the most recent thought (a one-line gist)
//	3 -> label     : a bare "(N thoughts)" count (maximally lossy — presence without content)
//
// A level above Max clamps to Max; a negative level clamps to 0. An empty input is "".
func LevelGist(texts []string, level int) string {
	if len(texts) == 0 {
		return ""
	}
	if level < 0 {
		level = 0
	}
	if level > MaxCompressionLevel {
		level = MaxCompressionLevel
	}
	switch level {
	case 0:
		return strings.Join(texts, " ")
	case 1:
		return texts[len(texts)-1]
	case 2:
		return headline(texts[len(texts)-1], 48)
	default: // 3
		return "(" + strconv.Itoa(len(texts)) + " thoughts)"
	}
}

// StepCompression moves a compression level toward MORE lossy (+1) or LESS lossy (-1), clamped to
// [0, MaxCompressionLevel]. +1 frees room (deeper gist); -1 restores detail (expand one level). This is
// the multi-level analogue of the binary compress/expand toggle.
func StepCompression(level, delta int) int {
	level += delta
	if level < 0 {
		return 0
	}
	if level > MaxCompressionLevel {
		return MaxCompressionLevel
	}
	return level
}

// headline trims s to at most n runes, appending an ellipsis when it truncated.
func headline(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
