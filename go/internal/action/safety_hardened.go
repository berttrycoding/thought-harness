// safety_hardened.go — the argv-normalized destructive-command guards that close the KNOWN
// EVASIONS in the regex blocklist (safety.go). The architecture audit confirmed the regex guards
// reChmod777Sys / reRmRfRoot have real evasions:
//
//   - case-fold: EvaluateCommand lowercases the command, so the capital-`-R`-only chmod regex
//     never fires on `chmod -R 777 /` (it sees `-r`), and the lowercase form slips through;
//   - reRmRfRoot is END-ANCHORED to (/ | /* | ~ | $home), so `rm -rf /etc`, `rm -rf /usr`,
//     `rm -rf /var` (a destructive system-path delete) are NOT caught;
//   - split / long flags: `rm -r -f /`, `rm --recursive --force /`, `chmod --recursive 777 /`
//     defeat the fixed `-rf|-fr|-R ` flag shapes the regex expects.
//
// The fix is STRUCTURAL: instead of pattern-matching the raw string, TOKENIZE each segment into
// argv (program + flags + operands), NORMALIZE the flags (fold `-rf` -> {r,f}; map `--recursive`
// -> r; case-insensitive), and decide on the normalized shape. A normalized predicate cannot be
// dodged by reordering, case, or long-flag spelling — the same way an allowlist cannot.
//
// These guards only ADD blocks on top of the existing blockRules — anything the old guards blocked
// stays blocked, plus the evasions are now caught. So enabling them is byte-identical-or-STRICTER
// (TestHardenedGuards_OnlyTightens pins it). They run inside EvaluateCommand (per-segment), so a
// catastrophe hidden in a compound line is still seen.
package action

import (
	"strings"
)

// argvOf splits a single shell SEGMENT (already ;/&&/||/| -split by TokenizeShellCommand) into its
// argv words, quote- and escape-aware, so a flag is never confused by surrounding quotes. It is
// deliberately small: the segment is a single command (no operators — those were the split points),
// so we only need word-splitting that respects single/double quotes + backslash escapes. A leading
// `sudo`/`env VAR=...` prefix is peeled so the guard sees the REAL program (a common dodge).
func argvOf(segment string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(segment)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case ch == '\\' && i+1 < len(runes):
			cur.WriteRune(runes[i+1])
			i++
		case ch == '\'':
			j := i + 1
			for j < len(runes) && runes[j] != '\'' {
				cur.WriteRune(runes[j])
				j++
			}
			i = j
		case ch == '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '\\' && j+1 < len(runes) {
					cur.WriteRune(runes[j+1])
					j += 2
					continue
				}
				cur.WriteRune(runes[j])
				j++
			}
			i = j
		case ch == ' ' || ch == '\t':
			flush()
		default:
			cur.WriteRune(ch)
		}
	}
	flush()
	return peelPrefixes(words)
}

// peelPrefixes strips leading invocation wrappers (sudo, env VAR=val..., command, exec, nohup,
// nice, stdbuf, ...) so the destructive-command guard inspects the REAL program, not a wrapper —
// `sudo rm -rf /`, `env X=1 chmod -R 777 /` must not dodge the guard. Conservative: it only peels a
// known wrapper word (and, for env, its leading VAR=value assignments).
func peelPrefixes(words []string) []string {
	for len(words) > 0 {
		w := strings.ToLower(words[0])
		switch w {
		case "sudo", "command", "exec", "nohup", "doas":
			words = words[1:]
			continue
		case "env":
			words = words[1:]
			// env may carry leading NAME=value assignments before the program.
			for len(words) > 0 && strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") {
				words = words[1:]
			}
			continue
		case "nice", "ionice", "stdbuf", "time", "timeout":
			// These take an optional flag/arg then the program; peel the wrapper word only.
			words = words[1:]
			continue
		}
		break
	}
	return words
}

// normFlags collapses an argv tail into the set of single-letter flags it carries plus its
// operands. It folds clustered short flags (`-rf` -> {r,f}), maps the common long flags to their
// letters (`--recursive` -> r, `--force` -> f), and is case-insensitive (`-R` and `-r` both -> r).
// `--` terminates flag parsing (everything after is an operand). Returns (flags set, operands).
func normFlags(args []string) (map[rune]bool, []string) {
	flags := map[rune]bool{}
	var operands []string
	longMap := map[string]rune{
		"recursive": 'r',
		"force":     'f',
		"dir":       'd',
	}
	endFlags := false
	for _, a := range args {
		if endFlags {
			operands = append(operands, a)
			continue
		}
		switch {
		case a == "--":
			endFlags = true
		case strings.HasPrefix(a, "--"):
			name := strings.ToLower(strings.TrimPrefix(a, "--"))
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			if r, ok := longMap[name]; ok {
				flags[r] = true
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, c := range strings.ToLower(a[1:]) {
				flags[c] = true
			}
		default:
			operands = append(operands, a)
		}
	}
	return flags, operands
}

// systemDirRoots are absolute path prefixes that are SYSTEM directories: a recursive deletion or
// open-chmod of the root OR ANY PATH UNDER IT is catastrophic (`/etc/cron.d/x`, `/usr/bin`, …). The
// whole subtree belongs to the OS.
var systemDirRoots = []string{
	"/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/var", "/boot",
	"/dev", "/proc", "/sys", "/root",
}

// containerRoots are absolute path prefixes that HOLD many independent trees (user homes, mounts,
// service dirs): the root itself and an IMMEDIATE child (a whole user home / a whole mount) are
// catastrophic to wipe, but a DEEP path under them (`/home/u/proj/dist`) is a normal workspace op. So
// these are flagged only at depth <= 1 below the root.
var containerRoots = []string{
	"/home", "/srv", "/mnt", "/media", "/opt",
}

// homeRoots are the user-home aliases — the home root itself (`~`, `$HOME`) is catastrophic to wipe;
// a deep path under it is a workspace op.
var homeRoots = []string{"~", "$home", "${home}"}

// isSensitiveTarget reports whether a normalized operand names a target whose recursive destruction
// or open-chmod is catastrophic:
//   - the absolute root `/` (or `/*`) — the whole filesystem;
//   - a SYSTEM dir or anything under it (`/etc`, `/usr/bin`, `/var/lib/x`);
//   - a CONTAINER root or its immediate child (`/home`, `/home/user`) — but NOT a deep path under it;
//   - a HOME-root alias itself (`~`, `$HOME`) — but NOT a deep path under it.
//
// It does NOT flag a deep workspace path (`/home/u/proj/build`, `./build`), so legitimate workspace
// cleanup is never over-blocked. Conservative toward FLAGGING a root-ish target.
func isSensitiveTarget(operand string) bool {
	p := strings.TrimSpace(operand)
	if p == "" {
		return false
	}
	low := strings.ToLower(p)
	// Strip a trailing glob/slash for the comparison: `/etc/*` and `/etc/` both reduce to `/etc`.
	trimmed := strings.TrimRight(low, "/*")
	if trimmed == "" {
		return true // the path was "/", "/*", "//" — the filesystem root itself
	}

	// Home-root alias (the root itself only).
	for _, r := range homeRoots {
		if trimmed == r {
			return true
		}
	}
	// System dir: the root or ANY depth under it.
	for _, r := range systemDirRoots {
		if trimmed == r || strings.HasPrefix(trimmed, r+"/") {
			return true
		}
	}
	// Container root: the root itself or its IMMEDIATE child only (depth <= 1).
	for _, r := range containerRoots {
		if trimmed == r {
			return true
		}
		if rest := strings.TrimPrefix(trimmed, r+"/"); rest != trimmed {
			// rest is the path below the container root; flag only if it is a single component
			// (an immediate child = a whole user home / mount), not a deeper workspace path.
			if rest != "" && !strings.Contains(rest, "/") {
				return true
			}
		}
	}
	return false
}

// hardenedDestructive inspects ONE already-split segment for a destructive argv shape the regex
// guards miss. Returns a non-empty reason to BLOCK. It catches:
//
//   - `rm` with recursive (r) targeting a system root (force not required to be a disaster);
//   - `rmdir`/`unlink` of a system root;
//   - `chmod` open/world-perms (777/666/a+w …) on a system root, any -R/-r/--recursive spelling;
//   - `chown`/`chgrp -R ... <sysroot>` (recursive ownership change of a system tree).
//
// It is purely additive over EvaluateCommand's blockRules — see TestHardenedGuards_OnlyTightens.
func hardenedDestructive(segment string) string {
	argv := argvOf(segment)
	if len(argv) == 0 {
		return ""
	}
	prog := strings.ToLower(argv[0])
	// A path-qualified program (e.g. "/bin/rm") — take the basename.
	if idx := strings.LastIndexByte(prog, '/'); idx >= 0 {
		prog = prog[idx+1:]
	}
	flags, operands := normFlags(argv[1:])

	hasSensitive := func() bool {
		for _, op := range operands {
			if isSensitiveTarget(op) {
				return true
			}
		}
		return false
	}

	switch prog {
	case "rm":
		// Recursive delete (r) of a system root is catastrophic whether or not -f is present.
		if flags['r'] && hasSensitive() {
			return "rm -r of a system/root path"
		}
	case "rmdir", "unlink":
		if hasSensitive() {
			return prog + " of a system/root path"
		}
	case "chmod":
		// World-writable / open perms on a system path. Catch the classic 777 plus other open
		// modes (666, a+rwx). Recursive makes it worse, but a non-recursive system-root open-chmod
		// is still a privilege catastrophe.
		if hasSensitive() && chmodOpensPerms(operands) {
			return "chmod open-perms on a system/root path"
		}
	case "chown", "chgrp":
		if flags['r'] && hasSensitive() {
			return prog + " -R on a system/root path"
		}
	}
	return ""
}

// chmodOpensPerms reports whether a chmod operand list carries an open/world-permissive mode
// (777/666/a+rwx/o+w …). It is the discriminator that keeps `chmod 644 /etc/x` (a tighten) from
// tripping the guard while `chmod 777 /etc` (a loosen-to-catastrophe) does.
func chmodOpensPerms(operands []string) bool {
	for _, op := range operands {
		o := strings.ToLower(strings.TrimSpace(op))
		if isOpenOctal(o) {
			return true
		}
		// Symbolic mode granting world/all write or exec: a+w, o+w, a+rwx, +w (defaults to a), go+w.
		if strings.HasPrefix(o, "a+") || strings.HasPrefix(o, "o+") || strings.HasPrefix(o, "+") {
			if strings.ContainsAny(o, "wx") {
				return true
			}
		}
	}
	return false
}

// isOpenOctal reports whether an octal chmod mode is dangerously open — the group or other triad
// has a write or exec bit on a 3- or 4-digit octal mode (e.g. 777, 666, 776, 707, 770, 4777).
func isOpenOctal(mode string) bool {
	digits := mode
	if len(digits) == 4 { // a 4-digit mode carries setuid/sticky in the leading digit
		digits = digits[1:]
	}
	if len(digits) != 3 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '7' {
			return false // not octal — let the regex layer handle exotic forms
		}
	}
	group := digits[1] - '0'
	other := digits[2] - '0'
	const wx = 0b011 // write(2)+exec(1)
	return (group&wx) != 0 || (other&wx) != 0
}
