// Package retrieval holds the one shared retrieval primitive — "given a query, find the most relevant
// stored items" — and the paraphrase-relevance benchmark that validates it (memory-stack §1).
//
// dataset.go is the P1.1 deliverable: a PARAPHRASE-relevance benchmark. Unlike the lexical zoommem
// dataset (where relevant units share words with the focus), here every relevant item is worded
// DIFFERENTLY from its query — synonyms, restructured phrasing, a different register — so word-overlap
// retrieval cannot find it. Each probe also carries lexical-TRAP distractors: items that share surface
// words with the query but are about something else. A purely lexical retriever is drawn to the traps
// and misses the paraphrases; this is the set that measures the ~66% lexical ceiling the zoommem report
// found, and the gate P1.2's hybrid retriever must beat. The fixture is in-code (no file I/O) so the
// benchmark is fully deterministic.
package retrieval

// Item is one stored memory unit: its text plus entity tags (the cues a retriever scores against).
type Item struct {
	ID       int
	Text     string
	Entities []string
}

// Probe is one retrieval query with its ground truth: the Relevant items (worded DIFFERENTLY from the
// query — paraphrases) that must surface, and the Distractor items (high word overlap, semantically
// irrelevant — lexical traps) that must not outrank them.
type Probe struct {
	Query      string
	Relevant   []int // item ids that SHOULD surface — paraphrased, low lexical overlap with the query
	Distractor []int // item ids that share WORDS with the query but are irrelevant (the lexical traps)
	Why        string
}

// Episode bundles a topic's items with its probes.
type Episode struct {
	Name   string
	Items  []Item
	Probes []Probe
}

// ParaphraseDataset returns the P1.1 paraphrase-relevance benchmark: 5 topical episodes, 30 probes.
// Every probe's relevant item is a paraphrase of the query (so lexical retrieval misses it) and most
// carry a lexical-trap distractor (so lexical retrieval is actively misled). Used by P1.2's R1
// A/B/C benchmark (lexical vs hybrid vs hybrid+rerank).
func ParaphraseDataset() []Episode {
	return []Episode{
		dbPerfEpisode(),
		kitchenEpisode(),
		travelEpisode(),
		fitnessEpisode(),
		financeEpisode(),
	}
}

// AllProbes flattens the dataset's probes (the benchmark iterates over these).
func AllProbes() []Probe {
	var out []Probe
	for _, ep := range ParaphraseDataset() {
		out = append(out, ep.Probes...)
	}
	return out
}

func dbPerfEpisode() Episode {
	return Episode{
		Name: "db-performance",
		Items: []Item{
			{0, "Once the orders table passed a few million rows, our SELECT statements started taking several seconds each.", []string{"orders", "select", "latency"}},
			{1, "Adding a B-tree index on the customer_id column cut the slow join down to a few milliseconds.", []string{"index", "join", "customer_id"}},
			{2, "We moved the nightly report off the primary by pointing it at a read replica, so analytics no longer competes with live traffic.", []string{"replica", "report", "primary"}},
			{3, "The connection pool kept getting exhausted under load because each request opened its own socket and never returned it.", []string{"connection pool", "load", "socket"}},
			{4, "Caching the rendered product page in Redis for sixty seconds removed almost all of the repeated database reads.", []string{"cache", "redis", "product page"}},
			{5, "The table tennis club grew so popular that the evening sessions ran several hours long.", []string{"table tennis", "club"}},
			{6, "He indexed his vinyl record collection by customer reviews, sorting each sleeve onto its own shelf.", []string{"vinyl", "collection", "shelf"}},
			{7, "Vacuuming the warehouse floor every night kept the dust off the primary loading dock.", []string{"warehouse", "primary", "dock"}},
		},
		Probes: []Probe{
			{"queries got slow after the data set grew large", []int{0}, []int{5}, "0 paraphrases 'rows/seconds'; 5 is a 'grew/table' lexical trap"},
			{"how we sped up a sluggish lookup that spanned two tables", []int{1}, nil, "1 paraphrases join/index without those query words"},
			{"keep the heavy analytics work from slowing down customer requests", []int{2}, []int{7}, "2 = replica offload; 7 shares 'primary' only"},
			{"the app ran out of available database handles when busy", []int{3}, nil, "3 paraphrases connection-pool exhaustion"},
			{"avoid hitting the store repeatedly for the same page", []int{4}, nil, "4 = Redis cache, no shared words"},
			{"cataloguing a music library onto shelves", []int{6}, []int{1}, "6 is the real match; 1 shares 'indexed/customer/reviews' as a trap"},
		},
	}
}

func kitchenEpisode() Episode {
	return Episode{
		Name: "kitchen",
		Items: []Item{
			{0, "Letting the steak rest for ten minutes after searing lets the juices settle back through the meat instead of spilling onto the board.", []string{"steak", "rest", "juices"}},
			{1, "If the sauce is too thin, stir in a slurry of cornstarch and water and it thickens as it comes back to a simmer.", []string{"sauce", "cornstarch", "thicken"}},
			{2, "Salting the eggplant and leaving it twenty minutes draws out the bitter moisture before frying.", []string{"eggplant", "salt", "bitter"}},
			{3, "A dull knife is actually more dangerous than a sharp one because it slips instead of biting in.", []string{"knife", "dull", "danger"}},
			{4, "Bring the butter and eggs to room temperature first or the cake batter will curdle when you cream them.", []string{"butter", "eggs", "batter"}},
			{5, "We let the toddlers rest after lunch so they wouldn't be cranky all afternoon.", []string{"toddlers", "rest", "lunch"}},
			{6, "The water in the reservoir was too thin a stream to thicken the river downstream.", []string{"reservoir", "stream", "river"}},
		},
		Probes: []Probe{
			{"why you should wait before slicing a cooked piece of beef", []int{0}, []int{5}, "0 paraphrases rest/steak; 5 is a 'rest' trap"},
			{"a trick to make a runny gravy heavier", []int{1}, []int{6}, "1 = cornstarch slurry; 6 shares 'thin/thicken' but is a river"},
			{"removing the harsh liquid from a vegetable before cooking", []int{2}, nil, "2 paraphrases salting eggplant"},
			{"a blunt blade causes more accidents than a keen one", []int{3}, nil, "3 = dull knife, restated"},
			{"keep dairy and eggs warm so the mixture doesn't split", []int{4}, nil, "4 paraphrases room-temp creaming"},
		},
	}
}

func travelEpisode() Episode {
	return Episode{
		Name: "travel",
		Items: []Item{
			{0, "Booking the flight on a Tuesday afternoon usually lands a noticeably cheaper fare than a weekend purchase.", []string{"flight", "fare", "tuesday"}},
			{1, "Rolling clothes instead of folding them frees up a surprising amount of room in the carry-on.", []string{"packing", "carry-on", "clothes"}},
			{2, "A layover under forty-five minutes is risky because a single delayed inbound leg makes you miss the next plane.", []string{"layover", "delay", "connection"}},
			{3, "Carry a power adapter for the local sockets or your charger simply won't fit the wall.", []string{"adapter", "socket", "charger"}},
			{4, "Tell your bank you're going abroad first, otherwise the card gets frozen on the first foreign transaction.", []string{"bank", "card", "abroad"}},
			{5, "The Tuesday market sells fares of fresh fish cheaper than the weekend stalls.", []string{"market", "fish", "tuesday"}},
			{6, "He rolled the heavy suitcase down the room's long carpet to the elevator.", []string{"suitcase", "room", "carpet"}},
		},
		Probes: []Probe{
			{"the cheapest day of the week to purchase airline tickets", []int{0}, []int{5}, "0 = Tuesday fare; 5 is a 'tuesday/fares' market trap"},
			{"fitting more into a small bag by changing how you store garments", []int{1}, []int{6}, "1 = rolling clothes; 6 shares 'rolled/room' trap"},
			{"why a tight gap between two planes is a gamble", []int{2}, nil, "2 paraphrases short layover risk"},
			{"making sure your devices can charge in another country", []int{3}, nil, "3 = power adapter, restated"},
			{"avoiding a declined card while overseas", []int{4}, nil, "4 paraphrases telling the bank"},
		},
	}
}

func fitnessEpisode() Episode {
	return Episode{
		Name: "fitness",
		Items: []Item{
			{0, "Taking a full rest day between heavy lifting sessions is when the muscle actually repairs and grows stronger.", []string{"rest day", "muscle", "recovery"}},
			{1, "Sipping water steadily through the day beats gulping a litre right before a run.", []string{"hydration", "water", "run"}},
			{2, "Easing off the pace for the last five minutes lets your heart rate come down gently instead of crashing.", []string{"cooldown", "heart rate", "pace"}},
			{3, "Swapping the elevator for the stairs is the easiest extra movement to fold into a desk job.", []string{"stairs", "movement", "desk"}},
			{4, "Eating some protein within an hour of training gives the body what it needs to rebuild.", []string{"protein", "training", "rebuild"}},
			{5, "The hotel gave every guest a rest day voucher for the muscle spa downstairs.", []string{"hotel", "rest day", "spa"}},
		},
		Probes: []Probe{
			{"why a break between workouts helps you get fitter", []int{0}, []int{5}, "0 = recovery day; 5 is a 'rest day/muscle' hotel trap"},
			{"a better way to stay watered than chugging a bottle pre-exercise", []int{1}, nil, "1 paraphrases steady hydration"},
			{"bringing your pulse down slowly at the end of a session", []int{2}, nil, "2 = cooldown, restated"},
			{"sneaking more activity into a sedentary office routine", []int{3}, nil, "3 paraphrases taking the stairs"},
			{"what to eat soon after exercising to aid repair", []int{4}, nil, "4 = post-workout protein"},
		},
	}
}

func financeEpisode() Episode {
	return Episode{
		Items: []Item{
			{0, "Setting aside roughly three to six months of expenses in a separate account is the cushion that stops a job loss from becoming a crisis.", []string{"emergency fund", "savings", "cushion"}},
			{1, "Paying off the card with the steepest interest first saves the most money over time, even if it isn't the smallest balance.", []string{"debt", "interest", "card"}},
			{2, "Having the contribution leave your paycheck automatically means you never feel tempted to skip a month's saving.", []string{"automatic", "paycheck", "saving"}},
			{3, "Spreading the money across many unrelated holdings means one bad bet can't sink the whole pot.", []string{"diversify", "holdings", "risk"}},
			{4, "Starting to invest in your twenties lets compounding do most of the heavy lifting by retirement.", []string{"compounding", "invest", "early"}},
			{5, "The emergency exit fund of the theatre held six rows of separate seats.", []string{"theatre", "exit", "rows"}},
		},
		Name: "finance",
		Probes: []Probe{
			{"how big a safety reserve to keep for a sudden loss of income", []int{0}, []int{5}, "0 = emergency fund; 5 is an 'emergency/fund/rows' theatre trap"},
			{"which loan to clear first to lose the least to fees", []int{1}, nil, "1 paraphrases highest-interest-first"},
			{"making yourself save without relying on willpower", []int{2}, nil, "2 = automatic contributions"},
			{"not putting all your eggs in one basket when investing", []int{3}, nil, "3 paraphrases diversification"},
			{"the advantage of beginning to put money away while young", []int{4}, nil, "4 = compounding early"},
		},
	}
}
