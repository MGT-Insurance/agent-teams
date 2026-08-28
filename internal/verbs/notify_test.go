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
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
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

// fakeTransportFor returns ft or an error, for injection into notifyKong.
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
	// rememberErr, if non-nil, is returned by bd remember — the failing
	// publishStewardTopics stub.
	rememberErr error
	// remembered records the --key= argument of each bd remember call, i.e.
	// every publishStewardTopics upsert this fake saw.
	remembered []string
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
	// remember --key=<key> <value> — publishStewardTopics
	if len(args) >= 2 && args[0] == "remember" {
		if f.rememberErr != nil {
			return "", f.rememberErr
		}
		f.remembered = append(f.remembered, args[1])
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

// ── notifyKong.Run flag validation ─────────────────────────────────────────────
//
// Required-flag / positional enforcement (--file required, <id> required) is
// kong's job now (struct tags), exercised by the framework's own parse tests
// and TestAllVerbsRegistered (verbs_test.go). Only the Run-level file-exists
// check (mirroring noteKong/registerKong) is ours to test here.

func TestNotify_FileNotFound(t *testing.T) {
	nbd := &notifyFakeBD{issue: bd.Issue{ID: "at-abc"}}
	cmd := &notifyKong{
		ID:           "at-abc",
		File:         "/no/such/file.txt",
		transportFor: func(home string) (transport.Transport, error) { return nil, nil },
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// TestNotify_ImageNotFound mirrors TestNotify_FileNotFound for the --image
// flag: a nonexistent image path is a usage error, same shape as a
// nonexistent --file.
func TestNotify_ImageNotFound(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	nbd := &notifyFakeBD{issue: bd.Issue{ID: "at-abc"}}
	cmd := &notifyKong{
		ID:           "at-abc",
		File:         bodyFile,
		Image:        "/no/such/image.png",
		transportFor: func(home string) (transport.Transport, error) { return nil, nil },
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if code := cli.ExitCode(err); code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// TestNotify_WithImage_SetsImagePathOnOutboundMessage confirms --image flows
// through to transport.OutboundMessage.ImagePath on the same Send call a
// text-only notify makes — no separate send, no separate flag path.
func TestNotify_WithImage_SetsImagePathOnOutboundMessage(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "here's a screenshot")
	imagePath := filepath.Join(t.TempDir(), "screenshot.png")
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write temp image: %v", err)
	}

	ft := &fakeTransport{returnRef: "999"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "my initiative", Labels: []string{"at-00o"}},
	}
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		Image:        imagePath,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}

	ctx, _, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ImagePath != imagePath {
		t.Errorf("ImagePath = %q, want %q", ft.calls[0].ImagePath, imagePath)
	}
	if ft.calls[0].Body != "here's a screenshot" {
		t.Errorf("Body = %q, want %q", ft.calls[0].Body, "here's a screenshot")
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
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			recordedLabel = label
			// Also drive it through the fake's Run to record labelsAdded.
			_, err := nbd.Run("label", "add", id, strings.TrimPrefix(label, "thread:"))
			return err
		},
	}

	ctx, out, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
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
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			labelAddCalled = true
			return nil
		},
	}

	ctx, out, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
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
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
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
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		Title:        "Override",
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
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
	cmd := &notifyKong{
		ID:   "at-00o",
		File: bodyFile,
		transportFor: func(home string) (transport.Transport, error) {
			return nil, fmt.Errorf("no transport configured")
		},
		labelAdd: func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	err := cmd.Run(ctx)
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
	cmd := &notifyKong{
		ID:           "at-bad",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected error for bad initiative id, got nil")
	}
	if !strings.Contains(err.Error(), "look up initiative") {
		t.Errorf("error = %q, want to contain 'look up initiative'", err.Error())
	}
}

// TestNotify_LabelWriteFailureIsNonFatal confirms that a failure to record the
// thread label does not cause Run to return an error (non-fatal per contract).
// TestNotify_LabelWriteFailure_RetriedThenLoud confirms the Part A hardening
// (agent-teams-6rru.10, comment on .1): when the thread label write
// repeatedly fails after a successful Send, sendAndLabelThread retries it
// (bounded by labelWriteMaxAttempts) and then surfaces the failure loudly —
// Run returns a non-nil error (no longer swallowed to a stderr-only warning
// as before) and stderr makes clear the topic is replyable but unroutable.
func TestNotify_LabelWriteFailure_RetriedThenLoud(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "55"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00o", Title: "x"},
	}
	attempts := 0
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			attempts++
			return fmt.Errorf("bd label add: permission denied")
		},
	}
	ctx, _, errBuf := newNotifyCtx(nbd)
	err := cmd.Run(ctx)
	if err == nil {
		t.Fatal("expected Run to return an error once retries are exhausted (loud, not swallowed)")
	}
	if attempts != labelWriteMaxAttempts {
		t.Errorf("labelAdd attempts = %d, want %d (bounded retry)", attempts, labelWriteMaxAttempts)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("returned error = %q, want it to wrap the underlying labelAdd error", err.Error())
	}
	if !strings.Contains(errBuf.String(), "UNROUTABLE") {
		t.Errorf("expected a loud UNROUTABLE diagnostic on stderr, got: %q", errBuf.String())
	}
}

// TestNotify_LabelWriteFailure_RetrySucceedsWithinBound confirms a transient
// label-write failure that succeeds within the bounded retry window does not
// fail Run and does not surface a loud failure.
func TestNotify_LabelWriteFailure_RetrySucceedsWithinBound(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "66"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{ID: "at-00p", Title: "x"},
	}
	attempts := 0
	cmd := &notifyKong{
		ID:           "at-00p",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			attempts++
			if attempts < labelWriteMaxAttempts {
				return fmt.Errorf("transient dolt lock contention")
			}
			return nil
		},
	}
	ctx, out, errBuf := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run should succeed once the retry lands within bound, got: %v", err)
	}
	if attempts != labelWriteMaxAttempts {
		t.Errorf("labelAdd attempts = %d, want %d (succeeds on final attempt)", attempts, labelWriteMaxAttempts)
	}
	if !strings.Contains(out.String(), "thread_ref: 66") {
		t.Errorf("output missing thread_ref: %q", out.String())
	}
	if strings.Contains(errBuf.String(), "UNROUTABLE") {
		t.Errorf("should not surface a loud failure when the retry succeeds, stderr: %q", errBuf.String())
	}
}

// pushRecordingBD is a cli.BDRunner that records every Run call (joined
// args) and answers `show` with a configured issue — used to verify
// sendAndLabelThread's post-label-write best-effort `dolt push`
// (agent-teams-6rru.10 Part A) without pulling in notifyFakeBD's
// label-add-specific bookkeeping.
type pushRecordingBD struct {
	issue bd.Issue
	calls []string
}

func (f *pushRecordingBD) Run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) > 0 && args[0] == "show" {
		raw, err := json.Marshal([]bd.Issue{f.issue})
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return "", nil
}

func (f *pushRecordingBD) RunJSON(dst any, args ...string) error {
	return fmt.Errorf("pushRecordingBD: unexpected RunJSON(%v)", args)
}

// TestNotify_LabelWriteSuccess_AttemptsDoltPush confirms that a successful
// label write is followed by a best-effort `bd dolt push` (agent-teams-6rru.10
// Part A) so peer machines can pull the label before a reply arrives.
func TestNotify_LabelWriteSuccess_AttemptsDoltPush(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "88"}
	pbd := &pushRecordingBD{issue: bd.Issue{ID: "at-00q", Title: "x"}}
	cmd := &notifyKong{
		ID:           "at-00q",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			_, err := b.Run("label", "add", id, strings.TrimPrefix(label, "thread:"))
			return err
		},
	}
	ctx, _, _ := newNotifyCtx(pbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	found := false
	for _, c := range pbd.calls {
		if c == "dolt push" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a 'dolt push' call after a successful label write, got calls: %v", pbd.calls)
	}
}

// ── BriefingHandle ────────────────────────────────────────────────────────────

// TestNotify_Briefing_FirstNotify_CreatesTopicAndPersistsFile confirms:
//   - no bd lookup occurs for the briefing handle (notifyFakeBD would error on
//     an unexpected Run call if one were attempted)
//   - first notify sends with ThreadRef="" (new topic)
//   - the returned threadRef is persisted to StewardBriefingThreadPath, not a
//     bead label
//   - default title is "Briefings" when --title is not given
func TestNotify_Briefing_FirstNotify_CreatesTopicAndPersistsFile(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "cross-initiative briefing body")
	home := t.TempDir()

	ft := &fakeTransport{returnRef: "777"}
	nbd := &notifyFakeBD{} // no issue configured; any Run call fails the test

	cmd := &notifyKong{
		ID:           BriefingHandle,
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the briefing handle")
			return nil
		},
	}

	ctx, out, _ := newNotifyCtx(nbd)
	ctx.Home = home
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "" {
		t.Errorf("expected ThreadRef empty on first briefing notify, got %q", ft.calls[0].ThreadRef)
	}
	if ft.calls[0].InitiativeID != BriefingHandle {
		t.Errorf("InitiativeID = %q, want %q", ft.calls[0].InitiativeID, BriefingHandle)
	}
	if ft.calls[0].Title != "Briefings" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Briefings")
	}
	if ft.calls[0].Body != "cross-initiative briefing body" {
		t.Errorf("Body = %q, want %q", ft.calls[0].Body, "cross-initiative briefing body")
	}

	path := StewardBriefingThreadPath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected briefing thread file at %s: %v", path, err)
	}
	if strings.TrimSpace(string(data)) != "777" {
		t.Errorf("persisted thread ref = %q, want %q", string(data), "777")
	}

	output := out.String()
	if !strings.Contains(output, "thread_ref: 777") {
		t.Errorf("output missing thread_ref: %q", output)
	}
	if !strings.Contains(output, "initiative: "+BriefingHandle) {
		t.Errorf("output missing initiative line: %q", output)
	}
}

// TestNotify_Briefing_SecondNotify_ReusesPersistedFile confirms:
// - an existing StewardBriefingThreadPath file is read and sent as ThreadRef
// - no bd lookup and no labelAdd occurs
func TestNotify_Briefing_SecondNotify_ReusesPersistedFile(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "follow-up briefing")
	home := t.TempDir()

	nbd := &notifyFakeBD{}
	ctx, out, _ := newNotifyCtx(nbd)
	ctx.Home = home

	path := StewardBriefingThreadPath(ctx)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("321"), 0o644); err != nil {
		t.Fatalf("seed briefing thread file: %v", err)
	}

	ft := &fakeTransport{returnRef: "321"} // thread still open
	cmd := &notifyKong{
		ID:           BriefingHandle,
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the briefing handle")
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "321" {
		t.Errorf("expected ThreadRef=321 reused from file, got %q", ft.calls[0].ThreadRef)
	}

	if !strings.Contains(out.String(), "thread_ref: 321") {
		t.Errorf("output missing thread_ref: %q", out.String())
	}
}

// TestNotify_Briefing_ExplicitTitle confirms --title overrides the "Briefings" default.
func TestNotify_Briefing_ExplicitTitle(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	home := t.TempDir()
	ft := &fakeTransport{returnRef: "1"}
	nbd := &notifyFakeBD{}
	cmd := &notifyKong{
		ID:           BriefingHandle,
		File:         bodyFile,
		Title:        "Weekly Roundup",
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	ctx.Home = home
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ft.calls[0].Title != "Weekly Roundup" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Weekly Roundup")
	}
}

// ── ReviewsHandle ─────────────────────────────────────────────────────────────

// TestNotify_Reviews_FirstNotify_CreatesTopicAndPersistsFile confirms the
// shared PR-review topic bootstraps like the briefing topic:
//   - no bd show for the reviews handle (notifyFakeBD errors on an
//     unexpected Run, and no issue is configured)
//   - first notify sends with ThreadRef="" (new topic)
//   - the returned ref is persisted to StewardReviewsThreadPath, not a label
//   - default title is "Reviews", declared sender is KindNotifyReviews
func TestNotify_Reviews_FirstNotify_CreatesTopicAndPersistsFile(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "Review started · #12 midgard — Fix flaky retry logic")
	home := t.TempDir()

	ft := &fakeTransport{returnRef: "888"}
	nbd := &notifyFakeBD{}

	cmd := &notifyKong{
		ID:           ReviewsHandle,
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the reviews handle")
			return nil
		},
	}

	ctx, out, _ := newNotifyCtx(nbd)
	ctx.Home = home
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "" {
		t.Errorf("expected ThreadRef empty on first reviews notify, got %q", ft.calls[0].ThreadRef)
	}
	if ft.calls[0].InitiativeID != ReviewsHandle {
		t.Errorf("InitiativeID = %q, want %q", ft.calls[0].InitiativeID, ReviewsHandle)
	}
	if ft.calls[0].Title != "Reviews" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Reviews")
	}
	if ft.calls[0].Sender != sentlog.KindNotifyReviews {
		t.Errorf("Sender = %q, want %q", ft.calls[0].Sender, sentlog.KindNotifyReviews)
	}

	path := StewardReviewsThreadPath(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected reviews thread file at %s: %v", path, err)
	}
	if strings.TrimSpace(string(data)) != "888" {
		t.Errorf("persisted thread ref = %q, want %q", string(data), "888")
	}

	output := out.String()
	if !strings.Contains(output, "thread_ref: 888") {
		t.Errorf("output missing thread_ref: %q", output)
	}
	if !strings.Contains(output, "initiative: "+ReviewsHandle) {
		t.Errorf("output missing initiative line: %q", output)
	}
}

// TestNotify_Reviews_SecondNotify_ReusesPersistedFile is the whole point of a
// SHARED review topic: the second review must land in the topic the first one
// opened. A non-empty ThreadRef on Send is what tells the transport to post
// into the existing topic rather than create another one, so asserting it is
// asserting that no second forum topic is opened.
func TestNotify_Reviews_SecondNotify_ReusesPersistedFile(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "Review complete · #13 midgard — Add retry jitter")
	home := t.TempDir()

	nbd := &notifyFakeBD{}
	ctx, out, _ := newNotifyCtx(nbd)
	ctx.Home = home

	path := StewardReviewsThreadPath(ctx)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("888"), 0o644); err != nil {
		t.Fatalf("seed reviews thread file: %v", err)
	}

	ft := &fakeTransport{returnRef: "888"} // topic still open
	cmd := &notifyKong{
		ID:           ReviewsHandle,
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the reviews handle")
			return nil
		},
	}

	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "888" {
		t.Errorf("expected ThreadRef=888 reused from file, got %q", ft.calls[0].ThreadRef)
	}
	if !strings.Contains(out.String(), "thread_ref: 888") {
		t.Errorf("output missing thread_ref: %q", out.String())
	}
}

// TestNotify_Reviews_ExplicitTitle confirms --title overrides the "Reviews" default.
func TestNotify_Reviews_ExplicitTitle(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	ft := &fakeTransport{returnRef: "1"}
	nbd := &notifyFakeBD{}
	cmd := &notifyKong{
		ID:           ReviewsHandle,
		File:         bodyFile,
		Title:        "PR Reviews",
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	ctx.Home = t.TempDir()
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ft.calls[0].Title != "PR Reviews" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "PR Reviews")
	}
}

// ── shared-topic publish trigger ──────────────────────────────────────────────

// TestNotify_SharedTopic_PublishesOnEverySend is the regression guard for
// agent-teams-p9dm.6. publishStewardTopics used to fire only inside the
// first-creation branch, so an install whose topic was opened before that
// code shipped could never acquire a steward:topics record — leaving
// isKnownStewardTopic always false and the relay's peer-topic skip dead. The
// REUSE send seeded here (thread-ref file already present) is exactly the
// case that published nothing before, and both shared handles run through
// sendSharedTopic, so both are pinned.
func TestNotify_SharedTopic_PublishesOnEverySend(t *testing.T) {
	for _, tc := range []struct {
		handle string
		path   func(*cli.Context) string
	}{
		{BriefingHandle, StewardBriefingThreadPath},
		{ReviewsHandle, StewardReviewsThreadPath},
	} {
		t.Run(tc.handle, func(t *testing.T) {
			bodyFile := makeTempBodyFile(t, "body")
			nbd := &notifyFakeBD{}
			ctx, _, _ := newNotifyCtx(nbd)
			ctx.Home = t.TempDir()

			path := tc.path(ctx)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte("555"), 0o644); err != nil {
				t.Fatalf("seed thread file: %v", err)
			}

			ft := &fakeTransport{returnRef: "555"}
			cmd := &notifyKong{
				ID:           tc.handle,
				File:         bodyFile,
				transportFor: fakeTransportFor(ft, nil),
				labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
			}
			if err := cmd.Run(ctx); err != nil {
				t.Fatalf("Run returned error: %v", err)
			}

			if len(nbd.remembered) != 1 {
				t.Fatalf("expected 1 publishStewardTopics upsert on a reuse send, got %d: %v", len(nbd.remembered), nbd.remembered)
			}
			if !strings.HasPrefix(nbd.remembered[0], "--key="+stewardTopicsKeyPrefix) {
				t.Errorf("remember key = %q, want the %q namespace", nbd.remembered[0], stewardTopicsKeyPrefix)
			}
		})
	}
}

// TestNotify_Reviews_PublishFailureStillSends confirms the publish stays
// fail-soft now that it runs on every send: a broken memory store warns on
// stderr and must not fail the notify, because losing the message is strictly
// worse than a stale synced topics record.
func TestNotify_Reviews_PublishFailureStillSends(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	nbd := &notifyFakeBD{rememberErr: fmt.Errorf("dolt is down")}
	ctx, out, errBuf := newNotifyCtx(nbd)
	ctx.Home = t.TempDir()

	ft := &fakeTransport{returnRef: "999"}
	cmd := &notifyKong{
		ID:           ReviewsHandle,
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("a failing publish must not fail the send, got: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if !strings.Contains(out.String(), "thread_ref: 999") {
		t.Errorf("output missing thread_ref: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "could not publish steward topics") {
		t.Errorf("stderr missing publish warning: %q", errBuf.String())
	}
	// The ref still persisted — the publish failure is downstream of it.
	data, err := os.ReadFile(StewardReviewsThreadPath(ctx))
	if err != nil {
		t.Fatalf("expected reviews thread file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "999" {
		t.Errorf("persisted thread ref = %q, want %q", string(data), "999")
	}
}

// ── DirectHandle ──────────────────────────────────────────────────────────────

// TestNotify_Direct_PostsToGeneralChannel confirms single-channel @mention
// addressing (agent-teams-4x83) under `--to general` — the regression guard on
// today's behavior now that --to selects the destination (agent-teams-ncn5.11):
//   - no bd lookup occurs for the direct handle (notifyFakeBD would error on
//     an unexpected Run call if one were attempted)
//   - Send is called with General:true, ChatRef:"" and no ThreadRef
//   - no thread-ref file is written (no dedicated topic to persist)
//   - no labelAdd occurs
//   - default title is "Steward" when --title is not given
func TestNotify_Direct_PostsToGeneralChannel(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "direct message body")
	home := t.TempDir()

	ft := &fakeTransport{returnRef: ""}
	nbd := &notifyFakeBD{} // no issue configured; any Run call fails the test

	cmd := &notifyKong{
		ID:           DirectHandle,
		File:         bodyFile,
		To:           generalDest,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the direct handle")
			return nil
		},
	}

	ctx, out, errBuf := newNotifyCtx(nbd)
	ctx.Home = home
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if !ft.calls[0].General {
		t.Error("expected General=true on direct-handle Send")
	}
	if ft.calls[0].ChatRef != "" {
		t.Errorf("expected ChatRef empty for --to general, got %q", ft.calls[0].ChatRef)
	}
	if ft.calls[0].ThreadRef != "" {
		t.Errorf("expected ThreadRef empty for a General send, got %q", ft.calls[0].ThreadRef)
	}
	if strings.Contains(errBuf.String(), "--to was omitted") {
		t.Errorf("explicit --to general must not warn about an omitted flag: %q", errBuf.String())
	}
	if ft.calls[0].InitiativeID != DirectHandle {
		t.Errorf("InitiativeID = %q, want %q", ft.calls[0].InitiativeID, DirectHandle)
	}
	if ft.calls[0].Title != "Steward" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Steward")
	}
	if ft.calls[0].Body != "direct message body" {
		t.Errorf("Body = %q, want %q", ft.calls[0].Body, "direct message body")
	}

	if _, err := os.Stat(filepath.Join(StewardHome(ctx), "direct-thread")); !os.IsNotExist(err) {
		t.Errorf("expected no direct thread-ref file to be written, stat err = %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "thread_ref: \n") {
		t.Errorf("output missing empty thread_ref line: %q", output)
	}
	if !strings.Contains(output, "initiative: "+DirectHandle) {
		t.Errorf("output missing initiative line: %q", output)
	}
}

// TestNotify_Direct_ExplicitTitle confirms --title overrides the "Steward" default.
func TestNotify_Direct_ExplicitTitle(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "body")
	home := t.TempDir()
	ft := &fakeTransport{returnRef: "1"}
	nbd := &notifyFakeBD{}
	cmd := &notifyKong{
		ID:           DirectHandle,
		File:         bodyFile,
		Title:        "Direct Line",
		To:           generalDest,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	ctx, _, _ := newNotifyCtx(nbd)
	ctx.Home = home
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if ft.calls[0].Title != "Direct Line" {
		t.Errorf("Title = %q, want %q", ft.calls[0].Title, "Direct Line")
	}
}

// ── DirectHandle --to routing (agent-teams-ncn5.11) ───────────────────────────

// logLineDepth returns the transport.Logf indentation depth of line — the
// number of two-space indent groups immediately after the fixed-width
// "YYYY-MM-DD HH:MM:SS " timestamp prefix.
func logLineDepth(t *testing.T, line string) int {
	t.Helper()
	const prefixLen = len("2006-01-02 15:04:05 ")
	if len(line) < prefixLen {
		t.Fatalf("log line too short to carry a timestamp prefix: %q", line)
	}
	spaces := 0
	for _, r := range line[prefixLen:] {
		if r != ' ' {
			break
		}
		spaces++
	}
	return spaces / 2
}

// runDirectWithTo runs a direct-handle notify with the given --to value and
// returns the fake transport plus the captured stderr.
func runDirectWithTo(t *testing.T, to string) (*fakeTransport, string) {
	t.Helper()
	bodyFile := makeTempBodyFile(t, "direct message body")
	ft := &fakeTransport{returnRef: ""}
	cmd := &notifyKong{
		ID:           DirectHandle,
		File:         bodyFile,
		To:           to,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the direct handle")
			return nil
		},
	}
	ctx, _, errBuf := newNotifyCtx(&notifyFakeBD{})
	ctx.Home = t.TempDir()
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	return ft, errBuf.String()
}

// TestNotify_Direct_ToRef_AddressesConversation confirms `--to <ref>` routes the
// reply into the conversation the ref names: ChatRef carries the value verbatim
// and General is false, so the transport addresses that chat rather than the
// shared channel.
func TestNotify_Direct_ToRef_AddressesConversation(t *testing.T) {
	const ref = "8675309:42"
	ft, stderr := runDirectWithTo(t, ref)

	if ft.calls[0].ChatRef != ref {
		t.Errorf("ChatRef = %q, want %q", ft.calls[0].ChatRef, ref)
	}
	if ft.calls[0].General {
		t.Error("expected General=false when --to names a conversation")
	}
	if stderr != "" {
		t.Errorf("expected no diagnostic when --to is supplied, got: %q", stderr)
	}
}

// TestNotify_Direct_ToRef_PassedThroughUnparsed confirms the ref is opaque to
// notify: a value notify has no way to make sense of still reaches the
// transport verbatim, because deciding whether a destination is well-formed or
// permitted belongs to the owning transport, not here.
func TestNotify_Direct_ToRef_PassedThroughUnparsed(t *testing.T) {
	for _, ref := range []string{"not:a:real:ref", "  spaced  ", "GENERAL", "general-ish", "{}"} {
		t.Run(ref, func(t *testing.T) {
			ft, _ := runDirectWithTo(t, ref)
			if ft.calls[0].ChatRef != ref {
				t.Errorf("ChatRef = %q, want %q verbatim", ft.calls[0].ChatRef, ref)
			}
			if ft.calls[0].General {
				t.Errorf("expected General=false for --to %q", ref)
			}
		})
	}
}

// TestNotify_Direct_ToAbsent_DeliversToGeneralAndLogsLoudly confirms the
// fallback contract for an omitted --to: the message STILL goes out, addressed
// exactly as `--to general` would address it (losing the answer entirely is
// worse than delivering it to the wrong place), and the omission is reported
// as a depth-0 fault. The diagnostic is the whole point of the clause — an
// absent --to is the only signal that the steward's prose was not followed, so
// assert the log line, not just the send.
func TestNotify_Direct_ToAbsent_DeliversToGeneralAndLogsLoudly(t *testing.T) {
	ft, stderr := runDirectWithTo(t, "")

	if !ft.calls[0].General {
		t.Error("expected General=true on the omitted --to fallback")
	}
	if ft.calls[0].ChatRef != "" {
		t.Errorf("expected ChatRef empty on the omitted --to fallback, got %q", ft.calls[0].ChatRef)
	}

	var found string
	for _, line := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
		if strings.Contains(line, "--to") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("expected a diagnostic naming --to on stderr, got: %q", stderr)
	}
	if depth := logLineDepth(t, found); depth != 0 {
		t.Errorf("expected depth-0 log line, got depth %d: %q", depth, found)
	}
	for _, want := range []string{"FAULT", "omitted", "General"} {
		if !strings.Contains(found, want) {
			t.Errorf("diagnostic missing %q: %q", want, found)
		}
	}
}

// ── --to on a handle that does not read it (agent-teams-ncn5.14) ──────────────
//
// The direct handle is the only reader of --to. The complementary case — a
// direct send with --to must NOT draw this warning — is already pinned by
// TestNotify_Direct_ToRef_AddressesConversation's empty-stderr assertion.

// assertIgnoredToWarning pins the depth-0 diagnostic for a --to no code path
// read. It must name the ignored value verbatim: "some --to was dropped" does
// not tell the reader WHICH conversation they thought they were replying to.
func assertIgnoredToWarning(t *testing.T, stderr, ref string) {
	t.Helper()
	var found string
	for _, line := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
		if strings.Contains(line, "IGNORED") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("expected a diagnostic reporting the ignored --to on stderr, got: %q", stderr)
	}
	if depth := logLineDepth(t, found); depth != 0 {
		t.Errorf("expected depth-0 log line, got depth %d: %q", depth, found)
	}
	if !strings.Contains(found, ref) {
		t.Errorf("diagnostic must name the ignored value %q verbatim: %q", ref, found)
	}
	if !strings.Contains(found, "--to") {
		t.Errorf("diagnostic must name the flag: %q", found)
	}
}

// TestNotify_Initiative_ToIgnored_WarnsAndStillSends confirms an initiative
// send names a supplied --to at depth 0 rather than dropping it in silence,
// and still delivers on the topic the bead's thread label pins. The send
// proceeding is half the contract: --to cannot misroute a handle that never
// reads it, so refusing the command would cost a message to buy nothing.
func TestNotify_Initiative_ToIgnored_WarnsAndStillSends(t *testing.T) {
	const ref = "8675309:42"
	bodyFile := makeTempBodyFile(t, "initiative body")

	ft := &fakeTransport{returnRef: "555"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o", "thread:555"},
		},
	}
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		To:           ref,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called when a thread label already exists")
			return nil
		},
	}

	ctx, _, errBuf := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ThreadRef != "555" {
		t.Errorf("ThreadRef = %q, want the recorded topic %q", ft.calls[0].ThreadRef, "555")
	}
	if ft.calls[0].ChatRef != "" {
		t.Errorf("ChatRef = %q, want empty — --to must not leak into a non-direct send", ft.calls[0].ChatRef)
	}
	if ft.calls[0].General {
		t.Error("expected General=false on an initiative send")
	}

	assertIgnoredToWarning(t, errBuf.String(), ref)
}

// TestNotify_Briefing_ToIgnored_WarnsAndStillSends is the same contract for the
// briefing handle, whose destination comes from StewardBriefingThreadPath
// rather than a bead label — a second handle that accepts --to and has nowhere
// to put it.
func TestNotify_Briefing_ToIgnored_WarnsAndStillSends(t *testing.T) {
	const ref = "8675309:42"
	bodyFile := makeTempBodyFile(t, "briefing body")

	ft := &fakeTransport{returnRef: "777"}
	cmd := &notifyKong{
		ID:           BriefingHandle,
		File:         bodyFile,
		To:           ref,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called for the briefing handle")
			return nil
		},
	}

	ctx, _, errBuf := newNotifyCtx(&notifyFakeBD{})
	ctx.Home = t.TempDir()
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(ft.calls) != 1 {
		t.Fatalf("expected 1 Send call, got %d", len(ft.calls))
	}
	if ft.calls[0].ChatRef != "" {
		t.Errorf("ChatRef = %q, want empty — --to must not leak into a briefing send", ft.calls[0].ChatRef)
	}
	if ft.calls[0].General {
		t.Error("expected General=false on a briefing send")
	}

	assertIgnoredToWarning(t, errBuf.String(), ref)
}

// TestNotify_Initiative_NoTo_Silent confirms the warning is scoped to an
// actually-supplied --to: the overwhelmingly common initiative send must stay
// quiet, or the diagnostic becomes noise that trains the reader to ignore it.
func TestNotify_Initiative_NoTo_Silent(t *testing.T) {
	bodyFile := makeTempBodyFile(t, "initiative body")

	ft := &fakeTransport{returnRef: "555"}
	nbd := &notifyFakeBD{
		issue: bd.Issue{
			ID:     "at-00o",
			Title:  "my initiative",
			Labels: []string{"at-00o", "thread:555"},
		},
	}
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         bodyFile,
		transportFor: fakeTransportFor(ft, nil),
		labelAdd: func(b cli.BDRunner, id, label string) error {
			t.Fatalf("labelAdd should not be called when a thread label already exists")
			return nil
		},
	}

	ctx, _, errBuf := newNotifyCtx(nbd)
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if errBuf.String() != "" {
		t.Errorf("expected no diagnostic when --to is absent on an initiative send, got: %q", errBuf.String())
	}
}

// TestNotify_NilContext confirms nil ctx returns an error immediately.
func TestNotify_NilContext(t *testing.T) {
	cmd := &notifyKong{
		ID:           "at-00o",
		File:         "/dev/null",
		transportFor: func(home string) (transport.Transport, error) { return nil, nil },
		labelAdd:     func(b cli.BDRunner, id, label string) error { return nil },
	}
	err := cmd.Run(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}
