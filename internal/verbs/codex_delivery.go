package verbs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

const codexMailPrompt = "You have unread agent-teams mail. Run `ateam mail inbox` now and act on every message."

var errCodexDeliveryBusy = errors.New("codex delivery already in progress")

type codexWakeFunc func(*cli.Context, bd.Issue) error

type codexDeliveryLock struct {
	file *os.File
}

func acquireCodexDeliveryLock(home, initiativeID string) (*codexDeliveryLock, error) {
	dir := filepath.Join(home, "runtimes", string(sessionruntime.Codex))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create Codex runtime directory: %w", err)
	}
	path := filepath.Join(dir, initiativeID+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Codex delivery lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errCodexDeliveryBusy
		}
		return nil, fmt.Errorf("acquire Codex delivery lock: %w", err)
	}
	return &codexDeliveryLock{file: file}, nil
}

func (l *codexDeliveryLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return fmt.Errorf("release Codex delivery lock: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Codex delivery lock: %w", closeErr)
	}
	return nil
}

func defaultCodexWake(ctx *cli.Context, recipient bd.Issue) error {
	return runCodexWake(ctx, recipient, codexMailPrompt, "", startRuntimeWorker)
}

func runCodexWake(ctx *cli.Context, recipient bd.Issue, prompt, model string, submit runtimeStartFunc) error {
	fields := initiative.Of(recipient)
	if len(fields.Sessions) == 0 {
		return fmt.Errorf("Codex initiative %s has no bound session thread", recipient.ID)
	}
	lock, err := acquireCodexDeliveryLock(ctx.Home, recipient.ID)
	if err != nil {
		return err
	}
	defer lock.Close()

	if prompt == "" {
		prompt = codexMailPrompt
	}
	return submit(ctx, runtimeStartRequest{
		Runtime:      sessionruntime.Codex,
		InitiativeID: recipient.ID,
		Worktree:     fields.Worktree,
		Prompt:       prompt,
		Model:        model,
		ResumeID:     fields.Sessions[len(fields.Sessions)-1],
	})
}

// reconcileCodexInboxDoorbell clears the wake edge only after inbox has
// consumed mail. It removes the old edge before taking a fresh unread snapshot
// and re-arms it when mail remains or the snapshot fails. A concurrent sender
// that touches the file after the snapshot therefore cannot have its new edge
// erased by this drain.
func reconcileCodexInboxDoorbell(ctx *cli.Context, initiativeID string) {
	lock, err := acquireCodexDeliveryLock(ctx.Home, initiativeID)
	if err != nil {
		if !errors.Is(err, errCodexDeliveryBusy) {
			fmt.Fprintf(ctx.Stderr, "ateam inbox: reconcile Codex doorbell: %v\n", err)
		}
		return
	}
	defer lock.Close()

	doorbellPath := filepath.Join(ctx.Home, "mailbox", initiativeID+".wake")
	if err := os.Remove(doorbellPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(ctx.Stderr, "ateam inbox: remove Codex doorbell: %v\n", err)
		return
	}
	messages, err := queryUnreadMessages(ctx, initiativeID)
	if err != nil {
		if touchErr := touchFile(doorbellPath); touchErr != nil {
			fmt.Fprintf(ctx.Stderr, "ateam inbox: re-arm Codex doorbell after query failure: %v\n", touchErr)
		}
		fmt.Fprintf(ctx.Stderr, "ateam inbox: reconcile Codex doorbell query: %v\n", err)
		return
	}
	if len(messages) > 0 {
		if err := touchFile(doorbellPath); err != nil {
			fmt.Fprintf(ctx.Stderr, "ateam inbox: re-arm Codex doorbell: %v\n", err)
		}
	}
}
