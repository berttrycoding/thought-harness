package types

import "testing"

// TestAllNineEnumsRoundTrip is the Tier-0 enum gate: every member of all NINE enums must
// round-trip name<->value (String() then Parse, and Parse then String()). This is
// load-bearing — the Controller's _llm_decide does Decision[choice], and appraisal/cogngraph
// re-parse Verdict.name / Source.name strings out of Event.data, so a broken table silently
// corrupts the wire.
//
// Each sub-table lists EVERY member of its enum. A bare String() on a value gives the wire
// name; Parse on that name must return the exact same value (and ok=true); the reverse
// (name -> value -> name) must also be stable.
func TestAllNineEnumsRoundTrip(t *testing.T) {
	// roundTrip[E] asserts name == String(v) and Parse(name) == (v, true) for one member.
	check := func(name string, v int, got int, ok bool, enum string) {
		t.Helper()
		if !ok {
			t.Errorf("%s: Parse(%q) returned ok=false", enum, name)
			return
		}
		if got != v {
			t.Errorf("%s: Parse(%q) = %d, want %d (String()->Parse round-trip broken)", enum, name, got, v)
		}
	}

	// 1) Source — 6 members, 1-based.
	for _, v := range []Source{INJECTED, GENERATED, OBSERVATION, USER_INPUT, METACOG, PERCEPT} {
		name := v.String()
		got, ok := ParseSource(name)
		check(name, int(v), int(got), ok, "Source")
	}

	// 2) Operator — 7 members.
	for _, v := range []Operator{DECOMPOSE, VALIDATE, COMPARE, GENERALIZE, ABSTRACT, SIMULATE, GENERATE} {
		name := v.String()
		got, ok := ParseOperator(name)
		check(name, int(v), int(got), ok, "Operator")
	}

	// 3) Resolution — 2 members.
	for _, v := range []Resolution{EXPANDED, COMPRESSED} {
		name := v.String()
		got, ok := ParseResolution(name)
		check(name, int(v), int(got), ok, "Resolution")
	}

	// 4) Status — 4 members.
	for _, v := range []Status{ACTIVE, STASHED, DEAD, MERGED} {
		name := v.String()
		got, ok := ParseStatus(name)
		check(name, int(v), int(got), ok, "Status")
	}

	// 5) Decision — 6 members.
	for _, v := range []Decision{THINK, BRANCH, MERGE, BACKTRACK, ACT, STOP} {
		name := v.String()
		got, ok := ParseDecision(name)
		check(name, int(v), int(got), ok, "Decision")
	}

	// 6) Verdict — 3 members.
	for _, v := range []Verdict{ADMIT, REJECT, FLAG} {
		name := v.String()
		got, ok := ParseVerdict(name)
		check(name, int(v), int(got), ok, "Verdict")
	}

	// 7) StopKind — 5 members.
	for _, v := range []StopKind{GOAL_MET, GIVE_UP, BLOCKED_REALITY, BLOCKED_USER, INTERRUPTED} {
		name := v.String()
		got, ok := ParseStopKind(name)
		check(name, int(v), int(got), ok, "StopKind")
	}

	// 8) SystemState — 6 members. Note S_ACTIVE wires as "ACTIVE" (the Go identifier is
	//    prefixed only to dodge the Status.ACTIVE collision); the round-trip string is "ACTIVE".
	for _, v := range []SystemState{IDLE, S_ACTIVE, AWAITING_REALITY, AWAITING_USER, SUSPENDED, DONE} {
		name := v.String()
		got, ok := ParseSystemState(name)
		check(name, int(v), int(got), ok, "SystemState")
	}
	if S_ACTIVE.String() != "ACTIVE" {
		t.Errorf("SystemState S_ACTIVE must wire as %q, got %q", "ACTIVE", S_ACTIVE.String())
	}

	// 9) Arousal — 3 members.
	for _, v := range []Arousal{AWAKE, DROWSY, ASLEEP} {
		name := v.String()
		got, ok := ParseArousal(name)
		check(name, int(v), int(got), ok, "Arousal")
	}

	// Negative: an unknown name returns ok=false for every enum (the Go form of Python's
	// Enum[name] KeyError that the round-trip relies on to fall back, not crash).
	if _, ok := ParseSource("NOPE"); ok {
		t.Error("ParseSource(NOPE) should return ok=false")
	}
	if _, ok := ParseOperator("NOPE"); ok {
		t.Error("ParseOperator(NOPE) should return ok=false")
	}
	if _, ok := ParseResolution("NOPE"); ok {
		t.Error("ParseResolution(NOPE) should return ok=false")
	}
	if _, ok := ParseStatus("NOPE"); ok {
		t.Error("ParseStatus(NOPE) should return ok=false")
	}
	if _, ok := ParseDecision("NOPE"); ok {
		t.Error("ParseDecision(NOPE) should return ok=false")
	}
	if _, ok := ParseVerdict("NOPE"); ok {
		t.Error("ParseVerdict(NOPE) should return ok=false")
	}
	if _, ok := ParseStopKind("NOPE"); ok {
		t.Error("ParseStopKind(NOPE) should return ok=false")
	}
	if _, ok := ParseSystemState("NOPE"); ok {
		t.Error("ParseSystemState(NOPE) should return ok=false")
	}
	if _, ok := ParseArousal("NOPE"); ok {
		t.Error("ParseArousal(NOPE) should return ok=false")
	}
}
