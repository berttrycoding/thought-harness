package tui

// bridge.go — the seam between the headless-pure engine and the Bubble Tea app (DESIGN §4.5). The
// EngineBridge owns the engine.Engine and the events.Bus subscription, and exposes three things the
// app folds in as typed Msgs:
//
//   1. stepCmd      — engine.Step() run in a goroutine (it may block on a slow LLM call), returning a
//                     stepResultMsg with the modeEpoch stamped at dispatch (DESIGN §4.5 primitive 1).
//   2. stepTickCmd  — the self-rescheduling tea.Tick stepping interval (DESIGN §4.5 primitive 2).
//   3. pumpCmd      — a Cmd that drains a batch of bus events from a buffered channel into an
//                     eventBatchMsg (DESIGN §4.5 primitive 3); a bus callback pushes onto that chan.
//
// BuildSnapshot() reads the engine's read-only accessors (DESIGN §4.5 engine surface) at end-of-tick
// and returns a cognition.SnapshotData — the live fields the Python panels.render_* read off `eng`.
// The bridge is the ONLY thing that touches engine internals on the TUI side; the panels stay pure
// views over the snapshot + the event stream. The engine never learns the TUI exists.
//
// This mirrors the Python tui/bridge.py EngineBridge (engine lifecycle + bus→conversation drain),
// extended for Bubble Tea's off-loop discipline (Python's Textual @work(thread=True) → a tea.Cmd
// goroutine; Python's set_interval → tea.Tick).

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/host"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/tui/cognition"
	"github.com/berttrycoding/thought-harness/internal/tui/popup"
	"github.com/berttrycoding/thought-harness/internal/web"
)

// stepInterval is the default reactive stepping cadence (Python set_interval(0.4)). Continuous mode
// runs its own cadence inside the engine; the tick just keeps dispatching while there is work.
const stepInterval = 400 * time.Millisecond

// evqCapacity bounds the event-pump channel. The bus is synchronous and a single step can emit many
// events; the channel buffers a step's worth so the bus callback never blocks the off-loop step. A
// drain pulls up to drainBatch at a time.
const (
	evqCapacity = 4096
	drainBatch  = 256
)

// freezeCap bounds the freeze-tap's event-history ring (Track G, G1). The tap retains the most recent
// freezeCap events so a ^P freeze of a long awake session reconstructs a bounded scrubbable window
// (not the unbounded full history — a long-running mind would grow without limit). It is sized to the
// bus replay ring (events.NewDefault = 4000) so the frozen window matches what the bus itself keeps.
const freezeCap = 4000

// EngineBridge owns the engine, its bus subscription, and the bus→(events, conversation) drains. It
// is constructed once and survives engine rebuilds (a config change rebuilds the engine but keeps the
// bridge + its channel, re-subscribing the new bus). Mirrors Python EngineBridge.
type EngineBridge struct {
	// eng is read by the off-loop step goroutine (Step) AND written by Attach on a rebuild (the Update
	// loop) — an atomic.Pointer so that swap is race-free (D2a). Callers Load() once and use the captured
	// value; the step goroutine never re-reads it (BuildSnapshot takes the engine explicitly).
	eng atomic.Pointer[engine.Engine]

	// the event-pump channel: a bus callback pushes every event here; pumpCmd drains a batch.
	evq chan events.Event
	// unsubscribe detaches the current engine's bus callback before a rebuild (so a stale bus does
	// not keep pushing). nil before the first engine is attached.
	unsubscribe func()

	// the bus→conversation drain (Python EngineBridge._conv): (role, text) pairs queued from the
	// watched-seam events, popped on the Update loop. Guarded by mu because the bus callback runs on
	// the off-loop step goroutine while drain() runs on the Update loop.
	mu   sync.Mutex
	conv []ConvPair

	// freeze is the bounded event-history tap for the post-session ANALYSIS surface (Track G, G1): a
	// passive ring of the most recent events so a ^P freeze (Shift+Tab on the paused mind) can build a
	// real cognition.RecordFromFrozen of the RUNNING session — not the synthetic SampleAnalysisRecord.
	// It is observation-only (a snapshot copy off the live bus); it never feeds back onto the bus and
	// never touches engine state, so it is byte-identical to not tapping. Guarded by mu (the bus
	// callback runs on the step goroutine; FreezeRecord runs on the Update loop). Survives a rebuild
	// (the freeze window is per-bridge, spanning engine swaps) — Attach re-taps the new bus.
	freeze     []events.Event
	freezeHead int
	freezeLen  int
	freezeOn   bool // gate: capture only when tui.session_record is on (set at Attach from the engine)
}

// NewBridge builds a bridge around a ready engine and subscribes its bus. The engine must already be
// constructed (the app/CLI owns substrate resolution — there is no offline path, DESIGN §8). Pass the
// engine; the bridge owns the bus drain from here on.
func NewBridge(eng *engine.Engine) *EngineBridge {
	b := &EngineBridge{evq: make(chan events.Event, evqCapacity)}
	b.Attach(eng)
	return b
}

// Attach (re)points the bridge at an engine, subscribing its bus to the event pump + the conversation
// drain. A prior subscription is detached first. Called on construction and after a live engine
// rebuild (Python EngineBridge.build re-subscribes the new bus). The channel is preserved across the
// swap so an in-flight pumpCmd is unaffected.
func (b *EngineBridge) Attach(eng *engine.Engine) {
	if b.unsubscribe != nil {
		b.unsubscribe()
		b.unsubscribe = nil
	}
	b.eng.Store(eng)
	if eng == nil {
		return
	}
	// gate the freeze tap on tui.session_record (G1): default OFF ⇒ no capture, no ring, byte-identical.
	b.mu.Lock()
	b.freezeOn = eng.Features() != nil && eng.Features().Tui.SessionRecord
	b.mu.Unlock()
	b.unsubscribe = eng.Bus().Subscribe(func(ev events.Event) {
		// push to the event pump (non-blocking: drop on a full buffer rather than stall the
		// deterministic synchronous bus — the replay ring still holds the full history for the
		// trace panel, so a dropped pump event is only a cosmetic miss, never a state loss).
		select {
		case b.evq <- ev:
		default:
		}
		// queue the few events that belong in the open chat (the watched seam made open).
		if pair, ok := conversationPair(ev); ok {
			b.mu.Lock()
			b.conv = append(b.conv, pair)
			b.mu.Unlock()
		}
		// tap the bounded freeze ring for the ANALYSIS surface (G1): retain the most recent event so a
		// ^P freeze reconstructs the running session. Passive — a copy off the live bus, never fed back.
		b.captureFreeze(ev)
	})
}

// captureFreeze appends one event to the bounded freeze ring (a passive copy off the live bus). The
// ring is allocated lazily so a bridge that never freezes pays nothing; once full it overwrites the
// oldest, keeping the most recent freezeCap events (the scrubbable window). Guarded by mu.
func (b *EngineBridge) captureFreeze(ev events.Event) {
	b.mu.Lock()
	if !b.freezeOn {
		b.mu.Unlock()
		return // tui.session_record OFF (default) ⇒ no capture, no allocation, byte-identical
	}
	if b.freeze == nil {
		b.freeze = make([]events.Event, freezeCap)
	}
	b.freeze[b.freezeHead] = ev
	b.freezeHead = (b.freezeHead + 1) % freezeCap
	if b.freezeLen < freezeCap {
		b.freezeLen++
	}
	b.mu.Unlock()
}

// FreezeEvents returns a snapshot of the freeze ring in emission order (oldest-to-newest), the bounded
// window of the running session the ANALYSIS surface freezes. A copy, so the caller may hold it across
// ticks without racing the live tap. Empty before the first event (a fresh, un-stepped engine).
func (b *EngineBridge) FreezeEvents() []events.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]events.Event, b.freezeLen)
	for i := 0; i < b.freezeLen; i++ {
		// the oldest entry sits at freezeHead when the ring is full; before full, entries start at 0.
		idx := i
		if b.freezeLen == freezeCap {
			idx = (b.freezeHead + i) % freezeCap
		}
		out[i] = b.freeze[idx]
	}
	return out
}

// FreezeRecord reconstructs a real cognition.AnalysisRecord from the frozen live session — the G1
// data path that replaces the synthetic SampleAnalysisRecord when the analysis surface is opened on a
// running (paused) mind. It funnels the captured event window through cognition.RecordFromFrozen (the
// same core the on-disk loader uses), with no SignalFrame series (the live TUI bridge does not run the
// G0 recorder; the events still drive the stimulus index, decision history, reward ledger, scoreboard,
// and impulse capture — the cognition the benchmark reads). name labels the record header.
func (b *EngineBridge) FreezeRecord(name string) cognition.AnalysisRecord {
	rec := cognition.RecordFromFrozen(b.FreezeEvents(), nil)
	rec.Name = name
	if eng := b.eng.Load(); eng != nil {
		rec.Substrate = eng.SubstrateClass()
	}
	return rec
}

// Engine exposes the wrapped engine for the app's read-side queries (lifecycle steppability, the
// backend label for the header, etc.). The app reads; it does not drive the engine except through
// the bridge's Cmds. nil when no model is reachable.
func (b *EngineBridge) Engine() *engine.Engine { return b.eng.Load() }

// Rebuild constructs a FRESH engine from cfg (re-resolving the thinking substrate, DESIGN §8) off the
// newSessionEngine constructs the engine for the LIVE interactive TUI session: it turns RESUME on and
// wires the real-world sense SEAMS (clock/host), so the product TUI senses + resumes exactly like the CLI
// `run` path (cmd/thought newEngineWith). The engine stays headless-pure — the Wall seams enter only here
// at the edge; the go-test path builds engines via engine.NewEngine directly (no seams wired), so the
// suite stays time-blind/deterministic. The backend is nil: the product path resolves the real substrate
// inside NewEngine. Resume is a no-op without a Store; a --state / awake-profile session continues its
// prior cognitive cursor. Must set Resume BEFORE NewEngine (loadResume runs there).
func newSessionEngine(cfg engine.EngineConfig) (*engine.Engine, error) {
	if cfg.Features == nil {
		f := config.AllOn()
		cfg.Features = &f
	}
	cfg.Features.Persist.Resume = true
	// WEB-LANE BUNDLE (capability-enhancement T1.1/T1.4): opting into web_search auto-includes the two
	// validated web-lane improvements (query_formulation + fetch_url) — the --web config the +5pp
	// HotpotQA-fullwiki lift was measured on. Edge-only (like Resume), no-op when web_search is off.
	cfg.Features.EnableWebLaneBundle()
	eng, err := engine.NewEngine(&cfg, nil)
	if err != nil {
		return nil, err
	}
	eng.SetClock(clock.Wall{}, 0)
	eng.SetHost(host.Wall{})
	// fetch_web (follow-up #15 — the OUTWARD distal sense): wire the real Web seam so flipping sense.web ON
	// in the TUI just works. The sense.web knob DEFAULTS OFF, so the wired seam stays DORMANT (no fetch)
	// until flipped — the kitty UAT runs offline (--backend test) and never touches the network unless the
	// knob is deliberately turned on.
	eng.SetWeb(web.NewWall())
	// fetch_url (T1.4 — the OUTWARD page-FETCH seam, sibling of web): wire the real page fetcher so flipping
	// subconscious.fetch_url ON in the TUI just works. It stays DORMANT (the tool is not registered) until
	// flipped — the kitty UAT runs offline (--backend test) and never touches the network unless the knob is
	// deliberately turned on.
	eng.SetPageFetcher(web.NewPager())
	return eng, nil
}

// Update loop — substrate resolution may block on a model probe. It does NOT swap the engine in
// itself (that would race the Update loop's engine reads); it returns the new engine on the msg and the
// Update handler calls Attach on-loop. On failure the prior engine is kept and the error is reported.
func (b *EngineBridge) Rebuild(cfg engine.EngineConfig) tea.Cmd {
	// Carry the prior engine's cross-session persistence store into the rebuild (the §4.4 live-session
	// data-loss fix): a Settings change used to rebuild the engine and silently drop every learned
	// skill/operator/specialist/belief. Now the FRESH engine re-seeds from the same Store, so the learned
	// state survives the rebuild. First flush the OLD engine's state to disk so anything not yet persisted
	// is captured before the new engine loads it.
	if prior := b.eng.Load(); prior != nil && cfg.Store == nil {
		if st := prior.Store(); st != nil {
			prior.FlushState()
			cfg.Store = st
		}
	}
	return func() tea.Msg {
		eng, err := newSessionEngine(cfg)
		if err != nil {
			return engineRebuiltMsg{ok: false, err: err.Error()}
		}
		return engineRebuiltMsg{eng: eng, ok: true}
	}
}

// substrateClass groups a substrate name into its PROVENANCE class — the namespace its persisted
// registry records are tagged with (Meta.Substrate: llm:<model> vs cc:session vs claude:* vs test). A
// switch ACROSS classes is consequential: it changes what a running worker must be (session), and mixing
// classes in one Store violates the substrate-hygiene rule (S3/S5). local/llm/auto/frontier are all
// model-backed (one "local" class); session/cc, claude*, and test are their own classes.
func substrateClass(s string) string {
	canonical, ok := llm.CanonicalSubstrate(s) // the ONE alias table (llm.SubstrateMenu vocabulary)
	if !ok {
		return "local" // unknown name: the optimistic model-backed guess (resolution will error loudly)
	}
	if canonical == "auto" {
		return "local" // a policy, not a class — the optimistic guess until resolution stamps the truth
	}
	// frontier is its OWN class (split from local 2026-06-12): frontier-minted state must never mix
	// with local-minted state (substrate hygiene), so local<->frontier is a consequential,
	// gate-and-rebuild-fresh change — and /model (an lms affordance) correctly refuses on it.
	return canonical
}

// RebuildFresh rebuilds the engine for a SUBSTRATE-CLASS change WITHOUT carrying the prior Store: the new
// class is a new provenance namespace, so mixing its records into the old Store would contaminate it
// (S5). The prior engine is still flushed first (the OLD class's learned state is saved to its dir) — the
// new engine just starts clean in that dir rather than inheriting cross-class records.
func (b *EngineBridge) RebuildFresh(cfg engine.EngineConfig) tea.Cmd {
	if prior := b.eng.Load(); prior != nil {
		if st := prior.Store(); st != nil {
			prior.FlushState()
		}
	}
	cfg.Store = nil // do NOT carry the prior Store across the class boundary
	return func() tea.Msg {
		eng, err := newSessionEngine(cfg)
		if err != nil {
			return engineRebuiltMsg{ok: false, err: err.Error()}
		}
		return engineRebuiltMsg{eng: eng, ok: true}
	}
}

// ListModelsCmd lists the local LLMs the harness can switch to (off the UI loop — `lms ls` can be slow),
// folding the result back as a modelsListedMsg the App turns into the picker. Each choice carries a
// params·arch·size detail and a loaded flag.
func (b *EngineBridge) ListModelsCmd() tea.Cmd {
	return func() tea.Msg {
		infos, err := llm.ListLocalLLMs()
		if err != nil {
			return modelsListedMsg{err: err.Error()}
		}
		choices := make([]popup.ModelChoice, 0, len(infos))
		for _, m := range infos {
			detail := m.Params
			if m.Arch != "" {
				detail = strings.TrimSpace(detail + " · " + m.Arch)
			}
			detail = strings.TrimPrefix(strings.TrimSpace(detail+" · "+sizeGB(m.SizeGB)), " · ")
			choices = append(choices, popup.ModelChoice{Key: m.Key, Detail: detail, Loaded: m.Loaded})
		}
		return modelsListedMsg{models: choices}
	}
}

// PlanSwitchCmd gathers the memory picture for switching to targetKey (off-loop): what is currently
// resident (`lms ps`) and the target's load estimate (`lms load --estimate-only`). It pre-checks every
// non-embedder model that isn't the target for unloading (the swap default), and folds back a
// switchPlanMsg the App turns into the "ask me each time" plan screen. The embedder is shown but never
// offered to unload (retrieval keeps it).
func (b *EngineBridge) PlanSwitchCmd(targetKey, targetDetail string) tea.Cmd {
	return func() tea.Msg {
		loaded, err := llm.LoadedModels()
		if err != nil {
			return switchPlanMsg{target: targetKey, err: err.Error()}
		}
		estGB, fits, note := llm.EstimateModel(targetKey)
		rows := make([]popup.LoadedRow, 0, len(loaded))
		for _, m := range loaded {
			row := popup.LoadedRow{ID: m.ID, Label: shortKey(m.Key) + "  " + sizeGB(m.SizeGB), Embedder: m.IsEmbedding}
			if !m.IsEmbedding && m.Key != targetKey && m.ID != targetKey {
				row.Unload = true // swap default: free everything else
			}
			rows = append(rows, row)
		}
		detail := targetDetail
		if estGB > 0 {
			detail = strings.TrimSpace(detail + " · est " + sizeGB(estGB))
		}
		if !fits && note == "" {
			note = "LM Studio estimates this may not fit — unload more, or pick a smaller model"
		}
		return switchPlanMsg{target: targetKey, targetDetail: detail, note: note, rows: rows}
	}
}

// ApplySwitchCmd executes the plan (off-loop): unload the chosen residents FIRST (free space), then
// force-load the target + wait until it is served. Folds back a modelSwitchedMsg the App turns into an
// engine rebuild. A failed unload is best-effort (it must not block the load attempt).
func (b *EngineBridge) ApplySwitchCmd(targetKey string, unload []string) tea.Cmd {
	return func() tea.Msg {
		for _, id := range unload {
			_ = llm.UnloadModel(id)
		}
		served, err := llm.LoadLocalModel(targetKey)
		if err != nil {
			return modelSwitchedMsg{key: targetKey, err: err.Error()}
		}
		return modelSwitchedMsg{key: targetKey, served: served}
	}
}

// -- cognition-model version manager (proposal §6 + §11 Track 1) -----------------------------------
//
// These Cmds run the persist.Store snapshot methods OFF the Update loop (they touch disk) and fold the
// result back as a typed Msg. The engine stays read-only here except via the Store's own snapshot
// methods: Save captures the LIVE learned state (eng.Store().Snapshot()) tagged with the running
// substrate + the seeded tick; Reset/Delete/Baseline mutate the snapshot file; Diff is pure-read. A nil
// store (the in-memory/test path) yields a clean "persistence is off" note, never a crash.

// CogModelsListCmd lists the saved cognition models off-loop (persist.ListSnapshots) and, for each, the
// EXACT structural delta vs the baseline (persist.DiffSnapshots — counts only, zero noise). It folds back
// a cogModelsListedMsg the App turns into (open) the popup. The capability delta is NOT computed here:
// it is the popup's literal "needs K-replay" placeholder (the ±56pp noise needs a K-replay band). open
// distinguishes the initial /models open from a post-action refresh.
func (b *EngineBridge) CogModelsListCmd(baselinePref string, open bool) tea.Cmd {
	return func() tea.Msg {
		eng := b.eng.Load()
		if eng == nil {
			return cogModelsListedMsg{open: open, err: "no engine attached"}
		}
		st := eng.Store()
		if st == nil {
			return cogModelsListedMsg{open: open, err: "persistence is off (in-memory only — no saved cognition models)"}
		}
		metas, err := st.ListSnapshots()
		if err != nil {
			return cogModelsListedMsg{open: open, err: err.Error()}
		}
		// the effective baseline: the caller's tracked preference if it still exists, else the reserved
		// cold-baseline name, else the OLDEST snapshot (ListSnapshots is newest-first, so the last row).
		baseline := resolveBaseline(metas, baselinePref)
		rows := make([]popup.CogModelRow, 0, len(metas))
		for _, m := range metas {
			structural := "—"
			if baseline != "" && m.Name != baseline {
				if diff, derr := st.DiffSnapshots(baseline, m.Name); derr == nil {
					structural = renderStructuralDelta(diff)
				}
			}
			rows = append(rows, popup.CogModelRow{
				Name:        m.Name,
				Runtime:     fmt.Sprintf("tick %d", m.CreatedTick),
				Substrate:   m.Substrate,
				StructuralΔ: structural,
				IsBaseline:  m.Name == baseline,
			})
		}
		return cogModelsListedMsg{
			rows:      rows,
			baseline:  baseline,
			substrate: eng.SubstrateClass(),
			open:      open,
		}
	}
}

// CogModelSaveCmd captures the LIVE learned state as a new named snapshot off-loop, tagged with the
// running substrate + the seeded tick (the same shape engine/persist.go:170 uses for the auto-baseline).
// It folds back a cogModelActionMsg the App turns into a refresh.
func (b *EngineBridge) CogModelSaveCmd(name string) tea.Cmd {
	return func() tea.Msg {
		eng := b.eng.Load()
		if eng == nil {
			return cogModelActionMsg{err: "no engine attached"}
		}
		st := eng.Store()
		if st == nil {
			return cogModelActionMsg{err: "persistence is off — cannot save a cognition model"}
		}
		rec := persist.SnapshotRecord{
			Meta: persist.SnapshotMeta{
				Name:        name,
				Substrate:   eng.BackendLabel(),
				CreatedTick: eng.Bus().Tick,
			},
			Data: *st.Snapshot(),
		}
		if err := st.SaveSnapshot(rec); err != nil {
			return cogModelActionMsg{err: err.Error()}
		}
		return cogModelActionMsg{note: "saved cognition model: " + name}
	}
}

// CogModelResetCmd replaces the LIVE learned state with a named snapshot off-loop (persist.ResetToSnapshot
// — the revert / dev-reset; dev reset = load the cold-baseline version). It folds back a cogModelActionMsg.
func (b *EngineBridge) CogModelResetCmd(name string) tea.Cmd {
	return func() tea.Msg {
		eng := b.eng.Load()
		if eng == nil {
			return cogModelActionMsg{err: "no engine attached"}
		}
		st := eng.Store()
		if st == nil {
			return cogModelActionMsg{err: "persistence is off — nothing to reset"}
		}
		if err := st.ResetToSnapshot(name); err != nil {
			return cogModelActionMsg{err: err.Error()}
		}
		return cogModelActionMsg{note: "reset live state to: " + name}
	}
}

// CogModelDeleteCmd deletes a named snapshot off-loop (persist.DeleteSnapshot).
func (b *EngineBridge) CogModelDeleteCmd(name string) tea.Cmd {
	return func() tea.Msg {
		eng := b.eng.Load()
		if eng == nil {
			return cogModelActionMsg{err: "no engine attached"}
		}
		st := eng.Store()
		if st == nil {
			return cogModelActionMsg{err: "persistence is off — nothing to delete"}
		}
		if err := st.DeleteSnapshot(name); err != nil {
			return cogModelActionMsg{err: err.Error()}
		}
		return cogModelActionMsg{note: "deleted cognition model: " + name}
	}
}

// CogModelDiffCmd diffs two named snapshots off-loop (persist.DiffSnapshots — exact counts) and folds back
// a cogModelActionMsg with the structural delta rendered as the popup's note.
func (b *EngineBridge) CogModelDiffCmd(from, to string) tea.Cmd {
	return func() tea.Msg {
		eng := b.eng.Load()
		if eng == nil {
			return cogModelActionMsg{err: "no engine attached"}
		}
		st := eng.Store()
		if st == nil {
			return cogModelActionMsg{err: "persistence is off — cannot diff"}
		}
		diff, err := st.DiffSnapshots(from, to)
		if err != nil {
			return cogModelActionMsg{err: err.Error()}
		}
		return cogModelActionMsg{note: "diff " + from + " → " + to + ":  " + renderStructuralDelta(diff)}
	}
}

// resolveBaseline picks the effective baseline name from the snapshot metas: the caller's preference if it
// still exists, else the reserved cold-baseline name if present, else the OLDEST snapshot (metas are
// newest-first, so the last entry). "" when there are no snapshots.
func resolveBaseline(metas []persist.SnapshotMeta, pref string) string {
	has := func(name string) bool {
		for _, m := range metas {
			if m.Name == name {
				return true
			}
		}
		return false
	}
	if pref != "" && has(pref) {
		return pref
	}
	if has(popup.ColdBaselineName) {
		return popup.ColdBaselineName
	}
	if n := len(metas); n > 0 {
		return metas[n-1].Name
	}
	return ""
}

// renderStructuralDelta renders a persist.SnapshotDiff as the EXACT, zero-noise structural delta string
// ("+6 skills +3 spec +41 bel") in a stable artifact order. Empty (no change) renders "—". This is the
// trustworthy half of the two-class metric (the capability half is the popup's K-replay placeholder).
func renderStructuralDelta(diff *persist.SnapshotDiff) string {
	if diff == nil {
		return "—"
	}
	// stable order + compact artifact abbreviations.
	type ent struct{ key, abbr string }
	order := []ent{
		{"skills", "skill"}, {"operators", "op"}, {"specialists", "spec"},
		{"episodes", "ep"}, {"beliefs", "bel"}, {"knowledge", "know"},
		{"preferences", "pref"}, {"gate_priors", "prior"},
	}
	var parts []string
	for _, e := range order {
		if n := diff.Added[e.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("+%d %s", n, e.abbr))
		}
	}
	// changed artifacts (a same-key body swap) are noted with a ~ so a revert is legible.
	for _, e := range order {
		if n := diff.Changed[e.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("~%d %s", n, e.abbr))
		}
	}
	for _, e := range order {
		if n := diff.Removed[e.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("-%d %s", n, e.abbr))
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}

// shortKey trims a model key to its last path segment for compact display ("qwen/qwen3.6-27b" ->
// "qwen3.6-27b"), keeping the bare key when it has no slash.
func shortKey(k string) string {
	if i := strings.LastIndex(k, "/"); i >= 0 && i+1 < len(k) {
		return k[i+1:]
	}
	return k
}

// sizeGB formats a model size for the picker detail ("18.0 GB").
func sizeGB(gb float64) string {
	if gb <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f GB", gb)
}

// -- the three off-loop Cmds (DESIGN §4.5) ---------------------------------------------------------

// stepCmd runs ONE engine.Step() off the Update loop (it may block on a slow model call) and returns
// a stepResultMsg, carrying the per-tick result, the end-of-tick snapshot, and the epoch stamped at
// dispatch. Update drops the message when epoch != app.modeEpoch (a stale result after a mode
// switch). The bus callback fires synchronously inside Step on this goroutine, so the events for this
// tick are already queued on evq by the time the result returns. A nil engine yields no Cmd.
func (b *EngineBridge) stepCmd(epoch int) tea.Cmd {
	eng := b.eng.Load()
	if eng == nil {
		return nil
	}
	return func() tea.Msg {
		// the goroutine uses the CAPTURED eng for both the step and the snapshot, so it never reads the
		// shared b.eng pointer (which Attach may swap on a concurrent rebuild) — D2a/D6 state-safety.
		res := eng.Step()
		return stepResultMsg{res: res, snap: b.snapshotOf(eng), epoch: epoch}
	}
}

// animInterval is the animation frame cadence (~30fps): smooth enough for the wave/shimmer/border
// animations at half the cost of 60fps. The animTick is armed only while an animation is active
// (App.animating), so this cadence never runs against a fully idle surface.
const animInterval = 33 * time.Millisecond

// animTickCmd schedules the next animation frame. The app re-arms it from the animTickMsg handler
// while App.animating() holds, and revives it via ensureAnim when a state change starts an animation.
func animTickCmd() tea.Cmd {
	return tea.Tick(animInterval, func(time.Time) tea.Msg { return animTickMsg{} })
}

// stepTickCmd is the self-rescheduling stepping interval. The app re-arms it every tick (DESIGN §4.5
// primitive 2): on stepTickMsg, if no step is in flight and the lifecycle is steppable, dispatch
// stepCmd; always re-arm. Keeping the interval cheap (no work dispatched while idle) keeps idle CPU
// flat. Package-level (no engine state needed).
func stepTickCmd() tea.Cmd {
	return tea.Tick(stepInterval, func(time.Time) tea.Msg { return stepTickMsg{} })
}

// pumpCmd drains a batch of bus events from the pump channel into an eventBatchMsg (DESIGN §4.5
// primitive 3). It blocks for the first event (so it is not a busy-loop), then non-blockingly drains
// up to drainBatch more — events accumulate during the off-loop step and surface atomically. Update
// folds the batch into the panel ViewModel + the chat, then re-issues pumpCmd. Ordering within a
// batch is the engine's deterministic emission order (the bus is synchronous).
func (b *EngineBridge) pumpCmd() tea.Cmd {
	evq := b.evq
	return func() tea.Msg {
		batch := make([]events.Event, 0, drainBatch)
		// block for the first event so the Cmd parks instead of spinning.
		first, ok := <-evq
		if !ok {
			return eventBatchMsg{}
		}
		batch = append(batch, first)
		for len(batch) < drainBatch {
			select {
			case ev, ok := <-evq:
				if !ok {
					return eventBatchMsg{events: batch}
				}
				batch = append(batch, ev)
			default:
				return eventBatchMsg{events: batch}
			}
		}
		return eventBatchMsg{events: batch}
	}
}

// Drain pops every (role, text) the bus has queued for the open chat since the last Drain (called on
// the Update loop, Python EngineBridge.drain). Returns nil when nothing is queued. Thread-safe — the
// bus callback runs on the off-loop step goroutine.
func (b *EngineBridge) Drain() []ConvPair {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.conv) == 0 {
		return nil
	}
	out := b.conv
	b.conv = nil
	return out
}

// -- the end-of-tick snapshot (DESIGN §4.3, §4.5 engine surface) ------------------------------------

// BuildSnapshot reads the engine's read-only accessors and returns the live fields the Python
// panels.render_* read directly off `eng`. It is called at end-of-tick (inside stepCmd, after
// Step returns) so the snapshot reflects this tick. It NEVER mutates the engine; every read is a copy
// or a lower-tier accessor (DESIGN §4.5). A nil engine yields an empty snapshot (the "no model" state).
func (b *EngineBridge) BuildSnapshot() cognition.SnapshotData {
	return b.snapshotOf(b.eng.Load())
}

// snapshotOf builds the end-of-tick snapshot from a SPECIFIC engine (the one the caller captured), so
// the off-loop step goroutine reads its own captured engine rather than the shared b.eng pointer.
func (b *EngineBridge) snapshotOf(e *engine.Engine) cognition.SnapshotData {
	if e == nil {
		return cognition.SnapshotData{}
	}
	snap := cognition.SnapshotData{
		Tick:           e.Bus().Tick,
		Mode:           e.Mode(),
		LifecycleState: e.Lifecycle().State.String(),
		Arousal:        e.Arousal().String(),
		UserWaiting:    e.UserWaiting(),
		Substrate:      e.SubstrateClass(),
	}

	// lifecycle history — last 6, formatted like render_lifecycle ("<STATE pad16> <reason[:30]>").
	hist := e.Lifecycle().History
	from := len(hist) - 6
	if from < 0 {
		from = 0
	}
	for _, h := range hist[from:] {
		reason := h.Reason
		if len(reason) > 30 {
			reason = reason[:30]
		}
		snap.LifecycleHistory = append(snap.LifecycleHistory, h.State.String()+"  "+reason)
	}

	// CONSCIOUS — the thought graph (nil before an episode opens, render_conscious's idle case).
	if g := e.Graph(); g != nil {
		ab := g.ActiveBranch
		snap.ActiveBranch = &ab
		for _, th := range g.ActiveContext() {
			tag := th.Source.String()
			if len(tag) > 3 {
				tag = tag[:3]
			}
			snap.ActiveContext = append(snap.ActiveContext, cognition.ThoughtVM{
				ID:         th.ID,
				Text:       th.Text,
				Source:     th.Source.String(),
				SourceTag:  tag,
				Confidence: th.Confidence,
			})
		}
		// branches + the per-branch value map (render_conscious tree + render_value bars), id-ascending.
		snap.Values = make(map[int]float64, len(g.Branches))
		ids := make([]int, 0, len(g.Branches))
		for id := range g.Branches {
			ids = append(ids, id)
		}
		for _, bid := range sortedBranchIDs(ids) {
			br := g.Branches[bid]
			// the branch gist (the A* frontier read): the COMPRESSED summary if present, else the latest
			// thought's text (so a stashed/expanded branch still shows what line of reasoning it holds).
			gist := ""
			if br.Summary != nil {
				gist = *br.Summary
			} else if n := len(br.ThoughtIDs); n > 0 {
				if t, ok := g.Nodes[br.ThoughtIDs[n-1]]; ok {
					gist = t.Text
				}
			}
			reason := ""
			if br.Reason != nil {
				reason = *br.Reason
			}
			snap.Branches = append(snap.Branches, cognition.BranchVM{
				ID:           br.ID,
				Resolution:   br.Resolution.String(),
				Value:        br.Value,
				Status:       br.Status.String(),
				Gist:         gist,
				Depth:        g.Depth(bid),
				ThoughtCount: len(br.ThoughtIDs),
				Reason:       reason,
			})
			snap.Values[br.ID] = br.Value
		}
	}

	// CRITIC — the executive's last decision (render_critic; zero-value before any decision).
	m := e.Controller().LastMeta
	if m.Decision != "" {
		stopKind := ""
		if m.StopKind != nil {
			stopKind = *m.StopKind
		}
		snap.LastMeta = &cognition.ControllerMetaVM{
			Decision:          m.Decision,
			StopKind:          stopKind,
			Reason:            m.Reason,
			BranchExhausted:   m.BranchExhausted,
			LoopExhausted:     m.LoopExhausted,
			Flagged:           m.Flagged,
			NeedsGroundTruth:  m.NeedsGroundTruth,
			Mode:              e.Controller().Mode(),
			Ambiguity:         m.Ambiguity,
			Escalated:         m.Escalated,
			HeuristicDecision: m.HeuristicDecision,
			LLMDecision:       m.LLMDecision,
			Agree:             m.Agree,
		}
	}

	// SUBCONSCIOUS — the live recognised workflow (render_subconscious's workflow line).
	if wf := e.Workflow(); wf != nil && wf.Recognized() {
		snap.Workflow = &cognition.WorkflowVM{
			Name:       wf.Name,
			PhaseIndex: wf.I(),
			OpName:     wf.Current().OpName,
			Recognized: true,
		}
	}

	// ACTION / WATCHED seam (render_action).
	snap.ActionOutstanding = e.ActionOutstanding()
	snap.ActionLatencyTicks = e.ActionLatency()
	snap.ActedBranches = e.ActedBranches()
	snap.LastBridge = e.LastBridge()
	snap.LastFabricated = e.LastFabricated()

	// DURABILITY — the regulator metrics + history + the stability checklist (render_durability).
	r := e.Regulator()
	snap.Theta = r.Theta()
	snap.LamBar = r.LamBar()
	snap.LamHat = r.LamHat()
	snap.N = r.N()
	snap.Mu = r.Mu()
	snap.U = r.Util()
	for _, d := range r.History() {
		snap.RegHistory = append(snap.RegHistory, cognition.RegSnapshotVM{
			Theta: asFloat(d["theta"]),
			N:     asFloat(d["n"]),
		})
	}
	for _, c := range r.Stability(e.Mode(), false) {
		snap.Stability = append(snap.Stability, cognition.StabilityCheckVM{
			Name: c.Name, Pass: c.Pass, NA: c.NA,
		})
	}

	// MEMORY — the learned/consolidated state (render_memory): the convertibility organ's minted
	// specialists/skills + gate priors, the operator catalog's minted operators, and the
	// conversation-memory turn count. Copies, so the panel never mutates engine state.
	if cv := e.Convert(); cv != nil {
		snap.MintedPrimitiveSubAgents = append([]string(nil), cv.Minted...)
		snap.Demoted = append([]string(nil), cv.Demoted...)
		snap.MintedSkills = append([]string(nil), cv.MintedSkill...)
		if len(cv.GatePrior) > 0 {
			snap.GatePriors = make(map[string]float64, len(cv.GatePrior))
			for dom, w := range cv.GatePrior {
				snap.GatePriors[dom] = w
			}
		}
		// the LIVE learning candidates (toward minting), so convertibility is watchable before it mints.
		for _, p := range cv.Patterns() {
			snap.ConvCandidates = append(snap.ConvCandidates, cognition.ConvCandidateVM{
				GoalKey: p.GoalKey, Generated: p.Generated, Value: p.Value, Minted: p.Minted,
			})
		}
		for _, pr := range cv.ProgramRuns() {
			snap.ConvPrograms = append(snap.ConvPrograms, cognition.ProgramRunVM{
				Shape: pr.Shape, Count: pr.Count, Minted: pr.Minted,
			})
		}
		snap.DomainTally = cv.DomainTally()
		snap.MetacogRuns = cv.MetacogRuns()
		snap.MintAfter = cv.MintAfter()
		snap.MintValue = cv.MintValue()
		snap.MetacogAfter = cv.MetacogAfter()
	}
	if cat := e.Catalog(); cat != nil {
		snap.MintedOperators = cat.Minted()
		snap.OperatorTotal = len(cat.Names())
	}
	snap.TranscriptTurns = len(e.Transcript())

	// declarative memory sizes (the live registries) for the compact metrics row.
	snap.EpisodicCount = e.Episodic().Len()
	snap.SemanticCount = e.Semantic().Len()
	snap.PersonCount = len(e.Person().Applied())
	snap.RetrieverMode = e.RetrieverMode()

	return snap
}

// -- helpers ---------------------------------------------------------------------------------------

// conversationPair maps a bus event to a chat (role, text) pair, returning ok=false for the events
// that are not part of the open conversation. Mirrors Python EngineBridge._on_event: the user's
// inbound percept (a "port"/received event), the harness's response, a substantive reality-check
// observation, and the cognition-surfacing skill events. The substantive screen for observations is
// left to the engine's emitted summary here; the base reproduces the role routing.
func conversationPair(ev events.Event) (ConvPair, bool) {
	switch ev.Kind {
	case events.Port:
		if strings.HasPrefix(ev.Summary, "received") {
			if text, _ := ev.Data["text"].(string); strings.TrimSpace(text) != "" {
				return ConvPair{Role: "user", Text: text}, true
			}
		}
	case events.Respond:
		// outreach (the mind speaking unprompted) renders under a DISTINCT chrome header, with the
		// clean model-voiced text from Data — never the "(unprompted)" tag baked into the words.
		if k, _ := ev.Data["kind"].(string); k == "outreach" {
			text, _ := ev.Data["text"].(string)
			if text == "" {
				text = strings.TrimPrefix(ev.Summary, "(unprompted) ")
			}
			return ConvPair{Role: "outreach", Text: text}, true
		}
		return ConvPair{Role: "harness", Text: ev.Summary}, true
	case events.Observation:
		if async, _ := ev.Data["async_"].(bool); !async {
			text := strings.TrimPrefix(ev.Summary, "reality: ")
			return ConvPair{Role: "action", Text: text}, true
		}
	case events.SkillMatch:
		skill, _ := ev.Data["skill"].(string)
		shape, _ := ev.Data["shape"].(string)
		return ConvPair{Role: "sys", Text: "skill ▸ " + skill + "  ·  " + shape}, true
	case events.SkillMint:
		name, _ := ev.Data["name"].(string)
		return ConvPair{Role: "sys", Text: "learned a new skill ▸ " + name}, true
	case events.Decision:
		// the one conversationally-relevant decision: the mind left the USER's line WITHOUT answering
		// (set_aside, stamped by the engine). Endogenous wander lines finishing are panel detail and
		// never reach chat (the "finished that line of thought" spam, UAT 2026-06-12). Terse chrome.
		if aside, _ := ev.Data["set_aside"].(bool); aside {
			return ConvPair{Role: "sys", Text: "set aside — unanswered"}, true
		}
	}
	return ConvPair{}, false
}

// sortedBranchIDs returns the branch ids ascending (Python dict insertion order == id-ascending,
// since bids are monotonic). The graph hands its ids unordered (Go map iteration); sort for a
// deterministic panel render.
func sortedBranchIDs(ids []int) []int {
	out := append([]int(nil), ids...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// asFloat coerces a regulator history value (an events.D entry, already a rounded float64) to float64,
// tolerating a non-float entry (0.0) rather than panicking — the snapshot is best-effort cosmetic.
func asFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
