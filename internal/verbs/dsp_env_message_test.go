package verbs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewMessageMailOpFieldOrderAndCap(t *testing.T) {
	raw, err := json.Marshal(newMessageMailOp("at-ab12", "hello\nworld"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"op":"file","type":"mail","title":"at-ab12: message","body":"initiative: at-ab12\nkind: message\n---\nhello\nworld"}`
	if string(raw) != want {
		t.Errorf("got  %s\nwant %s", raw, want)
	}

	op := newMessageMailOp("at-ab12", strings.Repeat("x", maxGateOutboxAskRunes+1))
	if got := len(op.Body) - len("initiative: at-ab12\nkind: message\n---\n"); got != maxGateOutboxAskRunes {
		t.Errorf("text kept %d runes, want %d", got, maxGateOutboxAskRunes)
	}
}

func TestOutboxAckOpMarshals(t *testing.T) {
	raw, err := json.Marshal(outboxAckOp{Op: "ack", Seqs: []int{48213, 48214}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"op":"ack","seqs":[48213,48214]}`; string(raw) != want {
		t.Errorf("got %s, want %s", raw, want)
	}
}
