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
			!strings.Contains(err.Error(), "a laptop initiative must not be registered in the hosted workspace; nothing sent") {
			t.Fatalf("desc %q: err = %v", tc.desc, err)
		}
		assertNothingSent(t, e)
	}
}

var errIssueNotFound = errors.New("Error: Issue at-nope not found")

func TestSendRun_PeerReadErrorRefused(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.ctx.BD = &fakeBD{runFn: func(...string) (string, error) { return "", errors.New("dolt timed out") }}
	err := sendPeer(e, "at-nope", "x")
	if err == nil || !strings.Contains(err.Error(), "read recipient at-nope") || !strings.Contains(err.Error(), "dolt timed out") || !strings.Contains(err.Error(), "nothing sent") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerOwnRunBeadRefused(t *testing.T) {
	e, bdCalls := peerSendEnv(t, bd.Issue{ID: "dsp-run7", Status: "open", Description: "where: run:dsp-peer9\n"})
	t.Setenv("DSP_BEAD", "dsp-run7")
	t.Setenv("DSP_META_INITIATIVE", "at-mine")
	err := sendPeer(e, "dsp-run7", "hi")
	if err == nil || !strings.Contains(err.Error(), "is this run's own run bead") || !strings.Contains(err.Error(), "initiative id (like at-xxx)") || !strings.Contains(err.Error(), "at-mine") {
		t.Fatalf("err = %v", err)
	}
	if *bdCalls != 0 {
		t.Errorf("%d bd calls; want 0", *bdCalls)
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

// peerRunBeadEnv is a run inbox env whose bd show fails (a run bead id is not in
// the initiatives DB) and whose bd list answers with list.
func peerRunBeadEnv(t *testing.T, list []bd.Issue) *runInboxEnv {
	t.Helper()
	e := newRunInboxEnv(t, 0)
	e.ctx.BD = &fakeBD{
		runFn: func(...string) (string, error) { return "", errIssueNotFound },
		runJSONFn: func(dst any, args ...string) error {
			raw, _ := json.Marshal(list)
			return json.Unmarshal(raw, dst)
		},
	}
	return e
}

func TestSendRun_PeerRunBeadResolvedToInitiative(t *testing.T) {
	e := peerRunBeadEnv(t, []bd.Issue{
		{ID: "at-other", Status: "open", Description: "where: run:eri-other\n"},
		{ID: "at-ihu", Status: "open", Description: "where: run:eri-fqumy\n"},
	})
	if err := sendPeer(e, "eri-fqumy", "hi"); err != nil {
		t.Fatal(err)
	}
	want := `{"op":"mail","to":"at-ihu","run":"eri-fqumy","body":"hi"}` + "\n"
	if got := e.helperStdin(); got != want {
		t.Errorf("outbox line:\n got %q\nwant %q", got, want)
	}
	wantOut := "resolved run bead eri-fqumy to initiative at-ihu; address peers by initiative id (like at-xxx), find them with: ateam list\n" +
		"sent to at-ihu through the run outbox (run eri-fqumy)\n"
	if got := e.stdout.String(); got != wantOut {
		t.Errorf("stdout = %q, want %q", got, wantOut)
	}
}

func TestSendRun_PeerRunBeadSeveralOpenRefused(t *testing.T) {
	e := peerRunBeadEnv(t, []bd.Issue{
		{ID: "at-a", Status: "open", Description: "where: run:eri-dup\n"},
		{ID: "at-b", Status: "open", Description: "where: run:eri-dup\n"},
	})
	err := sendPeer(e, "eri-dup", "hi")
	if err == nil || !strings.Contains(err.Error(), "at-a, at-b") || !strings.Contains(err.Error(), "initiative id (like at-xxx)") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerRunBeadOnlyClosedRefused(t *testing.T) {
	e := peerRunBeadEnv(t, []bd.Issue{{ID: "at-old", Status: "closed", Description: "where: run:eri-old\n"}})
	err := sendPeer(e, "eri-old", "hi")
	if err == nil || !strings.Contains(err.Error(), "at-old") || !strings.Contains(err.Error(), "closed") || !strings.Contains(err.Error(), "initiative id (like at-xxx)") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

// A wrong-kind id (not an initiative, not a run bead of any initiative) is
// queued for the laptop bridge, with a loud hint for the cloud-peer mistake.
func TestSendRun_PeerNoMatchQueuesForLaptopWithHint(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	var creates [][]string
	e.ctx.BD = &fakeBD{
		runFn: func(...string) (string, error) { return "", errIssueNotFound },
		runJSONFn: func(dst any, args ...string) error {
			switch args[0] {
			case "create":
				creates = append(creates, args)
				return json.Unmarshal([]byte(`{"id":"dsp-new1"}`), dst)
			}
			raw, _ := json.Marshal([]bd.Issue{{ID: "at-x", Status: "open", Description: "where: run:eri-x\n"}})
			return json.Unmarshal(raw, dst)
		},
	}
	t.Setenv("DSP_META_INITIATIVE", "at-mine")
	if err := sendPeer(e, "eri-nope1", "hi"); err != nil {
		t.Fatal(err)
	}
	if len(creates) != 1 || !slices.Contains(creates[0], "--labels=bridge:laptop") {
		t.Fatalf("creates = %v", creates)
	}
	out := e.stdout.String()
	for _, want := range []string{"queued for laptop pickup", "WARNING", "waits unread", "initiative id (like at-xxx)", "ateam list"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q: %q", want, out)
		}
	}
	if e.helperStdin() != "" {
		t.Errorf("outbox line written: %q", e.helperStdin())
	}
}

func TestSendRun_PeerRunBeadOpenBeatsClosed(t *testing.T) {
	e := peerRunBeadEnv(t, []bd.Issue{
		{ID: "at-old", Status: "closed", Description: "where: run:eri-dup\n"},
		{ID: "at-new", Status: "open", Description: "where: run:eri-dup\n"},
	})
	if err := sendPeer(e, "eri-dup", "hi"); err != nil {
		t.Fatal(err)
	}
	want := `{"op":"mail","to":"at-new","run":"eri-dup","body":"hi"}` + "\n"
	if got := e.helperStdin(); got != want {
		t.Errorf("outbox line:\n got %q\nwant %q", got, want)
	}
}

func TestSendRun_PeerRunBeadResolvingToOwnInitiativeRefused(t *testing.T) {
	e := peerRunBeadEnv(t, []bd.Issue{{ID: "at-mine", Status: "open", Description: "where: run:eri-mine\n"}})
	t.Setenv("DSP_META_INITIATIVE", "at-mine")
	err := sendPeer(e, "eri-mine", "hi")
	if err == nil || !strings.Contains(err.Error(), "own initiative") || !strings.Contains(err.Error(), "initiative id (like at-xxx)") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}

func TestSendRun_PeerRunBeadListFailureSurfaced(t *testing.T) {
	e := newRunInboxEnv(t, 0)
	e.ctx.BD = &fakeBD{
		runFn:     func(...string) (string, error) { return "", errIssueNotFound },
		runJSONFn: func(any, ...string) error { return errors.New("dolt exploded") },
	}
	err := sendPeer(e, "eri-nope1", "hi")
	if err == nil || !strings.Contains(err.Error(), "could not find initiative eri-nope1") || !strings.Contains(err.Error(), "ateam list") || !strings.Contains(err.Error(), "dolt exploded") {
		t.Fatalf("err = %v", err)
	}
	assertNothingSent(t, e)
}
