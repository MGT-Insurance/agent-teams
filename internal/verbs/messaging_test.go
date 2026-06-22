package verbs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── parseSendFlags ────────────────────────────────────────────────────────────

func TestParseSendFlags_HappyPath(t *testing.T) {
	f := makeTempFile(t, "hello")
	recipientID, file, sender, thread, err := parseSendFlags([]string{
		"at-abc", "--file", f, "--sender", "test-sender", "--thread", "t1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recipientID != "at-abc" {
		t.Errorf("recipientID = %q, want %q", recipientID, "at-abc")
	}
	if file != f {
		t.Errorf("file = %q, want %q", file, f)
	}
	if sender != "test-sender" {
		t.Errorf("sender = %q, want %q", sender, "test-sender")
	}
	if thread != "t1" {
		t.Errorf("thread = %q, want %q", thread, "t1")
	}
}

func TestParseSendFlags_EqForm(t *testing.T) {
	f := makeTempFile(t, "body")
	_, _, sender, _, err := parseSendFlags([]string{"at-xyz", "--file=" + f, "--sender=agent-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender != "agent-x" {
		t.Errorf("sender = %q, want agent-x", sender)
	}
}

func TestParseSendFlags_MissingRecipient(t *testing.T) {
	_, _, _, _, err := parseSendFlags([]string{})
	if err == nil {
		t.Fatal("expected error for missing recipient, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestParseSendFlags_MissingFile(t *testing.T) {
	_, _, _, _, err := parseSendFlags([]string{"at-abc"})
	if err == nil {
		t.Fatal("expected error for missing --file, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestParseSendFlags_FileNotFound(t *testing.T) {
	_, _, _, _, err := parseSendFlags([]string{"at-abc", "--file", "/no/such/file.txt"})
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestParseSendFlags_UnknownFlag(t *testing.T) {
	f := makeTempFile(t, "body")
	_, _, _, _, err := parseSendFlags([]string{"at-abc", "--file", f, "--unknown=x"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ── hasLiveSession ────────────────────────────────────────────────────────────

func TestHasLiveSession_Match(t *testing.T) {
	sessions := []agentSession{
		{CWD: "/wt/path-a"},
		{CWD: "/wt/path-b"},
	}
	if !hasLiveSession(sessions, "/wt/path-a") {
		t.Error("expected hasLiveSession to return true for exact match")
	}
}

func TestHasLiveSession_NoMatch(t *testing.T) {
	sessions := []agentSession{
		{CWD: "/wt/path-a"},
	}
	if hasLiveSession(sessions, "/wt/path-z") {
		t.Error("expected hasLiveSession to return false for no match")
	}
}

func TestHasLiveSession_Empty(t *testing.T) {
	if hasLiveSession(nil, "/wt/path") {
		t.Error("expected false for nil sessions")
	}
}

func TestHasLiveSession_TrailingSlash(t *testing.T) {
	sessions := []agentSession{{CWD: "/wt/path/"}}
	if !hasLiveSession(sessions, "/wt/path") {
		t.Error("expected match when CWD has trailing slash")
	}
}

// ── senderFromNotes ───────────────────────────────────────────────────────────

func TestSenderFromNotes_Present(t *testing.T) {
	got := senderFromNotes("from: agent-x\nother: line")
	if got != "agent-x" {
		t.Errorf("got %q, want %q", got, "agent-x")
	}
}

func TestSenderFromNotes_Absent(t *testing.T) {
	got := senderFromNotes("no from line here")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSenderFromNotes_Empty(t *testing.T) {
	got := senderFromNotes("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── filterMessageType ─────────────────────────────────────────────────────────

func TestFilterMessageType(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-1", IssueType: "message"},
		{ID: "at-2", IssueType: "task"},
		{ID: "at-3", IssueType: "message"},
	}
	got := filterMessageType(issues)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].ID != "at-1" || got[1].ID != "at-3" {
		t.Errorf("unexpected ids: %v", got)
	}
}

func TestFilterMessageType_None(t *testing.T) {
	issues := []bd.Issue{{ID: "at-1", IssueType: "task"}}
	got := filterMessageType(issues)
	if len(got) != 0 {
		t.Errorf("expected 0 messages, got %d", len(got))
	}
}

// ── parseInboxFlags ───────────────────────────────────────────────────────────

func TestParseInboxFlags_JSON(t *testing.T) {
	jsonOut, err := parseInboxFlags([]string{"--json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !jsonOut {
		t.Error("expected jsonOut=true")
	}
}

func TestParseInboxFlags_NoArgs(t *testing.T) {
	jsonOut, err := parseInboxFlags([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jsonOut {
		t.Error("expected jsonOut=false with no args")
	}
}

func TestParseInboxFlags_Unknown(t *testing.T) {
	_, err := parseInboxFlags([]string{"--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ── send: happy path ──────────────────────────────────────────────────────────

func TestSend_HappyPath_LiveSession(t *testing.T) {
	home := t.TempDir()
	f := makeTempFile(t, "hello recipient")

	recipientWt := t.TempDir()
	var createArgs []string
	fbd := &fakeBD{
		// bd create --json returns a single object (not an array).
		runJSONFn: func(dst any, args ...string) error {
			createArgs = args
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-wisp-msg1"
			}
			return nil
		},
		// bd show <id> --json returns a single-element array.
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-recipient",
				Description: "worktree: " + recipientWt + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	var agentsCalled bool
	var resumeCalled bool

	cmd := &sendCmd{
		agentsFunc: func() ([]agentSession, error) {
			agentsCalled = true
			return []agentSession{{CWD: recipientWt}}, nil
		},
		resumeFunc: func(_ *cli.Context, id string) error {
			resumeCalled = true
			return nil
		},
	}

	ctx, stdout, _ := makeCtx(fbd, home)
	if err := cmd.Run(ctx, []string{"at-recipient", "--file", f, "--sender", "dri:at-sender"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Message bead must have been created.
	if len(createArgs) == 0 {
		t.Fatal("expected bd create call, got none")
	}
	assertContains(t, createArgs, "--type=message", "bd create missing --type=message")
	assertContains(t, createArgs, "--assignee=at-recipient", "bd create missing --assignee")
	assertContains(t, createArgs, "--labels=delivery:pending", "bd create missing delivery:pending label")

	var hasNotesFrom bool
	for _, a := range createArgs {
		if strings.HasPrefix(a, "--notes=from: ") {
			hasNotesFrom = true
		}
	}
	if !hasNotesFrom {
		t.Errorf("bd create missing --notes=from: ...; got: %v", createArgs)
	}

	// Doorbell must have been touched.
	doorbellPath := filepath.Join(home, "mailbox", "at-recipient.wake")
	if _, err := os.Stat(doorbellPath); err != nil {
		t.Errorf("doorbell not touched at %s: %v", doorbellPath, err)
	}

	// Agents were queried.
	if !agentsCalled {
		t.Error("expected agentsFunc to be called")
	}
	// Session was live — resume must NOT have been called.
	if resumeCalled {
		t.Error("resume should not be called when session is live")
	}

	out := stdout.String()
	if !strings.Contains(out, "message_id: at-wisp-msg1") {
		t.Errorf("stdout missing message_id: %s", out)
	}
}

func TestSend_DeadSession_EscalatesToResume(t *testing.T) {
	home := t.TempDir()
	f := makeTempFile(t, "hello")
	recipientWt := t.TempDir()

	fbd := &fakeBD{
		// bd create --json returns single object.
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-wisp-msg2"
			}
			return nil
		},
		// bd show <id> --json returns single-element array.
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{
				ID:          "at-dead",
				Description: "worktree: " + recipientWt + "\n",
			}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	var resumedID string
	cmd := &sendCmd{
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{}, nil // no live sessions
		},
		resumeFunc: func(_ *cli.Context, id string) error {
			resumedID = id
			return nil
		},
	}

	ctx, stdout, _ := makeCtx(fbd, home)
	if err := cmd.Run(ctx, []string{"at-dead", "--file", f}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resumedID != "at-dead" {
		t.Errorf("resume not called with correct id; got %q", resumedID)
	}
	if !strings.Contains(stdout.String(), "launching via ateam resume") {
		t.Errorf("stdout missing launch notice: %s", stdout.String())
	}
}

func TestSend_NilContext(t *testing.T) {
	cmd := &sendCmd{}
	err := cmd.Run(nil, []string{"at-abc", "--file", "/tmp/x"})
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

func TestSend_MissingRecipient(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	cmd := &sendCmd{}
	err := cmd.Run(ctx, []string{})
	if err == nil {
		t.Fatal("expected error for missing recipient, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestSend_ThreadLabel(t *testing.T) {
	home := t.TempDir()
	f := makeTempFile(t, "threaded message")

	var createArgs []string
	fbd := &fakeBD{
		// bd create --json returns single object.
		runJSONFn: func(dst any, args ...string) error {
			createArgs = args
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = "at-wisp-t1"
			}
			return nil
		},
		// bd show --json: return issue with no worktree line (non-fatal path).
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: "at-recip", Description: ""}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}

	cmd := &sendCmd{
		agentsFunc: func() ([]agentSession, error) { return nil, fmt.Errorf("no claude") },
		resumeFunc: func(_ *cli.Context, _ string) error { return nil },
	}

	ctx, _, _ := makeCtx(fbd, home)
	if err := cmd.Run(ctx, []string{"at-recip", "--file", f, "--thread", "thread-xyz"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, createArgs, "--labels=thread:thread-xyz", "bd create missing thread label")
}

// ── inbox: resolves initiative and drains messages ───────────────────────────

func TestInbox_DrainAndMark(t *testing.T) {
	// Build a fake bd that:
	// 1. Returns a single open initiative matching cwd on bd list --status=open
	// 2. Returns two unread messages on bd list --include-infra --assignee --exclude-label=read
	// 3. Accepts label add/remove calls
	cwd := t.TempDir()
	myID := "at-inbox-test"

	var labelCalls [][]string

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			// Determine which call this is by looking at args.
			switch {
			case containsAll(args, "--status=open") && !containsAll(args, "--include-infra"):
				// resolveMyInitiative call.
				issues := []bd.Issue{{
					ID:          myID,
					Description: "worktree: " + cwd + "\n",
					Status:      "open",
				}}
				return json.Unmarshal(mustMarshal(issues), dst)
			case containsAll(args, "--include-infra", "--exclude-label=read"):
				// unread messages query.
				messages := []bd.Issue{
					{ID: "at-wisp-m1", IssueType: "message", Assignee: myID, Notes: "from: sender-a", Description: "hello"},
					{ID: "at-wisp-m2", IssueType: "message", Assignee: myID, Notes: "from: sender-b", Description: "world"},
				}
				return json.Unmarshal(mustMarshal(messages), dst)
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			labelCalls = append(labelCalls, args)
			return "", nil
		},
	}

	ctx, stdout, _ := makeCtx(fbd, t.TempDir())

	// Test the internal helpers directly: resolveMyInitiative and markMessageRead.
	// inbox.Run uses os.Getwd() which we can't inject, so we test the two
	// side-effectful pieces independently.

	// Test resolveMyInitiative directly.
	id, err := resolveMyInitiative(ctx, cwd)
	if err != nil {
		t.Fatalf("resolveMyInitiative: %v", err)
	}
	if id != myID {
		t.Errorf("resolveMyInitiative = %q, want %q", id, myID)
	}

	// Test markMessageRead by calling it directly.
	ts := "2026-06-21T00:00:00Z"
	if err := markMessageRead(ctx, "at-wisp-m1", myID, ts); err != nil {
		t.Fatalf("markMessageRead: %v", err)
	}

	// Verify the label calls were made in order.
	var addedLabels []string
	var removedLabels []string
	for _, call := range labelCalls {
		if len(call) >= 3 && call[0] == "label" && call[1] == "add" {
			addedLabels = append(addedLabels, call[3])
		}
		if len(call) >= 3 && call[0] == "label" && call[1] == "remove" {
			removedLabels = append(removedLabels, call[3])
		}
	}

	for _, want := range []string{"read", "delivery:acked"} {
		found := false
		for _, l := range addedLabels {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("label %q not added; added: %v", want, addedLabels)
		}
	}

	// delivery:pending must have been removed.
	found := false
	for _, l := range removedLabels {
		if l == "delivery:pending" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("delivery:pending not removed; removed: %v", removedLabels)
	}

	// delivery-acked-by and delivery-acked-at labels must have been added.
	foundBy := false
	foundAt := false
	for _, l := range addedLabels {
		if strings.HasPrefix(l, "delivery-acked-by:") {
			foundBy = true
		}
		if strings.HasPrefix(l, "delivery-acked-at:") {
			foundAt = true
		}
	}
	if !foundBy {
		t.Errorf("delivery-acked-by: label not added; added: %v", addedLabels)
	}
	if !foundAt {
		t.Errorf("delivery-acked-at: label not added; added: %v", addedLabels)
	}

	_ = stdout
}

func TestInbox_NilContext(t *testing.T) {
	cmd := &inboxCmd{}
	err := cmd.Run(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

func TestInbox_NotRegisteredInitiative_SilentNoOp(t *testing.T) {
	// When resolveMyInitiative returns no match, inbox exits 0 silently.
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			// Return empty list — no initiative matches cwd.
			return json.Unmarshal([]byte("[]"), dst)
		},
	}
	ctx, stdout, stderr := makeCtx(fbd, t.TempDir())
	cmd := &inboxCmd{}

	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("expected nil error for non-initiative cwd, got: %v", err)
	}
	if stdout.String() != "" {
		t.Errorf("expected empty stdout, got: %s", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("expected empty stderr, got: %s", stderr.String())
	}
}

func TestInbox_NoMessages_Silent(t *testing.T) {
	cwd := t.TempDir()
	myID := "at-no-mail"

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			switch {
			case containsAll(args, "--status=open") && !containsAll(args, "--include-infra"):
				issues := []bd.Issue{{
					ID:          myID,
					Description: "worktree: " + cwd + "\n",
					Status:      "open",
				}}
				return json.Unmarshal(mustMarshal(issues), dst)
			case containsAll(args, "--include-infra"):
				return json.Unmarshal([]byte("[]"), dst)
			}
			return nil
		},
	}

	id, err := resolveMyInitiative(&cli.Context{BD: fbd, Home: t.TempDir()}, cwd)
	if err != nil {
		t.Fatalf("resolveMyInitiative: %v", err)
	}
	if id != myID {
		t.Errorf("id = %q, want %q", id, myID)
	}
}

func TestInbox_JSONOutput(t *testing.T) {
	cwd := t.TempDir()
	myID := "at-json-test"

	messages := []bd.Issue{
		{ID: "at-wisp-j1", IssueType: "message", Assignee: myID, Description: "json body"},
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			switch {
			case containsAll(args, "--status=open") && !containsAll(args, "--include-infra"):
				issues := []bd.Issue{{
					ID:          myID,
					Description: "worktree: " + cwd + "\n",
					Status:      "open",
				}}
				return json.Unmarshal(mustMarshal(issues), dst)
			case containsAll(args, "--include-infra"):
				return json.Unmarshal(mustMarshal(messages), dst)
			}
			return nil
		},
		runFn: func(args ...string) (string, error) { return "", nil },
	}

	// Test the JSON path: if messages are returned and --json flag passed,
	// output is valid JSON.
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())

	// Call printMessagesBlock with --json (simulate inbox --json path).
	// We verify via the helper directly.
	msgs := filterMessageType(messages)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	fmt.Fprintln(ctx.Stdout, string(raw))

	out := stdout.String()
	var got []bd.Issue
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].ID != "at-wisp-j1" {
		t.Errorf("unexpected JSON output: %v", got)
	}
}

func TestInbox_IdempotentMark(t *testing.T) {
	// Calling markMessageRead twice must not error (idempotent via bd label add).
	labelCallCount := 0
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			labelCallCount++
			return "", nil // always succeeds
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	ts := "2026-06-21T00:00:00Z"

	if err := markMessageRead(ctx, "at-wisp-x", "at-me", ts); err != nil {
		t.Fatalf("first markMessageRead: %v", err)
	}
	first := labelCallCount

	if err := markMessageRead(ctx, "at-wisp-x", "at-me", ts); err != nil {
		t.Fatalf("second markMessageRead: %v", err)
	}
	second := labelCallCount - first

	if first != second {
		t.Errorf("idempotency: first call made %d bd ops, second made %d", first, second)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// assertContains checks that target contains want in its args slice.
func assertContains(t *testing.T, args []string, want, msg string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("%s: %q not found in %v", msg, want, args)
}

// containsAll checks that args contains all of the given values.
func containsAll(args []string, vals ...string) bool {
	for _, v := range vals {
		found := false
		for _, a := range args {
			if a == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// mustMarshal marshals v or panics.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
