package tiera

import "github.com/berttrycoding/thought-harness/internal/events"

// knownEventKinds is the set of event Kinds the event-key matcher recognizes as
// a prefix when parsing a "kind.field=value" trace-oracle key (e.g.
// "critic.decision=BACKTRACK" → kind "critic.decision", field "decision"). The
// event Kinds themselves contain dots, so the matcher needs the registry to
// tell where the kind ends and the ".field=value" selector begins.
//
// It references the exported events.* constants (not the events package's
// unexported allKinds slice) so each entry is a compile-checked symbol that
// moves with a rename. It covers every kind a Tier-A trace oracle plausibly
// keys on across the six mechanisms (spec §3.2/§3.3/§3.6 + §5.2). A kind NOT in
// this list still matches when used bare (no "=value" selector); the list only
// matters to locate the field boundary in a selector key.
var knownEventKinds = []events.Kind{
	// subconscious
	events.SubDispatch, events.SubFire, events.SubWorkflow, events.SubQuiet,
	events.SubSynthesize, events.SubOperator, events.SubSubagent,
	events.SkillMatch, events.SkillMint, events.SubSource, events.SubConcretize,
	// seam (hidden)
	events.Filter, events.Gate, events.Transform, events.Inject, events.Assemble,
	// conscious
	events.Generate, events.Append, events.MCP, events.XRef,
	// action / watched seam
	events.Intention, events.Act, events.Observation, events.Respond, events.Ask,
	events.ActionTool, events.ActionSandboxDeny, events.ActionSafetyBlock, events.ActionBlocked,
	// critic
	events.Decision, events.Exhaustion, events.Interrupt,
	// backend
	events.LLM, events.LLMFallback,
	// value / regulator / convert
	events.Value, events.Regulator, events.Stability, events.Schedule,
	events.Convert, events.PathMint,
	// grounding
	events.Ground, events.Percept,
	// session
	events.SessionSpawn, events.SessionDispatch, events.SessionMerge, events.SessionTerminate,
	// memory / retrieval / knowledge
	events.MemoryRecord, events.MemoryRecall, events.MemoryReflect,
	events.Retrieval,
	events.KnowledgeRecord, events.KnowledgeRecall, events.KnowledgeInvalidate,
	// config / escalation / persistence
	events.ConfigLoad, events.ConfigToggle, events.ConfigSkip,
	events.EscalationFloorStands,
	events.PersistLoad, events.PersistSave, events.PersistCurate,
	// lifecycle / ports
	events.Goal, events.Port, events.Lifecycle, events.Episode, events.Arousal, events.Tick,
}
