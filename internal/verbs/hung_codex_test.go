package verbs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withCodexSessionsDir points codexSessionsDir at dir for the duration of
// the test, restoring the prior value on cleanup.
func withCodexSessionsDir(t *testing.T, dir string) {
	t.Helper()
	old := codexSessionsDir
	codexSessionsDir = dir
	t.Cleanup(func() { codexSessionsDir = old })
}

// installCodexFixture copies the testdata fixture at fixtureName into
// <sessionsDir>/<yyyy>/<mm>/<dd>/rollout-<stamp>-<threadID>.jsonl — the real
// on-disk shape defaultCodexRolloutRead globs for.
func installCodexFixture(t *testing.T, sessionsDir, fixtureName, threadID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixtureName))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixtureName, err)
	}
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir day dir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2026-08-24T10-26-52-"+threadID+".jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}
	return path
}

// ── defaultCodexRolloutRead: fixtures i-iv ───────────────────────────────────

func TestDefaultCodexRolloutRead_Fixtures(t *testing.T) {
	sessionsDir := t.TempDir()
	withCodexSessionsDir(t, sessionsDir)

	tests := []struct {
		name       string
		fixture    string
		threadID   string
		wantKind   string
		wantTime   string // RFC3339
		wantExited bool
	}{
		{
			name:       "clean task_complete tail",
			fixture:    "codex_rollout_clean_task_complete_tail.jsonl",
			threadID:   "thread-clean",
			wantKind:   "task_complete",
			wantTime:   "2026-08-24T15:34:20.098Z",
			wantExited: false,
		},
		{
			name:       "Exited. tail",
			fixture:    "codex_rollout_exited_tail.jsonl",
			threadID:   "thread-exited",
			wantKind:   "task_complete",
			wantTime:   "2026-08-20T14:21:31.406Z",
			wantExited: true,
		},
		{
			name:       "trailing function_call (wait_agent)",
			fixture:    "codex_rollout_function_call_tail.jsonl",
			threadID:   "thread-fcall",
			wantKind:   "function_call",
			wantTime:   "2026-08-24T15:30:40.811Z",
			wantExited: false,
		},
		{
			name:       "mid-turn reasoning tail",
			fixture:    "codex_rollout_mid_turn_reasoning_tail.jsonl",
			threadID:   "thread-reasoning",
			wantKind:   "reasoning",
			wantTime:   "2026-08-24T15:27:12.998Z",
			wantExited: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installCodexFixture(t, sessionsDir, tc.fixture, tc.threadID)

			kind, ts, exited, found := defaultCodexRolloutRead(tc.threadID)
			if !found {
				t.Fatal("found = false, want true")
			}
			if kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", kind, tc.wantKind)
			}
			wantTS, err := time.Parse(time.RFC3339, tc.wantTime)
			if err != nil {
				t.Fatalf("bad test setup: %v", err)
			}
			if !ts.Equal(wantTS) {
				t.Errorf("time = %v, want %v", ts, wantTS)
			}
			if exited != tc.wantExited {
				t.Errorf("exited = %v, want %v", exited, tc.wantExited)
			}
		})
	}
}

// ── fixture v: file absent ───────────────────────────────────────────────────

func TestDefaultCodexRolloutRead_FileAbsent(t *testing.T) {
	withCodexSessionsDir(t, t.TempDir())

	_, _, _, found := defaultCodexRolloutRead("no-such-thread")
	if found {
		t.Error("found should be false when no rollout file matches")
	}
}

func TestDefaultCodexRolloutRead_EmptyThreadID(t *testing.T) {
	withCodexSessionsDir(t, t.TempDir())

	_, _, _, found := defaultCodexRolloutRead("")
	if found {
		t.Error("found should be false for an empty threadID")
	}
}

func TestDefaultCodexRolloutRead_UnresolvableHome(t *testing.T) {
	withCodexSessionsDir(t, "")

	_, _, _, found := defaultCodexRolloutRead("any-thread")
	if found {
		t.Error("found should be false when codexSessionsDir is empty")
	}
}

// ── malformed/degenerate content ─────────────────────────────────────────────

func TestDefaultCodexRolloutRead_MalformedLastLine(t *testing.T) {
	sessionsDir := t.TempDir()
	withCodexSessionsDir(t, sessionsDir)
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2026-08-24T10-26-52-thread-bad.jsonl")
	content := `{"timestamp":"2026-08-24T15:34:20.091Z","type":"event_msg","payload":{"type":"token_count"}}
{this is not valid json`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, _, found := defaultCodexRolloutRead("thread-bad")
	if found {
		t.Error("found should be false when the last line is malformed JSON")
	}
}

func TestDefaultCodexRolloutRead_UnparseableTimestamp(t *testing.T) {
	sessionsDir := t.TempDir()
	withCodexSessionsDir(t, sessionsDir)
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2026-08-24T10-26-52-thread-badts.jsonl")
	content := `{"timestamp":"not-a-timestamp","type":"event_msg","payload":{"type":"task_complete"}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, _, found := defaultCodexRolloutRead("thread-badts")
	if found {
		t.Error("found should be false when the last line's timestamp is unparseable")
	}
}

func TestDefaultCodexRolloutRead_EmptyFile(t *testing.T) {
	sessionsDir := t.TempDir()
	withCodexSessionsDir(t, sessionsDir)
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2026-08-24T10-26-52-thread-empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, _, found := defaultCodexRolloutRead("thread-empty")
	if found {
		t.Error("found should be false for an empty rollout file")
	}
}

func TestDefaultCodexRolloutRead_TrailingBlankLineSkipped(t *testing.T) {
	sessionsDir := t.TempDir()
	withCodexSessionsDir(t, sessionsDir)
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2026-08-24T10-26-52-thread-trailblank.jsonl")
	// A trailing blank line after the last real event, as most real rollout
	// files have.
	content := `{"timestamp":"2026-08-24T15:34:20.091Z","type":"event_msg","payload":{"type":"token_count"}}
{"timestamp":"2026-08-24T15:34:20.098Z","type":"event_msg","payload":{"type":"task_complete"}}

`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	kind, _, _, found := defaultCodexRolloutRead("thread-trailblank")
	if !found {
		t.Fatal("found should be true")
	}
	if kind != "task_complete" {
		t.Errorf("kind = %q, want task_complete (trailing blank line should be skipped)", kind)
	}
}

// ── multiple candidate files: newest by mtime wins ───────────────────────────

func TestFindCodexRolloutFile_MultipleMatches_NewestWins(t *testing.T) {
	sessionsDir := t.TempDir()
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	older := filepath.Join(dayDir, "rollout-2026-08-24T08-00-00-thread-dup.jsonl")
	newer := filepath.Join(dayDir, "rollout-2026-08-24T10-00-00-thread-dup.jsonl")
	if err := os.WriteFile(older, []byte("old"), 0o644); err != nil {
		t.Fatalf("write older: %v", err)
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes older: %v", err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0o644); err != nil {
		t.Fatalf("write newer: %v", err)
	}

	got, err := findCodexRolloutFile(sessionsDir, "thread-dup")
	if err != nil {
		t.Fatalf("findCodexRolloutFile: %v", err)
	}
	if got != newer {
		t.Errorf("findCodexRolloutFile = %q, want newest match %q", got, newer)
	}
}

// ── tailLines: bounded read, does not slurp the whole file ──────────────────

// countingReaderAt wraps an io.ReaderAt and tracks the total bytes requested
// across all ReadAt calls, so a test can assert a bounded read pattern
// without depending on timing.
type countingReaderAt struct {
	io.ReaderAt
	totalRequested int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.totalRequested += int64(len(p))
	return c.ReaderAt.ReadAt(p, off)
}

func TestTailLines_LargeFile_DoesNotSlurpWholeFile(t *testing.T) {
	// Build a large synthetic file (well beyond codexTailReadChunkBytes) out
	// of small, uniform JSONL-shaped lines, ending in a distinguishable tail.
	var buf bytes.Buffer
	for i := 0; i < 200000; i++ {
		fmt.Fprintf(&buf, `{"timestamp":"2026-08-24T00:00:00.000Z","type":"event_msg","payload":{"type":"filler-%d"}}`+"\n", i)
	}
	buf.WriteString(`{"timestamp":"2026-08-24T15:34:20.098Z","type":"event_msg","payload":{"type":"task_complete"}}` + "\n")

	data := buf.Bytes()
	if int64(len(data)) < 10*codexTailReadChunkBytes {
		t.Fatalf("test setup: synthetic file too small to prove a bounded read (%d bytes)", len(data))
	}

	counting := &countingReaderAt{ReaderAt: bytes.NewReader(data)}
	lines, err := tailLines(counting, int64(len(data)), codexTailEventCount)
	if err != nil {
		t.Fatalf("tailLines: %v", err)
	}
	if len(lines) != codexTailEventCount {
		t.Fatalf("got %d lines, want %d", len(lines), codexTailEventCount)
	}
	last, ok := parseCodexRolloutLine(lines[len(lines)-1])
	if !ok || last.Payload.Type != "task_complete" {
		t.Errorf("last line = %+v, ok=%v, want task_complete", last, ok)
	}

	// The whole point of a tail read: total bytes requested across all
	// ReadAt calls must stay a small fraction of the file size, not grow
	// with it.
	if counting.totalRequested > 4*codexTailReadChunkBytes {
		t.Errorf("tailLines requested %d bytes against a %d-byte file (bounded-read budget exceeded)", counting.totalRequested, len(data))
	}
}

func TestTailLines_FileSmallerThanMaxLines(t *testing.T) {
	data := []byte("{\"timestamp\":\"2026-08-24T00:00:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\"}}\n" +
		"{\"timestamp\":\"2026-08-24T00:00:01Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}\n")
	lines, err := tailLines(bytes.NewReader(data), int64(len(data)), codexTailEventCount)
	if err != nil {
		t.Fatalf("tailLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (file has fewer lines than maxLines)", len(lines))
	}
}

// ── integration: defaultCodexRolloutRead against a real multi-MB-scale file ──

func TestDefaultCodexRolloutRead_LargeFile(t *testing.T) {
	sessionsDir := t.TempDir()
	withCodexSessionsDir(t, sessionsDir)
	dayDir := filepath.Join(sessionsDir, "2026", "08", "24")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-2026-08-24T10-26-52-thread-large.jsonl")

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 200000; i++ {
		fmt.Fprintf(f, `{"timestamp":"2026-08-24T00:00:00.000Z","type":"event_msg","payload":{"type":"filler-%d"}}`+"\n", i)
	}
	fmt.Fprintln(f, `{"timestamp":"2026-08-24T15:34:20.098Z","type":"event_msg","payload":{"type":"task_complete"}}`)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() < 5*1024*1024 {
		t.Fatalf("test setup: file too small to represent the observed multi-MB case (%d bytes)", info.Size())
	}

	kind, ts, exited, found := defaultCodexRolloutRead("thread-large")
	if !found {
		t.Fatal("found should be true")
	}
	if kind != "task_complete" {
		t.Errorf("kind = %q, want task_complete", kind)
	}
	wantTS, _ := time.Parse(time.RFC3339, "2026-08-24T15:34:20.098Z")
	if !ts.Equal(wantTS) {
		t.Errorf("time = %v, want %v", ts, wantTS)
	}
	if exited {
		t.Error("exited should be false")
	}
}
