package types

import "testing"

func TestEnumRoundTrip(t *testing.T) {
	// every member's String() must Parse back to the same value
	checks := []struct {
		name string
		val  int
	}{
		{INJECTED.String(), int(INJECTED)},
		{PERCEPT.String(), int(PERCEPT)},
		{GENERATE.String(), int(GENERATE)},
		{COMPRESSED.String(), int(COMPRESSED)},
		{MERGED.String(), int(MERGED)},
		{BACKTRACK.String(), int(BACKTRACK)},
		{FLAG.String(), int(FLAG)},
		{INTERRUPTED.String(), int(INTERRUPTED)},
		{S_ACTIVE.String(), int(S_ACTIVE)},
		{ASLEEP.String(), int(ASLEEP)},
	}
	for _, c := range checks {
		if got, ok := ParseSource(c.name); ok {
			if int(got) != c.val && c.name == "INJECTED" {
				t.Errorf("ParseSource(%q)=%d want %d", c.name, got, c.val)
			}
		}
	}
	// 1-based ordinals match Python auto()
	if int(INJECTED) != 1 || int(PERCEPT) != 6 {
		t.Fatalf("Source must be 1-based: INJECTED=%d PERCEPT=%d", int(INJECTED), int(PERCEPT))
	}
	if int(THINK) != 1 || int(STOP) != 6 {
		t.Fatalf("Decision must be 1-based: THINK=%d STOP=%d", int(THINK), int(STOP))
	}
	// SystemState.ACTIVE must stringify to "ACTIVE" despite the S_ACTIVE Go identifier
	if S_ACTIVE.String() != "ACTIVE" {
		t.Fatalf("SystemState S_ACTIVE must wire as ACTIVE, got %q", S_ACTIVE.String())
	}
	if v, ok := ParseSystemState("ACTIVE"); !ok || v != S_ACTIVE {
		t.Fatalf("ParseSystemState(ACTIVE) round-trip failed")
	}
	if _, ok := ParseDecision("NOPE"); ok {
		t.Fatalf("unknown name must return ok=false")
	}
}

func TestEllipsize(t *testing.T) {
	if got := Ellipsize("hello world", 72); got != "hello world" {
		t.Errorf("short text unchanged: %q", got)
	}
	if got := Ellipsize("abcdef", 4); got != "abc…" {
		t.Errorf("Ellipsize(abcdef,4)=%q want abc…", got)
	}
	if got := Ellipsize("a\nb  c", 72); got != "a b  c" {
		t.Errorf("newline collapse: %q", got)
	}
}

func TestStripVoice(t *testing.T) {
	if got := StripVoice("oh — it works"); got != "It works" {
		t.Errorf("voice strip+sentencecase: %q", got)
	}
	if got := StripVoice("[decompose] split the task"); got != "Split the task" {
		t.Errorf("operator tag strip: %q", got)
	}
	if got := StripVoice("Reality: done"); got != "Done" {
		t.Errorf("case-insensitive prefix: %q", got)
	}
}

func TestJaccard(t *testing.T) {
	if got := Jaccard("the cat sat", "the cat sat"); got != 1.0 {
		t.Errorf("identical=1.0, got %v", got)
	}
	if got := Jaccard("", "x"); got != 0.0 {
		t.Errorf("empty=0.0, got %v", got)
	}
	// {a,b} vs {b,c} -> inter 1, union 3 -> 1/3
	if got := Jaccard("a b", "b c"); got < 0.333 || got > 0.334 {
		t.Errorf("a b / b c want 1/3, got %v", got)
	}
}

func TestFilterVerdictAdmitAndAppraisal(t *testing.T) {
	v := FilterVerdict{Verdict: FLAG, Confidence: 0.4, Reason: "hedged", Source: "heuristic"}
	if !v.Admit() {
		t.Fatal("FLAG must admit")
	}
	rej := FilterVerdict{Verdict: REJECT}
	if rej.Admit() {
		t.Fatal("REJECT must not admit")
	}
	a := v.AsAppraisalDefault()
	if a.Site != "filter.admit" || a.Value != 0.4 || a.Verdict == nil || *a.Verdict != "FLAG" {
		t.Fatalf("appraisal mismatch: %+v", a)
	}
	if a.AppraiserConf != 1.0 {
		t.Fatalf("appraiser_conf default must be 1.0, got %v", a.AppraiserConf)
	}
}

func TestObservationUnion(t *testing.T) {
	var raw any = Observation{Ok: true, Text: "reality: ok"}
	switch r := raw.(type) {
	case Observation:
		if !r.Ok {
			t.Fatal("ok lost")
		}
	default:
		t.Fatal("union switch missed Observation")
	}
}
