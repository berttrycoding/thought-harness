package engine

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/router"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// route_advisor.go wires the READ-ONLY LANE ROUTER (O-3) into the awake loop — the auto-dev "read-only
// router" (docs/internal/notes/2026-06-20-auto-dev-lathe-vs-fleet.md §6/§7 P2) brought INWARD and run over the
// harness's own runtime lanes. The §6 differentiator is the design key: the conductor's "what is hottest"
// scan need not be a bash heuristic — it can be the harness's OWN value signal V(s) scoring the highest-
// value runnable lane. Here the lanes are the live STANDING faculty/drive seed roots (the runtime analog
// of dev-side track lanes); the router ranks them by V(s) (Branch.Value) under per-lane thresholds +
// cooldowns and emits the ranked next-runnable + audit line.
//
// THE INVARIANT (LATHE's negative result, §1/§5): it DECIDES but NEVER DISPATCHES. The router is purely
// ADVISORY — it does NOT focus a branch, does NOT seed a goal, does NOT touch any operator/seed/fan-out/
// regulator. The existing fair-share scheduler (scheduleFaculty) / frontier argmax still OWN which line is
// focused; the router only NAMES the value-routed pick alongside the live selection. So the PLANT IS
// UNCHANGED — n / U / K·g / μ / fan-out are all untouched (no new branch, no new excitation) — and the
// durability gate need not re-pass. This is the §7 P2 "the conductor gets a deterministic, auditable
// 'what's hottest' instead of eyeballing" delivered at runtime.
//
// ADDITIVE + FLAG-GATED + DEFAULT-OFF (conscious.activity.route_advisor): with the flag OFF maybeRouteAdvise
// returns immediately, no router runs, no conscious.route fires, and every golden stays byte-identical.
// Awake-only — the reactive loop never calls it. USER lines are out of scope: an unanswered user turn is
// handled by the percept/interrupt path upstream, never routed (the router ranks the ENDOGENOUS standing
// lanes, the same set the fair-share scheduler arbitrates).
//
// Determinism: the only ordering sources are Branch.Value (V(s), deterministic CONTROL) and branch id
// (the router's tie-break); the cooldown reads facultyLastFocus (recorded ticks, no clock, no RNG). Two
// identical engine states produce an identical Ranking and an identical emitted event.

// routeThreshold is the router's per-lane runnability floor — a standing lane below it is reported as
// below-thresh (not worth resuming). It mirrors the Controller's pursuit threshold so the router's
// "runnable" agrees with the loop's own "worth pursuing" sense (the same V(s) gate the frontier-resume
// uses), keeping the advisory consistent with the live arbitration it sits beside.
func (e *Engine) routeThreshold() float64 {
	if e.controller != nil {
		return e.controller.PursuitThreshold()
	}
	return 0.4 // the critic-config default, defensive when no controller is wired
}

// routeCooldown is the router's default per-lane cooldown in ticks — the anti-thrash spacing (§7 P2,
// "maps cleanly onto our λ̄=μ/(1−n) cooldown intuition"). A lane that held focus within this many ticks is
// reported as on-cooldown so the audit explains why a hot lane is being rested. Kept modest (a few ticks)
// so a genuinely dominant lane still surfaces once the window passes.
const routeCooldown = 3

// maybeRouteAdvise runs the read-only lane router over the live standing faculty/drive lanes and emits
// conscious.route with the value-routed ranking + audit line. ADVISORY ONLY — it never changes focus.
// No-op unless conscious.activity.route_advisor is ON and there are standing lanes to rank.
func (e *Engine) maybeRouteAdvise(tick int) {
	if e.features == nil || !e.features.Conscious.Activity.RouteAdvisor {
		return
	}
	if e.graph == nil || len(e.branchFaculty) == 0 {
		return
	}

	lanes := e.routeLanes()
	if len(lanes) == 0 {
		return // every standing root is DEAD/MERGED — nothing live to route over this tick
	}

	rk := router.Rank(lanes, tick, router.Policy{Threshold: e.routeThreshold(), Cooldown: routeCooldown})

	// Build the per-lane wire breakdown in rank order (deterministic — Rank already ordered All).
	wireLanes := make([]map[string]any, 0, len(rk.All))
	for _, r := range rk.All {
		wireLanes = append(wireLanes, map[string]any{
			"id":       r.Lane.ID,
			"label":    r.Lane.Label,
			"value":    round2(r.Lane.Value),
			"runnable": r.Runnable,
			"reason":   r.Reason,
		})
	}

	topLabel := "none"
	topID := -1
	if id, ok := rk.Top(); ok {
		topID = id
		for _, r := range rk.All {
			if r.Lane.ID == id {
				topLabel = laneLabel(r.Lane)
				break
			}
		}
	}

	e.bus.Emit(events.Route, rk.Audit, events.D{
		"now":      tick,
		"runnable": len(rk.Next),
		"total":    len(rk.All),
		"next":     topLabel, // the would-be pick's label — ADVISORY (focus is owned by the scheduler/argmax)
		"next_id":  topID,
		"top":      topID, // alias: the highest-value runnable lane id; -1 ⇒ nothing runnable
		"audit":    rk.Audit,
		"lanes":    wireLanes,
		"advisory": true, // never-dispatches contract, made explicit on the wire
	})
}

// routeLanes builds the router's candidate lanes from the LIVE standing faculty/drive roots: one lane per
// faculty (the highest-value live branch in that faculty, mirroring the scheduler's per-faculty pick), the
// lane value = that branch's V(s) (Branch.Value), and LastFired = the faculty's last-focus tick (-1 = never
// focused, never on cooldown). A faculty whose every branch is DEAD/MERGED contributes no lane. The result
// is sorted by faculty enum order then branch id so the lane set itself is deterministic before Rank.
func (e *Engine) routeLanes() []router.Lane {
	type fbest struct {
		bid   int
		value float64
	}
	best := map[int]fbest{} // faculty-int -> its best live branch

	bids := make([]int, 0, len(e.branchFaculty))
	for bid := range e.branchFaculty {
		bids = append(bids, bid)
	}
	sort.Ints(bids)
	for _, bid := range bids {
		b, ok := e.graph.Branches[bid]
		if !ok || b.Status == types.DEAD || b.Status == types.MERGED {
			continue
		}
		fac := int(e.branchFaculty[bid])
		cur, seen := best[fac]
		if !seen || b.Value > cur.value || (b.Value == cur.value && bid > cur.bid) {
			best[fac] = fbest{bid: bid, value: b.Value}
		}
	}

	facs := make([]int, 0, len(best))
	for fac := range best {
		facs = append(facs, fac)
	}
	sort.Ints(facs) // canonical faculty enum order -> deterministic lane order

	lanes := make([]router.Lane, 0, len(facs))
	for _, fac := range facs {
		fb := best[fac]
		faculty := cognition.SeedFaculty(fac)
		last := -1
		if t, ok := e.facultyLastFocus[faculty]; ok {
			last = t
		}
		lanes = append(lanes, router.Lane{
			ID:        fb.bid,
			Label:     faculty.String(), // the faculty name is the lane's stable, human label
			Value:     fb.value,
			LastFired: last,
		})
	}
	return lanes
}

// laneLabel returns the router lane's display label (the human tag carried in Lane.Label).
func laneLabel(l router.Lane) string { return l.Label }
