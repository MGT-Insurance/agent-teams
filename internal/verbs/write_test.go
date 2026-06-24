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

// ── register ──────────────────────────────────────────────────────────────────

func TestRegister_PrintsID(t *testing.T) {
	bodyFile := makeTempFile(t, "initiative body")
	issue := bd.Issue{ID: "at-abc123", Title: "My Init"}
	jsonOut, _ := json.Marshal(issue)

	ctx, calls := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})
	cmd := &registerKong{Title: "My Init", File: bodyFile}
	err := cmd.Run(ctx)
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
	cmd := &registerKong{Title: "T", File: bodyFile}
	err := cmd.Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(stdoutOf(ctx))
	if out != "at-xyz" {
		t.Errorf("stdout = %q, want %q", out, "at-xyz")
	}
}

func TestRegister_MissingTitle(t *testing.T) {
	// kong enforces required:"" at parse time; verify via parser.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"register", "--file", "/tmp/f.md"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing --title")
	}
}

func TestRegister_MissingFile(t *testing.T) {
	// kong enforces required:"" at parse time; verify via parser.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"register", "--title", "T"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing --file")
	}
}

func TestRegister_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&registerKong{Title: "T", File: "/nonexistent/path.md"}).Run(ctx)
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
	err := (&registerKong{Title: "T", File: bodyFile}).Run(ctx)
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
	err := (&noteKong{ID: "at-1", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-1", "--file=" + f})
}

func TestNote_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "note")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&noteKong{ID: "at-1", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-1", "--file=" + f})
}

func TestNote_MissingID(t *testing.T) {
	// ID is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"note"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <id>")
	}
}

func TestNote_MissingFile(t *testing.T) {
	// File is required:""; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"note", "at-1"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing --file")
	}
}

func TestNote_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&noteKong{ID: "at-1", File: "/no/such/file"}).Run(ctx)
	assertUsageError(t, err, "file not found")
}

// ── gate ──────────────────────────────────────────────────────────────────────

func TestGate_NoteAndLabel(t *testing.T) {
	// No --kind: defaults to question => 3 calls (note, label add human, label add gate:question)
	f := makeTempFile(t, "question")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&gateKong{ID: "at-2", File: f, Kind: "question"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"note", "at-2", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "add", "at-2", "human"})
	assertArgs(t, *calls, 2, []string{"label", "add", "at-2", "gate:question"})
}

func TestGate_KindReview(t *testing.T) {
	f := makeTempFile(t, "pr ready")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&gateKong{ID: "at-2", File: f, Kind: "review"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"note", "at-2", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "add", "at-2", "human"})
	assertArgs(t, *calls, 2, []string{"label", "add", "at-2", "gate:review"})
}

func TestGate_KindReviewEqualsForm(t *testing.T) {
	f := makeTempFile(t, "pr ready")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&gateKong{ID: "at-2", File: f, Kind: "review"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 2, []string{"label", "add", "at-2", "gate:review"})
}

func TestGate_KindQuestionExplicit(t *testing.T) {
	f := makeTempFile(t, "question")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&gateKong{ID: "at-2", File: f, Kind: "question"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 2, []string{"label", "add", "at-2", "gate:question"})
}

func TestGate_KindBogus(t *testing.T) {
	// enum:"review,question" is enforced at parse time; verify via parser.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	f := makeTempFile(t, "question")
	_, parseErr := p.Parse([]string{"gate", "at-2", "--file", f, "--kind=bogus"})
	if parseErr == nil {
		t.Fatal("expected parse error for bogus kind")
	}
	// kong reports: "--kind must be one of "review","question" but got "bogus""
	if !strings.Contains(parseErr.Error(), "review") || !strings.Contains(parseErr.Error(), "question") {
		t.Errorf("error = %q, want kind enum violation message", parseErr.Error())
	}
}

func TestGate_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "question")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&gateKong{ID: "at-2", File: f, Kind: "question"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-2", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "add", "at-2", "human"})
	assertArgs(t, *calls, 2, []string{"label", "add", "at-2", "gate:question"})
}

func TestGate_MissingID(t *testing.T) {
	// ID is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"gate"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <id>")
	}
}

func TestGate_MissingFile(t *testing.T) {
	// Validate() enforces --file required when no structured form used.
	g := &gateKong{ID: "at-2", Kind: "question"}
	err := g.Validate(nil)
	assertUsageError(t, err, "--file required")
}

// ── gate: structured-ask flags ────────────────────────────────────────────────

func TestGate_StructuredAsk_WriteSentinelBlock(t *testing.T) {
	// Structured form: 3 bd calls (note with sentinel content, label add human, label add gate:question)
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&gateKong{
		ID:             "at-s1",
		Decision:       "Should we use approach A?",
		Recommendation: "Yes, use approach A",
		Alternative:    "Use approach B instead",
		Kind:           "question",
	}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
	// call 0: note with temp file containing sentinel block
	noteCall := (*calls)[0]
	if noteCall.args[0] != "note" {
		t.Errorf("call[0] = %q, want note", noteCall.args[0])
	}
	if noteCall.args[1] != "at-s1" {
		t.Errorf("call[0] id = %q, want at-s1", noteCall.args[1])
	}
	if !containsArgPrefix(noteCall.args, "--file=") {
		t.Errorf("call[0] missing --file=: %v", noteCall.args)
	}
	// call 1: label add human
	assertArgs(t, *calls, 1, []string{"label", "add", "at-s1", "human"})
	// call 2: label add gate:question (default kind)
	assertArgs(t, *calls, 2, []string{"label", "add", "at-s1", "gate:question"})
}

func TestGate_StructuredAsk_SentinelFormat(t *testing.T) {
	// Capture the temp file path from the note call and read its content to
	// verify the exact sentinel-delimited format from contract j9s section 2.
	var capturedFile string
	calls := &[]capturedCall{}
	idx := 0
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		*calls = append(*calls, capturedCall{args: stripped})
		if idx == 0 {
			// note call: capture the --file= path
			for _, a := range stripped {
				if strings.HasPrefix(a, "--file=") {
					capturedFile = a[len("--file="):]
				}
			}
		}
		idx++
		return []byte("ok"), nil, nil
	}
	client := bd.NewClientWithExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	contextFile := makeTempFile(t, "some optional context here")
	err := (&gateKong{
		ID:             "at-s2",
		Decision:       "Which design to pick?",
		Recommendation: "Design A",
		Alternative:    "Design B",
		ContextFile:    contextFile,
		Kind:           "review",
	}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Re-do with a content-capturing exec to read file before deferred Remove.
	var capturedContent string
	idx2 := 0
	execFn2 := func(name string, args ...string) ([]byte, []byte, error) {
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		if idx2 == 0 {
			for _, a := range stripped {
				if strings.HasPrefix(a, "--file=") {
					path := a[len("--file="):]
					data, _ := os.ReadFile(path)
					capturedContent = string(data)
				}
			}
		}
		idx2++
		return []byte("ok"), nil, nil
	}
	client2 := bd.NewClientWithExec(t.TempDir(), execFn2)
	var stdout2, stderr2 bytes.Buffer
	ctx2 := &cli.Context{Home: t.TempDir(), BD: client2, Stdout: &stdout2, Stderr: &stderr2}

	contextFile2 := makeTempFile(t, "some optional context here")
	if err := (&gateKong{
		ID:             "at-s2",
		Decision:       "Which design to pick?",
		Recommendation: "Design A",
		Alternative:    "Design B",
		ContextFile:    contextFile2,
		Kind:           "review",
	}).Run(ctx2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "<<<ateam-ask\ndecision: Which design to pick?\nrecommendation: Design A\nalternative: Design B\ncontext: some optional context here\n>>>"
	if capturedContent != want {
		t.Errorf("sentinel block =\n%q\nwant:\n%q", capturedContent, want)
	}
	_ = capturedFile
}

func TestGate_StructuredAsk_WithoutContext(t *testing.T) {
	var capturedContent string
	idx := 0
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		if idx == 0 {
			for _, a := range stripped {
				if strings.HasPrefix(a, "--file=") {
					data, _ := os.ReadFile(a[len("--file="):])
					capturedContent = string(data)
				}
			}
		}
		idx++
		return []byte("ok"), nil, nil
	}
	client := bd.NewClientWithExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	if err := (&gateKong{
		ID:             "at-s3",
		Decision:       "Go or no-go?",
		Recommendation: "Go",
		Alternative:    "No-go",
		Kind:           "question",
	}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "<<<ateam-ask\ndecision: Go or no-go?\nrecommendation: Go\nalternative: No-go\n>>>"
	if capturedContent != want {
		t.Errorf("sentinel block without context =\n%q\nwant:\n%q", capturedContent, want)
	}
}

func TestGate_StructuredAsk_DecisionTooLong(t *testing.T) {
	long := strings.Repeat("x", 121)
	g := &gateKong{
		ID:             "at-s4",
		Decision:       long,
		Recommendation: "r",
		Alternative:    "a",
		Kind:           "question",
	}
	err := g.Validate(nil)
	assertUsageError(t, err, "exceeds 120 chars")
}

func TestGate_StructuredAsk_EmptyDecision(t *testing.T) {
	// Using another structured flag but empty --decision triggers the required check.
	g := &gateKong{
		ID:             "at-s5",
		Recommendation: "r",
		Alternative:    "a",
		Kind:           "question",
	}
	err := g.Validate(nil)
	assertUsageError(t, err, "--decision required")
}

func TestGate_StructuredAsk_ContextTooLong(t *testing.T) {
	contextFile := makeTempFile(t, strings.Repeat("y", 281))
	g := &gateKong{
		ID:             "at-s6",
		Decision:       "A short decision",
		Recommendation: "r",
		Alternative:    "a",
		ContextFile:    contextFile,
		Kind:           "question",
	}
	err := g.Validate(nil)
	assertUsageError(t, err, "exceeds 280 chars")
}

func TestGate_StructuredAsk_ContextExactLimit(t *testing.T) {
	// 280 chars should be accepted.
	var capturedContent string
	idx := 0
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		if idx == 0 {
			for _, a := range stripped {
				if strings.HasPrefix(a, "--file=") {
					data, _ := os.ReadFile(a[len("--file="):])
					capturedContent = string(data)
				}
			}
		}
		idx++
		return []byte("ok"), nil, nil
	}
	client := bd.NewClientWithExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	exactContext := strings.Repeat("z", 280)
	contextFile := makeTempFile(t, exactContext)
	if err := (&gateKong{
		ID:             "at-s7",
		Decision:       "Boundary check",
		Recommendation: "r",
		Alternative:    "a",
		ContextFile:    contextFile,
		Kind:           "question",
	}).Run(ctx); err != nil {
		t.Fatalf("unexpected error for 280-char context: %v", err)
	}
	if !strings.Contains(capturedContent, exactContext) {
		t.Errorf("expected 280-char context in sentinel block")
	}
}

func TestGate_StructuredAsk_MutuallyExclusiveWithFile(t *testing.T) {
	f := makeTempFile(t, "prose")
	// xor enforcement is at parse time; test via parser.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"gate", "at-s8", "--file", f, "--decision", "d"})
	if parseErr == nil {
		t.Fatal("expected parse error for --file + structured flag together")
	}
	if !strings.Contains(parseErr.Error(), "mutually exclusive") && !strings.Contains(parseErr.Error(), "can't be used together") {
		t.Errorf("error = %q, want mutual exclusion message", parseErr.Error())
	}
}

func TestGate_StructuredAsk_SetsHumanAndGateKind(t *testing.T) {
	calls := &[]capturedCall{}
	idx := 0
	execFn := func(name string, args ...string) ([]byte, []byte, error) {
		stripped := args
		if len(args) >= 2 && args[0] == "-C" {
			stripped = args[2:]
		}
		*calls = append(*calls, capturedCall{args: stripped})
		idx++
		return []byte("ok"), nil, nil
	}
	client := bd.NewClientWithExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	if err := (&gateKong{
		ID:             "at-s9",
		Decision:       "Should we proceed?",
		Recommendation: "Yes",
		Alternative:    "No",
		Kind:           "review",
	}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 1, []string{"label", "add", "at-s9", "human"})
	assertArgs(t, *calls, 2, []string{"label", "add", "at-s9", "gate:review"})
}

// ── clear-gate ────────────────────────────────────────────────────────────────

func TestClearGate_WithFile(t *testing.T) {
	// 4 calls: comment, label remove human, label remove gate:review, label remove gate:question
	f := makeTempFile(t, "response")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateKong{ID: "at-3", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 4 {
		t.Fatalf("expected 4 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"comment", "at-3", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "human"})
	assertArgs(t, *calls, 2, []string{"label", "remove", "at-3", "gate:review"})
	assertArgs(t, *calls, 3, []string{"label", "remove", "at-3", "gate:question"})
}

func TestClearGate_WithoutFile(t *testing.T) {
	// 3 calls: label remove human, label remove gate:review, label remove gate:question
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateKong{ID: "at-3"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"label", "remove", "at-3", "human"})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "gate:review"})
	assertArgs(t, *calls, 2, []string{"label", "remove", "at-3", "gate:question"})
}

func TestClearGate_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "response")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateKong{ID: "at-3", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"comment", "at-3", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "human"})
	assertArgs(t, *calls, 2, []string{"label", "remove", "at-3", "gate:review"})
	assertArgs(t, *calls, 3, []string{"label", "remove", "at-3", "gate:question"})
}

func TestClearGate_MissingID(t *testing.T) {
	// ID is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"clear-gate"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <id>")
	}
}

func TestClearGate_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&clearGateKong{ID: "at-3", File: "/no/such"}).Run(ctx)
	assertUsageError(t, err, "file not found")
}

// ── learn ─────────────────────────────────────────────────────────────────────

func TestLearn_CallsBDRemember(t *testing.T) {
	f := makeTempFile(t, "learned content here")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&learnKong{Role: "planner", Slug: "design-heuristics", File: f}).Run(ctx)
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
	// Default slugs now get the fresh: prefix.
	if call.args[1] != "--key=planner:fresh:design-heuristics" {
		t.Errorf("args[1] = %q, want %q", call.args[1], "--key=planner:fresh:design-heuristics")
	}
	if call.args[2] != "learned content here" {
		t.Errorf("args[2] = %q, want %q", call.args[2], "learned content here")
	}
}

func TestLearn_DefaultSlugGetsFreshPrefix(t *testing.T) {
	f := makeTempFile(t, "body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := (*calls)[0].args[1]; got != "--key=implementer:fresh:foo" {
		t.Errorf("key = %q, want --key=implementer:fresh:foo", got)
	}
}

func TestLearn_HotSlugPassthrough(t *testing.T) {
	f := makeTempFile(t, "body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "hot:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := (*calls)[0].args[1]; got != "--key=implementer:hot:foo" {
		t.Errorf("key = %q, want --key=implementer:hot:foo", got)
	}
}

func TestLearn_FreshSlugPassthrough(t *testing.T) {
	f := makeTempFile(t, "body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "fresh:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must not produce implementer:fresh:fresh:foo.
	if got := (*calls)[0].args[1]; got != "--key=implementer:fresh:foo" {
		t.Errorf("key = %q, want --key=implementer:fresh:foo (no double-prefix)", got)
	}
}

func TestLearn_MissingRole(t *testing.T) {
	// Role is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"learn"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <role>")
	}
}

func TestLearn_MissingSlug(t *testing.T) {
	// Slug is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"learn", "planner"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <slug>")
	}
}

func TestLearn_MissingFile(t *testing.T) {
	// File is required:""; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"learn", "planner", "slug"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing --file")
	}
}

func TestLearn_FileNotFound(t *testing.T) {
	ctx, _ := newCtx(t, nil)
	err := (&learnKong{Role: "planner", Slug: "slug", File: "/no/such/file"}).Run(ctx)
	assertUsageError(t, err, "file not found")
}

// ── close ─────────────────────────────────────────────────────────────────────

func TestClose_BareID(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeKong{ID: "at-5"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5"})
}

func TestClose_WithReason(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeKong{ID: "at-5", Reason: "shipped"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=shipped"})
}

func TestClose_WithReasonEqualsForm(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeKong{ID: "at-5", Reason: "shipped"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=shipped"})
}

func TestClose_WithFile(t *testing.T) {
	content := "reason from file"
	f := makeTempFile(t, content)
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&closeKong{ID: "at-5", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// --file should override reason inline
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=" + content})
}

func TestClose_MissingID(t *testing.T) {
	// ID is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"close"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <id>")
	}
}

// ── reopen ────────────────────────────────────────────────────────────────────

func TestReopen_CallsBDReopen(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	err := (&reopenKong{ID: "at-6"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"reopen", "at-6"})
}

func TestReopen_MissingID(t *testing.T) {
	// ID is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"reopen"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <id>")
	}
}

// ── pull ──────────────────────────────────────────────────────────────────────

func TestPull_CallsBDDoltPull(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "pull complete"}})
	err := (&pullKong{}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"dolt", "pull"})
}

func TestPull_NilContext(t *testing.T) {
	err := (&pullKong{}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

// ── sync ──────────────────────────────────────────────────────────────────────

func TestSync_CallsCommitThenPullThenPush(t *testing.T) {
	// sync must commit first (to clear any dirty working set), then pull, then push.
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: ""}, // commit: no-op when clean
		{stdout: "pull complete"},
		{stdout: "push complete"},
	})
	err := (&syncKong{}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 calls (commit, pull, push), got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"dolt", "commit"})
	assertArgs(t, *calls, 1, []string{"dolt", "pull"})
	assertArgs(t, *calls, 2, []string{"dolt", "push"})
}

func TestSync_CommitNothingToCommitIsSuccess(t *testing.T) {
	// "Nothing to commit" from dolt commit exits 0 in bd 1.0.5, so Run returns
	// ("", nil). Guard: even if surfaced as error, sync must proceed to pull+push.
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: "Nothing to commit.", err: nil}, // clean WS: no-op, no error
		{stdout: "pull complete"},
		{stdout: "push complete"},
	})
	err := (&syncKong{}).Run(ctx)
	if err != nil {
		t.Fatalf("expected success when commit is a no-op, got: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 calls (commit, pull, push), got %d", len(*calls))
	}
}

func TestSync_RetriesPushOnceAfterNonFF(t *testing.T) {
	// First push fails with a non-fast-forward error; sync should pull again
	// and retry the push exactly once, succeeding on the retry.
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: ""},              // commit: no-op
		{stdout: "pull complete"}, // initial pull
		{errOut: "! [rejected] main -> main (non-fast-forward)", err: fmt.Errorf("bd dolt push: exit status 1\n! [rejected] main -> main (non-fast-forward)")}, // first push: non-ff
		{stdout: "pull complete"}, // retry pull
		{stdout: "push complete"}, // retry push: success
	})
	err := (&syncKong{}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error on retry success: %v", err)
	}
	if len(*calls) != 5 {
		t.Fatalf("expected 5 calls (commit, pull, push[non-ff], pull, push), got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"dolt", "commit"})
	assertArgs(t, *calls, 1, []string{"dolt", "pull"})
	assertArgs(t, *calls, 2, []string{"dolt", "push"})
	assertArgs(t, *calls, 3, []string{"dolt", "pull"})
	assertArgs(t, *calls, 4, []string{"dolt", "push"})
}

func TestSync_SurfacesErrorWhenRetryAlsoFails(t *testing.T) {
	// Both push attempts fail with non-ff; the error must be returned and
	// sync must NOT retry more than once (total push calls == 2).
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: ""}, // commit: no-op
		{stdout: "pull complete"},
		{errOut: "! [rejected] main -> main (non-fast-forward)", err: fmt.Errorf("bd dolt push: exit status 1\n! [rejected] main -> main (non-fast-forward)")},
		{stdout: "pull complete"},
		{errOut: "! [rejected] main -> main (non-fast-forward)", err: fmt.Errorf("bd dolt push: exit status 1\n! [rejected] main -> main (non-fast-forward)")},
	})
	err := (&syncKong{}).Run(ctx)
	if err == nil {
		t.Fatal("expected error when retry push also fails")
	}
	// Must not have retried more than once: exactly 5 calls total (commit, pull, push, pull, push).
	if len(*calls) != 5 {
		t.Fatalf("expected 5 calls (commit, pull, push, pull, push), got %d — retry loop may be unbounded", len(*calls))
	}
}

func TestSync_NoRetryOnNonFFUnrelatedError(t *testing.T) {
	// Push fails with a non-retryable error (e.g. auth) — sync must return
	// immediately without retrying.
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: ""}, // commit: no-op
		{stdout: ""}, // pull
		{errOut: "Permission denied", err: fmt.Errorf("bd dolt push: exit status 1\nPermission denied")},
	})
	err := (&syncKong{}).Run(ctx)
	if err == nil {
		t.Fatal("expected error from push failure")
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 calls (commit, pull, push), got %d — non-ff check may be too broad", len(*calls))
	}
}

func TestSync_NilContext(t *testing.T) {
	err := (&syncKong{}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestSync_CommitRealErrorAbortsBeforePull(t *testing.T) {
	// commit fails with a real (non-"Nothing to commit") error — sync must
	// return the error immediately and NOT proceed to pull or push.
	commitErr := fmt.Errorf("bd dolt commit: exit status 1\ndisk full")
	ctx, calls := newCtx(t, []fakeResp{
		{errOut: "disk full", err: commitErr},
	})
	err := (&syncKong{}).Run(ctx)
	if err == nil {
		t.Fatal("expected error when commit fails with real error")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error %q should contain 'disk full'", err.Error())
	}
	// Only the commit call must have been made — pull and push must be skipped.
	if len(*calls) != 1 {
		t.Fatalf("expected 1 call (commit only), got %d: %v — pull/push must not run after commit failure", len(*calls), *calls)
	}
	assertArgs(t, *calls, 0, []string{"dolt", "commit"})
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
		{stdout: "ok"},            // gate: label add human
		{stdout: "ok"},            // gate: label add gate:question
		{stdout: "ok"},            // clear-gate: comment
		{stdout: "ok"},            // clear-gate: label remove human
		{stdout: "ok"},            // clear-gate: label remove gate:review
		{stdout: "ok"},            // clear-gate: label remove gate:question
	}
	execFn, calls := fakeExec(responses)
	client := bd.NewClientWithExec(dir, execFn)
	var stdout bytes.Buffer
	ctx := &cli.Context{Home: dir, BD: client, Stdout: &stdout, Stderr: &bytes.Buffer{}}

	// register
	if err := (&registerKong{Title: "Round Trip Init", File: bodyFile}).Run(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	gotID := strings.TrimSpace(stdout.String())
	if gotID != "at-round1" {
		t.Errorf("register: id = %q, want %q", gotID, "at-round1")
	}

	// gate (default kind=question)
	if err := (&gateKong{ID: "at-round1", File: questionFile, Kind: "question"}).Run(ctx); err != nil {
		t.Fatalf("gate: %v", err)
	}

	// clear-gate with file
	if err := (&clearGateKong{ID: "at-round1", File: responseFile}).Run(ctx); err != nil {
		t.Fatalf("clear-gate: %v", err)
	}

	// Verify call sequence
	if len(*calls) != 8 {
		t.Fatalf("expected 8 bd calls, got %d: %v", len(*calls), *calls)
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
	// call 3: label add gate:question
	assertArgs(t, *calls, 3, []string{"label", "add", "at-round1", "gate:question"})
	// call 4: comment
	assertArgs(t, *calls, 4, []string{"comment", "at-round1", "--file=" + responseFile})
	// call 5: label remove human
	assertArgs(t, *calls, 5, []string{"label", "remove", "at-round1", "human"})
	// call 6: label remove gate:review
	assertArgs(t, *calls, 6, []string{"label", "remove", "at-round1", "gate:review"})
	// call 7: label remove gate:question
	assertArgs(t, *calls, 7, []string{"label", "remove", "at-round1", "gate:question"})
}

// ── stdout forwarding ─────────────────────────────────────────────────────────

func TestNote_ForwardsBDStdout(t *testing.T) {
	f := makeTempFile(t, "note content")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Note added to at-1"}})
	if err := (&noteKong{ID: "at-1", File: f}).Run(ctx); err != nil {
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
	if err := (&noteKong{ID: "at-1", File: f}).Run(ctx); err != nil {
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
		{stdout: "✓ Added label 'gate:question'"},
	})
	if err := (&gateKong{ID: "at-2", File: f, Kind: "question"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := stdoutOf(ctx)
	if !strings.Contains(got, "✓ Note added to at-2") {
		t.Errorf("stdout missing note output; got %q", got)
	}
	if !strings.Contains(got, "✓ Added label 'human'") {
		t.Errorf("stdout missing label output; got %q", got)
	}
	if !strings.Contains(got, "✓ Added label 'gate:question'") {
		t.Errorf("stdout missing gate:question label output; got %q", got)
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
		{stdout: "✓ Removed label 'gate:review'"},
		{stdout: "✓ Removed label 'gate:question'"},
	})
	if err := (&clearGateKong{ID: "at-3", File: f}).Run(ctx); err != nil {
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
	ctx, _ := newCtx(t, []fakeResp{
		{stdout: "✓ Removed label 'human'"},
		{stdout: "✓ Removed label 'gate:review'"},
		{stdout: "✓ Removed label 'gate:question'"},
	})
	if err := (&clearGateKong{ID: "at-3"}).Run(ctx); err != nil {
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
	if err := (&learnKong{Role: "planner", Slug: "slug", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Stored planner:slug") {
		t.Errorf("stdout = %q, want bd remember output", got)
	}
}

func TestLearn_ColdSlugWritesBareKey(t *testing.T) {
	f := makeTempFile(t, "cold body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "cold:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// cold:<slug> must produce role:<slug> — no tier tag, no fresh: prefix.
	if got := (*calls)[0].args[1]; got != "--key=implementer:foo" {
		t.Errorf("key = %q, want --key=implementer:foo (bare cold key)", got)
	}
}

func TestLearn_ColdSlugNotDoublePrefixed(t *testing.T) {
	f := makeTempFile(t, "cold body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "dri", Slug: "cold:some-insight", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := (*calls)[0].args[1]
	// Must be bare role:slug — must not contain fresh: or cold:.
	if got != "--key=dri:some-insight" {
		t.Errorf("key = %q, want --key=dri:some-insight (no tier tag)", got)
	}
}

func TestClose_BareID_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Closed at-5"}})
	if err := (&closeKong{ID: "at-5"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Closed at-5") {
		t.Errorf("stdout = %q, want close confirmation", got)
	}
}

func TestClose_WithReason_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Closed at-5"}})
	if err := (&closeKong{ID: "at-5", Reason: "shipped"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Closed at-5") {
		t.Errorf("stdout = %q, want close confirmation", got)
	}
}

func TestReopen_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Reopened at-6"}})
	if err := (&reopenKong{ID: "at-6"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Reopened at-6") {
		t.Errorf("stdout = %q, want reopen confirmation", got)
	}
}

func TestSync_ForwardsBDStdout(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{
		{stdout: ""}, // commit: no-op
		{stdout: ""}, // pull
		{stdout: "push complete"},
	})
	if err := (&syncKong{}).Run(ctx); err != nil {
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
	if err := (&registerKong{Title: "T", File: bodyFile}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// register must print exactly the bare id, not the full JSON
	got := strings.TrimSpace(stdoutOf(ctx))
	if got != "at-only" {
		t.Errorf("register stdout = %q, want bare id %q", got, "at-only")
	}
}

// ── kong verb core-path tests ─────────────────────────────────────────────────

// TestGateKong_XorRejectsFileAndDecision verifies kong's xor enforcement fires
// when --file and --decision are both supplied (should exit 2).
func TestGateKong_XorRejectsFileAndDecision(t *testing.T) {
	f := makeTempFile(t, "prose")
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"gate", "at-1", "--file", f, "--decision", "d"})
	if parseErr == nil {
		t.Fatal("expected parse error for --file + --decision together")
	}
	if !strings.Contains(parseErr.Error(), "can't be used together") {
		t.Errorf("error = %q, want 'can't be used together'", parseErr.Error())
	}
}

// TestGateKong_ValidateDecisionTooLong verifies Validate fires for --decision > 120.
func TestGateKong_ValidateDecisionTooLong(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	long := strings.Repeat("x", 121)
	_, parseErr := p.Parse([]string{"gate", "at-1", "--decision", long, "--recommendation", "r", "--alternative", "a"})
	if parseErr == nil {
		t.Fatal("expected validation error for --decision > 120 chars")
	}
	if !strings.Contains(parseErr.Error(), "exceeds 120 chars") {
		t.Errorf("error = %q, want 'exceeds 120 chars'", parseErr.Error())
	}
}

// TestGateKong_ValidateMissingDecision verifies Validate fires when structured
// flags are used but --decision is absent.
func TestGateKong_ValidateMissingDecision(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"gate", "at-1", "--recommendation", "r", "--alternative", "a"})
	if parseErr == nil {
		t.Fatal("expected validation error for missing --decision in structured form")
	}
	if !strings.Contains(parseErr.Error(), "--decision required") {
		t.Errorf("error = %q, want '--decision required'", parseErr.Error())
	}
}

// TestGateKong_ValidateFileMissing verifies Validate fires when neither --file
// nor any structured flag is provided.
func TestGateKong_ValidateFileMissing(t *testing.T) {
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"gate", "at-1"})
	if parseErr == nil {
		t.Fatal("expected validation error when no form is supplied")
	}
	if !strings.Contains(parseErr.Error(), "--file required") {
		t.Errorf("error = %q, want '--file required'", parseErr.Error())
	}
}

// TestNoteKong_RunCallsBDNote verifies the kong noteKong struct calls bd note
// with the correct arguments.
func TestNoteKong_RunCallsBDNote(t *testing.T) {
	f := makeTempFile(t, "note body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	cmd := &noteKong{ID: "at-1", File: f}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"note", "at-1", "--file=" + f})
}

// TestCloseKong_FilePrecedenceOverReason verifies --file overrides --reason
// (preserved from legacy closeKong behaviour).
func TestCloseKong_FilePrecedenceOverReason(t *testing.T) {
	content := "reason from file"
	f := makeTempFile(t, content)
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	cmd := &closeKong{ID: "at-5", Reason: "inline reason", File: f}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"close", "at-5", "--reason=" + content})
}

// TestLearnKong_AppliesFreshPrefix verifies learnKong uses the learnKey helper
// to prepend the fresh: tier prefix for bare slugs.
func TestLearnKong_AppliesFreshPrefix(t *testing.T) {
	f := makeTempFile(t, "learning content")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	cmd := &learnKong{Role: "planner", Slug: "tip", File: f}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := (*calls)[0].args[1]; got != "--key=planner:fresh:tip" {
		t.Errorf("key = %q, want --key=planner:fresh:tip", got)
	}
}

// TestForgetKong_KeyFormed verifies forgetKong concatenates role:slug.
func TestForgetKong_KeyFormed(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	cmd := &forgetKong{Role: "dri", Slug: "hot:old-item"}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"forget", "dri:hot:old-item"})
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

// ── forget ────────────────────────────────────────────────────────────────────

func TestForget_ColdKeyFormed(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "✓ Deleted dri:stale-slug"}})
	err := (&forgetKong{Role: "dri", Slug: "stale-slug"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"forget", "dri:stale-slug"})
}

func TestForget_HotKeyFormed(t *testing.T) {
	// Callers pass slug as "hot:<name>" to target the hot-tier key.
	ctx, calls := newCtx(t, []fakeResp{{stdout: "✓ Deleted dri:hot:hot-item"}})
	err := (&forgetKong{Role: "dri", Slug: "hot:hot-item"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"forget", "dri:hot:hot-item"})
}

func TestForget_MissingRole(t *testing.T) {
	// Role is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"forget"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <role>")
	}
}

func TestForget_MissingSlug(t *testing.T) {
	// Slug is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"forget", "dri"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <slug>")
	}
}

func TestForget_NilContext(t *testing.T) {
	err := (&forgetKong{Role: "dri", Slug: "slug"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestForget_ForwardsBDOutput(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "✓ Deleted dri:foo"}})
	if err := (&forgetKong{Role: "dri", Slug: "foo"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.TrimSpace(stdoutOf(ctx))
	if !strings.Contains(got, "✓ Deleted dri:foo") {
		t.Errorf("stdout = %q, want bd forget output", got)
	}
}

// ── condense ──────────────────────────────────────────────────────────────────

// condensePacketFor runs condenseKong with a fakeBD returning the given memories
// map and parses the JSON packet from stdout.
func condensePacketFor(t *testing.T, role string, memories map[string]any) condensePacket {
	t.Helper()
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = memories
			return nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	if err := (&condenseKong{Role: role}).Run(ctx); err != nil {
		t.Fatalf("condense.Run: %v", err)
	}
	var pkt condensePacket
	if err := json.NewDecoder(stdout).Decode(&pkt); err != nil {
		t.Fatalf("packet JSON decode: %v (raw: %q)", err, stdout.String())
	}
	return pkt
}

func TestCondense_PacketContainsAllRoleMemories(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:alpha":      "body alpha",
		"dri:beta":       "body beta",
		"dri:hot:gamma":  "body gamma (hot)",
		"planner:other":  "should not appear",
		"schema_version": 1,
	})

	if len(pkt.Memories) != 3 {
		t.Fatalf("expected 3 memories (both tiers, dri: prefix only), got %d: %+v", len(pkt.Memories), pkt.Memories)
	}
	keys := make(map[string]string, len(pkt.Memories))
	for _, m := range pkt.Memories {
		keys[m.Key] = m.Body
	}
	if keys["dri:alpha"] != "body alpha" {
		t.Errorf("dri:alpha body = %q, want %q", keys["dri:alpha"], "body alpha")
	}
	if keys["dri:beta"] != "body beta" {
		t.Errorf("dri:beta body = %q, want %q", keys["dri:beta"], "body beta")
	}
	if keys["dri:hot:gamma"] != "body gamma (hot)" {
		t.Errorf("dri:hot:gamma body = %q, want %q", keys["dri:hot:gamma"], "body gamma (hot)")
	}
	if _, ok := keys["planner:other"]; ok {
		t.Error("planner:other must not appear in dri condense packet")
	}
}

func TestCondense_PacketContainsBudget(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:one": "body",
	})
	if pkt.HotBudget != condenseBudgetTokens {
		t.Errorf("HotBudget = %d, want %d", pkt.HotBudget, condenseBudgetTokens)
	}
}

func TestCondense_PacketContainsContract(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:one": "body",
	})
	if pkt.Contract == "" {
		t.Fatal("instruction_contract must not be empty")
	}
	// Contract must mention the key verbs the consuming agent uses.
	for _, want := range []string{"ateam learn", "ateam forget", "PROMOTE", "DEMOTE", "EVICT"} {
		if !strings.Contains(pkt.Contract, want) {
			t.Errorf("contract missing %q", want)
		}
	}
}

func TestCondense_ZeroWritesOccur(t *testing.T) {
	var calls []string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			calls = append(calls, args[0])
			return "", nil
		},
		runJSONFn: func(dst any, args ...string) error {
			m := dst.(*map[string]any)
			*m = map[string]any{"dri:foo": "body"}
			return nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	if err := (&condenseKong{Role: "dri"}).Run(ctx); err != nil {
		t.Fatalf("condense.Run: %v", err)
	}
	for _, c := range calls {
		if c == "remember" || c == "forget" {
			t.Errorf("condense issued a write call %q — must be zero-write", c)
		}
	}
}

func TestCondense_MemoriesSorted(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:zzz": "last",
		"dri:aaa": "first",
		"dri:mmm": "middle",
	})
	if len(pkt.Memories) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(pkt.Memories))
	}
	if pkt.Memories[0].Key != "dri:aaa" || pkt.Memories[1].Key != "dri:mmm" || pkt.Memories[2].Key != "dri:zzz" {
		t.Errorf("memories not sorted: %v", pkt.Memories)
	}
}

func TestCondense_MissingRole(t *testing.T) {
	// Role is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"condense"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <role>")
	}
}

func TestCondense_NilContext(t *testing.T) {
	err := (&condenseKong{Role: "dri"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestCondense_EmptyRoleSet(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"planner:something": "other role",
	})
	if len(pkt.Memories) != 0 {
		t.Errorf("expected 0 memories for empty role set, got %d", len(pkt.Memories))
	}
}

func TestCondense_SchemaVersionExcluded(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"schema_version": 1,
		"dri:real":       "real body",
	})
	for _, m := range pkt.Memories {
		if m.Key == "schema_version" {
			t.Error("schema_version must not appear in condense packet")
		}
	}
	if len(pkt.Memories) != 1 || pkt.Memories[0].Key != "dri:real" {
		t.Errorf("expected only dri:real, got: %+v", pkt.Memories)
	}
}

func TestCondense_RoleInPacket(t *testing.T) {
	pkt := condensePacketFor(t, "implementer", map[string]any{
		"implementer:foo": "body",
	})
	if pkt.Role != "implementer" {
		t.Errorf("packet Role = %q, want %q", pkt.Role, "implementer")
	}
}
