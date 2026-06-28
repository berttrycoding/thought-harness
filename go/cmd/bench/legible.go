package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/legible"
)

// legibleReport is the --legible-report mode: a read-only telemetry ROLLUP over a JSONL EVENT LOG (the
// file `thought run/scenario/tui --log FILE.jsonl` writes, or any stream of trace.JsonlSink records). It
// folds the legible.* events in that log into the WF-E CC-1 part-3 rollup (fast-path hit rate, novel-tag
// histogram, per-seam parity) and prints the report — it runs NO benchmark campaign. This is how you read
// the three scaling numbers AFTER a real-model run with seam.legible_generation ON.
//
// The log is the GOLDEN-WRITER shape: one JSON record per line, {tick,kind,layer,summary,data}. We decode
// just enough (kind + data) to feed the rollup; a malformed/blank line is skipped (a crashed run leaves a
// valid prefix + maybe one torn line — we tolerate it rather than abort the rollup). topN caps the
// histogram rows (0 = all).
func legibleReport(logPath string, topN int) error {
	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open event log %q: %w", logPath, err)
	}
	defer f.Close()

	roll, n, err := rollupFromLog(f)
	if err != nil {
		return fmt.Errorf("read event log %q: %w", logPath, err)
	}
	progressf("bench: legible-report — scanned %d event(s) from %s\n", n, logPath)
	fmt.Print(roll.Report(topN))
	return nil
}

// logRecord is the minimal decode of a trace.JsonlSink line: only the kind + data drive the rollup. The
// other golden fields (tick/layer/summary) are ignored here. A bufio.Scanner over the file gives one
// record per line; the default 64KB line cap is raised because an LLM-call record (full prompt + raw
// response) can be large — but legible.* records are small, so this only matters for mixed logs.
type logRecord struct {
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
}

// rollupFromLog folds a JSONL event-log stream into a legible.Rollup. It returns the rollup, the count of
// records scanned (for the progress line), and any read error. A line that fails to decode is SKIPPED
// (not fatal) so a single torn line in a crashed run's log cannot drop the whole rollup.
func rollupFromLog(r io.Reader) (*legible.Rollup, int, error) {
	roll := legible.NewRollup()
	sc := bufio.NewScanner(r)
	// Raise the line cap: a mixed log can carry a large llm.call record; legible.* records are small but
	// the scanner must not choke on a big neighbour line and stop early.
	const maxLine = 8 << 20 // 8 MiB
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)

	scanned := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // tolerate a torn/blank line — a crashed run leaves a valid prefix
		}
		scanned++
		roll.Observe(events.Event{Kind: rec.Kind, Data: rec.Data})
	}
	if err := sc.Err(); err != nil {
		return roll, scanned, err
	}
	return roll, scanned, nil
}
