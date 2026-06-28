// Package seams holds the two seams of opposite transparency between the three layers.
//
// watched.go is the watched seam — conscious <-> action, VISIBLE by design (ported from
// thought_harness/seams/watched.py). Intention out, reality in. The FrontActuator's purpose is to
// import ground truth the closed loop cannot manufacture (run the code, the experiment, send the
// thing). Reality must be caught in the open, so the seam is never silent. The async variant fires
// and keeps thinking (continuous mode), inserting the dead-time the durability analysis bounds
// (§2.3) as a DETERMINISTIC latency in TICKS — never real goroutine timing (the stream order must
// stay deterministic for the golden oracle).
package seams

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ToolExecutor is the narrow facet of action's executor the watched seam needs: dispatch one
// ToolCall through the gate pipeline and get the captured ToolResult back. It is declared HERE
// (an interface defined at the consumer) rather than imported as the concrete type so the seam
// need not depend on action/executor.go (Tier 2) — the concrete *action.ToolExecutor satisfies it
// structurally. Python's FrontActuator held the executor as a duck-typed `object` and called
// `.execute(call)`; this is the Go equivalent.
type ToolExecutor interface {
	Execute(call action.ToolCall) action.ToolResult
}

// FrontActuator is the opening. Effectful, reality-facing.
//
// With a real ToolExecutor wired (a workspace configured), Act dispatches a genuine tool — runs the
// suite, runs a command — and returns the REAL captured result, so a failing test produces a real
// Ok=false observation that can refute the closed-loop guess. With no executor (offline tests/CI),
// it falls back to heuristicAct — a deterministic stand-in for execute_in_world(); that fallback is
// the ONLY place a canned outcome survives, and it is never the product path.
type FrontActuator struct {
	// Executor is nil when no workspace is configured (the offline path). A nil interface value
	// (not a non-nil interface wrapping a nil pointer) is what the Act gate checks, mirroring
	// Python's `self.executor is not None`.
	Executor ToolExecutor
}

// NewFrontActuator builds a FrontActuator with the given executor (pass nil for the offline path).
func NewFrontActuator(executor ToolExecutor) *FrontActuator {
	return &FrontActuator{Executor: executor}
}

// Act dispatches the intention to reality and returns a typed Observation. With an executor wired
// and the intention mappable to a tool call, it runs the real tool; otherwise it falls back to the
// deterministic offline stand-in. Mirrors Python FrontActuator.act (which returned a heterogeneous
// dict; the Go form returns the typed Observation union member).
func (f *FrontActuator) Act(intention types.Intention) types.Observation {
	if f.Executor != nil {
		// STRUCTURED (N.4): the intention carries a named Tool + Args formed at the decision point, so
		// dispatch it DIRECTLY — any tool in the registry is reachable, not just the two the scraper maps.
		// Authored=true: this act crossed the WATCHED seam as the conscious's own intention (03 §3) — so a
		// world-change passes the gate-router; a sub-agent's unauthored write does not.
		if intention.Tool != "" {
			return f.obsFromResult(f.Executor.Execute(action.ToolCall{Name: intention.Tool, Args: intention.Args, Authored: true}), "structured")
		}
		// SCRAPED: the unstructured/offline fallback — regex-map the intention to a tool.
		if call, ok := f.toToolCall(intention); ok {
			call.Authored = true // a scraped intention is still the conscious's authored act
			return f.obsFromResult(f.Executor.Execute(call), "scraped")
		}
		// NONE: an executor was present but neither structured nor scraped mapped — an explicit
		// grounding-bridge failure (visible on the observation), never a silent drop.
		obs := f.heuristicAct(intention)
		obs.Bridge = "none"
		return obs
	}
	obs := f.heuristicAct(intention)
	obs.Bridge = "none" // no executor wired ⇒ no real bridge to reality
	return obs
}

// obsFromResult builds an OBSERVATION from a real tool result, tagged with the bridge path that produced
// it (N.4). The OBSERVATION text is a concise grounded summary; the FULL captured output rides on Result
// (the *action.ToolResult, held as `any` so types stays a Tier-0 leaf) for trace-back.
func (f *FrontActuator) obsFromResult(result action.ToolResult, bridge string) types.Observation {
	r := result
	return types.Observation{
		Ok:       !result.IsError,
		Text:     "reality: " + action.SummarizeToolResult(result),
		Tool:     result.Name,
		ExitCode: result.ExitCode,
		Result:   &r,
		Bridge:   bridge,
	}
}

// -- intention -> tool call (a heuristic distillation; staged for LLM-structured emit) -----------

// toToolCall maps an intention to a concrete tool call, returning ok=false (Python's `return None`)
// when the intention is not effectful against a tool (reflect / send with nothing to read/search ->
// the offline heuristic act).
//
// It delegates ENTIRELY to the ONE shared, deterministic selector (action.SelectTool) — the same
// selector the subconscious primitives and sub-agents use, so all three agree on which tools exist.
// This is the grounding fix: a read/search intent (e.g. the reality-sourcer's "source: ...reading
// config/limits.go..." with Kind="measure") now dispatches read_file/search instead of falling
// through to the run_tests measure-default, which is what the old hardcoded switch always did.
func (f *FrontActuator) toToolCall(intention types.Intention) (action.ToolCall, bool) {
	return action.SelectTool(intention.Text, intention.Kind)
}

// -- offline fallback: deterministic stand-in (NOT the product path) -----------------------------

// heuristicAct is the deterministic offline stand-in for execute_in_world(): no executor wired, so
// no real ground truth — a canned but plausible outcome keyed on the intention. The Python if/elif
// ladder becomes a guarded sequence (order is load-bearing: reflect and send short-circuit before
// the keyword checks). NOT the product path.
func (f *FrontActuator) heuristicAct(intention types.Intention) types.Observation {
	low := strings.ToLower(intention.Text)
	// Every outcome here is MADE UP — no real tool ran — so each carries Fabricated:true, the tier-0
	// breadcrumb. It can wire up the plumbing and read as "reality: ..." in the trace, but it is not a
	// grounding source: GroundsReality() is false, and the grounding loop / experiment memory must
	// never let it validate, refute, or be stored as ground truth (P0.6 / SR-4 grounding-integrity).
	switch {
	case intention.Kind == "reflect":
		// No external channel to import ground truth from — reality can't help here.
		return types.Observation{Ok: false, Fabricated: true,
			Text: "I reasoned it through but couldn't reach a confident answer."}
	case intention.Kind == "send":
		return types.Observation{Ok: true, Fabricated: true,
			Text: "reality: delivered, acknowledged by the recipient"}
	case strings.Contains(low, "arithmetic") || strings.Contains(low, "calcul"):
		return types.Observation{Ok: true, Fabricated: true,
			Text: "reality: worked it through by hand — the result checks out"}
	case intention.Kind == "measure" || strings.Contains(low, "test") || strings.Contains(low, "suite"):
		return types.Observation{Ok: true, Fabricated: true,
			Text: "reality: ran the test suite — 12/12 pass, behaviour preserved"}
	case strings.Contains(low, "fixed") || strings.Contains(low, "retry"):
		return types.Observation{Ok: true, Fabricated: true,
			Text: "reality: it ran cleanly this time, exit 0"}
	default:
		return types.Observation{Ok: false, Fabricated: true,
			Text: "reality: NameError at runtime, line 3 — the guess was wrong"}
	}
}

// WatchedSeam is the synchronous (reactive mode) seam: act, then block until reality returns.
type WatchedSeam struct {
	front *FrontActuator
	emit  events.Emit
}

// NewWatchedSeam builds the synchronous watched seam over a FrontActuator and the emit closure.
func NewWatchedSeam(front *FrontActuator, emit events.Emit) *WatchedSeam {
	return &WatchedSeam{front: front, emit: emit}
}

// OpenToReality emits the intention, acts (CONSCIOUS, monitored), emits the act + observation, and
// returns the observation as a high-prior OBSERVATION Thought (id=-1, set when appended). Mirrors
// Python WatchedSeam.open_to_reality.
func (s *WatchedSeam) OpenToReality(intention types.Intention) types.Thought {
	s.emit(events.Intention, "intention: "+intention.Text, events.D{
		"kind": intention.Kind, "text": intention.Text,
	})
	obs := s.front.Act(intention) // CONSCIOUS, monitored
	verdict := "ok"
	if !obs.Ok {
		verdict = "FAILED"
	}
	s.emit(events.Act, "act ("+intention.Kind+") -> "+verdict, events.D{
		"kind": intention.Kind, "ok": obs.Ok,
	})
	s.emit(events.Observation, obs.Text, events.D{"ok": obs.Ok, "watched": true})
	conf := 0.95 // observations carry a high prior either way
	if !obs.Ok {
		conf = 0.9
	}
	return types.Thought{
		ID: -1, Text: graphObservationText(obs), Source: types.OBSERVATION, Confidence: conf, RawReturn: obs,
	}
}

// graphObservationText is the text the OBSERVATION thought carries INTO the graph (the re-readable,
// voiceable, SCORED surface) — value-preserving for a real read/search/shell result so a grounded value
// that sits past the one-line summary's clip is not lost off the scored surface (the A1 voicing-stability
// fix). The one-line emit above still uses obs.Text (the concise summary) for the trace/console. The
// FABRICATED stand-in and any obs with no real ToolResult keep obs.Text verbatim, so the offline/golden
// path is byte-identical (obs.Text == the heuristic outcome there). Only a real tool result with a full
// captured Content carries the value-preserving text — the §6 "reality only via the watched seam" path.
func graphObservationText(obs types.Observation) string {
	if obs.Fabricated {
		return obs.Text // the offline stand-in: no real content, keep the canned outcome (golden-safe)
	}
	r, ok := obs.Result.(*action.ToolResult)
	if !ok || r == nil {
		return obs.Text
	}
	return "reality: " + action.GraphObservationText(*r) // mirror obsFromResult's "reality: " prefix
}

// outstanding is a fired async action awaiting its (deterministic, tick-clocked) feedback. Mirrors
// Python's _Outstanding dataclass.
type outstanding struct {
	intention types.Intention
	readyTick int
}

// PolledObservation pairs a returned async observation with the branch id it originated from
// (Python's (Thought, branch_id | None) tuple). BranchID is *int (nil == Python None). Claim carries
// the originating intention's grounded assertion so the engine can feed the async observation into the
// reality-grounding spine (N.1a) the same way the synchronous path does.
type PolledObservation struct {
	Thought  types.Thought
	BranchID *int
	Claim    string
}

// AsyncWatchedSeam is the non-blocking (continuous mode) seam: fire, keep thinking; feedback returns
// later as a percept. It EMBEDS WatchedSeam (Python subclass), so OpenToReality is still available
// (the engine may still act synchronously while awake). Latency is a deterministic number of TICKS.
type AsyncWatchedSeam struct {
	*WatchedSeam
	Latency     int           // dead-time in TICKS before feedback is ready (Python default 2)
	outstanding []outstanding // fired actions awaiting feedback
}

// NewAsyncWatchedSeam builds the async watched seam with the given tick latency (Python default 2;
// pass 2 explicitly to match the default constructor).
func NewAsyncWatchedSeam(front *FrontActuator, emit events.Emit, latency int) *AsyncWatchedSeam {
	return &AsyncWatchedSeam{
		WatchedSeam: NewWatchedSeam(front, emit),
		Latency:     latency,
	}
}

// Fire dispatches the intention non-blocking and records it as outstanding, ready `latency` ticks
// from now. It emits the intention + the fired-async act, but NOT the observation (that is deferred
// to Poll). Mirrors Python AsyncWatchedSeam.fire.
func (s *AsyncWatchedSeam) Fire(intention types.Intention, nowTick int) {
	s.emit(events.Intention, "async intention: "+intention.Text, events.D{
		"kind": intention.Kind, "text": intention.Text, "async_": true,
	})
	s.outstanding = append(s.outstanding, outstanding{intention: intention, readyTick: nowTick + s.Latency})
	s.emit(events.Act, "fired async ("+intention.Kind+"); feedback in "+itoa(s.Latency)+" ticks", events.D{
		"kind": intention.Kind, "async_": true,
	})
}

// Poll returns one PolledObservation per outstanding action whose feedback is ready (readyTick <=
// nowTick), removing it from the outstanding set. The ready actions are acted on (now, against
// reality), emitted as async observations, and surfaced as high-prior OBSERVATION Thoughts paired
// with their originating branch id. Mirrors Python AsyncWatchedSeam.poll.
//
// Partition order is preserved (ready and remaining keep their original relative order) so the
// emitted event sequence matches Python's two list comprehensions exactly.
func (s *AsyncWatchedSeam) Poll(nowTick int) []PolledObservation {
	var ready, remaining []outstanding
	for _, o := range s.outstanding {
		if o.readyTick <= nowTick {
			ready = append(ready, o)
		} else {
			remaining = append(remaining, o)
		}
	}
	s.outstanding = remaining

	out := make([]PolledObservation, 0, len(ready))
	for _, o := range ready {
		obs := s.front.Act(o.intention)
		s.emit(events.Observation, "(async) "+obs.Text, events.D{
			"ok": obs.Ok, "watched": true, "async_": true,
		})
		conf := 0.95
		if !obs.Ok {
			conf = 0.9
		}
		thought := types.Thought{
			ID: -1, Text: graphObservationText(obs), Source: types.OBSERVATION, Confidence: conf, RawReturn: obs,
		}
		claim := o.intention.Claim
		if claim == "" {
			claim = o.intention.Text
		}
		out = append(out, PolledObservation{Thought: thought, BranchID: o.intention.BranchID, Claim: claim})
	}
	return out
}

// HasOutstanding reports whether any fired async action is still awaiting feedback. Mirrors Python
// AsyncWatchedSeam.has_outstanding.
func (s *AsyncWatchedSeam) HasOutstanding() bool { return len(s.outstanding) > 0 }

// OutstandingCount reports how many fired async actions are still awaiting feedback — the durability
// accounting reads it for action_outstanding (Python `len(self.awatched.outstanding)`). The
// outstanding slice is unexported, so the engine reaches it through this accessor.
func (s *AsyncWatchedSeam) OutstandingCount() int { return len(s.outstanding) }

// itoa is a tiny stdlib-free int->string for the one summary-string interpolation here (keeps the
// seam from importing strconv just to format the latency in a console summary).
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
