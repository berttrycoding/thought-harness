package popup

// confirm.go — (d) the Confirmation dialog (DESIGN §4.4 row d), ported from options ui/confirm.go +
// lathe ui/approval.go.
//
// A DoubleBorder centered box: `CONFIRM / <prompt> / y confirm   n / Esc cancel`. Presented before any
// consequential action — a real ACTION-layer side effect, /reset, switching to a live substrate — and
// it gates the watched-seam act-threshold (the lathe approval→reality-in pattern). ONLY `y`/`Y` confirms;
// ANY other key cancels, so a world-changing action never fires by accident. The blocking caller hands a
// `chan bool` stashed in the pending state and awaits it on its own goroutine (lathe's approval channel):
// y sends true, anything else sends false; either way the popup closes. DESIGN §4.4.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmResultMsg carries the outcome of the Confirm popup for the App to fold in (it mirrors the
// package-internal confirmResultMsg the base defines in msgs.go). Name identifies which pending action
// this answers; Confirmed is true only on y/Y. Exported so internal/tui/app.go can type-switch on it.
type ConfirmResultMsg struct {
	Name      string
	Confirmed bool
}

// Confirm is the y/n confirmation dialog sub-model (DESIGN §4.4 row d). It owns the key sink while
// pending: y/Y confirms, every other key cancels. It supports BOTH wiring idioms from the references:
//   - a blocking caller (the watched-seam act gate) hands a `chan bool` via SetPending; the popup sends
//     true on y / false otherwise so the caller's goroutine unblocks (lathe approval channel), and
//   - the App folds the same outcome in via the returned ConfirmResultMsg (options' re-dispatch idiom).
type Confirm struct {
	visible bool
	name    string    // which pending action this confirms (the re-dispatch key)
	prompt  string    // the human-readable consequence shown in the box
	respCh  chan bool // optional: a blocking caller awaits the result here
	width   int
}

// NewConfirm creates a hidden confirmation dialog.
func NewConfirm() Confirm { return Confirm{} }

// Visible reports whether the dialog is showing (the modal-priority chain in App.Update checks this).
func (c *Confirm) Visible() bool { return c.visible }

// SetPending opens the dialog for action `name` with the given prompt and (optionally) a response
// channel the blocking caller awaits. Pass respCh=nil when only the App-side ConfirmResultMsg is wanted.
func (c *Confirm) SetPending(name, prompt string, respCh chan bool) {
	c.visible = true
	c.name = name
	c.prompt = prompt
	c.respCh = respCh
}

// SetWidth records the available width for sizing the box.
func (c *Confirm) SetWidth(w int) { c.width = w }

// Hide closes the dialog WITHOUT answering a waiting caller (use the Update key path or Cancel to send
// a result). Defensive cleanup only.
func (c *Confirm) Hide() {
	c.visible = false
	c.name, c.prompt, c.respCh = "", "", nil
}

// Update drives the dialog while it owns the key sink: ONLY y/Y confirms; ANY other key cancels (the
// options handleConfirmKey discipline — n, Esc, Enter, or stray typing all cancel). It answers a waiting
// channel and returns a ConfirmResultMsg so the App can re-dispatch. ctrl+c still quits the app (handled
// by the App before delegating). Value-receiver modal idiom.
func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	if !c.visible {
		return c, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	confirmed := km.String() == "y" || km.String() == "Y"
	name := c.name
	c.answer(confirmed)
	return c, func() tea.Msg { return ConfirmResultMsg{Name: name, Confirmed: confirmed} }
}

// answer sends the outcome to a waiting caller (if any) and closes the dialog. Single exit so a result
// is never sent twice and the channel never leaks a second send.
func (c *Confirm) answer(confirmed bool) {
	if c.respCh != nil {
		c.respCh <- confirmed
	}
	c.visible = false
	c.name, c.prompt, c.respCh = "", "", nil
}

// Confirm answers a waiting caller with true and closes (the programmatic accept path, e.g. an
// auto-approve mode). Mirrors lathe ApprovalState.Approve.
func (c *Confirm) Confirm() {
	if c.visible {
		c.answer(true)
	}
}

// Cancel answers a waiting caller with false and closes (the programmatic deny path). Mirrors lathe
// ApprovalState.Deny.
func (c *Confirm) Cancel() {
	if c.visible {
		c.answer(false)
	}
}

// View renders the centered DoubleBorder dialog: `CONFIRM / <prompt> / y confirm   n / Esc cancel`.
// Returns the natural-sized box; the App lipgloss.Place's it center/center (DESIGN §4.4). Ported from
// options ConfirmModal.
func (c *Confirm) View() string {
	if !c.visible {
		return ""
	}
	choices := emphStyle.Render("y") + mutedStyle.Render(" confirm") +
		faintStyle.Render("      ") + emphStyle.Render("n") + mutedStyle.Render(" / Esc cancel")
	lines := []string{
		titleStyle.Render("CONFIRM"),
		"",
		emphStyle.Render(c.prompt),
		"",
		choices,
	}
	return popupBox.Render(strings.Join(lines, "\n"))
}
