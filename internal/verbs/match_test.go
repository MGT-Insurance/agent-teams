package verbs_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/erlloyd/agent-teams/internal/bd"
	"github.com/erlloyd/agent-teams/internal/cli"
	"github.com/erlloyd/agent-teams/internal/verbs"
)

// fakeExec returns a fixed JSON payload as stdout for any bd call.
func fakeExec(payload []bd.Issue) bd.ExecFunc {
	return func(_ string, _ ...string) ([]byte, []byte, error) {
		out, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		return out, nil, nil
	}
}

// fakeExecErr returns an error for every bd call.
func fakeExecErr() bd.ExecFunc {
	return func(_ string, _ ...string) ([]byte, []byte, error) {
		return nil, []byte("bd: something went wrong"), &testExecError{}
	}
}

type testExecError struct{}

func (e *testExecError) Error() string { return "exit status 1" }

func matchReg() cli.Registry {
	reg := make(cli.Registry)
	verbs.RegisterMatch(reg)
	return reg
}

func runVerb(t *testing.T, name string, issues []bd.Issue, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	reg := matchReg()
	cmd, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("verb %q not found", name)
	}

	var outBuf, errBuf bytes.Buffer
	client := bd.NewClientWithExec("/fake/home", fakeExec(issues))
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     client,
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	err := cmd.Run(ctx, args)
	exitCode = cli.ExitCode(err)
	return outBuf.String(), errBuf.String(), exitCode
}

func runVerbErr(t *testing.T, name string, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	reg := matchReg()
	cmd, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("verb %q not found", name)
	}

	var outBuf, errBuf bytes.Buffer
	client := bd.NewClientWithExec("/fake/home", fakeExecErr())
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     client,
		Stdout: &outBuf,
		Stderr: &errBuf,
	}
	err := cmd.Run(ctx, args)
	exitCode = cli.ExitCode(err)
	return outBuf.String(), errBuf.String(), exitCode
}

// ── audit tests ───────────────────────────────────────────────────────────────

func TestAuditClean(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-aaa", Title: "Init A", Description: "problem: foo\nworktree: /x/y\nbranch: main"},
		{ID: "at-bbb", Title: "Init B", Description: "worktree: /a/b\nsome: other"},
	}
	stdout, stderr, code := runVerb(t, "audit", issues, nil)
	if code != 0 {
		t.Errorf("audit clean: exit code %d, want 0", code)
	}
	if !strings.Contains(stdout, "audit: clean") {
		t.Errorf("audit clean: stdout %q doesn't contain 'audit: clean'", stdout)
	}
	if stderr != "" {
		t.Errorf("audit clean: unexpected stderr %q", stderr)
	}
}

func TestAuditOffenders(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-aaa", Title: "Good One", Description: "worktree: /x/y\nbranch: main"},
		{ID: "at-bad", Title: "Leaked Bead", Description: "some: data\nno worktree line here"},
	}
	stdout, stderr, code := runVerb(t, "audit", issues, nil)
	if code != 1 {
		t.Errorf("audit offenders: exit code %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("audit offenders: unexpected stdout %q", stdout)
	}
	if !strings.Contains(stderr, "LEAKED work beads") {
		t.Errorf("audit offenders: stderr %q missing LEAKED header", stderr)
	}
	if !strings.Contains(stderr, "at-bad") {
		t.Errorf("audit offenders: stderr %q missing offender id", stderr)
	}
	if !strings.Contains(stderr, "Leaked Bead") {
		t.Errorf("audit offenders: stderr %q missing offender title", stderr)
	}
	if !strings.Contains(stderr, "global workspace holds ONLY initiative-tracking beads") {
		t.Errorf("audit offenders: stderr %q missing guidance line", stderr)
	}
}

func TestAuditBDError(t *testing.T) {
	// bd error: treat as clean (no offenders), exit 0 with clean message.
	_, _, code := runVerbErr(t, "audit", nil)
	if code != 0 {
		t.Errorf("audit bd-error: exit code %d, want 0", code)
	}
}

func TestAuditOffenderHeaderLine(t *testing.T) {
	// Verify the exact header wording matches bash line 66.
	issues := []bd.Issue{
		{ID: "at-x1", Title: "Bad", Description: "no worktree line"},
	}
	_, stderr, _ := runVerb(t, "audit", issues, nil)
	want := "audit: LEAKED work beads in the global workspace — these belong in the PROJECT repo, NOT here:"
	if !strings.Contains(stderr, want) {
		t.Errorf("audit: stderr missing exact header line\ngot:  %q\nwant: %q", stderr, want)
	}
}

// ── resume-match tests ────────────────────────────────────────────────────────

func TestResumeMatchExact(t *testing.T) {
	path := "/a/b/wt-match"
	issues := []bd.Issue{
		{ID: "at-111", Title: "Match Me", Description: "problem: x\nworktree: " + path + "\nbranch: feat"},
		{ID: "at-222", Title: "Other", Description: "worktree: /other/path"},
	}
	stdout, _, code := runVerb(t, "resume-match", issues, []string{path})
	if code != 0 {
		t.Errorf("resume-match exact: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "at-111" {
		t.Errorf("resume-match exact: got %q, want %q", strings.TrimSpace(stdout), "at-111")
	}
}

func TestResumeMatchNoMatch(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-111", Title: "One", Description: "worktree: /a/b/wt"},
	}
	stdout, _, code := runVerb(t, "resume-match", issues, []string{"/no/such/path"})
	if code != 0 {
		t.Errorf("resume-match no-match: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("resume-match no-match: got %q, want empty", stdout)
	}
}

// TestResumeMatchPrefixCollision is case 3c: registered /a/b/wt, querying /a/b must return empty.
func TestResumeMatchPrefixCollision(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-abc", Title: "Registered", Description: "worktree: /a/b/wt\nbranch: main"},
	}
	// /a/b is a prefix of /a/b/wt — must NOT match
	stdout, _, code := runVerb(t, "resume-match", issues, []string{"/a/b"})
	if code != 0 {
		t.Errorf("resume-match prefix-collision: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("resume-match prefix-collision: got %q, want empty (prefix must not match)", stdout)
	}
}

func TestResumeMatchBDError(t *testing.T) {
	stdout, _, code := runVerbErr(t, "resume-match", []string{"/any/path"})
	if code != 0 {
		t.Errorf("resume-match bd-error: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("resume-match bd-error: got %q, want empty", stdout)
	}
}

func TestResumeMatchMissingArg(t *testing.T) {
	_, _, code := runVerb(t, "resume-match", nil, []string{})
	if code != 2 {
		t.Errorf("resume-match missing-arg: exit code %d, want 2", code)
	}
}

// ── resume-match-closed tests ─────────────────────────────────────────────────

func TestResumeMatchClosedExact(t *testing.T) {
	path := "/wt/closed"
	issues := []bd.Issue{
		{ID: "at-c1", Title: "Closed One", Description: "worktree: " + path, CreatedAt: "2026-01-01T00:00:00Z"},
	}
	stdout, _, code := runVerb(t, "resume-match-closed", issues, []string{path})
	if code != 0 {
		t.Errorf("resume-match-closed exact: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "at-c1" {
		t.Errorf("resume-match-closed exact: got %q, want at-c1", strings.TrimSpace(stdout))
	}
}

// TestResumeMatchClosedNewest verifies the most-recently-created match wins.
func TestResumeMatchClosedNewest(t *testing.T) {
	path := "/wt/multi"
	issues := []bd.Issue{
		{ID: "at-old", Title: "Older", Description: "worktree: " + path, CreatedAt: "2025-06-01T00:00:00Z"},
		{ID: "at-new", Title: "Newer", Description: "worktree: " + path, CreatedAt: "2026-06-01T00:00:00Z"},
		{ID: "at-mid", Title: "Middle", Description: "worktree: " + path, CreatedAt: "2026-01-01T00:00:00Z"},
	}
	stdout, _, code := runVerb(t, "resume-match-closed", issues, []string{path})
	if code != 0 {
		t.Errorf("resume-match-closed newest: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "at-new" {
		t.Errorf("resume-match-closed newest: got %q, want at-new", strings.TrimSpace(stdout))
	}
}

func TestResumeMatchClosedNoMatch(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-c2", Title: "Other", Description: "worktree: /other", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	stdout, _, code := runVerb(t, "resume-match-closed", issues, []string{"/no/match"})
	if code != 0 {
		t.Errorf("resume-match-closed no-match: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("resume-match-closed no-match: got %q, want empty", stdout)
	}
}

// TestResumeMatchClosedPrefixCollision ensures /a/b does not match /a/b/wt.
func TestResumeMatchClosedPrefixCollision(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-xyz", Title: "Closed", Description: "worktree: /a/b/wt", CreatedAt: "2026-01-01T00:00:00Z"},
	}
	stdout, _, code := runVerb(t, "resume-match-closed", issues, []string{"/a/b"})
	if code != 0 {
		t.Errorf("resume-match-closed prefix-collision: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("resume-match-closed prefix-collision: got %q, want empty", stdout)
	}
}

func TestResumeMatchClosedBDError(t *testing.T) {
	stdout, _, code := runVerbErr(t, "resume-match-closed", []string{"/any/path"})
	if code != 0 {
		t.Errorf("resume-match-closed bd-error: exit code %d, want 0", code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("resume-match-closed bd-error: got %q, want empty", stdout)
	}
}

func TestResumeMatchClosedMissingArg(t *testing.T) {
	_, _, code := runVerb(t, "resume-match-closed", nil, []string{})
	if code != 2 {
		t.Errorf("resume-match-closed missing-arg: exit code %d, want 2", code)
	}
}
