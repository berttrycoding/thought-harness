# Silent-Injection Cognition — Full System Specification

*A complete systems-engineering spec of the architecture: subsystems, components, states, interfaces, processes, scenarios, and full combinatorial coverage. Companion to the narrative architecture doc (v5) and the pseudocode spec.*

> Layer names are Subconscious / Conscious / Action (originally framed as BACK / MIDDLE / FRONT in the v4 design; see `docs/reference/architecture-glossary.md` §0).

> **Spec rev: adds (a) the Critic split into FILTER (admission, on the intake/hidden seam, validating raw returns *before* transform) + CONTROLLER (executive, graph-level, in Conscious); (b) USER_INPUT modeled via an inbound Interaction Port that interrupts and resets goal/value; (c) a system lifecycle state machine with IDLE, partial-idle, suspended, and terminal stopping states, plus the quiescence condition and idle-time consolidation.**

---

## Table of contents

1. System overview
2. Core concepts & glossary
3. The operation trichotomy (foundational)
4. Subsystem catalog (responsibility · components · states · interfaces)
5. Component state machines
6. Interface registry
7. Process flows
8. Scenarios & worked examples
9. Combination & coverage matrices
10. Scope, assumptions, failure modes
11. Contrast with current harness engineering

---

## 1. System overview

### 1.1 Purpose
A pre-AGI cognitive architecture in which **retrieval and internal computation run silently and automatically**, **the conscious stream manages itself as a navigable thought graph**, and **only world-changing action is explicit and monitored**. The design target is *thinking* — the ability to operate on one's own reasoning — not merely an agent loop.

### 1.2 Central thesis
- Cognition is a **closed recombination engine** (recombines what is already known) with **one deliberate opening to reality** (effectful action) that imports ground truth the engine cannot manufacture.
- Capability **migrates from effortful→automatic** with practice (convertibility).
- Intelligence concentrates in two organs: the **Gate** (what surfaces) and the **Critic** (trust / stop / act) — both fed by a single cross-cutting **value signal**.

### 1.3 Top-level structure
```
        ┌──────── Interaction Port (inbound USER_INPUT, can interrupt) ───────┐
        ▼                                                                     │
   Action      — action layer    conscious · effectful · reality-facing        │
     ▲  WATCHED SEAM (intention out ▼ / reality in ▲) — visible by design     │
   Conscious   — main session     the thought graph · the self-narrative ──────┘
     │      └─ CONTROLLER (executive: exhaustion · decide-next · stopping)
     ▲  HIDDEN SEAM:  FILTER (admit raw) → GATE (arbitrate) → TRANSFORM (re-voice)
   Subconscious — hidden engine   silent specialists · dispatch · workflow/operators
```
*Speaking to the user = an Action (outbound, watched). Hearing from the user = inbound USER_INPUT (Interaction Port). The FILTER validates raw returns before TRANSFORM voices them.*

---

## 2. Core concepts & glossary

| Term | Definition |
|---|---|
| **Thought** | An indexed, addressable node in the graph; carries text, source, confidence, edges. |
| **Branch** | An ordered path of thoughts; one is ACTIVE/EXPANDED, others STASHED/COMPRESSED. |
| **Specialist** | A silent sub-agent bound to one domain; fires on relevance, returns a raw payload. |
| **Operator** | An abstract, domain-general transform (decompose, validate, compare, …). |
| **Workflow** | A learned, phased sequence of operators (e.g. decompose→generate→validate). |
| **Injection** | A specialist return, re-voiced through the hidden seam, arriving as the next thought. |
| **Gate** | Arbiter that selects which candidate surfaces and forks the losers. |
| **Filter** | Admission layer on intake/hidden seam; validates a *raw* candidate before it is voiced; admit/reject/flag. |
| **Controller** | Executive in Conscious: graph-level exhaustion detection, decide-next, stopping, quiescence, interrupt handling. (The Critic, split: Filter = admission, Controller = executive.) |
| **Interaction Port** | Inbound channel delivering USER_INPUT; can interrupt; (re)sets goal and value. |
| **Idle / quiescence** | System state with no goal, no pending input, no outstanding action, no high-value stashed branch. Idle time is when convertibility consolidates. |
| **Critic** | Validator/controller: trust, stop, branch-exhaustion, loop-exhaustion→act. |
| **Thought MCP** | The deliberate metacognitive interface (branch/merge/rerank/compress/expand/focus). |
| **Hidden seam** | Subconscious↔Conscious interface; select + transform; invisible by design. |
| **Watched seam** | Conscious↔Action interface; intention out, reality in; monitored by design. |
| **Convertibility** | Migration of a capability from effortful (generated/metacog) to automatic (injected/workflow). |
| **Value signal** | The single credit/priority signal feeding rerank, validation, act-threshold, and compilation. |

---

## 3. The operation trichotomy (foundational)

Every operation in the system is exactly one of three types. This is the axis the whole design rests on.

| # | Operation type | Layer | Conscious? | Effectful (world)? | Cost | Transparency | Convertible target? |
|---|---|---|---|---|---|---|---|
| 1 | **Injection** | Subconscious→Conscious | No (silent) | No | Cheap | Hidden seam | *Is* the automatic target |
| 2 | **Thought-MCP (metacog)** | within Conscious | Yes (deliberate) | No (internal only) | Medium | Internal | Yes → compiles to Gate habit/workflow |
| 3 | **Action** | Conscious→Action | Yes (deliberate) | **Yes** | Expensive | Watched seam | **No — never** |

**Key invariant:** effectfulness, not cognitive type, decides transparency. Internal compute (even hard math) can be injection-type (silent); only world-changing operations are action-type (watched). Action is the *only* type that can never be converted to silent, because its purpose is to catch information from outside the closed system.

### 3.1 The implementation trichotomy — which ENGINE drives a decision (the three-pattern model)

Orthogonal to the *operation* trichotomy above (which decides transparency) is the **implementation** trichotomy: for each decision, *what engine produces it* — pure math, the model, or a hybrid. This is a property of the **role**, fixed at design time, not chosen at runtime. (Built and enforced in the Go implementation: the Pattern-A CONTROL math lives in `internal/control`, the CONTENT roles behind `internal/backends`, and the Pattern-C escalations as optional backend capabilities; the term realignment is in `docs/reference/architecture-glossary.md`.)

| Pattern | What | Model? | On model failure | Roles |
|---|---|---|---|---|
| **A — pure CONTROL** | closed-form math over well-understood signals | never | n/a (no model) | Gate.Rank · Value V(s) · Scheduler · Regulator · sourcing-ladder order · the Controller **structural** moves (conflict→BRANCH, flag→verify, dry→BACKTRACK, converge→MERGE, fact-based STOP) |
| **B — pure CONTENT** | natural-language generation/understanding the model must own | always | **surface the gap** (return `""`; the caller shows the raw/honest surface) — **NEVER** a deterministic substitute | Generate · Transform · Summarize · Respond · OperatorApply · Specialist · concretize-fuse |
| **C — hybrid ESCALATION** | a deterministic FLOOR always decides; the model is an optional **ceiling** escalated ONLY on a deterministically-flagged fuzzy case | conditional | the **floor stands** (a correct, cheaper answer) and the non-escalation is **surfaced** via `escalation.floor_stands` (never silent) | the Controller threshold judgments (goal-met? line-spent?) · the Filter admission (`ScoreAdmit` floor + `JudgeAdmission` ceiling) · Intention · SynthesizeProgram |

**The litmus (escalation ≠ fallback):** if removing the model leaves a *correct* (if cheaper) answer, it is escalation (Pattern C); if removing it leaves a *lie*, it is a banned fallback (so Pattern B has no floor — the floor would be the lie). The model may **never** override a structural fact (a Pattern-A move, or a `refuted_by_reality`/`contradicts_belief` REJECT). The CONTROL math lives in a leaf module that **never** references the test double; the production control path never reaches through the test backend.

---

## 4. Subsystem catalog

Each subsystem is defined by **responsibility · components · states · interfaces (in/out)**.

### 4.1 Subconscious — Hidden Engine
- **Responsibility:** silently produce candidate thoughts by recombining the known; run workflow control flow.
- **Components:** `Specialist[]`, `Dispatch`, `Workflow`/`Operator` system.
- **States:** `IDLE` (nothing relevant) · `FIRING` (≥1 specialist active) · `WORKFLOW_PHASE_k` (sequencing).
- **Interfaces:**
  - *in* `read(active_context)` — read-only subscription to Conscious's active branch.
  - *out* `raw_returns[]` → Hidden Seam.
  - *internal* `Workflow.instantiate(phase) → Specialist` (runtime-defined sub-agents).

### 4.2 Conscious — Main Session (Thought Graph)
- **Responsibility:** hold the self-narrative as a graph; convert parallel potential into a serial focused stream; expose the Thought MCP.
- **Components:** `ThoughtGraph`, `ThoughtMCP`, serial `generate()` loop.
- **States:** `FOCUSED` (one expanded branch) · `BRANCHING` · `SWITCHING` (compress↔expand) · `MERGING` · `GENERATING` (effortful).
- **Interfaces:**
  - *in* injected thought (from Hidden Seam); observation (from Watched Seam).
  - *out* `active_context` (to Subconscious); `intention` (to Watched Seam).
  - *self* Thought MCP ops (branch/merge/rerank/compress/expand/focus).

### 4.3 Action — Action Layer (Actuator)
- **Responsibility:** execute effectful intentions on the world and surface the result as ground truth.
- **Components:** `Actuator`, external tool/effector bindings (IDE, shell, send, measure).
- **States:** `IDLE` · `EXECUTING` · `AWAITING_FEEDBACK` · `RETURNED`.
- **Interfaces:**
  - *in* `act(intention)` (from Watched Seam).
  - *out* `Observation` (back through Watched Seam, monitored).

### 4.4 Hidden Seam (Subconscious↔Conscious interface)
- **Responsibility:** make engine returns indistinguishable from the session's own thoughts.
- **Components:** `Filter.admit`, `Gate.select`, `transform_to_narrative`.
- **Pipeline:** `FILTER (admit raw) → GATE (arbitrate survivors) → TRANSFORM (re-voice)`.
- **States:** `RELAYING` · `PASS_THROUGH` (no returns → Conscious generates instead).
- **Interfaces:** *in* `raw_returns[]`, `history`, `gate_bias`, `value`; *out* one `Thought(source=INJECTED)` re-voiced.
- **Invariant:** validation happens on the *raw* candidate, before voicing; output reads as a seamless continuation; raw payload retained only for trace-back.

### 4.5 Watched Seam (Conscious↔Action interface)
- **Responsibility:** carry intention to the world and catch reality's return in the open.
- **Components:** `open_to_reality`.
- **States:** `OUTBOUND` (intention) · `MONITORING` · `INBOUND` (observation).
- **Interfaces:** *in* `intention`; *out* `Thought(source=OBSERVATION)`.
- **Invariant:** never silent; must be consciously monitored.

### 4.6 Gate (arbiter)
- **Responsibility:** decide what surfaces; fork rather than discard.
- **States:** `ARBITRATING` · `FORKING` (conflict) · `SINGLETON` (one candidate).
- **Interfaces:** *in* `candidates[]`, `history`, `bias`; *out* `winner`, plus `branch()` side-effect for losers.

### 4.7 Filter (admission) — *the Critic, half 1*
- **Responsibility:** decide whether a candidate thought earns a place in the stream; assign confidence. Validates the **raw** candidate *before* transform voices it (kills laundered hallucination at source).
- **Position:** on the intake. For injections it sits on the hidden seam **before** the Gate; it also *scores* GENERATED candidates, OBSERVATIONS, and USER_INPUT.
- **Honest scope (admit/reject is NOT total coverage).** The admit/REJECT decision is *enforced* only on what crosses **into** the conscious stream from elsewhere — the SUBCONSCIOUS-INJECTION path (the hidden seam's Relay: a rejected candidate never becomes a survivor) and the awake default-mode generator (gated on `admit?`). The CONSCIOUS layer's **own** effortful thought (`engine.generate`) is scored by the same Filter, but only the **confidence** is consumed — the thought is voiced *regardless* of the verdict, i.e. it is trusted at the level a conventional harness trusts raw model output. So "kills laundered hallucination" applies to the *injection membrane*, not to consciousness's self-authored next thought. (Impl: `internal/seams/hidden.go` Relay vs `internal/engine/reactive.go` `generate`.)
- **States:** `SCORING` · `ADMIT` · `REJECT` · `FLAG` (admit but low-confidence → likely to trigger BRANCH/ACT).
- **Interfaces:** *in* `raw_candidate`, `history`, `value`; *out* `confidence`, `admit?`.
- **Intake pipeline (injection):** `FILTER (score/admit) → GATE (arbitrate survivors; conflict → fork) → TRANSFORM (re-voice admitted)`.

### 4.7b Controller (executive) — *the Critic, half 2*
- **Responsibility:** reason over the *whole graph*: branch-exhaustion, loop-exhaustion, decide-next, stopping, quiescence, and interrupt handling for USER_INPUT.
- **Lives in:** Conscious (not on the seam — it needs the graph, not a single return).
- **States:** `DECIDING` · `EXHAUSTED_BRANCH` · `EXHAUSTED_LOOP` · `QUIESCENT` · `INTERRUPTED`.
- **Interfaces:** *in* `graph`, `admitted_thought`, `pending_input?`, `outstanding_action?`; *out* `Decision`, lifecycle transition.

### 4.8 Thought MCP (metacognitive interface)
- **Responsibility:** expose deliberate graph operations to the main model.
- **Operations:** `branch · merge · rerank · compress · expand · focus`.
- **Note:** every op can also be invoked automatically by the Gate; deliberate→automatic is convertibility.

### 4.9 Workflow / Operator System
- **Responsibility:** recognize a known thinking shape and auto-sequence phases, biasing the Gate.
- **States:** `DORMANT` · `RECOGNIZED` · `PHASE_k` · `COMPLETE`.
- **Interfaces:** *in* `context`; *out* `phase`, `gate_bias`, runtime `Specialist`.

### 4.10 Convertibility / Learning Subsystem (cross-episode)
- **Responsibility:** compile repeated effortful patterns into cheap automatic ones.
- **Transitions:** `GENERATED pattern → Specialist`; `METACOG sequence → Workflow / Gate habit`.
- **Interfaces:** *in* episode traces; *out* new specialists, workflows, gate priors.

### 4.11 Value Signal (cross-cutting organ)
- **Responsibility:** single credit/priority signal consumed by four sites.
- **Consumers:** `Branch.value` (rerank) · `Filter.admit` (trust) · `Controller.loop_exhausted` (act-threshold) · convertibility (what to compile).

### 4.12 Interaction Port (external inbound)
- **Responsibility:** receive unsolicited external input (USER_INPUT) and deliver it into Conscious as a high-salience thought; raise an interrupt.
- **Components:** inbound queue, interrupt signal, goal/value re-seeding.
- **States:** `LISTENING` · `DELIVERING` · `INTERRUPTING`.
- **Interfaces:** *in* external `message`; *out* `Thought(source=USER_INPUT)` via the Filter; `interrupt` to Controller.
- **Symmetry:** inbound = USER_INPUT here; outbound (responding to the user) = an **action** (Action layer) across the watched seam. Hearing is input; speaking is effectful.
- **Interrupt policy:** on arrival, Controller compresses the active branch (SUSPENDED), opens/*focuses* a branch for the input, and re-seeds the value signal toward the new goal.

---

## 5. Component state machines

### 5.1 Branch — states × resolution
| State | Allowed resolution | Meaning | Transitions out |
|---|---|---|---|
| ACTIVE | EXPANDED only | the one branch in focus | → STASHED (focus switch), → DEAD (pruned), → MERGED |
| STASHED | COMPRESSED only | retained fork, faded to gist | → ACTIVE (expand+focus), → DEAD, → MERGED |
| DEAD | COMPRESSED | abandoned, kept for trace | (terminal) |
| MERGED | — | folded into another branch | (terminal) |

**Invariant:** at most one ACTIVE/EXPANDED branch at any time (bounded focus).

### 5.2 Thought source — lifecycle
| Source | Created by | Seam crossed | Validated? | Compressible? |
|---|---|---|---|---|
| INJECTED | specialist + transform | hidden | yes | yes |
| GENERATED | Conscious serial loop | none | yes | yes |
| OBSERVATION | Action return | watched | yes (high prior) | yes |
| USER_INPUT | Interaction Port (external) | inbound port | yes (parsed/interpreted by Filter) | yes |
| METACOG | Thought-MCP op | none | n/a (structural) | yes |

### 5.3 Decision (Critic output) — precondition → effect
| Decision | Precondition | Graph effect | Subsystem invoked | Next state |
|---|---|---|---|---|
| THINK | candidate accepted, branch viable | append to active branch | Conscious | FOCUSED |
| BRANCH | conflicting/divergent candidates | fork; winner active, losers compressed | Gate / MCP | BRANCHING |
| MERGE | two branches equivalent/complementary | combine into one | MCP | MERGING |
| BACKTRACK | branch exhausted, loop NOT exhausted | focus best stashed sibling | MCP.rerank+focus | SWITCHING |
| ACT | **loop exhausted** (no internal options) | append OBSERVATION after reality return | Watched Seam / Action | AWAITING_FEEDBACK |
| STOP | goal satisfied / give-up threshold | finalize | — | DONE |

### 5.4 Workflow phase progression
`DORMANT → (recognize) → RECOGNIZED → PHASE_1 → … → PHASE_n → COMPLETE`, each phase emitting a `gate_bias` and possibly an ephemeral specialist.

### 5.5 System lifecycle state machine (idle, suspended, stopping)
The thinking loop is wrapped in a lifecycle. Stopping is **terminal** (done) or **suspended** (resumable).

| State | Meaning | Entered when | Exits to |
|---|---|---|---|
| `IDLE` | no goal, nothing pending; background consolidation may run | quiescence holds | `ACTIVE` on USER_INPUT |
| `ACTIVE` | thinking loop running | input arrives / resume | any state below |
| `AWAITING_REALITY` | suspended on an Action's feedback | Decision ACT issued | `ACTIVE` on OBSERVATION |
| `AWAITING_USER` | turn handed back to the user | STOP(handoff) / needs info | `ACTIVE` on USER_INPUT |
| `SUSPENDED` | paused mid-task (interrupt / budget cap) | interrupt, resource limit | `ACTIVE` on resume |
| `DONE` | episode terminal | STOP(goal-met / give-up) | `IDLE` |

**Quiescence condition (how IDLE is reached):**
`IDLE  ⇔  (goal resolved ∨ abandoned) ∧ ¬pending_input ∧ ¬outstanding_action ∧ max(stashed.value) < UNPROMPTED_PURSUIT_θ`

**Partial idle:** each subsystem carries an idle flag. Composite rules:
| Conscious | Action | Subconscious | Pending input | Composite |
|---|---|---|---|---|
| idle | idle | idle | no | **FULLY IDLE** |
| idle | idle | consolidating | no | **IDLE (background consolidating)** |
| idle | executing | — | no | `AWAITING_REALITY` |
| idle | idle | idle | yes | transitioning → `ACTIVE` |
| busy | — | — | — | `ACTIVE` |

**Stopping-state taxonomy:**
| Stop kind | Trigger | Terminal/suspended | Lifecycle target |
|---|---|---|---|
| GOAL_MET | goal satisfied | terminal | DONE→IDLE (or AWAITING_USER if conversational) |
| GIVE_UP | loop exhausted + over budget | terminal | DONE→IDLE / AWAITING_USER |
| BLOCKED_REALITY | action feedback outstanding | suspended | AWAITING_REALITY |
| BLOCKED_USER | needs user info | suspended | AWAITING_USER |
| INTERRUPTED | USER_INPUT preempts | suspended | SUSPENDED → ACTIVE (new goal) |

**Idle-time consolidation:** in IDLE the foreground loop is not competing for the workspace, so the **convertibility subsystem runs** (§4.10) — compiling repeated patterns into specialists/workflows. This is the architecture's rest/sleep analogue: "fully idle" is rarely truly idle, it is foreground-quiescent + background-consolidating.

### 5.6 Operating regimes: reactive (episodic) vs continuous (awake)
§5.5 as written is **reactive/episodic**: the system waits in IDLE until prompted, and ACT *blocks* the think loop until feedback returns. That models an assistant, not wakefulness. **Continuous (awake) mode** is the more general regime; reactive mode is its special case.

**Continuous mode = the superset.** A self-sustaining loop that perceives, thinks, and acts every tick with no external trigger. Requires these additions:

| Added component | Role | States | Interfaces |
|---|---|---|---|
| **Arousal** | scalar driving loop rate + wake/sleep; replaces binary idle/active | `AWAKE` · `DROWSY` · `ASLEEP` | *out* tick rate, perception gain |
| **Drives / Endogenous goals** | supply goals when none are given (curiosity, unfinished high-value branches, maintenance) | `SATED` · `ACTIVE_DRIVE` | *out* goal + value into the value signal |
| **Default-mode generator** | spontaneous associative firing when no task/drive dominates (mind-wandering); a spontaneous thought can *become* a goal | `WANDERING` | reads internal state → candidate thoughts |
| **Perception Port** (generalizes Interaction Port §4.12) | always-on afferent stream; percepts (incl. USER_INPUT) arrive unsolicited and compete for attention | `STREAMING` | *out* `Thought(source=PERCEPT/USER_INPUT)` via Filter |
| **Async action** (non-blocking watched seam) | fire an action and *keep thinking*; feedback returns later as a percept | `OUTSTANDING` (tracked, not blocking) | *out* intention; *in* async OBSERVATION |

**Arousal-driven lifecycle (supersedes the binary):**
```
   ASLEEP  ──(arousal↑ / salient percept)──▶  AWAKE  ──(continuous perceive·think·act)──▶
     ▲                                          │
     └──────────(arousal↓ / quiescence)─────────┘
   ASLEEP = consolidation dominates (convertibility); "dreaming" = replaying compressed branches
   DROWSY = slowed loop, dampened perception, more spontaneous / less goal-directed
```

**Reactive mode = continuous mode with:** drives off, default-mode off, and **arousal coupled to input arrival** (AWAKE only while an external task is open; ASLEEP/IDLE the instant it resolves). ACT may run blocking in reactive mode; in continuous mode it is async by default.

**Regulation requirement (the cost of being awake):** a self-driven loop needs **homeostatic control** — arousal regulation, drive satiation, attention bounds — or it thrashes on one drive or spins forever. Reactive mode borrows this regulation from the user (prompt starts, answer stops); continuous mode must supply its own. This is also where the autonomy/safety surface concentrates.

| Property | Reactive (episodic) | Continuous (awake) |
|---|---|---|
| Loop trigger | external prompt | self-sustaining tick |
| Goals | exogenous only | endogenous + exogenous |
| Between tasks | IDLE (consolidate) | mind-wandering / drive-pursuit |
| Perception | solicited (input + action feedback) | always-on stream |
| Action | may block (AWAITING_REALITY) | async, non-blocking |
| Regulation | supplied by the user | self-supplied (homeostatic) |
| Relationship | **special case** | **general case** |

---

## 6. Interface registry

| Interface | From → To | Direction | Payload | Transparency | Monitored |
|---|---|---|---|---|---|
| `read(active_context)` | Subconscious ← Conscious | pull (read-only) | active branch thoughts | n/a | no |
| `raw_returns` | Subconscious → Hidden Seam | push | list of raw payloads | hidden | no |
| `relay` | Hidden Seam → Conscious | push | 1 re-voiced INJECTED thought | hidden | no |
| `admit` | Filter → (Gate/Conscious) | gate | confidence + admit/reject on raw candidate | hidden/intake | no |
| `message` | external → Interaction Port → Conscious | push (async) | USER_INPUT thought (+ interrupt) | inbound | yes (parsed) |
| `generate` | Conscious → Conscious | self | 1 GENERATED thought | internal | no |
| MCP ops | Conscious → ThoughtGraph | self | graph mutation | internal | no |
| `intention` | Conscious → Watched Seam | push | a single effectful intention | watched | yes |
| `act` | Watched Seam → Action | push | intention | watched | yes |
| `Observation` | Action → Watched Seam → Conscious | push | reality result | watched | **yes** |
| `gate_bias` | Workflow → Gate | push | per-phase priority prior | internal | no |
| `value` | Value Signal → {Gate, Critic, Convertibility} | pull | scalar credit/priority | internal | no |

---

## 7. Process flows

### 7.1 Main loop (per step)
1. Conscious exposes `active_context` (current branch, expanded).
2. Subconscious dispatches: every relevant specialist fires (pull, parallel); workflow may bias/sequence.
3. If candidates **conflict** → Gate auto-`branch`.
4. Hidden Seam: `select` winner → `transform` to narrative → emit INJECTED thought. *If no candidates → Conscious `generate()` (effortful GENERATED thought).*
5. Critic `validate()` sets confidence; append thought (indexed).
6. Critic `decide_next()` → one of {THINK, BRANCH, MERGE, BACKTRACK, ACT, STOP}.
7. Execute the decision (MCP op, or open the Watched Seam for ACT).
8. Advance workflow phase if active. Repeat.

### 7.2 Injection cycle (silent path)
specialist.fire → raw_return → Gate.select → transform_to_narrative → INJECTED thought → validate → append. *No seam visible to Conscious.*

### 7.3 Effortful cycle (generation path)
no specialist fired → Conscious.generate (serial) → candidate → validate → append. *Felt as effort; the convertibility subsystem watches for repeats.*

### 7.4 Branch / backtrack cycle
conflict OR deliberate → MCP.branch → focus winner, compress siblings → … → branch exhausted (loop not) → rerank → focus best sibling → expand.

### 7.5 Action cycle (opening to reality)
loop exhausted → form_intention → Watched Seam.act → Action executes → Observation caught (monitored) → re-enter Conscious as OBSERVATION → validate (high prior) → append.

### 7.6 Convertibility cycle (across episodes)
traces → detect repeated GENERATED pattern → mint Specialist; detect repeated METACOG sequence → mint Workflow / gate prior. Result: next time the same work arrives injected/automatic.

---

## 8. Scenarios & worked examples

### S1 — Fluent recall (injection only)
"What's 7×8?" for an expert. Specialist fires → transform → "56" arrives as own thought. Decision: THINK→STOP. *Exercises:* Subconscious, Hidden Seam, Gate(singleton), Critic(validate).

### S2 — Stuck / effortful (generation)
Child doing unfamiliar long division. No specialist fires → Conscious.generate repeatedly, each validated, low confidence. *Exercises:* generation path, Critic, eventual BACKTRACK or ACT.

### S3 — Conflicting injections (branch)
"Is this refactor safe?" Two specialists return opposite verdicts. Gate detects conflict → auto-branch. One branch expanded, other compressed. *Exercises:* Gate(forking), MCP.branch, Branch states.

### S4 — Exhaustion → backtrack (internal exit)
Current proof strategy stalls but an earlier idea remains. Critic: branch_exhausted, loop NOT exhausted → rerank → focus stashed sibling → expand. *Exercises:* rerank, focus, compress/expand, value signal.

### S5 — Exhaustion → act (external exit)
"Will this code run?" Internal simulation inconclusive. Critic: loop_exhausted → form_intention → Watched Seam → run it → Observation returns error → append → resume. *Exercises:* Watched Seam, Action, OBSERVATION, the opening.

### S6 — Workflow execution (design-build-validate)
"Design a small API." Workflow recognized → PHASE decompose (operator) → PHASE generate (runtime specialist) → PHASE validate (Critic privileged via gate_bias). *Exercises:* Workflow, Operators, runtime instantiation, gate bias.

### S7 — Convertibility (effortful → automatic)
After many S2-style episodes, long division stops being generated and starts arriving injected. *Exercises:* Convertibility subsystem, GENERATED→Specialist transition.

### S8 — Bad transform caught (validation guard)
Transform smooths a *wrong* memory into a confident-sounding thought. Critic.validate assigns low confidence on cross-check → BRANCH to verify or ACT to confirm. *Exercises:* Critic guarding the hidden seam, value signal as trust.

### S9 — Merge (convergent branches)
Two explored branches turn out to be the same insight. Critic→MERGE → MCP.merge. *Exercises:* MERGE decision, Branch MERGED terminal state.

### S10 — Metacognitive deliberate control
Model explicitly decides "let me steelman the opposite" → calls MCP.branch directly (METACOG), not gate-triggered. *Exercises:* Thought MCP as conscious interface; contrast with S3's automatic branch.

### S11 — User interrupt mid-task
Mid-reasoning, a new USER_INPUT arrives. Interaction Port raises interrupt → Controller compresses active branch (SUSPENDED) → focuses a branch for the input → re-seeds value. Later may resume the suspended branch. *Exercises:* Interaction Port, interrupt policy, SUSPENDED state, value re-seed.

### S12 — Reaching idle + consolidation
Goal met, response delivered (Action), no pending input. Quiescence holds → DONE → IDLE. In IDLE, convertibility compiles a repeated pattern from the episode into a specialist. *Exercises:* lifecycle, quiescence, stopping taxonomy, idle-time consolidation.

### S13 — Bad input rejected at intake
USER_INPUT or an injection is malformed/untrustworthy. Filter scores low → REJECT or FLAG before it enters the stream (and before transform voices it). *Exercises:* Filter as admission guard on the intake, validate-before-voice.

### S14 — Continuous mind-wandering → unprompted idea (continuous mode)
No task, no input. AWAKE + above sleep arousal. Default-mode generator fires associatively; a spontaneous thought clears the unprompted-pursuit threshold → becomes a goal → the loop pursues it without any prompt. *Exercises:* Arousal, Drives/endogenous goals, default-mode, value signal, self-direction.

### S15 — Act while thinking (async action, continuous mode)
The loop fires an action (non-blocking) and *keeps thinking* on the same branch; the result returns later as a percept through the Perception Port and is integrated when it arrives. *Exercises:* async watched seam, Perception Port, OUTSTANDING action state, concurrency of think+act.

### S16 — Falling asleep / waking (arousal transitions)
Quiescence + arousal↓ → DROWSY → ASLEEP; consolidation dominates (dreaming = replaying compressed branches). A salient percept or arousal↑ → AWAKE. *Exercises:* arousal-driven lifecycle, consolidation, perception gain.

---

## 9. Combination & coverage matrices

### 9.1 Operation type × layer × seam (full cross-product)
| Operation type | Originating layer | Seam crossed | Conscious | Effectful |
|---|---|---|---|---|
| Injection | Subconscious | hidden | no | no |
| Generation | Conscious | none | partial (felt as effort) | no |
| Metacog (MCP) | Conscious | none | yes | no |
| Action | Action | watched | yes | yes |
| Observation return | Action→Conscious | watched | yes | no (it's the *result*) |

### 9.2 Thought source × resulting decision (which sources lead where)
| Source ↓ / Decision → | THINK | BRANCH | MERGE | BACKTRACK | ACT | STOP |
|---|---|---|---|---|---|---|
| INJECTED | ✓ | ✓ (if conflict) | ✓ | ✓ | ✓ | ✓ |
| GENERATED | ✓ | ✓ | – | ✓ | ✓ | ✓ |
| OBSERVATION | ✓ | ✓ (if surprising) | ✓ | ✓ | ✓ (chain) | ✓ |
| METACOG | ✓ (structural) | self | self | self | – | – |

### 9.3 Exhaustion state × available exit
| Branch exhausted? | Loop exhausted? | Exit taken |
|---|---|---|
| no | no | THINK (continue) |
| yes | no | BACKTRACK (internal) |
| yes | yes | ACT (external) |
| n/a | goal met | STOP |

### 9.4 Seam matrix (both interfaces, all properties)
| Seam | Direction | Transform applied | Monitored | Failure if violated |
|---|---|---|---|---|
| Hidden | Subconscious→Conscious | yes (re-voice) | no | seam shows → reads mechanical |
| Watched | Conscious↔Action | no (raw, by design) | yes | miss feedback → no ground truth |

### 9.5 Convertibility transition table
| From (effortful) | Detector | To (automatic) | Consumed by |
|---|---|---|---|
| repeated GENERATED pattern | pattern frequency × value | new Specialist | Subconscious injection |
| repeated METACOG sequence | sequence frequency × value | Workflow / Gate prior | Workflow, Gate |
| repeated ACT→OBSERVATION pair | reliability of result | cached belief (still re-checkable) | Critic prior |

### 9.6 Value-signal consumer coverage
| Site | Question it answers | Failure without it |
|---|---|---|
| Branch.value (rerank) | which branch to expand next | thrash / wrong revisit |
| Critic.validate | trust this injection? | laundered hallucination |
| Critic.loop_exhausted | think more or act? | infinite loop / premature action |
| Convertibility | compile which pattern? | bloat / compiling noise |

### 9.7 Scenario × subsystem coverage (verifies completeness)
| | Subconscious | Conscious | Action | Hidden | Watched | Gate | Critic | MCP | Workflow | Convert |
|---|---|---|---|---|---|---|---|---|---|---|
| S1 | ✓ | ✓ | | ✓ | | ✓ | ✓ | | | |
| S2 | | ✓ | | | | | ✓ | ✓ | | (feeds) |
| S3 | ✓ | ✓ | | ✓ | | ✓ | ✓ | ✓ | | |
| S4 | | ✓ | | | | | ✓ | ✓ | | |
| S5 | | ✓ | ✓ | | ✓ | | ✓ | | | |
| S6 | ✓ | ✓ | | ✓ | | ✓ | ✓ | | ✓ | |
| S7 | ✓ | ✓ | | | | | | | | ✓ |
| S8 | ✓ | ✓ | | ✓ | | ✓ | ✓ | ✓ | | |
| S9 | | ✓ | | | | | ✓ | ✓ | | |
| S10 | | ✓ | | | | | | ✓ | | |

*Every subsystem appears in ≥2 scenarios; every decision and both seams are exercised.*

---

## 10. Scope, assumptions, failure modes

### 10.1 In scope
Single-agent cognition; one conscious focus; bounded working memory; deliberate and automatic graph control; one external opening; within- and cross-episode learning.

### 10.2 Out of scope (explicit)
Multi-agent social cognition; affect/emotion as a subsystem (could be a future value-signal source); embodiment beyond the generic Action-layer actuator; consciousness claims (the spec is functional, not phenomenal).

### 10.3 Assumptions
- Thoughts are indexable and addressable.
- Compression is lossy but gist-preserving enough for useful revisit.
- A usable value signal exists or can be learned.
- The transform preserves *content* while changing *voice*.

### 10.4 Failure modes (and the guard)
| Failure | Cause | Guard |
|---|---|---|
| Laundered hallucination | transform too smooth, no check | Critic.validate + value signal |
| Narrative break | raw return leaked to Conscious | Hidden Seam transform invariant |
| Missed ground truth | action backgrounded/unmonitored | Watched Seam monitored invariant |
| Thrash | rerank with no value signal | value signal organ |
| Runaway loop | no loop-exhaustion detection | Critic.loop_exhausted → ACT/STOP |
| Compile noise | converting low-value repeats | value-gated convertibility |
| Lost branch | over-compression | resolution policy + trace retention |

---

## 11. Contrast with current harness engineering (summary)

| Dimension | Current harness | This architecture |
|---|---|---|
| Dispatch | push (orchestrator routes) | pull (specialists subscribe, fire on relevance) |
| Orchestrator | explicit, central | none; emergent |
| Retrieval | narrated in-band | silent, pre-conscious |
| Tool returns | dumped raw | transformed to narrative (hidden seam) |
| Seams | uniformly visible | asymmetric: one hidden, one watched |
| Action's role | task execution | importing ground truth the loop can't make |
| Working memory | flat growing context | compressed thought graph, bounded focus |
| Control flow | hardcoded pipeline | workflow recognized & instantiated at runtime |
| Learning | frozen at inference | convertibility: effortful→automatic |
| Locus of intelligence | the model / routing | the Gate + Critic + value signal |
| Metacognition | absent / ad hoc | first-class Thought MCP |

---

## 12. Implementation & Training

How to build this with today's models and compress it over time. The guiding principle is the architecture's own: **migrate from expensive/general to cheap/specialized along the automatic-vs-effortful axis.**

### 12.1 The value signal — formal specification
A learned scalar **V(s)** = expected worth of a state s (thought / branch / frontier / candidate), consumed at the four sites of §9.6. RL formulation:

| Element | Mapping |
|---|---|
| state s | thought-graph context (active branch + frontier) |
| action a | a Thought-MCP op (branch / rerank / expand / backtrack / act) |
| reward r | goal progress; correctness **confirmed by reality** (an OBSERVATION); task completion |
| policy π | the Gate's surfacing decisions |
| value V | the rerank heuristic / the Filter's trust prior |
| credit assignment | TD / backprop-through-graph over the thought graph |

**Hard rule — grounded reward:** r must derive from reality-confirmed outcomes (OBSERVATIONs), **not** from a model grading its own traces. Self-grading distills *confidence*, including confident mistakes. Reality calibrates; recombination cannot (the closed-loop/opening principle, applied to training).

### 12.2 Bootstrap — LLM-propose-and-recommend (zero training)
Before any V is learned, approximate it in-context: **propose K candidate actions, then rank/recommend** (LLM-as-critic). This borrows the pretrained model's judgment as an approximate value function and makes the search non-blind from day one (a serviceable A\* heuristic with no training). It is **plausible, not calibrated** — it can confidently recommend a bad branch and, with no reward loop, never learn otherwise.

### 12.3 Migration — bootstrap → learned
Each grounded outcome (reality confirms/refutes a branch) yields a training pair. Distill the recommender's choices **plus grounded reward** into a cheap learned V. Judgment thus migrates expensive→cheap — *convertibility applied to judgment itself*, the same generated→injected move the architecture makes for skills.

### 12.4 Design option — asymmetric model sizes (tiny Conscious / big resolver)
*Tagged as an option, not baked in.*
- An **ultra-tiny fast model runs Conscious's hot loop**; it is exposed **parameterless tools** — it can only signal *intent* ("search memory", "compute", "act"), not fill arguments.
- When an intent must actually fire, a **bigger model reads the context and resolves the parameters**.
- **Maps to the trichotomy:** parameterless intent = Conscious forming an *intention* (cheap, high-frequency); parameter resolution = the Transform/effector seam-crossing (expensive, rare).
- **Payoff:** lowers per-tick cost, which (per the durability analysis) raises the sustainable thought rate λ̄ = μ/(1−n) for a fixed compute budget.
- **Risk + guard:** a tiny Conscious can form *bad intentions* a big model faithfully executes (garbage-intent-in, faithful-call-out). The **Filter/value signal must gate intent before the big model spends compute** on resolution.
- **The bet:** if Conscious mostly *thinks* rather than *calls tools*, the heavy lifting lives in the Subconscious (the hidden layer), and the conscious loop stays cheap and fast.

### 12.5 Staged deployment — frontier → distilled
| Stage | What | Purpose |
|---|---|---|
| 1 | **All-frontier scaffold:** every component is a frontier-model call | works end-to-end; **harvests traces** (the training set you can't otherwise get) |
| 2 | **Selective RL / distillation per subsystem** (against grounded reward) | compress the parts that convert well |
| 3 | **Compressed system:** small Conscious + distilled specialists + learned V; frontier retained only at rare hard seam-crossings | cheap, fast, durable |

### 12.6 Per-subsystem compression schedule
Convertibility tracks the architecture's own axis: narrow/repeated/automatic-able → small RL'd models; broad/novel/effortful → stay frontier.

| Tier | Subsystems | Why | Method |
|---|---|---|---|
| **Convert first** (best targets) | value signal/rerank · individual specialists · Filter · Transform | narrow, high-frequency, clean reward/objective | RL value fn; distillation; seq2seq |
| **Convert with care** | Gate · Controller | frequent but judgment-heavy; errors expensive | distill last, most data, keep frontier fallback |
| **Keep frontier longest** | Conscious generation on novel problems · big resolver at hard tool-parameterization | broad, open-ended, long-tail/novelty | retain frontier; these resist compression |

**Rule of thumb:** you compress exactly the components that, in cognition, would have migrated effortful→automatic anyway. *The architecture predicts its own compression schedule.*

### 12.7 Training discipline (two non-negotiables)
1. **Grounded reward always.** Bootstrap traces are fine for *initializing* the heuristic; the *correction* signal must be reality-grounded, or you get a faster model with the same blind spots.
2. **Order: Filter + value signal before shrinking Conscious.** A small Conscious on a durable high-frequency stream accumulates small per-step errors over many ticks — precisely the regime where the Critic/Filter and value signal matter most. Shrinking Conscious without a solid Filter is the drift failure case.

---

*End of specification. The **value signal** (§4.11, §9.6) — the single component consumed at four sites and serving as the durable search's heuristic — now has a concrete path: bootstrap via LLM-propose-and-recommend (§12.2), correct via reality-grounded RL (§12.1, §12.3), and distill into a cheap learned function as the system compresses (§12.5–12.6). Existence of the durable regime is established (durability analysis §6); its *efficiency* rests on this value signal being learnable, which §12 argues it is.*
