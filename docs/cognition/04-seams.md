# Seams Subsystem — design discussion (WORKING DRAFT)

> Part of the **Cognition Architecture** — see [`00-INDEX.md`](00-INDEX.md).
>
> Status: **WORKING DRAFT.** Developed during the seams pass (2026-06-14). The two seams were already
> a faithful, disciplined port — so this pass **ratifies** that, **integrates** the seams with the
> locked subconscious (Capability/Context — [`01-subconscious.md`](01-subconscious.md)) and conscious
> (thought graph — [`02-conscious.md`](02-conscious.md)) models, and **adds** the temporal-routing +
> rate-matching objective the membrane lacked. Tags: **DECIDED** (architecture, settled) ·
> **PROPOSED** (this pass, not yet built) · **CODE-STATE** (what the code does today, honest). Where
> they disagree, CODE-STATE is the truth and PROPOSED is the target.

## Scope

The two **seams** between the three layers, of deliberately **opposite transparency**. A seam is a
**disciplined information channel** — it carries both a *discipline* (what it must not do) and a
*performance objective* (what it must optimize). **Neither seam is an arbiter:** the conscious
decides; the membranes only admit/surface (hidden) and dispatch/witness (watched).

The spine of this pass: **the seam is a latency-bearing information channel between two asynchronous
processes sharing finite compute.** Its *discipline* keeps the channel honest and grounded; its
*objective* maximizes information, lands each item where it is still relevant, and rate-matches the two
ends.

> **Landed-status note (the "objective" half is partly built now).** When this pass was written the code
> implemented only the discipline (a synchronous, memoryless membrane). Since then the **intake band-pass
> (§2.1)** has shipped (`internal/seams/bandpass.go`, flag `seam.band_pass`, **default-OFF** — it
> suppresses first-appearance transients, so it is opt-in to keep goldens byte-identical) and the
> **pending-injection buffer (§3.4)** exists (`internal/seams/pending.go`). Still genuinely open: the
> §4 `MAX_OUTSTANDING` back-pressure cap, anchored-injection/retracement over the live timeline (§3.1–3.3
> — `Reenter` is built in `internal/graph`, but the seam-proposes-anchored-injection wire is not), and the
> §5 rate-matching control variable. Read §2–§5 "PROPOSED" tags against this note.

---

## §1 The discipline — constraints (DECIDED, mostly built)

Each seam enforces exactly one core invariant, and neither decides content.

| | **Hidden seam** (Sub→Con) | **Watched seam** (Con↔Action) |
|---|---|---|
| Transparency | invisible — the one-way mirror | visible — witnessed by design |
| Invariant | **honesty** (no laundered hallucination) | **grounding** (no fabricated reality) |
| Decides content? | No — surfaces survivors, Controller picks | No — dispatches, reality decides |
| Protected core | **Filter** (honesty floor) — frozen | **never-fabricate** breadcrumb — frozen |
| Tunable skin (tuned in dev, frozen at runtime) | Gate bias, Transform voice, band-pass cutoffs | bridge mapping, async latency |

**SR-1 — seam-as-channel (CODE-STATE, `hidden.go:9`).** The Gate *orders* survivors and reports
whether **more than one survived** (competing alternatives exist) but **never resolves the
competition** — "Reconciling competing ideas is the Conscious layer's job, not the membrane's." The
Controller owns the BRANCH spine.

**Protected spine vs tunable skin (DECIDED).** The Filter's honesty floor and the watched seam's
never-fabricate breadcrumb are **protected core** (immutable, anti-wireheading — 01 §2.8). The Gate's
ordering bias, the Transform's voice, and the band-pass cutoffs (§2.1) are **tunable — but tuned
OFFLINE in dev (the keep-or-revert experiment loop) and FROZEN at runtime** (Q1). So the membrane never
drifts in production: no online learning, hence no online wireheading; all tuning happens under
supervision, inside the stability budget. *The membrane refuses to fake and refuses to decide — those
two refusals are the whole job.*

**Pattern split per organ (CODE-STATE).**
- **Filter = Pattern C** — deterministic floor (`control.ScoreAdmit`) + optional model ceiling
  (`JudgeAdmission`). The **structural facts** — `refuted_by_reality` / `contradicts_belief` /
  `asserts_ungrounded_observation` — are **NEVER escalated**; the floor is authoritative (the model may
  not launder a reality-refuted claim back in). `hidden.go` `Filter.Admit` (~`:118`).
- **Gate = Pattern A** — `control.Rank` + per-domain bias, **NO model ever** (closed-form ordering).
  `hidden.go` `Gate.Select` (~`:310`, `control.Rank` at ~`:329`).
- **Transform = Pattern B** — `backend.Transform` re-voices the winner; on model failure it **surfaces
  the raw winner text, never a template** (no faked intelligence reaches the stream). `hidden.go`
  `TransformToNarrative` (~`:419`).

All three organs are **bypass-not-delete gated** (`seam.hidden_filter/gate/transform`): OFF →
admit-all / rank-identity / raw-relay — wire preserved, decision skipped. An **OFF-by-default
legible-generation shadow** parses an in-band control tag and measures parity against the floor (an
interpretability instrument; §6 future direction).

---

## §2 The objective — performance (PROPOSED — this pass)

The discipline is the *constraint*; the three jobs below are the *objective function* the membrane
optimizes. (This is the half the current code does not yet have.)

| | **Hidden seam** | **Watched seam** |
|---|---|---|
| **Objective** | **max signal, suppress noise** | witness every effect, import real ground truth |
| **Latency modeled?** | **No today — instantaneous → model capture→inject Δ** | Yes — async dead-time in ticks |
| **Item targeting** | head-of-stream → **anchored to a decision node; retracement / drop** | acting branch — **already anchored via `BranchID`** |
| **Stateful?** | No (memoryless per `Relay`) → **pending-injection buffer (anchors + decay)** | Yes (the `outstanding` set) |
| **Rate role** | observe production rate | observe consumption + feed the rate actuators |

1. **Information throughput (idea 1).** Maximize signal, suppress noise, to keep the conscious stream
   clean — an objective *above* the per-candidate honesty floor. **Realized as a band-pass filter
   (§2.1)** — never an ad-hoc gate; a true-but-not-yet-relevant signal is *held and anchored* (§3), not
   discarded.
2. **Latency + timing (idea 2).** The hidden seam has a non-zero capture→inject round-trip, today
   treated as instantaneous. Model it, and route each injection to the graph point where it is still
   relevant. (§3.)
3. **Synchronization (idea 3).** The seam is the *only* place that sees both the producer
   (subconscious) and consumer (conscious) rates; it observes the mismatch and feeds the actuators.
   (§5.)

### §2.1 The band-pass filter — noise-suppression as loop-shaping (DECIDED — Q3)

Noise-suppression is **not an ad-hoc relevance gate** — it is a **band-pass filter** in the
control-theoretic sense, so it matches (and is bounded by) the durability/stability framework. Two
complementary filters do the two halves of "max signal, kill noise":

| Filter | Passes | Rejects | Cognitive job | Control analogue |
|---|---|---|---|---|
| **Low-pass (LPF)** | persistent / corroborated signal | high-frequency transients | kill the flash-in-the-pan hallucination — a one-tick spike isn't real | integral-like; adds phase **lag** |
| **High-pass (HPF)** | novel / changed signal | the constant known background (DC) | inject only what *adds* information; let the already-known fade | derivative-like; adds phase **lead** — the **D** of the PID regulator |

Together = a **band-pass**: inject only what is *persistent enough to be real* (LPF) **and** *novel
enough to be worth it* (HPF). The stream stays clean from both sides — not flooded by transient noise,
not flooded by restatement of the known.

**Stability tie (why control theory, not a gate).** The seam sits in the **feedback path** of the
cognitive loop, so the filter shapes the loop response and its **cutoff frequencies are control
parameters inside the same stability budget the regulator enforces** (`n<1`, `0<K·g<2`). The two
filters pull the margin in **opposite directions** — LPF adds lag (more noise rejection, less margin),
HPF adds lead (more responsiveness, more high-freq noise) — so balancing them *is* classical
**loop-shaping**, mirroring the PID regulator (I ≈ low-pass, D ≈ high-pass). A band that is too
wide / high-gain raises the injection rate → pushes excitation `n` toward 1 → runaway; so the cutoffs
are *how* the seam keeps its share of `n` subcritical. `thought stability` validates it as part of the
loop.

**Determinism.** Discrete-time filters over **TICKS**: the low-pass is an EMA
`y[t] = (1−α)·y[t−1] + α·x[t]`; the high-pass is `x[t] − LPF`. Cheap deterministic recurrences — no
wall clock, no RNG — fitting the tick-clocked engine.

**What it unifies.**
- **Relevance-decay = the HPF time constant.** A late injection's novelty fades as the conscious
  absorbs / moves past the topic → the HPF attenuates the aging signal → that *is* drop-as-stale (§3.2),
  in frequency terms. The decay is the cutoff, not an ad-hoc function.
- **The run-ahead bet (§5) gets its mechanism.** A real "light-bulb" late injection is
  high-novelty / high-information → passes the band-pass strongly → earns a retracement; a
  stale / redundant one is attenuated → dropped. The filter retracts *only* for insights that matter.
- **Placement.** LPF (persistence / corroboration) sits on the **Filter / admission** side
  (honesty-adjacent — a one-tick spike can't be trusted); HPF (novelty / staleness) sits on the
  **Gate + held-buffer** side (relevance / timing-adjacent). Honesty and relevance stay cleanly
  separate. **The LPF *cutoff* is tunable skin** (tuned offline, frozen at runtime, §1) — it does **not**
  touch the Filter's **honesty floor** (protected-core, immutable): persistence-filtering and the
  never-launder honesty test are *separate knobs on the same admission organ* — one tunable, one frozen.

---

## §3 Hidden seam — from instantaneous membrane to temporal router

**CODE-STATE.** `HiddenSeam.Relay` (`hidden.go` ~`:500`) admits each raw candidate, the Gate picks a
winner, Transform re-voices it, and the result is injected **at the head of the stream** as a
`Thought{ID:-1, Source:INJECTED}` — synchronous and anchorless (the §3.4 pending-injection buffer in
`pending.go` adds the stateful path opt-in).

**The upgrade (PROPOSED).**

- **3.1 Anchored injection.** Each candidate carries the **thought-graph anchor** it is relevant to
  (the decision node) plus a **grounding-provenance stub** (what the SubAgent actually observed, so the
  Filter checks honesty against the *work*, not just the conscious history). The watched seam already
  does the primitive version — it returns each async observation paired with its `BranchID` + `Claim`
  (`watched.go:187`). The hidden seam **generalizes that anchor.**
- **3.2 Relevance over the episodic timeline.** An injection's relevance **decays with the conscious's
  distance from its anchor**, computed over the **episodic timeline** (02 §2a), not the graph. This
  decay **is the high-pass cutoff** (§2.1) — staleness is novelty-attenuation, not a separate rule.
  Three outcomes: **inject-at-head** (still relevant now) / **propose a retracement** (relevant to a
  passed decision) / **drop-as-stale** (no longer relevant anywhere).
- **3.3 Retracement = propose, don't drive.** On a passed-decision injection the seam **proposes**
  re-entry: it hands the Controller the injection anchored to the nearest **decision node** + emits a
  **retracement event**. The Controller fires the `Reenter` MCP op (02 §2b) and picks
  re-traverse / skip / branch. The mirror stays one-way (the conscious experiences *"a new thought
  about this earlier decision,"* not *"the seam moved me"*); **nothing is overwritten — the graph
  forks, the timeline appends.**
- **3.4 Statefulness + determinism.** The hidden seam becomes a **pending-injection buffer**
  (anchors + decay), where `Relay` today is memoryless. **Hard constraint:** the latency/decay must be
  modeled in **TICKS** (as the async watched seam already does), never the wall clock — the golden
  oracle requires it.

---

## §4 Watched seam — sync, async, and the grounding bridge (mostly built)

**CODE-STATE** (`watched.go`):
- **Sync** (`WatchedSeam.OpenToReality`): act, block until reality returns, surface a high-prior
  `OBSERVATION` thought (conf 0.95 ok / 0.9 fail).
- **Async** (`AsyncWatchedSeam.Fire/Poll`): fire non-blocking, feedback returns `Latency` ticks later
  (**deterministic, default 2**) as a `PolledObservation` paired with its `BranchID` + `Claim`. This is
  the **dead-time** the durability analysis bounds.
- **The grounding bridge** (`FrontActuator.Act`): **structured** (intention carries `Tool`+`Args` →
  direct dispatch, the target) / **scraped** (regex via the unified `action.SelectTool` — the grounding
  fix) / **none** (explicit bridge failure, never silent).
- **never-fabricate:** the offline `heuristicAct` stand-in stamps every made-up outcome
  `Fabricated:true`, `GroundsReality()=false` — the grounding loop must never store it as truth.
  **Protected core.**

**§4.1 Read vs write — the seam carries both, but only *write* is action (DECIDED — action pass, 03).**
The watched seam dispatches *both* a **perception** (`inspect` — the reality-sourcer reads a file / runs a
probe) and an **action** (`mutate` / external write — change the world), and today it treats them the same
(both `OpenToReality` → an OBSERVATION). They are not the same. The **grounding invariant (never-fabricate)
applies to both** — any imported reality must be real, whether read or write. But the **explicit /
conscious-decided / counts-as-action** property applies **only to a world-write**: a *read* is **perception**
(subconscious, frictionless, *resourced* by the conscious's focus — not commanded; [`03-action.md`](03-action.md)
§4/D1); a *write* is the conscious's **authored, watched action**. So the seam's *discipline* is one
(grounding), but its *gating* forks by the tool's **category × reach** (03 §2/§3): local read = free, network
read = budgeted (03 §7), world-write = conscious-gated. This **refines** INDEX's *"only world-changing action
crosses the watched seam"* — reads cross it for **grounding**, not as actions. (Async non-blocking applies to
distal reads too, not only writes — see the outstanding-set cap below.)

**Gap (PROPOSED) — bounded outstanding actions.** Fan-out is bounded (`W_max=8`); the **outstanding
set is not** — `OutstandingCount()` is read for durability accounting, but `Fire` has no cap. Add a
`MAX_OUTSTANDING` back-pressure cap mirroring `W_max`, for bounded dead-time.

---

## §5 Rate / frequency matching (PROPOSED — idea 3)

The seam **observes both rates** (subconscious *production* = candidates arriving; conscious
*consumption* = thoughts processed) and **feeds the rate actuators** (the V(s)-keyed scheduler + the
regulator). It does **not** duplicate the actuator — the regulator owns durability (`n<1`, `U≤1`,
`0<K·g<2`); the seam supplies the **cross-layer rate-error** the regulator/scheduler act on. (It is the
only vantage that sees *both* sides.)

**The staleness↔rate link.** Idea-2 staleness is *caused by* idea-3 mismatch: a subconscious that
outpaces the conscious creates the backlog whose injections go **stale**; a conscious that outpaces the
subconscious **starves** (thinks alone). So the two are one loop — **rate-match at the source keeps
injections fresh; temporal routing (§3) handles the stale residue.** "Neither outpaces the other" *is*
producer/consumer schedulability — the `U≤1` / `λ̄ = μ/(1−n)` the durability math already expresses;
the seam makes it a **measured, actuated** quantity.

**Conscious-runs-ahead (DECIDED — EXPERIMENTAL, Q2).** We do **not** force lockstep: the conscious is
allowed to outpace the subconscious, and **retracement (§3) is the catch** for late insights — the
band-pass (§2.1) ensures only high-novelty late injections trigger a retracement, while stale ones
decay away. This is a **hypothesis to validate** (does the late-retract actually recover the insight in
practice?), not an assumption.

**Two knobs the user named:** **hardware** (real compute share between the layers) and **software**
(the tick-frequency ratio). *Open:* which is the control variable, and the exact mechanism.

---

## §6 Decisions

**Resolved this pass:**
- The seam is a **latency-bearing channel** with a *discipline* (constraints) + an *objective*
  (info / timing / sync). **DECIDED.**
- Retracement is **non-destructive**: a focus-shift / re-entry, never an overwrite — **graph forks,
  timeline appends.** **DECIDED.**
- The **seam proposes** re-entry (anchor + retracement event); the **Controller fires `Reenter`** and
  picks the outcome. The one-way mirror stays intact. **DECIDED.**
- Re-entry is **branch-granular first**; node-granular deferred until a benchmark shows the coarser
  anchor loses the thread. **DECIDED.**
- **Protected spine** (Filter honesty + never-fabricate) frozen at runtime. **DECIDED.**
- **Seam params (Gate bias + band-pass cutoffs) are tuned OFFLINE in dev** (keep-or-revert) **and
  FROZEN at runtime**, inside the `n<1 / 0<K·g<2` budget — no online learning, no online wireheading
  (Q1). **DECIDED.** *(Supersedes the earlier "tunable skin adapts": the skin is tuned-but-frozen, not
  online-adaptive.)*
- **Noise-suppression = a tick-domain band-pass** (LPF on admission + HPF on surfacing), loop-shaped to
  the stability framework (§2.1) — not an ad-hoc gate (Q3). **DECIDED.**
- **Relevance-decay = the HPF cutoff; placement = LPF→Filter, HPF→Gate** — resolves the old
  "where does noise-suppression live / what is the decay function" opens. **DECIDED.**
- **Conscious runs ahead; retracement is the catch — EXPERIMENTAL** (a hypothesis to validate, Q2).
- Bounded outstanding async actions (`MAX_OUTSTANDING`). **PROPOSED.**

**Still open (carry to the next pass):**
- The **rate-matching control variable** + mechanism (hardware compute-share vs tick-frequency ratio).
- The precise **decision-node detection** rule (fork / ACT / conflict node — which, and how marked).
- Numeric **cutoff / budget values** for the band-pass (the dev-tuning targets that keep `n<1`).
- **Validating the run-ahead bet** (Q2) — does late-retract recover the insight on a real workload?

---

## Future direction — legible generation

The OFF-by-default shadow lets the generator emit an in-band tag **predicting** the seam's routing,
measured for **parity** against the floor. If parity climbs, generation is becoming *legible* — the
model learning to predict/explain the control floor. This is the seam's natural **self-improvement /
interpretability** thread; parked here exactly as the conscious doc parks metacognition.
