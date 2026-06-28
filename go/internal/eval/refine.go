package eval

import "sort"

// refine.go GENERALISES the eval object into the UNIFORM PER-REGISTRY REFINE LOOP
// (01-subconscious.md §3.17 + §3.20 — the self-improvement mechanism every
// registry needs). It is the layer that makes "measuring-stick = registry-able
// reference, measurement = instance" apply to ANY registry, not just the convert
// mint gate (the first concrete instance, §3.19).
//
// The shape (§3.17): every registry carries a measuring-stick REFERENCE (its
// mint gate, the absolute "does it belong?" bar) and produces measurement
// INSTANCES of its entries; a per-registry refine SIGNAL (improve / keep / prune)
// is surfaced per entry, computed COMPARATIVELY against that entry's own past
// measurements (§3.20 "vs past similar measurements of the same stick").
//
// LEAF DISCIPLINE: this package must stay below the concrete registries it
// generalises over (it already imports only events/action/resolve — never
// cognition/subconscious). So the registry is reached through the small
// RefinableRegistry INTERFACE declared here; the concrete operators / skills /
// primitive-subagent / tool registries are ADAPTED to it at the engine level,
// never imported here (that would cycle). Everything stays deterministic — ticks
// are passed in, the loop reads no wall clock.

// RefineEntry is one registry entry presented to the refine loop: a stable id
// (the registry key) plus the opaque SUBJECT the registry's mint-gate stick
// knows how to read (a grounded value, a rubric record — the stick's contract).
type RefineEntry struct {
	// ID is the stable per-entry key (e.g. an operator/skill/specialist name);
	// it groups measurements per reference so the comparative baseline is per
	// entry (§3.20), not pooled across the registry.
	ID string
	// Subject is the opaque thing the stick scores. Its concrete type is the
	// stick's contract (the registry adapter and its stick agree on it).
	Subject any
}

// RefinableRegistry is the small port the refine loop needs from ANY registry to
// run the uniform §3.17 loop over it. A concrete registry (operators / skills /
// primitive subagents / tools) is ADAPTED to this at the engine level — the eval
// package never imports those packages (leaf discipline).
type RefinableRegistry interface {
	// Name identifies the registry in the refine report / trace.
	Name() string
	// Stick is this registry's measuring-stick reference — the SAME stick used as
	// its mint gate (reference-eval, §3.19). ok=false means the registry has no
	// attached stick yet (the loop then reports nothing, never panics).
	Stick() (stick MeasuringStick, ok bool)
	// Entries lists the registry's current entries to be measured this pass, in a
	// stable order (the caller owns determinism). An empty slice is fine.
	Entries() []RefineEntry
}

// Verdict is the per-entry refine outcome the loop surfaces (§3.17): the action a
// registry's self-improvement would take for this reference. It is a SIGNAL by
// default — the loop never mutates a registry; a caller decides whether to act on
// Prune (behind its own flag).
type Verdict int

const (
	// Keep: the entry clears its stick's absolute bar and is not regressing.
	Keep Verdict = iota
	// Improve: the entry clears the bar AND its instance-eval trends Up vs its own
	// history — a reference worth reinforcing / raising in standing.
	Improve
	// Prune: the entry FAILS its stick's absolute bar (reference-eval reject) — it
	// no longer belongs. (A Down-trend that still clears the bar stays Keep — a dip
	// is not an eviction; only failing the absolute bar prunes.)
	Prune
)

func (v Verdict) String() string {
	switch v {
	case Improve:
		return "improve"
	case Prune:
		return "prune"
	default:
		return "keep"
	}
}

// EntryReport is the refine result for one entry: the absolute benchmark verdict
// (passes the stick?), the comparative instance-eval signal vs the entry's own
// history, the measurement that produced them, and the derived Verdict.
type EntryReport struct {
	// ID is the entry's stable key.
	ID string
	// Pass is the absolute reference-eval verdict (does it still belong?).
	Pass bool
	// Refine is the comparative instance-eval signal vs this entry's own past
	// measurements (§3.20).
	Refine RefineSignal
	// Verdict is the derived improve / keep / prune action signal.
	Verdict Verdict
	// Measurement is the instance this pass produced (kept for the trace / ledger).
	Measurement Measurement
}

// RefineReport is one registry's whole refine pass: the registry name, the stick
// it measured against, and the per-entry reports in the stable entry order.
type RefineReport struct {
	// Registry is the registry's name.
	Registry string
	// Stick is the name of the measuring stick the pass used.
	Stick string
	// Entries are the per-entry reports, in the registry's Entries() order.
	Entries []EntryReport
}

// RefineLoop is the uniform per-registry self-improvement loop (§3.17). It holds
// the per-(stick,subject) measurement HISTORY so the comparative baseline is per
// reference and accumulates across passes — the durable effort lives in the loop,
// the measurements are records (§2.6). It is generic + deterministic (logical
// ticks passed in) and mutates NO registry: it surfaces a RefineReport (signal
// only). A caller wires the eventual prune/keep action itself, behind its flag.
type RefineLoop struct {
	// history is keyed by stick name + subject id so each reference is measured
	// against its OWN past instances (§3.20), never a pool across entries.
	history map[string][]Measurement
	// epsilon is the dead-band for the comparative signal (noise floor); a |Delta|
	// below it reads Flat, keeping the loop from refining on measurement noise.
	epsilon float64
	// seq is the deterministic logical tick the loop stamps on its measurements
	// (no wall clock); it advances once per measured entry.
	seq int64
}

// NewRefineLoop builds an empty refine loop with the given comparative dead-band
// (epsilon). Pass 0 for a strict comparison.
func NewRefineLoop(epsilon float64) *RefineLoop {
	return &RefineLoop{history: map[string][]Measurement{}, epsilon: epsilon}
}

// SingleRefine is the eval object's ATOMIC self-improvement step (§3.19 + §3.20),
// factored out so EVERY concrete instance — the convert mint gate, the per-
// registry RefineLoop, an ad-hoc caller — runs the SAME composition: benchmark
// the subject against the stick (absolute admit) AND measure it comparatively vs
// the supplied history (the refine signal). The history GROUPING is the caller's
// policy (per-subject, per-registry pool, …); this primitive just composes the
// two modes over whatever history it is handed. It is the shared kernel the
// RefineLoop.Refine path and the convert mint gate both express in terms of, so
// the generalisation SUBSUMES the convert mint gate rather than duplicating it.
//
// Returns: admit (the absolute reference-eval verdict), the comparative refine
// signal, and the measurement produced (which the caller appends to its history).
func SingleRefine(stick MeasuringStick, subjectID string, subject any, tick int64, history []Measurement, epsilon float64) (admit bool, sig RefineSignal, m Measurement) {
	admit, m = MintGate(stick, subjectID, subject, tick)
	sig = Measure(m, history, epsilon)
	return admit, sig, m
}

// histKey groups measurements per reference (per stick, per subject) — the
// §3.20 "same stick" comparison, refined to the same SUBJECT so an entry is
// compared against its own past, not its registry siblings.
func histKey(stick, subjectID string) string { return stick + "\x00" + subjectID }

// Refine runs ONE refine pass over a registry: measure each entry against the
// registry's stick (reference-eval, absolute) AND comparatively against the
// entry's own history (instance-eval), derive a per-entry Verdict, and return the
// whole RefineReport. The loop accumulates the new measurements into its history
// so the NEXT pass's baseline includes them. With no attached stick (ok=false)
// the report is empty — the loop is a no-op (additive, never panics).
//
// IMPORTANT: Refine NEVER mutates the registry. It only produces the signal. The
// caller decides whether to act on a Prune (and does so behind its own flag).
func (l *RefineLoop) Refine(reg RefinableRegistry) RefineReport {
	rep := RefineReport{Registry: reg.Name()}
	stick, ok := reg.Stick()
	if !ok {
		return rep
	}
	rep.Stick = stick.Name
	for _, e := range reg.Entries() {
		l.seq++
		key := histKey(stick.Name, e.ID)
		// the SAME atomic step the convert mint gate runs (benchmark + comparative),
		// here grouped per SUBJECT so each reference is measured against its own past.
		pass, sig, m := SingleRefine(stick, e.ID, e.Subject, l.seq, l.history[key], l.epsilon)
		l.history[key] = append(l.history[key], m)

		v := Keep
		if !pass {
			v = Prune // failed the absolute bar -> no longer belongs (reference-eval reject)
		} else if sig.Direction == Up {
			v = Improve // clears the bar and trending up -> reinforce
		}
		rep.Entries = append(rep.Entries, EntryReport{
			ID:          e.ID,
			Pass:        pass,
			Refine:      sig,
			Verdict:     v,
			Measurement: m,
		})
	}
	return rep
}

// History returns the accumulated measurements for one reference (stick +
// subject), in tick order — the per-reference scorecard (§3.20 standing
// instance-eval seed). Returns nil for an unmeasured reference.
func (l *RefineLoop) History(stick, subjectID string) []Measurement {
	h := l.history[histKey(stick, subjectID)]
	if len(h) == 0 {
		return nil
	}
	out := make([]Measurement, len(h))
	copy(out, h)
	return out
}

// Counts tallies a RefineReport's verdicts (improve / keep / prune) — the
// per-registry summary a self-improvement loop or a TUI panel reads.
func (r RefineReport) Counts() (improve, keep, prune int) {
	for _, e := range r.Entries {
		switch e.Verdict {
		case Improve:
			improve++
		case Prune:
			prune++
		default:
			keep++
		}
	}
	return
}

// Prunable returns the ids of the entries this pass flagged Prune, sorted for a
// deterministic order. This is the SIGNAL a caller would act on (behind its own
// flag) to evict references that no longer belong — the loop itself never does.
func (r RefineReport) Prunable() []string {
	var ids []string
	for _, e := range r.Entries {
		if e.Verdict == Prune {
			ids = append(ids, e.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
