// This file is owned by the at-wolk CONTRACT track (agent-teams-e3mq.1). It
// freezes the shared vocabulary — reserved handles/paths, the two envelope
// formats, and the ledger record schema — that LOOP Tracks A, B, C, E, and F
// (agent-teams-e3mq.2 through .5, and the follow-on wiring track) depend on.
// Every other track imports this file read-only; only the contract track
// edits it.
//
// ── The Steward loop, in one paragraph ──────────────────────────────────────
//
// The Steward is a long-running, machine-scoped persona — not tied to any one
// initiative — that watches DRI sessions across the machine and gates
// plan/scope/merge/design-fork/unblock decisions through Eric. A gate
// (Track B) or a relay carrying Eric's reply (Track C) hands the Steward a
// self-contained sentinel-delimited envelope (BuildStewardGateEnvelope /
// BuildStewardReplyEnvelope below) via the existing `ateam mail send`
// machinery, addressed to StewardHandle; that machinery also touches the
// doorbell at StewardDoorbellPath. wake-watcher.sh (Track A; NOT modified by
// this file) recognizes the Steward's own session by the presence of
// StewardSessionMarkerPath in its cwd, sets match_id=StewardHandle, and polls
// that doorbell instead of resolving an initiative by worktree line — and
// because the Steward is machine-scoped rather than a closeable initiative,
// its heartbeat re-arm always continues (no stop-on-closed check, unlike a
// normal initiative watcher). Every decision the Steward renders — approve,
// correct, or escalate — is appended to StewardLedgerPath as one JSON line
// per StewardLedgerRecord.
package verbs

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/cli"
)

// ── Reserved handles/paths ───────────────────────────────────────────────────

// StewardHandle is the reserved mailbox recipient id for the Steward
// persona. `ateam mail send steward ...` (sendKong.RecipientID in
// messaging.go) accepts any free-text recipient id — nothing at the
// messaging layer reserves "steward" — so every caller sending to, or
// resolving, the Steward MUST use this constant rather than a literal
// string.
const StewardHandle = "steward"

// BriefingHandle is the reserved `ateam notify` recipient id for the
// Steward's high-level, cross-initiative briefing topic. Unlike a normal
// `ateam notify <initiative-id>` call, no bead lives behind this id — notify
// reads/writes its thread ref from StewardBriefingThreadPath instead of an
// initiative bead's "thread:<n>" label. Every caller posting to, or
// resolving, the briefing topic MUST use this constant rather than a
// literal string.
const BriefingHandle = "briefing"

// DirectHandle is the reserved `ateam notify` recipient id for the
// Steward's direct-message channel — a dedicated forum topic for messaging
// the Steward directly, outside any initiative context. Like BriefingHandle,
// no bead lives behind this id — notify reads/writes its thread ref from
// StewardDirectThreadPath instead of an initiative bead's "thread:<n>"
// label. Every caller posting to, or resolving, the direct-message topic
// MUST use this constant rather than a literal string. Not StewardHandle:
// that is the mail handle for initiative-scoped Gate/Relay traffic;
// DirectHandle is the notify handle for out-of-band direct traffic.
const DirectHandle = "direct"

const (
	stewardDirName                = "steward"
	stewardSessionDirName         = "session"
	stewardSessionMarkerName      = ".steward-session"
	stewardLedgerFileName         = "ledger.jsonl"
	stewardBriefingThreadFileName = "briefing-thread"
	stewardDirectThreadFileName   = "direct-thread"
	stewardDoorbellFileSuffix     = ".wake"
)

// StewardHome returns the Steward's home directory, <workspace-home>/steward,
// joined against ctx.Home the same way other verbs join workspace-relative
// paths (e.g. messaging.go's sendKong.Run: filepath.Join(ctx.Home, "mailbox")).
func StewardHome(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, stewardDirName)
}

// StewardSessionDir returns the Steward's session directory,
// <StewardHome>/session.
func StewardSessionDir(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), stewardSessionDirName)
}

// StewardSessionMarkerPath returns the marker file that wake-watcher.sh's
// marker-based branch (Track A) checks for under $PWD to identify the
// Steward's own session and set match_id=StewardHandle, bypassing the normal
// "worktree:" line initiative lookup used for every other session.
func StewardSessionMarkerPath(ctx *cli.Context) string {
	return filepath.Join(StewardSessionDir(ctx), stewardSessionMarkerName)
}

// StewardLedgerPath returns the path to the Steward's append-only decision
// ledger: one JSON line per StewardLedgerRecord.
func StewardLedgerPath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), stewardLedgerFileName)
}

// StewardBriefingThreadPath returns the path to the Steward's gated
// high-level briefing-thread file.
func StewardBriefingThreadPath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), stewardBriefingThreadFileName)
}

// StewardDirectThreadPath returns the path to the Steward's direct-message
// channel thread-ref file.
func StewardDirectThreadPath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), stewardDirectThreadFileName)
}

// StewardDoorbellPath returns the doorbell (wake) file wake-watcher.sh polls
// for the Steward: <workspace-home>/mailbox/steward.wake. Mirrors
// sendKong.Run's doorbellPath construction in messaging.go with
// RecipientID=StewardHandle.
func StewardDoorbellPath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, "mailbox", StewardHandle+stewardDoorbellFileSuffix)
}

// ── Envelope formats ──────────────────────────────────────────────────────────
//
// Both envelopes follow the sentinel-delimited convention already used by
// query.go's extractLatestAsk / write.go's buildAskBlock (the "<<<ateam-ask
// ... >>>" block): an opening sentinel, a body, and a closing ">>>" anchored
// to its own line. Unlike ateam-ask, each envelope here folds its routing
// metadata (initiative id, and for the gate the kind) into the opening
// sentinel line itself, so the envelope is fully self-contained — the
// Steward never needs a race-prone re-read of the source bead to learn which
// initiative or kind an ask belongs to.

const stewardEnvelopeClose = ">>>"

// ── Gate→Steward envelope ────────────────────────────────────────────────────

// StewardGateKind enumerates the two structured-ask kinds a gate can send to
// the Steward. Mirrors gateKind's QUESTION|REVIEW resolution in query.go,
// lower-cased for on-the-wire use.
type StewardGateKind string

const (
	StewardGateKindQuestion StewardGateKind = "question"
	StewardGateKindReview   StewardGateKind = "review"
)

// Valid reports whether k is a recognized StewardGateKind.
func (k StewardGateKind) Valid() bool {
	return k == StewardGateKindQuestion || k == StewardGateKindReview
}

const stewardGateOpenPrefix = "<<<steward-gate initiative:"

// StewardGateEnvelope holds the parsed fields of a Gate→Steward envelope.
type StewardGateEnvelope struct {
	InitiativeID string
	Kind         StewardGateKind
	Body         string
}

// BuildStewardGateEnvelope renders the self-contained Gate→Steward envelope:
//
//	<<<steward-gate initiative:<id> kind:<question|review>>>>
//	<body>
//	>>>
//
// body is typically buildAskMessage's output (write.go) — the full
// human-readable ask, carried in full so the Steward needs no further lookup.
func BuildStewardGateEnvelope(initiativeID string, kind StewardGateKind, body string) (string, error) {
	if initiativeID == "" {
		return "", fmt.Errorf("steward gate envelope: initiative id is empty")
	}
	if !kind.Valid() {
		return "", fmt.Errorf("steward gate envelope: invalid kind %q", kind)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s kind:%s%s\n", stewardGateOpenPrefix, initiativeID, kind, stewardEnvelopeClose)
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// ParseStewardGateEnvelope parses an envelope produced by
// BuildStewardGateEnvelope. Returns false when text isn't well-formed: no
// header, no " kind:" separator, an unrecognized kind, or a missing closing
// sentinel line.
func ParseStewardGateEnvelope(text string) (StewardGateEnvelope, bool) {
	if !strings.HasPrefix(text, stewardGateOpenPrefix) {
		return StewardGateEnvelope{}, false
	}
	nl := strings.IndexByte(text, '\n')
	if nl == -1 {
		return StewardGateEnvelope{}, false
	}
	header, rest := text[:nl], text[nl+1:]

	fields, ok := strings.CutSuffix(header[len(stewardGateOpenPrefix):], stewardEnvelopeClose)
	if !ok {
		return StewardGateEnvelope{}, false
	}
	idPart, kindPart, ok := strings.Cut(fields, " kind:")
	if !ok || idPart == "" || kindPart == "" {
		return StewardGateEnvelope{}, false
	}
	kind := StewardGateKind(kindPart)
	if !kind.Valid() {
		return StewardGateEnvelope{}, false
	}

	body, ok := strings.CutSuffix(rest, "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardGateEnvelope{}, false
	}

	return StewardGateEnvelope{InitiativeID: idPart, Kind: kind, Body: body}, true
}

// ── Relay→Steward envelope ───────────────────────────────────────────────────

const stewardReplyOpenPrefix = "<<<steward-reply initiative:"

// StewardReplyEnvelope holds the parsed fields of a Relay→Steward envelope
// (Eric's reply, relayed to the Steward so it knows which DRI the reply
// answers).
type StewardReplyEnvelope struct {
	InitiativeID string
	Body         string
}

// BuildStewardReplyEnvelope renders the self-contained Relay→Steward
// envelope:
//
//	<<<steward-reply initiative:<id>>>>
//	<body>
//	>>>
func BuildStewardReplyEnvelope(initiativeID, body string) (string, error) {
	if initiativeID == "" {
		return "", fmt.Errorf("steward reply envelope: initiative id is empty")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s\n", stewardReplyOpenPrefix, initiativeID, stewardEnvelopeClose)
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// ParseStewardReplyEnvelope parses an envelope produced by
// BuildStewardReplyEnvelope. Returns false when text isn't well-formed: no
// header or a missing closing sentinel line.
func ParseStewardReplyEnvelope(text string) (StewardReplyEnvelope, bool) {
	if !strings.HasPrefix(text, stewardReplyOpenPrefix) {
		return StewardReplyEnvelope{}, false
	}
	nl := strings.IndexByte(text, '\n')
	if nl == -1 {
		return StewardReplyEnvelope{}, false
	}
	header, rest := text[:nl], text[nl+1:]

	idPart, ok := strings.CutSuffix(header[len(stewardReplyOpenPrefix):], stewardEnvelopeClose)
	if !ok || idPart == "" {
		return StewardReplyEnvelope{}, false
	}

	body, ok := strings.CutSuffix(rest, "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardReplyEnvelope{}, false
	}

	return StewardReplyEnvelope{InitiativeID: idPart, Body: body}, true
}

// ── Closed-initiative→Steward envelope ───────────────────────────────────────
//
// The relay safety net (agent-teams-7dup.2): a human reply arrives in a
// topic whose owning initiative has since closed. Reopening a topic in the
// Telegram UI does not change beads state, so the relay keys off the
// initiative's beads status, not Telegram topic state — see
// relayKong.routeClosedInitiativeSafetyNet (relay.go). Distinct from
// StewardReplyEnvelope so the Steward can tell the two apart without
// re-querying bd: an ordinary reply answers an in-flight gate/ask, while a
// closed-initiative message needs the Steward to decide, at its judgment via
// the normal Eric-gated flow, whether to answer directly or re-dispatch
// (e.g. `ateam reopen <id>` / /dispatch-dri).

const stewardClosedInitiativeOpenPrefix = "<<<steward-closed-initiative initiative:"

// StewardClosedInitiativeEnvelope holds the parsed fields of a
// Closed-initiative→Steward envelope.
type StewardClosedInitiativeEnvelope struct {
	InitiativeID string
	Body         string
}

// BuildStewardClosedInitiativeEnvelope renders the self-contained
// Closed-initiative→Steward envelope:
//
//	<<<steward-closed-initiative initiative:<id>>>>
//	<body>
//	>>>
func BuildStewardClosedInitiativeEnvelope(initiativeID, body string) (string, error) {
	if initiativeID == "" {
		return "", fmt.Errorf("steward closed-initiative envelope: initiative id is empty")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s\n", stewardClosedInitiativeOpenPrefix, initiativeID, stewardEnvelopeClose)
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// ParseStewardClosedInitiativeEnvelope parses an envelope produced by
// BuildStewardClosedInitiativeEnvelope. Returns false when text isn't
// well-formed: no header or a missing closing sentinel line.
func ParseStewardClosedInitiativeEnvelope(text string) (StewardClosedInitiativeEnvelope, bool) {
	if !strings.HasPrefix(text, stewardClosedInitiativeOpenPrefix) {
		return StewardClosedInitiativeEnvelope{}, false
	}
	nl := strings.IndexByte(text, '\n')
	if nl == -1 {
		return StewardClosedInitiativeEnvelope{}, false
	}
	header, rest := text[:nl], text[nl+1:]

	idPart, ok := strings.CutSuffix(header[len(stewardClosedInitiativeOpenPrefix):], stewardEnvelopeClose)
	if !ok || idPart == "" {
		return StewardClosedInitiativeEnvelope{}, false
	}

	body, ok := strings.CutSuffix(rest, "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardClosedInitiativeEnvelope{}, false
	}

	return StewardClosedInitiativeEnvelope{InitiativeID: idPart, Body: body}, true
}

// ── Direct→Steward envelope ──────────────────────────────────────────────────
//
// Unlike the gate/reply envelopes above, a direct message carries no
// initiative id — it's explicitly out-of-band, so the open sentinel is a
// fixed header line rather than a prefix with trailing metadata.

const stewardDirectOpenPrefix = "<<<steward-direct>>>"

// StewardDirectEnvelope holds the parsed fields of a Direct→Steward
// envelope (Eric messaging the Steward directly, outside any initiative
// context).
type StewardDirectEnvelope struct {
	Body string
}

// BuildStewardDirectEnvelope renders the self-contained Direct→Steward
// envelope:
//
//	<<<steward-direct>>>
//	<body>
//	>>>
func BuildStewardDirectEnvelope(body string) (string, error) {
	var b strings.Builder
	b.WriteString(stewardDirectOpenPrefix)
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// ParseStewardDirectEnvelope parses an envelope produced by
// BuildStewardDirectEnvelope. Returns false when text isn't well-formed: no
// header line or a missing closing sentinel line.
func ParseStewardDirectEnvelope(text string) (StewardDirectEnvelope, bool) {
	header := stewardDirectOpenPrefix + "\n"
	if !strings.HasPrefix(text, header) {
		return StewardDirectEnvelope{}, false
	}
	body, ok := strings.CutSuffix(text[len(header):], "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardDirectEnvelope{}, false
	}
	return StewardDirectEnvelope{Body: body}, true
}

// ── Briefing-reply→Steward envelope ──────────────────────────────────────────
//
// A human reply posted in the Steward's Briefings topic (BriefingHandle,
// StewardBriefingThreadPath) has no bead behind it by design — `ateam notify
// briefing` maintains that topic's thread ref outside any initiative bead's
// "thread:<n>" label, so the relay's bd label lookup would always miss and
// the message would die silently (agent-teams-8beo.1). Distinct from
// steward-direct: the reply surface differs (Briefings, a cross-initiative
// broadcast topic, vs the Steward's dedicated 1:1 direct channel). Distinct
// from steward-reply: like steward-direct, this carries no initiative id —
// it's content-addressed (the Steward reads the reply against recent
// briefing context and decides where it belongs), not thread-addressed to a
// single known initiative.

const stewardBriefingReplyOpenPrefix = "<<<steward-briefing-reply>>>"

// StewardBriefingReplyEnvelope holds the parsed fields of a
// Briefing-reply→Steward envelope.
type StewardBriefingReplyEnvelope struct {
	Body string
}

// BuildStewardBriefingReplyEnvelope renders the self-contained
// Briefing-reply→Steward envelope:
//
//	<<<steward-briefing-reply>>>
//	<body>
//	>>>
func BuildStewardBriefingReplyEnvelope(body string) (string, error) {
	var b strings.Builder
	b.WriteString(stewardBriefingReplyOpenPrefix)
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// ParseStewardBriefingReplyEnvelope parses an envelope produced by
// BuildStewardBriefingReplyEnvelope. Returns false when text isn't
// well-formed: no header line or a missing closing sentinel line.
func ParseStewardBriefingReplyEnvelope(text string) (StewardBriefingReplyEnvelope, bool) {
	header := stewardBriefingReplyOpenPrefix + "\n"
	if !strings.HasPrefix(text, header) {
		return StewardBriefingReplyEnvelope{}, false
	}
	body, ok := strings.CutSuffix(text[len(header):], "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardBriefingReplyEnvelope{}, false
	}
	return StewardBriefingReplyEnvelope{Body: body}, true
}

// ── Ledger record ─────────────────────────────────────────────────────────────

// StewardLedgerCategory enumerates the categories of decision the Steward
// records.
type StewardLedgerCategory string

const (
	StewardLedgerCategoryPlanApproval  StewardLedgerCategory = "plan-approval"
	StewardLedgerCategoryScopeCall     StewardLedgerCategory = "scope-call"
	StewardLedgerCategoryMergeApproval StewardLedgerCategory = "merge-approval"
	StewardLedgerCategoryDesignFork    StewardLedgerCategory = "design-fork"
	StewardLedgerCategoryUnblockAction StewardLedgerCategory = "unblock-action"
)

// Valid reports whether c is a recognized StewardLedgerCategory.
func (c StewardLedgerCategory) Valid() bool {
	switch c {
	case StewardLedgerCategoryPlanApproval, StewardLedgerCategoryScopeCall,
		StewardLedgerCategoryMergeApproval, StewardLedgerCategoryDesignFork,
		StewardLedgerCategoryUnblockAction:
		return true
	}
	return false
}

// StewardLedgerVerdict enumerates the two outcomes of a Steward decision.
type StewardLedgerVerdict string

const (
	StewardLedgerVerdictAccepted  StewardLedgerVerdict = "accepted"
	StewardLedgerVerdictCorrected StewardLedgerVerdict = "corrected"
)

// Valid reports whether v is a recognized StewardLedgerVerdict.
func (v StewardLedgerVerdict) Valid() bool {
	return v == StewardLedgerVerdictAccepted || v == StewardLedgerVerdictCorrected
}

// StewardLedgerRecord is one decision event appended as a single JSON line to
// StewardLedgerPath.
type StewardLedgerRecord struct {
	Timestamp      time.Time             `json:"ts"`
	Category       StewardLedgerCategory `json:"category"`
	Initiative     string                `json:"initiative"`
	Recommendation string                `json:"recommendation"`
	Verdict        StewardLedgerVerdict  `json:"verdict"`
}

// Validate reports whether r has all required fields set and recognized
// enum values.
func (r StewardLedgerRecord) Validate() error {
	if r.Timestamp.IsZero() {
		return fmt.Errorf("steward ledger record: ts is zero")
	}
	if !r.Category.Valid() {
		return fmt.Errorf("steward ledger record: invalid category %q", r.Category)
	}
	if r.Initiative == "" {
		return fmt.Errorf("steward ledger record: initiative is empty")
	}
	if r.Recommendation == "" {
		return fmt.Errorf("steward ledger record: recommendation is empty")
	}
	if !r.Verdict.Valid() {
		return fmt.Errorf("steward ledger record: invalid verdict %q", r.Verdict)
	}
	return nil
}

// MarshalLine validates r, then renders it as a single JSONL line (a JSON
// object followed by "\n") ready to append to StewardLedgerPath.
func (r StewardLedgerRecord) MarshalLine() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("steward ledger record: marshal: %w", err)
	}
	return append(data, '\n'), nil
}

// ParseStewardLedgerRecord unmarshals a single JSONL line (with or without
// its trailing newline) into a StewardLedgerRecord and validates it.
func ParseStewardLedgerRecord(line []byte) (StewardLedgerRecord, error) {
	var r StewardLedgerRecord
	if err := json.Unmarshal(line, &r); err != nil {
		return StewardLedgerRecord{}, fmt.Errorf("steward ledger record: unmarshal: %w", err)
	}
	if err := r.Validate(); err != nil {
		return StewardLedgerRecord{}, err
	}
	return r, nil
}
