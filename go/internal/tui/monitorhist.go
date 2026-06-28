package tui

// monitorhist.go — the rolling per-tick strip histories behind the runtime monitors. The monitor
// strips (admit/flag/reject/voiced lanes, the registries used-lane, action acts, value reward, …)
// are "did X happen this tick" booleans over the horizon. The app folds the event stream each tick;
// this accumulates a column per lane and commits it on the tick boundary, so the live pull-up's
// strips show real rolling activity rather than empty lanes.

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// monitorStripCap is the rolling window the monitor strips keep (the default horizon).
const monitorStripCap = 50

// monitorHistory holds one bounded boolean ring per strip lane plus the last seam voicing (the
// raw→voiced pair). cur is the in-progress tick: events set its lanes; commit() pushes them.
type monitorHistory struct {
	admit, flag, reject, voiced []bool // hidden seam
	used, recall                []bool // registries / memory in-use
	acts, input                 []bool // action·grounding
	reward, deferred            []bool // value / regulator

	cur map[string]bool // lanes that fired since the last commit

	rawVoice, voicedVoice string // the most recent seam re-voicing (raw candidate → voiced thought)
}

func newMonitorHistory() *monitorHistory { return &monitorHistory{cur: map[string]bool{}} }

// observe folds one event into the current tick's lane flags. On a Tick event it commits a column.
func (h *monitorHistory) observe(ev events.Event) {
	switch ev.Kind {
	case events.Filter:
		// the verdict rides the summary ("…: ADMIT (0.79) …" / FLAG / REJECT).
		switch {
		case strings.Contains(ev.Summary, "ADMIT"):
			h.cur["admit"] = true
		case strings.Contains(ev.Summary, "REJECT"):
			h.cur["reject"] = true
		case strings.Contains(ev.Summary, "FLAG"):
			h.cur["flag"] = true
		}
	case events.Inject:
		h.cur["voiced"] = true
	case events.Transform:
		// capture the raw→voiced pair from the transform summary ("raw: '...' -> voiced: '...'").
		if raw, voiced, ok := splitVoice(ev.Summary); ok {
			h.rawVoice, h.voicedVoice = raw, voiced
		}
	case events.SubFire, events.SkillMatch:
		h.cur["used"] = true
	case events.MemoryRecall, events.KnowledgeRecall:
		h.cur["used"] = true
		h.cur["recall"] = true
	case events.Act, events.Respond:
		h.cur["acts"] = true
	case events.Port, events.Observation:
		h.cur["input"] = true
	case events.Tick:
		h.commit()
	}
}

// commit pushes the current tick's lane flags onto each ring and resets for the next tick.
func (h *monitorHistory) commit() {
	push := func(ring *[]bool, lane string) {
		*ring = append(*ring, h.cur[lane])
		if len(*ring) > monitorStripCap {
			*ring = (*ring)[len(*ring)-monitorStripCap:]
		}
	}
	push(&h.admit, "admit")
	push(&h.flag, "flag")
	push(&h.reject, "reject")
	push(&h.voiced, "voiced")
	push(&h.used, "used")
	push(&h.recall, "recall")
	push(&h.acts, "acts")
	push(&h.input, "input")
	push(&h.reward, "reward")
	push(&h.deferred, "deferred")
	h.cur = map[string]bool{}
}

// splitVoice extracts the raw and voiced halves of a seam.transform summary of the shape
// `raw: 'X' -> voiced: 'Y'`. ok=false when the summary is not that shape.
func splitVoice(summary string) (raw, voiced string, ok bool) {
	const sep = " -> voiced: "
	i := strings.Index(summary, sep)
	if i < 0 || !strings.HasPrefix(summary, "raw: ") {
		return "", "", false
	}
	raw = strings.Trim(strings.TrimPrefix(summary[:i], "raw: "), " '\"")
	voiced = strings.Trim(summary[i+len(sep):], " '\"")
	return raw, voiced, true
}
