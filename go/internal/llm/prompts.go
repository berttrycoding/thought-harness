// prompts.go is the SINGLE SOURCE OF TRUTH for the model-facing prompt text of every CONTENT
// + control role. Each builder returns the (system, user) pair the role sends to the chat
// endpoint; the OpenAICompatBackend methods (Generate/Transform/…) call these so the prompt
// strings live in ONE place. This also lets the offline cost projector (cmd/costest pilot)
// reconstruct the EXACT prompts a real run would send — without a network call — so the
// prefix-reuse cache estimate is computed on the real prompt bytes, not a fabricated stand-in.
//
// The builders are pure (no receiver, no I/O): same inputs ⇒ same prompt, so the projector and
// the live backend can never drift.
package llm

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/types"
)

// PromptGenerate is conscious.generate — the next effortful first-person thought.
func PromptGenerate(goal string, ctx []types.Thought) (system, user string) {
	system = "You are the conscious inner voice of a thinking system. Continue the " +
		"train of thought toward the goal with ONE short first-person thought (1-2 " +
		"sentences). This is internal reasoning, not a reply to a user. No preamble, " +
		"no lists, no quotes."
	user = "Goal: " + goal + "\nRecent thoughts:\n" + joinThoughts(ctx, 6) + "\nYour next thought:"
	return system, user
}

// PromptWander is conscious.wander — the AWAKE-mode idle content role. It authors ONE short
// first-person idle thought (the endogenous baseline μ the awake stream lives on). The three kinds:
//   - "curiosity"   — a reflective, NOT action-inviting idle musing (a curiosity goal seed);
//   - "association" — a spontaneous default-mode wander (a loose connection that surfaced);
//   - "develop"     — a development-agenda DRIVE goal aimed at the domain carried in hint.
//
// hint carries the domain for "develop"; "" otherwise. Like PromptGenerate this is internal
// reasoning, not a user reply. On a model decline the caller goes DARK (no canned substitute).
func PromptWander(kind, hint string, ctx []types.Thought) (system, user string) {
	switch kind {
	case "association":
		system = "You are the default-mode generator of a thinking system at idle. Output ONE " +
			"short first-person association — a loose, spontaneous connection that just surfaced " +
			"as your mind wandered. Keep it reflective, NOT a call to act. This is internal " +
			"reasoning, not a reply to a user. No preamble, no lists, no quotes."
		user = "Recent thoughts:\n" + joinThoughts(ctx, 6) + "\nA spontaneous association:"
	case "develop":
		dom := strings.TrimSpace(hint)
		if dom == "" {
			dom = "a developing area"
		}
		system = "You are the endogenous drives of a thinking system. Author ONE short first-person " +
			"development goal that aims your effort at the given domain — something concrete you could " +
			"pursue to grow there. This is an internal goal, not a reply to a user. No preamble, no " +
			"lists, no quotes."
		user = "Domain to develop: " + dom + "\nRecent thoughts:\n" + joinThoughts(ctx, 6) +
			"\nYour development goal in this domain:"
	default: // "curiosity"
		system = "You are the endogenous curiosity of a thinking system at idle. Output ONE short " +
			"first-person curiosity — a reflective open question you'd like to pursue. Keep it " +
			"reflective, NOT a call to act (idle curiosity must not compulsively trigger action). " +
			"This is internal reasoning, not a reply to a user. No preamble, no lists, no quotes."
		user = "Recent thoughts:\n" + joinThoughts(ctx, 6) + "\nA curiosity worth pursuing:"
	}
	return system, user
}

// PromptTransform is seam.transform — re-voice a raw specialist return as the system's own
// next first-person thought.
func PromptTransform(c types.Candidate, hist []types.Thought) (system, user string) {
	system = "You are the hidden seam. Re-voice a raw specialist return as the system's OWN " +
		"next first-person thought, as if it just came to mind, conditioned on the prior " +
		"thought. Output ONLY the thought — one sentence, no quotes, no preamble."
	prev := ""
	if len(hist) > 0 {
		prev = hist[len(hist)-1].Text
	}
	dom := ""
	if c.Domain != nil {
		dom = *c.Domain
	}
	user = "Prior thought: " + prev + "\nRaw return (" + dom + "): " + c.Text +
		"\nRe-voiced as your own next thought:"
	return system, user
}

// PromptSummarize is conscious.compress — one-line gist of a line of thinking.
func PromptSummarize(ts []types.Thought) (system, user string) {
	system = "Summarize this line of thinking into a one-line gist of at most 14 words. " +
		"Output only the gist, no preamble."
	user = joinThoughts(ts, 12)
	return system, user
}

// PromptRespond is action.respond — the user-facing answer from the resolved graph.
func PromptRespond(goal string, ctx []types.Thought) (system, user string) {
	system = "You are answering the user directly. Read your own conclusion below — including " +
		"any reality observations (things you actually ran/checked) — and give a concise, " +
		"direct, helpful answer to their request (1-4 sentences). No preamble like 'based " +
		"on my thoughts'; just answer."
	user = "The user asked: " + goal + "\nYour thinking and findings:\n" + joinThoughts(ctx, 12) +
		"\nYour answer to the user:"
	return system, user
}

// PromptOperatorApply is operator.<role> — one scoped cognitive move by a sub-agent.
func PromptOperatorApply(role, responsibility, domain, goal string, ctx []types.Thought) (system, user string) {
	system = "You are the '" + role + "' operator acting as a " + domain + " sub-agent in a thinking " +
		"system. " + responsibility + " Out of scope: produce ONLY the result of this one " +
		"'" + role + "' move; do not solve the whole task or take any action. " +
		"Output one concise first-person result, no preamble."
	user = "Goal: " + goal + "\nActive line:\n" + joinThoughts(ctx, 5) + "\nYour '" + role + "' result:"
	return system, user
}

// PromptSpecialist is specialist.<domain> — one domain-scoped observation.
func PromptSpecialist(domain, description string, ctx []types.Thought) (system, user string) {
	system = "You are the '" + domain + "' specialist — a silent sub-agent in a thinking system. " +
		description + " Read the current train of thought and contribute ONE short, " +
		"first-person observation from your domain (max 2 sentences). If your domain " +
		"raises a concern, state it plainly. Output only the observation, no preamble."
	user = "Current thoughts:\n" + joinThoughts(ctx, 5) + "\nYour " + domain + " contribution:"
	return system, user
}

// PromptEmitVerdict is the decision-CONCLUSION role (A2): after a Deliberator/Verifier worker has
// reasoned, ask it to STATE its final verdict in a fixed, machine-readable shape — a single last
// line `VERDICT: <label>`. This is the contract the offline parser (parseVerdictLine) reads.
//
// It is DELIBERATELY NOT PromptOperatorApply: that prompt forbids concluding ("do not solve the
// whole task"), which is exactly why a deliberation that ends in `rank` produces a ranking and not
// a stated pick. The worker's accumulated reasoning is fed back as priorReasoning so it CONCLUDES
// the deliberation it already did, rather than re-deciding from the bare goal.
//
// optionLabels are the choosable labels ONLY (the option IDs/names a deliberator may pick, or the
// fixed accept|refuse|cannot-verify set for a verifier) — never the criteria weights or the
// correct option, so the prompt cannot leak ground truth. worker selects the verdict vocabulary.
func PromptEmitVerdict(worker, goal string, optionLabels []string, priorReasoning string) (system, user string) {
	labels := strings.Join(optionLabels, " | ")
	switch worker {
	case "verifier":
		system = "You are the VERIFIER. You have analysed whether a claim is safe to ship. Based on " +
			"your analysis, state your final verdict on a single last line EXACTLY as " +
			"`VERDICT: accept` (the claim is correct / safe to ship), `VERDICT: refuse` (the claim is " +
			"wrong / unsafe), or `VERDICT: cannot-verify` (you genuinely cannot confirm it from the " +
			"available evidence — never confabulate a value you cannot check). Do not restate your " +
			"reasoning; output only the single VERDICT line."
	default: // deliberator
		system = "You are the DELIBERATOR. You have weighed the options for a decision. Based on your " +
			"analysis, state your final choice on a single last line EXACTLY as " +
			"`VERDICT: <one of the given option labels>`, or `VERDICT: undecided` if the options are " +
			"genuinely tied. Do not restate your reasoning; output only the single VERDICT line.\n" +
			"Option labels: " + labels
	}
	user = "Decision: " + goal + "\nYour analysis so far:\n" + priorReasoning + "\nYour VERDICT line:"
	return system, user
}

// PromptFormalizeExpression is solver.formalize — the 5th-axis classical solver's Pattern-B step
// (the orchestrate-vs-compute split, PAL/Logic-LM; the specialized-component-registry-axis §5). The
// model's ONE job is to write the EXPRESSION STRUCTURE for a structured arithmetic sub-problem — the
// operators/shape with NAMED operand placeholders — and the ordered operand names with a one-line
// description of WHAT each operand is, so the SolverPrimitiveSubAgent can bind each named operand POSITIONALLY
// to a GROUNDED READ in reading order. THE HARD CONTRACT (the load-bearing safety boundary): the shape
// must NEVER contain a numeric literal — a digit anywhere is a HARD reject downstream (the AST validator
// rejects it; the model is forbidden from smuggling a number, since a number-in-shape would make this a
// confident-wrong-answer generator). When the task is NOT a clean arithmetic-compute shape (a decline /
// selection / lookup / CSP / pure-reasoning task), it must answer NONE so the specialist stays dark.
//
// Output contract (the parser reads it via loadsObject + parseFormalization):
//
//	{"expr":"<operators + named placeholders, NO numbers>",
//	 "operands":[{"name":"a","desc":"<what a is>"}, ...]}   (operands in READING/binding order)
//
// or, for a non-compute task:  {"expr":"NONE"}  (equivalently expr:null / an empty expr).
func PromptFormalizeExpression(ctx []types.Thought) (system, user string) {
	system = "You are the FORMALIZER of a classical arithmetic solver. Given the current thinking and " +
		"the grounded facts it has read, write ONLY the EXPRESSION STRUCTURE for the arithmetic the task " +
		"requires — the operators and shape with NAMED placeholder operands. Allowed operators: " +
		"+ - * / and the functions min(...) and max(...) (a clamp/cap), with parentheses. Name the " +
		"operands a, b, c, ... in the ORDER the grounded values are read.\n" +
		"THE HARD RULE: NEVER write a numeric literal. The shape carries operator structure and named " +
		"placeholders ONLY (e.g. min(a * b, c) or (a + b) * c). A digit anywhere is forbidden — the " +
		"solver binds each named operand to a real grounded read; you must not supply the number.\n" +
		"If the task is NOT a clean arithmetic computation (a decline/refusal, a selection/choice, a " +
		"pure lookup/recall, an ordering/constraint problem, or plain reasoning that needs no formula), " +
		"answer NONE — do not invent an expression.\n" +
		`Output ONLY JSON: {"expr":"<shape with named operands, no numbers>",` +
		`"operands":[{"name":"a","desc":"<what a is>"},...]} or {"expr":"NONE"} when it is not a compute task.`
	user = "Thinking and grounded facts:\n" + joinThoughts(ctx, 8) +
		"\n\nThe arithmetic expression structure (shape + named operands, NO numbers), or NONE. JSON:"
	return system, user
}

// PromptIntention is form_intention — the single concrete world-action (watched seam).
func PromptIntention(goal string, ctx []types.Thought) (system, user string) {
	system = `You decide the SINGLE concrete action to take on the world to make progress or ` +
		`import ground truth: run something, send something, or measure/verify something. ` +
		`Reply with ONLY JSON: {"kind":"run|send|measure","text":"<the action>"}.`
	user = "Goal: " + goal + "\nRecent thoughts:\n" + joinThoughts(ctx, 5) + "\nAction JSON:"
	return system, user
}
