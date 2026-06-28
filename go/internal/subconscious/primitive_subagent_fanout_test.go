package subconscious

import (
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// SEAM #2 — per-tick base-specialist model-call fan-out (07-OPTIMISATION-SURVEY.md §A.1 item 3).
// These tests are the base-specialist analogue of parallel_phase_test.go / parallel_speedup_test.go
// (seam #1): they pin that running the reason-only model-call base specialists (social/skeptic/advocate)
// CONCURRENTLY yields the EXACT same fired set + event stream as serial (byte-identical, only wall-clock
// changes), that the safe set is exactly the marked specialists, and that the model calls actually overlap.

// specCaller is a backends.SpecialistCaller test double whose Specialist call optionally consults a bound
// scheduler (mirroring the real llm backend's background-budget Grant on "specialist.<domain>") and injects
// a fixed latency. It counts the GRANTED calls atomically (the concurrent goroutines race to it). This is
// what makes the budget-deferral (gap #2) and the wall-clock overlap observable without a live model.
type specCaller struct {
	sched *scheduler.LLMScheduler
	delay time.Duration
	calls int64 // GRANTED Specialist calls (atomic)
}

func (c *specCaller) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	if c.sched != nil && !c.sched.Grant("specialist."+domain) {
		return "", false // budget spent ⇒ the specialist fires nothing (its serial budget-exhausted outcome)
	}
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	atomic.AddInt64(&c.calls, 1)
	return domain + " says: a reasoned stance", true
}

func (c *specCaller) count() int64 { return atomic.LoadInt64(&c.calls) }

// runPrimitiveSubAgentFanout drives ONE Dispatch over a base roster whose context fires SEVERAL parallel-safe
// model-call specialists (advocate + skeptic on the review shape, plus compute on the arithmetic), with
// the per-tick concurrency flag toggled, capturing the fired set + the full event stream. cfg.sched binds
// a scheduler so the budget-deferral path is exercised; cfg.caller is the model port (with optional delay).
type primSubAgentFanoutCfg struct {
	parallel bool
	theta    float64
	sched    *scheduler.LLMScheduler
	caller   backends.SpecialistCaller
}

func runPrimitiveSubAgentFanout(t *testing.T, cfg primSubAgentFanoutCfg) ([]string, []recEvent, *SubconsciousEngine) {
	t.Helper()

	var trace []recEvent
	rec := func(kind, summary string, data events.D) events.Event {
		trace = append(trace, recEvent{kind: kind, summary: summary, data: renderData(data)})
		return events.Event{}
	}

	caller := cfg.caller
	if caller == nil {
		caller = &fakeCaller{out: "a reasoned stance", ok: true}
	}
	recaller := &fakeRecaller{facts: map[string]string{}}

	// No executor / no workflow ⇒ the base roster is the whole roster; read/search/run stay dark (no
	// executor), so the only firers are the model-call stance roles + the deterministic compute primitive.
	eng := NewSubconsciousEngine(
		DefaultPrimitiveSubAgents(recaller, nil, caller, rec, nil, nil, false),
		cpyrand.New(1234), rec, nil, nil,
	)
	if cfg.sched != nil {
		eng.SetScheduler(cfg.sched)
	}

	prev := parallelPhases
	parallelPhases = cfg.parallel
	defer func() { parallelPhases = prev }()

	// A review shape (fires advocate + skeptic) plus an arithmetic expression (fires the pure compute
	// specialist) — a multi-specialist tick exercising both the concurrent set and a serial pure firer.
	ctx := dispatchCtx([]string{"is it safe to refactor this module; also what is 2 + 2?"})
	fired, _ := eng.Dispatch(ctx, cfg.theta, nil)

	texts := make([]string, 0, len(fired))
	for _, c := range fired {
		dom := ""
		if c.Domain != nil {
			dom = *c.Domain
		}
		texts = append(texts, dom+"|"+c.Text)
	}
	return texts, trace, eng
}

// TestPrimitiveSubAgentFanoutDeterministicEquality is the seam-#2 determinism gate: running the per-tick
// base-specialist fan-out with THOUGHT_PARALLEL_PHASES ON must yield the EXACT same fired candidates AND
// the exact same event stream as the serial (OFF) path. Concurrency may change wall-clock, never the
// outcome or the trace. Run many times so a completion-order race would eventually surface as a mismatch.
func TestPrimitiveSubAgentFanoutDeterministicEquality(t *testing.T) {
	serialFired, serialTrace, _ := runPrimitiveSubAgentFanout(t, primSubAgentFanoutCfg{parallel: false, theta: 0.3})

	for iter := 0; iter < 32; iter++ {
		parFired, parTrace, _ := runPrimitiveSubAgentFanout(t, primSubAgentFanoutCfg{parallel: true, theta: 0.3})
		if !reflect.DeepEqual(serialFired, parFired) {
			t.Fatalf("iter %d: fired set differs under specialist fan-out\n serial=%v\n   par=%v",
				iter, serialFired, parFired)
		}
		if !reflect.DeepEqual(serialTrace, parTrace) {
			t.Fatalf("iter %d: event stream differs under specialist fan-out\n serial=%#v\n   par=%#v",
				iter, serialTrace, parTrace)
		}
	}

	// Sanity: the fixture really fired BOTH model-call stance roles (the parallel set) AND the pure compute
	// specialist (a serial firer), else the test is vacuous.
	var sawAdvocate, sawSkeptic, sawCompute bool
	for _, txt := range serialFired {
		switch {
		case len(txt) >= 8 && txt[:8] == "advocate":
			sawAdvocate = true
		case len(txt) >= 7 && txt[:7] == "skeptic":
			sawSkeptic = true
		case len(txt) >= 7 && txt[:7] == "compute":
			sawCompute = true
		}
	}
	if !sawAdvocate || !sawSkeptic || !sawCompute {
		t.Fatalf("fixture did not exercise the mixed fan-out (advocate=%v skeptic=%v compute=%v): %v",
			sawAdvocate, sawSkeptic, sawCompute, serialFired)
	}
}

// TestPrimitiveSubAgentFanoutSafeSet pins the parallelizable set EXACTLY: only the reason-only model-call
// specialists (social/skeptic/advocate) implement parallelSafePrimitiveSubAgent; the pure (compute/recall/minted)
// and effectful (read/search/run/solver) ones do NOT. This is the guard that a NEW specialist is
// serial-by-default (it can only join the concurrent set by an author asserting the four safety properties).
func TestPrimitiveSubAgentFanoutSafeSet(t *testing.T) {
	caller := &fakeCaller{out: "x", ok: true}
	safe := map[string]bool{}
	for _, s := range DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false) {
		if _, ok := s.(parallelSafePrimitiveSubAgent); ok {
			safe[s.Domain()] = true
		}
	}
	want := map[string]bool{"social": true, "skeptic": true, "advocate": true}
	if !reflect.DeepEqual(safe, want) {
		t.Fatalf("parallel-safe specialist set = %v, want exactly %v", safe, want)
	}
	// Explicitly assert the pure + effectful specialists are NOT marked (serial-by-default).
	for _, s := range DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false) {
		if _, ok := s.(parallelSafePrimitiveSubAgent); ok {
			continue
		}
		switch s.Domain() {
		case "compute", "recall", "read", "search", "run":
			// expected: unmarked
		default:
			t.Fatalf("unexpected unmarked specialist domain %q (review whether it is parallel-safe)", s.Domain())
		}
	}
}

// TestPrimitiveSubAgentFanoutRespectsTheta (GAP #1): the pre-fire dispatches ONLY the specialists the serial loop
// would admit (eff>theta). With theta above the stance-role relevance (0.75) the safe set is empty, so the
// pre-fire declines (nil) and makes the SAME zero model calls the serial loop would (the roles are dark too).
func TestPrimitiveSubAgentFanoutRespectsTheta(t *testing.T) {
	caller := &specCaller{}
	recaller := &fakeRecaller{facts: map[string]string{}}
	eng := NewSubconsciousEngine(
		DefaultPrimitiveSubAgents(recaller, nil, caller, noopEmit, nil, nil, false),
		cpyrand.New(1234), noopEmit, nil, nil,
	)
	prev := parallelPhases
	parallelPhases = true
	defer func() { parallelPhases = prev }()

	// theta 0.8 > advocate/skeptic relevance 0.75 ⇒ the safe set is empty; the roster (compute aside) is dark.
	roster := eng.Specialists()
	pre := eng.preFirePrimitiveSubAgents(roster, dispatchCtx([]string{"is it safe to refactor this module?"}), 0.8, nil)
	if pre != nil {
		t.Fatalf("theta=0.8 > eff(0.75) ⇒ nothing admitted, pre-fire must decline (got %d)", len(pre))
	}
	if got := caller.count(); got != 0 {
		t.Fatalf("theta-skipped pre-fire made %d model calls; expected 0 (gap #1: extra calls)", got)
	}
}

// TestPrimitiveSubAgentFanoutBudgetDeterministic (GAP #2): with a background budget of 1 and TWO admitted safe
// specialists, concurrency is pointless (k<2) so the pre-fire declines and the serial loop fires+denies in
// roster order — deterministic, no completion-order grant lottery. Then with budget 2 both pre-fire (granted),
// proving the budget sizes the concurrent set up front.
func TestPrimitiveSubAgentFanoutBudgetDeterministic(t *testing.T) {
	ctx := dispatchCtx([]string{"is it safe to refactor this module?"}) // fires advocate + skeptic (2 safe)

	// budget 1 ⇒ k<2 ⇒ pre-fire declines (serial handles both).
	for run := 0; run < 8; run++ {
		sched := scheduler.New(nil, &scheduler.Config{BgBudget: 1, BgBudgetIdle: 1, EngageValue: 0})
		sched.TickReset(1.0, true)
		caller := &specCaller{sched: sched}
		eng := NewSubconsciousEngine(
			DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false),
			cpyrand.New(1234), noopEmit, nil, nil,
		)
		eng.SetScheduler(sched)
		prev := parallelPhases
		parallelPhases = true
		if pre := eng.preFirePrimitiveSubAgents(eng.Specialists(), ctx, 0.3, nil); pre != nil {
			parallelPhases = prev
			t.Fatalf("run %d: budget 1 ⇒ k<2 ⇒ pre-fire must decline (got %d)", run, len(pre))
		}
		parallelPhases = prev
	}

	// budget 2 ⇒ both admitted safe specialists pre-fire concurrently (granted).
	for run := 0; run < 8; run++ {
		sched := scheduler.New(nil, &scheduler.Config{BgBudget: 2, BgBudgetIdle: 2, EngageValue: 0})
		sched.TickReset(1.0, true)
		caller := &specCaller{sched: sched}
		eng := NewSubconsciousEngine(
			DefaultPrimitiveSubAgents(&fakeRecaller{facts: map[string]string{}}, nil, caller, noopEmit, nil, nil, false),
			cpyrand.New(1234), noopEmit, nil, nil,
		)
		eng.SetScheduler(sched)
		prev := parallelPhases
		parallelPhases = true
		pre := eng.preFirePrimitiveSubAgents(eng.Specialists(), ctx, 0.3, nil)
		parallelPhases = prev
		if len(pre) != 2 {
			t.Fatalf("run %d: budget 2 ⇒ both safe specialists pre-fire, got %d", run, len(pre))
		}
		if got := caller.count(); got != 2 {
			t.Fatalf("run %d: exactly 2 granted model calls expected, got %d", run, got)
		}
	}
}

// TestPrimitiveSubAgentFanoutOverlapsModelCalls is the WALL-CLOCK proof for seam #2 (the offline, deterministic
// twin of the live-claude bench): with a fixed per-call delay D injected into the model call, the serial
// (OFF) path spends ~2D firing advocate+skeptic back-to-back while the parallel (ON) path overlaps them (~D).
// A regression that re-serialised the fan-out would push the ON time back toward 2D and trip this.
func TestPrimitiveSubAgentFanoutOverlapsModelCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("wall-clock timing test (sleeps); skipped under -short")
	}
	const delay = 120 * time.Millisecond

	timeDispatch := func(parallel bool) (time.Duration, int) {
		caller := &specCaller{delay: delay}
		start := time.Now()
		fired, _, _ := runPrimitiveSubAgentFanout(t, primSubAgentFanoutCfg{parallel: parallel, theta: 0.3, caller: caller})
		return time.Since(start), len(fired)
	}

	serial, nSerial := timeDispatch(false)
	parallel, nPar := timeDispatch(true)
	if nSerial < 3 || nPar < 3 {
		t.Fatalf("fixture did not fire the mixed set both ways (serial=%d par=%d) — timing is meaningless", nSerial, nPar)
	}

	ceiling := 3 * delay / 2 // 1.5 * D — clear of the 2D serial floor and the ~1D parallel target
	if parallel >= ceiling {
		t.Fatalf("parallel specialist fan-out (%v) did not overlap the model calls (>= %v ceiling); serial was %v "+
			"(expected serial ~2D=%v, parallel ~D=%v) — the seam-#2 concurrency may have re-serialised",
			parallel, ceiling, serial, 2*delay, delay)
	}
	if serial < 2*delay {
		t.Fatalf("serial fan-out (%v) was under 2D=%v — the fixture did not run both model calls serially "+
			"(the comparison is vacuous)", serial, 2*delay)
	}
	t.Logf("seam #2 overlap: serial=%v parallel=%v (D=%v per call) — speed-up ~%.1f%% of the fan-out's call cost",
		serial, parallel, delay, 100*(1-float64(parallel)/float64(serial)))
}
