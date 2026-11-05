package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspaceconfig"
)

type cloudOwnedHarness struct {
	cmd         *relayKong
	sent        *fakeSendCapture
	acked       *fakeAck
	cloudCalls  []string
	closedCalls int
	pulls       int
	sleeps      int
}

// newCloudOwnedHarness builds a relayKong whose open query is empty, so every
// reply is untied, and whose cloudOwned seam returns (owned, err).
func newCloudOwnedHarness(fallback bool, owned bool, ownedErr error) *cloudOwnedHarness {
	h := &cloudOwnedHarness{sent: &fakeSendCapture{}, acked: &fakeAck{}}
	h.cmd = &relayKong{
		bdQuery: newFakeBDQuery().query,
		bdQueryClosed: func(string, string) ([]bd.Issue, error) {
			h.closedCalls++
			return nil, nil
		},
		doltPull: func(string) error { h.pulls++; return nil },
		sleeper:  func(time.Duration) { h.sleeps++ },
		send:     h.sent.send,
		ack:      h.acked.ack,
		cloudOwned: func(label string) (bool, error) {
			h.cloudCalls = append(h.cloudCalls, label)
			return owned, ownedErr
		},
		claimsLocally:       alwaysClaimsLocally,
		isFallbackResponder: func(*cli.Context) bool { return fallback },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	return h
}

func cloudOwnedReply() transport.Reply {
	return transport.Reply{ThreadRef: "77", MessageRef: "-1:9", Text: "go ahead"}
}

func TestCloudOwned_SkipsWithoutAckOrMail(t *testing.T) {
	for _, fallback := range []bool{true, false} {
		h := newCloudOwnedHarness(fallback, true, nil)
		ctx := newRelayCtx(t)
		if err := h.cmd.handleReply(ctx, cloudOwnedReply()); err != nil {
			t.Fatalf("fallback=%v: handleReply: %v", fallback, err)
		}
		if len(h.cloudCalls) != 1 || h.cloudCalls[0] != "thread:77" {
			t.Errorf("fallback=%v: cloudOwned calls = %v, want [thread:77]", fallback, h.cloudCalls)
		}
		if len(h.sent.calls) != 0 || len(h.acked.refs) != 0 {
			t.Errorf("fallback=%v: sends=%d acks=%v, want none", fallback, len(h.sent.calls), h.acked.refs)
		}
		if h.closedCalls != 0 || h.pulls != 0 || h.sleeps != 0 {
			t.Errorf("fallback=%v: closed=%d pulls=%d sleeps=%d, want 0 (skip precedes freshen and closed net)", fallback, h.closedCalls, h.pulls, h.sleeps)
		}
		if !strings.Contains(relayStderr(ctx), "cloud-owned") {
			t.Errorf("fallback=%v: missing cloud-owned diagnostic: %q", fallback, relayStderr(ctx))
		}
	}
}

func TestCloudOwned_NotOwnedKeepsUnroutedPath(t *testing.T) {
	h := newCloudOwnedHarness(true, false, nil)
	if err := h.cmd.handleReply(newRelayCtx(t), cloudOwnedReply()); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if len(h.sent.calls) != 1 {
		t.Fatalf("sends = %d, want 1 (unrouted to steward)", len(h.sent.calls))
	}
	if h.closedCalls != 1 {
		t.Errorf("closed-net queries = %d, want 1", h.closedCalls)
	}
}

func TestCloudOwned_QueryErrorFallsBackToUnroutedPath(t *testing.T) {
	h := newCloudOwnedHarness(true, false, errors.New("hosted down"))
	ctx := newRelayCtx(t)
	if err := h.cmd.handleReply(ctx, cloudOwnedReply()); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if len(h.sent.calls) != 1 {
		t.Fatalf("sends = %d, want 1 (today's unrouted path)", len(h.sent.calls))
	}
	if !strings.Contains(relayStderr(ctx), "hosted down") {
		t.Errorf("missing error diagnostic: %q", relayStderr(ctx))
	}
}

func TestCloudOwned_NilSeamKeepsUnroutedPath(t *testing.T) {
	h := newCloudOwnedHarness(true, true, nil)
	h.cmd.cloudOwned = nil
	if err := h.cmd.handleReply(newRelayCtx(t), cloudOwnedReply()); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if len(h.sent.calls) != 1 {
		t.Fatalf("sends = %d, want 1", len(h.sent.calls))
	}
}

func TestCloudOwned_TiedReplyNeverCallsSeam(t *testing.T) {
	h := newCloudOwnedHarness(true, true, nil)
	issue := bd.Issue{ID: "at-local", Status: "open"}
	open := newFakeBDQuery()
	open.results["thread:77"] = []bd.Issue{issue}
	h.cmd.bdQuery = open.query
	if err := h.cmd.handleReply(newRelayCtx(t), cloudOwnedReply()); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if len(h.cloudCalls) != 0 {
		t.Errorf("cloudOwned calls = %v, want none for a tied reply", h.cloudCalls)
	}
	if len(h.sent.calls) != 1 {
		t.Errorf("sends = %d, want 1 (routed to local initiative)", len(h.sent.calls))
	}
}

func TestCloudOwned_AmbiguousReplyNeverCallsSeam(t *testing.T) {
	h := newCloudOwnedHarness(true, true, nil)
	open := newFakeBDQuery()
	open.results["thread:77"] = []bd.Issue{{ID: "at-a", Status: "open"}, {ID: "at-b", Status: "open"}}
	h.cmd.bdQuery = open.query
	if err := h.cmd.handleReply(newRelayCtx(t), cloudOwnedReply()); err != nil {
		t.Fatalf("handleReply: %v", err)
	}
	if len(h.cloudCalls) != 0 {
		t.Errorf("cloudOwned calls = %v, want none for an ambiguous reply", h.cloudCalls)
	}
}

func TestCloudOwned_DefaultUnsetCloudHomeIsNotOwned(t *testing.T) {
	owned, err := defaultCloudOwned(t.TempDir())("thread:77")
	if err != nil || owned {
		t.Fatalf("got (%v, %v), want (false, nil) when cloud_home is unset", owned, err)
	}
}

func fakeCloudRun(out string, err error, got *[]string) cloudRunFunc {
	return func(_ context.Context, args ...string) (string, error) {
		if got != nil {
			*got = args
		}
		return out, err
	}
}

func TestCloudOwned_LabelExistsQueriesAllStatuses(t *testing.T) {
	var args []string
	owned, err := cloudLabelExists(fakeCloudRun(`[{"id":"dsp-1","status":"closed"}]`, nil, &args), "thread:77", time.Second)
	if err != nil || !owned {
		t.Fatalf("got (%v, %v), want (true, nil)", owned, err)
	}
	if got := strings.Join(args, " "); got != "list --all --label=thread:77 --json --limit 1" {
		t.Errorf("bd args = %q", got)
	}
	if owned, err := cloudLabelExists(fakeCloudRun(`[]`, nil, nil), "thread:78", time.Second); err != nil || owned {
		t.Errorf("empty result: got (%v, %v), want (false, nil)", owned, err)
	}
	if _, err := cloudLabelExists(fakeCloudRun("", errors.New("boom"), nil), "thread:79", time.Second); err == nil {
		t.Error("runner error must propagate")
	}
	if _, err := cloudLabelExists(fakeCloudRun("not json", nil, nil), "thread:80", time.Second); err == nil {
		t.Error("bad JSON must error")
	}
}

func TestCloudOwned_HungQueryTimesOut(t *testing.T) {
	blocked := func(ctx context.Context, _ ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	owned, err := cloudLabelExists(blocked, "thread:77", 50*time.Millisecond)
	if owned || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got (%v, %v), want (false, DeadlineExceeded)", owned, err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v, want about the 50ms bound", d)
	}
}

func TestCloudOwned_DefaultSameWorkspaceIsNotOwned(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	for name, cloud := range map[string]string{"same": home, "trailing slash": home + "/", "symlink": link} {
		cfg := "cloud_home = \"" + cloud + "\"\n"
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := workspaceconfig.CloudHome(home); err != nil || !ok || got == "" {
			t.Fatalf("%s: config not read: (%q, %v, %v)", name, got, ok, err)
		}
		owned, err := defaultCloudOwned(home)("thread:77")
		if err != nil || owned {
			t.Errorf("%s: got (%v, %v), want (false, nil) with no hosted query", name, owned, err)
		}
	}
}
