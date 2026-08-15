package verbs

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

const codexSubmitTimeout = 30 * time.Second

// runtimeWorkerKong is the internal Codex turn submitter retained as a hidden
// compatibility entry point. The managed app-server, not this command, owns
// the turn after acceptance.
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
		return fmt.Errorf("ateam runtime-worker: runtime %q is not supported by the app-server submitter", kind)
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
		InitiativeID:   c.InitiativeID,
		AgentTeamsHome: ctx.Home,
		Worktree:       c.Worktree,
		Prompt:         c.Prompt,
		Model:          c.Model,
		Events:         events,
		Stderr:         ctx.Stderr,
	}
	workerCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	workerCtx, timeoutCancel := context.WithTimeout(workerCtx, codexSubmitTimeout)
	defer timeoutCancel()
	if c.ResumeID != "" {
		return adapter.Resume(workerCtx, req, sessionruntime.SessionRef{Runtime: kind, ID: c.ResumeID})
	}
	return adapter.Launch(workerCtx, req, func(ref sessionruntime.SessionRef) error {
		return appendSessionID(ctx, c.InitiativeID, ref.ID)
	})
}

// startRuntimeWorker submits directly to the managed app-server. The name is
// retained while callers move to the delivery-coordinator vocabulary; unlike
// the former implementation it creates no detached ateam process.
func startRuntimeWorker(ctx *cli.Context, req runtimeStartRequest) error {
	if req.Runtime != sessionruntime.Codex {
		return fmt.Errorf("runtime worker: unsupported runtime %q", req.Runtime)
	}
	return (&runtimeWorkerKong{
		Runtime:      string(req.Runtime),
		InitiativeID: req.InitiativeID,
		Worktree:     req.Worktree,
		Prompt:       req.Prompt,
		Model:        req.Model,
		ResumeID:     req.ResumeID,
	}).Run(ctx)
}

func printCodexControls(w io.Writer, home, initiativeID, sessionID string) {
	fmt.Fprintln(w, "\nWatch and control:")
	fmt.Fprintf(w, "  tail -f %s  # runtime events\n", sessionruntime.EventLogPath(home, initiativeID))
	if sessionID != "" {
		fmt.Fprintf(w, "  codex resume %s  # open the durable thread interactively\n", sessionID)
	} else {
		fmt.Fprintln(w, "  ateam show "+initiativeID+"  # inspect the bound session: thread id")
	}
}
