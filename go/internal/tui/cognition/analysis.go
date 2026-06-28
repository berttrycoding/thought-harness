package cognition

// analysis.go — the POST-SESSION ANALYSIS surface's data contract + sample fixture + chart
// primitives (the Shift+Tab twin of the runtime ^O monitors; mock: 2026-06-20-tui-mockups-analysis.md,
// redesign: 2026-06-20-shift-tab-analysis-redesign.md). The real G1 loader will build an
// AnalysisRecord from an event JSONL + a SignalFrame sidecar; THIS file ships the renderers + a
// deterministic SAMPLE record so the layout is viewable + reviewable BEFORE the data path exists.
// Renderers are pure over the record (analysis_panels.go).

import (
	"fmt"
	"math"
	"strings"
)

// AnalysisRecord is one loaded/frozen session — the post-session twin of the live SnapshotData. Every
// per-tick series has length == Ticks (the scrub axis); 0..1-normalised unless noted.
type AnalysisRecord struct {
	Name      string
	Substrate string
	Mode      string // "awake" | "reactive"
	Ticks     int
	WallSecs  int

	// outcome scoreboard (what a benchmark scores on)
	SolveVerdict string // "SOLVED" | "UNSOLVED"
	Delivered    int
	Grounded     int
	Refuted      int
	Fabricated   int
	Reverts      int
	TokIn        int
	TokOut       int

	// per-tick series (len == Ticks)
	Condition  []float64
	N          []float64
	U          []float64
	Theta      []float64
	VActive    []float64
	Reserve    []float64
	Pressure   []float64
	TokOutRate []float64
	GroundHits []bool

	Stimuli   []Stimulus
	Decisions []DecisionEvent
	Rewards   []RewardEvent

	// the registry/memory FAMILY (§6, G3) — the "is the learned machinery real AND in use?" view.
	// FamilyEntries is one row per learned item (operator/specialist/skill/knowledge/source) with its
	// per-tick firing history; Ledger is the mint/demote audit (with the evidence reason). Both are
	// reconstructed Pattern-A off the recorded event stream (loader.go fillFamily).
	FamilyEntries []FamilyEntry
	Ledger        []LedgerEvent

	// the DEEP ledgers + tree (§5/§7/§8/§9, G4) — the per-subsystem DETAIL over the whole session, all
	// reconstructed Pattern-A off the recorded event stream (loader.go fillDeep). A deterministic read
	// of a session that actually ran, never a re-simulation.
	Branches    []BranchNode       // §5 CONSCIOUS thought tree (one node per branch, parent-linked)
	Compression []CompressionEvent // §5 the COMPRESS/EXPAND history (the lossy-graph memory management)
	Actions     []ActionEvent      // §7 the ACTION·GROUNDING ledger (ACT -> grounded/refuted/blocked)
	Workers     []Worker           // §7 the SESSIONS·SUB-AGENTS spawn tree (the helper team it spawned)
	SpawnDepth  int                // §7 the deepest sub-agent depth reached (the spawn-tree bound)
	SpawnTokens int                // §7 the whole-tree token spend (session.terminate)
	Roles       []RoleShare        // §8 THROUGHPUT: completion tokens per CONTENT role (where the budget went)
	TierBig     int                // §8 calls on the primary/big model tier
	TierCheap   int                // §8 calls on the cheap/utility model tier
	PeakTick    int                // §8 the single most expensive tick (the slow point), -1 if none
	PeakTokens  int                // §8 the completion tokens spent at the peak tick
	SelfChanges []SelfChange       // §9 SELF·EVOLUTION: the self-change ledger (what it changed about itself)
	SelfScope   string             // §9 the governance scope it ran under (SAFE / settings-only / ...), "" if unknown
	SelfBaseSet bool               // §9 whether a persisted baseline was loaded (the lineage anchor)

	// the TRACE/FLOW swimlane (§G6) — the round-trip placed by (tick, lane). TraceEvents is the lightweight
	// per-event projection (tick + lane + kind + gist + the desync flag) the swimlane renderer places on
	// the lane×tick grid; reconstructed Pattern-A off the recorded event stream (loader.go fillTrace).
	TraceEvents []TraceEvent

	// impulse headline (stimulus → milestones), in ticks
	ImpStimulusTick        int
	ImpStimulusText        string
	ImpToFire, ImpToInject int
	ImpToAct, ImpToDeliver int
}

// Stimulus is a salient arrival marked on the scrub axis (the impulse origin).
type Stimulus struct {
	Tick int
	Kind string // "user" | "reality"
	Text string
}

// DecisionEvent is one Controller decision in the session's decision history.
type DecisionEvent struct {
	Tick     int
	Move     string // THINK/BRANCH/MERGE/BACKTRACK/ACT/STOP/DELIVER
	Reason   string
	StopKind string // GOAL_MET/GIVE_UP/… when Move==STOP
}

// RewardEvent is one grounded reward (never self-graded — always has a source observation).
type RewardEvent struct {
	Tick   int
	Value  float64
	Reason string
}

// FamilyEntry is one learned/registered item on the §6 heat map — an operator, specialist, skill,
// knowledge fact, or memory source — with WHEN it fired across the session. Family is the registry it
// belongs to ("op"/"spec"/"skill"/"know"/"src"); Name is its identity; Fires is its per-tick boolean
// history (len == Ticks, true on a tick it ran); Total is the fire count; BornTick is the mint tick (-1
// if it predates the run / was seeded); Demoted marks an item that was demoted/pruned in this session.
type FamilyEntry struct {
	Family   string
	Name     string
	Fires    []bool
	Total    int
	BornTick int
	Demoted  bool
}

// LedgerEvent is one mint/demote/prune entry in the §6 evidence ledger — the audit that says WHY a
// learned item entered or left the registry. Action is MINTED/DEMOTED/PRUNED/INVALIDATED; Family +
// Name identify the item; Evidence is the reason the engine logged (e.g. "3 grounded repeats").
type LedgerEvent struct {
	Tick     int
	Action   string
	Family   string
	Name     string
	Evidence string
}

// -- the deep ledgers + tree (§5/§7/§8/§9, G4) ------------------------------

// BranchNode is one branch in the §5 CONSCIOUS thought tree — a line of reasoning. ID is the branch
// id (b0, b2, …); Parent is the branch it forked from (-1 for the root); Text is the line's gist;
// State is EXPANDED / COMPRESSED / DEAD / MERGED; Born is the tick it forked; DeadTick + DeadReason
// mark a branch the engine abandoned (the mock's "✗ b27 DEAD @t88 (refuted)"); Active flags the one
// branch being thought about at the cursor. Reconstructed off the conscious.mcp + decision events.
type BranchNode struct {
	ID         int
	Parent     int
	Text       string
	State      string // EXPANDED | COMPRESSED | DEAD | MERGED
	Born       int
	DeadTick   int    // -1 if alive
	DeadReason string // why it died (refuted / dry line / merged-into-bN), "" if alive
}

// CompressionEvent is one COMPRESS/EXPAND in the §5 lossy-graph memory history — how the conscious
// stream manages limited focus (fold a branch to a gist, reopen a gist to push further). Op is
// COMPRESS or EXPAND; Branch is the branch id it acted on. Reconstructed off conscious.mcp ops.
type CompressionEvent struct {
	Tick   int
	Op     string // COMPRESS | EXPAND
	Branch int
}

// ActionEvent is one line in the §7 ACTION·GROUNDING ledger — the watched seam in audit form. Kind is
// ACT (intention out) / GROUNDED (reality confirmed) / REFUTED (reality corrected a belief — a HEALTHY
// revert) / BLOCKED (a fabricated/unsafe call the safety check caught) / DENIAL (a sandbox scope
// denial). Text is the intention/claim; Tool is the tool dispatched (when known); GroundedAfterTick
// links an ACT to the reality that came back. Reconstructed off action.* + grounding.* events.
type ActionEvent struct {
	Tick int
	Kind string // ACT | GROUNDED | REFUTED | BLOCKED | DENIAL
	Text string
	Tool string
}

// Worker is one sub-agent in the §7 SESSIONS·SUB-AGENTS spawn tree — a helper the engine spawned for a
// phase, each LIMITED to a tool scope (the watched-seam guarantee). Role is its job; Domain its
// specialty; ToolScope the tools it was allowed; Depth its spawn depth; Phase the phase it served.
// Reconstructed off session.dispatch + subconscious.subagent events.
type Worker struct {
	Role      string
	Domain    string
	ToolScope []string
	Depth     int
	Phase     int
}

// RoleShare is one §8 THROUGHPUT row — the completion tokens a CONTENT role (generate / transform /
// respond / summarize / operator_apply / …) spent over the session, the "where the budget went" split.
type RoleShare struct {
	Role   string
	Tokens int
}

// SelfChange is one line in the §9 SELF·EVOLUTION ledger — what the harness changed about ITSELF. Action
// is ADDED (a learned item kept) / UNDID (a change reverted by keep-or-revert) / REFUSED (a change the
// safety gate refused); Item is what changed; Evidence is the logged cause; Scope is the governance mode
// it ran under. Reconstructed off registry.batch / convert.* / persist.* events — the self-modification
// audit the mock's "a change that ISN'T in the log" bug-signature watches for.
type SelfChange struct {
	Tick     int
	Action   string // ADDED | UNDID | REFUSED
	Item     string
	Evidence string
}

// TraceEvent is one event projected onto the §G6 TRACE/FLOW swimlane — the seed->thought->seam->
// subconscious->action round-trip read by (tick, lane). Tick is the scrub-axis column; Lane is the row
// (laneOf maps the event's namespace to PORT/CONSCIOUS/SEAM/SUBCONSCIOUS/ACTION, "" for an off-lane
// event the swimlane drops); Glyph is the one-rune lane marker (the event's role within its lane);
// Kind is the wire kind (the read-back identity); Gist is a one-line summary; Desync flags a
// phase-misalignment event the swimlane highlights — a late seam.inject that re-opens a passed node or
// a conscious Reenter/expand (a retracement). Reconstructed Pattern-A off the recorded event stream.
type TraceEvent struct {
	Tick   int
	Lane   string // PORT | CONSCIOUS | SEAM | SUBCONSCIOUS | ACTION
	Glyph  string // the one-rune lane marker
	Kind   string
	Gist   string
	Desync bool // a late-injection / retracement marker (the phase-misalignment signal)
}

// -- chart primitives -------------------------------------------------------

// heatRunes is the §6 coldness-vs-topics intensity ramp (` ·▪▩█` — idle through peak). The heat map is
// "coloured by HOW HOT it ran", so a denser firing bucket gets a hotter glyph; an empty bucket is the
// faint `·` so an idle row reads as a flat cold line (the prune signal).
var heatRunes = []rune{'·', '▪', '▩', '█'}

// heatStrip renders a per-entry firing history (one bool per tick) as a fixed-width row of intensity
// glyphs: the ticks are bucketed into `width` columns and each column's glyph is keyed to its FIRING
// DENSITY (fraction of ticks in the bucket that fired), so a heavily-used entry stays hot (`█`) and an
// idle one reads cold (`·`). Pure — the §6 heat-map primitive, the coldness-vs-topics grid the mock
// specifies (mock: "coloured by how hot it ran over the session, not by glyph size").
func heatStrip(fires []bool, width int) string {
	if width <= 0 {
		return ""
	}
	if len(fires) == 0 {
		return strings.Repeat(string(heatRunes[0]), width)
	}
	out := make([]rune, width)
	for i := 0; i < width; i++ {
		lo := i * len(fires) / width
		hi := (i + 1) * len(fires) / width
		if hi <= lo {
			hi = lo + 1
		}
		if hi > len(fires) {
			hi = len(fires)
		}
		hits, n := 0, 0
		for j := lo; j < hi; j++ {
			if fires[j] {
				hits++
			}
			n++
		}
		out[i] = heatGlyph(hits, n)
	}
	return string(out)
}

// heatGlyph maps a bucket's firing density to the intensity ramp. ANY fire lifts the bucket off the
// cold floor (a single fire in a quiet bucket is still a sighting worth showing); the hotter glyphs
// require a rising fraction of the bucket to have fired.
func heatGlyph(hits, n int) rune {
	if hits == 0 || n == 0 {
		return heatRunes[0]
	}
	frac := float64(hits) / float64(n)
	switch {
	case frac >= 0.5:
		return heatRunes[3]
	case frac >= 0.2:
		return heatRunes[2]
	default:
		return heatRunes[1]
	}
}

// lastFireTick returns the last tick index at which an entry fired, or -1 if it never fired. Used to
// flag a "cold since tNN" prune candidate in the §6 view. Pure.
func lastFireTick(fires []bool) int {
	for i := len(fires) - 1; i >= 0; i-- {
		if fires[i] {
			return i
		}
	}
	return -1
}

var sparkRunes = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// spark renders a 0..1 series as a block-height sparkline of exactly `width` columns, averaging the
// series into `width` buckets (newest-right is preserved by index order). Pure.
func sparkW(vals []float64, width int) string {
	if width <= 0 || len(vals) == 0 {
		return strings.Repeat(" ", max0(width))
	}
	out := make([]rune, width)
	for i := 0; i < width; i++ {
		lo := i * len(vals) / width
		hi := (i + 1) * len(vals) / width
		if hi <= lo {
			hi = lo + 1
		}
		if hi > len(vals) {
			hi = len(vals)
		}
		var s float64
		var c int
		for j := lo; j < hi; j++ {
			s += vals[j]
			c++
		}
		v := 0.0
		if c > 0 {
			v = s / float64(c)
		}
		idx := int(v*float64(len(sparkRunes)-1) + 0.5)
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkRunes) {
			idx = len(sparkRunes) - 1
		}
		out[i] = sparkRunes[idx]
	}
	return string(out)
}

// scrubBar renders the timeline `├───●────┤` with the cursor at its proportional position. Pure.
func scrubBar(cursor, total, width int) string {
	if width < 4 {
		width = 4
	}
	inner := width - 2
	pos := 0
	if total > 0 {
		pos = cursor * (inner - 1) / total
	}
	if pos < 0 {
		pos = 0
	}
	if pos > inner-1 {
		pos = inner - 1
	}
	b := make([]rune, width)
	b[0] = '├'
	b[width-1] = '┤'
	for i := 1; i < width-1; i++ {
		b[i] = '─'
	}
	b[1+pos] = '●'
	return string(b)
}

// stimuliRow renders the stimulus markers (`▲u`/`▲r`) aligned under a bar of `width` columns. Pure.
func stimuliRow(stim []Stimulus, total, width int) string {
	row := []rune(strings.Repeat(" ", width))
	for _, s := range stim {
		if total <= 0 {
			continue
		}
		col := s.Tick * (width - 2) / total
		if col < 0 || col >= width-1 {
			continue
		}
		row[col] = '▲'
		tag := 'u'
		if s.Kind == "reality" {
			tag = 'r'
		}
		row[col+1] = tag
	}
	return string(row)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// valAt reads a 0..1 series at a tick, clamped. Pure.
func valAt(vals []float64, tick int) float64 {
	if len(vals) == 0 {
		return 0
	}
	if tick < 0 {
		tick = 0
	}
	if tick >= len(vals) {
		tick = len(vals) - 1
	}
	return vals[tick]
}

// -- the deterministic SAMPLE records (so the layout renders with no loader) --

// sampleSeries fills n points of a 0..1 curve from f(x), x in [0,1]. Deterministic (no RNG/clock).
func sampleSeries(n int, f func(x float64) float64) []float64 {
	if n < 2 {
		n = 2
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		x := float64(i) / float64(n-1)
		v := f(x)
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		out[i] = v
	}
	return out
}

func bump(x, center, width float64) float64 {
	d := (x - center) / width
	return math.Exp(-d * d)
}

// SampleAnalysisRecord returns a deterministic representative session for the prototype. variant "A" =
// power-ON (good outcome), "B" = power-OFF (drifts toward the cliff, gives up). Same task, one knob.
func SampleAnalysisRecord(variant string) AnalysisRecord {
	const T = 412
	on := variant != "B"

	stim := []Stimulus{
		{Tick: 12, Kind: "user", Text: "is this refactor safe to ship?"},
		{Tick: 88, Kind: "reality", Text: "test suite returned"},
		{Tick: 184, Kind: "user", Text: "what's the simplest case that would break it?"},
		{Tick: 300, Kind: "reality", Text: "cache config observed"},
		{Tick: 360, Kind: "reality", Text: "go test ./... exit 0"},
	}

	r := AnalysisRecord{
		Name:      "run-2026-06-20-claude-" + map[bool]string{true: "AWAKE-ON", false: "AWAKE-OFF"}[on],
		Substrate: "cc:sonnet",
		Mode:      "awake",
		Ticks:     T,
		WallSecs:  1278,
		Stimuli:   stim,
		Condition: sampleSeries(T, func(x float64) float64 { return 0.2 + 0.6*bump(x, 0.45, 0.20) }),
		Reserve:   sampleSeries(T, func(x float64) float64 { return 0.85 - 0.6*bump(x, 0.47, 0.14) }),
		Pressure:  sampleSeries(T, func(x float64) float64 { return 0.1 + 0.8*bump(x, 0.46, 0.10) }),
		Theta:     sampleSeries(T, func(x float64) float64 { return 0.5 + 0.35*bump(x, 0.5, 0.12) }),
		VActive:   sampleSeries(T, func(x float64) float64 { return 0.4 + 0.32*math.Sin(x*7) }),
		TokOutRate: sampleSeries(T, func(x float64) float64 {
			return 0.2 + 0.7*bump(x, 0.42, 0.05) + 0.4*bump(x, 0.7, 0.06)
		}),
		GroundHits: make([]bool, T),
	}
	for _, t := range []int{88, 130, 200, 300, 360, 372} {
		if t < T {
			r.GroundHits[t] = true
		}
	}

	if on {
		r.SolveVerdict, r.Delivered, r.Grounded, r.Refuted, r.Fabricated, r.Reverts = "SOLVED", 3, 9, 2, 1, 1
		r.TokIn, r.TokOut = 41000, 9000
		r.N = sampleSeries(T, func(x float64) float64 { return 0.05 + 0.35*bump(x, 0.5, 0.08) })
		r.U = sampleSeries(T, func(x float64) float64 { return 0.3 + 0.62*bump(x, 0.5, 0.22) })
		r.Decisions = []DecisionEvent{
			{Tick: 12, Move: "THINK", Reason: "open the line on the refactor"},
			{Tick: 31, Move: "BRANCH", Reason: "two failure hypotheses worth splitting"},
			{Tick: 47, Move: "ACT", Reason: "question demands ground truth the closed loop can't manufacture"},
			{Tick: 80, Move: "BACKTRACK", Reason: "this branch stopped paying out"},
			{Tick: 101, Move: "ACT", Reason: "confirm behaviour preserved against the suite"},
			{Tick: 108, Move: "STOP", StopKind: "GOAL_MET", Reason: "test suite passed, behaviour preserved"},
		}
		r.Rewards = []RewardEvent{
			{Tick: 88, Value: +1.0, Reason: "ran the test suite — 12/12 pass"},
			{Tick: 200, Value: -1.0, Reason: "believed gate forks on conflict · reality: test FAILED"},
			{Tick: 360, Value: +1.0, Reason: "go test ./... exits 0 — knowledge fact recorded"},
		}
		r.ImpStimulusTick, r.ImpStimulusText = 184, "what's the simplest case that would break it?"
		r.ImpToFire, r.ImpToInject, r.ImpToAct, r.ImpToDeliver = 1, 2, 17, 31
	} else {
		r.SolveVerdict, r.Delivered, r.Grounded, r.Refuted, r.Fabricated, r.Reverts = "UNSOLVED", 1, 4, 1, 0, 0
		r.TokIn, r.TokOut = 38000, 14000
		r.N = sampleSeries(T, func(x float64) float64 { return 0.1 + 0.7*x }) // drifts toward the cliff
		r.U = sampleSeries(T, func(x float64) float64 { return 0.3 + 0.6*math.Min(1, x*1.4) })
		r.Decisions = []DecisionEvent{
			{Tick: 12, Move: "THINK", Reason: "open the line on the refactor"},
			{Tick: 31, Move: "BRANCH", Reason: "many failure hypotheses, none decisive"},
			{Tick: 47, Move: "THINK", Reason: "keep reasoning — no ground truth imported"},
			{Tick: 80, Move: "BACKTRACK", Reason: "this branch stopped paying out"},
			{Tick: 140, Move: "BACKTRACK", Reason: "still no progress on the chain"},
			{Tick: 205, Move: "STOP", StopKind: "GIVE_UP", Reason: "exhausted the lines without grounding"},
		}
		r.Rewards = []RewardEvent{
			{Tick: 200, Value: -1.0, Reason: "guessed the answer · reality never consulted"},
		}
		r.ImpStimulusTick, r.ImpStimulusText = 184, "what's the simplest case that would break it?"
		r.ImpToFire, r.ImpToInject, r.ImpToAct, r.ImpToDeliver = 2, 4, 0, 58
	}
	r.FamilyEntries = sampleFamilyEntries(T, on)
	r.Ledger = sampleLedger(on)
	r.fillSampleDeep(on)
	return r
}

// fillSampleDeep fills the §5/§7/§8/§9 (G4) sample so the four DEEP panels render before any record is
// loaded. Deterministic (a fixed trajectory per arm, no RNG/clock). The ON arm is the healthy picture
// (a broad-and-pruned tree, real grounding, a small bounded spawn team, tiered spend, a kept+undone
// self-change pair); the OFF arm drifts (more dead branches, fewer reality checks, more tokens, no
// learned change). It mirrors the §1–§4 sample arms so a COMPARE between them tells a coherent story.
func (r *AnalysisRecord) fillSampleDeep(on bool) {
	if on {
		r.Branches = []BranchNode{
			{ID: 0, Parent: -1, Text: "is this refactor safe to ship?", State: "EXPANDED", Born: 12, DeadTick: -1},
			{ID: 2, Parent: 0, Text: "where does value pull hardest", State: "COMPRESSED", Born: 31, DeadTick: -1},
			{ID: 16, Parent: 2, Text: "which guess breaks first", State: "EXPANDED", Born: 47, DeadTick: -1},
			{ID: 11, Parent: 0, Text: "simplest case that breaks it", State: "COMPRESSED", Born: 31, DeadTick: -1},
			{ID: 41, Parent: 0, Text: "what compresses, what stays sharp", State: "EXPANDED", Born: 180, DeadTick: -1},
			{ID: 27, Parent: 2, Text: "assume the cache is cold", State: "DEAD", Born: 60, DeadTick: 88, DeadReason: "refuted by reality"},
		}
		r.Compression = []CompressionEvent{
			{Tick: 171, Op: "COMPRESS", Branch: 2},
			{Tick: 180, Op: "EXPAND", Branch: 41},
		}
		r.Actions = []ActionEvent{
			{Tick: 47, Kind: "ACT", Text: "run the test suite to confirm behaviour", Tool: "run"},
			{Tick: 49, Kind: "GROUNDED", Text: "12/12 pass", Tool: "run"},
			{Tick: 80, Kind: "ACT", Text: "check the cache assumption vs prod", Tool: "read"},
			{Tick: 83, Kind: "REFUTED", Text: "warm in prod — corrected belief b27", Tool: "read"},
			{Tick: 102, Kind: "BLOCKED", Text: "made-up result — blocked by the safety check"},
			{Tick: 91, Kind: "DENIAL", Text: "tried to write /etc outside its scope"},
		}
		r.Workers = []Worker{
			{Role: "verifier", Domain: "GROUND", ToolScope: []string{"read", "run"}, Depth: 1, Phase: 1},
			{Role: "research", Domain: "search", ToolScope: []string{"search"}, Depth: 2, Phase: 2},
			{Role: "summarizer", Domain: "summarize", ToolScope: nil, Depth: 1, Phase: 3},
		}
		r.SpawnDepth, r.SpawnTokens = 2, 9000
		r.Roles = []RoleShare{
			{Role: "generate", Tokens: 3700}, {Role: "transform", Tokens: 1980},
			{Role: "respond", Tokens: 1620}, {Role: "summarize", Tokens: 990}, {Role: "operator_apply", Tokens: 710},
		}
		r.TierBig, r.TierCheap = 18, 5
		r.PeakTick, r.PeakTokens = 178, 9400
		r.SelfChanges = []SelfChange{
			{Tick: 142, Action: "ADDED", Item: "skill verify", Evidence: "3 grounded repeats · passed gate"},
			{Tick: 152, Action: "UNDID", Item: "batch-7", Evidence: "accuracy dropped 4pp · reverted"},
		}
		r.SelfScope, r.SelfBaseSet = "SAFE · settings only", true
		return
	}
	// the OFF arm: a guess-driven run — a broad tree that mostly died, almost no grounding, more tokens,
	// no learned change (it never grounded enough to mint or keep anything).
	r.Branches = []BranchNode{
		{ID: 0, Parent: -1, Text: "is this refactor safe to ship?", State: "EXPANDED", Born: 12, DeadTick: -1},
		{ID: 2, Parent: 0, Text: "many failure hypotheses", State: "COMPRESSED", Born: 31, DeadTick: -1},
		{ID: 9, Parent: 2, Text: "guess the cache is cold", State: "DEAD", Born: 50, DeadTick: 140, DeadReason: "dry line — never grounded"},
		{ID: 14, Parent: 0, Text: "guess the lock order", State: "DEAD", Born: 70, DeadTick: 180, DeadReason: "dry line — never grounded"},
	}
	r.Compression = []CompressionEvent{{Tick: 130, Op: "COMPRESS", Branch: 2}}
	r.Actions = []ActionEvent{
		{Tick: 60, Kind: "ACT", Text: "guessed the answer · reality never consulted"},
	}
	r.Workers = nil // no helpers spawned — it never opened a real session
	r.SpawnDepth, r.SpawnTokens = 0, 0
	r.Roles = []RoleShare{
		{Role: "generate", Tokens: 8200}, {Role: "transform", Tokens: 3100}, {Role: "respond", Tokens: 2700},
	}
	r.TierBig, r.TierCheap = 26, 1
	r.PeakTick, r.PeakTokens = 198, 13900
	r.SelfChanges = nil // it learned nothing this run
	r.SelfScope, r.SelfBaseSet = "SAFE · settings only", true
}

// sampleFamilyEntries fills the §6 heat-map sample so the registry/memory family panel renders before
// any record is loaded. Deterministic (a fixed firing schedule per entry, no RNG/clock). The ON arm is
// the healthy picture (hot operators, a freshly-minted skill firing, a cold prune candidate); the OFF
// arm is sparser (less learned machinery in use).
func sampleFamilyEntries(T int, on bool) []FamilyEntry {
	fireAt := func(ticks ...int) []bool {
		b := make([]bool, T)
		for _, t := range ticks {
			if t >= 0 && t < T {
				b[t] = true
			}
		}
		return b
	}
	rangeFire := func(lo, hi, step int) []bool {
		b := make([]bool, T)
		for t := lo; t < hi && t < T; t += step {
			if t >= 0 {
				b[t] = true
			}
		}
		return b
	}
	count := func(b []bool) int {
		n := 0
		for _, v := range b {
			if v {
				n++
			}
		}
		return n
	}
	mk := func(fam, name string, born int, demoted bool, b []bool) FamilyEntry {
		return FamilyEntry{Family: fam, Name: name, Fires: b, Total: count(b), BornTick: born, Demoted: demoted}
	}
	if on {
		return []FamilyEntry{
			mk("op", "GROUND", -1, false, rangeFire(12, 380, 9)),
			mk("op", "REFRAME", -1, false, fireAt(20, 90, 200, 340)),
			mk("spec", "compute", -1, false, rangeFire(8, 360, 11)),
			mk("spec", "social", -1, false, fireAt(52)), // cold since t52 — the prune candidate
			mk("skill", "verify", 142, false, fireAt(150, 168, 240, 360)),
			mk("know", "fact-12", 168, false, fireAt(200, 300)),
		}
	}
	return []FamilyEntry{
		mk("op", "GROUND", -1, false, fireAt(12, 100)),
		mk("spec", "compute", -1, false, fireAt(8, 40)),
		mk("spec", "social", -1, false, fireAt(52)),
	}
}

// sampleLedger fills the §6 mint/demote evidence ledger sample. Deterministic.
func sampleLedger(on bool) []LedgerEvent {
	if on {
		return []LedgerEvent{
			{Tick: 142, Action: "MINTED", Family: "skill", Name: "verify", Evidence: "3 grounded repeats"},
			{Tick: 168, Action: "MINTED", Family: "know", Name: "fact-12", Evidence: "grounded fact recorded"},
			{Tick: 152, Action: "DEMOTED", Family: "spec", Name: "simulation", Evidence: "accuracy regressed @ Tier-1"},
		}
	}
	return nil
}

// moveCounts summarizes a decision history as "THINK×7 BRANCH×4 …" (top moves, fixed order). Pure.
func moveCounts(ds []DecisionEvent) string {
	order := []string{"THINK", "BRANCH", "MERGE", "BACKTRACK", "ACT", "STOP", "DELIVER"}
	n := map[string]int{}
	var stop string
	for _, d := range ds {
		n[d.Move]++
		if d.Move == "STOP" && d.StopKind != "" {
			stop = d.StopKind
		}
	}
	var parts []string
	for _, m := range order {
		if n[m] > 0 {
			parts = append(parts, fmt.Sprintf("%s×%d", m, n[m]))
		}
	}
	out := strings.Join(parts, " ")
	if stop != "" {
		out += " (" + stop + ")"
	}
	return out
}
