package campaign

// skillcurve.go — A3: drive the Skill-Miner over a task STREAM and measure the SELF-IMPROVEMENT CURVE.
//
// WHAT A3 IS (vs W5-2b/2c, which it builds on). W5-2b/2c is a TWO-POINT comparison: a COLD arm (no skill,
// Synthesize runs the toolmaker) vs a WARM arm whose skill was PRE-SEEDED (SeedRecurringSkill) — it proves
// the recall PATH short-circuits synthesis but it does not exercise the autonomous flywheel (the skill was
// planted, not minted). A3 measures the CURVE: drive a STREAM of exposures of a recurring goal family
// through fresh engines that SHARE ONE state dir with persistence + SkillMint ON, so the per-goal program-run
// counter ACCUMULATES across exposures (1->2->3, the W5-2b persist fix 25e3ea8), the skill MINTS itself at
// the MintAfter threshold during the idle Consolidate, persists, and is RECALLED on later exposures. The
// y-axis is per-exposure cost + faculty fire; the curve should BEND DOWN at the mint point. This is the
// genuine self-improvement curve (effortful synthesis -> automatic recall) — autonomous, not planted.
//
// THE TWO AXES (the brief — read project-cognition-scaling-faculties-not-answers + the W5 plan):
//   - EFFICIENCY (PRIMARY, cost-reliable): completion-tokens per exposure FALLING as the mint fires and
//     recall short-circuits SynthesizeProgram. Gated with the W5-1 ruler (COST axis). The W5 definition-
//     of-done. (Prior point W5-2c: the recall is ~20% cheaper at grounded utility but BELOW the cost floor
//     at n=5 — the curve needs adequate N to clear the floor.)
//   - CAPABILITY (caveated, saturated): per-faculty fire-rate via the cognition probe. The ruler reads the
//     binary axis as knife-edge on the saturated suite — report DIRECTIONAL, never a clean strict-`>` lift.
//
// HONEST SCOPE / WHY THE OFFLINE DOUBLE PROVES WIRING NOT MAGNITUDE. The test double emits no real usage
// (completion=0), so OFFLINE this proves the MINT-AND-RECALL WIRING fires at the right exposure (the curve's
// SHAPE — a mint event, then recall short-circuits, observed in convert.*/synth Source) — the token MAGNITUDE
// (does the curve bend DOWN, and by how much) is the claude follow-up (skillcurve_claude_test.go). The
// apparatus is measurement-only: it drives the SAME NewEngine the caller injects (test double offline, claude
// live); it does not tune the miner (no plant change), so no durability re-pass is needed.

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// CurvePoint is one EXPOSURE's measurement: the exposure index (0-based position in the stream), the goal
// run at that exposure, and the per-exposure cost + faculty signals. A "self-improvement" curve is a
// sequence of these whose Completion falls (efficiency axis) and/or whose Fired rises (faculty axis) once
// the mint fires and recall begins short-circuiting synthesis.
type CurvePoint struct {
	// Exposure is the 0-based position of this goal in the stream (exposure 0 is the first time the family
	// is seen — never recalled; the mint, if it fires, fires at/after MintAfter exposures).
	Exposure int
	// Goal is the goal run at this exposure (distinct surface goals that reduce to the SAME goal key — the
	// recurrence regime; convert.goalKey collapses them so the minted skill's triggers fire on later ones).
	Goal string
	// Completion is the completion (output) tokens this exposure decoded — the CACHE-IMMUNE efficiency
	// signal (the W5 y-axis). 0 on the offline test double (no real usage); the real magnitude is the
	// claude follow-up. A skill recall short-circuits SynthesizeProgram so a recalled exposure decodes fewer.
	Completion int
	// Calls is the number of CONTENT (llm.call) calls this exposure made — a coarse structural cost proxy
	// that is non-zero even on the offline double (the double counts calls, just reports completion=0). A
	// recall exposure makes FEWER synthesis calls, so Calls drops even offline — that is the offline curve.
	Calls int
	// Recalled is true if this exposure RECALLED a minted skill (synth step-0 library.Match returned a
	// Source "skill:…") instead of synthesising — the mechanism that makes the cost fall. Observed from the
	// event stream, never asserted: it is the autonomous-flywheel signal (the skill minted on an EARLIER
	// exposure and is now firing).
	Recalled bool
	// Minted is true if the idle Consolidate MINTED a skill at this exposure (convert.skill_mint / the
	// MintedSkill record grew) — the inflection point the curve bends at. Typically fires at exposure
	// == MintAfter-1 .. MintAfter (the count crosses the threshold).
	Minted bool
	// Grounded is true if this exposure imported a reality observation (held-positive-utility check — the
	// efficiency claim is only meaningful at held utility; on a non-grounding bank this stays false and the
	// cost number carries the W5-2b zero-grounding caveat).
	Grounded bool
	// Solved is the answer-oracle verdict (Expect substring in the answer/any thought) when the goal carries
	// one; for an empty-oracle goal it falls back to Grounded. The capability/utility axis per exposure.
	Solved bool
	// Signature is the cognition faculty this exposure was meant to elicit ("" = the faculty axis was not
	// scored for this exposure). Fired is whether that faculty's signature was observed in the run (the
	// CAPABILITY axis — caveated/saturated; a structured recalled skill can keep a faculty firing without
	// re-paying synthesis). Fired is only meaningful when Signature != "".
	Signature string
	Fired     bool
}

// CurveTask is one stream exposure spec: the goal to run and (optionally) an answer oracle + the faculty the
// goal is meant to elicit (for the capability axis). All exposures of a family share the goal key so the
// minted skill recalls across them. A nil Signature means the faculty axis is not scored for that exposure.
type CurveTask struct {
	Goal      string
	Expect    string // answer oracle (Expect substring); "" = grounded-success fallback
	Signature string // the cognition faculty the goal elicits (branch/act/…); "" = not faculty-scored
}

// SkillCurve drives a STREAM of exposures of a recurring goal family through the SAME state dir and returns
// the per-exposure curve. The stateDir MUST persist (a real persist.JSONLStore wired into the injected
// engine via NewEngine, with SkillMint + Persist ON — config.New() defaults) so the recurrence counter
// ACCUMULATES across exposures and the mint fires autonomously; an in-memory (nil store) engine resets the
// counter every exposure and the curve never bends (the mutation-test failure mode — verified in the test).
//
// Each exposure: build a FRESH engine seeded from stateDir (it reloads the accumulated counter + any minted
// skill), run the goal, record cost + faculty + recall/mint flags from the event stream. The engine's idle
// Consolidate (reactive loop, IDLE) mints + persists at MintAfter; the NEXT fresh engine reloads it and the
// synth step-0 recall fires. Returns the points in stream order (exposure 0..N-1).
//
// This is measurement-only: it runs whatever NewEngine builds (test double offline, claude live) and never
// mutates the miner — no plant change, no durability re-pass. Reuses the addLLMCost cost wiring (the single
// shared source) so the cost accounting is identical to Probe/Bench/ProbeReplays.
func (b EngineBencher) SkillCurve(stream []CurveTask, stateDir string) ([]CurvePoint, error) {
	return b.SkillCurveStreamed(stream, stateDir, nil)
}

// SkillCurveStreamed is SkillCurve with a per-exposure callback `onPoint`, invoked with each CurvePoint THE
// MOMENT that exposure completes (before the next exposure starts). It is the DURABILITY hook the metered
// claude pilot uses: persist (fsync) each row as it lands, so a mid-stream death (the ~60-70min socket-close
// that kills claude measurement agents) loses at MOST the in-flight exposure, never the whole curve. onPoint
// may be nil (then this is exactly SkillCurve). The curve is inherently serial — each exposure must persist
// its learned state before the next reloads the accumulated counter — so streaming the points adds no
// concurrency hazard.
func (b EngineBencher) SkillCurveStreamed(stream []CurveTask, stateDir string, onPoint func(CurvePoint)) ([]CurvePoint, error) {
	maxTicks := b.MaxTicks
	if maxTicks <= 0 {
		maxTicks = 40
	}
	out := make([]CurvePoint, len(stream))
	for i, t := range stream {
		eng, err := b.NewEngine(stateDir)
		if err != nil {
			return nil, err
		}
		var calls, tokens, completion int
		recalled := false
		var fac facultySignals
		eng.Bus().Subscribe(func(ev events.Event) {
			// engine is serial within one run → this closure is single-threaded per exposure.
			addLLMCost(ev, &calls, &tokens, &completion)
			if isSkillRecall(ev) {
				recalled = true
			}
			fac.observe(ev) // capability axis: collect the calibrated faculty signatures (same as cogprobe)
		})
		groundBefore := eng.Grounding().Len()
		eng.SubmitDefault(t.Goal)
		eng.Run(maxTicks)
		grounded := eng.Grounding().Len() > groundBefore
		solved := scoreSolvedEngine(HeldOutTask{Goal: t.Goal, Expect: t.Expect}, eng, grounded)
		// faculty fire for the elicited signature (only scored when the task names one) — the same calibrated
		// post-run signals scoreCognition reads (graph fork, depth, honest-decline), so the curve's capability
		// axis matches the cognition probe exactly.
		fired := false
		if t.Signature != "" {
			fired = fac.fired(t.Signature, eng, grounded)
		}

		// minted-this-exposure: the cumulative MintedSkill count GREW vs the previous exposure (the idle
		// Consolidate paved a new skill this run). Read post-run from the engine's convertibility (the
		// autonomous mint record, eng.Convert().MintedSkill — an existing public accessor, no engine edit),
		// not asserted — the inflection the curve bends at. NOTE: MintedSkill is the names minted IN THIS
		// engine's lifetime; because a fresh engine is built per exposure, the count is per-exposure, so a
		// non-empty MintedSkill on exposure i means the mint fired on exposure i (the genuine inflection).
		minted := len(eng.Convert().MintedSkill) > 0

		out[i] = CurvePoint{
			Exposure: i, Goal: t.Goal, Completion: completion, Calls: calls,
			Recalled: recalled, Minted: minted, Grounded: grounded, Solved: solved,
			Signature: t.Signature, Fired: fired,
		}
		if onPoint != nil {
			onPoint(out[i]) // durable per-exposure hook: persist this row NOW (the death-resilience guarantee)
		}
	}
	return out, nil
}

// facultySignals collects the calibrated cognition-faculty signatures from the event stream during one
// exposure — the SAME signals scoreCognition (cogprobe.go) watches, mirrored here so the curve's capability
// axis matches the cognition probe without coupling SkillCurve to the cognition-probe internals. Calibrated
// (Phase-2): branch = an EXPLICIT graph fork (a BRANCH decision / mcp branch / >1 branch), NOT inline
// reasoning; conflict = a REAL held conflict (the gate's conflict flag); deliberate = graph depth ≥5.
type facultySignals struct {
	branched, acted, conflicted, decomposed, filtered bool
}

// observe folds one event into the faculty flags (call from the bus subscription).
func (f *facultySignals) observe(ev events.Event) {
	switch ev.Kind {
	case string(events.Decision):
		switch strings.ToUpper(asStr(ev.Data["decision"])) {
		case "BRANCH":
			f.branched = true
		case "ACT":
			f.acted = true
		}
	case string(events.MCP):
		if op := asStr(ev.Data["op"]); op == "branch" || op == "reenter" {
			f.branched = true
		}
	case string(events.Act), string(events.Observation), string(events.Ground):
		f.acted = true
	case string(events.Gate):
		if c, ok := ev.Data["conflict"].(bool); ok && c {
			f.conflicted = true
		}
	case string(events.SubWorkflow), string(events.SubSynthesize), string(events.SubOperator):
		f.decomposed = true
	case string(events.Filter):
		if v := strings.ToUpper(asStr(ev.Data["verdict"])); v == "REJECT" || v == "FLAG" {
			f.filtered = true
		}
	}
}

// fired reports whether the named faculty signature fired this exposure, folding in the post-run graph-shape
// signals (an explicit fork, reasoning depth) + the honest-decline read — identical to scoreCognition's
// final obs map. grounded is the episode's reality-import flag (the act faculty's strongest signal).
func (f *facultySignals) fired(signature string, eng *engine.Engine, grounded bool) bool {
	branched, acted, deep := f.branched, f.acted || grounded, false
	if g := eng.Graph(); g != nil {
		if len(g.Branches) > 1 {
			branched = true
		}
		if len(g.History()) >= 5 {
			deep = true
		}
	}
	low := strings.ToLower(eng.LastResponse())
	honest := f.filtered || strings.TrimSpace(eng.LastResponse()) == ""
	for _, m := range honestUnknownMarkers {
		if strings.Contains(low, m) {
			honest = true
		}
	}
	obs := map[string]bool{
		"branch": branched, "act": acted, "conflict": f.conflicted,
		"decompose": f.decomposed, "honest": honest, "deliberate": deep,
	}
	return obs[signature]
}

// isSkillRecall reports whether an event signals that the synthesiser RECALLED a minted skill at step-0
// (library.Match short-circuited SynthesizeProgram) rather than synthesising. The synth path emits the
// recall on the subconscious.synthesize stream carrying a "skill:"-prefixed source (cognition/synth.go
// step-0 library.Match → Program.Source). Read defensively from the event Data so a vocabulary change
// surfaces as no-recall (a false-negative the curve test catches), never a crash.
func isSkillRecall(ev events.Event) bool {
	if ev.Kind != string(events.SubSynthesize) {
		return false
	}
	src := asStr(ev.Data["source"])
	if src == "" {
		src = asStr(ev.Data["program_source"])
	}
	return len(src) >= 6 && src[:6] == "skill:"
}

// CurveStream builds a recurrence STREAM of `exposures` copies of a recurring goal family: distinct surface
// goals (so it is not the literally-identical string each time) that all reduce to the SAME goal key (the
// recurrence regime convert.goalKey collapses), interleaving the supplied family goals. Every goal carries
// the family's oracle + signature. This is the input to SkillCurve: the same family seen `exposures` times
// so the counter accumulates 1..exposures and the mint fires at MintAfter.
//
// The family goals MUST share the goal key (first three alpha content words) — use a family whose surface
// goals differ only past the keyed prefix (e.g. the GroundedEfficiencyBank symbols, which all key to
// "investigate source files"). If fewer family goals than exposures, the family is cycled.
func CurveStream(family []CurveTask, exposures int) []CurveTask {
	if len(family) == 0 || exposures < 1 {
		return nil
	}
	out := make([]CurveTask, 0, exposures)
	for i := 0; i < exposures; i++ {
		out = append(out, family[i%len(family)])
	}
	return out
}
