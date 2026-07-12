package main

import (
	"reflect"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/eval"
)

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

func TestRunCmd_ConfigAndModelConflict_Errors(t *testing.T) {
	// Resolved before eval.LoadTaskSpec, same as the unknown-config case.
	code := runCmd([]string{"--task", "/does/not/matter.json", "--config", "opus-noadvisor", "--model", "sonnet"})
	if code != 1 {
		t.Errorf("runCmd with --config and --model = %d, want 1", code)
	}
}

func TestRunCmd_AdvisorWithoutModel_Errors(t *testing.T) {
	code := runCmd([]string{"--task", "/does/not/matter.json", "--advisor", "opus"})
	if code != 1 {
		t.Errorf("runCmd with --advisor and no --model = %d, want 1", code)
	}
}

func TestRunCmd_NeitherConfigNorModel_Errors(t *testing.T) {
	code := runCmd([]string{"--task", "/does/not/matter.json"})
	if code != 1 {
		t.Errorf("runCmd with neither --config nor --model = %d, want 1", code)
	}
}

func TestResolveConfig(t *testing.T) {
	cases := []struct {
		name                       string
		configName, model, advisor string
		wantErr                    bool
		want                       eval.ConfigFingerprint
	}{
		{
			name:       "known config preset",
			configName: "opus-noadvisor",
			want:       eval.ConfigFingerprint{Name: "opus-noadvisor", DRIModel: "opus", Advisor: ""},
		},
		{
			name:       "unknown config preset",
			configName: "bogus",
			wantErr:    true,
		},
		{
			name:  "model only derives noadvisor name",
			model: "sonnet",
			want:  eval.ConfigFingerprint{Name: "sonnet-noadvisor", DRIModel: "sonnet", Advisor: ""},
		},
		{
			name:    "model and advisor derives advisor name",
			model:   "sonnet",
			advisor: "opus",
			want:    eval.ConfigFingerprint{Name: "sonnet-advisor:opus", DRIModel: "sonnet", Advisor: "opus"},
		},
		{
			name:    "advisor without model errors",
			advisor: "opus",
			wantErr: true,
		},
		{
			name:       "config plus model conflicts",
			configName: "opus-noadvisor",
			model:      "sonnet",
			wantErr:    true,
		},
		{
			name:       "config plus advisor conflicts",
			configName: "opus-noadvisor",
			advisor:    "opus",
			wantErr:    true,
		},
		{
			name:    "neither config nor model errors",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveConfig(tc.configName, tc.model, tc.advisor)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveConfig(%q, %q, %q) = %+v, want error", tc.configName, tc.model, tc.advisor, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConfig(%q, %q, %q) unexpected error: %v", tc.configName, tc.model, tc.advisor, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveConfig(%q, %q, %q) = %+v, want %+v", tc.configName, tc.model, tc.advisor, got, tc.want)
			}
		})
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
