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

A **beads-backed mailbox** (durable, synced `type=message` beads) + **drain** (`ateam inbox`, per-turn via `UserPromptSubmit`) + **wake** (an `asyncRewake` Stop-hook background watcher that blocks on a per-recipient doorbell file and re-wakes the idle session on arrival, self-renewing via a heartbeat so it never hits the harness hook-timeout) + a **conditional `ateam resume` fallback** for sessions whose process has exited. Cross-session messaging is the **generalization of the gate protocol**: same store, same drain-on-turn habit, sender widened from human-only to anyone, plus a real wake for idle recipients.

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
| 11 | A watcher **survives sleep** when the sleep doesn't cross its `timeout` | `sleeptest2` (timeout 24h, slept ~2.7min) — process alive, log resumed, doorbell still woke it (`proof: woke`) |
| 12 | The hook `timeout` **counts wall-clock through sleep** — a sleep that crosses it reaps the watcher mid-sleep | `sleeptest` (timeout 120s, slept ~8min > timeout) — watcher **dead**, doorbell no longer woke it |

**Recommendation: each session arms a singleton `asyncRewake` Stop-hook watcher that blocks on a per-recipient doorbell file and exits 2 to wake the session, self-renewing via a heartbeat so it never hits the hook timeout. The sender rings the doorbell. A `claude --resume`/relaunch fallback covers exited sessions.**

**Mechanism.** The plugin ships a `Stop` hook `{type:"command", command:"<watcher>", async:true, asyncRewake:true, timeout:<T>}`. When a turn ends and the session goes idle, the hook launches a background watcher:

1. **Singleton.** Kill any prior watcher (pidfile), record own pid. Exactly one watcher regardless of how many Stop events fire (finding #9) — otherwise each Stop accumulates a watcher and a single doorbell fires all of them.
2. **Block.** A dependency-free poll-loop waits for *either* the doorbell *or* a heartbeat deadline. The poll-loop (not `fswatch` — not installed; FSEvents coalesces/drops) survives machine sleep (finding #10).
3. **Real mail** (doorbell present) → `exit 2`, stderr "you have mail; run `ateam inbox`" → session wakes (finding #5), drains.
4. **Heartbeat deadline** (just under `timeout`) → `exit 2`, stderr "heartbeat, no mail, do nothing" → cheap no-op turn → `Stop` fires → fresh watcher arms. Re-arming **before** the timeout defeats reaping (findings #6/#8); the session is guarded **indefinitely, no ceiling**.
5. **Stop-on-closed.** Each cycle the watcher cheaply checks initiative status (`ateam show <id>` — a shell call, not a session turn). **CLOSED** → `exit 0`, no re-arm: pulsing stops, the session goes quiet. The DRI does **not** stop its own session (the skill forbids self-`claude stop`), so on close the process lingers idle until a human reaps it — the watcher going quiet is what stops the token cost, since close does not kill the process.
6. **Disarm-on-active.** A `UserPromptSubmit` hook kills the pending watcher when the session becomes active for any reason; it re-arms on the next `Stop`. Prevents a stale watcher firing mid-active-turn.

**Defaults: heartbeat 4h, `timeout` 24h.** Mail latency is **not** the heartbeat — the doorbell poll (~1s) drives same-machine read latency to seconds (findings #5/#8); the heartbeat is purely keepalive (re-arm before the harness `timeout` reaps the watcher) plus the sleep margin. So heartbeat could be longer (its only cost is the no-op re-arm turn); 4h is a conservative cadence. The re-arm happens ~every 4h of *awake* time, one cheap no-op turn each, and `timeout` 24h is the death cap. Pulsing persists while the initiative is **OPEN** (incl. awaiting-merge — when pr-shepherd needs the session reachable) and self-silences on CLOSED (§3 step 5).

**Sleep behavior (laptop-tested, findings #11/#12).** The `timeout` counts wall-clock *through* sleep, but each heartbeat re-arm resets a fresh `timeout`. So the watcher dies **only when a single continuous sleep crosses the 24h `timeout`** from its last re-arm — i.e. a near-full-day uninterrupted close. Any shorter sleep: the watcher survives, and on wake the poll-loop re-stats the doorbell (catches mail) and fires the overdue heartbeat → re-arm. Normal overnight/weekend closes are fine. The residual >24h-continuous-close case leaves the session **alive but unguarded** until it next takes a turn; the clean recovery (filed as an enhancement, not v1) is a **wake-time re-arm** (a `pmset`/LaunchAgent wake hook that re-arms live sessions' watchers), with the larger `timeout` (e.g. 7d) as a zero-infra alternative. Manual recovery — making a session active again re-arms it — works for sessions a human touches but not for autonomous ones.

**Per-session, per-initiative keying (multi-session).** Each session arms its *own* singleton watcher, keyed on its initiative id: doorbell `~/.agent-teams/mailbox/<initiative-id>.wake`, pidfile `~/.agent-teams/mailbox/<initiative-id>.watcher.pid`. The session resolves "which initiative am I" by `worktree: $PWD` (as `compact-recovery.sh` does). N concurrent sessions → N independent watchers/doorbells; `ateam send <id>` rings exactly that initiative's doorbell, so only the right session wakes. Sessions whose `$PWD` isn't a registered initiative (teammate subagents, ad-hoc `claude`) arm nothing. The "singleton" is per-session — the kill-prior pidfile is scoped per initiative, so one session's re-arm never touches another's watcher.

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

**Recommendation: a beads `type=message` bead** in the GLOBAL workspace (`~/.agent-teams`), one per message. A small **per-recipient doorbell file** (e.g. `~/.agent-teams/mailbox/<initiative-id>.wake`) is the local wake signal the watcher blocks on — **not** the store, just the bell.

**Mechanism (validated against Gas City source — same substrate).** Beads has a **native `message` issue type** (GH#1347, "re-promoted to built-in for inter-agent communication"). It needs **no `types.custom` config**, is created `Ephemeral: true` (wisp tier), and is **excluded from work queries** (`bd ready`/`bd list` — verified: 0 message beads surface). Gas City stores a message exactly this way: `Type="message"`, `Assignee`=recipient, `From`=sender, title=subject, description=body. We adopt that schema (§8). It lives in the workspace Dolt DB, synced over `refs/dolt/data` like the registry and role memories — durability + cross-machine sync for free. The doorbell is local-only; cross-machine delivery is the synced message + the relaunch fallback after `bd dolt pull`, not the doorbell.

**No-TTL-on-unread invariant (verified in beads + Gas City).** `Ephemeral: true` only makes a message *eligible* for opt-in cleanup (`bd cleanup --ephemeral --older-than N`); there is **no background process that silently expires unread mail**, and `compaction_enabled`/`auto_compact_enabled` default **false**. Gas City's sweep only touches **read** mail (`Type:"message", Label:"read"`, ~60min). So an undelivered message persists indefinitely; the self-cleaning applies only to already-read/closed mail — which is the benefit, not a risk.

**Beads has no native send.** `bd mail` is a *delegation shim* (it execs the configured provider, default `gt mail`). So `ateam send`/`ateam inbox` aren't reinventing beads mail — they *are* the provider beads delegates to (we could `bd config set mail.delegate "ateam mail"`). The value of `ateam send` is **convention, not capability** (recipient addressing; doorbell; read/ack lifecycle) — so it stays thin.

**Alternatives rejected:** (a) `task`+`msg:*` label — pollutes the work queue (`bd ready` would surface messages) and is a weaker discriminator than the native type; (b) a filesystem mailbox as the *store* — loses beads' cross-machine sync (a doorbell *file* as a *signal* is fine); (c) a new table/DB — violates reuse-beads-if-it-fits.

**Cardinal-rule extension.** The workspace gains a **third** sanctioned bead kind (message). Mitigation: `ateam audit` recognizes `type=message` beads as legitimate (a clean intrinsic discriminator) and still catches leaked work beads — a **required** enhancement (§11).

## 5. Q2 — Addressing

**Recommendation: the initiative id is the stable recipient handle** (the session id rotates; worktree/branch are stable but repo-relative).

**Mechanism.** The recipient initiative id is stored in the message bead's **`assignee`** field (Gas City's convention: `Assignee`=recipient, `From`=sender). The drain resolves "messages for me" by the recipient's own initiative (resolved by `worktree: $PWD`, as `compact-recovery.sh` does today) and queries `assignee == <my initiative id>`. External senders holding only branch/PR identity (pr-shepherd) resolve branch → initiative id via the `branch:` line in open initiative bodies — the `agent-teams-fkr.6` branch-match — and that lives with the consumer, not this primitive. The resolver should **reject ambiguous addresses** (Gas City's `ResolveRecipient` errors rather than guessing).

**Liveness/session resolution = `claude agents --json`** (finding #2): sessionId, cwd, name(=slug), status for every live bg session. This is the conditional-wake check and removes any need to record a session id in the bead — liveness is queried live, and the relaunch fallback resolves by worktree. (This deletes the session-id-recording change the first draft required.)

## 6. Q3 — Drain (pickup)

**Recommendation: a new verb `ateam inbox` drains the mailbox, run on every turn via a `UserPromptSubmit` hook (Gas City's model), with `SessionStart` reserved for priming.** `ateam inbox` resolves the recipient by `worktree: $PWD`, queries unread message beads (`assignee == me`, no `read` label), prints them as a `<system-reminder>` block, and marks each **read** (adds the `read` label, **keeps the bead open**) + writes the delivery ack (§7).

**Why `UserPromptSubmit`, not `SessionStart` (corrected from the first draft, per Gas City).** Draining on *every turn boundary* catches mail that arrives **mid-session**, not just at startup — Gas City wires `gt mail check --inject` to `UserPromptSubmit` and reserves `SessionStart` for `gt prime`. We do the same: the `UserPromptSubmit` hook **both drains the mailbox and disarms the wake watcher** (the session is now active; it re-arms on the next `Stop`). `SessionStart` runs `ateam inbox` only as a cold-path catch (startup/relaunch-resume/post-compaction). On an `asyncRewake` wake (§3) the rewake instruction tells the session to run `ateam inbox`, so the idle-wake path drains even if the rewake turn does not itself fire `UserPromptSubmit`.

## 7. Q5 — Delivery guarantees

**At-least-once + idempotent drain + two-phase delivery acks (Gas Town's pattern, which Gas City regressed on).** Beads is durable + synced, so a message persists until drained. The drain dedups by **message id** and is idempotent. Lifecycle (Gas City invariants): a message is **open + no `read` label** = unread; the drain adds the **`read` label and keeps the bead open** (still queryable); **archive/close** is a separate, later cleanup (sweep only touches read mail). So the dedup key for "don't re-inject" is the `read` label, not closing the bead.

**Delivery acks are in scope, not deferred.** Gas Town uses cheap, crash-safe label-based two-phase delivery: every send starts `delivery:pending`; the drain writes `delivery-acked-by:<id>`, `delivery-acked-at:<ts>`, `delivery:acked` and removes `delivery:pending`, idempotently. Gas City dropped this and admits "no delivery confirmation" — a regression we avoid. pr-shepherd itself is fire-and-forget (HTTP 2xx), but the ack labels give an overseer or a sender that *does* care visibility into whether a message was actually seen, at near-zero cost.

## 8. Schema, verbs, hook contract (frozen in the CONTRACT bead)

A message is a beads `type=message` bead (native, `Ephemeral: true`). Fields map to beads' built-ins (Gas City convention):

| Field | beads field | Meaning |
|---|---|---|
| id | bead id | Dedup key |
| sender | `From` | Free-text identity (`pr-shepherd-conductor`, `overseer`, `dri:<id>`) — keeps the primitive symmetric (Q6) |
| recipient | `Assignee` | Recipient **initiative id** |
| subject | `Title` | Short subject |
| body | `Description` | Message text |
| created | bead-native | Timestamp |
| unread/read | status open + `read` label | Open & no `read` label = unread; drain adds `read`, keeps open; archive/close = later cleanup |
| delivery ack | `delivery:pending`→`delivery:acked` + `delivery-acked-by/at` labels | Two-phase, idempotent (§7) |
| thread | `thread:<id>` label | Threading (Q6) |

**Verbs (frozen):**
- `ateam send <recipient-initiative-id> --file <path> [--sender <id>] [--thread <id>]` — create a `type=message` bead (`Assignee`=recipient, `From`=sender, `delivery:pending`) **and** touch the doorbell; then if `claude agents --json` shows a live session → done, else `ateam resume <id>`.
- `ateam inbox [--json]` — drain: resolve recipient by `worktree:$PWD`, query unread (`assignee==me`, no `read` label), print as `<system-reminder>`, add `read` label (keep open), write the delivery ack.
- (reuse) `ateam resume <id>` — dead-session fallback (#16).

**Hook contract (frozen):**
- `UserPromptSubmit` hook → runs `ateam inbox` (per-turn drain) **and** disarms the wake watcher (session is active; re-arms on next Stop). The primary drain path.
- `Stop` hook `{type:"command", command:"<watcher>", async:true, asyncRewake:true, timeout:<T>}` → the wake watcher: **singleton** (pidfile, kill-prior); **poll-loop** block (no `fswatch` dep; sleep-safe); `exit 2` on doorbell (→ run `ateam inbox`) or heartbeat-deadline (→ cheap re-arm turn); **stop-on-closed** (per-cycle status check → `exit 0`); heartbeat interval just under `timeout`.
- `SessionStart` hook (`startup|resume|clear|compact`) → `ateam inbox` cold-path catch (priming otherwise reserved for existing hooks).

## 9. Q6 — Peer-to-peer (secondary)

**Same primitive, symmetric.** A peer writes `ateam send <target-initiative-id>` + rings the doorbell; `sender` is just a field. **Distinct from the already-shipped intra-team P2P** (`#18`/at-pkj — teammates in *one* session using the harness `SendMessage` tool, in-process/ephemeral). This is the **cross-session, durable** layer; `SendMessage` is its in-session analogue. Gated additions: peer etiquette / anti-wake-spam (cross-session; distinct from `agent-teams-3cn`'s intra-team "don't decide over the DRI's head") and permissions.

## 10. Prior art and where we diverge

This sits in the **durable-signal** family — Temporal **signals**, LangGraph **resume**, the Claude **Sessions API** enqueue-to-session-id. Our recipient handle (initiative id) is the analogue of the workflow/session id; our drain is the idempotent handler.

Closest concrete prior art is **Gas Town / Gas City** (gastownhall, the same org as beads) — investigated at source. We adopt its **validated schema** (`type=message` bead, `Assignee`=recipient, `From`=sender, read-label, `thread:` label), its **per-turn `UserPromptSubmit` drain**, and Gas Town's **two-phase delivery acks** (Gas City dropped these — a regression we avoid). We **diverge on the wake, with evidence on our side.** Gas Town's *primary* drain is the same `UserPromptSubmit` queue we use — but it states plainly *"idle agents never submit, so queued nudges deadlock,"* and its fix is a **daemon-started poller (10s) that injects via `tmux send-keys`** (terminal puppeting, with copy-mode guards, SIGWINCH dances, literal-mode + separate-Enter + 3× verified-Enter retries, per-session flock — all the machinery that proves send-keys is fragile). So **even Gas Town has no clean non-tmux idle-wake**; the poller+tmux is exactly the fallback our **`asyncRewake` doorbell replaces** — a push-on-enqueue wake (spike-proven, §3) that needs no daemon and no terminal. Their own "normal wake" is feed-subscription (`bd activity --follow`), which still needs a live listener; asyncRewake is that listener, harness-native. We also verified the cleaner-looking alternatives (FileChanged, live `--resume`, the Managed-Agents Sessions API) do **not** apply to local `claude --bg` sessions; the Managed-Agents cloud API is the only first-class "enqueue to a session id" but would require re-platforming execution off local CLI sessions — recorded as a future foundation, not a v1 dependency. One watch-item Gas Town's design flags: the recurring failure mode is *"idle agent never drains its own queue"* — our defense is that the doorbell→exit-2 wake is genuinely push, not a queue the idle agent must notice.

## 11. Bead plan (concentric)

Filed in the project repo (`agent-teams-*`, tagged `at-1xd`). **Design-first: nothing implemented before this doc is approved.** The wake is now **spike-proven**, so the riskiest unknown is retired and the loop-closing set is smaller than the first draft.

**CONTRACT** (`agent-teams-29k`, the review artifact): freezes §8 — the `type=message` schema (validated by Gas City), addressing (initiative id in `assignee`; `claude agents --json` for liveness; **no session-id-in-bead**), the verbs, and the hook contract (`UserPromptSubmit` drain+disarm; `Stop` asyncRewake watcher — singleton + heartbeat + stop-on-closed + poll-loop; `SessionStart` cold drain). Bead type is **resolved** (native `message`), so the only open CONTRACT decisions are the heartbeat/timeout defaults and the stop-on-closed status-check (doc §12).

**LOOP-CLOSING POC SET** (blocked on CONTRACT + review; file-disjoint):
1. **`ateam send`/`ateam inbox` verbs** — `type=message` bead write (`assignee`/`From`/`delivery:pending`) + query unread + mark read (label, keep open) + two-phase delivery ack + doorbell touch + `claude agents --json` liveness check + `ateam resume` escalation.
2. **Hooks + watcher** — the `UserPromptSubmit` drain+disarm hook, the `asyncRewake` Stop watcher (singleton/heartbeat/poll-loop/stop-on-closed), the `SessionStart` cold drain — wired in plugin `hooks.json`, coexisting with the existing plugin hooks.
3. **Loop validation** — in a **real `/dri` bg session**: `ateam send` → idle session woken by the doorbell → `ateam inbox` drains (read + ack) → message marked read; heartbeat re-arms across the timeout; a second message after many heartbeats still wakes; mid-session mail drains via `UserPromptSubmit`; stop-on-closed silences the pulse; dead-session fallback escalates to `ateam resume` without double-launch.

**GATED ENHANCEMENTS** (blocked until loop closes):
- `ateam audit` awareness of the `type=message` bead kind — **required** (§4 mitigation).
- Read-mail sweep / cleanup (`bd cleanup --ephemeral`, read-only) — self-cleaning mailbox.
- Message threading beyond a `thread:` label (Q6).
- pr-shepherd conductor wiring — **references `agent-teams-fkr.6`** as the consumer; does not duplicate it.
- P2P etiquette / anti-wake-spam (Q6).
- `ateam resume` double-launch guard — fold the conditional `claude agents --json` check into `agent-teams-o05`.

(Two-phase delivery acks are now **in the loop-closing set** (Track 1), not gated — Gas Town shows them cheap + crash-safe, §7.)

## 12. Open questions for the human (review gate)

Resolved: **bead type** (native `type=message`), **read/delivered marker** (`read` label + keep open), **drain timing** (`UserPromptSubmit` per-turn), **heartbeat/timeout** (4h / 24h — mail latency is the ~1s doorbell, not the heartbeat; 24h is the sleep-survival cap), **sleep recovery** (24h timeout survives normal closes; >24h-continuous-close is a noted edge with a wake-time re-arm enhancement). Remaining:

1. **Stop-on-closed via status check** — accept the per-heartbeat `ateam show` status check as the silence mechanism (since close does not kill the process), or prefer an explicit teardown step that signals the watcher?
