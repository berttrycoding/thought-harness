package decisionoracle

import (
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// workerSkill maps an A2 worker kind to the SEED skill the engine staffs for it (the
// gold decompositions in cognition.seedSkills): the Deliberator is the trade-off skill
// par(compare,contrast)→rank, the Verifier is the validate skill (a check move that the
// Gate's skeptic stance privileges). These are the SAME skill bodies the engine's
// Subconscious layer expands and runs — so Drive exercises the real worker, not a stub.
//
// NOTE (honest scope, flagged in the package report): the bank goals are decision/ship
// QUESTIONS whose surface text does NOT trip these skills' keyword TRIGGERS, so the
// engine's goal->skill ROUTER (SkillRegistry.Match) would not auto-staff them from the
// raw goal — that routing gap is a separate, real finding (a router-coverage problem,
// not a worker-capability problem). Drive deliberately staffs the worker DIRECTLY by the
// fixture's authored worker kind (which IS the routing decision, made by the bank), then
// measures whether the worker, once staffed, reaches the RIGHT verdict. That isolates the
// A2 question ("does the Deliberator/Verifier decide correctly?") from the orthogonal
// routing question.
var workerSkill = map[Worker]string{
	WorkerDeliberator: "evaluate-options", // seq(decompose, par(compare,contrast), rank) — the BRANCH+rank faculty
	WorkerVerifier:    "validate-result",  // seq(validate) — the check/ship faculty
}

// Result is one fixture's full A2 outcome: the worker that ran, the skill that staffed it,
// the program shape it executed, the raw OUTPUT text the real sub-agents produced, the
// extracted Verdict, and the oracle Score of that verdict against the fixture's ground
// truth. ID/Worker carry through for the per-worker report rollup.
type Result struct {
	// ID / Worker identify the fixture.
	ID     string
	Worker Worker
	// Goal is the request the worker was driven on.
	Goal string
	// Skill is the seed skill that staffed the worker (the gold decomposition).
	Skill string
	// Shape is the one-line control-flow signature of the expanded worker program.
	Shape string
	// Staffed is true when the worker skill expanded + verified + ran at least one
	// sub-agent. false = a wiring gap (no skill / empty program) — scored as a hard
	// fail (no verdict), never a silent pass.
	Staffed bool
	// Output is the raw concatenated text the worker's real sub-agents produced PLUS its
	// stated verdict response (the surface ExtractVerdict parses). Kept for the ledger's
	// RawOutput + audit.
	Output string
	// VerdictLinePresent is true when the worker OBEYED the verdict contract — it stated a
	// well-formed `VERDICT: <label>` line in its conclusion response (whether or not the label
	// is a valid option; a garbled label is still format-obedient). false = format-disobedience
	// (no line at all) -> the prose fallback fired. The present-RATE rolls up into WorkerStat
	// and a sub-threshold rate fails the bench LOUD (A2 fix #6) — so format-disobedience cannot
	// be silently reported-and-ignored while the prose fallback re-imports the whack-a-mole.
	VerdictLinePresent bool
	// Verdict is the machine-readable distillate ExtractVerdict pulled from Output.
	Verdict Verdict
	// VerdictSource records HOW the verdict was obtained so the report distinguishes a contract
	// pick from a fallback pick (A2 fix #7 — read the new instrument): "line" = the worker's
	// stated `VERDICT:` line mapped to a verdict; "fallback" = no usable line, the prose parser
	// produced the verdict; "none" = neither yielded a verdict (a hard non-decision).
	VerdictSource string
	// Score is the oracle's score of Verdict against the fixture's ground truth.
	Score Score
}

// Pass reports whether this fixture's worker reached a CORRECT verdict over the bar.
func (r Result) Pass() bool { return r.Score.Pass }

// Drive runs the REAL Deliberator/Verifier sub-agents on one fixture, the way the engine's
// Subconscious layer does, and scores the worker's actual verdict against the fixture's
// (vetted-sound, slice-1) decision/ship oracle. The pipeline, per fixture:
//
//   - resolve the worker's SEED skill (workerSkill[fx.Worker]) from a fresh seeded
//     SkillRegistry (the gold decompositions) and a fresh seeded OperatorRegistry (the
//     gold operator catalog) — the exact registries the engine staffs from;
//   - EXPAND the skill into a bounded, acyclic pure-operator Program and structurally
//     VERIFY it (cognition.VerifyProgram) — the same build-time gate the engine runs;
//   - build a Workflow FromProgram and run it phase-by-phase, instantiating one real
//     SubAgent per step (par groups fan out to several) and FIRING each — every fire is a
//     genuine backend.OperatorApply call (a model call on --backend claude; a deterministic
//     fragment on the test double), exactly as a live tick;
//   - CONCATENATE the sub-agents' candidate texts into the worker's output surface;
//   - EXTRACT a Verdict from that surface (no model — lexical distillation) and SCORE it
//     against the fixture's computed ground truth.
//
// Determinism: the SubAgents run on the seeded cpyrand passed in (the reason path is pure);
// the test double's OperatorApply is a closed-form fragment, so two runs are byte-identical.
// No executor / cognition view is wired (these workers are pure-reasoning faculties — the
// Deliberator weighs, the Verifier checks; neither needs a real tool to produce its verdict
// on these fixtures), so Fire always takes the reason path.
func Drive(fx Fixture, backend backends.Backend, rng *cpyrand.Random) Result {
	res := Result{ID: fx.ID, Worker: fx.Worker, Goal: fx.Goal}

	skillName, ok := workerSkill[fx.Worker]
	if !ok {
		// Unknown worker kind — a bank error. No verdict, hard fail (scored below).
		res.Score = Score{Axes: map[string]float64{}, Reason: "unknown worker " + string(fx.Worker) + " (bank error)"}
		return res
	}
	res.Skill = skillName

	lib := cognition.NewSkillRegistry(true)
	cat := cognition.NewOperatorRegistry()

	skill, found := lib.Get(skillName)
	if !found {
		res.Score = Score{Axes: map[string]float64{}, Reason: "worker skill " + skillName + " not in seed library (build error)"}
		return res
	}
	prog, err := lib.Expand(skill)
	if err != nil {
		res.Score = Score{Axes: map[string]float64{}, Reason: "worker skill " + skillName + " failed to expand: " + err.Error()}
		return res
	}
	if okVerify, reasons := cognition.VerifyProgram(prog, cat); !okVerify {
		res.Score = Score{Axes: map[string]float64{}, Reason: "worker program failed verify: " + strings.Join(reasons, "; ")}
		return res
	}
	res.Shape = prog.Shape()

	output, verdictLine := runWorker(&prog, cat, backend, fx, rng)
	res.Output = output
	res.Staffed = strings.TrimSpace(output) != ""
	res.VerdictLinePresent = verdictLine

	res.Verdict = ExtractVerdict(output, fx)
	res.VerdictSource = classifyVerdictSource(output, fx, res.Verdict)
	res.Score = ScoreVerdict(res.Verdict, fx)
	return res
}

// classifyVerdictSource reports HOW the extracted verdict was obtained — "line" when the worker's
// stated `VERDICT:` line yielded it, "fallback" when the prose parser did, "none" when no verdict
// was extracted. It re-derives the contract path with the SAME parseVerdictLine ExtractVerdict
// uses (so the classification can never disagree with the verdict actually scored), then checks
// whether a verdict was extracted at all. This drives the report's line-vs-fallback distinction
// (fix #7) without changing what is scored.
func classifyVerdictSource(output string, fx Fixture, v Verdict) string {
	if _, ok := parseVerdictLine(output, fx); ok {
		return "line"
	}
	// No usable contract line — was the prose fallback's verdict non-empty?
	switch fx.Worker {
	case WorkerVerifier:
		if v.Decision != "" {
			return "fallback"
		}
	default:
		if v.PickedOption != "" || v.Undecided {
			return "fallback"
		}
	}
	return "none"
}

// verdictLabels returns the LABELS the EmitVerdict contract offers the worker — the choosable
// vocabulary ONLY, never the ground-truth winner or the criteria weights (the anti-leak guarantee,
// A2 mandatory-fix #1):
//
//   - deliberator: each option's ID and (when distinct) its human Name, so the worker can state its
//     pick by either. The order is the fixture's option order — it does NOT encode the winner.
//   - verifier: the fixed accept | refuse | cannot-verify set (carries no claim ground truth).
//
// The criteria weights, the per-criterion scores, and BetterOption's computed winner are NEVER
// passed in, so the contract call cannot leak the answer the oracle scores against.
func verdictLabels(fx Fixture) []string {
	if fx.Worker == WorkerVerifier {
		return []string{"accept", "refuse", "cannot-verify"}
	}
	labels := make([]string, 0, len(fx.Options)*2)
	for _, o := range fx.Options {
		if o.ID != "" {
			labels = append(labels, o.ID)
		}
		if o.Name != "" && !strings.EqualFold(o.Name, o.ID) {
			labels = append(labels, o.Name)
		}
	}
	return labels
}

// runWorker runs the worker's expanded program through the Subconscious workflow runner,
// firing one real SubAgent per step and collecting their candidate texts in execution
// order, THEN asks the worker to STATE its verdict in the fixed contract shape (A2 fix #1/#2).
// This is the SAME FromProgram → Instantiate → Fire machinery the engine's tick loop uses
// (subconscious/workflow.go), minus the seam/Critic re-voicing — the worker's raw reasoning
// surface plus its stated verdict line is exactly what the decision/ship oracle should score
// (the oracle is the measuring stick for the worker's OUTPUT, before the seam launders it).
//
// The verdict call (backend.EmitVerdict) is DISTINCT from the program's per-step OperatorApply
// fires: the Deliberator skill ends in `rank` (a ranking, not a stated pick), so without a
// conclusion step the worker never states a verdict. EmitVerdict feeds the accumulated reasoning
// (the decompose/compare/contrast/rank outputs) back as priorReasoning so the worker CONCLUDES the
// deliberation it already did — it is not re-deciding from the bare goal (that would measure
// "one-shot the goal," not "does the deliberation conclude correctly," which is what A2 measures).
//
// Returns the combined surface (the per-step reasoning + the verdict response) AND whether a
// well-formed `VERDICT:` line was present in the verdict response (the contract-obedience signal
// the present-rate guard rolls up, A2 fix #6).
func runWorker(prog *cognition.Program, cat *cognition.OperatorRegistry,
	backend backends.Backend, fx Fixture, rng *cpyrand.Random) (output string, verdictLine bool) {
	goal := fx.Goal
	wf := subconscious.FromProgram(prog, cat, backend, nil, goal)
	// Seed the context with the goal as the active line (what a sub-agent's ContextSlice
	// anchors on), exactly as the conscious stream would carry the question.
	ctx := []types.Thought{{Text: goal, Source: types.GENERATED, Confidence: 0.5}}

	var parts []string
	for !wf.Exhausted() {
		phase := wf.Current()
		for _, agent := range wf.Instantiate(phase, nil, nil) {
			if c := agent.Fire(ctx, rng); c != nil {
				t := strings.TrimSpace(c.Text)
				if t != "" {
					parts = append(parts, t)
				}
			}
		}
		wf.Advance()
	}
	reasoning := strings.Join(parts, "\n")

	// CONCLUSION step: ask the worker to STATE its verdict in the fixed contract shape, feeding
	// back its own accumulated reasoning (fix #2) and the choosable labels only (fix #1 anti-leak).
	verdictResp := backend.EmitVerdict(string(fx.Worker), goal, verdictLabels(fx), reasoning)
	verdictLine = hasVerdictLine(verdictResp)

	// The combined surface ExtractVerdict reads: the worker's reasoning (for the soundness axis +
	// the prose fallback when the contract is disobeyed) followed by the verdict response (the
	// contract line). Joined so the LAST `VERDICT:` line is the stated verdict.
	switch {
	case reasoning == "":
		output = verdictResp
	case strings.TrimSpace(verdictResp) == "":
		output = reasoning
	default:
		output = reasoning + "\n" + verdictResp
	}
	return output, verdictLine
}

// hasVerdictLine reports whether a worker's verdict response carries a well-formed `VERDICT:`
// line (a non-empty label at a line head) — the contract-obedience signal. It is the same
// line-detection lastVerdictLabel uses, exposed as a bool for the present-rate guard, and does
// NOT require the label to be a VALID option (a garbled `VERDICT: xyz` is still "present" — the
// worker obeyed the format; the parser then falls back). This separates format-disobedience
// (no line at all) from a wrong/garbled label.
func hasVerdictLine(verdictResp string) bool {
	return lastVerdictLabel(verdictResp) != ""
}

// DriveAll runs Drive over every fixture in input order (deterministic) on a fresh seeded
// RNG per fixture (seeded by seedBase + the fixture index), so adding/removing a fixture
// does not perturb another's seed. The backend is shared (a model bridge is reused across
// fixtures; the test double is stateless).
func DriveAll(fixtures []Fixture, backend backends.Backend, seedBase uint64) []Result {
	out := make([]Result, 0, len(fixtures))
	for i, fx := range fixtures {
		out = append(out, Drive(fx, backend, cpyrand.New(seedBase+uint64(i))))
	}
	return out
}

// WorkerStat is the per-worker-kind rollup the report headlines: how many of this worker's
// fixtures the real sub-agents got CORRECT (passed the oracle), the total, and the mean
// oracle score. PassRate is Passed/Total (0 when Total==0).
type WorkerStat struct {
	Worker       Worker
	Total        int
	Passed       int
	Decided      int     // fixtures where a verdict was extracted at all (vs a hard non-decision)
	VerdictLines int     // fixtures where the worker OBEYED the contract (stated a VERDICT line)
	SumScore     float64 // sum of oracle scores (for the mean)
}

// VerdictLineRate is the fraction of this worker's fixtures whose response carried a well-formed
// `VERDICT:` line (the contract-obedience rate). A low rate means the worker is disobeying the
// format and the run is silently leaning on the prose fallback — the present-rate guard (#6) fails
// the bench LOUD below a threshold so that is never silently ignored. 0 when the worker has no
// fixtures.
func (w WorkerStat) VerdictLineRate() float64 {
	if w.Total == 0 {
		return 0
	}
	return float64(w.VerdictLines) / float64(w.Total)
}

// PassRate is the headline A2 baseline number for this worker: the fraction of its fixtures
// whose verdict cleared the oracle bar. 0 when the worker has no fixtures.
func (w WorkerStat) PassRate() float64 {
	if w.Total == 0 {
		return 0
	}
	return float64(w.Passed) / float64(w.Total)
}

// MeanScore is the mean oracle score over this worker's fixtures (0 when none).
func (w WorkerStat) MeanScore() float64 {
	if w.Total == 0 {
		return 0
	}
	return w.SumScore / float64(w.Total)
}

// Rollup aggregates Drive results into a per-worker-kind pass-rate, returned in a stable
// worker order (deliberator before verifier, then any others alphabetically) so the report
// is deterministic.
func Rollup(results []Result) []WorkerStat {
	byWorker := map[Worker]*WorkerStat{}
	for _, r := range results {
		ws := byWorker[r.Worker]
		if ws == nil {
			ws = &WorkerStat{Worker: r.Worker}
			byWorker[r.Worker] = ws
		}
		ws.Total++
		ws.SumScore += r.Score.Score
		if r.Score.Decided {
			ws.Decided++
		}
		if r.VerdictLinePresent {
			ws.VerdictLines++
		}
		if r.Score.Pass {
			ws.Passed++
		}
	}
	out := make([]WorkerStat, 0, len(byWorker))
	for _, ws := range byWorker {
		out = append(out, *ws)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return workerOrder(out[i].Worker) < workerOrder(out[j].Worker)
	})
	return out
}

// workerOrder gives a stable sort key: deliberator first, verifier second, then any
// unknown worker kind alphabetically after.
func workerOrder(w Worker) string {
	switch w {
	case WorkerDeliberator:
		return "0:" + string(w)
	case WorkerVerifier:
		return "1:" + string(w)
	default:
		return "9:" + string(w)
	}
}
