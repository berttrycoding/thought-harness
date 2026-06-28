package eval

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/resolve"
)

// TestStickRegistrySeedAndReuse: a seeded stick is found by the resolve spine and
// REUSED (no synthesis).
func TestStickRegistrySeedAndReuse(t *testing.T) {
	r := NewStickRegistry()
	r.Seed(gradedStick("grounding", 0.7))

	s, out, _ := r.MintStick("grounding")
	if out != resolve.Reused {
		t.Fatalf("a seeded stick should be REUSED; got %v", out)
	}
	if s.Minted {
		t.Fatalf("a reused seeded stick must not be marked minted")
	}
}

// TestStickRegistryMintViaSynth: an unknown query synthesises + verifies + stores
// a stick (marked Minted) through the uniform resolve spine.
func TestStickRegistryMintViaSynth(t *testing.T) {
	r := NewStickRegistry()
	r.Synth = func(query string) (MeasuringStick, bool) {
		return MeasuringStick{
			Name:      query,
			Facet:     "skill",
			Threshold: 0.5,
			Check:     func(any) Score { return Score{Pass: true, Value: 1} },
		}, true
	}

	s, out, reason := r.MintStick("novel-stick")
	if out != resolve.Created {
		t.Fatalf("an unknown stick should be CREATED; got %v (%s)", out, reason)
	}
	if !s.Minted {
		t.Fatalf("a synthesised stick must be marked minted")
	}
	// now stored -> a second resolve reuses it.
	_, out2, _ := r.MintStick("novel-stick")
	if out2 != resolve.Reused {
		t.Fatalf("the minted stick should now REUSE; got %v", out2)
	}
}

// TestStickRegistryReuseOnly: with no Synth, an unknown query fails (reuse-only —
// the safe default for a seeded-only stick set).
func TestStickRegistryReuseOnly(t *testing.T) {
	r := NewStickRegistry()
	_, out, _ := r.MintStick("unknown")
	if out != resolve.Failed {
		t.Fatalf("a reuse-only registry must Fail an unknown query; got %v", out)
	}
}

// TestStickRegistryVerifyRejectsBadStick: a synthesised stick with no check (or a
// bad threshold) fails Verify and is NOT stored.
func TestStickRegistryVerifyRejectsBadStick(t *testing.T) {
	r := NewStickRegistry()
	r.Synth = func(query string) (MeasuringStick, bool) {
		return MeasuringStick{Name: query /* no Check */}, true
	}
	_, out, reason := r.MintStick("bad")
	if out != resolve.Failed || reason == "" {
		t.Fatalf("a stick with no check must fail Verify; got %v (%q)", out, reason)
	}
	if r.Len() != 0 {
		t.Fatalf("a failed-verify stick must not be stored; len=%d", r.Len())
	}

	// out-of-range threshold is also rejected.
	r.Synth = func(query string) (MeasuringStick, bool) {
		return MeasuringStick{Name: query, Threshold: 2, Check: func(any) Score { return Score{} }}, true
	}
	if _, out, _ := r.MintStick("bad2"); out != resolve.Failed {
		t.Fatalf("an out-of-range threshold must fail Verify; got %v", out)
	}
}

// TestSelfImprovementLoop: the end-to-end §3.17 shape — instances run, are
// MEASURED comparatively vs past instances (instance-eval, §3.20), and the
// refine signal feeds back. Here a stick's instances trend up; the loop reports
// Up, which a registry would use to keep/raise the reference's standing.
func TestSelfImprovementLoop(t *testing.T) {
	stick := gradedStick("grounding", 0.5)

	var history []Measurement
	values := []float64{0.40, 0.55, 0.70, 0.85} // a reference improving over runs
	dirs := make([]Direction, 0, len(values))
	for i, v := range values {
		m := stick.Measure("ref-under-test", scoreSubject{v: v}, int64(i))
		sig := Measure(m, history, 0.01) // instance-eval vs the accumulated history
		dirs = append(dirs, sig.Direction)
		history = append(history, m)
	}

	// first run has no history (Flat); each later run beats the running mean (Up).
	if dirs[0] != Flat {
		t.Fatalf("first measurement should be Flat; got %v", dirs[0])
	}
	for i := 1; i < len(dirs); i++ {
		if dirs[i] != Up {
			t.Fatalf("an improving reference should measure Up at step %d; got %v", i, dirs[i])
		}
	}
}
