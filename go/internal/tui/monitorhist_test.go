package tui

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

func TestMonitorHistoryAccruesLanesPerTick(t *testing.T) {
	h := newMonitorHistory()
	// tick 1: an admit + a voiced injection + a specialist fire.
	h.observe(events.Event{Kind: events.Filter, Summary: "INJECTED/compute: ADMIT (0.79) source-trusted"})
	h.observe(events.Event{Kind: events.Inject, Summary: "INJECTED (compute, conf=0.79)"})
	h.observe(events.Event{Kind: events.SubFire, Summary: "compute fired"})
	h.observe(events.Event{Kind: events.Tick}) // commit
	// tick 2: a reject only.
	h.observe(events.Event{Kind: events.Filter, Summary: "INJECTED/advocate: REJECT (0.03) refuted_by_reality"})
	h.observe(events.Event{Kind: events.Tick})

	if len(h.admit) != 2 || !h.admit[0] || h.admit[1] {
		t.Errorf("admit lane wrong: %v", h.admit)
	}
	if len(h.reject) != 2 || h.reject[0] || !h.reject[1] {
		t.Errorf("reject lane wrong: %v", h.reject)
	}
	if !h.voiced[0] || h.voiced[1] {
		t.Errorf("voiced lane wrong: %v", h.voiced)
	}
	if !h.used[0] || h.used[1] {
		t.Errorf("used lane (SubFire) wrong: %v", h.used)
	}
}

func TestMonitorHistoryCapsAtHorizon(t *testing.T) {
	h := newMonitorHistory()
	for i := 0; i < monitorStripCap+20; i++ {
		h.observe(events.Event{Kind: events.Tick})
	}
	if len(h.admit) != monitorStripCap {
		t.Errorf("lane should cap at %d, got %d", monitorStripCap, len(h.admit))
	}
}

func TestSplitVoice(t *testing.T) {
	raw, voiced, ok := splitVoice("raw: '12 / 12 = 1' -> voiced: 'Right, that gives 12 / 12 = 1.'")
	if !ok || raw != "12 / 12 = 1" || voiced != "Right, that gives 12 / 12 = 1." {
		t.Errorf("splitVoice wrong: raw=%q voiced=%q ok=%v", raw, voiced, ok)
	}
	if _, _, ok := splitVoice("not a voice summary"); ok {
		t.Error("non-voice summary should not parse")
	}
}
