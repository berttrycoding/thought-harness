// Package interaction ports the inbound channels — the Interaction Port (reactive) and the
// Perception Port (continuous).
//
// Hearing is input (USER_INPUT / PERCEPT via the Filter); speaking is an Action across the
// watched seam. The port can raise an interrupt: on arrival the Controller compresses the
// active branch, focuses a branch for the input, and re-seeds the value signal.
//
// Tier honesty: deliver() needs a Filter, but the Filter lives in the seams package (Tier 2,
// wide deps). To keep interaction at its dependency depth, deliver() takes a NARROW one-method
// Admitter interface (below) instead of hard-importing seams.Filter. *seams.Filter satisfies
// it structurally — its Admit method has exactly this signature — so the engine passes a real
// Filter at wire time with no adapter.
package interaction

import (
	"math"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// Admitter is the narrow admission contract deliver() depends on — exactly the one method it
// uses off the Filter. *seams.Filter satisfies this structurally (same signature), so we avoid
// the seams import (a Tier-2 wide dep) and keep the tier honest. Mirrors the only call
// interaction.deliver makes against Python's Filter: `filt.admit(cand, history, value)`.
type Admitter interface {
	Admit(c types.Candidate, hist []types.Thought, value float64) types.FilterVerdict
}

// msg is one queued inbound message. Mirrors Python's `_Msg` dataclass:
//
//	text: str; source: Source = USER_INPUT; salient: bool = True
//
// The defaults are applied by the Receive helper (Go has no field defaults).
type msg struct {
	text    string
	source  types.Source
	salient bool
}

// InteractionPort is the external inbound (reactive) channel. It delivers USER_INPUT as a
// high-salience thought and may interrupt. Mirrors Python InteractionPort.
type InteractionPort struct {
	emit  events.Emit
	inbox []msg
}

// NewInteractionPort builds a port over the injected emit closure. inbox starts empty.
func NewInteractionPort(emit events.Emit) *InteractionPort {
	return &InteractionPort{emit: emit}
}

// Receive queues a message and emits port. Mirrors Python receive(message, *, source, salient):
// the keyword defaults (source=USER_INPUT, salient=True) are the Python signature defaults; the
// caller passes them explicitly here. The summary reprs the first 48 code points of the text and
// the data carries the full text plus the source NAME string.
func (p *InteractionPort) Receive(message string, source types.Source, salient bool) {
	p.inbox = append(p.inbox, msg{text: message, source: source, salient: salient})
	p.emit(
		events.Port,
		"received: "+pyRepr(runeSlice(message, 48)),
		events.D{"text": message, "source": source.String()},
	)
}

// ReceiveDefault is the Python default call: source=USER_INPUT, salient=True. Provided so the
// common path stays a one-arg call (Python's keyword defaults).
func (p *InteractionPort) ReceiveDefault(message string) {
	p.Receive(message, types.USER_INPUT, true)
}

// Pending reports whether the inbox holds any message. Mirrors Python pending().
func (p *InteractionPort) Pending() bool { return len(p.inbox) > 0 }

// QueuedMessage is the exported view of a queued inbound message, so the engine can carry the
// pending inbox across a reactive<->continuous port swap (set_mode). Python read/wrote the port's
// `inbox` list directly; Go's inbox is unexported, so the transfer goes through these two methods.
type QueuedMessage struct {
	Text    string
	Source  types.Source
	Salient bool
}

// PendingMessages snapshots the queued inbound messages (FIFO order). Used by the engine's set_mode
// to carry the inbox over to the new port type. Mirrors Python's `pending = list(self.port.inbox)`.
func (p *InteractionPort) PendingMessages() []QueuedMessage {
	out := make([]QueuedMessage, len(p.inbox))
	for i, m := range p.inbox {
		out[i] = QueuedMessage{Text: m.text, Source: m.source, Salient: m.salient}
	}
	return out
}

// RestoreMessages replaces the inbox with the given queued messages (FIFO order). The companion to
// PendingMessages for the set_mode port swap. Mirrors Python's `self.port.inbox = pending`.
func (p *InteractionPort) RestoreMessages(msgs []QueuedMessage) {
	p.inbox = make([]msg, len(msgs))
	for i, m := range msgs {
		p.inbox[i] = msg{text: m.Text, source: m.Source, salient: m.Salient}
	}
}

// Pop takes the next raw message text (used to seed a new episode's goal). Returns "", false on
// an empty inbox — the Go form of Python's `str | None` (None == ok=false). Mirrors Python pop():
// a FIFO dequeue (inbox.pop(0)) of the head's text.
func (p *InteractionPort) Pop() (string, bool) {
	if len(p.inbox) == 0 {
		return "", false
	}
	head := p.inbox[0]
	p.inbox = p.inbox[1:]
	return head.text, true
}

// Deliver admits the next inbound message at intake (the Filter screens the RAW candidate before
// it is voiced) and returns the voiced USER_INPUT thought, or nil when the inbox is empty OR the
// input is rejected. Mirrors Python deliver(filt, history, value):
//   - empty inbox -> nil (no event);
//   - FIFO dequeue the head; wrap it as a Candidate with relevance=0.95;
//   - filt.Admit screens it; if not admitted, emit the REJECTED port event and return nil;
//   - on admission, emit the delivered port event and return a Thought with id=-1 (the engine
//     assigns the real id on append) carrying the verdict's confidence.
func (p *InteractionPort) Deliver(filt Admitter, history []types.Thought, value float64) *types.Thought {
	if len(p.inbox) == 0 {
		return nil
	}
	m := p.inbox[0]
	p.inbox = p.inbox[1:]
	cand := types.Candidate{Text: m.text, Source: m.source, Relevance: 0.95}
	verdict := filt.Admit(cand, history, value) // parsed / screened at intake
	if !verdict.Admit() {
		p.emit(events.Port, "input REJECTED at intake: "+pyRepr(runeSlice(m.text, 40)), events.D{})
		return nil
	}
	p.emit(
		events.Port,
		"delivered USER_INPUT: "+pyRepr(runeSlice(m.text, 40)),
		events.D{"text": m.text},
	)
	return &types.Thought{ID: -1, Text: m.text, Source: m.source, Confidence: verdict.Confidence}
}

// PerceptionPort generalises the Interaction Port: an always-on afferent stream (continuous
// mode). Percepts (incl. USER_INPUT and async action-feedback) arrive unsolicited and compete
// for attention; gain scales with arousal. Go has no inheritance — it EMBEDS *InteractionPort,
// reproducing Python's `class PerceptionPort(InteractionPort)`, so Receive/Pending/Pop/Deliver
// are promoted unchanged.
type PerceptionPort struct {
	*InteractionPort
}

// NewPerceptionPort builds a PerceptionPort over the injected emit closure.
func NewPerceptionPort(emit events.Emit) *PerceptionPort {
	return &PerceptionPort{InteractionPort: NewInteractionPort(emit)}
}

// Stream takes a gain-scaled batch of percepts off the head of the inbox and emits port when the
// batch is non-empty. Mirrors Python stream(gain):
//
//	take = max(1, int(round(gain * len(self.inbox))))
//	out, self.inbox = self.inbox[:take], self.inbox[take:]
//
// round() is Python's round-half-to-even on the float gain*len; take is then clamped to ≥1 and
// to the inbox length (slicing past the end is a no-op in Python — replicated by the cap). An
// empty inbox returns an empty batch with no event. Each percept is surfaced as a QueuedMessage
// (the exported view of the internal _Msg; the continuous loop reads Text/Source/Salient off it).
func (p *PerceptionPort) Stream(gain float64) []QueuedMessage {
	if len(p.inbox) == 0 {
		return nil
	}
	take := int(pyRound(gain * float64(len(p.inbox))))
	if take < 1 {
		take = 1
	}
	if take > len(p.inbox) {
		take = len(p.inbox) // Python slicing past the end clamps; the explicit clamp mirrors it
	}
	batch := p.inbox[:take]
	p.inbox = p.inbox[take:]
	out := make([]QueuedMessage, len(batch))
	for i, m := range batch {
		out[i] = QueuedMessage{Text: m.text, Source: m.source, Salient: m.salient}
	}
	if len(out) > 0 {
		p.emit(
			events.Port,
			"perceived "+strconv.Itoa(len(out))+" percept(s) (gain="+f2(gain)+")",
			events.D{"count": len(out)},
		)
	}
	return out
}

// Salient reports whether any queued percept is flagged salient. Mirrors Python salient():
// `any(m.salient for m in self.inbox)`.
func (p *PerceptionPort) Salient() bool {
	for _, m := range p.inbox {
		if m.salient {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// formatting helpers — faithful to the Python f-string / repr formatting at the SAME emit
// sites, so the JSONL wire stays byte-identical (mirrors the seams package's private copies;
// duplicated here to avoid importing seams, a Tier-2 wide dep, into this Tier-3 leaf).
// ----------------------------------------------------------------------------

// pyRound reproduces Python 3 round(x) (no ndigits) — round-half-to-even to the nearest integer,
// returned as a float (Python returns an int, but the only caller immediately int()s it). Go's
// math.RoundToEven is exactly Python's banker's rounding.
func pyRound(x float64) float64 { return math.RoundToEven(x) }

// runeSlice reproduces Python str slicing `s[:n]` — by code point (rune), not byte.
func runeSlice(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// f2 reproduces Python f"{x:.2f}" — fixed 2 decimals, round-half-to-even.
func f2(x float64) string { return strconv.FormatFloat(x, 'f', 2, 64) }

// pyRepr reproduces CPython's str repr (the f-string `!r` conversion), so the port summaries are
// byte-identical to Python. Pick the quote (single by default; double when the string holds a
// single quote and no double quote), escape backslash, the chosen quote, \n \r \t, and
// non-printable code points as \xHH / \uHHHH / \UHHHHHHHH.
func pyRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case isPyPrintable(r):
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(`\x`)
			b.WriteString(hex2(uint32(r), 2))
		case r < 0x10000:
			b.WriteString(`\u`)
			b.WriteString(hex2(uint32(r), 4))
		default:
			b.WriteString(`\U`)
			b.WriteString(hex2(uint32(r), 8))
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// isPyPrintable approximates Python str.isprintable() for the repr escape decision (exact for
// ASCII-and-common-text; errs toward printing beyond, matching the seams copy).
func isPyPrintable(r rune) bool {
	if r < 0x20 || r == 0x7f {
		return false // C0 controls + DEL
	}
	if r >= 0x80 && r <= 0xa0 {
		return false // C1 controls + NBSP boundary
	}
	return true
}

// hex2 formats v as lower-case hex left-padded with zeros to width.
func hex2(v uint32, width int) string {
	s := strconv.FormatUint(uint64(v), 16)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
