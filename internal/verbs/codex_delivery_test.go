package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sessionruntime"
)

func TestRunCodexWakeUsesLastBoundThreadAndDefaultPrompt(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	recipient := bd.Issue{ID: "at-codex", Description: strings.Join([]string{
		"runtime: codex",
		"worktree: /worktree",
		"session: thread-old",
		"session: thread-current",
	}, "\n") + "\n"}
	var got runtimeStartRequest
	err := runCodexWake(ctx, recipient, "", "gpt-test", func(_ *cli.Context, req runtimeStartRequest) error {
		got = req
		return nil
	})
	if err != nil {
		t.Fatalf("wake: %v", err)
	}
	if got.Runtime != sessionruntime.Codex || got.InitiativeID != "at-codex" || got.Worktree != "/worktree" || got.ResumeID != "thread-current" || got.Model != "gpt-test" {
		t.Fatalf("request = %+v", got)
	}
	if got.Prompt != codexMailPrompt {
		t.Fatalf("prompt = %q", got.Prompt)
	}
}

func TestRunCodexWakeSerializesDeliveryChoice(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	recipient := bd.Issue{ID: "at-codex", Description: "runtime: codex\nworktree: /w\nsession: thread-1\n"}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	submit := func(_ *cli.Context, _ runtimeStartRequest) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- runCodexWake(ctx, recipient, "mail", "", submit) }()
	<-entered
	if err := runCodexWake(ctx, recipient, "mail", "", submit); !errors.Is(err, errCodexDeliveryBusy) {
		t.Fatalf("second wake error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first wake: %v", err)
	}

	called := false
	if err := runCodexWake(ctx, recipient, "mail", "", func(_ *cli.Context, _ runtimeStartRequest) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("wake after release: %v", err)
	}
	if !called {
		t.Fatal("lock was not released")
	}
}

func TestRunCodexWakeRequiresBoundThread(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	err := runCodexWake(ctx, bd.Issue{ID: "at-codex", Description: "runtime: codex\nworktree: /w\n"}, "mail", "", func(_ *cli.Context, _ runtimeStartRequest) error {
		t.Fatal("submit called")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "no bound session") {
		t.Fatalf("error = %v", err)
	}
}

func TestReconcileCodexInboxDoorbellPreservesNewWakeAfterSnapshot(t *testing.T) {
	home := t.TempDir()
	doorbell := filepath.Join(home, "mailbox", "at-codex.wake")
	if err := os.MkdirAll(filepath.Dir(doorbell), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := touchFile(doorbell); err != nil {
		t.Fatal(err)
	}
	fbd := &fakeBD{runJSONFn: func(dst any, _ ...string) error {
		// The old edge has already been removed. Model a sender touching a new
		// edge just after the unread snapshot was selected.
		if err := touchFile(doorbell); err != nil {
			t.Fatal(err)
		}
		return json.Unmarshal([]byte("[]"), dst)
	}}
	ctx, _, _ := makeCtx(fbd, home)
	reconcileCodexInboxDoorbell(ctx, "at-codex")
	if _, err := os.Stat(doorbell); err != nil {
		t.Fatalf("new wake edge was erased: %v", err)
	}
}

func TestReconcileCodexInboxDoorbellRearmsAfterQueryFailure(t *testing.T) {
	home := t.TempDir()
	doorbell := filepath.Join(home, "mailbox", "at-codex.wake")
	if err := os.MkdirAll(filepath.Dir(doorbell), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, _, stderr := makeCtx(&fakeBD{runJSONFn: func(any, ...string) error {
		return errors.New("beads unavailable")
	}}, home)
	reconcileCodexInboxDoorbell(ctx, "at-codex")
	if _, err := os.Stat(doorbell); err != nil {
		t.Fatalf("query failure did not leave a retryable doorbell: %v", err)
	}
	if !strings.Contains(stderr.String(), "beads unavailable") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInboxKongCodexReconcilesDoorbellAfterConsumption(t *testing.T) {
	for _, tc := range []struct {
		name          string
		remainingMail bool
		wantDoorbell  bool
	}{
		{name: "drained", wantDoorbell: false},
		{name: "mail arrived during drain", remainingMail: true, wantDoorbell: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worktree := t.TempDir()
			home := t.TempDir()
			doorbell := filepath.Join(home, "mailbox", "at-codex.wake")
			if err := os.MkdirAll(filepath.Dir(doorbell), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := touchFile(doorbell); err != nil {
				t.Fatal(err)
			}
			unreadQueries := 0
			fbd := &fakeBD{
				runJSONFn: func(dst any, args ...string) error {
					if containsAll(args, "--include-infra") {
						unreadQueries++
						messages := []bd.Issue{{ID: "at-msg-1", IssueType: "message", Assignee: "at-codex", Description: "first"}}
						if unreadQueries > 1 && !tc.remainingMail {
							messages = nil
						}
						if unreadQueries > 1 && tc.remainingMail {
							messages[0].ID = "at-msg-2"
						}
						return json.Unmarshal(mustMarshal(messages), dst)
					}
					issues := []bd.Issue{{ID: "at-codex", Status: "open", Description: "runtime: codex\nworktree: " + worktree + "\n"}}
					return json.Unmarshal(mustMarshal(issues), dst)
				},
				runFn: func(...string) (string, error) { return "", nil },
			}
			ctx, _, _ := makeCtx(fbd, home)
			t.Chdir(worktree)
			if err := (&inboxKong{}).Run(ctx); err != nil {
				t.Fatalf("inbox: %v", err)
			}
			_, statErr := os.Stat(doorbell)
			if tc.wantDoorbell && statErr != nil {
				t.Fatalf("doorbell should remain armed: %v", statErr)
			}
			if !tc.wantDoorbell && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("doorbell should be cleared, stat err = %v", statErr)
			}
			if unreadQueries != 2 {
				t.Fatalf("unread queries = %d, want initial read plus reconciliation", unreadQueries)
			}
		})
	}
}

func TestInboxKongCodexConsumesTwoRapidMessagesInOneWake(t *testing.T) {
	worktree := t.TempDir()
	home := t.TempDir()
	doorbell := filepath.Join(home, "mailbox", "at-codex.wake")
	if err := os.MkdirAll(filepath.Dir(doorbell), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := touchFile(doorbell); err != nil {
		t.Fatal(err)
	}
	unreadQueries := 0
	closed := map[string]bool{}
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if containsAll(args, "--include-infra") {
				unreadQueries++
				var messages []bd.Issue
				if unreadQueries == 1 {
					messages = []bd.Issue{
						{ID: "at-msg-1", IssueType: "message", Assignee: "at-codex"},
						{ID: "at-msg-2", IssueType: "message", Assignee: "at-codex"},
					}
				}
				return json.Unmarshal(mustMarshal(messages), dst)
			}
			issues := []bd.Issue{{ID: "at-codex", Status: "open", Description: "runtime: codex\nworktree: " + worktree + "\n"}}
			return json.Unmarshal(mustMarshal(issues), dst)
		},
		runFn: func(args ...string) (string, error) {
			if len(args) == 2 && args[0] == "close" {
				closed[args[1]] = true
			}
			return "", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, home)
	t.Chdir(worktree)
	if err := (&inboxKong{}).Run(ctx); err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if !closed["at-msg-1"] || !closed["at-msg-2"] || len(closed) != 2 {
		t.Fatalf("closed messages = %#v", closed)
	}
	if _, err := os.Stat(doorbell); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("doorbell should be cleared after both messages, stat err = %v", err)
	}
}
