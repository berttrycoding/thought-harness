// Package external is the FORMAT ADAPTER that ingests an EXTERNAL benchmark bank
// (an ARC-AGI-2 / GAIA public-JSON-shaped file) and converts it into the INTERNAL
// realhard task+oracle shape, so the EXISTING bare-vs-harness arm + realhard runner
// (internal/bench/realhard) can consume an external bank with zero new runner code.
//
// WHY IT EXISTS. The SOTA-benchmark-suite design (docs/internal/2026-06-21-sota-
// benchmark-suite.md §7.1 "bank ingestion") adopts a small set of named external banks
// (ARC-AGI-2, GAIA, ...) that must each become a bank the realhard A/B can run. Rather
// than re-implement each bank's loader, this is ONE adapter over a COMMON external
// JSON envelope: a bank file declares its items, each item declares its grader kind,
// and this maps the envelope onto realhard.Task + realhard.OracleKind. The realhard
// oracle (exact / numeric-tolerance / set-membership / decline) is the four-way grader;
// an external item picks one. No network, no model — pure decode + map (CLAUDE.md
// determinism: a bank is a checked-in file or a caller-supplied reader).
//
// ============================================================================
// THE EXTERNAL JSON SCHEMA (the envelope this adapter expects)
// ============================================================================
//
// A bank is ONE JSON object:
//
//	{
//	  "schema":  "thought-external-bank/v1",   // REQUIRED, exact (version gate)
//	  "source":  "arc-agi-2" | "gaia" | ...,   // provenance tag (free text)
//	  "id_prefix": "arc",                       // OPTIONAL stable ID namespace
//	  "items": [ <item>, <item>, ... ]          // REQUIRED, >=1
//	}
//
// Each <item> (one task):
//
//	{
//	  "id":         "arc-agi-2-eval-007a",   // REQUIRED, unique within the bank
//	  "capability": "multi-hop-grounding"    // OPTIONAL realhard.Capability; default by source
//	                | "adaptive-backtracking"
//	                | "anti-confabulation"
//	                | "long-horizon-consistency",
//	  "prompt":     "<the exact request fed to both arms>",  // REQUIRED, non-empty
//	  "files":      { "rel/path.txt": "<contents>", ... },   // OPTIONAL workspace materials
//	  "grader":     "exact" | "numeric-tolerance"            // REQUIRED
//	                | "set-membership" | "decline",
//	  "expected":   "<ground-truth>",        // REQUIRED unless grader=="decline"
//	                                         //   exact:          one token/number
//	                                         //   numeric-tol:    a number
//	                                         //   set-membership: members joined by spaces
//	                                         //   decline:        omitted/empty
//	  "normalizer": "number" | "token" | "lower" | "",  // OPTIONAL exact normalizer
//	  "tolerance":  0.5,                     // OPTIONAL numeric-tolerance absolute tol
//	  "lure":       "<the confident wrong answer>",     // OPTIONAL prior-lure
//	  "why":        "<one-line difficulty note>",       // OPTIONAL doc note
//	  "human_min":  20.0                     // OPTIONAL est. skilled-human minutes (METR x-axis)
//	}
//
// ARC-AGI-2 mapping (grid puzzles): the canonical answer is the output GRID serialized
// as a deterministic token string (e.g. rows joined by ';', cells by ','); grader =
// "exact", normalizer = "token". The serialization is the bank author's job (do it the
// SAME way for the prompt's expected-output format and for "expected") — this adapter
// scores the strings the bank gives it; it does not parse grids.
//
// GAIA mapping (general-assistant exact-answer): grader = "exact" (normalizer "lower"
// for free-text, "number" for numeric answers) or "numeric-tolerance"; "files" carries
// any attached material the harness read-tools ground against.
package external

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/bench/realhard"
)

// SchemaV1 is the exact `schema` string this adapter ingests. A bank with any other
// schema is rejected loudly (a version gate so a future incompatible envelope can be
// added beside this without silently mis-mapping).
const SchemaV1 = "thought-external-bank/v1"

// Bank is the decoded external bank envelope.
type Bank struct {
	Schema   string `json:"schema"`
	Source   string `json:"source"`
	IDPrefix string `json:"id_prefix"`
	Items    []Item `json:"items"`
}

// Item is one external task as it appears in the bank file.
type Item struct {
	ID         string            `json:"id"`
	Capability string            `json:"capability"`
	Prompt     string            `json:"prompt"`
	Files      map[string]string `json:"files"`
	Grader     string            `json:"grader"`
	Expected   string            `json:"expected"`
	Normalizer string            `json:"normalizer"`
	Tolerance  float64           `json:"tolerance"`
	Lure       string            `json:"lure"`
	Why        string            `json:"why"`
	HumanMin   float64           `json:"human_min"`
}

// LoadFile reads, decodes, and converts an external bank file at path into the internal
// realhard task shape. It is the convenience wrapper over LoadReader for a checked-in
// fixture or a caller-supplied bank file.
func LoadFile(path string) ([]realhard.Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("external bank %s: %w", path, err)
	}
	defer f.Close()
	tasks, err := LoadReader(f)
	if err != nil {
		return nil, fmt.Errorf("external bank %s: %w", path, err)
	}
	return tasks, nil
}

// LoadReader decodes an external bank from r and converts every item into a
// realhard.Task. It is the testable core (a checked-in fixture is opened by LoadFile;
// a test drives this directly with a strings.Reader). It is strict: a schema mismatch,
// an empty item list, a duplicate ID, an unknown grader, or a missing required field is
// an ERROR — never a silently-dropped or defaulted-to-garbage task (a mis-mapped task
// would score a real run wrong and read as a false signal).
func LoadReader(r io.Reader) ([]realhard.Task, error) {
	var bank Bank
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields() // an unexpected key is a schema drift — fail loud, do not ignore.
	if err := dec.Decode(&bank); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return Convert(bank)
}

// Convert maps a decoded Bank into realhard tasks with full validation. Pure (no I/O):
// the deterministic decode->map step the round-trip test pins.
func Convert(bank Bank) ([]realhard.Task, error) {
	if bank.Schema != SchemaV1 {
		return nil, fmt.Errorf("schema %q is not %q (unsupported external bank version)", bank.Schema, SchemaV1)
	}
	if len(bank.Items) == 0 {
		return nil, fmt.Errorf("bank has no items")
	}
	seen := make(map[string]bool, len(bank.Items))
	tasks := make([]realhard.Task, 0, len(bank.Items))
	for i, it := range bank.Items {
		t, err := convertItem(bank, it)
		if err != nil {
			return nil, fmt.Errorf("item[%d] %q: %w", i, it.ID, err)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("item[%d]: duplicate id %q", i, t.ID)
		}
		seen[t.ID] = true
		tasks = append(tasks, t)
	}
	// stable order by ID so a run over a converted bank is deterministic regardless of the
	// file's item order (the realhard runner already preserves slice order).
	sort.SliceStable(tasks, func(a, b int) bool { return tasks[a].ID < tasks[b].ID })
	return tasks, nil
}

// convertItem maps ONE external item to a realhard.Task with field-level validation.
func convertItem(bank Bank, it Item) (realhard.Task, error) {
	id := strings.TrimSpace(it.ID)
	if id == "" {
		return realhard.Task{}, fmt.Errorf("missing id")
	}
	if p := strings.TrimSpace(bank.IDPrefix); p != "" && !strings.HasPrefix(id, p) {
		id = p + "-" + id
	}
	if strings.TrimSpace(it.Prompt) == "" {
		return realhard.Task{}, fmt.Errorf("missing prompt")
	}

	cap, err := mapCapability(it.Capability, bank.Source)
	if err != nil {
		return realhard.Task{}, err
	}
	oracle, err := mapGrader(it.Grader)
	if err != nil {
		return realhard.Task{}, err
	}

	t := realhard.Task{
		ID:         id,
		Capability: cap,
		Prompt:     it.Prompt,
		Materials:  it.Files,
		Oracle:     oracle,
		Expected:   strings.TrimSpace(it.Expected),
		Normalizer: it.Normalizer,
		Tolerance:  it.Tolerance,
		PriorLure:  it.Lure,
		Why:        it.Why,
		HumanMin:   it.HumanMin, // 0 -> realhard's per-capability heuristic; non-zero places it precisely on the METR axis
	}

	// per-grader requirements: a grader that scores against a ground truth MUST carry one
	// (a missing expected would silently pass/fail every answer). decline carries none.
	switch oracle {
	case realhard.OracleExact, realhard.OracleNumericTolerance, realhard.OracleSetMembership:
		if t.Expected == "" {
			return realhard.Task{}, fmt.Errorf("grader %q requires a non-empty expected", it.Grader)
		}
	case realhard.OracleDecline:
		// decline scores an honest non-confabulation; expected is irrelevant (cleared so a
		// stray value cannot read as a token to match).
		t.Expected = ""
	}
	// numeric graders need a positive tolerance to be meaningful; default a tight one so a
	// bank that omits it still scores exactly (0 tolerance = exact-equal numbers).
	if oracle == realhard.OracleNumericTolerance && t.Tolerance < 0 {
		return realhard.Task{}, fmt.Errorf("numeric-tolerance tolerance must be >= 0 (got %g)", t.Tolerance)
	}
	return t, nil
}

// mapCapability maps the item's capability string (or, when empty, a source default) to a
// realhard.Capability. An explicit unknown value is an error; an empty value falls back to
// the source's natural family (ARC-AGI-2 is fluid-novelty reasoning -> multi-hop-grounding
// as the closest realhard family; GAIA is multi-tool grounding -> multi-hop-grounding).
func mapCapability(s, source string) (realhard.Capability, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case string(realhard.CapMultiHopGrounding):
		return realhard.CapMultiHopGrounding, nil
	case string(realhard.CapAdaptiveBacktracking):
		return realhard.CapAdaptiveBacktracking, nil
	case string(realhard.CapAntiConfabulation):
		return realhard.CapAntiConfabulation, nil
	case string(realhard.CapLongHorizonConsistency):
		return realhard.CapLongHorizonConsistency, nil
	case "":
		// source default (the bank-level natural family for items that omit it).
		switch strings.TrimSpace(strings.ToLower(source)) {
		case "arc-agi-2", "arc", "gaia":
			return realhard.CapMultiHopGrounding, nil
		default:
			return realhard.CapMultiHopGrounding, nil
		}
	default:
		return "", fmt.Errorf("unknown capability %q (want multi-hop-grounding|adaptive-backtracking|anti-confabulation|long-horizon-consistency)", s)
	}
}

// mapGrader maps the item's grader string to a realhard.OracleKind. Unknown is an error
// (a silently-defaulted grader would mis-score the whole item).
func mapGrader(s string) (realhard.OracleKind, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case string(realhard.OracleExact):
		return realhard.OracleExact, nil
	case string(realhard.OracleNumericTolerance):
		return realhard.OracleNumericTolerance, nil
	case string(realhard.OracleSetMembership):
		return realhard.OracleSetMembership, nil
	case string(realhard.OracleDecline):
		return realhard.OracleDecline, nil
	default:
		return "", fmt.Errorf("unknown grader %q (want exact|numeric-tolerance|set-membership|decline)", s)
	}
}
