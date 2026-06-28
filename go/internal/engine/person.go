// person.go wires the P7.3 person-adaptation loop into the engine — the "looks built, doesn't run"
// gap from the 2026-06-10 audit (PersonRegistry existed, was persisted, and was never consulted).
// The loop is the registry's designed semantics made real:
//
//	OBSERVE   a user turn that arrives as FEEDBACK on the previous answer and asks for a different
//	          interaction style is an OVERRIDE — detected by a deterministic keyword floor (Pattern A;
//	          a model has no authority over what the user literally asked for).
//	LEARN     PersonRegistry.ObserveOverride accumulates evidence; a consistent repeat crosses the
//	          threshold and becomes a LEARNED preference (observable on memory.record).
//	APPLY     learned preferences become a persona fragment appended to the RESPOND system prompt
//	          (the outward-facing surface only — thinking stays unstyled), via the same optional-
//	          interface pattern as the legible fragment. No learned preferences ⇒ empty fragment ⇒
//	          byte-identical prompt (and the test double ignores it entirely ⇒ goldens hold).
package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// overrideRule is one deterministic style-override detector: any of the phrases, said as feedback,
// observes (trait, value).
type overrideRule struct {
	phrases []string
	trait   string
	value   string
}

// overrideRules is the keyword FLOOR for style feedback. Deliberately small and literal — each entry
// is something a user says when the previous answer's STYLE (not content) missed: the floor must not
// fire on substantive follow-ups. First match wins (order: most specific first).
var overrideRules = []overrideRule{
	{[]string{"shorter", "too long", "be brief", "be terse", "less verbose", "too verbose"}, "verbosity", "terse"},
	{[]string{"more detail", "elaborate", "too short", "longer answer", "more verbose"}, "verbosity", "detailed"},
	{[]string{"bullet points", "as a list", "use bullets"}, "format", "list"},
	{[]string{"in prose", "no bullets", "no lists", "full sentences"}, "format", "prose"},
	{[]string{"plain language", "less jargon", "simpler words", "plain english"}, "style", "plain"},
	{[]string{"more technical", "more precise terminology"}, "style", "technical"},
}

// detectOverride classifies a user turn as a style override, returning the (trait, value) it asks
// for. ok=false when the turn is not style feedback (the common case — substantive turns never match).
func detectOverride(text string) (trait, value string, ok bool) {
	t := strings.ToLower(text)
	for _, r := range overrideRules {
		for _, p := range r.phrases {
			if strings.Contains(t, p) {
				return r.trait, r.value, true
			}
		}
	}
	return "", "", false
}

// observePersonFeedback runs the P7.3 OBSERVE step for one incoming user turn: only a turn that is
// FEEDBACK (the previous transcript turn was the assistant's answer) can be an override of it. On a
// detected override the registry accumulates evidence; the moment a preference crosses the threshold
// it is LEARNED and surfaced on the bus (memory.record, kind=preference). Deterministic; no model.
func (e *Engine) observePersonFeedback(userText string) {
	if e.person == nil || len(e.transcript) == 0 {
		return
	}
	if e.transcript[len(e.transcript)-1].Role != "assistant" {
		return // not feedback on an answer (e.g. the conversation's first turn)
	}
	trait, value, ok := detectOverride(userText)
	if !ok {
		return
	}
	if learned := e.person.ObserveOverride(trait, value); learned {
		p, _ := e.person.Preference(trait)
		e.bus.Emit(events.MemoryRecord,
			"person: learned preference "+trait+"="+value+" (evidence "+itoa(p.Evidence)+")",
			events.D{"kind": "preference", "trait": trait, "value": value, "evidence": p.Evidence})
	}
}

// personaFragment renders the learned preferences as the RESPOND prompt's adaptation instruction.
// "" when nothing is learned (the common case ⇒ the prompt is byte-identical to before).
func (e *Engine) personaFragment() string {
	if e.person == nil {
		return ""
	}
	applied := e.person.Applied()
	if len(applied) == 0 {
		return ""
	}
	parts := make([]string, 0, len(applied))
	for _, p := range applied {
		parts = append(parts, p.Trait+"="+p.Value)
	}
	return "Adapt to this user's LEARNED preferences (consistent past feedback): " +
		strings.Join(parts, "; ") + "."
}

// applyPersonaFragment pushes the current persona fragment into the backend before an outward-facing
// respond — the same optional-interface seam as the legible fragment. The test double does not
// implement PersonaPrompter, so the offline/golden path is untouched.
func (e *Engine) applyPersonaFragment() {
	pp, ok := e.backend.(backends.PersonaPrompter)
	if !ok {
		return
	}
	pp.SetPersonaFragment(e.personaFragment())
}
