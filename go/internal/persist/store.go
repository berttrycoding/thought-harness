package persist

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// ErrUngrounded is returned by SaveEpisode/SaveBelief/SaveKnowledge when the record is not grounded —
// never-fabricate enforced at the persistence boundary (a fabricated record never reaches disk).
var ErrUngrounded = errors.New("persist: refusing to save an ungrounded record (never-fabricate)")

// Store is the cross-session persistence port the engine holds (injected via EngineConfig.Store; nil ⇒
// in-memory only, so tests/heuristic never touch disk). Load reads every artifact at start; each Save*
// records one artifact on its mint; Flush writes the durable state to disk (on exit / a debounce).
// Curate returns the in-memory records for the curator to operate on, and Replace writes the curated
// result back — the curator stays PURE (no I/O) by working over these slices.
//
// Registry Ledger (W1): snapshot/reset/diff/list for named, substrate-tagged state versions.
//
// Self-Change Ledger (W1): append-only ledger of every self-modification with scope + evidence + gate + revert handle.
type Store interface {
	Load() (*Snapshot, error)
	SaveSkill(SkillRecord) error
	SaveOperator(OpRecord) error
	SaveSpecialist(SpecialistRecord) error
	SaveProgramRun(ProgramRunRecord) error        // the trace->skill recurrence counter (durable mint-from-recurrence)
	SaveGatePriors(map[string]float64, int) error // (priors, tick)
	SaveEpisode(EpisodeRecord) error
	SaveBelief(BeliefRecord) error
	SaveKnowledge(KnowledgeRecord) error
	SavePreference(PreferenceRecord) error
	SaveKeyframe(KeyframeRecord) error // the loop-closure / recurrence keyframe DB (F-M7)
	Snapshot() *Snapshot               // the live in-memory records (the curator's working set)
	Replace(*Snapshot)                 // write the curated snapshot back as the live set
	Flush() error                      // persist the live set to disk
	SetEmit(events.Emit)               // wire the bus closure (persist.save) — nil ⇒ silent

	// Registry Ledger (W1)
	SaveSnapshot(SnapshotRecord) error                    // persist a named snapshot (immutable, substrate-tagged)
	LoadSnapshot(name string) (*SnapshotRecord, error)    // load a named snapshot by name
	ListSnapshots() ([]SnapshotMeta, error)               // list all named snapshots (metadata only)
	DeleteSnapshot(name string) error                     // delete a named snapshot
	ResetToSnapshot(name string) error                    // replace live state with a named snapshot (revert)
	DiffSnapshots(from, to string) (*SnapshotDiff, error) // diff two named snapshots

	// Deterministic resume (cognitive power-cycle): the RNG cursor + tick (resume.go)
	SaveResume(ResumeRecord) error
	LoadResume() (*ResumeRecord, error)

	// Deterministic percept-log (cognitive power-cycle, Track 1.5): the once-recorded,
	// replayable boundary sensed values (the clock now; world/host later) — percept_log.go.
	SavePerceptLog(PerceptLogRecord) error
	LoadPerceptLog() (*PerceptLogRecord, error)

	// Compressed graph spine (cognitive power-cycle, Track 2): the lossy L1 view of the
	// active branch a resumed session re-grounds in ("where I was") — graph_spine.go.
	SaveGraphSpine(GraphSpineRecord) error
	LoadGraphSpine() (*GraphSpineRecord, error)

	// Self-Change Ledger (W1)
	SaveLedgerEntry(LedgerEntry) error       // append a ledger entry
	LoadLedger() ([]LedgerEntry, error)      // load all ledger entries (newest first)
	SaveLedgerConfig(LedgerConfig) error     // persist ledger configuration
	LoadLedgerConfig() (LedgerConfig, error) // load ledger configuration (returns default if none)
}

// JSONLStore is the default Store: per-artifact append-only JSONL / small JSON files under a directory
// (the CHARTER/memory-stack default; SQLite+FTS5 is a future knob behind Persist.Backend). It keeps the
// records in memory (the source of truth the curator mutates) and rewrites each file on Flush — small,
// bounded learned-state files, so a full rewrite is cheaper + simpler than append+compact and lets the
// curator's invalidate/dedup/cap actually shrink a file. Concurrency-safe (the engine may Save from a
// fan-out goroutine while the loop flushes).
type JSONLStore struct {
	dir  string
	emit events.Emit

	mu   sync.Mutex
	snap Snapshot
}

// the per-artifact filenames under the state dir.
const (
	fileSkills             = "skills.jsonl"
	fileOperators          = "operators.jsonl"
	filePrimitiveSubAgents = "specialists.jsonl"
	fileProgramRuns        = "program_runs.jsonl"
	fileEpisodes           = "episodes.jsonl"
	fileBeliefs            = "beliefs.jsonl"
	fileKnowledge          = "knowledge.jsonl"
	filePreferences        = "preferences.jsonl"
	fileKeyframes          = "keyframes.jsonl" // the loop-closure / recurrence keyframe DB (F-M7)
	filePriors             = "gate_priors.json"
	fileSnapshots          = "snapshots.jsonl"    // named snapshots (registry ledger)
	fileLedger             = "ledger.jsonl"       // self-change ledger
	fileLedgerCfg          = "ledger_config.json" // ledger configuration
)

// NewJSONLStore builds a JSONL store rooted at dir, creating it if needed. The dir holds one file per
// artifact; a missing file is a cold start (not an error).
func NewJSONLStore(dir string) (*JSONLStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("persist: empty state dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &JSONLStore{dir: dir}, nil
}

// SetEmit wires the bus closure so persist.save/persist.load become observable. nil ⇒ silent.
func (s *JSONLStore) SetEmit(emit events.Emit) { s.emit = emit }

// Dir reports the store's root directory.
func (s *JSONLStore) Dir() string { return s.dir }

// Load reads every artifact file into the in-memory snapshot and returns it. Best-effort per line (a
// malformed row is skipped, never a crash); never-fabricate is re-applied (an ungrounded episode/belief/
// knowledge row is dropped, and a record without an explicit Grounded but valid by validity-window is
// kept only when its Grounded flag is set). DORMANT/ARCHIVED records are read into the store (so the
// curator still sees them) but are NOT part of the active re-seed (the engine seeds only active ones).
func (s *JSONLStore) Load() (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap = Snapshot{}

	loadJSONL(s.path(fileSkills), func(line []byte) {
		var r SkillRecord
		if json.Unmarshal(line, &r) == nil {
			s.snap.Skills = append(s.snap.Skills, r)
		}
	})
	loadJSONL(s.path(fileOperators), func(line []byte) {
		var r OpRecord
		if json.Unmarshal(line, &r) == nil {
			s.snap.Operators = append(s.snap.Operators, r)
		}
	})
	loadJSONL(s.path(filePrimitiveSubAgents), func(line []byte) {
		var r SpecialistRecord
		if json.Unmarshal(line, &r) == nil {
			s.snap.Specialists = append(s.snap.Specialists, r)
		}
	})
	loadJSONL(s.path(fileProgramRuns), func(line []byte) {
		var r ProgramRunRecord
		if json.Unmarshal(line, &r) == nil {
			s.snap.ProgramRuns = append(s.snap.ProgramRuns, r)
		}
	})
	loadJSONL(s.path(fileEpisodes), func(line []byte) {
		var r EpisodeRecord
		if json.Unmarshal(line, &r) == nil && r.Meta.Grounded {
			s.snap.Episodes = append(s.snap.Episodes, r)
		}
	})
	loadJSONL(s.path(fileBeliefs), func(line []byte) {
		var r BeliefRecord
		if json.Unmarshal(line, &r) == nil && r.Meta.Grounded {
			s.snap.Beliefs = append(s.snap.Beliefs, r)
		}
	})
	loadJSONL(s.path(fileKnowledge), func(line []byte) {
		var r KnowledgeRecord
		if json.Unmarshal(line, &r) == nil && r.Meta.Grounded {
			s.snap.Knowledge = append(s.snap.Knowledge, r)
		}
	})
	loadJSONL(s.path(filePreferences), func(line []byte) {
		var r PreferenceRecord
		if json.Unmarshal(line, &r) == nil {
			s.snap.Preferences = append(s.snap.Preferences, r)
		}
	})
	loadJSONL(s.path(fileKeyframes), func(line []byte) {
		var r KeyframeRecord
		if json.Unmarshal(line, &r) == nil && r.Descriptor != "" {
			s.snap.Keyframes = append(s.snap.Keyframes, r)
		}
	})
	if data, err := os.ReadFile(s.path(filePriors)); err == nil {
		var r PriorsRecord
		if json.Unmarshal(data, &r) == nil {
			s.snap.Priors = &r
		}
	}

	// NOTE: persist.load is emitted by the ENGINE (deferred to the first Step), not here — Load runs
	// inside NewEngine BEFORE the CLI/TUI sinks subscribe, so emitting now would be lost on the console
	// (the same deferral config.load uses). The store stays I/O-only; the engine owns the bus event.
	cp := s.snap
	return &cp, nil
}

// SaveSkill records a minted skill (dedup-by-hash, version-on-body-change). It upserts the in-memory set
// and emits persist.save; the durable write happens on Flush.
func (s *JSONLStore) SaveSkill(r SkillRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Name, mapKey(r.Body), strings.Join(r.Triggers, ","))
	for i := range s.snap.Skills {
		if s.snap.Skills[i].Name == r.Name {
			if s.snap.Skills[i].Meta.Hash == r.Meta.Hash {
				return nil // identical re-mint: idempotent
			}
			r.Meta.Version = s.snap.Skills[i].Meta.Version + 1 // body changed: a new version
			r.Meta.CreatedTick = s.snap.Skills[i].Meta.CreatedTick
			s.snap.Skills[i] = withDefaults(r.Meta, r).(SkillRecord)
			s.saved("skill", r.Name, r.Meta.Version)
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(SkillRecord)
	s.snap.Skills = append(s.snap.Skills, r)
	s.saved("skill", r.Name, r.Meta.Version)
	return nil
}

// SaveOperator records a minted operator (dedup-by-name+intent).
func (s *JSONLStore) SaveOperator(r OpRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Name, r.Family, r.Intent, r.Move)
	for i := range s.snap.Operators {
		if s.snap.Operators[i].Name == r.Name {
			if s.snap.Operators[i].Meta.Hash == r.Meta.Hash {
				return nil
			}
			r.Meta.Version = s.snap.Operators[i].Meta.Version + 1
			r.Meta.CreatedTick = s.snap.Operators[i].Meta.CreatedTick
			s.snap.Operators[i] = withDefaults(r.Meta, r).(OpRecord)
			s.saved("operator", r.Name, r.Meta.Version)
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(OpRecord)
	s.snap.Operators = append(s.snap.Operators, r)
	s.saved("operator", r.Name, r.Meta.Version)
	return nil
}

// SaveSpecialist records a minted specialist (dedup-by-domain; a re-mint with a new answer versions it).
func (s *JSONLStore) SaveSpecialist(r SpecialistRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Domain, r.Answer)
	for i := range s.snap.Specialists {
		if s.snap.Specialists[i].Domain == r.Domain {
			if s.snap.Specialists[i].Meta.Hash == r.Meta.Hash &&
				s.snap.Specialists[i].Demoted == r.Demoted {
				return nil
			}
			r.Meta.Version = s.snap.Specialists[i].Meta.Version + 1
			r.Meta.CreatedTick = s.snap.Specialists[i].Meta.CreatedTick
			if r.Demoted {
				r.Meta.Status = StatusDemoted
			}
			s.snap.Specialists[i] = withDefaults(r.Meta, r).(SpecialistRecord)
			s.saved("specialist", r.Domain, r.Meta.Version)
			return nil
		}
	}
	if r.Demoted {
		r.Meta.Status = StatusDemoted
	}
	r = withDefaults(r.Meta, r).(SpecialistRecord)
	s.snap.Specialists = append(s.snap.Specialists, r)
	s.saved("specialist", r.Domain, r.Meta.Version)
	return nil
}

// SaveProgramRun records a recurring synthesised-program counter (dedup-by-goalKey; the latest count/
// shape/body wins). It is the durable trace->skill recurrence counter: each consolidation upserts the
// goal family's current count so a fresh-engine episode can resume the tally instead of resetting to 1.
// An unchanged (same count + shape) re-save is idempotent. The body is the whole-program ToDict
// envelope, so a re-loaded run can re-mint the same Program when it crosses MintAfter.
func (s *JSONLStore) SaveProgramRun(r ProgramRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.GoalKey, r.Shape, itoa(r.Count), mapKey(r.Body))
	for i := range s.snap.ProgramRuns {
		if s.snap.ProgramRuns[i].GoalKey == r.GoalKey {
			if s.snap.ProgramRuns[i].Meta.Hash == r.Meta.Hash &&
				s.snap.ProgramRuns[i].Minted == r.Minted {
				return nil // identical (same count/shape/body/minted): idempotent
			}
			r.Meta.Version = s.snap.ProgramRuns[i].Meta.Version + 1
			r.Meta.CreatedTick = s.snap.ProgramRuns[i].Meta.CreatedTick
			s.snap.ProgramRuns[i] = withDefaults(r.Meta, r).(ProgramRunRecord)
			s.saved("program_run", r.GoalKey, r.Meta.Version)
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(ProgramRunRecord)
	s.snap.ProgramRuns = append(s.snap.ProgramRuns, r)
	s.saved("program_run", r.GoalKey, r.Meta.Version)
	return nil
}

// SaveGatePriors records the compiled gate priors as one snapshot record (latest wins).
func (s *JSONLStore) SaveGatePriors(priors map[string]float64, tick int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(priors) == 0 {
		return nil
	}
	cp := make(map[string]float64, len(priors))
	for d, v := range priors {
		cp[d] = v
	}
	rec := PriorsRecord{Priors: cp}
	rec.Meta = Meta{Status: StatusActive, Grounded: true, LastUsedTick: tick, UseCount: 1, Hash: hashPriors(cp)}
	if s.snap.Priors != nil {
		rec.Meta.CreatedTick = s.snap.Priors.Meta.CreatedTick
		rec.Meta.Version = s.snap.Priors.Meta.Version + 1
	} else {
		rec.Meta.CreatedTick = tick
	}
	s.snap.Priors = &rec
	s.saved("gate_priors", "priors", rec.Meta.Version)
	return nil
}

// SaveEpisode records a grounded episode (never-fabricate: an ungrounded record is rejected).
func (s *JSONLStore) SaveEpisode(r EpisodeRecord) error {
	if !r.Meta.Grounded {
		return ErrUngrounded
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Goal, r.Outcome)
	for i := range s.snap.Episodes {
		if s.snap.Episodes[i].Meta.Hash == r.Meta.Hash {
			s.snap.Episodes[i].Meta.UseCount++
			s.snap.Episodes[i].Meta.LastUsedTick = r.Tick
			return nil // identical episode: collapse (dedup), don't re-append
		}
	}
	r = withDefaults(r.Meta, r).(EpisodeRecord)
	s.snap.Episodes = append(s.snap.Episodes, r)
	s.saved("episode", clip(r.Goal, 40), r.Meta.Version)
	return nil
}

// SaveBelief records a grounded belief (never-fabricate). Bi-temporal: a re-saved belief with the same
// statement updates its ValidTo (an invalidation rides along), so refutation reconstructs exactly.
func (s *JSONLStore) SaveBelief(r BeliefRecord) error {
	if !r.Meta.Grounded {
		return ErrUngrounded
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Statement, itoa(r.ValidFrom))
	for i := range s.snap.Beliefs {
		if s.snap.Beliefs[i].Meta.Hash == r.Meta.Hash {
			// update the validity window (an invalidation came in for the same belief instance)
			s.snap.Beliefs[i].ValidTo = r.ValidTo
			if r.ValidTo != 0 {
				s.snap.Beliefs[i].Meta.Status = StatusDemoted // refuted: kept for audit, excluded from recall
			}
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(BeliefRecord)
	if r.ValidTo != 0 {
		r.Meta.Status = StatusDemoted
	}
	s.snap.Beliefs = append(s.snap.Beliefs, r)
	s.saved("belief", clip(r.Statement, 40), r.Meta.Version)
	return nil
}

// SaveKnowledge records a grounded knowledge item (never-fabricate). Bi-temporal like beliefs.
func (s *JSONLStore) SaveKnowledge(r KnowledgeRecord) error {
	if !r.Meta.Grounded {
		return ErrUngrounded
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Statement, r.Kind, itoa(r.ValidFrom))
	for i := range s.snap.Knowledge {
		if s.snap.Knowledge[i].Meta.Hash == r.Meta.Hash {
			s.snap.Knowledge[i].ValidTo = r.ValidTo
			if r.ValidTo != 0 {
				s.snap.Knowledge[i].Meta.Status = StatusDemoted
			}
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(KnowledgeRecord)
	if r.ValidTo != 0 {
		r.Meta.Status = StatusDemoted
	}
	s.snap.Knowledge = append(s.snap.Knowledge, r)
	s.saved("knowledge", clip(r.Statement, 40), r.Meta.Version)
	return nil
}

// SavePreference records a learned person preference (upsert by trait; latest value wins).
func (s *JSONLStore) SavePreference(r PreferenceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Trait, r.Value)
	for i := range s.snap.Preferences {
		if s.snap.Preferences[i].Trait == r.Trait {
			if s.snap.Preferences[i].Meta.Hash == r.Meta.Hash {
				s.snap.Preferences[i].Meta.UseCount++
				return nil
			}
			r.Meta.Version = s.snap.Preferences[i].Meta.Version + 1
			r.Meta.CreatedTick = s.snap.Preferences[i].Meta.CreatedTick
			s.snap.Preferences[i] = withDefaults(r.Meta, r).(PreferenceRecord)
			s.saved("preference", r.Trait, r.Meta.Version)
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(PreferenceRecord)
	s.snap.Preferences = append(s.snap.Preferences, r)
	s.saved("preference", r.Trait, r.Meta.Version)
	return nil
}

// SaveKeyframe records one loop-closure / recurrence keyframe (F-M7), upsert by Descriptor. The
// engine exports the WHOLE live DB each consolidation, so a re-save of an existing descriptor takes
// the latest count/closures/last-seen (which only grow) and keeps the EARLIEST first-seen (the
// bi-temporal loop-back point survives across runs). The descriptor is the dedup key (Meta.Hash).
func (s *JSONLStore) SaveKeyframe(r KeyframeRecord) error {
	if r.Descriptor == "" {
		return nil // a blank descriptor is no keyframe (never-fabricate of the recurrence key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Meta.Hash = hashStr(r.Descriptor)
	for i := range s.snap.Keyframes {
		if s.snap.Keyframes[i].Descriptor == r.Descriptor {
			cur := &s.snap.Keyframes[i]
			cur.Count = r.Count
			cur.Closures = r.Closures
			cur.Gist = r.Gist
			if r.LastSeenTick > cur.LastSeenTick {
				cur.LastSeenTick = r.LastSeenTick
			}
			if cur.FirstSeenTick == 0 || (r.FirstSeenTick != 0 && r.FirstSeenTick < cur.FirstSeenTick) {
				cur.FirstSeenTick = r.FirstSeenTick
			}
			cur.Meta.Version++
			cur.Meta.LastUsedTick = r.LastSeenTick
			cur.Meta.Substrate = r.Meta.Substrate
			return nil
		}
	}
	r = withDefaults(r.Meta, r).(KeyframeRecord)
	s.snap.Keyframes = append(s.snap.Keyframes, r)
	s.saved("keyframe", clip(r.Gist, 40), r.Meta.Version)
	return nil
}

// Snapshot returns a deep-ish copy of the live in-memory records (the curator's working set). Copies the
// slices so the caller (curator) can rebuild without racing the store; the maps inside Priors are copied.
func (s *JSONLStore) Snapshot() *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := Snapshot{
		Skills:      append([]SkillRecord(nil), s.snap.Skills...),
		Operators:   append([]OpRecord(nil), s.snap.Operators...),
		Specialists: append([]SpecialistRecord(nil), s.snap.Specialists...),
		ProgramRuns: append([]ProgramRunRecord(nil), s.snap.ProgramRuns...),
		Episodes:    append([]EpisodeRecord(nil), s.snap.Episodes...),
		Beliefs:     append([]BeliefRecord(nil), s.snap.Beliefs...),
		Knowledge:   append([]KnowledgeRecord(nil), s.snap.Knowledge...),
		Preferences: append([]PreferenceRecord(nil), s.snap.Preferences...),
		Keyframes:   append([]KeyframeRecord(nil), s.snap.Keyframes...),
	}
	if s.snap.Priors != nil {
		pc := *s.snap.Priors
		pc.Priors = make(map[string]float64, len(s.snap.Priors.Priors))
		for d, v := range s.snap.Priors.Priors {
			pc.Priors[d] = v
		}
		cp.Priors = &pc
	}
	return &cp
}

// Replace overwrites the live record set with the curated snapshot (the curator's output). The next
// Flush writes it to disk. nil ⇒ no-op.
func (s *JSONLStore) Replace(sn *Snapshot) {
	if sn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap = *sn
}

// Flush writes every artifact's live records to its file (a full rewrite — small bounded state). All
// records are written (active/demoted/dormant/archived), so the bi-temporal + audit history survives;
// the engine re-seeds only the ACTIVE ones on the next Load. Errors short-circuit (the first failure).
func (s *JSONLStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSONL(s.path(fileSkills), len(s.snap.Skills), func(i int) any { return s.snap.Skills[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(fileOperators), len(s.snap.Operators), func(i int) any { return s.snap.Operators[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(filePrimitiveSubAgents), len(s.snap.Specialists), func(i int) any { return s.snap.Specialists[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(fileProgramRuns), len(s.snap.ProgramRuns), func(i int) any { return s.snap.ProgramRuns[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(fileEpisodes), len(s.snap.Episodes), func(i int) any { return s.snap.Episodes[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(fileBeliefs), len(s.snap.Beliefs), func(i int) any { return s.snap.Beliefs[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(fileKnowledge), len(s.snap.Knowledge), func(i int) any { return s.snap.Knowledge[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(filePreferences), len(s.snap.Preferences), func(i int) any { return s.snap.Preferences[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.path(fileKeyframes), len(s.snap.Keyframes), func(i int) any { return s.snap.Keyframes[i] }); err != nil {
		return err
	}
	if s.snap.Priors != nil {
		data, err := json.MarshalIndent(s.snap.Priors, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(s.path(filePriors), append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// --- Registry Ledger (W1) ---------------------------------------------------

// SaveSnapshot persists a named, immutable snapshot of the entire learned state.
// The snapshot is substrate-tagged so frontier and local state never mix.
func (s *JSONLStore) SaveSnapshot(rec SnapshotRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure meta is complete
	if rec.Meta.Name == "" {
		return errors.New("persist: snapshot name is required")
	}
	if rec.Meta.CreatedAt == 0 {
		rec.Meta.CreatedAt = time.Now().UnixNano()
	}
	if rec.Meta.Version == 0 {
		rec.Meta.Version = 1
	}

	// Count artifacts
	rec.Meta.SkillCount = len(rec.Data.Skills)
	rec.Meta.OperatorCount = len(rec.Data.Operators)
	rec.Meta.PrimitiveSubAgentCount = len(rec.Data.Specialists)
	rec.Meta.EpisodeCount = len(rec.Data.Episodes)
	rec.Meta.BeliefCount = len(rec.Data.Beliefs)
	rec.Meta.KnowledgeCount = len(rec.Data.Knowledge)
	rec.Meta.PreferenceCount = len(rec.Data.Preferences)
	rec.Meta.HasPriors = rec.Data.Priors != nil

	// Append to snapshots.jsonl
	f, err := os.OpenFile(s.path(fileSnapshots), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := enc.Encode(rec); err != nil {
		return err
	}

	s.emitSnapshot("save", rec.Meta.Name, rec.Meta.Substrate)
	return nil
}

// LoadSnapshot loads a named snapshot by name.
func (s *JSONLStore) LoadSnapshot(name string) (*SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path(fileSnapshots))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec SnapshotRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Meta.Name == name {
			return &rec, nil
		}
	}
	return nil, errors.New("persist: snapshot not found: " + name)
}

// ListSnapshots returns metadata for all named snapshots (newest first).
func (s *JSONLStore) ListSnapshots() ([]SnapshotMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path(fileSnapshots))
	if err != nil {
		if os.IsNotExist(err) {
			return []SnapshotMeta{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var metas []SnapshotMeta
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec SnapshotRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		metas = append(metas, rec.Meta)
	}

	// Reverse to show newest first (append-only, so last written is newest)
	for i, j := 0, len(metas)-1; i < j; i, j = i+1, j-1 {
		metas[i], metas[j] = metas[j], metas[i]
	}
	return metas, nil
}

// DeleteSnapshot deletes a named snapshot (rewrites the file without it).
func (s *JSONLStore) DeleteSnapshot(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path(fileSnapshots))
	if err != nil {
		return err
	}
	defer f.Close()

	var records []SnapshotRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec SnapshotRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Meta.Name != name {
			records = append(records, rec)
		}
	}

	// Rewrite file
	return s.writeSnapshots(records)
}

// ResetToSnapshot replaces the live state with a named snapshot (revert).
func (s *JSONLStore) ResetToSnapshot(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, err := s.loadSnapshotUnlocked(name)
	if err != nil {
		return err
	}

	// Replace live state
	s.snap = rec.Data
	// reset is its OWN observable decision (registry.reset), not a snapshot-save: a revert mutates the
	// live registries, where a snapshot only captures them. Conflating the two under registry.snapshot
	// left registry.reset dead (defined, never emitted) — the W1 finish wires it to its real call site.
	s.emitReset(name, rec.Meta.Substrate)
	return nil
}

// DiffSnapshots computes a human-readable diff between two named snapshots.
func (s *JSONLStore) DiffSnapshots(from, to string) (*SnapshotDiff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	fromRec, err := s.loadSnapshotUnlocked(from)
	if err != nil {
		return nil, err
	}
	toRec, err := s.loadSnapshotUnlocked(to)
	if err != nil {
		return nil, err
	}

	diff := &SnapshotDiff{
		FromName: from,
		ToName:   to,
		Added:    make(map[string]int),
		Removed:  make(map[string]int),
		Changed:  make(map[string]int),
	}

	// Content-level diff (not a count delta): each artifact is keyed by its identity, so an added,
	// removed, OR a same-key-different-body CHANGED record is detected. A count-only diff missed the
	// keep-or-revert campaign's most common case — a same-size batch that SWAPS one item for another
	// (e.g. revert replaces skill A with skill B): equal counts, but the set genuinely changed.
	diffKeyed("skills", skillKeys(fromRec.Data.Skills), skillKeys(toRec.Data.Skills), diff)
	diffKeyed("operators", opKeys(fromRec.Data.Operators), opKeys(toRec.Data.Operators), diff)
	diffKeyed("specialists", specialistKeys(fromRec.Data.Specialists), specialistKeys(toRec.Data.Specialists), diff)
	diffKeyed("episodes", episodeKeys(fromRec.Data.Episodes), episodeKeys(toRec.Data.Episodes), diff)
	diffKeyed("beliefs", beliefKeys(fromRec.Data.Beliefs), beliefKeys(toRec.Data.Beliefs), diff)
	diffKeyed("knowledge", knowledgeKeys(fromRec.Data.Knowledge), knowledgeKeys(toRec.Data.Knowledge), diff)
	diffKeyed("preferences", preferenceKeys(fromRec.Data.Preferences), preferenceKeys(toRec.Data.Preferences), diff)
	diffKeyed("gate_priors", priorsKeys(fromRec.Data.Priors), priorsKeys(toRec.Data.Priors), diff)

	s.emitDiff(from, to, diff)
	return diff, nil
}

// loadSnapshotUnlocked loads a snapshot by name (must hold lock).
func (s *JSONLStore) loadSnapshotUnlocked(name string) (*SnapshotRecord, error) {
	f, err := os.Open(s.path(fileSnapshots))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec SnapshotRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Meta.Name == name {
			return &rec, nil
		}
	}
	return nil, errors.New("persist: snapshot not found: " + name)
}

// writeSnapshots rewrites the snapshots file with the given records.
func (s *JSONLStore) writeSnapshots(records []SnapshotRecord) error {
	f, err := os.Create(s.path(fileSnapshots))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

// emitSnapshot emits a registry.snapshot event (a named snapshot was captured — not a mutation).
func (s *JSONLStore) emitSnapshot(action, name, substrate string) {
	if s.emit == nil {
		return
	}
	s.emit(events.RegistrySnapshot, "registry: snapshot "+action+" "+name, events.D{
		"action":    action,
		"name":      name,
		"substrate": substrate,
	})
}

// emitReset emits a registry.reset event (the live registries were reverted to a snapshot — a real
// mutation of state, distinct from capturing a snapshot, hence its own kind).
func (s *JSONLStore) emitReset(name, substrate string) {
	if s.emit == nil {
		return
	}
	s.emit(events.RegistryReset, "registry: reset to "+name, events.D{
		"action":    "reset",
		"name":      name,
		"substrate": substrate,
	})
}

// emitDiff emits a registry.diff event with the added/removed/changed counts (the keep-or-revert
// campaign's witness — what a batch did to the registries, never silent).
func (s *JSONLStore) emitDiff(from, to string, diff *SnapshotDiff) {
	if s.emit == nil {
		return
	}
	s.emit(events.RegistryDiff, "registry: diff "+from+" -> "+to, events.D{
		"from":    from,
		"to":      to,
		"added":   sumCounts(diff.Added),
		"removed": sumCounts(diff.Removed),
		"changed": sumCounts(diff.Changed),
	})
}

// sumCounts totals a per-artifact count map (for the event summary scalar).
func sumCounts(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// path joins the state dir with a filename.
func (s *JSONLStore) path(name string) string { return filepath.Join(s.dir, name) }

// saved emits persist.save (called under the lock — emit is cheap + non-blocking on the bus).
func (s *JSONLStore) saved(artifact, id string, version int) {
	if s.emit == nil {
		return
	}
	s.emit(events.PersistSave, "persist: saved "+artifact+" "+id, events.D{
		"artifact": artifact, "id": id, "version": version,
	})
}

// -- helpers ---------------------------------------------------------------

// loadJSONL reads a JSONL file line-by-line, calling fn for each non-empty line. A missing file is a
// silent cold start; a malformed line is skipped by fn (which only appends on a successful unmarshal).
func loadJSONL(path string, fn func(line []byte)) {
	f, err := os.Open(path)
	if err != nil {
		return // cold start / unreadable: nothing to load
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		fn(append([]byte(nil), line...))
	}
}

// writeJSONL rewrites path with n records (one JSON object per line) via the indexed accessor. An empty
// set truncates the file (a curated-to-zero artifact leaves an empty file, not a stale one).
func writeJSONL(path string, n int, at func(i int) any) error {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	enc := json.NewEncoder(w)
	for i := 0; i < n; i++ {
		if err := enc.Encode(at(i)); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// withDefaults fills the curator metadata defaults (Status active, Version 0) on a fresh record's Meta,
// returning the record with the updated Meta. The type switch keeps persist free of reflection; each
// record type carries Meta as its first field, set here.
func withDefaults(m Meta, rec any) any {
	if m.Status == "" {
		m.Status = StatusActive
	}
	switch r := rec.(type) {
	case SkillRecord:
		r.Meta = m
		return r
	case OpRecord:
		r.Meta = m
		return r
	case SpecialistRecord:
		r.Meta = m
		return r
	case ProgramRunRecord:
		r.Meta = m
		return r
	case EpisodeRecord:
		r.Meta = m
		return r
	case BeliefRecord:
		r.Meta = m
		return r
	case KnowledgeRecord:
		r.Meta = m
		return r
	case PreferenceRecord:
		r.Meta = m
		return r
	case KeyframeRecord:
		r.Meta = m
		return r
	}
	return rec
}

// hashStr is the content dedup key: an FNV-1a hash of the joined identity fields, hex-encoded. Stable +
// deterministic (the same content always hashes the same), so dedup is reproducible across runs.
func hashStr(parts ...string) string {
	h := fnv.New64a()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// hashPriors hashes a gate-prior map deterministically (sorted keys), so an unchanged priors map is a
// dedup no-op.
func hashPriors(m map[string]float64) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		parts = append(parts, k, strconv.FormatFloat(m[k], 'f', 6, 64))
	}
	return hashStr(parts...)
}

// mapKey renders a node-dict deterministically for hashing (json.Marshal sorts map keys), so a skill
// body's hash is stable.
func mapKey(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// itoa / clip are tiny stdlib-light helpers reused across the package (summaries + hash parts).
func itoa(n int) string { return strconv.Itoa(n) }

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

// diffKeyed compares two artifact sets keyed by identity->body-hash and records added/removed/changed.
// A key present only in `to` is ADDED; only in `from` is REMOVED; in both with a different body hash is
// CHANGED. This catches the same-count content swap a count-only diff missed.
func diffKeyed(name string, from, to map[string]string, diff *SnapshotDiff) {
	var added, removed, changed int
	for k, toHash := range to {
		if fromHash, ok := from[k]; !ok {
			added++
		} else if fromHash != toHash {
			changed++
		}
	}
	for k := range from {
		if _, ok := to[k]; !ok {
			removed++
		}
	}
	if added > 0 {
		diff.Added[name] = added
	}
	if removed > 0 {
		diff.Removed[name] = removed
	}
	if changed > 0 {
		diff.Changed[name] = changed
	}
}

// the per-artifact identity->body-hash projections the content diff keys on. The identity is the
// dedup key the Save* path uses (name/domain/trait/statement), so a re-mint that bumps a body is a
// CHANGED, not an Added+Removed pair. Deterministic (hashStr is FNV over the body fields).
func skillKeys(rs []SkillRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.Name] = hashStr(r.Tier, mapKey(r.Body), strings.Join(r.Triggers, ","), r.Description)
	}
	return m
}

func opKeys(rs []OpRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.Name] = hashStr(r.Family, r.Intent, r.Move)
	}
	return m
}

func specialistKeys(rs []SpecialistRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.Domain] = hashStr(r.Answer, itoa(boolToInt(r.Demoted)))
	}
	return m
}

func episodeKeys(rs []EpisodeRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		key := hashStr(r.Goal, r.Outcome)
		m[key] = key // episodes are identity-only (dedup-by-hash); presence is the signal
	}
	return m
}

func beliefKeys(rs []BeliefRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[hashStr(r.Statement, itoa(r.ValidFrom))] = hashStr(itoa(r.ValidTo)) // ValidTo change = invalidation
	}
	return m
}

func knowledgeKeys(rs []KnowledgeRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[hashStr(r.Statement, r.Kind, itoa(r.ValidFrom))] = hashStr(itoa(r.ValidTo))
	}
	return m
}

func preferenceKeys(rs []PreferenceRecord) map[string]string {
	m := make(map[string]string, len(rs))
	for _, r := range rs {
		m[r.Trait] = hashStr(r.Value)
	}
	return m
}

func priorsKeys(p *PriorsRecord) map[string]string {
	if p == nil {
		return nil
	}
	m := make(map[string]string, len(p.Priors))
	for d, v := range p.Priors {
		m[d] = strconv.FormatFloat(v, 'f', 6, 64)
	}
	return m
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Self-Change Ledger (W1) ------------------------------------------------

// SaveLedgerEntry appends a ledger entry to the append-only ledger file.
func (s *JSONLStore) SaveLedgerEntry(entry LedgerEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path(fileLedger), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	if err := enc.Encode(entry); err != nil {
		return err
	}

	s.emitLedger("save", entry.Scope, entry.SafetyMode)
	return nil
}

// LoadLedger loads all ledger entries (newest first).
func (s *JSONLStore) LoadLedger() ([]LedgerEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path(fileLedger))
	if err != nil {
		if os.IsNotExist(err) {
			return []LedgerEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []LedgerEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry LedgerEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	// Reverse to show newest first (append-only, so last written is newest)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// SaveLedgerConfig persists the ledger configuration.
func (s *JSONLStore) SaveLedgerConfig(cfg LedgerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(fileLedgerCfg), append(data, '\n'), 0o644)
}

// LoadLedgerConfig loads the ledger configuration (returns default if none).
func (s *JSONLStore) LoadLedgerConfig() (LedgerConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path(fileLedgerCfg))
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultLedgerConfig(), nil
		}
		return LedgerConfig{}, err
	}
	var cfg LedgerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultLedgerConfig(), nil
	}
	return cfg, nil
}

// emitLedger emits a registry.ledger event.
func (s *JSONLStore) emitLedger(action string, scope LedgerScope, mode SafetyMode) {
	if s.emit == nil {
		return
	}
	s.emit(events.RegistryBatch, "registry: ledger "+action, events.D{
		"action":      action,
		"scope":       string(scope),
		"safety_mode": string(mode),
	})
}

// ensure the JSONLStore satisfies the Store interface at compile time.
var _ Store = (*JSONLStore)(nil)
