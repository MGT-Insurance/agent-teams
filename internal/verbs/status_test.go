package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
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
			got := computeExecutionStatus(tc.labels, tc.sessions, tc.wt)
			if got != tc.want {
				t.Errorf("computeExecutionStatus(%v, ..., %q) = %q, want %q",
					tc.labels, tc.wt, got, tc.want)
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
		name    string
		cwdFn   func() string // session cwd
		wtFn    func() string // worktree path passed to isActivelyWorking
		status  string
		want    bool
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

// TestComputeExecutionStatus_TrailingSlashOverridesReviewGate verifies the
// contract invariant: an initiative with gate:review AND a session whose cwd
// has a trailing slash resolves to IN-PROGRESS (not REVIEWABLE).
func TestComputeExecutionStatus_TrailingSlashOverridesReviewGate(t *testing.T) {
	const wt = "/home/agent/worktrees/my-initiative"

	// Session reports cwd with trailing slash; worktree stored without.
	sess := []agentSession{session(wt+"/", "busy", "")}
	labels := []string{"human", "gate:review"}

	got := computeExecutionStatus(labels, sess, wt)
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

// ── executionStatusCmd.Run (integration-level) ────────────────────────────────

// fakeListJSON builds a bd.Client exec func that returns a JSON array of issues.
func fakeListExec(issues []bd.Issue) func(name string, args ...string) ([]byte, []byte, error) {
	return func(name string, args ...string) ([]byte, []byte, error) {
		raw, _ := json.Marshal(issues)
		return raw, nil, nil
	}
}

func TestExecutionStatusCmd_Run_NilCtx(t *testing.T) {
	cmd := &executionStatusCmd{agentsFunc: func() ([]agentSession, error) { return nil, nil }}
	err := cmd.Run(nil, nil)
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
	cmd := &executionStatusCmd{agentsFunc: func() ([]agentSession, error) {
		return nil, agentsErr
	}}

	if err := cmd.Run(ctx, nil); err != nil {
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

	cmd := &executionStatusCmd{agentsFunc: func() ([]agentSession, error) {
		return fakeSessions, nil
	}}

	if err := cmd.Run(ctx, nil); err != nil {
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

	// Verify pr field is empty when notes contain no PR URL.
	for _, id := range []string{"at-001", "at-002", "at-003", "at-004"} {
		if byID[id].PR != "" {
			t.Errorf("%s: expected empty pr, got %q", id, byID[id].PR)
		}
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

	bdClient := bd.NewClientWithExec("/fake/home", fakeListExec(issues))
	buf := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     bdClient,
		Stdout: buf,
		Stderr: &bytes.Buffer{},
	}
	cmd := &executionStatusCmd{agentsFunc: func() ([]agentSession, error) { return nil, nil }}

	if err := cmd.Run(ctx, nil); err != nil {
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

	bdClient := bd.NewClientWithExec("/fake/home", fakeListExec(issues))
	buf := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/fake/home",
		BD:     bdClient,
		Stdout: buf,
		Stderr: &bytes.Buffer{},
	}
	cmd := &executionStatusCmd{agentsFunc: func() ([]agentSession, error) { return nil, nil }}

	if err := cmd.Run(ctx, nil); err != nil {
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

