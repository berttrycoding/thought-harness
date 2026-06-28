package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestRetracementDrainRouting pins slice (c) / §2b: a late injection buffered against a PASSED decision
// node makes the Controller fire mcp.Reenter (a new line forks, nothing overwritten, a conscious.mcp
// retracement event is emitted); one anchored to the ACTIVE line injects at the head; an aged one drops
// as stale. Default (retracement OFF) never drains the buffer.
func TestRetracementDrainRouting(t *testing.T) {
	mk := func(on bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Conscious.Activity.Retracement = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		e.startEpisode("solve x", true) // root branch 0
		return e
	}

	// --- ProposeRetracement: anchor is a PASSED branch -> Reenter fires (fork + retracement event) ---
	e := mk(true)
	passed := e.graph.ActiveBranch // branch 0 — the decision node we will leave behind
	nb := e.mcp.Branch("explore alt", nil)
	e.mcp.Focus(nb) // now the active line is nb; branch 0 is passed
	if e.graph.ActiveBranch == passed {
		t.Fatal("setup: expected to have focused off the anchor branch")
	}
	var reentered bool
	e.bus.Subscribe(func(ev events.Event) {
		if ev.Kind == string(events.MCP) {
			if op, _ := ev.Data["op"].(string); op == "reenter" {
				reentered = true
			}
		}
	})
	before := len(e.graph.Branches)
	e.BufferLateInjection("the calc came back: x=7", passed, 0)
	e.drainRetracements(1)
	if !reentered {
		t.Error("ProposeRetracement: expected a conscious.mcp reenter event")
	}
	if len(e.graph.Branches) <= before {
		t.Errorf("ProposeRetracement: expected a new forked line (branches %d -> %d)", before, len(e.graph.Branches))
	}
	if _, ok := e.graph.Branches[passed]; !ok {
		t.Error("ProposeRetracement: the passed line must NOT be overwritten")
	}

	// --- InjectAtHead: anchor IS the active line -> the injection is appended (no fork) ---
	e2 := mk(true)
	active := e2.graph.ActiveBranch
	nThoughts := len(e2.graph.History())
	nBranches := len(e2.graph.Branches)
	e2.BufferLateInjection("late note on the current line", active, 0)
	e2.drainRetracements(1)
	if len(e2.graph.Branches) != nBranches {
		t.Errorf("InjectAtHead: must not fork (branches %d -> %d)", nBranches, len(e2.graph.Branches))
	}
	if len(e2.graph.History()) <= nThoughts {
		t.Error("InjectAtHead: expected the injection appended to the active head")
	}

	// --- DropStale: an aged injection drops, no fork, no append ---
	e3 := mk(true)
	dThoughts := len(e3.graph.History())
	dBranches := len(e3.graph.Branches)
	e3.BufferLateInjection("ancient note", e3.graph.ActiveBranch, 0)
	e3.drainRetracements(100) // 100 - 0 > maxAge(8) -> stale
	if len(e3.graph.History()) != dThoughts || len(e3.graph.Branches) != dBranches {
		t.Error("DropStale: an aged injection must not touch the graph")
	}

	// --- default OFF: retracementEnabled is false even with a buffered item ---
	eOff := mk(false)
	eOff.BufferLateInjection("note", eOff.graph.ActiveBranch, 0)
	if eOff.retracementEnabled() {
		t.Error("retracement OFF: the buffer must not be drained")
	}
}
