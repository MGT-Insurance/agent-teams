# Steward operations

How the Steward is launched, kept singleton, and removed from a machine — everything a human or launcher needs, and the running Steward itself does not (SKILL.md §1 covers what the Steward acts on at its own startup).

## Launching

**Exactly ONE steward session may run per machine.** The sanctioned launch:

```bash
ateam steward start
```

`steward start` is the one-command form of the full manual sequence: it runs `ateam steward init`, then a singleton pre-flight (refuses to launch — exit code 1, naming the live session's id — if a steward session is already running; fails soft, with a warning, if `claude agents` can't be queried), then orphan-watcher hygiene (a dead watcher pidfile is removed; a live orphaned watcher — which has left a relaunched steward deaf before — is killed and its pidfile removed), then launches. Under the hood, that last step is:

```bash
ateam steward init && cd "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}/steward/session" && claude --bg --permission-mode bypassPermissions "/agent-teams:steward"
```

`--permission-mode bypassPermissions` is required — a background steward launched without it hangs invisibly on its first permission prompt, with no one watching to approve it. Running `ateam steward init` BEFORE the session starts ensures the session marker exists before any SessionStart hook can fire for it.

`ateam steward init` is idempotent — safe to run again if you're ever unsure it's been done for this machine. Pure backstop: `steward start` already runs it.

## Disabling the Steward on a machine

Gate->Steward routing (`notifyToSteward`) is guarded on `StewardSessionMarkerPath` existing: a machine with no marker sees every `ateam gate` behave exactly as it did before the Steward existed (labels + park + dashboard only, no mail, no doorbell). This is what keeps a steward-less machine from accumulating unread steward-message beads forever.

Two ways to disable it (paths below are under the workspace root — `$AGENT_TEAMS_HOME`, default `~/.agent-teams`):

- **Manual**: delete `<workspace>/steward/session` (the marker lives inside it). Routing stops immediately; `ateam steward init` re-creates it idempotently if you want it back.
- **`ateam steward remove`**: the supported way to de-steward a machine. Removes the session dir (marker included) and the doorbell (`<workspace>/mailbox/steward.wake`); idempotent (nothing to remove is still a success). Keeps `<workspace>/steward/ledger.jsonl` and `<workspace>/steward/briefing-thread` by default and prints their paths — that's the state to copy over when relocating the Steward to another machine. Pass `--purge` to delete those too. It also reports (never modifies) how many unread messages are still assigned to the `steward` handle, so mid-flight mail is visible before you walk away.
