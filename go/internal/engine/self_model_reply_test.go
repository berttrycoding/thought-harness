package engine

// self_model_reply_test.go — the OFFLINE proof that the grounded self-model REACHES the RESPOND context
// (board SELF-MODEL; the DELIVER-time fix for the live gap TestLiveClaudeSelfModelGroundsWhatItIs).
//
// THE GAP these pin. The standing-core self-model was grounded INTO the stream (perception.self_model) but
// the RESPOND path ignored it — so "what are you / what can you do / where are you running" answered from
// the bare-model "I'm an LLM" prior. selfModelReplyContext folds the grounded self-model + a targeted
// SelfModelLookup into the RESPOND context when the question is self-directed (the relevance gate). These
// tests prove DETERMINISTICALLY (offline, no live model) that:
//   - the harness IDENTITY + its real CAPABILITIES (tools/specialists/operators) + runtime grounding reach
//     the context the responder consumes when sense.self_model is ON and the goal is an identity question;
//   - they are ABSENT (the context is byte-identical) when the flag is OFF;
//   - they are ABSENT on a NON-self-directed question even with the flag ON (the relevance gate — a normal
//     answer is never bloated with a self-description);
//   - driving the FULL loop on a recording backend, the captured RESPOND context carries the self-model on
//     an identity question (the end-to-end wire — the context actually reaches Respond).
//
// This is exactly the CONTENT path the test double MASKS (its canned Respond answers from a hardcoded line),
// so we assert on the CONTEXT the model receives — backend-independent — not the answer string. The live
// claude DoD (TestLiveClaudeSelfModelGroundsWhatItIs) proves the model then ANSWERS from it.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// selfModelReplyFeatures builds the awake stack that gives the introspective root focus turns and turns the
// self-model on (mirrors self_model_property_test.go's selfModelAwakeFeatures, here in package engine).
func selfModelReplyFeatures(on bool) *config.HarnessConfig {
	feat := config.New()
	a := &feat.Conscious.Activity
	a.SeedIntents = true
	a.SeedIntentCount = cognition.SeedPortfolioSize()
	a.FacultyScheduler = true
	feat.Sense.SelfModel = on
	feat.Validate()
	return feat
}

func newSelfModelReplyEngine(t *testing.T, on bool) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Workspace = "." // a real workspace so the self-model reads real tools + a genuine cwd
	cfg.Features = selfModelReplyFeatures(on)
	e, err := NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

// selfModelText markers the grounded self-knowledge MUST carry: the harness identity (not the base model)
// and the capability index (read from the real registries).
func assertSelfModelInContext(t *testing.T, ctx []types.Thought) {
	t.Helper()
	joined := ""
	for _, c := range ctx {
		joined += c.Text + "\n"
	}
	low := strings.ToLower(joined)
	if !strings.Contains(joined, "(self-model)") {
		t.Fatalf("the RESPOND context does not carry the self-model unit; ctx=%q", joined)
	}
	// IDENTITY — the harness, not the bare model.
	if !strings.Contains(joined, "Silent-Injection") || !strings.Contains(joined, "three layers") {
		t.Fatalf("the RESPOND context carries no harness IDENTITY/architecture; ctx=%q", joined)
	}
	// CAPABILITIES — read from the real registries (specialists + operators are always present).
	if !strings.Contains(low, "specialists") || !strings.Contains(low, "operators") {
		t.Fatalf("the RESPOND context carries no real CAPABILITY index; ctx=%q", joined)
	}
}

func assertNoSelfModelInContext(t *testing.T, ctx []types.Thought) {
	t.Helper()
	for _, c := range ctx {
		if strings.Contains(c.Text, "(self-model)") {
			t.Fatalf("the RESPOND context unexpectedly carries the self-model unit: %q", c.Text)
		}
	}
}

// TestSelfModelReplyOnFoldsIntoRespondContextForIdentityQuestion is the core proof: flag ON + a self-
// directed identity question ⇒ the grounded self-model (identity + real capabilities) reaches the context
// the responder consumes.
func TestSelfModelReplyOnFoldsIntoRespondContextForIdentityQuestion(t *testing.T) {
	e := newSelfModelReplyEngine(t, true)
	e.Step() // open the boot episode so the registries are live

	for _, q := range []string{
		"what are you, what can you do, and where are you running?",
		"who are you?",
		"what can you do?",
		"where are you running?",
		"what tools do you have?",
		"describe yourself",
		"tell me about yourself",
	} {
		e.graph.Goal = q
		ctx := e.selfModelReplyContext(e.workingContext())
		assertSelfModelInContext(t, ctx)
	}
}

// TestSelfModelReplyOffIsByteIdentical pins the DEFAULT-OFF contract: with the flag OFF, the RESPOND
// context is returned UNCHANGED — even on an identity question.
func TestSelfModelReplyOffIsByteIdentical(t *testing.T) {
	e := newSelfModelReplyEngine(t, false)
	e.Step()
	e.graph.Goal = "what are you, what can you do, and where are you running?"
	raw := e.workingContext()
	ctx := e.selfModelReplyContext(raw)
	assertNoSelfModelInContext(t, ctx)
	// Byte-identical: same length and same units (no fold, no transform).
	if len(ctx) != len(raw) {
		t.Fatalf("flag OFF must return the context UNCHANGED; len %d -> %d", len(raw), len(ctx))
	}
	for i := range raw {
		if ctx[i].Text != raw[i].Text || ctx[i].ID != raw[i].ID || ctx[i].Source != raw[i].Source {
			t.Fatalf("flag OFF must return the context byte-identical; unit %d changed", i)
		}
	}
}

// TestSelfModelReplyRelevanceGateDoesNotBloatNormalAnswers pins the relevance gate: flag ON but a NON-self-
// directed question ⇒ the context is unchanged (a normal answer is never bloated with a self-description).
func TestSelfModelReplyRelevanceGateDoesNotBloatNormalAnswers(t *testing.T) {
	e := newSelfModelReplyEngine(t, true)
	e.Step()
	for _, q := range []string{
		"what is 2 + 2?",
		"summarize the config file",
		"what is the capital of France?",
		"refactor this function for me",
		"what are the risks of this migration?", // "what are" but no self-reference -> not self-directed
	} {
		e.graph.Goal = q
		ctx := e.selfModelReplyContext(e.workingContext())
		assertNoSelfModelInContext(t, ctx)
	}
}

// respondCtxRecorder wraps the test double and CAPTURES the ctx passed to Respond — the end-to-end proof
// that selfModelReplyContext's fold actually reaches the backend's Respond call on the live loop.
type respondCtxRecorder struct {
	*backends.TestBackend
	respondCtx [][]types.Thought
}

func (r *respondCtxRecorder) Respond(goal string, ctx []types.Thought) string {
	cp := make([]types.Thought, len(ctx))
	copy(cp, ctx)
	r.respondCtx = append(r.respondCtx, cp)
	return r.TestBackend.Respond(goal, ctx)
}

var _ backends.Backend = (*respondCtxRecorder)(nil)

// kindCounter is a tiny bus sink that tallies events by kind (eventLog lives in package engine_test; this
// test is package engine so it can reach the unexported wire). countOf returns the tally for a kind.
type kindCounter struct{ counts map[string]int }

func newKindCounter(e *Engine) *kindCounter {
	kc := &kindCounter{counts: map[string]int{}}
	e.Bus().Subscribe(func(ev events.Event) { kc.counts[ev.Kind]++ })
	return kc
}

func (kc *kindCounter) countOf(kind string) int { return kc.counts[kind] }

// TestSelfModelReplyReachesRespondOnFullLoop is the WIRE proof: driving the FULL continuous loop with a
// recording backend, an identity question submitted mid-wander produces a Respond call whose context
// carries the grounded self-model. (The context the model would answer FROM — the thing the live DoD then
// confirms it answers from.)
func TestSelfModelReplyReachesRespondOnFullLoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Workspace = "."
	cfg.Features = selfModelReplyFeatures(true)
	rec := &respondCtxRecorder{TestBackend: backends.NewTest()}
	e, err := NewEngine(&cfg, rec)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	kc := newKindCounter(e)

	// Let the awake mind settle and ground its standing self-model first (the field scenario).
	for i := 0; i < 25 && kc.countOf(events.PerceptionSelfModel) == 0; i++ {
		e.Step()
	}

	// Ask what it is mid-wander, then run until it answers.
	e.SubmitDefault("what are you, what can you do, and where are you running?")
	answered := false
	for i := 0; i < 15 && !answered; i++ {
		e.Step()
		if kc.countOf(events.Respond) > 0 {
			answered = true
		}
	}
	if !answered {
		t.Fatal("the awake mind never reached a Respond for the identity question in 15 ticks")
	}

	// At least one Respond call's context must carry the grounded self-model.
	sawSelfModel := false
	for _, ctx := range rec.respondCtx {
		for _, c := range ctx {
			if strings.Contains(c.Text, "(self-model)") {
				sawSelfModel = true
			}
		}
	}
	if !sawSelfModel {
		t.Fatal("the identity question reached Respond but NO Respond context carried the grounded self-model — the fold did not reach the reply (the live gap)")
	}

	// The fold must be observable on the bus (never silent).
	if n := kc.countOf(events.PerceptionSelfModelReply); n == 0 {
		t.Fatal("the self-model was folded into the reply but emitted no perception.self_model_reply event (the fold must be observable)")
	}
}

// TestSelfModelReplyOffNeverFoldsOnFullLoop is the OFF twin: the full loop with the flag OFF never folds the
// self-model into a Respond context, even on an identity question (byte-identical reply path).
func TestSelfModelReplyOffNeverFoldsOnFullLoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Workspace = "."
	cfg.Features = selfModelReplyFeatures(false)
	rec := &respondCtxRecorder{TestBackend: backends.NewTest()}
	e, err := NewEngine(&cfg, rec)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	kc := newKindCounter(e)

	for i := 0; i < 10; i++ {
		e.Step()
	}
	e.SubmitDefault("what are you, what can you do, and where are you running?")
	for i := 0; i < 15 && kc.countOf(events.Respond) == 0; i++ {
		e.Step()
	}
	for _, ctx := range rec.respondCtx {
		for _, c := range ctx {
			if strings.Contains(c.Text, "(self-model)") {
				t.Fatal("flag OFF: a self-model unit leaked into a Respond context (not byte-identical)")
			}
		}
	}
	if n := kc.countOf(events.PerceptionSelfModelReply); n != 0 {
		t.Fatalf("flag OFF must emit NO perception.self_model_reply event; got %d", n)
	}
}
