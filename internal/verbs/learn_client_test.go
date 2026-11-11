package verbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

const unreachableMsg = "failed to open database: Dolt server unreachable at host:3307: dial tcp: i/o timeout"

// learnFake is a cli.BDRunner that records every argv, serves a fixed memory
// map for the memories read, and fails every call with err when err is set.
type learnFake struct {
	calls    [][]string
	memories map[string]any
	err      error
	// failRunAt makes the Nth (0-based) Run call fail with err; -1 never.
	failRunAt int
	runs      int
}

func newLearnFake() *learnFake {
	return &learnFake{
		memories: map[string]any{
			"dri:hot:a":   "A",
			"dri:fresh:b": "B",
			"user:p":      "P",
		},
		failRunAt: -1,
	}
}

func (f *learnFake) Run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	n := f.runs
	f.runs++
	if f.err != nil && (f.failRunAt < 0 || f.failRunAt == n) {
		return "", f.err
	}
	return "", nil
}

func (f *learnFake) RunJSON(dst any, args ...string) error {
	f.calls = append(f.calls, args)
	if f.err != nil && f.failRunAt < 0 {
		return f.err
	}
	b, _ := json.Marshal(f.memories)
	return json.Unmarshal(b, dst)
}

func learnCtx(local, learn cli.BDRunner, home string) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	ctx, out, errb := makeCtx(local, home)
	ctx.Learn = learn
	return ctx, out, errb
}

func writeCloudHome(t *testing.T, home, cloud string) {
	t.Helper()
	cfg := "cloud_home = \"" + cloud + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNewLearnClient(t *testing.T) {
	local := &learnFake{}

	t.Run("cloud_home unset returns the same runner", func(t *testing.T) {
		if got := NewLearnClient(t.TempDir(), local); got != cli.BDRunner(local) {
			t.Errorf("got %T, want the local runner itself", got)
		}
	})
	t.Run("cloud_home naming the same dir or a symlink to it returns local", func(t *testing.T) {
		home := t.TempDir()
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(home, link); err != nil {
			t.Fatal(err)
		}
		for _, cloud := range []string{home, home + "/", link} {
			writeCloudHome(t, home, cloud)
			if got := NewLearnClient(home, local); got != cli.BDRunner(local) {
				t.Errorf("cloud_home %q: got %T, want the local runner", cloud, got)
			}
		}
	})
	t.Run("cloud_home naming another dir returns a different client", func(t *testing.T) {
		home := t.TempDir()
		writeCloudHome(t, home, t.TempDir())
		got := NewLearnClient(home, local)
		if got == cli.BDRunner(local) {
			t.Fatal("got the local runner, want a hosted client")
		}
		if _, ok := got.(*bd.Client); !ok {
			t.Errorf("got %T, want *bd.Client", got)
		}
	})
	t.Run("config error unrelated to cloud_home keeps learnings local", func(t *testing.T) {
		for _, cfg := range []string{"unknown_key = 1\n", "unknown_key = [\n", "work_runtime = \"bogus\"\n"} {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := NewLearnClient(home, local); got != cli.BDRunner(local) {
				t.Errorf("config %q: got %T, want the local runner", cfg, got)
			}
		}
	})
	t.Run("valid cloud_home survives an unrelated unknown key", func(t *testing.T) {
		home := t.TempDir()
		cfg := "unknown_key = 1\ncloud_home = \"" + t.TempDir() + "\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := NewLearnClient(home, local).(*bd.Client); !ok {
			t.Error("want a hosted *bd.Client")
		}
	})
	t.Run("bad cloud_home value stays a loud error", func(t *testing.T) {
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("unknown_key = 1\ncloud_home = \"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := NewLearnClient(home, local)
		if _, err := got.Run("memories"); err == nil || !strings.HasPrefix(err.Error(), "ateam: learnings store:") {
			t.Errorf("Run err = %v", err)
		}
	})
	t.Run("malformed config yields a failing runner", func(t *testing.T) {
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("cloud_home = [\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := NewLearnClient(home, local)
		if _, err := got.Run("memories"); err == nil || !strings.HasPrefix(err.Error(), "ateam: learnings store:") {
			t.Errorf("Run err = %v", err)
		}
		var dst any
		if err := got.RunJSON(&dst, "memories"); err == nil || !strings.HasPrefix(err.Error(), "ateam: learnings store:") {
			t.Errorf("RunJSON err = %v", err)
		}
	})
}

func TestLearnBDAndRemote(t *testing.T) {
	a, b := &learnFake{}, &learnFake{}
	ctx, _, _ := makeCtx(a, t.TempDir())
	if learnBD(ctx) != cli.BDRunner(a) || learnRemote(ctx) {
		t.Error("nil Learn: want ctx.BD and not remote")
	}
	ctx.Learn = a
	if learnRemote(ctx) {
		t.Error("Learn == BD must not be remote")
	}
	ctx.Learn = b
	if learnBD(ctx) != cli.BDRunner(b) || !learnRemote(ctx) {
		t.Error("Learn != BD: want ctx.Learn and remote")
	}
}

func TestIsStoreUnreachable(t *testing.T) {
	if !isStoreUnreachable(errors.New(unreachableMsg)) {
		t.Error("bd unreachable message not recognised")
	}
	if isStoreUnreachable(nil) || isStoreUnreachable(errors.New("boom")) {
		t.Error("false positive")
	}
}

// verbCase runs one routed learnings verb against ctx.
type verbCase struct {
	name string
	run  func(t *testing.T, ctx *cli.Context) error
}

func learnVerbCases(t *testing.T) []verbCase {
	file := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(file, []byte("body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []verbCase{
		{"learnings", func(t *testing.T, c *cli.Context) error { return runLearnings(c, "dri") }},
		{"recall", func(t *testing.T, c *cli.Context) error { return runRecall(c, "dri", "a") }},
		{"prime", func(t *testing.T, c *cli.Context) error { return runPrime(c) }},
		{"roles", func(t *testing.T, c *cli.Context) error { return runRoles(c) }},
		{"memories-json", func(t *testing.T, c *cli.Context) error { return runMemoriesJSON(c) }},
		{"condense-check", func(t *testing.T, c *cli.Context) error { return (&condenseCheckKong{Role: "dri"}).Run(c) }},
		{"condense", func(t *testing.T, c *cli.Context) error { return (&condenseKong{Role: "dri"}).Run(c) }},
		{"learn", func(t *testing.T, c *cli.Context) error {
			return (&learnKong{Role: "dri", Slug: "hot:x", File: file}).Run(c)
		}},
		{"forget", func(t *testing.T, c *cli.Context) error { return (&forgetKong{Role: "dri", Slug: "hot:x"}).Run(c) }},
		{"applied", func(t *testing.T, c *cli.Context) error { return (&appliedKong{Role: "dri", Slug: "x"}).Run(c) }},
		{"fresh-drain", func(t *testing.T, c *cli.Context) error { return (&freshDrainKong{Role: "dri"}).Run(c) }},
	}
}

var memRO = []string{"--readonly", "memories", "--json"}

// TestLearnVerbs_ArgvUnchangedWithoutCloudHome pins the exact bd argv each
// routed verb sends when Learn == BD (cloud_home unset). These literals are
// the argv the verbs sent before routing existed.
func TestLearnVerbs_ArgvUnchangedWithoutCloudHome(t *testing.T) {
	want := map[string][][]string{
		"learnings":      {memRO},
		"recall":         {memRO},
		"prime":          {memRO},
		"roles":          {memRO},
		"memories-json":  {memRO},
		"condense-check": {memRO},
		"condense":       {memRO},
		"learn":          {{"remember", "--key=dri:hot:x", "body"}},
		"forget":         {{"forget", "dri:hot:x"}},
		"applied":        {memRO, nil}, // second call carries a timestamp; checked below
		"fresh-drain":    {memRO, {"remember", "--key=dri:b", "B"}, {"forget", "dri:fresh:b"}},
	}
	for _, vc := range learnVerbCases(t) {
		t.Run(vc.name, func(t *testing.T) {
			f := newLearnFake()
			// Learn == BD: the same value, as NewLearnClient returns with no cloud_home.
			ctx, _, _ := learnCtx(f, NewLearnClient(t.TempDir(), f), t.TempDir())
			if learnRemote(ctx) {
				t.Fatal("setup: learnings must not be remote")
			}
			if err := vc.run(t, ctx); err != nil {
				t.Fatal(err)
			}
			got := f.calls
			exp := want[vc.name]
			if vc.name == "applied" {
				if len(got) != 2 || len(got[1]) != 3 || got[1][0] != "remember" || got[1][1] != "--key=applied:dri:x" ||
					!strings.HasPrefix(got[1][2], `{"count":1,"last_applied":"`) {
					t.Fatalf("applied calls = %v", got)
				}
				got = got[:1]
				exp = exp[:1]
			}
			if !reflect.DeepEqual(got, exp) {
				t.Errorf("argv = %v, want %v", got, exp)
			}
		})
	}
}

// TestLearnVerbs_RoutedToLearnOnly proves that with Learn != BD every routed
// verb touches only Learn, and the steward topic and prime-cap sentinel
// call sites touch only BD.
func TestLearnVerbs_RoutedToLearnOnly(t *testing.T) {
	for _, vc := range learnVerbCases(t) {
		t.Run(vc.name, func(t *testing.T) {
			local, hosted := newLearnFake(), newLearnFake()
			ctx, _, _ := learnCtx(local, hosted, t.TempDir())
			if err := vc.run(t, ctx); err != nil {
				t.Fatal(err)
			}
			if len(local.calls) != 0 {
				t.Errorf("local BD was called: %v", local.calls)
			}
			if len(hosted.calls) == 0 {
				t.Error("Learn was not called")
			}
		})
	}

	t.Run("sentinel and steward topics stay on BD", func(t *testing.T) {
		local, hosted := newLearnFake(), newLearnFake()
		ctx, _, _ := learnCtx(local, hosted, t.TempDir())
		if err := installSentinelMemory(ctx); err != nil {
			t.Fatal(err)
		}
		isKnownStewardTopic(ctx, "thread:1")
		if len(local.calls) == 0 {
			t.Error("BD was not called")
		}
		if len(hosted.calls) != 0 {
			t.Errorf("Learn was called: %v", hosted.calls)
		}
	})
}

func hostedCtx(t *testing.T, f *learnFake) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	writeCloudHome(t, home, "/hosted/ws")
	ctx, out, _ := learnCtx(newLearnFake(), f, home)
	return ctx, out
}

func TestLearnVerbs_Unreachable(t *testing.T) {
	const store = "/hosted/ws"

	t.Run("learnings prints the continue line and exits 0", func(t *testing.T) {
		f := newLearnFake()
		f.err = errors.New(unreachableMsg)
		ctx, out := hostedCtx(t, f)
		if err := runLearnings(ctx, "dri"); err != nil {
			t.Fatal(err)
		}
		want := "[learnings store unreachable (" + store + "): continuing WITHOUT dri learnings]\n"
		if out.String() != want {
			t.Errorf("stdout = %q, want %q", out.String(), want)
		}
	})
	t.Run("prime prints the continue line for user and exits 0", func(t *testing.T) {
		f := newLearnFake()
		f.err = errors.New(unreachableMsg)
		ctx, out := hostedCtx(t, f)
		if err := runPrime(ctx); err != nil {
			t.Fatal(err)
		}
		want := "[learnings store unreachable (" + store + "): continuing WITHOUT user learnings]\n"
		if out.String() != want {
			t.Errorf("stdout = %q, want %q", out.String(), want)
		}
	})
	t.Run("other reads return the unreachable error", func(t *testing.T) {
		for _, vc := range learnVerbCases(t) {
			switch vc.name {
			case "recall", "roles", "memories-json", "condense-check", "condense":
			default:
				continue
			}
			f := newLearnFake()
			f.err = errors.New(unreachableMsg)
			ctx, _ := hostedCtx(t, f)
			err := vc.run(t, ctx)
			if err == nil || !strings.Contains(err.Error(), "learnings store unreachable ("+store+")") {
				t.Errorf("%s: err = %v", vc.name, err)
			}
		}
	})
	t.Run("writes say may not be saved", func(t *testing.T) {
		for _, vc := range learnVerbCases(t) {
			switch vc.name {
			case "learn", "forget", "applied":
			default:
				continue
			}
			f := newLearnFake()
			f.err = errors.New(unreachableMsg)
			ctx, _ := hostedCtx(t, f)
			err := vc.run(t, ctx)
			want := "ateam " + vc.name + ": learnings store unreachable (" + store + "): may not be saved; retry later"
			if err == nil || err.Error() != want {
				t.Errorf("%s: err = %v, want %q", vc.name, err, want)
			}
			if code := cli.ExitCode(err); code == 0 {
				t.Errorf("%s: exit code 0", vc.name)
			}
		}
	})
	t.Run("fresh-drain stops at the first failed write and reports the count", func(t *testing.T) {
		f := newLearnFake()
		f.memories = map[string]any{"dri:fresh:a": "A", "dri:fresh:b": "B", "dri:fresh:c": "C"}
		f.err = errors.New(unreachableMsg)
		f.failRunAt = 2 // run 0,1 = remember+forget of a; run 2 = remember b fails
		ctx, _ := hostedCtx(t, f)
		err := (&freshDrainKong{Role: "dri"}).Run(ctx)
		want := "ateam fresh-drain: learnings store unreachable (" + store + "): may not be saved; retry later (moved 1 keys)"
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
		// read + remember a + forget a + failed remember b; nothing after.
		if len(f.calls) != 4 {
			t.Errorf("calls = %v, want 4 (stops at first failed write)", f.calls)
		}
	})
}

// hostedTimeoutMsg is the literal bd output of a live hosted read that timed
// out; it carries none of bd's "Dolt server unreachable" text.
const hostedTimeoutMsg = "[mysql] 2026/11/10 12:26:30 packets.go:58 read tcp 192.168.1.155:50420->44.243.111.100:3306: i/o timeout\n" +
	`Error: failed to open database: failed to check if database "beads_eric_lloyd_at" exists on server mgt-spike.dbs.hosted.doltdb.com:3306: invalid connection`

func TestLearnVerbs_HostedConnectionFailureFailsOpen(t *testing.T) {
	const store = "/hosted/ws"
	for _, msg := range []string{hostedTimeoutMsg, "dial tcp 1.2.3.4:3306: connect: connection refused", "dial tcp: lookup h: no such host"} {
		f := newLearnFake()
		f.err = errors.New(msg)
		ctx, out := hostedCtx(t, f)
		if err := runLearnings(ctx, "dri"); err != nil {
			t.Fatalf("%q: learnings err = %v, want exit 0", msg, err)
		}
		want := "[learnings store unreachable (" + store + "): continuing WITHOUT dri learnings]\n"
		if out.String() != want {
			t.Errorf("stdout = %q, want %q", out.String(), want)
		}
	}
	// A write fails with the may-not-be-saved message.
	f := newLearnFake()
	f.err = errors.New(hostedTimeoutMsg)
	ctx, _ := hostedCtx(t, f)
	err := (&forgetKong{Role: "dri", Slug: "hot:x"}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "may not be saved; retry later") {
		t.Errorf("forget err = %v", err)
	}
}

// breakerOpenMsg is the literal bd output while its circuit breaker is open
// after repeated hosted failures.
const breakerOpenMsg = "Error: failed to open database: dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)"

func TestLearnVerbs_BreakerOpenFailsOpen(t *testing.T) {
	const store = "/hosted/ws"
	f := newLearnFake()
	f.err = errors.New(breakerOpenMsg)
	ctx, out := hostedCtx(t, f)
	if err := runLearnings(ctx, "dri"); err != nil {
		t.Fatalf("learnings err = %v, want exit 0", err)
	}
	want := "[learnings store unreachable (" + store + "): continuing WITHOUT dri learnings]\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}

	file := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(file, []byte("body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f = newLearnFake()
	f.err = errors.New(breakerOpenMsg)
	ctx, _ = hostedCtx(t, f)
	err := (&learnKong{Role: "dri", Slug: "hot:x", File: file}).Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "may not be saved; retry later") {
		t.Errorf("learn err = %v, want may not be saved; retry later", err)
	}
}

func TestLearnVerbs_NonConnectionErrorStillFails(t *testing.T) {
	raw := errors.New(`Error: database "x" not found`)
	f := newLearnFake()
	f.err = raw
	ctx, out := hostedCtx(t, f)
	if err := runLearnings(ctx, "dri"); err != raw {
		t.Errorf("err = %v, want the raw error", err)
	}
	if strings.Contains(out.String(), "learnings store unreachable") {
		t.Errorf("stdout = %q, want no unreachable line", out.String())
	}
}

// TestLearnVerbs_UnreachableKeepsTodaysPathWhenLocal proves the same error
// with learnings kept local (Learn == BD) is returned unchanged.
func TestLearnVerbs_UnreachableKeepsTodaysPathWhenLocal(t *testing.T) {
	raw := errors.New(unreachableMsg)
	for _, vc := range learnVerbCases(t) {
		t.Run(vc.name, func(t *testing.T) {
			f := newLearnFake()
			f.err = raw
			home := t.TempDir()
			writeCloudHome(t, home, "/hosted/ws") // configured, but ctx.Learn == ctx.BD
			ctx, out, _ := learnCtx(f, f, home)
			err := vc.run(t, ctx)
			if err != raw {
				t.Errorf("err = %v, want the raw bd error", err)
			}
			if strings.Contains(out.String(), "learnings store unreachable") {
				t.Errorf("stdout = %q, want no unreachable line", out.String())
			}
		})
	}
}
