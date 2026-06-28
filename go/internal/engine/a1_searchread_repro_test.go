package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// comprehendBackend wraps the deterministic TestBackend and ADDS a scripted RealityComprehender so the
// offline test exercises the EXACT live-shape "to_operator" handoff (the agentic search-then-read flow)
// without a real model. The scripting mirrors what a correct agent does: when no concrete value is yet
// in view it SEARCHES for the symbol; once a search result naming the file is in context it READS that
// file. This isolates the WIRING of the search->read handoff (does read_file get offered + selected, and
// does the value reach a thought) from the question of whether a real model makes the right call.
type comprehendBackend struct {
	*backends.TestBackend
	symbol  string         // the identifier the investigator is after
	pathRe  *regexp.Regexp // matches a "<path>:line:" search-hit so we can lift the file to read
	lastTgt string         // last target we returned (for the test to assert on)
}

func newComprehendBackend(symbol string) *comprehendBackend {
	return &comprehendBackend{
		TestBackend: backends.NewTest(),
		symbol:      symbol,
		// a search hit looks like "limits.go:3:const AdmitAmbiguityThreshold = 0.5" — lift the path.
		pathRe: regexp.MustCompile(`([\w./-]+\.go):\d+:`),
	}
}

// Comprehend is the scripted to_operator: READ the file once a search hit names it; else SEARCH the symbol.
func (b *comprehendBackend) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	joined := ""
	for _, t := range ctx {
		joined += " " + t.Text
	}
	if m := b.pathRe.FindStringSubmatch(joined); m != nil {
		b.lastTgt = m[1]
		return "read", m[1], true // a source is named but not yet quoted -> read it
	}
	b.lastTgt = b.symbol
	return "search", b.symbol, true // nothing concrete yet -> find where the symbol lives
}

var _ backends.RealityComprehender = (*comprehendBackend)(nil)

// TestA1SearchThenReadOffersReadFile is the A1 "early-give-up-after-search" CLASSIFICATION probe. A
// grounded-investigator goal whose answer is a clean `const = 0.5` declaration in the workspace, driven
// through the FULL reactive loop with a scripted comprehender (the deterministic stand-in for the live
// agent's to_operator decision). The symptom (powered probe): the harness grounded + searched but then
// gave up ("unable to directly read the file with the tools available to me") despite read_file existing
// and the value having a plain declaration. This test settles whether the search->read HANDOFF is wired:
// once a search hit names the file, is read_file offered + selected, and does the value reach a thought?
func TestA1SearchThenReadOffersReadFile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = t.TempDir()
	cfg.Features = config.New() // AllOn -> watched_sync, real executor, all specialists
	be := newComprehendBackend("AdmitAmbiguityThreshold")
	e, err := NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if e.executor == nil {
		t.Fatal("a workspace engine must build a real executor")
	}
	ws := e.cfg.Workspace
	if err := os.WriteFile(filepath.Join(ws, "limits.go"),
		[]byte("package config\n\nconst AdmitAmbiguityThreshold = 0.5 // ambiguity admit floor\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var tools []string
	e.Bus().Subscribe(func(ev events.Event) {
		switch ev.Kind {
		case events.SubFire, events.Observation, events.ActionTool:
			if tn, ok := ev.Data["tool"].(string); ok && tn != "" {
				tools = append(tools, tn)
			}
		}
	})

	e.SubmitDefault("In this Go codebase, find the value assigned to AdmitAmbiguityThreshold.")
	e.Run(40)

	var values []string
	for _, th := range e.Graph().History() {
		if strings.Contains(th.Text, "0.5") {
			values = append(values, th.Text)
		}
	}

	usedRead, usedSearch := false, false
	for _, tn := range tools {
		if tn == "read_file" {
			usedRead = true
		}
		if tn == "search" {
			usedSearch = true
		}
	}
	t.Logf("tools offered/run: %v", tools)
	t.Logf("used read_file=%v used search=%v value-in-thought=%v", usedRead, usedSearch, len(values) > 0)
	t.Logf("last response: %q", e.LastResponse())

	if !usedSearch {
		t.Fatal("the search affordance never fired despite a search-shaped goal + comprehender")
	}
	if !usedRead {
		t.Fatal("A1 WIRING GAP: after search named the file, read_file was never offered/selected for the follow-up read")
	}
	if len(values) == 0 {
		t.Fatal("A1 WIRING GAP: the grounded value 0.5 never reached a thought in g.History() (not voiceable)")
	}
}

// giveUpBackend wraps TestBackend with a comprehender that SEARCHES once, then declines (need=none) —
// the exact "model gives up after search" behaviour the powered probe saw. It proves the give-up is a
// COMPREHENSION/MODEL decision: with the SAME wiring as the passing test above, when the agent declines
// to issue the follow-up read, read_file never fires (not because it is missing, but because nothing
// asked for it). This is the control that classifies the A1 symptom as model-give-up, not a wiring bug.
type giveUpBackend struct {
	*backends.TestBackend
	symbol   string
	searched bool
}

func (b *giveUpBackend) Comprehend(ctx []types.Thought) (need, target string, ok bool) {
	if b.searched {
		return "none", "", true // the model "gives up" after the search instead of reading
	}
	b.searched = true
	return "search", b.symbol, true
}

var _ backends.RealityComprehender = (*giveUpBackend)(nil)

// TestA1GiveUpAfterSearchIsModelDecision is the classification CONTROL: with byte-identical wiring to
// the passing handoff test, a comprehender that DECLINES the follow-up read (need=none) reproduces the
// symptom — read_file never fires. The machinery is ready; the give-up is the agent's call. This is what
// makes the A1 symptom a MODEL-give-up (prompt/orchestration, validated only by a before/after lift run),
// not an offline-fixable wiring bug.
func TestA1GiveUpAfterSearchIsModelDecision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Workspace = t.TempDir()
	cfg.Features = config.New()
	be := &giveUpBackend{TestBackend: backends.NewTest(), symbol: "AdmitAmbiguityThreshold"}
	e, err := NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if err := os.WriteFile(filepath.Join(e.cfg.Workspace, "limits.go"),
		[]byte("package config\n\nconst AdmitAmbiguityThreshold = 0.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var tools []string
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.SubFire || ev.Kind == events.Observation || ev.Kind == events.ActionTool {
			if tn, ok := ev.Data["tool"].(string); ok && tn != "" {
				tools = append(tools, tn)
			}
		}
	})
	e.SubmitDefault("In this Go codebase, find the value assigned to AdmitAmbiguityThreshold.")
	e.Run(40)

	usedRead := false
	for _, tn := range tools {
		if tn == "read_file" {
			usedRead = true
		}
	}
	t.Logf("tools (give-up branch): %v", tools)
	// The CONTROL property: identical wiring, the model declined -> read_file does NOT fire. read_file is
	// not absent from the engine (the passing test fires it); it is simply not requested. This is why the
	// symptom is model-give-up, not a missing/dropped affordance.
	if usedRead {
		t.Fatal("control failed: read_file fired even though the comprehender declined — the give-up branch should leave read unrequested")
	}
}
