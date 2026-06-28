package zoommem

// Step B of the retrieval upgrade: does a SMALL LLM rerank past memories better than the lexical
// signal? This is the part grep/lexical can't do — it bridges synonyms ("exhaustion" ~ "saturation").
// Model-gated: skips cleanly if no local model is reachable, so the rest of the suite stays offline.
//
// Design mirrors real agentic retrieval: cheap lexical recall builds a shortlist, the small model
// reranks it. We compare recall of the planted should_surface memories three ways — the shortlist
// ceiling, lexical top-K, and LLM-reranked top-K.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const lmURL = "http://localhost:1234/v1"

func modelReachable() (string, bool) {
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get(lmURL + "/models")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var d struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&d) != nil || len(d.Data) == 0 {
		return "", false
	}
	return d.Data[0].ID, true
}

var intRe = regexp.MustCompile(`-?\d+`)

func parseIDs(s string, k int) []int {
	if l, r := strings.Index(s, "["), strings.LastIndex(s, "]"); l >= 0 && r > l {
		s = s[l : r+1] // prefer the JSON array if present
	}
	var ids []int
	for _, m := range intRe.FindAllString(s, -1) {
		if n, err := strconv.Atoi(m); err == nil {
			ids = append(ids, n)
		}
		if len(ids) >= k {
			break
		}
	}
	return ids
}

func llmPickIDs(model, focus string, cands []Unit, k int) ([]int, error) {
	var sb strings.Builder
	for _, u := range cands {
		fmt.Fprintf(&sb, "[%d] %s\n", u.ID, u.Thought)
	}
	system := "You decide which earlier notes are most relevant to bring back into mind for the current thought. " +
		"Reply with ONLY a JSON array of the note id numbers (the numbers shown in brackets), most relevant first, no other text."
	user := fmt.Sprintf("Current thought:\n%s\n\nEarlier notes:\n%s\nReturn the ids of the %d most relevant notes as a JSON array, most relevant first.",
		focus, sb.String(), k)
	body, _ := json.Marshal(map[string]any{
		"model":       model,
		"temperature": 0,
		"max_tokens":  300,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	c := &http.Client{Timeout: 90 * time.Second}
	resp, err := c.Post(lmURL+"/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("no choices")
	}
	text := out.Choices[0].Message.Content
	if strings.TrimSpace(text) == "" {
		text = out.Choices[0].Message.Reasoning // small reasoning models sometimes leave content empty
	}
	return parseIDs(text, k), nil
}

func idSet(us []Unit) map[int]bool {
	m := map[int]bool{}
	for _, u := range us {
		m[u.ID] = true
	}
	return m
}

func recallOf(have map[int]bool, want []int) float64 {
	if len(want) == 0 {
		return 0
	}
	hit := 0
	for _, id := range want {
		if have[id] {
			hit++
		}
	}
	return float64(hit) / float64(len(want))
}

func TestDataset_StepB_LLMRerank(t *testing.T) {
	if os.Getenv("ZOOMMEM_RERANK") == "" {
		t.Skip("set ZOOMMEM_RERANK=1 (and load a model in LM Studio) to run the live rerank experiment")
	}
	model, ok := modelReachable()
	if !ok {
		t.Skip("no local model at " + lmURL + " — load a small model in LM Studio to run the rerank experiment")
	}
	const shortlistN, K = 15, 6
	var shortRec, lexRec, llmRec []float64
	probes := 0
	for _, ep := range loadEpisodes(t) {
		goal := ep.Units[0].Thought
		for _, p := range ep.Probes {
			focus := find(ep.Units, p.FocusID)
			var cands []Unit // PAST cross-branch memories (what a retrieval step would consider)
			for _, u := range ep.Units {
				if u.Tick < focus.Tick && u.Branch != focus.Branch {
					cands = append(cands, u)
				}
			}
			if len(cands) == 0 {
				continue
			}
			sort.SliceStable(cands, func(i, j int) bool {
				return relevanceOnly(cands[i], focus, goal) > relevanceOnly(cands[j], focus, goal)
			})
			shortlist := cands
			if len(shortlist) > shortlistN {
				shortlist = shortlist[:shortlistN]
			}
			shortRec = append(shortRec, recallOf(idSet(shortlist), p.ShouldSurface))

			lexTopK := shortlist
			if len(lexTopK) > K {
				lexTopK = lexTopK[:K]
			}
			lexRec = append(lexRec, recallOf(idSet(lexTopK), p.ShouldSurface))

			picks, err := llmPickIDs(model, focus.Thought, shortlist, K)
			if err != nil {
				t.Logf("probe focus #%d: llm error %v (recall 0)", p.FocusID, err)
			}
			pickSet := map[int]bool{}
			for _, id := range picks {
				pickSet[id] = true
			}
			llmRec = append(llmRec, recallOf(pickSet, p.ShouldSurface))
			if testing.Verbose() {
				var slIDs, lexIDs []int
				for _, u := range shortlist {
					slIDs = append(slIDs, u.ID)
				}
				for _, u := range lexTopK {
					lexIDs = append(lexIDs, u.ID)
				}
				t.Logf("  [%s] focus#%d want=%v | shortlist=%v | lexTop%d=%v | llm=%v",
					ep.Name, p.FocusID, p.ShouldSurface, slIDs, K, lexIDs, picks)
			}
			probes++
		}
	}
	t.Logf("Step B rerank (model=%s, %d probes, shortlist=%d, K=%d):", model, probes, shortlistN, K)
	t.Logf("  shortlist recall@%d (the ceiling both pickers share) = %.0f%%", shortlistN, mean(shortRec)*100)
	t.Logf("  lexical  top-%d recall                                = %.0f%%", K, mean(lexRec)*100)
	t.Logf("  LLM rerank top-%d recall                              = %.0f%%", K, mean(llmRec)*100)
}
