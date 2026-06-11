---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads is installed, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
---

Set up this machine for agent-teams. Work through these steps in order, reporting each result.

## 1. Verify beads

`bd --version`. If missing: STOP and tell the human — agent-teams hard-requires beads (https://github.com/gastownhall/beads). Do not improvise a fallback.

## 2. Resolve the workspace location

`ATH = ${AGENT_TEAMS_HOME:-$HOME/.agent-teams}`. If the human wants a non-default location, have them set `AGENT_TEAMS_HOME` in the `env` block of `~/.claude/settings.json` (applies to all future sessions), and use that value now.

## 3. Create or clone the workspace

Ask the human: do you already have an agent-teams memory remote (e.g. a private `agent-teams-memory` repo from another machine)?

### Existing remote → clone

```bash
git clone <remote-url> "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
(cd "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" && bd init --prefix at --non-interactive)
```

`bd init` detects the git origin and auto-bootstraps from `refs/dolt/data` — no separate `bd dolt pull` needed (and `bd dolt pull` alone is a footgun: it may pull from a wrong configured remote). Verify knowledge arrived:

```bash
bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories dri
```

### Fresh → init

**Step 1 — create the git repo and initialize beads** (`bd -C` does not work for `init`; a subshell is required):

```bash
mkdir -p "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}"
git -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" init
(cd "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" && bd init --prefix at --non-interactive)
```

**Step 2 — have the human create a private remote**, e.g.:

```
gh repo create <user>/agent-teams-memory --private
```

**Step 3 — wire up the git remote and push the initial commit** (the remote must have at least one commit before `bd dolt push`):

```bash
git -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remote add origin <url>
git -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" add -A
git -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" commit -m "init agent-teams workspace"
git -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" branch -M main
git -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" push -u origin main
```

**Step 4 — add the Dolt remote** (separate from the git remote, but can use the same URL) **and push the Dolt data**:

```bash
bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" dolt remote add origin <url>
bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" dolt push
```

## 4. Smoke test

Run on BOTH paths (clone or fresh) after step 3 completes:

1. Write a test memory:
   ```bash
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" remember --key "dri:setup-smoke" "setup smoke test. WHY: verify store. HOW TO APPLY: n/a."
   ```
2. Verify it appears:
   ```bash
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" memories dri
   ```
   Expected: shows `dri:setup-smoke`.
3. Sync roundtrip:
   ```bash
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" dolt push
   ```
   Expected: push succeeds.
4. Clean up the smoke entry and push again to leave the store clean:
   ```bash
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" forget dri:setup-smoke
   bd -C "${AGENT_TEAMS_HOME:-$HOME/.agent-teams}" dolt push
   ```

## 5. Report

Confirm to the human: workspace path, remote URL, smoke-test results, and that `/dri` is ready to use.
