// Package ledger is the append-only keep-or-revert audit ledger of the
// registry-scaling measuring stick (docs/internal/notes/measuring-stick-spec.md §5.7,
// docs/internal/notes/registry-scaling-strategy.md §6).
//
// It is the single source of truth for the campaign's two questions:
//
//   - "is mechanism M validated at iteration k?" — IsValidated reads the keep
//     rule's recorded verdict (spec §4.6) straight off the ledger.
//   - "what was proposed, measured, and kept-or-reverted, and on which checker
//     version?" — every batch × arm × checker-version is one row.
//
// The discipline is INVALIDATE-NOT-DELETE (spec §4.7, §5.7; the append-only
// audit pattern): a row is NEVER rewritten or removed. When a checker is
// re-characterized, Invalidate APPENDS invalidation rows that flip the dependent
// rows' Status to "invalidated" — the original rows stay on disk, byte-for-byte,
// so the full history reconstructs exactly. The lift number a keep-rule reads is
// always the latest non-invalidated row for that (mechanism, tier, arm,
// checker-version) tuple.
//
// Determinism: every Record carries its own Tick (a logical, seeded tick supplied
// by the caller) and Seed. This package NEVER reads the wall clock and NEVER draws
// unseeded randomness — the durability math and the reproducible replay path both
// require that ticks/seeds are inputs, not side effects (CLAUDE.md "Determinism by
// default"). Load preserves file order, which is append order, which is tick
// order when the caller appends in tick order.
//
// The package is data + I/O only: it imports internal/bench/types for the shared
// enums (Mechanism, Tier, Arm) and the Contrast/Estimate effect estimates the
// report renders. It has no engine dependency and no TUI dependency (the report
// is plain text — no emoji, no lipgloss; this is the bench layer).
package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/berttrycoding/thought-harness/internal/bench/types"
)

// ledgerFile is the fixed JSONL filename inside the ledger directory.
const ledgerFile = "ledger.jsonl"

// Status is the lifecycle of a ledger row. A row is born Active and can only ever
// transition to Invalidated — never back, never deleted (spec §4.7,
// invalidate-not-delete).
type Status string

const (
	// StatusActive — the row is live; its lift number is trusted by the keep rule.
	StatusActive Status = "active"
	// StatusInvalidated — a re-characterized checker (a new CheckerVersion) has
	// superseded this row; it is kept for the audit trail but excluded from any
	// keep decision. The flip is recorded by an APPENDED invalidation row, never by
	// mutating this row in place.
	StatusInvalidated Status = "invalidated"
)

// RowKind distinguishes a primary measurement row from an appended invalidation
// marker. Invalidation markers are how invalidate-not-delete is honored: rather
// than rewrite a prior line, Invalidate appends a marker that points at the rows
// it supersedes.
type RowKind string

const (
	// KindMeasurement — a normal per-batch × arm × checker-version measurement row
	// (the bulk of the ledger). Spec §5.7.
	KindMeasurement RowKind = "measurement"
	// KindInvalidation — an appended marker that flips the Status of every
	// dependent active measurement row whose CheckerVersion differs from the new
	// one, as of a given tick. Spec §4.7.
	KindInvalidation RowKind = "invalidation"
	// KindVerdict — a recorded keep-rule verdict for a (mechanism, tier) at an
	// iteration (spec §4.6). IsValidated reads these. Kept in the same append-only
	// stream so the validation decision and its evidence share one source of truth.
	KindVerdict RowKind = "verdict"
)

// Record is one append-only ledger row (spec §5.7). It serializes to exactly one
// JSONL line. The fields the spec §5.7 names verbatim — item/scenario id, seed,
// arm, raw checker output, oracle verdict, isolation-predicate result, the run's
// events pointer, and the checker version hash — are all present; Kind/Status and
// the invalidation/verdict fields carry the invalidate-not-delete and keep-rule
// machinery.
//
// All time is the logical Tick supplied by the caller; there is no wall-clock
// field by design.
type Record struct {
	// Kind tags the row as a measurement, an invalidation marker, or a verdict.
	// Defaults to KindMeasurement when appended via Append.
	Kind RowKind `json:"kind"`
	// Tick is the logical, seeded tick at which this row was appended (NOT a
	// wall-clock time). The caller threads it from the engine's seeded clock so the
	// ledger is reproducible. Spec determinism rule (CLAUDE.md).
	Tick int `json:"tick"`
	// BatchID identifies the batch this measurement belongs to (a pilot or one
	// incremental batch of the sequential campaign, spec §4.7).
	BatchID string `json:"batch_id"`
	// Mechanism is the load-bearing mechanism under test on this row. Spec §3, §5.7.
	Mechanism types.Mechanism `json:"mechanism"`
	// Tier is A (atomic quizzes) or B (multi-turn scenarios). Spec §1.2.
	Tier types.Tier `json:"tier"`
	// Arm is the configuration the item/scenario was run under (bare / harness /
	// gate-on / gate-off / ...), paired by Seed across arms. Spec §5.1, §5.7.
	Arm types.Arm `json:"arm"`
	// ItemID is the item (Tier A) or scenario (Tier B) identifier this row scores.
	ItemID string `json:"item_id"`
	// Seed is the RNG seed the run was paired on (same seed across arms = a paired
	// contrast). Spec §5.1.
	Seed int64 `json:"seed"`
	// Substrate is the thinking-substrate provenance tag the arm ran on ("test",
	// "llm:<model>", "cc:session"). Rows from different substrates must never be
	// compared as one dataset (CLAUDE.md substrate hygiene); the tag makes mixing
	// detectable. Empty on rows written before the tag existed.
	Substrate string `json:"substrate,omitempty"`
	// RawOutput is the model/arm's raw answer text — kept for audit and rubric
	// replay (spec §5.7 "raw checker output").
	RawOutput string `json:"raw_output"`
	// OracleVerdict is the deterministic answer-oracle's own result (separate from
	// the isolation guard). Spec §5.2, §5.7.
	OracleVerdict bool `json:"oracle_verdict"`
	// IsolationResult is whether the isolation predicate witnessed genuine mechanism
	// use; a pass with IsolationResult=false is a mechanism-bypass, excluded from
	// the lift numerator. Spec §1.4, §3.2, §5.7.
	IsolationResult bool `json:"isolation_result"`
	// EventsPointer locates the full event trace for this run (path+offset or run
	// id) so the ledger row stays small. Spec §5.7.
	EventsPointer string `json:"events_pointer,omitempty"`
	// CheckerVersion is the hash of the checker (oracle / trace predicate / rubric)
	// that produced this row. A re-characterized checker mints a new hash; rows on
	// an old hash are invalidated (never deleted) when that happens. Spec §4.7, §5.7.
	CheckerVersion string `json:"checker_version"`
	// Status is the row's lifecycle: active or invalidated. Born active; only ever
	// flipped to invalidated by an appended invalidation marker. Spec §4.7.
	Status Status `json:"status"`

	// --- Invalidation-marker fields (Kind == KindInvalidation only) ---

	// InvalidatedByVersion is the NEW checker version whose arrival invalidated the
	// dependent rows. Rows on any OTHER version (older than this marker, as of Tick)
	// are flipped to invalidated. Empty on measurement/verdict rows.
	InvalidatedByVersion string `json:"invalidated_by_version,omitempty"`

	// --- Verdict-row fields (Kind == KindVerdict only) ---

	// IterK is the campaign iteration this verdict was recorded for (spec §4.6
	// iter-0-baseline-gated; "is mechanism M validated at iteration k").
	IterK int `json:"iter_k,omitempty"`
	// KeepVerdict is the keep-rule outcome for (Mechanism, Tier) at IterK: "keep"
	// (all four §4.6 conditions held) or "flag" (a partial / not-yet-validated
	// result). Empty on non-verdict rows. Spec §4.6.
	KeepVerdict string `json:"keep_verdict,omitempty"`
	// Contrast bundles the recorded effect estimates behind the verdict (harness−bare,
	// gate-on−gate-off, isolation rate) so the report can render them. Spec §1.4, §4.6.
	Contrast *types.Contrast `json:"contrast,omitempty"`
	// RawP is the raw (uncorrected) p-value of the mechanism-specific contrast
	// (gate-on−gate-off) as computed by the stats layer (McNemar / Wilcoxon, spec
	// §5.6). The report renders it next to the BH-corrected value. 0 means unset.
	RawP float64 `json:"raw_p,omitempty"`
	// BHP is the Benjamini–Hochberg-corrected p-value across the campaign's
	// many-batch comparisons (spec §4.6 condition 1, §7.4). The keep rule reads BH
	// significance; the report renders it. 0 means unset.
	BHP float64 `json:"bh_p,omitempty"`
	// IsolationFloor is the mechanism's isolation-rate floor (spec §4.6 condition 2),
	// recorded with the verdict so the report can show whether the rate cleared it.
	IsolationFloor float64 `json:"isolation_floor,omitempty"`
	// MDE is the pre-registered minimum detectable effect for this contrast (spec
	// §4.5/§4.6), recorded so the report can show "CI-lower vs MDE".
	MDE float64 `json:"mde,omitempty"`
}

// Verdict outcome string values for KeepVerdict (spec §4.6).
const (
	// VerdictKeep — the mechanism's machinery is KEPT: BH-significant, BCa-lower >
	// MDE, isolation floor cleared, no regression floor breached, beats best-so-far.
	VerdictKeep = "keep"
	// VerdictFlag — a reportable partial / not-yet-validated result (e.g.
	// "generic-scaffolding win, not this mechanism", or a futility-stopped bank).
	// Recorded, never a silent pass. Spec §4.6, §4.7.
	VerdictFlag = "flag"
)

// Store is a handle to one append-only ledger directory. It holds no in-memory
// row cache: Append writes through to disk immediately (so a crash mid-campaign
// loses nothing already appended) and Load re-reads the file. This keeps the
// "the file is the source of truth" invariant — there is no shadow state to drift.
type Store struct {
	dir  string
	path string
}

// Open opens (creating the directory if needed) the ledger at dir. It does not
// truncate or rewrite an existing ledger — an existing ledger.jsonl is left
// exactly as found, ready to be appended to. Returns an error only if the
// directory cannot be created.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("ledger: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ledger: mkdir %q: %w", dir, err)
	}
	return &Store{dir: dir, path: filepath.Join(dir, ledgerFile)}, nil
}

// Path returns the absolute-or-relative path of the underlying JSONL file (useful
// for an EventsPointer or a report header).
func (s *Store) Path() string { return s.path }

// Append writes ONE record as a single JSON line, O_APPEND so it can never
// overwrite or reorder a prior line (spec §5.7 append-only). A measurement row
// with an empty Kind defaults to KindMeasurement; an empty Status defaults to
// StatusActive (a row is born active). Append never mutates the file's existing
// contents.
func (s *Store) Append(r Record) error {
	if r.Kind == "" {
		r.Kind = KindMeasurement
	}
	if r.Status == "" {
		r.Status = StatusActive
	}
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("ledger: marshal: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("ledger: open %q: %w", s.path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("ledger: write: %w", err)
	}
	return nil
}

// Load reads every row in file (append) order. It is best-effort on a malformed
// line in the spirit of the rest of the repo (a corrupt line is skipped, never a
// crash) but returns the scanner error if the read itself fails. The returned
// slice is the RAW history — every appended row, including superseded measurement
// rows and the invalidation markers themselves. Use Resolved to apply the markers
// and get each row's effective Status.
func (s *Store) Load() ([]Record, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // an unopened ledger is an empty history, not an error.
		}
		return nil, fmt.Errorf("ledger: open %q: %w", s.path, err)
	}
	defer f.Close()
	return loadFrom(f)
}

func loadFrom(rd io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []Record
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue // best-effort: skip a malformed line, keep the rest of the audit.
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// Invalidate APPENDS invalidation markers (it never mutates or deletes a prior
// line) recording that, as of tick, the checker has been re-characterized to
// checkerVersion. Every currently-active MEASUREMENT row whose CheckerVersion is
// DIFFERENT from checkerVersion is dependent on the old checker and is marked
// invalidated — by appending one KindInvalidation row that names the superseding
// version and carries the (mechanism, tier, arm, item, batch, old version) of the
// row it supersedes, so the audit shows exactly what was invalidated and by what.
//
// This is the invalidate-not-delete discipline of spec §4.7 / §5.7: the original
// measurement rows remain on disk byte-for-byte; Resolved/IsValidated read the
// markers to compute the effective Status. Calling Invalidate twice for the same
// version is idempotent (rows already on checkerVersion are never invalidated;
// rows already superseded are not re-marked).
//
// Returns the number of measurement rows newly invalidated.
func (s *Store) Invalidate(checkerVersion string, tick int) (int, error) {
	if checkerVersion == "" {
		return 0, fmt.Errorf("ledger: invalidate with empty checker version")
	}
	rows, err := s.Load()
	if err != nil {
		return 0, err
	}
	eff := effectiveStatus(rows)

	n := 0
	for i, r := range rows {
		if r.Kind != KindMeasurement {
			continue
		}
		if r.CheckerVersion == checkerVersion {
			continue // rows already on the new checker are not dependents.
		}
		if eff[i] != StatusActive {
			continue // already invalidated — don't double-mark (idempotent).
		}
		marker := Record{
			Kind:                 KindInvalidation,
			Tick:                 tick,
			BatchID:              r.BatchID,
			Mechanism:            r.Mechanism,
			Tier:                 r.Tier,
			Arm:                  r.Arm,
			ItemID:               r.ItemID,
			Seed:                 r.Seed,
			CheckerVersion:       r.CheckerVersion, // the OLD version being superseded.
			InvalidatedByVersion: checkerVersion,   // the NEW version.
			Status:               StatusInvalidated,
		}
		if err := s.Append(marker); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// effectiveStatus computes, for each row index in rows, the row's status AFTER
// applying every invalidation marker — without touching disk. A measurement row
// is invalidated iff a later KindInvalidation marker matches it on the identifying
// tuple (mechanism, tier, arm, item, batch, checker-version). The markers
// themselves and verdict rows carry their own stored Status. This is the read-side
// of invalidate-not-delete: the raw rows are immutable; the effective view is
// derived.
func effectiveStatus(rows []Record) []Status {
	// Index: which (mechanism,tier,arm,item,batch,version) tuples have been
	// invalidated by a marker. Order does not matter for the boolean "was it ever
	// invalidated", and a marker only ever appears after the row it supersedes
	// (append-only), so a single pass collecting markers then resolving rows is
	// correct and deterministic.
	invalidated := map[string]bool{}
	for _, r := range rows {
		if r.Kind == KindInvalidation {
			invalidated[depKey(r)] = true
		}
	}
	out := make([]Status, len(rows))
	for i, r := range rows {
		switch r.Kind {
		case KindMeasurement:
			if invalidated[depKey(r)] {
				out[i] = StatusInvalidated
			} else {
				out[i] = StatusActive
			}
		default:
			// invalidation markers and verdict rows carry their stored status.
			if r.Status == "" {
				out[i] = StatusActive
			} else {
				out[i] = r.Status
			}
		}
	}
	return out
}

// depKey is the identifying tuple a measurement row and its invalidation marker
// share. CheckerVersion is part of the key so a marker for version X invalidates
// only the rows actually on version X (and re-running on a fresh version is not
// retro-invalidated).
func depKey(r Record) string {
	return fmt.Sprintf("%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s",
		r.Mechanism, r.Tier, r.Arm, r.ItemID, r.BatchID, r.CheckerVersion)
}

// Resolved returns the full history paired with each row's effective Status after
// all invalidation markers are applied. The raw Record is unchanged on disk; the
// returned Record's Status field is overwritten with the effective value so a
// caller can filter on r.Status == StatusActive directly.
func (s *Store) Resolved() ([]Record, error) {
	rows, err := s.Load()
	if err != nil {
		return nil, err
	}
	eff := effectiveStatus(rows)
	out := make([]Record, len(rows))
	for i, r := range rows {
		r.Status = eff[i]
		out[i] = r
	}
	return out, nil
}

// IsValidated answers "is mechanism M validated at iteration k?" by reading ONLY
// the ledger (spec §5.7: the single source of truth). It looks for the LATEST
// recorded keep-rule verdict (KindVerdict) for the mechanism at iteration iterK
// and returns true iff that verdict is VerdictKeep and the verdict row is itself
// still active (a verdict whose evidence was invalidated does not count).
//
// "Latest" is by append order then by Tick — the last word wins, which lets a
// later iteration's re-run overturn an earlier keep without deleting the history
// (invalidate-not-delete at the verdict layer too). If no verdict has been
// recorded for (mechanism, iterK), it returns false (not-yet-validated is the
// safe default).
func (s *Store) IsValidated(mechanism types.Mechanism, iterK int) (bool, error) {
	rows, err := s.Resolved()
	if err != nil {
		return false, err
	}
	found := false
	verdict := ""
	for _, r := range rows {
		if r.Kind != KindVerdict {
			continue
		}
		if r.Mechanism != mechanism || r.IterK != iterK {
			continue
		}
		if r.Status != StatusActive {
			continue // an invalidated verdict row is not the current word.
		}
		// Append order is campaign order; the last matching active verdict wins.
		found = true
		verdict = r.KeepVerdict
	}
	if !found {
		return false, nil
	}
	return verdict == VerdictKeep, nil
}

// VerdictInput is the keep-rule verdict the campaign records for a (mechanism,
// tier) at an iteration (spec §4.6). It bundles the verdict outcome, the effect
// estimates behind it, and the statistics (raw + BH p) the report renders.
type VerdictInput struct {
	Tick           int
	Mechanism      types.Mechanism
	Tier           types.Tier
	IterK          int
	KeepVerdict    string // VerdictKeep | VerdictFlag
	Contrast       *types.Contrast
	RawP           float64
	BHP            float64
	IsolationFloor float64
	MDE            float64
}

// RecordVerdict is the convenience for appending a keep-rule verdict row (spec
// §4.6). It is the write counterpart IsValidated reads. The verdict row is born
// active; a later iteration can supersede it by appending a fresh verdict for the
// same (mechanism, iterK), and the keep rule's evidence can be invalidated via
// Invalidate exactly like a measurement row.
func (s *Store) RecordVerdict(v VerdictInput) error {
	if v.KeepVerdict != VerdictKeep && v.KeepVerdict != VerdictFlag {
		return fmt.Errorf("ledger: invalid keep verdict %q (want %q or %q)", v.KeepVerdict, VerdictKeep, VerdictFlag)
	}
	return s.Append(Record{
		Kind:           KindVerdict,
		Tick:           v.Tick,
		Mechanism:      v.Mechanism,
		Tier:           v.Tier,
		IterK:          v.IterK,
		KeepVerdict:    v.KeepVerdict,
		Contrast:       v.Contrast,
		RawP:           v.RawP,
		BHP:            v.BHP,
		IsolationFloor: v.IsolationFloor,
		MDE:            v.MDE,
		Status:         StatusActive,
	})
}

// ActiveMeasurements returns every measurement row whose effective Status is
// active — the rows a fresh lift computation should consume. Helper for the
// stats / keep-rule layer; deterministic in append order.
func (s *Store) ActiveMeasurements() ([]Record, error) {
	rows, err := s.Resolved()
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, r := range rows {
		if r.Kind == KindMeasurement && r.Status == StatusActive {
			out = append(out, r)
		}
	}
	return out, nil
}
