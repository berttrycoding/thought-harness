package tui

// msgs.go — the typed tea.Msg structs the bridge and app fold in single-writer (DESIGN §4.1, §4.5).
// Every cross-goroutine result re-enters Update as one of these, so state is mutated only on the
// Update loop. The off-loop step (which may block on a slow model call) and the bus event pump both
// deliver their results here.

import (
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
	"github.com/berttrycoding/thought-harness/internal/tui/popup"
)

// stepResultMsg carries the outcome of one off-loop engine.Step() (DESIGN §4.5 primitive 1). The
// step runs in a tea.Cmd goroutine because it may block on a slow LLM call; the result re-enters
// Update here. epoch is the modeEpoch stamped at dispatch — Update DROPS the message when
// epoch != app.modeEpoch (a stale result after a Shift+Tab mode switch). snap is the end-of-tick
// engine snapshot the bridge built off the read accessors (DESIGN §4.3), folded into the panels.
type stepResultMsg struct {
	res   engine.StepResult
	snap  cognition.SnapshotData
	epoch int
}

// eventBatchMsg carries a batch of bus events drained from the event-pump channel (DESIGN §4.5
// primitive 3). The bus is synchronous and the step runs off-loop, so ordering within a batch is the
// engine's deterministic emission order. Update folds these into the panel ViewModel and the chat,
// then re-issues the pump Cmd.
type eventBatchMsg struct {
	events []events.Event
}

// stepTickMsg is the self-rescheduling stepping interval (DESIGN §4.5 primitive 2). On each tick, if
// no step is in flight and the lifecycle is steppable, the app dispatches a step; it always re-arms
// the tick. The interval keeps idle CPU flat (work is only dispatched while the engine is active).
type stepTickMsg struct{}

// animTickMsg is the self-rescheduling animation frame clock (~30fps). On each tick the app advances
// a.frame and re-arms the tick ONLY while an animation is active (App.animating) — so idle CPU stays
// flat: a fully-idle cognition surface with no events and the welcome dismissed stops ticking
// entirely, and a state change that starts an animation revives it via ensureAnim.
type animTickMsg struct{}

// ConvPair is one queued conversation line — a (role, text) the bridge drained off the bus (Python's
// (role, text) tuple). role is one of the theme.Roles keys (user/harness/action/sys).
type ConvPair struct {
	Role string
	Text string
}

// branchResolvedMsg carries the git branch resolved once (off-loop) at startup — the chrome's
// branch label. Empty when not a git repo / resolution failed (the renderers drop an empty branch).
type branchResolvedMsg struct{ branch string }

// loadProgressMsg carries one progress line from the LM Studio auto-loader, drained off the
// llm.SetAutoLoadLog sink (off-loop) into the welcome screen's substrate-loading status. text is the
// latest line ("LM Studio: loading qwen/… (1.0 GB)…"); an empty text is the sentinel that unblocks
// the drain when resolution completes. See App.loadProgressCmd.
type loadProgressMsg struct {
	text string
}

// engineRebuiltMsg signals that the bridge has (re)built the engine after a config change (Python
// EngineBridge.apply/reset). err is the reason there is NO engine when the rebuild failed (the
// harness has no offline path — DESIGN §8, llm.ResolveSubstrate). The app re-arms its stepping tick
// on success or shows the "configure a model" state on failure.
type engineRebuiltMsg struct {
	eng *engine.Engine // the freshly-built engine to swap in (on the Update loop), nil on failure
	ok  bool
	err string
}

// modelsListedMsg carries the local LLM menu the bridge fetched off-loop (`lms ls`), folded back so the
// App can open the Model picker. err is set (and models nil) when the list could not be fetched.
type modelsListedMsg struct {
	models []popup.ModelChoice
	err    string
}

// switchPlanMsg carries the memory picture the bridge gathered for a switch (`lms ps` + the target's
// estimate), folded back so the App can open the plan screen where the user chooses what to unload.
type switchPlanMsg struct {
	target       string
	targetDetail string
	note         string
	rows         []popup.LoadedRow
	err          string
}

// modelSwitchedMsg carries the result of applying a switch off-loop (unload chosen + load target).
// served is the served /v1 id on success; err is the reason on failure. The App rebuilds onto `served`.
type modelSwitchedMsg struct {
	key    string
	served string
	err    string
}

// cogModelsListedMsg carries the saved cognition-model version rows the bridge gathered off-loop
// (persist.ListSnapshots + a structural DiffSnapshots vs the baseline), folded back so the App can open
// (or refresh) the Cognition Models popup. baseline is the effective baseline name; substrate is the
// running brain's lineage tag. err is set (and rows nil) when the store could not be read. open is true
// when this list result should OPEN the popup (the /models entry), false when it only refreshes an
// already-open one after an action.
type cogModelsListedMsg struct {
	rows      []popup.CogModelRow
	baseline  string
	substrate string
	open      bool
	err       string
}

// cogModelActionMsg carries the result of one cognition-model action the bridge applied off-loop
// (save/reset/baseline/delete) or a completed diff. note is the human-readable status line the popup
// renders under the list (e.g. "diff cold-baseline → curious-v1: +6 skills"); err is the failure reason.
// On a non-error action the App re-lists (refresh) so the rows/deltas reflect the change.
type cogModelActionMsg struct {
	note string
	err  string
}
