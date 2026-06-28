package estimate

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// staleEstimator builds an M1 estimator with the M4 staleness layer ON at rate q, capturing emitted events.
func staleEstimator(q float64) (*Estimator, *[]events.Event) {
	log := &[]events.Event{}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Staleness = true
	cfg.StalenessQ = q
	e := New(cfg, func(kind, summary string, d map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: d}
		*log = append(*log, ev)
		return ev
	})
	return e, log
}

func countKind(log *[]events.Event, kind string) int {
	n := 0
	for _, ev := range *log {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// TestDecay_GrowsAnUnrefreshedGroundedBelief is the core M4 stateful property: a belief grounded at tick T
// (low variance) that is NOT re-observed has its variance GROWN by the per-tick Decay() sweep — back toward
// the prior ceiling, monotone with un-refreshed age — and emits estimate.decay. This is the dynamic-map
// process noise (Q>0, P4): a fact left un-refreshed becomes stale and the estimator wants to re-observe it.
func TestDecay_GrowsAnUnrefreshedGroundedBelief(t *testing.T) {
	e, log := staleEstimator(0.2)
	id := FromThoughtID(11)

	// Ground the belief at tick 1 -> it gets a low variance.
	e.SetTick(1)
	e.Note(id, 0.7)
	e.Observe(id, 1.0, control.TierPrecision(5)) // reality confirms at the gold tier
	groundedVar := e.varOf(id)
	if groundedVar >= e.cfg.PriorVar0 {
		t.Fatalf("a grounded belief must be more certain than the prior; grounded=%v prior=%v", groundedVar, e.cfg.PriorVar0)
	}

	// Advance a few ticks WITHOUT re-observing; each Decay() must GROW the variance strictly (it is far from
	// the ceiling at q=0.2 in the first handful of ticks), toward — but never past — the prior ceiling.
	prev := groundedVar
	for tick := 2; tick <= 8; tick++ {
		e.SetTick(tick)
		e.Decay()
		v := e.varOf(id)
		if v <= prev {
			t.Fatalf("tick %d: an un-refreshed grounded belief must DECAY (variance grow); prev=%v now=%v", tick, prev, v)
		}
		if v > e.cfg.PriorVar0+1e-9 {
			t.Fatalf("tick %d: decayed variance %v exceeded the prior ceiling %v", tick, v, e.cfg.PriorVar0)
		}
		prev = v
	}
	if countKind(log, events.EstimateDecay) == 0 {
		t.Fatal("WIRING GAP: a decaying belief must emit estimate.decay")
	}
	// far in the future the belief saturates AT the ceiling (a forever-stale fact is as uncertain as a
	// never-grounded one, never more) and the decay becomes a no-op (no more growth, no more event).
	for tick := 9; tick <= 60; tick++ {
		e.SetTick(tick)
		e.Decay()
	}
	if sat := e.varOf(id); sat > e.cfg.PriorVar0+1e-9 || sat < e.cfg.PriorVar0-1e-6 {
		t.Fatalf("a forever-stale belief must saturate AT the prior ceiling; got %v (ceiling %v)", sat, e.cfg.PriorVar0)
	}
}

// TestDecay_RefreshResetsTheClock is the freshness-reset property: re-grounding a belief resets its
// staleness clock (lastObs := now), so the NEXT tick's decay starts from age 1 again, not the accumulated
// age. A belief that is kept fresh by re-observation never goes stale.
func TestDecay_RefreshResetsTheClock(t *testing.T) {
	e, _ := staleEstimator(0.3)
	id := FromThoughtID(5)

	e.SetTick(1)
	e.Note(id, 0.6)
	e.Observe(id, 1.0, control.TierPrecision(4))

	// let it age 10 ticks (decaying).
	for tick := 2; tick <= 11; tick++ {
		e.SetTick(tick)
		e.Decay()
	}
	stale := e.varOf(id)

	// RE-OBSERVE at tick 11 -> the clock resets and the variance shrinks again (grounding is the var-reducer).
	e.Observe(id, 1.0, control.TierPrecision(4))
	refreshed := e.varOf(id)
	if refreshed >= stale {
		t.Fatalf("re-grounding must refresh (shrink) a stale belief; stale=%v refreshed=%v", stale, refreshed)
	}
	// one tick later the decay restarts from age 1 (a tiny growth), NOT the old accumulated age.
	e.SetTick(12)
	e.Decay()
	afterOneTick := e.varOf(id)
	if afterOneTick <= refreshed {
		t.Fatalf("decay must resume after a refresh; refreshed=%v afterOneTick=%v", refreshed, afterOneTick)
	}
	// the single-tick decay off a fresh grounding must be far smaller than the 10-tick-stale variance.
	if afterOneTick >= stale {
		t.Fatalf("a freshly re-grounded belief must not be as stale as the 10-tick-aged one; afterOneTick=%v stale=%v", afterOneTick, stale)
	}
}

// TestDecay_NeverTouchesNeverGroundedBeliefs: a belief that was only Note()d (never grounded) has no
// freshness stamp, so Decay() skips it — it is already at the high PriorVar0 (nothing to decay). The sweep
// only ages REALITY-grounded facts, which is the point of P4 (stale = "I observed this once, long ago").
func TestDecay_NeverTouchesNeverGroundedBeliefs(t *testing.T) {
	e, log := staleEstimator(0.4)
	id := FromThoughtID(9)

	e.SetTick(1)
	e.Note(id, 0.9) // asserted, never grounded -> at PriorVar0
	before := e.varOf(id)
	for tick := 2; tick <= 20; tick++ {
		e.SetTick(tick)
		e.Decay()
	}
	if e.varOf(id) != before {
		t.Fatalf("a never-grounded belief must not decay (it has no freshness stamp); before=%v after=%v", before, e.varOf(id))
	}
	if countKind(log, events.EstimateDecay) != 0 {
		t.Fatal("a never-grounded belief must emit no estimate.decay")
	}
}

// TestDecay_OffIsByteIdentical: with the staleness layer OFF, Decay() is a no-op — a grounded belief left
// un-refreshed keeps its grounded variance forever and emits nothing. The OFF path is byte-identical to M1.
func TestDecay_OffIsByteIdentical(t *testing.T) {
	log := &[]events.Event{}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Staleness = false // M1 only
	e := New(cfg, func(kind, summary string, d map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: d}
		*log = append(*log, ev)
		return ev
	})
	id := FromThoughtID(2)
	e.SetTick(1)
	e.Note(id, 0.7)
	e.Observe(id, 1.0, control.TierPrecision(4))
	grounded := e.varOf(id)
	for tick := 2; tick <= 50; tick++ {
		e.SetTick(tick)
		if n := e.Decay(); n != 0 {
			t.Fatalf("tick %d: Decay() must be a no-op with the layer off; decayed %d", tick, n)
		}
	}
	if e.varOf(id) != grounded {
		t.Fatalf("layer OFF: a grounded belief must not decay; grounded=%v after=%v", grounded, e.varOf(id))
	}
	if countKind(log, events.EstimateDecay) != 0 {
		t.Fatal("layer OFF: estimate.decay must not fire")
	}
}

// TestDecay_StaysConsistentUnderM5: the M5 monitor accounts a decay as ZERO information gain (variance
// growth can never be spurious information). With the consistency monitor ON, a long decay stretch must
// leave the witness CONSISTENT (no spurious gain) — M4 is provably safe against the Huang-2010
// inconsistency the M5 witness guards.
func TestDecay_StaysConsistentUnderM5(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Staleness = true
	cfg.StalenessQ = 0.25
	cfg.Monitor = true // M5 consistency accounting on
	e := New(cfg, nil)
	id := FromThoughtID(4)

	e.SetTick(1)
	e.Note(id, 0.7)
	e.Observe(id, 1.0, control.TierPrecision(5)) // one legitimate grounded gain
	for tick := 2; tick <= 40; tick++ {
		e.SetTick(tick)
		e.Decay() // pure variance GROWTH -> no information gained -> no spurious gain
	}
	c := e.ConsistencyState()
	if !c.Consistent() {
		t.Fatalf("staleness decay must not break the M5 consistency invariant; spuriousGain=%v", c.SpuriousGain)
	}
	if c.SpuriousGain > consistencyEpsilon {
		t.Fatalf("decay (variance growth) must gain NO information; spuriousGain=%v", c.SpuriousGain)
	}
}
