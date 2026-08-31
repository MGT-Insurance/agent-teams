package verbs_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/verbs"
)

// ── path helpers ──────────────────────────────────────────────────────────────

func TestStewardPaths(t *testing.T) {
	ctx := &cli.Context{Home: "/fake/home"}

	if got, want := verbs.StewardHome(ctx), filepath.Join("/fake/home", "steward"); got != want {
		t.Errorf("StewardHome = %q, want %q", got, want)
	}
	if got, want := verbs.StewardSessionDir(ctx), filepath.Join("/fake/home", "steward", "session"); got != want {
		t.Errorf("StewardSessionDir = %q, want %q", got, want)
	}
	if got, want := verbs.StewardSessionMarkerPath(ctx), filepath.Join("/fake/home", "steward", "session", ".steward-session"); got != want {
		t.Errorf("StewardSessionMarkerPath = %q, want %q", got, want)
	}
	if got, want := verbs.StewardLedgerPath(ctx), filepath.Join("/fake/home", "steward", "ledger.jsonl"); got != want {
		t.Errorf("StewardLedgerPath = %q, want %q", got, want)
	}
	if got, want := verbs.StewardBriefingThreadPath(ctx), filepath.Join("/fake/home", "steward", "briefing-thread"); got != want {
		t.Errorf("StewardBriefingThreadPath = %q, want %q", got, want)
	}
	if got, want := verbs.StewardReviewsThreadPath(ctx), filepath.Join("/fake/home", "steward", "reviews-thread"); got != want {
		t.Errorf("StewardReviewsThreadPath = %q, want %q", got, want)
	}
	if got, want := verbs.StewardDoorbellPath(ctx), filepath.Join("/fake/home", "mailbox", "steward.wake"); got != want {
		t.Errorf("StewardDoorbellPath = %q, want %q", got, want)
	}
	if got, want := verbs.StewardFallbackMarkerPath(ctx), filepath.Join("/fake/home", "steward", "fallback-responder"); got != want {
		t.Errorf("StewardFallbackMarkerPath = %q, want %q", got, want)
	}
}

// ── Gate→Steward envelope round-trip ─────────────────────────────────────────

func TestStewardGateEnvelope_RoundTrip(t *testing.T) {
	body := "Which design to pick?\n\nRecommended: A\nAlternative: B"
	text, err := verbs.BuildStewardGateEnvelope("agent-teams-e3mq", verbs.StewardGateKindQuestion, nil, body)
	if err != nil {
		t.Fatalf("BuildStewardGateEnvelope: %v", err)
	}

	want := "<<<steward-gate initiative:agent-teams-e3mq kind:question attachments:0>>>\n" + body + "\n>>>"
	if text != want {
		t.Fatalf("BuildStewardGateEnvelope =\n%q\nwant\n%q", text, want)
	}

	got, ok := verbs.ParseStewardGateEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardGateEnvelope: ok=false, want true")
	}
	if got.InitiativeID != "agent-teams-e3mq" {
		t.Errorf("InitiativeID = %q, want %q", got.InitiativeID, "agent-teams-e3mq")
	}
	if got.Kind != verbs.StewardGateKindQuestion {
		t.Errorf("Kind = %q, want %q", got.Kind, verbs.StewardGateKindQuestion)
	}
	if len(got.Attachments) != 0 {
		t.Errorf("Attachments = %+v, want empty", got.Attachments)
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

// TestStewardGateEnvelope_LegacyNoAttachmentsField pins backward
// compatibility with the pre-agent-teams-n0jt.1 wire format, which had no
// " attachments:<N>" header segment at all: an envelope built by an older
// binary (or already in flight across a rolling deploy) must still parse,
// with Attachments empty rather than ok=false. The literal text here is
// deliberately hardcoded, not built via BuildStewardGateEnvelope — that is
// the whole point of the check.
func TestStewardGateEnvelope_LegacyNoAttachmentsField(t *testing.T) {
	body := "Should we ship the release?"
	text := "<<<steward-gate initiative:agent-teams-e3mq kind:review>>>\n" + body + "\n>>>"

	got, ok := verbs.ParseStewardGateEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardGateEnvelope: ok=false for a legacy envelope, want true")
	}
	if got.Kind != verbs.StewardGateKindReview {
		t.Errorf("Kind = %q, want %q", got.Kind, verbs.StewardGateKindReview)
	}
	if len(got.Attachments) != 0 {
		t.Errorf("Attachments = %+v, want empty", got.Attachments)
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

// TestStewardGateEnvelope_MultiAttachment_RoundTrips is the core-path proof
// for agent-teams-n0jt.1's ACCEPTANCE: a live-test-review gate carrying two
// --attach files (one photo, one document) round-trips through
// Build/Parse with the header's attachments:2 count, a photo line and a
// document line in order, and — since the encoding is TAB-delimited, not
// space-delimited — a path containing a space survives unchanged.
func TestStewardGateEnvelope_MultiAttachment_RoundTrips(t *testing.T) {
	attachments := []verbs.Attachment{
		{Path: "/tmp/proof screenshot.png", Kind: "photo"},
		{Path: "/tmp/network.har", Kind: "document"},
	}
	body := "live test verified end to end"
	text, err := verbs.BuildStewardGateEnvelope("agent-teams-n0jt", verbs.StewardGateKindLiveTestReview, attachments, body)
	if err != nil {
		t.Fatalf("BuildStewardGateEnvelope: %v", err)
	}

	wantHeader := "<<<steward-gate initiative:agent-teams-n0jt kind:live-test-review attachments:2>>>"
	if !strings.HasPrefix(text, wantHeader+"\n") {
		t.Fatalf("BuildStewardGateEnvelope header = %q, want prefix %q", text, wantHeader)
	}

	got, ok := verbs.ParseStewardGateEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardGateEnvelope: ok=false, want true (envelope: %q)", text)
	}
	if got.Kind != verbs.StewardGateKindLiveTestReview {
		t.Errorf("Kind = %q, want %q", got.Kind, verbs.StewardGateKindLiveTestReview)
	}
	if len(got.Attachments) != 2 {
		t.Fatalf("Attachments = %+v, want 2 entries", got.Attachments)
	}
	if got.Attachments[0] != attachments[0] {
		t.Errorf("Attachments[0] = %+v, want %+v", got.Attachments[0], attachments[0])
	}
	if got.Attachments[1] != attachments[1] {
		t.Errorf("Attachments[1] = %+v, want %+v", got.Attachments[1], attachments[1])
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestStewardGateEnvelope_InvalidKind(t *testing.T) {
	if _, err := verbs.BuildStewardGateEnvelope("id", verbs.StewardGateKind("bogus"), nil, "body"); err == nil {
		t.Fatal("BuildStewardGateEnvelope: expected error for invalid kind, got nil")
	}
}

// TestStewardGateEnvelope_InvalidAttachment verifies Build rejects a
// malformed attachment before rendering: an empty path, a path containing
// the envelope's own TAB/newline delimiters, and an unrecognized kind.
func TestStewardGateEnvelope_InvalidAttachment(t *testing.T) {
	for name, attachments := range map[string][]verbs.Attachment{
		"empty path":        {{Path: "", Kind: "photo"}},
		"tab in path":       {{Path: "a\tb.png", Kind: "photo"}},
		"newline in path":   {{Path: "a\nb.png", Kind: "photo"}},
		"unrecognized kind": {{Path: "a.png", Kind: "bogus"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verbs.BuildStewardGateEnvelope("id", verbs.StewardGateKindLiveTestReview, attachments, "body"); err == nil {
				t.Errorf("BuildStewardGateEnvelope: expected error for %s, got nil", name)
			}
		})
	}
}

func TestParseStewardGateEnvelope_Malformed(t *testing.T) {
	if _, ok := verbs.ParseStewardGateEnvelope("not an envelope"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false for non-envelope text")
	}
	if _, ok := verbs.ParseStewardGateEnvelope("<<<steward-gate initiative:x kind:question>>>\nbody with no closer"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false for missing closing sentinel")
	}
	if _, ok := verbs.ParseStewardGateEnvelope("<<<steward-gate initiative:x kind:question attachments:bogus>>>\nbody\n>>>"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false for non-numeric attachments count")
	}
	if _, ok := verbs.ParseStewardGateEnvelope("<<<steward-gate initiative:x kind:question attachments:1>>>\nbody\n>>>"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false when fewer attachment lines than declared")
	}
	if _, ok := verbs.ParseStewardGateEnvelope("<<<steward-gate initiative:x kind:question attachments:1>>>\nbogus-no-tab\nbody\n>>>"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false for an attachment line missing its TAB")
	}
}

// ── Relay→Steward envelope round-trip ────────────────────────────────────────

func TestStewardReplyEnvelope_RoundTrip(t *testing.T) {
	body := "Go with design A."
	text, err := verbs.BuildStewardReplyEnvelope("agent-teams-e3mq", body)
	if err != nil {
		t.Fatalf("BuildStewardReplyEnvelope: %v", err)
	}

	want := "<<<steward-reply initiative:agent-teams-e3mq>>>\n" + body + "\n>>>"
	if text != want {
		t.Fatalf("BuildStewardReplyEnvelope =\n%q\nwant\n%q", text, want)
	}

	got, ok := verbs.ParseStewardReplyEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardReplyEnvelope: ok=false, want true")
	}
	if got.InitiativeID != "agent-teams-e3mq" {
		t.Errorf("InitiativeID = %q, want %q", got.InitiativeID, "agent-teams-e3mq")
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

// ── Hung-wake→Steward envelope round-trip ────────────────────────────────────

func TestStewardHungWakeEnvelope_RoundTrip(t *testing.T) {
	body := "[hung-scan] agent-teams-e3mq (some title) has been STUCK since 2026-07-21T00:00:00Z with no gate raised (wake attempt 1/2). Please check on it."
	text, err := verbs.BuildStewardHungWakeEnvelope("agent-teams-e3mq", body)
	if err != nil {
		t.Fatalf("BuildStewardHungWakeEnvelope: %v", err)
	}

	want := "<<<steward-hung-wake initiative:agent-teams-e3mq>>>\n" + body + "\n>>>"
	if text != want {
		t.Fatalf("BuildStewardHungWakeEnvelope =\n%q\nwant\n%q", text, want)
	}

	gotID, gotBody, ok := verbs.IsStewardHungWake(text)
	if !ok {
		t.Fatalf("IsStewardHungWake: ok=false, want true")
	}
	if gotID != "agent-teams-e3mq" {
		t.Errorf("initiativeID = %q, want %q", gotID, "agent-teams-e3mq")
	}
	if gotBody != body {
		t.Errorf("body = %q, want %q", gotBody, body)
	}

	// A hung-wake envelope must NOT be matched by the steward-reply parser —
	// this is the exact mix-up agent-teams-6rru.16 fixes (a mechanical wake
	// misread as a genuine Eric reply).
	if _, ok := verbs.ParseStewardReplyEnvelope(text); ok {
		t.Errorf("ParseStewardReplyEnvelope(hung-wake text) ok=true, want false")
	}
}

func TestIsStewardHungWake_RejectsStewardReplyEnvelope(t *testing.T) {
	replyText, err := verbs.BuildStewardReplyEnvelope("agent-teams-e3mq", "Go with design A.")
	if err != nil {
		t.Fatalf("BuildStewardReplyEnvelope: %v", err)
	}
	if _, _, ok := verbs.IsStewardHungWake(replyText); ok {
		t.Errorf("IsStewardHungWake(reply text) ok=true, want false")
	}
}

// ── Closed-initiative→Steward envelope round-trip ────────────────────────────

func TestStewardClosedInitiativeEnvelope_RoundTrip(t *testing.T) {
	body := "is this still happening?"
	text, err := verbs.BuildStewardClosedInitiativeEnvelope("agent-teams-7dup", body)
	if err != nil {
		t.Fatalf("BuildStewardClosedInitiativeEnvelope: %v", err)
	}

	want := "<<<steward-closed-initiative initiative:agent-teams-7dup>>>\n" + body + "\n>>>"
	if text != want {
		t.Fatalf("BuildStewardClosedInitiativeEnvelope =\n%q\nwant\n%q", text, want)
	}

	got, ok := verbs.ParseStewardClosedInitiativeEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardClosedInitiativeEnvelope: ok=false, want true")
	}
	if got.InitiativeID != "agent-teams-7dup" {
		t.Errorf("InitiativeID = %q, want %q", got.InitiativeID, "agent-teams-7dup")
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestStewardClosedInitiativeEnvelope_EmptyID(t *testing.T) {
	if _, err := verbs.BuildStewardClosedInitiativeEnvelope("", "body"); err == nil {
		t.Error("BuildStewardClosedInitiativeEnvelope: expected error for empty initiative id, got nil")
	}
}

func TestParseStewardClosedInitiativeEnvelope_Malformed(t *testing.T) {
	if _, ok := verbs.ParseStewardClosedInitiativeEnvelope("not an envelope"); ok {
		t.Error("ParseStewardClosedInitiativeEnvelope: expected ok=false for non-envelope text")
	}
	if _, ok := verbs.ParseStewardClosedInitiativeEnvelope("<<<steward-closed-initiative initiative:at-1>>>\nbody with no closer"); ok {
		t.Error("ParseStewardClosedInitiativeEnvelope: expected ok=false for missing closing sentinel")
	}
}

// ── Unrouted→Steward envelope round-trip ─────────────────────────────────────

func TestStewardUnroutedEnvelope_RoundTrip(t *testing.T) {
	body := "reply text"
	text, err := verbs.BuildStewardUnroutedEnvelope("52", "ambiguous: 2 open initiatives", body)
	if err != nil {
		t.Fatalf("BuildStewardUnroutedEnvelope: %v", err)
	}

	want := "<<<steward-unrouted thread:52 reason:ambiguous: 2 open initiatives>>>\n" + body + "\n>>>"
	if text != want {
		t.Fatalf("BuildStewardUnroutedEnvelope =\n%q\nwant\n%q", text, want)
	}

	got, ok := verbs.ParseStewardUnroutedEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardUnroutedEnvelope: ok=false, want true")
	}
	if got.ThreadRef != "52" {
		t.Errorf("ThreadRef = %q, want %q", got.ThreadRef, "52")
	}
	if got.Reason != "ambiguous: 2 open initiatives" {
		t.Errorf("Reason = %q, want %q", got.Reason, "ambiguous: 2 open initiatives")
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

// TestStewardUnroutedEnvelope_ReasonWithEmbeddedNewline verifies
// agent-teams-8beo.3's newline-safety fix: a reason containing an embedded
// newline plus multi-line text (the real shape produced when relay.go
// wraps a bd CLI error — see internal/bd/bd.go's Client.Run, which formats
// failures as fmt.Errorf("bd %s: %w\n%s", ..., stderrText)) must not corrupt
// the single-line sentinel header. Before this fix, an embedded newline
// pushed the closing ">>>" of the header onto a later line, and
// ParseStewardUnroutedEnvelope — which expects the whole header on the
// first line — failed to parse the envelope at all.
func TestStewardUnroutedEnvelope_ReasonWithEmbeddedNewline(t *testing.T) {
	reason := "bd query error: bd list --status=open: exit status 1\nsome multi-line\nstderr output"
	body := "reply text"
	text, err := verbs.BuildStewardUnroutedEnvelope("52", reason, body)
	if err != nil {
		t.Fatalf("BuildStewardUnroutedEnvelope: %v", err)
	}

	headerEnd := len(text)
	if nl := strings.IndexByte(text, '\n'); nl != -1 {
		headerEnd = nl
	}
	if !strings.HasSuffix(text[:headerEnd], ">>>") {
		t.Fatalf("BuildStewardUnroutedEnvelope: header line does not end with the closing sentinel: %q", text[:headerEnd])
	}

	got, ok := verbs.ParseStewardUnroutedEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardUnroutedEnvelope: ok=false, want true (envelope: %q)", text)
	}
	if got.ThreadRef != "52" {
		t.Errorf("ThreadRef = %q, want %q", got.ThreadRef, "52")
	}
	if strings.Contains(got.Reason, "\n") {
		t.Errorf("Reason still contains an embedded newline: %q", got.Reason)
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestStewardUnroutedEnvelope_EmptyReason(t *testing.T) {
	if _, err := verbs.BuildStewardUnroutedEnvelope("52", "", "body"); err == nil {
		t.Error("BuildStewardUnroutedEnvelope: expected error for empty reason, got nil")
	}
	if _, err := verbs.BuildStewardUnroutedEnvelope("52", "\n  \n", "body"); err == nil {
		t.Error("BuildStewardUnroutedEnvelope: expected error for all-whitespace reason, got nil")
	}
}

func TestStewardUnroutedEnvelope_EmptyThreadRef(t *testing.T) {
	if _, err := verbs.BuildStewardUnroutedEnvelope("", "reason", "body"); err == nil {
		t.Error("BuildStewardUnroutedEnvelope: expected error for empty thread ref, got nil")
	}
}

func TestParseStewardUnroutedEnvelope_Malformed(t *testing.T) {
	if _, ok := verbs.ParseStewardUnroutedEnvelope("not an envelope"); ok {
		t.Error("ParseStewardUnroutedEnvelope: expected ok=false for non-envelope text")
	}
	if _, ok := verbs.ParseStewardUnroutedEnvelope("<<<steward-unrouted thread:52 reason:x>>>\nbody with no closer"); ok {
		t.Error("ParseStewardUnroutedEnvelope: expected ok=false for missing closing sentinel")
	}
}

// ── Direct→Steward envelope round-trip ───────────────────────────────────────

func TestStewardDirectEnvelope_RoundTrip(t *testing.T) {
	body := "Hey Steward, what's the status on the >>> deploy?\n\nAlso: any blockers?"
	text, err := verbs.BuildStewardDirectEnvelope("", body)
	if err != nil {
		t.Fatalf("BuildStewardDirectEnvelope: %v", err)
	}

	want := "<<<steward-direct>>>\n" + body + "\n>>>"
	if text != want {
		t.Fatalf("BuildStewardDirectEnvelope =\n%q\nwant\n%q", text, want)
	}

	got, ok := verbs.ParseStewardDirectEnvelope(text)
	if !ok {
		t.Fatalf("ParseStewardDirectEnvelope: ok=false, want true")
	}
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
	if got.ReplyTo != "" {
		t.Errorf("ReplyTo = %q, want empty", got.ReplyTo)
	}
}

// TestStewardDirectEnvelope_RoundTripWithReplyTo covers the DM shape
// (agent-teams-ncn5.9): the opaque reply-to ref rides in the sentinel header
// and comes back out of Parse byte-identical. The refs below deliberately
// differ in shape — a composite "<chat>:<message>" and a bare id — because
// nothing in this package may depend on which one the transport emits.
func TestStewardDirectEnvelope_RoundTripWithReplyTo(t *testing.T) {
	for _, replyTo := range []string{"-1001234567890:88", "88"} {
		t.Run(replyTo, func(t *testing.T) {
			body := "is the >>> deploy done?\n\nsecond line"
			text, err := verbs.BuildStewardDirectEnvelope(replyTo, body)
			if err != nil {
				t.Fatalf("BuildStewardDirectEnvelope: %v", err)
			}

			want := "<<<steward-direct reply-to:" + replyTo + ">>>\n" + body + "\n>>>"
			if text != want {
				t.Fatalf("BuildStewardDirectEnvelope =\n%q\nwant\n%q", text, want)
			}

			got, ok := verbs.ParseStewardDirectEnvelope(text)
			if !ok {
				t.Fatalf("ParseStewardDirectEnvelope: ok=false, want true")
			}
			if got.ReplyTo != replyTo {
				t.Errorf("ReplyTo = %q, want %q", got.ReplyTo, replyTo)
			}
			if got.Body != body {
				t.Errorf("Body = %q, want %q", got.Body, body)
			}
		})
	}
}

// TestParseStewardDirectEnvelope_LegacyNoReplyTo pins backward compatibility
// with the pre-agent-teams-ncn5.9 wire format, which had no header metadata
// at all: an envelope written by an older relay (in flight across a rolling
// restart) must still parse, with ReplyTo empty rather than ok=false. The
// literal text here is deliberately hardcoded, not built — that is the whole
// point of the check.
func TestParseStewardDirectEnvelope_LegacyNoReplyTo(t *testing.T) {
	got, ok := verbs.ParseStewardDirectEnvelope("<<<steward-direct>>>\nhello steward\n>>>")
	if !ok {
		t.Fatalf("ParseStewardDirectEnvelope: ok=false for a legacy envelope, want true")
	}
	if got.ReplyTo != "" {
		t.Errorf("ReplyTo = %q, want empty", got.ReplyTo)
	}
	if got.Body != "hello steward" {
		t.Errorf("Body = %q, want %q", got.Body, "hello steward")
	}
}

func TestParseStewardDirectEnvelope_Malformed(t *testing.T) {
	for _, tt := range []struct{ name, text string }{
		{"non-envelope text", "not an envelope"},
		{"missing closing sentinel", "<<<steward-direct>>>\nbody with no closer"},
		{"missing closing sentinel, with reply-to", "<<<steward-direct reply-to:12:3>>>\nbody with no closer"},
		{"empty reply-to ref", "<<<steward-direct reply-to:>>>\nbody\n>>>"},
		{"unrecognized header metadata", "<<<steward-direct chat:12>>>\nbody\n>>>"},
		{"header not closed", "<<<steward-direct reply-to:12:3\nbody\n>>>"},
	} {
		if _, ok := verbs.ParseStewardDirectEnvelope(tt.text); ok {
			t.Errorf("ParseStewardDirectEnvelope: expected ok=false for %s", tt.name)
		}
	}
}

// ── Cross-parser rejection matrix ────────────────────────────────────────────

// TestStewardEnvelopes_CrossParserRejection feeds each of the five envelope
// formats to all five parsers and confirms only the matching parser accepts
// — no cross-match between gate/reply/hung-wake/closed-initiative/direct.
func TestStewardEnvelopes_CrossParserRejection(t *testing.T) {
	gateText, err := verbs.BuildStewardGateEnvelope("agent-teams-e3mq", verbs.StewardGateKindQuestion, nil, "body")
	if err != nil {
		t.Fatalf("BuildStewardGateEnvelope: %v", err)
	}
	replyText, err := verbs.BuildStewardReplyEnvelope("agent-teams-e3mq", "body")
	if err != nil {
		t.Fatalf("BuildStewardReplyEnvelope: %v", err)
	}
	hungWakeText, err := verbs.BuildStewardHungWakeEnvelope("agent-teams-e3mq", "body")
	if err != nil {
		t.Fatalf("BuildStewardHungWakeEnvelope: %v", err)
	}
	closedInitiativeText, err := verbs.BuildStewardClosedInitiativeEnvelope("agent-teams-e3mq", "body")
	if err != nil {
		t.Fatalf("BuildStewardClosedInitiativeEnvelope: %v", err)
	}
	// With a reply-to ref: the header-metadata variant is the one that could
	// plausibly collide with another format's "<prefix> <key>:<value>" header.
	directText, err := verbs.BuildStewardDirectEnvelope("-100123:88", "body")
	if err != nil {
		t.Fatalf("BuildStewardDirectEnvelope: %v", err)
	}

	texts := map[string]string{
		"gate":              gateText,
		"reply":             replyText,
		"hung-wake":         hungWakeText,
		"closed-initiative": closedInitiativeText,
		"direct":            directText,
	}

	for textName, text := range texts {
		if _, ok := verbs.ParseStewardGateEnvelope(text); ok != (textName == "gate") {
			t.Errorf("ParseStewardGateEnvelope(%s) ok=%v, want %v", textName, ok, textName == "gate")
		}
		if _, ok := verbs.ParseStewardReplyEnvelope(text); ok != (textName == "reply") {
			t.Errorf("ParseStewardReplyEnvelope(%s) ok=%v, want %v", textName, ok, textName == "reply")
		}
		if _, _, ok := verbs.IsStewardHungWake(text); ok != (textName == "hung-wake") {
			t.Errorf("IsStewardHungWake(%s) ok=%v, want %v", textName, ok, textName == "hung-wake")
		}
		if _, ok := verbs.ParseStewardClosedInitiativeEnvelope(text); ok != (textName == "closed-initiative") {
			t.Errorf("ParseStewardClosedInitiativeEnvelope(%s) ok=%v, want %v", textName, ok, textName == "closed-initiative")
		}
		if _, ok := verbs.ParseStewardDirectEnvelope(text); ok != (textName == "direct") {
			t.Errorf("ParseStewardDirectEnvelope(%s) ok=%v, want %v", textName, ok, textName == "direct")
		}
	}
}

// ── Ledger record marshal ─────────────────────────────────────────────────────

func TestStewardLedgerRecord_MarshalParseRoundTrip(t *testing.T) {
	rec := verbs.StewardLedgerRecord{
		Timestamp:      time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		Category:       verbs.StewardLedgerCategoryMergeApproval,
		Initiative:     "agent-teams-e3mq",
		Recommendation: "merge PR #100",
		Verdict:        verbs.StewardLedgerVerdictAccepted,
	}

	line, err := rec.MarshalLine()
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}
	if line[len(line)-1] != '\n' {
		t.Fatalf("MarshalLine: expected trailing newline, got %q", line)
	}

	got, err := verbs.ParseStewardLedgerRecord(line)
	if err != nil {
		t.Fatalf("ParseStewardLedgerRecord: %v", err)
	}
	if !got.Timestamp.Equal(rec.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, rec.Timestamp)
	}
	if got.Category != rec.Category || got.Initiative != rec.Initiative ||
		got.Recommendation != rec.Recommendation || got.Verdict != rec.Verdict {
		t.Errorf("ParseStewardLedgerRecord = %+v, want %+v", got, rec)
	}
	if got.Decision != "" {
		t.Errorf("Decision = %q, want empty (not set on this record)", got.Decision)
	}
}

// TestStewardLedgerRecord_MarshalParseRoundTrip_PreservesDecision verifies
// the agent-teams-7ew5.1 Decision field round-trips through MarshalLine and
// ParseStewardLedgerRecord.
func TestStewardLedgerRecord_MarshalParseRoundTrip_PreservesDecision(t *testing.T) {
	rec := verbs.StewardLedgerRecord{
		Timestamp:      time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		Category:       verbs.StewardLedgerCategoryScopeCall,
		Initiative:     "agent-teams-e3mq",
		Recommendation: "narrow scope to API layer",
		Verdict:        verbs.StewardLedgerVerdictCorrected,
		Decision:       "keep the UI layer too, just stub the backend",
	}

	line, err := rec.MarshalLine()
	if err != nil {
		t.Fatalf("MarshalLine: %v", err)
	}

	got, err := verbs.ParseStewardLedgerRecord(line)
	if err != nil {
		t.Fatalf("ParseStewardLedgerRecord: %v", err)
	}
	if got.Decision != rec.Decision {
		t.Errorf("Decision = %q, want %q", got.Decision, rec.Decision)
	}
}

// TestParseStewardLedgerRecord_ToleratesLegacyLineWithoutDecision verifies a
// ledger line written before agent-teams-7ew5.1 (no "decision" key) still
// parses cleanly, with Decision defaulting to "".
func TestParseStewardLedgerRecord_ToleratesLegacyLineWithoutDecision(t *testing.T) {
	line := `{"ts":"2026-07-13T12:00:00Z","category":"merge-approval","initiative":"agent-teams-e3mq","recommendation":"merge PR #100","verdict":"accepted"}`

	got, err := verbs.ParseStewardLedgerRecord([]byte(line))
	if err != nil {
		t.Fatalf("ParseStewardLedgerRecord: %v", err)
	}
	if got.Decision != "" {
		t.Errorf("Decision = %q, want empty for legacy line", got.Decision)
	}
	if got.Verdict != verbs.StewardLedgerVerdictAccepted {
		t.Errorf("Verdict = %q, want accepted", got.Verdict)
	}
}

// ── Synced steward-topics record (agent-teams-5y8a.1) ────────────────────────

// TestStewardTopicsKey verifies the frozen key convention:
// "steward:topics:<hostname>".
func TestStewardTopicsKey(t *testing.T) {
	if got, want := verbs.StewardTopicsKey("machine-a"), "steward:topics:machine-a"; got != want {
		t.Errorf("StewardTopicsKey(%q) = %q, want %q", "machine-a", got, want)
	}
}

// TestStewardTopicsRecord_MarshalParseRoundTrip verifies the frozen value
// schema round-trips through JSON with the "briefing" and "reviews" field
// names (agent-teams-p9dm.7 added Reviews alongside the pre-existing
// Briefing field).
func TestStewardTopicsRecord_MarshalParseRoundTrip(t *testing.T) {
	rec := verbs.StewardTopicsRecord{Briefing: "topic-1", Reviews: "topic-2"}

	value, err := rec.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"briefing":"topic-1","reviews":"topic-2"}`; value != want {
		t.Fatalf("Marshal = %q, want %q", value, want)
	}

	got, err := verbs.ParseStewardTopicsRecord(value)
	if err != nil {
		t.Fatalf("ParseStewardTopicsRecord: %v", err)
	}
	if got != rec {
		t.Errorf("ParseStewardTopicsRecord = %+v, want %+v", got, rec)
	}
}

// TestParseStewardTopicsRecord_Malformed verifies invalid JSON is rejected.
func TestParseStewardTopicsRecord_Malformed(t *testing.T) {
	if _, err := verbs.ParseStewardTopicsRecord("not json"); err == nil {
		t.Error("ParseStewardTopicsRecord: expected error for malformed JSON, got nil")
	}
}

// TestParseStewardTopicsRecord_ToleratesLegacyDirectField verifies
// agent-teams-4x83's schema change (dropping StewardTopicsRecord.Direct)
// stays backward-compatible with records already published by peers on the
// older schema: a value still carrying a "direct" key must parse cleanly,
// with Direct simply dropped (Go's json.Unmarshal ignores unknown fields by
// default). The fixture also carries no "reviews" key, so this doubles as
// coverage for agent-teams-p9dm.7's schema addition: a record from a peer
// still on the OLDER schema (pre-Reviews) must parse with Reviews == "",
// not fail — asserted implicitly by the struct equality below, since `want`
// leaves Reviews at its zero value.
func TestParseStewardTopicsRecord_ToleratesLegacyDirectField(t *testing.T) {
	got, err := verbs.ParseStewardTopicsRecord(`{"briefing":"12","direct":"9"}`)
	if err != nil {
		t.Fatalf("ParseStewardTopicsRecord: %v", err)
	}
	if want := (verbs.StewardTopicsRecord{Briefing: "12"}); got != want {
		t.Errorf("ParseStewardTopicsRecord = %+v, want %+v", got, want)
	}
}

// ── Shared PR-review topic (agent-teams-p9dm.7) ──────────────────────────────

// TestReviewsStartLineFormat verifies the frozen two-line message the
// dispatch --topic path posts to the shared Reviews topic renders verbatim
// byte-for-byte, for both the title-available case and the prTitleFunc
// fail-soft no-title case (agent-teams-p9dm.7's title-segment convention:
// one frozen format string, the caller pre-composes the segment — " — " +
// title when present, "" when absent — rather than a second
// ...NoTitleFormat constant). No finding counts, no severity, no verdict in
// either case.
func TestReviewsStartLineFormat(t *testing.T) {
	withTitle := fmt.Sprintf(verbs.ReviewsStartLineFormat, "4517", "midgard", " — Fix flaky retry logic", "https://github.com/MGT-Insurance/midgard/pull/4517")
	if want := "Review started · #4517 midgard — Fix flaky retry logic\nhttps://github.com/MGT-Insurance/midgard/pull/4517"; withTitle != want {
		t.Errorf("ReviewsStartLineFormat (with title) = %q, want %q", withTitle, want)
	}

	withoutTitle := fmt.Sprintf(verbs.ReviewsStartLineFormat, "4517", "midgard", "", "https://github.com/MGT-Insurance/midgard/pull/4517")
	if want := "Review started · #4517 midgard\nhttps://github.com/MGT-Insurance/midgard/pull/4517"; withoutTitle != want {
		t.Errorf("ReviewsStartLineFormat (no title, fail-soft) = %q, want %q", withoutTitle, want)
	}
}

func TestStewardLedgerRecord_ValidateRejectsBadEnums(t *testing.T) {
	base := verbs.StewardLedgerRecord{
		Timestamp:      time.Now(),
		Category:       verbs.StewardLedgerCategoryScopeCall,
		Initiative:     "agent-teams-e3mq",
		Recommendation: "do the thing",
		Verdict:        verbs.StewardLedgerVerdictCorrected,
		Decision:       "narrow to just the API layer",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("Validate on well-formed record: %v", err)
	}

	badCategory := base
	badCategory.Category = "not-a-category"
	if err := badCategory.Validate(); err == nil {
		t.Error("Validate: expected error for invalid category, got nil")
	}

	badVerdict := base
	badVerdict.Verdict = "maybe"
	if err := badVerdict.Validate(); err == nil {
		t.Error("Validate: expected error for invalid verdict, got nil")
	}

	noInitiative := base
	noInitiative.Initiative = ""
	if err := noInitiative.Validate(); err == nil {
		t.Error("Validate: expected error for empty initiative, got nil")
	}
}

// TestStewardLedgerRecord_ValidateRequiresDecisionOnCorrected verifies the
// agent-teams-7ew5.1 rule: verdict=corrected requires a non-empty Decision
// (what Eric actually decided), while verdict=accepted leaves it optional.
func TestStewardLedgerRecord_ValidateRequiresDecisionOnCorrected(t *testing.T) {
	base := verbs.StewardLedgerRecord{
		Timestamp:      time.Now(),
		Category:       verbs.StewardLedgerCategoryScopeCall,
		Initiative:     "agent-teams-e3mq",
		Recommendation: "do the thing",
	}

	correctedNoDecision := base
	correctedNoDecision.Verdict = verbs.StewardLedgerVerdictCorrected
	if err := correctedNoDecision.Validate(); err == nil {
		t.Error("Validate: expected error for corrected verdict with empty Decision, got nil")
	}

	correctedWithDecision := base
	correctedWithDecision.Verdict = verbs.StewardLedgerVerdictCorrected
	correctedWithDecision.Decision = "narrow to just the API layer"
	if err := correctedWithDecision.Validate(); err != nil {
		t.Errorf("Validate: unexpected error for corrected verdict with Decision set: %v", err)
	}

	acceptedNoDecision := base
	acceptedNoDecision.Verdict = verbs.StewardLedgerVerdictAccepted
	if err := acceptedNoDecision.Validate(); err != nil {
		t.Errorf("Validate: unexpected error for accepted verdict with empty Decision: %v", err)
	}
}
