package flywheel

// Recorder is the per-engine flywheel tap: it BUFFERS the current episode's (state, action) decision
// tuples (the outcome unknown at decision time) and, at episode close, BACKFILLS the terminal grounded
// Outcome onto every buffered tuple before flushing them to the Sink. This is the offline-RL Monte-Carlo
// credit-assignment shape — the grounded label of a trajectory is known only at the end, so it is
// assigned uniformly back over the trajectory's decisions.
//
// The Recorder holds NO clock and NO RNG — the caller passes the seeded tick. It is constructed only when
// the flywheel.capture knob is ON (the engine builds nil otherwise), so the OFF path never allocates a
// Recorder, never buffers, and never writes — byte-identical.
//
// emit is an OPTIONAL observability hook: when set, Recorder fires it once per finalised tuple at close so
// the live loop is observable on flywheel.capture (the bus IS the per-subsystem log). nil ⇒ no event (the
// test path may capture directly off the Sink without the bus).
type Recorder struct {
	sink    Sink
	emit    func(t DecisionTuple)
	buf     []DecisionTuple
	episode string // the current open episode id (set on OpenEpisode)
	step    int    // the next decision index within the open episode
}

// NewRecorder builds a Recorder over a Sink. emit may be nil (no observability event).
func NewRecorder(sink Sink, emit func(t DecisionTuple)) *Recorder {
	return &Recorder{sink: sink, emit: emit}
}

// OpenEpisode starts a fresh trajectory: it FLUSHES any leftover (an episode that opened without a close —
// e.g. an interrupted run) as UNFILLED so no decision is silently dropped, then resets the buffer + step
// counter. Idempotent on the same id is not assumed; the caller opens once per episode.
func (r *Recorder) OpenEpisode(id string) {
	if r == nil {
		return
	}
	r.flushUnfilled()
	r.episode = id
	r.step = 0
	r.buf = r.buf[:0]
}

// RecordDecision buffers one (state, action) tuple for the open episode, stamped with the seeded tick and
// the within-episode step index. The Outcome is left zero (Filled=false) until CloseEpisode backfills it.
func (r *Recorder) RecordDecision(tick int, state StateFeatures, action string) {
	if r == nil {
		return
	}
	r.buf = append(r.buf, DecisionTuple{
		Episode: r.episode,
		Tick:    tick,
		Step:    r.step,
		State:   state,
		Action:  action,
	})
	r.step++
}

// CloseEpisode backfills the terminal grounded Outcome onto every buffered tuple (the Monte-Carlo return
// assignment), flushes them to the Sink (and fires emit per tuple if set), then clears the buffer. With no
// buffered decisions it is a no-op (an episode that decided nothing produces no rows).
func (r *Recorder) CloseEpisode(out Outcome) {
	if r == nil || len(r.buf) == 0 {
		return
	}
	for i := range r.buf {
		r.buf[i].Outcome = out
		r.buf[i].Filled = true
		_ = r.sink.Write(r.buf[i])
		if r.emit != nil {
			r.emit(r.buf[i])
		}
	}
	r.buf = r.buf[:0]
}

// flushUnfilled writes any buffered-but-unclosed tuples as UNFILLED (Filled=false) so an interrupted
// episode's decisions are not lost. Used on OpenEpisode (a new episode supersedes a stale buffer).
func (r *Recorder) flushUnfilled() {
	for i := range r.buf {
		_ = r.sink.Write(r.buf[i])
		if r.emit != nil {
			r.emit(r.buf[i])
		}
	}
	r.buf = r.buf[:0]
}

// Pending reports how many decision tuples are buffered for the open episode (test/inspection only).
func (r *Recorder) Pending() int {
	if r == nil {
		return 0
	}
	return len(r.buf)
}
