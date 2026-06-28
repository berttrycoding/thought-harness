// persist.go is the cross-session persistence of the durable knowledge store (mirrors memory/persist.go):
// knowledge items as append-only JSONL (one record per line). Bi-temporal invalidations ride along as
// the ValidTo field on each row, so a refuted item reconstructs exactly (append-only, invalidate-not-
// delete). Load is best-effort — a malformed line is skipped, never a crash — and only GROUNDED records
// are re-admitted (never-fabricate holds across a restart). Persistence is opt-in (M4 wires the store),
// so tests stay deterministic.
package knowledge

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

// Save writes every knowledge item (including invalidated ones, with their ValidTo) as one JSON line,
// so the bi-temporal history reconstructs exactly.
func (r *KnowledgeRegistry) Save(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, k := range r.items {
		if err := enc.Encode(k); err != nil {
			return err
		}
	}
	return nil
}

// Load reads knowledge items and re-admits the grounded ones (never-fabricate survives the restart),
// preserving ValidFrom/ValidTo exactly so invalidations survive. Best-effort; returns how many loaded.
// It re-admits rows WITHOUT going through Record (so it does not re-emit knowledge.record on every
// restart and preserves the bi-temporal fields verbatim — the same discipline as memory's Load).
func (r *KnowledgeRegistry) Load(rd io.Reader) (int, error) {
	sc := jsonlScanner(rd)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var k Knowledge
		if err := json.Unmarshal(line, &k); err != nil {
			continue
		}
		if !k.Grounded { // never-fabricate survives the restart
			continue
		}
		r.items = append(r.items, k)
		n++
	}
	return n, sc.Err()
}
