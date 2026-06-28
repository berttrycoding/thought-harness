package persist

import (
	"errors"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// FrozenStore is a READ-ONLY, in-memory Store that serves ONE frozen Snapshot — the substrate for a
// SHADOW engine (the self-benchmark loop, Track H SB0 — benchmark-taxonomy §7.2). The self-bench
// measures a CHECKPOINT, never the live, mutating engine: a shadow engine constructed over a FrozenStore
// re-seeds its registries from the captured snapshot (Load returns it) and CANNOT write back — every
// Save*/Flush/Reset is a silent no-op, so the shadow run produces ZERO contamination of the live store's
// disk and ZERO durable side effects. This dissolves the "measuring yourself while you run contaminates
// the measurement" objection at the persistence boundary: the snapshot is immutable for the run.
//
// It is the in-memory twin of JSONLStore for a frozen read: same interface, but no directory, no I/O,
// and no mutation. Construct it with NewFrozenStore(snap); hand it to engine.EngineConfig.Store.
type FrozenStore struct {
	snap Snapshot
	emit events.Emit
}

// NewFrozenStore wraps a frozen Snapshot as a read-only Store. The snapshot is copied by value (the
// caller's slices are not retained), so a later mutation of the source cannot leak into the shadow run.
func NewFrozenStore(snap Snapshot) *FrozenStore {
	return &FrozenStore{snap: cloneSnapshot(snap)}
}

// cloneSnapshot deep-ish copies a Snapshot (the slices + the Priors map) so the FrozenStore owns its
// own immutable view — the same copy discipline JSONLStore.Snapshot uses.
func cloneSnapshot(s Snapshot) Snapshot {
	cp := Snapshot{
		Skills:      append([]SkillRecord(nil), s.Skills...),
		Operators:   append([]OpRecord(nil), s.Operators...),
		Specialists: append([]SpecialistRecord(nil), s.Specialists...),
		ProgramRuns: append([]ProgramRunRecord(nil), s.ProgramRuns...),
		Episodes:    append([]EpisodeRecord(nil), s.Episodes...),
		Beliefs:     append([]BeliefRecord(nil), s.Beliefs...),
		Knowledge:   append([]KnowledgeRecord(nil), s.Knowledge...),
		Preferences: append([]PreferenceRecord(nil), s.Preferences...),
	}
	if s.Priors != nil {
		pc := *s.Priors
		pc.Priors = make(map[string]float64, len(s.Priors.Priors))
		for d, v := range s.Priors.Priors {
			pc.Priors[d] = v
		}
		cp.Priors = &pc
	}
	return cp
}

// Load returns the frozen snapshot — the shadow engine re-seeds its registries from it.
func (s *FrozenStore) Load() (*Snapshot, error) {
	cp := cloneSnapshot(s.snap)
	return &cp, nil
}

// Snapshot returns the live (== frozen) in-memory records. Used by any consolidation read; the engine
// never mutates a FrozenStore, so this is the same immutable view.
func (s *FrozenStore) Snapshot() *Snapshot {
	cp := cloneSnapshot(s.snap)
	return &cp
}

// SetEmit wires the bus closure (kept for interface parity; a FrozenStore emits nothing — it is silent).
func (s *FrozenStore) SetEmit(emit events.Emit) { s.emit = emit }

// --- write path: every mutation is a silent no-op (read-only) ---------------

func (s *FrozenStore) SaveSkill(SkillRecord) error                  { return nil }
func (s *FrozenStore) SaveOperator(OpRecord) error                  { return nil }
func (s *FrozenStore) SaveSpecialist(SpecialistRecord) error        { return nil }
func (s *FrozenStore) SaveProgramRun(ProgramRunRecord) error        { return nil }
func (s *FrozenStore) SaveGatePriors(map[string]float64, int) error { return nil }
func (s *FrozenStore) SaveEpisode(EpisodeRecord) error              { return nil }
func (s *FrozenStore) SaveBelief(BeliefRecord) error                { return nil }
func (s *FrozenStore) SaveKnowledge(KnowledgeRecord) error          { return nil }
func (s *FrozenStore) SaveKeyframe(KeyframeRecord) error            { return nil } // F-M7: immutable, ignore
func (s *FrozenStore) SavePreference(PreferenceRecord) error        { return nil }
func (s *FrozenStore) Replace(*Snapshot)                            {} // immutable: ignore
func (s *FrozenStore) Flush() error                                 { return nil }

// --- named-snapshot / ledger / resume / percept / spine: not served by a frozen shadow ---

func (s *FrozenStore) SaveSnapshot(SnapshotRecord) error { return nil }
func (s *FrozenStore) LoadSnapshot(name string) (*SnapshotRecord, error) {
	return nil, errors.New("persist: frozen store serves no named snapshots")
}
func (s *FrozenStore) ListSnapshots() ([]SnapshotMeta, error) { return []SnapshotMeta{}, nil }
func (s *FrozenStore) DeleteSnapshot(string) error            { return nil }
func (s *FrozenStore) ResetToSnapshot(string) error           { return nil }
func (s *FrozenStore) DiffSnapshots(from, to string) (*SnapshotDiff, error) {
	return nil, errors.New("persist: frozen store serves no named snapshots")
}

func (s *FrozenStore) SaveResume(ResumeRecord) error { return nil }
func (s *FrozenStore) LoadResume() (*ResumeRecord, error) {
	return nil, errors.New("persist: frozen store has no resume record")
}
func (s *FrozenStore) SavePerceptLog(PerceptLogRecord) error { return nil }
func (s *FrozenStore) LoadPerceptLog() (*PerceptLogRecord, error) {
	return nil, errors.New("persist: frozen store has no percept log")
}
func (s *FrozenStore) SaveGraphSpine(GraphSpineRecord) error { return nil }
func (s *FrozenStore) LoadGraphSpine() (*GraphSpineRecord, error) {
	return nil, errors.New("persist: frozen store has no graph spine")
}

func (s *FrozenStore) SaveLedgerEntry(LedgerEntry) error   { return nil }
func (s *FrozenStore) LoadLedger() ([]LedgerEntry, error)  { return []LedgerEntry{}, nil }
func (s *FrozenStore) SaveLedgerConfig(LedgerConfig) error { return nil }
func (s *FrozenStore) LoadLedgerConfig() (LedgerConfig, error) {
	return DefaultLedgerConfig(), nil
}

// ensure FrozenStore satisfies the Store interface at compile time.
var _ Store = (*FrozenStore)(nil)
