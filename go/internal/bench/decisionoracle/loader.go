package decisionoracle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadFixtures reads a JSONL bank of decision/ship fixtures (one Fixture per line).
// Blank lines are skipped; a malformed line fails loud with its 1-based line number
// (mirrors synthfidelity.LoadFixtures / tiera.LoadItems).
func LoadFixtures(path string) ([]Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("decisionoracle: open fixture bank %q: %w", path, err)
	}
	defer f.Close()
	return LoadFixturesReader(f)
}

// LoadFixturesReader is the io.Reader form of LoadFixtures (so a test can load from an
// embedded string without a temp file).
func LoadFixturesReader(r io.Reader) ([]Fixture, error) {
	var out []Fixture
	sc := bufio.NewScanner(r)
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
			return nil, fmt.Errorf("decisionoracle: malformed fixture on line %d: %w", line, err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("decisionoracle: read fixture bank: %w", err)
	}
	return out, nil
}
