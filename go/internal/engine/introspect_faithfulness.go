// introspect_faithfulness.go is the INTROSPECTIVE-FAITHFULNESS self-report instrument (Track H,
// benchmark-taxonomy docs/internal/notes/2026-06-20-benchmark-taxonomy.md §8). It answers §8's question — "can you
// ask the harness what it is thinking / how confident it is / what its goal is, and is the answer FAITHFUL
// to the actual internal state?" — and makes the answer an OBSERVABLE, failable property of a live run.
//
// WHY IT IS TESTABLE — THERE IS A GROUND TRUTH. The observability contract means every readable-layer state
// is addressable: the active goal (e.graph.Goal), the EXPANDED branch's tip thought (the active LINE — what
// the mind is thinking about now), the lifecycle STATE, the confidence/value V(s) (e.valueScalar — the
// active branch's epistemic value), and the engine's OWN recent-event count (the read_event_log ring). So a
// SELF-REPORT can be checked against the state that actually IS. Faithfulness = agreement between what the
// harness SAYS it is doing/thinking and what the graph/lifecycle/V(s)/bus show. A confabulated self-report is
// laundered hallucination in the introspective channel — the same failure the Filter exists to kill — so the
// faithfulness check is a Filter for self-reports.
//
// THE READABLE LAYERS (this slice) + THE HONEST "I CAN'T SEE THAT" (the opaque layer). The transparency
// ladder (§8): conscious thought / active line, action/reasoning, perception, and state/confidence/goal are
// READABLE (the watched seam + the event bus + the addressable graph). The SUBCONSCIOUS hidden seam
// (FILTER->GATE->TRANSFORM) is OPAQUE BY DESIGN — it re-voices the winning candidate so it reads as the
// mind's own next thought; surfacing it would change the cognition. So this instrument reports the readable
// layers faithfully AND reports the subconscious layer as UNOBSERVABLE rather than confabulating a
// plausible-but-false answer (the introspective twin of the DECLINE neg-control). Piercing the hidden seam
// needs a separate, gated transparency layer (out of this slice — named in the §8 backlog); the honest
// decline is the correct behaviour without it.
//
// FAITHFUL BY CONSTRUCTION, FAILABLE BY CHECK. The report is ASSEMBLED from the readable ground truth, so
// every readable field agrees with its source — faithful=true holds over the readable layers. The check is
// not a tautology: it re-reads each field's ground truth INDEPENDENTLY and compares, so a field whose
// reported value does NOT match its source (a confabulation injected into the report) is caught and marks
// the field — and the run — UNFAITHFUL. The cognition-property test drives exactly that: it confabulates a
// field and asserts the check flags it (and asserts the opaque layer is declined, not confabulated).
//
// PURE CONTROL (PATTERN-A). It is a deterministic read + comparison over engine state. NO model call, NO
// seeded-RNG draw, NO clock read. It authors no conscious-stream text and injects no thought — it only emits
// the introspect.faithfulness witness on the bus.
//
// DEFAULT OFF => BYTE-IDENTICAL. The self-report is assembled + emitted ONLY when the opt-in
// introspect.self_report knob is ON. Default OFF => no report, no faithfulness check, no
// introspect.faithfulness event => the live loop is byte-identical to the pre-instrument engine. Mirrors the
// senseClock / orientOnce / wireEventTap gating shape.
//
// HEADLESS-PURE. No I/O, no wall clock, no unseeded randomness.
package engine

import (
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// introspectLayerSubconscious names the OPAQUE hidden-seam layer the self-report honestly declines to
// observe (the "I can't see that" property). Surfacing FILTER->GATE->TRANSFORM is not free — it would change
// the cognition (the seam re-voices the winning candidate so it reads as the mind's own thought), so absent
// a gated transparency layer the faithful answer is "unobservable", never a confabulated arbitration story.
const introspectLayerSubconscious = "subconscious"

// reportField is one readable-layer claim in the self-report: WHAT the harness reports (Reported), WHAT the
// independently re-read ground truth IS (Observed), and whether the two AGREE (Faithful). Layer names the
// transparency-ladder layer the claim belongs to (so a TUI/trace can group by layer). Built FROM the ground
// truth, so Faithful is true for an honest report; a confabulated Reported value flips it to false — that is
// the failable property.
type reportField struct {
	Layer    string // the transparency-ladder layer: conscious | state | value | perception
	Reported string // what the self-report SAYS (the introspective claim)
	Observed string // the INDEPENDENTLY re-read ground truth (the state that actually IS)
	Faithful bool   // Reported == Observed (the per-field agreement)
}

// selfReport is the structured introspective answer to "what are you thinking / how confident / what is your
// goal", over the READABLE layers, plus the OPAQUE layer(s) it honestly declines to observe. It is a frozen
// snapshot (value copies), so it carries no live pointers into engine structures.
type selfReport struct {
	Goal         string        // the goal currently being thought (state layer)
	Line         string        // the active LINE — the EXPANDED branch's tip thought (conscious layer)
	State        string        // the lifecycle state (state layer)
	Value        float64       // V(s) — the active branch's epistemic value, the confidence self-report (value layer)
	RecentEvents int           // how many of the engine's OWN events the read_event_log ring holds (perception layer)
	Fields       []reportField // every readable claim, each with its ground-truth agreement
	Opaque       []string      // the layer(s) reported as UNOBSERVABLE (honest "I can't see that")
	Faithful     bool          // the conjunction over every readable field (an unfaithful field fails the run)
}

// selfReportEnabled reports whether the introspective-faithfulness instrument may fire: the opt-in
// introspect.self_report knob is ON. nil features => false (the default path never reads the flag, so it
// stays byte-identical). It also requires a live graph (the readable ground truth comes from it).
func (e *Engine) selfReportEnabled() bool {
	return e.features != nil && e.features.Introspect.SelfReport && e.graph != nil
}

// activeLineTip returns the active LINE the mind is thinking about now — the EXPANDED (active) branch's tip
// thought text. This is the "what are you thinking about?" ground truth: the watched/conscious layer's
// current focus. "" when the active branch is empty (a fresh, unthought branch).
func (e *Engine) activeLineTip() string {
	if e.graph == nil {
		return ""
	}
	if t := e.graph.Last(); t != nil {
		return strings.TrimSpace(t.Text)
	}
	return ""
}

// buildSelfReport assembles the structured self-report over the READABLE layers and CHECKS each field against
// its independently re-read ground truth. The opaque subconscious (hidden-seam) layer is reported as
// unobservable (the honest "I can't see that"). It is a pure read — no model call, no RNG, no clock. The
// report is built FROM the ground truth, so each readable field is faithful by construction; the per-field
// check is still real (it re-derives the ground truth independently and compares), which is what lets the
// cognition-property test inject a confabulation and observe the check flag it.
func (e *Engine) buildSelfReport() selfReport {
	r := selfReport{
		Goal:         e.graph.Goal,
		Line:         e.activeLineTip(),
		State:        e.lifecycle.State.String(),
		Value:        e.valueScalar(),
		RecentEvents: e.recentEventCount(),
	}
	// The honest "I can't see that": the subconscious hidden seam is OPAQUE by design (no gated
	// transparency layer in this slice), so the self-report declines it rather than confabulating an
	// arbitration story.
	r.Opaque = []string{introspectLayerSubconscious}
	// The readable claims, each tagged with a DISTINCT layer key so the faithfulness oracle can re-derive
	// each field's ground truth unambiguously, each checked against its independently re-read ground truth.
	r.Fields = []reportField{
		introspectField("goal", r.Goal, e.graph.Goal),
		introspectField("conscious", r.Line, e.activeLineTip()),
		introspectField("lifecycle", r.State, e.lifecycle.State.String()),
		introspectField("value", fmtValue(r.Value), fmtValue(e.valueScalar())),
		introspectField("perception", itoa(r.RecentEvents), itoa(e.recentEventCount())),
	}
	r.Faithful = true
	for _, f := range r.Fields {
		if !f.Faithful {
			r.Faithful = false
		}
	}
	return r
}

// introspectField builds one readable claim and resolves its agreement: Faithful iff the reported value
// matches the independently re-read ground truth. A confabulated reported value (one that does not match its
// source) yields Faithful=false — the failable property the §8 test asserts.
func introspectField(layer, reported, observed string) reportField {
	return reportField{Layer: layer, Reported: reported, Observed: observed, Faithful: reported == observed}
}

// checkSelfReport re-checks a (possibly tampered) self-report against the engine's CURRENT readable ground
// truth — the faithfulness ORACLE. It re-derives each readable field's ground truth INDEPENDENTLY (it does
// NOT trust the report's Observed field) and recomputes Faithful, then folds the conjunction. This is the
// surface the cognition-property test confabulates against: hand it a report with a doctored Goal/Line and it
// returns a report whose matching field is Faithful=false and whose top-level Faithful is false. The opaque
// layer(s) are carried through unchanged (declined, never re-checked into a confabulation). Pure read.
func (e *Engine) checkSelfReport(r selfReport) selfReport {
	// The independently re-read ground truth, keyed by the field's DISTINCT layer tag. The report's own
	// Observed/Faithful are NOT trusted (a confabulator could have lied about them too) — only the layer tag
	// + the engine's current state decide agreement.
	truth := map[string]string{
		"goal":       e.graph.Goal,
		"conscious":  e.activeLineTip(),
		"lifecycle":  e.lifecycle.State.String(),
		"value":      fmtValue(e.valueScalar()),
		"perception": itoa(e.recentEventCount()),
	}
	rechecked := make([]reportField, 0, len(r.Fields))
	allFaithful := true
	for _, f := range r.Fields {
		observed, known := truth[f.Layer]
		if !known {
			// An unrecognised layer cannot be grounded against the readable truth — it is not a faithful
			// readable claim, so it fails the conjunction rather than passing silently.
			observed = f.Observed
		}
		faithful := known && f.Reported == observed
		if !faithful {
			allFaithful = false
		}
		rechecked = append(rechecked, reportField{Layer: f.Layer, Reported: f.Reported, Observed: observed, Faithful: faithful})
	}
	out := r
	out.Fields = rechecked
	out.Faithful = allFaithful
	return out
}

// maybeSelfReport is the LIVE-LOOP wire: it emits ONE introspective-faithfulness self-report at quiescence
// (reactive IDLE / awake ASLEEP) and bounds it to AT MOST ONCE per quiescence via the selfReportedEpisode
// guard, so a sustained idle/asleep run does not re-emit it every tick. It is a no-op unless the opt-in
// introspect.self_report knob is ON (default OFF ⇒ no report, no event ⇒ byte-identical) — and the guard is
// only ever read/written on that path, so the bare loop never touches it. startEpisode clears the guard so
// each new episode produces one fresh self-report at its own quiescence.
func (e *Engine) maybeSelfReport(tick int) {
	if !e.selfReportEnabled() || e.selfReportedEpisode {
		return
	}
	e.selfReportedEpisode = true
	e.emitSelfReport(tick)
}

// emitSelfReport assembles, checks, and emits the introspect.faithfulness witness — the engine half of the
// §8 introspective-faithfulness instrument. It is a no-op when the introspect.self_report knob is off (knob
// off => no report, no event => byte-identical). The report is faithful over the readable layers by
// construction; the witness carries the per-field agreement + the declined opaque layer(s) so a TUI/trace/
// test reads the true self-model honesty. Returns the assembled report (for direct test inspection) and
// whether it was faithful; ({}, false) when disabled. Pure CONTROL (no model, no RNG, no clock).
func (e *Engine) emitSelfReport(tick int) (selfReport, bool) {
	if !e.selfReportEnabled() {
		return selfReport{}, false
	}
	r := e.checkSelfReport(e.buildSelfReport())
	e.bus.Emit(events.IntrospectFaithfulness, selfReportSummary(r), events.D{
		"tick":          tick,
		"goal":          r.Goal,
		"line":          runeSlice(r.Line, 80),
		"state":         r.State,
		"value":         r.Value,
		"recent_events": r.RecentEvents,
		"fields":        introspectFieldsData(r.Fields),
		"opaque":        r.Opaque,
		"faithful":      r.Faithful,
	})
	return r, true
}

// introspectFieldsData renders the per-field agreement for the event payload (a stable []map, deterministic
// order = the readable-claim order). Each entry is {layer, reported, observed, faithful} so the witness
// carries the exact self-report-vs-ground-truth comparison the §8 test asserts.
func introspectFieldsData(fields []reportField) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		out = append(out, map[string]any{
			"layer":    f.Layer,
			"reported": runeSlice(f.Reported, 80),
			"observed": runeSlice(f.Observed, 80),
			"faithful": f.Faithful,
		})
	}
	return out
}

// selfReportSummary renders the one-line console string for the introspect.faithfulness event. Deterministic
// — no clock, no RNG. It names the verdict (FAITHFUL / UNFAITHFUL), the readable state, and the declined
// opaque layer (the honest "I can't see that").
func selfReportSummary(r selfReport) string {
	verdict := "FAITHFUL"
	if !r.Faithful {
		verdict = "UNFAITHFUL"
	}
	var b strings.Builder
	b.WriteString("self-report ")
	b.WriteString(verdict)
	b.WriteString(": state=")
	b.WriteString(r.State)
	b.WriteString(" V(s)=")
	b.WriteString(fmtValue(r.Value))
	if strings.TrimSpace(r.Goal) != "" {
		b.WriteString(" goal=")
		b.WriteString(runeSlice(strings.TrimSpace(r.Goal), 40))
	}
	if len(r.Opaque) > 0 {
		b.WriteString(" | opaque(can't-see): ")
		b.WriteString(strings.Join(r.Opaque, ","))
	}
	return b.String()
}

// fmtValue renders V(s) deterministically (a fixed 2-decimal form, sign-correct) so the reported confidence
// and the re-read ground truth compare byte-for-byte (a float printed twice the same way is stably
// comparable). strconv.FormatFloat with -1 precision would vary by value; a fixed 2 decimals is stable and
// locale-free, and it renders negative epistemic values honestly (a refuted line's V(s) can go below zero).
func fmtValue(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
