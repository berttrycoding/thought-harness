package llm

import (
	"sync"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
)

// TestBackendConcurrentChatRaceFree exercises the per-phase-parallelism hazard (07 §A.1 task #7):
// under THOUGHT_PARALLEL_PHASES several reason-only sub-agents call the SAME shared
// *OpenAICompatBackend concurrently (Fire -> OperatorApply -> chat). chat mutates shared state — the
// Calls/Fallbacks/degradedLogged counters, the Log ring, and the scheduler's budget counters — which
// were previously UNGUARDED. Run under `go test -race` this fails loudly on the old code and passes on
// the mutex-guarded code. It also asserts no counter updates are LOST (every call is accounted for),
// which a data race would silently corrupt.
//
// NOTE: this is the GPU-free proof of race-freedom. It does NOT assert serial==parallel RESULT
// determinism under budget contention — that is a separate, still-open property (the parallel
// pre-fire path also fires below-theta sub-agents and grants background budget in completion order),
// which is why THOUGHT_PARALLEL_PHASES stays default-OFF. See 07 §A.1.
func TestBackendConcurrentChatRaceFree(t *testing.T) {
	fc := newFakeChat(t, chatResponse("ok move", "", "stop", nil)) // always a valid completion
	be := NewOpenAICompat(Options{BaseURL: fc.server.URL + "/v1", Model: "m", Timeout: 2 * time.Second})

	// Thread-safe emit (the shared bus is mutex-safe in production; here a guarded counter avoids the
	// test helper's deliberately-racy slice append). Exercises b.emit concurrently.
	var emitMu sync.Mutex
	emitCount := 0
	be.BindEmit(func(kind, summary string, data map[string]any) events.Event {
		emitMu.Lock()
		emitCount++
		emitMu.Unlock()
		return events.Event{}
	})

	// A small per-tick background budget so SOME calls are granted (-> reach the model -> Calls++) and
	// the rest are deferred (-> Deferred++): both shared-write paths run under the goroutine storm.
	const budget = 8
	sch := scheduler.New(nil, &scheduler.Config{BgBudget: budget, BgBudgetIdle: budget, EngageValue: 0.0})
	sch.TickReset(1.0, true) // remaining = budget
	be.BindScheduler(sch)

	const N = 64
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// "operator.<role>" is a BACKGROUND role -> subject to the budget; some grant, some defer.
			_ = be.OperatorApply("skeptic", "responsibility", "intent", "general", "goal", ctxR())
		}()
	}
	wg.Wait()

	// Accounting integrity: every call either spent the model (granted+succeeded on the fake server)
	// or was deferred. A data race on these counters would drop updates and break the sum.
	if be.Calls+sch.Deferred != N {
		t.Fatalf("lost counter updates under concurrency: Calls=%d + Deferred=%d != N=%d", be.Calls, sch.Deferred, N)
	}
	// Exactly `budget` background calls are granted; the fake server makes them all succeed.
	if be.Calls != budget {
		t.Fatalf("expected exactly budget(%d) granted+succeeded calls, got Calls=%d (Deferred=%d)", budget, be.Calls, sch.Deferred)
	}
}
