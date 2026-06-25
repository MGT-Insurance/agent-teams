package verbs

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// ── mail test helpers ─────────────────────────────────────────────────────────

// mailIssue constructs a message bd.Issue for mail tests.
func mailIssue(id, assignee, from, title string, labels []string, createdAt time.Time) bd.Issue {
	return bd.Issue{
		ID:        id,
		IssueType: "message",
		Assignee:  assignee,
		Notes:     "from: " + from,
		Title:     title,
		Labels:    labels,
		CreatedAt: createdAt.UTC().Format(time.RFC3339),
	}
}

// mailFakeListBD returns a fakeBD whose RunJSON yields the given issues.
func mailFakeListBD(issues []bd.Issue) *fakeBD {
	return &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			raw, _ := json.Marshal(issues)
			return json.Unmarshal(raw, dst)
		},
	}
}

// ── core-path tests ───────────────────────────────────────────────────────────

func TestMail_NilContext(t *testing.T) {
	c := &mailKong{Limit: 20}
	err := c.Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if !strings.Contains(err.Error(), "nil context") {
		t.Errorf("error should mention nil context; got: %v", err)
	}
}

func TestMail_EmptyList_PrintsNoMail(t *testing.T) {
	fbd := mailFakeListBD(nil)
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailKong{Limit: 20}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "no mail") {
		t.Errorf("expected 'no mail'; got: %q", stdout.String())
	}
}

func TestMail_TableNewestFirst(t *testing.T) {
	// 3 issues passed oldest-first so that the Go sort is exercised.
	oldest := mailIssue("at-m1", "init-a", "alice", "message from alice", nil,
		time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	middle := mailIssue("at-m2", "init-b", "bob", "message from bob", nil,
		time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC))
	newest := mailIssue("at-m3", "init-c", "carol", "message from carol", nil,
		time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC))

	fbd := mailFakeListBD([]bd.Issue{oldest, middle, newest})
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailKong{Limit: 20}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()

	// Header row must be present.
	for _, col := range []string{"ID", "TO", "FROM", "SUBJECT", "STATUS", "CREATED"} {
		if !strings.Contains(out, col) {
			t.Errorf("header column %q missing; got:\n%s", col, out)
		}
	}

	// Newest row (at-m3) must appear before oldest (at-m1).
	posNewest := strings.Index(out, "at-m3")
	posOldest := strings.Index(out, "at-m1")
	if posNewest == -1 || posOldest == -1 {
		t.Fatalf("expected all row IDs in output; got:\n%s", out)
	}
	if posNewest > posOldest {
		t.Errorf("expected newest (at-m3) before oldest (at-m1); got:\n%s", out)
	}
}

func TestMail_StatusDerivation(t *testing.T) {
	acked := mailIssue("at-m-ack", "init-a", "alice", "acked msg",
		[]string{"delivery:acked"}, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	readMsg := mailIssue("at-m-rd", "init-b", "bob", "read msg",
		[]string{"read"}, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	pending := mailIssue("at-m-pend", "init-c", "carol", "pending msg",
		[]string{"delivery:pending"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	fbd := mailFakeListBD([]bd.Issue{acked, readMsg, pending})
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailKong{Limit: 20}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{"acked", "read", "pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected status %q in output; got:\n%s", want, out)
		}
	}
}

func TestMail_StatusDerivation_PrefixAck(t *testing.T) {
	// delivery-acked-by:<token> prefix must trigger "acked" independently of
	// the literal "delivery:acked" label — this guards the HasPrefix branch in
	// mailStatus (mail.go ~line 78).
	msg := mailIssue("at-m-pax", "init-d", "dave", "prefix acked msg",
		[]string{"delivery-acked-by:init-x"}, time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))

	fbd := mailFakeListBD([]bd.Issue{msg})
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailKong{Limit: 20}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "acked") {
		t.Errorf("expected status 'acked' for delivery-acked-by: prefix label; got:\n%s", out)
	}
}

func TestMail_LimitCapsRows(t *testing.T) {
	var issues []bd.Issue
	for i := 0; i < 10; i++ {
		issues = append(issues, mailIssue(
			fmt.Sprintf("at-mc%d", i),
			"init-a", "alice", "a message", nil,
			time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
		))
	}

	fbd := mailFakeListBD(issues)
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailKong{Limit: 3}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := stdout.String()
	dataRows := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "at-mc") {
			dataRows++
		}
	}
	if dataRows != 3 {
		t.Errorf("expected 3 data rows (limit=3), got %d; output:\n%s", dataRows, out)
	}
}

func TestMail_ReadOnly(t *testing.T) {
	// Verify no write subcommands are invoked: only "list" RunJSON calls allowed.
	writeSubcmds := []string{"label", "close", "note", "update"}
	var runCalls [][]string

	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			issues := []bd.Issue{
				mailIssue("at-m1", "init-a", "alice", "hello", nil,
					time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			}
			raw, _ := json.Marshal(issues)
			return json.Unmarshal(raw, dst)
		},
		runFn: func(args ...string) (string, error) {
			runCalls = append(runCalls, args)
			return "", nil
		},
	}

	ctx, _, _ := makeCtx(fbd, t.TempDir())
	c := &mailKong{Limit: 20}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Run (non-JSON) must have zero calls (mail only uses RunJSON for list).
	if len(runCalls) != 0 {
		t.Errorf("expected no Run() calls (read-only), got %d: %v", len(runCalls), runCalls)
	}

	// Belt-and-suspenders: scan for any write subcommand in Run calls.
	for _, call := range runCalls {
		if len(call) == 0 {
			continue
		}
		for _, bad := range writeSubcmds {
			if call[0] == bad {
				t.Errorf("read-only violation: Run(%q) was called", call)
			}
		}
	}
}
