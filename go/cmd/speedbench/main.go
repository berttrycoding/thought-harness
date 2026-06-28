// Command speedbench measures the RAW GENERATION SPEED of whatever model is currently loaded on an
// OpenAI-compatible endpoint (LM Studio by default) — so we can compare model variants (qwen MLX /
// GGUF / MTP, gemma, nemotron, ...) on a like-for-like prompt and pick the harness substrate + the
// dev-loop model by speed×quality.
//
// It is a SPEED probe, not a capability bench: a fixed prompt, a fixed max_tokens, N reps; per rep it
// times the call and reads usage.completion_tokens (which counts ALL generated tokens incl. a reasoning
// model's reasoning channel — the true decode work), and reports tokens/sec = completion_tokens/elapsed
// plus latency. Run it once per loaded model (one at a time — NEVER swap a model while a benchmark owns
// the GPU; see runs/gpu.lock + the model-lock protocol). Each run appends a row to --out for a
// cross-model table.
//
//	go run ./cmd/speedbench --label qwen-mtp                 # times the currently-loaded model
//	go run ./cmd/speedbench --label gemma --max-tokens 512 --reps 5
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// the fixed probe prompt — asks for a sustained, deterministic-length generation so the decode phase
// (not prefill) dominates and tokens/sec is comparable across models.
const probePrompt = "Explain, step by step and in detail, how a modern CPU cache hierarchy (L1/L2/L3) " +
	"works and why it speeds up memory access. Write several full paragraphs."

// result is one timed call's outcome — the testable unit (probe fills it, no wall-clock in the math).
type result struct {
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Finish           string `json:"finish_reason"`
	ElapsedMS        int64  `json:"elapsed_ms"`
	Err              string `json:"error,omitempty"`
}

// tokPerSec is the headline metric: generated tokens per wall-clock second. Pure (testable).
func tokPerSec(r result) float64 {
	if r.ElapsedMS <= 0 {
		return 0
	}
	return float64(r.CompletionTokens) / (float64(r.ElapsedMS) / 1000.0)
}

// parseUsage extracts the token counts + finish reason from a /chat/completions response body. Split
// out from the HTTP call so the parsing is unit-testable against a fixture.
func parseUsage(body []byte) (promptT, completionT, totalT int, finish string, err error) {
	var resp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if e := json.Unmarshal(body, &resp); e != nil {
		return 0, 0, 0, "", e
	}
	if resp.Error != nil {
		return 0, 0, 0, "", fmt.Errorf("%s", resp.Error.Message)
	}
	if len(resp.Choices) > 0 {
		finish = resp.Choices[0].FinishReason
	}
	return resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, finish, nil
}

// probe runs ONE timed completion against the endpoint and returns the measured result.
func probe(client *http.Client, baseURL, apiKey, model string, maxTokens int) result {
	reqBody, _ := json.Marshal(map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": probePrompt}},
		"max_tokens":  maxTokens,
		"temperature": 0.7,
		"stream":      false,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return result{Model: model, Err: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{Model: model, ElapsedMS: time.Since(start).Milliseconds(), Err: err.Error()}
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	elapsed := time.Since(start).Milliseconds()
	p, c, t, finish, perr := parseUsage(buf.Bytes())
	r := result{Model: model, PromptTokens: p, CompletionTokens: c, TotalTokens: t, Finish: finish, ElapsedMS: elapsed}
	if perr != nil {
		r.Err = perr.Error()
	}
	return r
}

// autodetectModel reads /v1/models and returns the first id (the loaded model in LM Studio).
func autodetectModel(client *http.Client, baseURL, apiKey string) (string, error) {
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&parsed); e != nil {
		return "", e
	}
	if len(parsed.Data) == 0 {
		return "", fmt.Errorf("no model loaded at %s/models", baseURL)
	}
	return parsed.Data[0].ID, nil
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func main() {
	url := flag.String("url", envOr("THOUGHT_LLM_BASE_URL", "http://localhost:1234/v1"), "OpenAI-compatible base URL")
	model := flag.String("model", "", "model id (empty = autodetect the loaded model)")
	apiKey := flag.String("api-key", envOr("THOUGHT_LLM_API_KEY", "lm-studio"), "API key")
	label := flag.String("label", "", "label for this model variant in the report (e.g. qwen-mtp); defaults to the model id")
	reps := flag.Int("reps", 5, "number of timed calls")
	maxTokens := flag.Int("max-tokens", 256, "max_tokens per call (the generation length to time)")
	warmup := flag.Bool("warmup", true, "do one untimed warmup call first (load/JIT the model)")
	out := flag.String("out", "runs/speedbench.jsonl", "append the summary row here for a cross-model table")
	flag.Parse()

	client := &http.Client{Timeout: 600 * time.Second}

	m := *model
	if m == "" {
		det, err := autodetectModel(client, *url, *apiKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "speedbench: cannot autodetect model: %v\n(is a model loaded? is %s reachable?)\n", err, *url)
			os.Exit(1)
		}
		m = det
	}
	lbl := *label
	if lbl == "" {
		lbl = m
	}

	fmt.Printf("speedbench: model=%q label=%q url=%s reps=%d max_tokens=%d\n", m, lbl, *url, *reps, *maxTokens)
	if *warmup {
		fmt.Print("warmup... ")
		w := probe(client, *url, *apiKey, m, *maxTokens)
		if w.Err != "" {
			fmt.Fprintf(os.Stderr, "\nspeedbench: warmup failed: %s\n", w.Err)
			os.Exit(1)
		}
		fmt.Printf("ok (%d tok in %dms)\n", w.CompletionTokens, w.ElapsedMS)
	}

	var tps []float64
	var lat []float64
	var lastP, lastC int
	for i := 0; i < *reps; i++ {
		r := probe(client, *url, *apiKey, m, *maxTokens)
		if r.Err != "" {
			fmt.Fprintf(os.Stderr, "speedbench: rep %d error: %s\n", i+1, r.Err)
			continue
		}
		t := tokPerSec(r)
		tps = append(tps, t)
		lat = append(lat, float64(r.ElapsedMS))
		lastP, lastC = r.PromptTokens, r.CompletionTokens
		fmt.Printf("  rep %d: %6.1f tok/s  (%d completion tok, %dms, finish=%s)\n", i+1, t, r.CompletionTokens, r.ElapsedMS, r.Finish)
	}
	if len(tps) == 0 {
		fmt.Fprintln(os.Stderr, "speedbench: all reps failed")
		os.Exit(1)
	}

	medTPS := median(tps)
	medLat := median(lat)
	fmt.Printf("\n== %s ==\n  median: %.1f tok/s   |   median latency: %.0f ms   |   prompt≈%d completion≈%d tok   (n=%d)\n",
		lbl, medTPS, medLat, lastP, lastC, len(tps))

	// append a machine-readable summary row for a cross-model table.
	row := map[string]any{
		"label": lbl, "model": m, "url": *url, "reps": len(tps), "max_tokens": *maxTokens,
		"median_tok_per_s": round1(medTPS), "median_latency_ms": round1(medLat),
		"prompt_tokens": lastP, "completion_tokens": lastC,
	}
	if err := appendJSONL(*out, row); err != nil {
		fmt.Fprintf(os.Stderr, "speedbench: could not append to %s: %v\n", *out, err)
	} else {
		fmt.Printf("  -> appended to %s\n", *out)
	}
}

func appendJSONL(path string, row map[string]any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(row)
	_, err = f.Write(append(b, '\n'))
	return err
}

func round1(x float64) float64 { return float64(int64(x*10+0.5)) / 10 }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
