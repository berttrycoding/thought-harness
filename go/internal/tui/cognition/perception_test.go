package cognition

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// The PERCEPTION ("Senses") panel must surface the two power-cycle senses that otherwise live only in
// the raw JSONL log: the read_clock sense (value + record|replay mode) and the orientation pass (the
// prior-focus gist + the self-state — open_lines, host footprint, recent_events — and whether a
// grounded belief was written).
func TestRenderPerceptionSurfacesSenses(t *testing.T) {
	vm := ViewModel{
		Width: 72,
		Events: []events.Event{
			{Kind: events.PerceptionClock, Summary: "read_clock [record]: 2026-06-20T18:30:00Z", Data: map[string]any{
				"value": "2026-06-20T18:30:00Z", "mode": "record", "tick": 7}},
			{Kind: events.PerceptionOrient, Summary: "orient", Data: map[string]any{
				"tick": 0, "gist": "ship the deploy refactor safely", "clock": "2026-06-20T18:30:00Z",
				"self": "is this refactor safe to ship?", "open_lines": 2, "belief": true, "resume": true,
				"host_ok": true, "alloc_mb": 41, "sys_mb": 72, "goroutines": 18, "recent_events": 240}},
		},
	}
	body := ansi.Strip(renderPerception(vm).Body)
	for _, want := range []string{
		"clock", "2026-06-20T18:30:00Z", "record", // the clock sense: value + mode
		"orient", "resume", // the orientation pass header
		"ship the deploy refactor safely",                  // the prior-focus gist
		"is this refactor safe to ship?",                   // the self-state goal
		"2 open",                                           // the open-lines self-state
		"41MB alloc", "18 goroutines", "240 recent events", // the host footprint
		"sensed date written", // the grounded-belief verdict
	} {
		if !strings.Contains(body, want) {
			t.Errorf("perception panel missing %q:\n%s", want, body)
		}
	}
}

// A missing gist (a cold orient, or a session with no compressed prior focus) renders the explicit
// "(none)" placeholder rather than a bare blank, and a no-belief orient says so.
func TestRenderPerceptionGistNoneAndNoBelief(t *testing.T) {
	vm := ViewModel{
		Width: 72,
		Events: []events.Event{
			{Kind: events.PerceptionOrient, Summary: "orient", Data: map[string]any{
				"tick": 0, "gist": "", "self": "", "open_lines": 0, "belief": false, "resume": false}},
		},
	}
	body := ansi.Strip(renderPerception(vm).Body)
	for _, want := range []string{"orient", "cold", "(none)", "not written"} {
		if !strings.Contains(body, want) {
			t.Errorf("cold-orient perception panel missing %q:\n%s", want, body)
		}
	}
}

// The divergence-refusal variant of perception.clock (a version/substrate mismatch refused replay)
// renders as a distinct REFUSED line, not a normal read.
func TestRenderPerceptionDivergenceRefusal(t *testing.T) {
	vm := ViewModel{
		Width: 72,
		Events: []events.Event{
			{Kind: events.PerceptionClock, Summary: "percept-log REFUSED (divergence)", Data: map[string]any{
				"reason": "divergence", "log_version": 1, "want_version": 2,
				"log_substrate": "cc:sonnet", "want_substrate": "test"}},
		},
	}
	body := ansi.Strip(renderPerception(vm).Body)
	for _, want := range []string{"REFUSED", "divergence", "cc:sonnet", "test"} {
		if !strings.Contains(body, want) {
			t.Errorf("divergence-refusal perception panel missing %q:\n%s", want, body)
		}
	}
}

// With no perception.* events the panel explains itself (sensing is a knob, off by default) rather than
// rendering empty.
func TestRenderPerceptionEmptyPlaceholder(t *testing.T) {
	body := ansi.Strip(renderPerception(ViewModel{Width: 72}).Body)
	if !strings.Contains(body, "no senses") {
		t.Fatalf("empty perception panel should explain itself:\n%q", body)
	}
}

// The panel obeys the rail's width contract at a range of column widths (the wrapped gist / host
// footprint must never emit a line wider than vm.Width).
func TestRenderPerceptionWidthContract(t *testing.T) {
	for _, w := range []int{28, 40, 47, 64, 96} {
		vm := ViewModel{
			Width: w,
			Events: []events.Event{
				{Kind: events.PerceptionClock, Data: map[string]any{
					"value": "2026-06-20T18:30:00Z", "mode": "replay", "tick": 7}},
				{Kind: events.PerceptionOrient, Data: map[string]any{
					"gist": strings.Repeat("a long compressed prior focus ", 4), "self": strings.Repeat("g", 60),
					"open_lines": 5, "belief": true, "resume": true,
					"host_ok": true, "alloc_mb": 41, "sys_mb": 72, "goroutines": 18, "recent_events": 240}},
			},
		}
		body := ansi.Strip(renderPerception(vm).Body)
		for i, ln := range strings.Split(body, "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("w=%d line %d width=%d > %d:\n%q", w, i, got, w, ln)
			}
		}
	}
}
