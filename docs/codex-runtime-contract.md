# Runtime-neutral session and delivery contract

- **Initiative:** `agent-teams-bhe0`
- **Contract bead:** `agent-teams-bhe0.2`
- **Status:** amended 2026-08-06 for the managed app-server architecture

This document is the shared contract for the runtime adapter, Codex delivery
coordinator, lifecycle hooks, and Codex role definitions. The
downstream implementation beads may refine private implementation details, but
must not silently change the behavior or public types frozen here.

The first slice supports the existing Claude runtime and one background Codex
DRI thread per initiative. It does not attempt a general multi-runtime
scheduler or persistent Codex role-agent threads.

## 0. Managed app-server amendment

The live lifecycle spikes invalidated the original assumption that Codex needs
one detached `codex exec` child plus a long-lived `ateam` supervisor for every
turn. This document now states the replacement contract directly.

The selected deployment contract is:

- Codex runtime support requires the standalone Codex installation and its
  fixed managed binary under `~/.codex/packages/standalone/current/codex`.
- The supported `codex app-server daemon` owns the long-lived machine daemon.
  `ateam` does not install a second user service and does not remain alive to
  supervise the daemon.
- Every launch, resume, wake, status, or stop operation first performs the
  appropriate managed-daemon health/lifecycle operation and then speaks the
  app-server protocol over its Unix socket.
- A short-lived per-initiative delivery coordinator serializes state
  inspection and one delivery decision. It uses `turn/steer` for the actual
  active turn, or `thread/resume` plus `turn/start` when idle/not loaded.
- `ateam mail send` invokes that coordinator. For Codex, it is the only active
  delivery and wake authority; there is no background Codex mail watcher.
- Codex owns turn lifetime. Client death does not stop an active turn. Daemon
  death makes that turn interrupted; a later coordinator restarts the daemon,
  resumes the same thread, and retries still-unread durable mail.
- Beads unread messages remain the delivery truth. An accepted JSON-RPC call,
  thread status, or completed model turn does not itself acknowledge mail.

This current contract supersedes the lifecycle spikes' former hook-delivery
conclusion. Those historical spike documents remain historical evidence and
are not rewritten by this amendment.

The managed daemon detached under parent PID 1, treated duplicate start as
idempotent, drained an active turn during supported stop, and recovered the
same thread after hard daemon death. See the
[app-server lifecycle spike](./2026-08-06-codex-app-server-spike.md).

Setup and runtime selection share one compatibility check:

1. no `codex` executable: report Codex unavailable;
2. `codex` exists but the standalone managed binary is absent or incompatible:
   warn during general setup and explain how to install standalone Codex;
3. a compatible standalone installation: report its CLI and app-server
   versions; and
4. an explicitly selected Codex runtime fails closed for states 1 and 2.

The existing Claude `/setup-agent-teams` flow and the future Codex setup skill
must invoke this shared check rather than maintaining separate shell probes.

## 1. Terms and sources of truth

- A **runtime** is the program that owns an agent session: `claude` or `codex`.
  It is not the initiative's launch `mode`.
- A **session** is an opaque runtime-scoped identifier. It is a Claude session
  ID for `claude` and a Codex thread ID for `codex`.
- A **Codex daemon** is the standalone CLI's managed app-server process. It
  owns machine-local Codex threads and turns across client connections.
- A **delivery coordinator** is a short-lived `ateam` operation that serializes
  thread inspection and one wake/delivery decision for an initiative. It does
  not own the daemon or the complete turn lifetime.
- A **doorbell** is an expendable wake signal. The unread Beads messages are
  the durable mail payload.

Beads is authoritative for initiative metadata, bound sessions, messages,
gates, and initiative state. Files under `AGENT_TEAMS_HOME` are
machine-local, reconstructible observations and coordination state. A local
state file must never override contradictory durable metadata.

The domain identity of a session is the tuple `{runtime, id}`. Code outside a
runtime adapter must not infer runtime identity from an environment variable,
ID shape, executable name, or state-file location.

## 2. Initiative metadata

### 2.1 Canonical fields

Initiative descriptions gain one single-valued field:

```text
runtime: claude|codex
```

The existing `mode:` field remains launch behavior such as `bg`; it must not
be reused for runtime selection. `runtime:` is immutable after initiative
creation. An initiative cannot contain sessions from more than one runtime.

New dispatches resolve a concrete runtime and always write it, including
`--no-launch` dispatches. Durable metadata never stores `auto`.

Resolution for a new dispatch is:

1. an explicit `--runtime claude|codex`.
2. `ATEAM_RUNTIME`, when set to `claude` or `codex`.
3. the selected key from `$AGENT_TEAMS_HOME/config.toml`.
4. `claude`.

`--runtime auto` follows steps 2 through 4. A valid higher-priority value
prevents lower tiers from selecting the runtime. An invalid explicit value or
invalid `ATEAM_RUNTIME` is an error. It must not silently launch Claude.

The machine-local config is strict, flat TOML with exactly three optional keys.
The two runtime-selection keys are:

```toml
work_runtime = "codex"
review_runtime = "claude"
```

Those values are the initial recommended split. An exact `--topic reviews`
dispatch selects `review_runtime`. Every other dispatch selects
`work_runtime`. Each present value must be exactly lowercase `claude` or
`codex`, and no table or unknown key is accepted. A missing file or missing
selected key falls through to step 4. Once this tier is consulted, an
unreadable present file, malformed TOML, unknown or non-flat key, empty value,
or other invalid value fails before any dispatch side effect. The error names
the config path and relevant key or parse context without printing unrelated
config contents. This selection does not add Codex PR-review execution.

The third key is an optional integer token count:

```toml
auto_compact_window = 300000
```

There is no default. `ateam setup codex` does not add the key. When it is
absent, agent-teams does not send `model_auto_compact_token_limit`, so native
Codex behavior is unchanged. When present, the value must be positive and fit
a signed 64-bit integer. Invalid config stops the attempted Codex launch or
resume before an app-server thread or turn request. Invalid config includes
malformed TOML, the wrong type, zero, a negative value, overflow, a table, or
an unknown key. The error gives the config path and the relevant key or parse
context.

For Codex requests, a non-empty
`CLAUDE_PLUGIN_OPTION_AUTO_COMPACT_WINDOW` is an explicit cross-runtime
compatibility input. It takes precedence over the workspace key. Existing
numeric forms resolve to an integer: plain tokens, `k` or `m` suffixes, and the
documented bare 100–1000 thousands shorthand. The explicit value `auto` also
takes precedence, but it emits no Codex limit. Any other non-empty value fails
the Codex request instead of falling through. This precedence does not change
existing Claude launch behavior.

The override applies only to agent-teams-managed Codex threads. Fresh dispatch,
explicit resume or cold reload, and mail wake use the same request construction.
The value is supplied on `thread/start` and `thread/resume`. Updating the
key does not retrofit a thread that the managed app server already loaded. For
that case, the contract only requires agent-teams to send the current override.
Codex child role agents inherit the root thread config natively. Thus, the five
role TOMLs do not copy this key.

Agent-teams never creates, edits, reads, or validates the user-owned Codex
`config.toml` for this feature. It does not affect ordinary Codex threads or set
`model_context_window`, `model_auto_compact_token_limit_scope`, or any native
internal scope key.

For backward compatibility, a missing `runtime:` on an existing initiative
resolves to `claude` without rewriting the initiative on read. A non-empty,
unknown runtime is an error for launch, resume, wake, tie, and stop. Status may
display it as unknown, but must not act through the Claude adapter.

Resume and wake always use the durable initiative runtime. A runtime flag on
resume is an assertion: it must match the stored or legacy-resolved runtime
and cannot migrate the initiative. Machine-local config likewise cannot
override or migrate an existing initiative. Session-first mailbox and hook
lookup continue to use the durable recorded runtime/session identity described
below.

### 2.2 Session fields

The existing repeated `session:` field remains the durable binding mechanism.
IDs remain opaque and unprefixed because `runtime:` supplies the namespace.

- Claude preserves its existing ordered-set behavior.
- For Codex, the last distinct non-empty `session:` is the initiative's active
  thread. Ordinary resume/wake must not append the same ID again.
- The first Codex launch appends the emitted thread ID as soon as it observes
  the runtime's thread-start event.
- Appending a different Codex thread is reserved for an explicit replacement
  or recovery operation outside the first loop-closing slice.

The cross-initiative uniqueness rule applies to `{runtime, id}`, not a bare
ID. A missing runtime on a legacy initiative is treated as `claude` for this
comparison.

`ateam tie-session --session-id <id>` remains the runtime-neutral explicit
boundary. Claude hooks may continue using `CLAUDE_CODE_SESSION_ID` as a
compatibility fallback. Codex code must pass the captured thread ID
explicitly; the domain must not invent a Codex environment-variable contract.

A running agent's own mailbox resolution (`ateam mail inbox`) may consume the
runtime-native session-id env var it is already given — `CLAUDE_CODE_SESSION_ID`
for Claude, `CODEX_THREAD_ID` for Codex — to resolve its initiative
independently of cwd, by matching it against the initiative's recorded
`session:` values. This is distinct from the two things this section still
bars: inventing an ateam-defined Codex env var, and passing the session tie
itself through the environment. Consuming a variable the runtime already
sets is not inventing one, and resolving a lookup is not writing a tie.

If a Codex thread cannot be durably tied to its initiative, the coordinator
interrupts its verified active turn when possible and reports an error. It must
not acknowledge mail or leave an active, unrouteable thread intentionally.

## 3. Package boundaries and frozen interfaces

Use `internal/sessionruntime` for runtime-neutral types and adapter selection.
Use `internal/worker` for machine-local delivery state and locking; the package
may be renamed during the amendment implementation. This avoids conflating the
adapter with Go's standard `runtime` package.

The implementation may add private fields, but downstream work may depend on
the following semantic interface:

```go
package sessionruntime

type Kind string

const (
    Claude Kind = "claude"
    Codex  Kind = "codex"
)

type SessionRef struct {
    Runtime Kind
    ID      string
}

type Request struct {
    InitiativeID string
    Worktree     string
    Prompt       string
    Model        string
    Events       io.Writer
    Stderr       io.Writer
}

type SessionSink func(SessionRef) error

type Adapter interface {
    Kind() Kind
    Launch(context.Context, Request, SessionSink) error
    Resume(context.Context, Request, SessionRef) error
}
```

Interface semantics:

- Codex `Launch` and `Resume` return after app-server accepts the turn. The
  managed daemon owns the complete turn lifetime; disconnecting the adapter
  client does not stop the turn.
- `SessionSink` may be called zero or one time. The Codex adapter must call it
  once after `thread/start` and before `turn/start`, so a binding failure does
  not leave an active unrouteable turn. Resume verifies both
  `thread/resume` and `thread/read` identify the requested thread.
- The adapter owns managed-daemon start, app-server initialization, protocol
  parsing, and event output. Verbs and skills must not assemble Codex daemon
  commands or JSON-RPC messages themselves.
- Resume inspects current thread state. It uses `turn/steer` with the actual
  in-progress turn id when active, and `turn/start` only when no in-progress
  turn exists. An active thread with no identifiable active turn fails closed.
- Status, stop, and monitoring are coordinator/app-server concerns layered over
  the adapter. Stopping a Codex turn does not archive or delete its durable
  thread.
- Tests inject daemon and app-server seams. Paid live Codex runs remain behind
  the explicit `ATEAM_LIVE_CODEX=1` guard and are never ordinary build gates.

The delivery-coordinator implementation may extend the internal app-server
client with list, interrupt, and completion observation. Public dispatch,
resume, and mail paths request coordination; they do not construct app-server
requests themselves. Codex lifecycle hooks do not request coordination.

## 4. Machine-local delivery state

Runtime state lives under the resolved `AGENT_TEAMS_HOME`:

```text
<home>/runtimes/<runtime>/<initiative-id>.json
<home>/runtimes/<runtime>/<initiative-id>.lock
```

The JSON schema starts at version 1:

```json
{
  "version": 1,
  "runtime": "codex",
  "initiative_id": "agent-teams-1234",
  "session_id": "opaque-thread-id",
  "phase": "delivering",
  "generation": "unique-delivery-token",
  "active_turn_id": "opaque-turn-id",
  "daemon_version": "0.146.1",
  "updated_at": "2026-08-03T12:00:00.000000000Z"
}
```

`session_id` and turn fields may be absent while starting. State writes are
atomic and private to the user. `generation` changes for each acquired
delivery attempt and prevents an old coordinator from overwriting a newer
generation. Daemon status comes from the managed-daemon operation and
app-server protocol, not a cached PID.

The initial phase vocabulary is:

- `starting`: coordinator is ensuring the daemon or binding a new thread;
- `delivering`: coordinator is inspecting or delivering to a thread;
- `active`: app-server reports an active turn, whether or not a coordinator is
  connected;
- `idle`: app-server reports the thread idle or not loaded;
- `interrupted`: the last active turn was interrupted and unread mail remains
  eligible for retry; and
- `unavailable`: the standalone daemon contract is unavailable or incompatible.

## 5. Per-initiative delivery lock

There may be at most one Codex delivery decision per initiative at a time.
The coordinator acquires an exclusive, non-blocking OS-backed lock before it
reads app-server thread state and holds it through the selected `turn/steer` or
`turn/start` request and immediate durable-state reconciliation.

The lock does **not** represent ownership of the model turn and does not need
to survive for the complete turn lifetime. App-server prevents simultaneous
turn execution. The lock prevents racing senders from both reading stale state
and making contradictory delivery choices.

Lock-file existence and timestamps are never ownership evidence. An abandoned
lock file is harmless. Multiple wake callers may race; exactly one acquires
the lock. A loser exits successfully with delivery still pending and leaves
the doorbell intact. Timestamp-based stale-lock stealing is not acceptable.

### 5.1 Reconnect and stale recovery

After acquiring the lock, the coordinator:

- ensures the managed daemon is running using its idempotent start operation;
- reconciles durable initiative/session metadata before cached observations;
- resumes the durable thread when app-server reports it not loaded;
- treats a prior `interrupted` turn as retryable only while durable unread mail
  remains; and
- never clears the doorbell or acknowledges mail from a cached turn status.

A coordinator crash releases the lock without affecting the active Codex
turn. A later coordinator reconnects and repeats reconciliation. Daemon death
is detected through the managed lifecycle/socket, not through cached PIDs.

## 6. Mail wake state machine

The durable-message-first behavior remains unchanged:

1. `ateam mail send` creates the message bead.
2. It creates or repairs the initiative doorbell.
3. It requests a runtime-neutral wake for the target initiative.

For Claude, wake delegates to the existing watcher/respawn behavior. For
Codex, that wake invokes the app-server delivery coordinator. This coordinator
is the only Codex active-delivery and wake authority. A busy lock is success:
durable mail and the doorbell remain pending for the winning or next
coordinator.

The Codex coordinator performs:

```text
acquire lock
reconcile durable initiative + local state
idempotently ensure the managed daemon is running
start a new thread, or resume/read the bound thread
if an actual turn is active: turn/steer(expectedTurnId)
otherwise: turn/start
record the delivery observation, then release the lock
```

Later senders and the coordinator's durable-state reconciliation cover race
windows:

- Mail arriving during an active turn is delivered with `turn/steer`.
- Mail arriving after a turn becomes idle starts a new turn.
- Client death leaves the doorbell intact; a new delivery attempt reads the
  current state and makes the same choice safely.
- Daemon death leaves the old turn interrupted; restart and unread-mail replay
  start a recovery turn on the same thread.

The doorbell is never cleared merely because a client or turn started, stopped,
or completed. It is cleared or reconciled only after `ateam mail inbox` successfully
consumes all mail that was unread for that initiative. If unread messages
remain, a missing doorbell is repaired.

Codex inbox reconciliation acquires the same initiative delivery lock, removes
the old wake edge, then takes a fresh unread-mail snapshot. It re-arms the
doorbell if unread mail remains or the snapshot fails. This remove-before-query
ordering ensures a concurrent sender that touches a new wake edge after the
snapshot cannot have that edge erased by the drain.

Consequences:

- mail during daemon or client transitions stays pending;
- two rapid messages may share one doorbell and one turn, but both durable
  messages are read;
- a failed resume leaves mail retriable;
- a client or daemon crash does not acknowledge mail; and
- relayed and Telegram-originated messages use the same send/wake path.

Sender-side doorbell creation and inbox-side reconciliation remain owned by the
existing messaging domain. Codex lifecycle hooks never wake, steer, start,
block, acknowledge, create, clear, or repair a doorbell; the hook adapter does
not access `$AGENT_TEAMS_HOME/mailbox/*.wake`.

An initiative that is closed, abandoned, or at an unresolved human gate is
not blindly resumed. The doorbell and unread mail remain visible; status
explains why the wake was suppressed.

## 7. Public command behavior

The first slice exposes:

```text
ateam dispatch --runtime claude|codex|auto ...
ateam resume <initiative-id> [--runtime claude|codex]
ateam runtime status <initiative-id>
ateam runtime stop <initiative-id>
```

- Dispatch records the concrete runtime before requesting a launch.
- Codex dispatch/resume returns after the app-server request is accepted and a
  new thread ID is durably tied when applicable; it does not wait for the
  complete model turn.
- Status combines durable initiative/session data with managed-daemon and
  app-server observations. It distinguishes `starting`, `delivering`,
  `active`, `idle`, `interrupted`, and `unavailable`.
- Stop is idempotent and runtime-neutral. Claude preserves its current stop
  behavior; Codex uses `turn/interrupt` for the initiative's verified active
  turn. Stopping the shared machine daemon is an administrative operation, not
  ordinary initiative stop.
- Monitoring instructions come from `Adapter.Controls`, so dispatch and resume
  output contains no hard-coded Claude commands for a Codex initiative.

An internal delivery entry point may be added for senders. It is not a
skill-facing API and may change as long as serialization and durable-mail
semantics above remain intact. Codex lifecycle hooks use only their separate
session-binding and cold-catch-up surface.

## 8. Lifecycle and role startup boundaries

Codex custom role agents did not receive the probed SubagentStart/SubagentStop
hooks. Therefore neither memory freshness nor correctness may depend on those
hooks.

Each Codex role definition must, in its own startup instructions:

1. run `ateam pull` before reading role memory;
2. run `ateam learnings <role>` (or a future combined freshness command);
3. reconstruct current assignment from the initiative epic and work beads;
   and
4. use durable Beads/mail for handoff rather than assuming a persistent child
   thread.

The Codex plugin retains exactly one mail/lifecycle adapter invocation: a
`SessionStart` hook with matcher `startup|resume|clear|compact`.
`ensure-ateam-link.sh` remains in that `SessionStart` hook group. The manifest
contains no `UserPromptSubmit` or `Stop` entry, and
`user-prompt-submit` and `stop` are not supported `ateam codex-hook` event
arguments.

For every retained `SessionStart` invocation, the adapter resolves the Codex
initiative from the session first, with cwd fallback, and attempts the existing
idempotent `session:` binding. The adapter has no doorbell-repair dependency
and does not deliver or wake mail. Only payload
`source=startup` or `source=resume` queries unread mail and may return
`SessionStart` `additionalContext` instructing `ateam mail inbox`. For
`clear`, `compact`, a missing source, or an unknown source, the adapter still
binds but neither queries unread mail nor injects mail context.

## 9. Ownership map

| Concern | Owner |
|---|---|
| Parse/preserve `runtime:` and `session:` | `internal/initiative` |
| Runtime kinds, session refs, adapters, controls | `internal/sessionruntime` |
| Delivery lock, local observations, generation | `internal/worker` (rename optional) |
| Durable messages and doorbell reconciliation | existing messaging domain |
| Dispatch/resume/status/stop orchestration | CLI verbs using adapter registry and coordinator |
| Managed lifecycle, app-server protocol, event parsing | Codex adapter/client |
| Active Codex mail delivery and wake | app-server delivery coordinator invoked by `ateam mail send` |
| Sender doorbell creation; inbox doorbell reconciliation | existing messaging domain |
| SessionStart session binding and source-scoped cold catch-up | Codex lifecycle adapter |
| Role memory freshness and assignment reconstruction | Codex role definitions themselves |

Skills invoke public `ateam` commands. They do not read runtime JSON, inspect
lock files, speak app-server, or construct daemon commands.

## 10. Required conformance tests

The downstream beads must collectively cover these tests with fake runtimes
except where an explicit manual spike is noted:

1. Legacy initiative without `runtime:` resolves to Claude.
2. New Claude and Codex dispatches persist a concrete runtime; `auto` is never
   persisted.
3. Unknown runtime fails closed and does not invoke Claude.
4. Codex runtime selection fails with actionable output when Codex is absent or
   installed without the compatible standalone managed binary.
5. Managed-daemon start is idempotent; a client can reconnect to a turn it did
   not create.
6. A Codex thread start appends exactly one session ID; bind failure interrupts
   the verified turn and leaves mail retriable.
7. Resume uses the last active Codex session and rejects a response for a
   different thread.
8. Concurrent delivery attempts make one state-based delivery choice; losing
   attempts leave the doorbell intact.
9. Mail while idle resumes the same thread and uses `turn/start`.
10. Mail while busy targets the actual active turn with `turn/steer`.
11. A second `turn/start` is never used as a busy-thread delivery primitive.
12. Two quick messages cause serialized delivery and both are consumed.
13. Client death does not stop the active turn; a new client reconnects.
14. Daemon death leaves the old turn interrupted; idempotent restart, thread
    resume, and unread-mail replay recover on the same thread.
15. Failed start/resume/steer retains unread mail and a repairable doorbell.
16. Stop interrupts only the initiative's verified active turn and does not
    stop the shared daemon.
17. Claude dispatch, resume, watcher wake, status, and monitoring output retain
    their existing behavior.
18. Codex role startup succeeds without SubagentStart by pulling and fetching
    its own learnings.
19. In a fresh session with the integrated plugin installed, a uniquely tagged
    `ateam mail send` to the bound Codex initiative produces exactly one
    app-server unread-mail user prompt and a real `ateam mail inbox`
    consumption/read marker.
20. That direct-delivery proof records no duplicate hook-generated developer
    reminder and no hook-created follow-up turn when the turn stops.
21. A SessionStart `startup` or `resume` invocation binds and can provide the
    unread-mail `additionalContext`; `clear`, `compact`, missing, and unknown
    sources bind without an unread query or injected mail context.
22. Manifest and unit tests prove that the retained `SessionStart` surface has
    no `UserPromptSubmit` or `Stop` registration, that the adapter has no
    doorbell-repair dependency, and that it does not access
    `$AGENT_TEAMS_HOME/mailbox/*.wake`.

## 11. Explicit non-goals for this slice

- remote app-server transport or multi-machine lock coordination;
- simultaneous Codex turns for one initiative;
- runtime migration of an existing initiative;
- durable persistent identity for planner/implementer/tester/reviewer child
  agents;
- replacing Beads mail with process IPC; and
- making local delivery JSON a second registry.

Any implementation need that contradicts this contract should update this
document and the contract bead before dependent work proceeds.
