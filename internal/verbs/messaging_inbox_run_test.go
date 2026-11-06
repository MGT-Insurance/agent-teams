package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// runInboxEnv is a fake Dispatcher VM for `ateam mail inbox` and `ateam mail
// send`: DSP env vars, a fake outbox-append, and an initiative worktree cwd.
type runInboxEnv struct {
	t      *testing.T
	sup    string
	ctx    *cli.Context
	stdout *strings.Builder
	stderr *strings.Builder
	atMail []bd.Issue
}

type sbWriter struct{ sb *strings.Builder }

func (w sbWriter) Write(p []byte) (int, error) { return w.sb.Write(p) }

func newRunInboxEnv(t *testing.T, helperExit int) *runInboxEnv {
	t.Helper()
	e := &runInboxEnv{t: t, sup: t.TempDir(), stdout: &strings.Builder{}, stderr: &strings.Builder{}}
	t.Setenv("DSP_BEAD", "dsp-run1")
	t.Setenv("DSP_SUP", e.sup)
	t.Setenv("DSP_META_INITIATIVE", "at-run1")
	t.Setenv(sessionIDEnvVar, "")
	t.Setenv(codexSessionIDEnvVar, "")
	writeOutboxHelper(t, e.sup, 0o755, helperExit, "helper says hi")
	if err := os.MkdirAll(filepath.Join(e.sup, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	t.Chdir(cwd)
	fbd := &fakeBD{
		runJSONFn: func(dst any, args ...string) error {
			switch {
			case containsAll(args, "--include-infra"):
				return json.Unmarshal(mustMarshal(e.atMail), dst)
			case containsAll(args, "--status=open"):
				issues := []bd.Issue{{ID: "at-run1", Description: "worktree: " + cwd + "\n", Status: "open"}}
				return json.Unmarshal(mustMarshal(issues), dst)
			}
			return nil
		},
	}
	e.ctx = &cli.Context{Home: t.TempDir(), BD: fbd, Stdout: sbWriter{e.stdout}, Stderr: sbWriter{e.stderr}}
	return e
}

func (e *runInboxEnv) putInbox(seq string, content string) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.sup, "inbox", seq+".json"), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *runInboxEnv) putRead(seq string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Join(e.sup, "inbox", "read"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.sup, "inbox", "read", seq+".json"), []byte("{}"), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *runInboxEnv) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(e.sup, rel))
	return err == nil
}

func (e *runInboxEnv) helperStdin() string {
	data, _ := os.ReadFile(filepath.Join(e.sup, "helper-stdin"))
	return string(data)
}

const (
	inboxMsg5 = `{"seq":5,"author":"agent:steward","text":"first words","at":"2026-11-06T18:04:05.000Z"}`
	inboxMsg6 = `{"seq":6,"author":"agent:steward","text":"second words","at":"2026-11-06T18:05:05.000Z"}`
)

func TestInboxRun_PrintsRunMessagesBeforeAtMailAndAcks(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.atMail = []bd.Issue{{ID: "at-m1", IssueType: "message", Assignee: "at-run1", Description: "from at mail"}}
	e.putInbox("6", inboxMsg6)
	e.putInbox("5", inboxMsg5)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	out := e.stdout.String()
	i5 := strings.Index(out, "from agent:steward (run comment 5, 2026-11-06T18:04:05.000Z):\nfirst words")
	i6 := strings.Index(out, "from agent:steward (run comment 6, 2026-11-06T18:05:05.000Z):\nsecond words")
	iAt := strings.Index(out, "from at mail")
	if i5 < 0 || i6 < i5 || iAt < i6 {
		t.Fatalf("order wrong (5=%d 6=%d at=%d):\n%s", i5, i6, iAt, out)
	}
	if !strings.Contains(out, "You have 3 unread message(s):") {
		t.Errorf("count line missing:\n%s", out)
	}
	if got, want := e.helperStdin(), `{"op":"ack","seqs":[5,6]}`+"\n"; got != want {
		t.Errorf("ack line = %q, want %q", got, want)
	}
	for _, s := range []string{"5", "6"} {
		if e.exists("inbox/" + s + ".json") {
			t.Errorf("inbox/%s.json was not moved", s)
		}
		if !e.exists("inbox/read/" + s + ".json") {
			t.Errorf("inbox/read/%s.json missing", s)
		}
	}
}

func TestInboxRun_AckFailureKeepsFilesAndExitsZero(t *testing.T) {
	e := newRunInboxEnv(t, outboxHelperExitNothingWritten)
	e.putInbox("5", inboxMsg5)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatalf("want exit 0, got %v", err)
	}
	if !strings.Contains(e.stderr.String(), "ack failed") {
		t.Errorf("stderr lacks \"ack failed\": %q", e.stderr.String())
	}
	if !e.exists("inbox/5.json") || e.exists("inbox/read/5.json") {
		t.Error("file must stay in inbox when the ack fails")
	}
}

func TestInboxRun_RepeatIsNotPrintedButIsAckedAndRemoved(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.putRead("5")
	e.putInbox("5", inboxMsg5)
	e.putInbox("6", inboxMsg6)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.stdout.String(), "first words") {
		t.Errorf("repeat was printed:\n%s", e.stdout.String())
	}
	if !strings.Contains(e.stdout.String(), "second words") {
		t.Errorf("new message not printed:\n%s", e.stdout.String())
	}
	if got, want := e.helperStdin(), `{"op":"ack","seqs":[5,6]}`+"\n"; got != want {
		t.Errorf("ack line = %q, want %q", got, want)
	}
	if e.exists("inbox/5.json") {
		t.Error("repeat copy must be removed")
	}
}

func TestInboxRun_BadJSONSkippedAndLeftInPlace(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.putInbox("4", "{not json")
	e.putInbox("5", inboxMsg5)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.stderr.String(), "ateam inbox: skip 4.json:") {
		t.Errorf("stderr = %q", e.stderr.String())
	}
	if !e.exists("inbox/4.json") {
		t.Error("bad file must stay")
	}
	if got, want := e.helperStdin(), `{"op":"ack","seqs":[5]}`+"\n"; got != want {
		t.Errorf("ack line = %q, want %q", got, want)
	}
}

func TestInboxRun_PeekCountsFilesAndConsumesNothing(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.atMail = []bd.Issue{{ID: "at-m1", IssueType: "message", Assignee: "at-run1"}}
	e.putInbox("5", inboxMsg5)
	e.putInbox("6", inboxMsg6)

	if err := (&inboxKong{Peek: true}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stdout.String(); got != "3 unread message(s)\n" {
		t.Errorf("stdout = %q", got)
	}
	if !e.exists("inbox/5.json") || e.exists("helper-stdin") {
		t.Error("peek consumed something")
	}
}

func TestInboxRun_JSONUnchangedAndConsumesNothing(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.atMail = []bd.Issue{{ID: "at-m1", IssueType: "message", Assignee: "at-run1", Description: "x"}}
	e.putInbox("5", inboxMsg5)

	if err := (&inboxKong{JSON: true}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.stdout.String(), "first words") || !strings.HasPrefix(e.stdout.String(), "[") {
		t.Errorf("--json stdout changed: %q", e.stdout.String())
	}
	if !strings.Contains(e.stderr.String(), "ateam inbox: 1 run message(s) not shown in --json; run ateam mail inbox") {
		t.Errorf("stderr = %q", e.stderr.String())
	}
	if !e.exists("inbox/5.json") || e.exists("helper-stdin") {
		t.Error("--json consumed run messages")
	}
}

func TestInboxRun_NoFilesBehavesAsBefore(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stdout.String(); got != "no unread mail\n" {
		t.Errorf("stdout = %q", got)
	}
	if e.exists("helper-stdin") {
		t.Error("an ack line was written with no messages")
	}
}

func TestInboxOutsideRunIgnoresInboxFiles(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	t.Setenv("DSP_BEAD", "")
	e.putInbox("5", inboxMsg5)

	if err := (&inboxKong{}).Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.stdout.String(); got != "no unread mail\n" {
		t.Errorf("stdout = %q", got)
	}
	if !e.exists("inbox/5.json") || e.exists("helper-stdin") {
		t.Error("outside a run, inbox files must not be touched")
	}
}

func sendToSteward(e *runInboxEnv, body string) error {
	file := filepath.Join(e.t.TempDir(), "msg.md")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return (&sendKong{RecipientID: StewardHandle, File: file}).Run(e.ctx)
}

func TestSendRun_StewardGoesThroughOutboxOnly(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	// A bd call or a doorbell would fail this test: the fake bd has no create handler.
	var bdCalls int
	e.ctx.BD = &fakeBD{
		runFn:     func(args ...string) (string, error) { bdCalls++; return "", nil },
		runJSONFn: func(dst any, args ...string) error { bdCalls++; return nil },
	}
	if err := sendToSteward(e, "hello steward\nline two"); err != nil {
		t.Fatal(err)
	}
	want := `{"op":"file","type":"mail","title":"at-run1: message","body":"initiative: at-run1\nkind: message\n---\nhello steward\nline two"}` + "\n"
	if got := e.helperStdin(); got != want {
		t.Errorf("outbox line:\n got %q\nwant %q", got, want)
	}
	if bdCalls != 0 {
		t.Errorf("%d bd calls; nothing may go to at", bdCalls)
	}
	if _, err := os.Stat(filepath.Join(e.ctx.Home, "mailbox")); err == nil {
		t.Error("a doorbell dir was created")
	}
	if !strings.Contains(e.stdout.String(), "sent to steward through the run outbox") {
		t.Errorf("stdout = %q", e.stdout.String())
	}
}

func TestSendRun_StewardTextCappedAt8000Runes(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	if err := sendToSteward(e, strings.Repeat("é", 9000)); err != nil {
		t.Fatal(err)
	}
	var op outboxMailOp
	if err := json.Unmarshal([]byte(e.helperStdin()), &op); err != nil {
		t.Fatal(err)
	}
	prefix := "initiative: at-run1\nkind: message\n---\n"
	if got := strings.TrimPrefix(op.Body, prefix); len([]rune(got)) != 8000 {
		t.Errorf("text length = %d runes, want 8000", len([]rune(got)))
	}
}

func TestSendRun_StewardHelperFailureFailsCommand(t *testing.T) {
	e := newRunInboxEnv(t, outboxHelperExitNothingWritten)
	if err := sendToSteward(e, "hi"); err == nil {
		t.Fatal("want an error when outbox-append writes nothing")
	}
}
