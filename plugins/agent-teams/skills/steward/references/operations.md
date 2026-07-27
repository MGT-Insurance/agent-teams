# Steward operations

How the Steward is launched, kept singleton, and removed from a machine — everything a human or launcher needs, and the running Steward itself does not (SKILL.md §1 covers what the Steward acts on at its own startup).

## The `ateam` binary

`ateam` ships as a prebuilt per-platform binary in the plugin's `bin/`, which is auto-added to PATH; `/setup-agent-teams` installs and verifies it. That is why SKILL.md calls it as a bare `ateam` everywhere and never as a path.

## Launching

**Exactly ONE steward session may run per machine.** The sanctioned launch:

```bash
ateam steward start
```

`steward start` is the one-command form of the full manual sequence: it runs `ateam steward init`, then a singleton pre-flight (refuses to launch — exit code 1, naming the live session's id — if a steward session is already running; fails soft, with a warning, if `claude agents` can't be queried), then orphan-watcher hygiene (a dead watcher pidfile is removed; a live orphaned watcher — which has left a relaunched steward deaf before — is killed and its pidfile removed), then launches. Under the hood, that last step is:

```bash
ateam steward init && cd "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}/steward/session" && claude --bg --permission-mode bypassPermissions --settings '{"env":{"ATEAM_ROLE":"steward"}}' "/agent-teams:steward"
```

`--permission-mode bypassPermissions` is required — a background steward launched without it hangs invisibly on its first permission prompt, with no one watching to approve it. Running `ateam steward init` BEFORE the session starts ensures the session marker exists before any SessionStart hook can fire for it.

`ateam steward init` is idempotent — safe to run again if you're ever unsure it's been done for this machine. Pure backstop: `steward start` already runs it.

## Why SKILL.md §1's startup order is what it is

**The duplicate check comes before the inbox drain, and its own guard is not enough.** `ateam mail inbox` carries a session-of-record guard that backstops a duplicate steward if the Step 0 check is skipped — but it is a backstop, not the mechanism. Draining mail as a duplicate consumes the incumbent's unread messages, and the guard only narrows the window in which that happens. Check first; don't lean on the guard.

**Why all three context loads, before anything else.** `ateam steward ledger stats` gives per-category accepted/corrected counts, so the Steward knows its own track record before it makes new recommendations. `ateam execution-status` is the machine-wide overview of every open initiative — id, title, execution status, pending ask, PR link — so a wake is not the Steward's first look at the landscape. Both, plus `ateam learnings steward`, load before any inbox work.

**Why the startup load is not enough on its own.** Compaction happens often in a persistent singleton, and the startup load compacts with everything else — which is why SKILL.md §2 requires `ateam steward ledger recall` and `ateam recall steward` to be re-run at decision time rather than trusted from startup. Same failure mode as the attribution rule in §2: what the session remembers is not a record.

**Wake plumbing.** The Steward wakes on mail arriving at the reserved `steward` handle (doorbell + wake-watcher machinery — see the hooks section of the plugin's CLAUDE.md) or on the periodic heartbeat.

## `ateam hung-scan` — the full field list

SKILL.md §2's scan bullets key off a subset of these. One JSON entry per open initiative, classified `WORKING` / `AWAITING-HUMAN` / `DEAD` / `STUCK`, plus:

- `hung` — live session idle past the threshold.
- `cwd_present` / `pid_present` — whether the worktree and the process still exist.
- `mode` — `bg` or `interactive`. `interactive` is excluded from every mechanical wake path.
- `dead_hung` — DEAD-with-worktree past 15 min.
- Work-product fields: `wp_last_progress_at`, `wp_flat_seconds`, `wp_trip_eligible`, `failure_tokens_found`.

`wp_trip_eligible:true` is the busy-forever case, and means all three of: `mode:bg`, git/bead artifacts flat for ≥30 min, and a claimed bead. The mechanical wake carries that evidence with it, so the Steward does not recompute it.

## Learnings tiers

`ateam learnings steward` auto-injects only the hot+fresh tiers — that is what the startup load and the SubagentStart hook inject. `ateam recall steward <query>` is a substring search over key+body across the FULL set, cold and archived entries included, printing matches directly. Use `recall` at decision time; `learnings` is the ambient tier.

## Ledger CLI

`ateam steward ledger record` REJECTS a `corrected` verdict submitted without `--decision` — the flag is required there and optional on `accepted`. The rejection is deliberate: a `corrected` row with no record of what Eric actually decided is the one shape of ledger entry that teaches nothing.

## Disabling the Steward on a machine

Gate->Steward routing (`notifyToSteward`) is guarded on `StewardSessionMarkerPath` existing: a machine with no marker sees every `ateam gate` behave exactly as it did before the Steward existed (labels + park + dashboard only, no mail, no doorbell). This is what keeps a steward-less machine from accumulating unread steward-message beads forever.

Two ways to disable it (paths below are under the workspace root — `$AGENT_TEAMS_HOME`, default `~/.agent-teams`):

- **Manual**: delete `<workspace>/steward/session` (the marker lives inside it). Routing stops immediately; `ateam steward init` re-creates it idempotently if you want it back.
- **`ateam steward remove`**: the supported way to de-steward a machine. Removes the session dir (marker included) and the doorbell (`<workspace>/mailbox/steward.wake`); idempotent (nothing to remove is still a success). Keeps `<workspace>/steward/ledger.jsonl` and `<workspace>/steward/briefing-thread` by default and prints their paths — that's the state to copy over when relocating the Steward to another machine. Pass `--purge` to delete those too. It also reports (never modifies) how many unread messages are still assigned to the `steward` handle, so mid-flight mail is visible before you walk away.
