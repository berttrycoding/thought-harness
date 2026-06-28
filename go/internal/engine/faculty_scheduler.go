package engine

import (
	"sort"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// faculty_scheduler.go is the FLAT FAIR-SHARE faculty attention scheduler — the de-risking experiment
// for the seed-intent hierarchy redesign (docs/internal/notes/2026-06-19-seed-intent-hierarchy-redesign.md
// §0/§7, audit §3). It answers ONE question: is the awake seed-starvation an arbitration bug fixable
// with a flat fair-share scheduler (NO hierarchy)?
//
// The measured starvation (confirmed): with the flat-19 seed portfolio in the live awake loop, pure
// frontier-argmax resume keeps picking the highest-value standing line — so only ~3 of 5 faculties ever
// get re-focused (Perceptual and Mnemonic starve after seeding). A flat set has no arbitration
// structure; nothing reserves a turn per faculty.
//
// The fix (this file): when conscious.activity.faculty_scheduler is ON, the awake loop's "resume a
// better sibling" selection (continuous.go, the exhausted-line branch) picks the standing faculty/drive
// line(s) by LEAST-RECENTLY-FOCUSED fair-share among the seeded faculty roots — round-robin is the W=1
// degenerate case — so every faculty gets a turn. W (attention_width) is the number of faculties kept
// "hot": W=1 = serial (today's engine); W>1 selects the top-W least-recently-focused faculties (the
// scalability seam — true concurrent EXECUTION is not yet wired, but the SELECTION already honours W).
//
// ADDITIVE + FLAG-GATED + DEFAULT-OFF: with the flag OFF nothing in this file runs, the awake loop's
// existing argmax path is untouched, no conscious.attention event fires, and every golden stays
// byte-identical. USER lines still preempt — the scheduler is consulted only on the endogenous
// (non-user) "what to think about next" path, never over an unresolved user turn (the μ-floor / userLine
// priority holds upstream in continuous.go and in maybeReachOut).
//
// Determinism: the only ordering sources are the branch-id sort (stable) and the recorded
// facultyLastFocus ticks (no clock, no RNG). Two faculties tied on last-focus tick break by the canonical
// faculty enum order, then by branch id — fully reproducible.

// facultySchedulerOn reports whether the fair-share faculty attention scheduler is enabled (the opt-in
// knob, default OFF). Only meaningful in the awake/continuous loop.
func (e *Engine) facultySchedulerOn() bool {
	return e.features != nil && e.features.Conscious.Activity.FacultyScheduler
}

// attentionWidth returns W — the number of faculty branches the scheduler keeps "hot" concurrently —
// clamped to [1, config.WMax]. W=1 is serial (the round-robin degenerate case). Validate already clamps
// the stored value, but this re-clamps defensively so a directly-constructed config is always sane.
func (e *Engine) attentionWidth() int {
	w := 1
	if e.features != nil {
		w = e.features.Conscious.Activity.AttentionWidth
	}
	if w < 1 {
		w = 1
	}
	if w > config.WMax {
		w = config.WMax
	}
	return w
}

// scheduleFaculty is the fair-share arbiter. Among the LIVE seeded faculty/drive roots (the branches
// tagged in branchFaculty that are still ACTIVE/STASHED in the forest), it groups by faculty, picks the
// W least-recently-focused faculties, then within the single winning faculty picks the highest-value
// resumable branch, focuses it, records the focus tick, and emits conscious.attention. It returns the
// chosen branch id and true; (-1, false) when there is nothing to schedule (no seeded roots live), so
// the caller falls through to its existing endogenous-baseline path.
//
// W>1 widens the SELECTION (the top-W faculties are eligible this tick) even though true concurrent
// EXECUTION is not yet wired — the serial engine then expands the single best line among the hot set, the
// scalability seam the redesign §13 needs once parallel execution exists. The other (W-1) hot faculties
// are recorded in the event's "candidates" so the seam is observable.
func (e *Engine) scheduleFaculty(tick int) (int, bool) {
	if !e.facultySchedulerOn() || len(e.branchFaculty) == 0 || e.graph == nil || e.mcp == nil {
		return -1, false
	}
	// USER PRECEDENCE (redesign R8 / the μ-floor priority): the scheduler is the ENDOGENOUS "what next"
	// arbiter — it must never take focus from a waiting user. An unresolved user turn preempts every
	// standing faculty line, so the scheduler stands down and the loop handles the user line on its
	// reactive/interrupt path. (Defence in depth: the percept/interrupt path upstream already focuses the
	// user line; this guard guarantees the scheduler does not steal it back on the exhausted-resume path.)
	if e.graph.UserWaiting() {
		return -1, false
	}

	// Collect the live seeded roots grouped by faculty. A root is live iff its branch is ACTIVE or
	// STASHED (a pruned/DEAD branch is skipped — scheduling a dead line would wedge the loop, the same
	// guard UserWaiting uses). Within a faculty keep the highest-value (then highest-id) resumable branch.
	type fbranch struct {
		bid   int
		value float64
	}
	best := map[cognition.SeedFaculty]fbranch{}
	bids := make([]int, 0, len(e.branchFaculty))
	for bid := range e.branchFaculty {
		bids = append(bids, bid)
	}
	sort.Ints(bids) // stable order before any argmax — determinism
	for _, bid := range bids {
		b, ok := e.graph.Branches[bid]
		if !ok || b.Status == types.DEAD || b.Status == types.MERGED {
			continue
		}
		fac := e.branchFaculty[bid]
		cur, seen := best[fac]
		if !seen || b.Value > cur.value || (b.Value == cur.value && bid > cur.bid) {
			best[fac] = fbranch{bid: bid, value: b.Value}
		}
	}
	if len(best) == 0 {
		return -1, false
	}

	// Order the candidate faculties by LEAST-RECENTLY-FOCUSED (the fair-share key): a faculty never
	// focused (no facultyLastFocus entry) is most-starved (treated as tick -1). Ties break by the
	// canonical faculty enum order, then branch id — deterministic.
	facs := make([]cognition.SeedFaculty, 0, len(best))
	for fac := range best {
		facs = append(facs, fac)
	}
	lastFocus := func(fac cognition.SeedFaculty) int {
		if t, ok := e.facultyLastFocus[fac]; ok {
			return t
		}
		return -1 // never focused — maximally starved
	}
	sort.SliceStable(facs, func(i, j int) bool {
		li, lj := lastFocus(facs[i]), lastFocus(facs[j])
		if li != lj {
			return li < lj // earlier last-focus = more starved = first
		}
		if facs[i] != facs[j] {
			return facs[i] < facs[j] // canonical faculty enum order
		}
		return best[facs[i]].bid < best[facs[j]].bid
	})

	configW := e.attentionWidth() // the POLICY width (clamped to [1,WMax])
	hotN := configW               // effective hot count: cannot exceed the number of candidate faculties
	if hotN > len(facs) {
		hotN = len(facs)
	}
	hot := facs[:hotN]         // the top-W least-recently-focused faculties (the "hot" set, ≤ configW)
	winner := hot[0]           // serial engine expands the most-starved faculty's line this tick
	chosen := best[winner].bid // its highest-value resumable branch

	e.mcp.Tick = tick
	e.mcp.Focus(chosen)
	e.facultyLastFocus[winner] = tick

	// Observability: which faculty got focus, the branch, the policy width W, the effective hot count, the
	// prior last-focus tick, and the other hot faculties (the W>1 selection seam). The seed-intent name
	// rides off the branch reason.
	name := ""
	if b, ok := e.graph.Branches[chosen]; ok && b.Reason != nil {
		name = *b.Reason
	}
	candidates := make([]string, 0, len(hot))
	for _, f := range hot {
		candidates = append(candidates, f.String())
	}
	e.bus.Emit(events.Attention,
		"faculty-schedule ["+winner.String()+"] branch "+itoa(chosen)+" (W="+itoa(configW)+", hot="+itoa(hotN)+")",
		events.D{
			"faculty":         winner.String(),
			"branch":          chosen,
			"name":            name,
			"width":           configW, // the configured policy width W
			"hot":             hotN,    // the effective hot-faculty count this tick (≤ W, ≤ #candidates)
			"last_focus_tick": lastFocus(winner),
			"candidates":      candidates,
		})
	return chosen, true
}
