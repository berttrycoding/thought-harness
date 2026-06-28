package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseUsage(t *testing.T) {
	body := []byte(`{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":256,"total_tokens":298}}`)
	p, c, tot, finish, err := parseUsage(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != 42 || c != 256 || tot != 298 || finish != "stop" {
		t.Fatalf("got p=%d c=%d tot=%d finish=%q", p, c, tot, finish)
	}
}

func TestParseUsageError(t *testing.T) {
	if _, _, _, _, err := parseUsage([]byte(`{"error":{"message":"no model loaded"}}`)); err == nil {
		t.Fatal("expected an error from an error-payload response")
	}
}

func TestTokPerSec(t *testing.T) {
	// 256 tokens in 2000ms = 128 tok/s.
	if got := tokPerSec(result{CompletionTokens: 256, ElapsedMS: 2000}); got != 128 {
		t.Fatalf("tok/s = %v, want 128", got)
	}
	// guard: zero/negative elapsed → 0, never a divide-by-zero.
	if got := tokPerSec(result{CompletionTokens: 100, ElapsedMS: 0}); got != 0 {
		t.Fatalf("tok/s with 0 elapsed = %v, want 0", got)
	}
}

func TestMedian(t *testing.T) {
	if m := median([]float64{3, 1, 2}); m != 2 {
		t.Fatalf("median(3,1,2) = %v, want 2", m)
	}
	if m := median([]float64{4, 1, 3, 2}); m != 2.5 {
		t.Fatalf("median(4,1,3,2) = %v, want 2.5", m)
	}
	if m := median(nil); m != 0 {
		t.Fatalf("median(nil) = %v, want 0", m)
	}
}

// TestProbeAgainstFakeServer drives the full probe path against a fake completions endpoint that delays
// ~120ms and returns a known usage — asserting the token counts parse and the measured elapsed yields a
// finite, positive tok/s. No model / GPU needed.
func TestProbeAgainstFakeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		time.Sleep(120 * time.Millisecond) // simulate generation time
		io.WriteString(w, `{"choices":[{"finish_reason":"length"}],"usage":{"prompt_tokens":30,"completion_tokens":200,"total_tokens":230}}`)
	}))
	defer srv.Close()

	r := probe(srv.Client(), srv.URL+"/v1", "", "fake-model", 200)
	if r.Err != "" {
		t.Fatalf("probe error: %s", r.Err)
	}
	if r.CompletionTokens != 200 || r.PromptTokens != 30 || r.Finish != "length" {
		t.Fatalf("parsed wrong: %+v", r)
	}
	if r.ElapsedMS < 100 {
		t.Fatalf("elapsed %dms too low (server delayed 120ms)", r.ElapsedMS)
	}
	if tps := tokPerSec(r); tps <= 0 || tps > 100000 {
		t.Fatalf("tok/s = %v, want a finite positive rate", tps)
	}
}

func TestAutodetectModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":[{"id":"qwen/qwen3.6-35b-a3b"},{"id":"other"}]}`)
	}))
	defer srv.Close()
	m, err := autodetectModel(srv.Client(), srv.URL+"/v1", "")
	if err != nil || m != "qwen/qwen3.6-35b-a3b" {
		t.Fatalf("autodetect = %q err=%v, want the first model id", m, err)
	}
}
