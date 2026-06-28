package engine_test

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// THE live repro (2026-06-12): "hi" in continuous mode — the engine thought forever and never replied.
func TestAwakeMindAnswersAGreeting(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 8; i++ {
		eng.Step()
	}
	eng.SubmitDefault("hi")
	for i := 0; i < 60 && len(respondsOf(log)) == 0; i++ {
		eng.Step()
	}
	if len(respondsOf(log)) == 0 {
		t.Fatalf("a greeting was never answered in 60 ticks (outreach=%d) — the live mute repro",
			len(log.of(events.Respond))-len(respondsOf(log)))
	}
}

// And the conversational probe — "are you there?" — must produce a reply, not just the interrupt.
func TestAwakeMindAnswersAreYouThere(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)
	for i := 0; i < 8; i++ {
		eng.Step()
	}
	eng.SubmitDefault("are you there?")
	for i := 0; i < 60 && len(respondsOf(log)) == 0; i++ {
		eng.Step()
	}
	if len(respondsOf(log)) == 0 {
		t.Fatal("'are you there?' got an interrupt but never a reply — heard and then ignored")
	}
}
