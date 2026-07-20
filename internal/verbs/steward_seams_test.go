package verbs_test

import (
	"path/filepath"
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
	if got, want := verbs.StewardDirectThreadPath(ctx), filepath.Join("/fake/home", "steward", "direct-thread"); got != want {
		t.Errorf("StewardDirectThreadPath = %q, want %q", got, want)
	}
	if got, want := verbs.StewardDoorbellPath(ctx), filepath.Join("/fake/home", "mailbox", "steward.wake"); got != want {
		t.Errorf("StewardDoorbellPath = %q, want %q", got, want)
	}
}

// ── Gate→Steward envelope round-trip ─────────────────────────────────────────

func TestStewardGateEnvelope_RoundTrip(t *testing.T) {
	body := "Which design to pick?\n\nRecommended: A\nAlternative: B"
	text, err := verbs.BuildStewardGateEnvelope("agent-teams-e3mq", verbs.StewardGateKindQuestion, body)
	if err != nil {
		t.Fatalf("BuildStewardGateEnvelope: %v", err)
	}

	want := "<<<steward-gate initiative:agent-teams-e3mq kind:question>>>\n" + body + "\n>>>"
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
	if got.Body != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestStewardGateEnvelope_InvalidKind(t *testing.T) {
	if _, err := verbs.BuildStewardGateEnvelope("id", verbs.StewardGateKind("bogus"), "body"); err == nil {
		t.Fatal("BuildStewardGateEnvelope: expected error for invalid kind, got nil")
	}
}

func TestParseStewardGateEnvelope_Malformed(t *testing.T) {
	if _, ok := verbs.ParseStewardGateEnvelope("not an envelope"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false for non-envelope text")
	}
	if _, ok := verbs.ParseStewardGateEnvelope("<<<steward-gate initiative:x kind:question>>>\nbody with no closer"); ok {
		t.Error("ParseStewardGateEnvelope: expected ok=false for missing closing sentinel")
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

// ── Direct→Steward envelope round-trip ───────────────────────────────────────

func TestStewardDirectEnvelope_RoundTrip(t *testing.T) {
	body := "Hey Steward, what's the status on the >>> deploy?\n\nAlso: any blockers?"
	text, err := verbs.BuildStewardDirectEnvelope(body)
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
}

func TestParseStewardDirectEnvelope_Malformed(t *testing.T) {
	if _, ok := verbs.ParseStewardDirectEnvelope("not an envelope"); ok {
		t.Error("ParseStewardDirectEnvelope: expected ok=false for non-envelope text")
	}
	if _, ok := verbs.ParseStewardDirectEnvelope("<<<steward-direct>>>\nbody with no closer"); ok {
		t.Error("ParseStewardDirectEnvelope: expected ok=false for missing closing sentinel")
	}
}

// ── Cross-parser rejection matrix ────────────────────────────────────────────

// TestStewardEnvelopes_CrossParserRejection feeds each of the four envelope
// formats to all four parsers and confirms only the matching parser accepts
// — no cross-match between gate/reply/closed-initiative/direct.
func TestStewardEnvelopes_CrossParserRejection(t *testing.T) {
	gateText, err := verbs.BuildStewardGateEnvelope("agent-teams-e3mq", verbs.StewardGateKindQuestion, "body")
	if err != nil {
		t.Fatalf("BuildStewardGateEnvelope: %v", err)
	}
	replyText, err := verbs.BuildStewardReplyEnvelope("agent-teams-e3mq", "body")
	if err != nil {
		t.Fatalf("BuildStewardReplyEnvelope: %v", err)
	}
	closedInitiativeText, err := verbs.BuildStewardClosedInitiativeEnvelope("agent-teams-e3mq", "body")
	if err != nil {
		t.Fatalf("BuildStewardClosedInitiativeEnvelope: %v", err)
	}
	directText, err := verbs.BuildStewardDirectEnvelope("body")
	if err != nil {
		t.Fatalf("BuildStewardDirectEnvelope: %v", err)
	}

	texts := map[string]string{
		"gate":              gateText,
		"reply":             replyText,
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
}

func TestStewardLedgerRecord_ValidateRejectsBadEnums(t *testing.T) {
	base := verbs.StewardLedgerRecord{
		Timestamp:      time.Now(),
		Category:       verbs.StewardLedgerCategoryScopeCall,
		Initiative:     "agent-teams-e3mq",
		Recommendation: "do the thing",
		Verdict:        verbs.StewardLedgerVerdictCorrected,
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
