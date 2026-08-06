package sessionruntime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeCodex(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executable fixture is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "codex")
	contents := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCodexAdapterLaunchCapturesThreadAndArguments(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	exe := fakeCodex(t, "printf '%s\\n' \"$*\" > \"$ARGS_FILE\"\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-123\"}' '{\"type\":\"turn.completed\"}'")
	t.Setenv("ARGS_FILE", argsFile)
	var events bytes.Buffer
	var got SessionRef
	err := (CodexAdapter{Executable: exe}).Launch(context.Background(), Request{
		InitiativeID: "at-1",
		Worktree:     t.TempDir(),
		Prompt:       "$dri at-1",
		Model:        "gpt-test",
		Events:       &events,
	}, func(ref SessionRef) error {
		got = ref
		return nil
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if got != (SessionRef{Runtime: Codex, ID: "thread-123"}) {
		t.Fatalf("session = %+v", got)
	}
	args, _ := os.ReadFile(argsFile)
	if string(args) != "exec --json --model gpt-test $dri at-1\n" {
		t.Fatalf("args = %q", args)
	}
	if !strings.Contains(events.String(), "thread.started") {
		t.Fatalf("event log missing thread event: %s", events.String())
	}
}

func TestCodexAdapterResumeVerifiesThread(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	exe := fakeCodex(t, "printf '%s\\n' \"$*\" > \"$ARGS_FILE\"\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-123\"}'")
	t.Setenv("ARGS_FILE", argsFile)
	err := (CodexAdapter{Executable: exe}).Resume(context.Background(), Request{Worktree: t.TempDir(), Prompt: "wake"}, SessionRef{Runtime: Codex, ID: "thread-123"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	args, _ := os.ReadFile(argsFile)
	if string(args) != "exec resume --json thread-123 wake\n" {
		t.Fatalf("args = %q", args)
	}
}

func TestCodexAdapterFailures(t *testing.T) {
	t.Run("missing thread event", func(t *testing.T) {
		exe := fakeCodex(t, "printf '%s\\n' '{\"type\":\"turn.completed\"}'")
		err := (CodexAdapter{Executable: exe}).Launch(context.Background(), Request{Worktree: t.TempDir(), Prompt: "p"}, func(SessionRef) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "without a thread.started") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("resume mismatch", func(t *testing.T) {
		exe := fakeCodex(t, "printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"wrong\"}'")
		err := (CodexAdapter{Executable: exe}).Resume(context.Background(), Request{Worktree: t.TempDir(), Prompt: "p"}, SessionRef{Runtime: Codex, ID: "wanted"})
		if err == nil || !strings.Contains(err.Error(), "runtime emitted wrong") {
			t.Fatalf("error = %v", err)
		}
	})
}
