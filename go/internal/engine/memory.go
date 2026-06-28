package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/memory"
	"github.com/berttrycoding/thought-harness/internal/retrieval"
)

// RecallFact satisfies subconscious.MemoryRecaller (M2 §2.4): it is the port the `recall` primitive
// consults so the conscious stream can pull what the engine actually grounded. It recalls over the REAL
// stores — semantic beliefs first (distilled, durable), then episodic outcomes — applying their existing
// never-fabricate + relevance-gated Recall (an empty store / unrelated query surfaces nothing). It NEVER
// fabricates a miss: ok=false means the conscious stream gets no injected fact, not an invented one. This
// replaces the deleted 5-fact toy KB the old MemorySpecialist read — the worst synergy gap, now closed.
func (e *Engine) RecallFact(query string) (string, bool) {
	if e.semantic != nil {
		if bs := e.semantic.Recall(query, 1); len(bs) > 0 {
			return bs[0].Statement, true
		}
	}
	if e.episodic != nil {
		if eps := e.episodic.Recall(query, 1); len(eps) > 0 {
			// surface the grounded outcome of the most relevant past episode (first-person experience)
			if strings.TrimSpace(eps[0].Outcome) != "" {
				return eps[0].Outcome, true
			}
		}
	}
	return "", false
}

// reflectionValueFloor is the value an episode must clear before an idle-tick reflection distils it into
// a standing belief — only a high-value GROUNDED outcome earns a semantic fact (a low-value or barely-
// grounded episode distils nothing, so semantic memory stays trustworthy).
const reflectionValueFloor = 0.5

// recallMemory consults the declarative memory at the start of a fresh episode (P2.3): it recalls past
// episodes related to the new goal and emits memory.recall when any surface — cross-episode transfer made
// observable. Relevance-gated: an unrelated goal recalls nothing (no event). Silent (and golden-safe) on
// a cold store, which is every single-episode scenario's first episode.
func (e *Engine) recallMemory(goal string) {
	// CONFIG (M1): memory.recall OFF ⇒ skip episode-start recall (bypass, not delete — the store stays,
	// the wire stays, the TUI renders it DISABLED). Determinism unchanged (a no-op emits no recall).
	if e.gates.memRecall.Disabled() {
		e.gates.memRecall.Skip("recall bypassed")
		return
	}
	eps := e.episodic.Recall(goal, 3)
	if len(eps) == 0 {
		return
	}
	e.bus.Emit(events.MemoryRecall, "recalled "+itoa(len(eps))+" related episode(s)", events.D{
		"goal": goal, "count": len(eps), "top": eps[0].Goal,
	})
	// the shared hybrid retriever made this recall (P1.x): surface the breakdown — the top match's
	// lexical score + the retriever mode (semantic was used inside Recall iff mode==hybrid). Makes the
	// retrieval primitive observable at the point it actually runs.
	top := retrieval.Item{Text: eps[0].Goal + " " + eps[0].Outcome, Entities: eps[0].Entities}
	e.bus.Emit(events.Retrieval, "retrieval ("+e.retrieverMode+"): "+itoa(len(eps))+" of "+itoa(e.episodic.Len()),
		events.D{"mode": e.retrieverMode, "lexical": round2(retrieval.LexicalScore(goal, top)),
			"candidates": e.episodic.Len(), "recalled": len(eps)})
}

// recordEpisode writes the just-finished episode into episodic memory at episode-end (P2.3). NEVER-
// FABRICATE: an episode is recorded only if it GROUNDED a claim against reality this episode (the
// grounding ledger grew) — an ungrounded episode is rejected and emits nothing, so memory holds only
// outcomes reality actually confirmed. That gate is also why memory.record only joins the scenario
// goldens where the harness grounded something.
func (e *Engine) recordEpisode() {
	if e.graph == nil {
		return
	}
	// CONFIG (M1): memory.episodic OFF ⇒ skip episode-end recording (bypass, not delete).
	if e.gates.memRecord.Disabled() {
		e.gates.memRecord.Skip("episode record bypassed")
		return
	}
	grounded := e.grounding.Len() > e.episodeGroundBase
	ep := memory.Episode{
		Goal:     e.graph.Goal,
		Outcome:  e.lastResponse,
		Grounded: grounded,
		Value:    e.graph.Active().Value,
		Tick:     e.bus.Tick,
	}
	if !e.episodic.Record(ep) {
		return // ungrounded — never-fabricate, not stored, not emitted
	}
	e.bus.Emit(events.MemoryRecord, "recorded episode: "+runeSlice(ep.Goal, 50), events.D{
		"goal": ep.Goal, "grounded": ep.Grounded, "value": round2(ep.Value), "episodes": e.episodic.Len(),
	})
}

// reflectMemory runs the idle-tick consolidation (P6.2): a grounded, high-value episode is distilled into
// a standing semantic belief (transfer), and a refuted one invalidates a matching belief (bi-temporal).
// Emits memory.reflect only when something was distilled — a low-value run consolidates nothing (silent).
func (e *Engine) reflectMemory() {
	// CONFIG (M1): memory.reflect OFF ⇒ skip the idle-tick distillation (bypass, not delete — no belief
	// is minted, semantic memory simply does not grow this tick).
	if e.gates.memReflect.Disabled() {
		e.gates.memReflect.Skip("reflection bypassed")
		return
	}
	n := memory.Reflect(e.episodic, e.semantic, reflectionValueFloor, e.bus.Tick)
	if n == 0 {
		return
	}
	e.bus.Emit(events.MemoryReflect, "reflection distilled "+itoa(n)+" belief(s)", events.D{
		"distilled": n, "beliefs": e.semantic.Len(),
	})
}
