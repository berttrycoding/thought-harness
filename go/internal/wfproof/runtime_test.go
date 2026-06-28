package wfproof

import "testing"

// TestRuntime_HeadlessSpawn proves our harness can RUN the swarm headless, in-process, spawning a
// session per Dispatch — no cc-spawn, no external process. swarm(1) -> 3 sprints -> each sprint spawns
// 4 chains = 1 + 3 + 12 = 16 sessions.
func TestRuntime_HeadlessSpawn(t *testing.T) {
	lib, root := LatheSwarm()
	r := NewRunner(lib)
	res := r.Run(root)

	if r.Err != "" {
		t.Fatalf("runtime error: %s", r.Err)
	}
	if r.Halted {
		t.Fatalf("unexpected halt: %s", r.HaltReason)
	}
	sessions := SessionCount(res)
	t.Logf("ran swarm headless: %d sessions spawned, peak fan-out %d (<= MaxParWidth %d)",
		r.Spawned, r.PeakFanout, MaxParWidth)
	t.Logf("hooks fired at runtime: inject=%d floor=%d block=%d",
		r.HooksFired[Inject], r.HooksFired[Floor], r.HooksFired[Block])
	if r.Spawned != 16 {
		t.Errorf("expected 16 sessions (1 swarm + 3 sprints + 12 chains), got %d", r.Spawned)
	}
	if sessions != r.Spawned {
		t.Errorf("result tree (%d) disagrees with spawn count (%d)", sessions, r.Spawned)
	}
	if r.PeakFanout > MaxParWidth {
		t.Errorf("peak fan-out %d exceeds the schedulability budget %d", r.PeakFanout, MaxParWidth)
	}
	// the run actually did work (skills + scripts executed) and gates actually fired.
	if r.HooksFired[Inject] == 0 || r.HooksFired[Floor] == 0 {
		t.Errorf("expected inject + floor hooks to fire during the run")
	}
}

// TestRuntime_SpawnDepthBudget proves the spawn-depth bound is enforced AT RUNTIME (not just at verify):
// with a budget of 1, the swarm can't reach its chains and halts. This is the runtime guarantee that a
// spawn tree terminates.
func TestRuntime_SpawnDepthBudget(t *testing.T) {
	lib, root := LatheSwarm()
	r := NewRunner(lib)
	r.MaxSpawnDepth = 1 // swarm(0) -> sprint(1) -> chain(2) > 1  => must halt
	r.Run(root)

	if r.Err == "" {
		t.Fatal("expected a spawn-depth-budget error at runtime")
	}
	t.Logf("spawn-depth budget enforced: %s", r.Err)
}

// TestRuntime_BlockHookHalts proves a HOOK gates at runtime: if the post-critic attendance check fails,
// the Block hook halts the session mid-flight (lathe's exit-2 semantics), in our own loop.
func TestRuntime_BlockHookHalts(t *testing.T) {
	lib, root := LatheSwarm()
	r := NewRunner(lib)
	r.Failing = map[string]bool{"post-critic-attendance": true}
	r.Run(root)

	if !r.Halted {
		t.Fatal("expected the Block hook to halt the run when its check fails")
	}
	t.Logf("runtime gate fired: %s", r.HaltReason)
}

// TestRuntime_FloorHookSetsVerdict proves a Floor hook overrides the verdict at runtime with no agent
// say (the contract-check semantics): the chain comes back "floored", not "ok".
func TestRuntime_FloorHookSetsVerdict(t *testing.T) {
	lib := Library{
		"chain": {
			Name: "chain",
			Root: Seq{Children: []Node{
				skill("implement", "x"),
				skill("critic", "x", Guard{On: OnPre, Check: "contract-check", Action: Floor}),
			}},
		},
	}
	r := NewRunner(lib)
	r.Failing = map[string]bool{"contract-check": true}
	res := r.Run("chain")

	if res.Verdict != "floored:contract-check" {
		t.Fatalf("expected the Floor hook to set the verdict, got %q", res.Verdict)
	}
	t.Logf("contract-check floored the verdict at runtime: %s", res.Verdict)
}
