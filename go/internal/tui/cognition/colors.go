// colors.go — the source/verdict/decision/arousal tone maps, ported verbatim from panels.py /
// theme.py (DESIGN §4.3, §7). The parent tui package's theme.go holds the same maps keyed on the
// types enums; the cognition package cannot import tui (tui imports cognition — a cycle), and the
// ViewModel carries provenance/verdict/decision/arousal as WIRE NAMES (strings) off the snapshot +
// event stream, so these map the name -> lipgloss tone with the exact panels.py defaults. The hexes
// are byte-for-byte the same single design language (the colXXX constants in dashboard.go).
package cognition

import "github.com/charmbracelet/lipgloss"

// SrcColorFor maps a thought's Source NAME to its tone in the conscious stream (panels.py _SRC_COLOR,
// keyed by the wire name; default = colText, the Python `_SRC_COLOR.get(src, TXT)`). The harness's own
// voiced injection is the accent; reality (an observation) is the one semantic green; everything else
// is a grayscale tier.
func SrcColorFor(name string) lipgloss.Color {
	switch name {
	case "INJECTED":
		return colAccent // a voiced injection — the harness's "own" next thought
	case "OBSERVATION":
		return colOk // reality / ground truth
	case "GENERATED", "USER_INPUT":
		return colText // effortful generation / user input
	case "PERCEPT":
		return colSubtext
	case "METACOG":
		return colFaint // structural bookkeeping — dimmest
	default:
		return colText
	}
}

// VerdictColorFor maps a Filter verdict NAME to its tone (panels.py render_seam: ADMIT/FLAG/REJECT),
// default = colText (the Python `{...}.get(v, TXT)`).
func VerdictColorFor(name string) lipgloss.Color {
	switch name {
	case "ADMIT":
		return colOk
	case "FLAG":
		return colWarn
	case "REJECT":
		return colErr
	default:
		return colText
	}
}

// DecColorFor maps a Controller decision NAME to its tone with the "else -> accent" default panels.py
// uses (Python `{"ACT":OK,"STOP":SUB,"THINK":TXT}.get(dec, ACC)`): ACT did something real (green),
// STOP is neutral-done, THINK is plain text, every other (structural) move is the accent.
func DecColorFor(name string) lipgloss.Color {
	switch name {
	case "ACT":
		return colOk
	case "STOP":
		return colSubtext
	case "THINK":
		return colText
	default:
		return colAccent
	}
}

// ArousalColorFor maps the arousal level NAME to its tone (panels.py render_lifecycle), default =
// colText (the Python `{"AWAKE":OK,"DROWSY":WARN,"ASLEEP":FNT}.get(name, TXT)`).
func ArousalColorFor(name string) lipgloss.Color {
	switch name {
	case "AWAKE":
		return colOk
	case "DROWSY":
		return colWarn
	case "ASLEEP":
		return colFaint
	default:
		return colText
	}
}
