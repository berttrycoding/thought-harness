// gateroute_test.go — tests for the action redesign's two-axis taxonomy + gate router + §4 invariant
// (docs/cognition/03-action.md). Pure, deterministic, offline (no executor, no model, no I/O).
package action

import "testing"

// --- (1) the two-axis taxonomy + classifier ------------------------------------------------------

// TestOperationReachStrings pins the §3.10a wire values (the category registry round-trips through
// these strings; ReachLocalWorld deliberately serializes to "local", not "localworld").
func TestOperationReachStrings(t *testing.T) {
	opCases := map[Operation]string{OpInspect: "inspect", OpMutate: "mutate", OpExecute: "execute"}
	for op, want := range opCases {
		if got := op.String(); got != want {
			t.Errorf("Operation(%d).String() = %q, want %q", op, got, want)
		}
	}
	reachCases := map[Reach]string{ReachSelf: "self", ReachLocalWorld: "local", ReachExternal: "external"}
	for r, want := range reachCases {
		if got := r.String(); got != want {
			t.Errorf("Reach(%d).String() = %q, want %q", r, got, want)
		}
	}
	if got := (TaxClass{Op: OpInspect, Reach: ReachLocalWorld}).String(); got != "inspect/local" {
		t.Errorf("TaxClass.String() = %q, want %q", got, "inspect/local")
	}
}

// TestClassifyFlatCategory checks the flat {inspect,mutate,execute,external} set maps onto the
// (Operation x Reach) model exactly as the §8 delta row specifies — including that the flat "external"
// tag was a network READ (inspect/external), not a write.
func TestClassifyFlatCategory(t *testing.T) {
	cases := []struct {
		flat   string
		wantOp Operation
		wantR  Reach
		wantOK bool
	}{
		{"inspect", OpInspect, ReachLocalWorld, true},
		{"mutate", OpMutate, ReachLocalWorld, true},
		{"execute", OpExecute, ReachLocalWorld, true},
		{"external", OpInspect, ReachExternal, true},      // the flat tag meant a distal SENSE
		{"  Inspect  ", OpInspect, ReachLocalWorld, true}, // case + space tolerant
		{"EXTERNAL", OpInspect, ReachExternal, true},
		{"bogus", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		got, ok := ClassifyFlatCategory(c.flat)
		if ok != c.wantOK {
			t.Errorf("ClassifyFlatCategory(%q) ok = %v, want %v", c.flat, ok, c.wantOK)
			continue
		}
		if !c.wantOK {
			continue
		}
		if got.Op != c.wantOp || got.Reach != c.wantR {
			t.Errorf("ClassifyFlatCategory(%q) = %v, want {%v %v}", c.flat, got, c.wantOp, c.wantR)
		}
	}
}

// --- (2) the gate router -------------------------------------------------------------------------

// TestRouteThreeClasses is the headline routing test from the slice spec: a local inspect routes FREE
// (local-sense); a network inspect routes BUDGETED (distal-sense); a mutate / external write routes to
// WORLD-CHANGE (conscious-gated). Plus the execute split (03 §6).
func TestRouteThreeClasses(t *testing.T) {
	// A generous network policy so the distal-sense rows are about the CLASS, not the quota.
	open := RouteBounds{NetworkEnabled: true, NetworkQuota: 10}

	cases := []struct {
		name      string
		tc        TaxClass
		bounds    RouteBounds
		wantClass ToolClass
		wantAuth  bool // NeedsConsciousAuthor
		wantQuota bool // QuotaExceeded
	}{
		// local inspect -> FREE
		{"local inspect (self)", TaxClass{OpInspect, ReachSelf}, open, ClassLocalSense, false, false},
		{"local inspect (world)", TaxClass{OpInspect, ReachLocalWorld}, open, ClassLocalSense, false, false},
		// network inspect -> BUDGETED (within an enabled policy + quota)
		{"network inspect", TaxClass{OpInspect, ReachExternal}, open, ClassDistalSense, false, false},
		// mutate (any reach) -> WORLD-CHANGE, conscious-authored
		{"local mutate", TaxClass{OpMutate, ReachLocalWorld}, open, ClassWorldChange, true, false},
		{"external write (mutate)", TaxClass{OpMutate, ReachExternal}, open, ClassWorldChange, true, false},
		{"self mutate", TaxClass{OpMutate, ReachSelf}, open, ClassWorldChange, true, false},
		// execute splits at the sandbox (03 §6): local run = sense; external run = world-change
		{"local execute (in-sandbox probe)", TaxClass{OpExecute, ReachLocalWorld}, open, ClassLocalSense, false, false},
		{"external execute (escapes sandbox)", TaxClass{OpExecute, ReachExternal}, open, ClassWorldChange, true, false},
	}
	for _, c := range cases {
		dec := Route(c.tc, c.bounds)
		if dec.Class != c.wantClass {
			t.Errorf("%s: Route(%v).Class = %v, want %v", c.name, c.tc, dec.Class, c.wantClass)
		}
		if dec.NeedsConsciousAuthor != c.wantAuth {
			t.Errorf("%s: NeedsConsciousAuthor = %v, want %v", c.name, dec.NeedsConsciousAuthor, c.wantAuth)
		}
		if dec.QuotaExceeded != c.wantQuota {
			t.Errorf("%s: QuotaExceeded = %v, want %v", c.name, dec.QuotaExceeded, c.wantQuota)
		}
	}
}

// TestRouteDistalSenseQuota covers the offline-safe distal-sense gate (03 §7): a network read is
// flagged QuotaExceeded when the policy is OFF or the budget is spent, but the class stays DistalSense
// (the executor declines + falls back to local perception; it does not reclassify or error).
func TestRouteDistalSenseQuota(t *testing.T) {
	netRead := TaxClass{OpInspect, ReachExternal}

	cases := []struct {
		name      string
		bounds    RouteBounds
		wantQuota bool
	}{
		{"policy on, budget left", RouteBounds{NetworkEnabled: true, NetworkQuota: 3}, false},
		{"policy on, budget spent", RouteBounds{NetworkEnabled: true, NetworkQuota: 0}, true},
		{"policy on, budget negative", RouteBounds{NetworkEnabled: true, NetworkQuota: -1}, true},
		{"policy off (offline default)", RouteBounds{}, true},
		{"policy off but budget set", RouteBounds{NetworkEnabled: false, NetworkQuota: 5}, true},
	}
	for _, c := range cases {
		dec := Route(netRead, c.bounds)
		if dec.Class != ClassDistalSense {
			t.Errorf("%s: class = %v, want DistalSense", c.name, dec.Class)
		}
		if dec.QuotaExceeded != c.wantQuota {
			t.Errorf("%s: QuotaExceeded = %v, want %v", c.name, dec.QuotaExceeded, c.wantQuota)
		}
		if dec.NeedsConsciousAuthor {
			t.Errorf("%s: a read must never need conscious authoring", c.name)
		}
	}
}

// TestRouteClassifyComposes checks the classifier + router compose end-to-end: a flat tag classifies,
// then routes to the expected class (the path the executor will take from a legacy flat-tagged tool).
func TestRouteClassifyComposes(t *testing.T) {
	open := RouteBounds{NetworkEnabled: true, NetworkQuota: 10}
	cases := []struct {
		flat      string
		wantClass ToolClass
	}{
		{"inspect", ClassLocalSense},
		{"external", ClassDistalSense}, // flat "external" = network read
		{"mutate", ClassWorldChange},
		{"execute", ClassLocalSense}, // local execute = in-sandbox probe
	}
	for _, c := range cases {
		tc, ok := ClassifyFlatCategory(c.flat)
		if !ok {
			t.Fatalf("ClassifyFlatCategory(%q) failed", c.flat)
		}
		if dec := Route(tc, open); dec.Class != c.wantClass {
			t.Errorf("flat %q -> %v, want %v", c.flat, dec.Class, c.wantClass)
		}
	}
}

func TestToolClassStrings(t *testing.T) {
	cases := map[ToolClass]string{
		ClassLocalSense:  "local_sense",
		ClassDistalSense: "distal_sense",
		ClassWorldChange: "world_change",
	}
	for cl, want := range cases {
		if got := cl.String(); got != want {
			t.Errorf("ToolClass(%d).String() = %q, want %q", cl, got, want)
		}
	}
}

// --- (3) the §4 invariant: a mutate never targets the self-substrate -----------------------------

// TestTargetsSelfSubstrate covers both arms of the §4-open mechanism (capability flag + path
// namespace), the segment-boundary anchoring, and the world-path negatives.
func TestTargetsSelfSubstrate(t *testing.T) {
	selfTargets := []string{
		// capability-flag arm
		"self",
		"self:registry",
		"self/memory",
		"SELF", // case-insensitive
		// path-namespace arm (default roots)
		"data/registry/specialists.jsonl",
		"data/registry", // exact root
		"data/memory/store.jsonl",
		"data/graph/state.json",
		"data/state/engine.json",
		"runs/2026-06-15/audit.jsonl",
		".thought/self.json",
		"./data/registry/x.jsonl", // leading ./ trimmed
		"/data/memory/y",          // leading / trimmed
		"data\\registry\\win.txt", // backslash normalized
	}
	for _, target := range selfTargets {
		if !TargetsSelfSubstrate(target) {
			t.Errorf("TargetsSelfSubstrate(%q) = false, want true (self-substrate)", target)
		}
	}

	worldTargets := []string{
		"",
		"internal/action/gateroute.go", // repo source = the world
		"README.md",
		"src/main.go",
		"datacenter/config.yaml", // must NOT match "data/" — segment boundary
		"data-export/out.csv",    // "data" is not "data/registry..."
		"myself/notes.txt",       // "self" only matches as the whole tag / self:/ self/ prefix
		"runsheet.txt",           // "runs/" needs the slash boundary
		"/etc/hosts",
		"https://example.com/page",
	}
	for _, target := range worldTargets {
		if TargetsSelfSubstrate(target) {
			t.Errorf("TargetsSelfSubstrate(%q) = true, want false (world target)", target)
		}
	}
}

// TestRefuseSelfMutation checks the composed executor predicate: ONLY a mutate aimed at the
// self-substrate is refused. An inspect / execute on the self-substrate is introspection / a self-probe
// (allowed), and a mutate on the world is a legitimate (conscious-gated) action (allowed by THIS
// predicate — the conscious-author gate is separate).
func TestRefuseSelfMutation(t *testing.T) {
	cases := []struct {
		name   string
		op     Operation
		target string
		want   bool
	}{
		{"mutate self-registry -> REFUSE", OpMutate, "data/registry/x.jsonl", true},
		{"mutate self flag -> REFUSE", OpMutate, "self:memory", true},
		{"mutate world repo -> allow", OpMutate, "internal/action/x.go", false},
		{"inspect self-registry -> allow (introspection)", OpInspect, "data/registry/x.jsonl", false},
		{"execute self -> allow (self-probe)", OpExecute, "data/state/run.sh", false},
		{"inspect world -> allow", OpInspect, "README.md", false},
	}
	for _, c := range cases {
		if got := RefuseSelfMutation(c.op, c.target); got != c.want {
			t.Errorf("%s: RefuseSelfMutation(%v, %q) = %v, want %v", c.name, c.op, c.target, got, c.want)
		}
	}
}

// TestRegisterSelfSubstrateRoot checks the engine extension point: a custom state-dir root becomes
// covered by the §4 invariant after registration, and registration is idempotent + ignores blanks.
// It saves/restores the package set so the test is self-contained (no cross-test leakage).
func TestRegisterSelfSubstrateRoot(t *testing.T) {
	saved := SelfSubstrateRoots()
	t.Cleanup(func() { selfSubstrateRoots = saved })

	custom := "data/registry-claude"
	if TargetsSelfSubstrate(custom + "/row.jsonl") {
		t.Fatalf("custom root should not yet be covered")
	}
	RegisterSelfSubstrateRoot(custom)
	if !TargetsSelfSubstrate(custom + "/row.jsonl") {
		t.Errorf("after RegisterSelfSubstrateRoot(%q), a write under it should be self-substrate", custom)
	}

	// Idempotent: a second registration does not duplicate the prefix.
	before := len(selfSubstrateRoots)
	RegisterSelfSubstrateRoot(custom)
	if len(selfSubstrateRoots) != before {
		t.Errorf("duplicate RegisterSelfSubstrateRoot grew the set: %d -> %d", before, len(selfSubstrateRoots))
	}

	// Blank input is ignored.
	RegisterSelfSubstrateRoot("   ")
	if len(selfSubstrateRoots) != before {
		t.Errorf("blank RegisterSelfSubstrateRoot grew the set")
	}
}
