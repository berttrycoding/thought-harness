package cognition

// loader_test.go — the G1 SESSION-RECORD loader's PROPERTY tests (the THINKING the analysis surface
// must read back, not just that the loader parses). Each test asserts a cognition claim the
// benchmarking workbench depends on (mock §0/§1/§2): the loader reconstructs the OUTCOME verdict from
// the recorded trajectory, the IMPULSE latency off the user stimulus, the DECISION fingerprint, the
// grounded REWARD ledger, the STIMULUS index, and the per-tick scrub series from the SignalFrame
// sidecar — and the power-ON-beats-OFF property: a session that ACTed + grounded reads SOLVED while
// one that gave up without grounding reads UNSOLVED. Pure: deterministic event/frame fixtures, no
// engine, no model, no clock.

import (
	"strings"
	"testing"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// ev is a terse event-fixture builder (tick/kind/data → events.Event), so a recorded stream reads
// like the JSONL it stands in for.
func ev(tick int, kind string, data map[string]any) events.Event {
	layer := kind
	if i := strings.IndexByte(kind, '.'); i >= 0 {
		layer = kind[:i]
	}
	if data == nil {
		data = map[string]any{}
	}
	return events.Event{Tick: tick, Kind: kind, Layer: layer, Data: data}
}

// onSession is the recorded stream of a power-ON run that SOLVED: a user turn at t1, the engine fires
// + injects, ACTs to import reality (grounded), reality refutes a wrong belief (a healthy revert),
// then STOPs GOAL_MET and DELIVERs. This is the trajectory the SESSION/COMPARE panels score on.
func onSession() []events.Event {
	return []events.Event{
		ev(1, events.Port, map[string]any{"source": "USER_INPUT", "text": "is this refactor safe to ship?"}),
		ev(2, events.SubFire, nil),
		ev(3, events.Inject, nil),
		ev(4, events.Decision, map[string]any{"decision": "THINK", "reason": "open the line"}),
		ev(6, events.Decision, map[string]any{"decision": "ACT", "reason": "needs ground truth the loop can't manufacture"}),
		ev(7, events.ActionTool, map[string]any{"tool": "run"}),
		ev(8, events.Ground, map[string]any{"verdict": "grounded", "claim": "suite 12/12 pass"}),
		ev(8, events.Value, map[string]any{"reward": 1.0}),
		ev(9, events.Ground, map[string]any{"verdict": "refuted", "claim": "cache is cold"}),
		ev(9, events.Value, map[string]any{"reward": -1.0}),
		ev(10, events.Decision, map[string]any{"decision": "STOP", "stop_kind": "GOAL_MET", "reason": "suite passed"}),
		ev(11, events.Respond, map[string]any{"kind": "respond", "goal": "ship?"}),
	}
}

// offSession is the recorded stream of a power-OFF run that gave up: a user turn, the engine keeps
// THINKing without ever ACTing/grounding, then STOPs GIVE_UP and never delivers. The benchmark must
// read this as UNSOLVED with zero grounding — the divergence from onSession.
func offSession() []events.Event {
	return []events.Event{
		ev(1, events.Port, map[string]any{"source": "USER_INPUT", "text": "is this refactor safe to ship?"}),
		ev(2, events.SubFire, nil),
		ev(3, events.Inject, nil),
		ev(4, events.Decision, map[string]any{"decision": "THINK", "reason": "keep reasoning — no ground truth"}),
		ev(6, events.Decision, map[string]any{"decision": "BACKTRACK", "reason": "branch dry"}),
		ev(9, events.Decision, map[string]any{"decision": "STOP", "stop_kind": "GIVE_UP", "reason": "exhausted the lines"}),
	}
}

// TestLoaderReconstructsSolvedOutcome — the SESSION verdict the workbench scores on is READ from the
// recorded trajectory, not re-judged: a STOP/GOAL_MET + a delivery is SOLVED.
func TestLoaderReconstructsSolvedOutcome(t *testing.T) {
	rec := RecordFromFrozen(onSession(), nil)
	if rec.SolveVerdict != "SOLVED" {
		t.Errorf("a STOP/GOAL_MET + delivery must read SOLVED; got %q", rec.SolveVerdict)
	}
	if rec.Delivered != 1 {
		t.Errorf("one Respond must count as 1 delivery; got %d", rec.Delivered)
	}
	if rec.Grounded != 1 || rec.Refuted != 1 {
		t.Errorf("reality ledger wrong: grounded=%d refuted=%d (want 1,1)", rec.Grounded, rec.Refuted)
	}
}

// TestLoaderReconstructsGaveUpOutcome — the power-OFF trajectory reads UNSOLVED with no grounding: the
// divergence the COMPARE diff (the headline benchmark) is built to surface.
func TestLoaderReconstructsGaveUpOutcome(t *testing.T) {
	rec := RecordFromFrozen(offSession(), nil)
	if rec.SolveVerdict != "UNSOLVED" {
		t.Errorf("a STOP/GIVE_UP with no delivery must read UNSOLVED; got %q", rec.SolveVerdict)
	}
	if rec.Grounded != 0 {
		t.Errorf("the OFF run never grounded; got grounded=%d", rec.Grounded)
	}
	if rec.Delivered != 0 {
		t.Errorf("the OFF run never delivered; got delivered=%d", rec.Delivered)
	}
}

// TestLoaderCapturesImpulseLatency — the responsiveness headline (mock §1) is the stimulus→milestone
// offsets. The loader must hang them off the user stimulus tick and measure each in ticks-after.
func TestLoaderCapturesImpulseLatency(t *testing.T) {
	rec := RecordFromFrozen(onSession(), nil)
	if rec.ImpStimulusTick != 1 {
		t.Fatalf("impulse origin must be the user stimulus tick (1); got %d", rec.ImpStimulusTick)
	}
	if !strings.Contains(rec.ImpStimulusText, "safe to ship") {
		t.Errorf("impulse text must carry the user's question; got %q", rec.ImpStimulusText)
	}
	// fire @t2, inject @t3, the reality-reach (ActionTool) @t7, deliver @t11 — offsets from t1. The
	// "act" milestone is the moment it reached out to REALITY (the action event), not the ACT decision.
	if rec.ImpToFire != 1 {
		t.Errorf("ImpToFire: SubFire @t2 is +1 from t1; got %d", rec.ImpToFire)
	}
	if rec.ImpToInject != 2 {
		t.Errorf("ImpToInject: Inject @t3 is +2 from t1; got %d", rec.ImpToInject)
	}
	if rec.ImpToAct != 6 {
		t.Errorf("ImpToAct: the reality-reach (ActionTool) @t7 is +6 from t1; got %d", rec.ImpToAct)
	}
	if rec.ImpToDeliver != 10 {
		t.Errorf("ImpToDeliver: Respond @t11 is +10 from t1 (the latency headline); got %d", rec.ImpToDeliver)
	}
}

// TestLoaderReconstructsDecisionFingerprint — the LOOP·CONTROLLER decision history (mock §3) is the
// move sequence in tick order; the fingerprint summary is what the COMPARE diff fingerprints on.
func TestLoaderReconstructsDecisionFingerprint(t *testing.T) {
	rec := RecordFromFrozen(onSession(), nil)
	if len(rec.Decisions) != 3 {
		t.Fatalf("expected 3 decisions (THINK/ACT/STOP); got %d", len(rec.Decisions))
	}
	wantMoves := []string{"THINK", "ACT", "STOP"}
	for i, d := range rec.Decisions {
		if d.Move != wantMoves[i] {
			t.Errorf("decision %d: move=%q want %q", i, d.Move, wantMoves[i])
		}
	}
	fp := moveCounts(rec.Decisions)
	if !strings.Contains(fp, "ACT×1") || !strings.Contains(fp, "GOAL_MET") {
		t.Errorf("fingerprint must show the ACT + the GOAL_MET stop; got %q", fp)
	}
}

// TestLoaderRewardLedgerIsGrounded — the reward ledger (mock §4: "only from reality, never
// self-graded") must carry BOTH the +1 grounded reward and the −1 revert, and count the revert.
func TestLoaderRewardLedgerIsGrounded(t *testing.T) {
	rec := RecordFromFrozen(onSession(), nil)
	if len(rec.Rewards) != 2 {
		t.Fatalf("expected 2 grounded rewards (+1, −1); got %d", len(rec.Rewards))
	}
	if rec.Reverts != 1 {
		t.Errorf("the −1 reward is a revert (reality corrected a belief); reverts=%d want 1", rec.Reverts)
	}
}

// TestLoaderStimulusIndex — the scrub axis's `{`/`}` jumps key off the stimulus index: the user turn
// and each reality arrival must be marked, with the right kind tag.
func TestLoaderStimulusIndex(t *testing.T) {
	rec := RecordFromFrozen(onSession(), nil)
	var users, reals int
	for _, s := range rec.Stimuli {
		switch s.Kind {
		case "user":
			users++
		case "reality":
			reals++
		}
	}
	if users != 1 {
		t.Errorf("one user stimulus expected; got %d", users)
	}
	if reals != 2 { // the two grounding arrivals (grounded + refuted)
		t.Errorf("two reality stimuli expected (the grounding arrivals); got %d", reals)
	}
}

// TestLoaderScrubSeriesFromSidecar — the per-tick vital series (the SESSION/COMPARE charts) come from
// the SignalFrame sidecar. The loader must lay one column per frame and map the level/normalised
// reads correctly (n/U/θ pass through 0..1; reserve 0..100 → 0..1; condition → severity intensity).
func TestLoaderScrubSeriesFromSidecar(t *testing.T) {
	frames := []SignalFrame{
		{Tick: 0, N: 0.05, U: 0.30, Theta: 0.50, VActive: 0.40, Reserve: 70, Condition: "NOMINAL"},
		{Tick: 1, N: 0.41, U: 0.96, Theta: 0.84, VActive: 0.71, Reserve: 4, Condition: "LOADED", ObservationsInWindow: 1},
		{Tick: 2, N: 1.05, U: 0.50, Theta: 0.60, VActive: 0.30, Reserve: 50, Condition: "DEGRADED", ObservationsInWindow: 1},
	}
	rec := RecordFromFrozen(onSession(), frames)
	if rec.Ticks != 3 {
		t.Fatalf("the scrub axis width is the frame count (3); got %d", rec.Ticks)
	}
	if got := rec.N[1]; got != 0.41 {
		t.Errorf("n series passes through 0..1; N[1]=%v want 0.41", got)
	}
	if got := rec.N[2]; got != 1.0 { // 1.05 clamps to the 1.0 runaway line
		t.Errorf("n>1 clamps to the runaway line; N[2]=%v want 1.0", got)
	}
	if got := rec.Reserve[0]; got != 0.70 {
		t.Errorf("reserve 70/100 → 0.70; Reserve[0]=%v", got)
	}
	// condition intensity: NOMINAL low, DEGRADED the alarm-high reading.
	if rec.Condition[0] >= rec.Condition[2] {
		t.Errorf("DEGRADED must read higher intensity than NOMINAL; got NOMINAL=%v DEGRADED=%v", rec.Condition[0], rec.Condition[2])
	}
	// grounding hit lands on the tick the window count first grew (t1), not the steady tick.
	if !rec.GroundHits[1] {
		t.Errorf("a fresh reality import (window grew at t1) must mark a grounding hit")
	}
	if rec.GroundHits[2] {
		t.Errorf("t2's window count did not grow (steady at 1) — no fresh import, no hit")
	}
}

// TestLoaderMissingSidecarStillScrubsEvents — a record with NO SignalFrame sidecar (the live frozen
// path runs no G0 recorder) still reconstructs the full cognition from the events: the verdict,
// stimuli, decisions, and impulse all survive; only the per-tick vital CHARTS are absent.
func TestLoaderMissingSidecarStillScrubsEvents(t *testing.T) {
	rec := RecordFromFrozen(onSession(), nil)
	if rec.Ticks != 0 {
		t.Errorf("no sidecar ⇒ no scrub series; Ticks=%d want 0", rec.Ticks)
	}
	if rec.SolveVerdict != "SOLVED" || len(rec.Decisions) == 0 || rec.ImpToDeliver == 0 {
		t.Errorf("the cognition (verdict/decisions/impulse) must survive a missing sidecar; got verdict=%q decisions=%d deliver=%d",
			rec.SolveVerdict, len(rec.Decisions), rec.ImpToDeliver)
	}
}

// TestSidecarPath — the loader finds the sidecar by the same rule cmd/thought's writer uses.
func TestSidecarPath(t *testing.T) {
	cases := map[string]string{
		"runs/x.jsonl":   "runs/x.signals.jsonl",
		"runs/x":         "runs/x.signals.jsonl",
		"/a/b/run.jsonl": "/a/b/run.signals.jsonl",
	}
	for in, want := range cases {
		if got := SidecarPath(in); got != want {
			t.Errorf("SidecarPath(%q)=%q want %q", in, got, want)
		}
	}
}

// TestDecodeRoundTrip — the on-disk readers decode the wire formats the harness writes (the
// trace.JsonlSink event shape + the *.signals.jsonl frame schema), tolerating a truncated tail.
func TestDecodeRoundTrip(t *testing.T) {
	eventJSONL := `{"tick":1,"kind":"port","layer":"port","summary":"received","data":{"source":"USER_INPUT","text":"hi"}}
{"tick":2,"kind":"critic.decision","layer":"critic","summary":"STOP","data":{"decision":"STOP","stop_kind":"GOAL_MET"}}
{"tick":2,"kind":"action.respond","layer":"action","summary":"answer","data":{}}
{truncated mid-write`
	evs, err := decodeEventLog(strings.NewReader(eventJSONL))
	if err != nil {
		t.Fatalf("decodeEventLog: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("the truncated tail line is skipped, the 3 good lines kept; got %d", len(evs))
	}
	sidecar := `{"schema":1,"tick":1,"n":0.4,"u":0.9,"reserve":10,"condition":"LOADED"}
{bad line}
{"schema":1,"tick":2,"n":0.2,"u":0.3,"reserve":70,"condition":"NOMINAL"}`
	frames := decodeSignalSidecar(strings.NewReader(sidecar))
	if len(frames) != 2 {
		t.Fatalf("the bad sidecar line is skipped, the 2 good kept; got %d", len(frames))
	}
	if frames[0].N != 0.4 || frames[0].Condition != "LOADED" {
		t.Errorf("frame 0 decode wrong: %+v", frames[0])
	}
}
