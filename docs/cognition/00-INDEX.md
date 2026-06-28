# Cognition Architecture — Master Index

> The map of the whole cognitive system, by subsystem, with cross-cutting concerns and status.
> Each subsystem has its own doc; this index ties them together. Started 2026-06-14.

## The shape

**Three layers, two seams.** The conscious thinks; the subconscious watches it and reaches up by
relevance, re-voicing results as the conscious's own next thought; only world-changing action is the
explicit, conscious decision (reads cross the watched seam as perception/grounding, not as actions).

```
        GOAL  (conscious objective: user | drive | subgoal)
          │ shapes
          ▼
  ┌───────────────────┐
  │  CONSCIOUS         │  thought graph · Controller · perception/intake · goal management
  └───────────────────┘
        ▲  │  HIDDEN SEAM (FILTER→GATE→TRANSFORM): silent injection, re-voiced; one-way mirror
        │  ▼
  ┌───────────────────┐
  │  SUBCONSCIOUS      │  Capability → Workflow → Operator → SubAgent · registries · context · eval
  └───────────────────┘
          ▲ │  WATCHED SEAM (Conscious ↔ Action): intention out, reality in — the only explicit seam
          │ ▼
  ┌───────────────────┐
  │  ACTION            │  effectors · gated executor · sandbox — world-change OUT (act); reality IN = perception
  └───────────────────┘
```

## Subsystems

| # | Subsystem | Scope | Status | Doc |
|---|---|---|---|---|
| 01 | **Subconscious** | the `Capability → Workflow → Operator → SubAgent` stack + object model | **LOCKED** | [`01-subconscious.md`](01-subconscious.md) |
| 02 | **Conscious** | thought graph, Controller, **Goal-as-setpoint**, activity policy, perception/intake | **WORKING DRAFT** (goal §1 · soft policy §4 · RL §5 · **episodic timeline + retracement §2a/2b** · **drives/conscience/identity §7**) | [`02-conscious.md`](02-conscious.md) |
| 03 | **Action** | **world-change** = the effector mechanism + the gate; reads = perception & minting = self-write are **moved out** | **WORKING DRAFT** (action pass 2026-06-15) | [`03-action.md`](03-action.md) |
| 04 | **Seams** | hidden seam (Subconscious→Conscious) + watched seam (Conscious↔Action) — temporal routing + rate-matching | **WORKING DRAFT** (seams pass) | [`04-seams.md`](04-seams.md) |

## Landed redesigns folded into 01–04

Dated redesigns that have shipped AND folded into their owner docs (their content is now canonical
in those docs — this table is the audit record, not a pointer to an out-of-doc proposal).

| Date | Redesign | Folded into (canonical) | Status |
|---|---|---|---|
| 2026-06-20 | **Cognitive Power-Cycle & Grounded Sensing** — reframes "session" as a hardware power-cycle (boot/sleep/resume); concrete perception + introspection (`reach=self`) sensing that fills the empty router buckets; deterministic record-replay resume; memory/context rehydration via the live `Context` object; orientation pass | 02 §7.1a (sensors) · §7.3 (power-model) · §7.4 (resume contract) · §7.5 (orientation) · 01 §3.10a (reach buckets) · 01 §3.11 (Context, cited) | **LANDED** (mechanism shipped, default-ON; resume EDGE-WIRED; orientation **efficacy-limited** pending Summarize robustness; `fetch_web` + autonomous per-tick sensing DEFERRED) |

## Cross-cutting concerns (span subsystems)

These apply system-wide; **detailed across the subsystem docs** (the **Owner** column points to each), may lift to a foundations doc.
**This table is the OWNER REGISTRY** (the single-source rule, Conventions below): each shared fact has
**one owner**; every other doc **cites** the owner, never restates it.

| Concern | What | Owner — cite, don't restate |
|---|---|---|
| **Object model** | reference vs instance (universal except Context); seeded **and** create; minting | 01 §2.4, §2.5, §3.15 |
| **Registries** | one per object type; generic `resolve.Registry[T]`; Category is itself a registry | 01 §3.14, §3.10a |
| **Category taxonomy** | tool operation `{inspect,mutate,execute}` × reach `{self,local,external}`; skill cats `{reasoning,analysis,synthesis,verification}` | **01 §3.10a** (03-action cites) |
| **Scope** | per-registry filter; ceiling eager (Capability) + pick lazy (`Resolve[T]`); open per-type facet | 01 §3.3a, §3.8a |
| **Eval** | measuring stick (mintable ref) + measurement (instance); benchmark vs measure; 3 levels | 01 §3.18–3.21 |
| **Self-improvement** | every registry refines its references from instance measurements | 01 §3.17 |
| **Protected core** | immutable kernel: durability invariants + seam + Filter + eval gate **+ conscience/identity standard**; anti-wireheading | **01 §2.8** (grants) |
| **Conscience / alignment** | the governing good/bad values layer + creator-assigned, agent-immutable identity; vets every drive-goal & action (Pattern-C eval gate, "Axioms Always Win") | **02 §7.2** (the ported identity system); core-protected by 01 §2.8 |
| **Self-access ladder** | L1 refine · L2 mint · L3 self-docs · L4 self-code; reflexive meta-Capability | 01 §2.8 |
| **Silent injection** | the one-way mirror — conscious ignorant of subconscious | 01 §2.1, 04-seams |
| **Watched-seam contract** | what crosses Conscious↔Action: world-change = authored action; reads = perception/grounding (not actions) | **03 §4.1 / 04 §4.1** (01 §2.1 defers) |
| **Episodic timeline** | dual rep: graph (structural) + time-event log (temporal), correlated to action by ticks — *conscious attention-log; **≠** the subconscious **Episodic memory** registry (01 §3.20)* | 02 §2a-2b · 04 §3 |
| **Three-pattern split** | A control (math) · B content (model) · C escalation (floor+ceiling) | CLAUDE.md; per-subsystem |
| **Durability / regulator** | the stability budget every subsystem is bounded by: `n<1`, `μ>0`, `U≤1`, `0<K·g<2`, fan-out `W_max`, `MAX_OUTSTANDING` | **code: `internal/regulator` + `internal/stability`** (no folder doc; cited in 01 §2.7, 02 §5.4, 04 §2.1) |
| **Power-state projection** | "session" reframed as a hardware power-cycle: `booting/awake/drowsy/asleep/waiting/off` projected (read-only) from arousal × lifecycle + an edge graceful-shutdown handler | **02 §7.3** (arousal/drive tiers stay §7.2; lifecycle = `internal/lifecycle`) |
| **Determinism / resume contract** | deterministic *continuation*: persist `{[]RNGState (plural MT19937 streams), tick, compressed `Context` spine, logged boundary percepts}`; EDGE-WIRED (off in the all-on baseline) | **02 §7.4** (extends the `Context` spine owned by 01 §3.11; code `internal/{cpyrand,persist}`) |

## Conventions — single-source discipline

These docs are forward-only per-subsystem passes at different maturity. To stop shared facts from drifting
across them (the failure mode behind the 2026-06-15 9-issue reconciliation), three rules:

1. **One owner per shared fact; everyone else cites.** Each shared fact / term / taxonomy has **one owner**
   (the registry above); other docs **link to it** instead of restating it — a restatement is a future
   stale copy. (Applied: category taxonomy → 01 §3.10a; watched-seam contract → 03/04; `Scope` object →
   01 §3.3a; eval granularity is **"levels," not "scopes"** — `Scope` is reserved for the object.)
2. **Settle the ripple before closing a pass.** When a pass refines a shared fact, **propagate it to the
   owner + every citing doc *before* the pass is marked done** — never record it locally as a one-off
   "refinement" (that is exactly what produced the drift).
3. **Trace + grep before coining a concept.** Before writing a new concept in, walk the full containment
   hierarchy — `Goal → Capability → Workflow → Operator → SubAgent → Skill` — to confirm it doesn't cross a
   layer, and **grep the term** to confirm it doesn't collide with an existing one.

## How the layers connect (one line)

Goal (conscious) shapes the thought stream → the stream **affords** a Capability (subconscious) by
relevance → the Capability captures context and runs a Workflow of Operators staffed by SubAgents →
results re-voice through the **hidden seam** as the conscious's own thoughts → a world-changing step
crosses the **watched seam** into Action as the conscious's authored act (reads cross it too — as
perception/grounding, not actions).

## Changelog — terminology & shape changes

Append-only log of renames / shape changes, so a reader landing on an OLDER doc knows what is stale and
where the current source lives. Format: date · change · **what is now outdated** · **new source**.

| Date | Change | Outdated (still says the old thing) | New source (canonical) |
|---|---|---|---|
| 2026-06-20 | **Cognitive Power-Cycle & Grounded Sensing LANDED** (mechanism shipped, default-ON; resume EDGE-WIRED). Folded the proposal into its owner docs: concrete perception/introspection **sensors** (`read_clock`/`read_host`/`read_event_log`, `reach=self` bucket filled) → **02 §7.1a**; **power-state projection** (boot/awake/drowsy/asleep/waiting/off + edge shutdown) → **02 §7.3** (NEW owner row); **determinism/resume contract** (plural `[]RNGState` + tick + compressed `Context` spine + logged percepts; EDGE-WIRED, off in the all-on baseline) → **02 §7.4** (NEW owner row, extends 01 §3.11); **orientation pass** (read-current-state on resume; **efficacy-limited** — empty Summarize gist → "Prior focus: (none)", a pending follow-up) → **02 §7.5**; reach buckets noted on **01 §3.10a**. `fetch_web` (`reach=external`) + autonomous per-tick standing-intent sensing DEFERRED. | The proposal's `PROPOSED`/`§8` "eventual owner" wording (now LANDED); any reading of "session" as the unit of cognition (it is a power-cycle); "no self-initiated sensing / empty `reach=self` bucket" (the introspection bucket is now filled). | `02-conscious.md` **§7.1a / §7.3 / §7.4 / §7.5**; owner registry rows **Power-state projection** + **Determinism / resume contract**; proposal banner = **LANDED**; code `internal/engine/{orient,introspect,graph_spine}.go`, `internal/{cpyrand,host,clock}`. |
| 2026-06-20 | **Seed-intent portfolio: 19→20 roots, 5→6 faculties.** Added the **Validative** faculty + the **"Validation"** root (the loop's independent reward source — the same-model-ceiling antidote; backed by the existing EvalGate + keep-or-revert experiment, RPIV template). Renamed the 4th faculty **affective → motivational** (the §7.2 "Affective — emotional" arousal *tier* is a separate concept and KEPT). Reconciled the stray "deliberative" seed-faculty label → actional. | Internal design notes that still say "five faculties … affective" / `coveredFaculties / 5` / "affective faculty", and the historical board rows B3/B4 ("count=19 / 5 faculties" — annotated, left intact). | `02-conscious.md` **§1.8 / §1.8a** (self-contained) ; code `internal/cognition/seedintent.go` |
