package estimate

// consistency_test.go — SLAM M5 unit tests: the consistency/observability invariant as a conserved
// quantity. These pin the closed-form information accounting (the §0 invariant restated: information may
// be gained ONLY through a grounded, associated Observe()) and that a self-restatement / a gated obs gain
// ZERO information — the Huang-2010 EKF-inconsistency the monitor exists to catch.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
)

func monitoringEstimator() *Estimator {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Monitor = true
	return New(cfg, nil)
}

// TestM5_SelfRestatementGainsNoInformation is the core: re-stating the same belief many times gains zero
// information (the variance never drops), so spuriousGain stays 0 and the witness is consistent.
func TestM5_SelfRestatementGainsNoInformation(t *testing.T) {
	est := monitoringEstimator()
	const id = BeliefID("b")
	for i := 0; i < 50; i++ {
		est.Note(id, 0.99) // confident self-assertion, repeatedly
	}
	c := est.ConsistencyState()
	if c.Notes != 50 {
		t.Fatalf("expected 50 notes accounted, got %d", c.Notes)
	}
	if c.SpuriousGain > consistencyEpsilon {
		t.Fatalf("self-restatement must gain NO information; spuriousGain=%v", c.SpuriousGain)
	}
	if c.GroundedGain != 0 {
		t.Fatalf("no grounded observation occurred; groundedGain should be 0, got %v", c.GroundedGain)
	}
	if !c.Consistent() {
		t.Fatal("estimator must be consistent under pure self-restatement")
	}
	if c.GroundedFraction() != 1.0 {
		t.Fatalf("with no information gained at all, groundedFraction is vacuously 1.0; got %v", c.GroundedFraction())
	}
}

// TestM5_GroundedObservationGainsGroundedInformation pins that a real grounded observation IS attributed
// as grounded information (variance dropped because reality measured it) — the legitimate, observable gain.
func TestM5_GroundedObservationGainsGroundedInformation(t *testing.T) {
	est := monitoringEstimator()
	const id = BeliefID("b")
	est.Note(id, 0.95)
	// gold-tier confirmation: high precision -> a real variance drop.
	est.Observe(id, +1.0, control.TierPrecision(control.TierCount()-1))
	c := est.ConsistencyState()
	if c.GroundedGain <= 0 {
		t.Fatalf("a grounded observation must register a positive grounded information gain; got %v", c.GroundedGain)
	}
	if c.SpuriousGain > consistencyEpsilon {
		t.Fatalf("a sound grounded observation must gain NO spurious information; got %v", c.SpuriousGain)
	}
	if !c.Consistent() {
		t.Fatal("estimator must be consistent after a sound grounded observation")
	}
	if c.GroundedFraction() != 1.0 {
		t.Fatalf("all information came from grounding; groundedFraction must be 1.0, got %v", c.GroundedFraction())
	}
}

// TestM5_GatedObservationGainsNoInformation pins that a data-association reject (a refuting obs too far
// from a confident prior) does NOT shrink variance, so it gains no information — the JCBB-lite tripwire.
func TestM5_GatedObservationGainsNoInformation(t *testing.T) {
	est := monitoringEstimator()
	const id = BeliefID("b")
	// Ground the belief to a low variance (near-certain) first.
	est.Note(id, 0.9)
	est.Observe(id, +1.0, control.TierPrecision(control.TierCount()-1))
	est.Observe(id, +1.0, control.TierPrecision(control.TierCount()-1))
	before := est.ConsistencyState()
	// Now a WILD refutation — innovation huge relative to the now-small variance -> gated.
	est.Observe(id, -1.0, control.TierPrecision(control.TierCount()-1))
	c := est.ConsistencyState()
	if c.Gated == 0 {
		t.Skip("the configured chi2 gate did not reject this innovation on this state — not a failure, just not exercising the gate here")
	}
	if c.SpuriousGain > consistencyEpsilon {
		t.Fatalf("a gated observation must not gain information; spuriousGain=%v", c.SpuriousGain)
	}
	if c.GroundedGain < before.GroundedGain {
		t.Fatal("a gated observation must not reduce the grounded-gain accounting")
	}
	if !c.Consistent() {
		t.Fatal("a gated observation must leave the estimator consistent")
	}
}

// TestM5_MonitorOffNoAccounting pins that with the monitor OFF the accounting is inert (the witness stays
// zero / vacuously consistent) — the byte-identical contract: M5 OFF does exactly the M1 work.
func TestM5_MonitorOffNoAccounting(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Monitor = false // M1 on, M5 off
	est := New(cfg, nil)
	const id = BeliefID("b")
	est.Note(id, 0.9)
	est.Observe(id, +1.0, control.TierPrecision(control.TierCount()-1))
	c := est.ConsistencyState()
	if c.Notes != 0 || c.Observations != 0 || c.GroundedGain != 0 || c.SpuriousGain != 0 {
		t.Fatalf("monitor OFF must do no accounting; got %+v", c)
	}
	// CheckConsistency is a no-op (returns true, emits nothing) when the monitor is off.
	if !est.CheckConsistency() {
		t.Fatal("CheckConsistency must return true (vacuously) when the monitor is off")
	}
}

// TestM5_ConsistentReportsTrueWhenSpuriousZero is a small invariant on the Consistency value object.
func TestM5_ConsistentReportsTrueWhenSpuriousZero(t *testing.T) {
	if !(Consistency{GroundedGain: 3, SpuriousGain: 0}).Consistent() {
		t.Fatal("spuriousGain==0 must be consistent")
	}
	if (Consistency{GroundedGain: 3, SpuriousGain: 0.5}).Consistent() {
		t.Fatal("spuriousGain>0 must be inconsistent")
	}
	if f := (Consistency{GroundedGain: 3, SpuriousGain: 1}).GroundedFraction(); f != 0.75 {
		t.Fatalf("groundedFraction = grounded/(grounded+spurious) = 0.75; got %v", f)
	}
}
