package campaign

// decode_aggregator.go — GATE-1 instrument (Phase-0 efficiency measurability). The
// existing cost wiring (addLLMCost) folds every llm.call event's completion_tokens into
// ONE running total per task. This aggregator is the same fold GROUPED BY ROLE: it answers
// "what share of a task's decode (completion) tokens does each role spend?" — the load-
// bearing question for whether the recall-saves-synthesis efficiency win is claude-
// measurable. The win is only claude-detectable if `synthesize_program`'s decode share is
// LARGE enough that skipping it on a recall clears the ~950-tok claude cost noise floor
// (W5-2c pooled SD). If synthesis is a minority even on the heaviest planning task, the
// A3-curve / W5-4 / D2 efficiency axes are honestly W6-only (a local substrate), like D5.
//
// The role string is carried verbatim on the event (internal/llm/openai.go:550 — the
// per-call `role`: conscious.generate / seam.transform / synthesize_program / action.respond
// / operator.<role> / specialist.<domain> / Controller.decide / Filter.judge_admission /
// form_intention / comprehend / emit_verdict / …). The grouping is over those exact strings.
//
// Determinism + additivity: this is a NEW pure reduction over the SAME event stream
// addLLMCost reads; it adds no behaviour and changes no existing default (the byte-identical
// gate). It is read-only over events — no RNG, no clock, no I/O.

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// RoleDecode is one role's decode (completion-token) footprint over a task: the role label,
// how many llm.call events it accounted for, and the completion (output) tokens those calls
// summed — the cache-immune cost the efficiency axis gates on. Prompt/total are NOT tracked
// here: the prompt side is masked by claude's prompt-cache (see the substrate note), so only
// completion is a trustworthy per-role efficiency signal on claude.
type RoleDecode struct {
	Role       string
	Calls      int
	Completion int
}

// DecodeBreakdown is a task's full per-role decode aggregate: the per-role rows (the grouped
// fold) plus the task total (so a role's SHARE = Role.Completion / Total is recoverable). The
// total matches what addLLMCost would have summed over the same stream — this is that sum,
// partitioned by role, never a different number.
type DecodeBreakdown struct {
	// Roles are the per-role decode rows, sorted by Completion DESC (the heaviest decoder
	// first — the role whose skip would move the cost needle most).
	Roles []RoleDecode
	// TotalCompletion is the sum of every role's Completion (== the task's full decode cost,
	// identical to addLLMCost's completion total over the same events).
	TotalCompletion int
	// TotalCalls is the sum of every role's call count (== addLLMCost's call total).
	TotalCalls int
}

// ShareOf returns the fraction of the task's total decode the named role spent (0 when the
// role is absent or the task did no decoding). This is the gate-1 read: ShareOf("synthesize_program").
func (d DecodeBreakdown) ShareOf(role string) float64 {
	if d.TotalCompletion == 0 {
		return 0
	}
	for _, r := range d.Roles {
		if r.Role == role {
			return float64(r.Completion) / float64(d.TotalCompletion)
		}
	}
	return 0
}

// CompletionOf returns the completion (output) tokens the named role spent on the task (0 if
// absent) — the absolute number behind the share (the synthesis-skip would save THIS many).
func (d DecodeBreakdown) CompletionOf(role string) int {
	for _, r := range d.Roles {
		if r.Role == role {
			return r.Completion
		}
	}
	return 0
}

// DecodeAggregator folds an llm.call event stream into a per-role decode breakdown. It is the
// GROUP-BY-role analogue of addLLMCost: subscribe it to an engine's bus (Fold per event), run
// the task, then read Breakdown(). It is single-threaded per engine run (the engine is serial
// within one run, so the closure that calls Fold is single-threaded — same contract addLLMCost
// relies on). For a parallel multi-task probe use ONE aggregator per task (per fresh engine),
// exactly as Bench/Probe use one set of counters per task.
type DecodeAggregator struct {
	byRole map[string]*RoleDecode
}

// NewDecodeAggregator builds an empty per-role decode aggregator.
func NewDecodeAggregator() *DecodeAggregator {
	return &DecodeAggregator{byRole: make(map[string]*RoleDecode)}
}

// Fold folds ONE event into the per-role aggregate: a non-llm.call event is ignored (same
// guard as addLLMCost); an llm.call event adds one call + its completion_tokens to its role's
// bucket. The role is read from the event Data (the verbatim per-call role label); a missing
// role label buckets under "" (so the total still reconciles with addLLMCost). This is the
// SINGLE place the per-role fold lives, mirroring addLLMCost as the single place the total fold
// lives.
func (a *DecodeAggregator) Fold(ev events.Event) {
	if ev.Kind != events.LLM {
		return
	}
	role := asStr(ev.Data["role"])
	c := intData(ev.Data, "completion_tokens")
	rd := a.byRole[role]
	if rd == nil {
		rd = &RoleDecode{Role: role}
		a.byRole[role] = rd
	}
	rd.Calls++
	rd.Completion += c
}

// Breakdown returns the accumulated per-role decode breakdown: rows sorted by completion DESC
// (ties broken by call count DESC then role name ASC for a stable, deterministic order), plus
// the reconciling totals. Pure — calling it does not mutate the aggregator, so it can be read
// mid-run or at the end.
func (a *DecodeAggregator) Breakdown() DecodeBreakdown {
	d := DecodeBreakdown{}
	for _, rd := range a.byRole {
		d.Roles = append(d.Roles, *rd)
		d.TotalCompletion += rd.Completion
		d.TotalCalls += rd.Calls
	}
	sort.Slice(d.Roles, func(i, j int) bool {
		if d.Roles[i].Completion != d.Roles[j].Completion {
			return d.Roles[i].Completion > d.Roles[j].Completion
		}
		if d.Roles[i].Calls != d.Roles[j].Calls {
			return d.Roles[i].Calls > d.Roles[j].Calls
		}
		return d.Roles[i].Role < d.Roles[j].Role
	})
	return d
}

// MergeBreakdowns pools several per-task breakdowns into ONE suite-level per-role aggregate
// (the share of synthesis decode ACROSS the heavy-synthesis suite, not just one task) — the
// gate-1 verdict reads the pooled synthesize_program share. Pure; deterministic in the input
// order (the output is re-sorted, so input order does not affect the result).
func MergeBreakdowns(bds []DecodeBreakdown) DecodeBreakdown {
	agg := NewDecodeAggregator()
	for _, bd := range bds {
		for _, r := range bd.Roles {
			rd := agg.byRole[r.Role]
			if rd == nil {
				rd = &RoleDecode{Role: r.Role}
				agg.byRole[r.Role] = rd
			}
			rd.Calls += r.Calls
			rd.Completion += r.Completion
		}
	}
	return agg.Breakdown()
}
