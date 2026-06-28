package eval

import "testing"

func meas(stick string, v float64, tick int64) Measurement {
	return Measurement{Stick: stick, SubjectID: "subj", Score: Score{Value: v}, Tick: tick}
}

// TestMeasureFlatOnEmptyHistory: with no past measurements, the comparative mode
// has nothing to refine against -> Flat, zero baseline.
func TestMeasureFlatOnEmptyHistory(t *testing.T) {
	sig := Measure(meas("rubric", 0.8, 5), nil, 0)
	if sig.Direction != Flat || sig.N != 0 {
		t.Fatalf("empty history must be Flat with N=0; got %+v", sig)
	}
}

// TestMeasureUpDown: a new score above the baseline of past measurements is Up
// (a lift signal); below is Down (a regression / demote signal). The baseline is
// the mean of the past values, and Delta = new - baseline.
func TestMeasureUpDown(t *testing.T) {
	hist := []Measurement{
		meas("rubric", 0.4, 1),
		meas("rubric", 0.6, 2), // baseline = 0.5
	}

	up := Measure(meas("rubric", 0.9, 3), hist, 0)
	if up.Direction != Up {
		t.Fatalf("0.9 vs 0.5 baseline should be Up; got %+v", up)
	}
	if up.Baseline != 0.5 || up.N != 2 {
		t.Fatalf("baseline should be the mean 0.5 over N=2; got %+v", up)
	}
	if d := up.Delta - 0.4; d > 1e-9 || d < -1e-9 {
		t.Fatalf("delta should be 0.4; got %v", up.Delta)
	}

	down := Measure(meas("rubric", 0.1, 4), hist, 0)
	if down.Direction != Down {
		t.Fatalf("0.1 vs 0.5 baseline should be Down; got %+v", down)
	}
}

// TestMeasureDeadBand: a |Delta| inside epsilon is Flat (no signal) — keeps the
// refine loop from reacting to measurement noise.
func TestMeasureDeadBand(t *testing.T) {
	hist := []Measurement{meas("rubric", 0.50, 1)}
	// new value 0.53, baseline 0.50, delta 0.03 < epsilon 0.05 => Flat.
	sig := Measure(meas("rubric", 0.53, 2), hist, 0.05)
	if sig.Direction != Flat {
		t.Fatalf("a delta inside the dead-band should be Flat; got %+v", sig)
	}
	// the same delta with a strict (0) epsilon is Up.
	strict := Measure(meas("rubric", 0.53, 2), hist, 0)
	if strict.Direction != Up {
		t.Fatalf("with epsilon 0 the same delta should be Up; got %+v", strict)
	}
}

// TestMeasureOnlyComparesSameStick: history from a DIFFERENT stick is ignored —
// you only measure against the same yardstick.
func TestMeasureOnlyComparesSameStick(t *testing.T) {
	hist := []Measurement{
		meas("rubric-A", 0.9, 1), // same stick
		meas("rubric-B", 0.1, 2), // different stick — must be excluded
	}
	sig := Measure(meas("rubric-A", 0.5, 3), hist, 0)
	if sig.N != 1 || sig.Baseline != 0.9 {
		t.Fatalf("only same-stick history should count; baseline 0.9 N=1; got %+v", sig)
	}
	if sig.Direction != Down {
		t.Fatalf("0.5 vs the 0.9 same-stick baseline should be Down; got %+v", sig)
	}
}
