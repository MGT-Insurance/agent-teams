package verbs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// TestStewardLearningsServedFromHostedStore proves the steward loads its
// learnings from the hosted store once learnings-seed has run. Two scratch
// embedded bd workspaces stand in for the local and hosted stores; neither
// touches the real workspace.
func TestStewardLearningsServedFromHostedStore(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	t.Setenv("DSP_BEAD", "")
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

	const sentinel = "RULE: proof-sentinel body 7f3a9c"
	local := map[string]string{
		"steward:hot:proof-sentinel": sentinel,
		"steward:fresh:proof-fresh":  "RULE: proof fresh body",
		"steward:proof-cold":         "RULE: proof cold body",
		"steward:topics:host1":       "per-machine topic record",
	}
	for k, v := range local {
		if _, err := localBD.Run("remember", v, "--key", k); err != nil {
			t.Fatalf("remember %s: %v", k, err)
		}
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

	out.Reset()
	if err := (&learningsKong{Role: "steward"}).Run(ctx); err != nil {
		t.Fatalf("learnings steward: %v", err)
	}
	got := out.String()
	for _, w := range []string{"steward: 2 entries", "hot 1", "fresh 1", sentinel} {
		if !strings.Contains(got, w) {
			t.Errorf("learnings steward output missing %q\n%s", w, got)
		}
	}

	out.Reset()
	if err := (&recallKong{Role: "steward", Query: "proof-cold"}).Run(ctx); err != nil {
		t.Fatalf("recall steward: %v", err)
	}
	if !strings.Contains(out.String(), "steward:proof-cold") {
		t.Errorf("recall steward missing steward:proof-cold\n%s", out)
	}

	var hosted, stillLocal map[string]any
	if err := readMemoriesJSON(hostedBD, &hosted); err != nil {
		t.Fatal(err)
	}
	if err := readMemoriesJSON(localBD, &stillLocal); err != nil {
		t.Fatal(err)
	}
	for k := range hosted {
		if strings.HasPrefix(k, "steward:topics:") {
			t.Errorf("hosted store holds excluded key %q", k)
		}
	}
	if _, ok := stillLocal["steward:topics:host1"]; !ok {
		t.Error("local store lost steward:topics:host1")
	}
}
