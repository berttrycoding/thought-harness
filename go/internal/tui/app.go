package tui

// app.go — the root Bubble Tea model (DESIGN §4.1). One pointer-receiver aggregate, lathe-style: it
// OWNS the EngineBridge, the bubbles textarea(input) + viewport, the chat HeaderBar/FooterBar/ChatView,
// the cognition Dashboard (driven by a ViewModel folded from a SnapshotData + the event stream), and
// the four popup sub-models. State is single-writer — only Update mutates — and a modeEpoch is stamped
// at every mode switch so a stale async step result (from a slow off-loop Step) is dropped after a
// Shift+Tab (the options stale-epoch discipline).
//
// Update is a strict MODAL-PRIORITY chain (DESIGN §4.1): ctrl+c quits first; then if any popup is
// Visible() the key is routed to it (so Shift+Tab is the popup's prev-item there, NOT a mode switch);
// else Shift+Tab toggles the mode (+modeEpoch, recompute layout); else the global keys (^k command,
// ^b panel/mode, etc.); else the active mode handles it. The off-loop step result (stepResultMsg) and
// the bus event batch (eventBatchMsg) are folded in single-writer here too.
//
// View switches on mode → viewChat (lathe vertical-concat, §4.2) / viewCognition (options Dashboard,
// §4.3), returning a centered popup early when a full-screen one is open. The engine stays
// headless-pure throughout: the App reads it only through the bridge's read accessors + the bus.

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/tui/chat"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
	"github.com/berttrycoding/thought-harness/internal/tui/popup"
)

// Version is the harness version shown in the header's default title.
const Version = "0.1.0"

// eventRingMax bounds the App's own copy of the recent event stream (the cognition panels' trace tail
// + the seam/critic/value/subconscious scans read off it). It mirrors the bus replay ring's order
// (oldest-first) but is trimmed to what the panels actually scan (Python panels read eng.bus.log).
const eventRingMax = 512

// borderAnimFrames is the length (in ~30fps animation frames) of a panel's border-transition
// animation. ~18 frames ≈ 0.6s — a frame-eased state-transition cue on the panel border.
const borderAnimFrames = 18

// inputBoxStyle is the square NormalBorder box wrapped around the textarea each frame (lathe input box;
// the Confirm popup replaces it in place). WHITE border (every border is white).
var inputBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("#ffffff"))

// App is the root tea.Model — the single aggregate that owns every sub-widget and folds cross-goroutine
// results in via typed tea.Msg (DESIGN §4.1).
type App struct {
	mode      Mode // the primary surface (CHAT / COGNITION)
	modeEpoch int  // stamped at each switch; a stale stepResultMsg (epoch mismatch) is dropped
	w, h      int  // terminal dimensions (set on WindowSizeMsg)
	ready     bool // true once the first WindowSizeMsg has sized the layout

	// the engine bridge (the headless-pure engine lives behind this — the App never touches engine
	// internals directly; it reads through the bridge's accessors + the bus).
	bridge   *EngineBridge
	stepping bool // a step goroutine is in flight (guards against overlapping steps)

	// cfg is the engine build config this session was started with — the base the Settings popup edits
	// against (applySettings folds the changed knobs back: mode/cognition live, the rest via a rebuild).
	cfg engine.EngineConfig

	// sessionID is a short per-launch id shown in the chrome header (set once in NewApp).
	sessionID string

	// initialPrompt is the `tui --prompt` opener: submitted ONCE on mount (in Init when the engine is
	// already live, else deferred until the async substrate resolve attaches one). Mirrors Python
	// ChatHarnessApp.on_mount's `if self._prompt: self.engine.submit(self._prompt)`. "" = no opener.
	initialPrompt string

	// substrate auto-load (the product path resolves the thinking substrate — and auto-loads a local
	// LM Studio model if none is loaded — AFTER the TUI is up, on the welcome screen, so the launch is
	// never blocked on a model probe/load). loadCh receives the auto-loader's progress lines off-loop;
	// loadStatus is the latest line shown on the welcome card; substrateLoading is true while the
	// resolve Cmd is in flight; loadErr is set (and shown with a /settings hint) when it failed.
	loadCh           chan string
	loadStatus       string
	substrateLoading bool
	loadErr          string

	// CHAT mode widgets (lathe layout).
	input    textarea.Model
	viewport viewport.Model
	chatView chat.ChatView
	header   chat.HeaderBar
	footer   chat.FooterBar

	// COGNITION mode — a set of full-screen tabs (cogtabs.go), driven by a folded ViewModel.
	vm        cognition.ViewModel // the per-frame snapshot (SnapshotData + recent events) the panels read
	cogTab    int                 // the active COGNITION tab (Tab cycles; 0 = Overview, the full grid)
	cogScroll int                 // vertical scroll offset within the active COGNITION tab
	cogRegSel int                 // the selected registry on the Registry tab (↑↓ picks; index into cognitionTabs' registry sections)
	cogCfgSel int                 // the selected SECTION on the Config tab (Tab/←→ picks)
	cogCfgRow int                 // the selected ROW within the Config section (↑↓ picks; Space flips)

	// The Config/Registry views read LIVE engine maps; the off-loop Step() mutates those same maps. To
	// avoid a concurrent map read/write (D2), the View reads these CACHES instead — rebuilt in Update by
	// refreshCogCaches only when NO step is in flight (the engine is then quiescent). The cache is the
	// last consistent picture; a step's mints/flips show on the next refresh (stepResult).
	cfgCache cognition.ConfigView
	regCache cognition.RegistryCatalog

	// paused stops the stepping loop dispatching new engine steps (^P). The in-flight step (if any)
	// completes and folds; nothing new starts until resumed. The awake mind's brake (P1).
	paused bool
	// userEngaged latches true on the FIRST user input (a typed message or a --prompt opener). Until then,
	// an awake/continuous session starts PAUSED on launch — it does not auto-spin; the first input un-pauses
	// it, and thereafter the user controls the loop with ^P. (set in onEngineRebuilt / submitInput.)
	userEngaged bool

	// pullup toggles the runtime MONITOR overlay (^O): the live validation-instrument stack rendered
	// over the current surface from the end-of-tick snapshot (W2 — the two-mode monitor surface).
	pullup bool
	// pullupScroll is the top-line offset into the (taller-than-screen) monitor stack while the pull-up
	// is open. The Update-side handler owns the bounds (clampPullupScroll); View only reads it (D3 purity).
	pullupScroll int
	// pullupLayoutSig is the last-emitted G5 customized-layout signature (panel order + horizon). It
	// de-dupes the tui.pullup observability event so the App emits ONCE per distinct layout, not on every
	// render frame. Empty until the first customized render. View-side only; never read by the engine.
	pullupLayoutSig string

	// traceViewSig is the last-emitted G6 TRACE/FLOW swimlane signature (record name + trip metrics). It
	// de-dupes the tui.trace_view observability event so the App emits ONCE per distinct trip the TRACE
	// tab is shown for, not every render frame. Empty until the first TRACE render. View-side only.
	traceViewSig string

	// analysis preview (^Y): the post-session ANALYSIS surface prototype (the Shift+Tab twin of the ^O
	// monitors), rendered from a SAMPLE record so the layout is reviewable before the G1 record-loader
	// lands. anTab = active analysis tab, anCursor = scrub tick, anCompare = the power-ON/OFF diff mode.
	anPreview      bool
	anTab          int
	anCursor       int
	anCompare      bool
	anRecA, anRecB cognition.AnalysisRecord

	// monHist accrues the per-tick strip lanes (admit/flag/reject/voiced/used/…) from the event
	// stream, so the live monitors' strips show real rolling activity (built in NewApp).
	monHist *monitorHistory

	// thinkingOnUser drives the transient animated status under the user's last turn (spinner · verb ·
	// elapsed · thoughts) — the legible-silence instrument (product decision 2026-06-12). Set on submit,
	// cleared when the reply lands (action.respond kind=respond) or the line is set aside.
	thinkingOnUser bool
	userTurnAt     time.Time // when the in-flight user turn was submitted (elapsed display; TUI cosmetics may read the clock)

	// pendingSettings holds the Settings values whose substrate-CLASS change is awaiting the user's confirm
	// (S3); applyConfirmed("substrate-switch") folds + rebuilds them, a cancel discards them.
	pendingSettings *popup.SettingsValues

	// pendingEngineOps queues engine-MUTATING ops (submit a goal, flip a toggle, switch mode) that arrive
	// from the Update loop while a step owns the engine off-loop (a.stepping). They drain on the next
	// stepResult so the off-loop Step() is the only engine writer at any instant (D2). engineMutate routes
	// every on-loop engine mutation through this.
	pendingEngineOps []func()

	// animation clock (the wave / shimmer / border animations read a.frame). The animTick advances
	// frame at ~30fps but is armed only while a.animating() holds, so idle CPU stays flat (DESIGN §4.5).
	frame         int            // monotonic animation frame counter (advanced by animTickMsg)
	animTicking   bool           // is the animTick currently armed? (guards against double-arming)
	borderAnim    map[string]int // panel id -> frame at which its border animation ends
	welcomeSettle int            // frame at which the welcome wave stops animating, so an idle welcome
	//                              screen settles to a static render instead of pinning a core forever (T3)

	// the popups (DESIGN §4.4). Each is a small modal sub-model with Visible()/Show()/Hide().
	settings popup.Settings
	cmd      popup.Command
	slash    popup.Slash
	confirm  popup.Confirm
	model    popup.ModelPicker // /model — switch the local LLM live
	plan     popup.SwitchPlan  // the memory plan shown after picking a model (what to unload)
	cogmodel popup.CogModels   // /models — the cognition-model version manager (save/load/reset/diff)

	// cogBaseline tracks which saved cognition model is the structural-delta baseline (the [b] target /
	// the diff "from"). Persisted by convention name across re-lists; "" ⇒ the bridge falls back to the
	// reserved cold-baseline name, else the oldest snapshot.
	cogBaseline string
}

// NewApp builds the root model around a ready EngineBridge (the CLI owns substrate resolution — there
// is no offline product path, DESIGN §8). The engine may be nil behind the bridge (no model reachable);
// the App then shows the "configure a model" state and simply never dispatches a step.
//
// cfg is the engine build config this session started with — the base the Settings popup edits against.
// prompt is the optional `tui --prompt` opener (Python ChatHarnessApp(prompt=...)): submitted once on
// mount (Init). "" = no opener.
func NewApp(bridge *EngineBridge, cfg engine.EngineConfig, prompt string) *App {
	ta := textarea.New()
	ta.Placeholder = "Ask the harness something, or /help for commands"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	ta.CharLimit = 0
	ta.Focus()

	vp := viewport.New(0, 0)

	// the model + mode labels for the header/footer come off the engine when present.
	// branch starts empty (the chrome omits an empty branch) and is filled by resolveBranchCmd off-loop
	// at startup — the old hardcoded "main" showed the wrong branch in any worktree/non-main checkout.
	mode, model, branch := "reactive", "no model loaded", ""
	if eng := bridge.Engine(); eng != nil {
		mode = eng.Mode()
		model = eng.BackendLabel()
	}

	a := &App{
		bridge: bridge,
		// awake/continuous starts PAUSED on launch (the pre-built-engine path: `tui --profile awake`); the
		// product path (nil eng now, continuous after async resolve) pauses in onEngineRebuilt. Silent — the
		// welcome screen (empty chat) is the "type to begin" cue; the first input / ^P un-pauses.
		paused:        mode == "continuous",
		cfg:           cfg,
		monHist:       newMonitorHistory(),
		sessionID:     newSessionID(),
		input:         ta,
		viewport:      vp,
		chatView:      chat.NewChatView(),
		header:        chat.NewHeaderBar(Version),
		footer:        chat.NewFooterBar(mode, model, branch),
		settings:      popup.NewSettings(settingsFromCfg(cfg)),
		cmd:           popup.NewCommand(popup.DefaultCommands),
		slash:         popup.NewSlash(popup.CompletionsFromCommands(popup.DefaultCommands)),
		confirm:       popup.NewConfirm(),
		model:         popup.NewModelPicker(),
		plan:          popup.NewSwitchPlan(),
		cogmodel:      popup.NewCogModels(),
		borderAnim:    map[string]int{},
		initialPrompt: strings.TrimSpace(prompt),
		loadCh:        make(chan string, 64),
	}
	// No seeded banner/system lines: an empty conversation is the welcome-screen state (viewChat shows
	// welcomeView while chatView is empty, mirroring lathe's welcome-to-chat transition).
	a.armWelcomeSettle() // open the welcome animation window from frame 0 (T3)
	return a
}

// settingsFromCfg projects the live engine build config into the Settings popup's plain-data view (the
// popup never imports engine — plain data flows in, DESIGN §4.3). Reading from a.cfg means the popup
// always opens showing the CURRENT knob values (not hardcoded defaults), so a change round-trips.
func settingsFromCfg(cfg engine.EngineConfig) popup.SettingsValues {
	return popup.SettingsValues{
		Profile:          cfg.Profile,
		Mode:             cfg.Mode,
		Seed:             cfg.Seed,
		Cognition:        cfg.Cognition,
		Substrate:        cfg.Substrate,
		MaxTicks:         cfg.MaxTicks,
		ProactivityFloor: cfg.ProactivityFloor,
	}
}

// Init starts the event pump + the stepping tick (DESIGN §4.5). The pump parks on the bus channel and
// surfaces a batch atomically; the tick self-reschedules and drives stepping while the engine has
// work. textarea blink keeps the cursor alive.
//
// Init is the Bubble Tea analog of Python ChatHarnessApp.on_mount: when a `tui --prompt` opener was
// given AND a model is reachable, it submits the opener once here. SubmitDefault emits the port event
// the bridge drains back as the single user echo (so the opener appears exactly once, like a typed
// turn) and wakes the stepping loop — there is no local chat echo (the double-echo fix).
// resolveBranchCmd resolves the current git branch ONCE, off-loop (a fast local exec, but kept off the
// Update loop per the no-IO-in-Update discipline). `git rev-parse --abbrev-ref HEAD` is worktree-aware
// (returns the worktree's branch, e.g. cc-lane), and yields "" on any failure (not a repo / detached),
// which the chrome renders as no branch. Replaces the old hardcoded "main".
func resolveBranchCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
		if err != nil {
			return branchResolvedMsg{branch: ""}
		}
		b := strings.TrimSpace(string(out))
		if b == "HEAD" { // detached
			b = ""
		}
		return branchResolvedMsg{branch: b}
	}
}

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textarea.Blink,
		a.bridge.pumpCmd(),
		stepTickCmd(),
		resolveBranchCmd(),
	}

	if a.bridge.Engine() == nil {
		// PRODUCT PATH: no engine was built at launch (the CLI deferred substrate resolution so the UI
		// is instant). Resolve it now, off-loop — NewEngine may probe LM Studio and auto-load a model.
		// Route the auto-loader's progress onto the welcome screen and drain it; the opener (if any) is
		// deferred to onEngineRebuilt, when the engine is live.
		a.substrateLoading = true
		a.loadStatus = "resolving the model…"
		ch := a.loadCh
		llm.SetAutoLoadLog(func(s string) {
			select {
			case ch <- s:
			default:
			}
		})
		cmds = append(cmds, a.bridge.Rebuild(a.cfg), a.loadProgressCmd())
	} else if a.initialPrompt != "" {
		// the engine is already live (the --backend dev override): submit the opener once, now.
		a.bridge.Engine().SubmitDefault(a.initialPrompt)
	}

	// the welcome screen animates from the first frame (chat starts empty), so arm the anim clock now.
	if a.animating() {
		a.animTicking = true
		cmds = append(cmds, animTickCmd())
	}
	return tea.Batch(cmds...)
}

// loadProgressCmd parks on the auto-loader's progress channel and returns the next line as a
// loadProgressMsg (the welcome-screen substrate-loading status). The empty-string sentinel (pushed by
// unblockLoadPump when resolution completes) unblocks the final read so the goroutine never leaks.
func (a *App) loadProgressCmd() tea.Cmd {
	ch := a.loadCh
	return func() tea.Msg {
		return loadProgressMsg{text: <-ch}
	}
}

// unblockLoadPump pushes the empty-string sentinel so a parked loadProgressCmd returns once resolution
// is done (non-blocking — a full buffer means it will drain on its own).
func (a *App) unblockLoadPump() {
	select {
	case a.loadCh <- "":
	default:
	}
}

// Update is the strict modal-priority chain (DESIGN §4.1) plus the single-writer fold of the off-loop
// step result, the bus event batch, and the popup result carriers.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.ready = true
		a.recomputeLayout()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case stepTickMsg:
		// self-rescheduling stepping interval: dispatch a step when the engine is steppable and none
		// is in flight; always re-arm the tick (DESIGN §4.5 primitive 2).
		var cmds []tea.Cmd
		if !a.paused && !a.stepping && a.steppable() {
			a.stepping = true
			a.footer.SetThinking(true)
			cmds = append(cmds, a.bridge.stepCmd(a.modeEpoch))
		}
		cmds = append(cmds, stepTickCmd())
		return a, tea.Batch(cmds...)

	case animTickMsg:
		// advance the animation clock and re-arm ONLY while an animation is active, so a fully idle
		// surface stops ticking (DESIGN §4.5 idle-CPU discipline). ensureAnim revives it on a state change.
		a.frame++
		// the live thinking instrument re-renders ~10fps (every 3rd frame) while a user turn is in flight.
		if a.thinkingOnUser && a.frame%3 == 0 {
			a.chatView.SetStatus(a.thinkStatusLine())
			a.refreshViewport()
		}
		if a.animating() {
			return a, animTickCmd()
		}
		a.animTicking = false
		return a, nil

	case stepResultMsg:
		// fold the off-loop step result single-writer; DROP it if the epoch is stale (a mode switch
		// happened while the slow step was off-loop). DESIGN §4.5 primitive 1.
		a.stepping = false
		a.footer.SetThinking(false)
		// the engine is quiescent again (the step goroutine has returned): apply any mutations queued during
		// the step and refresh the Config/Registry read caches — ALWAYS, even when the snapshot is dropped as
		// stale, so a mode-switch/rebuild mid-step doesn't strand the queue (D2).
		a.drainEngineOps()
		a.refreshCogCaches()
		if msg.epoch != a.modeEpoch {
			return a, nil // stale snapshot — discard (the mutations + caches above still applied)
		}
		a.footer.IncrementSteps()
		a.vm.Snap = msg.snap // the end-of-tick live-engine reads feed the panels
		a.refreshFooterFromSnap(msg.snap)
		return a, a.ensureAnim()

	case eventBatchMsg:
		// fold a batch of bus events: append to the App's event ring (the panels' trace tail), drain
		// the bridge's queued conversation pairs into the chat, then re-issue the pump (DESIGN §4.5
		// primitive 3).
		a.foldEvents(msg.events)
		a.drainConversation()
		// Refresh the snapshot between steps so the snapshot-driven panels (durability, value, lifecycle,
		// arousal, memory, action) don't freeze on last-stepped values while the event-fed panels keep
		// updating (E3). Only when NO step is in flight: the engine is fully serial, so with a.stepping
		// false the Update loop is the only toucher and this read is race-free; an in-flight step will
		// publish its own fresh snapshot on completion. Events that arrive mid-step are folded above and
		// the snapshot catches up on the next non-stepping batch (or the step result).
		if !a.stepping && a.bridge.Engine() != nil {
			snap := a.bridge.BuildSnapshot()
			a.vm.Snap = snap
			a.refreshFooterFromSnap(snap)
		}
		a.refreshViewport()
		return a, tea.Batch(a.bridge.pumpCmd(), a.ensureAnim())

	case popup.PaletteResultMsg:
		a.cmd.Hide()
		return a.handleSlashCommand(msg.Command)

	case branchResolvedMsg:
		a.footer.SetBranch(msg.branch) // chrome header/footer/welcome all read footer.Branch()
		return a, nil

	case modelsListedMsg:
		if msg.err != "" {
			a.chatView.System("could not list models: " + msg.err)
			a.refreshViewport()
			return a, nil
		}
		a.model.SetSize(a.w, a.h)
		a.model.Show(msg.models)
		return a, nil

	case popup.ModelChosenMsg:
		// the user picked a model: gather the memory plan (what's loaded + the estimate) off-loop, then
		// open the plan screen so they choose what to unload (the "ask me each time" policy).
		a.chatView.System("checking memory for " + msg.Key + "…")
		a.refreshViewport()
		return a, a.bridge.PlanSwitchCmd(msg.Key, msg.Detail)

	case switchPlanMsg:
		if msg.err != "" {
			a.chatView.System("could not read loaded models: " + msg.err)
			a.refreshViewport()
			return a, nil
		}
		a.plan.SetSize(a.w, a.h)
		a.plan.Show(msg.target, msg.targetDetail, msg.note, msg.rows)
		return a, nil

	case popup.SwitchPlanMsg:
		// the user approved the plan: unload the chosen residents + load the target off-loop (can take a
		// while for a large model), then the engine rebuilds onto it (modelSwitchedMsg).
		note := "loading " + msg.Target + "…"
		if len(msg.Unload) > 0 {
			note = "unloading " + itoa(len(msg.Unload)) + " model(s) + loading " + msg.Target + "…"
		}
		a.chatView.System(note + " (a large model can take a minute)")
		a.refreshViewport()
		return a, a.bridge.ApplySwitchCmd(msg.Target, msg.Unload)

	case modelSwitchedMsg:
		if msg.err != "" {
			a.chatView.System("could not switch model: " + msg.err)
			a.refreshViewport()
			return a, nil
		}
		// point the build config at the now-served model + rebuild the engine onto it (the live swap
		// lands via engineRebuiltMsg). substrate=local + an explicit model is the "use exactly this" path.
		a.cfg.LLMModel = msg.served
		a.cfg.Substrate = "local"
		a.chatView.System("switching to " + msg.served + " — rebuilding engine…")
		a.refreshViewport()
		return a, a.bridge.Rebuild(a.cfg)

	case cogModelsListedMsg:
		// the bridge listed the saved cognition models (off-loop) + their structural deltas. Either OPEN
		// the popup (the /models entry) or REFRESH the already-open one after an action.
		if msg.err != "" {
			a.chatView.System("cognition models: " + msg.err)
			a.refreshViewport()
			return a, nil
		}
		a.cogBaseline = msg.baseline
		if msg.open {
			a.cogmodel.SetSize(a.w, a.h)
			a.cogmodel.Show(msg.rows, msg.baseline, msg.substrate)
		} else if a.cogmodel.Visible() {
			note := a.cogmodel.Note()
			a.cogmodel.Show(msg.rows, msg.baseline, msg.substrate)
			a.cogmodel.SetNote(note) // keep the last action's status line across the refresh
		}
		return a, nil

	case cogModelActionMsg:
		// one cognition-model action completed off-loop (save/reset/baseline/delete/diff). Surface the
		// result on the popup's note line and re-list so the rows + structural deltas reflect the change.
		if msg.err != "" {
			a.cogmodel.SetNote("error: " + msg.err)
			return a, nil
		}
		a.cogmodel.SetNote(msg.note)
		return a, a.bridge.CogModelsListCmd(a.cogBaseline, false)

	case popup.CogModelSaveMsg:
		return a, a.bridge.CogModelSaveCmd(msg.Name)
	case popup.CogModelLoadMsg:
		return a, a.bridge.CogModelResetCmd(msg.Name)
	case popup.CogModelBaselineMsg:
		// record the new baseline (persisted by convention name) + re-list so the deltas re-measure from it.
		a.cogBaseline = msg.Name
		a.cogmodel.SetNote("baseline → " + msg.Name)
		return a, a.bridge.CogModelsListCmd(a.cogBaseline, false)
	case popup.CogModelDiffMsg:
		return a, a.bridge.CogModelDiffCmd(msg.From, msg.To)
	case popup.CogModelDeleteMsg:
		return a, a.bridge.CogModelDeleteCmd(msg.Name)

	case popup.ConfirmResultMsg:
		// the Confirm popup answered; re-dispatch the pending action (here: the mode switch / reset
		// gated by a confirm). The watched-seam act gate uses the channel path instead (it blocks the
		// off-loop step's goroutine), so this carrier path only handles the App-initiated confirms.
		if msg.Confirmed {
			return a.applyConfirmed(msg.Name)
		}
		a.pendingSettings = nil // a cancelled substrate-switch confirm discards the stashed values (S3)
		return a, nil

	case loadProgressMsg:
		// fold one auto-loader progress line onto the welcome card; keep draining while the resolve is
		// in flight (the running welcome anim re-renders it). A non-empty line during loading updates the
		// status; once loading is done we stop re-arming (the sentinel unblocks the parked read).
		if a.substrateLoading {
			if msg.text != "" {
				a.loadStatus = msg.text
			}
			return a, a.loadProgressCmd()
		}
		return a, nil

	case engineRebuiltMsg:
		a.onEngineRebuilt(msg)
		return a, nil
	}

	return a, nil
}

// handleKey is the modal-priority key chain (DESIGN §4.1): ctrl+c quits; a visible popup grabs the key
// (so Shift+Tab is the popup's prev-item, not a mode switch); else Shift+Tab toggles the mode; else the
// global keys; else the active mode handles it.
func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 1. ctrl+c always quits, before anything else.
	if key == "ctrl+c" {
		return a, tea.Quit
	}

	// 2. modal priority: a visible popup owns the key sink. Each popup is checked in priority order;
	// Shift+Tab inside the Slash popup is its prev-item (NOT a mode switch) — that is exactly why this
	// guard runs before the Shift+Tab mode toggle below.
	if a.confirm.Visible() {
		var cmd tea.Cmd
		a.confirm, cmd = a.confirm.Update(msg)
		return a, cmd
	}
	if a.model.Visible() {
		var cmd tea.Cmd
		a.model, cmd = a.model.Update(msg)
		return a, cmd
	}
	if a.plan.Visible() {
		var cmd tea.Cmd
		a.plan, cmd = a.plan.Update(msg)
		return a, cmd
	}
	if a.cogmodel.Visible() {
		var cmd tea.Cmd
		a.cogmodel, cmd = a.cogmodel.Update(msg)
		return a, cmd
	}
	if a.cmd.Visible() {
		var cmd tea.Cmd
		a.cmd, cmd = a.cmd.Update(msg)
		return a, cmd
	}
	if a.settings.Visible() {
		var scmd tea.Cmd
		a.settings, scmd = a.settings.Update(msg)
		// Apply the moment the editor CLOSES (Esc/Enter/q). The previous code only applied when the
		// closing keystroke itself changed a value — but ←/→ edits happen during navigation and the
		// closing Esc changes nothing, so it never fired (the inert-knobs bug). applySettings diffs the
		// final values against the live cfg, so it is a no-op when nothing actually changed.
		if !a.settings.Visible() {
			return a, tea.Batch(scmd, a.applySettings(a.settings.Values()))
		}
		return a, scmd
	}
	if a.slash.Visible() {
		switch key {
		case "esc":
			a.slash.Dismiss()
			return a, nil
		case "tab", "ctrl+n":
			a.slash.Next()
			return a, nil
		case "shift+tab", "ctrl+p":
			a.slash.Prev()
			return a, nil
		case "enter":
			if sel := a.slash.Selected(); sel != "" {
				a.input.SetValue("")
				a.slash.Dismiss()
				return a.handleSlashCommand(sel)
			}
		}
		// any other key edits the input and re-filters the autocomplete (fall through).
	}

	// 2b. The runtime MONITOR pull-up owns navigation while open: scroll the stack, esc/^O closes.
	// (Other keys are swallowed so the overlay is modal — typing never leaks to the surface behind it.)
	if a.pullup {
		switch key {
		case "ctrl+o", "esc", "q":
			a.pullup = false
			a.pullupScroll = 0
		case "up", "k":
			a.pullupScroll--
			a.clampPullupScroll()
		case "down", "j":
			a.pullupScroll++
			a.clampPullupScroll()
		case "pgup":
			a.pullupScroll -= 10
			a.clampPullupScroll()
		case "pgdown", " ":
			a.pullupScroll += 10
			a.clampPullupScroll()
		case "home", "g":
			a.pullupScroll = 0
		case "end", "G":
			a.pullupScroll = 1 << 30
			a.clampPullupScroll()
		}
		return a, nil
	}

	// 2c. The ANALYSIS preview (^Y) is modal too: Tab cycles panels, [ ]/{ } scrub, c toggles the
	// power-ON/OFF compare, g/G to ends, esc/^Y closes. The Update side owns the bounds (clampAnalysis).
	if a.anPreview {
		switch key {
		case "ctrl+y", "esc", "q":
			a.anPreview = false
		case "tab", "right", "l":
			a.anTab++
			a.clampAnalysis()
			a.emitTraceView() // G6: witness the TRACE swimlane on the bus when we land on it (no-op when the knob is OFF)
		case "shift+tab", "left", "h":
			a.anTab--
			a.clampAnalysis()
			a.emitTraceView()
		case "]", ".":
			a.anCursor += 8
			a.clampAnalysis()
		case "[", ",":
			a.anCursor -= 8
			a.clampAnalysis()
		case "}":
			a.anCursor = a.nextStimulus(a.anCursor, +1)
		case "{":
			a.anCursor = a.nextStimulus(a.anCursor, -1)
		case "c":
			// toggle COMPARE. With tui.compare_load ON, entering COMPARE loads the two most recent recorded
			// runs from disk into A/B (the G2 power-ON/OFF benchmark over REAL recordings); default OFF keeps
			// the prototype frozen-A/sample-B pair. enterCompare owns the gate + the fall-back.
			a.enterCompare()
		case "g", "home":
			a.anCursor = 0
		case "G", "end":
			a.anCursor = 1 << 30
			a.clampAnalysis()
		}
		return a, nil
	}

	// 3. Shift+Tab toggles the mode (no popup owns the key here). +modeEpoch drops stale async results.
	if key == "shift+tab" {
		a.mode = a.mode.next()
		a.modeEpoch++
		a.cogTab, a.cogScroll = 0, 0 // enter COGNITION on the Overview tab, scrolled to the top
		a.recomputeLayout()
		return a, a.ensureAnim() // entering the welcome / a transitioning surface revives the anim clock
	}

	// 4. global keys (work in either mode, even during a step).
	switch key {
	case "ctrl+o":
		// OBSERVE: open the runtime MONITOR pull-up over the current surface — the full live
		// validation-instrument stack (VITALS / LOOP / SUBCONSCIOUS / SEAM / CONSCIOUS / VALUE / …),
		// read from the end-of-tick snapshot. (Closing is handled by the modal block above.)
		a.pullup = true
		a.pullupScroll = 0
		a.emitPullupCustomize() // G5: witness the customized layout on the bus (no-op when the knob is OFF)
		return a, nil
	case "ctrl+y":
		// ANALYSIS surface: open the post-session analysis over the current surface. When the live mind
		// has run, A is the FROZEN RUNNING SESSION — a real cognition.RecordFromFrozen off the bridge's
		// event tap (G1), so the surface analyses what the mind ACTUALLY did. Before the first event (a
		// fresh, un-stepped engine) it falls back to the deterministic SAMPLE so the layout is always
		// reviewable. B stays the OFF sample for the COMPARE A/B layout until the picker lands two records.
		a.anRecA = a.frozenOrSample()
		a.anRecB = cognition.SampleAnalysisRecord("B")
		a.anPreview, a.anTab, a.anCompare = true, 0, false
		a.anCursor = a.anRecA.Ticks / 2
		return a, nil
	case "ctrl+k":
		a.cmd.Show()
		return a, nil
	case "ctrl+p":
		// PAUSE the mind (P1 — the control the first live awake session lacked): stop dispatching
		// steps; an in-flight step finishes (its result still folds) but no new one starts. On a paid
		// substrate this is the spend brake. Resume re-arms on the next tick.
		a.paused = !a.paused
		if a.paused {
			a.chatView.System("mind paused — no new thinking dispatched (^P to resume)")
		} else {
			a.chatView.System("mind resumed")
		}
		a.refreshViewport()
		return a, nil
	case "ctrl+b":
		// the header's "Panel" affordance: in this harness it toggles the mode (same as Shift+Tab) —
		// there is no separate sidebar, the COGNITION mode IS the panel surface.
		a.mode = a.mode.next()
		a.modeEpoch++
		a.cogTab, a.cogScroll = 0, 0
		a.recomputeLayout()
		return a, a.ensureAnim()
	}

	// 5. the active mode handles the key.
	return a.handleModeKey(msg)
}

// handleModeKey routes a non-global key to the active mode. CHAT drives the input + the viewport;
// COGNITION scrolls the rail's chat viewport (the rail itself is a pure view of the snapshot).
func (a *App) handleModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Enter submits the input in BOTH modes — the input bar is present in chat AND cognition, so a goal
	// can be sent while inspecting the mind (Shift/Alt+Enter inserts a newline, handled by the textarea).
	if key == "enter" {
		return a.submitInput()
	}

	// Tab cycles the COGNITION full-screen tabs (Overview → Conscious → Subconscious → Action → Systems,
	// wrapping); each tab starts scrolled to the top. Shift+Tab stays the CHAT⇄COGNITION switch. In CHAT
	// mode Tab falls through to the input.
	if a.mode == ModeCognition && key == "tab" {
		n := len(a.cogTabs()) // mode-filtered count (Awake tab hidden in reactive)
		a.cogTab = (clampIndex(a.cogTab, n) + 1) % n
		a.cogScroll = 0
		return a, a.ensureAnim()
	}

	// CONFIG tab: it OWNS its keys (section switch / row move / toggle flip / tunable bump / bulk ops),
	// intercepted BEFORE the generic scroll + input fall-through so Space/letters don't type into the
	// input bar. Returns handled=true when the key was a config action.
	if a.mode == ModeCognition && a.activeCogTab().id == "config" {
		if model, cmd, handled := a.handleConfigKey(key); handled {
			return model, cmd
		}
	}

	var cmds []tea.Cmd

	// scroll keys drive the active surface; every other key types into the (always-present) input bar.
	switch key {
	case "pgup", "pgdown", "up", "down", "home", "end":
		if a.mode == ModeCognition {
			// REGISTRY tab: ↑↓ PICK the registry (the left index), not scroll — the detail pane scrolls on
			// PgUp/PgDn/Home/End instead. Picking a new registry resets the detail scroll to the top.
			if a.activeCogTab().id == "registry" && (key == "up" || key == "down") {
				if key == "up" {
					a.cogRegSel--
				} else {
					a.cogRegSel++
				}
				a.clampCogSelections() // clamp both bounds against the live section count (Update-side, D3)
				a.cogScroll = 0
				return a, a.ensureAnim()
			}
			// the panel grid is taller than its viewport — scroll it; clampCogScroll bounds the offset in
			// Update so windowGrid can render pure (D3).
			switch key {
			case "up":
				a.cogScroll--
			case "down":
				a.cogScroll++
			case "pgup":
				a.cogScroll -= a.h - 2
			case "pgdown":
				a.cogScroll += a.h - 2
			case "home":
				a.cogScroll = 0
			case "end":
				a.cogScroll = 1 << 30 // clampCogScroll pins it to maxScroll
			}
			a.clampCogScroll()
			break
		}
		// scroll the chat viewport (CHAT).
		var cmd tea.Cmd
		a.viewport, cmd = a.viewport.Update(msg)
		cmds = append(cmds, cmd)
		a.chatView.SetFollowBottom(a.viewport.AtBottom())
	default:
		// the input bar owns typing in BOTH modes (present in chat and cognition).
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(msg)
		cmds = append(cmds, cmd)
		// re-filter the inline slash autocomplete from the new input value (lathe's per-change call).
		a.slash.Update(a.input.Value())
		// the slash box appearing/disappearing changes the input height (and so the chat viewport's
		// residual height) with no resize — re-sync it here in Update so viewChat stays pure (D3).
		a.syncChatHeight()
	}
	return a, tea.Batch(cmds...)
}

// handleConfigKey processes a key while the Config tab is focused (§4.6). It returns handled=false for
// any key the tab does not claim (so it falls through to the input bar / scroll). The live flip mutates
// the engine's SHARED HarnessConfig in place via the bridge — NO engine rebuild, so learned state
// survives (unlike the Settings popup). The cursor is the (section, row) pair; the view rebuilds each
// frame and reclamps, so the indices here only need to move, not validate.
//
//	↑/↓        move the row within the section
//	←/→        a tunable row: bump it ∓1; otherwise switch section
//	[ / ]      always switch section (so a tunable row can still change section)
//	space      flip the focused BOOL toggle
//	A / O      all-on / all-off for the focused section's bools
//	d          reset the focused row to its default
func (a *App) handleConfigKey(key string) (tea.Model, tea.Cmd, bool) {
	cv := a.cfgCache // the cache (D2) — refreshed in Update after each flip via engineMutate
	nSec := len(cv.Sections)
	if nSec == 0 {
		return a, nil, false
	}
	if a.cogCfgSel >= nSec {
		a.cogCfgSel = nSec - 1
	}
	if a.cogCfgSel < 0 {
		a.cogCfgSel = 0
	}
	sec := cv.Sections[a.cogCfgSel]
	row := a.cogCfgRow
	if row >= len(sec.Rows) {
		row = len(sec.Rows) - 1
	}
	if row < 0 {
		row = 0
	}
	a.cogCfgRow = row

	switchSection := func(d int) {
		a.cogCfgSel = (a.cogCfgSel + d + nSec) % nSec
		a.cogCfgRow = 0
		a.cogScroll = 0
	}
	focused := func() (cognition.CfgRow, bool) {
		if row < len(sec.Rows) {
			return sec.Rows[row], true
		}
		return cognition.CfgRow{}, false
	}

	switch key {
	case "up":
		a.cogCfgRow--
		if a.cogCfgRow < 0 {
			a.cogCfgRow = 0
		}
		return a, a.ensureAnim(), true
	case "down":
		a.cogCfgRow++
		if a.cogCfgRow >= len(sec.Rows) {
			a.cogCfgRow = len(sec.Rows) - 1
		}
		return a, a.ensureAnim(), true
	case "[", "shift+left":
		switchSection(-1)
		return a, a.ensureAnim(), true
	case "]", "shift+right":
		switchSection(1)
		return a, a.ensureAnim(), true
	case "left":
		if r, ok := focused(); ok && r.Kind == cognition.CfgInt {
			path := r.Path
			a.engineMutate(func() { a.bridge.BumpConfigTunable(path, -1) })
		} else if ok && r.Kind == cognition.CfgFloat {
			path := r.Path
			a.engineMutate(func() { a.bridge.BumpConfigTunableFloat(path, -0.05) })
		} else {
			switchSection(-1)
		}
		return a, a.ensureAnim(), true
	case "right":
		if r, ok := focused(); ok && r.Kind == cognition.CfgInt {
			path := r.Path
			a.engineMutate(func() { a.bridge.BumpConfigTunable(path, +1) })
		} else if ok && r.Kind == cognition.CfgFloat {
			path := r.Path
			a.engineMutate(func() { a.bridge.BumpConfigTunableFloat(path, +0.05) })
		} else {
			switchSection(1)
		}
		return a, a.ensureAnim(), true
	case " ", "space":
		if r, ok := focused(); ok && r.Kind == cognition.CfgBool {
			path, on := r.Path, !r.On
			a.engineMutate(func() { a.bridge.ApplyConfigToggle(path, on) })
		}
		return a, a.ensureAnim(), true
	case "A":
		for _, r := range sec.Rows {
			if r.Kind == cognition.CfgBool && !r.On {
				path := r.Path
				a.engineMutate(func() { a.bridge.ApplyConfigToggle(path, true) })
			}
		}
		return a, a.ensureAnim(), true
	case "O":
		for _, r := range sec.Rows {
			if r.Kind == cognition.CfgBool && r.On {
				path := r.Path
				a.engineMutate(func() { a.bridge.ApplyConfigToggle(path, false) })
			}
		}
		return a, a.ensureAnim(), true
	case "d":
		if r, ok := focused(); ok && r.Kind == cognition.CfgBool {
			// reset-to-default == back ON (defaults are strictly all-ON, §4.1).
			path := r.Path
			a.engineMutate(func() { a.bridge.ApplyConfigToggle(path, true) })
		}
		return a, a.ensureAnim(), true
	}
	return a, nil, false
}

// submitInput sends the input as a user prompt to the engine (DESIGN §4.2: Enter sends). A slash
// command typed in full is dispatched as a command instead. Empty input is a no-op. Submitting a goal
// wakes the stepping loop (the next tick dispatches a step).
//
// The user line is NOT echoed locally here: SubmitDefault emits a `port`/received event that the
// bridge queues as a {user, text} pair (conversationPair), and drainConversation voices it ONCE on the
// next batch. Echoing here too would print the turn twice (the visual-UAT double-echo). This matches
// Python tui/app.py on_input_submitted, which only calls engine.submit and lets the bus drain show it.
func (a *App) submitInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return a, nil
	}
	a.input.Reset()
	a.slash.Dismiss()

	// a complete slash command dispatches as a command, not a prompt.
	if strings.HasPrefix(text, "/") {
		return a.handleSlashCommand(text)
	}

	// The user spoke: engage the mind. The first real message un-pauses a continuous loop that started
	// PAUSED on launch (the awake startup UX); thereafter the loop is under the user's ^P control.
	if !a.userEngaged {
		a.userEngaged = true
		a.paused = false
	}

	if a.bridge.Engine() != nil {
		// on the session substrate, a goal with no worker servicing the spool would hang the whole retry
		// window with only a "thinking…" cue — say so up front so the user can start the worker (S1).
		if a.runningSubstrateClass() == "session" {
			if fresh, _ := llm.SessionWorkerFresh(llm.SessionSpoolDir()); !fresh {
				a.chatView.System("no cc:session worker is servicing the spool — start tools/cc-worker.sh, or this goal will hang.")
			}
		}
		// route through engineMutate so a goal submitted while a step is in flight (common in continuous
		// mode) doesn't mutate the engine concurrently with Step() — it applies at the next step boundary
		// (D2). The port event is drained back as the single user echo.
		a.engineMutate(func() {
			if eng := a.bridge.Engine(); eng != nil {
				eng.SubmitDefault(text)
			}
		})
		// start the live "thinking on your line" instrument (cleared when the reply / set-aside lands).
		a.thinkingOnUser = true
		a.userTurnAt = time.Now()
		a.chatView.SetStatus(a.thinkStatusLine())
	} else {
		// no model reachable: there is no engine to emit the port event. Match Python's "no model yet"
		// branch of on_input_submitted — a system hint, NOT a user echo (Python does not voice the turn
		// here either; it just tells the user to configure a model).
		a.chatView.System("no model yet — open /settings to configure one (the harness has no offline mode)")
		a.refreshViewport()
	}
	return a, a.ensureAnim() // the thinking status line animates while the mind is on the turn
}

// handleSlashCommand dispatches a slash command (from the palette, the slash autocomplete, or a typed
// full command). Unknown commands echo a help hint. DESIGN §4.4.
func (a *App) handleSlashCommand(cmd string) (tea.Model, tea.Cmd) {
	cmd = strings.TrimSpace(strings.SplitN(cmd, " ", 2)[0])
	switch cmd {
	case "/help":
		a.chatView.System("commands: /settings /model /models /clear /reset /mode /doctor /stability /substrate /exit")
		a.chatView.System("keys: Shift+Tab mode · ^K palette · ^B panel · ^P pause mind · Enter send · ^C quit")
	case "/models":
		// the COGNITION MODELS version manager (§6): list the saved brain snapshots + their structural
		// growth vs the baseline, with save/load/reset/diff. The list (and the structural diffs) are read
		// off-loop because they touch disk; the popup opens when the result folds back.
		a.chatView.System("listing cognition models…")
		a.refreshViewport()
		return a, a.bridge.CogModelsListCmd(a.cogBaseline, true)
	case "/model":
		// /model manages LOCAL LM Studio models (load/unload/swap) and ends by switching the substrate to
		// local. On a non-local substrate that silently tears down session/claude — hint + redirect to
		// /settings (which confirm-gates the substrate-class change) instead of doing it behind their back (S2).
		if cls := a.runningSubstrateClass(); cls != "local" {
			a.chatView.System("/model manages LOCAL models; you're on the " + cls + " model.")
			a.chatView.System("switch the model via /settings first (it confirms the change), then /model.")
			a.refreshViewport()
			return a, nil
		}
		a.chatView.System("listing local models…")
		a.refreshViewport()
		return a, a.bridge.ListModelsCmd()
	case "/settings":
		a.settings.Show(settingsFromCfg(a.cfg))
	case "/clear":
		a.chatView.Clear()
		a.chatView.Banner("thought — Silent-Injection Cognition harness")
		a.armWelcomeSettle() // back to the welcome screen — re-open the animation window (T3)
	case "/reset":
		// a consequential action: gate it behind the Confirm popup (the App-initiated confirm path).
		a.confirm.SetPending("reset", "Reset the engine and discard this session?", nil)
	case "/mode":
		a.confirm.SetPending("toggle-mode", "Toggle the loop mode (reactive ⇄ awake)?", nil)
	case "/doctor":
		a.chatView.System("doctor probes the LLM-backed subsystems — run `thought doctor` from the CLI")
	case "/stability":
		a.runStabilityReport()
	case "/substrate":
		a.settings.Show(settingsFromCfg(a.cfg))
	case "/exit":
		return a, tea.Quit
	default:
		a.chatView.System("unknown command: " + cmd + " (try /help)")
	}
	a.refreshViewport()
	return a, a.ensureAnim() // revive the anim tick if a command (e.g. /clear) re-opened the welcome (T3)
}

// applyConfirmed re-dispatches an App-initiated action after the user confirmed it (DESIGN §4.4 row d).
func (a *App) applyConfirmed(name string) (tea.Model, tea.Cmd) {
	switch name {
	case "toggle-mode":
		if a.bridge.Engine() != nil {
			// the read (eng.Mode) AND the write (SetMode) both touch the engine, so do them together under
			// the stepping discipline (D2): immediately when idle, or queued to the next step boundary.
			a.engineMutate(func() {
				eng := a.bridge.Engine()
				if eng == nil {
					return
				}
				next := "continuous"
				if eng.Mode() == "continuous" {
					next = "reactive"
				}
				eng.SetMode(next)
				a.footer.SetMode(next)
				a.chatView.System("loop mode -> " + config.ModeLabel(next))
				a.refreshViewport()
			})
		}
	case "reset":
		a.chatView.Clear()
		a.chatView.Banner("session reset")
		a.armWelcomeSettle() // back to the welcome screen — re-open the animation window (T3)
		a.refreshViewport()
	case "substrate-switch":
		// the user confirmed the substrate-CLASS change: fold the stashed Settings values and rebuild
		// FRESH (no Store carry across the provenance boundary, S5).
		if a.pendingSettings == nil {
			return a, nil
		}
		v := *a.pendingSettings
		a.pendingSettings = nil
		a.cfg.Mode, a.cfg.Seed, a.cfg.Cognition = v.Mode, v.Seed, v.Cognition
		a.cfg.Substrate, a.cfg.MaxTicks, a.cfg.ProactivityFloor = v.Substrate, v.MaxTicks, v.ProactivityFloor
		a.chatView.System("switching model -> " + substrateClass(v.Substrate) + " — rebuilding engine (fresh state)…")
		a.refreshViewport()
		return a, a.bridge.RebuildFresh(a.cfg)
	}
	return a, a.ensureAnim() // /reset re-opens the welcome — revive the anim tick (T3)
}

// applySettings folds the Settings popup's edited knobs back (DESIGN §4.4 row a). The mode change is
// applied live; the rest (seed / substrate / cognition / max-ticks / proactive-floor) require an
// engine rebuild, which the App surfaces as a note (a live rebuild lands with the engine-rebuild Cmd).
func (a *App) applySettings(v popup.SettingsValues) tea.Cmd {
	eng := a.bridge.Engine()

	// PROFILE — a one-pick preset of the cognition knobs. Picking a DIFFERENT named profile sets the
	// whole HarnessConfig + the loop regime and forces a rebuild; the (custom) sentinel never overwrites
	// a hand-tuned config. Folded BEFORE the per-field logic so v.Mode reflects the profile's regime.
	profileChanged := v.Profile != "" && v.Profile != popup.ProfileCustom && v.Profile != a.cfg.Profile
	if profileChanged {
		if p, ok := config.ProfileByName(v.Profile); ok {
			a.cfg.Features = p.Build()
			a.cfg.Profile = p.Name
			v.Mode = p.Mode // the profile's regime drives the mode field the rest of this fn reads
			a.chatView.System("profile -> " + p.Name + " — " + p.Desc)
			// Self-contained: a persisting profile auto-wires memory to disk (substrate-tagged) if none is
			// set, so picking it gives a complete mind whose learning survives — nothing to configure by hand.
			if p.Persist && a.cfg.Store == nil {
				if dir := engine.DefaultStateDir(a.runningSubstrateClass()); dir != "" { // real class (test ⇒ skip)
					if st, err := persist.NewJSONLStore(dir); err == nil {
						a.cfg.Store = st
						a.chatView.System("memory persisting to " + dir)
					}
				}
			}
		} else {
			profileChanged = false
		}
	}

	// LIVE knobs — applied on the spot, no rebuild (the session/graph is preserved):
	//   • loop mode (reactive <-> continuous) via eng.SetMode
	//   • the Controller's decision mode (control | llm | hybrid) via Controller.Reconfigure — this is
	//     the knob that was silently inert; it now takes effect immediately (falling back to the control
	//     floor when the backend can't satisfy Decider, which we surface).
	if eng != nil {
		// these read + mutate engine state, so run them under the stepping discipline (D2): immediately
		// when idle, or queued to the next step boundary if a step is in flight.
		prevCognition := a.cfg.Cognition
		a.engineMutate(func() {
			eng := a.bridge.Engine()
			if eng == nil {
				return
			}
			if eng.Mode() != v.Mode {
				eng.SetMode(v.Mode)
				a.footer.SetMode(v.Mode)
				a.chatView.System("loop mode -> " + config.ModeLabel(v.Mode))
			}
			if prevCognition != v.Cognition {
				eng.Controller().Reconfigure(eng.Backend(), v.Cognition)
				if got := eng.Controller().Mode(); got != v.Cognition {
					a.chatView.System("cognition -> " + v.Cognition + " (no model backend; running " + got + ")")
				} else {
					a.chatView.System("cognition -> " + got)
				}
			}
		})
	}

	// REBUILD knobs — seed / substrate / max-ticks / proactivity floor are construction-time, so a
	// change rebuilds the engine (re-resolving the substrate). Compare against the OLD cfg first.
	needsRebuild := profileChanged || a.cfg.Seed != v.Seed || a.cfg.Substrate != v.Substrate ||
		a.cfg.MaxTicks != v.MaxTicks || a.cfg.ProactivityFloor != v.ProactivityFloor

	// A substrate-CLASS change (e.g. local -> session) is consequential: it re-homes provenance and, for
	// session, needs a live worker. Gate it behind the Confirm popup (the promised-but-unbuilt gate, S3)
	// — DON'T fold the cfg yet, so a cancel leaves the engine + cfg untouched. applyConfirmed does the
	// fold + a fresh (no-Store-carry) rebuild (S5).
	if needsRebuild && substrateClass(a.cfg.Substrate) != substrateClass(v.Substrate) {
		from, to := substrateClass(a.cfg.Substrate), substrateClass(v.Substrate)
		warn := "Switch model " + from + " -> " + to + "? Learned state stays in its own dir; the new model starts clean."
		if to == "session" {
			warn = "Switch model -> session? A worker must be running (tools/cc-worker.sh) or every goal will hang."
		}
		vv := v
		a.pendingSettings = &vv
		a.confirm.SetPending("substrate-switch", warn, nil)
		a.refreshViewport()
		return nil
	}

	// fold the edited knobs into the base cfg (the new baseline + the rebuild input).
	a.cfg.Mode, a.cfg.Seed, a.cfg.Cognition = v.Mode, v.Seed, v.Cognition
	a.cfg.Substrate, a.cfg.MaxTicks, a.cfg.ProactivityFloor = v.Substrate, v.MaxTicks, v.ProactivityFloor

	if needsRebuild {
		a.chatView.System("rebuilding engine (model/seed/budget changed)…")
		a.refreshViewport()
		return a.bridge.Rebuild(a.cfg) // same substrate class — carrying the Store is safe
	}
	a.refreshViewport()
	return nil
}

// runStabilityReport voices the current durability checklist into the chat (a cheap read off the
// snapshot the panels already carry). The full re-derivation is the CLI `thought stability`.
func (a *App) runStabilityReport() {
	if len(a.vm.Snap.Stability) == 0 {
		a.chatView.System("stability: no checks yet (submit a prompt to start an episode)")
		return
	}
	hold := 0
	for _, c := range a.vm.Snap.Stability {
		if c.Pass {
			hold++
		}
	}
	a.chatView.System("stability: " + itoa(hold) + "/" + itoa(len(a.vm.Snap.Stability)) + " hard checks hold")
}

// onEngineRebuilt re-arms the App after the engine was (re)built off-loop — either the INITIAL async
// substrate resolve (a.substrateLoading, shown on the welcome screen) or a Settings rebuild. DESIGN §8.
func (a *App) onEngineRebuilt(msg engineRebuiltMsg) {
	initial := a.substrateLoading
	if !msg.ok {
		if initial {
			// the welcome-screen substrate load failed (no model + nothing to auto-load): surface it on
			// the welcome card with a hint, and stop the progress drain. The harness has no offline path.
			a.substrateLoading = false
			a.loadStatus = ""
			a.loadErr = msg.err
			a.unblockLoadPump()
			a.refreshViewport()
			return
		}
		a.chatView.System("could not rebuild the engine: " + msg.err)
		a.refreshViewport()
		return
	}
	// swap the freshly-built engine in ON THE UPDATE LOOP (Rebuild built it off-loop, to avoid racing
	// the engine reads here). Bump the epoch so an in-flight step from the PREVIOUS engine is dropped
	// when it returns, and clear the thinking cue; the parked pump picks up the new bus.
	a.bridge.Attach(msg.eng)
	a.modeEpoch++
	a.stepping = false
	a.footer.SetThinking(false)
	a.footer.SetMode(msg.eng.Mode())
	a.footer.SetModel(msg.eng.BackendLabel())
	if initial {
		// the model is live: drop the welcome loading status and submit the deferred --prompt opener.
		a.substrateLoading = false
		a.loadStatus = ""
		a.loadErr = ""
		a.unblockLoadPump()
		if a.initialPrompt != "" {
			a.userEngaged = true // a --prompt opener is user-initiated input — the awake mind runs, not paused
			msg.eng.SubmitDefault(a.initialPrompt)
		}
	} else {
		a.chatView.System("engine rebuilt — settings applied")
	}
	// Awake mind starts PAUSED on TUI launch until the user engages (the awake startup UX you described):
	// a continuous-mode engine does NOT auto-spin its loop on attach — the user un-pauses by typing (first
	// message, submitInput) or ^P. A reactive engine has nothing to auto-run (it idles until a turn). The
	// userEngaged latch prevents re-pausing on a mid-session settings rebuild after the user has spoken.
	if msg.eng.Mode() == "continuous" && !a.userEngaged {
		a.paused = true // silent — the welcome screen (empty chat) is the "type to begin" cue; ^P also resumes
	}
	// the engine is fresh + quiescent (a.stepping just cleared): apply any ops queued during the rebuild
	// against the NEW engine, and refresh the Config/Registry read caches off it (D2). The dropped stale
	// step (epoch bumped) would otherwise never drain the queue.
	a.drainEngineOps()
	a.refreshCogCaches()
	a.refreshViewport()
}

// -- the event/state folds (single-writer, on the Update loop) -------------------------------------

// foldEvents appends a batch of bus events to the App's bounded ring (the panels' trace tail + the
// seam/critic/value/subconscious scans) and arms a border pulse for the panel whose subsystem fired.
func (a *App) foldEvents(evs []events.Event) {
	for _, ev := range evs {
		a.vm.Events = append(a.vm.Events, ev)
		a.monHist.observe(ev) // accrue the per-tick monitor strip lanes
		a.armPulse(ev.Kind)
		// the user's in-flight turn resolved — a direct reply landed, or the line was set aside:
		// stop the animated thinking status (the real turn / marker takes its place).
		if a.thinkingOnUser {
			if ev.Kind == events.Respond {
				if k, _ := ev.Data["kind"].(string); k == "respond" {
					a.thinkingOnUser = false
					a.chatView.SetStatus("")
				}
			} else if ev.Kind == events.Decision {
				if aside, _ := ev.Data["set_aside"].(bool); aside {
					a.thinkingOnUser = false
					a.chatView.SetStatus("")
				}
			}
		}
	}
	if len(a.vm.Events) > eventRingMax {
		a.vm.Events = a.vm.Events[len(a.vm.Events)-eventRingMax:]
	}
}

// drainConversation pops the (role, text) pairs the bridge queued from the bus and voices them in the
// chat (the watched seam made open). Mirrors Python EngineBridge.drain -> ChatLog.say.
func (a *App) drainConversation() {
	for _, p := range a.bridge.Drain() {
		a.chatView.Say(p.Role, p.Text)
		if p.Role == "harness" || p.Role == "user" {
			a.footer.IncrementMessages()
		}
	}
}

// armPulse starts a panel's border animation when its subsystem just emitted (DESIGN §4.5 — the
// "something happened" cue, now a frame-eased border transition). Only the kinds that map to a visible
// panel arm one; the animation runs for borderAnimFrames off the animation frame clock.
func (a *App) armPulse(kind string) {
	for _, id := range panelsForKind(kind) {
		a.borderAnim[id] = a.frame + borderAnimFrames
	}
}

// animating reports whether any animation is in flight, so the animTick keeps re-arming (and idle CPU
// stays flat once everything settles). In CHAT mode only the welcome screen animates (and only while
// the conversation is empty); in COGNITION mode a panel's border animation animates.
func (a *App) animating() bool {
	if a.thinkingOnUser {
		return true // the live thinking instrument animates until the turn resolves
	}
	if a.mode == ModeChat && a.chatView.Len() == 0 {
		// the welcome screen animates the wave field while the conversation is empty — but only for a
		// bounded SETTLE window (then it freezes to a static render, so an idle welcome no longer pins a
		// core at 30fps forever, T3). While the substrate is still loading the spinner must keep spinning,
		// so animation continues regardless of the window.
		return a.substrateLoading || a.frame < a.welcomeSettle
	}
	for _, until := range a.borderAnim {
		if a.frame < until {
			return true
		}
	}
	return false
}

// welcomeSettleFrames is how long the welcome wave animates before settling — ~20s at the ~30fps
// animTick. Long enough to feel alive on arrival, short enough that an unattended welcome stops burning
// CPU (T3). armWelcomeSettle (re)opens the window when the conversation (re)empties.
const welcomeSettleFrames = 600

// armWelcomeSettle reopens the welcome animation window from the current frame and revives the anim tick
// — called at startup and whenever the conversation is cleared back to the welcome screen.
func (a *App) armWelcomeSettle() {
	a.welcomeSettle = a.frame + welcomeSettleFrames
}

// thinkVerbs rotate on the status instrument (~5s each) — chrome labels on a live gauge, not the
// mind's voice (the words the mind SPEAKS always come from the model).
var thinkVerbs = []string{"thinking", "working on it", "connecting the pieces"}

// thinkStatusLine builds the live "mind is on your line" instrument: spinner · verb · (elapsed ·
// thought count). All values are REAL — wall-clock elapsed (TUI cosmetics) and the live line's thought
// count off the latest snapshot — the eye-contact analogue made honest.
func (a *App) thinkStatusLine() string {
	spin := welcomeSpinner[(a.frame/3)%len(welcomeSpinner)]
	verb := thinkVerbs[(a.frame/150)%len(thinkVerbs)]
	elapsed := time.Since(a.userTurnAt).Round(time.Second)
	n := len(a.vm.Snap.ActiveContext)
	stat := fmt.Sprintf("(%s · %d thought", elapsed, n)
	if n != 1 {
		stat += "s"
	}
	stat += ")"
	return spin + " " + verb + "… " + stat
}

// ensureAnim revives the animation clock if a just-applied state change started an animation while the
// tick was stopped. Returns the tick Cmd to batch (nil when already ticking or nothing to animate).
func (a *App) ensureAnim() tea.Cmd {
	if !a.animTicking && a.animating() {
		a.animTicking = true
		return animTickCmd()
	}
	return nil
}

// borderHexFor returns the animated border hex for a panel mid state-transition: a brightness ease
// from the accent (the instant its subsystem fired) back to the resting WHITE border over
// borderAnimFrames. "" once the animation has elapsed (the border then rests at the default white).
func (a *App) borderHexFor(id string) string {
	end := a.borderAnim[id]
	if a.frame >= end {
		return ""
	}
	t := float64(end-a.frame) / float64(borderAnimFrames) // 1 at the fire instant, → 0 at rest
	if t > 1 {
		t = 1
	}
	return lerpHex("#ffffff", string(Pal.Accent), t)
}

// lerpHex linearly interpolates two "#rrggbb" colors: t=0 → a, t=1 → b.
func lerpHex(a, b string, t float64) string {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	lerp := func(x, y int) int { return x + int(float64(y-x)*t+0.5) }
	return fmt.Sprintf("#%02x%02x%02x", lerp(ar, br), lerp(ag, bg), lerp(ab, bb))
}

// hexRGB parses "#rrggbb" into its 8-bit channels (0,0,0 on a malformed string).
func hexRGB(s string) (int, int, int) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return 0, 0, 0
	}
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, 0, 0
	}
	return int(v>>16) & 0xff, int(v>>8) & 0xff, int(v) & 0xff
}

// panelsForKind maps an event kind to the rail panel(s) it lights up (the pulse targets). A subsystem
// that is split into a _metrics + _text panel returns BOTH ids so the whole subsystem pulses together.
func panelsForKind(kind string) []string {
	switch {
	case kind == events.SkillMint:
		// a recurring program promoted to a skill: the generative + learning organs both fired.
		return []string{"generative", "convert", "memory_metrics", "memory_text"}
	case kind == events.Convert:
		return []string{"convert", "memory_metrics", "memory_text"}
	// SUBCONSCIOUS — the generative layer (synthesise/operator/subagent/skill_match) vs. the plain scan.
	case kind == events.SubSynthesize, kind == events.SubOperator, kind == events.SubSubagent, kind == events.SkillMatch:
		return []string{"generative", "subconscious"}
	case strings.HasPrefix(kind, "subconscious."):
		return []string{"subconscious"}
	case strings.HasPrefix(kind, "seam."):
		return []string{"seam"}
	// CONSCIOUS — the metacognitive ops / cross-refs vs. the plain append (both reshape the frontier).
	case kind == events.MCP, kind == events.XRef:
		return []string{"mcp", "conscious", "frontier"}
	case kind == events.Generate || kind == events.Append:
		return []string{"conscious", "frontier"}
	// ACTION — real tool dispatch / gates / outward actions also light the tool-execution panel.
	case kind == events.ActionTool, kind == events.ActionSandboxDeny, kind == events.ActionSafetyBlock,
		kind == events.ActionBlocked, kind == events.Respond, kind == events.Ask:
		return []string{"toolexec", "action_text", "action_metrics"}
	case strings.HasPrefix(kind, "action."):
		return []string{"action_text", "action_metrics"}
	case strings.HasPrefix(kind, "critic."):
		return []string{"critic_metrics", "critic_text"}
	case strings.HasPrefix(kind, "value."):
		return []string{"value", "frontier"} // V(s) reranks the frontier
	// REGULATOR — the scheduler's defers light the scheduler panel; the rest light durability.
	case kind == events.Schedule:
		return []string{"scheduler"}
	case strings.HasPrefix(kind, "regulator."):
		return []string{"durability"}
	// the language backend's calls/fallbacks light the backend health panel.
	case strings.HasPrefix(kind, "llm."):
		return []string{"backend"}
	// REALITY / MEMORY / RUNTIME organs — each has a dedicated rail panel that was rendering live data
	// but never flashed on an event (E4). Pulse the panel its stream feeds.
	case strings.HasPrefix(kind, "grounding."):
		return []string{"grounding"}
	case strings.HasPrefix(kind, "session."):
		return []string{"session"}
	case strings.HasPrefix(kind, "knowledge."):
		return []string{"knowledge"}
	case strings.HasPrefix(kind, "retrieval."):
		return []string{"retrieval"}
	case strings.HasPrefix(kind, "memory."):
		return []string{"memory_metrics", "memory_text"}
	case strings.HasPrefix(kind, "persist."):
		return []string{"persist", "memory_metrics", "memory_text"}
	case strings.HasPrefix(kind, "config."):
		return []string{"config_events"}
	// PERCEPTION — the live senses (read_clock + the orientation pass) from the cognitive power-cycle
	// light the Perception panel.
	case strings.HasPrefix(kind, "perception."):
		return []string{"perception"}
	// the Pattern-C escalation floor-stood marker is an executive (Controller/Filter) decision.
	case strings.HasPrefix(kind, "escalation."):
		return []string{"critic_metrics", "critic_text"}
	// CONTINUOUS / awake — drives/default-mode (port), arousal, and the awake decision policy all light
	// the continuous (Awake-tab) panel.
	case kind == events.Port:
		return []string{"continuous"}
	case strings.HasPrefix(kind, "continuous."):
		return []string{"continuous"}
	case strings.HasPrefix(kind, "arousal."):
		return []string{"continuous", "lifecycle_metrics", "lifecycle_text"}
	case strings.HasPrefix(kind, "lifecycle."):
		return []string{"lifecycle_metrics", "lifecycle_text"}
	default:
		return nil
	}
}

// refreshFooterFromSnap pushes the snapshot's mode/arousal-derived state onto the footer (the model +
// branch are static; the mode + thinking cue track the engine).
func (a *App) refreshFooterFromSnap(s cognition.SnapshotData) {
	if s.Mode != "" {
		a.footer.SetMode(s.Mode)
	}
}

// -- layout + viewport --------------------------------------------------------------------------

// recomputeLayout re-sizes every region after a resize or a mode switch (each mode sizes differently).
// Single-writer (only Update calls it).
func (a *App) recomputeLayout() {
	if !a.ready {
		return
	}
	// CHAT widths: the header/footer/input span the terminal; the input box reserves its border.
	a.header.SetWidth(a.w)
	a.footer.SetWidth(a.w)
	a.input.SetWidth(a.w - inputBorder - 2) // border(2) + prompt slack
	a.slash.SetWidth(a.w)
	a.cmd.SetSize(a.w, a.h)
	a.model.SetSize(a.w, a.h)
	a.plan.SetSize(a.w, a.h)
	a.confirm.SetWidth(a.w)

	// the chat viewport gets the residual height inside the shared chrome frame (refreshViewport →
	// syncChatHeight sets the height from contentHeight; the slash box growing the input is re-synced in
	// Update, not in View).
	a.viewport.Width = a.w
	a.chatView.SetWidth(a.w - 2)
	a.refreshViewport()
	// refresh the Config/Registry read caches (when not stepping) before clamping against them, so a
	// resize re-clamp sees the live section/row counts (D2 cache + D4 re-clamp).
	a.refreshCogCaches()
	a.clampCogScroll()
	a.clampCogSelections()
}

// refreshViewport re-renders the chat content into the viewport and pins it to the bottom when the
// view is following (lathe followBottom discipline). Called after any chat mutation.
func (a *App) refreshViewport() {
	a.viewport.SetContent(a.chatView.View())
	a.syncChatHeight()
}

// syncChatHeight keeps the chat viewport's height matched to the residual content area and re-pins the
// bottom when following. The slash-autocomplete box grows the input mid-typing (changing contentHeight
// with no WindowSizeMsg), so this is called from Update wherever that can happen — keeping the viewChat
// View pure (it no longer sets Height / GotoBottom itself, D3). Single-writer (only Update calls it).
func (a *App) syncChatHeight() {
	a.viewport.Height = a.contentHeight()
	if a.chatView.FollowBottom() {
		a.viewport.GotoBottom()
	}
}

// runningSubstrateClass reports the provenance class of the ACTUALLY-RUNNING substrate, derived from the
// live backend label — which reflects a `--backend` dev override too, unlike cfg.Substrate (that stays
// "auto" when --backend selects the backend directly). Falls back to the configured substrate when no
// engine is attached. This is what the /model guard + the no-worker hint key off, so they fire on the
// real substrate, not just the one the Settings picker set (S1/S2).
func (a *App) runningSubstrateClass() string {
	if eng := a.bridge.Engine(); eng != nil {
		if cls := eng.SubstrateClass(); cls != "" {
			return cls // the backend's own construction-time stamp (llm.ClassOf) — the one truth
		}
	}
	return substrateClass(a.cfg.Substrate)
}

// engineMutate runs an engine-MUTATING op under the single-writer discipline (D2): while a step owns the
// engine off-loop (a.stepping), the op is QUEUED and drained when the step completes (drainEngineOps on
// stepResultMsg); otherwise it runs immediately and the read caches are refreshed. Every on-loop engine
// mutation (submit a goal, flip a config toggle, switch the loop mode) routes through here so the off-loop
// Step() is the only engine writer at any instant. The op closures run on the Update loop in both paths.
func (a *App) engineMutate(op func()) {
	if a.stepping {
		a.pendingEngineOps = append(a.pendingEngineOps, op)
		return
	}
	op()
	a.refreshCogCaches()
}

// drainEngineOps applies every queued engine mutation in arrival order (called from stepResultMsg once
// a.stepping is false — the engine is quiescent again). Runs on the Update loop.
func (a *App) drainEngineOps() {
	ops := a.pendingEngineOps
	a.pendingEngineOps = nil
	for _, op := range ops {
		op()
	}
}

// refreshCogCaches rebuilds the Config/Registry read caches the COGNITION View reads, but ONLY when no
// step is in flight (a step owns the engine maps off-loop; reading them then would race — D2). A step's
// mints/flips surface on the next refresh (stepResult). Cheap (it ranges a few maps); called after a
// step, after a config flip, and on resize / mode switch.
func (a *App) refreshCogCaches() {
	if a.stepping {
		return
	}
	a.cfgCache = a.bridge.ConfigView()
	a.regCache = a.bridge.BuildRegistryCatalog()
}

// clampIndex clamps a selection index into [0, n-1], returning 0 for an empty list. Used by the pure
// config/registry render helpers (read-only) and the Update-side selection clamp.
func clampIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}

// clampIndexOffset clamps a scroll offset into [0, max]. Used by the pure windowGrid render.
func clampIndexOffset(i, max int) int {
	if max < 0 {
		max = 0
	}
	if i > max {
		i = max
	}
	if i < 0 {
		i = 0
	}
	return i
}

// cogScrollMax is the maximum vertical scroll offset for the active COGNITION tab: the rendered body's
// line count minus the body viewport height, floored at 0. It rebuilds the tab body (cheap string
// assembly, only on key/resize events — never per frame) so Update can clamp a.cogScroll authoritatively
// without the View writing the model (D3/D4).
func (a *App) cogScrollMax() int {
	strip := a.cognitionTabStrip()
	bodyH := a.contentHeight() - (strings.Count(strip, "\n") + 1)
	if bodyH < 1 {
		bodyH = 1
	}
	lines := strings.Count(a.cognitionTabBody(), "\n") + 1
	if m := lines - bodyH; m > 0 {
		return m
	}
	return 0
}

// clampCogScroll re-clamps a.cogScroll into [0, cogScrollMax] from Update — after a scroll key moves it
// and after a resize shrinks the grid (D4). No-op outside COGNITION mode (nothing scrolls there).
func (a *App) clampCogScroll() {
	if a.mode != ModeCognition {
		return
	}
	a.cogScroll = clampIndexOffset(a.cogScroll, a.cogScrollMax())
}

// clampCogSelections re-clamps the Config (section/row) and Registry selection indices against the live
// section/row counts from Update — after a selection key and after a resize/config reload (D3/D4). The
// render helpers then only read these indices.
func (a *App) clampCogSelections() {
	cv := a.cfgCache // the cache, not the live engine (D2) — clamps run on the Update loop
	a.cogCfgSel = clampIndex(a.cogCfgSel, len(cv.Sections))
	if len(cv.Sections) > 0 {
		a.cogCfgRow = clampIndex(a.cogCfgRow, len(cv.Sections[a.cogCfgSel].Rows))
	} else {
		a.cogCfgRow = 0
	}
	a.cogRegSel = clampIndex(a.cogRegSel, len(a.regCache.Sections))
}

// -- steppability -------------------------------------------------------------------------------

// steppable reports whether the engine has work to advance (so the stepping tick should dispatch a
// step). Continuous mode is always-on (it generates its own endogenous work); reactive mode steps
// while a goal/percept is pending, an async action is outstanding, or the loop is mid-episode (not
// idle-and-quiescent). Mirrors the engine's own Run() stop test (Idle && !pending && !outstanding).
func (a *App) steppable() bool {
	eng := a.bridge.Engine()
	if eng == nil {
		return false
	}
	if eng.Mode() == "continuous" {
		return true
	}
	if eng.PortPending() || eng.HasOutstandingAction() {
		return true
	}
	// mid-episode: the lifecycle is ACTIVE / AWAITING_* (not IDLE / DONE).
	switch eng.LifecycleState() {
	case "ACTIVE", "AWAITING_REALITY", "SUSPENDED":
		return true
	}
	return false
}

// -- View (DESIGN §4.1) -------------------------------------------------------------------------

// View renders the active mode and returns a centered full-screen popup early when one is open
// (the modal idioms of DESIGN §4.4). The Slash popup is composited inline inside viewChat (§4.2), so
// only the three centered popups return early here.
func (a *App) View() string {
	if !a.ready {
		return ""
	}

	var base string
	switch a.mode {
	case ModeChat:
		base = a.viewChat()
	case ModeCognition:
		base = a.viewCognition()
	default:
		base = a.viewChat()
	}

	// the ANALYSIS preview (^Y) overlays the post-session analysis surface (shell + the active tab),
	// rendered from a SAMPLE record — the Shift+Tab surface prototype, top-aligned over the screen.
	if a.anPreview {
		w := a.w - 4
		if w > 130 {
			w = 130
		}
		shell := cognition.RenderAnalysisShell(a.anRecA, a.anCursor, a.anCompare, a.anTab, w)
		panel := cognition.RenderAnalysisTab(a.anRecA, a.anRecB, a.anCursor, a.anCompare, a.anTab, w, a.registryHeatEnabled(), a.deepLedgersEnabled(), a.traceFlowEnabled())
		return lipgloss.Place(a.w, a.h, lipgloss.Top, lipgloss.Center, shell+"\n"+panel)
	}

	// the runtime MONITOR pull-up (^O) overlays the live validation-instrument stack, centered over
	// the current surface — the panels render from the live end-of-tick snapshot.
	if a.pullup {
		lines, viewH, maxScroll := a.pullupBounds()
		off := a.pullupScroll // View stays pure: clamp into a local, never write the field back (D3)
		if off > maxScroll {
			off = maxScroll
		}
		if off < 0 {
			off = 0
		}
		end := off + viewH
		if end > len(lines) {
			end = len(lines)
		}
		shown := strings.Join(lines[off:end], "\n")
		label := "runtime monitors · ↑↓ scroll · ^O to close"
		if maxScroll > 0 {
			label = fmt.Sprintf("runtime monitors · lines %d–%d of %d · ↑↓/PgUp/PgDn scroll · ^O close", off+1, end, len(lines))
		}
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#5b6270")).Render(label)
		return lipgloss.Place(a.w, a.h, lipgloss.Top, lipgloss.Center, shown+"\n"+hint)
	}

	// full-screen-centered popups replace the screen (DESIGN §4.1 View switch).
	switch {
	case a.cmd.Visible():
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.cmd.View())
	case a.settings.Visible():
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.settings.View())
	case a.confirm.Visible():
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.confirm.View())
	case a.model.Visible():
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.model.View())
	case a.plan.Visible():
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.plan.View())
	case a.cogmodel.Visible():
		return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, a.cogmodel.View())
	}
	return base
}

// -- COGNITION-mode view helpers (used by mode.go viewCognition) --------------------------------

// cognitionHeader builds the unified top chrome bar shown in BOTH modes — the SESSION IDENTITY bar.
// Left zone: app + version · session id · tick · the active surface (chat/cognition) · lifecycle state.
// Right zone: the ENGINE identity — model · branch only. The loop regime (reactive/continuous) used to
// sit here too, mixing a mode with the identity; it moved to the above-input line, so the right zone is
// now coherent (just model + branch).
func (a *App) cognitionHeader() cognition.Panel {
	s := a.vm.Snap
	state := s.LifecycleState
	if state == "" {
		state = "IDLE"
	}
	left := "thought " + Version + "  ·  session " + a.sessionID + "  ·  tick " + itoa(s.Tick) + "  ·  " + a.mode.String() + " · " + state
	return cognition.Panel{Body: chromeRow(left, a.identityRight(), a.w-4), Chrome: true}
}

// identityRight is the header's right zone: the engine identity — the active model and the branch.
func (a *App) identityRight() string {
	model := a.footer.Model()
	if model == "" {
		model = "no model loaded"
	}
	r := model
	if b := a.footer.Branch(); b != "" {
		r += "  ·  branch:" + b
	}
	return r
}

// cognitionStatus builds the unified bottom chrome bar — the live STATUS/telemetry bar, DYNAMIC by
// mode. COGNITION (inspecting the mind): the durability metrics (arousal · θ · n) on the left. CHAT
// (conversing): the session counters (msgs · steps) on the left. Both put the most-recent bus event
// kind on the right. (The thinking cue + the Shift+Tab toggle live on the above-input line instead.)
func (a *App) cognitionStatus() cognition.Panel {
	s := a.vm.Snap
	var left string
	if a.mode == ModeCognition {
		left = "θ=" + ftoa2(s.Theta) + " · n=" + ftoa2(s.N)
		if s.Arousal != "" {
			left = s.Arousal + " · " + left
		}
	} else {
		var parts []string
		if m := a.footer.MsgCount(); m > 0 {
			parts = append(parts, "msgs:"+itoa(m))
		}
		if st := a.footer.StepsRun(); st > 0 {
			parts = append(parts, "steps:"+itoa(st))
		}
		left = strings.Join(parts, " · ")
		if left == "" {
			left = "no turns yet"
		}
	}
	return cognition.Panel{Body: chromeRow(left, a.lastEventKind(), a.w-4), Chrome: true}
}

// lastEventKind is the most-recent bus event kind — the footer's right zone in both modes ("" until an
// event has fired).
func (a *App) lastEventKind() string {
	if n := len(a.vm.Events); n > 0 {
		return a.vm.Events[n-1].Kind
	}
	return ""
}

// aboveInputLine is the lathe-style identity line directly above the input box in BOTH modes: the loop
// regime (reactive/continuous, tone-coded) and the Shift+Tab affordance that switches the surface, with
// a "thinking…" cue on the right while a step is off-loop. Plain text (no chrome bar), one line wide.
func (a *App) aboveInputLine() string {
	loop := config.ModeLabel(a.footer.Mode()) // "awake" / "reactive" — the consolidated user vocabulary
	loopTone := Pal.Ok
	if a.footer.Mode() == "continuous" {
		loopTone = Pal.Warn
	}
	other := "cognition"
	if a.mode == ModeCognition {
		other = "chat"
	}
	key := lipgloss.NewStyle().Foreground(Pal.Accent)
	mut := lipgloss.NewStyle().Foreground(Pal.Muted)
	left := " " + lipgloss.NewStyle().Foreground(loopTone).Render(loop) + mut.Render(" mode  ·  ") +
		key.Render("Shift+Tab") + mut.Render(" ⇄ "+other) + mut.Render("  ·  ") +
		key.Render("^O") + mut.Render(" monitors") + mut.Render("  ·  ") +
		key.Render("^Y") + mut.Render(" analysis") + mut.Render("  ·  ") +
		key.Render("^P") + mut.Render(" pause")
	if a.paused {
		left = " " + lipgloss.NewStyle().Foreground(Pal.Warn).Render("PAUSED") + mut.Render("  ·  ") +
			key.Render("^P") + mut.Render(" resume  ·  ") + key.Render("Shift+Tab") + mut.Render(" ⇄ "+other)
	}
	right := ""
	if a.footer.Thinking() {
		right = key.Render("thinking…") + " "
	}
	return chromeRow(left, right, a.w)
}

// chromeRow lays a chrome-bar body out as a left zone and a right zone justified to `inner` visible
// columns (the bar's content width = terminal width − border − padding). The right zone anchors to the
// far edge; it is dropped when there is no room (the bar then shows the left zone alone, which renderFit
// clips if still too wide). Widths are measured visibly (ANSI/■wide-rune aware) via lipgloss.Width.
func chromeRow(left, right string, inner int) string {
	if inner < 1 {
		return left
	}
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if rw == 0 || lw+rw+1 > inner {
		return left
	}
	return left + strings.Repeat(" ", inner-lw-rw) + right
}

// panelTitle renders a rail panel's bordered title line (the rail spec owns the title; the panels
// carry only their body). Subtext tone, foreground-only, no bold (DESIGN §5).
func (a *App) panelTitle(title string) string {
	return SectionStyle.Render(title)
}

// -- small helpers ------------------------------------------------------------------------------

// newSessionID returns a short per-launch session id (the low 8 hex digits of the start time). The
// TUI layer may read the clock — only the headless engine is held to deterministic seeded ticks.
func newSessionID() string {
	h := strconv.FormatInt(time.Now().UnixNano(), 16)
	if len(h) > 8 {
		h = h[len(h)-8:]
	}
	return h
}

// itoa is the int->string used in the header / status / chat lines (avoids importing strconv twice).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ftoa2 formats a float to two decimals for the status bar (a tiny fixed-format helper; the panels do
// their own %.2f via fmt — the status bar stays allocation-light).
func ftoa2(f float64) string {
	neg := f < 0
	if neg {
		f = -f
	}
	whole := int(f)
	frac := int((f-float64(whole))*100 + 0.5)
	if frac >= 100 {
		whole++
		frac -= 100
	}
	s := itoa(whole) + "."
	if frac < 10 {
		s += "0"
	}
	s += itoa(frac)
	if neg {
		s = "-" + s
	}
	return s
}

// compile-time assertion: *App is a tea.Model.
var _ tea.Model = (*App)(nil)

// Run wraps the engine in the App + tea.Program (kept here so cmd/thought never imports charmbracelet
// directly — it calls Run, DESIGN §0/§8). prompt is the optional `tui --prompt` opener, submitted once
// on mount (Python's tui --prompt -> ChatHarnessApp(prompt=...).on_mount); "" = no opener.
func Run(eng *engine.Engine, cfg engine.EngineConfig, prompt string) error {
	// The alt-screen owns the terminal, so the LM Studio auto-loader must never write to stderr from
	// here: mute it by default. The product path (eng == nil) re-points the sink in App.Init to stream
	// progress onto the welcome screen instead; a later Settings-rebuild load stays muted (the rebuild
	// surfaces its own "rebuilding…/applied/failed" notes in chat).
	llm.SetAutoLoadLog(nil)
	bridge := NewBridge(eng)
	app := NewApp(bridge, cfg, prompt)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
