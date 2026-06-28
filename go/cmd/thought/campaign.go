package main

// campaign.go — the `thought campaign` subcommand (W5 B3): run ONE registry-scaling batch
// end-to-end (snapshot → generate → funnel → bench A/B → keep-rule → keep/revert via the ledger),
// wiring the real campaign adapters. Dry-runs free on --backend test; the real run is --backend
// claude with cost caps, supervised. The decision logic + the loop are the tested internal/campaign.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/campaign"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
)

func cmdCampaign(argv []string) int {
	fs := flag.NewFlagSet("campaign", flag.ContinueOnError)
	state := fs.String("state", "", "the LIVE registry state dir (required) — the baseline a kept batch folds into")
	candPath := fs.String("candidates", "", "JSON file of candidate entries ([]funnel.Candidate) (required)")
	suitePath := fs.String("suite", "", "JSON file of held-out tasks ([]campaign.HeldOutTask) (required)")
	backend := fs.String("backend", "test", "substrate: test (offline dry-run) | claude (real, metered)")
	llmModel := fs.String("llm-model", "auto", "model id for --backend claude")
	batchID := fs.String("batch-id", "001", "batch identifier (names the staged dir + the ledger row)")
	batchSize := fs.Int("batch-size", 0, "cap candidates to the first N (0 = all)")
	maxTicks := fs.Int("max-ticks", 40, "engine ticks per held-out task")
	maxCalls := fs.Int("max-calls", 0, "per-arm model-CALL budget (0 = unbounded) — aborts loudly")
	maxTokens := fs.Int("max-tokens", 0, "per-arm TOKEN budget (0 = unbounded) — aborts loudly")
	batchBase := fs.String("batch-dir", "runs/campaign-batches", "where staged batch state dirs are created")
	concurrency := fs.Int("concurrency", 1, "held-out tasks run through N worker goroutines (parallel-scaling throughput; tasks are independent — fresh engine+backend each; tune to the substrate rate limit). 1 = serial.")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *state == "" || *candPath == "" || *suitePath == "" {
		fmt.Fprintln(os.Stderr, "campaign: --state, --candidates and --suite are required")
		return 2
	}

	// the held-out task suite.
	suite, err := loadSuite(*suitePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "campaign:", err)
		return 1
	}

	// the live store (the baseline; a kept batch folds into it). Loaded so its in-memory snapshot
	// reflects disk (the ledger snapshot + the with-batch seeding read it).
	live, err := persist.NewJSONLStore(*state)
	if err != nil {
		fmt.Fprintln(os.Stderr, "campaign: open live state:", err)
		return 1
	}
	if _, err := live.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "campaign: load live state:", err)
		return 1
	}

	substrate, makeBackend := campaignBackend(*backend, *llmModel)

	// the engine factory the bencher uses: a fresh engine on the chosen backend, seeded from a state
	// dir — "" means the LIVE baseline state, a batch dir means the with-batch (live+batch) state.
	newEngine := func(stateDir string) (*engine.Engine, error) {
		dir := stateDir
		if dir == "" {
			dir = *state // baseline arm = the live registries
		}
		st, err := persist.NewJSONLStore(dir)
		if err != nil {
			return nil, err
		}
		be, err := makeBackend()
		if err != nil {
			return nil, err
		}
		cfg := engine.DefaultConfig()
		cfg.Mode = "reactive"
		cfg.Seed = 7
		cfg.Store = st
		return engine.NewEngine(&cfg, be)
	}

	runner := &campaign.Runner{
		Gen:    campaign.FileGenerator{Path: *candPath},
		Funnel: campaign.RealFunnel{DedupTheta: 0.9},
		Bench: campaign.EngineBencher{
			Tasks: suite, MaxTicks: *maxTicks, NewEngine: newEngine,
			MaxTokens: *maxTokens, MaxCalls: *maxCalls, Concurrency: *concurrency,
		},
		Store:  &campaign.JSONLBatchStore{Live: live, BaseDir: *batchBase, Substrate: substrate},
		Ledger: campaign.StoreLedger{Store: live, Substrate: substrate},
		Rule:   campaign.DefaultKeepRule(),
	}

	if *backend != "test" {
		fmt.Fprintf(os.Stderr, "campaign: running on substrate %q (metered) — caps: %d calls / %d tokens per arm\n", substrate, *maxCalls, *maxTokens)
	}
	out, err := runner.RunBatch(*batchID, *batchSize)
	if err != nil {
		fmt.Fprintln(os.Stderr, "campaign: batch aborted:", err)
		return 1
	}
	printCampaignOutcome(out)
	if out.Verdict.Decision == campaign.Margin {
		return 3 // a margin batch is STAGED — the human decides (distinct exit code)
	}
	return 0
}

// campaignBackend resolves the substrate label + a backend factory for the bencher's engines.
func campaignBackend(backend, model string) (substrate string, makeBackend func() (backends.Backend, error)) {
	if backend == "test" {
		return "test", func() (backends.Backend, error) { return backends.NewTest(), nil }
	}
	be, err := llm.MakeBackend(backend, "", model, 0)
	sub := "claude:" + model
	if err == nil {
		sub = llm.ClassOf(be) + ":" + model
	}
	return sub, func() (backends.Backend, error) { return llm.MakeBackend(backend, "", model, 0) }
}

func loadSuite(path string) ([]campaign.HeldOutTask, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var suite []campaign.HeldOutTask
	if err := json.Unmarshal(data, &suite); err != nil {
		return nil, fmt.Errorf("parse suite %s: %w", path, err)
	}
	if len(suite) == 0 {
		return nil, fmt.Errorf("suite %s is empty", path)
	}
	return suite, nil
}

func printCampaignOutcome(out campaign.BatchOutcome) {
	v := out.Verdict
	fmt.Printf("\n=== campaign batch %s ===\n", out.BatchID)
	fmt.Printf("generated %d · admitted %d · Tier-1 %s\n", out.Generated, out.Admitted, passWord(out.Tier1Pass))
	fmt.Printf("capability lift: %+.1f%% (fixed %d, broke %d, p=%.3f)\n", v.Lift*100, v.Fixed, v.Broke, v.McNemarP)
	fmt.Printf("efficiency:      %+.0f tokens/solved (positive = cheaper)\n", v.EfficiencyDelta)
	fmt.Printf("VERDICT: %s — %s\n", v.Decision, v.Reason)
	fmt.Printf("action:  %s\n", out.Action)
}

func passWord(ok bool) string {
	if ok {
		return "PASS"
	}
	return "REGRESSED"
}
