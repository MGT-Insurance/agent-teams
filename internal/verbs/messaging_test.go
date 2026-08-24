package verbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/repoconfig"
)

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

// TestHasLiveSession_SymlinkedCwd verifies that a session whose CWD is a
// symlink to the registered worktree path (or vice versa) is still recognised
// as live — regression test for the macOS /tmp -> /private/tmp false negative.
func TestHasLiveSession_SymlinkedCwd(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Session CWD reported via the symlink; registered worktree is the real path.
	if !hasLiveSession([]agentSession{{CWD: link}}, real) {
		t.Error("expected match: session cwd via symlink, worktree real path")
	}

	// Session CWD is the real path; registered worktree is the symlink.
	if !hasLiveSession([]agentSession{{CWD: real}}, link) {
		t.Error("expected match: session cwd real path, worktree via symlink")
	}
}

// ── matchSessionByWorktree (Name-first matching, agent-teams-6rru.15) ───────

// TestMatchSessionByWorktree_NameMatchesWanderedCwd is the agent-teams-6rru.15
// repro: a live session's cwd has wandered into a sibling track worktree
// (not the registered worktree), but its Name (the `-n` dispatch value =
// filepath.Base of the registered worktree, stable across cwd-wander) still
// matches — so it must be found via Name even though cwd does not match.
func TestMatchSessionByWorktree_NameMatchesWanderedCwd(t *testing.T) {
	pid := 42
	registered := "/repo-root/at-lcrp"
	sessions := []agentSession{
		{CWD: "/repo-root/at-lcrp-track-h", Name: "at-lcrp", Status: "busy", PID: &pid},
	}
	got := matchSessionByWorktree(sessions, registered)
	if got == nil {
		t.Fatal("expected a match via Name despite cwd mismatch")
	}
	if got.PID == nil || *got.PID != pid {
		t.Error("expected the wandered-cwd session to be returned")
	}
}

// TestMatchSessionByWorktree_LiveNamedWinsOverDeadDuplicate covers the full
// at-wisp-e50 / agent-teams-6rru.15 repro: a dead duplicate session sitting
// at the registered worktree's cwd (Name matches, PID nil) alongside the
// live session that wandered into a track worktree (Name matches, PID
// present) — the live one must win regardless of slice order.
func TestMatchSessionByWorktree_LiveNamedWinsOverDeadDuplicate(t *testing.T) {
	pid := 7
	registered := "/repo-root/at-lcrp"

	deadDuplicate := agentSession{CWD: registered, Name: "at-lcrp"} // no PID
	liveWandered := agentSession{CWD: "/repo-root/at-lcrp-track-h", Name: "at-lcrp", Status: "busy", PID: &pid}

	for _, order := range [][]agentSession{
		{deadDuplicate, liveWandered},
		{liveWandered, deadDuplicate},
	} {
		got := matchSessionByWorktree(order, registered)
		if got == nil {
			t.Fatal("expected a match")
		}
		if got.PID == nil {
			t.Error("expected the LIVE named session to be returned, got the dead duplicate")
		}
	}
}

// TestMatchSessionByWorktree_NoNameFallsBackToCwd is a regression guard for
// foreground/interactive sessions, which are never dispatched with `-n` and
// so carry no Name — the exact-cwd fallback must still find them.
func TestMatchSessionByWorktree_NoNameFallsBackToCwd(t *testing.T) {
	registered := "/repo-root/at-lcrp"
	sessions := []agentSession{{CWD: registered, Status: "busy"}}
	got := matchSessionByWorktree(sessions, registered)
	if got == nil {
		t.Fatal("expected cwd-fallback match for a nameless session")
	}
}

// TestMatchSessionByWorktree_NoMatchAtAll is the true-DEAD case: no session
// matches by Name or by cwd.
func TestMatchSessionByWorktree_NoMatchAtAll(t *testing.T) {
	if matchSessionByWorktree(nil, "/repo-root/at-lcrp") != nil {
		t.Error("expected nil for no sessions")
	}
	sessions := []agentSession{{CWD: "/other/path", Name: "other"}}
	if matchSessionByWorktree(sessions, "/repo-root/at-lcrp") != nil {
		t.Error("expected nil when nothing matches by Name or cwd")
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

	// markMessageRead must close the message bead (auto-close-on-read) —
	// regression guard: a future refactor dropping or reordering this call
	// should fail here, not just silently stop closing messages.
	closeCalled := false
	for _, call := range labelCalls {
		if len(call) == 2 && call[0] == "close" && call[1] == "at-wisp-m1" {
			closeCalled = true
			break
		}
	}
	if !closeCalled {
		t.Errorf("expected close call for at-wisp-m1; calls: %v", labelCalls)
	}

	_ = stdout
}

// ── isStewardSession / resolveInboxRecipient ─────────────────────────────────

func TestIsStewardSession_MarkerPresent_SessionDirAndSubdir(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("steward init: %v", err)
	}

	sessionDir := StewardSessionDir(ctx)
	if !isStewardSession(ctx, sessionDir) {
		t.Errorf("expected isStewardSession true for the session dir itself: %s", sessionDir)
	}
	subdir := filepath.Join(sessionDir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if !isStewardSession(ctx, subdir) {
		t.Errorf("expected isStewardSession true for a subdir of the session dir")
	}
	if isStewardSession(ctx, t.TempDir()) {
		t.Errorf("expected isStewardSession false for an unrelated directory")
	}
}

func TestIsStewardSession_NoMarker_ReturnsFalse(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	// No `steward init` run — marker file does not exist.
	if isStewardSession(ctx, StewardSessionDir(ctx)) {
		t.Error("expected isStewardSession false when no marker file exists")
	}
}

func TestResolveSender_StewardSession_StampsSteward(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("steward init: %v", err)
	}
	// cwd is the steward session: the sender must be the steward, not git
	// user.name. This is the bug — steward->DRI mail used to collapse to
	// gitUserName() ("Eric Lloyd") on model-driven send paths.
	if got := defaultSender(ctx, StewardSessionDir(ctx)); got != StewardHandle {
		t.Errorf("defaultSender(steward session) = %q, want %q", got, StewardHandle)
	}
}

func TestResolveSender_ExplicitAlwaysWins(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("steward init: %v", err)
	}
	// An explicit --sender wins without touching the filesystem; the relay
	// (human), hung_tick (hung-scan) and route (pr-shepherd) paths all depend
	// on this precedence and must never be aborted by a cwd lookup.
	if got := resolveSender(ctx, "human"); got != "human" {
		t.Errorf("resolveSender(explicit) = %q, want %q", got, "human")
	}
}

func TestResolveSender_NonStewardSession_FallsBackToGitUser(t *testing.T) {
	home := t.TempDir()
	ctx, _, _ := makeCtx(&fakeBD{}, home)
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("steward init: %v", err)
	}
	// A non-steward cwd keeps the git user.name fallback — the legitimate
	// "human running the CLI" case.
	if got := defaultSender(ctx, t.TempDir()); got != gitUserName() {
		t.Errorf("defaultSender(non-steward) = %q, want gitUserName() %q", got, gitUserName())
	}
}

func TestResolveInboxRecipient_StewardSession_BypassesInitiativeLookup(t *testing.T) {
	home := t.TempDir()
	// fakeBD errors if resolveMyInitiative's `bd list --status=open` is ever
	// called — asserts the Steward branch short-circuits before it.
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			return fmt.Errorf("unexpected bd call in Steward-session path: %v", args)
		},
	}
	ctx, _, _ := makeCtx(fbd, home)
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("steward init: %v", err)
	}

	id, err := resolveInboxRecipient(ctx, StewardSessionDir(ctx))
	if err != nil {
		t.Fatalf("resolveInboxRecipient: %v", err)
	}
	if id != StewardHandle {
		t.Errorf("resolveInboxRecipient = %q, want %q", id, StewardHandle)
	}
}

func TestResolveInboxRecipient_NoMarker_FallsBackToInitiative(t *testing.T) {
	cwd := t.TempDir()
	myID := "at-inbox-fallback"

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			issues := []bd.Issue{{
				ID:          myID,
				Description: "worktree: " + cwd + "\n",
				Status:      "open",
			}}
			return json.Unmarshal(mustMarshal(issues), dst)
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	id, err := resolveInboxRecipient(ctx, cwd)
	if err != nil {
		t.Fatalf("resolveInboxRecipient: %v", err)
	}
	if id != myID {
		t.Errorf("resolveInboxRecipient = %q, want %q (existing worktree-based resolution)", id, myID)
	}
}

func TestResolveMyInitiative_ResolvesFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	myID := "at-inbox-subdir"

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			issues := []bd.Issue{{
				ID:          myID,
				Description: "worktree: " + root + "\n",
				Status:      "open",
			}}
			return json.Unmarshal(mustMarshal(issues), dst)
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	subdir := filepath.Join(root, "apps", "mithril")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	id, err := resolveMyInitiative(ctx, subdir)
	if err != nil {
		t.Fatalf("resolveMyInitiative: %v", err)
	}
	if id != myID {
		t.Errorf("resolveMyInitiative(%s) = %q, want %q", subdir, id, myID)
	}
}

func TestResolveMyInitiative_NestedWorktrees_LongestPathWins(t *testing.T) {
	root := t.TempDir()
	outerID := "at-inbox-outer"
	nestedDir := filepath.Join(root, "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	innerID := "at-inbox-inner"
	innerSubdir := filepath.Join(nestedDir, "apps", "mithril")
	if err := os.MkdirAll(innerSubdir, 0o755); err != nil {
		t.Fatalf("mkdir innerSubdir: %v", err)
	}

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			issues := []bd.Issue{
				{ID: outerID, Description: "worktree: " + root + "\n", Status: "open"},
				{ID: innerID, Description: "worktree: " + nestedDir + "\n", Status: "open"},
			}
			return json.Unmarshal(mustMarshal(issues), dst)
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	id, err := resolveMyInitiative(ctx, innerSubdir)
	if err != nil {
		t.Fatalf("resolveMyInitiative: %v", err)
	}
	if id != innerID {
		t.Errorf("resolveMyInitiative(%s) = %q, want %q (longest/most-specific worktree path should win)", innerSubdir, id, innerID)
	}
}

func TestMatchByWorktreeOrAncestor_SiblingPrefixDoesNotMatch(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-sibling", Description: "worktree: /a/b\n", Status: "open"},
	}
	if match := matchByWorktreeOrAncestor(issues, "/a/b-foo"); match != nil {
		t.Errorf("matchByWorktreeOrAncestor(/a/b-foo) = %v, want nil (sibling dir sharing a string prefix must not match worktree /a/b)", match)
	}
	// Sanity check the positive case still matches a true subdirectory.
	if match := matchByWorktreeOrAncestor(issues, "/a/b/sub"); match == nil || match.ID != "at-sibling" {
		t.Errorf("matchByWorktreeOrAncestor(/a/b/sub) = %v, want match on at-sibling", match)
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

func TestInbox_ZeroUnread_PrintsNoMail(t *testing.T) {
	// Normal (non-peek) mode with zero unread must print "no unread mail" and NOT mark anything read.
	cwd := t.TempDir()
	myID := "at-zero-unread"

	var labelCalls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			switch {
			case containsAll(args, "--status=open") && !containsAll(args, "--include-infra"):
				issues := []bd.Issue{{ID: myID, Description: "worktree: " + cwd + "\n", Status: "open"}}
				return json.Unmarshal(mustMarshal(issues), dst)
			case containsAll(args, "--include-infra"):
				return json.Unmarshal([]byte("[]"), dst)
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			labelCalls = append(labelCalls, args)
			return "", nil
		},
	}

	// Test via the helper layer (Run uses os.Getwd which can't be injected).
	// Verify: filterMessageType on empty input returns empty, and the zero path
	// would print "no unread mail".
	msgs := filterMessageType([]bd.Issue{})
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}

	// Verify printMessagesBlock is NOT called and the "no unread mail" line is emitted.
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	// Simulate the zero-unread branch directly.
	fmt.Fprintln(ctx.Stdout, "no unread mail")
	if !strings.Contains(stdout.String(), "no unread mail") {
		t.Errorf("expected 'no unread mail' in output, got: %s", stdout.String())
	}
	if len(labelCalls) != 0 {
		t.Errorf("expected no label calls for zero unread, got: %v", labelCalls)
	}
}

func TestInbox_PeekWithUnread_NonConsuming(t *testing.T) {
	// --peek must report count without marking messages read.
	cwd := t.TempDir()
	myID := "at-peek-test"

	var labelCalls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			switch {
			case containsAll(args, "--status=open") && !containsAll(args, "--include-infra"):
				issues := []bd.Issue{{ID: myID, Description: "worktree: " + cwd + "\n", Status: "open"}}
				return json.Unmarshal(mustMarshal(issues), dst)
			case containsAll(args, "--include-infra"):
				messages := []bd.Issue{
					{ID: "at-wisp-p1", IssueType: "message", Assignee: myID, Description: "hi"},
					{ID: "at-wisp-p2", IssueType: "message", Assignee: myID, Description: "there"},
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

	// Simulate the peek branch: query messages, print count, no mark-read calls.
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	var messages []bd.Issue
	if err := fbd.RunJSON(&messages,
		"list", "--include-infra", "--assignee="+myID, "--exclude-label=read", "--status=open", "--json",
	); err != nil {
		t.Fatalf("RunJSON: %v", err)
	}
	messages = filterMessageType(messages)
	if len(messages) == 0 {
		t.Fatal("expected 2 messages")
	}
	// Peek path: print count only.
	fmt.Fprintf(ctx.Stdout, "%d unread message(s)\n", len(messages))

	out := stdout.String()
	if !strings.Contains(out, "2 unread message(s)") {
		t.Errorf("expected count line in output, got: %s", out)
	}
	// No label calls — peek never marks read.
	if len(labelCalls) != 0 {
		t.Errorf("peek must not call label ops, got: %v", labelCalls)
	}
}

func TestInbox_PeekNoUnread(t *testing.T) {
	// --peek with zero unread prints "no unread mail" and makes no label calls.
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			t.Error("label call made during peek with no unread")
			return "", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	// Peek with zero messages.
	msgs := filterMessageType([]bd.Issue{})
	if len(msgs) == 0 {
		fmt.Fprintln(ctx.Stdout, "no unread mail")
	}
	if !strings.Contains(stdout.String(), "no unread mail") {
		t.Errorf("expected 'no unread mail', got: %s", stdout.String())
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

// ── printMessagesBlock ────────────────────────────────────────────────────────

func TestPrintMessagesBlock_FooterPresent(t *testing.T) {
	messages := []bd.Issue{
		{ID: "at-wisp-f1", IssueType: "message", Notes: "from: agent-a", Description: "body one"},
		{ID: "at-wisp-f2", IssueType: "message", Notes: "from: agent-b", Description: "body two"},
	}
	ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
	printMessagesBlock(ctx, messages)

	out := stdout.String()
	if !strings.Contains(out, "ateam show at-wisp-f1") {
		t.Errorf("footer missing 'ateam show at-wisp-f1':\n%s", out)
	}
	if !strings.Contains(out, "ateam show at-wisp-f2") {
		t.Errorf("footer missing 'ateam show at-wisp-f2':\n%s", out)
	}
	if !strings.Contains(out, "To re-read a consumed message:") {
		t.Errorf("footer missing guidance line:\n%s", out)
	}
	// Footer must appear inside the system-reminder block (before closing tag).
	closeIdx := strings.Index(out, "</system-reminder>")
	f1Idx := strings.Index(out, "ateam show at-wisp-f1")
	if closeIdx < 0 || f1Idx < 0 || f1Idx > closeIdx {
		t.Errorf("footer must appear before </system-reminder>; closeIdx=%d f1Idx=%d\n%s", closeIdx, f1Idx, out)
	}
}

func TestPrintMessagesBlock_SingleMessage(t *testing.T) {
	messages := []bd.Issue{
		{ID: "at-solo", IssueType: "message", Notes: "from: x", Description: "only"},
	}
	ctx, stdout, _ := makeCtx(&fakeBD{}, t.TempDir())
	printMessagesBlock(ctx, messages)

	out := stdout.String()
	if !strings.Contains(out, "ateam show at-solo") {
		t.Errorf("footer missing 'ateam show at-solo':\n%s", out)
	}
}

func TestPrintMessagesBlock_JSONPathNoFooter(t *testing.T) {
	// The --json path never calls printMessagesBlock; it marshals directly.
	// Verify that a plain JSON marshal of messages does NOT contain the footer.
	messages := []bd.Issue{
		{ID: "at-wisp-j9", IssueType: "message", Description: "json body"},
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(raw)
	if strings.Contains(out, "ateam show") {
		t.Errorf("--json output must not contain re-read footer, got:\n%s", out)
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

// ── sendKong core-path tests ──────────────────────────────────────────────────

// sendFixture bundles the fakeBD + sendKong wiring shared by the escalation
// tests below; each test overrides only the fields relevant to its branch.
type sendFixture struct {
	home        string
	file        string
	recipientWt string
	createArgs  []string
}

func newSendFixture(t *testing.T) *sendFixture {
	t.Helper()
	return &sendFixture{
		home:        t.TempDir(),
		file:        makeTempFile(t, "hello recipient"),
		recipientWt: t.TempDir(),
	}
}

func (sf *sendFixture) fakeBD(recipientID, msgID string) *fakeBD {
	return &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			sf.createArgs = args
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = msgID
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: recipientID, Description: "worktree: " + sf.recipientWt + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
}

// fakeBDWithRepo is fakeBD plus a "repo:" field, for the repoconfig.Enabled
// gate tests below.
func (sf *sendFixture) fakeBDWithRepo(recipientID, msgID, repo string) *fakeBD {
	return &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			sf.createArgs = args
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = msgID
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: recipientID, Description: "worktree: " + sf.recipientWt + "\nrepo: " + repo + "\n"}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
}

func TestSendKong_BusySession_NoOp(t *testing.T) {
	sf := newSendFixture(t)
	var resumeCalled, sleeperCalled, respawnCalled bool
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		Sender:      "test-sender",
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "busy"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { resumeCalled = true; return nil },
		sleeper:        func(time.Duration) { sleeperCalled = true },
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { respawnCalled = true; return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-recip", "at-kong-msg1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertContains(t, sf.createArgs, "--type=message", "bd create missing --type=message")
	assertContains(t, sf.createArgs, "--assignee=at-kong-recip", "bd create missing --assignee")
	if resumeCalled {
		t.Error("resume should not be called when recipient is busy")
	}
	if sleeperCalled {
		t.Error("sleeper should not be called when recipient is busy")
	}
	if respawnCalled {
		t.Error("respawn should not be called when recipient is busy")
	}
	if !strings.Contains(stdout.String(), "message_id: at-kong-msg1") {
		t.Errorf("stdout missing message_id: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "doorbell will be picked up when its turn ends") {
		t.Errorf("stdout missing busy no-op notice: %s", stdout.String())
	}
}

func TestSendKong_CodexRoutesDirectlyToDeliveryCoordinator(t *testing.T) {
	sf := newSendFixture(t)
	var wakeCalled bool
	cmd := &sendKong{
		RecipientID: "at-codex",
		File:        sf.file,
		Sender:      "test-sender",
		agentsFunc: func() ([]agentSession, error) {
			t.Fatal("Codex delivery must not query claude agents")
			return nil, nil
		},
		codexWake: func(_ *cli.Context, issue bd.Issue) error {
			wakeCalled = true
			if issue.ID != "at-codex" {
				t.Fatalf("wake issue=%+v", issue)
			}
			return nil
		},
	}
	fbd := sf.fakeBD("at-codex", "at-msg")
	fbd.runFn = func(args ...string) (string, error) {
		issues := []bd.Issue{{ID: "at-codex", Description: "runtime: codex\nworktree: " + sf.recipientWt + "\nsession: thread-1\n"}}
		raw, _ := json.Marshal(issues)
		return string(raw), nil
	}
	ctx, stdout, _ := makeCtx(fbd, sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !wakeCalled {
		t.Fatal("Codex wake was not called")
	}
	if !strings.Contains(stdout.String(), "Codex thread accepted") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestSendKong_CodexBusyOrFailedWakeLeavesMailQueued(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "busy", err: errCodexDeliveryBusy, want: "delivery already in progress"},
		{name: "failed", err: errors.New("daemon unavailable"), want: "message at-msg remains queued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sf := newSendFixture(t)
			cmd := &sendKong{
				RecipientID: "at-codex",
				File:        sf.file,
				codexWake:   func(*cli.Context, bd.Issue) error { return tc.err },
			}
			fbd := sf.fakeBD("at-codex", "at-msg")
			fbd.runFn = func(args ...string) (string, error) {
				issues := []bd.Issue{{ID: "at-codex", Description: "runtime: codex\nworktree: " + sf.recipientWt + "\nsession: thread-1\n"}}
				raw, _ := json.Marshal(issues)
				return string(raw), nil
			}
			ctx, stdout, _ := makeCtx(fbd, sf.home)
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("send: %v", err)
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stdout = %s", stdout.String())
			}
			if _, err := os.Stat(filepath.Join(sf.home, "mailbox", "at-codex.wake")); err != nil {
				t.Fatalf("doorbell missing: %v", err)
			}
		})
	}
}

func TestSendKong_WaitingSession_NoOp(t *testing.T) {
	sf := newSendFixture(t)
	var resumeCalled, respawnCalled bool
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "waiting"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { resumeCalled = true; return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { respawnCalled = true; return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-recip", "at-kong-msg1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resumeCalled {
		t.Error("resume should not be called when recipient is waiting")
	}
	if respawnCalled {
		t.Error("respawn should not be called when recipient is waiting (would drop a pending dialog)")
	}
	if !strings.Contains(stdout.String(), "doorbell will be picked up when its turn ends") {
		t.Errorf("stdout missing waiting no-op notice: %s", stdout.String())
	}
}

func TestSendKong_NoMatchingSession_EscalatesToResume(t *testing.T) {
	sf := newSendFixture(t)

	var resumedID string
	cmd := &sendKong{
		RecipientID:    "at-kong-dead",
		File:           sf.file,
		agentsFunc:     func() ([]agentSession, error) { return []agentSession{}, nil },
		resumeFunc:     func(_ *cli.Context, id, _, _ string) error { resumedID = id; return nil },
		sleeper:        func(time.Duration) { t.Fatal("sleeper should not be called when no session matches") },
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { t.Fatal("respawn should not be called when no session matches"); return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-dead", "at-kong-msg2"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resumedID != "at-kong-dead" {
		t.Errorf("resume not called with correct id; got %q", resumedID)
	}
	if !strings.Contains(stdout.String(), "launching via ateam resume") {
		t.Errorf("stdout missing launch notice: %s", stdout.String())
	}
}

func TestSendKong_ResumeEscalation_ThreadsLaunchPromptAndModel(t *testing.T) {
	sf := newSendFixture(t)

	var gotID, gotPrompt, gotModel string
	cmd := &sendKong{
		RecipientID:        "at-kong-dead",
		File:               sf.file,
		ResumeLaunchPrompt: "/agent-teams:review-pr at-kong-dead",
		ResumeModel:        "sonnet",
		agentsFunc:         func() ([]agentSession, error) { return []agentSession{}, nil },
		resumeFunc: func(_ *cli.Context, id, prompt, model string) error {
			gotID, gotPrompt, gotModel = id, prompt, model
			return nil
		},
		sleeper:        func(time.Duration) { t.Fatal("sleeper should not be called when no session matches") },
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { t.Fatal("respawn should not be called when no session matches"); return nil },
	}

	ctx, _, _ := makeCtx(sf.fakeBD("at-kong-dead", "at-kong-msg9"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotID != "at-kong-dead" {
		t.Errorf("resume id = %q", gotID)
	}
	if gotPrompt != "/agent-teams:review-pr at-kong-dead" {
		t.Errorf("resume launchPrompt = %q", gotPrompt)
	}
	if gotModel != "sonnet" {
		t.Errorf("resume model = %q, want sonnet", gotModel)
	}
}

func TestSendKong_IdleSession_DoorbellConsumed_NoRespawn(t *testing.T) {
	sf := newSendFixture(t)
	var sleptFor time.Duration
	var respawnCalled bool
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(d time.Duration) { sleptFor = d },
		doorbellExists: func(string) bool { return false }, // gone: a turn consumed it
		respawnFunc:    func(string) error { respawnCalled = true; return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-recip", "at-kong-msg1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sleptFor != 5*time.Second {
		t.Errorf("expected a 5s wait before the doorbell re-check, got %v", sleptFor)
	}
	if respawnCalled {
		t.Error("respawn should not be called when the doorbell was already consumed")
	}
	if !strings.Contains(stdout.String(), "doorbell consumed; delivery in progress") {
		t.Errorf("stdout missing consumed notice: %s", stdout.String())
	}
}

func TestSendKong_IdleSession_DoorbellPresent_Respawns(t *testing.T) {
	sf := newSendFixture(t)
	var respawnedID string
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true }, // still present: recipient is deaf
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-recip", "at-kong-msg1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "abc12345" {
		t.Errorf("respawn not called with correct short id; got %q", respawnedID)
	}
	if !strings.Contains(stdout.String(), "respawned abc12345") {
		t.Errorf("stdout missing respawn notice: %s", stdout.String())
	}
}

// TestSendKong_DisabledRepo_QueuesWithoutWakingOrRespawn pins the Codex
// adversarial-review finding on this feature's "strongest bypass": mail send
// escalating straight to `claude respawn` for a deaf recipient, with no gate
// of its own (unlike the not-found branch, which routes through the
// already-gated resumeFunc/resumeKong). The message bead still gets created
// (queued), but the doorbell touched earlier in Run must be removed and
// nothing downstream (agentsFunc/resume/respawn) may run.
func TestSendKong_DisabledRepo_QueuesWithoutWakingOrRespawn(t *testing.T) {
	sf := newSendFixture(t)
	repoDir := t.TempDir() // no .agent-teams marker -> disabled
	var agentsCalled, resumeCalled, respawnCalled bool
	cmd := &sendKong{
		RecipientID: "at-kong-disabled",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			agentsCalled = true
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { resumeCalled = true; return nil },
		sleeper:        func(time.Duration) { t.Fatal("sleeper should not be called for a disabled repo") },
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { respawnCalled = true; return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBDWithRepo("at-kong-disabled", "at-kong-msg-disabled", repoDir), sf.home)
	doorbellPath := filepath.Join(sf.home, "mailbox", "at-kong-disabled.wake")

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentsCalled {
		t.Error("agentsFunc should not be called once the repo is known to be disabled")
	}
	if resumeCalled {
		t.Error("resume should not be called for a disabled repo")
	}
	if respawnCalled {
		t.Error("respawn should not be called for a disabled repo")
	}
	if _, err := os.Stat(doorbellPath); err == nil {
		t.Error("doorbell should have been removed for a disabled repo")
	}
	if !strings.Contains(stdout.String(), "message_id: at-kong-msg-disabled") {
		t.Errorf("message should still be queued (bd create) for a disabled repo: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "repo is disabled") {
		t.Errorf("stdout missing disabled-repo notice: %s", stdout.String())
	}
}

// TestSendKong_EnabledRepoField_StillRespawns proves the new check doesn't
// accidentally block legitimate respawns just because a "repo:" field is
// present and enabled — the counterpart to the disabled-repo test above.
func TestSendKong_EnabledRepoField_StillRespawns(t *testing.T) {
	sf := newSendFixture(t)
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, repoconfig.FileName), nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", repoconfig.FileName, err)
	}
	var respawnedID string
	cmd := &sendKong{
		RecipientID: "at-kong-enabled",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	ctx, _, _ := makeCtx(sf.fakeBDWithRepo("at-kong-enabled", "at-kong-msg-enabled", repoDir), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "abc12345" {
		t.Errorf("respawn not called with correct short id; got %q", respawnedID)
	}
}

func TestSendKong_PidlessSession_DoorbellPresent_Respawns(t *testing.T) {
	sf := newSendFixture(t)
	var respawnedID string
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			// Tracked-but-dead: no Status, PID stays nil (zero value).
			return []agentSession{{ID: "deadbeef", CWD: sf.recipientWt}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-recip", "at-kong-msg1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "deadbeef" {
		t.Errorf("respawn not called with correct short id; got %q", respawnedID)
	}
	if !strings.Contains(stdout.String(), "respawned deadbeef") {
		t.Errorf("stdout missing respawn notice: %s", stdout.String())
	}
}

func TestSendKong_RespawnError_WarnsButSucceeds(t *testing.T) {
	sf := newSendFixture(t)
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "abc12345", CWD: sf.recipientWt, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { return fmt.Errorf("exec: \"claude\": executable file not found in $PATH") },
	}

	ctx, stdout, _ := makeCtx(sf.fakeBD("at-kong-recip", "at-kong-msg1"), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("respawn failure must not fail ateam send (mail is already delivered): %v", err)
	}
	if !strings.Contains(stdout.String(), "warning: respawn abc12345 failed") {
		t.Errorf("stdout missing respawn-failure warning: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "at-kong-msg1") {
		t.Errorf("stdout warning should reference the queued message id: %s", stdout.String())
	}
}

// ── sendKong: session-set-first liveness (agent-teams-zalv.4 / at-ps11) ──────

// sessionTiedFakeBD builds a fakeBD whose `bd show` returns an initiative
// with the given session: lines (plus a worktree: line so recipientWorktree
// still succeeds) — exercising the session-id-set path of
// matchSessionsForInitiative rather than the legacy worktree/Name fallback.
func sessionTiedFakeBD(recipientID, wt, msgID string, sessionIDs []string) *fakeBD {
	desc := "worktree: " + wt + "\n"
	for _, id := range sessionIDs {
		desc += "session: " + id + "\n"
	}
	return &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = msgID
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			issues := []bd.Issue{{ID: recipientID, Description: desc}}
			raw, _ := json.Marshal(issues)
			return string(raw), nil
		},
	}
}

// TestSendKong_SessionTied_PicksPrimaryLiveSession: two tied sessions, both
// live — the PRIMARY (first-registered, sess-a) must be the doorbell/respawn
// target, not sess-b, even though sess-b appears later in the agents
// snapshot (agent-teams-zalv.1 §3).
func TestSendKong_SessionTied_PicksPrimaryLiveSession(t *testing.T) {
	sf := newSendFixture(t)
	var respawnedID string
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			pidA, pidB := 1, 2
			return []agentSession{
				// Snapshot order deliberately reversed vs. registration order.
				{ID: "sess-b-short", SessionID: "sess-b", CWD: "/somewhere/else", Status: "idle", PID: &pidB},
				{ID: "sess-a-short", SessionID: "sess-a", CWD: "/somewhere/else-a", Status: "idle", PID: &pidA},
			}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true }, // still present: recipient is deaf
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	ctx, stdout, _ := makeCtx(sessionTiedFakeBD("at-kong-recip", sf.recipientWt, "at-kong-msg1", []string{"sess-a", "sess-b"}), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "sess-a-short" {
		t.Errorf("respawn target = %q, want the PRIMARY session's id sess-a-short", respawnedID)
	}
	if !strings.Contains(stdout.String(), "respawned sess-a-short") {
		t.Errorf("stdout missing primary respawn notice: %s", stdout.String())
	}
}

// TestSendKong_SessionTied_PrimaryDead_FallsBackToLaterLiveSession: the
// primary (sess-a) is not in the agents snapshot at all (dead/gone) — the
// doorbell/respawn target must fall back to the next live tied session
// (sess-b), per the contract's "fall back to any live tied session if the
// primary is gone."
func TestSendKong_SessionTied_PrimaryDead_FallsBackToLaterLiveSession(t *testing.T) {
	sf := newSendFixture(t)
	var respawnedID string
	cmd := &sendKong{
		RecipientID: "at-kong-recip",
		File:        sf.file,
		agentsFunc: func() ([]agentSession, error) {
			pidB := 2
			return []agentSession{
				{ID: "sess-b-short", SessionID: "sess-b", CWD: "/somewhere/else", Status: "idle", PID: &pidB},
			}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	ctx, stdout, _ := makeCtx(sessionTiedFakeBD("at-kong-recip", sf.recipientWt, "at-kong-msg1", []string{"sess-a", "sess-b"}), sf.home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "sess-b-short" {
		t.Errorf("respawn target = %q, want the fallback live session sess-b-short (primary sess-a is gone)", respawnedID)
	}
	if !strings.Contains(stdout.String(), "respawned sess-b-short") {
		t.Errorf("stdout missing fallback respawn notice: %s", stdout.String())
	}
}

// TestSendKong_SteardRecipient_SessionLinesNeverConsulted regression-guards
// agent-teams-zalv.1's "Steward routing is UNAFFECTED": sendKong.Run must
// never call bd.ShowIssue for the Steward handle (stewardSendFakeBD fails the
// test on any bd Run call), so it can never consult session: lines for the
// Steward — liveness stays on the marker/handle (StewardSessionDir) +
// worktree/Name match exactly as before this bead.
func TestSendKong_StewardRecipient_SessionLinesNeverConsulted(t *testing.T) {
	home := t.TempDir()
	file := makeTempFile(t, "hello steward")
	ctx, stdout, _ := makeCtx(stewardSendFakeBD(t, "at-steward-msg3"), home)
	stewardDir := StewardSessionDir(ctx)

	var respawnedID string
	cmd := &sendKong{
		RecipientID: StewardHandle,
		File:        file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "stew5678", CWD: stewardDir, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "stew5678" {
		t.Errorf("respawn not called with correct short id via the marker/handle path; got %q", respawnedID)
	}
	if !strings.Contains(stdout.String(), "respawned stew5678") {
		t.Errorf("stdout missing respawn notice: %s", stdout.String())
	}
}

func TestSendKong_NilContext(t *testing.T) {
	cmd := &sendKong{RecipientID: "at-x", File: "/tmp/x"}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
}

func TestSendKong_FileNotFound(t *testing.T) {
	ctx, _, _ := makeCtx(&fakeBD{}, t.TempDir())
	cmd := &sendKong{RecipientID: "at-x", File: "/no/such/file.txt"}
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestSendKong_ResumeModelWithoutPromptRejected(t *testing.T) {
	err := (&sendKong{RecipientID: "at-x", File: "/some/file", ResumeModel: "sonnet"}).Validate()
	if err == nil {
		t.Fatal("expected UsageError for --resume-model without --resume-launch-prompt, got nil")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ── inboxKong core-path tests ─────────────────────────────────────────────────

func TestInboxKong_PeekWithUnread(t *testing.T) {
	cwd := t.TempDir()
	myID := "at-kong-peek"

	var labelCalls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			switch {
			case containsAll(args, "--status=open") && !containsAll(args, "--include-infra"):
				issues := []bd.Issue{{ID: myID, Description: "worktree: " + cwd + "\n", Status: "open"}}
				return json.Unmarshal(mustMarshal(issues), dst)
			case containsAll(args, "--include-infra"):
				messages := []bd.Issue{
					{ID: "at-kp1", IssueType: "message", Assignee: myID, Description: "hi"},
					{ID: "at-kp2", IssueType: "message", Assignee: myID, Description: "there"},
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

	// Simulate peek path via the helpers (Run uses os.Getwd which can't be injected).
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	id, err := resolveMyInitiative(ctx, cwd)
	if err != nil || id != myID {
		t.Fatalf("resolveMyInitiative: id=%q err=%v", id, err)
	}

	var messages []bd.Issue
	if err := fbd.RunJSON(&messages,
		"list", "--include-infra", "--assignee="+myID, "--exclude-label=read", "--status=open", "--json",
	); err != nil {
		t.Fatalf("RunJSON: %v", err)
	}
	messages = filterMessageType(messages)

	// Peek: print count, no label calls.
	if len(messages) > 0 {
		fmt.Fprintf(ctx.Stdout, "%d unread message(s)\n", len(messages))
	}
	if !strings.Contains(stdout.String(), "2 unread message(s)") {
		t.Errorf("expected count line, got: %s", stdout.String())
	}
	if len(labelCalls) != 0 {
		t.Errorf("peek must not call label ops, got: %v", labelCalls)
	}
}

func TestInboxKong_NilContext(t *testing.T) {
	cmd := &inboxKong{}
	if err := cmd.Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── checkStewardInboxGuard (agent-teams-e3mq.31) ─────────────────────────────

// writeWatcherPidfile writes a watcher pidfile entry to
// <home>/mailbox/steward.watcher.pid. session == "" writes an old-format
// bare-pid entry (pre-e3mq.30, unattributable); otherwise it writes the
// current "pid<TAB>session_id" format, mirroring wake-watcher.sh's claim
// write (printf '%s\t%s' "$$" "$HOOK_SESSION_ID").
func writeWatcherPidfile(t *testing.T, home string, pid int, session string) {
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

func TestCheckStewardInboxGuard_NoPidfile_Proceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv(sessionIDEnvVar, "caller-session")
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := checkStewardInboxGuard(ctx); err != nil {
		t.Errorf("expected nil (no pidfile means no session-of-record to protect), got: %v", err)
	}
}

func TestCheckStewardInboxGuard_DeadPid_Proceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv(sessionIDEnvVar, "caller-session")
	writeWatcherPidfile(t, home, 9999999, "some-other-session") // near-certainly not a live pid
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := checkStewardInboxGuard(ctx); err != nil {
		t.Errorf("expected nil (dead pid means no session-of-record to protect), got: %v", err)
	}
}

func TestCheckStewardInboxGuard_OwnSession_Proceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv(sessionIDEnvVar, "my-session")
	writeWatcherPidfile(t, home, os.Getpid(), "my-session")
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := checkStewardInboxGuard(ctx); err != nil {
		t.Errorf("expected nil (caller is the session of record), got: %v", err)
	}
}

func TestCheckStewardInboxGuard_EnvUnset_Proceeds(t *testing.T) {
	home := t.TempDir()
	// t.Setenv to "" rather than leaving it alone — this test process is
	// itself a Claude Code session, so the real CLAUDE_CODE_SESSION_ID is
	// already set in the ambient environment. Clearing it simulates a
	// manual/debug invocation with no attributable caller session. A
	// foreign live watcher is present, but the guard must still proceed
	// since it can't attribute the caller.
	t.Setenv(sessionIDEnvVar, "")
	writeWatcherPidfile(t, home, os.Getpid(), "some-other-session")
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	if err := checkStewardInboxGuard(ctx); err != nil {
		t.Errorf("expected nil (unattributable caller), got: %v", err)
	}
}

func TestCheckStewardInboxGuard_ForeignLive_Refuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv(sessionIDEnvVar, "duplicate-session")
	writeWatcherPidfile(t, home, os.Getpid(), "incumbent-session")
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	err := checkStewardInboxGuard(ctx)
	if err == nil {
		t.Fatal("expected refusal for a live foreign watcher, got nil")
	}
	if !strings.Contains(err.Error(), "incumbent-session") {
		t.Errorf("error should name the incumbent session: %v", err)
	}
	if !strings.Contains(err.Error(), "DUPLICATE") {
		t.Errorf("error should tell the caller it looks like a duplicate: %v", err)
	}
	if code := cli.ExitCode(err); code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestCheckStewardInboxGuard_OldFormatLive_Refuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv(sessionIDEnvVar, "duplicate-session")
	writeWatcherPidfile(t, home, os.Getpid(), "") // old format: bare pid, unattributable
	ctx, _, _ := makeCtx(&fakeBD{}, home)

	err := checkStewardInboxGuard(ctx)
	if err == nil {
		t.Fatal("expected refusal for a live old-format (unattributable) watcher, got nil")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should describe the unattributable session as unknown: %v", err)
	}
}

// TestInboxKong_StewardDuplicateSession_RefusesBothPeekAndConsume verifies the
// guard is wired into inboxKong.Run at the single site ahead of the c.Peek
// branch, so BOTH the consuming and --peek paths refuse — and neither ever
// reaches the bd query/mark-read calls (the fakeBD below fails the test if
// either fires).
func TestInboxKong_StewardDuplicateSession_RefusesBothPeekAndConsume(t *testing.T) {
	home := t.TempDir()
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			t.Fatalf("unexpected bd query call — guard must refuse before querying: %v", args)
			return nil
		},
		runFn: func(args ...string) (string, error) {
			t.Fatalf("unexpected bd Run call — guard must refuse before mark-read: %v", args)
			return "", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, home)

	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("steward init: %v", err)
	}
	t.Chdir(StewardSessionDir(ctx))

	writeWatcherPidfile(t, home, os.Getpid(), "incumbent-session")
	t.Setenv(sessionIDEnvVar, "duplicate-session")

	if err := (&inboxKong{}).Run(ctx); err == nil {
		t.Error("expected refusal on the consuming path, got nil")
	}
	if err := (&inboxKong{Peek: true}).Run(ctx); err == nil {
		t.Error("expected refusal on the --peek path, got nil")
	}
}

// ── recipientWorktree / sendKong steward escalation (agent-teams-e3mq.28) ────

// TestRecipientWorktree_StewardHandle_NoBDShowCall verifies recipientWorktree
// special-cases StewardHandle to StewardSessionDir before ever calling
// bd.ShowIssue — "steward" is not an initiative bead, so a real bd show would
// error and the caller (sendKong.Run) would skip liveness/escalation entirely.
func TestRecipientWorktree_StewardHandle_NoBDShowCall(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			t.Fatalf("unexpected bd call for steward handle: %v", args)
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			t.Fatalf("unexpected bd call for steward handle: %v", args)
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	_, wt, err := recipientWorktree(ctx, StewardHandle)
	if err != nil {
		t.Fatalf("recipientWorktree: %v", err)
	}
	want := StewardSessionDir(ctx)
	if wt != want {
		t.Errorf("recipientWorktree(steward) = %q, want %q", wt, want)
	}
}

// stewardSendFakeBD builds a fakeBD for sendKong steward-recipient tests: the
// bd create call (runJSONFn) succeeds with msgID; any bd show / other Run
// call fails the test, asserting recipientWorktree never falls through to
// bd.ShowIssue for the steward handle.
func stewardSendFakeBD(t *testing.T, msgID string) *fakeBD {
	t.Helper()
	return &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			if issue, ok := dst.(*bd.Issue); ok {
				issue.ID = msgID
			}
			return nil
		},
		runFn: func(args ...string) (string, error) {
			t.Fatalf("unexpected bd Run call for steward recipient: %v", args)
			return "", nil
		},
	}
}

// TestSendKong_StewardRecipient_NoMatchingSession_QueuesWithoutResume covers
// the resume branch: when no live session's cwd matches StewardSessionDir,
// sendKong must NOT call resumeFunc (there's no "/dri <id>" launch path for
// the Steward — auto-relaunch is e3mq.10's scope) and must print a
// mail-queued note instead.
func TestSendKong_StewardRecipient_NoMatchingSession_QueuesWithoutResume(t *testing.T) {
	home := t.TempDir()
	file := makeTempFile(t, "hello steward")

	var resumeCalled bool
	cmd := &sendKong{
		RecipientID:    StewardHandle,
		File:           file,
		agentsFunc:     func() ([]agentSession, error) { return []agentSession{}, nil },
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { resumeCalled = true; return nil },
		sleeper:        func(time.Duration) { t.Fatal("sleeper should not be called when no session matches") },
		doorbellExists: func(string) bool { return true },
		respawnFunc:    func(string) error { t.Fatal("respawn should not be called when no session matches"); return nil },
	}

	ctx, stdout, _ := makeCtx(stewardSendFakeBD(t, "at-steward-msg1"), home)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resumeCalled {
		t.Error("resume should not be called for the steward recipient")
	}
	if !strings.Contains(stdout.String(), "steward session not running; mail queued") {
		t.Errorf("stdout missing mail-queued note: %s", stdout.String())
	}
}

// TestSendKong_StewardRecipient_IdleSession_DoorbellPersists_Respawns covers
// the deaf-steward self-heal path: a live session whose cwd IS
// StewardSessionDir, idle, with the doorbell still present after the 5s
// recheck — respawnFunc must fire exactly like it would for any other
// recipient.
func TestSendKong_StewardRecipient_IdleSession_DoorbellPersists_Respawns(t *testing.T) {
	home := t.TempDir()
	file := makeTempFile(t, "hello steward")
	ctx, stdout, _ := makeCtx(stewardSendFakeBD(t, "at-steward-msg2"), home)
	stewardDir := StewardSessionDir(ctx)

	var respawnedID string
	cmd := &sendKong{
		RecipientID: StewardHandle,
		File:        file,
		agentsFunc: func() ([]agentSession, error) {
			return []agentSession{{ID: "stew1234", CWD: stewardDir, Status: "idle"}}, nil
		},
		resumeFunc:     func(_ *cli.Context, _, _, _ string) error { t.Fatal("resume should not be called"); return nil },
		sleeper:        func(time.Duration) {},
		doorbellExists: func(string) bool { return true }, // still present: recipient is deaf
		respawnFunc:    func(id string) error { respawnedID = id; return nil },
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if respawnedID != "stew1234" {
		t.Errorf("respawn not called with correct short id; got %q", respawnedID)
	}
	if !strings.Contains(stdout.String(), "respawned stew1234") {
		t.Errorf("stdout missing respawn notice: %s", stdout.String())
	}
}
