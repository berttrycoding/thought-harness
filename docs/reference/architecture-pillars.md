# Architecture pillars — the recurring principles

The same handful of principles keep re-deciding the design across every subsystem. They are listed
here as **pillars**: each is a one-line rule, the reason it holds, where it lives in the code, and —
crucially — **how it is validated** (a pillar with no test is an aspiration). They are grouped:
generative, trust, measurement/testing, robustness, and architecture-shape.

These map to the harness's stack-portable invariants, lessons-learned, best-practices, and
benchmark-validation contracts. A pillar is portable across
languages — it's a contract, not an implementation.

---

## A. Generative pillars (build structure at runtime, don't hardcode it)

### A1 — Dynamic synthesis on the fly (toolmaker, not tool-user)
**Rule:** structure the system needs — workflows, operators, sub-agents — is *constructed from
context at runtime*, not enumerated in advance. The system writes its own tools.
**Why:** a fixed roster/pipeline can only handle shapes the engineer foresaw; real problems need
shapes assembled to match. (cap-op `concept-dynamic-rule-synthesis`.)
**Where:** `internal/cognition/synth.go` (programs built per goal), `internal/cognition/operators.go` (Mint) (new operators at
runtime), `internal/subconscious/subagent.go` (sub-agents defined per phase).
**Validated:** `the synthesis tests in internal/cognition`,
``.

### A2 — Progressive specialisation (accumulate; effortful → automatic)
**Rule:** verified synthesised components are *kept* and reused; the system gets cheaper and richer in
domains it has worked in, without retraining.
**Why:** cumulative cost should grow sub-linearly — later work reuses earlier work. (cap-op
`operator-specialize`; spec convertibility.)
**Where:** persistent `engine.catalog` (minted operators carry across episodes); `internal/convert/convert.go` (repeated
GENERATED pattern → minted Specialist; repeated METACOG → workflow/gate prior).
**Validated:** `the synthesis tests in internal/cognition`;
`the engine tests in internal/engine`.

---

## B. Trust pillars (never trust generated output on its own word)

### B1 — Verify before trust (the two-layer discipline)
**Rule:** anything synthesised — an operator, a program, a candidate thought — is checked by a
**separate, non-generating** verifier *before* it is used; invalid output is dropped, not run.
**Why:** letting the generator certify its own output collapses the asymmetry that makes the check
worth anything. (cap-op `concept-two-layer-architecture`, `concept-llm-as-uncertain-plant`.)
**Where:** `operators.OperatorRegistry.verify` (a minted operator must verify; seed operators are
**frozen invariants**), `program.verify_program` (known ops, bounded loops/size, before execution),
the **Filter** (`internal/seams/hidden.go` — a different component from the producing PrimitiveSubAgent).
**Validated:** `the synthesis tests in internal/cognition`,
``, ``.

### B2 — Producer ≠ verifier
**Rule:** the component that *makes* a contribution never *admits* it. Admission is a separate organ.
**Why:** self-certification hides exactly the failure modes you most need to catch.
**Where:** a PrimitiveSubAgent (a subconscious worker faculty) produces a raw `Candidate`; the
**Filter** (`score_admit`) judges it before the Transform voices it. The Critic is split: Filter
(admission) + Controller (executive).
**Validated:** `internal/engine/cognition_property_test.go` (Filter distrusts hedging / contradiction / reality-refuted claims).

### B3 — Least-privilege, default-deny
**Rule:** a component gets the *minimum* capability it needs; new capability is opt-in, never ambient.
**Why:** the moment a tool-bearing sub-agent exists, default-deny is the only safe construction.
**Where:** `internal/subconscious/subagent.go` `tool_scope` defaults to `()` (synthesise-from-context only) and is now
**load-bearing**: a sub-agent runs `executor.Scoped(tool_scope).Execute(...)`, where `Scoped` materialises a
registry holding only the scoped tools — so any out-of-scope call fails to resolve → deny (reality-wiring P2).
**Validated:** `the synthesis tests in internal/cognition` (empty default) + the P2 tool-execution tests.

---

## C. Measurement & testing pillars (definition-of-done is measured, not asserted)

### C1 — Measurement as Pillar-0 (baseline-gated, never auto-kept)
**Rule:** nothing is "better" or "done" without a *measured* comparison against a baseline; the gate is
a number, not a vibe. A change is kept only if it beats the baseline on a real metric.
**Why:** confident prose is not evidence; reward-hacking breaks benchmarks that don't pin ground truth.
(the Phase-0 verifier-characterization discipline; the measurement-as-Pillar-0 discipline.)
**Where:** `cogbench/` (reference answers + `must_not_include` traps, deterministic-first graders);
`docs/internal/notes/controller-design-proof.md` (heuristic vs LLM head-to-head, judge-validated 4/4 vs 2/4).
**Validated:** `cogbench` runs end-to-end (`python -m cogbench`); the controller proof is reproducible
(`thought compare S1 S3 S5 S8`).

### C2 — Test the cognition, not just the plumbing
**Rule:** automated tests assert on cognitive *properties* and *reasoning shape* — did it branch on
conflict, open to reality when stuck, keep multi-turn context, give up honestly — not merely that the
loop ran without crashing.
**Why:** a run can produce a passing artifact while reasoning badly; plumbing-only tests pass straight
through real cognition bugs.
**Where:** `internal/engine/cognition_property_test.go`; `cogbench` reasoning-shape grading (`Shape` assertions).
**Validated:** these tests caught real cognition bugs (awake-mode reality-correction not sticking) that
every mechanical test passed.

### C3 — Layered automated validation (unit → integration → live UAT)
**Rule:** three rungs, each catching what the one below cannot: headless unit/regression
(`pytest`), cross-harness integration on hard tasks (`cogbench`), and live visual UAT driving the real
TUI (`tools/kitty-mcp` + the `/uat explore` loop).
**Why:** render/layout bugs only appear on a real terminal; capability gaps only appear on hard,
domain-diverse tasks.
**Where:** `tests/`, `cogbench/`, `tools/kitty-mcp/`. A render-resolution test catches invalid colours
that build fine but blow up at paint time.

---

## D. Robustness pillars

### D1 — Bounded everything (the durability regime)
**Rule:** every loop, queue, and budget is bounded; nothing runs unbounded. Bounded focus (one branch
EXPANDED), bounded loops (`max_iter ≤ 6`), single-shot sub-agents, program size-cap `MAX_STEPS = 24`,
bounded fan-out (`MAX_PAR_WIDTH` default 8, a compute budget — decoupled from `n`), subcritical
excitation `n < 1`, schedulable `U ≤ 1`.
**Why:** durability (`λ̄ = μ/(1−n)`) is a *regulated regime*, not a free consequence; unbounded
anything breaks it.
**Where:** `internal/regulator/regulator.go`, `internal/cognition/program.go` bounds + `verify_program`, `internal/subconscious/subagent.go` `single_shot`,
`graph` focus capacity.
**Validated:** `the engine tests in internal/engine` (asserts `n<1`, `U≤1`);
`the synthesis tests in internal/cognition`.

### D2 — Graceful fallback (the system never breaks)
**Rule:** every dynamic/LLM path has a deterministic fallback; the whole system is observably working
offline with no API key. A failure degrades to heuristic + emits one event — it never crashes the loop.
**Why:** a harness that breaks when the model is slow/absent/unparseable is not a harness.
**Where:** every `Backend` method's heuristic fallback; `OpenAICompatBackend` → `HeuristicBackend` on
any error (`llm.fallback`); `operator_apply` typed fallback; `synth` falls back to the heuristic shape.
**Validated:** the test suite runs entirely on the heuristic/unreachable backend; `--backend llm`
never breaks.

---

## E. Observability & shape pillars

### E1 — Emit, never print — and log everything as data
**Rule:** components emit structured events on a shared bus; they never print. The bus is the
connective tissue, and the event log *is* the per-subsystem log. Synthesised structure (programs,
minted operators, sub-agent definitions) is logged in full — a dataset to later standardise and train.
**Where:** `internal/events/event.go`; `--log FILE.jsonl`; `subconscious.synthesize` / `subconscious.operator` / `subconscious.subagent`
events carry the full definitions; the unified model is built *from* the bus.
**Validated:** `the synthesis tests in internal/cognition...` (asserts the program dict is logged),
``.

### E2 — One unified, addressable model
**Rule:** every entity across every layer has a stable id and a place in *one* model; any entity's
cross-layer provenance is reconstructable.
**Where:** `internal/cogngraph` (process→subconscious→seam→conscious→critic→action, ids + provenance);
`internal/search/search.go` (the Conscious thought-stream as A* best-first search).
**Validated:** `test_internal/cogngraph` (all-layers coverage, cross-layer provenance, pure-from-the-bus
reconstruction).

### E3 — Opposite-transparency seams; pull, not push
**Rule:** structure determines visibility — the Subconscious→Conscious seam is hidden *by design* (validate before
voice), the Conscious↔Action seam is watched *by design* (intention out, reality in). Contributors fire on
relevance (pull); no orchestrator picks them.
**Where:** `internal/seams/hidden.go`, `internal/seams/watched.go`, `internal/subconscious/dispatch.go` (relevance-gated firing).
**Validated:** `internal/engine/cognition_property_test.go` (silent injection re-voices, never dumps; the gate forks on conflict).

---

## How to use this page
When adding a subsystem, walk the pillars: does it synthesise rather than hardcode (A)? is its output
verified by something else before trust (B)? is its "done" a measured gate (C)? is it bounded and
does it fall back (D)? does it emit + fit the one model (E)? A "no" is either a deliberate, documented
exception or a design smell.
