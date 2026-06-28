// conformance.go is the engine half of the L0 conformance WIRING SCAN (Track H, benchmark-taxonomy
// docs/internal/notes/2026-06-20-benchmark-taxonomy.md §1 L0 + §5 build-order #1). It makes the wiring-gate lesson
// — "tests passing != the feature runs; a unit that exists but is not on the engine's actual tick is dead"
// — an OBSERVABLE, failable property of a live run.
//
// THE MECHANISM. When the opt-in conformance.self_check knob is ON, NewEngine attaches a PASSIVE coverage
// tap (wireConformanceTap) to the engine's OWN bus. The tap records the SET of subsystem LAYERS the live
// loop actually emitted this run (subconscious / conscious / seam / critic / value / regulator / lifecycle
// / action / ...) plus a raw event count. A run that COMPILED but never exercised a named subsystem (a
// dead-wired component) shows that layer MISSING — and the rollup that owns the per-run-class required-set
// turns the absence into a FAIL. The conscious-stream content is irrelevant here; this is pure CONTROL
// observability (no model call, no RNG, no clock).
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. The tap is wired ONLY when conformance.self_check is ON, so the default
// engine adds NO subscriber, records NOTHING, and emits NO conformance.wiring event — the live loop is
// byte-identical to the pre-conformance engine. The tap is provably side-effect-free: its subscriber only
// flips bits in its own map (it never emits, mutates engine state, or touches the dispatched event), so it
// cannot re-order/duplicate/drop an emit any other subscriber sees (the bus snapshots its subscriber list
// per Emit and fans out independently). Mirrors the introspect.go read_event_log tap exactly.
//
// HEADLESS-PURE + POLICY-FREE. The engine records coverage but holds NO opinion on which layers a run MUST
// exercise — that policy lives in the conformance rollup (internal/conformance), which passes the required
// set to EmitWiringScan. So a new scenario class never forces an engine edit.
package engine

import (
	"sort"
	"sync"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// conformanceTap is the passive wiring-coverage recorder: a side-effect-free set of the subsystem LAYERS
// the engine emitted on its OWN bus this run (events.Event.Layer, derived by the bus from the kind's
// namespace), plus a raw event count. It carries its own mutex because the bus runs subscribers OUTSIDE
// its lock (mid-dispatch fan-out), so concurrent emits append race-free. Not exported — an engine-internal
// instrument observed through conformance.wiring / WiringCoverage.
type conformanceTap struct {
	mu     sync.Mutex
	layers map[string]bool
	events int
}

func newConformanceTap() *conformanceTap {
	return &conformanceTap{layers: make(map[string]bool, 16)}
}

// record marks one layer seen and bumps the event count. A blank layer (a bare/unnamespaced kind with no
// "." — e.g. "tick", "port") still counts as an event but contributes the whole-string layer the bus
// derives, so coverage stays faithful to what the bus actually fanned out.
func (t *conformanceTap) record(layer string) {
	t.mu.Lock()
	t.layers[layer] = true
	t.events++
	t.mu.Unlock()
}

// covered returns the sorted set of layers seen (a stable, deterministic order for the event payload + the
// rollup's set math).
func (t *conformanceTap) covered() []string {
	t.mu.Lock()
	out := make([]string, 0, len(t.layers))
	for l := range t.layers {
		out = append(out, l)
	}
	t.mu.Unlock()
	sort.Strings(out)
	return out
}

// eventCount returns the raw number of events the tap has seen (the "did the loop run at all" floor).
func (t *conformanceTap) eventCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events
}

// conformanceEnabled reports whether the wiring-coverage tap may fire: the opt-in conformance.self_check
// knob is ON AND the tap is wired. nil features / nil tap ⇒ false (the default), so the bare engine never
// records coverage and stays byte-identical.
func (e *Engine) conformanceEnabled() bool {
	return e.features != nil && e.features.Conformance.SelfCheck && e.confTap != nil
}

// wireConformanceTap installs the passive wiring-coverage tap IFF the opt-in conformance.self_check knob is
// on. Called once from NewEngine (after the bus + features resolve). The default path (knob off) wires
// NOTHING: no tap, no subscriber, no behavior change — the live loop stays byte-identical. Mirrors
// wireEventTap.
func (e *Engine) wireConformanceTap() {
	if e.features == nil || !e.features.Conformance.SelfCheck || e.bus == nil {
		return
	}
	e.confTap = newConformanceTap()
	tap := e.confTap
	e.confTapUnsub = e.bus.Subscribe(func(ev events.Event) {
		tap.record(ev.Layer)
	})
}

// WiringCoverage returns (the sorted set of subsystem layers this engine emitted this run, the raw event
// count). When the conformance tap is off it returns (nil, 0). The rollup reads this after a scenario run
// to compute which REQUIRED layers are missing. Exported so internal/conformance can roll per-run coverage
// up across S1..S16.
func (e *Engine) WiringCoverage() (covered []string, eventCount int) {
	if !e.conformanceEnabled() {
		return nil, 0
	}
	return e.confTap.covered(), e.confTap.eventCount()
}

// EmitWiringScan emits the conformance.wiring witness: which subsystem layers the live loop exercised this
// run, and which of the caller-supplied REQUIRED layers are missing (the dead-wired subsystems). It is a
// no-op when the conformance tap is off (knob off ⇒ no witness ⇒ byte-identical). The required set is the
// rollup's policy (the engine holds no opinion on it), so a new scenario class never forces an engine edit.
// Returns whether the scan PASSED (every required layer covered AND at least one event seen) so the caller
// can fold it into the rollup verdict without re-deriving it.
func (e *Engine) EmitWiringScan(required []string) (ok bool) {
	if !e.conformanceEnabled() {
		return false
	}
	covered, count := e.WiringCoverage()
	seen := make(map[string]bool, len(covered))
	for _, l := range covered {
		seen[l] = true
	}
	var missing []string
	for _, r := range required {
		if !seen[r] {
			missing = append(missing, r)
		}
	}
	sort.Strings(missing)
	ok = len(missing) == 0 && count > 0
	e.bus.Emit(events.ConformanceWiring, wiringSummary(ok, len(covered), len(missing), count),
		events.D{
			"covered": covered,
			"missing": missing,
			"events":  count,
			"ok":      ok,
		})
	return ok
}

// wiringSummary renders the one-line console string for the conformance.wiring event. Deterministic — no
// clock, no RNG.
func wiringSummary(ok bool, nCovered, nMissing, events int) string {
	verdict := "PASS"
	if !ok {
		verdict = "FAIL"
	}
	s := "wiring " + verdict + ": " + itoa(nCovered) + " layers, " + itoa(events) + " events"
	if nMissing > 0 {
		s += " (" + itoa(nMissing) + " missing)"
	}
	return s
}
