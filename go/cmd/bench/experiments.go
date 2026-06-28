package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/berttrycoding/thought-harness/internal/bench/eval"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
	"github.com/berttrycoding/thought-harness/internal/cost"
)

// ---------------------------------------------------------------------------
// The EXPERIMENT LEDGER: one append-only row per bench RUN.
//
// Every campaign — whatever its config — appends exactly ONE row to a single
// persistent file (runs/experiments.jsonl) so the whole sweep of experiments is
// revisitable in ONE place: which config (model / temp / concurrency / mechanisms
// / K / fixed-seed) produced which aggregate tokens, which $ estimate (and under
// which rate model), which cache-hit %, and the key per-mechanism metrics (lift
// harness−bare, gate-on−gate-off, σ_noise, verdict). It is APPEND-ONLY — a new
// run never rewrites or truncates the file; old rows stand as history.
//
// This is distinct from the per-MEASUREMENT ledger (runs/<name>-ledger.jsonl,
// one row per item×arm×replay cell): THAT is the raw keep/revert audit; THIS is
// the per-CAMPAIGN index over those audits. The two never collide.
//
// The wall-clock timestamp is recorded here (cmd/bench is a normal binary, so
// time.Now() is legitimate) — it is NEVER read from engine code (the engine stays
// deterministic on the seeded clock; CLAUDE.md "never call the wall clock in
// engine logic"). The timestamp lives only in this driver-side ledger row.
// ---------------------------------------------------------------------------

// experimentsLedgerPath is the single, fixed append-only experiment index. Every
// bench run appends one row here regardless of its --out (the per-measurement
// ledger) path, so the campaign sweep is queryable from one file.
const experimentsLedgerPath = "runs/experiments.jsonl"

// experimentRow is ONE bench campaign's index row. It is a flat, self-describing
// JSON object (schema_version-tagged) so the whole experiments.jsonl is jq-able:
// the wall-clock timestamp, the full config, the aggregate token accounting, the
// $ estimate + the rate model it was priced under, the cache-hit %, and the key
// per-mechanism metrics. Every field is JSON-tagged snake_case.
type experimentRow struct {
	// SchemaVersion lets a reader evolve the row shape without ambiguity (bump on
	// any breaking field change; readers branch on it).
	SchemaVersion int `json:"schema_version"`
	// Timestamp is the run's wall-clock completion time, RFC3339 (driver-side only —
	// never engine clock). RunID is a stable id derived from it for cross-reference.
	Timestamp string `json:"timestamp"`
	RunID     string `json:"run_id"`

	// Config is the campaign configuration that produced this row.
	Config experimentConfig `json:"config"`

	// Tokens is the campaign-wide aggregate token accounting (in/out/cached/reasoning),
	// summed across every model-call of every cell.
	Tokens experimentTokens `json:"tokens"`

	// Cost is the $ estimate + the rate model + the rate-card source it was priced
	// against, plus the cache-hit % (the discount lever). A local (unpriced) run
	// reports the PROJECTED $ under --rate-model instead of a billed $.
	Cost experimentCost `json:"cost"`

	// PerTick is the per-engine-tick spend rollup (the WF-E baseline headline):
	// mean/median LLM calls + tokens per tick, over the campaign's populated ticks.
	PerTick experimentPerTick `json:"per_tick"`

	// ModelCalls is the total backend call count; Wall is the measured wall-clock.
	ModelCalls int     `json:"model_calls"`
	WallSec    float64 `json:"wall_sec"`

	// Mechanisms is the per-mechanism×tier key-metrics array (lift contrasts,
	// σ_noise, verdict) — the experiment's substantive result, one entry per result.
	Mechanisms []experimentMech `json:"mechanisms"`

	// Contaminated is true iff the GPU/model GUARD caught a mid-run model swap (the
	// loaded model changed away from the pinned expected id while the campaign ran).
	// Every metric in this row was computed PARTLY against the wrong model — the row
	// stands as history but must NEVER be read as a clean result. Swap carries the
	// detail (expected/got/cell). Omitted (false/null) on a clean run.
	Contaminated bool        `json:"contaminated,omitempty"`
	Swap         *swapDetail `json:"swap,omitempty"`
}

// experimentConfig is the run's reproducibility knobs.
type experimentConfig struct {
	Backend     string   `json:"backend"`
	Model       string   `json:"model"`
	LLMURL      string   `json:"llm_url,omitempty"`
	Temp        float64  `json:"temp"`
	Concurrency int      `json:"concurrency"`
	Mechanisms  []string `json:"mechanisms"`
	Tiers       string   `json:"tiers"`
	KTierA      int      `json:"k_tier_a"`
	KTierB      int      `json:"k_tier_b"`
	SeedBase    int64    `json:"seed_base"`
	FixedSeed   bool     `json:"fixed_seed"`
}

// experimentTokens is the campaign-wide token split (the cost substrate).
type experimentTokens struct {
	TotalIn    int `json:"total_in"`
	UncachedIn int `json:"uncached_in"`
	CachedIn   int `json:"cached_in"`
	Out        int `json:"out"`
	Reasoning  int `json:"reasoning"`
	Total      int `json:"total"`
}

// experimentPerTick is the per-engine-tick spend summary (the WF-E baseline
// HEADLINE): the mean/median LLM calls per tick and tokens per tick, taken over
// every POPULATED (run, tick) cell across the campaign (a tick that fired >=1 model
// call). Ticks is that populated-cell count. All zero on the offline double (no
// llm.* events). This is the baseline-spend figure WF-E exists to cut.
type experimentPerTick struct {
	// Ticks is the number of populated (run, tick) cells the statistics span.
	Ticks int `json:"ticks"`
	// CallsPerTickMean / CallsPerTickMedian are the LLM-call count per populated tick.
	CallsPerTickMean   float64 `json:"calls_per_tick_mean"`
	CallsPerTickMedian float64 `json:"calls_per_tick_median"`
	// TokensPerTickMean / TokensPerTickMedian are the token total per populated tick.
	TokensPerTickMean   float64 `json:"tokens_per_tick_mean"`
	TokensPerTickMedian float64 `json:"tokens_per_tick_median"`
}

// experimentCost is the $ estimate + how it was priced. USD is the BILLED cost
// when every model is in the rate card (HasUSD true); for a LOCAL/unpriced run it
// is 0 and ProjectedUSD carries the local-vs-API projection under RateModel.
type experimentCost struct {
	// USD is the billed cost (meaningful only when Priced is true).
	USD float64 `json:"usd"`
	// Priced is true iff every model in the run was in the rate card (else the run
	// was local/unpriced and USD is not billable — see ProjectedUSD).
	Priced bool `json:"priced"`
	// ProjectedUSD is this run's tokens priced under RateModel (the local-vs-API
	// projection). Set when --rate-model resolves in the card; null otherwise.
	ProjectedUSD *float64 `json:"projected_usd"`
	// RateModel is the model id the projection used (--rate-model). Empty when no
	// projection was requested.
	RateModel string `json:"rate_model,omitempty"`
	// RateCard is the rate-card source the $ figures were priced against.
	RateCard string `json:"rate_card"`
	// CacheHitPct is the cache-HIT fraction of input tokens, as a percent (the
	// prefix-cache discount lever).
	CacheHitPct float64 `json:"cache_hit_pct"`
	// InputSharePct / OutputSharePct are the I/O token split, as percents.
	InputSharePct  float64 `json:"input_share_pct"`
	OutputSharePct float64 `json:"output_share_pct"`
	// UnknownModels lists the model ids the rate card did not price (so a partial
	// or fully-unpriced run is auditable). Empty when Priced is true.
	UnknownModels []string `json:"unknown_models,omitempty"`
}

// experimentMech is one mechanism×tier's key metrics: the two lift contrasts
// (point + CI), the σ_noise (raw + K-averaged), the feasibility gate, and the
// one-line verdict — the experiment's substantive read, persisted per row.
type experimentMech struct {
	Mechanism          string             `json:"mechanism"`
	Tier               string             `json:"tier"`
	K                  int                `json:"k"`
	Items              int                `json:"items"`
	Verdict            string             `json:"verdict"`
	SigmaNoise         float64            `json:"sigma_noise"`            // K-averaged harness σ (the gate value)
	SigmaNoiseRaw      float64            `json:"sigma_noise_raw"`        // pooled within-item harness σ
	FeasibilityGate    bool               `json:"feasibility_gate"`       // σ_noise < MDE/2
	HarnessMinusBare   experimentEstimate `json:"harness_minus_bare"`     // total lift
	GateOnMinusGateOff experimentEstimate `json:"gate_on_minus_gate_off"` // mechanism-specific lift
	GateOffSupported   bool               `json:"gate_off_supported"`
	IsolationRate      float64            `json:"isolation_rate"`
}

// experimentEstimate is a lift contrast point + its BCa 95% CI (NaN/Inf scrubbed
// to keep the row valid JSON — JSON cannot encode non-finite floats).
type experimentEstimate struct {
	Point  float64 `json:"point"`
	CILow  float64 `json:"ci_low"`
	CIHigh float64 `json:"ci_high"`
	N      int     `json:"n"`
}

// buildExperimentRow assembles the campaign's one index row from the parsed config,
// the cost breakdown, the per-mechanism results, and the run wall-clock + completion
// time. ts is the driver-side wall clock (time.Now() at the call site; legitimate in
// a normal binary). It never reads the engine clock.
func buildExperimentRow(cfg config, bd cost.Breakdown, perTick cost.PerTick, results []eval.MechResult, modelCalls int, wall time.Duration, ts time.Time) experimentRow {
	t := bd.Total.Tokens

	ec := experimentCost{
		USD:            bd.Total.USD,
		Priced:         bd.Total.HasUSD,
		RateModel:      cfg.rateModel,
		RateCard:       cardSource(bd.Card),
		CacheHitPct:    100 * t.CacheHitFraction(),
		InputSharePct:  100 * t.InputShare(),
		OutputSharePct: 100 * t.OutputShare(),
		UnknownModels:  bd.Total.UnknownModels,
	}
	// For a LOCAL/unpriced run, carry the projected-$ under the chosen rate model so
	// the row still answers "what would this have cost on the API?".
	if cfg.rateModel != "" {
		if usd, ok := bd.ProjectUSD(cfg.rateModel); ok {
			ec.ProjectedUSD = ptr(finite(usd))
		}
	}

	mechs := make([]experimentMech, 0, len(results))
	for _, r := range results {
		mechs = append(mechs, experimentMech{
			Mechanism:          r.Mechanism.String(),
			Tier:               string(r.Tier),
			K:                  r.K,
			Items:              r.Items,
			Verdict:            string(r.Verdict),
			SigmaNoise:         finite(r.Phase0.SigmaHarnessAveraged()),
			SigmaNoiseRaw:      finite(r.Phase0.SigmaHarness),
			FeasibilityGate:    r.Feasible(),
			HarnessMinusBare:   estimate(r.HarnessMinusBare.Diff),
			GateOnMinusGateOff: estimate(r.GateOnMinusGateOff.Diff),
			GateOffSupported:   r.GateOffSupported,
			IsolationRate:      finite(r.Isolation.Rate),
		})
	}

	return experimentRow{
		SchemaVersion: experimentSchemaVersion,
		Timestamp:     ts.UTC().Format(time.RFC3339),
		RunID:         "bench-" + ts.UTC().Format("20060102T150405Z"),
		Config: experimentConfig{
			Backend:     cfg.backend,
			Model:       resolvedModel(cfg),
			LLMURL:      llmURLForRow(cfg),
			Temp:        cfg.temp,
			Concurrency: cfg.concurrency,
			Mechanisms:  mechStrings(cfg.mechanisms),
			Tiers:       tierLabel(cfg.tier),
			KTierA:      cfg.replaysA,
			KTierB:      cfg.replaysB,
			SeedBase:    cfg.seedBase,
			FixedSeed:   cfg.fixedSeed,
		},
		Tokens: experimentTokens{
			TotalIn:    t.TotalIn(),
			UncachedIn: t.UncachedIn,
			CachedIn:   t.CachedIn,
			Out:        t.Out,
			Reasoning:  t.Reasoning,
			Total:      t.Total(),
		},
		Cost: ec,
		PerTick: experimentPerTick{
			Ticks:               perTick.Ticks,
			CallsPerTickMean:    finite(perTick.MeanCalls),
			CallsPerTickMedian:  finite(perTick.MedianCalls),
			TokensPerTickMean:   finite(perTick.MeanTokens),
			TokensPerTickMedian: finite(perTick.MedianTokens),
		},
		ModelCalls: modelCalls,
		WallSec:    wall.Seconds(),
		Mechanisms: mechs,
	}
}

// experimentSchemaVersion is the experiments.jsonl row schema version (bump on a
// breaking field change so readers can branch). v2 adds the per_tick spend rollup
// (the WF-E baseline headline: calls/tokens per engine tick). v3 adds the
// contaminated flag + swap detail (the GPU/model GUARD's mid-run-swap record).
const experimentSchemaVersion = 3

// appendExperimentRow appends ONE row to the append-only experiments.jsonl. It
// opens the file O_APPEND|O_CREATE (never O_TRUNC — the ledger is history) and
// writes one compact JSON line. A write failure is returned (the caller logs it
// non-fatally — a failed index append must not lose the report the run produced).
func appendExperimentRow(row experimentRow) (string, error) {
	if dir := filepath.Dir(experimentsLedgerPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir experiments dir %q: %w", dir, err)
		}
	}
	f, err := os.OpenFile(experimentsLedgerPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open experiments ledger %q: %w", experimentsLedgerPath, err)
	}
	defer f.Close()

	line, err := json.Marshal(row)
	if err != nil {
		return "", fmt.Errorf("marshal experiment row: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return "", fmt.Errorf("append experiment row: %w", err)
	}
	return experimentsLedgerPath, nil
}

// estimate projects a types.Estimate into the row's finite-scrubbed estimate.
func estimate(e benchtypes.Estimate) experimentEstimate {
	return experimentEstimate{
		Point:  finite(e.Point),
		CILow:  finite(e.CILow),
		CIHigh: finite(e.CIHigh),
		N:      e.N,
	}
}

// cardSource returns the rate-card source label (or "" for a nil card).
func cardSource(c *cost.RateCard) string {
	if c == nil {
		return ""
	}
	return c.Source
}

// mechStrings renders a mechanism list as strings.
func mechStrings(ms []benchtypes.Mechanism) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.String()
	}
	return out
}

// llmURLForRow returns the LLM URL only for an llm-backed run (omitted for the
// offline double, where it is meaningless).
func llmURLForRow(cfg config) string {
	if cfg.backend == "llm" {
		return cfg.llmURL
	}
	return ""
}

// ptr returns a pointer to a float (for the nullable ProjectedUSD field).
func ptr(f float64) *float64 { return &f }
