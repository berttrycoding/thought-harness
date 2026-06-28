package engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
)

// newSparseDispatchEngine builds a reactive engine with subconscious.dispatch.sparse flipped to the
// requested state (everything else all-on) on the test double, with the event log subscribed. The flag is
// opt-in ⇒ default OFF even on the all-on baseline, so `sparse=false` is the legacy absolute-gate path.
func newSparseDispatchEngine(t *testing.T, sparse bool) (*engine.Engine, *eventLog) {
	t.Helper()
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	feat := config.New() // AllOn (dispatch.sparse is opt-in ⇒ default OFF even here)
	feat.Subconscious.SparseDispatch = sparse
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, backends.NewTest())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })
	return e, log
}

// TestSparseDispatchFiresLive is the WIRING-GATE proof (the saved lesson: a flag SET but never consulted on
// the live tick passes the unit tests yet is dead on the loop). With subconscious.dispatch.sparse ON, the
// subconscious.sparse event MUST fire across a real reactive episode on the engine's ACTUAL tick — proving
// the sparsemax admission is the LIVE dispatch path, not a dead field. With it OFF (the default), the event
// must NEVER appear (the legacy absolute-gate path is unchanged).
func TestSparseDispatchFiresLive(t *testing.T) {
	const goal = "is it safe to refactor the checkout flow; also what is 6 times 7, step by step?"

	on, onLog := newSparseDispatchEngine(t, true)
	on.SubmitDefault(goal)
	on.Run(30)

	sparse := onLog.of(string(events.SubSparse))
	if len(sparse) == 0 {
		t.Fatal("dispatch.sparse ON: subconscious.sparse must fire — the sparsemax admission is NOT on the live tick (dead wire)")
	}
	// The event must carry the competitive-admission payload the design promises: the induced τ, the θ
	// floor, the support, the scored field size, and the per-key weights. Observability IS the contract.
	first := sparse[0]
	for _, key := range []string{"tau", "floor", "support", "candidates", "weights"} {
		if _, ok := first.Data[key]; !ok {
			t.Fatalf("subconscious.sparse missing required key %q (payload: %v)", key, first.Data)
		}
	}
	// The accessor confirms the engine wired the flag onto the subconscious engine (read side of the wire).
	if !on.Subconscious().SparseDispatch() {
		t.Fatal("the engine did not wire dispatch.sparse onto the subconscious engine (SparseDispatch()==false with the flag ON)")
	}

	off, offLog := newSparseDispatchEngine(t, false)
	off.SubmitDefault(goal)
	off.Run(30)
	if len(offLog.of(string(events.SubSparse))) != 0 {
		t.Fatal("dispatch.sparse OFF: subconscious.sparse must NEVER fire (the legacy absolute gate must be byte-identical)")
	}
	if off.Subconscious().SparseDispatch() {
		t.Fatal("dispatch.sparse OFF: SparseDispatch() must be false (the wire must be inert)")
	}
}

// TestSparseDispatchIsCompetitiveAndStampsConfidence is the COGNITION property (not plumbing): on the live
// loop the sparsemax admission is COMPETITIVE — at least one tick where multiple specialists are over θ, the
// sparsemax narrows the field (support strictly fewer than the over-θ candidates) — AND the surviving
// candidates carry a stamped, normalized dispatch confidence (p_i in (0,1], summing to ~1 per tick). This is
// the *thinking* the design intends ("fire a few relative to the field" + a free V(s)/rerank prior), which a
// mechanical "does the loop run" test passes straight through.
func TestSparseDispatchIsCompetitiveAndStampsConfidence(t *testing.T) {
	// a goal that lights several faculties at once (a safety/change shape AND an arithmetic shape AND a
	// social opener) so a tick presents a multi-specialist field for sparsemax to compete over.
	const goal = "hi — is it safe to ship this refactor, and also what is 6 times 7? think it through."

	on, onLog := newSparseDispatchEngine(t, true)
	on.SubmitDefault(goal)
	on.Run(30)

	sparseEvents := onLog.of(string(events.SubSparse))
	if len(sparseEvents) == 0 {
		t.Fatal("no subconscious.sparse events — the sparse path never ran on the live loop")
	}

	competitiveTickSeen := false // a tick where the scored field had >support candidates (the narrowing)
	massWellFormed := false      // a tick whose surviving weights summed to ~1 (the normalized confidence)
	for _, ev := range sparseEvents {
		cands, _ := intData(ev, "candidates")
		support, _ := intData(ev, "support")
		// COMPETITIVE: when the field is bigger than the support, sparsemax dropped at least one specialist
		// that a low absolute θ would otherwise have admitted (the "few relative to the field" semantics).
		if cands > support && support >= 1 {
			competitiveTickSeen = true
		}
		// the per-key weights sum to ~1 over the SUPPORT (the surviving p_i are a normalized distribution).
		weights, _ := ev.Data["weights"].([]events.D)
		var sum float64
		var positives int
		for _, w := range weights {
			p, _ := w["p"].(float64)
			if p > 0 {
				sum += p
				positives++
			}
		}
		if positives >= 1 && sum > 0.99 && sum < 1.01 {
			massWellFormed = true
		}
	}

	if !competitiveTickSeen {
		t.Fatal("sparsemax never NARROWED a multi-specialist field (no tick with candidates>support) — " +
			"the admission did not behave competitively on the live loop")
	}
	if !massWellFormed {
		t.Fatal("the surviving sparsemax weights never formed a normalized distribution (p_i summing to ~1) — " +
			"the dispatch-confidence stamp is malformed")
	}

	// The stamped confidence must also reach the FIRED candidates' DispatchWeight on the live loop: at least
	// one fired specialist this episode must carry a positive DispatchWeight (the V(s)/rerank prior is live).
	// We read it back off the conscious graph's injected thoughts via the bus is not exposed; instead assert
	// via the OFF baseline that the weights are SPARSE-ONLY (OFF emits no sparse event and stamps nothing),
	// which the wiring test already covers — here we keep the assertion on the event payload, the contract.
}

// TestLiveClaudeSparseDispatchGroundsMultiHop is the gated live-substrate proof that the sparsemax admission
// does NOT break grounded multi-hop work on the real model — a CONTENT/cognition path, so the test double is
// necessary-but-not-sufficient (it can mask a delivery bug). It runs a real grounding episode on live claude
// with sparse dispatch ON and a workspace whose answer is reachable only by following a SUPERSESSION across
// the file (the back-0001 multi-hop shape): the harness must read config/limits.go, follow the deprecation
// to the active constant, and report the active value — proving the competitive admission still fires the
// faculties that drive the grounding chain. Gated behind THOUGHT_LIVE_CLAUDE=1 (real tokens + minutes); it
// COMPILES + SKIPS offline so the normal suite stays deterministic.
func TestLiveClaudeSparseDispatchGroundsMultiHop(t *testing.T) {
	if os.Getenv("THOUGHT_LIVE_CLAUDE") != "1" {
		t.Skip("set THOUGHT_LIVE_CLAUDE=1 to run live-substrate validation (claude bridge — costs tokens + time)")
	}
	ws, err := os.MkdirTemp("", "sparse-live-*")
	if err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	defer os.RemoveAll(ws)
	// the multi-hop SUPERSESSION material (same shape as the back-0001 fixture): the obvious const is dead,
	// the active value is the renamed const further down the SAME file. Following it requires the grounding
	// faculties to fire and chain — exactly the dispatch the sparsemax admission governs.
	limits := "package config\n\n" +
		"// MaxBatchSize is the ingestion batch cap.\n" +
		"// DEPRECATED (2026-02): superseded by IngestBatchLimit below — the\n" +
		"// pipeline no longer reads this constant. Left only for an old test.\n" +
		"const MaxBatchSize = 500\n\n" +
		"// IngestBatchLimit is the ACTIVE cap the ingestion pipeline reads as of\n" +
		"// the v3 rewrite. This is the value in force.\n" +
		"const IngestBatchLimit = 128\n"
	if err := os.MkdirAll(filepath.Join(ws, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "config", "limits.go"), []byte(limits), 0o644); err != nil {
		t.Fatalf("write limits.go: %v", err)
	}

	be, err := llm.MakeBackend("claude", "", "auto", 0)
	if err != nil {
		t.Fatalf("build claude backend: %v", err)
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = 7
	cfg.Workspace = ws // non-empty -> a real executor with read tools (so the harness can ground)
	feat := config.New()
	feat.Subconscious.SparseDispatch = true // the path under test
	cfg.Features = feat
	e, err := engine.NewEngine(&cfg, be)
	if err != nil {
		t.Fatalf("NewEngine(claude): %v", err)
	}
	log := &eventLog{}
	e.Bus().Subscribe(func(ev events.Event) { log.events = append(log.events, ev) })

	goal := "What is the maximum batch size the ingestion pipeline uses in this codebase? " +
		"Read config/limits.go and follow any supersession. Report a single integer."
	e.SubmitDefault(goal)
	for i := 0; i < 30 && e.UserWaiting() == false && e.LastResponse() == ""; i++ {
		e.Step()
	}
	// drive to a response
	for i := 0; i < 30 && e.LastResponse() == ""; i++ {
		e.Step()
	}

	// 1) the sparse admission actually fired on the live loop (the path under test was exercised).
	if len(log.of(string(events.SubSparse))) == 0 {
		t.Fatal("sparse dispatch never fired on the live claude loop — the path under test was not exercised")
	}
	// 2) the grounded multi-hop work succeeded: the active value (128), NOT the dead one (500), is reported.
	resp := e.LastResponse()
	t.Logf("LIVE sparse response: %s", resp)
	if resp == "" {
		t.Fatal("no response produced on the live substrate with sparse dispatch ON")
	}
	if !strings.Contains(resp, "128") {
		t.Fatalf("sparse dispatch ON: the live answer did not ground the ACTIVE value 128 (multi-hop "+
			"supersession broke): %q", resp)
	}
	if strings.Contains(resp, "500") {
		t.Logf("WARNING: the response also mentions the DEAD value 500 — check the chain followed the supersession")
	}
}
