// builtins.go — the five concrete tools, the real effectors (ported from action/builtins.py,
// itself a port of lathe's tools/{file,shell}.go).
//
// Each performs a GENUINE effect via the stdlib and returns the real captured output. None of them
// fabricate: run_tests shells a real pytest and parses its real summary; run_shell returns the
// real exit code even on failure; read/write touch real bytes; search greps the real tree. All
// effects are scoped to a workdir (the workspace / sandbox root) so relative paths resolve
// predictably.
//
// Import-pure: CONSTRUCTING a tool does nothing; the effect happens only inside Execute. The
// structs hold only configuration (workdir, timeout); no goroutine, file handle, or subprocess is
// created until Execute runs.
package action

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

// maxOutput caps captured output so a runaway command can't flood the graph (Python _MAX_OUTPUT).
const maxOutput = 20_000

// clip truncates text to maxOutput chars (codepoints, matching Python's str-slice). Reproduces
// Python _clip: keep the first maxOutput-200 runes, append a truncation marker carrying the true
// total length.
func clip(text string) string {
	r := []rune(text)
	if len(r) <= maxOutput {
		return text
	}
	head := string(r[:maxOutput-200])
	return fmt.Sprintf("%s\n… [output truncated, %d chars total]", head, len(r))
}

// workspaceListing returns up to max relative file paths under root (slash-form, sorted) — the honest
// "here is what IS here" recovery context for a tool error. A failing tool does NOT magically resolve a
// path; it tells the conscious what the workspace actually contains, and the conscious re-issues the call
// at the right path on its next thought (act -> observe -> reason -> re-act; the intelligence stays in the
// conscious, the tool stays a dumb honest effector).
func workspaceListing(root string, max int) []string {
	var files []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if rel, e := filepath.Rel(root, p); e == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	if len(files) > max {
		files = files[:max]
	}
	return files
}

// resolve resolves a tool arg path against the tool's workdir (Python _resolve): an absolute path
// is used as-is; a relative path is joined onto workdir.
func resolve(workdir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workdir, path)
}

// baseTool holds the shared workdir config (Python _Base). The workdir is stored absolute up front
// (Python os.path.abspath(workdir)), so all five tools resolve relative paths identically.
type baseTool struct {
	workdir string
}

func newBase(workdir string) baseTool {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = workdir
	}
	return baseTool{workdir: abs}
}

// Workdir exposes the workspace root for the executor's sandbox gate (the workdirer facet — the Go
// stand-in for Python's getattr(tool, "workdir", None)).
func (b baseTool) Workdir() string { return b.workdir }

// --- read_file -----------------------------------------------------------------------------------

// ReadFile reads a file from the workspace and returns its contents.
type ReadFile struct{ baseTool }

// NewReadFile constructs a read_file tool scoped to workdir.
func NewReadFile(workdir string) *ReadFile { return &ReadFile{newBase(workdir)} }

func (t *ReadFile) Name() string { return "read_file" }

// Category: a file read changes nothing and touches only the local world (inspect/local) — a free
// local sense (gap 6: the tool owns its taxonomy, replacing the name-switch guess).
func (t *ReadFile) Category() TaxClass { return TaxClass{Op: OpInspect, Reach: ReachLocalWorld} }
func (t *ReadFile) Description() string {
	return "Read a file from the workspace and return its contents."
}
func (t *ReadFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "file path (relative to the workspace)"},
		},
		"required": []any{"path"},
	}
}

func (t *ReadFile) Execute(args map[string]any) ToolResult {
	path := strings.TrimSpace(argStr(args, "path"))
	if path == "" {
		return ToolResult{Name: t.Name(), Content: "missing 'path'", IsError: true, ErrorCode: ErrBadArgs}
	}
	raw, err := os.ReadFile(resolve(t.workdir, path))
	if err == nil {
		return t.readContent(path, raw, "")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot read %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
	}
	// PATH-MISS RECOVERY (the grounding de-flake): the intent named a file that is not at this EXACT
	// path. If EXACTLY ONE file in the workspace carries the requested basename, it is the unambiguous
	// target — read it ("risk.yaml" -> the only risk.yaml is config/risk.yaml). Deterministic + safe
	// (fires only on a UNIQUE basename match) + disclosed (the resolution is noted in the content, not
	// silent). This removes the re-read round that made a bare-name read flaky against the tick budget;
	// a bare name that is ambiguous or absent still returns the honest listing below.
	if rel, ok := resolveByBasename(t.workdir, path); ok {
		if raw2, e2 := os.ReadFile(resolve(t.workdir, rel)); e2 == nil {
			return t.readContent(rel, raw2, fmt.Sprintf("(resolved %q -> %s)\n", path, rel))
		}
	}
	// HONEST + RECOVERABLE: a path MISS, not a nonexistent file — say so, and list what the workspace
	// actually holds so the conscious can re-read at the right path next thought.
	msg := fmt.Sprintf("could not find %q at that path", path)
	if listing := workspaceListing(t.workdir, 40); len(listing) > 0 {
		msg += " — the workspace contains: " + strings.Join(listing, ", ")
	} else {
		msg += " — the workspace is empty"
	}
	return ToolResult{Name: t.Name(), Content: msg, IsError: true, ErrorCode: ErrUnavailable}
}

// readContent builds the success ToolResult for read_file: a NUL byte in the first 4096 bytes flags a
// binary file (Python's b"\x00" in raw[:4096]); otherwise the bytes are decoded utf-8 with replacement
// and clipped. `note` (empty on a direct hit) prepends the path-resolution disclosure. Factored so the
// direct-read and basename-resolved paths share one content path — the empty-note case is byte-identical
// to the pre-fix behaviour, so existing goldens are unchanged.
func (t *ReadFile) readContent(path string, raw []byte, note string) ToolResult {
	probe := raw
	if len(probe) > 4096 {
		probe = probe[:4096]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("%s%s: binary file (%d bytes), not shown", note, path, len(raw))}
	}
	return ToolResult{Name: t.Name(), Content: note + clip(decodeUTF8Replace(raw))}
}

// resolveByBasename returns the workspace-relative path of the UNIQUE file whose basename equals the
// requested path's basename, or ok=false when there is no match OR more than one (ambiguous — the
// caller then returns the honest listing rather than guess). Deterministic (a sorted walk). Only the
// basename is compared, so "risk.yaml" finds "config/risk.yaml", and "wrong/dir/q3.txt" still finds the
// unique "data/sets/q3.txt". The match is the file the intent unambiguously named — not magic.
func resolveByBasename(root, path string) (string, bool) {
	want := filepath.Base(filepath.FromSlash(strings.TrimSpace(path)))
	if want == "" || want == "." || want == string(filepath.Separator) {
		return "", false
	}
	var matches []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(p) == want {
			if rel, e := filepath.Rel(root, p); e == nil {
				matches = append(matches, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

// --- write_file ----------------------------------------------------------------------------------

// WriteFile writes (creates or overwrites) a file in the workspace.
type WriteFile struct{ baseTool }

// NewWriteFile constructs a write_file tool scoped to workdir.
func NewWriteFile(workdir string) *WriteFile { return &WriteFile{newBase(workdir)} }

func (t *WriteFile) Name() string { return "write_file" }

// Category: a file write alters the local world (mutate/local) — a world-change (gap 6).
func (t *WriteFile) Category() TaxClass { return TaxClass{Op: OpMutate, Reach: ReachLocalWorld} }
func (t *WriteFile) Description() string {
	return "Write (create or overwrite) a file in the workspace with the given contents."
}
func (t *WriteFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []any{"path", "content"},
	}
}

func (t *WriteFile) Execute(args map[string]any) ToolResult {
	path := strings.TrimSpace(argStr(args, "path"))
	if path == "" {
		return ToolResult{Name: t.Name(), Content: "missing 'path'", IsError: true, ErrorCode: ErrBadArgs}
	}
	// Python: content = args.get("content", ""); if not a str, str(content). anyToStr reproduces
	// the str() coercion of a non-string payload.
	content := anyToStr(args["content"])
	full := resolve(t.workdir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot write %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot write %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
	}
	// Python reports len(content) — codepoints, not bytes; use rune count to match.
	return ToolResult{Name: t.Name(), Content: fmt.Sprintf("wrote %d bytes to %s", len([]rune(content)), path)}
}

// --- run_shell -----------------------------------------------------------------------------------

// RunShell executes a shell command in the workspace and returns its output (stdout + stderr +
// exit code).
type RunShell struct {
	baseTool
	timeout time.Duration
}

// NewRunShell constructs a run_shell tool scoped to workdir with a wall-clock timeout (Python
// default 30s).
func NewRunShell(workdir string, timeout time.Duration) *RunShell {
	return &RunShell{baseTool: newBase(workdir), timeout: timeout}
}

func (t *RunShell) Name() string { return "run_shell" }

// Category: a shell run executes on the local machine (execute/local) — a grounding probe inside the
// sandbox (the SANDBOX, not this tag, distinguishes a sandbox-escaping run, 03 §6). RunTests embeds
// RunShell, so it inherits this category (execute/local) without its own method (gap 6).
func (t *RunShell) Category() TaxClass { return TaxClass{Op: OpExecute, Reach: ReachLocalWorld} }
func (t *RunShell) Description() string {
	return "Execute a shell command in the workspace and return its output (stdout + stderr + " +
		"exit code). Use for builds, git, and system operations — not for editing files."
}
func (t *RunShell) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "the shell command to run"},
		},
		"required": []any{"command"},
	}
}

// run is the shared subprocess core (Python RunShell._run). RunTests composes RunShell through this
// method (Go embedding, not inheritance) — the result is stamped with `name` so a RunTests result
// reads "run_tests" even though the work happens here.
//
// Effects (the subprocess) happen ONLY here, inside an Execute call. The command runs under
// /bin/sh -lc in its own process group (Setpgid == Python start_new_session=True) so a timeout can
// be enforced and stray children don't outlive the call. The timeout is a context deadline
// (Python's subprocess timeout).
func (t *RunShell) run(name, command string) ToolResult {
	if strings.TrimSpace(command) == "" {
		return ToolResult{Name: name, Content: "empty command", IsError: true, ErrorCode: ErrBadArgs}
	}
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	cmd.Dir = t.workdir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own session/process group (start_new_session)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return ToolResult{Name: name,
			Content:   fmt.Sprintf("command timed out after %s: %s", formatTimeout(t.timeout), command),
			IsError:   true,
			ErrorCode: ErrTimeout,
		}
	}

	// An OSError-class failure (the binary couldn't be launched) — distinct from a non-zero exit.
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return ToolResult{Name: name, Content: fmt.Sprintf("cannot run command: %v", err), IsError: true, ErrorCode: ErrUnavailable}
	}

	rc := 0
	if exitErr != nil {
		rc = exitErr.ExitCode()
	}

	var parts []string
	if so := strings.TrimRight(stdout.String(), "\n"); so != "" {
		parts = append(parts, so)
	}
	if se := strings.TrimRight(stderr.String(), "\n"); se != "" {
		parts = append(parts, "STDERR:\n"+se)
	}
	out := strings.Join(parts, "\n")
	if out == "" {
		out = "(no output)"
	}
	if rc != 0 {
		out += fmt.Sprintf("\n(exit code %d)", rc)
	}
	// Non-zero exit is NOT an error_code — it is a real reality signal (IsError reflects it).
	exit := rc
	return ToolResult{Name: name, Content: clip(out), IsError: rc != 0, ExitCode: &exit}
}

func (t *RunShell) Execute(args map[string]any) ToolResult {
	return t.run(t.Name(), argStr(args, "command"))
}

// --- run_tests -----------------------------------------------------------------------------------

// RunTests runs the workspace's test suite. The single most important real effect: a failing test
// is a genuine ok=False observation that can REFUTE the closed loop's optimistic guess. It composes
// RunShell via the shared run() (Go embedding, NOT inheritance).
type RunTests struct {
	RunShell
}

// NewRunTests constructs a run_tests tool scoped to workdir.
func NewRunTests(workdir string, timeout time.Duration) *RunTests {
	return &RunTests{RunShell: *NewRunShell(workdir, timeout)}
}

func (t *RunTests) Name() string { return "run_tests" }
func (t *RunTests) Description() string {
	return "Run the workspace's test suite (pytest) and return the real pass/fail summary + exit code."
}
func (t *RunTests) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{"type": "string", "description": "optional path/expr to pass to pytest"},
		},
		"required": []any{},
	}
}

func (t *RunTests) Execute(args map[string]any) ToolResult {
	target := strings.TrimSpace(argStr(args, "target"))
	// Python uses sys.executable to drive pytest (a bare "python" is often absent on macOS / the
	// login-shell PATH). Go has no interpreter-of-record; resolve a Python at call time (python3,
	// then python) so the workspace's suite runs with whatever interpreter is on PATH. This is the
	// one necessary deviation from the Python source — see the port notes.
	py := pythonInterpreter()
	cmd := py + " -m pytest -q"
	if target != "" {
		cmd += " " + target
	}
	// run() stamps the result with "run_tests" (passed explicitly, since RunTests overrides Name()).
	return t.run(t.Name(), cmd)
}

// pythonInterpreter resolves the interpreter run_tests drives pytest through: python3 if present on
// PATH, else python, else the bare "python3" (so the run fails with a clear "cannot run command").
func pythonInterpreter() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			if strings.ContainsAny(p, " \t") {
				return quoteCmd(p) // a space-containing path must be quoted for /bin/sh -lc
			}
			return p
		}
	}
	return "python3"
}

// --- search --------------------------------------------------------------------------------------

// Search searches the workspace for a regex pattern and returns matching file:line:text hits.
type Search struct{ baseTool }

// NewSearch constructs a search tool scoped to workdir.
func NewSearch(workdir string) *Search { return &Search{newBase(workdir)} }

func (t *Search) Name() string { return "search" }

// Category: a workspace search reads the local tree and changes nothing (inspect/local) — a free local
// sense (gap 6).
func (t *Search) Category() TaxClass { return TaxClass{Op: OpInspect, Reach: ReachLocalWorld} }
func (t *Search) Description() string {
	return "Search the workspace for a regex pattern and return matching file:line:text hits."
}
func (t *Search) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string"},
			"glob":    map[string]any{"type": "string", "description": "optional path glob"},
		},
		"required": []any{"pattern"},
	}
}

// searchSkipDirs mirrors the Python dir prune list (.git/.venv/__pycache__/node_modules).
var searchSkipDirs = map[string]bool{".git": true, ".venv": true, "__pycache__": true, "node_modules": true}

// reTestPath matches a search-hit path (a "file:line:text" hit, or a bare path) that lives in a TEST
// or FIXTURE file — a Go/Python/JS/Rust test file (foo_test.go / test_foo.py / foo.test.ts / foo_spec.rb),
// or anything under a test/tests/testdata/fixtures/golddata/mocks/__tests__ directory. These are where a
// symbol's value is OVERRIDDEN for a test (e.g. `LearnRate = 0.5` in a regulator test) rather than the
// canonical declaration (`LearnRate = 0.05` in config). The path is the leading "file:line:" segment of a
// hit; the regex is anchored on the whole hit so it matches the file portion of "x/y_test.go:12:...".
var reTestPath = regexp.MustCompile(`(?i)(?:^|[\\/])(?:tests?|testdata|fixtures?|golddata|mocks?|__tests__|spec)[\\/]|(?:^|[\\/])test_[^\\/:]*|_test\.[a-z0-9]+(?::|$)|\.(?:test|spec)\.[a-z0-9]+(?::|$)|_spec\.[a-z0-9]+(?::|$)`)

// isTestHit reports whether a search-hit line (or path) lives in a test/fixture file. Used to order the
// CANONICAL (production) hits ahead of test-file hits so the first match the conscious reads is the
// symbol's real declaration, not a test override — the #43 test-vs-production read-disambiguation gap.
func isTestHit(hit string) bool {
	// Compare only the path portion (before the first ":line:") so the matched-text never trips it.
	path := hit
	if i := strings.IndexByte(hit, ':'); i > 0 {
		path = hit[:i]
	}
	return reTestPath.MatchString(path)
}

// canonicalFirst returns the hits reordered so production (non-test) hits precede test/fixture hits,
// preserving each group's original relative order (a STABLE partition). Deterministic — it depends only
// on the hit text, no clock/randomness — so the golden oracle + the stability ticks stay reproducible.
// This is the #43 fix: a search for `LearnRate` whose test override (`LearnRate=0.5`) sorted first now
// surfaces the canonical `LearnRate=0.05` ahead of it, so the conscious extracts the production value.
func canonicalFirst(hits []string) []string {
	canon := make([]string, 0, len(hits))
	tests := make([]string, 0, len(hits))
	for _, h := range hits {
		if isTestHit(h) {
			tests = append(tests, h)
		} else {
			canon = append(canon, h)
		}
	}
	return append(canon, tests...)
}

// splitNonEmptyLines splits ripgrep's stdout into its hit lines, dropping any empty line, so
// canonicalFirst partitions a clean list (the join back to a single Content string is unchanged in
// shape — same lines, only the production-before-test order differs).
func splitNonEmptyLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

func (t *Search) Execute(args map[string]any) ToolResult {
	pattern := strings.TrimSpace(argStr(args, "pattern"))
	if pattern == "" {
		return ToolResult{Name: t.Name(), Content: "missing 'pattern'", IsError: true, ErrorCode: ErrBadArgs}
	}

	// Prefer ripgrep (Python shutil.which("rg")); fall through to the stdlib walk on its absence or
	// any error.
	if rg, err := exec.LookPath("rg"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, rg, "-n", "--no-heading", pattern, t.workdir)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		var out bytes.Buffer
		cmd.Stdout = &out
		// ripgrep exits 1 on "no matches" (not an OSError) — that's fine, we read its stdout either
		// way; only a launch failure / timeout falls through to the walk.
		runErr := cmd.Run()
		var exitErr *exec.ExitError
		if ctx.Err() != context.DeadlineExceeded && (runErr == nil || errors.As(runErr, &exitErr)) {
			hits := strings.TrimSpace(out.String())
			if hits != "" {
				// CANONICAL-FIRST (#43): order production hits ahead of test/fixture hits so the first
				// line the conscious reads is the symbol's real declaration, not a test override
				// (deterministic, stable partition — see canonicalFirst). ripgrep groups by file, so we
				// reorder its line list rather than trust its native order.
				ordered := canonicalFirst(splitNonEmptyLines(hits))
				return ToolResult{Name: t.Name(), Content: clip(strings.Join(ordered, "\n"))}
			}
			return ToolResult{Name: t.Name(), Content: fmt.Sprintf("no matches for %q in the workspace", pattern)}
		}
		// otherwise fall through to the stdlib walk
	}

	rx, err := regexp.Compile(pattern)
	if err != nil {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("bad pattern: %v", err), IsError: true, ErrorCode: ErrBadArgs}
	}

	var hits []string
	_ = filepath.Walk(t.workdir, func(fp string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Python's per-file OSError is swallowed (continue); skip on walk errors too
		}
		if info.IsDir() {
			if searchSkipDirs[info.Name()] && fp != t.workdir {
				return filepath.SkipDir
			}
			return nil
		}
		f, oerr := os.Open(fp)
		if oerr != nil {
			return nil // Python except OSError: continue
		}
		defer f.Close()
		lineNo := 0
		sc := newLineScanner(f)
		for sc.Scan() {
			lineNo++
			line := sc.Text()
			if rx.MatchString(line) {
				rel, rerr := filepath.Rel(t.workdir, fp)
				if rerr != nil {
					rel = fp
				}
				hits = append(hits, fmt.Sprintf("%s:%d:%s", rel, lineNo, strings.TrimRight(line, " \t\r\n\f\v")))
				if len(hits) >= 200 {
					// Python breaks the inner per-line loop (stops reading this file) at 200, then
					// keeps walking; replicate by breaking out of the line loop only.
					break
				}
			}
		}
		return nil
	})

	if len(hits) == 0 {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("no matches for %q in the workspace", pattern)}
	}
	// CANONICAL-FIRST (#43): same production-before-test ordering as the ripgrep path, so the two search
	// backends agree on which hit leads (the symbol's declaration, not a test override).
	return ToolResult{Name: t.Name(), Content: clip(strings.Join(canonicalFirst(hits), "\n"))}
}

// DefaultTools returns the five builtins scoped to a workspace directory (Python default_tools).
func DefaultTools(workdir string, timeout time.Duration) []Tool {
	return []Tool{
		NewReadFile(workdir),
		NewWriteFile(workdir),
		NewRunShell(workdir, timeout),
		NewRunTests(workdir, timeout),
		NewSearch(workdir),
	}
}
