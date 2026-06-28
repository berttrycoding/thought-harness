package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReadDocumentPlaintextPath: the always-available path — a .txt / .md fixture under the workdir is
// read directly (no parser) and its text returned. This is the deterministic, CI-assertable behaviour
// (the parser shell-out paths are environment-dependent and not asserted here).
func TestReadDocumentPlaintextPath(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "notes.txt"), []byte("hello document world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "readme.md"), []byte("# Title\n\nbody text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := NewReadDocument(d, time.Second)

	if r := rd.Execute(map[string]any{"path": "notes.txt"}); r.IsError || r.Content != "hello document world" {
		t.Errorf("txt read: %+v", r)
	}
	if r := rd.Execute(map[string]any{"path": "readme.md"}); r.IsError || r.Content != "# Title\n\nbody text" {
		t.Errorf("md read: %+v", r)
	}
}

// TestReadDocumentMissingFile: a missing path is a clean error (HONEST + RECOVERABLE — lists the
// workspace contents like read_file), never a crash.
func TestReadDocumentMissingFile(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "present.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := NewReadDocument(d, time.Second)
	r := rd.Execute(map[string]any{"path": "absent.pdf"})
	if !r.IsError || r.ErrorCode != ErrUnavailable {
		t.Fatalf("missing file must be an ErrUnavailable error; got %+v", r)
	}
	if !strings.Contains(r.Content, `could not find "absent.pdf"`) || !strings.Contains(r.Content, "present.txt") {
		t.Errorf("missing-file content should name the path + list the workspace; got %q", r.Content)
	}
	// missing 'path' arg
	if r := rd.Execute(map[string]any{}); !r.IsError || r.ErrorCode != ErrBadArgs {
		t.Errorf("empty path must be ErrBadArgs; got %+v", r)
	}
}

// TestReadDocumentLengthCapped: a large plaintext fixture is capped at maxDocOutput runes (a document
// read is a BOUNDED sense, never an unbounded dump), with a truncation marker carrying the true length.
func TestReadDocumentLengthCapped(t *testing.T) {
	d := t.TempDir()
	big := strings.Repeat("a", maxDocOutput+5000) // well over the cap
	if err := os.WriteFile(filepath.Join(d, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := NewReadDocument(d, time.Second)
	r := rd.Execute(map[string]any{"path": "big.txt"})
	if r.IsError {
		t.Fatalf("big plaintext read errored: %+v", r)
	}
	if n := len([]rune(r.Content)); n > maxDocOutput {
		t.Errorf("output not capped: %d runes > %d", n, maxDocOutput)
	}
	if !strings.Contains(r.Content, "document truncated") {
		t.Errorf("capped output must carry the truncation marker; got tail %q", lastRunes(r.Content, 60))
	}
}

// TestReadDocumentCannotEscapeJail: a relative path resolves against the tool's OWN workdir (the same
// jail-resolve read_file/edit_file use) — not the process CWD — so a bare relative arg stays anchored in
// the workspace. The executor's Sandbox is the HARD boundary that catches a ../ traversal that climbs out
// (it gates the actual touched path); at the TOOL level we assert read_document at least anchors a relative
// arg on its own workdir, identical to read_file's resolve contract (the engine wire test exercises the
// real sandbox path).
func TestReadDocumentCannotEscapeJail(t *testing.T) {
	d := t.TempDir()
	rd := NewReadDocument(d, time.Second)

	abs, _ := filepath.Abs(d)
	if rd.Workdir() != abs {
		t.Fatalf("ReadDocument.Workdir() = %q, want %q (the sandbox check resolves against this)", rd.Workdir(), abs)
	}
	// A relative "inner.txt" resolves under the workdir, never under the process CWD (the same anchoring
	// read_file/write_file/edit_file rely on for the sandbox to do its job).
	if got, want := resolve(rd.Workdir(), "inner.txt"), filepath.Join(abs, "inner.txt"); got != want {
		t.Errorf("resolve anchored wrong: %q, want %q", got, want)
	}
	// ReadDocument exposes Workdir() so the executor's sandbox can check the path it will actually touch.
	if _, ok := any(rd).(interface{ Workdir() string }); !ok {
		t.Fatal("ReadDocument must expose Workdir() for the sandbox gate")
	}
}

// TestReadDocumentNoParserAvailable pins the best-effort no-parser contract WITHOUT depending on
// poppler/libreoffice being installed in CI: it drives the pure availability/routing function. parsersFor
// returns the candidate parsers for a type; resolveParser reports ok=false when none is on PATH; and the
// Execute branch returns a clear ErrUnavailable naming the parser to install — never a crash.
func TestReadDocumentNoParserAvailable(t *testing.T) {
	// An unsupported extension has NO candidate parsers (routing-only, no I/O).
	if ps := parsersFor(".xyz"); ps != nil {
		t.Errorf("an unsupported extension must have no parsers; got %v", ps)
	}
	// A supported type routes to a named candidate.
	if ps := parsersFor(".pdf"); len(ps) == 0 || ps[0].bin != "pdftotext" {
		t.Errorf(".pdf must route to pdftotext; got %v", ps)
	}
	// The no-parser MESSAGE names the concrete package to install (the best-effort, actionable error).
	if m := noParserMessage(".pdf"); !strings.Contains(m, "poppler") || !strings.Contains(m, "pdftotext") {
		t.Errorf("a .pdf no-parser message must name poppler/pdftotext; got %q", m)
	}
	if m := noParserMessage(".docx"); !strings.Contains(m, "LibreOffice") {
		t.Errorf("a .docx no-parser message must name LibreOffice; got %q", m)
	}
	if m := noParserMessage(".xyz"); !strings.Contains(m, "read_file") {
		t.Errorf("an unsupported-type message should point at read_file; got %q", m)
	}

	// Execute on a .docx whose parser is absent returns a clean error (not a crash, not fabricated text).
	// If LibreOffice happens to be installed on the dev box, resolveParser succeeds and the converter may
	// run — so this is gated on the parser genuinely being unavailable (the CI condition).
	if _, _, ok := resolveParser(".docx"); !ok {
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "report.docx"), []byte("PK\x03\x04 not really a docx"), 0o644); err != nil {
			t.Fatal(err)
		}
		rd := NewReadDocument(d, time.Second)
		r := rd.Execute(map[string]any{"path": "report.docx"})
		if !r.IsError || r.ErrorCode != ErrUnavailable {
			t.Fatalf("a .docx with no installed parser must be a clean ErrUnavailable error; got %+v", r)
		}
		if !strings.Contains(r.Content, "no parser for .docx") {
			t.Errorf("no-parser Execute must name the missing parser; got %q", r.Content)
		}
	}
}

// TestReadDocumentCategoryIsInspectLocal pins the taxonomy: read_document is a READ (inspect/local —
// identical to read_file), NOT a mutation. It must agree with the name-only classifyCall fallback so the
// two taxonomies never drift when the tool is registered.
func TestReadDocumentCategoryIsInspectLocal(t *testing.T) {
	rd := NewReadDocument(t.TempDir(), time.Second)
	want := TaxClass{Op: OpInspect, Reach: ReachLocalWorld}
	if got := rd.Category(); got != want {
		t.Errorf("read_document.Category() = %s, want %s", got, want)
	}
	if byName := classifyCall("read_document"); byName != want {
		t.Errorf("classifyCall(read_document) = %s, want %s (taxonomy drift)", byName, want)
	}
	if FileModifyTools["read_document"] {
		t.Error("read_document must NOT be in FileModifyTools — it is a read, not a mutation")
	}
}

// TestReadDocumentNotInDefaultTools is the byte-identity guard at the action level: read_document must NOT
// be in the unconditional DefaultTools 5-tool set (it is registered only on the flag-ON engine path), so
// the parity 5-tool registry is unchanged.
func TestReadDocumentNotInDefaultTools(t *testing.T) {
	for _, tool := range DefaultTools(t.TempDir(), time.Second) {
		if tool.Name() == "read_document" {
			t.Fatal("read_document must NOT be in DefaultTools (it is flag-gated, registered only when on)")
		}
	}
}

// lastRunes returns the last n runes of s (a small test helper for asserting a truncation tail).
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// TestReadDocumentTimeoutBoundsForkingParser is the red-team T2.3 regression: a parser that FORKS a
// detached grandchild which inherits the stdout pipe (the LibreOffice shape) must NOT hang Wait past the
// bound. Before the fix (Setpgid with nothing signalling the group) this blocked ~30s; after it
// (Cancel kills the group + WaitDelay force-closes the inherited pipe) it returns within
// timeout + WaitDelay + grace. Unix-only (process groups), like the tool itself.
func TestReadDocumentTimeoutBoundsForkingParser(t *testing.T) {
	// A fake `pdftotext` that backgrounds a 30s sleeper (inheriting stdout) then exits 0 — the
	// direct child returns fast but the grandchild holds the pipe (red-team repro B).
	fakeDir := t.TempDir()
	fake := filepath.Join(fakeDir, "pdftotext")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 30 &\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "doc.pdf"), []byte("%PDF-1.4 fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := NewReadDocument(ws, 500*time.Millisecond)

	start := time.Now()
	r := rd.Execute(map[string]any{"path": "doc.pdf"})
	elapsed := time.Since(start)

	if !r.IsError {
		t.Errorf("a forking/hung parser must yield a best-effort error, got %+v", r)
	}
	// Bound = timeout (0.5s) + WaitDelay (2s) + generous grace; crucially MUCH less than the 30s sleep.
	if bound := 6 * time.Second; elapsed >= bound {
		t.Fatalf("read_document hung %s on a forking parser (bound %s) — the timeout does not bound wall-clock", elapsed, bound)
	}
}
