# Silent-Injection Cognition: A Pre-AGI Architecture

*An architecture sketch derived from introspection on how human thinking actually feels — and a contrast with how LLM harnesses are built today.*

> Layer names are Subconscious / Conscious / Action (originally framed as BACK / MIDDLE / FRONT in the v4 design; see `docs/reference/architecture-glossary.md` §0).

> **v3 — adds the control layer:** cognitive operators (abstract, domain-general transforms) compose into workflows that Layer 2 recognizes and auto-executes as phases, each filled by a runtime-instantiated subagent. This is a third way thought moves, on top of the closed recombination loop and its opening to reality.
>
> **v4 — the spatial structure:** three layers (Subconscious engine / Conscious main session / Action layer) with two interfaces of *opposite transparency* — a **hidden seam** (Subconscious↔Conscious: select + transform returns into the narrative, frictionless and unnoticed) and a **watched seam** (Conscious↔Action: intention out, reality feedback in, consciously monitored).
>
> **v5 — Conscious is a thought graph:** the main session is re-entrant and indexed (thoughts are counted), forks into branches when injections conflict, holds one branch at full resolution while compressing the rest to gist, and manages it all with three operations — **rerank · expand · compress**. This is how parallel injection is converted back into a serial, navigable train of thought.

---

## Core idea in one line

Cognition is a **closed inner engine** that silently recombines what it already knows, plus one **deliberate opening to the world** — effectful action — that exists to pull in ground truth the engine cannot generate on its own.

The dividing principle is not *retrieval vs. compute*. It is **world-changing-ness**: does this change state outside the thought-stream, or not?

---

## The organizing axis: non-effectful vs. effectful

Everything hangs on one line, and it is **not** the cognitive *type* of the operation.

| | Non-effectful | Effectful |
|---|---|---|
| **Touches** | only your own thought-space | state outside the stream (a file, a buffer, the world) |
| **Examples** | recall, intuition, simulation, mental math, "seeing" that code runs or an expression simplifies | editing the actual line, running the actual command, sending the thing |
| **Mode** | silent, parallel, injected | foreground, serial, deliberate, narrated |
| **Convertible by practice?** | **Yes** — migrates from effortful→silent with fluency | **No** — stays foreground forever |
| **Why** | closed system; nothing in the world needs to receive it | the world must actually receive it, targeted and in order |

**Compute is not inherently foreground.** Strong mathematicians see a simplification without working it; fluent coders read behavior off a function; you can run a small program in your head and the output just arrives. Internal compute changes only your thought-space, so it can be a silent specialist — and fluency is exactly what moves it there.

**Effectful action is permanently foreground** — not because it's "harder," but because it's the stream's only channel to information it cannot produce internally.

---

## The three layers and the two interfaces

Spatially, the system is **three layers with an interface between each pair.** The functional spine (closed loop → opening → return) runs *through* this arrangement.

- **Subconscious — the hidden engine.** Silent specialists, the dispatch that triggers them, and the workflow/operator control flow. Everything here runs out of view; many things can trigger it (relevance, a workflow phase, an operator). This is the closed recombination loop's machinery.
- **Conscious — the main session.** The language stream, the self-narrative, the conscious workspace. The only layer that experiences itself as "thinking."
- **Action — the action layer.** Conscious, effectful, reality-facing. Executes intentions and *expects feedback from reality* when it does. This is the opening.

The two interfaces are the heart of it, and they have **opposite transparency by design.**

### The Subconscious↔Conscious interface — the *hidden seam* (select + transform)
A subagent's return **cannot be handed to the main session as a subagent return.** If the Conscious layer received "subagent X returned {…}", the narrative would break — it would feel like reading tool output, not thinking. So this interface does two jobs:

- **Select** — choose which return to use (arbitration; the Gate).
- **Transform** — re-voice that raw return into the *self-narrative's format*, conditioned on the **previous thought history**, so it lands as a seamless continuation: *"oh, I get it — if we insert the return here…"* rather than a foreign data blob.

The point is **frictionlessness**: the injection is silent and seamless enough that the main session **doesn't even notice** it came from elsewhere — it experiences the transformed return as its own next thought. This seam is hidden *on purpose.*

### The Conscious↔Action interface — the *watched seam* (intention out, reality in)
The opposite. The main session forms an intention; the Action layer executes it **consciously**, and the system is **watching for the feedback**, because the result is ground truth it cannot generate internally. This seam is visible *on purpose* — you have to be present when reality answers, or it isn't feedback.

```
   ┌──────────────────────────────────────────────────────┐
   │ Action — action layer   (conscious, effectful)         │
   │   executes intention · WATCHES for reality feedback    │
   └───────────────▲───────────────────┬───────────────────┘
                   │ reality feedback   │ intention to act
   ─ ─ ─ ─ ─ ─ ─ ─ │ ─ WATCHED SEAM ─ ─ │ ─ (visible on purpose) ─ ─
                   │                    ▼
   ┌──────────────────────────────────────────────────────┐
   │ Conscious — main session  (the self-narrative)         │
   │   experiences itself as thinking                       │
   └───────────────▲────────────────────────────────────────┘
                   │ transformed, seamless injection
   ─ ─ ─ ─ ─ ─ ─ ─ │ ─ HIDDEN SEAM ─ (invisible on purpose) ─ ─ ─ ─
                   │  SELECT (gate) + TRANSFORM (re-voice to
                   │  narrative, conditioned on thought history)
   ┌───────────────┴────────────────────────────────────────┐
   │ Subconscious — hidden engine                            │
   │   silent specialists · dispatch · workflow / operators  │
   └─────────────────────────────────────────────────────────┘
```

One seam built to be **invisible** (the engine's returns must dissolve into the narrative) and one built to be **monitored** (reality's returns must be caught in the open). Each is transparent or opaque *because of what it carries*: the engine carries recombinations of the already-known (safe to dissolve); the Action layer carries reality (must be caught).

---

## The Conscious layer is a thought graph (branch · compress · expand · rerank)

The main session is **not a linear stream.** It is **re-entrant** — it re-reads and re-processes its own prior thoughts — and the thoughts are **counted/indexed**, so each is an addressable node. That indexing is what makes everything below possible: you cannot branch from or return to a thought you cannot address.

**Branching on conflict.** The Subconscious fires many injections in parallel, and they can **conflict or point in different directions.** When they do, Conscious doesn't pick one and bin the rest — it **forks the context into branches.** The structure is a **graph, not a line.**

**Bounded focus + compression.** Only *one* branch can be held at full resolution. The active branch is expanded; every other branch is **compressed to gist.** When you switch, the side you leave **compacts** and the side you enter **expands** — attention stays bounded while nothing is thrown away, and a stashed branch can be revisited later. This is a *hard constraint*, not a feature: two full trains of thought cannot be held at once, so the architecture is forced to compress the inactive.

**This is the parallel→serial conversion.** Injection is inherently parallel (potential branches); conscious thought is serial (one focus). Conscious converts one into the other by **focusing a single branch and compressing the rest** — a serial thought-graph reconstructed from parallel potential via *selective compression.*

**Three operations over the graph:**
- **rerank** — reprioritize the frontier: which branch/thought deserves focus next.
- **expand** — decompress a stashed branch, or unfold a single thought into detail.
- **compress** — compact the inactive (fade-to-gist) to free capacity for the current focus.

Two earlier components get refined here:

- **The Gate forks, it doesn't discard.** Earlier the Gate "picked a winner." More precisely: the winner becomes the **active focus**; the losers become **retained, compressed sibling branches.** Arbitration is forking, not deletion.
- **Exhaustion has two exits, not one.** When the current branch is spent, the system can **backtrack** — pop to a compressed sibling and expand it (cheap, internal) — *or* **act** to import new information (expensive, external). Backtracking the graph is the internal alternative to opening the channel to reality.

**Open question this raises:** compression is **lossy.** A revisited branch may have lost the detail that made it promising, and `rerank` needs a *value signal* to know which branch deserves expansion. So: what drives rerank, and how lossy can compression get before revisit stops being worth it? This sits directly against the Critic.

---

## Inside the layers (information side)

*Zooming into Subconscious and Conscious. (Action — the action layer — is the opening, covered by the spine below.)*

### Layer 1 — the language stream
- Inner speech. Same faculty whether talking aloud or to yourself.
- The conscious working context. The part that gets "read out."
- Acquires its next thought one of two ways: it **receives** one (injected) or **generates** one (serial effortful loop).

### Layer 2 — the silent specialists
- Background processes — memory, arithmetic, spatial reasoning, simulation, every trained faculty — each a **sub-agent** in its own domain, separate from the language context.
- **Not orchestrated.** Nothing routes to them. Each continuously *reads* the Layer-1 stream and **fires on its own** when the contents fall in its domain.
- On firing, it **injects** its result back into Layer 1, already assembled. You experience the output, never the fetch.
- Semi-permeable: mostly silent, but an injected thought can sometimes be *traced back* toward the process that made it.

---

## Two sources of a thought (the cost gradient)

| | Injected thought | Generated thought |
|---|---|---|
| **Source** | a specialist fired on relevance | Layer 1 ran its own serial loop |
| **Feels like** | "it just came to me" | "I had to work it out" |
| **Cost** | cheap, parallel, silent | expensive, serial, effortful |
| **When** | fluency / expertise / flow | novelty / being stuck / learning |

**Effort is Layer 1 generating because the specialists went quiet.** **Learning is convertibility** — practice builds a specialist, migrating a capability from generated to injected. **Flow** is the loop between generation and injection running tight: each injection becomes context for the next thought, which triggers the next injection. Fluency lives in *handoff speed*, not only specialist quality.

> Convertibility's hard limit: anything non-effectful is in principle convertible to silent. **Effectful action is the one thing convertibility can never pull into the background.**

---

## The spine: closed loop / opening / return

This is the part that makes the foreground/background asymmetry *make sense* instead of being an arbitrary rule.

### 1. The closed inner loop — non-effectful, convertible, silent
The swarm (retrieval + intuition + simulation + mental compute) can only ever **recombine what is already in you.** It is a closed system. However fluent, it cannot produce the thing it doesn't already contain — and it can be **confidently wrong with no internal signal of it.**

### 2. The opening — effectful action, foreground and deliberate
Action is the stream's **only channel to ground truth.** You run the code not because you couldn't simulate it, but because the simulation could be wrong and *reality is the thing that corrects it.* This is why effectful action stays foreground, targeted, and attention-gated: you have to be **watching when the feedback lands**, or it isn't feedback. Backgrounding it would destroy the very thing it's for.

### 3. The return — reality's feedback re-enters
The result comes back in as a **new injected observation** — the one kind of input the swarm could never have generated on its own. The loop closes: think → form intention → act → reality returns feedback → that feedback conditions the next thought.

### Corollary: what "stuck" actually is
Stuck is **not** "no specialist fired." Stuck is the **closed loop exhausting itself** — it has recombined everything it has and still has no answer. That is precisely the signal to **act and import new information**, not to think harder. Getting stuck is the system correctly detecting it has hit the wall of its closed part and must open the channel to reality.

---

## The control layer: cognitive operators and workflows

Reactive injection and serial generation are both *local* — they produce the next thought, one at a time. Neither explains **structured, multi-phase thinking**, where you follow a shape like **design → build → validate** across a whole episode. That structure is a third kind of movement: **templated control flow.**

**Cognitive operators** are abstract, domain-general primitives — *decompose, validate, compare, generalize, abstract, simulate.* They are **not specialists.** A specialist is domain-bound (the math sub-agent, the memory sub-agent); an operator is a *transform on whatever is in the stream, regardless of domain.* "Decompose" works the same on a proof, an essay, or a system design.

**Workflows** are learned compositions of operators into phased sequences. *Design-build-validate ≈ decompose → generate → check.* Once learned, Layer 2 can **recognize** that the current situation calls for a known workflow and **auto-execute it as phases** — rather than you consciously stepping through each one.

Three properties make this the missing control layer:

- **Runtime-instantiated subagents.** Each phase spins up an *ephemeral worker defined at runtime* to do that phase's job — not drawn from a fixed predefined roster. The workflow assembles its own pipeline on the fly.
- **It biases the Gate.** A workflow conditions *which specialists may surface when.* In the "validate" phase the validate operator and the Critic are privileged; in "build," generation is. The workflow is a **time-varying prior** over what the gate lets through.
- **It is convertible too.** A workflow once walked through deliberately becomes an **auto-firing macro.** Expertise isn't only better specialists and faster handoff — it's whole *workflows* that run automatically. The migration story (generated → injected) applies to control flow, not just to facts and compute.

So thought now moves **three** ways: reactive injection (specialists fire on relevance), serial generation (the effortful loop), and **templated control flow** (a recognized workflow auto-sequencing phases, each filled by a runtime subagent). The first two are local; the third gives thinking its *shape over time.*

---

## The two unsolved cores

Two components are where the cognition actually lives; a naive "wire up agents" build skips them.

### The Gate (arbitration)
When multiple specialists fire at once — *and Layer 1 is also trying to write the next thought* — something must decide what surfaces, in what order, and resolve conflicts. The gate is **not a specialist**; it's what decides which specialist wins. In humans this is attention. **This is arguably where cognition lives.**

### The Critic (validation + stopping)
The stream must validate an injected thought rather than trust it, know when it's *done*, when to keep searching, when to give up — and (crucially) **when the closed loop is exhausted and it's time to act.** Trust every injection and you hallucinate.

---

## Reference architecture

```
                    ┌─────────────────────────────────┐
                    │   LAYER 1: language stream        │
   generated  ─────▶│   (conscious working context)     │◀──── injected
   (serial loop)    │   thought_t → thought_t+1 → ...    │      (specialists)
                    └───┬───────────────────────────▲───┘
                        │ broadcast (read-only)      │ feedback returns
                        │                            │ as new observation
            ┌───────────▼───────────┐                │
            │ WORKFLOW / OPERATORS  │  runtime control flow:
            │ decompose→build→valid │  sequences phases, each filled
            │ (recognized at runtime)│ by an ephemeral subagent;
            └───────────┬───────────┘  biases the gate per phase
            ┌───────────▼───────────┐                │
            │ GATE (attention)      │                │
            │ what surfaces & order │                │
            └───────────┬───────────┘                │
            ┌───────────▼───────────┐                │
            │ CRITIC                │                │
            │ trust? search? stop?  │                │
            │ exhausted → ACT       │                │
            └─────┬───────────┬─────┘                │
                  │           │ intention to act     │
   ┌──────────────▼──┐   ┌────▼─────────────────┐    │
   │ CLOSED LOOP      │   │ THE OPENING          │    │
   │ (non-effectful)  │   │ effector layer       │    │
   │ memory · math ·  │   │ (foreground, serial) │    │
   │ spatial · sim ·  │   │ IDE · bash · send ·  │────┘
   │ language ...     │   │ run the experiment   │  reality
   │ silent, parallel │   └──────────────────────┘  returns
   │ fires on relevance                              truth
   └──────────────────┘
```

**Loop:** Layer 1 broadcasts → non-effectful specialists read it in parallel and inject silently → a recognized **workflow** may sequence the episode into phases (each filled by a runtime subagent) and bias what the Gate admits → Gate arbitrates competing injections (incl. Layer 1's own candidate) → Critic validates, and if the closed loop is exhausted, forms an **intention to act** → the effector layer executes serially in the foreground → reality returns feedback → that feedback re-enters Layer 1 as a new observation.

---

## Key differences vs. current harness engineering

What makes this pre-AGI / cognitive rather than just another agent framework.

1. **Pull, not push.** Harnesses *push* work via an orchestrator that decides and routes. Here, non-effectful specialists *pull* from a shared stream and volunteer on relevance. Orchestration is **emergent**, not imposed.

2. **No orchestrator.** No central controller makes conscious routing decisions. Memory isn't *called*; it answers when addressed by *content*, not command.

3. **Silence the epistemic, narrate only the effectful.** This is the sharpened critique. Current harnesses don't err by narrating *tool calls* — they err by putting **everything in the same foreground serial channel**: a memory lookup gets the same explicit "I will now call the tool" treatment as a destructive command. The fix: non-effectful retrieval/compute runs silent and parallel; **only world-changing action stays explicit and monitored** — and there, the current explicit model is *correct, not a flaw.*

4. **Two thought-sources with a cost gradient.** Injected (cheap, parallel) vs. generated (expensive, serial) are first-class, and the system is built to move work from the latter to the former.

5. **Convertibility — learning to think cheaper.** Current systems freeze weights at inference and can't migrate a skill from effortful to automatic *during operation.* Here, practice compiles a repeatedly-generated pattern into a fast silent specialist. The system gets cheaper at thinking over time.

6. **The gate is the locus of cognition.** Not the LLM, not the routing logic — the **arbiter** that decides what surfaces, plus the **critic** that validates and decides when to stop thinking and start acting. Specialists are commodity; the gate is the mind.

7. **Action is for ground truth, not just "doing."** Harnesses treat tool calls as task execution. Here, effectful action's *primary epistemic purpose* is to import information the closed loop cannot manufacture — and "stuck" is the explicit signal that it's time to open that channel.

8. **Workflows are learned and instantiated at runtime, not hardcoded.** In current harnesses the control flow *is* the engineer's code — a fixed pipeline — and subagents are predefined. Here, workflows are **cognitive structures recognized at runtime** from the stream ("this is a design-build-validate shape"), and each phase **instantiates an ephemeral subagent on the fly.** A fixed pipeline vs. a system that recognizes the shape of the problem and assembles the pipeline to match — and can compile that whole assembly into an auto-firing macro with practice.

9. **Returns are transformed to match the narrative, not dumped raw.** Current harnesses inject raw tool results straight into the stream — "tool returned {…}" — which is exactly why agent transcripts read as mechanical: *the hidden seam shows.* Here, the Subconscious→Conscious interface **re-voices** every return into the self-narrative's format, conditioned on prior thought, so it continues the thought instead of interrupting it. Narrative coherence is a hard requirement of the silent path, not a cosmetic nicety — and the two seams are deliberately asymmetric (one invisible, one watched) rather than uniformly visible.

10. **Working memory is a compressed thought graph, not a linear context window.** Harnesses append to a flat, growing transcript and pay for all of it at full resolution. Here, Conscious is a **graph** of indexed thoughts with exactly one branch expanded and the rest compressed to gist, plus explicit **rerank/expand/compress** operations and revisitable forks for conflicting returns. Bounded focus and lossy-by-design compression are the mechanism, not a context-length limitation to be engineered away.

---

## What to build / test next

1. **The Gate** — an arbiter taking N silent injections + 1 self-generated candidate and deciding what surfaces, with conflict resolution. Hardest and most important.
2. **The Critic's exhaustion-detector** — the mechanism that recognizes the closed loop has nothing left and converts that into an *intention to act* rather than more thinking.
3. **Convertibility** — a mechanism that compiles a repeatedly-generated effortful pattern into a fast specialist, so the system observably gets cheaper with practice.
4. **Workflow extraction & runtime instantiation** — recognizing that the stream is in a known workflow shape, sequencing its phases, spinning up an ephemeral subagent per phase, and biasing the Gate per phase. Plus making workflows themselves convertible (deliberate → auto-firing).
5. **The transform interface** — selecting a return and re-voicing it into the ongoing first-person narrative, conditioned on thought history, so injections are *seamless*. This is what makes the silent path silent rather than merely fast, and it's the single component current harnesses most lack.
6. **The thought-graph manager** — indexed thoughts, forking on conflicting returns, one expanded branch with the rest compressed, and rerank/expand/compress operations. The hard sub-problems: the *value signal* that drives rerank, and how lossy compression can be while keeping revisit worthwhile.

---

## Lineage (where this rhymes with prior work)

- **Blackboard architectures** (classical AI): a shared workspace many specialists read/write opportunistically, no central control.
- **Global Workspace Theory** (cognitive science): a broadcast conscious workspace monitored by unconscious specialists that activate on relevance.
- **Dual-process accounts** (System 1 / System 2): maps loosely onto injected (fast/automatic) vs. generated (slow/effortful).
- **Active inference / action-as-evidence-gathering**: rhymes with "action exists to import ground truth the closed model can't produce."
- **SOAR / production systems**: cognition as *operators* applied to a state, with *chunking* compiling sequences into faster units — a close rhyme for cognitive operators + convertibility.
- **Hierarchical Task Networks (HTN)**: decomposing a task into phased sub-tasks — rhymes with workflows, though here the workflow is recognized and instantiated at runtime rather than authored in advance.

The novel emphasis: the **effectful/non-effectful axis as the true dividing line**, the **cost gradient + convertibility**, **the gate as locus**, and **action-as-opening-to-reality** (with "stuck = exhausted closed loop = signal to act") — applied as a pointed critique of push-based, uniformly-narrating LLM harnesses.
