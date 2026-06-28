// Package appraisal is the reasoning-capture dataset, read back from the event bus (P6).
//
// Every appraising decision emits its (value, verdict, reason, signals, source) on the bus —
// the Filter (seam.filter), the Gate (seam.gate), the value signal (value.update), and the
// Controller (critic.decision). Collect reads those events back into a flat []types.Appraisal:
// one labelled row per decision — the substrate a learning layer trains on (the deterministic
// control-floor signals paired with, eventually, an LLM's reasoning for the SAME decision). No new
// bookkeeping layer:
// the bus already carried it; this just collects it.
//
// Data, not control: this package only reads the event log and pairs the deterministic control
// FLOOR (control.ScoreAdmit) with the model's ESCALATION judgment (a FilterEscalator) for the
// teaching pair — it changes no decision. It imports types, events, control and backends.
package appraisal

import (
	"math"
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Collect flattens the appraisal-bearing events of a run into Appraisal records (the labelled
// dataset). Mirrors Python collect_appraisals: a pure read-back of the event log, one Appraisal
// per Filter / Gate / Value / Decision event, in event order.
func Collect(evs []events.Event) []types.Appraisal {
	out := make([]types.Appraisal, 0, len(evs))
	for _, e := range evs {
		d := e.Data
		switch e.Kind {
		case events.Filter:
			out = append(out, types.Appraisal{
				Site:          "filter.admit",
				Value:         getFloat(d, "confidence", 0.0),
				Verdict:       getStrPtr(d, "verdict"),
				Reason:        getStr(d, "reason", ""),
				Signals:       copyAnyMap(getMap(d, "signals")),
				Source:        getStr(d, "appraiser", control.Appraiser),
				AppraiserConf: 1.0,
			})
		case events.Gate:
			scores := getMap(d, "scores")
			reasons := getMap(d, "reasons")
			winner := getStr(d, "winner", "")
			top := maxFloat(scores, 0.0)
			reason, ok := reasons[winner]
			reasonStr := ""
			if ok {
				reasonStr = toStr(reason)
			}
			if reasonStr == "" {
				if truthy(d["conflict"]) {
					reasonStr = "conflict -> forked losers"
				} else {
					reasonStr = "highest-ranked survivor"
				}
			}
			out = append(out, types.Appraisal{
				Site:          "gate.select",
				Value:         top,
				Verdict:       getStrPtr(d, "winner"),
				Reason:        reasonStr,
				Signals:       copyAnyMap(scores),
				Source:        getStr(d, "appraiser", control.Appraiser),
				AppraiserConf: 1.0,
			})
		case events.Value:
			active := getInt(d, "active", 0)
			values := getMap(d, "values")
			key := "b" + strconv.Itoa(active)
			val := toFloat(values[key]) // 0.0 when absent (Python .get(..., 0.0))
			verdict := key
			out = append(out, types.Appraisal{
				Site:          "value.branch",
				Value:         val,
				Verdict:       &verdict,
				Reason:        getStr(d, "reason", ""),
				Signals:       copyAnyMap(getMap(d, "signals")),
				Source:        getStr(d, "appraiser", control.Appraiser),
				AppraiserConf: 1.0,
			})
		case events.Decision:
			reason := getStr(d, "why", "")
			if reason == "" {
				reason = getStr(d, "reason", "")
			}
			source := control.Appraiser // the deterministic control floor decided (Pattern A/C floor)
			if truthy(d["escalated"]) {
				source = "llm"
			}
			out = append(out, types.Appraisal{
				Site:          "critic.decide",
				Value:         getFloat(d, "ambiguity", 0.0),
				Verdict:       getStrPtr(d, "decision"),
				Reason:        reason,
				Signals:       map[string]any{}, // Python signals={} (non-nil empty)
				Source:        source,
				AppraiserConf: 1.0,
			})
		}
	}
	return out
}

// DualResult is the teaching pair from DualAppraise: the deterministic control FLOOR and the
// model's ESCALATION appraisal of the SAME admission decision, whether they agree on the verdict,
// and the gap between their values (the supervision signal for distilling one into the other).
// Floor is the always-valid Pattern-A control answer (control.ScoreAdmit); LLM is the Pattern-C
// ceiling — the model's escalation judgment, or (when no model is wired / it declines) the floor
// itself with Source noting "model-declined" (Rule 4: the floor stands, surfaced).
type DualResult struct {
	Floor    types.Appraisal
	LLM      types.Appraisal
	Agree    bool
	ValueGap float64
}

// DualAppraise pairs the deterministic control FLOOR with the model's ESCALATION judgment of the
// SAME admission decision — the teaching pair: the floor's cheap signal breakdown next to the
// model's reasoning. The floor is control.ScoreAdmit (always valid, no backend). The model side is
// the Pattern-C escalation: if llmBackend is a FilterEscalator, the model REFINES the floor verdict
// via JudgeAdmission; otherwise (no model, or it declines) the floor STANDS and is returned with
// Source="model-declined" so the non-escalation is surfaced (Rule 4), never silently faked. Returns
// both as Appraisals, whether they agree on the verdict, and the rounded value gap. Data, not
// control — it changes no decision.
func DualAppraise(candidate types.Candidate, history []types.Thought, value float64,
	llmBackend backends.Backend) DualResult {
	floorV := control.ScoreAdmit(candidate, history, value)
	f := floorV.AsAppraisalDefault()

	m := f // default: the floor stands as the "LLM" appraisal when no model refines it
	if esc, ok := llmBackend.(backends.FilterEscalator); ok {
		if v, refined := esc.JudgeAdmission(candidate, history, floorV); refined {
			m = v.AsAppraisalDefault()
		} else {
			// model declined / unavailable -> the floor stands; mark the non-escalation (Rule 4).
			m.Source = "model-declined"
		}
	} else {
		// no model escalator wired -> the floor IS the appraisal; mark it.
		m.Source = "model-declined"
	}

	agree := verdictEq(f.Verdict, m.Verdict)
	return DualResult{
		Floor:    f,
		LLM:      m,
		Agree:    agree,
		ValueGap: round3(math.Abs(f.Value - m.Value)),
	}
}

// ----------------------------------------------------------------------------
// any-coercion helpers — the event log is map[string]any; these reproduce Python's d.get /
// float() / dict() / max(default=) over the heterogeneous payload without ever panicking.
// ----------------------------------------------------------------------------

// getMap returns d[key] as a map[string]any, or nil if absent/not a map. Mirrors Python
// `d.get(key) or {}` (a missing or non-map value yields the empty map at every read site).
func getMap(d map[string]any, key string) map[string]any {
	if m, ok := d[key].(map[string]any); ok {
		return m
	}
	return nil
}

// getStr returns d[key] as a string, or def if absent/not a string (Python d.get(key, def)).
func getStr(d map[string]any, key, def string) string {
	if s, ok := d[key].(string); ok {
		return s
	}
	return def
}

// getStrPtr returns *string for d[key] when it is a string, else nil — the *string optional that
// reproduces Python's `d.get(key)` defaulting to None (which Appraisal.verdict tolerates).
func getStrPtr(d map[string]any, key string) *string {
	if s, ok := d[key].(string); ok {
		return &s
	}
	return nil
}

// getFloat returns d[key] coerced to float64, or def if absent (Python float(d.get(key, def))).
func getFloat(d map[string]any, key string, def float64) float64 {
	if v, ok := d[key]; ok {
		return toFloat(v)
	}
	return def
}

// getInt returns d[key] coerced to int, or def if absent. The Value event emits `active` as an
// int in-memory; JSON round-trips it as float64 — handle both.
func getInt(d map[string]any, key string, def int) int {
	v, ok := d[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	}
	return def
}

// toFloat coerces an any (int/float family) to float64; non-numeric (incl. nil) -> 0.0, matching
// Python float() over the numeric payloads the bus actually carries at these sites.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0.0
}

// toStr coerces an any to string (only strings are expected at the reason sites); else "".
func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// truthy reproduces Python's bool(x) for the flag keys (conflict / escalated): absent or false
// -> false. Only the bool case is load-bearing here (both keys are emitted as bools).
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case nil:
		return false
	}
	return v != nil
}

// maxFloat reproduces Python max(scores.values(), default=def): the largest coerced value over
// the map, or def when the map is empty.
func maxFloat(m map[string]any, def float64) float64 {
	if len(m) == 0 {
		return def
	}
	first := true
	best := def
	for _, v := range m {
		f := toFloat(v)
		if first || f > best {
			best = f
			first = false
		}
	}
	return best
}

// copyAnyMap returns a shallow copy of m (nil-safe), so a collected Appraisal never aliases the
// source event's map — Python's dict(...) copy at the read site. A nil source yields a non-nil
// empty map so the record's Signals marshals as {} not null (the value/filter signals are always
// dicts in Python).
func copyAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// verdictEq reports whether two optional verdicts are equal — Python `h.verdict == m.verdict`,
// where each is a string (or None). Two None verdicts compare equal.
func verdictEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// round3 reproduces Python's round(x, 3): format to 3 fixed decimals (round-half-to-even, as both
// strconv.FormatFloat and CPython's float __round__ use) and parse back, so value_gap matches the
// Python wire byte-for-byte. +∞/NaN pass through unchanged. Mirrors the value/regulator round3.
func round3(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	r, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 3, 64), 64)
	return r
}
