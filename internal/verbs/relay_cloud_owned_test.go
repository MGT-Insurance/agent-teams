package verbs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
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

type fakeCloudRunner struct {
	out  string
	err  error
	args []string
}

func (f *fakeCloudRunner) Run(args ...string) (string, error) { return f.out, f.err }
func (f *fakeCloudRunner) RunJSON(dst any, args ...string) error {
	f.args = args
	if f.err != nil {
		return f.err
	}
	return json.Unmarshal([]byte(f.out), dst)
}

func TestCloudOwned_LabelExistsQueriesAllStatuses(t *testing.T) {
	r := &fakeCloudRunner{out: `[{"id":"dsp-1","status":"closed"}]`}
	owned, err := cloudLabelExists(r, "thread:77")
	if err != nil || !owned {
		t.Fatalf("got (%v, %v), want (true, nil)", owned, err)
	}
	if got := strings.Join(r.args, " "); got != "list --all --label=thread:77 --json --limit 1" {
		t.Errorf("bd args = %q", got)
	}
	r = &fakeCloudRunner{out: `[]`}
	if owned, err := cloudLabelExists(r, "thread:78"); err != nil || owned {
		t.Errorf("empty result: got (%v, %v), want (false, nil)", owned, err)
	}
	r = &fakeCloudRunner{err: errors.New("boom")}
	if _, err := cloudLabelExists(r, "thread:79"); err == nil {
		t.Error("runner error must propagate")
	}
}
