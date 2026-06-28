package cognition

// mechanisms.go — the "surface every mechanism" panels. panels.go renders each LAYER as a compact
// overview; these render the deeper MECHANISMS the engine emits but the overview can't fit, so the
// architecture can be tuned by watching each one work. Every renderer here is a PURE VIEW over the
// event stream (the observability contract: "a mechanism is visible by rendering its events"). They are
// surfaced through the per-system TABS (railPanelsFor → tabForPanel), not the curated Overview.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// renderFrontier — the CONSCIOUS thinking seen AS A* best-first search: the CURRENT node (the one
// EXPANDED branch), the OPEN set (stashed siblings, ranked by V(s) — the heuristic that picks what to
// pursue next), and the CLOSED set (DEAD / MERGED, pruned). Each frontier row shows V, depth (g), the
// thought-count cost, and the branch's gist + fork reason — the search structure the flat graph hides.
func renderFrontier(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	if len(s.Branches) == 0 {
		return Panel{Body: faintStr(clip("(no graph — submit a goal)", w))}
	}
	var lines []string
	// the current (active, EXPANDED) node.
	if s.ActiveBranch != nil {
		for _, b := range s.Branches {
			if b.ID == *s.ActiveBranch {
				lines = append(lines, txt("current", colSubtext).render())
				head := fmt.Sprintf("  ●b%d  v%.2f  d%d  (%d thoughts)", b.ID, b.Value, b.Depth, b.ThoughtCount)
				lines = append(lines, txt(clip(head, w), colAccent).render())
				if b.Gist != "" {
					lines = append(lines, wrapEntry(txt("    ", colFaint).render(), 4, b.Gist, colFaint, w)...)
				}
			}
		}
	}
	// the open set (stashed siblings), ranked by V(s) — the rerank heuristic.
	var open []BranchVM
	for _, b := range s.Branches {
		if b.Status == "STASHED" {
			open = append(open, b)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].Value > open[j].Value })
	openTotal := len(open)
	if len(open) > maxBranchRows { // L3: the frontier is the bulk of a 100-branch graph — cap, best-V first
		open = open[:maxBranchRows]
	}
	lines = append(lines, "")
	lines = append(lines, txt(clip("frontier · open set (best V first)", w), colSubtext).render())
	if openTotal == 0 {
		lines = append(lines, faintStr(clip("  (empty — nothing stashed to revisit)", w)))
	}
	for _, b := range open {
		head := fmt.Sprintf("  ○b%d  v%.2f  d%d  ×%d", b.ID, b.Value, b.Depth, b.ThoughtCount)
		left := txt(head+"  ", colMuted).render()
		lines = append(lines, ansi.Truncate(left+txt(b.Gist, colFaint).render(), w, "…"))
		if b.Reason != "" {
			lines = append(lines, wrapEntry(txt("       ↳ ", colFaint).render(), 9, b.Reason, colFaint, w)...)
		}
	}
	if openTotal > len(open) {
		lines = append(lines, faintStr(clip(fmt.Sprintf("  …+%d more in frontier", openTotal-len(open)), w)))
	}
	// the closed set (pruned): DEAD / MERGED.
	var closed []string
	for _, b := range s.Branches {
		if b.Status == "DEAD" || b.Status == "MERGED" {
			closed = append(closed, fmt.Sprintf("b%d(%s)", b.ID, b.Status))
		}
	}
	if len(closed) > 0 {
		lines = append(lines, wrapEntry(txt("closed: ", colMuted).render(), 8, strings.Join(closed, " "), colFaint, w)...)
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderGenerative — the SUBCONSCIOUS generative layer (operators → synthesised programs → sub-agents →
// skills). This is the open, runtime-minting engine: the Synthesizer builds a Program (a Seq/Par/Loop of
// operators) on the fly, minting+verifying new operators as needed; each operator step can instantiate a
// sub-agent (role/persona/tool-scope); recurring shapes get matched to / minted as named skills. It reads
// subconscious.{synthesize,operator,subagent,skill_match,skill_mint}.
func renderGenerative(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string

	// the latest synthesised PROGRAM: its shape + provenance (skill: / llm / heuristic) + rationale.
	if ev := lastEvent(vm, events.SubSynthesize); ev != nil {
		lines = append(lines, txt("program", colSubtext).render())
		lines = append(lines, wrapEntry(txt("  shape  ", colMuted).render(), 9, dataStr(ev, "shape"), colAccent, w)...)
		if src := dataStr(ev, "source"); src != "" {
			lines = append(lines, lineLR(txt("  source", colMuted).render(), txt(src, colSubtext).render(), w))
		}
		if r := dataStr(ev, "rationale"); r != "" {
			lines = append(lines, wrapEntry(txt("  why    ", colMuted).render(), 9, r, colFaint, w)...)
		}
	} else {
		lines = append(lines, faintStr(clip("(no program synthesised yet — submit a goal)", w)))
	}

	// operators MINTED at runtime (new capability verified into the catalog), newest first.
	if ops := eventsOfKind(vm, events.SubOperator, 4); len(ops) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt("operators minted", colSubtext).render())
		for i := range ops {
			e := &ops[i]
			head := join(
				txt("  + "+dataStr(e, "name"), colOk),
				txt(" ("+dataStr(e, "family")+")", colFaint),
			)
			lines = append(lines, wrapEntry(head, lipgloss.Width(ansi.Strip(head))+1, dataStr(e, "intent"), colFaint, w)...)
		}
	}

	// sub-agents INSTANTIATED for an operator step (the ephemeral role/persona/tool-scope worker).
	if sas := eventsOfKind(vm, events.SubSubagent, 4); len(sas) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt("sub-agents", colSubtext).render())
		for i := range sas {
			e := &sas[i]
			title := join(
				txt("  ▸ "+dataStr(e, "role"), colAccent),
				txt(" · "+dataStr(e, "domain"), colMuted),
			)
			lines = append(lines, ansi.Truncate(title, w, "…"))
			tools := "—"
			if ts := dataStrings(e, "tool_scope"); len(ts) > 0 {
				tools = strings.Join(ts, ", ")
			}
			lines = append(lines, wrapEntry(txt("      tools ", colMuted).render(), 12, tools, colFaint, w)...)
			if r := dataStr(e, "responsibility"); r != "" {
				lines = append(lines, wrapEntry(txt("      does  ", colMuted).render(), 12, r, colFaint, w)...)
			}
		}
	}

	// skills: a goal MATCHED a library skill, or a recurring program was MINTED into a named skill.
	var skillLines []string
	if ev := lastEvent(vm, events.SkillMatch); ev != nil {
		skillLines = append(skillLines, wrapEntry(txt("  matched ", colMuted).render(), 10, dataStr(ev, "skill")+" ("+dataStr(ev, "shape")+")", colSubtext, w)...)
	}
	mints := eventsOfKind(vm, events.SkillMint, 3)
	for i := range mints {
		skillLines = append(skillLines, wrapEntry(txt("  minted  ", colMuted).render(), 10, dataStr(&mints[i], "name"), colOk, w)...)
	}
	if len(skillLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt("skills", colSubtext).render())
		lines = append(lines, skillLines...)
	}

	return Panel{Body: strings.Join(lines, "\n")}
}

// renderConvert — CONVERTIBILITY made watchable BEFORE it fires: the effortful patterns progressing
// toward minting a specialist (count vs MintAfter, value vs the value-gate), the recurring program
// shapes progressing toward a skill, and the gate-win domain tally progressing toward a gate prior.
// This is where "effortful → automatic" is tuned — you can see why something did or didn't compile.
func renderConvert(vm ViewModel) Panel {
	s := vm.Snap
	w := contentW(vm)
	var lines []string
	if len(s.ConvCandidates) == 0 && len(s.ConvPrograms) == 0 && len(s.DomainTally) == 0 && len(s.Demoted) == 0 {
		return Panel{Body: faintStr(clip("(nothing learning yet — patterns compile after repeats)", w))}
	}

	// patterns → specialists: <goalKey>  <gen>/<MintAfter>  v<val>/<gate>, tone by mint readiness.
	if len(s.ConvCandidates) > 0 {
		lines = append(lines, txt(clip(fmt.Sprintf("patterns → specialists (mint at %d · value ≥ %.2f)", s.MintAfter, s.MintValue), w), colSubtext).render())
		for i, c := range s.ConvCandidates {
			if i >= maxConvRows {
				break
			}
			right := fmt.Sprintf("%d/%d · v%.2f", c.Generated, s.MintAfter, c.Value)
			rc := colFaint
			switch {
			case c.Minted:
				rc = colOk
			case c.Generated >= s.MintAfter && c.Value < s.MintValue:
				rc = colWarn // repeated enough but value-gated out
			case c.Generated >= s.MintAfter:
				rc = colAccent // ready to mint
			}
			lines = append(lines, leaderRow("  "+c.GoalKey, colMuted, right, rc, w))
		}
		if extra := len(s.ConvCandidates) - maxConvRows; extra > 0 {
			lines = append(lines, faintStr(clip(fmt.Sprintf("  …+%d more", extra), w)))
		}
	}

	// programs → skills.
	if len(s.ConvPrograms) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip(fmt.Sprintf("programs → skills (mint at %d)", s.MintAfter), w), colSubtext).render())
		for i, p := range s.ConvPrograms {
			if i >= maxConvRows {
				break
			}
			rc := colFaint
			if p.Minted {
				rc = colOk
			} else if p.Count >= s.MintAfter {
				rc = colAccent
			}
			lines = append(lines, leaderRow("  "+p.Shape, colMuted, fmt.Sprintf("×%d/%d", p.Count, s.MintAfter), rc, w))
		}
		if extra := len(s.ConvPrograms) - maxConvRows; extra > 0 {
			lines = append(lines, faintStr(clip(fmt.Sprintf("  …+%d more", extra), w)))
		}
	}

	// gate priors: the domain tally toward a compiled standing bias (after MetacogAfter metacog ops).
	if len(s.DomainTally) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip(fmt.Sprintf("gate priors (compile at %d metacog · runs %d/%d)", s.MetacogAfter, s.MetacogRuns, s.MetacogAfter), w), colSubtext).render())
		// stable order: by tally desc.
		type kv struct {
			dom string
			v   float64
		}
		kvs := make([]kv, 0, len(s.DomainTally))
		for d, v := range s.DomainTally {
			kvs = append(kvs, kv{d, v})
		}
		sort.SliceStable(kvs, func(i, j int) bool { return kvs[i].v > kvs[j].v })
		for i, e := range kvs {
			if i >= maxConvRows {
				break
			}
			lines = append(lines, leaderRow("  "+e.dom, colMuted, fmt.Sprintf("%.1f wins", e.v), colSubtext, w))
		}
		if extra := len(kvs) - maxConvRows; extra > 0 {
			lines = append(lines, faintStr(clip(fmt.Sprintf("  …+%d more", extra), w)))
		}
	}

	// DEMOTE (keep-or-revert, P0.5): mints that reality later REFUTED are reverted — the specialist stops
	// firing + the pattern drops below the mint floor. Shown in the err tone (a learned thing un-learned).
	if len(s.Demoted) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("demoted (reality refuted the mint)", w), colSubtext).render())
		for _, d := range s.Demoted {
			lines = append(lines, leaderRow("  "+d, colMuted, "reverted", colErr, w))
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderMCP — the CONSCIOUS metacognitive ops (the Thought MCP): branch / merge / rerank / compress /
// expand / focus reshape the thought graph, and typed cross-refs (CONTRADICTS / SUPERSEDES / SUPPORTS)
// wire branches. Reads conscious.mcp + conscious.xref. This is the deliberate control over the graph
// that the static graph panel can't show (it shows the result, not the moves).
func renderMCP(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string
	ops := eventsOfKind(vm, events.MCP, 8)
	if len(ops) == 0 {
		lines = append(lines, faintStr(clip("(no metacognitive ops yet)", w)))
	}
	for i := range ops {
		e := &ops[i]
		op := dataStr(e, "op")
		lines = append(lines, wrapEntry(txt("  "+padRight(op, 9), colAccent).render(), 11, e.Summary, colSubtext, w)...)
	}
	if xrefs := eventsOfKind(vm, events.XRef, 6); len(xrefs) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt("cross-refs", colSubtext).render())
		for i := range xrefs {
			c := colAccent
			switch {
			case strings.Contains(xrefs[i].Summary, "CONTRADICTS"):
				c = colErr
			case strings.Contains(xrefs[i].Summary, "SUPPORTS"):
				c = colOk
			case strings.Contains(xrefs[i].Summary, "SUPERSEDES"):
				c = colWarn
			}
			lines = append(lines, txt(clip("  "+xrefs[i].Summary, w), c).render())
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderToolExec — the WATCHED seam's real-tool dispatch: each action.tool (ok / exit code), plus the
// gates that can stop a call (sandbox-deny on a file write, safety-block on a command, blocked) and the
// outward actions (respond / ask). This is reality wiring — the only place ground truth enters.
func renderToolExec(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string
	tools := eventsOfKind(vm, events.ActionTool, 6)
	for i := range tools {
		e := &tools[i]
		mark, c := "✓", colOk
		if !dataBool(e, "ok") {
			mark, c = "✗", colErr
		}
		lines = append(lines, wrapEntry(txt("  "+mark+" ", c).render(), 4, e.Summary, colSubtext, w)...)
	}
	// the safety / sandbox / blocked gates (a denied or blocked call — important to SEE when tuning).
	for _, k := range []string{events.ActionSandboxDeny, events.ActionSafetyBlock, events.ActionBlocked} {
		for _, e := range eventsOfKind(vm, k, 2) {
			lines = append(lines, wrapEntry(txt("  ⊘ ", colErr).render(), 4, e.Summary, colErr, w)...)
		}
	}
	// outward-facing actions: the answer (respond) and the ask-the-user (ask).
	if ev := lastEvent(vm, events.Respond); ev != nil {
		lines = append(lines, wrapEntry(txt("  → respond ", colMuted).render(), 12, ev.Summary, colSubtext, w)...)
	}
	if ev := lastEvent(vm, events.Ask); ev != nil {
		lines = append(lines, wrapEntry(txt("  ? ask ", colMuted).render(), 8, ev.Summary, colWarn, w)...)
	}
	if len(lines) == 0 {
		lines = append(lines, faintStr(clip("(no tool calls yet — the harness has not acted)", w)))
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderScheduler — the V(s)-keyed LLM-call rate actuator + the Pattern-C escalation health.
// Foreground roles (reasoning, decision, the Filter escalation) are always served; background CONTENT
// roles (specialists, synthesis) share a per-tick budget keyed on the active line's value, and a
// deferred background call skips the model (Pattern B — the content gap is surfaced, never a
// substituted template). Reads regulator.schedule (a defer = a background call that hit the budget).
// The deferrals are how you tell the model is the bottleneck. Below the budget it shows the Pattern-C
// ESCALATION/floor-stands health from escalation.floor_stands — per site (filter / critic), how often
// the deterministic FLOOR stood because the model was not consulted or declined (Rule 4). That line
// only appears in an llm/hybrid run; the default control mode escalates nothing and emits none.
func renderScheduler(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string
	lines = append(lines, faintStr(clip("foreground always served · background V(s)-gated", w)))
	defers := eventsOfKind(vm, events.Schedule, 8)
	if len(defers) == 0 {
		lines = append(lines, faintStr(clip("(no deferrals — every call served)", w)))
	} else {
		lines = append(lines, lineLR(txt("deferred (model skipped)", colMuted).render(), txt(fmt.Sprintf("%d", len(defers)), colWarn).render(), w))
		for i := range defers {
			lines = append(lines, wrapEntry(txt("  ⌛ ", colWarn).render(), 4, defers[i].Summary, colFaint, w)...)
		}
	}
	lines = append(lines, escalationHealthLines(vm, w)...)
	return Panel{Body: strings.Join(lines, "\n")}
}

// escalationHealthLines is the Pattern-C health: per site (filter.admit / critic.decide), the count of
// escalation.floor_stands — the deterministic FLOOR standing because the model was not consulted or
// declined (Rule 4). Returns no lines when nothing escalated (the default control mode), so the
// scheduler panel is unchanged on the default path. modelConsulted distinguishes a model that was asked
// and DECLINED (a degraded escalation) from a case the floor simply held without asking.
func escalationHealthLines(vm ViewModel, w int) []string {
	stands := eventsOfKind(vm, events.EscalationFloorStands, 64)
	if len(stands) == 0 {
		return nil
	}
	// tally per site: total floor-stands + how many followed a model that was actually consulted.
	type tally struct{ total, consulted int }
	bySite := map[string]*tally{}
	order := []string{}
	for i := range stands {
		site := dataStr(&stands[i], "site")
		if site == "" {
			site = "?"
		}
		t := bySite[site]
		if t == nil {
			t = &tally{}
			bySite[site] = t
			order = append(order, site)
		}
		t.total++
		if dataBool(&stands[i], "model_consulted") {
			t.consulted++
		}
	}
	var lines []string
	lines = append(lines, lineLR(txt("Pattern-C · floor stood", colMuted).render(),
		txt(fmt.Sprintf("%d", len(stands)), colWarn).render(), w))
	for _, site := range order {
		t := bySite[site]
		// short site label: "filter" / "critic" off "filter.admit" / "critic.decide".
		label := site
		if i := strings.IndexByte(label, '.'); i >= 0 {
			label = label[:i]
		}
		detail := fmt.Sprintf("×%d", t.total)
		if t.consulted > 0 {
			detail += fmt.Sprintf(" (%d model declined)", t.consulted)
		}
		lines = append(lines, lineLR(txt("  "+label, colSubtext).render(), txt(detail, colFaint).render(), w))
	}
	return lines
}

// renderBackend — the language faculty's health (the `thought doctor` view, live): each llm.call (role ·
// latency) and each llm.fallback (a CONTENT role whose model was unavailable — server down / timeout /
// unparseable JSON — so the gap is surfaced, never a substituted template). Intelligence is the model's;
// the deterministic control is architecture. Watching the OK vs gap mix tells you which subsystems a
// given local model can actually drive.
func renderBackend(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string
	// interleave calls + fallbacks by recency (just read the recent tail of both kinds).
	calls := eventsOfKind(vm, events.LLM, 6)
	fbs := eventsOfKind(vm, events.LLMFallback, 6)
	if len(calls) == 0 && len(fbs) == 0 {
		lines = append(lines, faintStr(clip("(no model calls — test backend / offline)", w)))
		return Panel{Body: strings.Join(lines, "\n")}
	}
	for i := range calls {
		e := &calls[i]
		left := join(txt("  ✓ ", colOk), txt(dataStr(e, "role"), colSubtext))
		ms := fmt.Sprintf("%dms", int(dataFloat(e, "ms")))
		lines = append(lines, lineLR(left, txt(ms, colFaint).render(), w))
	}
	for i := range fbs {
		lines = append(lines, wrapEntry(txt("  ⚠ ", colWarn).render(), 4, fbs[i].Summary, colWarn, w)...)
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderContinuous — the AWAKE/continuous-mode home (its own COGNITION tab, E1). It gathers every awake-
// regime signal the reactive loop never produces: Arousal (sleep/wake) + its recent transitions, the
// awake DECISION policy (continuous.decision — resume / curiosity / wander / reach-out / stay-quiet), the
// endogenous activity stream (Drives + Default-mode, on the port), and PROACTIVE OUTREACH (the engine
// speaking unprompted — tagged here so it is distinguishable from a reactive reply). Reactive mode shows
// the arousal line + a dormant note; awake mode shows the full endogenous picture.
func renderContinuous(vm ViewModel) Panel {
	w := contentW(vm)
	s := vm.Snap
	var lines []string
	lines = append(lines, lineLR(txt("arousal", colMuted).render(), txt(s.Arousal, ArousalColorFor(s.Arousal)).render(), w))
	lines = append(lines, lineLR(txt("mode", colMuted).render(), txt(s.Mode, colSubtext).render(), w))

	ports := eventsOfKind(vm, events.Port, maxConvRows)
	transitions := eventsOfKind(vm, events.Arousal, 4)
	decisions := eventsOfKind(vm, events.ContinuousDecision, 4)
	outreach := outreachEvents(vm, maxConvRows)

	if s.Mode != "continuous" && len(ports) == 0 && len(decisions) == 0 && len(outreach) == 0 {
		lines = append(lines, "")
		lines = append(lines, faintStr(clip("(reactive mode — arousal/drives/default-mode/outreach dormant;", w)))
		lines = append(lines, faintStr(clip(" run --mode continuous for the awake regime)", w)))
		return Panel{Body: strings.Join(lines, "\n")}
	}

	// arousal transitions — the sleep/wake history (most-recent last).
	if len(transitions) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("arousal transitions", w), colSubtext).render())
		for i := range transitions {
			to := dataStr(&transitions[i], "to")
			lines = append(lines, lineLR(txt("  → "+transitions[i].Summary, colFaint).render(),
				txt(to, ArousalColorFor(to)).render(), w))
		}
	}

	// the awake DECISION policy — what the engine chose to do this tick (resume / curiosity / wander /
	// reach-out / stay-quiet), the GATE-ON contrast the continuous probe surfaces.
	if len(decisions) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("awake decision", w), colSubtext).render())
		for i := range decisions {
			lines = append(lines, wrapEntry(txt("  ◆ ", colAccent).render(), 4, decisions[i].Summary, colText, w)...)
		}
	}

	// PROACTIVE OUTREACH — the engine reaching out unprompted (tagged; this is the one awake action that
	// crosses the watched seam to the user, so it reads distinctly from a reactive answer).
	if len(outreach) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("proactive outreach (unprompted)", w), colOk).render())
		for i := range outreach {
			msg := strings.TrimPrefix(outreach[i].Summary, "(unprompted) ")
			val := dataFloat(&outreach[i], "value")
			head := txt("  ◀ reached out", colOk).render()
			lines = append(lines, lineLR(head, txt(fmt.Sprintf("V=%.2f", val), colFaint).render(), w))
			lines = append(lines, wrapEntry("    ", 4, msg, colSubtext, w)...)
		}
	}

	// endogenous activity — Drives (resume/curiosity) + Default-mode wander, on the perception port.
	if len(ports) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("endogenous activity (drives · default-mode)", w), colSubtext).render())
		for i := range ports {
			lines = append(lines, wrapEntry(txt("  • ", colAccent).render(), 4, ports[i].Summary, colFaint, w)...)
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// outreachEvents returns the last n proactive-outreach actions — action.respond events tagged
// kind=outreach by maybeReachOut (continuous.go). They are pulled out of the plain Respond stream so the
// awake tab can render them distinctly from reactive replies (E1: "indistinguishable" was the symptom).
func outreachEvents(vm ViewModel, n int) []events.Event {
	var out []events.Event
	for _, e := range vm.Events {
		if e.Kind == events.Respond && dataStr(&e, "kind") == "outreach" {
			out = append(out, e)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// rungColorFor maps a sourcing-ladder rung NAME to its tone (M3 §1.3): the four GROUNDED rungs are
// trusted tiers (reality the highest green, present/knowledge/memory the grayscale-up), and `generated`
// is the LOW-TRUST floor (warn) — the marker the panel flags, mirroring the FABRICATED marker so an
// ungrounded invention is visible at a glance. `none` (nothing sourced) is the err tone.
func rungColorFor(rung string) lipgloss.Color {
	switch rung {
	case "reality":
		return colOk // a real observation crossed the watched seam — the highest trust
	case "present", "knowledge", "memory":
		return colSubtext // a grounded, trusted source
	case "generated":
		return colWarn // the model invented it — LOW trust (the floor the Filter distrusts at 0.42)
	case "none":
		return colErr // nothing sourced (and generation forbidden) — the candidate was dropped
	default:
		return colText
	}
}

// renderSourcing — the SOURCING LADDER (M3 §3.2): watch a fuel-needing move's need fall through the five
// wells in strict order (present → knowledge → memory → reality → generated), or bottom out at none. A
// pure view over subconscious.source (the rung resolution) + subconscious.concretize (the fuse-or-drop).
// The `generated` rung is flagged LOW-TRUST (the ungrounded floor) so an invention is visible — the
// sourcing-side analog of the FABRICATED marker. Empty when no fuel-needing move has fired.
func renderSourcing(vm ViewModel) Panel {
	w := contentW(vm)
	srcs := eventsOfKind(vm, events.SubSource, 8)
	cons := eventsOfKind(vm, events.SubConcretize, 6)
	if len(srcs) == 0 && len(cons) == 0 {
		var empty []string
		for _, ln := range wrapPlain("(no fuel-needing move yet — a GROUND/REFRAME op draws fuel through "+
			"the ladder: present → knowledge → memory → reality → generated)", w, w) {
			empty = append(empty, faintStr(ln))
		}
		return Panel{Body: strings.Join(empty, "\n")}
	}
	var lines []string
	if len(srcs) > 0 {
		lines = append(lines, txt(clip("resolved needs  (rung · provider · trust)", w), colSubtext).render())
		for i := range srcs {
			e := &srcs[i]
			rung := dataStr(e, "rung")
			rc := rungColorFor(rung)
			tag := "  " + padRight(rung, 9)
			right := fmt.Sprintf("t%.2f", dataFloat(e, "trust"))
			if !dataBool(e, "grounded") {
				right += " ungrounded"
			}
			left := txt(tag, rc).render() + txt(dataStr(e, "query"), colFaint).render()
			lines = append(lines, lineLR(left, txt(right, rc).render(), w))
		}
	}
	if len(cons) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("concretize  (fuse the sourced fuel before the seam, or DROP)", w), colSubtext).render())
		for i := range cons {
			e := &cons[i]
			dropped := dataBool(e, "dropped")
			rung := dataStr(e, "rung")
			c := rungColorFor(rung)
			glyph := "  ▸ "
			if dropped {
				glyph, c = "  ⊘ ", colErr
			}
			prefix := txt(glyph+dataStr(e, "operator")+" ", c).render()
			body := dataStr(e, "to")
			if dropped {
				body = "DROP (unsourced — never voiced a hollow candidate)"
			}
			lines = append(lines, wrapEntry(prefix, lipgloss.Width(ansi.Strip(prefix)), body, c, w)...)
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderKnowledge — the KNOWLEDGE registry stream (M3 §3.1): the durable domain-knowledge layer
// (third-person, never-fabricate) made watchable — each knowledge.record (a grounded item entered, its
// kind/source/trust), knowledge.recall (the registry surfaced a hit for a query), and knowledge.invalidate
// (a refuted item bi-temporally retired). A pure view over the knowledge.* stream. Empty until the first
// grounded item enters (knowledge starts empty and earns it from reality + distillation).
func renderKnowledge(vm ViewModel) Panel {
	w := contentW(vm)
	recs := eventsOfKind(vm, events.KnowledgeRecord, 6)
	recalls := eventsOfKind(vm, events.KnowledgeRecall, 4)
	invs := eventsOfKind(vm, events.KnowledgeInvalidate, 3)
	if len(recs) == 0 && len(recalls) == 0 && len(invs) == 0 {
		var empty []string
		for _, ln := range wrapPlain("(knowledge empty — it earns items from reality write-back + "+
			"distillation; a recorded fact is a rung-2 hit next time the ladder needs it)", w, w) {
			empty = append(empty, faintStr(ln))
		}
		return Panel{Body: strings.Join(empty, "\n")}
	}
	var lines []string
	if len(recs) > 0 {
		lines = append(lines, txt(clip("recorded  (kind · source · trust)", w), colSubtext).render())
		for i := range recs {
			e := &recs[i]
			kind := dataStr(e, "kind")
			prefix := txt("  + ["+kind+"] ", colOk).render()
			lines = append(lines, wrapEntry(prefix, lipgloss.Width(ansi.Strip(prefix)), e.Summary, colSubtext, w)...)
			meta := "source " + dataStr(e, "source") + fmt.Sprintf(" · trust %.2f", dataFloat(e, "trust"))
			lines = append(lines, txt("    ↳ "+clip(meta, w-6), colFaint).render())
		}
	}
	if len(recalls) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("recalled  (query → hits)", w), colSubtext).render())
		for i := range recalls {
			e := &recalls[i]
			right := fmt.Sprintf("%d hit", int(dataFloat(e, "hits")))
			lines = append(lines, lineLR(txt("  ? "+dataStr(e, "query"), colMuted).render(), txt(right, colSubtext).render(), w))
		}
	}
	if len(invs) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("invalidated  (reality refuted — kept for audit)", w), colSubtext).render())
		for i := range invs {
			lines = append(lines, wrapEntry(txt("  ✗ ", colErr).render(), 4, invs[i].Summary, colErr, w)...)
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderRetrieval — the HYBRID RETRIEVER score breakdown (the shared precision floor under memory +
// knowledge): each retrieval.fused (the mode lexical/hybrid, the top lexical score, candidates scanned,
// recalled), plus the memory.recall / memory.record / memory.reflect stream the retriever feeds. A pure
// view over retrieval.* + memory.*. The mode line is always shown so the retriever primitive is visible
// even on a cold store (no recall yet) — the "always-visible mode" requirement.
func renderRetrieval(vm ViewModel) Panel {
	w := contentW(vm)
	s := vm.Snap
	var lines []string
	// the retriever MODE is always shown (hybrid = embedder reachable; lexical = offline floor).
	mode := s.RetrieverMode
	if mode == "" {
		mode = "lexical"
	}
	mc := colSubtext
	if mode == "hybrid" {
		mc = colOk
	}
	lines = append(lines, lineLR(txt("retriever mode", colMuted).render(), txt(mode, mc).render(), w))
	lines = append(lines, leaderRow("episodic · semantic", colMuted,
		fmt.Sprintf("%d · %d", s.EpisodicCount, s.SemanticCount), colText, w))

	// the FUSED score breakdown for each recent retrieval (mode · lexical · candidates · recalled).
	fused := eventsOfKind(vm, events.Retrieval, 5)
	if len(fused) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("fused scores  (lexical · candidates → recalled)", w), colSubtext).render())
		for i := range fused {
			e := &fused[i]
			right := fmt.Sprintf("lex %.2f · %d→%d", dataFloat(e, "lexical"),
				int(dataFloat(e, "candidates")), int(dataFloat(e, "recalled")))
			lines = append(lines, lineLR(txt("  ["+dataStr(e, "mode")+"]", colMuted).render(), txt(right, colSubtext).render(), w))
		}
	}

	// the memory.* stream the retriever serves: recall (cross-episode transfer), record, reflect (distil).
	var memLines []string
	for _, k := range []struct {
		kind, glyph string
		c           lipgloss.Color
	}{
		{events.MemoryRecall, "↺", colAccent},
		{events.MemoryRecord, "+", colOk},
		{events.MemoryReflect, "~", colSubtext},
	} {
		for _, e := range eventsOfKind(vm, k.kind, 2) {
			memLines = append(memLines, wrapEntry(txt("  "+k.glyph+" ", k.c).render(), 4, e.Summary, colFaint, w)...)
		}
	}
	if len(memLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("declarative memory stream (recall · record · reflect)", w), colSubtext).render())
		lines = append(lines, memLines...)
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderPersist — CROSS-SESSION PERSISTENCE + the lifecycle CURATOR (M4 §4.4/§4.5): the learned state
// that survives a restart. A pure view over persist.* — the load summary (what was restored at start),
// the recent saves (a learned artifact persisted beside its mint), and the curator's actions
// (version/dedup/decay/demote/gc/cap, off the hot path at IDLE consolidation). Empty when no Store is
// bound (the in-memory test/heuristic default never touches disk).
func renderPersist(vm ViewModel) Panel {
	w := contentW(vm)
	load := lastEvent(vm, events.PersistLoad)
	saves := eventsOfKind(vm, events.PersistSave, 5)
	curates := eventsOfKind(vm, events.PersistCurate, 6)
	if load == nil && len(saves) == 0 && len(curates) == 0 {
		var empty []string
		for _, ln := range wrapPlain("(no Store bound — learned state is in-memory only; run with "+
			"--state DIR to persist skills/operators/specialists/priors/beliefs/knowledge across restarts)", w, w) {
			empty = append(empty, faintStr(ln))
		}
		return Panel{Body: strings.Join(empty, "\n")}
	}
	var lines []string
	if load != nil {
		lines = append(lines, txt(clip("restored at start", w), colSubtext).render())
		lines = append(lines, wrapEntry(txt("  ↻ ", colOk).render(), 4, load.Summary, colSubtext, w)...)
		meta := fmt.Sprintf("skills %d · ops %d · spec %d · eps %d · beliefs %d · know %d",
			int(dataFloat(load, "skills")), int(dataFloat(load, "operators")), int(dataFloat(load, "specialists")),
			int(dataFloat(load, "episodes")), int(dataFloat(load, "beliefs")), int(dataFloat(load, "knowledge")))
		lines = append(lines, txt("    ↳ "+clip(meta, w-6), colFaint).render())
	}
	if len(saves) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("saved  (beside each mint, debounced)", w), colSubtext).render())
		for i := range saves {
			lines = append(lines, wrapEntry(txt("  + ", colOk).render(), 4, saves[i].Summary, colFaint, w)...)
		}
	}
	if len(curates) > 0 {
		lines = append(lines, "")
		lines = append(lines, txt(clip("curator  (version · dedup · decay · demote · gc · cap)", w), colSubtext).render())
		for i := range curates {
			e := &curates[i]
			action := dataStr(e, "action")
			c := colSubtext
			switch action {
			case "demote", "refute":
				c = colErr
			case "gc", "archive":
				c = colWarn
			}
			prefix := txt("  ["+action+"] ", c).render()
			lines = append(lines, wrapEntry(prefix, lipgloss.Width(ansi.Strip(prefix)), dataStr(e, "reason"), colFaint, w)...)
		}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}

// renderConfigEvents — the live CONFIG event stream (M1 §4.3): config.load (the run config that was
// loaded, with the OFF-toggle count), config.toggle (a live flip), and config.skip (a disabled
// component that BYPASSED its decision this tick — the proof that a toggle is a bypass, not a delete).
// A pure view over config.*. The Config TAB shows the toggle STATE; this panel shows the toggle EVENTS.
func renderConfigEvents(vm ViewModel) Panel {
	w := contentW(vm)
	var lines []string
	if ev := lastEvent(vm, events.ConfigLoad); ev != nil {
		lines = append(lines, wrapEntry(txt("loaded  ", colMuted).render(), 8, ev.Summary, colSubtext, w)...)
	}
	toggles := eventsOfKind(vm, events.ConfigToggle, 4)
	if len(toggles) > 0 {
		lines = append(lines, txt(clip("live flips", w), colSubtext).render())
		for i := range toggles {
			lines = append(lines, wrapEntry(txt("  ◆ ", colAccent).render(), 4, toggles[i].Summary, colSubtext, w)...)
		}
	}
	// the bypassed decisions THIS tick — a disabled component short-circuiting to pass-through (the wire
	// stays, the decision is skipped). Dedup by component so a chatty skip doesn't flood the panel.
	skips := eventsOfKind(vm, events.ConfigSkip, 16)
	if len(skips) > 0 {
		seen := map[string]bool{}
		var uniq []string
		for i := len(skips) - 1; i >= 0; i-- {
			comp := dataStr(&skips[i], "component")
			if comp == "" || seen[comp] {
				continue
			}
			seen[comp] = true
			uniq = append([]string{comp}, uniq...)
		}
		lines = append(lines, "")
		lines = append(lines, txt(clip("bypassed (disabled → pass-through)", w), colSubtext).render())
		for _, comp := range uniq {
			lines = append(lines, txt(clip("  ⊘ "+comp, w), colFaint).render())
		}
	}
	if len(lines) == 0 {
		return Panel{Body: faintStr(clip("(all-on — no config events; flip a toggle in the Config tab)", w))}
	}
	return Panel{Body: strings.Join(lines, "\n")}
}
