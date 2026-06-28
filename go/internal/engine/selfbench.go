package engine

import (
	"strings"

	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/eval"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

// selfbench.go is the SB0 primitive of the self-benchmark loop (Track H — benchmark-taxonomy §7): the
// single new engine capability that lets the harness OWN its own fitness function. A self-improving
// harness must be able to measure itself — but the naive reading ("benchmark the live self") is
// self-contradicting: the live engine is a MUTATING substrate (registry/memory/grounding all change
// mid-episode), so measuring yourself WHILE you run contaminates the measurement (§7.2). The resolution
// is to benchmark a frozen CHECKPOINT in a SHADOW engine, never the live one:
//
//	checkpoint = persist.Snapshot of the just-consolidated learned state (frozen, immutable)
//	      │
//	      ▼
//	SelfBench(checkpoint, suite)   ← a shadow-engine POOL (one fresh engine per probe) loaded FROM a
//	      │                          FrozenStore over that snapshot, NEVER the live engine → zero
//	      ▼                          contamination, deterministic per cell (the bench/runner pattern)
//	a structured Report (passed/total/score) emitted on the LIVE bus as bench.*
//
// The DECIDED default (§7.5) is PROPOSE-AND-GATE: SelfBench MEASURES + proposes; it does NOT promote or
// revert a checkpoint off its own measurement. Closed-loop autonomy (self-commit on a clean win) is a
// later, separately-gated slice (SB2) that also requires the resource-safety interlock (SB-R). This
// primitive is pure measurement + observability.

// SelfProbe is one item of a self-bench SUITE: a prompt fed to the shadow engine and the expected
// answer the deterministic oracle checks the response against. Expect is matched case-insensitively as
// a SUBSTRING of the de-voiced response (a graded, model-free oracle — the same shape the conformance/
// mechanism benches use). A probe with an empty Expect is a "runs without error" conformance check (any
// non-empty response passes).
type SelfProbe struct {
	// Name identifies the probe in the bench.cell event + the report row.
	Name string
	// Prompt is the goal submitted to the shadow engine for one bounded reactive episode.
	Prompt string
	// Expect is the canonical answer the oracle looks for (case-insensitive substring). "" ⇒ a
	// conformance check (any non-empty answer passes; the engine ran + delivered).
	Expect string
}

// SelfSuite is a named set of probes — the fitness function the harness runs against a checkpoint.
type SelfSuite struct {
	// Name identifies the suite (e.g. "conformance") in the bench.* events + the report.
	Name string
	// Probes is the ordered set of items the shadow engine answers.
	Probes []SelfProbe
	// MaxTicks bounds each probe's reactive episode (0 ⇒ a conservative default). A self-bench runs
	// real episodes; the bound keeps a runaway probe from spending the whole consolidation window.
	MaxTicks int
}

// SeedSelfBenchSuite returns the seed conformance suite (L0/L1 flavour, benchmark-taxonomy §1): a small,
// deterministic set of probes that exercise the loop end-to-end and check it delivers a grounded answer.
// On the test double these are model-free scorable (the double answers arithmetic/greetings from its
// canned competence); on a real substrate the SAME suite is a capability probe. Deterministic + bounded
// so a self-bench is cheap and repeatable.
func SeedSelfBenchSuite() SelfSuite {
	return SelfSuite{
		Name:     "conformance",
		MaxTicks: selfBenchDefaultMaxTicks,
		Probes: []SelfProbe{
			// arithmetic: a specialist computes it; the answer is deterministic on the double and a
			// trivial capability check on a real substrate.
			{Name: "arith-sum", Prompt: "What is 2 + 3?", Expect: "5"},
			{Name: "arith-product", Prompt: "What is 6 times 7?", Expect: "42"},
			// conformance: the loop runs + delivers SOME grounded answer (Expect "" ⇒ any non-empty
			// response passes). Catches a checkpoint that no longer reaches the deliver guarantee.
			{Name: "responds", Prompt: "Hello there.", Expect: ""},
		},
	}
}

// selfBenchDefaultMaxTicks bounds a probe's reactive episode. Smaller than the campaign default (60) —
// a conformance probe answers fast, and a self-bench at consolidation must stay bounded.
const selfBenchDefaultMaxTicks = 24

// SelfCell is one scored probe: the shadow engine's answer + the oracle verdict.
type SelfCell struct {
	Probe  string  // the probe name
	Pass   bool    // did the answer clear the oracle?
	Value  float64 // graded score in [0,1]
	Answer string  // the shadow engine's de-voiced answer (truncated for the row)
	Reason string  // the oracle's rationale
}

// SelfReport is the structured result of one SelfBench run over a checkpoint (the return value of the
// primitive + the thing the propose-and-gate disposition reads).
type SelfReport struct {
	Checkpoint  string     // the checkpoint name benchmarked
	Suite       string     // the suite name
	Passed      int        // probes that cleared the oracle
	Total       int        // probes run
	Score       float64    // Passed/Total in [0,1] (0 when Total==0)
	Cells       []SelfCell // per-probe outcomes
	Disposition string     // "propose" (default) — measured, NOT self-committed (§7.5)
	Committed   bool       // always false here (closed-loop self-commit is the later SB2 slice)
}

// SelfBench runs `suite` against a SHADOW engine loaded from the FROZEN checkpoint `snap` (named
// `checkpoint`), and returns a structured SelfReport. It NEVER touches the live engine's mutable state:
// each probe runs on a FRESH shadow engine over a read-only persist.FrozenStore, so the run is
// zero-contamination + deterministic per cell (the bench/runner fresh-engine-per-cell pattern). Every
// step is emitted on the LIVE bus as bench.* so the loop is visible in the TUI + headless trace.
//
// It is propose-and-gate ONLY (§7.5): it scores the checkpoint and returns the report; it does NOT
// promote or revert anything. The caller (maybeSelfBench) decides the disposition.
func (e *Engine) SelfBench(snap persist.Snapshot, checkpoint string, suite SelfSuite) SelfReport {
	maxTicks := suite.MaxTicks
	if maxTicks <= 0 {
		maxTicks = selfBenchDefaultMaxTicks
	}
	rep := SelfReport{
		Checkpoint:  checkpoint,
		Suite:       suite.Name,
		Total:       len(suite.Probes),
		Disposition: "propose",
		Committed:   false,
	}
	e.bus.Emit(events.BenchStart, "self-bench: "+suite.Name+" on "+checkpoint, events.D{
		"checkpoint": checkpoint,
		"suite":      suite.Name,
		"probes":     len(suite.Probes),
		"shadow":     true,
	})

	// The oracle: a graded MeasuringStick reused for every probe (the eval REFERENCE — the existing
	// generic primitive, not a new scorer). It scores the shadow answer against the probe's Expect.
	for _, probe := range suite.Probes {
		answer := e.runShadowProbe(snap, maxTicks, probe)
		stick := selfProbeStick(probe)
		score := stick.Check(answer)
		cell := SelfCell{
			Probe:  probe.Name,
			Pass:   score.Pass,
			Value:  score.Value,
			Answer: clipRune(strings.TrimSpace(answer), 80),
			Reason: score.Reason,
		}
		rep.Cells = append(rep.Cells, cell)
		if cell.Pass {
			rep.Passed++
		}
		e.bus.Emit(events.BenchCell, "self-bench cell: "+probe.Name+" "+passWord(cell.Pass), events.D{
			"probe":  probe.Name,
			"pass":   cell.Pass,
			"value":  cell.Value,
			"answer": cell.Answer,
			"reason": cell.Reason,
		})
	}

	if rep.Total > 0 {
		rep.Score = float64(rep.Passed) / float64(rep.Total)
	}
	verdict := "fail"
	if rep.Total > 0 && rep.Passed == rep.Total {
		verdict = "pass"
	} else if rep.Passed > 0 {
		verdict = "partial"
	}
	e.bus.Emit(events.BenchVerdict, "self-bench verdict: "+verdict, events.D{
		"checkpoint": checkpoint,
		"suite":      suite.Name,
		"passed":     rep.Passed,
		"total":      rep.Total,
		"score":      rep.Score,
		"verdict":    verdict,
	})
	return rep
}

// runShadowProbe constructs a FRESH shadow engine over the frozen snapshot and runs ONE bounded reactive
// episode for the probe, returning the de-voiced answer. The shadow engine:
//   - reuses the LIVE backend (the same substrate the checkpoint was minted on — so the bench measures
//     the checkpoint's behaviour on its own substrate, not a different one);
//   - reads the FROZEN snapshot through a read-only persist.FrozenStore (so it re-seeds the checkpoint's
//     learned registries but can NEVER write back — zero contamination);
//   - inherits the live feature config but with selfbench DISABLED (so a shadow engine never recurses
//     into another self-bench);
//   - has its OWN bus (so the shadow run's internal events do not flood the live trace — only the
//     SelfBench bench.* roll-up is emitted on the live bus).
//
// On a shadow-construction failure the probe answer is "" (the oracle then fails the cell — surfaced,
// never silently passed).
func (e *Engine) runShadowProbe(snap persist.Snapshot, maxTicks int, probe SelfProbe) string {
	shadowFeatures := e.shadowFeatures()
	cfg := &EngineConfig{
		Mode:      "reactive",
		Seed:      e.cfg.Seed,
		MaxTicks:  maxTicks,
		Cognition: e.cfg.Cognition,
		Features:  shadowFeatures,
		Store:     persist.NewFrozenStore(snap),
	}
	shadow, err := NewEngine(cfg, e.backend)
	if err != nil {
		return ""
	}
	shadow.SubmitDefault(probe.Prompt)
	shadow.Run(maxTicks)
	return shadow.LastResponse()
}

// shadowFeatures clones the live feature config but forces selfbench OFF — a shadow engine must never
// recurse into its own self-bench (an unbounded loop). Persistence stays whatever the live config is,
// but the shadow's Store is a read-only FrozenStore so a Flush/Save is a no-op regardless.
func (e *Engine) shadowFeatures() *config.HarnessConfig {
	base := config.New() // AllOn baseline
	if e.features != nil {
		clone := *e.features // value copy of the live config
		base = &clone
	}
	base.SelfBench.Enabled = false
	return base
}

// selfProbeStick builds the eval MeasuringStick (the REFERENCE — the existing generic eval primitive,
// not a new scorer) for one probe: a graded substring oracle. An empty Expect is a conformance check
// (any non-empty answer passes). The de-voiced answer is matched case-insensitively.
func selfProbeStick(probe SelfProbe) eval.MeasuringStick {
	expect := strings.ToLower(strings.TrimSpace(probe.Expect))
	return eval.MeasuringStick{
		Name:  "self-probe:" + probe.Name,
		Facet: "selfbench",
		Check: func(subject any) eval.Score {
			ans, _ := subject.(string)
			low := strings.ToLower(strings.TrimSpace(ans))
			if expect == "" {
				// conformance: a non-empty answer means the loop ran + delivered.
				if low != "" {
					return eval.Score{Pass: true, Value: 1, Reason: "delivered a non-empty answer"}
				}
				return eval.Score{Pass: false, Value: 0, Reason: "no answer delivered"}
			}
			if strings.Contains(low, expect) {
				return eval.Score{Pass: true, Value: 1, Reason: "answer contains expected " + probe.Expect}
			}
			return eval.Score{Pass: false, Value: 0, Reason: "answer missing expected " + probe.Expect}
		},
	}
}

// maybeSelfBench is the LIVE-LOOP wire of the self-bench loop: at IDLE consolidation (after the learned
// state is persisted/curated), when selfbench.enabled is ON, the engine captures a frozen CHECKPOINT of
// its just-consolidated learned state and self-benchmarks it on a shadow engine. The DECIDED default is
// PROPOSE-AND-GATE (§7.5): the engine MEASURES + proposes; it does NOT self-commit (no promote/revert
// off its own measurement — that is the later SB2 slice). It emits bench.report with the proposed
// disposition so the loop is auditable.
//
// Default OFF (selfbench.enabled is an opt-in knob, default false) ⇒ this is a no-op: no shadow engine
// is spun up, no bench.* event fires ⇒ byte-identical to the pre-SelfBench engine. Idempotent per
// consolidation: a guard prevents re-benchmarking an unchanged checkpoint every idle tick.
func (e *Engine) maybeSelfBench() {
	if e.features == nil || !e.features.SelfBench.Enabled {
		return
	}
	// Capture the checkpoint: the live learned state at this consolidation. When a Store is present we
	// snapshot its records; otherwise (in-memory engine) the checkpoint is the empty seed state — the
	// suite still exercises the loop end-to-end (the seed registries are byte-identical to a fresh boot).
	var snap persist.Snapshot
	if st := e.cfg.Store; st != nil {
		if s := st.Snapshot(); s != nil {
			snap = *s
		}
	}
	checkpoint := selfBenchCheckpointName(e.bus.Tick)
	// Guard: a self-bench at every idle tick would burn the consolidation window; run it AT MOST ONCE
	// per distinct checkpoint tick (the mint count is the cheap "did anything change?" pre-filter the
	// design names — only bench a checkpoint whose state actually advanced).
	if e.selfBenchedAt == checkpoint {
		return
	}
	e.selfBenchedAt = checkpoint

	rep := e.SelfBench(snap, checkpoint, SeedSelfBenchSuite())
	e.bus.Emit(events.BenchReport, "self-bench report: "+rep.Disposition, events.D{
		"checkpoint":  rep.Checkpoint,
		"score":       rep.Score,
		"passed":      rep.Passed,
		"total":       rep.Total,
		"disposition": rep.Disposition, // "propose" — measured, NOT self-committed (§7.5)
		"committed":   rep.Committed,   // always false here (closed-loop is the later SB2 slice)
	})
}

// selfBenchCheckpointName names the checkpoint by the consolidation tick (deterministic — the seeded
// tick, never the wall clock). Engine-local checkpoint tag, distinct from a named persist snapshot.
func selfBenchCheckpointName(tick int) string {
	return "checkpoint@tick-" + itoaSelf(tick)
}

// passWord renders a cell verdict for the event summary.
func passWord(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// clipRune truncates s to at most n runes (a trailing marker when cut), for the bench.cell event row.
func clipRune(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// itoaSelf is a tiny int->string for the checkpoint tag (avoids an extra strconv import dependency in a
// hot-path-adjacent file; the engine already uses similar small helpers).
func itoaSelf(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
