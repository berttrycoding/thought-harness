package eval

import "testing"

// scoreSubject is a tiny test subject: a value the rubric stick reads directly.
type scoreSubject struct{ v float64 }

// gradedStick scores a scoreSubject by its v (a graded [0,1] rubric).
func gradedStick(name string, threshold float64) MeasuringStick {
	return MeasuringStick{
		Name:      name,
		Facet:     "grounding",
		Threshold: threshold,
		Check: func(subject any) Score {
			s, ok := subject.(scoreSubject)
			if !ok {
				return Score{Pass: false, Value: 0, Reason: "wrong subject type"}
			}
			return Score{Pass: s.v >= threshold, Value: s.v, Reason: "graded"}
		},
	}
}

// TestMeasureProducesInstanceRecord: a stick run yields a Measurement carrying the
// stick name, subject id, score, and the PASSED-IN tick (no wall clock).
func TestMeasureProducesInstanceRecord(t *testing.T) {
	s := gradedStick("rubric-A", 0.5)
	m := s.Measure("subj-1", scoreSubject{v: 0.8}, 42)
	if m.Stick != "rubric-A" {
		t.Fatalf("measurement should carry the stick name; got %q", m.Stick)
	}
	if m.SubjectID != "subj-1" {
		t.Fatalf("measurement should carry the subject id; got %q", m.SubjectID)
	}
	if m.Tick != 42 {
		t.Fatalf("measurement tick must be the passed-in tick; got %d", m.Tick)
	}
	if m.Score.Value != 0.8 || !m.Score.Pass {
		t.Fatalf("score mismatch; got %+v", m.Score)
	}
}

// TestMeasureIsDeterministic: the same (stick, subject, tick) yields an identical
// measurement every time — reproducibility (no wall clock, pure check).
func TestMeasureIsDeterministic(t *testing.T) {
	s := gradedStick("rubric-A", 0.5)
	a := s.Measure("subj", scoreSubject{v: 0.7}, 10)
	b := s.Measure("subj", scoreSubject{v: 0.7}, 10)
	if a != b {
		t.Fatalf("measurement must be deterministic; got %+v vs %+v", a, b)
	}
}

// TestNilCheckFailsCleanly: a stick with no check yields a failing score, never a
// panic (the stick should have been Verify'd, but Measure stays safe).
func TestNilCheckFailsCleanly(t *testing.T) {
	s := MeasuringStick{Name: "broken"}
	m := s.Measure("subj", nil, 1)
	if m.Score.Pass || m.Score.Value != 0 {
		t.Fatalf("nil-check stick must fail cleanly; got %+v", m.Score)
	}
}
