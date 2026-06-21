# Agent Teams — Cross-Session Messaging Design

**Date:** 2026-06-19
**Status:** Proposed design, pre-implementation. The wake mechanism is **validated by spike** (see §3); the POC is gated on human review of this doc.
**Origin:** Initiative `at-1xd`. Generalizes the existing human-gate protocol into a delivery primitive any sender can use, and supplies the message-delivery mechanism that `agent-teams-fkr.6` (pr-shepherd ROUTE-TO-EXISTING) named as its gating unknown.

## 1. Problem

Today an agent-teams session can only be reached by **one** sender (the human), through **one** channel (the gate protocol: a note on the initiative bead + a `human`/`gate:*` label, drained on the session's next turn via `ateam human-list`). Two gaps follow:

1. **Sender is hardcoded to "human."** A PR-shepherd conductor, an overseer, or a peer DRI session has no sanctioned way to put a message in front of a running session. `agent-teams-fkr.6`: *"There is no current mechanism to push a message into an already-running/idle background claude session."*
2. **No wake.** The gate protocol relies on the recipient *taking a turn* to drain. A live, interactive session does that naturally. An **idle** background session — parked at a gate, or done with its last turn (delivered + awaiting PR merge) — never drains until something makes it act. This idle/awaiting-merge state is the primary case: it is exactly when the pr-shepherd reports PR comments, CI failures, or an out-of-date branch, and the session must be reachable to act on them.

We want: any sender can deliver a durable message to any session, addressed by a stable handle, and — for idle recipients — wake them to process it. Cross-machine, crash-survivable, built by **reuse, not a parallel channel**.

## 2. Recommendation in one line

A **beads-backed mailbox** (durable, synced message beads) + **drain** (`ateam inbox`, at `SessionStart` and on wake) + **wake** (an `asyncRewake` Stop-hook background watcher that blocks on a per-recipient doorbell file and re-wakes the idle session on arrival, self-renewing via a heartbeat so it never hits the harness hook-timeout) + a **conditional `ateam resume` fallback** for sessions whose process has exited. Cross-session messaging is the **generalization of the gate protocol**: same store, same drain-on-turn habit, sender widened from human-only to anyone, plus a real wake for idle recipients.

The six questions are answered mechanism-first with alternatives-rejected. **§3 (idle wake) is backed by an empirical spike against Claude Code 2.1.183** and is presented first because it constrains everything else.

## 3. Q4 — Idle wake (the core), and what the spike established

Every load-bearing assumption was tested against the running harness (CC 2.1.183, real `claude --bg` sessions), not inferred from docs:

| # | Finding | How verified |
|---|---|---|
| 1 | Background sessions **stay alive idle** after their turn | `claude agents --json` shows finished DRIs as `idle/done` with live pids |
| 2 | `claude agents --json` is a **liveness + addressing** primitive (sessionId, cwd, name=slug, status) | direct call; `kill -0 pid` corroborates. `claude stop` resolves by **id**, not name |
| 3 | **FileChanged is delivery-only, NOT a wake** — fires while idle and injects `additionalContext`, but does not trigger a turn; it buffers for the next turn | haiku-idle, sonnet-idle, sonnet-busy + transcript inspection; docs (FileChanged "Can block? No"; only SessionStart `initialUserMessage` "creates the turn") and GH #58182/#31854 |
| 4 | **`claude --resume -p` is refused on a live bg session** ("currently running as a background agent") | direct attempt; works only after the session has exited |
| 5 | **`asyncRewake` Stop hook DOES wake an idle session** — `{async,asyncRewake}` runs the command backgrounded; **exit code 2** wakes the session, stderr becomes the instruction; re-arms each Stop | spike: idle + background watcher → external write → `idle→busy` → ran instructed command; 2 cycles |
| 6 | **async hooks are reaped at `timeout`** (default **600s**), and cancelled on session end | `rewake-long` (default) watcher **DEAD at 601s** |
| 7 | A **large explicit `timeout` is honored** | `rewake-big` (`timeout: 86400`) watcher **ALIVE at 679s**, still rewakes |
| 8 | A **self-renewal heartbeat** defeats the timeout | `hb` watcher: **8 re-arm cycles** with no mail, then a real message still woke + drained |
| 9 | A **pidfile singleton** prevents watcher accumulation | naive 3 watchers → 3 fires (bug); singleton 3 arms → 1 fire (fixed) |
| 10 | A **dependency-free poll-loop survives macOS sleep** (`fswatch`/`inotifywait` not installed; `sleep` is wall-clock `nanosleep` — process freezes/resumes, re-stats on next tick) | Apple FSEvents + fswatch/watchexec research |

**Recommendation: each session arms a singleton `asyncRewake` Stop-hook watcher that blocks on a per-recipient doorbell file and exits 2 to wake the session, self-renewing via a heartbeat so it never hits the hook timeout. The sender rings the doorbell. A `claude --resume`/relaunch fallback covers exited sessions.**

**Mechanism.** The plugin ships a `Stop` hook `{type:"command", command:"<watcher>", async:true, asyncRewake:true, timeout:<T>}`. When a turn ends and the session goes idle, the hook launches a background watcher:

1. **Singleton.** Kill any prior watcher (pidfile), record own pid. Exactly one watcher regardless of how many Stop events fire (finding #9) — otherwise each Stop accumulates a watcher and a single doorbell fires all of them.
2. **Block.** A dependency-free poll-loop waits for *either* the doorbell *or* a heartbeat deadline. The poll-loop (not `fswatch` — not installed; FSEvents coalesces/drops) survives machine sleep (finding #10).
3. **Real mail** (doorbell present) → `exit 2`, stderr "you have mail; run `ateam inbox`" → session wakes (finding #5), drains.
4. **Heartbeat deadline** (just under `timeout`) → `exit 2`, stderr "heartbeat, no mail, do nothing" → cheap no-op turn → `Stop` fires → fresh watcher arms. Re-arming **before** the timeout defeats reaping (findings #6/#8); the session is guarded **indefinitely, no ceiling**.
5. **Stop-on-closed.** Each cycle the watcher cheaply checks initiative status (`ateam show <id>` — a shell call, not a session turn). **CLOSED** → `exit 0`, no re-arm: pulsing stops, the session goes quiet. The DRI does **not** stop its own session (the skill forbids self-`claude stop`), so on close the process lingers idle until a human reaps it — the watcher going quiet is what stops the token cost, since close does not kill the process.
6. **Disarm-on-active.** A `UserPromptSubmit` hook kills the pending watcher when the session becomes active for any reason; it re-arms on the next `Stop`. Prevents a stale watcher firing mid-active-turn.

**Cost is a tunable knob, near-zero.** The heartbeat interval is set just under a large `timeout` (finding #7), so the re-arm turn happens ~once per interval — minutes-to-~daily — one cheap no-op turn per interval. This gives unbounded guard (no finite ceiling — critical for awaiting-merge PRs that sit for days) at a cost approaching zero. Pulsing persists while the initiative is **OPEN**, including awaiting-merge — exactly when the pr-shepherd needs the session reachable.

**Sender side.** `ateam send <id>` (§8) writes the message bead **and** touches the recipient's doorbell file; it never spawns a process. N messages → N beads + one doorbell → **0 sessions**. Then:
- `claude agents --json` shows a **live** session → the doorbell wakes it. Done.
- **No live session** (exited/torn down) → escalate to **`ateam resume <id>`** (shipped, #16): resolves the worktree, relaunches `/dri`, whose `SessionStart` drains. Conditional + idempotent (checked against `claude agents --json`), so no double-launch (the `agent-teams-o05` concern).

**Alternatives rejected (spike-verified):**
- **FileChanged as the wake** — delivery-only; never triggers a turn on an idle session (#3).
- **`claude --resume -p` into a live session** — refused (#4); viable only post-exit (the fallback case).
- **`ateam resume` as *primary* wake** — relaunches a *fresh* `/dri` (heavier, loses in-memory context) and double-launches if uncoordinated (`o05`). Correct only as the conditional dead-session fallback.
- **`/loop` self-poll** — burns a real turn every interval regardless of mail; the heartbeat only pays when re-arming near the timeout and wakes *immediately* on real mail.
- **Unbounded watcher (no heartbeat)** — killed at the hook timeout (#6), then unguarded with no turn to re-arm. The heartbeat is the fix.
- **Routines / fresh spawn** — new session, can't reach a specific live one with its context.

## 4. Q1 — Transport: where a message lives

**Recommendation: beads.** Dedicated message beads in the GLOBAL workspace (`~/.agent-teams`), one per message, labeled by recipient initiative. A small **per-recipient doorbell file** (e.g. `~/.agent-teams/mailbox/<initiative-id>.wake`) is the local wake signal the watcher blocks on — **not** the store, just the bell.

**Mechanism.** The message is a bead (a `msg:*` label or beads' built-in `Message` type — resolve in CONTRACT with evidence) carrying the §8 schema, living in the workspace Dolt DB, synced over `refs/dolt/data` like the registry and role memories — durability + cross-machine sync for free. The doorbell is local-only; cross-machine delivery is the synced message + the relaunch fallback after `bd dolt pull`, not the doorbell.

**Beads has no native send.** `bd mail` is a *delegation shim* ("mail functionality is typically provided by the orchestrator… delegates to the configured mail provider", default `gt mail`), and there is **no native `Message` issue type** (types: `bug|feature|task|epic|chore|decision`). So `ateam send`/`ateam inbox` aren't reinventing beads mail — they *are* the provider beads delegates to (we could `bd config set mail.delegate "ateam mail"`). The value of `ateam send` is **convention, not capability** (recipient = initiative id; doorbell; delivered-marker) — so it stays thin.

**Alternatives rejected:** (a) gate notes on the initiative bead — conflates phase narrative with inbound mail, no per-message lifecycle/dedup; (b) a filesystem mailbox as the *store* — loses beads' cross-machine sync (a doorbell *file* as a *signal* is fine); (c) a new table/DB — violates reuse-beads-if-it-fits.

**Cardinal-rule extension.** The workspace gains a **third** sanctioned bead kind (message). Mitigation: a distinct type/label so `ateam audit` recognizes message beads and still catches leaked work beads — a **required** enhancement (§9).

## 5. Q2 — Addressing

**Recommendation: the initiative id is the stable recipient handle** (the session id rotates; worktree/branch are stable but repo-relative).

**Mechanism.** A sender addresses a message to an initiative id; the drain resolves "messages for me" by the recipient's own initiative (resolved by `worktree: $PWD`, as `compact-recovery.sh` does today). External senders holding only branch/PR identity (pr-shepherd) resolve branch → initiative id via the `branch:` line in open initiative bodies — the `agent-teams-fkr.6` branch-match — and that lives with the consumer, not this primitive.

**Liveness/session resolution = `claude agents --json`** (finding #2): sessionId, cwd, name(=slug), status for every live bg session. This is the conditional-wake check and removes any need to record a session id in the bead — liveness is queried live, and the relaunch fallback resolves by worktree. (This deletes the session-id-recording change the first draft required.)

## 6. Q3 — Drain (pickup)

**Recommendation: a new verb `ateam inbox` drains the mailbox; it runs at `SessionStart` and on every wake.** It resolves the recipient by `worktree: $PWD`, queries undelivered message beads, prints them (`--json` for the hook), and marks each delivered (§7). It runs (1) in a `SessionStart` hook (`startup|resume|clear|compact`) for the cold path — startup, post-compaction, relaunch-resume; and (2) on a doorbell wake via the watcher's rewake instruction (§3). The session may also call it explicitly mid-turn.

## 7. Q5 — Delivery guarantees

**At-least-once + idempotent drain.** Beads is durable + synced, so a message persists until drained. The drain dedups by **message id** and marks each delivered (close the bead or a `delivered` label — CONTRACT picks one), so a re-fired hook/heartbeat never re-injects a seen message. Exactly-once is not attempted. **Acks/read-receipts deferred** (gated) — pr-shepherd is fire-and-forget (HTTP 2xx only).

## 8. Schema, verbs, hook contract (frozen in the CONTRACT bead)

| Field | Req | Meaning |
|---|---|---|
| `id` | yes | Bead id; dedup key |
| `sender` | yes | Free-text identity (`pr-shepherd-conductor`, `overseer`, `dri:<id>`) — a field, not a type, keeping the primitive symmetric (Q6) |
| `recipient` | yes | Recipient **initiative id** |
| `body` | yes | Message text |
| `created` | yes | Timestamp |
| `delivered` | yes | Marker set by the drain |
| `replies_to` | no | Parent message id (threading, Q6) |

**Verbs (frozen):**
- `ateam send <recipient-initiative-id> --file <path> [--sender <id>] [--replies-to <id>]` — write a message bead **and** touch the doorbell; then if `claude agents --json` shows a live session → done, else `ateam resume <id>`.
- `ateam inbox [--json]` — drain: resolve by `worktree:$PWD`, print undelivered, mark delivered.
- (reuse) `ateam resume <id>` — dead-session fallback (#16).

**Wake hook contract (frozen):**
- `Stop` hook `{type:"command", command:"<watcher>", async:true, asyncRewake:true, timeout:<T>}` in plugin `hooks.json`.
- Watcher: **singleton** (pidfile, kill-prior); **poll-loop** block (no `fswatch` dep; sleep-safe); `exit 2` on doorbell (drain) or heartbeat-deadline (re-arm); **stop-on-closed** (status check → `exit 0`); heartbeat interval just under `timeout`.
- `UserPromptSubmit` hook **disarms** the watcher when the session becomes active (re-arms on next Stop).
- `SessionStart` hook (`startup|resume|clear|compact`) runs `ateam inbox` for the cold path.

## 9. Q6 — Peer-to-peer (secondary)

**Same primitive, symmetric.** A peer writes `ateam send <target-initiative-id>` + rings the doorbell; `sender` is just a field. **Distinct from the already-shipped intra-team P2P** (`#18`/at-pkj — teammates in *one* session using the harness `SendMessage` tool, in-process/ephemeral). This is the **cross-session, durable** layer; `SendMessage` is its in-session analogue. Gated additions: peer etiquette / anti-wake-spam (cross-session; distinct from `agent-teams-3cn`'s intra-team "don't decide over the DRI's head") and permissions.

## 10. Prior art and where we diverge

This sits in the **durable-signal** family — Temporal **signals**, LangGraph **resume**, the Claude **Sessions API** enqueue-to-session-id. Our recipient handle (initiative id) is the analogue of the workflow/session id; our drain is the idempotent handler.

Closest concrete prior art is **Gas Town** (gastownhall, the same org as beads): durable **mail** + a **nudge** wake. We adopt the split and **diverge** on the wake: Gas Town nudges via **`tmux send-keys`** (terminal puppeting, no delivery confirmation, layout-fragile); we use an **`asyncRewake` background hook** — a documented primitive that reopens the exact session and returns an exit status, no terminal. We verified (§3) the cleaner-looking alternatives (FileChanged, live `--resume`, the Managed-Agents Sessions API) do **not** apply to local `claude --bg` sessions; the Managed-Agents cloud API is the only first-class "enqueue to a session id" but would require re-platforming execution off local CLI sessions — recorded as a future foundation, not a v1 dependency.

## 11. Bead plan (concentric)

Filed in the project repo (`agent-teams-*`, tagged `at-1xd`). **Design-first: nothing implemented before this doc is approved.** The wake is now **spike-proven**, so the riskiest unknown is retired and the loop-closing set is smaller than the first draft.

**CONTRACT** (`agent-teams-29k`, the review artifact): freezes §8 — schema, addressing (initiative id; `claude agents --json` for liveness; **no session-id-in-bead**), the verbs, and the wake-hook contract (singleton + heartbeat + stop-on-closed + disarm-on-active + poll-loop). Resolves the bead-type question with evidence.

**LOOP-CLOSING POC SET** (blocked on CONTRACT + review; file-disjoint):
1. **`ateam send`/`ateam inbox` verbs** — message-bead write/query/mark-delivered + doorbell touch + `claude agents --json` liveness check + `ateam resume` escalation.
2. **Wake hook + watcher** — the `asyncRewake` Stop hook, the singleton/heartbeat/poll-loop/stop-on-closed watcher, the `UserPromptSubmit` disarm hook, the `SessionStart` `ateam inbox` drain — wired in plugin `hooks.json`.
3. **Loop validation** — in a **real `/dri` bg session** (alongside the plugin's existing Stop hooks): `ateam send` → idle session woken → `ateam inbox` drains → delivered; heartbeat re-arms across the timeout; a second message after many heartbeats still wakes; stop-on-closed silences the pulse. (The earlier session-id-recording track is dropped.)

**GATED ENHANCEMENTS** (blocked until loop closes):
- `ateam audit` awareness of the message bead kind — **required** (Q1 mitigation).
- Acks / read-receipts (Q5).
- Message threading `replies_to` (Q6).
- pr-shepherd conductor wiring — **references `agent-teams-fkr.6`** as the consumer; does not duplicate it.
- P2P etiquette / anti-wake-spam (Q6).
- `ateam resume` double-launch guard — fold the conditional `claude agents --json` check into `agent-teams-o05`.

## 12. Open questions for the human (review gate)

1. **Heartbeat interval / timeout** — defaults (e.g. `timeout: 86400`, heartbeat ~daily) vs. a tighter pulse for faster recovery after an undetected watcher death.
2. **Bead type** — built-in `Message` type if it fits, else `task`+`msg:*`: decide at CONTRACT time, or pre-commit now?
3. **Delivered marker** — close the message bead vs. a `delivered` label?
4. **Stop-on-closed via status check** — accept the per-heartbeat `ateam show` status check as the silence mechanism (since close does not kill the process)?
