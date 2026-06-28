package engine

import (
	"fmt"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/convert"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/keyframe"
	"github.com/berttrycoding/thought-harness/internal/knowledge"
	"github.com/berttrycoding/thought-harness/internal/memory"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// persist.go wires the M4 cross-session persistence + lifecycle/cleanup curator into the engine
// (representation-space-rebuild.md §4.4/§4.5). The Store is INJECTED (EngineConfig.Store; nil ⇒ in-memory
// only — tests/heuristic never touch disk), so the engine stays headless-pure: it only CALLS the store;
// the store's file I/O lives in internal/persist. The flow:
//
//	NewEngine  →  loadState()    re-seed every registry from the store BEFORE episode 1
//	IDLE/ASLEEP →  curateState()  snapshot learned state → Curator (pure) → Replace → debounced Flush
//	DONE        →  flushState()   final persist of the curated learned set
//
// Persistence is OPT-IN: a nil Store makes every hook a no-op, so the bare/test path is byte-identical to
// pre-M4 and the scenario goldens are unchanged. Determinism is preserved — the store/curator read+write
// but introduce NO wall-clock; every tick is the seeded bus tick.

// loadState re-seeds the registries from the injected store (called once in NewEngine, after every
// registry is built, before the first episode). nil store / persistence-disabled ⇒ no-op. NEVER-FABRICATE
// survives: only grounded episodes/beliefs/knowledge are re-admitted (the store + the registry Seed both
// enforce it). The persist.load event is emitted by the store's Load.
func (e *Engine) loadState() {
	if e.cfg.Store == nil || !e.features.Persist.Enabled {
		return
	}
	e.cfg.Store.SetEmit(e.bus.Emit)
	snap, err := e.cfg.Store.Load()
	if err != nil || snap == nil {
		return
	}
	// capture the load summary — the engine emits persist.load DEFERRED on the first Step (Load runs in
	// NewEngine before the CLI/TUI sinks subscribe; emitting now would be lost). Only when something loaded.
	if total := len(snap.Skills) + len(snap.Operators) + len(snap.Specialists) + len(snap.Episodes) +
		len(snap.Beliefs) + len(snap.Knowledge) + len(snap.Preferences); total > 0 || snap.Priors != nil {
		e.loadSummary = events.D{
			"skills": len(snap.Skills), "operators": len(snap.Operators),
			"specialists": len(snap.Specialists), "episodes": len(snap.Episodes),
			"beliefs": len(snap.Beliefs), "knowledge": len(snap.Knowledge),
			"preferences": len(snap.Preferences), "priors": snap.Priors != nil,
		}
	}

	// operators — re-mint each persisted minted operator into the live catalog (active only).
	for _, r := range snap.Operators {
		if r.Meta.Status != persist.StatusActive {
			continue
		}
		e.catalog.MintWithMove(r.Name, r.Family, r.Intent, cognition.Move(r.Move))
	}
	// skills — reconstruct each minted skill's WHOLE Program body from its ToDict envelope + re-mint it.
	// Save wrote sk.Body.ToDict() (the {goal, synthesized, rationale, root} envelope), so the load must
	// use the matching whole-program deserializer ProgramFromDict — NOT NodeFromDict(r.Body) directly,
	// which expects a NODE dict keyed on "kind" and errors "unknown program node kind: None" on the
	// envelope (the trace->skill round-trip bug that made a real minted-skill body unreloadable).
	for _, r := range snap.Skills {
		if r.Meta.Status != persist.StatusActive || r.Body == nil {
			continue
		}
		body, perr := cognition.ProgramFromDict(r.Body)
		if perr != nil {
			continue // a malformed body is skipped, never a crash
		}
		body.Synthesized = true // a re-minted skill is always Synthesized=true (so Match recalls it)
		e.skills.Mint(r.Name, r.Triggers, body, r.Tier, r.Description)
	}
	// minted specialists + gate priors — restored into convertibility (which re-registers the specialists
	// and rebuilds the keep-or-revert bookkeeping). A demoted record stays dark.
	if len(snap.Specialists) > 0 {
		recs := make([]convert.SpecialistRecord, 0, len(snap.Specialists))
		for _, r := range snap.Specialists {
			if r.Meta.Status == persist.StatusArchived {
				continue
			}
			recs = append(recs, convert.SpecialistRecord{
				Domain: r.Domain, GoalKey: r.GoalKey, Triggers: r.Triggers, Answer: r.Answer,
				Relevance: r.Relevance, Generated: r.Generated, Value: r.Value,
				Demoted: r.Demoted || r.Meta.Status == persist.StatusDemoted,
			})
		}
		e.convert.SeedPrimitiveSubAgents(recs)
	}
	if snap.Priors != nil {
		e.convert.SeedGatePriors(snap.Priors.Priors)
	}
	// trace->skill recurrence counters — restore the per-goal program-run COUNTS so a fresh-engine
	// episode RESUMES the tally (1->2->3) instead of resetting to 1, which is the primary blocker that
	// made the autonomous mint-from-recurrence never fire (ProbeReplays / per-episode engines reset the
	// in-memory count). Behind the SkillMint knob: a recurrence counter is dead data when the skill mint
	// is off, so it is neither seeded nor persisted then (and the default no-state run is untouched —
	// the Store is nil there). The body round-trips via cognition.ProgramFromDict (active records only).
	if len(snap.ProgramRuns) > 0 && e.features.Convert.SkillMint {
		recs := make([]convert.ProgramRunRecord, 0, len(snap.ProgramRuns))
		for _, r := range snap.ProgramRuns {
			if r.Meta.Status != persist.StatusActive || r.Body == nil {
				continue
			}
			prog, perr := cognition.ProgramFromDict(r.Body)
			if perr != nil {
				continue // a malformed body is skipped, never a crash
			}
			prog.Synthesized = true
			recs = append(recs, convert.ProgramRunRecord{
				GoalKey: r.GoalKey, Shape: r.Shape, Count: r.Count, Minted: r.Minted, Program: prog,
			})
		}
		e.convert.SeedProgramRuns(recs)
	}
	// episodic / semantic / knowledge / person — re-seed verbatim (bi-temporal fields preserved). Active
	// records seed the live recall set; demoted/dormant are kept for the curator but excluded here.
	for _, r := range snap.Episodes {
		if r.Meta.Status != persist.StatusActive {
			continue
		}
		e.episodic.Seed(memory.Episode{
			Goal: r.Goal, Entities: r.Entities, Outcome: r.Outcome, Grounded: r.Meta.Grounded,
			Value: r.Value, Tick: r.Tick,
		})
	}
	for _, r := range snap.Beliefs {
		if r.Meta.Status == persist.StatusArchived || r.Meta.Status == persist.StatusDormant {
			continue
		}
		e.semantic.Seed(memory.Belief{
			Statement: r.Statement, Entities: r.Entities, Source: r.Source, Grounded: r.Meta.Grounded,
			ValidFrom: r.ValidFrom, ValidTo: r.ValidTo,
		})
	}
	for _, r := range snap.Knowledge {
		if r.Meta.Status == persist.StatusArchived || r.Meta.Status == persist.StatusDormant {
			continue
		}
		e.knowledge.Seed(knowledge.Knowledge{
			Statement: r.Statement, Kind: r.Kind, Entities: r.Entities, Source: r.Source,
			Grounded: r.Meta.Grounded, Trust: r.Trust, ValidFrom: r.ValidFrom, ValidTo: r.ValidTo,
		})
	}
	for _, r := range snap.Preferences {
		e.person.Seed(memory.Preference{
			Trait: r.Trait, Value: r.Value, Evidence: r.Count, Learned: r.Learned,
		})
	}
	// Loop-closure / recurrence keyframe DB (Track F, F-M7): when persistence.keyframe_db is ON, build the
	// recurrence index and SEED it from the prior run's keyframes, so a re-entry this run can be a CROSS-RUN
	// loop closure (the un-persisted-recurrence gap G3). Default OFF ⇒ e.keyframes stays nil ⇒ no observe,
	// no event ⇒ byte-identical. The seed boundary is the latest restored LastSeenTick (everything restored
	// pre-dates this run), so a re-sight after the boundary is tagged cross_run.
	e.seedKeyframes(snap)

	// The self-change ledger records only NEW self-changes: seed the baseline at the post-load mint
	// count, so re-loading prior-session mints is not logged as fresh self-modification (W1).
	e.ledgerRecorded = e.mintCount()
	e.ensureBaselineSnapshot()
}

// seedKeyframes builds the loop-closure recurrence DB (F-M7) and restores the prior run's keyframes
// from the snapshot, when persistence.keyframe_db is ON. Active records only (a demoted/archived
// keyframe is kept on disk for audit but not re-seeded into the live index). The seed boundary is the
// latest restored LastSeenTick, so any re-entry on a later tick is a CROSS-RUN closure. nil store /
// keyframe_db OFF ⇒ e.keyframes stays nil ⇒ the recurrence wire is inert (byte-identical).
func (e *Engine) seedKeyframes(snap *persist.Snapshot) {
	if !e.features.Persist.KeyframeDB {
		return
	}
	e.keyframes = keyframe.New(0)
	if snap == nil || len(snap.Keyframes) == 0 {
		return
	}
	frames := make([]keyframe.Keyframe, 0, len(snap.Keyframes))
	boundary := 0
	for _, r := range snap.Keyframes {
		if r.Meta.Status != persist.StatusActive || r.Descriptor == "" {
			continue
		}
		frames = append(frames, keyframe.Keyframe{
			Descriptor: r.Descriptor, Gist: r.Gist, Count: r.Count, Closures: r.Closures,
			FirstSeenTick: r.FirstSeenTick, LastSeenTick: r.LastSeenTick, Substrate: r.Meta.Substrate,
		})
		if r.LastSeenTick > boundary {
			boundary = r.LastSeenTick
		}
	}
	e.keyframes.Seed(frames, boundary)
}

// ensureBaselineSnapshot takes the once-per-session PRE-MINT snapshot (W1) — a clean revert point so
// a session's self-changes (recordLedger) can be reverted as a batch. Only when the ledger is on,
// auto_snapshot is enabled, and no named snapshot exists yet (an operator/campaign baseline already
// present is preferred — never clobbered). Runs at session start, before any mint. nil store ⇒ no-op.
func (e *Engine) ensureBaselineSnapshot() {
	st := e.cfg.Store
	if st == nil || !e.features.Ledger.Enabled || !e.features.Ledger.AutoSnapshot || e.ledgerBaselined {
		return
	}
	e.ledgerBaselined = true
	if metas, err := st.ListSnapshots(); err != nil || len(metas) > 0 {
		return // a baseline already exists (operator- or prior-session-made) — don't clobber it
	}
	_ = st.SaveSnapshot(persist.SnapshotRecord{
		Meta: persist.SnapshotMeta{Name: ledgerBaselineSnapshot, Substrate: e.backendLabel, CreatedTick: e.bus.Tick},
		Data: *st.Snapshot(),
	})
}

// persistLearned snapshots the live learned state into the store (debounced — called at consolidation +
// before a flush, NOT every tick). It is the §4.4 "Save* beside each mint" obligation gathered into one
// deterministic pass over the registries, so the engine loop stays clean and no mint site needs an extra
// call. nil store / persistence-disabled ⇒ no-op. Each Save* deduplicates by content hash, so re-saving
// an unchanged artifact is idempotent (no unbounded growth). NEVER-FABRICATE holds: an ungrounded
// episode/belief/knowledge is rejected by the store (ErrUngrounded), so it never reaches disk.
func (e *Engine) persistLearned() {
	st := e.cfg.Store
	if st == nil || !e.features.Persist.Enabled {
		return
	}
	now := e.bus.Tick
	// Substrate provenance: every record saved this pass is stamped with WHO was thinking
	// (the backend display name), so a frontier-derived dataset stays distinguishable from a
	// local-minted one (mapping doc §6.2 — the re-localization prerequisite).
	sub := e.backendLabel

	// minted operators (active catalog growth).
	for _, name := range e.catalog.Minted() {
		spec, ok := e.catalog.Get(name)
		if !ok {
			continue
		}
		st.SaveOperator(persist.OpRecord{
			Meta: persist.Meta{Grounded: true, LastUsedTick: now, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Name: spec.Name, Family: spec.Family, Intent: spec.Intent, Move: string(spec.Move),
		})
	}
	// minted skills (with their Program body, round-tripped via ToDict).
	for _, name := range e.skills.Minted() {
		sk, ok := e.skills.Get(name)
		if !ok {
			continue
		}
		st.SaveSkill(persist.SkillRecord{
			Meta: persist.Meta{Grounded: true, LastUsedTick: now, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Name: sk.Name, Tier: sk.Tier, Triggers: sk.Triggers,
			Body: sk.Body.ToDict(), Description: sk.Description,
		})
	}
	// minted specialists + the compiled gate priors (from convertibility's export).
	for _, r := range e.convert.ExportSpecialists() {
		st.SaveSpecialist(persist.SpecialistRecord{
			Meta:   persist.Meta{Grounded: true, LastUsedTick: now, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Domain: r.Domain, GoalKey: r.GoalKey, Triggers: r.Triggers, Answer: r.Answer,
			Relevance: r.Relevance, Generated: r.Generated, Value: r.Value, Demoted: r.Demoted,
		})
	}
	if priors := e.convert.ExportGatePriors(); len(priors) > 0 {
		st.SaveGatePriors(priors, now)
	}
	// loop-closure / recurrence keyframe DB (F-M7) — export the whole live recurrence index so the
	// count + bi-temporal window + substrate tag accumulate across runs (the cross-session loop closure).
	// nil when persistence.keyframe_db is OFF ⇒ nothing saved (byte-identical). The descriptor is the
	// dedup/upsert key; the recurrence count is itself the "grounding" (a count is a measured fact, not a
	// fabricated claim), so Grounded=true is honest.
	if e.keyframes != nil {
		for _, kf := range e.keyframes.Export() {
			st.SaveKeyframe(persist.KeyframeRecord{
				Meta:          persist.Meta{Grounded: true, LastUsedTick: kf.LastSeenTick, UseCount: kf.Count, Status: persist.StatusActive, Substrate: sub},
				Descriptor:    kf.Descriptor,
				Gist:          kf.Gist,
				Count:         kf.Count,
				Closures:      kf.Closures,
				FirstSeenTick: kf.FirstSeenTick,
				LastSeenTick:  kf.LastSeenTick,
			})
		}
	}
	// trace->skill recurrence counters — persist the per-goal program-run COUNTS + bodies so the count
	// accumulates across episodes that share a stateDir (1->2->3 -> mints on the 3rd). Behind the
	// SkillMint knob (a counter is dead data when the skill mint is off). The body round-trips via the
	// whole-program ToDict envelope (ProgramFromDict on reload). A run whose body is not the concrete
	// *cognition.Program is skipped (it cannot re-mint; defensive — convert always passes that type).
	if e.features.Convert.SkillMint {
		for _, r := range e.convert.ExportProgramRuns() {
			prog, ok := asCognitionProgram(r.Program)
			if !ok {
				continue
			}
			status := persist.StatusActive
			st.SaveProgramRun(persist.ProgramRunRecord{
				Meta:    persist.Meta{Grounded: true, LastUsedTick: now, UseCount: 1, Status: status, Substrate: sub},
				GoalKey: r.GoalKey, Shape: r.Shape, Count: r.Count, Minted: r.Minted, Body: prog.ToDict(),
			})
		}
	}
	// declarative memory + knowledge + person — grounded-only (the store re-checks).
	for _, ep := range e.episodic.All() {
		st.SaveEpisode(persist.EpisodeRecord{
			Meta: persist.Meta{Grounded: ep.Grounded, LastUsedTick: ep.Tick, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Goal: ep.Goal, Entities: ep.Entities, Outcome: ep.Outcome, Value: ep.Value, Tick: ep.Tick,
		})
	}
	for _, b := range e.semanticAll() {
		st.SaveBelief(persist.BeliefRecord{
			Meta:      persist.Meta{Grounded: b.Grounded, LastUsedTick: now, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Statement: b.Statement, Entities: b.Entities, Source: b.Source,
			ValidFrom: b.ValidFrom, ValidTo: b.ValidTo,
		})
	}
	for _, k := range e.knowledgeAll() {
		st.SaveKnowledge(persist.KnowledgeRecord{
			Meta:      persist.Meta{Grounded: k.Grounded, LastUsedTick: now, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Statement: k.Statement, Kind: k.Kind, Entities: k.Entities, Source: k.Source,
			Trust: k.Trust, ValidFrom: k.ValidFrom, ValidTo: k.ValidTo,
		})
	}
	for _, p := range e.person.All() {
		st.SavePreference(persist.PreferenceRecord{
			Meta:  persist.Meta{Grounded: true, LastUsedTick: now, UseCount: 1, Status: persist.StatusActive, Substrate: sub},
			Trait: p.Trait, Value: p.Value, Count: p.Evidence, Learned: p.Learned,
		})
	}
}

// curateState runs the lifecycle/cleanup curator at IDLE/ASLEEP consolidation (off the hot path): it
// persists the live learned state, then runs the PURE Curator (versioning/dedup/decay/demotion/GC/caps)
// over the snapshot + the seeded tick, writes the curated set back, and Flushes (debounced). nil store /
// persistence-disabled ⇒ no-op; curator-disabled ⇒ persist + flush WITHOUT the cleanup pass (a toggle is
// a bypass, not a delete). Determinism: the curator's decisions are a function of (snapshot, tick) only.
func (e *Engine) curateState() {
	st := e.cfg.Store
	if st == nil || !e.features.Persist.Enabled {
		return
	}
	e.persistLearned()
	if e.features.Persist.Curator {
		if e.curator == nil {
			e.curator = persist.NewCurator(nil, e.bus.Emit)
		}
		curated := e.curator.Curate(st.Snapshot(), e.bus.Tick)
		st.Replace(curated)
	}
	if err := st.Flush(); err == nil {
		e.bus.Emit(events.PersistSave, "persist: flushed learned state", events.D{"tick": e.bus.Tick})
		e.recordLedger(st) // W1: record this consolidation's self-changes (mints) in the ledger
	}
	e.saveResumeCursor() // deterministic-resume cursor (RNG states + tick) alongside the learned flush (resume.go)
	e.savePerceptLog()   // deterministic percept-log (recorded boundary senses) alongside the flush (percept.go)
	e.saveGraphSpine()   // compressed graph spine (Track 2: where-I-was, light re-orientation) alongside the flush (graph_spine.go)
}

// mintCount is the running total of self-changes the engine has made to its own registries —
// minted operators + skills + specialists (pattern + triple). It is the quantity the self-change
// ledger tracks: a growth means the engine changed itself, the unit the SELF·EVOLUTION panel and
// the campaign's keep-or-revert reason about.
func (e *Engine) mintCount() int {
	return len(e.catalog.Minted()) + len(e.skills.Minted()) +
		len(e.convert.Minted) + len(e.convert.MintedTriple)
}

// recordLedger appends a self-change ledger entry (W1) when a consolidation actually minted
// something new (the total grew since the last record), gated by the ledger config (SAFE mode by
// default). All runtime mints are REGISTRY CONTENT (scope S1), which the SAFE safety mode permits;
// a scope the current mode forbids is refused + surfaced (the governance boundary — no such mint
// exists today, but the check is the future-proof guard). The revert handle names the session's
// auto-baseline snapshot so a batch of self-changes is revertible. nil store / ledger-disabled ⇒ no-op.
func (e *Engine) recordLedger(st persist.Store) {
	if st == nil || !e.features.Ledger.Enabled {
		return
	}
	total := e.mintCount()
	if total <= e.ledgerRecorded {
		return // no NEW self-change this consolidation
	}
	mode := persist.SafetyMode(e.features.Ledger.SafetyMode)
	scope := persist.LedgerScopeS1 // every runtime mint is registry content
	cfg := persist.LedgerConfig{SafetyMode: mode, RequireGate: e.features.Ledger.RequireGate}
	if !cfg.ScopeAllowed(scope) {
		// the configured safety mode forbids this scope — surface the refusal, never silently mint.
		e.bus.Emit(events.RegistryBatch, "ledger: refused "+string(scope)+" under "+string(mode),
			events.D{"action": "refused", "scope": string(scope), "safety_mode": string(mode)})
		return
	}
	revert := e.ledgerRevertHandle(st)
	gate := "grounded-only"
	if e.features.Persist.Curator {
		gate = "grounded-only + curator"
	}
	delta := total - e.ledgerRecorded
	_ = st.SaveLedgerEntry(persist.LedgerEntry{
		Tick:         e.bus.Tick,
		Scope:        scope,
		SafetyMode:   mode,
		Description:  fmt.Sprintf("minted %d self-change(s) (total %d: ops %d, skills %d, specialists %d)", delta, total, len(e.catalog.Minted()), len(e.skills.Minted()), len(e.convert.Minted)+len(e.convert.MintedTriple)),
		Evidence:     "grounded consolidation",
		GatePassed:   gate,
		RevertHandle: revert,
		Substrate:    e.backendLabel,
		SubmittedBy:  "cognition",
	})
	e.ledgerRecorded = total
	// H-SB2: close the self-improvement loop. The freq×value heuristic admitted this batch (the cheap
	// PRE-FILTER); now MEASURE its SelfBench fitness delta + RE-PASS the durability gate, and keep-or-revert
	// against the pre-mint baseline. Default propose-and-gate; closed-loop self-reverts on fail. Opt-in:
	// OFF ⇒ no SelfBench pass ⇒ no selfbench.* events ⇒ byte-identical. The gate runs AFTER the ledger entry
	// (a batch must be RECORDED to be benched) so e.ledgerRecorded can re-baseline on a closed-loop revert.
	if e.features.Ledger.SelfBenchGate {
		e.selfBenchGate(st, delta, revert)
	}
}

// ledgerRevertHandle returns the name of the session's revert point — the auto-baseline snapshot if
// one exists, else the newest named snapshot, else a marker. Read-only.
func (e *Engine) ledgerRevertHandle(st persist.Store) string {
	metas, err := st.ListSnapshots()
	if err != nil || len(metas) == 0 {
		return "(no baseline snapshot)"
	}
	for _, m := range metas { // prefer the auto-baseline (the clean pre-mint point)
		if m.Name == ledgerBaselineSnapshot {
			return m.Name
		}
	}
	return metas[0].Name // newest (ListSnapshots is newest-first)
}

// ledgerBaselineSnapshot is the name of the once-per-session pre-mint snapshot the engine auto-takes
// (when ledger.auto_snapshot is on and no snapshot exists yet) so a session's self-changes revert as
// a batch.
const ledgerBaselineSnapshot = "auto:baseline"

// flushState persists + flushes the learned state on lifecycle→DONE / clean shutdown (the §4.4 Flush on
// DONE). Runs the curator if enabled (a final cleanup), so the on-disk state is bounded. nil store ⇒ no-op.
func (e *Engine) flushState() { e.curateState() }

// FlushState is the exported flush the CLI / TUI bridge calls on a clean shutdown or before an engine
// rebuild, so the learned state is persisted before the process exits / the engine is swapped. nil
// store ⇒ no-op. (The internal IDLE/DONE hooks use the unexported flushState.)
func (e *Engine) FlushState() { e.flushState() }

// Store exposes the injected cross-session persistence port (nil ⇒ in-memory only) so the TUI bridge can
// carry it across an engine Rebuild — a Settings change reloads the persisted learned state rather than
// dropping it (the §4.4 live-session data-loss fix). Read-only handle; the engine owns the calls.
func (e *Engine) Store() persist.Store { return e.cfg.Store }

// semanticAll / knowledgeAll return the FULL stored set (including invalidated rows) so a bi-temporal
// invalidation is persisted too (Current() drops invalidated rows; for persistence we keep them so the
// refutation survives the restart). They mirror the registries' All()/Current() but include history.
func (e *Engine) semanticAll() []memory.Belief        { return e.semantic.AllForPersist() }
func (e *Engine) knowledgeAll() []knowledge.Knowledge { return e.knowledge.AllForPersist() }

// asCognitionProgram recovers the concrete cognition.Program from convert's Shape()-only Program port so
// the engine can serialise its body (ToDict). convert always carries the concrete *cognition.Program (the
// res.Program from reactive synthesis), passed either by value or pointer; an unexpected port type returns
// ok=false (the run is skipped — it cannot re-mint, never a panic).
func asCognitionProgram(p convert.Program) (cognition.Program, bool) {
	switch v := p.(type) {
	case cognition.Program:
		return v, true
	case *cognition.Program:
		if v != nil {
			return *v, true
		}
	}
	return cognition.Program{}, false
}
