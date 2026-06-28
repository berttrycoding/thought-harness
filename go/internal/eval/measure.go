package eval

// Instance-eval = MEASURE (§3.20): the COMPARATIVE mode. At discard time an
// instance is measured comparatively vs PAST SIMILAR measurements of the same
// stick -> a refine signal that feeds reference refinement (§2.6, §3.17). This
// is measuring, NOT benchmarking: there is no absolute bar, only "is this run
// better or worse than what this stick has seen before?"

// Direction is the comparative verdict of a measure against its history.
type Direction int

const (
	// Flat: no usable history (first measurement) or the score equals the baseline.
	Flat Direction = iota
	// Up: the new score is above the baseline of past similar measurements (a lift).
	Up
	// Down: the new score is below the baseline (a regression — a demote signal).
	Down
)

func (d Direction) String() string {
	switch d {
	case Up:
		return "up"
	case Down:
		return "down"
	default:
		return "flat"
	}
}

// RefineSignal is the output of instance-eval (§3.20): the comparative verdict
// for one new measurement against the baseline of past ones, the magnitude of
// the gap (Delta = new - baseline), and the baseline it was compared to. It is
// the signal a registry's self-improvement loop (§3.17) consumes to refine /
// keep / demote a reference — NOT an absolute pass/fail.
type RefineSignal struct {
	// Direction is up / down / flat relative to the baseline.
	Direction Direction
	// Delta is new.Value - baseline (positive = improvement).
	Delta float64
	// Baseline is the mean Score.Value of the comparison history (0 when empty).
	Baseline float64
	// N is the number of past measurements the baseline was computed over.
	N int
}

// Measure runs the comparative instance-eval (§3.20): score the new measurement
// against the baseline (mean Value) of PAST measurements of the SAME stick. A
// caller passes the relevant history (e.g. past measurements of the same
// reference, grouped by stick) — the "similar" grouping is the caller's policy;
// this function does the comparison. With no history the direction is Flat
// (nothing to refine against yet).
//
// epsilon is the dead-band: a |Delta| below it counts as Flat (no signal),
// keeping the refine loop from reacting to measurement noise. Pass 0 for a
// strict comparison.
func Measure(newM Measurement, history []Measurement, epsilon float64) RefineSignal {
	if len(history) == 0 {
		return RefineSignal{Direction: Flat, Delta: 0, Baseline: 0, N: 0}
	}
	var sum float64
	n := 0
	for _, h := range history {
		// Only compare against measurements from the same stick (same yardstick).
		if h.Stick != newM.Stick {
			continue
		}
		sum += h.Score.Value
		n++
	}
	if n == 0 {
		return RefineSignal{Direction: Flat, Delta: 0, Baseline: 0, N: 0}
	}
	baseline := sum / float64(n)
	delta := newM.Score.Value - baseline
	dir := Flat
	if delta > epsilon {
		dir = Up
	} else if delta < -epsilon {
		dir = Down
	}
	return RefineSignal{Direction: dir, Delta: delta, Baseline: baseline, N: n}
}
