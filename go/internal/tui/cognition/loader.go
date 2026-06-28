package cognition

// loader.go — the G1 SESSION-RECORD loader: the REAL data path behind the post-session ANALYSIS
// surface (Shift+Tab / ^Y). It reconstructs an AnalysisRecord from a recorded session — the event
// JSONL (trace.JsonlSink, --log) PLUS its per-tick SignalFrame sidecar (*.signals.jsonl, the G0
// linchpin) — turning the prototype's synthetic SampleAnalysisRecord into a scrubbable timeline of a
// session that ACTUALLY RAN. (Design: docs/internal/notes/2026-06-20-shift-tab-analysis-redesign.md §4 Phase
// 1; mock: docs/internal/notes/2026-06-20-tui-mockups-analysis.md §0.)
//
// Two sources, one record:
//   - LoadAnalysisRecord(eventPath) — a saved run on disk (SINGLE / COMPARE A·B from the picker).
//   - RecordFromFrozen(events, frames) — the FROZEN live session (^P pause -> Shift+Tab analyses the
//     running mind off the in-memory bus history + the recorder's accumulated frames).
//
// PATTERN A — pure CONTROL reconstruction. The loader is a deterministic function of the
// (deterministically-ordered) recorded streams: no model call, no wall clock, no RNG. It derives the
// per-tick scrub series from the SignalFrames and the stimulus index / decision history / reward
// ledger / outcome scoreboard / impulse capture from the events. It EMITS NOTHING back onto a bus and
// touches no engine state — so it is a passive reader, never a cognition change, and the event golden
// is untouched (the analysis surface only ever READS a record, never re-runs the model, per the mock).
//
// HEADLESS-PURE boundary held: this package imports internal/events + the stdlib, never an engine
// package (the §4.3 rule). It reads the SignalFrame SIDECAR via the documented wire SCHEMA (snake_case
// tags, schemaFrame below) rather than importing the engine-side recorder type — the same decoupling
// internal/costest/log.go uses (pin the wire contract as a literal so the reader does not depend on
// the producer). The frozen-live path takes the events + frames by value (the bridge owns the bus
// subscription, not this reader).

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// SignalFrame is the loader's decode target for one line of the *.signals.jsonl sidecar — the
// documented G0 wire schema (docs/internal/notes/2026-06-12-tui-mockups-vitals.md "The SignalFrame", the
// per-tick vital-signs vector). The JSON tags ARE the wire contract; the loader pins them here so it
// decodes any sidecar a G0 recorder wrote without importing the engine-side producer. json's
// ignore-unknown-fields makes it forward-compatible (a newer schema with extra rows still decodes;
// the Schema int distinguishes versions if a breaking field-rename ever lands).
type SignalFrame struct {
	Schema int `json:"schema"`
	Tick   int `json:"tick"`

	TickLatencyMs int `json:"tick_latency_ms"`
	CallsInTick   int `json:"calls_in_tick"`

	N         float64 `json:"n"`
	U         float64 `json:"u"`
	Mu        float64 `json:"mu"`
	Theta     float64 `json:"theta"`
	LambdaBar float64 `json:"lambda_bar"`

	Reserve int `json:"reserve"`

	VActive float64 `json:"v_active"`

	ObservationsInWindow int     `json:"observations_in_window"`
	GroundedRatio        float64 `json:"grounded_ratio"`

	UserWaiting     bool    `json:"user_waiting"`
	WaitingAgeTicks int     `json:"waiting_age_ticks"`
	Ambiguity       float64 `json:"ambiguity"`

	FallbacksInWindow     int `json:"fallbacks_in_window"`
	ParseFailuresInWindow int `json:"parse_failures_in_window"`

	Decision string `json:"decision"`

	SalientInput bool   `json:"salient_input"`
	InputKind    string `json:"input_kind"`

	Condition string `json:"condition"`
	Arousal   string `json:"arousal"`
}

// LoadAnalysisRecord reads a recorded session from disk into an AnalysisRecord. eventPath is the
// event JSONL (--log); the SignalFrame sidecar is found by SidecarPath(eventPath). Either source may
// be partial (a crashed run leaves a truncated tail) — a malformed line is skipped, never fatal — so
// a partial log still scrubs. Returns an error only on an unreadable event file. A missing/empty
// sidecar yields a record with no scrub series (the events still drive the stimulus index + ledgers).
func LoadAnalysisRecord(eventPath string) (AnalysisRecord, error) {
	evs, err := readEventLog(eventPath)
	if err != nil {
		return AnalysisRecord{}, err
	}
	frames := readSignalSidecar(SidecarPath(eventPath))
	rec := RecordFromFrozen(evs, frames)
	if rec.Name == "" {
		rec.Name = baseName(eventPath)
	}
	return rec, nil
}

// SidecarPath maps an event-log path to its SignalFrame sidecar, mirroring cmd/thought's writer: a
// ".jsonl" tail becomes ".signals.jsonl"; any other path just appends ".signals.jsonl". Exported so
// the picker can probe whether a record has a sidecar (the scrub series source).
func SidecarPath(eventPath string) string {
	if strings.HasSuffix(eventPath, ".jsonl") {
		return strings.TrimSuffix(eventPath, ".jsonl") + ".signals.jsonl"
	}
	return eventPath + ".signals.jsonl"
}

// RecordFromFrozen reconstructs an AnalysisRecord from an in-memory recorded session: the event
// stream (tick-stamped, in emission order) plus the per-tick SignalFrames. This is the core both the
// on-disk loader and the ^P frozen-live path funnel through, so the two record sources are
// byte-identical given the same streams. The frames define the scrub axis (one column per tick); the
// events fill the stimulus index, the decision history, the reward ledger, the outcome scoreboard,
// and the impulse-response capture. Pure over its inputs.
func RecordFromFrozen(evs []events.Event, frames []SignalFrame) AnalysisRecord {
	rec := AnalysisRecord{Mode: "reactive"}

	// --- scrub axis from the SignalFrames (the per-tick vital series) -------------------------
	rec.fillSeriesFromFrames(frames)

	// --- the stimulus index + ledgers + scoreboard from the events ---------------------------
	rec.fillFromEvents(evs)

	// --- the registry/memory FAMILY heat map + mint/demote ledger (§6, G3) --------------------
	rec.fillFamily(evs)

	// --- the DEEP ledgers + tree (§5/§7/§8/§9, G4) --------------------------------------------
	rec.fillDeep(evs)

	// --- the impulse-response capture (latency off the chosen stimulus) ----------------------
	rec.fillImpulse(evs)

	// --- the TRACE/FLOW swimlane projection (§G6) ---------------------------------------------
	rec.fillTrace(evs)

	return rec
}

// fillTrace projects the recorded event stream onto the §G6 TRACE/FLOW swimlane — the
// seed->thought->seam->subconscious->action ROUND-TRIP placed by (tick, lane). Each event maps to a
// lane (laneOf) and a one-rune marker (the event's role within its lane); an off-lane event (a control
// organ — value/regulator/critic/lifecycle/etc.) is dropped so the swimlane stays the five-layer trip.
//
// The DESYNC markers (the phase-misalignment signals the view exists to surface):
//   - a LATE seam.inject — an injection that arrives AFTER the trip has already reached the ACTION lane
//     (an ACT/RESPOND has fired), i.e. the silent injection re-opens a node the trip had passed; and
//   - a conscious RETRACEMENT — a Thought-MCP `reenter`/`expand`/`focus` op that reopens a folded line
//     (the conscious stream looping back), the re-traversal of a node already explored.
//
// Pattern-A deterministic single pass over the (deterministically-ordered) stream — no model, no clock,
// no RNG. The projection is computed unconditionally (the tui.trace_flow knob gates only whether the
// TRACE tab RENDERS it, never whether the loader builds it — same as the other deep-panel families).
func (r *AnalysisRecord) fillTrace(evs []events.Event) {
	reachedAction := false // the trip has reached the ACTION lane (an ACT/RESPOND fired)
	for _, ev := range evs {
		lane, glyph := laneOf(ev)
		if lane == "" {
			continue // an off-lane control organ — not part of the five-layer trip
		}
		te := TraceEvent{
			Tick:  ev.Tick,
			Lane:  lane,
			Glyph: glyph,
			Kind:  ev.Kind,
			Gist:  traceGist(ev),
		}
		switch ev.Kind {
		case events.Act, events.Respond, events.ActionTool:
			reachedAction = true
		case events.Inject:
			// a late injection: the seam re-voiced a candidate AFTER the trip had already reached ACTION
			// — the silent injection re-opening a passed node (the phase-misalignment signal).
			if reachedAction {
				te.Desync = true
			}
		case events.MCP:
			// a conscious retracement: reopening a folded/explored line (reenter / expand / focus).
			switch strField(ev.Data, "op") {
			case "reenter", "expand", "focus":
				te.Desync = true
			}
		}
		r.TraceEvents = append(r.TraceEvents, te)
	}
}

// laneOf maps an event to its TRACE swimlane row + the one-rune marker for its role within that lane,
// or ("", "") for an off-lane control organ (value / regulator / critic / lifecycle / convert / …)
// that is not part of the five-layer round-trip. The lanes follow the architecture's layers + seams:
// PORT (the perception/intake edge), CONSCIOUS (the thinking stream + Thought-MCP), SEAM (the hidden
// FILTER->GATE->TRANSFORM->INJECT), SUBCONSCIOUS (specialists/dispatch/synthesis/sub-agents), ACTION
// (the watched seam: intention out, reality in). Pattern-A — a pure namespace dispatch.
func laneOf(ev events.Event) (lane, glyph string) {
	switch ev.Kind {
	case events.Port:
		return "PORT", "◆" // a salient arrival across the perception port (the seed/stimulus)
	case events.Filter:
		return "SEAM", "f"
	case events.Gate:
		return "SEAM", "g"
	case events.Transform:
		return "SEAM", "t"
	case events.Inject:
		return "SEAM", "▸" // the re-voiced injection landing in CONSCIOUS
	case events.Act, events.Intention:
		return "ACTION", "↑" // intention out
	case events.Observation, events.Ground:
		return "ACTION", "↓" // reality in
	case events.ActionTool:
		return "ACTION", "*"
	case events.Respond, events.Ask:
		return "ACTION", "▣" // the delivery to the user
	case events.MCP, events.Generate, events.Append:
		return "CONSCIOUS", "•" // a Thought-MCP op / generated thought on the conscious stream
	}
	// fall back on the derived layer for the broad subconscious/conscious families not enumerated above.
	switch ev.Layer {
	case "subconscious":
		return "SUBCONSCIOUS", "s" // a specialist fire / dispatch / synthesis / sub-agent
	case "conscious":
		return "CONSCIOUS", "•"
	case "session":
		return "SUBCONSCIOUS", "▽" // a sub-agent session spawn/dispatch (the helper team)
	case "perception":
		return "PORT", "◇" // a sensor read across the perception port
	}
	return "", ""
}

// traceGist is the one-line label for a swimlane event — the event summary, trimmed to keep the lane
// row readable. Pattern-A (a pure string read).
func traceGist(ev events.Event) string {
	s := strings.TrimSpace(ev.Summary)
	if s == "" {
		s = ev.Kind
	}
	return clipName(s, 44)
}

// fillDeep reconstructs the G4 per-subsystem DETAIL — the §5 CONSCIOUS thought tree + compression
// history, the §7 ACTION·GROUNDING ledger + the SESSIONS·SUB-AGENTS spawn tree, the §8 THROUGHPUT
// per-role/tier spend, and the §9 SELF·EVOLUTION change ledger — Pattern-A off the recorded event
// stream. Each panel is the audit the mock specifies: the tree answers "did dead branches die for a
// REASON?", the action ledger answers "did reality push back (REFUTED) and was a fabrication BLOCKED?",
// the spawn tree answers "did the helper team stay bounded + scoped?", throughput answers "where did
// the budget go?", and the self ledger answers "is every self-change in the log with a cause?". A
// deterministic read of a session that ran — never a re-simulation. Walks the stream once per panel.
func (r *AnalysisRecord) fillDeep(evs []events.Event) {
	r.fillTree(evs)
	r.fillActions(evs)
	r.fillSpawnTree(evs)
	r.fillThroughput(evs)
	r.fillSelf(evs)
}

// fillTree reconstructs the §5 thought tree + compression history off the conscious.mcp ops (the
// Thought MCP: branch / merge / compress / expand) and the decision history (a BACKTRACK kills the
// branch it left — a dead line that stopped paying out). The branch a fork is FROM is the active
// branch at the fork tick (the most-recently-opened live branch), so the tree is parent-linked, not a
// flat list. A merged branch carries a "merged-into bN" death; a refuted/dry one its BACKTRACK reason.
func (r *AnalysisRecord) fillTree(evs []events.Event) {
	byID := map[int]*BranchNode{}
	var order []int
	active := -1 // the branch most-recently opened/focused — the parent of the next fork
	get := func(id int) *BranchNode {
		b := byID[id]
		if b == nil {
			b = &BranchNode{ID: id, Parent: -1, State: "EXPANDED", DeadTick: -1}
			byID[id] = b
			order = append(order, id)
		}
		return b
	}
	for _, ev := range evs {
		switch ev.Kind {
		case events.MCP:
			op := strField(ev.Data, "op")
			switch op {
			case "branch":
				id, ok := intField(ev.Data, "branch")
				if !ok {
					continue
				}
				b := get(id)
				b.Born = ev.Tick
				b.Parent = active // forked from the line we were on
				b.Text = firstNonEmpty(strField(ev.Data, "reason"), mcpReasonFromSummary(ev.Summary))
				active = id
			case "merge":
				gone, okG := intField(ev.Data, "gone")
				into, okI := intField(ev.Data, "into")
				if okG {
					b := get(gone)
					b.State = "MERGED"
					if b.DeadTick < 0 {
						b.DeadTick = ev.Tick
					}
					if okI {
						b.DeadReason = "merged into b" + itoa2(into)
					} else {
						b.DeadReason = "merged"
					}
				}
				if okI {
					active = into
				}
			case "compress":
				if id, ok := intField(ev.Data, "branch"); ok {
					b := get(id)
					if b.State == "EXPANDED" {
						b.State = "COMPRESSED"
					}
					r.Compression = append(r.Compression, CompressionEvent{Tick: ev.Tick, Op: "COMPRESS", Branch: id})
				}
			case "expand", "focus", "reenter":
				if id, ok := intField(ev.Data, "branch"); ok {
					b := get(id)
					b.State = "EXPANDED"
					active = id
					if op == "expand" {
						r.Compression = append(r.Compression, CompressionEvent{Tick: ev.Tick, Op: "EXPAND", Branch: id})
					}
				}
			}
		case events.Decision:
			// a BACKTRACK abandons the line it was on (the active branch) — a branch that died for a
			// reason (dry/exhausted), which is the §5 "✗ DEAD @t with a reason" the panel reads.
			if strField(ev.Data, "decision") == "BACKTRACK" && active >= 0 {
				b := get(active)
				if b.State != "MERGED" && b.DeadTick < 0 {
					b.State = "DEAD"
					b.DeadTick = ev.Tick
					b.DeadReason = firstNonEmpty(strField(ev.Data, "reason"), ev.Summary)
				}
			}
		}
	}
	r.Branches = make([]BranchNode, 0, len(order))
	for _, id := range order {
		r.Branches = append(r.Branches, *byID[id])
	}
}

// fillActions reconstructs the §7 ACTION·GROUNDING ledger — the watched seam in audit form: each ACT
// (an intention out), each grounding verdict (GROUNDED = reality confirmed, REFUTED = reality corrected
// a belief — the HEALTHY revert), each safety BLOCK (a fabricated/unsafe call the check caught), and
// each sandbox DENIAL. The ledger interleaves them in tick order so an ACT reads next to the reality it
// pulled. Off action.act / action.tool / grounding.ground / action.safety_block / action.sandbox_deny.
func (r *AnalysisRecord) fillActions(evs []events.Event) {
	for _, ev := range evs {
		switch ev.Kind {
		case events.Act:
			r.Actions = append(r.Actions, ActionEvent{Tick: ev.Tick, Kind: "ACT",
				Text: firstNonEmpty(strField(ev.Data, "kind"), ev.Summary)})
		case events.ActionTool:
			r.Actions = append(r.Actions, ActionEvent{Tick: ev.Tick, Kind: "ACT",
				Text: ev.Summary, Tool: strField(ev.Data, "tool")})
		case events.Ground:
			kind := ""
			switch strField(ev.Data, "verdict") {
			case "grounded", "GROUNDED":
				kind = "GROUNDED"
			case "refuted", "REFUTED":
				kind = "REFUTED"
			}
			if kind == "" {
				continue
			}
			r.Actions = append(r.Actions, ActionEvent{Tick: ev.Tick, Kind: kind,
				Text: firstNonEmpty(strField(ev.Data, "claim"), ev.Summary), Tool: strField(ev.Data, "tier")})
		case events.ActionSafetyBlock, events.ActionBlocked:
			r.Actions = append(r.Actions, ActionEvent{Tick: ev.Tick, Kind: "BLOCKED",
				Text: firstNonEmpty(ev.Summary, "blocked by the safety check")})
		case events.ActionSandboxDeny:
			r.Actions = append(r.Actions, ActionEvent{Tick: ev.Tick, Kind: "DENIAL",
				Text: firstNonEmpty(ev.Summary, "denied — outside its tool scope")})
		}
	}
}

// fillSpawnTree reconstructs the §7 SESSIONS·SUB-AGENTS spawn tree — the helper team the engine spawned,
// each bounded to a tool scope (the watched-seam guarantee). A session.dispatch opens a phase worker; a
// subconscious.subagent carries the role/domain/tool-scope; session.terminate carries the whole-tree
// node count + max depth + token spend (the bound the durability law keeps). One Worker per dispatch.
func (r *AnalysisRecord) fillSpawnTree(evs []events.Event) {
	for _, ev := range evs {
		switch ev.Kind {
		case events.SessionDispatch:
			w := Worker{Role: firstNonEmpty(strField(ev.Data, "goal"), ev.Summary)}
			if d, ok := intField(ev.Data, "depth"); ok {
				w.Depth = d
				if d > r.SpawnDepth {
					r.SpawnDepth = d
				}
			}
			if p, ok := intField(ev.Data, "phase"); ok {
				w.Phase = p
			}
			r.Workers = append(r.Workers, w)
		case events.SubSubagent:
			// a sub-agent's role/domain/tool-scope refines the most recent worker row when present, else
			// adds its own (so the panel shows the role + scope, the "limited to specific tools" read).
			role := strField(ev.Data, "role")
			dom := strField(ev.Data, "domain")
			scope := strSliceField(ev.Data, "tool_scope")
			if n := len(r.Workers); n > 0 && r.Workers[n-1].Domain == "" {
				if role != "" {
					r.Workers[n-1].Role = role
				}
				r.Workers[n-1].Domain = dom
				r.Workers[n-1].ToolScope = scope
			} else {
				r.Workers = append(r.Workers, Worker{Role: firstNonEmpty(role, "subagent"), Domain: dom, ToolScope: scope})
			}
		case events.SessionTerminate:
			if t, ok := intField(ev.Data, "tokens"); ok {
				r.SpawnTokens = t
			}
			if d, ok := intField(ev.Data, "depth"); ok && d > r.SpawnDepth {
				r.SpawnDepth = d
			}
		}
	}
}

// fillThroughput reconstructs the §8 metabolism — where the token budget went, split by CONTENT role
// (generate / transform / respond / …) and by model TIER (the big primary vs the cheap utility), plus
// the single most expensive tick (the slow point). Off llm.call events: each carries the role, the
// model, and the completion-token count. The tier split keys on the utility-model heuristic (a model
// name containing "haiku"/"utility"/"mini"/"small"/"flash" is the cheap tier) since the bridge does not
// stamp a tier field; an unknown model falls to the big tier (the conservative read).
func (r *AnalysisRecord) fillThroughput(evs []events.Event) {
	byRole := map[string]int{}
	var roleOrder []string
	perTick := map[int]int{}
	r.PeakTick = -1
	for _, ev := range evs {
		if ev.Kind != events.LLM {
			continue
		}
		role := strField(ev.Data, "role")
		tok := 0
		if n, ok := numField(ev.Data, "completion_tokens"); ok {
			tok = int(n + 0.5)
		}
		if role != "" {
			if _, seen := byRole[role]; !seen {
				roleOrder = append(roleOrder, role)
			}
			byRole[role] += tok
		}
		if isCheapTier(strField(ev.Data, "model")) {
			r.TierCheap++
		} else {
			r.TierBig++
		}
		perTick[ev.Tick] += tok
	}
	// roles ordered by spend, hottest first (the "where the budget went" read tops the panel).
	r.Roles = sortedRoleShares(byRole, roleOrder)
	// the peak tick — the single most expensive moment (deterministic tie-break on the lower tick).
	for tick, tok := range perTick {
		if tok > r.PeakTokens || (tok == r.PeakTokens && (r.PeakTick < 0 || tick < r.PeakTick)) {
			r.PeakTokens = tok
			r.PeakTick = tick
		}
	}
}

// fillSelf reconstructs the §9 SELF·EVOLUTION change ledger — what the harness changed about ITSELF,
// the self-modification audit. A kept learned item (a mint that earned its keep) reads ADDED; a
// keep-or-revert reversal reads UNDID; a safety-gate refusal reads REFUSED. The governance SCOPE rides
// the registry.batch safety_mode; a persist.load anchors the lineage baseline. Off convert.mint /
// registry.batch / persist.load — the audit the mock's "a change that ISN'T in the log" signature
// guards (every self-change here came from a logged engine event, never invented).
func (r *AnalysisRecord) fillSelf(evs []events.Event) {
	for _, ev := range evs {
		switch ev.Kind {
		case events.PersistLoad:
			r.SelfBaseSet = true
		case events.SkillMint:
			r.SelfChanges = append(r.SelfChanges, SelfChange{Tick: ev.Tick, Action: "ADDED",
				Item: "skill " + strField(ev.Data, "name"), Evidence: firstNonEmpty(skillMintEvidence(ev.Data), "promoted from repeats")})
		case events.Convert:
			switch strField(ev.Data, "kind") {
			case "specialist":
				r.SelfChanges = append(r.SelfChanges, SelfChange{Tick: ev.Tick, Action: "ADDED",
					Item: "specialist " + strField(ev.Data, "domain"), Evidence: convertMintEvidence(ev.Data, ev.Summary)})
			case "demote":
				r.SelfChanges = append(r.SelfChanges, SelfChange{Tick: ev.Tick, Action: "UNDID",
					Item: "specialist " + strField(ev.Data, "domain"), Evidence: "reality refuted it (keep-or-revert)"})
			}
		case events.RegistryBatch:
			if mode := strField(ev.Data, "safety_mode"); mode != "" {
				r.SelfScope = strings.ToUpper(mode)
			}
			action := strField(ev.Data, "action")
			if strings.Contains(action, "refus") {
				r.SelfChanges = append(r.SelfChanges, SelfChange{Tick: ev.Tick, Action: "REFUSED",
					Item: strField(ev.Data, "scope"), Evidence: firstNonEmpty(ev.Summary, "refused by the safety gate")})
			}
		}
	}
}

// mcpReasonFromSummary salvages a branch's gist from the conscious.mcp summary ("branch -> bN: reason")
// when the data map carried no explicit reason (an older recorder). Returns "" if no ": " is present.
func mcpReasonFromSummary(summary string) string {
	if i := strings.Index(summary, ": "); i >= 0 {
		return summary[i+2:]
	}
	return ""
}

// isCheapTier is the §8 utility-tier heuristic: a model name naming a small/utility variant
// (haiku / utility / mini / small / flash / nano) is the cheap tier; anything else is the big tier
// (the conservative default). The bridge does not stamp a tier field, so the model name is the signal.
func isCheapTier(model string) bool {
	m := strings.ToLower(model)
	for _, marker := range []string{"haiku", "utility", "mini", "small", "flash", "nano"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

// sortedRoleShares flattens the role->tokens map into a deterministic spend-descending order (the
// hottest role tops the §8 panel), first-seen order as the stable tie-break. Pure (no map-iteration
// nondeterminism — keys are walked via the first-seen order slice and re-sorted by explicit comparators).
func sortedRoleShares(byRole map[string]int, order []string) []RoleShare {
	seen := map[string]int{}
	for i, k := range order {
		seen[k] = i
	}
	out := make([]RoleShare, 0, len(order))
	for _, k := range order {
		out = append(out, RoleShare{Role: k, Tokens: byRole[k]})
	}
	less := func(a, b RoleShare) bool {
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		return seen[a.Role] < seen[b.Role]
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && less(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// fillSeriesFromFrames lays the per-tick vital series onto the scrub axis. The SignalFrames are in
// strictly increasing tick order (the recorder's contract); the record's series are dense (index ==
// frame index), and Ticks is the number of frames (the scrub width). A missing sidecar leaves the
// series empty and Ticks 0 — the renderers tolerate that (sparkW returns blanks).
func (r *AnalysisRecord) fillSeriesFromFrames(frames []SignalFrame) {
	n := len(frames)
	r.Ticks = n
	if n == 0 {
		return
	}
	r.Condition = make([]float64, n)
	r.N = make([]float64, n)
	r.U = make([]float64, n)
	r.Theta = make([]float64, n)
	r.VActive = make([]float64, n)
	r.Reserve = make([]float64, n)
	r.Pressure = make([]float64, n)
	r.TokOutRate = make([]float64, n)
	r.GroundHits = make([]bool, n)

	maxLatency := 1
	for _, f := range frames {
		if f.TickLatencyMs > maxLatency {
			maxLatency = f.TickLatencyMs
		}
	}
	awake := false
	for i, f := range frames {
		// condition is a 0..1 intensity for the sparkline: DEGRADED highest, then LOADED/ENGAGED.
		r.Condition[i] = conditionIntensity(f.Condition)
		r.N[i] = clamp01(f.N)
		r.U[i] = clamp01(f.U)
		r.Theta[i] = clamp01(f.Theta)
		r.VActive[i] = clamp01(f.VActive)
		// reserve frames are 0..100; the scrub series is 0..1.
		r.Reserve[i] = clamp01(float64(f.Reserve) / 100.0)
		// pressure: a user-waiting line under demand — proxy on the waiting age + ambiguity.
		r.Pressure[i] = pressureIntensity(f)
		// token-out rate: per-tick latency normalised over the session's busiest tick (a throughput proxy).
		r.TokOutRate[i] = float64(f.TickLatencyMs) / float64(maxLatency)
		r.GroundHits[i] = groundedThisTick(frames, i)
		if f.Arousal == "AWAKE" {
			awake = true
		}
	}
	if awake {
		r.Mode = "awake"
	}
}

// groundedThisTick reports whether the trailing-window observation count grew at frame i (a fresh
// reality import landed this tick), so GroundHits marks the ACTUAL grounding ticks rather than the
// whole window. The window count is monotone WITHIN the window, so a strict increase over the prior
// frame is the per-tick arrival signal (it can fall when an old contribution ages out of the window,
// which is not a fresh import — hence the strict >).
func groundedThisTick(frames []SignalFrame, i int) bool {
	if i == 0 {
		return frames[i].ObservationsInWindow > 0
	}
	return frames[i].ObservationsInWindow > frames[i-1].ObservationsInWindow
}

// fillFromEvents walks the recorded event stream once, building the stimulus index (the impulse
// origins), the decision history, the reward ledger, the outcome scoreboard, and the run header
// (name / substrate). Tick-stamped events map onto the scrub axis by their tick.
func (r *AnalysisRecord) fillFromEvents(evs []events.Event) {
	for _, ev := range evs {
		switch ev.Kind {
		case events.Port:
			if strField(ev.Data, "source") == "USER_INPUT" {
				r.Stimuli = append(r.Stimuli, Stimulus{
					Tick: ev.Tick,
					Kind: "user",
					Text: firstNonEmpty(strField(ev.Data, "text"), ev.Summary),
				})
			}
		case events.Observation, events.Ground:
			// a reality arrival across the watched seam (the grounding/observation impulse origin).
			text := firstNonEmpty(strField(ev.Data, "claim"), ev.Summary)
			r.Stimuli = append(r.Stimuli, Stimulus{Tick: ev.Tick, Kind: "reality", Text: text})
			if ev.Kind == events.Ground {
				switch strField(ev.Data, "verdict") {
				case "grounded", "GROUNDED":
					r.Grounded++
				case "refuted", "REFUTED":
					r.Refuted++
				}
			}
		case events.Decision:
			move := strField(ev.Data, "decision")
			if move == "" {
				continue
			}
			r.Decisions = append(r.Decisions, DecisionEvent{
				Tick:     ev.Tick,
				Move:     move,
				Reason:   firstNonEmpty(strField(ev.Data, "reason"), ev.Summary),
				StopKind: strField(ev.Data, "stop_kind"),
			})
		case events.Value:
			// a grounded reward rides a value.update carrying a non-zero "reward" (reality, never self-graded).
			if rw, ok := numField(ev.Data, "reward"); ok && rw != 0 {
				r.Rewards = append(r.Rewards, RewardEvent{Tick: ev.Tick, Value: rw, Reason: ev.Summary})
			}
		case events.Respond:
			r.Delivered++
		case events.ActionSafetyBlock, events.ActionBlocked:
			r.Fabricated++
		case events.Tick:
			// the run mode rides the tick summary ("tick N [reactive] ..."); kept as a cheap hint only
			// when the SignalFrames did not already set awake (the arousal row wins when present).
			if r.Mode == "reactive" && strings.Contains(ev.Summary, "[continuous]") {
				r.Mode = "awake"
			}
		}
	}
	r.Reverts = countReverts(r.Rewards)
	r.SolveVerdict = deriveVerdict(r.Decisions, r.Delivered)
}

// fillFamily reconstructs the §6 registry/memory FAMILY view — the "is the learned machinery real AND
// in use?" heat map + the mint/demote evidence ledger — Pattern-A off the recorded event stream. Each
// learned/registered item that FIRED gets a row (FamilyEntry) with its per-tick firing history (the
// coldness-vs-topics strip); each mint/demote/prune/invalidate gets a Ledger line with the engine's
// own logged evidence. The two are the audit the mock specifies: the heat map answers "is this skill
// USED, not just collected?", the ledger answers "did it enter/leave for a logged reason?".
//
// The wire keying (the documented Data fields the emitters write, pinned here as the §6 contract):
//   - subconscious.fire    -> family "spec", name = data["domain"]   (a specialist/operator fired)
//   - subconscious.operator-> family "op",   name = data["name"]     (a NEW operator was minted)
//   - subconscious.skill_match -> family "skill", name = data["skill"]  (a library skill was recalled)
//   - knowledge.recall     -> family "know",  name = data["top"]     (a knowledge fact was surfaced)
//   - subconscious.source  -> family "src",   name = data["provider"] (a memory/source rung resolved)
//
// A row's Fires[tick] is set on every tick the item ran; Total is the count; BornTick is the mint tick
// for minted items (skill/operator/specialist), -1 for a seeded item present before the run.
func (r *AnalysisRecord) fillFamily(evs []events.Event) {
	// the firing strips are sized to the scrub axis (the SignalFrame count) when there is one; with NO
	// sidecar (the frozen-live path runs no G0 recorder) they are sized to the event span instead, so
	// the heat map still renders from the events alone. This is a LOCAL width — r.Ticks is left at the
	// frame count (the loader contract: no sidecar ⇒ no scrub series), it is not mutated here.
	width := r.Ticks
	if width <= 0 {
		for _, ev := range evs {
			if ev.Tick+1 > width {
				width = ev.Tick + 1
			}
		}
	}
	if width <= 0 {
		return
	}

	entries := map[familyKey]*FamilyEntry{}
	var order []familyKey // first-seen order, for a deterministic tie-break under the family sort
	mark := func(fam, name string, tick int, born int) {
		if name == "" {
			return
		}
		k := familyKey{fam, name}
		e := entries[k]
		if e == nil {
			e = &FamilyEntry{Family: fam, Name: name, Fires: make([]bool, width), BornTick: -1}
			entries[k] = e
			order = append(order, k)
		}
		if born >= 0 && (e.BornTick < 0 || born < e.BornTick) {
			e.BornTick = born
		}
		if tick >= 0 && tick < width && !e.Fires[tick] {
			e.Fires[tick] = true
			e.Total++
		}
	}

	for _, ev := range evs {
		switch ev.Kind {
		case events.SubFire:
			mark("spec", strField(ev.Data, "domain"), ev.Tick, -1)
		case events.SubOperator:
			// a runtime-minted operator: its mint tick is its birth, and it counts as a fire that tick.
			name := strField(ev.Data, "name")
			mark("op", name, ev.Tick, ev.Tick)
			r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: "MINTED", Family: "op", Name: name,
				Evidence: firstNonEmpty(strField(ev.Data, "family"), ev.Summary)})
		case events.SkillMatch:
			mark("skill", strField(ev.Data, "skill"), ev.Tick, -1)
		case events.KnowledgeRecall:
			mark("know", strField(ev.Data, "top"), ev.Tick, -1)
		case events.SubSource:
			mark("src", strField(ev.Data, "provider"), ev.Tick, -1)
		case events.SkillMint:
			name := strField(ev.Data, "name")
			mark("skill", name, ev.Tick, ev.Tick)
			r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: "MINTED", Family: "skill", Name: name,
				Evidence: firstNonEmpty(skillMintEvidence(ev.Data), ev.Summary)})
		case events.Convert:
			r.recordConvertLedger(ev)
		case events.PersistCurate:
			r.recordCurateLedger(ev)
		case events.KnowledgePromote:
			if isTrue(ev.Data, "demote") {
				r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: "DEMOTED", Family: "know",
					Name: strField(ev.Data, "statement"), Evidence: "prior reverted — reality refuted"})
			}
		case events.KnowledgeInvalidate:
			r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: "INVALIDATED", Family: "know",
				Name: strField(ev.Data, "statement"), Evidence: firstNonEmpty(ev.Summary, "refuted — invalidated")})
		}
	}

	// flag the entries that were demoted/pruned in this session (their ledger line crossed them off).
	demoted := map[familyKey]bool{}
	for _, le := range r.Ledger {
		if le.Action == "DEMOTED" || le.Action == "PRUNED" || le.Action == "INVALIDATED" {
			demoted[familyKey{le.Family, le.Name}] = true
		}
	}
	for _, k := range order {
		if demoted[k] {
			entries[k].Demoted = true
		}
	}

	r.FamilyEntries = sortedFamilyEntries(entries, order)
}

// skillMintEvidence builds the §6 evidence string for a skill mint from the data the emitter wrote (the
// repeat count), so the ledger reads "N repeated programs" rather than a bare summary.
func skillMintEvidence(d map[string]any) string {
	if n, ok := numField(d, "count"); ok && n > 0 {
		return itoaF(n) + " grounded repeats"
	}
	return ""
}

// recordConvertLedger maps a convert.mint event onto a ledger line: the specialist-mint and
// specialist-demote rails (kind "specialist"/"demote") both ride convert.mint with a distinguishing
// "kind". The mint-gate verdict (kind "mint_gate") is the upstream gate, not a registry change, so it
// is not a ledger entry.
func (r *AnalysisRecord) recordConvertLedger(ev events.Event) {
	switch strField(ev.Data, "kind") {
	case "specialist":
		r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: "MINTED", Family: "spec",
			Name: strField(ev.Data, "domain"), Evidence: convertMintEvidence(ev.Data, ev.Summary)})
	case "demote":
		r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: "DEMOTED", Family: "spec",
			Name: strField(ev.Data, "domain"), Evidence: "reality refuted it (keep-or-revert)"})
	}
}

// convertMintEvidence reads the effortful-repeat count off a specialist mint so the ledger evidence is
// the real cause ("N effortful repeats") rather than the raw summary.
func convertMintEvidence(d map[string]any, summary string) string {
	if n, ok := numField(d, "generated"); ok && n > 0 {
		return itoaF(n) + " effortful repeats (GENERATED -> INJECTED)"
	}
	return summary
}

// recordCurateLedger maps a persist.curate event onto a ledger line for the registry-shaping actions
// that REMOVE a learned item — demote, gc (garbage-collect / archive), cap (over-size eviction). The
// dedup/decay bookkeeping actions are not registry departures a benchmark reads, so they are skipped.
func (r *AnalysisRecord) recordCurateLedger(ev events.Event) {
	action := strField(ev.Data, "action")
	var label string
	switch action {
	case "demote":
		label = "DEMOTED"
	case "gc", "cap":
		label = "PRUNED"
	default:
		return
	}
	r.Ledger = append(r.Ledger, LedgerEvent{Tick: ev.Tick, Action: label,
		Family: curateFamily(strField(ev.Data, "artifact")), Name: strField(ev.Data, "id"),
		Evidence: firstNonEmpty(strField(ev.Data, "reason"), ev.Summary)})
}

// curateFamily maps a curator artifact label ("belief"/"knowledge"/"skill"/"operator"/"specialist") to
// the §6 family code; an unknown artifact passes through (the renderer shows it verbatim).
func curateFamily(artifact string) string {
	switch artifact {
	case "knowledge", "belief":
		return "know"
	case "skill":
		return "skill"
	case "operator":
		return "op"
	case "specialist":
		return "spec"
	default:
		return artifact
	}
}

// sortedFamilyEntries flattens the entry map into a deterministic order: by family rank (op, spec,
// skill, know, src — the mock's reading order), then HOTTEST-first within a family (so the busy items
// top the scroll, per the redesign's "sort so the top is what matters"), then first-seen order as the
// final stable tie-break. Pure (no map-iteration nondeterminism — the keys are walked via the
// first-seen `order` slice and re-sorted by explicit comparators).
func sortedFamilyEntries(entries map[familyKey]*FamilyEntry, order []familyKey) []FamilyEntry {
	seen := map[familyKey]int{}
	for i, k := range order {
		seen[k] = i
	}
	out := make([]FamilyEntry, 0, len(order))
	for _, k := range order {
		out = append(out, *entries[k])
	}
	famRank := func(f string) int {
		switch f {
		case "op":
			return 0
		case "spec":
			return 1
		case "skill":
			return 2
		case "know":
			return 3
		case "src":
			return 4
		default:
			return 5
		}
	}
	// insertion sort (the entry count is small — bounded by the registry roster) keeps the leaf
	// stdlib-light and the order fully deterministic.
	less := func(a, b FamilyEntry) bool {
		ra, rb := famRank(a.Family), famRank(b.Family)
		if ra != rb {
			return ra < rb
		}
		if a.Total != b.Total {
			return a.Total > b.Total // hottest first within a family
		}
		return seen[familyKey{a.Family, a.Name}] < seen[familyKey{b.Family, b.Name}]
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && less(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// fillImpulse derives the impulse-response capture: it picks the LAST user stimulus as the impulse
// origin (the most recent thing the user asked — what a responsiveness read hangs off) and measures
// the milestone offsets (first fire / inject / act / deliver) AFTER that stimulus tick, in ticks. A
// session with no user stimulus leaves the impulse zeroed (the renderer shows no headline).
func (r *AnalysisRecord) fillImpulse(evs []events.Event) {
	origin := -1
	originText := ""
	for _, s := range r.Stimuli {
		if s.Kind == "user" {
			origin = s.Tick
			originText = s.Text
		}
	}
	if origin < 0 {
		return
	}
	r.ImpStimulusTick = origin
	r.ImpStimulusText = originText

	for _, ev := range evs {
		if ev.Tick < origin {
			continue
		}
		off := ev.Tick - origin
		switch ev.Kind {
		case events.SubFire:
			if r.ImpToFire == 0 {
				r.ImpToFire = off
			}
		case events.Inject:
			if r.ImpToInject == 0 {
				r.ImpToInject = off
			}
		case events.Act, events.ActionTool:
			if r.ImpToAct == 0 {
				r.ImpToAct = off
			}
		case events.Respond:
			if r.ImpToDeliver == 0 {
				r.ImpToDeliver = off
			}
		}
	}
}

// deriveVerdict reads the outcome from the recorded trajectory: a STOP/GOAL_MET (or any STOP that is
// not a GIVE_UP) with at least one delivery is SOLVED; an explicit GIVE_UP or no delivery is
// UNSOLVED. This is the recorded outcome the scoreboard scores on — no re-judgement, the verdict the
// session itself reached.
func deriveVerdict(ds []DecisionEvent, delivered int) string {
	gaveUp := false
	goalMet := false
	for _, d := range ds {
		if d.Move != "STOP" {
			continue
		}
		switch d.StopKind {
		case "GIVE_UP", "GAVE_UP":
			gaveUp = true
		case "GOAL_MET":
			goalMet = true
		}
	}
	if goalMet && delivered > 0 {
		return "SOLVED"
	}
	if gaveUp || delivered == 0 {
		return "UNSOLVED"
	}
	if delivered > 0 {
		return "SOLVED"
	}
	return "UNSOLVED"
}

// countReverts counts the negative rewards in the ledger — reality refuting a belief (a healthy
// revert: the system dropped a wrong line because reality pushed back, never self-graded).
func countReverts(rs []RewardEvent) int {
	n := 0
	for _, r := range rs {
		if r.Value < 0 {
			n++
		}
	}
	return n
}

// --- per-tick intensity derivations (Pattern-A, deterministic) --------------------------------

// conditionIntensity maps the one-word condition to a 0..1 sparkline height (DEGRADED is the
// alarm-high reading, NOMINAL the calm-low baseline). The mock renders condition as a story-arc
// sparkline, so a monotone severity scale is what the chart wants.
func conditionIntensity(cond string) float64 {
	switch cond {
	case "DEGRADED":
		return 1.0
	case "LOADED":
		return 0.75
	case "CONSOLIDATING":
		return 0.6
	case "ENGAGED":
		return 0.5
	case "NOMINAL":
		return 0.2
	default:
		return 0.2
	}
}

// pressureIntensity proxies demand-on-the-system for the scrub series: a user waiting raises the
// floor, the waiting age adds urgency (saturating), and ambiguity adds load. Clamped 0..1.
func pressureIntensity(f SignalFrame) float64 {
	p := 0.5 * f.Ambiguity
	if f.UserWaiting {
		p += 0.4
		// waiting age saturates by ~25 ticks (the user has been kept waiting "a while").
		age := float64(f.WaitingAgeTicks)
		if age > 25 {
			age = 25
		}
		p += 0.4 * (age / 25.0)
	}
	return clamp01(p)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// --- small stdlib-only readers ----------------------------------------------------------------

// readEventLog parses a --log JSONL into an in-memory event slice (tick/kind/layer/summary/data),
// tolerating a truncated tail (a crashed run). It only errors on an unreadable file. The scanner
// buffer is grown so a single fat line (a full LLM prompt+response) is never split.
func readEventLog(path string) ([]events.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeEventLog(f)
}

func decodeEventLog(r io.Reader) ([]events.Event, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []events.Event
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev events.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip a malformed / truncated line, never drop the whole log
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}

// readSignalSidecar parses a *.signals.jsonl into SignalFrames. A missing or unreadable sidecar
// returns nil (the record then has no scrub series — the events still drive the ledgers). A malformed
// line is skipped. json's ignore-unknown-fields keeps an older/newer schema decoding best-effort.
func readSignalSidecar(path string) []SignalFrame {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	return decodeSignalSidecar(f)
}

func decodeSignalSidecar(r io.Reader) []SignalFrame {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)
	var frames []SignalFrame
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var fr SignalFrame
		if err := json.Unmarshal(line, &fr); err != nil {
			continue
		}
		frames = append(frames, fr)
	}
	return frames
}

// --- field helpers ---------------------------------------------------------------------------

// familyKey identifies one §6 heat-map row by (family code, entry name). Package-scoped so fillFamily
// and sortedFamilyEntries share it.
type familyKey struct{ fam, name string }

func strField(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if s, ok := d[key].(string); ok {
		return s
	}
	return ""
}

// isTrue reports whether the data map carries a truthy bool at key (the §6 ledger distinguishes a
// knowledge.promote DEMOTE from a normal consolidation via data["demote"]==true). A non-bool / absent
// value is false. Pure.
func isTrue(d map[string]any, key string) bool {
	if d == nil {
		return false
	}
	b, _ := d[key].(bool)
	return b
}

// itoaF formats a small non-negative count (carried as a JSON float64) as an integer string for the
// ledger evidence ("3 grounded repeats"). Pure.
func itoaF(n float64) string {
	return strconv.Itoa(int(n + 0.5))
}

// numField reads a numeric value out of an event data map, tolerating the int/float64/json.Number
// forms a JSON round-trip yields. Returns (0, false) when absent or non-numeric.
func numField(d map[string]any, key string) (float64, bool) {
	if d == nil {
		return 0, false
	}
	switch v := d[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// intField reads an integer value out of an event data map (a JSON round-trip carries ints as
// float64), tolerating the same numeric forms as numField. Returns (0, false) when absent/non-numeric.
// The G4 tree/spawn-tree reconstruction reads branch ids, depths, phases, and token counts through it.
func intField(d map[string]any, key string) (int, bool) {
	if f, ok := numField(d, key); ok {
		return int(f + 0.5), true
	}
	return 0, false
}

// strSliceField reads a []string out of an event data map (a JSON round-trip carries a string list as
// []any of strings), so the §7 spawn tree can read a sub-agent's tool_scope. A non-list / absent value
// yields nil. Non-string elements are skipped (the scope is a list of tool names). Pure.
func strSliceField(d map[string]any, key string) []string {
	if d == nil {
		return nil
	}
	raw, ok := d[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// itoa2 formats an int as a string (the §5 tree's "merged into bN" reason). Pure.
func itoa2(n int) string { return strconv.Itoa(n) }

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// baseName returns the final path element (the run name shown in the header) without a directory
// prefix. A simple last-slash split is portable and deterministic (no os/filepath dependence beyond
// Open).
func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// FindRecentRecords lists the recorded EVENT logs in dir, newest-first, for the COMPARE benchmark
// LOAD (G2, the redesign §0 picker / §7 goal). It returns the paths of the *.jsonl event logs only —
// the *.signals.jsonl SignalFrame SIDECARS are excluded (they are not event logs; LoadAnalysisRecord
// finds each log's sidecar itself via SidecarPath). Newest-first by modification time, the §0 "sort
// newest" default, so the two most recent runs (the natural A/B benchmark pair) are at the front. A
// missing/unreadable dir yields nil (no records ⇒ the caller keeps the prototype A/B). This is the
// edge I/O the load path runs once on a user keypress — NOT engine logic — so reading mod-time here is
// allowed (the View layer; the engine itself stays clock-blind and deterministic).
func FindRecentRecords(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type rec struct {
		path string
		mod  int64
	}
	var recs []rec
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".signals.jsonl") {
			continue // event logs only; the sidecar is found per-log by SidecarPath
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		recs = append(recs, rec{path: dir + "/" + name, mod: info.ModTime().UnixNano()})
	}
	// newest-first; tie-break on path so the order is stable for two records written in the same instant.
	sortRecsNewestFirst(recs, func(i, j int) bool {
		if recs[i].mod != recs[j].mod {
			return recs[i].mod > recs[j].mod
		}
		return recs[i].path > recs[j].path
	})
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.path
	}
	return out
}

// sortRecsNewestFirst is a tiny insertion sort (the record count is small — a runs directory, not a
// hot path) so the loader keeps its stdlib-light footprint without pulling sort into the leaf for one
// call. less(i,j) reports whether recs[i] should precede recs[j].
func sortRecsNewestFirst[T any](recs []T, less func(i, j int) bool) {
	for i := 1; i < len(recs); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			recs[j], recs[j-1] = recs[j-1], recs[j]
		}
	}
}

// LoadComparePair loads the two most recent recorded runs in dir into an A/B AnalysisRecord pair for
// the COMPARE benchmark (newest = A, the next = B). ok is false when fewer than two records exist (the
// caller then keeps the prototype A/B rather than show a half-empty compare). The pure RecordFromFrozen
// core does the reconstruction; this only resolves WHICH two files and loads them.
func LoadComparePair(dir string) (a, b AnalysisRecord, ok bool) {
	paths := FindRecentRecords(dir)
	if len(paths) < 2 {
		return AnalysisRecord{}, AnalysisRecord{}, false
	}
	ra, err := LoadAnalysisRecord(paths[0])
	if err != nil {
		return AnalysisRecord{}, AnalysisRecord{}, false
	}
	rb, err := LoadAnalysisRecord(paths[1])
	if err != nil {
		return AnalysisRecord{}, AnalysisRecord{}, false
	}
	return ra, rb, true
}
