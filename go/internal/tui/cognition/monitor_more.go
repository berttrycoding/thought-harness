package cognition

// monitor_more.go — the registry-family + system monitors (W2): MEMORY, KNOWLEDGE, OPERATORS,
// SESSIONS·SUBAGENTS, TRIGGERS·SCHEDULE, THROUGHPUT, SELF·EVOLUTION. All pure renderers on the
// primitives, in the locked grammar.

import (
	"fmt"
	"strings"
)

// scaleRow renders a "label  SEED (+N minted) · verb N" registry-style row.
func scaleRow(name string, seed, minted, inUse int, verb, extra string) string {
	scale := fmt.Sprintf("%d", seed)
	if minted > 0 {
		scale += fmt.Sprintf(" + %d minted", minted)
	}
	row := label(name) + txt(scale, colAccent).render()
	if extra != "" {
		row += txt(" ("+extra+")", colFaint).render()
	}
	if verb != "" {
		row += txt(" · "+verb+" ", colFaint).render() + txt(fmt.Sprintf("%d", inUse), colOk).render()
	}
	return row
}

// usedStrip renders a "label  █_█.. N in Wt" used/recall lane.
func usedStrip(name string, hits []bool, w int) string {
	if w <= 0 {
		w = monitorHorizon
	}
	n := 0
	for _, h := range hits {
		if h {
			n++
		}
	}
	return label(name) + Strip(hits, w, colOk) + txt(fmt.Sprintf("   %d in %dt", n, w), colFaint).render()
}

// twoRowLast appends a "last HDR / reason SENTENCE" pair to lines when present.
func twoRowLast(lines []string, event, detail string, tick int, reason string) []string {
	if event == "" {
		return lines
	}
	hdr := event
	if detail != "" {
		hdr += " " + detail
	}
	hdr += fmt.Sprintf(" · tick %d", tick)
	lines = append(lines, label("last")+txt(hdr, colSubtext).render())
	if reason != "" {
		lines = append(lines, label("reason")+txt(reason, colMuted).render())
	}
	return lines
}

// -- MEMORY -----------------------------------------------------------------

type MemoryView struct {
	Horizon                              int
	Episodes, EpisodesRecalled           int
	Beliefs, BeliefsDistilled, Consulted int
	Prefs, PrefsApplied                  int
	Recall                               []bool
	LastEvent, LastDetail                string
	LastTick                             int
	LastReason                           string
}

func RenderMemoryMonitor(v MemoryView) string {
	var lines []string
	lines = append(lines, scaleRow("episodic", v.Episodes, 0, v.EpisodesRecalled, "recalled", ""))
	lines = append(lines, scaleRow("semantic", v.Beliefs, 0, v.Consulted, "consulted",
		fmt.Sprintf("%d distilled this session", v.BeliefsDistilled)))
	lines = append(lines, scaleRow("person", v.Prefs, 0, v.PrefsApplied, "applied", ""))
	lines = append(lines, usedStrip("recall", v.Recall, v.Horizon))
	return strings.Join(twoRowLast(lines, v.LastEvent, v.LastDetail, v.LastTick, v.LastReason), "\n")
}

// -- KNOWLEDGE --------------------------------------------------------------

type KnowledgeView struct {
	Horizon                         int
	Entries, FromReality, Distilled int
	PreStocked                      int // must be 0 by design — a violation if not
	Recalled, FedToConcretize       int
	Recall                          []bool
	LastEvent, LastDetail           string
	LastTick                        int
	LastReason                      string
}

func RenderKnowledgeMonitor(v KnowledgeView) string {
	var lines []string
	lines = append(lines, label("entries")+txt(fmt.Sprintf("%d", v.Entries), colAccent).render())
	born := label("born") + txt(fmt.Sprintf("%d", v.FromReality), colAccent).render() +
		txt(" from reality · ", colFaint).render() + txt(fmt.Sprintf("%d", v.Distilled), colAccent).render() +
		txt(" distilled · ", colFaint).render()
	psTone := colOk
	if v.PreStocked > 0 {
		psTone = colErr // a non-zero pre-stocked count is a discovered-not-pre-stocked violation
	}
	born += txt(fmt.Sprintf("%d", v.PreStocked), psTone).render() + txt(" pre-stocked", colFaint).render()
	lines = append(lines, born)
	lines = append(lines, label("used")+txt(fmt.Sprintf("%d", v.Recalled), colOk).render()+
		txt(" recalled · ", colFaint).render()+txt(fmt.Sprintf("%d", v.FedToConcretize), colOk).render()+
		txt(" fed into concretize", colFaint).render())
	lines = append(lines, usedStrip("recall", v.Recall, v.Horizon))
	return strings.Join(twoRowLast(lines, v.LastEvent, v.LastDetail, v.LastTick, v.LastReason), "\n")
}

// -- OPERATORS --------------------------------------------------------------

type OperatorsView struct {
	Horizon                       int
	Seed, Minted, Families        int
	Offered, CatalogTotal         int // the W3 bounded-prompt invariant: offered N of M
	FiredDistinct                 int
	TopMovers                     string // e.g. "GROUND 5 · REFRAME 3"
	Skills, SkillsMinted, Matched int
	Workflows                     int
	Apply                         []bool
	LastEvent, LastDetail         string
	LastTick                      int
	LastReason                    string
}

func RenderOperatorsMonitor(v OperatorsView) string {
	var lines []string
	lines = append(lines, label("catalog")+txt(fmt.Sprintf("%d seed + %d minted", v.Seed, v.Minted), colAccent).render()+
		txt(fmt.Sprintf(" · %d families", v.Families), colFaint).render())
	lines = append(lines, label("offered")+txt(fmt.Sprintf("%d of %d", v.Offered, v.CatalogTotal), colAccent).render()+
		txt(" to the last synthesis (scored subset)", colFaint).render())
	fired := label("fired") + txt(fmt.Sprintf("%d distinct", v.FiredDistinct), colAccent).render()
	if v.TopMovers != "" {
		fired += txt(" · top "+v.TopMovers, colFaint).render()
	}
	lines = append(lines, fired)
	lines = append(lines, label("skills")+txt(fmt.Sprintf("%d + %d minted", v.Skills, v.SkillsMinted), colAccent).render()+
		txt(fmt.Sprintf(" · matched %d · %d workflows synthesised", v.Matched, v.Workflows), colFaint).render())
	lines = append(lines, usedStrip("apply", v.Apply, v.Horizon))
	return strings.Join(twoRowLast(lines, v.LastEvent, v.LastDetail, v.LastTick, v.LastReason), "\n")
}

// -- SESSIONS · SUB-AGENTS --------------------------------------------------

type SubAgentRow struct {
	Name, State, Op, Tools string
	Age, Calls             int
}

type SessionsView struct {
	Horizon                 int
	Episode                 string
	Spawned, Running, Depth int
	MaxDepth                int
	Agents                  []SubAgentRow
	Spawn                   []bool
	LastEvent, LastDetail   string
	LastTick                int
	LastReason              string
}

func RenderSessionsMonitor(v SessionsView) string {
	var lines []string
	lines = append(lines, label("tree")+txt(fmt.Sprintf("episode %s", v.Episode), colSubtext).render()+
		txt(fmt.Sprintf(" · %d spawned · %d running · depth %d of %d", v.Spawned, v.Running, v.Depth, v.MaxDepth), colFaint).render())
	for _, a := range v.Agents {
		marker := "  "
		if a.State == "running" {
			marker = txt("▸ ", colOk).render()
		}
		row := marker + txt(fmt.Sprintf("%-14s", a.Name), colText).render() +
			txt(a.State, colFaint).render()
		if a.Op != "" {
			row += txt(" · op "+a.Op, colFaint).render()
		}
		if a.Tools != "" {
			row += txt(" · tools "+a.Tools, colFaint).render()
		}
		lines = append(lines, row)
	}
	lines = append(lines, usedStrip("spawn", v.Spawn, v.Horizon))
	return strings.Join(twoRowLast(lines, v.LastEvent, v.LastDetail, v.LastTick, v.LastReason), "\n")
}

// -- TRIGGERS · SCHEDULE ----------------------------------------------------

type ArmedRow struct {
	Kind, Name, Note string
	Age              int
}

type TriggersView struct {
	Horizon               int
	Sensors, Drives       int
	CooldownLeft          int
	Due                   string // e.g. "action feedback at tick 186 (in 2t) · consolidation on next idle"
	Armed                 []ArmedRow
	Fired                 []bool
	LastEvent, LastDetail string
	LastTick              int
	LastReason            string
}

func RenderTriggersMonitor(v TriggersView) string {
	var lines []string
	lines = append(lines, label("armed")+
		txt(fmt.Sprintf("%d sensors · %d drives", v.Sensors, v.Drives), colAccent).render()+
		txt(fmt.Sprintf(" · outreach cooldown %dt left", v.CooldownLeft), colFaint).render())
	if v.Due != "" {
		lines = append(lines, label("due")+txt(v.Due, colSubtext).render())
	}
	for _, a := range v.Armed {
		row := txt(fmt.Sprintf("  %-8s ", a.Kind), colFaint).render() + txt(fmt.Sprintf("%-14s", a.Name), colText).render()
		if a.Note != "" {
			row += txt(quote(a.Note), colMuted).render()
		}
		lines = append(lines, row)
	}
	lines = append(lines, usedStrip("fired", v.Fired, v.Horizon))
	return strings.Join(twoRowLast(lines, v.LastEvent, v.LastDetail, v.LastTick, v.LastReason), "\n")
}

// -- THROUGHPUT -------------------------------------------------------------

type ThroughputView struct {
	TokensInPerMin, TokensOutPerMin float64
	ThinkingPerMin, AnswerPerMin    float64
	Roles                           string // "generate 41% · transform 22% · ..."
	IntakeReality, IntakeUser       float64
	CachePct                        int
	PeakRole                        string
	PeakTokens, PeakSecs, PeakTick  int
}

func RenderThroughputMonitor(v ThroughputView) string {
	var lines []string
	lines = append(lines, label("tokens")+txt(fmt.Sprintf("in %.1fk/min", v.TokensInPerMin/1000), colAccent).render()+
		txt(" · ", colFaint).render()+txt(fmt.Sprintf("out %.1fk/min", v.TokensOutPerMin/1000), colAccent).render())
	lines = append(lines, label("thinking")+txt(fmt.Sprintf("%.1fk/min", v.ThinkingPerMin/1000), colAccent).render()+
		txt(" reasoning · ", colFaint).render()+txt(fmt.Sprintf("%.1fk/min", v.AnswerPerMin/1000), colAccent).render()+
		txt(" answer", colFaint).render())
	if v.Roles != "" {
		lines = append(lines, label("roles")+txt(v.Roles, colSubtext).render())
	}
	lines = append(lines, label("intake")+txt(fmt.Sprintf("reality %.1fk tok/min", v.IntakeReality/1000), colAccent).render()+
		txt(" · ", colFaint).render()+txt(fmt.Sprintf("user %.1fk tok/min", v.IntakeUser/1000), colAccent).render())
	lines = append(lines, label("cache")+txt(fmt.Sprintf("%d%%", v.CachePct), colAccent).render()+
		txt(" of input cached", colFaint).render())
	if v.PeakRole != "" {
		lines = append(lines, label("peak")+txt(v.PeakRole, colSubtext).render()+
			txt(fmt.Sprintf(" · %d tokens · %ds · tick %d", v.PeakTokens, v.PeakSecs, v.PeakTick), colFaint).render())
	}
	return strings.Join(lines, "\n")
}

// -- SELF · EVOLUTION -------------------------------------------------------

type SelfView struct {
	Build, StateName            string
	Deltas                      int
	DeltaDetail                 string // "+2 specialists · +1 skill · +3 beliefs since baseline"
	Reverts                     int
	Mode                        string // SAFE | EXPAND | REWRITE
	StartupChecks, StartupTotal int
	StabilityPass, StabilityTot int
	StabilityAge                int
	LastEvent, LastDetail       string
	LastTick                    int
	LastReason                  string
}

func RenderSelfMonitor(v SelfView) string {
	var lines []string
	lines = append(lines, label("version")+txt("build "+v.Build, colSubtext).render()+
		txt(fmt.Sprintf(" · state %q +%d deltas", v.StateName, v.Deltas), colFaint).render())
	if v.DeltaDetail != "" {
		lines = append(lines, label("deltas")+txt(v.DeltaDetail, colSubtext).render())
	}
	lines = append(lines, label("reverts")+txt(fmt.Sprintf("%d this session", v.Reverts), colAccent).render())
	// mode: SAFE is the default; structure/code are EXPERIMENTAL + locked.
	modeRow := label("mode") + txt(v.Mode, colOk).render()
	if v.Mode == "SAFE" {
		modeRow += txt(" (params + registry) · structure EXPERIMENTAL · code EXPERIMENTAL", colFaint).render()
	}
	lines = append(lines, modeRow)
	lines = append(lines, label("checks")+
		txt(fmt.Sprintf("startup %d/%d roles OK", v.StartupChecks, v.StartupTotal), colFaint).render()+
		txt(" · ", colFaint).render()+
		txt(fmt.Sprintf("stability %d/%d PASS (%s)", v.StabilityPass, v.StabilityTot, ageLabel(v.StabilityAge)), colFaint).render())
	return strings.Join(twoRowLast(lines, v.LastEvent, v.LastDetail, v.LastTick, v.LastReason), "\n")
}
