package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
)

// stewardRouteFakeBD backs notifyToSteward's two bd touchpoints:
//   - "bd show <initiativeID> --json" (kind derivation) returns issueLabels.
//   - "bd show <anything else> --json" (sendKong's recipientWorktree lookup
//     of StewardHandle) fails, mirroring production where "steward" is a
//     reserved mailbox recipient, not a bd initiative bead — sendKong.Run is
//     expected to fall back to its "skip liveness check" no-op path.
//   - "bd create ..." (RunJSON) records the message bead's assignee and the
//     content of its --body-file, then returns msgID.
type stewardRouteFakeBD struct {
	initiativeID string
	issueLabels  []string
	msgID        string

	createArgs []string
	createBody string
}

func (f *stewardRouteFakeBD) Run(args ...string) (string, error) {
	if len(args) >= 3 && args[0] == "show" && args[2] == "--json" {
		if args[1] == f.initiativeID {
			issue := bd.Issue{ID: f.initiativeID, Labels: f.issueLabels}
			raw, err := json.Marshal([]bd.Issue{issue})
			return string(raw), err
		}
		return "", fmt.Errorf("stewardRouteFakeBD: no such initiative: %s", args[1])
	}
	return "", fmt.Errorf("stewardRouteFakeBD: unexpected Run(%v)", args)
}

func (f *stewardRouteFakeBD) RunJSON(dst any, args ...string) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("stewardRouteFakeBD: unexpected RunJSON(%v)", args)
	}
	f.createArgs = args
	for _, a := range args {
		if bodyFile, ok := strings.CutPrefix(a, "--body-file="); ok {
			data, err := os.ReadFile(bodyFile)
			if err != nil {
				return err
			}
			f.createBody = string(data)
		}
	}
	id := f.msgID
	if id == "" {
		id = "at-steward-msg1"
	}
	if issue, ok := dst.(*bd.Issue); ok {
		issue.ID = id
	}
	return nil
}

// ── notifyToSteward: envelope + routing ──────────────────────────────────────

func TestNotifyToSteward_ReviewKind_BuildsEnvelopeAndRoutesToSteward(t *testing.T) {
	initiativeID := "at-x9"
	fbd := &stewardRouteFakeBD{initiativeID: initiativeID, issueLabels: []string{"human", "gate:review"}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	askFile := makeTempFile(t, "Should we ship the release?")
	if err := notifyToSteward(ctx, initiativeID, askFile); err != nil {
		t.Fatalf("notifyToSteward: unexpected error: %v", err)
	}

	assertContains(t, fbd.createArgs, "--assignee="+StewardHandle, "bd create missing steward assignee")

	env, ok := ParseStewardGateEnvelope(fbd.createBody)
	if !ok {
		t.Fatalf("message body is not a well-formed steward-gate envelope:\n%s", fbd.createBody)
	}
	if env.InitiativeID != initiativeID {
		t.Errorf("envelope InitiativeID = %q, want %q", env.InitiativeID, initiativeID)
	}
	if env.Kind != StewardGateKindReview {
		t.Errorf("envelope Kind = %q, want %q", env.Kind, StewardGateKindReview)
	}
	if env.Body != "Should we ship the release?" {
		t.Errorf("envelope Body = %q, want %q", env.Body, "Should we ship the release?")
	}

	doorbell := StewardDoorbellPath(ctx)
	if _, err := os.Stat(doorbell); err != nil {
		t.Errorf("expected doorbell touched at %s: %v", doorbell, err)
	}
}

func TestNotifyToSteward_NoReviewLabel_DefaultsToQuestionKind(t *testing.T) {
	initiativeID := "at-x10"
	fbd := &stewardRouteFakeBD{initiativeID: initiativeID, issueLabels: []string{"human", "gate:question"}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	askFile := makeTempFile(t, "what should we name it?")
	if err := notifyToSteward(ctx, initiativeID, askFile); err != nil {
		t.Fatalf("notifyToSteward: unexpected error: %v", err)
	}

	env, ok := ParseStewardGateEnvelope(fbd.createBody)
	if !ok {
		t.Fatalf("message body is not a well-formed steward-gate envelope:\n%s", fbd.createBody)
	}
	if env.Kind != StewardGateKindQuestion {
		t.Errorf("envelope Kind = %q, want %q", env.Kind, StewardGateKindQuestion)
	}
}

func TestNotifyToSteward_FileNotFound_ReturnsError(t *testing.T) {
	fbd := &stewardRouteFakeBD{initiativeID: "at-x11"}
	ctx, _, _ := makeCtx(fbd, t.TempDir())

	if err := notifyToSteward(ctx, "at-x11", "/no/such/file"); err == nil {
		t.Fatal("expected error for missing ask file, got nil")
	}
}

// ── gate -> notifyToSteward: non-fatal on failure ────────────────────────────

// TestGate_NotifyToStewardFailureIsNonFatal proves that when notifyToSteward
// fails (here: the bd show lookup used for kind derivation errors out), the
// gate itself still succeeds — same non-fatal contract as notifyForGate
// (write_test.go's TestGate_NotifyFailureIsNonFatal), now exercised against
// the real notifyToSteward wired in exactly as kong_converted.go wires it.
func TestGate_NotifyToStewardFailureIsNonFatal(t *testing.T) {
	f := makeTempFile(t, "should we proceed?")
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: "ok"},                          // note
		{stdout: "ok"},                          // label add human
		{stdout: "ok"},                          // label add gate:question
		{err: fmt.Errorf("bd show: not found")}, // notifyToSteward's bd.ShowIssue
	})
	errBuf := ctx.Stderr.(*bytes.Buffer)

	cmd := &gateKong{
		ID:      "at-5",
		File:    f,
		Kind:    "question",
		enabled: func(string) bool { return true },
		notify:  notifyToSteward,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("gate must succeed even when notifyToSteward fails, got: %v", err)
	}
	if len(*calls) != 4 {
		t.Fatalf("expected 4 bd calls (3 gate + 1 notifyToSteward show), got %d", len(*calls))
	}
	if !strings.Contains(errBuf.String(), "warning") {
		t.Errorf("expected warning on stderr, got: %q", errBuf.String())
	}
}
