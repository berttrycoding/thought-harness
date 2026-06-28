// Package conformance is the L0 conformance ROLLUP — the single PASS/FAIL front-door that answers "does it
// even run as a harness?" (Track H, docs/internal/notes/2026-06-20-benchmark-taxonomy.md §1 L0 + §5 build-order #1).
//
// THE GAP IT CLOSES. The L0 PIECES already existed — S1..S16 golden conformance fixtures (internal/scenarios),
// the deterministic control floor (internal/control), the seed's requirement checklist — but there was no
// single command that runs all three and emits ONE verdict. This package is that rollup: it drives S1..S16
// on the live engine, applies a deterministic REQUIREMENT CHECKLIST per run (the loop terminated, events
// emitted, the step-cap held, the lifecycle reached a sane state, no orphan tool-results, the run is
// deterministic), runs the WIRING SCAN (the named subsystems actually FIRED on the live loop — the
// "tests pass != feature runs" gate, observed through engine.EmitWiringScan / WiringCoverage), and rolls
// it all into a single PASS/FAIL emitted as conformance.rollup.
//
// L0 DISCIPLINE (benchmark-taxonomy §1 L0): PASS/FAIL, deterministic, offline, ~free. NO model, NO noise —
// the rollup runs on the test double (a Pattern-A CONTROL instrument). It never authors text and never
// reaches a backend; it observes the live loop and applies closed-form checks.
//
// DETERMINISM. Every engine is built on a FIXED seed (seedBase) and the test double, so each scenario's
// event-kind sequence is reproducible; the "deterministic" requirement re-runs each scenario and asserts
// the event-kind sequence is byte-stable run-to-run (the golden-stability half of L0). No wall clock, no
// unseeded randomness.
package conformance

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scenarios"
)

// seedBase is the fixed RNG seed every conformance engine is built on, so each scenario's trajectory (and
// thus its event stream) is reproducible. 7 matches the harness's canonical golden seed.
const seedBase = 7

// coreLayers are the subsystem layers EVERY S1..S16 run must exercise on the live loop — the per-run wiring
// floor. A run that compiled but never drove the conscious stream / a seam / the Critic / the value signal /
// the regulator / the lifecycle / a tick is a dead-wired harness and FAILS the scan. (subconscious and
// action are scenario-specific — a fluent-recall scenario may keep the subconscious quiet, a pure-thinking
// scenario takes no action — so they are NOT in the per-run floor; they are required ACROSS the suite, see
// suiteLayers.)
var coreLayers = []string{"conscious", "seam", "critic", "value", "regulator", "lifecycle", "tick"}

// suiteLayers are the layers that must appear SOMEWHERE across the full S1..S16 suite (a superset of
// coreLayers): the subconscious must fire on at least one scenario, and the action layer must be exercised
// by at least one scenario (S5/S6 take real action). If the whole suite never touches a layer, that
// subsystem is dead-wired suite-wide.
var suiteLayers = []string{"conscious", "seam", "critic", "value", "regulator", "lifecycle", "tick", "subconscious", "action"}

// Result is the rolled-up L0 conformance verdict over S1..S16.
type Result struct {
	Pass         bool
	Scenarios    int         // how many scenarios ran (target: len(scenarios.All()))
	ChecksPassed int         // requirement-checklist + wiring checks that passed
	ChecksTotal  int         // total checks applied
	WiringOK     bool        // every per-run wiring scan passed AND every suite-wide layer appeared
	Runs         []RunResult // per-scenario detail
	SuiteCovered []string    // the union of layers any scenario exercised
	SuiteMissing []string    // suiteLayers never seen by any scenario (dead-wired suite-wide)
	Failures     []string    // human-readable failure lines (empty ⇒ PASS)
}

// RunResult is one scenario's conformance outcome.
type RunResult struct {
	ID       string
	Pass     bool
	Checks   []Check  // the per-run requirement checklist + wiring scan
	Covered  []string // layers this run exercised
	Events   int      // events emitted this run
	Failures []string // this run's failed-check messages
}

// Check is one named conformance assertion + its outcome.
type Check struct {
	Name string
	Pass bool
	Why  string // failure detail (empty when Pass)
}

// Run executes the full L0 conformance rollup over S1..S16 on the test double and returns the rolled-up
// verdict. It emits one conformance.wiring per scenario (the live-loop wiring witness) and one
// conformance.rollup verdict on rollupBus (a caller-supplied bus the CLI front-door subscribes its sinks
// to; pass nil to skip the verdict emit). Deterministic + offline — no model, no clock, no unseeded RNG.
func Run(rollupBus *events.Bus) Result {
	res := Result{Pass: true, WiringOK: true}
	suiteSeen := make(map[string]bool, len(suiteLayers))

	for _, sc := range scenarios.All() {
		rr := runOne(sc.ID)
		res.Runs = append(res.Runs, rr)
		res.Scenarios++
		for _, c := range rr.Checks {
			res.ChecksTotal++
			if c.Pass {
				res.ChecksPassed++
			}
		}
		if !rr.Pass {
			res.Pass = false
			for _, f := range rr.Failures {
				res.Failures = append(res.Failures, sc.ID+": "+f)
			}
		}
		for _, l := range rr.Covered {
			suiteSeen[l] = true
		}
	}

	// suite-wide coverage: every suiteLayer must appear in at least one scenario.
	for l := range suiteSeen {
		res.SuiteCovered = append(res.SuiteCovered, l)
	}
	sort.Strings(res.SuiteCovered)
	for _, l := range suiteLayers {
		if !suiteSeen[l] {
			res.SuiteMissing = append(res.SuiteMissing, l)
		}
	}
	sort.Strings(res.SuiteMissing)
	if len(res.SuiteMissing) > 0 {
		res.WiringOK = false
		res.Pass = false
		res.Failures = append(res.Failures, "suite-wide wiring: layer(s) never exercised by any scenario: "+joinComma(res.SuiteMissing))
	}
	// per-run wiring failures also clear WiringOK (the suite verdict reflects every layer of the scan).
	for _, rr := range res.Runs {
		for _, c := range rr.Checks {
			if c.Name == "wiring-scan" && !c.Pass {
				res.WiringOK = false
			}
		}
	}

	if rollupBus != nil {
		rollupBus.Emit(events.ConformanceRollup, rollupSummary(res),
			events.D{
				"pass":          res.Pass,
				"scenarios":     res.Scenarios,
				"checks_passed": res.ChecksPassed,
				"checks_total":  res.ChecksTotal,
				"wiring_ok":     res.WiringOK,
				"failures":      res.Failures,
			})
	}
	return res
}

// runOne drives ONE scenario with the conformance.self_check tap ON, captures its event stream, applies the
// requirement checklist + the wiring scan, and returns the per-run outcome. Determinism is re-checked by a
// second identical run whose event-kind sequence must match the first.
func runOne(id string) RunResult {
	rr := RunResult{ID: id}

	seq1, cov1, evCount1, ok1, scanOK1, err1 := driveScenario(id)
	if err1 != nil {
		rr.Pass = false
		rr.Failures = append(rr.Failures, "engine build/run error: "+err1.Error())
		rr.Checks = append(rr.Checks, Check{Name: "runs", Pass: false, Why: err1.Error()})
		return rr
	}
	rr.Covered = cov1
	rr.Events = evCount1

	add := func(name string, pass bool, why string) {
		rr.Checks = append(rr.Checks, Check{Name: name, Pass: pass, Why: why})
		if !pass {
			rr.Failures = append(rr.Failures, name+": "+why)
		}
	}

	// 1. the loop RAN (events emitted) — the floor that proves the harness ticked at all.
	add("events-emitted", evCount1 > 0, "no events emitted (the loop never ran)")

	// 2. the loop TERMINATED — it reached a recognised lifecycle state and did not corrupt it. ok1 is the
	//    engine's reported lifecycle state at the end of the drive.
	add("loop-terminated", ok1 != "", "engine ended in an empty/unrecognised lifecycle state")

	// 3. step-cap HELD — the tick count never exceeded the scenario's budget (a runaway loop is a FAIL).
	tick, budget := lastTickAndBudget(id, seq1)
	add("step-cap-held", tick <= budget, "tick "+itoaSmall(tick)+" exceeded budget "+itoaSmall(budget))

	// 4. no orphan tool-results — every action.observation is preceded by an action.intention/act on the
	//    same run (an observation with no preceding intention = an orphan tool-result, LATHE's
	//    SanitizeMessages invariant).
	orphan, why := hasOrphanObservation(seq1)
	add("no-orphan-tool-results", !orphan, why)

	// 5. WIRING SCAN — the named subsystems fired on the live loop (engine.EmitWiringScan witness). scanOK1
	//    is the engine's own per-run scan verdict over coreLayers.
	missing := missingLayers(coreLayers, cov1)
	add("wiring-scan", scanOK1 && len(missing) == 0, "required layer(s) never fired: "+joinComma(missing))

	// 6. DETERMINISTIC — a second identical run produces the same event-kind sequence (the golden-stability
	//    half of L0). This is the cheap reproducibility guard the goldens encode.
	seq2, _, _, _, _, err2 := driveScenario(id)
	det := err2 == nil && sameSeq(seq1, seq2)
	why6 := ""
	if !det {
		why6 = "event-kind sequence differs run-to-run (non-deterministic)"
	}
	add("deterministic", det, why6)

	rr.Pass = len(rr.Failures) == 0
	return rr
}

// driveScenario builds a fresh engine with conformance.self_check ON, runs the scenario, captures the
// event-kind sequence, and returns it plus the wiring coverage, event count, the final lifecycle state, and
// the engine's per-run wiring-scan verdict. The engine runs on the test double (offline, deterministic).
func driveScenario(id string) (seq []string, covered []string, eventCount int, finalState string, scanOK bool, err error) {
	cfg := engine.DefaultConfig()
	cfg.Seed = seedBase
	feats := config.New()              // AllOn baseline
	feats.Conformance.SelfCheck = true // arm the wiring-coverage tap
	cfg.Features = feats

	sc, ok := scenarios.Get(id)
	if !ok {
		return nil, nil, 0, "", false, &scenarios.UnknownScenarioError{Name: id}
	}
	cfg.Mode = sc.Mode

	eng, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		return nil, nil, 0, "", false, err
	}
	// capture the event-kind sequence as the engine emits (subscribe BEFORE the run so the first emit is
	// seen). The tap inside the engine records coverage independently; this captures the ordered kinds for
	// the orphan + determinism checks.
	var kinds []string
	eng.Bus().Subscribe(func(ev events.Event) { kinds = append(kinds, ev.Kind) })

	if _, err := scenarios.RunScenario(id, eng); err != nil {
		return nil, nil, 0, "", false, err
	}

	covered, eventCount = eng.WiringCoverage()
	finalState = eng.LifecycleState()
	scanOK = eng.EmitWiringScan(coreLayers)
	return kinds, covered, eventCount, finalState, scanOK, nil
}

// lastTickAndBudget returns the highest tick the run reached (counted from the "tick" events) and the
// scenario's step budget. A tick event is emitted once per Step, so the count is the tick high-water mark.
func lastTickAndBudget(id string, seq []string) (tick, budget int) {
	for _, k := range seq {
		if k == events.Tick {
			tick++
		}
	}
	if sc, ok := scenarios.Get(id); ok {
		budget = sc.MaxTicks
	}
	return tick, budget
}

// hasOrphanObservation reports whether an action.observation appears with no preceding action.intention or
// action.act in the same run (an orphan tool-result — the LATHE SanitizeMessages invariant). Returns the
// offending detail on failure.
func hasOrphanObservation(seq []string) (orphan bool, why string) {
	intentSeen := false
	for _, k := range seq {
		switch k {
		case events.Intention, events.Act:
			intentSeen = true
		case events.Observation:
			if !intentSeen {
				return true, "action.observation with no preceding action.intention/act (orphan tool-result)"
			}
		}
	}
	return false, ""
}

// missingLayers returns the required layers not present in covered (sorted).
func missingLayers(required, covered []string) []string {
	seen := make(map[string]bool, len(covered))
	for _, l := range covered {
		seen[l] = true
	}
	var miss []string
	for _, r := range required {
		if !seen[r] {
			miss = append(miss, r)
		}
	}
	sort.Strings(miss)
	return miss
}

// sameSeq reports whether two event-kind sequences are element-for-element equal (the determinism guard).
func sameSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rollupSummary renders the one-line console string for the conformance.rollup verdict.
func rollupSummary(r Result) string {
	v := "PASS"
	if !r.Pass {
		v = "FAIL"
	}
	return "conformance " + v + ": " + itoaSmall(r.ChecksPassed) + "/" + itoaSmall(r.ChecksTotal) +
		" checks over " + itoaSmall(r.Scenarios) + " scenarios"
}

// joinComma joins with ", " (stdlib-free, the package keeps a tiny surface).
func joinComma(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

// itoaSmall is a tiny non-negative int->string for summary strings (the rollup never sees negatives).
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
