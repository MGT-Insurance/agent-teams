// Package cost estimates Claude token spend for an agent-teams initiative.
// Cost figures are estimated (token count × published price), never billed.
package cost

import "strings"

// TokenUsage holds the six token-count fields present in every assistant record.
// CacheCreationInputTokens is the TOTAL cache creation (5m + 1h combined) from
// the top-level message.usage field — it is the stable token field exposed in
// JSON output.
// CacheCreation5mTokens and CacheCreation1hTokens come from the nested
// message.usage.cache_creation object and carry the split used for accurate
// pricing; see Cost for the fallback rule.
type TokenUsage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64 // TOTAL cache creation = 5m + 1h
	CacheCreation5mTokens    int64 // from message.usage.cache_creation.ephemeral_5m_input_tokens
	CacheCreation1hTokens    int64 // from message.usage.cache_creation.ephemeral_1h_input_tokens
	CacheReadInputTokens     int64
}

// modelRates holds per-million-token prices for one model.
type modelRates struct {
	input  float64 // input tokens
	output float64 // output tokens
	cw5m   float64 // cache write, 5-minute TTL (1.25× input)
	cw1h   float64 // cache write, 1-hour TTL (2× input)
	cr     float64 // cache read (0.1× input)
}

// priceTable maps model name prefixes to their published per-million-token rates.
// Keys are matched as prefixes of the model string from the transcript (e.g.
// "claude-haiku-4-5" matches "claude-haiku-4-5-20251001" which Claude Code
// appends as a date-stamp version suffix). Prefix matching is intentional: the
// model family determines pricing, and the date suffix carries no pricing signal.
var priceTable = map[string]modelRates{
	"claude-opus-4-8":   {input: 5, output: 25, cw5m: 6.25, cw1h: 10, cr: 0.50},
	"claude-opus-4-7":   {input: 5, output: 25, cw5m: 6.25, cw1h: 10, cr: 0.50},
	"claude-sonnet-4-6": {input: 3, output: 15, cw5m: 3.75, cw1h: 6, cr: 0.30},
	"claude-haiku-4-5":  {input: 1, output: 5, cw5m: 1.25, cw1h: 2, cr: 0.10},
	"claude-fable-5":    {input: 10, output: 50, cw5m: 12.50, cw1h: 20, cr: 1.00},
}

// lookup finds the rates for model by prefix-matching priceTable keys.
// Returns the rates and true when found, zero value and false otherwise.
func lookup(model string) (modelRates, bool) {
	for key, rates := range priceTable {
		if strings.HasPrefix(model, key) {
			return rates, true
		}
	}
	return modelRates{}, false
}

// Cost returns an estimated USD cost for u under model's published price table,
// and whether the model was found in the price table. An unknown model returns
// (0, false) — tokens are still counted by the caller; cost is never guessed.
//
// Cache creation pricing uses the 5m/1h split when available (verified in real
// at-2jh transcript data: ephemeral_1h tokens are present and pricing all
// cache creation at the 5m rate would undercount 1h writes by 37.5%, which is
// material on Opus at $10/M vs $6.25/M). When both split fields are zero but
// CacheCreationInputTokens is positive (records without the nested
// cache_creation object), the total is priced at the 5m rate as a conservative
// floor — it underestimates rather than overestimates, and the caller documents
// the estimation gap via estimated=true in all output.
func Cost(model string, u TokenUsage) (usd float64, priced bool) {
	rates, ok := lookup(model)
	if !ok {
		return 0, false
	}

	const perM = 1_000_000.0

	var cacheCreationCost float64
	if u.CacheCreation5mTokens > 0 || u.CacheCreation1hTokens > 0 {
		// Prefer the 5m/1h split — accurate when the nested cache_creation object is present.
		cacheCreationCost = float64(u.CacheCreation5mTokens)*rates.cw5m/perM +
			float64(u.CacheCreation1hTokens)*rates.cw1h/perM
	} else if u.CacheCreationInputTokens > 0 {
		// Fallback floor: no nested split; price the total at the 5m rate.
		// Underestimates when some writes were 1h, but avoids double-counting.
		cacheCreationCost = float64(u.CacheCreationInputTokens) * rates.cw5m / perM
	}

	total := float64(u.InputTokens)*rates.input/perM +
		float64(u.OutputTokens)*rates.output/perM +
		cacheCreationCost +
		float64(u.CacheReadInputTokens)*rates.cr/perM

	return total, true
}

// IsPriced reports whether model has an entry in the price table.
func IsPriced(model string) bool {
	_, ok := lookup(model)
	return ok
}
