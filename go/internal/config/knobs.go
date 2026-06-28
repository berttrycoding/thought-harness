package config

import (
	"errors"
	"strconv"
	"strings"
)

// KnobKind distinguishes a bool toggle from a typed tunable so a consumer (CLI / env / TUI) renders
// + parses it correctly.
type KnobKind int

const (
	KnobBool   KnobKind = iota // an on/off toggle
	KnobInt                    // an int tunable (e.g. max_par_width)
	KnobString                 // a string tunable (e.g. persistence.backend)
	KnobFloat                  // a float tunable (e.g. conscious.activity.*)
)

// Knob is one addressable config entry: a dotted path, a human label, its kind, and typed
// get/set accessors over a *HarnessConfig. The single []Knob table (Knobs()) is the ONE place a
// toggle is declared — CLI --enable/--disable, THOUGHT_CFG_* env parsing, and the TUI Config panel
// all consume it, so adding a toggle is a single-table edit.
type Knob struct {
	Path  string   // dotted path, e.g. "seam.hidden_transform"
	Label string   // human label for the TUI panel
	Kind  KnobKind // bool / int / float / string

	// OptIn marks a bool toggle whose BASELINE is OFF (an opt-in instrument, not an all-on ablation).
	// Such a knob being OFF is NOT a deviation from the all-on baseline, so OffPaths() excludes it —
	// keeping the config.load OFF-summary (and thus every golden) byte-identical when only opt-in knobs
	// are off. The one such knob today is seam.legible_generation (the WF-E CC-1 SHADOW instrument).
	OptIn bool

	getBool   func(*HarnessConfig) bool
	setBool   func(*HarnessConfig, bool)
	getInt    func(*HarnessConfig) int
	setInt    func(*HarnessConfig, int)
	getString func(*HarnessConfig) string
	setString func(*HarnessConfig, string)
	getFloat  func(*HarnessConfig) float64
	setFloat  func(*HarnessConfig, float64)
}

// GetBool reads a bool knob; ok=false if the knob is not a bool.
func (k Knob) GetBool(c *HarnessConfig) (bool, bool) {
	if k.getBool == nil {
		return false, false
	}
	return k.getBool(c), true
}

// SetBool writes a bool knob; ok=false if the knob is not a bool.
func (k Knob) SetBool(c *HarnessConfig, v bool) bool {
	if k.setBool == nil {
		return false
	}
	k.setBool(c, v)
	return true
}

// GetInt reads an int knob; ok=false if not an int.
func (k Knob) GetInt(c *HarnessConfig) (int, bool) {
	if k.getInt == nil {
		return 0, false
	}
	return k.getInt(c), true
}

// GetString reads a string knob; ok=false if not a string.
func (k Knob) GetString(c *HarnessConfig) (string, bool) {
	if k.getString == nil {
		return "", false
	}
	return k.getString(c), true
}

// GetFloat reads a float knob; ok=false if the knob is not a float.
func (k Knob) GetFloat(c *HarnessConfig) (float64, bool) {
	if k.getFloat == nil {
		return 0, false
	}
	return k.getFloat(c), true
}

// SetFloat writes a float knob; ok=false if the knob is not a float.
func (k Knob) SetFloat(c *HarnessConfig, v float64) bool {
	if k.setFloat == nil {
		return false
	}
	k.setFloat(c, v)
	return true
}

// SetFromString parses+applies a value off its string form (the env / CLI surface). A bool accepts
// on/off/true/false/1/0/yes/no/enable/disable (case-insensitive); an int parses base-10; a string
// is taken verbatim. Returns an error on a malformed value.
func (k Knob) SetFromString(c *HarnessConfig, raw string) error {
	switch k.Kind {
	case KnobBool:
		b, err := parseBool(raw)
		if err != nil {
			return err
		}
		k.setBool(c, b)
		return nil
	case KnobInt:
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return errors.New("not an integer: " + raw)
		}
		k.setInt(c, n)
		return nil
	case KnobString:
		k.setString(c, raw)
		return nil
	case KnobFloat:
		f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return errors.New("not a float: " + raw)
		}
		k.setFloat(c, f)
		return nil
	}
	return errors.New("unknown knob kind")
}

// parseBool accepts the friendly on/off vocabulary (case-insensitive) the CLI/env surface uses.
func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "true", "1", "yes", "y", "enable", "enabled":
		return true, nil
	case "off", "false", "0", "no", "n", "disable", "disabled":
		return false, nil
	}
	return false, errors.New("not a boolean (use on|off): " + raw)
}

// KnobByPath finds a knob by its dotted path; ok=false if no such knob.
func KnobByPath(path string) (Knob, bool) {
	for _, k := range Knobs() {
		if k.Path == path {
			return k, true
		}
	}
	return Knob{}, false
}

// ApplyToggle flips a BOOL knob by path; ok=false if the path is unknown or not a bool. The single
// entry CLI --enable/--disable and the TUI live-flip both call. Live flips mutate the SHARED config
// pointer in place, so the engine's gates observe the change with no rebuild.
func ApplyToggle(c *HarnessConfig, path string, on bool) bool {
	k, ok := KnobByPath(path)
	if !ok || k.Kind != KnobBool {
		return false
	}
	return k.SetBool(c, on)
}

// SetTunable sets a non-bool knob (int/string) by path from its string form; ok=false on an unknown
// path or a parse error.
func SetTunable(c *HarnessConfig, path, raw string) bool {
	k, ok := KnobByPath(path)
	if !ok {
		return false
	}
	return k.SetFromString(c, raw) == nil
}

// boolKnob is a terse constructor for a bool toggle row (baseline ON — all-on).
func boolKnob(path, label string, get func(*HarnessConfig) bool, set func(*HarnessConfig, bool)) Knob {
	return Knob{Path: path, Label: label, Kind: KnobBool, getBool: get, setBool: set}
}

// optInBoolKnob is a terse constructor for a bool toggle whose BASELINE is OFF — an opt-in instrument,
// not an all-on ablation. It is addressable like any other knob (CLI/env/TUI can flip it on) but its
// default-OFF state is excluded from OffPaths() so the config.load summary stays byte-identical.
func optInBoolKnob(path, label string, get func(*HarnessConfig) bool, set func(*HarnessConfig, bool)) Knob {
	return Knob{Path: path, Label: label, Kind: KnobBool, OptIn: true, getBool: get, setBool: set}
}

// intKnob is a terse constructor for an int tunable row.
func intKnob(path, label string, get func(*HarnessConfig) int, set func(*HarnessConfig, int)) Knob {
	return Knob{Path: path, Label: label, Kind: KnobInt, getInt: get, setInt: set}
}

// floatKnob is a terse constructor for a float tunable row.
func floatKnob(path, label string, get func(*HarnessConfig) float64, set func(*HarnessConfig, float64)) Knob {
	return Knob{Path: path, Label: label, Kind: KnobFloat, getFloat: get, setFloat: set}
}

// stringKnob is a terse constructor for a string tunable row.
func stringKnob(path, label string, get func(*HarnessConfig) string, set func(*HarnessConfig, string)) Knob {
	return Knob{Path: path, Label: label, Kind: KnobString, getString: get, setString: set}
}

// knobTable is the canonical, ordered list of every addressable config knob — built once. The order
// is the TUI panel's left-rail order (subconscious -> conscious -> ... -> representation ->
// persistence). One table, three consumers (CLI, env, TUI).
var knobTable = []Knob{
	// subconscious
	boolKnob("subconscious.specialists", "Specialists",
		func(c *HarnessConfig) bool { return c.Subconscious.Specialists }, func(c *HarnessConfig, v bool) { c.Subconscious.Specialists = v }),
	boolKnob("subconscious.dispatch", "Dispatch",
		func(c *HarnessConfig) bool { return c.Subconscious.Dispatch }, func(c *HarnessConfig, v bool) { c.Subconscious.Dispatch = v }),
	boolKnob("subconscious.operators", "Operators",
		func(c *HarnessConfig) bool { return c.Subconscious.Operators }, func(c *HarnessConfig, v bool) { c.Subconscious.Operators = v }),
	boolKnob("subconscious.operator_mint", "Operator mint",
		func(c *HarnessConfig) bool { return c.Subconscious.OperatorMint }, func(c *HarnessConfig, v bool) { c.Subconscious.OperatorMint = v }),
	boolKnob("subconscious.synthesis", "Synthesis",
		func(c *HarnessConfig) bool { return c.Subconscious.Synthesis }, func(c *HarnessConfig, v bool) { c.Subconscious.Synthesis = v }),
	boolKnob("subconscious.workflows", "Workflows",
		func(c *HarnessConfig) bool { return c.Subconscious.Workflows }, func(c *HarnessConfig, v bool) { c.Subconscious.Workflows = v }),
	boolKnob("subconscious.subagents", "Sub-agents",
		func(c *HarnessConfig) bool { return c.Subconscious.SubAgents }, func(c *HarnessConfig, v bool) { c.Subconscious.SubAgents = v }),
	boolKnob("subconscious.skills", "Skills",
		func(c *HarnessConfig) bool { return c.Subconscious.Skills }, func(c *HarnessConfig, v bool) { c.Subconscious.Skills = v }),
	boolKnob("subconscious.sourcing", "Sourcing ladder",
		func(c *HarnessConfig) bool { return c.Subconscious.Sourcing }, func(c *HarnessConfig, v bool) { c.Subconscious.Sourcing = v }),
	boolKnob("subconscious.concretize", "Concretize",
		func(c *HarnessConfig) bool { return c.Subconscious.Concretize }, func(c *HarnessConfig, v bool) { c.Subconscious.Concretize = v }),
	intKnob("subconscious.max_par_width", "Max parallel width",
		func(c *HarnessConfig) int { return c.Subconscious.MaxParWidth }, func(c *HarnessConfig, v int) { c.Subconscious.MaxParWidth = v }),
	optInBoolKnob("subconscious.capability", "Capability produces workflow",
		func(c *HarnessConfig) bool { return c.Subconscious.Capability }, func(c *HarnessConfig, v bool) { c.Subconscious.Capability = v }),
	optInBoolKnob("subconscious.capability_dispatch", "Capability is the live relevance/dispatch entry",
		func(c *HarnessConfig) bool { return c.Subconscious.CapabilityDispatch }, func(c *HarnessConfig, v bool) { c.Subconscious.CapabilityDispatch = v }),
	optInBoolKnob("subconscious.capability_specialists", "Capability owns specialist firing (§3.3a domain band)",
		func(c *HarnessConfig) bool { return c.Subconscious.CapabilityPrimitiveSubAgents }, func(c *HarnessConfig, v bool) { c.Subconscious.CapabilityPrimitiveSubAgents = v }),
	optInBoolKnob("subconscious.solver_specialist", "Solver specialist (5th-axis classical solver)",
		func(c *HarnessConfig) bool { return c.Subconscious.SolverPrimitiveSubAgent }, func(c *HarnessConfig, v bool) { c.Subconscious.SolverPrimitiveSubAgent = v }),
	optInBoolKnob("subconscious.tier_router", "Tier router (cost-aware substrate routing)",
		func(c *HarnessConfig) bool { return c.Subconscious.TierRouter }, func(c *HarnessConfig, v bool) { c.Subconscious.TierRouter = v }),
	optInBoolKnob("subconscious.semantic_recall", "Semantic recall (embeddings sidecar lights up dense retrieval)",
		func(c *HarnessConfig) bool { return c.Subconscious.SemanticRecall }, func(c *HarnessConfig, v bool) { c.Subconscious.SemanticRecall = v }),
	optInBoolKnob("subconscious.graph_recall", "Graph-native recall + write-back (cogngraph multi-hop; no separate store)",
		func(c *HarnessConfig) bool { return c.Subconscious.GraphRecall }, func(c *HarnessConfig, v bool) { c.Subconscious.GraphRecall = v }),
	// SPARSE-DISPATCH: sparsemax over the specialist relevance field replaces the per-key absolute eff>theta
	// admission with a competitive relative one (θ survives as a floor under τ; p_i stamped as dispatch
	// confidence). optInBoolKnob ⇒ default-OFF excluded from OffPaths() ⇒ the config.load summary + goldens
	// stay byte-identical. Env: THOUGHT_CFG_SUBCONSCIOUS_DISPATCH_SPARSE=on.
	optInBoolKnob("subconscious.dispatch.sparse", "Sparsemax dispatch (competitive relative admission over the specialist field)",
		func(c *HarnessConfig) bool { return c.Subconscious.SparseDispatch }, func(c *HarnessConfig, v bool) { c.Subconscious.SparseDispatch = v }),
	// SUB-AGENT GUARD: collapse the per-tick sub-agent fan-out to its single best member — the teaming-guard
	// reference arm. optInBoolKnob ⇒ default-OFF excluded from OffPaths() ⇒ byte-identical config summary + goldens.
	optInBoolKnob("subconscious.single_strong_agent", "Single strong agent (collapse the sub-agent fan-out to its best member — the teaming-guard reference arm)",
		func(c *HarnessConfig) bool { return c.Subconscious.SingleStrongAgent }, func(c *HarnessConfig, v bool) { c.Subconscious.SingleStrongAgent = v }),
	// WEB-SEARCH: register the model-callable web_search tool + give expose-affordances the web_search scope
	// so a lookup-shaped goal dispatches a real web search over the injected web.Web seam (the GAIA enabler).
	// optInBoolKnob ⇒ default-OFF excluded from OffPaths() ⇒ the config.load summary + goldens stay byte-
	// identical; double-gated on a wired Web seam, so a knob ON with no edge-wired Web is inert (the go-test
	// path). Env: THOUGHT_CFG_SUBCONSCIOUS_WEB_SEARCH=on.
	optInBoolKnob("subconscious.web_search", "Web search (model-callable web_search tool over the injected web seam — GAIA / web-lookup enabler)",
		func(c *HarnessConfig) bool { return c.Subconscious.WebSearch }, func(c *HarnessConfig, v bool) { c.Subconscious.WebSearch = v }),
	// FETCH-URL (T1.4): register the model-callable fetch_url tool + give expose-affordances the fetch_url
	// scope so a goal/observation carrying a result URL fetches that page over the injected web.PageFetcher
	// seam (the BrowseComp browse-loop enabler, sibling of web_search). optInBoolKnob ⇒ default-OFF excluded
	// from OffPaths() ⇒ the config.load summary + goldens stay byte-identical; double-gated on a wired
	// PageFetcher seam, so a knob ON with no edge-wired seam is inert (the go-test path). The browse loop is
	// EMERGENT (no hardcoded multi-step loop ⇒ plant unchanged). Env: THOUGHT_CFG_SUBCONSCIOUS_FETCH_URL=on.
	optInBoolKnob("subconscious.fetch_url", "Fetch URL (model-callable fetch_url tool over the injected page-fetch seam — BrowseComp browse-loop enabler, sibling of web_search, T1.4)",
		func(c *HarnessConfig) bool { return c.Subconscious.FetchURL }, func(c *HarnessConfig, v bool) { c.Subconscious.FetchURL = v }),
	// QUERY-FORMULATION (T1.1): formulate the web_search query from the actual question (strip a leading
	// instruction/wrapper clause) instead of searching the whole goal verbatim — the MEASURED bench fix.
	// optInBoolKnob ⇒ default-OFF excluded from OffPaths() ⇒ the config.load summary + goldens stay byte-
	// identical; pure deterministic string transform, inert unless web_search also fires. Env:
	// THOUGHT_CFG_SUBCONSCIOUS_QUERY_FORMULATION=on.
	optInBoolKnob("subconscious.query_formulation", "Query formulation (search the actual question, not the whole goal — strip the instruction wrapper, T1.1)",
		func(c *HarnessConfig) bool { return c.Subconscious.QueryFormulation }, func(c *HarnessConfig, v bool) { c.Subconscious.QueryFormulation = v }),
	// EDIT-FILE (T1.2): register the model-callable edit_file tool + give expose-affordances the edit_file
	// scope so a mutate-capable sub-agent can surgically str-replace an existing file instead of overwriting
	// it (the str_replace-editor / aider shape). Pure file-op tool (no injected seam, no double-gate);
	// edit_file is already in action.FileModifyTools so the gate-router/sandbox/autopermission treat it like
	// write_file on registration. optInBoolKnob ⇒ default-OFF excluded from OffPaths() ⇒ the config.load
	// summary + goldens stay byte-identical (the tool is unregistered + unscoped when off). Env:
	// THOUGHT_CFG_SUBCONSCIOUS_EDIT_FILE=on.
	optInBoolKnob("subconscious.edit_file", "Edit file (model-callable edit_file str-replace tool — surgical edit of an existing file vs a whole-file overwrite, T1.2)",
		func(c *HarnessConfig) bool { return c.Subconscious.EditFile }, func(c *HarnessConfig, v bool) { c.Subconscious.EditFile = v }),
	// READ-DOCUMENT (T2.3): register the model-callable read_document tool + give expose-affordances the
	// read_document scope so a sub-agent can extract TEXT from a non-plaintext document (PDF/xlsx/docx/…) by
	// shelling out to a host parser (poppler/LibreOffice — the same shape as run_tests shelling pytest), the
	// GAIA-file-task enabler. Pure file-op tool (no injected seam, no double-gate); a READ (inspect/local —
	// not in action.FileModifyTools) so the gate-router/sandbox treat it like read_file on registration.
	// Best-effort: a text file reads directly, a binary type with no parser returns a clear install-this
	// error — never a crash. optInBoolKnob ⇒ default-OFF excluded from OffPaths() ⇒ the config.load summary +
	// goldens stay byte-identical (the tool is unregistered + unscoped + absent from DefaultTools when off).
	// Env: THOUGHT_CFG_SUBCONSCIOUS_READ_DOCUMENT=on.
	optInBoolKnob("subconscious.read_document", "Read document (model-callable read_document shell-out tool — extract text from a PDF/xlsx/docx via a host parser, GAIA file-task enabler, T2.3)",
		func(c *HarnessConfig) bool { return c.Subconscious.ReadDocument }, func(c *HarnessConfig, v bool) { c.Subconscious.ReadDocument = v }),

	// conscious
	boolKnob("conscious.generate", "Generate",
		func(c *HarnessConfig) bool { return c.Conscious.Generate }, func(c *HarnessConfig, v bool) { c.Conscious.Generate = v }),
	boolKnob("conscious.mcp", "Thought MCP",
		func(c *HarnessConfig) bool { return c.Conscious.MCP }, func(c *HarnessConfig, v bool) { c.Conscious.MCP = v }),
	boolKnob("conscious.xref", "Cross-references",
		func(c *HarnessConfig) bool { return c.Conscious.XRef }, func(c *HarnessConfig, v bool) { c.Conscious.XRef = v }),
	boolKnob("conscious.allow_backtrack", "Allow backtrack",
		func(c *HarnessConfig) bool { return c.Conscious.AllowBacktrack }, func(c *HarnessConfig, v bool) { c.Conscious.AllowBacktrack = v }),
	boolKnob("conscious.endogenous_drive", "Endogenous drive (awake)",
		func(c *HarnessConfig) bool { return c.Conscious.EndogenousDrive }, func(c *HarnessConfig, v bool) { c.Conscious.EndogenousDrive = v }),

	// conscious.activity — the Controller's decision thresholds, lifted to tunable knobs (slice (a))
	floatKnob("conscious.activity.done_confidence", "Done confidence",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.DoneConfidence }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.DoneConfidence = v }),
	floatKnob("conscious.activity.flag_threshold", "Flag threshold",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.FlagThreshold }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.FlagThreshold = v }),
	floatKnob("conscious.activity.exhaust_conf", "Exhaust confidence floor",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.ExhaustConf }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.ExhaustConf = v }),
	intKnob("conscious.activity.exhaust_after", "Exhaust after (steps)",
		func(c *HarnessConfig) int { return c.Conscious.Activity.ExhaustAfter }, func(c *HarnessConfig, v int) { c.Conscious.Activity.ExhaustAfter = v }),
	floatKnob("conscious.activity.pursuit_threshold", "Pursuit threshold",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.PursuitThreshold }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.PursuitThreshold = v }),
	intKnob("conscious.activity.max_steps", "Max steps (give-up cap)",
		func(c *HarnessConfig) int { return c.Conscious.Activity.MaxSteps }, func(c *HarnessConfig, v int) { c.Conscious.Activity.MaxSteps = v }),
	floatKnob("conscious.activity.similar_repeat", "Similar-repeat ratio",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.SimilarRepeat }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.SimilarRepeat = v }),
	floatKnob("conscious.activity.merge_threshold", "Merge threshold (Jaccard)",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.MergeThreshold }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.MergeThreshold = v }),
	optInBoolKnob("conscious.activity.soft", "Soft policy (softmax)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.Soft }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.Soft = v }),
	floatKnob("conscious.activity.temperature", "Temperature (τ)",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.Temperature }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.Temperature = v }),
	floatKnob("conscious.activity.branch_propensity", "Branch propensity (β)",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.BranchPropensity }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.BranchPropensity = v }),
	optInBoolKnob("conscious.activity.learn", "Learn (REINFORCE β)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.Learn }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.Learn = v }),
	floatKnob("conscious.activity.learn_rate", "Learn rate (α)",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.LearnRate }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.LearnRate = v }),
	optInBoolKnob("conscious.activity.forest", "Forest rerank (per-branch goal)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.Forest }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.Forest = v }),
	floatKnob("conscious.activity.self_dev_floor", "Self-dev focus floor (μ_min)",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.SelfDevFloor }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.SelfDevFloor = v }),
	optInBoolKnob("conscious.activity.retracement", "Retracement (late-injection re-entry)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.Retracement }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.Retracement = v }),
	optInBoolKnob("conscious.activity.goal_feedback", "Goal feedback (unmeetable → revise)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.GoalFeedback }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.GoalFeedback = v }),
	optInBoolKnob("conscious.activity.drive_agenda", "Drive agenda (mint + conscience-gate)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.DriveAgenda }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.DriveAgenda = v }),
	optInBoolKnob("conscious.activity.seed_intents", "Seed-intent portfolio (standing forest roots)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.SeedIntents }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.SeedIntents = v }),
	intKnob("conscious.activity.seed_intent_count", "Seed-intent set size (kernel-of-3 -> portfolio)",
		func(c *HarnessConfig) int { return c.Conscious.Activity.SeedIntentCount }, func(c *HarnessConfig, v int) { c.Conscious.Activity.SeedIntentCount = v }),
	optInBoolKnob("conscious.activity.experiment", "Activity-θ keep-or-revert (outer loop)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.Experiment }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.Experiment = v }),
	optInBoolKnob("conscious.activity.conscience_ceiling", "Conscience model ceiling",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.ConscienceCeiling }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.ConscienceCeiling = v }),
	optInBoolKnob("conscious.activity.acceptance_ceiling", "Acceptance model ceiling",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.AcceptanceCeiling }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.AcceptanceCeiling = v }),
	optInBoolKnob("conscious.activity.proactive_outreach", "Proactive outreach (reach out unprompted)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.ProactiveOutreach }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.ProactiveOutreach = v }),
	optInBoolKnob("conscious.activity.faculty_scheduler", "Faculty attention scheduler (fair-share)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.FacultyScheduler }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.FacultyScheduler = v }),
	intKnob("conscious.activity.attention_width", "Attention width (W — hot faculties)",
		func(c *HarnessConfig) int { return c.Conscious.Activity.AttentionWidth }, func(c *HarnessConfig, v int) { c.Conscious.Activity.AttentionWidth = v }),
	optInBoolKnob("conscious.activity.rpiv", "RPIV (research->plan->implement->validate)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.RPIV }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.RPIV = v }),
	optInBoolKnob("conscious.activity.autonomous_sense", "Autonomous sense (standing-intent self-sensing)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.AutonomousSense }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.AutonomousSense = v }),
	optInBoolKnob("conscious.activity.route_advisor", "Route advisor (read-only value-routed lane ranking)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.RouteAdvisor }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.RouteAdvisor = v }),
	optInBoolKnob("conscious.activity.inbox_escalation", "Inbox escalation (re-surface ignored outreach)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.InboxEscalation }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.InboxEscalation = v }),
	optInBoolKnob("conscious.activity.awake_user_dispatch", "Awake user-line dispatch (synthesise + engage the subconscious on an awake user turn)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.AwakeUserDispatch }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.AwakeUserDispatch = v }),
	optInBoolKnob("conscious.activity.awake_user_engage", "Awake user-line engagement floor (boost the focused unresolved user line's V(s) so it wins the produce-competition)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.AwakeUserEngage }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.AwakeUserEngage = v }),
	floatKnob("conscious.activity.awake_user_engage_weight", "Awake user-line engagement weight (additive V(s) boost; conservative default 0.5)",
		func(c *HarnessConfig) float64 { return c.Conscious.Activity.AwakeUserEngageWeight }, func(c *HarnessConfig, v float64) { c.Conscious.Activity.AwakeUserEngageWeight = v }),
	optInBoolKnob("conscious.activity.awake_user_engage_judge", "Awake engagement ceiling (Pattern-C: model judges a fuzzy non-task-shaped user line worth engaging the subconscious; requires awake_user_dispatch)",
		func(c *HarnessConfig) bool { return c.Conscious.Activity.AwakeUserEngageJudge }, func(c *HarnessConfig, v bool) { c.Conscious.Activity.AwakeUserEngageJudge = v }),

	// controller (executive / Critic policy)
	optInBoolKnob("controller.active_resource", "Active re-sourcing (low-V(s) -> re-invoke the sourcing ladder)",
		func(c *HarnessConfig) bool { return c.Controller.ActiveResource }, func(c *HarnessConfig, v bool) { c.Controller.ActiveResource = v }),
	optInBoolKnob("controller.answer_verify", "Independent answer-verifier (re-retrieve web evidence before committing a factual answer, T2.1)",
		func(c *HarnessConfig) bool { return c.Controller.AnswerVerify }, func(c *HarnessConfig, v bool) { c.Controller.AnswerVerify = v }),

	// seam
	boolKnob("seam.hidden_filter", "Hidden: Filter",
		func(c *HarnessConfig) bool { return c.Seam.HiddenFilter }, func(c *HarnessConfig, v bool) { c.Seam.HiddenFilter = v }),
	boolKnob("seam.hidden_gate", "Hidden: Gate",
		func(c *HarnessConfig) bool { return c.Seam.HiddenGate }, func(c *HarnessConfig, v bool) { c.Seam.HiddenGate = v }),
	boolKnob("seam.hidden_transform", "Hidden: Transform",
		func(c *HarnessConfig) bool { return c.Seam.HiddenTransform }, func(c *HarnessConfig, v bool) { c.Seam.HiddenTransform = v }),
	boolKnob("seam.assembly", "Context assembly",
		func(c *HarnessConfig) bool { return c.Seam.Assembly }, func(c *HarnessConfig, v bool) { c.Seam.Assembly = v }),
	boolKnob("seam.gate_priors", "Gate priors",
		func(c *HarnessConfig) bool { return c.Seam.GatePriors }, func(c *HarnessConfig, v bool) { c.Seam.GatePriors = v }),
	boolKnob("seam.watched_sync", "Watched: sync",
		func(c *HarnessConfig) bool { return c.Seam.WatchedSync }, func(c *HarnessConfig, v bool) { c.Seam.WatchedSync = v }),
	boolKnob("seam.watched_async", "Watched: async",
		func(c *HarnessConfig) bool { return c.Seam.WatchedAsync }, func(c *HarnessConfig, v bool) { c.Seam.WatchedAsync = v }),
	optInBoolKnob("seam.legible_generation", "Legible gen (shadow)",
		func(c *HarnessConfig) bool { return c.Seam.LegibleGeneration }, func(c *HarnessConfig, v bool) { c.Seam.LegibleGeneration = v }),
	optInBoolKnob("seam.band_pass", "Intake band-pass (LPF·HPF)",
		func(c *HarnessConfig) bool { return c.Seam.BandPass }, func(c *HarnessConfig, v bool) { c.Seam.BandPass = v }),
	floatKnob("seam.band_pass_floor", "Band-pass inject floor",
		func(c *HarnessConfig) float64 { return c.Seam.BandPassFloor }, func(c *HarnessConfig, v float64) { c.Seam.BandPassFloor = v }),
	optInBoolKnob("seam.sufficiency_gate", "Sufficiency gate (CRAG abstain-vs-over-commit)",
		func(c *HarnessConfig) bool { return c.Seam.SufficiencyGate }, func(c *HarnessConfig, v bool) { c.Seam.SufficiencyGate = v }),
	optInBoolKnob("seam.band_pass_coldstart", "Band-pass cold-start fix (first-appearance step-edge)",
		func(c *HarnessConfig) bool { return c.Seam.BandPassColdStart }, func(c *HarnessConfig, v bool) { c.Seam.BandPassColdStart = v }),

	// action
	boolKnob("action.tools", "Real tools",
		func(c *HarnessConfig) bool { return c.Action.Tools }, func(c *HarnessConfig, v bool) { c.Action.Tools = v }),
	boolKnob("action.sandbox", "Sandbox",
		func(c *HarnessConfig) bool { return c.Action.Sandbox }, func(c *HarnessConfig, v bool) { c.Action.Sandbox = v }),
	boolKnob("action.safety_gate", "Safety gate",
		func(c *HarnessConfig) bool { return c.Action.SafetyGate }, func(c *HarnessConfig, v bool) { c.Action.SafetyGate = v }),
	optInBoolKnob("action.gate_router", "Gate-router (op x reach)",
		func(c *HarnessConfig) bool { return c.Action.GateRouter }, func(c *HarnessConfig, v bool) { c.Action.GateRouter = v }),
	// action.auto_permission (SECURITY-SANDBOX, 2026-06-21 — the tiered AUTO-PERMISSION policy). When ON,
	// the executor classifies every tool call SAFE (read-only / in-jail write / allowlisted) ⇒ self-approved
	// with no human prompt (emits action.auto_approve) or DANGEROUS (irreversible / out-of-jail / non-
	// allowlisted / destructive) ⇒ denied + escalated for review (emits action.escalate). optInBoolKnob ⇒
	// its default-OFF state is excluded from OffPaths(), so the config.load summary + every golden stay
	// byte-identical; default OFF ⇒ no classifier, no event ⇒ the gated-executor pipeline is unchanged.
	optInBoolKnob("action.auto_permission", "Auto-permission (SAFE auto-approve / DANGEROUS escalate)",
		func(c *HarnessConfig) bool { return c.Action.AutoPermission }, func(c *HarnessConfig, v bool) { c.Action.AutoPermission = v }),
	// action.auto_permission_config_file (SECURITY-SANDBOX follow-up) — the per-workspace EXTENSIBLE
	// allowlist + pre-auth config file the classifier loads (relative to the workspace, or absolute).
	// Empty default (a string knob ⇒ never in OffPaths()) ⇒ only the curated seed set is SAFE ⇒ goldens
	// byte-identical. Honoured only when action.auto_permission is ON.
	stringKnob("action.auto_permission_config_file", "Auto-permission workspace config file",
		func(c *HarnessConfig) string { return c.Action.AutoPermissionConfigFile },
		func(c *HarnessConfig, v string) { c.Action.AutoPermissionConfigFile = v }),
	// action.auto_permission_pre_auth (SECURITY-SANDBOX follow-up, the L4-autonomy channel) — a
	// comma-separated grant list of specific DANGEROUS classes (e.g. "go run,make,npm install") a human
	// pre-authorizes so the harness self-approves that class. Empty default (a string knob ⇒ never in
	// OffPaths()) ⇒ no class pre-authorized ⇒ escalate-everything-dangerous floor ⇒ goldens byte-identical.
	// EXPLICIT grant only — never a default loosening. Honoured only when action.auto_permission is ON.
	stringKnob("action.auto_permission_pre_auth", "Auto-permission pre-authorized dangerous classes",
		func(c *HarnessConfig) string { return c.Action.AutoPermissionPreAuth },
		func(c *HarnessConfig, v string) { c.Action.AutoPermissionPreAuth = v }),

	// value
	boolKnob("value.signal", "Value signal V(s)",
		func(c *HarnessConfig) bool { return c.Value.Signal }, func(c *HarnessConfig, v bool) { c.Value.Signal = v }),
	boolKnob("value.grounded_reward", "Grounded reward",
		func(c *HarnessConfig) bool { return c.Value.GroundedReward }, func(c *HarnessConfig, v bool) { c.Value.GroundedReward = v }),

	// convert
	boolKnob("convert.specialist_mint", "Specialist mint",
		func(c *HarnessConfig) bool { return c.Convert.PrimitiveSubAgentMint }, func(c *HarnessConfig, v bool) { c.Convert.PrimitiveSubAgentMint = v }),
	boolKnob("convert.skill_mint", "Skill mint",
		func(c *HarnessConfig) bool { return c.Convert.SkillMint }, func(c *HarnessConfig, v bool) { c.Convert.SkillMint = v }),
	boolKnob("convert.gate_prior_mint", "Gate-prior mint",
		func(c *HarnessConfig) bool { return c.Convert.GatePriorMint }, func(c *HarnessConfig, v bool) { c.Convert.GatePriorMint = v }),
	boolKnob("convert.path_mint", "Path mint",
		func(c *HarnessConfig) bool { return c.Convert.PathMint }, func(c *HarnessConfig, v bool) { c.Convert.PathMint = v }),
	optInBoolKnob("convert.eval_gate", "Eval mint gate (does it belong?)",
		func(c *HarnessConfig) bool { return c.Convert.EvalGate }, func(c *HarnessConfig, v bool) { c.Convert.EvalGate = v }),
	optInBoolKnob("convert.refine_loop", "Per-registry refine loop (improve/keep/prune signal)",
		func(c *HarnessConfig) bool { return c.Convert.RefineLoop }, func(c *HarnessConfig, v bool) { c.Convert.RefineLoop = v }),
	optInBoolKnob("convert.skill_reframe", "Skill reframe (prompt + runtime resolve, no goal-match)",
		func(c *HarnessConfig) bool { return c.Convert.SkillReframe }, func(c *HarnessConfig, v bool) { c.Convert.SkillReframe = v }),
	optInBoolKnob("convert.cost_gate", "Cost-aware skill mint (gate growth on the efficiency ruler)",
		func(c *HarnessConfig) bool { return c.Convert.CostGate }, func(c *HarnessConfig, v bool) { c.Convert.CostGate = v }),
	optInBoolKnob("convert.facts", "Convertibility on facts (consolidate high-V recalled facts -> priors)",
		func(c *HarnessConfig) bool { return c.Convert.Facts }, func(c *HarnessConfig, v bool) { c.Convert.Facts = v }),

	// regulator
	boolKnob("regulator.enforce", "Enforce (durability)",
		func(c *HarnessConfig) bool { return c.Regulator.Enforce }, func(c *HarnessConfig, v bool) { c.Regulator.Enforce = v }),
	boolKnob("regulator.scheduler", "LLM scheduler",
		func(c *HarnessConfig) bool { return c.Regulator.Scheduler }, func(c *HarnessConfig, v bool) { c.Regulator.Scheduler = v }),

	// memory
	boolKnob("memory.episodic", "Episodic",
		func(c *HarnessConfig) bool { return c.Memory.Episodic }, func(c *HarnessConfig, v bool) { c.Memory.Episodic = v }),
	boolKnob("memory.semantic", "Semantic",
		func(c *HarnessConfig) bool { return c.Memory.Semantic }, func(c *HarnessConfig, v bool) { c.Memory.Semantic = v }),
	boolKnob("memory.person", "Person",
		func(c *HarnessConfig) bool { return c.Memory.Person }, func(c *HarnessConfig, v bool) { c.Memory.Person = v }),
	boolKnob("memory.recall", "Recall",
		func(c *HarnessConfig) bool { return c.Memory.Recall }, func(c *HarnessConfig, v bool) { c.Memory.Recall = v }),
	boolKnob("memory.reflect", "Reflect",
		func(c *HarnessConfig) bool { return c.Memory.Reflect }, func(c *HarnessConfig, v bool) { c.Memory.Reflect = v }),
	boolKnob("memory.retrieval", "Retrieval",
		func(c *HarnessConfig) bool { return c.Memory.Retrieval }, func(c *HarnessConfig, v bool) { c.Memory.Retrieval = v }),

	// knowledge
	boolKnob("knowledge.registry", "Registry",
		func(c *HarnessConfig) bool { return c.Knowledge.Registry }, func(c *HarnessConfig, v bool) { c.Knowledge.Registry = v }),
	boolKnob("knowledge.ingest", "Ingest",
		func(c *HarnessConfig) bool { return c.Knowledge.Ingest }, func(c *HarnessConfig, v bool) { c.Knowledge.Ingest = v }),
	boolKnob("knowledge.reality_write_back", "Reality write-back",
		func(c *HarnessConfig) bool { return c.Knowledge.RealityWriteBack }, func(c *HarnessConfig, v bool) { c.Knowledge.RealityWriteBack = v }),
	boolKnob("knowledge.distillation", "Distillation",
		func(c *HarnessConfig) bool { return c.Knowledge.Distillation }, func(c *HarnessConfig, v bool) { c.Knowledge.Distillation = v }),

	// representation: moves
	boolKnob("representation.moves.ground", "Move: ground",
		func(c *HarnessConfig) bool { return c.Repr.Moves.Ground }, func(c *HarnessConfig, v bool) { c.Repr.Moves.Ground = v }),
	boolKnob("representation.moves.lift", "Move: lift",
		func(c *HarnessConfig) bool { return c.Repr.Moves.Lift }, func(c *HarnessConfig, v bool) { c.Repr.Moves.Lift = v }),
	boolKnob("representation.moves.reframe", "Move: reframe",
		func(c *HarnessConfig) bool { return c.Repr.Moves.Reframe }, func(c *HarnessConfig, v bool) { c.Repr.Moves.Reframe = v }),
	boolKnob("representation.moves.transcode", "Move: transcode",
		func(c *HarnessConfig) bool { return c.Repr.Moves.Transcode }, func(c *HarnessConfig, v bool) { c.Repr.Moves.Transcode = v }),
	// representation: sources
	boolKnob("representation.sources.present", "Source: present",
		func(c *HarnessConfig) bool { return c.Repr.Sources.Present }, func(c *HarnessConfig, v bool) { c.Repr.Sources.Present = v }),
	boolKnob("representation.sources.knowledge", "Source: knowledge",
		func(c *HarnessConfig) bool { return c.Repr.Sources.Knowledge }, func(c *HarnessConfig, v bool) { c.Repr.Sources.Knowledge = v }),
	boolKnob("representation.sources.memory", "Source: memory",
		func(c *HarnessConfig) bool { return c.Repr.Sources.Memory }, func(c *HarnessConfig, v bool) { c.Repr.Sources.Memory = v }),
	boolKnob("representation.sources.reality", "Source: reality",
		func(c *HarnessConfig) bool { return c.Repr.Sources.Reality }, func(c *HarnessConfig, v bool) { c.Repr.Sources.Reality = v }),
	boolKnob("representation.sources.generated", "Source: generated",
		func(c *HarnessConfig) bool { return c.Repr.Sources.Generated }, func(c *HarnessConfig, v bool) { c.Repr.Sources.Generated = v }),
	// representation: paths
	boolKnob("representation.paths.analogy", "Path: analogy",
		func(c *HarnessConfig) bool { return c.Repr.Paths.Analogy }, func(c *HarnessConfig, v bool) { c.Repr.Paths.Analogy = v }),
	boolKnob("representation.paths.induction", "Path: induction",
		func(c *HarnessConfig) bool { return c.Repr.Paths.Induction }, func(c *HarnessConfig, v bool) { c.Repr.Paths.Induction = v }),
	boolKnob("representation.paths.deduction", "Path: deduction",
		func(c *HarnessConfig) bool { return c.Repr.Paths.Deduction }, func(c *HarnessConfig, v bool) { c.Repr.Paths.Deduction = v }),

	// persistence
	boolKnob("persistence.enabled", "Enabled",
		func(c *HarnessConfig) bool { return c.Persist.Enabled }, func(c *HarnessConfig, v bool) { c.Persist.Enabled = v }),
	boolKnob("persistence.curator", "Curator",
		func(c *HarnessConfig) bool { return c.Persist.Curator }, func(c *HarnessConfig, v bool) { c.Persist.Curator = v }),
	stringKnob("persistence.backend", "Backend",
		func(c *HarnessConfig) string { return c.Persist.Backend }, func(c *HarnessConfig, v string) { c.Persist.Backend = v }),
	// keyframe DB (Track F, F-M7 — loop-closure / recurrence index). OPT-IN: its default-OFF state is
	// excluded from OffPaths(), keeping the all-on config.load summary + goldens byte-identical. It only
	// observes when ON AND a Store is present, so the go-test (nil store) path stays byte-identical.
	optInBoolKnob("persistence.keyframe_db", "Keyframe DB (loop-closure / recurrence)",
		func(c *HarnessConfig) bool { return c.Persist.KeyframeDB }, func(c *HarnessConfig, v bool) { c.Persist.KeyframeDB = v }),

	// ledger (self-change + safety modes)
	boolKnob("ledger.enabled", "Ledger enabled",
		func(c *HarnessConfig) bool { return c.Ledger.Enabled }, func(c *HarnessConfig, v bool) { c.Ledger.Enabled = v }),
	stringKnob("ledger.safety_mode", "Safety mode",
		func(c *HarnessConfig) string { return c.Ledger.SafetyMode }, func(c *HarnessConfig, v string) { c.Ledger.SafetyMode = v }),
	intKnob("ledger.max_entries", "Max ledger entries",
		func(c *HarnessConfig) int { return c.Ledger.MaxEntries }, func(c *HarnessConfig, v int) { c.Ledger.MaxEntries = v }),
	boolKnob("ledger.require_gate", "Require gate for S2/S3",
		func(c *HarnessConfig) bool { return c.Ledger.RequireGate }, func(c *HarnessConfig, v bool) { c.Ledger.RequireGate = v }),
	boolKnob("ledger.auto_snapshot", "Auto-snapshot before S1+",
		func(c *HarnessConfig) bool { return c.Ledger.AutoSnapshot }, func(c *HarnessConfig, v bool) { c.Ledger.AutoSnapshot = v }),
	optInBoolKnob("ledger.selfbench_gate", "SelfBench loop-close (measured delta + durability re-gate + keep-or-revert)",
		func(c *HarnessConfig) bool { return c.Ledger.SelfBenchGate }, func(c *HarnessConfig, v bool) { c.Ledger.SelfBenchGate = v }),
	optInBoolKnob("ledger.selfbench_closed_loop", "SelfBench closed-loop autonomy (self-revert on fail)",
		func(c *HarnessConfig) bool { return c.Ledger.SelfBenchClosedLoop }, func(c *HarnessConfig, v bool) { c.Ledger.SelfBenchClosedLoop = v }),

	// sense (grounded sensing — cognitive power-cycle). DEFAULT-ON (2026-06-20, product go-live). The
	// sensors are DOUBLE-GATED — a knob ON senses only when its seam is wired (clock/host) or a Store is
	// present, so the go-test path (no seam, nil store) stays byte-identical even with the knobs on; the
	// CLI/TUI edge wires the Wall seams (newEngineWith). Turning a knob OFF is the ablation (then in OffPaths).
	boolKnob("sense.clock", "Sense: read_clock (logged percept)",
		func(c *HarnessConfig) bool { return c.Sense.Clock }, func(c *HarnessConfig, v bool) { c.Sense.Clock = v }),
	boolKnob("sense.orient", "Sense: orientation pass (re-ground on resume)",
		func(c *HarnessConfig) bool { return c.Sense.Orient }, func(c *HarnessConfig, v bool) { c.Sense.Orient = v }),
	boolKnob("sense.host", "Sense: read_host (process footprint)",
		func(c *HarnessConfig) bool { return c.Sense.Host }, func(c *HarnessConfig, v bool) { c.Sense.Host = v }),
	boolKnob("sense.event_log", "Sense: read_event_log (own event ring)",
		func(c *HarnessConfig) bool { return c.Sense.EventLog }, func(c *HarnessConfig, v bool) { c.Sense.EventLog = v }),
	// fetch_web (follow-up #15 — the OUTWARD distal sense) is the EXPLICIT EXCEPTION: it DEFAULTS OFF (an
	// OUTWARD network read touches the network + costs, opt-in + budgeted per Fork 2), so it is an
	// optInBoolKnob — its default-OFF state is excluded from OffPaths(), keeping the config.load summary +
	// goldens byte-identical. Addressable like any knob (CLI/env/TUI can flip it on).
	optInBoolKnob("sense.web", "Sense: fetch_web (outward web/news percept)",
		func(c *HarnessConfig) bool { return c.Sense.Web }, func(c *HarnessConfig, v bool) { c.Sense.Web = v }),
	// sense.self_model (SELF-MODEL — the baseline DECLARATIVE self-model). When ON in the awake loop and a
	// standing INTROSPECTIVE seed root holds focus, the engine injects a small STANDING CORE thought (its
	// identity + a bounded, constant-size CAPABILITY INDEX read from the real registries + runtime facts)
	// and emits perception.self_model; per-capability DETAIL is pulled LAZILY on demand (SelfModelLookup),
	// never eagerly dumped. A single μ-baseline percept APPEND (n unchanged — the #18 self-watch cell
	// re-gates it). optInBoolKnob => its default-OFF state is excluded from OffPaths(), so the config.load
	// summary + every golden stay byte-identical; default OFF => no self-model thought, no event.
	optInBoolKnob("sense.self_model", "Sense: declarative self-model (identity + capability index + runtime; lazy detail)",
		func(c *HarnessConfig) bool { return c.Sense.SelfModel }, func(c *HarnessConfig, v bool) { c.Sense.SelfModel = v }),

	// conformance (L0 conformance instrument — Track H, benchmark-taxonomy §1/§5). An opt-in MEASUREMENT
	// instrument, NOT a cognitive faculty: SelfCheck ON installs the passive wiring-coverage tap on the
	// engine's own bus + lets EmitWiringScan emit conformance.wiring. optInBoolKnob ⇒ its default-OFF state
	// is excluded from OffPaths(), so the config.load summary + every golden stay byte-identical.
	optInBoolKnob("conformance.self_check", "Conformance self-check (wiring-coverage tap)",
		func(c *HarnessConfig) bool { return c.Conformance.SelfCheck }, func(c *HarnessConfig, v bool) { c.Conformance.SelfCheck = v }),

	// dev-side auto-dev (Track O — the plan-carries-a-falsifiable-gate symbol-audit, O-2). An opt-in
	// DEV-PROCESS knob (gates the keep step, NOT the cognition tick), so default-OFF is excluded from
	// OffPaths() and the runtime goldens stay byte-identical — `thought plangate` runs the audit only
	// when this is on, and is a no-op PASS (config.skip) otherwise.
	optInBoolKnob("dev.plan_gate", "Dev: plan-gate (producers/acceptance symbol-audit before keep)",
		func(c *HarnessConfig) bool { return c.Dev.PlanGate }, func(c *HarnessConfig, v bool) { c.Dev.PlanGate = v }),

	// slam (SLAM self-state estimator — Track F / M1, docs/internal/notes/2026-06-20-slam-M1-build-spec.md). An
	// opt-in CALIBRATION instrument, NOT a cognitive faculty: Innovation ON runs the explicit scalar-
	// Kalman measurement update on the action->reality path (control.Innovate) with the FEJ-anchored
	// trust rule + Mahalanobis gate, emitting estimate.*; OFF ⇒ the estimator is inert ⇒ no event ⇒
	// byte-identical. optInBoolKnob ⇒ its default-OFF state is excluded from OffPaths(), so the
	// config.load summary + every golden stay byte-identical. Env: THOUGHT_CFG_SLAM_INNOVATION=on.
	optInBoolKnob("slam.innovation", "SLAM: innovation + FEJ-anchored Filter (action->reality residual)",
		func(c *HarnessConfig) bool { return c.Slam.Innovation }, func(c *HarnessConfig, v bool) { c.Slam.Innovation = v }),

	// slam.calibration (SLAM M9 calibration meta-estimator — Track F / G9, docs/internal/2026-06-20-slam-
	// self-state-estimation.md §3b.3 #5). An opt-in CALIBRATION-of-the-calibrator instrument: ON it LEARNS
	// each source's reliability per trust tier from the M1 predicted-vs-actual residual stream and
	// RE-ESTIMATES the measurement precision R the innovation update uses (the lever on the measured
	// same-model self-judging ceiling), emitting estimate.calibrate; OFF ⇒ the calibrator is inert ⇒ no
	// event, no re-weighting (the estimator uses the fixed TierPrecision prior exactly) ⇒ byte-identical.
	// It REQUIRES slam.innovation (it consumes that residual stream) — the engine only calibrates when
	// BOTH are on. optInBoolKnob ⇒ default-OFF excluded from OffPaths(). Env: THOUGHT_CFG_SLAM_CALIBRATION=on.
	optInBoolKnob("slam.calibration", "SLAM: calibration meta-estimation (learn R per source/tier; requires slam.innovation)",
		func(c *HarnessConfig) bool { return c.Slam.Calibration }, func(c *HarnessConfig, v bool) { c.Slam.Calibration = v }),

	// slam.consistency (SLAM M5 consistency/observability invariant — Track F / M5, docs/internal/2026-06-20-
	// slam-self-state-estimation.md §4 P2/P3 + §5 #7 + §5b). The AWAKE-DURABILITY gate requirement: a
	// failable WITNESS that the self-estimator gains NO spurious information in unobservable directions (the
	// Huang-2010 EKF-inconsistency overconfidence that compounds over a long awake run). ON it accounts every
	// belief-variance reduction as grounded (an associated Observe()) vs spurious (a self-restatement or a
	// gated obs that lowered variance) and emits estimate.consistency (consistent iff spuriousGain==0); OFF ⇒
	// no accounting ⇒ no event ⇒ byte-identical. It REQUIRES slam.innovation (it monitors that update's
	// variance trajectory) — the engine only monitors when BOTH are on. Pure CONTROL (closed-form accounting,
	// no model), a pure witness that never alters the estimate. optInBoolKnob ⇒ default-OFF excluded from
	// OffPaths(). Env: THOUGHT_CFG_SLAM_CONSISTENCY=on.
	optInBoolKnob("slam.consistency", "SLAM: consistency/observability invariant (no spurious info gain; awake-durability gate; requires slam.innovation)",
		func(c *HarnessConfig) bool { return c.Slam.Consistency }, func(c *HarnessConfig, v bool) { c.Slam.Consistency = v }),

	// slam.covariance (SLAM M2 sparse-covariance / Information layer — Track F / M2, docs/internal/2026-06-20-
	// slam-self-state-estimation.md §3b.3 #2 + §6 M2). The off-diagonal structure SLAM exists for: ON it
	// records WHICH beliefs co-vary (share a grounding upstream) and, on a grounded REFUTATION, propagates a
	// correlated loss-of-certainty (variance INFLATION) to the co-varying siblings — catching CORRELATED
	// self-deception (two beliefs confidently wrong because one bad upstream) that no per-belief scalar can
	// see ("the correlations ARE the information", Thm 2), emitting estimate.correlate; OFF ⇒ no correlation
	// graph, no propagation, no event ⇒ byte-identical. The graph stays SPARSE (only beliefs sharing an
	// upstream get an edge), never the dense O(n^2) filter form; a propagation only RAISES variance so it
	// stays inside the §0/M5 consistency invariant. It REQUIRES slam.innovation (it correlates that update's
	// variance trajectory) — the engine only correlates when BOTH are on. Pure CONTROL (no model).
	// optInBoolKnob ⇒ default-OFF excluded from OffPaths(). Env: THOUGHT_CFG_SLAM_COVARIANCE=on.
	optInBoolKnob("slam.covariance", "SLAM: sparse belief covariance / Information layer (correlated self-deception detection; requires slam.innovation)",
		func(c *HarnessConfig) bool { return c.Slam.Covariance }, func(c *HarnessConfig, v bool) { c.Slam.Covariance = v }),

	// slam.infogain (SLAM M6 active-inference info-gain / next-best-observation — Track F / M6, docs/internal/
	// 2026-06-20-slam-self-state-estimation.md §3b.3 #7 + §5 #4 + §6 M6). The principled explore/exploit
	// term: ON it RANKS the live tracked beliefs by expected JOINT information gain (a belief's own variance
	// AND its correlation reach across co-varying siblings, via control.ExpectedInfoGain) and surfaces the
	// one whose grounding reduces the most uncertainty — the active-SLAM NEXT-BEST-OBSERVATION ("what to
	// verify next"), directing grounding by expected uncertainty reduction (not just outcome reward) at the
	// measured under-grounding / give-up behaviour, emitting estimate.infogain; OFF ⇒ no ranking, no event ⇒
	// byte-identical. PURE RANKING (no model) — it reads the variance trajectory and never alters it, so it
	// stays inside the §0/M5 consistency invariant (it DIRECTS the grounding that legitimately shrinks a
	// variance; it never shrinks one itself). It REQUIRES slam.innovation (it ranks that update's variance
	// trajectory) — the engine only ranks when BOTH are on. optInBoolKnob ⇒ default-OFF excluded from
	// OffPaths(). Env: THOUGHT_CFG_SLAM_INFOGAIN=on.
	optInBoolKnob("slam.infogain", "SLAM: active-inference info-gain / next-best-observation (directed grounding; requires slam.innovation)",
		func(c *HarnessConfig) bool { return c.Slam.InfoGain }, func(c *HarnessConfig, v bool) { c.Slam.InfoGain = v }),

	// slam.staleness (SLAM M4 freshness / staleness-decay — Track F / M4, docs/internal/2026-06-20-slam-self-
	// state-estimation.md §4 P4 + §3b.2 + §6 M4). The dynamic-map process noise: ON, each tick the estimator
	// GROWS every grounded belief's variance back toward the prior ceiling as a function of its un-refreshed
	// AGE (control.StalenessInflation) — a belief grounded long ago, left un-refreshed, decays toward "stale,
	// re-observe", forcing re-grounding of a fact the moving world may have changed (Q>0); OFF ⇒ no decay
	// sweep, no estimate.decay event ⇒ byte-identical. Pure CONTROL (no model). Decay only RAISES variance
	// (loses certainty), so it stays inside the §0/M5 consistency invariant (admitting staleness can never be
	// spurious information). It REQUIRES slam.innovation (it decays that update's variance trajectory) — the
	// engine only decays when BOTH are on. optInBoolKnob ⇒ default-OFF excluded from OffPaths(). Env:
	// THOUGHT_CFG_SLAM_STALENESS=on.
	optInBoolKnob("slam.staleness", "SLAM: freshness / staleness decay (Q>0; belief variance grows with un-refreshed age; requires slam.innovation)",
		func(c *HarnessConfig) bool { return c.Slam.Staleness }, func(c *HarnessConfig, v bool) { c.Slam.Staleness = v }),
	// slam.staleness_q — the per-tick process-noise RATE in [0,1] the M4 decay uses (the fraction of the
	// remaining gap to the prior ceiling a grounded belief loses per un-refreshed tick). 0 = stationary (no
	// decay even with slam.staleness on); higher = decays faster (a fast-drift world). Default a small
	// slow-drift rate so a belief stays usefully fresh for a few ticks but measurably decays over an idle
	// stretch. A plain tunable (not an opt-in gate): the slam.staleness knob is what enables the layer.
	floatKnob("slam.staleness_q", "SLAM: staleness process-noise rate Q (per-tick decay fraction, [0,1])",
		func(c *HarnessConfig) float64 { return c.Slam.StalenessQ }, func(c *HarnessConfig, v float64) { c.Slam.StalenessQ = v }),

	// tui — post-session ANALYSIS-surface instrumentation (Track G). Pure observability sidecar, not a
	// cognition change: opt-in, default OFF (excluded from OffPaths so config.load stays byte-identical).
	optInBoolKnob("tui.signal_frames", "SignalFrame sidecar (per-tick vitals vector → *.signals.jsonl)",
		func(c *HarnessConfig) bool { return c.Tui.SignalFrames }, func(c *HarnessConfig, v bool) { c.Tui.SignalFrames = v }),
	// tui.session_record (Track G, G1). The live freeze tap is a passive bounded ring off the event bus
	// that lets ^P + the analysis surface reconstruct a real session record of the running mind.
	// optInBoolKnob ⇒ default-OFF state excluded from OffPaths(), keeping the config.load summary + every
	// golden byte-identical (default OFF ⇒ no ring allocated, no capture ⇒ the surface renders the sample).
	// Env: THOUGHT_CFG_TUI_SESSION_RECORD=on.
	optInBoolKnob("tui.session_record", "ANALYSIS: live freeze tap (record the running session for ^P analysis)",
		func(c *HarnessConfig) bool { return c.Tui.SessionRecord }, func(c *HarnessConfig, v bool) { c.Tui.SessionRecord = v }),
	// tui.compare_load (Track G, G2 — the benchmark LOAD). When on, the analysis surface's COMPARE loads
	// the two most recent recorded session logs from disk (newest = A, next = B) so the user benchmarks two
	// REAL recorded runs (the power-ON/OFF DIFF). optInBoolKnob ⇒ default-OFF excluded from OffPaths(); a
	// default-OFF run never enumerates runs (COMPARE keeps the frozen-A/sample-B prototype) ⇒ byte-identical.
	// Env: THOUGHT_CFG_TUI_COMPARE_LOAD=on.
	optInBoolKnob("tui.compare_load", "ANALYSIS: COMPARE loads the two most recent recorded runs from disk (power-ON/OFF benchmark)",
		func(c *HarnessConfig) bool { return c.Tui.CompareLoad }, func(c *HarnessConfig, v bool) { c.Tui.CompareLoad = v }),
	// tui.registry_heatmap (Track G, G3 — the registry/memory FAMILY view). When on, the analysis
	// surface's REGISTRIES tab renders the §6 coldness-vs-topics HEAT MAP (one row per learned item,
	// coloured by use) + the mint/demote evidence LEDGER, reconstructed off the recorded event stream.
	// optInBoolKnob ⇒ default-OFF excluded from OffPaths(); a default-OFF run keeps the REGISTRIES tab's
	// "panel pending" placeholder (the heat map is never shown) ⇒ byte-identical. Pure observability.
	// Env: THOUGHT_CFG_TUI_REGISTRY_HEATMAP=on.
	optInBoolKnob("tui.registry_heatmap", "ANALYSIS: registry/memory FAMILY heat map + mint/demote ledger (coldness-vs-topics §6)",
		func(c *HarnessConfig) bool { return c.Tui.RegistryHeatmap }, func(c *HarnessConfig, v bool) { c.Tui.RegistryHeatmap = v }),
	// tui.deep_ledgers (Track G, G4 — the DEEP ledgers + tree). When on, the analysis surface's four
	// remaining tabs render: CONSCIOUS thought tree + compression history (§5), ACTION·GROUNDING ledger
	// + SESSIONS·SUB-AGENTS spawn tree (§7), THROUGHPUT per-role/tier spend (§8), SELF·EVOLUTION change
	// ledger (§9) — all reconstructed Pattern-A off the recorded event stream. optInBoolKnob ⇒ default-OFF
	// excluded from OffPaths(); a default-OFF run keeps each tab's "panel pending" placeholder (the deep
	// panels are never shown) ⇒ byte-identical. Pure observability. Env: THOUGHT_CFG_TUI_DEEP_LEDGERS=on.
	optInBoolKnob("tui.deep_ledgers", "ANALYSIS: deep ledgers + tree — CONSCIOUS tree/compression · ACTION/SESSIONS · THROUGHPUT · SELF (§5/§7/§8/§9)",
		func(c *HarnessConfig) bool { return c.Tui.DeepLedgers }, func(c *HarnessConfig, v bool) { c.Tui.DeepLedgers = v }),
	// tui.trace_flow (Track G, G6 — the TRACE/FLOW swimlane). When on, the analysis surface's TRACE tab
	// renders the seed->thought->seam->subconscious->action ROUND-TRIP as a swimlane timeline (lanes
	// PORT/CONSCIOUS/SEAM/SUBCONSCIOUS/ACTION, X = ticks) over the loaded record's events, with the
	// late-injection/Reenter DESYNC markers highlighted and a PHASE/FREQ readout (trip length, retracement
	// count, land->deliver lag, θ/cadence). optInBoolKnob ⇒ default-OFF excluded from OffPaths(); a
	// default-OFF run keeps the TRACE tab's "panel pending" placeholder (the swimlane is never shown) ⇒
	// byte-identical. Pure observability. Env: THOUGHT_CFG_TUI_TRACE_FLOW=on.
	optInBoolKnob("tui.trace_flow", "ANALYSIS: TRACE/FLOW swimlane — seed->thought->seam->subconscious->action round-trip + phase/freq readout (§G6)",
		func(c *HarnessConfig) bool { return c.Tui.TraceFlow }, func(c *HarnessConfig, v bool) { c.Tui.TraceFlow = v }),

	// selfbench (the self-benchmark loop primitive, Track H SB0). DEFAULTS OFF (opt-in instrument): a
	// self-bench runs real episodes on a SHADOW engine loaded from a frozen checkpoint, so it never rides
	// the default tick. Default-OFF state is excluded from OffPaths() (opt-in), keeping the config.load
	// summary + goldens byte-identical. Flip on to have the engine self-benchmark its checkpoint at IDLE
	// consolidation (propose-and-gate; it measures, never self-commits).
	optInBoolKnob("selfbench.enabled", "Self-bench loop (benchmark a frozen checkpoint on a shadow engine)",
		func(c *HarnessConfig) bool { return c.SelfBench.Enabled }, func(c *HarnessConfig, v bool) { c.SelfBench.Enabled = v }),

	// introspective-faithfulness self-report instrument (Track H §8). An opt-in MEASUREMENT/safety instrument
	// (NOT a cognitive faculty): SelfReport ON assembles a self-report of the readable layers + checks each
	// field against its ground truth + emits introspect.faithfulness, which would change the event stream. So
	// it is an optInBoolKnob — its default-OFF state is excluded from OffPaths(), keeping the config.load
	// summary + goldens byte-identical. Addressable like any knob (CLI/env/TUI can flip it on).
	optInBoolKnob("introspect.self_report", "Introspect: faithful self-report (readable layers + honest 'can't see that')",
		func(c *HarnessConfig) bool { return c.Introspect.SelfReport }, func(c *HarnessConfig, v bool) { c.Introspect.SelfReport = v }),
	// introspect.suite (Track H, benchmark-taxonomy §8 + §7.6 #5 — the STANDING introspective-faithfulness
	// SUITE; H-SB3). When on, the engine at quiescence runs a fixed SET of self-report probes (conscious
	// thought / reasoning-why / state-confidence / honest-decline of the opaque subconscious) against the
	// addressable ground truth and emits the rolled-up introspect.suite verdict — so "is its self-model
	// honest?" is a first-class, repeatable check. DISTINCT from introspect.self_report (the single-shot
	// witness): this is the standing SUITE. optInBoolKnob ⇒ its default-OFF state is excluded from
	// OffPaths(), keeping the config.load summary + goldens byte-identical; default OFF ⇒ no probes, no event.
	optInBoolKnob("introspect.suite", "Introspection: standing self-report faithfulness suite (§8)",
		func(c *HarnessConfig) bool { return c.Introspect.Suite }, func(c *HarnessConfig, v bool) { c.Introspect.Suite = v }),

	// flywheel — the OFFLINE-RL DATA FLYWHEEL (Track C, RL roadmap §6 P0). An opt-in CAPTURE instrument, NOT
	// a faculty/learner: with it OFF the engine builds no Recorder, captures nothing, writes no dataset, and
	// emits no flywheel.* event ⇒ byte-identical. optInBoolKnob ⇒ its default-OFF is excluded from OffPaths()
	// so the config.load summary + goldens stay byte-identical. With it ON the engine logs a per-decision
	// (state, action, GROUNDED outcome) tuple to an append-only dataset for a later offline learner.
	optInBoolKnob("flywheel.capture", "Flywheel: capture per-decision (state,action,grounded-outcome) training tuples (offline-RL P0)",
		func(c *HarnessConfig) bool { return c.Flywheel.Capture }, func(c *HarnessConfig, v bool) { c.Flywheel.Capture = v }),

	// tui — runtime-monitor / analysis-surface CUSTOMIZATION (Track G, G5). Pure View-layer observability,
	// not a cognition change: the master gate is opt-in (default OFF) so it is excluded from OffPaths() and
	// a default surface stays byte-identical. With it ON the operator chooses which `^O` panels show + their
	// order (tui.pullup_order) and the per-panel strip horizon (tui.strip_horizon); the analysis tabs derive
	// from the SAME panel registry.
	optInBoolKnob("tui.pullup.panels", "ANALYSIS: customize the ^O monitor pull-up (which panels + order, shared by the analysis tabs)",
		func(c *HarnessConfig) bool { return c.Tui.PullupPanels }, func(c *HarnessConfig, v bool) { c.Tui.PullupPanels = v }),
	// tui.pullup_order — the persisted, ordered panel-ID set the customized pull-up shows (canon IDs in
	// config.PanelRegistry). A comma-joined string on the CLI/env surface (e.g. VITALS,LOOP,SEAM); the
	// resolver drops unknown IDs + collapses duplicates. Empty ⇒ the full canon order. Honoured only when
	// tui.pullup.panels is ON. Env: THOUGHT_CFG_TUI_PULLUP_ORDER=VITALS,LOOP,...
	stringKnob("tui.pullup_order", "ANALYSIS: chosen ^O panel order (comma-joined canon IDs)",
		func(c *HarnessConfig) string { return strings.Join(c.Tui.PullupOrder, ",") },
		func(c *HarnessConfig, v string) { c.Tui.PullupOrder = splitPanelOrder(v) }),
	// tui.strip_horizon — the per-panel rolling-strip window depth (the `^O` strips' history depth). 0 ⇒ the
	// locked default (config.DefaultStripHorizon); Validate clamps a non-zero value to [Min, Max]StripHorizon.
	// Honoured only when tui.pullup.panels is ON. Env: THOUGHT_CFG_TUI_STRIP_HORIZON=80.
	intKnob("tui.strip_horizon", "ANALYSIS: per-panel strip horizon (rolling-window depth)",
		func(c *HarnessConfig) int { return c.Tui.StripHorizon }, func(c *HarnessConfig, v int) { c.Tui.StripHorizon = v }),
}

// splitPanelOrder parses the comma-joined panel-order string form (the CLI/env surface) into a slice,
// trimming + dropping empties. Validation (unknown-ID drop, dedupe) is the resolver's job, so this only
// tokenizes — keeping the string knob a pure transport and the customization THINKING in one place.
func splitPanelOrder(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Knobs returns the canonical knob table (the one place a toggle is declared). The returned slice is
// the shared table — callers read it, never mutate it.
func Knobs() []Knob { return knobTable }
