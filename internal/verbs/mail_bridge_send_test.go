package verbs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// bridgeSendEnv is a run env whose fake bd records every call. runFn answers
// `bd show`; runJSONFn answers the dedup list and `bd create`.
type bridgeSendEnv struct {
	*runInboxEnv
	calls   [][]string
	showErr error
	showIss *bd.Issue
	dedup   []bd.Issue
}

func newBridgeSendEnv(t *testing.T) *bridgeSendEnv {
	t.Helper()
	b := &bridgeSendEnv{runInboxEnv: newRunInboxEnv(t, 0)}
	b.ctx.BD = &fakeBD{
		runFn: func(args ...string) (string, error) {
			b.calls = append(b.calls, args)
			if b.showErr != nil {
				return "", b.showErr
			}
			raw, _ := json.Marshal([]bd.Issue{*b.showIss})
			return string(raw), nil
		},
		runJSONFn: func(dst any, args ...string) error {
			b.calls = append(b.calls, args)
			switch args[0] {
			case "list":
				return json.Unmarshal(mustMarshal(b.dedup), dst)
			case "create":
				return json.Unmarshal([]byte(`{"id":"dsp-new1"}`), dst)
			}
			return nil
		},
	}
	return b
}

func (b *bridgeSendEnv) send(k *sendKong, body string) error {
	file := filepath.Join(b.t.TempDir(), "msg.md")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		b.t.Fatal(err)
	}
	k.File = file
	return k.Run(b.ctx)
}

func (b *bridgeSendEnv) creates() [][]string {
	var out [][]string
	for _, c := range b.calls {
		if len(c) > 0 && c[0] == "create" {
			out = append(out, c)
		}
	}
	return out
}

func (b *bridgeSendEnv) assertNothingWritten() {
	b.t.Helper()
	if n := len(b.creates()); n != 0 {
		b.t.Errorf("%d create calls, want 0", n)
	}
	assertNothingSent(b.t, b.runInboxEnv)
}

const bridgeQueuedLine = "queued for laptop pickup (bridge:laptop); a laptop running `ateam relay` delivers it within seconds\n"

func TestBridgeSendLaptopRecipientCreatesBridgeBead(t *testing.T) {
	b := newBridgeSendEnv(t)
	b.showErr = errors.New("Error: Issue at-lap01 not found")
	k := &sendKong{RecipientID: "at-lap01", Thread: "t1", DedupKey: "k1"}
	if err := b.send(k, "hello laptop"); err != nil {
		t.Fatal(err)
	}
	cr := b.creates()
	if len(cr) != 1 {
		t.Fatalf("create calls = %d, want 1", len(cr))
	}
	for _, want := range []string{
		"--type=message",
		"--assignee=at-lap01",
		"--notes=from: at-run1",
		"--labels=delivery:pending",
		"--labels=bridge:laptop",
		"--labels=thread:t1",
		"--labels=dedup:k1",
		"--body-file=" + k.File,
		"--title=message from at-run1",
		"--json",
	} {
		if !slices.Contains(cr[0], want) {
			t.Errorf("create args missing %q: %v", want, cr[0])
		}
	}
	for _, c := range b.calls {
		if len(c) > 0 && c[0] == "label" {
			t.Errorf("label applied in a separate call: %v", c)
		}
	}
	if want := "message_id: dsp-new1\n" + bridgeQueuedLine; b.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", b.stdout.String(), want)
	}
	if b.helperStdin() != "" {
		t.Errorf("outbox line written: %q", b.helperStdin())
	}
}

func TestBridgeSendLocalStewardHandle(t *testing.T) {
	b := newBridgeSendEnv(t)
	if err := b.send(&sendKong{RecipientID: LocalStewardHandle}, "hi steward"); err != nil {
		t.Fatal(err)
	}
	cr := b.creates()
	if len(cr) != 1 || !slices.Contains(cr[0], "--assignee=local-steward") || !slices.Contains(cr[0], "--labels=bridge:laptop") {
		t.Fatalf("create = %v", cr)
	}
	for _, c := range b.calls {
		if c[0] == "show" {
			t.Errorf("recipient read for local-steward: %v", c)
		}
	}
}

func TestBridgeSendStewardStillUsesOutbox(t *testing.T) {
	b := newBridgeSendEnv(t)
	if err := b.send(&sendKong{RecipientID: StewardHandle}, "hi"); err != nil {
		t.Fatal(err)
	}
	if b.helperStdin() == "" {
		t.Error("no outbox line for the cloud steward")
	}
	if n := len(b.creates()); n != 0 {
		t.Errorf("%d create calls for the cloud steward", n)
	}
}

func TestBridgeSendCloudPeerStillUsesOutbox(t *testing.T) {
	b := newBridgeSendEnv(t)
	b.showIss = &bd.Issue{ID: "at-peer1", Status: "open", Description: "where: run:dsp-peer9\n"}
	if err := b.send(&sendKong{RecipientID: "at-peer1"}, "hi peer"); err != nil {
		t.Fatal(err)
	}
	if b.helperStdin() == "" {
		t.Error("no outbox line for a cloud peer")
	}
	if n := len(b.creates()); n != 0 {
		t.Errorf("%d create calls for a cloud peer", n)
	}
}

func TestBridgeSendRefusesSelfInvalidIdEmptyBody(t *testing.T) {
	b := newBridgeSendEnv(t)
	cause := errors.New("Issue x not found")
	for _, tc := range []struct{ name, to, body, want string }{
		{"self", "at-run1", "x", "own initiative"},
		{"invalid", "Bad Id", "x", "not a valid initiative id"},
		{"empty body", "at-lap01", "", "is empty; nothing sent"},
	} {
		file := filepath.Join(t.TempDir(), "m.md")
		if err := os.WriteFile(file, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		err := (&sendKong{RecipientID: tc.to, File: file}).sendBridgeFromRun(b.ctx, cause)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	b.assertNothingWritten()

	t.Setenv("DSP_META_INITIATIVE", "")
	file := filepath.Join(t.TempDir(), "m.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := (&sendKong{RecipientID: LocalStewardHandle, File: file}).sendBridgeFromRun(b.ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "DSP_META_INITIATIVE is empty") {
		t.Errorf("empty initiative: err = %v", err)
	}
	b.assertNothingWritten()
}

func TestBridgeSendDedupKeyHitCreatesNothing(t *testing.T) {
	b := newBridgeSendEnv(t)
	b.dedup = []bd.Issue{{ID: "dsp-old9", IssueType: "message"}}
	if err := b.send(&sendKong{RecipientID: LocalStewardHandle, DedupKey: "k1"}, "again"); err != nil {
		t.Fatal(err)
	}
	if n := len(b.creates()); n != 0 {
		t.Errorf("%d create calls on a dedup hit", n)
	}
	if want := "message_id: dsp-old9 (deduplicated)\n" + bridgeQueuedLine; b.stdout.String() != want {
		t.Errorf("stdout = %q, want %q", b.stdout.String(), want)
	}
}

func TestBridgeSendShowTransientErrorStillRefuses(t *testing.T) {
	b := newBridgeSendEnv(t)
	b.showErr = errors.New("dolt: connection timed out")
	err := b.send(&sendKong{RecipientID: "at-lap01"}, "hi")
	if err == nil || !strings.Contains(err.Error(), "read recipient at-lap01") || !strings.Contains(err.Error(), "nothing sent") {
		t.Fatalf("err = %v", err)
	}
	b.assertNothingWritten()
}
