package stability

// B4 COMBINED self-modification AXIS: the durability-vs-self-modification curve with the FULL B4 winning
// config (soft ON at every level, + the full awake stack at L4). The standing `--axis` (MeasureSelfModCurve)
// turns soft OFF; this re-walks the same 5-rung ladder with soft forced ON so the axis answers the goal's
// requirement — 5/5 self-mod levels stay durable with EVERYTHING on, not just the awake stack. Offline
// TestBackend double, seed=7 (deterministic). The standing curve is untouched; this is additive.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
)

// selfModFeaturesSoft is selfModFeatures + soft ON at every level (the B4 combined plant). L0-L3 are
// reactive (soft is a reactive lifter); L4 is the full awake stack + soft.
func selfModFeaturesSoft(l SelfModLevel) *config.HarnessConfig {
	feat := config.New() // AllOn
	feat.Conscious.Activity.Soft = true
	if l == L4Awake {
		feat.Conscious.Activity.Forest = true
		feat.Conscious.Activity.DriveAgenda = true
		feat.Conscious.Activity.SeedIntents = true
		feat.Conscious.Activity.SeedIntentCount = cognition.SeedPortfolioSize()
	}
	feat.Validate()
	return feat
}

// measureSelfModLevelSoft mirrors MeasureSelfModLevel but runs on the soft-ON combined config.
func measureSelfModLevelSoft(l SelfModLevel) SelfModResult {
	mode := "reactive"
	if l == L4Awake {
		mode = "continuous"
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = mode
	cfg.Seed = 7
	cfg.Features = selfModFeaturesSoft(l)

	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("measureSelfModLevelSoft: NewEngine on the test double failed: " + err.Error())
	}
	res := SelfModResult{Level: l, MinMu: 1.0}
	runSelfMod(e, l, &res)
	res.Report = CheckEngine(e, l.String()+" [soft ON]", mode)
	res.Regime = regimeOf(res.Report.Regime)
	res.GainMeasured = res.Report.GainMeasured
	if res.Report.Metrics != nil {
		res.MaxFanout = res.Report.Metrics.MaxFanout
	}
	res.MintedOps = len(e.Catalog().Minted())
	res.MintedSpec = len(e.Convert().Minted)
	res.MintedSkill = len(e.Convert().MintedSkill)
	return res
}

// TestB4CombinedAxisDurable is the B4 combined-AXIS guard: every self-modification level (L0..L4) holds
// the durable regime with the FULL winning config (soft ON; the awake stack at L4). This is the goal's
// "5/5 self-mod levels stay durable, saturated-bounded, with everything ON". RUN WITH THE FLAG ON:
//
//	THOUGHT_WAKE_TRANSCRIPT=on go test ./internal/stability -run TestB4CombinedAxis -v
//
// Offline, seed=7.
func TestB4CombinedAxisDurable(t *testing.T) {
	t.Logf("B4 combined AXIS: durability vs self-mod level, FULL winning config (soft ON; awake stack at L4)")
	t.Logf("THOUGHT_WAKE_TRANSCRIPT=%v", wakeTranscriptOn())
	t.Logf("%-42s | verdict | peak_n | peak_U | min_mu | fan | regime", "self-mod level [soft ON]")
	t.Logf("%s", "----------------------------------------------------------------------------------------------------")
	durable := 0
	for _, l := range SelfModLevels() {
		r := measureSelfModLevelSoft(l)
		verdict := "DURABLE"
		if !r.Durable() {
			verdict = "BROKEN"
		} else {
			durable++
		}
		mu := "  n/a"
		if l == L4Awake {
			mu = ftoa(r.MinMu)
		}
		t.Logf("%-42s | %-7s | %6.2f | %6.2f | %6s | %3d | %s",
			l.String(), verdict, r.PeakN, r.PeakU, mu, r.MaxFanout, r.Regime.String())
		if !r.Durable() {
			for _, c := range r.Report.Checks {
				if !c.OK {
					t.Logf("    - VIOLATED %s: %s", c.Name, c.Detail)
				}
			}
			t.Errorf("self-mod level %s BROKEN under the soft-ON combined config", l.String())
		}
	}
	if durable != len(SelfModLevels()) {
		t.Fatalf("B4 combined axis: only %d/%d self-mod levels durable", durable, len(SelfModLevels()))
	}
	t.Logf("B4 combined axis: %d/%d self-modification levels hold the durable regime with everything ON", durable, len(SelfModLevels()))
}
