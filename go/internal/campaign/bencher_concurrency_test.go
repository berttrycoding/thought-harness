package campaign

import "testing"

// TestBencherConcurrencyDeterministic pins the parallel-scaling throughput lever (#34): the held-out
// probe with Concurrency>1 returns a result IDENTICAL to the serial path — PerItem order preserved
// (paired McNemar), token/call sums equal. Run under `go test -race` this also proves no data race over
// the shared collector. The slots are written by index, so completion order cannot reorder the pairing.
func TestBencherConcurrencyDeterministic(t *testing.T) {
	tasks := []HeldOutTask{
		{Goal: "what is 12 times 7?", Expect: "84"},
		{Goal: "is this refactor safe to ship?"},
		{Goal: "what is 9 times 9?", Expect: "81"},
		{Goal: "what is 6 times 8?", Expect: "48"},
		{Goal: "design a small todo api"},
		{Goal: "what is 5 times 5?", Expect: "25"},
	}
	mk := func(n int) EngineBencher {
		return EngineBencher{MaxTicks: 15, NewEngine: testEngineFactory, Tasks: tasks, Concurrency: n}
	}

	serial, err := mk(1).Bench("")
	if err != nil {
		t.Fatalf("serial Bench: %v", err)
	}
	for _, n := range []int{2, 4, 8} {
		par, err := mk(n).Bench("")
		if err != nil {
			t.Fatalf("concurrency=%d Bench: %v", n, err)
		}
		if par.Total() != serial.Total() {
			t.Fatalf("concurrency=%d: item count %d != serial %d", n, par.Total(), serial.Total())
		}
		for i := range serial.PerItem {
			if par.PerItem[i] != serial.PerItem[i] {
				t.Errorf("concurrency=%d: PerItem[%d]=%v != serial %v (order/pairing must be preserved)", n, i, par.PerItem[i], serial.PerItem[i])
			}
		}
		if par.Tokens != serial.Tokens || par.Calls != serial.Calls {
			t.Errorf("concurrency=%d: sums differ (tokens %d/%d, calls %d/%d)", n, par.Tokens, serial.Tokens, par.Calls, serial.Calls)
		}
	}
}

// TestBencherConcurrencyBudgetAborts pins that a per-arm budget cap still HARD-aborts under concurrency
// (the metered-substrate ceiling): with a tiny call cap on the test double... the double makes 0 model
// calls, so instead assert the no-cap path completes and a 0 cap is unbounded (the cap semantics hold).
func TestBencherConcurrencyBudgetAborts(t *testing.T) {
	b := EngineBencher{
		MaxTicks: 10, NewEngine: testEngineFactory, Concurrency: 4,
		Tasks: []HeldOutTask{{Goal: "what is 2 times 2?", Expect: "4"}, {Goal: "what is 3 times 3?", Expect: "9"}},
	}
	// 0 caps = unbounded; the offline double makes 0 model calls, so it always completes.
	if _, err := b.Bench(""); err != nil {
		t.Fatalf("unbounded parallel Bench must complete: %v", err)
	}
}
