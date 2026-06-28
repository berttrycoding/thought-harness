// Package critic holds the Controller — the Critic, executive half (Tier 3, PORT-PLAN #24,
// the HARDEST core module).
//
// It reasons over the *whole graph* (not a single return): branch-exhaustion,
// loop-exhaustion, decide-next, stopping, quiescence, interrupt handling. The decision spine
// of §5.3 / §9.3:
//
//	branch not exhausted          -> THINK
//	branch exhausted, loop not    -> BACKTRACK (internal exit)
//	branch & loop exhausted       -> ACT (external exit — import ground truth)
//	goal satisfied / over budget  -> STOP
//
// It also runs the validate-before-trust guard: a FLAG'd (low-confidence) thought triggers
// BRANCH (verify) or, when the question demands ground truth, ACT.
//
// Ported from the (now-removed) Python thought_harness/critic/controller.py. The Critic is split: this is
// the executive half (over the whole graph); the admission half is the Filter (seams/hidden).
//
// DETERMINISTIC-AGENTIC PIPELINE (P5.1, controller-redesign.md). The Controller is a deterministic
// SPINE with agentic MUSCLE at exactly the nodes that are provably not a closed-form function:
//  1. state extraction / context  — HEURISTIC (reads the graph; the decider sees the actual working
//     context, P4.1, not just statistics) ·
//  2. STRUCTURAL moves (THINK/BRANCH/BACKTRACK/ACT/MERGE/STOP) — HEURISTIC: the `choose` decision
//     ladder, a deterministic closed-form over well-understood signals (and the brittle stance-conflict
//     input was removed at the seam, P0.3 — conflict is now content-neutral survivor count) ·
//  3. THRESHOLD judgments (is-the-goal-met / is-this-line-spent) — AGENTIC: escalated to the model on
//     the hybrid path (DecideViaModel), the genuinely-uncalculable calls — BUT the model can NEVER
//     override a structural move (TestHybridProtectsStructuralDecisions).
//
// The deterministic harness keeps the system analyzable + stable (the design proof: heuristic 4/4 vs
// blind-llm 2/4 on the compare scenarios — full-LLM decides WORSE).
package critic

import (
	"math"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// CriticConfig holds the Controller's thresholds. Mirrors Python CriticConfig (the field
// defaults are the dataclass defaults; NewController applies them when nil is passed).
type CriticConfig struct {
	DoneConfidence float64 // an admitted, confident answer ends the episode (Python 0.7)
	FlagThreshold  float64 // matches the Filter's ADMIT cutoff: an admitted injection below this is
	//                        in the Filter's FLAG band (low-confidence -> verify/act) (Python 0.6)
	ExhaustConf      float64 // recent-confidence floor that signals a spinning/stuck branch (Python 0.5)
	ExhaustAfter     int     // active-branch length before exhaustion is possible (Python 4)
	PursuitThreshold float64 // stashed-branch value worth backtracking to / pursuing (Python 0.4)
	MaxSteps         int     // give-up cap on a single branch (Python 16)
	SimilarRepeat    float64 // repetition ratio that signals a spinning branch (Python 0.72)
}

// DefaultCriticConfig returns the Python CriticConfig dataclass defaults.
func DefaultCriticConfig() CriticConfig {
	return CriticConfig{
		DoneConfidence:   0.7,
		FlagThreshold:    0.6,
		ExhaustConf:      0.5,
		ExhaustAfter:     4,
		PursuitThreshold: 0.4,
		MaxSteps:         16,
		SimilarRepeat:    0.72,
	}
}

// groundTruth is the set of phrases that genuinely demand a reality check. Phrase-level on
// purpose — bare words like "runtime"/"output"/"actually" match incidental text in
// observations and musings, which re-opens a question reality already closed (a self-sustaining
// act-loop). Matched as a SUBSTRING (Python `k in text`), order preserved. Mirrors Python
// _GROUND_TRUTH.
var groundTruth = []string{
	"will this run", "will it run", "does this run", "does it run", "will it work",
	"is it correct", "is this correct", "verify", "will this code run", "does the code run",
	"run correctly",
	// A verification / safety / regression question a REAL test run can answer — these route the
	// Controller to ACT (consult reality) rather than answer a ship/merge decision from priors.
	"safe to ship", "safe to merge", "ready to ship", "ready to merge", "ship it", "ship this",
	"regress", "break anything", "breaks anything", "tests pass", "pass the test", "does it pass",
	"did it work", "is it fixed", "is this fixed", "confirm the fix", "run the test",
}

// DecideOptions carries the optional keyword arguments to DecideNext. The zero value is the
// Python default for every field EXCEPT BudgetStop and ActOnExhaustion, which default to
// true in Python — so callers MUST use DefaultDecideOptions() (or set those two) rather than
// a bare zero value. (DESIGN §6 cross-cutting: an 8-kwarg signature becomes an options struct.)
type DecideOptions struct {
	Conflict        bool // a conflict was detected this step (positional in Python)
	ActedBranch     bool // this branch has already imported ground truth (positional in Python)
	VerifiedBranch  bool // a verify-branch has already been spawned (Python default False)
	AlreadyForked   bool // a fork already happened this step (Python default False)
	WorkflowPending bool // a synthesised workflow still has phases to advance (Python default False)
	BudgetStop      bool // honour the step budget (Python default True)
	ActOnExhaustion bool // exhaustion may open the channel to reality (Python default True)

	// AwakeUserBudget is the awake-mode deliver deadline for a line that holds an UNANSWERED user
	// turn: after this many real steps without the goal being satisfied, the mind STOPs working it
	// and ANSWERS the user (give-up -> DELIVER) rather than wandering off and setting the turn aside.
	// 0 ⇒ disabled (reactive uses the episodic BudgetStop/MaxSteps give-up instead). Only the engine's
	// continuous loop sets it; choose() guards ONLY a user-holding line, never the endless wander.
	AwakeUserBudget int
}

// DefaultDecideOptions returns DecideOptions with the Python keyword defaults: BudgetStop and
// ActOnExhaustion true, everything else false. Construct from this and override the fields the
// call site sets, so the two true-by-default kwargs are never silently dropped.
func DefaultDecideOptions() DecideOptions {
	return DecideOptions{BudgetStop: true, ActOnExhaustion: true}
}

// ControllerMeta is the structured record of the last decision (Python self.last_meta dict).
// Kept as a struct so the panels / engine read fields by name. The Escalated/LLMDecision/
// Agree/LLMWhy fields mirror the `last_meta.update(...)` the smart-hybrid path performs; they
// are zero-valued (Escalated=false, LLMSet=false) on the heuristic path.
type ControllerMeta struct {
	Decision          string // the FINAL decision name (may be the escalated llm pick)
	HeuristicDecision string // the heuristic's own pick (never overwritten)
	StopKind          *string
	Reason            string
	BranchExhausted   bool
	LoopExhausted     bool
	Flagged           bool
	NeedsGroundTruth  bool
	Ambiguity         float64 // round(ambiguity, 2) at the emit site
	Escalated         bool

	// smart-hybrid escalation extras (set only when an escalation actually occurred)
	LLMSet      bool   // whether the LLM fields below were populated (Python: keys present)
	LLMDecision string // the model's pick name
	Agree       bool   // llm_decision is heuristic decision
	LLMWhy      string // the model's captured reasoning (P6)
}

// controlMode is the deterministic-floor cognition mode (the resolved-mode default). Named here so
// the Rule-4 floor_stands gating ("emit only in a non-control mode") and the resolved-mode default
// share a single source of truth.
const controlMode = "control"

// Controller is the Critic's executive half. Mirrors Python Controller.
type Controller struct {
	emit     events.Emit
	cfg      CriticConfig
	LastMeta ControllerMeta

	// mode: "control" (deterministic floor — fast path), "llm" (model decides every step), "hybrid"
	// (model only on ambiguous decisions). llm/hybrid require a backend that satisfies the Decider
	// interface.
	mode    string
	backend backends.Backend

	// backtrack gates the BACKTRACK structural move (CONFIG: conscious.allow_backtrack). Nil-safe:
	// a nil gate reports Enabled()==true, so the default (Features=nil / gate unset) keeps BACKTRACK
	// available — byte-identical to before. When the toggle is OFF the BACKTRACK exit of `choose`
	// is forbidden and the ladder falls through to its alternative (ACT/STOP/THINK), degrading the
	// graph to a single line. It is the multi-step-retrace ablation (measuring-stick-spec §5.8).
	backtrack *config.Gate

	// activity is the LIVE conscious.activity threshold config (slice (a)). When wired (non-nil and
	// non-degenerate) the Controller reads its decision thresholds from it instead of the static cfg,
	// so a TUI live-flip is observed with no rebuild — the same shared-pointer pattern as backtrack.
	// nil / zero ⇒ fall back to cfg, so the pre-config default is byte-identical.
	activity *config.ConsciousActivityCfg

	// activeResource gates V(s)-triggered active re-sourcing (A-RAG4: controller.active_resource). This is
	// an OPT-IN feature whose baseline is OFF, so the nil-as-enabled convention of *config.Gate is INVERTED
	// here on purpose: resourceTriggerOn() treats a nil/disabled gate as OFF (a Gate nil-receiver reports
	// Enabled()==true, which is the wrong default for an opt-in toggle). The engine always wires the gate
	// from the controller.active_resource knob (default OFF), so the trigger never fires unless the knob is
	// flipped ⇒ byte-identical. Shared pointer, so a TUI live-flip is observed with no rebuild — the same
	// discipline as backtrack/activity.
	activeResource *config.Gate

	// rng is the seeded RNG the soft policy samples with (slice d). nil ⇒ no soft sampling.
	rng *cpyrand.Random
	// traj records the current episode's soft-policy decisions for the REINFORCE update (Phase 5, §5.2);
	// retBaseline is the running-mean return baseline (variance reduction).
	traj        []softStep
	retBaseline float64
	retCount    int

	// θ-bandit (slice h, §5.3 OUTER loop): keep-or-revert over the activity θ (β, τ) across a window of
	// episodes. thetaExp holds J_best; thetaBeta/thetaTau is the θ snapshot taken at window open; winReturns
	// accumulates the window's returns; winLen is episodes per window. Used only when the caller drives
	// ExperimentWindow (conscious.activity.experiment).
	thetaExp            *convert.Experiment
	thetaOpen           bool
	thetaBeta, thetaTau float64
	winReturns          []float64
	winLen              int

	Escalations int // how many decisions were escalated to the model
	Decisions   int // how many decisions were made
}

// SetBacktrackGate installs the conscious.allow_backtrack gate (the retrace-off ablation). nil ⇒
// always-enabled (BACKTRACK available), so the pre-config default is unchanged. The gate reads the
// SHARED config pointer each call, so a live flip is observed with no Controller rebuild.
func (c *Controller) SetBacktrackGate(g *config.Gate) { c.backtrack = g }

// backtrackAllowed reports whether the Controller may issue BACKTRACK. Nil-safe (nil gate ⇒ true).
// When forbidden it emits config.skip once per reason so the bypass is observable, never silent.
func (c *Controller) backtrackAllowed() bool {
	if c.backtrack.Disabled() {
		c.backtrack.Skip("BACKTRACK forbidden -> single-line graph")
		return false
	}
	return true
}

// SetActivityConfig wires the LIVE conscious.activity thresholds (slice (a)). nil ⇒ the static cfg is
// used (pre-config default). The pointer is read each decision, so a live flip is observed with no
// rebuild — the same shared-pointer discipline as SetBacktrackGate.
func (c *Controller) SetActivityConfig(a *config.ConsciousActivityCfg) { c.activity = a }

// SetActiveResourceGate wires the controller.active_resource gate (A-RAG4 V(s)-triggered re-sourcing).
// The gate reads the SHARED config pointer each call, so a live flip is observed with no Controller
// rebuild. nil / disabled ⇒ the trigger is OFF (opt-in baseline), byte-identical.
func (c *Controller) SetActiveResourceGate(g *config.Gate) { c.activeResource = g }

// eff is the EFFECTIVE decision config: the live activity overlay when wired + non-degenerate, else the
// static cfg. The MaxSteps==0 guard makes a zero-value config (a non-AllOn HarnessConfig) fall back to
// the defaults rather than zero out every threshold.
func (c *Controller) eff() CriticConfig {
	if c.activity == nil || c.activity.MaxSteps == 0 {
		return c.cfg
	}
	return CriticConfig{
		DoneConfidence:   c.activity.DoneConfidence,
		FlagThreshold:    c.activity.FlagThreshold,
		ExhaustConf:      c.activity.ExhaustConf,
		ExhaustAfter:     c.activity.ExhaustAfter,
		PursuitThreshold: c.activity.PursuitThreshold,
		MaxSteps:         c.activity.MaxSteps,
		SimilarRepeat:    c.activity.SimilarRepeat,
	}
}

// mergeThreshold is the fuzzy near-duplicate Jaccard cutoff for MERGE — live from activity, else the
// historical literal 0.6.
func (c *Controller) mergeThreshold() float64 {
	if c.activity != nil && c.activity.MaxSteps != 0 {
		return c.activity.MergeThreshold
	}
	return 0.6
}

// SetRNG wires the seeded RNG the soft policy samples with (slice d). nil disables soft sampling.
func (c *Controller) SetRNG(r *cpyrand.Random) { c.rng = r }

// softEnabled reports whether the Boltzmann soft policy is active (conscious.activity.soft + an RNG).
func (c *Controller) softEnabled() bool {
	return c.activity != nil && c.activity.Soft && c.rng != nil
}

// softChoose samples a discretionary move from a Boltzmann (softmax) policy over per-move pressures at
// temperature τ (02-conscious.md §4.2). Called ONLY when the hard ladder fell through to THINK and soft
// is on — the hard rails already fired, so this just makes the previously flat THINK zone probabilistically
// active. Only VIABLE moves are candidates (MERGE needs a target; BACKTRACK needs a viable sibling + the
// gate). Deterministic under the seeded RNG.
func (c *Controller) softChoose(g *graph.ThoughtGraph, real []types.Thought, be bool) types.Decision {
	tau := c.activity.Temperature
	if tau < 0.01 {
		tau = 0.01
	}
	bBranch := c.activity.BranchPropensity
	if bBranch <= 0 {
		bBranch = 1.0
	}
	moves := []types.Decision{types.THINK, types.BRANCH}
	q := []float64{meanRecentConf(real), bBranch * 0.4} // zThink = line going well; zBranch = baseline explore
	if c.MergeTarget(g) != nil {
		moves = append(moves, types.MERGE)
		q = append(q, 0.5)
	}
	if be && c.anyViableSibling(g) && c.backtrackAllowed() {
		moves = append(moves, types.BACKTRACK)
		q = append(q, 0.5)
	}
	probs := softmax(q, tau)
	r := c.rng.Float64()
	chosen, acc := len(probs)-1, 0.0
	for i, p := range probs {
		if acc += p; r < acc {
			chosen = i
			break
		}
	}
	if c.activity.Learn {
		// record the step for REINFORCE (§5.2). BRANCH is index 1; z_branch=0.4 is the pre-β pressure
		// (the β gradient). qChosen / qExpected (the post-β pressures of the chosen move and the policy
		// mean) drive the temperature gradient (§5.2): qExpected = Σ π(i)·q(i).
		qExp := 0.0
		for i := range probs {
			qExp += probs[i] * q[i]
		}
		c.traj = append(c.traj, softStep{
			tau: tau, zBranch: 0.4, piBranch: probs[1], choseBranch: chosen == 1,
			qChosen: q[chosen], qExpected: qExp,
		})
	}
	return moves[chosen]
}

// softmax returns the Boltzmann distribution of q at temperature tau (max-shifted for numeric stability).
func softmax(q []float64, tau float64) []float64 {
	mx := q[0]
	for _, v := range q {
		if v > mx {
			mx = v
		}
	}
	out := make([]float64, len(q))
	sum := 0.0
	for i, v := range q {
		out[i] = math.Exp((v - mx) / tau)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// meanRecentConf is the mean confidence of the last (≤3) real thoughts — the THINK pressure (a line
// going well keeps thinking). 0.5 for an empty branch.
func meanRecentConf(real []types.Thought) float64 {
	if len(real) == 0 {
		return 0.5
	}
	n := len(real)
	if n > 3 {
		n = 3
	}
	sum := 0.0
	for _, t := range real[len(real)-n:] {
		sum += t.Confidence
	}
	return sum / float64(n)
}

// softStep records one soft-policy decision for the REINFORCE update (Phase 5, §5.2): the
// temperature τ, the BRANCH pressure z_branch, the policy's BRANCH probability, whether BRANCH was
// chosen (the β gradient), and the post-β pressures of the chosen move + the policy mean (qChosen,
// qExpected — the τ gradient).
type softStep struct {
	tau, zBranch, piBranch float64
	choseBranch            bool
	qChosen, qExpected     float64
}

// LearnFromReturn applies a REINFORCE update to BOTH soft-policy parameters from the episode return g
// (Phase 5, §5.2), baselined by the running-mean return (variance reduction):
//   - β_branch (the BRANCH pressure): closed-form softmax log-prob gradient
//     `(1/τ)·z_branch·(1[a=BRANCH] − π(BRANCH))`, clamped to [0.05, 3.0]. Branching that led to a good
//     return drifts β_branch up.
//   - τ (the temperature / explore-exploit knob): the temperature gradient
//     `∂logπ(a)/∂τ = (E_π[q] − q_chosen)/τ²`, clamped to [0.05, 1.0]. A good return from a HIGH-value
//     move sharpens the policy (τ down → exploit); a good return from a LOW-value exploratory move
//     softens it (τ up → explore).
//
// Both updates write back to the SHARED activity config (so they persist + show in the TUI) and clear the
// trajectory. A no-op without a trajectory or an activity config.
func (c *Controller) LearnFromReturn(g float64) {
	if c.activity == nil || len(c.traj) == 0 {
		c.traj = c.traj[:0]
		return
	}
	adv := g - c.retBaseline // advantage uses the OLD baseline...
	c.retCount++
	c.retBaseline += (g - c.retBaseline) / float64(c.retCount) // ...then update it
	betaGrad, tauGrad := 0.0, 0.0
	for _, s := range c.traj {
		ind := 0.0
		if s.choseBranch {
			ind = 1.0
		}
		betaGrad += (1.0 / s.tau) * s.zBranch * (ind - s.piBranch)
		tauGrad += (s.qExpected - s.qChosen) / (s.tau * s.tau)
	}
	alpha := c.activity.LearnRate
	if alpha <= 0 {
		alpha = 0.05
	}
	b := clampRange(c.activity.BranchPropensity+alpha*adv*betaGrad, 0.05, 3.0)
	c.activity.BranchPropensity = b
	tau := clampRange(c.activity.Temperature+alpha*adv*tauGrad, 0.05, 1.0)
	c.activity.Temperature = tau
	c.traj = c.traj[:0]
}

// ExperimentWindow runs the §5.3 OUTER keep-or-revert loop over the activity θ (β, τ). It snapshots θ at a
// window's open, accumulates each episode's goal-relative return, and at window close (winLen episodes)
// scores J = the mean window return and proposes it to a keep-or-revert Experiment: KEEP the
// inner-loop-adjusted θ iff J STRICTLY beats the best window so far (the lineage's strict `>`), else REVERT
// θ to the snapshot — the window's drift did not pay off. Returns (windowClosed, decision). A no-op
// (false, Revert) without an activity config. Deterministic; gated by the caller (conscious.activity
// .experiment). The inner REINFORCE loop (LearnFromReturn) is the proposer; this is the accept/reject.
func (c *Controller) ExperimentWindow(ret float64) (windowClosed bool, decision convert.Decision) {
	if c.activity == nil {
		return false, convert.Revert
	}
	if !c.thetaOpen { // open a window: snapshot θ + (re)arm the experiment
		c.thetaBeta = c.activity.BranchPropensity
		c.thetaTau = c.activity.Temperature
		c.winReturns = c.winReturns[:0]
		c.thetaOpen = true
		if c.winLen <= 0 {
			c.winLen = 3
		}
		if c.thetaExp == nil {
			c.thetaExp = convert.NewExperiment(0) // J_best floor 0 — any positive window adopts
		}
	}
	c.winReturns = append(c.winReturns, ret)
	if len(c.winReturns) < c.winLen {
		return false, convert.Revert
	}
	sum := 0.0
	for _, r := range c.winReturns {
		sum += r
	}
	J := sum / float64(len(c.winReturns))
	d := c.thetaExp.Propose(J)
	if d == convert.Revert {
		c.activity.BranchPropensity = c.thetaBeta // restore θ — the window's drift did not strictly improve
		c.activity.Temperature = c.thetaTau
	}
	c.thetaOpen = false
	return true, d
}

// clampRange clamps x to [lo, hi].
func clampRange(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// NewController builds the Controller over the injected emit closure. cfg may be nil (Python
// `config or CriticConfig()`); mode is "control" | "llm" | "hybrid"; backend may be nil. As
// in Python, mode falls back to "control" (the deterministic floor) unless the backend actually
// satisfies Decider (the `hasattr(backend, "decide")` guard).
func NewController(emit events.Emit, cfg *CriticConfig, mode string, backend backends.Backend) *Controller {
	c := CriticConfig{}
	if cfg != nil {
		c = *cfg
	} else {
		c = DefaultCriticConfig()
	}
	resolved := controlMode
	if backend != nil {
		if _, ok := backend.(backends.Decider); ok {
			resolved = mode
		}
	}
	return &Controller{
		emit:    emit,
		cfg:     c,
		mode:    resolved,
		backend: backend,
	}
}

// -- helpers -------------------------------------------------------------

// realThoughts filters out METACOG markers (Python Controller._real) — structural bookkeeping
// like "[branch] ... verify alternative" is not a real thought.
func realThoughts(ctx []types.Thought) []types.Thought {
	out := make([]types.Thought, 0, len(ctx))
	for _, t := range ctx {
		if t.Source != types.METACOG {
			out = append(out, t)
		}
	}
	return out
}

// similar is the word-overlap repetition ratio (Python Controller._similar). Identical to the
// shared Jaccard helper (lower-case, split on whitespace into word sets, |a∩b|/|a∪b|), so it
// delegates rather than re-implement.
func (c *Controller) similar(a, b string) float64 { return types.Jaccard(a, b) }

// Reconfigure swaps the decision backend + mode on a live Controller — the faithful port of
// Python's `controller.backend = backend; controller.mode = mode` (the compare diagnostic holds
// the whole engine on the control floor and varies ONLY the Controller's decision organ). As in
// NewController, the mode falls back to "control" unless the backend actually satisfies Decider (the
// `hasattr(backend, "decide")` guard), so an LLM backend is required to enable llm/hybrid.
func (c *Controller) Reconfigure(backend backends.Backend, mode string) {
	c.backend = backend
	resolved := controlMode
	if backend != nil {
		if _, ok := backend.(backends.Decider); ok {
			resolved = mode
		}
	}
	c.mode = resolved
}

// Mode reports the Controller's resolved decision mode (control | llm | hybrid). Python reads
// controller.mode directly; mode is unexported here, so callers (the compare diagnostic) reach it
// through this accessor.
func (c *Controller) Mode() string { return c.mode }

// PursuitThreshold exposes the configured stashed-branch pursuit cutoff. Python reads
// controller.cfg.pursuit_threshold directly; cfg is unexported here, so the engine reaches it via
// this accessor (the Drives constructor + the continuous-loop resume gate).
func (c *Controller) PursuitThreshold() float64 { return c.eff().PursuitThreshold }

// needsGroundTruth reports whether the active line is asking a question that genuinely demands
// a reality check. Mirrors Python Controller._needs_ground_truth.
func (c *Controller) needsGroundTruth(g *graph.ThoughtGraph) bool {
	// Scan real content only — METACOG markers like "[branch] ... verify alternative" are
	// structural bookkeeping, not a question demanding reality (that leaked 'verify' -> acting).
	real := realThoughts(g.ActiveContext())
	// Reality already answered on this line -> the question is closed; integrate the result,
	// don't re-ask. (Without this, an observation's own wording re-triggers acting -> a loop.)
	for _, t := range last(real, 3) {
		if t.Source == types.OBSERVATION {
			return false
		}
	}
	text := g.Goal
	for _, t := range last(real, 3) {
		text += " " + t.Text
	}
	textLow := strings.ToLower(text)
	for _, k := range groundTruth {
		if strings.Contains(textLow, k) {
			return true
		}
	}
	return false
}

// -- exhaustion ----------------------------------------------------------

// BranchExhausted reports whether the active branch is spinning: too long, recently
// low-confidence, or repeating itself. Mirrors Python Controller.branch_exhausted.
func (c *Controller) BranchExhausted(g *graph.ThoughtGraph) bool {
	real := realThoughts(g.ActiveContext())
	if len(real) < c.eff().ExhaustAfter {
		return false
	}
	recent := last(real, 3)
	if maxConfidence(recent) < c.eff().ExhaustConf {
		return true
	}
	n := len(real)
	if n >= 2 && c.similar(real[n-1].Text, real[n-2].Text) >= c.eff().SimilarRepeat {
		return true
	}
	return false
}

// LoopExhausted reports that all internal options are spent: the branch is exhausted and no
// viable stashed sibling exists -> ACT. Mirrors Python Controller.loop_exhausted.
func (c *Controller) LoopExhausted(g *graph.ThoughtGraph) bool {
	if !c.BranchExhausted(g) {
		return false
	}
	return !c.anyViableSibling(g)
}

// GoalSatisfied reports whether the episode goal is met. Mirrors Python
// Controller.goal_satisfied.
func (c *Controller) GoalSatisfied(g *graph.ThoughtGraph) bool {
	real := realThoughts(g.ActiveContext())
	if len(real) < 2 {
		return false
	}
	lastT := real[len(real)-1]
	if lastT.Source == types.OBSERVATION {
		if o, ok := lastT.RawReturn.(types.Observation); ok && o.Ok {
			return true // reality confirmed success
		}
	}
	// A question that DEMANDS ground truth (ship/verify/regression) is not "answered" by a
	// confident internal thought — it must be confirmed by a real observation first. Without
	// this, the harness would close a ship-safety question from priors and never run the tests.
	if c.needsGroundTruth(g) && !anyObservation(last(real, 4)) {
		return false
	}
	if lastT.Confidence >= c.eff().DoneConfidence &&
		(lastT.Source == types.INJECTED || lastT.Source == types.GENERATED) {
		// an answer, not a restated question
		return !endsWithQ(lastT.Text)
	}
	return false
}

// MergeTarget returns the id of a frontier branch the active line should merge into (exact
// duplicate state, or a fuzzy near-duplicate of the last thought), or nil for none. Mirrors
// Python Controller.merge_target (returns int | None).
func (c *Controller) MergeTarget(g *graph.ThoughtGraph) *int {
	real := realThoughts(g.ActiveContext())
	if len(real) == 0 {
		return nil
	}
	lastText := real[len(real)-1].Text
	activeKey := g.StateKey(g.ActiveBranch)
	for _, b := range g.Frontier() {
		if activeKey != "" && g.StateKey(b.ID) == activeKey { // exact duplicate state (A*)
			id := b.ID
			return &id
		}
		bt := realThoughts(g.BranchThoughts(b.ID))
		if len(bt) > 0 && c.similar(bt[len(bt)-1].Text, lastText) >= c.mergeThreshold() { // fuzzy near-duplicate
			id := b.ID
			return &id
		}
	}
	return nil
}

// -- the decision --------------------------------------------------------

// DecideNext is the whole-graph decision (THINK/BRANCH/MERGE/BACKTRACK/ACT/STOP). The Python
// 8-kwarg signature becomes (graph, opts) — construct opts from DefaultDecideOptions() so the
// two true-by-default kwargs (BudgetStop / ActOnExhaustion) are preserved. Mirrors Python
// Controller.decide_next.
func (c *Controller) DecideNext(g *graph.ThoughtGraph, opts DecideOptions) types.Decision {
	real := realThoughts(g.ActiveContext())
	var lastT *types.Thought
	if len(real) > 0 {
		lastT = &real[len(real)-1]
	}
	// A flagged *injection* is a suspicious claim to verify; flagged *generation* is just
	// "still working it out" and does not by itself warrant a branch.
	flaggedInjection := lastT != nil && lastT.Source == types.INJECTED &&
		lastT.Confidence < c.eff().FlagThreshold
	needsTruth := c.needsGroundTruth(g)
	be := c.BranchExhausted(g)
	le := c.LoopExhausted(g)

	decision, stopKind, reason := c.choose(g, lastT, flaggedInjection, needsTruth, opts, be, real)
	// Soft policy (slice d, §4.2): in the THINK-default zone, sample a discretionary move from the
	// Boltzmann policy so the previously flat chain becomes tunably active. Hard rails already fired.
	if decision == types.THINK && c.softEnabled() {
		if m := c.softChoose(g, real, be); m != types.THINK {
			decision, reason = m, "soft policy: "+m.String()
		}
	}
	c.Decisions++
	ambiguity := c.ambiguity(g, lastT, real, be)

	var stopKindName *string
	if stopKind != nil {
		s := stopKind.String()
		stopKindName = &s
	}
	c.LastMeta = ControllerMeta{
		Decision:          decision.String(),
		HeuristicDecision: decision.String(),
		StopKind:          stopKindName,
		Reason:            reason,
		BranchExhausted:   be,
		LoopExhausted:     le,
		Flagged:           flaggedInjection,
		NeedsGroundTruth:  needsTruth,
		Ambiguity:         round2(ambiguity),
		Escalated:         false,
	}

	// Smart switch. 'llm' escalates every decision (the naive baseline). 'hybrid' escalates only
	// GENUINE judgment calls: ambiguous AND non-structural. Structural moves (BRANCH/ACT/
	// BACKTRACK/MERGE) are architectural rules the model must not override — the head-to-head
	// showed a blind model wrongly STOPs on a conflict, so it only advises the continue-vs-stop
	// judgment (THINK/STOP), never the graph moves.
	structural := decision == types.BRANCH || decision == types.ACT ||
		decision == types.BACKTRACK || decision == types.MERGE ||
		decision == types.DELIVER // a waiting user is a FACT; the model may not silence the reply
	doEscalate := c.mode == "llm" ||
		(c.mode == "hybrid" && ambiguity >= 0.5 && !structural)
	if doEscalate {
		llmDecision, ok := c.llmDecide(g, opts.ActedBranch)
		if ok {
			c.Escalations++
			c.LastMeta.Escalated = true
			c.LastMeta.LLMSet = true
			c.LastMeta.LLMDecision = llmDecision.String()
			c.LastMeta.Agree = llmDecision == decision
			if llmDecision != decision {
				c.emit(events.Decision,
					"escalate: heuristic="+decision.String()+" -> llm="+
						llmDecision.String()+" (ambiguity="+f2(ambiguity)+")",
					events.D{
						"decision":  llmDecision.String(),
						"escalated": true,
						"why":       c.LastMeta.LLMWhy, // P6: surface the model's reasoning
					})
			}
			decision = llmDecision
			c.LastMeta.Decision = decision.String()
		} else {
			// Pattern-C Rule 4: the model was consulted on an escalation-eligible decision but
			// declined (off-list / no Decider) — the deterministic FLOOR stands. Surface it, never
			// silent. (The default control mode never reaches here: doEscalate is false.)
			c.emitFloorStands(decision, ambiguity, "model-declined", true)
		}
	} else if c.mode != controlMode && structural && ambiguity >= 0.5 {
		// Pattern-C Rule 4: a HIGH-ambiguity STRUCTURAL move in an escalating mode would have
		// escalated but for the structural guard (the model may not override BRANCH/ACT/BACKTRACK/
		// MERGE). The floor is authoritative; surface the non-escalation. (hybrid only — llm mode
		// would have escalated regardless, so it is handled in the doEscalate branch above.)
		c.emitFloorStands(decision, ambiguity, "structural-move", false)
	}

	c.emit(events.Exhaustion,
		"branch_exhausted="+boolStr(be)+" loop_exhausted="+boolStr(le)+
			" flagged="+boolStr(flaggedInjection)+" ambiguity="+f2(ambiguity),
		events.D{
			"branch_exhausted":   c.LastMeta.BranchExhausted,
			"loop_exhausted":     c.LastMeta.LoopExhausted,
			"flagged":            c.LastMeta.Flagged,
			"needs_ground_truth": c.LastMeta.NeedsGroundTruth,
		})
	c.emit(events.Decision, decision.String()+": "+reason,
		events.D{
			"decision":  decision.String(),
			"reason":    reason,
			"stop_kind": stopKindData(stopKind),
		})
	return decision
}

// ambiguity scores how close the situation is to a GENUINE judgment threshold — "is the goal
// met?" (confidence near the done threshold) and "is this line spent?" (recent confidence near
// the exhaustion floor). Structural decisions (conflict->BRANCH, flag->verify) are
// architectural *rules*, not judgment calls, so the heuristic stays authoritative there.
// Mirrors Python Controller._ambiguity. (flaggedInjection/conflict/be are passed in Python but
// only `last` and `real` are read; we keep the same body.)
func (c *Controller) ambiguity(_ *graph.ThoughtGraph, lastT *types.Thought, real []types.Thought, _ bool) float64 {
	score := 0.0
	if lastT != nil && (lastT.Source == types.INJECTED || lastT.Source == types.GENERATED) {
		// borderline "is the goal met?" — confidence sitting near the done threshold
		score = math.Max(score, 1.0-math.Min(1.0, math.Abs(lastT.Confidence-c.eff().DoneConfidence)/0.12))
	}
	if len(real) > 0 && len(real) >= c.eff().ExhaustAfter-1 {
		recent := maxConfidence(last(real, 3))
		// borderline "is this line spent?" — recent confidence near the exhaustion floor
		score = math.Max(score, 1.0-math.Min(1.0, math.Abs(recent-c.eff().ExhaustConf)/0.12))
	}
	return math.Min(1.0, score)
}

// llmDecide asks the model to choose among the VALID moves; the chosen name is mapped back to a
// Decision via Decision[choice]. ok=false keeps the heuristic (Python returned None). It informs
// the model of the machine-computed situation only via the options list (a fair test). Mirrors
// Python Controller._llm_decide — but on the CORRECTED, narrowed Decider signature
// (Decide(goal, ctx, options) -> (choice, why)); the Python `heuristic.name`/`state` args are
// dropped (LLM-path narrowing; validated via doctor, not the oracle). Mirrors Python
// Controller._llm_decide.
func (c *Controller) llmDecide(g *graph.ThoughtGraph, actedBranch bool) (types.Decision, bool) {
	decider, ok := c.backend.(backends.Decider)
	if !ok {
		return 0, false
	}
	options := []string{"THINK", "BRANCH", "STOP"}
	if !actedBranch {
		options = append(options, "ACT")
	}
	if len(g.Frontier()) > 0 && c.backtrackAllowed() {
		// CONFIG (retrace-off): don't offer BACKTRACK to the model when the toggle forbids it, so the
		// ablation holds on the llm/hybrid paths too (the deterministic ladder already excludes it).
		options = append(options, "BACKTRACK")
	}
	if c.MergeTarget(g) != nil {
		options = append(options, "MERGE")
	}
	choice, why := decider.Decide(g.Goal, g.ActiveContext(), options)
	// decide returns (choice, why) — capture the model's reasoning (P6), don't discard it.
	if why != "" {
		c.LastMeta.LLMWhy = why
	}
	// choice=="" is Python's None (model declined / off-list) -> keep the heuristic.
	d, ok := types.ParseDecision(choice)
	if !ok {
		return 0, false
	}
	return d, true
}

// choose is the decision ladder: a sequence of GUARDED returns, first match wins. Mirrors
// Python Controller._choose exactly (order is load-bearing).
func (c *Controller) choose(
	g *graph.ThoughtGraph, lastT *types.Thought, flaggedInjection, needsTruth bool,
	opts DecideOptions, be bool, real []types.Thought,
) (types.Decision, *types.StopKind, string) {
	_ = lastT // last is read only for flagged_injection upstream; the ladder reads the graph

	if c.GoalSatisfied(g) && !opts.WorkflowPending {
		return c.stopOrDeliver(g, types.GOAL_MET, "goal satisfied")
	}
	// Awake user-line deliver deadline. The endless awake STREAM never gives up, but a line that
	// holds an UNANSWERED user turn is a BOUNDED conversational obligation, not the wander: once the
	// mind has worked it for the budget without satisfying the goal — and it is not still owed a
	// FIRST grounding ACT (a ground-truth question must reach reality before it answers) — give up
	// and ANSWER the user (stopOrDeliver -> DELIVER, since the user is waiting), so a conversational
	// turn a real substrate never trips GoalSatisfied on still gets a reply instead of being set aside
	// forever. Budget 0 (reactive / disabled) ⇒ never fires; a non-user active line ⇒ never fires
	// (UnresolvedUserInput is false), so the endogenous wander is untouched.
	if opts.AwakeUserBudget > 0 && g.UnresolvedUserInput(g.ActiveBranch) &&
		len(real) >= opts.AwakeUserBudget && !(needsTruth && !opts.ActedBranch) {
		return c.stopOrDeliver(g, types.GIVE_UP, "awake: user line worked to budget -> answer the user")
	}
	// The step budget is an episodic give-up; it does not apply to the endless awake stream.
	if opts.BudgetStop && len(real) >= c.eff().MaxSteps {
		return c.stopOrDeliver(g, types.GIVE_UP, "over step budget")
	}
	if opts.WorkflowPending {
		return types.THINK, nil, "advancing through workflow phases"
	}
	if opts.Conflict && !opts.AlreadyForked {
		return types.BRANCH, nil, "conflicting injections -> fork"
	}
	if flaggedInjection && !opts.ActedBranch {
		if needsTruth {
			return types.ACT, nil, "flagged injection + needs ground truth -> act"
		}
		if !opts.VerifiedBranch {
			return types.BRANCH, nil, "flagged injection -> branch to verify"
		}
		if opts.ActOnExhaustion {
			return types.ACT, nil, "still flagged after verifying -> act to confirm"
		}
		return types.THINK, nil, "still flagged — awake mind moves on, doesn't act"
	}
	if be {
		// CONFIG (retrace-off): BACKTRACK resumes a viable stashed sibling — the internal exit that
		// keeps the graph multi-line. When conscious.allow_backtrack is OFF the move is forbidden, so
		// even with a viable sibling the ladder falls through to the external exit (ACT) / STOP / THINK,
		// and the graph degrades to a single line (measuring-stick-spec §5.8 ablation). Bypass, not
		// delete: the wire stays, config.skip records it, determinism holds.
		if c.anyViableSibling(g) && c.backtrackAllowed() {
			return types.BACKTRACK, nil, "branch exhausted; viable sibling exists"
		}
		if opts.ActOnExhaustion {
			if !opts.ActedBranch {
				return types.ACT, nil, "loop exhausted -> import ground truth"
			}
			return c.stopOrDeliver(g, types.GIVE_UP, "loop exhausted, already acted")
		}
		// Awake wandering that runs dry doesn't "go run code" — it just moves on to a new line.
		return types.THINK, nil, "branch exhausted — let the awake mind move on"
	}
	if needsTruth && !opts.ActedBranch && len(real) >= 3 {
		return types.ACT, nil, "question demands ground truth"
	}
	if c.MergeTarget(g) != nil {
		return types.MERGE, nil, "convergent branches -> merge"
	}
	return types.THINK, nil, "continue on current branch"
}

// stopOrDeliver is the terminal fork of the ladder (A2): closing a line while the user is waiting
// means SPEAKING — DELIVER, the executive's own decision, never engine plumbing glued to STOP.
// STOP is the silent close. The fork is a structural FACT (the graph holds an unanswered
// USER_INPUT — Pattern A, the GLOBAL derivation: an async observation may satisfy the goal on a
// line that does not itself hold the user's words, and the goal being closed IS the user's goal
// until per-line goal binding, G5). The model may never override it; delivery resolves the line,
// so the same situation re-decides as STOP and one turn gets one reply.
func (c *Controller) stopOrDeliver(g *graph.ThoughtGraph, kind types.StopKind, reason string) (types.Decision, *types.StopKind, string) {
	if g.UserWaiting() {
		return types.DELIVER, stopKindPtr(kind), reason + " -> deliver the answer"
	}
	return types.STOP, stopKindPtr(kind), reason
}

// anyViableSibling reports whether any frontier branch's value clears the pursuit threshold —
// the `any(b.value >= pursuit_threshold for b in graph.frontier())` test shared by
// loop_exhausted and the exhaustion branch of _choose.
func (c *Controller) anyViableSibling(g *graph.ThoughtGraph) bool {
	for _, b := range g.Frontier() {
		if b.Value >= c.eff().PursuitThreshold {
			return true
		}
	}
	return false
}

// -- A-RAG4: V(s)-triggered active re-sourcing -------------------------------

// resourceRelevanceFloor is the lexical-overlap floor the active node's text must clear against the
// goal to count as a "goal-relevant" node (the active-inference trigger fires only when the line is
// uncertain ABOUT THE GOAL, not idly wandering). Set at the present-rung floor so a node is judged
// goal-relevant the same way the sourcing ladder judges "present" — one consistent relevance notion.
//
// BORROWED-THRESHOLD NOTE (recognition discipline, MAD #2): the literal 0.34 is borrowed from the
// sourcing ladder's `presentFloor` (subconscious/sourcing.go) — but that floor was tuned for a
// retrieval.LexicalScore comparison, whereas THIS gate scores with types.Jaccard (see c.similar). The
// two scorers need not share a distribution, so reusing the literal is a SILENT cross-gate borrow: a
// threshold tuned for gate-A (present-rung, LexicalScore) reused for gate-B (resource-trigger, Jaccard).
// It is NOT reset here because controller.active_resource is now DEFAULT-ON (A-RAG4 default-flip,
// 2026-06-21), so re-tuning it would change the live path — it needs a PAIRED LIVE-CLAUDE A/B before any
// change (the recognition discipline: measure the borrow, do not silently keep OR silently re-tune it).
const resourceRelevanceFloor = 0.34

// resourceTriggerOn reports whether V(s)-triggered active re-sourcing is enabled. This is an OPT-IN
// feature whose baseline is OFF, so off is the DEFAULT and must be a SILENT no-op — it does NOT emit
// config.skip (that event is for the ablation toggles whose default is ON and whose bypass is the
// observably-meaningful event; an opt-in default-OFF instrument being off is not a deviation, so emitting
// a skip every tick would break the byte-identical goldens). nil gate ⇒ OFF (the nil-as-enabled
// convention of *config.Gate is inverted here on purpose — see the activeResource field doc).
func (c *Controller) resourceTriggerOn() bool {
	return c.activeResource != nil && c.activeResource.Enabled()
}

// ResourceTrigger is the A-RAG4 Pattern-A control decision: should the harness ACTIVELY RE-SOURCE the
// active node? It fires iff (controller.active_resource ON) AND (V(s) of the active branch is LOW — at or
// below the exhaustion-confidence floor, i.e. the line is not earning its keep) AND (the active node is
// GOAL-RELEVANT — its text overlaps the goal above resourceRelevanceFloor, so re-sourcing pays into the
// goal, not an idle tangent) AND (this branch has NOT already re-sourced — the BOUND that keeps it from
// looping; the engine passes alreadySourced from its per-branch marker). It returns the re-source query
// (the active node's text, the locus of the uncertainty) and a human reason. This is the FLARE / active-
// inference epistemic trigger expressed in the harness's PRINCIPLED uncertainty signal (V(s)), not a
// token-probability hack: low V(s) on a goal-relevant line == high expected information gain from
// retrieval. It NEVER acts itself — it only signals; the engine re-invokes the sourcing ladder. No model
// call (pure CONTROL). fire=false leaves the loop byte-identical.
//
// The bound is structural, not a step counter: at most ONE re-source per branch (alreadySourced), so the
// plant's branching ratio is untouched (a re-source is an injection-fuel read, not a fork) — the awake
// durability conditions hold (re-validated by the durability gate; plantMoved on the build contract).
func (c *Controller) ResourceTrigger(g *graph.ThoughtGraph, vActive float64, alreadySourced bool) (fire bool, query, reason string) {
	if !c.resourceTriggerOn() || alreadySourced || g == nil {
		return false, "", ""
	}
	real := realThoughts(g.ActiveContext())
	if len(real) == 0 {
		return false, "", "" // nothing voiced yet — no node to re-source
	}
	// low V(s): the active line's value is at/below the exhaustion-confidence floor — it is not earning
	// its keep, exactly the regime FLARE retrieves into (the line is uncertain). Using the same floor the
	// Controller already uses for "is this line spent?" keeps ONE notion of low-value across the executive.
	if vActive > c.eff().ExhaustConf {
		return false, "", ""
	}
	// goal-relevant: the recent active LINE works on a goal-relevant question. Scan the last few real
	// nodes (the same recent window the rerank/exhaustion logic reads) and re-source the MOST goal-relevant
	// one — the locus of the goal-relevant uncertainty. This is line-level, not strictly the last node: a
	// freshly-generated tangent as the very last node should not mask that the line is working the goal.
	// The goal ROOT (a node whose text IS the goal — the scaffolding) is EXCLUDED: matching it is trivial
	// (every line carries the root) and tells us nothing about whether the line is doing goal-relevant work.
	recent := last(real, 3)
	goalText := strings.TrimSpace(g.Goal)
	bestSim, bestText := 0.0, ""
	for _, t := range recent {
		if strings.TrimSpace(t.Text) == goalText {
			continue // the goal root — scaffolding, not work on the goal
		}
		if s := c.similar(t.Text, g.Goal); s > bestSim {
			bestSim, bestText = s, t.Text
		}
	}
	if bestSim < resourceRelevanceFloor {
		return false, "", "" // the recent line is an idle tangent — re-sourcing it does not pay into the goal
	}
	q := strings.TrimSpace(bestText)
	if q == "" {
		q = strings.TrimSpace(g.Goal)
	}
	reason = "low V(s)=" + f2(vActive) + " on goal-relevant line -> re-source"
	c.emit(events.ResourceTrigger, "active re-source: "+clipText(q, 48),
		events.D{
			"v_active":     round2(vActive),
			"exhaust_conf": round2(c.eff().ExhaustConf),
			"goal_sim":     round2(bestSim),
			"query":        clipText(q, 96),
			"branch":       g.ActiveBranch,
			"reason":       reason,
		})
	return true, q, reason
}

// -- quiescence & interrupt ---------------------------------------------

// Quiescent reports whether the engine should fall idle: no pending input, no outstanding
// action, and no frontier branch worth pursuing. Mirrors Python Controller.quiescent.
func (c *Controller) Quiescent(g *graph.ThoughtGraph, pendingInput, outstandingAction bool) bool {
	if pendingInput || outstandingAction {
		return false
	}
	best := 0.0
	for _, b := range g.Frontier() {
		if b.Value > best {
			best = b.Value
		}
	}
	return best < c.eff().PursuitThreshold
}

// OnInterrupt compresses the active branch (SUSPEND), focuses a fresh branch seeded by the
// user input, and re-seeds its value toward the new goal. Returns the new branch id. Mirrors
// Python Controller.on_interrupt.
func (c *Controller) OnInterrupt(g *graph.ThoughtGraph, mcp *graph.ThoughtMCP, userThought types.Thought) int {
	seed := userThought
	newID := mcp.Branch("user interrupt: "+userThought.Short(40), &seed)
	mcp.Focus(newID)
	// Warm-start only: any rerank BEFORE the next value.Update sees the user's line on top. The
	// re-seed is SUSTAINED by the pending-user term inside V(s) itself (value.AppraiseBranch, A1)
	// until delivery — this write alone was a one-tick blip clobbered by the next Update. The
	// epistemic projection warm-starts too (the pre-A1 behavior: being addressed lit up trust for
	// the remainder of the tick; the next Update recomputes it honestly).
	g.Branches[newID].Value = 1.0
	g.Branches[newID].Epistemic = 1.0
	c.emit(events.Interrupt, "interrupt -> suspend, focus b"+strconv.Itoa(newID),
		events.D{"branch": newID, "text": userThought.Text})
	return newID
}

// ============================================================================
// small local helpers
// ============================================================================

// last returns the last n elements of ts (Python ts[-n:]), or all of them if fewer.
func last(ts []types.Thought, n int) []types.Thought {
	if len(ts) <= n {
		return ts
	}
	return ts[len(ts)-n:]
}

// maxConfidence returns the max confidence over ts, or 0.0 for an empty slice (Python
// max((t.confidence for t in ts), default=0.0)).
func maxConfidence(ts []types.Thought) float64 {
	m := 0.0
	for i, t := range ts {
		if i == 0 || t.Confidence > m {
			m = t.Confidence
		}
	}
	return m
}

// anyObservation reports whether any thought in ts is an OBSERVATION (Python
// `any(t.source is Source.OBSERVATION for t in ...)`).
func anyObservation(ts []types.Thought) bool {
	for _, t := range ts {
		if t.Source == types.OBSERVATION {
			return true
		}
	}
	return false
}

// endsWithQ reports whether the stripped text ends with a question mark (Python
// `text.strip().endswith("?")`).
func endsWithQ(text string) bool {
	return strings.HasSuffix(strings.TrimSpace(text), "?")
}

// stopKindPtr returns a pointer to a StopKind (the _choose ladder returns StopKind | None).
func stopKindPtr(k types.StopKind) *types.StopKind { return &k }

// stopKindData maps an optional StopKind to its wire value: the member NAME string, or nil for
// None (Python `stop_kind.name if stop_kind else None`).
func stopKindData(k *types.StopKind) any {
	if k == nil {
		return nil
	}
	return k.String()
}

// boolStr renders a Go bool as Python's bool repr ("True"/"False") for the summary strings,
// matching the Python f-string interpolation of a bool.
func boolStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// emitFloorStands surfaces a Pattern-C non-escalation (Rule 4): the Controller's deterministic
// decision FLOOR stood because the model was not consulted or declined. modelConsulted is true only
// when the model was actually asked and declined (vs the structural-skip, where it was never asked).
// Mirrors the Filter's escalation.floor_stands emit so the two hybrid escalators read identically.
func (c *Controller) emitFloorStands(decision types.Decision, ambiguity float64, reason string, modelConsulted bool) {
	c.emit(
		events.EscalationFloorStands,
		"critic.decide floor stands ("+decision.String()+", "+reason+
			", ambiguity="+f2(ambiguity)+")",
		events.D{
			"site":            "critic.decide",
			"decision":        decision.String(),
			"floor_decision":  decision.String(),
			"ambiguity":       round2(ambiguity),
			"reason":          reason,
			"model_consulted": modelConsulted,
		},
	)
}

// f2 reproduces Python f"{x:.2f}" — fixed 2 decimals, round-half-to-even (Go's
// FormatFloat and CPython's format both round half-to-even).
func f2(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }

// clipText truncates s to at most n runes (appending no ellipsis) for the resource-trigger event
// summary/data — a rune-safe clip so a long active-node text does not bloat the wire.
func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// round2 reproduces Python round(x, 2) at the emit site (the sink does NOT round): format to 2
// fixed decimals (round-half-to-even) and parse back so the emitted value matches the Python
// wire. +-Inf/NaN pass through unchanged.
func round2(x float64) float64 {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return x
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}
