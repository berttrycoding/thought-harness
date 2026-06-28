package cognition

// deep_ledgers_test.go — the G4 DEEP ledgers + tree PROPERTY tests (the THINKING the §5/§7/§8/§9
// analysis tabs must read back, not just that they render). Each test asserts a cognition claim the
// per-subsystem detail view depends on (mock §5/§7/§8/§9): the thought tree reconstructs the
// parent-linked branch structure AND flags a dead branch WITH the reason it died; the action ledger
// distinguishes a HEALTHY revert (REFUTED) from a fabrication the safety check BLOCKED; the spawn tree
// carries each helper's bounded tool SCOPE; throughput reconstructs the real per-role/tier spend; and
// the self ledger records a keep-or-revert UNDID alongside the ADD (not just one direction). Pure:
// deterministic event fixtures, no engine, no model, no clock.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// deepSession is a recorded stream exercising all four G4 panels: the conscious stream forks two
// branches (one of which the engine BACKTRACKs off — a dead line with a reason) and folds/reopens a
// gist; the watched seam ACTs, gets a GROUNDED then a REFUTED (a healthy revert) and the safety check
// BLOCKs a fabrication; a phase worker is dispatched with a bounded tool scope; the model spends tokens
// across roles on two tiers; and a skill is minted (kept) while a specialist is demoted (undone). This
// is the trajectory the four deep panels reconstruct.
func deepSession() []events.Event {
	return []events.Event{
		ev(1, events.Port, map[string]any{"source": "USER_INPUT", "text": "is this refactor safe to ship?"}),
		// §5 the thought tree: b2 forks off the root b0, then b9 forks off b2.
		ev(3, events.MCP, map[string]any{"op": "branch", "branch": 2, "reason": "where does value pull hardest"}),
		ev(5, events.MCP, map[string]any{"op": "branch", "branch": 9, "reason": "assume the cache is cold"}),
		// the engine BACKTRACKs off b9 — a dead line that died for a REASON (the §5 read).
		ev(7, events.Decision, map[string]any{"decision": "BACKTRACK", "reason": "this branch stopped paying out — dry line"}),
		// §5 compression: fold b2 to a gist, then reopen it.
		ev(8, events.MCP, map[string]any{"op": "compress", "branch": 2}),
		ev(12, events.MCP, map[string]any{"op": "expand", "branch": 2}),
		// §7 the watched-seam ledger: ACT -> GROUNDED, then a REFUTED (healthy revert), then a BLOCKED fab.
		ev(6, events.ActionTool, map[string]any{"tool": "run"}),
		ev(7, events.Ground, map[string]any{"verdict": "grounded", "claim": "suite 12/12 pass", "tier": "T1"}),
		ev(9, events.Ground, map[string]any{"verdict": "refuted", "claim": "cache is cold — reality: warm in prod"}),
		ev(11, events.ActionSafetyBlock, map[string]any{}),
		// §7 the spawn tree: a phase worker dispatched, then its sub-agent role/scope.
		ev(4, events.SessionDispatch, map[string]any{"goal": "verify phase", "depth": 1.0, "phase": 1.0, "tokens": 600.0}),
		ev(4, events.SubSubagent, map[string]any{"role": "verifier", "domain": "GROUND", "tool_scope": []any{"read", "run"}}),
		ev(20, events.SessionTerminate, map[string]any{"reason": "goal_met", "nodes": 3.0, "depth": 2.0, "tokens": 1800.0}),
		// §8 throughput: model calls across roles on two tiers (sonnet = big, haiku = cheap).
		ev(3, events.LLM, map[string]any{"role": "generate", "model": "claude-sonnet", "completion_tokens": 800.0}),
		ev(5, events.LLM, map[string]any{"role": "generate", "model": "claude-sonnet", "completion_tokens": 400.0}),
		ev(8, events.LLM, map[string]any{"role": "transform", "model": "claude-sonnet", "completion_tokens": 300.0}),
		ev(10, events.LLM, map[string]any{"role": "summarize", "model": "claude-haiku", "completion_tokens": 120.0}),
		ev(14, events.LLM, map[string]any{"role": "summarize", "model": "claude-haiku", "completion_tokens": 90.0}),
		// §9 self-evolution: a baseline loaded, a skill minted (kept), a specialist demoted (undone).
		ev(0, events.PersistLoad, map[string]any{"skills": 4.0}),
		ev(15, events.SkillMint, map[string]any{"kind": "skill", "name": "verify", "count": 3.0}),
		ev(16, events.Convert, map[string]any{"kind": "demote", "domain": "simulation"}),
		ev(18, events.RegistryBatch, map[string]any{"action": "apply", "scope": "registry", "safety_mode": "safe"}),
		ev(20, events.Decision, map[string]any{"decision": "STOP", "stop_kind": "GOAL_MET", "reason": "done"}),
		ev(20, events.Respond, nil),
	}
}

// findBranch returns the reconstructed BranchNode with id, or nil.
func findBranch(rec AnalysisRecord, id int) *BranchNode {
	for i := range rec.Branches {
		if rec.Branches[i].ID == id {
			return &rec.Branches[i]
		}
	}
	return nil
}

// TestTreeReconstructsParentLinkAndDeadReason — the §5 read "did dead branches die for a REASON?": the
// tree must be parent-linked (a fork records the branch it forked from — not a flat list) AND a branch
// the engine abandoned must carry the reason it died. A reconstruction that lost the parent link (a
// flat tree) or dropped the death reason (a branch dying for no recorded reason = wasted effort the
// panel exists to flag) would NOT surface the cognition. THIS is the thinking the panel reads.
func TestTreeReconstructsParentLinkAndDeadReason(t *testing.T) {
	rec := RecordFromFrozen(deepSession(), nil)

	if len(rec.Branches) == 0 {
		t.Fatal("the thought tree reconstructed ZERO branches off a stream that forked two — not reading the conscious structure")
	}
	b2 := findBranch(rec, 2)
	b9 := findBranch(rec, 9)
	if b2 == nil || b9 == nil {
		t.Fatalf("missing reconstructed branches: b2=%v b9=%v", b2, b9)
	}
	// parent-linked, not flat: b9 forked off b2 (the active line at its fork tick).
	if b9.Parent != 2 {
		t.Errorf("the tree is not parent-linked: b9 forked while b2 was active, want Parent=2, got %d", b9.Parent)
	}
	// the abandoned branch (the one we BACKTRACKed off) must be DEAD and carry WHY it died.
	if b9.State != "DEAD" {
		t.Errorf("the back-tracked branch b9 was not marked DEAD; state=%q", b9.State)
	}
	if strings.TrimSpace(b9.DeadReason) == "" {
		t.Error("the dead branch b9 carries no death reason — a branch dying for no recorded reason is exactly the wasted-effort bug the §5 view exists to flag")
	}
	if b9.DeadTick != 7 {
		t.Errorf("the dead branch's death tick is wrong: want t7 (the BACKTRACK), got t%d", b9.DeadTick)
	}

	// the rendered CONSCIOUS body must surface the dead branch WITH its reason (the ✗ ... (reason) read).
	// (the reason text is clip-fit to one line, so assert on its head — the "(reason" parenthesis the row
	// renders — not the full string, which is by-design truncated for a one-line row.)
	body := renderConsciousBody(rec, rec.Ticks/2, 80)
	if !strings.Contains(body, "✗") {
		t.Error("the rendered tree did not mark the dead branch with ✗")
	}
	if !strings.Contains(body, "(this branch stopped") {
		t.Error("the rendered tree dropped the dead branch's reason — the 'died for a reason' read is missing")
	}
	// the compression history must reconstruct the fold + reopen (the lossy-graph memory management).
	if len(rec.Compression) < 2 {
		t.Errorf("the compression history lost the compress+expand pair; got %d events", len(rec.Compression))
	}
}

// TestActionLedgerDistinguishesRevertFromBlockedFabrication — the §7 read: a REFUTED is a HEALTHY
// revert (reality corrected a belief — GOOD), distinct from a BLOCKED fabrication (the safety alarm). A
// ledger that collapsed the two (or dropped one) would miss the cognition the watched-seam audit
// exists to surface: that reality pushed back AND that a made-up result was caught. The ACT must pair
// with the reality that came back.
func TestActionLedgerDistinguishesRevertFromBlockedFabrication(t *testing.T) {
	rec := RecordFromFrozen(deepSession(), nil)

	if len(rec.Actions) == 0 {
		t.Fatal("the action ledger is empty off a stream that ACTed, grounded, refuted, and blocked — not reading the watched seam")
	}
	var sawAct, sawGrounded, sawRefuted, sawBlocked bool
	for _, a := range rec.Actions {
		switch a.Kind {
		case "ACT":
			sawAct = true
		case "GROUNDED":
			sawGrounded = true
		case "REFUTED":
			sawRefuted = true
		case "BLOCKED":
			sawBlocked = true
		}
	}
	if !sawAct {
		t.Error("the ledger dropped the ACT (the intention out) — an ACT with no recorded reach is the unpaired half")
	}
	if !sawGrounded {
		t.Error("the ledger dropped the GROUNDED reality (reality confirmed)")
	}
	if !sawRefuted {
		t.Error("the ledger dropped the REFUTED reality — the HEALTHY revert (reality corrected a belief) is the cognition the §7 view exists to surface")
	}
	if !sawBlocked {
		t.Error("the ledger dropped the BLOCKED fabrication — the safety alarm is the bug-signature the §7 view watches for")
	}

	// the rendered body must read REFUTED and BLOCKED as DISTINCT lines (a revert is not a fabrication).
	body := renderActionSessBody(rec, 0, 80)
	if !strings.Contains(body, "REFUTED") {
		t.Error("the rendered action ledger did not surface the REFUTED healthy revert")
	}
	if !strings.Contains(body, "BLOCKED") {
		t.Error("the rendered action ledger did not surface the BLOCKED fabrication")
	}
}

// TestSpawnTreeCarriesBoundedToolScope — the §7 spawn-tree read: each helper is LIMITED to a tool scope
// (the watched-seam guarantee). The reconstruction must carry the worker's role AND the tools it was
// allowed — a spawn tree that lost the scope would hide whether a helper could use a tool it wasn't
// allowed (the bug the panel watches for). The depth bound (the durability-law fan-out) must come back.
func TestSpawnTreeCarriesBoundedToolScope(t *testing.T) {
	rec := RecordFromFrozen(deepSession(), nil)

	if len(rec.Workers) == 0 {
		t.Fatal("the spawn tree reconstructed ZERO workers off a stream that dispatched a phase worker — not reading the helper team")
	}
	var verifier *Worker
	for i := range rec.Workers {
		if rec.Workers[i].Role == "verifier" {
			verifier = &rec.Workers[i]
		}
	}
	if verifier == nil {
		t.Fatal("the dispatched 'verifier' worker never surfaced in the spawn tree")
	}
	if len(verifier.ToolScope) == 0 {
		t.Error("the verifier worker lost its tool scope — the 'limited to specific tools' watched-seam guarantee is exactly what the §7 spawn tree exists to surface")
	}
	// the whole-tree token spend + depth bound must come back from session.terminate.
	if rec.SpawnTokens != 1800 {
		t.Errorf("the spawn-tree token spend was not reconstructed; want 1800, got %d", rec.SpawnTokens)
	}
	if rec.SpawnDepth < 2 {
		t.Errorf("the spawn-tree depth bound was not reconstructed; want >=2, got %d", rec.SpawnDepth)
	}

	body := renderActionSessBody(rec, 0, 80)
	if !strings.Contains(body, "tools ") {
		t.Error("the rendered spawn tree did not surface a worker's bounded tool scope (tools ...)")
	}
}

// TestThroughputReconstructsRealRoleAndTierSpend — the §8 read "where did the budget go?": the per-role
// split must reflect the REAL recorded spend (generate is the hottest, hottest-first ordered), and the
// tier split must distinguish the cheap utility model from the big primary (the cost-saving routing
// read). A hollow/constant reconstruction would not track the real spend the metabolism panel exists
// to show.
func TestThroughputReconstructsRealRoleAndTierSpend(t *testing.T) {
	rec := RecordFromFrozen(deepSession(), nil)

	if len(rec.Roles) == 0 {
		t.Fatal("throughput reconstructed ZERO role spend off a stream with model calls — not reading the metabolism")
	}
	// generate spent the most (800+400=1200) — it must top the hottest-first ordering.
	if rec.Roles[0].Role != "generate" {
		t.Errorf("the role split is not hottest-first: want generate on top, got %q", rec.Roles[0].Role)
	}
	if rec.Roles[0].Tokens != 1200 {
		t.Errorf("the generate role spend was not summed from the real calls; want 1200, got %d", rec.Roles[0].Tokens)
	}
	// the tier split must distinguish the cheap (haiku, 2 calls) from the big (sonnet, 3 calls).
	if rec.TierCheap != 2 {
		t.Errorf("the cheap-tier call count is wrong (haiku calls); want 2, got %d", rec.TierCheap)
	}
	if rec.TierBig != 3 {
		t.Errorf("the big-tier call count is wrong (sonnet calls); want 3, got %d", rec.TierBig)
	}

	body := renderThroughputBody(rec, 0, 80)
	if !strings.Contains(body, "generate") {
		t.Error("the rendered throughput panel did not surface the hottest role")
	}
	if !strings.Contains(body, "cheap model") {
		t.Error("the rendered throughput panel did not surface the cost-saving tier split")
	}
}

// TestSelfLedgerRecordsBothKeepAndRevert — the §9 read: the self-change ledger must record a keep-or-
// revert UNDID alongside an ADD (not just one direction). A ledger of only ADDs with zero UNDIDs is the
// bug-signature the audit watches for (the keep-or-revert gate not actually catching the bad ones), so
// the reconstruction must surface BOTH a kept change and an undone one, each WITH its logged cause.
func TestSelfLedgerRecordsBothKeepAndRevert(t *testing.T) {
	rec := RecordFromFrozen(deepSession(), nil)

	if len(rec.SelfChanges) == 0 {
		t.Fatal("the self-change ledger is empty off a stream with a mint AND a demote — not reading the self-modification audit")
	}
	var added, undone *SelfChange
	for i := range rec.SelfChanges {
		switch rec.SelfChanges[i].Action {
		case "ADDED":
			added = &rec.SelfChanges[i]
		case "UNDID":
			undone = &rec.SelfChanges[i]
		}
	}
	if added == nil {
		t.Error("the self ledger dropped the ADDED change (the kept skill mint)")
	} else if strings.TrimSpace(added.Evidence) == "" {
		t.Error("the ADDED self-change carries no evidence (the cause) — an off-the-books change is the bug-signature the §9 audit watches for")
	}
	if undone == nil {
		t.Error("the self ledger dropped the UNDID change — a ledger of only ADDs with zero reverts is the bug-signature the keep-or-revert gate exists to expose")
	}
	// the baseline + scope must come back (the lineage anchor + the governance mode it ran under).
	if !rec.SelfBaseSet {
		t.Error("the self panel did not reconstruct the baseline-loaded anchor off persist.load")
	}
	if rec.SelfScope == "" {
		t.Error("the self panel did not reconstruct the governance scope off registry.batch")
	}

	body := renderSelfBody(rec, 0, 80)
	if !strings.Contains(body, "ADDED") || !strings.Contains(body, "UNDID") {
		t.Error("the rendered self ledger did not surface BOTH the kept (ADDED) and the reverted (UNDID) change")
	}
}

// TestDeepLedgersRenderGatedOff — the flag-OFF guarantee at the render seam: with deepLedgers=false, the
// four G4 tabs (CONSCIOUS / ACTION·SESS / THROUGHPUT / SELF) keep the "panel pending" placeholder and
// show NONE of the deep content, so a default-OFF surface is byte-identical to the G2/G3 state. With it
// ON, the deep panels render. The G3 REGISTRIES flag is held OFF here, so this isolates the G4 gate.
func TestDeepLedgersRenderGatedOff(t *testing.T) {
	rec := RecordFromFrozen(deepSession(), nil)

	for _, tab := range []string{"CONSCIOUS", "ACTION·SESS", "THROUGHPUT", "SELF"} {
		idx := -1
		for i, name := range analysisTabs {
			if name == tab {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("no %s tab in the analysis tab strip", tab)
		}

		off := RenderAnalysisTab(rec, rec, 0, false, idx, 80, false, false, false)
		if !strings.Contains(off, "panel pending") {
			t.Errorf("%s with deepLedgers=OFF did not keep the 'panel pending' placeholder — the flag-off surface is not byte-identical", tab)
		}

		on := RenderAnalysisTab(rec, rec, 0, false, idx, 80, false, true, false)
		if strings.Contains(on, "panel pending") {
			t.Errorf("%s with deepLedgers=ON still showed the 'panel pending' placeholder — the deep panel did not render", tab)
		}
	}
}
