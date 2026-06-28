package campaign

// bencher.go — the capability probe (B2): the held-out task suite run through a fresh engine per
// task on the chosen substrate, scored per-item (oracle or grounded-success) with the model tokens
// summed from the llm.call event stream. This is the with-batch-vs-baseline arm the keep-rule reads;
// the engine factory is injected so the probe runs free on the test double and real on Claude.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// HeldOutTask is one capability item: a goal, and how to score success. Expect is a substring the
// answer must contain (a simple oracle); an empty Expect falls back to GROUNDED-SUCCESS (the episode
// imported a reality observation — it didn't just answer from priors).
type HeldOutTask struct {
	Goal   string
	Expect string
}

// EngineBencher runs the held-out suite. NewEngine builds a FRESH engine for a given registry state
// dir ("" = the baseline arm; a batch dir = the with-batch arm) on the chosen backend — injected so
// the probe is testable on the deterministic double and runnable on Claude.
type EngineBencher struct {
	Tasks     []HeldOutTask
	MaxTicks  int
	NewEngine func(stateDir string) (*engine.Engine, error)

	// MaxTokens / MaxCalls bound ONE arm's spend (0 = unbounded) — the campaign's cost ceiling on
	// the metered substrate. The probe aborts loudly the moment a running total crosses, so a batch
	// can never silently overrun (the W4 cost-guard analogue for the held-out probe).
	MaxTokens int
	MaxCalls  int

	// Concurrency runs the held-out tasks through N worker goroutines (the parallel-scaling throughput
	// lever, mirroring cmd/bench/pool.go). The tasks are INDEPENDENT — each gets its own fresh engine +
	// backend + read-only baseline store (NewEngine), so concurrent claude -p spawns share no mutable
	// state. PerItem is written into a fixed per-task SLOT (order preserved for the paired McNemar), and
	// the token/call sums are order-independent — so a SUCCESSFUL run is byte-identical regardless of N.
	// 0/1 = serial (degenerate, identical to the old loop). Tune N to the substrate's rate limit.
	Concurrency int
}

// Bench runs every task through a fresh engine seeded with stateDir, returns the paired per-item
// pass/fail + the summed prompt+completion tokens (0 on the offline double). With Concurrency>1 the tasks
// run in parallel (independent engines); PerItem keeps task order (paired McNemar), the sums are
// order-independent, so the result is identical to the serial path. A budget cap trips a HARD abort (the
// feeder stops dispatching, in-flight tasks drain) returning the error — same as serial: the batch aborts.
func (b EngineBencher) Bench(stateDir string) (ArmResult, error) {
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	n := b.Concurrency
	if n < 1 {
		n = 1
	}

	perItem := make([]bool, len(b.Tasks))
	var (
		mu                                 sync.Mutex
		totTokens, totCompletion, totCalls int
		budgetErr                          error
		aborted                            bool
	)
	// runOne executes ONE task on a fresh engine (the slow part — parallel, no shared state) and returns
	// its score + per-task token/call counts (total + completion-only, the cache-immune efficiency signal).
	runOne := func(i int) (solved bool, tokens, completion, calls int, err error) {
		eng, err := b.NewEngine(stateDir)
		if err != nil {
			return false, 0, 0, 0, err
		}
		eng.Bus().Subscribe(func(ev events.Event) {
			// engine is serial within one run, so this closure is single-threaded per task
			addLLMCost(ev, &calls, &tokens, &completion)
		})
		groundBefore := eng.Grounding().Len()
		eng.SubmitDefault(b.Tasks[i].Goal)
		eng.Run(maxTicks)
		return scoreSolvedEngine(b.Tasks[i], eng, eng.Grounding().Len() > groundBefore), tokens, completion, calls, nil
	}

	in := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for i := range in {
				solved, tokens, completion, calls, err := runOne(i) // parallel: no shared state
				mu.Lock()                                           // serialized: slot write + running totals + budget
				if err != nil {
					if budgetErr == nil {
						budgetErr = err
					}
					aborted = true
				} else {
					perItem[i] = solved
					totTokens += tokens
					totCompletion += completion
					totCalls += calls
					if b.MaxTokens > 0 && totTokens >= b.MaxTokens {
						if budgetErr == nil {
							budgetErr = fmt.Errorf("campaign probe BUDGET EXCEEDED: %d tokens reached the cap %d", totTokens, b.MaxTokens)
						}
						aborted = true
					}
					if b.MaxCalls > 0 && totCalls >= b.MaxCalls {
						if budgetErr == nil {
							budgetErr = fmt.Errorf("campaign probe BUDGET EXCEEDED: %d calls reached the cap %d", totCalls, b.MaxCalls)
						}
						aborted = true
					}
				}
				mu.Unlock()
			}
		}()
	}
	// FEEDER: dispatch tasks until done OR a budget/engine abort. On abort, stop sending NEW tasks; the
	// workers drain whatever is in flight, the channel closes, Wait returns.
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
		return ArmResult{}, budgetErr // hard abort — the batch aborts (same as serial)
	}
	return ArmResult{PerItem: perItem, Tokens: totTokens, CompletionTokens: totCompletion, Calls: totCalls}, nil
}

// ProbeResult is one gap-probe row: a task, whether the baseline harness SOLVED it, and what it produced
// (so a FAILURE is diagnosable — what gap it reveals). Phase-1 gap-finding: the failures are the ranked
// target list for registry scaling (where adding skills/operators/knowledge would LIFT vs sit idle).
type ProbeResult struct {
	Goal       string
	Expect     string
	Pass       bool
	Grounded   bool
	Answer     string // the harness's final response (snippet), for diagnosing the gap
	Tokens     int    // prompt + completion (total) summed over the task's llm.call events
	Completion int    // completion ONLY — the CACHE-IMMUNE replay-cost / Skill-Miner-curve signal: a minted
	//                    skill that recalls a Program instead of re-synthesising drops THIS on the reuse,
	//                    at held utility (the W5 definition-of-done; gate on completion-tokens, not answers)
	Calls int
}

// Probe runs the BASELINE harness over the suite (one arm, no candidates) and returns per-task detail —
// the Phase-1 gap-finding pass. Parallel over Concurrency workers (the #34 throughput lever), order
// preserved. Unlike Bench it captures the answer + grounded flag per task so a failure is diagnosable.
// stateDir seeds the baseline registry ("" = the default live state via NewEngine).
func (b EngineBencher) Probe(stateDir string) []ProbeResult {
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	n := b.Concurrency
	if n < 1 {
		n = 1
	}
	out := make([]ProbeResult, len(b.Tasks))
	in := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for i := range in {
				t := b.Tasks[i]
				r := ProbeResult{Goal: t.Goal, Expect: t.Expect}
				eng, err := b.NewEngine(stateDir)
				if err != nil {
					r.Answer = "ENGINE ERROR: " + err.Error()
					out[i] = r
					continue
				}
				eng.Bus().Subscribe(func(ev events.Event) {
					addLLMCost(ev, &r.Calls, &r.Tokens, &r.Completion)
				})
				groundBefore := eng.Grounding().Len()
				eng.SubmitDefault(t.Goal)
				eng.Run(maxTicks)
				r.Grounded = eng.Grounding().Len() > groundBefore
				r.Pass = scoreSolvedEngine(t, eng, r.Grounded)
				r.Answer = eng.LastResponse()
				out[i] = r // distinct slot per worker — no shared write, no lock needed
			}
		}()
	}
	for i := range b.Tasks {
		in <- i
	}
	close(in)
	wg.Wait()
	return out
}

// ProbeStability is one ANSWER-ORACLE task's NOISE-FLOOR row over K replays — the answer-path mirror of
// CogStability (A1 instrument gap). The model is non-deterministic on claude, so a task that solves
// run-to-run-flips is instrument noise, not signal (Phase-0 verifier characterization). Solved/Grounded
// count the replays that passed; Completion sums the cache-immune output-token cost across ALL replays.
type ProbeStability struct {
	Goal       string
	Expect     string
	Solved     int // of Replays — how many replays passed the oracle/grounded check
	Grounded   int // of Replays — how many replays imported a reality observation
	Replays    int
	Completion int // total completion (output) tokens summed over ALL replays — the cache-immune replay-cost
	//             signal (W5-0b); its per-replay mean (MeanCompletion) is the Skill-Miner curve y-axis, and
	//             the run-to-run variance IS the cost noise floor. 0 on the offline test double (no real usage).
	Completions []int // the PER-REPLAY completion-token vector (length == Replays once the run completes) — the
	//                    raw cost samples the ruler reduces into the WITHIN-task cost-σ noise floor (W5-1 cost axis).
	//                    Completion == sum(Completions); the vector is additive over the existing sum, never a
	//                    replacement. All zeros on the offline test double (no real usage → cost-σ=0 → honest DEGENERATE).
}

// SolvedRate is the per-task pass fraction over the K replays (the noise-floored capability rate).
func (s ProbeStability) SolvedRate() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.Solved) / float64(s.Replays)
}

// GroundedRate is the per-task grounded-success fraction over the K replays.
func (s ProbeStability) GroundedRate() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.Grounded) / float64(s.Replays)
}

// MeanCompletion is the per-replay average completion (output) token cost over the K replays — the
// cache-immune replay-cost the Skill-Miner curve tracks (0 on the offline test double).
func (s ProbeStability) MeanCompletion() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.Completion) / float64(s.Replays)
}

// Unstable reports whether the task's solved verdict FLIPPED across replays (passed at least once but
// not every time) — the per-task noise the instrument carries on a non-deterministic substrate.
func (s ProbeStability) Unstable() bool {
	return s.Solved != 0 && s.Solved != s.Replays
}

// ProbeReplays runs each suite task K times and aggregates the noise floor — the answer-oracle mirror of
// CognitionProbeReplays (A1 instrument gap). Per task it reports the solved-rate (fraction of replays that
// passed), the grounded-rate, the per-task mean completion-token cost, and the per-task variance/band
// (Unstable). Parallel over (task × replay) units on Concurrency workers (the #34 throughput lever); the
// seed is fixed, so on the deterministic test double all replays match (zero noise), while on claude the
// model's variance shows as a per-task solved-rate, gating how big a config delta must be to be trusted.
func (b EngineBencher) ProbeReplays(stateDir string, replays int) []ProbeStability {
	if replays < 1 {
		replays = 1
	}
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	n := b.Concurrency
	if n < 1 {
		n = 1
	}
	out := make([]ProbeStability, len(b.Tasks))
	for i := range b.Tasks {
		out[i] = ProbeStability{Goal: b.Tasks[i].Goal, Expect: b.Tasks[i].Expect, Replays: replays}
	}
	solves := make([]int, len(b.Tasks))
	grounds := make([]int, len(b.Tasks))
	comps := make([]int, len(b.Tasks)) // completion (output) tokens summed across ALL replays of each task
	// compVec[task][rep] is the PER-REPLAY completion total — written into a fixed (task,rep) slot so the
	// vector is order-independent of the worker schedule (deterministic on the test double; the within-task
	// cost-σ the ruler reduces does not depend on which worker ran which replay).
	compVec := make([][]int, len(b.Tasks))
	for i := range compVec {
		compVec[i] = make([]int, replays)
	}
	var mu sync.Mutex
	type unit struct{ task, rep int }
	in := make(chan unit)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for u := range in {
				t := b.Tasks[u.task]
				var calls, tokens, completion int
				eng, err := b.NewEngine(stateDir)
				if err != nil {
					continue // an engine-build failure scores as a non-pass for this replay (no fabricated success)
				}
				eng.Bus().Subscribe(func(ev events.Event) {
					// engine is serial within one run, so this closure is single-threaded per (task,replay)
					addLLMCost(ev, &calls, &tokens, &completion)
				})
				groundBefore := eng.Grounding().Len()
				eng.SubmitDefault(t.Goal)
				eng.Run(maxTicks)
				grounded := eng.Grounding().Len() > groundBefore
				solved := scoreSolvedEngine(t, eng, grounded)
				mu.Lock()
				if solved {
					solves[u.task]++
				}
				if grounded {
					grounds[u.task]++
				}
				comps[u.task] += completion         // accumulate the replay's cache-immune cost (0 on the test double)
				compVec[u.task][u.rep] = completion // record the per-replay sample in its fixed slot (cost-σ input)
				mu.Unlock()
			}
		}()
	}
	for i := range b.Tasks {
		for r := 0; r < replays; r++ {
			in <- unit{i, r}
		}
	}
	close(in)
	wg.Wait()
	for i := range b.Tasks {
		out[i].Solved = solves[i]
		out[i].Grounded = grounds[i]
		out[i].Completion = comps[i]
		out[i].Completions = compVec[i]
	}
	return out
}

// DecodeProbeRow is one task's GATE-1 per-role decode result: the goal, the full per-role
// breakdown (the grouped completion-token fold), and whether the episode grounded/solved (a
// sanity tag — a per-role number off a task that did NOTHING is meaningless). The verdict reads
// SynthShare/SynthCompletion — synthesize_program's share of, and absolute, decode.
type DecodeProbeRow struct {
	Goal            string
	Breakdown       DecodeBreakdown
	SynthShare      float64 // synthesize_program's fraction of the task's total decode
	SynthCompletion int     // synthesize_program's completion (output) tokens on this task
	Grounded        bool
	Solved          bool
}

// DecodeProbe runs each task through a fresh engine and captures the PER-ROLE decode breakdown
// (GATE-1: where do the completion tokens go, and how big is synthesize_program's share). It is
// the per-role analogue of Probe — same fresh-engine-per-task, same Concurrency pool, order
// preserved — but it subscribes a DecodeAggregator (group-by-role) instead of the flat
// addLLMCost counters. Each task gets its OWN aggregator (per fresh engine), so concurrent runs
// share no mutable state and the result is order-independent. stateDir seeds the baseline
// registry ("" = bare harness). Use this on the HEAVIEST synthesis/planning tasks (design /
// decompose family) — where SynthesizeProgram should be largest — to read the synthesis share.
func (b EngineBencher) DecodeProbe(stateDir string) []DecodeProbeRow {
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	n := b.Concurrency
	if n < 1 {
		n = 1
	}
	out := make([]DecodeProbeRow, len(b.Tasks))
	in := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for i := range in {
				t := b.Tasks[i]
				row := DecodeProbeRow{Goal: t.Goal}
				eng, err := b.NewEngine(stateDir)
				if err != nil {
					out[i] = row // an engine-build failure scores as an empty breakdown (no fabricated cost)
					continue
				}
				agg := NewDecodeAggregator()
				eng.Bus().Subscribe(func(ev events.Event) {
					agg.Fold(ev) // engine is serial within one run → this closure is single-threaded per task
				})
				groundBefore := eng.Grounding().Len()
				eng.SubmitDefault(t.Goal)
				eng.Run(maxTicks)
				row.Grounded = eng.Grounding().Len() > groundBefore
				row.Solved = scoreSolvedEngine(t, eng, row.Grounded)
				row.Breakdown = agg.Breakdown()
				row.SynthShare = row.Breakdown.ShareOf("synthesize_program")
				row.SynthCompletion = row.Breakdown.CompletionOf("synthesize_program")
				out[i] = row // distinct slot per worker — no shared write, no lock needed
			}
		}()
	}
	for i := range b.Tasks {
		in <- i
	}
	close(in)
	wg.Wait()
	return out
}

// scoreSolved decides whether a task was solved: the oracle substring when given, else
// grounded-success (the episode imported reality, not just answered from priors).
func scoreSolved(t HeldOutTask, answer string, grounded bool) bool {
	if t.Expect != "" {
		return strings.Contains(strings.ToLower(answer), strings.ToLower(t.Expect))
	}
	return grounded
}

// scoreSolvedEngine is the robust oracle: the Expect substring may appear in the final answer OR in
// ANY thought the episode reached (the answer surfaced internally — robust to a flaky voicing role
// and the place recalled knowledge lands), else grounded-success. This decouples the capability
// measure from the Respond role's reliability.
func scoreSolvedEngine(t HeldOutTask, eng *engine.Engine, grounded bool) bool {
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

// addLLMCost folds ONE llm.call event into the running cost counters: one more call, its total
// (prompt+completion) tokens, and its completion-ONLY tokens — the cache-immune replay-cost /
// Skill-Miner-curve signal (a minted skill that recalls a Program instead of re-synthesising drops
// THIS at held utility; gate on completion, not answers). Non-llm.call events are ignored. This is the
// SINGLE source of the probe/bench cost wiring — `Probe`, `Bench`, and `scoreCognition` all call it, so a
// regression in completion accounting fails in exactly one place (TestAddLLMCost), not three. Any nil
// pointer is skipped (scoreCognition tracks no token totals).
func addLLMCost(ev events.Event, calls, tokens, completion *int) {
	if ev.Kind != events.LLM {
		return
	}
	if calls != nil {
		*calls++
	}
	c := intData(ev.Data, "completion_tokens") // the cache-immune output cost (source of truth: llm.call event, internal/llm/openai.go)
	if completion != nil {
		*completion += c
	}
	if tokens != nil {
		*tokens += intData(ev.Data, "prompt_tokens") + c
	}
}

// intData reads an integer-valued event Data field tolerantly (in-process emit stores int; a
// JSON round-trip stores float64). Negative/absent → 0 (the "-1 = absent" token convention).
func intData(d events.D, key string) int {
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
