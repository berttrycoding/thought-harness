package engine_test

// Cognition-property tests for the L0 conformance WIRING SCAN (Track H, benchmark-taxonomy §1 L0).
//
// The "cognition" this instrument asserts is the WIRING-GATE LESSON itself: "tests passing != the feature
// runs; a unit that exists but is not on the engine's actual tick is dead." A scan that always PASSED would
// be a tautology — useless. These tests pin the property that makes it REAL:
//
//   1. POSITIVE — with the flag ON, a live scenario run emits the conformance.wiring witness on the engine's
//      OWN bus AND the scan recognises every named subsystem layer that actually fired (the live loop is
//      wired, the gate says PASS). This is the live-loop wiring proof the build discipline requires.
//   2. NEGATIVE — when a REQUIRED layer is absent from the live loop's coverage, the scan FAILS and names the
//      missing layer (a dead-wired subsystem is caught). This is the half a tautological gate would miss.
//   3. BYTE-IDENTICAL — with the flag OFF (the default) the engine attaches NO tap, records NOTHING, and
//      emits NO conformance.wiring event — the live loop is unchanged.
//
// Deterministic + offline: every engine runs on the TestBackend test double + the seeded RNG. CONTROL-only
// instrument (no model call), so it is exempt from the live-claude definition-of-done.

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/scenarios"
)

// newConformanceEngine builds an engine with the conformance.self_check tap ON (the wiring-coverage tap
// armed) on the test double + a fixed seed, and subscribes an eventLog so the test can read the stream.
func newConformanceEngine(t *testing.T, id string) (*engine.Engine, *eventLog) {
	t.Helper()
	sc, ok := scenarios.Get(id)
	if !ok {
		t.Fatalf("unknown scenario %q", id)
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = sc.Mode
	cfg.Seed = 7
	feats := config.New()
	feats.Conformance.SelfCheck = true
	cfg.Features = feats
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// TestConformanceWiringScanCatchesLiveAndDeadWiring is the wiring-gate cognition-property test. It pins
// both halves of the lesson on ONE live run: the scan SEES the subsystems that fired, and the scan FAILS on
// the one it pretends is required-but-absent.
func TestConformanceWiringScanCatchesLiveAndDeadWiring(t *testing.T) {
	e, _ := newConformanceEngine(t, "S5") // S5 exercises the widest set (Watched Seam, Action, Observation)
	if _, err := scenarios.RunScenario("S5", e); err != nil {
		t.Fatalf("RunScenario(S5): %v", err)
	}

	covered, count := e.WiringCoverage()
	if count == 0 {
		t.Fatalf("wiring coverage empty: the live loop emitted nothing — the tap is not seeing the bus")
	}
	// the live loop MUST have driven the core cognitive subsystems (this is the wiring proof: these are not
	// asserted to merely compile — they are asserted to have FIRED on the actual tick).
	want := []string{"conscious", "seam", "critic", "value", "regulator", "lifecycle", "action"}
	seen := make(map[string]bool, len(covered))
	for _, l := range covered {
		seen[l] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("wiring scan: required layer %q never fired on the live loop (covered=%v) — dead-wired", w, covered)
		}
	}

	// POSITIVE: requiring exactly the layers that fired ⇒ the scan PASSES and emits conformance.wiring.
	posLog := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { posLog.events = append(posLog.events, ev) })
	if ok := e.EmitWiringScan(want); !ok {
		t.Errorf("EmitWiringScan(fired layers) = false, want true — the gate is falsely failing a wired loop")
	}
	wires := posLog.of(events.ConformanceWiring)
	if len(wires) != 1 {
		t.Fatalf("conformance.wiring emitted %d times on the live bus, want 1 (the live-loop witness must fire)", len(wires))
	}
	if okv, _ := boolData(wires[0], "ok"); !okv {
		t.Errorf("conformance.wiring ok=false on a fully-wired run, want true: %v", wires[0].Data)
	}

	// NEGATIVE: require a layer that provably did NOT fire (a dead-wired subsystem) ⇒ the scan FAILS and the
	// witness names it as missing. THIS is what makes the gate real, not a tautology.
	negLog := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { negLog.events = append(negLog.events, ev) })
	deadWire := "deadwire_never_fires"
	if ok := e.EmitWiringScan(append([]string{deadWire}, want...)); ok {
		t.Errorf("EmitWiringScan(with a never-fired layer) = true, want false — the gate failed to catch a dead-wired subsystem")
	}
	negWires := negLog.of(events.ConformanceWiring)
	if len(negWires) != 1 {
		t.Fatalf("conformance.wiring (negative) emitted %d times, want 1", len(negWires))
	}
	if okv, _ := boolData(negWires[0], "ok"); okv {
		t.Errorf("conformance.wiring ok=true with a missing required layer, want false")
	}
	missing, _ := negWires[0].Data["missing"].([]string)
	foundMissing := false
	for _, m := range missing {
		if m == deadWire {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("conformance.wiring missing=%v does not name the dead-wired layer %q", missing, deadWire)
	}
}

// TestConformanceFlagOffIsByteIdentical pins the default-OFF contract: with conformance.self_check OFF the
// engine attaches no tap, records no coverage, and emits NO conformance.wiring — the live loop is unchanged.
func TestConformanceFlagOffIsByteIdentical(t *testing.T) {
	// default engine (flag OFF) over the same scenario.
	e, log := runScenarioLogged(t, "S5")
	if wires := log.of(events.ConformanceWiring); len(wires) != 0 {
		t.Errorf("conformance.wiring emitted %d times with the flag OFF, want 0 (default must be silent)", len(wires))
	}
	if cov, count := e.WiringCoverage(); cov != nil || count != 0 {
		t.Errorf("WiringCoverage() = (%v, %d) with the flag OFF, want (nil, 0)", cov, count)
	}
	// EmitWiringScan is a no-op (returns false, emits nothing) when the flag is off.
	if ok := e.EmitWiringScan([]string{"conscious"}); ok {
		t.Errorf("EmitWiringScan with the flag OFF returned true, want false (no-op)")
	}
	if wires := log.of(events.ConformanceWiring); len(wires) != 0 {
		t.Errorf("EmitWiringScan emitted a conformance.wiring event with the flag OFF, want none")
	}
}
