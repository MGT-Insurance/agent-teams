package verbs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/initiative"
)

// followHelperEnv switches the test binary into the detached-follower stand-in
// that TestMailFollowSpawnDetaches starts through followSpawn.
const followHelperEnv = "FOLLOW_SPAWN_HELPER_DIR"

func init() {
	dir := os.Getenv(followHelperEnv)
	if dir == "" || len(os.Args) < 4 || os.Args[len(os.Args)-1] != "--child" {
		return
	}
	sid, _ := syscall.Getsid(0)
	stdin, _ := os.Stdin.Stat()
	null, _ := os.Stat(os.DevNull)
	_ = os.WriteFile(filepath.Join(dir, "child.txt"), []byte(strings.Join([]string{
		strconv.Itoa(os.Getpid()), strconv.Itoa(sid), strconv.FormatBool(stdin != nil && null != nil && os.SameFile(stdin, null)),
		os.Getenv("AGENT_TEAMS_HOME"),
	}, "\n")), 0o644)
	os.Stdout.WriteString("child-stdout\n")
	os.Exit(0)
}

type followCall struct {
	args []string
}

// followHarness drives mailFollowKong.follow with scripted bd runs. Each
// script step feeds lines and returns stderr; after the last step the context
// is cancelled so follow returns.
type followHarness struct {
	t       *testing.T
	home    string
	stderr  bytes.Buffer
	calls   []followCall
	sleeps  []time.Duration
	cancel  context.CancelFunc
	steps   []followStep
	headOut string
	headErr error
	headQ   []string
	where   map[string]string // initiative id -> where: value
	lookups []string
	alive   map[int]bool
}

type followStep struct {
	lines  []string
	stderr string
}

func newFollowHarness(t *testing.T, hosted bool) *followHarness {
	t.Helper()
	t.Setenv("AGENT_TEAMS_HOST", "laptop-a")
	home := t.TempDir()
	if hosted {
		followWriteHosted(t, home)
	}
	return &followHarness{t: t, home: home, headOut: `[{"head":10}]`, where: map[string]string{}, alive: map[int]bool{}}
}

func followWriteHosted(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"dolt_mode":"server","dolt_server_host":"db.example.com"}`
	if err := os.WriteFile(filepath.Join(home, ".beads", "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
}

func followWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *followHarness) writeSeq(n string) {
	h.t.Helper()
	followWriteFile(h.t, filepath.Join(h.home, "mailbox", "host.seq"), n)
}

func (h *followHarness) seq() string {
	b, err := os.ReadFile(filepath.Join(h.home, "mailbox", "host.seq"))
	if err != nil {
		return ""
	}
	return string(b)
}

func (h *followHarness) rungFor(id string) bool {
	_, err := os.Stat(filepath.Join(h.home, "mailbox", id+".wake"))
	return err == nil
}

func (h *followHarness) rung() bool { return h.rungFor("at-xyz") }

func (h *followHarness) watcher(id string, pid int, alive bool) {
	h.t.Helper()
	followWriteFile(h.t, filepath.Join(h.home, "mailbox", id+".watcher.pid"), strconv.Itoa(pid)+"\tsess")
	h.alive[pid] = alive
}

func (h *followHarness) seams(c *mailFollowKong) {
	c.headQuery = func(q string) (string, error) {
		h.headQ = append(h.headQ, q)
		return h.headOut, h.headErr
	}
	c.sleep = func(_ context.Context, d time.Duration) { h.sleeps = append(h.sleeps, d) }
	if c.now == nil {
		c.now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }
	}
	c.lookup = func(id string) (initiative.Fields, error) {
		h.lookups = append(h.lookups, id)
		w, ok := h.where[id]
		if !ok {
			return initiative.Fields{}, errors.New("no such initiative")
		}
		return initiative.Fields{Where: w}, nil
	}
	c.pidAlive = func(pid int) bool { return h.alive[pid] }
}

func (h *followHarness) cliCtx() *cli.Context {
	return &cli.Context{Home: h.home, Stderr: &h.stderr, Stdout: &bytes.Buffer{}}
}

func (h *followHarness) run(c *mailFollowKong) error {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	defer cancel()
	c.runBD = func(_ context.Context, args []string, onLine func(string)) (string, error) {
		h.calls = append(h.calls, followCall{args: args})
		if len(h.steps) == 0 {
			cancel()
			return "", nil
		}
		s := h.steps[0]
		h.steps = h.steps[1:]
		for _, l := range s.lines {
			onLine(l)
		}
		if len(h.steps) == 0 {
			cancel()
		}
		return s.stderr, os.ErrClosed
	}
	h.seams(c)
	return c.follow(ctx, h.cliCtx())
}

func followRowJSON(seq, ts, op, typ, assignee string) string {
	return `{"seq":` + seq + `,"ts":"` + ts + `","op":"` + op + `","issue":{"issue_type":"` + typ + `","assignee":"` + assignee + `"}}`
}

func followMsg(seq, assignee string) string {
	return followRowJSON(seq, "2026-10-23T13:00:00Z", "create", "message", assignee)
}

func TestMailFollowNotHostedExitsZeroAndSpawnsNothing(t *testing.T) {
	h := newFollowHarness(t, false)
	spawned := false
	c := &mailFollowKong{spawn: func(*cli.Context) error { spawned = true; return nil }}
	if err := c.Run(h.cliCtx()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawned || h.stderr.Len() != 0 {
		t.Fatalf("non-hosted must be inert: spawned=%v stderr=%q", spawned, h.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(h.home, "mailbox")); err == nil {
		t.Fatal("non-hosted must not create the mailbox")
	}
}

func TestMailFollowHostFollowOffIsInertOnHostedWorkspace(t *testing.T) {
	h := newFollowHarness(t, true)
	t.Setenv("AGENT_TEAMS_HOST_FOLLOW", "off")
	c := &mailFollowKong{spawn: func(*cli.Context) error {
		t.Fatal("AGENT_TEAMS_HOST_FOLLOW=off must not spawn")
		return nil
	}}
	c.runBD = func(context.Context, []string, func(string)) (string, error) {
		t.Fatal("AGENT_TEAMS_HOST_FOLLOW=off must not run bd")
		return "", nil
	}
	if err := c.Run(h.cliCtx()); err != nil || h.stderr.Len() != 0 {
		t.Fatalf("Run err=%v stderr=%q", err, h.stderr.String())
	}
	// Any other value keeps today's behavior.
	t.Setenv("AGENT_TEAMS_HOST_FOLLOW", "on")
	n := 0
	c2 := &mailFollowKong{spawn: func(*cli.Context) error { n++; return nil }}
	if err := c2.Run(h.cliCtx()); err != nil || n != 1 {
		t.Fatalf("value 'on': err=%v spawns=%d", err, n)
	}
}

func TestMailFollowHostedParentSpawnsOnceAndDoesNotFollow(t *testing.T) {
	h := newFollowHarness(t, true)
	n := 0
	c := &mailFollowKong{spawn: func(*cli.Context) error { n++; return nil }}
	c.runBD = func(context.Context, []string, func(string)) (string, error) {
		t.Fatal("the parent invocation must not run bd")
		return "", nil
	}
	if err := c.Run(h.cliCtx()); err != nil || n != 1 {
		t.Fatalf("Run err=%v spawns=%d", err, n)
	}
}

func TestMailFollowSpawnDetaches(t *testing.T) {
	h := newFollowHarness(t, true)
	out := t.TempDir()
	t.Setenv(followHelperEnv, out)
	start := time.Now()
	if err := followSpawn(h.cliCtx()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("spawn waited on the child")
	}
	var got string
	for i := 0; i < 200 && got == ""; i++ {
		if b, err := os.ReadFile(filepath.Join(out, "child.txt")); err == nil && strings.Count(string(b), "\n") == 3 {
			got = string(b)
		} else {
			time.Sleep(25 * time.Millisecond)
		}
	}
	if got == "" {
		t.Fatal("child never ran")
	}
	f := strings.Split(got, "\n")
	if sid, _ := strconv.Atoi(f[1]); sid != mustAtoi(t, f[0]) {
		t.Errorf("child is not a session leader: pid=%s sid=%s", f[0], f[1])
	}
	if f[2] != "true" {
		t.Errorf("child stdin is not /dev/null")
	}
	if f[3] != h.home {
		t.Errorf("child AGENT_TEAMS_HOME=%q want %q", f[3], h.home)
	}
	var logBody []byte
	for i := 0; i < 200 && !strings.Contains(string(logBody), "child-stdout"); i++ {
		logBody, _ = os.ReadFile(filepath.Join(h.home, "debug", "mail-follow.log"))
		time.Sleep(25 * time.Millisecond)
	}
	if !strings.Contains(string(logBody), "child-stdout") {
		t.Errorf("child output not appended to mail-follow.log: %q", logBody)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMailFollowRingDecisions(t *testing.T) {
	cases := []struct {
		name       string
		assignee   string
		where      map[string]string
		wantRing   bool
		wantLookup bool
	}{
		{"local where host:", "at-a", map[string]string{"at-a": "host:laptop-a"}, true, true},
		{"remote where host:", "at-a", map[string]string{"at-a": "host:sandbox-1"}, false, true},
		{"empty where", "at-a", map[string]string{"at-a": ""}, true, true},
		{"run where (reserved)", "at-a", map[string]string{"at-a": "run:bead-1"}, true, true},
		{"lookup failure", "at-gone", map[string]string{}, true, true},
		{"steward skips lookup", "steward", map[string]string{}, true, false},
		{"unsafe assignee", "../evil", map[string]string{}, false, false},
		{"empty assignee", "", map[string]string{}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFollowHarness(t, true)
			h.writeSeq("10")
			h.where = tc.where
			h.steps = []followStep{{lines: []string{followMsg("11", tc.assignee)}}}
			if err := h.run(&mailFollowKong{}); err != nil {
				t.Fatal(err)
			}
			wakes, _ := filepath.Glob(filepath.Join(h.home, "mailbox", "*.wake"))
			if rung := len(wakes) > 0; rung != tc.wantRing {
				t.Fatalf("wake files=%v want ring=%v", wakes, tc.wantRing)
			}
			if tc.wantRing && !h.rungFor(tc.assignee) {
				t.Fatalf("doorbell for %s missing: %v", tc.assignee, wakes)
			}
			if _, err := os.Stat(filepath.Join(h.home, "evil.wake")); err == nil {
				t.Fatal("unsafe assignee escaped the mailbox")
			}
			if looked := len(h.lookups) > 0; looked != tc.wantLookup {
				t.Fatalf("lookups=%v want lookup=%v", h.lookups, tc.wantLookup)
			}
			if h.seq() != "11" {
				t.Fatalf("host.seq=%q want 11 (advances on every row)", h.seq())
			}
		})
	}
}

func TestMailFollowRingsOnlyForMessageCreate(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"create message", followRowJSON("11", "2026-10-23T13:00:00Z", "create", "message", "at-xyz"), true},
		{"update op", followRowJSON("11", "2026-10-23T13:00:00Z", "update", "message", "at-xyz"), false},
		{"comment op", followRowJSON("11", "2026-10-23T13:00:00Z", "comment", "message", "at-xyz"), false},
		{"close op", followRowJSON("11", "2026-10-23T13:00:00Z", "close", "message", "at-xyz"), false},
		{"task type", followRowJSON("11", "2026-10-23T13:00:00Z", "create", "task", "at-xyz"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFollowHarness(t, true)
			h.writeSeq("10")
			h.steps = []followStep{{lines: []string{tc.line}}}
			if err := h.run(&mailFollowKong{}); err != nil {
				t.Fatal(err)
			}
			if got := h.rung(); got != tc.want {
				t.Fatalf("rung=%v want %v", got, tc.want)
			}
			if h.seq() != "11" {
				t.Fatalf("checkpoint=%q want 11 (advances on every row)", h.seq())
			}
		})
	}
}

func TestMailFollowUsesOneHostCheckpointNotPerInitiative(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("10")
	h.steps = []followStep{{lines: []string{followMsg("11", "at-a"), followMsg("12", "at-b")}}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if h.seq() != "12" || !h.rungFor("at-a") || !h.rungFor("at-b") {
		t.Fatalf("seq=%q a=%v b=%v", h.seq(), h.rungFor("at-a"), h.rungFor("at-b"))
	}
	seqs, _ := filepath.Glob(filepath.Join(h.home, "mailbox", "*.seq"))
	if len(seqs) != 1 || filepath.Base(seqs[0]) != "host.seq" {
		t.Fatalf("checkpoint files=%v want only host.seq", seqs)
	}
}

func TestMailFollowSkipsUnparseableLineAndKeepsGoing(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("10")
	h.steps = []followStep{{lines: []string{`{"seq": bogus`, followMsg("11", "at-xyz")}}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if !h.rung() || !strings.Contains(h.stderr.String(), "unparseable") {
		t.Fatalf("rung=%v stderr=%q", h.rung(), h.stderr.String())
	}
}

func TestMailFollowReconnectsFromCheckpointWithBackoff(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("10")
	h.steps = []followStep{
		{lines: []string{followRowJSON("11", "2026-10-23T13:00:00Z", "update", "task", "x")}},
		{}, // exits with no row: backoff doubles
		{},
	}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if len(h.calls) != 3 {
		t.Fatalf("calls=%d want 3", len(h.calls))
	}
	wantSince := []string{"10", "11", "11"}
	for i, c := range h.calls {
		if got := c.args[len(c.args)-1]; got != wantSince[i] {
			t.Errorf("call %d --since %s want %s (args %v)", i, got, wantSince[i], c.args)
		}
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	if !reflect.DeepEqual(h.sleeps, want) {
		t.Fatalf("sleeps=%v want %v", h.sleeps, want)
	}
}

func TestMailFollowBackoffCapsAtThirtySeconds(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	for i := 0; i < 8; i++ {
		h.steps = append(h.steps, followStep{})
	}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	last := h.sleeps[len(h.sleeps)-1]
	if last != 30*time.Second {
		t.Fatalf("last backoff %v want 30s (all %v)", last, h.sleeps)
	}
}

func TestMailFollowTruncatedRingsEveryLiveWatcherAndResumesFromFloorMinusOne(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("2")
	h.watcher("at-live1", 101, true)
	h.watcher("steward", 102, true)
	h.watcher("at-dead", 103, false)
	trunc := "{\n  \"code\": \"events_journal_truncated\",\n  \"floor\": 50,\n  \"head\": 80,\n  \"since\": 2\n}"
	h.steps = []followStep{{stderr: trunc}, {}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if !h.rungFor("at-live1") || !h.rungFor("steward") {
		t.Fatal("truncation must ring every initiative with a live watcher")
	}
	if h.rungFor("at-dead") || h.rungFor("at-xyz") {
		t.Fatal("truncation must not ring dead-watcher or watcherless initiatives")
	}
	if got := h.calls[1].args[len(h.calls[1].args)-1]; got != "49" {
		t.Fatalf("resume --since %s want 49", got)
	}
	if h.seq() != "49" {
		t.Fatalf("host.seq=%q want 49", h.seq())
	}
}

func TestMailFollowMissingCheckpointUsesBaselineQuery(t *testing.T) {
	h := newFollowHarness(t, true)
	h.steps = []followStep{{}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if len(h.headQ) != 1 || h.headQ[0] != followHeadQuery {
		t.Fatalf("headQ=%v", h.headQ)
	}
	if got := h.calls[0].args[len(h.calls[0].args)-1]; got != "10" {
		t.Fatalf("--since %s want baseline head 10", got)
	}
	if h.seq() != "10" {
		t.Fatalf("baseline checkpoint=%q want 10", h.seq())
	}
}

func TestMailFollowBaselineFailureReplaysFromZeroRingingOnlyNewRows(t *testing.T) {
	h := newFollowHarness(t, true)
	h.headErr = os.ErrNotExist
	h.steps = []followStep{{lines: []string{
		followRowJSON("1", "2026-10-23T11:59:59Z", "create", "message", "at-xyz"), // before start (12:00:00)
		followRowJSON("2", "2026-10-23T12:00:00Z", "create", "message", "at-new"), // at start
	}}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if got := h.calls[0].args[len(h.calls[0].args)-1]; got != "0" {
		t.Fatalf("--since %s want 0", got)
	}
	if !h.rungFor("at-new") {
		t.Fatal("row at follower start must ring")
	}
	if h.rung() {
		t.Fatal("row older than follower start must not ring in fallback mode")
	}
}

// followLifetimeKong builds a child follower whose bd run blocks until its
// context ends, with watcher liveness read from h.alive and fast lifetime
// checks. active counts bd runs in flight.
func followLifetimeKong(h *followHarness, active *atomic.Int32, started chan<- struct{}) *mailFollowKong {
	c := &mailFollowKong{pollEvery: time.Millisecond}
	h.seams(c)
	c.sleep = followSleep
	c.runBD = func(ctx context.Context, _ []string, _ func(string)) (string, error) {
		active.Add(1)
		defer active.Add(-1)
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	return c
}

func followDone(t *testing.T, ch <-chan error, what string) {
	t.Helper()
	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

func followLockHeld(t *testing.T, home string) bool {
	t.Helper()
	release, ok, err := followTryLock(filepath.Join(home, "mailbox", "host-follow.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		release()
	}
	return !ok
}

func TestMailFollowExitsAfterThreeEmptyChecksAndCleansUp(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	var active atomic.Int32
	checks := 0
	c := followLifetimeKong(h, &active, nil)
	c.pidAlive = func(int) bool { return false }
	c.lifeSleep = func(ctx context.Context, d time.Duration) { checks++; followSleep(ctx, d) }
	done := make(chan error, 1)
	go func() { done <- c.runChild(context.Background(), h.cliCtx()) }()
	followDone(t, done, "follower with no watchers")
	if checks < followIdleChecks {
		t.Fatalf("exited after %d checks, want at least %d", checks, followIdleChecks)
	}
	if _, err := os.Stat(filepath.Join(h.home, "mailbox", "host-follow.pid")); err == nil {
		t.Fatal("pidfile left behind")
	}
	if followLockHeld(t, h.home) {
		t.Fatal("lock still held after exit")
	}
}

func TestMailFollowLiveWatcherKeepsFollowerAndPidfileHoldsPid(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	h.watcher("at-w", 4242, true)
	var active atomic.Int32
	started := make(chan struct{}, 1)
	c := followLifetimeKong(h, &active, started)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.runChild(ctx, h.cliCtx()) }()
	<-started
	time.Sleep(100 * time.Millisecond) // many lifetime checks, all non-empty
	if active.Load() != 1 {
		t.Fatalf("follower exited with a live watcher (active=%d)", active.Load())
	}
	b, _ := os.ReadFile(filepath.Join(h.home, "mailbox", "host-follow.pid"))
	if string(b) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("pidfile=%q want %d", b, os.Getpid())
	}
	cancel() // SIGTERM path
	followDone(t, done, "cancelled follower")
	if _, err := os.Stat(filepath.Join(h.home, "mailbox", "host-follow.pid")); err == nil {
		t.Fatal("SIGTERM must remove the pidfile")
	}
	if followLockHeld(t, h.home) {
		t.Fatal("lock still held after SIGTERM")
	}
}

func TestMailFollowHandshakeKeepsRunningWhenWatcherAppearsDuringIt(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	var active atomic.Int32
	c := followLifetimeKong(h, &active, nil)
	var mu sync.Mutex
	h.alive[777] = false
	c.pidAlive = func(pid int) bool { mu.Lock(); defer mu.Unlock(); return h.alive[pid] }
	handshakes := 0
	c.afterRelease = func() {
		handshakes++
		if handshakes == 1 {
			mu.Lock()
			h.alive[777] = true
			mu.Unlock()
			followWriteFile(t, filepath.Join(h.home, "mailbox", "at-w.watcher.pid"), "777\tsess")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.runChild(ctx, h.cliCtx()) }()
	for i := 0; i < 400; i++ {
		if _, err := os.Stat(filepath.Join(h.home, "mailbox", "at-w.watcher.pid")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if active.Load() != 1 || !followLockHeld(t, h.home) {
		t.Fatalf("follower must keep running and hold the lock: active=%d", active.Load())
	}
	if _, err := os.Stat(filepath.Join(h.home, "mailbox", "host-follow.pid")); err != nil {
		t.Fatalf("pidfile must be rewritten after re-locking: %v", err)
	}
	cancel()
	followDone(t, done, "follower")
}

func TestMailFollowSingletonRaceExactlyOneHoldsTheLock(t *testing.T) {
	h := newFollowHarness(t, true)
	h.writeSeq("1")
	h.watcher("at-w", 4242, true) // keeps the winner alive
	const n = 40
	var active atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, n)
	var returned atomic.Int32
	for i := 0; i < n; i++ {
		c := followLifetimeKong(h, &active, nil)
		go func() {
			<-start
			err := c.runChild(ctx, h.cliCtx())
			returned.Add(1)
			done <- err
		}()
	}
	close(start)
	deadline := time.Now().Add(10 * time.Second)
	for returned.Load() < n-1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := returned.Load(); got != n-1 {
		t.Fatalf("%d children returned, want %d losers", got, n-1)
	}
	if active.Load() != 1 {
		t.Fatalf("%d followers running, want exactly 1", active.Load())
	}
	cancel()
	for i := 0; i < n; i++ {
		followDone(t, done, "child")
	}
	if followLockHeld(t, h.home) {
		t.Fatal("lock still held after every child exited")
	}
}

// A new child that arrives before the old follower releases the lock loses the
// lock, but its watcher pidfile is already visible, so the old follower's
// final count keeps it running: never zero followers while a watcher lives.
func TestMailFollowHandshakeKeepsLockWhenNewChildStartedBeforeRelease(t *testing.T) {
	for i := 0; i < 20; i++ {
		h := newFollowHarness(t, true)
		h.writeSeq("1")
		var active atomic.Int32
		h.alive[777] = true
		a := followLifetimeKong(h, &active, nil)
		a.pidAlive = func(pid int) bool { return pid == 777 && h.watcherWritten() }
		a.beforeRelease = func() {
			followWriteFile(t, filepath.Join(h.home, "mailbox", "at-w.watcher.pid"), "777\tsess")
			b := followLifetimeKong(h, &active, nil)
			if err := b.runChild(context.Background(), h.cliCtx()); err != nil {
				t.Errorf("new child: %v", err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- a.runChild(ctx, h.cliCtx()) }()
		for j := 0; j < 400 && !h.watcherWritten(); j++ {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(30 * time.Millisecond)
		if active.Load() != 1 || !followLockHeld(t, h.home) {
			t.Fatalf("iter %d: want the old follower to keep running: active=%d", i, active.Load())
		}
		cancel()
		followDone(t, done, "old follower")
	}
}

// A new child that arrives after the old follower released the lock takes
// over, and the old follower's final count (or failed re-lock) makes it exit.
func TestMailFollowHandshakeHandsOverWhenNewChildStartsAfterRelease(t *testing.T) {
	for i := 0; i < 20; i++ {
		h := newFollowHarness(t, true)
		h.writeSeq("1")
		var active atomic.Int32
		newStarted := make(chan struct{}, 1)
		a := followLifetimeKong(h, &active, nil)
		a.pidAlive = func(pid int) bool { return pid == 777 && h.watcherWritten() }
		ctx, cancel := context.WithCancel(context.Background())
		bDone := make(chan error, 1)
		a.afterRelease = func() {
			followWriteFile(t, filepath.Join(h.home, "mailbox", "at-w.watcher.pid"), "777\tsess")
			b := followLifetimeKong(h, &active, newStarted)
			b.pidAlive = a.pidAlive
			go func() { bDone <- b.runChild(ctx, h.cliCtx()) }()
			select {
			case <-newStarted:
			case <-time.After(5 * time.Second):
				t.Error("new child never took the released lock")
			}
		}
		aDone := make(chan error, 1)
		go func() { aDone <- a.runChild(ctx, h.cliCtx()) }()
		followDone(t, aDone, "old follower")
		time.Sleep(30 * time.Millisecond)
		if active.Load() != 1 || !followLockHeld(t, h.home) {
			t.Fatalf("iter %d: want exactly the new follower running: active=%d", i, active.Load())
		}
		cancel()
		followDone(t, bDone, "new follower")
	}
}

func (h *followHarness) watcherWritten() bool {
	_, err := os.Stat(filepath.Join(h.home, "mailbox", "at-w.watcher.pid"))
	return err == nil
}

func TestParseFollowHeadAcceptsArrayAndObject(t *testing.T) {
	for _, in := range []string{`[{"head":7}]`, `{"head":7}`, `[{"head":"7"}]`} {
		if n, err := parseFollowHead(in); err != nil || n != 7 {
			t.Errorf("parseFollowHead(%s) = %d, %v", in, n, err)
		}
	}
	if _, err := parseFollowHead(`[]`); err == nil {
		t.Error("empty rows must error")
	}
}
