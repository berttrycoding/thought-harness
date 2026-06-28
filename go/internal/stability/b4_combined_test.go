package stability

// B4 COMBINED-CONFIG durability characterization (autonomous validation, NOT a default flip). It runs
// the EXACT C0a/C0b durability gate (the StabilityRegime path CheckEngine + the stability subcommand
// use) over the FULL B4 winning config — every validated config-search winner ON TOGETHER for the FIRST
// time:
//   - conscious.activity.soft  (B1's reactive lifter — raises branch propensity via the Boltzmann policy)
//   - conscious.activity.forest + drive_agenda + seed_intents@full-portfolio (B3's awake stack)
//   - THOUGHT_WAKE_TRANSCRIPT=on (B3-outreach's wake-path fix; set via env on the test invocation, read
//     once at engine init — confirmed live by the wakeTranscriptEnabled package var)
//   - continuous/awake mode (the regime these knobs target)
//
// THE GAP THIS CLOSES: every knob was durability-validated ALONE (B1 soft as a reactive lifter; B3 the
// awake stack to L4; the wake-transcript re-pass durable-with-flag-on). The FULL combination — soft +
// awake stack + wake-transcript, all-ON, together — had never been a single durability pass. This file
// MEASURES the combined plant and reports the interaction diff vs each knob alone (per-branch green !=
// merged green). It is ADDITIVE: it flips no plant default; it only constructs engines, runs them, and
// measures. Offline TestBackend double, seed=7 (deterministic).

import (
	"os"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// wakeTranscriptOn reports the THOUGHT_WAKE_TRANSCRIPT env-knob's state for the diff banner (the engine
// reads the same env once at init via its own unexported resolver — this is a reporting mirror, not the
// source of truth). The combined plant the engine runs honors the env regardless of this read.
func wakeTranscriptOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("THOUGHT_WAKE_TRANSCRIPT"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// b4Features builds an AllOn config with the requested awake knobs + the soft policy flipped on. soft is
// the ONLY knob the B3 featuresFor() did not cover; everything else mirrors featuresFor's awake stack so
// the diff vs the B3 single-knob passes is apples-to-apples. The wake-transcript flag is NOT a config
// field — it is the THOUGHT_WAKE_TRANSCRIPT env-knob read once at engine init, so the caller sets it on
// the test invocation (TestMain-free: the package var resolves at first import).
func b4Features(soft, forest, driveAgenda, seedIntents bool, seedCount int) *config.HarnessConfig {
	feat := config.New() // AllOn (soft OFF, awake knobs OFF by default)
	feat.Conscious.Activity.Soft = soft
	feat.Conscious.Activity.Forest = forest
	feat.Conscious.Activity.DriveAgenda = driveAgenda
	feat.Conscious.Activity.SeedIntents = seedIntents
	if seedIntents {
		feat.Conscious.Activity.SeedIntentCount = seedCount
	}
	feat.Validate()
	return feat
}

// b4Combined is the FULL B4 winning config: soft + the full awake stack at the two-digit portfolio.
func b4Combined() *config.HarnessConfig {
	return b4Features(true, true, true, true, cognition.SeedPortfolioSize())
}

// b4condResult is one config's measured per-condition verdict (every-tick peaks over the extended run +
// the end-of-run gate regime/gain provenance). It is the row the interaction-diff table renders.
type b4condResult struct {
	label    string
	peakN    float64
	peakU    float64
	minMu    float64
	maxOut   int
	regime   regulator.Regime
	measured bool
	kgNA     bool
	gateOK   bool
	gateReg  string
}

// measureB4 runs the gate (24-tick continuous, the exact subcommand path) + the extended every-tick peak
// run (600t) for a feature config, returning the full per-condition row. seed=7 (deterministic).
func measureB4(label string, feat *config.HarnessConfig) b4condResult {
	const gateTicks = 24  // the stability subcommand's continuous-row horizon
	const longTicks = 600 // the C1/B3 extended-run horizon (the worst-case the sparse gate could miss)
	rep := CheckEngine(runAwakeWithFeatures(feat, gateTicks), "B4:"+label, "continuous")
	pN, pU, mMu, mOut, rg, m, kgNA := awakePeaks(feat, longTicks)
	return b4condResult{
		label:    label,
		peakN:    pN,
		peakU:    pU,
		minMu:    mMu,
		maxOut:   mOut,
		regime:   rg,
		measured: m,
		kgNA:     kgNA,
		gateOK:   rep.OK(),
		gateReg:  rep.Regime,
	}
}

// durableEveryTick reports whether the five conditions held at the every-tick extended-run peak, with the
// loop in the benign saturated-bounded open-loop regime (K·g vacuously NA, g on the prior fallback — an
// open-loop K·g must never read as a measured pass). This is the SAME predicate the B3 standing guard +
// CheckEngine enforce, applied to the combined plant.
func (r b4condResult) durableEveryTick() bool {
	conds := r.peakN < 1.0 && r.peakU <= 1.0 && r.minMu > 0.0 && r.maxOut <= cognition.MaxParWidth
	benign := r.regime == regulator.RegimeSaturatedBounded && !r.measured && r.kgNA
	return conds && benign
}

// TestB4CombinedConfigDurable is the B4 LOAD-BEARING combined-config durability guard. It asserts the FULL
// winning config (soft + forest + drive_agenda + seed_intents@19 + wake-transcript) holds the durable
// regime as a SINGLE pass — the gap the single-knob passes could not catch. RUN WITH THE FLAG ON:
//
//	THOUGHT_WAKE_TRANSCRIPT=on go test ./internal/stability -run TestB4Combined -v
//
// The wake-transcript flag is read once at engine init; this test does NOT assert it is on (it is a wake-
// PATH transcript fix that doesn't move the durability numbers — see the interaction diff), but the B4
// validation is RUN with it on so the measured plant is the real combined plant. Offline, seed=7.
func TestB4CombinedConfigDurable(t *testing.T) {
	full := b4Combined()

	// (1) the sound gate (24-tick continuous row, the exact subcommand path) PASSES and is benign.
	rep := CheckEngine(runAwakeWithFeatures(full, 24), "B4 combined (soft+awake-stack+wake-transcript)", "continuous")
	if !rep.OK() {
		t.Fatalf("B4 combined winning config failed the durability gate:\n%s", rep.Format())
	}
	if rep.Regime != regulator.RegimeSaturatedBounded.String() {
		t.Fatalf("B4 combined regime = %q, want saturated-bounded (θ pinned at the floor => benign open loop)", rep.Regime)
	}
	if rep.GainMeasured {
		t.Fatalf("B4 combined gain must be the PRIOR fallback (loop open), not identified — an open-loop K·g must never be a real check")
	}

	// (2) every-tick peaks over the extended (600-tick) run hold the five conditions in the benign regime.
	r := measureB4("combined", full)
	if !r.durableEveryTick() {
		t.Fatalf("B4 combined not durable over 600t: peak_n=%.2f peak_U=%.2f min_mu=%.2f max_out=%d regime=%v measured=%v kgNA=%v",
			r.peakN, r.peakU, r.minMu, r.maxOut, r.regime, r.measured, r.kgNA)
	}

	// (3) the ADVERSARIAL excitation bound — the LOAD-BEARING combined-plant finding. soft RAISES branch
	//     propensity (the Boltzmann policy makes the flat THINK zone choose BRANCH probabilistically); the
	//     awake stack alone (soft OFF) recorded peak_n ~0.39 (B3, ~0.61 headroom to the n=1 cliff). MEASURED
	//     2026-06-18 (every-tick peak, seed=7, TestBackend): the FULL combined plant reaches peak_n ~0.91 at
	//     600t / ~0.92 at 2000t and NEVER crosses 1.0 (TestB4LongHorizonCrossingCheck) — soft is the DOMINANT
	//     n-excitation knob (soft-alone is already ~0.92; the awake stack adds ~0.00 on top), so the
	//     interaction is "soft sets n, the awake stack rides under it", not a compounding climb. The verdict:
	//     DURABLE but the n-headroom is COMPRESSED from ~0.61 to ~0.08. The bound is 0.95 — the cliff is the
	//     real hard fail (n<1, asserted by durableEveryTick); this guards against the peak drifting up from
	//     the recorded ~0.92. branch_propensity β is the dial that restores headroom (β=0.5 => peak_n ~0.84;
	//     β=0.25 => ~0.75 — TestB4SoftExcitationSweep). Raise this bound only with a re-measure.
	if r.peakN >= 0.95 {
		t.Fatalf("B4 combined excited branching past the recorded bound: peak_n=%.2f >= 0.95 (the soft excitation drifted up from the recorded ~0.92 toward the n=1 cliff — re-tune branch_propensity)", r.peakN)
	}

	// (4) the awake stream stays ALIVE, DIVERSE, and does NOT compulsively act, with the full faculty breadth.
	q := measureAwakeQuality(full, 60)
	if q.realThoughts <= 20 {
		t.Fatalf("B4 combined stream died out: %d real thoughts", q.realThoughts)
	}
	if q.diversity <= 0.30 {
		t.Fatalf("B4 combined stream degenerated into repetition: diversity=%.2f", q.diversity)
	}
	if q.acts > 3 {
		t.Fatalf("B4 combined compulsively acted (%dx) instead of moving on", q.acts)
	}
	if q.seedRoots != cognition.SeedPortfolioSize() {
		t.Fatalf("B4 combined planted %d standing roots, want the full portfolio %d", q.seedRoots, cognition.SeedPortfolioSize())
	}
	if q.seedFaculties != cognition.SeedFacultyCount() {
		t.Fatalf("B4 combined (full portfolio) must keep all %d faculties alive, got %d",
			cognition.SeedFacultyCount(), q.seedFaculties)
	}

	t.Logf("B4 combined DURABLE: gate=PASS regime=%s | every-tick(600t) peak_n=%.2f peak_U=%.2f min_mu=%.2f max_out=%d regime=%v g=prior kgNA=%v | quality real=%d divrs=%.2f acts=%d seeds=%d faculties=%d",
		rep.Regime, r.peakN, r.peakU, r.minMu, r.maxOut, r.regime, r.kgNA, q.realThoughts, q.diversity, q.acts, q.seedRoots, q.seedFaculties)
}

// TestB4InteractionDiff prints the per-condition interaction diff (run with -v): the FULL combination vs
// each knob ALONE and vs the soft-OFF B3 awake stack — so the load-bearing question (does combining soft
// with the awake stack + wake-transcript push any condition past where the single knobs sat?) is answered
// with numbers. Verbose only; the standing guard is TestB4CombinedConfigDurable. Offline, seed=7.
func TestB4InteractionDiff(t *testing.T) {
	full := cognition.SeedPortfolioSize()
	rows := []b4condResult{
		measureB4("baseline (all OFF, awake)", b4Features(false, false, false, false, 0)),
		measureB4("soft ALONE (reactive lifter, awake)", b4Features(true, false, false, false, 0)),
		measureB4("awake stack ALONE (B3, soft OFF)", b4Features(false, true, true, true, full)),
		measureB4("FULL COMBINED (soft+awake stack)", b4Combined()),
	}

	t.Logf("B4 combined-config interaction diff (TestBackend double, seed=7, every-tick peaks over 600t; gate=24t continuous)")
	t.Logf("THOUGHT_WAKE_TRANSCRIPT=%v (the wake-path fix; a transcript append, not a durability lever)", wakeTranscriptOn())
	t.Logf("seed portfolio: full=%d", full)
	t.Logf("%-38s | gate | peak_n | peak_U | min_mu | fan/out | regime (K·g)            | g", "config")
	t.Logf("%s", "-----------------------------------------------------------------------------------------------------------------------")
	for _, r := range rows {
		gate := "PASS"
		if !r.gateOK {
			gate = "FAIL"
		}
		gprov := "prior"
		if r.measured {
			gprov = "ident"
		}
		t.Logf("%-38s | %-4s | %6.2f | %6.2f | %6.2f | %3d/%-3d | %-22s | %s",
			r.label, gate, r.peakN, r.peakU, r.minMu, r.maxOut, r.maxOut, r.regime.String(), gprov)
	}

	// The interaction delta on the recursive fork ratio n: soft's branch propensity is the excitation knob.
	soft := rows[1].peakN
	stack := rows[2].peakN
	combined := rows[3].peakN
	t.Logf("INTERACTION (peak_n): soft-alone=%.2f  awake-stack-alone=%.2f  FULL=%.2f  => combined delta vs awake-stack=%+.2f, vs soft-alone=%+.2f",
		soft, stack, combined, combined-stack, combined-soft)
	t.Logf("INTERACTION (peak_U): awake-stack-alone=%.2f  FULL=%.2f  => delta=%+.2f (the U<=1 zero-margin watch)",
		rows[2].peakU, rows[3].peakU, rows[3].peakU-rows[2].peakU)
}
