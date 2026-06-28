package tiera

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

// LoadItems reads a JSONL file of Tier-A items (one types.TierAItem per line)
// into memory. Blank lines (and pure-whitespace lines) are skipped so a
// hand-edited bank with trailing newlines loads cleanly; a malformed line fails
// loud with its 1-based line number so a bad bank is debuggable (spec §5.2).
func LoadItems(path string) ([]benchtypes.TierAItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("tiera: open item bank %q: %w", path, err)
	}
	defer f.Close()
	return LoadItemsReader(f)
}

// LoadItemsReader is the io.Reader form of LoadItems (so a caller can load from
// an embedded fixture, a pipe, or a test string without a temp file). Each
// non-blank line must decode to one types.TierAItem.
func LoadItemsReader(r io.Reader) ([]benchtypes.TierAItem, error) {
	var items []benchtypes.TierAItem
	sc := bufio.NewScanner(r)
	// Tier-A items embed an artifact Materialization ([]byte) that can be large;
	// raise the per-line cap well above bufio's 64 KiB default so a fixture file
	// or run-record doesn't trip the scanner.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var item benchtypes.TierAItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, fmt.Errorf("tiera: malformed item on line %d: %w", line, err)
		}
		items = append(items, item)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("tiera: read item bank: %w", err)
	}
	return items, nil
}

// Sandbox is a materialized per-item workspace: the root dir the artifact lives
// under (the value handed to runner.Runner.Workspace so the Action layer's
// sandboxed tools read REAL bytes) and the in-sandbox absolute path of the
// materialized artifact file (empty for a non-file or zero-valued artifact).
type Sandbox struct {
	// Root is the per-item temp directory (the sandbox workspace root).
	Root string
	// ArtifactPath is the absolute path of the materialized artifact file inside
	// Root, or "" when the item has no file artifact.
	ArtifactPath string
}

// Materialize writes item.Artifact into a fresh per-item temp sandbox so the
// grounding tools have something real to read (spec §5.2). It returns the
// Sandbox + a cleanup func the caller MUST defer; cleanup removes the whole
// temp tree. A zero-valued Artifact (a model-only probe, e.g. the
// continuous-autonomy frozen-snapshot forced-choice) yields an empty sandbox
// with no artifact file — never an error.
//
// The artifact's in-sandbox Path is treated as relative to the sandbox root
// (an absolute Path is re-rooted under the sandbox so an item can never escape
// its own workspace — a leading "/" or ".." is stripped to a sandbox-relative
// path). The materialized bytes are item.Artifact.Materialization (the artifact's
// ground-truth source); when Path is set but Materialization is empty, an empty
// file is still created so the fixed path the prompt refers to exists.
func Materialize(item benchtypes.TierAItem) (Sandbox, func(), error) {
	root, err := os.MkdirTemp("", "tiera-"+sanitizeID(item.ID)+"-")
	if err != nil {
		return Sandbox{}, func() {}, fmt.Errorf("tiera: create sandbox for %q: %w", item.ID, err)
	}
	cleanup := func() { _ = os.RemoveAll(root) }

	art := item.Artifact
	// A zero-valued artifact (no Kind, no Path, no Spec, no bytes) is a model-only
	// probe — the empty sandbox is correct, no file to write.
	if art.Path == "" {
		return Sandbox{Root: root}, cleanup, nil
	}

	rel := sandboxRel(art.Path)
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		cleanup()
		return Sandbox{}, func() {}, fmt.Errorf("tiera: mkdir for artifact %q (%s): %w", item.ID, art.Path, err)
	}
	if err := os.WriteFile(abs, art.Materialization, 0o644); err != nil {
		cleanup()
		return Sandbox{}, func() {}, fmt.Errorf("tiera: write artifact %q (%s): %w", item.ID, art.Path, err)
	}
	// MULTI-FILE: materialize any ADDITIONAL files (sandbox path -> contents) so a multi-step item can
	// read an index/manifest and then the file it points to. Paths are sandbox-relative (a "/"-escape is
	// clamped by sandboxRel). Deterministic content; the map order does not matter (each is a distinct path).
	for p, content := range art.Files {
		fabs := filepath.Join(root, sandboxRel(p))
		if err := os.MkdirAll(filepath.Dir(fabs), 0o755); err != nil {
			cleanup()
			return Sandbox{}, func() {}, fmt.Errorf("tiera: mkdir for extra file %q (%s): %w", item.ID, p, err)
		}
		if err := os.WriteFile(fabs, []byte(content), 0o644); err != nil {
			cleanup()
			return Sandbox{}, func() {}, fmt.Errorf("tiera: write extra file %q (%s): %w", item.ID, p, err)
		}
	}
	return Sandbox{Root: root, ArtifactPath: abs}, cleanup, nil
}

// sandboxRel turns an artifact's declared Path into a safe sandbox-relative
// path: a leading separator is dropped and any ".." segments are removed so a
// crafted bank can never write outside its own per-item sandbox.
func sandboxRel(p string) string {
	clean := filepath.Clean("/" + filepath.ToSlash(p)) // anchor at root, collapse ".."
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || clean == "." {
		clean = "artifact"
	}
	return filepath.FromSlash(clean)
}

// sanitizeID makes an item ID safe to embed in a temp-dir name (only the temp
// suffix; os.MkdirTemp adds the random tail). Non-alphanumerics become '-'.
func sanitizeID(id string) string {
	if id == "" {
		return "item"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
