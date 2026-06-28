package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/bench/synthfidelity"
)

// synthFidelityReport is the --synth-fidelity mode (A5, Track A,
// docs/internal/notes/2026-06-16-registry-target-spec.md §1/§3): it loads the goal->
// expected-synthesis fixtures at bankPath, drives the REAL synthesiser
// (cognition.Synthesize) over each goal OFFLINE + DETERMINISTICALLY (a TestBackend
// wired to RecognizeShapeDict — no model, no network), scores the produced program
// against the fixture's expected STRUCTURE with the deterministic oracle, and prints
// a per-worker fidelity report. It runs NO bench campaign, no arms, no model — it
// scores the synthesiser's CONSTRUCTION directly, so a miss is a precise, rankable
// capability gap.
//
// The report flags DRIFT (a fixture whose measured fidelity disagrees with its
// authored synthesiser_covers expectation) and exits NON-ZERO on any drift, so the
// bank cannot silently rot as the synthesiser's capability moves.
func synthFidelityReport(bankPath string) error {
	fixtures, err := synthfidelity.LoadFixtures(bankPath)
	if err != nil {
		return err
	}
	if len(fixtures) == 0 {
		return fmt.Errorf("synth-fidelity bank %q is empty", bankPath)
	}

	results := synthfidelity.DriveAll(fixtures, synthfidelity.DefaultWeights())

	var b strings.Builder
	fmt.Fprintf(&b, "AGENT-SYNTHESIS-FIDELITY (A5) — %s\n", bankPath)
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 78))
	fmt.Fprintf(&b, "%-28s %-12s %-22s %6s %5s %5s\n", "fixture", "worker", "source", "score", "pass", "drift")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 78))

	// Per-worker rollup + global tallies.
	type wstat struct {
		n, faithful int
		sumScore    float64
	}
	byWorker := map[string]*wstat{}
	var workerOrder []string
	covered, gaps, drift, faithful := 0, 0, 0, 0

	for i, r := range results {
		fx := fixtures[i]
		ws := byWorker[r.Worker]
		if ws == nil {
			ws = &wstat{}
			byWorker[r.Worker] = ws
			workerOrder = append(workerOrder, r.Worker)
		}
		ws.n++
		ws.sumScore += r.Verdict.Score
		if r.Verdict.Pass {
			ws.faithful++
			faithful++
		}
		if fx.SynthesiserCovers {
			covered++
		} else {
			gaps++
		}
		driftMark := ""
		if r.Drift {
			drift++
			driftMark = "DRIFT"
		}
		fmt.Fprintf(&b, "%-28s %-12s %-22s %6.3f %5s %5s\n",
			trunc(r.ID, 28), trunc(r.Worker, 12), trunc(r.Source, 22), r.Verdict.Score, passMark(r.Verdict.Pass), driftMark)
	}

	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 78))
	fmt.Fprintf(&b, "PER-WORKER (faithful / total, mean structural fidelity):\n")
	sort.Strings(workerOrder)
	for _, wk := range workerOrder {
		ws := byWorker[wk]
		mean := 0.0
		if ws.n > 0 {
			mean = ws.sumScore / float64(ws.n)
		}
		fmt.Fprintf(&b, "  %-22s %d/%d faithful   mean=%.3f\n", wk, ws.faithful, ws.n, mean)
	}

	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 78))
	fmt.Fprintf(&b, "TOTAL: %d fixtures | %d faithful (real synthesis matched the expected structure) | "+
		"%d authored-covered + %d KNOWN GAPS\n", len(results), faithful, covered, gaps)
	if drift > 0 {
		fmt.Fprintf(&b, "DRIFT: %d fixture(s) — the measured fidelity disagrees with the authored "+
			"synthesiser_covers flag; the bank must be revisited.\n", drift)
	} else {
		fmt.Fprintf(&b, "DRIFT: none — every fixture's measured fidelity matches its authored expectation.\n")
	}

	fmt.Print(b.String())

	if drift > 0 {
		return fmt.Errorf("synth-fidelity: %d fixture(s) drifted from the authored expectation", drift)
	}
	return nil
}

// passMark renders a compact pass/fail token for the report column.
func passMark(pass bool) string {
	if pass {
		return "yes"
	}
	return "no"
}

// trunc truncates s to at most n runes for a fixed-width report column.
func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "~"
}
