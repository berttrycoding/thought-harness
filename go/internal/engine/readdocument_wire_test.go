package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// toolCallReadDocument builds a read_document ToolCall (the dispatch shape the model authors — a path the
// deterministic floor cannot infer beyond a bare name, so read_document is a P3 model-authored call).
func toolCallReadDocument(path string) action.ToolCall {
	return action.ToolCall{Name: "read_document", Args: map[string]any{"path": path}}
}

// TestReadDocumentFlagOnWiresToolAndScope is the engine WIRING-GATE test for subconscious.read_document
// (T2.3): with the flag ON the engine (1) registers the read_document tool in the action registry — so it
// dispatches through the same gated/sandboxed executor as read_file and really extracts a workspace file's
// text — and (2) grants the expose-affordances operator the read_document tool scope. Both are flag-gated
// edges (engine.go buildExecutor append + the GrantToolScope) — a built-but-unwired feature would pass
// neither. No injected seam (read_document is a pure file-op tool); the deterministic plaintext path is the
// CI-assertable proof it is live (the parser shell-out paths are environment-dependent).
func TestReadDocumentFlagOnWiresToolAndScope(t *testing.T) {
	feat := config.New() // AllOn
	feat.Subconscious.ReadDocument = true
	feat.Validate()

	e, _ := newWorkspaceEngine(t, feat)

	// Seed a real text document in the engine's workspace so the registered tool has something to read.
	ws := e.cfg.Workspace
	if err := os.WriteFile(filepath.Join(ws, "brief.txt"), []byte("the answer is 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// (1) the read_document tool is REGISTERED + DISPATCHABLE through the gated executor and returns the
	//     extracted text (the plaintext path — deterministic, always available).
	res := e.executor.Execute(toolCallReadDocument("brief.txt"))
	if res.IsError {
		t.Fatalf("read_document ON must be registered + dispatch through the executor; got IsError code=%q content=%q", res.ErrorCode, res.Content)
	}
	if res.Content != "the answer is 42" {
		t.Fatalf("read_document ON must return the document text; got %q", res.Content)
	}

	// (2) the expose-affordances operator was GRANTED the read_document scope (a sub-agent can now author a
	//     read_document call alongside its read_file/search local tools).
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if !containsStr(spec.ToolScope, "read_document") {
		t.Fatalf("flag ON: expose-affordances ToolScope must include read_document; got %v", spec.ToolScope)
	}
}

// TestReadDocumentFlagOffByteIdentical is the byte-identical-OFF arm: with the flag OFF (the default), the
// read_document tool is NOT registered (a dispatch errors as an unknown tool) AND the expose-affordances
// scope is unchanged ({search, read_file}). No registration, no scope add — the pipeline is byte-identical
// to the pre-flag engine.
func TestReadDocumentFlagOffByteIdentical(t *testing.T) {
	e, _ := newWorkspaceEngine(t, config.New()) // AllOn, read_document OFF (default)

	ws := e.cfg.Workspace
	if err := os.WriteFile(filepath.Join(ws, "brief.txt"), []byte("the answer is 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// (1) read_document is NOT in the registry — an unknown-tool dispatch errors (never a fabricated result).
	res := e.executor.Execute(toolCallReadDocument("brief.txt"))
	if !res.IsError {
		t.Fatalf("read_document OFF must NOT be registered (dispatch must error); got content=%q", res.Content)
	}

	// (2) expose-affordances keeps its default scope — no read_document granted.
	spec, ok := e.catalog.Get("expose-affordances")
	if !ok {
		t.Fatal("expose-affordances operator missing from the catalog")
	}
	if containsStr(spec.ToolScope, "read_document") {
		t.Fatalf("flag OFF: expose-affordances must NOT carry read_document (byte-identical); got %v", spec.ToolScope)
	}
}
