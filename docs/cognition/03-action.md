# Action Subsystem — design discussion (WORKING DRAFT)

> Part of the **Cognition Architecture** — see [`00-INDEX.md`](00-INDEX.md).
>
> Status: **WORKING DRAFT** (action pass, 2026-06-15). Supersedes the earlier stub. Worked through against
> the locked subconscious object model ([`01-subconscious.md`](01-subconscious.md)) and the seams
> ([`04-seams.md`](04-seams.md)). Tags: **DECIDED** (settled this pass) · **PROPOSED** (target, not built) ·
> **CODE-STATE** (what the code does today).

## Scope

The **action** layer, redefined: **the system *changing the world*.** That is its whole job — the sole
**per-act conscious decision** to alter anything outside the machine. It sits behind the **watched seam**
(Conscious ↔ Action).

Two things that used to be lumped here are **not** action:

- **Reading / sensing reality is PERCEPTION** — subconscious, frictionless (§4).
- **Self-improvement / minting is LEARNING** — subconscious, on the *self*-substrate (the registries),
  handled by the per-registry self-improvement loop + the self-access ladder (01 §2.6, §2.8), **never** an
  action tool (§4).

The effector + gated executor + sandbox **mechanism** stays here and is **shared** across sense and act
(it is the body's hardware — both senses and muscles). Only **world-change** is "action."

---

## §0 Landed-status banner (the §8 CODE-STATE column is stale)

> **Most of this pass's PROPOSED/delta has SHIPPED.** The §8 mapping table's "CODE-STATE / today"
> column was written against the pre-redesign action layer and now **lags the implementation**. What is
> BUILT in `internal/action`:
> - The **two-axis category taxonomy** — operation `{inspect, mutate, execute}` × reach
>   `{self, local, external}` — is in code (`Tool.Category()`, `gateroute.go` `Operation`/`Reach`),
>   **replacing** the old flat `{inspect, mutate, execute, external}` tag set. (Owned by 01 §3.10a.)
> - The **gate routes by (operation × reach)** into the three gating classes of §2/§3
>   (`internal/action/gateroute.go`).
> - The **network `external` budget / policy** exists (`RouteBounds.NetworkEnabled` / `NetworkQuota`,
>   `RouteDecision.QuotaExceeded`; a distal sense with the policy off / quota spent is declined,
>   **offline-safe** — `executor.go`). The §7 distal-sense policy+quota is built, not just decided.
> - The **never-fabricate breadcrumb** + sandbox are kept and shared across sense + act (`watched.go`).
> So read §1–§7 DECIDED items as **built**, and the §8 "CODE-STATE" column as the historical baseline
> (its "delta" rows are largely **done**). The genuinely-open items are in "Open / carry-in" at the end
> (async world-change + bounded dead-time, numeric quota values, the self-vs-world substrate detector).

## §1 The cut is EFFECT, not "who holds a tool" (DECIDED)

The subconscious↔conscious boundary is **effect on substrate**, not whether a tool is involved. A SubAgent
*referencing* read tools by category-scope (01 §3.6) does **not** encroach on the action layer, because a
read changes nothing. The four quadrants of (**substrate** × **operation**) land in **four different
mechanisms the architecture already has** — only one of them is the action layer:

| | **READ** (`inspect`) | **WRITE** (`mutate`) |
|---|---|---|
| **SELF** (registries, memory, graph) | introspection — subconscious | self-modification — the **self-improvement loop + self-access ladder L1–L4** (01 §2.6/§2.8), gated by the **protected core**; *not* the action layer |
| **WORLD** (repo, services, network) | perception — subconscious (free local / gated distal, §4/§7) | **ACTION — `mutate`/external write → the watched seam**, conscious-decided, gated |

So the worry "does a subconscious with tools mix the action role?" is dissolved by the existing
architecture: **minting is the registries' job; reading is perception; only world-write is action.**

---

## §2 One mechanism, three gating classes (DECIDED)

The effector/executor/sandbox is **one module**, but every tool call it runs is routed into one of three
disciplines, by **operation × reach**:

| class | what | discipline / gate | example |
|---|---|---|---|
| **Local sense** | `inspect` on the machine | **subconscious, free** — frictionless; still grounded (never-fabricate) | read a local file; check a test result |
| **Distal sense** | `inspect` over the network | **subconscious + automatic, but inside a conscious-set policy + quota** (§7); offline-safe | fetch a page; read an external API |
| **World-change** | `mutate` / external write | **conscious, per-act decision** — explicit, witnessed at the watched seam | write a fix to the repo; send; deploy |

The grounding discipline (**never-fabricate** — imported reality must be real) and the sandbox /
least-privilege machinery apply to **all three**; only the *conscious-decided, counts-as-action* property
is reserved for world-change.

---

## §3 The gate = the router + the conscious-set bounds (DECIDED)

The "gated executor" becomes the place that routes a call by (operation × reach) against the bounds the
conscious set, expressed as the run's **Scope ceiling** (01 §3.3a):

- **reads:** ceiling is **open for local**, **budgeted for the network** (§7). No per-read approval.
- **writes:** require the conscious's **per-act authorization** — the conscious *authored* this change.
- **a worker can never widen its own ceiling at runtime** — only the conscious gate (the network policy, or
  the per-act write decision) can (01 §6 Scope model).

This resolves the action stub's old open question — *"the boundary between a subagent's tool category-scope
and the action layer's real permission/sandbox enforcement."* The SubAgent **references** a tool by
category; the action gate **enforces** by (operation × reach) against the conscious-set ceiling. A `read`
category resolves to free (local) or budgeted (network) dispatch; a `mutate`/external category **cannot
enter a worker's ceiling** without the explicit gate (the conscious decision).

---

## §4 What is NOT action — moved out (DECIDED)

- **Minting / self-modification → the registries' self-improvement + self-access ladder** (01 §2.6/§2.8).
  **Invariant: an action tool never writes the *self*-substrate** (memory/registries/graph). Self-writes
  go through the self-improvement loop; the action layer only ever touches the world. The self-write
  *depth* gradient is the **self-access ladder** (L1 refine / L2 mint = routine, subconscious; L3 self-docs
  / L4 self-code = deep, gated by the **protected core** + eval keep-or-revert).
- **Reading → perception.** A read is sensing, not an action. The conscious does **not** issue a read
  command down into the machinery (the one-way mirror, 01 §2.1); it only sets **focus / scope** — *what* to
  attend to — and the subconscious freely figures out what to actually read inside that focus. Like the
  eye: pointing the gaze allocates perceptual resources; the *seeing* is automatic and changes nothing.
  Directed reads run on top of a constant **ambient** sense (the standing sensors / always-on perception)
  the conscious did not point at. **Reads still cross the watched seam for the grounding (never-fabricate)
  discipline, but they are not conscious-decided actions** (the seam note, 04-seams §4).

---

## §5 The conscious's two roles, re: action (DECIDED)

Both are deliberate; neither is "perform a read":

1. **Sets the bounds** — focus/scope for local sensing; on/off + quota for the network — occasionally, as a
   resource decision.
2. **Authors each world-change** — the per-act decision that crosses into action.

The subconscious senses freely *inside* those bounds and does the work. **The conscious never performs a
read; it frames it — and it owns every world-change.**

---

## §6 `execute` — split by the sandbox (DECIDED)

Running isn't cleanly read or write, so the **sandbox boundary** is the discriminator:

- **run-to-measure inside the sandbox = sense** (you run tests to *perceive* a result — the grounding
  probe). Subconscious.
- **run with external / persistent effect (escapes the sandbox) = world-change = action.** Conscious.

---

## §7 Network reach — distal sense, conscious policy + quota (DECIDED)

Network reads are **gated, but the gate is a *policy*, not per-read approval** — two levels:

- **Top (conscious, occasional):** the **policy** — is network sensing on? what's its **quota**? A
  resource-optimization decision (the network is an *enhancement to the senses*), set/adjusted now and
  then. This is the gate; it sets the `external`-category ceiling.
- **Bottom (subconscious, per-moment):** within an enabled policy + budget, the subconscious reaches the
  network **freely and automatically** as relevant — distal perception — spending against the quota.

**Offline is the default-safe state:** policy off or no network ⇒ the system runs entirely on local
perception, fully functional. The **quota is the efficiency lever** — tuned experimentally (does a tighter
budget force more efficient sensing?).

---

## §8 Mapping to current code — the delta (CODE-STATE → target)

| Today (CODE-STATE) | Delta to reach the model |
|---|---|
| Action "changes the world **and** imports ground truth"; the reality-sourcer's **reads cross the watched seam** like actions (`watched.go OpenToReality`) | reframe: reads are **perception** (grounded, not conscious-decided); action = **world-change only** (§1, §4) |
| ~~Tool categories `{inspect, mutate, execute, external}` — a flat tag set~~ **(SUPERSEDED — now built)** | The **two-axis taxonomy is built in code** (`Tool.Category()` = operation `{inspect,mutate,execute}` × reach `{self,local,external}`, `gateroute.go`); the gate reads off both (§2/§3). Delta **done.** |
| ~~Gated executor = least-privilege dispatch~~ **(BUILT)** | Now the **router by (operation × reach)** against the conscious-set bounds (`gateroute.go` `Route`); delta **done** for routing. |
| `mutate` could in principle target anything | **invariant: `mutate` tools target the world only**; the self-substrate is written via self-improvement, never a tool (§4) |
| ~~No network quota~~ **(BUILT)** | The **`external` budget** exists (`RouteBounds.NetworkEnabled`/`NetworkQuota`, `RouteDecision.QuotaExceeded`); a distal sense with policy off / quota spent is declined, **offline-safe** (`executor.go`). Delta **done** (numeric quota values still dev-tuned, per Open / carry-in). |
| never-fabricate breadcrumb + sandbox (protected core, 04 §1) | **kept and shared** across sense + act |

---

## §9 Decisions (this pass)

- **Action = world-change only.** Reading is perception; minting is self-modification — both leave the
  action layer. **DECIDED.**
- **The cut is EFFECT × SUBSTRATE**, not tool-possession; the four quadrants land in four mechanisms, one of
  which is action. **DECIDED.**
- **Three gating classes** (local-sense free / distal-sense budgeted / world-change conscious), one shared
  effector mechanism. **DECIDED.**
- **The gate routes by (operation × reach) against the conscious-set Scope ceiling**; a worker can't
  self-widen to network or write. **DECIDED.** (The operation×reach **category taxonomy is owned by
  01 §3.10a** — now adopted there as the two-axis seed set; this doc references it, does not re-decide it.)
- **Action never writes the self-substrate** (registries/memory) — invariant. **DECIDED.**
- **The conscious has two deliberate roles:** set the bounds (focus; network on/off+quota), author each
  world-change. **DECIDED.**
- **Reads cross the watched seam for grounding but are not conscious-decided actions** (paired note in
  04-seams §4). **DECIDED.**
- **`execute` splits at the sandbox** (in-sandbox = sense, escaping = action). **DECIDED.**
- **Network = distal sense under a conscious policy + quota; offline-safe; quota experimental.** **DECIDED.**

## Open / carry-in

- **Async / non-blocking world-change** (awake mode) + **bounded dead-time** for durability (the watched
  seam's `outstanding`/`MAX_OUTSTANDING`, 04 §4) — applies to writes and distal reads alike.
- **Numeric network-quota values** (dev-tuned, like the band-pass cutoffs in 04 §2.1).
- **Detecting the self-vs-world substrate** of a write target in code (a registered set of self-substrates /
  a path-namespace convention / a tool capability flag) — the mechanism behind the §4 invariant.
