# Beads server mode (global workspace)

The global `~/.agent-teams` workspace runs Dolt in **server mode** as of September 15, 2026. Project repos run **embedded**. This doc explains why, how to migrate a workspace, and how the server is operated.

## Why server mode for the global workspace

Embedded Dolt takes an exclusive lock at storage-open, before any query, so every concurrent `bd`/`ateam` call — reads included — serializes. Under 15 or more concurrent sessions whose hooks each invoke `ateam`, that lock pushed per-prompt hooks past their 30-second timeout.

Server mode runs one shared `dolt sql-server`; concurrent callers connect over the MySQL wire and run in parallel. Benchmark on an Apple Silicon Mac (`bd list`, 100 issues, median of 3, warm-up discarded):

| N concurrent | embedded wall | server wall | speedup |
|---|---|---|---|
| 8 | 3.22s | 0.28s | 11.4x |
| 16 | 6.01s | 0.53s | 11.3x |
| 24 | 8.46s | 0.80s | 10.6x |

The serialization ratio wall(N)/wall(1) is close to N for embedded (fully serial) and stays flat for server. Server mode raised the parallel-call ceiling roughly 10–11x. The residual server climb at high N is client-side process-spawn cost, not storage serialization.

Server mode does **not** change write correctness. The same-record read-modify-write race described in `verifications.md` §6 is unaffected, and it is avoided by design in both modes.

## Which workspaces use which mode

- **Global `~/.agent-teams`:** server. It is the machine-wide hot path — every session hits it through hooks.
- **Project repos (agent-teams, midgard, and others):** embedded. Per-repo concurrency is far lower. Move a project repo to server only when it measurably contends, and verify the git-worktree interaction first — a worktree checkout carries no local Dolt data dir.

Mode is **per-machine**. `.beads/metadata.json` is git-tracked, but `ateam sync` only runs the Dolt push/pull, never the git working tree, so flipping one machine does not propagate to another. An embedded machine and a server machine sync the same repo through `refs/dolt/data` in both directions (verified across a bd/dolt version gap).

## Migration runbook (embedded to server)

Run these against the target workspace; the paths shown are for the global workspace. Confirm nothing is writing during the sub-second move.

1. Push everything first, as insurance:
   ```bash
   ateam sync
   ```
2. Move the data from the embedded location to the server location. `<db>` is the `dolt_database` value in `metadata.json`, for example `at`:
   ```bash
   mkdir -p ~/.agent-teams/.beads/dolt
   mv ~/.agent-teams/.beads/embeddeddolt/<db> ~/.agent-teams/.beads/dolt/<db>
   ```
3. Set `"dolt_mode": "server"` in `~/.agent-teams/.beads/metadata.json`.
4. Start the server once. This allocates a port and writes `.beads/dolt-server.port`:
   ```bash
   bd -C ~/.agent-teams dolt start
   ```
5. Verify the server answers and the data reads back:
   ```bash
   bd -C ~/.agent-teams dolt show      # expect: Server connection OK
   ```
6. Push once, then confirm the signage branch stayed dead:
   ```bash
   ateam sync
   git -C ~/.agent-teams ls-remote origin 'refs/heads/__dolt_remote_info__'   # expect no output
   ```

The move is lossless — it carries full history, including unpushed commits. `.beads/dolt/` is gitignored.

To roll back, reverse the move: run `bd -C ~/.agent-teams dolt stop`, move `dolt/<db>` back to `embeddeddolt/<db>`, and set `"dolt_mode": "embedded"`.

## Operating the server

- **Starting it:** after the one-time migration start, you don't. bd auto-starts the server on the next database call when it is not running (`bd dolt --help`: "auto-started transparently when needed").
- **Mac sleep:** the server can stop on sleep; the next `bd`/`ateam` call respawns it — one slightly slower call, then normal.
- **Reboot:** the server is not running until the first `bd`/`ateam` call, which starts it.
- **It is not a supervised daemon.** Use `bd dolt start` / `stop` / `status` for explicit control and diagnostics.
- **Do not wire it into launchd, cron, or a login item.** A server spawned from a non-login shell does not inherit the empty `DOLT_REMOTE_INFO_BRANCH` from `~/.zshenv`, which resurrects the `__dolt_remote_info__` signage branch and its CI build churn. Demand-start is the correct startup mechanism.

## The `__dolt_remote_info__` signage branch

`DOLT_REMOTE_INFO_BRANCH` is set empty (in `~/.zshenv` and `~/.claude/settings.json`) to suppress the `__dolt_remote_info__` branch, which otherwise triggers failing CI builds on repos whose branches build. A bd-started server inherits that empty value and writes no signage. After any migration, or on a new machine, confirm suppression holds:

```bash
git -C <workspace> ls-remote origin 'refs/heads/__dolt_remote_info__'   # expect no output
```

If the branch reappears, a background process is pushing without the suppression env. Find that process rather than only deleting the branch.

## Versions

The tested pair is **bd 1.1.0 + standalone dolt 2.1.10**. Server mode shells out to whichever `dolt` is on `PATH`, so its engine version is that binary, independent of bd's linked embedded engine. Do not upgrade past the tested pair without re-verifying cross-machine sync and the migration: dolt 2.3.0 through 2.3.2 carried a `DOLT_RESET('--hard')` regression (dolt #11581, fixed in 2.3.3). Upgrading beads with `brew upgrade beads` pulls a matching dolt as a dependency; treat that as its own change with its own verification.
