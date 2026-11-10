package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// seedStore is a fake bd workspace holding memories. It records every Run call
// and applies "import <file>" memory lines to its map.
type seedStore struct {
	mem       map[string]string
	runs      [][]string
	imported  []string // raw import file contents, snapshotted inside Run
	importErr error
}

func (s *seedStore) Run(args ...string) (string, error) {
	s.runs = append(s.runs, append([]string(nil), args...))
	if len(args) == 2 && args[0] == "import" {
		data, err := os.ReadFile(args[1])
		if err != nil {
			return "", err
		}
		s.imported = append(s.imported, string(data))
		if s.importErr != nil {
			return "", s.importErr
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var l seedMemoryLine
			if err := json.Unmarshal([]byte(line), &l); err != nil {
				return "", err
			}
			s.mem[l.Key] = l.Value
		}
	}
	return "", nil
}

func (s *seedStore) RunJSON(dst any, args ...string) error {
	s.runs = append(s.runs, append([]string(nil), args...))
	raw, err := json.Marshal(s.mem)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func (s *seedStore) importCalls() int { return len(s.imported) }

func seedCtx(t *testing.T, cloudHome string, local, hosted *seedStore) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	t.Setenv("DSP_BEAD", "")
	home := t.TempDir()
	if cloudHome != "" {
		cfg := fmt.Sprintf("cloud_home = %q\n", cloudHome)
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: home, BD: local, Learn: hosted, Stdout: out, Stderr: &bytes.Buffer{}}
	return ctx, out
}

func localMemories() map[string]string {
	return map[string]string{
		"dri:hot:a":               "dri a",
		"planner:fresh:b":         "planner b <&> \"q\"\nline2",
		"implementer:cold:c":      "same value",
		"tester:proj:d":           "tester d",
		"user:pref":               "user pref",
		"applied:implementer:c":   `{"count":2}`,
		"implementer:hot:differs": "local version",
		"000-prime-cap-sentinel":  "x",
		"schema_version":          "7",
		"steward:topic:1":         "s",
		"misc":                    "m",
		"other:thing":             "o",
	}
}

func TestSeedRefusals(t *testing.T) {
	t.Run("DSP_BEAD set", func(t *testing.T) {
		local, hosted := &seedStore{mem: localMemories()}, &seedStore{mem: map[string]string{}}
		ctx, _ := seedCtx(t, "/somewhere/hosted", local, hosted)
		t.Setenv("DSP_BEAD", "at-123")
		err := (&learningsSeedKong{}).Run(ctx)
		if !isUsage(err) {
			t.Fatalf("want usage error, got %v", err)
		}
		if len(local.runs)+len(hosted.runs) != 0 {
			t.Fatalf("expected no bd calls, got %v %v", local.runs, hosted.runs)
		}
	})
	t.Run("cloud_home unset", func(t *testing.T) {
		local := &seedStore{mem: localMemories()}
		hosted := &seedStore{mem: map[string]string{}}
		ctx, _ := seedCtx(t, "", local, hosted)
		err := (&learningsSeedKong{}).Run(ctx)
		if !isUsage(err) {
			t.Fatalf("want usage error, got %v", err)
		}
		if len(local.runs)+len(hosted.runs) != 0 {
			t.Fatalf("expected no bd calls")
		}
	})
	t.Run("same workspace", func(t *testing.T) {
		local := &seedStore{mem: localMemories()}
		ctx, _ := seedCtx(t, "ignored", local, local)
		// cloud_home names this very workspace; Learn == BD
		if err := os.WriteFile(filepath.Join(ctx.Home, "config.toml"), []byte(fmt.Sprintf("cloud_home = %q\n", ctx.Home)), 0o600); err != nil {
			t.Fatal(err)
		}
		err := (&learningsSeedKong{}).Run(ctx)
		if !isUsage(err) {
			t.Fatalf("want usage error, got %v", err)
		}
		if len(local.runs) != 0 {
			t.Fatalf("expected no bd calls, got %v", local.runs)
		}
	})
}

func TestSeedCopiesMissingKeysInOneImport(t *testing.T) {
	local := &seedStore{mem: localMemories()}
	hosted := &seedStore{mem: map[string]string{
		"implementer:cold:c":      "same value",
		"implementer:hot:differs": "hosted version",
		"planner:fresh:cloud":     "cloud only",
	}}
	ctx, out := seedCtx(t, "/somewhere/hosted", local, hosted)

	if err := (&learningsSeedKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if hosted.importCalls() != 1 {
		t.Fatalf("want 1 import, got %d", hosted.importCalls())
	}
	for _, r := range local.runs {
		if r[0] == "import" {
			t.Fatalf("import ran against local")
		}
	}

	want := ""
	// byte-exact expected lines, in sorted key order, with raw (unescaped-HTML) values
	want += `{"_type":"memory","key":"applied:implementer:c","value":"{\"count\":2}"}` + "\n"
	want += `{"_type":"memory","key":"dri:hot:a","value":"dri a"}` + "\n"
	want += `{"_type":"memory","key":"planner:fresh:b","value":"planner b <&> \"q\"\nline2"}` + "\n"
	want += `{"_type":"memory","key":"tester:proj:d","value":"tester d"}` + "\n"
	want += `{"_type":"memory","key":"user:pref","value":"user pref"}` + "\n"
	if hosted.imported[0] != want {
		t.Fatalf("import file mismatch\n got: %q\nwant: %q", hosted.imported[0], want)
	}

	if hosted.mem["implementer:hot:differs"] != "hosted version" {
		t.Fatal("hosted value was overwritten")
	}
	for _, k := range []string{"000-prime-cap-sentinel", "schema_version", "steward:topic:1", "misc", "other:thing"} {
		if _, ok := hosted.mem[k]; ok {
			t.Fatalf("out-of-scope key %q was written", k)
		}
	}

	got := out.String()
	for _, w := range []string{
		"learnings-seed dri: local 1 hosted 0 to-write 1 kept-differs 0\n",
		"learnings-seed planner: local 1 hosted 1 to-write 1 kept-differs 0\n",
		"learnings-seed implementer: local 2 hosted 2 to-write 0 kept-differs 1\n",
		"learnings-seed tester: local 1 hosted 0 to-write 1 kept-differs 0\n",
		"learnings-seed reviewer: local 0 hosted 0 to-write 0 kept-differs 0\n",
		"learnings-seed user: local 1 hosted 0 to-write 1 kept-differs 0\n",
		"learnings-seed applied: local 1 hosted 0 to-write 1 kept-differs 0\n",
		"learnings-seed: kept hosted implementer:hot:differs (differs)\n",
		"learnings-seed total: local 7 hosted 3 to-write 5 kept-differs 1\n",
		"learnings-seed: imported 5 keys in ",
	} {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q\n%s", w, got)
		}
	}
	if strings.Contains(got, "[dry-run]") {
		t.Error("unexpected dry-run prefix")
	}

	// Second run: nothing missing, no import.
	out.Reset()
	if err := (&learningsSeedKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if hosted.importCalls() != 1 {
		t.Fatalf("second run called import: %d", hosted.importCalls())
	}
	if !strings.Contains(out.String(), "learnings-seed total: local 7 hosted 8 to-write 0 kept-differs 1\n") {
		t.Errorf("second run output:\n%s", out.String())
	}
}

func TestSeedDryRunWritesNothing(t *testing.T) {
	local := &seedStore{mem: localMemories()}
	hosted := &seedStore{mem: map[string]string{"implementer:hot:differs": "hosted version"}}
	ctx, out := seedCtx(t, "/somewhere/hosted", local, hosted)

	if err := (&learningsSeedKong{DryRun: true}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if hosted.importCalls() != 0 {
		t.Fatal("dry-run called import")
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if !strings.HasPrefix(line, "[dry-run] ") {
			t.Errorf("line lacks [dry-run] prefix: %q", line)
		}
	}
	if !strings.Contains(out.String(), "[dry-run] learnings-seed total: local 7 hosted 1 to-write 6 kept-differs 1\n") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestSeedImportFailure(t *testing.T) {
	local := &seedStore{mem: localMemories()}
	hosted := &seedStore{mem: map[string]string{}, importErr: errors.New("boom")}
	ctx, _ := seedCtx(t, "/somewhere/hosted", local, hosted)
	err := (&learningsSeedKong{}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want import error, got %v", err)
	}
}

func TestSeedHostedUnreachable(t *testing.T) {
	local := &seedStore{mem: localMemories()}
	ctx, _ := seedCtx(t, "/somewhere/hosted", local, nil)
	ctx.Learn = &errRunner{err: errors.New("failed to open database: Dolt server unreachable at h:1")}
	err := (&learningsSeedKong{}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "learnings store unreachable") {
		t.Fatalf("want unreachable error, got %v", err)
	}
}

// TestSeedEmbeddedIntegration runs the verb against two scratch embedded bd
// workspaces: a "local" one holding the source and a "hosted" one standing in
// for the hosted store. It never touches the real workspace or hosted.
func TestSeedEmbeddedIntegration(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	t.Setenv("DSP_BEAD", "")
	// HOME is redirected so bd neither reads the user's ~/.beads config (whose
	// sync.remote makes init clone the real agent-teams database) nor writes
	// outside the temp dir.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BEADS_DIR", "")
	root := t.TempDir()
	localDir, hostedDir := filepath.Join(root, "local"), filepath.Join(root, "hosted")
	for _, d := range []string{localDir, hostedDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", d, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		cmd := exec.Command("bd", "init", "--non-interactive", "--prefix", "at", "--skip-hooks", "--skip-agents")
		cmd.Dir = d
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("bd init failed (%v): %s", err, out)
		}
	}
	localBD, hostedBD := bd.NewClient(localDir), bd.NewClient(hostedDir)
	for _, c := range []*bd.Client{localBD, hostedBD} {
		var fresh map[string]any
		if err := readMemoriesJSON(c, &fresh); err != nil {
			t.Fatal(err)
		}
		for k := range fresh {
			if k != "schema_version" {
				t.Fatalf("fresh scratch db already holds memory %q: bd init cloned a remote", k)
			}
		}
	}
	local := localMemories()
	local["implementer:hot:multi"] = "line one\nline two\t\"quoted\" <tag> & \\ end"
	for k, v := range local {
		if _, err := localBD.Run("remember", v, "--key", k); err != nil {
			t.Fatalf("remember %s: %v", k, err)
		}
	}
	if _, err := hostedBD.Run("remember", "hosted version", "--key", "implementer:hot:differs"); err != nil {
		t.Fatal(err)
	}

	cfg := fmt.Sprintf("cloud_home = %q\n", hostedDir)
	if err := os.WriteFile(filepath.Join(localDir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: localDir, BD: localBD, Learn: hostedBD, Stdout: out, Stderr: &bytes.Buffer{}}

	if err := (&learningsSeedKong{}).Run(ctx); err != nil {
		t.Fatalf("seed: %v\n%s", err, out)
	}
	t.Logf("first run:\n%s", out)

	var raw map[string]any
	if err := readMemoriesJSON(hostedBD, &raw); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			got[k] = s
		}
	}
	for _, k := range []string{"dri:hot:a", "planner:fresh:b", "implementer:cold:c", "tester:proj:d", "user:pref", "applied:implementer:c", "implementer:hot:multi"} {
		if got[k] != local[k] {
			t.Errorf("key %s: hosted %q, want %q", k, got[k], local[k])
		}
	}
	if got["implementer:hot:differs"] != "hosted version" {
		t.Errorf("differing key overwritten: %q", got["implementer:hot:differs"])
	}
	for _, k := range []string{"000-prime-cap-sentinel", "schema_version", "steward:topic:1", "misc", "other:thing"} {
		if _, ok := got[k]; ok {
			t.Errorf("out-of-scope key %q copied", k)
		}
	}
	if !strings.Contains(out.String(), "kept hosted implementer:hot:differs (differs)") {
		t.Errorf("missing kept line:\n%s", out)
	}

	out.Reset()
	if err := (&learningsSeedKong{}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("second run:\n%s", out)
	if !strings.Contains(out.String(), "to-write 0") || !strings.Contains(out.String(), "imported 0 keys") {
		t.Errorf("second run should write 0:\n%s", out)
	}
}
