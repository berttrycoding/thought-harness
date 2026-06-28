package engine

import (
	"math"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/critic"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// itoa is a small int->string used in the engine's summary strings (keeps the byte-identical emit
// sites from importing strconv at each call; the conversion itself matches strconv.Itoa).
func itoa(n int) string { return strconv.Itoa(n) }

// runeSlice reproduces Python str slicing `s[:n]` — by code point (rune), not byte. Used at the emit
// sites that truncate text the way Python does (e.g. goal[:60], text[:54]).
func runeSlice(s string, n int) string {
	r := []rune(s)
	if n < 0 {
		n = 0
	}
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// round2 reproduces Python round(x, 2) at the emit site (the sink does NOT round): format to 2 fixed
// decimals (round-half-to-even) and parse back so the emitted value matches the Python wire. +-Inf/NaN
// pass through unchanged.
func round2(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}

// setHas reports membership in a set (Python `x in s`).
func setHas(s map[int]struct{}, k int) bool {
	_, ok := s[k]
	return ok
}

// intPtr returns a pointer to v (for the optional *int branch-parent args).
func intPtr(v int) *int { return &v }

// derefIntOr dereferences p, or returns def when nil (the *int branch_id read at an emit site).
func derefIntOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// candPtrs converts a Candidate value slice (e.g. DefaultMode.Wander's return) to the pointer slice
// the reason core threads as raw returns (Python kept them as objects; Go's relay path takes a
// []*Candidate so the dispatch result and the wander result share one shape).
func candPtrs(cands []types.Candidate) []*types.Candidate {
	out := make([]*types.Candidate, len(cands))
	for i := range cands {
		c := cands[i]
		out[i] = &c
	}
	return out
}

// metaToMap reconstructs the Python `dict(self.controller.last_meta)` carried on StepResult.Meta — a
// faithful copy of the Controller's decision record. (StepResult is a return value, not an emitted
// event, so this map is consumed only by the CLI/TUI, never the golden JSONL stream.) The base keys are
// always present; the escalation extras (llm_decision/agree) appear only when an escalation occurred,
// and llm_why only when the model captured a reason — matching Python's conditional `last_meta.update`.
func metaToMap(m critic.ControllerMeta) map[string]any {
	out := map[string]any{
		"decision":           m.Decision,
		"heuristic_decision": m.HeuristicDecision,
		"stop_kind":          stopKindMeta(m.StopKind),
		"reason":             m.Reason,
		"branch_exhausted":   m.BranchExhausted,
		"loop_exhausted":     m.LoopExhausted,
		"flagged":            m.Flagged,
		"needs_ground_truth": m.NeedsGroundTruth,
		"ambiguity":          m.Ambiguity,
		"escalated":          m.Escalated,
	}
	if m.LLMSet {
		out["llm_decision"] = m.LLMDecision
		out["agree"] = m.Agree
	}
	if m.LLMWhy != "" {
		out["llm_why"] = m.LLMWhy
	}
	return out
}

// stopKindMeta resolves the optional stop-kind to its meta value: the NAME string, or nil for None
// (Python `stop_kind.name if stop_kind else None`).
func stopKindMeta(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
