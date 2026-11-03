package verbs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// recBD records every bd call and answers the sentinel read with the sentinel.
type recBD struct {
	calls []string
}

func (r *recBD) Run(args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	return "", nil
}

func (r *recBD) RunJSON(dst any, args ...string) error {
	r.calls = append(r.calls, strings.Join(args, " "))
	if m, ok := dst.(*map[string]any); ok {
		*m = map[string]any{primeCapSentinelKey: primeCapSentinelBody}
	}
	return nil
}

func hostedSetupCtx(t *testing.T, cfg string, prime bool) (*cli.Context, *recBD, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cfg != "" {
		if err := os.WriteFile(filepath.Join(home, ".beads", "config.yaml"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if prime {
		if err := os.WriteFile(filepath.Join(home, ".beads", "PRIME.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rec := &recBD{}
	var errb bytes.Buffer
	return &cli.Context{Home: home, BD: rec, Stdout: &bytes.Buffer{}, Stderr: &errb}, rec, &errb
}

const fullHostedConfig = "# comment\nevents-journal: true\nprime:\n    max-memories: 1\n    max-memory-chars: \"1\"\n"

func TestEnsureHostedHomeSetup_AllSetMakesNoBDCall(t *testing.T) {
	ctx, rec, _ := hostedSetupCtx(t, fullHostedConfig, true)
	if err := ensureHostedHomeSetup(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("bd calls on the all-set path = %q, want none", rec.calls)
	}
}

func TestEnsureHostedHomeSetup_AllSetWithSentinelCheckOnlyReads(t *testing.T) {
	ctx, rec, _ := hostedSetupCtx(t, fullHostedConfig, true)
	if err := ensureHostedHomeSetup(ctx, true); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 1 || !strings.Contains(rec.calls[0], "memories") {
		t.Errorf("bd calls = %q, want one sentinel read", rec.calls)
	}
}

func TestEnsureHostedHomeSetup_InstallsEachMissingItem(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		prime   bool
		want    []string
		wantNot []string
	}{
		{"all missing", "", false,
			[]string{"config set events-journal true", "config set prime.max-memories 1", "config set prime.max-memory-chars 1"}, nil},
		{"only journal missing", "prime:\n    max-memories: 1\n    max-memory-chars: 1\n", true,
			[]string{"config set events-journal true"}, []string{"prime.max-memories"}},
		{"only caps missing", "events-journal: true\n", true,
			[]string{"config set prime.max-memories 1"}, []string{"events-journal"}},
		{"journal false", "events-journal: false\nprime.max-memories: 1\nprime.max-memory-chars: 1\n", true,
			[]string{"config set events-journal true"}, nil},
		{"only PRIME.md missing", fullHostedConfig, false, nil, []string{"config set"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec, _ := hostedSetupCtx(t, tc.cfg, tc.prime)
			if err := ensureHostedHomeSetup(ctx, false); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(rec.calls, "\n")
			for _, w := range tc.want {
				if !strings.Contains(joined, w) {
					t.Errorf("missing call %q in:\n%s", w, joined)
				}
			}
			for _, w := range tc.wantNot {
				if strings.Contains(joined, w) {
					t.Errorf("unexpected call %q in:\n%s", w, joined)
				}
			}
			if _, err := os.Stat(filepath.Join(ctx.Home, ".beads", "PRIME.md")); err != nil {
				t.Errorf("PRIME.md absent after setup: %v", err)
			}
			if !strings.Contains(joined, "memories") {
				t.Errorf("something was missing, so the sentinel must be checked:\n%s", joined)
			}
		})
	}
}

func TestEnsureHostedHomeSetupSoft_WarnsAndReturns(t *testing.T) {
	ctx, _, errb := hostedSetupCtx(t, "", true)
	ctx.BD = failBD{}
	ensureHostedHomeSetupSoft(ctx, false)
	if !strings.Contains(errb.String(), "WARNING: hosted workspace setup") {
		t.Errorf("stderr = %q, want a warning", errb.String())
	}
}

type failBD struct{}

func (failBD) Run(...string) (string, error) { return "", os.ErrPermission }
func (failBD) RunJSON(any, ...string) error  { return os.ErrPermission }

// firstFailBD fails the first bd call and succeeds afterward.
type firstFailBD struct {
	recBD
	failed bool
}

func (f *firstFailBD) Run(args ...string) (string, error) {
	if !f.failed {
		f.failed = true
		f.calls = append(f.calls, strings.Join(args, " "))
		return "", os.ErrPermission
	}
	return f.recBD.Run(args...)
}

func TestEnsureHostedHomeSetup_ContinuesPastFailedItem(t *testing.T) {
	ctx, _, _ := hostedSetupCtx(t, "", false)
	fb := &firstFailBD{}
	ctx.BD = fb
	err := ensureHostedHomeSetup(ctx, false)
	if err == nil || !strings.Contains(err.Error(), "set events-journal") {
		t.Fatalf("err = %v, want it to name the events-journal failure", err)
	}
	if _, serr := os.Stat(filepath.Join(ctx.Home, ".beads", "PRIME.md")); serr != nil {
		t.Errorf("PRIME.md not installed after an earlier item failed: %v", serr)
	}
	if !strings.Contains(strings.Join(fb.calls, "\n"), "prime.max-memories") {
		t.Errorf("caps not attempted after failure: %q", fb.calls)
	}
}

func TestReadBeadsConfigKeys_DeeperNestingReadsUnset(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	body := "prime:\n  other:\n    max-memories: 1\n  max-memory-chars: 1\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readBeadsConfigKeys(cfg)
	if v, ok := got["prime.max-memories"]; ok {
		t.Errorf("prime.max-memories = %q, want unset", v)
	}
}
