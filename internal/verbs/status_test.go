package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── computeExecutionStatus ────────────────────────────────────────────────────

// session returns a minimal agentSession for a given worktree + status/state pair.
func session(wt, status, state string) agentSession {
	return agentSession{CWD: wt, Status: status, State: state, Kind: "background"}
}

func TestComputeExecutionStatus(t *testing.T) {
	const wt = "/home/agent/worktrees/my-initiative"

	busySession := []agentSession{session(wt, "busy", "")}
	idleSession := []agentSession{session(wt, "idle", "done")}
	workingSession := []agentSession{session(wt, "idle", "working")} // state=working overrides status
	waitingSession := []agentSession{session(wt, "waiting", "")}
	noSession := []agentSession{}
	otherSession := []agentSession{session("/other/path", "busy", "working")} // no cwd match

	// probe/mergeState are left zero throughout this table: these cases predate
	// the merge probe and must be unaffected by it. Rule 3's probe-aware
	// sub-cascade has its own table in TestComputeExecutionStatus_Rule3.
	tests := []struct {
		name     string
		labels   []string
		sessions []agentSession
		wt       string
		want     string
	}{
		// NEEDS-DECISION: human + gate:question; session state is irrelevant.
		{
			name:     "needs-decision: human+gate:question, no session",
			labels:   []string{"human", "gate:question"},
			sessions: noSession,
			wt:       wt,
			want:     "NEEDS-DECISION",
		},
		{
			name:     "needs-decision: human+gate:question, busy session",
			labels:   []string{"human", "gate:question"},
			sessions: busySession,
			wt:       wt,
			want:     "NEEDS-DECISION",
		},
		{
			name:     "needs-decision: human+gate:question+gate:review, no session",
			labels:   []string{"human", "gate:question", "gate:review"},
			sessions: noSession,
			wt:       wt,
			want:     "NEEDS-DECISION",
		},

		// IN-PROGRESS (rule 2): actively working OVERRIDES gate:review.
		{
			name:     "working session with gate:review => IN-PROGRESS not REVIEWABLE",
			labels:   []string{"human", "gate:review"},
			sessions: busySession,
			wt:       wt,
			want:     "IN-PROGRESS",
		},
		{
			name:     "state=working (bg session) with gate:review => IN-PROGRESS",
			labels:   []string{"human", "gate:review"},
			sessions: workingSession,
			wt:       wt,
			want:     "IN-PROGRESS",
		},
		{
			name:     "busy session, no gates => IN-PROGRESS",
			labels:   []string{},
			sessions: busySession,
			wt:       wt,
			want:     "IN-PROGRESS",
		},

		// REVIEWABLE: human + gate:review, NOT actively working.
		{
			name:     "reviewable: idle session, human+gate:review",
			labels:   []string{"human", "gate:review"},
			sessions: idleSession,
			wt:       wt,
			want:     "REVIEWABLE",
		},
		{
			name:     "reviewable: waiting session, human+gate:review",
			labels:   []string{"human", "gate:review"},
			sessions: waitingSession,
			wt:       wt,
			want:     "REVIEWABLE",
		},
		{
			name:     "reviewable: no session, human+gate:review",
			labels:   []string{"human", "gate:review"},
			sessions: noSession,
			wt:       wt,
			want:     "REVIEWABLE",
		},
		{
			name:     "reviewable: no cwd match, human+gate:review",
			labels:   []string{"human", "gate:review"},
			sessions: otherSession,
			wt:       wt,
			want:     "REVIEWABLE",
		},

		// IN-PROGRESS (rule 4): open, no gate.
		{
			name:     "open no gate, idle session => IN-PROGRESS",
			labels:   []string{},
			sessions: idleSession,
			wt:       wt,
			want:     "IN-PROGRESS",
		},
		{
			name:     "open no gate, no session => IN-PROGRESS",
			labels:   []string{},
			sessions: noSession,
			wt:       wt,
			want:     "IN-PROGRESS",
		},
		{
			name:     "human label only (no gate label) => IN-PROGRESS",
			labels:   []string{"human"},
			sessions: noSession,
			wt:       wt,
			want:     "IN-PROGRESS",
		},

		// Exact-line worktree match: prefix collision must NOT join.
		{
			name:     "no false-join on prefix: other/path-extended should not match wt",
			labels:   []string{"human", "gate:review"},
			sessions: []agentSession{session(wt+"-extended", "busy", "working")},
			wt:       wt,
			want:     "REVIEWABLE", // no match => not working => REVIEWABLE
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeExecutionStatus(tc.labels, tc.sessions, tc.wt, "", "")
			if got != tc.want {
				t.Errorf("computeExecutionStatus(%v, ..., %q) = %q, want %q",
					tc.labels, tc.wt, got, tc.want)
			}
		})
	}
}

// ── rule 3's sub-cascade (external_review.go §7) ──────────────────────────────

// TestComputeExecutionStatus_Rule3 pins the order inside rule 3 —
// STALE-MERGED, then the declared label, then REVIEWABLE — plus the two
// invariants that order exists to protect: STALE-MERGED requires
// pr_probe=="ok", and AWAITING-EXTERNAL-REVIEW does not.
func TestComputeExecutionStatus_Rule3(t *testing.T) {
	const wt = "/home/agent/worktrees/rule3"

	reviewGated := []string{"human", "gate:review"}
	handedOff := []string{"human", "gate:review", externalReviewLabel}
	noSession := []agentSession{}
	busySession := []agentSession{session(wt, "busy", "")}

	tests := []struct {
		name       string
		labels     []string
		sessions   []agentSession
		probe      string
		mergeState string
		want       string
	}{
		// (a) STALE-MERGED wins, including over the declared label.
		{"merged, not handed off", reviewGated, noSession, prProbeOK, prStateMerged, StatusStaleMerged},
		{"merged, handed off => merged wins", handedOff, noSession, prProbeOK, prStateMerged, StatusStaleMerged},
		{"closed, handed off => merged wins", handedOff, noSession, prProbeOK, prStateClosed, StatusStaleMerged},

		// (b) the declared label, independent of the probe.
		{"handed off, PR open", handedOff, noSession, prProbeOK, prStateOpen, StatusAwaitingExternalReview},
		{"handed off, probe unreachable", handedOff, noSession, prProbeUnreachable, "", StatusAwaitingExternalReview},
		{"handed off, no PR URL", handedOff, noSession, prProbeNone, "", StatusAwaitingExternalReview},

		// (c) REVIEWABLE — today's answer, unchanged.
		{"review-gated, PR open", reviewGated, noSession, prProbeOK, prStateOpen, "REVIEWABLE"},
		{"review-gated, probe unreachable", reviewGated, noSession, prProbeUnreachable, "", "REVIEWABLE"},
		{"review-gated, no PR URL", reviewGated, noSession, prProbeNone, "", "REVIEWABLE"},

		// A merge state the probe could not vouch for must NEVER produce
		// StatusStaleMerged (external_review.go §5's INVARIANT).
		{"unreachable probe carrying MERGED is ignored", reviewGated, noSession, prProbeUnreachable, prStateMerged, "REVIEWABLE"},

		// Rules 1, 2 and 4 still win over everything rule 3 can say.
		{
			"rule 1 beats a merged, handed-off row",
			[]string{"human", "gate:question", "gate:review", externalReviewLabel},
			noSession, prProbeOK, prStateMerged, "NEEDS-DECISION",
		},
		{"rule 2 beats a merged, handed-off row", handedOff, busySession, prProbeOK, prStateMerged, "IN-PROGRESS"},
		{
			"un-gated: the label is inert and the probe is ignored",
			[]string{externalReviewLabel},
			noSession, prProbeOK, prStateMerged, "IN-PROGRESS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeExecutionStatus(tc.labels, tc.sessions, wt, tc.probe, tc.mergeState)
			if got != tc.want {
				t.Errorf("computeExecutionStatus(%v, ..., probe=%q, state=%q) = %q, want %q",
					tc.labels, tc.probe, tc.mergeState, got, tc.want)
			}
		})
	}
}

// ── isActivelyWorking ─────────────────────────────────────────────────────────

func TestIsActivelyWorking(t *testing.T) {
	const wt = "/path/to/wt"

	tests := []struct {
		name     string
		sessions []agentSession
		wt       string
		want     bool
	}{
		{"busy matches", []agentSession{session(wt, "busy", "")}, wt, true},
		{"state=working matches", []agentSession{session(wt, "idle", "working")}, wt, true},
		{"idle does not match", []agentSession{session(wt, "idle", "done")}, wt, false},
		{"waiting does not match", []agentSession{session(wt, "waiting", "")}, wt, false},
		{"done does not match", []agentSession{session(wt, "idle", "done")}, wt, false},
		{"no session => false", []agentSession{}, wt, false},
		{"cwd mismatch => false", []agentSession{session("/other", "busy", "working")}, wt, false},
		{"prefix is not a match", []agentSession{session(wt+"/sub", "busy", "")}, wt, false},
		{"empty worktree => false", []agentSession{session("", "busy", "")}, "", false},
		// Interactive session: has status but no state.
		{
			"interactive busy session",
			[]agentSession{{CWD: wt, Kind: "interactive", Status: "busy"}},
			wt, true,
		},
		{
			"interactive idle session",
			[]agentSession{{CWD: wt, Kind: "interactive", Status: "idle"}},
			wt, false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isActivelyWorking(tc.sessions, tc.wt)
			if got != tc.want {
				t.Errorf("isActivelyWorking(%v, %q) = %v, want %v",
					tc.sessions, tc.wt, got, tc.want)
			}
		})
	}
}

// ── B1: trailing-slash normalisation ─────────────────────────────────────────

// TestIsActivelyWorking_TrailingSlash verifies that isActivelyWorking treats a
// cwd with a trailing slash as equal to the stored worktree path (which has no
// trailing slash), and vice versa.
func TestIsActivelyWorking_TrailingSlash(t *testing.T) {
	const wt = "/home/agent/worktrees/my-initiative"

	tests := []struct {
		name   string
		cwdFn  func() string // session cwd
		wtFn   func() string // worktree path passed to isActivelyWorking
		status string
		want   bool
	}{
		{
			name:   "session cwd has trailing slash, worktree does not",
			cwdFn:  func() string { return wt + "/" },
			wtFn:   func() string { return wt },
			status: "busy",
			want:   true,
		},
		{
			name:   "session cwd no trailing slash, worktree has trailing slash",
			cwdFn:  func() string { return wt },
			wtFn:   func() string { return wt + "/" },
			status: "busy",
			want:   true,
		},
		{
			name:   "both have trailing slash",
			cwdFn:  func() string { return wt + "/" },
			wtFn:   func() string { return wt + "/" },
			status: "busy",
			want:   true,
		},
		{
			name:   "trailing slash match, state=working",
			cwdFn:  func() string { return wt + "/" },
			wtFn:   func() string { return wt },
			status: "idle",
			want:   true, // state=working below
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := ""
			if tc.name == "trailing slash match, state=working" {
				state = "working"
			}
			sess := []agentSession{session(tc.cwdFn(), tc.status, state)}
			got := isActivelyWorking(sess, tc.wtFn())
			if got != tc.want {
				t.Errorf("isActivelyWorking(cwd=%q, wt=%q) = %v, want %v",
					tc.cwdFn(), tc.wtFn(), got, tc.want)
			}
		})
	}
}

// TestIsActivelyWorking_SymlinkedCwd verifies that a session whose CWD is a
// symlink to the registered worktree path (or vice versa) is still matched —
// regression test for the macOS /tmp -> /private/tmp false negative.
func TestIsActivelyWorking_SymlinkedCwd(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if !isActivelyWorking([]agentSession{session(link, "busy", "")}, real) {
		t.Error("expected match: session cwd via symlink, worktree real path")
	}
	if !isActivelyWorking([]agentSession{session(real, "busy", "")}, link) {
		t.Error("expected match: session cwd real path, worktree via symlink")
	}
}

// TestComputeExecutionStatus_TrailingSlashOverridesReviewGate verifies the
// contract invariant: an initiative with gate:review AND a session whose cwd
// has a trailing slash resolves to IN-PROGRESS (not REVIEWABLE).
func TestComputeExecutionStatus_TrailingSlashOverridesReviewGate(t *testing.T) {
	const wt = "/home/agent/worktrees/my-initiative"

	// Session reports cwd with trailing slash; worktree stored without.
	sess := []agentSession{session(wt+"/", "busy", "")}
	labels := []string{"human", "gate:review"}

	got := computeExecutionStatus(labels, sess, wt, prProbeNone, "")
	if got != "IN-PROGRESS" {
		t.Errorf("computeExecutionStatus with trailing-slash session: got %q, want IN-PROGRESS", got)
	}
}

// ── agentSession JSON decoding ────────────────────────────────────────────────

// TestAgentSessionDecode verifies the extended struct handles both background
// and interactive session shapes without panicking.
func TestAgentSessionDecode(t *testing.T) {
	// Background session (all fields present).
	bgJSON := `[{
		"cwd":       "/worktrees/foo",
		"kind":      "background",
		"status":    "busy",
		"name":      "foo",
		"state":     "working",
		"sessionId": "abc123",
		"pid":       42
	}]`

	var bgSessions []agentSession
	if err := json.Unmarshal([]byte(bgJSON), &bgSessions); err != nil {
		t.Fatalf("background session decode: %v", err)
	}
	s := bgSessions[0]
	if s.CWD != "/worktrees/foo" {
		t.Errorf("CWD = %q, want /worktrees/foo", s.CWD)
	}
	if s.Kind != "background" {
		t.Errorf("Kind = %q, want background", s.Kind)
	}
	if s.Status != "busy" {
		t.Errorf("Status = %q, want busy", s.Status)
	}
	if s.Name != "foo" {
		t.Errorf("Name = %q, want foo", s.Name)
	}
	if s.State != "working" {
		t.Errorf("State = %q, want working", s.State)
	}

	// Interactive session: no state/name/id fields.
	interactiveJSON := `[{
		"cwd":       "/worktrees/bar",
		"kind":      "interactive",
		"status":    "idle",
		"sessionId": "xyz"
	}]`

	var iSessions []agentSession
	if err := json.Unmarshal([]byte(interactiveJSON), &iSessions); err != nil {
		t.Fatalf("interactive session decode: %v", err)
	}
	is := iSessions[0]
	if is.CWD != "/worktrees/bar" {
		t.Errorf("CWD = %q, want /worktrees/bar", is.CWD)
	}
	if is.Kind != "interactive" {
		t.Errorf("Kind = %q, want interactive", is.Kind)
	}
	if is.Status != "idle" {
		t.Errorf("Status = %q, want idle", is.Status)
	}
	// Absent fields must be zero-value — no panic.
	if is.Name != "" {
		t.Errorf("Name = %q, want empty for interactive", is.Name)
	}
	if is.State != "" {
		t.Errorf("State = %q, want empty for interactive", is.State)
	}
}

// ── executionStatusKong.Run (integration-level) ───────────────────────────────

// fakeListJSON builds a bd.Client exec func that returns a JSON array of issues.
func fakeListExec(issues []bd.Issue) func(name string, args ...string) ([]byte, []byte, error) {
	return func(name string, args ...string) ([]byte, []byte, error) {
		raw, _ := json.Marshal(issues)
		return raw, nil, nil
	}
}

// failingPRMerge is a prMergeFunc that fails the test if it is ever called —
// for cases whose initiatives must not be probed at all.
func failingPRMerge(t *testing.T) prMergeFunc {
	t.Helper()
	return func(ownerRepo string, prNumber int) (string, error) {
		t.Errorf("unexpected gh probe for %s#%d", ownerRepo, prNumber)
		return "", fmt.Errorf("unexpected probe")
	}
}

// okPreflight is the preflight seam for cases where gh is available.
func okPreflight() error { return nil }

func TestExecutionStatusCmd_Run_NilCtx(t *testing.T) {
	cmd := &executionStatusKong{agentsFunc: func() ([]agentSession, error) { return nil, nil }}
	err := cmd.Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestExecutionStatusCmd_Run_GracefulDegrade(t *testing.T) {
	// When claude agents --json fails, all entries get execution_status "unknown".
	wt := "/tmp/wt-test"
	issues := []bd.Issue{
		{
			ID:          "at-abc",
			Title:       "test initiative",
			Description: "worktree: " + wt,
			Labels:      []string{"human", "gate:review"},
			Status:      "open",
		},
	}

	bdClient := bd.NewClientWithExec("/fake/home", fakeListExec(issues))
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     bdClient,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}

	agentsErr := fmt.Errorf("claude not in PATH")
	cmd := &executionStatusKong{
		agentsFunc:    func() ([]agentSession, error) { return nil, agentsErr },
		prMergeFunc:   failingPRMerge(t),
		preflightFunc: func() error { t.Error("preflight ran despite the agents join failing"); return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var result []initiativeStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(ctx.Stdout.(*bytes.Buffer).String())), &result); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].ExecutionStatus != "unknown" {
		t.Errorf("expected unknown on agents failure, got %q", result[0].ExecutionStatus)
	}
	// No PR URL in this initiative's notes: never probed, and no stderr noise
	// (external_review.go §8's NO PR URL clause).
	if result[0].PRProbe != prProbeNone {
		t.Errorf("pr_probe = %q, want %q", result[0].PRProbe, prProbeNone)
	}
	if s := ctx.Stderr.(*bytes.Buffer).String(); s != "" {
		t.Errorf("expected no stderr, got %q", s)
	}
}

func TestExecutionStatusCmd_Run_MultipleInitiatives(t *testing.T) {
	wt1 := "/tmp/wt-alpha"
	wt2 := "/tmp/wt-beta"
	wt3 := "/tmp/wt-gamma"
	wt4 := "/tmp/wt-delta"

	issues := []bd.Issue{
		{
			ID:          "at-001",
			Title:       "alpha",
			Description: "worktree: " + wt1,
			Labels:      []string{"human", "gate:question"},
			Notes:       "decision: pick A or B",
			Status:      "open",
		},
		{
			ID:          "at-002",
			Title:       "beta",
			Description: "worktree: " + wt2,
			Labels:      []string{"human", "gate:review"},
			Status:      "open",
		},
		{
			ID:          "at-003",
			Title:       "gamma",
			Description: "worktree: " + wt3,
			Labels:      []string{"human", "gate:review"},
			Status:      "open",
		},
		{
			ID:          "at-004",
			Title:       "delta",
			Description: "worktree: " + wt4,
			Labels:      []string{},
			Status:      "open",
		},
	}

	// wt2 has a busy session (IN-PROGRESS overrides gate:review).
	// wt3 has an idle session (REVIEWABLE).
	// wt4 has no session (IN-PROGRESS — open, no gate).
	fakeSessions := []agentSession{
		{CWD: wt2, Kind: "background", Status: "busy", State: "working"},
		{CWD: wt3, Kind: "background", Status: "idle", State: "done"},
	}

	bdClient := bd.NewClientWithExec("/fake/home", fakeListExec(issues))
	buf := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     bdClient,
		Stdout: buf,
		Stderr: &bytes.Buffer{},
	}

	cmd := &executionStatusKong{
		agentsFunc:    func() ([]agentSession, error) { return fakeSessions, nil },
		prMergeFunc:   failingPRMerge(t), // no initiative here has a PR URL
		preflightFunc: okPreflight,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var result []initiativeStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(result) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(result))
	}

	byID := make(map[string]initiativeStatus, len(result))
	for _, r := range result {
		byID[r.ID] = r
	}

	cases := []struct{ id, want string }{
		{"at-001", "NEEDS-DECISION"},
		{"at-002", "IN-PROGRESS"}, // busy session overrides gate:review
		{"at-003", "REVIEWABLE"},  // idle session + gate:review
		{"at-004", "IN-PROGRESS"}, // open, no gate
	}
	for _, c := range cases {
		got, ok := byID[c.id]
		if !ok {
			t.Errorf("id %s missing from output", c.id)
			continue
		}
		if got.ExecutionStatus != c.want {
			t.Errorf("id %s: execution_status = %q, want %q", c.id, got.ExecutionStatus, c.want)
		}
	}

	// Verify ask is extracted from notes for at-001 (has bare "decision: pick A or B" — not a
	// sentinel block, so no structured ask should be present).
	if byID["at-001"].Ask != nil {
		t.Errorf("at-001: expected nil ask for bare notes (no sentinel block), got %+v", byID["at-001"].Ask)
	}

	// Verify pr field is empty when notes contain no PR URL, and that such a
	// row reports pr_probe="none" without stderr noise.
	for _, id := range []string{"at-001", "at-002", "at-003", "at-004"} {
		if byID[id].PR != "" {
			t.Errorf("%s: expected empty pr, got %q", id, byID[id].PR)
		}
		if byID[id].PRProbe != prProbeNone {
			t.Errorf("%s: pr_probe = %q, want %q", id, byID[id].PRProbe, prProbeNone)
		}
	}
	if s := ctx.Stderr.(*bytes.Buffer).String(); s != "" {
		t.Errorf("expected no stderr, got %q", s)
	}
}

// TestExecutionStatusCmd_Run_AskAndPRFields verifies that the ask and pr fields
// are correctly populated from notes containing a sentinel ask block and a PR URL.
func TestExecutionStatusCmd_Run_AskAndPRFields(t *testing.T) {
	const wt = "/tmp/wt-ask-pr"
	const prURL = "https://github.com/mgt-insurance/agent-teams/pull/42"
	notes := "pr: " + prURL + "\n" +
		"<<<ateam-ask\n" +
		"decision: merge approach A or B?\n" +
		"recommendation: A (simpler)\n" +
		"alternative: B (more flexible)\n" +
		"context: see discussion in PR\n" +
		">>>\n"

	issues := []bd.Issue{
		{
			ID:          "at-ask1",
			Title:       "ask-and-pr test",
			Description: "worktree: " + wt,
			Labels:      []string{"human", "gate:question"},
			Notes:       notes,
			Status:      "open",
		},
	}

	home := t.TempDir()
	bdClient := bd.NewClientWithExec(home, fakeListExec(issues))
	buf := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   home,
		BD:     bdClient,
		Stdout: buf,
		Stderr: &bytes.Buffer{},
	}
	cmd := &executionStatusKong{
		agentsFunc:    func() ([]agentSession, error) { return nil, nil },
		prMergeFunc:   func(string, int) (string, error) { return prStateOpen, nil },
		preflightFunc: okPreflight,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var result []initiativeStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	r := result[0]

	if r.PRProbe != prProbeOK {
		t.Errorf("pr_probe = %q, want %q", r.PRProbe, prProbeOK)
	}
	if r.ExecutionStatus != "NEEDS-DECISION" {
		t.Errorf("execution_status = %q, want NEEDS-DECISION", r.ExecutionStatus)
	}
	if r.PR != prURL {
		t.Errorf("pr = %q, want %q", r.PR, prURL)
	}
	if r.Ask == nil {
		t.Fatal("ask is nil, expected structured block")
	}
	if r.Ask.Decision != "merge approach A or B?" {
		t.Errorf("ask.decision = %q, want %q", r.Ask.Decision, "merge approach A or B?")
	}
	if r.Ask.Recommendation != "A (simpler)" {
		t.Errorf("ask.recommendation = %q, want %q", r.Ask.Recommendation, "A (simpler)")
	}
	if r.Ask.Alternative != "B (more flexible)" {
		t.Errorf("ask.alternative = %q, want %q", r.Ask.Alternative, "B (more flexible)")
	}
	if r.Ask.Context != "see discussion in PR" {
		t.Errorf("ask.context = %q, want %q", r.Ask.Context, "see discussion in PR")
	}
}

// TestExecutionStatusCmd_Run_NilAskWhenNoBlock verifies that ask is null (nil)
// when notes contain no sentinel block, and pr is populated from the notes URL.
func TestExecutionStatusCmd_Run_NilAskWhenNoBlock(t *testing.T) {
	const wt = "/tmp/wt-no-ask"
	const prURL = "https://github.com/mgt-insurance/agent-teams/pull/7"
	notes := "pr: " + prURL + "\nSome plain prose without a structured ask block."

	issues := []bd.Issue{
		{
			ID:          "at-noask",
			Title:       "no-ask test",
			Description: "worktree: " + wt,
			Labels:      []string{"human", "gate:review"},
			Notes:       notes,
			Status:      "open",
		},
	}

	home := t.TempDir()
	bdClient := bd.NewClientWithExec(home, fakeListExec(issues))
	buf := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   home,
		BD:     bdClient,
		Stdout: buf,
		Stderr: &bytes.Buffer{},
	}
	cmd := &executionStatusKong{
		agentsFunc:    func() ([]agentSession, error) { return nil, nil },
		prMergeFunc:   func(string, int) (string, error) { return prStateOpen, nil },
		preflightFunc: okPreflight,
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var result []initiativeStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	r := result[0]

	if r.Ask != nil {
		t.Errorf("ask = %+v, want nil (no sentinel block)", r.Ask)
	}
	if r.PR != prURL {
		t.Errorf("pr = %q, want %q", r.PR, prURL)
	}
	// An OPEN PR leaves rule 3 exactly where it was.
	if r.ExecutionStatus != "REVIEWABLE" {
		t.Errorf("execution_status = %q, want REVIEWABLE", r.ExecutionStatus)
	}
	if r.PRProbe != prProbeOK {
		t.Errorf("pr_probe = %q, want %q", r.PRProbe, prProbeOK)
	}
}

// ── S2: line-anchored close sentinel ─────────────────────────────────────────

// TestExtractLatestAsk_InlineTripleArrow verifies that ">>>" embedded in prose
// (not at the start of a line) does NOT truncate a block's body — the block
// must parse fully.
func TestExtractLatestAsk_InlineTripleArrow(t *testing.T) {
	// ">>>" appears mid-line inside the recommendation field — must not close block.
	notes := "<<<ateam-ask\n" +
		"decision: pick one\n" +
		"recommendation: use A >>> B (A is faster)\n" +
		"alternative: use B\n" +
		">>>\n"

	got, ok := extractLatestAsk(notes)
	if !ok {
		t.Fatal("extractLatestAsk returned ok=false, want true")
	}
	if got.decision != "pick one" {
		t.Errorf("decision = %q, want %q", got.decision, "pick one")
	}
	if got.recommendation != "use A >>> B (A is faster)" {
		t.Errorf("recommendation = %q, want %q", got.recommendation, "use A >>> B (A is faster)")
	}
	if got.alternative != "use B" {
		t.Errorf("alternative = %q, want %q", got.alternative, "use B")
	}
}

// TestExtractLatestAsk_InlineTripleArrowNotPartialParse verifies that a block
// whose context field contains ">>>" mid-line does not produce a partial parse
// (i.e., the context field is not silently truncated).
func TestExtractLatestAsk_InlineTripleArrowNotPartialParse(t *testing.T) {
	notes := "<<<ateam-ask\n" +
		"decision: merge approach\n" +
		"recommendation: approach A\n" +
		"alternative: approach B\n" +
		"context: see git conflict markers (>>>) in history\n" +
		">>>\n"

	got, ok := extractLatestAsk(notes)
	if !ok {
		t.Fatal("extractLatestAsk returned ok=false, want true")
	}
	if got.context != "see git conflict markers (>>>) in history" {
		t.Errorf("context = %q, want full value with embedded >>>", got.context)
	}
}

// ── N1: unclosed block skipped, later valid block wins ────────────────────────

// TestExtractLatestAsk_UnclosedThenValid verifies that an unclosed block is
// skipped and the subsequent valid block is returned (last-valid-wins).
func TestExtractLatestAsk_UnclosedThenValid(t *testing.T) {
	// First block has no closing >>>; second block is well-formed.
	notes := "<<<ateam-ask\n" +
		"decision: stale incomplete block\n" +
		"recommendation: stale-rec\n" +
		"alternative: stale-alt\n" +
		// no closing >>>
		"<<<ateam-ask\n" +
		"decision: valid block decision\n" +
		"recommendation: valid-rec\n" +
		"alternative: valid-alt\n" +
		"context: valid-ctx\n" +
		">>>\n"

	got, ok := extractLatestAsk(notes)
	if !ok {
		t.Fatal("extractLatestAsk returned ok=false; expected valid block to be found")
	}
	if got.decision != "valid block decision" {
		t.Errorf("decision = %q, want %q (unclosed block must be skipped)", got.decision, "valid block decision")
	}
	if got.recommendation != "valid-rec" {
		t.Errorf("recommendation = %q, want valid-rec", got.recommendation)
	}
	if got.context != "valid-ctx" {
		t.Errorf("context = %q, want valid-ctx", got.context)
	}
}

// ── p9dm.17: the merge probe wired into rule 3 ───────────────────────────────

// statusRun runs execution-status over issues with the given seams and returns
// the decoded rows (by id) plus whatever went to stderr. It fails the test if
// stdout is not a single parseable JSON array — stdout purity is contractual
// (external_review.go §8), since consumers parse it.
func statusRun(t *testing.T, home string, issues []bd.Issue, merge prMergeFunc, preflight func() error) (map[string]initiativeStatus, string) {
	t.Helper()

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   home,
		BD:     bd.NewClientWithExec(home, fakeListExec(issues)),
		Stdout: stdout,
		Stderr: stderr,
	}
	cmd := &executionStatusKong{
		agentsFunc:    func() ([]agentSession, error) { return nil, nil },
		prMergeFunc:   merge,
		preflightFunc: preflight,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var rows []initiativeStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &rows); err != nil {
		t.Fatalf("stdout is not pure JSON (%v): %q", err, stdout.String())
	}
	byID := make(map[string]initiativeStatus, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	return byID, stderr.String()
}

// prIssue builds an open initiative carrying a PR URL in its notes.
func prIssue(id string, prNumber int, labels ...string) bd.Issue {
	notes := ""
	if prNumber > 0 {
		notes = fmt.Sprintf("pr: https://github.com/MGT-Insurance/midgard/pull/%d\n", prNumber)
	}
	return bd.Issue{
		ID:          id,
		Title:       id,
		Description: "worktree: /tmp/wt-" + id,
		Labels:      labels,
		Notes:       notes,
		Status:      "open",
	}
}

// TestExecutionStatusCmd_Run_StaleMergedAndHandoff is the loop-closing check:
// a real run reports STALE-MERGED for a merged PR and AWAITING-EXTERNAL-REVIEW
// for a handed-off one, with the declared answer holding whether or not the
// initiative even has a PR to probe.
func TestExecutionStatusCmd_Run_StaleMergedAndHandoff(t *testing.T) {
	issues := []bd.Issue{
		prIssue("at-merged", 4501, "human", "gate:review"),
		prIssue("at-handed", 4515, "human", "gate:review", externalReviewLabel),
		prIssue("at-nopr", 0, "human", "gate:review", externalReviewLabel),
		prIssue("at-plain", 4600, "human", "gate:review"),
	}
	states := map[int]string{4501: prStateMerged, 4515: prStateOpen, 4600: prStateOpen}

	rows, stderr := statusRun(t, t.TempDir(), issues,
		func(ownerRepo string, n int) (string, error) {
			if ownerRepo != "mgt-insurance/midgard" {
				t.Errorf("probe owner/repo = %q, want lower-cased mgt-insurance/midgard", ownerRepo)
			}
			return states[n], nil
		}, okPreflight)

	want := []struct{ id, status, probe string }{
		{"at-merged", StatusStaleMerged, prProbeOK},
		{"at-handed", StatusAwaitingExternalReview, prProbeOK},
		{"at-nopr", StatusAwaitingExternalReview, prProbeNone},
		{"at-plain", "REVIEWABLE", prProbeOK},
	}
	for _, w := range want {
		got := rows[w.id]
		if got.ExecutionStatus != w.status || got.PRProbe != w.probe {
			t.Errorf("%s: got (%s, %s), want (%s, %s)", w.id, got.ExecutionStatus, got.PRProbe, w.status, w.probe)
		}
	}
	if stderr != "" {
		t.Errorf("expected no stderr on a clean run, got %q", stderr)
	}
}

// TestExecutionStatusCmd_Run_PreflightFailureDegrades covers §8's PREFLIGHT
// path: no probe runs, one stderr line total, and the DECLARED state still
// reports — an unauthenticated machine must not hide a handoff.
func TestExecutionStatusCmd_Run_PreflightFailureDegrades(t *testing.T) {
	home := t.TempDir()
	issues := []bd.Issue{
		prIssue("at-merged", 4501, "human", "gate:review"),
		prIssue("at-handed", 4515, "human", "gate:review", externalReviewLabel),
		prIssue("at-nopr", 0, "human", "gate:review", externalReviewLabel),
	}

	rows, stderr := statusRun(t, home, issues, failingPRMerge(t),
		func() error { return fmt.Errorf("gh not found on PATH") })

	want := []struct{ id, status, probe string }{
		// The merge verdict is gone, so this falls back to today's answer —
		// never STALE-MERGED (§5's INVARIANT).
		{"at-merged", "REVIEWABLE", prProbeUnreachable},
		{"at-handed", StatusAwaitingExternalReview, prProbeUnreachable},
		{"at-nopr", StatusAwaitingExternalReview, prProbeNone},
	}
	for _, w := range want {
		got := rows[w.id]
		if got.ExecutionStatus != w.status || got.PRProbe != w.probe {
			t.Errorf("%s: got (%s, %s), want (%s, %s)", w.id, got.ExecutionStatus, got.PRProbe, w.status, w.probe)
		}
	}
	if n := strings.Count(strings.TrimSpace(stderr), "\n") + 1; stderr == "" || n != 1 {
		t.Errorf("want exactly one stderr line, got %d: %q", n, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, prStateFileName)); !os.IsNotExist(err) {
		t.Errorf("cache file written despite no probe running (stat err = %v)", err)
	}
}

// TestExecutionStatusCmd_Run_ProbeFailureIsCached covers §8's PER-PR FAILURE
// path: one stderr line when the negative entry is newly written, and none on
// the next run inside prProbeFailureTTL — the cache is what keeps a dead PR
// from costing a line per hung tick.
func TestExecutionStatusCmd_Run_ProbeFailureIsCached(t *testing.T) {
	home := t.TempDir()
	issues := []bd.Issue{prIssue("at-dead", 999, "human", "gate:review")}

	probes := 0
	merge := func(string, int) (string, error) {
		probes++
		return "", fmt.Errorf("gh: HTTP 404")
	}

	rows, stderr := statusRun(t, home, issues, merge, okPreflight)
	if got := rows["at-dead"]; got.ExecutionStatus != "REVIEWABLE" || got.PRProbe != prProbeUnreachable {
		t.Errorf("first run: got (%s, %s), want (REVIEWABLE, %s)", got.ExecutionStatus, got.PRProbe, prProbeUnreachable)
	}
	if !strings.Contains(stderr, "mgt-insurance/midgard#999") {
		t.Errorf("first run: want one stderr line naming the PR, got %q", stderr)
	}

	rows, stderr = statusRun(t, home, issues, merge, okPreflight)
	if probes != 1 {
		t.Errorf("second run re-probed: %d gh calls, want 1 (negative entry is cached for %v)", probes, prProbeFailureTTL)
	}
	if got := rows["at-dead"]; got.PRProbe != prProbeUnreachable {
		t.Errorf("second run: pr_probe = %q, want %q", got.PRProbe, prProbeUnreachable)
	}
	if stderr != "" {
		t.Errorf("second run: cache hit must not repeat the stderr line, got %q", stderr)
	}
}

// TestExecutionStatusCmd_Run_CacheHitSkipsProbe verifies the success TTL: a
// second run inside prProbeSuccessTTL answers STALE-MERGED from the cache
// without shelling out again.
func TestExecutionStatusCmd_Run_CacheHitSkipsProbe(t *testing.T) {
	home := t.TempDir()
	issues := []bd.Issue{prIssue("at-merged", 4501, "human", "gate:review")}

	probes := 0
	merge := func(string, int) (string, error) {
		probes++
		return prStateMerged, nil
	}

	if rows, _ := statusRun(t, home, issues, merge, okPreflight); rows["at-merged"].ExecutionStatus != StatusStaleMerged {
		t.Fatalf("first run: execution_status = %q, want %s", rows["at-merged"].ExecutionStatus, StatusStaleMerged)
	}
	rows, _ := statusRun(t, home, issues, merge, okPreflight)
	if probes != 1 {
		t.Errorf("%d gh calls across two runs, want 1", probes)
	}
	if got := rows["at-merged"]; got.ExecutionStatus != StatusStaleMerged || got.PRProbe != prProbeOK {
		t.Errorf("second run: got (%s, %s), want (%s, %s)", got.ExecutionStatus, got.PRProbe, StatusStaleMerged, prProbeOK)
	}
}
