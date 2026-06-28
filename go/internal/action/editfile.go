// editfile.go — the str_replace-editor SURGICAL edit tool (capability-enhancement T1.2).
//
// Where write_file OVERWRITES a whole file (the token cost on a big file + the model's elision habit:
// it emits "// ... rest unchanged" and silently drops the body), edit_file does a targeted STRING
// REPLACEMENT — the str_replace-editor / aider shape, NOT a unified diff (a diff line-drifts and
// hard-fails on the slightest context mismatch). The model supplies the exact text to replace plus its
// replacement; the tool locates it and swaps it in place, leaving the rest of the file byte-identical.
//
// Category mutate/local (identical to write_file): an edit alters the local world — the gate-router
// routes it like write_file, the sandbox checks the resolved path, and autopermission treats it as a
// local-world mutation (FileModifyTools already lists it). It is NOT a read.
//
// edit_file EDITS an EXISTING file; CREATING a file is write_file's job — a missing file or an empty
// old_string is a bad-args error that points the caller at write_file. The ambiguity contract is the
// str_replace-editor contract: an old_string must be UNIQUE (or replace_all set) so an edit is never
// applied to the wrong occurrence — a non-unique old_string errors with the match count and asks for
// more surrounding context. A not-found old_string gets ONE conservative fuzzy retry (the common case
// where the model's leading/trailing indentation is slightly off); the fuzzy match must still resolve
// to exactly ONE line-aligned region or it errors honestly rather than guess.
//
// Determinism: pure string ops + file I/O — no clock, no randomness — so the golden oracle + the
// stability ticks stay reproducible (the tool itself is not on a golden, but the discipline holds).
package action

import (
	"fmt"
	"os"
	"strings"
)

// EditFile performs a surgical string replacement in an existing workspace file.
type EditFile struct{ baseTool }

// NewEditFile constructs an edit_file tool scoped to workdir (the sibling of NewWriteFile).
func NewEditFile(workdir string) *EditFile { return &EditFile{newBase(workdir)} }

func (t *EditFile) Name() string { return "edit_file" }

// Category: an edit alters the local world (mutate/local) — the SAME tag write_file carries, so the
// gate-router routes it, the sandbox checks it, and a mutate-scoped sub-agent admits it identically.
func (t *EditFile) Category() TaxClass { return TaxClass{Op: OpMutate, Reach: ReachLocalWorld} }

func (t *EditFile) Description() string {
	return "Edit an existing file by replacing an exact string with another (a surgical str-replace, " +
		"not a whole-file overwrite). old_string must match exactly and be unique unless replace_all is " +
		"set; to CREATE a file use write_file. Prefer this over write_file for changes to existing files."
}

func (t *EditFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "file path (relative to the workspace)"},
			"old_string":  map[string]any{"type": "string", "description": "the exact text to replace (must be unique unless replace_all)"},
			"new_string":  map[string]any{"type": "string", "description": "the text to put in its place"},
			"replace_all": map[string]any{"type": "boolean", "description": "replace every occurrence (default false)"},
		},
		"required": []any{"path", "old_string", "new_string"},
	}
}

// Execute resolves the path under the workdir jail (identical to write_file — never escapes it), reads
// the file, locates old_string, replaces it, and writes the result back. The replacement rules are the
// str_replace-editor contract:
//
//	missing file       -> ErrUnavailable (edit_file edits; create with write_file)
//	empty old_string   -> ErrBadArgs (no anchor; create with write_file)
//	0 exact matches    -> one conservative fuzzy retry (leading/trailing per-line whitespace ignored);
//	                      a UNIQUE fuzzy region replaces, otherwise ErrBadArgs "old_string not found"
//	1 exact match      -> replace it
//	>1 exact matches    -> ErrBadArgs unless replace_all, then replace all
//
// new_string is taken verbatim (anyToStr coerces a non-string payload the way write_file's content does).
// On any failure the file is left UNTOUCHED — the read/replace/write are sequenced so an error returns
// before the write.
func (t *EditFile) Execute(args map[string]any) ToolResult {
	path := strings.TrimSpace(argStr(args, "path"))
	if path == "" {
		return ToolResult{Name: t.Name(), Content: "missing 'path'", IsError: true, ErrorCode: ErrBadArgs}
	}
	// old_string is read RAW (no TrimSpace): leading/trailing whitespace is load-bearing for an exact
	// match. new_string is coerced the way write_file coerces content (a non-string payload -> str()).
	oldStr := argStr(args, "old_string")
	if oldStr == "" {
		return ToolResult{Name: t.Name(), Content: "empty 'old_string' — edit_file replaces existing text; to create a file use write_file", IsError: true, ErrorCode: ErrBadArgs}
	}
	newStr := anyToStr(args["new_string"])

	full := resolve(t.workdir, path)
	raw, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot edit %s: no such file — edit_file edits an existing file; to create one use write_file", path), IsError: true, ErrorCode: ErrUnavailable}
		}
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot read %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
	}
	content := decodeUTF8Replace(raw)

	n := strings.Count(content, oldStr)
	var result string
	var nReplaced int
	switch {
	case n == 0:
		// CONSERVATIVE FUZZY FALLBACK (aider-style): the model's leading/trailing whitespace is the most
		// common reason an otherwise-correct old_string misses. Retry the match ignoring per-line
		// leading/trailing whitespace differences; replace ONLY when it resolves to exactly ONE line-aligned
		// region (otherwise error — never guess between candidates).
		region, ok := fuzzyMatchRegion(content, oldStr)
		if !ok {
			return ToolResult{Name: t.Name(), Content: fmt.Sprintf("old_string not found in %s — it must match the file exactly (whitespace included); read the file and copy the text verbatim", path), IsError: true, ErrorCode: ErrBadArgs}
		}
		result = content[:region.start] + newStr + content[region.end:]
		nReplaced = 1
	case n == 1:
		result = strings.Replace(content, oldStr, newStr, 1)
		nReplaced = 1
	default: // n > 1
		if !boolArg(args, "replace_all") {
			return ToolResult{Name: t.Name(), Content: fmt.Sprintf("old_string not unique in %s (%d matches) — add surrounding context to make it unique, or set replace_all", path, n), IsError: true, ErrorCode: ErrBadArgs}
		}
		result = strings.ReplaceAll(content, oldStr, newStr)
		nReplaced = n
	}

	if err := os.WriteFile(full, []byte(result), 0o644); err != nil {
		return ToolResult{Name: t.Name(), Content: fmt.Sprintf("cannot write %s: %v", path, err), IsError: true, ErrorCode: ErrUnavailable}
	}
	plural := "replacement"
	if nReplaced != 1 {
		plural = "replacements"
	}
	return ToolResult{Name: t.Name(), Content: fmt.Sprintf("edited %s: %d %s", path, nReplaced, plural)}
}

// boolArg reads a boolean tool arg tolerantly: a real bool is used as-is; a JSON-decoded "true"/"false"
// string or a numeric 1/0 also count (a model occasionally stringifies the flag). Anything else -> false.
func boolArg(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}

// region is a [start,end) byte span in the source content.
type region struct{ start, end int }

// fuzzyMatchRegion locates a UNIQUE line-aligned region of `content` that matches `pattern` ignoring
// per-line leading/trailing whitespace differences (the aider-style conservative fallback). It is a
// deliberately narrow fix for the single most common near-miss — the model gets the text right but the
// indentation slightly wrong — NOT a general fuzzy matcher:
//
//   - both sides are split into lines; each line is compared on its TrimSpace form (so a 4-space vs
//     2-space indent, or a trailing-space difference, still matches);
//   - the pattern is anchored at LINE boundaries in content (a window of len(patternLines) consecutive
//     lines), so a partial-line match never fires;
//   - it returns ok=true ONLY when EXACTLY ONE window matches (a second match is ambiguous -> the caller
//     errors rather than guess). The returned span covers the matched lines verbatim (including their
//     original whitespace + the joining newlines), so the caller's slice replacement preserves the rest
//     of the file byte-identically.
//
// A single-line pattern with no trailing newline matches a single content line; the returned span is
// that line's exact byte extent (no surrounding newline consumed), so the replacement does not eat a
// line break.
func fuzzyMatchRegion(content, pattern string) (region, bool) {
	patLines := strings.Split(pattern, "\n")
	// Trim a trailing empty element from a pattern that ends in "\n" so "foo\n" matches the single line
	// "foo" rather than requiring an extra blank line after it.
	if len(patLines) > 1 && patLines[len(patLines)-1] == "" {
		patLines = patLines[:len(patLines)-1]
	}
	if len(patLines) == 0 {
		return region{}, false
	}
	patTrim := make([]string, len(patLines))
	for i, l := range patLines {
		patTrim[i] = strings.TrimSpace(l)
	}

	// Build content lines WITH their byte offsets so a matched window maps back to an exact byte span.
	type lineSpan struct {
		text       string
		start, end int // [start,end) byte span of the line text itself (newline excluded)
	}
	var lines []lineSpan
	pos := 0
	for {
		nl := strings.IndexByte(content[pos:], '\n')
		lineEnd := len(content) // position of the '\n' (or EOF for the last, newline-less line)
		next := -1
		if nl >= 0 {
			lineEnd = pos + nl
			next = lineEnd + 1
		}
		text := content[pos:lineEnd]
		end := lineEnd
		// Exclude a trailing '\r' (CRLF) from BOTH the line text and its span end, so a fuzzy splice
		// leaves the "\r\n" terminator with the surrounding (un-replaced) text instead of consuming the
		// '\r' and silently producing mixed line endings. (LF files have no '\r' ⇒ behaviour unchanged.)
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text = text[:len(text)-1]
			end--
		}
		lines = append(lines, lineSpan{text: text, start: pos, end: end})
		if next < 0 {
			break
		}
		pos = next
	}

	var found region
	matches := 0
	for i := 0; i+len(patLines) <= len(lines); i++ {
		ok := true
		for j := range patLines {
			if strings.TrimSpace(lines[i+j].text) != patTrim[j] {
				ok = false
				break
			}
		}
		if ok {
			matches++
			found = region{start: lines[i].start, end: lines[i+len(patLines)-1].end}
		}
	}
	if matches == 1 {
		return found, true
	}
	return region{}, false
}
