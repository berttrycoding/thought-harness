package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/subconscious"
)

// TestRung4RealitySourceLandsValueInHistory is the A1 "grounded-but-not-voiced" cognition-property
// test: when the rung-4 reality sourcer crosses the watched seam, GROUNDS a real observation
// (grounds=true), and that observation carries an imported VALUE (e.g. a read/searched constant), the
// verbatim value must REACH a thought in g.History() — it must not flow on only as fuel into a model
// fusion that may paraphrase the literal value away. Before the fix the grounded observation was never
// appended (rung 4 only returned it as fuel), so a grounded episode could still score FALSE because no
// thought carried the value. After the fix the grounded observation is appended as an OBSERVATION
// thought, so the imported reality is voiceable/scoreable.
func TestRung4RealitySourceLandsValueInHistory(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn ⇒ watched_sync enabled, real executor
	ws := e.cfg.Workspace
	// a known constant lives in the workspace (the grounded-investigator target shape).
	if err := os.WriteFile(filepath.Join(ws, "regulator.go"),
		[]byte("ActionMargin:  1.5, // phase-margin proxy for async dead-time\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// start an episode so the thought graph exists (the rung-4 sourcer appends the grounded
	// observation onto the active branch; voicing/scoring read g.History()).
	e.startEpisode("In this Go codebase, find the value assigned to ActionMargin.", true)

	rs := &realitySourcer{e}
	need := subconscious.FuelNeed{
		Query:        "read regulator.go",
		Kind:         "fact",
		AllowReality: true,
	}
	text, ok, grounds, _ := rs.SourceReality(need)
	if !ok || !grounds {
		t.Fatalf("rung-4 should source a grounded reality observation (ok=%v grounds=%v, text=%q)", ok, grounds, text)
	}
	if !strings.Contains(text, "1.5") {
		t.Fatalf("the sourced reality text should carry the imported value 1.5, got %q", text)
	}

	// THE PROPERTY: the imported value must reach a thought in g.History() — not be lost upstream of
	// voicing. (Before the fix the grounded observation was never appended, so this FAILED.)
	found := false
	for _, th := range e.graph.History() {
		if strings.Contains(th.Text, "1.5") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the grounded value 1.5 never reached a thought in g.History() — A1 grounded-but-not-voiced gap")
	}
}
