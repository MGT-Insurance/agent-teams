# Codex app-server lifecycle spike

- **Beads:** `agent-teams-bhe0.10`, `agent-teams-bhe0.11`
- **Date:** 2026-08-06
- **Codex:** `codex-cli 0.145.0` (transient server), `0.146.1` (managed daemon)
- **Branch:** `codex/runtime-worker-contract`

[Open the styled HTML report](./2026-08-06-codex-app-server-spike.html)

## Decision

Use Codex app-server, rather than a detached `codex exec` process per turn, as
the preferred Codex DRI runtime.

Require the standalone Codex installation and use its supported
`codex app-server daemon` lifecycle. This eliminates the need for an
`ateam`-owned always-running process supervisor. The managed daemon owns its
process lifetime, active turns, interruption, thread persistence, and steering.
`ateam` still owns an idempotent daemon-start health check, a short-lived
per-initiative delivery lock, Beads-backed mail truth, and crash reconciliation.

Do not begin `agent-teams-bhe0.4` against the current `codex exec` worker
contract. Amend the contract and PR #160 first. Keep the existing runtime
metadata, thread binding, and adapter seam; replace the Codex per-turn worker
implementation with an app-server client and daemon manager.

## Why this changes the architecture

```text
durable Beads mail + doorbell
              |
       ateam delivery lock
              |
      app-server client
       /             \
 active thread      idle/notLoaded thread
 turn/steer         thread/resume + turn/start
       \             /
     Codex managed app-server daemon
              |
       persisted Codex thread
```

There is no long-lived `ateam` supervisor in this design. Every delivery
attempt first calls the idempotent managed-daemon start operation, connects to
the Unix socket, and reconciles the target thread while holding the initiative
delivery lock. If that short-lived client dies, the turn continues. If the
daemon dies, the next delivery attempt restarts it; the old active turn is
durably marked interrupted and unread mail starts a recovery turn.

## Reproduction

The documented protocol baseline is OpenAI's
[Codex app-server reference](https://developers.openai.com/codex/app-server/):
JSON-RPC initialization, thread start/resume/read/list, turn start/steer,
completion notifications, interruption, and runtime status. Everything labeled
as an observed result below came from the live harness rather than being
inferred from that documentation.

Run from an isolated worktree with no managed app-server daemon already active:

```bash
node scripts/spike-codex-app-server.mjs --run-live > /tmp/codex-app-server-spike.json
```

The harness:

- starts app-server on a private loopback WebSocket port;
- records JSON-RPC responses and complete thread/turn snapshots;
- separates the Node CLI launcher PID from the native app-server PID;
- kills client, launcher, and native-server layers independently;
- stops every process it creates; and
- refuses to disturb an already-running managed daemon.

The managed-daemon follow-up is independently reproducible with:

```bash
node scripts/spike-codex-managed-daemon.mjs --run-live \
  > /tmp/codex-managed-daemon-spike.json
```

It requires the standalone Codex layout, refuses to run when a daemon is
already active, and restores the daemon to its initial stopped state.

The final successful run used thread
`019fd84e-7418-7bf3-9f4a-2555292b0e8f`. Its durable rollout is local Codex
evidence, not a checked-in fixture; IDs and timings below identify that run.

## Observed results

| Case | Result | Consequence |
|---|---|---|
| Client disconnect during active turn | New client read the same thread as `active`; the turn completed | A mail sender or UI connection is not the turn owner |
| Active mail via `turn/steer` | Nonce appeared in the same turn and final response | This is the correct busy-thread wake primitive |
| Idle mail via `turn/start` | New turn completed under the same thread ID | This is the correct idle-thread wake primitive |
| Second `turn/start` while active | Accepted, but input was folded into the existing turn; the returned new turn ID was not persisted as a distinct turn | Never use `turn/start` as a concurrency probe or active-mail API; inspect state and use `turn/steer` |
| `turn/interrupt` | Active turn became durably `interrupted` | `ateam runtime stop` has a native implementation |
| Graceful native app-server `SIGTERM` | Server waited about 16 seconds for the active turn to complete, then exited zero | Normal shutdown drains work rather than orphaning it |
| CLI launcher `SIGKILL`, native app-server alive | Native PID survived; a new client reconnected, read the turn as active, steered mail, and observed completion | A dead launcher/supervisor does not strand a live Codex session |
| Native app-server `SIGKILL` | Restarted server read the old turn as `interrupted`; `thread/resume` returned idle; a new recovery turn completed | Daemon death is recoverable from durable thread + durable mail state |
| `thread/list` after restart | Same thread was discoverable with path, cwd, git branch, version, and idle status | Status/list tooling can be built without scanning process tables |
| Official managed daemon start | Fixed standalone binary detached with parent PID 1; a second start returned `alreadyRunning` | Codex, not `ateam`, can own the durable daemon process |
| Managed daemon graceful stop during a turn | Stop waited about 10.7 seconds for completion; restart read the completed turn | Supported stop drains active work |
| Managed daemon `SIGKILL` during a turn | Restart read the old turn as `interrupted`; resume returned idle; recovery turn completed | A short-lived delivery attempt can restart and reconcile daemon failure |

### Exact lifecycle evidence

- Client disconnected at `18:20:49Z`; another connection read the active turn at
  `18:20:50Z`; it completed at `18:21:01Z`.
- Active mail nonce `MAIL_WAKE_NONCE_7f3a9c` was accepted by `turn/steer` and
  appeared in the final answer.
- Idle wake nonce `IDLE_MAIL_WAKE_NONCE_51c2d8` completed in about 1.97 seconds.
- Killing launcher PID `3879` left native PID `3880` alive. The reconnecting
  client delivered `MAIL_AFTER_LAUNCHER_DEATH_83b0ca`; that turn completed.
- Killing native PID `3880` caused turn
  `019fd84f-a40a-7533-b2e0-30b130b0ec40` to reappear as `interrupted` after
  restart. Thread resume returned `idle`, and recovery turn
  `019fd84f-af81-7cf0-b190-561825457cdc` completed with the expected nonce.

## Important caveats

### App-server does not replace the ateam delivery lock

`turn/start` during an active turn did not run a second model turn in parallel,
which is safer than concurrent `codex exec resume`. However, it returned a new
in-progress turn ID while placing the input into the already-active turn. That
response shape is too surprising to use as a durable acknowledgment.

`ateam` must serialize delivery per initiative:

1. acquire the initiative delivery lock;
2. read the thread and identify the actual active turn ID;
3. use `turn/steer(expectedTurnId=...)` when active;
4. use `thread/resume` plus `turn/start` when idle or not loaded;
5. retain the doorbell until `ateam mail inbox` consumes durable unread mail;
6. reconcile again after completion, disconnect, or daemon restart.

### Runtime status is advisory, not the mail acknowledgment

During development, `thread/read` briefly returned a just-started turn as
`interrupted` with `completedAt: null` and no items, although the durable rollout
then recorded its normal completion. The corrected harness requires a terminal
status **and** non-null completion timestamp for live-turn polling.

Production delivery should primarily consume `turn/completed` notifications and
use persisted turn metadata for reconnect recovery. Beads message consumption,
not Codex thread status, remains the authoritative mail acknowledgment.

### Standalone Codex is the selected installation contract

The npm/mise Codex 0.145.0 installation could run transient app-server but was
rejected by `codex app-server daemon start`. After installing standalone Codex
0.146.1, the managed daemon started from
`~/.codex/packages/standalone/current/codex`, detached under parent PID 1, and
passed reconnect, graceful restart, hard-crash recovery, and thread discovery.

The first Codex slice therefore requires standalone Codex. Shared setup/audit
logic must distinguish three states: Codex absent, Codex present but without
the standalone managed binary, and a compatible standalone installation. Both
the Claude `/setup-agent-teams` flow and the future Codex setup skill must
surface the same result. General setup warns for an incompatible optional
Codex install; selecting `runtime: codex` fails with an actionable installer
message.

### Source classification was unexpected

The thread created through app-server reported `source: "vscode"` and
`threadSource: null` even though the request supplied `threadSource:
"appServer"`. Unfiltered `thread/list` found it. Initial `ateam` discovery must
therefore use the durable thread ID mapping and cwd, not rely exclusively on the
`appServer` source-kind filter.

## Contract changes for PR #160

Preserve:

- `runtime: codex` metadata and legacy Claude fallback;
- runtime-scoped session/thread binding;
- runtime adapter boundaries;
- fake-backed runtime selection and CLI tests.

Replace or amend:

- detached `runtime-worker` ownership of a full `codex exec` turn;
- per-turn PID as the primary Codex liveness signal;
- active mail waiting for process exit;
- the assumption that a new resume process is required for every wake.

Add:

- the standalone managed-daemon endpoint and idempotent start operation;
- a JSON-RPC client for initialize, thread read/list/resume, turn start/steer,
  turn completion, and interrupt;
- an initiative delivery lock around state inspection and wake selection;
- crash reconciliation that treats an interrupted old turn as retryable while
  preserving unread mail;
- regression fixtures for active `turn/start` folding and transient incomplete
  terminal snapshots.

## Remaining operational verification

The daemon deployment decision is settled: **GO with standalone Codex and its
managed app-server daemon**. Logout/reboot and CLI-upgrade behavior remains an
operational compatibility check, not a blocker for replacing the incorrect
per-turn `codex exec` contract. Delivery must always call the idempotent start
operation, so it does not rely on login-time bootstrap to wake mail.
