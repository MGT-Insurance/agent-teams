package verbs

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cost"
)

const (
	// cloudBootDefaultStall is how long the lead may write nothing before
	// cloud-boot parks the run (contract dsp-7gr.2, item 2).
	cloudBootDefaultStall = 90 * time.Minute
	// cloudBootStallEvalMax bounds how often pollLoop re-reads the transcripts
	// and task-output files.
	cloudBootStallEvalMax = 5 * time.Minute
	// cloudBootTranscriptTailBytes is how much of each transcript's tail the
	// scan reads.
	cloudBootTranscriptTailBytes = 256 * 1024
)

// cloudActivity is the newest evidence that the lead session is doing
// something. Zero means none was found.
type cloudActivity struct {
	// Transcript is the newest content-typed record timestamp across the lead
	// transcript and its subagent transcripts.
	Transcript time.Time
	// TaskOutput is the newest mtime of any background-task output file.
	TaskOutput time.Time
}

// defaultCloudActivity reads the lead's transcript, its subagent transcripts,
// and its background-task output files. Missing files are normal and yield
// zero times.
func defaultCloudActivity(cwd, sid string) cloudActivity {
	slug := cost.SlugifyCWD(cwd)
	var act cloudActivity
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, ".claude", "projects", slug)
		paths := []string{filepath.Join(dir, sid+".jsonl")}
		if subs, err := filepath.Glob(filepath.Join(dir, sid, "subagents", "*.jsonl")); err == nil {
			paths = append(paths, subs...)
		}
		for _, p := range paths {
			if ts := newestTranscriptRecord(p); ts.After(act.Transcript) {
				act.Transcript = ts
			}
		}
	}
	outs, err := filepath.Glob(filepath.Join("/tmp", "claude-"+strconv.Itoa(os.Getuid()), slug, sid, "tasks", "*.output"))
	if err == nil {
		for _, p := range outs {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(act.TaskOutput) {
				act.TaskOutput = fi.ModTime()
			}
		}
	}
	return act
}

// newestTranscriptRecord returns the timestamp of the last content-typed
// record in the final cloudBootTranscriptTailBytes of path, or zero. When the
// tail holds no complete record because the last record is larger than the
// window, it returns the file's mtime: the file was written, and the size
// shows the writer is a working session.
func newestTranscriptRecord(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return time.Time{}
	}
	off := int64(0)
	if fi.Size() > cloudBootTranscriptTailBytes {
		off = fi.Size() - cloudBootTranscriptTailBytes
	}
	buf := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return time.Time{}
	}
	lines := bytes.Split(buf, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if len(bytes.TrimSpace(lines[i])) == 0 {
			continue
		}
		var rec transcriptRecord
		if json.Unmarshal(lines[i], &rec) != nil || !isContentTypedWorkTurn(rec) {
			continue // also skips a partial first line of the tail window
		}
		ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil {
			continue
		}
		return ts
	}
	if off > 0 {
		// The window ends inside one oversized record, so nothing parsed.
		return fi.ModTime()
	}
	return time.Time{}
}

// stallClock tracks the run's progress clock for pollLoop. Progress is the
// newest of the loop start, the transcript activity, and the task-output
// activity; status is not an input.
type stallClock struct {
	start    time.Time
	interval time.Duration
	lastEval time.Time
	act      cloudActivity
}

func newStallClock(start time.Time, stall time.Duration) *stallClock {
	iv := cloudBootStallEvalMax
	if half := stall / 2; half < iv {
		iv = half
	}
	return &stallClock{start: start, interval: iv, lastEval: start}
}

// refresh re-reads activity when at least interval has passed since the last
// read. Each field keeps the newer of the old and new reading, so a transient
// zero (a file that is briefly unreadable) never moves the clock backward.
func (s *stallClock) refresh(now time.Time, read func() cloudActivity) {
	if now.Sub(s.lastEval) < s.interval {
		return
	}
	s.lastEval = now
	got := read()
	if got.Transcript.After(s.act.Transcript) {
		s.act.Transcript = got.Transcript
	}
	if got.TaskOutput.After(s.act.TaskOutput) {
		s.act.TaskOutput = got.TaskOutput
	}
}

func (s *stallClock) progress() time.Time {
	p := s.start
	if s.act.Transcript.After(p) {
		p = s.act.Transcript
	}
	if s.act.TaskOutput.After(p) {
		p = s.act.TaskOutput
	}
	return p
}

func (s *stallClock) silent(now time.Time) time.Duration {
	return now.Sub(s.progress()).Round(time.Second)
}

func (s *stallClock) taskOutAge(now time.Time) string {
	if s.act.TaskOutput.IsZero() {
		return "none"
	}
	return now.Sub(s.act.TaskOutput).Round(time.Second).String()
}
