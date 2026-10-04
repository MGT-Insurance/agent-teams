package verbs

import (
	"fmt"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
)

// TestRelay_ClosedInitiative_LocalOwnerSendFailureDoesNotAck pins the
// acknowledgement boundary for the newly reachable exact-CLOSED path: a
// local non-fallback owner attempts the existing closed envelope once, but a
// failed steward send must leave the source Telegram message unacknowledged.
func TestRelay_ClosedInitiative_LocalOwnerSendFailureDoesNotAck(t *testing.T) {
	const threadRef = "reopened-send-failure"
	closed := bd.Issue{ID: "at-closed-send-failure", Status: "closed"}
	closedQuery := newFakeBDQuery()
	closedQuery.results["thread:"+threadRef] = []bd.Issue{closed}
	failedSend := &fakeSendCapture{err: fmt.Errorf("steward unavailable")}
	acked := &fakeAck{}

	cmd := &relayKong{
		bdQuery:             newFakeBDQuery().query,
		bdQueryClosed:       closedQuery.query,
		send:                failedSend.send,
		ack:                 acked.ack,
		claimsLocally:       func(got bd.Issue) bool { return got.ID == closed.ID },
		isFallbackResponder: func(*cli.Context) bool { return false },
		knownStewardTopic:   neverKnownStewardTopic,
	}
	if err := cmd.handleReply(newRelayCtx(t), transport.Reply{
		ThreadRef:  threadRef,
		MessageRef: "-10099:88",
		Text:       "retry this closed topic",
	}); err != nil {
		t.Fatalf("handleReply: %v", err)
	}

	if len(failedSend.calls) != 1 {
		t.Fatalf("closed local-owner send attempts = %d, want 1", len(failedSend.calls))
	}
	if len(acked.refs) != 0 {
		t.Errorf("acks after closed-route send failure = %v, want none", acked.refs)
	}
}
