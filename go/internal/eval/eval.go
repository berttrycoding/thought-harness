// Package eval implements the eval OBJECT model from the cognition redesign
// (01-subconscious.md §3.18-§3.21): eval is one object type on the universal
// reference/instance axis (§2.4).
//
//   - the REFERENCE = a MEASURING STICK (a rubric / oracle / test — the
//     registry-able, MINTABLE thing) that scores a subject;
//   - the INSTANCE = a MEASUREMENT (a run + its score — a record).
//
// Two eval MODES sit on top of that one pair (§3.19-§3.20):
//
//   - reference-eval = BENCHMARK (absolute pass/fail — the mint gate,
//     "does this belong in the registry?"); and
//   - instance-eval = MEASURE (comparative vs past similar measurements of the
//     same stick -> a refine signal, not a pass/fail).
//
// Everything here is generic + deterministic: ticks are passed in (no wall
// clock), and a stick's check is an injected func, so the package has no engine
// dependency. It mirrors the registry style of internal/resolve.
package eval

// Score is what a MeasuringStick's check produces for one subject: a binary
// Pass plus a [0,1] Value (the graded score) and a human-readable Reason. Pass
// is the absolute verdict (used by the benchmark mode); Value is the comparable
// magnitude (used by the measure mode).
type Score struct {
	// Pass is the absolute verdict — does the subject clear the stick's bar?
	Pass bool
	// Value is the graded score in [0,1]; the comparable magnitude for measure-mode.
	Value float64
	// Reason is a short human-readable rationale (for the trace / audit).
	Reason string
}

// Check scores one subject against a stick. The subject is opaque (the stick
// knows how to read it) — a rubric clause, an oracle answer, or a test input.
// A Check is pure: it must not touch the wall clock or mutate global state, so a
// measurement is reproducible from (stick, subject).
type Check func(subject any) Score

// MeasuringStick is the eval REFERENCE (§3.18): a named, registry-able,
// mintable check that scores a subject. The Threshold is the absolute bar the
// benchmark mode (§3.19) compares Score.Value against; Pass from the check is
// honoured directly, and Value>=Threshold is the graded gate.
type MeasuringStick struct {
	// Name is the registry key — unique per stick.
	Name string
	// Facet groups sticks the way Category groups tags (e.g. "tool", "skill",
	// "grounding"); lets a registry attach the right stick as a type's mint gate.
	Facet string
	// Threshold is the absolute pass bar on Score.Value in [0,1] for benchmark
	// mode (§3.19). When >0 the graded bar is authoritative: a subject passes the
	// gate iff its Score.Value >= Threshold. When ==0 the check's own Score.Pass
	// is honoured directly ("Pass only", no graded bar).
	Threshold float64
	// Minted is false for a seeded stick, true for one synthesised at runtime and
	// promoted (the reference/instance origin distinction, §2.4 / §3.15).
	Minted bool
	// Check is the rubric/oracle/test. nil is an unusable stick (Verify rejects it).
	Check Check
}

// Measure runs the stick's check on a subject at a given logical tick and
// returns the INSTANCE record (§3.18). The tick is passed in — the package
// never reads the wall clock, so a run is reproducible. A nil Check yields a
// failing Score rather than panicking (the stick should have been Verify'd).
func (s MeasuringStick) Measure(subjectID string, subject any, tick int64) Measurement {
	var sc Score
	if s.Check == nil {
		sc = Score{Pass: false, Value: 0, Reason: "stick has no check"}
	} else {
		sc = s.Check(subject)
	}
	return Measurement{
		Stick:     s.Name,
		SubjectID: subjectID,
		Score:     sc,
		Tick:      tick,
	}
}

// Measurement is the eval INSTANCE (§3.18): a single run of a stick against one
// subject + its score, stamped with the logical tick. It is a record — the
// durable effort goes into the stick (the reference), per §2.6.
type Measurement struct {
	// Stick is the name of the MeasuringStick that produced this measurement.
	Stick string
	// SubjectID identifies the thing measured (so measurements can be grouped per
	// subject for refinement).
	SubjectID string
	// Score is the result the stick's check produced.
	Score Score
	// Tick is the logical time the measurement was taken (passed in, not read).
	Tick int64
}
