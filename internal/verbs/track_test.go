package verbs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

func TestTrackAddKongValidate(t *testing.T) {
	tests := []struct {
		name string
		cmd  trackAddKong
		want string
	}{
		{name: "empty initiative", cmd: trackAddKong{Path: "/tmp/track"}, want: "initiative-id must not be empty"},
		{name: "empty path", cmd: trackAddKong{InitiativeID: "at-track"}, want: "absolute-path must not be empty"},
		{name: "relative path", cmd: trackAddKong{InitiativeID: "at-track", Path: "relative/track"}, want: "must be absolute"},
		{name: "line break", cmd: trackAddKong{InitiativeID: "at-track", Path: "/tmp/track\nother"}, want: "must not contain a line break"},
		{name: "edge whitespace", cmd: trackAddKong{InitiativeID: "at-track", Path: "/tmp/track "}, want: "must not have leading or trailing whitespace"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cmd.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
	if err := (&trackAddKong{InitiativeID: "at-track", Path: "/tmp/track with spaces"}).Validate(); err != nil {
		t.Fatalf("valid absolute path rejected: %v", err)
	}
}

func TestTrackAddKongRunAppendsTrackAndPreservesDescription(t *testing.T) {
	issue := bd.Issue{
		ID:          "at-track",
		Description: "repo: /project\nepic: repo-root\npr: https://github.com/owner/repo/pull/7\n",
	}
	var updated string
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		switch args[0] {
		case "show":
			return issueJSON(issue), nil
		case "update":
			bodyPath := strings.TrimPrefix(args[2], "--body-file=")
			body, err := os.ReadFile(bodyPath)
			updated = string(body)
			return "", err
		default:
			t.Fatalf("unexpected bd call: %v", args)
			return "", nil
		}
	}}
	ctx, stdout, _ := makeCtx(global, t.TempDir())
	path := filepath.Join(t.TempDir(), "track with spaces")
	if err := (&trackAddKong{InitiativeID: issue.ID, Path: path}).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	fields := initiative.Of(bd.Issue{Description: updated})
	if len(fields.Tracks) != 1 || fields.Tracks[0] != path {
		t.Fatalf("tracks = %v, want [%s]\n%s", fields.Tracks, path, updated)
	}
	if !strings.Contains(updated, "pr: https://github.com/owner/repo/pull/7\n") {
		t.Fatalf("existing description was not preserved:\n%s", updated)
	}
	if !strings.Contains(stdout.String(), "track add: recorded "+path+" on "+issue.ID) {
		t.Fatalf("stdout = %q, want recorded confirmation", stdout.String())
	}
}

func TestTrackAddKongRunIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track")
	issue := bd.Issue{ID: "at-track", Description: "track-worktree: " + path + "\n"}
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] != "show" {
			t.Fatalf("idempotent add must not update, got %v", args)
		}
		return issueJSON(issue), nil
	}}
	ctx, stdout, _ := makeCtx(global, t.TempDir())
	if err := (&trackAddKong{InitiativeID: issue.ID, Path: path}).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout.String(), "already recorded") {
		t.Fatalf("stdout = %q, want idempotent confirmation", stdout.String())
	}
}

func TestTrackAddKongRunFailsBeforeReadWhenLockCannotBeCreated(t *testing.T) {
	homeFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(homeFile, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("write fake home: %v", err)
	}
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		t.Fatalf("bd must not be read when lock acquisition fails, got %v", args)
		return "", nil
	}}
	ctx, _, _ := makeCtx(global, homeFile)
	err := (&trackAddKong{InitiativeID: "at-track", Path: "/tmp/track"}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "acquire initiative at-track lock") {
		t.Fatalf("Run error = %v, want lock acquisition failure", err)
	}
}

func TestTrackAddKongRunReleasesLockAndReportsUpdateError(t *testing.T) {
	home := t.TempDir()
	issue := bd.Issue{ID: "at-track", Description: "repo: /project\n"}
	global := &fakeBD{runFn: func(args ...string) (string, error) {
		if args[0] == "show" {
			return issueJSON(issue), nil
		}
		return "", errors.New("injected track update failure")
	}}
	ctx, stdout, _ := makeCtx(global, home)
	err := (&trackAddKong{InitiativeID: issue.ID, Path: "/tmp/track"}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "injected track update failure") {
		t.Fatalf("Run error = %v, want update failure", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failure emitted success output: %q", stdout.String())
	}

	lockPath := initiativeMutationLockPath(home, issue.ID)
	file, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open released lock: %v", err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock remained held after update error: %v", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func TestTrackAddKongConcurrentMappedPRAddPreservesAllRails(t *testing.T) {
	if os.Getenv(prAddProcessHelperEnv) != "" {
		t.Skip("parent-only concurrency test")
	}
	const (
		prURL      = "https://github.com/owner/repo/pull/51"
		workstream = "repo-root.1"
		trackPath  = "/tmp/track-pr-concurrent"
	)
	description := runConcurrentInitiativeMutations(t, []concurrentInitiativeMutation{
		{action: "pr", url: prURL, workstream: workstream},
		{action: "track", track: trackPath},
	})
	issue := bd.Issue{ID: "at-concurrent", Description: description}
	fields := initiative.Of(issue)
	if len(fields.PRs) != 1 || fields.PRs[0] != prURL {
		t.Errorf("final PRs = %v, want [%s]:\n%s", fields.PRs, prURL, description)
	}
	if len(fields.Tracks) != 1 || fields.Tracks[0] != trackPath {
		t.Errorf("final tracks = %v, want [%s]:\n%s", fields.Tracks, trackPath, description)
	}
	associations := initiative.PRWorkstreams(issue)
	if len(associations) != 1 || associations[0].PR != prURL || associations[0].Workstream != workstream {
		t.Errorf("final PR associations = %#v, want %s -> %s:\n%s", associations, prURL, workstream, description)
	}
}

func TestTrackAddKongConcurrentSessionTiePreservesBothRails(t *testing.T) {
	if os.Getenv(prAddProcessHelperEnv) != "" {
		t.Skip("parent-only concurrency test")
	}
	const (
		sessionID = "sess-track-concurrent"
		trackPath = "/tmp/track-session-concurrent"
	)
	description := runConcurrentInitiativeMutations(t, []concurrentInitiativeMutation{
		{action: "session", session: sessionID},
		{action: "track", track: trackPath},
	})
	fields := initiative.Of(bd.Issue{ID: "at-concurrent", Description: description})
	if len(fields.Sessions) != 1 || fields.Sessions[0] != sessionID {
		t.Errorf("final sessions = %v, want [%s]:\n%s", fields.Sessions, sessionID, description)
	}
	if len(fields.Tracks) != 1 || fields.Tracks[0] != trackPath {
		t.Errorf("final tracks = %v, want [%s]:\n%s", fields.Tracks, trackPath, description)
	}
}

func TestRegisterTrackKongAddedAsKongVerb(t *testing.T) {
	parser, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterAllKong(parser)
	if _, err := parser.Parse([]string{"track", "add", "--help"}); err != nil && strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("track add was not registered: %v", err)
	}
}
