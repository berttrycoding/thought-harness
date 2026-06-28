package funnel

import (
	"errors"
	"math"
	"testing"
)

// --- ArmStats metric math -------------------------------------------------

func TestArmStatsMetrics(t *testing.T) {
	a := ArmStats{
		PerItem:           []bool{true, true, false, true},
		CompletionPerItem: []int{100, 200, 50, 100}, // total 450; solved tasks cost 100+200+100=400
	}
	if a.Total() != 4 {
		t.Fatalf("Total = %d, want 4", a.Total())
	}
	if a.Solved() != 3 {
		t.Fatalf("Solved = %d, want 3", a.Solved())
	}
	if a.PassRate() != 0.75 {
		t.Fatalf("PassRate = %v, want 0.75", a.PassRate())
	}
	if a.CompletionTokens() != 450 {
		t.Fatalf("CompletionTokens = %d, want 450", a.CompletionTokens())
	}
	// CompletionPerSolved is total completion / solved = 450/3 = 150 (the cache-immune efficiency metric;
	// the failed task's completion is still spend, so it is in the numerator).
	if a.CompletionPerSolved() != 150 {
		t.Fatalf("CompletionPerSolved = %v, want 150", a.CompletionPerSolved())
	}
	// a zero-solved arm is +Inf per-solved (never reads as cheaper).
	zero := ArmStats{PerItem: []bool{false, false}, CompletionPerItem: []int{10, 20}}
	if !math.IsInf(zero.CompletionPerSolved(), 1) {
		t.Fatalf("zero-solved CompletionPerSolved = %v, want +Inf", zero.CompletionPerSolved())
	}
}

// --- EvaluateLift gate ladder (the testable core) -------------------------

// the gate ladder: each case is a distinct branch of EvaluateLift, mutation-sensitive by construction.
func TestEvaluateLiftGateLadder(t *testing.T) {
	cfg := Tier2Config{Alpha: 0.05, MinEfficiency: 5.0}

	// helper: a paired suite with explicit per-item pass + completion spend.
	arm := func(pass []bool, comp []int) ArmStats {
		return ArmStats{PerItem: pass, CompletionPerItem: comp}
	}

	t.Run("tier1-regression is a hard revert regardless of the numbers", func(t *testing.T) {
		// the batch is BOTH smarter and cheaper, but Tier-1 regressed -> REVERT (the hard gate).
		base := arm([]bool{false, false, false, false, false, false}, []int{100, 100, 100, 100, 100, 100})
		batch := arm([]bool{true, true, true, true, true, true}, []int{10, 10, 10, 10, 10, 10})
		v := EvaluateLift(base, batch, false /* tier1 FAILED */, cfg)
		if v.Decision != LiftRevert {
			t.Fatalf("tier1-fail must REVERT, got %s (%s)", v.Decision, v.Reason)
		}
		if !v.Tier1Regressed {
			t.Fatalf("Tier1Regressed flag not set")
		}
	})

	t.Run("significant capability lift -> KEEP (smarter)", func(t *testing.T) {
		// many tasks flip fail->pass, none pass->fail: a clear significant lift.
		base := arm(repeatBool(false, 12), repeatInt(100, 12))
		batch := arm(repeatBool(true, 12), repeatInt(100, 12))
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftKeep {
			t.Fatalf("significant lift must KEEP, got %s (%s, p=%.4f)", v.Decision, v.Reason, v.McNemarP)
		}
		if v.Fixed != 12 || v.Broke != 0 {
			t.Fatalf("expected fixed=12 broke=0, got fixed=%d broke=%d", v.Fixed, v.Broke)
		}
	})

	t.Run("significant capability regression -> REVERT (breaks more than fixes)", func(t *testing.T) {
		base := arm(repeatBool(true, 12), repeatInt(100, 12))
		batch := arm(repeatBool(false, 12), repeatInt(10, 12)) // cheaper, but breaks everything
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftRevert {
			t.Fatalf("significant regression must REVERT, got %s (%s)", v.Decision, v.Reason)
		}
		if v.Broke <= v.Fixed {
			t.Fatalf("expected broke>fixed, got broke=%d fixed=%d", v.Broke, v.Fixed)
		}
	})

	t.Run("flat capability + cheaper beyond floor -> KEEP (the saturated lever)", func(t *testing.T) {
		// SAME pass pattern both arms (flat capability, no discordant pairs), batch spends far fewer
		// completion tokens per solved -> KEEP on the cache-immune efficiency axis.
		pass := []bool{true, true, true, true, true, true}
		base := arm(pass, repeatInt(100, 6)) // 100/solved
		batch := arm(pass, repeatInt(40, 6)) // 40/solved -> delta = 60 >> MinEfficiency 5
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftKeep {
			t.Fatalf("flat+cheaper must KEEP, got %s (%s)", v.Decision, v.Reason)
		}
		if v.CapabilityLift != 0 {
			t.Fatalf("expected flat capability (lift 0), got %v", v.CapabilityLift)
		}
		if v.CompletionDelta < cfg.MinEfficiency {
			t.Fatalf("expected completion-delta >= MinEfficiency, got %v", v.CompletionDelta)
		}
	})

	t.Run("flat capability + costlier beyond floor -> REVERT", func(t *testing.T) {
		pass := []bool{true, true, true, true, true, true}
		base := arm(pass, repeatInt(40, 6))
		batch := arm(pass, repeatInt(100, 6)) // costs MORE per solved, no capability gain
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftRevert {
			t.Fatalf("flat+costlier must REVERT, got %s (%s)", v.Decision, v.Reason)
		}
	})

	t.Run("flat capability + cost change BELOW the floor -> REVERT (filler)", func(t *testing.T) {
		// THE MUTATION-SENSITIVE BAR: a completion saving SMALLER than MinEfficiency must NOT be kept.
		pass := []bool{true, true, true, true, true, true}
		base := arm(pass, repeatInt(100, 6)) // 100/solved
		batch := arm(pass, repeatInt(98, 6)) // 98/solved -> delta 2 < MinEfficiency 5 -> filler
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftRevert {
			t.Fatalf("a sub-floor saving must REVERT (filler), got %s (%s, delta=%v)", v.Decision, v.Reason, v.CompletionDelta)
		}
		// and the SAME numbers with a LOWER floor must flip to KEEP — proves the floor is load-bearing.
		loose := Tier2Config{Alpha: 0.05, MinEfficiency: 1.0}
		if v2 := EvaluateLift(base, batch, true, loose); v2.Decision != LiftKeep {
			t.Fatalf("with a 1.0 floor the 2-token saving must KEEP, got %s (%s)", v2.Decision, v2.Reason)
		}
	})

	t.Run("non-significant positive lean at flat cost -> MARGIN (human decides)", func(t *testing.T) {
		// one fix, no break -> a positive lean but NOT significant (1 discordant pair, p~1), cost flat.
		base := arm([]bool{false, true, true, true, true, true}, repeatInt(100, 6))
		batch := arm([]bool{true, true, true, true, true, true}, repeatInt(100, 6))
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftMargin {
			t.Fatalf("non-significant positive lean at flat cost must be MARGIN, got %s (%s, p=%.3f)", v.Decision, v.Reason, v.McNemarP)
		}
	})

	t.Run("zero gain, zero cost change -> REVERT (filler)", func(t *testing.T) {
		pass := []bool{true, true, false, true, false, true}
		base := arm(pass, repeatInt(100, 6))
		batch := arm(pass, repeatInt(100, 6)) // identical -> no smarter, no cheaper
		v := EvaluateLift(base, batch, true, cfg)
		if v.Decision != LiftRevert {
			t.Fatalf("no gain no cost change must REVERT (filler), got %s (%s)", v.Decision, v.Reason)
		}
	})
}

func TestEvaluateLiftUnpairedArmsRevert(t *testing.T) {
	base := ArmStats{PerItem: []bool{true, false}, CompletionPerItem: []int{10, 10}}
	mismatched := ArmStats{PerItem: []bool{true}, CompletionPerItem: []int{10}}
	v := EvaluateLift(base, mismatched, true, DefaultTier2())
	if v.Decision != LiftRevert {
		t.Fatalf("unpaired arms must REVERT (never a silent keep), got %s (%s)", v.Decision, v.Reason)
	}
	empty := EvaluateLift(ArmStats{}, ArmStats{}, true, DefaultTier2())
	if empty.Decision != LiftRevert {
		t.Fatalf("empty suite must REVERT, got %s", empty.Decision)
	}
}

// the metric is COMPLETION tokens, not TOTAL — a batch that cuts cached INPUT but not completion must NOT
// read as cheaper (the saturated-frontier cache-immunity lesson, research §"Code gaps" #3). ArmStats
// carries ONLY completion per item, so a total-only saving is structurally invisible to the keep rule.
func TestEvaluateLiftIgnoresInputOnlySaving(t *testing.T) {
	pass := []bool{true, true, true, true, true, true}
	// both arms identical COMPLETION spend (the only thing ArmStats records) -> no efficiency signal,
	// even if the real run saved enormous cached-input tokens. Capability flat -> filler REVERT.
	base := ArmStats{PerItem: pass, CompletionPerItem: repeatInt(100, 6)}
	batch := ArmStats{PerItem: pass, CompletionPerItem: repeatInt(100, 6)}
	v := EvaluateLift(base, batch, true, Tier2Config{Alpha: 0.05, MinEfficiency: 5})
	if v.Decision != LiftRevert {
		t.Fatalf("a completion-flat batch must REVERT (input-only savings are invisible by design), got %s", v.Decision)
	}
	if v.CompletionDelta != 0 {
		t.Fatalf("expected zero completion-delta, got %v", v.CompletionDelta)
	}
}

// --- Tier2Runner with an injected FAKE bench (offline, deterministic) ------

// fakeBench is a deterministic LiftBench: it returns a fixed ArmStats per stateDir. The "" arm is the
// baseline; any non-empty dir is the with-batch arm. No model, no tokens, no engine — it isolates the
// runner + keep-rule logic.
type fakeBench struct {
	baseline  ArmStats
	withBatch ArmStats
	errOn     string // a stateDir that errors (to test the abort path); "" = never error
}

func (f fakeBench) BenchArm(stateDir string) (ArmStats, error) {
	if f.errOn != "" && stateDir == f.errOn {
		return ArmStats{}, errors.New("fake bench budget overrun")
	}
	if stateDir == "" {
		return f.baseline, nil
	}
	return f.withBatch, nil
}

func TestTier2RunnerKeepsCheaperBatch(t *testing.T) {
	pass := []bool{true, true, true, true, true, true}
	bench := fakeBench{
		baseline:  ArmStats{PerItem: pass, CompletionPerItem: repeatInt(100, 6)},
		withBatch: ArmStats{PerItem: pass, CompletionPerItem: repeatInt(30, 6)}, // far cheaper, flat capability
	}
	r := NewTier2Runner(bench)
	res, err := r.RunTier2("batch-dir", true)
	if err != nil {
		t.Fatalf("RunTier2: %v", err)
	}
	if res.Verdict.Decision != LiftKeep {
		t.Fatalf("cheaper-at-flat-capability batch must KEEP, got %s (%s)", res.Verdict.Decision, res.Verdict.Reason)
	}
	// the raw arms are echoed for the ledger.
	if res.Baseline.Solved() != 6 || res.WithBatch.Solved() != 6 {
		t.Fatalf("arm evidence not echoed: base=%d batch=%d solved", res.Baseline.Solved(), res.WithBatch.Solved())
	}
}

func TestTier2RunnerRevertsFillerBatch(t *testing.T) {
	pass := []bool{true, true, true, true, true, true}
	bench := fakeBench{
		baseline:  ArmStats{PerItem: pass, CompletionPerItem: repeatInt(100, 6)},
		withBatch: ArmStats{PerItem: pass, CompletionPerItem: repeatInt(100, 6)}, // identical -> filler
	}
	res, err := NewTier2Runner(bench).RunTier2("batch-dir", true)
	if err != nil {
		t.Fatalf("RunTier2: %v", err)
	}
	if res.Verdict.Decision != LiftRevert {
		t.Fatalf("filler batch (no gain, no saving) must REVERT, got %s (%s)", res.Verdict.Decision, res.Verdict.Reason)
	}
}

func TestTier2RunnerBenchErrorAborts(t *testing.T) {
	bench := fakeBench{errOn: "batch-dir"} // the with-batch arm errors
	_, err := NewTier2Runner(bench).RunTier2("batch-dir", true)
	if err == nil {
		t.Fatalf("a bench error must ABORT the batch (returned), not a silent keep")
	}
}

func TestTier2RunnerNoBenchErrors(t *testing.T) {
	r := &Tier2Runner{} // no bench injected
	_, err := r.RunTier2("dir", true)
	if err == nil {
		t.Fatalf("a runner with no LiftBench must error, not nil-panic")
	}
}

// --- tiny test helpers -----------------------------------------------------

func repeatBool(v bool, n int) []bool {
	out := make([]bool, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func repeatInt(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}
