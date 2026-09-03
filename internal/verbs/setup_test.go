package verbs

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func compatibleCodexFixture(t *testing.T) string {
	t.Helper()
	return fakeCodexCLI(t, `printf '%s\n' '{"status":"stopped","managedCodexPath":"/standalone/codex","managedCodexVersion":"0.146.1","cliVersion":"0.146.1"}'`)
}

func TestSetupCodexInstallsDefinitionsAndDetectsDrift(t *testing.T) {
	codexHome := t.TempDir()
	ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
	cmd := &setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("first setup: %v", err)
	}
	distinctiveRules := map[string]string{
		"planner":      "Decompose in concentric circles",
		"implementer":  "Never guess on design",
		"reviewer":     "After-the-fact identifiability",
		"tester":       "Only the DRI starts a dev server",
		"investigator": "vs PLANNER",
	}
	expectedModels := map[string]string{
		"planner":      "gpt-5.6-sol",
		"implementer":  "gpt-5.6-terra",
		"investigator": "gpt-5.6-terra",
		"reviewer":     "gpt-5.6-terra",
		"tester":       "gpt-5.6-terra",
	}
	for _, role := range []string{"planner", "implementer", "tester", "reviewer", "investigator"} {
		path := filepath.Join(codexHome, "agents", "agent-teams-"+role+".toml")
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", role, err)
		}
		if !strings.Contains(string(body), `name = "agent-teams-`+role+`"`) ||
			!strings.Contains(string(body), "ateam learnings "+role) ||
			!strings.Contains(string(body), distinctiveRules[role]) {
			t.Fatalf("invalid %s definition: %s", role, body)
		}

		modelAssignments := 0
		reasoningEffortAssignments := 0
		for _, line := range strings.Split(string(body), "\n") {
			key, _, hasAssignment := strings.Cut(line, "=")
			if !hasAssignment {
				continue
			}
			switch strings.TrimSpace(key) {
			case "model":
				modelAssignments++
				if got, want := strings.TrimSpace(line), `model = "`+expectedModels[role]+`"`; got != want {
					t.Errorf("%s model assignment = %q, want %q", role, got, want)
				}
			case "model_reasoning_effort":
				reasoningEffortAssignments++
				if got, want := strings.TrimSpace(line), `model_reasoning_effort = "high"`; got != want {
					t.Errorf("%s reasoning effort assignment = %q, want %q", role, got, want)
				}
			}
		}
		if modelAssignments != 1 {
			t.Errorf("%s model assignments = %d, want exactly 1", role, modelAssignments)
		}
		if reasoningEffortAssignments != 1 {
			t.Errorf("%s reasoning effort assignments = %d, want exactly 1", role, reasoningEffortAssignments)
		}
	}
	if !strings.Contains(stdout.String(), "start a new Codex session") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	configPath := filepath.Join(codexHome, "config.toml")
	configOverride := []byte("# human setting\nmodel_auto_compact_token_limit_scope = \"body_after_prefix\"\nmodel_auto_compact_token_limit = 123456\n")
	if err := os.WriteFile(configPath, configOverride, 0o600); err != nil {
		t.Fatal(err)
	}

	drifted := filepath.Join(codexHome, "agents", "agent-teams-planner.toml")
	if err := os.WriteFile(drifted, []byte("local change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(ctx); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("drift error = %v", err)
	}
	cmd.Force = true
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("forced setup: %v", err)
	}
	restored, _ := os.ReadFile(drifted)
	if strings.Contains(string(restored), "local change") {
		t.Fatal("force did not restore bundled definition")
	}
	if got, err := os.ReadFile(configPath); err != nil || !bytes.Equal(got, configOverride) {
		t.Fatalf("force changed config override: got %q, err %v", got, err)
	}
}

func TestSetupCodexMergesConfigWithoutClobbering(t *testing.T) {
	const wantDefault = "model_auto_compact_token_limit = 300000\n"
	tests := []struct {
		name       string
		initial    *string
		mode       os.FileMode
		want       string
		wantMode   os.FileMode
		wantOutput string
	}{
		{
			name:       "missing file",
			want:       wantDefault,
			wantMode:   0o600,
			wantOutput: "installed default:",
		},
		{
			name:       "comments root keys and trailing table",
			initial:    stringPointer("# keep this comment\nmodel = \"gpt-5.6-sol\"\n\n[tools]\nweb_search = true\n"),
			mode:       0o640,
			want:       wantDefault + "# keep this comment\nmodel = \"gpt-5.6-sol\"\n\n[tools]\nweb_search = true\n",
			wantMode:   0o640,
			wantOutput: "installed default:",
		},
		{
			name:       "explicit scope without threshold",
			initial:    stringPointer("model_auto_compact_token_limit_scope = \"body_after_prefix\"\n[profile.long]\nmodel = \"gpt-5.6-terra\"\n"),
			mode:       0o600,
			want:       wantDefault + "model_auto_compact_token_limit_scope = \"body_after_prefix\"\n[profile.long]\nmodel = \"gpt-5.6-terra\"\n",
			wantMode:   0o600,
			wantOutput: "installed default:",
		},
		{
			name:       "custom threshold and scope",
			initial:    stringPointer("# user override\nmodel_auto_compact_token_limit = 123456\nmodel_auto_compact_token_limit_scope = \"body_after_prefix\"\n"),
			mode:       0o600,
			want:       "# user override\nmodel_auto_compact_token_limit = 123456\nmodel_auto_compact_token_limit_scope = \"body_after_prefix\"\n",
			wantMode:   0o600,
			wantOutput: "preserved override:",
		},
		{
			name:       "same key inside table does not mask root default",
			initial:    stringPointer("[profile.custom]\nmodel_auto_compact_token_limit = 999999\n"),
			mode:       0o600,
			want:       wantDefault + "[profile.custom]\nmodel_auto_compact_token_limit = 999999\n",
			wantMode:   0o600,
			wantOutput: "installed default:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codexHome := t.TempDir()
			configPath := filepath.Join(codexHome, "config.toml")
			if tt.initial != nil {
				if err := os.WriteFile(configPath, []byte(*tt.initial), tt.mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(configPath, tt.mode); err != nil {
					t.Fatal(err)
				}
			}
			ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
			cmd := &setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("first setup: %v", err)
			}
			got, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("config = %q, want %q", got, tt.want)
			}
			info, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if gotMode := info.Mode().Perm(); gotMode != tt.wantMode {
				t.Fatalf("config mode = %o, want %o", gotMode, tt.wantMode)
			}
			if !strings.Contains(stdout.String(), tt.wantOutput) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.wantOutput)
			}

			first := append([]byte(nil), got...)
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("second setup: %v", err)
			}
			second, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(second, first) {
				t.Fatalf("second setup changed config: first %q, second %q", first, second)
			}
			temps, err := filepath.Glob(filepath.Join(codexHome, ".config.toml.tmp-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(temps) != 0 {
				t.Fatalf("atomic-write temp files remain: %v", temps)
			}
		})
	}
}

func TestSetupCodexPreservesSymlinkedConfig(t *testing.T) {
	const wantDefault = "model_auto_compact_token_limit = 300000\n"
	tests := []struct {
		name       string
		initial    string
		want       string
		wantOutput string
	}{
		{
			name:       "inserts missing root default into target",
			initial:    "# keep this comment\nmodel = \"gpt-5.6-sol\"\n\n[tools]\nweb_search = true\n",
			want:       wantDefault + "# keep this comment\nmodel = \"gpt-5.6-sol\"\n\n[tools]\nweb_search = true\n",
			wantOutput: "installed default:",
		},
		{
			name:       "preserves existing override",
			initial:    "# user override\nmodel_auto_compact_token_limit = 123456\nmodel_auto_compact_token_limit_scope = \"body_after_prefix\"\n",
			want:       "# user override\nmodel_auto_compact_token_limit = 123456\nmodel_auto_compact_token_limit_scope = \"body_after_prefix\"\n",
			wantOutput: "preserved override:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codexHome := t.TempDir()
			targetDir := t.TempDir()
			targetPath := filepath.Join(targetDir, "shared-config.toml")
			if err := os.WriteFile(targetPath, []byte(tt.initial), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(targetPath, 0o640); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(codexHome, "config.toml")
			linkTarget, err := filepath.Rel(codexHome, targetPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(linkTarget, configPath); err != nil {
				t.Fatal(err)
			}

			ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
			cmd := &setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("first setup: %v", err)
			}
			assertSymlinkTarget(t, configPath, linkTarget)
			got, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("target config = %q, want %q", got, tt.want)
			}
			if count := strings.Count(string(got), codexCompactionKey+" = "); count != 1 {
				t.Fatalf("root default assignments = %d, want exactly 1 in %q", count, got)
			}
			info, err := os.Stat(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			if gotMode := info.Mode().Perm(); gotMode != 0o640 {
				t.Fatalf("target mode = %o, want 640", gotMode)
			}
			if !strings.Contains(stdout.String(), tt.wantOutput) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.wantOutput)
			}

			first := append([]byte(nil), got...)
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("second setup: %v", err)
			}
			assertSymlinkTarget(t, configPath, linkTarget)
			second, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(second, first) {
				t.Fatalf("second setup changed target: first %q, second %q", first, second)
			}
		})
	}
}

func TestSetupCodexRejectsInvalidSymlinkTargetsBeforeWrites(t *testing.T) {
	tests := []struct {
		name       string
		makeTarget func(t *testing.T, codexHome string) string
	}{
		{
			name: "dangling",
			makeTarget: func(t *testing.T, codexHome string) string {
				t.Helper()
				return filepath.Join(codexHome, "missing-config.toml")
			},
		},
		{
			name: "non-regular",
			makeTarget: func(t *testing.T, codexHome string) string {
				t.Helper()
				target := filepath.Join(t.TempDir(), "config-directory")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				return target
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codexHome := t.TempDir()
			configPath := filepath.Join(codexHome, "config.toml")
			linkTarget := tt.makeTarget(t, codexHome)
			if err := os.Symlink(linkTarget, configPath); err != nil {
				t.Fatal(err)
			}

			ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
			err := (&setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}).Run(ctx)
			if err == nil || !strings.Contains(err.Error(), configPath) {
				t.Fatalf("error = %v, want config path %s", err, configPath)
			}
			assertSymlinkTarget(t, configPath, linkTarget)
			if _, err := os.Stat(filepath.Join(codexHome, "agents")); !os.IsNotExist(err) {
				t.Fatalf("agents directory should not be created, stat err = %v", err)
			}
		})
	}
}

func TestSetupCodexRejectsFIFOConfigBeforeWrites(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, codexHome string) (configPath string, fifoPath string, linkTarget string)
	}{
		{
			name: "direct",
			prepare: func(t *testing.T, codexHome string) (string, string, string) {
				t.Helper()
				configPath := filepath.Join(codexHome, "config.toml")
				if err := syscall.Mkfifo(configPath, 0o600); err != nil {
					t.Fatalf("create FIFO: %v", err)
				}
				return configPath, configPath, ""
			},
		},
		{
			name: "symlinked",
			prepare: func(t *testing.T, codexHome string) (string, string, string) {
				t.Helper()
				fifoPath := filepath.Join(t.TempDir(), "shared-config.toml")
				if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
					t.Fatalf("create FIFO: %v", err)
				}
				configPath := filepath.Join(codexHome, "config.toml")
				linkTarget, err := filepath.Rel(codexHome, fifoPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(linkTarget, configPath); err != nil {
					t.Fatal(err)
				}
				return configPath, fifoPath, linkTarget
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codexHome := t.TempDir()
			configPath, fifoPath, linkTarget := tt.prepare(t, codexHome)
			executable := compatibleCodexFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSetupCodexFIFOHelper$", "-test.v")
			cmd.Env = append(os.Environ(),
				"ATEAM_SETUP_FIFO_HELPER=1",
				"ATEAM_SETUP_FIFO_HOME="+codexHome,
				"ATEAM_SETUP_FIFO_EXECUTABLE="+executable,
			)
			output, err := cmd.CombinedOutput()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("setup blocked while opening FIFO; helper was killed and reaped after timeout")
			}
			if err != nil {
				t.Fatalf("setup helper: %v\n%s", err, output)
			}
			if got := string(output); !strings.Contains(got, configPath) || !strings.Contains(got, "not a regular file") {
				t.Fatalf("error output = %q, want config path %q and non-regular-file error", got, configPath)
			}

			info, err := os.Lstat(fifoPath)
			if err != nil {
				t.Fatalf("lstat FIFO: %v", err)
			}
			if info.Mode()&os.ModeNamedPipe == 0 {
				t.Fatalf("FIFO mode = %v, want named pipe", info.Mode())
			}
			if linkTarget != "" {
				assertSymlinkTarget(t, configPath, linkTarget)
			}
			if _, err := os.Stat(filepath.Join(codexHome, "agents")); !os.IsNotExist(err) {
				t.Fatalf("agents directory should not be created, stat err = %v", err)
			}
		})
	}
}

func TestSetupCodexFIFOHelper(t *testing.T) {
	if os.Getenv("ATEAM_SETUP_FIFO_HELPER") != "1" {
		return
	}
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	err := (&setupCodexKong{
		executable: os.Getenv("ATEAM_SETUP_FIFO_EXECUTABLE"),
		codexHome:  os.Getenv("ATEAM_SETUP_FIFO_HOME"),
	}).Run(ctx)
	if err == nil {
		t.Fatal("setup succeeded, want FIFO rejection")
	}
	t.Log(err)
}

func assertSymlinkTarget(t *testing.T, path, wantTarget string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s mode = %v, want symlink", path, info.Mode())
	}
	gotTarget, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("read symlink: %v", err)
	}
	if gotTarget != wantTarget {
		t.Fatalf("symlink target = %q, want %q", gotTarget, wantTarget)
	}
}

func TestSetupCodexConfigPreflightFailuresDoNotWrite(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "malformed",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("model = [\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unreadable",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codexHome := t.TempDir()
			configPath := filepath.Join(codexHome, "config.toml")
			tt.setup(t, configPath)
			ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
			err := (&setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}).Run(ctx)
			if err == nil || !strings.Contains(err.Error(), configPath) {
				t.Fatalf("error = %v, want config path %s", err, configPath)
			}
			if _, err := os.Stat(filepath.Join(codexHome, "agents")); !os.IsNotExist(err) {
				t.Fatalf("agents directory should not be created, stat err = %v", err)
			}
			info, statErr := os.Stat(configPath)
			if statErr != nil {
				t.Fatalf("config changed or disappeared: %v", statErr)
			}
			if tt.name == "unreadable" {
				if !info.IsDir() {
					t.Fatalf("unreadable config was replaced: mode %v", info.Mode())
				}
				return
			}
			got, readErr := os.ReadFile(configPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "model = [\n" {
				t.Fatalf("malformed config changed: %q", got)
			}
		})
	}
}

func TestSetupCodexAgentConflictDoesNotWriteConfig(t *testing.T) {
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	initial := []byte("# untouched until all preflight checks pass\n")
	if err := os.WriteFile(configPath, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	agentsDir := filepath.Join(codexHome, "agents")
	if err := os.Mkdir(agentsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(agentsDir, "agent-teams-planner.toml")
	if err := os.WriteFile(conflict, []byte("local change\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	err := (&setupCodexKong{executable: compatibleCodexFixture(t), codexHome: codexHome}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, initial) {
		t.Fatalf("config changed before conflict error: got %q, want %q", got, initial)
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestSetupCodexFailsBeforeWritingWhenInstallIsIncompatible(t *testing.T) {
	codexHome := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	err := (&setupCodexKong{executable: filepath.Join(t.TempDir(), "missing"), codexHome: codexHome}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "official standalone") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "agents")); !os.IsNotExist(err) {
		t.Fatalf("agents directory should not be created, stat err = %v", err)
	}
}
