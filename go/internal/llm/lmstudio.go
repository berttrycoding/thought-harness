// LM Studio auto-loading. The local product path talks to LM Studio's OpenAI-compatible server,
// but that server reports "up" even when NO model is loaded — so a request 400s and every role
// falls back to the heuristic templates (the "heuristic setting doesn't work" symptom). This file
// closes that gap: when the local server is up but nothing is served, drive the `lms` CLI to LOAD a
// model from a prioritized probe list, polling /v1/models until it is actually serving — so the
// local path "just works" with no manual `lms load`.
//
// Policy knobs (env): THOUGHT_LLM_AUTOLOAD=0 disables auto-load (you manage LM Studio yourself);
// THOUGHT_LLM_TTL sets the idle-unload seconds for an auto-loaded model (default 3600 — the harness
// gives the RAM back when idle); LMS_BIN overrides the `lms` CLI path.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// autoLoadLog is where the loader reports progress — model loads can take 30s+, so silence reads as
// a hang. Defaults to stderr (visible at CLI startup and before the TUI's alt-screen takes over).
// The TUI mutes rebuild-time loads via SetAutoLoadLog so they never write over the rendered grid.
var autoLoadLog = func(s string) { fmt.Fprintln(os.Stderr, "thought: "+s) }

// SetAutoLoadLog overrides the auto-load progress sink. nil mutes it.
func SetAutoLoadLog(fn func(string)) {
	if fn == nil {
		autoLoadLog = func(string) {}
		return
	}
	autoLoadLog = fn
}

// autoLoadEnabled reports whether automatic model loading is on (default yes; any of
// 0/false/no/off in THOUGHT_LLM_AUTOLOAD disables it).
func autoLoadEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("THOUGHT_LLM_AUTOLOAD"))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// autoLoadTTL is the idle-unload window (seconds) for an auto-loaded model. Override THOUGHT_LLM_TTL.
func autoLoadTTL() int { return envInt("THOUGHT_LLM_TTL", 3600) }

// isLocalURL reports whether a base URL points at a loopback host (the inverse of frontierConfig's
// "remote" test). Only local endpoints get auto-load — a remote frontier we cannot `lms load`.
func isLocalURL(u string) bool {
	return strings.Contains(u, "localhost") ||
		strings.Contains(u, "127.0.0.1") || strings.Contains(u, "0.0.0.0")
}

// ---------------------------------------------------------------------------
// the `lms` CLI
// ---------------------------------------------------------------------------

// lmsBin locates the LM Studio CLI: $LMS_BIN, then ~/.lmstudio/bin/lms, then `lms` on PATH. "" = none.
func lmsBin() string {
	if b := strings.TrimSpace(os.Getenv("LMS_BIN")); b != "" {
		return b
	}
	if home, err := os.UserHomeDir(); err == nil {
		cand := filepath.Join(home, ".lmstudio", "bin", "lms")
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	if p, err := exec.LookPath("lms"); err == nil {
		return p
	}
	return ""
}

// lmsModel is one entry of `lms ls --json`.
type lmsModel struct {
	Type      string `json:"type"` // "llm" | "embedding"
	ModelKey  string `json:"modelKey"`
	SizeBytes int64  `json:"sizeBytes"`
	Params    string `json:"paramsString"`
	Arch      string `json:"architecture"`
}

// lmsDownloadedLLMs runs `lms ls --json` and returns the downloaded LLMs (drops embeddings).
func lmsDownloadedLLMs(bin string, timeout time.Duration) ([]lmsModel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "ls", "--json").Output()
	if err != nil {
		return nil, err
	}
	var all []lmsModel
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, err
	}
	llms := make([]lmsModel, 0, len(all))
	for _, m := range all {
		if m.Type == "" || m.Type == "llm" { // tolerate a missing type; only drop known embeddings
			llms = append(llms, m)
		}
	}
	return llms, nil
}

// ModelInfo describes a downloaded local LLM for the TUI model picker: the lms model key (also what
// `lms load` + the /v1 request use), its size + params + arch, and whether it is currently SERVED.
type ModelInfo struct {
	Key    string
	Params string
	Arch   string
	SizeGB float64
	Loaded bool
}

// ListLocalLLMs returns the downloaded local LLMs (embeddings dropped), each flagged with whether it is
// currently served at /v1/models — the model menu the TUI offers for switching. Errors when the `lms`
// CLI is unavailable (LM Studio not installed / not on PATH). Sorted largest-first then by key.
func ListLocalLLMs() ([]ModelInfo, error) {
	bin := lmsBin()
	if bin == "" {
		return nil, errString("the `lms` CLI was not found (looked at $LMS_BIN, ~/.lmstudio/bin/lms, PATH) — " +
			"install LM Studio's CLI or load a model in the app")
	}
	dl, err := lmsDownloadedLLMs(bin, 10*time.Second)
	if err != nil {
		return nil, errString("could not list LM Studio models (`lms ls --json`): " + firstLine(err.Error()))
	}
	loaded, _ := getLoadedModels(defaultBaseURL, defaultKey, 3*time.Second) // best-effort: server may be down
	loadedSet := make(map[string]bool, len(loaded))
	for _, id := range loaded {
		loadedSet[id] = true
	}
	out := make([]ModelInfo, 0, len(dl))
	for _, m := range dl {
		// belt-and-suspenders: drop an embedding model whose `lms ls` type was mis-reported as llm/"" —
		// it can't serve as a chat substrate, so it has no place in the switch menu.
		if strings.Contains(strings.ToLower(m.ModelKey), "embedding") || strings.Contains(strings.ToLower(m.Arch), "bert") {
			continue
		}
		out = append(out, ModelInfo{
			Key: m.ModelKey, Params: m.Params, Arch: m.Arch,
			SizeGB: float64(m.SizeBytes) / 1e9, Loaded: loadedSet[m.ModelKey],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SizeGB != out[j].SizeGB {
			return out[i].SizeGB > out[j].SizeGB
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// LoadedModel is one currently-resident model from `lms ps` — its identifier (what `lms unload` takes),
// size, and whether it is an embedding model (which the harness keeps loaded for retrieval, never swaps).
type LoadedModel struct {
	ID          string
	Key         string
	SizeGB      float64
	IsEmbedding bool
}

// LoadedModels lists the models currently resident in LM Studio (`lms ps --json`) — the live memory
// picture the switch-plan screen shows. Empty (nil error) when nothing is loaded.
func LoadedModels() ([]LoadedModel, error) {
	bin := lmsBin()
	if bin == "" {
		return nil, errString("the `lms` CLI was not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "ps", "--json").Output()
	if err != nil {
		return nil, errString("could not list loaded models (`lms ps --json`): " + firstLine(err.Error()))
	}
	var raw []struct {
		Type       string `json:"type"`
		ModelKey   string `json:"modelKey"`
		Identifier string `json:"identifier"`
		SizeBytes  int64  `json:"sizeBytes"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	models := make([]LoadedModel, 0, len(raw))
	for _, m := range raw {
		id := m.Identifier
		if id == "" {
			id = m.ModelKey
		}
		models = append(models, LoadedModel{
			ID: id, Key: m.ModelKey, SizeGB: float64(m.SizeBytes) / 1e9,
			IsEmbedding: m.Type == "embedding",
		})
	}
	return models, nil
}

// EstimateModel asks LM Studio whether a model can be loaded under the current resource guardrails
// (`lms load <key> --estimate-only`) — returns the estimated total memory (GB) and a fit verdict, so the
// switch plan can warn before a load that would fail. Best-effort: an unparseable estimate is fits=true.
func EstimateModel(key string) (gb float64, fits bool, note string) {
	bin := lmsBin()
	if bin == "" {
		return 0, true, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, "load", key, "--estimate-only").CombinedOutput()
	text := string(out)
	fits = true
	for _, ln := range strings.Split(text, "\n") {
		l := strings.TrimSpace(ln)
		if strings.HasPrefix(l, "Estimated Total Memory:") {
			fields := strings.Fields(l)
			for i, f := range fields {
				if strings.HasPrefix(f, "Gi") || f == "GiB" || f == "GB" {
					if i > 0 {
						if v, err := strconv.ParseFloat(fields[i-1], 64); err == nil {
							gb = v
						}
					}
				}
			}
		}
		if strings.Contains(strings.ToLower(l), "may not be loaded") ||
			strings.Contains(strings.ToLower(l), "will not") ||
			strings.Contains(strings.ToLower(l), "insufficient") {
			fits, note = false, l
		}
	}
	return gb, fits, note
}

// UnloadModel unloads one resident model by its identifier (`lms unload <id>`). The harness uses it to
// SWAP — free the previous substrate when switching — never the embedding model.
func UnloadModel(id string) error {
	bin := lmsBin()
	if bin == "" {
		return errString("the `lms` CLI was not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "unload", id)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := firstLine(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return errString("lms unload " + id + ": " + msg)
	}
	return nil
}

// LoadLocalModel force-loads a downloaded local model (idempotent if already resident) and waits until
// it is served, returning the served /v1 id. Used when the user SWITCHES models in the TUI — unlike the
// auto-resolve path, this forces the chosen model even when another is already loaded. Blocks (a large
// model can take minutes); the caller runs it off the UI loop.
func LoadLocalModel(key string) (string, error) {
	bin := lmsBin()
	if bin == "" {
		return "", errString("the `lms` CLI was not found — cannot load a model")
	}
	if err := lmsLoad(bin, key, autoLoadTTL(), 5*time.Minute); err != nil {
		return "", err
	}
	if id := waitServed(defaultBaseURL, defaultKey, key, 20*time.Second); id != "" {
		return id, nil
	}
	return key, nil // loaded but not yet visible in /v1; the rebuild health-check confirms it
}

// lmsLoad runs `lms load <key> -y --ttl <ttl>` — synchronous; blocks until the model is resident or
// the command fails. The spinner on stdout is discarded; stderr is captured for the error message.
func lmsLoad(bin, key string, ttl int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"load", key, "-y"}
	if ttl > 0 {
		args = append(args, "--ttl", strconv.Itoa(ttl))
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := firstLine(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return errString("lms load " + key + ": " + msg)
	}
	return nil
}

// ---------------------------------------------------------------------------
// the OpenAI-compatible /models probe (standalone — runs before a backend is built)
// ---------------------------------------------------------------------------

// getLoadedModels GETs <baseURL>/models and returns the served model ids. An error means the server
// itself is unreachable; an empty slice with nil error means "up, but nothing loaded".
func getLoadedModels(baseURL, apiKey string, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// ---------------------------------------------------------------------------
// the orchestrator
// ---------------------------------------------------------------------------

// EnsureLocalModel makes the local OpenAI-compatible server (LM Studio) actually SERVE a model and
// returns the served model id. Policy:
//
//  1. Server unreachable                           -> error (caller maps to BackendUnavailable).
//  2. A model already loaded                        -> use it (prefer `preferred` if that is the one
//     loaded), never disturbing the user's manual choice.
//  3. Nothing loaded + auto-load on + `lms` present -> load the first candidate from the probe list
//     that loads & serves, polling /v1/models.
//  4. Nothing loaded + cannot auto-load             -> error with a precise, actionable message.
//
// The probe list is: `preferred`, then the package default model, then every other downloaded LLM
// smallest-first (a smaller fallback is likelier to fit) — de-duped, restricted to downloaded models.
func EnsureLocalModel(baseURL, apiKey, preferred string, timeout time.Duration) (string, error) {
	if apiKey == "" {
		apiKey = defaultKey
	}
	loaded, err := getLoadedModels(baseURL, apiKey, min(timeout, 5*time.Second))
	if err != nil {
		return "", errString("local server not reachable at " + baseURL + ": " + firstLine(err.Error()))
	}
	if len(loaded) > 0 {
		if preferred != "" && preferred != "auto" && containsStr(loaded, preferred) {
			return preferred, nil
		}
		return loaded[0], nil
	}

	// Up, but nothing loaded — auto-load if we can.
	if !autoLoadEnabled() {
		return "", errString("LM Studio is up at " + baseURL + " but no model is loaded " +
			"(auto-load off via THOUGHT_LLM_AUTOLOAD); run `lms load <model>` or load one in the app")
	}
	bin := lmsBin()
	if bin == "" {
		return "", errString("LM Studio is up at " + baseURL + " but no model is loaded, and the `lms` " +
			"CLI was not found (looked at $LMS_BIN, ~/.lmstudio/bin/lms, PATH) — load a model in LM Studio")
	}
	downloaded, err := lmsDownloadedLLMs(bin, min(timeout, 10*time.Second))
	if err != nil {
		return "", errString("could not list LM Studio models (`lms ls --json`): " + firstLine(err.Error()))
	}
	candidates := probeList(preferred, downloaded)
	if len(candidates) == 0 {
		return "", errString("LM Studio has no LLM downloaded to auto-load — get one, e.g. " +
			"`lms get qwen/qwen3.5-0.8b`")
	}

	var lastErr string
	for _, key := range candidates {
		autoLoadLog("LM Studio: loading " + key + " (" + humanSize(sizeOf(key, downloaded)) + ")…")
		if err := lmsLoad(bin, key, autoLoadTTL(), min(timeout, 5*time.Minute)); err != nil {
			lastErr = firstLine(err.Error())
			autoLoadLog("  " + key + " did not load: " + lastErr)
			continue
		}
		if id := waitServed(baseURL, apiKey, key, 15*time.Second); id != "" {
			autoLoadLog("LM Studio: " + id + " ready")
			return id, nil
		}
		lastErr = key + " loaded but never appeared in /v1/models"
		autoLoadLog("  " + lastErr)
	}
	hint := ""
	if lastErr != "" {
		hint = " (last: " + lastErr + ")"
	}
	return "", errString("could not auto-load any local model" + hint)
}

// probeList builds the prioritized load order: preferred, the package default, then the rest
// smallest-first — de-duped and restricted to actually-downloaded models.
func probeList(preferred string, downloaded []lmsModel) []string {
	have := make(map[string]bool, len(downloaded))
	for _, m := range downloaded {
		have[m.ModelKey] = true
	}
	bySize := make([]lmsModel, len(downloaded))
	copy(bySize, downloaded)
	sort.SliceStable(bySize, func(i, j int) bool { return bySize[i].SizeBytes < bySize[j].SizeBytes })

	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if k == "" || k == "auto" || seen[k] || !have[k] {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	add(preferred)
	add(defaultModel)
	for _, m := range bySize {
		add(m.ModelKey)
	}
	return out
}

// waitServed polls /v1/models until `key` (or, failing that, any model) is served, up to `within`.
// Returns the served id (preferring an exact match) or "" on timeout. `lms load` is synchronous, so
// this is a defensive confirmation rather than the primary wait.
func waitServed(baseURL, apiKey, key string, within time.Duration) string {
	deadline := time.Now().Add(within)
	for {
		if ids, err := getLoadedModels(baseURL, apiKey, 4*time.Second); err == nil {
			if containsStr(ids, key) {
				return key
			}
			if len(ids) > 0 {
				return ids[0]
			}
		}
		if !time.Now().Before(deadline) {
			return ""
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// sizeOf returns the download size of `key` among `ms` (0 if unknown).
func sizeOf(key string, ms []lmsModel) int64 {
	for _, m := range ms {
		if m.ModelKey == key {
			return m.SizeBytes
		}
	}
	return 0
}

// humanSize renders a byte count as a compact "1.0 GB" (binary units).
func humanSize(b int64) string {
	const unit = 1024.0
	if b < int64(unit) {
		return strconv.FormatInt(b, 10) + " B"
	}
	f := float64(b)
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	i := -1
	for f >= unit && i < len(units)-1 {
		f /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// firstLine returns the first non-empty trimmed line of s (load errors are often multi-line).
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}
