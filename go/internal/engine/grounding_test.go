package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/grounding"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// realObs builds a watched-seam OBSERVATION thought whose RawReturn is a REAL (non-fabricated)
// observation — what a genuine tool execution leaves behind, as opposed to the heuristic stand-in
// which marks every observation Fabricated:true.
func realObs(ok bool, bridge string) types.Thought {
	return types.Thought{
		ID: -1, Source: types.OBSERVATION, RawReturn: types.Observation{Ok: ok, Fabricated: false, Bridge: bridge},
	}
}

// TestGroundingWireEmitsOnRealObservation is the Phase-A gate for the grounding wire: a REAL
// observation feeds the reality-grounding spine and emits grounding.ground (verdict/tier/status), and
// the claim is grounded/refuted in the ledger. The ok case grounds; the failed case refutes.
func TestGroundingWireEmitsOnRealObservation(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	var got []events.Event
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Ground {
			got = append(got, ev)
		}
	})

	e.groundObservation(types.Intention{Claim: "the build passes"}, realObs(true, "structured"))
	e.groundObservation(types.Intention{Claim: "the tests pass"}, realObs(false, "structured"))

	if len(got) != 2 {
		t.Fatalf("want 2 grounding.ground events, got %d", len(got))
	}
	if got[0].Data["verdict"] != "grounded" {
		t.Errorf("first claim should be grounded, got %v", got[0].Data["verdict"])
	}
	if got[1].Data["verdict"] != "refuted" {
		t.Errorf("failed observation should refute, got %v", got[1].Data["verdict"])
	}
	// the ledger reflects it: a grounded firsthand observation reads as BELIEVE (single observation,
	// not deterministic), a refuted one also as BELIEVE (we know it's false).
	if e.Grounding().Status("the build passes") != grounding.Believe {
		t.Errorf("grounded observation should be BELIEVE, got %v", e.Grounding().Status("the build passes"))
	}
	if e.Grounding().Len() != 2 {
		t.Errorf("ledger should hold 2 experiments, got %d", e.Grounding().Len())
	}
}

// TestGroundingWireRejectsFabricated is the golden-safety invariant in test form: a FABRICATED tier-0
// observation (every heuristic act) is rejected by the grounding wire — it emits nothing and never
// enters the ledger. This is WHY the scenario goldens stay byte-identical with the wire live.
func TestGroundingWireRejectsFabricated(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	emitted := 0
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Ground {
			emitted++
		}
	})
	fabricated := types.Thought{ID: -1, Source: types.OBSERVATION,
		RawReturn: types.Observation{Ok: true, Fabricated: true}}
	e.groundObservation(types.Intention{Claim: "2+2=5"}, fabricated)
	if emitted != 0 {
		t.Fatalf("fabricated observation must emit nothing, got %d grounding events", emitted)
	}
	if e.Grounding().Len() != 0 {
		t.Fatalf("fabricated observation must never enter the ledger, got %d", e.Grounding().Len())
	}
}

// TestGroundClaimComputeGroundsOffline covers the Filter-side compute wire (N.1): a voiced computable
// claim is grounded/refuted deterministically with no model and no ACT — so the spine catches a
// confident arithmetic hallucination offline. A true claim grounds (KNOW, math doesn't lie), a false
// one refutes, a non-arithmetic thought emits nothing, and a re-voiced claim is reused (one ledger row).
func TestGroundClaimComputeGroundsOffline(t *testing.T) {
	e := newHeuristicEngine(t, "reactive")
	var got []events.Event
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Ground {
			got = append(got, ev)
		}
	})

	e.groundClaim("It comes to me: 12 × 31 = 372.") // true (typographic ×)
	e.groundClaim("clearly 2 + 2 = 5")              // false
	e.groundClaim("the deploy looks risky")         // not computable
	e.groundClaim("12 × 31 = 372")                  // same claim, ASCII — reused, no new row

	if len(got) != 2 {
		t.Fatalf("want 2 grounding events (true + false; non-arith skipped, reuse skipped), got %d", len(got))
	}
	if got[0].Data["verdict"] != "grounded" || got[0].Data["claim"] != "12 * 31 = 372" {
		t.Errorf("first: want grounded 12*31=372, got %v %v", got[0].Data["verdict"], got[0].Data["claim"])
	}
	if got[0].Data["status"] != "KNOW" {
		t.Errorf("a deterministically-proven true claim should read KNOW, got %v", got[0].Data["status"])
	}
	if got[1].Data["verdict"] != "refuted" {
		t.Errorf("2+2=5 should refute, got %v", got[1].Data["verdict"])
	}
}

// TestGroundSensorsEmitsPercept covers the standing re-grounding wire (N.1a-cont): a registered sensor
// whose percept contradicts reality refutes a claim with no ACT and emits grounding.percept.
func TestGroundSensorsEmitsPercept(t *testing.T) {
	e := newHeuristicEngine(t, "continuous")
	percepts := 0
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Percept {
			percepts++
		}
	})
	// a scripted sensor that, at tick 0, reports a failing watched signal (refutes a claim).
	e.AddSensor(grounding.ScriptedSensor{Schedule: map[int][]grounding.Percept{
		0: {{Claim: "the watched test is green", Ok: false, Source: "test-watcher"}},
	}})
	e.bus.Tick = 0
	e.groundSensors()
	if percepts == 0 {
		t.Fatalf("a contradicting sensor percept should emit grounding.percept")
	}
}
