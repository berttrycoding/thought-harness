// graph_spine.go is the engine glue for the COMPRESSED GRAPH SPINE persist/rehydrate path
// (cognitive power-cycle, Track 2; proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §4 + §11 Track 2;
// Fork 1 resolved to LIGHT RE-ORIENTATION, §9).
//
// WHAT IT DOES. It is a CONSUMER of the now-live subconscious.Context object (e.episodeContext,
// captured at episode-open in reactive.go; owner = 01 §3.11 / subconscious/context.go) — NOT a
// second owner. On the learned-state flush it projects e.episodeContext's Goal + L1 (the lossy
// gist + branch thought IDs + resolution) into a persisted GraphSpineRecord; on boot, when the
// resume knob is ON, it rehydrates that record into e.priorContext (a Context with ONLY Goal + L1
// populated) so a resumed session can re-ground in "where I was". The heavy full Snapshot is NOT
// persisted (§4/§9: the Gist is the re-grounding material; full-graph rehydration is deferred).
//
// NOTHING CONSUMES e.priorContext YET. Track 3's orientation pass will read it (via PriorContext).
// Track 2's job is persist + rehydrate + the round-trip gate, nothing more.
//
// THE DIVERGENCE CONTRACT (mirrors percept.go). A loaded spine carries a Version + a Substrate
// stamp; the ENGINE refuses to rehydrate a record whose Version or Substrate does not match the
// running engine's — it returns as-if-cold (e.priorContext stays nil), NEVER a best-effort partial
// rehydrate. A missing/corrupt file ⇒ no rehydrate (a fresh cold boot).
//
// DEFAULT OFF ⇒ BYTE-IDENTICAL. SAVING the spine when persistence is on is fine (it writes a file,
// never mutates engine state). RESTORING into e.priorContext is gated by the resume knob
// (e.features.Persist.Resume) + a non-nil Store + persistence-on; default OFF ⇒ e.priorContext stays
// nil, and nothing reads it, so the live path is byte-identical. Mirrors loadResume / loadPerceptLog.
//
// HEADLESS-PURE. No I/O (only the injected Store); no wall clock; no seeded-RNG threading.
package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// snapshotGraphSpine builds the on-disk compressed-spine record. It is a PROJECTION of a Context's Goal
// + L1Snapshot (the lossy gist + ordered thought IDs + resolution) — the heavy full Snapshot is dropped
// (§4/§9).
//
// CAPTURE TIMING (live-claude finding, 2026-06-20): it captures from the LIVE GROWN graph at flush time,
// NOT the episode-OPEN e.episodeContext. e.episodeContext freezes at episode-open when the active branch
// is just the goal-root, so its L1 gist is "" — live-claude validation showed orientation re-grounding on
// "prior focus: (none)" (the gap-2 capture-timing root). Capturing from e.graph here yields a real
// "where I was" gist for re-grounding. Falls back to e.episodeContext when no live graph exists; a nil of
// both yields an empty-but-valid record (a harmless empty re-grounding marker).
func (e *Engine) snapshotGraphSpine() persist.GraphSpineRecord {
	rec := persist.GraphSpineRecord{
		Version:   persist.GraphSpineVersion,
		Substrate: e.backendLabel,
	}
	if e.graph != nil {
		ctx := subconscious.CaptureContext(e.graph, e.backend, e.graph.Goal, nil)
		rec.Goal = ctx.Goal
		rec.BranchID = ctx.L1.BranchID
		rec.Gist = ctx.L1.Gist
		if strings.TrimSpace(rec.Gist) == "" {
			// backend.Summarize can return "" on claude reasoning models (they fill reasoning_content,
			// leave content empty — CLAUDE.md edge), so the gist is empty and orientation re-grounds on
			// "(none)" (live-claude #14 finding; the OFFLINE canned Summarize populates fine, proving the
			// capture is correct). Fall back to a DETERMINISTIC gist from the branch tail so re-grounding
			// carries real "where I was" content even when Summarize is unavailable.
			rec.Gist = spineGistFallback(ctx.Snapshot)
		}
		rec.ThoughtIDs = append([]int(nil), ctx.L1.ThoughtIDs...)
		rec.Resolution = ctx.L1.Resolution
	} else if e.episodeContext != nil {
		rec.Goal = e.episodeContext.Goal
		rec.BranchID = e.episodeContext.L1.BranchID
		rec.Gist = e.episodeContext.L1.Gist
		rec.ThoughtIDs = append([]int(nil), e.episodeContext.L1.ThoughtIDs...)
		rec.Resolution = e.episodeContext.L1.Resolution
	}
	return rec
}

// spineGistFallback derives a deterministic one-line gist from a branch's thoughts — used when
// backend.Summarize returns "" (the claude reasoning-model edge). Returns the last non-empty thought
// text, clipped to a one-line marker; "" only when the branch has no text. No model call, no clock.
func spineGistFallback(thoughts []types.Thought) string {
	for i := len(thoughts) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(thoughts[i].Text); t != "" {
			return runeSlice(t, 100)
		}
	}
	return ""
}

// saveGraphSpine persists the compressed spine alongside the learned-state flush (called from
// curateState beside saveResumeCursor / savePerceptLog). nil store / persistence-off ⇒ no-op (the
// spine is meaningless offline). Saving is always safe — it writes a file, never mutates engine
// state — so it is NOT gated by the Resume knob; only RESTORING is (loadGraphSpine). The record is
// Version + Substrate stamped so the next boot's divergence check has its two keys.
func (e *Engine) saveGraphSpine() {
	if e.cfg.Store == nil || !e.features.Persist.Enabled {
		return
	}
	_ = e.cfg.Store.SaveGraphSpine(e.snapshotGraphSpine())
}

// loadGraphSpine rehydrates the compressed spine into e.priorContext on boot when the Resume knob
// is ON, so a resumed session re-grounds in "where I was" (§4 + §9, light re-orientation). Default
// OFF ⇒ e.priorContext stays nil ⇒ no consumer ⇒ byte-identical. Gated additionally by a non-nil
// store + persistence-on. Runs AFTER loadResume / loadPerceptLog (process + perception state first,
// then the orientation spine). Enforces the DIVERGENCE CONTRACT: a version OR substrate mismatch is
// REFUSED (e.priorContext stays nil) — never a best-effort partial rehydrate. The rehydrated Context
// carries ONLY Goal + L1 (and an empty, non-nil L2 so a later consumer can write to it safely); the
// heavy full Snapshot is not persisted, so it is nil here (consumers re-expand on demand via the IDs).
func (e *Engine) loadGraphSpine() {
	if e.cfg.Store == nil || !e.features.Persist.Enabled || !e.features.Persist.Resume {
		return
	}
	r, err := e.cfg.Store.LoadGraphSpine()
	if err != nil || r == nil {
		return
	}
	// The divergence contract (mirrors loadPerceptLog): a version OR substrate mismatch REFUSES the
	// rehydrate — never a best-effort partial apply. The refused spine is left untouched on disk; this
	// run boots as-if-cold and OVERWRITES it with a fresh, matching record on the next flush.
	if r.Version != persist.GraphSpineVersion || r.Substrate != e.backendLabel {
		return
	}
	e.priorContext = &subconscious.Context{
		Goal: r.Goal,
		L1: subconscious.L1Snapshot{
			BranchID:   r.BranchID,
			Gist:       r.Gist,
			ThoughtIDs: append([]int(nil), r.ThoughtIDs...),
			Resolution: r.Resolution,
		},
		L2: map[string]any{},
	}
}

// PriorContext returns the compressed graph spine rehydrated from a prior power-cycle (Track 2),
// or nil when this is a cold boot / the resume knob was OFF / the persisted spine diverged. It is
// the read accessor the Track-3 orientation pass will consume to re-ground a resumed session; today
// nothing reads it on the live path, so it has no effect on cognition. Read-only.
func (e *Engine) PriorContext() *subconscious.Context { return e.priorContext }
