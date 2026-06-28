package subconscious

import (
	"testing"

	"github.com/berttrycoding/thought-harness/internal/cognition"
	"github.com/berttrycoding/thought-harness/internal/events"
)

// TestFormulateQuery is the COGNITION table over the pure T1.1 transform (subconscious.query_formulation,
// FLARE arXiv:2305.06983): a web_search query is formulated from the ACTUAL question by stripping a leading
// instruction/wrapper clause, while a content colon (a ratio, a URL, a time) and a no-wrapper goal are left
// INTACT. This is the thinking the flag intends — "search the question, not the wrapper prose" — not just
// the plumbing.
func TestFormulateQuery(t *testing.T) {
	cases := []struct {
		name string
		goal string
		want string
	}{
		// --- wrapper-strip: the MEASURED bench shape (answer/question framings ONLY) ---
		{"multi_hop_wrapper", "Answer this multi-hop question: Who founded the company that makes X?", "Who founded the company that makes X?"},
		{"bare_question_label", "Question: When was the bridge built?", "When was the bridge built?"},
		{"answer_the_following", "Answer the following: how many moons does Mars have?", "how many moons does Mars have?"},
		{"answer_the_question", "Answer the question: who wrote the Iliad?", "who wrote the Iliad?"},
		{"please_answer", "Please answer: what is the boiling point of mercury?", "what is the boiling point of mercury?"},
		{"leading_whitespace", "   Answer this question:   Who painted the ceiling?  ", "Who painted the ceiling?"},
		{"keeps_full_question_not_keyword", "Question: Who was the second person to walk on the moon and in what year?",
			"Who was the second person to walk on the moon and in what year?"},

		// --- no false strip: a CONTENT colon must NOT be a wrapper boundary (the colon-followed-by-space guard) ---
		{"content_ratio", "What is the ratio 3:4 used for?", "What is the ratio 3:4 used for?"},
		{"content_time", "What happened at 3:00 in the film?", "What happened at 3:00 in the film?"},
		{"content_url", "Summarize https://example.com/page for me", "Summarize https://example.com/page for me"},
		{"no_wrapper_plain_question", "Who founded the company that makes X?", "Who founded the company that makes X?"},
		{"questionnaire_not_question", "questionnaire results: explain the trend", "questionnaire results: explain the trend"},

		// --- red-team T1.1 regression: a bare IMPERATIVE VERB is NOT a wrapper (stripping would amputate the
		//     SUBJECT / cut inside a URL). These were FALSE strips in the first build; now left verbatim. ---
		{"verb_subject_find", "Find Waldo: where is he hidden in the picture", "Find Waldo: where is he hidden in the picture"},
		{"verb_subject_investigate", "Investigate Aquaman: the 2018 film box office", "Investigate Aquaman: the 2018 film box office"},
		{"verb_url_search", "Search the latest news at https://news.example.com: the merger", "Search the latest news at https://news.example.com: the merger"},
		{"bare_find_label", "Find: the capital of the country east of Spain", "Find: the capital of the country east of Spain"},
		{"bare_look_up_label", "Look up: who wrote the Iliad", "Look up: who wrote the Iliad"},
		{"bare_research_label", "Research: the boiling point of mercury", "Research: the boiling point of mercury"},

		// --- guards: never empty, leading colon, trailing-only wrapper ---
		{"strip_would_be_empty_keeps_raw", "Question:", "Question:"},
		{"wrapper_empty_after_strip", "Question:   ", "Question:"},
		{"leading_colon_no_strip", ": orphan colon goal", ": orphan colon goal"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formulateQuery(tc.goal); got != tc.want {
				t.Errorf("formulateQuery(%q) = %q, want %q", tc.goal, got, tc.want)
			}
		})
	}
}

// TestFormulateQueryDeterministic pins the Pattern-A determinism contract: the same input yields the same
// output across repeated calls (no clock, no RNG, no model) — so the goldens reproduce.
func TestFormulateQueryDeterministic(t *testing.T) {
	goal := "Answer this multi-hop question: What river flows through the capital of France?"
	first := formulateQuery(goal)
	for i := 0; i < 8; i++ {
		if got := formulateQuery(goal); got != first {
			t.Fatalf("formulateQuery not deterministic: call %d = %q, first = %q", i, got, first)
		}
	}
	if first != "What river flows through the capital of France?" {
		t.Fatalf("unexpected formulation: %q", first)
	}
}

// TestQueryFormulationWiredOnReformulates is the WIRE test for the flag-ON path: a web_search-scoped
// sub-agent with the query-formulation gate set strips the instruction wrapper from the goal before it
// issues the web_search call — proving WithQueryFormulation actually changes the query the floor dispatches,
// and that the reformulation emits subconscious.query_formulate (it is visible in the trace).
func TestQueryFormulationWiredOnReformulates(t *testing.T) {
	goal := "Answer this multi-hop question: Who founded the company that makes the Model S?"
	want := "Who founded the company that makes the Model S?"
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "web_search"}

	var captured []events.Event
	emit := func(kind, summary string, data events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: data}
		captured = append(captured, ev)
		return ev
	}
	sa := NewSubAgent(spec, "general", goal, nil, emit, "sa:expose-affordances", scope, webExecutor(t), nil).
		WithQueryFormulation(true)

	call, ok := sa.floorToolCall()
	if !ok || call.Name != "web_search" {
		t.Fatalf("floor did not pick web_search (ok=%v, name=%q)", ok, call.Name)
	}
	if q, _ := call.Args["query"].(string); q != want {
		t.Fatalf("formulated query = %q, want the stripped question %q", q, want)
	}

	// The reformulation must be VISIBLE on the bus (subconscious.query_formulate), carrying both the raw goal
	// and the formulated query.
	var found *events.Event
	for i := range captured {
		if captured[i].Kind == events.SubQueryFormulate {
			found = &captured[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no subconscious.query_formulate event emitted for a reformulated query")
	}
	if got, _ := found.Data["query"].(string); got != want {
		t.Fatalf("event query = %q, want %q", got, want)
	}
	if got, _ := found.Data["goal"].(string); got != goal {
		t.Fatalf("event goal = %q, want the raw goal %q", got, goal)
	}
}

// TestQueryFormulationOffByteIdentical is the default-OFF arm: WITHOUT the gate set (the default), the SAME
// wrapped goal is searched VERBATIM — the query equals strings.TrimSpace(goal) exactly as today — and NO
// subconscious.query_formulate event is emitted. This proves the flag-OFF pipeline is byte-identical.
func TestQueryFormulationOffByteIdentical(t *testing.T) {
	goal := "Answer this multi-hop question: Who founded the company that makes the Model S?"
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "web_search"}

	var captured []events.Event
	emit := func(kind, summary string, data events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: data}
		captured = append(captured, ev)
		return ev
	}
	// No .WithQueryFormulation ⇒ the gate is off (the default).
	sa := NewSubAgent(spec, "general", goal, nil, emit, "sa:expose-affordances", scope, webExecutor(t), nil)

	call, ok := sa.floorToolCall()
	if !ok || call.Name != "web_search" {
		t.Fatalf("floor did not pick web_search (ok=%v, name=%q)", ok, call.Name)
	}
	if q, _ := call.Args["query"].(string); q != goal { // goal has no surrounding whitespace ⇒ TrimSpace == goal
		t.Fatalf("OFF query = %q, want the whole goal verbatim %q (default must be byte-identical)", q, goal)
	}
	for _, ev := range captured {
		if ev.Kind == events.SubQueryFormulate {
			t.Fatal("subconscious.query_formulate emitted with the flag OFF — the default path is not byte-identical")
		}
	}
}

// TestQueryFormulationOnNoOpStripIsSilent guards the event discipline: when the gate is ON but the goal has
// NO wrapper to strip (the formulated query equals the raw goal), NO event is emitted — only an actual
// reformulation is visible, so an ineffective pass stays silent.
func TestQueryFormulationOnNoOpStripIsSilent(t *testing.T) {
	goal := "Who founded the company that makes the Model S?" // no instruction wrapper
	spec := cognition.OperatorSpec{Name: "expose-affordances", Family: "relational", Intent: "reveal affordances"}
	scope := []string{"search", "read_file", "web_search"}

	var captured []events.Event
	emit := func(kind, summary string, data events.D) events.Event {
		ev := events.Event{Kind: events.Kind(kind), Summary: summary, Data: data}
		captured = append(captured, ev)
		return ev
	}
	sa := NewSubAgent(spec, "general", goal, nil, emit, "sa:expose-affordances", scope, webExecutor(t), nil).
		WithQueryFormulation(true)

	call, ok := sa.floorToolCall()
	if !ok || call.Name != "web_search" {
		t.Fatalf("floor did not pick web_search (ok=%v, name=%q)", ok, call.Name)
	}
	if q, _ := call.Args["query"].(string); q != goal {
		t.Fatalf("no-wrapper goal must pass through unchanged; query = %q, want %q", q, goal)
	}
	for _, ev := range captured {
		if ev.Kind == events.SubQueryFormulate {
			t.Fatal("subconscious.query_formulate emitted for a no-op reformulation (should be silent)")
		}
	}
}
