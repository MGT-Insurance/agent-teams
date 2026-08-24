package verbs_test

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestHookScanBDErrorSilent(t *testing.T) {
	stdout, stderr, code := runHookScanErr(t, []string{"/any/path"})
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("stdout %q, want empty", stdout)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr %q — hooks must fail soft", stderr)
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
