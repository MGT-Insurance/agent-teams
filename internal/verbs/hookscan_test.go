package verbs_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

// runHookScan dispatches the hook-scan verb through RegisterHookScanKong.
// Reuses fakeExec/fakeExecErr from match_test.go (same package).
func runHookScan(t *testing.T, issues []bd.Issue, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	client := bd.NewClientWithExec("/fake/home", fakeExec(issues))
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     client,
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterHookScanKong(p)
	tokens := append([]string{"hook-scan"}, args...)
	kctx, parseErr := p.Parse(tokens)
	if parseErr != nil {
		exitCode = cli.ExitCode(parseErr)
		return outBuf.String(), errBuf.String(), exitCode
	}
	kctx.Bind(ctx)
	runErr := kctx.Run(ctx)
	exitCode = cli.ExitCode(runErr)
	return outBuf.String(), errBuf.String(), exitCode
}

func runHookScanErr(t *testing.T, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	client := bd.NewClientWithExec("/fake/home", fakeExecErr())
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     client,
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterHookScanKong(p)
	tokens := append([]string{"hook-scan"}, args...)
	kctx, parseErr := p.Parse(tokens)
	if parseErr != nil {
		exitCode = cli.ExitCode(parseErr)
		return outBuf.String(), errBuf.String(), exitCode
	}
	kctx.Bind(ctx)
	runErr := kctx.Run(ctx)
	exitCode = cli.ExitCode(runErr)
	return outBuf.String(), errBuf.String(), exitCode
}

// countingExec wraps fakeExec but records how many times bd was invoked, so
// tests can assert the "exactly one bd call" structural guarantee.
func countingExec(t *testing.T, payload []bd.Issue, count *int) bd.ExecFunc {
	t.Helper()
	inner := fakeExec(payload)
	return func(name string, args ...string) ([]byte, []byte, error) {
		*count++
		return inner(name, args...)
	}
}

func runHookScanCounting(t *testing.T, issues []bd.Issue, args []string) (stdout string, calls int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	calls = 0
	client := bd.NewClientWithExec("/fake/home", countingExec(t, issues, &calls))
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     client,
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterHookScanKong(p)
	tokens := append([]string{"hook-scan"}, args...)
	kctx, parseErr := p.Parse(tokens)
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}
	kctx.Bind(ctx)
	if runErr := kctx.Run(ctx); runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return outBuf.String(), calls
}

// ── path resolution + unread, single-call guarantee ─────────────────────────

func TestHookScanExactMatchNoMail(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
	}
	stdout, calls := runHookScanCounting(t, issues, []string{path})
	if calls != 1 {
		t.Errorf("hook-scan: made %d bd calls, want exactly 1", calls)
	}
	if !strings.Contains(stdout, "id: at-111") {
		t.Errorf("hook-scan: stdout %q missing id line", stdout)
	}
	if !strings.Contains(stdout, "unread: 0") {
		t.Errorf("hook-scan: stdout %q missing unread:0", stdout)
	}
}

func TestHookScanAncestorSubdirectory(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: /a/b/wt"},
	}
	stdout, _, code := runHookScan(t, issues, []string{"/a/b/wt/apps/mithril"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: at-111") {
		t.Errorf("stdout %q missing id line", stdout)
	}
}

func TestHookScanUnreadPresent(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
		{ID: "msg-1", IssueType: "message", Assignee: "at-111", Status: "open", Labels: []string{"delivery:pending"}},
	}
	stdout, _, code := runHookScan(t, issues, []string{path})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: at-111") {
		t.Errorf("stdout %q missing id line", stdout)
	}
	if !strings.Contains(stdout, "unread: 1") {
		t.Errorf("stdout %q missing unread:1", stdout)
	}
}

func TestHookScanUnreadAlreadyRead(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
		{ID: "msg-1", IssueType: "message", Assignee: "at-111", Status: "open", Labels: []string{"read"}},
	}
	stdout, _, code := runHookScan(t, issues, []string{path})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "unread: 0") {
		t.Errorf("stdout %q missing unread:0 — a 'read'-labeled message must not count", stdout)
	}
}

// TestHookScanUnreadCountReflectsActualCount verifies FIX 4: unread reports
// the actual count of unread messages, not a 0/1 flag capped at the first
// match.
func TestHookScanUnreadCountReflectsActualCount(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
		{ID: "msg-1", IssueType: "message", Assignee: "at-111", Status: "open"},
		{ID: "msg-2", IssueType: "message", Assignee: "at-111", Status: "open"},
		{ID: "msg-3", IssueType: "message", Assignee: "at-111", Status: "open", Labels: []string{"read"}},
	}
	stdout, _, code := runHookScan(t, issues, []string{path})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "unread: 2") {
		t.Errorf("stdout %q missing unread:2 (2 unread, 1 read, must not cap at 1)", stdout)
	}
}

// TestHookScanExcludesMessageBeadFromMatch verifies FIX 3: --include-infra
// widens the list to include message-type beads, but a message bead must
// never be a match candidate for matchByWorktreeOrAncestor even if its body
// happens to contain a literal "worktree: <path>" line matching the query —
// only the real initiative may match.
func TestHookScanExcludesMessageBeadFromMatch(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		// A message bead whose body coincidentally contains a worktree: line
		// matching the query path — must never be treated as a match
		// candidate.
		{ID: "msg-1", IssueType: "message", Assignee: "at-111", Status: "open", Description: "worktree: " + path},
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
	}
	stdout, _, code := runHookScan(t, issues, []string{path})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: at-111") {
		t.Errorf("stdout %q must report the initiative id: at-111, not the message bead", stdout)
	}
	if strings.Contains(stdout, "id: msg-1") {
		t.Errorf("stdout %q must never match a message-type bead", stdout)
	}
}

func TestHookScanUnreadForOtherRecipientIgnored(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
		{ID: "at-222", Title: "Other", Description: "worktree: /other"},
		{ID: "msg-1", IssueType: "message", Assignee: "at-222", Status: "open"},
	}
	stdout, _, code := runHookScan(t, issues, []string{path})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "unread: 0") {
		t.Errorf("stdout %q missing unread:0 — mail addressed to a different initiative must not count", stdout)
	}
}

func TestHookScanNoMatchSilent(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-111", Title: "One", Description: "worktree: /a/b/wt"},
	}
	stdout, stderr, code := runHookScan(t, issues, []string{"/no/such/path"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout %q, want empty (no match)", stdout)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr %q — hooks must stay silent", stderr)
	}
}

func TestHookScanRepoDisabledSuppressed(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, repoconfig.FileName), []byte("disabled: true\n"), 0o644); err != nil {
		t.Fatalf("write .agent-teams: %v", err)
	}
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: /a/b/wt\nrepo: " + repoDir + "\n"},
	}
	stdout, stderr, code := runHookScan(t, issues, []string{"/a/b/wt"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout %q, want empty (repo disabled)", stdout)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr %q", stderr)
	}
}

// TestHookScanSessionFirstHitFromNonMatchingCwd is the hook-scan half of ring
// .4 (at-1k234): a session tied to an initiative via "session: <id>" resolves
// that initiative — and its unread count — even when the path being scanned
// matches no registered worktree, restoring the mail signal for a session
// whose launch cwd doesn't match its registered worktree.
func TestHookScanSessionFirstHitFromNonMatchingCwd(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-mine", Title: "Mine", Description: "worktree: /a/b/wt\nsession: sess-mine\n"},
		{ID: "msg-1", IssueType: "message", Assignee: "at-mine", Status: "open"},
	}
	stdout, stderr, code := runHookScan(t, issues, []string{"/no/such/path", "--session-id", "sess-mine"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: at-mine") {
		t.Errorf("stdout %q missing id line (session tie must win over a non-matching path)", stdout)
	}
	if !strings.Contains(stdout, "unread: 1") {
		t.Errorf("stdout %q missing unread:1", stdout)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr %q", stderr)
	}
}

// TestHookScanEmptySessionIDUsesPathResolution verifies an empty
// --session-id (flag omitted, or passed as "") leaves path resolution exactly
// as before this field existed.
func TestHookScanEmptySessionIDUsesPathResolution(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
	}
	stdout, _, code := runHookScan(t, issues, []string{path, "--session-id="})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: at-111") {
		t.Errorf("stdout %q missing id line", stdout)
	}
}

// TestHookScanSessionIDNoTieFallsBackToPath verifies a --session-id that ties
// to no open initiative falls through to path resolution unchanged.
func TestHookScanSessionIDNoTieFallsBackToPath(t *testing.T) {
	path := "/a/b/wt"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Mine", Description: "worktree: " + path},
	}
	stdout, _, code := runHookScan(t, issues, []string{path, "--session-id", "sess-untied"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: at-111") {
		t.Errorf("stdout %q missing id line (path fallback)", stdout)
	}
}

// TestHookScanBDErrorPropagates verifies FIX 1: an infrastructure (bd list)
// failure now propagates as a non-zero exit instead of being silently
// swallowed as exit 0. inbox-drain.sh's `2>/dev/null || true` capture at the
// call site absorbs this unchanged, so hook behavior is unaffected.
func TestHookScanBDErrorPropagates(t *testing.T) {
	stdout, _, code := runHookScanErr(t, []string{"/any/path"})
	if code == 0 {
		t.Errorf("exit code %d, want non-zero — a bd failure must propagate, not swallow", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout %q, want empty", stdout)
	}
}

// ── --id fast path (e.g. the Steward, unresolvable via worktree matching) ──

func TestHookScanByID(t *testing.T) {
	issues := []bd.Issue{
		{ID: "msg-1", IssueType: "message", Assignee: "steward", Status: "open"},
	}
	stdout, _, code := runHookScan(t, issues, []string{"--id=steward"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "id: steward") {
		t.Errorf("stdout %q missing id line", stdout)
	}
	if !strings.Contains(stdout, "unread: 1") {
		t.Errorf("stdout %q missing unread:1", stdout)
	}
}

func TestHookScanByIDNoMail(t *testing.T) {
	stdout, _, code := runHookScan(t, nil, []string{"--id=steward"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "unread: 0") {
		t.Errorf("stdout %q missing unread:0", stdout)
	}
}

func TestHookScanRequiresPathOrID(t *testing.T) {
	_, _, code := runHookScan(t, nil, nil)
	if code != 2 {
		t.Errorf("exit code %d, want 2 (usage error)", code)
	}
}

func TestHookScanPathAndIDMutuallyExclusive(t *testing.T) {
	_, _, code := runHookScan(t, nil, []string{"/a/b", "--id=steward"})
	if code != 2 {
		t.Errorf("exit code %d, want 2 (usage error)", code)
	}
}

// ── FIX 2: duplicate-steward guard backstop (agent-teams-e3mq.31) ──────────
//
// hook-scan's --id path must fire checkStewardInboxGuard exactly as the old
// `ateam mail inbox --peek` did (inboxKong.Run, messaging.go), restoring the
// defense-in-depth backstop the hook-scan collapse dropped. Mirrors the
// guard's decision table (messaging.go's checkStewardInboxGuard doc).

// writeHookScanWatcherPidfile writes a watcher pidfile entry to
// <home>/mailbox/steward.watcher.pid, mirroring wake-watcher.sh's claim
// write ("pid\tsession_id"). session == "" writes an old-format bare-pid
// entry.
func writeHookScanWatcherPidfile(t *testing.T, home string, pid int, session string) {
	t.Helper()
	mailboxDir := filepath.Join(home, "mailbox")
	if err := os.MkdirAll(mailboxDir, 0o755); err != nil {
		t.Fatalf("mkdir mailbox: %v", err)
	}
	entry := strconv.Itoa(pid)
	if session != "" {
		entry += "\t" + session
	}
	path := filepath.Join(mailboxDir, "steward.watcher.pid")
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
}

// runHookScanWithHome is runHookScan but with a caller-supplied (real,
// writable) Home directory — needed for the steward-guard tests, which read
// a pidfile off ctx.Home from disk.
func runHookScanWithHome(t *testing.T, home string, issues []bd.Issue, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	client := bd.NewClientWithExec(home, fakeExec(issues))
	ctx := &cli.Context{
		Home:   home,
		BD:     client,
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	p, err := cli.NewParser()
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	verbs.RegisterHookScanKong(p)
	tokens := append([]string{"hook-scan"}, args...)
	kctx, parseErr := p.Parse(tokens)
	if parseErr != nil {
		exitCode = cli.ExitCode(parseErr)
		return outBuf.String(), errBuf.String(), exitCode
	}
	kctx.Bind(ctx)
	runErr := kctx.Run(ctx)
	exitCode = cli.ExitCode(runErr)
	return outBuf.String(), errBuf.String(), exitCode
}

func TestHookScanStewardGuard_DuplicateSession_Refuses(t *testing.T) {
	home := t.TempDir()
	writeHookScanWatcherPidfile(t, home, os.Getpid(), "incumbent-session")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "duplicate-session")

	issues := []bd.Issue{
		{ID: "msg-1", IssueType: "message", Assignee: "steward", Status: "open"},
	}
	stdout, _, code := runHookScanWithHome(t, home, issues, []string{"--id=steward"})
	if code == 0 {
		t.Fatalf("expected non-zero exit for a duplicate steward session, got 0 (stdout=%q)", stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout %q, want empty — a refused duplicate steward must not report id/unread", stdout)
	}
}

func TestHookScanStewardGuard_SameSession_Proceeds(t *testing.T) {
	home := t.TempDir()
	writeHookScanWatcherPidfile(t, home, os.Getpid(), "my-session")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "my-session")

	issues := []bd.Issue{
		{ID: "msg-1", IssueType: "message", Assignee: "steward", Status: "open"},
	}
	stdout, _, code := runHookScanWithHome(t, home, issues, []string{"--id=steward"})
	if code != 0 {
		t.Fatalf("exit code %d, want 0 (caller is the session of record): stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "id: steward") {
		t.Errorf("stdout %q missing id line", stdout)
	}
	if !strings.Contains(stdout, "unread: 1") {
		t.Errorf("stdout %q missing unread:1", stdout)
	}
}

func TestHookScanStewardGuard_NoPidfile_Proceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CODE_SESSION_ID", "any-session")

	stdout, _, code := runHookScanWithHome(t, home, nil, []string{"--id=steward"})
	if code != 0 {
		t.Fatalf("exit code %d, want 0 (no pidfile means no session-of-record to protect): stdout=%q", code, stdout)
	}
	if !strings.Contains(stdout, "id: steward") {
		t.Errorf("stdout %q missing id line", stdout)
	}
}
