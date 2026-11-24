package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

type bridgeDelivery struct{ recipient, sender, body, key string }

// bridgeHarness drives runBridge with scripted journal output and a fake
// cloud_home bd.
type bridgeHarness struct {
	t      *testing.T
	home   string
	stderr bytes.Buffer
	steps  []followStep
	runs   [][]string // runBD args
	cloud  [][]string // cloud bd args
	shows  map[string]string
	list   string
	sent   []bridgeDelivery
	local  map[string]string // dedup key -> local message id already present

	sendErr  error
	closeErr error
	noTarget bool
	locked   bool
	headOut  string
	cancel   context.CancelFunc
}

func newBridgeHarness(t *testing.T, withCloud bool) *bridgeHarness {
	t.Helper()
	t.Setenv("AGENT_TEAMS_HOST", "laptop-a")
	h := &bridgeHarness{t: t, home: t.TempDir(), shows: map[string]string{}, list: "[]", headOut: `[{"head":10}]`}
	if withCloud {
		writeCloudHomeConfig(t, h.home, filepath.Join(t.TempDir(), "cloud"))
	}
	return h
}

func bridgeIssueJSON(id, status, assignee string, labels ...string) string {
	quoted := make([]string, len(labels))
	for i, l := range labels {
		quoted[i] = `"` + l + `"`
	}
	return fmt.Sprintf(`[{"id":"%s","status":"%s","assignee":"%s","title":"message from at-cloud1","notes":"from: at-cloud1","description":"hello laptop","labels":[%s]}]`,
		id, status, assignee, strings.Join(quoted, ","))
}

func bridgeRowJSON(seq, op, typ string, labels ...string) string {
	quoted := make([]string, len(labels))
	for i, l := range labels {
		quoted[i] = `"` + l + `"`
	}
	return `{"seq":` + seq + `,"ts":"2026-11-23T13:00:00Z","op":"` + op + `","issue_id":"wisp-` + seq + `","issue":{"issue_type":"` + typ + `","assignee":"at-local1","labels":[` + strings.Join(quoted, ",") + `]}}`
}

func (h *bridgeHarness) follower() *bridgeFollower {
	return &bridgeFollower{
		headQuery: func(string) (string, error) { return h.headOut, nil },
		sleep:     func(context.Context, time.Duration) {},
		now:       func() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) },
		tryLock: func(path string) (func(), bool, error) {
			if h.locked {
				return nil, false, nil
			}
			return func() {}, true, nil
		},
		cloud: func(_ context.Context, args ...string) (string, error) {
			h.cloud = append(h.cloud, args)
			switch args[0] {
			case "list":
				return h.list, nil
			case "show":
				if out, ok := h.shows[args[1]]; ok {
					return out, nil
				}
				return "", errors.New("not found")
			case "close":
				return "", h.closeErr
			}
			return "", nil
		},
		target: func(_ *cli.Context, assignee string) (string, bool) {
			if h.noTarget {
				return "", false
			}
			return assignee, true
		},
		deliver: func(_ *cli.Context, recipient, sender, body, key string) (string, error) {
			if h.sendErr != nil {
				return "", h.sendErr
			}
			h.sent = append(h.sent, bridgeDelivery{recipient, sender, body, key})
			if h.local == nil {
				h.local = map[string]string{}
			}
			h.local[key] = "local-1"
			return "local-1", nil
		},
		localCopy: func(_ *cli.Context, _, key string) (string, bool, error) {
			id, ok := h.local[key]
			return id, ok, nil
		},
	}
}

func (h *bridgeHarness) cliCtx() *cli.Context {
	return &cli.Context{Home: h.home, Stderr: &h.stderr, Stdout: &bytes.Buffer{}}
}

func (h *bridgeHarness) run(b *bridgeFollower) (bridgeSkip, error) {
	run, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	defer cancel()
	b.runBD = func(_ context.Context, args []string, onLine func(string)) (string, error) {
		h.runs = append(h.runs, args)
		if len(h.steps) == 0 {
			cancel()
			return "", nil
		}
		s := h.steps[0]
		h.steps = h.steps[1:]
		for _, l := range s.lines {
			onLine(l)
		}
		if len(h.steps) == 0 {
			cancel()
		}
		return s.stderr, os.ErrClosed
	}
	return runBridge(run, h.cliCtx(), b)
}

func (h *bridgeHarness) cloudCalls(verb string) int {
	n := 0
	for _, c := range h.cloud {
		if c[0] == verb {
			n++
		}
	}
	return n
}

func (h *bridgeHarness) seqFile(name string) string {
	b, err := os.ReadFile(filepath.Join(h.home, "mailbox", name))
	if err != nil {
		return ""
	}
	return string(b)
}

func TestBridgeMatchingCreateDeliversAndCloses(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.shows["wisp-11"] = bridgeIssueJSON("wisp-11", "open", "at-local1", bridgeLabel)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel, "delivery:pending")}}}

	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(h.sent))
	}
	d := h.sent[0]
	if d.recipient != "at-local1" || d.sender != "at-cloud1" || d.key != bridgeDedupKey("wisp-11") {
		t.Errorf("delivery = %+v", d)
	}
	if !strings.Contains(d.body, "hello laptop") || !strings.Contains(d.body, "Reply with: ateam mail send at-cloud1 --file <file>") {
		t.Errorf("body = %q", d.body)
	}
	var verbs []string
	for _, c := range h.cloud {
		verbs = append(verbs, strings.Join(c[:min(len(c), 3)], " "))
	}
	want := []string{"list --include-infra --type=message", "show wisp-11 --json", "close wisp-11 --reason=bridged to laptop-a",
		"label add wisp-11", "label remove wisp-11", "label remove wisp-11"}
	if strings.Join(verbs, "|") != strings.Join(want, "|") {
		t.Errorf("cloud calls = %q, want %q", verbs, want)
	}
	last := h.cloud[len(h.cloud)-1]
	if strings.Join(last, " ") != "label remove wisp-11 delivery:pending" {
		t.Errorf("last close-out step = %v, want delivery:pending removal", last)
	}
	if h.seqFile("bridge.seq") != "11" {
		t.Errorf("bridge.seq = %q, want 11", h.seqFile("bridge.seq"))
	}
	if h.runs[0][len(h.runs[0])-1] != "10" {
		t.Errorf("tail did not start from the baseline head: %v", h.runs[0])
	}
}

func TestBridgeIgnoresRowsThatAreNotBridgeMessages(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.steps = []followStep{{lines: []string{
		bridgeRowJSON("11", "create", "task", bridgeLabel),
		bridgeRowJSON("12", "create", "message", "delivery:pending"),
		bridgeRowJSON("13", "update", "message", bridgeLabel),
		"not json at all {",
		`{"bad`,
	}}}
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 0 || h.cloudCalls("show") != 0 {
		t.Errorf("sent %d, shows %d; want none", len(h.sent), h.cloudCalls("show"))
	}
	if h.seqFile("bridge.seq") != "13" {
		t.Errorf("bridge.seq = %q, want 13 (rows are checkpointed even when ignored)", h.seqFile("bridge.seq"))
	}
	if !strings.Contains(h.stderr.String(), "ateam bridge: skipping unparseable journal line") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

func TestBridgeTruncationRunsCatchUpSweep(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.shows["wisp-7"] = bridgeIssueJSON("wisp-7", "open", "at-local1", bridgeLabel)
	h.steps = []followStep{
		{stderr: `{"error":"events_journal_truncated","floor": 50}`},
		{},
	}
	b := h.follower()
	if _, err := h.run(b); err != nil {
		t.Fatal(err)
	}
	if n := h.cloudCalls("list"); n != 2 {
		t.Errorf("sweeps = %d, want 2 (start + truncation)", n)
	}
	if h.seqFile("bridge.seq") != "49" {
		t.Errorf("bridge.seq = %q, want floor-1", h.seqFile("bridge.seq"))
	}
	if got := h.runs[1][len(h.runs[1])-1]; got != "49" {
		t.Errorf("resumed from %s, want 49", got)
	}
}

func TestBridgeNoRedeliverClosedOrBridged(t *testing.T) {
	for name, show := range map[string]string{
		"closed":  bridgeIssueJSON("wisp-11", "closed", "at-local1", bridgeLabel),
		"bridged": bridgeIssueJSON("wisp-11", "open", "at-local1", bridgeLabel, bridgeDeliveredLabel),
	} {
		t.Run(name, func(t *testing.T) {
			h := newBridgeHarness(t, true)
			h.shows["wisp-11"] = show
			h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel)}}}
			if _, err := h.run(h.follower()); err != nil {
				t.Fatal(err)
			}
			if len(h.sent) != 0 || h.cloudCalls("close") != 0 {
				t.Errorf("sent %d, closes %d; want none", len(h.sent), h.cloudCalls("close"))
			}
		})
	}
}

func TestBridgeCatchUpOnStart(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.list = bridgeIssueJSON("wisp-3", "open", "at-local1", bridgeLabel)
	h.shows["wisp-3"] = h.list
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 1 || h.sent[0].key != bridgeDedupKey("wisp-3") {
		t.Fatalf("sent = %+v, want exactly one delivery of wisp-3", h.sent)
	}
	if h.cloud[0][0] != "list" {
		t.Errorf("first cloud call = %v, want the sweep", h.cloud[0])
	}
	want := []string{"list", "--include-infra", "--type=message", "--label=bridge:laptop", "--status=open", "--json"}
	if strings.Join(h.cloud[0], " ") != strings.Join(want, " ") {
		t.Errorf("sweep args = %v", h.cloud[0])
	}
}

func TestBridgeDeliveryFailureLeavesBeadOpen(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.sendErr = errors.New("recipient asleep")
	h.shows["wisp-11"] = bridgeIssueJSON("wisp-11", "open", "at-local1", bridgeLabel)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel)}}}
	b := h.follower()
	if _, err := h.run(b); err != nil {
		t.Fatal(err)
	}
	if h.cloudCalls("close") != 0 {
		t.Error("bead was closed after a failed delivery")
	}
	if !strings.Contains(h.stderr.String(), "recipient asleep") || b.delivered != 0 || b.left != 1 {
		t.Errorf("stderr %q delivered %d left %d", h.stderr.String(), b.delivered, b.left)
	}
}

func TestBridgeCloseFailureThenRerunFindsLocalCopyAndOnlyClosesOut(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.closeErr = errors.New("hosted down")
	h.shows["wisp-11"] = bridgeIssueJSON("wisp-11", "open", "at-local1", bridgeLabel)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel)}}}
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 1 {
		t.Fatalf("first pass deliveries = %d, want 1", len(h.sent))
	}
	if h.cloudCalls("label") != 0 {
		t.Error("labels changed although close failed")
	}
	// The next start sweeps the still-open bead, finds the local copy by its
	// dedup key, and retries only the close-out.
	h.list = h.shows["wisp-11"]
	h.closeErr = nil
	h.cloud, h.steps = nil, nil
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 1 {
		t.Errorf("deliveries after rerun = %d, want still 1", len(h.sent))
	}
	if h.cloudCalls("close") != 1 || h.cloudCalls("label") != 3 {
		t.Errorf("rerun close-out calls: close %d label %d; want 1 and 3", h.cloudCalls("close"), h.cloudCalls("label"))
	}
}

func TestBridgeLeavesUndeliverableRecipient(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.noTarget = true
	h.shows["wisp-11"] = bridgeIssueJSON("wisp-11", "open", "at-elsewhere", bridgeLabel)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel)}}}
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 0 || h.cloudCalls("close") != 0 {
		t.Error("an undeliverable message was delivered or closed")
	}
}

func TestBridgeNoCloudNoop(t *testing.T) {
	h := newBridgeHarness(t, false)
	skip, err := h.run(h.follower())
	if err != nil || skip != bridgeSkipNoCloud {
		t.Fatalf("skip %q err %v", skip, err)
	}
	if len(h.runs)+len(h.cloud) != 0 {
		t.Errorf("spawned %d tails and %d cloud calls; want none", len(h.runs), len(h.cloud))
	}
}

func TestBridgeLockHeldIsCleanSkip(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.locked = true
	skip, err := h.run(h.follower())
	if err != nil || skip != bridgeSkipLocked || len(h.runs) != 0 {
		t.Fatalf("skip %q err %v runs %d", skip, err, len(h.runs))
	}
}

func TestBridgeSeparateCheckpoint(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "task")}}}
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.home, "mailbox", "host.seq")); !os.IsNotExist(err) {
		t.Errorf("bridge wrote host.seq (err %v)", err)
	}
	if h.seqFile("bridge.seq") != "11" {
		t.Errorf("bridge.seq = %q", h.seqFile("bridge.seq"))
	}
}

func TestBridgeOnceTailsWithoutFollowAndSweeps(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.list = bridgeIssueJSON("wisp-3", "open", "at-local1", bridgeLabel)
	h.shows["wisp-3"] = h.list
	h.steps = []followStep{{}}
	b := h.follower()
	b.once = true
	if _, err := h.run(b); err != nil {
		t.Fatal(err)
	}
	if len(h.runs) != 1 || strings.Contains(strings.Join(h.runs[0], " "), "--follow") {
		t.Errorf("tail runs = %v, want one pass without --follow", h.runs)
	}
	if len(h.sent) != 1 {
		t.Errorf("sent %d, want 1", len(h.sent))
	}
}

func TestMailBridgeOnceNoCloudHome(t *testing.T) {
	home := t.TempDir()
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: home, Stdout: out, Stderr: &bytes.Buffer{}}
	if err := (&mailBridgeKong{Once: true}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "bridge: cloud_home not set; nothing to do\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestMailBridgeOncePrintsCounts(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.list = bridgeIssueJSON("wisp-3", "open", "at-local1", bridgeLabel)
	h.shows["wisp-3"] = h.list
	b := h.follower()
	b.runBD = func(context.Context, []string, func(string)) (string, error) { return "", nil }
	out := &bytes.Buffer{}
	ctx := &cli.Context{Home: h.home, Stdout: out, Stderr: &h.stderr}
	if err := (&mailBridgeKong{Once: true, follower: b}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ateam bridge: delivered wisp-3 to at-local1 as local-1\ndelivered 1, left 0\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestBridgeSenderFallsBackToTitle(t *testing.T) {
	if got := bridgeSender(bd.Issue{Title: "message from at-t9"}); got != "at-t9" {
		t.Errorf("sender = %q", got)
	}
	if got := bridgeSender(bd.Issue{Title: "other", Notes: "x\nfrom: at-n1"}); got != "at-n1" {
		t.Errorf("sender = %q", got)
	}
}

// TestFollowExecBDEnvAppends runs a stub bd to show the bridge env reaches the
// child and the host path adds nothing.
func TestFollowExecBDEnvAppends(t *testing.T) {
	dir := t.TempDir()
	stub := "#!/bin/sh\necho \"tls=$BEADS_DOLT_SERVER_TLS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BEADS_DOLT_SERVER_TLS", "")

	var got []string
	if _, err := followExecBDEnv(context.Background(), "/x", []string{hostedTLSEnv}, []string{"events"}, func(l string) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "tls=1" {
		t.Errorf("bridge env: %v", got)
	}
	got = nil
	if _, err := followExecBD(context.Background(), "/x", []string{"events"}, func(l string) { got = append(got, l) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "tls=" {
		t.Errorf("host path env: %v", got)
	}
}

// TestHostFollowUsesHostCheckpointAndLock pins the host spec's state files.
func TestHostFollowUsesHostCheckpointAndLock(t *testing.T) {
	h := newFollowHarness(t, true)
	h.steps = []followStep{{lines: []string{followMsg("11", "at-xyz")}}}
	if err := h.run(&mailFollowKong{}); err != nil {
		t.Fatal(err)
	}
	if h.seq() != "11" {
		t.Errorf("host.seq = %q", h.seq())
	}
	if _, err := os.Stat(filepath.Join(h.home, "mailbox", "bridge.seq")); !os.IsNotExist(err) {
		t.Error("host follow wrote bridge.seq")
	}
	var locked string
	c := &mailFollowKong{tryLock: func(path string) (func(), bool, error) { locked = filepath.Base(path); return nil, false, nil }}
	if err := c.runChild(context.Background(), h.cliCtx()); err != nil {
		t.Fatal(err)
	}
	if locked != "host-follow.lock" {
		t.Errorf("host lock = %q", locked)
	}
}

// ── relay integration ────────────────────────────────────────────────────────

func newBridgeRelay(ft *relayFakeTransport, enabled bool, ran *atomic.Int32) *relayKong {
	return &relayKong{
		enabled:             func(string) bool { return enabled },
		transportFor:        func(string) (transport.Transport, error) { return ft, nil },
		bdQuery:             newFakeBDQuery().query,
		send:                (&fakeSend{}).send,
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: alwaysFallbackResponder,
		knownStewardTopic:   neverKnownStewardTopic,
		bridgeRun: func(_ *cli.Context, run context.Context) {
			ran.Add(1)
			if !enabled {
				return
			}
			<-run.Done()
		},
	}
}

func TestRelayNoTelegramNoCloudIsNoop(t *testing.T) {
	ctx := newRelayCtx(t)
	var ran atomic.Int32
	if err := newBridgeRelay(&relayFakeTransport{}, false, &ran).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := relayStdout(ctx); got != "messaging not configured; relay is a no-op\n" {
		t.Errorf("stdout = %q", got)
	}
	if ran.Load() != 0 {
		t.Error("bridge ran with no cloud")
	}
}

func TestRelayNoTelegramWithCloudRunsBridgeOnly(t *testing.T) {
	ctx := newRelayCtx(t)
	writeCloudHomeConfig(t, ctx.Home, filepath.Join(t.TempDir(), "cloud"))
	ft := &relayFakeTransport{}
	var ran atomic.Int32
	if err := newBridgeRelay(ft, false, &ran).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if ran.Load() != 1 || ft.received {
		t.Errorf("bridge ran %d times, Receive called %v", ran.Load(), ft.received)
	}
	if !strings.Contains(relayStderr(ctx), "starting bridge-only relay (no transport)") {
		t.Errorf("stderr = %q", relayStderr(ctx))
	}
}

func TestRelayTelegramWithCloudStartsBridgeGoroutine(t *testing.T) {
	ctx := newRelayCtx(t)
	writeCloudHomeConfig(t, ctx.Home, filepath.Join(t.TempDir(), "cloud"))
	ft := &relayFakeTransport{}
	var ran atomic.Int32
	if err := newBridgeRelay(ft, true, &ran).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !ft.received {
		t.Error("Receive not called")
	}
	if ran.Load() != 1 {
		t.Errorf("bridge goroutine ran %d times, want 1", ran.Load())
	}
}

func TestRelayTelegramNoCloudStartsNoBridge(t *testing.T) {
	ctx := newRelayCtx(t)
	var ran atomic.Int32
	if err := newBridgeRelay(&relayFakeTransport{}, true, &ran).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if ran.Load() != 0 {
		t.Error("bridge ran with no cloud")
	}
}

func TestBridgeCloseForcesHostedClose(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.shows["wisp-11"] = bridgeIssueJSON("wisp-11", "open", "at-local1", bridgeLabel)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel)}}}
	if _, err := h.run(h.follower()); err != nil {
		t.Fatal(err)
	}
	var closeArgs []string
	for _, c := range h.cloud {
		if c[0] == "close" {
			closeArgs = c
		}
	}
	if !strings.Contains(strings.Join(closeArgs, " "), "--force") {
		t.Errorf("close args = %v, want --force", closeArgs)
	}
}

// A message the recipient already read is closed locally; the repeat-send
// check must still see it and only finish the hosted close-out.
func TestBridgeExistingLocalCopyIsNotRedelivered(t *testing.T) {
	h := newBridgeHarness(t, true)
	h.local = map[string]string{bridgeDedupKey("wisp-11"): "local-9"}
	h.shows["wisp-11"] = bridgeIssueJSON("wisp-11", "open", "at-local1", bridgeLabel)
	h.steps = []followStep{{lines: []string{bridgeRowJSON("11", "create", "message", bridgeLabel)}}}
	b := h.follower()
	if _, err := h.run(b); err != nil {
		t.Fatal(err)
	}
	if len(h.sent) != 0 || b.delivered != 0 {
		t.Errorf("sent %d delivered %d; want no new delivery", len(h.sent), b.delivered)
	}
	if h.cloudCalls("close") != 1 || h.cloudCalls("label") != 3 {
		t.Errorf("close-out calls: close %d label %d; want 1 and 3", h.cloudCalls("close"), h.cloudCalls("label"))
	}
}
