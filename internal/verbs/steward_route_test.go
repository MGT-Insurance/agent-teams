package verbs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
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
	// gateKong.Run's own note/label calls, ahead of notify — no-op ok so this
	// fake can drive a full gateKong.Run() (see
	// TestGate_NotifyToStewardMarkerPresent_RoutesEndToEnd below).
	if len(args) >= 1 && (args[0] == "note" || args[0] == "label") {
		return "ok", nil
	}
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

// requireStewardMarker creates the Steward session marker in ctx.Home (via
// the real stewardInitKong.Run) so notifyToSteward's presence guard
// (agent-teams-e3mq.24) passes. Doesn't touch ctx.BD, so it's safe to call
// against any fake BD, including a queued fakeExec.
func requireStewardMarker(t *testing.T, ctx *cli.Context) {
	t.Helper()
	if err := (&stewardInitKong{}).Run(ctx); err != nil {
		t.Fatalf("requireStewardMarker: steward init: %v", err)
	}
}

// ── notifyToSteward: presence guard (agent-teams-e3mq.24) ───────────────────

func TestNotifyToSteward_NoMarker_NoOpsWithoutBuildingOrSending(t *testing.T) {
	initiativeID := "at-x12"
	fbd := &stewardRouteFakeBD{initiativeID: initiativeID, issueLabels: []string{"human", "gate:review"}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	// No requireStewardMarker call: this machine has no steward configured.

	askFile := makeTempFile(t, "Should we ship the release?")
	if err := notifyToSteward(ctx, initiativeID, askFile, nil); err != nil {
		t.Fatalf("notifyToSteward: expected nil (no-op) with no marker, got: %v", err)
	}

	if fbd.createArgs != nil {
		t.Errorf("expected no bd create call with no steward marker, got args: %v", fbd.createArgs)
	}
	doorbell := StewardDoorbellPath(ctx)
	if _, err := os.Stat(doorbell); !os.IsNotExist(err) {
		t.Errorf("expected doorbell NOT touched with no steward marker, stat: %v", err)
	}
}

// ── notifyToSteward: envelope + routing ──────────────────────────────────────

func TestNotifyToSteward_ReviewKind_BuildsEnvelopeAndRoutesToSteward(t *testing.T) {
	initiativeID := "at-x9"
	fbd := &stewardRouteFakeBD{initiativeID: initiativeID, issueLabels: []string{"human", "gate:review"}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	requireStewardMarker(t, ctx)

	askFile := makeTempFile(t, "Should we ship the release?")
	if err := notifyToSteward(ctx, initiativeID, askFile, nil); err != nil {
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

// TestNotifyToSteward_PerPRReviewLabel_YieldsReviewKind is the regression
// test for agent-teams-ssib.32: a per-PR "gate:review:<pr-url>" label (the
// form written once a PR is resolved, docs/multi-pr-contract.md §3) must
// still produce StewardGateKindReview. Before the fix, gateKind's bare-only
// comparison fell through to StewardGateKindQuestion here, misrouting the
// Steward-facing notification for every multi-PR review gate.
func TestNotifyToSteward_PerPRReviewLabel_YieldsReviewKind(t *testing.T) {
	initiativeID := "at-x13"
	fbd := &stewardRouteFakeBD{
		initiativeID: initiativeID,
		issueLabels:  []string{"human", "gate:review:https://github.com/acme/widget/pull/9"},
	}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	requireStewardMarker(t, ctx)

	askFile := makeTempFile(t, "Should we ship the release?")
	if err := notifyToSteward(ctx, initiativeID, askFile, nil); err != nil {
		t.Fatalf("notifyToSteward: unexpected error: %v", err)
	}

	env, ok := ParseStewardGateEnvelope(fbd.createBody)
	if !ok {
		t.Fatalf("message body is not a well-formed steward-gate envelope:\n%s", fbd.createBody)
	}
	if env.Kind != StewardGateKindReview {
		t.Errorf("envelope Kind = %q, want %q (per-PR review label misrouted)", env.Kind, StewardGateKindReview)
	}
}

func TestNotifyToSteward_NoReviewLabel_DefaultsToQuestionKind(t *testing.T) {
	initiativeID := "at-x10"
	fbd := &stewardRouteFakeBD{initiativeID: initiativeID, issueLabels: []string{"human", "gate:question"}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	requireStewardMarker(t, ctx)

	askFile := makeTempFile(t, "what should we name it?")
	if err := notifyToSteward(ctx, initiativeID, askFile, nil); err != nil {
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
	requireStewardMarker(t, ctx)

	if err := notifyToSteward(ctx, "at-x11", "/no/such/file", nil); err == nil {
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
	requireStewardMarker(t, ctx)
	errBuf := ctx.Stderr.(*bytes.Buffer)

	cmd := &gateKong{
		ID:     "at-5",
		File:   f,
		Kind:   "question",
		notify: notifyToSteward,
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

// TestGate_NotifyToStewardNoMarker_NoMessageAndGateSucceeds proves the
// agent-teams-e3mq.24 guard end to end through gateKong.Run: with no steward
// marker, the gate records its labels exactly as before, notifyToSteward
// no-ops silently (no "notify failed" warning — a nil return isn't a notify
// failure), and no bd show/create calls fire for steward routing.
func TestGate_NotifyToStewardNoMarker_NoMessageAndGateSucceeds(t *testing.T) {
	f := makeTempFile(t, "should we proceed?")
	ctx, calls := newCtx(t, []fakeResp{
		{stdout: "ok"}, // note
		{stdout: "ok"}, // label add human
		{stdout: "ok"}, // label add gate:question
	})
	// No requireStewardMarker: this machine has no steward configured.
	errBuf := ctx.Stderr.(*bytes.Buffer)

	cmd := &gateKong{
		ID:     "at-6",
		File:   f,
		Kind:   "question",
		notify: notifyToSteward,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("gate must succeed with no steward marker, got: %v", err)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 bd calls (gate only; notifyToSteward no-ops before any bd show/create), got %d", len(*calls))
	}
	if strings.Contains(errBuf.String(), "warning") {
		t.Errorf("expected no warning on stderr (nil is a clean no-op, not a notify failure), got: %q", errBuf.String())
	}
}

// ── gate -> notifyToSteward: routes with no transport configured ────────────

// TestGate_NotifyToStewardMarkerPresent_RoutesEndToEnd is the regression test
// for agent-teams-e3mq.26: gateKong is wired exactly as RegisterWriteKong
// wires it in production (notify: notifyToSteward, no enabled/transport
// field — there is no such field anymore). With the steward marker present,
// the gate must route to the Steward end to end — labels set, initiative
// looked up, envelope built, message bead created with the Steward as
// assignee — even though nothing here configures or checks any transport
// (Telegram, phone, etc.). Before the fix, this path additionally required
// transport.Enabled(ctx.Home) to return true, so a steward-only machine with
// no Telegram config would silently never reach here.
func TestGate_NotifyToStewardMarkerPresent_RoutesEndToEnd(t *testing.T) {
	initiativeID := "at-e3mq26"
	f := makeTempFile(t, "should we ship the release?")
	fbd := &stewardRouteFakeBD{initiativeID: initiativeID, issueLabels: []string{"human", "gate:review"}}
	ctx, _, _ := makeCtx(fbd, t.TempDir())
	requireStewardMarker(t, ctx)

	cmd := &gateKong{
		ID:     initiativeID,
		File:   f,
		Kind:   "review",
		notify: notifyToSteward,
	}
	if err := cmd.Run(ctx); err != nil {
		t.Fatalf("gate must succeed, got: %v", err)
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
}
