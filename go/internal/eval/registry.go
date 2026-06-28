package eval

import "github.com/berttrycoding/thought-harness/internal/resolve"

// compile-time proof the stick registry plugs into the uniform resolve spine.
var _ resolve.Registry[MeasuringStick] = (*StickRegistry)(nil)

// StickRegistry is the registry of MeasuringSticks (§3.18: eval is itself a
// registry-able, mintable object type). It satisfies resolve.Registry[T] so the
// uniform Resolve spine (SEARCH -> reuse-or-create -> VERIFY -> STORE) drives stick
// minting exactly like every other capability registry.
//
// It is a CAPABILITY-family registry (resolve.go): Create synthesises a stick
// when none is found, via an injected synthesiser (so the package stays
// engine-free + deterministic — the caller supplies how a stick is born from a
// query). With no synthesiser, Create refuses (reuse-only), which is the safe
// default for a seeded-only stick set.
type StickRegistry struct {
	byName map[string]MeasuringStick
	// Synth builds a candidate stick for a query when none is found. nil = the
	// registry is reuse-only (Create refuses).
	Synth func(query string) (MeasuringStick, bool)
}

// NewStickRegistry returns an empty stick registry (reuse-only until Synth is set
// or sticks are seeded via Seed).
func NewStickRegistry() *StickRegistry {
	return &StickRegistry{byName: map[string]MeasuringStick{}}
}

// Seed loads seeded (non-minted) sticks into the registry.
func (r *StickRegistry) Seed(sticks ...MeasuringStick) {
	for _, s := range sticks {
		s.Minted = false
		r.byName[s.Name] = s
	}
}

// Get returns a stick by name.
func (r *StickRegistry) Get(name string) (MeasuringStick, bool) {
	s, ok := r.byName[name]
	return s, ok
}

// Len is the number of sticks in the registry.
func (r *StickRegistry) Len() int { return len(r.byName) }

// Find satisfies resolve.Registry: look the stick up by name (the query is the
// stick name).
func (r *StickRegistry) Find(query string) (MeasuringStick, bool) {
	s, ok := r.byName[query]
	return s, ok
}

// Create satisfies resolve.Registry: synthesise a candidate stick (marked
// Minted) via the injected Synth, or refuse when there is no synthesiser.
func (r *StickRegistry) Create(query string) (MeasuringStick, bool) {
	if r.Synth == nil {
		return MeasuringStick{}, false
	}
	s, ok := r.Synth(query)
	if !ok {
		return MeasuringStick{}, false
	}
	s.Minted = true
	if s.Name == "" {
		s.Name = query
	}
	return s, true
}

// Verify satisfies resolve.Registry: a stick must have a name and a usable check
// before it can be trusted in the registry (the two-layer discipline applied to
// the eval object). An out-of-range threshold is rejected.
func (r *StickRegistry) Verify(s MeasuringStick) (bool, string) {
	if s.Name == "" {
		return false, "stick has no name"
	}
	if s.Check == nil {
		return false, "stick has no check"
	}
	if s.Threshold < 0 || s.Threshold > 1 {
		return false, "stick threshold out of [0,1]"
	}
	return true, ""
}

// Store satisfies resolve.Registry: persist the verified stick (keyed by name).
func (r *StickRegistry) Store(s MeasuringStick) { r.byName[s.Name] = s }

// MintStick runs the uniform resolve spine to reuse-or-mint a stick for a query.
// It is sugar over resolve.Resolve so a caller does not import resolve directly.
func (r *StickRegistry) MintStick(query string) (MeasuringStick, resolve.Outcome, string) {
	return resolve.Resolve[MeasuringStick](r, query)
}
