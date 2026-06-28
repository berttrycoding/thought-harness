// Command trprobe runs ONE grounding bench item through the harness arm against a
// real model and dumps the event trace, to answer "was the file actually provided to
// the model?" — i.e. did the engine's rung-4 reality sourcer read the artifact, did
// the bytes reach a model prompt, and what did the model finally answer.
//
//	go run ./cmd/trprobe [item-id]      # default grounding-A-gold-0001
//	PROBE_MODEL=qwen/qwen3.6-35b-a3b go run ./cmd/trprobe
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/bench/runner"
	"github.com/berttrycoding/thought-harness/internal/bench/tiera"
	benchtypes "github.com/berttrycoding/thought-harness/internal/bench/types"
)

func main() {
	id := "grounding-A-gold-0001"
	if len(os.Args) > 1 {
		id = os.Args[1]
	}
	// The byte the file contains and the lure it must beat (item 0001 specifics; for
	// other items pass the needle as the 2nd arg).
	needle := "MaxParWidth"
	if len(os.Args) > 2 {
		needle = os.Args[2]
	}

	items, err := tiera.LoadItems("internal/bench/banks/pilot/grounding-tiera.jsonl")
	if err != nil {
		panic(err)
	}
	var item benchtypes.TierAItem
	for _, it := range items {
		if it.ID == id {
			item = it
			break
		}
	}
	if item.ID == "" {
		panic("item not found: " + id)
	}

	model := os.Getenv("PROBE_MODEL")
	if model == "" {
		model = "qwen/qwen3.6-35b-a3b"
	}
	factory := runner.LLMFactory("http://localhost:1234/v1", model)

	sb, cleanup, err := tiera.Materialize(item)
	defer cleanup()
	if err != nil {
		panic(err)
	}
	workspace := sb.Root
	if sb.ArtifactPath == "" {
		workspace = ""
	}
	r := runner.New(factory, workspace)
	spec := runner.Spec{Prompt: item.Prompt, Arm: benchtypes.ArmHarness, Mechanism: item.Mechanism, Seed: 1729}
	run := r.Run(spec)

	fmt.Printf("=== ITEM %s | ARM harness | MODEL %s ===\n", item.ID, model)
	fmt.Printf("WORKSPACE %s\nARTIFACT  %s  (exists=%v)\n", workspace, sb.ArtifactPath, fileExists(sb.ArtifactPath))
	fmt.Printf("PROMPT    %s\n\n", item.Prompt)
	fmt.Printf("FINAL ANSWER: %q\n\n", run.Text)

	// Histogram + the three load-bearing questions.
	hist := map[string]int{}
	sawObs := false
	obsHadFile := false
	fileInPrompt := false
	for _, ev := range run.Events {
		hist[ev.Kind]++
		blob := ev.Summary + " " + marshal(ev.Data)
		if strings.Contains(ev.Kind, "observation") {
			sawObs = true
			if strings.Contains(blob, needle) {
				obsHadFile = true
			}
		}
		// A model prompt carrying the file bytes (the system/user fields of an llm.* event).
		if strings.HasPrefix(ev.Kind, "llm") {
			if strings.Contains(dataStr(ev.Data, "system")+dataStr(ev.Data, "user"), needle) {
				fileInPrompt = true
			}
		}
	}

	fmt.Println("---- Q1: did the engine READ the file? (action.observation fired) ----")
	fmt.Printf("   action.observation fired: %v\n", sawObs)
	fmt.Printf("   observation contained %q: %v\n", needle, obsHadFile)
	fmt.Println("---- Q2: did the file bytes reach a MODEL PROMPT? ----")
	fmt.Printf("   a model prompt contained %q: %v\n", needle, fileInPrompt)
	fmt.Println()

	fmt.Println("---- every event mentioning the needle (where the file shows up) ----")
	for _, ev := range run.Events {
		blob := ev.Summary + " " + marshal(ev.Data)
		if strings.Contains(blob, needle) {
			fmt.Printf("   [%s] %s\n", ev.Kind, trunc(oneLine(blob), 200))
		}
	}
	fmt.Println()

	// TRPROBE_DUMP_LLM=1 prints EVERY llm.call / llm.fallback in order — role, finish_reason, completion
	// tokens, and whether the role carries a ".escalation" suffix (the max-context escalation tier fired).
	// The fastest way to see why a final answer surfaced the "thinking substrate unavailable" gap.
	if os.Getenv("TRPROBE_DUMP_LLM") != "" {
		fmt.Println("---- every llm.call / llm.fallback (role | finish | ctok | escalation?) ----")
		for _, ev := range run.Events {
			if !strings.HasPrefix(ev.Kind, "llm") {
				continue
			}
			role := dataStr(ev.Data, "role")
			esc := ""
			if strings.Contains(role, ".escalation") {
				esc = "  <-- ESCALATED (max-ctx)"
			}
			fin := dataStr(ev.Data, "finish_reason")
			ctok := ""
			if v, ok := ev.Data["completion_tokens"]; ok {
				ctok = fmt.Sprintf("%v", v)
			}
			fmt.Printf("   %-14s role=%-26s finish=%-7s ctok=%-6s%s\n", ev.Kind, role, fin, ctok, esc)
		}
		fmt.Println()
	}

	fmt.Println("---- event-kind histogram ----")
	for k, n := range hist {
		fmt.Printf("   %-34s %d\n", k, n)
	}
}

func dataStr(d map[string]any, key string) string {
	if d == nil {
		return ""
	}
	if v, ok := d[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
func marshal(d map[string]any) string { b, _ := json.Marshal(d); return string(b) }
func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", "\\n"), "\t", " ")
}
func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}
