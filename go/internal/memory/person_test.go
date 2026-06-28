package memory

import (
	"bytes"
	"testing"
)

// TestPersonAdaptationLearnsConsistentOverride is the P7.3 gate: a consistently-overridden default is
// learned and changes behaviour, and survives into the next session (persistence) — while a one-off or
// inconsistent override does NOT become a preference.
func TestPersonAdaptationLearnsConsistentOverride(t *testing.T) {
	r := NewPersonRegistry(3)

	// a single override is noise — not yet learned.
	if r.ObserveOverride("verbosity", "terse") {
		t.Fatal("one override must not be learned")
	}
	if _, ok := r.Preference("verbosity"); ok {
		t.Fatal("no preference should be learned after a single override")
	}

	// a second consistent override — still accumulating.
	r.ObserveOverride("verbosity", "terse")
	if _, ok := r.Preference("verbosity"); ok {
		t.Fatal("not learned before the threshold")
	}

	// the third crosses the threshold — now learned.
	if !r.ObserveOverride("verbosity", "terse") {
		t.Fatal("the third consistent override should LEARN the preference")
	}
	pref, ok := r.Preference("verbosity")
	if !ok || pref.Value != "terse" {
		t.Fatalf("the learned preference should be verbosity=terse; got %+v", pref)
	}
	if len(r.Applied()) != 1 {
		t.Fatalf("one preference should now change behaviour; got %v", r.Applied())
	}

	// next session: the preference survives a restart (so it actually changes next-session behaviour).
	var buf bytes.Buffer
	if err := r.Save(&buf); err != nil {
		t.Fatalf("Save: %v", err)
	}
	next := NewPersonRegistry(3)
	if _, err := next.Load(&buf); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p, ok := next.Preference("verbosity"); !ok || p.Value != "terse" {
		t.Fatalf("the learned preference must survive into the next session; got %+v ok=%v", p, ok)
	}
}

// TestPersonAdaptationInconsistentOverrideResets: flip-flopping overrides do NOT crystallise into a
// preference — only a STABLE pattern is learned.
func TestPersonAdaptationInconsistentOverrideResets(t *testing.T) {
	r := NewPersonRegistry(3)
	r.ObserveOverride("tone", "formal")
	r.ObserveOverride("tone", "casual") // flipped -> evidence resets to this new value
	r.ObserveOverride("tone", "formal") // flipped back -> resets again
	if _, ok := r.Preference("tone"); ok {
		t.Fatal("an inconsistent (flip-flopping) override must not be learned as a stable preference")
	}
}
