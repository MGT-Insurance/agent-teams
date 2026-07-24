package verbs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mgt-insurance/agent-teams/internal/bd"
	"github.com/mgt-insurance/agent-teams/internal/cli"
	"github.com/mgt-insurance/agent-teams/internal/transport"
	"github.com/mgt-insurance/agent-teams/internal/workspace"
)

// relayEnabledFunc reports whether the active transport is configured.
// Injected so tests can control the result without touching env / config files.
type relayEnabledFunc func(home string) bool

// relayTransportForFunc resolves the active Transport.
// Injected so tests can substitute a fake.
type relayTransportForFunc func(home string) (transport.Transport, error)

// relayBDQueryFunc queries the workspace beads for open initiatives carrying a
// given thread label. Returns the matching issues (may be empty or many).
// Injected so tests can substitute a fake.
type relayBDQueryFunc func(home, label string) ([]bd.Issue, error)

// relaySendFunc execs `ateam mail send steward --file <tmp> --sender human`.
// The destination is always the Steward (StewardHandle); the mapped
// initiative id travels inside the envelope written to file, not as a CLI
// arg. Injected so tests can capture calls without running a subprocess.
type relaySendFunc func(ctx *cli.Context, file string) error

// knownStewardTopicFunc reports whether threadRef is a KNOWN steward topic
// (briefing/direct) belonging to ANOTHER machine, per the synced record
// frozen by steward_seams.go (StewardTopicsKey/StewardTopicsRecord).
// Injected so tests can substitute a fake without touching the memory
// store. Default: isKnownStewardTopic (steward_topics.go,
// agent-teams-5y8a.3).
type knownStewardTopicFunc func(ctx *cli.Context, threadRef string) bool

// nonTopicThreadRefPlaceholder stands in for reply.ThreadRef when routing a
// non-topic (ThreadRef=="") message via sendUnroutedToSteward:
// BuildStewardUnroutedEnvelope (steward_seams.go) rejects an empty thread
// ref, and a General-topic/DM message has no real thread to report.
const nonTopicThreadRefPlaceholder = "(general)"

// replyPreviewLen caps how much of a reply's text appears in the "received
// message" log line (transport.PreviewText). N=70 per "N ≈ 60-80" guidance —
// not configurable, no new flags.
const replyPreviewLen = 70

// firstBotMention returns the first entry in mentions that looks like a bot
// username (Telegram requires every bot's username to end in "bot"), or ""
// if none do. mentions is already lowercased by the transport, so this is a
// plain case-sensitive suffix check. Used by handleReply's non-topic rule 2
// to skip traffic addressed to some other bot without a peer-bot registry.
func firstBotMention(mentions []string) string {
	for _, m := range mentions {
		if strings.HasSuffix(m, "bot") {
			return m
		}
	}
	return ""
}

// defaultBDQuery runs `bd list --status=open --label=<label> --json` against
// the global workspace home and returns matching issues.
func defaultBDQuery(home, label string) ([]bd.Issue, error) {
	client := bd.NewClient(home)
	var issues []bd.Issue
	if err := client.RunJSON(&issues, "list", "--status=open", "--label="+label, "--json"); err != nil {
		return nil, err
	}
	return issues, nil
}

// relayBDQueryClosedFunc queries the workspace beads for CLOSED initiatives
// carrying a given thread label. Used only in the case-0 branch of
// handleReply — after the open-only bdQuery seam already found zero open
// matches — as the closed-initiative safety net (agent-teams-7dup.2):
// reopening a Telegram topic in the UI does not change beads state, so this
// is keyed off beads status, not Telegram topic state.
//
// NOTE: `bd list` defaults to open-only, the same as every other bd list
// invocation in this file — there is no "return all statuses" mode via a
// bare --label query (confirmed empirically: `bd list --label=X --json`
// omits closed issues; `bd list --label=X --status=closed --json` is
// required to see them). So this seam queries --status=closed directly
// rather than querying unfiltered and filtering client-side.
// Injected so tests can substitute a fake.
type relayBDQueryClosedFunc func(home, label string) ([]bd.Issue, error)

// defaultBDQueryClosed runs `bd list --status=closed --label=<label> --json`
// against the global workspace home and returns matching closed issues.
func defaultBDQueryClosed(home, label string) ([]bd.Issue, error) {
	client := bd.NewClient(home)
	var issues []bd.Issue
	if err := client.RunJSON(&issues, "list", "--status=closed", "--label="+label, "--json"); err != nil {
		return nil, err
	}
	return issues, nil
}

// relayDoltPullFunc pulls the global workspace's dolt-backed beads DB from
// its remote. Used only by freshenBeforeUntied below to absorb cross-machine
// label-sync lag before conceding a reply is genuinely untied
// (agent-teams-6rru.10 Part B). Injected so tests can substitute a fake
// without touching dolt/git.
type relayDoltPullFunc func(home string) error

// defaultDoltPull runs `bd -C <home> dolt pull`.
func defaultDoltPull(home string) error {
	_, err := bd.NewClient(home).Run("dolt", "pull")
	return err
}

// defaultRelaySend execs `ateam mail send steward --file <file> --sender
// human` as a subprocess so the relay loop is not blocked by the in-process
// send machinery. The Steward is always the recipient; it reads the mapped
// initiative id out of the reply envelope already written to file.
func defaultRelaySend(_ *cli.Context, file string) error {
	cmd := exec.Command("ateam", "mail", "send", StewardHandle, "--file", file, "--sender", "human")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// relayAckFunc acks one inbound message back on the transport (e.g. a
// Telegram read-receipt reaction), keyed by the transport-opaque
// transport.Reply.MessageRef. Injected so tests can substitute a fake.
type relayAckFunc func(messageRef string) error

// relayAcker is an optional transport capability (Telegram read receipts via
// message reactions) for acking a forwarded inbound message. Mirrors the
// topicCloser precedent (kong_converted.go): asserted here at the call site
// (Run, once the transport resolves) rather than folded into
// transport.Transport, which stays capability-minimal — most transports
// have no notion of an ackable message.
type relayAcker interface {
	Ack(reply transport.Reply) error
}

// RegisterRelayKong registers the relay verb onto p using a native kong struct.
func RegisterRelayKong(p *cli.Parser) {
	p.AddVerb("relay", "Long-poll the configured transport and relay human replies to the Steward.", &relayKong{
		enabled:             transport.Enabled,
		transportFor:        transport.For,
		bdQuery:             defaultBDQuery,
		bdQueryClosed:       defaultBDQueryClosed,
		doltPull:            defaultDoltPull,
		sleeper:             defaultSleeper,
		send:                defaultRelaySend,
		claimsLocally:       claimsInitiativeLocally,
		isFallbackResponder: isFallbackResponder,
		knownStewardTopic:   isKnownStewardTopic,
	})
}

// relayKong is the kong-native form of relayCmd: `ateam relay` (no args).
type relayKong struct {
	enabled       relayEnabledFunc       `kong:"-"`
	transportFor  relayTransportForFunc  `kong:"-"`
	bdQuery       relayBDQueryFunc       `kong:"-"`
	bdQueryClosed relayBDQueryClosedFunc `kong:"-"`
	doltPull      relayDoltPullFunc      `kong:"-"`
	sleeper       sleeperFunc            `kong:"-"`
	send          relaySendFunc          `kong:"-"`
	ack           relayAckFunc           `kong:"-"`

	// Multi-machine ownership-gating seams (agent-teams-5y8a.5) — see the
	// doc comment on handleReply below for what each gates.
	claimsLocally       claimsLocallyFunc       `kong:"-"`
	isFallbackResponder isFallbackResponderFunc `kong:"-"`
	knownStewardTopic   knownStewardTopicFunc   `kong:"-"`
}

// Run satisfies the kong runner interface; ctx is injected via kong.Bind.
//
// If messaging is not configured, prints a clear message and exits 0 (opt-in).
// Otherwise calls transport.Receive, blocking until killed.
func (c *relayKong) Run(ctx *cli.Context) error {
	if ctx == nil {
		return fmt.Errorf("ateam relay: nil context")
	}

	home := workspace.Home()

	if !c.enabled(home) {
		fmt.Fprintln(ctx.Stdout, "messaging not configured; relay is a no-op")
		return nil
	}

	t, err := c.transportFor(home)
	if err != nil {
		return fmt.Errorf("ateam relay: resolve transport: %w", err)
	}

	if c.ack == nil {
		if acker, ok := transport.Capability[relayAcker](t); ok {
			c.ack = func(messageRef string) error {
				return acker.Ack(transport.Reply{MessageRef: messageRef})
			}
		}
	}

	transport.Logf(ctx.Stderr, 0, "starting on transport %q", t.Name())

	go runHungTick(ctx, t)

	return t.Receive(func(reply transport.Reply) error {
		return c.handleReply(ctx, reply)
	})
}

// handleReply routes one inbound human reply. Returns nil always (log-and-skip
// on routing failures) unless the error is permanent-transport-level, in which
// case returning non-nil aborts Receive.
//
// Multi-machine gating (agent-teams-5y8a.5): in Design A every machine runs
// one bot + one relay + one steward, all in the same Telegram forum
// (privacy OFF, #114/#115), so EVERY machine's relay receives EVERY human
// message. Exactly-once routing means each relay must SUPPRESS the
// branches it does not own, not rescue a drop. This machine's own steward
// topics (briefing short-circuit below) is never suppressed — it only ever
// matches THIS machine's own persisted thread ref. Everything else is
// gated: a TIED reply (thread resolves to exactly one open initiative)
// routes only when claimsLocally reports this machine holds that
// initiative's checkout; UNTIED traffic (no thread ref, a bd query error,
// or zero/2+ open matches) routes only when isFallbackResponder reports
// this machine is the designated fallback responder; and a reply whose
// thread ref is a KNOWN peer steward topic (knownStewardTopic) is skipped
// outright — that peer's own relay already routes it locally.
func (c *relayKong) handleReply(ctx *cli.Context, reply transport.Reply) error {
	// Canonical "message received" line (REQUIRED #3) — transport-agnostic,
	// so it covers every transport, not just Telegram. Emitted first, before
	// any routing decision, so every reply that reaches handleReply produces
	// this line regardless of which branch below it ultimately takes.
	transport.Logf(ctx.Stderr, 1, "received message (thread=%q): %q", reply.ThreadRef, transport.PreviewText(reply.Text, replyPreviewLen))

	// Non-topic messages (General channel) arrive with ThreadRef == "".
	// Single-channel @mention addressing (agent-teams-4x83) replaces the old
	// per-machine [Direct] topic short-circuit and applies, in order:
	//
	//  1. reply.MentionsSelf: this bot was @mentioned — route to MY steward
	//     as a steward-direct envelope, regardless of fallback-responder
	//     status. Reuses handleDirectReply/BuildStewardDirectEnvelope.
	//     Peers naturally skip this rule since MentionsSelf is false for them.
	//  2. Else, a mentioned username ends in "bot" (Telegram platform rule —
	//     all bot usernames must end in "bot") and it is not me: some OTHER
	//     bot was addressed — skip silently (no registry, no metadata).
	//  3. Else (no bot mention at all — including a human-only mention like
	//     "@eric"): existing fallback-responder steward-unrouted behavior,
	//     unchanged.
	if reply.ThreadRef == "" {
		if reply.MentionsSelf {
			transport.Logf(ctx.Stderr, 2, "mentions me — routing to steward")
			return c.handleDirectReply(ctx, reply)
		}
		if mentioned := firstBotMention(reply.Mentions); mentioned != "" {
			transport.Logf(ctx.Stderr, 2, "mentions @%s — not me, skipping", mentioned)
			return nil
		}
		if !c.isFallbackResponder(ctx) {
			transport.Logf(ctx.Stderr, 2, "skipping non-topic message (no thread ref)")
			return nil
		}
		transport.Logf(ctx.Stderr, 2, "routing non-topic message to steward (no thread ref)")
		c.sendUnroutedToSteward(ctx, reply.MessageRef, nonTopicThreadRefPlaceholder, "non-topic message (no thread ref)", reply.Text)
		return nil
	}

	// Briefing-channel short-circuit: a message posted in the Steward's
	// Briefings topic (contract: BriefingHandle, StewardBriefingThreadPath in
	// steward_seams.go) has no initiative bead behind it by design, so the bd
	// label lookup below would always miss and the message would die
	// silently (agent-teams-8beo.1). If this reply's thread ref matches the
	// persisted briefing-channel thread ref, route it to the Steward as a
	// steward-briefing-reply envelope, bypassing the initiative lookup
	// entirely. An absent/empty thread-ref file (no briefing ever posted, or
	// a read failure) falls through to the existing initiative-reply path
	// below.
	briefingRef, err := readThreadRefFile(StewardBriefingThreadPath(ctx))
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "read steward briefing thread ref: %v", err)
	} else if briefingRef != "" && reply.ThreadRef == briefingRef {
		return c.handleBriefingReply(ctx, reply)
	}

	// Peer steward-topic skip (agent-teams-5y8a.5): a reply whose thread ref
	// is a KNOWN steward topic belonging to ANOTHER machine (synced via
	// steward:topics:<hostname>, steward_topics.go) is that peer's own
	// briefing/direct traffic — its relay already short-circuits it locally
	// above. isKnownStewardTopic excludes THIS machine's own local refs, so
	// the short-circuits above always fire first for topics we own; this
	// check only ever matches a peer's. Skip before the bd label query so a
	// peer's topic ref never falls into the untied/fallback path below.
	if c.knownStewardTopic(ctx, reply.ThreadRef) {
		transport.Logf(ctx.Stderr, 2, "skipping peer steward topic (thread %q)", reply.ThreadRef)
		return nil
	}

	label := "thread:" + reply.ThreadRef
	home := workspace.Home()

	issues, queryErr := c.bdQuery(home, label)
	var open []bd.Issue
	untied := false
	if queryErr != nil {
		transport.Logf(ctx.Stderr, 2, "bd query for label %q failed: %v — skipping", label, queryErr)
		untied = true
	} else {
		// Filter to open issues only (bdQuery already filters, but guard
		// against implementations that do not).
		for _, iss := range issues {
			if strings.EqualFold(iss.Status, "open") {
				open = append(open, iss)
			}
		}
		untied = len(open) == 0
	}

	if untied {
		// Untied (agent-teams-5y8a.5): only the designated fallback
		// responder attempts the freshen-then-safety-net path below — every
		// other machine sees the same untied reply and must suppress it
		// rather than also routing it.
		if !c.isFallbackResponder(ctx) {
			if queryErr != nil {
				transport.Logf(ctx.Stderr, 2, "not fallback responder — skipping unrouted reply")
			} else {
				transport.Logf(ctx.Stderr, 2, "not fallback responder — skipping untied reply (thread %q)", reply.ThreadRef)
			}
			return nil
		}

		// Freshen before conceding the reply is genuinely untied
		// (agent-teams-6rru.10 Part B): the thread label can still be
		// invisible here even though it exists — same-machine commit lag
		// (notify/dispatch just wrote it) or cross-machine dolt-sync lag
		// (a DIFFERENT machine wrote it and this one hasn't pulled yet).
		// Applies whether "untied" came from the query-error branch above
		// or the zero-open-match case below — either way it just means "no
		// resolution yet from the local view", not "genuinely untied". A
		// successful freshen feeds directly into the same routing below, so
		// there is exactly one tied/ambiguous decision point either way.
		if freshOpen := c.freshenBeforeUntied(ctx, home, label); len(freshOpen) > 0 {
			transport.Logf(ctx.Stderr, 2, "freshen resolved label %q (%d open match(es)) — routing, not straying", label, len(freshOpen))
			open = freshOpen
			untied = false
		}
	}

	if untied {
		if queryErr != nil {
			c.sendUnroutedToSteward(ctx, reply.MessageRef, reply.ThreadRef, fmt.Sprintf("bd query error: %v", queryErr), reply.Text)
			return nil
		}
		routed, reason := c.routeClosedInitiativeSafetyNet(ctx, home, label, reply)
		if routed {
			return nil
		}
		transport.Logf(ctx.Stderr, 2, "no open initiative found for label %q — skipping", label)
		c.sendUnroutedToSteward(ctx, reply.MessageRef, reply.ThreadRef, reason, reply.Text)
		return nil
	}

	switch len(open) {
	case 1:
		// Tied (agent-teams-5y8a.5): only the machine that holds this
		// initiative's checkout routes it — every other machine sees the
		// same tied reply and must suppress it to avoid duplicate routing.
		if !c.claimsLocally(open[0]) {
			transport.Logf(ctx.Stderr, 2, "not claimed locally — skipping tied reply for %s (thread %q)", open[0].ID, reply.ThreadRef)
			return nil
		}
	default:
		// Ambiguous (agent-teams-5y8a.5): same fallback-only gate as the
		// untied branch above. (len(open) can't be 0 here — the untied
		// block above always either returns or clears untied by producing a
		// non-empty open.)
		if !c.isFallbackResponder(ctx) {
			transport.Logf(ctx.Stderr, 2, "not fallback responder — skipping ambiguous reply (thread %q)", reply.ThreadRef)
			return nil
		}
		reason := fmt.Sprintf("ambiguous: %d open initiatives", len(open))
		transport.Logf(ctx.Stderr, 2, "ambiguous: %d open initiatives carry label %q — skipping", len(open), label)
		c.sendUnroutedToSteward(ctx, reply.MessageRef, reply.ThreadRef, reason, reply.Text)
		return nil
	}

	id := open[0].ID

	// REQUIRED #4: the explicit resolution-outcome line — the gap that let
	// the original bug hide. Every OTHER terminal branch of handleReply
	// already logs a skip/ambiguous/unrouted reason at depth 2 above; this
	// success line means every reply now produces exactly one depth-2 "what
	// happened to this message" line, with no silent branch left over.
	transport.Logf(ctx.Stderr, 2, "routed to initiative %s (%s)", id, open[0].Title)

	envelope, err := BuildStewardReplyEnvelope(id, reply.Text)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "build steward reply envelope for %s: %v — skipping", id, err)
		return nil
	}

	c.sendEnvelopeToSteward(ctx, reply.MessageRef, envelope, fmt.Sprintf("initiative %s", id))
	return nil
}

// relayFreshenBackoff is the short bounded backoff freshenBeforeUntied sleeps
// before its re-query, absorbing same-machine label-commit lag (this
// process's view of a label notify/dispatch just wrote locally hasn't
// caught up yet). Small enough not to stall the relay's Receive loop.
const relayFreshenBackoff = 250 * time.Millisecond

// freshenBeforeUntied re-resolves label against a fresh view before
// handleReply's untied path concedes the reply is genuinely untied
// (agent-teams-6rru.10 Part B — the routing-race fix: a topic created by
// Send is replyable immediately, but its thread label is written after Send
// and may not be visible yet, either on this machine or (multi-machine) on
// whichever machine's relay fields the reply).
//
// Performs at most ONE dolt-pull (absorbs cross-machine label-sync lag —
// the label may have been written by a DIFFERENT machine's notify/dispatch
// and not yet synced here) and, after one short bounded backoff (absorbs
// same-machine label-commit lag), ONE re-query — never a retry loop, so a
// reply that is genuinely untied is never held up for long. A pull failure
// (no remote configured, transient network hiccup) is warned and
// non-fatal: the re-query still picks up a same-machine write even without
// a working remote. A re-query failure is also warned. Either way the
// caller treats a nil/empty return as "still untied".
func (c *relayKong) freshenBeforeUntied(ctx *cli.Context, home, label string) []bd.Issue {
	if c.doltPull != nil {
		if err := c.doltPull(home); err != nil {
			transport.Logf(ctx.Stderr, 2, "freshen: dolt pull failed (continuing): %v", err)
		}
	}
	if c.sleeper != nil {
		c.sleeper(relayFreshenBackoff)
	}
	issues, err := c.bdQuery(home, label)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "freshen: re-query for label %q failed: %v", label, err)
		return nil
	}
	var open []bd.Issue
	for _, iss := range issues {
		if strings.EqualFold(iss.Status, "open") {
			open = append(open, iss)
		}
	}
	return open
}

// routeClosedInitiativeSafetyNet handles the case-0 branch of handleReply:
// zero OPEN initiatives carry the reply's thread label. Before giving up, it
// queries CLOSED initiatives for the same label — reopening a topic in the
// Telegram UI does not change beads state, so a human reply posted into a
// since-closed initiative's topic would otherwise be silently dropped
// forever (agent-teams-7dup, the bug this safety net closes). If exactly one
// CLOSED initiative owns the label, the reply is routed to the Steward as a
// steward-closed-initiative envelope carrying the closed initiative's id,
// and (true, "") is returned so the caller skips its own "no open
// initiative" log. Zero or ambiguous (2+) closed matches, a query error, or
// no bdQueryClosed seam configured, all fall through to (false, reason) —
// the caller logs its existing message AND (agent-teams-8beo.2) routes the
// reply to the Steward as a steward-unrouted envelope carrying reason so the
// message still reaches the Steward instead of being dropped.
func (c *relayKong) routeClosedInitiativeSafetyNet(ctx *cli.Context, home, label string, reply transport.Reply) (bool, string) {
	if c.bdQueryClosed == nil {
		return false, "closed-initiative safety net not configured"
	}
	all, err := c.bdQueryClosed(home, label)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "bd query (closed) for label %q failed: %v", label, err)
		return false, fmt.Sprintf("bd query error: %v", err)
	}

	// Defensive filter in case the seam ever returns more than closed
	// issues (e.g. a future fake or a bd behavior change) — bdQueryClosed
	// is expected to already filter to --status=closed server-side.
	var closed []bd.Issue
	for _, iss := range all {
		if strings.EqualFold(iss.Status, "closed") {
			closed = append(closed, iss)
		}
	}
	switch len(closed) {
	case 0:
		return false, "no open or closed initiative found"
	case 1:
		// Exactly one match — route below.
	default:
		return false, fmt.Sprintf("ambiguous: %d closed initiatives", len(closed))
	}
	id := closed[0].ID

	envelope, err := BuildStewardClosedInitiativeEnvelope(id, reply.Text)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "build steward closed-initiative envelope for %s: %v — skipping", id, err)
		return false, fmt.Sprintf("build closed-initiative envelope failed: %v", err)
	}

	wrote, sendErr := c.sendEnvelopeToSteward(ctx, reply.MessageRef, envelope, fmt.Sprintf("closed initiative %s", id))
	if !wrote {
		return false, fmt.Sprintf("write envelope temp file failed: %v", sendErr)
	}
	if sendErr != nil {
		return true, ""
	}

	transport.Logf(ctx.Stderr, 2, "routed message to steward for closed initiative %s (label %q)", id, label)
	return true, ""
}

// handleDirectReply routes a reply whose thread ref matches the Steward's
// persisted direct-message channel thread ref (see the short-circuit in
// handleReply above): it wraps reply.Text in a steward-direct envelope and
// sends it straight to the Steward, with no initiative lookup involved.
func (c *relayKong) handleDirectReply(ctx *cli.Context, reply transport.Reply) error {
	envelope, err := BuildStewardDirectEnvelope(reply.Text)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "build steward direct envelope: %v — skipping", err)
		return nil
	}

	c.sendEnvelopeToSteward(ctx, reply.MessageRef, envelope, "direct message")
	return nil
}

// handleBriefingReply routes a reply whose thread ref matches the Steward's
// persisted Briefings-topic thread ref (see the short-circuit in handleReply
// above): it wraps reply.Text in a steward-briefing-reply envelope and sends
// it straight to the Steward, with no initiative lookup involved.
func (c *relayKong) handleBriefingReply(ctx *cli.Context, reply transport.Reply) error {
	envelope, err := BuildStewardBriefingReplyEnvelope(reply.Text)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "build steward briefing-reply envelope: %v — skipping", err)
		return nil
	}

	c.sendEnvelopeToSteward(ctx, reply.MessageRef, envelope, "briefing reply")
	return nil
}

// sendUnroutedToSteward is the last-resort catch-all (agent-teams-8beo.2,
// agent-teams-8beo.3): called from handleReply's top-level bdQuery-error
// branch, its ambiguous-open-initiatives branch, and from the case-0 branch
// when routeClosedInitiativeSafetyNet also fails to place the reply. It
// builds a steward-unrouted envelope carrying threadRef, reason, and body,
// and sends it to the Steward via sendEnvelopeToSteward below. Callers are
// expected to ALSO keep their own stderr diagnostic log (this helper does
// not replace that visibility, it supplements it); a failure of the send
// itself only logs here, same as every other send path in this file — it
// never aborts the relay loop.
func (c *relayKong) sendUnroutedToSteward(ctx *cli.Context, messageRef, threadRef, reason, body string) {
	envelope, err := BuildStewardUnroutedEnvelope(threadRef, reason, body)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "build steward unrouted envelope: %v — skipping", err)
		return
	}

	c.sendEnvelopeToSteward(ctx, messageRef, envelope, fmt.Sprintf("unrouted reply, thread %q", threadRef))
}

// sendEnvelopeToSteward writes envelope to a temp file and sends it to the
// Steward via c.send, removing the temp file afterward — the
// write-temp-file/send/log-on-failure shape shared by every envelope-send
// path in this file (agent-teams-8beo.3, finding 3: this used to be
// repeated inline at each call site). failCtx is a short human-readable
// description of the call site (e.g. "initiative at-001", "direct
// message"), spliced into the send-failure log line as "ateam mail send
// steward failed (<failCtx>): <err> — skipping".
//
// This is the single choke point where forward-success is known (c.send
// returned nil), so it is also the single ack (read-receipt) point
// (agent-teams-a0ml.3): on success, messageRef is acked via ackForward
// before returning. NOT acked on a send error or a write-temp-file
// failure — only a genuine forward acks the originating message.
//
// Returns wrote=false only when writeEnvelopeToTemp itself failed — the
// envelope was never handed to c.send at all — with err set to that
// failure (already logged). Otherwise wrote=true and err is c.send's error
// (nil on success, already logged on failure); most callers ignore both
// return values since they always continue regardless, but
// routeClosedInitiativeSafetyNet needs the distinction to preserve its
// existing (bool, reason) contract.
func (c *relayKong) sendEnvelopeToSteward(ctx *cli.Context, messageRef, envelope, failCtx string) (wrote bool, err error) {
	tmpPath, err := writeEnvelopeToTemp(envelope)
	if err != nil {
		transport.Logf(ctx.Stderr, 2, "%v — skipping", err)
		return false, err
	}
	defer os.Remove(tmpPath)

	if err := c.send(ctx, tmpPath); err != nil {
		transport.Logf(ctx.Stderr, 2, "ateam mail send %s failed (%s): %v — skipping", StewardHandle, failCtx, err)
		return true, err
	}
	c.ackForward(ctx, messageRef)
	return true, nil
}

// ackForward acks messageRef on the transport (e.g. fires a Telegram read
// receipt), if an ack seam is configured and messageRef is non-empty.
// Log-only: never fails the pipeline, matching the discipline of every
// other send path in this file.
func (c *relayKong) ackForward(ctx *cli.Context, messageRef string) {
	if c.ack == nil || messageRef == "" {
		return
	}
	if err := c.ack(messageRef); err != nil {
		transport.Logf(ctx.Stderr, 2, "read receipt failed: %v", err)
		return
	}
	transport.Logf(ctx.Stderr, 2, "read receipt sent")
}

// writeEnvelopeToTemp writes envelope to a new temp file (so ateam mail send
// can read it via --file) and returns its path. Caller owns removing it.
func writeEnvelopeToTemp(envelope string) (string, error) {
	tmp, err := os.CreateTemp("", "ateam-relay-reply-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(envelope); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()
	return tmpPath, nil
}
