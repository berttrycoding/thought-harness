package tui

// compare_load_test.go — the G2 COMPARE disk-load WIRING gate (Track G). Proves the user goal is
// actually reachable through the live App: ^Y opens the ANALYSIS surface, `c` enters COMPARE, and with
// the tui.compare_load knob ON the surface LOADS the two most recent recorded runs from disk into A/B
// (the power-ON/OFF benchmark over REAL recordings). Default OFF keeps the prototype frozen-A/sample-B
// pair (byte-identical, no filesystem touch). The wiring-gate lesson: a unit that exists but never
// runs on the App's actual key path is dead — so this drives the real Update keystrokes.

import (
	"os"
	"testing"
	"time"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"

	tea "github.com/charmbracelet/bubbletea"
)

// mustOldTime returns a timestamp a minute in the past so a backdated file sorts strictly before a
// file written now (unambiguous newest-first ordering even at coarse mod-time granularity).
func mustOldTime() time.Time { return time.Now().Add(-time.Minute) }

// onLog / offLog are recorded event JSONL streams (a SOLVED ON run, a gave-up OFF run) the COMPARE
// load reconstructs into A/B.
const compareOnLog = `{"tick":1,"kind":"port","data":{"source":"USER_INPUT","text":"is this refactor safe to ship?"}}
{"tick":2,"kind":"subconscious.fire","data":{}}
{"tick":3,"kind":"seam.transform","data":{}}
{"tick":4,"kind":"critic.decision","data":{"decision":"ACT","reason":"needs ground truth the loop can't manufacture"}}
{"tick":5,"kind":"action.tool","data":{"tool":"run"}}
{"tick":5,"kind":"grounding","data":{"verdict":"grounded","claim":"suite 12/12 pass"}}
{"tick":6,"kind":"critic.decision","data":{"decision":"STOP","stop_kind":"GOAL_MET","reason":"suite passed"}}
{"tick":6,"kind":"action.respond","data":{}}`

const compareOffLog = `{"tick":1,"kind":"port","data":{"source":"USER_INPUT","text":"is this refactor safe to ship?"}}
{"tick":2,"kind":"subconscious.fire","data":{}}
{"tick":4,"kind":"critic.decision","data":{"decision":"THINK","reason":"keep reasoning, no ground truth"}}
{"tick":6,"kind":"critic.decision","data":{"decision":"STOP","stop_kind":"GIVE_UP","reason":"exhausted the lines"}}`

// newCompareApp builds a real App whose engine carries tui.compare_load = `load`, and points the
// COMPARE runs dir at a temp directory holding the OFF (older) + ON (newer) recorded logs.
func newCompareApp(t *testing.T, load bool) *App {
	t.Helper()
	dir := t.TempDir()
	writeRun(t, dir+"/run-off.jsonl", compareOffLog, true) // older
	writeRun(t, dir+"/run-on.jsonl", compareOnLog, false)  // newer ⇒ A
	t.Setenv("THOUGHT_RUNS_DIR", dir)

	feat := config.New()
	feat.Tui.CompareLoad = load
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	a.w, a.h = 120, 40
	a.recomputeLayout()
	return a
}

// writeRun writes a recorded log; backdate=true sets an older mod-time so newest-first ordering is
// unambiguous (the ON run, written without backdating, sorts as the most recent = A).
func writeRun(t *testing.T, path, body string, backdate bool) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if backdate {
		old := mustOldTime()
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}
}

func key(a *App, k string) { a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}) }

func keyCtrl(a *App, t tea.KeyType) { a.Update(tea.KeyMsg{Type: t}) }

// TestCompareLoadOnLoadsTwoRecordedRuns — the user goal proven through the App: ^Y opens analysis, `c`
// enters COMPARE, and with tui.compare_load ON the surface loads the two newest recorded runs into A/B.
// A (newest = the ON run) must read SOLVED, B (the OFF run) UNSOLVED — the loaded benchmark, not the
// synthetic sample.
func TestCompareLoadOnLoadsTwoRecordedRuns(t *testing.T) {
	a := newCompareApp(t, true)

	keyCtrl(a, tea.KeyCtrlY) // open the ANALYSIS surface
	if !a.anPreview {
		t.Fatal("^Y must open the ANALYSIS surface")
	}
	key(a, "c") // enter COMPARE ⇒ enterCompare loads the disk pair
	if !a.anCompare {
		t.Fatal("`c` must enter COMPARE")
	}
	if a.anRecA.SolveVerdict != "SOLVED" {
		t.Errorf("A (newest recorded run, the ON arm) must read SOLVED off disk; got %q (name %q)", a.anRecA.SolveVerdict, a.anRecA.Name)
	}
	if a.anRecB.SolveVerdict != "UNSOLVED" {
		t.Errorf("B (the OFF arm) must read UNSOLVED off disk; got %q (name %q)", a.anRecB.SolveVerdict, a.anRecB.Name)
	}
	// the loaded A must be a REAL recorded run, not the synthetic sample (the sample is "run-2026-...").
	if a.anRecA.Name == "run-2026-06-20-claude-AWAKE-ON" {
		t.Error("A is still the synthetic sample — the disk load did not replace it")
	}
}

// TestCompareLoadOffKeepsPrototype — the default-OFF gate: with tui.compare_load OFF, `c` toggles
// COMPARE but does NOT load from disk; A stays the frozen/sample record and B the synthetic OFF sample
// (byte-identical to the prototype, no filesystem touch).
func TestCompareLoadOffKeepsPrototype(t *testing.T) {
	a := newCompareApp(t, false)

	keyCtrl(a, tea.KeyCtrlY)
	beforeA := a.anRecA.Name
	key(a, "c")
	if !a.anCompare {
		t.Fatal("`c` must still toggle COMPARE when the load knob is off")
	}
	// A is unchanged (the frozen/sample record), NOT a loaded disk run.
	if a.anRecA.Name != beforeA {
		t.Errorf("compare_load OFF must NOT replace A from disk; A changed from %q to %q", beforeA, a.anRecA.Name)
	}
	// B is the synthetic OFF sample, not the recorded OFF run.
	if a.anRecB.Name != "run-2026-06-20-claude-AWAKE-OFF" {
		t.Errorf("compare_load OFF must keep the synthetic sample B; got %q", a.anRecB.Name)
	}
}

// TestCompareToggleLeavesSingleView — `c` a second time leaves COMPARE (back to the SINGLE record),
// so the toggle is reversible regardless of the load knob.
func TestCompareToggleLeavesSingleView(t *testing.T) {
	a := newCompareApp(t, true)
	keyCtrl(a, tea.KeyCtrlY)
	key(a, "c")
	if !a.anCompare {
		t.Fatal("first `c` enters COMPARE")
	}
	key(a, "c")
	if a.anCompare {
		t.Error("second `c` must leave COMPARE (back to the SINGLE view)")
	}
}
