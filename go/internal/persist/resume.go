package persist

import (
	"encoding/json"
	"os"
)

// resume.go persists the deterministic-resume CURSOR — every advancing engine RNG
// stream's MT19937 state plus the logical tick — so a powered-down cognition CONTINUES
// its seeded stream instead of restarting it from position 0 (proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §3).
//
// This is PROCESS state ("where I was"), distinct from the learned-state Snapshot
// ("what I learned"): it lives in its own resume.json and is NOT part of Load() or the
// curator's working set.

const fileResume = "resume.json"

// RNGStreamState is one MT19937 generator's serialized state — the 624-word vector
// (Words) plus the read index — for a named engine RNG stream. Kept as a plain
// []uint32 (not the engine's fixed [624]uint32) so the persist layer stays free of the
// cpyrand dependency; the engine converts at the boundary.
type RNGStreamState struct {
	Words []uint32 `json:"words"`
	Index int      `json:"index"`
}

// ResumeRecord is the full deterministic-resume cursor: every RNG stream's state keyed
// by name, plus the tick reached. Substrate-tagged so a cursor is never silently
// replayed against the wrong substrate's state.
type ResumeRecord struct {
	Streams   map[string]RNGStreamState `json:"streams"`
	Tick      int                       `json:"tick"`
	Substrate string                    `json:"substrate,omitempty"`
}

// SaveResume writes the resume cursor to resume.json, overwriting the prior cursor.
func (s *JSONLStore) SaveResume(r ResumeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(fileResume), append(data, '\n'), 0o644)
}

// LoadResume reads the resume cursor. (nil, nil) when none exists yet (a cold start) or
// the file is unparsable — the caller cold-boots rather than crashing on a corrupt
// cursor.
func (s *JSONLStore) LoadResume() (*ResumeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(fileResume))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r ResumeRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil
	}
	return &r, nil
}
