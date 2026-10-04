//go:build unix

package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// fakePS builds a small process table for selectTransportTargets/descendant
// tests. lstart is deliberately the same literal string across all rows
// unless a test overrides it — only identity-recheck tests care about it
// differing.
func fakePS(entries ...psEntry) []psEntry { return entries }

const fakeLStart = "Thu Sep 24 09:40:32 2026"

func TestSelectTransportTargets_PicksGitUnderServerAndDescendants(t *testing.T) {
	const serverPID = 100
	entries := fakePS(
		psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"},
		psEntry{PID: 200, PPID: serverPID, LStart: fakeLStart, Command: "git fetch --no-tags --refmap= origin +refs/dolt/data:refs/dolt/remotes/origin/dolt/data/abc"},
		psEntry{PID: 201, PPID: 200, LStart: fakeLStart, Command: "/path/to/hang-ssh.sh git@example.com git-upload-pack '/x.git'"},
		psEntry{PID: 202, PPID: 201, LStart: fakeLStart, Command: "sleep 999999"}, // grandchild of the git parent
	)

	descendants, gitParents, err := selectTransportTargets(entries, serverPID)
	if err != nil {
		t.Fatalf("selectTransportTargets: %v", err)
	}
	if len(gitParents) != 1 || gitParents[0].PID != 200 {
		t.Fatalf("gitParents = %+v, want exactly pid 200", gitParents)
	}
	gotPIDs := map[int]bool{}
	for _, d := range descendants {
		gotPIDs[d.PID] = true
	}
	if len(gotPIDs) != 2 || !gotPIDs[201] || !gotPIDs[202] {
		t.Fatalf("descendants = %+v, want exactly pids {201, 202}", descendants)
	}
}

// TestSelectTransportTargets_PicksGitLsRemote covers the hang point
// team-lead's review flagged: a connection dropped during ref advertisement
// blocks on `git ls-remote`, not `git fetch` — the case this bead's own
// keystone reproduced. selectTransportTargets must still find it, argv[0]
// basename "git" with a "--git-dir" flag preceding the subcommand (the
// real-world shape observed) included.
func TestSelectTransportTargets_PicksGitLsRemote(t *testing.T) {
	const serverPID = 100
	entries := fakePS(
		psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"},
		psEntry{PID: 200, PPID: serverPID, LStart: fakeLStart, Command: "git --git-dir /path/to/repo.git ls-remote --heads -- origin"},
		psEntry{PID: 201, PPID: 200, LStart: fakeLStart, Command: "/path/to/hang-ssh.sh git@example.com git-upload-pack '/x.git'"},
	)

	descendants, gitParents, err := selectTransportTargets(entries, serverPID)
	if err != nil {
		t.Fatalf("selectTransportTargets: %v", err)
	}
	if len(gitParents) != 1 || gitParents[0].PID != 200 {
		t.Fatalf("gitParents = %+v, want exactly pid 200", gitParents)
	}
	if len(descendants) != 1 || descendants[0].PID != 201 {
		t.Fatalf("descendants = %+v, want exactly pid 201", descendants)
	}
}

// TestSelectTransportTargets_IgnoresNonTransportGitSubcommand confirms the
// match stays scoped to "fetch"/"ls-remote": a git child running some other
// subcommand under the server is not a network-transport hang and must not
// be selected.
func TestSelectTransportTargets_IgnoresNonTransportGitSubcommand(t *testing.T) {
	const serverPID = 100
	entries := fakePS(
		psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"},
		psEntry{PID: 200, PPID: serverPID, LStart: fakeLStart, Command: "git status"},
	)

	if _, _, err := selectTransportTargets(entries, serverPID); err == nil {
		t.Fatal("selectTransportTargets: want error for a non-transport git subcommand, got nil")
	}
}

func TestSelectTransportTargets_IgnoresGitFetchWithWrongParent(t *testing.T) {
	const serverPID = 100
	entries := fakePS(
		psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"},
		// Same command text, but its parent is NOT the server — must be
		// ignored (ancestry only, never name-based matching).
		psEntry{PID: 300, PPID: 999, LStart: fakeLStart, Command: "git fetch --no-tags --refmap= origin +refs/dolt/data:refs/dolt/remotes/origin/dolt/data/abc"},
	)

	_, _, err := selectTransportTargets(entries, serverPID)
	if err == nil {
		t.Fatal("selectTransportTargets: want error (no git child under the server), got nil")
	}
}

func TestSelectTransportTargets_NeverIncludesServerPID(t *testing.T) {
	const serverPID = 100
	entries := fakePS(
		psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"},
		psEntry{PID: 200, PPID: serverPID, LStart: fakeLStart, Command: "git fetch --no-tags --refmap= origin +refs/dolt/data:refs/dolt/remotes/origin/dolt/data/abc"},
		// A pathological row claiming the server pid as its own child —
		// must never come back as a target.
		psEntry{PID: serverPID, PPID: 200, LStart: fakeLStart, Command: "should never be selected"},
	)

	descendants, gitParents, err := selectTransportTargets(entries, serverPID)
	if err != nil {
		t.Fatalf("selectTransportTargets: %v", err)
	}
	for _, e := range append(append([]psEntry{}, descendants...), gitParents...) {
		if e.PID == serverPID {
			t.Fatalf("selectTransportTargets returned the server pid as a target: %+v", e)
		}
	}
}

func TestSelectTransportTargets_NoGitParentFound_Errors(t *testing.T) {
	const serverPID = 100
	entries := fakePS(
		psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"},
		psEntry{PID: 200, PPID: serverPID, LStart: fakeLStart, Command: "some other child, not a dolt-data fetch"},
	)

	if _, _, err := selectTransportTargets(entries, serverPID); err == nil {
		t.Fatal("selectTransportTargets: want error when the server has no matching git child, got nil")
	}
}

func TestVerifyServerIdentity_MismatchIsError(t *testing.T) {
	entries := fakePS(
		psEntry{PID: 100, PPID: 1, LStart: fakeLStart, Command: "not-dolt some-other-process"},
	)
	if _, err := verifyServerIdentity(entries, 100, "60279"); err == nil {
		t.Fatal("verifyServerIdentity: want error for a pid whose command isn't dolt sql-server, got nil")
	}

	// Right binary, wrong port: also a mismatch.
	entries = fakePS(
		psEntry{PID: 100, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 11111"},
	)
	if _, err := verifyServerIdentity(entries, 100, "60279"); err == nil {
		t.Fatal("verifyServerIdentity: want error for a port mismatch, got nil")
	}

	// pid not present at all.
	if _, err := verifyServerIdentity(fakePS(), 100, "60279"); err == nil {
		t.Fatal("verifyServerIdentity: want error when the pid isn't in the process table, got nil")
	}
}

func TestVerifyServerIdentity_Match(t *testing.T) {
	entries := fakePS(
		psEntry{PID: 100, PPID: 1, LStart: fakeLStart, Command: "/opt/homebrew/bin/dolt sql-server -H 127.0.0.1 -P 60279 --loglevel=warning"},
	)
	got, err := verifyServerIdentity(entries, 100, "60279")
	if err != nil {
		t.Fatalf("verifyServerIdentity: %v", err)
	}
	if got.PID != 100 {
		t.Fatalf("verifyServerIdentity: got pid %d, want 100", got.PID)
	}
}

// spySignal records every (pid, sig) it's asked to send and always
// succeeds, so tests can assert exactly which PIDs were signaled — and, for
// the mismatch/no-target cases, that NONE were.
type spySignal struct {
	mu    sync.Mutex
	calls []int
}

func (s *spySignal) fn(pid int, _ syscall.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, pid)
	return nil
}

func (s *spySignal) pids() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int{}, s.calls...)
}

func withFakeProcessSeams(t *testing.T, lookup func(ctx context.Context, pid int) (psEntry, bool, error)) *spySignal {
	t.Helper()
	origLookup, origSignal := psLookupPID, signalPID
	spy := &spySignal{}
	psLookupPID = lookup
	signalPID = spy.fn
	t.Cleanup(func() {
		psLookupPID = origLookup
		signalPID = origSignal
	})
	return spy
}

func TestSignalIfUnchanged_SignalsWhenIdentityMatches(t *testing.T) {
	target := psEntry{PID: 201, PPID: 200, LStart: fakeLStart, Command: "sleep 999999"}
	spy := withFakeProcessSeams(t, func(_ context.Context, pid int) (psEntry, bool, error) {
		if pid != target.PID {
			t.Fatalf("psLookupPID called with unexpected pid %d", pid)
		}
		return target, true, nil
	})

	sent, err := signalIfUnchanged(context.Background(), target, 100, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("signalIfUnchanged: %v", err)
	}
	if !sent {
		t.Fatal("signalIfUnchanged: want signal sent when identity matches")
	}
	if got := spy.pids(); len(got) != 1 || got[0] != target.PID {
		t.Fatalf("signalPID calls = %v, want exactly [%d]", got, target.PID)
	}
}

func TestSignalIfUnchanged_SkipsWhenLStartChanged(t *testing.T) {
	target := psEntry{PID: 201, PPID: 200, LStart: fakeLStart, Command: "sleep 999999"}
	recycled := psEntry{PID: 201, PPID: 1, LStart: "Thu Sep 24 09:59:59 2026", Command: "sleep 999999"} // PID reused by an unrelated process
	withFakeSeams := withFakeProcessSeams(t, func(_ context.Context, pid int) (psEntry, bool, error) {
		return recycled, true, nil
	})

	sent, err := signalIfUnchanged(context.Background(), target, 100, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("signalIfUnchanged: %v", err)
	}
	if sent {
		t.Fatal("signalIfUnchanged: must not signal a pid whose lstart changed (PID reuse)")
	}
	if got := withFakeSeams.pids(); len(got) != 0 {
		t.Fatalf("signalPID calls = %v, want none", got)
	}
}

func TestSignalIfUnchanged_NeverSignalsServerPID(t *testing.T) {
	const serverPID = 100
	spy := withFakeProcessSeams(t, func(_ context.Context, pid int) (psEntry, bool, error) {
		t.Fatal("psLookupPID must not be called for the server pid — it must be rejected before any lookup")
		return psEntry{}, false, nil
	})

	target := psEntry{PID: serverPID, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"}
	sent, err := signalIfUnchanged(context.Background(), target, serverPID, syscall.SIGTERM)
	if err == nil {
		t.Fatal("signalIfUnchanged: want error when target is the server pid, got nil")
	}
	if sent {
		t.Fatal("signalIfUnchanged: must not report a signal sent to the server pid")
	}
	if got := spy.pids(); len(got) != 0 {
		t.Fatalf("signalPID calls = %v, want none", got)
	}
}

func TestSignalIfUnchanged_AlreadyGone_NoSignal(t *testing.T) {
	target := psEntry{PID: 201, PPID: 200, LStart: fakeLStart, Command: "sleep 999999"}
	spy := withFakeProcessSeams(t, func(_ context.Context, pid int) (psEntry, bool, error) {
		return psEntry{}, false, nil // already exited
	})

	sent, err := signalIfUnchanged(context.Background(), target, 100, syscall.SIGTERM)
	if err != nil {
		t.Fatalf("signalIfUnchanged: %v", err)
	}
	if sent {
		t.Fatal("signalIfUnchanged: must not report a signal sent to a pid that's already gone")
	}
	if got := spy.pids(); len(got) != 0 {
		t.Fatalf("signalPID calls = %v, want none", got)
	}
}

// TestTerminateHungPullUnix_ServerIdentityMismatch_SendsNoSignals exercises
// the full seam terminateHungPull is plugged into: a dolt-server.pid/port
// pair that doesn't match any real dolt sql-server in the (faked) process
// table must return an error and must never call the signal seam — the
// frozen safety invariant that a misidentified server is a no-op, not a
// misdirected kill.
func TestTerminateHungPullUnix_ServerIdentityMismatch_SendsNoSignals(t *testing.T) {
	home := t.TempDir()
	beadsDir := filepath.Join(home, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "dolt-server.pid"), []byte("100\n"), 0o644); err != nil {
		t.Fatalf("write dolt-server.pid: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "dolt-server.port"), []byte("60279\n"), 0o644); err != nil {
		t.Fatalf("write dolt-server.port: %v", err)
	}
	t.Setenv("AGENT_TEAMS_HOME", home)

	origSnapshot, origLookup, origSignal := psSnapshotAll, psLookupPID, signalPID
	spy := &spySignal{}
	psSnapshotAll = func(_ context.Context) ([]psEntry, error) {
		// pid 100 exists, but it's not a dolt sql-server at all.
		return fakePS(psEntry{PID: 100, PPID: 1, LStart: fakeLStart, Command: "some-unrelated-process"}), nil
	}
	psLookupPID = func(_ context.Context, pid int) (psEntry, bool, error) {
		t.Fatalf("psLookupPID must not be called after a server-identity mismatch (pid %d)", pid)
		return psEntry{}, false, nil
	}
	signalPID = spy.fn
	t.Cleanup(func() {
		psSnapshotAll, psLookupPID, signalPID = origSnapshot, origLookup, origSignal
	})

	err := terminateHungPullUnix(context.Background(), inFlightPull{ConnID: 1, Seconds: 200})
	if err == nil {
		t.Fatal("terminateHungPullUnix: want error on server-identity mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "not a dolt sql-server") {
		t.Fatalf("terminateHungPullUnix error = %q, want it to name the identity mismatch", err.Error())
	}
	if got := spy.pids(); len(got) != 0 {
		t.Fatalf("signalPID calls = %v, want none after a server-identity mismatch", got)
	}
}

func TestTerminateHungPullUnix_NoGitChild_SendsNoSignals(t *testing.T) {
	home := t.TempDir()
	beadsDir := filepath.Join(home, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "dolt-server.pid"), []byte("100"), 0o644); err != nil {
		t.Fatalf("write dolt-server.pid: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "dolt-server.port"), []byte("60279"), 0o644); err != nil {
		t.Fatalf("write dolt-server.port: %v", err)
	}
	t.Setenv("AGENT_TEAMS_HOME", home)

	origSnapshot, origSignal := psSnapshotAll, signalPID
	spy := &spySignal{}
	psSnapshotAll = func(_ context.Context) ([]psEntry, error) {
		return fakePS(psEntry{PID: 100, PPID: 1, LStart: fakeLStart, Command: "dolt sql-server -H 127.0.0.1 -P 60279"}), nil
	}
	signalPID = spy.fn
	t.Cleanup(func() {
		psSnapshotAll, signalPID = origSnapshot, origSignal
	})

	err := terminateHungPullUnix(context.Background(), inFlightPull{ConnID: 1, Seconds: 200})
	if err == nil {
		t.Fatal("terminateHungPullUnix: want error when the server has no git-fetch child, got nil")
	}
	if got := spy.pids(); len(got) != 0 {
		t.Fatalf("signalPID calls = %v, want none when there is nothing to target", got)
	}
}

func TestParsePSOutput_RealShapedLine(t *testing.T) {
	line := "34375     1 34375 Tue Sep 15 22:55:55 2026 /opt/homebrew/bin/dolt sql-server -H 127.0.0.1 -P 62318 --loglevel=warning"
	entries := parsePSOutput(line)
	if len(entries) != 1 {
		t.Fatalf("parsePSOutput: got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.PID != 34375 || e.PPID != 1 || e.PGID != 34375 {
		t.Fatalf("parsePSOutput: got pid/ppid/pgid = %d/%d/%d, want 34375/1/34375", e.PID, e.PPID, e.PGID)
	}
	if e.LStart != "Tue Sep 15 22:55:55 2026" {
		t.Fatalf("parsePSOutput: got lstart %q", e.LStart)
	}
	if e.Command != "/opt/homebrew/bin/dolt sql-server -H 127.0.0.1 -P 62318 --loglevel=warning" {
		t.Fatalf("parsePSOutput: got command %q", e.Command)
	}
}

func TestParsePSOutput_SkipsMalformedRows(t *testing.T) {
	out := "not-a-pid ppid pgid lstart-ish\n" + "34375     1 34375 Tue Sep 15 22:55:55 2026 dolt sql-server"
	entries := parsePSOutput(out)
	if len(entries) != 1 {
		t.Fatalf("parsePSOutput: got %d entries, want the malformed row skipped and 1 kept", len(entries))
	}
}
