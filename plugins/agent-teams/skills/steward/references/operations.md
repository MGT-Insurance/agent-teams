# Steward operations

How the Steward is launched, kept singleton, and removed from a machine — everything a human or launcher needs, and the running Steward itself does not (SKILL.md §1 covers what the Steward acts on at its own startup).

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

**Why the startup load is not enough on its own.** Compaction happens often in a persistent singleton, and the startup load — the same one the SubagentStart hook injects — compacts with everything else. That's why SKILL.md §2 requires `ateam steward ledger recall` and `ateam recall steward` to be re-run at decision time rather than trusted from startup. Same failure mode as the attribution rule in §2: what the session remembers is not a record.

**Wake plumbing.** The Steward wakes on mail arriving at the reserved `steward` handle (doorbell + wake-watcher machinery — see the hooks section of the plugin's CLAUDE.md) or on the periodic heartbeat.

## `ateam execution-status` — what the values mean

The startup load (SKILL.md §1), gate enrichment (§2), and every direct question about the landscape read this verb. One entry per open initiative: `id`, `title`, `worktree`, `labels`, `execution_status`, `ask`, `pr`. The statuses:

- `NEEDS-DECISION` — a question gate. Eric's, now.
- `IN-PROGRESS` — actively being worked, or open with no gate. Not his.
- `REVIEWABLE` — a PR awaiting ERIC that he has not yet looked at. This is the queue that means "you"; it does not mean "a PR exists."
- `AWAITING-EXTERNAL-REVIEW` — **healthy, not idle.** Eric has declared he is done looking (references/handoff.md); the PR is with third-party reviewers. NOT ours and NOT his.
- `unknown` — `claude agents --json` failed; every row degrades to this and no status is trustworthy for that run.

**`AWAITING-EXTERNAL-REVIEW` is the trap.** It is the one healthy state that looks exactly like a stall: no live session, no movement, a gate label still on the bead. Chasing things that look idle is your whole job, so absent an explicit rule you will chase this one and generate precisely the noise the state exists to remove. Explicitly, then: do NOT nudge it, do NOT surface it in a briefing as needing attention, do NOT count it as stalled or hung, do NOT ask Eric about it. It leaves the state only when he says so (`ateam handoff <id> --clear`), or when the DRI resumes and runs `ateam clear-gate`.

`hung-scan` never sees it as a problem either: a handed-off initiative keeps its `human` + `gate:review` pair, so it still classifies AWAITING-HUMAN and SKILL.md §2's scan bullets already say "no action." That pair is retained deliberately — dropping it would arm both the DEAD escalation ladder and the work-product flatline trip (`internal/verbs/external_review.go` §2).

## `ateam hung-scan` — the full field list

SKILL.md §2's scan bullets key off a subset of these. One JSON entry per open initiative, classified `WORKING` / `AWAITING-HUMAN` / `DEAD` / `STUCK`, plus:

- `hung` — live session idle past the threshold.
- `cwd_present` / `pid_present` — whether the worktree and the process still exist.
- `mode` — `bg` or `interactive`. `interactive` is excluded from every mechanical wake path.
- `dead_hung` — DEAD-with-worktree past 15 min. For these, `claude respawn <shortid>`'s argument isn't in this JSON — cross-reference `claude agents --all --json` by worktree to find it.
- Work-product fields: `wp_last_progress_at`, `wp_flat_seconds`, `wp_trip_eligible`, `failure_tokens_found`.

`wp_trip_eligible:true` is the busy-forever case, and means all three of: `mode:bg`, git/bead artifacts flat for ≥30 min, and a claimed bead. The mechanical wake carries that evidence with it, so the Steward does not recompute it.

## Ledger CLI

`ateam steward ledger record` REJECTS a `corrected` verdict submitted without `--decision` — the flag is required there and optional on `accepted`. The rejection is deliberate: a `corrected` row with no record of what Eric actually decided is the one shape of ledger entry that teaches nothing.

## Disabling the Steward on a machine

Gate->Steward routing (`notifyToSteward`) is guarded on `StewardSessionMarkerPath` existing: a machine with no marker sees every `ateam gate` behave exactly as it did before the Steward existed (labels + park + dashboard only, no mail, no doorbell). This is what keeps a steward-less machine from accumulating unread steward-message beads forever.

Two ways to disable it (paths below are under the workspace root — `$AGENT_TEAMS_HOME`, default `~/.agent-teams`):

- **Manual**: delete `<workspace>/steward/session` (the marker lives inside it). Routing stops immediately; `ateam steward init` re-creates it idempotently if you want it back.
- **`ateam steward remove`**: the supported way to de-steward a machine. Removes the session dir (marker included) and the doorbell (`<workspace>/mailbox/steward.wake`); idempotent (nothing to remove is still a success). Keeps THREE files by default — `<workspace>/steward/ledger.jsonl`, `<workspace>/steward/briefing-thread` and `<workspace>/steward/reviews-thread` — and prints them under `kept (carry these when relocating the Steward to another machine):`. Copy over all three, not just the ledger: each thread-ref file is per-machine storage, and `reviews-thread` is what binds this machine to the shared Reviews topic, so leaving it behind makes the new machine's first review open a SECOND "Reviews" topic with no error anywhere. Pass `--purge` to delete all three instead. It also reports (never modifies) how many unread messages are still assigned to the `steward` handle, so mid-flight mail is visible before you walk away.
