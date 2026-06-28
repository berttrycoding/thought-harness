// Package signals derives the per-tick SignalFrame — the cognition's "vital signs" vector —
// purely from the event bus, and persists it to a sidecar JSONL next to the --log event stream.
//
// THE LINCHPIN of the post-session ANALYSIS surface (Track G, G0). Every analysis chart, the
// impulse-response capture, and the power-ON/OFF benchmark diff read this vector. It is the
// future ML/RL substrate: a per-tick labelled vector of the organism's state, persisted per
// session with full provenance. (Design: docs/internal/notes/2026-06-12-tui-mockups-vitals.md "The
// SignalFrame", docs/internal/notes/2026-06-20-shift-tab-analysis-redesign.md §4 Phase 0.)
//
// PATTERN A — pure CONTROL derivation. Every field below is a closed-form read over signals that
// ALREADY exist on the bus (the regulator's n/U/mu/theta, value.update, llm.call telemetry,
// grounding.ground, the port/observation arrivals). NOTHING is invented; this is instrumentation,
// not new cognition. No model call, no wall clock, no unseeded randomness — the derivation is a
// deterministic function of the (deterministically-ordered) event stream, so the frames are
// reproducible and goldenable.
//
// HEADLESS-PURE + ADDITIVE. The Recorder is a plain bus SUBSCRIBER (events.Bus.Subscribe); it
// imports only internal/events and the stdlib. It never reaches into the engine, never mutates
// engine state, and emits NOTHING back onto the bus — so wiring it in is byte-identical to not:
// the event golden is untouched. The frames land in a SIDECAR file (*.signals.jsonl), exactly so
// the event golden stays byte-identical.
package signals

import (
	"encoding/json"
	"io"
	"math"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// SignalFrame is the unified per-tick state vector (one frame per engine tick), derived from the
// event bus. The field set is the vitals-mock schema (docs/internal/notes/2026-06-12-tui-mockups-vitals.md).
// JSON tags use snake_case (the sidecar wire contract); the schema is versioned (SchemaVersion) so a
// later field add does not break a loader reading an older sidecar.
type SignalFrame struct {
	Schema int `json:"schema"` // SchemaVersion — bump on a breaking field change
	Tick   int `json:"tick"`

	// cadence (heart rate + HRV): the substrate-latency story for the tick.
	TickLatencyMs int `json:"tick_latency_ms"` // sum of model-call latency observed this tick
	CallsInTick   int `json:"calls_in_tick"`   // model calls observed this tick

	// excitation / load / baseline / threshold — the regulator's own terms (the durability state).
	N         float64 `json:"n"`          // subcritical-excitation read (1.0 = runaway)
	U         float64 `json:"u"`          // schedulability load (1.0 = saturated)
	Mu        float64 `json:"mu"`         // positive baseline μ
	Theta     float64 `json:"theta"`      // dispatch threshold θ
	LambdaBar float64 `json:"lambda_bar"` // predicted stationary rate λ̄ (∞-guarded → 0 at the cliff)

	// reserve (body battery): budget headroom as a 0–100 read (1−U), drains under load.
	Reserve int `json:"reserve"`

	// value (the active line's worth this tick).
	VActive float64 `json:"v_active"`

	// grounding (respiration): reality observations imported in the trailing window + the grounded ratio.
	ObservationsInWindow int     `json:"observations_in_window"`
	GroundedRatio        float64 `json:"grounded_ratio"`

	// pressure (stress): demand on the system — a user waiting (+ its age in ticks) and ambiguity.
	UserWaiting     bool    `json:"user_waiting"`
	WaitingAgeTicks int     `json:"waiting_age_ticks"`
	Ambiguity       float64 `json:"ambiguity"`

	// faults (fever): substrate fallback + parse-failure rate in the trailing window.
	FallbacksInWindow     int `json:"fallbacks_in_window"`
	ParseFailuresInWindow int `json:"parse_failures_in_window"`

	// focus (coherence): the dominant Controller move this tick (the decision fingerprint).
	Decision string `json:"decision"` // last critic.decision move this tick, "" if none

	// stimulus (the senses): a salient arrival this tick + its kind (the impulse-capture origin).
	SalientInput bool   `json:"salient_input"`
	InputKind    string `json:"input_kind"` // "user" | "reality" | "none"

	// condition: the one-word system story (NOMINAL/ENGAGED/LOADED/DEGRADED), Pattern-A composed.
	Condition string `json:"condition"`

	// arousal (continuous-mode state, "AWAKE"/"DROWSY"/… or "" in reactive mode).
	Arousal string `json:"arousal"`
}

// SchemaVersion is the SignalFrame wire-schema version. The sidecar is append-only history (old logs
// must stay loadable), so a breaking field change bumps this and the loader keys off it.
const SchemaVersion = 1

// windowTicks is the trailing-window width (in ticks) for the windowed counts (grounding, faults).
// Matches the vitals mock's "in 50t" framing. A fixed constant keeps the derivation Pattern-A.
const windowTicks = 50

// Recorder is the stateful bus subscriber that derives one SignalFrame per tick and persists it. It
// accumulates per-tick deltas, and on each events.Tick boundary it FLUSHES the frame for the tick
// that just completed (the state AS OF that tick) before resetting the per-tick accumulators. The
// windowed counts are kept as bounded ring tallies so a long awake session stays bounded.
//
// Not safe for concurrent On() calls — but the bus dispatches synchronously and single-emitter
// (events.Bus contract), so On() is always called from one goroutine in tick order. That is the same
// contract trace.JsonlSink relies on.
type Recorder struct {
	w      io.Writer // sidecar destination (one JSON line per frame); nil ⇒ frames are dropped
	frames []SignalFrame

	// running latched state (carried across ticks — the "level" signals).
	n, u, mu, theta, lambdaBar float64
	vActive                    float64
	ambiguity                  float64
	arousal                    string
	userWaiting                bool
	waitingSince               int // tick a user first started waiting (−1 = not waiting)

	// per-tick accumulators (reset on each tick boundary).
	tick         int
	startedTick  bool
	curLatencyMs int
	curCalls     int
	curDecision  string
	curSalient   bool
	curInputKind string

	// pendingStimulus carries a stimulus that arrived BEFORE the first tick opened (the common
	// reactive case: Submit() emits a port event at tick 0, then the loop runs). It is attributed to
	// the first tick that opens. "" = none. User dominates reality (set once to "user").
	pendingStimulus string

	// trailing-window ring tallies (per-tick contributions; summed over the last windowTicks).
	groundWin    *ringInt // grounded+refuted observations per tick
	groundedWin  *ringInt // grounded-only per tick (for the ratio)
	fallbackWin  *ringInt // llm fallbacks per tick
	parseFailWin *ringInt // parse failures per tick (a fallback with a parse cause)
	curGround    int
	curGrounded  int
	curFallback  int
	curParseFail int
}

// NewRecorder builds a Recorder that writes each frame as a JSON line to w. A nil w makes the
// recorder accumulate frames in memory only (Frames()) and write nothing — handy for tests.
func NewRecorder(w io.Writer) *Recorder {
	return &Recorder{
		w:            w,
		waitingSince: -1,
		groundWin:    newRingInt(windowTicks),
		groundedWin:  newRingInt(windowTicks),
		fallbackWin:  newRingInt(windowTicks),
		parseFailWin: newRingInt(windowTicks),
	}
}

// On is the bus subscriber: it folds one event into the running + per-tick state, and on a Tick
// boundary flushes the completed tick's frame. Register it with bus.Subscribe(rec.On).
func (r *Recorder) On(ev events.Event) {
	switch ev.Kind {
	case events.Tick:
		// A Tick event marks the START of a new tick. If a prior tick was in progress, flush its
		// frame first (the state as of that tick), then reset the per-tick accumulators.
		if r.startedTick {
			r.flush()
		}
		r.startedTick = true
		r.tick = ev.Tick
		r.resetTick()
		// carry a stimulus that arrived before this tick opened (Submit-then-Step) into this tick.
		if r.pendingStimulus != "" {
			r.curSalient = true
			r.curInputKind = r.pendingStimulus
			r.pendingStimulus = ""
		}
		return
	case events.Regulator:
		r.n = num(ev.Data, "n", r.n)
		r.u = num(ev.Data, "U", r.u)
		r.mu = num(ev.Data, "mu", r.mu)
		r.theta = num(ev.Data, "theta", r.theta)
		r.lambdaBar = finiteOrZero(num(ev.Data, "lam_bar", r.lambdaBar))
	case events.Value:
		// The active branch's V(s): the value.update event carries the active branch id ("active")
		// and a per-branch value map ("values": {"b{id}": v}). Read V(active) out of the map; fall
		// back to the grounded "reward" only when the active value is not resolvable.
		if vActive, ok := activeValue(ev.Data); ok {
			r.vActive = vActive
		} else {
			r.vActive = num(ev.Data, "reward", r.vActive)
		}
	case events.LLM:
		r.curCalls++
		r.curLatencyMs += int(num(ev.Data, "ms", 0))
	case events.LLMFallback:
		r.curFallback++
		if reasonContains(ev, "parse", "unparseable", "length") {
			r.curParseFail++
		}
	case events.Ground:
		r.curGround++
		if str(ev.Data, "verdict") == "grounded" {
			r.curGrounded++
		}
		r.markStimulus("reality")
	case events.Observation:
		r.markStimulus("reality")
	case events.Port:
		if str(ev.Data, "source") == "USER_INPUT" {
			r.markStimulus("user")
			r.userWaiting = true
			if r.waitingSince < 0 {
				r.waitingSince = ev.Tick
			}
		}
	case events.Decision:
		if d := str(ev.Data, "decision"); d != "" {
			r.curDecision = d
		}
	case events.Respond, events.Ask:
		// the system answered / asked — the user is no longer waiting on a pending line.
		r.userWaiting = false
		r.waitingSince = -1
	case events.Arousal:
		// the live awake loop emits the arousal level under "to" (continuous.go:69,:263);
		// SetMode emits the engine mode under "mode". Read "to" first, fall back to "mode",
		// so the arousal vital actually tracks AWAKE/DROWSY on the live path (G0 red-team fix).
		if a := str(ev.Data, "to"); a != "" {
			r.arousal = a
		} else if a := str(ev.Data, "mode"); a != "" {
			r.arousal = a
		}
	}
	// ambiguity rides any event that carries it (the port/intake metadata).
	if a, ok := numOK(ev.Data, "ambiguity"); ok {
		r.ambiguity = a
	}
}

// markStimulus records a salient arrival for the current tick. User input dominates a reality
// arrival in the same tick (the impulse origin a benchmark hangs the response latency off). A
// stimulus that arrives before the first tick opens (Submit-then-Step) is buffered and attributed to
// the first tick that opens, so the impulse origin is never dropped.
func (r *Recorder) markStimulus(kind string) {
	if !r.startedTick {
		if r.pendingStimulus != "user" { // user dominates a pre-tick reality arrival
			r.pendingStimulus = kind
		}
		return
	}
	r.curSalient = true
	if r.curInputKind != "user" {
		r.curInputKind = kind
	}
}

// resetTick clears the per-tick accumulators (the level signals carry; the deltas reset).
func (r *Recorder) resetTick() {
	r.curLatencyMs = 0
	r.curCalls = 0
	r.curDecision = ""
	r.curSalient = false
	r.curInputKind = "none"
	r.curGround = 0
	r.curGrounded = 0
	r.curFallback = 0
	r.curParseFail = 0
}

// flush derives the completed tick's frame from the running + per-tick state, pushes it onto the
// trailing-window rings, persists it, and appends it to Frames(). Pure given the accumulated state.
func (r *Recorder) flush() {
	// roll the per-tick contributions into the trailing-window rings.
	r.groundWin.push(r.curGround)
	r.groundedWin.push(r.curGrounded)
	r.fallbackWin.push(r.curFallback)
	r.parseFailWin.push(r.curParseFail)

	obs := r.groundWin.sum()
	grounded := r.groundedWin.sum()
	ratio := 0.0
	if obs > 0 {
		ratio = round3(float64(grounded) / float64(obs))
	}

	waitAge := 0
	if r.userWaiting && r.waitingSince >= 0 {
		waitAge = r.tick - r.waitingSince
		if waitAge < 0 {
			waitAge = 0
		}
	}

	reserve := int(math.Round((1.0 - r.u) * 100))
	if reserve < 0 {
		reserve = 0
	}
	if reserve > 100 {
		reserve = 100
	}

	f := SignalFrame{
		Schema:                SchemaVersion,
		Tick:                  r.tick,
		TickLatencyMs:         r.curLatencyMs,
		CallsInTick:           r.curCalls,
		N:                     round3(r.n),
		U:                     round3(r.u),
		Mu:                    round3(r.mu),
		Theta:                 round3(r.theta),
		LambdaBar:             round3(r.lambdaBar),
		Reserve:               reserve,
		VActive:               round3(r.vActive),
		ObservationsInWindow:  obs,
		GroundedRatio:         ratio,
		UserWaiting:           r.userWaiting,
		WaitingAgeTicks:       waitAge,
		Ambiguity:             round3(r.ambiguity),
		FallbacksInWindow:     r.fallbackWin.sum(),
		ParseFailuresInWindow: r.parseFailWin.sum(),
		Decision:              r.curDecision,
		SalientInput:          r.curSalient,
		InputKind:             r.curInputKind,
		Arousal:               r.arousal,
	}
	f.Condition = condition(f)

	r.frames = append(r.frames, f)
	if r.w != nil {
		if b, err := json.Marshal(f); err == nil {
			_, _ = r.w.Write(b)
			_, _ = io.WriteString(r.w, "\n")
		}
	}
}

// Close flushes the final in-progress tick (the last Tick event has no successor to trigger its
// flush). Idempotent. Always call it at the end of a recorded session.
func (r *Recorder) Close() {
	if r.startedTick {
		r.flush()
		r.startedTick = false
	}
}

// Frames returns the frames derived so far (in tick order). The in-progress tick is included only
// after Close(). The returned slice is the recorder's own buffer — callers read, never mutate.
func (r *Recorder) Frames() []SignalFrame { return r.frames }

// condition composes the one-word system story from the frame's regulator + pressure state, by the
// locked Pattern-A rules (vitals mock §"The monitor"): runaway excitation ⇒ DEGRADED, saturated load
// ⇒ LOADED, awake or a user waiting ⇒ ENGAGED, else NOMINAL.
func condition(f SignalFrame) string {
	switch {
	case f.N >= 1.0:
		return "DEGRADED"
	case f.U >= 0.9:
		return "LOADED"
	case f.UserWaiting || f.Arousal == "AWAKE":
		return "ENGAGED"
	default:
		return "NOMINAL"
	}
}

// --- small helpers (stdlib-only, deterministic) -------------------------------------------------

// ringInt is a fixed-width ring of per-tick int contributions, summed for the trailing window.
type ringInt struct {
	buf  []int
	n    int // count of pushes (caps at len(buf) for the sum window)
	head int
}

func newRingInt(width int) *ringInt {
	if width < 1 {
		width = 1
	}
	return &ringInt{buf: make([]int, width)}
}

func (r *ringInt) push(v int) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

func (r *ringInt) sum() int {
	s := 0
	for _, v := range r.buf {
		s += v
	}
	return s
}

// num reads a float from a data map, tolerating the int/float64/json.Number forms json round-trips
// through; returns def when absent or unparseable.
func num(d map[string]any, key string, def float64) float64 {
	if v, ok := numOK(d, key); ok {
		return v
	}
	return def
}

func numOK(d map[string]any, key string) (float64, bool) {
	if d == nil {
		return 0, false
	}
	switch v := d[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// activeValue extracts V(active) from a value.update payload: the "active" branch id keys into the
// "values" map ({"b{id}": v}). Returns (v, true) when both resolve. Tolerates the int/float forms a
// JSON round-trip yields (the live bus carries native ints; a replayed log carries float64).
func activeValue(d map[string]any) (float64, bool) {
	if d == nil {
		return 0, false
	}
	a, ok := numOK(d, "active")
	if !ok {
		return 0, false
	}
	vals, ok := d["values"].(map[string]any)
	if !ok {
		return 0, false
	}
	key := "b" + itoa(int(a))
	return numOK(vals, key)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func str(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if s, ok := d[key].(string); ok {
		return s
	}
	return ""
}

// reasonContains reports whether the event's summary or its "reason"/"finish_reason" data mentions
// any of the substrings (a coarse parse-failure classifier for fallbacks).
func reasonContains(ev events.Event, subs ...string) bool {
	hay := ev.Summary + " " + str(ev.Data, "reason") + " " + str(ev.Data, "finish_reason") + " " + str(ev.Data, "cause")
	for _, s := range subs {
		if containsFold(hay, s) {
			return true
		}
	}
	return false
}

func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// finiteOrZero maps a non-finite λ̄ (the n≥1 cliff yields +∞) to 0 so the sidecar JSON stays valid
// (json.Marshal errors on ±Inf/NaN). 0 is the honest "off the chart" read for a chart consumer.
func finiteOrZero(f float64) float64 {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0
	}
	return f
}

// round3 pins a derived float to 3 decimals — the wire-contract rounding the components apply right
// before emit (the JsonlSink does NO rounding), keeping the frames stable + goldenable.
func round3(f float64) float64 {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0
	}
	return math.Round(f*1000) / 1000
}
