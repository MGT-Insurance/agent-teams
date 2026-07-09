package main

import "testing"

// These cover the CLI's own argument-validation logic only — never real
// eval.Run/eval.Collect/eval.Clean, which shell out to `ateam dispatch` /
// `claude -p` / Langfuse and are exercised in internal/eval's own test
// suite via fakes. runCmd's happy path is intentionally out of scope here.

func TestConfigNames_SortedAndKnown(t *testing.T) {
	got := configNames()
	want := "opus-noadvisor, sonnet-advisor"
	if got != want {
		t.Errorf("configNames() = %q, want %q", got, want)
	}
}

func TestRunCmd_MissingTaskAndConfig_Errors(t *testing.T) {
	if code := runCmd(nil); code != 1 {
		t.Errorf("runCmd(nil) = %d, want 1", code)
	}
}

func TestRunCmd_UnknownConfig_Errors(t *testing.T) {
	// Resolved before eval.LoadTaskSpec is ever called, so this never
	// touches the filesystem for --task's path.
	code := runCmd([]string{"--task", "/does/not/matter.json", "--config", "bogus"})
	if code != 1 {
		t.Errorf("runCmd with unknown --config = %d, want 1", code)
	}
}

func TestCollectCmd_WrongArgCount_Errors(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		if code := collectCmd(args); code != 1 {
			t.Errorf("collectCmd(%v) = %d, want 1", args, code)
		}
	}
}

func TestPushCmd_WrongArgCount_Errors(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		if code := pushCmd(args); code != 1 {
			t.Errorf("pushCmd(%v) = %d, want 1", args, code)
		}
	}
}

func TestCleanCmd_WrongArgCount_Errors(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		if code := cleanCmd(args); code != 1 {
			t.Errorf("cleanCmd(%v) = %d, want 1", args, code)
		}
	}
}

func TestRun_NoArgs_Errors(t *testing.T) {
	if code := run(nil); code != 1 {
		t.Errorf("run(nil) = %d, want 1", code)
	}
}

func TestRun_UnknownSubcommand_Errors(t *testing.T) {
	if code := run([]string{"bogus"}); code != 1 {
		t.Errorf("run([bogus]) = %d, want 1", code)
	}
}
