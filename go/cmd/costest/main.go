// Command costest is the PART-4 no-paid-run cost projector. It estimates the DeepSeek $ of a
// workload from a --log JSONL of llm.call events WITHOUT spending a cent, via two estimators
// (internal/costest):
//
//   - PREFIX-REUSE cache-hit %: DeepSeek caches on an exact prompt-PREFIX match across
//     requests; the tool walks the actual sequence of prompts in the log, finds the longest
//     shared token-prefix each call has against everything seen so far, and reports the
//     fraction of input tokens that would be cache HITS.
//   - TOKEN counts: for a log with NO server usage (an LM Studio / test-double pilot), it
//     estimates prompt/completion tokens via a chars/token heuristic, CALIBRATED against any
//     calls in the same log that DO carry real usage.
//
// Usage:
//
//	costest --log FILE.jsonl [--rate-model deepseek-reasoner] [--rates PATH] [--block 64]
//	    → prints estimated tokens (in/out), estimated cache-hit %, and the projected $.
//
//	costest pilot [--out FILE.jsonl] [--mode reactive|continuous] [--prompt TEXT]
//	    → generates an OFFLINE pilot --log: runs the engine on the deterministic test double
//	      but emits each call's REAL prompt (the same builder the live model uses) as an
//	      llm.call event, so costest has a prompt-bearing log to analyse with no API key.
//
// The whole tool is offline + stdlib-only: it reads a file and does math; it never calls a
// model or needs a key.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/cost"
	"github.com/berttrycoding/thought-harness/internal/costest"
	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/trace"
	"github.com/berttrycoding/thought-harness/internal/types"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "pilot" {
		if err := runPilot(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "costest pilot:", err)
			os.Exit(1)
		}
		return
	}
	if err := runEstimate(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "costest:", err)
		os.Exit(1)
	}
}

// runEstimate is the default subcommand: read a --log, estimate, project, print.
func runEstimate(args []string) error {
	fs := flag.NewFlagSet("costest", flag.ContinueOnError)
	logPath := fs.String("log", "", "the --log JSONL of llm.call events to analyse (required)")
	rateModel := fs.String("rate-model", "deepseek-reasoner", "rate-card model id to project the $ against")
	ratesPath := fs.String("rates", "", "rate-card JSON (default: embedded DeepSeek card)")
	block := fs.Int("block", 0, "prefix-cache block size in tokens (0 → DeepSeek default 64)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *logPath == "" {
		fs.Usage()
		return fmt.Errorf("--log FILE.jsonl is required")
	}

	calls, err := costest.ReadLog(*logPath)
	if err != nil {
		return err
	}
	if len(calls) == 0 {
		return fmt.Errorf("no llm.call events in %s — generate a pilot log with: costest pilot --out %s",
			*logPath, *logPath)
	}

	card, err := cost.LoadFile(*ratesPath)
	if err != nil {
		return err
	}
	proj := costest.Project(calls, card, *rateModel, *block)
	fmt.Print(costest.Report(proj, calls, card.Source))
	return nil
}

// ---------------------------------------------------------------------------
// pilot: generate an offline prompt-bearing --log on the deterministic test double.
// ---------------------------------------------------------------------------

// runPilot runs the engine on the test double but with a prompt-capturing backend wrapper that
// emits each role's REAL prompt as an llm.call event — so the offline run produces the same
// prompt-bearing log a real model run would, with no network/key. The engine drives the calls;
// the wrapper only adds the llm.call emission + delegates content to the test double.
func runPilot(args []string) error {
	fs := flag.NewFlagSet("costest pilot", flag.ContinueOnError)
	outPath := fs.String("out", "/tmp/pilot.jsonl", "where to write the pilot --log JSONL")
	mode := fs.String("mode", "reactive", "engine mode: reactive | continuous")
	prompt := fs.String("prompt", "What's 7×8, and is it safe to ship?", "the seed prompt to think about")
	ticks := fs.Int("ticks", 40, "tick budget to run")
	seed := fs.Int("seed", 7, "seeded RNG")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sink, err := trace.NewJsonlSink(*outPath)
	if err != nil {
		return err
	}
	defer sink.Close()

	cfg := engine.DefaultConfig()
	cfg.Mode = *mode
	cfg.Seed = *seed
	// The prompt-capturing wrapper over the deterministic test double: it emits each role's real
	// prompt as an llm.call (usage absent, since the double makes no server call) and delegates
	// the content. EmitBinder is implemented, so NewEngine wires the bus into it. The wrapper is
	// NOT the *TestBackend type the engine recognises, so wire the inner double's ShapeRecognizer
	// ourselves (NewEngine only wires it for a bare *TestBackend) — so synthesised workflows fire
	// and the pilot exercises the operator/program roles too.
	inner := backends.NewTest()
	inner.ShapeRecognizer = cognition.RecognizeShapeDict
	be := newPilotBackend(inner)

	eng, err := engine.NewEngine(&cfg, be)
	if err != nil {
		return err
	}
	eng.Bus().Subscribe(sink.On)
	eng.SubmitDefault(*prompt)
	eng.Run(*ticks)

	fmt.Printf("pilot: wrote %s (mode=%s, prompt=%q)\n", *outPath, *mode, *prompt)
	fmt.Printf("       analyse it with:  costest --log %s --rate-model deepseek-reasoner\n", *outPath)
	return nil
}

// pilotBackend wraps the deterministic TestBackend and, for each CONTENT role, emits an
// llm.call event carrying the SAME prompt the real model backend would send (built by the
// exported llm.Prompt* builders — single source of truth) before delegating the content to the
// double. The emitted event has the prompt text but NO usage (-1), exactly the shape an
// LM Studio pilot that doesn't report usage produces — which is the case costest is built for.
type pilotBackend struct {
	inner *backends.TestBackend
	emit  events.Emit
}

func newPilotBackend(inner *backends.TestBackend) *pilotBackend {
	return &pilotBackend{inner: inner}
}

// BindEmit receives the bus emitter from the engine (backends.EmitBinder) so the wrapper can
// publish llm.call events onto the same trace the JsonlSink writes.
func (p *pilotBackend) BindEmit(emit events.Emit) { p.emit = emit }

// emitCall publishes one prompt-bearing llm.call event in the SAME data shape the real backend
// emits (role/model/system/user/raw + usage), but with usage absent (-1): the test double made
// no server call, so there is no real token count — exactly the usage-less pilot case.
func (p *pilotBackend) emitCall(role, system, user, raw string) {
	if p.emit == nil {
		return
	}
	p.emit(events.LLM, "["+role+"] test-double (pilot): "+headRunes(raw, 36),
		events.D{
			"role": role, "model": "test-double", "ms": 0, "raw": raw,
			"system": system, "user": user, "finish_reason": "stop",
			"reasoning_tokens": -1, "salvage_used": false, "retry_count": 0,
			"prompt_tokens": -1, "completion_tokens": -1, "total_tokens": -1,
			"cached_input_tokens": -1, "cache_miss_tokens": -1,
		})
}

func (p *pilotBackend) Generate(goal string, ctx []types.Thought, rng *cpyrand.Random) string {
	out := p.inner.Generate(goal, ctx, rng)
	s, u := llm.PromptGenerate(goal, ctx)
	p.emitCall("conscious.generate", s, u, out)
	return out
}

func (p *pilotBackend) Wander(kind, hint string, ctx []types.Thought, rng *cpyrand.Random) string {
	out := p.inner.Wander(kind, hint, ctx, rng)
	s, u := llm.PromptWander(kind, hint, ctx)
	p.emitCall("conscious.wander", s, u, out)
	return out
}

func (p *pilotBackend) Transform(c types.Candidate, hist []types.Thought) string {
	out := p.inner.Transform(c, hist)
	s, u := llm.PromptTransform(c, hist)
	p.emitCall("seam.transform", s, u, out)
	return out
}

func (p *pilotBackend) Summarize(ts []types.Thought) string {
	out := p.inner.Summarize(ts)
	s, u := llm.PromptSummarize(ts)
	p.emitCall("conscious.compress", s, u, out)
	return out
}

func (p *pilotBackend) Respond(goal string, ctx []types.Thought) string {
	out := p.inner.Respond(goal, ctx)
	s, u := llm.PromptRespond(goal, ctx)
	p.emitCall("action.respond", s, u, out)
	return out
}

func (p *pilotBackend) OperatorApply(role, responsibility, intent, domain, goal string, ctx []types.Thought) string {
	out := p.inner.OperatorApply(role, responsibility, intent, domain, goal, ctx)
	s, u := llm.PromptOperatorApply(role, responsibility, domain, goal, ctx)
	p.emitCall("operator."+role, s, u, out)
	return out
}

func (p *pilotBackend) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	out, ok := p.inner.Specialist(domain, description, ctx)
	if ok {
		s, u := llm.PromptSpecialist(domain, description, ctx)
		p.emitCall("specialist."+domain, s, u, out)
	}
	return out, ok
}

func (p *pilotBackend) EmitVerdict(worker, goal string, optionLabels []string, priorReasoning string) string {
	out := p.inner.EmitVerdict(worker, goal, optionLabels, priorReasoning)
	s, u := llm.PromptEmitVerdict(worker, goal, optionLabels, priorReasoning)
	p.emitCall("emit_verdict", s, u, out)
	return out
}

func (p *pilotBackend) SynthesizeProgram(goal string, ctx []types.Thought, opNames []string) (map[string]any, bool) {
	return p.inner.SynthesizeProgram(goal, ctx, opNames)
}

func (p *pilotBackend) AppraiserName() string { return p.inner.AppraiserName() }

// compile-time: the wrapper is a full Backend + the optional capabilities it forwards.
var (
	_ backends.Backend          = (*pilotBackend)(nil)
	_ backends.EmitBinder       = (*pilotBackend)(nil)
	_ backends.SpecialistCaller = (*pilotBackend)(nil)
)

// headRunes is the event-summary previewer (first n runes, single line).
func headRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
