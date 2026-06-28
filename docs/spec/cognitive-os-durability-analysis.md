# The Cognitive Real-Time OS — Durability Analysis

*Does continuous specialist-triggering give a sustained (durable, not infinite) thought stream? A validation using branching/self-exciting processes, real-time scheduling theory, control theory, and the bare-metal interrupt model.*

> Layer names are Subconscious / Conscious / Action (originally framed as BACK / MIDDLE / FRONT in the v4 design; see `docs/reference/architecture-glossary.md` §0).

---

## 0. The claim under test

> Once prompted, the Subconscious↔Conscious interface continuously extracts from and triggers specialists, returning more thoughts to the main session. With Conscious able to trace thoughts and the Thought MCP searching the thought-space, does this yield an **ultra-durable thought stream** — sustained, but not infinite/runaway?

**Verdict:** Yes, *conditionally and provably*. Durability requires **subcritical excitation (n < 1)** plus **a positive endogenous baseline (μ > 0)**, held there by **feedback regulation**. Those three are exactly the awake-mode components (gate/filter, drives/default-mode, arousal control). Without the baseline you get reactive bursts that die; without subcriticality you get runaway; without regulation you drift to one or the other.

---

## 1. The thought stream as a dynamical system

### 1.1 Branching-process model (the trichotomy)
Treat each thought as a node that, when focused, spawns offspring thoughts by triggering specialists. Mean offspring = **R** (the *thought reproduction number*, analogous to epidemic R₀). For a Galton–Watson process:

- **R < 1** → extinction almost surely. The stream dies out → "stuck."
- **R = 1** → critical. Marginal survival, heavy-tailed (infinite-variance) durations.
- **R > 1** → supercritical. Explosive growth → runaway / obsessive spiral.

Open-loop triggering is therefore a **knife-edge**: generically you die or you explode. Sustained-but-finite is non-generic and must be *engineered*.

### 1.2 Self-exciting (Hawkes) model — the central result
Refine to a continuous-time intensity:

```
λ(t) = μ + Σ_{t_i < t} φ(t − t_i),     n = ∫₀^∞ φ(s) ds        (branching ratio)
```

- **μ ≥ 0** — baseline/immigrant rate = endogenous drives + default-mode firing.
- **φ(·) ≥ 0** — excitation kernel: a thought raises the rate of later thoughts.
- **n** — expected direct offspring per thought (≡ R in 1.1).

**Stationarity theorem (standard Hawkes result):** the process is stationary with finite mean rate iff **n < 1**, and then

```
λ̄ = E[λ(t)] = μ / (1 − n).
```

**Cluster size:** one immigrant (baseline) thought seeds a cluster of expected total size

```
E[|cluster|] = 1 / (1 − n).
```

**Reading off durability:**

| Condition | Behavior | Mode |
|---|---|---|
| n ≥ 1 | explosive, non-stationary | runaway / obsessive |
| n < 1, μ = 0 | finite clusters that die after the seed | **reactive** (prompt → burst → quiescence) |
| n < 1, μ > 0 | **stationary forever at λ̄ = μ/(1−n)** | **awake / durable** |

So the durable stream is precisely a **subcritical (n<1), baseline-seeded (μ>0) self-exciting process.** It cannot die (μ re-seeds every interval) and cannot explode (subcritical). This is the mathematical content of "sustained but not infinite."

### 1.3 Interpretation of the parameters
- **n** is set by the **Gate + Filter + arousal**: stricter admission / higher threshold → fewer injections survive → smaller n. Arousal scales excitation gain.
- **μ** is the **drives + default-mode** baseline: μ = 0 is reactive (assistant) mode; μ > 0 is awake (autonomous) mode.
- **"Stuck"** = local μ-starvation or φ-collapse → λ decays → triggers **ACT** (open an external information channel, i.e. inject fresh μ from reality) or **BACKTRACK**.
- **"Runaway"** = n → 1⁻ pushes λ̄ → ∞; the regulator (below) must pull n back.

### 1.4 Thinking as search (why the value signal is load-bearing)
The Thought MCP is graph search over the thought-space:

| MCP op | Search role |
|---|---|
| branch | expand the frontier (new search node) |
| rerank | priority / best-first ordering |
| expand | deepen a node |
| compress | prune / memoize (bound the open set) |
| backtrack (focus sibling) | DFS unwind |
| merge | detect & collapse duplicate states |

Durable thinking = **continuous best-first search fed by injections, kept in the n<1, μ>0 regime.** The search **heuristic is the value signal**. A* converges only with an informative heuristic; an uninformative one degrades to unbounded breadth-first wandering. This is why the value signal keeps surfacing as the missing organ — *it is the heuristic that makes the durable search converge rather than diffuse.*

---

## 2. Homeostatic control — holding the durable regime

Subcriticality is not self-maintaining; n drifts with arousal, fatigue, and content. A controller is required. **This is "the cost of being awake," made precise.**

### 2.1 Setpoint and control law
Regulate the thought rate λ to a setpoint λ\* by modulating an admission threshold θ (gate/filter strictness) — higher θ lowers effective n, hence lowers λ. Proportional control:

```
θ_{k+1} = θ_k + K · (λ_k − λ*)          (raise threshold when over-active)
```

Optionally add an integral term (eliminate steady-state error) and couple arousal a to drive μ(a).

### 2.2 Stability
Let g = −∂λ/∂θ > 0 (threshold suppresses rate). Linearize the error e_k = λ_k − λ\*:

```
e_{k+1} = (1 − K·g) · e_k        ⇒  stable iff |1 − K·g| < 1  ⇔  0 < K·g < 2.
```

Lyapunov function V(e) = e² is strictly decreasing in that range ⇒ λ_k → λ\* and the durable regime is an **asymptotically stable equilibrium**. Outside the range: under-damped oscillation (K·g near 2) or divergence (K·g > 2). So a bounded-gain regulator on the gate/arousal **converts the 1.1 knife-edge into a stable sustained operating point.**

### 2.3 Async action introduces dead-time (the act-while-thinking constraint)
Non-blocking action (fire, keep thinking, feedback returns later) inserts a transport delay τ between acting and observing. In the perceive→think→act→sense loop this is **dead-time**, contributing phase lag ω·τ. By the Nyquist/phase-margin criterion, the closed loop stays stable only while

```
ω_c · τ  <  phase margin        (ω_c = gain-crossover frequency)
```

**Plain meaning:** if you act much faster than reality returns feedback, the loop oscillates — over-acting before seeing results (impulsivity/instability). Durable async action therefore bounds *action rate relative to feedback latency*. This is a real, named constraint, not a metaphor.

---

## 3. The Real-Time Cognitive OS (bare-metal / embedded-C mapping)

The architecture is a **superloop + interrupts** real-time system, with one twist: the components are generative and the interrupt outputs are *transformed to be indistinguishable from main-loop output*.

### 3.1 Structural correspondence
| Embedded / RTOS concept | Architecture component |
|---|---|
| `main()` superloop | Conscious serial thought loop (generation) |
| ISR (interrupt service routine) | a specialist injection / a percept |
| Interrupt controller (NVIC/PIC), priority + vectoring | **Hidden seam**: Filter→Gate→Transform |
| ISR writing to a shared buffer | injection written into the thought stream |
| **the twist:** ISR output is re-voiced to read as main-loop output | **Transform** (narrative coherence; no other RTOS does this) |
| Scheduler (fixed-priority / EDF) | **Gate** (arbitration) |
| Task priority | **value signal** |
| Single CPU core | **bounded focus** (one EXPANDED branch) |
| Context switch | `focus()` (compress current, expand next) |
| Paging / swap to slower store | **compression** of stashed branches (lossy) |
| Watchdog timer (fires on stall) | Controller's **exhaustion / quiescence** detector → BACKTRACK/ACT/sleep |
| NMI (non-maskable interrupt) | high-salience **USER_INPUT** / urgent percept |
| DMA / background transfer | **consolidation** during IDLE/ASLEEP |
| Sleep / low-power mode | DROWSY/ASLEEP arousal states |
| Real-time clock / tick | **arousal-driven loop tick** |

### 3.2 Schedulability cross-check (independent confirmation of §1)
Model each active specialist/branch as a periodic task with compute Cᵢ and period Tᵢ on the single focus-CPU. Utilization:

```
U = Σ_i (C_i / T_i)
```

- **EDF:** schedulable (no missed deadlines, bounded backlog) iff **U ≤ 1**.
- **Rate-monotonic (m tasks):** sufficient if U ≤ m(2^{1/m} − 1) → ln 2 ≈ 0.693 as m→∞.
- **U > 1:** backlog grows without bound → **thrash** (the scheduling-theoretic form of explosion).

The schedulability cliff **U = 1** coincides with the branching cliff **n = 1**. Two independent theories (queueing/branching vs. real-time scheduling) agree on the threshold — strong corroboration that durable operation lives strictly *below* unity in both.

### 3.3 Interrupt semantics
- Specialists are **edge-triggered ISRs**: fire when their relevance crosses threshold (a hardware-interrupt-on-condition).
- The Hidden seam is the **interrupt controller**: it masks (Filter rejects), prioritizes (Gate), and vectors (Transform writes into the narrative). Filter = interrupt mask + validity check *before* the handler runs.
- USER_INPUT = **NMI**: preempts, cannot be ignored, forces an interrupt path (Controller `on_interrupt` → suspend/refocus).

### 3.4 Failure modes (named, with guards)
| RTOS failure | Cognitive form | Guard |
|---|---|---|
| Starvation | a high-value branch never gets focus | value-signal priority + aging |
| Priority inversion | a low-value branch blocks a high-value one | value re-rank / preemption |
| Overload (U>1) | branch backlog → thrash | shed load: compress/backtrack; raise gate θ |
| Livelock | endless ISR servicing, no main progress | watchdog → force ACT/BACKTRACK |
| Deadlock | two branches each await the other | Controller cycle-detection → merge/kill |
| Interrupt storm | injection flood drowns the stream | rate-limit μ, raise θ (regulation §2) |
| Jitter | unstable handoff latency → flow breaks | bound transform latency (fluency) |

---

## 4. Synthesis — the durable operating point

Combining the models, the **durable regime** is the intersection:

```
   n < 1            (subcritical excitation — gate/filter/arousal)
   μ > 0            (endogenous baseline — drives/default-mode)
   U ≤ 1            (focus not over-subscribed — scheduler)
   0 < K·g < 2      (regulator stable — homeostatic control)
                    (NOTE: on standing workloads the LIVE stability suite reports K·g as TELEMETRY-ONLY, not a measured pass — the plant gain is not identifiable; the other four bounds ARE measured. See CLAUDE.md.)
   ω_c·τ < PM       (async action delay bounded — control theory)
   informative value signal   (search converges — the heuristic)
```

Inside this set: a **stationary thought rate λ̄ = μ/(1−n)** sustained indefinitely — the ultra-durable stream, never extinct, never explosive, with the Thought-MCP performing convergent best-first search over a continuously-fed graph. Outside it: extinction (stuck), explosion (runaway), thrash (overload), or oscillation (impulsive/under-damped).

**Reactive mode** is the corner μ→0: a single subcritical cluster of expected size 1/(1−n) that runs to quiescence after each prompt — provably safe and self-terminating, which is exactly why an assistant is the safe special case.

---

## 5. Algorithms

### 5.1 Regulated durable loop (homeostatic)
```
state: θ (admission threshold), a (arousal), λ̂ (rate estimate)
loop every tick:
    percepts ← perceive(gain(a))                      # μ-channel + async feedback
    candidates ← specialists.fire_if(relevance > θ)   # excitation (sets effective n)
    if candidates empty and no goal:
        candidates ← drives.propose() or default_mode.wander()   # μ > 0 keeps it alive
    t ← Filter→Gate→Transform(candidates) or generate()
    append(t);  λ̂ ← ema(λ̂, instantaneous_rate())
    θ ← θ + K·(λ̂ − λ*)                               # CONTROL: hold subcritical
    a ← regulate_arousal(a, λ̂, drives, percepts)      # may → DROWSY/ASLEEP
    if watchdog.stalled(): force(ACT or BACKTRACK)     # anti-livelock
```

### 5.2 Stability self-check (run online)
```
assert 0 < K * estimate_gain(∂λ/∂θ) < 2        # §2.2 convergence
assert utilization(active_branches) ≤ 1        # §3.2 schedulable
assert action_rate * feedback_latency < margin # §2.3 no oscillation
assert max(branch.value) heuristic informative # §1.4 search converges
```

---

## 6. What this proves — and what it does not

**Proved (given the models):**
- Continuous specialist-triggering **alone** does not give durability — it is pure excitation and is knife-edge (dies or explodes).
- A **sustained, finite-rate, non-terminating** thought stream provably exists in a well-defined regime (n<1, μ>0, U≤1, regulated), with closed-form rate λ̄ = μ/(1−n).
- Two independent formalisms (branching/Hawkes and real-time scheduling) **agree** the boundary is at unity, and control theory supplies the stabilizer and the async-action delay bound.
- The architecture maps cleanly onto a **bare-metal RTOS** (superloop + prioritized ISRs + watchdog + DMA-consolidation), with one genuine novelty: the **Transform** makes interrupt output narratively indistinguishable from main-loop output.

**Not proved (open):**
- That an **informative value signal** (the search heuristic / task priority) is *learnable*. The regime's existence is established; its *efficiency* depends on this heuristic. **A concrete path now exists** (see spec §12): bootstrap it with LLM-propose-and-recommend as an approximate value function, correct it with reality-grounded RL (reward from confirmed OBSERVATIONs, never self-grading), and distill into a cheap learned V as the system compresses. This converts the open question from "does a heuristic exist" to "does grounded RL sharpen it enough" — an empirical question, not a structural gap.
- Exact kernel φ and μ for real cognition (the model is structural, not fitted).
- Global (non-linear) stability of the full coupled system beyond the linearized regulator.

**Bottom line:** the durable stream is real and characterizable, but it is a *regulated subcritical seeded* regime, not a free consequence of triggering specialists. The continuous mode buys durability; the bill is a controller and a value signal.
