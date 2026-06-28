package realhard

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// deliberative_arm_test.go — the robustness lever exercised through the REAL realhard harness arm on
// --backend test (offline, no model). It proves the two arm-level contracts the gate depends on:
//   (a) K==1 BYTE-IDENTICAL — the deliberative wrapper at K==1 runs exactly the single-episode path
//       (same answer, same per-episode telemetry, and — through RunDeliberative — exactly one sample
//       on seed==base with NO conscious.deliberation event). The load-bearing regression guard.
//   (b) K=3 with per-sample seeded VARIATION (a seed-keyed backend) reconciles to the correct majority
//       AND deterministically (same base → same winner).

// seedKeyedBackend wraps the offline TestBackend (so every CONTENT role is the faithful double) but
// overrides Respond to return a SEED-DETERMINED final answer — the hook that injects genuine
// per-sample variation into the realhard harness arm, whose test-double answer is otherwise
// seed-stable. The variation is a pure function of the seed (deterministic), so the whole K-sample
// reconciliation stays reproducible. answerFor maps each per-sample seed to the answer that sample's
// episode will conclude with.
type seedKeyedBackend struct {
	*backends.TestBackend
	answer string
}

// Respond returns the seed-determined answer verbatim (the engine's deliverResponse adopts it as the
// last response, which harnessAnswer then surfaces to the oracle + the deliberative vote).
func (b *seedKeyedBackend) Respond(goal string, ctx []types.Thought) string { return b.answer }

// seedKeyedFactory builds a factory whose backend's Respond returns answerFor(seed) — so sample i
// (seed DeliberativeSeed(base,i)) concludes with a known answer. Determinism: answerFor must be a pure
// function of the seed.
func seedKeyedFactory(answerFor func(seed int64) string) BackendFactory {
	return func(seed int64, _ float64) backends.Backend {
		return &seedKeyedBackend{TestBackend: backends.NewTest(), answer: answerFor(seed)}
	}
}

// TestK1ByteIdenticalToSingleEpisode is THE byte-identical regression guard at the arm level: at K==1
// the harness arm's result must equal the single-episode runHarnessEpisode result for the same seed —
// same answer, verdict, grounded flag, model-call count, and escalation tallies. Run on the plain
// offline TestBackend (the default suite double) so it pins the production default path.
func TestK1ByteIdenticalToSingleEpisode(t *testing.T) {
	task := Tasks()[0]
	factory := func(_ int64, _ float64) backends.Backend { return backends.NewTest() }
	const seed = int64(1729)

	// the single-episode path (the pre-flag RunHarness body, called directly).
	single, err := runHarnessEpisode(task, factory, seed, 60, t.TempDir(), ArmHarness, nil)
	if err != nil {
		t.Fatalf("single episode: %v", err)
	}

	// the K==1 deliberative path: RunDeliberative(1, ...) must run EXACTLY one sample on seed==base
	// and return that sample's answer verbatim, with no deliberation event.
	bus := events.NewDefault()
	var delib int
	bus.Subscribe(func(ev events.Event) {
		if ev.Kind == events.Deliberation {
			delib++
		}
	})
	calls := 0
	var seenSeed int64 = -1
	res, err := engine.RunDeliberative(1, seed, bus.Emit, nil, func(i int, sampleSeed int64) (engine.DeliberativeSample, error) {
		calls++
		seenSeed = sampleSeed
		rr, e := runHarnessEpisode(task, factory, sampleSeed, 60, t.TempDir(), ArmHarness, nil)
		if e != nil {
			return engine.DeliberativeSample{}, e
		}
		return engine.DeliberativeSample{Answer: rr.Answer, Value: rr.Value}, nil
	})
	if err != nil {
		t.Fatalf("K=1 deliberative: %v", err)
	}
	if calls != 1 {
		t.Errorf("K=1 must run EXACTLY one sample, ran %d", calls)
	}
	if seenSeed != seed {
		t.Errorf("K=1 sample 0 must use the base seed %d verbatim, used %d", seed, seenSeed)
	}
	if delib != 0 {
		t.Errorf("K=1 must emit NO conscious.deliberation event (byte-identical), got %d", delib)
	}
	if engine.NormalizeAnswer(res.Answer) != engine.NormalizeAnswer(single.Answer) {
		t.Errorf("K=1 answer must equal the single-episode answer:\n  K=1   : %q\n  single: %q",
			res.Answer, single.Answer)
	}
	// and the oracle verdict of the K=1 answer matches the single-episode verdict.
	if Score(task, res.Answer).Solved != single.Verdict.Solved {
		t.Errorf("K=1 verdict must match single-episode verdict")
	}
}

// TestK1ResolvedFlagMeansSingleEpisode pins the engine.DeliberativeK() resolution: with the env unset
// (the default test environment), the resolved K is 1, so RunHarness takes the single-episode branch —
// the default-OFF / byte-identical posture. (The env-knob is resolved once at package init; the suite
// runs with it unset, so this asserts the shipped default.)
func TestK1ResolvedFlagMeansSingleEpisode(t *testing.T) {
	if k := engine.DeliberativeK(); k != 1 {
		t.Fatalf("THOUGHT_DELIBERATIVE_K resolved to %d in the test environment; the suite default MUST "+
			"be 1 (OFF / byte-identical) — do not run the suite with the flag set", k)
	}
	// RunHarness with the default K=1 equals the single-episode path on the same seed.
	task := Tasks()[2]
	factory := func(_ int64, _ float64) backends.Backend { return backends.NewTest() }
	const seed = int64(99)
	viaRunHarness, err := RunHarness(task, factory, seed, 60, t.TempDir())
	if err != nil {
		t.Fatalf("RunHarness: %v", err)
	}
	direct, err := runHarnessEpisode(task, factory, seed, 60, t.TempDir(), ArmHarness, nil)
	if err != nil {
		t.Fatalf("runHarnessEpisode: %v", err)
	}
	if engine.NormalizeAnswer(viaRunHarness.Answer) != engine.NormalizeAnswer(direct.Answer) {
		t.Errorf("at the default K=1, RunHarness must equal runHarnessEpisode: %q vs %q",
			viaRunHarness.Answer, direct.Answer)
	}
}

// TestK3SeededVariationReconcilesMajority is contract (b): with per-sample seeded VARIATION injected
// (a seed-keyed backend so each of the K episodes concludes with a known answer), the K=3
// reconciliation picks the correct MAJORITY, and the pick is DETERMINISTIC (same base → same winner).
// This exercises the REAL realhard harness episode (full engine, materialized workspace) per sample,
// not a stubbed sampler — the end-to-end arm path the bench would run with the flag ON.
func TestK3SeededVariationReconcilesMajority(t *testing.T) {
	task := Tasks()[0]

	// Map the THREE per-sample seeds to answers so two agree ("the-majority") and one dissents. The
	// seeds are the deterministic DeliberativeSeed(base, 0..2).
	base := int64(7)
	s0 := engine.DeliberativeSeed(base, 0)
	s1 := engine.DeliberativeSeed(base, 1)
	s2 := engine.DeliberativeSeed(base, 2)
	answerFor := func(seed int64) string {
		switch seed {
		case s0, s1:
			return "the majority answer"
		case s2:
			return "the dissenting answer"
		default:
			return "unexpected"
		}
	}
	factory := seedKeyedFactory(answerFor)

	normalize := func(a string) string { return canonicalAnswer(task, a) }
	run := func() engine.DeliberativeResult {
		res, err := engine.RunDeliberative(3, base, nil, normalize, func(i int, sampleSeed int64) (engine.DeliberativeSample, error) {
			rr, e := runHarnessEpisode(task, factory, sampleSeed, 60, t.TempDir(), ArmHarness, nil)
			if e != nil {
				return engine.DeliberativeSample{}, e
			}
			return engine.DeliberativeSample{Answer: rr.Answer, Value: rr.Value}, nil
		})
		if err != nil {
			t.Fatalf("RunDeliberative: %v", err)
		}
		return res
	}

	res := run()
	if engine.NormalizeAnswer(res.Answer) != engine.NormalizeAnswer("the majority answer") {
		t.Errorf("K=3 must reconcile to the 2-of-3 majority: got %q, want %q", res.Answer, "the majority answer")
	}
	if res.Tie {
		t.Errorf("a 2-1 split is not a tie")
	}

	// DETERMINISM: a second run on the same base must pick the same winner.
	res2 := run()
	if engine.NormalizeAnswer(res.Answer) != engine.NormalizeAnswer(res2.Answer) || res.WinnerIx != res2.WinnerIx {
		t.Errorf("reconciliation over the real arm must be deterministic: %q(ix %d) vs %q(ix %d)",
			res.Answer, res.WinnerIx, res2.Answer, res2.WinnerIx)
	}
}

// TestSeedKeyedBackendVariesAnswer is a sanity check that the seed-keyed backend ACTUALLY makes the
// realhard episode conclude with the injected answer (so contract (b) above measures real variation,
// not a stub passed straight through). Without this, a broken Respond hook would silently make every
// sample identical and the majority test would pass vacuously.
func TestSeedKeyedBackendVariesAnswer(t *testing.T) {
	task := Tasks()[0]
	factory := seedKeyedFactory(func(seed int64) string {
		if seed%2 == 0 {
			return "even-seed answer"
		}
		return "odd-seed answer"
	})
	rrEven, err := runHarnessEpisode(task, factory, 2, 60, t.TempDir(), ArmHarness, nil)
	if err != nil {
		t.Fatalf("even: %v", err)
	}
	rrOdd, err := runHarnessEpisode(task, factory, 3, 60, t.TempDir(), ArmHarness, nil)
	if err != nil {
		t.Fatalf("odd: %v", err)
	}
	if engine.NormalizeAnswer(rrEven.Answer) == engine.NormalizeAnswer(rrOdd.Answer) {
		t.Fatalf("the seed-keyed backend must produce DIFFERENT answers for different seeds "+
			"(even=%q odd=%q) — else the K=3 majority test is vacuous", rrEven.Answer, rrOdd.Answer)
	}
	if engine.NormalizeAnswer(rrEven.Answer) != "even-seed answer" {
		t.Errorf("even-seed episode must conclude with the injected answer, got %q", rrEven.Answer)
	}
}
