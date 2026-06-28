package realhard

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// tasks_heldout_test.go — MUTATION + INTEGRITY coverage for the held-out
// generalization set (tasks_heldout.go) and the --only-task FilterTasks selector.
//
// The held-out set's WHOLE POINT is to measure the GENERAL grounding-completeness
// / lure-resistance capability without leaking the in-suite trap KEYWORDS. So
// these tests assert, beyond the standard oracle soundness (ground truth solves /
// lure fails, which the shared oracle_test.go already covers for ALL tasks incl.
// these):
//
//   1. KEYWORD HYGIENE — no held-out task's MATERIAL contains the banned tokens
//      (deprecated / superseded / erratum / flag / multiplier). This is the
//      integrity contract: a fix that lifts here cannot be keyword-matching.
//   2. SHAPE — exactly 6 tasks, 3 backtrack-class + 3 multi-hop-class, with the
//      negative-control being a decline task whose first-named value is a TRAP.
//   3. RE-DERIVED EXPECTEDS — the computed answers (the unit conversion, the
//      recency picks, the chain endpoints) are re-derived in code, so the
//      Expected values are PROVEN, not hand-asserted (mirrors oracle_test.go's
//      TestLongHorizonGroundTruthArithmetic).
//   4. NEGATIVE CONTROL behaviour — the decline task credits a decline, rejects
//      the asset-TTL lure (a forced supersession / dangling-pointer confabulation),
//      AND rejects an over-firing fix that "resolves" the absent value.
//   5. The --only-task FILTER selects the right subset and is byte-identical when
//      empty.

// bannedTrapKeywords are the in-suite trap tokens the held-out set must NOT use
// in its material — so a lift here proves the general capability, not keyword
// reflex. Lower-cased; the check is case-insensitive on the material.
var bannedTrapKeywords = []string{
	"deprecated", "superseded", "erratum", "flag", "multiplier",
}

// heldOutIDs is the stable ID set of the held-out tasks (used by the filter test).
var heldOutIDs = []string{
	"realhard-held-0001", "realhard-held-0002", "realhard-held-0003",
	"realhard-held-0004", "realhard-held-0005", "realhard-held-0006",
}

// TestHeldOutKeywordHygiene is the load-bearing INTEGRITY check: none of the six
// held-out tasks' materials may contain a banned in-suite trap keyword. If this
// fails, the held-out set is no longer a clean generalization probe (it could be
// passed by keyword-matching) and MUST be reworded.
func TestHeldOutKeywordHygiene(t *testing.T) {
	for _, task := range heldOutTasks() {
		var hay strings.Builder
		// Check the MATERIAL (what the reader grounds against). The prompt is
		// excluded deliberately — the prompt may say "value in force", but the
		// MATERIAL must carry no banned token, since that is what a keyword fix
		// would react to.
		for _, content := range task.Materials {
			hay.WriteString(strings.ToLower(content))
			hay.WriteString("\n")
		}
		material := hay.String()
		for _, kw := range bannedTrapKeywords {
			if strings.Contains(material, kw) {
				t.Errorf("[%s] MATERIAL contains banned trap keyword %q — the held-out set must "+
					"probe the capability WITHOUT the in-suite keywords (reword the trap)", task.ID, kw)
			}
		}
	}
}

// TestHeldOutShape asserts the composition: 6 tasks, IDs stable, 3 backtrack-class
// + 3 multi-hop-class, exactly one decline (the negative control), all IDs unique
// and present in the full suite.
func TestHeldOutShape(t *testing.T) {
	tasks := heldOutTasks()
	if len(tasks) != 6 {
		t.Fatalf("held-out set must have exactly 6 tasks, got %d", len(tasks))
	}
	caps := map[Capability]int{}
	oracles := map[OracleKind]int{}
	ids := map[string]bool{}
	for _, task := range tasks {
		caps[task.Capability]++
		oracles[task.Oracle]++
		if ids[task.ID] {
			t.Errorf("duplicate held-out task ID %s", task.ID)
		}
		ids[task.ID] = true
		if task.Capability != CapAdaptiveBacktracking && task.Capability != CapMultiHopGrounding {
			t.Errorf("[%s] held-out tasks must reuse CapAdaptiveBacktracking/CapMultiHopGrounding, got %s",
				task.ID, task.Capability)
		}
		if strings.TrimSpace(task.Why) == "" {
			t.Errorf("[%s] held-out task must document its trap in Why", task.ID)
		}
	}
	if caps[CapAdaptiveBacktracking] != 3 {
		t.Errorf("expected 3 backtrack-class held-out tasks, got %d", caps[CapAdaptiveBacktracking])
	}
	if caps[CapMultiHopGrounding] != 3 {
		t.Errorf("expected 3 multi-hop-class held-out tasks, got %d", caps[CapMultiHopGrounding])
	}
	if oracles[OracleDecline] != 1 {
		t.Errorf("expected exactly 1 decline (negative-control) held-out task, got %d", oracles[OracleDecline])
	}
	// every held-out task must appear in the aggregated suite (registration check)
	suiteIDs := map[string]bool{}
	for _, task := range Tasks() {
		suiteIDs[task.ID] = true
	}
	for _, id := range heldOutIDs {
		if !suiteIDs[id] {
			t.Errorf("held-out task %s is NOT registered in Tasks()", id)
		}
	}
}

// TestHeldOutGroundTruthArithmetic RE-DERIVES every COMPUTED held-out answer in
// code so the Expected values are proven, not hand-asserted (mirrors
// TestLongHorizonGroundTruthArithmetic). The recency picks and the chain
// endpoints are constants of the material; the conversions are arithmetic.
func TestHeldOutGroundTruthArithmetic(t *testing.T) {
	// 0001: recency by version — v3.2 (8s) post-dates v1.0 (30s) -> in force = 8.
	// The pick is the more-recent stamp; assert we picked the v3.2 value.
	legacyVal, currentVal := 30, 8
	legacyVer, currentVer := versionOrder("v1.0"), versionOrder("v3.2")
	inForce := legacyVal
	if currentVer > legacyVer {
		inForce = currentVal
	}
	assertHeldExpected(t, "realhard-held-0001", inForce)

	// 0002: unit conversion — 2048 MB / 1024 = 2 GB (integer).
	rawMB := 2048
	gb := rawMB / 1024
	assertHeldExpected(t, "realhard-held-0002", gb)

	// 0003: recency by date — 2024-11-02 (16) post-dates 2023-04-10 (4).
	type dated struct {
		date string
		val  int
	}
	entries := []dated{{"2023-04-10", 4}, {"2024-11-02", 16}}
	latest := entries[0]
	for _, e := range entries[1:] {
		if e.date > latest.date { // ISO dates compare lexicographically
			latest = e
		}
	}
	assertHeldExpected(t, "realhard-held-0003", latest.val)

	// 0004: reworded pointer chain — payments | standard 99.5 | enterprise 99.99.
	// Re-derive by parsing the appendix row the prompt asks for (enterprise col).
	enterprisePayments := parseSLARow(t, "payments")
	assertHeldExpectedF(t, "realhard-held-0004", enterprisePayments)

	// 0005: 3-file chain endpoint — ingest route inflight_cap = 12.
	ingestCap := parseInflightCap(t, "ingest")
	assertHeldExpected(t, "realhard-held-0005", ingestCap)

	// 0006: negative control — Expected is "" (decline); no arithmetic, asserted
	// in TestHeldOutNegativeControl below.
	if got := taskByID(t, "realhard-held-0006").Expected; got != "" {
		t.Errorf("[realhard-held-0006] negative control Expected must be empty (decline), got %q", got)
	}
}

// TestHeldOutLureFails confirms each held-out task's lure FAILS its oracle (the
// shared TestPriorLureFails covers this for ALL tasks, but we assert it
// explicitly on the held-out set so a future refactor of the shared test cannot
// silently drop the held-out coverage).
func TestHeldOutLureFails(t *testing.T) {
	for _, task := range heldOutTasks() {
		if strings.TrimSpace(task.PriorLure) == "" {
			t.Errorf("[%s] held-out task must declare a PriorLure (the headroom hypothesis)", task.ID)
			continue
		}
		var lureAns string
		switch task.Oracle {
		case OracleDecline:
			lureAns = "The value is " + task.PriorLure + "."
		default:
			lureAns = "After grounding, the answer is " + task.PriorLure + "."
		}
		if v := Score(task, lureAns); v.Solved {
			t.Errorf("[%s] the lure %q must FAIL the oracle, but SOLVED: %s",
				task.ID, task.PriorLure, v.Reason)
		}
	}
}

// TestHeldOutGroundTruthSolves confirms each held-out ground truth SOLVES (also
// covered by the shared TestGroundTruthSolves; asserted here for locality).
func TestHeldOutGroundTruthSolves(t *testing.T) {
	for _, task := range heldOutTasks() {
		if v := Score(task, groundTruthAnswer(task)); !v.Solved {
			t.Errorf("[%s] ground truth must SOLVE, got fail: %s (answer=%q)",
				task.ID, v.Reason, groundTruthAnswer(task))
		}
	}
}

// TestHeldOutNegativeControl is the behaviour spec for task 0006 (the negative
// control). It must:
//   - CREDIT an honest decline ("not in the material");
//   - REJECT the asset-TTL lure (300) — the forced-supersession / dangling-pointer
//     confabulation;
//   - REJECT a fabricated resolution of the absent value (any confident number);
//   - REJECT an empty give-up (not an explicit decline).
func TestHeldOutNegativeControl(t *testing.T) {
	task := taskByID(t, "realhard-held-0006")
	if task.Oracle != OracleDecline {
		t.Fatalf("0006 must be an OracleDecline negative control, got %s", task.Oracle)
	}

	// honest declines SOLVE
	for _, ok := range []string{
		"The session cache TTL is not determinable from this file; it is provisioned externally.",
		"That value is not defined in this repository.",
		"I cannot determine the session cache TTL from the material.",
	} {
		if v := Score(task, ok); !v.Solved {
			t.Errorf("0006: honest decline %q must SOLVE, got: %s", ok, v.Reason)
		}
	}

	// the asset-TTL lure (300) — the trap value present in the file for a DIFFERENT
	// key — must FAIL (asserting it is the confabulation).
	for _, bad := range []string{
		"The session cache TTL is 300 seconds.",
		"It is 300.",
	} {
		v := Score(task, bad)
		if v.Solved {
			t.Errorf("0006: asserting the asset-TTL lure %q must FAIL (confabulation), got SOLVE: %s", bad, v.Reason)
		}
		if !v.AssertedLure {
			t.Errorf("0006: answer %q asserts the lure 300 but AssertedLure was not flagged", bad)
		}
	}

	// a fabricated resolution (an over-firing fix inventing a number to satisfy the
	// dangling vault pointer) — any confident number — must FAIL.
	for _, fab := range []string{
		"Following the vault pointer, the TTL is 600 seconds.",
		"The TTL resolves to 120.",
	} {
		if v := Score(task, fab); v.Solved {
			t.Errorf("0006: a fabricated resolution %q must FAIL (confabulation), got SOLVE: %s", fab, v.Reason)
		}
	}

	// empty / give-up FAILS (not an explicit decline).
	for _, gaveUp := range []string{"", "   ", "\n"} {
		if v := Score(task, gaveUp); v.Solved {
			t.Errorf("0006: empty/give-up %q must FAIL (not an explicit decline), got SOLVE: %s", gaveUp, v.Reason)
		}
	}
}

// --- the --only-task FilterTasks selector --------------------------------------

func TestFilterTasksEmptyIsAll(t *testing.T) {
	all := Tasks()
	for _, only := range []string{"", "   ", ",", " , , "} {
		got := FilterTasks(all, only)
		if len(got) != len(all) {
			t.Errorf("FilterTasks(all, %q) must return ALL %d tasks, got %d", only, len(all), len(got))
		}
		// byte-identical ordering
		for i := range got {
			if got[i].ID != all[i].ID {
				t.Errorf("FilterTasks(all, %q) reordered: [%d] = %s, want %s", only, i, got[i].ID, all[i].ID)
			}
		}
	}
}

func TestFilterTasksSelectsHeldOut(t *testing.T) {
	got := FilterTasks(Tasks(), "held")
	if len(got) != 6 {
		t.Fatalf("--only-task=held must select the 6 held-out tasks, got %d", len(got))
	}
	want := map[string]bool{}
	for _, id := range heldOutIDs {
		want[id] = true
	}
	for _, task := range got {
		if !want[task.ID] {
			t.Errorf("--only-task=held selected non-held-out task %s", task.ID)
		}
	}
}

func TestFilterTasksMultiSubstring(t *testing.T) {
	// "back,mhop" = the in-suite collateral backtrack + multi-hop sets (NOT held-out).
	got := FilterTasks(Tasks(), "back,mhop")
	if len(got) == 0 {
		t.Fatal("--only-task=back,mhop selected no tasks")
	}
	for _, task := range got {
		if !strings.Contains(task.ID, "back") && !strings.Contains(task.ID, "mhop") {
			t.Errorf("--only-task=back,mhop selected unrelated task %s", task.ID)
		}
		// must NOT include the held-out tasks (their IDs carry 'held', not 'back'/'mhop')
		if strings.Contains(task.ID, "held") {
			t.Errorf("--only-task=back,mhop wrongly selected held-out task %s", task.ID)
		}
	}
	// the collateral count = in-suite backtrack(2) + multihop(3) = 5
	if len(got) != 5 {
		t.Errorf("--only-task=back,mhop expected 5 in-suite tasks (2 back + 3 mhop), got %d", len(got))
	}
}

func TestFilterTasksSingleID(t *testing.T) {
	got := FilterTasks(Tasks(), "realhard-held-0006")
	if len(got) != 1 || got[0].ID != "realhard-held-0006" {
		t.Errorf("--only-task=realhard-held-0006 must select exactly that task, got %v", idsOf(got))
	}
}

func TestFilterTasksNoMatch(t *testing.T) {
	got := FilterTasks(Tasks(), "does-not-exist-xyz")
	if len(got) != 0 {
		t.Errorf("--only-task with no match must select 0 tasks, got %d", len(got))
	}
}

// --- helpers -------------------------------------------------------------------

func idsOf(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, tk := range tasks {
		out[i] = tk.ID
	}
	return out
}

// versionOrder maps a "vMAJOR.MINOR" tag to a sortable integer (major*1000+minor)
// so the recency check is a real comparison, not a hand-coded pick.
func versionOrder(v string) int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, ".", 2)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) == 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major*1000 + minor
}

// parseSLARow reads the held-out 0004 appendix material and returns the
// enterprise-column figure for the named service, so 0004's Expected is derived
// from the material, not hand-typed.
func parseSLARow(t *testing.T, service string) float64 {
	t.Helper()
	task := taskByID(t, "realhard-held-0004")
	appendix := task.Materials["docs/appendix-sla.md"]
	for _, line := range strings.Split(appendix, "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) != 3 {
			continue
		}
		if strings.TrimSpace(cols[0]) == service {
			ent, err := strconv.ParseFloat(strings.TrimSpace(cols[2]), 64)
			if err != nil {
				t.Fatalf("0004: cannot parse enterprise col %q: %v", cols[2], err)
			}
			return ent
		}
	}
	t.Fatalf("0004: service %q not found in appendix", service)
	return 0
}

// parseInflightCap reads the held-out 0005 defaults material and returns the
// inflight_cap for the named route — deriving 0005's Expected from the material.
func parseInflightCap(t *testing.T, route string) int {
	t.Helper()
	task := taskByID(t, "realhard-held-0005")
	defaults := task.Materials["routing/admission-defaults.yaml"]
	lines := strings.Split(defaults, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == route+":" {
			// the next line holds inflight_cap: N (possibly with a trailing comment)
			for _, follow := range lines[i+1:] {
				ft := strings.TrimSpace(follow)
				if strings.HasPrefix(ft, "inflight_cap:") {
					rest := strings.TrimSpace(strings.TrimPrefix(ft, "inflight_cap:"))
					// strip a trailing # comment
					if hash := strings.Index(rest, "#"); hash >= 0 {
						rest = strings.TrimSpace(rest[:hash])
					}
					n, err := strconv.Atoi(rest)
					if err != nil {
						t.Fatalf("0005: cannot parse inflight_cap %q for route %q: %v", rest, route, err)
					}
					return n
				}
				// stop at the next route key (a non-indented-cap line)
				if strings.HasSuffix(ft, ":") && !strings.HasPrefix(ft, "inflight_cap") {
					break
				}
			}
		}
	}
	t.Fatalf("0005: route %q inflight_cap not found", route)
	return 0
}

func assertHeldExpected(t *testing.T, id string, got int) {
	t.Helper()
	task := taskByID(t, id)
	if task.Expected != strconv.Itoa(got) {
		t.Errorf("[%s] re-derived ground truth = %d, but task.Expected = %q (FIX one of them)",
			id, got, task.Expected)
	}
}

func assertHeldExpectedF(t *testing.T, id string, got float64) {
	t.Helper()
	task := taskByID(t, id)
	want, err := strconv.ParseFloat(task.Expected, 64)
	if err != nil {
		t.Fatalf("[%s] task.Expected %q is not a number: %v", id, task.Expected, err)
	}
	if math.Abs(want-got) > 1e-9 {
		t.Errorf("[%s] re-derived ground truth = %g, but task.Expected = %q (FIX one of them)",
			id, got, task.Expected)
	}
}
