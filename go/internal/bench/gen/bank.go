package gen

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// PilotBanksRoot is the canonical on-disk root of the authored pilot banks
// (one JSONL file per mechanism per tier). It is relative to the Go module root
// (go/) so a caller from cmd/thought resolves it directly; tests pass an explicit
// root to a temp dir instead. Spec §5.4 (the mined-shape-seeded gold the
// generator few-shots from).
const PilotBanksRoot = "internal/bench/banks/pilot"

// maxScanLine is the per-line buffer cap for the JSONL scanner — large enough for
// an item that embeds an Artifact.Materialization ([]byte) or a multi-turn
// scenario with planted payloads (the tiera loader uses the same 8 MiB cap).
const maxScanLine = 8 * 1024 * 1024

// BankFileA returns the canonical filename of a Tier-A bank for one mechanism
// under root, e.g. "<root>/grounding-tiera.jsonl". The name encodes mechanism +
// tier so a banks directory is self-describing (spec §5.4).
func BankFileA(root string, m benchtypes.Mechanism) string {
	return filepath.Join(root, fmt.Sprintf("%s-tiera.jsonl", m))
}

// BankFileB returns the canonical filename of a Tier-B bank for one mechanism
// under root, e.g. "<root>/grounding-tierb.jsonl".
func BankFileB(root string, m benchtypes.Mechanism) string {
	return filepath.Join(root, fmt.Sprintf("%s-tierb.jsonl", m))
}

// SaveBankA writes a slice of Tier-A items to a JSONL file (one item per line),
// creating the parent directory if it does not exist. The output is the exact
// wire format tiera.LoadItems reads back. An empty slice writes an empty file
// (a valid, zero-item bank) rather than erroring (spec §5.2, §5.4).
func SaveBankA(path string, items []benchtypes.TierAItem) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("gen: mkdir for bank %q: %w", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("gen: create bank %q: %w", path, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for i, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			return fmt.Errorf("gen: marshal item %d (%q): %w", i, it.ID, err)
		}
		if _, err := w.Write(b); err != nil {
			return fmt.Errorf("gen: write item %d (%q): %w", i, it.ID, err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("gen: write newline after item %d (%q): %w", i, it.ID, err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("gen: flush bank %q: %w", path, err)
	}
	return nil
}

// LoadBankA reads a JSONL Tier-A bank into memory, skipping blank lines and
// failing loud (with the 1-based line number) on a malformed line so a bad bank
// is debuggable. A missing file is reported as a wrapped error (callers that
// treat "no bank yet" as empty should check os.IsNotExist). Spec §5.2.
func LoadBankA(path string) ([]benchtypes.TierAItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("gen: open Tier-A bank %q: %w", path, err)
	}
	defer f.Close()
	return loadBankAReader(f)
}

func loadBankAReader(r io.Reader) ([]benchtypes.TierAItem, error) {
	var items []benchtypes.TierAItem
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxScanLine)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var it benchtypes.TierAItem
		if err := json.Unmarshal([]byte(raw), &it); err != nil {
			return nil, fmt.Errorf("gen: malformed Tier-A item on line %d: %w", line, err)
		}
		items = append(items, it)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("gen: read Tier-A bank: %w", err)
	}
	return items, nil
}

// SaveBankB writes a slice of Tier-B scenarios to a JSONL file (one scenario per
// line), creating the parent directory if needed. The output is the wire format
// the tierb loader reads back. Spec §5.3, §5.4.
func SaveBankB(path string, scenarios []benchtypes.TierBScenario) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("gen: mkdir for bank %q: %w", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("gen: create bank %q: %w", path, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for i, sc := range scenarios {
		b, err := json.Marshal(sc)
		if err != nil {
			return fmt.Errorf("gen: marshal scenario %d (%q): %w", i, sc.ID, err)
		}
		if _, err := w.Write(b); err != nil {
			return fmt.Errorf("gen: write scenario %d (%q): %w", i, sc.ID, err)
		}
		if err := w.WriteByte('\n'); err != nil {
			return fmt.Errorf("gen: write newline after scenario %d (%q): %w", i, sc.ID, err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("gen: flush bank %q: %w", path, err)
	}
	return nil
}

// LoadBankB reads a JSONL Tier-B bank into memory, skipping blank lines and
// failing loud on a malformed line. Spec §5.3.
func LoadBankB(path string) ([]benchtypes.TierBScenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("gen: open Tier-B bank %q: %w", path, err)
	}
	defer f.Close()
	return loadBankBReader(f)
}

func loadBankBReader(r io.Reader) ([]benchtypes.TierBScenario, error) {
	var scenarios []benchtypes.TierBScenario
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxScanLine)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var s benchtypes.TierBScenario
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, fmt.Errorf("gen: malformed Tier-B scenario on line %d: %w", line, err)
		}
		scenarios = append(scenarios, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("gen: read Tier-B bank: %w", err)
	}
	return scenarios, nil
}
