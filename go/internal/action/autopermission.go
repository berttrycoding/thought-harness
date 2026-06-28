// autopermission.go — the tiered AUTO-PERMISSION policy (SECURITY-SANDBOX, 2026-06-21, board row
// SECURITY-SANDBOX; design doc 2026-06-21-preagi-harness-levels-roadmap.md §1.5 the access ladder).
//
// THE KEY INSIGHT: sandbox + auto-permission are two sides of one coin. You can remove the human
// from the per-call approval loop ONLY because the sandbox + automated gates confine what a call
// can do. This file is the classifier that decides, per call:
//
//	SAFE      — read-only / sandboxed-reversible / allowlisted, in-jail  ⇒ AUTO-APPROVE (no prompt),
//	            the "auto mode" that lets autonomous/awake mode act without per-call approval.
//	DANGEROUS — irreversible / outward-facing / out-of-sandbox / write-outside-workspace / a
//	            non-allowlisted or hardened-destructive command ⇒ ESCALATE (in a headless/autonomous
//	            context = DENY + emit action.escalate for later human review / higher-autonomy
//	            pre-authorization).
//
// It is an ALLOWLIST, not a blocklist: a command is SAFE only if its program is on the curated
// allowlist AND its argv shape is benign AND its targets stay in the jail. Everything else escalates.
// An allowlist cannot be evaded by a regex-dodge (case-fold / split flags / a novel destructive
// command) — an unknown program is escalated by construction, which is the structural fix for the
// known blocklist evasions.
//
// Pattern A (pure CONTROL): the classification is closed-form (allowlist membership + argv + jail
// containment), NO model. It is layered INSIDE the existing executor pipeline (executor.go) — it
// does not replace the sandbox / safety / gate-router stages; it is the FIRST gate when enabled, and
// is OFF by default (nil policy ⇒ byte-identical to today's pipeline).
package action

import (
	"path/filepath"
	"strings"
)

// Permission is the tier a tool call classifies into.
type Permission int

const (
	// PermSafe — read-only / in-jail-reversible / allowlisted: AUTO-APPROVE, no human prompt.
	PermSafe Permission = iota
	// PermDangerous — irreversible / out-of-jail / non-allowlisted: ESCALATE (deny + event in headless).
	PermDangerous
)

// String returns a stable label for the tier (events / traces / tests).
func (p Permission) String() string {
	if p == PermSafe {
		return "safe"
	}
	return "dangerous"
}

// commandAllowlist is the curated set of program names that are SAFE to run with no human prompt:
// read-only / inspection / build-and-test commands whose worst case is captured by the sandbox + the
// timeout. A program NOT on this list is escalated (the allowlist IS the gate — an unknown or novel
// destructive command can never auto-pass). Deliberately small: the harness's real run_shell use is
// build/test/inspection. (A mutating-but-reversible-in-jail command like a workspace `rm -rf build`
// is handled by the in-jail target check below, not by allowlisting `rm` outright — `rm` stays OFF
// the allowlist so an out-of-jail `rm` always escalates.)
var commandAllowlist = map[string]bool{
	// build / test / language tooling
	"go": true, "gofmt": true, "gotest": true, "python": true, "python3": true,
	"pytest": true, "node": true, "npm": true, "pnpm": true, "yarn": true,
	"cargo": true, "rustc": true, "make": true, "bazel": true,
	// read-only inspection
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true, "stat": true,
	"file": true, "echo": true, "printf": true, "pwd": true, "date": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true,
	"find": true, "tree": true, "diff": true, "cmp": true, "sort": true, "uniq": true,
	"cut": true, "awk": true, "sed": true, "tr": true, "basename": true, "dirname": true,
	"git": true, "true": true, "false": true, "test": true, "which": true, "type": true,
	"env": true, "printenv": true, "uname": true, "hostname": true, "id": true, "whoami": true,
}

// inJailMutators are programs that DO write but whose write is confined + reversible WHEN their
// operands stay inside the sandbox jail (a workspace-scoped mkdir/touch/cp/mv/rm of a build dir). They
// are SAFE only if EVERY path operand resolves inside the jail; an out-of-jail operand escalates. They
// are kept SEPARATE from commandAllowlist so a path-less invocation never auto-passes.
var inJailMutators = map[string]bool{
	"mkdir": true, "touch": true, "cp": true, "mv": true, "rm": true, "rmdir": true,
	"tee": true, "ln": true, "chmod": true,
}

// AutoPermissionPolicy holds the inputs the classifier needs: the sandbox jail (for filesystem
// confinement) and the per-tool workdir resolver. A nil policy on the executor disables the whole
// stage (byte-identical to today). It is supplied by the engine when action.auto_permission is on.
type AutoPermissionPolicy struct {
	// Sandbox is the jail every write/path-operand must resolve inside. A SAFE in-jail write is one
	// whose resolved target the sandbox would ADMIT (Check returns ""); a target the sandbox denies is
	// DANGEROUS (out-of-jail / protected) and escalates. Nil ⇒ no filesystem confinement (every write
	// escalates, the most conservative stance).
	Sandbox *Sandbox

	// ExtraAllowlist is a PER-WORKSPACE extension of commandAllowlist: additional program names a
	// project has granted into the SAFE tier (its own build/test tooling — e.g. `mvn`, `gradle`, `tsc`,
	// `dotnet`). It is MERGED ONTO the curated seed set (it never SHRINKS it), so a project can widen
	// what auto-passes without weakening the baseline. The hardened-destructive + interpreter-inline +
	// dangerous-subcommand + redirection + in-jail checks ALL still fire on an extra-allowlisted program
	// (the extension says "this program is benign to RUN", not "anything it does is safe") — so a granted
	// `gradle` still escalates on `gradle > /etc/x` or a destructive shape. Empty/nil ⇒ only the curated
	// seed set is SAFE (byte-identical to today). Loaded deterministically from a workspace config file
	// (LoadWorkspaceAutoPermission); never the wall clock, never the network.
	ExtraAllowlist map[string]bool

	// PreAuthClasses is the HIGHER-AUTONOMY PRE-AUTHORIZATION channel (the L4-autonomy hook, roadmap
	// §1.5 / L4): the set of specific DANGEROUS COMMAND CLASSES a human has granted AHEAD of time
	// (e.g. "go run", "make", "npm install"). A class in this set moves DANGEROUS→SAFE for THAT class
	// ONLY — the harness then self-authorizes that class instead of always-escalating. It is an
	// EXPLICIT grant: empty/nil (the default) means escalate-everything-dangerous exactly as today, so
	// NO class is ever pre-authorized by default. A grant only ever suppresses the SPECIFIC
	// dangerous-subcommand escalation it names; every OTHER guard (hardened-destructive, jail
	// containment, redirection escape, interpreter inline-code, non-allowlisted program) still fires —
	// a pre-auth can NEVER loosen the jail or unblock a catastrophe (a granted "git push" does not also
	// auto-pass `rm -rf /etc`). The class vocabulary is the keys dangerousSubcommandClass returns; an
	// unknown grant string is inert (it matches no class). Loaded deterministically from config/flag.
	PreAuthClasses map[string]bool
}

// isAllowlisted reports whether prog is on the curated seed allowlist OR the per-workspace extension.
// The extension is purely additive: it widens the SAFE set, never narrows it.
func (pol *AutoPermissionPolicy) isAllowlisted(prog string) bool {
	if commandAllowlist[prog] {
		return true
	}
	return pol.ExtraAllowlist[prog]
}

// isPreAuthorized reports whether the given dangerous-command class has been pre-authorized. A nil
// grant set (the default) authorizes nothing — escalate-everything-dangerous stays the floor.
func (pol *AutoPermissionPolicy) isPreAuthorized(class string) bool {
	if class == "" {
		return false
	}
	return pol.PreAuthClasses[class]
}

// PermissionDecision is the classifier's verdict for one call: the tier + a human-readable reason
// (always set, so an escalation event carries WHY). Pure data.
type PermissionDecision struct {
	Tier   Permission
	Reason string
}

// ClassifyPermission decides the auto-permission tier for a call. It is the heart of the policy.
//
// The decision tree (allowlist-first, deny-by-default):
//
//  1. INSPECT (read / sense, local or self reach) ⇒ SAFE. A read changes nothing. (A distal/network
//     read is NOT auto-safe — it is outward-facing — and is left to the gate-router's quota.)
//  2. A FILE-MUTATING tool (write_file/edit_file) ⇒ SAFE iff the target resolves INSIDE the jail
//     (the sandbox admits it); otherwise DANGEROUS (write outside the workspace).
//  3. A COMMAND tool (run_shell/run_tests) ⇒ classify EACH segment:
//     - the program is on commandAllowlist AND the segment is not a hardened-destructive shape ⇒ SAFE;
//     - the program is an in-jail mutator AND every path operand resolves inside the jail ⇒ SAFE;
//     - otherwise (non-allowlisted program / out-of-jail operand / destructive shape / network reach)
//     ⇒ DANGEROUS.
//  4. Anything else (an external-reach mutate/execute, an unknown taxonomy) ⇒ DANGEROUS.
//
// It never grants SAFE to a world-change with external reach, an out-of-jail write, or a command it
// cannot positively recognize as benign — escalation is the default.
func (pol *AutoPermissionPolicy) ClassifyPermission(call ToolCall, tc TaxClass) PermissionDecision {
	// (1) Reads / local senses are always safe.
	if tc.Op == OpInspect {
		if tc.Reach == ReachExternal {
			return PermissionDecision{PermDangerous, "outward (network) read — outward-facing, escalate"}
		}
		return PermissionDecision{PermSafe, "read-only inspect (local) — auto-approved"}
	}

	// (2) File-mutating tools: safe only if the target stays inside the jail.
	if FileModifyTools[call.Name] {
		path := pathArg(call.Args)
		if path == "" {
			return PermissionDecision{PermDangerous, "file write with no path — cannot confine, escalate"}
		}
		if pol.inJail(path) {
			return PermissionDecision{PermSafe, "in-jail file write (sandbox-reversible) — auto-approved"}
		}
		return PermissionDecision{PermDangerous, "file write outside the workspace jail — escalate"}
	}

	// (3) Command tools: classify each segment; the WHOLE call is safe only if EVERY segment is.
	if CommandTools[call.Name] {
		command := commandArg(call.Args)
		if strings.TrimSpace(command) == "" {
			// An empty command is inert; let the tool's own bad-args handling deal with it (safe).
			return PermissionDecision{PermSafe, "empty command — inert"}
		}
		return pol.classifyCommand(command)
	}

	// (4) External-reach execute / a mutate that is not a file tool / an unrecognized shape: escalate.
	if tc.Op == OpMutate {
		return PermissionDecision{PermDangerous, "mutate (" + tc.String() + ") — world-change, escalate"}
	}
	if tc.Reach == ReachExternal {
		return PermissionDecision{PermDangerous, "external-reach execute — outward-facing, escalate"}
	}
	// A local execute via a non-command tool path we cannot inspect — conservative escalate.
	return PermissionDecision{PermDangerous, "unrecognized effectful call — escalate (deny-by-default)"}
}

// classifyCommand classifies a (possibly compound) shell command. SAFE iff EVERY tokenized segment is
// individually safe; a single dangerous segment makes the whole call dangerous (a compound line can
// hide a catastrophe in any segment — the same reason EvaluateCommand checks per-segment).
func (pol *AutoPermissionPolicy) classifyCommand(command string) PermissionDecision {
	segs := TokenizeShellCommand(command)
	if len(segs) == 0 {
		return PermissionDecision{PermSafe, "empty command — inert"}
	}
	for _, seg := range segs {
		// A hardened-destructive shape is always dangerous (defense in depth alongside the safety gate).
		if reason := hardenedDestructive(seg); reason != "" {
			return PermissionDecision{PermDangerous, "destructive command (" + reason + ") — escalate"}
		}
		// Shell REDIRECTION (> / >>) writes to an arbitrary path with NO write-program — an allowlisted
		// `echo`/`cat` becomes a write-anywhere escape. Escalate unless the redirect target stays in the
		// jail. (The tokenizer keeps a `>` in-segment, so it is invisible to the program/argv check.)
		if reason := pol.redirectionEscape(seg); reason != "" {
			return PermissionDecision{PermDangerous, reason + " — escalate"}
		}
		argv := argvOf(seg)
		if len(argv) == 0 {
			continue
		}
		prog := strings.ToLower(argv[0])
		if idx := strings.LastIndexByte(prog, '/'); idx >= 0 {
			prog = prog[idx+1:]
		}
		// An interpreter invoked with an INLINE-CODE flag (`python -c`, `node -e`, `awk '<prog>'`)
		// runs ARBITRARY code that bypasses the allowlist entirely — escalate even though the program
		// itself is allowlisted (a build/test interpreter is safe; an inline-eval is not).
		if reason := interpreterInlineCode(prog, argv[1:]); reason != "" {
			return PermissionDecision{PermDangerous, reason + " — arbitrary code, escalate"}
		}
		// An allowlisted program with a DANGEROUS subcommand/flag (`find -delete/-exec`, `git push`,
		// `go run`, …) runs arbitrary code or has outward effect — escalate even though the program is
		// allowlisted (the allowlist vets the program, not its every subcommand surface). UNLESS the
		// specific class has been PRE-AUTHORIZED (the L4-autonomy channel): a human-granted class
		// (e.g. "go run") moves DANGEROUS→SAFE for that class only, so the harness self-authorizes it
		// instead of escalating. The grant is explicit (empty set ⇒ escalate exactly as before) and
		// suppresses ONLY this specific escalation — the hardened-destructive / redirection / jail
		// checks above and the allowlist-membership check below still apply.
		if class, reason := dangerousSubcommandClass(prog, argv[1:]); reason != "" {
			if !pol.isPreAuthorized(class) {
				return PermissionDecision{PermDangerous, reason + " — escalate"}
			}
			// pre-authorized class: fall through to the allowlist-membership check (the program must
			// still be allowlisted to run at all — a pre-auth grants a CLASS, not a free pass).
		}
		switch {
		case pol.isAllowlisted(prog):
			// allowlisted read-only/build/test program (curated seed OR per-workspace extension) — safe.
			continue
		case inJailMutators[prog]:
			// a mutator: safe only if every path operand stays in the jail.
			if !pol.mutatorOperandsInJail(prog, argv[1:]) {
				return PermissionDecision{PermDangerous, "command '" + prog + "' touches a path outside the jail — escalate"}
			}
			continue
		default:
			return PermissionDecision{PermDangerous, "command '" + prog + "' is not on the allowlist — escalate"}
		}
	}
	return PermissionDecision{PermSafe, "allowlisted / in-jail command — auto-approved"}
}

// inlineCodeInterpreters maps an interpreter program to the flags that make it execute ARBITRARY
// inline code (bypassing the allowlist + the sandbox's file scoping). `python -c '...'`, `node -e
// '...'`, `sh -c '...'`, `perl -e`, `ruby -e` — these run code we cannot inspect, so they escalate.
var inlineCodeInterpreters = map[string]map[string]bool{
	"python":  {"-c": true, "--command": true},
	"python3": {"-c": true, "--command": true},
	"node":    {"-e": true, "--eval": true, "-p": true, "--print": true},
	"perl":    {"-e": true, "-E": true},
	"ruby":    {"-e": true},
	"sh":      {"-c": true},
	"bash":    {"-c": true},
	"zsh":     {"-c": true},
	"php":     {"-r": true},
}

// interpreterInlineCode returns a non-empty reason iff prog is a known interpreter invoked with an
// inline-code flag (an arbitrary-code-execution surface the allowlist cannot vet). Empty otherwise.
func interpreterInlineCode(prog string, args []string) string {
	flags, ok := inlineCodeInterpreters[prog]
	if !ok {
		return ""
	}
	for _, a := range args {
		al := strings.ToLower(a)
		if eq := strings.IndexByte(al, '='); eq >= 0 { // --eval=... form
			al = al[:eq]
		}
		if flags[al] {
			return "interpreter '" + prog + " " + a + "' executes inline code"
		}
	}
	return ""
}

// dangerousSubcommandRules maps an allowlisted program to a predicate over its args that, when true,
// makes the call DANGEROUS despite the program being allowlisted. These are subcommands/flags that run
// arbitrary code (`find -exec`, `go run`) or have outward/irreversible effect (`git push`). Each rule
// returns a (class, reason) pair: the CLASS is the stable grant key the pre-authorization channel names
// (e.g. "go run", "make", "npm install"); the REASON is the human-readable escalation text. An empty
// reason means "not dangerous" (the plain allowlisted use stays safe). The class key is canonical —
// `<prog> <subcommand>` — so a human grant is unambiguous and a typo'd grant simply matches nothing.
var dangerousSubcommandRules = map[string]func(args []string) (class, reason string){
	"find": func(args []string) (string, string) {
		for _, a := range args {
			switch strings.ToLower(a) {
			case "-delete":
				return "find -delete", "find -delete (destructive)"
			case "-exec", "-execdir", "-ok", "-okdir":
				return "find -exec", "find " + a + " (runs an arbitrary command)"
			}
		}
		return "", ""
	},
	"git": func(args []string) (string, string) {
		for _, a := range args {
			switch strings.ToLower(a) {
			case "push", "pull", "fetch", "clone", "remote", "submodule":
				return "git " + strings.ToLower(a), "git " + a + " (network / outward effect)"
			case "clean":
				return "git clean", "git clean (destructive)"
			case "reset":
				return "git reset", "git reset (can discard work)"
			}
		}
		return "", ""
	},
	"go": func(args []string) (string, string) {
		for _, a := range args {
			switch strings.ToLower(a) {
			case "run", "install", "get", "generate":
				return "go " + strings.ToLower(a), "go " + a + " (runs / fetches arbitrary code)"
			}
		}
		return "", ""
	},
	"make": func(args []string) (string, string) {
		// make runs Makefile recipes — arbitrary project-defined code. Always escalate (the recipe is
		// not inspectable here); the conscious can author a specific build target explicitly, OR a human
		// can pre-authorize the "make" class for an autonomous build session.
		return "make", "make (runs arbitrary Makefile recipes)"
	},
	"npm":  npmRunGuard("npm"),
	"pnpm": npmRunGuard("pnpm"),
	"yarn": npmRunGuard("yarn"),
}

// npmRunGuard flags npm/pnpm/yarn subcommands that run arbitrary lifecycle scripts or fetch code. The
// class key normalizes the install aliases (`i`/`ci` -> `install`) so one grant covers the family, and
// is program-specific (`npm install` vs `yarn install`) so a grant is scoped to the tool it names.
func npmRunGuard(prog string) func(args []string) (string, string) {
	return func(args []string) (string, string) {
		for _, a := range args {
			sub := strings.ToLower(a)
			switch sub {
			case "run", "run-script", "exec", "install", "i", "ci", "add", "publish":
				canon := sub
				if sub == "i" || sub == "ci" {
					canon = "install"
				}
				return prog + " " + canon, prog + " " + a + " (runs arbitrary lifecycle scripts / fetches code)"
			}
		}
		return "", ""
	}
}

// dangerousSubcommandClass returns the (class, reason) iff an allowlisted program is invoked with a
// dangerous subcommand/flag. An empty reason means the plain allowlisted use is safe. The class is the
// stable key the pre-authorization channel grants against.
func dangerousSubcommandClass(prog string, args []string) (class, reason string) {
	if rule, ok := dangerousSubcommandRules[prog]; ok {
		return rule(args)
	}
	return "", ""
}

// redirectionEscape returns a non-empty reason iff a segment contains an output redirection (`>` or
// `>>`) whose target resolves OUTSIDE the jail (a write-anywhere escape with no write-program). A
// redirect to an in-jail path is safe; a redirect to a system/out-of-jail path escalates. Input
// redirection (`<`) reads and is harmless. The scan is quote-aware so a `>` inside quotes is literal.
func (pol *AutoPermissionPolicy) redirectionEscape(seg string) string {
	runes := []rune(seg)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch ch {
		case '\\':
			i++ // skip the escaped char
		case '\'':
			i = scanSingleQuote(runes, i) - 1
		case '"':
			i = scanDoubleQuote(runes, i) - 1
		case '>':
			// Skip a `>>` (append) — same confinement check on the target.
			j := i + 1
			if j < len(runes) && runes[j] == '>' {
				j++
			}
			// A `2>`/`&>` fd-prefixed redirect: the `>` is what we matched; the target follows.
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				j++
			}
			// Read the target token (up to whitespace / next operator).
			start := j
			for j < len(runes) && runes[j] != ' ' && runes[j] != '\t' && runes[j] != '>' && runes[j] != '<' && runes[j] != '|' {
				j++
			}
			target := strings.Trim(strings.TrimSpace(string(runes[start:j])), `"'`)
			// A redirect to /dev/null (or an fd like &1) is harmless.
			if target == "" || target == "/dev/null" || strings.HasPrefix(target, "&") {
				i = j - 1
				continue
			}
			if !pol.inJail(target) {
				return "output redirection to '" + target + "' outside the jail"
			}
			i = j - 1
		}
	}
	return ""
}

// mutatorOperandsInJail reports whether every PATH operand of an in-jail mutator resolves inside the
// sandbox jail. A flag is not a path; a non-path token (e.g. a chmod mode `644`) is ignored. With no
// sandbox the answer is false (cannot confine ⇒ not safe). Conservative: an operand it cannot resolve
// to an in-jail path makes the call dangerous.
func (pol *AutoPermissionPolicy) mutatorOperandsInJail(prog string, args []string) bool {
	if pol.Sandbox == nil {
		return false
	}
	_, operands := normFlags(args)
	// chmod's first operand is the MODE, not a path — drop it from the path set.
	if prog == "chmod" && len(operands) > 0 {
		operands = operands[1:]
	}
	if len(operands) == 0 {
		// A mutator with no path operand (e.g. bare `rm`) — cannot positively confine; escalate.
		return false
	}
	for _, op := range operands {
		if !pol.inJail(op) {
			return false
		}
	}
	return true
}

// inJail reports whether a path operand resolves INSIDE the sandbox jail — i.e. the sandbox would
// ADMIT a write to it (Check returns ""). A relative path is resolved against the jail root so a
// bare `build/` is judged as `<jail>/build`. An absolute path is checked as-is. A jailed sandbox with
// no allowed roots admits everything-but-protected, which is NOT a real jail — so inJail requires the
// sandbox to have at least one allowed root (a real confinement boundary); otherwise it is false.
func (pol *AutoPermissionPolicy) inJail(path string) bool {
	if pol.Sandbox == nil || len(pol.Sandbox.allowedRoots) == 0 {
		return false // no real jail configured ⇒ cannot claim a write is confined
	}
	resolved := path
	if !filepath.IsAbs(path) {
		// Resolve against the first allowed root (the workspace) so a relative operand is judged
		// inside the jail it would actually land in.
		resolved = filepath.Join(pol.Sandbox.allowedRoots[0], path)
	}
	return pol.Sandbox.Check(resolved) == ""
}
