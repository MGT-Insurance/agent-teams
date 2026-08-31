# Why the inbound envelope kinds exist

Rationale only — never the dispatch rules themselves. SKILL.md §2 is self-sufficient without this file; read it only for the "why" behind a rule that otherwise looks arbitrary.

`internal/verbs/steward_seams.go` is the frozen contract for the envelope format itself. Read it if a parse ever looks off — never guess at the format.

## steward-closed-initiative

Reopening a topic in the Telegram UI does not reopen the initiative — beads state is the source of truth — so the relay's closed-initiative safety net routes what would otherwise be a silently-dropped message here instead. Without this envelope kind, a reply to an old, closed initiative's topic gets no response at all: the mechanical router has no live DRI to hand it to, and dropping it silently is worse than a Steward judgment call.

## steward-unrouted

The relay's last-resort catch-all fires on three failure modes: 2+ open initiatives shared the thread label (ambiguous — the router can't tell which one a reply belongs to), the closed-initiative safety net also came up empty or ambiguous, or a bd query itself errored. Unlike steward-closed-initiative, there's no concrete target to act on directly — no initiative id, no clean reply surface back into the original thread.

**Multi-machine sync-lag caveat:** each machine syncs beads/topic-refs on its own schedule. A reply belonging to another machine's topic, or to an initiative this machine doesn't own, can arrive here simply because the sync that would have routed it correctly hasn't landed yet. That's why the dispatch rule says stay silent or minimal in that case — reacting confidently on stale state produces confusing double-replies once the sync catches up.

## steward-gate — kind:live-test-review

Proof-forwarding, not the question/review decision-escalation flow the rest of this envelope's handler exists for. A tester's live verification already produced concrete evidence — a screenshot, a captured payload — so folding it into a digested recommendation would throw away exactly what the human asked to see. The handler skips enrichment and the ledger entirely and instead fans the attachment list out through the audited `ateam notify --image`/`--document` channel, then sends the short text body last.

Each attachment's caption is its own basename, not the proof body repeated: `--file` is required on every `ateam notify` call, and reusing the same summary as every attachment's caption would show the human the same text N+1 times — noise against the "judge quickly" goal. Basename is the only per-attachment text the envelope actually carries (kind + path, no per-attachment description), so it's the honest maximum without inventing content.

No-steward tolerance falls out of the existing mechanism for free: `notifyToSteward` no-ops when no steward marker is present, so a live-test-review gate WAITS exactly like a question/review gate does — no separate detection or fallback needed here.

## steward-reply — why the answer message is the unblock

The parked DRI is waiting on the answer message itself; that mail is what it resumes on. Clearing the gate is a separate step the DRI performs for itself once it has processed the answer, which is why the Steward never calls `ateam clear-gate` — doing so would clear a gate the DRI has not yet acted on.

## steward-reply — the learning-capture rationale

Why a `corrected` verdict gets an immediate learning, not just a ledger row: the ledger's `--decision` field captures the raw call for that one case, but a case-by-case log doesn't generalize — you'd have to re-derive the pattern from scratch on every similar future gate. A distilled RULE/TRIGGER/APPLY learning turns one correction into a reusable rule, which is the highest-value thing a persistent Steward can produce, because it's what makes the NEXT recommendation closer to the human's actual preference instead of just repeating the same miss. The ledger is the tally; the learning is the lesson.

## steward-direct

**Two sources, one envelope kind.** The human reaches the Steward outside any initiative in two ways: by @mentioning the bot in the shared General channel, or by DMing the bot 1:1. Both produce a steward-direct envelope, because to the Steward they are the same thing — a conversation with no initiative behind it and no dedicated topic to reply into. Neither ever carries a thread ref.

The DM path additionally admits only allow-listed senders, so whether a given DM reaches you at all is settled upstream, before any envelope exists. That gate is not yours to reason about: act on the envelope you actually received, never on a belief about which paths are currently open.

**Why the header carries a reply-to ref anyway.** What the two sources do NOT share is where the answer belongs: an @mention was asked in front of the group and is answered in front of the group, while a DM was asked privately and must be answered privately. Nothing in the envelope body distinguishes them, so the relay puts the distinction in the header — a DM gets `reply-to:<ref>`, an @mention gets a bare header. That ref is the transport's own opaque handle to the originating message; only the transport can read it, which is why the Steward's job is to copy it rather than interpret it, and why `ateam notify direct` always takes an explicit `--to` (the ref, or the literal `general`) instead of inferring a destination it cannot see.

## steward-briefing-reply

The briefing-ack is never optional. A briefing reply has no initiative to route to, so silence on the Briefings thread reads as lost rather than as handled — the ack is the routing confirmation, not courtesy. That holds even when the substance itself is being routed to an initiative topic instead: the ack shrinks to a pointer, but it still gets sent.

No bead lives behind the Briefings topic by design — it's cross-initiative by construction, so there's no single initiative record to anchor it to. That's also why it isn't a fixed "always bounce back to Briefings" case: unlike an initiative topic, there's no beads state to check for whether the topic itself is still "open."

## Briefing topic identity (SKILL.md §7)

No initiative bead backs the `briefing` handle; the topic and its thread are created and persisted automatically on first use, the same way a dedicated per-initiative topic is created on first use — there's no separate provisioning step.
