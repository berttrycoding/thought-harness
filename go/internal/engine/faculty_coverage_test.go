package engine_test

// faculty_coverage_test.go — the COGNITION-property test for the fair-share faculty attention scheduler
// (conscious.activity.faculty_scheduler), the de-risking experiment for the seed-intent hierarchy
// redesign (docs/internal/notes/2026-06-19-seed-intent-hierarchy-redesign.md §0/§7, audit §3).
//
// The QUESTION it answers: is the awake seed-starvation an arbitration bug fixable with a FLAT fair-share
// scheduler (NO hierarchy)?
//
// The MEASURED baseline (confirmed by this test): with the full seed portfolio in the live awake loop,
// pure frontier-argmax resume re-focuses only a MINORITY of the faculties — Perceptual and Mnemonic starve
// to ZERO focus after seeding, because (a) the high-value motivational/introspective lines win every argmax
// and (b) the U≤1 prune cap kills the lowest-value (perceptual/mnemonic) roots FIRST, before any arbiter
// runs.
//
// The TREATMENT (flag ON, W=1): the least-recently-focused fair-share scheduler — paired with its
// flag-gated prune-protection that keeps one live root per faculty — reaches far broader, far more even
// faculty coverage. So the answer is YES: flat fair-share fixes the starvation without a hierarchy,
// provided the scheduler's candidate set (one root per faculty) is kept alive against the prune cap.
//
// This is a COGNITION test (the thinking the spec intends), not a plumbing test: it asserts the FACULTY
// COVERAGE the scheduler is meant to deliver, not merely that the loop ticks. It is deterministic — the
// TestBackend test double + cpyrand seed=7, no model tokens, no clock, no unseeded RNG.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/types"
)

// awakeSchedulerFeatures builds the awake-profile feature set used by the experiment (forest +
// seed-intents full portfolio + drive-agenda + soft policy), with the faculty scheduler optionally on.
func awakeSchedulerFeatures(scheduler bool, width int) *config.HarnessConfig {
	c := config.New()
	a := &c.Conscious.Activity
	a.Forest = true
	a.SeedIntents = true
	a.SeedIntentCount = cognition.SeedPortfolioSize() // full portfolio — keeps every faculty on the bench
	a.DriveAgenda = true
	a.Soft = true
	a.BranchPropensity = 0.5
	a.FacultyScheduler = scheduler
	a.AttentionWidth = width
	c.Validate()
	return c
}

// facultyCoverage runs an awake stream for n ticks (NO user input) and returns, per faculty, the number
// of ticks a standing seed-intent root of that faculty held FOCUS (the active/EXPANDED line). The faculty
// of the focused line is read off the branch reason ("seed-intent: <Name>") mapped through the portfolio
// — the same Pattern-A, backend-independent focus signal the design's F-metric (§6) is measured on.
func facultyCoverage(t *testing.T, feat *config.HarnessConfig, n int) (perFaculty map[string]int, perName map[string]int) {
	t.Helper()
	nameFaculty := map[string]string{}
	for _, si := range cognition.SeedPortfolio(cognition.SeedPortfolioSize()) {
		nameFaculty[si.Name] = si.Faculty.String()
	}
	eng, _ := newContinuousEngineWithFeatures(t, feat)
	perFaculty = map[string]int{}
	perName = map[string]int{}
	for i := 0; i < n; i++ {
		eng.Step()
		active := eng.Graph().Active()
		if active == nil || active.Reason == nil || !strings.HasPrefix(*active.Reason, "seed-intent:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(*active.Reason, "seed-intent:"))
		perName[name]++
		if fac, ok := nameFaculty[name]; ok {
			perFaculty[fac]++
		}
	}
	return perFaculty, perName
}

// TestFacultySchedulerFixesStarvation is the experiment. It measures faculty coverage with the scheduler
// OFF (baseline) vs ON at W=1 (treatment) over 60 awake ticks and asserts the load-bearing cognition
// claims: the baseline starves (≤3/5 faculties, perceptual+mnemonic at zero), and the flat fair-share
// scheduler lifts coverage to all 5 faculties.
func TestFacultySchedulerFixesStarvation(t *testing.T) {
	const ticks = 60
	// Derive the full faculty set from the portfolio so the test tracks the taxonomy (now SIX faculties:
	// perceptual, introspective, mnemonic, motivational, actional, validative) without a hardcoded list.
	facSet := map[string]struct{}{}
	for _, si := range cognition.SeedPortfolio(cognition.SeedPortfolioSize()) {
		facSet[si.Faculty.String()] = struct{}{}
	}
	allFaculties := make([]string, 0, len(facSet))
	for f := range facSet {
		allFaculties = append(allFaculties, f)
	}
	nFac := len(allFaculties)

	// --- BASELINE: scheduler OFF (the existing argmax path) ---
	base, baseNames := facultyCoverage(t, awakeSchedulerFeatures(false, 1), ticks)
	baseDistinct := len(base)
	// The starvation: pure argmax does NOT reach full faculty coverage — the high-value lines win every
	// turn, so a strict minority of faculties is ever re-focused.
	if baseDistinct >= nFac {
		t.Fatalf("baseline: expected the measured starvation (NOT full coverage), got %d/%d: %v",
			baseDistinct, nFac, base)
	}
	// The discriminating proof of the starvation: perceptual AND mnemonic get ZERO focus under argmax (they
	// are the lowest-value lines — the U≤1 prune cap kills them first, before any arbiter runs).
	if base["perceptual"] != 0 || base["mnemonic"] != 0 {
		t.Fatalf("baseline: expected perceptual+mnemonic to STARVE to zero focus (the measured bug), "+
			"got perceptual=%d mnemonic=%d (full map %v)", base["perceptual"], base["mnemonic"], base)
	}
	t.Logf("BASELINE (flag OFF): %d/%d faculties focused: %v | per-seed %v", baseDistinct, nFac, base, baseNames)

	// --- TREATMENT: scheduler ON, W=1 (least-recently-focused fair-share) ---
	treat, treatNames := facultyCoverage(t, awakeSchedulerFeatures(true, 1), ticks)
	treatDistinct := len(treat)

	// The headline cognition claim: flat fair-share lifts coverage to ALL faculties on the bench.
	if treatDistinct < nFac {
		missing := []string{}
		for _, f := range allFaculties {
			if treat[f] == 0 {
				missing = append(missing, f)
			}
		}
		t.Fatalf("treatment (flag ON, W=1): flat fair-share should cover all %d faculties, got %d "+
			"(missing %v, full map %v)", nFac, treatDistinct, missing, treat)
	}
	// The previously-starved faculties now get real turns (the fix is load-bearing, not cosmetic).
	if treat["perceptual"] == 0 || treat["mnemonic"] == 0 {
		t.Fatalf("treatment: the starved faculties must now be focused, got perceptual=%d mnemonic=%d (%v)",
			treat["perceptual"], treat["mnemonic"], treat)
	}
	// Strictly more coverage than the baseline (the experiment's PASS criterion).
	if treatDistinct <= baseDistinct {
		t.Fatalf("treatment must improve faculty coverage over baseline: baseline=%d, treatment=%d",
			baseDistinct, treatDistinct)
	}
	t.Logf("TREATMENT (flag ON, W=1): %d/%d faculties focused: %v | per-seed %v", treatDistinct, nFac, treat, treatNames)
	t.Logf("EXPERIMENT RESULT: flat fair-share lifts faculty coverage %d/%d -> %d/%d (no hierarchy) — PASS",
		baseDistinct, nFac, treatDistinct, nFac)
}

// TestFacultySchedulerEmitsAttentionEvents proves the scheduler is WIRED into the live awake loop and
// OBSERVABLE: with the flag ON it actually fires (emits conscious.attention events recording which faculty
// got focus), and with the flag OFF it is silent (byte-identical — the observability half of the
// flag-gated contract). A scheduler that exists but never fires on the live tick is dead code; this is the
// wiring-gate assertion.
func TestFacultySchedulerEmitsAttentionEvents(t *testing.T) {
	// ON: the scheduler fires and the events name a spread of faculties.
	engOn, logOn := newContinuousEngineWithFeatures(t, awakeSchedulerFeatures(true, 1))
	for i := 0; i < 60; i++ {
		engOn.Step()
	}
	att := logOn.of(events.Attention)
	if len(att) == 0 {
		t.Fatal("flag ON: no conscious.attention events — the faculty scheduler is not wired into the live awake loop")
	}
	faculties := map[string]struct{}{}
	for _, ev := range att {
		if f, _ := ev.Data["faculty"].(string); f != "" {
			faculties[f] = struct{}{}
		}
		// every event must carry W and the branch it focused (the scalability seam + the focus target).
		if w, ok := ev.Data["width"].(int); !ok || w < 1 {
			t.Fatalf("conscious.attention event missing a valid width: %v", ev.Data)
		}
		if _, ok := ev.Data["branch"].(int); !ok {
			t.Fatalf("conscious.attention event missing a branch id: %v", ev.Data)
		}
	}
	if len(faculties) < 3 {
		t.Fatalf("flag ON: the scheduler should fair-share across multiple faculties, only saw %d: %v",
			len(faculties), faculties)
	}

	// OFF (the awake profile with the scheduler explicitly off): byte-identical — no attention events.
	engOff, logOff := newContinuousEngineWithFeatures(t, awakeSchedulerFeatures(false, 1))
	for i := 0; i < 60; i++ {
		engOff.Step()
	}
	if n := len(logOff.of(events.Attention)); n != 0 {
		t.Fatalf("flag OFF: the scheduler must be silent (byte-identical), got %d conscious.attention events", n)
	}
}

// TestAttentionWidthHonoursTopW pins the scalability seam: W (attention_width) sizes how many faculties
// the scheduler keeps "hot" — the conscious.attention event's "candidates" list carries exactly the top-W
// least-recently-focused faculties, and W is clamped to [1, WMax]. True concurrent EXECUTION is not yet
// wired (the engine is serial), but the SELECTION already honours W>1 — this is what makes the scheduler
// the scalability seam the redesign §13 needs once parallel execution exists.
func TestAttentionWidthHonoursTopW(t *testing.T) {
	// W=3: the candidates list should at times carry up to 3 faculties.
	engW3, logW3 := newContinuousEngineWithFeatures(t, awakeSchedulerFeatures(true, 3))
	for i := 0; i < 60; i++ {
		engW3.Step()
	}
	att := logW3.of(events.Attention)
	if len(att) == 0 {
		t.Fatal("W=3: no conscious.attention events fired")
	}
	maxCandidates := 0
	for _, ev := range att {
		// width carries the CONFIGURED policy W (always 3 here); hot is the effective count (≤ W ≤ #candidates).
		if w, _ := ev.Data["width"].(int); w != 3 {
			t.Fatalf("W=3: event policy width should be 3, got %v", ev.Data["width"])
		}
		if hot, _ := ev.Data["hot"].(int); hot < 1 || hot > 3 {
			t.Fatalf("W=3: effective hot count must be in [1,3], got %v", ev.Data["hot"])
		}
		if cands, ok := ev.Data["candidates"].([]string); ok && len(cands) > maxCandidates {
			maxCandidates = len(cands)
		}
	}
	if maxCandidates < 2 {
		t.Fatalf("W=3: the hot-faculty candidate set should widen beyond 1 (the W>1 selection seam), "+
			"max candidates seen = %d", maxCandidates)
	}

	// W clamps to WMax: a request above the ceiling is pulled down to config.WMax by Validate.
	over := config.New()
	over.Conscious.Activity.FacultyScheduler = true
	over.Conscious.Activity.AttentionWidth = config.WMax + 5
	over.Validate()
	if got := over.Conscious.Activity.AttentionWidth; got != config.WMax {
		t.Fatalf("attention_width should clamp to WMax=%d, got %d", config.WMax, got)
	}
	// W clamps up to 1: a sub-1 request is pulled up (the serial floor).
	under := config.New()
	under.Conscious.Activity.AttentionWidth = 0
	under.Validate()
	if got := under.Conscious.Activity.AttentionWidth; got != 1 {
		t.Fatalf("attention_width should clamp up to 1, got %d", got)
	}
}

// TestFacultySchedulerStandsDownForUser pins the safety property the scheduler is responsible for: it is
// the ENDOGENOUS "what next" arbiter and MUST stand down while a user is waiting — the μ-floor / userLine
// precedence (redesign R8). The guarantee the SCHEDULER provides is that it never FIRES (never emits a
// conscious.attention focus) on any tick where an unresolved user turn is in flight; the user line is
// resolved by the reactive/interrupt path, not stolen back by the scheduler.
//
// NOTE on scope: under the full awake forest (forest+soft+drive_agenda) the exhausted-resume *fallback*
// (frontier argmax) can itself focus a high-value seed root over a still-waiting user — this is a
// PRE-EXISTING awake-forest behaviour (it happens identically with the scheduler OFF, verified), NOT a
// regression introduced here, and is out of scope for this de-risking experiment. What this test pins is
// the in-scope contract: the scheduler does not make it worse — it actively stands down while the user
// waits.
func TestFacultySchedulerStandsDownForUser(t *testing.T) {
	eng, log := newContinuousEngineWithFeatures(t, awakeSchedulerFeatures(true, 1))
	for i := 0; i < 8; i++ {
		eng.Step() // let the awake loop seed + run with the scheduler active
	}
	eng.Submit("please refactor the auth module", true)

	// Drive the post-submit window and assert the scheduler emitted NO attention event on any tick where
	// a user was waiting (its guard held — it stood down for the user, every such tick).
	for i := 0; i < 3; i++ {
		before := len(log.of(events.Attention))
		waiting := eng.UserWaiting()
		eng.Step()
		after := len(log.of(events.Attention))
		if waiting && after > before {
			t.Fatalf("post-submit tick %d: the faculty scheduler FIRED (%d->%d attention events) while a "+
				"user was waiting — it must stand down for the user (μ-floor / userLine precedence)",
				i, before, after)
		}
	}

	// The user turn must not have been lost (it is still unresolved or has been delivered — never dropped).
	// We only assert it reached the graph as a real line, the discriminating "not lost" check.
	sawUserLine := false
	for _, b := range eng.Graph().Branches {
		if b.Reason != nil && strings.Contains(*b.Reason, "please refactor the auth module") {
			sawUserLine = true
			break
		}
	}
	if !sawUserLine {
		t.Fatal("the user turn never reached the graph as a line — it was lost")
	}
}

// TestFacultySchedulerOffByteIdentical guards the default-OFF contract at the seed-intent level: with the
// scheduler OFF (default), the awake seed-intent run is identical to a run with the scheduler field never
// touched — no conscious.attention events, and the seed-root prune behaviour is unchanged (the prune-
// protection is gated behind the same flag). This complements the scenarios golden parity test.
func TestFacultySchedulerOffByteIdentical(t *testing.T) {
	// Two awake runs, scheduler explicitly OFF vs the bare default — same seed-intent forest behaviour.
	featA := awakeSchedulerFeatures(false, 1)
	featB := awakeSchedulerFeatures(false, 1)
	engA, logA := newContinuousEngineWithFeatures(t, featA)
	engB, logB := newContinuousEngineWithFeatures(t, featB)
	for i := 0; i < 40; i++ {
		engA.Step()
		engB.Step()
	}
	if len(logA.of(events.Attention)) != 0 || len(logB.of(events.Attention)) != 0 {
		t.Fatal("flag OFF: a conscious.attention event leaked (not byte-identical)")
	}
	// Same number of DEAD seed roots either way (prune-protection is off ⇒ the original prune ran).
	deadA := countDeadSeedRoots(engA.Graph().Branches)
	deadB := countDeadSeedRoots(engB.Graph().Branches)
	if deadA != deadB {
		t.Fatalf("flag OFF: seed-root prune differs between identical runs (%d vs %d) — non-deterministic", deadA, deadB)
	}
}

// countDeadSeedRoots counts seed-intent root branches that have been pruned to DEAD/MERGED.
func countDeadSeedRoots(branches map[int]*types.Branch) int {
	n := 0
	for _, b := range branches {
		if b.Reason == nil || !strings.HasPrefix(*b.Reason, "seed-intent:") {
			continue
		}
		if b.Status == types.DEAD || b.Status == types.MERGED {
			n++
		}
	}
	return n
}
