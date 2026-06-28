package plangate

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// captureBus collects every emitted Event so a test can assert the wire-contract (the plangate.* kinds
// fire) AND the falsifiability decision (verdict pass/fail, named misses). It is the faithful stand-in
// for subscribing a sink to the live bus.
func captureBus() (*events.Bus, *[]events.Event) {
	bus := events.New(256)
	var log []events.Event
	bus.Subscribe(func(e events.Event) { log = append(log, e) })
	return bus, &log
}

func kinds(log []events.Event, k string) []events.Event {
	var out []events.Event
	for _, e := range log {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// TestPlanGatePassesWhenSymbolLands is the POSITIVE half of the gate's thinking: a declared producer
// symbol that ACTUALLY appears in its file's added lines + a matching acceptance regex => PASS.
func TestPlanGatePassesWhenSymbolLands(t *testing.T) {
	bus, log := captureBus()
	c := Contract{
		Plan: "O-2-demo",
		Producers: []Producer{
			{File: "internal/plangate/plangate.go", Symbols: []string{"func (g *Gate) Audit", "PlanGateVerdict"}},
		},
		Checks: []string{`Audit\(c Contract`},
	}
	diff := Diff{
		"internal/plangate/plangate.go": "func (g *Gate) Audit(c Contract, d Diff) Verdict {\n\temit(events.PlanGateVerdict, ...)\n",
	}
	v := New(bus.Emit).Audit(c, diff)
	if !v.Pass {
		t.Fatalf("expected PASS when every declared symbol/regex landed; missing=%v", v.Missing)
	}
	if v.Producers != 2 || v.Checks != 1 {
		t.Fatalf("audited counts wrong: producers=%d checks=%d", v.Producers, v.Checks)
	}
	// the verdict event must fire and report pass=true (the wire contract).
	verdicts := kinds(*log, events.PlanGateVerdict)
	if len(verdicts) != 1 {
		t.Fatalf("expected exactly one plangate.verdict, got %d", len(verdicts))
	}
	if verdicts[0].Data["pass"] != true {
		t.Fatalf("plangate.verdict should carry pass=true on a clean audit, got %v", verdicts[0].Data["pass"])
	}
}

// TestPlanGateRefusesWhenSymbolMissing is the CORE COGNITION the spec intends: the falsifiable refusal.
// O-2 exists to kill "declared-not-landed" — the plan SAYS it will land a symbol, the diff does NOT
// contain it, so the gate must REFUSE the keep (Pass=false) and NAME the precise miss. A gate that
// passed here would be the exact failure the project hit repeatedly (the wiring-gate lesson).
func TestPlanGateRefusesWhenSymbolMissing(t *testing.T) {
	bus, log := captureBus()
	c := Contract{
		Plan: "declared-not-landed",
		Producers: []Producer{
			{File: "internal/engine/loop.go", Symbols: []string{"PlanGateAudit"}}, // the plan SAYS this wires in...
		},
	}
	diff := Diff{
		// the file was touched, but the declared wiring symbol never landed (a real declared-not-landed diff:
		// it must not appear anywhere in the added lines — not even in a comment, since the gate is a
		// grep-style substring audit by design).
		"internal/engine/loop.go": "// touched a comment, but the wiring was never added\nfunc tick() {}\n",
	}
	v := New(bus.Emit).Audit(c, diff)
	if v.Pass {
		t.Fatal("FALSIFIABILITY BROKEN: the gate PASSED a plan whose declared symbol is absent from the diff " +
			"(this is exactly the declared-not-landed cheat O-2 exists to refuse)")
	}
	if len(v.Missing) != 1 || v.Missing[0].Symbol != "PlanGateAudit" {
		t.Fatalf("the refusal must name the precise missing symbol; got missing=%+v", v.Missing)
	}
	if v.Missing[0].Reason != "symbol absent from added lines" {
		t.Fatalf("the miss reason must be falsifiable evidence, got %q", v.Missing[0].Reason)
	}
	// the verdict event must fire with pass=false and the named miss (so the keep step is observable).
	verdicts := kinds(*log, events.PlanGateVerdict)
	if len(verdicts) != 1 || verdicts[0].Data["pass"] != false {
		t.Fatalf("expected one plangate.verdict with pass=false, got %+v", verdicts)
	}
	missing, _ := verdicts[0].Data["missing"].([]string)
	if len(missing) != 1 {
		t.Fatalf("plangate.verdict must carry the missing list, got %v", verdicts[0].Data["missing"])
	}
}

// TestPlanGateRefusesWhenFileNotTouched: a producer whose FILE is not in the diff at all is a distinct,
// equally-falsifiable miss — the change claimed to wire a call site it never touched.
func TestPlanGateRefusesWhenFileNotTouched(t *testing.T) {
	c := Contract{
		Plan:      "untouched-file",
		Producers: []Producer{{File: "internal/engine/loop.go", Symbols: []string{"gate.Audit"}}},
	}
	diff := Diff{"internal/plangate/plangate.go": "func New(...) {}\n"} // a DIFFERENT file changed
	v := New(nil).Audit(c, diff)
	if v.Pass {
		t.Fatal("gate must refuse when the declared producer file is not in the diff")
	}
	if v.Missing[0].Reason != "file not in diff" {
		t.Fatalf("the miss must distinguish an untouched file, got %q", v.Missing[0].Reason)
	}
}

// TestPlanGateRefusesOnUnmatchedAcceptanceCheck: the symbol can land yet an acceptance regex still fail
// (the wiring landed but the BEHAVIOUR contract — e.g. "the event is emitted at the call site" — did
// not). The gate must refuse on any unmatched check.
func TestPlanGateRefusesOnUnmatchedAcceptanceCheck(t *testing.T) {
	c := Contract{
		Plan:      "behaviour-gap",
		Producers: []Producer{{File: "a.go", Symbols: []string{"WireGate"}}},
		Checks:    []string{`emit\(events\.PlanGateVerdict`}, // the wiring exists but never emits the verdict
	}
	diff := Diff{"a.go": "func WireGate() { /* but no emit */ }\n"}
	v := New(nil).Audit(c, diff)
	if v.Pass {
		t.Fatal("gate must refuse when an acceptance regex matches no added line, even if the symbol landed")
	}
	if len(v.Missing) != 1 || v.Missing[0].Kind != "check" {
		t.Fatalf("the miss must be the unmatched check, got %+v", v.Missing)
	}
}

// TestPlanGateInvalidRegexHardFails: a malformed acceptance regex must HARD FAIL the check, never
// silently pass — a broken gate that admits everything is worse than no gate.
func TestPlanGateInvalidRegexHardFails(t *testing.T) {
	c := Contract{Plan: "bad-regex", Checks: []string{`([unterminated`}}
	v := New(nil).Audit(c, Diff{"a.go": "anything\n"})
	if v.Pass {
		t.Fatal("an invalid acceptance regex must hard-fail the check, not silently pass")
	}
}

// TestEmptyContractPasses: a build with NO declared producers/checks audits clean (the gate only ever
// REFUSES on a declared-but-absent symbol — it never invents a requirement). This keeps the gate
// opt-in per slice without becoming a blanket blocker.
func TestEmptyContractPasses(t *testing.T) {
	bus, log := captureBus()
	v := New(bus.Emit).Audit(Contract{Plan: "noop"}, Diff{"a.go": "x\n"})
	if !v.Pass {
		t.Fatal("an empty contract must pass (no declared requirement to falsify)")
	}
	// even the empty audit is OBSERVABLE — the verdict event still fires (legibility, not silence).
	if len(kinds(*log, events.PlanGateVerdict)) != 1 {
		t.Fatal("an empty-contract audit must still emit a plangate.verdict (observability)")
	}
}

// TestNilEmitIsNoOp: a nil emit closure (the OFF / offline path) must audit identically and emit
// nothing — the byte-identical default-OFF contract holds at the package level too.
func TestNilEmitIsNoOp(t *testing.T) {
	c := Contract{Plan: "p", Producers: []Producer{{File: "a.go", Symbols: []string{"X"}}}}
	v := New(nil).Audit(c, Diff{"a.go": "X landed\n"})
	if !v.Pass {
		t.Fatal("nil-emit audit must still compute the verdict")
	}
}
