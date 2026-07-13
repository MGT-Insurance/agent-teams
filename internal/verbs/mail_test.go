package verbs

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
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
	c := &mailListKong{Limit: 20}
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
	c := &mailListKong{Limit: 20}
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
	c := &mailListKong{Limit: 20}
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
	c := &mailListKong{Limit: 20}
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
	c := &mailListKong{Limit: 20}
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
	c := &mailListKong{Limit: 3}
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

// ── --json tests ──────────────────────────────────────────────────────────────

func TestMail_JSON_FullLabels(t *testing.T) {
	msg := mailIssue("at-mj1", "init-a", "alice", "hello from alice",
		[]string{"delivery-acked-by:init-b", "delivery-acked-at:2026-01-05T00:00:00Z", "thread:th-1"},
		time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))
	msg.Description = "the message body"

	fbd := mailFakeListBD([]bd.Issue{msg})
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailListKong{Limit: 20, JSON: true}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var records []mailRecord
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatalf("expected valid JSON array; got %q: %v", stdout.String(), err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r.Body != "the message body" {
		t.Errorf("expected body == Description; got %q", r.Body)
	}
	if r.ReadBy == nil || *r.ReadBy != "init-b" {
		t.Errorf("expected readBy == %q, got %v", "init-b", r.ReadBy)
	}
	if r.ReadAt == nil || *r.ReadAt != "2026-01-05T00:00:00Z" {
		t.Errorf("expected readAt == %q, got %v", "2026-01-05T00:00:00Z", r.ReadAt)
	}
	if r.Thread == nil || *r.Thread != "th-1" {
		t.Errorf("expected thread == %q, got %v", "th-1", r.Thread)
	}
}

func TestMail_JSON_UnreadHasNullFields(t *testing.T) {
	msg := mailIssue("at-mj2", "init-a", "alice", "pending msg",
		[]string{"delivery:pending"}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	fbd := mailFakeListBD([]bd.Issue{msg})
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailListKong{Limit: 20, JSON: true}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var records []mailRecord
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatalf("expected valid JSON array; got %q: %v", stdout.String(), err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r.Status != "pending" {
		t.Errorf("expected status \"pending\", got %q", r.Status)
	}
	if r.ReadAt != nil || r.ReadBy != nil {
		t.Errorf("expected readAt/readBy nil for unread message; got readAt=%v readBy=%v", r.ReadAt, r.ReadBy)
	}
}

func TestMail_JSON_EmptyListEmitsBrackets(t *testing.T) {
	fbd := mailFakeListBD(nil)
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailListKong{Limit: 20, JSON: true}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := strings.TrimSpace(stdout.String())
	if got != "[]" {
		t.Errorf("expected \"[]\" for empty mailbox in --json mode; got %q", got)
	}
	if strings.Contains(stdout.String(), "no mail") {
		t.Errorf("must not print 'no mail' text in --json mode; got %q", stdout.String())
	}
}

func TestMail_JSON_ReadOnly(t *testing.T) {
	var runCalls [][]string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			issues := []bd.Issue{
				mailIssue("at-mj3", "init-a", "alice", "hello", nil,
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
	c := &mailListKong{Limit: 20, JSON: true}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runCalls) != 0 {
		t.Errorf("expected no Run() calls in --json mode (read-only), got %d: %v", len(runCalls), runCalls)
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
	c := &mailListKong{Limit: 20}
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

// TestMailList_QueryIncludesStatusAll guards the --status=all addition:
// without it, auto-close-on-read would empty the dashboard mail tab of all
// read history (bd list defaults to open-only).
func TestMailList_QueryIncludesStatusAll(t *testing.T) {
	var gotArgs []string
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			gotArgs = args
			return json.Unmarshal([]byte(`[]`), dst)
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	c := &mailListKong{Limit: 20}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range gotArgs {
		if a == "--status=all" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --status=all in query args; got %v", gotArgs)
	}
}

// TestMail_JSON_ClosedField guards the closed axis: it must track the bead
// lifecycle (bd status), independent of the pending/read/acked delivery
// status derived from labels.
func TestMail_JSON_ClosedField(t *testing.T) {
	closedMsg := mailIssue("at-mc-closed", "init-a", "alice", "closed msg", nil,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	closedMsg.Status = "closed"
	openMsg := mailIssue("at-mc-open", "init-a", "alice", "open msg", nil,
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	openMsg.Status = "open"

	fbd := mailFakeListBD([]bd.Issue{closedMsg, openMsg})
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailListKong{Limit: 20, JSON: true}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var records []mailRecord
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatalf("expected valid JSON array; got %q: %v", stdout.String(), err)
	}
	byID := map[string]mailRecord{}
	for _, r := range records {
		byID[r.ID] = r
	}
	if !byID["at-mc-closed"].Closed {
		t.Errorf("expected closed=true for closed bead; got %+v", byID["at-mc-closed"])
	}
	if byID["at-mc-open"].Closed {
		t.Errorf("expected closed=false for open bead; got %+v", byID["at-mc-open"])
	}
}

// ── mailCloseKong ─────────────────────────────────────────────────────────────

func TestMailClose_CallsBDClose(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			calls = append(calls, args)
			return "closed at-m1", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	c := &mailCloseKong{ID: "at-m1"}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"close", "at-m1"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Errorf("got calls %v, want [%v]", calls, want)
	}
	if !strings.Contains(stdout.String(), "closed at-m1") {
		t.Errorf("expected bd output echoed to stdout; got %q", stdout.String())
	}
}

func TestMailClose_NilContext(t *testing.T) {
	c := &mailCloseKong{ID: "at-m1"}
	if err := c.Run(nil); err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── mailPurgeKong ─────────────────────────────────────────────────────────────

func TestMailPurge_DefaultForcesDelete(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			calls = append(calls, args)
			return "purged 3", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	c := &mailPurgeKong{OlderThan: "7d"}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"purge", "--older-than", "7d", "--force"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Errorf("got calls %v, want [%v]", calls, want)
	}
}

func TestMailPurge_DryRunOmitsForce(t *testing.T) {
	var calls [][]string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			calls = append(calls, args)
			return "would purge 3", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	c := &mailPurgeKong{OlderThan: "7d", DryRun: true}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"purge", "--older-than", "7d", "--dry-run"}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Errorf("got calls %v, want [%v]; --dry-run must never include --force", calls, want)
	}
}

// ── unified `mail` parent verb (mail_register.go) ───────────────────────────

// TestRegisterMailKong_NestedSubcommandLimitFlag proves `ateam mail list
// --limit=N --json` parses as a nested kong subcommand (mailCmd has no Run
// of its own — RunNode must walk down to the List leaf) and that --limit
// reaches the leaf's field.
func TestRegisterMailKong_NestedSubcommandLimitFlag(t *testing.T) {
	var issues []bd.Issue
	for i := 0; i < 5; i++ {
		issues = append(issues, mailIssue(fmt.Sprintf("at-nest-%d", i), "init-a", "alice", "msg", nil,
			time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC)))
	}

	p, err := cli.NewParser(kong.Exit(func(int) {}))
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	RegisterMailKong(p)

	kctx, parseErr := p.Parse([]string{"mail", "list", "--limit=2", "--json"})
	if parseErr != nil {
		t.Fatalf("expected `ateam mail list --limit=2 --json` to parse as a nested subcommand; got: %v", parseErr)
	}

	fbd := mailFakeListBD(issues)
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	if err := kctx.Run(ctx); err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	var records []mailRecord
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatalf("expected valid JSON; got %q: %v", stdout.String(), err)
	}
	if len(records) != 2 {
		t.Errorf("expected --limit=2 to cap at 2 records (proves the flag reached the nested leaf); got %d", len(records))
	}
}

// TestMailListAliasKong_DeprecationNoteStderrOnly proves the hidden
// debug-mail alias warns on stderr and keeps stdout as clean JSON — the
// dashboard parses `ateam debug-mail --json` on stdout.
func TestMailListAliasKong_DeprecationNoteStderrOnly(t *testing.T) {
	fbd := mailFakeListBD(nil)
	ctx, stdout, stderr := makeCtx(fbd, t.TempDir())
	c := &mailListAliasKong{mailListKong: mailListKong{Limit: 20, JSON: true}}
	if err := c.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stderr.String(), "deprecated") {
		t.Errorf("expected deprecation note on stderr; got %q", stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got != "[]" {
		t.Errorf("expected clean JSON on stdout, no deprecation noise; got %q", stdout.String())
	}
}
