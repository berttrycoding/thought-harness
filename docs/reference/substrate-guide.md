# Substrate guide — the thinking backends, how each connects, and how it's driven

Technical reference for the harness's thinking substrates: what each one is, the call SHAPE
(per-call HTTP vs spawned process vs spooled worker), auth, what it reports, and the worker patterns
for the spooled path. The canonical selection layer is `internal/llm` (`CanonicalSubstrate`,
`SubstrateMenu`, `ClassOf`, `ResolveSubstrate`, `MakeBackend`); see
[[reference-substrate-class-vs-backend-label]].

> **Cost / billing is NOT characterized here.** The substrates have different call shapes (below),
> which MAY have different cost characteristics — but this has not been measured, so this doc makes
> NO billing claims either way. Treat "which substrate is cheaper" as an open, to-be-measured
> question, not an assumption.

## Verified working (2026-06-13)

Both Claude paths were confirmed end-to-end via `thought doctor` on Sonnet — every content role
returned real output (including the JSON `Filter.judge_admission` escalation role, which used to
parse-fail):

- **`claude` (direct spawn):** `doctor --backend claude --llm-model sonnet` → all roles OK. Required
  the `--bare` removal (below); before that fix every content role returned empty ("Not logged in").
- **`session` (spool worker):** a `claude -p` worker (`tools/session-worker.sh WORKER_CLI=claude`)
  serviced every `doctor --backend session` probe through the spool → all roles OK. Same looping
  `claude -p` worker mechanism `cc-lane.sh` runs in a kitty tab, launched directly.

Both were cleaned up after (worker killed, spool removed — the token-burn rule).

A small grounding bench then ran end-to-end on `claude:sonnet` (3 Tier-A items, 85 model-calls, cost cap
respected): harness 2/3 vs bare 0/3 (+0.667), gate contributes +0.333, isolation 1.00, NEEDS-MORE-N at
N=3. Token accounting works (799k tokens, 98.7% cache-hit) — the efficiency axis is live on `claude`
direct. Full write-up + the substrate-tag fix it surfaced: `docs/internal/notes/2026-06-13-grounding-on-claude-smoke.md`.

## The menu

`SubstrateMenu = auto · frontier · local · session · claude · test` (the TUI Settings picker and the
CLI `--backend` flag share this; aliases `llm`/`lmstudio`/`openai`→local, `cc`→session,
`api`→frontier, `none`→test all normalize through `CanonicalSubstrate`).

`auto` is a POLICY, not a backend: it resolves to a configured `frontier` if reachable, else `local`,
else errors (no offline product path). `ClassOf(backend)` reports the running class (stamped at
construction, never parsed from labels).

**Current dev/bench default = `claude` (masterplan D4, claude-first).** Reach a frontier model via
`claude` (direct per-call `claude -p` spawn) or `session` (the spool, answered by a SPAWNED `claude -p`
worker — NOT the open dev session). The `thought` job-queue MCP is deliberately OUT of the shared
`.mcp.json` (only `tui`/kitty is there — `tools/mcp/README.md` is the map), so dev sessions never
service cognition. `frontier` (HTTP API) and `local` (LM Studio — deferred, W6) are configured paths,
not the dev default. The kitty TUI UAT runs offline on `--backend test`.

## The five real substrates

### `test` — the deterministic double
Offline, no model, no network. `backends.TestBackend`. Content roles return canned/deterministic
text; control roles (the Pattern-A math) are real. Used for tests + UAT of the *plumbing* (the
mechanisms run; only the language is faked). Zero cost. NOT a product path.

### `local` — LM Studio (OpenAI-compatible HTTP)
- **Shape:** one HTTP POST to `/v1/chat/completions` per content call (`internal/llm/openai.go`),
  default endpoint `http://localhost:1234/v1`, default model `google/gemma-4-26b-a4b`.
- **Auth:** local key `lm-studio` (none, effectively).
- **Reports:** `usage` tokens (prompt/completion/cached) natively; temperature controllable.
- **Driver:** the GPU. Model load/unload via `lms` CLI. The bench model-swap GUARD + `runs/gpu.lock`
  apply here only.

### `frontier` — remote OpenAI-compatible API
- **Shape:** one HTTP POST per content call, same client as `local`, to a remote endpoint.
- **Config:** `ANTHROPIC_API_KEY` → `https://api.anthropic.com/v1` (default model `claude-sonnet-4-6`);
  OR `THOUGHT_LLM_API_KEY` + a remote (non-localhost) `THOUGHT_LLM_BASE_URL`.
- **Reports:** `usage` tokens; temperature controllable. The API-credit path (not the Claude Code
  subscription).

### `claude` — the Claude Code CLI bridge (spawned `claude -p`, per call)
- **Shape:** the engine SPAWNS a fresh `claude -p` subprocess PER content call
  (`internal/llm/claudecode.go` → `claudeArgs` + `claudeExecTransport`). Each spawn is a new process
  and a new context — no caching across calls; ~5–6s startup+inference per call observed.
- **Args:** `claude -p <user> --output-format json --no-session-persistence --tools "" [--model M]
  [--system-prompt S]`, run with `cmd.Dir = os.TempDir()` (neutral cwd → no project-context leak).
  **`--bare` is deliberately NOT used** — it bypasses the keychain login, which made every call return
  `{is_error:true,"Not logged in"}` until removed (2026-06-13). See
  `internal/llm/claudecode.go` `claudeArgs`.
- **Auth:** the spawned process resolves the interactive keychain login (no token needed once `--bare`
  is gone), OR `CLAUDE_CODE_OAUTH_TOKEN` if exported. Verify with `thought doctor --backend claude`.
- **Tiers:** primary `sonnet` (`THOUGHT_CLAUDE_MODEL`), utility `haiku`
  (`THOUGHT_CLAUDE_UTILITY_MODEL`, "none" disables) — a `TieredBackend` routes trivial roles to utility.
- **Reports:** parses `usage.output_tokens` from the CLI JSON envelope (`parseClaudeEnvelope`).
  Temperature NOT controllable (no CLI knob).
- **Self-contained:** no spool, no separate worker — the engine spawns directly. Simplest to run.

### `session` (alias `cc`) — the spooled-worker bridge (a SPAWNED claude worker, not the dev session)
- **Shape:** the engine does NOT call a model directly. It writes each content call as a JSON job
  into a file SPOOL (`THOUGHT_SESSION_SPOOL`, default `/tmp/thought-cc-spool`) and BLOCKS on the
  result file (`internal/llm/sessionbridge.go`). A separate SPAWNED worker (`tools/cc-worker.sh`, a
  headless `claude -p`) services the spool via the WORKER-SCOPED `thought` MCP server (`thought
  mcp-serve` in `tools/mcp/worker-thought.json` — NOT the shared `.mcp.json`, which holds only `tui`):
  loop `session_get_job` → think → `session_submit_result` (the `cc_*` names are back-compat aliases).
- **Tiers:** job tier hints `session` / `session-utility` ride each job; the worker's own model answers.
- **Auth/cost driver:** lives in the WORKER, not the engine (see worker patterns below).
- **Liveness:** the worker touches a heartbeat each poll; the bridge's health probe reads its freshness.
- **Decoupled:** ONE worker session can service MANY jobs (the job stream is sequential through the
  spool); the engine is substrate-agnostic — any MCP-speaking CLI worker can answer.

## Worker patterns for the `session` substrate

The spool needs a worker. Two shapes:

1. **The open interactive session as worker (interactive only).** The Claude Code session you are
   in services the spool through the `thought` MCP tools. Used for INTERACTIVE TUI testing: you drive
   the TUI (`--backend session`) and your session answers each job. A session canNOT be both the
   orchestrator and the blocking worker, so this only works when a *human-driven* surface (the TUI)
   posts jobs and the same session answers between turns.

2. **A spawned worker session (kitty tab).** `tools/cc-lane.sh worker` spawns a kitty tab running
   `tools/cc-worker.sh` → `tools/session-worker.sh` (`WORKER_CLI=claude|opencode|custom`), which runs
   ONE persistent `claude -p` (or other CLI) session that LOOPS servicing jobs (Pattern A: internal
   loop). `cc-lane.sh keeper` respawns it between job batches. This is the established cc-lane
   validation pattern. The worker runs from a NEUTRAL home dir (clean-substrate rule).
   - opencode workers use Pattern B instead (external per-job loop, fresh context) —
     `WORKER_CLI=opencode`; see `docs/internal/notes/2026-06-13-opencode-worker-instructions.md`.

   **TIMING (gotcha, learned 2026-06-13):** the worker EXITS after ~20 consecutive empty polls
   (~10s). So it must be started ALONGSIDE the job source, not before it: start the worker, then
   *immediately* run the thing that posts jobs (`doctor --backend session`, the TUI, or the
   campaign). If you start the worker against an empty spool with no poster, it polls 20 times,
   reports "0 jobs serviced", and exits. Once jobs are flowing the worker stays alive (a serviced
   job resets the empty counter). `SESSION_WORKER_MAX_JOBS=N` bounds the total it services before a
   clean exit; `cc-lane.sh keeper` respawns it for long campaigns.

## `claude` (direct) vs `session` (kitty worker) — the technical difference

Both ultimately reach the same Claude models, but the CALL SHAPE differs:

| | `--backend claude` (direct) | `--backend session` + kitty `claude -p` worker |
|---|---|---|
| who calls the model | the engine, per content call | a separate persistent worker session |
| process per call | a FRESH `claude -p` spawn each call | none — one looping worker session handles many jobs |
| context across calls | none (each spawn is isolated) | accumulates within the worker session (provider caching may apply) |
| indirection | none | the file spool + the `thought` MCP server |
| setup | none (self-contained) | reconnect `thought mcp-serve` + spawn a worker |
| substrate-tag | `claude:<model>` | `cc:<tier>` (or the worker's `THOUGHT_SESSION_WORKER` label) |
| **token / efficiency metric** | **MEASURED** — the engine parses `usage.output_tokens` from each `claude -p` JSON envelope (`parseClaudeEnvelope`) | **NOT measured** — `submit_result` carries only `{id, content, error}`; the worker's tokens never flow back, so the engine's per-call token counts stay -1 (absent) |
| capability metric (pass/fail) | measured | measured (identical) |

Whether these two SHAPES differ in COST is **not measured** — do not assume. Two differences ARE
established: (1) mechanical — per-call fresh spawns vs one persistent looping worker through a spool;
(2) **metric coverage — `claude` direct measures per-call tokens (so the efficiency / tokens-per-
solved axis works), `session` does NOT** (the spool result has no usage field, so token-based metrics
read 0). CAPABILITY (pass/fail) is measured identically on both. To measure efficiency on `session`,
the submit protocol would need a token field (the worker reporting its own usage) — not built.

### Implication for the W5 campaign
The keep-rule decides on capability lift OR efficiency (tokens-per-solved). On **`claude` direct**
both axes work — a batch can be kept for being smarter OR cheaper. On **`session`** only the
capability axis works; efficiency reads flat (tokens 0 for both arms), so the "cheaper at flat
capability" branch can never fire there. If the campaign should be able to keep a flat-but-cheaper
batch, run it on `claude` direct (or add token reporting to the submit protocol first).

## Substrate hygiene (applies to all model substrates)
Persisted records carry `Meta.Substrate` (e.g. `claude:sonnet`, `cc:session`, `llm:gemma`). Keep
frontier/claude-minted state in a SEPARATE `--state` dir from local-minted state; never compare or
mix rows from different substrates as one dataset (the re-localization prerequisite). Temperature is
controllable on `local`/`frontier` only — never compare a temp-uncontrolled `claude`/`session` row
against a temp-held local row as if temp were held.

## Quick reference
```
# claude (direct) — self-contained, just probe it:
thought doctor --backend claude --llm-model sonnet

# session (spool worker) — start the worker, then IMMEDIATELY post jobs (the worker exits on ~20
# empty polls, so they must overlap). Kill the worker + clean the spool after (token-burn rule):
rm -rf /tmp/thought-cc-spool && mkdir -p /tmp/thought-cc-spool
WORKER_CLI=claude WORKER_MODEL=sonnet SESSION_WORKER_MAX_JOBS=15 \
  THOUGHT_SESSION_SPOOL=/tmp/thought-cc-spool bash tools/session-worker.sh &   # background
THOUGHT_SESSION_SPOOL=/tmp/thought-cc-spool thought doctor --backend session   # posts jobs now
pkill -f session-worker; pkill -f 'claude -p'                                  # kill after; verify dead

thought run --backend <sub> --mode reactive --prompt "..."   # one headless episode
thought tui --backend session                                # interactive: this session answers via the MCP
tools/cc-lane.sh worker                                       # the kitty-tab worker (long campaigns)

# A small grounding bench on claude (the "previous grounding test" shape). NOTE: bench is a SEPARATE
# binary — `go run ./cmd/bench` or build ./bin/bench; `thought bench` does NOT exist (it falls through
# to the TUI surface and dies headless with "could not open a new TTY"). Always cost-cap a metered run.
go build -o bin/bench ./cmd/bench
./bin/bench --bank internal/bench/banks/pilot --mechanisms grounding --tier A \
  --max-items 3 --replays 1 --backend claude --llm-model sonnet \
  --max-calls 250 --max-tokens 300000 --concurrency 3 \
  --out runs/grounding-claude-smoke/ledger.jsonl --report runs/grounding-claude-smoke/report.txt
pkill -f 'claude -p'   # belt-and-braces: kill any stray spawn after; verify 0
```

> **GOTCHA (2026-06-13): `bench` ≠ `thought`.** `thought` has no `bench` subcommand, so `thought bench …`
> silently launches the default TUI surface, which opens `/dev/tty` and aborts with
> `could not open a new TTY: open /dev/tty: device not configured` when run headless (piped / no
> terminal). That message is a **wrong-binary signal**, NOT an auth/backend problem — the `claude`
> substrate works headless fine (a direct `claude -p … --output-format json` returns real content +
> usage; `doctor --backend claude` is 8/8). Use `go run ./cmd/bench` or `./bin/bench`.
