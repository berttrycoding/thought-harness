package regulator

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// collectEmit records every emitted event so a test can assert on summary + data.
func collectEmit(sink *[]events.Event) events.Emit {
	return func(kind, summary string, data map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: data}
		*sink = append(*sink, ev)
		return ev
	}
}

// TestUpdateForkedSentinel pins the load-bearing forked=-1 sentinel: with no fork count
// supplied (DefaultUpdateOpts), offspring falls back to the admitted-1 proxy (n driven by
// admits); with an explicit fork count, n decouples from admitted. Golden values captured
// from the Python regulator.
func TestUpdateForkedSentinel(t *testing.T) {
	// Case A: sentinel -> proxy offspring = max(0, admitted-1) = 2.
	var evA []events.Event
	a := New(collectEmit(&evA), nil)
	oa := DefaultUpdateOpts() // Forked == -1
	oa.Fired, oa.Admitted, oa.Baseline, oa.BranchesLive = 2, 3, 1, 4
	a.Update(oa)
	wantA := events.D{"theta": 0.58, "lam_hat": 1.7, "lam_bar": 1.1666666666666665,
		"n": 0.7, "mu": 0.35, "U": 0.5}
	assertSnap(t, "A", evA[len(evA)-1].Data, wantA)
	if got := evA[len(evA)-1].Summary; got != "θ=0.58 λ̂=1.70 λ̄=1.17 n=0.70 μ=0.35 U=0.50" {
		t.Fatalf("A summary mismatch: %q", got)
	}

	// Case B: explicit Forked=0 -> offspring 0, n decoupled from admitted (n=0 not 0.7).
	var evB []events.Event
	b := New(collectEmit(&evB), nil)
	b.Update(UpdateOpts{Fired: 2, Admitted: 3, Baseline: 1, BranchesLive: 4, Forked: 0})
	wantB := events.D{"theta": 0.58, "lam_hat": 1.7, "lam_bar": 0.35,
		"n": 0.0, "mu": 0.35, "U": 0.5}
	assertSnap(t, "B", evB[len(evB)-1].Data, wantB)
}

// TestLamBarInfGuard drives n past 1 and confirms LamBar returns +Inf and the summary shows ∞.
func TestLamBarInfGuard(t *testing.T) {
	var ev []events.Event
	r := New(collectEmit(&ev), nil)
	for i := 0; i < 50; i++ {
		r.Update(UpdateOpts{Fired: 5, Admitted: 10, Baseline: 0, BranchesLive: 8, Forked: 10})
	}
	if !math.IsInf(r.LamBar(), 1) {
		t.Fatalf("expected LamBar == +Inf at n>=1, got %v (n=%v)", r.LamBar(), r.N())
	}
	if got := ev[len(ev)-1].Summary; got != "θ=0.95 λ̂=5.00 λ̄=∞ n=10.00 μ=0.00 U=1.00" {
		t.Fatalf("inf summary mismatch: %q", got)
	}
	if lb, ok := ev[len(ev)-1].Data["lam_bar"].(float64); !ok || !math.IsInf(lb, 1) {
		t.Fatalf("snap lam_bar should be +Inf, got %v", ev[len(ev)-1].Data["lam_bar"])
	}
}

// TestStabilityReactiveVsAwake pins the μ>0 NA-in-reactive / bool-in-awake split, the count, and the
// emitted wire shape (check name -> bool / NA string), plus the C0a/C0b reframe on a single-tick
// history: with no sustained loop the 0<K·g<2 check is NA ("insufficient-loop/open-loop") under the
// INSUFFICIENT-LOOP regime (HOLE 2 — a one-tick history is a vacuous loop, distinct from both a
// saturated rail and an honest fail) — NOT a silent prior-pass — so it does NOT count toward the held
// total.
func TestStabilityReactiveVsAwake(t *testing.T) {
	var ev []events.Event
	r := New(collectEmit(&ev), nil)
	r.Update(func() UpdateOpts {
		o := DefaultUpdateOpts()
		o.Fired, o.Admitted, o.Baseline, o.BranchesLive = 2, 3, 1, 4
		return o
	}())

	// reactive: μ>0 is NA; K·g is NA (insufficient-loop, one tick = no sustained loop); n<1, U<=1,
	// w*tau<PM hold => 3/5 counted, regime insufficient-loop.
	rea := r.Stability("reactive", true)
	if len(rea) != 5 {
		t.Fatalf("want 5 checks, got %d", len(rea))
	}
	mu := rea[4]
	if mu.Name != "mu>0 (awake baseline)" || !mu.NA {
		t.Fatalf("reactive μ>0 must be NA: %+v", mu)
	}
	kg := rea[2]
	if kg.Name != "0<K*g<2 (regulator stable)" || !kg.NA || kg.Pass {
		t.Fatalf("reactive K·g must be NA (open-loop), not a prior-pass: %+v", kg)
	}
	if kg.NADetail != "K·g N/A — insufficient-loop/open-loop" {
		t.Fatalf("reactive K·g NA detail wrong: %q", kg.NADetail)
	}
	st := ev[len(ev)-1]
	if st.Kind != events.Stability {
		t.Fatalf("expected stability event, got %s", st.Kind)
	}
	if st.Summary != "durable regime [insufficient-loop]: 3/5 hard checks hold" {
		t.Fatalf("reactive count summary wrong: %q", st.Summary)
	}
	if st.Data["regime"] != "insufficient-loop" {
		t.Fatalf("reactive regime wire value wrong: %v", st.Data["regime"])
	}
	if st.Data["0<K*g<2 (regulator stable)"] != "K·g N/A — insufficient-loop/open-loop" {
		t.Fatalf("reactive K·g wire value wrong: %v", st.Data["0<K*g<2 (regulator stable)"])
	}
	if st.Data["mu>0 (awake baseline)"] != "N/A reactive (self-terminating)" {
		t.Fatalf("reactive μ>0 wire value wrong: %v", st.Data["mu>0 (awake baseline)"])
	}
	if st.Data["n<1 (subcritical)"] != true {
		t.Fatalf("n<1 should be bool true on the wire, got %v", st.Data["n<1 (subcritical)"])
	}
	if st.Data["mode"] != "reactive" {
		t.Fatalf("mode key missing/wrong: %v", st.Data["mode"])
	}

	// awake: μ>0 becomes a real bool (mu=0.35 > 0 => true); K·g still NA (insufficient-loop); so 4/5
	// counted (n<1, U<=1, w*tau<PM, μ>0), regime insufficient-loop.
	awa := r.Stability("awake", true)
	if awa[4].NA || !awa[4].Pass {
		t.Fatalf("awake μ>0 must be a passing bool: %+v", awa[4])
	}
	if got := ev[len(ev)-1].Summary; got != "durable regime [insufficient-loop]: 4/5 hard checks hold" {
		t.Fatalf("awake count summary wrong: %q", got)
	}
	if ev[len(ev)-1].Data["mu>0 (awake baseline)"] != true {
		t.Fatalf("awake μ>0 wire value should be bool true")
	}
}

// TestStabilityNoEmit confirms emit=false returns the checks without producing an event.
func TestStabilityNoEmit(t *testing.T) {
	var ev []events.Event
	r := New(collectEmit(&ev), nil)
	checks := r.Stability("reactive", false)
	if len(checks) != 5 {
		t.Fatalf("want 5 checks, got %d", len(checks))
	}
	if len(ev) != 0 {
		t.Fatalf("emit=false must not emit, got %d events", len(ev))
	}
}

// TestHistoryRing240 confirms the EMA history is a bounded ring of 240 snapshots.
func TestHistoryRing240(t *testing.T) {
	r := New(nil, nil) // nil emit is allowed
	for i := 0; i < 300; i++ {
		r.Update(UpdateOpts{Fired: 1, Admitted: 1, Baseline: 0, BranchesLive: 1, Forked: 0})
	}
	h := r.History()
	if len(h) != 240 {
		t.Fatalf("history ring must cap at 240, got %d", len(h))
	}
	// nil emit must not panic and history still records.
	if _, ok := h[0]["theta"]; !ok {
		t.Fatalf("snapshot missing theta key")
	}
}

// TestRound3 pins round3 to Python's round(x, 3) for representative EMA-like values.
func TestRound3(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0.3, 0.3}, {0.1234567, 0.123}, {0.0005, 0.001}, {0.9995, 1.0},
		{1.0 / 3, 0.333}, {0.12345, 0.123}, {0.05, 0.05}, {0.95, 0.95},
	}
	for _, c := range cases {
		if got := round3(c.in); got != c.want {
			t.Fatalf("round3(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := round3(math.Inf(1)); !math.IsInf(got, 1) {
		t.Fatalf("round3(+Inf) should pass through, got %v", got)
	}
}

func assertSnap(t *testing.T, tag string, got, want events.D) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s snap key count: got %d want %d", tag, len(got), len(want))
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("%s snap missing key %q", tag, k)
		}
		gf, gok := gv.(float64)
		wf := wv.(float64)
		if !gok || gf != wf {
			t.Fatalf("%s snap[%q] = %v, want %v", tag, k, gv, wf)
		}
	}
}
