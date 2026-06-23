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
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// ── fakeTransport ─────────────────────────────────────────────────────────────

// fakeTransport records Send calls and returns a configured threadRef.
type fakeTransport struct {
	returnRef string
	returnErr error
	calls     []transport.OutboundMessage
}

func (f *fakeTransport) Name() string { return "fake" }

func (f *fakeTransport) Send(msg transport.OutboundMessage) (string, error) {
	f.calls = append(f.calls, msg)
	return f.returnRef, f.returnErr
}

func (f *fakeTransport) Receive(handler func(transport.Reply) error) error {
	return fmt.Errorf("fakeTransport.Receive: not implemented")
}

// ── fakeTransportFor ──────────────────────────────────────────────────────────

// fakeTransportFor returns ft or an error, for injection into notifyCmd.
func fakeTransportFor(ft *fakeTransport, err error) transportForFunc {
	return func(home string) (transport.Transport, error) {
		return ft, err
	}
}

// ── notifyFakeBD ──────────────────────────────────────────────────────────────

// notifyFakeBD is an injectable cli.BDRunner that responds to bd show and
// records label add calls. Named to avoid collision with dispatch_test.go's fakeBD.
type notifyFakeBD struct {
	// issue returned by bd show <id> --json
	issue bd.Issue
	// showErr, if non-nil, is returned by bd show
	showErr error
	// labelAddErr, if non-nil, is returned by bd label add
	labelAddErr error
	// labelsAdded records "id:label" pairs passed to bd label add
	labelsAdded []string
}

func (f *notifyFakeBD) Run(args ...string) (string, error) {
	// show <id> --json — used by bd.ShowIssue which calls r.Run directly
	if len(args) >= 3 && args[0] == "show" && args[2] == "--json" {
		if f.showErr != nil {
			return "", f.showErr
		}
		raw, err := json.Marshal([]bd.Issue{f.issue})
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	// label add <id> <label>
	if len(args) >= 4 && args[0] == "label" && args[1] == "add" {
		if f.labelAddErr != nil {
			return "", f.labelAddErr
		}
		f.labelsAdded = append(f.labelsAdded, args[2]+":"+args[3])
		return "", nil
	}
	return "", fmt.Errorf("notifyFakeBD: unexpected Run(%v)", args)
}

func (f *notifyFakeBD) RunJSON(dst any, args ...string) error {
	return fmt.Errorf("notifyFakeBD: unexpected RunJSON(%v)", args)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeTempBodyFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "body.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return p
}

func newNotifyCtx(b cli.BDRunner) (*cli.Context, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	return &cli.Context{
		Home:   "/fake/home",
		BD:     b,
		Stdout: out,
		Stderr: errBuf,
	}, out, errBuf
}

// ── parseNotifyFlags ──────────────────────────────────────────────────────────

func TestParseNotifyFlags_HappyPath(t *testing.T) {
	f := makeTempBodyFile(t, "hello")
	id, file, title, err := parseNotifyFlags([]string{"at-abc", "--file", f, "--title", "My title"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "at-abc" {
		t.Errorf("id = %q, want at-abc", id)
	}
	if file != f {
		t.Errorf("file = %q, want %q", file, f)
	}
	if title != "My title" {
		t.Errorf("title = %q, want \"My title\"", title)
	}
}

func TestParseNotifyFlags_EqForm(t *testing.T) {
	f := makeTempBodyFile(t, "body")
	id, _, title, err := parseNotifyFlags([]string{"at-xyz", "--file=" + f, "--title=eq-title"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "at-xyz" {
		t.Errorf("id = %q, want at-xyz", id)
	}
	if title != "eq-title" {
		t.Errorf("title = %q, want eq-title", title)
	}
}

func TestParseNotifyFlags_NoTitle(t *testing.T) {
	f := makeTempBodyFile(t, "body")
	_, _, title, err := parseNotifyFlags([]string{"at-abc", "--file", f})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "" {
		t.Errorf("title = %q, want empty when not specified", title)
	}
}

func TestParseNotifyFlags_MissingID(t *testing.T) {
	_, _, _, err := parseNotifyFlags([]string{})
	if err == nil {
		t.Fatal("expected error for missing initiative-id")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestParseNotifyFlags_MissingFile(t *testing.T) {
	_, _, _, err := parseNotifyFlags([]string{"at-abc"})
	if err == nil {
		t.Fatal("expected error for missing --file")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestParseNotifyFlags_FileNotFound(t *testing.T) {
	_, _, _, err := parseNotifyFlags([]string{"at-abc", "--file", "/no/such/file.txt"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

func TestParseNotifyFlags_UnknownFlag(t *testing.T) {
	f := makeTempBodyFile(t, "body")
	_, _, _, err := parseNotifyFlags([]string{"at-abc", "--file", f, "--unknown=x"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// ── threadLabelValue ──────────────────────────────────────────────────────────

func TestThreadLabelValue_Present(t *testing.T) {
	got := threadLabelValue([]string{"at-00o", "thread:42", "delivery:pending"})
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}

func TestThreadLabelValue_Absent(t *testing.T) {
	got := threadLabelValue([]string{"at-00o", "delivery:pending"})
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestThreadLabelValue_Empty(t *testing.T) {
	got := threadLabelValue(nil)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ── notifyCmd.Run ─────────────────────────────────────────────────────────────

// TestNotify_FirstNotify_OpensThreadAndRecordsLabel confirms:
// - first notify sends with ThreadRef="" (new topic)
// - the returned threadRef is recorded as "thread:<ref>" on the initiative bead
// - output contains the thread ref and initiative id
func TestNotify_FirstNotify_OpensThreadAndRecordsLabel(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "first message body")

	ft := &fakeTransport{returnRef: "999"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o"},
		},
	}

	var recordedLabel string
	cmd := &notifyCmd{
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			recordedLabel = label
			// Also drive it through the fake's Run to record labelsAdded.
			_, err := nbd.Run("label", "add", id, strings.TrimPrefix(label, "thread:"))
			return err
		},
	}

	ctx, out, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx, []string{"at-00o", "--file", bodyFile}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Transport was called with ThreadRef=""
	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "" {
		t.Errorf("expected ThreadRef empty on first notify, got %q", ft.calls[0].ThreadRef)
	}
	if ft.calls[0].InitiativeID != "at-00o" {
		t.Errorf("InitiativeID = %q, want at-00o", ft.calls[0].InitiativeID)
	}
	if ft.calls[0].Body != "first message body" {
		t.Errorf("Body = %q, want %q", ft.calls[0].Body, "first message body")
	}

	// Thread label was recorded.
	if recordedLabel != "thread:999" {
		t.Errorf("recorded label = %q, want thread:999", recordedLabel)
	}

	// Output includes thread_ref.
	output := out.String()
	if !strings.Contains(output, "thread_ref: 999") {
		t.Errorf("output missing thread_ref: %q", output)
	}
	if !strings.Contains(output, "initiative: at-00o") {
		t.Errorf("output missing initiative: %q", output)
	}
}

// TestNotify_SecondNotify_ReusesExistingLabel confirms:
// - subsequent notify sends with the existing ThreadRef from the bead label
// - labelAdd is NOT called (no new label write)
func TestNotify_SecondNotify_ReusesExistingLabel(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "follow-up body")

	ft := &fakeTransport{returnRef: "999"} // Send returns same ref (thread still open)
	nbd := &notifyFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o", "thread:999"},
		},
	}

	labelAddCalled := false
	cmd := &notifyCmd{
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			labelAddCalled = true
			return nil
		},
	}

	ctx, out, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx, []string{"at-00o", "--file", bodyFile}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Transport was called with the existing ThreadRef.
	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "999" {
		t.Errorf("expected ThreadRef=999 on second notify, got %q", ft.calls[0].ThreadRef)
	}

	// labelAdd must NOT be called — thread already recorded.
	if labelAddCalled {
		t.Error("labelAdd should not be called when thread label already exists")
	}

	output := out.String()
	if !strings.Contains(output, "thread_ref: 999") {
		t.Errorf("output missing thread_ref: %q", output)
	}
}

// TestNotify_TitleFromInitiative confirms the initiative title is used when
// --title is not supplied.
func TestNotify_TitleFromInitiative(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "1"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "Initiative Title"},
	}
	cmd := &notifyCmd{
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx, []string{"at-00o", "--file", bodyFile}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].Title != "Initiative Title" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Initiative Title")
	}
}

// TestNotify_ExplicitTitle confirms --title overrides the initiative title.
func TestNotify_ExplicitTitle(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "1"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "Initiative Title"},
	}
	cmd := &notifyCmd{
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx, []string{"at-00o", "--file", bodyFile, "--title", "Override"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ft.calls[0].Title != "Override" {
		t.Errorf("Title = %q, want Override", ft.calls[0].Title)
	}
}

// TestNotify_NoTransport confirms the error path when no transport is configured.
func TestNotify_NoTransport(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "x"},
	}
	cmd := &notifyCmd{
		transportFor: func(home string) (transport.Transport, error) {
			return nil, fmt.Errorf("no transport configured")
		},
		labelAdd: func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	err := cmd.Run(ctx, []string{"at-00o", "--file", bodyFile})
	if err == nil {
		t.Fatal("expected error for no transport, got nil")
	}
	if !strings.Contains(err.Error(), "no transport configured") {
		t.Errorf("error = %q, want to contain 'no transport configured'", err.Error())
	}
}

// TestNotify_BadInitiativeID confirms the error path when bd show fails.
func TestNotify_BadInitiativeID(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	nbd := &notifyFakeBD{
		showErr: fmt.Errorf("bd show at-bad: not found"),
	}
	ft := &fakeTransport{returnRef: "1"}
	cmd := &notifyCmd{
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	err := cmd.Run(ctx, []string{"at-bad", "--file", bodyFile})
	if err == nil {
		t.Fatal("expected error for bad initiative id, got nil")
	}
	if !strings.Contains(err.Error(), "look up initiative") {
		t.Errorf("error = %q, want to contain 'look up initiative'", err.Error())
	}
}

// TestNotify_LabelWriteFailureIsNonFatal confirms that a failure to record the
// thread label does not cause Run to return an error (non-fatal per contract).
func TestNotify_LabelWriteFailureIsNonFatal(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "55"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "x"},
	}
	cmd := &notifyCmd{
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			return fmt.Errorf("bd label add: permission denied")
		},
	}
	ctx, out, errBuf := newNotifyCtx(nbd)
	err := cmd.Run(ctx, []string{"at-00o", "--file", bodyFile})
	if err != nil {
		t.Fatalf("Run should succeed despite label write failure, got: %v", err)
	}
	// thread_ref still printed.
	if !strings.Contains(out.String(), "thread_ref: 55") {
		t.Errorf("output missing thread_ref: %q", out.String())
	}
	// Warning emitted to stderr.
	if !strings.Contains(errBuf.String(), "warning") {
		t.Errorf("expected warning on stderr, got: %q", errBuf.String())
	}
}

// TestNotify_NilContext confirms nil ctx returns an error immediately.
func TestNotify_NilContext(t *testing.T) {
	cmd := &notifyCmd{
		transportFor: func(home string) (transport.Transport, error) { return nil, nil },
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	err := cmd.Run(nil, []string{"at-00o", "--file", "/dev/null"})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}
