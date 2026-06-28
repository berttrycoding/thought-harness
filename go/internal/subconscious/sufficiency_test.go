package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// onGate / offGate build an ON / OFF seam.sufficiency_gate config gate for the tests.
func onGate() *config.Gate {
	on := true
	return config.NewGate("seam.sufficiency_gate", func() bool { return on }, nil)
}
func offGate() *config.Gate {
	off := false
	return config.NewGate("seam.sufficiency_gate", func() bool { return off }, nil)
}

// judgeBackend embeds the test double (so it satisfies backends.Backend) and ADDS a SufficiencyJudge
// ceiling — the test double itself does NOT implement SufficiencyJudge, so this is how we exercise the
// model-ceiling escalation path deterministically. verdict is what JudgeSufficiency returns; called
// records each call so a test can assert the ceiling was (or was NOT) consulted.
type judgeBackend struct {
	backends.Backend
	verdict string
	ok      bool
	calls   *[]judgeCall
}

type judgeCall struct{ query, fuel, floor string }

func (b judgeBackend) JudgeSufficiency(query, fuelText, floorVerdict string) (string, bool) {
	if b.calls != nil {
		*b.calls = append(*b.calls, judgeCall{query, fuelText, floorVerdict})
	}
	return b.verdict, b.ok
}

// TestSufficiencyGateAbstainsOnOffTopicRecall is the CRAG abstention-paradox guard at the cognition
// level: when the sourcing ladder returns fuel that does NOT cover the need (an off-topic recall), the
// harness ABSTAINS — the fuel-needing candidate is DROPPED before the seam rather than over-committed
// as a hollow grounded thought. This is the THINKING the spec intends, not just that the loop ran: a
// mechanical pipeline would voice the off-topic recall; the gate must refuse to.
func TestSufficiencyGateAbstainsOnOffTopicRecall(t *testing.T) {
	// the knowledge rung returns a high-trust GROUNDED hit that shares NO content words with the need
	// (an off-topic but confidently-sourced recall — exactly the abstention-paradox trap).
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{
		{Statement: "the recipe needs flour butter sugar and three large eggs", Kind: "fact", Grounded: true, Trust: 0.9},
	}}
	p := NewSourcingPolicy(kn, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	suff := NewSufficiencyGate("control", backends.NewTest(), onGate(), nil)

	c := genCand("capital city of france paris location")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, suff, nil, nil)

	if len(out) != 0 {
		t.Fatalf("off-topic recall must be ABSTAINED (dropped), got %d candidate(s): %+v", len(out), out)
	}
}

// TestSufficiencyGatePassesCoveringRecall is the other half: when the recall actually COVERS the need,
// the gate lets it through (it does NOT over-abstain — abstention must be earned by under-coverage, not
// reflexive). Proves the gate discriminates rather than blanket-dropping.
func TestSufficiencyGatePassesCoveringRecall(t *testing.T) {
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{
		{Statement: "the capital city of france is paris located on the seine", Kind: "fact", Grounded: true, Trust: 0.9},
	}}
	p := NewSourcingPolicy(kn, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	suff := NewSufficiencyGate("control", backends.NewTest(), onGate(), nil)

	c := genCand("capital city of france paris location")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, suff, nil, nil)

	if len(out) != 1 {
		t.Fatalf("a covering recall must pass the gate (not over-abstain), got %d", len(out))
	}
}

// TestSufficiencyGateOffIsPassThrough: with the toggle OFF (the default), the gate is a no-op — even an
// off-topic recall passes through (byte-identical to the pre-A-RAG1 behaviour). This pins the additive,
// default-OFF contract: the gate only ever subtracts a candidate when explicitly enabled.
func TestSufficiencyGateOffIsPassThrough(t *testing.T) {
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{
		{Statement: "an entirely unrelated off-topic recall", Kind: "fact", Grounded: true, Trust: 0.9},
	}}
	p := NewSourcingPolicy(kn, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	suff := NewSufficiencyGate("control", backends.NewTest(), offGate(), nil)

	c := genCand("capital city of france paris location")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, suff, nil, nil)

	if len(out) != 1 {
		t.Fatalf("OFF gate must pass every recall through (byte-identical), got %d", len(out))
	}
	// and a NIL gate (never wired) is the same as OFF.
	c2 := genCand("capital city of france paris location")
	if got := Concretize([]*types.Candidate{c2}, nil, p, alwaysFuelNeeding, identityFuse, true, true, nil, nil, nil); len(got) != 1 {
		t.Fatalf("nil gate must pass through, got %d", len(got))
	}
}

// TestSufficiencyGateUngroundedNeverSufficient is the laundered-hallucination guard at the gate level:
// a covering-but-UNGROUNDED recall (the GENERATED rung) is capped at AMBIGUOUS by the floor; in control
// mode (no escalation) an ambiguous floor verdict is NOT insufficient, so the candidate is KEPT (the
// Filter then prices it at the 0.42 GENERATED floor downstream — abstention is for off-topic fuel, not
// for low-trust-but-on-topic fuel, which the existing provenance machinery already distrusts).
func TestSufficiencyGateUngroundedNeverSufficient(t *testing.T) {
	// no grounded rung is sourceable -> the ladder bottoms out at the GENERATED rung with covering text.
	p := NewSourcingPolicy(&fakeKnowledge{}, fakeMemory{}, fakeReality{},
		fakeGen{text: "the capital city of france is paris located on the seine"}, allReprSources(), nil, nil)
	suff := NewSufficiencyGate("control", backends.NewTest(), onGate(), nil)

	c := genCand("capital city of france paris location")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, suff, nil, nil)

	// the covering generated fuel is AMBIGUOUS (not insufficient) -> kept, but still stamped GENERATED.
	if len(out) != 1 {
		t.Fatalf("covering ungrounded fuel is ambiguous (kept, distrusted downstream), got %d", len(out))
	}
	if out[0].Source.String() != "GENERATED" {
		t.Fatalf("ungrounded fuel must stay GENERATED for the Filter to distrust, got %v", out[0].Source)
	}
}

// TestSufficiencyGateEscalatesOnlyFlaggedFuzzy is the Pattern-C contract: in HYBRID mode the model
// ceiling is consulted ONLY on a flagged-fuzzy floor verdict (an AMBIGUOUS grading), and a model verdict
// of "insufficient" then drives the abstain. A clear-covering recall (structural sufficient) must NOT
// consult the model (the floor is authoritative). This is the abstain-vs-over-commit decision spine.
func TestSufficiencyGateEscalatesOnlyFlaggedFuzzy(t *testing.T) {
	// CASE 1: a borderline (ambiguous-floor) recall -> the model IS consulted and says insufficient ->
	// the harness abstains on the model's judgment.
	var calls []judgeCall
	jb := judgeBackend{Backend: backends.NewTest(), verdict: "insufficient", ok: true, calls: &calls}
	// a partial-overlap recall lands the floor in the ambiguous band (some shared words, mid trust).
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{
		{Statement: "paris is a large european city with many museums", Kind: "fact", Grounded: true, Trust: 0.55},
	}}
	p := NewSourcingPolicy(kn, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	suff := NewSufficiencyGate("hybrid", jb, onGate(), nil)

	c := genCand("capital city of france paris location population")
	out := Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, suff, nil, nil)

	floorV := control.ScoreSufficiency(c.Text, kn.hits[0].Statement, 0.55, true)
	if floorV.Verdict != control.SuffAmbiguous {
		t.Fatalf("test fixture not ambiguous on the floor (got %v) — adjust the fixture", floorV.Verdict)
	}
	if len(calls) != 1 {
		t.Fatalf("a flagged-fuzzy (ambiguous) floor verdict must consult the model ONCE, got %d calls", len(calls))
	}
	if calls[0].floor != "ambiguous" {
		t.Fatalf("the model must be told the floor verdict it is refining, got %q", calls[0].floor)
	}
	if len(out) != 0 {
		t.Fatalf("model said insufficient -> harness must ABSTAIN, got %d kept", len(out))
	}

	// CASE 2: a clearly-covering recall is STRUCTURAL-sufficient -> the model must NOT be consulted.
	var calls2 []judgeCall
	jb2 := judgeBackend{Backend: backends.NewTest(), verdict: "insufficient", ok: true, calls: &calls2}
	kn2 := &fakeKnowledge{hits: []knowledge.Knowledge{
		{Statement: "the capital city of france is paris located on the seine", Kind: "fact", Grounded: true, Trust: 0.92},
	}}
	p2 := NewSourcingPolicy(kn2, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, nil)
	suff2 := NewSufficiencyGate("hybrid", jb2, onGate(), nil)
	c2 := genCand("capital city of france paris location")
	out2 := Concretize([]*types.Candidate{c2}, nil, p2, alwaysFuelNeeding, identityFuse, true, true, suff2, nil, nil)
	if len(calls2) != 0 {
		t.Fatalf("a clear-sufficient floor verdict must NOT escalate to the model, got %d calls", len(calls2))
	}
	if len(out2) != 1 {
		t.Fatalf("clear-sufficient recall must pass, got %d", len(out2))
	}
}

// TestSufficiencyGateEmitsEvent asserts the gate is OBSERVABLE — a grading emits seam.sufficiency with
// the verdict + abstain flag (the observability contract; an invisible gate is untestable in the TUI).
func TestSufficiencyGateEmitsEvent(t *testing.T) {
	var seen []events.Event
	emit := func(kind, summary string, data map[string]any) events.Event {
		e := events.Event{Kind: kind, Summary: summary, Data: data}
		seen = append(seen, e)
		return e
	}
	kn := &fakeKnowledge{hits: []knowledge.Knowledge{
		{Statement: "an entirely unrelated off-topic recall about cooking", Kind: "fact", Grounded: true, Trust: 0.9},
	}}
	p := NewSourcingPolicy(kn, fakeMemory{}, fakeReality{}, fakeGen{}, allReprSources(), nil, emit)
	suff := NewSufficiencyGate("control", backends.NewTest(), onGate(), emit)

	c := genCand("capital city of france paris location")
	Concretize([]*types.Candidate{c}, nil, p, alwaysFuelNeeding, identityFuse, true, true, suff, nil, emit)

	var found *events.Event
	for i := range seen {
		if seen[i].Kind == string(events.Sufficiency) {
			found = &seen[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no seam.sufficiency event emitted (the gate must be observable)")
	}
	if found.Data["verdict"] != "insufficient" {
		t.Fatalf("event verdict=%v, want insufficient", found.Data["verdict"])
	}
	if found.Data["abstained"] != true {
		t.Fatalf("event abstained=%v, want true", found.Data["abstained"])
	}
}
