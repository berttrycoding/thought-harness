package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// modelsServer is a fake OpenAI-compatible /v1/models endpoint whose reported
// loaded model id can be swapped at runtime (the exact mid-run swap the guard
// exists to catch). The returned base URL has no trailing slash, like --llm-url.
func modelsServer(t *testing.T, current *atomic.Value) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		id, _ := current.Load().(string)
		var data []map[string]string
		if id != "" {
			data = []map[string]string{{"id": id}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv.URL // e.g. http://127.0.0.1:PORT
}

func TestResolveExpectedModel_Explicit(t *testing.T) {
	// An explicit --llm-model is taken verbatim, no probe.
	got, err := resolveExpectedModel(config{llmModel: "my/model", llmURL: "http://127.0.0.1:0"})
	if err != nil {
		t.Fatalf("explicit model: unexpected error: %v", err)
	}
	if got != "my/model" {
		t.Fatalf("explicit model: got %q want my/model", got)
	}
}

func TestResolveExpectedModel_Auto(t *testing.T) {
	var cur atomic.Value
	cur.Store("loaded/model-A")
	url := modelsServer(t, &cur)

	got, err := resolveExpectedModel(config{llmModel: "auto", llmURL: url})
	if err != nil {
		t.Fatalf("auto: unexpected error: %v", err)
	}
	if got != "loaded/model-A" {
		t.Fatalf("auto: got %q want loaded/model-A", got)
	}
}

func TestResolveExpectedModel_RefusesEmpty(t *testing.T) {
	var cur atomic.Value
	cur.Store("") // no model loaded
	url := modelsServer(t, &cur)

	_, err := resolveExpectedModel(config{llmModel: "auto", llmURL: url})
	if err == nil {
		t.Fatal("auto with no model loaded: expected a refuse-to-start error, got nil")
	}
}

func TestModelGuard_NoSwap(t *testing.T) {
	var cur atomic.Value
	cur.Store("model-A")
	url := modelsServer(t, &cur)

	g := newModelGuard("model-A", url, true)
	if ok := g.Verify(0, 10); !ok {
		t.Fatal("no-swap: Verify returned not-ok")
	}
	if g.Aborted() {
		t.Fatal("no-swap: guard aborted with no swap")
	}
}

func TestModelGuard_DetectsSwap(t *testing.T) {
	var cur atomic.Value
	cur.Store("model-A")
	url := modelsServer(t, &cur)

	g := newModelGuard("model-A", url, true)
	// First check: still model-A.
	if ok := g.Verify(8, 24); !ok {
		t.Fatal("pre-swap check returned not-ok")
	}
	// SWAP the loaded model mid-run.
	cur.Store("model-B")
	if ok := g.Verify(16, 24); ok {
		t.Fatal("post-swap check returned ok — the swap was NOT caught")
	}
	if !g.Aborted() {
		t.Fatal("post-swap: guard did not set aborted")
	}
	d := g.Detail()
	if d.Expected != "model-A" || d.Got != "model-B" {
		t.Fatalf("swap detail wrong: expected=%q got=%q", d.Expected, d.Got)
	}
	if d.CellIndex != 16 || d.CellTotal != 24 {
		t.Fatalf("swap cell detail wrong: %d/%d (want 16/24)", d.CellIndex, d.CellTotal)
	}
}

func TestModelGuard_DisabledIsInert(t *testing.T) {
	// A disabled guard (--no-guard / test double) must never abort even on a swap.
	var cur atomic.Value
	cur.Store("model-A")
	url := modelsServer(t, &cur)

	g := newModelGuard("model-A", url, false)
	cur.Store("model-B") // would be a swap if enabled
	if ok := g.Verify(8, 24); !ok {
		t.Fatal("disabled guard returned not-ok")
	}
	if g.Aborted() {
		t.Fatal("disabled guard aborted")
	}
	if g.shouldCheck(8) {
		t.Fatal("disabled guard shouldCheck returned true")
	}
}

func TestModelGuard_ProbeErrorDoesNotAbort(t *testing.T) {
	// A transient GET failure is "cannot verify", NOT a swap — it must not abort
	// (only a CONFIRMED different id aborts).
	g := newModelGuard("model-A", "http://127.0.0.1:1/v1", true) // unreachable
	if ok := g.Verify(8, 24); !ok {
		t.Fatal("probe error: Verify aborted (should be cannot-verify, non-fatal)")
	}
	if g.Aborted() {
		t.Fatal("probe error: guard aborted on a transient GET failure")
	}
}

func TestGPULock_WriteReadRemove(t *testing.T) {
	// Run in a temp CWD so the fixed runs/gpu.lock path is sandboxed.
	dir := t.TempDir()
	chdir(t, dir)

	remove, err := writeGPULock("model-A", "bench-test", []string{"grounding", "safety"})
	if err != nil {
		t.Fatalf("writeGPULock: %v", err)
	}
	lock, ok := readGPULock()
	if !ok {
		t.Fatal("lock not readable after write")
	}
	if lock.Model != "model-A" || lock.RunID != "bench-test" {
		t.Fatalf("lock fields wrong: %+v", lock)
	}
	if lock.PID != os.Getpid() {
		t.Fatalf("lock pid %d != our pid %d", lock.PID, os.Getpid())
	}
	if lock.Started == "" {
		t.Fatal("lock started is empty")
	}
	if len(lock.Mechanisms) != 2 {
		t.Fatalf("lock mechanisms wrong: %v", lock.Mechanisms)
	}
	// Clean exit removes the lock.
	remove()
	if _, err := os.Stat(gpuLockPath); !os.IsNotExist(err) {
		t.Fatalf("lock not removed after release (stat err=%v)", err)
	}
}

func TestShouldCheckCadence(t *testing.T) {
	g := newModelGuard("m", "http://x", true)
	for _, tc := range []struct {
		n    int
		want bool
	}{{1, false}, {7, false}, {8, true}, {9, false}, {16, true}, {24, true}} {
		if got := g.shouldCheck(tc.n); got != tc.want {
			t.Errorf("shouldCheck(%d)=%v want %v", tc.n, got, tc.want)
		}
	}
}

// chdir changes the working directory for the test and restores it on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	// Ensure a runs/ dir exists at the new CWD for the lock writer (it mkdirs, but be explicit).
	_ = os.MkdirAll(filepath.Join(dir, "runs"), 0o755)
}

// ensure the banner functions don't panic on a zero detail (defensive).
func TestBannersDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("banner panicked: %v", r)
		}
	}()
	printSwapBanner("a", "b", 1, 2)
	_ = contaminationBanner(swapDetail{Expected: "a", Got: "b", CellIndex: 1, CellTotal: 2, At: "now"})
	_ = fmt.Sprint(gpuLock{})
}
