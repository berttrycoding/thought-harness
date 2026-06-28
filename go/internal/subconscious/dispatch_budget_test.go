package subconscious

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/scheduler"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// fakeModel is a minimal thread-safe OpenAI-compatible completions server counting its hits.
type fakeModel struct {
	mu   sync.Mutex
	hits int
}

func (f *fakeModel) handler(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/models") {
		io.WriteString(w, `{"data":[{"id":"fake"}]}`)
		return
	}
	f.mu.Lock()
	f.hits++
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"a real reasoned move"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
}

func (f *fakeModel) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.hits }

// budgetHarness builds: a fake model server, an llm backend bound to a background budget of `budget`,
// a SubconsciousEngine with the scheduler wired, and `n` reason-only sub-agents.
func budgetHarness(t *testing.T, budget, n int) (*fakeModel, *SubconsciousEngine, []*SubAgent) {
	t.Helper()
	fm := &fakeModel{}
	srv := httptest.NewServer(http.HandlerFunc(fm.handler))
	t.Cleanup(srv.Close)
	be := llm.NewOpenAICompat(llm.Options{BaseURL: srv.URL + "/v1", Model: "fake", Timeout: 2 * time.Second})
	sched := scheduler.New(nil, &scheduler.Config{BgBudget: budget, BgBudgetIdle: budget, EngageValue: 0})
	sched.TickReset(1.0, true)
	be.BindScheduler(sched)

	e := NewSubconsciousEngine(nil, cpyrand.New(7), nil, nil, nil)
	e.SetScheduler(sched)

	catalog := cognition.NewOperatorRegistry()
	spec, _ := catalog.Get("hypothesize") // a reason-only seed operator
	var sas []*SubAgent
	for i := 0; i < n; i++ {
		sas = append(sas, NewSubAgent(spec, "general", "exercise the fan-out", be, nil,
			"sa:"+itoaB(i), nil, nil, nil))
	}
	return fm, e, sas
}

func itoaB(i int) string { return string(rune('a' + i)) }

// TestPreFireParallelBudgetDeterministic (07 §A.1 task #9 part 2): with background budget 2 and a
// 4-wide reason-only fan-out, the pre-fire concurrency is SIZED to the budget — exactly the FIRST 2
// (step order) reach the model, the rest fall to the serial loop where the spent budget denies them
// into the deterministic placeholder path. Identical outcome across repeated runs (no completion-
// order lottery for grants).
func TestPreFireParallelBudgetDeterministic(t *testing.T) {
	ctx := []types.Thought{{ID: 1, Text: "exercise the fan-out", Source: types.GENERATED}}
	for run := 0; run < 8; run++ {
		fm, e, sas := budgetHarness(t, 2, 4)
		pre := e.preFireParallel(ctx, sas, 0.3, nil) // theta=0.3 < eff (0.9) ⇒ all 4 admitted (gap #1 no-op here)
		if len(pre) != 2 {
			t.Fatalf("run %d: pre-fired set must be sized to the budget (2), got %d", run, len(pre))
		}
		if _, ok := pre[sas[0]]; !ok {
			t.Fatalf("run %d: allocation must be first-k in step order (sa[0] missing)", run)
		}
		if _, ok := pre[sas[1]]; !ok {
			t.Fatalf("run %d: allocation must be first-k in step order (sa[1] missing)", run)
		}
		if got := fm.count(); got != 2 {
			t.Fatalf("run %d: exactly 2 model calls expected from the granted set, got %d", run, got)
		}
		// the remainder fire SERIALLY (the roster loop's path) and are denied -> placeholder text.
		rng := cpyrand.New(7)
		for i := 2; i < 4; i++ {
			c := e.fireRosterEntry(sas[i], ctx, pre)
			if c == nil {
				t.Fatalf("run %d: a denied branch still yields the placeholder candidate", run)
			}
			if !strings.HasPrefix(c.Text, "[") {
				t.Fatalf("run %d: denied branch must carry the placeholder, got %q", run, c.Text)
			}
			_ = rng
		}
		if got := fm.count(); got != 2 {
			t.Fatalf("run %d: the denied serial branches must NOT reach the model, still 2 calls, got %d", run, got)
		}
	}
}

// TestPreFireParallelRespectsTheta (GAP #1, real-backend): the pre-fire dispatches ONLY the steps the
// serial loop would admit (eff>theta). The seed sub-agents have relevance 0.9; with theta raised above
// that (0.95) the admitted set is EMPTY, so a correct pre-fire returns nil and makes ZERO model calls.
// The pre-θ-fix code pre-fired the whole group regardless of theta — it would make 4 model calls here.
// fm.count()==0 is the decisive assertion (it FAILS on the bug, PASSES on the fix).
func TestPreFireParallelRespectsTheta(t *testing.T) {
	ctx := []types.Thought{{ID: 1, Text: "exercise the fan-out", Source: types.GENERATED}}
	for run := 0; run < 8; run++ {
		fm, e, sas := budgetHarness(t, 99, 4) // ample budget: a model call would happen but for the theta gate
		pre := e.preFireParallel(ctx, sas, 0.95, nil)
		if pre != nil {
			t.Fatalf("run %d: theta=0.95 > eff(0.9) ⇒ nothing admitted, pre-fire must decline (got %d)", run, len(pre))
		}
		if got := fm.count(); got != 0 {
			t.Fatalf("run %d: theta-skipped pre-fire made %d model calls; expected 0 (gap #1: extra calls)", run, got)
		}
	}
	// And a PARTIAL admission: theta between two relevances would admit only the higher ones. The seed
	// sub-agents are all 0.9, so a partial split needs differing relevances — covered by the package-level
	// TestParallelPhasesPreFireRespectsTheta (high-theta whole-group skip via the dispatch loop). Here we
	// pin the all-or-nothing real-backend boundary, which is the call-count-decisive case.
}

// TestPreFireParallelBudgetOneOrZeroGoesSerial: a budget below 2 makes concurrency pointless — the
// pre-fire declines entirely (nil) and the serial loop handles every branch.
func TestPreFireParallelBudgetOneOrZeroGoesSerial(t *testing.T) {
	ctx := []types.Thought{{ID: 1, Text: "exercise the fan-out", Source: types.GENERATED}}
	for _, budget := range []int{0, 1} {
		_, e, sas := budgetHarness(t, budget, 4)
		if pre := e.preFireParallel(ctx, sas, 0.3, nil); pre != nil {
			t.Fatalf("budget %d: pre-fire must decline (serial handles all), got %d", budget, len(pre))
		}
	}
}

// TestPreFireParallelNoSchedulerUnbounded: with no scheduler wired (the test/offline path) the whole
// group pre-fires — the existing determinism contract is unchanged.
func TestPreFireParallelNoSchedulerUnbounded(t *testing.T) {
	ctx := []types.Thought{{ID: 1, Text: "exercise the fan-out", Source: types.GENERATED}}
	fm, e, sas := budgetHarness(t, 99, 4)
	e.SetScheduler(nil)
	pre := e.preFireParallel(ctx, sas, 0.3, nil) // theta=0.3 < eff (0.9) ⇒ all 4 admitted
	if len(pre) != 4 {
		t.Fatalf("nil scheduler must pre-fire the whole group, got %d", len(pre))
	}
	if got := fm.count(); got != 4 {
		t.Fatalf("all 4 should reach the model (the backend's own budget of 99 covers them), got %d", got)
	}
}
