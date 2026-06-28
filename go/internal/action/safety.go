// safety.go — the two independent safety gates the executor runs BEFORE any effect (ported from
// action/safety.py, itself a port of lathe's safety/sandbox.go + safety/evaluator.go):
//
//   - Sandbox.Check(path) — a path boundary for file-modifying tools. Resolves symlinks (so a
//     symlink can't escape), always denies protected roots (.git/.venv/.claude/.hg/.svn), and
//     (when allowed roots are configured) denies anything outside them. Hard block, not overridable.
//   - EvaluateCommand(cmd) — a content gate for command tools. Returns a non-empty reason to BLOCK
//     catastrophic commands (rm -rf /, mkfs, dd to a device, fork bomb, curl|sh, eval, …). Pure: no
//     user interaction. Mirrors lathe's Block-severity rows + cross-segment checks.
//
// The classification sets (FileModifyTools / CommandTools) tell the executor which gate applies to
// which tool, exactly as lathe's safety/approval.go does.
package action

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileModifyTools / CommandTools classify which gate applies to which tool (Python's frozensets).
// Membership is checked by the executor; expressed as map[string]bool sets for O(1) lookup.
var (
	FileModifyTools = map[string]bool{"write_file": true, "edit_file": true}
	CommandTools    = map[string]bool{"run_shell": true, "run_tests": true}
)

// protectedDirnames are roots never writable, regardless of allowedRoots (lathe protectedRoots).
var protectedDirnames = []string{".git", ".venv", ".claude", ".hg", ".svn"}

// evalSymlinksLenient resolves symlinks as far as the path exists; it tolerates a not-yet-created
// leaf (we may be about to write it). Mirrors Python's _eval_symlinks_lenient / lathe's
// evalSymlinksLenient.
//
// Go's filepath.EvalSymlinks errors on a missing leaf, so this walks up to the deepest existing
// ancestor, resolves THAT, then rejoins the non-existent suffix — exactly the Python algorithm
// (suffix collected leaf-first, rejoined in reverse so it reads root->leaf).
func evalSymlinksLenient(absPath string) string {
	path := absPath
	var suffix []string // collected deepest-leaf first (matches Python's append order)
	for {
		if _, err := os.Lstat(path); err == nil {
			real, rerr := filepath.EvalSymlinks(path)
			if rerr != nil {
				// Mirror Python os.path.realpath's tolerance: on a resolve failure fall back to the
				// unresolved (already-cleaned) path rather than erroring.
				real = path
			}
			if len(suffix) == 0 {
				return real
			}
			// Rejoin the suffix in root->leaf order (Python: *reversed(suffix)).
			parts := make([]string, 0, len(suffix)+1)
			parts = append(parts, real)
			for i := len(suffix) - 1; i >= 0; i-- {
				parts = append(parts, suffix[i])
			}
			return filepath.Join(parts...)
		}
		parent, leaf := filepath.Split(path)
		parent = strings.TrimSuffix(parent, string(os.PathSeparator))
		if parent == "" {
			parent = string(os.PathSeparator)
		}
		if parent == path { // reached the root
			return absPath
		}
		suffix = append(suffix, leaf)
		path = parent
	}
}

// Sandbox is a path boundary. With no allowed roots, only the protected roots are enforced; with
// allowed roots, every write must resolve inside one of them.
type Sandbox struct {
	allowedRoots []string
}

// NewSandbox builds a sandbox, resolving each allowed root to an absolute, symlink-free path up
// front (Python __init__: realpath(abspath(r))).
func NewSandbox(allowedRoots []string) *Sandbox {
	resolved := make([]string, 0, len(allowedRoots))
	for _, r := range allowedRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = r
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			real = abs // tolerate a not-yet-existing allowed root (matches realpath's leniency)
		}
		resolved = append(resolved, real)
	}
	return &Sandbox{allowedRoots: resolved}
}

// Check returns "" (Python None) if the path may be modified, else a human-readable denial reason.
func (s *Sandbox) Check(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	resolved := evalSymlinksLenient(abs)

	// Protected roots first — always enforced (a .git/.venv/.claude dir on the path).
	parts := make(map[string]bool)
	for _, p := range strings.Split(resolved, string(os.PathSeparator)) {
		parts[p] = true
	}
	for _, d := range protectedDirnames {
		if parts[d] {
			return fmt.Sprintf("path %s is protected (contains %s)", path, d)
		}
	}

	if len(s.allowedRoots) == 0 {
		return "" // no sandbox configured; only protected paths enforced
	}

	for _, root := range s.allowedRoots {
		rootSep := root
		if !strings.HasSuffix(rootSep, string(os.PathSeparator)) {
			rootSep = root + string(os.PathSeparator)
		}
		if resolved == root || strings.HasPrefix(resolved, rootSep) {
			return ""
		}
	}
	return fmt.Sprintf("path %s is outside the sandbox (allowed: %v)", path, s.allowedRoots)
}

// --- command content gate (lathe safety/evaluator.go) --------------------------------------------

var (
	rePipeToShell    = regexp.MustCompile(`\|\s*(sh|bash|zsh)\b`)
	reForkBomb       = regexp.MustCompile(`\w*\(\)\s*\{\s*\w*\s*\|\s*\w*\s*&\s*\}?`)
	reDDToDevice     = regexp.MustCompile(`of=/dev/[a-z]`)
	reOverwriteDev   = regexp.MustCompile(`>\s*/dev/(?:sd|nvme|hd|vd|xvd)`)
	reRmRfRoot       = regexp.MustCompile(`(?i)\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r|-rf|-fr)\b.*\s+(/|/\*|~|\$home)\s*$`)
	reChmod777Sys    = regexp.MustCompile(`\bchmod\s+(-R\s+)?0?777\s+(/|/etc|/usr|/bin|/var|/boot)\b`)
	reMkfs           = regexp.MustCompile(`\bmkfs\b`)
	reProcSubToShell = regexp.MustCompile(`\b(sh|bash)\s+<\(\s*(curl|wget)`)
)

// blockRule is a Block-severity row: a reason + a predicate over the LOWERCASED command. Ported
// from the Python _BLOCK_RULES list of (reason, predicate) tuples, in the same order (order is not
// behaviourally load-bearing — the first match wins and the reasons are distinct — but preserved
// for trace parity).
type blockRule struct {
	Reason string
	Pred   func(c string) bool
}

// blockRules is built once at package init (Python's module-level _BLOCK_RULES). Each predicate
// reproduces the Python lambda exactly, including the substring shortcuts alongside the regexps.
var blockRules = []blockRule{
	{"rm -rf of a root/home path", func(c string) bool {
		return reRmRfRoot.MatchString(c) || strings.Contains(c, " rm -rf /") || strings.HasSuffix(strings.TrimSpace(c), "rm -rf /")
	}},
	{"mkfs (format a filesystem)", func(c string) bool { return reMkfs.MatchString(c) }},
	{"dd to a block device", func(c string) bool { return strings.Contains(c, "dd ") && reDDToDevice.MatchString(c) }},
	{"overwrite a block device", func(c string) bool { return reOverwriteDev.MatchString(c) }},
	{"fork bomb", func(c string) bool {
		return strings.Contains(c, ":(){ :|:") || strings.Contains(c, ":(){ : | :") || reForkBomb.MatchString(c)
	}},
	{"chmod 777 on a system path", func(c string) bool { return reChmod777Sys.MatchString(c) }},
	{"pipe a download straight into a shell", func(c string) bool {
		return (strings.Contains(c, "curl") || strings.Contains(c, "wget")) && rePipeToShell.MatchString(c)
	}},
	{"decode base64 straight into a shell", func(c string) bool {
		return strings.Contains(c, "base64") && rePipeToShell.MatchString(c)
	}},
	{"process-substitution into a shell", func(c string) bool { return reProcSubToShell.MatchString(c) }},
}

// EvaluateCommand returns a non-empty reason to BLOCK a catastrophic command, else "". Pure safety
// gate, no user interaction. Two-phase, so a catastrophe joined into a compound line is still caught:
//
//	Phase 1 (per-segment): TokenizeShellCommand splits the line on ; && || | newlines (quote/escape/
//	  heredoc aware) AND extracts $()/backtick/process-substitution inner commands; each segment is
//	  checked, so a position-anchored rule (e.g. `rm -rf /` must end the string) fires on the hidden
//	  command — "echo ok && rm -rf /", "ls; mkfs /dev/sda", "$(rm -rf /)".
//	Phase 2 (whole command): the pipe-to-shell rules (curl|sh, base64|sh, sh <(curl)) span a segment
//	  boundary — the pipe IS the signal — so they are checked on the unsplit command (this also keeps
//	  the original whole-string behaviour as a backstop).
func EvaluateCommand(command string) string {
	if strings.TrimSpace(command) == "" {
		return ""
	}
	// Phase 1 — per-segment.
	for _, seg := range TokenizeShellCommand(command) {
		low := strings.ToLower(seg)
		for _, rule := range blockRules {
			if rule.Pred(low) {
				return rule.Reason
			}
		}
		// Hardened argv-normalized guards (safety_hardened.go): close the regex evasions (case-fold
		// -R, /etc-class roots, split/long flags) the blockRules above miss. Purely ADDITIVE — it
		// runs on the RAW (case-preserving) segment so its own normalization is authoritative, and it
		// only ever ADDS a block, so EvaluateCommand stays byte-identical-or-stricter.
		if reason := hardenedDestructive(seg); reason != "" {
			return reason
		}
	}
	// Phase 2 — whole command (catches the cross-segment pipe-to-shell patterns).
	low := strings.ToLower(command)
	for _, rule := range blockRules {
		if rule.Pred(low) {
			return rule.Reason
		}
	}
	return ""
}
