package action

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/web"
)

// TestFetchURLToolDropsIntoTheToolSystem is the PROOF that fetch_url is a SMALL change in the
// subconscious's tool system (the sibling of web_search): it implements the Tool contract, registers with
// one call, and dispatches a real URL through the injected web.PageFetcher seam — using the SAME machinery
// (registry + executor + gate-router + sub-agent toolScope) that already drives web_search/read_file.
func TestFetchURLToolDropsIntoTheToolSystem(t *testing.T) {
	tool := NewFetchURL(web.NewFakePager()) // deterministic page-fetch seam (the offline double)

	// (1) It satisfies the Tool contract + declares the right taxonomy (a network READ — identical to
	//     web_search, so it routes + scopes the same way).
	var _ Tool = tool
	if tool.Name() != "fetch_url" {
		t.Fatalf("name = %q", tool.Name())
	}
	if c := tool.Category(); c.Op != OpInspect || c.Reach != ReachExternal {
		t.Fatalf("category = %s, want inspect/external (same as web_search)", c)
	}

	// (2) It registers with ONE call and is retrievable by name (the registry path).
	r := NewToolRegistry([]Tool{tool})
	got, ok := r.Get("fetch_url")
	if !ok {
		t.Fatal("fetch_url not in the registry after Register")
	}

	// (3) A dispatch flows url -> page-fetch seam -> result content (the FakePager's fixed page text).
	res := got.Execute(map[string]any{"url": "https://example.org/transcontinental-railroad"})
	if res.IsError || res.Content == "" {
		t.Fatalf("expected page text from the seam, got IsError=%v content=%q", res.IsError, res.Content)
	}
	if res.Content != web.NewFakePager().R.Text {
		t.Fatalf("fetch_url content = %q, want the FakePager's fixed page text", res.Content)
	}
}

// TestFetchURLBestEffortHonesty pins the best-effort contract: page-blind / empty / non-http(s) / a failed
// read each return IsError with a clear code — never a crash, never a fabricated page voiced as a result.
func TestFetchURLBestEffortHonesty(t *testing.T) {
	tool := NewFetchURL(web.NewFakePager())

	// page-blind seam (nil) -> blind read.
	if blind := (&FetchURL{}).Execute(map[string]any{"url": "https://x.com"}); !blind.IsError || blind.ErrorCode != "web_blind_or_empty_url" {
		t.Errorf("nil page-fetch seam must be a blind read; got %+v", blind)
	}
	// empty / missing url -> blind read.
	if empty := tool.Execute(map[string]any{}); !empty.IsError || empty.ErrorCode != "web_blind_or_empty_url" {
		t.Errorf("empty url must be a blind read; got %+v", empty)
	}
	// non-http(s) url -> bad-url (rejected up front, never steered at a file:// / data: target).
	for _, bad := range []string{"file:///etc/passwd", "data:text/html,<h1>x</h1>", "ftp://x/y", "example.com/page"} {
		if r := tool.Execute(map[string]any{"url": bad}); !r.IsError || r.ErrorCode != "fetch_url_bad_url" {
			t.Errorf("non-http url %q must be rejected as fetch_url_bad_url; got %+v", bad, r)
		}
	}
	// a failed read (seam returns OK=false) -> web_read_failed, never partial content.
	failing := NewFetchURL(&web.FakePager{R: web.Result{OK: false}})
	if r := failing.Execute(map[string]any{"url": "https://x.com/page"}); !r.IsError || r.ErrorCode != "web_read_failed" || r.Content != "" {
		t.Errorf("a failed page read must be web_read_failed with no content; got %+v", r)
	}
}
