package seams

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

func sp(s string) *string { return &s }

// captureBus subscribes a slice collector to a fresh Bus (the Python reference harness shape).
func captureBus() (*events.Bus, *[]events.Event) {
	bus := events.NewDefault()
	var got []events.Event
	bus.Subscribe(func(e events.Event) { got = append(got, e) })
	return bus, &got
}

// TestHiddenSeamRelayParity pins the FILTER -> GATE -> TRANSFORM -> INJECT pipeline against the
// Python reference (PORT-PLAN Tier-2 gate: "seams relay produces the same seam.filter/gate/
// transform/inject events for a fixed candidate set"). The full event stream + the RelayResult
// were captured by RUNNING thought_harness/seams/hidden.py over the identical fixture:
//
//	history: [GENERATED conf 0.7 "Working through the safety review of the deploy script",
//	          GENERATED conf 0.6 "The script touches production config"]
//	candidates (rawReturns):
//	  safety: INJECTED rel 0.9 stance "safe"  "The deploy looks safe to ship now"
//	  risk:   INJECTED rel 0.85 stance "unsafe" "Actually this deploy is unsafe and risky"
//	  guess:  INJECTED rel 0.4  (hedged)       "maybe it could possibly work"
//	  tiny:   INJECTED rel 0.3                 "x"   (rejected as malformed)
//	bias = {safety: 0.05, risk: 0.1}; value = 0.5
//
// Python result: winner=risk (biased 0.943 > safety 0.900 — a stable-sort/bias outcome), conflict
// (safe vs unsafe stances), losers [safety, guess], injected conf 0.806, voiced text
// "It comes to me: actually this deploy is unsafe and risky.".
func TestHiddenSeamRelayParity(t *testing.T) {
	bus, got := captureBus()
	backend := backends.NewTest()
	filt := NewFilter("control", backend, bus.Emit)
	gate := NewGate(bus.Emit)
	seam := NewHiddenSeam(gate, filt, backend, bus.Emit)

	hist := []types.Thought{
		{ID: 1, Text: "Working through the safety review of the deploy script",
			Source: types.GENERATED, Confidence: 0.7},
		{ID: 2, Text: "The script touches production config",
			Source: types.GENERATED, Confidence: 0.6},
	}
	cands := []types.Candidate{
		{Text: "The deploy looks safe to ship now", Source: types.INJECTED,
			Domain: sp("safety"), Relevance: 0.9, Stance: sp("safe")},
		{Text: "Actually this deploy is unsafe and risky", Source: types.INJECTED,
			Domain: sp("risk"), Relevance: 0.85, Stance: sp("unsafe")},
		{Text: "maybe it could possibly work", Source: types.INJECTED,
			Domain: sp("guess"), Relevance: 0.4},
		{Text: "x", Source: types.INJECTED, Domain: sp("tiny"), Relevance: 0.3},
	}
	bias := map[string]float64{"safety": 0.05, "risk": 0.1}

	res := seam.Relay(cands, hist, bias, 0.5)

	// -- RelayResult parity --
	if res.Thought == nil {
		t.Fatalf("expected an injected thought")
	}
	if res.Thought.Text != "It comes to me: actually this deploy is unsafe and risky." {
		t.Errorf("voiced text=%q", res.Thought.Text)
	}
	if res.Thought.Source != types.INJECTED {
		t.Errorf("thought source=%v want INJECTED", res.Thought.Source)
	}
	if !floatNear(res.Thought.Confidence, 0.806) {
		t.Errorf("thought confidence=%v want 0.806", res.Thought.Confidence)
	}
	if res.Winner == nil || res.Winner.Domain == nil || *res.Winner.Domain != "risk" {
		t.Errorf("winner domain=%v want risk", res.Winner)
	}
	if !res.Conflict {
		t.Errorf("expected conflict (safe vs unsafe stances)")
	}
	gotLosers := make([]string, len(res.Losers))
	for i, l := range res.Losers {
		gotLosers[i] = *l.Domain
	}
	if len(gotLosers) != 2 || gotLosers[0] != "safety" || gotLosers[1] != "guess" {
		t.Errorf("losers=%v want [safety guess]", gotLosers)
	}
	// verdicts are over EVERY raw return (the full intake record), in input order.
	wantVerdicts := []struct {
		domain  string
		verdict types.Verdict
		conf    float64
	}{
		{"safety", types.ADMIT, 0.824},
		{"risk", types.ADMIT, 0.806},
		{"guess", types.FLAG, 0.464},
		{"tiny", types.REJECT, 0.0},
	}
	if len(res.Verdicts) != len(wantVerdicts) {
		t.Fatalf("verdicts len=%d want %d", len(res.Verdicts), len(wantVerdicts))
	}
	for i, w := range wantVerdicts {
		v := res.Verdicts[i]
		if *v.Candidate.Domain != w.domain || v.Verdict.Verdict != w.verdict ||
			!floatNear(v.Verdict.Confidence, w.conf) {
			t.Errorf("verdict[%d] = %s/%v/%.4f want %s/%v/%.4f",
				i, *v.Candidate.Domain, v.Verdict.Verdict, v.Verdict.Confidence,
				w.domain, w.verdict, w.conf)
		}
	}

	// -- event stream parity: 4 filter + 1 gate + 1 transform + 1 inject, in order --
	type ev struct {
		kind, summary string
		data          events.D
	}
	want := []ev{
		{events.Filter, "INJECTED/safety: ADMIT (0.82) source-trusted (prior 0.72)",
			events.D{"verdict": "ADMIT", "confidence": 0.8240000000000001,
				"reason":    "source-trusted (prior 0.72)",
				"signals":   map[string]any{"source_prior": 0.724, "value_prior": 0.1},
				"appraiser": control.Appraiser, "source": "INJECTED",
				"domain": "safety", "text": "The deploy looks safe to ship now"}},
		{events.Filter, "INJECTED/risk: ADMIT (0.81) source-trusted (prior 0.71)",
			events.D{"verdict": "ADMIT", "confidence": 0.806,
				"reason":    "source-trusted (prior 0.71)",
				"signals":   map[string]any{"source_prior": 0.706, "value_prior": 0.1},
				"appraiser": control.Appraiser, "source": "INJECTED",
				"domain": "risk", "text": "Actually this deploy is unsafe and risky"}},
		{events.Filter, "INJECTED/guess: FLAG (0.46) hedged (-0.18) lowered trust",
			events.D{"verdict": "FLAG", "confidence": 0.4640000000000001,
				"reason":    "hedged (-0.18) lowered trust",
				"signals":   map[string]any{"source_prior": 0.544, "value_prior": 0.1, "hedged": -0.18},
				"appraiser": control.Appraiser, "source": "INJECTED",
				"domain": "guess", "text": "maybe it could possibly work"}},
		{events.Filter, "INJECTED/tiny: REJECT (0.00) empty/malformed",
			events.D{"verdict": "REJECT", "confidence": 0.0, "reason": "empty/malformed",
				"signals":   map[string]any{"malformed": -1.0},
				"appraiser": control.Appraiser, "source": "INJECTED",
				"domain": "tiny", "text": "x"}},
		{events.Gate, "winner=risk (of 3) [CONFLICT->fork losers]",
			events.D{"winner": "risk", "conflict": true,
				"losers": []any{"safety", "guess"},
				"scores": map[string]any{"risk": 0.843, "safety": 0.85, "guess": 0.383},
				"reasons": map[string]any{
					"safety": "relevance 0.90", "risk": "relevance 0.85",
					"guess": "relevance 0.40, hedged"},
				"appraiser": control.Appraiser}},
		{events.Transform,
			"raw: 'Actually this deploy is unsafe and risky' -> voiced: 'It comes to me: actually this deploy is un'",
			events.D{"raw": "Actually this deploy is unsafe and risky",
				"voiced": "It comes to me: actually this deploy is unsafe and risky.",
				"domain": "risk"}},
		{events.Inject, "INJECTED (risk, conf=0.81)",
			events.D{"domain": "risk", "confidence": 0.806}},
	}
	if len(*got) != len(want) {
		for i, e := range *got {
			t.Logf("got[%d] %s %q %#v", i, e.Kind, e.Summary, e.Data)
		}
		t.Fatalf("emitted %d events, want %d", len(*got), len(want))
	}
	for i, w := range want {
		g := (*got)[i]
		if g.Kind != w.kind {
			t.Errorf("event[%d] kind=%q want %q", i, g.Kind, w.kind)
		}
		if g.Summary != w.summary {
			t.Errorf("event[%d] summary=%q want %q", i, g.Summary, w.summary)
		}
		if !seamDataEqual(g.Data, w.data) {
			t.Errorf("event[%d] (%s) data:\n got  = %#v\n want = %#v", i, w.kind, g.Data, w.data)
		}
	}
}

// SR-1 (seam-as-channel) removed Gate.Conflicting: the seam no longer derives "conflict" from
// hand-set stances. Conflict is now content-neutral — whether more than one candidate survived
// admission (competing alternatives exist) — and the Controller, not the membrane, resolves it. The
// stance-detector test that pinned the old behaviour is intentionally gone; the survivor-count
// signal is covered by TestHiddenSeamRelayParity (3 survivors -> conflict) + the engine property
// test TestGateForksConflictKeepsBothViews (competing candidates surface as branches).

func floatNear(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// seamDataEqual deep-compares two event-data maps with numeric coercion and []any handling, so a
// golden literal matches the emitted map regardless of int/float representation. nil-vs-nil (the
// JSON-null domain) compares equal.
func seamDataEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !seamValEqual(av, bv) {
			return false
		}
	}
	return true
}

func seamValEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !seamValEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && seamDataEqual(av, bv)
	default:
		if an, aok := seamFloat(a); aok {
			if bn, bok := seamFloat(b); bok {
				return an == bn
			}
			return false
		}
		return a == b
	}
}

func seamFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
