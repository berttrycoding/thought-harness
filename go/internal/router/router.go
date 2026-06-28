// Package router is the READ-ONLY lane router — a pure ranking policy over candidate lanes.
//
// It is the runtime instantiation of the auto-dev "read-only router" (docs/internal/2026-06-20-auto-dev-
// lathe-vs-fleet.md §6/§7 P2): a pure function over lane state that returns a ranked "next-runnable" list
// plus an audit line, applying PER-LANE THRESHOLDS + COOLDOWNS as a tunable anti-thrash policy, and is
// optionally VALUE-ROUTED. The §6 differentiator made this concrete: the conductor's "what's hottest"
// scan need not be a bash heuristic — it can be the harness's OWN value signal V(s) scoring "what is the
// highest-value runnable lane," dogfooding the architecture. This package is that scan, reused at runtime
// over the awake loop's standing faculty/drive lanes (the runtime analog of dev-side lanes).
//
// THE INVARIANT (LATHE's negative result, §1/§5): the router DECIDES but NEVER DISPATCHES. It returns a
// Ranking; the caller (the conductor / the awake loop) owns whether to act on it. Selecting writers
// concentrates risk faster than it amortizes judgment, so the authority boundary holds: read-only,
// suggest-only.
//
// PURITY + DETERMINISM: no RNG, no clock, no I/O, no engine import. The two ordering sources are the
// lane value (descending) and, on ties, the lane id (ascending). Two identical inputs always rank
// identically — so a caller that wires this in can stay byte-identical when it ignores the ranking, and
// the ranking itself is reproducible. The package is a Tier-1 leaf: it imports nothing from the engine.
//
// THRESHOLD + COOLDOWN, the anti-thrash policy (§7 P2, "maps cleanly onto our λ̄=μ/(1−n) cooldown
// intuition"): a lane is RUNNABLE iff (a) its value clears the per-lane (or default) threshold AND (b) it
// is off cooldown — it has not fired within Cooldown ticks of Now. A lane that fails either gate is
// reported with a Reason (below-threshold / on-cooldown) so the audit line is honest about WHY a hot lane
// was held back, never silent.
package router

import (
	"fmt"
	"sort"
	"strings"
)

// Lane is one candidate the router ranks. It is the substrate-agnostic shape: an id, a human label, a
// value (V(s) when value-routed; any monotone "hotness" otherwise), the tick it last fired (-1 = never),
// and an OPTIONAL per-lane threshold/cooldown override (0 ⇒ use the policy default). Cooldown is in
// ticks; a lane with LastFired == -1 is never on cooldown.
type Lane struct {
	ID        int
	Label     string
	Value     float64
	LastFired int // tick the lane last fired; -1 = never fired

	// Per-lane overrides. Zero means "use the Policy default". A negative override is clamped to the
	// default by the caller's construction; the router treats < 0 as "use default" defensively.
	Threshold float64 // a lane below this value is not runnable (0 ⇒ Policy.Threshold)
	Cooldown  int     // ticks a lane must wait after firing (0 ⇒ Policy.Cooldown)
}

// Policy holds the default per-lane gates. Both default to permissive (0): with the zero Policy every
// lane is runnable and the ranking is a pure value-descending order — the degenerate "no anti-thrash"
// case. Tighten Threshold to gate weak lanes; tighten Cooldown to space out a lane's re-firing.
type Policy struct {
	Threshold float64 // default runnability floor (a lane below it is held back)
	Cooldown  int     // default ticks between a lane's re-firings
}

// holdReason names WHY a lane was held back (empty ⇒ runnable). Reported per-lane so the audit is honest.
type holdReason string

const (
	holdRunnable  holdReason = ""             // clears both gates
	holdThreshold holdReason = "below-thresh" // value < effective threshold
	holdCooldown  holdReason = "on-cooldown"  // fired within effective cooldown of Now
)

// Ranked is one lane's place in the Ranking: its lane, its computed runnability, and the reason it was
// held back (empty ⇒ runnable). Carried in value-descending, id-ascending order.
type Ranked struct {
	Lane     Lane
	Runnable bool
	Reason   string // "" when runnable; "below-thresh" / "on-cooldown" otherwise
}

// Ranking is the router's read-only verdict: every lane ranked (best first), the runnable subset's ids
// in rank order (the "next-runnable" the conductor would consider), and a one-line audit string. Next is
// nil when no lane is runnable this tick (every lane held back) — the honest "nothing to run" signal.
type Ranking struct {
	All   []Ranked // every candidate, value-desc / id-asc, with its runnability + reason
	Next  []int    // runnable lane ids in rank order (best-first); nil ⇒ nothing runnable
	Audit string   // a compact, deterministic audit line for the trace/event
}

// Top returns the single highest-value runnable lane id and true, or (-1, false) when nothing is
// runnable. This is the "next" the conductor would pick — but the router only NAMES it; the caller acts.
func (r Ranking) Top() (int, bool) {
	if len(r.Next) == 0 {
		return -1, false
	}
	return r.Next[0], true
}

// effThreshold / effCooldown resolve a lane's effective gate: the per-lane override when set (> 0), else
// the Policy default. A non-positive override means "use the default" (so a zero-valued Lane field is the
// natural "inherit" case).
func (p Policy) effThreshold(l Lane) float64 {
	if l.Threshold > 0 {
		return l.Threshold
	}
	return p.Threshold
}

func (p Policy) effCooldown(l Lane) int {
	if l.Cooldown > 0 {
		return l.Cooldown
	}
	return p.Cooldown
}

// Rank is the pure ranking function: given the candidate lanes, the current tick (now), and the policy,
// it ranks every lane value-descending (id-ascending on ties), computes each lane's runnability under the
// threshold + cooldown gates, and assembles the next-runnable list + audit line. It NEVER dispatches and
// NEVER mutates its inputs (lanes is copied before sorting). Deterministic: identical inputs ⇒ identical
// Ranking.
func Rank(lanes []Lane, now int, p Policy) Ranking {
	// Copy before sorting — Rank must not mutate the caller's slice (it is read-only over live state).
	sorted := make([]Lane, len(lanes))
	copy(sorted, lanes)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Value != sorted[j].Value {
			return sorted[i].Value > sorted[j].Value // hottest first
		}
		return sorted[i].ID < sorted[j].ID // deterministic tie-break
	})

	all := make([]Ranked, 0, len(sorted))
	var next []int
	for _, l := range sorted {
		reason := runnability(l, now, p)
		runnable := reason == holdRunnable
		all = append(all, Ranked{Lane: l, Runnable: runnable, Reason: string(reason)})
		if runnable {
			next = append(next, l.ID)
		}
	}

	return Ranking{All: all, Next: next, Audit: auditLine(all, now, p)}
}

// runnability gates one lane: threshold first (a structurally weak lane is not worth running at all),
// then cooldown (a hot lane that JUST fired is held back to avoid thrash). The order matters only for the
// reported reason — both must pass for runnable.
func runnability(l Lane, now int, p Policy) holdReason {
	if l.Value < p.effThreshold(l) {
		return holdThreshold
	}
	cd := p.effCooldown(l)
	if cd > 0 && l.LastFired >= 0 && now-l.LastFired < cd {
		return holdCooldown
	}
	return holdRunnable
}

// auditLine renders the deterministic one-line audit: how many lanes are runnable, the top runnable lane
// (or "none"), and a compact per-lane breakdown (best-first) with each lane's value + state. This is what
// rides the trace/event so the "what's hottest" scan is legible — the conductor's eyeballing, replaced by
// an auditable line.
func auditLine(all []Ranked, now int, p Policy) string {
	runnable := 0
	for _, r := range all {
		if r.Runnable {
			runnable++
		}
	}
	top := "none"
	for _, r := range all {
		if r.Runnable {
			top = laneTag(r.Lane)
			break
		}
	}
	var parts []string
	for _, r := range all {
		state := "RUN"
		if !r.Runnable {
			state = strings.ToUpper(strings.ReplaceAll(r.Reason, "-", ""))
		}
		parts = append(parts, fmt.Sprintf("%s=%.2f[%s]", laneTag(r.Lane), r.Lane.Value, state))
	}
	return fmt.Sprintf("route@%d: %d/%d runnable, next=%s | %s",
		now, runnable, len(all), top, strings.Join(parts, " "))
}

// laneTag is a stable lane identifier for the audit: the label when present, else the id. Kept short so
// the audit line stays compact.
func laneTag(l Lane) string {
	if l.Label != "" {
		return l.Label
	}
	return fmt.Sprintf("lane%d", l.ID)
}
