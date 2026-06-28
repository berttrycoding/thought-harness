// deadline.go is the WF-G time-awareness core (09 §4): the engine-side deadline check over the
// injected Clock seam. Default = time-blind (nil clock / zero deadline ⇒ no time read, no behavior
// change); with a Clock + deadline wired, an episode that exceeds its wall-clock budget is forced to
// STOP — answer best-so-far — and the stop is OBSERVABLE (lifecycle.deadline carries deadline_ms +
// elapsed_ms). The check is Pattern-A (a pure comparison); determinism in tests comes from clock.Fake
// exactly as the seeded RNG gives deterministic randomness.
package engine

import (
	"time"

	clockpkg "github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// SetClock wires the time seam: c is the clock (clock.Wall at the edge, clock.Fake in tests; nil
// reverts to time-blind), deadline the per-episode wall-clock budget (0 = no deadline). Call before
// Process/Run; the stamp is taken at each startEpisode.
func (e *Engine) SetClock(c clockpkg.Clock, deadline time.Duration) {
	e.clk = c
	e.episodeDeadline = deadline
}

// stampEpisodeStart records the episode's wall-clock start when a clock is wired (called from
// startEpisode) and re-arms the once-per-episode deadline event. Time-blind engines never reach the
// clock.
func (e *Engine) stampEpisodeStart() {
	e.deadlineFired = false
	if e.clk != nil {
		e.episodeStart = e.clk.Now()
	}
}

// deadlineExceeded reports whether the running episode has spent its wall-clock budget, emitting the
// observable lifecycle.deadline event ONCE per episode on the tick it first trips (the forced STOP's
// WHY on the bus). Pure comparison; nil clock or zero deadline always reports false without reading
// time.
func (e *Engine) deadlineExceeded() bool {
	if e.clk == nil || e.episodeDeadline <= 0 {
		return false
	}
	elapsed := e.clk.Now().Sub(e.episodeStart)
	if elapsed < e.episodeDeadline {
		return false
	}
	if !e.deadlineFired {
		e.deadlineFired = true
		e.bus.Emit(events.Deadline,
			"episode deadline expired ("+elapsed.Truncate(time.Millisecond).String()+" >= "+
				e.episodeDeadline.String()+") -> STOP best-so-far",
			events.D{
				"deadline_ms": e.episodeDeadline.Milliseconds(),
				"elapsed_ms":  elapsed.Milliseconds(),
			})
	}
	return true
}
