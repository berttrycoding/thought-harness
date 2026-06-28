package scaling

// tier2_bench.go — the REAL wiring of the funnel's Tier-2 LIFT RUNNER into internal/engine. The funnel
// keeps Tier-2 a leaf by INJECTING a funnel.LiftBench; this is the production implementation of that
// interface (it imports the engine + the event bus, which the funnel leaf must not). It runs the
// held-out lift suite per task on a FRESH engine seeded with the chosen registry state dir ("" =
// baseline arm, a staged dir = with-batch arm) and returns the PER-ITEM funnel.ArmStats the keep-or-
// revert decision pairs over.
//
// THE METRIC IS COMPLETION TOKENS PER ITEM (the cache-immune efficiency signal): each task's completion
// tokens are summed off its own llm.call event stream, exactly as the campaign bencher accounts them,
// and recorded per item so the funnel can compute completion-tokens-per-solved at held utility. Total /
// cached-input tokens are deliberately NOT carried — they are dominated by cached input on a high-cache
// substrate and would read as substrate noise (research §"Code gaps" #3).
//
// This shares the held-out task shape (campaign.HeldOutTask) + the same engine-factory injection +
// per-task token accounting as internal/campaign's EngineBencher; it does NOT rewrite that bencher (the
// campaign keeps its summed-arm Bench for its own keep-rule). It is the bridge that makes the funnel's
// Tier-2 a thing that RUNS on a real engine, offline-testable via a fake engine factory.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/campaign"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/funnel"
)

// EngineLiftBench implements funnel.LiftBench over a real engine factory. It runs each held-out task
// through a fresh engine and records the per-item solved verdict + per-item completion-token spend (the
// cache-immune efficiency signal). NewEngine is injected so the lift runs free on the deterministic test
// double and real on the chosen substrate behind the cost guard.
type EngineLiftBench struct {
	// Tasks is the held-out lift suite (the same shape as the campaign bencher's). REQUIRED.
	Tasks []campaign.HeldOutTask
	// NewEngine builds a FRESH engine seeded with stateDir ("" = baseline). REQUIRED.
	NewEngine func(stateDir string) (*engine.Engine, error)
	// MaxTicks bounds one task's episode (0 ⇒ 40, matching the campaign bencher's default).
	MaxTicks int
	// Concurrency runs the tasks through N workers (independent fresh engines, no shared mutable state).
	// 0/1 = serial. PerItem keeps task order (the paired McNemar), so a run is order-independent.
	Concurrency int
	// MaxTokens / MaxCalls bound ONE arm's TOTAL spend (0 = unbounded) — the cost ceiling on a metered
	// substrate. The arm aborts loudly (returns an error → the batch aborts) the moment a running total
	// crosses, so a Tier-2 lift can never silently overrun. (Budgeting uses TOTAL tokens; the per-item
	// METRIC is completion-only.)
	MaxTokens int
	MaxCalls  int
}

// BenchArm runs the suite for one arm and returns the per-item funnel.ArmStats (solved + completion
// tokens per task). It implements funnel.LiftBench. An engine-construction failure or a budget overrun
// returns an error — the batch then aborts, never a silent keep.
func (b EngineLiftBench) BenchArm(stateDir string) (funnel.ArmStats, error) {
	if b.NewEngine == nil {
		return funnel.ArmStats{}, fmt.Errorf("scaling: EngineLiftBench has no NewEngine factory")
	}
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	n := b.Concurrency
	if n < 1 {
		n = 1
	}

	perItemPass := make([]bool, len(b.Tasks))
	perItemCompletion := make([]int, len(b.Tasks))
	var (
		mu                  sync.Mutex
		totTokens, totCalls int
		budgetErr           error
		aborted             bool
	)

	// runOne executes ONE task on a fresh engine (the slow part — parallel, no shared state) and returns
	// its solved verdict + per-task TOTAL tokens (for budgeting) + per-task COMPLETION tokens (the metric)
	// + per-task call count.
	runOne := func(i int) (solved bool, totalTokens, completion, calls int, err error) {
		eng, err := b.NewEngine(stateDir)
		if err != nil {
			return false, 0, 0, 0, err
		}
		eng.Bus().Subscribe(func(ev events.Event) {
			if ev.Kind != events.LLM {
				return
			}
			calls++ // engine is serial within one run, so this closure is single-threaded per task
			c := intDataEvent(ev.Data, "completion_tokens")
			totalTokens += intDataEvent(ev.Data, "prompt_tokens") + c
			completion += c
		})
		groundBefore := eng.Grounding().Len()
		eng.SubmitDefault(b.Tasks[i].Goal)
		eng.Run(maxTicks)
		return scoreTask(b.Tasks[i], eng, eng.Grounding().Len() > groundBefore), totalTokens, completion, calls, nil
	}

	in := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for i := range in {
				solved, total, completion, calls, err := runOne(i) // parallel: no shared state
				mu.Lock()                                          // serialized: slot write + running totals + budget
				if err != nil {
					if budgetErr == nil {
						budgetErr = err
					}
					aborted = true
				} else {
					perItemPass[i] = solved
					perItemCompletion[i] = completion
					totTokens += total
					totCalls += calls
					if b.MaxTokens > 0 && totTokens >= b.MaxTokens {
						if budgetErr == nil {
							budgetErr = fmt.Errorf("Tier-2 lift BUDGET EXCEEDED: %d tokens reached the cap %d", totTokens, b.MaxTokens)
						}
						aborted = true
					}
					if b.MaxCalls > 0 && totCalls >= b.MaxCalls {
						if budgetErr == nil {
							budgetErr = fmt.Errorf("Tier-2 lift BUDGET EXCEEDED: %d calls reached the cap %d", totCalls, b.MaxCalls)
						}
						aborted = true
					}
				}
				mu.Unlock()
			}
		}()
	}
	// FEEDER: dispatch tasks until done OR a budget/engine abort. On abort, stop sending NEW tasks; the
	// workers drain in-flight, the channel closes, Wait returns.
	for i := range b.Tasks {
		mu.Lock()
		ab := aborted
		mu.Unlock()
		if ab {
			break
		}
		in <- i
	}
	close(in)
	wg.Wait()

	if budgetErr != nil {
		return funnel.ArmStats{}, budgetErr // hard abort — the batch aborts
	}
	return funnel.ArmStats{PerItem: perItemPass, CompletionPerItem: perItemCompletion}, nil
}

// scoreTask decides whether a held-out task was solved on this engine: the Expect substring (in the
// final answer OR any reached thought — robust to a flaky voicing role), else grounded-success. Mirrors
// the campaign bencher's scoreSolvedEngine semantics (kept local so scaling does not depend on an
// unexported campaign helper).
func scoreTask(t campaign.HeldOutTask, eng *engine.Engine, grounded bool) bool {
	if t.Expect == "" {
		return grounded
	}
	want := strings.ToLower(t.Expect)
	if strings.Contains(strings.ToLower(eng.LastResponse()), want) {
		return true
	}
	if g := eng.Graph(); g != nil {
		for _, th := range g.History() {
			if strings.Contains(strings.ToLower(th.Text), want) {
				return true
			}
		}
	}
	return false
}

// intDataEvent reads an integer-valued event Data field tolerantly (in-process emit stores int; a JSON
// round-trip stores float64). Negative/absent → 0 (the "-1 = absent" token convention).
func intDataEvent(d events.D, key string) int {
	switch v := d[key].(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return 0
}
