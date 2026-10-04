// Package verbs — hung_codex.go implements defaultCodexRolloutRead, the real
// codexRolloutReadFunc (hung_scan.go) that classifyCodexLiveness reads codex
// thread liveness through.
//
// A codex DRI's activity lives in its rollout JSONL file under
// ~/.codex/sessions, not in `claude agents --all --json` — this file is the
// seam's only implementation of talking to that filesystem. It is pure I/O:
// every age/threshold decision stays in classifyCodexLiveness (hung_scan.go)
// so that logic remains unit-testable with fake times and no filesystem.
package verbs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// codexTailEventCount (K) is how many trailing events defaultCodexRolloutRead
// scans for the "Exited." agent_message marker. "Exited." sits a few events
// before the terminal task_complete (agent-teams-n4bv.1's captured format:
// user_message "exit" -> agent_message "Exited." -> message -> token_count ->
// task_complete), so K=8 leaves comfortable margin.
const codexTailEventCount = 8

// codexTailReadChunkBytes is the initial (and, for the overwhelming majority
// of rollout files, only) chunk read backward from EOF when hunting for the
// trailing codexTailEventCount lines. JSONL rollout lines are commonly a few
// hundred bytes to a few KB; 64KiB comfortably holds the last several dozen
// lines without ever approaching the file's full size (observed up to
// several MB), which is the whole point of a tail read.
const codexTailReadChunkBytes = 64 * 1024

// codexSessionsDir is the root codex rollout files live under:
// <home>/.codex/sessions/<yyyy>/<mm>/<dd>/rollout-*.jsonl. A package var
// (not a const) so tests can point it at a temp directory.
var codexSessionsDir = defaultCodexSessionsDir()

func defaultCodexSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// codexRolloutLine is the frozen top-level shape of one rollout JSONL line
// (agent-teams-n4bv.1's captured production format): {"timestamp":
// RFC3339-with-millis, "type": ..., "payload": {"type": ..., "message": ...,
// ...}}. Event kind is payload.type, not the top-level type. Only the
// sub-fields this reader needs are modeled; unknown payload keys are ignored
// by encoding/json, and a payload shape that lacks "type"/"message" simply
// zero-values them (e.g. world_state/turn_context lines, which carry no
// payload.type at all).
type codexRolloutLine struct {
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"payload"`
}

// defaultCodexRolloutRead is the real codexRolloutReadFunc (hung_scan.go):
// locate threadID's rollout file under codexSessionsDir, tail-read its last
// event, and report whether a trailing "Exited." agent_message marks a
// genuine exit.
//
// found=false (all-zero return) whenever: home/codexSessionsDir cannot be
// resolved, no rollout file matches threadID, the file is empty, or the
// final line is malformed/unparseable. classifyCodexLiveness maps
// not-found -> DEAD (mirrors "no live session").
func defaultCodexRolloutRead(threadID string) (lastEventKind string, lastEventTime time.Time, exitedMarker bool, found bool) {
	if codexSessionsDir == "" || threadID == "" {
		return "", time.Time{}, false, false
	}

	path, err := findCodexRolloutFile(codexSessionsDir, threadID)
	if err != nil || path == "" {
		return "", time.Time{}, false, false
	}

	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, false, false
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return "", time.Time{}, false, false
	}

	lines, err := tailLines(f, info.Size(), codexTailEventCount)
	if err != nil || len(lines) == 0 {
		return "", time.Time{}, false, false
	}

	// The literal last line is the last event. Per spec: malformed/
	// unparseable -> found=false entirely (never fall back to an earlier
	// line — a corrupt tail is not trustworthy evidence either way).
	last, ok := parseCodexRolloutLine(lines[len(lines)-1])
	if !ok {
		return "", time.Time{}, false, false
	}
	ts, err := time.Parse(time.RFC3339, last.Timestamp)
	if err != nil {
		return "", time.Time{}, false, false
	}

	exited := false
	for _, raw := range lines {
		l, ok := parseCodexRolloutLine(raw)
		if !ok {
			continue // best-effort: an unparseable non-last line just isn't a witness
		}
		if l.Payload.Type == "agent_message" && strings.TrimSpace(l.Payload.Message) == "Exited." {
			exited = true
			break
		}
	}

	return last.Payload.Type, ts, exited, true
}

// findCodexRolloutFile globs <sessionsDir>/*/*/*/rollout-*-<threadID>.jsonl
// (yyyy/mm/dd nesting) and returns the newest match by mtime. Empty string,
// nil error when nothing matches — that's the normal "no rollout for this
// thread" case, not a failure.
func findCodexRolloutFile(sessionsDir, threadID string) (string, error) {
	pattern := filepath.Join(sessionsDir, "*", "*", "*", "rollout-*-"+threadID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) == 1 {
		return matches[0], nil
	}

	// Should not happen — threadID is unique — but pick the newest by mtime
	// rather than guessing from lexical/glob order.
	sort.Slice(matches, func(i, j int) bool {
		ti, erri := os.Stat(matches[i])
		tj, errj := os.Stat(matches[j])
		if erri != nil || errj != nil {
			return false
		}
		return ti.ModTime().After(tj.ModTime())
	})
	return matches[0], nil
}

// parseCodexRolloutLine unmarshals one JSONL line. Empty/whitespace-only
// input (the trailing blank line most files end with) is reported as
// !ok rather than an error, so callers can skip it uniformly.
func parseCodexRolloutLine(raw string) (codexRolloutLine, bool) {
	raw = strings.TrimRight(raw, "\r")
	if strings.TrimSpace(raw) == "" {
		return codexRolloutLine{}, false
	}
	var l codexRolloutLine
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return codexRolloutLine{}, false
	}
	return l, true
}

// tailLines returns up to maxLines complete, non-empty trailing lines from
// ra (a size-byte readable), without reading the whole file when the tail
// fits comfortably near EOF — which it does for the overwhelming majority of
// rollout files, since JSONL lines are small relative to
// codexTailReadChunkBytes. It grows the read window (doubling) only if the
// first chunk doesn't contain enough newline-delimited lines, and never
// reads past the start of the file.
//
// A trailing blank line (most rollout files end with one) is dropped as
// part of "non-empty" rather than counted toward maxLines.
func tailLines(ra io.ReaderAt, size int64, maxLines int) ([]string, error) {
	if size <= 0 || maxLines <= 0 {
		return nil, nil
	}

	chunk := int64(codexTailReadChunkBytes)
	if chunk > size {
		chunk = size
	}

	for {
		start := size - chunk
		if start < 0 {
			start = 0
		}
		buf := make([]byte, size-start)
		n, err := ra.ReadAt(buf, start)
		if err != nil && err != io.EOF {
			return nil, err
		}
		buf = buf[:n]

		lines := splitNonEmptyLines(buf)
		// If we already hold the whole file, this is the best we'll get,
		// however many lines that is.
		if start == 0 || len(lines) > maxLines {
			if len(lines) > maxLines {
				lines = lines[len(lines)-maxLines:]
			}
			return lines, nil
		}

		// Not enough lines yet and there's more file before start: grow the
		// window and try again. Guard against overflow/runaway growth by
		// capping at size (handled by the start<0 clamp above).
		chunk *= 2
		if chunk > size {
			chunk = size
		}
	}
}

// splitNonEmptyLines splits buf on '\n' and drops empty/whitespace-only
// entries (a leading partial line from a mid-file chunk boundary is
// naturally excluded by the caller re-deriving line count, not here — this
// helper only trims trailing/blank noise, it does not attempt to detect a
// truncated first line, which the tailLines chunk-growth loop already
// avoids by only trusting the result once it holds more lines than needed
// or the whole file).
func splitNonEmptyLines(buf []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var out []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
