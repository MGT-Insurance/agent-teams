package sessionruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveCodexManagedAdapter is deliberately excluded from ordinary test
// runs: it creates real model turns. Run with ATEAM_LIVE_CODEX=1 after the
// fake-backed suite passes.
func TestLiveCodexManagedAdapter(t *testing.T) {
	if os.Getenv("ATEAM_LIVE_CODEX") != "1" {
		t.Skip("set ATEAM_LIVE_CODEX=1 to run paid managed app-server turns")
	}
	worktree, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	adapter := CodexAdapter{}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	launchNonce := "ATEAM_GO_APP_SERVER_LAUNCH_81c4"
	var thread SessionRef
	if err := adapter.Launch(ctx, Request{
		Worktree: worktree,
		Prompt:   "Reply exactly " + launchNonce + ".",
	}, func(ref SessionRef) error {
		thread = ref
		return nil
	}); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !waitForLiveThreadText(t, ctx, adapter, thread.ID, launchNonce) {
		t.Fatalf("launch nonce not found in thread %s", thread.ID)
	}

	resumeNonce := "ATEAM_GO_APP_SERVER_RESUME_f537"
	if err := adapter.Resume(ctx, Request{
		Worktree: worktree,
		Prompt:   "Reply exactly " + resumeNonce + ".",
	}, thread); err != nil {
		t.Fatalf("idle resume: %v", err)
	}
	if !waitForLiveThreadText(t, ctx, adapter, thread.ID, resumeNonce) {
		t.Fatalf("resume nonce not found in thread %s", thread.ID)
	}

	if err := adapter.Resume(ctx, Request{
		Worktree: worktree,
		Prompt:   "Run the shell command `sleep 8`, then report whether a newer instruction arrived.",
	}, thread); err != nil {
		t.Fatalf("start active turn: %v", err)
	}
	time.Sleep(time.Second)
	steerNonce := "ATEAM_GO_APP_SERVER_STEER_254a"
	if err := adapter.Resume(ctx, Request{
		Worktree: worktree,
		Prompt:   "Reply exactly " + steerNonce + " now.",
	}, thread); err != nil {
		t.Fatalf("active steer: %v", err)
	}
	if !waitForLiveThreadText(t, ctx, adapter, thread.ID, steerNonce) {
		t.Fatalf("steer nonce not found in thread %s", thread.ID)
	}
	t.Logf("managed app-server production adapter thread: %s", thread.ID)
}

func waitForLiveThreadText(t *testing.T, ctx context.Context, adapter CodexAdapter, threadID, text string) bool {
	t.Helper()
	info, err := ensureManagedCodexDaemon(ctx, adapter.Executable)
	if err != nil {
		t.Fatalf("ensure daemon while polling: %v", err)
	}
	client, err := dialManagedAppServer(ctx, info.SocketPath, io.Discard)
	if err != nil {
		t.Fatalf("dial while polling: %v", err)
	}
	defer client.Close()
	var initialized map[string]any
	if err := client.Request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "agent_teams_live_test", "title": "agent-teams live test", "version": "0.1.0"},
	}, &initialized); err != nil {
		t.Fatalf("initialize while polling: %v", err)
	}
	if err := client.Notify(ctx, "initialized", map[string]any{}); err != nil {
		t.Fatalf("initialized notification: %v", err)
	}

	// This poll deliberately keeps includeTurns:true: it needs the actual
	// message text to confirm the nonce landed, not just turn metadata, and
	// this test's threads stay tiny (a handful of turns). It is independent
	// of CodexAdapter.Resume, which now avoids full-history hydration on
	// resume (excludeTurns:true + paginated thread/turns/list) so a
	// long-running thread's rollout doesn't blow the app-server's 4MB read
	// limit.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var result json.RawMessage
		if err := client.Request(ctx, "thread/read", map[string]any{
			"threadId": threadID, "includeTurns": true,
		}, &result); err != nil {
			t.Fatalf("thread/read: %v", err)
		}
		if strings.Contains(string(result), text) && strings.Contains(string(result), `"status":"completed"`) {
			return true
		}
		select {
		case <-ctx.Done():
			t.Fatalf("poll %s: %v", threadID, ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Log(fmt.Sprintf("timed out waiting for %q in %s", text, threadID))
	return false
}
