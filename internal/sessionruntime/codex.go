package sessionruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// CodexAdapter owns Codex CLI command construction and JSONL event parsing.
// Executable is injectable so tests never need a paid Codex run.
type CodexAdapter struct {
	Executable string
}

func (a CodexAdapter) Kind() Kind { return Codex }

func (a CodexAdapter) Launch(ctx context.Context, req Request, sink SessionSink) error {
	if sink == nil {
		return fmt.Errorf("codex launch: nil session sink")
	}
	return a.run(ctx, req, nil, sink)
}

func (a CodexAdapter) Resume(ctx context.Context, req Request, session SessionRef) error {
	if session.Runtime != Codex {
		return fmt.Errorf("codex resume: session runtime is %q", session.Runtime)
	}
	if session.ID == "" {
		return fmt.Errorf("codex resume: empty session id")
	}
	return a.run(ctx, req, &session, nil)
}

type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
}

func (a CodexAdapter) run(ctx context.Context, req Request, resume *SessionRef, sink SessionSink) error {
	executable := a.Executable
	if executable == "" {
		executable = "codex"
	}
	if _, err := exec.LookPath(executable); err != nil {
		return fmt.Errorf("codex: executable %q not found: %w", executable, err)
	}
	if req.Worktree == "" {
		return fmt.Errorf("codex: empty worktree")
	}
	if req.Prompt == "" {
		return fmt.Errorf("codex: empty prompt")
	}

	args := []string{"exec"}
	if resume != nil {
		args = append(args, "resume")
	}
	args = append(args, "--json")
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if resume != nil {
		args = append(args, resume.ID)
	}
	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = req.Worktree
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex: stdout pipe: %w", err)
	}
	if req.Stderr != nil {
		cmd.Stderr = req.Stderr
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("codex: start: %w", err)
	}

	events := req.Events
	if events == nil {
		events = io.Discard
	}
	scanner := bufio.NewScanner(stdout)
	// Tool results can make JSONL events much larger than Scanner's default.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	sawThread := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if _, err := events.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("codex: write event log: %w", err)
		}
		var event codexEvent
		if json.Unmarshal(line, &event) != nil || event.Type != "thread.started" || event.ThreadID == "" {
			continue
		}
		if sawThread {
			continue
		}
		sawThread = true
		if resume != nil {
			if event.ThreadID != resume.ID {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return fmt.Errorf("codex resume: requested thread %s but runtime emitted %s", resume.ID, event.ThreadID)
			}
			continue
		}
		if err := sink(SessionRef{Runtime: Codex, ID: event.ThreadID}); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("codex launch: bind thread %s: %w", event.ThreadID, err)
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("codex: read events: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("codex: worker: %w", err)
	}
	if !sawThread {
		return fmt.Errorf("codex: worker exited without a thread.started event")
	}
	return nil
}
