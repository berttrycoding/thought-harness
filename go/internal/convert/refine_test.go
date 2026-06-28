package convert

// refine_test.go proves the COGNITION of GAP 11 — the uniform per-registry
// refine loop (01-subconscious.md §3.17/§3.20) as the FIRST CONCRETE engine-side
// instance of the generalised eval object. It asserts the THINKING, not the
// plumbing: the loop FIRES over the minted-specialist registry, measures each
// entry against the registry's measuring-stick reference (absolute) AND
// comparatively vs its own past (instance-eval), surfaces the right
// improve/keep/prune SIGNAL, and — critically — NEVER mutates the registry on
// this path (signal-only).

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/eval"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// recEmit records emitted events so a test can inspect the refine signal.
type recEmit struct{ ev []events.Event }

func (r *recEmit) emit(kind, summary string, data events.D) events.Event {
	e := events.Event{Kind: kind, Summary: summary, Data: data}
	r.ev = append(r.ev, e)
	return e
}

// withKind returns the recorded events of one kind.
func (r *recEmit) withKind(k events.Kind) []events.Event {
	var out []events.Event
	for _, e := range r.ev {
		if e.Kind == string(k) {
			out = append(out, e)
		}
	}
	return out
}

// TestRefineLoopDisabledIsByteSilent: with the loop OFF (the default), a refine
// pass is a no-op — it emits nothing and reports not-ran. This is the
// default-OFF byte-identical contract at the convert seam.
func TestRefineLoopDisabledIsByteSilent(t *testing.T) {
	rec := &recEmit{}
	c := New(&fakeReg{}, rec.emit, nil, nil)
	c.SetMintGate(gateStick(0.2)) // a gate attached, but the loop is OFF
	c.Observe(buildEpisode("compute the tax bracket value", 3, 0.9))
	c.Consolidate() // mints (emits convert.mint), but no refine pass

	if rep, ran := c.RefineRegistry(); ran || len(rep.Entries) != 0 {
		t.Fatalf("with the loop OFF, RefineRegistry must be a no-op; ran=%v entries=%d", ran, len(rep.Entries))
	}
	if got := rec.withKind(events.RegistryRefine); len(got) != 0 {
		t.Fatalf("the disabled loop must emit no convert.refine events; got %d", len(got))
	}
}

// TestRefineLoopFiresAndKeepsAHealthyMint: the loop ON measures a minted entry
// that still clears its stick -> Keep (steady state). The pass FIRES (a summary
// event), the entry passes the absolute bar, and the registry is NOT mutated
// (the mint stays live — signal-only).
func TestRefineLoopFiresAndKeepsAHealthyMint(t *testing.T) {
	rec := &recEmit{}
	reg := &fakeReg{}
	c := New(reg, rec.emit, nil, nil)
	c.SetMintGate(gateStick(0.2))
	c.EnableRefineLoop(true, 0.05)

	c.Observe(buildEpisode("compute the tax bracket value", 3, 0.9)) // value 0.9 >> floor
	if minted := c.Consolidate(); len(minted) != 1 {
		t.Fatalf("setup: expected one mint; got %v", minted)
	}

	rep, ran := c.RefineRegistry()
	if !ran {
		t.Fatal("the loop ON must FIRE over the one minted entry")
	}
	if len(rep.Entries) != 1 {
		t.Fatalf("one minted entry => one entry report; got %d", len(rep.Entries))
	}
	e := rep.Entries[0]
	if !e.Pass {
		t.Fatalf("a 0.9-value mint must clear the 0.2 stick (Pass); got %+v", e)
	}
	if e.Verdict == eval.Prune {
		t.Fatalf("a healthy mint must not be Prune-flagged; got %+v", e)
	}
	// signal-only: the registry was NOT mutated — the mint is still live, not demoted.
	if len(c.Demoted) != 0 {
		t.Fatalf("the refine pass must NOT demote (signal-only); Demoted=%v", c.Demoted)
	}
	if sp := mintedPrimitiveSubAgent(reg); sp == nil || sp.Demoted() {
		t.Fatalf("the minted specialist must remain live after a refine pass; sp=%v", sp)
	}
	// the per-registry summary event fired.
	if got := rec.withKind(events.RegistryRefine); len(got) == 0 {
		t.Fatal("the refine pass must emit a per-registry convert.refine summary")
	}
}

// TestRefineLoopFlagsPrunableWithoutMutating is the load-bearing cognition
// assertion: a minted entry whose LATEST grounded outcome falls BELOW the stick
// bar is flagged PRUNE by the refine loop (reference-eval reject — "no longer
// belongs"), the Prunable signal names it, AND a per-entry convert.refine event
// surfaces it — yet the loop NEVER demotes the entry itself (the keep-or-revert
// path owns mutation). The refine loop is the SIGNAL; the eviction stays
// separately gated.
func TestRefineLoopFlagsPrunableWithoutMutating(t *testing.T) {
	rec := &recEmit{}
	reg := &fakeReg{}
	c := New(reg, rec.emit, nil, nil)
	c.SetMintGate(gateStick(0.2))
	c.EnableRefineLoop(true, 0.05)

	goal := "compute the tax bracket value"
	// mint at high value, then re-observe at a value BELOW the stick floor WITHOUT triggering the
	// keep-or-revert demotion (Observe accumulates lastVal; we drive the refine pass directly so we
	// isolate the refine signal from the demotion mechanism).
	c.Observe(buildEpisode(goal, 3, 0.9))
	if minted := c.Consolidate(); len(minted) != 1 {
		t.Fatalf("setup: expected one mint; got %v", minted)
	}
	// the pattern's standing slips below the bar (a real regression in its grounded outcome).
	c.Observe(buildEpisode(goal, 1, 0.10)) // 0.10 < 0.20 stick floor

	// the keep-or-revert path would normally consume this on the NEXT Consolidate; here we measure the
	// refine SIGNAL it raises FIRST, on the still-live entry.
	rep, ran := c.RefineRegistry()
	if !ran || len(rep.Entries) != 1 {
		t.Fatalf("the loop must fire over the one (still-live) minted entry; ran=%v entries=%d", ran, len(rep.Entries))
	}
	e := rep.Entries[0]
	if e.Pass {
		t.Fatalf("a 0.10-value entry must FAIL the 0.20 stick; got %+v", e)
	}
	if e.Verdict != eval.Prune {
		t.Fatalf("a below-bar entry must be flagged Prune; got verdict %v (%+v)", e.Verdict, e)
	}
	prunable := rep.Prunable()
	if len(prunable) != 1 || prunable[0] != "learned:"+goalKeyOf(goal) {
		t.Fatalf("Prunable must name the failing entry; got %v", prunable)
	}
	// SIGNAL-ONLY: the refine pass itself did NOT demote — c.Demoted is untouched by RefineRegistry.
	if len(c.Demoted) != 0 {
		t.Fatalf("the refine loop must NOT mutate the registry (signal-only); Demoted=%v", c.Demoted)
	}
	// a per-entry convert.refine event surfaced the prune signal (observability).
	var sawPrune bool
	for _, ev := range rec.withKind(events.RegistryRefine) {
		if ev.Data["kind"] == "refine_entry" && ev.Data["verdict"] == "prune" {
			sawPrune = true
		}
	}
	if !sawPrune {
		t.Fatal("a per-entry convert.refine event must surface the prune verdict")
	}
}

// goalKeyOf exposes the package-private goalKey for the test's expected id.
func goalKeyOf(goal string) string { return goalKey(goal) }
