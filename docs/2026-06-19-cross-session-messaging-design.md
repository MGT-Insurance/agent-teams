# Agent Teams — Cross-Session Messaging Design

**Date:** 2026-06-19
**Status:** Proposed design, pre-implementation (design-first; the POC is gated on human review of this doc)
**Origin:** Initiative `at-1xd`. Generalizes the existing human-gate protocol into a delivery primitive any sender can use, and supplies the message-delivery mechanism that `agent-teams-fkr.6` (pr-shepherd ROUTE-TO-EXISTING) named as its gating unknown.

## 1. Problem

Today an agent-teams session can only be reached by **one** sender (the human), through **one** channel (the gate protocol: a note on the initiative bead + a `human`/`gate:*` label, drained on the session's next turn via `ateam human-list`). Two gaps follow:

1. **Sender is hardcoded to "human."** A PR-shepherd conductor, an overseer, or a peer DRI session has no sanctioned way to put a message in front of a running session. `agent-teams-fkr.6` states this directly: *"There is no current mechanism to push a message into an already-running/idle background claude session."*
2. **No wake.** The gate protocol relies on the recipient *taking a turn* to drain. A live, interactive session does that naturally. An **idle** background session — parked at a gate, or simply done with its last turn — never drains until something makes it act.

We want: any sender can deliver a durable message to any session, addressed by a stable handle, and — for idle recipients — wake them to process it. Cross-machine, crash-survivable, and built by **reuse, not a parallel channel**.

## 2. Recommendation in one line

A **beads-backed mailbox** (durable, synced message beads in the global workspace) + **drain-on-next-turn** (a `SessionStart` hook and an explicit `ateam inbox` verb) + **resume-to-wake** (`ateam wake <initiative-id>` runs `claude --resume <session-id> -p "<drain prompt>"`). Mail and wake are **cleanly separated concerns**, exactly as Gas Town (gastownhall, the same org as beads) separates durable `gt mail` from the `gt nudge`. Cross-session messaging is the **generalization of the gate protocol**: same store, same drain-on-turn habit, with the sender widened from human-only to anyone and a wake added for idle recipients.

The six design questions below each give the **recommendation, the mechanism first, then the alternatives and why they lost.**

## 3. Q1 — Transport: where a queued message physically lives

**Recommendation: beads.** Dedicated lightweight **message beads** in the GLOBAL workspace (`~/.agent-teams`), one bead per message, labeled/keyed by recipient initiative.

**Mechanism.** A message is a bead of a distinct kind (a `msg:*` label, or the beads built-in `Message` issue type — see below). Its body carries the schema in §8. It lives in the workspace's Dolt DB and syncs over `refs/dolt/data` on the workspace's git remote — the same persistence the initiative registry and role memories already use ([framework design](2026-06-11-agent-teams-framework-design.md) D4/D6). Durability and cross-machine sync come for free: a message persists until explicitly drained, and a sender on machine A can reach a recipient whose session is on machine B after a `bd dolt push`/`pull`. Writes are independent inserts (one bead per message), which embedded-mode beads serializes with zero loss — the exact write profile that made embedded mode safe for `register`/`bd create` (framework design D16a). No new daemon, no new store.

**Why the global workspace and not the project repo.** The recipient handle is the initiative (Q2), and the one machine-wide place that already indexes initiatives is the global workspace. A message addressed to an initiative on an arbitrary repo has nowhere else to land that every sender can reach.

**Evaluate: beads' built-in `Message` type.** Beads ships a `Message` issue type with threading. If it carries sender/recipient/body cleanly and its threading maps to replies-to (Q6), prefer it over a hand-rolled `task`+`msg:*` label — it is more reuse, not less. The CONTRACT bead must verify the `Message` type's fields and whether `ateam`'s `bd` client can create/query it; if it doesn't fit, fall back to `task` + `msg:*` label. Either way the schema in §8 is the frozen interface; the bead-type choice is an implementation detail behind it.

**Alternatives considered and rejected:**

- **(a) Generalize gate notes directly on the initiative bead.** Simplest, maximal reuse — the gate protocol already writes notes there. Rejected as the *store*: it conflates **phase narrative** (the initiative bead's notes are the session's own log: "Phase 4, spawned implementers") with **inbound messages from others**, and a note has **no per-message lifecycle** — no id to dedup on, no per-message delivered marker, no clean "unread" query. The mailbox needs per-message state; notes don't provide it. (The gate protocol's *habit* — write, drain on next turn — is exactly what we keep and generalize; only the storage moves from a note to a bead.)
- **(b) A filesystem mailbox under the workspace.** A `messages/` dir of files. Rejected: loses the cross-machine sync beads gives for free. A file mailbox on machine A is invisible to a recipient on machine B; we'd have to rebuild sync that `refs/dolt/data` already provides.
- **(c) A brand-new table or DB.** Rejected by the project's standing rule: reuse beads if it fits, and it fits. A new store adds a schema, a migration path, and a second source of truth for "what's pending," for no capability beads lacks.

**Cardinal-rule extension — called out explicitly.** The global workspace today holds **only** initiative-tracking beads + role memories; **all** work beads live in the project repo (plugin `CLAUDE.md`, "CARDINAL RULE"). Introducing a **message bead kind** in the workspace is a **deliberate, one-time extension** of that rule — the third sanctioned kind of workspace bead. Mitigation: messages carry a **distinct type/label** (`msg:*` or the `Message` type) so `ateam audit` — which flags any workspace issue lacking the initiative-tracking schema as a leaked work bead — can be taught to recognize message beads as legitimate and still catch genuine leaks. Without that, every message would trip the audit. `ateam audit` awareness of the message kind is therefore a **required** enhancement bead, not optional polish.

## 4. Q2 — Addressing: how a recipient is identified

**Recommendation: the initiative id is the stable recipient handle.**

**Mechanism.** Every running session is the DRI for exactly one initiative (framework design D10), registered in the workspace with `{repo, worktree, branch, team, mode}`. The initiative id is stable across the session's whole life; the **session id rotates on resume**, the worktree and branch are stable but repo-relative. A sender addresses a message to an initiative id; the drain (Q3) resolves "messages for me" by matching the recipient's own initiative (the `SessionStart` hook already resolves the initiative by `worktree: $PWD` — `compact-recovery.sh` does exactly this today).

**External senders that don't know the initiative id** address by what they *do* hold and let the workspace resolve it. pr-shepherd carries **branch/PR identity, not an initiative id**; the conductor resolves branch → initiative id by matching the PR head branch against the `branch:` line in open initiative bodies — **precisely the branch-match step `agent-teams-fkr.6` specifies** (`dispatch.go:204` writes `branch: <slug>`; surfaced via `ateam list-json`). Branch-match resolution is the conductor's job and lives with that consumer; the messaging primitive only needs the resolved initiative id.

**The gap this exposes, and the fix.** To **wake** an idle recipient (Q4) we need its **live session id**, and the workspace **does not record it today**. `dispatch.go:201-206` writes `repo/worktree/branch/team/mode` into the initiative body — **no session id.** So the messaging system requires a new write: **the workspace must record the live session id per initiative.** This is a small, contained change (one more schema line + the place that writes it), frozen in the CONTRACT bead.

**POC must-verify (flagged, not assumed).** Before committing to "store the session id," the POC must check whether `claude --resume` **run from the initiative's worktree auto-selects that worktree's most recent session** — if it does, the wake can `cd` to the worktree and resume without us storing any id, and the session-id-recording change is unnecessary. This is the single load-bearing unknown in addressing; the POC resolves it with evidence before the schema is finalized. Until resolved, the contract assumes session-id-recording is needed (the safe default).

## 5. Q3 — Pickup on next turn (non-idle recipient)

**Recommendation: a `SessionStart` hook drains the mailbox, plus an explicit `ateam inbox` verb the session can call mid-turn.**

**Mechanism.** A new verb **`ateam inbox`** (a.k.a. drain): resolves the recipient initiative by `worktree: $PWD`, queries undelivered message beads addressed to it, prints them, and marks each delivered (Q5). A new `SessionStart` hook calls `ateam inbox` and injects its output via **stdout → `additionalContext`** — the **exact mechanism** `compact-recovery.sh` already uses, and that PR #19 (`prime-user-memories.sh`, `subagent-prime-learnings.sh`) extends. The hook uses matchers **`startup|resume|clear|compact`** — the same set PR #19 adopts — so the mailbox drains on session start, on resume (the wake path, Q4), on clear, and after compaction. This is precisely Gas Town's `gt mail check --inject`. The session may also call `ateam inbox` explicitly at any point in a turn.

**The boundary, stated plainly.** A `SessionStart` hook fires at session **start/resume**, not mid-conversation. For a long-running interactive session sitting in the middle of a turn, the hook will **not** re-fire — so a message delivered to it relies on either (i) the session calling `ateam inbox` itself, or (ii) the **wake** (Q4), which triggers a `--resume` that fires the `SessionStart(resume)` hook. **The EXTERNAL → idle path is the wake path:** sender writes the message, sender fires the wake, the resume fires the hook, the hook drains. Drain-on-turn and wake are two halves of one delivery; neither alone covers the idle recipient.

**Alternative rejected: poll loop in the session.** Having the session periodically call `ateam inbox` on a timer. Rejected: it burns turns/tokens on empty polls, and the harness has no clean in-session timer; the wake is event-driven and costs nothing while idle.

## 6. Q4 — Idle wake (the hard part)

**Recommendation: `ateam wake <initiative-id>` = resolve the recipient's session id and run `claude --resume <session-id> -p "<short drain prompt>"`. This is our nudge. The SENDER fires it. No daemon in v1.**

**Mechanism.** `ateam wake <initiative-id>` looks up the recipient initiative's recorded live session id (Q2) and shells out to `claude --resume <session-id> -p "<drain prompt>"`. `claude --resume <id>` reopens **that specific idle session with its full context** and `-p` makes it **take one turn**; the `SessionStart(resume)` hook fires and drains the mailbox (Q3); the drain prompt is a short belt-and-suspenders instruction ("you have new mail; run `ateam inbox`"). Unlike Gas Town's nudge — `tmux send-keys`, which injects keystrokes into a pane with **no delivery confirmation** and breaks if the pane layout drifts — `claude --resume <id> -p` is a **documented harness primitive**: it returns an exit status, reopens the exact session, and is the same family as `dispatch.go`'s existing `claude --bg` launch (`launchBGSession`, line 87). It reuses the launch primitive's shape.

**The sender fires the wake.** The senders we have are **online when they send**: the pr-shepherd conductor is a running HTTP server (`agent-teams-fkr.1`), an overseer is a live session, a peer DRI is mid-turn. So the durable-mailbox-write and the wake are **two calls the sender makes back-to-back** — write the message, then `ateam wake <recipient>`. The mailbox guarantees the message survives even if the wake fails or the recipient is mid-turn; the wake guarantees a *currently-idle* recipient acts now.

**DECISION: no daemon/heartbeat in v1.** A background supervisor that polls the mailbox and wakes recipients with no live sender (Gas Town's 3-minute scheduler tick) is **deliberately deferred** — it is load-bearing complexity only when **no sender is online to fire the wake**, which none of v1's senders require. Per the project's simplicity prior, complexity must be justified by a mechanism we actually need now; this one isn't. It is filed as a **gated enhancement** (§9), not built.

**The unavoidable fork, stated explicitly.** "Durable across a crash **and** instant wake of a recipient that has no live sender" requires **either** a live sender that fires the resume **or** a scheduler tick that periodically drains-and-wakes. Beads gives us the durable mailbox for free; the **wake is the part we own.** v1 picks **"live sender fires resume"** because all v1 senders are online. The scheduler tick is the *only* thing that covers the senderless-idle case, and it is exactly the deferred enhancement — so the boundary of v1 is precise: every message has an online sender that can fire the wake.

**Alternatives considered and rejected:**

- **Routines / `claude` fire-and-forget spawn.** Spawns a **fresh** session — it cannot resume *a specific idle session* with its accumulated context, which is the whole requirement. A fresh session has none of the recipient's working state.
- **Keystroke injection (`tmux send-keys`, Gas Town's approach).** Fragile, layout-dependent, **no delivery confirmation**. `--resume` is strictly better: documented, addressable, status-returning.
- **PushNotification / RemoteControl / attach.** All need a **human** to act on them; the wake must be agent-to-agent with no human in the loop.

## 7. Q5 — Delivery guarantees

**Recommendation: at-least-once, with an idempotent drain.**

**Mechanism.** Beads is durable and synced, so a message **persists until drained** — that gives at-least-once by construction. The drain (`ateam inbox`) is **idempotent**: it dedups by **message id** and marks each delivered message — by **closing the message bead** or setting a **`delivered` label/field** — so a re-fired hook (resume, then a second resume) does not re-inject an already-seen message. This is the standard **at-least-once + idempotent-handler** contract from durable-execution systems (Temporal signals, etc.): the transport may deliver more than once; the handler makes redelivery harmless. Exactly-once is not attempted (it's the wrong, more-expensive guarantee for this).

**Acks / read-receipts back to the sender are deferred (gated).** v1's only concrete external sender, pr-shepherd, is **fire-and-forget** — its conductor checks only HTTP 2xx and ignores the response body (`agent-teams-fkr.1`, "pr-shepherd checks ONLY response.ok"). So the sender does not need to know the message was drained. A read-receipt channel (a reply message, or a sender-visible delivered flag) is filed as a gated enhancement (§9) for senders that later want it (an overseer tracking DRI responsiveness).

## 8. Message schema (frozen in the CONTRACT bead)

The schema is the interface every track builds against; the bead-type choice (built-in `Message` vs. `task`+`msg:*`) sits behind it.

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | Message identity (the bead id). Dedup key for the idempotent drain. |
| `sender` | yes | Free-text sender identity (e.g. `pr-shepherd-conductor`, `overseer`, `dri:<initiative-id>`). A field, not a type — keeps the primitive symmetric (Q6). |
| `recipient` | yes | The **recipient initiative id** (Q2). |
| `body` | yes | The message text. |
| `created` | yes | Creation timestamp (bead-native). |
| `delivered` | yes | Delivered marker — set by the drain. Closed bead **or** `delivered` label/field (CONTRACT picks one). |
| `replies_to` | no | Parent message id for threading (Q6). Maps to the `Message` type's threading if that type is used. |

**Verb signatures (frozen):**

- `ateam send <recipient-initiative-id> --file <path> [--sender <id>] [--replies-to <msg-id>]` — write a message bead. `--file` for the body (same `--body-file` discipline the existing write verbs use, avoiding `\n#` shell issues — see `write.go`).
- `ateam inbox` — drain: resolve recipient by `worktree: $PWD`, print undelivered messages, mark delivered. (`--json` for the hook to consume, mirroring `list-json`.)
- `ateam wake <initiative-id>` — resolve session id, `claude --resume <session-id> -p "<drain prompt>"`.

**Hook contract (frozen):** a `SessionStart` hook, matchers `startup|resume|clear|compact`, calling `ateam inbox` and injecting via stdout → `additionalContext` (the `compact-recovery.sh` / PR #19 mechanism). The drain must be a **silent no-op** when the mailbox is empty or cwd is not a registered initiative (same discipline as `compact-recovery.sh`) — teammate sessions also fire `SessionStart`, and must not be spammed.

**Session-id-recording change (frozen, pending the Q2 POC verify):** the workspace records the live session id per initiative (a new schema line written where `dispatch.go:201-206` writes the others), unless the POC proves worktree-scoped `--resume` auto-selection makes it unnecessary.

## 9. Bead plan (concentric)

Filed in the **project repo** (`agent-teams-*` prefix), tagged `at-1xd`. **Design-first constraint (from Eric): the POC must NOT be implemented before this doc is reviewed.** Every POC and enhancement bead therefore depends on the CONTRACT bead **and** is annotated "do not implement until design reviewed."

**CONTRACT (the thing the human reviews):**

- Freezes the §8 message schema, the addressing rule (Q2), the three verb signatures, the hook contract, and the session-id-recording change. Resolves the bead-type question (`Message` type vs. `task`+`msg:*`) with evidence. States the Q2 POC must-verify (worktree-scoped `--resume` auto-selection). Frozen interface = the fan-out point.

**LOOP-CLOSING POC SET (smallest end-to-end loop; blocked on CONTRACT + design review):** send → wake → drain → delivered. File-disjoint tracks so they parallelize:

1. **Go verbs** — `ateam send`, `ateam inbox`, `ateam wake` (new `internal/verbs/*.go`, `RegisterMessaging` in `main.go:62-67`, `cli.UsageText`). Owns the message-bead create/query/mark-delivered + the `--resume` shell-out.
2. **Hook script** — the `SessionStart(startup|resume|clear|compact)` drain script + `hooks.json` wiring (disjoint from the Go).
3. **Session-id recording** — the workspace schema line + write site near `dispatch.go:201-206` (disjoint file/concern).
4. **Loop validation** — exercise the load-bearing assumption: a message to an **idle bg session** wakes it via `claude --resume` and the hook drains it; message ends up delivered. This is what proves the design.

**GATED ENHANCEMENTS (all blocked until the loop closes / design approved):**

- **`ateam audit` awareness of the message bead kind** — required for the workspace to keep auditing clean (Q1 cardinal-rule mitigation). Highest-priority enhancement.
- **Poll/heartbeat supervisor** — senderless wake (the deferred Gas Town scheduler tick; Q4).
- **Acks / read-receipts** — sender-visible delivery (Q5).
- **Message threading** — `replies_to` chains surfaced in the drain (Q6).
- **pr-shepherd conductor wiring** — **references `agent-teams-fkr.6`** as the consumer; does **not** duplicate it. fkr.6 is the branch-match + deliver-to-DRI ring; this messaging primitive is the "deliver" half it was missing. Link, don't rebuild.
- **P2P etiquette / anti-wake-spam** — peer-to-peer rate-limiting/permissions (Q6).

## 10. Q6 — Peer-to-peer (secondary)

**Recommendation: the same primitive serves session → session. No new mechanism.**

**Mechanism.** A peer DRI writes a message addressed to another initiative (`ateam send <target-initiative-id>`) and optionally calls `ateam wake <target>`. `sender` is just a field (§8), so the primitive is **symmetric** — human, conductor, overseer, and peer session are interchangeable senders. The recipient drains identically; it cannot tell a peer message from a conductor message except by the `sender` field, which is correct.

**The only additions are gated:** peer **etiquette / anti-wake-spam** (a peer shouldn't be able to wake another session in a tight loop) and any **permissions** (who may address whom). Neither is load-bearing for v1's sender set (conductor/overseer/human), so both are deferred (§9). Confirming the primitive is symmetric is a design assertion, not extra code.

## 11. Prior art and where we diverge

This sits in the **durable-signal** family: a durable store plus an out-of-band signal that resumes a waiting computation — Temporal **signals** (signal-to-a-workflow-id, at-least-once, idempotent handler), LangGraph **resume**, and the Claude **Sessions API enqueue-to-session-id**. Our recipient handle (initiative id → session id) is the analogue of the workflow/session id; our drain is the idempotent handler.

The closest concrete prior art is **Gas Town** (gastownhall, the same org as beads), which already splits **durable mail** (`gt mail`) from a **wake** (`gt nudge`). We adopt that split wholesale. We **diverge in two places**:

1. **Our wake is `claude --resume <id> -p`** — a documented, addressable, status-returning harness primitive — **not** Gas Town's `tmux send-keys` keystroke shim (fragile, layout-dependent, no delivery confirmation).
2. **We defer the scheduler/heartbeat** that Gas Town ships, because all v1 senders are online to fire the wake themselves. We pay for it (the senderless-idle case is uncovered in v1) and we name the price (Q4's fork): the scheduler is the gated enhancement that closes that gap when a senderless sender appears.

## 12. Open questions for the human (review gate)

1. **Bead type:** accept "use beads' built-in `Message` type if it fits, else `task`+`msg:*`" as a CONTRACT-time decision, or pre-commit to one now?
2. **Delivered marker:** close the message bead vs. a `delivered` label — preference? (Closing is cleaner for "unread = open"; a label keeps message history queryable in the workspace, which `ateam audit` then has to tolerate.)
3. **The Q2 POC verify:** is "store the session id" acceptable as the safe default, with the POC free to remove it if worktree-scoped `--resume` auto-selection works?
4. **v1 sender boundary:** confirm that "every message has an online sender that can fire the wake" is an acceptable v1 constraint (i.e., the scheduler/heartbeat genuinely defers).
