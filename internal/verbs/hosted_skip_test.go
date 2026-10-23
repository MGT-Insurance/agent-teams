package verbs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// hostedHome returns a temp workspace home whose .beads/metadata.json carries
// metadata (the F1 shape from docs/hosted-beads.md).
func hostedHome(t *testing.T, metadata string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".beads", "metadata.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

const hostedMetadata = `{"dolt_mode":"server","dolt_server_host":"sandbox.example.com"}`

// noExecCtx builds a cli.Context whose bd client fails the test on any exec.
func noExecCtx(t *testing.T, home string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	client := bd.NewClientWithExec(home, func(name string, args ...string) ([]byte, []byte, error) {
		t.Fatalf("unexpected exec on hosted workspace: %s %v", name, args)
		return nil, nil, nil
	})
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	return &cli.Context{Home: home, BD: client, Stdout: out, Stderr: errBuf}, out, errBuf
}

func TestHostedSkip_Pull(t *testing.T) {
	ctx, out, errBuf := noExecCtx(t, hostedHome(t, hostedMetadata))
	if err := (&pullKong{}).Run(ctx); err != nil {
		t.Fatalf("pull returned error: %v", err)
	}
	if want := "ateam pull: skipped: hosted server sandbox.example.com; no local replica\n"; errBuf.String() != want {
		t.Errorf("stderr = %q, want %q", errBuf.String(), want)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
}

func TestHostedSkip_Sync(t *testing.T) {
	ctx, out, errBuf := noExecCtx(t, hostedHome(t, hostedMetadata))
	if err := (&syncKong{}).Run(ctx); err != nil {
		t.Fatalf("sync returned error: %v", err)
	}
	if want := "ateam sync: skipped: hosted server sandbox.example.com\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if errBuf.Len() != 0 {
		t.Errorf("stderr = %q, want empty", errBuf.String())
	}
}

func TestHostedSkip_RelayDoltPull(t *testing.T) {
	// A hosted home must return before guardedPull builds a real bd client;
	// a non-hosted home would exec the real bd, so only the hosted case is
	// asserted here (nil error, no process spawned).
	if err := defaultDoltPull(hostedHome(t, hostedMetadata)); err != nil {
		t.Fatalf("defaultDoltPull on hosted home returned error: %v", err)
	}
}

func TestHostedSkip_NotifyPushSkipped(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	pbd := &pushRecordingBD{}
	cmd := &notifyKong{
		ID:           "at-00q",
		File:         bodyFile,
		transportFor: fakeTransportFor(&fakeTransport{returnRef: "88"}, nil),
		labelAdd:     func(cli.BDRunner, string, string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(pbd)
	ctx.Home = hostedHome(t, hostedMetadata)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	for _, c := range pbd.calls {
		if strings.HasPrefix(c, "dolt push") {
			t.Errorf("hosted workspace must not push, got calls: %v", pbd.calls)
		}
	}
}

func TestHostedSkipHost_NonHostedKeepsBehavior(t *testing.T) {
	for name, md := range map[string]string{
		"embedded":     `{"dolt_mode":"embedded"}`,
		"loopback":     `{"dolt_mode":"server","dolt_server_host":"127.0.0.1"}`,
		"unparseable":  `{not json`,
		"server-empty": `{"dolt_mode":"server"}`,
	} {
		if host, ok := hostedSkipHost(hostedHome(t, md)); ok {
			t.Errorf("%s: hosted = true (host %q), want false", name, host)
		}
	}
	if _, ok := hostedSkipHost(""); ok {
		t.Error("empty home reported hosted")
	}
	if _, ok := hostedSkipHost(t.TempDir()); ok {
		t.Error("missing metadata.json reported hosted")
	}
}
