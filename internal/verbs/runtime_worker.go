package verbs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

type runtimeStartRequest struct {
	Runtime      sessionruntime.Kind
	InitiativeID string
	Worktree     string
	Prompt       string
	Model        string
	ResumeID     string
}

type runtimeStartFunc func(*cli.Context, runtimeStartRequest) error

// runtimeWorkerKong is the detached process owner for one Codex turn. Phase 2
// extends this boundary with locking and post-exit mail reconciliation; the
// adapter already remains owned for the full Codex process lifetime.
type runtimeWorkerKong struct {
	Runtime      string `name:"runtime" required:"" hidden:""`
	InitiativeID string `name:"initiative" required:"" hidden:""`
	Worktree     string `name:"worktree" required:"" hidden:""`
	Prompt       string `name:"prompt" required:"" hidden:""`
	Model        string `name:"model" hidden:""`
	ResumeID     string `name:"resume-id" hidden:""`

	codex sessionruntime.Adapter `kong:"-"`
}

func (c *runtimeWorkerKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam runtime-worker: nil context")
	}
	kind, err := sessionruntime.ParseKind(c.Runtime)
	if err != nil {
		return fmt.Errorf("ateam runtime-worker: %w", err)
	}
	if kind != sessionruntime.Codex {
		return fmt.Errorf("ateam runtime-worker: runtime %q is not supported by the detached worker", kind)
	}

	adapter := c.codex
	if adapter == nil {
		adapter = sessionruntime.CodexAdapter{}
	}
	eventPath := sessionruntime.EventLogPath(ctx.Home, c.InitiativeID)
	if err := os.MkdirAll(filepath.Dir(eventPath), 0o700); err != nil {
		return fmt.Errorf("ateam runtime-worker: create runtime directory: %w", err)
	}
	events, err := os.OpenFile(eventPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("ateam runtime-worker: open event log: %w", err)
	}
	defer events.Close()

	req := sessionruntime.Request{
		InitiativeID: c.InitiativeID,
		Worktree:     c.Worktree,
		Prompt:       c.Prompt,
		Model:        c.Model,
		Events:       events,
		Stderr:       ctx.Stderr,
	}
	workerCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if c.ResumeID != "" {
		return adapter.Resume(workerCtx, req, sessionruntime.SessionRef{Runtime: kind, ID: c.ResumeID})
	}
	return adapter.Launch(workerCtx, req, func(ref sessionruntime.SessionRef) error {
		return appendSessionID(ctx, c.InitiativeID, ref.ID)
	})
}

// startRuntimeWorker starts a detached ateam process. That process blocks for
// the complete Codex child lifetime, so dispatch itself can remain a quick
// background operation without orphaning a pipe reader or JSON event parser.
func startRuntimeWorker(ctx *cli.Context, req runtimeStartRequest) error {
	if req.Runtime != sessionruntime.Codex {
		return fmt.Errorf("runtime worker: unsupported runtime %q", req.Runtime)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("runtime worker: resolve ateam executable: %w", err)
	}
	workerLog := sessionruntime.WorkerLogPath(ctx.Home, req.InitiativeID)
	if err := os.MkdirAll(filepath.Dir(workerLog), 0o700); err != nil {
		return fmt.Errorf("runtime worker: create runtime directory: %w", err)
	}
	logFile, err := os.OpenFile(workerLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("runtime worker: open worker log: %w", err)
	}
	defer logFile.Close()

	args := []string{
		"runtime-worker",
		"--runtime", string(req.Runtime),
		"--initiative", req.InitiativeID,
		"--worktree", req.Worktree,
		"--prompt", req.Prompt,
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.ResumeID != "" {
		args = append(args, "--resume-id", req.ResumeID)
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = req.Worktree
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("runtime worker: start: %w", err)
	}
	return cmd.Process.Release()
}

func printCodexControls(w io.Writer, home, initiativeID, sessionID string) {
	fmt.Fprintln(w, "\nWatch and control:")
	fmt.Fprintf(w, "  tail -f %s  # runtime events\n", sessionruntime.EventLogPath(home, initiativeID))
	fmt.Fprintf(w, "  tail -f %s  # worker diagnostics\n", sessionruntime.WorkerLogPath(home, initiativeID))
	if sessionID != "" {
		fmt.Fprintf(w, "  codex resume %s  # open the durable thread interactively\n", sessionID)
	} else {
		fmt.Fprintln(w, "  ateam show "+initiativeID+"  # thread id appears as session: after startup")
	}
}
