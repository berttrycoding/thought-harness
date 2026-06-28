package persist

import (
	"encoding/json"
	"os"
)

// percept_log.go persists the deterministic PERCEPT-LOG — the once-recorded,
// replayable boundary SENSED VALUES (the clock now; world/host later) — so a
// resumed or golden run REPLAYS each non-deterministic sense instead of
// re-reading reality (proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md
// §3.2 + §11 Track 1.5; red-team amendment 3).
//
// WHY a separate file (mirrors resume.go): real-world sensing breaks the
// seeded-RNG / golden determinism contract UNLESS each sensed value is captured
// ONCE at the seam and replayed thereafter. This is PROCESS/perception state,
// distinct from the learned-state Snapshot and from the resume CURSOR — it lives
// in its own percept_log.json and is NOT part of Load() or the curator's set.
//
// The DIVERGENCE CONTRACT (red-team amendment 3): a log carries a Version + a
// Substrate stamp; on replay the ENGINE refuses to apply a log whose Version or
// Substrate does not match the running engine's (it falls back to cold-sense /
// time-blind) — never a best-effort partial replay. A corrupt/missing file ⇒
// (nil, nil) cold start. This file is I/O-only; the engine owns the divergence
// check (engine/percept.go), exactly as the engine (not the store) owns the
// resume RNG-width check.

const filePerceptLog = "percept_log.json"

// PerceptLogVersion is the percept-log schema version stamped into every record.
// A replay whose stamped version does not match the running engine's expected
// version is REFUSED (divergence contract) — a schema change cannot silently
// mis-replay an old log. Bump this whenever the PerceptEntry shape changes.
const PerceptLogVersion = 1

// PerceptEntry is one recorded boundary sense: the logical Tick it was read on,
// the sensor Kind ("clock"; "world"/"host" later), and the captured Value
// (string-encoded so any sensor's reading round-trips through one log). On
// REPLAY the entry for (Tick, Kind) returns Value instead of re-reading reality.
type PerceptEntry struct {
	Tick  int    `json:"tick"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// PerceptLogRecord is the full replayable percept-log: the stamped Version +
// Substrate (the divergence contract's two keys) plus the ordered entries.
// Substrate-tagged so a percept recorded against one substrate is never silently
// replayed against another (a clock read on claude vs the Fake are not the same
// lineage — the same hygiene rule the resume cursor honours).
type PerceptLogRecord struct {
	Version   int            `json:"version"`
	Substrate string         `json:"substrate,omitempty"`
	Entries   []PerceptEntry `json:"entries"`
}

// SavePerceptLog writes the percept-log to percept_log.json, overwriting the
// prior log. Saving is always safe (it writes a file, never mutates engine
// state), mirroring SaveResume.
func (s *JSONLStore) SavePerceptLog(r PerceptLogRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(filePerceptLog), append(data, '\n'), 0o644)
}

// LoadPerceptLog reads the percept-log. (nil, nil) when none exists yet (a cold
// start) or the file is unparsable — the caller cold-senses rather than crashing
// on a corrupt log. The DIVERGENCE check (version/substrate match) is the
// ENGINE's job, not the store's: the store loads the raw record; the engine
// decides whether it is replayable.
func (s *JSONLStore) LoadPerceptLog() (*PerceptLogRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(filePerceptLog))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r PerceptLogRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil
	}
	return &r, nil
}
