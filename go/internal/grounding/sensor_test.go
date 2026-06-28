package grounding

import "testing"

// TestWatchedTestFailingRefutesWithNoAct is the N.1a-cont gate: a continuous sensor (a watched test)
// refutes a standing claim the moment it fails — WITHOUT the system ACTing. This is awake-mode standing
// re-grounding: reality contradicts the claim between acts and the loop catches it immediately.
func TestWatchedTestFailingRefutesWithNoAct(t *testing.T) {
	m := NewExperimentMemory()
	claim := "the suite is green"

	// the system believes the suite is green (no ACT performed).
	m.RecordSource(claim, Grounded, TierTestimony, "it was green last we looked", 1)

	// a build/test WATCHER runs continuously; at tick 5 it observes the suite go RED, unsolicited.
	sensor := ScriptedSensor{Schedule: map[int][]Percept{
		5: {{Claim: claim, Ok: false, Source: "test-watcher"}},
	}}

	// simulate the awake loop re-grounding every tick — no ACT anywhere.
	var refutedAt int
	for tick := 1; tick <= 10; tick++ {
		m.IngestSensor(sensor, tick)
		if e, ok := m.Recall(claim); ok && e.Verdict == Refuted && refutedAt == 0 {
			refutedAt = tick
		}
	}

	if refutedAt != 5 {
		t.Fatalf("the watched test failing should refute the claim at tick 5 with no ACT; refuted at %d", refutedAt)
	}
	if e, _ := m.Recall(claim); e.Verdict != Refuted || e.Method != "observation" {
		t.Fatalf("the claim should be refuted by the firsthand percept; got %v from %q", e.Verdict, e.Method)
	}
}

// TestSensorHealthyPerceptGroundsContinuously: a healthy watcher continuously confirms a claim (grounds
// it true) — continuous feedback in both directions.
func TestSensorHealthyPerceptGroundsContinuously(t *testing.T) {
	m := NewExperimentMemory()
	claim := "the service is responding"
	sensor := ScriptedSensor{Schedule: map[int][]Percept{
		3: {{Claim: claim, Ok: true, Source: "health-monitor"}},
	}}
	for tick := 1; tick <= 5; tick++ {
		m.IngestSensor(sensor, tick)
	}
	if e, ok := m.Recall(claim); !ok || e.Verdict != Grounded {
		t.Fatalf("a healthy percept should ground the claim true; got %v ok=%v", e.Verdict, ok)
	}
}

// TestFabricatedPerceptNeverGrounds: a percept marked Fabricated is NOT grounded — the grounding spine
// refuses to launder synthetic "reality" into the ledger, even from a sensor source. Guards the
// never-fabricate invariant at the continuous-perception boundary.
func TestFabricatedPerceptNeverGrounds(t *testing.T) {
	m := NewExperimentMemory()
	claim := "the deploy succeeded"
	sensor := ScriptedSensor{Schedule: map[int][]Percept{
		2: {{Claim: claim, Ok: true, Source: "synthetic-sensor", Fabricated: true}},
	}}
	grounded := 0
	for tick := 1; tick <= 5; tick++ {
		grounded += m.IngestSensor(sensor, tick)
	}
	if grounded != 0 {
		t.Fatalf("a fabricated percept must never ground; grounded count = %d", grounded)
	}
	if _, ok := m.Recall(claim); ok {
		t.Fatalf("a fabricated percept must not enter the ledger at all")
	}
}
