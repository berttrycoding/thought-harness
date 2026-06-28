package costest

import "strings"

// defaultCharsPerToken is the fallback chars/token ratio when the log has NO ground-truth
// usage to calibrate against. ~4 chars/token is the well-known English-text rule of thumb
// (the BPE tokenizers GPT/DeepSeek/Llama use average ~3.7–4.2 chars/token on prose; code
// and JSON skew lower). 4.0 is the conservative middle the OpenAI cookbook quotes. The log
// path PREFERS a calibrated ratio (CalibrateCPT) whenever even one call carries real usage.
const defaultCharsPerToken = 4.0

// TokenModel turns TEXT into an estimated token count. CharsPerToken is the divisor (a
// non-positive value falls back to defaultCharsPerToken). Calibrated is the provenance flag
// the report shows so a reader knows whether the number is anchored to the real tokenizer
// (a usage-bearing sample in the log) or the generic chars/4 heuristic. Samples is how many
// ground-truth calls the calibration averaged over (0 ⇒ heuristic).
type TokenModel struct {
	CharsPerToken float64
	Calibrated    bool
	Samples       int
}

// HeuristicTokenModel is the chars/4 fallback used when nothing in the log reports usage.
func HeuristicTokenModel() TokenModel {
	return TokenModel{CharsPerToken: defaultCharsPerToken, Calibrated: false, Samples: 0}
}

// Estimate returns the estimated token count for a text via len(runes)/CharsPerToken,
// rounded to the nearest int (never negative). Empty text ⇒ 0. Runes (not bytes) so a
// multibyte prompt isn't over-counted on its UTF-8 width.
func (m TokenModel) Estimate(text string) int {
	cpt := m.CharsPerToken
	if cpt <= 0 {
		cpt = defaultCharsPerToken
	}
	n := len([]rune(text))
	if n == 0 {
		return 0
	}
	// round-half-up: (chars + cpt/2) / cpt, in int math via +0.5.
	est := float64(n)/cpt + 0.5
	if est < 0 {
		return 0
	}
	return int(est)
}

// CalibrateCPT derives the chars-per-token ratio from the calls that DO carry real usage:
// for each usage-bearing call, prompt-chars / prompt_tokens (and, when the response text +
// completion_tokens are both present, response-chars / completion_tokens). The returned
// TokenModel averages those per-call ratios (weighted by tokens, so a long calibration call
// dominates a short one — the right weighting since we apply the ratio to ALL chars). With
// NO usage-bearing call it returns HeuristicTokenModel (chars/4, uncalibrated). This is what
// makes the estimate ANCHORED to the real tokenizer the moment even one ground-truth sample
// exists in the same log.
func CalibrateCPT(calls []Call) TokenModel {
	var totalChars, totalToks float64
	samples := 0
	for _, c := range calls {
		if c.PromptTokens > 0 {
			chars := float64(len([]rune(c.Concat())))
			if chars > 0 {
				totalChars += chars
				totalToks += float64(c.PromptTokens)
				samples++
			}
		}
		if c.CompletionTokens > 0 && c.Response != "" {
			chars := float64(len([]rune(c.Response)))
			if chars > 0 {
				totalChars += chars
				totalToks += float64(c.CompletionTokens)
				samples++
			}
		}
	}
	if samples == 0 || totalToks <= 0 {
		return HeuristicTokenModel()
	}
	return TokenModel{CharsPerToken: totalChars / totalToks, Calibrated: true, Samples: samples}
}

// TokenEstimate is the per-log token total the report prints: the input/output token counts
// (real usage where the server reported it, estimated-from-text otherwise) plus the token
// model that produced the estimated portion and how many calls fell back to estimation.
type TokenEstimate struct {
	// InTokens / OutTokens are the workload's total input / output tokens (real where
	// reported, estimated otherwise).
	InTokens  int
	OutTokens int
	// EstimatedCalls is how many calls had NO server usage and were sized from text; Calls is
	// the total. (EstimatedCalls==Calls ⇒ the whole projection is heuristic, e.g. an LM Studio
	// or test-double pilot.)
	EstimatedCalls int
	Calls          int
	// Model is the chars/token model used for the estimated portion (its Calibrated flag +
	// CharsPerToken go into the report so the method is transparent).
	Model TokenModel
}

// EstimateTokens sizes the whole workload: it first CALIBRATES a chars/token model off any
// usage-bearing calls, then for each call uses the server's real prompt/completion tokens
// when present and the calibrated text estimate otherwise. The cache-hit estimate is layered
// on top separately (prefix.go) — this function only counts TOTAL in/out tokens.
func EstimateTokens(calls []Call) TokenEstimate {
	model := CalibrateCPT(calls)
	te := TokenEstimate{Model: model, Calls: len(calls)}
	for _, c := range calls {
		// Input: trust real usage; else estimate from the concatenated prompt text.
		if c.PromptTokens >= 0 {
			te.InTokens += c.PromptTokens
		} else {
			te.InTokens += model.Estimate(c.Concat())
			te.EstimatedCalls++
		}
		// Output: trust real usage; else estimate from the response text.
		if c.CompletionTokens >= 0 {
			te.OutTokens += c.CompletionTokens
		} else {
			te.OutTokens += model.Estimate(c.Response)
		}
	}
	return te
}

// charsHead is a tiny helper for the report's prompt-preview column (first n runes of a
// single line of text, newlines collapsed). Kept here so the estimator package owns its own
// display helper rather than re-importing one.
func charsHead(text string, n int) string {
	flat := strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "\t", " ")
	r := []rune(flat)
	if len(r) <= n {
		return flat
	}
	return string(r[:n]) + "…"
}
