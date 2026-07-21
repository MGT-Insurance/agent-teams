package verbs_test

import (
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
	if got, want := verbs.StewardDirectThreadPath(ctx), filepath.Join("/fake/home", "steward", "direct-thread"); got != want {
		t.Errorf("StewardDirectThreadPath = %q, want %q", got, want)
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

// TestStewardEnvelopes_CrossParserRejection feeds each of the five envelope
// formats to all five parsers and confirms only the matching parser accepts
// — no cross-match between gate/reply/hung-wake/closed-initiative/direct.
func TestStewardEnvelopes_CrossParserRejection(t *testing.T) {
	gateText, err := verbs.BuildStewardGateEnvelope("agent-teams-e3mq", verbs.StewardGateKindQuestion, "body")
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
	directText, err := verbs.BuildStewardDirectEnvelope("body")
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
// schema round-trips through JSON with the "briefing"/"direct" field names.
func TestStewardTopicsRecord_MarshalParseRoundTrip(t *testing.T) {
	rec := verbs.StewardTopicsRecord{Briefing: "topic-1", Direct: "topic-2"}

	value, err := rec.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := `{"briefing":"topic-1","direct":"topic-2"}`; value != want {
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
