package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cost"
)

// ExtractMetrics's signature is frozen with no jobsDir/projectsDir params (it
// derives them from os.UserHomeDir(), matching `ateam cost`), so these tests
// fake $HOME rather than injecting paths directly — os.UserHomeDir() reads
// $HOME on darwin/linux, and t.Setenv restores the prior value on cleanup.

func TestExtractMetrics_basic(t *testing.T) {
	const id = "test-eval-metrics-001"
	home := t.TempDir()
	t.Setenv("HOME", home)

	jobsDir := filepath.Join(home, ".claude", "jobs")
	projectsDir := filepath.Join(home, ".claude", "projects")

	cwd := "/Users/testuser/worktrees/eval-metrics-test"
	sessionID := "11112222-3333-4444-5555-666677778888"

	jobDir := filepath.Join(jobsDir, "job1")
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		t.Fatal(err)
	}
	stateData, _ := json.Marshal(map[string]string{
		"intent":    "/dri " + id,
		"sessionId": sessionID,
		"cwd":       cwd,
	})
	if err := os.WriteFile(filepath.Join(jobDir, "state.json"), stateData, 0644); err != nil {
		t.Fatal(err)
	}

	slug := cost.SlugifyCWD(cwd)
	projDir := filepath.Join(projectsDir, slug)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Two assistant turns, 5 minutes apart, on a priced model (claude-opus-4-8),
	// with 2 tool_use blocks total.
	turnStart := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-06-30T15:00:00.000Z",
		"message": map[string]any{
			"id":    "msg-1",
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(1000),
				"output_tokens": int64(500),
			},
			"content": []map[string]any{
				{"type": "tool_use"},
				{"type": "tool_use"},
			},
		},
	}
	turnEnd := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-06-30T15:05:00.000Z",
		"message": map[string]any{
			"id":    "msg-2",
			"role":  "assistant",
			"model": "claude-opus-4-8",
			"usage": map[string]any{
				"input_tokens":  int64(100),
				"output_tokens": int64(50),
			},
		},
	}

	f, err := os.Create(filepath.Join(projDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []any{turnStart, turnEnd} {
		b, _ := json.Marshal(rec)
		f.Write(b)
		f.WriteString("\n")
	}
	f.Close()

	metrics, err := ExtractMetrics(RunManifest{InitiativeID: id})
	if err != nil {
		t.Fatalf("ExtractMetrics error: %v", err)
	}

	if metrics.InputTokens != 1100 {
		t.Errorf("InputTokens=%d, want 1100", metrics.InputTokens)
	}
	if metrics.OutputTokens != 550 {
		t.Errorf("OutputTokens=%d, want 550", metrics.OutputTokens)
	}
	if metrics.TotalTokens != metrics.InputTokens+metrics.OutputTokens {
		t.Errorf("TotalTokens=%d, want Input+Output=%d", metrics.TotalTokens, metrics.InputTokens+metrics.OutputTokens)
	}
	if metrics.CostUSD <= 0 {
		t.Errorf("CostUSD=%v, want > 0 for priced model claude-opus-4-8", metrics.CostUSD)
	}
	if metrics.WallClockSeconds != 300 {
		t.Errorf("WallClockSeconds=%v, want 300 (5 minutes)", metrics.WallClockSeconds)
	}
	if metrics.ToolCallCount != 2 {
		t.Errorf("ToolCallCount=%d, want 2", metrics.ToolCallCount)
	}
	if metrics.NTurns != 2 {
		t.Errorf("NTurns=%d, want 2", metrics.NTurns)
	}
}

func TestExtractMetrics_noSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude", "jobs"), 0755); err != nil {
		t.Fatal(err)
	}

	metrics, err := ExtractMetrics(RunManifest{InitiativeID: "no-such-initiative"})
	if err != nil {
		t.Fatalf("ExtractMetrics error: %v", err)
	}
	if metrics != (Metrics{}) {
		t.Errorf("expected zero Metrics for no sessions, got %+v", metrics)
	}
}
