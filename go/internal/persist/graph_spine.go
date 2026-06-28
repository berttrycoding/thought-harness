package persist

import (
	"encoding/json"
	"os"
)

// graph_spine.go persists the COMPRESSED GRAPH SPINE — the lossy L1 view of the active
// branch a powered-down cognition was on (its Goal + the branch's gist + ordered thought
// IDs + resolution) — so a resumed session re-grounds in "where I was" without carrying the
// heavy full thought graph (proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §4 + §11
// Track 2; Fork 1 resolved to LIGHT RE-ORIENTATION, §9).
//
// WHY the COMPRESSED spine only (not the full Snapshot []types.Thought): §4/§9 — light
// re-orientation re-grounds a resumed session in the prior LINE via the lossy gist, not a
// byte-identical mid-thought continuation. The Gist is the re-grounding material; the heavy
// full-graph rehydration is deferred. This is a PERSISTENCE PROJECTION of subconscious.Context's
// L1 (its owner is 01 §3.11 / subconscious/context.go), NOT a second Context type — Track 2
// EXTENDS the live object with a save/restore path, it does not re-own it.
//
// WHY a separate file (mirrors resume.go / percept_log.go): this is PROCESS/orientation state
// ("where I was"), distinct from the learned-state Snapshot ("what I learned"). It lives in its
// own graph_spine.json and is NOT part of Load() or the curator's working set.
//
// The DIVERGENCE CONTRACT (mirrors percept_log.go): the record carries a Version + a Substrate
// stamp; on load the ENGINE refuses to rehydrate a record whose Version or Substrate does not
// match the running engine's (it falls back to as-if-cold) — NEVER a best-effort partial
// rehydrate. A corrupt/missing file ⇒ (nil, nil) cold start. This file is I/O-only; the engine
// owns the divergence check (engine/graph_spine.go), exactly as it owns the resume RNG-width and
// percept-log divergence checks.

const fileGraphSpine = "graph_spine.json"

// GraphSpineVersion is the graph-spine schema version stamped into every record. A rehydrate
// whose stamped version does not match the running engine's is REFUSED (divergence contract) —
// a schema change cannot silently mis-rehydrate an old spine. Bump this whenever the
// GraphSpineRecord shape changes.
const GraphSpineVersion = 1

// GraphSpineRecord is the persisted compressed spine: the stamped Version + Substrate (the
// divergence contract's two keys) plus the projection of subconscious.Context's Goal + L1Snapshot
// (BranchID, Gist, ThoughtIDs, Resolution). The full Snapshot []types.Thought is deliberately NOT
// carried — the Gist is the light-re-orientation material (§4/§9). Substrate-tagged so a spine
// recorded against one substrate is never silently rehydrated against another (the same hygiene
// rule the resume cursor + percept-log honour).
type GraphSpineRecord struct {
	Version    int    `json:"version"`
	Substrate  string `json:"substrate,omitempty"`
	Goal       string `json:"goal"`
	BranchID   int    `json:"branch_id"`
	Gist       string `json:"gist"`
	ThoughtIDs []int  `json:"thought_ids"`
	Resolution string `json:"resolution"`
}

// SaveGraphSpine writes the compressed spine to graph_spine.json, overwriting the prior spine.
// Saving is always safe (it writes a file, never mutates engine state), mirroring SaveResume /
// SavePerceptLog.
func (s *JSONLStore) SaveGraphSpine(r GraphSpineRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(fileGraphSpine), append(data, '\n'), 0o644)
}

// LoadGraphSpine reads the compressed spine. (nil, nil) when none exists yet (a cold start) or
// the file is unparsable — the caller cold-boots rather than crashing on a corrupt spine. The
// DIVERGENCE check (version/substrate match) is the ENGINE's job, not the store's: the store
// loads the raw record; the engine decides whether it is rehydratable.
func (s *JSONLStore) LoadGraphSpine() (*GraphSpineRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(fileGraphSpine))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r GraphSpineRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil
	}
	return &r, nil
}
