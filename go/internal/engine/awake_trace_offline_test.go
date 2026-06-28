package engine_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestOfflineAwakeMultiHopFate is the DETERMINISTIC offline twin of TestLiveClaudeAwakeMultiHopTrace.
// It drives the AWAKE (continuous) engine on the test double with three multi-hop inputs arriving over
// the stream and prints, per input, whether the SUBCONSCIOUS engaged (subconscious.dispatch/fire) or
// stayed quiet, and whether a deliver/respond happened. The test double fires specialists FOR REAL
// (only their content is canned) — so this faithfully answers the wiring question "does an awake user
// input reach the subconscious pull-dispatch?" without a live run. Diagnostic, not a pass/fail gate.
func TestOfflineAwakeMultiHopFate(t *testing.T) {
	eng, log := newSeededEngine(t, "continuous", 7)

	maxTick := func() int {
		m := 0
		for _, e := range log.events {
			if e.Tick > m {
				m = e.Tick
			}
		}
		return m
	}
	submits := map[int]string{}
	step := func(n int) {
		for i := 0; i < n; i++ {
			eng.Step()
		}
	}

	step(3) // already awake + wandering
	submits[maxTick()+1] = "INPUT1 rate-limiter per-tenant+global"
	eng.SubmitDefault("design a rate limiter that supports BOTH per-tenant and a global cap, and explain how the two interact when a tenant's burst would push the system past the global cap")
	step(5)
	submits[maxTick()+1] = "INPUT2 hot-reload lower cap mid-traffic"
	eng.SubmitDefault("now, given that design, what happens to in-flight requests when the global cap is hot-reloaded to a LOWER value mid-traffic?")
	step(5)
	submits[maxTick()+1] = "INPUT3 refill vs burst-drain race"
	eng.SubmitDefault("separately: trace how a token refill and a burst-drain race if they fire on the same tick, and which wins")
	step(8)

	if len(log.events) == 0 {
		t.Fatal("no events captured — the awake stream produced nothing")
	}

	// 1. kind-count table (the whole stream).
	counts := map[string]int{}
	for _, e := range log.events {
		counts[string(e.Kind)]++
	}
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	var tbl strings.Builder
	for _, k := range kinds {
		tbl.WriteString(k)
		tbl.WriteString("=")
		tbl.WriteString(itoa(counts[k]))
		tbl.WriteString("  ")
	}
	t.Logf("KIND COUNTS: %s", tbl.String())

	// 2. headline: subconscious engagement vs quiet, and any deliver/respond.
	t.Logf("subconscious.dispatch=%d  subconscious.fire=%d  subconscious.quiet=%d",
		counts[string(events.SubDispatch)], counts[string(events.SubFire)], counts[string(events.SubQuiet)])

	// 3. per-tick timeline of the load-bearing kinds, with the input-submit markers inline, so the
	//    input's fate is readable: after an INPUT, do we see subconscious.dispatch/fire + a deliver?
	relevant := func(k string) bool {
		for _, p := range []string{"subconscious.", "perception.", "seam.", "action.", "lifecycle.", "conscious."} {
			if strings.HasPrefix(k, p) {
				return true
			}
		}
		for _, s := range []string{"deliver", "respond", "interrupt", "outreach", "answer", "user"} {
			if strings.Contains(strings.ToLower(k), s) {
				return true
			}
		}
		return false
	}
	t.Log("--- TIMELINE (tick | kind | summary) ---")
	lastTick := -1
	for _, e := range log.events {
		if m, ok := submits[e.Tick]; ok && e.Tick != lastTick {
			t.Logf(">>> tick %d  <<< SUBMIT %s", e.Tick, m)
		}
		lastTick = e.Tick
		if relevant(string(e.Kind)) {
			sum := e.Summary
			if len(sum) > 70 {
				sum = sum[:70]
			}
			t.Logf("t%-3d %-26s %s", e.Tick, e.Kind, sum)
		}
	}
}

// TestOfflineReactiveInputFate is the disambiguation control: the SAME open-ended engineering input,
// driven in REACTIVE mode on the test double. If the subconscious also stays quiet here, the gap is
// SPECIALIST-ROSTER COVERAGE (the roster matches nothing on real engineering content) and is NOT
// awake-specific; if it fires here but not awake, the gap is awake-loop wiring. Deterministic.
func TestOfflineReactiveInputFate(t *testing.T) {
	eng, log := newSeededEngine(t, "reactive", 7)
	eng.SubmitDefault("design a rate limiter that supports BOTH per-tenant and a global cap, and explain how the two interact when a tenant's burst would push the system past the global cap")
	for i := 0; i < 25; i++ {
		eng.Step()
	}
	counts := map[string]int{}
	for _, e := range log.events {
		counts[string(e.Kind)]++
	}
	t.Logf("REACTIVE same input: subconscious.dispatch=%d  fire=%d  quiet=%d  respond=%d",
		counts[string(events.SubDispatch)], counts[string(events.SubFire)],
		counts[string(events.SubQuiet)], counts["action.respond"])
}
