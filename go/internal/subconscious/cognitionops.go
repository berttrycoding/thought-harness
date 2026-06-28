// Cognition operators that EXECUTE against real cognitive state (P3).
//
// Some operators don't touch the world (no tool) but still do genuine work on the *thinking
// itself*:
//
//	rank        order the live branches by their ACTUAL value V(s)
//	eliminate   drop the concretely weakest line (lowest V(s))
//	decompose   segment the goal into REAL sub-parts (split on clauses/conjunctions)
//
// Where P2 made effectful operators dispatch real TOOLS, this makes these operators compute
// against the real graph + value signal — instead of emitting a name-keyed template sentence
// ("inputs, core step, check"). The result is carried as a structured payload on the Candidate
// (the operator contract: a rank move returns an ordering, an eliminate move returns the dropped
// line, a decompose returns parts).
//
// Ported from the (now-removed) Python thought_harness/subconscious/cognition_ops.py. This file emits NO events
// (it returns payloads; the caller — the sub-agent in COGNITION-EXEC mode — voices the result).
// The payload map type is events.D (a map[string]any alias) for project-wide consistency; its keys
// + values are byte-identical to the Python dict so the downstream wire stream matches. The numeric
// payload fields replicate Python round(x, 3) at this same site (the sink does not round).
package subconscious

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/value"
)

// BranchScore is one (branch id, V(s)) pair — the Go form of Python's (int, float) tuple in
// CognitiveView.branch_scores. A named struct keeps the ordering/min logic readable.
type BranchScore struct {
	ID    int
	Value float64
}

// CognitiveView is a read-only window onto the live thinking state, handed to a sub-agent so a
// cognition operator computes against REAL branches + their value — not a thought slice. Branch
// values are read LIVE through the value signal, so a rank reflects the current V(s) regardless of
// tick ordering. Mirrors Python CognitiveView.
type CognitiveView struct {
	graph *graph.ThoughtGraph
	value *value.ValueSignal
}

// NewCognitiveView binds a view to the live graph + value signal (Python CognitiveView.__init__).
func NewCognitiveView(g *graph.ThoughtGraph, v *value.ValueSignal) *CognitiveView {
	return &CognitiveView{graph: g, value: v}
}

// Goal is the episode goal (Python CognitiveView.goal property).
func (c *CognitiveView) Goal() string { return c.graph.Goal }

// BranchScores returns (branch id, V(s)) for every live branch, value computed fresh from the
// value signal. Mirrors Python branch_scores. Python iterates self._graph.branches in dict
// insertion order (= id-ascending, bids monotonic); Go map iteration is unordered, so the bids are
// sorted ascending to reproduce that order deterministically (the value math is read-only).
func (c *CognitiveView) BranchScores() []BranchScore {
	bids := make([]int, 0, len(c.graph.Branches))
	for bid := range c.graph.Branches {
		bids = append(bids, bid)
	}
	sort.Ints(bids)
	out := make([]BranchScore, 0, len(bids))
	for _, bid := range bids {
		out = append(out, BranchScore{ID: bid, Value: c.value.BranchValue(c.graph, bid)})
	}
	return out
}

// connectorsRe matches the clause/conjunction boundaries a goal really decomposes on (a genuine
// split, not a template). Mirrors Python _CONNECTORS:
//
//	re.compile(r"\s*(?:\band then\b|\bthen\b|\band\b|[;,]|->|→)\s*", re.IGNORECASE)
//
// Go's regexp (RE2) supports `\b`, `(?i)`, alternation, and the literal `->`/`→`, so the pattern is
// reproduced verbatim with the (?i) inline flag standing in for re.IGNORECASE.
var connectorsRe = regexp.MustCompile(`(?i)\s*(?:\band then\b|\bthen\b|\band\b|[;,]|->|→)\s*`)

// SegmentGoal splits a goal into real sub-parts on its clause boundaries. Falls back to the whole
// goal as a single part; returns an empty slice for an empty goal. Mirrors Python segment_goal.
//
// Python strips " ." from each part (str.strip(" .")) and keeps only non-empty parts; if at least
// two survive it returns them, else it returns the whole stripped goal (or [] if empty).
func SegmentGoal(goal string) []string {
	parts := make([]string, 0)
	for _, p := range connectorsRe.Split(goal, -1) {
		p = strings.Trim(p, " .")
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) >= 2 {
		return parts
	}
	g := strings.Trim(goal, " .")
	if g != "" {
		return []string{g}
	}
	return []string{}
}

// ExecuteCognitionOp runs a cognition operator against the live state. It returns (text, payload,
// true) for an operator with real cognitive semantics here, or ("", nil, false) when this operator
// has none (the Go form of Python returning None ⇒ the sub-agent reasons instead). Mirrors Python
// execute_cognition_op; the trailing bool replaces Python's `... | None`.
func ExecuteCognitionOp(opName, goal string, view *CognitiveView) (string, events.D, bool) {
	switch opName {
	case "rank":
		scores := view.BranchScores()
		if len(scores) == 0 {
			return "", nil, false
		}
		// sorted(..., key=value, reverse=True): descending by value, STABLE so equal-value
		// branches keep BranchScores' id-ascending order (Python's sorted is stable too).
		sorted := make([]BranchScore, len(scores))
		copy(sorted, scores)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Value > sorted[j].Value })
		// order = [(bid, round(v, 3)) for bid, v in scores]: each entry holds the round3'd value.
		// The wire payload's `ordering` is a list of [bid, value] pairs (a 2-element list, matching
		// the Python tuple, which JSON-serialises as a 2-element array).
		ordering := make([]any, 0, len(sorted))
		textParts := make([]string, 0, len(sorted))
		for _, s := range sorted {
			rv := round3(s.Value)
			ordering = append(ordering, []any{s.ID, rv})
			// text uses the value FROM `order` (i.e. the already-round3'd rv) formatted as %.2f,
			// reproducing Python's f"b{b}({v:.2f})" iterating over `order` (round(round(x,3),2)).
			textParts = append(textParts, fmt.Sprintf("b%d(%s)", s.ID, format2f(rv)))
		}
		text := "ranked the lines by value: " + strings.Join(textParts, ", ")
		return text, events.D{"op": "rank", "ordering": ordering}, true

	case "eliminate":
		scores := view.BranchScores()
		if len(scores) == 0 {
			return "", nil, false
		}
		// min(scores, key=value): Python's min returns the FIRST minimum on ties; BranchScores is
		// id-ascending, so the first-min mirrors Python exactly with a strict-less comparison.
		minS := scores[0]
		for _, s := range scores[1:] {
			if s.Value < minS.Value {
				minS = s
			}
		}
		text := fmt.Sprintf("eliminated the weakest line b%d (value %s)", minS.ID, format2f(minS.Value))
		return text, events.D{"op": "eliminate", "dropped": minS.ID, "value": round3(minS.Value)}, true

	case "decompose":
		parts := SegmentGoal(goal)
		if len(parts) == 0 {
			return "", nil, false
		}
		// to_dict-style: parts is carried as a []string; it JSON-serialises as a list of strings,
		// matching the Python list. The payload value keeps the same element type ([]string ->
		// []any not needed; a []string marshals identically).
		text := "decomposed into: " + strings.Join(parts, "; ")
		return text, events.D{"op": "decompose", "parts": parts}, true
	}

	return "", nil, false
}

// round3 reproduces Python's round(x, 3): format to 3 fixed decimals (round-half-to-even) and parse
// back, so the payload value matches the Python wire byte-for-byte. +∞/NaN pass through. Mirrors
// the value/regulator round3 (the per-emit-site rounding obligation; the caller's sink does NOT
// round).
func round3(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return r
}

// format2f reproduces Python's f"{v:.2f}" — 2 fixed decimals, round-half-to-even (both
// strconv.FormatFloat and CPython format use round-half-to-even at the boundary). Used for the
// human-readable text, not the payload.
func format2f(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }
