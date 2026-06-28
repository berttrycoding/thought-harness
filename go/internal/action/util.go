// util.go — small stdlib helpers shared by builtins.go (arg coercion, utf-8 decoding, line
// scanning, command quoting). None of these are part of the Python wire contract; they exist to
// reproduce Python str/bytes semantics faithfully in Go.
package action

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// argStr coerces a tool arg to a string the way Python's str(args.get(k, "")) does: a missing key
// or nil -> ""; a string -> itself; anything else -> its str() form.
func argStr(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return anyToStr(v)
}

// anyToStr reproduces Python str(x) for the value kinds a tool arg can carry (the write_file
// content fallback: `if not isinstance(content, str): content = str(content)`). It is NOT a general
// repr — it covers the JSON-decoded scalar types args realistically hold.
func anyToStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "" // matches the "" default used at every call site
	case string:
		return x
	case bool:
		if x {
			return "True" // Python str(True)
		}
		return "False"
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		// Python str(float) — strconv with -1 precision gives the shortest round-trippable form,
		// the closest stdlib analogue.
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// decodeUTF8Replace decodes bytes as UTF-8 with the U+FFFD replacement character on invalid
// sequences (Python's raw.decode("utf-8", "replace")). Valid UTF-8 (the common case) passes through
// untouched; only malformed input incurs the rune rewrite.
func decodeUTF8Replace(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			sb.WriteRune('�')
			b = b[1:]
			continue
		}
		sb.WriteRune(r)
		b = b[size:]
	}
	return sb.String()
}

// newLineScanner returns a bufio.Scanner that yields lines (newline-stripped) with a buffer large
// enough that a long source line is not silently dropped (the default 64 KiB cap would truncate).
// Mirrors Python's `for i, line in enumerate(fh, 1)` line iteration with errors="ignore" (invalid
// bytes are decoded with replacement at read time when the line is examined).
func newLineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return sc
}

// formatTimeout renders a duration the way Python's f-string prints the float seconds it was given
// (e.g. 30.0 -> "30.0s"), so the timeout message reads identically to the Python source.
func formatTimeout(d time.Duration) string {
	secs := d.Seconds()
	if secs == float64(int64(secs)) {
		return fmt.Sprintf("%.1fs", secs) // Python default timeout 30.0 prints "30.0"
	}
	return strconv.FormatFloat(secs, 'g', -1, 64) + "s"
}

// quoteCmd wraps a path containing whitespace for safe interpolation into a /bin/sh -lc command
// line (Python's subprocess.list2cmdline analogue for the single-arg interpreter path).
func quoteCmd(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}
