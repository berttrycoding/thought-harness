// diff.go parses a unified `git diff` into the per-file added-line view the gate audits. It is a PURE
// text parser (no exec): the CLI boundary runs `git diff` and hands the output here, keeping this
// package headless-pure and the parse deterministically testable from a string fixture.
package plangate

import "strings"

// ParseUnified parses unified-diff text (the output of `git diff` / `git diff --cached`) into a Diff:
// for each file, the concatenation of its ADDED lines (the `+` lines, minus the `+` prefix). Removed
// (`-`) and context (` `) lines are ignored — the gate asks "did the change PRODUCE this symbol", so
// only added text counts. File headers handled: `diff --git a/<f> b/<f>` opens a file; `+++ b/<path>`
// fixes the path (preferred — it carries the post-rename path); `/dev/null` (a deletion) is skipped.
// The `+++ ` header line itself is NOT counted as an added line (it starts with `+` but is a header).
func ParseUnified(diff string) Diff {
	out := Diff{}
	var cur string         // current file path (from the +++ header)
	var b *strings.Builder // accumulator for cur
	flush := func() {      // commit the current file's added text
		if cur != "" && b != nil {
			out[cur] = b.String()
		}
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			// new file section opens; the path is fixed by the upcoming +++ header.
			flush()
			cur, b = "", nil
		case strings.HasPrefix(line, "--- "):
			// old-file header — ignore (the +++ header carries the live path).
		case strings.HasPrefix(line, "+++ "):
			flush()
			cur = headerPath(strings.TrimPrefix(line, "+++ "))
			if cur == "" {
				b = nil
			} else {
				b = &strings.Builder{}
			}
		case strings.HasPrefix(line, "@@"):
			// hunk header — not content.
		case strings.HasPrefix(line, "+"):
			if b != nil {
				b.WriteString(line[1:]) // drop the leading '+'
				b.WriteByte('\n')
			}
		}
	}
	flush()
	return out
}

// headerPath extracts the file path from a `+++ ` header value, stripping the `b/` prefix git uses and
// returning "" for `/dev/null` (a deletion has no producing file).
func headerPath(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "\t"); i >= 0 { // git appends a tab + timestamp on some diffs
		v = v[:i]
	}
	if v == "/dev/null" {
		return ""
	}
	v = strings.TrimPrefix(v, "b/")
	return v
}
