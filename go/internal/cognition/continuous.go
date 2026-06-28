// This file ports continuous.py — the awake-mode additions.
//
// Continuous (awake) mode is the GENERAL regime: self-sustaining perceive·think·act.
// Reactive mode is the special case (drives off, default-mode off, arousal coupled to input).
// These supply the awake-mode additions: Arousal (loop rate + wake/sleep), Drives (endogenous
// goals when none are given — the μ baseline that keeps the stream alive), and the Default-mode
// generator (spontaneous association — a wandered thought can clear threshold and *become* a
// goal).
//
// regulate_arousal/perception_gain are stateless free functions of the arousal level; Drives and
// DefaultMode are structs over the injected emit closure (and Drives' deterministic counter). The
// modular indexing (_n%3 curiosity cycle, <0.2 wander-skip) is kept EXACT for reproducibility — the
// golden oracle compares the resulting port-event stream.
package cognition

import (
	"strconv"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// The curiosity-musing / association string pools that USED to live here have been DELETED: they were
// "manufactured intelligence" (a counter-indexed fixed list voiced as a thought), which violates the
// content-roles-are-the-model principle (feedback-heuristic-control-only / the three-pattern split).
// The awake-mind's idle content (curiosity, default-mode association, drive-goal text) is now AUTHORED
// by the CONTENT layer: the engine injects a Wanderer closure (a backend.Wander call) into Drives and
// DefaultMode. The legitimate OFFLINE pools now live ONLY in backends.TestBackend.Wander (the test
// double, the one place canned content is allowed, rotated by the rng so the goldens stay deterministic
// AND varied). On a model DECLINE the closure returns "" and these generators go DARK — exactly like
// the existing lull/ablation branches — never a canned fallback (the standing guard test asserts this).

// Wanderer authors ONE short first-person idle thought for the awake stream — the CONTENT half of the
// endogenous baseline μ. kind ∈ "curiosity" | "association" | "develop"; hint carries the domain for
// "develop" (else ""). It is the engine's `backend.Wander(kind, hint, ctx, rng)` bound as a closure. ""
// is the model's gap-surface on decline (the generator goes dark, no canned substitute). A nil Wanderer
// (never wired) is ALSO dark — there is no hardcoded fallback in the production path by construction.
type Wanderer func(kind, hint string) string

// pursuitDefault is the Python Drives default pursuit_threshold=0.4 — a frontier branch at or above
// it is worth resuming (the maintenance drive) rather than minting a fresh curiosity goal.
const pursuitDefault = 0.4

// Drives are endogenous goals: unfinished high-value branches first, then curiosity. Supplies μ > 0
// — the positive baseline that keeps the awake stream from decaying to silence. Mirrors the Python
// Drives class (the emit closure + the deterministic _n counter).
type Drives struct {
	emit    events.Emit
	pursuit float64
	n       int      // the deterministic curiosity-cycle counter (Python self._n); NOT rng-driven
	author  Wanderer // the CONTENT author for curiosity/fresh-line text; nil ⇒ DARK (no canned fallback)
}

// NewDrives builds a Drives over the injected emit closure with the Python default pursuit
// threshold (0.4). The CONTENT author (Wanderer) is wired separately via SetAuthor — until then the
// curiosity/fresh-line generators are DARK (no canned fallback in the production path).
func NewDrives(emit events.Emit) *Drives {
	return &Drives{emit: emit, pursuit: pursuitDefault}
}

// NewDrivesWithThreshold builds a Drives with an explicit pursuit threshold (the Python
// pursuit_threshold keyword). Used where the engine tunes the maintenance-drive cutoff. The CONTENT
// author is wired separately via SetAuthor.
func NewDrivesWithThreshold(emit events.Emit, pursuitThreshold float64) *Drives {
	return &Drives{emit: emit, pursuit: pursuitThreshold}
}

// SetAuthor wires the CONTENT author (the engine's backend.Wander closure) used to author curiosity /
// fresh-line text. A nil author leaves the generators DARK — there is NO hardcoded fallback in the
// production path (the standing guard test asserts a nil-author Drives goes dark).
func (d *Drives) SetAuthor(author Wanderer) { d.author = author }

// ProposeGoal returns an endogenous goal for this tick, or nil. Two strategies, in order:
//  1. resume the best unfinished high-value branch (maintenance drive) — emits a "resume" port
//     event and returns nil (the loop will backtrack to it; no new goal node needed);
//  2. curiosity on the deterministic _n%3 cycle — keeps the baseline alive, emits a "curiosity"
//     port event and returns a fresh GENERATED Thought.
//
// Otherwise SATED this tick (returns nil). Mirrors Python Drives.propose_goal. The frontier-best
// argument is accepted to match the Python signature but is unused (the live frontier is read off
// the graph, exactly as Python does); pass the engine's frontier_best for signature parity.
func (d *Drives) ProposeGoal(g *graph.ThoughtGraph, frontierBest float64) *types.Thought {
	_ = frontierBest // Python takes it but reads graph.frontier() directly; kept for parity.
	// 1. resume the best unfinished high-value branch (maintenance drive)
	fr := g.Frontier()
	if len(fr) > 0 && fr[0].Value >= d.pursuit {
		if d.emit != nil {
			d.emit(events.Port,
				"drive: resume high-value b"+itoa(fr[0].ID)+" (v="+fmt2(fr[0].Value)+")",
				events.D{"drive": "resume"})
		}
		return nil // the loop will backtrack to it; no new goal node needed
	}
	// 2. curiosity (deterministic cycle) — keeps the baseline alive. The CYCLE is control (the d.n%3
	// gate is preserved); only the TEXT is now authored by the CONTENT layer. On a dark author (nil /
	// model declined → "") the generator goes DARK this tick (returns nil) — exactly like the SATED
	// branch — never a canned musing.
	d.n++
	if d.n%3 == 0 {
		if d.author == nil {
			return nil // DARK — no CONTENT author wired (no hardcoded fallback in the production path)
		}
		text := d.author("curiosity", "")
		if text == "" {
			return nil // DARK — the model declined; surface the gap, never a canned substitute
		}
		if d.emit != nil {
			d.emit(events.Port, "drive: curiosity goal -> "+truncate(text, 48),
				events.D{"drive": "curiosity"})
		}
		return &types.Thought{ID: -1, Text: text, Source: types.GENERATED, Confidence: 0.5}
	}
	return nil // SATED this tick
}

// FreshGoal returns an endogenous goal to seed a fresh line when the mind moves on (no resume
// bounce). The d.n bump (the deterministic cycle position) is CONTROL and is preserved; the TEXT is
// now authored by the CONTENT layer (the curiosity Wanderer kind). On a dark author (nil / model
// declined → "") it returns nil and the fresh line seeds NOTHING this tick — the same ablation the
// awake-regime-off branch already tolerates — never a canned musing.
func (d *Drives) FreshGoal() *types.Thought {
	d.n++
	if d.author == nil {
		return nil // DARK — no CONTENT author wired (no hardcoded fallback in the production path)
	}
	text := d.author("curiosity", "")
	if text == "" {
		return nil // DARK — the model declined; surface the gap, never a canned substitute
	}
	if d.emit != nil {
		d.emit(events.Port, "drive: new line -> "+truncate(text, 48),
			events.D{"drive": "fresh-line"})
	}
	return &types.Thought{ID: -1, Text: text, Source: types.GENERATED, Confidence: 0.5}
}

// DefaultMode is the spontaneous associative generator: it fires when no task/drive dominates
// (mind-wandering). The lull-skip is rng-driven CONTROL (preserved); the association TEXT is now
// authored by the injected CONTENT author (the Wanderer "association" kind).
type DefaultMode struct {
	emit   events.Emit
	author Wanderer // the CONTENT author for the association text; nil ⇒ DARK (no canned fallback)
}

// NewDefaultMode builds a DefaultMode over the injected emit closure. The CONTENT author (Wanderer) is
// wired separately via SetAuthor — until then Wander goes DARK (no canned fallback).
func NewDefaultMode(emit events.Emit) *DefaultMode {
	return &DefaultMode{emit: emit}
}

// SetAuthor wires the CONTENT author (the engine's backend.Wander closure) used to author the
// association text. A nil author leaves Wander DARK — there is NO hardcoded fallback in the production
// path (the standing guard test asserts a nil-author DefaultMode wanders dark).
func (m *DefaultMode) SetAuthor(author Wanderer) { m.author = author }

// Wander fires a spontaneous association most of the time, returning a one-element Candidate slice;
// on the occasional lull (rng draw < 0.2) it returns an empty slice so arousal can drowse toward
// sleep when idle. The endogenous baseline μ must stay positive to sustain the awake stream
// (durability) — hence "most of the time".
//
// The lull-skip draw (rng.Float64() < 0.2) is CONTROL and is preserved — the rng is still threaded so
// arousal can drowse. The association TEXT is now authored by the CONTENT layer (the Wanderer
// "association" kind) instead of an rng-indexed fixed pool: on a dark author (nil / model declined →
// "") Wander returns an empty slice (DARK — same as the lull), never a canned association. The rng is
// passed through to the author so the test double can rotate its offline pool deterministically.
func (m *DefaultMode) Wander(rng *cpyrand.Random) []types.Candidate {
	// Fire most of the time; the occasional lull lets arousal drowse toward sleep when idle.
	if rng.Float64() < 0.2 {
		return []types.Candidate{}
	}
	if m.author == nil {
		return []types.Candidate{} // DARK — no CONTENT author wired (no hardcoded fallback)
	}
	text := m.author("association", "")
	if text == "" {
		return []types.Candidate{} // DARK — the model declined; surface the gap, never a canned one
	}
	if m.emit != nil {
		m.emit(events.Port, "default-mode: "+truncate(text, 48), events.D{"mode": "wander"})
	}
	domain := "default-mode"
	return []types.Candidate{{
		Text:      text,
		Source:    types.GENERATED,
		Domain:    &domain,
		Relevance: 0.3,
	}}
}

// RegulateArousal advances the arousal hysteresis by one step (the cost of being awake: regulate or
// thrash). Mirrors Python regulate_arousal; the dict-of-dicts transition tables become switches
// over the current level. Salient perception forces AWAKE; a quiescent, drive-less, slow loop steps
// down toward sleep; an active drive or a saturated rate steps up toward wake; otherwise hold.
func RegulateArousal(current types.Arousal, rate float64, driveActive, perceptSalient, quiescent bool) types.Arousal {
	if perceptSalient {
		return types.AWAKE
	}
	if quiescent && !driveActive && rate < 0.5 {
		// step down toward sleep (consolidation dominates)
		switch current {
		case types.AWAKE:
			return types.DROWSY
		case types.DROWSY:
			return types.ASLEEP
		case types.ASLEEP:
			return types.ASLEEP
		}
	}
	if driveActive || rate >= 1.0 {
		// step up toward wake
		switch current {
		case types.ASLEEP:
			return types.DROWSY
		case types.DROWSY:
			return types.AWAKE
		case types.AWAKE:
			return types.AWAKE
		}
	}
	return current
}

// PerceptionGain is the afferent gain for an arousal level: 1.0 AWAKE, 0.5 DROWSY, 0.0 ASLEEP.
// Mirrors Python perception_gain (the 3-way map). An unknown level returns 0.0 (the map would
// KeyError in Python, but the enum is closed so this branch is unreachable in practice).
func PerceptionGain(arousal types.Arousal) float64 {
	switch arousal {
	case types.AWAKE:
		return 1.0
	case types.DROWSY:
		return 0.5
	case types.ASLEEP:
		return 0.0
	}
	return 0.0
}

// -- local helpers --------------------------------------------------------

// truncate returns the first n runes of s with NO ellipsis — the faithful port of Python's
// `text[:n]` slice (code-point based). The musing/association corpus is ASCII so byte==rune; rune
// slicing is the correct general match. When len(s) <= n the whole string is returned.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// fmt2 formats a float to two decimals, matching Python f"{x:.2f}" — strconv.FormatFloat with
// precision 2 uses round-half-to-even, identical to CPython's format(). Used only in the console
// summary string (not a wire-data float), so no round(x,3) obligation applies here.
func fmt2(x float64) string {
	return strconv.FormatFloat(x, 'f', 2, 64)
}
