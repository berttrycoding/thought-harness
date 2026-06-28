package cogngraph

import (
	"math"
	"strconv"
	"strings"
)

// This file holds the any-coercion + Python-formatting helpers. The event data map is
// map[string]any (the JSON-native payload), so every Python `d.get(...)`, `str(...)` and
// f-string interpolation over it is reproduced here without ever panicking. These mirror the
// equivalent helpers in internal/appraisal — kept local so cogngraph stays a self-contained
// derived package.

// getStr returns d[key] as a string, or def if absent/not a string (Python d.get(key, def)
// where the value is always a string at the call site). Used where the Python default is a
// string literal ("" / "?") and the value is genuinely a string field.
func getStr(d map[string]any, key, def string) string {
	if s, ok := d[key].(string); ok {
		return s
	}
	return def
}

// getStrDefault is getStr with a non-empty default that must be returned even when the value is
// present-but-not-a-string. It exists only to read the same way as Python's d.get(key, "?") at
// the domain/skill/workflow sites (the value there is always a string in practice).
func getStrDefault(d map[string]any, key, def string) string { return getStr(d, key, def) }

// truthy reproduces Python's bool(x) for the flag keys (soft / proactive): absent (nil) or
// false -> false; a non-empty string / non-zero number / true -> true. Only the bool and nil
// cases are load-bearing (both flags are emitted as bools), but the full coercion keeps the
// `if d.get(...)` semantics exact for any payload shape.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case float32:
		return x != 0
	}
	return v != nil
}

// runeTrunc reproduces Python's str[:n] — a truncation to n CODE POINTS with NO ellipsis (the
// bare slice the labels use, distinct from types.Ellipsize which adds "…"). A string of ≤ n
// runes is returned whole. n<0 yields "" (Python s[:negative] would slice from the end, but no
// call site passes a negative width, so clamping to "" is the safe faithful choice).
func runeTrunc(s string, n int) string {
	if n <= 0 {
		if n < 0 {
			return ""
		}
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pyStr reproduces Python's str(x) / f-string `{x}` interpolation for the values the event data
// map carries (None / bool / string / int / float). This is the single formatter the id-builders
// and labels funnel through, so `f"th:{process}:{i}"`, `f"dec:{tick}"`, `f"branch {bid}"` etc.
// render byte-identically to Python whether the value arrives as a live int (str(5) == "5") or
// a JSON-decoded float (str(5.0) == "5.0").
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return pyFloatRepr(float64(x))
	case float64:
		return pyFloatRepr(x)
	}
	// Any residual type: fall back to a generic string form (never reached for the wire
	// payloads, which are JSON-native primitives).
	return ""
}

// pyStr2 reproduces Python's str(d.get(key, default)) where the default is supplied — i.e.
// when the key is ABSENT it formats the default, otherwise it formats the present value with
// pyStr. This distinguishes the label sites that pass a default (str(d.get('text',”)) -> "")
// from those that don't (str(d.get('kind')) -> "None"). def is the already-stringified Python
// default literal.
func pyStr2(v any, def string) string {
	if v == nil {
		return def
	}
	return pyStr(v)
}

// pyFloatRepr reproduces CPython's repr(float)/str(float): the shortest decimal string that
// round-trips, but ALWAYS with a decimal point or exponent (a whole-valued float renders as
// "5.0", never "5"). Go's strconv 'g' drops the ".0", so reattach it when the shortest form has
// no '.', 'e' or 'E' and is a finite number. Special values match Python: inf -> "inf",
// -inf -> "-inf", nan -> "nan".
func pyFloatRepr(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	// Python uses 'e+NN'/'e-NN' (two-digit exponent) for repr; Go's 'g' uses 'e+NN' too for
	// large/small magnitudes, but the wire ids never reach exponent range (small integer ids),
	// so the mantissa-only fixup above is sufficient for every call site.
	return s
}
