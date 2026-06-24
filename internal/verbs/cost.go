// This file is owned by Track C (cost verb).
package verbs

import (
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// RegisterCostKong registers the cost verb onto p using its native kong struct.
// costKong is defined in kong_converted.go (LOOP bead ownership) because cost was
// one of the 3 verbs converted as the loop proof. The ring-track enh bead for
// single-verb files (illp) may move costKong here and remove this note.
func RegisterCostKong(p *cli.Parser) {
	p.AddVerb("cost", "Report estimated token cost for an initiative.", &costKong{})
}

// ── JSON output ───────────────────────────────────────────────────────────────

// jsonTotal is the total object in the JSON output.
type jsonTotal struct {
	InputTokens              int64   `json:"input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
	EstimatedCostUSD         float64 `json:"estimated_cost_usd"`
}

// jsonModel is one by_model entry.
type jsonModel struct {
	Model                    string  `json:"model"`
	InputTokens              int64   `json:"input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	CacheCreationInputTokens int64   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64   `json:"cache_read_input_tokens"`
	EstimatedCostUSD         float64 `json:"estimated_cost_usd"`
	Priced                   bool    `json:"priced"`
}

// jsonReport is the top-level JSON shape (frozen per agent-teams-9er).
type jsonReport struct {
	InitiativeID   string      `json:"initiative_id"`
	Estimated      bool        `json:"estimated"`
	DRISessions    int         `json:"dri_sessions"`
	Total          jsonTotal   `json:"total"`
	ByModel        []jsonModel `json:"by_model"`
	UnpricedModels []string    `json:"unpriced_models"`
}

// buildJSONReport converts a cost.Report into the frozen JSON shape.
func buildJSONReport(r cost.Report) jsonReport {
	models := make([]jsonModel, 0, len(r.ByModel))
	var total jsonTotal
	var unpriced []string

	for _, mu := range r.ByModel {
		usd, priced := cost.Cost(mu.Model, mu.TokenUsage)
		entry := jsonModel{
			Model:                    mu.Model,
			InputTokens:              mu.InputTokens,
			OutputTokens:             mu.OutputTokens,
			CacheCreationInputTokens: mu.CacheCreationInputTokens,
			CacheReadInputTokens:     mu.CacheReadInputTokens,
			EstimatedCostUSD:         usd,
			Priced:                   priced,
		}
		models = append(models, entry)

		total.InputTokens += mu.InputTokens
		total.OutputTokens += mu.OutputTokens
		total.CacheCreationInputTokens += mu.CacheCreationInputTokens
		total.CacheReadInputTokens += mu.CacheReadInputTokens
		if priced {
			total.EstimatedCostUSD += usd
		} else {
			unpriced = append(unpriced, mu.Model)
		}
	}

	// Sort: estimated_cost_usd desc, then model asc.
	sort.Slice(models, func(i, j int) bool {
		if models[i].EstimatedCostUSD != models[j].EstimatedCostUSD {
			return models[i].EstimatedCostUSD > models[j].EstimatedCostUSD
		}
		return models[i].Model < models[j].Model
	})

	sort.Strings(unpriced)

	if unpriced == nil {
		unpriced = []string{}
	}
	if models == nil {
		models = []jsonModel{}
	}

	return jsonReport{
		InitiativeID:   r.InitiativeID,
		Estimated:      true,
		DRISessions:    r.DRISessions,
		Total:          total,
		ByModel:        models,
		UnpricedModels: unpriced,
	}
}

func renderJSON(ctx *cli.Context, r cost.Report) error {
	out := buildJSONReport(r)
	enc := json.NewEncoder(ctx.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// ── Human-readable table ──────────────────────────────────────────────────────

func renderTable(ctx *cli.Context, r cost.Report) error {
	jr := buildJSONReport(r)

	w := tabwriter.NewWriter(ctx.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Initiative: %s\tDRI sessions: %d\n", r.InitiativeID, r.DRISessions)
	fmt.Fprintln(w, "Note: estimated (token x published price), not billed")
	fmt.Fprintln(w)

	if len(jr.ByModel) == 0 {
		fmt.Fprintln(w, "No token usage found.")
		w.Flush()
		return nil
	}

	fmt.Fprintln(w, "Model\tInput\tOutput\tCache Write\tCache Read\tEst. Cost (USD)")
	fmt.Fprintln(w, "-----\t-----\t------\t-----------\t----------\t---------------")
	for _, m := range jr.ByModel {
		costStr := fmt.Sprintf("$%.4f", m.EstimatedCostUSD)
		if !m.Priced {
			costStr = "(unpriced)"
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%s\n",
			m.Model, m.InputTokens, m.OutputTokens,
			m.CacheCreationInputTokens, m.CacheReadInputTokens,
			costStr)
	}

	fmt.Fprintln(w, "-----\t-----\t------\t-----------\t----------\t---------------")
	fmt.Fprintf(w, "TOTAL\t%d\t%d\t%d\t%d\t$%.4f\n",
		jr.Total.InputTokens, jr.Total.OutputTokens,
		jr.Total.CacheCreationInputTokens, jr.Total.CacheReadInputTokens,
		jr.Total.EstimatedCostUSD)

	if len(jr.UnpricedModels) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Unpriced models (tokens counted, cost excluded): %v\n", jr.UnpricedModels)
	}

	return w.Flush()
}
