// waketranscript.go — B3-OUTREACH WIRING FIX: persist the seed user turn on the continuous WAKE path.
//
// THE BUG (B3-outreach characterization — measured, fired 0x over ~38 min on claude). The continuous
// loop's first awake tick takes the graph==nil branch (continuous.go): it Pop()s the queued user
// percept and seeds the episode via startEpisode(seed, fromUser=true) — which stamps the goal
// USER_INPUT but NEVER appends the user turn to e.transcript.
//
// The reactive loop DOES append the user turn (reactive.go:693); the continuous percept-stream path
// DOES (continuous.go:76); ONLY the WAKE path doesn't. Because Pop() FIFO-dequeues the seed, the
// percept-stream Stream(gain) never re-delivers it either, so the seed user turn is lost to the
// transcript entirely.
//
// CONSEQUENCE. maybeReachOut builds userTopics from role=="user" transcript turns; on the wake path
// that set is always empty, so the gate returns early and proactive outreach can NEVER fire live. It
// also breaks the catch-22 (the topic is gone after it is answered) because the topic never entered
// the transcript to persist in the first place.
//
// THE FIX (flag-gated, default-OFF byte-identical). When the flag is ON, the wake path appends the
// seed user turn to e.transcript so it MATCHES the reactive + percept-stream paths. The wake path is
// the ONLY caller missing the append (reactive.go:693 and continuous.go:76 already append on their own
// paths and are untouched — no double-append). Default OFF => the wake path is byte-identical (the bug
// remains the default until the claude characterization validates flipping it on).
//
// FLAG. THOUGHT_WAKE_TRANSCRIPT (env-knob, resolved ONCE at init like THOUGHT_FORCE_GROUND /
// THOUGHT_MODEL_SELECT). ON for "1"/"true"/"yes"/"on"; else OFF.
package engine

import (
	"os"
	"strings"
)

// wakeTranscriptEnabled is the THOUGHT_WAKE_TRANSCRIPT toggle resolved ONCE at init (unset / false / 0
// is OFF — byte-identical). Mirrors the resolveForceGround env-knob pattern.
var wakeTranscriptEnabled = resolveWakeTranscript()

// resolveWakeTranscript reads THOUGHT_WAKE_TRANSCRIPT once. ON for "1"/"true"/"yes"/"on"; else OFF.
func resolveWakeTranscript() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("THOUGHT_WAKE_TRANSCRIPT"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// wakeTranscriptOn reports whether the wake-path transcript wiring is active: the CONFIG knob
// (conscious.activity.proactive_outreach — set by the awake profiles so outreach works out of the box)
// OR the env override. Config-driven so a profile pick gives a complete self-reaching awake mind
// without an env var; env stays a manual override.
func (e *Engine) wakeTranscriptOn() bool {
	if e.features != nil && e.features.Conscious.Activity.ProactiveOutreach {
		return true
	}
	return wakeTranscriptEnabled
}
