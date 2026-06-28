package subconscious

import (
	"sort"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cpyrand"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// TestDispatchFiresRealPrimitiveSet is the Tier-6 dispatch gate after the representation-space rebuild
// (M2): dispatch fires the RIGHT real-primitive set at a fixed theta. The old Python-parity fixture
// (py_dispatch.json) pinned the now-DELETED fake roster (arithmetic/memory/simulation/safety/refactor);
// that model no longer exists, so this gate is native Go and asserts the real primitives instead —
//
//	compute   : always live, deterministic exact math (lights on a binary expression in context)
//	recall    : real-store retrieval (lights only when a recall request + the store has a grounded hit)
//	skeptic   : model-driven stance role (lights only when a model backend is wired, on a review shape)
//	advocate  : model-driven stance role (the opposite stance, so the Gate still sees fork-on-conflict)
//	read/search/run : tool-backed senses+hands (light only with a bound executor — dark offline here)
//
// The fired-set is deterministic (keyword scan + the never-fabricate store gate), so the comparison is
// exact. No canned simulation/safety/refactor string can fire — that is exactly what M2 removed.
func TestDispatchFiresRealPrimitiveSet(t *testing.T) {
	// A model port so the skeptic/advocate roles are live; a recaller with one grounded fact so recall
	// can hit. No executor ⇒ read/search/run stay dark (the honest offline posture — no fake "it runs").
	caller := &fakeCaller{out: "a reasoned stance", ok: true}
	recaller := &fakeRecaller{facts: map[string]string{}}
	recaller.facts["recall"] = "the build passed last time we tried this"

	cases := []struct {
		name  string
		texts []string
		theta float64
		want  []string
	}{
		{"compute", []string{"What is 6 times 7?"}, 0.3, []string{"compute"}},
		{"review_forks", []string{"Is it safe to refactor this auth module?"}, 0.3,
			[]string{"advocate", "skeptic"}}, // the two stances fork the conflict
		{"recall_hit", []string{"recall what we know about the build"}, 0.3, []string{"recall"}},
		{"mixed", []string{"is it safe to refactor; also what is 2 + 2?"}, 0.3,
			[]string{"advocate", "compute", "skeptic"}},
		{"hi_theta", []string{"Is it safe to refactor this auth module?"}, 0.9, nil},
		{"lo_theta_neutral", []string{"just some neutral filler text here"}, 0.05, nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			eng := NewSubconsciousEngine(
				DefaultPrimitiveSubAgents(recaller, nil /*no executor*/, caller, noopEmit, nil, nil, false),
				cpyrand.New(1234), noopEmit, nil, nil,
			)
			fired, _ := eng.Dispatch(dispatchCtx(tc.texts), tc.theta, nil)
			got := firedDomains(fired)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !equalStrings(got, want) {
				t.Errorf("dispatch(%q, theta=%.2f) fired %v, want %v", tc.name, tc.theta, got, want)
			}
		})
	}
}

// TestNoCannedPrimitiveSubAgentFires is the anti-filler guard (M2 §2.6): the deleted fake specialists
// (simulation/safety/refactor) and the toy MemoryKB are gone, so no canned domain ever fires.
func TestNoCannedPrimitiveSubAgentFires(t *testing.T) {
	// Even with a model + recaller wired, the OLD fake domains must never appear in a fired set.
	caller := &fakeCaller{out: "reason", ok: true}
	recaller := &fakeRecaller{facts: map[string]string{"recall": "grounded"}}
	eng := NewSubconsciousEngine(
		DefaultPrimitiveSubAgents(recaller, nil, caller, noopEmit, nil, nil, false), cpyrand.New(1234), noopEmit, nil, nil,
	)
	texts := []string{"refactor and simulate whether this is safe at runtime"}
	fired, _ := eng.Dispatch(dispatchCtx(texts), 0.3, nil)
	for _, c := range fired {
		if c.Domain == nil {
			continue
		}
		switch *c.Domain {
		case "simulation", "safety", "refactor", "arithmetic", "memory":
			t.Errorf("a deleted fake/old domain fired: %q (text=%q)", *c.Domain, c.Text)
		}
	}
}

// --- fakes ------------------------------------------------------------------------------------

// fakeCaller is a backends.SpecialistCaller test double: returns a fixed reasoned stance.
type fakeCaller struct {
	out string
	ok  bool
}

func (f *fakeCaller) Specialist(domain, description string, ctx []types.Thought) (string, bool) {
	return f.out, f.ok
}

// fakeRecaller is a subconscious.MemoryRecaller test double over a tiny fact map; RecallFact hits when
// any map key is a substring of the lower-cased query (the never-fabricate-on-miss contract).
type fakeRecaller struct{ facts map[string]string }

func (f *fakeRecaller) RecallFact(query string) (string, bool) {
	for k, v := range f.facts {
		if k != "" && containsLower(query, k) {
			return v, true
		}
	}
	return "", false
}

func containsLower(s, sub string) bool {
	// the query is already lower-cased by ctxTextDefault; a plain substring test suffices.
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- helpers (kept from the prior fixture-driven gate) ------------------------------------------

// noopEmit is the events.Emit no-op (the gate compares the fired SET, not the event stream).
func noopEmit(kind, summary string, data events.D) events.Event { return events.Event{} }

// dispatchCtx builds a GENERATED-source context from text lines (Thought(id=i+1, text=t, GENERATED)).
func dispatchCtx(texts []string) []types.Thought {
	out := make([]types.Thought, len(texts))
	for i, txt := range texts {
		out[i] = types.Thought{ID: i + 1, Text: txt, Source: types.GENERATED}
	}
	return out
}

// firedDomains pulls the sorted domain set out of the fired candidates.
func firedDomains(fired []*types.Candidate) []string {
	out := make([]string, 0, len(fired))
	for _, c := range fired {
		if c.Domain != nil {
			out = append(out, *c.Domain)
		}
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
