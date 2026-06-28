// Package stability is durability validation for the DYNAMIC harness — the durability math,
// re-applied to a plant whose dimension varies at runtime (PORT-PLAN #41, ports stability.py).
//
// The subconscious layer is generative: operators are minted, programs are synthesised, sub-agents
// fan out in parallel. So the plant the regulator controls is *time-varying* — its configuration
// (per-tick excitation width, program depth, catalog size) changes as the system thinks. The
// classical durability conditions (n<1, U<=1, 0<K*g<2, mu>0) were stated for a fixed structure.
// This package re-applies them to the dynamic case and MEASURES the BOUNDEDNESS conditions (n<1, U<=1,
// bounded fan-out, mu>0 awake) hold (Phase-0 discipline: measure, don't assert).
//
// HONESTY ON 0<K·g<2 (the loop-gain condition): it is NOT measured on the representative workloads. On
// every workload the suite runs, the controller is either saturated/open (the awake steady-state pins θ
// at the floor) or insufficiently excited (a short reactive episode that ratchets θ once and settles), so
// the plant gain g is NOT identifiable from the ring — the check falls back to the configured prior and is
// reported as a VACUOUS NA: telemetry, not a held condition (see Report.KgTelemetryOnly / HeldCount). So
// on real workloads the regulator's durability gate is a flat BOUNDEDNESS check (n<1, U≤1, bounded fan-out,
// μ>0) — equivalent in force to LATHE's step-cap + MaxSameError backstops — NOT a measured control-theoretic
// proof. The 0<K·g<2 check is a REAL, failable check ONLY in the actively-controlled regime (g identified
// from the ring) or when the loop has genuinely lost control (a runaway/unidentified-active honest FAIL);
// neither regime is reached by the standing suite, so the check is telemetry there. The control-theoretic
// argument that the BOUNDS are sufficient is the north-star proof in
// docs/spec/cognitive-os-durability-analysis.md — the live system does NOT claim to measure K·g.
//
// Mapping to a control-theoretic framework:
//   - the regulator is the P controller: actuation u=θ, error e=λ̂−λ*, gain Kp=gain_K.
//   - the thought-stream excitation is the plant.
//   - dynamic synthesis makes the plant parameter-varying; the textbook answer is gain-scheduled P
//     and/or bounding the parameter variation so every reachable configuration is stable.
//
// The re-derivation (full argument in docs/reference/stability-dynamic-dimensionality.md):
//   - The fork-ratio n is the branching ratio (recursive forks per tick), NOT the parallel fan-out
//     width w(t). A first Phase-0 pass measured n via an `admitted−1` proxy and seemed to show an
//     n=1 cliff at w≥4 (⇒ a "W_max=3"); that was a MEASUREMENT ARTIFACT — the proxy over-counted a
//     zero-fork parallel burst. Measuring n as actual forks shows fan-out width does NOT drive n.
//     So D1 is: keep the fork-ratio n subcritical (n<1) — independent of fan-out width.
//   - Fan-out width is a SCHEDULABILITY / compute budget (not a durability bound): a parallel phase
//     fans out to ≤ cognition.MaxParWidth (default 8, THOUGHT_MAX_PAR_WIDTH-overridable), enforced by
//     VerifyProgram, and collapses to one Gate winner — so the burst doesn't raise n (D4).
//   - Program horizon (steps/depth/loops) is bounded ⇒ the switched plant visits a finite set of
//     configurations, each for bounded duration (D2).
//   - Catalog growth is excitation-neutral (D3): minting an operator grows the vocabulary dimension
//     but not the per-tick branching (firing is governed by program width, not catalog size). So the
//     "dimension" can grow without bound without threatening durability — the parameter that matters
//     for stability (fan-out width) is bounded, the one that grows (catalog) does not couple to n.
//
// Conclusion: a switched, P-controlled system with a subcritical fork-ratio (D1, n<1), bounded
// fan-out (D4), bounded horizon (D2) and excitation-neutral vocabulary growth (D3) is uniformly durable.
//
// This is a non-core package (it constructs engines, runs them, and Main prints a report), so it may
// build on the engine and use fmt for its console summary. The engines it builds run on the
// TestBackend test double (offline + deterministic) — the same path the parity tests pin — so
// the suite is reproducible and needs no model.
package stability

import (
	"fmt"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/regulator"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// NMargin is the subcritical cliff: the branching ratio must stay strictly below 1. Mirrors the
// Python module-level N_MARGIN = 1.0.
const NMargin = 1.0

// Check is one stability condition's verdict (Python @dataclass Check). It is local to this package
// and distinct from regulator.Check: this carries a free-text Detail rendered only on failure, where
// the regulator's Check carries the wire NA flag.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Metrics is the measured-over-the-run summary (Python's Report.metrics dict). A nil *Metrics means
// "no metrics" (the structural-bound report, whose Python dict is empty → no metrics line); a non-nil
// zero-valued Metrics still renders a line (the skill-composition report sets an explicit all-zero
// dict, which is truthy in Python). The field names match the Python dict keys consumed by Format.
type Metrics struct {
	PeakN     float64 // peak branching ratio over the run
	PeakU     float64 // peak utilisation over the run
	LamHatMax float64 // peak measured intensity over the run
	MuMax     float64 // peak baseline (awake) over the run
	MaxFanout int     // peak per-tick sub-agent fan-out over the run
	Minted    int     // operators minted this run
	ThetaEnd  float64 // the control variable at run end
}

// Report is one workload's verdict + metrics (Python @dataclass Report). Metrics is a pointer so a
// nil distinguishes "no metrics" from "zero metrics" (see Metrics).
//
// Regime is the C0a loop-gain regime label for the run (empty for the structural/skill reports that
// build no engine): "actively-controlled-stable" (g identified, 0<K·g<2 a real check),
// "saturated-bounded" (controller pinned → loop open → K·g vacuous, durable by the other four), or
// "unidentified-active-FAIL" (plant hot but loop gain unvouched — honest fail). GainMeasured records
// whether g was IDENTIFIED from the ring (true) or is the prior fallback (false) — so the report can
// never hide a prior-pass as a real check.
type Report struct {
	Workload     string
	Checks       []Check
	Metrics      *Metrics
	Regime       string
	GainMeasured bool
	// Warnings are non-failing advisories rendered under the head (e.g. a ZERO-MARGIN condition that
	// PASSES at the boundary but has no slack left). They never flip OK() — they make a boundary case
	// visible instead of silent (HOLE 3a: U==1.0 is a pass with no margin).
	Warnings []string
	// KgTelemetryOnly is set when the 0<K·g<2 loop-gain entry is VACUOUS on this workload (an NA entry:
	// the loop is saturated/open or insufficiently excited, so the plant gain g was NOT identified from
	// the ring — it is the configured prior). On such a workload the loop-gain condition is UNVALIDATED:
	// it is telemetry, not a held control-theoretic check, so it is rendered as an explicit telemetry-only
	// line AND excluded from the held-conditions count (HeldCount). The durable verdict on these workloads
	// rests entirely on the FOUR boundedness conditions (n<1, U<=1, bounded fan-out, μ>0 awake) — the same
	// force as LATHE's step-cap + MaxSameError, NOT a measured control-theoretic K·g proof. When the loop
	// is genuinely actively-controlled (g IDENTIFIED), this is false and the K·g entry IS a held check.
	KgTelemetryOnly bool
}

// OK reports whether every check held (Python Report.ok property: all(c.ok for c in checks)). An
// empty check list is vacuously OK, matching Python's all([]) == True.
func (r Report) OK() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// kgCheckName is the loop-gain entry's name (shared with the regulator's Check.Name). Used to detect
// and exclude the vacuous/telemetry-only K·g entry from the held-conditions count.
const kgCheckName = "0<K*g<2 (regulator stable)"

// HeldCount is the number of REAL held control-theoretic conditions on this workload — the conditions
// that actually gate durability, NOT counting the loop-gain entry when it is telemetry-only (NA: the
// loop is saturated/open and g was not identified, so K·g is UNVALIDATED on this workload). It returns
// (held, total): a condition is held iff its check is OK AND it is not a vacuous K·g telemetry entry; the
// total likewise excludes a telemetry-only K·g. This is what the report renders, so the verdict can never
// silently bank a prior-fallback K·g as a measured control-theoretic check (the proven-vacuous over-claim
// this method exists to retire). On a genuinely actively-controlled workload (g IDENTIFIED) the K·g entry
// is a real check and IS counted.
func (r Report) HeldCount() (held, total int) {
	for _, c := range r.Checks {
		if r.KgTelemetryOnly && c.Name == kgCheckName {
			continue // vacuous loop-gain entry: telemetry, not a held condition — excluded from both.
		}
		total++
		if c.OK {
			held++
		}
	}
	return held, total
}

// Format renders the one-or-two-line head + the failed-check detail lines. Mirrors Python
// Report.format(): "[PASS|FAIL] <workload>", an optional metrics line (only when metrics are set),
// then one "     - <name>: <detail>" line per FAILED check. The metrics line reproduces the Python
// format string byte-for-byte (2-decimal floats, bare ints).
func (r Report) Format() string {
	head := "[FAIL] " + r.Workload
	if r.OK() {
		head = "[PASS] " + r.Workload
	}
	lines := []string{head}
	if r.Regime != "" {
		// The C0a loop-gain regime label + the gain provenance + the held/total condition count, so the
		// verdict is never a hidden prior-pass: a saturated-bounded row says so (K·g N/A), an
		// actively-controlled row says g was identified, and an unidentified-active row reads as the honest
		// fail it is. The held count EXCLUDES a telemetry-only K·g (see HeldCount), so a vacuous loop-gain
		// entry is never banked as a held control-theoretic check.
		prov := "g=prior-fallback"
		if r.GainMeasured {
			prov = "g=identified"
		}
		held, total := r.HeldCount()
		lines = append(lines, fmt.Sprintf("   regime: %s (%s); %d/%d held conditions", r.Regime, prov, held, total))
	}
	if r.KgTelemetryOnly {
		// The loop-gain condition is UNVALIDATED on this workload: g was NOT identified from the ring
		// (the loop is saturated/open or insufficiently excited), so 0<K·g<2 is telemetry, not a held
		// control-theoretic check — it is excluded from the held-conditions count above. Surface it
		// explicitly so the durable verdict reads honestly as a BOUNDEDNESS verdict (n<1, U≤1, fan-out,
		// μ>0), not a measured control-theoretic K·g proof.
		lines = append(lines, "   loop-gain: g not identified on this workload (prior-fallback); K·g telemetry-only (UNVALIDATED, not a held condition)")
	}
	for _, w := range r.Warnings {
		// A zero-margin (boundary) PASS rendered so it is not silent (HOLE 3a). It does not flip OK().
		lines = append(lines, "   warning: "+w)
	}
	if m := r.Metrics; m != nil {
		lines = append(lines, fmt.Sprintf(
			"   metrics: peak_n=%.2f peak_U=%.2f max_fanout=%d lam_hat_max=%.2f minted=%d",
			m.PeakN, m.PeakU, m.MaxFanout, m.LamHatMax, m.Minted))
	}
	for _, c := range r.Checks {
		if !c.OK {
			lines = append(lines, "     - "+c.Name+": "+c.Detail)
		}
	}
	return strings.Join(lines, "\n")
}

// measure scans a finished run's event log for the durability metrics (Python _measure): the peak
// regulator readings, the peak per-tick sub-agent fan-out, and the minted-operator count. The full
// replay ring is read via Bus.Recent (a large n returns every retained event, matching Python's
// iteration over engine.bus.log). Regulator data values are round3 float64s on the wire.
func measure(e *engine.Engine) *Metrics {
	bus := e.Bus()
	all := bus.Recent(1<<30, nil) // the whole replay ring (Python iterates engine.bus.log)

	fan := map[int]int{}
	var (
		peakN, peakU, lamHatMax, muMax float64
		thetaEnd                       float64
		sawReg                         bool
	)
	for _, ev := range all {
		switch ev.Kind {
		case events.Regulator:
			sawReg = true
			peakN = maxF(peakN, dataF(ev.Data, "n"))
			peakU = maxF(peakU, dataF(ev.Data, "U"))
			lamHatMax = maxF(lamHatMax, dataF(ev.Data, "lam_hat"))
			muMax = maxF(muMax, dataF(ev.Data, "mu"))
			thetaEnd = dataF(ev.Data, "theta") // last regulator event wins (Python regs[-1]["theta"])
		case events.SubSubagent:
			fan[ev.Tick]++
		}
	}
	if !sawReg {
		// Python's max((...), default=0.0) over an empty regs list yields 0.0 for every reading.
		peakN, peakU, lamHatMax, muMax, thetaEnd = 0, 0, 0, 0, 0
	}
	return &Metrics{
		PeakN:     peakN,
		PeakU:     peakU,
		LamHatMax: lamHatMax,
		MuMax:     muMax,
		MaxFanout: maxFanout(fan),
		Minted:    len(e.Catalog().Minted()),
		ThetaEnd:  thetaEnd,
	}
}

// CheckEngine validates the durability conditions over a finished run — static + dynamic, all
// measured (Python check_engine). The dynamic conditions (peak n/U, fan-out) are measured over the
// whole run; the static regulator conditions (gain stability, async dead-time, awake baseline) come
// from regulator.Stability for the mode. In a non-reactive mode the awake baseline mu>0 is also
// asserted on the measured peak.
func CheckEngine(e *engine.Engine, workload string, mode string) Report {
	m := measure(e)
	rep := Report{Workload: workload, Metrics: m}

	// Dynamic conditions (measured over the whole run, not just the final tick).
	rep.Checks = append(rep.Checks, Check{
		Name:   "n<1 (subcritical, peak over run)",
		OK:     m.PeakN < NMargin,
		Detail: fmt.Sprintf("peak branching n=%.2f reached the n=1 cliff", m.PeakN),
	})
	rep.Checks = append(rep.Checks, Check{
		Name:   "U<=1 (schedulable, peak over run)",
		OK:     m.PeakU <= 1.0,
		Detail: fmt.Sprintf("peak utilisation U=%.2f over-subscribed focus", m.PeakU),
	})
	// U==1.0 is schedulable (the check PASSES) but has ZERO margin — the scheduler is exactly fully
	// committed, so any added load tips it to U>1 (unschedulable). Surface it as a non-failing warning
	// rather than letting a zero-margin pass read identically to a comfortable one (HOLE 3a).
	if m.PeakU == 1.0 {
		rep.Warnings = append(rep.Warnings,
			"U==1.00 zero-margin: schedulable at the boundary (no slack — any added load tips to U>1)")
	}
	rep.Checks = append(rep.Checks, Check{
		Name: fmt.Sprintf("fan-out<=W_max (%d)", cognition.MaxParWidth),
		OK:   m.MaxFanout <= cognition.MaxParWidth,
		Detail: fmt.Sprintf("per-tick fan-out %d exceeded the compute/schedulability budget "+
			"(raise THOUGHT_MAX_PAR_WIDTH)", m.MaxFanout),
	})

	// Static regulator conditions (gain stability, async dead-time, awake baseline) + the C0a loop-gain
	// regime. The Go regulator returns typed []Check where Pass holds and NA marks a VACUOUS entry —
	// the reactive μ>0 entry, or (C0a) the 0<K·g<2 entry when the controller is saturated/open-loop. A
	// check is OK iff it passed OR is an NA entry: an NA K·g is "vacuous, durable by the other four"
	// (saturated-bounded), NOT a silent prior-pass — an unidentified-but-active plant returns
	// Pass=false (honest fail), which fails here.
	regChecks, regime, _, measured := e.Regulator().StabilityRegime(mode)
	rep.Regime = regime.String()
	rep.GainMeasured = measured
	for _, sc := range regChecks {
		ok := sc.Pass || sc.NA
		detail := "violated (" + regCheckValue(sc) + ")"
		// The 0<K·g<2 entry is TELEMETRY-ONLY (UNVALIDATED) on this workload when it is a vacuous NA:
		// the loop is saturated/open or insufficiently excited, so the plant gain g was not identified
		// (the configured prior is used). On such a workload the loop-gain check does not gate the
		// verdict — it is excluded from the held-conditions count and rendered as an explicit
		// telemetry-only line (see HeldCount / Format). It remains a REAL, failable check only when the
		// loop is actively-controlled (g IDENTIFIED) or has genuinely lost control (an honest Pass=false).
		if sc.Name == kgCheckName && sc.NA {
			rep.KgTelemetryOnly = true
		}
		rep.Checks = append(rep.Checks, Check{Name: sc.Name, OK: ok, Detail: detail})
	}

	if mode != "reactive" {
		rep.Checks = append(rep.Checks, Check{
			Name:   "mu>0 (awake baseline measured)",
			OK:     m.MuMax > 0.0,
			Detail: fmt.Sprintf("awake mode produced no baseline (mu_max=%.2f)", m.MuMax),
		})
	}
	return rep
}

// CheckEngineReactive is the common call: mode="reactive" (Python check_engine's default).
func CheckEngineReactive(e *engine.Engine, workload string) Report {
	return CheckEngine(e, workload, "reactive")
}

// run builds an engine on the offline TestBackend test double, submits the goal (twice in
// continuous mode, after the awake seed), and runs it for ticks. Mirrors Python _run(goal, mode,
// ticks=24). The TestBackend keeps the run deterministic + offline (the parity-pinned path),
// standing in for Python's default-substrate Engine(EngineConfig(...)). A construction failure is
// impossible on the test double (no model to resolve), so the error is dropped after a guard.
func run(goal string, mode string, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = mode
	cfg.MaxTicks = ticks
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		// Unreachable on the test double; panic surfaces a wiring regression loudly rather
		// than returning a nil engine the caller would dereference.
		panic("stability.run: NewEngine on the test double failed: " + err.Error())
	}
	if goal != "" {
		e.SubmitDefault(goal) // continuous with no goal self-seeds its wander (no synthetic kickoff)
	}
	e.Run(ticks)
	return e
}

// runReactive is the common 1-arg form: a reactive run of 24 ticks (Python _run's defaults).
func runReactive(goal string) *engine.Engine { return run(goal, "reactive", 24) }

// runCapabilityPlant builds a reactive engine with subconscious.capability ON — the subconscious
// object-model rewire plant (the Capability sources a Scope ceiling, captures a rich Context, and staffs
// every worker with both). It changes WHAT a worker reads (the whole frozen branch vs the ≤5 slice) and
// CONSTRAINS its tool picks to the ceiling (ScopedToolScope can only REDUCE the tool set, never add) — so
// the plant's control dimensions (n fork-depth, U schedulability, fan-out width, call count) are unchanged
// or reduced vs the flag-OFF plant. This encodes that as a STANDING failable cell so the control-theory
// gate re-passes automatically on the capability plant rather than being hand-derived. seed pinned by
// DefaultConfig (the deterministic test-double stream).
func runCapabilityPlant(goal string, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.MaxTicks = ticks
	feat := config.New() // AllOn baseline
	feat.Subconscious.Capability = true
	feat.Validate()
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runCapabilityPlant: NewEngine on the test double failed: " + err.Error())
	}
	if goal != "" {
		e.SubmitDefault(goal)
	}
	e.Run(ticks)
	return e
}

// runCapabilityDispatchPlant builds a reactive engine with BOTH subconscious.capability AND subconscious.
// capability_dispatch ON — the GAP 5-DEEPER relevance/dispatch ENTRY (01-subconscious §3.3): the producing
// Capability OWNS the dispatch loop's per-tick workflow-recognition (Capability.RecognizeWorkflow) instead
// of the Workflow self-triggering.
//
// DISPATCH-ENTRY NOTE: with capability_dispatch ON the per-tick recognition is OWNED by the producing
// Capability instead of the Workflow self-triggering, but the recognition PREDICATE for a NON-bespoke
// workflow is the PERMISSIVE has-any `gradedRelevance(stream, Triggers) > 0` — the SAME relevance criterion
// as the binary has-any keyword match (theta is NOT consulted at recognition; θ-gating recognition is the
// refuted double-gate, see Workflow.recognizeViaGraded). So the recognition set EQUALS the binary has-any
// set — the entry fires a recognised phase exactly as often as the binary path (the plant is unchanged on
// the recognition dimension; only WHO decides moved). Excitation n (fork-depth), fan-out width,
// schedulability U, and the call count are therefore unchanged vs the binary plant, and μ/K·g are untouched
// (recognition gates a phase, not the regulator's loop). The bespoke short-circuit is preserved, so the
// episode-production path (which produces bespoke workflows) is likewise unchanged. This encodes the
// dispatch-entry plant as a STANDING failable cell so the control-theory gate re-passes automatically rather
// than being hand-derived — the formal continuous-mode-operator gate is REQUIRED before any flag-flip (do
// NOT self-certify).
func runCapabilityDispatchPlant(goal string, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.MaxTicks = ticks
	feat := config.New() // AllOn baseline
	feat.Subconscious.Capability = true
	feat.Subconscious.CapabilityDispatch = true
	feat.Validate()
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runCapabilityDispatchPlant: NewEngine on the test double failed: " + err.Error())
	}
	if goal != "" {
		e.SubmitDefault(goal)
	}
	e.Run(ticks)
	return e
}

// runCapabilityPrimitiveSubAgentsPlant builds a reactive engine with BOTH subconscious.capability AND
// subconscious.capability_specialists ON — the GAP 5-DEEPER PART 2 SPECIALIST-firing ENTRY (01-subconscious
// §3.3): the producing Capability OWNS the dispatch loop's per-specialist admission (Capability.
// AdmitPrimitiveSubAgent — the §3.3a Scope domain band) instead of the bare relevance gate firing every over-θ
// specialist.
//
// WHY THE PLANT IS BOUNDED — the admission is a DENY-ONLY filter LAYERED on top of the relevance gate. A
// specialist is admitted iff (eff>theta AND the Capability's Scope domain band allows its domain), so the
// fired set is a SUBSET of the bare-relevance fired set — the entry fires a specialist no MORE often, never
// more. The episode Scope is general (engine.episodeScope sets domain ""), so on the live episode path the
// admission set is byte-IDENTICAL to the bare firing (every domain admitted); a domain-banded Capability
// can only REDUCE it. So excitation n (fork-depth from candidate count), the dispatch fan-out, the per-tick
// call count, schedulability U, μ, and K·g are unchanged-or-REDUCED vs the capability-ON plant. The gate is
// applied IDENTICALLY at all three admission sites (the serial loop + both concurrency pre-fires, the D1
// seams), so the default-ON parallel path stays byte-identical to serial under the gate. This encodes the
// specialist-firing-entry plant as a STANDING failable cell so the control-theory gate re-passes
// automatically rather than being hand-derived — the formal continuous-mode-operator gate is REQUIRED before
// any flag-flip (do NOT self-certify).
func runCapabilityPrimitiveSubAgentsPlant(goal string, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.MaxTicks = ticks
	feat := config.New() // AllOn baseline
	feat.Subconscious.Capability = true
	feat.Subconscious.CapabilityPrimitiveSubAgents = true
	feat.Validate()
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runCapabilityPrimitiveSubAgentsPlant: NewEngine on the test double failed: " + err.Error())
	}
	if goal != "" {
		e.SubmitDefault(goal)
	}
	e.Run(ticks)
	return e
}

// runSparseDispatchPlant builds a reactive engine with subconscious.dispatch.sparse ON — the SPARSEMAX
// admission over the base-specialist relevance field (docs/internal/2026-06-21-attention-mechanisms-
// litreview.md §4): the dispatch loop admits a base specialist iff its sparsemax mass p_i>0 AND eff>theta
// (θ surviving as a floor under the induced τ), instead of the per-key absolute eff>theta gate.
//
// WHY THE PLANT STAYS BOUNDED — sparsemax is a COMPETITIVE NARROWING layered on the θ floor, NOT a widening:
// the θ floor still gates admission (eff>theta is preserved), and the sparsemax can only ZERO additional
// specialists the absolute gate would have admitted (τ >= 0 over a field that sums to 1 with the dominant
// peers, so a weak-but-over-θ specialist is dropped, never an extra one added). So the per-tick FIRED set
// is a SUBSET-or-equal of the absolute-gate set ⇒ the candidate count entering the conscious fork is
// unchanged-or-REDUCED ⇒ the fork-depth excitation n is unchanged-or-REDUCED, and the dispatch fan-out /
// per-tick call count / schedulability U / μ are likewise unchanged-or-reduced. The conscious focus is
// UNTOUCHED (hard argmax — sparsemax is the subconscious pull only, never branch selection), so the
// branching plant the durability law governs is not widened. This encodes the sparse-dispatch plant as a
// STANDING failable cell so the control-theory gate re-passes automatically rather than being hand-derived
// — the formal continuous-mode-operator gate is REQUIRED before any flag-flip (do NOT self-certify).
func runSparseDispatchPlant(goal string, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.MaxTicks = ticks
	feat := config.New() // AllOn baseline
	feat.Subconscious.SparseDispatch = true
	feat.Validate()
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runSparseDispatchPlant: NewEngine on the test double failed: " + err.Error())
	}
	if goal != "" {
		e.SubmitDefault(goal)
	}
	e.Run(ticks)
	return e
}

// runSparseDispatchAwakePlant builds the AWAKE flag-ON plant WITH sparse dispatch added — the standing
// suite's continuous-mode cell for the sparse admission. The awake stack is the one with the most active
// faculties (the heaviest per-tick dispatch field), so it is where sparsemax most changes which/how-many
// fire; gating it under continuous mode confirms n<1 / U<=1 / bounded fan-out / μ>0 still hold over a
// standing awake run with the competitive admission engaged.
func runSparseDispatchAwakePlant(seed, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = seed
	feat := awakeFlagOnFeatures()
	feat.Subconscious.SparseDispatch = true
	feat.Validate()
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runSparseDispatchAwakePlant: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(ticks)
	return e
}

// awakeFlagOnFeatures builds the FLAG-ON awake plant the standing suite must cover: the awake stack
// (forest + seed_intents@full-portfolio + drive_agenda + soft@β=0.5) PLUS the three knobs the prior
// continuous-mode-operator gate validated as a NEW plant — the FLAT FAIR-SHARE faculty scheduler
// (conscious.activity.faculty_scheduler), serial attention (attention_width=1), and the Validative
// faculty's standing RPIV capability (conscious.activity.rpiv). This is the exact config the gate that
// landed faculty_scheduler (3216271) + the Validation faculty + RPIV (5a765dd) measured durable; it
// mirrors engine.rpivFeatures so the suite cell is apples-to-apples with the cognition tests. The
// standing suite previously tested only the flag-OFF plant — this makes the flag-ON plant's durability
// a STANDING, failable cell (re-MEASURED on every change), not a hand-derived one-off.
func awakeFlagOnFeatures() *config.HarnessConfig {
	feat := config.New() // AllOn baseline (every experimental faculty OFF)
	a := &feat.Conscious.Activity
	a.Forest = true                                   // per-branch goal rerank — the forest itself
	a.SeedIntents = true                              // standing forest roots
	a.SeedIntentCount = cognition.SeedPortfolioSize() // full portfolio — keeps all faculties alive (incl. Validative)
	a.DriveAgenda = true                              // self-directed drive goals (conscience-gated)
	a.Soft = true                                     // the softmax activity policy (the dominant n-excitation knob)
	a.BranchPropensity = 0.5                          // durability dial — restores n-headroom under Soft (B4)
	a.FacultyScheduler = true                         // the flat fair-share faculty attention scheduler (3216271)
	a.AttentionWidth = 1                              // serial (the round-robin degenerate case)
	a.RPIV = true                                     // the Validative faculty's standing RPIV capability (5a765dd)
	feat.Validate()
	return feat
}

// runAwakeFlagOn builds a continuous-mode engine on the offline TestBackend double with the flag-ON
// awake plant and runs it for ticks. seed is explicit so the suite cell + the standing guard pin the
// same deterministic stream (no clock, no unseeded RNG). A construction failure is impossible on the
// test double (no model to resolve), so the error is dropped after a panic guard (a wiring regression
// surfaces loudly, never a nil-engine deref).
func runAwakeFlagOn(seed, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = seed
	cfg.Features = awakeFlagOnFeatures()
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runAwakeFlagOn: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(ticks)
	return e
}

// runRefineLoopPlant builds a continuous-mode engine on the offline TestBackend double with the awake
// stack AND convert.refine_loop ON — the GAP-11 uniform per-registry refine loop plant. The refine pass
// runs at idle/asleep consolidation (off the hot path) and is SIGNAL-ONLY: it measures the minted-
// specialist registry's entries against the registry's measuring-stick reference and surfaces an
// improve/keep/prune SIGNAL on the bus, but synthesises NO operators/programs/sub-agents and triggers NO
// fan-out. So the plant's control dimensions (n fork-depth, U schedulability, fan-out width, μ baseline,
// K·g) are unchanged vs the flag-OFF awake plant. This encodes that as a STANDING failable cell so the
// control-theory gate re-passes automatically on the refine-loop plant rather than being hand-derived.
// seed=7 (the deterministic seed the awake guards pin).
func runRefineLoopPlant(seed, ticks int) *engine.Engine {
	feat := awakeFlagOnFeatures() // the same flag-ON awake stack the gate already validates
	feat.Convert.RefineLoop = true
	feat.Validate()
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = seed
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runRefineLoopPlant: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(ticks)
	return e
}

// runAllRedesignFlagsOnPlant builds a reactive engine on the offline TestBackend double with ALL FOUR
// cognition-redesign live-path flags ON TOGETHER — subconscious.capability + subconscious.capability_dispatch
// + convert.skill_reframe + convert.refine_loop — the FULL fully-activated redesign plant. No per-flag cell
// tests this COMBINATION; this is the endgame all-flags-on durability gate.
//
// What each flag adds to the plant, and why the combination is still bounded:
//   - capability ON: the producing Capability sources a Scope CEILING (ScopedToolScope can only REDUCE the
//     tool set, never add) and staffs the worker with the rich Context. Control dims (n, U, fan-out, calls)
//     are unchanged-or-REDUCED vs flag-OFF (proven by the standing capability cell).
//   - capability_dispatch ON: the Capability OWNS the per-tick recognition with the PERMISSIVE has-any
//     predicate (gradedRelevance>0), which EQUALS the binary has-any set (theta is not consulted —
//     θ-gating recognition is the refuted double-gate) — so a recognised phase fires exactly as often as
//     the binary path. n / U / fan-out unchanged (proven by the standing dispatch cell).
//   - skill_reframe ON: retires the Skill's own goal-self-match; the Capability recovers recall via
//     recallReframed, which MINTS a per-skill operator into the persistent catalog on a reframed-skill HIT.
//     Catalog growth is excitation-NEUTRAL (D3): minting grows the vocabulary dimension but NOT the per-tick
//     branching (firing is governed by program width, not catalog size). The recall produces a single-phase
//     bespoke program (depth-bounded, the durability guard rejects a degenerate prompt at Verify).
//   - refine_loop ON: a SIGNAL-ONLY idle/asleep consolidation pass (improve/keep/prune signal); synthesises
//     NOTHING, triggers NO fan-out. Control dims unchanged (proven by the standing refine-loop cell).
//
// The COMBINATION claim: the four deltas are mutually independent on the control dimensions (each is
// neutral-or-reducing on n/U/fan-out/μ/K·g), so their union is too. This cell MEASURES that — the gate
// re-passes on the full plant rather than being inferred from the per-flag cells. The goal STAFFS A WORKER
// (a design-and-validate goal that asks for a write ⇒ mutate admitted) and exercises the reframed-recall
// catalog-mint path when a reframed skill is seeded. seed pinned by DefaultConfig (deterministic stream).
func runAllRedesignFlagsOnPlant(goal string, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.MaxTicks = ticks
	feat := config.New()                        // AllOn baseline (every experimental faculty OFF)
	feat.Subconscious.Capability = true         // the producing Capability + Scope ceiling + rich staffing
	feat.Subconscious.CapabilityDispatch = true // the Capability owns the permissive has-any recognition entry
	feat.Convert.SkillReframe = true            // reframe: prompt-body skills + Capability-owned recall (mints a per-skill op)
	feat.Convert.RefineLoop = true              // the per-registry improve/keep/prune signal (off the hot path)
	feat.Validate()
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runAllRedesignFlagsOnPlant: NewEngine on the test double failed: " + err.Error())
	}
	if goal != "" {
		e.SubmitDefault(goal)
	}
	e.Run(ticks)
	return e
}

// parallelPhasesWorkload is the goal whose recognised workflow contains a PARALLEL phase-group
// (par(compare||contrast) for "Compare A versus B") — the exact shape the per-phase concurrency seam
// (07-OPTIMISATION-SURVEY.md §A.1, seam #1) parallelises. The standing suite already runs this goal in the
// SERIAL plant ("parallel (compare||contrast)" row); the parallel-phases cell re-runs it with the
// concurrency flag forced ON so the speed-up's PLANT is durability-gated, not just the serial one.
const parallelPhasesWorkload = "Compare REST versus GraphQL for our API"

// runParallelPhasesPlant builds a reactive engine on the offline TestBackend double, forces the per-phase
// concurrency flag ON for the duration of the run via the in-process hook (the flag is resolved once from
// env at init, so a stability cell must toggle it in-process to exercise the concurrent path), runs the
// PARALLEL-phase-group workload, and returns the finished engine. Determinism is preserved because the
// concurrency seam is RNG-free + buffered + index-ordered (byte-identical to serial); the flag changes
// wall-clock, not the plant — so the measured metrics MUST match the serial "parallel (compare||contrast)"
// row, and the five durability conditions MUST still hold. This is the cell that makes the speed-up's plant
// a STANDING, failable durability check (concurrency raises λ̂/U toward MAX_PAR_WIDTH; n is fork DEPTH, not
// fan-out width, so it is unaffected — the §2.1 control-theory gate re-passes on the concurrent plant).
func runParallelPhasesPlant(ticks int) *engine.Engine {
	restore := subconscious.SetParallelPhasesForTest(true)
	defer restore()
	return run(parallelPhasesWorkload, "reactive", ticks)
}

// specialistFanoutWorkload is the goal whose per-tick base-specialist fire-set is MULTI-SPECIALIST: a
// refactor-safety review lights BOTH model-call stance roles (skeptic + advocate) on the same tick — the
// exact "fire on relevance" fan-out the seam-#2 concurrency parallelises (07-OPTIMISATION-SURVEY.md §A.1
// item 3). The standing suite already runs review-shaped goals serially; this cell re-runs one with the
// concurrency flag forced ON so the speed-up's PLANT (the concurrent base-specialist fan-out) is
// durability-gated, not just the serial one.
const specialistFanoutWorkload = "Is this refactor safe to ship, weighing both sides?"

// runPrimitiveSubAgentFanoutPlant builds a reactive engine on the offline TestBackend double, forces the
// concurrency flag ON for the run via the in-process hook (the flag is resolved once from env at init, so a
// stability cell must toggle it in-process), runs the MULTI-SPECIALIST workload, and returns the finished
// engine. Determinism is preserved because the seam-#2 fan-out is RNG-free + bus-silent-in-Fire +
// index-ordered (byte-identical to serial); the flag changes wall-clock, not the plant — so the measured
// metrics MUST match the serial review-shaped row, and the five durability conditions MUST still hold.
// Concurrency raises λ̂/U toward MAX_PAR_WIDTH (more model calls overlap per tick); n is fork DEPTH, not the
// per-tick fire-set width, so it is unaffected — the §2.1 control-theory gate re-passes on the concurrent
// base-specialist plant exactly as it does for the seam-#1 Par plant.
func runPrimitiveSubAgentFanoutPlant(ticks int) *engine.Engine {
	restore := subconscious.SetParallelPhasesForTest(true)
	defer restore()
	return run(specialistFanoutWorkload, "reactive", ticks)
}

// FacultySchedulerBoundHolds is the STANDING keepStashed-vs-faculty-count guard — the structural bound
// the flag-ON faculty scheduler depends on, encoded so a future faculty addition can never silently
// break the U≤1 schedule. The flat fair-share scheduler can only give a faculty a turn if a live root
// for it survives the U≤1 prune cap; pruneBranches protects at most keepStashed = FocusCapacity-2 seed
// roots (one per faculty). So the invariant is: keepStashed >= SeedFacultyCount — every faculty fits a
// protected slot under the cap. Today FocusCapacity=8 ⇒ keepStashed=6 == SeedFacultyCount=6 (the exact
// boundary). Adding a 7TH faculty without raising FocusCapacity would make SeedFacultyCount=7 >
// keepStashed=6, so the new faculty's root would be dropped before arbitration AND each newly-protected
// root raises the U ceiling 8→9 (U>1, unschedulable). The carry-forward (the failure message): a 7th
// faculty needs FocusCapacity→9 + a durability re-gate. Metrics stays nil (a structural report — no
// engine, no run; like StructuralBoundHolds).
func FacultySchedulerBoundHolds() Report {
	// FocusCapacity is the regulator's schedulable branch budget; the prune cap reserves two slots (the
	// ACTIVE branch + one a BRANCH may add this tick) — the SAME arithmetic engine.pruneBranches uses.
	rcfg := regulator.DefaultConfig()
	focusCapacity := rcfg.FocusCapacity
	keepStashed := focusCapacity - 2
	if keepStashed < 1 {
		keepStashed = 1
	}
	faculties := cognition.SeedFacultyCount()

	rep := Report{Workload: "faculty-scheduler prune-protection bound (keepStashed >= faculties)"}
	rep.Checks = append(rep.Checks, Check{
		Name: fmt.Sprintf("keepStashed (FocusCapacity-2 = %d) >= SeedFacultyCount (%d)", keepStashed, faculties),
		OK:   keepStashed >= faculties,
		Detail: fmt.Sprintf(
			"a %dth faculty was added without raising FocusCapacity: keepStashed=%d < faculties=%d, so the "+
				"new faculty's seed root is pruned before arbitration AND protecting it raises the U ceiling "+
				"%d→%d (U>1, unschedulable). CARRY-FORWARD: bump FocusCapacity to %d (keepStashed→%d) and "+
				"re-run the continuous-mode-operator durability gate (a new faculty changes the plant).",
			faculties, keepStashed, faculties, focusCapacity, focusCapacity+1, faculties+2, faculties),
	})
	return rep
}

// StructuralBoundHolds is the structural guarantee: VerifyProgram REJECTS a fan-out wider than the
// durable bound and ACCEPTS one at the bound. Mirrors Python structural_bound_holds(). Metrics stays
// nil (Python's empty dict ⇒ no metrics line).
func StructuralBoundHolds() Report {
	cat := cognition.NewOperatorRegistry()
	tooWide := cognition.Program{
		Root:        cognition.NewPar(hypothesizeSteps(cognition.MaxParWidth + 1)...),
		Synthesized: true,
	}
	okWide := cognition.Program{
		Root:        cognition.NewSeq(cognition.NewPar(hypothesizeSteps(cognition.MaxParWidth)...)),
		Synthesized: true,
	}
	tooWideOK, _ := cognition.VerifyProgram(tooWide, cat)
	okWideOK, _ := cognition.VerifyProgram(okWide, cat)
	rejected := !tooWideOK
	accepted := okWideOK

	rep := Report{Workload: "structural fan-out bound (verify_program)"}
	rep.Checks = append(rep.Checks, Check{
		Name:   fmt.Sprintf("reject width %d", cognition.MaxParWidth+1),
		OK:     rejected,
		Detail: "over-wide Par was accepted",
	})
	rep.Checks = append(rep.Checks, Check{
		Name:   fmt.Sprintf("accept width %d", cognition.MaxParWidth),
		OK:     accepted,
		Detail: "max-width Par was rejected",
	})
	return rep
}

// SkillCompositionBounded is the skill layer's durability obligation: sub-skill expansion is bounded
// + acyclic, so the executed artifact is always a bounded operator Program (durability already proven
// for those). Mirrors Python skill_composition_bounded():
//   - every seed skill expands within bounds to a VERIFIED operator program;
//   - the expanded depth stays within MaxDepth;
//   - a self-referential skill is rejected at mint (no unbounded recursion can enter the library).
//
// The cyclic-rejection step differs only in mechanism: Python injects a self-calling skill directly
// into the private map then mints a second skill that calls it; the Go SkillRegistry has no public
// map insertion, so the equivalent guard is exercised through the public Mint — minting a skill whose
// body calls ITSELF, which Expand rejects as a cycle at mint time (the same durability obligation,
// the same code path). Metrics is an explicit all-zero (non-nil) value, matching Python's explicit
// zero dict (so the metrics line renders, unlike the structural report).
func SkillCompositionBounded() Report {
	rep := Report{Workload: "skill composition (bounded + acyclic expansion)"}
	lib := cognition.NewSkillRegistry(true)
	cat := cognition.NewOperatorRegistry()

	// every seed skill expands within bounds and to a VERIFIED operator program.
	allOK := true
	deep := 0
	for _, name := range lib.Names() {
		sk, ok := lib.Get(name)
		if !ok {
			allOK = false
			continue
		}
		prog, err := lib.Expand(sk)
		if err != nil {
			allOK = false
			continue
		}
		ok, _ = cognition.VerifyProgram(prog, cat)
		allOK = allOK && ok
		if d := depthOf(prog.Root); d > deep {
			deep = d
		}
	}
	rep.Checks = append(rep.Checks, Check{
		Name:   "seed skills expand to verified bounded programs",
		OK:     allOK,
		Detail: "a seed skill failed to expand/verify",
	})
	rep.Checks = append(rep.Checks, Check{
		Name:   fmt.Sprintf("expanded depth <= MAX_DEPTH (%d)", cognition.MaxDepth),
		OK:     deep <= cognition.MaxDepth,
		Detail: fmt.Sprintf("expansion produced depth %d", deep),
	})

	// a self-referential skill is rejected by mint (no unbounded recursion can enter the library): a
	// skill whose body calls itself is rejected by Expand's cycle-check at mint time.
	cyclicBody := cognition.Program{
		Root:        cognition.NewSeq(cognition.SkillStep("loopy", "general", "")),
		Synthesized: true,
	}
	_, minted := lib.Mint("loopy", []string{"x"}, cyclicBody, "composite", "")
	rejected := !minted
	rep.Checks = append(rep.Checks, Check{
		Name:   "cyclic sub-skill rejected at mint",
		OK:     rejected,
		Detail: "a cycle entered the library",
	})

	rep.Metrics = &Metrics{} // explicit zero metrics (Python's explicit zero dict)
	return rep
}

// RunSuite is the standing stability suite: representative dynamic workloads + the structural bounds.
// Mirrors Python run_suite() — the same workloads in the same order.
func RunSuite() []Report {
	return []Report{
		CheckEngineReactive(runReactive("Design a small API for a todo service"), "series (design-build-validate)"),
		CheckEngineReactive(runReactive("Compare REST versus GraphQL for our API"), "parallel (compare||contrast)"),
		CheckEngineReactive(runReactive("Optimize the checkout flow to be faster"), "loop (measure>eliminate)"),
		CheckEngineReactive(runReactive("Why is the build failing intermittently?"), "skill+sub-skill (diagnose)"),
		CheckEngineReactive(runReactive("What is 6 times 7?"), "simple Q&A (no workflow)"),
		CheckEngine(run("", "continuous", 24), "continuous (awake baseline)", "continuous"),
		// The FLAG-ON awake plant (faculty_scheduler + attention_width=1 + rpiv, over the awake stack) — the
		// new plant the faculty-scheduler (3216271) + Validation-faculty/RPIV (5a765dd) commits introduced.
		// The standing suite previously tested only the flag-OFF plant, so this plant's durability was
		// hand-derived by the continuous-mode-operator gate each time; this encodes it as a STANDING failable
		// cell (re-MEASURED on every change). seed=7 (the deterministic seed the awake guards pin).
		CheckEngine(runAwakeFlagOn(7, 24), "awake flag-ON (faculty_scheduler+rpiv)", "continuous"),
		// The AUTONOMOUS-SENSE plant (#18 self-watching cell, red-team amendment-5, conscious.activity
		// .autonomous_sense): the same flag-ON awake stack PLUS autonomous standing-intent sensing — a
		// standing perceptual/introspective seed root fires ONE bounded sensor read on its own when it
		// holds focus. The sense is a single percept APPEND (a μ-baseline immigrant), never a fork — so n
		// (fork-ratio), U (schedulability), and fan-out are UNCHANGED vs the flag-OFF awake plant, μ>0 is
		// preserved (the percept ADDS baseline), and K·g is untouched (sensing injects a percept, it does
		// not actuate θ). This makes the autonomous-sense plant a STANDING failable durability cell so the
		// control-theory gate re-passes automatically on it — #19's hard constraint (n<1 with the flag on)
		// is MEASURED here, never self-certified.
		AutonomousSenseDurabilityHolds(),
		// The SELF-MODEL plant (#18 self-watch, extended; sense.self_model): the same flag-ON awake stack
		// PLUS the baseline declarative self-model — a standing INTROSPECTIVE seed root grounds the STANDING
		// CORE self-model on its own when it holds focus, refreshed on a content-hash change. The grounding is
		// a single percept APPEND (a μ-baseline immigrant), never a fork — so n, U, and fan-out are UNCHANGED
		// vs the flag-OFF awake plant, μ>0 is preserved (the core ADDS baseline), and K·g is untouched. This
		// makes the self-model plant a STANDING failable durability cell so the control-theory gate re-passes
		// automatically on it — the n<1 self-knowledge claim is MEASURED here, never self-certified.
		SelfModelDurabilityHolds(),
		// The AWAKE-USER-ENGAGE plant (AWAKE-DISP rung 1, conscious.activity.awake_user_engage): the same
		// flag-ON awake stack PLUS the rung-1 engagement VALUE FLOOR — a focused unresolved user line's V(s)
		// carries an additive boost so it out-competes the endogenous wander and wins the produce-competition.
		// The boost re-orders the frontier (a value comparison); it spawns no operator/sub-agent/branch and
		// opens no new branch, so n (fork-ratio), U (schedulability), and fan-out are UNCHANGED vs the flag-OFF
		// awake plant, mu>0 is preserved (the boost RELEASES when the user line resolves, so wander resumes),
		// and K·g is untouched (the floor re-orders V(s), it does not actuate theta). This makes the rung-1
		// plant a STANDING failable durability cell so the control-theory gate re-passes automatically — the
		// formal continuous-mode-operator gate is REQUIRED before any flag-flip (do NOT self-certify).
		AwakeUserEngageDurabilityHolds(),
		// The PARALLEL-PHASES plant (07-OPTIMISATION-SURVEY.md §A.1, seam #1): the SAME compare||contrast
		// workload as the serial row above, re-run with the per-phase concurrency flag forced ON. The seam
		// is RNG-free + buffered + index-ordered (byte-identical to serial), so the flag changes wall-clock,
		// not the plant — concurrency raises λ̂/U toward MAX_PAR_WIDTH but n (fork DEPTH) is unaffected. The
		// standing suite previously gated only the serial plant; this makes the speed-up's plant a STANDING
		// failable durability cell so the control-theory gate re-passes automatically on the concurrent path.
		CheckEngineReactive(runParallelPhasesPlant(24), "parallel-phases ON (compare||contrast concurrent)"),
		// The SEAM-#2 plant (07-OPTIMISATION-SURVEY.md §A.1 item 3): a MULTI-SPECIALIST tick (refactor-safety
		// review fires skeptic+advocate) re-run with the concurrency flag forced ON, so the per-tick base-
		// specialist model-call fan-out runs concurrently. The fan-out is RNG-free + bus-silent-in-Fire +
		// index-ordered (byte-identical to serial), so the flag changes wall-clock, not the plant — concurrency
		// raises λ̂/U toward MAX_PAR_WIDTH but n (fork DEPTH, not fire-set width) is unaffected. This makes the
		// seam-#2 speed-up's plant a STANDING failable durability cell so the control-theory gate re-passes
		// automatically on the concurrent base-specialist path.
		CheckEngineReactive(runPrimitiveSubAgentFanoutPlant(24), "specialist-fanout ON (skeptic||advocate concurrent)"),
		// The CAPABILITY plant (01-subconscious §3.3/§3.3a, subconscious.capability): the subconscious
		// object-model rewire — the producing Capability sources a Scope ceiling, captures a rich Context,
		// and staffs every worker with both (rich-context staffing + category-scoped tool picks). It changes
		// WHAT a worker reads and CONSTRAINS its tool reach (ScopedToolScope can only reduce the tool set,
		// never add), so the plant's control dimensions (n, U, fan-out, call count) are unchanged or reduced
		// vs flag-OFF. The standing suite previously gated only the flag-OFF plant; this makes the capability
		// plant a STANDING failable durability cell so the control-theory gate re-passes automatically.
		CheckEngineReactive(runCapabilityPlant("Design and validate a small API for a todo service", 24),
			"capability ON (Scope ceiling + rich-context staffing)"),
		// The CAPABILITY-DISPATCH plant (01-subconscious §3.3, GAP 5-DEEPER, subconscious.capability_dispatch):
		// the producing Capability is the LIVE relevance/dispatch ENTRY — it OWNS the dispatch loop's per-tick
		// workflow-recognition instead of the Workflow self-triggering. The predicate is the PERMISSIVE has-any
		// `gradedRelevance(stream)>0`, the SAME relevance criterion as the binary has-any match (theta is NOT
		// consulted at recognition — θ-gating recognition is the refuted double-gate) — so the recognition set
		// EQUALS the binary set and the phase fan-out is unchanged vs the capability-ON plant (n, U, fan-out,
		// K·g, μ untouched; bespoke short-circuit preserved). The standing suite gates the dispatch-entry plant
		// so the control-theory gate re-passes automatically — the formal continuous-mode-operator gate is
		// REQUIRED before any flag-flip.
		CheckEngineReactive(runCapabilityDispatchPlant("Design and validate a small API for a todo service", 24),
			"capability-dispatch ON (has-any relevance entry)"),
		// The CAPABILITY-SPECIALISTS plant (01-subconscious §3.3, GAP 5-DEEPER PART 2, subconscious.
		// capability_specialists): the producing Capability is the LIVE SPECIALIST-firing ENTRY — it OWNS the
		// dispatch loop's per-specialist admission (the §3.3a Scope domain band) instead of the bare relevance
		// gate firing every over-θ specialist. The gate is a DENY-ONLY filter layered on the relevance gate, so
		// the fired set is a SUBSET of the bare-relevance set (the episode general-Scope path is byte-identical;
		// a domain-banded Capability can only REDUCE firing). So n / U / fan-out / call count / μ / K·g are
		// unchanged-or-REDUCED vs the capability-ON plant, applied identically at all three admission sites
		// (the serial loop + both D1 concurrency pre-fires). The standing suite gates the specialist-firing
		// entry plant so the control-theory gate re-passes automatically — the formal continuous-mode-operator
		// gate is REQUIRED before any flag-flip.
		CheckEngineReactive(runCapabilityPrimitiveSubAgentsPlant("Design and validate a small API for a todo service", 24),
			"capability-specialists ON (Capability owns specialist firing, §3.3a domain band)"),
		// The SPARSE-DISPATCH plant (docs/internal/notes/2026-06-21-attention-mechanisms-litreview.md §4,
		// subconscious.dispatch.sparse): the dispatch loop admits base specialists by SPARSEMAX over the
		// relevance field (admit iff p_i>0 AND eff>theta — θ as a floor under the induced τ) instead of the
		// per-key absolute eff>theta gate. The sparsemax is a COMPETITIVE NARROWING layered on the θ floor:
		// the fired set is a SUBSET-or-equal of the absolute-gate set (a weak-but-over-θ specialist is
		// dropped when strong peers dominate the simplex, never an extra one added), so the candidate count
		// entering the conscious fork — and thus the fork-depth excitation n, the dispatch fan-out, the
		// per-tick call count, U, μ — is unchanged-or-REDUCED. The conscious focus is UNTOUCHED (hard argmax;
		// sparsemax is the subconscious pull only). Reactive + awake cells gate the plant so the control-
		// theory gate re-passes automatically — the formal continuous-mode-operator gate is REQUIRED before
		// any flag-flip.
		CheckEngineReactive(runSparseDispatchPlant("Design and validate a small API for a todo service", 24),
			"sparse-dispatch ON (sparsemax admission over the specialist field)"),
		CheckEngine(runSparseDispatchAwakePlant(7, 24), "sparse-dispatch ON awake (sparsemax over the awake faculty field)", "continuous"),
		// The REFINE-LOOP plant (01-subconscious §3.17/§3.20, convert.refine_loop — GAP 11): the awake stack
		// PLUS the uniform per-registry refine loop. The refine pass runs at idle/asleep consolidation (off the
		// hot path) and is SIGNAL-ONLY — it measures the minted registry's entries against its measuring-stick
		// reference and surfaces an improve/keep/prune SIGNAL, but synthesises NOTHING and triggers NO fan-out.
		// So the plant's control dimensions (n, U, fan-out, μ, K·g) are unchanged vs the flag-OFF awake plant.
		// This makes the refine-loop plant a STANDING failable durability cell so the control-theory gate re-
		// passes automatically rather than being hand-derived.
		CheckEngine(runRefineLoopPlant(7, 24), "refine-loop ON (per-registry improve/keep/prune signal)", "continuous"),
		// The FULLY-ACTIVATED REDESIGN plant (the endgame all-flags-on gate): ALL FOUR live-path redesign
		// flags ON TOGETHER — subconscious.capability + subconscious.capability_dispatch + convert.skill_reframe
		// + convert.refine_loop. NO per-flag cell tests this COMBINATION; the four deltas are each neutral-or-
		// reducing on the control dimensions (n fork-depth, U schedulability, fan-out width, μ, K·g) and mutually
		// independent, so their union is too — but that is a CLAIM the gate must MEASURE, not infer. This makes
		// the full-plant durability a STANDING failable cell that re-passes on every change. The goal staffs a
		// worker (design-and-validate asks for a write ⇒ mutate admitted) and exercises the reframed-recall
		// catalog-mint path. This is the durability half of the flag-flip product decision.
		CheckEngineReactive(runAllRedesignFlagsOnPlant("Design and validate a small API for a todo service", 24),
			"ALL redesign flags ON (capability+dispatch+reframe+refine_loop)"),
		FacultySchedulerBoundHolds(),
		StructuralBoundHolds(),
		SkillCompositionBounded(),
		// The SLAM M5 CONSISTENCY / OBSERVABILITY INVARIANT (Track F / M5, design §5 #7 + §5b): a SIXTH
		// durability obligation distinct from the five control-theoretic conditions — the self-state
		// estimator gains NO spurious information in unobservable directions (the Huang-2010 EKF-inconsistency
		// overconfidence that compounds over a long awake run). REQUIRED-WITH-M1 before any awake go-live. The
		// estimator + monitor are pure CONTROL (no model, no fan-out, no theta actuation, a pure witness), so
		// the M1+M5 plant's control dimensions are identical to the flag-OFF awake plant — folded in here so
		// the gate re-passes on the M1+M5 plant rather than being hand-derived.
		ConsistencyInvariantHolds(),
	}
}

// Main runs the suite and prints the report, returning the process exit code (Python main()). It is
// the only function in this package that does I/O — the engine itself never prints; it emits.
func Main() int {
	reports := RunSuite()
	// Python print("=== ... ===\n") emits the header line plus a blank line; reproduce that exact
	// two-newline output with a single Print (Println would add a third newline; vet flags it too).
	fmt.Print("=== Stability validation: durability under dynamic synthesis ===\n\n")
	for _, r := range reports {
		fmt.Println(r.Format())
	}
	nOK := 0
	for _, r := range reports {
		if r.OK() {
			nOK++
		}
	}
	fmt.Printf("\n%d/%d workloads hold the durable regime.\n", nOK, len(reports))
	if nOK == len(reports) {
		return 0
	}
	return 1
}

// -- helpers ----------------------------------------------------------------- //

// hypothesizeSteps builds n "hypothesize"/"general" Steps (Python's [step("hypothesize","general")
// for _ in range(n)]).
func hypothesizeSteps(n int) []cognition.Node {
	out := make([]cognition.Node, n)
	for i := range out {
		out[i] = cognition.NewStep("hypothesize", "general", "")
	}
	return out
}

// depthOf is the nesting depth of a program tree (Python _depth_of): a Step is 1; a Seq/Par is 1 +
// the max child depth (0 for empty); a Loop is 1 + its body's depth. The concrete Node types are
// exported, so this is a type switch over the closed set (mirrors cognition's private depthOf, which
// is not exported across the package boundary).
func depthOf(n cognition.Node) int {
	switch v := n.(type) {
	case cognition.Step:
		return 1
	case cognition.Seq:
		return 1 + maxChildDepth(v.Children)
	case cognition.Par:
		return 1 + maxChildDepth(v.Children)
	case cognition.Loop:
		return 1 + depthOf(v.Body)
	}
	return 1
}

// maxChildDepth is max(depthOf(c) for c in children, default 0).
func maxChildDepth(children []cognition.Node) int {
	best := 0
	for _, c := range children {
		if d := depthOf(c); d > best {
			best = d
		}
	}
	return best
}

// dataF reads a numeric event-data value as float64 (the regulator emits round3 float64s). Absent or
// non-numeric → 0.0 (Python's r.get(key, 0.0) over float-only wire values).
func dataF(d map[string]any, key string) float64 {
	switch v := d[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0.0
}

// maxF is max over two float64s.
func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// maxFanout is max(fan.values(), default=0) over the per-tick sub-agent counts.
func maxFanout(fan map[int]int) int {
	best := 0
	for _, v := range fan {
		if v > best {
			best = v
		}
	}
	return best
}

// regCheckValue renders a regulator check's value for the failure detail: the NA string for an NA
// entry (the reactive mu>0 entry's legacy string, or the C0a K·g saturated/open-loop detail), else
// the bool (True/False).
func regCheckValue(c regulator.Check) string {
	if c.NA {
		if c.NADetail != "" {
			return c.NADetail
		}
		return "N/A reactive (self-terminating)"
	}
	if c.Pass {
		return "True"
	}
	return "False"
}

// -- #18: the AUTONOMOUS-SENSE self-watching durability cell (red-team amendment-5) ------------------ //
//
// #19 wires a NEW capability into the awake plant: a standing PERCEPTUAL/INTROSPECTIVE seed root, when it
// holds focus, fires ONE bounded sensor read ON ITS OWN (conscious.activity.autonomous_sense). This cell
// is the self-watching obligation that capability carries — it MEASURES that the durable regime still
// holds with autonomous sensing ON, so the control-theory gate re-passes automatically on the
// autonomous-sense plant rather than being hand-derived (the red-team amendment-5 self-watching cell).
//
// Why the autonomous sense does NOT break durability (the claim this cell measures):
//   - n (fork-ratio) UNCHANGED: an autonomous sense is a SINGLE percept APPEND (a μ-baseline immigrant),
//     NOT a fork — it spawns no operator/sub-agent/branch (autonomous_sense.go injects exactly one
//     GENERATED thought via appendThought). The branching ratio is forks-per-tick; an append is not a
//     fork, so it adds NO standing excitation source that pushes n→1. This is the load-bearing condition
//     (#19's hard constraint): n<1 with the flag on.
//   - U (schedulability) UNCHANGED: the sense opens no new branch, so the live-branch count (the U load)
//     is unchanged — the per-(branch,tick) guard bounds it to one read per focus (no fan-out).
//   - fan-out UNCHANGED: a single read, never a parallel burst.
//   - μ>0 PRESERVED (and not collapsed): the sense ADDS a percept to the awake baseline; the awake loop
//     keeps μ>0 exactly as the flag-OFF awake plant does.
//   - K·g (loop gain) UNTOUCHED: sensing injects a percept, it does not actuate the regulator's control
//     variable θ — the controller's loop is unchanged.
//
// The plant is the SAME flag-ON awake stack the standing suite already gates (awakeFlagOnFeatures —
// forest + full seed-intent portfolio + faculty scheduler + RPIV) PLUS autonomous_sense ON. That stack's
// full portfolio keeps a perceptual AND an introspective standing root alive, and the faculty scheduler
// gives them fair-share focus turns, so the autonomous sense actually FIRES over the run (the cell
// exercises the live path, not a dead config). seed=7 (the deterministic seed the awake guards pin).

// autonomousSenseFeatures builds the flag-ON awake plant WITH autonomous standing-intent sensing ON. It
// extends awakeFlagOnFeatures (the exact stack the prior continuous-mode-operator gate validated durable)
// by flipping conscious.activity.autonomous_sense — so the cell is apples-to-apples with the standing
// awake-flag-ON row, isolating the autonomous-sense delta.
func autonomousSenseFeatures() *config.HarnessConfig {
	feat := awakeFlagOnFeatures()                  // forest + full seed-intent portfolio + faculty scheduler + RPIV
	feat.Conscious.Activity.AutonomousSense = true // #19: the standing root fires its sensor on its own
	feat.Validate()
	return feat
}

// runAutonomousSensePlant builds a continuous-mode engine on the offline TestBackend double with the
// autonomous-sense awake plant and runs it for ticks. A construction failure is impossible on the test
// double (no model to resolve), so the error is dropped after a panic guard (a wiring regression surfaces
// loudly, never a nil-engine deref). The sensors are seam-blind offline (no clock/web/host wired), so the
// autonomous sense fires its percept on the deterministic self-state/"nothing new" template — the plant's
// CONTROL dimensions (the thing this cell gates) are identical whether or not a seam is wired, because the
// sense is a single bounded percept APPEND regardless.
func runAutonomousSensePlant(seed, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = seed
	cfg.Features = autonomousSenseFeatures()
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runAutonomousSensePlant: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(ticks)
	return e
}

// AutonomousSenseDurabilityHolds is the #18 self-watching cell entry point: it runs the autonomous-sense
// awake plant and checks the five durability conditions (n<1, U≤1, 0<K·g<2 or saturated-bounded, μ>0,
// bounded fan-out) hold over the run — the red-team amendment-5 obligation. It additionally asserts the
// autonomous sense actually FIRED (a perception.sense event was emitted), so the cell can never pass
// vacuously on a dead config (a plant where the sense never fires would trivially be durable but would not
// be MEASURING the autonomous-sense plant). seed=7 (the deterministic awake seed).
func AutonomousSenseDurabilityHolds() Report {
	e := runAutonomousSensePlant(7, 24)
	rep := CheckEngine(e, "autonomous-sense ON (standing-root self-sensing, #18 self-watch)", "continuous")

	// Liveness: the autonomous sense must have FIRED over the run — else the cell is gating a dead config
	// (durable but not the plant under test). A perception.sense event is the witness the sense fired.
	fired := 0
	for _, ev := range e.Bus().Recent(1<<30, nil) {
		if ev.Kind == events.PerceptionSense {
			fired++
		}
	}
	rep.Checks = append(rep.Checks, Check{
		Name:   "autonomous sense fired (perception.sense emitted)",
		OK:     fired > 0,
		Detail: "the autonomous-sense plant never fired a perception.sense — the cell would gate a dead config (the sense was not exercised)",
	})
	return rep
}

// -- SELF-MODEL: the baseline declarative self-model durability cell (extends the #18 self-watch) ------ //
//
// SELF-MODEL wires a NEW capability into the awake plant: a standing INTROSPECTIVE seed root, when it holds
// focus, grounds the STANDING CORE self-model ON ITS OWN (sense.self_model) — refreshed on a content-hash
// change. This cell is the self-watching obligation that capability carries — it MEASURES that the durable
// regime still holds with the self-model ON, so the control-theory gate re-passes automatically on the
// self-model plant rather than being hand-derived (the #18 self-watch obligation, extended).
//
// Why the self-model does NOT break durability (the claim this cell measures) — identical in force to the
// autonomous-sense argument above:
//   - n (fork-ratio) UNCHANGED: the standing core is a SINGLE percept APPEND (a μ-baseline immigrant), NOT
//     a fork — self_model.go injects exactly one GENERATED thought via appendThought, spawning no operator/
//     sub-agent/branch. The branching ratio is forks-per-tick; an append is not a fork, so it adds NO
//     standing excitation source that pushes n→1. This is the load-bearing condition (n<1 with the flag on).
//   - U (schedulability) UNCHANGED: the grounding opens no new branch, so the live-branch count (the U load)
//     is unchanged — and the content-hash refresh bounds it to once per change (no per-tick spam).
//   - fan-out UNCHANGED: a single append, never a parallel burst.
//   - μ>0 PRESERVED: the standing core ADDS a percept to the awake baseline; μ>0 holds exactly as flag-OFF.
//   - K·g (loop gain) UNTOUCHED: grounding injects a percept; it does not actuate the regulator's θ.

// selfModelFeatures builds the flag-ON awake plant WITH the baseline declarative self-model ON. It extends
// awakeFlagOnFeatures (the exact stack the prior continuous-mode-operator gate validated durable) by
// flipping sense.self_model — so the cell is apples-to-apples with the standing awake-flag-ON row,
// isolating the self-model delta. The full seed-intent portfolio keeps an INTROSPECTIVE standing root alive
// and the faculty scheduler gives it fair-share focus turns, so the self-model actually grounds over the
// run (the cell exercises the live path, not a dead config).
func selfModelFeatures() *config.HarnessConfig {
	feat := awakeFlagOnFeatures() // forest + full seed-intent portfolio + faculty scheduler + RPIV
	feat.Sense.SelfModel = true   // SELF-MODEL: the standing introspective root grounds the self-model
	feat.Validate()
	return feat
}

// runSelfModelPlant builds a continuous-mode engine on the offline TestBackend double with the self-model
// awake plant and runs it for ticks. A construction failure is impossible on the test double (no model to
// resolve), so the error is dropped after a panic guard. The plant's CONTROL dimensions (the thing this
// cell gates) are identical whether or not a workspace is wired — the grounding is a single bounded percept
// APPEND regardless of how rich the read registries are.
func runSelfModelPlant(seed, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = seed
	cfg.Features = selfModelFeatures()
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runSelfModelPlant: NewEngine on the test double failed: " + err.Error())
	}
	e.Run(ticks)
	return e
}

// SelfModelDurabilityHolds is the SELF-MODEL self-watching cell entry point: it runs the self-model awake
// plant and checks the durability conditions (n<1, U≤1, 0<K·g<2 or saturated-bounded, μ>0, bounded fan-out)
// hold over the run — the #18 self-watch obligation extended to the self-model. It additionally asserts the
// self-model actually GROUNDED (a perception.self_model event was emitted), so the cell can never pass
// vacuously on a dead config (a plant where the self-model never grounds would trivially be durable but
// would not be MEASURING the self-model plant). seed=7 (the deterministic awake seed).
func SelfModelDurabilityHolds() Report {
	e := runSelfModelPlant(7, 24)
	rep := CheckEngine(e, "self-model ON (standing declarative self-model, #18 self-watch)", "continuous")

	// Liveness: the self-model must have GROUNDED over the run — else the cell is gating a dead config. A
	// perception.self_model event is the witness it grounded.
	grounded := 0
	for _, ev := range e.Bus().Recent(1<<30, nil) {
		if ev.Kind == events.PerceptionSelfModel {
			grounded++
		}
	}
	rep.Checks = append(rep.Checks, Check{
		Name:   "self-model grounded (perception.self_model emitted)",
		OK:     grounded > 0,
		Detail: "the self-model plant never grounded a perception.self_model — the cell would gate a dead config (the self-model was not exercised)",
	})
	return rep
}

// -- AWAKE-USER-ENGAGE: the rung-1 engagement value-floor durability cell ------------------------------ //
//
// AWAKE-DISP rung 1 (conscious.activity.awake_user_engage, docs/internal/2026-06-21-awake-engagement-and-
// dispatch.md) adds a deterministic VALUE FLOOR to the awake plant: a focused, unresolved user line's V(s)
// carries an additive engagement boost (the awake_user_engage_weight knob, conservative default 0.5) so the
// line reliably OUT-COMPETES the endogenous wander and WINS the produce-competition. It is a pure Pattern-A
// value computation — NO model call, no fork, no new operator/sub-agent.
//
// Why the engagement floor does NOT break durability (the claim this cell MEASURES) — the floor only changes
// WHICH line the single conscious focus pursues, not HOW MANY lines exist or how deep they fork:
//   - n (fork-ratio) UNCHANGED: the boost is added to an existing branch's V(s); it spawns no operator, no
//     sub-agent, no branch. It re-orders the frontier (a value comparison), it does not add a forking source
//     that pushes n->1. The user line was ALREADY a live branch (OnInterrupt forked it on the user turn);
//     the floor only makes the single focus resume it over wander. This is the load-bearing condition.
//   - U (schedulability) UNCHANGED: no new branch opens — the live-branch count (the U load) is identical;
//     the floor picks among the SAME branches.
//   - fan-out UNCHANGED: a value re-rank is not a parallel burst.
//   - mu>0 PRESERVED: the floor does not suppress the endogenous baseline — the boost RELEASES the moment the
//     user line resolves (MarkDelivered drops UnresolvedUserInput), so the wander resumes; mu>0 holds. The
//     conservative weight (it wins WHEN PRESENT but does not fully suppress wander) is the design guarantee.
//   - K·g (loop gain) UNTOUCHED: the floor re-orders V(s); it does not actuate the regulator's theta.
// So the rung-1 plant's control dimensions are identical to the flag-OFF awake plant — but that is a CLAIM
// the gate must MEASURE, so this is a STANDING failable cell that re-passes on every change.

// awakeUserEngageFeatures builds the flag-ON awake plant WITH the rung-1 engagement floor ON. It extends
// awakeFlagOnFeatures (the exact stack the prior continuous-mode-operator gate validated durable) by
// flipping conscious.activity.awake_user_engage (+ awake_user_dispatch so the focused user line also gets a
// subconscious workflow, the rung-0 companion the floor sits on top of) — so the cell is apples-to-apples
// with the standing awake-flag-ON row, isolating the engagement-floor delta. The conservative default weight
// (0.5) is left in place (DefaultConsciousActivity sets it), so the cell exercises the shipped configuration.
func awakeUserEngageFeatures() *config.HarnessConfig {
	feat := awakeFlagOnFeatures()                       // forest + full seed-intent portfolio + faculty scheduler + RPIV
	feat.Conscious.Activity.AwakeUserEngage = true      // rung 1: the engagement value floor on the focused user line
	feat.Conscious.Activity.AwakeUserDispatch = true    // rung 0: the focused user line also gets a subconscious workflow
	feat.Conscious.Activity.AwakeUserEngageWeight = 0.5 // the conservative shipped default (explicit for clarity)
	feat.Validate()
	return feat
}

// runAwakeUserEngagePlant builds a continuous-mode engine on the offline TestBackend double with the rung-1
// engagement plant, submits a user input mid-stream (the floor only activates on a focused unresolved user
// line), and runs it for ticks. A construction failure is impossible on the test double, so the error is
// dropped after a panic guard (a wiring regression surfaces loudly, never a nil-engine deref). seed=7 (the
// deterministic awake seed the other awake cells pin).
func runAwakeUserEngagePlant(seed, ticks int) *engine.Engine {
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = seed
	cfg.Features = awakeUserEngageFeatures()
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		panic("stability.runAwakeUserEngagePlant: NewEngine on the test double failed: " + err.Error())
	}
	// Wake + wander for a few ticks, then land a user turn mid-stream so the engagement floor is exercised
	// against a live endogenous wander (the produce-competition the floor is meant to win).
	for i := 0; i < 4; i++ {
		e.Step()
	}
	e.SubmitDefault("ponder slowly: the most elegant way to model a token-bucket rate limiter, and why")
	for i := 4; i < ticks; i++ {
		e.Step()
	}
	return e
}

// AwakeUserEngageDurabilityHolds is the AWAKE-DISP rung-1 self-watching cell entry point: it runs the
// engagement-floor awake plant and checks the durability conditions (n<1, U<=1, 0<K·g<2 or saturated-bounded,
// mu>0, bounded fan-out) hold over the run. It additionally asserts the engagement floor actually FIRED (a
// conscious.engage event was emitted), so the cell can never pass vacuously on a dead config (a plant where
// the floor never engages would trivially be durable but would not be MEASURING the rung-1 plant). seed=7.
func AwakeUserEngageDurabilityHolds() Report {
	e := runAwakeUserEngagePlant(7, 24)
	rep := CheckEngine(e, "awake-user-engage ON (rung-1 engagement value floor)", "continuous")

	// Liveness: the engagement floor must have FIRED over the run — else the cell is gating a dead config
	// (durable but not the plant under test). A conscious.engage event is the witness the floor engaged.
	fired := 0
	for _, ev := range e.Bus().Recent(1<<30, nil) {
		if ev.Kind == events.Engage {
			fired++
		}
	}
	rep.Checks = append(rep.Checks, Check{
		Name:   "engagement floor fired (conscious.engage emitted)",
		OK:     fired > 0,
		Detail: "the rung-1 plant never fired a conscious.engage — the cell would gate a dead config (the floor was not exercised)",
	})
	return rep
}
