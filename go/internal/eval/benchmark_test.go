package eval

import "testing"

// TestBenchmarkGradedBar: with a graded Threshold, the absolute verdict is
// Value >= Threshold — independent of the check's own Pass field.
func TestBenchmarkGradedBar(t *testing.T) {
	s := gradedStick("rubric", 0.6)

	pass, m := Benchmark(s, "above", scoreSubject{v: 0.7}, 1)
	if !pass {
		t.Fatalf("0.7 should clear the 0.6 bar; got fail (%+v)", m.Score)
	}
	fail, _ := Benchmark(s, "below", scoreSubject{v: 0.5}, 2)
	if fail {
		t.Fatalf("0.5 should NOT clear the 0.6 bar")
	}
	// at the bar is a pass (>=).
	atBar, _ := Benchmark(s, "at", scoreSubject{v: 0.6}, 3)
	if !atBar {
		t.Fatalf("0.6 should clear the 0.6 bar (>=)")
	}
}

// passOnlyStick has no graded bar (Threshold 0): the check's Score.Pass decides.
func passOnlyStick(name string) MeasuringStick {
	return MeasuringStick{
		Name:  name,
		Facet: "tool",
		Check: func(subject any) Score {
			ok, _ := subject.(bool)
			return Score{Pass: ok, Value: boolVal(ok), Reason: "pass-only"}
		},
	}
}

func boolVal(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// TestBenchmarkPassOnly: Threshold 0 honours the check's own Pass.
func TestBenchmarkPassOnly(t *testing.T) {
	s := passOnlyStick("oracle")
	if pass, _ := Benchmark(s, "yes", true, 1); !pass {
		t.Fatalf("pass-only stick should admit a true subject")
	}
	if pass, _ := Benchmark(s, "no", false, 2); pass {
		t.Fatalf("pass-only stick should reject a false subject")
	}
}

// TestMintGateAdmitsAndRejects: the mint gate is the benchmark mode wired as an
// admission gate — "does this belong?" A candidate clearing the gate is admitted,
// one below it is not, and the gating measurement is returned for the trace.
func TestMintGateAdmitsAndRejects(t *testing.T) {
	gate := gradedStick("mint-gate", 0.5)

	admit, m := MintGate(gate, "good-candidate", scoreSubject{v: 0.9}, 100)
	if !admit {
		t.Fatalf("a 0.9 candidate should be admitted by the 0.5 gate")
	}
	if m.SubjectID != "good-candidate" || m.Tick != 100 {
		t.Fatalf("gating measurement should record the candidate + tick; got %+v", m)
	}

	reject, _ := MintGate(gate, "weak-candidate", scoreSubject{v: 0.2}, 101)
	if reject {
		t.Fatalf("a 0.2 candidate must be rejected by the 0.5 gate")
	}
}
