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
)

// ModelUsage aggregates token counts for one distinct model string.
type ModelUsage struct {
	Model string
	TokenUsage
}

// Report holds the complete attribution result for one initiative.
type Report struct {
	InitiativeID string
	DRISessions  int
	ByModel      []ModelUsage // one entry per distinct model; unsorted
}

// SlugifyCWD converts an absolute cwd path to the slug that Claude Code uses
// when naming ~/.claude/projects/<slug>. Claude Code replaces each
// non-alphanumeric byte char-by-char with '-'; runs are NOT collapsed; leading
// '/' becomes '-'; '.' becomes '-' (so "/.agent-teams" → "--agent-teams").
// No lowercasing. No length cap. This is NOT gitutil.Slugify (which collapses
// runs, lowercases, trims, and caps at 50 chars).
//
// Byte-wise per the frozen contract (agent-teams-9er): observed ~/.claude/projects
// dir names are produced byte-by-byte, not rune-by-rune. Identical for ASCII
// (all real paths); diverges only for multi-byte UTF-8 sequences.
func SlugifyCWD(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for i := 0; i < len(cwd); i++ {
		c := cwd[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// stateJSON is the subset of ~/.claude/jobs/<id>/state.json we need.
type stateJSON struct {
	Intent    string `json:"intent"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
}

// driSession is one discovered DRI session.
type driSession struct {
	sessionID string
	cwd       string
}

// discoverSessions walks jobsDir/*/state.json and returns sessions whose intent
// matches "/dri <initiativeID>". Deduplication by sessionId handles resume
// cycles where multiple job dirs share the same session.
func discoverSessions(initiativeID, jobsDir string) ([]driSession, error) {
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	prefix := "/dri " + initiativeID
	seen := make(map[string]bool)
	var sessions []driSession

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		statePath := filepath.Join(jobsDir, entry.Name(), "state.json")
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue // missing or unreadable state.json — skip silently
		}
		var s stateJSON
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		// Match intent: must be "/dri <id>" or "/dri <id> <more text>".
		// The prefix check ensures the id token is matched exactly.
		if s.Intent != prefix && !strings.HasPrefix(s.Intent, prefix+" ") {
			continue
		}
		if s.SessionID == "" || seen[s.SessionID] {
			continue
		}
		seen[s.SessionID] = true
		sessions = append(sessions, driSession{sessionID: s.SessionID, cwd: s.CWD})
	}
	return sessions, nil
}

// cacheCreationJSON is the nested cache_creation split inside message.usage.
type cacheCreationJSON struct {
	Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
}

// msgUsageJSON mirrors message.usage in an assistant record.
type msgUsageJSON struct {
	InputTokens              int64             `json:"input_tokens"`
	OutputTokens             int64             `json:"output_tokens"`
	CacheCreationInputTokens int64             `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64             `json:"cache_read_input_tokens"`
	CacheCreation            cacheCreationJSON `json:"cache_creation"`
}

// recordJSON is the top-level shape of one .jsonl line.
type recordJSON struct {
	Type    string `json:"type"`
	Message struct {
		Id    string       `json:"id"`
		Role  string       `json:"role"`
		Model string       `json:"model"`
		Usage msgUsageJSON `json:"usage"`
	} `json:"message"`
}

// parseJSONL scans a .jsonl file line by line. For each assistant record it
// adds the token counts into acc, keyed by model string. Non-assistant lines,
// blank lines, and malformed lines are silently skipped.
//
// seen tracks message.id values already accumulated across this Attribute run
// (spanning main + all subagent transcripts). Each transcript turn is emitted
// as 2-5 duplicate JSONL lines sharing the same message.id; keeping only the
// first occurrence per id is lossless because duplicates carry identical usage.
// Records with an empty message.id cannot be deduped and are always counted.
func parseJSONL(path string, acc map[string]*TokenUsage, seen map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Transcripts can have large lines (embedded content). Use a 64 MB buffer.
	const maxLine = 64 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxLine)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec recordJSON
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // malformed line — skip
		}
		// Accept records identified as assistant at the top level.
		// (message.role=="assistant" is a redundant check but kept as belt+suspenders.)
		if rec.Type != "assistant" && rec.Message.Role != "assistant" {
			continue
		}
		model := rec.Message.Model
		if model == "" {
			continue
		}
		// Dedupe by message.id across main + subagent transcripts. Empty id
		// means the record cannot be keyed, so it is always accumulated.
		if id := rec.Message.Id; id != "" {
			if seen[id] {
				continue
			}
			seen[id] = true
		}
		u := acc[model]
		if u == nil {
			u = &TokenUsage{}
			acc[model] = u
		}
		u.InputTokens += rec.Message.Usage.InputTokens
		u.OutputTokens += rec.Message.Usage.OutputTokens
		u.CacheCreationInputTokens += rec.Message.Usage.CacheCreationInputTokens
		u.CacheReadInputTokens += rec.Message.Usage.CacheReadInputTokens
		u.CacheCreation5mTokens += rec.Message.Usage.CacheCreation.Ephemeral5m
		u.CacheCreation1hTokens += rec.Message.Usage.CacheCreation.Ephemeral1h
	}
	return scanner.Err()
}

// collectTranscripts reads the main transcript and all subagent transcripts for
// one session into acc. Missing files and directories are skipped silently.
// Any non-missing-file scanner error is returned (scanner buffer overflow, I/O
// error, etc.) so the caller can surface data-loss conditions.
// seen is the per-Attribute-run dedupe set threaded from Attribute; see parseJSONL.
func collectTranscripts(projectDir, sessionID string, acc map[string]*TokenUsage, seen map[string]bool) error {
	// Main transcript.
	mainPath := filepath.Join(projectDir, sessionID+".jsonl")
	if err := parseJSONL(mainPath, acc, seen); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Missing transcript is normal (session may have no file yet).
		} else {
			return fmt.Errorf("collectTranscripts %s: %w", mainPath, err)
		}
	}

	// Subagent transcripts: <projectDir>/<sessionID>/subagents/agent-*.jsonl
	subagentsDir := filepath.Join(projectDir, sessionID, "subagents")
	entries, err := os.ReadDir(subagentsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no subagents dir — normal for many sessions
		}
		return fmt.Errorf("collectTranscripts ReadDir %s: %w", subagentsDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		p := filepath.Join(subagentsDir, name)
		if err := parseJSONL(p, acc, seen); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // disappeared between ReadDir and open — skip
			}
			return fmt.Errorf("collectTranscripts %s: %w", p, err)
		}
	}
	return nil
}

// Attribute reconstructs token usage for one initiative from local Claude data.
// jobsDir is typically ~/.claude/jobs; projectsDir is ~/.claude/projects.
// Both are parameters (not hardcoded) so tests can inject temp directories.
// Returns a zero Report (not an error) when no DRI sessions are found.
func Attribute(initiativeID, jobsDir, projectsDir string) (Report, error) {
	sessions, err := discoverSessions(initiativeID, jobsDir)
	if err != nil {
		return Report{InitiativeID: initiativeID}, err
	}

	acc := make(map[string]*TokenUsage)
	seen := make(map[string]bool) // dedupe assistant turns by message.id across all transcripts
	for _, s := range sessions {
		slug := SlugifyCWD(s.cwd)
		projectDir := filepath.Join(projectsDir, slug)
		if err := collectTranscripts(projectDir, s.sessionID, acc, seen); err != nil {
			return Report{InitiativeID: initiativeID}, err
		}
	}

	report := Report{
		InitiativeID: initiativeID,
		DRISessions:  len(sessions),
	}
	for model, u := range acc {
		report.ByModel = append(report.ByModel, ModelUsage{Model: model, TokenUsage: *u})
	}
	return report, nil
}
