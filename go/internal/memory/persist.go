// persist.go is the cross-session persistence of the declarative memory stores (P7.1): episodes and
// beliefs as append-only JSONL (one record per line), per the persistence seam in
// reports/2026-06-07-memory-system-audit.md §4. Bi-temporal belief invalidations ride along as the
// ValidTo field on each row, so an overturned belief reconstructs exactly (append-only, invalidate-not-
// delete). Load is best-effort — a malformed line is skipped, never a crash — and only grounded records
// are re-admitted (never-fabricate holds across a restart). Persistence is opt-in, so tests stay
// deterministic.
package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

func jsonlScanner(rd io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return sc
}

// Save writes every stored episode as one JSON line.
func (r *EpisodicRegistry) Save(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, e := range r.episodes {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// Load reads episodes (one JSON object per line) and re-Records each (so never-fabricate is re-applied:
// an ungrounded row is rejected). Best-effort; returns how many were loaded.
func (r *EpisodicRegistry) Load(rd io.Reader) (int, error) {
	sc := jsonlScanner(rd)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Episode
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if r.Record(e) {
			n++
		}
	}
	return n, sc.Err()
}

// Save writes every belief (including invalidated ones, with their ValidTo) as one JSON line, so the
// bi-temporal history reconstructs exactly.
func (r *SemanticRegistry) Save(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, b := range r.beliefs {
		if err := enc.Encode(b); err != nil {
			return err
		}
	}
	return nil
}

// Load reads beliefs and re-admits the grounded ones (never-fabricate), preserving ValidFrom/ValidTo so
// invalidations survive the restart. Best-effort; returns how many were loaded.
func (r *SemanticRegistry) Load(rd io.Reader) (int, error) {
	sc := jsonlScanner(rd)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var b Belief
		if err := json.Unmarshal(line, &b); err != nil {
			continue
		}
		if !b.Grounded { // never-fabricate survives the restart
			continue
		}
		r.beliefs = append(r.beliefs, b) // preserve ValidFrom/ValidTo exactly (don't reset via Record)
		n++
	}
	return n, sc.Err()
}
