---
name: setup-agent-teams
description: One-time machine setup for the agent-teams framework. Verifies beads is installed, creates or clones the global agent-teams workspace (role learnings + initiative registry), configures its git remote for cross-machine sync, installs the ateam launcher, and smoke-tests the loop. Use on a new machine, or when /dri reports the workspace is missing.
---

Set up this machine for agent-teams. Work through these steps in order, reporting each result.

If you set AGENT_TEAMS_HOME to a custom path, use that literal path in place of `~/.agent-teams` below.

## 1. Verify beads

`bd --version`. If missing: STOP and tell the human — agent-teams hard-requires beads (https://github.com/gastownhall/beads). Do not improvise a fallback.

## 2. Resolve the workspace location

The default workspace path is `~/.agent-teams`. If the human wants a non-default location, have them set `AGENT_TEAMS_HOME` in the `env` block of `~/.claude/settings.json` (applies to all future sessions), and use that literal path in place of `~/.agent-teams` in every command below.

## 3. Create or clone the workspace

Ask the human: do you already have an agent-teams memory remote (e.g. a private `agent-teams-memory` repo from another machine)?

### Existing remote → clone

```bash
git clone <remote-url> ~/.agent-teams
(cd ~/.agent-teams && bd init --prefix at --non-interactive)
```

`bd init` detects the git origin and auto-bootstraps from `refs/dolt/data` — no separate `bd dolt pull` needed (and `bd dolt pull` alone is a footgun: it may pull from a wrong configured remote). Verify knowledge arrived:

```bash
bd -C ~/.agent-teams memories dri
```

### Fresh → init

**Step 1 — create the git repo and initialize beads** (`bd -C` does not work for `init`; a subshell is required):

```bash
mkdir -p ~/.agent-teams
git -C ~/.agent-teams init
(cd ~/.agent-teams && bd init --prefix at --non-interactive)
```

**Step 2 — have the human create a private remote**, e.g.:

```
gh repo create <user>/agent-teams-memory --private
```

**Step 3 — wire up the git remote and push the initial commit** (the remote must have at least one commit before `bd dolt push`):

```bash
git -C ~/.agent-teams remote add origin <url>
git -C ~/.agent-teams add -A
git -C ~/.agent-teams commit -m "init agent-teams workspace"
git -C ~/.agent-teams branch -M main
git -C ~/.agent-teams push -u origin main
```

**Step 4 — add the Dolt remote** (separate from the git remote, but can use the same URL) **and push the Dolt data**:

```bash
bd -C ~/.agent-teams dolt remote add origin <url>
bd -C ~/.agent-teams dolt push
```

## 4. Allowlist the `ateam` script

Skills and agents invoke the workspace tool as a literal absolute path resolved from the plugin directory — no symlink, no install step. You need to allowlist that path once so future agent sessions can call it without a permission prompt.

Resolve the absolute path now. This skill lives at `<plugin-root>/skills/setup-agent-teams/SKILL.md`, so the script is two levels up then `scripts/ateam`: `<plugin-root>/scripts/ateam`. For example, if the plugin repo is at `/Users/you/Code/agent-teams`, the path is `/Users/you/Code/agent-teams/plugins/agent-teams/scripts/ateam`.

Tell the human to add the following entry to the `permissions.allow` array in `~/.claude/settings.json`, substituting the real absolute path you resolved:

```
"Bash(/Users/you/Code/agent-teams/plugins/agent-teams/scripts/ateam:*)"
```

Note: this path changes if the plugin is reinstalled at a new location — re-allowlist then. (WS3 will ship `ateam` as a real CLI on PATH, removing this friction.)

Verify the script works (using the resolved absolute path):

```bash
<plugin-root>/scripts/ateam ws
```

Expected: prints the workspace path (e.g. `/Users/you/.agent-teams`).

## 5. Smoke test

Run on BOTH paths (clone or fresh) after step 4 completes. Use the resolved absolute path to `<plugin-root>/scripts/ateam` (the same path you identified in step 4) for every `<ateam>` call below.

1. Write a test memory to a temp file and record it:
   ```bash
   # Write the content first (no inline string — avoids the newline-# prompt)
   ```
   Use the Write tool to create `/tmp/ateam-smoke.txt` with content:
   `setup smoke test. WHY: verify store. HOW TO APPLY: n/a.`

   Then record it:
   ```bash
   <plugin-root>/scripts/ateam learn dri setup-smoke --file /tmp/ateam-smoke.txt
   ```

2. Verify it appears:
   ```bash
   <plugin-root>/scripts/ateam learnings dri
   ```
   Expected: shows `dri:setup-smoke`.

3. Sync roundtrip:
   ```bash
   <plugin-root>/scripts/ateam sync
   ```
   Expected: push succeeds.

4. Clean up the smoke entry and push again to leave the store clean:
   ```bash
   bd -C ~/.agent-teams forget dri:setup-smoke
   <plugin-root>/scripts/ateam sync
   ```

## 6. Report

Confirm to the human: workspace path, remote URL, resolved `ateam` script path, smoke-test results, and that `/dri` is ready to use.
