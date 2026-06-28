package estimate

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// covCfg builds an estimator with the M1 innovation + the M2 covariance layer BOTH on (M2 requires M1).
func covEstimator(t *testing.T) (*Estimator, *[]events.Event) {
	t.Helper()
	var log []events.Event
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Covariance = true
	e := New(cfg, func(kind, summary string, d events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: d}
		log = append(log, ev)
		return ev
	})
	return e, &log
}

func countCorrelate(log []events.Event) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.EstimateCorrelate {
			n++
		}
	}
	return n
}

// TestCovariance_RefuteInflatesCorrelatedSibling is the CORE M2 thinking: two beliefs that share a
// grounding upstream CO-VARY; when reality REFUTES one, the other LOSES certainty (its variance grows)
// because the shared grounding that backed it just proved unreliable — the estimator catches CORRELATED
// self-deception no per-belief scalar can see.
func TestCovariance_RefuteInflatesCorrelatedSibling(t *testing.T) {
	e, log := covEstimator(t)
	const up = UpstreamID("u-root") // a shared grounding ancestor
	a, b := BeliefID("a"), BeliefID("b")
	// both A and B were derived from the same upstream -> they co-vary.
	e.Link(a, up)
	e.Link(b, up)
	// B is asserted confidently but never grounded -> high prior variance (PriorVar0).
	e.Note(b, 0.9)
	bVarBefore := e.varOf(b)

	// reality REFUTES A hard (a big innovation). A is associated (not gated).
	rA := e.Observe(a, -1.0, control.TierPrecision(3))
	if rA.Gated {
		t.Fatalf("the refutation of A must not be gated for this test")
	}
	n := e.PropagateRefutation(a, abs(rA.Innov))
	if n != 1 {
		t.Fatalf("exactly one correlated sibling (B) must be inflated; inflated %d", n)
	}
	bVarAfter := e.varOf(b)
	if bVarAfter <= bVarBefore {
		t.Fatalf("the co-varying sibling B must LOSE certainty (variance grow) when A is refuted; before=%v after=%v", bVarBefore, bVarAfter)
	}
	if countCorrelate(*log) != 1 {
		t.Fatalf("a correlated inflation must emit exactly one estimate.correlate; got %d", countCorrelate(*log))
	}
}

// TestCovariance_IndependentBeliefUntouched is the SPARSITY/consistency floor: a belief that shares NO
// upstream with the refuted one is INDEPENDENT, so it must be left EXACTLY unchanged (no edge, no
// propagation, no event) — an episode with no shared grounding is byte-identical to M1.
func TestCovariance_IndependentBeliefUntouched(t *testing.T) {
	e, log := covEstimator(t)
	a, c := BeliefID("a"), BeliefID("c")
	e.Link(a, UpstreamID("u-x"))
	e.Link(c, UpstreamID("u-y")) // a DIFFERENT upstream -> independent of A
	e.Note(c, 0.9)
	cBefore := e.varOf(c)

	rA := e.Observe(a, -1.0, control.TierPrecision(3))
	if n := e.PropagateRefutation(a, abs(rA.Innov)); n != 0 {
		t.Fatalf("an independent belief must NOT be inflated; inflated %d", n)
	}
	if e.varOf(c) != cBefore {
		t.Fatalf("an independent belief must be untouched; before=%v after=%v", cBefore, e.varOf(c))
	}
	if countCorrelate(*log) != 0 {
		t.Fatalf("no correlated inflation -> no estimate.correlate; got %d", countCorrelate(*log))
	}
	if e.CovarianceEdges() != 0 {
		t.Fatalf("two beliefs with disjoint upstreams form NO edge (sparse); edges=%d", e.CovarianceEdges())
	}
}

// TestCovariance_LayerOff_NoOp is the BYTE-IDENTICAL guarantee: with the covariance layer OFF (M1 only),
// Link / PropagateRefutation are no-ops — no graph, no propagation, no event. This is what makes the
// default-OFF path identical to M1.
func TestCovariance_LayerOff_NoOp(t *testing.T) {
	var log []events.Event
	cfg := DefaultConfig()
	cfg.Enabled = true     // M1 on
	cfg.Covariance = false // M2 OFF
	e := New(cfg, func(kind, summary string, d events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: d}
		log = append(log, ev)
		return ev
	})
	const up = UpstreamID("u-root")
	a, b := BeliefID("a"), BeliefID("b")
	e.Link(a, up) // no-op when M2 is off
	e.Link(b, up)
	e.Note(b, 0.9)
	bBefore := e.varOf(b)
	rA := e.Observe(a, -1.0, control.TierPrecision(3))
	if n := e.PropagateRefutation(a, abs(rA.Innov)); n != 0 {
		t.Fatalf("M2 off must propagate nothing; inflated %d", n)
	}
	if e.varOf(b) != bBefore {
		t.Fatalf("M2 off must leave the sibling untouched; before=%v after=%v", bBefore, e.varOf(b))
	}
	if countCorrelate(log) != 0 {
		t.Fatalf("M2 off must emit no estimate.correlate; got %d", countCorrelate(log))
	}
	if e.CovarianceEdges() != 0 {
		t.Fatalf("M2 off must build no correlation graph; edges=%d", e.CovarianceEdges())
	}
}

// TestCovariance_PropagationNeverGainsInformation is the load-bearing M5/§0 invariant under M2: a
// correlated propagation may only RAISE variance (lose certainty), never lower it, so it can NEVER gain
// spurious information. We run BOTH the M5 monitor and M2 together and assert the consistency witness
// stays clean (zero spurious gain) after a refute-and-propagate — M2 stays inside the consistency
// invariant the awake-durability gate requires.
func TestCovariance_PropagationNeverGainsInformation(t *testing.T) {
	var log []events.Event
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Covariance = true
	cfg.Monitor = true // M5 consistency witness on
	e := New(cfg, func(kind, summary string, d events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: d}
		log = append(log, ev)
		return ev
	})
	const up = UpstreamID("u-root")
	a, b := BeliefID("a"), BeliefID("b")
	e.Link(a, up)
	e.Link(b, up)
	e.Note(b, 0.9)
	// a real grounded refutation of A (legitimate information) + the correlated inflation of B.
	rA := e.Observe(a, -1.0, control.TierPrecision(3))
	e.PropagateRefutation(a, abs(rA.Innov))
	c := e.ConsistencyState()
	if c.SpuriousGain > 1e-9 {
		t.Fatalf("M2 propagation must gain NO spurious information; spuriousGain=%v", c.SpuriousGain)
	}
	if !c.Consistent() {
		t.Fatalf("the estimator must stay CONSISTENT under M2 propagation; %+v", c)
	}
}

// TestCovariance_StrongerSharingInflatesMore checks the graded structure: a sibling sharing MORE
// upstreams with the refuted belief loses MORE certainty than one sharing fewer (the correlation
// coefficient is monotone in shared count).
func TestCovariance_StrongerSharingInflatesMore(t *testing.T) {
	e, _ := covEstimator(t)
	a := BeliefID("a")
	strong := BeliefID("strong")
	weak := BeliefID("weak")
	// a shares two upstreams with strong, one with weak.
	e.Link(a, UpstreamID("u1"))
	e.Link(a, UpstreamID("u2"))
	e.Link(strong, UpstreamID("u1"))
	e.Link(strong, UpstreamID("u2"))
	e.Link(weak, UpstreamID("u1"))
	strongBefore := e.varOf(strong)
	weakBefore := e.varOf(weak)
	rA := e.Observe(a, -1.0, control.TierPrecision(3))
	e.PropagateRefutation(a, abs(rA.Innov))
	strongDelta := e.varOf(strong) - strongBefore
	weakDelta := e.varOf(weak) - weakBefore
	if !(strongDelta > weakDelta && weakDelta > 0) {
		t.Fatalf("a more strongly-correlated sibling must lose more certainty; strongDelta=%v weakDelta=%v", strongDelta, weakDelta)
	}
}

// abs is a local |x| for the test (avoids a math import).
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
