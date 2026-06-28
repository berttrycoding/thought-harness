// Package scheduler is the LLM-call scheduler — the rate/throughput actuator we name but
// otherwise don't drive.
//
// The model is the scarce local bottleneck. When many subsystems want a call in one tick
// (foreground reasoning, the seam, the Critic, AND a dozen background specialists/sub-agents),
// serving them all is slow and wasteful. This scheduler reserves the model for the
// high-value foreground and caps the background, keyed on the value signal V(s); a deferred
// background CONTENT call surfaces the gap (Pattern B — it never substitutes a template).
// User-pending input preempts (more budget).
//
// It engages only the LLM backend; the test backend makes no model calls, so the scheduler
// is dormant there. Depends only on internal/events (it is part of the Tier-0/1 foundation
// because backends references its type).
package scheduler

import (
	"strings"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// foregroundRoles are the roles that always get the model — never starved behind background
// work. Role is the backend _chat role tag: 'generate', 'transform', 'respond',
// 'form_intention', the Critic 'decide', and the Filter ESCALATION 'judge_admission'.
// Everything else (specialist.*, operator.*, synthesize_program, default-mode) is BACKGROUND
// and shares a per-tick budget. A set (Python tuple membership -> map lookup).
//
// Note the three-pattern split (docs/internal/notes/heuristic-llm-pattern-refactor.md): the Gate's
// 'rank' and the admission FLOOR 'score_admit' are NO LONGER model roles — both are Pattern-A
// deterministic math in internal/control, so they never reach the scheduler at all. The only
// CONTROL role the model still touches is the Filter's Pattern-C ESCALATION ('judge_admission'),
// which is foreground (admission is on the hot path) but fires ONLY on a flagged-fuzzy
// candidate, so its call volume is far lower than the old whole-admit role it replaces.
var foregroundRoles = map[string]struct{}{
	"respond": {}, "form_intention": {}, "generate": {}, "transform": {}, "compress": {},
	"decide": {}, "judge_admission": {},
}

// Config holds the per-tick budget knobs (Python SchedulerConfig).
type Config struct {
	BgBudget     int     // background model calls per tick when engaged / a user waits (Python 3)
	BgBudgetIdle int     // fewer when the active line is low-value (Python 1)
	EngageValue  float64 // active-line value at/above which the fuller budget applies (Python 0.3)
}

// DefaultConfig returns the Python SchedulerConfig defaults.
func DefaultConfig() Config { return Config{BgBudget: 3, BgBudgetIdle: 1, EngageValue: 0.3} }

// LLMScheduler is the V(s)-keyed rate actuator: foreground unbounded, background capped ->
// heuristic.
type LLMScheduler struct {
	// mu guards the budget counters (remaining/Granted/Deferred) so Grant is race-free when several
	// reason-only sub-agents call the shared backend concurrently under per-phase parallelism
	// (THOUGHT_PARALLEL_PHASES). Held only around the counter logic, never across a model call.
	mu        sync.Mutex
	emit      events.Emit // may be nil (dormant)
	cfg       Config
	remaining int
	Granted   int
	Deferred  int
}

// New builds a scheduler. A nil emit is allowed (the deferral event is then skipped). A nil
// config pointer uses the Python defaults.
func New(emit events.Emit, cfg *Config) *LLMScheduler {
	c := DefaultConfig()
	if cfg != nil {
		c = *cfg
	}
	return &LLMScheduler{emit: emit, cfg: c, remaining: c.BgBudget}
}

// TickReset refreshes the per-tick budget. More background budget when a user is waiting or
// the active line is valuable; less when idle/low-value (the scarce model is spent where it
// pays). Mirrors Python tick_reset(value=0.5, user_pending=False) — call with those defaults
// via TickResetDefault.
func (s *LLMScheduler) TickReset(value float64, userPending bool) {
	// Guard the write under the same mu that Grant holds for remaining/Granted/Deferred: TickReset and
	// Grant both touch s.remaining, so the mutex is incomplete without it (safe today only because the
	// tick loop is parked in preFireParallel's wg.Wait while fan-out Grants run — don't rely on that).
	s.mu.Lock()
	if userPending || value >= s.cfg.EngageValue {
		s.remaining = s.cfg.BgBudget
	} else {
		s.remaining = s.cfg.BgBudgetIdle
	}
	s.mu.Unlock()
}

// TickResetDefault applies the Python default arguments (value=0.5, user_pending=False).
func (s *LLMScheduler) TickResetDefault() { s.TickReset(0.5, false) }

// BackgroundRemaining reports how many background grants are left in this tick's budget — the read
// the parallel-phase dispatcher uses to SIZE its concurrent set up front (deterministic first-k
// allocation in roster order) instead of letting goroutine completion order race for the budget.
func (s *LLMScheduler) BackgroundRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remaining
}

// IsForeground reports whether a role gets the model unconditionally. Role tags are
// "layer.operation" (e.g. "action.respond", "conscious.generate") — the foreground set lists
// OPERATIONS, so match the TAIL (rsplit('.',1)[-1]); a bare "respond" works too. Matching the
// HEAD ("action") silently demoted every reasoning call to background -> deferred -> heuristic.
func IsForeground(role string) bool {
	op := role
	if i := strings.LastIndexByte(role, '.'); i >= 0 {
		op = role[i+1:]
	}
	_, ok := foregroundRoles[op]
	return ok
}

// Grant decides whether this call may spend the model now: foreground always; background
// while budget remains. A deferred background call emits one regulator.schedule event (when
// an emit is wired) and returns false so the caller skips the model — a CONTENT role then
// surfaces the gap (Pattern B), never a substituted template.
func (s *LLMScheduler) Grant(role string) bool {
	s.mu.Lock()
	if IsForeground(role) {
		s.Granted++
		s.mu.Unlock()
		return true
	}
	if s.remaining > 0 {
		s.remaining--
		s.Granted++
		s.mu.Unlock()
		return true
	}
	s.Deferred++
	deferred := s.Deferred
	s.mu.Unlock()
	if s.emit != nil {
		s.emit(events.Schedule, "defer "+role+": background budget spent (model skipped)",
			events.D{"role": role, "deferred": deferred})
	}
	return false
}
