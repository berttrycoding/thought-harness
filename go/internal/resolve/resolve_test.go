package resolve

import "testing"

// capabilityReg is a CAPABILITY registry test double: Create synthesises a new item (reuse-or-create).
type capabilityReg struct {
	store    map[string]string
	creates  int
	verifyOK bool
}

func (r *capabilityReg) Find(q string) (string, bool) { v, ok := r.store[q]; return v, ok }
func (r *capabilityReg) Create(q string) (string, bool) {
	r.creates++
	return "synth:" + q, true // always synthesises
}
func (r *capabilityReg) Verify(item string) (bool, string) {
	if r.verifyOK {
		return true, ""
	}
	return false, "verify rejected"
}
func (r *capabilityReg) Store(item string) { r.store["x"+item] = item }

// TestResolveReuseOrCreateRoundTrip is the P2.1 gate: the uniform spine creates on first miss and reuses
// on the next match.
func TestResolveReuseOrCreateRoundTrip(t *testing.T) {
	r := &capabilityReg{store: map[string]string{}, verifyOK: true}

	// first ask: nothing found -> create + verify + store.
	it, out, _ := Resolve[string](r, "foo")
	if out != Created || it != "synth:foo" {
		t.Fatalf("first resolve should CREATE; got %v / %q", out, it)
	}
	if r.creates != 1 {
		t.Fatalf("create should have run once; ran %d", r.creates)
	}

	// make the stored item findable, then re-resolve -> reuse (no new create).
	r.store["foo"] = "synth:foo"
	it2, out2, _ := Resolve[string](r, "foo")
	if out2 != Reused || it2 != "synth:foo" {
		t.Fatalf("second resolve should REUSE; got %v / %q", out2, it2)
	}
	if r.creates != 1 {
		t.Fatalf("reuse must not create again; creates=%d", r.creates)
	}
}

// TestResolveVerifyFailsLeavesLibraryUntouched: a variant that fails Verify is not stored.
func TestResolveVerifyFailsLeavesLibraryUntouched(t *testing.T) {
	r := &capabilityReg{store: map[string]string{}, verifyOK: false}
	_, out, reason := Resolve[string](r, "bar")
	if out != Failed || reason == "" {
		t.Fatalf("a verify failure should yield Failed with a reason; got %v / %q", out, reason)
	}
	if len(r.store) != 0 {
		t.Fatalf("a failed verify must not store anything; store=%v", r.store)
	}
}

// knowledgeReg is a KNOWLEDGE registry test double: Create RECORDS only a grounded fact and never
// fabricates one (reuse-or-record). Unfounded queries (prefix "rumor:") refuse.
type knowledgeReg struct{ store map[string]bool }

func (r *knowledgeReg) Find(q string) (string, bool) { _, ok := r.store[q]; return q, ok }
func (r *knowledgeReg) Create(q string) (string, bool) {
	if len(q) >= 6 && q[:6] == "rumor:" {
		return "", false // never-fabricate: an ungrounded claim is not recorded
	}
	return q, true
}
func (r *knowledgeReg) Verify(item string) (bool, string) { return true, "" }
func (r *knowledgeReg) Store(item string)                 { r.store[item] = true }

// TestResolveKnowledgeNeverFabricates: the SAME spine, with a knowledge registry's Create, refuses to
// fabricate an ungrounded fact (Failed) while recording a grounded one (Created).
func TestResolveKnowledgeNeverFabricates(t *testing.T) {
	r := &knowledgeReg{store: map[string]bool{}}

	if _, out, _ := Resolve[string](r, "the index halved latency"); out != Created {
		t.Fatalf("a grounded fact should be recorded (Created); got %v", out)
	}
	if _, out, _ := Resolve[string](r, "rumor: it doubles speed"); out != Failed {
		t.Fatalf("an ungrounded claim must NOT be fabricated (Failed); got %v", out)
	}
	if len(r.store) != 1 {
		t.Fatalf("only the grounded fact should be stored; store=%v", r.store)
	}
}
