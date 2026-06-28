// readdocument.go — the read_document SHELL-OUT tool (capability-enhancement T2.3).
//
// read_document reads a NON-PLAINTEXT document (PDF / xlsx / docx / …) and returns its TEXT, so a
// GAIA-style "open this attached file and answer" task becomes reachable. It is the sibling of
// run_tests (which shells a real pytest): rather than embed a heavy Go PDF/office parser, it SHELLS
// OUT to whatever document parser is on the host PATH — poppler's `pdftotext` for PDF, LibreOffice's
// headless converter for office formats — exactly the way run_tests drives the host's python+pytest.
//
// Best-effort by contract. There is a deterministic, ALWAYS-available path for text-shaped files
// (.txt/.md/.csv/…): read the bytes directly, no parser needed. For a binary document type whose
// parser is NOT installed, read_document returns a CLEAR error naming the missing parser ("no parser
// for .pdf — install poppler/pdftotext") — it never crashes and never fabricates text. A parser that
// runs but fails / exits non-zero / yields nothing is likewise a clean error ToolResult, with the file
// left untouched.
//
// Category inspect/local (identical to read_file): reading a document changes nothing and reaches only
// the local workspace — it is NOT a mutation (read_document is deliberately absent from FileModifyTools),
// so the gate-router/sandbox treat it as a free local sense, the same as read_file.
//
// Determinism: the plaintext path is pure stdlib I/O (deterministic — assertable in CI). The shell-out
// paths are environment-dependent (a parser may or may not be installed) and are therefore BEST-EFFORT,
// not asserted by the offline suite; the parser-availability check (parserFor) is a pure, unit-testable
// function so the no-parser branch is covered without depending on poppler/libreoffice being present.
package action

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// maxDocOutput caps the extracted document text (codepoints) — a document read is a BOUNDED sense, never
// an unbounded dump that floods the thought graph. Smaller than the generic maxOutput (20k) because an
// extracted document is grounding context for a question, not a file to edit: ~8000 runes is several
// pages of prose, enough to answer a GAIA-style file question without swamping the context window.
const maxDocOutput = 8000

// docReadTimeout bounds the parser shell-out (the wall-clock guard, like RunShell's): a hung/huge
// converter can't stall a tick. The plaintext path does not shell out and is not subject to it.
const docReadTimeout = 60 * time.Second

// clipDoc truncates extracted text to maxDocOutput runes, appending a truncation marker carrying the
// true total length (the read_document analogue of clip — a separate, smaller cap so the generic
// 20k-rune clip on tool output is unaffected).
func clipDoc(text string) string {
	r := []rune(text)
	if len(r) <= maxDocOutput {
		return text
	}
	head := string(r[:maxDocOutput-200])
	return fmt.Sprintf("%s\n… [document truncated, %d chars total]", head, len(r))
}

// ReadDocument reads a document from the workspace and returns its extracted text. It holds a workdir
// (the jail) and a timeout for the parser shell-out.
type ReadDocument struct {
	baseTool
	timeout time.Duration
}

// NewReadDocument constructs a read_document tool scoped to workdir, with the given parser shell-out
// timeout (0 / negative -> the docReadTimeout default).
func NewReadDocument(workdir string, timeout time.Duration) *ReadDocument {
	if timeout <= 0 {
		timeout = docReadTimeout
	}
	return &ReadDocument{baseTool: newBase(workdir), timeout: timeout}
}

func (t *ReadDocument) Name() string { return "read_document" }

// Category: reading a document changes nothing and reaches only the local workspace (inspect/local) —
// the SAME tag read_file carries (it is a read, NOT a mutation; absent from FileModifyTools). This
// matches the name-only classifyCall fallback (default -> inspect/local) so the two taxonomies never
// drift when the tool is registered.
//
// HONEST POSTURE NOTE (red-team T2.3): unlike read_file, this read EXECS a native parser (pdftotext /
// libreoffice). Tagging it inspect/local means the auto-permission gate AUTO-APPROVES those parsers,
// whereas run_shell would ESCALATE them (they are NOT on commandAllowlist). This is a deliberate,
// bounded surface-area increase — the parser binary + flags are HARDCODED (only the path varies, and it
// is forced absolute + Stat-prechecked, so no command/argument injection), and the residual risk is a
// parser-CVE on an attacker-supplied document, bounded by the wall-clock timeout above. The model can
// already auto-run python/node parsers, so this does not open a new class of capability; it is opt-in
// (default-OFF) regardless.
func (t *ReadDocument) Category() TaxClass { return TaxClass{Op: OpInspect, Reach: ReachLocalWorld} }

func (t *ReadDocument) Description() string {
	return "Read a document (PDF, xlsx, docx, pptx, or a text file) from the workspace and return its " +
		"text. Use for attached/binary files a plain read_file cannot show; shells out to an installed " +
		"parser (poppler/LibreOffice) for non-text formats and returns a clear error if none is available."
}

func (t *ReadDocument) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "document path (relative to the workspace)"},
		},
		"required": []any{"path"},
	}
}

// plaintextExt is the set of extensions read DIRECTLY (no parser needed — the deterministic, always-
// available path). These are already-text formats; a read_document on one is just read_file with the
// document-cap, so a model that reaches for read_document on a .txt/.csv still gets its content.
var plaintextExt = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".tsv": true,
	".json": true, ".log": true, ".text": true, ".rst": true, ".yaml": true, ".yml": true,
}

// docParser names a host parser shell-out for a document type: the binary to LookPath plus a function
// that builds its argv given the resolved (absolute) document path. The argv emits extracted text to
// STDOUT (so read_document captures it the way run_tests captures pytest's output).
type docParser struct {
	bin  string                    // the binary that must be on PATH (exec.LookPath)
	argv func(abs string) []string // argv emitting the document's text to stdout
}

// parsersFor returns, for a document extension, the ordered list of candidate host parsers (first
// available wins). It is a PURE function — no I/O — so the routing + the no-parser error are unit-
// testable without poppler/libreoffice installed. An unknown / unsupported extension returns nil.
//
//	.pdf                         -> pdftotext <path> - (poppler; the canonical PDF->text extractor)
//	.xlsx/.docx/.pptx/.odt/...   -> libreoffice|soffice --headless --convert-to txt --cat <path>
//	                                (LibreOffice's headless converter; --cat writes the txt to stdout)
func parsersFor(ext string) []docParser {
	switch ext {
	case ".pdf":
		return []docParser{
			{bin: "pdftotext", argv: func(abs string) []string { return []string{abs, "-"} }},
		}
	case ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".odt", ".ods", ".odp", ".rtf":
		// LibreOffice headless: --cat converts to txt and writes it to stdout (no temp file to clean up).
		// soffice is the same binary under its alternate name on some installs — try both.
		mk := func(bin string) docParser {
			return docParser{bin: bin, argv: func(abs string) []string {
				return []string{"--headless", "--convert-to", "txt", "--cat", abs}
			}}
		}
		return []docParser{mk("libreoffice"), mk("soffice")}
	default:
		return nil
	}
}

// resolveParser returns the first candidate parser for ext whose binary is on PATH, plus the resolved
// binary path. ok=false means EITHER the extension is unsupported (parsersFor returned nil) OR no
// candidate binary is installed — the caller distinguishes the two for the error message. Split out so
// the availability check is independent of Execute (testable, and reused by a future doctor probe).
func resolveParser(ext string) (parser docParser, binPath string, ok bool) {
	for _, p := range parsersFor(ext) {
		if bp, err := exec.LookPath(p.bin); err == nil {
			return p, bp, true
		}
	}
	return docParser{}, "", false
}

func (t *ReadDocument) Execute(args map[string]any) ToolResult {
	path := strings.TrimSpace(argStr(args, "path"))
	if path == "" {
		return ToolResult{Name: t.Name(), Content: "missing 'path'", IsError: true, ErrorCode: ErrBadArgs}
	}
	full := resolve(t.workdir, path) // the workdir jail — identical to read_file/write_file (never escapes)
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("could not find %q at that path", path)
			if listing := workspaceListing(t.workdir, 40); len(listing) > 0 {
				msg += " — the workspace contains: " + strings.Join(listing, ", ")
			} else {
				msg += " — the workspace is empty"
			}
			return ToolResult{Name: t.Name(), Content: msg, IsError: true, ErrorCode: ErrUnavailable}
		}
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot read %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
	}

	ext := strings.ToLower(filepath.Ext(full))

	// PLAINTEXT PATH (deterministic, always available): an already-text format is read directly — no
	// parser needed — and capped. This is the CI-assertable path; it never shells out.
	if plaintextExt[ext] {
		raw, err := os.ReadFile(full)
		if err != nil {
			return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot read %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
		}
		text := strings.TrimRight(decodeUTF8Replace(raw), "\n")
		if strings.TrimSpace(text) == "" {
			return ToolResult{Name: t.Name(), Content: fmt.Sprintf("%s is empty", path), IsError: true, ErrorCode: ErrUnavailable}
		}
		return ToolResult{Name: t.Name(), Content: clipDoc(text)}
	}

	// PARSER PATH (best-effort, environment-dependent): route by extension to a host parser. No parser
	// for the type (unsupported ext OR none installed) -> a CLEAR best-effort error naming the parser to
	// install. This branch is NOT asserted by the offline suite (it depends on the host), but resolveParser
	// is unit-testable so the no-parser error IS covered.
	parser, binPath, ok := resolveParser(ext)
	if !ok {
		return ToolResult{Name: t.Name(), Content: noParserMessage(ext), IsError: true, ErrorCode: ErrUnavailable}
	}

	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, parser.argv(full)...)
	cmd.Dir = t.workdir
	// Own process group AND, on timeout, SIGKILL THE WHOLE GROUP — not just the direct child. A converter
	// like LibreOffice forks a DETACHED soffice.bin that inherits the stdout pipe; the default
	// CommandContext cancel kills only the direct child PID, leaving that grandchild alive holding the
	// pipe, so cmd.Run()'s Wait would block FAR past the deadline (a converter could stall a tick — the
	// durability bounded-dead-time invariant). Cancel kills the group (covers the still-running case);
	// WaitDelay force-closes the inherited pipes so Wait returns even when a grandchild lingers after the
	// direct child has already exited (the launcher-forks-then-exits case).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // negative pid ⇒ the whole process group
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return ToolResult{Name: t.Name(),
			Content: fmt.Sprintf("reading %s timed out after %s (the %s parser hung)", path, formatTimeout(t.timeout), parser.bin),
			IsError: true, ErrorCode: ErrTimeout}
	}
	if err != nil {
		// A parser failure (non-zero exit / launch error) is a clean best-effort error — the file is
		// untouched. Surface the parser's own stderr (clipped) so the conscious sees WHY it failed.
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return ToolResult{Name: t.Name(),
			Content: fmt.Sprintf("could not parse %s with %s: %s", path, parser.bin, clipDoc(detail)),
			IsError: true, ErrorCode: ErrUnavailable}
	}
	text := strings.TrimRight(decodeUTF8Replace(stdout.Bytes()), "\n")
	if strings.TrimSpace(text) == "" {
		return ToolResult{Name: t.Name(),
			Content: fmt.Sprintf("%s parsed to empty text (the document may be scanned/image-only or unsupported by %s)", path, parser.bin),
			IsError: true, ErrorCode: ErrUnavailable}
	}
	return ToolResult{Name: t.Name(), Content: clipDoc(text)}
}

// noParserMessage builds the best-effort "no parser available" error for an extension, naming the
// concrete package to install so the conscious (or the operator) can act on it. An unsupported
// extension (parsersFor returns nil) gets the generic guidance.
func noParserMessage(ext string) string {
	switch ext {
	case ".pdf":
		return fmt.Sprintf("no parser for %s — install poppler (the pdftotext binary) to read PDF documents", ext)
	case ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".odt", ".ods", ".odp", ".rtf":
		return fmt.Sprintf("no parser for %s — install LibreOffice (the libreoffice/soffice binary) to read office documents", ext)
	case "":
		return "read_document needs a file extension to choose a parser (e.g. .pdf, .docx); a text file can be read with read_file"
	default:
		return fmt.Sprintf("no parser for %s — read_document supports PDF (poppler) and office formats (LibreOffice); a text file (.txt/.md/.csv) can be read with read_file", ext)
	}
}
