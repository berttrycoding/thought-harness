package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// toolCallEditFile builds an edit_file ToolCall (the dispatch shape the model authors — old/new strings
// the deterministic floor cannot supply, so edit_file is a P3 model-authored call like write_file).
func toolCallEditFile(path, oldS, newS string) action.ToolCall {
	return action.ToolCall{Name: "edit_file", Args: map[string]any{"path": path, "old_string": oldS, "new_string": newS}, Authored: true}
}

// TestEditFileFlagOnWiresToolAndScope is the engine WIRING-GATE test for subconscious.edit_file (T1.2):
// with the flag ON the engine (1) registers the edit_file tool in the action registry — so it dispatches
// through the same gated/sandboxed executor as write_file and actually mutates a workspace file — and (2)
// grants the expose-affordances operator the edit_file tool scope. Both are flag-gated edges (engine.go
// buildExecutor append + the GrantToolScope) — a built-but-unwired feature would pass neither. No injected
// seam (edit_file is a pure file-op tool), so a real workspace file edit is the proof it is live.
func TestEditFileFlagOnWiresToolAndScope(t *testing.T) {
	feat := config.New() // AllOn
	feat.Subconscious.EditFile = true
	feat.Validate()

	e, _ := newWorkspaceEngine(t, feat)

	// Seed a real file in the engine's workspace so the registered tool has something to edit.
	ws := e.cfg.Workspace
	if err := os.WriteFile(filepath.Join(ws, "code.txt"), []byte("keep\nold value\nkeep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// (1) the edit_file tool is REGISTERED + DISPATCHABLE through the gated executor and really mutates the
	//     file (it runs the same sandbox/gate path write_file runs — edit_file is in FileModifyTools).
	res := e.executor.Execute(toolCallEditFile("code.txt", "old value", "new value"))
	if res.IsError {
		t.Fatalf("edit_file ON must be registered + dispatch through the executor; got IsError code=%q content=%q", res.ErrorCode, res.Content)
	}
	got, err := os.ReadFile(filepath.Join(ws, "code.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "keep\nnew value\nkeep\n"; string(got) != want {
		t.Fatalf("edit_file ON must really edit the file; got %q want %q", string(got), want)
	}

	// (2) the expose-affordances operator was GRANTED the edit_file scope (a mutate-capable sub-agent can now
	//     author an edit_file call alongside its read/search local tools).
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if !containsStr(spec.ToolScope, "edit_file") {
		t.Fatalf("flag ON: expose-affordances ToolScope must include edit_file; got %v", spec.ToolScope)
	}
}

// TestEditFileFlagOffByteIdentical is the byte-identical-OFF arm: with the flag OFF (the default), the
// edit_file tool is NOT registered (a dispatch errors as an unknown tool, the file untouched) AND the
// expose-affordances scope is unchanged ({search, read_file}). No registration, no scope add — the
// pipeline is byte-identical to the pre-flag engine.
func TestEditFileFlagOffByteIdentical(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn, edit_file OFF (default)

	ws := e.cfg.Workspace
	const before = "keep\nold value\nkeep\n"
	if err := os.WriteFile(filepath.Join(ws, "code.txt"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	// (1) edit_file is NOT in the registry — an unknown-tool dispatch errors and the file is untouched.
	res := e.executor.Execute(toolCallEditFile("code.txt", "old value", "new value"))
	if !res.IsError {
		t.Fatalf("edit_file OFF must NOT be registered (dispatch must error); got content=%q", res.Content)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "code.txt"))
	if string(got) != before {
		t.Fatalf("edit_file OFF must leave the file untouched (unregistered); got %q", string(got))
	}

	// (2) expose-affordances keeps its default scope — no edit_file granted.
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if containsStr(spec.ToolScope, "edit_file") {
		t.Fatalf("flag OFF: expose-affordances must NOT carry edit_file (byte-identical); got %v", spec.ToolScope)
	}
}
