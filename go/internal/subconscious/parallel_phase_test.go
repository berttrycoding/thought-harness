package subconscious

import (
	"fmt"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// schedBackend is a scheduler-aware test backend that mirrors the REAL llm backend's budget behaviour
// (which TestBackend lacks — it never consults the scheduler): its OperatorApply consults the bound
// LLMScheduler via Grant on the BACKGROUND role "operator.<role>" exactly as openai.go's chat does, so
// a fan-out wider than the per-tick background budget DEFERS some calls. A deferred call returns "" (the
// gap — Pattern B; fireReason then falls back to "[role] intent"), a granted call returns the embedded
// TestBackend's deterministic content. calls counts the GRANTED model calls atomically (the goroutines
// race to it under concurrency). This is what makes gap #1 (extra calls) + gap #2 (which calls defer)
// observable in a unit test without a live model.
type schedBackend struct {
	*backends.TestBackend
	sched *scheduler.LLMScheduler
	calls int64 // GRANTED OperatorApply calls (atomic; the concurrent goroutines increment it)
}

func newSchedBackend(s *scheduler.LLMScheduler) *schedBackend {
	return &schedBackend{TestBackend: backends.NewTest(), sched: s}
}

func (b *schedBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	if b.sched != nil && !b.sched.Grant("operator."+role) {
		return "" // budget spent ⇒ surface the gap (mirrors chat's deferred return)
	}
	atomic.AddInt64(&b.calls, 1)
	return b.TestBackend.OperatorApply(role, responsibility, intent, domain, goal, ctx)
}

func (b *schedBackend) callCount() int64 { return atomic.LoadInt64(&b.calls) }

// recEvent is one captured emit, used to compare the trace stream between flag-off and flag-on.
type recEvent struct {
	kind    string
	summary string
	data    string // a stable rendering of the data map for equality (events.D is map[string]any)
}

// parDispatchCfg configures one drive of the parallel phase-group fixture.
type parDispatchCfg struct {
	parallel bool                    // toggle THOUGHT_PARALLEL_PHASES for this call
	theta    float64                 // dispatch admission threshold (default fixture uses 0.3)
	sched    *scheduler.LLMScheduler // optional scheduler bound into the engine (nil ⇒ unbounded)
	backend  backends.Backend        // the language faculty (nil ⇒ a plain TestBackend)
}

// runParallelDispatchCfg drives ONE Dispatch through a workflow positioned at its PARALLEL phase-group
// (par(compare, contrast) for "compare A and B"), capturing the fired-candidate set + the full event
// stream. cfg.parallel toggles the package flag for the duration of the call (restored after). The base
// roster + workflow are reason-only (no executor, no cognition view) so the parallel group is exactly
// the case the concurrency path optimises — and the determinism assertion is meaningful. A scheduler can
// be bound (cfg.sched) so the budget-deferral path (gap #2) is exercised on the test backend.
func runParallelDispatchCfg(t *testing.T, cfg parDispatchCfg) ([]string, []recEvent) {
	t.Helper()

	var trace []recEvent
	rec := func(kind, summary string, data events.D) events.Event {
		trace = append(trace, recEvent{kind: kind, summary: summary, data: renderData(data)})
		return events.Event{}
	}

	// A model port so the reason path produces deterministic content (TestBackend.OperatorApply).
	caller := &fakeCaller{out: "a reasoned stance", ok: true}
	recaller := &fakeRecaller{facts: map[string]string{}}
	var be backends.Backend = cfg.backend
	if be == nil {
		be = backends.NewTest()
	}
	cat := cognition.NewOperatorRegistry()

	prog, ok := cognition.RecognizeShape("compare A and B", nil)
	if !ok {
		t.Fatal("RecognizeShape(\"compare A and B\") found no shape")
	}
	// reason-only sub-agents: no executor, no cognition view handed to the workflow.
	wf := FromProgram(prog, cat, be, rec, "compare A and B")

	// Position the cursor on the PARALLEL phase (par(compare, contrast)). For "compare A and B" the
	// schedule is [decompose, par(compare,contrast), rank]; advance once past decompose.
	parallelIdx := -1
	for i, ph := range wf.Phases {
		if ph.Plan.Parallel {
			parallelIdx = i
			break
		}
	}
	if parallelIdx < 0 {
		t.Fatal("workflow has no parallel phase to exercise")
	}
	for wf.I() < parallelIdx {
		wf.Advance()
	}
	if len(wf.Instantiate(wf.Current(), nil, nil)) < 2 {
		t.Fatalf("parallel phase instantiated <2 sub-agents; nothing to parallelise")
	}

	eng := NewSubconsciousEngine(
		DefaultPrimitiveSubAgents(recaller, nil /*no executor*/, caller, rec, nil, nil, false),
		cpyrand.New(1234), rec, wf, nil,
	)
	if cfg.sched != nil {
		eng.SetScheduler(cfg.sched)
	}

	// flip the package flag for this call only (resolved-once at init; the test overrides it directly).
	prev := parallelPhases
	parallelPhases = cfg.parallel
	defer func() { parallelPhases = prev }()

	ctx := dispatchCtx([]string{"compare A and B for this problem"})
	fired, _ := eng.Dispatch(ctx, cfg.theta, nil)

	texts := make([]string, 0, len(fired))
	for _, c := range fired {
		dom := ""
		if c.Domain != nil {
			dom = *c.Domain
		}
		texts = append(texts, dom+"|"+c.Text)
	}
	return texts, trace
}

// runParallelDispatch is the default-theta (0.3), unscheduled drive used by the equality gate.
func runParallelDispatch(t *testing.T, parallel bool) ([]string, []recEvent) {
	t.Helper()
	return runParallelDispatchCfg(t, parDispatchCfg{parallel: parallel, theta: 0.3})
}

// renderData renders an events.D deterministically (keys sorted) for equality. The on/off paths build
// the SAME data values, so a stable function of the value is all the comparison needs.
func renderData(d events.D) string {
	if len(d) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := "{"
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += k + "=" + fmt.Sprintf("%v", d[k])
	}
	return out + "}"
}

// TestParallelPhasesDeterministicEquality is the §A.1 determinism gate: running a Parallel phase-group
// with THOUGHT_PARALLEL_PHASES ON must yield the EXACT same fired candidates AND the exact same event
// stream as the serial (default-OFF) path. Concurrency may change wall-clock, never the outcome or the
// trace. Run many times so a completion-order race would eventually surface as a mismatch.
func TestParallelPhasesDeterministicEquality(t *testing.T) {
	serialFired, serialTrace := runParallelDispatch(t, false)

	for iter := 0; iter < 32; iter++ {
		parFired, parTrace := runParallelDispatch(t, true)
		if !reflect.DeepEqual(serialFired, parFired) {
			t.Fatalf("iter %d: fired set differs under concurrency\n serial=%v\n   par=%v",
				iter, serialFired, parFired)
		}
		if !reflect.DeepEqual(serialTrace, parTrace) {
			t.Fatalf("iter %d: event stream differs under concurrency\n serial=%#v\n   par=%#v",
				iter, serialTrace, parTrace)
		}
	}

	// Sanity: the fixture really exercised the parallel fan-out (both stances fired), else the test is
	// vacuous. compare + contrast are the two reason-only sub-agents of the par group.
	var sawCompare, sawContrast bool
	for _, ev := range serialTrace {
		if ev.kind == string(events.SubSubagent) {
			if contains(ev.summary, "compare") {
				sawCompare = true
			}
			if contains(ev.summary, "contrast") {
				sawContrast = true
			}
		}
	}
	if !sawCompare || !sawContrast {
		t.Fatalf("fixture did not exercise the parallel fan-out (compare=%v contrast=%v)", sawCompare, sawContrast)
	}
}

// TestParallelPhasesPreFireRespectsTheta is the GAP #1 catch: the parallel pre-fire must dispatch ONLY
// the steps the serial loop would admit (eff>theta), never the whole phase-group. With theta raised
// above every sub-agent's effective relevance (0.9), the serial loop fires NOTHING and makes ZERO model
// calls; a correct parallel path must also make ZERO calls. The pre-θ-fix code pre-fired every sub-agent
// unconditionally, so it would make 2 model calls here AND fire candidates the serial path skips — this
// test FAILS on that bug (extra calls + a differing fired set) and PASSES on the θ-respecting fix.
func TestParallelPhasesPreFireRespectsTheta(t *testing.T) {
	const hiTheta = 0.95 // > sub-agent eff (0.9) ⇒ the serial loop admits nothing

	// Serial reference: nothing fires, no model call.
	sBe := newSchedBackend(nil)
	serialFired, _ := runParallelDispatchCfg(t, parDispatchCfg{parallel: false, theta: hiTheta, backend: sBe})
	if len(serialFired) != 0 {
		t.Fatalf("serial reference fired %v at theta=%.2f; expected nothing (fixture broken)", serialFired, hiTheta)
	}
	if sBe.callCount() != 0 {
		t.Fatalf("serial reference made %d model calls at theta=%.2f; expected 0", sBe.callCount(), hiTheta)
	}

	// Parallel: must match — zero fired, zero pre-fire model calls (no extra calls / budget burn).
	for iter := 0; iter < 32; iter++ {
		pBe := newSchedBackend(nil)
		parFired, _ := runParallelDispatchCfg(t, parDispatchCfg{parallel: true, theta: hiTheta, backend: pBe})
		if !reflect.DeepEqual(serialFired, parFired) {
			t.Fatalf("iter %d: parallel fired %v at high theta; serial fired %v (gap #1: pre-fired below-theta steps)",
				iter, parFired, serialFired)
		}
		if pBe.callCount() != 0 {
			t.Fatalf("iter %d: parallel made %d model calls at high theta; expected 0 (gap #1: extra calls / budget burn)",
				iter, pBe.callCount())
		}
	}
}

// TestParallelPhasesBudgetGrantByIndex is the GAP #2 catch: when the parallel fan-out is wider than the
// per-tick BACKGROUND budget, WHICH sub-agents fire (granted, real content) vs defer (denied, fallback
// text) must be a function of step INDEX, never goroutine completion order. With BgBudget=1 and two
// reason-only sub-agents (compare@index0, contrast@index1), serial grants compare and defers contrast;
// the parallel path must do the EXACT same — byte-identical fired set + trace + the SAME single model
// call — on every run. A completion-order bug would sometimes grant contrast instead, surfacing here as
// a mismatch over the repeated runs (the budget-grant race the spec names).
func TestParallelPhasesBudgetGrantByIndex(t *testing.T) {
	cfg := scheduler.DefaultConfig()
	cfg.BgBudget = 1 // exactly one background grant per tick ⇒ the 2-wide fan-out must defer one

	// Serial reference under the 1-call budget: fixes the expected fire-vs-defer split by index.
	sSched := scheduler.New(nil, &cfg)
	sSched.TickResetDefault() // remaining := BgBudget (1) for this tick
	sBe := newSchedBackend(sSched)
	serialFired, serialTrace := runParallelDispatchCfg(t, parDispatchCfg{
		parallel: false, theta: 0.3, sched: sSched, backend: sBe,
	})
	if sBe.callCount() != 1 {
		t.Fatalf("serial reference made %d model calls under BgBudget=1; expected exactly 1", sBe.callCount())
	}
	// Sanity: both sub-agents still FIRE (one with real content, one with the deferred fallback) — the
	// budget bounds calls, not firing, so the fired SET is 2 and the split is what we pin.
	if len(serialFired) != 2 {
		t.Fatalf("serial reference fired %v under BgBudget=1; expected both sub-agents (compare granted, contrast deferred)", serialFired)
	}

	for iter := 0; iter < 64; iter++ {
		pSched := scheduler.New(nil, &cfg)
		pSched.TickResetDefault()
		pBe := newSchedBackend(pSched)
		parFired, parTrace := runParallelDispatchCfg(t, parDispatchCfg{
			parallel: true, theta: 0.3, sched: pSched, backend: pBe,
		})
		if pBe.callCount() != 1 {
			t.Fatalf("iter %d: parallel made %d model calls under BgBudget=1; expected exactly 1 (gap #2: budget over/under-spent)",
				iter, pBe.callCount())
		}
		if !reflect.DeepEqual(serialFired, parFired) {
			t.Fatalf("iter %d: parallel fire/defer split differs under budget pressure (gap #2: completion-order grant)\n serial=%v\n   par=%v",
				iter, serialFired, parFired)
		}
		if !reflect.DeepEqual(serialTrace, parTrace) {
			t.Fatalf("iter %d: parallel trace differs under budget pressure\n serial=%#v\n   par=%#v",
				iter, serialTrace, parTrace)
		}
	}
}

// TestResolveParallelPhasesDefaultOn guards the validated default: the per-phase concurrency flag now
// defaults ON (the speed-up, proven byte-identical to serial + durability-gated), and ONLY the documented
// falsy vocabulary (0/false/no/off/n) turns it OFF for the legacy serial path. An unset/garbage value keeps
// the default-ON. (The package-level var is resolved from env at init; this pins the resolver it is built
// from.) This is the one-line default-flip's regression guard — flipping it back to default-OFF, or letting
// a typo turn the speed-up off, would break this.
func TestResolveParallelPhasesDefaultOn(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		// default-ON: unset, unrecognised, and the truthy vocabulary all resolve ON.
		{"", true}, {"garbage", true}, {"1", true}, {"true", true}, {"TRUE", true}, {"yes", true}, {"on", true}, {"y", true},
		// only the explicit falsy vocabulary forces the legacy serial path OFF.
		{"0", false}, {"false", false}, {"FALSE", false}, {"no", false}, {"off", false}, {"n", false},
	}
	for _, tc := range cases {
		t.Setenv("THOUGHT_PARALLEL_PHASES", tc.val)
		if got := resolveParallelPhases(); got != tc.want {
			t.Errorf("resolveParallelPhases(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

// --- tiny helpers (kept local; the package already has indexOf/containsLower) -------------------

func contains(s, sub string) bool { return indexOf(s, sub) >= 0 }
