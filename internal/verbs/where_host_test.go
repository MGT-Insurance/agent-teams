package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

func TestRemoteInitiative(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop")
	cases := []struct {
		where string
		want  bool
	}{
		{"", false},
		{"host:laptop", false},
		{"host:sandbox-1", true},
		{"run:at-abc", false},
		{"other", false},
	}
	for _, tc := range cases {
		if got := remoteInitiative(initiative.Fields{Where: tc.where}); got != tc.want {
			t.Errorf("remoteInitiative(%q) = %t, want %t", tc.where, got, tc.want)
		}
	}
}

func TestDispatch_WritesWhereHostLine(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop-test")
	repoDir := newEnabledRepoDir(t)
	home := t.TempDir()

	var capturedBody string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			for _, a := range args {
				if p, ok := strings.CutPrefix(a, "--body-file="); ok {
					if data, err := os.ReadFile(p); err == nil {
						capturedBody = string(data)
					}
				}
			}
			if issue, ok := dst.(*bd.Issue); ok {
				return json.Unmarshal([]byte(`{"id":"at-test1","title":"Add undo stack"}`), issue)
			}
			return nil
		},
	}
	fg := &fakeGit{repoRootFn: func(string) (string, error) { return repoDir, nil }}
	ctx, _, _ := makeCtx(fbd, home)
	cmd := &dispatchKong{
		Problem:  "Add undo stack",
		Repo:     repoDir,
		NoLaunch: true,
		git:      fg,
		launch:   func(_ *cli.Context, _, _, _, _ string) error { return nil },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if n := strings.Count(capturedBody, "where: host:laptop-test\n"); n != 1 {
		t.Errorf("want exactly one where: host:laptop-test line, got %d:\n%s", n, capturedBody)
	}
}

// remoteSendCmd returns a sendKong whose every delivery seam fails the test.
func remoteSendCmd(t *testing.T, sf *sendFixture, recipient string) *sendKong {
	t.Helper()
	return &sendKong{
		RecipientID: recipient,
		File:        sf.file,
		Sender:      "test-sender",
		agentsFunc: func() ([]agentSession, error) {
			t.Error("agentsFunc called for a remote recipient")
			return nil, nil
		},
		resumeFunc: func(*cli.Context, string, string, string) error {
			t.Error("resumeFunc called for a remote recipient")
			return nil
		},
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc: func(string) error {
			t.Error("respawnFunc called for a remote recipient")
			return nil
		},
	}
}

func whereBD(sf *sendFixture, recipientID, msgID, where string) *fakeBD {
	b := sf.fakeBD(recipientID, msgID)
	b.runFn = func(...string) (string, error) {
		desc := "worktree: " + sf.recipientWt + "\n"
		if where != "" {
			desc += "where: " + where + "\n"
		}
		raw, _ := json.Marshal([]bd.Issue{{ID: recipientID, Description: desc}})
		return string(raw), nil
	}
	return b
}

func TestSendKong_RemoteRecipient_StopsAfterStoringMessage(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop")
	sf := newSendFixture(t)
	cmd := remoteSendCmd(t, sf, "at-remote")
	ctx, stdout, _ := makeCtx(whereBD(sf, "at-remote", "at-msg1", "host:sandbox-1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, sf.createArgs, "--type=message", "message bead not created")
	if !strings.Contains(stdout.String(), "recipient at-remote runs on host:sandbox-1; the journal rings its doorbell\n") {
		t.Errorf("stdout missing remote line: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(sf.home, "mailbox")); err == nil {
		t.Error("mailbox dir was created for a remote recipient")
	}
}

func TestSendKong_NotRemote_ReachesLocalDelivery(t *testing.T) {
	for _, where := range []string{"", "host:laptop", "run:x"} {
		t.Run("where="+where, func(t *testing.T) {
			t.Setenv("AGENT_TEAMS_HOST", "laptop")
			sf := newSendFixture(t)
			var agentsCalled bool
			cmd := remoteSendCmd(t, sf, "at-local")
			cmd.agentsFunc = func() ([]agentSession, error) {
				agentsCalled = true
				return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "busy"}}, nil
			}
			ctx, _, _ := makeCtx(whereBD(sf, "at-local", "at-msg2", where), sf.home)
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !agentsCalled {
				t.Error("local recipient did not reach the liveness query")
			}
			if _, err := os.Stat(filepath.Join(sf.home, "mailbox", "at-local.wake")); err != nil {
				t.Errorf("doorbell not touched: %v", err)
			}
		})
	}
}

func TestResumeKong_RemoteInitiative_ExitsOneWithoutTouchingWorktree(t *testing.T) {
	t.Setenv("AGENT_TEAMS_HOST", "laptop")
	missing := filepath.Join(t.TempDir(), "never-created")
	fbd := &fakeBD{
		runFn: func(...string) (string, error) {
			raw, _ := json.Marshal([]bd.Issue{{
				ID:          "at-rr1",
				Status:      "open",
				Description: "worktree: " + missing + "\nwhere: host:sandbox-1\n",
			}})
			return string(raw), nil
		},
	}
	launched := false
	ctx, _, stderr := makeCtx(fbd, t.TempDir())
	cmd := &resumeKong{
		ID: "at-rr1",
		launch: func(*cli.Context, string, string, string, string) error {
			launched = true
			return nil
		},
	}
	err := cmd.Run(ctx)
	if code := cli.ExitCode(err); err == nil || code != 1 {
		t.Fatalf("want exit 1, got err=%v code=%d", err, code)
	}
	if launched {
		t.Error("launch called for a remote initiative")
	}
	if !strings.Contains(stderr.String(), "ateam resume: at-rr1 runs on host:sandbox-1; resume it there") {
		t.Errorf("stderr missing remote line: %s", stderr.String())
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("worktree was recreated")
	}
}
