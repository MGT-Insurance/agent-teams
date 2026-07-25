package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/sentlog"
)

// These cases exist because every real call site now declares a Sender
// constant (agent-teams-48dh.2), which means the UNDECLARED guard and the
// log-write-failure path can no longer be reached by any real `ateam`
// invocation — the only way to exercise them is a direct unit test against
// loggingTransport itself.

// fakeTransport is a minimal Transport double: no network, no credentials,
// returns whatever the test configures.
type fakeTransport struct {
	name    string
	sendRef string
	sendErr error
}

func (f *fakeTransport) Name() string { return f.name }

func (f *fakeTransport) Send(msg OutboundMessage) (string, error) {
	return f.sendRef, f.sendErr
}

func (f *fakeTransport) Receive(handler func(Reply) error) error { return nil }

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String()
}

// lastRecord reads sentlog.Path(home) and decodes its last line.
func lastRecord(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(sentlog.Path(home))
	if err != nil {
		t.Fatalf("read sentlog: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &rec); err != nil {
		t.Fatalf("unmarshal record %q: %v", lines[len(lines)-1], err)
	}
	return rec
}

// TestLoggingTransportUndeclaredSenderGuard: an OutboundMessage with a zero
// Sender is still recorded — never dropped — as the literal "UNDECLARED",
// with a stderr warning (contract §1's runtime guard).
func TestLoggingTransportUndeclaredSenderGuard(t *testing.T) {
	home := t.TempDir()
	inner := &fakeTransport{name: "fake", sendRef: "ref-1"}
	lt := newLoggingTransport(inner, home)

	msg := OutboundMessage{InitiativeID: "at-test", Title: "t", Body: "b"} // Sender left zero value

	var ref string
	var sendErr error
	stderr := captureStderr(t, func() {
		ref, sendErr = lt.Send(msg)
	})

	if sendErr != nil {
		t.Fatalf("Send returned error: %v", sendErr)
	}
	if ref != "ref-1" {
		t.Fatalf("ref = %q, want ref-1", ref)
	}
	if !strings.Contains(stderr, "UNDECLARED") {
		t.Fatalf("stderr warning missing UNDECLARED mention: %q", stderr)
	}

	rec := lastRecord(t, home)
	if rec["sender"] != string(sentlog.KindUndeclared) {
		t.Fatalf("sender = %v, want %q", rec["sender"], sentlog.KindUndeclared)
	}
}

// TestLoggingTransportLogWriteFailureDoesNotAffectSendOutcome: contract §6 —
// a log-write failure must never change the outcome of a send. Force the
// write to fail by pre-creating sentlog.Path(home) as a directory (Append's
// OpenFile then fails), and assert Send still returns the inner transport's
// successful result, with only a stderr warning.
func TestLoggingTransportLogWriteFailureDoesNotAffectSendOutcome(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(sentlog.Path(home), 0o755); err != nil {
		t.Fatalf("pre-create log path as directory: %v", err)
	}

	inner := &fakeTransport{name: "fake", sendRef: "ref-2"}
	lt := newLoggingTransport(inner, home)

	msg := OutboundMessage{InitiativeID: "at-test", Title: "t", Body: "b", Sender: sentlog.KindNotify}

	var ref string
	var sendErr error
	stderr := captureStderr(t, func() {
		ref, sendErr = lt.Send(msg)
	})

	if sendErr != nil {
		t.Fatalf("Send returned error even though the inner Send succeeded (log-write failure must not affect the outcome): %v", sendErr)
	}
	if ref != "ref-2" {
		t.Fatalf("ref = %q, want ref-2", ref)
	}
	if !strings.Contains(stderr, "could not append sent-log record") {
		t.Fatalf("expected a stderr warning about the failed log append, got: %q", stderr)
	}
}

// TestLoggingTransportReturnsInnerErrorVerbatimAndRecordsFailedOutcome:
// when the inner Send fails, the decorator returns that error unchanged and
// still appends a record with outcome="failed" and the error string.
func TestLoggingTransportReturnsInnerErrorVerbatimAndRecordsFailedOutcome(t *testing.T) {
	home := t.TempDir()
	wantErr := errors.New("boom")
	inner := &fakeTransport{name: "fake", sendErr: wantErr}
	lt := newLoggingTransport(inner, home)

	msg := OutboundMessage{InitiativeID: "at-test", ThreadRef: "existing-ref", Sender: sentlog.KindClose}

	ref, err := lt.Send(msg)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if ref != "" {
		t.Fatalf("ref = %q, want empty on failure", ref)
	}

	rec := lastRecord(t, home)
	if rec["outcome"] != sentlog.OutcomeFailed {
		t.Fatalf("outcome = %v, want %q", rec["outcome"], sentlog.OutcomeFailed)
	}
	if rec["error"] != "boom" {
		t.Fatalf("error = %v, want boom", rec["error"])
	}
	// §2.1: on failure, thread_ref falls back to msg.ThreadRef since nothing
	// was returned.
	if rec["thread_ref"] != "existing-ref" {
		t.Fatalf("thread_ref = %v, want existing-ref (msg.ThreadRef fallback on failure)", rec["thread_ref"])
	}
}

// TestLoggingTransportRedactsURLCredentialsFromError: loggingTransport is
// generic — it wraps whatever Transport For returns and cannot call into
// that transport's own error-sanitizer (today, Telegram's
// sanitizeTransportErr). If some future transport's Send returns an error
// with a credential embedded in a URL (a bot-token-shaped path, here), that
// credential must not land on disk. Asserts on the raw bytes written to
// sentlog.Path(home), not on a return value (contract §6, amended).
func TestLoggingTransportRedactsURLCredentialsFromError(t *testing.T) {
	home := t.TempDir()
	const token = "123456:AAFsecretBotTokenValueXYZ"
	sendErr := fmt.Errorf("Post %q: dial tcp: connection refused", "https://api.telegram.org/bot"+token+"/sendMessage")
	inner := &fakeTransport{name: "fake", sendErr: sendErr}
	lt := newLoggingTransport(inner, home)

	msg := OutboundMessage{InitiativeID: "at-test", Sender: sentlog.KindNotify}
	if _, err := lt.Send(msg); err == nil {
		t.Fatalf("expected Send to return the inner error")
	}

	data, err := os.ReadFile(sentlog.Path(home))
	if err != nil {
		t.Fatalf("read sentlog: %v", err)
	}
	if strings.Contains(string(data), token) {
		t.Fatalf("token leaked into sent-log on disk: %s", data)
	}
	if !strings.Contains(string(data), "api.telegram.org") {
		t.Fatalf("expected host to survive redaction, got: %s", data)
	}
}
