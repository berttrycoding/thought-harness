// selfmod.go is the STABILITY AXIS of the cognition eval workbench: durability as a function of
// SELF-MODIFICATION LEVEL. It is the third eval axis (after capability + efficiency) and the strongest
// one, because the durability math is DETERMINISTIC — it runs entirely on the seeded TestBackend double,
// no model. It demonstrates the core control-theoretic claim: durability is a REGULATED regime, not a free
// consequence — the plant is time-varying BECAUSE the Subconscious layer self-modifies (mints + synthesises
// + sustains itself on the fly), and a bare model has no analog of this.
//
// THE LEVEL TAXONOMY (verified against the code — internal/convert, internal/cognition synth/operators,
// internal/subconscious/subagent, internal/engine continuous + drives). Each level is a strictly DEEPER
// self-modification of the plant the regulator controls:
//
//	L0 — fixed registry, APPLY-only. A simple Q&A workload whose RecognizeShape returns (nil,false): no
//	     program is synthesised, no sub-agents fan out, nothing is minted. The plant is STATIC — the
//	     closest the harness gets to a bare model (one specialist answers, the graph does not grow). This is
//	     the durability BASELINE every deeper level is measured against.
//	L1 — convertibility. A repeated reactive goal whose effortful GENERATED pattern + recurring program
//	     SHAPE compile (convert.Consolidate) into a minted PrimitiveSubAgent + a minted Skill + (METACOG) a gate
//	     prior. The plant's VOCABULARY self-modifies: the registry gains learned reflexes mid-run. MEASURED:
//	     the repetition makes the plant gain g IDENTIFIABLE (the loop-gain check becomes a real, failable
//	     bool — not the prior fallback).
//	L2 — program synthesis on the fly (+ sub-agent synthesis). Any workflow-shaped reactive goal drives
//	     cognition.Synthesize → a control-flow PROGRAM (series/parallel/loop) → FromProgram → a parallel
//	     phase fans out sub-agents (subconscious.NewSubAgent). The plant's STRUCTURE self-modifies per
//	     episode: program depth + fan-out width vary at runtime. (Sub-agent synthesis is COUPLED to program
//	     synthesis on this substrate — a synthesised program's Par phase IS what spawns the sub-agents — so
//	     the draft's separate "L4 sub-agent synthesis" merges here; they are not an independently-gated
//	     depth.) This is the level that stresses U + fan-out.
//	L3 — runtime operator minting. The synthesiser mints + VERIFIES brand-new operators into the open
//	     catalog (OperatorRegistry.Mint), growing the operator VOCABULARY DIMENSION without bound. On the
//	     offline test double the deterministic toolmaker (RecognizeShapeDict) only uses seed operators, so
//	     this level is driven through the same catalog.Mint API the synthesiser calls — the honest way to
//	     exercise D3 without a model. SCOPE (red-team-corrected): this proves the catalog DIMENSION can grow
//	     34→64 without breaking durability (the ops sit mostly inert); it does NOT prove "a synthesiser
//	     ACTIVELY selecting among 64 vs 34" is excitation-neutral (that would need the live toolmaker). The
//	     point of the level: the "dimension" grows large and durability must be UNAFFECTED.
//	L4 — awake self-sustaining. Continuous mode with the full endogenous stack ON (forest + drive_agenda +
//	     the seed-intent portfolio): Drives + DefaultMode generate endogenous goals, the seed-intent forest
//	     plants standing roots, and the loop modifies its OWN goal stack tick after tick — recursive
//	     self-modification, composing every lower level. This is the deepest level and the one the awake/
//	     Phase-0 track depends on.
//
// Why this is the strongest axis: every level is a DEEPER perturbation of the time-varying plant, and the
// five durability conditions are re-MEASURED at each (never assumed). The curve shows WHERE the regulated
// regime holds and where a condition tightens toward its cliff as self-modification deepens.
//
// All offline + deterministic (TestBackend double, seed=7, the seeded cpyrand RNG). This file is
// ADDITIVE: it changes no plant default — it only constructs engines, runs them, and measures. The
// standing 8/8 suite (RunSuite/Main) is untouched.
package stability

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// SelfModLevel is one rung of the self-modification depth ladder. The String is the curve's row label.
type SelfModLevel int

const (
	L0Fixed        SelfModLevel = iota // fixed registry, APPLY-only (the static-plant baseline)
	L1Convert                          // convertibility: mint specialists/skills/gate-priors
	L2Synthesize                       // program synthesis on the fly (+ sub-agent fan-out)
	L3OperatorMint                     // runtime operator minting (catalog dimension grows)
	L4Awake                            // awake self-sustaining (drives + default-mode + seed-intent forest)
)

// String renders the level label shown on the curve.
func (l SelfModLevel) String() string {
	switch l {
	case L0Fixed:
		return "L0 fixed (APPLY-only)"
	case L1Convert:
		return "L1 convertibility (mint reflexes)"
	case L2Synthesize:
		return "L2 program synthesis (+sub-agents)"
	case L3OperatorMint:
		return "L3 operator minting (catalog grows)"
	case L4Awake:
		return "L4 awake self-sustaining (drives+forest)"
	}
	return "unknown"
}

// SelfModLevels is the ordered ladder the stability axis walks (shallow → deep).
func SelfModLevels() []SelfModLevel {
	return []SelfModLevel{L0Fixed, L1Convert, L2Synthesize, L3OperatorMint, L4Awake}
}

// SelfModResult is one level's place on the durability-vs-self-mod-level curve: the five-condition
// verdict (the underlying Report) plus the level's worst-case condition values (every-tick peaks) and the
// self-modification EVIDENCE that proves the level actually exercised its mechanism (the curve must not be
// a vacuous all-pass — a level whose mechanism never fired would be measuring nothing). Regime is the
// end-of-run C0a loop-gain regime; GainMeasured records whether g was IDENTIFIED from the ring (a real
// loop-gain check) or is the prior fallback.
type SelfModResult struct {
	Level        SelfModLevel
	Report       Report // the five-condition verdict (the same CheckEngine path the standing suite uses)
	PeakN        float64
	PeakU        float64
	MinMu        float64 // awake levels only (1.0 sentinel = not applicable / never sampled positive)
	MaxFanout    int     // peak per-tick sub-agent fan-out (the fan-out<=W_max condition value, from the Report)
	MaxOutstand  int     // peak async-action dead-time (in-flight actions), the awake bounded-dead-time stressor
	Regime       regulator.Regime
	GainMeasured bool // g identified at END of run (the final CheckEngine read)
	GainEverID   bool // g was IDENTIFIED at SOME tick during the run (the loop-gain check became real)
	// self-modification evidence (the level genuinely self-modified the plant):
	MintedOps    int // operators minted into the open catalog (L3)
	MintedSpec   int // specialists minted by convertibility (L1)
	MintedSkill  int // skills minted by convertibility (L1)
	SynthEvents  int // programs synthesised (L2)
	SubagentFire int // sub-agent fan-out fires (L2)
	SeedRoots    int // standing seed-intent roots planted (L4)
}

// Durable reports the level's verdict (every condition held).
func (r SelfModResult) Durable() bool { return r.Report.OK() }

// selfModFeatures builds the config that turns ON exactly the self-modification mechanisms a level needs.
// L0–L3 run reactive on the default (AllOn) config — synthesis/convertibility/operator-mint are always
// wired on the default substrate (the Synthesis/SubAgents config fields are NOT consumed as ablation
// gates, verified), so a level is distinguished by its WORKLOAD, not by a feature flip. L4 flips on the
// full awake endogenous stack (forest + drive_agenda + the seed-intent portfolio).
func selfModFeatures(l SelfModLevel) *config.HarnessConfig {
	feat := config.New() // AllOn
	if l == L4Awake {
		feat.Conscious.Activity.Forest = true
		feat.Conscious.Activity.DriveAgenda = true
		feat.Conscious.Activity.SeedIntents = true
		feat.Conscious.Activity.SeedIntentCount = cognition.SeedPortfolioSize()
	}
	feat.Validate()
	return feat
}

// MeasureSelfModLevel runs the representative workload for a level and returns its place on the curve. It
// builds a fresh engine on the offline TestBackend double (seed=7), samples the regulator EVERY tick (the
// C1 lesson: n's EMA spikes BETWEEN every-100 sample points — a sparse sample under-reads the true peak),
// runs the level's self-modifying workload, and measures the five conditions over the whole run via the
// SAME CheckEngine path the standing suite + the stability subcommand use. Deterministic + offline.
func MeasureSelfModLevel(l SelfModLevel) SelfModResult {
	mode := "reactive"
	if l == L4Awake {
		mode = "continuous"
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = mode
	cfg.Seed = 7
	cfg.Features = selfModFeatures(l)

	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("MeasureSelfModLevel: NewEngine on the test double failed: " + err.Error())
	}

	res := SelfModResult{Level: l, MinMu: 1.0}
	// every-tick peak sampling (the worst case the sparse gate could miss) is folded into the run loop.
	runSelfMod(e, l, &res)

	// the five-condition verdict over the whole run — the standing CheckEngine path.
	res.Report = CheckEngine(e, l.String(), mode)
	res.Regime = regimeOf(res.Report.Regime)
	res.GainMeasured = res.Report.GainMeasured
	// the fan-out<=W_max condition value is the Report's measured per-tick sub-agent fan-out (the
	// per-tick sampler tracks async-outstanding, a separate dead-time stressor — kept on MaxOutstand).
	if res.Report.Metrics != nil {
		res.MaxFanout = res.Report.Metrics.MaxFanout
	}

	// self-modification evidence (so the curve is provably non-vacuous: the level fired its mechanism).
	res.MintedOps = len(e.Catalog().Minted())
	res.MintedSpec = len(e.Convert().Minted)
	res.MintedSkill = len(e.Convert().MintedSkill)
	return res
}

// runSelfMod drives the level's representative self-modifying workload, sampling the regulator every tick
// to capture the worst-case peaks (peak_n / peak_U / max async-outstanding) the sparse end-of-run read
// would miss. It returns nothing — it fills the peak + evidence fields on res. Determinism: one seeded
// engine, no wall clock.
//
// Per-level workload:
//   - L0: ONE simple Q&A goal whose shape is unrecognised (no synthesis, no fan-out, nothing minted).
//   - L1: the SAME optimize-loop goal repeated REPEAT_L1 times on one engine, so the effortful repetition
//   - the recurring program shape clear the convertibility mint gate (specialist + skill).
//   - L2: a sequence of distinct workflow-shaped goals (design / compare||contrast / diagnose) so each
//     synthesises a fresh program with a parallel fan-out — the structure-varying plant.
//   - L3: a workflow-shaped seed run, then mint MINT_L3 brand-new operators into the catalog (the
//     synthesiser's mint+verify API), then a fresh workflow run on the GROWN catalog (D3: the dimension
//     grew; durability must be unaffected).
//   - L4: the awake continuous loop over LONG_AWAKE ticks with the full endogenous stack — drives,
//     default-mode wander, and the seed-intent forest modifying the goal stack tick after tick.
func runSelfMod(e *engine.Engine, l SelfModLevel, res *SelfModResult) {
	// subscribe a counter for the self-mod evidence + a per-tick peak sampler wired through the run.
	wireSelfModCounters(e, res)

	switch l {
	case L0Fixed:
		e.SubmitDefault("What is 6 times 7?")
		runTicksSampling(e, 24, res)

	case L1Convert:
		for i := 0; i < repeatL1; i++ {
			e.SubmitDefault("Optimize the checkout flow to be faster")
			runTicksSampling(e, 24, res)
		}

	case L2Synthesize:
		for _, goal := range []string{
			"Design a small API for a todo service",
			"Compare REST versus GraphQL for our API",
			"Why is the build failing intermittently?",
		} {
			e.SubmitDefault(goal)
			runTicksSampling(e, 24, res)
		}

	case L3OperatorMint:
		// seed run (the synthesiser path), then grow the operator dimension via the same mint API the
		// synthesiser calls, then a fresh run on the grown catalog.
		e.SubmitDefault("Design a small API for a todo service")
		runTicksSampling(e, 24, res)
		for i := 0; i < mintL3; i++ {
			// "transformative" is a valid family; the >=3-word intent clears OperatorRegistry.Mint's gate.
			e.Catalog().Mint("synth_op_"+strconv.Itoa(i), "transformative",
				"a runtime-synthesised operator number "+strconv.Itoa(i)+" minted for the stability axis")
		}
		e.SubmitDefault("Design a small API for a todo service")
		runTicksSampling(e, 24, res)

	case L4Awake:
		runTicksSampling(e, longAwake, res)
	}
}

// repetition / sizing constants for the per-level workloads. Deliberately modest so the axis is fast +
// deterministic; the awake horizon matches the C1 extended-run guard.
const (
	repeatL1  = 8   // repeats of the optimize-loop goal to clear MintAfter=3 (specialist + skill)
	mintL3    = 30  // brand-new operators minted into the catalog (catalog grows from 34 → 64)
	longAwake = 600 // the awake extended-run horizon (the C1 / B3 guards' horizon)
)

// wireSelfModCounters subscribes the self-mod evidence counters to the engine bus. Read-only; counts the
// synthesis / sub-agent / seed-intent events that prove the level exercised its mechanism.
func wireSelfModCounters(e *engine.Engine, res *SelfModResult) {
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.SubSynthesize:
			res.SynthEvents++
		case events.SubSubagent:
			res.SubagentFire++
		case events.SeedIntent:
			res.SeedRoots++
		}
	})
}

// runTicksSampling steps the engine `ticks` times, sampling the regulator EVERY tick to capture the
// worst-case peaks (peak_n, peak_U) + the max async-outstanding, and the awake μ baseline on the
// steady-state every-100 cadence (μ=0 during the first-tick warmup transient by design). It accumulates
// into res across multiple calls (L1/L2/L3 run several workloads on one engine).
func runTicksSampling(e *engine.Engine, ticks int, res *SelfModResult) {
	for t := 1; t <= ticks; t++ {
		e.Step()
		r := e.Regulator()
		res.PeakN = maxF(res.PeakN, r.N())
		res.PeakU = maxF(res.PeakU, r.Util())
		if out := e.ActionOutstanding(); out > res.MaxOutstand {
			res.MaxOutstand = out
		}
		// the loop-gain check becomes a REAL, failable bool at any tick g is identified from the ring.
		// Record it (the end-of-run read can settle back to the prior fallback as θ pins).
		if _, measured := r.GainEstimate(); measured {
			res.GainEverID = true
		}
		if t%100 == 0 {
			if r.Mu() < res.MinMu {
				res.MinMu = r.Mu()
			}
		}
	}
}

// MeasureSelfModCurve walks the whole self-modification ladder (shallow → deep) and returns the
// durability-vs-self-mod-level curve. Deterministic + offline (TestBackend, seed=7).
func MeasureSelfModCurve() []SelfModResult {
	levels := SelfModLevels()
	out := make([]SelfModResult, 0, len(levels))
	for _, l := range levels {
		out = append(out, MeasureSelfModLevel(l))
	}
	return out
}

// FormatSelfModCurve renders the curve as a human-readable per-level condition table (the stability-axis
// report body). One header + one row per level: the verdict, the five worst-case condition values, the
// loop-gain regime + gain provenance, and the self-modification evidence. Pure string build — no I/O.
func FormatSelfModCurve(curve []SelfModResult) string {
	var b strings.Builder
	b.WriteString("=== Stability axis: durability vs self-modification level ===\n")
	b.WriteString("(deterministic offline: TestBackend double, seed=7, every-tick peak sampling)\n\n")
	b.WriteString(fmt.Sprintf("%-40s | %-7s | %-7s | %-7s | %-6s | %-3s | %-4s | %-18s | %-7s | %s\n",
		"self-mod level", "verdict", "peak_n", "peak_U", "min_mu", "fan", "out", "regime (K·g)", "g", "self-mod evidence"))
	b.WriteString(strings.Repeat("-", 165) + "\n")
	for _, r := range curve {
		verdict := "DURABLE"
		if !r.Durable() {
			verdict = "BROKEN"
		}
		mu := "  n/a"
		if r.Level == L4Awake {
			mu = fmt.Sprintf("%.2f", r.MinMu)
		}
		// gain provenance: "ident" if g was identified at end of run (the loop-gain check is a real bool);
		// "ident*" if it was identified at SOME tick but settled back to the prior as θ pinned; else "prior".
		gprov := "prior"
		if r.GainMeasured {
			gprov = "ident"
		} else if r.GainEverID {
			gprov = "ident*"
		}
		evidence := fmt.Sprintf("ops=%d spec=%d skill=%d synth=%d subagents=%d seeds=%d",
			r.MintedOps, r.MintedSpec, r.MintedSkill, r.SynthEvents, r.SubagentFire, r.SeedRoots)
		b.WriteString(fmt.Sprintf("%-40s | %-7s | %6.2f  | %6.2f  | %5s  | %3d | %4d | %-18s | %-7s | %s\n",
			r.Level.String(), verdict, r.PeakN, r.PeakU, mu, r.MaxFanout, r.MaxOutstand, r.Regime.String(), gprov, evidence))
		if !r.Durable() {
			// the violated-condition diagnosis under any BROKEN row.
			for _, c := range r.Report.Checks {
				if !c.OK {
					b.WriteString("        - VIOLATED " + c.Name + ": " + c.Detail + "\n")
				}
			}
		}
	}
	return b.String()
}

// MainAxis runs the stability AXIS (durability vs self-modification level), prints the per-level condition
// curve, and returns the process exit code. It is the `thought stability --axis` entry — the third eval
// axis, fully offline + deterministic (TestBackend double, seed=7). Exit 0 iff the durable regime holds at
// EVERY self-modification level (the regulated-regime claim); non-zero names the level that broke. It is
// the only function here that does I/O — the engine emits, it does not print.
func MainAxis() int {
	curve := MeasureSelfModCurve()
	fmt.Print(FormatSelfModCurve(curve))
	durable := 0
	for _, r := range curve {
		if r.Durable() {
			durable++
		}
	}
	fmt.Printf("\n%d/%d self-modification levels hold the durable regime.\n", durable, len(curve))
	if durable == len(curve) {
		return 0
	}
	return 1
}

// MutatedSupercriticalReport builds a Report through the EXACT regulator→Report path CheckEngine uses,
// over a regulator deliberately driven SUPERCRITICAL (a fork count that pushes the branching ratio n past
// the n=1 cliff). It is the FAILABLE-axis proof (task #4, the C0b anti-vacuity lesson): a config that
// violates a condition MUST read NOT-durable. If this Report is OK(), the curve is a vacuous all-pass and
// the whole axis is meaningless — so the standing guard asserts this is BROKEN. Pure + deterministic (a
// fixed xorshift drive, no wall clock, no RNG dependency). The driven regime is saturated-runaway (the
// controller rails at ThetaMax and λ̂ stays over the intensity ceiling): both the n<1 condition AND the
// loop-gain condition fail honestly.
func MutatedSupercriticalReport() Report {
	r := regulator.New(nil, nil)
	for i := 0; i < 30; i++ {
		o := regulator.DefaultUpdateOpts()
		o.Fired = 5
		o.Admitted = 5
		o.Forked = 10 // 10 offspring per thought -> the n EMA climbs well past the n=1 cliff
		o.BranchesLive = 2
		r.Update(o)
	}
	checks, regime, _, measured := r.StabilityRegime("reactive")
	rep := Report{Workload: "MUTATION: supercritical (n>=1) plant", Regime: regime.String(), GainMeasured: measured}
	for _, sc := range checks {
		rep.Checks = append(rep.Checks, Check{Name: sc.Name, OK: sc.Pass || sc.NA, Detail: "violated (" + regCheckValue(sc) + ")"})
	}
	return rep
}

// regimeOf maps a regime label string (the only handle the Report carries) back to the typed Regime, so
// SelfModResult can expose the typed value. The label set is closed (Regime.String), so an unknown label
// is impossible on the CheckEngine path; it falls through to RegimeActivelyControlled's zero value only if
// the Report carried no regime (the structural reports — never the case for a self-mod level engine).
func regimeOf(label string) regulator.Regime {
	for _, rg := range []regulator.Regime{
		regulator.RegimeActivelyControlled, regulator.RegimeSaturatedBounded, regulator.RegimeInsufficientLoop,
		regulator.RegimeUnidentifiedActive, regulator.RegimeSaturatedRunaway,
	} {
		if rg.String() == label {
			return rg
		}
	}
	return regulator.RegimeActivelyControlled
}
