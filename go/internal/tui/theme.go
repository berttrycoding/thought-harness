// Package tui is the Bubble Tea app — the ONLY place in the tree that imports charmbracelet/*.
// The engine and every cognition package stay headless-pure (DESIGN §0, §3): the TUI subscribes the
// event bus and reads engine state through the read-only accessors (DESIGN §4.5); the engine never
// imports the TUI.
//
// theme.go is the single design language (DESIGN §5): ONE Palette (the port of the Python
// tui/theme.py `C` dict), foreground-only, NormalBorder (square) for panels/input, DoubleBorder for
// popups, NO emoji, no Background except the screen base + the chrome bars. This is the authoritative
// palette for both modes (CHAT + COGNITION) and all four popups — it reconciles lathe's Catppuccin
// and options' monochrome onto thought_harness's own theme, which is the tie-breaker.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// Palette is the typed port of the Python tui/theme.py `C` dict — every key, foreground-only, as a
// lipgloss.Color. Distinct keys that share a hex (Text/Harness, Trace/Muted) are kept separate so the
// call sites read by the semantic name the Python panels use. 24-bit hex only (no ANSI color names —
// terminals remap the 16-color palette). DESIGN §5.
type Palette struct {
	// grayscale text ramp (foreground, brightest -> dimmest)
	Text    lipgloss.Color // #e6e9ef  primary content / the harness's voice
	Harness lipgloss.Color // #e6e9ef  the harness's voice == primary content (distinct key)
	User    lipgloss.Color // #ffffff  the user's turn — pure white (bold at the use site)
	Title   lipgloss.Color // #ffffff  titles / headers — bold white, never coloured
	Status  lipgloss.Color // #b8bcc6  header subtitle / secondary
	Subtext lipgloss.Color // #b8bcc6  secondary content, descriptions, section headers
	Trace   lipgloss.Color // #8891a0  de-emphasised notes, labels
	Muted   lipgloss.Color // #8891a0  labels (distinct key, same hex as Trace)
	Faint   lipgloss.Color // #5b6270  dimmest still-readable (ticks, inactive, line nums)

	// the single accent + the action tone
	Accent lipgloss.Color // #81a1c1  steel blue: active line / voiced thought / focus / system marker
	Action lipgloss.Color // #a0a8b4  tool I/O — light cool gray (NOT a bright peach)

	// structural (borders only — never text)
	Surface lipgloss.Color // #3b4452  panel + chat borders
	Dim     lipgloss.Color // #2c3340  faint inner borders / rail outer border
	Base    lipgloss.Color // #16181d  screen background (cool near-black)

	// muted semantic signal (ONLY for genuine state)
	Ok   lipgloss.Color // #a3be8c  pass / ADMIT / awake / reward>=0 / obs OK
	Warn lipgloss.Color // #ebcb8b  FLAG / drowsy / truthy flag / N-A stability
	Err  lipgloss.Color // #e88388  REJECT / conflict / obs failed / reward<0
}

// DefaultPalette is the one palette for the whole surface — the port of tui/theme.py `C`. The TUI
// uses this single instance; nothing rebuilds or swaps it (one design language, DESIGN §5).
var DefaultPalette = Palette{
	Text:    "#e6e9ef",
	Harness: "#e6e9ef",
	User:    "#ffffff",
	Title:   "#ffffff",
	Status:  "#b8bcc6",
	Subtext: "#b8bcc6",
	Trace:   "#8891a0",
	Muted:   "#8891a0",
	Faint:   "#5b6270",
	Accent:  "#81a1c1",
	Action:  "#a0a8b4",
	Surface: "#3b4452",
	Dim:     "#2c3340",
	Base:    "#16181d",
	Ok:      "#a3be8c",
	Warn:    "#ebcb8b",
	Err:     "#e88388",
}

// Pal is the package-level palette every renderer reads (the Python panels read module-level `C`).
var Pal = DefaultPalette

// -- the semantic maps (ported verbatim from panels.py / theme.py, the single source of truth) -----

// SrcColor maps a thought's Source to its tone in the conscious stream (Python panels._SRC_COLOR).
// The harness's own voiced injection is the accent; reality (an observation) is the one semantic
// green; everything else is a grayscale tier.
var SrcColor = map[types.Source]lipgloss.Color{
	types.INJECTED:    Pal.Accent,  // a voiced injection — the harness's "own" next thought
	types.OBSERVATION: Pal.Ok,      // reality / ground truth
	types.GENERATED:   Pal.Text,    // effortful generation
	types.USER_INPUT:  Pal.Text,    //
	types.PERCEPT:     Pal.Subtext, //
	types.METACOG:     Pal.Faint,   // structural bookkeeping — dimmest
}

// VerdictColor maps a Filter verdict to its tone (Python panels render_seam: ADMIT/FLAG/REJECT).
var VerdictColor = map[types.Verdict]lipgloss.Color{
	types.ADMIT:  Pal.Ok,
	types.FLAG:   Pal.Warn,
	types.REJECT: Pal.Err,
}

// DecColor maps a Controller decision to its tone (Python panels render_critic). ACT did something
// real (green); STOP is neutral-done; THINK is plain text; every other (structural) move is the
// accent — read DecColorFor for the "else -> accent" fallthrough Python's dict.get default encodes.
var DecColor = map[types.Decision]lipgloss.Color{
	types.ACT:     Pal.Ok,
	types.DELIVER: Pal.Ok, // speech crossed the seam — as real as an ACT
	types.STOP:    Pal.Subtext,
	types.THINK:   Pal.Text,
}

// ArousalColor maps the arousal level to its tone (Python panels render_lifecycle).
var ArousalColor = map[types.Arousal]lipgloss.Color{
	types.AWAKE:  Pal.Ok,
	types.DROWSY: Pal.Warn,
	types.ASLEEP: Pal.Faint,
}

// SrcColorFor returns the tone for a Source, defaulting to Text (Python `_SRC_COLOR.get(src, TXT)`).
func SrcColorFor(s types.Source) lipgloss.Color {
	if c, ok := SrcColor[s]; ok {
		return c
	}
	return Pal.Text
}

// VerdictColorFor returns the tone for a verdict NAME, defaulting to Text (the panels read the verdict
// string off the event data, so this keys on the wire name — Python `{...}.get(v, TXT)`).
func VerdictColorFor(name string) lipgloss.Color {
	switch name {
	case "ADMIT":
		return Pal.Ok
	case "FLAG":
		return Pal.Warn
	case "REJECT":
		return Pal.Err
	default:
		return Pal.Text
	}
}

// DecColorFor returns the tone for a decision NAME with the "else -> accent" default the panels use
// (Python `{"ACT":OK,"STOP":SUB,"THINK":TXT}.get(dec, ACC)`). The panels read the decision off the
// last_meta, which is a string, so this keys on the wire name.
func DecColorFor(name string) lipgloss.Color {
	switch name {
	case "ACT", "DELIVER":
		return Pal.Ok
	case "STOP":
		return Pal.Subtext
	case "THINK":
		return Pal.Text
	default:
		return Pal.Accent
	}
}

// ArousalColorFor returns the tone for an arousal NAME, defaulting to Text (Python
// `{"AWAKE":OK,"DROWSY":WARN,"ASLEEP":FNT}.get(name, TXT)`).
func ArousalColorFor(name string) lipgloss.Color {
	switch name {
	case "AWAKE":
		return Pal.Ok
	case "DROWSY":
		return Pal.Warn
	case "ASLEEP":
		return Pal.Faint
	default:
		return Pal.Text
	}
}

// -- conversation role voicing (CHAT mode + the cognition-tab chat column) -------------------------

// Role is one conversation role's voicing — the prefix + the palette tone (Python theme.ROLE +
// ChatLog.say). The chat is the watched seam made open; the only colour in it is the steel-blue
// `‹ harness` marker (the system speaking) and the muted-gray action line. Bold/Italic are applied
// at the use site, recorded here so a renderer reproduces theme.py's intent exactly.
type Role struct {
	Prefix string         // the leading marker, including any leading newline/spaces (verbatim from theme.ROLE)
	Color  lipgloss.Color // the palette tone for the marker + body
	Bold   bool           // the user turn is bold white
	Italic bool           // action + sys lines are italic
}

// Roles is the port of theme.ROLE — the four conversation roles, keyed by name. The prefixes are
// byte-for-byte from theme.py (including the leading "\n" on the user turn and the leading spaces on
// action/sys), so the chat reads identically.
var Roles = map[string]Role{
	"user":    {Prefix: "\n› you   ", Color: Pal.User, Bold: true}, // whole turn bold white
	"harness": {Prefix: "‹ harness  ", Color: Pal.Accent},          // accent marker + near-white body
	"action":  {Prefix: "  ↳ ", Color: Pal.Action, Italic: true},   // a reality check, italic action-gray
	"sys":     {Prefix: "  · ", Color: Pal.Trace, Italic: true},    // cognition surfacing, italic faint
}

// RoleFor returns the voicing for a role name, defaulting to the sys voicing for an unknown role (so
// a stray bus role never crashes the chat). Mirrors theme.ROLE's dict access with a safe default.
func RoleFor(name string) Role {
	if r, ok := Roles[name]; ok {
		return r
	}
	return Roles["sys"]
}

// -- shared glyphs (DESIGN §5) ---------------------------------------------------------------------

// Spark is the sparkline ramp `_SPARK` (Python panels). Eight levels, low-to-high.
const Spark = "▁▂▃▄▅▆▇█"

// Stability marks (Python render_durability): held / not-held / N-A.
const (
	MarkHeld    = "✓"
	MarkNotHeld = "·"
	MarkNA      = "~"
)

// Fill-bar glyphs (Python panels._bar, width 10): on / off.
const (
	BarOn  = "█"
	BarOff = "·"
)

// -- border styles (DESIGN §5) ---------------------------------------------------------------------

// PanelBorder is the square NormalBorder used for panels + the input box (lathe + options-panels
// discipline). Foreground-only — no Background.
var PanelBorder = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(Pal.Surface)

// PopupBorder is the DoubleBorder overlay signature used by all four popups (matching options/lathe).
// Padding(1, 2) is the modal padding; the accent border marks an overlay. Foreground-only.
var PopupBorder = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	BorderForeground(Pal.Accent).
	Padding(1, 2)

// TitleStyle / SectionStyle / FaintStyle are the few shared text styles the base reuses. The panels
// build their own runs; these are the recurring ones (bold title, bold section header, dim ticks).
var (
	TitleStyle   = lipgloss.NewStyle().Foreground(Pal.Title)
	SectionStyle = lipgloss.NewStyle().Foreground(Pal.Subtext)
	FaintStyle   = lipgloss.NewStyle().Foreground(Pal.Faint)
)
