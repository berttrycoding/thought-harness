package retrieval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// OpenAICompatEmbedder calls an OpenAI-compatible /v1/embeddings endpoint (LM Studio by default).
// STDLIB-ONLY (net/http). It is the real semantic signal; wrap it in a CachingEmbedder so each text is
// embedded once (the spec's "embed each stored item once, cache the vector" — determinism + cost).
type OpenAICompatEmbedder struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewOpenAICompatEmbedder builds an embedder. Empty baseURL/model fall back to env then defaults
// (THOUGHT_LLM_BASE_URL / THOUGHT_EMBED_MODEL, then localhost:1234 + nomic-embed-text).
func NewOpenAICompatEmbedder(baseURL, model, apiKey string) *OpenAICompatEmbedder {
	if baseURL == "" {
		baseURL = envOr("THOUGHT_LLM_BASE_URL", "http://localhost:1234/v1")
	}
	if model == "" {
		model = envOr("THOUGHT_EMBED_MODEL", "text-embedding-nomic-embed-text-v1.5")
	}
	if apiKey == "" {
		apiKey = envOr("THOUGHT_LLM_API_KEY", "lm-studio")
	}
	return &OpenAICompatEmbedder{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed returns the dense vector for text, or an error (unreachable endpoint / no model loaded / bad
// response). Callers fall back to lexical on error — a missing embedder is never fatal.
func (e *OpenAICompatEmbedder) Embed(text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]any{"input": text, "model": e.model})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings endpoint returned %s", resp.Status)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embeddings: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings: empty vector (no model loaded?)")
	}
	return parsed.Data[0].Embedding, nil
}

// CachingEmbedder memoizes vectors by text so each item is embedded exactly once — the determinism +
// cost discipline the spec requires (cached vectors -> reproducible benchmark). Concurrency-safe.
type CachingEmbedder struct {
	inner Embedder
	mu    sync.Mutex
	cache map[string][]float32
}

// NewCachingEmbedder wraps inner with a per-text memo.
func NewCachingEmbedder(inner Embedder) *CachingEmbedder {
	return &CachingEmbedder{inner: inner, cache: map[string][]float32{}}
}

func (c *CachingEmbedder) Embed(text string) ([]float32, error) {
	c.mu.Lock()
	if v, ok := c.cache[text]; ok {
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()
	v, err := c.inner.Embed(text)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[text] = v
	c.mu.Unlock()
	return v, nil
}

// ReachableEmbedder returns a caching real embedder if one answers a probe embed, else nil (so callers
// — and the offline test suite — degrade to lexical-only without failing).
func ReachableEmbedder() Embedder {
	emb, _ := ProbeEmbedder()
	return emb
}

// SidecarProbe is the OBSERVABLE outcome of probing the embeddings sidecar (A-RAG2): the engine surfaces
// it on a retrieval.semantic announce event so a silent lexical fallback is never mistaken for a lit
// dense channel. OK is true iff the sidecar answered a probe embed; Dims is the probe vector width;
// BaseURL + Model name the endpoint that answered (or would have); Reason carries the failure on !OK.
type SidecarProbe struct {
	OK      bool
	Dims    int
	BaseURL string
	Model   string
	Reason  string
}

// ProbeEmbedder probes the OpenAI-compatible /v1/embeddings sidecar and returns a caching real embedder
// (or nil) PLUS the observable SidecarProbe outcome. It is the intentional, reportable form of the
// incidental ReachableEmbedder probe: callers that want to ANNOUNCE whether the dense channel lit up use
// this; the embedder is nil on a failed probe so callers degrade to lexical-only without failing. Used
// by the engine ONLY behind the subconscious.semantic_recall knob (default OFF ⇒ the legacy silent
// ReachableEmbedder path runs, byte-identical).
func ProbeEmbedder() (Embedder, SidecarProbe) {
	e := NewOpenAICompatEmbedder("", "", "")
	p := SidecarProbe{BaseURL: e.baseURL, Model: e.model}
	v, err := e.Embed("probe")
	if err != nil {
		p.Reason = err.Error()
		return nil, p
	}
	p.OK, p.Dims = true, len(v)
	return NewCachingEmbedder(e), p
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
