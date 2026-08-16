# Codex runtime plan

**Initiative:** `agent-teams-bhe0`

[Open the styled HTML version](./2026-07-27-codex-runtime-plan.html)

## Objective

Bring a deliberately scoped part of agent-teams to Codex, beginning with a
Codex-native DRI workflow, custom role subagents, background dispatch, and the
mail-driven session wake-up loop.

The first loop-closing milestone is one Codex DRI that can:

1. dispatch into an initiative worktree;
2. spawn role-specific planner, implementer, tester, and reviewer subagents;
3. stop cleanly at a human or mail gate;
4. receive durable Beads-backed mail while idle;
5. wake in the same Codex thread; and
6. finish a small initiative.

This plan uses **loop-closing** in the existing agent-teams sense: the smallest
end-to-end set that proves the new mechanism works. It is the project's
concentric planning methodology, not a synonym for completing the whole Codex
port.

## Architectural direction

Do not implement this as a literal port of the Claude plugin. Keep shared
agent-teams semantics separate from runtime-specific session control:

```text
Shared agent-teams domain
  initiatives, beads, mail, gates, memories, role contracts
                         |
             runtime-neutral ateam API
                launch / wake / status
                  tie-session / stop
                  |                 |
          Claude adapter       Codex adapter
          claude --bg          managed app-server
          claude respawn       turn/start | turn/steer
          asyncRewake hook     delivery coordinator
```

Introduce only the runtime seams required by dispatch, session identity, mail,
and status. Preserve the existing Claude behavior and default until the Codex
path is proven.

The frozen interface and state-machine decisions for Phases 1–3 are in the
[runtime-neutral session and worker contract](./codex-runtime-contract.md).

## Phase 0 — Feasibility gates

### 0.1 Durable Codex thread continuity

- Launch `codex exec --json` in an isolated repository.
- Capture the emitted thread/session ID.
- Let the process finish.
- Run `codex exec resume <id> <prompt>`.
- Verify the same conversation, cwd, instructions, and skill context survive.

### 0.2 External wake-up

- Resume an idle thread from another process.
- Determine what happens if a client still has the thread open.
- Determine what happens when a turn is already running.
- Establish whether concurrent resume is rejected, queued, or unsafe.

### 0.3 Custom role agents

- Define temporary project agents under `.codex/agents/`.
- Prove that Codex discovers and spawns them by their custom type.
- Verify `SubagentStart` receives that type.
- Determine whether their threads remain addressable across parent turns.
- If persistence is insufficient, prove that a role can be reconstructed from
  the initiative epic and work beads.

### 0.4 Lifecycle hooks

- Prove `SessionStart` can tie a Codex thread ID to an initiative.
- Prove `UserPromptSubmit` can inject an unread-mail instruction.
- Prove `Stop` can detect pending mail and create exactly one continuation
  turn.
- Verify `stop_hook_active` can prevent accidental continuation loops.

**Gate result (2026-08-06): GO with managed app-server.** An idle thread was
externally prompted under the same thread ID; an active thread accepted mail
through `turn/steer`; client disconnect, graceful daemon restart, and hard
daemon failure were recoverable. The first slice requires standalone Codex
and uses its managed daemon rather than one `codex exec resume` process per
turn. See the [lifecycle spike](./2026-08-06-codex-app-server-spike.md).

## Phase 1 — Runtime-aware `ateam`

Add an explicit runtime without changing the Claude default:

```text
ateam dispatch --runtime claude|codex|auto
ateam resume <id> --runtime ...
ateam runtime status <id>
ateam runtime stop <id>
```

Work:

- Add `runtime: claude|codex` to initiative metadata.
- Treat a missing runtime on existing initiatives as Claude.
- Add a machine default, overridden by an explicit flag.
- Extract a narrow runtime interface for launch, resume/wake, status, stop, and
  monitoring instructions.
- Retain the current Claude implementation behind the Claude adapter.
- Add a Codex app-server client that idempotently starts the standalone managed
  daemon, starts or resumes a thread, and records its thread ID through a
  runtime-neutral session tie.
- Accept Claude and Codex session IDs at the adapter boundary rather than
  treating `CLAUDE_CODE_SESSION_ID` as the domain identity.
- Test with fake `claude` and `codex` executables. Never use the paid eval
  runner as a build or verification test.

## Phase 2 — Codex mail and wake loop

Mail remains Beads-backed. Only the waking mechanism is runtime-specific.

### Delivery state machine

1. `ateam mail send` creates the message bead.
2. It creates or reconciles the initiative doorbell.
3. A short-lived delivery coordinator calls the idempotent managed-daemon
   start operation and acquires the initiative delivery lock.
4. It reads the target thread and uses `turn/steer` when active.
5. If the thread is idle or not loaded, it resumes the thread and uses
   `turn/start`.
6. It reconciles unread mail and the doorbell after completion or reconnect;
   the Codex daemon, not the coordinator, owns the model turn lifetime.

Machine-local, reconstructible runtime state may live at:

```text
~/.agent-teams/runtimes/codex/<initiative-id>.json
```

It may contain the Codex thread ID, active turn ID, last delivery transition,
and runtime version. Initiative and message truth remain in Beads.

### Invariants

- Message beads are the durable payload.
- A doorbell is only a wake signal.
- Do not clear the Codex doorbell merely because a turn started.
- Clear or reconcile it only after `ateam mail inbox` successfully consumes
  all currently unread mail.
- Repair a missing doorbell whenever unread message beads exist.
- Treat a stale doorbell as harmless and self-reconciling.
- Hold a per-initiative lock while inspecting state and selecting/delivering a
  wake operation; do not hold it as a proxy for the daemon's turn lifetime.
- Deliver mail that arrives while the worker is active, stopping, starting,
  or crashing.
- Route relay- and Telegram-originated messages through the same path.

### Required scenarios

- Mail while idle wakes the same Codex thread.
- Mail while busy is consumed before dormancy.
- Two rapid messages cause one wake and both are read.
- A client or daemon crash leaves unread mail retriable.
- Duplicate wake attempts never create parallel turns for one initiative.

## Phase 3 — Codex role definitions

Create source-controlled custom agent definitions for:

- `agent-teams-planner`
- `agent-teams-implementer`
- `agent-teams-tester`
- `agent-teams-reviewer`

Adapt, rather than mechanically copy, the current Claude definitions:

- preserve role ownership, Beads hierarchy, worktree discipline, integration
  ownership, peer communication, and memory routing;
- remove Claude-only frontmatter, model aliases, and advisor instructions;
- use Codex model and reasoning settings;
- use project-scoped `.codex/agents/` definitions while developing; and
- add an explicit `ateam setup codex` installation step for stable
  cross-project definitions under `~/.codex/agents/`.

Codex plugins currently package skills, hooks, MCP servers, and assets. Custom
agents are configured separately under `.codex/agents/` or
`~/.codex/agents/`, so do not depend on an undocumented plugin-agent packaging
mechanism.

## Phase 4 — Codex-native `dri` skill

Create a separate Codex plugin while the runtime is being proven:

```text
plugins/agent-teams-codex/
  .codex-plugin/plugin.json
  skills/
    dri/
      SKILL.md
      references/
  hooks/
    hooks.json
    scripts/
```

Preserve the existing DRI phases:

1. preflight;
2. register or resume;
3. clarify;
4. plan;
5. execute;
6. deliver; and
7. wind down.

Codex adaptations:

- invoke explicitly as `$dri ...`, while supporting implicit skill selection;
- use Codex custom subagents;
- replace persistent Claude process assumptions with a durable Codex thread
  plus resumable workers;
- let human gates end the current worker turn cleanly;
- use mail to wake the same thread after a gate;
- keep `SKILL.md` concise and route detailed mechanics to references;
- defer advisor parity; and
- keep Beads sufficient to reconstruct role agents after a gate.

## Phase 5 — Codex-native `dispatch-dri` skill

Port dispatch only after the DRI and mail loop work.

The skill remains mechanical:

1. capture the human's framing;
2. resolve the target repository;
3. call `ateam dispatch --runtime codex`;
4. register the initiative and root epic;
5. launch `$dri <initiative-id>`; and
6. return runtime-neutral monitoring instructions.

All Codex process construction belongs in the `ateam` runtime adapter, not the
skill.

## Phase 6 — Packaging and installation

Add:

- a `.codex-plugin/plugin.json` manifest;
- a repository marketplace entry for local testing;
- `ateam setup codex`;
- custom-agent installation and drift checking;
- hook trust and setup diagnostics;
- `ateam audit --runtime codex`; and
- a minimum supported Codex CLI version check.

Keep the Claude and Codex release paths independent until the Codex runtime is
stable.

## Phase 7 — Hardening

Validate:

- new dispatch and completed-process resume;
- mail while idle, busy, gated, compacting, and crashing;
- duplicate DRI sessions;
- stale PID and runtime records;
- missing, archived, or deleted Codex threads;
- hook trust disabled or hooks globally disabled;
- machine sleep and restart;
- initiative close during a pending wake;
- role reconstruction after a human gate;
- Codex JSON event compatibility across supported versions; and
- concurrent Claude and Codex initiatives.

## Initial non-goals

- Replacing Claude support.
- Full Steward, PR-review, eval-judge, cost-attribution, or orphan-reaper
  parity.
- Depending on experimental app-server APIs before the CLI resume route is
  proven inadequate.
- Sharing every line between the Claude and Codex skills.
- Automatically migrating existing Claude initiatives.
- Expanding cross-machine wake behavior beyond the current Dolt/mail sync
  model.

## Planned Beads decomposition

Every work bead belongs under `agent-teams-bhe0`:

1. `agent-teams-bhe0.1` — document the roadmap and complete the Phase 0
   feasibility spikes;
2. `agent-teams-bhe0.2` — define the runtime-neutral session and worker-lock
   contract;
3. `agent-teams-bhe0.3` — implement the Codex dispatch and resume adapter;
4. `agent-teams-bhe0.4` — implement the Codex mail delivery coordinator;
5. `agent-teams-bhe0.5` — implement Codex lifecycle hooks;
6. `agent-teams-bhe0.6` — define and install Codex custom role agents;
7. `agent-teams-bhe0.7` — implement the Codex `dri` vertical slice;
8. implement Codex `dispatch-dri`;
9. add setup, plugin packaging, and audit;
10. run live end-to-end validation; and
11. open hardening rings only after loop closure.

Items 1–7 are the initial loop-closing set.

## Feasibility evidence

Spikes ran on 2026-07-27 with `codex-cli 0.145.0`. They used an isolated
temporary Git repository except where an already trusted project was required
to distinguish project trust from hook behavior. Temporary hook and custom
agent files in this repository were removed after each probe. No agent-teams
eval command ran.

### Durable thread continuity — PASS

An initial process:

```text
codex exec --json "<remember nonce>"
```

emitted thread ID:

```text
019fa507-a3f4-7203-a3bf-224df80f1a30
```

and returned `STORED cobalt-otter-731 phase-zero`. After that process exited,
a new process ran:

```text
codex exec resume --json 019fa507-a3f4-7203-a3bf-224df80f1a30 "<recall>"
```

It emitted the same thread ID and returned
`RECALLED cobalt-otter-731 phase-zero`. This establishes the minimum durable
thread primitive needed for an idle mail wake.

### Lifecycle context and Stop continuation — PASS

In the trusted agent-teams checkout, temporary project hooks proved:

- `SessionStart` received `session_id`, `transcript_path`, `cwd`, `model`,
  `permission_mode`, and `source`.
- `UserPromptSubmit` received the same session identity plus `turn_id` and the
  prompt.
- Both hooks injected model-visible developer context.
- The first `Stop` invocation received `stop_hook_active: false`, returned a
  blocking continuation decision, and caused Codex to run one automatic
  continuation.
- The second `Stop` invocation received `stop_hook_active: true`.

The visible result was:

```text
STOP_CONTINUED mail-wake-417
```

This is sufficient for the in-process half of mail delivery: when a doorbell
exists as an active worker reaches Stop, the hook can request another turn
instead of allowing the worker to become dormant.

Project-local hooks in an otherwise untrusted temporary repository did not
load, even when the command used `--dangerously-bypass-hook-trust`. Setup and
audit therefore need to treat **project trust** and **individual hook trust**
as separate prerequisites.

Codex 0.145.0 does not support asynchronous command hooks. The Claude
`asyncRewake` watcher cannot be reused for the idle-worker half.

### Custom agent definition — PASS WITH CAVEAT

A temporary `.codex/agents/agent-teams-spike-scout.toml` was discovered and
spawned as agent type `agent-teams-spike-scout`. The child applied:

- its `developer_instructions` token;
- `model = "gpt-5.6-terra"`;
- `model_reasoning_effort = "low"`; and
- its configured sandbox layer, subject to parent runtime policy.

A custom agent type cannot be combined with a full-history fork. The first
spawn attempt failed with:

```text
Full-history forked agents inherit the parent agent type; omit agent_type, or
spawn without a full-history fork.
```

Retrying with `fork_turns = "none"` succeeded. The DRI role-spawn contract must
therefore provide a self-contained prompt and use `none` or a bounded positive
turn count when selecting a custom role.

#### What failed in `SubagentStart`

The custom agent and the hook are separate mechanisms. Agent discovery worked:
Codex selected `agent-teams-spike-scout`, started the child, and the child
demonstrated its configured instructions, model, reasoning effort, and sandbox.
The failure was narrower: the temporary `SubagentStart` command hook produced
no observed invocation when that child started, so it could not inject the
role's learnings.

`agent-teams-bhe0.9` reran and expanded the matrix on 2026-07-28. The installed
version remained Codex 0.145.0. Each start hook wrote its stdin to an observable
file and returned a unique context token; each stop hook also wrote its stdin.
The child was instructed to report the hook token or
`HOOK_CONTEXT_ABSENT`.

Results:

1. Built-in `explorer`, project hook, no matcher: `SubagentStart` and
   `SubagentStop` both ran. The start payload contained
   `agent_type: "explorer"`, and the child returned the injected token.
2. Built-in `explorer`, project hook, exact `^explorer$` matcher: both hooks ran
   and the child again returned the injected token.
3. Custom `ateam-hook-spike`, project hook, no matcher: the custom profile ran,
   but neither hook produced a payload and the child returned
   `HOOK_CONTEXT_ABSENT`.
4. Custom `ateam_hook_spike`, project hook, exact
   `^ateam_hook_spike$` matcher: the custom profile ran, but neither hook
   produced a payload and the child returned `HOOK_CONTEXT_ABSENT`.
5. Custom `ateam_hook_spike` with an additional exact user-level hook: neither
   the project nor user hook produced a payload, and the child returned
   `HOOK_CONTEXT_ABSENT`. The temporary user hook was checksum-verified and
   removed immediately after the run.
6. Custom `ateam_plugin_hook_spike`, installed plugin hook, exact matcher: the
   validated plugin was installed from a temporary local marketplace and
   loaded in a fresh `codex exec` thread. The custom profile ran, but neither
   lifecycle hook produced a payload and the child returned
   `HOOK_CONTEXT_ABSENT`.
7. Built-in `explorer`, the same installed plugin hook after a matcher-only
   change and cachebuster reinstall: both lifecycle hooks ran, their payloads
   identified `agent_type: "explorer"`, and the child returned the plugin's
   injected context token.

The controls prove that project trust, hook trust bypass, command execution,
matcher syntax, hook stdout, and child context injection all worked in the
same checkout. Using both hyphenated and underscore custom names rules out the
obvious naming explanation. Capturing both lifecycle events also shows this is
not limited to the start hook: Codex 0.145.0 emitted neither
`SubagentStart` nor `SubagentStop` for these custom-agent spawns. The installed
plugin control proves this is not an artifact of testing loose project hooks:
plugin packaging changed hook discovery and paths, but did not change the
custom-agent event boundary.

The [current Codex hooks
documentation](https://learn.chatgpt.com/docs/hooks#subagentstart) says
`SubagentStart` matches on `agent_type`, receives `agent_id` and `agent_type`,
and can add developer context to the child. The observed result on Codex
0.145.0 therefore disagrees with the documented contract for custom agents.
The reproducible boundary is **built-in agent emits lifecycle events; custom
agent does not**, across project, user, and installed-plugin hook sources. The
internal implementation cause is not established from the public interface,
so the plan does not invent one.

This is a **hook-delivery caveat, not a custom-agent no-go**. The initial role
definitions must explicitly run `ateam learnings <role>` at startup rather
than rely on `SubagentStart` injection. That is deterministic and reconstructs
the same context from durable role memory.

Keep the workaround until a supported Codex version passes a regression probe:
the start hook must receive the custom `agent_type`, its returned
`additionalContext` must be visible inside the child, and the paired stop hook
must receive the same child identity.

### Concurrent resume — UNSAFE WITHOUT AN ATEAM LOCK

One process held a thread in an active tool call (`sleep 12`). While it was
still active, a second process ran:

```text
codex exec resume --json <same-thread-id> "<secondary prompt>"
```

The second turn was accepted immediately and completed
`SECONDARY_FINISHED`; the original turn later completed
`PRIMARY_FINISHED`. Codex CLI did not reject or serialize the concurrent
resume.

This confirms that the Codex runtime adapter must never infer safety from
`codex exec resume` itself. `ateam` needs a per-initiative worker lock covering
the full lifetime of every `codex exec` or `codex exec resume` process. Mail
sent while that lock is held leaves the doorbell pending; the Stop hook or the
supervisor's post-exit reconciliation consumes it. Only the lock holder may
start a turn.

### Phase 0 decision

**GO** for the loop-closing Codex slice, with these contracts frozen before
implementation:

1. Idle wake uses `codex exec resume <thread-id>`.
2. Active-worker mail uses a synchronous `Stop` continuation.
3. Every Codex worker is guarded by an ateam-owned per-initiative lock.
4. The worker supervisor reconciles pending mail immediately after process
   exit to close the Stop/exit race.
5. Custom role agents use self-contained non-full-history spawns.
6. Role learnings remain explicit in each agent definition until
   `SubagentStart` is proven reliable.
7. Setup and audit verify project trust and hook trust separately.

The app-server path is not required for the first implementation. It remains a
future option for richer status and turn control.

### Phase 0.5 addendum — app-server supervisor spike

The earlier CLI-first decision above is **superseded** by the
[2026-08-06 app-server lifecycle spike](./2026-08-06-codex-app-server-spike.html).

The spike proved that app-server provides the better turn-ownership boundary:

1. an active turn survives client disconnect;
2. active mail can be injected with `turn/steer`;
3. idle mail starts a new turn under the same thread;
4. killing the CLI launcher leaves the native app-server and turn reachable;
5. killing native app-server marks the old turn interrupted, after which the
   same thread resumes idle and accepts a recovery turn; and
6. graceful shutdown drains active work.

The revised direction is the managed app-server plus a short-lived delivery
coordinator, not a detached `codex exec` worker per turn or an ateam-owned
supervisor. An ateam-owned initiative delivery lock is still required:
`turn/start` while active was accepted but folded into the existing turn under
a surprising response ID, and transient `thread/read` snapshots were not
strong enough to serve as mail acknowledgments.

Before reshaping PR #160, settle the daemon deployment contract. On the tested
npm/mise installation, `codex app-server daemon start` required OpenAI's
standalone Codex installation. The next gate decides whether standalone Codex
is a prerequisite or ateam owns a user-service fallback.

### Phase 0.6 addendum — managed daemon decision

The deployment gate is now settled. Standalone Codex 0.146.1 exposed the
official managed daemon, which detached under parent PID 1, treated duplicate
start as idempotent, survived client disconnect, drained an active turn during
supported stop, and recovered the same thread after hard daemon death.

The first slice therefore requires standalone Codex and does not add an
always-running ateam supervisor. Each short-lived delivery attempt ensures the
managed daemon is running, acquires an initiative delivery lock, then selects
`turn/steer` for an active turn or `thread/resume` plus `turn/start` for an
idle/not-loaded thread. Beads unread mail remains authoritative.

Shared setup/audit logic must report Codex absent, incompatible non-standalone
Codex, or compatible standalone Codex. General setup warns for the optional
incompatible case; explicitly selecting the Codex runtime fails with an
actionable standalone-installer message.

### Phase 0.7 addendum — loop-closing vertical slice passed

The first Codex-native DRI slice passed a paid, live, isolated smoke on
2026-08-15.

The installed plugin now carries:

- `agent-teams-codex:dri`, a Beads-reconstructable DRI workflow built around
  bounded custom-role children rather than persistent Claude teammates;
- `agent-teams-codex:dispatch-dri`, which mechanically calls
  `ateam dispatch --runtime codex`;
- planner, implementer, tester, and reviewer custom agent definitions;
- SessionStart, UserPromptSubmit, and Stop mail hooks; and
- the shared local-main close-out helper required by `ateam close`.

Codex dispatch and resume explicitly name the installed namespaced DRI skill
instead of relying on fuzzy interpretation of a bare `/dri` prompt.

The live smoke created a disposable repository, project Beads database, and
global workspace, then dispatched initiative `at-p74` through the managed
app-server. The DRI:

1. reconstructed the initiative and root epic from Beads;
2. delegated planning to `agent-teams-planner`;
3. created an isolated implementation track and delegated it to
   `agent-teams-implementer`;
4. integrated commit `6907bee`;
5. delegated independent live verification to `agent-teams-tester`;
6. raised the exact `Smoke mail checkpoint` QUESTION gate and ended its turn;
7. received durable mail `at-wisp-yq8`, which started a second turn in the
   same thread `01a00678-23d3-7253-9600-dcb168ccdd06`;
8. ran `ateam mail inbox`, cleared the gate, removed its track, and closed the
   project epic plus initiative.

The run also exposed a managed-daemon environment boundary: a custom
`AGENT_TEAMS_HOME` set on the short-lived dispatcher is not inherited from
the already-running daemon process. The adapter now writes the resolved
workspace into Codex's sticky `shell_environment_policy.set` config on both
`thread/start` and `thread/resume`. The isolated DRI and all of its custom
children then resolved the correct workspace without prompt-level workarounds.
Reapplying the config on resume also repairs threads created by an older ateam
version.

This closes the first loop: managed dispatch, role delegation, durable state,
turn stop, mail wake, same-thread continuation, live verification, and
terminal cleanup all worked together. PR review and merge behavior remains the
normal next validation surface after PR #160 lands.
