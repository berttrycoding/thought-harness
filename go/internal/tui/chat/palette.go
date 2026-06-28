package chat

// palette.go — the chat package's leaf-local view of the single design palette (DESIGN §5) and the
// conversation-role voicing table (the port of Python theme.py `ROLE`).
//
// WHY THIS IS HERE (and not an import of the root internal/tui theme.go): the root tui package owns
// app.go, which imports this chat package. If chat imported tui back for the palette, that would be an
// `import cycle not allowed` (tui → chat → tui). So the chat package — a pure leaf renderer the app
// calls — holds its own copy of exactly the palette keys and role prefixes it voices, sourced from the
// SAME hex values as theme.py's `C` dict and theme.ROLE. This is the same localisation discipline the
// COGNITION view already uses (internal/tui/cognition/colors.go keeps its own color helpers rather
// than importing the root package). The hex strings are the single source of truth: theme.py / the
// root theme.go DefaultPalette / this file all carry identical values, so "ONE palette" still holds.
//
// foreground-only, 24-bit hex only (no ANSI color names — terminals remap the 16-color palette).

import "github.com/charmbracelet/lipgloss"

// chatPalette is the subset of the theme.py `C` ramp the conversation needs: the text tones, the
// accent + action voices, the dim/faint trace tones, and the bold-white title. Every value is verbatim
// from theme.py (and the root theme.go DefaultPalette).
type chatPalette struct {
	Text    lipgloss.Color // #e6e9ef  primary content
	Harness lipgloss.Color // #e6e9ef  the harness's voiced body (distinct key, same hex as Text)
	User    lipgloss.Color // #ffffff  the user's turn — pure white (bold at the use site)
	Title   lipgloss.Color // #ffffff  banners — bold white
	Status  lipgloss.Color // #b8bcc6  branch / secondary
	Subtext lipgloss.Color // #b8bcc6  stat values, path
	Trace   lipgloss.Color // #8891a0  the faint "  · …" cognition-surfacing line
	Muted   lipgloss.Color // #8891a0  labels (distinct key, same hex as Trace)
	Faint   lipgloss.Color // #5b6270  dimmest readable — timestamps, the divider rule, separators
	Accent  lipgloss.Color // #81a1c1  steel blue: the "‹ harness" marker, header keys, focus
	Action  lipgloss.Color // #a0a8b4  the muted-gray action line (a reality check)
	Ok      lipgloss.Color // #a3be8c  reactive (episodic) mode value
	Warn    lipgloss.Color // #ebcb8b  continuous (awake) mode value
}

// pal is the package-level palette every chat renderer reads (mirrors the Python panels' module-level
// `C`). Identical hex to theme.py / the root theme.go DefaultPalette.
var pal = chatPalette{
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
	Ok:      "#a3be8c",
	Warn:    "#ebcb8b",
}

// role is one conversation role's voicing — the verbatim prefix + the palette tone + the bold/italic
// intent (the port of theme.py `ROLE` + ChatLog.say). Bold/Italic are applied at the use site; recorded
// here so the renderer reproduces theme.py's intent exactly.
type role struct {
	Prefix string         // the leading marker, verbatim from theme.ROLE (incl. any leading "\n"/spaces)
	Color  lipgloss.Color // the palette tone for the marker (+ body, unless overridden per-role)
	Bold   bool           // the user turn is bold white throughout
	Italic bool           // action + sys bodies are italic
}

// roles is the port of theme.ROLE — the four conversation roles, keyed by name. Prefixes are
// byte-for-byte from theme.py (including the leading "\n" on the user turn and the leading spaces on
// action/sys) so the chat reads identically to the Python ChatLog.
var roles = map[string]role{
	"user":    {Prefix: "\n› you   ", Color: pal.User, Bold: true}, // whole turn bold white
	"harness": {Prefix: "‹ harness  ", Color: pal.Accent},          // accent marker + near-white body
	// outreach = the mind speaking UNPROMPTED: same voice as harness, but the header carries the
	// "· unprompted" tag as CHROME (the tag is metadata, never baked into the spoken words).
	"outreach": {Prefix: "‹ harness · unprompted  ", Color: pal.Accent},
	"action":   {Prefix: "  ↳ ", Color: pal.Action, Italic: true}, // a reality check, italic action-gray
	"sys":      {Prefix: "  · ", Color: pal.Trace, Italic: true},  // cognition surfacing, italic faint
}

// roleFor returns the voicing for a role name, defaulting to the sys voicing for an unknown role (so a
// stray bus role never crashes the chat). Mirrors theme.ROLE's dict access with a safe default.
func roleFor(name string) role {
	if r, ok := roles[name]; ok {
		return r
	}
	return roles["sys"]
}
