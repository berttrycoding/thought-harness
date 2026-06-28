package subconscious

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// fanout_panic_test.go — the regression that pins the fan-out worker-goroutine PANIC fix.
//
// THE BUG it guards: a panic inside a per-tick base-specialist fan-out worker goroutine
// (preFirePrimitiveSubAgents) — or a Par-phase sub-agent worker (preFireParallel) — is NOT recoverable
// by the dispatching goroutine's recover(). It unwinds the WORKER goroutine and takes the whole process
// down. In the bench A/B that is the silent "exit 1, no RUN-ERROR log, no report" at the single-strong
// arm: the per-arm safeArmRun recover runs on the bench's worker goroutine, never on the engine's
// fan-out children, so the crash escapes it entirely. The fix recovers IN the worker and re-raises the
// panic SYNCHRONOUSLY on the dispatching goroutine (after wg.Wait), where the engine -> bench call chain
// can finally catch it as an ordinary error (a logged FAIL cell) and the run CONTINUES.

// panicCaller is a backends.SpecialistCaller whose Specialist always panics — the live-substrate
// failure mode (a backend that blows up mid-call) reduced to a deterministic, offline trigger so the
// fan-out crash is reproducible without claude. The RolePrimitiveSubAgent (skeptic/advocate) wraps it,
// and those are the marked parallelSafePrimitiveSubAgent set, so a multi-stance tick fans them out
// concurrently and the panic fires inside a worker goroutine.
type panicCaller struct{ msg string }

func (c *panicCaller) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	panic(c.msg)
}

// TestPrimitiveSubAgentFanoutWorkerPanicIsRecoverable is the PROOF (seam #2): with a backend whose
// Specialist panics, a Dispatch that fans out >=2 reason-only stance specialists must surface the panic
// SYNCHRONOUSLY on the calling goroutine (so a deferred recover() catches it) — NOT crash the process
// in a detached worker goroutine. Pre-fix this would take the whole test binary down (an unrecoverable
// goroutine panic); post-fix the recover below catches it and the panic message names the fan-out worker.
func TestPrimitiveSubAgentFanoutWorkerPanicIsRecoverable(t *testing.T) {
	restore := SetParallelPhasesForTest(true) // force the concurrent fan-out path on
	defer restore()

	caught := func() (rec any) {
		defer func() { rec = recover() }()
		recaller := &fakeRecaller{facts: map[string]string{}}
		eng := NewSubconsciousEngine(
			DefaultPrimitiveSubAgents(recaller, nil, &panicCaller{msg: "BOOM: backend exploded mid-fire"}, noopEmit, nil, nil, false),
			cpyrand.New(1234), noopEmit, nil, nil,
		)
		// A review shape fires the advocate + skeptic stance specialists (the parallel-safe set), so the
		// fan-out runs >=2 workers concurrently and at least one panics inside its goroutine.
		ctx := dispatchCtx([]string{"is it safe to refactor this module; review the risks please"})
		eng.Dispatch(ctx, 0.3, nil)
		return nil // reached ONLY if Dispatch returned without panicking (the worker panic was swallowed)
	}()

	if caught == nil {
		t.Fatal("expected the fan-out worker panic to surface synchronously on the dispatching goroutine " +
			"(recoverable), but Dispatch returned normally — the panic was lost (or the fixture did not fan out)")
	}
	msg := panicText(caught)
	if !strings.Contains(msg, "fan-out worker panic") || !strings.Contains(msg, "BOOM: backend exploded mid-fire") {
		t.Fatalf("re-raised panic should name the fan-out worker AND carry the original message; got: %q", msg)
	}
}

// panicOperatorBackend is a Backend whose OperatorApply panics, for the Par-phase (seam #1) sub-agent
// fan-out. It embeds the test double so it satisfies the full interface and overrides only the one
// reason-path method the concurrent sub-agents call.
type panicOperatorBackend struct {
	*backends.TestBackend
	msg string
}

func (b *panicOperatorBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	panic(b.msg)
}

// TestParPhaseFanoutWorkerPanicIsRecoverable is the seam-#1 twin: a panic inside a Par-phase sub-agent
// worker (preFireParallel) likewise re-raises synchronously. It positions a recognised parallel workflow
// on its Par phase-group whose reason-only sub-agents fire on a backend whose OperatorApply panics.
func TestParPhaseFanoutWorkerPanicIsRecoverable(t *testing.T) {
	restore := SetParallelPhasesForTest(true)
	defer restore()

	caught := func() (rec any) {
		defer func() { rec = recover() }()
		be := &panicOperatorBackend{TestBackend: backends.NewTest(), msg: "KABOOM: operator backend exploded"}
		caller := &fakeCaller{out: "a reasoned stance", ok: true}
		recaller := &fakeRecaller{facts: map[string]string{}}
		cat := cognition.NewOperatorRegistry()

		prog, ok := cognition.RecognizeShape("compare A and B", nil)
		if !ok {
			t.Fatal("RecognizeShape(\"compare A and B\") found no shape")
		}
		wf := FromProgram(prog, cat, be, noopEmit, "compare A and B")
		// position the cursor on the PARALLEL phase (par(compare,contrast)).
		parIdx := -1
		for i, ph := range wf.Phases {
			if ph.Plan.Parallel {
				parIdx = i
				break
			}
		}
		if parIdx < 0 {
			t.Fatal("workflow has no parallel phase to exercise")
		}
		for wf.I() < parIdx {
			wf.Advance()
		}
		if len(wf.Instantiate(wf.Current(), nil, nil)) < 2 {
			t.Fatal("parallel phase instantiated <2 sub-agents; nothing to parallelise")
		}
		eng := NewSubconsciousEngine(
			DefaultPrimitiveSubAgents(recaller, nil, caller, noopEmit, nil, nil, false),
			cpyrand.New(1234), noopEmit, wf, nil,
		)
		eng.Dispatch(dispatchCtx([]string{"compare A and B"}), 0.0, nil)
		return nil
	}()

	if caught == nil {
		t.Fatal("expected the Par-phase fan-out worker panic to surface synchronously (recoverable), " +
			"but Dispatch returned normally — the panic was lost")
	}
	msg := panicText(caught)
	if !strings.Contains(msg, "fan-out worker panic") {
		t.Fatalf("re-raised Par-phase panic should name the fan-out worker; got: %q", msg)
	}
}

// panicText renders a recovered panic value to a string for the message assertions.
func panicText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
