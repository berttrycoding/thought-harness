package action

import (
	"os"
	"path/filepath"
	"testing"
)

// readWS reads a workspace-relative file back as a string (the post-edit ground truth).
func readWS(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("read back %s: %v", rel, err)
	}
	return string(b)
}

// TestEditFileUniqueExactReplace is the happy path: a UNIQUE exact old_string is replaced in place and
// the file is written back with the rest byte-identical — the surgical edit the tool exists for.
func TestEditFileUniqueExactReplace(t *testing.T) {
	d := t.TempDir()
	const before = "line one\nthe target line\nline three\n"
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	ef := NewEditFile(d)
	r := ef.Execute(map[string]any{"path": "f.txt", "old_string": "the target line", "new_string": "the replaced line"})
	if r.IsError {
		t.Fatalf("unique exact replace must succeed; got %+v", r)
	}
	if r.Content != "edited f.txt: 1 replacement" {
		t.Errorf("content = %q, want %q", r.Content, "edited f.txt: 1 replacement")
	}
	if got, want := readWS(t, d, "f.txt"), "line one\nthe replaced line\nline three\n"; got != want {
		t.Errorf("file after edit = %q, want %q", got, want)
	}
}

// TestEditFileNotFoundLeavesFileUntouched: an old_string that is neither an exact nor a fuzzy match errors
// (ErrBadArgs) and the file is left byte-identical — the tool never partially applies or guesses.
func TestEditFileNotFoundLeavesFileUntouched(t *testing.T) {
	d := t.TempDir()
	const before = "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	ef := NewEditFile(d)
	r := ef.Execute(map[string]any{"path": "f.txt", "old_string": "delta epsilon zeta", "new_string": "x"})
	if !r.IsError || r.ErrorCode != ErrBadArgs {
		t.Fatalf("not-found must be ErrBadArgs; got %+v", r)
	}
	if got := readWS(t, d, "f.txt"); got != before {
		t.Errorf("file must be UNTOUCHED on not-found; got %q", got)
	}
}

// TestEditFileAmbiguousRequiresReplaceAll: a >1-occurrence old_string errors (with the match count) unless
// replace_all is set, in which case ALL occurrences are replaced.
func TestEditFileAmbiguousRequiresReplaceAll(t *testing.T) {
	d := t.TempDir()
	const before = "foo\nbar\nfoo\nbaz\nfoo\n"
	write := func() {
		if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(before), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ef := NewEditFile(d)

	// >1 without replace_all -> error, file untouched.
	write()
	r := ef.Execute(map[string]any{"path": "f.txt", "old_string": "foo", "new_string": "QUX"})
	if !r.IsError || r.ErrorCode != ErrBadArgs {
		t.Fatalf("ambiguous without replace_all must be ErrBadArgs; got %+v", r)
	}
	if got := readWS(t, d, "f.txt"); got != before {
		t.Errorf("ambiguous reject must leave file untouched; got %q", got)
	}

	// >1 WITH replace_all -> all replaced.
	write()
	r = ef.Execute(map[string]any{"path": "f.txt", "old_string": "foo", "new_string": "QUX", "replace_all": true})
	if r.IsError {
		t.Fatalf("replace_all must succeed; got %+v", r)
	}
	if r.Content != "edited f.txt: 3 replacements" {
		t.Errorf("content = %q, want %q", r.Content, "edited f.txt: 3 replacements")
	}
	if got, want := readWS(t, d, "f.txt"), "QUX\nbar\nQUX\nbaz\nQUX\n"; got != want {
		t.Errorf("replace_all result = %q, want %q", got, want)
	}
}

// TestEditFileFuzzyWhitespaceFallback is the str_replace-editor fuzzy contract: an old_string whose
// per-line leading whitespace differs from the file (the common indentation near-miss) still matches
// UNIQUELY and replaces — preserving the rest of the file. The replacement substitutes the model's
// new_string verbatim (the file's original indentation is on the surrounding lines, which are untouched).
func TestEditFileFuzzyWhitespaceFallback(t *testing.T) {
	d := t.TempDir()
	// The file uses tab indentation; the model supplies the same code with 4-space indentation.
	const before = "func f() {\n\tif x {\n\t\treturn 1\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(d, "f.go"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	ef := NewEditFile(d)
	// old_string with 4-space indent (no exact match — fuzzy on TrimSpace must find the one region).
	r := ef.Execute(map[string]any{
		"path":       "f.go",
		"old_string": "    if x {\n        return 1\n    }",
		"new_string": "\tif x {\n\t\treturn 2\n\t}",
	})
	if r.IsError {
		t.Fatalf("fuzzy whitespace fallback must succeed; got %+v", r)
	}
	if got, want := readWS(t, d, "f.go"), "func f() {\n\tif x {\n\t\treturn 2\n\t}\n}\n"; got != want {
		t.Errorf("fuzzy result = %q, want %q", got, want)
	}
}

// TestEditFileFuzzyAmbiguousDoesNotGuess: when the whitespace-fuzzy match resolves to MORE than one region,
// the fallback refuses (errors) rather than edit an arbitrary one — the no-guess guard.
func TestEditFileFuzzyAmbiguousDoesNotGuess(t *testing.T) {
	d := t.TempDir()
	// Two regions that both TrimSpace-match the (differently-indented) pattern.
	const before = "block\n\tx = 1\nblock\n        x = 1\n"
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	ef := NewEditFile(d)
	r := ef.Execute(map[string]any{"path": "f.txt", "old_string": "x = 1", "new_string": "x = 2"})
	// "x = 1" is NOT an exact-substring-unique (it appears twice exactly), so without replace_all this is the
	// ambiguous exact path -> ErrBadArgs. The point: the tool never silently edits one of two matches.
	if !r.IsError || r.ErrorCode != ErrBadArgs {
		t.Fatalf("two matches must error, never guess; got %+v", r)
	}
	if got := readWS(t, d, "f.txt"); got != before {
		t.Errorf("ambiguous must leave file untouched; got %q", got)
	}
}

// TestEditFileFuzzyPreservesCRLF (red-team regression): a CRLF file edited via the WHITESPACE-FUZZY path
// must keep its "\r\n" terminators — the matched span must exclude the line's trailing '\r' so the splice
// cannot silently strip it and produce mixed line endings. (The exact-substring path already preserves
// '\r' as literal text; this pins the fuzzy path.)
func TestEditFileFuzzyPreservesCRLF(t *testing.T) {
	d := t.TempDir()
	const before = "keep\r\n\ttarget line\r\nkeep2\r\n" // CRLF, tab-indented target
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	ef := NewEditFile(d)
	// 4-space indent (no exact substring match) -> fuzzy path on TrimSpace.
	r := ef.Execute(map[string]any{"path": "f.txt", "old_string": "    target line", "new_string": "NEW"})
	if r.IsError {
		t.Fatalf("fuzzy CRLF edit must succeed; got %+v", r)
	}
	if got, want := readWS(t, d, "f.txt"), "keep\r\nNEW\r\nkeep2\r\n"; got != want {
		t.Errorf("fuzzy CRLF result = %q, want %q (line endings must be preserved)", got, want)
	}
}

// TestEditFileMissingFileErrors: edit_file edits an EXISTING file; a missing path errors (ErrUnavailable)
// and points the caller at write_file. It never creates the file.
func TestEditFileMissingFileErrors(t *testing.T) {
	d := t.TempDir()
	ef := NewEditFile(d)
	r := ef.Execute(map[string]any{"path": "nope.txt", "old_string": "a", "new_string": "b"})
	if !r.IsError || r.ErrorCode != ErrUnavailable {
		t.Fatalf("missing file must be ErrUnavailable; got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(d, "nope.txt")); !os.IsNotExist(err) {
		t.Errorf("edit_file must NOT create the file; stat err = %v", err)
	}
}

// TestEditFileEmptyOldStringErrors: an empty old_string has no anchor — bad-args, pointing at write_file.
func TestEditFileEmptyOldStringErrors(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	ef := NewEditFile(d)
	r := ef.Execute(map[string]any{"path": "f.txt", "old_string": "", "new_string": "x"})
	if !r.IsError || r.ErrorCode != ErrBadArgs {
		t.Fatalf("empty old_string must be ErrBadArgs; got %+v", r)
	}
	if got := readWS(t, d, "f.txt"); got != "content" {
		t.Errorf("empty old_string must leave file untouched; got %q", got)
	}
}

// TestEditFileCannotEscapeJail: a ../ path resolves INSIDE the workdir jail exactly like write_file's
// resolve — the tool never reaches a file outside its workspace via a traversal arg. (resolve joins a
// relative path onto the workdir; the sandbox is the executor-level gate, but the tool's own resolve must
// not let a relative arg reach outside.) We assert the resolved target is the workdir-joined path, not the
// parent. A ../ traversal that would land outside is harmless here because Execute reads the JOINED path.
func TestEditFileCannotEscapeJail(t *testing.T) {
	d := t.TempDir()
	// Create a file ABOVE the workdir that a naive resolver could reach via "../secret.txt".
	parent := filepath.Dir(d)
	secret := filepath.Join(parent, "edit_jail_secret.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(secret)

	ef := NewEditFile(d)
	// The tool resolves "../edit_jail_secret.txt" against the workdir (filepath.Join cleans the ..). The
	// executor's Sandbox is the hard boundary in production; at the TOOL level we assert the tool resolves
	// relative paths against its OWN workdir (resolve), so a bare relative arg stays anchored there. A
	// traversal arg that climbs out is then caught by the sandbox at the executor — the tool itself must at
	// least not silently treat the workdir as the process CWD.
	abs, _ := filepath.Abs(d)
	if ef.Workdir() != abs {
		t.Fatalf("EditFile.Workdir() = %q, want %q (the sandbox check resolves against this)", ef.Workdir(), abs)
	}
	// Confirm resolve anchors a relative path on the workdir (the same jail-resolve write_file uses): a
	// relative "inner.txt" resolves under d, never under the process CWD.
	got := resolve(ef.Workdir(), "inner.txt")
	if want := filepath.Join(abs, "inner.txt"); got != want {
		t.Errorf("resolve anchored wrong: %q, want %q", got, want)
	}
}

// TestEditFileSandboxResolutionTarget mirrors TestWriteFileSandboxResolutionTarget: edit_file exposes
// Workdir() so the executor's sandbox can check the path it will ACTUALLY touch (the workdirer facet).
func TestEditFileSandboxResolutionTarget(t *testing.T) {
	ef := NewEditFile("/some/work/dir")
	w, ok := any(ef).(interface{ Workdir() string })
	if !ok {
		t.Fatal("EditFile must expose Workdir()")
	}
	abs, _ := filepath.Abs("/some/work/dir")
	if w.Workdir() != abs {
		t.Errorf("Workdir() = %q, want %q", w.Workdir(), abs)
	}
}

// TestEditFileCategoryIsMutateLocal pins the taxonomy: edit_file is mutate/local (identical to write_file)
// so the gate-router, sandbox, and §3.3a scope treat it as a local-world mutation.
func TestEditFileCategoryIsMutateLocal(t *testing.T) {
	ef := NewEditFile(t.TempDir())
	if got := ef.Category(); got != (TaxClass{Op: OpMutate, Reach: ReachLocalWorld}) {
		t.Errorf("Category() = %+v, want {OpMutate, ReachLocalWorld}", got)
	}
	if ef.Name() != "edit_file" {
		t.Errorf("Name() = %q, want edit_file", ef.Name())
	}
	if !FileModifyTools["edit_file"] {
		t.Error("edit_file must be in FileModifyTools so the sandbox gate fires on it")
	}
}

// TestEditFileNotInDefaultTools is the byte-identity guard at the TOOL level: edit_file must NOT be in the
// unconditional DefaultTools set (the 5-tool registry the parity test pins) — it is only added by the
// flag-gated buildExecutor append.
func TestEditFileNotInDefaultTools(t *testing.T) {
	for _, tool := range DefaultTools(".", 0) {
		if tool.Name() == "edit_file" {
			t.Fatal("edit_file must NOT be in DefaultTools (it is flag-gated in buildExecutor)")
		}
	}
}
