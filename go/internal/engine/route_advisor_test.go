package engine_test

// route_advisor_test.go — the COGNITION-property test for the read-only LANE ROUTER (O-3,
// conscious.activity.route_advisor; docs/internal/notes/2026-06-20-auto-dev-lathe-vs-fleet.md §6/§7 P2).
//
// The QUESTION it answers: does the auto-dev "read-only router" brought INWARD actually do the thinking
// the spec intends — rank the live standing lanes by V(s) under per-lane thresholds + cooldowns, surface a
// deterministic "what's hottest" audit, and DECIDE-BUT-NEVER-DISPATCH (the load-bearing LATHE invariant)?
//
// This is a COGNITION test, not a plumbing test. It does not merely assert the loop ticks: it asserts
//   (1) the router FIRES when ON and is SILENT when OFF (the flag-gated, byte-identical-when-off posture);
//   (2) the verdict is VALUE-ROUTED — the named "top" lane is the highest-value runnable lane (best-first);
//   (3) the anti-thrash policy actually GATES — at least one lane is held back (on-cooldown / below-thresh),
//       reported honestly in the audit (never a silent drop);
//   (4) it NEVER DISPATCHES — with the router ON the awake loop focuses the EXACT SAME branch sequence as
//       with it OFF (same seed, same ticks): the router changed nothing, it only NAMED the pick.
//
// Deterministic: TestBackend test double + cpyrand seed=7 (via newContinuousEngineWithFeatures), no model
// tokens, no clock, no unseeded RNG. The router itself reads only V(s) (Branch.Value) and recorded focus
// ticks, so two identical runs produce an identical event stream.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// awakeRouterFeatures builds the awake-profile feature set the router experiment runs on: the standing
// seed-intent portfolio is planted (so there are live faculty/drive LANES to route over) with the read-only
// router optionally on. Crucially it does NOT enable the faculty scheduler — the router is proven to be a
// pure ADVISOR layered over the plain frontier-argmax loop, so its non-dispatch claim is unambiguous (no
// other arbiter is competing for the credit).
func awakeRouterFeatures(routeAdvisor bool) *config.HarnessConfig {
	c := config.New()
	a := &c.Conscious.Activity
	a.Forest = true
	a.SeedIntents = true
	a.SeedIntentCount = cognition.SeedPortfolioSize() // full portfolio — every faculty becomes a lane
	a.RouteAdvisor = routeAdvisor
	c.Validate()
	return c
}

// activeBranchTrace runs an awake stream for n ticks (NO user input) and returns the active-branch id after
// each tick — the focus trajectory. Two runs that produce the SAME trace focused the same lines in the same
// order; a divergence means something changed focus.
func activeBranchTrace(t *testing.T, feat *config.HarnessConfig, n int) []int {
	t.Helper()
	eng, _ := newContinuousEngineWithFeatures(t, feat)
	trace := make([]int, 0, n)
	for i := 0; i < n; i++ {
		eng.Step()
		trace = append(trace, eng.Graph().ActiveBranch)
	}
	return trace
}

func TestRouteAdvisorRanksRunnableNeverDispatches(t *testing.T) {
	const ticks = 60

	// --- OFF: the router is silent and the channel is empty (byte-identical posture) ---
	offEng, offLog := newContinuousEngineWithFeatures(t, awakeRouterFeatures(false))
	for i := 0; i < ticks; i++ {
		offEng.Step()
	}
	if got := len(offLog.of(events.Route)); got != 0 {
		t.Fatalf("router OFF must emit ZERO conscious.route events, got %d", got)
	}

	// --- ON: the router fires over the live standing lanes ---
	onEng, onLog := newContinuousEngineWithFeatures(t, awakeRouterFeatures(true))
	for i := 0; i < ticks; i++ {
		onEng.Step()
	}
	routes := onLog.of(events.Route)
	if len(routes) == 0 {
		t.Fatalf("router ON must fire conscious.route over the standing lanes — got zero (the scan never ran)")
	}

	// (2) VALUE-ROUTED: on every tick a runnable lane was named, that "top" must be the HIGHEST-value
	// runnable lane in the per-lane breakdown (best-first). And the audit must be non-empty + honest.
	sawRunnableTick := false
	for _, ev := range routes {
		if ev.Summary == "" {
			t.Fatalf("conscious.route carried an empty audit line")
		}
		lanes := laneRows(t, ev)
		topID, hasTop := intData(ev, "top")
		runnable, _ := intData(ev, "runnable")
		if runnable > 0 {
			sawRunnableTick = true
			if !hasTop || topID < 0 {
				t.Fatalf("router named %d runnable lanes but no valid top id (%v)", runnable, ev.Data)
			}
			// the named top must be the highest V(s) among the RUNNABLE lanes.
			var bestVal float64
			bestID := -1
			for _, l := range lanes {
				if l.runnable && (bestID < 0 || l.value > bestVal) {
					bestVal, bestID = l.value, l.id
				}
			}
			if topID != bestID {
				t.Fatalf("router top=%d is NOT the highest-value runnable lane (best=%d, val=%.3f); lanes=%v",
					topID, bestID, bestVal, lanes)
			}
		}
	}
	if !sawRunnableTick {
		t.Fatalf("over %d ticks the router never found a single runnable lane — the threshold/cooldown "+
			"policy is gating EVERYTHING (a stuck advisor is as useless as none)", ticks)
	}

	// (3) ANTI-THRASH actually GATES: across the run at least one lane was held back (on-cooldown or
	// below-thresh) and reported honestly — the policy is a real filter, not a pass-through. A held lane
	// must NEVER be silently dropped from the breakdown (the audit owes a reason).
	heldWithReason := false
	for _, ev := range routes {
		total, _ := intData(ev, "total")
		lanes := laneRows(t, ev)
		if len(lanes) != total {
			t.Fatalf("conscious.route lanes breakdown (%d) != total (%d) — a lane was dropped from the audit",
				len(lanes), total)
		}
		for _, l := range lanes {
			if !l.runnable {
				if l.reason == "" {
					t.Fatalf("a held-back lane (%d) carried no reason — the audit must be honest about WHY", l.id)
				}
				if l.reason == "on-cooldown" || l.reason == "below-thresh" {
					heldWithReason = true
				}
			}
		}
	}
	if !heldWithReason {
		t.Fatalf("the anti-thrash policy never held a single lane back over %d ticks — threshold+cooldown "+
			"is a no-op (not the tunable policy the spec intends)", ticks)
	}

	// (4) NEVER DISPATCHES: the focus trajectory with the router ON must be IDENTICAL to the trajectory
	// with it OFF (same seed, same ticks). The router only NAMES the pick; it must not move focus, seed a
	// goal, or touch the plant. A divergence means the "read-only / never-dispatches" contract is broken.
	offTrace := activeBranchTrace(t, awakeRouterFeatures(false), ticks)
	onTrace := activeBranchTrace(t, awakeRouterFeatures(true), ticks)
	if len(offTrace) != len(onTrace) {
		t.Fatalf("trace length differs ON=%d vs OFF=%d", len(onTrace), len(offTrace))
	}
	for i := range offTrace {
		if offTrace[i] != onTrace[i] {
			t.Fatalf("router CHANGED focus at tick %d: OFF focused branch %d, ON focused branch %d — the "+
				"read-only router DISPATCHED (the LATHE invariant is broken)", i, offTrace[i], onTrace[i])
		}
	}
}

// laneRow is the decoded per-lane breakdown carried in a conscious.route event's "lanes" payload.
type laneRow struct {
	id       int
	value    float64
	runnable bool
	reason   string
}

// laneRows decodes the "lanes" slice off a conscious.route event into typed rows. The event data is built
// in-process (not round-tripped through JSON), so the values are their native Go types.
func laneRows(t *testing.T, ev events.Event) []laneRow {
	t.Helper()
	raw, ok := ev.Data["lanes"]
	if !ok {
		t.Fatalf("conscious.route missing lanes payload: %v", ev.Data)
	}
	list, ok := raw.([]map[string]any)
	if !ok {
		t.Fatalf("conscious.route lanes is %T, want []map[string]any", raw)
	}
	rows := make([]laneRow, 0, len(list))
	for _, m := range list {
		var r laneRow
		if v, ok := m["id"].(int); ok {
			r.id = v
		}
		switch v := m["value"].(type) {
		case float64:
			r.value = v
		case int:
			r.value = float64(v)
		}
		if v, ok := m["runnable"].(bool); ok {
			r.runnable = v
		}
		if v, ok := m["reason"].(string); ok {
			r.reason = v
		}
		rows = append(rows, r)
	}
	return rows
}

// intData reads an int out of an event payload, with ok=false when absent or not an int.
func intData(ev events.Event, key string) (int, bool) {
	v, ok := ev.Data[key]
	if !ok {
		return 0, false
	}
	n, isInt := v.(int)
	return n, isInt
}
