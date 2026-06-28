# Stability under dynamic dimensionality — the durability math, re-derived

> Status: implemented + validated. `thought_harness/internal/stability/stability.go` + `tests/test_internal/stability/stability.go` +
> the `/stability-check` skill. Conditions measured, not asserted (Phase-0 discipline).

## The problem
The original durability analysis (`docs/spec/cognitive-os-durability-analysis.md`, `internal/regulator/regulator.go`)
established that thinking is durable iff it is a **regulated subcritical-seeded** regime:

    n < 1   subcritical branching       μ > 0   positive baseline (awake)
    U ≤ 1   schedulable                 0 < K·g < 2   regulator stable
    λ̄ = μ/(1−n)   stationary rate        ω·τ < PM   async dead-time bounded

Those conditions were stated for a **fixed structure**. But the Subconscious layer is now generative:
operators are minted, programs are synthesised, sub-agents fan out in parallel. So the plant the
regulator controls is **time-varying** — its dimension changes as the system thinks. Do the
conditions still hold, and how aggressively can we scale the parallelism?

## The control-theoretic framing
Map onto a control-theoretic framework:
- the **regulator is the P controller**: actuation `u = θ`, error `e = λ̂ − λ*`, gain `Kp = gain_K`.
- the **thought-stream excitation is the plant**.
- dynamic synthesis makes the plant **parameter-varying**; the concern is that the policy is both
  actuator and part of the measurement. The answers: bound the parameter variation, and/or
  gain-schedule.

## Phase-0, take 1 — and the modeling error it exposed
First characterisation fed the regulator a width-`w` parallel burst and read peak `n`:

| width w | 1 | 2 | 3 | 4 | 5 | 6 |
|---|---|---|---|---|---|---|
| peak n (proxy = admitted−1) | 0.00 | 0.35 | 0.70 | 1.05 | 1.05 | 1.40 |

That said "crosses the n=1 cliff at w≥4 ⇒ W_max=3." **But this was a measurement artifact.** The
regulator was estimating the branching ratio as `admitted − 1` — counting *every admitted candidate*
in a tick as offspring. Measuring the **actual forks** under a real width-2 parallel run:

    sub-agent fires: 4    actual forks (mcp.branch): 0    true branching ratio: 0.000
    proxy peak n (admitted−1): 0.81    ← conservative over-count

A parallel fan-out produced **zero forks**. The proxy over-counted by 0.81.

## The re-model — n is the recursive fork ratio, not the admit count
The `n<1` condition is the **Galton-Watson subcriticality** condition: mean *recursive offspring*
per individual < 1. In the thought process the recursive cascade is "a thought triggers specialists,
whose injections become thoughts that trigger more specialists." A **parallel workflow fan-out is not
that** — its `w` candidates are evaluated in one tick, collapse to **one gate winner** (voiced into
the active branch), and fork only on genuine conflict. They do not recurse.

So `n` is now measured as the **forks actually created** per tick (`internal/regulator/regulator.go`: `forked` →
`n = EMA(forks)`), and the raw fan-out load flows where it belongs:

| quantity | what it measures | does fan-out width drive it? |
|---|---|---|
| `n` (branching) | recursive forks / thought — the durability cliff | **No** — collapses to ≤1 fork |
| `λ̂` (intensity) | excitation events / tick — throughput load | **Yes** — this is the real cost |
| `U` (schedulability) | live branches / focus capacity | **No** — fan-out adds ≤1 live branch |

## Phase-0, take 2 — the corrected measurement
Driving genuinely wide parallel programs (width 2→8) through the real engine:

| width w | max fan-out | peak n | n<1? | peak U | λ̂ max |
|---|---|---|---|---|---|
| 2 | 2 | 0.00 | ✓ | 0.12 | 1.80 |
| 4 | 4 | 0.00 | ✓ | 0.12 | 2.40 |
| 6 | 6 | 0.00 | ✓ | 0.12 | 3.10 |
| 8 | 8 | 0.00 | ✓ | 0.12 | 3.80 |

**Branching `n` and schedulability `U` are flat across width; only `λ̂` (intensity) scales.** Parallel
breadth is a *compute / throughput* load, not a durability threat.

## The re-derived conditions (dynamic case)
The static conditions hold **at every tick** (time-varying, uniformly). What makes the
parameter-varying plant durable:

- **D1 — n is the recursive fork ratio**, bounded by the Controller's fork policy (`<1`), and
  *independent of fan-out width*. This is the key correction.
- **D2 — bounded horizon.** steps/depth/loops bounded ⇒ a finite set of reachable configurations,
  each for bounded duration. *(verify_program.)*
- **D3 — excitation-neutral growth.** minting grows the *vocabulary* dimension but not per-tick
  branching ⇒ the catalog may grow without bound without affecting `n`. *(test: catalog ×3, n
  unchanged.)*
- **D4 — fan-out is a compute/schedulability budget, not a durability bound.** `MAX_PAR_WIDTH`
  (`internal/program.go`) caps how many sub-agent calls a single tick launches. Default **8**, aligned with the
  regulator's `focus_capacity`, **and env-overridable (`THOUGHT_MAX_PAR_WIDTH`)** so it scales with
  the host's concurrency. The only thing that grows with width is `λ̂`; the regulator's θ control
  absorbs the intensity, and `n`/`U` are untouched.

**Claim.** With `n` measured as recursive forks (D1), bounded horizon (D2), excitation-neutral
vocabulary growth (D3), and fan-out treated as a scalable compute budget (D4), the system is
**uniformly durable at any parallel width** — durability does not constrain how aggressively the
harness parallelises. This keeps the recursion in the control-theoretic convergence regime, never
the divergence/chaos regime, independent of scale.

## What's enforced vs. measured
- **Enforced structurally:** horizon bounds + the (now compute-budget) fan-out cap by `verify_program`.
- D5 — sub-skill composition is bounded + acyclic (expands at build time to a bounded operator program; measured: a skill calling a sub-skill adds 0 forks, n stays 0). **Measured every run (Phase-0):** `internal/stability/stability.go` checks `peak_n < 1` (the true fork ratio),
  `peak_U ≤ 1`, `max_fanout ≤ MAX_PAR_WIDTH`, plus the static regulator conditions — over the whole
  run. The `/stability-check` skill and `tests/test_internal/stability/stability.go` both run this.

## The loop-gain regime — when `0<K·g<2` is real, vacuous, or a fail (C0a/C0b)
The `0<K·g<2` condition is an **asymptotic, closed-loop** stability property. Once the plant gain `g`
is *measured* from the regulator's history ring (`regulator/gain.go`, lag-1 regression), the condition
becomes a real failable check — but only **while the loop is actually closed**. Three measured regimes
decide how it is reported, so the verdict is never a hidden prior-pass nor a misleading FAIL:

- **Saturation = open loop.** When the control law is railed against a θ clamp it *cannot move θ*, so
  the loop is **open** and `g` is unidentifiable by construction. The `0<K·g<2` check is then **vacuous
  (NA)** — durability is governed by the other four boundedness conditions, with `λ̄ = μ/(1−n)` finite
  under `n<1`. **But which clamp matters (this is the soundness fix):**
  - **Railed at `ThetaMin`** (the gate fully relaxed because there is little to control — the awake
    steady state, `λ̂ < λ*`): a **benign** open loop. Regime `saturated-bounded`, the check passes
    vacuously. This is the legitimate awake-mode case.
  - **Railed at `ThetaMax`** (the gate at **maximum suppression**): the controller is *trying and
    failing* to bring `λ̂` down. A `ThetaMax` rail alone is not automatically benign — if `λ̂` is still
    far over setpoint, the controller has **lost intensity control**. So a `ThetaMax` rail requires an
    **intensity bound**: `λ̂ ≤ 1.5·λ*`. Within it (a transient overshoot the controller is holding) the
    regime stays `saturated-bounded`. Beyond it (e.g. driving `100·λ*` rails `θ` at `ThetaMax` while
    `λ̂` pins at 100) the regime is **`saturated-runaway-FAIL`** and `0<K·g<2` fails — a maxed-out
    controller that cannot suppress the plant is a control-loss, not a durable open loop.
- **Insufficient loop = vacuous, not a fail (C0b honesty).** A short reactive episode that opens,
  ratchets `θ` once toward a settle point, and quiesces is a **transient**, not a steady state — its
  asymptotic loop-gain is undefined. The honest `unidentified-active-FAIL` is reserved for a loop that
  **sustains** identifiable excitation (a persistently-moving `θ`) yet still cannot be identified.
  Operationally: fewer than `4·gainMinPairs` (=32) θ-*moving* sample-pairs ⇒ regime `insufficient-loop`
  (vacuous). The reactive scenarios (≈12–16 moving pairs over 15–30 ticks) fall here; the sustained
  anti-tautology probe (≈118 moving pairs) clears the horizon and still reaches the honest FAIL — so
  the grace is narrow and the tautology stays dead.
- **Closed + identified loop ⇒ real check.** `g` measured, loop genuinely moving: `0<K·g<2` is a real
  failable bool on the measured gain (regime `actively-controlled-stable`; a hot plant with `K·g≥2`
  fails it honestly).

**Zero-margin annotation.** `U≤1` passes at `U==1.0`, but with **no slack** — the scheduler is exactly
fully committed, so any added load tips to `U>1` (unschedulable). The stability output surfaces a
non-failing `U==1.00 zero-margin` warning so a boundary pass is not silently indistinguishable from a
comfortable one.

## Scaling further (the lever, if ever needed)
If a future variant needs *intensity* headroom (λ̂) beyond what fixed-gain θ control absorbs, the
answer is the **gain-scheduled-P** approach: schedule `gain_K` up with the active fan-out
width so the controller reacts to an intensity burst within the same tick. Until then, raising
`THOUGHT_MAX_PAR_WIDTH` (and `focus_capacity` for live-branch headroom) scales parallelism with no
durability cost, by the corrected model above.
