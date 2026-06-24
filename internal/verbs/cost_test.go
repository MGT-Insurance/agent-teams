package verbs

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// fakeReport builds a cost.Report for use in rendering tests without live data.
func fakeReport(id string, sessions int, models []cost.ModelUsage) cost.Report {
	return cost.Report{
		InitiativeID: id,
		DRISessions:  sessions,
		ByModel:      models,
	}
}

// ── buildJSONReport tests ────────────────────────────────────────────────────

func TestBuildJSONReport_EstimatedAlwaysTrue(t *testing.T) {
	r := fakeReport("at-test", 0, nil)
	out := buildJSONReport(r)
	if !out.Estimated {
		t.Error("expected estimated=true")
	}
}

func TestBuildJSONReport_ZeroSessions(t *testing.T) {
	r := fakeReport("at-test", 0, nil)
	out := buildJSONReport(r)
	if out.DRISessions != 0 {
		t.Errorf("expected dri_sessions=0, got %d", out.DRISessions)
	}
	if len(out.ByModel) != 0 {
		t.Errorf("expected empty by_model, got %d entries", len(out.ByModel))
	}
	if out.Total.EstimatedCostUSD != 0 {
		t.Errorf("expected total cost 0, got %f", out.Total.EstimatedCostUSD)
	}
}

func TestBuildJSONReport_SortingByCostDescThenModelAsc(t *testing.T) {
	// Use two known-priced models (opus > sonnet by price) and one unpriced.
	r := fakeReport("at-sort", 1, []cost.ModelUsage{
		{Model: "claude-sonnet-4-6", TokenUsage: cost.TokenUsage{InputTokens: 1000000}},
		{Model: "claude-opus-4-8", TokenUsage: cost.TokenUsage{InputTokens: 1000000}},
		{Model: "unknown-model-xyz", TokenUsage: cost.TokenUsage{InputTokens: 500}},
	})
	out := buildJSONReport(r)

	// by_model has 3 entries; opus (priced, higher rate) > sonnet (priced) > unknown (0 cost, priced=false)
	if len(out.ByModel) != 3 {
		t.Fatalf("expected 3 by_model entries, got %d", len(out.ByModel))
	}
	if out.ByModel[0].Model != "claude-opus-4-8" {
		t.Errorf("expected opus first (highest cost), got %s", out.ByModel[0].Model)
	}
	if out.ByModel[1].Model != "claude-sonnet-4-6" {
		t.Errorf("expected sonnet second, got %s", out.ByModel[1].Model)
	}
	if out.ByModel[2].Model != "unknown-model-xyz" {
		t.Errorf("expected unknown-model-xyz last (unpriced, cost=0), got %s", out.ByModel[2].Model)
	}
}

func TestBuildJSONReport_UnpricedModels(t *testing.T) {
	r := fakeReport("at-unpriced", 1, []cost.ModelUsage{
		{Model: "future-model-z", TokenUsage: cost.TokenUsage{InputTokens: 100}},
		{Model: "future-model-a", TokenUsage: cost.TokenUsage{InputTokens: 200}},
	})
	out := buildJSONReport(r)
	if len(out.UnpricedModels) != 2 {
		t.Fatalf("expected 2 unpriced_models, got %d", len(out.UnpricedModels))
	}
	// sorted asc
	if out.UnpricedModels[0] != "future-model-a" || out.UnpricedModels[1] != "future-model-z" {
		t.Errorf("unpriced_models not sorted: %v", out.UnpricedModels)
	}
}

func TestBuildJSONReport_TotalSumsPricedOnly(t *testing.T) {
	r := fakeReport("at-total", 2, []cost.ModelUsage{
		{Model: "claude-opus-4-8", TokenUsage: cost.TokenUsage{InputTokens: 1000000}},
		{Model: "unknown-xyz", TokenUsage: cost.TokenUsage{InputTokens: 999999}},
	})
	out := buildJSONReport(r)

	// Opus: 1M input at $5/M = $5.00. Unknown: $0 (unpriced).
	const wantTotal = 5.0
	if out.Total.EstimatedCostUSD != wantTotal {
		t.Errorf("total cost: want %.4f, got %.4f", wantTotal, out.Total.EstimatedCostUSD)
	}
	// Token totals include both models.
	wantInput := int64(1000000 + 999999)
	if out.Total.InputTokens != wantInput {
		t.Errorf("total input_tokens: want %d, got %d", wantInput, out.Total.InputTokens)
	}
}

func TestBuildJSONReport_No5m1hInJSON(t *testing.T) {
	r := fakeReport("at-fields", 1, []cost.ModelUsage{
		{
			Model: "claude-haiku-4-5",
			TokenUsage: cost.TokenUsage{
				InputTokens:              100,
				CacheCreation5mTokens:    50,
				CacheCreation1hTokens:    50,
				CacheCreationInputTokens: 100,
			},
		},
	})
	out := buildJSONReport(r)

	// Encode to JSON and verify 5m/1h fields are absent.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "5m") || strings.Contains(s, "1h") || strings.Contains(s, "ephemeral") {
		t.Errorf("JSON must not contain 5m/1h/ephemeral fields; got: %s", s)
	}
	// The stable field must be present.
	if !strings.Contains(s, "cache_creation_input_tokens") {
		t.Errorf("JSON must contain cache_creation_input_tokens; got: %s", s)
	}
}

// ── renderJSON tests ─────────────────────────────────────────────────────────

func TestRenderJSON_ValidJSON(t *testing.T) {
	r := fakeReport("at-json", 3, []cost.ModelUsage{
		{Model: "claude-sonnet-4-6", TokenUsage: cost.TokenUsage{InputTokens: 500000, OutputTokens: 10000}},
	})
	var buf bytes.Buffer
	ctx := &cli.Context{Stdout: &buf, Stderr: &buf}
	if err := renderJSON(ctx, r); err != nil {
		t.Fatalf("renderJSON: %v", err)
	}
	var out jsonReport
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if out.InitiativeID != "at-json" {
		t.Errorf("initiative_id: want at-json, got %s", out.InitiativeID)
	}
	if !out.Estimated {
		t.Error("estimated must be true")
	}
	if out.DRISessions != 3 {
		t.Errorf("dri_sessions: want 3, got %d", out.DRISessions)
	}
}

// ── renderTable tests ────────────────────────────────────────────────────────

func TestRenderTable_EstimatedLabel(t *testing.T) {
	r := fakeReport("at-tbl", 1, []cost.ModelUsage{
		{Model: "claude-haiku-4-5", TokenUsage: cost.TokenUsage{InputTokens: 1000}},
	})
	var buf bytes.Buffer
	ctx := &cli.Context{Stdout: &buf, Stderr: &buf}
	if err := renderTable(ctx, r); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "estimated") {
		t.Errorf("table must contain 'estimated'; got:\n%s", out)
	}
	if !strings.Contains(out, "not billed") {
		t.Errorf("table must contain 'not billed'; got:\n%s", out)
	}
}

func TestRenderTable_ZeroReport(t *testing.T) {
	r := fakeReport("at-zero", 0, nil)
	var buf bytes.Buffer
	ctx := &cli.Context{Stdout: &buf, Stderr: &buf}
	if err := renderTable(ctx, r); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No token usage found") {
		t.Errorf("zero report must say no usage; got:\n%s", out)
	}
}

// ── kong dispatch tests ───────────────────────────────────────────────────────

// runCostKong dispatches cost through a cli.Parser backed by RegisterCostKong.
func runCostKong(args []string) error {
	p, err := cli.NewParser()
	if err != nil {
		return err
	}
	RegisterCostKong(p)
	kctx, parseErr := p.Parse(append([]string{"cost"}, args...))
	if parseErr != nil {
		return parseErr
	}
	var buf bytes.Buffer
	ctx := &cli.Context{Stdout: &buf, Stderr: &buf}
	kctx.Bind(ctx)
	return kctx.Run(ctx)
}

func TestCostCmd_MissingID(t *testing.T) {
	// kong requires the <initiative-id> positional; omitting it is a parse error.
	err := runCostKong([]string{})
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestCostCmd_MissingIDWithJSONFlag(t *testing.T) {
	// --json without <id> is still a parse error for the required positional.
	err := runCostKong([]string{"--json"})
	if err == nil {
		t.Fatal("expected error for missing id with --json")
	}
}

func TestCostCmd_UnknownFlag(t *testing.T) {
	// kong rejects unknown flags as a parse error.
	err := runCostKong([]string{"--bogus-flag"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

// TestCostCmd_IDBeforeFlag verifies the primary use case: "ateam cost <id> --json"
// where the positional comes BEFORE the flag. kong handles this natively.
func TestCostCmd_IDBeforeFlag(t *testing.T) {
	err := runCostKong([]string{"at-qek", "--json"})
	// Attribute will fail because ~/.claude/{jobs,projects} may not contain
	// the fixture data, but it must NOT be a UsageError (missing-id) or a
	// SilentError (bad-flag). Either nil (dirs happen to exist and are empty)
	// or a non-usage, non-silent error is acceptable.
	if err == nil {
		return // dirs were empty / session not found → zero report, no error
	}
	if _, ok := err.(*cli.UsageError); ok {
		t.Errorf("id-before-flag must not produce UsageError; got: %v", err)
	}
	if _, ok := err.(*cli.SilentError); ok {
		t.Errorf("id-before-flag must not produce SilentError; got: %v", err)
	}
}
