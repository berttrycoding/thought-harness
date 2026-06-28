package chat

// chat.go — the ChatView: the message-viewport CONTENT renderer for CHAT mode (DESIGN §4.2 item 2).
// This is the Go port of the Python tui/widgets/chat_log.py (ChatLog.say / .system / .banner) — the
// open conversation, the *watched seam made open*. The four conversation roles (user / harness /
// action / sys) are voiced per the role table in palette.go (the leaf-local port of theme.py ROLE /
// theme.go Roles): a leading coloured marker, the role tag, a dim timestamp on a conversational turn,
// and the body indented two spaces. followBottom auto-scrolls to the newest line.
//
// Pure renderer: ChatView holds the message slice and returns a string from View(); the root app.go
// (package tui) owns the bubbles viewport.Model and feeds it this content each frame. The bridge
// (package tui) drains the bus into (role, text) pairs and app.go appends them via Say(). The palette
// + roles live in palette.go (leaf-local) so the chat package never imports the root tui package back
// (which would be an import cycle, since app.go in tui imports chat) — see palette.go.
//
// Faithful to chat_log.py: the role PREFIX, palette tone, and bold/italic come verbatim from `roles`
// (the byte-for-byte port of theme.ROLE), so the conversation reads identically. The only additions
// over the Python (which is a RichLog) are the lathe idioms the task asks for: a dim 15:04 timestamp
// on the user/harness turns and a 2-space body indent — the conversational turns get a header line;
// the action/sys trace lines stay compact one-liners as in the Python.

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// chatEntry is one voiced line in the open conversation. kind selects the voicing: a conversation
// role (user/harness/action/sys), a faint system trace line, or a bold banner.
type chatEntry struct {
	kind      entryKind
	role      string    // theme.Roles key for entryRole entries (user/harness/action/sys)
	text      string    // the spoken content (already re-voiced upstream for harness/injection)
	timestamp time.Time // when it was said — shown dim on conversational turns
}

type entryKind int

const (
	entryRole   entryKind = iota // a conversation turn voiced per theme.Roles
	entrySystem                  // a faint "  · …" trace line (help / status / command output)
	entryBanner                  // a bold titled line (welcome / reset marker)
)

// followBottomDefault — a fresh ChatView tracks the bottom of the conversation (auto-scroll), exactly
// like chat_log.py's RichLog(auto_scroll=True). app.go flips this off when the user scrolls up and
// back on when they return to the bottom (lathe followBottom discipline).
const followBottomDefault = true

// ChatView holds the conversation and renders it. The width is set on resize so the body wrap and the
// timestamp alignment track the viewport. followBottom mirrors the Python RichLog auto_scroll flag —
// app.go reads it to decide whether to GotoBottom() the viewport after appending.
type ChatView struct {
	messages     []chatEntry
	width        int
	followBottom bool
	// status is the TRANSIENT animated line rendered after the last entry (the "mind is on your line"
	// instrument: spinner · verb · elapsed · thought count). Not part of history; "" hides it. The app
	// rebuilds it each animation frame while the user's line is in flight.
	status string
}

// SetStatus sets (or clears, "") the transient animated status line below the conversation.
func (c *ChatView) SetStatus(s string) { c.status = s }

// NewChatView creates an empty chat view that auto-scrolls (chat_log.py RichLog(auto_scroll=True)).
func NewChatView() ChatView {
	return ChatView{followBottom: followBottomDefault}
}

// SetWidth sets the rendering width (the viewport's content width). Used for body wrapping.
func (c *ChatView) SetWidth(w int) { c.width = w }

// FollowBottom reports whether the view should auto-scroll to the newest line. app.go consults this
// after appending content to decide whether to pin the viewport to the bottom (lathe BUG-100 flag).
func (c *ChatView) FollowBottom() bool { return c.followBottom }

// SetFollowBottom sets the auto-scroll tracking flag. app.go sets false the moment the user scrolls
// up, true again when they reach the bottom.
func (c *ChatView) SetFollowBottom(v bool) { c.followBottom = v }

// Clear resets the conversation (the /reset marker path; chat_log carries no scrollback after).
func (c *ChatView) Clear() { c.messages = nil }

// Len reports the number of voiced lines (for tests / app.go welcome-vs-conversation branch).
func (c *ChatView) Len() int { return len(c.messages) }

// --- the chat_log.py API, ported ---

// Say voices one conversation turn (chat_log.py ChatLog.say). role is one of the `roles` keys
// (user / harness / action / sys); an unknown role falls back to the sys voicing (roleFor's safe
// default), so a stray bus role never breaks the chat.
func (c *ChatView) Say(role, text string) {
	c.messages = append(c.messages, chatEntry{
		kind:      entryRole,
		role:      role,
		text:      text,
		timestamp: time.Now(),
	})
}

// AddUser is a convenience for Say("user", text) — the user's turn.
func (c *ChatView) AddUser(text string) { c.Say("user", text) }

// AddHarness is a convenience for Say("harness", text) — the harness's voiced answer.
func (c *ChatView) AddHarness(text string) { c.Say("harness", text) }

// AddAction is a convenience for Say("action", text) — an effect taken in the open (a reality check).
func (c *ChatView) AddAction(text string) { c.Say("action", text) }

// System voices a faint "  · …" trace line (chat_log.py ChatLog.system): help, status, command
// output. Italic Trace tone, never a conversational turn.
func (c *ChatView) System(text string) {
	c.messages = append(c.messages, chatEntry{kind: entrySystem, text: text, timestamp: time.Now()})
}

// Banner voices a bold titled line (chat_log.py ChatLog.banner): the welcome / reset marker.
func (c *ChatView) Banner(text string) {
	c.messages = append(c.messages, chatEntry{kind: entryBanner, text: text, timestamp: time.Now()})
}

// View renders the whole conversation as the viewport content (DESIGN §4.2 item 2). Each entry is
// voiced per its kind; conversational turns (user/harness) carry a header line (marker + tag + dim
// timestamp) with the body indented two spaces, while the action/sys trace lines stay compact, exactly
// as chat_log.py renders them. Returns "" when empty (app.go shows the welcome screen instead).
func (c ChatView) View() string {
	if len(c.messages) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, e := range c.messages {
		if i > 0 {
			sb.WriteByte('\n')
		}
		switch e.kind {
		case entrySystem:
			sb.WriteString(c.renderSystem(e))
		case entryBanner:
			sb.WriteString(c.renderBanner(e))
		default: // entryRole
			sb.WriteString(c.renderRole(e))
		}
	}
	// the transient animated status (the live "thinking on your line" instrument) renders last, in the
	// trace tone — visually chrome, never a stored turn.
	if c.status != "" {
		sb.WriteString("\n" + wrapStyled(sysTraceStyle, "  "+c.status, c.width))
	}
	return sb.String()
}

// renderRole voices one conversation turn per `roles` (the port of chat_log.py ChatLog.say). The role
// record carries the verbatim prefix, the palette tone, and the bold/italic intent from theme.py.
//
//   - harness: a bold-accent "‹ harness" marker, near-white (Harness) body — the harness's own voiced
//     next thought. Header line with a dim timestamp; body indented two spaces.
//   - user:    a bold-white "› you" marker, whole turn bold white. Header line with a dim timestamp;
//     body indented two spaces.
//   - action:  a plain "↳" marker, italic action-gray body — a reality check. Compact one-liner.
//   - sys:     a faint "·" prefix, whole line italic — the cognition surfacing. Compact one-liner.
func (c ChatView) renderRole(e chatEntry) string {
	r := roleFor(e.role)

	markerStyle := lipgloss.NewStyle().Foreground(r.Color)
	bodyStyle := lipgloss.NewStyle().Foreground(r.Color)
	if r.Italic {
		bodyStyle = bodyStyle.Italic(true)
	}

	switch e.role {
	case "user", "harness", "outreach":
		// A conversational turn: marker + tag header with a dim timestamp, body indented two spaces.
		// The marker IS the role's verbatim prefix (theme.py "› you " / "‹ harness "); the harness body
		// is the near-white Harness tone (distinct from the accent marker), the user turn is bold white
		// throughout — both encoded in theme.Roles.
		// WORD-WRAP the body to the (indent-adjusted) viewport width — without this a long answer or a
		// proactive-outreach line is one over-wide line the viewport hard-clips mid-word (c.width was a
		// dead field). lipgloss .Width wraps + handles ANSI; guarded so an unset/narrow width renders raw.
		bw := c.width - 2 // the 2-space body indent
		body := wrapStyled(bodyStyle, e.text, bw)
		if e.role == "harness" || e.role == "outreach" {
			body = wrapStyled(lipgloss.NewStyle().Foreground(pal.Harness), e.text, bw)
		}
		ts := tsStyle.Render(e.timestamp.Format("15:04"))
		// Trim the leading "\n" the user prefix carries (theme.py "\n› you   ") into a real blank-line
		// gap between turns so the header reads cleanly.
		prefix := strings.TrimLeft(r.Prefix, "\n")
		gap := ""
		if strings.HasPrefix(r.Prefix, "\n") {
			gap = "\n"
		}
		header := markerStyle.Render(prefix) + " " + ts
		return gap + header + "\n" + indentBody(body, "  ")

	default: // action, sys — compact trace lines, no timestamp (chat_log.py renders these as one line)
		// action: plain marker + italic body. sys: the whole "  · text" line italic faint.
		if e.role == "action" {
			return markerStyle.Render(r.Prefix) + bodyStyle.Render(e.text)
		}
		// sys (and the safe-default fall-through): the prefix and text share one italic faint style.
		return bodyStyle.Render(r.Prefix + e.text)
	}
}

// renderSystem voices a faint "  · …" trace line (chat_log.py ChatLog.system) — italic Trace tone.
// Wrapped to the viewport width so long help/status/command output doesn't clip.
func (c ChatView) renderSystem(e chatEntry) string {
	return wrapStyled(sysTraceStyle, "  · "+e.text, c.width)
}

// wrapStyled word-wraps text to w columns under style st (lipgloss .Width wraps + pads + keeps ANSI).
// A non-positive or too-narrow w (unset on first render, or a tiny terminal) renders unwrapped — the
// viewport then clips, which is the safe degenerate behaviour rather than a 0-width panic.
func wrapStyled(st lipgloss.Style, text string, w int) string {
	if w < 24 {
		return st.Render(text)
	}
	return st.Width(w).Render(text)
}

// renderBanner voices a bold titled line (chat_log.py ChatLog.banner) — bold Title tone.
func (c ChatView) renderBanner(e chatEntry) string {
	return bannerStyle.Render(e.text)
}

// indentBody prepends prefix to every line of s (the 2-space body indent under a turn header).
func indentBody(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// Chat styles — foreground-only, sourced from the leaf-local palette (palette.go; DESIGN §5). The
// conversational role tones come from `roles` at render time; these are the few fixed tones (the dim
// timestamp, the sys trace line, the bold banner) that chat_log.py reads off theme.py's C dict directly.
var (
	tsStyle       = lipgloss.NewStyle().Foreground(pal.Faint)              // dim 15:04 stamp
	sysTraceStyle = lipgloss.NewStyle().Italic(true).Foreground(pal.Trace) // "  · …"
	bannerStyle   = lipgloss.NewStyle().Foreground(pal.Title)              // welcome / reset
)
