package config

import "github.com/berttrycoding/thought-harness/internal/events"

// Gate is the small read-only handle a component holds to its own toggle. A component checks
// g.Enabled() at its decision point; when disabled it short-circuits to pass-through and calls
// g.Skip(reason) to emit config.skip. The Gate carries a getter closure over the SHARED
// *HarnessConfig (so a live flip is seen with no reconstruction) + the component label + the emit
// closure.
//
// NIL-SAFE BY DESIGN: a nil *Gate reports Enabled()==true and Skip() is a no-op. So a component
// constructed with a nil gate (Features=nil, the default) behaves byte-identically to the
// pre-config code — that is what makes the all-on default a true no-op.
type Gate struct {
	component string         // the label carried in config.skip {component}
	get       func() bool    // reads the live toggle off the shared *HarnessConfig
	emit      events.Emit    // bus closure (nil ⇒ no config.skip)
	skipped   map[string]int // reasons already emitted this run -> count (dedup the skip stream)
}

// NewGate builds a Gate over a live toggle getter, a component label, and the emit closure. The
// getter reads the SHARED config pointer each call so a TUI live-flip is observed with no rebuild.
// Pass a nil getter for an always-on gate (used when a sub-feature has no dedicated toggle yet).
func NewGate(component string, get func() bool, emit events.Emit) *Gate {
	return &Gate{component: component, get: get, emit: emit, skipped: map[string]int{}}
}

// Enabled reports whether the component's toggle is currently ON. Nil-safe (nil gate ⇒ true); a nil
// getter ⇒ true (the always-on default). This is the one call a component makes at its decision point.
func (g *Gate) Enabled() bool {
	if g == nil || g.get == nil {
		return true
	}
	return g.get()
}

// Disabled is the inverse of Enabled (reads better at a short-circuit guard). Nil-safe.
func (g *Gate) Disabled() bool { return !g.Enabled() }

// Skip emits config.skip carrying {component, reason} — the observable trace that a disabled
// component bypassed its decision (toggle = bypass, not delete). Nil-safe (a nil gate / nil emit is a
// no-op). The skip stream is DEDUPED per (reason) so a hot-loop bypass does not flood the bus: the
// first skip of a reason emits; subsequent ones increment the count silently (the count is carried on
// the first event's reuse... — kept simple here: emit-once-per-reason keeps determinism + the bus
// readable, and the panel still shows the component as DISABLED from the live config).
func (g *Gate) Skip(reason string) {
	if g == nil || g.emit == nil {
		return
	}
	if _, seen := g.skipped[reason]; seen {
		g.skipped[reason]++
		return
	}
	g.skipped[reason] = 1
	g.emit(events.ConfigSkip, g.component+" disabled: "+reason, events.D{
		"component": g.component,
		"reason":    reason,
	})
}

// Component returns the gate's component label (used by callers that build a richer skip summary).
func (g *Gate) Component() string {
	if g == nil {
		return ""
	}
	return g.component
}
