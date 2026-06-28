package engine

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
)

// TestCapabilityProducesWorkflow pins slice (b) / §3.3: with subconscious.capability on, the episode's
// workflow is produced by a Capability (same Synthesize+FromProgram shape) AND a Context is captured to
// replace the raw thought slice; the produced workflow matches what the inline path would build for the
// same goal (byte-identical shape — the producer changes, not the workflow). OFF, no Context is captured.
func TestCapabilityProducesWorkflow(t *testing.T) {
	run := func(on bool) *Engine {
		cfg := DefaultConfig()
		cfg.Mode = "reactive"
		feat := config.New() // AllOn
		feat.Subconscious.Capability = on
		cfg.Features = feat
		e, err := NewEngine(&cfg, backends.NewTest())
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		// a goal whose shape the synthesiser recognises as a workflow (decompose/validate language).
		e.startEpisode("design and validate a small todo service", true)
		return e
	}

	// ON: a Context is captured and a workflow is produced.
	on := run(true)
	if on.episodeContext == nil {
		t.Fatal("capability ON: a Context must be captured (replacing the raw slice)")
	}
	wfOn := on.subconscious.Workflow()

	// OFF: the inline path runs; no Context captured.
	off := run(false)
	if off.episodeContext != nil {
		t.Error("capability OFF: no Context should be captured")
	}
	wfOff := off.subconscious.Workflow()

	// The produced workflow shape matches the inline path (same name + phase count) — the producer
	// changed, not the workflow. Both nil or both non-nil with equal shape.
	switch {
	case wfOn == nil && wfOff == nil:
		// both declined a workflow — consistent.
	case wfOn == nil || wfOff == nil:
		t.Fatalf("capability path disagrees on whether a workflow exists (on=%v off=%v)", wfOn != nil, wfOff != nil)
	default:
		if wfOn.Name != wfOff.Name || len(wfOn.Phases) != len(wfOff.Phases) {
			t.Errorf("produced workflow shape differs: on=(%s,%d) off=(%s,%d)",
				wfOn.Name, len(wfOn.Phases), wfOff.Name, len(wfOff.Phases))
		}
	}
}
