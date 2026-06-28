package engine

// shutdown.go: cooperative engine shutdown for the CLI/TUI edge (proposal
// docs/cognition/2026-06-20-cognitive-power-cycle-and-grounded-sensing.md §2.3,
// red-team amendment 6).
//
// The engine stays headless-pure — it does NO signal handling. The edge (cmd/thought,
// the TUI bridge) installs the OS signal handler and calls RequestStop, which makes the
// Run loop break at the next tick boundary so the edge can FlushState() (persist learned
// state + the resume cursor) before the process exits. Because Run RETURNS before the
// edge flushes, the flush never races the loop.

// RequestStop asks the engine's Run loop to stop at the next tick boundary. Safe to call
// from another goroutine (e.g. a signal-handler goroutine) — it sets an atomic flag the
// loop reads each iteration.
func (e *Engine) RequestStop() { e.stopReq.Store(true) }
