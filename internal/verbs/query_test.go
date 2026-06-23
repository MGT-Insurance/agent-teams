package verbs_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

// newCtx builds a cli.Context backed by a fake bd.Client that responds to
// subcommand calls via the provided map: key is the first arg passed to bd
// (the subcommand), value is the stdout bytes the fake returns.
func newCtx(t *testing.T, home string, responses map[string][]byte) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		if name != "bd" {
			t.Errorf("exec called with %q, want bd", name)
			return nil, nil, errors.New("unexpected binary")
		}
		// args is [-C, home, subcommand, ...]
		if len(args) < 3 {
			t.Errorf("expected at least 3 args, got %v", args)
			return nil, nil, errors.New("too few args")
		}
		sub := args[2] // subcommand after -C <home>
		resp, ok := responses[sub]
		if !ok {
			t.Errorf("unexpected subcommand %q (full args: %v)", sub, args)
			return nil, nil, errors.New("unexpected subcommand")
		}
		return resp, nil, nil
	}
	client := bd.NewClientWithExec(home, execFn)
	ctx := &cli.Context{
		Home:   home,
		BD:     client,
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	return ctx, out
}

// captureArgs returns an ExecFunc that records every call's args slice.
func captureArgs(calls *[][]string) bd.ExecFunc {
	return func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		*calls = append(*calls, cp)
		return []byte("result\n"), nil, nil
	}
}

// ── ws ────────────────────────────────────────────────────────────────────────

func TestWsPrintsHome(t *testing.T) {
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, ok := reg.Lookup("ws")
	if !ok {
		t.Fatal("ws not registered")
	}

	out := &bytes.Buffer{}
	ctx := &cli.Context{
		Home:   "/my/workspace",
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("ws.Run: %v", err)
	}
	if got := out.String(); got != "/my/workspace\n" {
		t.Errorf("ws output = %q, want %q", got, "/my/workspace\n")
	}
}

func TestWsNilCtxReturnsError(t *testing.T) {
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("ws")
	if err := cmd.Run(nil, nil); err == nil {
		t.Error("expected error for nil ctx, got nil")
	}
}

// ── list ──────────────────────────────────────────────────────────────────────

func TestListCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "list", "--status=open"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestListWritesOutput(t *testing.T) {
	ctx, out := newCtx(t, "/ws", map[string][]byte{
		"list": []byte("● issue-1 · My Issue   [● P1 · OPEN]\n"),
	})
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("list produced no output")
	}
}

// ── list-json ─────────────────────────────────────────────────────────────────

func TestListJSONCallsBDArgs(t *testing.T) {
	var calls [][]string
	issues := []bd.Issue{{ID: "at-abc", Title: "T", Status: "open", CreatedAt: "2026-06-01"}}
	raw, _ := json.Marshal(issues)
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return append(raw, '\n'), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list-json")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "list", "--status=open", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestListJSONEmitsValidJSON(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-x1", Title: "Init", Status: "open", CreatedAt: "2026-06-01"},
		{ID: "at-x2", Title: "Impl", Status: "open", CreatedAt: "2026-06-02"},
	}
	raw, _ := json.Marshal(issues)
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return append(raw, '\n'), nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("list-json")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("list-json.Run: %v", err)
	}

	var parsed []bd.Issue
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if len(parsed) != 2 {
		t.Errorf("parsed %d issues, want 2", len(parsed))
	}
}

// ── human-list ────────────────────────────────────────────────────────────────

func TestHumanListCallsBDArgs(t *testing.T) {
	var calls [][]string
	// captureArgs returns "result\n" which is not valid JSON; use a JSON stub instead.
	emptyJSON := []byte("[]\n")
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return emptyJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "human", "list", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

// newHumanListCtx builds a cli.Context whose bd fake returns the given issues
// as JSON for any "human" subcommand.
func newHumanListCtx(t *testing.T, issues []bd.Issue) (*cli.Context, *bytes.Buffer) {
	t.Helper()
	raw, err := json.Marshal(issues)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	raw = append(raw, '\n')
	out := &bytes.Buffer{}
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return raw, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	ctx := &cli.Context{
		Home:   "/ws",
		BD:     client,
		Stdout: out,
		Stderr: &bytes.Buffer{},
	}
	return ctx, out
}

func TestHumanListReviewGate(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-r1", Title: "Ship feature", Labels: []string{"human", "gate:review"}, Notes: "PR https://github.com/org/repo/pull/42 ready for review"},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[REVIEW]") {
		t.Errorf("expected [REVIEW] in output, got: %q", got)
	}
	if !strings.Contains(got, "at-r1") {
		t.Errorf("expected id at-r1 in output, got: %q", got)
	}
	if !strings.Contains(got, "PR https://github.com/org/repo/pull/42 ready for review") {
		t.Errorf("expected note text in output, got: %q", got)
	}
}

func TestHumanListQuestionGate(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-q1", Title: "Which approach?", Labels: []string{"human", "gate:question"}, Notes: "Should we use approach A or B?"},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[QUESTION]") {
		t.Errorf("expected [QUESTION] in output, got: %q", got)
	}
	if !strings.Contains(got, "at-q1") {
		t.Errorf("expected id at-q1 in output, got: %q", got)
	}
}

func TestHumanListBackwardCompatHumanOnly(t *testing.T) {
	// Pre-existing gated bead: only "human" label, no gate:* — must render as QUESTION.
	issues := []bd.Issue{
		{ID: "at-old1", Title: "Old gate bead", Labels: []string{"human"}, Notes: "Legacy question"},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[QUESTION]") {
		t.Errorf("expected [QUESTION] for backward-compat human-only bead, got: %q", got)
	}
	if strings.Contains(got, "[REVIEW]") {
		t.Errorf("backward-compat bead must not render as [REVIEW], got: %q", got)
	}
}

func TestHumanListEmptyNoteOmitsNoteLine(t *testing.T) {
	issues := []bd.Issue{
		{ID: "at-notnote", Title: "No note bead", Labels: []string{"human", "gate:review"}, Notes: ""},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// Should be exactly one line: the id/kind/title line.
	if len(lines) != 1 {
		t.Errorf("expected 1 line for bead with no note, got %d: %q", len(lines), got)
	}
	if !strings.Contains(got, "[REVIEW]") {
		t.Errorf("expected [REVIEW] in output, got: %q", got)
	}
}

// ── human-list: structured ask extraction ────────────────────────────────────

func TestHumanListStructuredAskRendered(t *testing.T) {
	// Notes contain a sentinel block — structured fields must appear, not raw Notes.
	notes := "<<<ateam-ask\ndecision: Use approach A\nrecommendation: A\nalternative: B\ncontext: Faster path\n>>>"
	issues := []bd.Issue{
		{ID: "at-s1", Title: "Pick approach", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "decision: Use approach A") {
		t.Errorf("expected decision field, got: %q", got)
	}
	if !strings.Contains(got, "recommendation: A") {
		t.Errorf("expected recommendation field, got: %q", got)
	}
	if !strings.Contains(got, "alternative: B") {
		t.Errorf("expected alternative field, got: %q", got)
	}
	if !strings.Contains(got, "context: Faster path") {
		t.Errorf("expected context field, got: %q", got)
	}
	// Raw Notes blob must NOT appear verbatim (sentinel markers must not leak).
	if strings.Contains(got, "<<<ateam-ask") {
		t.Errorf("sentinel open marker must not appear in output, got: %q", got)
	}
	if strings.Contains(got, ">>>") {
		t.Errorf("sentinel close marker must not appear in output, got: %q", got)
	}
}

func TestHumanListStructuredAskLastBlockWins(t *testing.T) {
	// Multiple blocks — only the last one should be rendered.
	notes := "<<<ateam-ask\ndecision: Old decision\nrecommendation: old-rec\nalternative: old-alt\n>>>\nsome notes\n<<<ateam-ask\ndecision: New decision\nrecommendation: new-rec\nalternative: new-alt\ncontext: updated\n>>>"
	issues := []bd.Issue{
		{ID: "at-s2", Title: "Multi-block", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "decision: New decision") {
		t.Errorf("expected last block's decision, got: %q", got)
	}
	if strings.Contains(got, "Old decision") {
		t.Errorf("earlier block must not appear, got: %q", got)
	}
	if !strings.Contains(got, "context: updated") {
		t.Errorf("expected last block's context, got: %q", got)
	}
}

func TestHumanListNoStructuredBlockFallsBackToRawNotes(t *testing.T) {
	// No sentinel block — raw Notes must appear unchanged.
	rawNote := "PR https://github.com/org/repo/pull/42 ready for review"
	issues := []bd.Issue{
		{ID: "at-s3", Title: "Review PR", Labels: []string{"human", "gate:review"}, Notes: rawNote},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, rawNote) {
		t.Errorf("expected raw note text %q in output, got: %q", rawNote, got)
	}
}

func TestHumanListMalformedBlockFallsBackToRawNotes(t *testing.T) {
	// Block with missing closing sentinel — treated as no block, raw fallback.
	notes := "some preamble\n<<<ateam-ask\ndecision: incomplete block\n"
	issues := []bd.Issue{
		{ID: "at-s4", Title: "Malformed", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	// Raw Notes should appear (fallback), not structured rendering.
	if !strings.Contains(got, notes) {
		t.Errorf("expected raw notes fallback for malformed block, got: %q", got)
	}
}

func TestHumanListStructuredAskContextOmittedWhenEmpty(t *testing.T) {
	// Block without context field — context line must not appear in output.
	notes := "<<<ateam-ask\ndecision: Ship it\nrecommendation: yes\nalternative: wait\n>>>"
	issues := []bd.Issue{
		{ID: "at-s5", Title: "Ship decision", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "context:") {
		t.Errorf("context line must be omitted when empty, got: %q", got)
	}
	if !strings.Contains(got, "decision: Ship it") {
		t.Errorf("expected decision field, got: %q", got)
	}
}

// ── human-list: lastNoteBlock fallback ───────────────────────────────────────

func TestHumanListFallbackMultiBlockShowsOnlyLast(t *testing.T) {
	// Multi-block notes: only the last block should appear; earlier blocks absent.
	notes := "First entry: old context\n\nSecond entry: more history\n\nThird entry: current status"
	issues := []bd.Issue{
		{ID: "at-mb1", Title: "Multi-block bead", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "Third entry: current status") {
		t.Errorf("expected last block in output, got: %q", got)
	}
	if strings.Contains(got, "First entry: old context") {
		t.Errorf("earlier block must not appear, got: %q", got)
	}
	if strings.Contains(got, "Second entry: more history") {
		t.Errorf("earlier block must not appear, got: %q", got)
	}
}

func TestHumanListFallbackLongBlockTruncated(t *testing.T) {
	// A last block longer than 10 lines: truncation indicator must appear,
	// and only the last 10 lines of the block are shown.
	var linesBuf strings.Builder
	for i := 1; i <= 15; i++ {
		fmt.Fprintf(&linesBuf, "line %d\n", i)
	}
	notes := strings.TrimRight(linesBuf.String(), "\n")
	issues := []bd.Issue{
		{ID: "at-long1", Title: "Long note bead", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "…older lines truncated") {
		t.Errorf("expected truncation indicator, got: %q", got)
	}
	// Last 10 lines (6-15) should appear.
	if !strings.Contains(got, "line 15") {
		t.Errorf("expected last line in output, got: %q", got)
	}
	if !strings.Contains(got, "line 6") {
		t.Errorf("expected line 6 (10th from end) in output, got: %q", got)
	}
	// First 5 lines should NOT appear.
	if strings.Contains(got, "line 1\n") {
		t.Errorf("truncated lines must not appear, got: %q", got)
	}
	if strings.Contains(got, "line 5\n") {
		t.Errorf("truncated lines must not appear, got: %q", got)
	}
}

func TestHumanListFallbackSingleBlockNoTruncation(t *testing.T) {
	// Single-block notes (no blank lines) rendered as-is when short enough.
	notes := "PR https://github.com/org/repo/pull/99 needs approval"
	issues := []bd.Issue{
		{ID: "at-sb1", Title: "Single block", Labels: []string{"human", "gate:review"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, notes) {
		t.Errorf("expected note text in output, got: %q", got)
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("must not show truncation indicator for short note, got: %q", got)
	}
}

func TestHumanListStructuredAskPathUnchanged(t *testing.T) {
	// Structured ask present: renderAsk is used, lastNoteBlock fallback not taken.
	// The raw sentinel markers must not appear, and structured fields must.
	notes := "preamble note\n\n<<<ateam-ask\ndecision: Use X\nrecommendation: X is faster\nalternative: Y\ncontext: benchmarked\n>>>"
	issues := []bd.Issue{
		{ID: "at-struct1", Title: "Structured ask", Labels: []string{"human", "gate:question"}, Notes: notes},
	}
	ctx, out := newHumanListCtx(t, issues)

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("human-list")
	if err := cmd.Run(ctx, nil); err != nil {
		t.Fatalf("human-list.Run: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "decision: Use X") {
		t.Errorf("expected structured decision field, got: %q", got)
	}
	if !strings.Contains(got, "context: benchmarked") {
		t.Errorf("expected structured context field, got: %q", got)
	}
	// Fallback must not be taken — sentinel markers must not leak.
	if strings.Contains(got, "<<<ateam-ask") {
		t.Errorf("sentinel open must not appear in output, got: %q", got)
	}
	// The preamble note block must not appear (fallback not taken).
	if strings.Contains(got, "preamble note") {
		t.Errorf("preamble must not appear when structured ask is present, got: %q", got)
	}
}

// ── show ──────────────────────────────────────────────────────────────────────

func TestShowMissingIDReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("show")

	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d (err: %v)", cli.ExitCode(err), err)
	}
}

func TestShowEmptyIDReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("show")

	err := cmd.Run(ctx, []string{""})
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d", cli.ExitCode(err))
	}
}

func TestShowCallsBDArgs(t *testing.T) {
	var calls [][]string
	client := bd.NewClientWithExec("/ws", captureArgs(&calls))
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("show")
	if err := cmd.Run(ctx, []string{"at-abc123"}); err != nil {
		t.Fatalf("show.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "show", "at-abc123"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

// ── learnings ─────────────────────────────────────────────────────────────────

func TestLearningsMissingRoleReturnsUsageError(t *testing.T) {
	ctx, _ := newCtx(t, "/ws", nil)
	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("learnings")

	err := cmd.Run(ctx, nil)
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	if cli.ExitCode(err) != 2 {
		t.Errorf("expected exit code 2, got %d (err: %v)", cli.ExitCode(err), err)
	}
}

func TestLearningsCallsBDArgs(t *testing.T) {
	var calls [][]string
	// The new implementation calls `memories --json`, not `memories <role>`.
	// captureArgs returns "result\n" which is not valid JSON; use a JSON stub.
	emptyJSON := []byte("{}\n")
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		cp := make([]string, len(args))
		copy(cp, args)
		calls = append(calls, cp)
		return emptyJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("learnings")
	if err := cmd.Run(ctx, []string{"planner"}); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(calls))
	}
	wantArgs := []string{"-C", "/ws", "memories", "--json"}
	for i, w := range wantArgs {
		if i >= len(calls[0]) || calls[0][i] != w {
			t.Errorf("bd args[%d] = %q, want %q (full: %v)", i, calls[0][i], w, calls[0])
		}
	}
}

func TestLearningsWritesOutput(t *testing.T) {
	// The new implementation filters by role prefix and prints full bodies.
	memoriesJSON := []byte(`{"planner:foo":"first line\nsecond line","dri:bar":"should not appear"}` + "\n")
	execFn := func(_ string, _ ...string) ([]byte, []byte, error) {
		return memoriesJSON, nil, nil
	}
	client := bd.NewClientWithExec("/ws", execFn)
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: "/ws", BD: client, Stdout: out, Stderr: &bytes.Buffer{}}

	reg := make(cli.Registry)
	verbs.RegisterQuery(reg)
	cmd, _ := reg.Lookup("learnings")
	if err := cmd.Run(ctx, []string{"planner"}); err != nil {
		t.Fatalf("learnings.Run: %v", err)
	}
	if out.Len() == 0 {
		t.Error("learnings produced no output")
	}
	got := out.String()
	if !strings.Contains(got, "planner:foo") {
		t.Errorf("expected planner:foo key in output; got: %q", got)
	}
	if strings.Contains(got, "dri:") {
		t.Errorf("cross-role dri: key must not appear in output; got: %q", got)
	}
}
