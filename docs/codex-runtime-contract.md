# Runtime-neutral session and worker contract

- **Initiative:** `agent-teams-bhe0`
- **Contract bead:** `agent-teams-bhe0.2`
- **Status:** frozen for the first Codex loop-closing slice

This document is the shared contract for the runtime adapter, Codex worker,
mail wake supervisor, lifecycle hooks, and Codex role definitions. The
downstream implementation beads may refine private implementation details, but
must not silently change the behavior or public types frozen here.

The first slice supports the existing Claude runtime and one background Codex
DRI thread per initiative. It does not attempt a general multi-runtime
scheduler or persistent Codex role-agent threads.

## 1. Terms and sources of truth

- A **runtime** is the program that owns an agent session: `claude` or `codex`.
  It is not the initiative's launch `mode`.
- A **session** is an opaque runtime-scoped identifier. It is a Claude session
  ID for `claude` and a Codex thread ID for `codex`.
- A **worker** is the machine-local process currently executing a turn for an
  initiative.
- A **supervisor** owns worker serialization, process lifecycle, and the
  post-exit mail check.
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

1. an explicit `--runtime claude|codex`;
2. `ATEAM_RUNTIME`, when set to `claude` or `codex`;
3. `claude`.

`--runtime auto` follows steps 2 and 3. An invalid explicitly selected value
or invalid `ATEAM_RUNTIME` is an error; it must not silently launch Claude.

For backward compatibility, a missing `runtime:` on an existing initiative
resolves to `claude` without rewriting the initiative on read. A non-empty,
unknown runtime is an error for launch, resume, wake, tie, and stop. Status may
display it as unknown, but must not act through the Claude adapter.

Resume and wake always use the durable initiative runtime. A runtime flag on
resume is an assertion: it must match the stored or legacy-resolved runtime
and cannot migrate the initiative.

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

If a Codex thread-start event cannot be durably tied to its initiative, the
supervisor stops that worker and reports an error. It must not leave an active,
unrouteable thread.

## 3. Package boundaries and frozen interfaces

Use `internal/sessionruntime` for runtime-neutral types and adapter selection.
Use `internal/worker` for machine-local state, locking, and supervision. This
avoids conflating the adapter with Go's standard `runtime` package.

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

- `Launch` and `Resume` block for the complete runtime-process lifetime. A
  detached `ateam runtime-worker` owns that blocking call, so public dispatch
  remains asynchronous without orphaning the JSONL reader. The mail
  supervisor extends this same process boundary with locking and post-exit
  reconciliation.
- `SessionSink` may be called zero or one time. The Codex adapter must call it
  once for a new thread and must verify the resumed event identifies the
  requested thread. The legacy Claude path may continue binding through its
  session hook.
- An adapter owns command construction, runtime event parsing, and
  event output. Verbs and skills must not assemble `claude` or `codex` command
  lines themselves.
- Status, stop, and monitoring are worker/supervisor concerns layered over the
  adapter. Stopping a Codex worker does not archive or delete its durable
  thread.
- Tests inject an adapter registry and fake executables. Paid live Codex runs
  are feasibility probes, not unit or integration test dependencies.

The supervisor is the only component allowed to call `Launch` or `Resume` for
Codex. Public dispatch, resume, mail, and hook paths request supervision; they
do not directly start a Codex turn.

## 4. Machine-local worker state

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
  "phase": "starting",
  "generation": "unique-launch-token",
  "supervisor_pid": 1001,
  "worker_pid": 1002,
  "worker_started_at": "2026-08-03T12:00:00.000000000Z",
  "updated_at": "2026-08-03T12:00:00.000000000Z"
}
```

`session_id` and worker fields may be absent while starting. State writes are
atomic and private to the user. `generation` changes for each acquired worker
lifetime and prevents an old supervisor from overwriting a newer generation.
The state-file PID is diagnostic, not proof of lock ownership or sufficient
authority to signal a process.

The initial phase vocabulary is:

- `starting`: lock held, runtime worker not yet durably bound;
- `running`: lock held and worker executing;
- `stopping`: a verified stop was requested and exit is pending;
- `idle`: no worker owns the initiative locally;
- `stale`: reported status when active-looking state exists without its
  corresponding ownership proof.

## 5. Per-initiative worker lock

There may be at most one active Codex worker per initiative on a machine. The
supervisor acquires an exclusive, non-blocking per-initiative lock before
starting either a new thread or a resumed turn and holds ownership through:

1. process creation;
2. the complete runtime process lifetime;
3. the post-exit unread-mail and doorbell reconciliation; and
4. any immediately chained turn required by that reconciliation.

Lock-file existence and timestamps are never ownership evidence. An abandoned
lock file is harmless. The implementation must use an OS-backed lease or an
equivalent primitive with these observable properties:

- a second supervisor cannot acquire it while the worker is alive;
- killing only the supervisor cannot permit a parallel worker while its child
  is still alive;
- supervisor and worker death release ownership without an age timeout; and
- a new supervisor can recover without manually deleting a file.

An inherited advisory-lock file descriptor is an acceptable implementation if
the crash test proves the child retains it. Any alternative must pass the same
test. Timestamp-only stale-lock stealing is not acceptable.

Multiple wake callers may race to start supervisors. Exactly one supervisor
acquires the lock; losers exit successfully as `already running` and leave the
doorbell intact. External callers must not acquire a lock and then attempt to
transfer it to a detached supervisor.

### 5.1 Stale recovery

After acquiring ownership and before launching a worker, the supervisor
reconciles local state with durable initiative metadata:

- stale `starting`, `running`, or `stopping` state from an older generation is
  replaced by the new generation;
- the durable initiative runtime and active session win over cached values;
- a missing Codex session prevents resume but does not discard unread mail or
  clear the doorbell; and
- stale state never authorizes killing a PID.

Stop verifies the recorded PID's process-start identity as well as the current
generation before signaling it. PID alone is insufficient because of PID
reuse. If ownership is absent, stop reconciles stale state to idle and is an
idempotent no-op. If ownership is present, the owner performs final cleanup;
the stop caller does not unlink the lock or declare the worker idle early.

## 6. Mail wake state machine

The durable-message-first behavior remains unchanged:

1. `ateam mail send` creates the message bead.
2. It creates or repairs the initiative doorbell.
3. It requests a runtime-neutral wake for the target initiative.

For Claude, wake delegates to the existing watcher/respawn behavior. For
Codex, wake starts a detached supervisor attempt. A busy lock is success: the
current worker or its post-exit reconciliation owns delivery.

The Codex supervisor runs this loop while retaining one lock generation:

```text
acquire lock
reconcile durable initiative + local state
launch new thread or resume active thread
wait for the complete Codex process to exit
reconcile unread messages + doorbell while still holding the lock
if delivery is pending and the initiative is runnable:
    resume the same thread and repeat
otherwise:
    record idle, then release the lock
```

The lifecycle hook and supervisor cover complementary race windows:

- If mail is pending when an active Codex turn reaches `Stop`, the Stop hook
  requests exactly one continuation instructing the thread to run
  `ateam mail inbox`. `stop_hook_active` prevents a continuation loop.
- If mail arrives after the Stop hook's last check but before process exit,
  the supervisor sees it during post-exit reconciliation and resumes the same
  thread before releasing ownership.
- If mail arrives after ownership is released, the sender's wake supervisor
  acquires the idle lock and resumes the same thread.

The doorbell is never cleared merely because a worker started, stopped, or
exited. It is cleared or reconciled only after `ateam mail inbox` successfully
consumes all mail that was unread for that initiative. If unread messages
remain, a missing doorbell is repaired.

Consequences:

- mail during `starting`, `running`, or `stopping` stays pending;
- two rapid messages may share one doorbell and one turn, but both durable
  messages are read;
- a failed resume leaves mail retriable;
- a worker crash releases ownership but does not acknowledge mail; and
- relayed and Telegram-originated messages use the same send/wake path.

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
- Codex dispatch/resume returns after a detached supervisor has accepted the
  request; the thread ID may appear asynchronously once the start event is
  durably tied.
- Status combines durable initiative/session data with adapter and local-lock
  observations. It distinguishes `idle`, `starting`, `running`, `stopping`,
  `stale`, and `unavailable` rather than treating a PID file as truth.
- Stop is idempotent and runtime-neutral. Claude preserves its current stop
  behavior; Codex terminates only the verified local worker/process group.
- Monitoring instructions come from `Adapter.Controls`, so dispatch and resume
  output contains no hard-coded Claude commands for a Codex initiative.

An internal supervisor entry point may be added to support detached workers.
It is not a skill-facing API and may change as long as the ownership and wake
semantics above remain intact.

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

Hooks remain optional optimization and lifecycle glue. The mail Stop hook must
be thin: inspect delivery state, request at most one continuation, and leave
durable consumption to `ateam mail inbox`.

## 9. Ownership map

| Concern | Owner |
|---|---|
| Parse/preserve `runtime:` and `session:` | `internal/initiative` |
| Runtime kinds, session refs, adapters, controls | `internal/sessionruntime` |
| Lock, local JSON, generation, process ownership | `internal/worker` |
| Durable messages and doorbell reconciliation | existing messaging domain |
| Dispatch/resume/status/stop orchestration | CLI verbs using adapter registry and supervisor |
| Runtime event parsing and command construction | Claude/Codex adapters |
| Stop continuation decision | thin Codex lifecycle hook |
| Role memory freshness and assignment reconstruction | Codex role definitions themselves |

Skills invoke public `ateam` commands. They do not read runtime JSON, inspect
lock files, parse Codex JSONL, or construct resume commands.

## 10. Required conformance tests

The downstream beads must collectively cover these tests with fake runtimes
except where an explicit manual spike is noted:

1. Legacy initiative without `runtime:` resolves to Claude.
2. New Claude and Codex dispatches persist a concrete runtime; `auto` is never
   persisted.
3. Unknown runtime fails closed and does not invoke Claude.
4. A Codex thread-start event appends exactly one session ID; bind failure
   stops the worker.
5. Resume uses the last active Codex session and rejects an event for a
   different thread.
6. Concurrent wake attempts produce one worker.
7. The lock remains owned for the full worker lifetime and post-exit check.
8. Killing the supervisor while its child lives cannot create a second worker
   (manual/OS integration test if necessary).
9. Killing worker and supervisor permits recovery without age-based stealing.
10. Mail while idle resumes the same thread.
11. Mail while busy is consumed before dormancy.
12. Mail between Stop-hook check and process exit triggers a chained resume.
13. Two quick messages cause one serialized wake and both are consumed.
14. Failed resume/crash retains unread mail and a repairable doorbell.
15. Stop verifies process identity and is idempotent on stale state.
16. Claude dispatch, resume, watcher wake, status, and monitoring output retain
    their existing behavior.
17. Codex role startup succeeds without SubagentStart by pulling and fetching
    its own learnings.

## 11. Explicit non-goals for this slice

- app-server transport or remote multi-machine lock coordination;
- simultaneous Codex turns for one initiative;
- runtime migration of an existing initiative;
- durable persistent identity for planner/implementer/tester/reviewer child
  agents;
- replacing Beads mail with process IPC; and
- making local worker JSON a second registry.

Any implementation need that contradicts this contract should update this
document and the contract bead before dependent work proceeds.
