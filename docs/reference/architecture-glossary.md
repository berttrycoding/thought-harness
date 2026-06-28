# Architecture & key-terms glossary — the canonical names

> The authoritative terminology for thought_harness. Grounded in the actual code (not aspiration) —
> the implementation is **Go**, under `go/internal/` (the original Python was removed); the "in code"
> references below point at the live `internal/<pkg>/<file>.go` paths.
> When a doc, comment, or conversation disagrees with this page, **this page wins** and the other
> gets fixed. Where a term has fuzzy informal aliases, they are listed as *retire* — stop using them.

## 0. The three layers — one name, everywhere

| layer | what it is | in code |
|---|---|---|
| **Subconscious** | the silent engine that *supplies* thoughts — specialists, operators, sub-agents, the generative control | `subconscious/` package · `subconscious.*` events |
| **Conscious** | the *thinking you experience* — the thought graph, search, the deliberating Controller | `conscious.*` events |
| **Action** | the *reality-facing* layer — acts on the world and answers the user | `action.*` events |

One name per layer, used identically in prose, the package folder, and the event namespace (the
earlier spatial `BACK / MIDDLE / FRONT` jargon is fully retired — no short-codes, no aliases).

**Role-label strings** (the `"layer.operation"` tags that flow into `llm.*` events + `doctor`
output) follow the same naming, matched on the TAIL after the dot (`scheduler.IsForeground`):

| role label | what it is | layer |
|---|---|---|
| `conscious.generate` | the Conscious serial effortful loop (next GENERATED thought) | Conscious |
| `conscious.compress` | the MCP compress to a one-line gist | Conscious |
| `action.respond` | the user-facing answer synthesised from the resolved graph | Action |
| `seam.transform` | the hidden-seam re-voice — **kept as-is**; the hidden seam is a real organ, not retired jargon, so it is named for the seam (not a layer) | Hidden Seam |

## 1. High-level component recap (one glance)

Three layers, two seams of opposite transparency. **Subconscious** is the silent, now-**generative**
engine; **Conscious** is the thinking session (the thought graph + search); **Action** is the
outward-facing layer.

```
                          ┌──────────────────────── ENGINE (orchestrator: tick loop, lifecycle) ───────────────────────┐
                          │                                                                                            │
   ACTION (inbound)       │   SUBCONSCIOUS — silent engine (generative)         CONSCIOUS — the thinking session       │
   InteractionPort  ─────────►  SubconsciousEngine (pull dispatch, θ-gated)             ThoughtGraph  (one branch EXPANDED,     │
   PerceptionPort   percepts │     ├─ Specialists (persistent, domain-bound)       rest COMPRESSED)                    │
                          │   │     └─ Synthesizer → Program → Workflow            SearchView   (the graph IS A*)        │
                          │   │            └─ Operator (open registry, mintable)    ThoughtMCP   (branch/merge/rerank…)   │
                          │   │                  └─ SubAgent (ephemeral, per phase) Controller   (THINK/BRANCH/ACT/STOP) │
                          │   │                                                                                          │
                          │   │   ══ HIDDEN SEAM (invisible) ══►   FILTER → GATE → TRANSFORM  ══►  injected Thought      │
                          │   │                                                                                          │
                          │   │                                   ◄══ WATCHED SEAM (monitored) ══                        │
   ACTION (outbound)      │   │   intention out ──► FrontActuator ──► reality ──► OBSERVATION in                        │
   respond / act          │   └──────────────────────────────────────────────────────────────────────────────────────┘
                          │   Regulator (holds n<1, U≤1) · ValueSignal V(s) · Convertibility (effortful→automatic)      │
                          └────────────────────────────────────────────────────────────────────────────────────────────┘
```

## 2. The containment / call hierarchy (your mental model, confirmed)

"A workflow uses dynamic operators, operators instantiate sub-agents, sub-agents use skills and
tools" — verified against code, with the build status of each link:

```
Engine                        orchestrates the tick loop                                [implemented]
 └─ SubconsciousEngine                pull-dispatches: fires specialists above θ                 [implemented]
     └─ Workflow              runs a Program phase-by-phase (built by the Synthesizer)   [implemented]
         └─ Program           a control-flow tree: Seq / Par / Loop of Steps            [implemented]
             └─ Operator      one Step = one operator (open registry, mintable on the fly)[implemented]
                 └─ SubAgent  instantiated per phase; applies the operator to a slice    [implemented]
                     ├─ Backend.operator_apply   the text-only cognitive move                [implemented]
                     ├─ Tools  (tool_scope)       least-privilege scope — DEFAULT-DENY enforced [implemented]
                     └─ Skills                    a goal-matched capability registry        [implemented — internal/cognition/skills.go]

Engine (Decision.ACT) ─► WatchedSeam ─► FrontActuator ─► reality (the only effectful path) [implemented]
```

> Reality-wiring P0–P6 landed (see `docs/internal/archive/reports/2026-06-03-checkpoint.md` + `docs/internal/archive/reports/2026-06-04-architecture-audit.md`):
> the tool layer (ToolRegistry + gated ToolExecutor + 5 real builtins), `tool_scope` enforcement
> (default-deny via a `Scoped` filtered registry), and the real FrontActuator effector are all built —
> the effects are real when a workspace is configured, a deterministic stub otherwise.

Skills sit ABOVE this: a **Goal** is **matched** to a **Skill** (unit or higher-level), whose body is
a Program that runs through the same chain. The **Synthesizer** ("workflow translator") builds a
verified Program when no skill matches.

### 2a. Dynamic vs library — which entities are minted, which are drawn from a registry

The generative layer is **not** "everything is minted." Each entity sits differently on the
dynamic-vs-library axis — the distinction that's easy to get wrong:

| entity | created on demand? | drawn from a library/registry? | notes |
|---|---|---|---|
| **Workflow / Program** | **yes** — synthesised on the fly when no skill matches | **yes** — a recurring one is promoted to a named **Skill** (`SkillRegistry`); known shapes via `RecognizeShape` | both: library-first, synthesise-fallback |
| **Operator** | **yes** — `catalog.mint(...)` (verify-before-trust), accumulates across episodes | **yes** — the **open** `OperatorRegistry`, seeded from the cap-op taxonomy | both: seeded *and* mintable |
| **SubAgent** | **yes** — ephemeral, one per workflow phase, then discarded | **no** — never a library item; the *Operator* it embodies is the library item | on-demand only |
| **Tool** | **no** — a fixed, registered effector set (not synthesised at runtime) | **yes** — the `ToolRegistry` of builtins; a sub-agent reaches a `Scoped` subset | library only |

So **operators + workflows are both mintable AND library; sub-agents are on-demand only; tools are a
fixed library, never minted.** (A reader of pillars A1 — which lists only "workflows/operators/sub-
agents" as runtime-generated — should not infer tools are minted too; they are the stable effector
floor beneath the generative layer.)

> Terminology note: the canonical spec under `docs/spec/` was migrated to the current
> Subconscious / Conscious / Action vocabulary (no exemption remains). The retired BACK/MIDDLE/FRONT
> labels now appear only in `docs/internal/archive/` history; each migrated spec carries a one-line
> provenance note pointing back to the v4 framing.

## 3. The two seams (always name which — never bare "seam")

| seam | between | transparency | mechanism |
|---|---|---|---|
| **Hidden Seam** | Subconscious → Conscious | hidden *by design* | `FILTER` (admit raw Candidate) → `GATE` (order + surface survivors; flags when >1 competed — the **Controller** arbitrates, not the seam: SR-1/P0.3) → `TRANSFORM` (re-voice into first-person) → an INJECTED Thought; competing survivors go up as **branches** |
| **Watched Seam** | Conscious ↔ Action | watched *by design* | intention out → `FrontActuator` → OBSERVATION in; every act/observation emitted `watched=True`; the **sole** channel to ground truth |

## 4. Canonical glossary (by layer)

**Orchestration**
- **Engine** (`internal/engine` — `engine.go` / `reactive.go` / `continuous.go`) — top-level orchestrator; wires every subsystem and drives the per-tick loop (reactive or continuous). *retire: "main loop", "superloop".*
- **Lifecycle** (`internal/lifecycle/lifecycle.go`) — state machine: IDLE / ACTIVE / AWAITING_REALITY / AWAITING_USER / SUSPENDED / DONE + quiescence.

**Conscious — the thinking session**
- **ThoughtGraph** (`internal/graph/graph.go`) — indexed substrate of addressable thought nodes; one branch EXPANDED, rest COMPRESSED. *retire: "thought stream" for the structure.*
- **SearchView** (`internal/search/search.go`) — read-only A* projection: thinking **is** best-first search. *retire: "optimization", "optimizer".*
- **ThoughtMCP** (`internal/graph/mcp.go`) — the metacognitive ops the model calls on its own graph (branch/merge/rerank/compress/expand/focus).
- **Program** (`internal/cognition/program.go`) — the control-flow tree (Seq/Par/Loop of Steps) a Workflow runs.
- **Controller** (`internal/critic/controller.go`) — the **executive half of the Critic**: decides THINK/BRANCH/MERGE/BACKTRACK/ACT/STOP. *retire: bare "Critic" for this; never "controller" for the Regulator.*

**Subconscious — the silent engine**
- **SubconsciousEngine** (`internal/subconscious/dispatch.go`) — pull-model dispatcher; fires specialists whose relevance crosses θ. *retire: "push".*
- **PrimitiveSubAgent** (`internal/subconscious/primitive_subagent.go`) — a **persistent**, silent, domain-bound worker faculty (the eight: compute/recall/read/search/run/skeptic/advocate/social) that fires a raw Candidate; the **reference** behind ephemeral SubAgent instances. *Renamed from **Specialist** (the retired prior name — one set of truth across doc/code/test). "Specialist" survives ONLY as the stable contract words: the `backends.SpecialistCaller` CONTENT role, the `specialist` event-kind/wire value, and the persist `SpecialistRecord` — not renamed.* *retire: "Layer 2 process", "Specialist" (as the Go type).*
- **SubAgent** (`internal/subconscious/subagent.go`) — an **ephemeral** runtime instance (composes a PrimitiveSubAgent) made per Workflow phase; carries role/persona/responsibility/tool_scope; applies one operator then is discarded. *PrimitiveSubAgent ≠ SubAgent: persistent reference vs ephemeral instance.*
- **Workflow** (`internal/subconscious/workflow.go`) — a recognized Program run phase-by-phase; biases the Gate per operator family.

**Workflow node vocabulary — the unified node** *(workflow-session-architecture.md; script/hook/dispatch/session/merge)*
- **skill node** — the **agentic** node: applies a matched Skill/Operator, instantiated as a SubAgent that reasons (and may dispatch a scoped tool). The default node kind.
- **script node** — a **deterministic** node: runs fixed code with **no model call** (a parser, a formula/units evaluator like `internal/grounding`, a transform). Determinism where the model is not needed; cheap + reproducible.
- **hook node (Guard)** — an **interceptor** wrapping other nodes with one of three powers: **block** (deny — e.g. the command-safety gate), **inject** (add context/precondition), **floor** (clamp a value/budget). Policy enforced structurally, not by prompt.
- **dispatch node** — **spawns a Session**: runs a bounded sub-workflow as a child episode (depth/budget bounded by the regulator). The generalization of a SubAgent (which runs *one* operator) to a whole sub-workflow.
- **merge node** — **combines a fan-out's results into one** (reduce / vote) — a *workflow-internal* recombination of a planned `Par`/Session fan-out (SR-3). Distinct from *competing* candidates, which go **up as branches** for the Controller (SR-1). Merge = combine a plan; conflict = let Conscious decide.
- **Session** — a **bounded worker episode** that runs a (sub-)workflow. Generalizes SubAgent. Its lifecycle is a quad: **horizon** (single_shot \| bounded \| long_horizon) × **schedule** (on_demand \| heartbeat \| async \| continuous, all tick-deterministic) × **state** (stateless \| scratch \| persistent — NOT the thought graph) × **terminate** (goal_met \| budget \| quiescence \| refuted \| superseded \| watchdog \| parent_ended). Must terminate.

**Subconscious — generative control**
- **Synthesizer** (`internal/cognition/synth.go`) — builds Programs on the fly (heuristic or LLM), mints new operators, verifies before trust. *= your "workflow translator".*
- **OperatorRegistry** (`internal/cognition/operators.go`) — the **open** registry of operators (seeded from the cap-op wiki; mintable at runtime).
- **Operator** (`internal/types/domain.go` — `Operator` + `OperatorSpec`) — an abstract domain-general transform (decompose/validate/compare…). *retire: "optimization operator".*

**Hidden Seam**
- **Filter** (`internal/seams/hidden.go`) — admission half of the Critic; validates a RAW Candidate before voicing. *retire: bare "Critic".*
- **Gate** (`internal/seams/hidden.go`) — orders the admitted survivors for presentation and flags when more than one competed (content-neutral survivor count, **not** a stance read). It does **not** arbitrate: the Controller decides, and competing survivors reach it as branches (SR-1, P0.3).
- **Transform** (`internal/seams/hidden.go`) — re-voices the winner into first-person self-narrative.
- **HiddenSeam** (`internal/seams/hidden.go`) — the FILTER→GATE→TRANSFORM pipeline returning a RelayResult.

**Watched Seam / Action**
- **WatchedSeam** / **AsyncWatchedSeam** (`internal/seams/watched.go`) — intention out, OBSERVATION in (sync blocks; async fires-and-polls).
- **FrontActuator** (`internal/seams/watched.go`) — the reality-facing effector. *(The type name `FrontActuator` survives in code as the historical exception — not renamed.)* Dispatches a REAL tool through the gated `ToolExecutor` when a workspace is configured; falls back to a deterministic stub (clearly "not the product path") when none is.
- **InteractionPort** / **PerceptionPort** (`internal/interaction/interaction.go`) — inbound channels (reactive USER_INPUT / continuous PERCEPT stream).

**Critic (the mandate)**
- **Critic** — an **abstract mandate**, never a runnable component, split into **Filter** (admission, on the hidden seam) + **Controller** (executive, in the Conscious layer). *Do not conflate with the Regulator.*

**Control & learning**
- **Regulator** (`internal/regulator/regulator.go`) — homeostatic θ controller holding the durable regime (n<1, μ>0, U≤1). *retire: "controller" for this.*
- **Stability** (`internal/stability/stability.go`) — dynamic durability validation under the time-varying (generative) plant; the `/stability-check` skill.
- **ValueSignal** (`internal/value/value.go`) — the single scalar V(s), grounded from OBSERVATION, consumed at four sites (rerank / Filter trust / act-threshold / convertibility).
- **Convertibility** (`internal/convert/convert.go`) — effortful→automatic: compiles repeated GENERATED patterns into Specialists, and repeated METACOG into Gate priors **and promoted Skills** (a recurring synthesised Workflow minted into a named, re-matchable Skill).

**Continuous (awake) mode**
- **Arousal** / **Drives** / **DefaultMode** (`internal/cognition/continuous.go` — driven by `internal/engine/continuous.go`) — AWAKE/DROWSY/ASLEEP state, endogenous goals (the μ>0 baseline), and mind-wandering generation.

**Cross-cutting**
- **Backend** (`internal/backends/backend.go`, `internal/llm`) — the swappable language faculty (generate/transform/summarize/score_admit/rank/respond/operator_apply/synthesize_program). *retire: "the LLM" for the seam.*
- **EventBus** (`internal/events/bus.go`) — the structured-event spine; components **emit, never print**.
- **CognitionGraph** (`internal/cogngraph/cogngraph.go`) — the unified cross-layer model: every entity (process→subconscious→seam→conscious→critic→action) addressable with provenance.
- **Candidate** vs **Thought** (`internal/types/domain.go`) — a **Candidate** is the raw pre-seam return; it becomes an **INJECTED Thought** only after FILTER→GATE→TRANSFORM. *retire: calling a raw Candidate an "injection".*

## 5. Key-term realignment (the readjustment)

Informal/overloaded terms you've used, mapped to the canonical name:

| you said | canonical | why |
|---|---|---|
| **BACK / MIDDLE / FRONT** | **Subconscious / Conscious / Action** | spatial jargon → self-explanatory names. now fully renamed: package `subconscious/`, events `subconscious.*` / `conscious.*` / `action.*` (§0). |
| **"main" / "main session"** | **Conscious** (the layer) · **Engine loop** (the orchestration tick) | "main" was doing three jobs. Use Conscious for the layer, Engine loop for the tick. |
| **"layer 2"** | **Subconscious** | there is no numbered layer — the three are Subconscious / Conscious / Action. |
| **`OperatorRegistry` / `SkillLibrary`** | **`OperatorRegistry` / `SkillRegistry`** | one pattern (a named registry you draw from) → one term: **Registry**. |
| **"workflow translator(s)"** | **Synthesizer** (`internal/cognition/synth.go`) | it translates a request into a Program; the Workflow then *runs* the Program. |
| **"optimization / optimization operator"** | **SearchView** (A* over the graph), driven by **Operator** choice + **ValueSignal** | there's no optimizer component; optimization *is* best-first search, with operators + value as inputs. |
| **specialist == sub-agent** | **PrimitiveSubAgent** (persistent worker faculty; renamed from "Specialist") vs **SubAgent** (ephemeral, per phase) | conflating them hides where ephemerality + tool-scoping live. "Specialist" is retired as the Go type — it survives only as the stable `SpecialistCaller` CONTENT role / `specialist` wire value / `SpecialistRecord`. |
| **candidate == injection** | **Candidate** (raw, pre-seam) vs **INJECTED Thought** (post-seam) | distinct types; a Candidate is only an injection *after* the seam. |
| **regulator / controller / Critic (loosely)** | **Critic** (mandate = Filter+Controller) · **Controller** (executive) · **Regulator** (θ homeostasis) | three distinct organs; never swap the names. |
| **bare "seam"** | **Hidden Seam** *or* **Watched Seam** | they have opposite transparency by design — always qualify. |

## 6. Status of the once-roadmap items (most now built — see `docs/internal/archive/reports/2026-06-04-architecture-audit.md`)

| concept | status | note (task) |
|---|---|---|
| **Skills + Goals** | **implemented** | `internal/cognition/skills.go` (Skill/SkillRegistry: unit + higher-level w/ sub-skills, matched, minted, logged) + `internal/cognition/goals.go` (first-class Goal). The matcher is the cheap shape tier; embedding/LLM tiers + goal decomposition are future. |
| **Tool-scope enforcement** | **implemented (default-deny)** | a sub-agent runs `executor.Scoped(tool_scope).Execute(...)`; `Scoped` builds a registry holding ONLY the scoped tools, so any out-of-scope call fails to resolve → deny. Structural, not advisory (reality-wiring P2). |
| **Effectful tools / real `execute_in_world`** | **implemented** | ToolRegistry + gated ToolExecutor + 5 real builtins (read_file/write_file/run_shell/run_tests/search) + sandbox/command-safety gate; FrontActuator dispatches them for real with a workspace configured (reality-wiring P0/P1). |
| **Proactive outreach** | **implemented** | awake mode: a developed, above-baseline endogenous line triggers an UNPROMPTED response (`Engine.maybeReachOut`, `internal/engine/continuous.go`), cooldown-gated so it stays durable not spammy; reactive mode never initiates. Per-drive-line first-class Goals remain a refinement. |
| **Continuous-mode durability at scale** | **suite-validated** | the awake regime passes the standing stability suite (n<1, U≤1, μ>0) and was validated at 1200-tick scale in the anchor (CHECKPOINT-2026-06-03 #45). Broader empirical validation beyond the suite remains open. |
| **Grounded/RL value signal** | bootstrap-only | V(s) is a deterministic propose-and-recommend shape, not learned. |
