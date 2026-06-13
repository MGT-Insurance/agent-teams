package cost

import (
	"math"
	"testing"
)

func TestCost_knownModels(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		usage   TokenUsage
		wantUSD float64
		wantOK  bool
	}{
		{
			name:    "opus exact match, all zeros",
			model:   "claude-opus-4-8",
			usage:   TokenUsage{},
			wantUSD: 0,
			wantOK:  true,
		},
		{
			name:  "opus input+output only",
			model: "claude-opus-4-8",
			usage: TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000},
			// 1M×$5 + 1M×$25 = $30
			wantUSD: 30.0,
			wantOK:  true,
		},
		{
			name:  "haiku with date-stamp suffix (prefix match)",
			model: "claude-haiku-4-5-20251001",
			usage: TokenUsage{InputTokens: 1_000_000},
			// 1M×$1 = $1
			wantUSD: 1.0,
			wantOK:  true,
		},
		{
			name:  "sonnet cache read",
			model: "claude-sonnet-4-6",
			usage: TokenUsage{CacheReadInputTokens: 1_000_000},
			// 1M×$0.30 = $0.30
			wantUSD: 0.30,
			wantOK:  true,
		},
		{
			name:  "fable output",
			model: "claude-fable-5",
			usage: TokenUsage{OutputTokens: 1_000_000},
			// 1M×$50 = $50
			wantUSD: 50.0,
			wantOK:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Cost(tc.model, tc.usage)
			if ok != tc.wantOK {
				t.Errorf("priced=%v, want %v", ok, tc.wantOK)
			}
			if math.Abs(got-tc.wantUSD) > 1e-9 {
				t.Errorf("usd=%v, want %v", got, tc.wantUSD)
			}
		})
	}
}

func TestCost_unknownModel(t *testing.T) {
	usd, ok := Cost("claude-unknown-99", TokenUsage{InputTokens: 1_000_000})
	if ok {
		t.Error("expected priced=false for unknown model")
	}
	if usd != 0 {
		t.Errorf("expected usd=0 for unknown model, got %v", usd)
	}
}

func TestCost_cacheCreationSplitPath(t *testing.T) {
	// When 5m+1h split is present, price via the split (NOT via the total).
	// Opus: cw5m=$6.25/M, cw1h=$10/M
	// 1M 5m-tokens: $6.25; 1M 1h-tokens: $10 → total cache creation cost = $16.25
	u := TokenUsage{
		CacheCreationInputTokens: 2_000_000, // total = 5m + 1h
		CacheCreation5mTokens:    1_000_000,
		CacheCreation1hTokens:    1_000_000,
	}
	got, ok := Cost("claude-opus-4-8", u)
	if !ok {
		t.Fatal("expected priced=true")
	}
	want := 16.25
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("split path: got %v, want %v", got, want)
	}
}

func TestCost_cacheCreationFallbackFloor(t *testing.T) {
	// When 5m+1h split is absent (both zero) but CacheCreationInputTokens > 0,
	// price the total at the 5m rate as a conservative floor.
	// Opus: cw5m=$6.25/M → 2M×$6.25 = $12.50
	u := TokenUsage{
		CacheCreationInputTokens: 2_000_000,
		CacheCreation5mTokens:    0,
		CacheCreation1hTokens:    0,
	}
	got, ok := Cost("claude-opus-4-8", u)
	if !ok {
		t.Fatal("expected priced=true")
	}
	want := 12.50
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("fallback floor: got %v, want %v", got, want)
	}
}

func TestCost_noDoubleCount(t *testing.T) {
	// When the split path is taken, CacheCreationInputTokens must NOT also
	// be added — only the split fields contribute to cache creation cost.
	// Opus: 1M 5m + 1M 1h = $16.25. If total (2M) were double-counted at
	// any rate, the result would exceed $16.25.
	u := TokenUsage{
		CacheCreationInputTokens: 2_000_000,
		CacheCreation5mTokens:    1_000_000,
		CacheCreation1hTokens:    1_000_000,
	}
	got, _ := Cost("claude-opus-4-8", u)
	// Double-counting at cw5m rate would yield 2M×6.25 + 1M×6.25 + 1M×10 = 28.75
	// Correct result is 16.25 (split only).
	if got > 16.25+1e-9 {
		t.Errorf("double-count detected: got %v, want ≤16.25", got)
	}
}

func TestIsPriced(t *testing.T) {
	if !IsPriced("claude-opus-4-8") {
		t.Error("claude-opus-4-8 should be priced")
	}
	if !IsPriced("claude-haiku-4-5-20251001") {
		t.Error("claude-haiku-4-5-20251001 should match claude-haiku-4-5 prefix")
	}
	if IsPriced("claude-unknown-99") {
		t.Error("claude-unknown-99 should not be priced")
	}
}
