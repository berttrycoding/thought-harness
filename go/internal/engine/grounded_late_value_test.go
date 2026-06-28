package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// TestRung4LateValueSurvivesSummary is the A1 "voicing-stability" residual: the prior grounded-but-not-
// voiced fix lands the OBSERVATION thought in g.History(), but that thought's text is the SUMMARISED tool
// result (action.SummarizeToolResult), which CLIPS read_file output to 240 runes and search to 6 hits /
// 320 runes. When the grounded value sits LATE in a real file (past the 240-rune cap — a real config file
// is longer than a one-line fixture), the summary truncates the value out of the scored surface, so a
// genuinely grounded episode still scores FALSE ~2/3 of the time on the live model (run.out: grounded 3/3,
// solved 1/3). This is the offline-fixable half of A1: the value IS imported into reality, but a clip on
// the observation->thought path removes it before voicing/scoring read it.
//
// The fixture puts "ActionMargin: 1.5" at byte offset ~300 (past the 240-rune read_file clip) inside a
// realistic file. The PROPERTY: the imported value must reach a thought in g.History() — the clip must not
// drop a late-but-grounded value off the scored surface.
func TestRung4LateValueSurvivesSummary(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn => watched_sync enabled, real executor
	ws := e.cfg.Workspace

	// A realistic config file: the asked-for const sits AFTER ~300 runes of preamble (the late-value
	// shape a real source file has — the one-line fixture in grounded_voicing_test.go hides this clip).
	var b strings.Builder
	b.WriteString("package regulator\n\n")
	b.WriteString("// regulator.go enforces the homeostatic durability regime over the generative\n")
	b.WriteString("// subconscious: subcritical excitation n<1, positive baseline mu>0, schedulable\n")
	b.WriteString("// U<=1, a stable controller 0<K*g<2, and a bounded async-action dead-time. These\n")
	b.WriteString("// constants are the phase-margin proxies the stability suite measures.\n\n")
	b.WriteString("const (\n")
	b.WriteString("\tActionMargin = 1.5 // phase-margin proxy for the async dead-time bound\n")
	b.WriteString(")\n")
	content := b.String()
	// sanity: the value really is past the 240-rune read_file clip (so a passing test PROVES the clip
	// no longer drops it, and a pre-fix run FAILS for the right reason).
	if idx := strings.Index(content, "1.5"); idx <= 240 {
		t.Fatalf("test fixture broken: the value must sit past the 240-rune clip, got offset %d", idx)
	}
	if err := os.WriteFile(filepath.Join(ws, "regulator.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	e.startEpisode("In this Go codebase, find the value assigned to ActionMargin.", true)

	rs := &realitySourcer{e}
	need := subconscious.FuelNeed{Query: "read regulator.go", Kind: "fact", AllowReality: true}
	text, ok, grounds, _ := rs.SourceReality(need)
	if !ok || !grounds {
		t.Fatalf("rung-4 should source a grounded reality observation (ok=%v grounds=%v, text=%q)", ok, grounds, text)
	}

	// THE PROPERTY: the imported value must reach a thought in g.History() — the SCORED surface
	// (scoreSolvedEngine scans th.Text). Before the fix the observation thought carries only the clipped
	// 240-rune summary, which ends before "ActionMargin = 1.5", so this FAILS even though grounds=true.
	found := false
	var seen []string
	for _, th := range e.graph.History() {
		seen = append(seen, fmt.Sprintf("[%s] %q", th.Source, th.Text))
		if strings.Contains(th.Text, "1.5") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the LATE grounded value 1.5 never reached a thought in g.History() — A1 voicing-stability "+
			"clip gap (SummarizeToolResult truncated it off the scored surface).\nhistory:\n%s",
			strings.Join(seen, "\n"))
	}
}
