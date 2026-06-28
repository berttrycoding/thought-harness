package stability

// B3-outreach LIVE characterization harness (opt-in, claude-only). It exercises the PROACTIVE-OUTREACH
// faculty that B3 left uncharacterized: the offline TestBackend double cannot generate an emergent,
// topically-relevant own-thought, and the headless `run` CLI seeds at most one user turn (which also
// becomes the goal, self-excluded from outreach). So this harness:
//
//  1. builds a CONTINUOUS-mode engine on the REAL claude bridge (frontier substrate),
//  2. seeds a few staggered USER turns (percepts) on topics the engine's endogenous lines can become
//     relevant to — the first turn seeds the goal, the rest add user TOPICS that are NOT the goal,
//  3. steps a BOUNDED horizon (cost-capped), running the real maybeReachOut gate every awake tick,
//  4. records every proactive outreach (action.respond + proactive=true) with its tick/value/text and
//     cross-checks it for appropriateness (relevant to a seeded topic), emergence (not the "[role] intent"
//     placeholder, not verbatim user text), and cooldown discipline.
//
// This is a CHARACTERIZATION, not a plant change: maybeReachOut never calls regulator.Update (B3 red-team),
// so outreach is not a durability lever. The harness touches no engine code; it drives the public
// Engine API (Step / Submit / Bus). It is GATED behind THOUGHT_OUTREACH_LIVE=1 so the standing
// `go test ./...` suite never spawns claude. Invoke explicitly:
//
//	THOUGHT_OUTREACH_LIVE=1 go test ./internal/stability -run TestB3OutreachLive -v -timeout 30m
//
// SUBSTRATE: --backend claude (claude:sonnet primary + haiku utility). ANNOUNCED in the test log.

import (
	"os"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// outreachRun is one staged user transcript for a single awake run: a goal-seeding first turn plus a few
// later turns that add user TOPICS (not the goal) the endogenous lines can become relevant to. tickFor[i]
// is the awake tick at which turn i is injected as a salient USER_INPUT percept.
type outreachRun struct {
	label   string
	turns   []string
	tickFor []int
}

// outreachRuns are the K=3 varied staged transcripts. Each seeds a goal-bearing first turn and 2 later
// topic turns at ticks the endogenous wander/curiosity lines will have developed past (>cooldown=8). The
// topics are chosen to be concept-rich (content nouns survive the outreachStopword filter) and the kind
// of thing a self-directed mind genuinely forms its OWN conclusions about — so an emergent own-thought
// can legitimately become relevant.
func outreachRuns() []outreachRun {
	return []outreachRun{
		{
			label: "A:learning-and-memory",
			turns: []string{
				"I'm trying to understand how spaced repetition strengthens long-term memory.",
				"What makes some study habits stick while others fade after a week?",
				"I keep forgetting vocabulary even after reviewing it many times.",
			},
			tickFor: []int{1, 10, 18},
		},
		{
			label: "B:distributed-systems",
			turns: []string{
				"I'm designing a distributed cache and worrying about consistency under partition.",
				"How do teams decide between strong consistency and eventual consistency?",
				"My replicas keep diverging when a network partition heals.",
			},
			tickFor: []int{1, 10, 18},
		},
		{
			label: "C:climate-and-energy",
			turns: []string{
				"I'm reading about grid-scale battery storage for renewable energy.",
				"Why is seasonal energy storage so much harder than daily storage?",
				"My solar setup produces way more in summer than I can use or save.",
			},
			tickFor: []int{1, 10, 18},
		},
	}
}

// outreachEvent is one recorded proactive outreach: the tick it fired, the line value, and the shared text.
type outreachEvent struct {
	tick  int
	value float64
	text  string
}

// TestB3OutreachLive is the live (claude) proactive-outreach faculty characterization. Opt-in via
// THOUGHT_OUTREACH_LIVE=1. It runs K=3 short awake runs (varied staged transcripts), records every
// outreach, and reports per-run + aggregate behaviour. It asserts NOTHING (a characterization, not a
// gate) — the verdict is read from the logged report.
func TestB3OutreachLive(t *testing.T) {
	if os.Getenv("THOUGHT_OUTREACH_LIVE") != "1" {
		t.Skip("live claude outreach characterization is opt-in: set THOUGHT_OUTREACH_LIVE=1")
	}
	const horizon = 30 // BOUNDED, cost-capped (cooldown=8 → up to ~3 outreach windows in 30 ticks)

	t.Logf("SUBSTRATE: --backend claude (claude:sonnet primary + haiku utility)")
	t.Logf("B3-outreach LIVE characterization: full portfolio (forest+drive_agenda+seed_intents=%d), horizon=%d, K=%d",
		cognition.SeedPortfolioSize(), horizon, len(outreachRuns()))

	type runResult struct {
		label      string
		outreaches []outreachEvent
		realCount  int
		seed       int
		calls      int
		fallbacks  int
	}
	var results []runResult

	for k, run := range outreachRuns() {
		seed := 7 + k // vary the seeded RNG per run
		cfg := engine.DefaultConfig()
		cfg.Mode = "continuous"
		cfg.Seed = seed
		cfg.Features = featuresFor(b3Config{name: "full", forest: true, driveAgenda: true,
			seedIntents: true, seedCount: cognition.SeedPortfolioSize()})
		// the REAL claude bridge — frontier substrate, per-call `claude -p` spawn.
		backend := llm.NewClaudeCode(llm.ClaudeCodeOptions{})
		e, err := engine.NewEngine(&cfg, backend)
		if err != nil {
			t.Fatalf("run %s: NewEngine(claude) failed: %v", run.label, err)
		}

		var got []outreachEvent
		var fbEvents int
		e.Bus().Subscribe(func(ev events.Event) {
			if ev.Kind == events.LLMFallback {
				fbEvents++
				return
			}
			if ev.Kind != events.Respond {
				return
			}
			pro, _ := ev.Data["proactive"].(bool)
			if !pro {
				return
			}
			txt, _ := ev.Data["text"].(string)
			val, _ := ev.Data["value"].(float64)
			got = append(got, outreachEvent{tick: e.Graph().History()[len(e.Graph().History())-1].ID, value: val, text: txt})
			t.Logf("  [%s] OUTREACH fired: value=%.2f text=%q", run.label, val, txt)
		})

		// Schedule the staged user turns at their ticks, then step.
		turnAt := map[int]string{}
		for i, tk := range run.tickFor {
			if i < len(run.turns) {
				turnAt[tk] = run.turns[i]
			}
		}
		for tick := 1; tick <= horizon; tick++ {
			if txt, ok := turnAt[tick]; ok {
				e.Submit(txt, true) // salient USER_INPUT percept → next awake tick appends to transcript
				t.Logf("  [%s] tick %d: injected user turn: %q", run.label, tick, txt)
			}
			e.Step()
		}

		var real int
		for _, th := range e.Graph().History() {
			if th.Source != types.METACOG {
				real++
			}
		}
		results = append(results, runResult{label: run.label, outreaches: got, realCount: real, seed: seed})
		t.Logf("  [%s] (seed=%d) DONE: %d outreach(es), %d real thoughts, transcript turns=%d",
			run.label, seed, len(got), real, len(e.Transcript()))
	}

	// -- aggregate report (the characterization verdict inputs) --
	t.Logf("================ B3-OUTREACH CHARACTERIZATION REPORT ================")
	fired := 0
	totalOutreaches := 0
	for _, r := range results {
		n := len(r.outreaches)
		totalOutreaches += n
		if n > 0 {
			fired++
		}
		t.Logf("run %-24s seed=%d  outreaches=%d", r.label, r.seed, n)
		for _, o := range r.outreaches {
			canned := isCannedPlaceholder(o.text)
			t.Logf("    value=%.2f  canned=%v  text=%q", o.value, canned, o.text)
		}
	}
	t.Logf("FIRE-RATE: %d/%d runs fired at least one outreach; total outreaches=%d",
		fired, len(results), totalOutreaches)
}

// outreachStopwordDiag mirrors engine.outreachStopword (unexported): function words that do NOT count as
// topical relevance. Kept in sync by hand — the diagnostic replicates maybeReachOut's gate to pinpoint
// WHICH condition blocks; it is not the production path.
var outreachStopwordDiag = map[string]bool{
	"what": true, "that": true, "this": true, "with": true, "would": true, "could": true,
	"should": true, "about": true, "tell": true, "know": true, "think": true, "thing": true,
	"have": true, "from": true, "your": true, "where": true, "when": true, "will": true,
	"there": true, "here": true, "just": true, "like": true, "want": true, "need": true,
}

// TestB3OutreachDiag is the per-tick gate-DIAGNOSTIC: it replicates maybeReachOut's gate over the engine's
// PUBLIC state (Graph/Transcript) each tick and reports which condition is the binding constraint, so a 0×
// fire-rate can be attributed to a specific gate (no engine code change). Opt-in via THOUGHT_OUTREACH_DIAG=1,
// K=1, horizon long enough to clear cooldown after all injections. claude substrate.
func TestB3OutreachDiag(t *testing.T) {
	if os.Getenv("THOUGHT_OUTREACH_DIAG") != "1" {
		t.Skip("live claude outreach diagnostic is opt-in: set THOUGHT_OUTREACH_DIAG=1")
	}
	const horizon = 24
	const floor = 0.2 // engine default ProactivityFloor
	t.Logf("SUBSTRATE: --backend claude (claude:sonnet primary + haiku utility)")
	// SINGLE early user turn, then a long quiet wander: this is the scenario where UserWaiting can
	// CLEAR (the turn gets answered) and the topic can persist, so the endogenous line has a window to
	// develop relevance and clear the outreach gate. The staged multi-turn runs re-armed UserWaiting
	// continuously (the 0x cause); this isolates whether the faculty CAN fire at all.
	run := outreachRun{
		label:   "single-turn-then-wander",
		turns:   []string{"I'm trying to understand how spaced repetition strengthens long-term memory."},
		tickFor: []int{1},
	}
	cfg := engine.DefaultConfig()
	cfg.Mode = "continuous"
	cfg.Seed = 7
	cfg.Features = featuresFor(b3Config{name: "full", forest: true, driveAgenda: true,
		seedIntents: true, seedCount: cognition.SeedPortfolioSize()})
	e, err := engine.NewEngine(&cfg, llm.NewClaudeCode(llm.ClaudeCodeOptions{}))
	if err != nil {
		t.Fatalf("NewEngine(claude) failed: %v", err)
	}
	var outreaches, replies int
	e.Bus().Subscribe(func(ev events.Event) {
		if ev.Kind == events.Respond {
			if pro, _ := ev.Data["proactive"].(bool); pro {
				outreaches++
				t.Logf("  >>> OUTREACH fired: %q", ev.Data["text"])
			} else {
				replies++
				t.Logf("  ... reactive reply (answers the user, not outreach): %q", ev.Summary)
			}
		}
	})
	turnAt := map[int]string{}
	for i, tk := range run.tickFor {
		if i < len(run.turns) {
			turnAt[tk] = run.turns[i]
		}
	}
	t.Logf("tick | userWait | userTopics | real | activeVal | ownThoughts | binding-constraint")
	for tick := 1; tick <= horizon; tick++ {
		if txt, ok := turnAt[tick]; ok {
			e.Submit(txt, true)
		}
		e.Step()
		// replicate the gate over public state.
		g := e.Graph()
		userTopics := map[string]struct{}{}
		userSaid := map[string]struct{}{}
		for _, turn := range e.Transcript() {
			if turn[0] != "user" {
				continue
			}
			userSaid[strings.TrimSpace(turn[1])] = struct{}{}
			for _, w := range strings.Fields(strings.ToLower(turn[1])) {
				if len([]rune(w)) > 3 && !outreachStopwordDiag[w] {
					userTopics[w] = struct{}{}
				}
			}
		}
		var real []types.Thought
		for _, th := range g.ActiveContext() {
			if th.Source != types.METACOG {
				real = append(real, th)
			}
		}
		goalTxt := strings.TrimSpace(g.Goal)
		activeVal := g.Active().Value
		ownRelevant := 0
		for _, th := range real {
			txt := strings.TrimSpace(th.Text)
			if txt == "" || strings.HasSuffix(txt, "?") || txt == goalTxt {
				continue
			}
			if _, said := userSaid[txt]; said {
				continue
			}
			relevant := false
			for _, w := range strings.Fields(strings.ToLower(txt)) {
				if len([]rune(w)) > 3 {
					if _, ok := userTopics[w]; ok {
						relevant = true
						break
					}
				}
			}
			if !relevant {
				continue
			}
			if th.Source == types.GENERATED || th.Source == types.OBSERVATION || th.Source == types.INJECTED {
				ownRelevant++
			}
		}
		// binding constraint (priority order of the gate — UserWaiting is checked FIRST in maybeReachOut).
		userWait := e.UserWaiting()
		bind := "WOULD-FIRE"
		switch {
		case userWait:
			bind = "user-waiting(defers)"
		case len(userTopics) == 0:
			bind = "no-user-topics"
		case len(real) < 3:
			bind = "line-not-developed(<3)"
		case activeVal < floor:
			bind = "value<floor"
		case ownRelevant == 0:
			bind = "no-own-relevant-thought"
		}
		t.Logf("%4d | %8v | %10d | %4d | %.3f     | %d           | %s  [transcript=%d turns]",
			tick, userWait, len(userTopics), len(real), activeVal, ownRelevant, bind, len(e.Transcript()))
	}
	// dump the final transcript so the topic-retention behaviour is visible.
	t.Logf("--- final transcript (%d turns) ---", len(e.Transcript()))
	for i, turn := range e.Transcript() {
		t.Logf("  [%d] %s: %.80s", i, turn[0], turn[1])
	}
	t.Logf("DIAG DONE: %d outreach(es), %d reactive replies over %d ticks", outreaches, replies, horizon)
}

// isCannedPlaceholder reports whether an outreach text is the "[role] intent" placeholder (the
// reference-firereason-placeholder marker) or otherwise an obviously non-emergent canned string. A model-
// generated emergent outreach is none of these.
func isCannedPlaceholder(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	// the SubAgent.fireReason "[role] intent" fallback shape: "[something] intent".
	if strings.HasPrefix(t, "[") && strings.Contains(t, "] intent") {
		return true
	}
	// the TestBackend canned scaffolds (would only appear if mistakenly on the double).
	for _, c := range []string{"No specialist fired", "Effortful step: if", "Grinding through it: what does",
		"Working it out from first principles", types.RecapPrefix} {
		if strings.Contains(t, c) {
			return true
		}
	}
	return false
}
