package verbs

import (
	"slices"
	"strings"
	"testing"
)

// The tests below drive sendKong.Run end to end, so they cover the routing in
// Run that the per-hook tests (which call the hooks directly) cannot.

func TestRunRoutesLaptopStewardToCloudSteward(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	e := newStewardSendEnv(t)
	c := &sendKong{RecipientID: CloudStewardHandle, Sender: StewardHandle, File: e.file}
	if err := c.Run(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.createCall != 1 {
		t.Fatalf("mail bead creates = %d, want 1", e.createCall)
	}
	if e.createArgs[0] != e.dispatcherDir || e.createArgs[1] != "laptop:steward" {
		t.Errorf("create args = %v", e.createArgs)
	}
	if got := e.stdout.String(); !strings.Contains(got, "sent to cloud-steward") {
		t.Errorf("stdout = %q", got)
	}
}

func TestRunRefusesCloudStewardFromNonStewardSender(t *testing.T) {
	t.Setenv("DSP_BEAD", "")
	e := newStewardSendEnv(t)
	c := &sendKong{RecipientID: CloudStewardHandle, Sender: "someone", File: e.file}
	err := c.Run(e.ctx)
	if err == nil || !strings.Contains(err.Error(), "only the laptop steward") {
		t.Fatalf("err = %v", err)
	}
	if e.createCall != 0 {
		t.Errorf("created %d mail beads, want 0", e.createCall)
	}
}

func TestRunRoutesHostedStewardToLocalStewardBridge(t *testing.T) {
	t.Setenv("BEADS_ACTOR", "agent:steward")
	b := newBridgeSendEnv(t)
	t.Setenv("DSP_BEAD", "") // newRunInboxEnv sets it; the cloud steward has no run
	b.ctx.Home = hostedHome(t, hostedMetadata)
	k := &sendKong{RecipientID: LocalStewardHandle}
	if err := b.send(k, "hello laptop steward"); err != nil {
		t.Fatal(err)
	}
	cr := b.creates()
	if len(cr) != 1 {
		t.Fatalf("create calls = %d, want 1", len(cr))
	}
	for _, want := range []string{"--assignee=local-steward", "--labels=bridge:laptop", "--notes=from: cloud-steward"} {
		if !slices.Contains(cr[0], want) {
			t.Errorf("create args missing %q: %v", want, cr[0])
		}
	}
	if b.helperStdin() != "" {
		t.Errorf("outbox line written: %q", b.helperStdin())
	}
}

func TestRunHostedLocalStewardRefusesOtherActors(t *testing.T) {
	t.Setenv("BEADS_ACTOR", "agent:other")
	b := newBridgeSendEnv(t)
	t.Setenv("DSP_BEAD", "")
	b.ctx.Home = hostedHome(t, hostedMetadata)
	err := b.send(&sendKong{RecipientID: LocalStewardHandle}, "hi")
	if err == nil || !strings.Contains(err.Error(), "nothing sent") {
		t.Fatalf("err = %v", err)
	}
	b.assertNothingWritten()
}
