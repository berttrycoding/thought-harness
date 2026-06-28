# Subconscious Subsystem — object model (living doc)

> Part of the **Cognition Architecture** — see [`00-INDEX.md`](00-INDEX.md) for the master map of all
> subsystems (Subconscious · Conscious · Action · Seams) and the cross-cutting concerns.
>
> Status: **BASELINED MODEL** (re-baselined 2026-06-14; **skill model 06-14, scope model + 9-issue
> cross-doc reconciliation 06-15**). The object model below is the **single source of truth** for the
> subconscious. §4 holds the current-code delta; §6 tracks decided vs open.
>
> **Scope:** the subconscious subsystem — the `Capability → Workflow → Operator → SubAgent` stack and
> its object model. Some sections here are **cross-cutting** (reference/instance §2.4, eval §3.18–3.21,
> self-improvement §3.17, protected core §2.8, the hidden seam §2.1); documented here for now and
> indexed in `00-INDEX.md` — they may lift to a foundations doc later.

---

## 1. The source ideas (as raised)

Five interlocking ideas drove the discussion:

- **Object-type menagerie + containment** — the subconscious is a chain of distinct **object types**.
- **Registry per object type + dynamic-create → mint** — each type is *listed* in a registry **or**
  *created on the fly* and then **minted** into the registry.
- **Definition + Context** — a created object's **definition** needs **context** to generate/justify it;
  context should be its own object.
- **Eval/benchmark at multiple levels** — overall system, per-subsystem, per-object-type **registry**,
  and per **instance**/**reference**. (Granularity *levels* — not the `Scope` object, §3.3a.)
- **Eval as an object type** — eval itself is registry-able and mintable.

---

## 2. THE MODEL (single source of truth)

> **LANDED-STATUS BANNER (2026-06-20, default-ON; supersedes the `GAP` tags below).** The cognition
> object-model redesign has SHIPPED and is **default-ON** — the `GAP`/"→ delta: build it" notes in §3–§5
> were written against the pre-redesign code and now **lag the implementation**. What is LIVE on the
> default path (`config.AllOn()` `Subconscious.Capability = true`, `CapabilityDispatch = true`):
> the **Capability** entry object (`internal/subconscious/capability.go`) that captures **Context**
> (`context.go`, the 3-layer L1 snapshot) and produces the episode workflow, sets the **Scope** ceiling
> (`scope.go`, eager, emitted as "this run's authority"), and is the **live dispatch/recognition entry**
> — wired in `internal/engine/reactive.go` + `internal/subconscious/dispatch.go`. The **Specialist→
> PrimitiveSubAgent** rename has landed (`primitive_subagent.go`; `specialist.go` is gone — only the
> stable `backends.SpecialistCaller` role + the `"specialist"` wire/event strings survive by design).
> The two-axis **Category** taxonomy (operation × reach) is built in `internal/action` (see §3.10a / 03).
> **Still genuinely open / opt-in (NOT yet default):** the Capability-as-specialist-firing-entry with the
> least-privilege bite (`CapabilityPrimitiveSubAgents`, default-OFF), SubAgent **owning** category-scoped
> tools+skills (the §3.6 delta), **Context prior** (§3.12), **Eval-as-object** (§3.18–3.21),
> the uniform **per-registry self-improvement loop** (§3.17), and **multi-level compression** (§8 OPT-5).
> Read the per-item tags below as: status-as-of-the-pre-redesign-snapshot — see this banner for what is now live.


### 2.1 The stack

The conscious thinks; the subconscious reaches **up** to it by relevance and injects results back,
**re-voiced** as the conscious's own next thought. The conscious **never knows** the subconscious
machinery exists (silent injection). The whole stack below lives on the **subconscious side of the
hidden seam**:

```
            CONSCIOUS   (thinks; manages its own thought graph)
                ▲                                  │
   arrives as   │  HIDDEN SEAM                     │  subconscious WATCHES the stream:
   its own next │  FILTER → GATE → TRANSFORM       ▼  reads the active branch for relevance
   thought      │  (re-voiced, never dumped)       │  + to capture context
                │                                  │
                │   Capability   ← relevance-pulled ENTRY; captures + passes Context;
                │     │             produces its workflow (reuse seed | synth on the fly); mintable
                │     ▼
                │   Workflow     ← STRUCTURE: a program of operators in serial / parallel / loop
                │     │
                │     ▼
                │   Operator     ← an in-workflow STEP / MOVE; matches to a SubAgent
                │     │
                │     ▼
                └─  SubAgent     ← the WORKER instance; owns its tools + skills (by category);
                                    staffed from the SubAgent registry; ephemeral
```

The knowledge is **one-directional**: subconscious watches conscious; conscious is ignorant of
subconscious. The **watched** seam (Conscious ↔ Action) is the one explicit seam: the conscious
**authors** every world-change there, and reality re-enters across it as **perception/grounding** — a
read crosses for grounding, *not* as an action (owner: 03-action §4.1 / 04-seams §4.1). What stays
invisible is the **Capability machinery** — never surfaced.

**Where Goal sits — above the stack, in the conscious layer.** A **Goal** (`USER` | `DRIVE` | `SUBGOAL`)
is what the thought graph pursues. It **shapes the thought stream**; the stream **affords Capabilities**
by relevance. A Goal **never invokes a Capability directly** (that would break the one-way mirror) — the
connection is *only* through the relevance-matched stream. Goals form a tree (subgoals); drives mint
endogenous ones. Goal therefore belongs to the **conscious subsystem** (the next topic), not the
subconscious stack.

### 2.2 The four stack roles (clear responsibilities)

| Object | Its one responsibility | Does NOT |
|---|---|---|
| **Capability** | relevance-pulled **entry**; captures + passes **context**; produces a workflow (seed or synth); mintable unit | own tools/skills; fix the structure |
| **Workflow** | the **structure** — serial / parallel / loop program of operators | trigger; own tools/skills |
| **Operator** | an in-workflow **step / move**; matches to a worker | trigger; own tools/skills; define structure |
| **SubAgent** | the **worker** — at init owns its **tools + skills** (by category); does the work | trigger; define structure |

### 2.3 The two orthogonal axes

- **Operator = the MOVE** (what step happens). Abstract form (a template in the registry) + concrete
  form (the move bound to the Capability's **Context** + matched to a worker).
- **SubAgent = the WORKER** (who does it). Its *type* is a **primitive subagent** (the worker faculty).

A workflow's program is operators (moves); each operator is **matched/staffed** to a SubAgent (worker)
via `resolve` — reuse a seeded worker whose category-scope fits, or **mint** one. Move and worker are
different things and have different registries.

### 2.4 Reference vs Instance (universal — except Context)

Every object type has a **reference** (its definition in a registry; origin **seeded** or **minted**)
and **instances** (runtime instantiations; a **seed instance** or a **minted instance**).
**Context is the one exception: instance-only** — it is always derived from the runtime, time-based
thought graph, so it can never be a reusable seeded template.

### 2.5 Seeded **and** create

Capabilities, workflows, operators, subagents, **and skills** can each be **seeded** (a reusable reference
in the registry) **or synthesized on the fly and minted** (tools are **seeded-only** — a new tool is
human-gated, §3.9). The Capability decides per-trigger whether to reuse
a seeded workflow or synthesize one — this is why the entry (Capability) must be **separate from** the
workflow (a synthesized workflow doesn't exist until the Capability fires).

### 2.6 Self-improvement is built into every registry

The durable effort goes into **references**, not instances. **Every registry has a self-improvement
loop**: instances run → are **measured** (comparatively, vs past similar instances) → the measurement
**refines the reference** (and/or gates mint/keep/demote). See §2.7 and §3 (eval cluster).

### 2.7 Bounded by construction

The stack stays inside the durability regime (`VerifyProgram` caps: ≤24 steps, depth ≤6, loops 1–6,
parallel width ≤8, ≤144 phases; regulator `n<1 / U≤1 / bounded fan-out`). Flexibility comes from
**late binding + re-concretization**, never from unbounded growth (§8 optimizations).

### 2.8 Protected core + self-access ladder (metacognition guardrails)

Self-improvement runs up a ladder: **L1** refine references · **L2** mint new references · **L3** read
its own **self-documentation** (model itself) · **L4** read/modify its own **source code**. L3/L4 are
served by a **reflexive Capability** so metacognition stays silently injected (§6 future note).
A **PROTECTED CORE (immutable kernel) — to be built — is read-only to the system at every level:** the
durability invariants (`n<1`, bounded fan-out, regulator), the hidden-seam discipline, the Filter
(honesty), the **eval gate + its core measuring sticks** (anti-**wireheading** — the system must not
edit its own stick to pass bad changes), **and the conscience / identity standard** — the governing
good/bad value set + the creator-assigned, agent-immutable identity (authored in 02-conscious §7.2 +
the ported identity system; **this doc grants it core protection**, closing the gap where the alignment
governor claimed protection 01 had not listed). Self-modification rides the same eval + keep-or-revert; the
core and its sticks cannot be touched by the system.

---

## 3. GLOSSARY — flat definitions

Status tags: **BUILT** (exists end-to-end) · **PARTIAL** (seed exists) · **GAP** (not present).
File refs point at the current code; "→ delta" notes the change to reach the model.

### Meta

**3.1 Object type** — a kind of thing in the subconscious with its own schema + lifecycle. Set:
Capability, Workflow, Operator, SubAgent, Primitive subagent, Skill, Tool, **Scope** (§3.3a), **Category**
(§3.10a), Goal, Context, Context prior, Eval.

**3.2 Reference vs Instance** — reference = the definition in a registry (seeded or minted); instance =
a runtime instantiation (seed or minted instance). Universal **except Context** (instance-only).
*Seed in code:* `Synthesized` flags + keep-or-revert demotion (`convert.go:510`).

### The stack

**3.3 Capability — LANDED (default-ON; was `GAP`).** The relevance-pulled object the conscious passively
reaches. On its trigger it (a) **captures** the active-branch Context (§3.11), (b) **produces** a
Workflow — reuse a seeded template or synthesize one on the fly, (c) draws workers from the SubAgent
registry, and (d) sets this run's **Scope** ceiling (§3.3a). Its inputs are the relevance-matched
**stream/Context** *and* the **active goal** (incl. `Goal.Level`, read across the one-way mirror —
subconscious watches conscious; the goal is a *separate input*, not a Context layer §3.11). **Dependency:**
*how* the active goal reaches the Capability (the Capability↔goal handshake) is owned by 02-conscious and
still open there. Mintable as a unit; reference + instances; lives entirely on the subconscious side of the
hidden seam. *Now built:* `internal/subconscious/capability.go` (`Capability`) — relevance triggers + `CaptureContext`
+ `Produce` (reuse-seed-or-synthesise) + the eager Scope ceiling; the LIVE relevance/dispatch entry under
`CapabilityDispatch` (default-ON), wired in `internal/engine/reactive.go` + `internal/subconscious/dispatch.go`.
It REPLACES `Workflow.Recognize`/`Triggers` self-trigger as the entry. Remaining opt-in slice: the
Capability as the specialist-FIRING entry (`CapabilityPrimitiveSubAgents`, default-OFF).

**3.3a Scope — LANDED (ceiling built+sourced+emitted; was `GAP`).** The constraint **twin of
Context**: where Context is the *material* the Capability passes down, Scope is the *authority* it passes
down. Two-part, opposite timing, applied per registry the run draws from (operators, faculties, skills,
tools, knowledge):
- **ceiling — EAGER, set by the Capability.** A bounded least-privilege band (domain + categories +, for
  skills, the tier from `Goal.Level`, §3.8a) capping the *maximum* each registry may be drawn from.
  Cheap (coarse, not concrete), **safety-critical** (a worker may never widen it at runtime — only an
  explicit gate can), auditable as "this run's authority." This is §2.7 (bounded by construction) applied
  to selection.
- **pick — LAZY, resolved just-in-time within the ceiling.** The concrete item, chosen by `Resolve[T]`
  (§3.14) at the layer that has the information: operators at workflow-assembly, skills/tools/workers at
  staffing. Nobody resolves a pick before the object that needs it is born. This is §8 OPT-2 (late
  binding).
- **open shape — per-object-type FACET.** Scope is not a fixed envelope the Capability hardcodes; each
  registry registers its own facet `{ceiling-rule, pick-rule}` through the uniform `resolve.Registry[T]`
  interface, so a new object type ships its own scope facet and the Capability never changes (same openness
  as Categories §3.10a).

So a Scope is literally a **lazy-loader: bounds populated up front, slots filled on demand.** The
discriminator: **eager = the bound (safety); lazy = the pick (relevance)** — a ceiling goes up at the
Capability, a choice defers to the object's birth. *Now built:* `internal/subconscious/scope.go` (`Scope` = domain + categories + skill-tier ceiling +
lazy picks); the Capability sources the eager ceiling (`reactive.go` `NewScope`/`WithScope`, emitted as
"this run's authority"). Remaining delta: per-object-type OPEN facets and the SubAgent owning its own
category-scoped picks (the §3.6 delta, still tied to `CapabilityPrimitiveSubAgents`/category-scoped staffing).

**3.4 Workflow — BUILT (structure).** A program of operators in serial / parallel / loop, **seeded or
synthesized** when its Capability fires. *Code:* `Workflow` (`subconscious/workflow.go:130`) wrapping
`Program` (`cognition/program.go:199`); scheduled once, bounded; can't grow at runtime (§8). → delta:
it is *produced by* a Capability rather than self-triggering.

**3.5 Operator — PARTIAL (in-workflow step / move).** A step in the workflow program; abstract +
concrete (concrete = abstract + the Capability's Context); **matches to a SubAgent**. *Code:*
`OperatorSpec` (`cognition/operators.go:72`), 34 seed + minted. → delta: operators **stop owning tools**
(moves to SubAgent) and **stop triggering workflows** (moves to Capability); they are purely the move.

**3.6 SubAgent — PARTIAL (worker instance).** The worker staffed into a workflow slot to carry out an
operator-step. **At init it owns its tools + skills, scoped by category.** **Ephemeral:** lives until
its **context is filled**, then is **discarded** (no compression/persistence — effort goes into the
reference). Of a primitive-subagent type; origin seed or minted. *Code:* `SubAgent`
(`subconscious/subagent.go:72`), ephemeral. → delta: it **owns its own category-scoped tools/skills**
instead of inheriting the operator's name-list.

**3.7 Primitive subagent — NEW name (worker faculty / subagent type).** The **reference** behind
SubAgent instances: a worker faculty = competence + persona + **tool-categories + skill-categories**
(its capability footprint, matched at staffing). The eight seeded faculties — `compute, recall, read,
search, run, skeptic, advocate, social` — are **kept**; more can be minted. *Code (RENAMED, landed):*
`PrimitiveSubAgent` iface (`subconscious/primitive_subagent.go`) + `MintedPrimitiveSubAgent`. → delta:
**renamed** from `Specialist` to primitive subagent, **repositioned** under Workflow → SubAgent, **add
category scopes**. ("Specialist" is retired as the Go symbol; it survives ONLY as the STABLE CONTRACT
words — the `backends.SpecialistCaller` CONTENT role, the `"specialist"` event-kind/wire values, and the
persist `SpecialistRecord` — which are deliberately NOT renamed.)

> **Faculty disambiguation (the word is overloaded — two DIFFERENT objects, same word; do not conflate):**
> the **WORKER faculty** here = a *primitive subagent* type (the eight subconscious workers
> compute/recall/read/search/run/skeptic/advocate/social). It is NOT the **SEED faculty** of
> `02-conscious.md §1.8` (the brain-model standing-intent grouping — the six
> Perceptual/Introspective/Mnemonic/Motivational/Actional/Validative). Worker faculty = a subconscious
> worker type; seed faculty = an awake-cognition standing-intent grouping. Cross-ref `02-conscious.md §1.8`
> and `internal/cognition/seedintent.go` (`SeedFaculty`). This is the exact drift class that produced the
> Specialist rename gap — keep them separate.

**3.8 Skill — BUILT (a worker PROMPT; reframe + reposition).** A capability a SubAgent invokes,
expressed as a **prompt** — **NOT a program of operators**. (The old `Skill.Body = Program` made a skill
an operator program, which collided head-on with Workflow; decoupling skill from the operator program is
the fix this pass. "Program" now means **Workflow's substrate only**.) Two tiers, both still called
"skill":
- **unit skill** — one capability, one prompt (the leaf).
- **high-level skill** — a prompt that **calls multiple sub-skills** (unit or high-level) *inside the
  prompt*; the composition is resolved **by the worker at run time**, not flattened into the workflow.
  Bounded — the runtime analogue of `VerifyProgram`'s static caps: sub-skill resolution stays **acyclic**,
  **depth ≤ `MaxSkillDepth=3`**, *and* **total resolved sub-skill calls ≤ `MaxSkillCalls`** (a concrete
  per-SubAgent cap, dev-set inside the regulator's fan-out budget §2.7). On either bound the resolver
  **stops and the worker proceeds with what it has** (truncate-to-floor — never unbounded). This
  re-grounds, at the new runtime enforcement point, the durability the old build-time `Expand` gave
  statically.

A skill's **definition folds in its scaffolding** (no separate bundle object): a Skill =
`{prompt + hooks + tool-scope + paired knowledge}`, where **hooks** are the deterministic pre/post
control (Pattern A/C) wrapped around the Pattern-B prompt. Invoked **by category** under the SubAgent.
A skill does **NOT match goals** — that is the Capability's job; "goal-matched" is retired here.
*Code:* `Skill` (`cognition/skills.go:60`), 19 seed + minted. → delta: `Body Program` →
`prompt + sub-skill refs`; build-time `Expand`-into-operators → a **runtime sub-skill resolver** keeping
the acyclic/depth-3 guard; drop goal-matching; add hooks + tool-scope + knowledge to the definition.

**3.8a Skill tier follows `Goal.Level` — set by the Capability, not matched to the goal.** `GAP` The
skill-tier **ceiling** (the tier coordinate of the skill facet of `Scope`, §3.3a) is **rough-matched to
the active goal's decomposition depth** — but the match happens **at the Capability**, never as a
goal→skill link (§3.8: a worker invokes skills *by category*; it does not match goals). A goal/subgoal
carries a `Level` (tree depth — [`02-conscious.md`](02-conscious.md) §1.6; top stable, leaf fluid). When a
Capability fires for that goal it reads `Level` and sets the ceiling:
- **coarse goal (shallow `Level`)** → ceiling **admits high-level skills** (a worker may invoke a composed
  skill — more work per call);
- **decomposed leaf subgoal (deep `Level`)** → ceiling **narrows to unit skills**.

Within the ceiling the worker **picks** via `Resolve[Skill]` (the §3.3a pick), preferring the highest tier
the ceiling allows. **The descent is goal-decomposition, not skill-search.** If the assembled workflow's
workers cannot satisfy the goal with in-ceiling skills, that **miss is feasibility feedback** (02-conscious
§1.9, "decomposition is itself a test"): the conscious **decomposes the goal a level** and a Capability
**re-fires at the finer subgoal** with a lower ceiling — so the skill tier descends *because the goal did*,
through the Capability, not by a worker scanning skills against the goal. The goal tree and the skill-tier
ceiling move **in lockstep because the same `Level` drives both**; the duality is real but routed through
the Capability + `Scope`, with **no layer crossed** (Goal→Capability→Workflow→Operator→SubAgent→Skill stays
intact).

**Cross-seam / cross-session — the feedback loop is owned by 02 §1.9, not here.** A skill-tier miss is a
*subconscious* event; it reaches the *conscious* decomposition loop the only way anything does — **through
the hidden seam**, surfaced as an injected thought ("this needs breaking down"), so the conscious
decomposes as its **own** move (one-way mirror intact, §2.1 / 04-seams). So the skill-miss is one
**feasibility-feedback source** that **02 §1.9 owns** — 02 §1.9 today lists only "decomposition / reasoning
in the stream" and should add skill-miss beside it (**open — coordinate with the conscious doc**). This
doc only *contributes* the signal via the seam; it does not define the loop.

**3.9 Tool — BUILT (effector), category GAP.** A fixed effector behind the gated executor; **tagged
with a category**. *Code:* `Tool` iface (`action/tool.go:63`), 5 builtins, never synthesized. → delta:
add a category/tag so subagent category-scopes can match.

**3.10 Goal — conscious-layer** (defined in [`02-conscious.md`](02-conscious.md)). Not a subconscious
object — a Goal is what the *conscious* pursues. It feeds this stack only **indirectly**: it shapes the
thought stream, which **affords Capabilities** by relevance (§2.1); it never invokes one directly. Full
treatment (setpoint stability, intake/classification, `/goal` vs inferred) lives in the conscious doc.
*Code:* `Goal` (`cognition/goals.go:49`).

**3.10a Category — NEW (growable tag registry).** `GAP` A label that objects (tools, skills, and the
category-scopes of primitive subagents) reference for matching. **Categories are their own registry** —
one shared `Category` registry, facet-discriminated (`tool` | `skill` | …), **seeded** with the starter
sets and **mintable + refinable** (split/merge/rename via the self-improvement loop), *not* a hardcoded
enum. **Owner-of-record for the category taxonomy** (03-action references this, does not re-decide it).
Seed sets: **tool operation** `{inspect, mutate, execute}` **× tool reach** `{self, local, external}` —
the two axes the action gate routes on (03 §2/§3); this **supersedes the earlier flat `{inspect, mutate,
execute, external}`** (which conflated operation and reach). Skills: `{reasoning, analysis, synthesis,
verification}`. A reference-style object (a tag): gets reference-eval (is it useful/distinct?), no
instances.

> **Bucket status (cognitive power-cycle, 2026-06-20).** The `reach=self` bucket — previously empty — now
> has live `op=inspect` sensors (`read_host`, `read_event_log`, the in-memory introspection read), and
> `reach=local` carries the logged `read_clock`. The `reach=external` bucket is still empty (outward
> `fetch_web` is DEFERRED). The sensor *mechanism* is owned by **02 §7.1a** (perception/intake); this axis
> only owns the taxonomy — cite 02, do not restate the sensors here.

### Context cluster

**3.11 Context — LANDED (live object; was `GAP`).** The material that concretizes an operator,
**captured by the Capability** at trigger time and passed down. Instance-only (§2.4). Layers:
- **L1 — compressed active branch (spine).** A **snapshot at trigger time** of the *entire* active
  branch in **compressed** form, with **thought IDs** for on-demand traverse/expand. Replaces the ≤5
  window (`subagent.go:154`) so context **scales**. The branch's compression shifts as we think
  (`graph/mcp.go` `focus`); the snapshot freezes it.
- **L2 — runtime-derived.** Context the workflow/subagent produces *during* the run.
- **L3 — paired domain knowledge (RAG-like).** A knowledge index declared in the operator/subagent
  definition, pulled from the knowledge store and handed to the worker.

*Substrate:* thought IDs + gist `Summary` + lossy `compress`/`expand`/`focus` (`types/domain.go:39`,
`graph/mcp.go:127`). *Gaps:* compression is **binary** (no multi-level states, `types/enums.go:77`);
workers get a ≤5 slice not a snapshot; no trigger-time snapshot; L3 not pre-attached.
*Quantification:* salience/relevance, grounding ratio, sufficiency (necessity deferred).

**3.12 Context prior — NEW (the mintable lesson).** `GAP` A context is never minted; the **lesson**
about it is. A learned association **semantic-pattern-of-the-branch → {knowledge to pull, operator/
capability to favour}**, minted when a run pans out (gated on context-quality + grounded outcome). Next
time a branch matches, pre-pull the knowledge (L3) and **bias selection**. Generalizes the existing
**gate priors** (`convert.go`). This is where the **context mint gate** lives.

**3.13 Definition + Context pair.** `GAP` The stored unit "this definition was generated from this
context," so a definition can be re-judged when its context changes. Today minting gates on outcome
value + frequency (`convert.go:525`), never context quality; the context mint gate (§3.12) adds it.

### Registry & self-improvement

**3.14 Registry / library — BUILT; generic wiring PARTIAL.** A named collection of one object type
(list / lookup / add). Generic contract `resolve.Registry[T]` (`resolve/resolve.go:16`,
`Find/Create/Verify/Store`) exists but bespoke registries dominate.

**3.15 Minting — BUILT; distributed.** Verify a runtime-created thing and promote it into its registry.
You mint **references** (definitions); instances are runtime; **Context isn't minted — the context
prior is** (§3.12). Mint sites: `convert.Consolidate`, `OperatorRegistry.Mint`, `SkillRegistry.Mint`,
`NewMintedSpecialist`. No single mint entry point.

**3.16 Registry features — PARTIAL.** Per-registry metadata (counts, minted-vs-seed, status) + attached
evals. Counts/minted/status exist in the TUI browser; attached-eval does not.

**3.17 Self-improvement mechanism (per registry) — REQUIRED.** `GAP` Every registry needs a loop that
**refines its references from instance measurements** (§3.20). Today partial/uneven (convert
mints/demotes; W1 ledger persists/reverts) — no uniform "refine the reference" loop. Target: a
**standard registry feature**, applied **as fit** per object type.

### Eval cluster

**3.18 Eval object — measuring stick + measurement.** `GAP` Eval is one object type on the universal
axis: the **reference = the measuring stick** (rubric/oracle/test — the registry-able, **mintable**
thing); the **instance = a measurement** (a run + its scores — a record). *Today:* `MechResult`/`Phase0`
/`Lift` (`internal/bench/eval`) are computed-and-discarded; only the LLM-judge **ruler**
(`internal/bench/judge`) is reusable, and it isn't registry-managed.

**3.19 Reference-eval = benchmark (absolute).** `GAP` A generic stick every minted-or-primitive
reference must pass — "does this belong in the registry?" This is the **mint gate, generalized**.

**3.20 Instance measurement & instance-eval — applied AS FIT.** `GAP` Two modes:
- **Discard-time measurement** (common — ephemeral subagents): at discard, **measure comparatively vs
  past similar instances** of the same reference → feed **reference refinement**. This is *measuring*,
  not *benchmarking*.
- **Standing instance-eval** (rare — genuinely long-lived instances): a lifetime scorecard.
*Seeds:* `V(s)` per-instance grounded reward (`internal/value`) = the discard signal; **episodic memory**
(the subconscious **registry** of past instances/facts — *distinct from* 02's conscious **episodic
timeline** §2a, which logs the attention *trajectory*; **two stores, not one**) = past-instance history
(not organized per-reference). *Gap:* incremental reference refinement (convert only mints/demotes).

**3.21 The three eval levels** (granularities — distinct from the `Scope` object §3.3a)**.** (a) whole-system — **BUILT** (`cmd/bench` + `internal/stability`);
(b) per-subsystem / per-registry — **PARTIAL** (per-mechanism/tier only); (c) per-instance /
per-reference — **GAP**. Applied **as fit**, not forced uniformly.

---

## 4. MAPPING TO CURRENT CODE (the delta to the model)

> **Reconcile with the §2 LANDED banner.** This table was written against the PRE-redesign code. The
> Capability / Scope / Context / Primitive-subagent / two-axis-Category rows have since **landed**
> (Capability + CaptureContext + Scope-ceiling + live dispatch are default-ON; the Specialist rename is
> done; the Category taxonomy is two-axis in `internal/action`). The "Code today" column below is the
> historical baseline; the "Delta" column is **done** for those rows and **opt-in/open** only for the
> items the banner lists as still-open (SubAgent-owns-category-scoped-tools, Context prior, Eval-as-object,
> per-registry self-improvement loop, multi-level compression).

| Model object | Code today | Delta to reach the model |
|---|---|---|
| **Capability** | none (closest: `Workflow.Recognize`/`Triggers`, specialist relevance) | **new object**: relevance entry + context capture + workflow seed/synth + sets the Scope ceiling |
| **Scope** | `SubAgent.toolScope` name-list (inherited from operator) | **new**: two-part **ceiling (eager, Capability) / pick (lazy, `Resolve[T]`)**, open per-type facet (§3.3a) |
| **Workflow** | `Workflow`+`Program` (`workflow.go:130`) | produced *by* a Capability (not self-triggering) |
| **Operator** | `OperatorSpec` (`operators.go:72`), carries `ToolScope`, **is** the program step | drop tool-ownership (→ SubAgent) + drop trigger role (→ Capability) |
| **SubAgent** | `SubAgent` (`subagent.go:72`), **inherits** operator tool list | **owns** its tools+skills by **category** |
| **Primitive subagent** | `Specialist` iface + `MintedSpecialist` (`specialist.go:82`) | rename + reposition + add category scopes; retire "specialist" |
| **Skill** | `Skill` (`skills.go:60`), `Body = Program` (operators) | reframe to a **prompt** (unit \| high-level; sub-skills called in-prompt, resolved at runtime, acyclic/depth-3); **fold in** hooks + tool-scope + knowledge; **drop goal-matching** (→ Capability); invoked by category under the SubAgent |
| **Tool** | `Tool` (`tool.go:63`) | add **categories** so subagent category-scopes can match |
| **Context** | raw `[]Thought` / ≤5 `ContextSlice` | 3-layer instance-only object; compressed-branch snapshot |
| **Context prior** | gate priors (`convert.go`) | generalize to pattern → knowledge + operator/capability |
| **Eval** | `internal/bench/eval` (computed), `judge` (reusable) | measuring-stick = registry-able mintable reference |

Other current-code facts: registries present (Operators, Skills, Tools, Specialists, Episodic,
Semantic, Person, Knowledge, Gate-priors, Legible-tags); persistence unified via `Store`(JSONL) +
`Curator` + W1 ledger (snapshot/reset/diff/audit); tool selection is **hardcoded in three divergent
switches** today (the category-scoped staffing replaces them — and is the fix for the flaky-grounding
root cause).

---

## 5. GAPS, RANKED

> **Reconcile with the §2 LANDED banner.** Items 1–2 (Capability as the relevance entry that captures
> Context + produces a workflow; Context as the live 3-layer object) have **landed default-ON** — they are
> no longer gaps. The still-open gaps are **3–8** below (SubAgent-owns-category-scoped-tools, Eval-as-object,
> per-registry self-improvement, Context prior, per-instance/per-registry eval, the single mint path),
> plus multi-level compression (§8 OPT-5).

1. **Capability** as the relevance entry that captures context + produces a workflow — fully new.
2. **Context** as a 3-layer instance-only object (compressed-branch snapshot) + multi-level compression.
3. **SubAgent owns category-scoped tools+skills** (replaces operator tool-inheritance + the 3 hardcoded switches).
4. **Eval as an object** (measuring stick = mintable reference; measurement = instance).
5. **Per-registry self-improvement loop** (refine references from instance measurements) — uniform.
6. **Context prior** (learned pattern → knowledge + operator/capability).
7. **Per-instance / per-reference eval**; **per-registry benchmark**.
8. **Unified registry-of-registries / single mint path** — partially seeded (`resolve.Registry[T]` + ledger).

---

## 6. DECISIONS

### Resolved
- **Entry object = Capability** (Option C): a relevance-pulled unifying object that captures context
  and produces a seeded-or-synthesized workflow over subagents. Beats a self-triggering workflow
  (can't be the entry for an on-the-fly workflow) and a thin trigger (Capability subsumes it).
- **Operator = the in-workflow step/move** (conventional). It does **not** trigger or own tools.
- **Two axes:** Operator (move) × Primitive subagent (worker) — separate, separate registries.
- **Silent injection preserved:** conscious stays ignorant; the subconscious reaches up by relevance and
  re-voices results through the hidden seam. The Capability machinery never surfaces; across the watched
  seam the conscious authors world-change and perceives reality (watched-seam contract: 03 §4.1 / 04 §4.1).
- **"Specialist" retired** → **primitive subagent**; the 8 faculties are kept (rename, don't delete).
- **Context = instance-only**, three layers, captured by the Capability.
- **Context isn't minted; the context prior is** (the learnable lesson).
- **Eval-as-object (fork #2):** the minted object is the **measuring stick**; measurements are instances.
- **Reference-eval = benchmark (absolute)**, **instance-eval = measure (comparative) → reference
  refinement**, applied **as fit, not forced**.
- **Self-improvement is a required feature of every registry.**
- **SubAgent uses tools + skills by category** (part of its definition) — "subagent-with-skills" is not a
  separate object.
- **Categories are a growable registry** (a shared `Category` object type, facet-discriminated), seeded
  with the starter sets and **mintable + refinable** — not a hardcoded enum (§3.10a).
- **Protected core (immutable kernel) — to be implemented** (§2.8): durability invariants + hidden seam +
  Filter + eval gate & its core sticks are **read-only to the system** (anti-wireheading).
- **Self-access ladder L1–L4** (§2.8): L1 refine refs · L2 mint refs · L3 self-docs · L4 self-code,
  served by the reflexive meta-Capability, gated by the protected core.

### Locked this pass
- **Entry object name = `Capability`** (final).
- **Seed categories** ship as **tool operation `{inspect, mutate, execute}` × reach `{self, local,
  external}`** (the action gate routes on both, 03 §2/§3) + skills `{reasoning, analysis, synthesis,
  verification}`; the registry grows/refines from there. (Two-axis supersedes the earlier flat tool set;
  taxonomy owner = §3.10a.)
- **Compression: binary (min/max) now**; add one level at a time, **benchmark-gated, as an optimization**
  (§8 OPT-5).

### Skill model (locked 2026-06-14)
- **A skill is a PROMPT, not an operator program.** This removes the skill↔Workflow conflation
  (`Skill.Body = Program`); "Program" now denotes **Workflow's substrate only**.
- **Two tiers kept by name:** **unit skill** (one prompt) and **high-level skill** (a prompt that **calls
  sub-skills**). Composition is **in-prompt, resolved by the worker at run time** — bounded **acyclic,
  depth ≤ `MaxSkillDepth=3`, and ≤ `MaxSkillCalls`** (a concrete per-SubAgent cap, §3.8; truncate-to-floor
  on either bound), re-grounding at runtime the static bound the old build-time `Expand` gave.
- **Hooks fold INTO the Skill definition** (no separate bundle object): a Skill =
  `{prompt + hooks + tool-scope + paired knowledge}`. Hooks = the deterministic pre/post control
  (Pattern A/C) around the Pattern-B prompt.
- **Goal-matching is NOT a skill property** — the **Capability** is the only goal-matcher; the
  "goal-matched skill" framing is retired.
- **Skills live under the SubAgent**, invoked **by category** (a worker calls skills; it does not match
  goals or define structure).
- **Skill tier follows `Goal.Level`, set at the Capability** (§3.8a) — *not* a goal→skill match (a worker
  invokes skills by category; only the Capability reads the goal). The Capability sets the skill-tier
  **ceiling** from `Goal.Level` (coarse → admit high-level; leaf → unit); the worker **picks** within it
  (§3.3a). A **miss is feasibility feedback**: the conscious **decomposes the goal**, a Capability
  **re-fires at the finer subgoal** with a lower ceiling — the tier descends *because the goal did*, routed
  through the Capability, **no layer crossed**.

### Scope model (locked 2026-06-15)
- **Scope is two-part with opposite timing** (§3.3a), the constraint twin of Context: a **ceiling** (eager,
  set by the Capability — the bounded least-privilege authority; §2.7) and a **pick** (lazy, resolved by
  `Resolve[T]` at the layer that knows — operators at assembly, skills/tools/workers at staffing; §8 OPT-2).
  **Eager = the bound (safety); lazy = the pick (relevance).**
- **A worker may never widen its ceiling at runtime** — only an explicit gate can. The ceiling is the run's
  auditable authority (ties to the protected core, §2.8).
- **Scope shape is open — a per-object-type facet** `{ceiling-rule, pick-rule}` registered through the
  uniform `resolve.Registry[T]`; a new object type brings its own facet and the Capability never changes
  (same openness as Categories §3.10a).
- **Supersedes the earlier "skill scope handed to the Workflow"** framing: skill scope is just the skill
  facet — ceiling at the Capability, pick at the SubAgent.

### Still open
- **Capability↔goal handshake** — how the active goal + `Goal.Level` reach the Capability (§3.3, §3.8a)
  is owned by 02-conscious and still open *there* (02 "Open questions"); the subconscious model depends on
  it but does not define it.
- **Numeric caps (dev-tuned, not set):** the worker `MaxSkillCalls` budget (§3.8) and the category
  reference-eval thresholds (§3.10a).
- The object model itself is locked — these are **bindings to neighbouring docs**, not gaps in the model.
  Next big topic: how the conscious actually drives (02).

### Future direction — metacognition / reflexive self-optimization (core confirmed → §2.8; rest later)
Metacognition **fits the model without breaking the hidden seam**: it is a **reflexive Capability**
(relevance = "the stream is about the system itself") whose subagents read the self-documentation and
inspect the registries, with the result **re-voiced** as the conscious's own insight. The conscious
*experiences* thinking-about-its-thinking without directly touching the subconscious — silent injection
holds. **The real gap it exposes: a PROTECTED CORE / immutable kernel.** With level-3/4 self-improvement
(change structure/code) the system must be unable to optimize away (1) the durability invariants
(`n<1`, bounded fan-out, regulator), (2) the hidden-seam discipline, (3) the Filter (honesty), and
(4) the **eval gate + its core measuring sticks** (anti-**wireheading** — it must not be able to edit its
own stick so bad changes pass). Self-modification rides the same eval + keep-or-revert; the core +
its sticks are **read-only to the system**. Self-docs become **L3 knowledge** for the meta-Capability
(requirement: the **map must match the territory**). Substrate is closer than it looks (self-managing
thought graph, Controller executive, convertibility, introspectable registries, self-docs); missing =
protected core + reflexive meta-Capability + eval-gated structural self-mod.

---

## 7. WORK ORDER

1. Lock naming (entry object) + category taxonomies (§6 open).
2. **Capability** object: relevance entry + context capture + workflow seed/synth + the **Scope ceiling**
   (§3.3a, eager; picks resolve lazily at each layer via `Resolve[T]`).
3. **Context** object: L1 compressed-branch snapshot (+ IDs) replacing the ≤5 slice.
4. **SubAgent** category-scoped tools+skills (retire the 3 hardcoded switches) + **primitive subagent**
   rename/reposition.
   - **4a. Skill reframe** (§3.8): `Skill.Body Program` → **prompt + sub-skill refs**; replace build-time
     `Expand`-into-operators with a **runtime sub-skill resolver** that keeps the acyclic/depth-3 guard;
     **fold** hooks + tool-scope + knowledge into the Skill definition; **drop goal-matching** (it moves to
     Capability). Worker invokes skills by category.
5. **Operator** slimmed to the move (drop tool-ownership + trigger role).
6. **Eval** as a measuring-stick object + the per-registry self-improvement loop.
7. (later) Context prior; multi-level compression; the §8 optimizations.

> Retire "specialist" across doc + code + test as part of step 4 (one set of truth).

---

## 8. OPTIMIZATIONS — bounded flexibility (DEFERRED, not core)

> **Status: OPTIMIZATION.** Layered on the bounded base once it works.

**Principle:** keep the program **shape** fixed (durability holds); make the slot **contents**
late-bound and updatable. Bounded growth = **late binding + re-concretization**, never appending.

- **OPT-1 (primary) — per-iteration re-concretization.** Keep the operator (move) fixed; update the
  **Context** each loop pass so the move re-concretizes. Turns the bounded `Loop` into a refinement
  loop (measure → update context → re-concretize → repeat), early-exiting via `SkipLoopIfSatisfied`
  (`workflow.go:270`). Bounded by `MaxIter` (≤6).
- **OPT-2 (complement) — late-bound placeholder slots.** A program slot defers *which* worker fills it
  until runtime, resolved via `resolve.Resolve[T]`. Bounded — slot *count* is fixed.
- **OPT-3 (context) — on-demand traversal.** A worker expands a compressed L1 node mid-fire when it
  needs detail compressed away. (Compressed-branch-as-context is core; live traversal is the opt.)
- **OPT-4 (context) — pre-attached RAG knowledge index (L3).** Pre-attach a knowledge index to a
  definition vs the reactive sourcing-ladder pull.

- **OPT-5 (context) — incremental compression levels.** Ship **binary (min/max)** compression first
  (today's `EXPANDED`/`COMPRESSED`, `types/enums.go:77`); then add **one intermediate level at a time**,
  each addition **benchmark-gated** (keep it only if it measurably improves L1 scaling). Multi-level
  compression is the L1 scaling lever, but it is an **optimization layered on the binary baseline**, not
  a day-one requirement.

**Avoid (breaks the model / durability):**
- **Appending phases at runtime** — unbounded; defeats `VerifyProgram` + the regulator.
- **Swapping the operator (the move) itself mid-loop** — keep the move stable; evolve the **context**.
