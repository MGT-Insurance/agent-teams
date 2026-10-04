//go:build unix && keystone

// pull_stale_unix_keystone_test.go runs terminateHungPullUnix — the real,
// unexported implementation this bead (agent-teams-qdeh.3) committed — against
// a REAL scratch dolt sql-server with a permanently-hung ssh transport. Every
// other test in this package fakes the process table; this one proves the
// actual code path (not a hand-issued kill(1) sequence) achieves the same
// outcome the keystone in the package doc comment describes by hand.
//
// Gated behind the `keystone` build tag (and unix) so it never runs as part
// of `go test ./...`/CI: it shells out to the real `bd` and `dolt` binaries,
// spawns real long-lived processes, and needs several seconds of real
// wall-clock time. Run explicitly:
//
//	go test -tags keystone -run TestTerminateHungPullUnix_LiveKeystone ./internal/verbs/... -v
//
// It cleans up every process it starts by PID (never pkill) via t.Cleanup,
// and removes its scratch directory (t.TempDir(), auto-cleaned) only after
// the server and both pull processes have exited.
package verbs

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runBd runs `bd <args...>` with its working directory set to dir (rather
// than `bd -C dir`, which `bd init` rejects: -C requires an already
// -initialized project) and the given env (nil means the test process's own
// environment). Fails the test on a non-zero exit.
func runBd(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// queryInFlightPulls runs the same probe probeInFlightPull's frozen contract
// describes, directly — bypassing probeInFlightPull itself, which is still a
// not-implemented stub on this un-rebased branch (qdeh.2's real body lives on
// the integration branch; merging it is the DRI's job, not this test's). This
// keeps the keystone's identification of the target connection independent of
// that stub, while terminateHungPullUnix itself is still exercised for real.
func queryInFlightPulls(t *testing.T, dir string) []processlistRow {
	t.Helper()
	out := runBd(t, dir, nil, "sql", "--json",
		"SELECT ID, TIME FROM information_schema.processlist WHERE UPPER(TRIM(INFO)) LIKE 'CALL DOLT_PULL%'")
	var rows []processlistRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("parse processlist JSON: %v\nraw: %s", err, out)
	}
	return rows
}

// largestByTime returns the row with the largest TIME (the query actually
// executing; any others are queued behind it — same heuristic
// probeInFlightPull's contract specifies), or nil if rows is empty.
func largestByTime(rows []processlistRow) *processlistRow {
	if len(rows) == 0 {
		return nil
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.Time > best.Time {
			best = r
		}
	}
	return &best
}

func TestTerminateHungPullUnix_LiveKeystone(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not on PATH")
	}
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not on PATH")
	}

	scratch := t.TempDir()
	proj := filepath.Join(scratch, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}

	hangScript := filepath.Join(scratch, "hang-ssh.sh")
	if err := os.WriteFile(hangScript, []byte("#!/bin/sh\nexec sleep 999999\n"), 0o755); err != nil {
		t.Fatalf("write hang script: %v", err)
	}

	// GIT_SSH_COMMAND must be set on the SERVER's own env at launch — it's
	// the server process that spawns git/ssh for a DOLT_PULL, not the bd
	// client issuing the query.
	serverEnv := append(os.Environ(), "GIT_SSH_COMMAND="+hangScript)
	runBd(t, proj, serverEnv, "init", "--server", "--non-interactive", "--prefix=ks")

	t.Cleanup(func() {
		stop := exec.Command("bd", "dolt", "stop")
		stop.Dir = proj
		_ = stop.Run()
	})

	runBd(t, proj, nil, "dolt", "remote", "add", "origin", "git+ssh://git@10.255.255.1/nonexistent.git")
	// bd refuses to auto-commit a dirty internal config key before a pull;
	// clear it the same way the by-hand keystone had to.
	runBd(t, proj, nil, "sql", "CALL DOLT_ADD('-A')")
	runBd(t, proj, nil, "sql", "CALL DOLT_COMMIT('-m','keystone setup','--author','keystone <keystone@example.com>')")

	// Point the real code under test at this scratch workspace.
	t.Setenv("AGENT_TEAMS_HOME", proj)

	startPull := func() *exec.Cmd {
		cmd := exec.Command("bd", "dolt", "pull")
		cmd.Dir = proj
		if err := cmd.Start(); err != nil {
			t.Fatalf("start bd dolt pull: %v", err)
		}
		return cmd
	}
	pullDone := make(chan struct{}, 2)
	waitPull := func(cmd *exec.Cmd) {
		_ = cmd.Wait()
		pullDone <- struct{}{}
	}

	pullA := startPull()
	go waitPull(pullA)
	time.Sleep(2 * time.Second) // let A actually reach ls-remote before B queues

	pullB := startPull()
	go waitPull(pullB)
	time.Sleep(2 * time.Second)

	t.Cleanup(func() {
		for _, cmd := range []*exec.Cmd{pullA, pullB} {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	})

	rows := queryInFlightPulls(t, proj)
	row := largestByTime(rows)
	if row == nil {
		t.Fatalf("no in-flight CALL DOLT_PULL found; processlist rows: %+v", rows)
	}
	t.Logf("keystone: targeting conn %d (age %ds) of %d in-flight rows", row.ID, row.Time, len(rows))

	// Snapshot the process tree under the server BEFORE the remedy, for the
	// after-comparison below.
	serverPID, port, err := readDoltServerFiles(proj)
	if err != nil {
		t.Fatalf("readDoltServerFiles: %v", err)
	}
	before, err := psSnapshotAll(context.Background())
	if err != nil {
		t.Fatalf("psSnapshotAll (before): %v", err)
	}
	if _, err := verifyServerIdentity(before, serverPID, port); err != nil {
		t.Fatalf("verifyServerIdentity: %v", err)
	}
	beforeDescendants := descendantsOf(before, serverPID, serverPID)
	t.Logf("keystone: %d descendant(s) of server pid %d before remedy", len(beforeDescendants), serverPID)
	if len(beforeDescendants) == 0 {
		t.Fatal("keystone: expected at least one descendant (the hung git/ssh transport) before the remedy")
	}

	// THE REAL CODE PATH under test — not a hand-issued kill(1).
	remedyErr := terminateHungPullUnix(context.Background(), inFlightPull{ConnID: row.ID, Seconds: row.Time})
	// terminateHungPullUnix's final step (waitForPullCleared) calls
	// probeInFlightPull, which is still a not-implemented stub on this
	// un-rebased branch — so it always returns a wrapped "still in flight"
	// error here regardless of whether the OS-level remedy actually
	// succeeded. That plumbing is qdeh.2/qdeh.4's concern, integrated by the
	// DRI; this test verifies the remedy's actual effect directly below,
	// rather than trusting terminateHungPullUnix's return value for it.
	if remedyErr != nil {
		t.Logf("terminateHungPullUnix returned %v (expected: probeInFlightPull is a stub pre-merge; verifying the real effect directly below)", remedyErr)
	}

	// (a) the targeted connection is gone from the processlist.
	deadline := time.Now().Add(10 * time.Second)
	var stillThere bool
	for {
		rows := queryInFlightPulls(t, proj)
		stillThere = false
		for _, r := range rows {
			if r.ID == row.ID {
				stillThere = true
			}
		}
		if !stillThere || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if stillThere {
		t.Fatalf("(a) FAILED: conn %d still in-flight after terminateHungPullUnix", row.ID)
	}
	t.Logf("(a) OK: conn %d cleared from processlist", row.ID)

	// (b) the queued pull doesn't hang forever on a leaked child: if it's now
	// the in-flight one, clear it too the same way, and confirm it finishes.
	rows = queryInFlightPulls(t, proj)
	if row2 := largestByTime(rows); row2 != nil {
		t.Logf("keystone: second conn %d now in flight; clearing it too", row2.ID)
		remedyErr2 := terminateHungPullUnix(context.Background(), inFlightPull{ConnID: row2.ID, Seconds: row2.Time})
		if remedyErr2 != nil {
			t.Logf("terminateHungPullUnix (second conn) returned %v (same stub caveat as above)", remedyErr2)
		}
	}
	select {
	case <-pullDone:
	case <-time.After(15 * time.Second):
		t.Fatal("(b) FAILED: neither queued bd dolt pull process exited within 15s of the remedy")
	}
	select {
	case <-pullDone:
		t.Log("(b) OK: both bd dolt pull processes exited")
	case <-time.After(15 * time.Second):
		t.Fatal("(b) FAILED: the second bd dolt pull process never exited")
	}

	// (c) no leaked git/ssh survives under the server.
	time.Sleep(1 * time.Second) // let the OS reap now-dead children
	after, err := psSnapshotAll(context.Background())
	if err != nil {
		t.Fatalf("psSnapshotAll (after): %v", err)
	}
	afterDescendants := descendantsOf(after, serverPID, serverPID)
	if len(afterDescendants) != 0 {
		t.Fatalf("(c) FAILED: %d orphaned descendant(s) of server pid %d survive the remedy: %+v", len(afterDescendants), serverPID, afterDescendants)
	}
	t.Log("(c) OK: zero descendants of the server pid survive the remedy")
}
