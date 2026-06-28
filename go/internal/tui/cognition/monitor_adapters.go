package cognition

// monitor_adapters.go — the live data path: map an end-of-tick SnapshotData onto each monitor's
// View contract. These cover the SCALAR spine (what the snapshot already carries); the per-tick
// strip histories are accrued by the pull-up frame and merged in. Pure functions.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// itoa3 renders a small count for an inline registry detail.
func itoa3(n int) string { return strconv.Itoa(n) }

// BoxedMonitor renders one titled monitor body as a boxed panel at the given total width: the title
// in the accent tone over the body, ANSI-clipped per line (renderFit) so a long gist/goal never
// WRAPS the box — the compact, no-wall-of-numbers read the locked spec requires. The tui package's
// stack assembler calls this for each panel.
func BoxedMonitor(title, body string, width int) string {
	return Panel{Body: txt(title, colAccent).render() + "\n" + body}.renderFit(width)
}

// ConsciousViewFromSnapshot maps the live snapshot onto the CONSCIOUS monitor view.
func ConsciousViewFromSnapshot(d SnapshotData) ConsciousView {
	v := ConsciousView{BestID: -1}
	if d.ActiveBranch != nil {
		v.ActiveID = *d.ActiveBranch
	}
	// active line text + thought count (the real thoughts on the active branch).
	for _, th := range d.ActiveContext {
		if th.Source != "METACOG" {
			if v.ActiveText == "" {
				v.ActiveText = th.Text
			}
			v.Thoughts++
		}
	}
	v.ActiveValue = d.Values[v.ActiveID]

	// branch counts + the strongest waiting (frontier) line — the resume pull.
	bestVal := -1.0
	for _, b := range d.Branches {
		switch b.Status {
		case "DEAD", "MERGED":
			v.DeadBranches++
			continue
		default:
			v.LiveBranches++
		}
		if b.ID != v.ActiveID && b.Value > bestVal {
			bestVal = b.Value
			v.BestID = b.ID
			v.BestText = b.Gist
			v.BestValue = b.Value
		}
	}
	return v
}

// RegulatorViewFromSnapshot maps the live snapshot onto the REGULATOR·SCHEDULER monitor view (the
// durability vitals; the scheduler budget + deferred strip are merged in by the frame).
func RegulatorViewFromSnapshot(d SnapshotData) RegulatorView {
	return RegulatorView{N: d.N, U: d.U, Mu: d.Mu, Theta: d.Theta}
}

// RegistriesViewFromSnapshot maps the live snapshot onto the REGISTRIES rollup (scale + the minted
// counts; the in-use counts + the used strip are merged in by the frame from the event history).
// This is the campaign's live verdict surface: is the learned machinery growing AND firing?
func RegistriesViewFromSnapshot(d SnapshotData) RegistriesView {
	opSeed := d.OperatorTotal - len(d.MintedOperators)
	if opSeed < 0 {
		opSeed = 0
	}
	mem := d.EpisodicCount + d.SemanticCount + d.PersonCount
	// Per-registry in-use counts are not yet wired (that needs per-registry event tallies); the rows
	// show SCALE, and the aggregate used-strip below carries the in-use signal honestly (UseVerb="").
	v := RegistriesView{
		Rows: []RegistryRow{
			{Name: "operators", Seed: opSeed, Minted: len(d.MintedOperators)},
			{Name: "specialists", Minted: len(d.MintedPrimitiveSubAgents)},
			{Name: "skills", Minted: len(d.MintedSkills)},
			{Name: "memory", Seed: mem,
				Extra: itoa3(d.EpisodicCount) + " episodes · " + itoa3(d.SemanticCount) + " beliefs · " + itoa3(d.PersonCount) + " prefs"},
		},
	}
	return v
}

// ControllerViewFromSnapshot maps the live snapshot onto the CONTROLLER monitor view (the judgment
// machinery; the escalate strip is merged in by the frame).
func ControllerViewFromSnapshot(d SnapshotData) ControllerView {
	v := ControllerView{}
	if m := d.LastMeta; m != nil {
		v.Mode = m.Mode
		v.GoalMet = m.Decision == "STOP" || m.Decision == "DELIVER"
		v.LineSpent = m.LoopExhausted
		v.NeedsTruth = m.NeedsGroundTruth
		v.Ambiguity = m.Ambiguity
		if m.Escalated {
			v.LastOutcome = "MODEL ADJUSTED"
			if m.Agree {
				v.LastOutcome = "KEPT OWN JUDGMENT"
			}
			v.LastTick = d.Tick
			v.LastReason = m.Reason
		} else if m.Mode != "" && m.Mode != "control" {
			v.LastOutcome = "KEPT OWN JUDGMENT"
			v.LastTick = d.Tick
			v.LastReason = m.Reason
		}
	}
	return v
}

// vitalsCondition composes the one-word organism story from the snapshot by fixed Pattern-A rules
// (the locked VITALS spec): runaway excitation ⇒ DEGRADED, saturated load ⇒ LOADED, awake or a user
// waiting ⇒ ENGAGED, else NOMINAL. (CONSOLIDATING is an idle-tick story not derivable here yet.)
func vitalsCondition(d SnapshotData) string {
	switch {
	case d.N >= 1.0:
		return "DEGRADED"
	case d.U >= 0.9:
		return "LOADED"
	case d.UserWaiting || strings.EqualFold(d.Arousal, "AWAKE"):
		return "ENGAGED"
	default:
		return "NOMINAL"
	}
}

// VitalsViewFromSnapshot maps the live snapshot onto the VITALS body-signal monitor. The cadence
// (wall timing) and the salient-input strip are merged in by the frame; the rest derive from the
// regulator + graph state already on the snapshot. Reserve is the budget headroom 1−U as a 0–100 read.
func VitalsViewFromSnapshot(d SnapshotData) VitalsView {
	v := VitalsView{
		Condition:         vitalsCondition(d),
		Arousal:           d.Arousal,
		N:                 d.N,
		U:                 d.U,
		GroundingInWindow: len(d.ActedBranches),
		UserWaiting:       d.UserWaiting,
	}
	res := int((1.0 - d.U) * 100)
	if res < 0 {
		res = 0
	}
	if res > 100 {
		res = 100
	}
	v.Reserve = res
	if d.LastMeta != nil {
		v.Ambiguity = d.LastMeta.Ambiguity
	}
	return v
}

// ValueViewFromSnapshot maps the live snapshot onto the VALUE monitor: the active line's priority,
// the live branch ranking (top 4 by V, live branches only), and the dominant-reason line when a user
// is waiting (the §5.6 pull). The reward strip is merged in by the frame. Quality (the epistemic
// projection) is not on the snapshot yet, so it reads 0 until the A-phase split is wired through.
func ValueViewFromSnapshot(d SnapshotData) ValueView {
	v := ValueView{ActiveID: -1}
	if d.ActiveBranch != nil {
		v.ActiveID = *d.ActiveBranch
		v.Priority = d.Values[v.ActiveID]
	}
	if d.UserWaiting {
		v.WhyText = "user is waiting on this line"
		v.WhyTerm = 0.50
	}
	rows := make([]RankRow, 0, len(d.Branches))
	for _, b := range d.Branches {
		if b.Status == "DEAD" || b.Status == "MERGED" {
			continue
		}
		rows = append(rows, RankRow{ID: b.ID, Value: b.Value})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Value > rows[j].Value })
	if len(rows) > 4 {
		rows = rows[:4]
	}
	v.Ranking = rows
	return v
}

// SubconsciousViewFromSnapshot maps the live snapshot onto the SUBCONSCIOUS monitor's running-process
// line (the recognised workflow + its phase/operator) and the dispatch threshold θ. The live-agent
// roster is event-fed (the per-firing scan), not on the end-of-tick snapshot, so it is left to the
// frame to populate; the process line + θ already tell the durable story.
func SubconsciousViewFromSnapshot(d SnapshotData) SubconsciousView {
	v := SubconsciousView{Theta: d.Theta}
	if d.Workflow != nil && d.Workflow.Recognized {
		v.Workflow = d.Workflow.Name
		v.Operator = d.Workflow.OpName
		if d.Workflow.PhaseIndex >= 0 {
			v.Phase = fmt.Sprintf("%d", d.Workflow.PhaseIndex+1)
		}
	}
	return v
}

// ActionViewFromSnapshot maps the live snapshot onto the ACTION·GROUNDING monitor: the async
// fired/returned tally (from acted branches vs the watched seam's outstanding count) and the tier-0
// fabrication alarm. The acts strip is merged in by the frame. The grounded/refuted verdict counts
// are event-fed tallies (not on the snapshot), so they read 0 until wired.
func ActionViewFromSnapshot(d SnapshotData) ActionView {
	v := ActionView{Fired: len(d.ActedBranches)}
	v.Returned = v.Fired - d.ActionOutstanding
	if v.Returned < 0 {
		v.Returned = 0
	}
	if d.LastFabricated {
		v.Fabricated = 1
		v.LastVerdict = "FABRICATED"
		v.LastTick = d.Tick
		v.LastReason = "a tier-0 fabrication was rejected at the seam"
	}
	return v
}

// MemoryViewFromSnapshot maps the live snapshot onto the MEMORY monitor's store sizes (the bi-temporal
// declarative registries). The recall strip + the per-store in-use verbs (recalled/consulted/applied)
// are event-fed and merged in by the frame.
func MemoryViewFromSnapshot(d SnapshotData) MemoryView {
	return MemoryView{
		Episodes: d.EpisodicCount,
		Beliefs:  d.SemanticCount,
		Prefs:    d.PersonCount,
	}
}

// KnowledgeViewFromSnapshot maps the live snapshot onto the KNOWLEDGE monitor. The knowledge-entry
// count + provenance tallies are not on the end-of-tick snapshot yet; the recall strip is merged in by
// the frame, and PreStocked stays 0 (the discovered-not-pre-stocked invariant reads healthy).
func KnowledgeViewFromSnapshot(d SnapshotData) KnowledgeView {
	return KnowledgeView{}
}

// OperatorsViewFromSnapshot maps the live snapshot onto the OPERATORS monitor: the catalog scale
// (seed + minted, the bounded-prompt total) and the minted-skill count. The apply strip is merged in
// by the frame; per-family / offered / fired-distinct tallies are event-fed and not on the snapshot.
func OperatorsViewFromSnapshot(d SnapshotData) OperatorsView {
	seed := d.OperatorTotal - len(d.MintedOperators)
	if seed < 0 {
		seed = 0
	}
	return OperatorsView{
		Seed:         seed,
		Minted:       len(d.MintedOperators),
		CatalogTotal: d.OperatorTotal,
		SkillsMinted: len(d.MintedSkills),
	}
}

// SessionsViewFromSnapshot maps the live snapshot onto the SESSIONS·SUB-AGENTS monitor. The spawn
// tree + budgets are event-fed (the subconscious session lifecycle), not on the end-of-tick snapshot,
// so this is a structural placeholder until that stream is wired through.
func SessionsViewFromSnapshot(d SnapshotData) SessionsView {
	return SessionsView{}
}

// TriggersViewFromSnapshot maps the live snapshot onto the TRIGGERS·SCHEDULE monitor. The armed
// sensors/drives + the schedule are continuous-mode state not on the end-of-tick snapshot yet;
// structural placeholder until wired.
func TriggersViewFromSnapshot(d SnapshotData) TriggersView {
	return TriggersView{}
}

// ThroughputViewFromSnapshot maps the live snapshot onto the THROUGHPUT monitor. Token accounting is
// not carried on the end-of-tick snapshot (it needs per-call token telemetry threaded from the
// backend); structural placeholder reading 0 until that telemetry is wired.
func ThroughputViewFromSnapshot(d SnapshotData) ThroughputView {
	return ThroughputView{}
}

// SelfViewFromSnapshot maps the live snapshot onto the SELF·EVOLUTION monitor: the deltas-since-
// baseline (minted specialists/skills/operators), the keep-or-revert reverts (demoted mints), and the
// live durability-check tally. Mode is the SAFE default (params + registry; structure/code locked).
func SelfViewFromSnapshot(d SnapshotData) SelfView {
	deltas := len(d.MintedPrimitiveSubAgents) + len(d.MintedSkills) + len(d.MintedOperators)
	pass := 0
	for _, c := range d.Stability {
		if c.Pass {
			pass++
		}
	}
	v := SelfView{
		Deltas:        deltas,
		Reverts:       len(d.Demoted),
		Mode:          "SAFE",
		StabilityPass: pass,
		StabilityTot:  len(d.Stability),
	}
	if deltas > 0 {
		v.DeltaDetail = fmt.Sprintf("+%d specialists · +%d skills · +%d operators since baseline",
			len(d.MintedPrimitiveSubAgents), len(d.MintedSkills), len(d.MintedOperators))
	}
	return v
}
