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

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/sentlog"
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

// DirectHandle is the reserved `ateam notify` recipient id for messaging the
// Steward directly, outside any initiative context. Single-channel @mention
// addressing (agent-teams-4x83): notify posts straight to the shared
// General channel (no thread ref, no bead behind this id) rather than a
// dedicated forum topic. Every caller posting to, or resolving, the direct
// handle MUST use this constant rather than a literal string. Not
// StewardHandle: that is the mail handle for initiative-scoped Gate/Relay
// traffic; DirectHandle is the notify handle for out-of-band direct traffic.
const DirectHandle = "direct"

// ReviewsHandle is the reserved `ateam notify` recipient id for the shared,
// cross-initiative PR-review topic (agent-teams-p9dm, at-jno7 item 1: one
// shared "Reviews" topic replacing a Forum topic opened per PR review). Like
// BriefingHandle, no bead lives behind this id — notify reads/writes its
// thread ref from StewardReviewsThreadPath instead of an initiative bead's
// "thread:<n>" label. Every caller posting to, or resolving, the reviews
// topic MUST use this constant rather than a literal string.
const ReviewsHandle = "reviews"

// ReviewsTopicTitle is the forum-topic NAME of the shared Reviews topic.
// transport.OutboundMessage.Title is what names a topic at creation, and
// whichever send opens the topic names it permanently — so BOTH senders must
// use this constant, never a literal: notify's runReviews (its default when
// --title is absent) and dispatch's --topic create path (agent-teams-p9dm.10),
// which in practice sends first. Two literals that drift apart would name the
// topic after whichever PR was reviewed first, or open a second one.
const ReviewsTopicTitle = "Reviews"

const (
	stewardDirName                = "steward"
	stewardSessionDirName         = "session"
	stewardLedgerFileName         = "ledger.jsonl"
	stewardBriefingThreadFileName = "briefing-thread"
	stewardReviewsThreadFileName  = "reviews-thread"
	stewardDoorbellFileSuffix     = ".wake"
	stewardFallbackMarkerFileName = "fallback-responder"
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
//
// Delegates to sentlog.StewardMarkerPath so there is exactly one path
// construction for this file (agent-teams-48dh contract §5) — sentlog's
// isStewardCwd needs the same path and cannot import this package.
func StewardSessionMarkerPath(ctx *cli.Context) string {
	return sentlog.StewardMarkerPath(ctx.Home)
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

// StewardReviewsThreadPath returns the path to the Steward's shared,
// cross-initiative reviews-topic thread-ref file, exactly paralleling
// StewardBriefingThreadPath.
func StewardReviewsThreadPath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), stewardReviewsThreadFileName)
}

// StewardDoorbellPath returns the doorbell (wake) file wake-watcher.sh polls
// for the Steward: <workspace-home>/mailbox/steward.wake. Mirrors
// sendKong.Run's doorbellPath construction in messaging.go with
// RecipientID=StewardHandle.
func StewardDoorbellPath(ctx *cli.Context) string {
	return filepath.Join(ctx.Home, "mailbox", StewardHandle+stewardDoorbellFileSuffix)
}

// StewardFallbackMarkerPath returns the path to the local static-primary
// fallback-responder marker: <StewardHome>/fallback-responder (contract
// agent-teams-5y8a.1). Presence of this file (contents ignored) designates
// this machine as the Steward's fallback responder for untied/unrouted
// traffic — see isFallbackResponderFunc below. A plain per-machine local
// file, deliberately NOT synced via the Dolt-backed memory store: being the
// fallback primary is a per-machine deployment choice (set once by whoever
// installs the steward on that machine), not shared cluster state.
func StewardFallbackMarkerPath(ctx *cli.Context) string {
	return filepath.Join(StewardHome(ctx), stewardFallbackMarkerFileName)
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

// ── Hung-wake→Steward envelope ───────────────────────────────────────────────
//
// agent-teams-6rru.16: the relay's hung-tick escalation ladder
// (sendHungWakeEnvelope, hung_tick.go) originally reused StewardReplyEnvelope
// for its mechanical wake nudges. That made the Steward's steward-reply
// handler treat the nudge as a genuine Eric reply — interpreting it against
// a pending recommendation, routing a bogus answer back into the initiative,
// and recording a spurious unblock-action ledger verdict — even though a
// hung-tick wake carries no Eric reply and often no pending recommendation
// at all. Distinct envelope kind so the Steward can tell a mechanical wake
// from a real Eric reply without any heuristic on the body text: it should
// just fall through to the every-wake `ateam hung-scan` scan, which
// surfaces the hung initiative and escalates normally.

const stewardHungWakeOpenPrefix = "<<<steward-hung-wake initiative:"

// BuildStewardHungWakeEnvelope renders the self-contained Hung-wake→Steward
// envelope:
//
//	<<<steward-hung-wake initiative:<id>>>>
//	<body>
//	>>>
//
// body is hungWakeBody's output (hung_tick.go) — carries the initiative id
// plus a short human-readable line so a person reading raw mail understands
// this is a mechanical wake, not an Eric reply.
func BuildStewardHungWakeEnvelope(initiativeID, body string) (string, error) {
	if initiativeID == "" {
		return "", fmt.Errorf("steward hung-wake envelope: initiative id is empty")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s%s\n", stewardHungWakeOpenPrefix, initiativeID, stewardEnvelopeClose)
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// IsStewardHungWake reports whether text is a well-formed Hung-wake→Steward
// envelope produced by BuildStewardHungWakeEnvelope, returning the recovered
// initiative id and body when it is. Returns ok=false when text isn't
// well-formed: no header or a missing closing sentinel line — same
// validation as the other envelopes' Parse* functions, just surfaced as a
// three-value return instead of a struct.
func IsStewardHungWake(text string) (initiativeID, body string, ok bool) {
	if !strings.HasPrefix(text, stewardHungWakeOpenPrefix) {
		return "", "", false
	}
	nl := strings.IndexByte(text, '\n')
	if nl == -1 {
		return "", "", false
	}
	header, rest := text[:nl], text[nl+1:]

	initiativeID, ok = strings.CutSuffix(header[len(stewardHungWakeOpenPrefix):], stewardEnvelopeClose)
	if !ok || initiativeID == "" {
		return "", "", false
	}

	body, ok = strings.CutSuffix(rest, "\n"+stewardEnvelopeClose)
	if !ok {
		return "", "", false
	}

	return initiativeID, body, true
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
// initiative id — it's explicitly out-of-band. Its one piece of header
// metadata is an OPTIONAL reply-to ref (agent-teams-ncn5.9), so the open
// sentinel is a bare header line when there is none and a prefix with
// trailing metadata when there is.

const stewardDirectOpenPrefix = "<<<steward-direct"

// StewardDirectEnvelope holds the parsed fields of a Direct→Steward
// envelope (Eric messaging the Steward directly, outside any initiative
// context).
type StewardDirectEnvelope struct {
	// ReplyTo is the opaque transport-native ref of the message this
	// envelope carries (transport.Reply.MessageRef) — what the Steward hands
	// back as `ateam notify direct --to <ref>` so its answer lands in the
	// conversation the message came from (OutboundMessage.ChatRef) instead
	// of the configured channel.
	//
	// Set ONLY for a 1:1 DM (relay.go handleDirectReply). An @mention in the
	// General channel produces a steward-direct envelope too, and leaves
	// this empty on purpose: that answer belongs in General, where the group
	// can see it.
	//
	// OPAQUE end to end. Nothing here or in relay.go parses, splits, or
	// compares it — only the owning transport understands its shape, which
	// is composite for Telegram (see the transport.Reply.MessageRef doc).
	// This envelope's only requirement on it is that it be a single line,
	// since it rides in the single-line sentinel header below.
	ReplyTo string
	Body    string
}

// BuildStewardDirectEnvelope renders the self-contained Direct→Steward
// envelope, with replyTo:
//
//	<<<steward-direct reply-to:<ref>>>>
//	<body>
//	>>>
//
// and without (replyTo == ""):
//
//	<<<steward-direct>>>
//	<body>
//	>>>
//
// An empty replyTo is a first-class case, not a degraded one — see the
// ReplyTo field doc above. Parameter order matches every other builder in
// this file: header metadata first, body last.
func BuildStewardDirectEnvelope(replyTo, body string) (string, error) {
	var b strings.Builder
	b.WriteString(stewardDirectOpenPrefix)
	if replyTo != "" {
		fmt.Fprintf(&b, " reply-to:%s", replyTo)
	}
	b.WriteString(stewardEnvelopeClose)
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// ParseStewardDirectEnvelope parses an envelope produced by
// BuildStewardDirectEnvelope. The reply-to ref is optional: a bare
// "<<<steward-direct>>>" header parses fine with ReplyTo == "", which keeps
// envelopes built by an older relay — in flight across a rolling restart, or
// produced by the @mention path — readable unchanged. Returns false when
// text isn't well-formed: no header line, header metadata that isn't a
// non-empty " reply-to:<ref>", or a missing closing sentinel line.
func ParseStewardDirectEnvelope(text string) (StewardDirectEnvelope, bool) {
	if !strings.HasPrefix(text, stewardDirectOpenPrefix) {
		return StewardDirectEnvelope{}, false
	}
	nl := strings.IndexByte(text, '\n')
	if nl == -1 {
		return StewardDirectEnvelope{}, false
	}
	header, rest := text[:nl], text[nl+1:]

	fields, ok := strings.CutSuffix(header[len(stewardDirectOpenPrefix):], stewardEnvelopeClose)
	if !ok {
		return StewardDirectEnvelope{}, false
	}
	var replyTo string
	if fields != "" {
		replyTo, ok = strings.CutPrefix(fields, " reply-to:")
		if !ok || replyTo == "" {
			return StewardDirectEnvelope{}, false
		}
	}

	body, ok := strings.CutSuffix(rest, "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardDirectEnvelope{}, false
	}
	return StewardDirectEnvelope{ReplyTo: replyTo, Body: body}, true
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

// ── Unrouted→Steward envelope ────────────────────────────────────────────────
//
// agent-teams-8beo.2: the last-resort catch-all. relay.go's handleReply has
// several branches where a reply's thread label can't be resolved to a
// single actionable target — 2+ open initiatives share the label
// (ambiguous), the closed-initiative safety net (StewardClosedInitiativeEnvelope
// above) also comes up empty or ambiguous, or the bd query itself errors —
// and today those branches log to stderr and silently drop the message. This
// envelope exists so the Steward sees those messages instead of losing them.
// Unlike StewardClosedInitiativeEnvelope, there is no concrete identified
// target the Steward can act on directly — only the original thread ref and
// a free-text reason the mechanical router failed, so the Steward is left to
// use judgment (e.g. ask Eric for clarification via `ateam notify direct`).
// NOT a substitute for the closed-initiative or briefing-reply
// short-circuits, which stay separate because they DO carry a concrete
// identified target.

const stewardUnroutedOpenPrefix = "<<<steward-unrouted thread:"

// StewardUnroutedEnvelope holds the parsed fields of an Unrouted→Steward
// envelope.
type StewardUnroutedEnvelope struct {
	ThreadRef string
	Reason    string
	Body      string
}

// BuildStewardUnroutedEnvelope renders the self-contained Unrouted→Steward
// envelope:
//
//	<<<steward-unrouted thread:<ref> reason:<reason>>>>
//	<body>
//	>>>
//
// reason is sanitized via sanitizeUnroutedReason before being spliced into
// the header line above: several call sites (relay.go) build reason via
// fmt.Sprintf("... %v", err) from real bd command errors, and
// internal/bd/bd.go's Client.Run formats CLI failures as fmt.Errorf("bd %s:
// %w\n%s", ..., stderrText) — i.e. a literal embedded newline plus raw
// multi-line CLI stderr is a normal, reachable shape for reason. Left
// unsanitized, an embedded newline would push the header's closing ">>>"
// onto a later line, and ParseStewardUnroutedEnvelope (which expects the
// full header on the first line) would fail to parse the envelope at all —
// silently losing the message, which defeats the point of this catch-all.
// Sanitizing collapses reason to a single well-formed line unconditionally,
// so the header is always parseable regardless of what error text flows in.
func BuildStewardUnroutedEnvelope(threadRef, reason, body string) (string, error) {
	if threadRef == "" {
		return "", fmt.Errorf("steward unrouted envelope: thread ref is empty")
	}
	reason = sanitizeUnroutedReason(reason)
	if reason == "" {
		return "", fmt.Errorf("steward unrouted envelope: reason is empty")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s reason:%s%s\n", stewardUnroutedOpenPrefix, threadRef, reason, stewardEnvelopeClose)
	b.WriteString(body)
	b.WriteString("\n" + stewardEnvelopeClose)
	return b.String(), nil
}

// sanitizeUnroutedReason collapses any run of whitespace in reason
// (including embedded newlines) down to a single space, and trims leading
// and trailing whitespace, via strings.Fields+Join. This makes reason safe to
// splice into the single-line steward-unrouted sentinel header — see the
// doc comment on BuildStewardUnroutedEnvelope for why this matters. A
// reason that is empty or all-whitespace sanitizes to "".
func sanitizeUnroutedReason(reason string) string {
	return strings.Join(strings.Fields(reason), " ")
}

// ParseStewardUnroutedEnvelope parses an envelope produced by
// BuildStewardUnroutedEnvelope. Returns false when text isn't well-formed:
// no header, no " reason:" separator, or a missing closing sentinel line.
func ParseStewardUnroutedEnvelope(text string) (StewardUnroutedEnvelope, bool) {
	if !strings.HasPrefix(text, stewardUnroutedOpenPrefix) {
		return StewardUnroutedEnvelope{}, false
	}
	nl := strings.IndexByte(text, '\n')
	if nl == -1 {
		return StewardUnroutedEnvelope{}, false
	}
	header, rest := text[:nl], text[nl+1:]

	fields, ok := strings.CutSuffix(header[len(stewardUnroutedOpenPrefix):], stewardEnvelopeClose)
	if !ok {
		return StewardUnroutedEnvelope{}, false
	}
	threadPart, reasonPart, ok := strings.Cut(fields, " reason:")
	if !ok || threadPart == "" || reasonPart == "" {
		return StewardUnroutedEnvelope{}, false
	}

	body, ok := strings.CutSuffix(rest, "\n"+stewardEnvelopeClose)
	if !ok {
		return StewardUnroutedEnvelope{}, false
	}

	return StewardUnroutedEnvelope{ThreadRef: threadPart, Reason: reasonPart, Body: body}, true
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
	// Decision is what Eric actually decided. Required when Verdict is
	// StewardLedgerVerdictCorrected (the recommendation alone doesn't say
	// what the right call was); optional otherwise. `omitempty` keeps
	// ledger lines written before this field existed backward-compatible —
	// they unmarshal with Decision == "".
	Decision string `json:"decision,omitempty"`
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
	if r.Verdict == StewardLedgerVerdictCorrected && r.Decision == "" {
		return fmt.Errorf("steward ledger record: verdict=corrected requires --decision (what the human actually decided)")
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

// ── Multi-machine routing predicates (Design A) ──────────────────────────────
//
// agent-teams-5y8a.1 (multi-machine steward): Design A runs one bot token +
// one relay + one steward per machine, all bots in the same Telegram forum
// supergroup (privacy OFF), so EVERY machine's relay receives EVERY human
// message. Exactly-once routing means each relay must SUPPRESS the N-1
// messages it does not own, not rescue a drop — #115 (agent-teams-b7lj)
// already routes every unclaimed reply to the LOCAL steward unconditionally;
// these two predicates are the ownership tests relay-gating
// (agent-teams-5y8a.5) consults before that unconditional routing fires.
// For a tied reply (thread resolves to an open initiative), claimsLocallyFunc
// answers "do I have this initiative's checkout?" — local, distributed, no
// config, no single point of failure. For untied traffic (thread resolves to
// no open initiative), isFallbackResponderFunc answers "am I the designated
// fallback responder?" — exactly one machine, static primary by default (see
// StewardFallbackMarkerPath above).
//
// Both are DI seam types only — mirroring relay.go's relayEnabledFunc /
// relayBDQueryFunc style (a named func type plus a "default" implementation
// wired in at registration, injectable on relayKong so tests substitute a
// fake). The default implementations (claimsInitiativeLocally,
// isFallbackResponder) are NOT declared here — they land in the predicates
// track's own file, internal/verbs/routing_ownership.go
// (agent-teams-5y8a.2), which this contract does not own.

// claimsLocallyFunc reports whether iss — an initiative issue matched by
// thread label — is claimed on THIS machine, i.e. this machine holds the
// initiative's worktree/checkout. Consumed as a DI seam on relayKong
// (agent-teams-5y8a.5) so tests can substitute a fake without touching the
// filesystem or git.
type claimsLocallyFunc func(iss bd.Issue) bool

// isFallbackResponderFunc reports whether THIS machine is the designated
// fallback responder for untied/unrouted traffic. Consumed as a DI seam on
// relayKong (agent-teams-5y8a.5) so tests can substitute a fake without
// touching the filesystem.
type isFallbackResponderFunc func(ctx *cli.Context) bool

// ── Synced steward-topics record (multi-machine) ─────────────────────────────
//
// agent-teams-5y8a.1: a non-owning relay must recognize ANOTHER machine's
// steward Briefings topic and SKIP it, rather than mis-routing it into the
// untied/fallback path (agent-teams-5y8a.5). Recognizing a peer's topic
// requires cross-machine sync — the local StewardBriefingThreadPath file
// only ever holds THIS machine's own ref. (Direct traffic no longer has a
// dedicated topic to sync — agent-teams-4x83 replaced it with @mention
// addressing in the shared General channel, routed by bot identity rather
// than by thread ref.)
//
// agent-teams-p9dm.7 extends this same per-machine-file / synced-record
// mechanism to the Reviews topic (ReviewsHandle / StewardReviewsThreadPath):
// a non-owning relay must likewise recognize and skip a peer machine's
// reviews topic, not just its briefing topic. ACCEPTED CAVEAT: because
// storage is per-machine, a review dispatched from a second machine creates
// a SECOND "Reviews" topic in the shared chat — one extra topic, not one
// per PR, which is the noise this design exists to remove (full writeup in
// agent-teams-p9dm.7).
//
// Storage: the dolt-synced memory store, reserved key
// steward:topics:<hostname> (hostname = os.Hostname(), one key per
// machine), value = JSON {"briefing":"<ref>","reviews":"<ref>"} (see
// StewardTopicsRecord below). Rationale, recorded here so the choice isn't
// re-litigated downstream: only the Dolt DB has automatic cross-machine
// push/pull; the
// memory store is already ateam-owned and, unlike a plain bead, does NOT
// trip `ateam audit` (which flags any non-tracking issue created in the
// global workspace). "steward" here is a reserved pseudo-role for this
// key's namespace only — it does NOT participate in the role/tier
// machinery (`ateam learn`/`learnings`/`condense`) real agent roles
// (planner, implementer, dri, ...) use; callers build the key via
// StewardTopicsKey below, not via learnKey.
//
// Legacy tolerance: records published by peers still on the older schema
// may carry a "direct" JSON key. ParseStewardTopicsRecord must keep parsing
// those cleanly — Go's json.Unmarshal ignores unknown fields by default, so
// simply dropping the Direct field below is sufficient; see
// TestParseStewardTopicsRecord_ToleratesLegacyDirectField. The same
// tolerance covers the reverse direction too: a record published by a peer
// still on the OLDER schema, before agent-teams-p9dm.7 added Reviews below,
// carries no "reviews" key at all and unmarshals with Reviews == "" rather
// than failing — no migration needed, same test extended to cover it.

// stewardTopicsKeyPrefix is the reserved memory-store key prefix one
// machine's steward publishes its topic refs under; see StewardTopicsKey.
const stewardTopicsKeyPrefix = "steward:topics:"

// StewardTopicsKey returns the reserved memory-store key one machine's
// steward publishes its topic refs under: "steward:topics:<hostname>".
// hostname is resolved by the caller (expected: os.Hostname()), not by this
// helper, so tests can supply a fixed value without touching the real host.
func StewardTopicsKey(hostname string) string {
	return stewardTopicsKeyPrefix + hostname
}

// StewardTopicsRecord is the JSON value schema stored at
// StewardTopicsKey(hostname): the publishing machine's Briefing and Reviews
// thread refs, so a non-owning relay can recognize (and skip) traffic
// addressed to another machine's steward topics. Reviews added by
// agent-teams-p9dm.7 alongside the pre-existing Briefing field — do not
// remove or rename Briefing.
type StewardTopicsRecord struct {
	Briefing string `json:"briefing"`
	Reviews  string `json:"reviews"`
}

// Marshal renders r as the JSON value stored at StewardTopicsKey.
func (r StewardTopicsRecord) Marshal() (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("steward topics record: marshal: %w", err)
	}
	return string(data), nil
}

// ParseStewardTopicsRecord parses a JSON value previously produced by
// StewardTopicsRecord.Marshal.
func ParseStewardTopicsRecord(value string) (StewardTopicsRecord, error) {
	var r StewardTopicsRecord
	if err := json.Unmarshal([]byte(value), &r); err != nil {
		return StewardTopicsRecord{}, fmt.Errorf("steward topics record: unmarshal: %w", err)
	}
	return r, nil
}

// Frozen function signatures — implemented in the synced-topics track's own
// file, internal/verbs/steward_topics.go (agent-teams-5y8a.3; extended for
// the Reviews ref by agent-teams-p9dm.9), which this contract does not own:
//
//	func publishStewardTopics(ctx *cli.Context) error
//	func isKnownStewardTopic(ctx *cli.Context, threadRef string) bool
//
// publishStewardTopics upserts THIS machine's briefing AND reviews thread
// refs (StewardBriefingThreadPath, StewardReviewsThreadPath) into the
// synced store at StewardTopicsKey(os.Hostname()) as a StewardTopicsRecord.
// An absent thread-ref file (no topic opened yet on this machine) publishes
// as an empty string for that field, not an error.
// isKnownStewardTopic reports whether threadRef is in the synced union of
// ALL machines' published briefing OR reviews refs AND is not this
// machine's own local ref for either topic (i.e. it's owned by another
// steward) — consumed by relay-gating (agent-teams-5y8a.5) as the
// peer-topic skip check ahead of the bd label query.

// ── Shared PR-review topic: dispatch flag, message lines, PR-title seam ─────
//
// agent-teams-p9dm.7 (at-jno7 item 1, Option A): collapses "one Forum topic
// per PR review" into the single shared ReviewsHandle topic above. Eric
// rejected findings-summarization in the topic (steward:hot:telegram-
// message-style caps acks at 25 words, and Eric refuses structural chrome).
// Verbatim: "Most of the time I don't care about the review content. I just
// want to know a review happened, and then if the PR title intrigues me I'd
// like to ask the steward for more info, or maybe even to dispatch a more
// focused review." So both message lines below carry PR number, repo
// basename, and PR TITLE — nothing else. No finding counts, no severity, no
// APPROVE/COMMENT verdict, no headers, no sender tags. The bare URL rides on
// its own second line so Telegram renders it as a tap target; that
// affordance is what turns "the title intrigues me" into actually looking,
// so it stays — it is not content summarization.
//
// --topic <handle> flag (declared on dispatchKong in dispatch.go,
// agent-teams-p9dm.10 — this comment freezes only the NAME and SEMANTICS,
// not the kong struct field):
//
//   - Value is a reserved `ateam notify` handle (currently only
//     ReviewsHandle).
//   - When set, dispatch posts its registration line into that handle's
//     SHARED topic instead of opening a per-initiative topic.
//   - When set, dispatch writes NO "thread:" label on the initiative bead.
//     This is load-bearing, not an oversight to "fix" later: two mechanisms
//     make a shared topic addressed by per-initiative "thread:" labels
//     actively broken. relay.go:401-412's default branch treats 2+ open
//     initiatives sharing a label as "(ambiguous)" and drops the reply on
//     the steward instead of routing it to any review session.
//     kong_converted.go's closeKong.sendCloseSignal (:526-557) reads
//     threadLabelValue and unconditionally calls CloseTopic on whatever
//     thread it resolves to, so the FIRST review to close would close the
//     shared topic for every other review still in flight. Writing no label
//     sidesteps both by construction.
//   - When absent, behavior is byte-identical to today: per-initiative
//     topic, thread label, no gh subprocess. This flag must not change any
//     existing dispatch path.
//   - An unrecognized --topic value is a usage error, not a silent fallback
//     to per-initiative topic creation.
//
// ReviewsStartLineFormat is the two-line dispatch-registration message
// posted to the shared ReviewsHandle topic (dispatch.go, agent-teams-
// p9dm.10), a Go fmt format string applied to exactly four args in order:
// pr-number, pr-repo (BASENAME of owner/repo, e.g. "midgard" not
// "MGT-Insurance/midgard"), title-segment, pr-url:
//
//	Review started · #%s %s%s
//	%s
//
// title-segment is a PRE-COMPOSED argument, not the bare title — this is
// the ONE frozen format string for both the with-title and the fail-soft
// no-title case (agent-teams-p9dm.7 deliberately rejected a second
// ...NoTitleFormat constant: it would duplicate the "Review started · #%s
// %s" prefix, so any later change to that prefix would have to be made in
// two places and the two renderings could drift). Callers build
// title-segment exactly as follows and MUST NOT inline " — " (or any other
// separator/spacing) themselves:
//
//   - title non-empty: " — " (one leading space, an em dash U+2014, one
//     trailing space) immediately followed by the title — e.g.
//     " — Fix flaky retry logic".
//   - title empty (the prTitleFunc fail-soft case below): "" — the empty
//     string, not a placeholder. The rendered line then reads
//     "Review started · #<n> <repo>" with no dangling separator.
const ReviewsStartLineFormat = "Review started · #%s %s%s\n%s"

// The completion line — posted by the review-pr skill via `ateam notify
// reviews` (plugins/agent-teams/skills/review-pr/SKILL.md, agent-teams-
// p9dm.13) — is frozen here as a doc comment rather than a Go constant
// because the skill emits it from shell (printf), which cannot import a Go
// constant. Verbatim, four args in order: pr-number, pr-repo (basename,
// same convention as the start line), title-segment, review-url:
//
//	Review complete · #%s %s%s
//	%s
//
// title-segment follows the exact same construction rule as
// ReviewsStartLineFormat's title-segment argument above: " — " + title when
// non-empty, "" when not — see that constant's doc comment for the byte-
// exact separator. Unlike the start line, the review-pr skill always has
// the PR title in hand (it read the PR to review it), so in practice this
// segment is never empty here. Still, the shell snippet emitting this line
// MUST assemble it via the same rule (never splice the title directly into
// a hardcoded "%s — %s" format), so the two lines can never drift apart on
// separator character or spacing.

// prTitleFunc is the DI seam for fetching a PR's title, consumed by
// dispatchKong (agent-teams-p9dm.10) as a kong:"-" injected field — never a
// CLI flag — mirroring the agentsJSONFunc pattern (messaging.go:259,292) so
// tests substitute a fake without spawning a real `gh` subprocess. Called
// ONLY on the --topic path; a dispatch without --topic must not invoke it.
//
// Default implementation (lives in dispatch.go, NOT here — this contract
// freezes only the seam):
//
//	gh pr view <pr-number> --repo <owner/repo> --json title -q .title
//
// with a 10s timeout.
//
// FAIL-SOFT, MANDATORY: gh absent, auth failure, 404, or timeout — any
// failure — yields "" title, which the caller turns into an empty
// title-segment ("") when composing ReviewsStartLineFormat's arguments (see
// that constant's doc comment above for the exact construction rule) — the
// line then renders WITHOUT the " — <title>" segment. A title-fetch failure
// may NEVER fail a dispatch: by the time this seam runs, bd create has
// already succeeded — the same fail-soft precedent dispatch.go:336-340
// already establishes for the rest of the topic path.
//
// Declared here rather than at each caller: one implementation, one seam,
// one test surface. route.go's spawnReviewInitiative has no gh dependency
// today and must not acquire one — it doesn't call this seam. Only
// dispatch.go's --topic path does.
type prTitleFunc func(ownerRepo string, prNumber int) (string, error)
