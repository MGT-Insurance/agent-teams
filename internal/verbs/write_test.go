package verbs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erlloyd/agent-teams/internal/bd"
	"github.com/erlloyd/agent-teams/internal/cli"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// capturedCall records a single fake bd invocation.
type capturedCall struct {
	args []string
}

// fakeExec returns an ExecFunc that records calls and returns the configured
// response for the given command index. jsonResp, if non-empty, is returned as
// stdout for that call.
func fakeExec(responses []fakeResp) (bd.ExecFunc, *[]capturedCall) {
	calls := &[]capturedCall{}
	idx := 0
	return func(name string, args ...string) ([]byte, []byte, error) {
		// Strip the leading -C <home> that Client prepends.
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		*calls = append(*calls, capturedCall{args: stripped})
		var resp fakeResp
		if idx < len(responses) {
			resp = responses[idx]
		}
		idx++
		if resp.err != nil {
			return nil, []byte(resp.errOut), resp.err
		}
		return []byte(resp.stdout), nil, nil
	}, calls
}

type fakeResp struct {
	stdout string
	errOut string
	err    error
}

// makeTempFile writes content to a temp file and returns its path.
func makeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ateam-test-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// newCtx builds a cli.Context backed by a fake bd client.
func newCtx(t *testing.T, responses []fakeResp) (*cli.Context, *[]capturedCall) {
	t.Helper()
	execFn, calls := fakeExec(responses)
	client := bd.NewClientWithExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	return &cli.Context{
		Home:   t.TempDir(),
		BD:     client,
		Stdout: &stdout,
		Stderr: &stderr,
	}, calls
}

// stdoutOf returns the string written to ctx.Stdout.
func stdoutOf(ctx *cli.Context) string {
	return ctx.Stdout.(*bytes.Buffer).String()
}

// ── parseRegisterFlags ────────────────────────────────────────────────────────

func TestParseRegisterFlags_BothForms(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantTitle string
		wantFile  string
	}{
		{
			name:      "space form",
			args:      []string{"--title", "My Init", "--file", "/tmp/body.md"},
			wantTitle: "My Init",
			wantFile:  "/tmp/body.md",
		},
		{
			name:      "equals form",
			args:      []string{"--title=My Init", "--file=/tmp/body.md"},
			wantTitle: "My Init",
			wantFile:  "/tmp/body.md",
		},
		{
			name:      "mixed forms",
			args:      []string{"--title", "Foo", "--file=/tmp/x.md"},
			wantTitle: "Foo",
			wantFile:  "/tmp/x.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, file, err := parseRegisterFlags(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if file != tt.wantFile {
				t.Errorf("file = %q, want %q", file, tt.wantFile)
			}
		})
	}
}

func TestParseRegisterFlags_Errors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "unknown flag",
			args:    []string{"--title=x", "--file=y", "--extra=z"},
			wantMsg: "unknown flag",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parseRegisterFlags(tt.args)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// ── register ──────────────────────────────────────────────────────────────────

func TestRegister_PrintsID(t *testing.T) {
	bodyFile := makeTempFile(t, "initiative body")
	issue := bd.Issue{ID: "at-abc123", Title: "My Init"}
	jsonOut, _ := json.Marshal(issue)

	ctx, calls := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})
	cmd := &registerCmd{}
	err := cmd.Run(ctx, []string{"--title", "My Init", "--file", bodyFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(stdoutOf(ctx))
	if out != "at-abc123" {
		t.Errorf("stdout = %q, want %q", out, "at-abc123")
	}

	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(*calls))
	}
	call := (*calls)[0]
	if !containsArg(call.args, "--json") {
		t.Errorf("bd args missing --json: %v", call.args)
	}
	if !containsArg(call.args, "--title=My Init") {
		t.Errorf("bd args missing --title: %v", call.args)
	}
	if !containsArg(call.args, "--type=task") {
		t.Errorf("bd args missing --type=task: %v", call.args)
	}
	if !containsArgPrefix(call.args, "--body-file=") {
		t.Errorf("bd args missing --body-file: %v", call.args)
	}
}

func TestRegister_EqualsForm(t *testing.T) {
	bodyFile := makeTempFile(t, "body")
	issue := bd.Issue{ID: "at-xyz", Title: "T"}
	jsonOut, _ := json.Marshal(issue)

	ctx, _ := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})
	cmd := &registerCmd{}
	err := cmd.Run(ctx, []string{"--title=T", "--file=" + bodyFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(stdoutOf(ctx))
	if out != "at-xyz" {
		t.Errorf("stdout = %q, want %q", out, "at-xyz")
	}
}

func TestRegister_MissingTitle(t *testing.T) {
	bodyFile := makeTempFile(t, "body")
	ctx, _ := newCtx(t, nil)
	err := (&registerCmd{}).Run(ctx, []string{"--file", bodyFile})
	if err == nil {
		t.Fatal("expected error for missing --title")
	}
	if _, ok := err.(*cli.UsageError); !ok {
		t.Errorf("expected UsageError, got %T: %v", err, err)
	}
}

func TestRegister_MissingFile(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&registerCmd{}).Run(ctx, []string{"--title", "T"})
	if err == nil {
		t.Fatal("expected error for missing --file")
	}
	if _, ok := err.(*cli.UsageError); !ok {
		t.Errorf("expected UsageError, got %T: %v", err, err)
	}
}

func TestRegister_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&registerCmd{}).Run(ctx, []string{"--title", "T", "--file", "/nonexistent/path.md"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("error %q does not contain 'file not found'", err.Error())
	}
}

func TestRegister_EmptyID(t *testing.T) {
	bodyFile := makeTempFile(t, "body")
	// bd returns JSON with no id field → issue.ID will be ""
	ctx, _ := newCtx(t, []fakeResp{{stdout: `{}`}})
	err := (&registerCmd{}).Run(ctx, []string{"--title", "T", "--file", bodyFile})
	if err == nil {
		t.Fatal("expected error when bd returns empty id")
	}
	if _, ok := err.(*cli.DepError); !ok {
		t.Errorf("expected *cli.DepError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "no id") {
		t.Errorf("error %q does not contain 'no id'", err.Error())
	}
	if stdoutOf(ctx) != "" {
		t.Errorf("stdout = %q, want empty on error", stdoutOf(ctx))
	}
}

// ── note ──────────────────────────────────────────────────────────────────────

func TestNote_CallsBDNote(t *testing.T) {
	f := makeTempFile(t, "note content")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&noteCmd{}).Run(ctx, []string{"at-1", "--file", f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-1", "--file=" + f})
}

func TestNote_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "note")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&noteCmd{}).Run(ctx, []string{"at-1", "--file=" + f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-1", "--file=" + f})
}

func TestNote_MissingID(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&noteCmd{}).Run(ctx, nil)
	assertUsageError(t, err, "missing <id>")
}

func TestNote_MissingFile(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&noteCmd{}).Run(ctx, []string{"at-1"})
	assertUsageError(t, err, "--file required")
}

func TestNote_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&noteCmd{}).Run(ctx, []string{"at-1", "--file", "/no/such/file"})
	assertUsageError(t, err, "file not found")
}

// ── gate ──────────────────────────────────────────────────────────────────────

func TestGate_NoteAndLabel(t *testing.T) {
	f := makeTempFile(t, "question")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}})
	err := (&gateCmd{}).Run(ctx, []string{"at-2", "--file", f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 bd calls, got %d", len(*calls))
	}
	// First call: note
	assertArgs(t, *calls, 0, []string{"note", "at-2", "--file=" + f})
	// Second call: label add
	assertArgs(t, *calls, 1, []string{"label", "add", "at-2", "human"})
}

func TestGate_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "question")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}})
	err := (&gateCmd{}).Run(ctx, []string{"at-2", "--file=" + f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-2", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "add", "at-2", "human"})
}

func TestGate_MissingID(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&gateCmd{}).Run(ctx, nil), "missing <id>")
}

func TestGate_MissingFile(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&gateCmd{}).Run(ctx, []string{"at-2"}), "--file required")
}

// ── clear-gate ────────────────────────────────────────────────────────────────

func TestClearGate_WithFile(t *testing.T) {
	f := makeTempFile(t, "response")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateCmd{}).Run(ctx, []string{"at-3", "--file", f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"comment", "at-3", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "human"})
}

func TestClearGate_WithoutFile(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&clearGateCmd{}).Run(ctx, []string{"at-3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"label", "remove", "at-3", "human"})
}

func TestClearGate_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "response")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateCmd{}).Run(ctx, []string{"at-3", "--file=" + f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"comment", "at-3", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "human"})
}

func TestClearGate_MissingID(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&clearGateCmd{}).Run(ctx, nil), "missing <id>")
}

func TestClearGate_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&clearGateCmd{}).Run(ctx, []string{"at-3", "--file", "/no/such"})
	assertUsageError(t, err, "file not found")
}

// ── parseLearnFlags ───────────────────────────────────────────────────────────

func TestParseLearnFlags_BothForms(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantRole string
		wantSlug string
		wantFile string
	}{
		{
			name:     "space form",
			args:     []string{"planner", "design-heuristics", "--file", "/tmp/f.md"},
			wantRole: "planner",
			wantSlug: "design-heuristics",
			wantFile: "/tmp/f.md",
		},
		{
			name:     "equals form",
			args:     []string{"planner", "design-heuristics", "--file=/tmp/f.md"},
			wantRole: "planner",
			wantSlug: "design-heuristics",
			wantFile: "/tmp/f.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, slug, file, err := parseLearnFlags(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if role != tt.wantRole {
				t.Errorf("role = %q, want %q", role, tt.wantRole)
			}
			if slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", slug, tt.wantSlug)
			}
			if file != tt.wantFile {
				t.Errorf("file = %q, want %q", file, tt.wantFile)
			}
		})
	}
}

func TestLearn_CallsBDRemember(t *testing.T) {
	f := makeTempFile(t, "learned content here")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&learnCmd{}).Run(ctx, []string{"planner", "design-heuristics", "--file", f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(*calls))
	}
	call := (*calls)[0]
	if len(call.args) < 3 {
		t.Fatalf("too few args: %v", call.args)
	}
	if call.args[0] != "remember" {
		t.Errorf("args[0] = %q, want %q", call.args[0], "remember")
	}
	if call.args[1] != "--key=planner:design-heuristics" {
		t.Errorf("args[1] = %q, want %q", call.args[1], "--key=planner:design-heuristics")
	}
	if call.args[2] != "learned content here" {
		t.Errorf("args[2] = %q, want %q", call.args[2], "learned content here")
	}
}

func TestLearn_MissingRole(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&learnCmd{}).Run(ctx, nil), "missing <role>")
}

func TestLearn_MissingSlug(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&learnCmd{}).Run(ctx, []string{"planner"}), "missing <slug>")
}

func TestLearn_MissingFile(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&learnCmd{}).Run(ctx, []string{"planner", "slug"}), "--file required")
}

func TestLearn_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&learnCmd{}).Run(ctx, []string{"planner", "slug", "--file", "/no/such/file"})
	assertUsageError(t, err, "file not found")
}

// ── parseCloseFlags ───────────────────────────────────────────────────────────

func TestParseCloseFlags_AllForms(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantID     string
		wantReason string
		wantFile   string
	}{
		{
			name:   "bare id",
			args:   []string{"at-1"},
			wantID: "at-1",
		},
		{
			name:       "reason space form",
			args:       []string{"at-1", "--reason", "done"},
			wantID:     "at-1",
			wantReason: "done",
		},
		{
			name:       "reason equals form",
			args:       []string{"at-1", "--reason=done"},
			wantID:     "at-1",
			wantReason: "done",
		},
		{
			name:     "file space form",
			args:     []string{"at-1", "--file", "/tmp/r.md"},
			wantID:   "at-1",
			wantFile: "/tmp/r.md",
		},
		{
			name:     "file equals form",
			args:     []string{"at-1", "--file=/tmp/r.md"},
			wantID:   "at-1",
			wantFile: "/tmp/r.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, reason, file, err := parseCloseFlags(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			if file != tt.wantFile {
				t.Errorf("file = %q, want %q", file, tt.wantFile)
			}
		})
	}
}

func TestClose_BareID(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeCmd{}).Run(ctx, []string{"at-5"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5"})
}

func TestClose_WithReason(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeCmd{}).Run(ctx, []string{"at-5", "--reason", "shipped"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=shipped"})
}

func TestClose_WithReasonEqualsForm(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeCmd{}).Run(ctx, []string{"at-5", "--reason=shipped"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=shipped"})
}

func TestClose_WithFile(t *testing.T) {
	content := "reason from file"
	f := makeTempFile(t, content)
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeCmd{}).Run(ctx, []string{"at-5", "--file", f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// --file should override reason inline
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=" + content})
}

func TestClose_MissingID(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&closeCmd{}).Run(ctx, nil), "missing <id>")
}

// ── reopen ────────────────────────────────────────────────────────────────────

func TestReopen_CallsBDReopen(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&reopenCmd{}).Run(ctx, []string{"at-6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"reopen", "at-6"})
}

func TestReopen_MissingID(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	assertUsageError(t, (&reopenCmd{}).Run(ctx, nil), "missing <id>")
}

// ── sync ──────────────────────────────────────────────────────────────────────

func TestSync_CallsBDDoltPush(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "push complete"}})
	err := (&syncCmd{}).Run(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"dolt", "push"})
}

func TestSync_NilContext(t *testing.T) {
	err := (&syncCmd{}).Run(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── integration: register + gate/clear-gate via temp workspace ─────────────────

// TestRegisterGateRoundtrip runs register → gate → clear-gate against a fake
// bd exec to verify the exact arg sequences without a real bd binary.
func TestRegisterGateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	bodyFile := filepath.Join(dir, "body.md")
	if err := os.WriteFile(bodyFile, []byte("initiative body"), 0600); err != nil {
		t.Fatal(err)
	}
	questionFile := filepath.Join(dir, "question.md")
	if err := os.WriteFile(questionFile, []byte("is this blocked?"), 0600); err != nil {
		t.Fatal(err)
	}
	responseFile := filepath.Join(dir, "response.md")
	if err := os.WriteFile(responseFile, []byte("no, proceeding"), 0600); err != nil {
		t.Fatal(err)
	}

	issue := bd.Issue{ID: "at-round1", Title: "Round Trip Init"}
	jsonOut, _ := json.Marshal(issue)

	responses := []fakeResp{
		{stdout: string(jsonOut)}, // register: create --json
		{stdout: "ok"},            // gate: note
		{stdout: "ok"},            // gate: label add
		{stdout: "ok"},            // clear-gate: comment
		{stdout: "ok"},            // clear-gate: label remove
	}
	execFn, calls := fakeExec(responses)
	client := bd.NewClientWithExec(dir, execFn)
	var stdout bytes.Buffer
	ctx := &cli.Context{Home: dir, BD: client, Stdout: &stdout, Stderr: &bytes.Buffer{}}

	// register
	if err := (&registerCmd{}).Run(ctx, []string{"--title", "Round Trip Init", "--file", bodyFile}); err != nil {
		t.Fatalf("register: %v", err)
	}
	gotID := strings.TrimSpace(stdout.String())
	if gotID != "at-round1" {
		t.Errorf("register: id = %q, want %q", gotID, "at-round1")
	}

	// gate
	if err := (&gateCmd{}).Run(ctx, []string{"at-round1", "--file", questionFile}); err != nil {
		t.Fatalf("gate: %v", err)
	}

	// clear-gate with file
	if err := (&clearGateCmd{}).Run(ctx, []string{"at-round1", "--file", responseFile}); err != nil {
		t.Fatalf("clear-gate: %v", err)
	}

	// Verify call sequence
	if len(*calls) != 5 {
		t.Fatalf("expected 5 bd calls, got %d: %v", len(*calls), *calls)
	}
	// call 0: create --json
	if (*calls)[0].args[0] != "create" {
		t.Errorf("call[0] = %v, want create …", (*calls)[0].args)
	}
	if !containsArg((*calls)[0].args, "--json") {
		t.Errorf("call[0] missing --json: %v", (*calls)[0].args)
	}
	// call 1: note
	assertArgs(t, *calls, 1, []string{"note", "at-round1", "--file=" + questionFile})
	// call 2: label add human
	assertArgs(t, *calls, 2, []string{"label", "add", "at-round1", "human"})
	// call 3: comment
	assertArgs(t, *calls, 3, []string{"comment", "at-round1", "--file=" + responseFile})
	// call 4: label remove human
	assertArgs(t, *calls, 4, []string{"label", "remove", "at-round1", "human"})
}

// ── stdout forwarding ─────────────────────────────────────────────────────────

func TestNote_ForwardsBDStdout(t *testing.T) {
	f := makeTempFile(t, "note content")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Note added to at-1"}})
	if err := (&noteCmd{}).Run(ctx, []string{"at-1", "--file", f}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Note added to at-1") {
		t.Errorf("stdout = %q, want it to contain bd output", got)
	}
}

func TestNote_NoBlankLineWhenEmpty(t *testing.T) {
	f := makeTempFile(t, "note content")
	ctx, _ := newCtx(t, []fakeResp{{stdout: ""}})
	if err := (&noteCmd{}).Run(ctx, []string{"at-1", "--file", f}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdoutOf(ctx) != "" {
		t.Errorf("stdout = %q, want empty when bd returns empty", stdoutOf(ctx))
	}
}

func TestGate_ForwardsBothOutputs(t *testing.T) {
	f := makeTempFile(t, "question")
	ctx, _ := newCtx(t, []fakeResp{
		{stdout: "✓ Note added to at-2"},
		{stdout: "✓ Added label 'human'"},
	})
	if err := (&gateCmd{}).Run(ctx, []string{"at-2", "--file", f}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := stdoutOf(ctx)
	if !strings.Contains(got, "✓ Note added to at-2") {
		t.Errorf("stdout missing note output; got %q", got)
	}
	if !strings.Contains(got, "✓ Added label 'human'") {
		t.Errorf("stdout missing label output; got %q", got)
	}
	// note output must appear before label output
	noteIdx := strings.Index(got, "✓ Note added to at-2")
	labelIdx := strings.Index(got, "✓ Added label 'human'")
	if noteIdx > labelIdx {
		t.Errorf("note output appeared after label output in stdout")
	}
}

func TestClearGate_WithFile_ForwardsBothOutputs(t *testing.T) {
	f := makeTempFile(t, "response")
	ctx, _ := newCtx(t, []fakeResp{
		{stdout: "✓ Comment added"},
		{stdout: "✓ Removed label 'human'"},
	})
	if err := (&clearGateCmd{}).Run(ctx, []string{"at-3", "--file", f}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := stdoutOf(ctx)
	if !strings.Contains(got, "✓ Comment added") {
		t.Errorf("stdout missing comment output; got %q", got)
	}
	if !strings.Contains(got, "✓ Removed label 'human'") {
		t.Errorf("stdout missing label-remove output; got %q", got)
	}
	commentIdx := strings.Index(got, "✓ Comment added")
	labelIdx := strings.Index(got, "✓ Removed label 'human'")
	if commentIdx > labelIdx {
		t.Errorf("comment output appeared after label-remove output in stdout")
	}
}

func TestClearGate_WithoutFile_ForwardsLabelOutput(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Removed label 'human'"}})
	if err := (&clearGateCmd{}).Run(ctx, []string{"at-3"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Removed label 'human'") {
		t.Errorf("stdout = %q, want label-remove output", got)
	}
}

func TestLearn_ForwardsBDStdout(t *testing.T) {
	f := makeTempFile(t, "learned content")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Stored planner:slug"}})
	if err := (&learnCmd{}).Run(ctx, []string{"planner", "slug", "--file", f}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Stored planner:slug") {
		t.Errorf("stdout = %q, want bd remember output", got)
	}
}

func TestClose_BareID_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Closed at-5"}})
	if err := (&closeCmd{}).Run(ctx, []string{"at-5"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Closed at-5") {
		t.Errorf("stdout = %q, want close confirmation", got)
	}
}

func TestClose_WithReason_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Closed at-5"}})
	if err := (&closeCmd{}).Run(ctx, []string{"at-5", "--reason", "shipped"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Closed at-5") {
		t.Errorf("stdout = %q, want close confirmation", got)
	}
}

func TestReopen_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Reopened at-6"}})
	if err := (&reopenCmd{}).Run(ctx, []string{"at-6"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Reopened at-6") {
		t.Errorf("stdout = %q, want reopen confirmation", got)
	}
}

func TestSync_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "push complete"}})
	if err := (&syncCmd{}).Run(ctx, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "push complete") {
		t.Errorf("stdout = %q, want push confirmation", got)
	}
}

func TestRegister_PrintsOnlyID(t *testing.T) {
	bodyFile := makeTempFile(t, "body")
	issue := bd.Issue{ID: "at-only", Title: "T"}
	jsonOut, _ := json.Marshal(issue)
	ctx, _ := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})
	if err := (&registerCmd{}).Run(ctx, []string{"--title", "T", "--file", bodyFile}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// register must print exactly the bare id, not the full JSON
	got := strings.TrimSpace(stdoutOf(ctx))
	if got != "at-only" {
		t.Errorf("register stdout = %q, want bare id %q", got, "at-only")
	}
}

// ── assertion helpers ─────────────────────────────────────────────────────────

func assertUsageError(t *testing.T, err error, wantSubstr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected UsageError containing %q, got nil", wantSubstr)
	}
	if _, ok := err.(*cli.UsageError); !ok {
		t.Errorf("expected *cli.UsageError, got %T: %v", err, err)
	}
	if wantSubstr != "" && !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}

func assertArgs(t *testing.T, calls []capturedCall, idx int, want []string) {
	t.Helper()
	if idx >= len(calls) {
		t.Fatalf("call[%d] missing (total calls: %d)", idx, len(calls))
	}
	got := calls[idx].args
	if len(got) != len(want) {
		t.Errorf("call[%d] args = %v, want %v", idx, got, want)
		return
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("call[%d] args[%d] = %q, want %q", idx, i, got[i], w)
		}
	}
}

func containsArg(args []string, arg string) bool {
	for _, a := range args {
		if a == arg {
			return true
		}
	}
	return false
}

func containsArgPrefix(args []string, prefix string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
