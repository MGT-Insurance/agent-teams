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
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// newLockCmd returns a condenseLockCmd whose clock is fixed at t0.
func newLockCmd(t0 time.Time) *condenseLockCmd {
	return &condenseLockCmd{nowFn: func() time.Time { return t0 }}
}

// writeStaleLock writes a lock file with a timestamp in the past.
func writeStaleLock(t *testing.T, path string, age time.Duration) {
	t.Helper()
	staleUnix := time.Now().Add(-age).Unix()
	content := fmt.Sprintf("pid\n%d\n", staleUnix)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup: write stale lock: %v", err)
	}
}

// TestCondenseLock_AcquireCreatesLockFile verifies acquire creates the lock file.
func TestCondenseLock_AcquireCreatesLockFile(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	cmd := newLockCmd(time.Now())

	if err := cmd.acquire(ctx, condenseLockPath(home)); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	path := condenseLockPath(home)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
}

// TestCondenseLock_SecondAcquireReturnsHeld verifies that a second acquire
// while a fresh lock is held returns the condenseLockHeldCode sentinel.
func TestCondenseLock_SecondAcquireReturnsHeld(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	cmd := newLockCmd(now)

	ctx, _, _ := makeCtx(&fakeBD{}, home)
	if err := cmd.acquire(ctx, condenseLockPath(home)); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// Second acquire should fail with condenseLockHeldCode.
	ctx2, _, _ := makeCtx(&fakeBD{}, home)
	err := cmd.acquire(ctx2, condenseLockPath(home))
	if err == nil {
		t.Fatal("expected held error on second acquire; got nil")
	}
	silent, ok := err.(*cli.SilentError)
	if !ok {
		t.Fatalf("expected *cli.SilentError, got %T: %v", err, err)
	}
	if silent.Code != condenseLockHeldCode {
		t.Errorf("held code = %d, want %d", silent.Code, condenseLockHeldCode)
	}
}

// TestCondenseLock_ReleaseRemovesLock verifies release removes the lock file
// so a subsequent acquire succeeds.
func TestCondenseLock_ReleaseRemovesLock(t *testing.T) {
	home := t.TempDir()
	cmd := newLockCmd(time.Now())
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := cmd.acquire(ctx, condenseLockPath(home)); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if err := cmd.release(condenseLockPath(home)); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	path := condenseLockPath(home)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected lock file gone after release; stat err: %v", err)
	}

	// Subsequent acquire must succeed.
	if err := cmd.acquire(ctx, condenseLockPath(home)); err != nil {
		t.Fatalf("re-acquire after release failed: %v", err)
	}
}

// TestCondenseLock_StaleTimestampIsStolen verifies that a lock file whose
// timestamp is older than condenseLockStaleAge is stolen by a new acquire.
func TestCondenseLock_StaleTimestampIsStolen(t *testing.T) {
	home := t.TempDir()
	path := condenseLockPath(home)

	// Write a stale lock (11 minutes old — beyond the 10-minute threshold).
	writeStaleLock(t, path, 11*time.Minute)

	now := time.Now()
	cmd := newLockCmd(now)
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := cmd.acquire(ctx, condenseLockPath(home)); err != nil {
		t.Fatalf("acquire on stale lock failed: %v", err)
	}

	// Lock file must still exist (stolen/overwritten, not deleted).
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file should exist after steal; got: %v", err)
	}

	// Second acquire against the freshly-stolen lock must return held.
	ctx2, _, _ := makeCtx(&fakeBD{}, home)
	err := cmd.acquire(ctx2, condenseLockPath(home))
	if err == nil {
		t.Fatal("expected held after steal; got nil")
	}
	if _, ok := err.(*cli.SilentError); !ok {
		t.Fatalf("expected *cli.SilentError after steal; got %T: %v", err, err)
	}
}

// TestCondenseLock_ReleaseIdempotent verifies release on a missing lock is a no-op.
func TestCondenseLock_ReleaseIdempotent(t *testing.T) {
	home := t.TempDir()
	cmd := newLockCmd(time.Now())

	if err := cmd.release(condenseLockPath(home)); err != nil {
		t.Fatalf("release on non-existent lock should be a no-op; got: %v", err)
	}
}

// TestCondenseLock_MissingActionReturnsParseError verifies parse error when
// action is omitted (kong enforces the required positional at parse time).
func TestCondenseLock_MissingActionReturnsParseError(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterCondenseLock(p)
	_, parseErr := p.Parse([]string{"condense-lock"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing action; got nil")
	}
}

// TestCondenseLock_UnknownActionReturnsParseError verifies parse error for
// unknown action (kong enforces the enum constraint at parse time).
func TestCondenseLock_UnknownActionReturnsParseError(t *testing.T) {
	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterCondenseLock(p)
	_, parseErr := p.Parse([]string{"condense-lock", "badaction"})
	if parseErr == nil {
		t.Fatal("expected parse error for unknown action; got nil")
	}
}

// TestCondenseLock_NilContextReturnsError verifies nil context returns an error.
func TestCondenseLock_NilContextReturnsError(t *testing.T) {
	err := (&condenseLockKong{Action: "acquire"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context; got nil")
	}
}

// TestParseLockTimestamp verifies the lock file parser handles valid and
// invalid content correctly.
func TestParseLockTimestamp(t *testing.T) {
	ts, err := parseLockTimestamp("pid\n1234567890\n")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if ts != 1234567890 {
		t.Errorf("ts = %d, want 1234567890", ts)
	}

	_, err = parseLockTimestamp("malformed")
	if err == nil {
		t.Error("expected error for malformed content; got nil")
	}
}

// TestCondenseLock_LockFileContainsTimestamp verifies acquire writes a
// parseable timestamp.
func TestCondenseLock_LockFileContainsTimestamp(t *testing.T) {
	home := t.TempDir()
	fixed := time.Unix(1700000000, 0)
	cmd := newLockCmd(fixed)
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := cmd.acquire(ctx, condenseLockPath(home)); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".condense.lock"))
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	ts, err := parseLockTimestamp(string(data))
	if err != nil {
		t.Fatalf("parse lock file: %v", err)
	}
	if ts != fixed.Unix() {
		t.Errorf("timestamp = %d, want %d", ts, fixed.Unix())
	}
}

// ── bd-backed lock ────────────────────────────────────────────────────────────

// exitStatusErr mimics *exec.ExitError for the bd client's wrapped errors.
type exitStatusErr struct{ code int }

func (e *exitStatusErr) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitStatusErr) ExitCode() int { return e.code }

// lockFakeBD is a stateful in-memory stand-in for the bd commands the bd lock
// issues. It models one issue, prefix-condense-lock, with an assignee.
type lockFakeBD struct {
	prefix   string
	exists   bool
	status   string
	assignee string
	calls    [][]string
	failAll  error // when set, every call returns it
}

func newLockFakeBD() *lockFakeBD { return &lockFakeBD{prefix: "at"} }

func (f *lockFakeBD) id() string { return f.prefix + "-condense-lock" }

func (f *lockFakeBD) countVerb(verb string) int {
	n := 0
	for _, c := range f.calls {
		if len(c) > 0 && c[0] == verb {
			n++
		}
	}
	return n
}

func (f *lockFakeBD) Run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.failAll != nil {
		return "", f.failAll
	}
	if len(args) == 4 && args[1] == "config" && args[3] == "issue_prefix" {
		return f.prefix, nil
	}
	switch args[0] {
	case "show":
		if !f.exists || args[1] != f.id() {
			return "", fmt.Errorf("bd show: issue %s not found", args[1])
		}
		b, _ := json.Marshal([]bd.Issue{{ID: f.id(), Status: f.status, Assignee: f.assignee}})
		return string(b), nil
	case "create":
		if f.exists {
			return "", fmt.Errorf("%s already exists", f.id())
		}
		f.exists, f.status = true, "open"
		return "", nil
	case "close":
		f.status = "closed"
		return "", nil
	case "update":
		if !f.exists {
			return "", fmt.Errorf("issue not found: %s", args[1])
		}
		var expect, assign string
		for i := 2; i+1 < len(args); i += 2 {
			switch args[i] {
			case "--if-assignee":
				expect = args[i+1]
			case "--assignee":
				assign = args[i+1]
			}
		}
		if f.assignee != expect {
			return "", &exitStatusErr{code: 13}
		}
		f.assignee = assign
		return "", nil
	}
	return "", fmt.Errorf("lockFakeBD: unexpected call %v", args)
}

func (f *lockFakeBD) RunJSON(dst any, args ...string) error {
	f.calls = append(f.calls, args)
	if f.failAll != nil {
		return f.failAll
	}
	out, err := f.Run(args...)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(out), dst)
}

// bdLockCtx builds a context whose learnings runner is learn. When remote is
// true Learn differs from BD, as with cloud_home set; otherwise Learn is nil
// and DSP_BEAD is expected to be set by the caller.
func bdLockCtx(learn cli.BDRunner, remote bool) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	ctx := &cli.Context{Home: "/unused/home", BD: &lockFakeBD{}, Stdout: &out, Stderr: &errb}
	if remote {
		ctx.Learn = learn
	} else {
		ctx.BD = learn
	}
	return ctx, &out, &errb
}

func lockAt(t0 time.Time, who string) *condenseLockCmd {
	return &condenseLockCmd{nowFn: func() time.Time { return t0 }, whoFn: func() string { return who }}
}

func requireHeldExit(t *testing.T, err error) {
	t.Helper()
	var s *cli.SilentError
	if !errors.As(err, &s) || s.Code != condenseLockHeldCode {
		t.Fatalf("err = %v, want silent exit %d", err, condenseLockHeldCode)
	}
}

// TestBDLock_FreeAcquireCreatesAndClosesBead covers first use: the CAS fails on
// the missing issue, the issue is created and closed, and the retry wins.
func TestBDLock_FreeAcquireCreatesAndClosesBead(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	ctx, _, _ := bdLockCtx(f, true)
	if err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if f.countVerb("create") != 1 || f.countVerb("close") != 1 {
		t.Errorf("create/close = %d/%d, want 1/1", f.countVerb("create"), f.countVerb("close"))
	}
	if f.status != "closed" {
		t.Errorf("status = %q, want closed", f.status)
	}
	if f.assignee != "condense:laptop@1000" {
		t.Errorf("assignee = %q", f.assignee)
	}
}

// TestBDLock_SecondAcquireNamesHolder covers the held path: exit 5 with the
// frozen message naming the holder, and no steal attempt.
func TestBDLock_SecondAcquireNamesHolder(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	ctx, _, errb := bdLockCtx(f, true)
	if err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	err := lockAt(time.Unix(1000+59*60, 0), "vm").acquireBD(ctx)
	requireHeldExit(t, err)
	if want := "ateam condense-lock: lock held by condense:laptop@1000\n"; errb.String() != want {
		t.Errorf("stderr = %q, want %q", errb.String(), want)
	}
	if f.assignee != "condense:laptop@1000" {
		t.Errorf("holder changed to %q", f.assignee)
	}
}

// TestBDLock_StaleHolderIsStolen covers a 61-minute-old holder.
func TestBDLock_StaleHolderIsStolen(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	ctx, _, _ := bdLockCtx(f, true)
	if err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	now := time.Unix(1000+61*60, 0)
	if err := lockAt(now, "vm").acquireBD(ctx); err != nil {
		t.Fatalf("steal: %v", err)
	}
	if want := fmt.Sprintf("condense:vm@%d", now.Unix()); f.assignee != want {
		t.Errorf("assignee = %q, want %q", f.assignee, want)
	}
	// The steal must be a CAS on the old holder, not an unconditional write.
	last := f.calls[len(f.calls)-1]
	if strings.Join(last, " ") != "update at-condense-lock --if-assignee condense:laptop@1000 --assignee condense:vm@"+fmt.Sprint(now.Unix()) {
		t.Errorf("steal call = %v", last)
	}
}

// TestBDLock_ReleaseByOwnerFreesLock covers release by the same <who>.
func TestBDLock_ReleaseByOwnerFreesLock(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	ctx, out, _ := bdLockCtx(f, true)
	if err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lockAt(time.Unix(1500, 0), "laptop").releaseBD(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if f.assignee != "" {
		t.Errorf("assignee = %q after release, want empty", f.assignee)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected stdout %q", out.String())
	}
	if f.status != "closed" {
		t.Errorf("status = %q, want closed", f.status)
	}
}

// TestBDLock_ReleaseByOtherPrintsFrozenLine covers release by another <who>: the
// frozen line, exit 0, and no update call.
func TestBDLock_ReleaseByOtherPrintsFrozenLine(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	ctx, out, _ := bdLockCtx(f, true)
	if err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	updates := f.countVerb("update")
	if err := lockAt(time.Unix(1500, 0), "vm").releaseBD(ctx); err != nil {
		t.Fatalf("release by other returned error: %v", err)
	}
	if want := "ateam condense-lock: not held by vm; nothing released\n"; out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if f.countVerb("update") != updates {
		t.Error("release by another owner made an update call")
	}
	if f.assignee != "condense:laptop@1000" {
		t.Errorf("holder changed to %q", f.assignee)
	}
}

// TestBDLock_ReleaseWhenFreePrintsFrozenLine covers release of a free lock and
// of a lock whose issue was never created.
func TestBDLock_ReleaseWhenFreePrintsFrozenLine(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	for _, created := range []bool{false, true} {
		f := newLockFakeBD()
		f.exists, f.status = created, "closed"
		ctx, out, _ := bdLockCtx(f, true)
		if err := lockAt(time.Unix(1500, 0), "vm").releaseBD(ctx); err != nil {
			t.Fatalf("created=%v: release: %v", created, err)
		}
		if want := "ateam condense-lock: not held by vm; nothing released\n"; out.String() != want {
			t.Errorf("created=%v: stdout = %q, want %q", created, out.String(), want)
		}
		if f.countVerb("update") != 0 {
			t.Errorf("created=%v: release of a free lock made an update call", created)
		}
	}
}

// TestBDLock_UnreachableSkipsWithFrozenText covers a hosted store that bd
// cannot reach: the frozen stderr text and exit 5.
func TestBDLock_UnreachableSkipsWithFrozenText(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	f.failAll = fmt.Errorf("failed to open database: Dolt server unreachable at host:3306: dial tcp: i/o timeout")
	ctx, _, errb := bdLockCtx(f, true)
	err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx)
	requireHeldExit(t, err)
	if want := "ateam condense-lock: learnings store unreachable; condense skipped\n"; errb.String() != want {
		t.Errorf("stderr = %q, want %q", errb.String(), want)
	}
}

// TestBDLock_UnreachableAfterPrefixStillSkips covers the store dropping away
// on a later call, not just the first.
func TestBDLock_UnreachableAfterPrefixStillSkips(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	ctx, _, errb := bdLockCtx(&unreachableAfterPrefix{lockFakeBD: f}, true)
	err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx)
	requireHeldExit(t, err)
	if !strings.Contains(errb.String(), "learnings store unreachable; condense skipped") {
		t.Errorf("stderr = %q", errb.String())
	}
}

type unreachableAfterPrefix struct{ *lockFakeBD }

func (u *unreachableAfterPrefix) Run(args ...string) (string, error) {
	return "", fmt.Errorf("bd %s: Dolt server unreachable at h:1", args[0])
}

// TestBDLock_DSPBeadSelectsBDBackend covers the second trigger: no cloud_home
// (Learn == BD) but DSP_BEAD set. The holder's <who> is DSP_BEAD.
func TestBDLock_DSPBeadSelectsBDBackend(t *testing.T) {
	t.Setenv("DSP_BEAD", "at-job7")
	f := newLockFakeBD()
	home := t.TempDir()
	ctx, _, _ := bdLockCtx(f, false)
	ctx.Home = home
	if err := (&condenseLockKong{Action: "acquire"}).Run(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !strings.HasPrefix(f.assignee, "condense:at-job7@") {
		t.Errorf("assignee = %q, want condense:at-job7@<unix>", f.assignee)
	}
	if _, err := os.Stat(condenseLockPath(home)); !os.IsNotExist(err) {
		t.Errorf("file lock created under DSP_BEAD; stat err = %v", err)
	}
	if err := (&condenseLockKong{Action: "release"}).Run(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if f.assignee != "" {
		t.Errorf("assignee = %q after release", f.assignee)
	}
}

// TestBDLock_FileLockWhenNeitherSet covers the byte-for-byte rule: with no
// cloud_home and no DSP_BEAD the file lock is used and bd is never called.
func TestBDLock_FileLockWhenNeitherSet(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	home := t.TempDir()
	ctx, _, _ := bdLockCtx(f, false)
	ctx.Home = home
	if condenseLockUsesBD(ctx) {
		t.Fatal("condenseLockUsesBD = true with neither trigger set")
	}
	if err := (&condenseLockKong{Action: "acquire"}).Run(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := os.Stat(condenseLockPath(home)); err != nil {
		t.Errorf("file lock missing: %v", err)
	}
	err := (&condenseLockKong{Action: "acquire"}).Run(ctx)
	requireHeldExit(t, err)
	if err := (&condenseLockKong{Action: "release"}).Run(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := os.Stat(condenseLockPath(home)); !os.IsNotExist(err) {
		t.Errorf("file lock present after release; stat err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("file backend made bd calls: %v", f.calls)
	}
}

// TestBDLock_PrefixComesFromBD verifies the lock id uses the prefix bd reports
// rather than a hard-coded one.
func TestBDLock_PrefixComesFromBD(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	f := newLockFakeBD()
	f.prefix = "zz"
	ctx, _, _ := bdLockCtx(f, true)
	if err := lockAt(time.Unix(1000, 0), "laptop").acquireBD(ctx); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	for _, c := range f.calls {
		if c[0] == "create" && c[2] != "zz-condense-lock" {
			t.Errorf("create id = %q, want zz-condense-lock", c[2])
		}
	}
}

// TestBDLock_RealEmbeddedWorkspace runs the lock against a real bd workspace
// created under t.TempDir(). HOME is redirected so bd neither reads the user's
// ~/.beads config (which carries a sync.remote that init would clone from) nor
// writes anywhere outside the temp dir. Skipped when bd is not on PATH.
func TestBDLock_RealEmbeddedWorkspace(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	t.Setenv("DSP_BEAD", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BEADS_DIR", "")
	ws := t.TempDir()
	init := exec.Command("bd", "init", "-p", "zz", "--non-interactive", "--quiet", "--skip-hooks", "--skip-agents")
	init.Dir = ws
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, out)
	}

	real := bd.NewClient(ws)
	ctx, _, errb := bdLockCtx(real, true)
	laptop := lockAt(time.Now(), "laptop")
	vm := lockAt(time.Now(), "vm")

	if err := laptop.acquireBD(ctx); err != nil {
		t.Fatalf("first acquire (want exit 0): %v", err)
	}
	err := vm.acquireBD(ctx)
	requireHeldExit(t, err)
	if !strings.Contains(errb.String(), "ateam condense-lock: lock held by condense:laptop@") {
		t.Errorf("held stderr = %q", errb.String())
	}
	if err := vm.releaseBD(ctx); err != nil {
		t.Fatalf("release by other (want exit 0): %v", err)
	}
	if err := laptop.releaseBD(ctx); err != nil {
		t.Fatalf("release by owner (want exit 0): %v", err)
	}
	// Free again: a new owner can take it.
	if err := vm.acquireBD(ctx); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := vm.releaseBD(ctx); err != nil {
		t.Fatalf("final release: %v", err)
	}

	iss, err := bd.ShowIssue(real, "zz-condense-lock")
	if err != nil {
		t.Fatalf("show lock issue: %v", err)
	}
	if iss.Status != "closed" {
		t.Errorf("lock issue status = %q, want closed", iss.Status)
	}
	if iss.Assignee != "" {
		t.Errorf("lock issue assignee = %q after release", iss.Assignee)
	}
	list, err := real.Run("list")
	if err != nil {
		t.Fatalf("bd list: %v", err)
	}
	if strings.Contains(list, "condense-lock") {
		t.Errorf("default bd list shows the lock issue:\n%s", list)
	}
}

// TestCondenseLock_HeldCodeIsDistinct verifies condenseLockHeldCode is not
// zero, 1, 2, 3, or 4 — those are reserved for other cli error types.
func TestCondenseLock_HeldCodeIsDistinct(t *testing.T) {
	reserved := map[int]string{
		0: "success",
		1: "generic error",
		2: "UsageError",
		3: "DepError",
		4: "WorkspaceError",
	}
	if name, conflict := reserved[condenseLockHeldCode]; conflict {
		t.Errorf("condenseLockHeldCode=%d conflicts with %q", condenseLockHeldCode, name)
	}
}
