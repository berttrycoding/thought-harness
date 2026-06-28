package estimate

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// igEstimator builds an estimator with M1 innovation + the M6 info-gain layer on (M6 requires M1). The
// covariance layer is also on so the correlation-reach term is live (M6 reads the M2 covGraph).
func igEstimator(t *testing.T) (*Estimator, *[]events.Event) {
	t.Helper()
	var log []events.Event
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Covariance = true
	cfg.InfoGain = true
	e := New(cfg, func(kind, summary string, d events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: d}
		log = append(log, ev)
		return ev
	})
	return e, &log
}

func countInfoGain(log []events.Event) int {
	n := 0
	for _, ev := range log {
		if ev.Kind == events.EstimateInfoGain {
			n++
		}
	}
	return n
}

// goldPrec is the gold-tier precision the engine ranks against (the most a single grounding can teach).
var goldPrec = control.TierPrecision(5)

// TestInfoGain_RanksUncertainBeliefFirst is the next-best-VIEW criterion: of several tracked beliefs, the
// one the harness is LEAST sure about (highest variance) is surfaced as the next-best-observation — the
// directed-grounding signal points at what the harness does NOT yet know. A grounded (low-variance)
// belief is NOT chosen even if it was grounded recently — re-confirming it cannot beat its first-grounding
// floor (P1), and the info-gain ranks it last.
func TestInfoGain_RanksUncertainBeliefFirst(t *testing.T) {
	e, _ := igEstimator(t)
	// b_certain: grounded by a strong observation -> low variance.
	e.Note("b_certain", 0.5)
	e.Observe("b_certain", 1.0, goldPrec) // reality confirmed -> P shrinks
	// b_uncertain: stated but never grounded -> stays at the high PriorVar0.
	e.Note("b_uncertain", 0.5)

	ranked := e.NextBestObservation(goldPrec)
	if len(ranked) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(ranked))
	}
	if ranked[0].ID != "b_uncertain" {
		t.Fatalf("next-best-observation must be the UNCERTAIN belief, got %q (gains: %v)", ranked[0].ID, ranked)
	}
	if !(ranked[0].Gain > ranked[1].Gain) {
		t.Fatalf("the uncertain belief must have strictly higher gain: %v <= %v", ranked[0].Gain, ranked[1].Gain)
	}
}

// TestInfoGain_RootBeatsLeafAtEqualVariance is the active-SLAM payoff: among beliefs of EQUAL variance,
// the one many siblings CO-VARY with (a shared grounding root) is the better next-best-observation —
// grounding it leverages the observation across everything that root backs. This is the joint-uncertainty
// reduction no per-belief scalar (M1 alone) can express; it needs the M2 correlation reach.
func TestInfoGain_RootBeatsLeafAtEqualVariance(t *testing.T) {
	e, _ := igEstimator(t)
	// Three sibling beliefs all derived from one shared upstream -> they co-vary through it.
	root := UpstreamID("u_shared")
	e.Note("b_root", 0.5)
	e.Link("b_root", root)
	e.Note("b_sib1", 0.5)
	e.Link("b_sib1", root)
	e.Note("b_sib2", 0.5)
	e.Link("b_sib2", root)
	// An ISOLATED belief at the SAME variance (no shared upstream).
	e.Note("b_leaf", 0.5)

	ranked := e.NextBestObservation(goldPrec)
	// Find the leaf and a root-cluster member; the cluster members must out-rank the isolated leaf.
	var leafGain, clusterGain float64
	for _, c := range ranked {
		switch c.ID {
		case "b_leaf":
			leafGain = c.Gain
		case "b_root":
			clusterGain = c.Gain
		}
	}
	if !(clusterGain > leafGain) {
		t.Fatalf("a co-varying root must out-rank an equal-variance isolated leaf: cluster=%v leaf=%v", clusterGain, leafGain)
	}
	// The leaf must have ZERO reach; a cluster member must have positive reach (the M2 leverage).
	for _, c := range ranked {
		if c.ID == "b_leaf" && c.Reach != 0 {
			t.Fatalf("isolated leaf must have 0 reach, got %v", c.Reach)
		}
		if c.ID == "b_root" && !(c.Reach > 0) {
			t.Fatalf("co-varying root must have positive reach, got %v", c.Reach)
		}
	}
}

// TestInfoGain_RankingNeverAltersBeliefs is the consistency invariant for M6 (it is a PURE RANKING): the
// next-best-observation reads the variance trajectory but must NEVER write it — only a grounded Observe()
// may move a belief. Ranking the same beliefs many times must leave the side-table byte-identical, so M6
// cannot fabricate certainty (the §0/M5 invariant the consistency witness guards).
func TestInfoGain_RankingNeverAltersBeliefs(t *testing.T) {
	e, _ := igEstimator(t)
	e.Note("a", 0.5)
	e.Note("b", 0.5)
	e.Observe("a", 1.0, goldPrec)
	beforeA := e.varOf("a")
	beforeB := e.varOf("b")
	for i := 0; i < 50; i++ {
		e.NextBestObservation(goldPrec)
	}
	if e.varOf("a") != beforeA || e.varOf("b") != beforeB {
		t.Fatalf("ranking altered a belief variance: a %v->%v, b %v->%v", beforeA, e.varOf("a"), beforeB, e.varOf("b"))
	}
}

// TestInfoGain_OffIsByteIdentical pins the default-OFF contract: with the info-gain layer off the ranking
// is a no-op (returns nil, emits NO estimate.infogain event), so an infogain-OFF run does exactly the
// M1/M2 work and is byte-identical.
func TestInfoGain_OffIsByteIdentical(t *testing.T) {
	var log []events.Event
	cfg := DefaultConfig()
	cfg.Enabled = true   // M1 on
	cfg.InfoGain = false // M6 off
	e := New(cfg, func(kind, summary string, d events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: d}
		log = append(log, ev)
		return ev
	})
	e.Note("a", 0.5)
	if got := e.NextBestObservation(goldPrec); got != nil {
		t.Fatalf("info-gain OFF must return nil, got %v", got)
	}
	if n := countInfoGain(log); n != 0 {
		t.Fatalf("info-gain OFF must emit no estimate.infogain event, got %d", n)
	}
	if e.Exploring() {
		t.Fatalf("Exploring() must be false when the layer is off")
	}
}

// TestInfoGain_NoBeliefsIsNoOp pins that ranking an empty estimator is a silent no-op (no candidates, no
// event) — an episode with nothing to rank is byte-identical.
func TestInfoGain_NoBeliefsIsNoOp(t *testing.T) {
	e, log := igEstimator(t)
	if got := e.NextBestObservation(goldPrec); got != nil {
		t.Fatalf("empty estimator must return nil, got %v", got)
	}
	if n := countInfoGain(*log); n != 0 {
		t.Fatalf("empty estimator must emit no event, got %d", n)
	}
}

// TestInfoGain_Deterministic locks the ranking is reproducible on the seeded loop: identical state ranks
// in identical order with identical gains every time (a non-deterministic sort would break the goldens).
func TestInfoGain_Deterministic(t *testing.T) {
	build := func() []InfoGainCandidate {
		e, _ := igEstimator(t)
		e.Note("x", 0.3)
		e.Note("y", 0.7)
		e.Note("z", 0.5)
		e.Observe("y", 1.0, goldPrec)
		return e.NextBestObservation(goldPrec)
	}
	a, b := build(), build()
	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d != %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Gain != b[i].Gain {
			t.Fatalf("non-deterministic ranking at %d: %+v != %+v", i, a[i], b[i])
		}
	}
}
