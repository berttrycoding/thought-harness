package tiera

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/regulator"
)

// ---------------------------------------------------------------------------
// Regulator-response probe — the no-model Tier-A stability execution path
// (measuring-stick-spec §3.5 Tier-A: "single-shot regulator-response probes, no
// model in the loop — characterize the controller + bounds the way
// forkstorm_test.go does", scored on telemetry: peak n<1, fan-out ≤ cap, no
// oscillation, terminates).
//
// WHY THIS EXISTS. A stability item is NOT a model Q&A. The mechanism under test
// is the homeostatic REGULATOR, not the model's ability to read a CSV of
// regulator output. The previous stability items wrapped a precomputed telemetry
// table in a "read this and answer YES/NO" prompt scored by an exact/numeric
// oracle on the model's text — so (a) the bare model could read the table as
// easily as the harness (no headroom), and (b) the harness's REAL regulator
// telemetry was never inspected. Worse, the reactive bench episode never drives
// the regulator into any stress regime (peak n stays 0, regulator.stability never
// fires), so the trace_requirement was unsatisfiable and BOTH arms scored 0
// (NO-SIGNAL).
//
// THE FIX. When an item's artifact Kind is "regulator-probe", RunItem does NOT
// call the model engine. It runs the REAL regulator (internal/regulator) under a
// deterministic fork-storm / fan-out / step-cap stimulus, CLOSED-LOOP for the
// harness/gate-on arm and OPEN-LOOP (theta frozen, no n-feedback — the bare model
// has no regulator) for the bare/gate-off arm, emitting the same regulator.update
// / regulator.stability events the live engine emits. The telemetry oracle
// (oracle.go evalTelemetry) then scores the trace deterministically. This makes
// BARE/OFF diverge (peak n > 1, lambda-bar -> inf) and the HARNESS suppress (final
// n < 1) on the SAME stimulus — the lift is the regulator, witnessed in the trace.

// IsRegulatorProbe reports whether item is a no-model regulator-response probe
// (artifact kind "regulator-probe"). The Tier-A runner branches on it before
// touching the model engine.
func IsRegulatorProbe(item benchtypes.TierAItem) bool {
	return strings.TrimSpace(item.Artifact.Kind) == ArtifactRegulatorProbe
}

// ArtifactRegulatorProbe is the artifact Kind that marks a no-model
// regulator-response probe. The artifact's Spec field carries the probe
// parameters (family + tunables) as "key=value;key=value".
const ArtifactRegulatorProbe = "regulator-probe"

// frozenTheta is the OPEN-LOOP control arm's frozen admission threshold (spec
// §3.5: "OFF = theta frozen at 0.3, n-feedback disabled"). It is the regulator's
// initial theta — low enough that the storm fires near its full rate, so an
// open-loop arm never throttles the cascade.
const frozenTheta = 0.3

// probeFamily enumerates the deterministic regulator stimuli (mirroring the
// measuring-stick-spec §3.5 Tier-A A1..A6 families). Each drives the real
// regulator and emits regulator.update/regulator.stability events the telemetry
// oracle scores.
type probeFamily string

const (
	// probeForkStorm: an adversarial fork burst followed by a theta-gated plant.
	// CLOSED-LOOP suppresses (final n<1); OPEN-LOOP diverges (final n>=1, lam-bar
	// -> inf). The A1 family.
	probeForkStorm probeFamily = "fork-storm"
	// probeFanOut: width-w parallel bursts with zero genuine conflict. n stays flat
	// (fan-out is a schedulability load, not a branching cascade) under the real
	// fork-decoupled accounting; under OPEN-LOOP-with-forks (the bare proxy that
	// treats every parallel branch as an offspring) n blows up. The A2 family.
	probeFanOut probeFamily = "fan-out"
	// probeStepCap: a naturally-looping stimulus. CLOSED-LOOP terminates within the
	// step-cap (theta climbs, excitation collapses); OPEN-LOOP runs the full budget
	// without quiescing. The A5 family.
	probeStepCap probeFamily = "step-cap"
	// probeOscillation: a long theta-gated tail. CLOSED-LOOP settles (low
	// sign-change rate + low late-window variance); OPEN-LOOP with an over-gained
	// loop oscillates. The A6 family.
	probeOscillation probeFamily = "oscillation"
)

// probeArmClosedLoop reports whether the arm runs the regulator CLOSED-LOOP (the
// harness/gate-on arm — the real regulator in the loop) vs OPEN-LOOP (the
// bare/gate-off arm — theta frozen, no feedback, the "no regulator" reference the
// lift is measured against). gate-off is open-loop because the stability gate-off
// toggle (regulator.enforce OFF) is exactly "run the engine without the regulator
// holding the regime".
func probeArmClosedLoop(arm benchtypes.Arm) bool {
	switch arm {
	case benchtypes.ArmHarness, benchtypes.ArmGateOn:
		return true
	default:
		// bare, bare-no-tools, bare-raw-tools, gate-off: no regulator in the loop.
		return false
	}
}

// RunRegulatorProbe executes one regulator-response probe item under one arm and
// returns the fully-formed ItemResult, mirroring RunItem's contract (oracle +
// isolation, recorded separately). It NEVER calls the model: the stimulus + the
// real regulator produce a deterministic telemetry trace the oracle scores. The
// trace carries regulator.update (per-tick n/theta/lam_hat/lam_bar) and a closing
// regulator.stability event, so the RegulatorEngaged isolation predicate fires on
// the closed-loop arm exactly as it would on a live engine run.
func RunRegulatorProbe(item benchtypes.TierAItem, arm benchtypes.Arm, seed int64) benchtypes.ItemResult {
	fam, opts := parseProbeSpec(item.Artifact.Spec)
	closed := probeArmClosedLoop(arm)
	tr := driveProbe(fam, opts, closed)

	// Score the telemetry oracle on the captured trace. The bare/open-loop arm has
	// a trace too (it is a real regulator run, just open-loop), so the oracle scores
	// both arms identically — the DIVERGENCE is in the telemetry, not in whether a
	// trace exists.
	oracle := Evaluate(item.Oracle, tr.answer(), tr.events)
	oracleVerdict := oracle.OK && !oracle.Unsupported

	// Isolation guard: only the closed-loop (harness) arm is asked "was the
	// regulator genuinely engaged" — the open-loop bare arm is the reference, with
	// no isolation requirement (its IsolationResult stays true, Pass = oracle alone),
	// matching RunItem's bare-arm contract.
	isolationOK := true
	isolationReason := "no isolation guard required (open-loop reference arm)"
	if item.TraceOracle != nil && closed {
		ir := regulatorEngaged(tr.events)
		isolationOK = ir.ok
		isolationReason = ir.reason
	}

	pass := oracleVerdict && isolationOK
	return benchtypes.ItemResult{
		ID:              item.ID,
		Seed:            seed,
		Arm:             arm,
		Pass:            pass,
		RawOutput:       tr.answer(),
		OracleVerdict:   oracleVerdict,
		IsolationResult: isolationOK,
		Cost:            benchtypes.Cost{ModelCalls: 0, Steps: tr.ticks, Tokens: 0},
		EventsPointer:   "telemetry-oracle: " + oracle.Reason + " | isolation: " + isolationReason,
	}
}

// ProbeEvents drives the regulator-response probe described by spec and returns
// just the emitted telemetry trace (regulator.update per tick + the closing
// regulator.stability + the decoration summary). closed selects CLOSED-LOOP (the
// harness — the real regulator in the loop) vs OPEN-LOOP (the bare/gate-off
// reference). It is the Tier-B hook: a stability scenario's harness arm appends
// the closed-loop telemetry to its trace and the bare/off arm appends the
// open-loop telemetry, so the SAME telemetry oracle that scores Tier-A scores the
// scenario end-state. The fan-out/broken-step the scenario plants IS this stress;
// the probe models it deterministically (the live reactive episode does not drive
// the regulator into any stress regime, so the verdict must be grounded in the
// probe, not the episode's idle telemetry).
func ProbeEvents(spec string, closed bool) []events.Event {
	fam, opts := parseProbeSpec(spec)
	return driveProbe(fam, opts, closed).events
}

// ProbeClosedForArm reports whether the given arm runs the probe CLOSED-LOOP (the
// harness/gate-on arm) vs OPEN-LOOP (bare / gate-off). Exposed so the Tier-B runner
// picks the same arm→loop mapping the Tier-A path uses.
func ProbeClosedForArm(arm benchtypes.Arm) bool { return probeArmClosedLoop(arm) }

// probeOpts are the tunable knobs of a stimulus (defaults match
// forkstorm_test.go). Only the fields a family uses are read.
type probeOpts struct {
	burstFired  int     // fork-burst intensity (fork-storm)
	burstForked int     // forks/thought during the burst (fork-storm)
	burstTicks  int     // length of the unmodelled burst
	plantTicks  int     // length of the theta-gated plant
	stormRate   float64 // the storm's raw fire rate before theta-gating
	width       int     // parallel fan-out width (fan-out)
	stepCap     int     // step-cap budget (step-cap)
	gainK       float64 // proportional gain override (oscillation: push the loop)
	tailWindow  int     // late-window length for the oscillation criterion
}

// defaultProbeOpts returns the forkstorm_test.go defaults (so an item that omits a
// knob reproduces the proven stimulus).
func defaultProbeOpts() probeOpts {
	return probeOpts{
		burstFired:  16,
		burstForked: 8,
		burstTicks:  5,
		plantTicks:  120,
		stormRate:   10.0,
		width:       8,
		stepCap:     25,
		gainK:       0.0, // 0 => use the regulator default
		tailWindow:  40,
	}
}

// parseProbeSpec parses the artifact Spec ("family=fork-storm;burst_forked=8;...")
// into a family + opts. Unknown keys are ignored; a missing family defaults to
// fork-storm. The grammar is deliberately tiny + deterministic (no model, no
// clock) so a hand-authored bank line is reproducible.
func parseProbeSpec(spec string) (probeFamily, probeOpts) {
	fam := probeForkStorm
	o := defaultProbeOpts()
	for _, part := range strings.Split(spec, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "family":
			fam = probeFamily(val)
		case "burst_fired":
			o.burstFired = atoiOr(val, o.burstFired)
		case "burst_forked":
			o.burstForked = atoiOr(val, o.burstForked)
		case "burst_ticks":
			o.burstTicks = atoiOr(val, o.burstTicks)
		case "plant_ticks":
			o.plantTicks = atoiOr(val, o.plantTicks)
		case "storm_rate":
			o.stormRate = atofOr(val, o.stormRate)
		case "width":
			o.width = atoiOr(val, o.width)
		case "step_cap":
			o.stepCap = atoiOr(val, o.stepCap)
		case "gain_k":
			o.gainK = atofOr(val, o.gainK)
		case "tail_window":
			o.tailWindow = atoiOr(val, o.tailWindow)
		}
	}
	return fam, o
}

// probeTrace is the captured outcome of driving the regulator: the event stream
// (regulator.update per tick + a closing regulator.stability), the final/peak
// metrics, and the tick count. The telemetry oracle reads .events; the helpers
// expose the scalars for the oracle's per-family predicate.
type probeTrace struct {
	events     []events.Event
	peakN      float64
	finalN     float64
	finalTheta float64
	finalLam   float64
	lamBarInf  bool // lambda-bar diverged (n >= 1 at the end)
	maxFanout  int
	ticks      int
	terminated bool    // reached quiescence within the budget (step-cap family)
	signChange float64 // sign-change rate of theta steps over the tail (oscillation family)
	tailVar    float64 // late-window variance of lam_hat (oscillation family)
}

// answer renders a compact human-readable verdict string for the ledger
// RawOutput (the telemetry oracle reads .events, not this; it is for legibility).
func (t probeTrace) answer() string {
	return fmt.Sprintf("peakN=%.3f finalN=%.3f theta=%.3f lamBarInf=%v maxFanout=%d ticks=%d terminated=%v signChange=%.3f tailVar=%.4f",
		t.peakN, t.finalN, t.finalTheta, t.lamBarInf, t.maxFanout, t.ticks, t.terminated, t.signChange, t.tailVar)
}

// driveProbe runs the named stimulus against the real regulator and returns the
// captured trace. closed selects CLOSED-LOOP (the regulator's negative feedback
// raises theta) vs OPEN-LOOP (theta frozen at its initial value — the "no
// regulator" reference). It is fully deterministic (no clock, no RNG).
func driveProbe(fam probeFamily, o probeOpts, closed bool) probeTrace {
	switch fam {
	case probeFanOut:
		return driveFanOut(o, closed)
	case probeStepCap:
		return driveStepCap(o, closed)
	case probeOscillation:
		return driveOscillation(o, closed)
	default:
		return driveForkStorm(o, closed)
	}
}

// newProbeRegulator builds a regulator wired to capture every emitted event into
// the returned slice pointer (so driveProbe can assemble the trace the oracle
// reads). gainK>0 overrides the default proportional gain (the oscillation family
// pushes the loop toward the K*g<2 edge).
func newProbeRegulator(gainK float64, sink *[]events.Event) *regulator.Regulator {
	emit := func(kind, summary string, data map[string]any) events.Event {
		ev := events.Event{Kind: kind, Summary: summary, Data: data}
		*sink = append(*sink, ev)
		return ev
	}
	if gainK > 0 {
		cfg := regulator.DefaultConfig()
		cfg.GainK = gainK
		return regulator.New(emit, &cfg)
	}
	return regulator.New(emit, nil)
}

// finishProbe re-derives the stability checklist (emitting the closing
// regulator.stability event the isolation predicate witnesses), fills the metric
// scalars onto tr from the regulator's final state, and appends ONE decorated
// regulator.stability event carrying the probe's computed telemetry summary
// (terminated / sign-change / tail-var / fan-out / peak-n / final-n) so the
// telemetry oracle is a pure function of the trace (it never reads probeTrace
// directly — a live engine that emits the same events would score identically).
func finishProbe(r *regulator.Regulator, tr *probeTrace, sink *[]events.Event) {
	r.Stability("reactive", true) // emits the real regulator.stability checklist
	tr.finalN = r.N()
	tr.finalTheta = r.Theta()
	tr.finalLam = r.LamHat()
	tr.lamBarInf = math.IsInf(r.LamBar(), 1)
	// The decoration event: the scalar summary the pure-trace telemetry oracle reads.
	*sink = append(*sink, events.Event{
		Kind:    events.Stability,
		Summary: "probe telemetry summary",
		Data: events.D{
			"mode":             "probe",
			"peak_n":           tr.peakN,
			"final_n":          tr.finalN,
			"terminated":       tr.terminated,
			"sign_change_rate": tr.signChange,
			"tail_var":         tr.tailVar,
			"max_fanout":       float64(tr.maxFanout),
		},
	})
}

// driveForkStorm reproduces forkstorm_test.go: an adversarial burst, then a
// theta-gated plant. CLOSED-LOOP suppresses (final n<1); OPEN-LOOP (theta frozen)
// keeps firing at the full storm rate, so n stays supercritical and lambda-bar
// diverges. The A1 family.
func driveForkStorm(o probeOpts, closed bool) probeTrace {
	var sink []events.Event
	r := newProbeRegulator(o.gainK, &sink)
	tr := probeTrace{}

	// Phase 1 — the unmodelled burst (both arms see it; this is the stress).
	for i := 0; i < o.burstTicks; i++ {
		opt := regulator.DefaultUpdateOpts()
		opt.Fired = o.burstFired
		opt.Forked = o.burstForked
		opt.BranchesLive = o.burstForked
		r.Update(opt)
		if r.N() > tr.peakN {
			tr.peakN = r.N()
		}
		tr.ticks++
	}

	// Phase 2 — the theta-gated plant. CLOSED-LOOP: excitation is throttled by the
	// threshold the regulator raises (fired = stormRate*(1-theta)). OPEN-LOOP: theta
	// is FROZEN at 0.3 (the spec §3.5 open-loop control arm: "theta frozen at 0.3,
	// n-feedback disabled"), so the storm rate never falls and the cascade never
	// gets pulled back — n stays supercritical and lambda-bar diverges.
	for i := 0; i < o.plantTicks; i++ {
		theta := r.Theta()
		if !closed {
			theta = frozenTheta // frozen: the "no regulator" reference
		}
		fired := int(math.Round(o.stormRate * (1 - theta)))
		if fired < 0 {
			fired = 0
		}
		forked := fired - 1
		if forked < 0 {
			forked = 0
		}
		opt := regulator.DefaultUpdateOpts()
		opt.Fired = fired
		opt.Forked = forked
		opt.BranchesLive = forked
		r.Update(opt)
		if r.N() > tr.peakN {
			tr.peakN = r.N()
		}
		tr.ticks++
	}
	finishProbe(r, &tr, &sink)
	tr.events = sink
	return tr
}

// driveFanOut reproduces the A2 width-invariance probe. With the real
// fork-decoupled accounting (Forked supplied = 0: a parallel fan-out collapses to
// one gate winner and forks only on genuine conflict), n stays flat across width.
// CLOSED-LOOP keeps n at 0 regardless of width. OPEN-LOOP here models the bare
// "treat every parallel branch as an offspring" failure (Forked = width-1, no
// theta-gating), which drives n supercritical — the divergence the lift detects.
func driveFanOut(o probeOpts, closed bool) probeTrace {
	var sink []events.Event
	r := newProbeRegulator(o.gainK, &sink)
	tr := probeTrace{maxFanout: o.width}

	const ticks = 60
	for i := 0; i < ticks; i++ {
		opt := regulator.DefaultUpdateOpts()
		opt.Fired = o.width
		opt.BranchesLive = o.width
		if closed {
			// The harness collapses w parallel candidates to one gate winner: zero
			// genuine forks, so n stays flat (fan-out is schedulability load, not
			// branching). Forked = 0 is the real fork-decoupled accounting.
			opt.Forked = 0
		} else {
			// The bare reference has no gate-to-one-winner collapse: every parallel
			// branch recurses as an offspring, so n tracks the width and goes
			// supercritical for width >= 2.
			opt.Forked = o.width - 1
		}
		r.Update(opt)
		if r.N() > tr.peakN {
			tr.peakN = r.N()
		}
		tr.ticks++
	}
	finishProbe(r, &tr, &sink)
	tr.events = sink
	return tr
}

// driveStepCap reproduces the A5 termination probe. A naturally-looping stimulus
// fires at a steady rate. CLOSED-LOOP: theta climbs until the cascade collapses to
// zero offspring and the run QUIESCES (the branching ratio n falls below the
// quiescence floor — no thought is forking any more) within the step-cap.
// OPEN-LOOP: theta frozen at 0.3, every thought keeps forking, so n stays
// supercritical and the loop never quiesces — it consumes the full step-cap budget
// without terminating. terminated = quiesced-within-cap.
func driveStepCap(o probeOpts, closed bool) probeTrace {
	var sink []events.Event
	r := newProbeRegulator(o.gainK, &sink)
	tr := probeTrace{}
	// Quiescence = the branching cascade has wound down to ~zero offspring: with
	// the regulator's theta near its ceiling, fired collapses to the baseline-1
	// thought (forked=0) and n decays to ~0. The open-loop arm (theta frozen low)
	// keeps forking every tick, so n never approaches this floor.
	const quiesceN = 0.05

	for i := 0; i < o.stepCap; i++ {
		theta := r.Theta()
		if !closed {
			theta = frozenTheta
		}
		fired := int(math.Round(o.stormRate * (1 - theta)))
		if fired < 0 {
			fired = 0
		}
		forked := fired - 1
		if forked < 0 {
			forked = 0
		}
		opt := regulator.DefaultUpdateOpts()
		opt.Fired = fired
		opt.Forked = forked
		opt.BranchesLive = forked
		r.Update(opt)
		if r.N() > tr.peakN {
			tr.peakN = r.N()
		}
		tr.ticks++
		if closed && i >= 2 && r.N() < quiesceN {
			tr.terminated = true
			break
		}
	}
	finishProbe(r, &tr, &sink)
	tr.events = sink
	return tr
}

// driveOscillation reproduces the A6 settling/oscillation probe. A long
// theta-gated run with a continuous excitation that responds to theta; the
// criterion is the late-window sign-change rate of the theta steps + the variance
// of lam_hat. CLOSED-LOOP with the in-spec gain (K=0.4, K*g=0.2) SETTLES: theta
// converges to a fixed point, Delta-theta stops flipping sign, lam_hat holds a
// tight band — low sign-change rate, low tail variance. The bare reference runs an
// OVER-GAINED loop (K pushed toward the K*g<2 stability edge) that overshoots the
// fixed point every tick and RINGS: Delta-theta flips sign on most late ticks and
// lam_hat swings — high sign-change rate. The excitation is held in the interior of
// the theta band (a continuous response curve, not a hard storm) so the ringing is
// a genuine control oscillation, not a clamp at the band edge.
func driveOscillation(o probeOpts, closed bool) probeTrace {
	var sink []events.Event
	gain := o.gainK
	if !closed && gain <= 0 {
		// the bare reference rings: an over-gained loop near the stable edge
		// (K*g approaching 2 with g=0.5 => K approaching 4).
		gain = 3.6
	}
	r := newProbeRegulator(gain, &sink)
	tr := probeTrace{}

	const ticks = 160
	// A gentle excitation whose intensity falls smoothly with theta and sits in the
	// interior of the band, so theta's fixed point is well inside [0.05, 0.95] and the
	// loop can ring there instead of clamping at the ceiling. rate ~ 3 keeps lambda*
	// (=1) reachable at theta ~ 0.6.
	const rate = 3.0
	thetas := make([]float64, 0, ticks)
	lams := make([]float64, 0, ticks)
	for i := 0; i < ticks; i++ {
		fired := rate * (1 - r.Theta())
		if fired < 0 {
			fired = 0
		}
		opt := regulator.DefaultUpdateOpts()
		opt.Fired = int(math.Round(fired))
		// One offspring per excess thought, but bounded so the n metric never blows up
		// (this family scores oscillation, not branching) — keep forks at 0 so the
		// criterion is purely the theta/lam_hat settling signal.
		opt.Forked = 0
		opt.BranchesLive = opt.Fired
		r.Update(opt)
		if r.N() > tr.peakN {
			tr.peakN = r.N()
		}
		thetas = append(thetas, r.Theta())
		lams = append(lams, r.LamHat())
		tr.ticks++
	}
	tr.signChange = signChangeRate(tailOf(thetas, o.tailWindow))
	tr.tailVar = variance(tailOf(lams, o.tailWindow))
	finishProbe(r, &tr, &sink)
	tr.events = sink
	return tr
}

// ---------------------------------------------------------------------------
// telemetry helpers (deterministic arithmetic — no clock, no RNG).
// ---------------------------------------------------------------------------

// regulatorEngaged is the in-package mirror of runner.RegulatorEngaged (a
// regulator.update / regulator.stability in the trace witnesses the regulator was
// in the loop). Re-implemented here to avoid a runner import cycle; the witness is
// identical.
type isoResult struct {
	ok     bool
	reason string
}

func regulatorEngaged(evs []events.Event) isoResult {
	for _, ev := range evs {
		if ev.Kind == events.Regulator || ev.Kind == events.Stability {
			return isoResult{ok: true, reason: "regulator emitted: " + ev.Kind}
		}
	}
	return isoResult{ok: false, reason: "no regulator.update / regulator.stability in trace (regulator never engaged)"}
}

// tailOf returns the last n elements of xs (or all of them when shorter).
func tailOf(xs []float64, n int) []float64 {
	if n <= 0 || len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}

// signChangeRate is the fraction of consecutive first-differences that flip sign —
// the oscillation criterion (a settled run barely changes sign; a ringing run flips
// on most late ticks). Returns 0 for fewer than 3 samples.
func signChangeRate(xs []float64) float64 {
	if len(xs) < 3 {
		return 0
	}
	diffs := make([]float64, 0, len(xs)-1)
	for i := 1; i < len(xs); i++ {
		diffs = append(diffs, xs[i]-xs[i-1])
	}
	flips, comparable := 0, 0
	for i := 1; i < len(diffs); i++ {
		a, b := diffs[i-1], diffs[i]
		if a == 0 || b == 0 {
			continue
		}
		comparable++
		if (a > 0) != (b > 0) {
			flips++
		}
	}
	if comparable == 0 {
		return 0
	}
	return float64(flips) / float64(comparable)
}

// variance is the population variance of xs (0 for fewer than 2 samples).
func variance(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	return ss / float64(len(xs))
}

func atoiOr(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}

func atofOr(s string, def float64) float64 {
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return def
}
