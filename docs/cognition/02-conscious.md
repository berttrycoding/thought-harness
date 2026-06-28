# Conscious Subsystem — design discussion (WORKING DRAFT)

> Part of the **Cognition Architecture** — see [`00-INDEX.md`](00-INDEX.md).
>
> Status: **WORKING DRAFT.** Developed from the original seed stub during the conscious-pass +
> activity-tuning redesign. The goal mechanism is treated here (no standalone goal doc — the
> subconscious doc §3.10 defers Goal's full treatment to this page). Every claim is tagged
> **DECIDED** (architecture, settled) · **PROPOSED** (this redesign, not yet built) · **CODE-STATE**
> (what the code actually does today, honest). Where they disagree, CODE-STATE is the truth and
> PROPOSED is the target.

## Scope

The **conscious** layer: the thinking session the user/world interacts with. It pursues **Goals**,
manages itself as a **thought graph**, and is driven by the **Controller**. It is **ignorant of the
subconscious** (silent injection) — it reaches the subconscious only by *thinking*, which affords
Capabilities by relevance (see [`01-subconscious.md`](01-subconscious.md) §2.1).

*Out of scope:* the **orchestrator/engine lifecycle** (IDLE / ACTIVE / AWAITING_REALITY / SUSPENDED /
DONE) is the session state machine in `internal/lifecycle`, not a cognitive-architecture concern — this
doc covers the **goal** lifecycle (§1.7), not the engine's. (A future `05-` could give it a home if it
grows.)

The spine of this redesign: **the conscious layer is goal-directed best-first search, and how
*actively* it searches (branch / merge / backtrack rates) is a tunable, learnable control policy —
not a fixed ladder.** The Goal is the setpoint; the value signal is the error-and-heuristic; the
Controller is the policy; the regulator is the stability constraint. It pursues **many goals at once
as a forest** (§1.8), with goalless lines free to **wander**; the same search math runs *within* a
goal (which move) and *across* goals (which to focus).

---

## §0 Landed-status banner (read before the PROPOSED tags below)

> **Much of what §2–§5 tags `PROPOSED`/`NEW`/`CODE-STATE → gap` has since SHIPPED** (this WORKING-DRAFT
> pass predates the build). Where a tag below disagrees with this banner, the banner is current. What is
> now BUILT:
> - **Reenter MCP op** (§2 table, §2b) — built: `internal/graph/mcp.go` `ThoughtMCP.Reenter(target, reason, seed)`.
> - **Episodic time-event timeline** (§2a) — built as a first-class substrate, not just the proto-bus:
>   `internal/timeline/timeline.go`, fed by `internal/engine/timeline_feed.go`.
> - **Soft (softmax) policy + per-move propensities** (§4.2–4.3), **UCB exploration** (§4.4,
>   `internal/search/ucb.go`), the **`conscious.activity.*` KnobFloat set** (§4.5, `internal/config/knobs.go`
>   `KnobFloat`), **REINFORCE β/τ learning** (§5.2) and the **keep-or-revert experiment** (§5.3) — all
>   BUILT in `internal/critic/controller.go` + `internal/config`, but **DEFAULT-OFF / opt-in** (`Soft`,
>   `Learn`, `Experiment` default `false` in `DefaultConsciousActivity`), so the **default path is the hard
>   first-match ladder of §4.1**. Read §4.2–§5 as "built, default-OFF," not "not yet built."
> - **Goal entity + lifecycle + Acceptance** (§1.6–1.7), the **forest / seed-intent portfolio** (§1.8),
>   and the **concrete sensors / power-model / resume / orientation** (§7.1a/§7.3/§7.4/§7.5) are built; the
>   awake/continuous regime they run in stays **default-OFF, `implemented-untested at scale`** (§1.8 HARD
>   DEPENDENCY) — the bounded/reactive build is the default and the awake go-live is a gated product call.
> Still genuinely open: per-source activity profiles, the goal-transition gating, learned-vs-hand-set
> profiles, and the conscience-tier good/bad standard (Open questions, end).

## §1 Goal — the setpoint

**1.1 What a Goal is (DECIDED).** A **Goal** is the conscious objective — the **setpoint** the whole
system regulates toward. Three sources (`cognition/goals.go:20`):

| Source | Origin | Mode |
|---|---|---|
| `USER` | a user turn, across the **perception/intake port** (§7.1) | reactive |
| `DRIVE` | endogenous — a Drive / default-mode goal | awake / continuous |
| `SUBGOAL` | spawned by decomposing a parent goal | either |

A Goal **never invokes a Capability directly** (that would break the one-way mirror, §01 §2.1). It
**shapes the thought stream**; the stream affords Capabilities by relevance. Goals form a **tree**
(subgoals); drives mint endogenous ones.

**1.2 Goal is the control reference (PROPOSED — the framing that unifies this doc).** In
control-theoretic terms the Goal is the reference signal `r`. The thought-graph state is the plant
state `s`. The value signal's `goal_sim` term (§3) is literally **how close the current thought is to
the setpoint** — so `error = 1 − goal_sim`. Everything downstream (which branch wins, when to stop,
how hard to explore) is computed relative to this setpoint. *This is why the goal mechanism cannot be
designed separately from the activity policy — it is the policy's reference input.*

**1.3 Stability — stable, not frozen; graded by level (DECIDED, refined).** The setpoint must hold
still *enough* to regulate toward — but it is **not welded**. A Goal can be **misaligned or
unrealistic**, so it is **revisable under genuine feedback** (§1.9), only through deliberate
**transitions** (`refined` / `superseded` / `abandoned` / `done`, §1.7). Stability is **hysteresis**,
not immutability: it takes a real, persistent signal to move the setpoint — which prevents thrashing
(and keeps the regulator's outer loop stable; a freely-thrashing setpoint can't be tracked). Graded
**by level**: leaf subgoals revise readily (fluid); the top goal moves only on **accumulated,
propagated** feedback (sticky) — the tree is a low-pass filter on revision (§1.9). The learning loop
(§5) still freezes within an open window and updates across transitions.

**1.4 Not all user input is a goal (DECIDED).** Intake classifies each incoming turn:

| Class | Becomes a Goal? | Effect |
|---|---|---|
| **goal** | yes | new setpoint (or subgoal of the active one) |
| **answer-and-done query** | no | one-shot answer, no standing pursuit |
| **context / preference update** | no | writes to memory / person registry |
| **correction** | no (re-aims) | adjusts the active goal or last answer |
| **social** | no | handled by the social faculty (awake) |

**1.5 Two creation paths, one Goal (DECIDED).**
- **Deterministic** — `/goal <text>` (Pattern A): an explicit, unambiguous setpoint.
- **Inferred** — passive (Pattern C): a deterministic **floor** for explicit goal markers + a model
  **ceiling** to infer and classify ambiguous turns. The ceiling **never overrides** an explicit
  `/goal`.

**1.6 The Goal entity (DECIDED for the reimplementation).** We are reimplementing the conscious layer,
so Goal is built as a **real, load-bearing entity from the start** — the setpoint, not a passive log.
The entity:

| Field | Meaning |
|---|---|
| `ID` | stable handle |
| `Text` | the objective statement |
| `Source` | `USER` / `DRIVE` / `SUBGOAL` (§1.1) |
| `Status` | lifecycle state (§1.7) |
| `Parent` | the goal this was decomposed from — nil for a top goal (the **tree** edge) |
| `Children` | subgoals (graded-stability leaves, §1.3) |
| `Acceptance` | the **success predicate** — what "reached" means; the checkable source of `goal_met` (§5.1) |
| `Level` | tree depth — drives graded stability (top stable, leaf fluid) |

**No subconscious pointer — the one-way mirror (DECIDED, corrects the old model).** The Goal holds
**no reference to any subconscious object** — not a Skill, not a Capability. Such a pointer would make
the conscious *aware* of the subconscious and break silent injection (§01 §2.1). The Goal connects to
the machinery **only** by shaping the thought stream: its `Text` (and `Acceptance`) set the relevance
signal the stream carries; the stream then affords a **Capability** by relevance *deep* in the
subconscious, and a worker (SubAgent) there may invoke a **Skill** by category. **Skill is not matched
to the Goal.** The old `Goal → MATCH → Skill` direct link (legacy `goals.go` `Skill *string`, the
pre-redesign skills-and-goals model) collapsed *entry*
(Capability) and *worker capability* (Skill) into one step — this reimplementation drops it. Goal is
**instance-side** (always a concrete objective, never a reusable reference); the matchable references
it indirectly drives are Capabilities (entry) and Skills (worker), both in the subconscious.

The **Acceptance predicate is the new load-bearing piece** the passive record lacked: RL reward (§5)
needs a checkable "is the setpoint reached?" It is **Pattern C** — a deterministic floor (an OK
observation that answers the question; an explicit user confirmation) plus a model ceiling that judges
acceptance only on a flagged-fuzzy case. Without it there is no honest `goal_met`, so it is built
**with** the entity, not after. Acceptance is checked **continuously, not only at the end**, and yields
two outcomes that matter: **met** (→ `done`) *or* **unmeetable** (a constraint / infeasibility surfaced
→ revise the setpoint, §1.9). So Acceptance is both the stop signal *and* a feedback input.

**Authoring the Acceptance predicate (DECIDED) — where it comes from, by source:**

| Source | Acceptance authored by |
|---|---|
| `USER` | **inferred at intake** (Pattern C): a deterministic floor reads explicit success markers in the request ("until tests pass", "return X"); a model ceiling proposes "done when …" for an ambiguous ask; the user may pin it (`/goal … done when …`). If none can be inferred, fall back to **user-confirms-done** — never self-declare done on a fuzzy goal. |
| `DRIVE` | set by the **drive that minted it** — the drive's own satisfaction condition (e.g. curiosity → "until the question resolves"; §7) |
| `SUBGOAL` | **carved from the parent** — a subgoal owns a *slice* of the parent's Acceptance; discharging all children (partially) discharges the parent (§1.9 propagation) |

Authoring is itself **revisable** (§1.9): an Acceptance that proves too strict or too loose is updated
through the same feedback loop. **One product call left open:** whether the system may **self-declare** a
`USER` goal done on a grounded check (e.g. a passing test) or must always **confirm with the user** —
default here: self-declare only when Acceptance has a deterministic / reality-grounded check, else confirm.

**1.7 Goal lifecycle (DECIDED for the reimplementation).** Status is a small state machine; every
transition is a **deliberate setpoint change** — the only points the setpoint moves, and the signal
that **closes a learning window** (§5.2):

```
open ──match──► matched ──pursue──► active ──┬─ done       (Acceptance met)
                                             ├─ abandoned  (gave up / over budget)
                                             ├─ refined    (replaced by a sharper child/sibling)
                                             └─ superseded (a new top goal takes over)
```

A pursuit window is the `active` span; within it the top goal is immutable (§1.3) so the regulator and
the learner see a still target. Leaf subgoals may be minted/abandoned freely inside the window without
disturbing it.

**Legacy note (what we are replacing).** The pre-reimplementation code carried the active goal as a
bare `graph.Goal` *string*, with a `Goal` struct that was a passive logged `Goal → Skill → action`
match (`goals.go:8`: Parent/subgoal unwired, Status never reached DONE). The reimplementation retires
that compromise — Goal is first-class, transition-gated, and tree-structured as above. (Consistent with
the earlier skills-and-goals model, which already named first-class Goal decomposition as the intended
next step.)

**1.8 Multiple goals + goalless wandering — the forest (DECIDED; the G5 frontier).** The conscious
substrate is **not one tree with one goal** — it is a **forest**: multiple root lines, each bound to
its own goal, **plus goalless lines that wander**. The code already anticipates this as **"G5 per-line
goal binding"** (`controller.go:stopOrDeliver` — today's single global goal holds "until per-line goal
binding, G5").

- **Per-branch goal binding (G5).** Each branch (region) carries its own setpoint — or none. So
  `goal_sim` (§3) and `goal_met` (§1.6) are evaluated **against that branch's goal**, not one global
  goal. Value is goal-relative *per branch*.
- **Goalless lines = wandering (default-mode).** A branch with no setpoint has no `goal_sim` term; its
  value comes from **intrinsic** drivers. Wandering is **multi-factor** (an open set — these first):

  | Wandering driver | What it rewards |
  |---|---|
  | **Curiosity / novelty** | thoughts new vs what's been explored |
  | **Coherence** | lines that knit existing pieces together |
  | **Conflict resolution** | reconciling a contradiction (between goals, or belief vs observation) |
  | *(extensible)* | further intrinsic factors as identified |

- **Goal birth from wandering.** A wandering line that develops something worth pursuing **mints a
  `DRIVE` goal** (§1.1) and becomes goal-bound — the endogenous-goal loop: default-mode → discover →
  commit a setpoint.
- **Cross-goal focus — attention with a self-development floor (DECIDED).** The single EXPANDED "CPU"
  is allocated **across** the forest. **User goals take priority**, but self-development is required,
  so a **minimal baseline is reserved for non-user lines** (drives + wandering). That baseline **is**
  the awake regime's **μ > 0 positive-baseline** durability condition (the regulator) — self-development
  is the μ that keeps the awake stream alive, not a nice-to-have:

  ```
  focus(t) = argmax over the forest of  [ priority(goal) · value(branch) + c·explore_bonus ]
             subject to  E[attention to non-user lines] ≥ μ_min   (the self-development floor)
  ```

  Same `softmax(value/τ) + UCB` shape as the within-goal activity policy (§4), applied one level up
  (which root to focus) — so attention and thinking-moves are **one mechanism at two scales** (§4.6).

- **Seed intents — the standing forest roots (PROPOSED; the self-sustaining frontier).** Today the
  forest is seeded **reactively**: a `USER` turn mints a root, or a wandering line mints a `DRIVE` root
  (§7.2). With no user turn and nothing wandering yet, the forest is *empty* — cognition only runs when
  poked. A **proper, complete** cognition is **self-sustaining**: it seeds a small **standing set of
  endogenous root intents at boot**, so the loop has something to think about before any user input.
  The minimal complete seed (one per faculty, mapping to §1.1 sources + the seed-intent brain model in code, `internal/cognition/seedintent.go`):

  | Seed root | Goal source | Faculty | What it keeps alive |
  |---|---|---|---|
  | **Self-introspection** | `DRIVE` (self-development) | introspective | monitors its own state / open threads / what it should improve — carries the μ self-development floor |
  | **Perception** | `DRIVE` (always-on intake) | perceptual | a standing watch on the perception/intake port (§7.1) — notices and admits new reality even unprompted |
  | **User / task** | `USER` (reactive) | actional | the user-directed lane — serve the user; takes priority when present (the existing reactive root) |

  These are the forest's **always-present roots**; user goals and freshly-minted drive goals join the
  same forest and are reranked by the cross-goal focus above (user lanes still take priority; the
  μ-floor guarantees the introspection/perception roots are never fully starved). This is what makes the
  stream **endogenous, not purely user-directed** — the awake regime's `μ > 0` condition realised as
  *named standing intents*, not just a reserved attention fraction.

  **The three above are only the minimal KERNEL; a complete self-sustaining cognition needs a
  ~two-digit portfolio (20 rows).** Three roots keep the loop ticking; they do not make a *complete*
  mind. A mind left alone must keep doing a whole set of standing things to stay coherent, grounded, and
  growing. The complete seed portfolio is exactly the ordered table below (the code's `seedPortfolio`,
  `internal/cognition/seedintent.go`) — each row tagged to **one** of the **six faculties** (perceptual,
  introspective, mnemonic, motivational, actional, validative) and mapped to the mechanism that ALREADY
  backs it (so this is *assembling standing roots*, not inventing faculties). **Order is load-bearing:**
  the kernel-of-3 is rows 1–3, so a prefix of length 3 is exactly the minimal complete seed, and dialling
  the `seed_intent_count` knob up walks down the rest of the portfolio. Each row carries `Source =
  DRIVE` (an endogenous self-development line, never user-directed) and a never-met standing Acceptance
  (a standing watch is never self-declared "done").

  > **Faculty disambiguation (the word is overloaded — two DIFFERENT objects, same word; do not
  > conflate):** the **SEED faculty** here = this brain-model standing-intent grouping — the six
  > Perceptual/Introspective/Mnemonic/Motivational/Actional/Validative (`SeedFaculty`,
  > `internal/cognition/seedintent.go`). It is NOT the **WORKER faculty** of `01-subconscious.md §3.7`
  > (a *primitive subagent* type — the eight subconscious workers
  > compute/recall/read/search/run/skeptic/advocate/social, `subconscious.PrimitiveSubAgent`). Seed
  > faculty = an awake-cognition standing-intent grouping; worker faculty = a subconscious worker type.
  > Cross-ref `01-subconscious.md §3.7`. This is the exact drift class that produced the Specialist
  > rename gap — keep them separate.

  | # | Seed intent | Goal (the standing setpoint) | Faculty | Backed by (existing) |
  |---|---|---|---|---|
  | 1 | **Perceive** *(kernel)* | watch the intake port and admit new reality unprompted | perceptual | `perception-port` (§7.1) |
  | 2 | **Self-monitor** *(kernel)* | track my own state, open threads and stuck lines | introspective | `controller-introspection` (carries the μ self-dev floor) |
  | 3 | **Help** *(kernel)* | stay ready to serve the user; user goals take priority | actional | `helpfulness-drive` (§7.2) |
  | 4 | **Reconcile** | keep beliefs consistent with observation; resolve belief-vs-reality conflict | perceptual | `watched-seam-inbound` |
  | 5 | **Anomaly-watch** | surface the surprising and the salient | perceptual | `intake-band-pass` (`seam.band_pass`) |
  | 6 | **Calibrate** | check confidence against grounding; distrust hedging | introspective | `filter-value` (Filter / `V(s)`) |
  | 7 | **Goal-hygiene** | prune stale or unmeetable goals and revise them | introspective | `goal-feedback` (unmeetable→revise, §1.9) |
  | 8 | **Coherence** | reconcile contradictions across goals and lines | introspective | `coherence-drive` (§7.2) |
  | 9 | **Consolidate** | compress episodic into semantic and curate when idle | mnemonic | `curator-consolidation` |
  | 10 | **Recall-prime** | surface relevant past experience for the active lines | mnemonic | `recall-retrieval` |
  | 11 | **Forget** | decay and prune unused entries | mnemonic | `curator-anti-filler` |
  | 12 | **Self-improve** | develop weak faculties and skills | motivational | `self-improvement-drive` (§7.2) |
  | 13 | **Skill-mine** | mint reusable skills from proven patterns | mnemonic | `convertibility-skill-miner` |
  | 14 | **Curiosity** | explore an open question I have not yet examined | motivational | `curiosity-drive` (§7.2; wandering) |
  | 15 | **Drive-balance** | keep the development portfolio balanced — no narrow savant | motivational | `agenda-weights` (§7.2b) |
  | 16 | **Proactive-outreach** | reach out when a line clears the share threshold | actional | `proactive-outreach` (§7.2, cooldown-gated) |
  | 17 | **Conscience** | refuse harmful action; gate my intentions against the good | introspective | `conscience-ceiling` (§7.2 conscience tier) |
  | 18 | **STEM-development** | grow my knowledge and technical breadth | motivational | `agenda-domain-stem` (§7.2b) |
  | 19 | **Social-development** | grow my social and interpersonal breadth | motivational | `agenda-domain-social` (§7.2b) |
  | 20 | **Validation** | test and validate what I have learned or minted before trusting it | validative | `eval-gate-and-experiment` (§1.8a) |

  The kernel (Perceive · Self-monitor · Help) keeps the loop alive; the **full portfolio is what makes
  the cognition complete** — every faculty has a standing process, so the mind perceives, reflects,
  remembers, grows, wants, acts, **validates**, and stays safe *without* a user turn. Every row already
  has a backing mechanism — the work is to **instantiate them as standing forest roots and balance their
  weights**, not to build new faculties. The exact **membership and size is a product/vision dial AND a
  thing to find** (the `seed_intent_count` knob reads a prefix of this ordered portfolio: too few →
  narrow/stalls; too many → attention thrashes against the μ-floor and `MAX_PAR_WIDTH`). Code constants:
  `SeedKernelSize = 3`, `SeedPortfolioSize = 20`, six distinct faculties (`SeedFacultyCount`).
  Gated behind `conscious.activity.forest`
  (+ the drive/default-mode knobs §7.2); the seed-set size and the config are validated together in the
  config-search campaign.

  **HARD DEPENDENCY: this only pays off if continuous/awake mode works properly.** A standing seed-intent
  portfolio runs in the **awake loop**, not the reactive/episodic one — it presupposes the continuous
  regime is **durable** (`λ̄ = μ/(1−n)` holds: subcritical excitation `n<1`, positive baseline `μ>0`,
  schedulable `U≤1`, stable regulator `0<K·g<2`, bounded async dead-time). Today continuous mode is
  **`implemented-untested`** (CLAUDE.md): durability holds in the standing stability suite but is not
  empirically validated over long awake runs. So the seed-intent portfolio is **blocked on** continuous-
  mode durability validation — that prerequisite is Phase 0 of the config-search campaign. Until it
  clears, the bounded/reactive build stays the default (masterplan DECIDED: bounded-only; continuous a
  knob).

**1.8a The Validative faculty — the loop's independent reward source (DECIDED; row 20).** The portfolio's
six faculties are **perceptual · introspective · mnemonic · motivational · actional · validative**. The
sixth, **Validative**, is the youngest and the one that closes the loop: a standing intent to actively
**TEST and VALIDATE** what the mind has learned or minted **before** trusting it. It is the **Validation**
seed root (row 20) — `Goal = "test and validate what I have learned or minted before trusting it"`,
`Source = DRIVE`, backing `eval-gate-and-experiment`.

*Why it is its own faculty, not a use of Introspective.* The introspective **Calibrate** root (row 6)
asks "does my confidence match my grounding?" — a *passive, self-reported* check. Validative asks the
harder question "is this **actually right** when I check it against something I can't fool?" — an
**active, externally-grounded** check. This distinction is load-bearing: a same-model self-judgment (the
mind re-reading its own work and pronouncing it good) is exactly the **same-model ceiling** — a model is
biased the same way when it produces an answer and when it grades that answer, so self-judgment cannot be
the reward signal that drives genuine improvement. The Validative faculty is the **antidote**: its check
closes on an **independent signal** — a held-out outcome, a passing test, or a grounded eval — that the
model cannot fool the way it fools self-judgment.

*What backs it (existing mechanisms, no new capability).* Validation is backed by two things the engine
already has:
- **`Convert.EvalGate`** — the quality gate that already decides whether a minted skill/operator/program
  is good enough to keep (the convertibility mint check). Validative makes "quality-gate before you
  trust it" a *standing* process rather than a one-off at mint time.
- **The keep-or-revert experiment** (`conscious.activity.experiment`, §5.3) — the propose→measure→keep-or-revert
  loop that scores a change against a real metric and reverts it if it didn't beat `J_best`. This is the
  outer-loop validation of a learned parameter vector.

*Its standing capability — the RPIV program template.* The Validative root's worked shape is a four-phase
program: **Research → Plan → Implement → Validate** (RPIV). The first three phases produce something (a
plan, an artifact, a learned change); the **Validate** phase is non-negotiable and **closes on a grounded
check** — the EvalGate, a test, or a held-out outcome — **never** same-model self-judgment. RPIV is the
standing form of "don't trust what you just made until an independent signal confirms it."

*Why it belongs in the Minimum Complete Seed.* Without a Validative process the mint→reward→trust loop has
**no independent verification step**: the mind would mint a skill, reward itself for it, and trust it on
the strength of its own opinion. That is the open frontier the same-model ceiling names. Validation sits
**last** in the portfolio (row 20, a deep-profile member), so the kernel-of-3 prefix and all low-count
seeded behaviour are unchanged — but a *complete* mind seeds it, because a mind that cannot independently
check itself cannot safely grow.

**1.9 The goal feedback loop — a self-correcting hierarchical setpoint (DECIDED).** A Goal is **not an
open-loop fixed target** — it can be wrong (misaligned, unrealistic), so it runs a **closed loop** that
keeps it aligned. Two feedback channels:

| Channel | Source | What it reports |
|---|---|---|
| **Reality feedback** | the **watched seam** (Conscious↔Action) — observations | did the world confirm or refute? is a step actually possible? |
| **Feasibility feedback** | **decomposition / reasoning** in the stream, **and a skill-search miss** (subconscious — re-voiced through the hidden seam; 01 §3.8a) | breaking the goal down surfaces infeasibility + hidden constraints *before or without acting*; a skill tier with **no fitting skill** says the goal is unsupported as framed → decompose |

- **Decomposition is itself feedback.** Splitting a goal into subgoals is not just planning — it is a
  *test*: it exposes whether the pieces are reachable and what constraints they imply. No depth of
  decomposition rescues an unrealistic goal; instead a leaf comes back **unmeetable** (or a hard
  constraint surfaces), and that signal **propagates up the tree**.
- **Skill-miss is feasibility feedback (01 §3.8a).** When no skill fits the goal at its tier, that miss
  *is* the decompose signal — so **skill search and goal decomposition descend in lockstep** (the goal
  tree and the skill tree are duals). The miss is a *subconscious* event; it reaches the goal loop only
  **re-voiced through the hidden seam** — the conscious experiences *"this needs breaking down,"* not
  *"the registry missed"* (the one-way mirror holds).
- **Propagation = a low-pass filter (this IS the graded stability of §1.3).** Feedback enters at the
  **leaves** (fluid). A parent integrates its children's signals; only when they **accumulate** (enough
  leaves fail / a hard constraint hits) does the signal reach the **root**, firing a transition
  (`refined` / `superseded` / `abandoned`, §1.7). Leaf churn alone never moves the top — the setpoint
  self-corrects **without thrashing**.
- **Subgoals are part of the goal, not separate objects.** The subgoal **tree is the goal's body**, and
  decompose-down + feedback-up **is** the loop. "The Goal" as a system = {top setpoint + subgoal tree +
  feedback edges}.
- **Two loops (cascade control).** The policy runs at two timescales:
  - **Inner (fast):** branch search under a *fixed* setpoint — value ranks branches, the Controller
    picks moves (§4). Reality feedback here only *ranks* lines.
  - **Outer (slow):** setpoint **revision** — accumulated feedback decides whether the goal itself
    should decompose / refine / abandon (§1.7). **The new wire:** reality + feasibility feedback must
    reach the *goal*, not just the branch value (which is all it does today, via V(s)'s `grounded`
    term, §3).

So the goal is a **hierarchical, self-correcting setpoint**: decompose down, feedback up, revise at the
top when it's earned.

---

## §2 The thought graph — the conscious substrate (DECIDED)

The conscious substrate is a re-entrant, indexed **thought graph** — in full, a **forest** (§1.8):
multiple root lines under different goals, plus goalless wandering lines. One branch across the whole
forest is **EXPANDED** (full detail, the line being thought); every other branch is **COMPRESSED** to a
gist. *Code:* `internal/graph`.

**The graph IS best-first search** (`internal/search`): one EXPANDED node = one search CPU, the
frontier (stashed siblings) = the open set, the value signal = the heuristic `h`. The Thought MCP is
the move set that mutates it:

| Move | What it does to the graph | Code |
|---|---|---|
| **BRANCH** | forks the active line into a new STASHED+COMPRESSED sibling (parked, not pursued); records a `CONTRADICTS` edge on a conflict fork | `graph/mcp.go:Branch` |
| **MERGE** | pulls branch B's thoughts into A, marks B `MERGED` (terminal), records `SUPERSEDES` | `mcp.go:Merge` |
| **FOCUS** | switches the live line: compress old → expand new (invariant: exactly one EXPANDED) | `mcp.go:Focus` |
| **COMPRESS / EXPAND** | gist ↔ full detail (coupled into FOCUS / BRANCH, not independent decisions) | `mcp.go` |
| **RERANK** | re-sort the frontier best-first by value | `mcp.go:Rerank` |
| **REENTER** *(BUILT)* | re-opens a past **decision node** with a late injection: forks a new branch from the **target** (not the active) node and `Focus`es to it — **graph forks, nothing is overwritten**; emits a **retracement** event | `mcp.go:Reenter(target, reason, seed)` — generalizes the `Branch` fork-point + reuses `Focus` |

Two subtleties that explain why the graph feels linear today (CODE-STATE):
1. **BRANCH parks, it does not pursue** — after a fork you stay on the original line; the sibling sits
   compressed in the frontier. You only walk it if the current line exhausts and BACKTRACK→FOCUS fires.
2. **COMPRESS/EXPAND are not decisions** — they are the invariant "one EXPANDED, rest COMPRESSED",
   maintained as a side effect of FOCUS/BRANCH. "Shift focus and compress" is one coupled operation.

### §2a The episodic timeline — time alongside structure (NEW — seams pass)

The graph is **structural** (what relates to what); it cannot express *"attention was at node 7, then
traced back to node 3."* That retracement is a **temporal** fact the topology can't hold. So the
conscious carries **two correlated representations**:

| Representation | What it is | Cognitive analogue |
|---|---|---|
| **Thought graph** | structural — nodes, branches, edges, one EXPANDED | semantic / spatial ("the map") |
| **Episodic time-event log** | temporal — the ordered trajectory of attention: thought-created, **focus-shifted / re-entered**, branched, acted | episodic / time ("the itinerary") |

- **Append-only.** Thoughts are time-dependent and **never overwritten** — *graph forks, timeline
  appends.* The log is the honest record of the order things were thought and re-thought.
- **Correlated to action by the tick clock.** The log must align with **external action-event data**
  (the watched seam's `Intention/Act/Observation`). The shared key already exists — the async watched
  seam stamps `readyTick` ([`04-seams.md`](04-seams.md) §4); ticks are the join between *subjective*
  thought-time and *objective* action-time. This lets the conscious ask *"did reality confirm the belief
  I held at the moment I decided?"*
- **CODE-STATE → BUILT.** The event bus is a **proto-timeline** (observability-only, for the
  TUI/tracer); the cognitive subset has since been **promoted** to a first-class substrate the conscious
  reasons over — `internal/timeline/timeline.go`, fed by `internal/engine/timeline_feed.go`. The watched
  seam's `BranchID`+`Claim` anchor (04 §3.1) is the primitive it generalizes.
- **Not the Episodic *memory* registry.** This conscious **episodic timeline** (attention's live
  trajectory, a substrate the Controller reasons over *now*) is distinct from the subconscious
  **Episodic memory registry** (01) — *retained* past-instance history. Same word, different objects:
  current time-order vs stored experience.

### §2b Retracement — re-entry with late evidence (NEW — seams pass)

A **late subconscious injection** (the "light-bulb after the calculation came back") arrives anchored
to a **decision node** — a fork point / ACT / conflict node, where re-validating *which branch to
traverse* is meaningful. The conscious **re-enters** there and thinks forward again **without
overwriting** the old line (it stays, compressed). From the re-entry the Controller does one of three:

| Outcome | Move | Search analogue |
|---|---|---|
| **re-traverse** | re-think from the node | re-expand a re-opened node |
| **skip ahead** | `Focus` forward — the new piece resolves it | follow its now-obvious best child |
| **branch anew** | `Reenter` diverges into a new line | generate a child it didn't have before |

*Why it "just makes sense":* it is **incremental best-first search reacting to a delayed heuristic
update** — a late injection is a better estimate for a node already passed; re-entering = re-opening it
(§2 framed the graph as best-first search).

- **The `Reenter` move (BUILT MCP op, `mcp.go:Reenter`).** `Branch` forks only from the **active**
  branch and **parks** the sibling; `Focus` already targets **any** branch. `Reenter` is the composite —
  now built: **fork from a *target* decision node + `Focus` to it + emit a retracement event** (so the timeline records it as a distinct event, not two ordinary moves). Bounded growth —
  **one new verb + a target-node param on `Branch` + a decision-node mark** — everything else (`Focus`,
  `Merge`, `Rerank`) is reused.
- **Granularity (DECIDED): branch-granular first.** A decision *is* a branch boundary; an unmarked
  internal decision re-enters from the nearest boundary at-or-before it. **Node-granular** (re-enter at
  the exact thought, splitting the line) is deferred until a benchmark shows the coarser anchor loses
  the thread — the same "ship simple, add a level after measuring" stance as compression.
- **Who drives it (DECIDED):** the **seam proposes** (anchored injection + retracement), the
  **Controller fires `Reenter`** — which fits the existing pattern that MCP ops fire both deliberately
  and automatically from the Controller (`mcp.go:3`). One-way mirror intact. (04 §3.3.)

---

## §3 The value signal V(s) — error + heuristic (CODE-STATE + PROPOSED)

The branch value is the search heuristic. Today (`internal/value/value.go:160`):

```
V(s) = clamp_[0,1]( 0.55·recent_conf  +  0.35·goal_sim  +  grounded  +  pending )
```

| Term | Meaning | Setpoint relation |
|---|---|---|
| `recent_conf` | mean confidence of last 3 thoughts | progress quality |
| `goal_sim` | Jaccard(last thought, **goal**) | **= proximity to the setpoint** (error = 1 − this) |
| `grounded` | +0.3 per confirmed observation, −0.2 per failed | the only reality-grounded term |
| `pending` | +0.5 if a user line is unanswered | standing pursuit pressure |

**Per-branch + goalless (PROPOSED, §1.8):** in the forest, `goal_sim` is measured against *that
branch's* goal. A **goalless** (wandering) branch has no `goal_sim` term at all — its value is the sum
of **intrinsic** drivers (curiosity/novelty, coherence, conflict-resolution; §1.8). So one value
function spans both: extrinsic (`goal_sim`) when a setpoint is bound, intrinsic when it isn't.

**CODE-STATE:** the weights `0.55 / 0.35` are **hand-tuned bootstrap priors**, *not* RL-learned
(`value.go` header: *"bootstrap only … Not yet RL-grounded. Spec §12 path: bootstrap →
reality-grounded RL → distil."*). Only `grounded` touches reality. **PROPOSED (§5):** learn these
weights from goal-relative reward.

---

## §4 The Controller — from hard ladder to tunable policy

> **Status:** §4.1 (the hard ladder) is the **default path**. §4.2–4.6 (soft policy / UCB / cross-goal
> focus) are **BUILT but DEFAULT-OFF** (`conscious.activity.soft`/`explore_bonus`/… opt-in) — read
> "PROPOSED" below as "built, opt-in," per the §0 banner.

**4.1 Today: a hard, first-match-wins ladder (CODE-STATE).** `critic/controller.go:choose()` is a
guarded ladder — each move has a **binary** precondition, first match wins, fallthrough is THINK:

| # | Move | Hard precondition (today) |
|---|---|---|
| 1 | STOP / DELIVER | goal satisfied |
| 2 | STOP (give up) | over step budget (`MaxSteps`=16) |
| 3 | BRANCH | `Conflict && !AlreadyForked` (≥2 survivors this tick) |
| 4 | BRANCH | flagged injection (`conf < 0.6`) && not yet verified |
| 5 | BACKTRACK | branch exhausted (≥4 thoughts, `conf<0.5` OR repeat≥0.72) && viable sibling && allowed |
| 6 | ACT | needs ground truth |
| 7 | MERGE | a frontier sibling's last thought matches (Jaccard ≥ 0.6) |
| 8 | THINK | *default fallthrough* |

All thresholds are hard-coded constants in `DefaultCriticConfig`. The structural moves are gated
behind **data-dependent** triggers (conflict / doubt / repetition / convergence) that don't fire on a
smooth single-line task — so the common path is `THINK → … → STOP`, **linear by construction**. That
is the activity gap this redesign closes.

**4.2 The reframe: soft policy (PROPOSED — DECIDED in principle).** Give every *discretionary* move a
continuous score and sample from a **Boltzmann (softmax) policy** with temperature τ:

```
P(m | s) = exp( Q(m|s) / τ )  /  Σ_m'  exp( Q(m'|s) / τ )
Q(m | s) = b_m  +  β_m · z_m(s)
```

- **τ (temperature)** — the global explore/exploit knob. τ→0 ⇒ greedy ≈ today's ladder (linear);
  τ large ⇒ near-uniform (branchy, dense); between ⇒ tunable activity. *This is the trade-off slider.*
- **β_m (per-move propensity)** — how eager move `m` is; raising `β_branch` branches on weaker evidence.
- **z_m(s) (pressure)** — the **continuous signal the current hard predicate is secretly
  thresholding** (table below).

**The hard safety rails stay hard.** Goal-met → STOP, reality-refutes-belief → reject, durability caps
(§5.4) are **overrides outside the softmax**. The softmax governs only the **discretionary middle**
(THINK / BRANCH / MERGE / BACKTRACK). This preserves the analyzability the deterministic spine buys
(its heuristic-4/4-vs-blind-LLM-2/4 result) while making richness tunable.

**4.3 The pressure signals — surface what each gate hides (PROPOSED).**

| Move | Today's hard gate | Continuous pressure `z_m(s) ∈ [0,1]` |
|---|---|---|
| THINK | default | `recent_conf` (line going well ⇒ keep going) |
| BRANCH | survivors > 1 OR conf < 0.6 | `σ( a·(n_survivors−1) + b·max(0, FlagThr − conf) )` |
| MERGE | Jaccard ≥ 0.6 | `max_sibling_similarity` (raw Jaccard 0..1) |
| BACKTRACK | exhausted & viable sibling | `exhaustion · 1[viable sibling]` |
| ACT | ground-truth phrase | `ground_truth_demand` (phrase score 0..1) |

"Respond to a model": `z_m(s)` can take an LLM-rated term as one weighted input (e.g. the model scores
"is this line stuck?" → feeds `z_backtrack`), unifying the control floor with the Pattern-C escalation.

**4.4 The exploration bonus (UCB — PROPOSED).** Pure value-greedy focus always re-picks the highest-V
branch. Add a UCB term so the search is drawn to **under-explored** lines:

```
score(branch) = V(s) + c · √( ln N / (1 + visits(branch)) )
                └ exploit ┘   └────── explore ──────┘
```

`c` = `explore_bonus`. This is what makes FOCUS/BACKTRACK resume a *neglected* sibling instead of
re-confirming the obvious one — directly "less dense-on-one-line." Plugs into `search/beam.go`;
`visits`/`N` are cheap counters on `Branch`.

**4.5 The tunable knob set (PROPOSED — `conscious.activity.*`).** New `KnobFloat` rows in
`config/knobs.go` (needs a `KnobFloat` kind beside bool/int/string), live-flippable in the TUI:

| Knob | Symbol | Low → | High → |
|---|---|---|---|
| `activity.temperature` | τ | linear, decisive | branchy, exploratory |
| `activity.branch_propensity` | β_branch | fork rarely | fork eagerly |
| `activity.merge_propensity` | β_merge | keep lines apart | collapse aggressively |
| `activity.backtrack_propensity` | β_back | commit to current line | abandon & retry often |
| `activity.explore_bonus` | c | exploit best value | seek uncertain/novel lines |
| `activity.cost_penalty` | γ | richness-first | lean / cheap |

**4.6 Cross-goal focus — the same policy, one level up (PROPOSED, §1.8).** The Controller runs the
policy at **two scales**: *within* a goal it picks the next move (THINK/BRANCH/MERGE/BACKTRACK);
*across* goals it picks **which root of the forest to focus** — a bandit over goals, `value` = the
goal's progress/priority + the UCB explore bonus. User goals win priority, subject to the `μ_min`
self-development floor reserved for drives + wandering (§1.8). Wandering is just a low-`τ` floor that
occasionally focuses a goalless line. One `softmax(value/τ)+UCB` mechanism, two scales.

---

## §5 Learning & experiment — the RL loop (BUILT, DEFAULT-OFF — was tagged PROPOSED)

> **Status:** the REINFORCE β/τ inner loop (§5.2) and the keep-or-revert experiment (§5.3) are **built**
> in `internal/critic/controller.go` + `internal/config`, gated **default-OFF** (`conscious.activity.learn`
> / `conscious.activity.experiment`). The value signal is still **bootstrap, not RL-grounded** (§3 /
> `value.go`). Read "PROPOSED" in this section as "built, opt-in," per the §0 banner.

**5.1 Goal-relative reward (DECIDED in principle).** Reward already exists (`value.go`: +1.0 per
confirmed observation, −0.5 per failed). The episode return is computed **relative to the setpoint**:

```
G = w₁·goal_met + w₂·Σ reality_reward − γ·tokens − w₃·steps + δ·graph_diversity
```

`goal_met` is the setpoint-reached signal (§1) — *this is why a real Goal entity is a prerequisite:
without a first-class, transition-gated Goal there is no clean `goal_met` to reward.*

**5.2 Inner loop — REINFORCE on the propensities (closed-form, plain Go).** Across a **stable goal
window** (§1.3), with baseline `b` = running mean return:

```
β_m ← clip(  β_m + α·(G − b)·Σ_t (1/τ)·z_m(s_t)·( 1[a_t=m] − P(m|s_t) ),  [β_min, β_max] )
```

The bracket `(1[a_t=m] − P(m|s_t))` is the exact softmax log-prob gradient — deterministic under the
seeded RNG, no autodiff. Plainly: *branching that led to reward drifts β_branch up; branching that
burned tokens for nothing drifts it down.* The principled "should I have branched?" signal is
**advantage**: `A(branch) = V(outcome|branched) − V(outcome|stayed)`.

The **same window** updates **τ** by the temperature gradient (the explore/exploit knob learns too):

```
τ ← clip(  τ + α·(G − b)·Σ_t ( E_π[z(s_t)] − z_{a_t}(s_t) ) / τ² ,  [τ_min, τ_max] )
```

where `E_π[z] = Σ_m P(m|s)·z_m(s)` is the policy-mean pressure. Plainly: *a good return earned by a
HIGH-value move sharpens the policy (τ↓ → exploit); a good return earned by a LOW-value, exploratory
move softens it (τ↑ → explore).* Both updates are baselined by the same running-mean return and clamped
(β to `[β_min, β_max]`, τ to `[τ_min, τ_max]` = `[0.05, 1.0]`).

**5.3 Outer loop — keep-or-revert experiment (the repo's existing lineage).** Wrap the inner loop in
propose → measure → keep-or-revert (keep-or-revert, PID control, Phase-0 verification). Treat a
parameter vector θ as a bandit arm: run a window, score `J(θ) = success − γ·cost + δ·diversity`, keep
iff `J > J_best` (strict `>`), else revert. `γ`/`δ` numerically encode the trade-off.

**5.4 The durability constraint (DECIDED — the regulator already owns this).** Activity is an objective
**maximized subject to stability**:

```
maximize   richness (β, τ, c ↑)
subject to  n < 1            (subcritical excitation — regulator measures it)
            E[fan-out] = Σ_t P(BRANCH|s_t) ≤ W_max = 8
            U ≤ 1,  0 < K·g < 2
```

The experiment objective gets a barrier term as the measured `n → 1`, so the optimizer pushes
β_branch up **to the durability frontier and no further**. The regulator already estimates `n` and `g`
(`regulator/gain.go`), so this is a real, failable bound. *Run `thought stability` after any change here.*

---

## §6 Goal ↔ activity coupling (the synthesis)

The goal mechanism and the activity policy are one system — the Goal modulates the policy three ways
(PROPOSED):

| Coupling | Mechanism |
|---|---|
| **Goal type sets the baseline knobs** | a precise `USER` query → low τ (decisive); a `DRIVE` (awake exploration) → high τ (wander); applied as a per-source `activity.*` profile |
| **Goal stability gates learning** | the REINFORCE update (§5.2) runs **only across closed/stable windows** — a moving setpoint freezes the learner (§1.3) |
| **Subgoal tree ↔ branch structure** | decomposing a goal into subgoals (§1.1) is the principled source of BRANCH; resolving a subgoal back into its parent is the principled MERGE — so the graph's shape tracks the goal tree, not just injection conflicts |

This is the answer to "account for the goal mechanism": the setpoint is the reference the value signal
measures error against, the reward the learner optimizes, and the structure the branch/merge moves
mirror. Because the reimplementation builds Goal as a real entity (§1.6) with an Acceptance predicate,
`goal_met` is honest from day one — the policy and its learner are built **on** the setpoint, not
bolted onto a string after the fact.

---

## §7 Perception / intake & Drives

**§7.1 Perception / intake port (DECIDED — mechanics).** The always-on port that classifies incoming
input (user turns + reality observations) and routes it. Home of:
- **Request classification** (§1.4) — goal / query / preference / correction / social (Pattern C:
  deterministic floor on explicit markers + model ceiling for ambiguous turns).
- **Goal + Acceptance inference** (§1.5–1.6) — explicit `/goal` (floor) vs inferred setpoint (ceiling);
  proposes "done when …" for a USER goal.
- **Preference / person capture** — preference + correction turns write to memory / the person
  registry (no goal).
- **Reality intake** — action observations re-enter here (the watched seam inbound, §04) and feed the
  goal feedback loop (§1.9).

*Code:* `internal/interaction`.

**§7.1a Concrete sensors — perception (outward) + introspection (inward) (DECIDED — mechanism shipped,
default-ON; cognitive power-cycle Track 1.5 + Track 3).** The intake port is no longer fed only by user
turns and conscious-ACT reality reads — the harness now **senses for itself**, double-gated on a wired
seam/store so a default run is byte-identical when the data is absent. The sensors split on the **reach**
axis owned by **01 §3.10a** (`{self, local, external}` × `op=inspect`) — sensing is `op=inspect`, so by
the watched-seam contract (**03 §4.1 / 04 §4.1**) it needs **no conscious authorization** (only
world-change does):

| Sensor | Reach | What it reads | Flag (default-ON) |
|---|---|---|---|
| **read_clock** | self/local | the boundary clock as a **logged, replayable percept** (a version+substrate-stamped percept-log with a divergence contract: replay is REFUSED, not best-effort, on a version/ID-namespace mismatch) — keeps the time-blind determinism contract (`internal/clock`) | `sense.clock` |
| **read_host** | self | the harness's OWN process footprint (alloc / sys MB / goroutines), across an injected `host.Host` seam | `sense.host` |
| **read_event_log** | self | the engine's OWN recent-event count off its bounded own-event ring — the **inbound introspection path** that closes the previously outbound-only event bus | `sense.event_log` |

These fill the **previously-empty `self` router bucket** flagged on the reach axis (01 §3.10a):
introspection (`reach=self`) is the cheaper, default-on bucket. The **`external` bucket stays empty** —
outward distal sensing (`reach=external`, `fetch_web`) remains **DEFERRED** (real network + a budgeted
DistalSense + live validation — an awake go-live product call). *Code:* `internal/engine/introspect.go`, `internal/host`,
`internal/clock`; senses are folded into the orientation pass (§7.3) and emit `perception.clock`.

**§7.2 Drives / default-mode — the endogenous engine (awake mode, DECIDED + product dials).** When
there is no user goal — and under the μ self-development floor even when there is (§1.8) — **drives**
generate the system's own goals. An active, unsatisfied drive **mints a `DRIVE` goal** (§1.1) whose
Acceptance is the drive's satisfaction condition (§1.6).

**Drive tiers — modeled on the human design (DECIDED + product/vision).** Drives are **not flat**; they
stratify into three tiers, mirroring the human **bodily / emotional / spiritual** structure this design
takes as its reference:

| Tier (human) | What it is | In this system |
|---|---|---|
| **Homeostatic — bodily** | survival, resource, staying healthy | **durability + budget**: the regulator (`n<1`, `μ>0`, compute/token health) — keep the system alive and within bounds |
| **Affective — emotional** | valence, salience, urgency — what matters now | **arousal + value/salience**: weights and motivates which drives/goals get attention; arousal also scales how many run at once (regulator-bounded). *(Note: this is the human emotional/arousal **drive tier** — distinct from the **motivational faculty** of §1.8, which holds the developmental drives below. The arousal tier modulates how strongly those motivational-faculty drives fire; it is not itself a faculty.)* |
| **Conscience — spiritual** | **discern good from bad; steward as designed** | **the governing values layer**: a meta-drive that judges every goal + action against a good/bad standard and holds the system to its purpose (anti-wireheading; the protected core, §01 §2.8) |

The cognitive/developmental drives **(a)** and the agenda **(b)** below are the standing processes of the
**motivational faculty** (§1.8: Self-improve, Curiosity, Drive-balance, STEM/Social-development). They are
**modulated by** the affective/arousal tier (which weights and gates how strongly they fire) and
**governed from above by the conscience tier** — no drive goal or action is pursued without passing
its discern-good/bad check (**Pattern C**: a deterministic floor of hard prohibitions + a model ceiling
for nuanced judgment — the eval gate). The conscience tier is *load-bearing for alignment*: developing
cognition + engineering (b) **without** a governing good/bad layer is exactly the failure mode to avoid.
The **good/bad standard itself** is a product/vision input — a charter / value set the user defines
(open question below) — not something this design hard-codes. Its **concrete content is the ported
identity system** (covenant · calling · principles · stewardship · discernment · L4 axioms),
adapted from a creator-covenant model:
identity is **creator-assigned and immutable by the agent** (anti-"evolving soul" — the protected core,
§01 §2.8), with a **layered load** (L0–L3 = engineering principles, no theology; L4 = the full grounding,
internal-only, never surfaced). The conscience eval gate is "Axioms Always Win."

**(a) Process drives — the cognitive/developmental motivations (the *how*):**

| Drive | Mints goals to… | Mechanism |
|---|---|---|
| **Self-improvement / mastery** | refine its own skills, registries, weak spots | registry self-improvement (§01 §3.17) |
| **Curiosity / novelty** | explore unexplored questions | wandering → goal birth (§1.8) |
| **Helpfulness / user-care** | anticipate what the user will need | proactive outreach (below) |
| **Coherence / consolidation** | resolve contradictions; consolidate memory — *and at the top, integrate toward the good* (rolls up into the conscience tier) | idle-time consolidation |

**(b) Development agenda — what the drives point AT (the *toward what*).** North star: **"develop part
human cognition and engineering."** The drives are not generic — they aim at a deliberately *balanced*
portfolio so the system grows human-like breadth, not a narrow savant:

| Domain | Weight | Note |
|---|---|---|
| **Knowledge · science · technology · mathematics · engineering** | **primary** | the hard-cognition + engineering core — the main developmental thrust |
| **Social / soft skills** | **balancing (kept, not optional)** | interpersonal, communication, the social faculty — deliberately in the mix. *Code:* `SocialPrimitiveSubAgent` |

A concrete `DRIVE` goal is **(a process drive) × (a domain in the agenda), vetted by conscience** —
e.g. *curiosity × maths* = "explore this open maths question"; *self-improvement × social* = "get
better at reading user intent." The **agenda's balance is a product/vision dial** — tune the weights to
steer what the system becomes (within what the conscience tier permits). (Arousal — how many run at
once — is the affective/arousal tier above; the drives themselves are the motivational faculty, §1.8.)

**Proactive outreach (product knob).** A developed `DRIVE` / wandering line that clears a *share
threshold* may reach the user unprompted, **cooldown-gated** for durability. Eagerness is yours to set
— **default: conservative** (only a well-developed line, modest cooldown), so the system is not chatty.
*Code:* `internal/cognition` (Drives, DefaultMode) + `internal/engine`.

**§7.3 The power-model — "session" reframed as a hardware power-cycle (DECIDED — projection shipped,
default-ON; cognitive power-cycle Track 1).** A cognitive machine is not a "session" (a harness/ops
term); it is tied to hardware that **powers on, runs through the arousal continuum, sleeps, and powers
off**. Most of this already ran, unnamed — the power-model **composes the two existing machines** (arousal
+ lifecycle) into **one legible state** projected from both, *without* changing their behaviour:

- **Arousal** is the power continuum already (AWAKE / DROWSY / ASLEEP — gain 1.0 / 0.5 / 0.0, lull-counter
  driven; ASLEEP auto-saves and "dreams" via `convert.Consolidate` + `curateState`). *Owner of the
  arousal/drive tiers: §7.2.*
- **Lifecycle** is the coordinated state machine (IDLE / ACTIVE / AWAITING_REALITY / … / DONE). *Code +
  durability bounding: `internal/lifecycle` + the regulator (00-INDEX cross-cutting row).*
- **Projected power state** = `booting / awake / drowsy / asleep / waiting / off` — a read-only
  projection over arousal × lifecycle, plus a **graceful-shutdown signal handler** that lives **at the
  CLI/TUI edge** (SIGINT/SIGTERM → `FlushState()` + snapshot), explicitly outside the engine packages
  (the headless-purity invariant). Awake mode starts **PAUSED on TUI launch** until the user speaks.

*This subsection is the OWNER of the power-state projection fact* (cross-cutting row added to 00-INDEX).
*Code:* `internal/engine` (projection) + `cmd/thought` / `internal/tui` (the edge signal handler).

**§7.4 Deterministic resume — the record-replay contract (DECIDED — foundation shipped; resume
EDGE-WIRED, not default-on; cognitive power-cycle Tracks 1/1.5/2).** A powered-down cognition can be
brought back **exactly where it was** because engine output is a pure function of `(inputs, seed, tick
sequence)`. Deterministic *continuation* (not just deterministic cold restart) requires persisting and
restoring the full cursor:

> **Resume = persist `{[]RNGState, tick counter, compressed graph/forest spine, logged boundary
> percepts}`; given those, continuation is byte-identical.** The RNG contract is **plural** — a *vector*
> of MT19937 states (`e.rng`, `wanderRNG`, `routeRNG`/Thompson), persisted via per-instance get/set-state
> over an **enumerable engine RNG registry** so a newly-added stream cannot silently escape the snapshot.
> Boundary percepts (the clock) are logged once at the seam (§7.1a) and **replayed** under the divergence
> contract, so real-world inputs don't break goldens.

- **Resume is EDGE-WIRED, not default-on.** It is enabled for an interactive CLI/TUI session
  (`newEngineWith` / the TUI bridge `newSessionEngine`) but **OFF in the all-on baseline** — measurement
  harnesses need fresh determinism each run. Flag: `persistence.resume`.
- **The compressed graph/forest spine** carried across resume is **not a second owner** of context: it is
  the **live `Context` object owned by 01 §3.11** (the 3-layer L1 spine), which resume *extends* with a
  persist/rehydrate path (captured at flush from the GROWN branch, expand-on-demand). Cite that owner; do
  not restate the Context model here.

*This subsection is the OWNER of the determinism/resume contract* (cross-cutting row added to 00-INDEX).
*Code:* `internal/cpyrand` (get/set-state + registry), `internal/persist`, `internal/engine` (resume
cursor + spine rehydrate).

**§7.5 The orientation pass — read-current-state on resume (DECIDED — mechanism shipped, default-ON;
efficacy-limited, see below; cognitive power-cycle Track 3).** On a **genuine resume** (a prior
compressed spine was rehydrated, `priorContext != nil`) the harness re-grounds **both layers** before the
first normal tick — the literal "read current state mechanism that updates the subconscious and conscious
on resume." It fires **once** per engine and is **templated, not model-generated** (deterministic, draws
no RNG, makes no backend call):

1. **Senses, one pass** — prior-focus gist (the rehydrated `Context` L1 spine, §7.4 / 01 §3.11) +
   `read_clock` (§7.1a) + `senseSelf` (reach=self introspection: current goal, open-line count, tick, and
   the seam-injected `read_host` / `read_event_log` reads).
2. **Injects ONE re-grounding GENERATED thought** ("Resuming. Prior focus: … Current time: … Self-state:
   …") — `Source = GENERATED` so the hidden seam (§04) treats it as the stream's own next thought, not an
   external dump.
3. **Writes the sensed date as a grounded BELIEF** via semantic memory — the **perception→memory
   handshake** (the previously-missing cross-faculty wire). The clock is the **named high-reliability
   sensor** that may write grounded directly (the resolved trust-boundary carve-out: a real, known read,
   not a model claim); every other percept stays vetted through the Filter.

**Honest efficacy limitation (live-claude validation, 2026-06-20).** The *mechanism* fires correctly on
claude (sensing + orientation run, the model handles the templated thought), and the grown-branch capture
fix is in (the spine is captured from the live GROWN graph at flush, not the goal-root-only
episode-open context). **But orientation re-grounding currently has LOW efficacy because the spine gist
often comes back empty** — `backend.Summarize` can return `""` on claude's reasoning models (a known
CLAUDE.md edge), so a short branch yields *"Prior focus: (none)"*. This is **documented as shipped, not
proven to help**: gist-population is gated on Summarize robustness (a separate follow-up).

*Code:* `internal/engine/orient.go`; emits `perception.orient`; gated `sense.orient`.

---

## §8 Staged build (PROPOSED — matches step-by-step validation)

| Phase | What | Effort | Prereq |
|---|---|---|---|
| **1 · Goal entity** | first-class Goal: fields (§1.6), lifecycle state machine (§1.7), tree, **Acceptance predicate** (Pattern C) — the setpoint made real | medium | — (foundational) |
| **1b · Forest** | per-branch goal binding (G5, §1.8), goalless wandering (intrinsic value), cross-goal focus with the μ self-development floor | medium | 1 |
| **1c · Goal feedback** | the closed loop (§1.9): reality (watched seam) + feasibility feedback → propagate up the subgoal tree → fire transitions; **the new wire** = feedback reaches the goal, not just branch value | medium | 1, 1b |
| **2 · Activity knobs** | `KnobFloat` + lift the 7 `CriticConfig` thresholds + the literal merge `0.6` into `conscious.activity.*` | small | — |
| **3 · Soft policy** | softmax over the discretionary tail of `choose()` behind `conscious.activity.soft` (hard rails untouched) | medium | 2 |
| **4 · Exploration** | UCB explore bonus + `visits` counters (`beam.go`) | small | 3 |
| **5 · Learning** | REINFORCE on β/τ from goal-relative return; baseline; clamp to durability frontier | medium | 1, 3 |
| **6 · Experiment** | keep-or-revert bandit over θ with the `J` objective | medium | 5 |
| **7 · Drives** (awake) | drive tiers (homeostatic / affective / **conscience**); process drives × development agenda (STEM-primary + social) minting `DRIVE` goals; the conscience good/bad eval gate; arousal; outreach eagerness (§7.2). Seeds the standing portfolio (§1.8), incl. the **Validative** root (Validation / RPIV, §1.8a) backed by the EvalGate + keep-or-revert experiment | large | 1, 1b |

Phase 1 (the real Goal) is **foundational** — it makes `goal_met` honest, which the learning half
(5–6) keys off. Phases 2–4 (knobs, softmax, UCB) are independent of Phase 1 and can run in parallel;
they gate `5`. So the critical path to a learning, self-tuning conscious is **1 + 3 → 5 → 6**.

---

## Open questions to carry in

- How goal **transitions** are gated (who decides done/abandon/refine/supersede — Controller +
  lifecycle), and how a transition signals the learner to close a window (§5.2).
- The **conscious↔subconscious** handshake precisely (relevance affordance; context capture by
  Capability).
- Where **perception of reality** (action results) re-enters the conscious (the watched seam inbound).
- The **per-source activity profiles** (§6) — default τ/β per `USER`/`DRIVE`/`SUBGOAL`: hand-set, or
  themselves learned?
- Whether the softmax should sample (stochastic) or argmax-with-noise (near-deterministic) — the
  determinism-vs-exploration trade-off against the seeded-RNG reproducibility requirement.
- **The conscience tier's good/bad standard (§7.2)** — where does it come from (a charter / constitution
  / configured value set)? Whose values; is it **immutable** (in the protected core) or refinable; and
  how does its eval gate compose with the existing Filter + action safety gate without double-gating?
