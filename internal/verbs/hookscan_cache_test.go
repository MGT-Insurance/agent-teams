package verbs_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

// cacheScan runs hook-scan against a real temp Home with a recording bd fake
// and returns stdout, stderr, and the bd call count.
func cacheScan(t *testing.T, home string, issues []bd.Issue, args ...string) (stdout, stderr string, calls int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	var argvs [][]string
	ctx := &cli.Context{Home: home, BD: bd.NewClientWithExec(home, recordingExec(issues, &argvs)), Stdout: &outBuf, Stderr: &errBuf}
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterHookScanKong(p)
	kctx, parseErr := p.Parse(append([]string{"hook-scan"}, args...))
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}
	kctx.Bind(ctx)
	if runErr := kctx.Run(ctx); runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return outBuf.String(), errBuf.String(), len(argvs)
}

type cacheFile struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	Unread    int       `json:"unread"`
	CheckedAt time.Time `json:"checked_at"`
	Trusted   bool      `json:"trusted"`
}

func cachePath(home, key string) string {
	return filepath.Join(home, "cache", "hookscan", key+".json")
}

func putCache(t *testing.T, home, key string, e cacheFile) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(cachePath(home, key)), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(e)
	if err := os.WriteFile(cachePath(home, key), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func getCache(t *testing.T, home, key string) (cacheFile, bool) {
	t.Helper()
	data, err := os.ReadFile(cachePath(home, key))
	if err != nil {
		return cacheFile{}, false
	}
	var e cacheFile
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("cache not JSON: %v", err)
	}
	return e, true
}

func ring(t *testing.T, home, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "mailbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mailbox", id+".wake"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	cachePath1  = "/a/b/wt"
	cacheInit   = bd.Issue{ID: "at-111", Description: "worktree: /a/b/wt\n"}
	cacheMail   = bd.Issue{ID: "at-m1", IssueType: "message", Assignee: "at-111", Status: "open"}
	cacheWithMl = []bd.Issue{cacheInit, cacheMail}
)

func TestHookScanCache_FullPathWritesTrustedEntry(t *testing.T) {
	home := t.TempDir()
	stdout, _, calls := cacheScan(t, home, []bd.Issue{cacheInit}, cachePath1, "--session-id", "s1")
	if calls != 1 || stdout != "id: at-111\nunread: 0\n" {
		t.Fatalf("calls=%d stdout=%q", calls, stdout)
	}
	e, ok := getCache(t, home, "s1")
	if !ok || e.ID != "at-111" || e.Unread != 0 || !e.Trusted || time.Since(e.CheckedAt) > time.Minute {
		t.Fatalf("cache = %+v ok=%v", e, ok)
	}
}

func TestHookScanCache_QuietPromptZeroBDCalls(t *testing.T) {
	home := t.TempDir()
	putCache(t, home, "s1", cacheFile{ID: "at-111", CheckedAt: time.Now().Add(-time.Minute), Trusted: true})
	stdout, _, calls := cacheScan(t, home, nil, cachePath1, "--session-id", "s1")
	if calls != 0 || stdout != "id: at-111\nunread: 0\n" {
		t.Fatalf("calls=%d stdout=%q", calls, stdout)
	}
}

func TestHookScanCache_FullPathCases(t *testing.T) {
	old := time.Now().Add(-11 * time.Minute)
	recent := time.Now().Add(-time.Minute)
	cases := []struct {
		name   string
		entry  *cacheFile // nil = no cache file
		raw    string     // non-empty = write this raw text instead
		bell   bool
		args   []string
		reason string // expected stderr reason when mail is found
	}{
		{name: "stale", entry: &cacheFile{ID: "at-111", CheckedAt: old, Trusted: true}, reason: "ttl"},
		{name: "untrusted", entry: &cacheFile{ID: "at-111", CheckedAt: recent}, reason: "untrusted"},
		{name: "missing"},
		{name: "corrupt", raw: "{not json"},
		{name: "doorbell", entry: &cacheFile{ID: "at-111", CheckedAt: recent, Trusted: true}, bell: true},
		{name: "cached unread nags", entry: &cacheFile{ID: "at-111", Unread: 1, CheckedAt: recent, Trusted: true}},
		{name: "future timestamp", entry: &cacheFile{ID: "at-111", CheckedAt: time.Now().Add(time.Hour), Trusted: true}, reason: "ttl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			args := tc.args
			if args == nil {
				args = []string{cachePath1, "--session-id", "s1"}
			}
			switch {
			case tc.raw != "":
				if err := os.MkdirAll(filepath.Dir(cachePath(home, "s1")), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath(home, "s1"), []byte(tc.raw), 0o644); err != nil {
					t.Fatal(err)
				}
			case tc.entry != nil:
				putCache(t, home, "s1", *tc.entry)
			}
			if tc.bell {
				ring(t, home, "at-111")
			}
			stdout, stderr, calls := cacheScan(t, home, cacheWithMl, args...)
			if calls != 1 {
				t.Fatalf("bd calls = %d, want 1", calls)
			}
			if stdout != "id: at-111\nunread: 1\n" {
				t.Errorf("stdout = %q", stdout)
			}
			if tc.reason == "" && stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if tc.reason != "" && !strings.Contains(stderr, "reason="+tc.reason) {
				t.Errorf("stderr = %q, want reason=%s", stderr, tc.reason)
			}
		})
	}
}

func TestHookScanCache_NoSessionIDAlwaysFullAndNoCache(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, _, calls := cacheScan(t, home, []bd.Issue{cacheInit}, cachePath1); calls != 1 {
			t.Fatalf("run %d: bd calls = %d, want 1", i, calls)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
		t.Errorf("cache dir exists without a session id: %v", err)
	}
}

// The prompt that consumes a doorbell writes an untrusted entry, so the next
// prompt rescans even though the entry is fresh and reports no unread mail;
// the rescan then trusts the cache again.
func TestHookScanCache_DoorbellMarksUntrustedAndNextPromptRescans(t *testing.T) {
	home := t.TempDir()
	putCache(t, home, "s1", cacheFile{ID: "at-111", CheckedAt: time.Now(), Trusted: true})
	ring(t, home, "at-111")
	_, _, calls := cacheScan(t, home, []bd.Issue{cacheInit}, cachePath1, "--session-id", "s1")
	if calls != 1 {
		t.Fatalf("doorbell prompt: bd calls = %d, want 1", calls)
	}
	if e, _ := getCache(t, home, "s1"); e.Trusted {
		t.Fatalf("cache trusted after a doorbell scan: %+v", e)
	}
	// inbox-drain.sh deletes the doorbell after hook-scan returns.
	if err := os.Remove(filepath.Join(home, "mailbox", "at-111.wake")); err != nil {
		t.Fatal(err)
	}
	// Mail landed after that list and lost its doorbell.
	stdout, stderr, calls := cacheScan(t, home, cacheWithMl, cachePath1, "--session-id", "s1")
	if calls != 1 || !strings.Contains(stdout, "unread: 1") {
		t.Fatalf("next prompt: calls=%d stdout=%q", calls, stdout)
	}
	if !strings.Contains(stderr, "reason=untrusted") {
		t.Errorf("stderr = %q, want reason=untrusted", stderr)
	}
	if e, _ := getCache(t, home, "s1"); !e.Trusted || e.Unread != 1 {
		t.Errorf("cache after rescan = %+v", e)
	}
}

func TestHookScanCache_NoMatchDeletesEntry(t *testing.T) {
	home := t.TempDir()
	putCache(t, home, "s1", cacheFile{ID: "at-111", CheckedAt: time.Now().Add(-time.Hour), Trusted: true})
	stdout, _, calls := cacheScan(t, home, nil, cachePath1, "--session-id", "s1")
	if calls != 1 || stdout != "" {
		t.Fatalf("calls=%d stdout=%q", calls, stdout)
	}
	if _, ok := getCache(t, home, "s1"); ok {
		t.Error("cache survived a no-match scan")
	}
}

// A disabled repo's cache entry is untrusted, so every prompt rescans (one bd
// call) and stays silent.
func TestHookScanCache_DisabledRepoStaysSilentOnRescan(t *testing.T) {
	home := t.TempDir()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, repoconfig.FileName), []byte("disabled: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	issue := bd.Issue{ID: "at-111", Description: "worktree: /a/b/wt\nrepo: " + repoDir + "\n"}
	stdout, _, calls := cacheScan(t, home, []bd.Issue{issue}, cachePath1, "--session-id", "s1")
	if calls != 1 || stdout != "" {
		t.Fatalf("full: calls=%d stdout=%q", calls, stdout)
	}
	stdout, _, calls = cacheScan(t, home, []bd.Issue{issue}, cachePath1, "--session-id", "s1")
	if calls != 1 || stdout != "" {
		t.Fatalf("rescan: calls=%d stdout=%q", calls, stdout)
	}
}

func TestHookScanCache_StewardFormKeyedByID(t *testing.T) {
	home := t.TempDir()
	stdout, _, calls := cacheScan(t, home, nil, "--id=notsteward")
	if calls != 1 || stdout != "id: notsteward\nunread: 0\n" {
		t.Fatalf("full: calls=%d stdout=%q", calls, stdout)
	}
	if _, ok := getCache(t, home, "notsteward"); !ok {
		t.Fatal("no cache written for --id form")
	}
	stdout, _, calls = cacheScan(t, home, nil, "--id=notsteward")
	if calls != 0 || stdout != "id: notsteward\nunread: 0\n" {
		t.Fatalf("fast: calls=%d stdout=%q", calls, stdout)
	}
}

func TestHookScanCache_UnsafeKeyUsesNoCache(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, _, calls := cacheScan(t, home, []bd.Issue{cacheInit}, cachePath1, "--session-id", "../evil"); calls != 1 {
			t.Fatalf("run %d: bd calls = %d, want 1", i, calls)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
		t.Errorf("cache dir exists for an unsafe key: %v", err)
	}
}

// A hint-resolved scan seeds the cache like any other resolution.
func TestHookScanCache_HintResolvedWritesEntry(t *testing.T) {
	home := t.TempDir()
	stdout, _, calls := cacheScan(t, home, []bd.Issue{cacheInit}, "/no/such/path", "--session-id", "s1", "--hint-id", "at-111")
	if calls != 1 || stdout != "id: at-111\nunread: 0\n" {
		t.Fatalf("calls=%d stdout=%q", calls, stdout)
	}
	if e, ok := getCache(t, home, "s1"); !ok || e.ID != "at-111" || !e.Trusted {
		t.Fatalf("cache = %+v ok=%v", e, ok)
	}
}

// A hint naming no open initiative, with no path match, deletes the entry.
func TestHookScanCache_HintMissNoMatchDeletesEntry(t *testing.T) {
	home := t.TempDir()
	putCache(t, home, "s1", cacheFile{ID: "at-111", CheckedAt: time.Now().Add(-time.Hour), Trusted: true})
	stdout, _, _ := cacheScan(t, home, nil, "/no/such/path", "--session-id", "s1", "--hint-id", "at-gone")
	if stdout != "" {
		t.Fatalf("stdout=%q", stdout)
	}
	if _, ok := getCache(t, home, "s1"); ok {
		t.Error("cache survived a hint-miss no-match scan")
	}
}
