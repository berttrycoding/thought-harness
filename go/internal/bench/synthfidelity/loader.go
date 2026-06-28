package synthfidelity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadFixtures reads a JSONL file of synth-fidelity fixtures (one Fixture per line)
// into memory. Blank lines are skipped so a hand-edited bank with trailing newlines
// loads cleanly; a malformed line fails loud with its 1-based line number so a bad
// bank is debuggable (mirrors tiera.LoadItems).
func LoadFixtures(path string) ([]Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("synthfidelity: open fixture bank %q: %w", path, err)
	}
	defer f.Close()
	return LoadFixturesReader(f)
}

// LoadFixturesReader is the io.Reader form of LoadFixtures (so a test can load from
// an embedded fixture or a string without a temp file). Each non-blank line must
// decode to one Fixture.
func LoadFixturesReader(r io.Reader) ([]Fixture, error) {
	var out []Fixture
	sc := bufio.NewScanner(r)
	// A fixture embeds two program dicts; raise the per-line cap above bufio's 64 KiB
	// default so a deep program tree does not trip the scanner.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var fx Fixture
		if err := json.Unmarshal([]byte(raw), &fx); err != nil {
			return nil, fmt.Errorf("synthfidelity: malformed fixture on line %d: %w", line, err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("synthfidelity: read fixture bank: %w", err)
	}
	return out, nil
}
