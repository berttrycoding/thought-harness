package zoommem

import "testing"

// budget is deliberately tight: well above the largest single thought (so the focus always fits at
// full) but far below the sum of all thoughts (so compression MUST engage). This is the regime the
// real system lives in.
const budget = 120

// TestZoomMem_Test1 is Test 1 from the worklog: does Zoomable Memory hold a context that stays WITHIN
// budget and stays COHERENT, across a whole growing session? Each property has a KILL line.
func TestZoomMem_Test1_BudgetAndCoherence(t *testing.T) {
	session := CookDinnerSession()
	maxShelved := 0

	// drive the session one thought at a time; focus = the newest thought.
	for n := 1; n <= len(session); n++ {
		units := session[:n]
		focusID := units[n-1].ID
		ctx := Assemble(units, focusID, budget)

		// (1) BUDGET — never overflow (the schedulability U<=1 analog).
		if ctx.Total > budget {
			t.Fatalf("n=%d OVERFLOW: total=%d > budget=%d\n%s", n, ctx.Total, budget, Render(ctx))
		}
		// (2) FOCUS SHARP — the thing we are thinking about is never faded below the committed thought.
		if fl := levelOf(ctx, focusID); fl > L1Thought {
			t.Fatalf("n=%d INCOHERENT: focus #%d faded to %s\n%s", n, focusID, fl, Render(ctx))
		}
		// (3) NO SILENT LOSS — every thought is represented somewhere (shown, or a shelved pointer).
		if got := len(ctx.Shown) + len(ctx.Shelved); got != n {
			t.Fatalf("n=%d LOST A THOUGHT: represented %d != %d units", n, got, n)
		}
		if len(ctx.Shelved) > maxShelved {
			maxShelved = len(ctx.Shelved)
		}
	}

	// (4) the test must be non-trivial: compression actually had to engage at some point.
	if maxShelved == 0 {
		t.Fatal("compression never engaged — budget too loose; the test would prove nothing")
	}

	// (5) COHERENCE/RELEVANCE — focus on the shopping list (#16: salmon/lemon/butter). The early recipe
	// thought #3 (salmon/lemon/butter) is OLD and on another branch, but relevant; the aside #15
	// (rain/hike) is RECENT but irrelevant. The relevant-old must be surfaced, and must NEVER lose a
	// detail slot to the irrelevant-recent. Proven across the whole budget range (not a knife-edge):
	//   - invariant: at every budget, #3 is never more faded than #15 (relevance >= recency).
	//   - pressure:  there is a budget where #3 is still shown while #15 has been shelved.
	ctx := Assemble(session, 16, budget)
	if l3 := levelOf(ctx, 3); l3 > L2OneLiner {
		t.Fatalf("RELEVANCE FAIL: relevant old thought #3 not surfaced (level=%s)\n%s", l3, Render(ctx))
	}
	sawPressure := false
	for b := budget; b >= 50; b-- {
		c := Assemble(session, 16, b)
		s3, s15 := levelOf(c, 3), levelOf(c, 15)
		if s3 > s15 {
			t.Fatalf("budget=%d: irrelevant-recent #15 (%s) beat relevant-old #3 (%s)", b, s15, s3)
		}
		if s3 <= L2OneLiner && s15 == L4Pointer {
			sawPressure = true
		}
	}
	if !sawPressure {
		t.Fatal("RELEVANCE FAIL: never saw the pressure case (relevant-old kept while irrelevant-recent shelved)")
	}

	// eyeball snapshots (go test -v).
	t.Log("\n--- early: n=6, focus #5 (recipe branch) ---\n" + Render(Assemble(session[:6], 5, budget)))
	t.Log("\n--- full: n=17, focus #16 (shopping list) ---\n" + Render(ctx))
	t.Logf("max shelved during session: %d/%d thoughts", maxShelved, len(session))
}

func levelOf(ctx Context, id int) Level {
	for _, s := range ctx.Shown {
		if s.Unit.ID == id {
			return s.Level
		}
	}
	return L4Pointer
}
