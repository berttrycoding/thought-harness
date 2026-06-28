package tui

// cogmodels_test.go — headless model test for the COGNITION MODELS version-manager popup (proposal
// §6 + §11 Track 1). It exercises the REAL path end-to-end: a JSONLStore (temp dir) holding three saved
// snapshots → the bridge's CogModelsListCmd projects them into rows with the EXACT structural diff vs the
// baseline → the popup renders them. The test asserts the THINKING the spec intends, not just that a box
// draws:
//   - the snapshot names appear,
//   - the STRUCTURAL delta is the real diff number (the cold baseline grew +N skills),
//   - the CAPABILITY column is the literal "needs K-replay" placeholder (never a fabricated solve-rate —
//     the thrice-paid lying-green trap the panel exists to refuse),
//   - the substrate provenance tag is shown (claude-grown vs other lineages stay distinct),
//   - selecting + diffing emits the right action message.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/tui/popup"
)

// newCogModelsStore builds a JSONLStore in a temp dir holding three named snapshots: a cold baseline, a
// version grown +2 skills / +3 beliefs off it, and a second-substrate lineage. The counts are EXACT, so
// the structural diff is a deterministic, failable assertion.
func newCogModelsStore(t *testing.T) *persist.JSONLStore {
	t.Helper()
	st, err := persist.NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}

	grounded := persist.Meta{Grounded: true, Status: persist.StatusActive}

	// the cold baseline — empty learned state, claude lineage.
	base := persist.SnapshotRecord{
		Meta: persist.SnapshotMeta{Name: popup.ColdBaselineName, Substrate: "claude:sonnet", CreatedTick: 0},
		Data: persist.Snapshot{},
	}
	if err := st.SaveSnapshot(base); err != nil {
		t.Fatalf("SaveSnapshot baseline: %v", err)
	}

	// a grown version off the baseline: +2 skills, +3 beliefs (claude lineage).
	grown := persist.SnapshotRecord{
		Meta: persist.SnapshotMeta{Name: "curious-v1", Substrate: "claude:sonnet", CreatedTick: 252},
		Data: persist.Snapshot{
			Skills: []persist.SkillRecord{
				{Meta: grounded, Name: "decompose"},
				{Meta: grounded, Name: "verify"},
			},
			Beliefs: []persist.BeliefRecord{
				{Meta: grounded, Statement: "x is true", ValidFrom: 1},
				{Meta: grounded, Statement: "y holds", ValidFrom: 2},
				{Meta: grounded, Statement: "z observed", ValidFrom: 3},
			},
		},
	}
	if err := st.SaveSnapshot(grown); err != nil {
		t.Fatalf("SaveSnapshot grown: %v", err)
	}

	// a different-substrate lineage (test), to prove the per-row substrate tag distinguishes lineages.
	other := persist.SnapshotRecord{
		Meta: persist.SnapshotMeta{Name: "local-run", Substrate: "test", CreatedTick: 40},
		Data: persist.Snapshot{
			Skills: []persist.SkillRecord{{Meta: grounded, Name: "decompose"}},
		},
	}
	if err := st.SaveSnapshot(other); err != nil {
		t.Fatalf("SaveSnapshot other: %v", err)
	}
	return st
}

// newCogModelsApp builds a live App over a test-backend engine whose Store is the three-snapshot fixture.
func newCogModelsApp(t *testing.T) *App {
	t.Helper()
	st := newCogModelsStore(t)
	cfg := engine.DefaultConfig()
	cfg.Store = st
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	a := NewApp(NewBridge(e), cfg, "")
	a.w, a.h, a.ready = 120, 40, true
	return a
}

// runCmd drains a tea.Cmd to its tea.Msg (the off-loop bridge Cmds are synchronous closures here).
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a non-nil Cmd")
	}
	return cmd()
}

// TestCogModelsPopupRendersStructuralAndCapabilityClasses is the cognitive-property test: the popup, fed
// the REAL structural diff off the persist store, separates the two metric classes honestly — exact
// structural numbers, and a literal not-yet-measured capability placeholder (never a fabricated number).
func TestCogModelsPopupRendersStructuralAndCapabilityClasses(t *testing.T) {
	a := newCogModelsApp(t)

	// open the popup via the bridge's list Cmd (the /models entry path), folding the result like Update.
	msg := runCmd(t, a.bridge.CogModelsListCmd(a.cogBaseline, true))
	listed, ok := msg.(cogModelsListedMsg)
	if !ok {
		t.Fatalf("expected cogModelsListedMsg, got %T", msg)
	}
	if listed.err != "" {
		t.Fatalf("list error: %s", listed.err)
	}
	a.cogBaseline = listed.baseline
	a.cogmodel.SetSize(a.w, a.h)
	a.cogmodel.Show(listed.rows, listed.baseline, listed.substrate)

	view := a.cogmodel.View()

	// the names of all three saved cognition models appear.
	for _, name := range []string{popup.ColdBaselineName, "curious-v1", "local-run"} {
		if !strings.Contains(view, name) {
			t.Errorf("view missing snapshot name %q\n---\n%s", name, view)
		}
	}

	// STRUCTURAL (exact): curious-v1 grew +2 skills and +3 beliefs off the cold baseline — the real diff.
	if !strings.Contains(view, "+2 skill") {
		t.Errorf("view missing exact structural delta '+2 skill'\n---\n%s", view)
	}
	if !strings.Contains(view, "+3 bel") {
		t.Errorf("view missing exact structural delta '+3 bel'\n---\n%s", view)
	}

	// CAPABILITY (noisy): the literal placeholder, never a fabricated solve-rate / cost number.
	if !strings.Contains(view, "needs K-replay") {
		t.Errorf("view missing the 'needs K-replay' capability placeholder\n---\n%s", view)
	}
	if strings.Contains(view, "pp") || strings.Contains(view, "solve") {
		t.Errorf("view appears to fabricate a capability number (found 'pp'/'solve')\n---\n%s", view)
	}

	// SUBSTRATE provenance: both lineages tagged (claude-grown vs test), so they never cross-compare blind.
	if !strings.Contains(view, "claude:sonnet") {
		t.Errorf("view missing the claude substrate tag\n---\n%s", view)
	}
	if !strings.Contains(view, "test") {
		t.Errorf("view missing the test substrate tag\n---\n%s", view)
	}

	// the action legend is present (the affordances are discoverable).
	if !strings.Contains(view, "[s]ave") || !strings.Contains(view, "[d]iff") {
		t.Errorf("view missing the action legend\n---\n%s", view)
	}
}

// TestCogModelsPopupSelectAndDiff drives selection + the [d]iff key and asserts the popup emits the
// correct CogModelDiffMsg (baseline → selected), and that the bridge's diff Cmd returns the exact
// structural delta — the gate-witness path (the keep-or-revert campaign's structural number).
func TestCogModelsPopupSelectAndDiff(t *testing.T) {
	a := newCogModelsApp(t)
	msg := runCmd(t, a.bridge.CogModelsListCmd(a.cogBaseline, true))
	listed := msg.(cogModelsListedMsg)
	a.cogBaseline = listed.baseline
	a.cogmodel.SetSize(a.w, a.h)
	a.cogmodel.Show(listed.rows, listed.baseline, listed.substrate)

	// the baseline must resolve to the cold baseline (the reserved name takes precedence).
	if listed.baseline != popup.ColdBaselineName {
		t.Fatalf("baseline = %q, want %q", listed.baseline, popup.ColdBaselineName)
	}

	// move the selection to curious-v1 (the grown version) then press [d] to diff vs the baseline.
	// the rows are newest-first: local-run, curious-v1, cold-baseline; Show() starts on the baseline row.
	cm := a.cogmodel
	// select the curious-v1 row explicitly by walking up/down until it is selected.
	cm = sendKey(cm, "up")
	cm = sendKey(cm, "up") // reach the top (local-run); then step down to curious-v1
	cm = sendKey(cm, "down")

	cm, cmd := cm.Update(keyMsg("d"))
	a.cogmodel = cm
	if cmd == nil {
		t.Fatal("[d] should emit a diff Cmd")
	}
	out := cmd()
	diffMsg, ok := out.(popup.CogModelDiffMsg)
	if !ok {
		t.Fatalf("expected popup.CogModelDiffMsg, got %T", out)
	}
	if diffMsg.From != popup.ColdBaselineName {
		t.Errorf("diff From = %q, want the baseline %q", diffMsg.From, popup.ColdBaselineName)
	}
	if diffMsg.To != "curious-v1" {
		t.Errorf("diff To = %q, want curious-v1 (the selected grown version)", diffMsg.To)
	}

	// the App folds the diff message into the bridge diff Cmd; the result note carries the exact delta.
	actionMsg := runCmd(t, a.bridge.CogModelDiffCmd(diffMsg.From, diffMsg.To))
	action, ok := actionMsg.(cogModelActionMsg)
	if !ok {
		t.Fatalf("expected cogModelActionMsg, got %T", actionMsg)
	}
	if action.err != "" {
		t.Fatalf("diff Cmd error: %s", action.err)
	}
	if !strings.Contains(action.note, "+2 skill") || !strings.Contains(action.note, "+3 bel") {
		t.Errorf("diff note missing the exact structural delta: %q", action.note)
	}
}

// keyMsg builds a tea.KeyMsg for a single named key (the popup's Update key sink).
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// sendKey drives one key through the popup's value-receiver Update (discarding the Cmd).
func sendKey(c popup.CogModels, s string) popup.CogModels {
	c, _ = c.Update(keyMsg(s))
	return c
}
