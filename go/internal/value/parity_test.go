package value

import (
	"math"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/control"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/graph"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// captureBus subscribes a slice collector to a fresh Bus and returns both. Mirrors the Python
// reference harness (a Bus + a subscriber that records each Event).
func captureBus() (*events.Bus, *[]events.Event) {
	bus := events.NewDefault()
	var got []events.Event
	bus.Subscribe(func(e events.Event) { got = append(got, e) })
	return bus, &got
}

// TestValueUpdateParity pins value.update against the Python reference (PORT-PLAN Tier-2 gate:
// "value.update emits identical values/reward"). The golden values + the single value.update
// event were captured by RUNNING thought_harness/value.py with the identical graph fixture:
//
//	goal "verify the patch runs cleanly"
//	b0 (active): [GENERATED conf 0.8 "the patch should run cleanly now",
//	              OBSERVATION conf 0.9 ok=True "ran the tests: 5 passed"]
//	b1:          [GENERATED conf 0.5 "maybe a different patch",
//	              OBSERVATION conf 0.6 ok=False "ran the tests: 2 failed"]
//
// Python output: values {b0: 0.6505555555555556, b1: 0.1413888888888889}, reward(active)=1.0,
// event summary "V(active b0)=0.65; frontier_best=0.14", signals
// {recent_conf:0.312, goal_sim:0.039, grounded_reality:0.3}, reason "reality confirmed this line".
func TestValueUpdateParity(t *testing.T) {
	bus, got := captureBus()

	g := graph.New("verify the patch runs cleanly")
	// b0 (the root/active branch): a confident generated thought + a confirmed observation.
	g.Append(&types.Thought{ID: -1, Text: "the patch should run cleanly now",
		Source: types.GENERATED, Confidence: 0.8}, 1)
	g.Append(&types.Thought{ID: -1, Text: "ran the tests: 5 passed",
		Source: types.OBSERVATION, Confidence: 0.9,
		RawReturn: types.Observation{Ok: true, Text: "5 passed"}}, 2)
	// b1: a refuted line (failed observation).
	parent := 0
	reason := "alt"
	b1 := g.NewBranch(&parent, &reason)
	prev := g.ActiveBranch
	g.ActiveBranch = b1
	g.Append(&types.Thought{ID: -1, Text: "maybe a different patch",
		Source: types.GENERATED, Confidence: 0.5}, 3)
	g.Append(&types.Thought{ID: -1, Text: "ran the tests: 2 failed",
		Source: types.OBSERVATION, Confidence: 0.6,
		RawReturn: types.Observation{Ok: false, Text: "2 failed"}}, 4)
	g.ActiveBranch = prev

	vs := New(bus.Emit)
	values := vs.Update(g)

	// values map parity (UNROUNDED math, compared to Python's float64 to ~1e-12).
	wantValues := map[int]float64{0: 0.6505555555555556, 1: 0.1413888888888889}
	if len(values) != len(wantValues) {
		t.Fatalf("values len=%d want %d (%v)", len(values), len(wantValues), values)
	}
	for bid, want := range wantValues {
		if math.Abs(values[bid]-want) > 1e-12 {
			t.Errorf("values[b%d]=%.16f want %.16f", bid, values[bid], want)
		}
	}

	// reward parity (active context grounded reward = +1.0 for the confirmed observation).
	if r := vs.Reward(g); r != 1.0 {
		t.Errorf("reward=%v want 1.0", r)
	}

	// exactly one value.update event, byte-identical to the Python wire.
	if len(*got) != 1 {
		t.Fatalf("emitted %d events, want 1: %v", len(*got), *got)
	}
	ev := (*got)[0]
	if ev.Kind != events.Value || ev.Layer != "value" {
		t.Errorf("kind/layer = %q/%q want value.update/value", ev.Kind, ev.Layer)
	}
	if ev.Summary != "V(active b0)=0.65; frontier_best=0.14" {
		t.Errorf("summary=%q", ev.Summary)
	}
	wantData := events.D{
		"active": 0,
		"values": map[string]any{"b0": 0.651, "b1": 0.141},
		"reward": 1.0,
		"signals": map[string]any{
			"recent_conf": 0.312, "goal_sim": 0.039, "grounded_reality": 0.3,
		},
		"reason":    "reality confirmed this line",
		"appraiser": control.Appraiser,
	}
	assertDataEqual(t, "value.update", ev.Data, wantData)
}

// TestValueRewardOnlyGrounded confirms reward counts ONLY grounded OBSERVATIONs in the active
// context (the structural property; Python ValueSignal.reward).
func TestValueRewardOnlyGrounded(t *testing.T) {
	g := graph.New("g")
	// a confirmed + a refuted observation in the active branch -> 1.0 + (-0.5) = 0.5.
	g.Append(&types.Thought{ID: -1, Text: "ok", Source: types.OBSERVATION,
		RawReturn: types.Observation{Ok: true}}, 0)
	g.Append(&types.Thought{ID: -1, Text: "no", Source: types.OBSERVATION,
		RawReturn: types.Observation{Ok: false}}, 0)
	// a non-observation contributes nothing.
	g.Append(&types.Thought{ID: -1, Text: "thinking", Source: types.GENERATED, Confidence: 0.9}, 0)
	vs := New(nil)
	if r := vs.Reward(g); math.Abs(r-0.5) > 1e-12 {
		t.Fatalf("reward=%v want 0.5", r)
	}
}

// TestAppraiseEmptyBranch pins the empty-branch appraisal (Python returns Appraisal with a fresh
// empty signals map, value 0.0, reason "empty branch"). The non-nil empty map is load-bearing.
func TestAppraiseEmptyBranch(t *testing.T) {
	g := graph.New("g")
	// fork an empty branch (only METACOG-free, no real thoughts)
	parent := 0
	reason := "empty"
	b := g.NewBranch(&parent, &reason)
	vs := New(nil)
	ap := vs.AppraiseBranch(g, b)
	if ap.Value != 0.0 || ap.Reason != "empty branch" {
		t.Fatalf("empty appraisal = %+v", ap)
	}
	if ap.Signals == nil {
		t.Fatalf("signals must be a non-nil empty map (marshals as {} not null)")
	}
	if len(ap.Signals) != 0 {
		t.Fatalf("signals should be empty: %v", ap.Signals)
	}
}

// assertDataEqual compares an emitted event's Data map against the expected golden, normalising
// numeric types (Go ints vs Python ints, float64s) so the wire-shape comparison matches Python.
func assertDataEqual(t *testing.T, label string, got, want events.D) {
	t.Helper()
	if !dataEqual(got, want) {
		t.Errorf("%s data mismatch:\n got  = %#v\n want = %#v", label, got, want)
	}
}

// dataEqual deep-compares two data maps with numeric coercion (int<->float64 by value) so a
// golden literal written with int keys matches a Go emit that produced float64 (and vice versa),
// matching the Python JSON wire where 0 and 0.0 are distinct only by display.
func dataEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !valEqual(av, bv) {
			return false
		}
	}
	return true
}

func valEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		return ok && dataEqual(av, bv)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		if an, aok := asFloat(a); aok {
			if bn, bok := asFloat(b); bok {
				return an == bn
			}
			return false
		}
		return a == b
	}
}

// asFloat coerces an int/int64/float64 to float64 for cross-type numeric comparison.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
