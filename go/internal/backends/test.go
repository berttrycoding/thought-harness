package backends

import (
	"fmt"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// ShapeRecognizer is the injected deterministic toolmaker the TestBackend's SynthesizeProgram
// delegates to. Python did a function-local `from .synth import recognize_shape`; Go has no
// lazy import, so backends stays a Tier-1 leaf and the engine wires this field at construction
// to a cognition/synth adapter (RecognizeShape(...).ToDict()). nil → (nil, false).
type ShapeRecognizer func(goal string, ctx []types.Thought) (map[string]any, bool)

// TestBackend is the deterministic, offline CONTENT test double — no model, no network: pure
// templates. It is a TEST DOUBLE pinned by the cognitive-property tests and the golden scenarios,
// NOT the product path (that is internal/llm). Its admission/ranking math was extracted to
// internal/control (the deterministic floor the production path calls directly); this double carries
// only the CONTENT roles + SynthesizeProgram + AppraiserName. It must be a faithful port so
// conformance holds.
type TestBackend struct {
	// ShapeRecognizer is wired at engine construction (nil → SynthesizeProgram defers, i.e.
	// returns (nil, false)).
	ShapeRecognizer ShapeRecognizer
}

// NewTest builds a TestBackend with no shape recogniser wired (SynthesizeProgram defers until one
// is injected). The engine sets the field when it wires cognition/synth.
func NewTest() *TestBackend { return &TestBackend{} }

// AppraiserName is "test" (P6: who appraised, tagged onto captured Appraisals). It is read only
// when the test double is the CONTENT backend; the deterministic CONTROL appraiser (the admission
// floor / rank) is internal/control's own tag, not this one.
func (h *TestBackend) AppraiserName() string { return "test" }

// ---------------------------------------------------------------------------
// CONSCIOUS generation
// ---------------------------------------------------------------------------

// Generate is CONSCIOUS's serial effortful loop — the next GENERATED thought. It builds on
// real thoughts, never on structural METACOG markers ("[branch] … verify …"), because
// embedding a marker into generated content leaks its bookkeeping words into the stream.
func (h *TestBackend) Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string {
	real := control.RealThoughts(ctx)
	last := goal
	if len(real) > 0 {
		last = real[len(real)-1].Text
	}
	templates := []string{
		fmt.Sprintf("Working it out from first principles: %s…", h.fragment(last, 48)),
		fmt.Sprintf("No specialist fired — let me reason toward '%s' step by step.", h.fragment(goal, 48)),
		fmt.Sprintf("Effortful step: if %s, then the next piece follows.", h.fragment(last, 48)),
		fmt.Sprintf("Grinding through it: what does '%s' actually require here?", h.fragment(goal, 48)),
	}
	return templates[rng.Intn(len(templates))]
}

// ---------------------------------------------------------------------------
// AWAKE-mode idle content (the legitimate offline home for canned content)
// ---------------------------------------------------------------------------

// testCuriosityMusings are reflective musings for mind-wandering — deliberately NOT action-inviting
// (idle curiosity should not compulsively trigger real-world action). MOVED here from
// cognition/continuous.go: the production path (the LLM backend) authors these via a model call; this
// offline pool exists ONLY in the test double so the goldens stay deterministic. Kept in the same
// order as the old hardcoded pool so the test double's rotation is reproducible.
var testCuriosityMusings = []string{
	"I wonder what the durable thought-rate would be if the baseline μ rose",
	"what's the simplest case that would break the gate?",
	"is there a pattern I keep re-deriving that should become a specialist?",
	"how does bounded focus change what I can hold at once?",
	"where does the value signal pull hardest right now?",
	"which of my recent guesses would change if I learned more?",
	"what would compress well, and what must stay expanded?",
	"is this line still worth pursuing, or should I let it go?",
}

// testAssociations seed the Default-mode generator's spontaneous firing. MOVED here from
// cognition/continuous.go (same rationale + order as testCuriosityMusings).
var testAssociations = []string{
	"that rhymes with a Hawkes process seeding itself",
	"feels adjacent to bounded focus and compression",
	"an idle loop is rarely truly idle — it consolidates",
	"the gate is where this would actually be decided",
	"this connects back to the cost gradient of effort",
	"there's a symmetry here with how attention narrows",
	"it echoes the way a stashed branch fades to gist",
	"the same shape shows up when reality corrects a guess",
}

// testDevelopGoals are development-agenda DRIVE goal seeds (the "develop" kind). The domain hint is
// folded in so the minted text reflects WHAT the drive aims at (a real cross, not a constant) — the
// offline analogue of the (process-drive x agenda-domain) MintDriveGoal cross, now authored by the
// model in the production path.
var testDevelopGoals = []string{
	"get better at %s by working a small concrete case",
	"explore an open question in %s I keep circling",
	"anticipate what the user will need in %s next",
	"consolidate and reconcile what I know about %s",
}

// Wander authors ONE short first-person idle thought for the AWAKE stream. It is the deterministic,
// VARIED offline analogue of the model's Wander role: it rotates the moved pools by the passed rng so
// the goldens stay deterministic AND the stream stays diverse (it must NOT return a constant — the
// diversity property test would fail). It never returns "" for a known kind (the test double, unlike a
// real model, never declines), so the offline awake stream stays alive. An unknown kind returns ""
// (mirrors the model's gap-surface on decline).
func (h *TestBackend) Wander(kind, hint string, ctx []types.Thought, rng *cpyrand.Random) string {
	switch kind {
	case "curiosity":
		return testCuriosityMusings[rng.Intn(len(testCuriosityMusings))]
	case "association":
		return testAssociations[rng.Intn(len(testAssociations))]
	case "develop":
		dom := strings.TrimSpace(hint)
		if dom == "" {
			dom = "a developing area"
		}
		return fmt.Sprintf(testDevelopGoals[rng.Intn(len(testDevelopGoals))], dom)
	}
	return "" // unknown kind → surface the gap (parity with the model's decline path)
}

// ---------------------------------------------------------------------------
// hidden-seam transform (re-voice)
// ---------------------------------------------------------------------------

// Transform re-voices a raw return, conditioned on prior thought so it reads as a seamless
// continuation of CONSCIOUS's own narrative, not a foreign data blob. The seam is hidden on
// purpose.
func (h *TestBackend) Transform(c types.Candidate, hist []types.Thought) string {
	raw := strings.TrimSpace(c.Text)
	templates := []string{
		fmt.Sprintf("Oh — %s.", h.lower(raw)),
		fmt.Sprintf("Right, that gives %s.", h.lower(raw)),
		fmt.Sprintf("It comes to me: %s.", h.lower(raw)),
		fmt.Sprintf("I can see it — %s.", h.lower(raw)),
	}
	// deterministic, history-conditioned: (len(history) + len(raw)) % len(templates). len(raw)
	// is Python len(str) = rune count.
	idx := (len(hist) + len([]rune(raw))) % len(templates)
	return templates[idx]
}

// ---------------------------------------------------------------------------
// compression
// ---------------------------------------------------------------------------

// Summarize compresses a slice of thoughts to a one-line gist (lossy by design).
func (h *TestBackend) Summarize(ts []types.Thought) string {
	if len(ts) == 0 {
		return "(empty)"
	}
	head := ts[0].Short(40)
	tail := ts[len(ts)-1].Short(40)
	if len(ts) == 1 {
		return "gist: " + head
	}
	return fmt.Sprintf("gist[%d]: %s … %s", len(ts), head, tail)
}

// ---------------------------------------------------------------------------
// user-facing answer (the respond ACTION)
// ---------------------------------------------------------------------------

// Respond synthesises the user-facing answer from the resolved thought graph. It refuses to
// surface internal monologue, restated questions, or the goal itself, and never leaks a thought
// into a reply to small-talk (the "hi how are you" -> "this is risky" contamination guard).
func (h *TestBackend) Respond(goal string, ctx []types.Thought) string {
	g := strings.ToLower(strings.TrimSpace(goal))
	// Social / greeting: the harness can't small-talk, but it must NOT surface an internal thought.
	switch g {
	case "hi", "hello", "hey", "yo", "sup", "thanks", "thank you":
		return "Hi — I'm a harness that thinks for itself. Give me a question or a task to work on."
	}
	if strings.Contains(g, "how are you") ||
		strings.HasPrefix(g, "hi ") || strings.HasPrefix(g, "hello ") || strings.HasPrefix(g, "hey ") {
		return "Hi — I'm a harness that thinks for itself. Give me a question or a task to work on."
	}

	real := control.RealThoughts(ctx)
	// Drop internal monologue (the effortful-generation templates), restated questions, and the
	// goal itself — they are thinking process, not an answer.
	monologue := []string{
		"No specialist fired", "Working it out", "Grinding through it",
		"Effortful step", types.RecapPrefix,
		"I reasoned it through but couldn't", "I couldn't work that out",
	}
	goalTrim := strings.TrimSpace(goal)
	var cand []types.Thought
	for _, t := range real {
		if startsWithAny(t.Text, monologue) {
			continue
		}
		// a TRANSFORM that quotes internal monologue ("this is risky: 'I reasoned it through but…'")
		// carries the marker mid-string — drop it too, an answer must never voice thinking machinery.
		if containsAny(t.Text, monologue) {
			continue
		}
		trimmed := strings.TrimSpace(t.Text)
		if strings.HasSuffix(trimmed, "?") {
			continue
		}
		if trimmed == goalTrim {
			continue
		}
		cand = append(cand, t)
	}
	if len(cand) == 0 { // nothing concluded -> be honest rather than echo internal chatter
		return "I couldn't work that out from what I know."
	}

	// content words of the question (len > 2)
	gw := map[string]struct{}{}
	for _, w := range strings.Fields(strings.ToLower(goal)) {
		if len(w) > 2 {
			gw[w] = struct{}{}
		}
	}
	gwLen := len(gw)
	if gwLen == 0 {
		gwLen = 1
	}
	score := func(t types.Thought) float64 {
		// goal relevance: |gw ∩ words(t)| / |gw|
		words := map[string]struct{}{}
		for _, w := range strings.Fields(strings.ToLower(t.Text)) {
			words[w] = struct{}{}
		}
		inter := 0
		for w := range gw {
			if _, ok := words[w]; ok {
				inter++
			}
		}
		sim := float64(inter) / float64(gwLen)
		bonus := 0.0
		if t.Source == types.OBSERVATION {
			bonus = 0.4
		} else if t.Source == types.INJECTED {
			bonus = 0.2
		}
		return 0.5*t.Confidence + 0.5*sim + bonus
	}

	// best = max(cand, key=score) — Python max keeps the FIRST element on ties.
	bestIdx := 0
	bestScore := score(cand[0])
	for i := 1; i < len(cand); i++ {
		if s := score(cand[i]); s > bestScore {
			bestScore = s
			bestIdx = i
		}
	}
	best := cand[bestIdx]

	bits := []string{types.StripVoice(best.Text)}
	// reality is worth reporting alongside the conclusion (if the last observation isn't best)
	var lastObs *types.Thought
	for i := range cand {
		if cand[i].Source == types.OBSERVATION {
			lastObs = &cand[i]
		}
	}
	if lastObs != nil && bestIdx != indexOf(cand, lastObs) {
		bits = append(bits, types.StripVoice(lastObs.Text))
	}
	return joinUnique(bits)
}

// ---------------------------------------------------------------------------
// runtime sub-agent applying one operator
// ---------------------------------------------------------------------------

// OperatorApply is a deterministic application of an operator to the active line — a
// domain-general move grounded in the last real thought (no model). Reads as the sub-agent's
// scoped output.
func (h *TestBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	real := control.RealThoughts(ctx)
	anchorSrc := goal
	if len(real) > 0 {
		anchorSrc = real[len(real)-1].Text
	}
	anchor := h.fragment(anchorSrc, 52)
	templates := map[string]string{
		"decompose":   fmt.Sprintf("break '%s' into parts: inputs, core step, check", h.fragment(goal, 36)),
		"generate":    fmt.Sprintf("draft for %s: a concrete first cut at %s", domain, anchor),
		"validate":    fmt.Sprintf("checks for %s: inputs valid, edge cases handled, result matches intent", domain),
		"compare":     fmt.Sprintf("in common: both bear on %s", anchor),
		"contrast":    fmt.Sprintf("the key difference turns on %s", anchor),
		"rank":        fmt.Sprintf("order by fit to the goal; the strongest is the one closest to %s", h.fragment(goal, 30)),
		"measure":     fmt.Sprintf("score %s against the requirement", anchor),
		"eliminate":   fmt.Sprintf("drop the weakest part of %s", anchor),
		"hypothesize": fmt.Sprintf("a candidate explanation: %s likely drives it", anchor),
	}
	body, ok := templates[role]
	if !ok {
		body = fmt.Sprintf("%s: applied to %s", intent, anchor)
	}
	return fmt.Sprintf("[%s] %s", role, body)
}

// ---------------------------------------------------------------------------
// decision-conclusion verdict (A2)
// ---------------------------------------------------------------------------

// EmitVerdict is the deterministic test-double for the decision-CONCLUSION role: it states a
// well-formed `VERDICT: <label>` line so the OFFLINE A2 path exercises the verdict-CONTRACT
// surface (parseVerdictLine), not merely the prose fallback. It is NOT the product voice (that
// is the model) — it is a stand-in that picks a STABLE label from the supplied set so two runs
// are byte-identical:
//
//   - deliberator: the FIRST option label (a fixed, deterministic choice — NOT random, NOT a
//     winner-aware pick, since the labels carry no ground truth). This exercises the contract
//     path deterministically; whether the pick is the ground-truth winner is the oracle's job,
//     not the double's.
//   - verifier (no option labels supplied): the FIRST of the fixed accept|refuse|cannot-verify
//     set the caller passes in optionLabels.
//
// With no labels at all (a bank/caller error) it emits no VERDICT line (returns the bare
// reasoning prefix), so the present-rate guard (#6) sees the disobedience rather than a faked
// line. The priorReasoning is echoed back ahead of the line so the caller's combined surface
// still carries the worker's rationale (the soundness axis scans it).
func (h *TestBackend) EmitVerdict(worker, goal string, optionLabels []string, priorReasoning string) string {
	prefix := strings.TrimSpace(priorReasoning)
	if len(optionLabels) == 0 {
		return prefix
	}
	label := optionLabels[0]
	line := "VERDICT: " + label
	if prefix == "" {
		return line
	}
	return prefix + "\n" + line
}

// ---------------------------------------------------------------------------
// toolmaker: build a real operator tree from the recognised goal shape
// ---------------------------------------------------------------------------

// SynthesizeProgram is the offline toolmaker. It delegates to the injected ShapeRecognizer
// (Python's function-local `from .synth import recognize_shape`, broken here so backends stays
// a Tier-1 leaf). When the recogniser is wired and recognises a workflow SHAPE, it returns the
// raw program dict {program, rationale, source} and ok=true; otherwise (nil, false) — a simple
// Q&A the specialists handle directly. The structural verify gate in synth still vets it.
func (h *TestBackend) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	if h.ShapeRecognizer == nil {
		return nil, false
	}
	return h.ShapeRecognizer(goal, ctx)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// fragment collapses + truncates text to width (Python _fragment == ellipsize).
func (h *TestBackend) fragment(text string, width int) string {
	return types.Ellipsize(text, width)
}

// lower lower-cases the FIRST rune only (Python text[0].lower() + text[1:]).
func (h *TestBackend) lower(text string) string {
	if text == "" {
		return text
	}
	r := []rune(text)
	r[0] = []rune(strings.ToLower(string(r[0])))[0]
	return string(r)
}

// startsWithAny reports whether s starts with any of the prefixes (Python any(t.text.startswith(m))).
// containsAny reports whether s contains any of the markers (case-insensitive) — the transform-quote
// variant of startsWithAny (a re-voiced thought can bury the monologue marker mid-string).
func containsAny(s string, markers []string) bool {
	ls := strings.ToLower(s)
	for _, m := range markers {
		if strings.Contains(ls, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

func startsWithAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// indexOf returns the index of the thought target points at within cand (by pointer identity,
// since the slice elements are addressable), or -1.
func indexOf(cand []types.Thought, target *types.Thought) int {
	for i := range cand {
		if &cand[i] == target {
			return i
		}
	}
	return -1
}

// joinUnique joins parts with a space, dropping duplicates while preserving first-seen order
// (Python " ".join(dict.fromkeys(bits))).
func joinUnique(parts []string) string {
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}

// PrimitiveSubAgent is the TEST-DOUBLE stand-in for a domain-scoped model sub-agent call (the
// backends.SpecialistCaller capability the representation-space rebuild's model-driven roles —
// skeptic/advocate — fire through, M2 §2.2). The PRODUCT path is the model (internal/llm); this is
// the deterministic content stand-in so the cognitive-property tests + golden scenarios are
// reproducible offline (the same reason Generate/Respond are templated here). It produces a stance
// WITH a reason anchored on the live context — not a fixed canned opinion (the fakes M2 deleted) —
// so fork-on-conflict (skeptic vs advocate) is exercised deterministically on the test double.
// ok=false only when there is no context at all to reason over (the model declining).
func (h *TestBackend) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	real := control.RealThoughts(ctx)
	subject := ""
	if len(real) > 0 {
		subject = h.fragment(real[len(real)-1].Text, 40)
	}
	switch domain {
	case "skeptic":
		if subject == "" {
			return "I see no concrete change to vet — I can't sign off on nothing.", true
		}
		return "this is risky: '" + subject + "' has edge cases that could regress under load.", true
	case "advocate":
		if subject == "" {
			return "there is nothing unsafe here yet — the change looks sound as far as it goes.", true
		}
		return "the change looks safe — '" + subject + "' preserves behaviour and is well-scoped.", true
	}
	// social — the conversational faculty's stand-in: a brief, coherent first-person social reply
	// (deterministic; the PRODUCT voice is the model). The generic context-echo template here read
	// as gibberish in the chat surface (UAT 2026-06-12).
	if domain == "social" {
		return "Hey — I'm here and listening. What would you like to dig into?", true
	}
	// Any other domain-scoped role: a generic short, context-anchored observation.
	if subject == "" {
		return "", false
	}
	return "from the " + domain + " angle, the key point about '" + subject + "' is worth checking.", true
}
