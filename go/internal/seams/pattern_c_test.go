package seams

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// These tests lock the M2 three-pattern wiring of the hidden seam:
//   - Pattern A: Gate.Select ranks via the deterministic control floor and never routes through a
//     backend (the Gate holds no backend at all — a structural guarantee).
//   - Pattern C: Filter.Admit runs the control floor ALWAYS and escalates to the model ONLY on a
//     flagged-fuzzy admission in llm/hybrid mode; the model may not override a structural reject;
//     every non-escalation of an escalation-eligible case is surfaced via escalation.floor_stands.

// escalatorStub is a backend that satisfies backends.FilterEscalator (the test double does NOT), so
// the Filter resolves to an escalating mode. It records every JudgeAdmission call and returns a
// configurable verdict + ok. It embeds the test double for the rest of the Backend surface so the
// non-escalation roles still work.
type escalatorStub struct {
	*backends.TestBackend
	calls   int                 // how many times JudgeAdmission was consulted
	verdict types.FilterVerdict // what to return when ok
	ok      bool                // whether the model "decided" (false ⇒ declined → floor stands)
}

func newEscalatorStub(verdict types.FilterVerdict, ok bool) *escalatorStub {
	return &escalatorStub{TestBackend: backends.NewTest(), verdict: verdict, ok: ok}
}

func (e *escalatorStub) JudgeAdmission(c types.Candidate, hist []types.Thought, floor types.FilterVerdict) (types.FilterVerdict, bool) {
	e.calls++
	if !e.ok {
		return floor, false
	}
	return e.verdict, true
}

var _ backends.FilterEscalator = (*escalatorStub)(nil)

// countKind tallies events of one kind in a captured stream.
func countKind(got *[]events.Event, kind string) int {
	n := 0
	for _, e := range *got {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// a clearly-trusted candidate (high source prior, no penalties) sits well above the ADMIT band edge
// → AdmitAmbiguity is low → not flagged-fuzzy. From a non-policed source so the source bump is off.
func trustedCandidate() types.Candidate {
	return types.Candidate{
		Text:      "the measurement returned a concrete grounded value of 42",
		Source:    types.OBSERVATION,
		Relevance: 0.95,
	}
}

// a borderline INJECTED candidate near the FLAG/ADMIT edge → flagged-fuzzy in hybrid mode.
func fuzzyCandidate() types.Candidate {
	// INJECTED with relevance tuned so the floor confidence lands near a band edge, and INJECTED is a
	// policed source (the membrane raises ambiguity for it), so it is reliably flagged-fuzzy.
	return types.Candidate{
		Text:      "this approach probably works for the cache layout",
		Source:    types.INJECTED,
		Domain:    sp("design"),
		Relevance: 0.6,
	}
}

func TestFilterControlModeNeverEscalates(t *testing.T) {
	bus, got := captureBus()
	esc := newEscalatorStub(types.FilterVerdict{Verdict: types.REJECT, Confidence: 0.1, Source: "llm"}, true)
	// mode "control" (the deterministic-floor mode value held through M1–M5) — the floor only.
	f := NewFilter(controlMode, esc, bus.Emit)

	for _, c := range []types.Candidate{trustedCandidate(), fuzzyCandidate()} {
		f.Admit(c, nil, 0.5)
	}
	if esc.calls != 0 {
		t.Fatalf("control mode must NEVER consult the model escalator; calls=%d", esc.calls)
	}
	if n := countKind(got, events.EscalationFloorStands); n != 0 {
		t.Fatalf("control mode is escalation-INELIGIBLE by design → no floor_stands events; got %d", n)
	}
}

func TestFilterHybridSkipsTrustedCandidate(t *testing.T) {
	bus, got := captureBus()
	esc := newEscalatorStub(types.FilterVerdict{Verdict: types.REJECT, Confidence: 0.1, Source: "llm"}, true)
	f := NewFilter("hybrid", esc, bus.Emit)

	c := trustedCandidate()
	floor := control.ScoreAdmit(c, nil, 0.5)
	if amb := control.AdmitAmbiguity(floor, c); amb >= control.AdmitAmbiguityThreshold {
		t.Fatalf("test invariant: trusted candidate must NOT be flagged-fuzzy (ambiguity=%v)", amb)
	}
	v := f.Admit(c, nil, 0.5)

	if esc.calls != 0 {
		t.Fatalf("hybrid must NOT escalate a non-flagged (clearly-trusted) candidate; calls=%d", esc.calls)
	}
	if v.Source == "llm" {
		t.Fatalf("a non-escalated verdict must be the floor's, not the model's")
	}
	// non-flagged hybrid case is escalation-ineligible → no floor_stands (avoid flooding).
	if n := countKind(got, events.EscalationFloorStands); n != 0 {
		t.Fatalf("a non-flagged hybrid case must not emit floor_stands (would flood); got %d", n)
	}
}

func TestFilterHybridEscalatesFlaggedCandidate(t *testing.T) {
	bus, _ := captureBus()
	// The model adopts a different verdict so we can prove its decision was taken.
	modelVerdict := types.FilterVerdict{Verdict: types.REJECT, Confidence: 0.12,
		Reason: "model saw a laundered claim", Source: "llm"}
	esc := newEscalatorStub(modelVerdict, true)
	f := NewFilter("hybrid", esc, bus.Emit)

	c := fuzzyCandidate()
	floor := control.ScoreAdmit(c, nil, 0.5)
	if amb := control.AdmitAmbiguity(floor, c); amb < control.AdmitAmbiguityThreshold {
		t.Fatalf("test invariant: fuzzy candidate must be flagged-fuzzy (ambiguity=%v < %v)",
			amb, control.AdmitAmbiguityThreshold)
	}
	v := f.Admit(c, nil, 0.5)

	if esc.calls != 1 {
		t.Fatalf("hybrid MUST escalate a flagged-fuzzy candidate exactly once; calls=%d", esc.calls)
	}
	if v.Source != "llm" || v.Verdict != types.REJECT || v.Reason != "model saw a laundered claim" {
		t.Fatalf("the model's escalated verdict must be ADOPTED; got %+v", v)
	}
}

func TestFilterDeclinedEscalationFloorStands(t *testing.T) {
	bus, got := captureBus()
	// ok=false ⇒ the model declined; the floor must stand AND floor_stands must fire (Rule 4).
	esc := newEscalatorStub(types.FilterVerdict{}, false)
	f := NewFilter("hybrid", esc, bus.Emit)

	c := fuzzyCandidate()
	floor := control.ScoreAdmit(c, nil, 0.5)
	v := f.Admit(c, nil, 0.5)

	if esc.calls != 1 {
		t.Fatalf("a flagged-fuzzy candidate must consult the model once; calls=%d", esc.calls)
	}
	if v.Verdict != floor.Verdict || v.Confidence != floor.Confidence || v.Source != floor.Source {
		t.Fatalf("on decline the FLOOR must stand; got %+v want floor %+v", v, floor)
	}
	if n := countKind(got, events.EscalationFloorStands); n != 1 {
		t.Fatalf("a declined escalation must surface exactly one floor_stands (Rule 4); got %d", n)
	}
	// the floor_stands data must say the model was consulted and declined.
	for _, e := range *got {
		if e.Kind == events.EscalationFloorStands {
			if e.Data["site"] != "filter.admit" {
				t.Errorf("floor_stands site=%v want filter.admit", e.Data["site"])
			}
			if e.Data["reason"] != "model-declined" {
				t.Errorf("floor_stands reason=%v want model-declined", e.Data["reason"])
			}
			if e.Data["model_consulted"] != true {
				t.Errorf("floor_stands model_consulted=%v want true", e.Data["model_consulted"])
			}
		}
	}
}

func TestFilterStructuralRejectNotEscalated(t *testing.T) {
	bus, got := captureBus()
	// A model that would ADMIT — to prove it is NOT given the chance to override a reality refutation.
	esc := newEscalatorStub(types.FilterVerdict{Verdict: types.ADMIT, Confidence: 0.9, Source: "llm"}, true)
	f := NewFilter("llm", esc, bus.Emit) // llm mode escalates everything EXCEPT structural facts

	// refuted-by-reality: a failed observation then an injection re-asserting success. The hard
	// refuted_by_reality signal is the STRUCTURAL FACT (reality said no) the model may not override —
	// regardless of which band the penalised score lands in. The high INJECTED prior keeps the score
	// in FLAG here, which is exactly why the guard must fire on the SIGNAL, not only on the REJECT band.
	failHist := []types.Thought{{
		Source:    types.OBSERVATION,
		RawReturn: types.Observation{Ok: false, Text: "tests failed"},
	}}
	stance := "runs"
	c := types.Candidate{Text: "it runs cleanly now", Source: types.INJECTED, Stance: &stance, Relevance: 0.9}

	floor := control.ScoreAdmit(c, failHist, 0.5)
	if _, ok := floor.Signals["refuted_by_reality"]; !ok {
		t.Fatalf("test invariant: candidate must carry the refuted_by_reality structural signal; got %+v", floor)
	}
	v := f.Admit(c, failHist, 0.5)

	if esc.calls != 0 {
		t.Fatalf("the model must NOT be consulted on a reality refutation (it cannot override it); calls=%d", esc.calls)
	}
	if v.Source == "llm" || v.Verdict != floor.Verdict || v.Confidence != floor.Confidence {
		t.Fatalf("a structural floor verdict must STAND unchanged; got %+v want floor %+v", v, floor)
	}
	// the structural skip in an escalating mode must be surfaced (Rule 4) with the structural reason.
	if n := countKind(got, events.EscalationFloorStands); n != 1 {
		t.Fatalf("a structural-fact skip in llm/hybrid mode must surface one floor_stands; got %d", n)
	}
	for _, e := range *got {
		if e.Kind == events.EscalationFloorStands {
			if e.Data["reason"] != "structural-reject" {
				t.Errorf("floor_stands reason=%v want structural-reject", e.Data["reason"])
			}
			if e.Data["model_consulted"] != false {
				t.Errorf("floor_stands model_consulted=%v want false (never asked)", e.Data["model_consulted"])
			}
		}
	}
}

func TestFilterNoModelWiredFloorStands(t *testing.T) {
	bus, got := captureBus()
	// hybrid mode requested but the backend is the test double (NOT a FilterEscalator) → the Filter
	// resolves to the control floor; a flagged-fuzzy case has no model to consult.
	f := NewFilter("hybrid", backends.NewTest(), bus.Emit)

	v := f.Admit(fuzzyCandidate(), nil, 0.5)
	if v.Source == "llm" {
		t.Fatalf("with no escalator wired the floor must stand; got %+v", v)
	}
	// the mode resolved to the floor (no FilterEscalator), so it is the pure-control posture →
	// escalation-ineligible → no floor_stands (consistent with the default control mode).
	if n := countKind(got, events.EscalationFloorStands); n != 0 {
		t.Fatalf("a non-escalator backend resolves to control → no floor_stands; got %d", n)
	}
}

// TestGateRanksWithoutBackend is the Pattern-A structural guarantee: a Gate built with NO backend
// ranks correctly via control.Rank. The Gate type no longer holds a backend field, so it CANNOT
// call a model — the test merely confirms the deterministic path produces the expected ordering.
func TestGateRanksWithoutBackend(t *testing.T) {
	bus, got := captureBus()
	gate := NewGate(bus.Emit) // no backend — ranking is control.Rank only

	cands := []types.Candidate{
		{Text: "a solid grounded answer with real substance to it", Source: types.INJECTED,
			Domain: sp("strong"), Relevance: 0.9},
		{Text: "maybe this could possibly work", Source: types.INJECTED,
			Domain: sp("weak"), Relevance: 0.3},
	}
	winner, losers, conflict, idx := gate.Select(cands, nil, map[string]float64{})

	if winner.Domain == nil || *winner.Domain != "strong" {
		t.Fatalf("control.Rank should pick the higher-relevance non-hedged candidate; got %v", winner.Domain)
	}
	if !conflict || len(losers) != 1 || idx != 0 {
		t.Fatalf("two survivors → conflict, one loser, winner idx 0; got conflict=%v losers=%d idx=%d",
			conflict, len(losers), idx)
	}
	// the gate emit's appraiser is the deterministic control floor (never a backend's name).
	for _, e := range *got {
		if e.Kind == events.Gate {
			if e.Data["appraiser"] != control.Appraiser {
				t.Fatalf("gate appraiser=%v want the control floor %q", e.Data["appraiser"], control.Appraiser)
			}
		}
	}
}
