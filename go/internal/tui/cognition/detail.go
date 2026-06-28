package cognition

// detail.go — the per-system "full-screen tab" renderers (the COGNITION tab split). Where panels.go
// renders each layer as ONE compact overview panel, these render a layer's INTERNALS at full width.
// The tui package lays the returned Panels out into the active tab; the rendering (tones, bars,
// clipping) stays here with the other panel renderers so the design language is one place.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/berttrycoding/thought-harness/internal/events"
)

// scanPrimitiveSubAgents extracts the per-specialist dispatch scan ({domain, effective, fired, …}) off the
// most recent SubDispatch/SubQuiet event. Shared by renderSubconscious (the overview list) and
// SubconsciousCards (the full-screen per-item cards) so both read the same source.
func scanPrimitiveSubAgents(vm ViewModel) []map[string]any {
	ev := lastEvent(vm, events.SubDispatch, events.SubQuiet)
	if ev == nil {
		return nil
	}
	var scan []map[string]any
	if raw, ok := ev.Data["scan"].([]any); ok {
		for _, r := range raw {
			if m, ok := r.(map[string]any); ok {
				scan = append(scan, m)
			}
		}
	} else if raw, ok := ev.Data["scan"].([]map[string]any); ok {
		scan = raw
	}
	return scan
}

// scanStaleTicks reports how many engine ticks have elapsed since the last dispatch/quiet scan event,
// relative to the current snapshot tick. In awake/continuous mode most ticks take a non-dispatch branch
// (continuous.go fires dispatch on one branch only), so the last scan can be many ticks old — the panel
// labels it stale rather than rendering a frozen scan as if it were this tick (the "all 0.00 in awake
// mode" symptom, E2). ok=false when there is no scan event yet, or no tick to compare against.
func scanStaleTicks(vm ViewModel) (int, bool) {
	ev := lastEvent(vm, events.SubDispatch, events.SubQuiet)
	if ev == nil || vm.Snap.Tick <= 0 {
		return 0, false
	}
	if d := vm.Snap.Tick - ev.Tick; d > 0 {
		return d, true
	}
	return 0, false
}

// SubconsciousCards renders ONE card Panel per scanned specialist — the Subconscious full-screen tab's
// "a dedicated panel per sub-agent / specialist" layout. Each card shows the domain, a relevance bar,
// the .2f effective relevance against θ, and whether it FIRED (crossed θ and emitted a Candidate);
// fired cards take an accent border. With no scan yet it returns a single placeholder card.
func SubconsciousCards(vm ViewModel) []Panel {
	scan := scanPrimitiveSubAgents(vm)
	w := contentW(vm)
	if len(scan) == 0 {
		return []Panel{{Body: faintStr(clip("(no specialists scanned yet — submit a goal)", w))}}
	}
	// awake non-dispatch ticks emit no fresh scan, so a stale roster reads as the live one — label it
	// (E2). A scan from this tick is silent; one from an earlier tick gets an explicit "idle since" head.
	var staleHead []Panel
	if age, ok := scanStaleTicks(vm); ok {
		staleHead = []Panel{{Body: warnStr(clip(fmt.Sprintf("subconscious idle — last scan %d tick(s) ago (no specialist pulled since)", age), w))}}
	}
	theta := vm.Snap.Theta
	// Cap to the most-relevant specialists (fired first, then by effective relevance) so a large roster
	// — which the registry-scaling work grows — doesn't render an enormous scroll of one 7-row card each
	// (L5). The overflow count is shown as a final card.
	total := len(scan)
	sort.SliceStable(scan, func(i, j int) bool {
		fi, _ := scan[i]["fired"].(bool)
		fj, _ := scan[j]["fired"].(bool)
		if fi != fj {
			return fi
		}
		ei, _ := asNum(scan[i]["effective"])
		ej, _ := asNum(scan[j]["effective"])
		return ei > ej
	})
	if len(scan) > maxConvRows {
		scan = scan[:maxConvRows]
	}
	cards := make([]Panel, 0, len(scan)+1)
	for _, sc := range scan {
		dom, _ := sc["domain"].(string)
		eff, _ := asNum(sc["effective"])
		fired, _ := sc["fired"].(bool)

		tone := colFaint
		status := faintStr("idle · below θ")
		border := ""
		if fired {
			tone = colAccent
			status = txt("◀ fired a Candidate", colAccent).render()
			border = string(colAccent)
		}
		title := join(txt("▸ ", tone), txt(clip(dom, w-2), tone))
		barLine := join(txt(bar(eff, 12), tone), txt(fmt.Sprintf("  %.2f", eff), colText))
		thetaLine := faintStr(fmt.Sprintf("θ = %.2f", theta))
		body := strings.Join([]string{title, "", barLine, status, thetaLine}, "\n")
		cards = append(cards, Panel{Body: body, Border: border})
	}
	if total > len(scan) {
		cards = append(cards, Panel{Body: faintStr(clip(fmt.Sprintf("…+%d more specialists (by relevance)", total-len(scan)), w))})
	}
	return append(staleHead, cards...)
}
