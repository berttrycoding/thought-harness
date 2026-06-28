// Package keyframe is the loop-closure / recurrence keyframe DB (Track F, F-M7 — "the HINGE").
//
// SLAM analogy (2026-06-20-slam-self-state-estimation.md §3b.4 point 3, gap G3): a keyframe is a
// searchable DESCRIPTOR of a past state; the recurrence index is the loop-closure database. "loop
// closure / convertibility / anti-rumination are IMPOSSIBLE without it" — without a persisted
// recurrence DB, the harness re-thinks the same line every run and never recognises "I already
// explored this thought" (the un-persisted-recurrence gap G3, memory `w5-efficiency-blocked`).
//
// This package owns the PURE, deterministic recurrence machinery (Pattern-A CONTROL — NO model call):
//   - Descriptor: a stable content fingerprint of the active thought-line (the FNV hash of the
//     normalised tip text, so a re-entered line maps to the same key — never the wall clock, never
//     unseeded randomness).
//   - Index: an in-memory recurrence count per descriptor, BI-TEMPORAL (FirstSeenTick / LastSeenTick)
//     and SUBSTRATE-TAGGED (which thinking substrate authored the keyframe), so a frontier-minted
//     recurrence stays distinguishable from a local-minted one (the re-localization hygiene the rest
//     of persist already enforces).
//   - Observe: fold the current state in; report a LOOP CLOSURE when the descriptor is a re-entry
//     (seen on an EARLIER tick, this run or restored from a prior run) — the anti-rumination signal.
//
// Persistence (the cross-run half of F-M7) is the engine's job: it Seeds the DB from the store at load
// and Exports it back at flush, so the count accumulates across runs. This package owns no I/O — it is
// headless-pure, deterministic, and never imports backends/persist (a Tier-1 leaf).
package keyframe

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// MinLineGapDefault is the floor on how far apart (in ticks) two observations of the same descriptor
// must be before the second counts as a LOOP CLOSURE. A within-the-same-tick or immediately-adjacent
// re-observation is the engine re-reading its own tip, not a genuine re-entry — so it is folded into
// the count but does not fire a closure. The default keeps consecutive ticks of one developing line
// from spuriously reading as rumination.
const MinLineGapDefault = 2

// Keyframe is one recorded state descriptor in the recurrence DB. Bi-temporal + substrate-tagged.
type Keyframe struct {
	// Descriptor is the stable content fingerprint of the thought-line (the recurrence key).
	Descriptor string
	// Gist is a short human-readable label for the line (the tip text, clipped) — observability only.
	Gist string
	// Count is how many times this descriptor has been observed across all runs that share the DB.
	Count int
	// FirstSeenTick / LastSeenTick are the bi-temporal validity bounds (the seeded bus tick).
	FirstSeenTick int
	LastSeenTick  int
	// Closures is how many of the observations were LOOP CLOSURES (a genuine re-entry) — the
	// anti-rumination counter. A first sighting is not a closure.
	Closures int
	// Substrate is the thinking-substrate provenance tag (the backend DisplayName, e.g. "claude:sonnet",
	// "test"), stamped at first sight. A frontier-minted keyframe stays distinguishable from a local one.
	Substrate string
}

// Closure is the report Observe returns when the active line re-enters a known descriptor.
type Closure struct {
	Descriptor string
	Gist       string
	// Count is the new total observation count for this descriptor (>= 2 on a closure).
	Count int
	// Closures is the new loop-closure count for this descriptor (>= 1 on a closure).
	Closures int
	// FirstSeenTick is the tick the descriptor was first recorded (the loop-back point).
	FirstSeenTick int
	// GapTicks is LastSeenTick - FirstSeenTick at the moment of closure (how long the loop was).
	GapTicks int
	// CrossRun is true when FirstSeenTick was restored from a prior run (the descriptor pre-dated this
	// engine's seed) — the durable, cross-session loop closure F-M7 unlocks.
	CrossRun bool
}

// DB is the recurrence keyframe database: a descriptor -> Keyframe index. NOT concurrency-safe by
// itself (the engine observes it on its single loop tick); the engine owns the lifecycle. It is a
// SPARSE map keyed on the descriptor — never a dense matrix — so it scales with distinct lines, not
// with thoughts squared.
type DB struct {
	frames    map[string]*Keyframe
	minGap    int
	seedTick  int // ticks <= seedTick were restored from a prior run (cross-run closures)
	seededLen int // how many descriptors were restored at seed (observability)
}

// New builds an empty recurrence DB. minGap <= 0 uses MinLineGapDefault.
func New(minGap int) *DB {
	if minGap <= 0 {
		minGap = MinLineGapDefault
	}
	return &DB{frames: map[string]*Keyframe{}, minGap: minGap}
}

// Descriptor computes the stable content fingerprint of a thought-line tip. It normalises the text
// (lowercase, collapse whitespace, strip punctuation) so a re-voiced or re-entered line that says the
// same thing maps to the SAME descriptor — the recurrence key. Deterministic (FNV-1a over the
// normalised text); never the wall clock, never RNG. Empty/blank text yields "" (no descriptor).
func Descriptor(tip string) string {
	norm := normalize(tip)
	if norm == "" {
		return ""
	}
	h := fnv.New64a()
	h.Write([]byte(norm))
	return strconv.FormatUint(h.Sum64(), 16)
}

// Seed restores keyframes from a prior run (the cross-session half: the engine loads these from the
// store before tick 1). The restored frames carry their prior FirstSeenTick, so a re-entry this run
// is a CROSS-RUN closure. seedTick marks the boundary: any descriptor whose FirstSeenTick <= seedTick
// pre-dates this engine. Called once before the first Observe; later Seeds merge (keeping the earliest
// FirstSeenTick + summing counts).
func (d *DB) Seed(frames []Keyframe, seedTick int) {
	if seedTick > d.seedTick {
		d.seedTick = seedTick
	}
	for i := range frames {
		f := frames[i]
		if f.Descriptor == "" {
			continue
		}
		if cur, ok := d.frames[f.Descriptor]; ok {
			cur.Count += f.Count
			cur.Closures += f.Closures
			if f.FirstSeenTick < cur.FirstSeenTick {
				cur.FirstSeenTick = f.FirstSeenTick
			}
			if f.LastSeenTick > cur.LastSeenTick {
				cur.LastSeenTick = f.LastSeenTick
			}
			continue
		}
		cp := f
		d.frames[f.Descriptor] = &cp
		d.seededLen++
	}
}

// Observe folds the active thought-line into the DB at the given tick and reports a loop CLOSURE when
// the descriptor is a genuine re-entry — seen on an EARLIER tick (this run or a prior one) at least
// minGap ticks ago. A first sighting records the keyframe and returns (nil, false). A re-observation
// within minGap (the engine re-reading its own developing tip) is folded into the count but is NOT a
// closure. Pure + deterministic: the same (tip, tick, substrate, DB state) always yields the same
// result. tip is the line's tip text; substrate is the thinking-substrate provenance tag.
func (d *DB) Observe(tip string, tick int, substrate string) (*Closure, bool) {
	desc := Descriptor(tip)
	if desc == "" {
		return nil, false
	}
	gist := clip(strings.TrimSpace(tip), 60)
	cur, ok := d.frames[desc]
	if !ok {
		// first sighting: record the keyframe (FirstSeen == LastSeen == this tick).
		d.frames[desc] = &Keyframe{
			Descriptor:    desc,
			Gist:          gist,
			Count:         1,
			FirstSeenTick: tick,
			LastSeenTick:  tick,
			Substrate:     substrate,
		}
		return nil, false
	}

	// a known descriptor: this is a re-entry candidate.
	crossRun := cur.FirstSeenTick <= d.seedTick && d.seedTick > 0
	gap := tick - cur.FirstSeenTick
	d.count(cur, tick)
	// A re-entry of a SEEDED descriptor (one explored in a PRIOR run) is ALWAYS a loop closure — it was
	// already thought through last session, so the within-run tick gap is irrelevant (the new run's tick
	// counter restarts, so gap can be 0 or negative). For a SAME-RUN re-entry the minGap guard still
	// applies: a re-read within minGap ticks of the first sighting is the engine developing one line, not
	// a genuine re-entry, so it is folded into the count without firing a closure.
	if !crossRun && gap < d.minGap {
		return nil, false
	}
	cur.Closures++
	return &Closure{
		Descriptor:    desc,
		Gist:          cur.Gist,
		Count:         cur.Count,
		Closures:      cur.Closures,
		FirstSeenTick: cur.FirstSeenTick,
		GapTicks:      cur.LastSeenTick - cur.FirstSeenTick,
		CrossRun:      crossRun,
	}, true
}

// count folds one more observation of an existing keyframe (advances Count + LastSeenTick).
func (d *DB) count(f *Keyframe, tick int) {
	f.Count++
	if tick > f.LastSeenTick {
		f.LastSeenTick = tick
	}
}

// Export returns the live keyframes in deterministic order (descriptor-ascending) for the engine to
// persist. A copy — the caller cannot mutate the DB through it.
func (d *DB) Export() []Keyframe {
	out := make([]Keyframe, 0, len(d.frames))
	for _, f := range d.frames {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Descriptor < out[j].Descriptor })
	return out
}

// Len reports the number of distinct descriptors recorded.
func (d *DB) Len() int { return len(d.frames) }

// SeededLen reports how many descriptors were restored from a prior run at Seed (observability).
func (d *DB) SeededLen() int { return d.seededLen }

// normalize lowercases, strips punctuation, and collapses whitespace so two re-voicings of the same
// line hash to the same descriptor. Deterministic, allocation-light.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true // leading-space trim
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// clip truncates s to n runes (a short gist label for observability).
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
