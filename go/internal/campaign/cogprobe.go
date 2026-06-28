package campaign

import (
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// cogprobe.go — the COGNITION gap-finder. This is a Silent-Injection cognitive architecture, not a calculator, so a
// gap is a missing cognitive FACULTY, not a wrong answer. Each task is designed to ELICIT one faculty, and
// the score is whether the faculty's signature FIRED in the run (the event stream + graph), NOT whether
// the final answer string matched an oracle. (The reality-access probe that returned "" instead of a
// confabulated number is the motivating case: answer-WRONG but cognition-RIGHT.)

// CognitionTask is one faculty probe: a goal crafted to elicit a cognitive move, and the Signature that
// move leaves. Signatures: branch (explore alternatives), act (consult reality when stuck), honest
// (resist hallucination — refuse to confabulate), conflict (fork on a genuine two-sided case), decompose
// (break a complex goal into structure), deliberate (sustained multi-step thinking, not one-shot).
//
// OUTCOME-TIE (the v2 validity fix). The legacy probe scored a task purely on whether the faculty's
// signature FIRED (process detection) — outcome-decoupled and gameable: any lever that emits the counted
// event "passes" by construction. A v2 faculty task carries an OBJECTIVE OUTCOME ORACLE (the realhard
// oracle kinds, reused verbatim so the scorer is the same mutation-tested code) so a run can be scored on
// "faculty fired AND the outcome improved", not "faculty fired". The oracle fields are OPTIONAL and
// ADDITIVE: a task with an empty Oracle is the legacy fire-only probe (byte-identical), so the original
// cognition-probe-001 suite parses + scores unchanged. The deliberate-trap tasks carry a fast-WRONG
// PriorLure (the System-1 answer) distinct from the correct Expected; the anti-confab tasks score
// OracleDecline (the correct behaviour is to decline, never confabulate the lure).
type CognitionTask struct {
	Goal      string `json:"goal"`
	Signature string `json:"signature"`
	Note      string `json:"note,omitempty"`

	// --- objective outcome oracle (OPTIONAL, additive — empty Oracle ⇒ legacy fire-only) ---

	// Oracle names how the ANSWER is scored against the ground truth, reusing the realhard oracle kinds
	// ("exact" | "numeric-tolerance" | "set-membership" | "decline"). Empty ⇒ no outcome tie (fire-only).
	Oracle string `json:"oracle,omitempty"`
	// Expected is the ground-truth answer (a number/token for exact/numeric, the canonical set joined by
	// space for set-membership, empty for decline).
	Expected string `json:"expected,omitempty"`
	// Normalizer names the canonicalizer applied before an exact compare ("number" | "token" | "lower" |
	// ""). numeric-tolerance always parses a number; set-membership always lowers+tokenizes.
	Normalizer string `json:"normalizer,omitempty"`
	// Tolerance is the absolute numeric tolerance for the numeric-tolerance oracle.
	Tolerance float64 `json:"tolerance,omitempty"`
	// PriorLure is the fast-WRONG System-1 / confabulated answer the task baits. For a deliberate-trap it
	// is the answer the naive (System-1) path lands on (distinct from Expected); for a decline task it is
	// the confident number a confabulating model emits. Asserting it is an automatic fail.
	PriorLure string `json:"prior_lure,omitempty"`
	// Family is an optional sub-family tag (e.g. "synthesis-heavy") so a sub-suite (the efficiency salvage)
	// can be filtered out without coupling the scorer to it. Empty ⇒ no sub-family.
	Family string `json:"family,omitempty"`
}

// HasOracle reports whether the task carries an objective outcome oracle (v2) vs being a legacy fire-only
// probe. The scorer ties Fired to the outcome only when this is true.
func (t CognitionTask) HasOracle() bool { return strings.TrimSpace(t.Oracle) != "" }

// CogResult is one cognition-probe row: did the expected faculty's signature fire, what DID fire (for
// diagnosis), and the answer + cost.
type CogResult struct {
	Goal       string
	Signature  string
	Fired      bool
	Observed   []string
	Answer     string
	Calls      int
	Completion int // completion (output) tokens summed over the task's llm.call events — the cache-immune
	//                replay-cost signal (W5 Skill-Miner curve); 0 on the offline test double (no real usage)

	// Aptness is the GRADED faculty-engagement score (GATE-2): the DEGREE the intended faculty
	// engaged, not the binary fired/not. It is the gate-2 instrument — a binary signature flips
	// 4/5 run-to-run (too noisy + saturated to detect a subtle lift), so this measures HOW MUCH a
	// faculty engaged (e.g. branch = how many forks, act = how many reality imports, deliberate =
	// how deep the chain) on a continuous 0..1 scale. It is bounded to [0,1] (saturating, so a
	// runaway count does not blow the scale) and is the score the ruler's graded path reduces into a
	// within-task σ / between-task-variance characterization. The binary Fired stays as the legacy
	// signal (byte-identical default); Aptness is purely additive.
	Aptness float64
	// AptnessParts is the per-faculty graded score for ALL six faculties (for diagnosis — a task
	// can engage a faculty other than its intended one). Aptness == AptnessParts[Signature].
	AptnessParts map[string]float64

	// --- OUTCOME-TIE (the v2 validity fix) ---

	// OutcomeTied is true when the task carried an objective outcome oracle (CognitionTask.HasOracle).
	// When false the row is the legacy fire-only probe (Outcome/Correct/FiredAndCorrect are zero).
	OutcomeTied bool
	// OutcomeSolved is whether the ANSWER was objectively correct per the task's oracle (realhard.Score).
	// Meaningful only when OutcomeTied; false otherwise.
	OutcomeSolved bool
	// OutcomeReason is the oracle's short explanation (for the report — why the answer solved/failed).
	OutcomeReason string
	// AssertedLure is true when the answer asserted the task's PriorLure — the System-1 / confabulation
	// signal (the deliberate-trap fell for the lure; the decline task confabulated). Reported for
	// diagnosis even when OutcomeSolved is already false for another reason.
	AssertedLure bool
	// FiredAndCorrect is the GATED faculty signal — the v2 outcome-tied metric: the intended faculty
	// fired AND the answer was objectively correct. This is what a faculty-lever A/B should gate on (NOT
	// bare Fired, which a counted-event lever games by construction). For a legacy fire-only task
	// (OutcomeTied false) it equals Fired (no oracle to gate against).
	FiredAndCorrect bool
}

// honestUnknownMarkers are phrases that signal the conscious HONESTLY declined to confabulate (the Filter /
// the never-fabricate discipline working) — the right move on an unknowable question.
var honestUnknownMarkers = []string{
	"i don't know", "i do not know", "cannot determine", "can't determine", "unable to",
	"no access", "don't have access", "cannot verify", "can't verify", "not sure", "unsure",
	"would need to", "without access", "insufficient", "cannot confirm", "unknown",
}

// CognitionProbe runs each task through a fresh BASELINE engine and scores whether the intended cognitive
// faculty FIRED — by watching the event stream (critic.decision moves, seam.filter/gate, conscious.mcp,
// action.*, subconscious.*, grounding) and the final graph shape. Parallel over Concurrency (the #34 pool),
// order preserved. NewEngine/MaxTicks/Concurrency are reused from the EngineBencher config the caller sets.
func (b EngineBencher) CognitionProbe(tasks []CognitionTask, stateDir string) []CogResult {
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	n := b.Concurrency
	if n < 1 {
		n = 1
	}
	out := make([]CogResult, len(tasks))
	in := make(chan int)
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for i := range in {
				out[i] = b.scoreCognition(tasks[i], stateDir, maxTicks)
			}
		}()
	}
	for i := range tasks {
		in <- i
	}
	close(in)
	wg.Wait()
	return out
}

// CogStability is one task's NOISE-FLOOR row: how many of K replays fired the intended faculty (the
// model is non-deterministic on claude, so a faculty that flips run-to-run is instrument noise, not
// signal — Phase-0 verifier characterization).
type CogStability struct {
	Goal       string
	Signature  string
	Fired      int // of Replays
	Replays    int
	Completion int // total completion (output) tokens summed over ALL replays — the cache-immune replay-cost
	//             signal whose run-to-run mean (MeanCompletion) is the Skill-Miner curve's y-axis; the per-task
	//             variance across replays IS the cost noise floor (W5-0b DoD: "completion-tokens with run-to-run
	//             variance"). 0 on the offline test double (no real usage).
	Completions []int // the PER-REPLAY completion-token vector (length == Replays once the run completes) — the
	//                    raw cost samples the ruler reduces into the WITHIN-task cost-σ noise floor (W5-1 cost axis).
	//                    Completion == sum(Completions); the vector is additive over the existing sum, never a
	//                    replacement. All zeros on the offline test double (cost-σ=0 → honest DEGENERATE on cost).

	// Aptness is the PER-REPLAY graded faculty-engagement vector (length == Replays) — the GATE-2
	// instrument. Where Fired is the binary fire-count (which flips 4/5 run-to-run on claude), this
	// is the continuous degree-of-engagement per replay [0,1]. Its WITHIN-task SD is the graded
	// noise floor σ; its BETWEEN-task spread is the real faculty-difference signal — the ruler's
	// graded path reduces this into a CLEARS-σ / RE-SATURATES verdict (does a graded signature
	// detect a faculty delta a binary signature cannot). Additive; nil/empty on the legacy path.
	Aptness []float64

	// --- OUTCOME-TIE (the v2 validity fix) ---

	// OutcomeTied is true when the task carried an objective outcome oracle.
	OutcomeTied bool
	// Correct is the count of the K replays whose ANSWER was objectively correct per the oracle. 0 on a
	// legacy fire-only task. The OUTCOME-tied lift axis: a faculty-lever A/B should move FiredAndCorrect,
	// not bare Fired.
	Correct int
	// FiredAndCorrect is the count of the K replays where the intended faculty fired AND the answer was
	// objectively correct — the GATED faculty signal (== Fired on a legacy fire-only task). This is the
	// binary success-count a future outcome-tied feasibility gate / A/B reduces over.
	FiredAndCorrect int
	// CorrectVec is the PER-REPLAY objective-correctness vector (length == Replays) — the raw 0/1 samples
	// for a future outcome-tied ruler reduction (mirrors Completions/Aptness). nil on the legacy path.
	CorrectVec []bool
}

// CorrectRate is the per-task objective-correctness fraction over the replays (0 on a fire-only task).
func (s CogStability) CorrectRate() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.Correct) / float64(s.Replays)
}

// FiredAndCorrectRate is the per-task GATED fraction (faculty fired AND outcome correct) over the
// replays — the outcome-tied analogue of Rate (which is fire-only).
func (s CogStability) FiredAndCorrectRate() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.FiredAndCorrect) / float64(s.Replays)
}

// MeanAptness is the per-replay average graded faculty-engagement over the K replays — the graded
// analogue of the binary Rate (degree of engagement, not fraction-fired).
func (s CogStability) MeanAptness() float64 {
	if len(s.Aptness) == 0 {
		return 0
	}
	var sum float64
	for _, a := range s.Aptness {
		sum += a
	}
	return sum / float64(len(s.Aptness))
}

// Rate is the per-task engagement fraction over the replays.
func (s CogStability) Rate() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.Fired) / float64(s.Replays)
}

// MeanCompletion is the per-replay average completion (output) token cost over the K replays — the
// cache-immune replay-cost the Skill-Miner curve tracks (0 on the offline test double).
func (s CogStability) MeanCompletion() float64 {
	if s.Replays == 0 {
		return 0
	}
	return float64(s.Completion) / float64(s.Replays)
}

// CognitionProbeReplays runs each task K times and counts how often its faculty fires — the Phase-0 noise
// floor (run-to-run stability on a non-deterministic substrate). Parallel over (task × replay) units. The
// seed is fixed, so on the deterministic test double all replays match (zero noise); on claude the model's
// variance shows as a per-task fire-rate, which gates how big a config delta must be to be trusted.
func (b EngineBencher) CognitionProbeReplays(tasks []CognitionTask, stateDir string, replays int) []CogStability {
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
	out := make([]CogStability, len(tasks))
	for i := range tasks {
		out[i] = CogStability{Goal: tasks[i].Goal, Signature: tasks[i].Signature, Replays: replays}
	}
	fires := make([]int, len(tasks))
	corrects := make([]int, len(tasks)) // objectively-correct answers summed across ALL replays (v2 outcome tie)
	fAndC := make([]int, len(tasks))    // faculty-fired-AND-correct summed across ALL replays (the gated metric)
	comps := make([]int, len(tasks))    // completion (output) tokens summed across ALL replays of each task
	// compVec[task][rep] is the PER-REPLAY completion total — written into a fixed (task,rep) slot so the
	// vector is order-independent of the worker schedule (the within-task cost-σ the ruler reduces does not
	// depend on which worker ran which replay). aptVec[task][rep] is the same for the GATE-2 graded aptness;
	// corrVec[task][rep] is the per-replay objective-correctness 0/1 (the outcome-tied σ input).
	compVec := make([][]int, len(tasks))
	aptVec := make([][]float64, len(tasks))
	corrVec := make([][]bool, len(tasks))
	tied := make([]bool, len(tasks))
	for i := range compVec {
		compVec[i] = make([]int, replays)
		aptVec[i] = make([]float64, replays)
		corrVec[i] = make([]bool, replays)
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
				r := b.scoreCognition(tasks[u.task], stateDir, maxTicks)
				mu.Lock()
				if r.Fired {
					fires[u.task]++
				}
				if r.OutcomeTied {
					tied[u.task] = true
					if r.OutcomeSolved {
						corrects[u.task]++
					}
				}
				if r.FiredAndCorrect {
					fAndC[u.task]++
				}
				corrVec[u.task][u.rep] = r.OutcomeSolved // per-replay objective correctness (outcome-tied σ input)
				comps[u.task] += r.Completion            // accumulate the replay's cache-immune cost (0 on the test double)
				compVec[u.task][u.rep] = r.Completion    // record the per-replay sample in its fixed slot (cost-σ input)
				aptVec[u.task][u.rep] = r.Aptness        // record the per-replay graded aptness (GATE-2 σ input)
				mu.Unlock()
			}
		}()
	}
	for i := range tasks {
		for r := 0; r < replays; r++ {
			in <- unit{i, r}
		}
	}
	close(in)
	wg.Wait()
	for i := range tasks {
		out[i].Fired = fires[i]
		out[i].Completion = comps[i]
		out[i].Completions = compVec[i]
		out[i].Aptness = aptVec[i]
		out[i].OutcomeTied = tied[i]
		out[i].Correct = corrects[i]
		out[i].FiredAndCorrect = fAndC[i]
		out[i].CorrectVec = corrVec[i]
	}
	return out
}

// scoreCognition runs ONE cognition task and detects which faculty signatures fired.
func (b EngineBencher) scoreCognition(t CognitionTask, stateDir string, maxTicks int) CogResult {
	r := CogResult{Goal: t.Goal, Signature: t.Signature}
	eng, err := b.NewEngine(stateDir)
	if err != nil {
		r.Answer = "ENGINE ERROR: " + err.Error()
		return r
	}
	// signal flags collected from the event stream (the engine is serial within a run, so this closure is
	// single-threaded per task — no lock needed). Calibrated (Phase-2): branch = an EXPLICIT graph fork (a
	// BRANCH decision / mcp branch / >1 branch), NOT inline reasoning; conflict = a REAL held conflict (the
	// gate's conflict flag, not any gate firing); deliberate = a sustained reasoning chain (graph depth).
	//
	// GATE-2 graded path: alongside each binary flag we COUNT the faculty's signature events (forks,
	// reality imports, decomposition steps, conflicts forked, filter catches). The count drives the graded
	// Aptness score (degree of engagement); the bool drives the legacy binary Fired (unchanged).
	var branched, acted, conflicted, decomposed, filtered bool
	var branchN, actN, conflictN, decomposeN, filterN int
	eng.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case string(events.LLM):
			addLLMCost(ev, &r.Calls, nil, &r.Completion) // nil tokens: the cognition probe tracks calls + completion (cache-immune cost), not the prompt-inflated total
		case string(events.Decision):
			switch strings.ToUpper(asStr(ev.Data["decision"])) {
			case "BRANCH":
				branched = true
				branchN++
			case "ACT":
				acted = true
				actN++
			}
		case string(events.MCP):
			if op := asStr(ev.Data["op"]); op == "branch" || op == "reenter" {
				branched = true
				branchN++
			}
		case string(events.Act), string(events.Observation), string(events.Ground):
			acted = true
			actN++
		case string(events.Gate):
			if c, ok := ev.Data["conflict"].(bool); ok && c {
				conflicted = true // a REAL held conflict — the gate forked the losers
				conflictN++
			}
		case string(events.SubWorkflow), string(events.SubSynthesize), string(events.SubOperator):
			decomposed = true
			decomposeN++
		case string(events.Filter):
			if v := strings.ToUpper(asStr(ev.Data["verdict"])); v == "REJECT" || v == "FLAG" {
				filtered = true
				filterN++
			}
		}
	})
	groundBefore := eng.Grounding().Len()
	eng.SubmitDefault(t.Goal)
	eng.Run(maxTicks)
	groundImported := eng.Grounding().Len() - groundBefore
	if groundImported > 0 {
		acted = true
		actN += groundImported // each imported observation is a degree of reality consultation
	}
	r.Answer = eng.LastResponse()

	// graph-shape signals (post-run): an explicit fork, and the reasoning DEPTH (deliberate = a sustained
	// multi-step chain on the active line, not a one-shot answer). The graded path reads the graph SIZE
	// (branch count, history depth) — the continuous version of the >1 / >=5 thresholds.
	deep := false
	branchesN, depthN := 0, 0
	if g := eng.Graph(); g != nil {
		branchesN = len(g.Branches)
		depthN = len(g.History())
		if branchesN > 1 {
			branched = true
			if extra := branchesN - 1; extra > branchN {
				branchN = extra // graph forks beyond the root the event stream may have missed
			}
		}
		if depthN >= 5 { // root + >=4 reasoning steps
			deep = true
		}
	}

	// honest-unknown: the conscious refused to confabulate (Filter caught it OR the answer hedges/declines
	// OR it honestly produced nothing) — the right move on an unknowable goal.
	low := strings.ToLower(r.Answer)
	honest := filtered || strings.TrimSpace(r.Answer) == ""
	honestMarkers := filterN
	for _, m := range honestUnknownMarkers {
		if strings.Contains(low, m) {
			honest = true
			honestMarkers++
		}
	}
	if strings.TrimSpace(r.Answer) == "" {
		honestMarkers++ // surfaced the gap (returned nothing) rather than confabulate
	}

	obs := map[string]bool{"branch": branched, "act": acted, "conflict": conflicted, "decompose": decomposed, "honest": honest, "deliberate": deep}
	for k, v := range obs {
		if v {
			r.Observed = append(r.Observed, k)
		}
	}
	r.Fired = obs[t.Signature]

	// GATE-2 graded aptness: a saturating 0..1 score per faculty (degree of engagement). The
	// normalizers (the "n" each count saturates at) are pre-registered per faculty — chosen so a
	// healthy-but-not-runaway engagement maps near the top of the scale and an absence maps to 0.
	// branch saturates at 3 forks, act at 4 reality imports, decompose at 4 structural steps,
	// conflict at 2 held conflicts, honest at 2 catches/markers; deliberate is graph depth scaled
	// against an 8-step deep-chain target. Saturating (min(1, count/n)) keeps it bounded so a noisy
	// outlier cannot dominate the σ characterization.
	r.AptnessParts = map[string]float64{
		"branch":     saturate(branchN, 3),
		"act":        saturate(actN, 4),
		"decompose":  saturate(decomposeN, 4),
		"conflict":   saturate(conflictN, 2),
		"honest":     saturate(honestMarkers, 2),
		"deliberate": saturate(depthN-1, 8), // depth-1 = reasoning steps past the root
	}
	r.Aptness = r.AptnessParts[t.Signature]

	// OUTCOME-TIE (v2): when the task carries an objective oracle, score the ANSWER against the ground
	// truth (the same realhard.Score the hard suite uses) and gate the faculty signal on it. The legacy
	// Fired stays untouched (byte-identical fire-only default); FiredAndCorrect is the new gated metric.
	if t.HasOracle() {
		v := scoreOutcome(t, r.Answer)
		r.OutcomeTied = true
		r.OutcomeSolved = v.Solved
		r.OutcomeReason = v.Reason
		r.AssertedLure = v.AssertedLure
		r.FiredAndCorrect = r.Fired && v.Solved
	} else {
		// No oracle to gate against — the gated signal degrades to the fire-only signal so a mixed suite
		// (legacy + v2 tasks) still produces a FiredAndCorrect count for every task.
		r.FiredAndCorrect = r.Fired
	}
	return r
}

// saturate maps a non-negative count to a bounded [0,1] engagement score: min(1, count/n). A
// count of 0 → 0 (faculty absent); a count >= n → 1 (saturated). The saturation keeps the graded
// score bounded so a runaway count cannot dominate the noise-floor characterization (GATE-2).
func saturate(count, n int) float64 {
	if count <= 0 || n <= 0 {
		return 0
	}
	v := float64(count) / float64(n)
	if v > 1 {
		return 1
	}
	return v
}

// asStr coerces an event Data value to a string (events store strings for these keys).
func asStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
