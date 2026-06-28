// Package flywheel is the OFFLINE-RL DATA FLYWHEEL — the capture substrate (Phase 0) of the
// learned-policy roadmap (docs/internal/notes/2026-06-21-harness-rl-ml-roadmap.md §6 P0 + §6.5). It logs, per
// Controller decision, a training TUPLE — (state-features, action, grounded-outcome) — into an
// append-only dataset so a LATER offline learner (a contextual bandit / REINFORCE / a distilled-V head,
// the §3 algorithm table) can train on it WITHOUT online exploration. The single binding constraint the
// roadmap fixes (W=1 single-trajectory, determinism-by-default, no online exploration) IS the offline-RL
// regime (Levine 2020), so the substrate everything else learns from is a passively-captured corpus, not
// an exploring agent. THIS PACKAGE IS THE DATA TAP, NOT THE LEARNER — it instruments only; no policy
// changes, no decision is altered by capture.
//
// THE §6.5 INVARIANT (load-bearing): the OUTCOME label is the INDEPENDENT TERMINAL/grounded signal — did
// the line get GROUNDED / SOLVED / REFUTED against reality — NEVER a self-judgment. The engine marking
// its own speech "ok" is forbidden (the Filter exists to kill laundered self-grading), so the label is
// sourced from the grounding spine (a REAL observation; fabricated reality is rejected upstream) and from
// the StopKind GOAL_MET (which itself requires a confirmed OBSERVATION). A naive learner regressing value
// on a self-graded label would reward the hallucinated branches the Filter kills — so the label MUST be
// the environment reward, captured here as such.
//
// DETERMINISM + HEADLESS PURITY. The Recorder holds NO clock and NO RNG of its own: every tuple is stamped
// with the seeded engine tick passed in by the caller. The JSONL writer is a plain io.Writer sink (the
// edge owns the file); the in-memory sink captures tuples for the deterministic test. So on the seeded
// test double the dataset is byte-for-byte reproducible across runs — the determinism contract (§5).
//
// CREDIT ASSIGNMENT (the offline-RL shape). A decision's grounded outcome is not known at decision time;
// it arrives at episode close (or when an ACT imports an observation). So the Recorder BUFFERS the
// per-episode decision tuples (state + action, outcome unknown) and BACKFILLS the terminal grounded label
// onto every tuple of the episode at CloseEpisode — the standard Monte-Carlo return assignment over a
// trajectory. Each finalised tuple is then flushed to the Sink.
package flywheel

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// StateFeatures is the projection o_t of the formal-model state s=(G,R,A,W,P) the policies actually
// observe (2026-06-21-harness-formal-model.md §1, §8): the active-branch scalars + the regulator + arousal
// + the workflow latch + the pending-user input. It is the low-dimensional, fixed-width feature vector a
// contextual bandit / REINFORCE / V-head consumes — NOT the latent whole (compressed-branch text, the
// substrate's hidden state). Every field traces to a cited accessor.
type StateFeatures struct {
	// --- the active branch (G_t projection, §1.1) ---
	BranchID       int     `json:"branch_id"`       // the EXPANDED branch a* (graph.ActiveBranch)
	Value          float64 `json:"value"`           // V(s_b) — the bootstrap critic (Branch.Value, value.go:163)
	Epistemic      float64 `json:"epistemic"`       // V_epi(s_b) — content quality, no pending term (Branch.Epistemic)
	BranchLen      int     `json:"branch_len"`      // |ThoughtIDs| of the active branch (line length so far)
	Depth          int     `json:"depth"`           // parent-branch hops to a root (graph.Depth)
	Frontier       int     `json:"frontier"`        // |Frontier| — the A* open-set size (stashed viable siblings)
	UserUnresolved bool    `json:"user_unresolved"` // UnresolvedUserInput(a*) — a held user turn on this line

	// --- the regulator R_t (§1.2) ---
	Theta  float64 `json:"theta"`  // admission threshold θ (the control variable, regulator.Theta)
	LamHat float64 `json:"lamhat"` // EMA intensity λ̂ (regulator.LamHat)
	N      float64 `json:"n"`      // branching ratio n = EMA(forks/tick) (regulator.N) — the n<1 durability axis
	Mu     float64 `json:"mu"`     // baseline/immigrant rate μ (regulator.Mu) — the μ>0 awake axis
	U      float64 `json:"u"`      // utilization = branchesLive/FocusCapacity (regulator.Util) — the U≤1 axis

	// --- arousal A_t (§1.3) + workflow W_t (§1.4) + pending P_t (§1.5) ---
	Arousal         string `json:"arousal"`          // ASLEEP|DROWSY|AWAKE (the afferent-gain automaton)
	WorkflowPending bool   `json:"workflow_pending"` // a synthesised program still has phases (blocks STOP)
	PendingUser     bool   `json:"pending_user"`     // UserWaiting(G) — the most decision-relevant exogenous input
	Mode            string `json:"mode"`             // reactive|continuous (the regime)
}

// Outcome is the GROUNDED label — the independent terminal signal (§6.5). It is filled in at episode close
// (the genuine environment reward), NEVER self-graded. GReturn / GoalMet are the goal-relative scalar the
// existing REINFORCE learner already uses (reactive.go:927); EpisodeGrounded / GroundedObs / RefutedObs
// come straight off the grounding spine (a real observation; fabricated reality is rejected upstream).
type Outcome struct {
	GReturn         float64 `json:"greturn"`          // 1.0 iff StopKind==GOAL_MET else 0.0 (the env return, reactive.go:927)
	GoalMet         bool    `json:"goal_met"`         // the StopKind was GOAL_MET (requires a confirmed OBSERVATION)
	StopKind        string  `json:"stop_kind"`        // the stopping taxonomy (GOAL_MET|GIVE_UP|...)
	EpisodeGrounded bool    `json:"episode_grounded"` // grounding.Len grew this episode (reality was imported at all)
	GroundedObs     int     `json:"grounded_obs"`     // count of OBSERVATIONs that GROUNDED a claim this episode (+1 each, §4.2)
	RefutedObs      int     `json:"refuted_obs"`      // count of OBSERVATIONs that REFUTED a claim this episode (-0.5 each, §4.2)
}

// DecisionTuple is one captured training row: (state, action, outcome) for a single Controller decision,
// stamped with the seeded tick + the episode id so the offline learner can group a trajectory and assign
// the Monte-Carlo return. Filled is true once the terminal outcome has been backfilled at episode close.
type DecisionTuple struct {
	Episode string        `json:"episode"` // the cognitive-process id (groups one trajectory's tuples)
	Tick    int           `json:"tick"`    // the seeded engine tick at the decision (NO clock/RNG here)
	Step    int           `json:"step"`    // the decision index within the episode (0-based)
	State   StateFeatures `json:"state"`   // o_t — the observed state projection at decision time
	Action  string        `json:"action"`  // a ∈ {THINK,BRANCH,MERGE,BACKTRACK,ACT,STOP,DELIVER} (the spine)
	Outcome Outcome       `json:"outcome"` // the grounded terminal label (backfilled at episode close)
	Filled  bool          `json:"filled"`  // the outcome has been assigned (false ⇒ episode still open)
}

// Sink consumes finalised tuples. The two implementations are JSONLSink (append-only file at the edge) and
// MemSink (deterministic in-memory capture for tests). The Recorder writes through it on CloseEpisode.
type Sink interface {
	Write(DecisionTuple) error
}

// JSONLSink writes one JSON object per line to an io.Writer (append-only, deterministic — no clock, no
// re-ordering). The edge owns the *os.File; this keeps the engine headless-pure (the engine never does
// I/O — it hands the Recorder a Sink).
type JSONLSink struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// NewJSONLSink wraps an io.Writer in a buffered, line-delimited JSON sink.
func NewJSONLSink(w io.Writer) *JSONLSink { return &JSONLSink{w: bufio.NewWriter(w)} }

// Write appends one tuple as a JSON line. Deterministic field order (encoding/json over a struct).
func (s *JSONLSink) Write(t DecisionTuple) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(b); err != nil {
		return err
	}
	_, err = s.w.Write([]byte("\n"))
	return err
}

// Flush flushes the buffered writer (call on close at the edge).
func (s *JSONLSink) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Flush()
}

// MemSink captures finalised tuples in memory — the deterministic test sink. It carries its own mutex
// because the bus/engine may emit from a worker during a parallel phase, though the Recorder itself is
// called only on the serial spine.
type MemSink struct {
	mu     sync.Mutex
	Tuples []DecisionTuple
}

// NewMemSink returns an empty in-memory sink.
func NewMemSink() *MemSink { return &MemSink{} }

// Write appends one finalised tuple.
func (m *MemSink) Write(t DecisionTuple) error {
	m.mu.Lock()
	m.Tuples = append(m.Tuples, t)
	m.mu.Unlock()
	return nil
}

// All returns a copy of the captured tuples (deterministic order = capture order).
func (m *MemSink) All() []DecisionTuple {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DecisionTuple, len(m.Tuples))
	copy(out, m.Tuples)
	return out
}
