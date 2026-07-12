package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestExtractTimeline_basic exercises the full timeline walk: min/max
// timestamp fold across ALL record types (not just assistant), assistant
// turn counting deduped by message.id, tool_use block counting, and
// subagent-transcript inclusion.
func TestExtractTimeline_basic(t *testing.T) {
	const id = "test-timeline-001"
	root := t.TempDir()
	jobsDir := filepath.Join(root, "jobs")
	projectsDir := filepath.Join(root, "projects")

	cwd := "/Users/testuser/worktrees/timeline-test"
	sessionID := "eeeeffff-0000-1111-2222-333344445555"

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

	slug := SlugifyCWD(cwd)
	projDir := filepath.Join(projectsDir, slug)
	if err := os.MkdirAll(filepath.Join(projDir, sessionID, "subagents"), 0755); err != nil {
		t.Fatal(err)
	}

	// Turn 1: earliest timestamp, one tool_use block, message.id "msg-1".
	turn1 := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-06-30T15:00:00.000Z",
		"message": map[string]any{
			"id":   "msg-1",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text"},
				{"type": "tool_use"},
			},
		},
	}
	// Turn 2: two tool_use blocks, no id (never deduped).
	turn2 := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-06-30T15:05:00.000Z",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "tool_use"},
				{"type": "tool_use"},
			},
		},
	}
	// Non-assistant record — latest timestamp; must still extend the span but
	// not count as a turn.
	userRecord := map[string]any{
		"type":      "human",
		"timestamp": "2026-06-30T15:10:00.000Z",
		"message": map[string]any{
			"role": "user",
		},
	}

	// turn1 written twice (duplicate message.id emission) to verify dedup.
	lines := []any{turn1, turn1, turn2, userRecord}
	writeJSONL(t, filepath.Join(projDir, sessionID+".jsonl"), lines, "malformed line here!!!")

	subTurn := map[string]any{
		"type":      "assistant",
		"timestamp": "2026-06-30T15:02:00.000Z",
		"message": map[string]any{
			"id":   "msg-sub",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "tool_use"},
			},
		},
	}
	writeJSONL(t, filepath.Join(projDir, sessionID, "subagents", "agent-abc.jsonl"), []any{subTurn}, "")

	tl, err := ExtractTimeline(id, jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("ExtractTimeline error: %v", err)
	}

	if tl.DRISessions != 1 {
		t.Errorf("DRISessions=%d, want 1", tl.DRISessions)
	}

	wantStart, _ := time.Parse(time.RFC3339, "2026-06-30T15:00:00.000Z")
	wantEnd, _ := time.Parse(time.RFC3339, "2026-06-30T15:10:00.000Z")
	if !tl.Start.Equal(wantStart) {
		t.Errorf("Start=%v, want %v", tl.Start, wantStart)
	}
	if !tl.End.Equal(wantEnd) {
		t.Errorf("End=%v, want %v", tl.End, wantEnd)
	}

	// turn1 deduped to one occurrence, turn2 (no id, always counted), subTurn = 3.
	if tl.NTurns != 3 {
		t.Errorf("NTurns=%d, want 3", tl.NTurns)
	}

	// turn1: 1 (counted once despite 2 lines), turn2: 2, subTurn: 1 = 4.
	// If the duplicate turn1 line were double-counted this would be 5.
	if tl.ToolCallCount != 4 {
		t.Errorf("ToolCallCount=%d, want 4 (duplicate message.id must not be recounted)", tl.ToolCallCount)
	}
}

func TestExtractTimeline_noSessions(t *testing.T) {
	root := t.TempDir()
	jobsDir := filepath.Join(root, "jobs")
	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(jobsDir, 0755); err != nil {
		t.Fatal(err)
	}

	tl, err := ExtractTimeline("no-such-initiative", jobsDir, projectsDir)
	if err != nil {
		t.Fatalf("ExtractTimeline error: %v", err)
	}
	if tl.DRISessions != 0 {
		t.Errorf("DRISessions=%d, want 0", tl.DRISessions)
	}
	if !tl.Start.IsZero() || !tl.End.IsZero() {
		t.Errorf("Start/End should be zero for no sessions, got Start=%v End=%v", tl.Start, tl.End)
	}
	if tl.ToolCallCount != 0 || tl.NTurns != 0 {
		t.Errorf("ToolCallCount/NTurns should be 0, got %d/%d", tl.ToolCallCount, tl.NTurns)
	}
}

func TestExtractTimeline_missingJobsDir(t *testing.T) {
	root := t.TempDir()
	_, err := ExtractTimeline("anything", filepath.Join(root, "nonexistent"), filepath.Join(root, "projects"))
	if err != nil {
		t.Errorf("missing jobsDir should return nil error (no jobs = zero Timeline), got: %v", err)
	}
}
