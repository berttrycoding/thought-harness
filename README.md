# Thought Harness

A research harness for modeling cognition as an architecture. The idea is to give a language model an
information architecture *around* it rather than a bigger prompt *inside* it: retrieval and internal
compute run silently and automatically, the conscious stream manages itself as a navigable thought
graph, and only world-changing action is explicit and monitored.

> **Status: research preview / experimental.** This is an active investigation, not a finished product.
> The architecture is implemented end to end and runs. Some of its larger claims (continuous "awake"
> operation, self-improvement at scale) are theses we are still testing, not delivered features. The
> [What's real vs. what's open](#whats-real-vs-whats-open) section below says which is which.

The premise: a useful pre-AGI system depends less on a smarter inner loop and more on the information
architecture around the model. What stays hidden, what reaches the conscious stream, and what is
allowed to touch reality. This repo is a runnable place to test that premise.

Implementation: Go and Bubble Tea, in [`go/`](go/). The design corpus is in [`docs/spec/`](docs/spec/)
(the system spec, the narrative architecture, executable-shaped pseudocode, and the durability
analysis), with per-layer notes in [`docs/cognition/`](docs/cognition/) and a terminology and component
map in [`docs/reference/architecture-glossary.md`](docs/reference/architecture-glossary.md).

## Architecture

Three layers, two seams. The seams have opposite transparency: one is hidden by design, the other is
monitored by design.

```
   ┌───────────────────────────────────────────────────────────┐
   │ ACTION       effectful · reality-facing · gated tools       │ ← only this touches the world
   └───────────────────────────────────────────────────────────┘
        ▲ intention out          reality in ▲
   ═══════════════ WATCHED SEAM — visible by design ════════════
        │                                  ▲
   ┌───────────────────────────────────────────────────────────┐
   │ CONSCIOUS    the thought graph + self-narrative             │ ← Controller drives it;
   │              (one branch EXPANDED, siblings COMPRESSED)     │   Perception Port can interrupt
   └───────────────────────────────────────────────────────────┘
        ▲ re-voiced as the stream's own next thought
   ┄┄┄┄┄┄┄┄┄┄┄┄┄ HIDDEN SEAM — invisible by design ┄┄┄┄┄┄┄┄┄┄┄┄┄
        │ filter (admit raw) → gate (arbitrate) → transform
   ┌───────────────────────────────────────────────────────────┐
   │ SUBCONSCIOUS silent specialists · dispatch · operators     │ ← fires on relevance, never seen
   └───────────────────────────────────────────────────────────┘
```

The hidden seam runs from Subconscious to Conscious. Raw retrieval is validated, arbitrated, and
re-voiced so it reads as the conscious stream's own next thought. The monitored seam runs from
Conscious to Action: intention goes out, ground truth comes back.

Every operation is exactly one of three types. That distinction is the axis the whole design rests on:

| Type | Layer | Conscious? | Effectful? | Transparency |
|---|---|---|---|---|
| **Injection** | Subconscious → Conscious | no (silent) | no | hidden seam |
| **Thought-MCP** (metacog) | within Conscious | yes | no | internal |
| **Action** | Conscious → Action | yes | **yes** | watched seam |

## Quickstart

Everything runs from `go/`, the Go module root. You can run the whole thing with no API key: the `test`
backend is a fully offline test double, which is also how the system stays reproducible.

```bash
cd go
go run ./cmd/thought tui --backend test          # multi-panel TUI, offline, no key
make test                                         # go test ./...  (offline, deterministic)

# headless traces (also offline on --backend test):
go run ./cmd/thought run --backend test                    # reactive episodic loop
go run ./cmd/thought run --backend test --mode continuous  # awake/continuous loop
go run ./cmd/thought scenario S5 --backend test            # "stuck -> act": the loop opens to reality
```

To drive it with a real model, point it at an OpenAI-compatible server:

```bash
go run ./cmd/thought run --backend local         # a local OpenAI-compatible server (e.g. LM Studio)
```

The full `--backend` menu is `auto | frontier | local | session | claude | test`. The `claude` and
`session` backends bridge to the Claude Code CLI and need a Claude login, so they are mainly for
development on this repo; from outside, use `test` (offline) or `local` (your own server).

On a real model, Conscious writes its own thoughts and the hidden seam re-voices injections. The Filter
can escalate an ambiguous admission to the model, while the Gate always ranks deterministically and
never calls a model. The `test` double returns canned strings for the content roles, so it exercises
the plumbing but not the model's judgment. A green offline run is necessary but not sufficient: a change
to a content or cognition path is only confirmed once it has run on a real model.

## What's real vs. what's open

The split below is here so you can check the claims against the code.

**Implemented and runnable today**
- The full three-layer architecture with both seams: silent injection, the FILTER → GATE → TRANSFORM
  hidden seam, and the watched action seam, end to end, with a live TUI panel per subsystem.
- The conscious thought graph: branch, merge, rerank, compress, expand, and focus, with one branch
  EXPANDED and its siblings COMPRESSED, navigated as A\* best-first search.
- A split Critic (the Filter handles admission, the Controller handles the executive), one value signal
  `V(s)` consumed at four sites, and a lifecycle state machine.
- Convertibility (effortful to automatic): repeated patterns mint specialists and workflows.
- Continuous ("awake") mode: arousal, endogenous drives, a default-mode generator, and proactive
  outreach. Opt-in and default-off; durability at scale is in the open-theses list below.
- A homeostatic regulator with control-theoretic durability conditions. A [stability suite](go/)
  measures the boundedness conditions over time-varying generative workloads: subcritical excitation
  `n<1`, schedulability `U≤1`, bounded fan-out, and positive baseline `μ>0` when awake. The fifth
  condition `0<K·g<2` is reported as telemetry-only / NA on standing workloads: when the loop is
  saturated or open, the plant gain is not identifiable and falls back to the configured prior, so the
  suite reports 3 to 4 of 5 hard checks holding and excludes `K·g` from the count. It is a failable
  check only in the actively-controlled regime.
- Real tools behind a gated, sandboxed executor; persisted registries; substrate-provenance tagging.

**Theses still being tested (research direction, not a guarantee)**
- **Self-improvement at scale.** The convertibility mechanism exists. Whether it compounds into
  durable, general capability gains is the open question, so every proposed improvement is gated on a
  measured metric (keep-or-revert) rather than asserted.
- **Durable continuous operation.** Awake-mode durability holds in the standing stability suite under
  the conditions above. Broad empirical validation beyond that suite is not done yet.
- **The value signal is bootstrap.** `V(s)` orders by real outcomes, but its weights are hand-tuned,
  not RL-grounded yet. Section 12 of the spec is the path to learned and distilled components.
- **Measurement is a first-class problem.** On a non-deterministic substrate, run-to-run noise can
  swamp small effects, so the noise floor is characterized before any lift is trusted.

## What this is not
- **Not a finished product.** It is an experimental research harness; interfaces and internals change.
- **Not the permanent home.** This repo is an active experimentation ground and accumulates a lot of
  try-things churn. When the architecture settles, the plan is to rewrite it clean in a fresh repo
  rather than carry the experiments forward.
- **Not a claim of sentience or AGI.** "Cognition" here names an architecture that is modeled and
  tested, not an achieved mind. It is deliberately pre-AGI.
- **Not RL-trained.** The learned-component path is designed but not built yet (see above).
- **Not benchmarked into the ground.** Capability evaluation is an open, active part of the work, not a
  settled scoreboard.

## What the TUI shows

Two surfaces, switched with Shift+Tab: a **chat** view, and a **cognition** view split into
full-screen tabs (Tab cycles them), one per cognition system, updating every tick.

- **Conscious** — the live thought stream, the branch tree (active EXPANDED, siblings COMPRESSED), and the A\* search view.
- **Subconscious** — specialists with relevance bars (which fired), the generative layer, and the hidden seam (Filter → Gate → Transform).
- **Action** — intentions out, observations (ground truth) in, and real tool execution.
- **Systems** — the Critic/Controller decision, the value signal `V(s)`, the durability dashboard (`n<1`, `U≤1`, `0<K·g<2`), lifecycle, memory, and the raw event trace.
- **Registry** — the capability inventory: operators, sub-agents, skills, workflows, tools, prompts, and memory (`↑↓` picks a registry, `PgUp/PgDn` scrolls).

Keys: `Enter` send a goal · `Shift+Tab` switch chat/cognition · `Tab` cycle cognition tabs · `^K` command palette · `^B` toggle panel surface · `PgUp/PgDn` scroll · `^C` quit.

## Layout

```
go/                 the implementation (Go + Bubble Tea)
  cmd/thought/      the CLI entrypoint (tui | run | scenario | doctor | compare | stability)
  internal/         the engine packages
  Makefile          build / install / tui / run / test
docs/spec/          the canonical design spec, narrative, pseudocode, and durability analysis
docs/cognition/     per-layer notes (subconscious / conscious / action / seams)
docs/reference/     terminology, glossary, and the subsystem map
```

## License

Licensed under the [Apache License, Version 2.0](LICENSE). See [`NOTICE`](NOTICE) for attribution.
