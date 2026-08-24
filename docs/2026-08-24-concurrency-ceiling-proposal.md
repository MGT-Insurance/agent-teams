# Raising the concurrent-agent ceiling for the shared beads workspace

Status: proposal, awaiting approval. Nothing here is implemented. Approving it
authorizes the one-time migration in "What approval authorizes"; it does not
change the storage backend, start a daemon, or touch the sync path on its own.

## Summary

The global `~/.agent-teams` workspace runs beads on embedded Dolt. Embedded Dolt
takes an exclusive, whole-database lock at storage-open, before any query runs,
so concurrent `ateam`/`bd` calls do not run in parallel — they queue. On this
machine, with 15 or more live agent sessions, that queue is now long enough that
the per-prompt agent-teams hook is killed at its 30-second timeout, and the
dashboard's `bd` calls time out too.

We measured the problem instead of assuming it. Eric's hypothesis — embedded mode
is too slow under many concurrent agents — is **verified**, and the mechanism is
sharper than "slow storage": every call re-opens the 776 MB database in its own
process (~1.55 s), and the lock forces those opens to happen one at a time.

**Recommended: move the workspace to Dolt managed-server mode.** It is the only
option that structurally raises the ceiling — from roughly one effective
concurrent caller to 16–30 — it is *lighter* on memory rather than heavier, and,
critically, beads' managed server is self-healing: it is auto-started on demand
and recreated by the next call if it dies, so it carries none of the standing
maintenance burden a supervised daemon would. Sync is unaffected. This holds even
if the separate per-prompt hook fix lands first — that fix lowers the call rate
but does not raise the ceiling.

**Second option: cache the hot reads in files** so they never open the database.
This is daemon-free and reduces load sharply, but it does not raise the underlying
one-writer ceiling. A separate in-flight fix already applies this approach to the
per-prompt hook; this proposal would extend it. The two options are complementary,
not exclusive.

## The problem

Every `ateam` call shells out to the `bd` binary as a fresh process
(`internal/bd/bd.go:19`, `47-49`), so each call is a process spawn plus a Dolt
open. Under concurrency those opens serialize behind the exclusive lock at
`~/.agent-teams/.beads/embeddeddolt/.lock`.

The contention is visible across three separate consumers:

- The per-prompt `UserPromptSubmit` hook (`inbox-drain.sh`) opens the database
  three times per prompt and is killed at the 30-second hook timeout when several
  sessions overlap; the mail wake-signal is silently dropped when that happens.
- The initiative dashboard reports frequent `bd human list` / `ateam list-json`
  timeouts, attributed to the same workspace-lock contention, and has since July.
- Session-start hooks open the database five to seven times per start and are also
  killed at the timeout on busy startups.

The requirement is to tolerate 15 or more concurrent sessions, not to run fewer.

## What we measured

All figures are medians from an isolated copy of the live database (776 MB, ~822
issues) at a scratch path; the live workspace was never touched. The machine was
under real load (load average 7–9), so absolute latencies are inflated relative to
an idle box — the steward measured a single call at 0.786 s where we saw 1.75 s.
The *relative* comparisons (concurrent versus sequential, embedded versus server)
hold regardless, because each pair was measured in the same window. The core
serialization result is independently reproduced by three separate measurements
(the steward's original, the per-prompt-hook investigation, and this one).

### Embedded mode serializes

| Concurrent calls | Wall time | Throughput |
|---|---|---|
| 1 | 1.00 s | 1.0 / s |
| 2 | 2.76 s | 0.72 / s |
| 4 | 6.81 s | 0.59 / s |
| 8 | 9.12 s | 0.88 / s |
| 16 | 19.6 s | 0.8 / s |

Eight concurrent calls (9.12 s) cost essentially the same as eight run one after
another (9.75 s). Throughput is flat at roughly one call per second no matter how
many callers there are — the signature of a serializing lock. During the wait the
CPU sits about half idle and swap does not move, so this is neither a CPU nor a
memory bottleneck; the calls are queuing.

### Where the time goes

| Component | Time | How measured |
|---|---|---|
| Process spawn / binary load | 0.20 s | `bd version`, no database open |
| Database open | ~1.55 s | `bd show` minus the spawn floor |
| Lock wait | the rest | the queue delay concurrency adds |

The database open dominates process spawn by roughly eight to one. That matters
for the choice of fix: because the cost is the *repeated open* and not the spawn,
a shared server — which opens the database once and keeps it open — removes the
dominant cost. If spawn had dominated, a server would not have helped and the
answer would have been elsewhere.

### The lock is at open, and read-only does not help

The serialization happens at storage-open, before any query: a call that opens the
store but reads no table (`bd query "SELECT 1"`) still serializes. beads exposes a
`--readonly` flag, but it does not relax the serialization — it gates write
statements at the SQL layer while the engine still takes the exclusive open lock
regardless of intent. So there is no "open read-only to skip the lock" escape;
this is confirmed by two independent measurements and by the fact that `bd` 1.1.0
has no shared-reader open mode to invoke.

### Server mode removes the serialization

| Concurrent calls | Embedded wall | Server wall |
|---|---|---|
| 1 | 1.00 s | 0.15 s |
| 8 | 9.12 s | 0.52 s |
| 16 | 19.6 s | 0.50 s |
| 32 | — | 1.11 s |

A single call drops about 14-fold (1.75 s to 0.125 s). Eight concurrent calls run
*faster* than eight sequential ones, and the server uses multiple cores (measured
at 359% CPU at 16 callers) where embedded sat idle waiting on the lock.

### The ceiling

- **Embedded: about one effective concurrent caller.** Throughput never exceeds
  ~1 call/s; N callers take about N × 1.2 s. With 15 live agents, a call can wait
  behind roughly 15 others (~18 s).
- **Server: roughly 16–30 concurrent callers** before the CPU and per-call floor
  cap throughput at about 30 calls/s on this 10-core box. Past that it degrades
  gracefully — wall time grows linearly, with no collapse.

### Memory: the server is lighter, not heavier

This reframes the usual "a server costs resources" intuition. Embedded loads the
working set into *each* process — about 420 MB resident per concurrent `bd` call.
The lock accidentally protects the machine by serializing, so only about one heavy
process runs at a time. Removing the lock *in-process* (by allowing concurrent
embedded opens) would put 16 × 420 MB ≈ 6.7 GB on a 24 GB box that already runs
with several GB of swap. The managed server is a single shared process of roughly
30–55 MB with thin clients, so its memory is flat regardless of concurrency. On a
memory-bound machine, the server is the lighter option.

## Why this revisits a settled decision

Server mode was considered and rejected in June 2026 (design decision D16;
`docs/verifications.md:316`): "Server mode is not needed and was rejected. It
would add a daemon to keep running AND would not fix the app-layer RMW race
anyway."

That rejection was correct for the question it answered, which was **write
correctness**, not throughput. The June work verified that 12 concurrent writes to
distinct records all land (they serialize through the lock and succeed), and that
12 concurrent writes to the *same* record lose updates silently — an
application-layer read-modify-write race in commands like `bd note`. Server mode
does not fix that race, because it is a read-then-write at the application layer,
not a lock problem. That remains true, and it is orthogonal to this proposal: the
race is avoided by design (each initiative and bead has a single sequential owner),
and this initiative is about the throughput ceiling, not correctness.

What the June analysis did not weigh is the *latency* of the lock serialization at
15-plus concurrent sessions, because it treated serialization as correctness-neutral
("all succeed"). That is the new problem. The other prong of the rejection — the
daemon cost — is real and is priced honestly in the next section, rather than
treated as a veto.

## The operational cost of a long-lived process, priced honestly

A shared server is a long-lived process, and that carries genuine operational cost.
It is worth being precise about what that cost is and is not. There is no standing
decision against a long-lived process on this machine: the relay-supervision
question was raised but never actually put to a human, so "daemons were declined
here" is not an established constraint. What this proposal owes instead is an honest
account of the two real costs.

**Death on sleep — covered by self-heal.** beads' managed server is auto-started on
demand, not a supervised always-on process:

- With the workspace in server mode and no server running, the next `bd` call
  auto-starts one and succeeds. A truly cold first boot (cold filesystem cache)
  costs up to about 6.8 s once; warm calls are about 0.11–0.15 s.
- When the server dies — which is exactly what a Mac sleep does to it — the next
  `bd` call transparently restarts it. Two independent tests killed the running
  server and saw the next call print an informational "Dolt server endpoint changed
  … (auto-start)" and succeed against real data (0.4–0.9 s for the respawn). No
  manual step, no hard failure.
- Self-heal is governed by one config key, `dolt.auto-start`, which defaults to
  true. Setting it false makes a dead server fast-fail with "connection refused"
  instead — so the behavior is a deliberate default, not an accident.

So a sleep costs one call in roughly the 1–2 s range on wake (server respawn plus
the full-store open), then normal speed — well inside the 30-second hook timeout.
There is no `launchd` unit to maintain, and the server is per-workspace (the shared
`beads_global` database needs an explicit flag that agent-teams never passes; a
supervised always-on `launchd` server is a documented but separate opt-in, not this
default path).

**Staleness on upgrade — a real, bounded maintenance cost.** The prior art is the
relay, which once ran a week-stale binary because nothing restarted it after an
upgrade. A long-lived dolt server has the same shape: upgrade the dolt binary while
a server keeps running, and that server serves the old version until it is
restarted. Two things bound this in practice — the server restarts on death, and
machine sleep (frequent on a laptop) kills and restarts it, so it rarely outlives a
sleep cycle. It is a cost to watch, not a blocker; a one-line "restart the workspace
server on upgrade" step closes it without standing supervision.

## Sync is unaffected

beads ships one storage engine (Dolt) in several deployment modes; server mode is
still Dolt. Cross-machine sync via `refs/dolt/data`, the passive
`.beads/issues.jsonl` export, and the behavior of `bd dolt push`/`pull` are
identical across modes. Switching modes is lossless and reversible.

One constraint must be verified before implementation, not assumed: the
`__dolt_remote_info__` signage branch — force-pushed on every push and responsible
for 858 failing Vercel builds — is currently suppressed only by an empty
`DOLT_REMOTE_INFO_BRANCH` environment variable set in the login shell, with nothing
in the repository encoding it. A server-mode push must still run in a process that
inherits that empty variable. This is an environment fact to confirm on the actual
push path, not a property of the storage mode; the signage branch still exists on
the remote today and is only kept quiet, not removed.

## Options

### Option A — managed-server mode (recommended)

Move the workspace to Dolt managed-server mode.

- Raises the ceiling from ~1 to ~16–30 concurrent callers; ~14× lower single-call
  latency; ~30× throughput at 8 concurrent.
- Lighter on memory (one shared process versus 420 MB per concurrent call).
- Self-healing via auto-start; no supervised daemon, no `launchd`, sleep-tolerant.
- Sync, export, and history preserved.
- Costs: a one-time migration (below); a one-time ~6.8 s cold boot, and a ~1–2 s
  first-call penalty each time the server has to respawn (after a sleep, for
  example), then normal speed; a bounded staleness-on-upgrade maintenance cost; it
  does not fix the same-record read-modify-write race (orthogonal, avoided by
  design); and the signage-branch environment inheritance must be verified on the
  push path first.

### Option B — cache the hot reads in files (daemon-free; complementary)

Answer the frequent read-only calls from files so they never open Dolt:

- `resolve-initiative` (used by several hooks) from a write-maintained
  cwd→initiative index. The existing `issues.jsonl` export cannot serve this — it
  is abandoned (5 records, last written June 12) — so this needs a new index
  written on dispatch and close.
- The mail unread-count peek from a per-recipient signal file (the mailbox holds
  only empty wake-doorbells today, so the count hits Dolt).
- Role-learnings and user-memory reads (`bd memories --json`) from a per-role
  export regenerated when `ateam learn` writes.

This removes most callers from the lock queue and is daemon-free, but it does not
raise the fundamental one-writer ceiling — the remaining opens still serialize,
just far fewer of them. The per-prompt slice of this is already owned by a separate
in-flight fix for the hook timeout; this proposal would extend the same approach to
the heavier session-start and subagent-spawn reads it does not cover.

### Two routes, and how they relate

Option A raises the ceiling *and* ends the hook timeouts as a side effect (warm
calls are ~0.11 s), which would make the file-cache work optional. Option B ends
the timeouts and cuts load without a daemon, but leaves the ceiling in place. They
can also combine — a server for the ceiling, plus file caches to keep even the
server's call rate low. The recommendation is Option A because only it raises the
concurrent-agent ceiling, which is the stated goal. This holds even if the
per-prompt hook fix lands first: that fix removes three opens per prompt, but the
heavier session-start opens (five to seven per start), subagent-spawn reads,
wake-watcher arms, and on-demand verb calls all remain and still serialize.
Reducing the per-prompt rate lowers the load; it does not raise the ceiling.

## What approval authorizes

Approving Option A authorizes a one-time migration and a mode switch, to be planned
and delivered as its own change:

1. Relocate the workspace's Dolt data: move the `at` database directory from
   `.beads/embeddeddolt/at` to `.beads/dolt/at`, where the managed server looks for
   it, or re-clone it from the Dolt remote with `bd bootstrap`. A bare
   `dolt_mode=server` flip without moving the data yields `database "at" not found`
   — the server stands up an empty default database instead. (`bd dolt set
   data-dir` is refused in server mode, so the server cannot simply be pointed at
   the existing `embeddeddolt` directory.)
2. Verify the server-mode push path inherits the empty `DOLT_REMOTE_INFO_BRANCH`,
   so the signage branch stays suppressed.
3. Switch the workspace metadata to server mode and confirm auto-start, self-heal
   after a killed server, and a clean cross-machine `push`/`pull`.

It does not authorize a supervised daemon, a `launchd` unit, or any change to how
the machine is used.

## Relationship to sibling work

- The per-prompt hook-timeout fix (the `inbox-drain.sh` 30-second kills) is a
  separate, narrower effort. It set server mode aside on the daemon cost, citing a
  precedent — that an always-on process had been declined for the relay — that does
  not hold up: that decision was never actually made. It also did not test
  auto-start, self-heal, or the memory comparison. This initiative measured those
  and priced the real maintenance cost, and server mode stays a live candidate. Its
  lock root-cause finding, which this initiative independently confirmed, stands,
  and its file-cache fix remains valuable as the first consumer of Option B.
- The dashboard `bd`-timeout diagnosis is the same lock contention seen from
  another consumer; Option A resolves it for the dashboard as well.

## Appendix — method and sources

- Measurements: isolated `cp` of the live `.beads` (776 MB, ~822 issues) at a
  scratch path; `bd -C <copy>`; benchmark op `bd show` (a point read equivalent to
  `resolve-initiative`'s `bd list`, `internal/verbs/match.go:103`); `/usr/bin/time`;
  3–10 repetitions per condition; medians reported; machine under load average 7–9.
  The live workspace was confirmed untouched (still embedded, no server, port free)
  after every run.
- Mode is inherited, not forced: `ateam` passes no mode flag anywhere
  (`internal/bd/bd.go:47-49`; `git log -S'--server'` over `plugins/ internal/
  scripts/` returns zero commits). The workspace runs embedded because that is
  `bd init`'s default, written into `~/.agent-teams/.beads/metadata.json`
  (`"dolt_mode":"embedded"`) by the bare `bd init` in the setup skill.
- Prior decision: `docs/verifications.md:288-317` (concurrency, verified
  2026-06-11) and design decision D16 in
  `docs/2026-06-11-agent-teams-framework-design.md`.
- Backends and sync: beads docs `architecture/dolt.md` and
  `core-concepts/sync-concepts.md` (the older `SYNC_CONCEPTS.md` URL now 404s);
  `git -C ~/.agent-teams ls-remote` shows `refs/dolt/data` and the still-present
  `__dolt_remote_info__` branch.
