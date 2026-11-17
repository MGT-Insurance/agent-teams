package verbs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// peerSendEnv is a run inbox env whose fake bd answers `bd show` with issue.
func peerSendEnv(t *testing.T, issue bd.Issue) (*runInboxEnv, *int) {
	t.Helper()
	e := newRunInboxEnv(t, 0)
	var bdCalls int
	e.ctx.BD = &fakeBD{
		runFn: func(args ...string) (string, error) {
			bdCalls++
			raw, _ := json.Marshal([]bd.Issue{issue})
			return string(raw), nil
		},
	}
	return e, &bdCalls
}

func sendPeer(e *runInboxEnv, to, body string) error {
	file := filepath.Join(e.t.TempDir(), "msg.md")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return (&sendKong{RecipientID: to, File: file}).Run(e.ctx)
}

func assertNothingSent(t *testing.T, e *runInboxEnv) {
	t.Helper()
	if e.helperStdin() != "" {
		t.Errorf("outbox line written: %q", e.helperStdin())
	}
	if _, err := os.Stat(filepath.Join(e.ctx.Home, "mailbox")); err == nil {
		t.Error("a doorbell dir was created")
	}
}

func TestSendRun_PeerCloudInitiativeWritesOutboxLine(t *testing.T) {
	e, _ := peerSendEnv(t, bd.Issue{ID: "at-peer1", Status: "open", Description: "where: run:dsp-peer9\n"})
	if err := sendPeer(e, "at-peer1", "hi peer\nline two"); err != nil {
		t.Fatal(err)
	}
	want := `{"op":"mail","to":"at-peer1","run":"dsp-peer9","body":"hi peer\nline two"}` + "\n"
	if got := e.helperStdin(); got != want {
		t.Errorf("outbox line:\n got %q\nwant %q", got, want)
	}
	if got, want := e.stdout.String(), "sent to at-peer1 through the run outbox (run dsp-peer9)\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestSendRun_PeerBodyCappedAt8000Runes(t *testing.T) {
	e, _ := peerSendEnv(t, bd.Issue{ID: "at-peer1", Status: "open", Description: "where: run:dsp-peer9\n"})
	if err := sendPeer(e, "at-peer1", strings.Repeat("é", 9000)); err != nil {
		t.Fatal(err)
	}
	var op outboxPeerMailOp
	if err := json.Unmarshal([]byte(e.helperStdin()), &op); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(op.Body)); n != 8000 {
		t.Errorf("body length = %d runes, want 8000", n)
	}
}

func TestSendRun_PeerSelfRefusedWithoutReadingAt(t *testing.T) {
	e, bdCalls := peerSendEnv(t, bd.Issue{ID: "at-run1", Status: "open", Description: "where: run:dsp-run1\n"})
	err := sendPeer(e, "at-run1", "to me")
	if err == nil || !strings.Contains(err.Error(), "at-run1 is this run's own initiative; nothing sent") {
		t.Fatalf("err = %v", err)
	}
	if *bdCalls != 0 {
		t.Errorf("%d bd calls; the self check runs first", *bdCalls)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerClosedRefused(t *testing.T) {
	e, _ := peerSendEnv(t, bd.Issue{ID: "at-peer1", Status: "closed", Description: "where: run:dsp-peer9\n"})
	err := sendPeer(e, "at-peer1", "x")
	if err == nil || !strings.Contains(err.Error(), "at-peer1 is closed; nothing sent") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerLaptopInitiativeRefused(t *testing.T) {
	for _, tc := range []struct{ desc, shown string }{
		{"where: host:laptop\n", "where: host:laptop)"},
		{"worktree: /w\n", "where: none)"},
		{"where: run:\n", "where: run:)"},
	} {
		e, _ := peerSendEnv(t, bd.Issue{ID: "at-peer1", Status: "open", Description: tc.desc})
		err := sendPeer(e, "at-peer1", "x")
		if err == nil || !strings.Contains(err.Error(), "is not a cloud initiative ("+tc.shown) ||
			!strings.Contains(err.Error(), "(dsp-43n.17); nothing sent") {
			t.Fatalf("desc %q: err = %v", tc.desc, err)
		}
		assertNothingSent(t, e)
	}
}

func TestSendRun_PeerReadErrorRefused(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.ctx.BD = &fakeBD{runFn: func(...string) (string, error) { return "", os.ErrNotExist }}
	err := sendPeer(e, "at-nope", "x")
	if err == nil || !strings.Contains(err.Error(), "read recipient at-nope") || !strings.Contains(err.Error(), "nothing sent") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerEmptyBodyRefused(t *testing.T) {
	e, _ := peerSendEnv(t, bd.Issue{ID: "at-peer1", Status: "open", Description: "where: run:dsp-peer9\n"})
	err := sendPeer(e, "at-peer1", "")
	if err == nil || !strings.Contains(err.Error(), "is empty; nothing sent") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerInvalidIDRefused(t *testing.T) {
	for _, id := range []string{"At-peer1", "peer1", "at-", "at-Peer", "at peer", "1at-x", "at-peer1\n"} {
		e, bdCalls := peerSendEnv(t, bd.Issue{ID: id, Status: "open", Description: "where: run:dsp-peer9\n"})
		err := sendPeer(e, id, "hi")
		if err == nil || !strings.Contains(err.Error(), "is not a valid initiative id; nothing sent") {
			t.Fatalf("id %q: err = %v", id, err)
		}
		if *bdCalls != 0 {
			t.Errorf("id %q: %d bd calls; the id check runs before the read", id, *bdCalls)
		}
		assertNothingSent(t, e)
	}
}
