// modelselect.go — FIX 1: MODEL-DRIVEN TOOL-CALL SELECTION (the grounding-chain fix).
//
// THE BUG (project-tool-selection-hardcoded, the long-flagged root cause). A SubAgent's effectful
// move distils a concrete ToolCall from action.SelectTool(s.goal, "") — a REGEX over the STATIC
// episode goal. On a multi-hop CHAIN (env.yaml names active_profile:prod -> read prod.yaml -> read
// the checkout block) the NEXT-HOP target (config/profiles/prod.yaml) is only known from the PREVIOUS
// read's content — it appears in the prior OBSERVATIONS / the current thought, NOT in the static goal
// — so the regex cannot extract it and the chain dies at hop 1. (Measured: realhard K=3 multi-hop
// 7/9 misses, all grounding-chain.)
//
// THE FIX (Pattern C — deterministic floor + model ceiling on a flagged-fuzzy case, mirroring the
// Filter/Controller escalation). The regex floor (action.SelectTool over the static goal) stays the
// deterministic pick AND the offline/test path. When the flag is ON and the floor yields nothing
// usable from the static goal for a grounding-shaped step, escalate to the MODEL
// (RealityComprehender.Comprehend over the LIVE context — the prior observations + the current
// thought) to pick the call: esp. read_file{path} on the path the model reasoned to. The pick is a
// ToolCall like any other and runs through executor.Execute (ALL gates fire — protected-core /
// self-mutation refusal / sandbox / command content-gate / approve). No side channel, no bypass.
//
// DISCIPLINE. The model NEVER overrides a STRUCTURAL move — it is only consulted when the floor is
// EMPTY/fuzzy (nothing concrete to run from the goal), and only to PICK the call. The pick is
// scope-gated (a read/search the sub-agent's least-privilege scope permits) exactly like the floor.
// On model decline / no model / an out-of-scope target it falls straight back to the floor (so the
// test double — NOT a RealityComprehender — is byte-identical, goldens untouched).
//
// FLAG. THOUGHT_MODEL_SELECT (env-knob, resolved ONCE at init like THOUGHT_MAX_PAR_WIDTH /
// THOUGHT_BEAM_LAMBDA). Default OFF => the floor-only path => byte-identical.
package subconscious

import (
	"os"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/action"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// modelSelectEnabled is the THOUGHT_MODEL_SELECT toggle resolved ONCE at init (a missing/false/0
// value is OFF — byte-identical). Mirrors the resolveMaxParWidth / resolveBeamLambda env-knob pattern.
var modelSelectEnabled = resolveModelSelect()

// resolveModelSelect reads THOUGHT_MODEL_SELECT once. ON for "1"/"true"/"yes"/"on" (case-insensitive);
// anything else (incl. unset / garbage) is OFF — the safe default that keeps the floor-only path.
func resolveModelSelect() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("THOUGHT_MODEL_SELECT"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// modelSelectCall is the Pattern-C CEILING for the sub-agent's effectful tool pick. When the flag is
// ON, the sub-agent is a RealityComprehender model, and the deterministic FLOOR (floorCall/floorOK —
// the regex over the static goal) did NOT yield a usable call, it asks the model what to observe AND
// on what (Comprehend over the live ctx). It builds a scope-gated read_file{path} / search{pattern}
// from the model's (need, target) and emits escalation.tool_select (the floor's pick vs the model's).
// Returns ok=false (=> the caller keeps the floor) when: the flag is OFF, the backend is not a
// RealityComprehender (the test double), the model declines, the target is empty, the chosen tool is
// out of scope, or the model's pick equals what the floor would already do.
//
// The "floor did not yield a usable call" gate is the flagged-fuzzy predicate: a step whose static
// goal already names a readable file gets the floor's pick (no escalation, byte-stable on those
// steps); only a grounding-shaped step the goal cannot resolve escalates.
func (s *SubAgent) modelSelectCall(ctx []types.Thought, floorCall action.ToolCall, floorOK bool) (action.ToolCall, bool) {
	if !modelSelectEnabled {
		return action.ToolCall{}, false
	}
	// ESCALATION TRIGGER: only when the floor is fuzzy/empty — it could not distil a usable call for
	// this step from the static goal. A floor that already picked a concrete read/search stands (the
	// model is not second-guessed where the deterministic path is sufficient).
	if floorOK && usableFloorCall(floorCall) {
		return action.ToolCall{}, false
	}
	rec, ok := s.backend.(backends.RealityComprehender)
	if !ok {
		return action.ToolCall{}, false // the test double is not a comprehender => floor stands (byte-identical)
	}
	need, target, cok := rec.Comprehend(ctx)
	if !cok || strings.TrimSpace(target) == "" {
		return action.ToolCall{}, false // model declined / no concrete target => floor stands
	}
	scope := s.toolScope
	var call action.ToolCall
	switch need {
	case "read":
		if !containsScope(scope, "read_file") {
			return action.ToolCall{}, false // out of this sub-agent's least-privilege scope => floor stands
		}
		call = action.ToolCall{Name: "read_file", Args: map[string]any{"path": target}}
	case "search":
		if !containsScope(scope, "search") {
			return action.ToolCall{}, false
		}
		call = action.ToolCall{Name: "search", Args: map[string]any{"pattern": target}}
	default:
		// need=="run"/"none"/other: the SubAgent effectful selection only escalates the read/search
		// senses (the grounding chain); run keeps the floor (its target is a command). => floor stands.
		return action.ToolCall{}, false
	}
	// Surface the escalation (Pattern-C, never silent): the floor's pick vs the model's pick.
	floorTool := ""
	if floorOK {
		floorTool = floorCall.Name
	}
	if s.emit != nil {
		s.emit(events.EscalationToolSelect,
			"subagent.tool_select escalated ("+s.Role()+"/"+s.domain+": floor="+floorOrNone(floorTool)+
				" -> model="+call.Name+" "+clipRunes(target, 24)+")",
			events.D{
				"site":         "subagent.tool_select",
				"floor":        floorOK,
				"floor_tool":   floorTool,
				"model_tool":   call.Name,
				"model_target": target,
				"escalated":    true,
			})
	}
	return call, true
}

// usableFloorCall reports whether the floor's ToolCall is a concrete, grounding-SUFFICIENT pick the
// model need not be consulted over. The bar is deliberately a NAMED-FILE READ (read_file with a path
// the static goal resolved) or a RUN — those are exact, context-independent grounding picks. A bare
// SEARCH is NOT floor-sufficient: on a grounding chain it is a keyword guess over the static goal (e.g.
// search "trace"), which is the flagged-fuzzy case the model resolves better from the prior
// observations. So a search (or an empty/no-path read) is escalation-ELIGIBLE — the model ceiling is
// consulted, and on decline / no context-derived target the floor's search still stands (Comprehend
// returns ok=false). This is what lets a multi-hop chain advance past hop 1.
func usableFloorCall(c action.ToolCall) bool {
	switch c.Name {
	case "read_file":
		return strings.TrimSpace(argString(c.Args, "path")) != "" // a named-file read from the goal stands
	case "run_tests", "run_shell":
		return true // a run is an exact pick; the model's read/search ceiling does not preempt it
	case "web_search":
		return strings.TrimSpace(argString(c.Args, "query")) != "" // a web search on the goal is an exact pick — the read/search ceiling does not preempt it
	case "fetch_url":
		return strings.TrimSpace(argString(c.Args, "url")) != "" // a fetch of a NAMED url is an exact pick — the read/search ceiling does not preempt it (T1.4)
	}
	return false // a bare search / empty pick is flagged-fuzzy -> let the model ceiling try
}

// argString reads a string arg from a ToolCall's args map ("" when absent / not a string). Local to
// this leaf so the modelselect path does not reach into the action package's unexported helpers.
func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// floorOrNone renders the floor tool name for the event summary, or "none" when the floor declined.
func floorOrNone(name string) string {
	if name == "" {
		return "none"
	}
	return name
}
