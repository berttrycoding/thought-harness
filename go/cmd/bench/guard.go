package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// GPU / model LOCK + GUARD.
//
// A mid-run model swap in LM Studio (load a different model while a campaign is
// in flight) silently turns a working mechanism into a false NO-SIGNAL: every
// cell after the swap is scored against the WRONG model, contaminating the whole
// contrast. It has happened twice (qwen, then nemotron). This makes it
// IMPOSSIBLE to miss:
//
//  1. RESOLVE the expected model id ONCE at start (explicit --llm-model, or the
//     first id from GET /v1/models for "auto"). Refuse to start with no model
//     loaded.
//  2. LOCK FILE runs/gpu.lock — advisory cross-process notice that this run owns
//     the GPU, removed on clean exit. A live foreign lock WARNS but proceeds.
//  3. GUARD — re-GET /v1/models before the run and every guardEveryCells cells;
//     on a mismatch, print a big stderr banner, mark the report + the
//     experiments row CONTAMINATED, flush what completed, and exit NON-ZERO.
//
// Only --backend llm is guarded — the offline test double never touches the
// network and is exempt (it has no loaded-model concept). --no-guard disables
// the periodic check (offline/test convenience).
// ---------------------------------------------------------------------------

// gpuLockPath is the advisory lock file written for the duration of an llm run.
const gpuLockPath = "runs/gpu.lock"

// guardEveryCells is the GUARD cadence: re-check the loaded model every this-many
// COLLECTED cells (cheap GET /v1/models). 8 keeps the check roughly per-mechanism
// at the pilot N while staying negligible against the per-cell model latency.
const guardEveryCells = 8

// guardHTTPTimeout bounds one /v1/models probe so a hung server cannot stall the
// guard (the probe is tiny; a slow/absent answer is treated as "cannot verify",
// which is non-fatal — only a CONFIRMED different id aborts).
const guardHTTPTimeout = 5 * time.Second

// gpuLock is the runs/gpu.lock payload: which model this run pinned, the driver
// pid + start time (so a foreign lock's liveness is checkable), the mechanisms in
// the campaign, and the run id (cross-references the experiments.jsonl row).
type gpuLock struct {
	Model      string   `json:"model"`
	PID        int      `json:"pid"`
	Started    string   `json:"started"` // RFC3339, driver-side time.Now()
	Mechanisms []string `json:"mechanisms"`
	RunID      string   `json:"run_id"`
}

// setupGuard wires the whole LOCK + GUARD for a campaign. For the test double it
// is a no-op (the offline path has no loaded model): it returns a disabled guard +
// a no-op release. For --backend llm it RESOLVES the expected model id (refusing to
// start with no model loaded), writes runs/gpu.lock, and builds the live guard. The
// resolved id is stored back on cfg (so the report/experiments model id is the
// concrete pinned id even for "auto"). --no-guard still resolves + locks but builds
// a DISABLED guard (no periodic check) for offline/test convenience.
//
// The returned release closure removes the lock; the caller must defer it. On any
// setup error the lock is NOT written and release is a no-op.
func setupGuard(cfg *config, runID string) (guard *modelGuard, release func(), err error) {
	noop := func() {}
	if cfg.backend != "llm" {
		// Test double: no network, no loaded model — guard is inert.
		return newModelGuard("", "", false), noop, nil
	}

	expected, err := resolveExpectedModel(*cfg)
	if err != nil {
		return nil, noop, err
	}
	cfg.expectedModel = expected
	progressf("bench: model guard — pinned expected model %q at %s (every %d cells)\n",
		expected, strings.TrimRight(cfg.llmURL, "/")+"/models", guardEveryCells)

	release, err = writeGPULock(expected, runID, mechStrings(cfg.mechanisms))
	if err != nil {
		// A lock-write failure must not silently disable the guard; surface it.
		return nil, noop, err
	}

	enabled := !cfg.noGuard
	if cfg.noGuard {
		progressf("bench: WARN --no-guard set — mid-run model-swap GUARD is DISABLED (lock still written)\n")
	}
	return newModelGuard(expected, cfg.llmURL, enabled), release, nil
}

// resolveExpectedModel pins the model id the whole campaign MUST run against.
// An explicit --llm-model (anything but "auto") is taken verbatim. "auto" probes
// GET /v1/models once and pins the FIRST loaded id — the same id the backend's
// own autodetect would pick, so the guard compares like-for-like. It REFUSES to
// start when no model is loaded (an empty server is the silent-NO-SIGNAL trap the
// guard exists to prevent).
func resolveExpectedModel(cfg config) (string, error) {
	explicit := strings.TrimSpace(cfg.llmModel)
	if explicit != "" && !strings.EqualFold(explicit, "auto") {
		return explicit, nil
	}
	ids, err := listLoadedModels(cfg.llmURL)
	if err != nil {
		return "", fmt.Errorf("resolve expected model: GET %s/models failed: %w "+
			"(is the LLM server up at --llm-url?)", strings.TrimRight(cfg.llmURL, "/"), err)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("resolve expected model: no model is loaded at %s — "+
			"load one in LM Studio before benching (refusing to start so a later swap "+
			"cannot silently contaminate the run)", strings.TrimRight(cfg.llmURL, "/"))
	}
	return ids[0], nil
}

// listLoadedModels GETs <baseURL>/models and returns the loaded model ids, in the
// server's order (ids[0] is the autodetect pick). It is the single cheap probe the
// resolver AND the guard share. A transport/decode failure is returned as an error
// (the resolver treats it as fatal at start; the guard treats it as "cannot verify"
// and does NOT abort on it).
func listLoadedModels(baseURL string) ([]string, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	ctx, cancel := context.WithTimeout(context.Background(), guardHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// LM Studio ignores auth; a real OpenAI-compatible gateway needs a bearer. Use
	// the same key the backend would (env, default lm-studio).
	key := os.Getenv("THOUGHT_LLM_API_KEY")
	if key == "" {
		key = "lm-studio"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// writeGPULock writes runs/gpu.lock for this run and returns a removal closure
// (defer it for clean-exit cleanup). If a lock already exists for a LIVE pid it
// WARNS loudly (another bench run is using the GPU) but PROCEEDS — the lock is
// advisory, not a mutex. A stale lock (dead pid / unreadable) is silently
// overwritten.
func writeGPULock(expectedModel, runID string, mechs []string) (remove func(), err error) {
	if dir := filepath.Dir(gpuLockPath); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return func() {}, fmt.Errorf("mkdir lock dir %q: %w", dir, mkErr)
		}
	}
	if existing, ok := readGPULock(); ok && existing.PID != os.Getpid() && pidAlive(existing.PID) {
		progressf("bench: WARN another bench run holds %s (model=%s pid=%d started=%s) — "+
			"the GPU may be SHARED; proceeding (advisory lock)\n",
			gpuLockPath, existing.Model, existing.PID, existing.Started)
	}
	lock := gpuLock{
		Model:      expectedModel,
		PID:        os.Getpid(),
		Started:    time.Now().Format(time.RFC3339),
		Mechanisms: mechs,
		RunID:      runID,
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return func() {}, fmt.Errorf("marshal gpu lock: %w", err)
	}
	if err := os.WriteFile(gpuLockPath, append(data, '\n'), 0o644); err != nil {
		return func() {}, fmt.Errorf("write %s: %w", gpuLockPath, err)
	}
	// Remove ONLY if the on-disk lock is still ours (another run may have taken it
	// over) — never delete a foreign run's lock.
	return func() {
		if cur, ok := readGPULock(); ok && cur.PID != os.Getpid() {
			return
		}
		_ = os.Remove(gpuLockPath)
	}, nil
}

// readGPULock reads + parses runs/gpu.lock; ok=false when absent or unparseable.
func readGPULock() (gpuLock, bool) {
	data, err := os.ReadFile(gpuLockPath)
	if err != nil {
		return gpuLock{}, false
	}
	var lock gpuLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return gpuLock{}, false
	}
	return lock, true
}

// pidAlive reports whether pid names a live process (a foreign lock's owner). On
// unix, signal-0 to the pid distinguishes alive (nil / EPERM) from dead (ESRCH).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 performs error checking without delivering a signal: nil ⇒ alive,
	// EPERM ⇒ alive-but-not-ours, ESRCH ⇒ dead.
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "operation not permitted")
}

// modelGuard verifies the loaded model still matches the pinned expected id, and
// records the swap detail on a mismatch so the report + the experiments row can be
// flagged CONTAMINATED. It is concurrency-safe: the pool calls Check under its
// collect lock, but the atomic + mutex make it safe regardless.
type modelGuard struct {
	expected   string
	baseURL    string
	enabled    bool // false for --no-guard / the test double
	everyCells int

	aborted atomic.Bool
	mu      sync.Mutex
	detail  swapDetail
}

// swapDetail captures a confirmed mid-run swap for the report + the experiments
// row (so the contamination is auditable, not just a console banner).
type swapDetail struct {
	Expected  string `json:"expected"`
	Got       string `json:"got"`
	CellIndex int    `json:"cell_index"` // cells completed when the swap was caught
	CellTotal int    `json:"cell_total"`
	At        string `json:"at"` // RFC3339 detection time
}

// newModelGuard builds the guard. enabled is false for the test double / --no-guard.
func newModelGuard(expected, baseURL string, enabled bool) *modelGuard {
	return &modelGuard{expected: expected, baseURL: baseURL, enabled: enabled, everyCells: guardEveryCells}
}

// Aborted reports whether a confirmed swap has been seen (read by the pool feeder
// to stop dispatching new cells, and by execute() to choose the exit path).
func (g *modelGuard) Aborted() bool { return g != nil && g.aborted.Load() }

// shouldCheck reports whether the guard should re-verify at this collected-cell
// count (every everyCells cells). False for a nil/disabled guard or once aborted.
func (g *modelGuard) shouldCheck(collected int) bool {
	if g == nil || !g.enabled || g.aborted.Load() || g.everyCells <= 0 {
		return false
	}
	return collected%g.everyCells == 0
}

// Detail returns the captured swap detail (valid only when Aborted()).
func (g *modelGuard) Detail() swapDetail {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.detail
}

// Verify does ONE loaded-model check at cell cellsDone of cellTotal. A CONFIRMED
// different id sets aborted + records the swap + prints the banner, and returns
// false (contaminated). A probe error is "cannot verify" — it logs a warning and
// returns true (never abort on a transient GET failure; only a confirmed swap
// aborts). A nil / disabled / already-aborted guard returns true and does no I/O
// (the feeder reads Aborted() to decide; Verify just performs the probe).
func (g *modelGuard) Verify(cellsDone, cellTotal int) (ok bool) {
	if g == nil || !g.enabled || g.aborted.Load() {
		return true
	}
	ids, err := listLoadedModels(g.baseURL)
	if err != nil {
		progressf("bench: WARN model guard could not verify loaded model at cell %d/%d "+
			"(%v) — continuing (only a CONFIRMED swap aborts)\n", cellsDone, cellTotal, err)
		return true
	}
	if len(ids) == 0 {
		progressf("bench: WARN model guard saw NO model loaded at cell %d/%d — "+
			"continuing (the server may be mid-reload; only a CONFIRMED different id aborts)\n",
			cellsDone, cellTotal)
		return true
	}
	// Is the EXPECTED (pinned) model still loaded? With MULTIPLE models loaded
	// (e.g. CC-3 tiering: a thinking model + a control-tier model), LM Studio routes
	// each request by its requested id — so the contamination risk is the EXPECTED
	// model being UNLOADED, not another model also being present. Check MEMBERSHIP of
	// the expected id in the loaded set, not ids[0] (which may be a co-loaded model).
	for _, id := range ids {
		if id == g.expected {
			return true
		}
	}
	got := ids[0] // expected is gone; report what IS loaded for the banner.
	// CONFIRMED SWAP (the expected model is no longer among the loaded set). Record once, print the banner, signal abort.
	if g.aborted.CompareAndSwap(false, true) {
		g.mu.Lock()
		g.detail = swapDetail{
			Expected:  g.expected,
			Got:       got,
			CellIndex: cellsDone,
			CellTotal: cellTotal,
			At:        time.Now().Format(time.RFC3339),
		}
		g.mu.Unlock()
		printSwapBanner(g.expected, got, cellsDone, cellTotal)
	}
	return false
}

// printSwapBanner writes the big unmistakable stderr banner. It is boxed and
// shouty by design — a mid-run swap silently destroyed two prior campaigns, so it
// must be impossible to scroll past.
func printSwapBanner(expected, got string, cellsDone, cellTotal int) {
	bar := strings.Repeat("=", 78)
	msg := []string{
		bar,
		"=== MODEL SWAPPED MID-RUN — BENCHMARK CONTAMINATED, ABORTING ===",
		bar,
		fmt.Sprintf("  expected model : %s", expected),
		fmt.Sprintf("  loaded  model : %s", got),
		fmt.Sprintf("  caught at cell : %d / %d", cellsDone, cellTotal),
		"",
		"  Every cell run AFTER the swap was scored against the WRONG model.",
		"  The report + the experiments.jsonl row are flagged CONTAMINATED=true.",
		"  Re-load the EXPECTED model and re-run. Exiting NON-ZERO.",
		bar,
	}
	fmt.Fprintln(os.Stderr, "\n"+strings.Join(msg, "\n"))
}

// contaminationBanner is the boxed CONTAMINATED notice prepended to the on-disk
// report text (so the report file itself cannot be read without seeing it). It
// mirrors the stderr banner.
func contaminationBanner(d swapDetail) string {
	bar := strings.Repeat("=", 78)
	return strings.Join([]string{
		bar,
		"!!! CONTAMINATED — MODEL SWAPPED MID-RUN — RESULTS BELOW ARE INVALID !!!",
		bar,
		fmt.Sprintf("expected model : %s", d.Expected),
		fmt.Sprintf("loaded  model : %s", d.Got),
		fmt.Sprintf("caught at cell : %d / %d   (%s)", d.CellIndex, d.CellTotal, d.At),
		"Every cell after the swap was scored against the WRONG model. Re-run.",
		bar,
		"", "",
	}, "\n")
}
