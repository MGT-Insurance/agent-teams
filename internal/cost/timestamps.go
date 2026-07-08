package cost

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Timeline holds the wall-clock span and turn/tool-call counts for one
// initiative, derived from the same DRI session discovery Attribute uses.
type Timeline struct {
	InitiativeID  string
	DRISessions   int
	Start         time.Time // zero if no timestamped records were found
	End           time.Time // zero if no timestamped records were found
	ToolCallCount int
	NTurns        int
}

// timelineRecordJSON is the subset of a .jsonl line needed for timeline
// extraction. Timestamp is read as a raw string (not time.Time) so a
// missing/malformed timestamp only drops the timestamp fold for that line,
// not the whole record's turn/tool-call accounting.
type timelineRecordJSON struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
		} `json:"content"`
	} `json:"message"`
}

// parseTimelineJSONL scans a .jsonl file line by line, folding every record's
// timestamp into tl.Start/tl.End (regardless of record type) and counting
// assistant turns / tool_use blocks. seen dedupes assistant turns by
// message.id across main + subagent transcripts, mirroring parseJSONL's
// rationale: each turn is emitted as 2-5 duplicate lines sharing one
// message.id, and records with no id cannot be deduped so are always counted.
func parseTimelineJSONL(path string, tl *Timeline, seen map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const maxLine = 64 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxLine)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec timelineRecordJSON
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // malformed line — skip
		}

		if rec.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339, rec.Timestamp); err == nil {
				if tl.Start.IsZero() || ts.Before(tl.Start) {
					tl.Start = ts
				}
				if ts.After(tl.End) {
					tl.End = ts
				}
			}
		}

		if rec.Type != "assistant" && rec.Message.Role != "assistant" {
			continue
		}
		if id := rec.Message.ID; id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		tl.NTurns++
		for _, block := range rec.Message.Content {
			if block.Type == "tool_use" {
				tl.ToolCallCount++
			}
		}
	}
	return scanner.Err()
}

// scanTimeline reads the main transcript and all subagent transcripts for one
// session, folding them into tl. Missing files/dirs are skipped silently,
// mirroring collectTranscripts.
func scanTimeline(projectDir, sessionID string, tl *Timeline, seen map[string]bool) error {
	mainPath := filepath.Join(projectDir, sessionID+".jsonl")
	if err := parseTimelineJSONL(mainPath, tl, seen); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("scanTimeline %s: %w", mainPath, err)
		}
	}

	subagentsDir := filepath.Join(projectDir, sessionID, "subagents")
	entries, err := os.ReadDir(subagentsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no subagents dir — normal for many sessions
		}
		return fmt.Errorf("scanTimeline ReadDir %s: %w", subagentsDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		p := filepath.Join(subagentsDir, name)
		if err := parseTimelineJSONL(p, tl, seen); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // disappeared between ReadDir and open — skip
			}
			return fmt.Errorf("scanTimeline %s: %w", p, err)
		}
	}
	return nil
}

// ExtractTimeline reconstructs the wall-clock span and turn/tool-call counts
// for one initiative from local Claude transcript data. jobsDir is typically
// ~/.claude/jobs; projectsDir is ~/.claude/projects. Both are parameters (not
// hardcoded) so tests can inject temp directories, matching Attribute.
// Returns a zero Timeline (not an error) when no DRI sessions are found.
func ExtractTimeline(initiativeID, jobsDir, projectsDir string) (Timeline, error) {
	sessions, err := discoverSessions(initiativeID, jobsDir)
	if err != nil {
		return Timeline{InitiativeID: initiativeID}, err
	}

	tl := Timeline{InitiativeID: initiativeID, DRISessions: len(sessions)}
	seen := make(map[string]bool) // dedupe assistant turns by message.id across all transcripts
	for _, s := range sessions {
		slug := SlugifyCWD(s.cwd)
		projectDir := filepath.Join(projectsDir, slug)
		if err := scanTimeline(projectDir, s.sessionID, &tl, seen); err != nil {
			return Timeline{InitiativeID: initiativeID}, err
		}
	}
	return tl, nil
}
