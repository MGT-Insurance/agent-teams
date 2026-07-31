package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// ── gate notify (agent-teams-tlx7) ────────────────────────────────────────────

// TestGate_NotifyFiredWithGateNote confirms that after labels are set the notify
// hook is called exactly once, with the same id and file as the gate.
func TestGate_NotifyFiredWithGateNote(t *testing.T) {
	f := makeTempFile(t, "should we proceed?")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})

	type notifyCall struct{ id, file string }
	var got []notifyCall
	cmd := &gateKong{
		ID:   "at-5",
		File: f,
		Kind: "question",
		notify: func(ctx *cli.Context, id, file string) error {
			got = append(got, notifyCall{id, file})
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 notify call, got %d", len(got))
	}
	if got[0].id != "at-5" {
		t.Errorf("notify id = %q, want at-5", got[0].id)
	}
	if got[0].file != f {
		t.Errorf("notify file = %q, want %q", got[0].file, f)
	}
}

// TestGate_NotifyFailureIsNonFatal confirms that a notify error does not cause
// gate to fail — labels are already set; the phone ping is best-effort only.
// This exercises the Enabled=true + Send-fails branch (warning surfaced, non-fatal).
func TestGate_NotifyFailureIsNonFatal(t *testing.T) {
	f := makeTempFile(t, "question body")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	errBuf := ctx.Stderr.(*bytes.Buffer)

	cmd := &gateKong{
		ID:   "at-5",
		File: f,
		Kind: "question",
		notify: func(ctx *cli.Context, id, file string) error {
			return fmt.Errorf("send failed: connection refused")
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("gate must succeed even when notify fails, got: %v", err)
	}
	// Labels were still set.
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
	// Warning emitted on stderr.
	if !strings.Contains(errBuf.String(), "warning") {
		t.Errorf("expected warning on stderr, got: %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "send failed") {
		t.Errorf("stderr missing send error; got: %q", errBuf.String())
	}
}

// TestGate_NilNotifySkipped confirms that a nil notify func is a no-op (zero-
// value gateCmd, used in tests without transport).
func TestGate_NilNotifySkipped(t *testing.T) {
	f := makeTempFile(t, "question")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	if err := (&gateKong{ID: "at-5", File: f, Kind: "question", notify: nil}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exactly 3 bd calls — no extra calls from notify.
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls, got %d", len(*calls))
	}
}

// ── gate: structured-ask notify body (agent-teams-lbxl) ──────────────────────

// TestGate_StructuredAsk_NotifyGetsHumanReadable confirms that when a
// structured-ask gate fires, the notify hook receives the human-readable
// body (decision / Recommended / Alternative) and NOT the sentinel block
// marker.
func TestGate_StructuredAsk_NotifyGetsHumanReadable(t *testing.T) {
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})

	type notifyCall struct{ id, file string }
	var got []notifyCall
	cmd := &gateKong{
		ID:             "at-lbxl1",
		Decision:       "Should we ship feature X?",
		Recommendation: "Yes, ship it",
		Alternative:    "Wait for next cycle",
		Kind:           "question",
		notify: func(ctx *cli.Context, id, file string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Errorf("notify: ReadFile(%q) failed: %v", file, err)
				return nil
			}
			got = append(got, notifyCall{id, string(data)})
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 notify call, got %d", len(got))
	}
	if got[0].id != "at-lbxl1" {
		t.Errorf("notify id = %q, want at-lbxl1", got[0].id)
	}
	body := got[0].file
	if !strings.Contains(body, "Should we ship feature X?") {
		t.Errorf("notify body missing decision; got: %q", body)
	}
	if !strings.Contains(body, "Recommended: Yes, ship it") {
		t.Errorf("notify body missing Recommended:; got: %q", body)
	}
	if !strings.Contains(body, "Alternative: Wait for next cycle") {
		t.Errorf("notify body missing Alternative:; got: %q", body)
	}
	// Must NOT contain the sentinel marker.
	if strings.Contains(body, "<<<ateam-ask") {
		t.Errorf("notify body must not contain sentinel marker; got: %q", body)
	}
}

// TestGate_StructuredAsk_NotifyGetsContextInBody confirms that the context-file
// content appears in the human-readable notify body.
func TestGate_StructuredAsk_NotifyGetsContextInBody(t *testing.T) {
	contextFile := makeTempFile(t, "some extra context here")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})

	var notifyBody string
	cmd := &gateKong{
		ID:             "at-lbxl2",
		Decision:       "Go or no-go?",
		Recommendation: "Go",
		Alternative:    "No-go",
		ContextFile:    contextFile,
		Kind:           "question",
		notify: func(ctx *cli.Context, id, file string) error {
			data, _ := os.ReadFile(file)
			notifyBody = string(data)
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(notifyBody, "Context: some extra context here") {
		t.Errorf("notify body missing context; got: %q", notifyBody)
	}
}

// TestGate_PlainFile_NotifyGetsFileContent confirms that a plain --file gate
// sends the original file content to notify (unchanged).
func TestGate_PlainFile_NotifyGetsFileContent(t *testing.T) {
	f := makeTempFile(t, "plain gate question body")
	ctx, _ := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})

	var notifyFile string
	cmd := &gateKong{
		ID:   "at-lbxl3",
		File: f,
		Kind: "question",
		notify: func(ctx *cli.Context, id, file string) error {
			notifyFile = file
			return nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// For plain --file gates, notify receives the original file path unchanged.
	if notifyFile != f {
		t.Errorf("plain gate: notify file = %q, want %q", notifyFile, f)
	}
}

// TestGate_StructuredAsk_BdNoteStillGetsSentinelBlock confirms that the bead
// note call still receives the sentinel block even when notify is set.
// The notify body is separate; the bd record must stay parseable.
func TestGate_StructuredAsk_BdNoteStillGetsSentinelBlock(t *testing.T) {
	var capturedNoteContent string
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
					capturedNoteContent = string(data)
				}
			}
		}
		idx++
		return []byte("ok"), nil, nil
	}
	client := bd.NewClientWithExec(t.TempDir(), execFn)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	cmd := &gateKong{
		ID:             "at-lbxl4",
		Decision:       "Proceed with refactor?",
		Recommendation: "Yes",
		Alternative:    "Defer",
		Kind:           "question",
		notify:         func(ctx *cli.Context, id, file string) error { return nil },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// The bd note must still contain the sentinel block.
	if !strings.Contains(capturedNoteContent, "<<<ateam-ask") {
		t.Errorf("bd note missing sentinel marker; got: %q", capturedNoteContent)
	}
	if !strings.Contains(capturedNoteContent, "decision: Proceed with refactor?") {
		t.Errorf("bd note missing decision field; got: %q", capturedNoteContent)
	}
}

// ── clear-gate ────────────────────────────────────────────────────────────────

func TestClearGate_WithFile(t *testing.T) {
	// 5 calls: comment, label remove human, label remove gate:review,
	// label remove gate:question, label remove external-review
	f := makeTempFile(t, "response")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateKong{ID: "at-3", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 5 {
		t.Fatalf("expected 5 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"comment", "at-3", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "human"})
	assertArgs(t, *calls, 2, []string{"label", "remove", "at-3", "gate:review"})
	assertArgs(t, *calls, 3, []string{"label", "remove", "at-3", "gate:question"})
	assertArgs(t, *calls, 4, []string{"label", "remove", "at-3", "external-review"})
}

func TestClearGate_WithoutFile(t *testing.T) {
	// 4 calls: label remove human, label remove gate:review, label remove
	// gate:question, label remove external-review
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateKong{ID: "at-3"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 4 {
		t.Fatalf("expected 4 bd calls, got %d", len(*calls))
	}
	assertArgs(t, *calls, 0, []string{"label", "remove", "at-3", "human"})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "gate:review"})
	assertArgs(t, *calls, 2, []string{"label", "remove", "at-3", "gate:question"})
	assertArgs(t, *calls, 3, []string{"label", "remove", "at-3", "external-review"})
}

func TestClearGate_EqualsForm(t *testing.T) {
	f := makeTempFile(t, "response")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}})
	err := (&clearGateKong{ID: "at-3", File: f}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 0, []string{"comment", "at-3", "--file=" + f})
	assertArgs(t, *calls, 1, []string{"label", "remove", "at-3", "human"})
	assertArgs(t, *calls, 2, []string{"label", "remove", "at-3", "gate:review"})
	assertArgs(t, *calls, 3, []string{"label", "remove", "at-3", "gate:question"})
	assertArgs(t, *calls, 4, []string{"label", "remove", "at-3", "external-review"})
}

// TestClearGate_ExternalReviewAbsentIsNonFatal confirms that removing
// external-review when it was never set (the common case — most clear-gate
// calls are on R, not H, per external_review.go §9) still succeeds, matching
// how the three pre-existing removals already tolerate absence.
func TestClearGate_ExternalReviewAbsentIsNonFatal(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}, {stdout: "ok"}, {stdout: "ok"}, {stdout: ""}})
	err := (&clearGateKong{ID: "at-3"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, *calls, 3, []string{"label", "remove", "at-3", "external-review"})
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
		{stdout: "ok"},            // clear-gate: label remove external-review
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
	if len(*calls) != 9 {
		t.Fatalf("expected 9 bd calls, got %d: %v", len(*calls), *calls)
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
	// call 8: label remove external-review
	assertArgs(t, *calls, 8, []string{"label", "remove", "at-round1", "external-review"})
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

// ── learn: write-time byte caps (agent-teams-b2xr.3, contract .2) ─────────────

func TestLearn_RejectsOverCap_Hot(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 901))
	ctx, calls := newCtx(t, nil)
	err := (&learnKong{Role: "implementer", Slug: "hot:foo", File: f}).Run(ctx)
	assertUsageError(t, err, "hot tier cap is 900 bytes, got 901")
	if len(*calls) != 0 {
		t.Fatalf("expected no bd remember call on rejection, got %d calls: %v", len(*calls), *calls)
	}
}

func TestLearn_RejectsOverCap_FreshExplicitPrefix(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 901))
	ctx, calls := newCtx(t, nil)
	err := (&learnKong{Role: "implementer", Slug: "fresh:foo", File: f}).Run(ctx)
	assertUsageError(t, err, "fresh tier cap is 900 bytes, got 901")
	if len(*calls) != 0 {
		t.Fatalf("expected no bd remember call on rejection, got %d calls: %v", len(*calls), *calls)
	}
}

func TestLearn_RejectsOverCap_FreshBareSlug(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 901))
	ctx, calls := newCtx(t, nil)
	err := (&learnKong{Role: "implementer", Slug: "foo", File: f}).Run(ctx)
	assertUsageError(t, err, "fresh tier cap is 900 bytes, got 901")
	if len(*calls) != 0 {
		t.Fatalf("expected no bd remember call on rejection, got %d calls: %v", len(*calls), *calls)
	}
}

func TestLearn_RejectsOverCap_Cold(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 1501))
	ctx, calls := newCtx(t, nil)
	err := (&learnKong{Role: "implementer", Slug: "cold:foo", File: f}).Run(ctx)
	assertUsageError(t, err, "cold tier cap is 1500 bytes, got 1501")
	if len(*calls) != 0 {
		t.Fatalf("expected no bd remember call on rejection, got %d calls: %v", len(*calls), *calls)
	}
}

func TestLearn_RejectionMessage_TeachesShape(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 901))
	ctx, _ := newCtx(t, nil)
	err := (&learnKong{Role: "implementer", Slug: "hot:foo", File: f}).Run(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{
		"RULE", "TRIGGER", "APPLY", "PROVENANCE",
		"bare initiative-id parenthetical",
		"linked bd issue",
		"Store the learning itself, not the story of how it was found — include only enough context to signal WHEN the learning is relevant, not a history lesson.",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q; got: %s", want, msg)
		}
	}
}

func TestLearn_AcceptsUnderCap_Hot(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 900))
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "hot:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd remember call, got %d", len(*calls))
	}
}

func TestLearn_AcceptsUnderCap_Fresh(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 900))
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "fresh:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd remember call, got %d", len(*calls))
	}
}

func TestLearn_AcceptsUnderCap_Cold(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 1500))
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "cold:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd remember call, got %d", len(*calls))
	}
}

func TestLearn_ColdRegression_2000BytesNowRejected(t *testing.T) {
	// Previously cold had no write-time cap at all; this now must be rejected.
	f := makeTempFile(t, strings.Repeat("x", 2000))
	ctx, calls := newCtx(t, nil)
	err := (&learnKong{Role: "implementer", Slug: "cold:foo", File: f}).Run(ctx)
	assertUsageError(t, err, "cold tier cap is 1500 bytes, got 2000")
	if len(*calls) != 0 {
		t.Fatalf("expected no bd remember call on rejection, got %d calls: %v", len(*calls), *calls)
	}
}

func TestLearn_ColdBetween900And1500_StillPasses(t *testing.T) {
	f := makeTempFile(t, strings.Repeat("x", 1200))
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "cold:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd remember call, got %d", len(*calls))
	}
}

func TestLearn_TrimsTrailingNewlineBeforeMeasuring(t *testing.T) {
	// 900 bytes of content plus a trailing newline must still be accepted —
	// the trailing newline must not eat the byte budget (mirrors
	// buildAskBlock's TrimRight behavior).
	trimmed := strings.Repeat("x", 900)
	f := makeTempFile(t, trimmed+"\n")
	ctx, calls := newCtx(t, []fakeResp{{stdout: "ok"}})
	if err := (&learnKong{Role: "implementer", Slug: "hot:foo", File: f}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd remember call, got %d", len(*calls))
	}
	// The stored body must be the trimmed content, not the raw file bytes —
	// the trailing newline must not be persisted either.
	if got := (*calls)[0].args[2]; got != trimmed {
		t.Errorf("stored body = %d bytes, want trimmed %d-byte body without trailing newline", len(got), len(trimmed))
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

// ── close: update-local-main wiring (agent-teams-q564.1) ────────────────────
//
// These use fakeBD/makeCtx (defined in dispatch_test.go, shared with
// resume_test.go) rather than newCtx/fakeExec, because closeKong.Run now
// issues a second bd call (bd.ShowIssue -> "show <id> --json") in addition to
// the "close" call, and fakeBD lets us branch the fake response on args[0]
// instead of relying on fakeExec's fixed response-per-index sequencing.

// TestCloseKong_UpdateLocalMain_RepoPathPresent verifies that when the closed
// issue's description has a repo: line, the injected runUpdateLocalMain fake
// is invoked with that path and its output is forwarded to stdout.
func TestCloseKong_UpdateLocalMain_RepoPathPresent(t *testing.T) {
	var gotRepo string
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			if len(args) > 0 && args[0] == "show" {
				issues := []bd.Issue{{ID: "at-5", Description: "repo: /some/repo\n"}}
				raw, _ := json.Marshal(issues)
				return string(raw), nil
			}
			return "ok", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &closeKong{
		ID: "at-5",
		runUpdateLocalMain: func(repoPath string) (string, error) {
			gotRepo = repoPath
			return "update-local-main: already up to date (/some/repo main)\n", nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRepo != "/some/repo" {
		t.Errorf("runUpdateLocalMain called with repo %q, want /some/repo", gotRepo)
	}
	if !strings.Contains(stdout.String(), "update-local-main: already up to date") {
		t.Errorf("stdout = %q, want update-local-main output forwarded", stdout.String())
	}
}

// TestCloseKong_UpdateLocalMain_RepoPathAbsent verifies close still succeeds
// and the injected fake is never invoked when the issue description has no
// repo: (or worktree:) line.
func TestCloseKong_UpdateLocalMain_RepoPathAbsent(t *testing.T) {
	called := false
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			if len(args) > 0 && args[0] == "show" {
				issues := []bd.Issue{{ID: "at-5", Description: "problem: no repo line here\n"}}
				raw, _ := json.Marshal(issues)
				return string(raw), nil
			}
			return "ok", nil
		},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	cmd := &closeKong{
		ID: "at-5",
		runUpdateLocalMain: func(repoPath string) (string, error) {
			called = true
			return "", nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("runUpdateLocalMain should not be invoked when no repo: line is present")
	}
}

// TestCloseKong_UpdateLocalMain_ScriptErrorIsFailSoft verifies that a script
// error (simulating script-not-found / exec failure) is swallowed: close
// still succeeds, and a one-line warning appears in stdout instead of an
// error propagating.
func TestCloseKong_UpdateLocalMain_ScriptErrorIsFailSoft(t *testing.T) {
	fbd := &fakeBD{
		runFn: func(args ...string) (string, error) {
			if len(args) > 0 && args[0] == "show" {
				issues := []bd.Issue{{ID: "at-5", Description: "repo: /some/repo\n"}}
				raw, _ := json.Marshal(issues)
				return string(raw), nil
			}
			return "ok", nil
		},
	}
	ctx, stdout, _ := makeCtx(fbd, t.TempDir())
	cmd := &closeKong{
		ID: "at-5",
		runUpdateLocalMain: func(repoPath string) (string, error) {
			return "", fmt.Errorf("update-local-main.sh not found")
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected close to succeed despite script error, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "update-local-main: skipped") {
		t.Errorf("stdout = %q, want a one-line skip warning", stdout.String())
	}
}

// TestRunUpdateLocalMainScript_NotFoundIsFailSoft exercises the stat-guard in
// runUpdateLocalMainScript's production implementation: os.Executable()
// resolves to the running test binary, which has no sibling
// hooks/scripts/update-local-main.sh, so the "not found" error path fires.
// This proves the fail-soft guard fires; it deliberately does not exercise
// real script behaviour (see live verification for that).
func TestRunUpdateLocalMainScript_NotFoundIsFailSoft(t *testing.T) {
	_, err := runUpdateLocalMainScript(t.TempDir())
	if err == nil {
		t.Fatal("expected error: no hooks/scripts/update-local-main.sh sibling to the test binary")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
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

// ── applied ───────────────────────────────────────────────────────────────────

func TestApplied_FirstCallCreatesCountOne(t *testing.T) {
	ctx, calls := newCtx(t, []fakeResp{{stdout: "{}"}, {stdout: "ok"}})
	err := (&appliedKong{Role: "planner", Slug: "foo"}).Run(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 bd calls, got %d: %+v", len(*calls), *calls)
	}
	assertArgs(t, *calls, 0, []string{"memories", "--json"})
	call := (*calls)[1]
	if call.args[0] != "remember" {
		t.Fatalf("args[0] = %q, want remember", call.args[0])
	}
	if call.args[1] != "--key=applied:planner:foo" {
		t.Fatalf("args[1] = %q, want --key=applied:planner:foo", call.args[1])
	}
	var rec appliedRecord
	if err := json.Unmarshal([]byte(call.args[2]), &rec); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, call.args[2])
	}
	if rec.Count != 1 {
		t.Errorf("Count = %d, want 1", rec.Count)
	}
	if _, err := time.Parse(time.RFC3339, rec.LastApplied); err != nil {
		t.Errorf("LastApplied = %q, not valid RFC3339: %v", rec.LastApplied, err)
	}
}

func TestApplied_SecondCallIncrementsAndUpdatesTimestamp(t *testing.T) {
	existingBody := `{"count":1,"last_applied":"2020-01-01T00:00:00Z"}`
	raw, err := json.Marshal(map[string]any{"applied:planner:foo": existingBody})
	if err != nil {
		t.Fatal(err)
	}
	ctx, calls := newCtx(t, []fakeResp{{stdout: string(raw)}, {stdout: "ok"}})
	if err := (&appliedKong{Role: "planner", Slug: "foo"}).Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := (*calls)[1]
	if call.args[1] != "--key=applied:planner:foo" {
		t.Fatalf("args[1] = %q, want --key=applied:planner:foo", call.args[1])
	}
	var rec appliedRecord
	if err := json.Unmarshal([]byte(call.args[2]), &rec); err != nil {
		t.Fatalf("body not valid JSON: %v (%q)", err, call.args[2])
	}
	if rec.Count != 2 {
		t.Errorf("Count = %d, want 2", rec.Count)
	}
	if rec.LastApplied == "2020-01-01T00:00:00Z" {
		t.Error("LastApplied was not updated on the second call")
	}
	if _, err := time.Parse(time.RFC3339, rec.LastApplied); err != nil {
		t.Errorf("LastApplied = %q, not valid RFC3339: %v", rec.LastApplied, err)
	}
}

func TestApplied_NilContext(t *testing.T) {
	err := (&appliedKong{Role: "planner", Slug: "foo"}).Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestApplied_MissingRole(t *testing.T) {
	// Role is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"applied"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <role>")
	}
}

func TestApplied_MissingSlug(t *testing.T) {
	// Slug is a required positional; enforced at parse time.
	p, err := cli.NewParser()
	if err != nil {
		t.Fatal(err)
	}
	RegisterWriteKong(p)
	_, parseErr := p.Parse([]string{"applied", "planner"})
	if parseErr == nil {
		t.Fatal("expected parse error for missing <slug>")
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
	// Emitted Key is role-relative (the "dri:" prefix is stripped, since
	// every entry in this packet already shares pkt.Role — see
	// condenseMemory's doc comment).
	byKey := make(map[string]condenseMemory, len(pkt.Memories))
	for _, m := range pkt.Memories {
		byKey[m.Key] = m
	}
	// Bare keys (alpha, beta) are cold: summary-only, body elided.
	if alpha := byKey["alpha"]; alpha.Tier() != "cold" || alpha.Body != "" || alpha.Summary != "body alpha" {
		t.Errorf("alpha = %+v, want tier=cold body=\"\" summary=%q", alpha, "body alpha")
	}
	if beta := byKey["beta"]; beta.Tier() != "cold" || beta.Body != "" || beta.Summary != "body beta" {
		t.Errorf("beta = %+v, want tier=cold body=\"\" summary=%q", beta, "body beta")
	}
	// Hot key keeps full body, no summary.
	if gamma := byKey["hot:gamma"]; gamma.Tier() != "hot" || gamma.Body != "body gamma (hot)" || gamma.Summary != "" {
		t.Errorf("hot:gamma = %+v, want tier=hot body=%q summary=\"\"", gamma, "body gamma (hot)")
	}
	for _, k := range []string{"planner:other", "dri:alpha", "dri:beta", "dri:hot:gamma"} {
		if _, ok := byKey[k]; ok {
			t.Errorf("packet must not emit role-prefixed key %q", k)
		}
	}
}

// TestCondense_ColdSummaryOnlyNoBody proves the acceptance criterion: cold
// entries carry a summary and no body.
func TestCondense_ColdSummaryOnlyNoBody(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:settled": "first line of settled cold body\nsecond line, elided",
	})
	if len(pkt.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(pkt.Memories))
	}
	m := pkt.Memories[0]
	if m.Tier() != "cold" {
		t.Errorf("Tier = %q, want cold", m.Tier())
	}
	if m.Body != "" {
		t.Errorf("Body = %q, want empty (cold body must be elided)", m.Body)
	}
	if m.Summary != "first line of settled cold body" {
		t.Errorf("Summary = %q, want first-line-only", m.Summary)
	}
}

// TestCondense_HotAndFreshFullBody proves the acceptance criterion: hot and
// fresh entries carry full body (and no summary).
func TestCondense_HotAndFreshFullBody(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:hot:h1":   "full hot body, multi\nline, preserved verbatim",
		"dri:fresh:f1": "full fresh body, multi\nline, preserved verbatim",
	})
	byKey := make(map[string]condenseMemory, len(pkt.Memories))
	for _, m := range pkt.Memories {
		byKey[m.Key] = m
	}
	hot := byKey["hot:h1"]
	if hot.Tier() != "hot" || hot.Body != "full hot body, multi\nline, preserved verbatim" || hot.Summary != "" {
		t.Errorf("hot entry = %+v, want full body, no summary", hot)
	}
	fresh := byKey["fresh:f1"]
	if fresh.Tier() != "fresh" || fresh.Body != "full fresh body, multi\nline, preserved verbatim" || fresh.Summary != "" {
		t.Errorf("fresh entry = %+v, want full body, no summary", fresh)
	}
}

// TestCondense_ColdSummaryTruncatedTo120Chars verifies condenseSummary
// truncates a long first line to condenseColdSummaryMaxChars runes with an
// ellipsis marker, rather than shipping an unbounded first line.
func TestCondense_ColdSummaryTruncatedTo120Chars(t *testing.T) {
	longLine := strings.Repeat("a", 200)
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:long": longLine,
	})
	if len(pkt.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(pkt.Memories))
	}
	summary := pkt.Memories[0].Summary
	runes := []rune(summary)
	if len(runes) != condenseColdSummaryMaxChars+1 { // +1 for the ellipsis rune
		t.Errorf("summary length = %d runes, want %d (%d chars + ellipsis)", len(runes), condenseColdSummaryMaxChars+1, condenseColdSummaryMaxChars)
	}
	if !strings.HasSuffix(summary, "…") {
		t.Errorf("truncated summary must end with ellipsis, got %q", summary)
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

// TestCondense_ContractWarnsRecallSubstringMiss proves the contract
// (agent-teams-0yd3.18) tells the consuming agent: pass the entry's own
// "key" verbatim as the recall term, that recall is a literal substring
// match (not a word search), and that a miss is silent (prints nothing,
// exits 0) so empty output must never be read as "no body exists".
func TestCondense_ContractWarnsRecallSubstringMiss(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:one": "body",
	})
	for _, want := range []string{
		"verbatim as <term>",
		"SUBSTRING",
		"NOT a word/phrase search",
		"exits 0",
		"NEVER evidence the entry lacks a body",
	} {
		if !strings.Contains(pkt.Contract, want) {
			t.Errorf("contract missing %q", want)
		}
	}
}

// TestCondense_ContractColdWriteUsesColdPrefix proves the contract
// (agent-teams-y1yr item 1) never instructs a verbatim `ateam learn` write
// for a bare cold key: a bare slug defaults to the fresh tier (learnKey,
// write.go), so writing it verbatim would leave the stale cold entry in
// place and duplicate it into fresh — re-arming the fresh-tier condense
// trigger the curation pass exists to clear. The contract must instead say
// to prepend "cold:" for a cold rewrite, and must name the learn/forget
// asymmetry (same bare key: learn defaults to FRESH, forget targets COLD).
func TestCondense_ContractColdWriteUsesColdPrefix(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:one": "body",
	})
	for _, want := range []string{
		"cold:<key>",
		"safe ONLY for hot and fresh",
		"FRESH",
		"COLD",
	} {
		if !strings.Contains(pkt.Contract, want) {
			t.Errorf("contract missing %q", want)
		}
	}
}

// TestCondense_ContractDropsBdMemories proves the contract (agent-teams-y1yr
// item 2) no longer offers `bd memories <keyword>` as a second body-read
// path. Measured against a real entry, bd memories truncates BELOW the
// packet's own summary while returning a plausible, correctly-matched,
// non-empty result — a worse failure mode than recall's silent-empty miss,
// because there is no signal the agent is deciding on a truncated body.
// ateam recall (guaranteed to match on the entry's own key) is the sole
// retrieval path this contract offers.
func TestCondense_ContractDropsBdMemories(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:one": "body",
	})
	if strings.Contains(pkt.Contract, "bd memories") {
		t.Errorf("contract must not offer bd memories as a retrieval path — it truncates below the packet summary")
	}
	if !strings.Contains(pkt.Contract, "ONLY retrieval path") {
		t.Errorf("contract must state ateam recall is the only retrieval path")
	}
}

// TestCondense_ContractBudgetPointsAtPacketField proves the contract
// (agent-teams-y1yr item 3) points the agent at the packet's own
// "hot_budget_tokens" field for the hot-tier budget instead of restating
// the number as literal prose — the Sprintf backing this string does not
// interpolate condenseBudgetTokens, so a hardcoded number goes stale
// silently the moment the constant changes.
func TestCondense_ContractBudgetPointsAtPacketField(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:one": "body",
	})
	if !strings.Contains(pkt.Contract, "hot_budget_tokens") {
		t.Errorf("contract must reference the packet's hot_budget_tokens field")
	}
	if strings.Contains(pkt.Contract, "6000") {
		t.Errorf("contract must not hardcode the budget value")
	}
	if !strings.Contains(pkt.Contract, "15-25 succinct learnings") {
		t.Errorf("contract must keep the shape guidance, which is not a duplicated constant")
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
	if pkt.Memories[0].Key != "aaa" || pkt.Memories[1].Key != "mmm" || pkt.Memories[2].Key != "zzz" {
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
	if len(pkt.Memories) != 1 || pkt.Memories[0].Key != "real" {
		t.Errorf("expected only real, got: %+v", pkt.Memories)
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

func TestCondense_JoinsAppliedCount(t *testing.T) {
	pkt := condensePacketFor(t, "dri", map[string]any{
		"dri:foo":         "body foo",
		"dri:hot:baz":     "body baz (hot)",
		"dri:bar":         "body bar (no applied sibling)",
		"applied:dri:foo": `{"count":5,"last_applied":"2024-01-01T00:00:00Z"}`,
		"applied:dri:baz": `{"count":2,"last_applied":"2024-02-02T00:00:00Z"}`,
	})

	byKey := make(map[string]condenseMemory, len(pkt.Memories))
	for _, m := range pkt.Memories {
		byKey[m.Key] = m
	}

	if foo := byKey["foo"]; foo.AppliedCount != 5 || foo.LastApplied != "2024-01-01T00:00:00Z" {
		t.Errorf("foo applied join = %+v, want count=5 last_applied=2024-01-01T00:00:00Z", foo)
	}

	// hot:baz's sibling is keyed on the BARE slug (applied:dri:baz, not
	// applied:dri:hot:baz) — the applied counter is tier-independent.
	if baz := byKey["hot:baz"]; baz.AppliedCount != 2 || baz.LastApplied != "2024-02-02T00:00:00Z" {
		t.Errorf("hot:baz applied join = %+v, want count=2 (tier-stripped slug lookup)", baz)
	}

	if bar := byKey["bar"]; bar.AppliedCount != 0 || bar.LastApplied != "" {
		t.Errorf("bar applied join = %+v, want zero value (no sibling)", bar)
	}

	// The applied: keys live under a top-level "applied:" prefix, not
	// "<role>:" — they must not leak into the memories list themselves.
	if _, ok := byKey["applied:dri:foo"]; ok {
		t.Error("applied:dri:foo must not appear as its own condense memory")
	}
}

// ── register epic creation ─────────────────────────────────────────────────────

// TestRegister_WithRepoLine_CreatesEpicAndAppends verifies that when the body
// file contains a "repo: <path>" line, registerKong calls createEpic with that
// path and appends "epic: <id>" to the body passed to bd create.
func TestRegister_WithRepoLine_CreatesEpicAndAppends(t *testing.T) {
	repoPath := t.TempDir()
	body := "problem: my feature\nrepo: " + repoPath + "\nworktree: /tmp/wt\n"
	bodyFile := makeTempFile(t, body)

	// Fake bd create response for the global initiative.
	issue := bd.Issue{ID: "at-init1", Title: "my feature"}
	jsonOut, _ := json.Marshal(issue)

	// Track the body file content passed to bd create.
	var capturedBodyContent string
	execFn, _ := fakeExec([]fakeResp{{stdout: string(jsonOut)}})
	wrappedExec := func(name string, args ...string) ([]byte, []byte, error) {
		// Intercept to capture body file content before delegating.
		for _, a := range args {
			if strings.HasPrefix(a, "--body-file=") {
				path := strings.TrimPrefix(a, "--body-file=")
				if b, err := os.ReadFile(path); err == nil {
					capturedBodyContent = string(b)
				}
			}
		}
		return execFn(name, args...)
	}
	client := bd.NewClientWithExec(t.TempDir(), wrappedExec)
	var stdout, stderr bytes.Buffer
	ctx := &cli.Context{Home: t.TempDir(), BD: client, Stdout: &stdout, Stderr: &stderr}

	// Inject fake epic creator that succeeds.
	cmd := &registerKong{
		Title: "my feature",
		File:  bodyFile,
		createEpic: func(gotRepo, gotTitle string) (string, error) {
			if gotRepo != repoPath {
				t.Errorf("createEpic repo = %q, want %q", gotRepo, repoPath)
			}
			if gotTitle != "my feature" {
				t.Errorf("createEpic title = %q, want %q", gotTitle, "my feature")
			}
			return "at-epic1", nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Printed id must be the initiative id.
	if got := strings.TrimSpace(stdout.String()); got != "at-init1" {
		t.Errorf("stdout = %q, want %q", got, "at-init1")
	}
	// The body passed to bd create must contain the epic: line.
	if !strings.Contains(capturedBodyContent, "epic: at-epic1") {
		t.Errorf("body passed to bd create missing 'epic: at-epic1':\n%s", capturedBodyContent)
	}
	// The repo: line must still be present.
	if !strings.Contains(capturedBodyContent, "repo: "+repoPath) {
		t.Errorf("body missing 'repo:' line:\n%s", capturedBodyContent)
	}
}

// TestRegister_EpicCreationFails_RegistrationSucceeds verifies fail-soft: when
// createEpic returns an error, registration still completes and returns the id.
func TestRegister_EpicCreationFails_RegistrationSucceeds(t *testing.T) {
	repoPath := t.TempDir()
	body := "problem: work\nrepo: " + repoPath + "\n"
	bodyFile := makeTempFile(t, body)

	issue := bd.Issue{ID: "at-failsoft", Title: "work"}
	jsonOut, _ := json.Marshal(issue)
	ctx, _ := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})

	cmd := &registerKong{
		Title: "work",
		File:  bodyFile,
		createEpic: func(_, _ string) (string, error) {
			return "", fmt.Errorf("bd: no beads db found")
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("expected success despite epic failure, got: %v", err)
	}
	if got := strings.TrimSpace(stdoutOf(ctx)); got != "at-failsoft" {
		t.Errorf("stdout = %q, want %q", got, "at-failsoft")
	}
	// Warning must be written to stderr.
	errOut := ctx.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(errOut, "fail-soft") {
		t.Errorf("stderr missing 'fail-soft' warning: %q", errOut)
	}
}

// TestRegister_NoRepoLine_SkipsEpicCreation verifies that when the body has no
// "repo:" or "worktree:" line, createEpic is never called.
func TestRegister_NoRepoLine_SkipsEpicCreation(t *testing.T) {
	bodyFile := makeTempFile(t, "initiative body with no repo line\n")
	issue := bd.Issue{ID: "at-norepo", Title: "T"}
	jsonOut, _ := json.Marshal(issue)
	ctx, calls := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})

	epicCalled := false
	cmd := &registerKong{
		Title: "T",
		File:  bodyFile,
		createEpic: func(_, _ string) (string, error) {
			epicCalled = true
			return "at-shouldnotbe", nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if epicCalled {
		t.Error("createEpic should not be called when body has no repo: line")
	}
	// Still exactly 1 bd call (the initiative create).
	if len(*calls) != 1 {
		t.Fatalf("expected 1 bd call, got %d", len(*calls))
	}
}

// TestRegister_WorktreeFallback_UsedWhenNoRepoLine verifies that when the body
// has no "repo:" line but does have a "worktree:" line, extractRepoPath returns
// the worktree path and epic creation is attempted with it.
func TestRegister_WorktreeFallback_UsedWhenNoRepoLine(t *testing.T) {
	wtPath := t.TempDir()
	body := "problem: work\nworktree: " + wtPath + "\n"
	bodyFile := makeTempFile(t, body)

	issue := bd.Issue{ID: "at-wt1", Title: "work"}
	jsonOut, _ := json.Marshal(issue)
	ctx, _ := newCtx(t, []fakeResp{{stdout: string(jsonOut)}})

	var gotRepo string
	cmd := &registerKong{
		Title: "work",
		File:  bodyFile,
		createEpic: func(repoPath, _ string) (string, error) {
			gotRepo = repoPath
			return "at-epic-wt", nil
		},
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotRepo != wtPath {
		t.Errorf("createEpic called with repo=%q, want worktree fallback %q", gotRepo, wtPath)
	}
}
