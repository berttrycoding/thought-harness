# Makefile — build the `thought` Go TUI and make it callable by name from anywhere.
#
#   make            build the binary to ./bin/thought
#   make install    build + symlink ~/.local/bin/thought -> ./bin/thought (one-time; safe to re-run)
#   make tui        build + launch the TUI
#   make run        build + run the binary (no args)
#   make test       run the test suite
#   make clean      remove ./bin
#
# ~/.local/bin/thought (already on PATH) is a SYMLINK to ./bin/thought, so once `make install` has run,
# every later `make` (or any `go build -o bin/thought ./cmd/thought`) refreshes that same file and the
# `thought` command immediately becomes the latest build — no reinstall step needed.

BIN  := bin/thought
LINK := $(HOME)/.local/bin/thought
PKG  := ./cmd/thought

BENCH := bin/bench

# Grounding-bench recipe — the VALIDATED config, PINNED so a retry "just works" (the 2026-06-13
# lesson: encode the instrument in code/config, never a session's memory). REPLAYS=3 majority
# averages out frontier run-to-run variance (K=1 flips correct items on re-roll); the complete
# assessment — trace witness + numeric presence-fallback + LLM-judge ruler — is built into the
# scorer. Cost-capped + substrate-tagged automatically. Override on the command line, e.g.
# `make grounding-claude GROUNDING_REPLAYS=1 GROUNDING_ITEMS=3` for a quick smoke, or
# `make grounding-claude GROUNDING_SKIP=16 GROUNDING_ITEMS=16` for the next chunk (items 17-32).
GROUNDING_BANK    := internal/bench/banks/cc-lane-grounding
GROUNDING_ITEMS   := 16
GROUNDING_SKIP    := 0
GROUNDING_REPLAYS := 3
GROUNDING_MODEL   := sonnet
GROUNDING_OUT     := runs/grounding-claude

.PHONY: build install tui run test clean bench grounding-claude cognition-bench gap-probe

# The cognition benchmark + gap-finding probes (the cognitive-architecture measuring stick).
COGNITION_SUITE   := ../data/campaign/cognition-probe-001.json
GAP_SUITE         := ../data/campaign/probe-gapfind-001.json
PROBE_CONC        := 6

build:
	go build -o $(BIN) $(PKG)

install: build
	@mkdir -p $(dir $(LINK))
	@ln -sf $(CURDIR)/$(BIN) $(LINK)
	@echo "linked $(LINK) -> $(CURDIR)/$(BIN)"

tui: build
	./$(BIN) tui

run: build
	./$(BIN)

test:
	go test ./...

clean:
	rm -f $(BIN)

bench:
	go build -o $(BENCH) ./cmd/bench

# The pinned grounding-on-claude recipe (see the variables above). Writes a substrate-tagged
# ledger+report under runs/grounding-claude/. Kill any stray `claude -p` after a metered run.
grounding-claude: bench
	@mkdir -p $(GROUNDING_OUT)
	./$(BENCH) --bank $(GROUNDING_BANK) --mechanisms grounding --tier A \
	  --skip-items $(GROUNDING_SKIP) --max-items $(GROUNDING_ITEMS) --replays $(GROUNDING_REPLAYS) \
	  --backend claude --llm-model $(GROUNDING_MODEL) \
	  --max-calls 2000 --max-tokens 24000000 --concurrency 4 \
	  --out $(GROUNDING_OUT)/ledger.jsonl --report $(GROUNDING_OUT)/report.txt
	@echo "grounding-claude done -> $(GROUNDING_OUT)/report.txt  (kill stray claude -p if any)"

# The COGNITION benchmark: faculty engagement on claude, baseline vs the soft-policy tuning lever.
# This is a cognitive architecture — the measuring stick is "did the right faculty fire?", not answer rate.
cognition-bench: build
	@echo "=== cognition benchmark: BASELINE (default config) ==="
	./$(BIN) probe --cognition --suite $(COGNITION_SUITE) --backend claude --llm-model auto --concurrency $(PROBE_CONC) --max-ticks 8
	@echo "=== cognition benchmark: SOFT-POLICY ON (the validated tuning lever, +2 faculties) ==="
	./$(BIN) probe --cognition --suite $(COGNITION_SUITE) --backend claude --llm-model auto --concurrency $(PROBE_CONC) --max-ticks 8 --enable conscious.activity.soft
	@echo "cognition-bench done (kill stray claude -p if any)"

# The gap-finding probe: where does the BASELINE harness fail (answer-oracle) — the capability target list.
gap-probe: build
	./$(BIN) probe --suite $(GAP_SUITE) --backend claude --llm-model auto --concurrency $(PROBE_CONC) --max-ticks 6
	@echo "gap-probe done (kill stray claude -p if any)"
