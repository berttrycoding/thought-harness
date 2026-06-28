// person.go is the user/person-adaptation store (P7.3): "how to interact with this individual". It is
// the third declarative registry (after episodic + semantic), on the same memory pattern. It learns
// from OVERRIDE PATTERNS — when the user consistently overrides a default the same way, that becomes a
// learned preference that changes future behaviour. A single override is noise; a repeated, CONSISTENT
// one is a signal. An override that flips the value resets the evidence (the preference isn't stable).
//
// Persistence (P7.1 pattern) is what makes it cross-session: a preference learned this session is
// reloaded next session, so the adaptation actually changes next-session behaviour.
package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sort"
)

// DefaultPersonThreshold is how many consistent overrides promote an observation to a learned preference.
const DefaultPersonThreshold = 3

// Preference is a learned way to interact with a person: a trait (e.g. "verbosity"), the overridden
// value (e.g. "terse"), the evidence count, and whether it has crossed the learning threshold.
type Preference struct {
	Trait    string
	Value    string
	Evidence int
	Learned  bool
}

// PersonRegistry accumulates override observations per trait and promotes consistent ones to learned
// preferences.
type PersonRegistry struct {
	prefs     map[string]*Preference
	threshold int
}

// NewPersonRegistry builds a person store. threshold<=0 uses DefaultPersonThreshold.
func NewPersonRegistry(threshold int) *PersonRegistry {
	if threshold <= 0 {
		threshold = DefaultPersonThreshold
	}
	return &PersonRegistry{prefs: map[string]*Preference{}, threshold: threshold}
}

// ObserveOverride records that the user overrode the default for trait toward value (e.g. the system
// was verbose and the user asked for terse → ObserveOverride("verbosity","terse")). Consistent repeats
// accumulate evidence; a DIFFERENT value resets the evidence (the preference isn't stable yet). Returns
// true the moment the preference becomes LEARNED (crosses the threshold).
func (r *PersonRegistry) ObserveOverride(trait, value string) bool {
	p := r.prefs[trait]
	if p == nil || p.Value != value {
		p = &Preference{Trait: trait, Value: value, Evidence: 0}
		r.prefs[trait] = p
	}
	p.Evidence++
	justLearned := false
	if !p.Learned && p.Evidence >= r.threshold {
		p.Learned = true
		justLearned = true
	}
	return justLearned
}

// Preference returns the LEARNED preference for a trait (ok=false if none has been learned yet).
func (r *PersonRegistry) Preference(trait string) (Preference, bool) {
	if p, ok := r.prefs[trait]; ok && p.Learned {
		return *p, true
	}
	return Preference{}, false
}

// Applied returns every learned preference (the adaptations that change behaviour), trait-sorted for
// determinism.
func (r *PersonRegistry) Applied() []Preference {
	var out []Preference
	for _, p := range r.prefs {
		if p.Learned {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Trait < out[j].Trait })
	return out
}

// Seed re-admits a persisted preference verbatim (cross-session persistence, M4): it inserts the
// preference with its evidence + learned flag intact, so an adaptation learned on a prior run is active
// next start. A blank trait is ignored.
func (r *PersonRegistry) Seed(p Preference) {
	if p.Trait == "" {
		return
	}
	cp := p
	r.prefs[p.Trait] = &cp
}

// All returns every preference (learned or accumulating), trait-sorted for determinism — the export the
// persist layer writes to the store (so an in-progress adaptation also survives, not just a learned one).
func (r *PersonRegistry) All() []Preference {
	out := make([]Preference, 0, len(r.prefs))
	for _, t := range sortedTraits(r.prefs) {
		out = append(out, *r.prefs[t])
	}
	return out
}

// Save writes every preference (learned or accumulating) as one JSON line.
func (r *PersonRegistry) Save(w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, t := range sortedTraits(r.prefs) {
		if err := enc.Encode(*r.prefs[t]); err != nil {
			return err
		}
	}
	return nil
}

// Load reads preferences back (best-effort), so a person adaptation survives into the next session.
func (r *PersonRegistry) Load(rd io.Reader) (int, error) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var p Preference
		if err := json.Unmarshal(line, &p); err != nil || p.Trait == "" {
			continue
		}
		cp := p
		r.prefs[p.Trait] = &cp
		n++
	}
	return n, sc.Err()
}

func sortedTraits(m map[string]*Preference) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
